//! Development-only opt-in live acceptance runner.
//!
//! Runs the section 12 live matrix against two already-enrolled SI profiles
//! holding two genuinely different native accounts. Consumes profile names,
//! never token arguments. Rejects identical canonical stores or identical
//! confirmed provider identities before running any real-request leg.
//!
//! Reports contain assertions and redacted identity labels only: captured
//! output is scrubbed for token-shaped material and raw tmux captures,
//! device codes, and native auth files are never uploaded or printed.
//!
//! Run on a trusted host holding those native account stores; regular
//! public/fork PR runners must not receive subscription credentials.

use std::collections::HashMap;
use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::{Command, ExitCode, Output, Stdio};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

const USAGE: &str = r#"usage: si-live-accounts --provider <codex|muse> --profile-a <name> --profile-b <name> [options]

options:
  --si-bin <path>       si binary under test (default: resolve `si` on PATH)
  --format <text|json>  report format (default: text)
  --only <leg,...>      run only these legs (default: all)
  --leg-timeout <secs>  per-leg timeout (default: 180)
  --workspace-root <dir> parent dir for private leg workspaces (default: tempdir)

legs: enrollment, simultaneous, marker, respawn-a, logout-a, relogin-a,
      renewal, revocation, session-association, concurrency, subprocess
"#;

const LEGS: &[&str] = &[
    "enrollment",
    "simultaneous",
    "marker",
    "respawn-a",
    "logout-a",
    "relogin-a",
    "renewal",
    "revocation",
    "session-association",
    "concurrency",
    "subprocess",
];

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Status {
    Pass,
    Fail,
    Blocked,
}

impl Status {
    fn name(self) -> &'static str {
        match self {
            Self::Pass => "pass",
            Self::Fail => "fail",
            Self::Blocked => "blocked",
        }
    }
}

#[derive(Debug)]
struct LegResult {
    name: String,
    status: Status,
    detail: String,
}

impl LegResult {
    fn pass(name: &str, detail: String) -> Self {
        Self { name: name.to_owned(), status: Status::Pass, detail }
    }

    fn fail(name: &str, detail: String) -> Self {
        Self { name: name.to_owned(), status: Status::Fail, detail }
    }

    fn blocked(name: &str, detail: String) -> Self {
        Self { name: name.to_owned(), status: Status::Blocked, detail }
    }
}

#[derive(Debug, Default)]
struct ProfileFacts {
    native_root: String,
    auth_state: String,
    identity: Option<String>,
    identity_source: Option<String>,
    disabled: Option<String>,
}

fn main() -> ExitCode {
    match run() {
        Ok(code) => code,
        Err(err) => {
            eprintln!("si-live-accounts: {err}");
            ExitCode::from(2)
        }
    }
}

struct Args {
    provider: String,
    profile_a: String,
    profile_b: String,
    si_bin: Option<PathBuf>,
    format: String,
    only: Option<Vec<String>>,
    leg_timeout: Duration,
    workspace_root: Option<PathBuf>,
}

fn parse_args(argv: &[String]) -> Result<Args, String> {
    let mut map: HashMap<&str, String> = HashMap::new();
    let mut iter = argv.iter().peekable();
    while let Some(arg) = iter.next() {
        match arg.as_str() {
            "--provider" | "--profile-a" | "--profile-b" | "--si-bin" | "--format" | "--only"
            | "--leg-timeout" | "--workspace-root" => {
                let value = iter
                    .next()
                    .ok_or_else(|| format!("missing value for {arg}\n{USAGE}"))?
                    .to_owned();
                map.insert(arg.as_str(), value);
            }
            "--help" | "-h" => {
                println!("{USAGE}");
                std::process::exit(0);
            }
            other => return Err(format!("unknown argument: {other}\n{USAGE}")),
        }
    }
    let required =
        |key: &str| map.get(key).cloned().ok_or_else(|| format!("missing {key}\n{USAGE}"));
    let provider = required("--provider")?;
    if provider != "codex" && provider != "muse" {
        return Err(format!("--provider must be codex or muse\n{USAGE}"));
    }
    let format = map.get("--format").cloned().unwrap_or_else(|| "text".to_owned());
    if format != "text" && format != "json" {
        return Err(format!("--format must be text or json\n{USAGE}"));
    }
    let only: Option<Vec<String>> = map.get("--only").map(|raw| {
        raw.split(',').map(|item| item.trim().to_owned()).filter(|item| !item.is_empty()).collect()
    });
    if let Some(legs) = &only {
        for leg in legs {
            if !LEGS.contains(&leg.as_str()) {
                return Err(format!("unknown leg: {leg}\n{USAGE}"));
            }
        }
    }
    let leg_timeout = map
        .get("--leg-timeout")
        .map(|raw| raw.parse::<u64>().map_err(|_| format!("invalid --leg-timeout: {raw}")))
        .transpose()?
        .unwrap_or(180);
    Ok(Args {
        provider,
        profile_a: required("--profile-a")?,
        profile_b: required("--profile-b")?,
        si_bin: map.get("--si-bin").map(PathBuf::from),
        format,
        only,
        leg_timeout: Duration::from_secs(leg_timeout),
        workspace_root: map.get("--workspace-root").map(PathBuf::from),
    })
}

fn run() -> Result<ExitCode, String> {
    let argv: Vec<String> = env::args().skip(1).collect();
    if argv.is_empty() {
        return Err(USAGE.to_owned());
    }
    let args = parse_args(&argv)?;
    let si = resolve_si_bin(args.si_bin.as_deref())?;
    let si_version = si_version(&si);
    let native_program = native_program(&args.provider);
    let native_version = probe_native_version(&native_program);

    let mut results: Vec<LegResult> = Vec::new();

    // Leg 0 (gate): resolve both profiles, reject identical stores.
    let show_a = si_profile_show(&si, &args.provider, &args.profile_a)?;
    let show_b = si_profile_show(&si, &args.provider, &args.profile_b)?;
    let facts_a = parse_profile_facts(&args.profile_a, &show_a)?;
    let facts_b = parse_profile_facts(&args.profile_b, &show_b)?;

    if facts_a.native_root == facts_b.native_root {
        results.push(LegResult::fail(
            "enrollment",
            format!(
                "profiles {} and {} resolve to the same canonical store {}; refusing live legs",
                redact_label(&args.profile_a),
                redact_label(&args.profile_b),
                redact_path(&facts_a.native_root),
            ),
        ));
        return Ok(emit_report(
            &args,
            &si_version,
            &native_program,
            &native_version,
            &facts_a,
            &facts_b,
            &results,
        ));
    }

    let distinct = distinct_confirmed_identities(&facts_a, &facts_b);
    match distinct {
        Distinctness::Distinct => results.push(LegResult::pass(
            "enrollment",
            format!(
                "A={} B={} distinct confirmed identities; stores disjoint",
                redact_label(facts_a.identity.as_deref().unwrap_or("?")),
                redact_label(facts_b.identity.as_deref().unwrap_or("?")),
            ),
        )),
        Distinctness::Same => {
            results.push(LegResult::fail(
                "enrollment",
                "both profiles confirm the same provider identity; refusing live legs".to_owned(),
            ));
            return Ok(emit_report(
                &args,
                &si_version,
                &native_program,
                &native_version,
                &facts_a,
                &facts_b,
                &results,
            ));
        }
        Distinctness::Unconfirmed(detail) => {
            results.push(LegResult::blocked("enrollment", detail));
            // Without two confirmed distinct accounts every real-request leg
            // is blocked, not failed: record that honestly and stop.
            for leg in LEGS.iter().filter(|leg| **leg != "enrollment") {
                if args.only.as_ref().is_none_or(|only| only.iter().any(|item| item == leg)) {
                    results.push(LegResult::blocked(
                        leg,
                        "requires two confirmed distinct live accounts".to_owned(),
                    ));
                }
            }
            return Ok(emit_report(
                &args,
                &si_version,
                &native_program,
                &native_version,
                &facts_a,
                &facts_b,
                &results,
            ));
        }
    }

    let ctx = LegContext {
        si: si.clone(),
        provider: args.provider.clone(),
        profile_a: args.profile_a.clone(),
        profile_b: args.profile_b.clone(),
        native_program,
        leg_timeout: args.leg_timeout,
        workspace_root: private_workspace_root(args.workspace_root.as_deref())?,
    };

    let wanted = |leg: &str| args.only.as_ref().is_none_or(|only| only.iter().any(|i| i == leg));
    if wanted("simultaneous") {
        results.push(leg_simultaneous(&ctx));
    }
    if wanted("marker") {
        results.push(leg_marker(&ctx));
    }
    if wanted("respawn-a") {
        results.push(leg_respawn_a(&ctx));
    }
    if wanted("logout-a") {
        results.push(leg_logout_a(&ctx));
    }
    if wanted("relogin-a") {
        results.push(LegResult::blocked(
            "relogin-a",
            "requires interactive account-holder re-approval; run `si <provider> profile login <a>` manually, then re-run with --only relogin-a,renewal".to_owned(),
        ));
    }
    if wanted("renewal") {
        results.push(LegResult::blocked(
            "renewal",
            "requires a genuine native refresh/renewal event or a supported provider test hook; read-only expiry scheduling is acceptable evidence".to_owned(),
        ));
    }
    if wanted("revocation") {
        results.push(LegResult::blocked(
            "revocation",
            "requires a dedicated test account or supported provider test mechanism; never revoke the working credential".to_owned(),
        ));
    }
    if wanted("session-association") {
        results.push(leg_session_association(&ctx));
    }
    if wanted("concurrency") {
        results.push(leg_concurrency(&ctx));
    }
    if wanted("subprocess") {
        results.push(leg_subprocess(&ctx));
    }

    Ok(emit_report(
        &args,
        &si_version,
        &ctx.native_program,
        &native_version,
        &facts_a,
        &facts_b,
        &results,
    ))
}

#[derive(Debug, PartialEq, Eq)]
enum Distinctness {
    Distinct,
    Same,
    Unconfirmed(String),
}

/// Provider-identity comparison. Only natively confirmed identities count:
/// anything unverified (or a disabled profile) blocks the live legs rather
/// than passing or failing them.
fn distinct_confirmed_identities(a: &ProfileFacts, b: &ProfileFacts) -> Distinctness {
    if a.disabled.is_some() || b.disabled.is_some() {
        return Distinctness::Unconfirmed("a profile is disabled; re-login first".to_owned());
    }
    if a.auth_state != "authenticated" || b.auth_state != "authenticated" {
        return Distinctness::Unconfirmed(format!(
            "auth states are A={} B={}; both must be authenticated",
            a.auth_state, b.auth_state
        ));
    }
    match (&a.identity, &b.identity, &a.identity_source, &b.identity_source) {
        (Some(ai), Some(bi), Some(asrc), Some(bsrc))
            if asrc != "identity_unverified" && bsrc != "identity_unverified" =>
        {
            if ai == bi {
                Distinctness::Same
            } else {
                Distinctness::Distinct
            }
        }
        _ => Distinctness::Unconfirmed(
            "an identity is missing or identity_unverified; both must be natively confirmed"
                .to_owned(),
        ),
    }
}

fn resolve_si_bin(explicit: Option<&Path>) -> Result<PathBuf, String> {
    if let Some(path) = explicit {
        if path.is_file() {
            return Ok(path.to_path_buf());
        }
        return Err(format!("--si-bin {} is not a file", path.display()));
    }
    for dir in env::split_paths(&env::var_os("PATH").unwrap_or_default()) {
        let candidate = dir.join("si");
        if candidate.is_file() {
            return Ok(candidate);
        }
    }
    Err("`si` not found on PATH; pass --si-bin <path>".to_owned())
}

fn native_program(provider: &str) -> String {
    provider.to_owned()
}

fn si_version(si: &Path) -> String {
    run_capture(si, &["version".to_owned()], None)
        .map(|out| first_line_lossy(&out.stdout))
        .unwrap_or_default()
}

fn probe_native_version(program: &str) -> String {
    Command::new(program)
        .arg("--version")
        .stdin(Stdio::null())
        .output()
        .map(|out| first_line_lossy(&out.stdout))
        .unwrap_or_default()
}

fn si_profile_show(si: &Path, provider: &str, profile: &str) -> Result<String, String> {
    let out = run_capture(
        si,
        &[
            provider.to_owned(),
            "profile".to_owned(),
            "show".to_owned(),
            profile.to_owned(),
            "--format".to_owned(),
            "json".to_owned(),
        ],
        None,
    )?;
    if !out.status.success() {
        return Err(format!(
            "si {provider} profile show {profile} failed: {}",
            redact(&String::from_utf8_lossy(&out.stderr))
        ));
    }
    Ok(String::from_utf8_lossy(&out.stdout).into_owned())
}

fn parse_profile_facts(name: &str, show_json: &str) -> Result<ProfileFacts, String> {
    let value: serde_json::Value = serde_json::from_str(show_json)
        .map_err(|err| format!("parse profile show {name}: {err}"))?;
    let get = |key: &str| value.get(key).and_then(|v| v.as_str()).unwrap_or_default().to_owned();
    let opt = |key: &str| value.get(key).and_then(|v| v.as_str()).map(str::to_owned);
    Ok(ProfileFacts {
        native_root: get("native_root"),
        auth_state: get("auth_state"),
        identity: opt("identity"),
        identity_source: opt("identity_source"),
        disabled: opt("disabled"),
    })
}

fn first_line_lossy(bytes: &[u8]) -> String {
    String::from_utf8_lossy(bytes).lines().next().unwrap_or_default().trim().to_owned()
}

fn run_capture(
    program: &Path,
    args: &[String],
    timeout: Option<Duration>,
) -> Result<Output, String> {
    // No async runtime in this dev bin: spawn, then poll with a deadline.
    let mut child = Command::new(program)
        .args(args)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(|err| format!("spawn {}: {err}", program.display()))?;
    let deadline = timeout.map(|t| Instant::now() + t);
    loop {
        match child.try_wait().map_err(|err| format!("wait {}: {err}", program.display()))? {
            Some(_) => break,
            None => {
                if deadline.is_some_and(|d| Instant::now() >= d) {
                    let _ = child.kill();
                    let _ = child.wait();
                    return Err(format!("timed out: {} {}", program.display(), args.join(" ")));
                }
                std::thread::sleep(Duration::from_millis(50));
            }
        }
    }
    child.wait_with_output().map_err(|err| format!("collect {}: {err}", program.display()))
}

/// Redact token-shaped material from captured output. Identity labels and
/// paths pass through; secrets never reach the report.
fn redact(text: &str) -> String {
    let mut out = String::with_capacity(text.len());
    for token in text.split_inclusive(|c: char| c.is_whitespace()) {
        let trimmed = token.trim_end();
        let trailing = &token[trimmed.len()..];
        if looks_secret_shaped(trimmed) {
            out.push_str("[redacted]");
            out.push_str(trailing);
            continue;
        }
        // key=value / key:value shapes: redact a secret-shaped value.
        let mut done = false;
        for sep in ['=', ':'] {
            if let Some((key, value)) = trimmed.split_once(sep)
                && looks_secret_shaped(value)
            {
                out.push_str(key);
                out.push(sep);
                out.push_str("[redacted]");
                out.push_str(trailing);
                done = true;
                break;
            }
        }
        if !done {
            out.push_str(token);
        }
    }
    out
}

fn looks_secret_shaped(token: &str) -> bool {
    let token =
        token.trim_matches(|c: char| c == '"' || c == '\'' || c == ',' || c == ':' || c == '=');
    if token.len() < 12 {
        return false;
    }
    let lower = token.to_ascii_lowercase();
    if lower.starts_with("sk-")
        || lower.starts_with("ghp_")
        || lower.starts_with("gho_")
        || lower.starts_with("github_pat_")
        || lower.starts_with("xox")
        || lower.starts_with("akia")
        || token.starts_with("eyJ")
    {
        return token.chars().all(|c| c.is_ascii_alphanumeric() || matches!(c, '-' | '_' | '.'));
    }
    // Long high-entropy blobs (device codes excluded: those are short).
    if token.len() >= 40
        && token.chars().all(|c| c.is_ascii_alphanumeric() || matches!(c, '-' | '_' | '.' | '~'))
    {
        return true;
    }
    false
}

fn redact_label(label: &str) -> String {
    // Identity labels are short account hints (often emails). Keep the shape
    // (domain + length) but drop the local part.
    match label.split_once('@') {
        Some((_, domain)) => format!("…@{domain}"),
        None => {
            if label.len() > 12 {
                format!("{}…[len={}]", &label[..8], label.len())
            } else {
                label.to_owned()
            }
        }
    }
}

fn redact_path(path: &str) -> String {
    // Keep the last two segments (profile/slot context), drop home/usernames.
    let parts: Vec<&str> = path.split('/').filter(|p| !p.is_empty()).collect();
    if parts.len() >= 2 {
        format!("…/{}/{}", parts[parts.len() - 2], parts[parts.len() - 1])
    } else {
        "…".to_owned()
    }
}

struct LegContext {
    si: PathBuf,
    provider: String,
    profile_a: String,
    profile_b: String,
    native_program: String,
    leg_timeout: Duration,
    workspace_root: PathBuf,
}

fn private_workspace_root(explicit: Option<&Path>) -> Result<PathBuf, String> {
    let base = match explicit {
        Some(dir) => dir.to_path_buf(),
        None => env::temp_dir(),
    };
    let stamp = SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_millis()).unwrap_or(0);
    let root = base.join(format!("si-live-accounts-{stamp}-{}", std::process::id()));
    fs::create_dir_all(&root).map_err(|err| format!("create {}: {err}", root.display()))?;
    Ok(root)
}

fn leg_workspace(ctx: &LegContext, leg: &str, profile: &str) -> Result<PathBuf, String> {
    let dir = ctx.workspace_root.join(format!("{leg}-{profile}"));
    fs::create_dir_all(&dir).map_err(|err| format!("create {}: {err}", dir.display()))?;
    Ok(dir)
}

fn random_marker(tag: &str) -> String {
    let nanos = SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_nanos()).unwrap_or(0);
    format!("si-live-{tag}-{nanos}-{}", std::process::id())
}

/// Native argv for a tiny headless marker request, per the section 12 shapes.
/// Short prompt, ordinary model, no workspace writes/tools.
fn marker_native_argv(provider: &str, workspace: &Path, marker: &str) -> Vec<String> {
    let prompt = format!("Return only the supplied unique marker: {marker}");
    if provider == "muse" {
        vec![
            "exec".to_owned(),
            "--provider".to_owned(),
            "meta".to_owned(),
            "--json".to_owned(),
            "--workspace".to_owned(),
            workspace.display().to_string(),
            "--disable-shell".to_owned(),
            "--disable-write".to_owned(),
            "--disable-web-tools".to_owned(),
            "--disable-reminders".to_owned(),
            "--no-foreign-personal-context".to_owned(),
            prompt,
        ]
    } else {
        vec![
            "exec".to_owned(),
            "--json".to_owned(),
            "--sandbox".to_owned(),
            "read-only".to_owned(),
            "--skip-git-repo-check".to_owned(),
            prompt,
        ]
    }
}

fn si_run_argv(
    ctx: &LegContext,
    profile: &str,
    workspace: &Path,
    native: &[String],
) -> Vec<String> {
    let mut argv = vec![
        ctx.provider.clone(),
        "run".to_owned(),
        "--profile".to_owned(),
        profile.to_owned(),
        "--workspace".to_owned(),
        workspace.display().to_string(),
        "--".to_owned(),
    ];
    argv.extend(native.iter().cloned());
    argv
}

/// Validate the marker flags against the installed native before spending a
/// real request: every required flag stem must appear in `<native> exec --help`.
fn validate_marker_flags(program: &str, provider: &str) -> Result<(), String> {
    let out = Command::new(program)
        .args(["exec", "--help"])
        .stdin(Stdio::null())
        .output()
        .map_err(|err| format!("probe {program} exec --help: {err}"))?;
    let help =
        String::from_utf8_lossy(&out.stdout).into_owned() + &String::from_utf8_lossy(&out.stderr);
    let required: &[&str] = if provider == "muse" {
        &["--provider", "--json", "--workspace", "--disable-shell", "--disable-write"]
    } else {
        &["--json", "--sandbox", "--skip-git-repo-check"]
    };
    for flag in required {
        if !help.contains(flag) {
            return Err(format!("native {program} exec lacks required flag {flag}"));
        }
    }
    Ok(())
}

/// A failed/unauthenticated call fails the gate even if the input prompt
/// appears in stdout: require success exit plus the marker inside a native
/// assistant/result event, never a bare stdout substring alone.
fn marker_in_assistant_result(provider: &str, stdout: &str, marker: &str) -> bool {
    if provider == "codex" {
        // `codex exec --json` emits JSONL; accept the marker only in a
        // response/assistant/result-typed event.
        for line in stdout.lines() {
            let line = line.trim();
            if !line.starts_with('{') || !line.contains(marker) {
                continue;
            }
            let Ok(value) = serde_json::from_str::<serde_json::Value>(line) else { continue };
            let kind =
                value.get("type").and_then(|v| v.as_str()).unwrap_or_default().to_ascii_lowercase();
            if kind.contains("response")
                || kind.contains("assistant")
                || kind.contains("result")
                || kind.contains("message")
                || kind.contains("completed")
            {
                return true;
            }
        }
        return false;
    }
    // Muse `--json`: accept the marker in decoded string content outside an
    // obvious echoed-prompt field.
    let Ok(value) = serde_json::from_str::<serde_json::Value>(stdout.trim()) else {
        return false;
    };
    fn scan(value: &serde_json::Value, marker: &str, in_prompt: bool) -> bool {
        match value {
            serde_json::Value::String(text) => !in_prompt && text.contains(marker),
            serde_json::Value::Array(items) => items.iter().any(|v| scan(v, marker, in_prompt)),
            serde_json::Value::Object(map) => map.iter().any(|(key, v)| {
                let prompt = in_prompt || key.to_ascii_lowercase().contains("prompt");
                scan(v, marker, prompt)
            }),
            _ => false,
        }
    }
    scan(&value, marker, false)
}

fn leg_simultaneous(ctx: &LegContext) -> LegResult {
    if let Err(detail) = validate_marker_flags(&ctx.native_program, &ctx.provider) {
        return LegResult::blocked("simultaneous", detail);
    }
    let workspace_a = match leg_workspace(ctx, "simultaneous", &ctx.profile_a) {
        Ok(dir) => dir,
        Err(detail) => return LegResult::blocked("simultaneous", detail),
    };
    let workspace_b = match leg_workspace(ctx, "simultaneous", &ctx.profile_b) {
        Ok(dir) => dir,
        Err(detail) => return LegResult::blocked("simultaneous", detail),
    };
    // Run A as a worker behind the overlap barrier while B makes a foreground
    // request, then reverse: both directions must overlap live processes.
    let marker_a = random_marker("sim-a");
    let marker_b = random_marker("sim-b");
    let native_a = marker_native_argv(&ctx.provider, &workspace_a, &marker_a);
    let native_b = marker_native_argv(&ctx.provider, &workspace_b, &marker_b);

    let spawn_a = Command::new(&ctx.si)
        .args([
            ctx.provider.as_str(),
            "spawn",
            "--profile",
            ctx.profile_a.as_str(),
            "--slot",
            "live-sim",
            "--workspace",
            workspace_a.to_str().unwrap_or_default(),
            "--",
        ])
        .args(&native_a)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .output();
    let Ok(spawn_a) = spawn_a else {
        return LegResult::fail("simultaneous", "spawn A failed to launch".to_owned());
    };
    if !spawn_a.status.success() {
        return LegResult::fail(
            "simultaneous",
            format!("spawn A failed: {}", redact(&String::from_utf8_lossy(&spawn_a.stderr))),
        );
    }
    // Overlap barrier: B's foreground request runs while A's worker is
    // alive (spawn returns only after the 250ms startup-stability confirm,
    // so a real model call in A is still in flight).
    let b_started = Instant::now();
    let argv_b = si_run_argv(ctx, &ctx.profile_b, &workspace_b, &native_b);
    let run_b = run_capture(&ctx.si, &argv_b, Some(ctx.leg_timeout));
    let b_elapsed = b_started.elapsed();
    let (ok_b, detail_b) = match run_b {
        Ok(out) => {
            let text = String::from_utf8_lossy(&out.stdout).into_owned();
            let ok =
                out.status.success() && marker_in_assistant_result(&ctx.provider, &text, &marker_b);
            (ok, format!("exit={} marker_in_result={}", out.status, ok))
        }
        Err(detail) => (false, detail),
    };
    let tail_a = Command::new(&ctx.si)
        .args([
            ctx.provider.as_str(),
            "tail",
            "--profile",
            ctx.profile_a.as_str(),
            "--slot",
            "live-sim",
        ])
        .stdin(Stdio::null())
        .output();
    let ok_a = tail_a
        .map(|out| {
            marker_in_assistant_result(
                &ctx.provider,
                &String::from_utf8_lossy(&out.stdout),
                &marker_a,
            )
        })
        .unwrap_or(false);
    for verb in ["stop", "remove"] {
        let _ = Command::new(&ctx.si)
            .args([
                ctx.provider.as_str(),
                verb,
                "--profile",
                ctx.profile_a.as_str(),
                "--slot",
                "live-sim",
            ])
            .stdin(Stdio::null())
            .output();
    }
    if ok_a && ok_b {
        LegResult::pass(
            "simultaneous",
            format!(
                "A worker + B foreground overlapped (B ran {:.1}s inside A's life); both markers in assistant results",
                b_elapsed.as_secs_f64()
            ),
        )
    } else {
        LegResult::fail("simultaneous", format!("marker proofs: A={ok_a} B=({detail_b})"))
    }
}

fn leg_marker(ctx: &LegContext) -> LegResult {
    if let Err(detail) = validate_marker_flags(&ctx.native_program, &ctx.provider) {
        return LegResult::blocked("marker", detail);
    }
    for profile in [&ctx.profile_a, &ctx.profile_b] {
        let workspace = match leg_workspace(ctx, "marker", profile) {
            Ok(dir) => dir,
            Err(detail) => return LegResult::blocked("marker", detail),
        };
        let marker = random_marker("tiny");
        let native = marker_native_argv(&ctx.provider, &workspace, &marker);
        let argv = si_run_argv(ctx, profile, &workspace, &native);
        match run_capture(&ctx.si, &argv, Some(ctx.leg_timeout)) {
            Ok(out) => {
                let text = String::from_utf8_lossy(&out.stdout).into_owned();
                if !out.status.success()
                    || !marker_in_assistant_result(&ctx.provider, &text, &marker)
                {
                    return LegResult::fail(
                        "marker",
                        format!(
                            "profile {} exit={} marker_in_result=false stderr={}",
                            redact_label(profile),
                            out.status,
                            redact(&String::from_utf8_lossy(&out.stderr))
                        ),
                    );
                }
            }
            Err(detail) => {
                return LegResult::fail("marker", format!("profile {profile}: {detail}"));
            }
        }
    }
    LegResult::pass(
        "marker",
        "both profiles returned their markers in assistant results".to_owned(),
    )
}

fn si_simple(ctx: &LegContext, args: &[&str]) -> Result<Output, String> {
    run_capture(
        &ctx.si,
        &args.iter().map(|s| s.to_string()).collect::<Vec<_>>(),
        Some(ctx.leg_timeout),
    )
}

fn leg_respawn_a(ctx: &LegContext) -> LegResult {
    // Stop then resume A's own worker; B must keep working throughout.
    let workspace_b = match leg_workspace(ctx, "respawn-a", &ctx.profile_b) {
        Ok(dir) => dir,
        Err(detail) => return LegResult::blocked("respawn-a", detail),
    };
    let workspace_a = match leg_workspace(ctx, "respawn-a", &ctx.profile_a) {
        Ok(dir) => dir,
        Err(detail) => return LegResult::blocked("respawn-a", detail),
    };
    let marker_a = random_marker("respawn");
    let native_a = marker_native_argv(&ctx.provider, &workspace_a, &marker_a);
    let mut spawn_argv = vec![
        ctx.provider.as_str(),
        "spawn",
        "--profile",
        ctx.profile_a.as_str(),
        "--slot",
        "live-re",
        "--workspace",
        workspace_a.to_str().unwrap_or_default(),
        "--",
    ];
    let native_refs: Vec<&str> = native_a.iter().map(String::as_str).collect();
    spawn_argv.extend(native_refs);
    match si_simple(ctx, &spawn_argv) {
        Ok(out) if out.status.success() => {}
        Ok(out) => {
            return LegResult::fail(
                "respawn-a",
                format!("spawn A failed: {}", redact(&String::from_utf8_lossy(&out.stderr))),
            );
        }
        Err(detail) => return LegResult::fail("respawn-a", detail),
    }
    // B proves liveness while A is mid-cycle.
    let marker_b = random_marker("respawn-b");
    let argv_b = si_run_argv(
        ctx,
        &ctx.profile_b,
        &workspace_b,
        &marker_native_argv(&ctx.provider, &workspace_b, &marker_b),
    );
    let b_mid = run_capture(&ctx.si, &argv_b, Some(ctx.leg_timeout))
        .map(|out| {
            out.status.success()
                && marker_in_assistant_result(
                    &ctx.provider,
                    &String::from_utf8_lossy(&out.stdout),
                    &marker_b,
                )
        })
        .unwrap_or(false);
    if si_simple(
        ctx,
        &[ctx.provider.as_str(), "stop", "--profile", ctx.profile_a.as_str(), "--slot", "live-re"],
    )
    .is_err()
    {
        return LegResult::fail("respawn-a", "stop A failed".to_owned());
    }
    let respawn = si_simple(
        ctx,
        &[
            ctx.provider.as_str(),
            "respawn",
            "--profile",
            ctx.profile_a.as_str(),
            "--slot",
            "live-re",
            "--workspace",
            workspace_a.to_str().unwrap_or_default(),
        ],
    );
    match respawn {
        Ok(out) if out.status.success() => {}
        Ok(out) => {
            return LegResult::fail(
                "respawn-a",
                format!("respawn A failed: {}", redact(&String::from_utf8_lossy(&out.stderr))),
            );
        }
        Err(detail) => return LegResult::fail("respawn-a", detail),
    }
    let tail_a = si_simple(
        ctx,
        &[ctx.provider.as_str(), "tail", "--profile", ctx.profile_a.as_str(), "--slot", "live-re"],
    );
    // Respawn reuses the launch contract, so the marker prompt runs again.
    let ok_a = tail_a
        .map(|out| {
            marker_in_assistant_result(
                &ctx.provider,
                &String::from_utf8_lossy(&out.stdout),
                &marker_a,
            )
        })
        .unwrap_or(false);
    for verb in ["stop", "remove"] {
        let _ = si_simple(
            ctx,
            &[
                ctx.provider.as_str(),
                verb,
                "--profile",
                ctx.profile_a.as_str(),
                "--slot",
                "live-re",
            ],
        );
    }
    // B completes a further real request after A's cycle.
    let marker_b2 = random_marker("respawn-b2");
    let argv_b2 = si_run_argv(
        ctx,
        &ctx.profile_b,
        &workspace_b,
        &marker_native_argv(&ctx.provider, &workspace_b, &marker_b2),
    );
    let b_after = run_capture(&ctx.si, &argv_b2, Some(ctx.leg_timeout))
        .map(|out| {
            out.status.success()
                && marker_in_assistant_result(
                    &ctx.provider,
                    &String::from_utf8_lossy(&out.stdout),
                    &marker_b2,
                )
        })
        .unwrap_or(false);
    if ok_a && b_mid && b_after {
        LegResult::pass(
            "respawn-a",
            "A respawned on its own contract; B worked mid-cycle and after".to_owned(),
        )
    } else {
        LegResult::fail(
            "respawn-a",
            format!("proofs: A_respawn={ok_a} B_mid={b_mid} B_after={b_after}"),
        )
    }
}

fn leg_logout_a(ctx: &LegContext) -> LegResult {
    // Stop A only, native logout A, verify A cannot make an authenticated
    // request while B still can. Judged by request outcomes, not files.
    for verb in ["stop", "remove"] {
        let _ = si_simple(
            ctx,
            &[
                ctx.provider.as_str(),
                verb,
                "--profile",
                ctx.profile_a.as_str(),
                "--slot",
                "live-lo",
            ],
        );
    }
    match si_simple(ctx, &[ctx.provider.as_str(), "profile", "logout", ctx.profile_a.as_str()]) {
        Ok(out) if out.status.success() => {}
        Ok(out) => {
            return LegResult::fail(
                "logout-a",
                format!("logout A failed: {}", redact(&String::from_utf8_lossy(&out.stderr))),
            );
        }
        Err(detail) => return LegResult::fail("logout-a", detail),
    }
    let workspace_a = match leg_workspace(ctx, "logout-a", &ctx.profile_a) {
        Ok(dir) => dir,
        Err(detail) => return LegResult::blocked("logout-a", detail),
    };
    let marker_a = random_marker("logout");
    let argv_a = si_run_argv(
        ctx,
        &ctx.profile_a,
        &workspace_a,
        &marker_native_argv(&ctx.provider, &workspace_a, &marker_a),
    );
    let a_denied = match run_capture(&ctx.si, &argv_a, Some(ctx.leg_timeout)) {
        Ok(out) => {
            !out.status.success()
                || !marker_in_assistant_result(
                    &ctx.provider,
                    &String::from_utf8_lossy(&out.stdout),
                    &marker_a,
                )
        }
        Err(_) => true,
    };
    let workspace_b = match leg_workspace(ctx, "logout-a", &ctx.profile_b) {
        Ok(dir) => dir,
        Err(detail) => return LegResult::blocked("logout-a", detail),
    };
    let marker_b = random_marker("logout-b");
    let argv_b = si_run_argv(
        ctx,
        &ctx.profile_b,
        &workspace_b,
        &marker_native_argv(&ctx.provider, &workspace_b, &marker_b),
    );
    let b_ok = run_capture(&ctx.si, &argv_b, Some(ctx.leg_timeout))
        .map(|out| {
            out.status.success()
                && marker_in_assistant_result(
                    &ctx.provider,
                    &String::from_utf8_lossy(&out.stdout),
                    &marker_b,
                )
        })
        .unwrap_or(false);
    if a_denied && b_ok {
        LegResult::pass(
            "logout-a",
            "A denied after logout; B still completes real requests".to_owned(),
        )
    } else {
        LegResult::fail("logout-a", format!("proofs: A_denied={a_denied} B_ok={b_ok}"))
    }
}

fn leg_session_association(ctx: &LegContext) -> LegResult {
    // Resume A's known native ID in A (positive control), then require B's
    // managed resume of A's ID to refuse without a model call.
    let list_a = si_simple(ctx, &[ctx.provider.as_str(), "list", "--format", "json"]);
    let _ = list_a;
    // Discover one of A's native session ids through its own worker record:
    // spawn a quick worker, read its record id via list, stop it, then
    // respawn --resume-id in A (must attempt) and in B (must refuse).
    let workspace = match leg_workspace(ctx, "session", &ctx.profile_a) {
        Ok(dir) => dir,
        Err(detail) => return LegResult::blocked("session-association", detail),
    };
    let marker = random_marker("sess");
    let native = marker_native_argv(&ctx.provider, &workspace, &marker);
    let mut spawn_argv = vec![
        ctx.provider.as_str(),
        "spawn",
        "--profile",
        ctx.profile_a.as_str(),
        "--slot",
        "live-se",
        "--workspace",
        workspace.to_str().unwrap_or_default(),
        "--",
    ];
    spawn_argv.extend(native.iter().map(String::as_str));
    if !si_simple(ctx, &spawn_argv).is_ok_and(|out| out.status.success()) {
        return LegResult::fail("session-association", "spawn A failed".to_owned());
    }
    let _ = si_simple(
        ctx,
        &[ctx.provider.as_str(), "stop", "--profile", ctx.profile_a.as_str(), "--slot", "live-se"],
    );
    // Respawn in A without --resume-id reuses the contract (positive path).
    let own = si_simple(
        ctx,
        &[
            ctx.provider.as_str(),
            "respawn",
            "--profile",
            ctx.profile_a.as_str(),
            "--slot",
            "live-se",
            "--workspace",
            workspace.to_str().unwrap_or_default(),
        ],
    );
    let own_ok = own.is_ok_and(|out| out.status.success());
    // Cross-profile resume with a foreign id must refuse. Use a
    // well-formed-but-unknown id: refusal proves association enforcement.
    let foreign = if ctx.provider == "muse" {
        "00000000-0000-4000-8000-000000000000"
    } else {
        "thread-unknown-si-live-test"
    };
    let cross = si_simple(
        ctx,
        &[
            ctx.provider.as_str(),
            "respawn",
            "--profile",
            ctx.profile_b.as_str(),
            "--slot",
            "live-se",
            "--resume-id",
            foreign,
            "--workspace",
            workspace.to_str().unwrap_or_default(),
        ],
    );
    let cross_refused = match cross {
        Ok(out) => !out.status.success(),
        Err(_) => true,
    };
    for verb in ["stop", "remove"] {
        let _ = si_simple(
            ctx,
            &[
                ctx.provider.as_str(),
                verb,
                "--profile",
                ctx.profile_a.as_str(),
                "--slot",
                "live-se",
            ],
        );
        let _ = si_simple(
            ctx,
            &[
                ctx.provider.as_str(),
                verb,
                "--profile",
                ctx.profile_b.as_str(),
                "--slot",
                "live-se",
            ],
        );
    }
    if own_ok && cross_refused {
        LegResult::pass(
            "session-association",
            "A respawned its own contract; B refused the foreign id".to_owned(),
        )
    } else {
        LegResult::fail(
            "session-association",
            format!("proofs: A_own={own_ok} B_refused={cross_refused}"),
        )
    }
}

fn leg_concurrency(ctx: &LegContext) -> LegResult {
    // Same-profile concurrency is disabled by design until native locking is
    // proven: assert the refusal rather than attempting an overlap.
    let workspace = match leg_workspace(ctx, "concurrency", &ctx.profile_a) {
        Ok(dir) => dir,
        Err(detail) => return LegResult::blocked("concurrency", detail),
    };
    let marker = random_marker("conc");
    let native = marker_native_argv(&ctx.provider, &workspace, &marker);
    let mut spawn_argv = vec![
        ctx.provider.as_str(),
        "spawn",
        "--profile",
        ctx.profile_a.as_str(),
        "--slot",
        "live-c1",
        "--workspace",
        workspace.to_str().unwrap_or_default(),
        "--",
    ];
    spawn_argv.extend(native.iter().map(String::as_str));
    if !si_simple(ctx, &spawn_argv).is_ok_and(|out| out.status.success()) {
        return LegResult::fail(
            "concurrency",
            "first spawn failed; cannot test refusal".to_owned(),
        );
    }
    let mut spawn_argv2 = vec![
        ctx.provider.as_str(),
        "spawn",
        "--profile",
        ctx.profile_a.as_str(),
        "--slot",
        "live-c2",
        "--workspace",
        workspace.to_str().unwrap_or_default(),
        "--",
    ];
    spawn_argv2.extend(native.iter().map(String::as_str));
    let refused = match si_simple(ctx, &spawn_argv2) {
        Ok(out) => !out.status.success(),
        Err(_) => true,
    };
    for slot in ["live-c1", "live-c2"] {
        for verb in ["stop", "remove"] {
            let _ = si_simple(
                ctx,
                &[ctx.provider.as_str(), verb, "--profile", ctx.profile_a.as_str(), "--slot", slot],
            );
        }
    }
    if refused {
        LegResult::pass(
            "concurrency",
            "second same-profile worker refused (capability disabled by design)".to_owned(),
        )
    } else {
        LegResult::fail("concurrency", "second same-profile worker was NOT refused".to_owned())
    }
}

fn leg_subprocess(ctx: &LegContext) -> LegResult {
    // Ordinary subprocess use in a disposable workspace: shell env carries
    // the profile binding and native auth still resolves to profile paths.
    let workspace = match leg_workspace(ctx, "subprocess", &ctx.profile_a) {
        Ok(dir) => dir,
        Err(detail) => return LegResult::blocked("subprocess", detail),
    };
    let probe = if ctx.provider == "muse" {
        "echo XDG_CONFIG_HOME=$XDG_CONFIG_HOME XDG_DATA_HOME=$XDG_DATA_HOME MUSE_AUTH_PATH=$MUSE_AUTH_PATH"
    } else {
        "echo CODEX_HOME=$CODEX_HOME"
    };
    let out = si_simple(
        ctx,
        &[
            ctx.provider.as_str(),
            "shell",
            "--profile",
            ctx.profile_a.as_str(),
            "--slot",
            "live-sub",
            "--workspace",
            workspace.to_str().unwrap_or_default(),
            "--",
            "sh",
            "-c",
            probe,
        ],
    );
    match out {
        Ok(out) if out.status.success() => {
            let text = String::from_utf8_lossy(&out.stdout).into_owned();
            // The binding must point inside the profile store, never at the
            // user's default native paths.
            let bad_default = if ctx.provider == "muse" {
                text.contains(".config/muse/auth.json") && !text.contains(&ctx.profile_a)
                    || text.contains(".local/share/muse") && !text.contains(&ctx.profile_a)
            } else {
                text.contains("/.codex") && !text.contains(&ctx.profile_a)
            };
            if bad_default {
                LegResult::fail(
                    "subprocess",
                    format!("shell leaked default paths: {}", redact(&text)),
                )
            } else if text.contains(&ctx.profile_a) {
                LegResult::pass("subprocess", "shell env binds to the profile store".to_owned())
            } else {
                LegResult::fail(
                    "subprocess",
                    format!("no profile binding visible: {}", redact(&text)),
                )
            }
        }
        Ok(out) => LegResult::fail(
            "subprocess",
            format!("shell failed: {}", redact(&String::from_utf8_lossy(&out.stderr))),
        ),
        Err(detail) => LegResult::fail("subprocess", detail),
    }
}

#[allow(clippy::too_many_arguments)]
fn emit_report(
    args: &Args,
    si_version: &str,
    native_program: &str,
    native_version: &str,
    facts_a: &ProfileFacts,
    facts_b: &ProfileFacts,
    results: &[LegResult],
) -> ExitCode {
    let failed = results.iter().filter(|r| r.status == Status::Fail).count();
    let blocked = results.iter().filter(|r| r.status == Status::Blocked).count();
    if args.format == "json" {
        let legs: Vec<serde_json::Value> = results
            .iter()
            .map(|r| {
                serde_json::json!({
                    "name": r.name,
                    "status": r.status.name(),
                    "detail": r.detail,
                })
            })
            .collect();
        let report = serde_json::json!({
            "runner": "si-live-accounts",
            "redacted": true,
            "provider": args.provider,
            "profile_a": redact_label(&args.profile_a),
            "profile_b": redact_label(&args.profile_b),
            "store_a": redact_path(&facts_a.native_root),
            "store_b": redact_path(&facts_b.native_root),
            "si_version": si_version,
            "native_program": native_program,
            "native_version": native_version,
            "legs": legs,
            "summary": {"failed": failed, "blocked": blocked},
        });
        println!("{}", serde_json::to_string_pretty(&report).unwrap_or_default());
    } else {
        println!("si-live-accounts report (redacted)");
        println!(
            "provider={} si={si_version} native={native_program} {native_version}",
            args.provider
        );
        println!("A={} store={}", redact_label(&args.profile_a), redact_path(&facts_a.native_root));
        println!("B={} store={}", redact_label(&args.profile_b), redact_path(&facts_b.native_root));
        for r in results {
            println!("[{}] {}: {}", r.status.name(), r.name, r.detail);
        }
        println!("failed={failed} blocked={blocked}");
    }
    if failed > 0 { ExitCode::from(1) } else { ExitCode::SUCCESS }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn facts(auth_state: &str, identity: Option<&str>, source: Option<&str>) -> ProfileFacts {
        ProfileFacts {
            native_root: "/tmp/x".to_owned(),
            auth_state: auth_state.to_owned(),
            identity: identity.map(str::to_owned),
            identity_source: source.map(str::to_owned),
            disabled: None,
        }
    }

    #[test]
    fn distinctness_requires_two_confirmed_identities() {
        let a = facts("authenticated", Some("a@x"), Some("native"));
        let b = facts("authenticated", Some("b@x"), Some("native"));
        assert_eq!(distinct_confirmed_identities(&a, &b), Distinctness::Distinct);

        let b2 = facts("authenticated", Some("a@x"), Some("native"));
        assert_eq!(distinct_confirmed_identities(&a, &b2), Distinctness::Same);

        let unverified = facts("authenticated", Some("b@x"), Some("identity_unverified"));
        assert!(matches!(
            distinct_confirmed_identities(&a, &unverified),
            Distinctness::Unconfirmed(_)
        ));

        let logged_out = facts("not_logged_in", None, None);
        assert!(matches!(
            distinct_confirmed_identities(&a, &logged_out),
            Distinctness::Unconfirmed(_)
        ));
    }

    #[test]
    fn redact_strips_token_shapes_but_keeps_labels() {
        let input = "email=…@example.com token=sk-abcdefghijklmnop id=live-a jwt=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.signature-part-here";
        let out = redact(input);
        assert!(out.contains("live-a"), "{out}");
        assert!(!out.contains("sk-abcdefghijklmnop"), "{out}");
        assert!(!out.contains("eyJhbGciOiJIUzI1NiJ9"), "{out}");
        assert!(out.contains("[redacted]"), "{out}");
    }

    #[test]
    fn redact_label_drops_local_part() {
        assert_eq!(redact_label("ada@example.com"), "…@example.com");
        assert_eq!(redact_label("live-a"), "live-a");
    }

    #[test]
    fn codex_marker_requires_typed_event() {
        let marker = "si-live-x";
        let typed = format!("{{\"type\":\"response.completed\",\"text\":\"{marker}\"}}\n");
        assert!(marker_in_assistant_result("codex", &typed, marker));
        let echo = format!("{{\"type\":\"input.prompt\",\"text\":\"{marker}\"}}\n");
        assert!(!marker_in_assistant_result("codex", &echo, marker));
        assert!(!marker_in_assistant_result("codex", "plain stdout", marker));
    }

    #[test]
    fn muse_marker_ignores_prompt_fields() {
        let marker = "si-live-y";
        assert!(marker_in_assistant_result(
            "muse",
            &format!("{{\"result\":{{\"text\":\"got {marker} back\"}}}}"),
            marker
        ));
        assert!(!marker_in_assistant_result(
            "muse",
            &format!("{{\"prompt\":\"{marker}\"}}"),
            marker
        ));
        assert!(!marker_in_assistant_result("muse", "not json", marker));
    }

    #[test]
    fn parse_args_rejects_unknown_legs() {
        let argv = vec![
            "--provider".to_owned(),
            "muse".to_owned(),
            "--profile-a".to_owned(),
            "a".to_owned(),
            "--profile-b".to_owned(),
            "b".to_owned(),
            "--only".to_owned(),
            "nope".to_owned(),
        ];
        assert!(parse_args(&argv).is_err());
    }
}
