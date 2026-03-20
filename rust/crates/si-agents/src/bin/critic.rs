use chrono::Utc;
use regex::Regex;
use serde::{Deserialize, Serialize};
use std::env;
use std::ffi::CString;
use std::fs;
use std::io::{BufRead, BufReader};
use std::os::unix::ffi::OsStrExt;
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::process::{Command, ExitCode};
use std::thread;
use std::time::{Duration, Instant};

const REPORT_BEGIN_MARKER: &str = "<<WORK_REPORT_BEGIN>>";
const REPORT_END_MARKER: &str = "<<WORK_REPORT_END>>";
const DYAD_TMUX_HISTORY_LIMIT: &str = "200000";
const TURN_ID_PREFIX: &str = "si-dyad-turn-id:";
const TASK_BOARD_PATH: &str = "/root/.si/TASK_BOARD.md";
const CONFIG_HEADER: &str = "# managed by critic";

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(message) => {
            eprintln!("critic: {message}");
            ExitCode::from(1)
        }
    }
}

fn run() -> Result<(), String> {
    ensure_codex_base_config()?;
    let cfg = load_loop_config();
    run_critic_loop(&cfg)
}

fn ensure_codex_base_config() -> Result<(), String> {
    let config_path = codex_config_path();
    let Some(parent) = config_path.parent() else {
        return Err(format!("invalid codex config path: {}", config_path.display()));
    };
    fs::create_dir_all(parent).map_err(|err| format!("mkdir {}: {err}", parent.display()))?;
    maybe_apply_host_ownership(parent);

    let browser_url = browser_mcp_url_from_env();
    let browser_section =
        browser_url.as_ref().map(|url| format!("[mcp_servers.browser]\nurl = \"{url}\"\n"));
    if !config_path.exists() {
        let mut content = String::from(CONFIG_HEADER);
        content.push('\n');
        content.push('\n');
        content.push_str("model_reasoning_effort = \"high\"\n");
        if let Some(section) = browser_section {
            content.push('\n');
            content.push_str(&section);
        }
        write_text_file(&config_path, &content)?;
        return Ok(());
    }

    if let Some(section) = browser_section {
        let existing = fs::read_to_string(&config_path)
            .map_err(|err| format!("read {}: {err}", config_path.display()))?;
        if !existing.contains("[mcp_servers.browser]") {
            let mut updated = existing.trim_end().to_string();
            updated.push_str("\n\n");
            updated.push_str(&section);
            updated.push('\n');
            write_text_file(&config_path, &updated)?;
        }
    }
    Ok(())
}

fn browser_mcp_url_from_env() -> Option<String> {
    if env_is_true("SI_BROWSER_MCP_DISABLED") {
        return None;
    }
    let internal = env_or_empty("SI_BROWSER_MCP_URL_INTERNAL");
    if !internal.is_empty() {
        return Some(internal);
    }
    let public = env_or_empty("SI_BROWSER_MCP_URL");
    if !public.is_empty() {
        return Some(public);
    }
    let container = env_or("SI_BROWSER_CONTAINER", "si-playwright-mcp-headed");
    let port = env_or("SI_BROWSER_MCP_PORT", "8931");
    Some(format!("http://{container}:{port}/mcp"))
}

fn codex_config_path() -> PathBuf {
    if let Some(home) = env::var_os("CODEX_HOME") {
        let trimmed = home.to_string_lossy().trim().to_string();
        if !trimmed.is_empty() {
            return PathBuf::from(trimmed).join("config.toml");
        }
    }
    PathBuf::from(env_or("HOME", "/root")).join(".codex").join("config.toml")
}

#[derive(Clone, Debug)]
struct LoopConfig {
    enabled: bool,
    dyad_name: String,
    actor_container: String,
    goal: String,
    state_dir: PathBuf,
    sleep_interval: Duration,
    startup_delay: Duration,
    turn_timeout: Duration,
    max_turns: usize,
    retry_max: usize,
    retry_base: Duration,
    seed_critic_prompt: String,
    prompt_lines: usize,
    allow_mcp_startup: bool,
    capture_mode: String,
    capture_lines: usize,
    strict_report: bool,
    codex_start_cmd: String,
    pause_poll: Duration,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
struct LoopState {
    #[serde(default)]
    turn: usize,
    #[serde(default)]
    last_actor_report: String,
    #[serde(default)]
    last_critic_report: String,
    #[serde(default)]
    updated_at: String,
}

#[derive(Clone, Debug)]
struct CodexTurnExecutor {
    actor_container: String,
    actor_session: String,
    critic_session: String,
    dyad_name: String,
    prompt_lines: usize,
    allow_mcp: bool,
    capture_mode: String,
    capture_lines: usize,
    strict_report: bool,
    start_cmd: String,
    ready_timeout: Duration,
    turn_timeout: Duration,
    poll_interval: Duration,
}

#[derive(Clone, Debug, Default)]
struct TmuxRunner {
    prefix: Vec<String>,
}

#[derive(Clone, Debug)]
struct StatusOptions {
    capture_mode: String,
    capture_lines: usize,
}

#[derive(Clone, Debug)]
struct PromptSegment {
    lines: Vec<String>,
    raw: Vec<String>,
}

fn load_loop_config() -> LoopConfig {
    let dyad = env_or("DYAD_NAME", "unknown");
    let member = env_or("DYAD_MEMBER", "critic").to_lowercase();
    let default_enabled = member == "critic" && !env_or_empty("ACTOR_CONTAINER").is_empty();
    let state_dir = match env_or_empty("DYAD_STATE_DIR") {
        value if !value.is_empty() => PathBuf::from(value),
        _ => default_dyad_state_dir(&dyad),
    };
    let goal = match env_or_empty("DYAD_LOOP_GOAL") {
        value if !value.is_empty() => value,
        _ => "Continuously improve the task outcome through actor execution and critic review."
            .to_string(),
    };
    LoopConfig {
        enabled: env_bool("DYAD_LOOP_ENABLED", default_enabled),
        dyad_name: dyad,
        actor_container: env_or_empty("ACTOR_CONTAINER"),
        goal,
        state_dir,
        sleep_interval: env_duration_seconds("DYAD_LOOP_SLEEP_SECONDS", 20),
        startup_delay: env_duration_seconds("DYAD_LOOP_STARTUP_DELAY_SECONDS", 2),
        turn_timeout: env_duration_seconds("DYAD_LOOP_TURN_TIMEOUT_SECONDS", 900),
        max_turns: env_int("DYAD_LOOP_MAX_TURNS", 0).max(0) as usize,
        retry_max: env_int("DYAD_LOOP_RETRY_MAX", 3).max(1) as usize,
        retry_base: env_duration_seconds("DYAD_LOOP_RETRY_BASE_SECONDS", 2),
        seed_critic_prompt: env_or_empty("DYAD_LOOP_SEED_CRITIC_PROMPT"),
        prompt_lines: env_int("DYAD_LOOP_PROMPT_LINES", 3).max(1) as usize,
        allow_mcp_startup: env_bool("DYAD_LOOP_ALLOW_MCP_STARTUP", false),
        capture_mode: env_or("DYAD_LOOP_TMUX_CAPTURE", "main").to_lowercase(),
        capture_lines: env_int("DYAD_LOOP_TMUX_CAPTURE_LINES", 8000).max(500) as usize,
        strict_report: env_bool("DYAD_LOOP_STRICT_REPORT", false),
        codex_start_cmd: env_or_empty("DYAD_CODEX_START_CMD"),
        pause_poll: env_duration_seconds("DYAD_LOOP_PAUSE_POLL_SECONDS", 5),
    }
}

fn run_critic_loop(cfg: &LoopConfig) -> Result<(), String> {
    if !cfg.enabled {
        log_line("critic loop disabled");
        return Ok(());
    }
    let reports_dir = cfg.state_dir.join("reports");
    fs::create_dir_all(&reports_dir)
        .map_err(|err| format!("mkdir {}: {err}", reports_dir.display()))?;
    maybe_apply_host_ownership(&reports_dir);

    if !cfg.startup_delay.is_zero() {
        thread::sleep(cfg.startup_delay);
    }

    let executor = CodexTurnExecutor::new(cfg)?;
    loop {
        match run_turn_loop(cfg, &executor) {
            Ok(()) => {}
            Err(err) if err == "critic requested stop" => {
                log_line("critic loop stopped by critic request");
            }
            Err(err) => {
                log_line(&format!("critic loop iteration failed: {err}"));
            }
        }
        if cfg.sleep_interval.is_zero() {
            return Ok(());
        }
        thread::sleep(cfg.sleep_interval);
    }
}

fn run_turn_loop(cfg: &LoopConfig, executor: &CodexTurnExecutor) -> Result<(), String> {
    let state_path = cfg.state_dir.join("loop-state.json");
    let mut state = load_loop_state(&state_path)?;

    if critic_requests_stop(&state.last_critic_report) {
        return Ok(());
    }

    if state.last_critic_report.trim().is_empty() {
        let seed_prompt = build_seed_critic_prompt(cfg);
        let (raw, report) = run_with_retries(cfg, "critic", |timeout| {
            let raw = executor.critic_turn(timeout, &seed_prompt)?;
            let report = extract_delimited_work_report(&raw)
                .ok_or_else(|| "critic seed output missing message".to_string())?;
            if critic_message_looks_placeholder(&report) {
                return Err("critic seed output looks placeholder/empty".to_string());
            }
            Ok((raw, report))
        })?;
        write_turn_artifacts(&cfg.state_dir, 0, "critic", &seed_prompt, &raw, &report)?;
        state.last_critic_report = report;
        state.updated_at = Utc::now().to_rfc3339();
        save_loop_state(&state_path, &state)?;
    }

    loop {
        if cfg.max_turns > 0 && state.turn >= cfg.max_turns {
            return Ok(());
        }
        let (stop, pause) = read_loop_control(&cfg.state_dir);
        if stop {
            return Ok(());
        }
        if pause {
            thread::sleep(cfg.pause_poll);
            continue;
        }

        let next_turn = state.turn + 1;
        match run_single_turn(cfg, next_turn, &mut state, executor) {
            Ok(stop_requested) => {
                save_loop_state(&state_path, &state)?;
                if stop_requested {
                    return Ok(());
                }
            }
            Err(err) if err == "critic requested stop" => {
                save_loop_state(&state_path, &state)?;
                return Ok(());
            }
            Err(err) => return Err(err),
        }
    }
}

fn run_single_turn(
    cfg: &LoopConfig,
    turn: usize,
    state: &mut LoopState,
    executor: &CodexTurnExecutor,
) -> Result<bool, String> {
    let actor_prompt = state.last_critic_report.trim().to_string();
    if actor_prompt.is_empty() {
        return Err("cannot run actor turn: missing prior critic message".to_string());
    }

    let (actor_raw, actor_report) = run_with_retries(cfg, "actor", |timeout| {
        let raw = executor.actor_turn(timeout, &actor_prompt)?;
        let report = extract_delimited_work_report(&raw)
            .ok_or_else(|| "actor output missing work report markers".to_string())?;
        if actor_report_looks_placeholder_with_mode(&report, cfg.strict_report) {
            return Err("actor output looks placeholder".to_string());
        }
        Ok((raw, report))
    })?;
    write_turn_artifacts(&cfg.state_dir, turn, "actor", &actor_prompt, &actor_raw, &actor_report)?;
    append_task_board_turn_log(TASK_BOARD_PATH, &cfg.dyad_name, turn, &actor_report);

    let critic_prompt = actor_report.clone();
    let (critic_raw, critic_report) = run_with_retries(cfg, "critic", |timeout| {
        let raw = executor.critic_turn(timeout, &critic_prompt)?;
        let report = extract_delimited_work_report(&raw)
            .ok_or_else(|| "critic output missing message".to_string())?;
        if critic_message_looks_placeholder(&report) {
            return Err("critic output looks placeholder/empty".to_string());
        }
        Ok((raw, report))
    })?;
    write_turn_artifacts(
        &cfg.state_dir,
        turn,
        "critic",
        &critic_prompt,
        &critic_raw,
        &critic_report,
    )?;

    state.turn = turn;
    state.last_actor_report = actor_report;
    state.last_critic_report = critic_report;
    state.updated_at = Utc::now().to_rfc3339();
    log_line(&format!("dyad turn {turn} complete"));
    if critic_requests_stop(&state.last_critic_report) {
        return Err("critic requested stop".to_string());
    }
    Ok(false)
}

fn run_with_retries<F>(cfg: &LoopConfig, label: &str, mut f: F) -> Result<(String, String), String>
where
    F: FnMut(Duration) -> Result<(String, String), String>,
{
    let mut last_err = String::new();
    for attempt in 1..=cfg.retry_max.max(1) {
        match f(cfg.turn_timeout) {
            Ok(output) => return Ok(output),
            Err(err) => {
                last_err = err;
                if attempt == cfg.retry_max.max(1) {
                    break;
                }
                thread::sleep(retry_backoff(cfg.retry_base, attempt));
            }
        }
    }
    Err(format!("{label} turn failed after retries: {last_err}"))
}

impl CodexTurnExecutor {
    fn new(cfg: &LoopConfig) -> Result<Self, String> {
        let session_suffix = sanitize_session_name(&cfg.dyad_name);
        let start_cmd = interactive_codex_command(&cfg.codex_start_cmd)?;
        run_command("tmux", &["-V"], Duration::from_secs(10))?;
        ensure_actor_container_running(Duration::from_secs(15), &cfg.actor_container)?;
        run_command(
            "docker",
            &["exec", cfg.actor_container.as_str(), "tmux", "-V"],
            Duration::from_secs(10),
        )?;
        Ok(Self {
            actor_container: cfg.actor_container.clone(),
            actor_session: format!("si-dyad-{session_suffix}-actor"),
            critic_session: format!("si-dyad-{session_suffix}-critic"),
            dyad_name: cfg.dyad_name.clone(),
            prompt_lines: cfg.prompt_lines.max(1),
            allow_mcp: cfg.allow_mcp_startup,
            capture_mode: if cfg.capture_mode == "alt" {
                "alt".to_string()
            } else {
                "main".to_string()
            },
            capture_lines: cfg.capture_lines.clamp(500, 50_000),
            strict_report: cfg.strict_report,
            start_cmd,
            ready_timeout: (cfg.turn_timeout / 3).max(Duration::from_secs(30)),
            turn_timeout: cfg.turn_timeout,
            poll_interval: Duration::from_millis(350),
        })
    }

    fn actor_turn(&self, timeout: Duration, prompt: &str) -> Result<String, String> {
        ensure_actor_container_running(
            timeout.min(Duration::from_secs(15)),
            &self.actor_container,
        )?;
        let runner = TmuxRunner {
            prefix: vec!["docker".to_string(), "exec".to_string(), self.actor_container.clone()],
        };
        self.run_turn(timeout, &runner, &self.actor_session, prompt, "actor")
    }

    fn critic_turn(&self, timeout: Duration, prompt: &str) -> Result<String, String> {
        let runner = TmuxRunner::default();
        self.run_turn(timeout, &runner, &self.critic_session, prompt, "critic")
    }

    fn run_turn(
        &self,
        timeout: Duration,
        runner: &TmuxRunner,
        session: &str,
        prompt: &str,
        role: &str,
    ) -> Result<String, String> {
        let start = Instant::now();
        let (pane_target, ready_output) =
            self.ensure_interactive_session(timeout, runner, session, role)?;
        let clean_ready = strip_ansi(&ready_output);
        let mut baseline_report_end =
            clean_ready.rfind(REPORT_END_MARKER).map(|idx| idx as isize).unwrap_or(-1);
        if let Ok(full) = tmux_capture(
            runner,
            &pane_target,
            &StatusOptions { capture_mode: self.capture_mode.clone(), capture_lines: 0 },
            remaining(timeout, start)?,
        ) {
            baseline_report_end =
                strip_ansi(&full).rfind(REPORT_END_MARKER).map(|idx| idx as isize).unwrap_or(-1);
        }
        tmux_send_keys(runner, &pane_target, &["C-u"], remaining(timeout, start)?)?;
        let (wire_prompt, turn_id) = wrap_turn_prompt(prompt, role);
        let normalized_prompt = normalize_interactive_prompt(&wire_prompt);
        tmux_send_literal(runner, &pane_target, &normalized_prompt, remaining(timeout, start)?)?;
        thread::sleep(Duration::from_millis(150));
        tmux_send_keys(runner, &pane_target, &["C-m"], remaining(timeout, start)?)?;
        self.wait_for_turn_completion(
            timeout,
            start,
            runner,
            &pane_target,
            baseline_report_end,
            role,
            &normalized_prompt,
            &turn_id,
        )
    }

    fn ensure_interactive_session(
        &self,
        timeout: Duration,
        runner: &TmuxRunner,
        session: &str,
        role: &str,
    ) -> Result<(String, String), String> {
        let pane_target = format!("{session}:0.0");
        let window_name = dyad_tmux_window_name(&self.dyad_name, role);
        if runner.output(timeout, &["has-session", "-t", session]).is_err() {
            runner.output(
                timeout,
                &[
                    "new-session",
                    "-d",
                    "-s",
                    session,
                    "-n",
                    &window_name,
                    "bash",
                    "-lc",
                    &self.start_cmd,
                ],
            )?;
        }
        apply_dyad_tmux_session_defaults(runner, session, timeout);
        let _ =
            runner.output(timeout, &["rename-window", "-t", &format!("{session}:0"), &window_name]);
        let _ = runner.output(timeout, &["select-pane", "-t", &pane_target, "-T", &window_name]);
        if let Ok(out) =
            runner.output(timeout, &["display-message", "-p", "-t", &pane_target, "#{pane_dead}"])
            && is_tmux_pane_dead_output(&out)
        {
            let _ = runner.output(timeout, &["kill-session", "-t", session]);
            runner.output(
                timeout,
                &[
                    "new-session",
                    "-d",
                    "-s",
                    session,
                    "-n",
                    &window_name,
                    "bash",
                    "-lc",
                    &self.start_cmd,
                ],
            )?;
            apply_dyad_tmux_session_defaults(runner, session, timeout);
            let _ = runner
                .output(timeout, &["rename-window", "-t", &format!("{session}:0"), &window_name]);
            let _ =
                runner.output(timeout, &["select-pane", "-t", &pane_target, "-T", &window_name]);
        }
        let _ =
            runner.output(timeout, &["resize-pane", "-t", &pane_target, "-x", "160", "-y", "60"]);
        let output = self.wait_for_prompt_ready(runner, &pane_target, timeout)?;
        Ok((pane_target, output))
    }

    fn wait_for_prompt_ready(
        &self,
        runner: &TmuxRunner,
        target: &str,
        timeout: Duration,
    ) -> Result<String, String> {
        let deadline = Instant::now() + self.ready_timeout.min(timeout);
        let mut last_output = String::new();
        while Instant::now() < deadline {
            let output = tmux_capture(
                runner,
                target,
                &StatusOptions {
                    capture_mode: self.capture_mode.clone(),
                    capture_lines: self.capture_lines,
                },
                timeout.min(self.poll_interval + Duration::from_secs(2)),
            )
            .unwrap_or_default();
            if !output.trim().is_empty() {
                last_output = output.clone();
            }
            let clean = strip_ansi(&output);
            if codex_auth_required(&clean) {
                let fallback = if last_output.trim().is_empty() { clean } else { last_output };
                return Err(format!(
                    "{fallback}\ncodex is prompting for sign-in; authenticate via `si login` or `si dyad peek` and complete the login flow"
                ));
            }
            if codex_prompt_ready(&clean, self.prompt_lines, self.allow_mcp) {
                return Ok(output);
            }
            thread::sleep(self.poll_interval);
        }
        if last_output.trim().is_empty() {
            return Err("timeout waiting for codex prompt".to_string());
        }
        Err("timeout waiting for codex prompt".to_string())
    }

    #[allow(clippy::too_many_arguments)]
    fn wait_for_turn_completion(
        &self,
        timeout: Duration,
        start: Instant,
        runner: &TmuxRunner,
        target: &str,
        baseline_report_end: isize,
        role: &str,
        sent_prompt: &str,
        turn_id: &str,
    ) -> Result<String, String> {
        let deadline = Instant::now() + timeout.min(self.turn_timeout);
        let mut last_output = String::new();
        let mut submit_attempts = 1usize;
        let mut last_submit = Instant::now();
        let mut sig = turn_prompt_signature(turn_id, sent_prompt);
        if sig.len() > 64 {
            sig.truncate(64);
        }

        while Instant::now() < deadline {
            let output = tmux_capture(
                runner,
                target,
                &StatusOptions {
                    capture_mode: self.capture_mode.clone(),
                    capture_lines: self.capture_lines,
                },
                remaining(timeout, start)?,
            )
            .unwrap_or_default();
            if !output.trim().is_empty() {
                last_output = output.clone();
            }
            let clean = strip_ansi(&output);
            let prompt_ready = codex_prompt_ready(&clean, self.prompt_lines, self.allow_mcp);
            let prompt_line = last_codex_prompt_line(&clean, self.prompt_lines);

            let mut report = extract_tagged_work_report(&clean, turn_id);
            let mut report_was_delimited = false;
            let placeholder_report = report.as_deref().is_some_and(|body| {
                (role == "actor"
                    && actor_report_looks_placeholder_with_mode(body, self.strict_report))
                    || (role == "critic"
                        && critic_report_looks_placeholder_with_mode(body, self.strict_report))
            });
            if placeholder_report {
                report = None;
            }

            let after = if sig.is_empty() { None } else { clean.rfind(&sig) };
            if report.is_none() {
                report = extract_delimited_work_report_after(&clean, after.map(|idx| idx as isize));
                report_was_delimited = report.is_some();
            }
            let placeholder_report = report.as_deref().is_some_and(|body| {
                (role == "actor"
                    && actor_report_looks_placeholder_with_mode(body, self.strict_report))
                    || (role == "critic"
                        && critic_report_looks_placeholder_with_mode(body, self.strict_report))
            });
            if placeholder_report {
                report = None;
            }

            if report.is_none()
                && after.is_none()
                && baseline_report_end >= 0
                && let Ok(full) = tmux_capture(
                    runner,
                    target,
                    &StatusOptions { capture_mode: self.capture_mode.clone(), capture_lines: 0 },
                    remaining(timeout, start)?,
                )
            {
                let full_clean = strip_ansi(&full);
                if let Some(body) = extract_tagged_work_report(&full_clean, turn_id)
                    && !body.trim().is_empty()
                    && ((role == "actor"
                        && !actor_report_looks_placeholder_with_mode(&body, self.strict_report))
                        || (role == "critic"
                            && !critic_report_looks_placeholder_with_mode(
                                &body,
                                self.strict_report,
                            )))
                {
                    return Ok(normalize_output_with_delimited_report(&full, &body));
                }
                if let Some(body) =
                    extract_delimited_work_report_after(&full_clean, Some(baseline_report_end))
                    && !body.trim().is_empty()
                    && ((role == "actor"
                        && !actor_report_looks_placeholder_with_mode(&body, self.strict_report))
                        || (role == "critic"
                            && !critic_report_looks_placeholder_with_mode(
                                &body,
                                self.strict_report,
                            )))
                {
                    return Ok(full);
                }
            }

            if report.is_none() {
                let segments = parse_prompt_segments_dual(&clean, &output);
                for segment in segments.iter().rev() {
                    let candidate = extract_report_lines_from_lines(&segment.raw, &segment.lines)
                        .filter(|value| !value.trim().is_empty())
                        .or_else(|| extract_sectioned_report_from_lines(&segment.lines));
                    let Some(candidate) = candidate else {
                        continue;
                    };
                    if role == "actor"
                        && actor_report_looks_placeholder_with_mode(&candidate, self.strict_report)
                    {
                        continue;
                    }
                    if role == "critic"
                        && critic_report_looks_placeholder_with_mode(&candidate, self.strict_report)
                    {
                        continue;
                    }
                    report = Some(candidate);
                    report_was_delimited = false;
                    break;
                }
            }

            if let Some(body) = report {
                if report_was_delimited {
                    return Ok(output);
                }
                return Ok(normalize_output_with_delimited_report(&output, &body));
            }

            if prompt_ready {
                if let Some(line) = prompt_line.as_ref()
                    && codex_prompt_line_looks_like_echo(line, sent_prompt)
                {
                    thread::sleep(self.poll_interval);
                    continue;
                }
                if self.strict_report {
                    if last_submit.elapsed() <= Duration::from_secs(2) {
                        thread::sleep(self.poll_interval);
                        continue;
                    }
                    if submit_attempts < 2 && last_submit.elapsed() > Duration::from_secs(4) {
                        let _ =
                            tmux_send_keys(runner, target, &["C-m"], remaining(timeout, start)?);
                        submit_attempts += 1;
                        last_submit = Instant::now();
                        thread::sleep(self.poll_interval);
                        continue;
                    }
                    if last_output.trim().is_empty() {
                        last_output = output;
                    }
                    if last_output.trim().is_empty() {
                        return Err(
                            "codex prompt ready but missing work report markers".to_string()
                        );
                    }
                    return Err("codex prompt ready but missing work report markers".to_string());
                }
                if submit_attempts < 2 && last_submit.elapsed() > Duration::from_secs(4) {
                    let _ = tmux_send_keys(runner, target, &["C-m"], remaining(timeout, start)?);
                    submit_attempts += 1;
                    last_submit = Instant::now();
                }
            }
            thread::sleep(self.poll_interval);
        }
        if last_output.trim().is_empty() {
            return Err("timeout waiting for codex report".to_string());
        }
        Err("timeout waiting for codex report".to_string())
    }
}

impl TmuxRunner {
    fn output(&self, timeout: Duration, args: &[&str]) -> Result<String, String> {
        if args.is_empty() {
            return Err("tmux args required".to_string());
        }
        if self.prefix.is_empty() {
            return run_command("tmux", args, timeout);
        }
        let mut command_args = self.prefix[1..].to_vec();
        command_args.push("tmux".to_string());
        command_args.extend(args.iter().map(|arg| arg.to_string()));
        let refs = command_args.iter().map(String::as_str).collect::<Vec<_>>();
        run_command(&self.prefix[0], &refs, timeout)
    }
}

fn dyad_tmux_window_name(dyad: &str, role: &str) -> String {
    let dyad = match dyad.trim() {
        "" => "unknown",
        value => value,
    };
    match role.trim().to_lowercase().as_str() {
        "actor" => format!("{} actor", dyad),
        "critic" => format!("{} critic", dyad),
        _ => dyad.to_string(),
    }
}

fn tmux_send_keys(
    runner: &TmuxRunner,
    target: &str,
    keys: &[&str],
    timeout: Duration,
) -> Result<(), String> {
    if keys.is_empty() {
        return Ok(());
    }
    let mut args = vec!["send-keys", "-t", target];
    args.extend(keys.iter().copied());
    runner.output(timeout, &args)?;
    Ok(())
}

fn tmux_send_literal(
    runner: &TmuxRunner,
    target: &str,
    text: &str,
    timeout: Duration,
) -> Result<(), String> {
    if text.trim().is_empty() {
        return Ok(());
    }
    runner.output(timeout, &["send-keys", "-t", target, "-l", text])?;
    Ok(())
}

fn tmux_capture(
    runner: &TmuxRunner,
    target: &str,
    opts: &StatusOptions,
    timeout: Duration,
) -> Result<String, String> {
    let start =
        if opts.capture_lines > 0 { format!("-{}", opts.capture_lines) } else { "-".to_string() };
    match opts.capture_mode.as_str() {
        "alt" => runner
            .output(timeout, &["capture-pane", "-t", target, "-p", "-J", "-S", &start, "-a", "-q"]),
        "main" => runner.output(timeout, &["capture-pane", "-t", target, "-p", "-J", "-S", &start]),
        other => Err(format!("unsupported tmux capture mode: {other}")),
    }
}

fn run_command(program: &str, args: &[&str], timeout: Duration) -> Result<String, String> {
    let timeout_arg = format!("{}s", timeout_secs(timeout.max(Duration::from_secs(1))));
    let output = Command::new("timeout")
        .arg("--signal=TERM")
        .arg(&timeout_arg)
        .arg(program)
        .args(args)
        .output()
        .map_err(|err| format!("spawn {program}: {err}"))?;
    let stdout = String::from_utf8_lossy(&output.stdout).trim().to_string();
    let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
    if output.status.success() {
        if !stderr.is_empty() {
            return Ok(join_output(&stdout, &stderr));
        }
        return Ok(stdout);
    }
    if output.status.code() == Some(124) {
        return Err("context deadline exceeded".to_string());
    }
    let message = if !stderr.is_empty() {
        join_output(&stderr, &stdout)
    } else if !stdout.is_empty() {
        stdout
    } else {
        format!("exit status {}", output.status)
    };
    Err(message)
}

fn ensure_actor_container_running(timeout: Duration, container: &str) -> Result<(), String> {
    let container = container.trim();
    if container.is_empty() {
        return Err("actor container required".to_string());
    }
    let inspect_args = ["inspect", "-f", "{{.State.Running}}", container];
    if run_command("docker", &inspect_args, timeout)
        .map(|value| is_true_string(&value))
        .unwrap_or(false)
    {
        return Ok(());
    }
    run_command("docker", &["start", container], timeout)?;
    let deadline = Instant::now() + Duration::from_secs(8);
    while Instant::now() < deadline {
        if run_command("docker", &inspect_args, Duration::from_secs(3))
            .map(|value| is_true_string(&value))
            .unwrap_or(false)
        {
            return Ok(());
        }
        thread::sleep(Duration::from_millis(250));
    }
    Err(format!("actor container {container} did not reach running state after restart"))
}

fn apply_dyad_tmux_session_defaults(runner: &TmuxRunner, session: &str, timeout: Duration) {
    if session.trim().is_empty() {
        return;
    }
    let _ = runner.output(timeout, &["set-option", "-t", session, "remain-on-exit", "off"]);
    let _ = runner.output(timeout, &["set-option", "-t", session, "mouse", "on"]);
    let _ = runner
        .output(timeout, &["set-option", "-t", session, "history-limit", DYAD_TMUX_HISTORY_LIMIT]);
}

fn extract_delimited_work_report(output: &str) -> Option<String> {
    let clean = normalize_newlines(output).trim().to_string();
    let start = clean.rfind(REPORT_BEGIN_MARKER)?;
    let end = clean.rfind(REPORT_END_MARKER)?;
    if end <= start {
        return None;
    }
    let body = clean[start + REPORT_BEGIN_MARKER.len()..end].trim();
    (!body.is_empty()).then(|| body.to_string())
}

fn extract_tagged_work_report(output: &str, turn_id: &str) -> Option<String> {
    let turn_id = turn_id.trim();
    if turn_id.is_empty() {
        return None;
    }
    let clean = normalize_newlines(output).trim().to_string();
    let begin = tagged_report_marker(REPORT_BEGIN_MARKER, turn_id);
    let end = tagged_report_marker(REPORT_END_MARKER, turn_id);
    let start = clean.rfind(&begin)?;
    let finish = clean.rfind(&end)?;
    if finish <= start {
        return None;
    }
    let body = clean[start + begin.len()..finish].trim();
    (!body.is_empty()).then(|| body.to_string())
}

fn extract_delimited_work_report_after(output: &str, after_end: Option<isize>) -> Option<String> {
    let clean = normalize_newlines(output).trim().to_string();
    let end = clean.rfind(REPORT_END_MARKER)? as isize;
    if let Some(after) = after_end
        && end <= after
    {
        return None;
    }
    let start = clean[..end as usize].rfind(REPORT_BEGIN_MARKER)? as isize;
    if let Some(after) = after_end
        && start <= after
    {
        return None;
    }
    let body = clean[start as usize + REPORT_BEGIN_MARKER.len()..end as usize].trim();
    (!body.is_empty()).then(|| body.to_string())
}

fn normalize_output_with_delimited_report(output: &str, report: &str) -> String {
    let mut normalized = output.trim().to_string();
    if !normalized.is_empty() {
        normalized.push('\n');
    }
    normalized.push_str(REPORT_BEGIN_MARKER);
    normalized.push('\n');
    normalized.push_str(report.trim());
    normalized.push('\n');
    normalized.push_str(REPORT_END_MARKER);
    normalized
}

fn strip_ansi(input: &str) -> String {
    let csi = Regex::new(r"\x1b\[[0-?]*[ -/]*[@-~]").expect("valid ansi regex");
    let osc = Regex::new(r"\x1b\][^\x07]*(\x07|\x1b\\)").expect("valid osc regex");
    osc.replace_all(&csi.replace_all(input, ""), "").to_string()
}

fn codex_prompt_ready(output: &str, prompt_lines: usize, allow_mcp_startup: bool) -> bool {
    let lower = output.to_lowercase();
    if !allow_mcp_startup && (lower.contains("starting mcp") || lower.contains("mcp startup")) {
        return false;
    }
    let max_lines = prompt_lines.max(1) * 4;
    for line in output.lines().rev().filter(|line| !line.trim().is_empty()).take(max_lines) {
        if line.trim_start().starts_with('›') {
            return true;
        }
    }
    false
}

fn codex_auth_required(output: &str) -> bool {
    let lower = output.to_lowercase();
    lower.contains("welcome to codex")
        && (lower.contains("sign in with chatgpt")
            || lower.contains("device code")
            || lower.contains("provide your own api key")
            || lower.contains("connect an api key"))
}

fn last_codex_prompt_line(output: &str, prompt_lines: usize) -> Option<String> {
    let max_lines = prompt_lines.max(1) * 6;
    output
        .lines()
        .rev()
        .filter(|line| !line.trim().is_empty())
        .take(max_lines)
        .find(|line| line.trim_start().starts_with('›'))
        .map(|line| line.trim().to_string())
}

fn codex_prompt_line_looks_like_echo(prompt_line: &str, sent_prompt: &str) -> bool {
    let trimmed = prompt_line.trim().trim_start_matches('›').trim().to_string();
    if trimmed.is_empty() {
        return false;
    }
    if trimmed.to_lowercase().contains("[pasted content") {
        return true;
    }
    let sent_prompt = sent_prompt.trim();
    if sent_prompt.is_empty() {
        return false;
    }
    let prefix = if sent_prompt.len() > 32 { &sent_prompt[..32] } else { sent_prompt };
    trimmed.contains(prefix)
}

fn parse_prompt_segments_dual(clean: &str, raw: &str) -> Vec<PromptSegment> {
    let clean_lines = clean.lines().map(str::to_string).collect::<Vec<_>>();
    let raw_lines = raw.lines().map(str::to_string).collect::<Vec<_>>();
    let max = clean_lines.len().max(raw_lines.len());
    let mut padded_clean = clean_lines;
    let mut padded_raw = raw_lines;
    padded_clean.resize(max, String::new());
    padded_raw.resize(max, String::new());

    let mut segments = Vec::new();
    let mut current: Option<PromptSegment> = None;
    for index in 0..max {
        let line = &padded_clean[index];
        let raw_line = &padded_raw[index];
        if line.trim_start().starts_with('›') {
            if let Some(segment) = current.take() {
                segments.push(segment);
            }
            current = Some(PromptSegment { lines: Vec::new(), raw: Vec::new() });
            continue;
        }
        if let Some(segment) = current.as_mut() {
            segment.lines.push(line.clone());
            segment.raw.push(raw_line.clone());
        }
    }
    if let Some(segment) = current {
        segments.push(segment);
    }
    segments
}

fn extract_report_lines_from_lines(raw_lines: &[String], clean_lines: &[String]) -> Option<String> {
    let max = raw_lines.len().min(clean_lines.len());
    let mut blocks: Vec<Vec<String>> = Vec::new();
    let mut current: Vec<String> = Vec::new();
    let mut in_report = false;
    let mut worked_line = String::new();

    for index in 0..max {
        let raw = raw_lines[index].trim_end_matches([' ', '\t']).to_string();
        let clean = clean_lines[index].trim_end_matches([' ', '\t']).to_string();
        let clean_core = clean.trim_start().to_string();
        if clean_core.to_lowercase().contains("worked for") {
            worked_line = clean.clone();
        }
        if clean_core.starts_with("• ") {
            in_report = true;
            current.push(raw);
            continue;
        }
        if !in_report {
            continue;
        }
        if clean.trim().is_empty() {
            if !current.is_empty() {
                blocks.push(current.clone());
                current.clear();
            }
            in_report = false;
            continue;
        }
        if clean.starts_with("  ") {
            current.push(raw);
            continue;
        }
        let core = clean.trim().to_string();
        if core.starts_with('⚠')
            || core.starts_with("Tip:")
            || core.starts_with('›')
            || core.starts_with("• Starting MCP")
            || core.starts_with("• Starting")
        {
            if !current.is_empty() {
                blocks.push(current.clone());
                current.clear();
            }
            break;
        }
        current.push(raw);
    }
    if !current.is_empty() {
        blocks.push(current);
    }
    for block in blocks.into_iter().rev() {
        if is_transient_report(&block) {
            continue;
        }
        let mut out = block;
        while out.last().map(|line| line.trim().is_empty()).unwrap_or(false) {
            out.pop();
        }
        if !worked_line.is_empty() && !out.iter().any(|line| line == &worked_line) {
            out.push(worked_line.clone());
        }
        if !out.is_empty() {
            return Some(out.join("\n"));
        }
    }
    None
}

fn extract_sectioned_report_from_lines(lines: &[String]) -> Option<String> {
    let start = lines.iter().position(|line| is_report_header(line))?;
    let mut out = Vec::new();
    for line in &lines[start..] {
        let core = line.trim();
        if !core.is_empty() {
            let lower = core.to_lowercase();
            if core.starts_with("Tip:") || core.starts_with('⚠') {
                break;
            }
            if lower.starts_with("model:") || lower.starts_with("directory:") {
                break;
            }
        }
        out.push(line.clone());
    }
    while out.last().map(|line| line.trim().is_empty()).unwrap_or(false) {
        out.pop();
    }
    let joined = out.join("\n").trim().to_string();
    (!joined.is_empty()).then_some(joined)
}

fn is_report_header(line: &str) -> bool {
    let lower = line.trim().to_lowercase();
    matches!(
        lower.as_str(),
        s if s.starts_with("summary:")
            || s.starts_with("changes:")
            || s.starts_with("validation:")
            || s.starts_with("open questions:")
            || s.starts_with("next step for critic:")
            || s.starts_with("assessment:")
            || s.starts_with("risks:")
            || s.starts_with("required fixes:")
            || s.starts_with("verification steps:")
            || s.starts_with("next actor prompt:")
            || s.starts_with("continue loop:")
    )
}

fn is_transient_report(lines: &[String]) -> bool {
    let head = match lines.first() {
        Some(line) => line.trim(),
        None => return true,
    };
    head.starts_with("• Working")
        || head.contains("esc to interrupt")
        || head.starts_with("• Starting MCP")
}

fn build_seed_critic_prompt(cfg: &LoopConfig) -> String {
    let seed_block = if cfg.seed_critic_prompt.trim().is_empty() {
        String::new()
    } else {
        format!("\nUser seed:\n{}\n", cfg.seed_critic_prompt.trim())
    };
    format!(
        r#"You are the CRITIC for the dyad "{}".

Goal: {}

Hard rules:
- You initiate: write the first message to the actor now.
- After this seed, your input will be ONLY the actor's work report (no templates).
- Your output will be sent verbatim to the actor.
- Keep it short; refer to /workspace/DYAD_PROTOCOL.md for the exact actor report format.

Task board:
- Use /root/.si/TASK_BOARD.md as the queue.
- Tell the actor to pick ONE task, move it to Doing, and update TASK_BOARD.md each turn.

Vault:
- The actor may use `si vault` as needed; never print secret values.

Output requirement:
- Output ONLY the next actor message, delimited as:
  <<WORK_REPORT_BEGIN>>
  <message to actor>
  <<WORK_REPORT_END>>{}
Now: write the first message to the actor to start useful work from TASK_BOARD.md."#,
        cfg.dyad_name, cfg.goal, seed_block
    )
}

fn write_turn_artifacts(
    state_dir: &Path,
    turn: usize,
    member: &str,
    prompt: &str,
    raw: &str,
    report: &str,
) -> Result<(), String> {
    if member.trim().is_empty() {
        return Err("member required".to_string());
    }
    let reports_dir = state_dir.join("reports");
    fs::create_dir_all(&reports_dir)
        .map_err(|err| format!("mkdir {}: {err}", reports_dir.display()))?;
    maybe_apply_host_ownership(&reports_dir);
    let base = reports_dir.join(format!("turn-{turn:04}-{member}"));
    write_text_file(&base.with_extension("prompt.txt"), &format!("{}\n", prompt))?;
    write_text_file(&base.with_extension("raw.txt"), &format!("{}\n", raw.trim()))?;
    write_text_file(&base.with_extension("report.md"), &format!("{}\n", report.trim()))?;
    Ok(())
}

fn append_task_board_turn_log(task_board_path: &str, dyad: &str, turn: usize, actor_report: &str) {
    let task_board = PathBuf::from(task_board_path.trim());
    if task_board_path.trim().is_empty() || !task_board.exists() {
        return;
    }
    let dyad = if dyad.trim().is_empty() { "unknown" } else { dyad.trim() };
    let mut summary = extract_actor_report_summary(actor_report);
    if summary.len() > 140 {
        summary.truncate(140);
        summary.push_str("...");
    }
    let line = format!("- {} {} actor Turn {}: {}\n", Utc::now().to_rfc3339(), dyad, turn, summary);
    if let Ok(mut existing) = fs::read_to_string(&task_board) {
        existing.push_str(&line);
        let _ = write_text_file(&task_board, &existing);
    }
}

fn extract_actor_report_summary(actor_report: &str) -> String {
    let lines = normalize_newlines(actor_report);
    let mut in_summary = false;
    for line in lines.lines() {
        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }
        let lower = trimmed.to_lowercase();
        if lower.starts_with("summary:") {
            in_summary = true;
            continue;
        }
        if in_summary {
            if trimmed.ends_with(':') && !trimmed.starts_with('-') {
                break;
            }
            if let Some(stripped) = trimmed.strip_prefix("-") {
                return stripped.trim().to_string();
            }
            return trimmed.to_string();
        }
    }
    for line in lines.lines() {
        let trimmed = line.trim();
        if trimmed.is_empty() || (trimmed.ends_with(':') && !trimmed.starts_with('-')) {
            continue;
        }
        return trimmed.trim_start_matches('-').trim().to_string();
    }
    String::new()
}

fn load_loop_state(path: &Path) -> Result<LoopState, String> {
    if !path.exists() {
        return Ok(LoopState::default());
    }
    let data = fs::read_to_string(path).map_err(|err| format!("read {}: {err}", path.display()))?;
    serde_json::from_str(&data).map_err(|err| format!("parse {}: {err}", path.display()))
}

fn save_loop_state(path: &Path, state: &LoopState) -> Result<(), String> {
    let Some(parent) = path.parent() else {
        return Err(format!("invalid state path: {}", path.display()));
    };
    fs::create_dir_all(parent).map_err(|err| format!("mkdir {}: {err}", parent.display()))?;
    maybe_apply_host_ownership(parent);
    let payload =
        serde_json::to_string_pretty(state).map_err(|err| format!("serialize state: {err}"))?;
    write_text_file(path, &format!("{payload}\n"))
}

fn default_dyad_state_dir(dyad: &str) -> PathBuf {
    let name = if dyad.trim().is_empty() { "unknown" } else { dyad.trim() };
    PathBuf::from(env_or("HOME", "/root")).join(".si").join("dyad").join(name)
}

fn read_loop_control(state_dir: &Path) -> (bool, bool) {
    (state_dir.join("control.stop").exists(), state_dir.join("control.pause").exists())
}

fn interactive_codex_command(custom: &str) -> Result<String, String> {
    let custom = custom.trim();
    if !custom.is_empty() {
        let lower = custom.to_lowercase();
        if lower.contains("codex exec") || lower.contains("codex-exec") {
            return Err(
                "DYAD_CODEX_START_CMD must not use `codex exec`; dyads require interactive Codex"
                    .to_string(),
            );
        }
        return Ok(custom.to_string());
    }
    Ok("export TERM=xterm-256color COLORTERM=truecolor COLUMNS=160 LINES=60 HOME=/root CODEX_HOME=/root/.codex; cd /workspace 2>/dev/null || true; codex --dangerously-bypass-approvals-and-sandbox".to_string())
}

fn normalize_interactive_prompt(prompt: &str) -> String {
    normalize_newlines(prompt).split_whitespace().collect::<Vec<_>>().join(" ")
}

fn build_turn_id(role: &str) -> String {
    let role = match role.trim().to_lowercase().as_str() {
        "actor" => "actor",
        "critic" => "critic",
        _ => "agent",
    };
    format!("{TURN_ID_PREFIX}{role}-{:x}", Utc::now().timestamp_nanos_opt().unwrap_or_default())
}

fn tagged_report_marker(base: &str, turn_id: &str) -> String {
    let root = base.trim().trim_end_matches(">>");
    format!("{root}:{}>>", turn_id.trim())
}

fn wrap_turn_prompt(prompt: &str, role: &str) -> (String, String) {
    let base = prompt.trim();
    if base.is_empty() {
        return (String::new(), String::new());
    }
    let turn_id = build_turn_id(role);
    let begin = tagged_report_marker(REPORT_BEGIN_MARKER, &turn_id);
    let end = tagged_report_marker(REPORT_END_MARKER, &turn_id);
    (
        format!(
            "[{turn_id}] {base} Reply using {begin} ... {end} (legacy {REPORT_BEGIN_MARKER}/{REPORT_END_MARKER} also acceptable)."
        ),
        turn_id,
    )
}

fn turn_prompt_signature(turn_id: &str, sent_prompt: &str) -> String {
    if !turn_id.trim().is_empty() {
        return format!("[{}]", turn_id.trim());
    }
    sent_prompt.trim().to_string()
}

fn sanitize_session_name(raw: &str) -> String {
    let filtered = raw
        .trim()
        .to_lowercase()
        .chars()
        .filter(|ch| ch.is_ascii_lowercase() || ch.is_ascii_digit() || *ch == '-' || *ch == '_')
        .collect::<String>();
    if filtered.is_empty() { "unknown".to_string() } else { filtered }
}

fn critic_requests_stop(report: &str) -> bool {
    normalize_newlines(report).lines().map(|line| line.trim().to_lowercase()).any(|line| {
        line.contains("continue loop: no")
            || line.contains("continue_loop=no")
            || line.contains("loop_continue=false")
            || line.contains("stop loop: yes")
            || line.contains("stop_loop=true")
            || line.contains("#stop_loop")
    })
}

fn critic_message_looks_placeholder(message: &str) -> bool {
    if contains_codex_chrome(message) {
        return true;
    }
    let norm = normalize_report_text(message);
    if norm.is_empty()
        || norm.contains("summary: - ...")
        || norm.contains("assessment: - ...")
        || norm.matches("...").count() >= 3
    {
        return true;
    }
    let words = norm.split_whitespace().collect::<Vec<_>>();
    words.is_empty() || (words.len() == 1 && matches!(words[0], "ok" | "okay"))
}

fn actor_report_looks_placeholder_with_mode(report: &str, strict: bool) -> bool {
    if contains_codex_chrome(report) {
        return true;
    }
    let norm = normalize_report_text(report);
    if !strict {
        if norm.is_empty() {
            return true;
        }
        if norm.contains("summary: - ...") && norm.contains("changes: - ...") {
            return true;
        }
        if norm.matches("...").count() >= 3 {
            return true;
        }
        if report.contains("• ")
            || report.contains("\n• ")
            || report.contains("\n- ")
            || report.contains("\n└")
            || norm.contains("explored")
            || norm.contains("changed")
            || norm.contains("validation")
            || norm.contains("next")
        {
            return false;
        }
        return norm.split_whitespace().count() < 8;
    }
    if !(norm.contains("summary:")
        && norm.contains("changes:")
        && norm.contains("validation:")
        && norm.contains("open questions:")
        && norm.contains("next step for critic:"))
    {
        return true;
    }
    norm.contains("summary: changes: validation: open questions: next step for critic:")
        || (norm.contains("summary: - ...")
            && norm.contains("changes: - ...")
            && norm.contains("validation: - ..."))
        || norm.contains("<at least")
        || norm.contains("<specific")
        || norm.contains("<what you")
        || (norm.matches("...").count() >= 2 && report_bullet_count(report) <= 2)
        || (norm.contains("summary:")
            && norm.contains("changes:")
            && report_bullet_count(report) < 2)
}

fn critic_report_looks_placeholder_with_mode(report: &str, strict: bool) -> bool {
    if contains_codex_chrome(report) {
        return true;
    }
    let norm = normalize_report_text(report);
    if !strict {
        if norm.is_empty() {
            return true;
        }
        if norm.contains("assessment: - ...") && norm.contains("required fixes: - ...") {
            return true;
        }
        if norm.matches("...").count() >= 3 {
            return true;
        }
        if report.contains("• ") || report.contains("\n• ") || report.contains("\n- ") {
            return false;
        }
        return norm.split_whitespace().count() < 10;
    }
    if !(norm.contains("assessment:")
        && norm.contains("risks:")
        && norm.contains("required fixes:")
        && norm.contains("verification steps:")
        && norm.contains("next actor prompt:")
        && norm.contains("continue loop:"))
    {
        return true;
    }
    norm.contains("continue loop: yes|no")
        || norm.contains("continue loop: <yes|no>")
        || norm.contains("continue loop: - yes|no")
        || !(norm.contains("continue loop: yes")
            || norm.contains("continue loop: no")
            || norm.contains("continue_loop=yes")
            || norm.contains("continue_loop=no")
            || norm.contains("loop_continue=true")
            || norm.contains("loop_continue=false"))
        || norm.contains(
            "assessment: risks: required fixes: verification steps: next actor prompt: continue loop: yes|no",
        )
        || (norm.contains("assessment: - ...")
            && norm.contains("required fixes: - ...")
            && norm.contains("verification steps: - ...")
            && norm.contains("next actor prompt: - ..."))
        || norm.contains("<clear actionable")
        || norm.contains("<single concrete")
        || (norm.matches("...").count() >= 2 && report_bullet_count(report) <= 3)
        || (norm.contains("assessment:") && norm.contains("required fixes:") && report_bullet_count(report) < 3)
}

fn normalize_report_text(value: &str) -> String {
    normalize_newlines(value).to_lowercase().split_whitespace().collect::<Vec<_>>().join(" ")
}

fn contains_codex_chrome(value: &str) -> bool {
    let lower = value.to_lowercase();
    lower.contains("openai codex")
        || lower.contains("context left")
        || lower.contains("model to change")
        || lower.contains("directory:")
        || lower.contains("tip:")
        || lower.contains("? for shortcuts")
        || value.contains('╭')
        || value.contains('╰')
        || value.contains('╯')
        || value.contains('│')
}

fn report_bullet_count(report: &str) -> usize {
    normalize_newlines(report)
        .lines()
        .filter(|line| {
            let trimmed = line.trim();
            trimmed.starts_with("- ") || trimmed.starts_with("* ") || trimmed.starts_with("• ")
        })
        .count()
}

fn maybe_apply_host_ownership(path: &Path) {
    if is_path_on_mount(path) {
        return;
    }
    let Some((uid, gid)) = host_ownership() else {
        return;
    };
    let Ok(c_path) = CString::new(path.as_os_str().as_bytes()) else {
        return;
    };
    unsafe { libc::chown(c_path.as_ptr(), uid as libc::uid_t, gid as libc::gid_t) };
}

fn is_path_on_mount(path: &Path) -> bool {
    let abs = if path.is_absolute() {
        path.to_path_buf()
    } else {
        fs::canonicalize(path).unwrap_or_else(|_| path.to_path_buf())
    };
    mount_points().into_iter().any(|mp| {
        !mp.as_os_str().is_empty() && mp != Path::new("/") && (abs == mp || abs.starts_with(&mp))
    })
}

fn mount_points() -> Vec<PathBuf> {
    let Ok(file) = fs::File::open("/proc/self/mountinfo") else {
        return Vec::new();
    };
    let reader = BufReader::new(file);
    reader
        .lines()
        .map_while(Result::ok)
        .filter_map(|line| {
            let left = line.split(" - ").next().unwrap_or_default().to_string();
            let fields = left.split_whitespace().collect::<Vec<_>>();
            (fields.len() >= 5).then(|| PathBuf::from(decode_mount_info_path(fields[4])))
        })
        .collect()
}

fn decode_mount_info_path(raw: &str) -> String {
    let mut bytes = Vec::with_capacity(raw.len());
    let raw_bytes = raw.as_bytes();
    let mut index = 0usize;
    while index < raw_bytes.len() {
        if raw_bytes[index] == b'\\'
            && index + 3 < raw_bytes.len()
            && let Ok(value) = u8::from_str_radix(&raw[index + 1..index + 4], 8)
        {
            bytes.push(value);
            index += 4;
            continue;
        }
        bytes.push(raw_bytes[index]);
        index += 1;
    }
    String::from_utf8_lossy(&bytes).to_string()
}

fn host_ownership() -> Option<(i32, i32)> {
    let uid = env_int("SI_HOST_UID", 0);
    let gid = env_int("SI_HOST_GID", 0);
    (uid > 0 && gid > 0).then_some((uid, gid))
}

fn write_text_file(path: &Path, content: &str) -> Result<(), String> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).map_err(|err| format!("mkdir {}: {err}", parent.display()))?;
    }
    fs::write(path, content).map_err(|err| format!("write {}: {err}", path.display()))?;
    let _ = fs::set_permissions(path, fs::Permissions::from_mode(0o644));
    maybe_apply_host_ownership(path);
    Ok(())
}

fn remaining(timeout: Duration, start: Instant) -> Result<Duration, String> {
    timeout
        .checked_sub(start.elapsed())
        .filter(|value| !value.is_zero())
        .ok_or_else(|| "context deadline exceeded".to_string())
}

fn retry_backoff(base: Duration, attempt: usize) -> Duration {
    let base = if base.is_zero() { Duration::from_secs(1) } else { base };
    let multiplier = 2u32.saturating_pow(attempt.saturating_sub(1) as u32);
    (base * multiplier).min(Duration::from_secs(30))
}

fn is_true_string(value: &str) -> bool {
    matches!(value.trim().to_lowercase().as_str(), "true" | "1" | "yes")
}

fn is_tmux_pane_dead_output(out: &str) -> bool {
    is_true_string(out)
}

fn timeout_secs(value: Duration) -> String {
    format!("{:.3}", value.as_secs_f64())
}

fn normalize_newlines(value: &str) -> String {
    value.replace("\r\n", "\n").replace('\r', "\n")
}

fn env_bool(key: &str, default: bool) -> bool {
    match env_or_empty(key).to_lowercase().as_str() {
        "" => default,
        "1" | "true" | "yes" | "on" => true,
        "0" | "false" | "no" | "off" => false,
        _ => default,
    }
}

fn env_int(key: &str, default: i32) -> i32 {
    env_or_empty(key).parse::<i32>().unwrap_or(default)
}

fn env_duration_seconds(key: &str, default_seconds: i32) -> Duration {
    let value = env_int(key, default_seconds).max(0);
    Duration::from_secs(value as u64)
}

fn env_is_true(key: &str) -> bool {
    env_bool(key, false)
}

fn env_or(key: &str, default: &str) -> String {
    let value = env_or_empty(key);
    if value.is_empty() { default.to_string() } else { value }
}

fn env_or_empty(key: &str) -> String {
    env::var(key).unwrap_or_default().trim().to_string()
}

fn join_output(primary: &str, secondary: &str) -> String {
    if primary.is_empty() {
        return secondary.to_string();
    }
    if secondary.is_empty() {
        return primary.to_string();
    }
    format!("{primary}\n{secondary}")
}

fn log_line(message: &str) {
    eprintln!("critic: {message}");
}
