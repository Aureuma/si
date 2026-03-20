use std::env;
use std::fs::{self, File};
use std::io::{self, BufRead, BufReader, Read, Write};
use std::path::PathBuf;
use std::process::{Command, ExitCode, Stdio};
use std::sync::mpsc::{self, RecvTimeoutError};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};

use chrono::Utc;
use regex::Regex;
use serde::Serialize;

const USAGE: &str = r#"Usage: codex-stdout-parser [flags]

Common flags:
  -prompt-regex <regex>
  -ready-regex <regex>
  -ignore-regex <regex>
  -end-regex <regex>
  -mode <block|last-line>
  -command <cmd>
  -prompt <text>            repeatable
  -prompt-file <path>
  -session-log <path>
  -raw-log <path>
"#;

#[derive(Clone)]
struct Config {
    prompt_regex: String,
    ready_regex: String,
    ignore_regex: String,
    end_regex: String,
    mode: Mode,
    strip_ansi: bool,
    flush_on_eof: bool,
    command: Option<String>,
    prompts: Vec<String>,
    prompt_file: Option<PathBuf>,
    send_exit: bool,
    prompt_delay: Duration,
    type_delay: Duration,
    bracketed_paste: bool,
    session_log: Option<PathBuf>,
    session_log_wait: Duration,
    raw_log: Option<PathBuf>,
    start_delay: Duration,
    idle_timeout: Duration,
    turn_timeout: Duration,
    submit_seq: String,
    term: String,
    lang: String,
    wait_ready: bool,
    strip_end: bool,
    eof_ready: bool,
    max_turns: usize,
    exit_grace: Duration,
    source: Option<String>,
}

#[derive(Clone, Copy)]
enum Mode {
    Block,
    LastLine,
}

#[derive(Serialize)]
struct TurnEvent {
    turn: usize,
    captured_at: String,
    status: String,
    ready_for_prompt: bool,
    final_report: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    source: Option<String>,
}

struct Parser {
    prompt_re: Regex,
    ready_re: Regex,
    ignore_re: Regex,
    end_re: Regex,
    ansi_re: Regex,
    mode: Mode,
    strip_ansi: bool,
    flush_on_eof: bool,
    eof_ready: bool,
    strip_end: bool,
    source: Option<String>,
    lines: Vec<String>,
    turns: usize,
}

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(message) => {
            eprintln!("codex-stdout-parser: {message}");
            ExitCode::from(1)
        }
    }
}

fn run() -> Result<(), String> {
    let args: Vec<String> = env::args().skip(1).collect();
    if args.iter().any(|arg| arg == "--help" || arg == "-h") {
        println!("{USAGE}");
        return Ok(());
    }
    let config = parse_args(args)?;
    let mut parser = Parser::new(&config)?;
    if let Some(path) = &config.session_log {
        return run_session_log(path, &config, &mut parser);
    }
    if config.command.is_some() {
        return run_command_mode(&config, &mut parser);
    }
    run_reader(io::stdin().lock(), &config, &mut parser)
}

impl Parser {
    fn new(config: &Config) -> Result<Self, String> {
        Ok(Self {
            prompt_re: Regex::new(&config.prompt_regex).map_err(|err| err.to_string())?,
            ready_re: Regex::new(&config.ready_regex).map_err(|err| err.to_string())?,
            ignore_re: Regex::new(&config.ignore_regex).map_err(|err| err.to_string())?,
            end_re: Regex::new(&config.end_regex).map_err(|err| err.to_string())?,
            ansi_re: Regex::new(r"\x1B\[[0-?]*[ -/]*[@-~]").map_err(|err| err.to_string())?,
            mode: config.mode,
            strip_ansi: config.strip_ansi,
            flush_on_eof: config.flush_on_eof,
            eof_ready: config.eof_ready,
            strip_end: config.strip_end,
            source: config.source.clone(),
            lines: Vec::new(),
            turns: 0,
        })
    }

    fn consume_line(&mut self, line: &str) -> Result<(), String> {
        let mut normalized = line.trim_end_matches(['\r', '\n']).to_string();
        if self.strip_ansi {
            normalized = self.ansi_re.replace_all(&normalized, "").into_owned();
        }
        if self.ignore_re.is_match(&normalized) {
            return Ok(());
        }
        if self.prompt_re.is_match(&normalized) {
            self.emit("turn_complete", true)?;
            return Ok(());
        }
        if self.end_re.is_match(&normalized) {
            if !self.strip_end && !normalized.is_empty() {
                self.lines.push(normalized);
            }
            self.emit("turn_complete_end", true)?;
            return Ok(());
        }
        if !normalized.is_empty() {
            self.lines.push(normalized.clone());
        } else if matches!(self.mode, Mode::Block) && !self.lines.is_empty() {
            self.lines.push(String::new());
        }
        if self.ready_re.is_match(&normalized) {
            self.emit("turn_complete_ready", true)?;
        }
        Ok(())
    }

    fn emit(&mut self, status: &str, ready: bool) -> Result<(), String> {
        let final_report = self.final_report();
        if final_report.is_empty() && status != "eof" {
            self.lines.clear();
            return Ok(());
        }
        self.turns += 1;
        let event = TurnEvent {
            turn: self.turns,
            captured_at: Utc::now().to_rfc3339(),
            status: status.to_string(),
            ready_for_prompt: ready,
            final_report,
            source: self.source.clone(),
        };
        let encoded = serde_json::to_string(&event).map_err(|err| err.to_string())?;
        println!("{encoded}");
        io::stdout().flush().map_err(|err| err.to_string())?;
        self.lines.clear();
        Ok(())
    }

    fn final_report(&self) -> String {
        match self.mode {
            Mode::LastLine => self
                .lines
                .iter()
                .rev()
                .find(|line| !line.trim().is_empty())
                .cloned()
                .unwrap_or_default(),
            Mode::Block => {
                let mut block = Vec::new();
                for line in self.lines.iter().rev() {
                    if line.trim().is_empty() && !block.is_empty() {
                        break;
                    }
                    if !line.trim().is_empty() || !block.is_empty() {
                        block.push(line.clone());
                    }
                }
                block.reverse();
                block.join("\n").trim().to_string()
            }
        }
    }

    fn finish(&mut self) -> Result<(), String> {
        if self.flush_on_eof {
            if self.eof_ready {
                self.emit("turn_complete_exit", true)?;
            } else {
                self.emit("eof", false)?;
            }
        }
        Ok(())
    }
}

fn run_reader<R: Read>(reader: R, config: &Config, parser: &mut Parser) -> Result<(), String> {
    let raw_log = open_raw_log(config.raw_log.clone())?;
    let mut reader = BufReader::new(reader);
    let mut line = String::new();
    loop {
        line.clear();
        let read = reader.read_line(&mut line).map_err(|err| err.to_string())?;
        if read == 0 {
            break;
        }
        if let Some(file) = raw_log.as_ref() {
            file.lock()
                .map_err(|_| "raw log lock poisoned".to_string())?
                .write_all(line.as_bytes())
                .map_err(|err| err.to_string())?;
        }
        parser.consume_line(&line)?;
    }
    parser.finish()
}

fn run_session_log(path: &PathBuf, config: &Config, parser: &mut Parser) -> Result<(), String> {
    let deadline = Instant::now() + config.session_log_wait;
    while !path.exists() && Instant::now() < deadline {
        thread::sleep(Duration::from_millis(100));
    }
    let file = File::open(path).map_err(|err| format!("open {}: {err}", path.display()))?;
    run_reader(file, config, parser)
}

fn run_command_mode(config: &Config, parser: &mut Parser) -> Result<(), String> {
    let command = config.command.as_ref().ok_or("missing command")?;
    let mut child = Command::new("bash");
    child.arg("-lc").arg(command);
    child.stdin(Stdio::piped());
    child.stdout(Stdio::piped());
    child.stderr(Stdio::piped());
    child.env("TERM", &config.term);
    child.env("LANG", &config.lang);
    child.env("LC_ALL", &config.lang);
    let mut child = child.spawn().map_err(|err| format!("spawn command: {err}"))?;

    let raw_log = open_raw_log(config.raw_log.clone())?;
    let (tx, rx) = mpsc::channel::<Option<String>>();

    if let Some(stdout) = child.stdout.take() {
        let tx = tx.clone();
        let raw = raw_log.clone();
        thread::spawn(move || {
            pump_reader(stdout, raw, tx);
        });
    }
    if let Some(stderr) = child.stderr.take() {
        let tx = tx.clone();
        let raw = raw_log.clone();
        thread::spawn(move || {
            pump_reader(stderr, raw, tx);
        });
    }
    drop(tx);

    let prompts = load_prompts(config)?;
    if let Some(mut stdin) = child.stdin.take() {
        let submit_seq = decode_escapes(&config.submit_seq);
        let config = config.clone();
        thread::spawn(move || {
            let _ = config.wait_ready;
            thread::sleep(config.start_delay);
            for prompt in prompts {
                thread::sleep(config.prompt_delay);
                let payload = if config.bracketed_paste {
                    format!("\u{1b}[200~{prompt}\u{1b}[201~")
                } else {
                    prompt
                };
                if config.type_delay.is_zero() {
                    let _ = stdin.write_all(payload.as_bytes());
                } else {
                    for ch in payload.chars() {
                        let _ = stdin.write_all(ch.to_string().as_bytes());
                        thread::sleep(config.type_delay);
                    }
                }
                let _ = stdin.write_all(submit_seq.as_bytes());
                let _ = stdin.flush();
            }
            if config.send_exit {
                let _ = stdin.write_all(b"exit");
                let _ = stdin.write_all(submit_seq.as_bytes());
                let _ = stdin.flush();
            }
        });
    }

    let mut last_activity = Instant::now();
    let start = Instant::now();
    let mut stream_closed = 0usize;
    loop {
        match rx.recv_timeout(Duration::from_millis(100)) {
            Ok(Some(line)) => {
                last_activity = Instant::now();
                parser.consume_line(&line)?;
                if config.max_turns > 0 && parser.turns >= config.max_turns {
                    let _ = child.kill();
                    thread::sleep(config.exit_grace);
                    break;
                }
            }
            Ok(None) => {
                stream_closed += 1;
                if stream_closed >= 2 {
                    break;
                }
            }
            Err(RecvTimeoutError::Timeout) => {
                if !parser.lines.is_empty() && last_activity.elapsed() >= config.idle_timeout {
                    parser.emit("turn_complete_idle", true)?;
                    last_activity = Instant::now();
                }
                if start.elapsed() >= config.turn_timeout && !parser.lines.is_empty() {
                    parser.emit("turn_complete_exit", true)?;
                    let _ = child.kill();
                    break;
                }
                if let Ok(Some(_)) = child.try_wait() {
                    continue;
                }
            }
            Err(RecvTimeoutError::Disconnected) => break,
        }
    }
    let _ = child.wait();
    parser.finish()
}

fn pump_reader<R: Read>(
    reader: R,
    raw_log: Option<Arc<Mutex<File>>>,
    tx: mpsc::Sender<Option<String>>,
) {
    let mut reader = BufReader::new(reader);
    let mut line = String::new();
    loop {
        line.clear();
        match reader.read_line(&mut line) {
            Ok(0) => {
                let _ = tx.send(None);
                break;
            }
            Ok(_) => {
                if let Some(file) = raw_log.as_ref()
                    && let Ok(mut file) = file.lock()
                {
                    let _ = file.write_all(line.as_bytes());
                }
                let _ = tx.send(Some(line.clone()));
            }
            Err(_) => {
                let _ = tx.send(None);
                break;
            }
        }
    }
}

fn load_prompts(config: &Config) -> Result<Vec<String>, String> {
    let mut prompts = config.prompts.clone();
    if let Some(path) = &config.prompt_file {
        let content = fs::read_to_string(path)
            .map_err(|err| format!("read prompt file {}: {err}", path.display()))?;
        for line in content.lines() {
            let trimmed = line.trim();
            if !trimmed.is_empty() {
                prompts.push(trimmed.to_string());
            }
        }
    }
    Ok(prompts)
}

fn open_raw_log(path: Option<PathBuf>) -> Result<Option<Arc<Mutex<File>>>, String> {
    match path {
        Some(path) => {
            let file = File::create(&path)
                .map_err(|err| format!("create raw log {}: {err}", path.display()))?;
            Ok(Some(Arc::new(Mutex::new(file))))
        }
        None => Ok(None),
    }
}

fn decode_escapes(value: &str) -> String {
    let mut out = String::new();
    let mut chars = value.chars().peekable();
    while let Some(ch) = chars.next() {
        if ch != '\\' {
            out.push(ch);
            continue;
        }
        match chars.next() {
            Some('r') => out.push('\r'),
            Some('n') => out.push('\n'),
            Some('t') => out.push('\t'),
            Some('x') => {
                let hi = chars.next();
                let lo = chars.next();
                if let (Some(hi), Some(lo)) = (hi, lo) {
                    let hex = format!("{hi}{lo}");
                    if let Ok(value) = u8::from_str_radix(&hex, 16) {
                        out.push(value as char);
                    }
                }
            }
            Some(other) => out.push(other),
            None => break,
        }
    }
    out
}

fn parse_args(args: Vec<String>) -> Result<Config, String> {
    let mut config = Config {
        prompt_regex: r"^(>\s*|codex>\s*|you>\s*|user>\s*)$".to_string(),
        ready_regex: r"(?i)(context left|openai codex|>_)".to_string(),
        ignore_regex: r"^(\s*[│╭╰╮╯╞╡╤╧╪─]+.*|\s*>_.*|\s*OpenAI Codex.*|\s*model:.*|\s*directory:.*|\s*Tip:.*|\s*›.*|\s*↳.*|\s*•\s*(Working|Preparing).*|\s*\d+%\s+context\s+left.*)$".to_string(),
        end_regex: "^DONE$".to_string(),
        mode: Mode::Block,
        strip_ansi: true,
        flush_on_eof: true,
        command: None,
        prompts: Vec::new(),
        prompt_file: None,
        send_exit: true,
        prompt_delay: Duration::from_millis(200),
        type_delay: Duration::ZERO,
        bracketed_paste: false,
        session_log: None,
        session_log_wait: Duration::from_secs(2),
        raw_log: None,
        start_delay: Duration::from_millis(800),
        idle_timeout: Duration::from_secs(2),
        turn_timeout: Duration::from_secs(120),
        submit_seq: r"\r".to_string(),
        term: "xterm-256color".to_string(),
        lang: "en_US.UTF-8".to_string(),
        wait_ready: true,
        strip_end: true,
        eof_ready: false,
        max_turns: 0,
        exit_grace: Duration::from_secs(2),
        source: None,
    };

    let mut idx = 0;
    while idx < args.len() {
        let raw = &args[idx];
        let (name, inline_value) = split_arg(raw);
        let key = name.trim_start_matches('-');
        match key {
            "prompt-regex" => config.prompt_regex = take_value(&args, &mut idx, inline_value, key)?,
            "ready-regex" => config.ready_regex = take_value(&args, &mut idx, inline_value, key)?,
            "ignore-regex" => config.ignore_regex = take_value(&args, &mut idx, inline_value, key)?,
            "end-regex" => config.end_regex = take_value(&args, &mut idx, inline_value, key)?,
            "mode" => {
                let value = take_value(&args, &mut idx, inline_value, key)?;
                config.mode = match value.as_str() {
                    "block" => Mode::Block,
                    "last-line" => Mode::LastLine,
                    other => return Err(format!("invalid mode: {other}")),
                };
            }
            "strip-ansi" => config.strip_ansi = take_bool(&args, &mut idx, inline_value, key)?,
            "flush-on-eof" => config.flush_on_eof = take_bool(&args, &mut idx, inline_value, key)?,
            "command" => config.command = Some(take_value(&args, &mut idx, inline_value, key)?),
            "prompt" => config.prompts.push(take_value(&args, &mut idx, inline_value, key)?),
            "prompt-file" => {
                config.prompt_file =
                    Some(PathBuf::from(take_value(&args, &mut idx, inline_value, key)?))
            }
            "send-exit" => config.send_exit = take_bool(&args, &mut idx, inline_value, key)?,
            "prompt-delay" => {
                config.prompt_delay =
                    parse_duration(&take_value(&args, &mut idx, inline_value, key)?)?
            }
            "type-delay" => {
                config.type_delay =
                    parse_duration(&take_value(&args, &mut idx, inline_value, key)?)?
            }
            "bracketed-paste" => {
                config.bracketed_paste = take_bool(&args, &mut idx, inline_value, key)?
            }
            "session-log" => {
                config.session_log =
                    Some(PathBuf::from(take_value(&args, &mut idx, inline_value, key)?))
            }
            "session-log-wait" => {
                config.session_log_wait =
                    parse_duration(&take_value(&args, &mut idx, inline_value, key)?)?
            }
            "raw-log" => {
                config.raw_log =
                    Some(PathBuf::from(take_value(&args, &mut idx, inline_value, key)?))
            }
            "start-delay" => {
                config.start_delay =
                    parse_duration(&take_value(&args, &mut idx, inline_value, key)?)?
            }
            "idle-timeout" => {
                config.idle_timeout =
                    parse_duration(&take_value(&args, &mut idx, inline_value, key)?)?
            }
            "turn-timeout" => {
                config.turn_timeout =
                    parse_duration(&take_value(&args, &mut idx, inline_value, key)?)?
            }
            "submit-seq" => config.submit_seq = take_value(&args, &mut idx, inline_value, key)?,
            "term" => config.term = take_value(&args, &mut idx, inline_value, key)?,
            "lang" => config.lang = take_value(&args, &mut idx, inline_value, key)?,
            "wait-ready" => config.wait_ready = take_bool(&args, &mut idx, inline_value, key)?,
            "strip-end" => config.strip_end = take_bool(&args, &mut idx, inline_value, key)?,
            "eof-ready" => config.eof_ready = take_bool(&args, &mut idx, inline_value, key)?,
            "max-turns" => {
                config.max_turns = take_value(&args, &mut idx, inline_value, key)?
                    .parse::<usize>()
                    .map_err(|err| format!("invalid max-turns: {err}"))?
            }
            "exit-grace" => {
                config.exit_grace =
                    parse_duration(&take_value(&args, &mut idx, inline_value, key)?)?
            }
            "source" => config.source = Some(take_value(&args, &mut idx, inline_value, key)?),
            other => return Err(format!("unknown arg: {other}\n{USAGE}")),
        }
        idx += 1;
    }
    Ok(config)
}

fn split_arg(raw: &str) -> (&str, Option<String>) {
    if let Some((name, value)) = raw.split_once('=') {
        (name, Some(value.to_string()))
    } else {
        (raw, None)
    }
}

fn take_value(
    args: &[String],
    idx: &mut usize,
    inline_value: Option<String>,
    key: &str,
) -> Result<String, String> {
    if let Some(value) = inline_value {
        return Ok(value);
    }
    *idx += 1;
    args.get(*idx).cloned().ok_or_else(|| format!("missing value for -{key}"))
}

fn take_bool(
    args: &[String],
    idx: &mut usize,
    inline_value: Option<String>,
    key: &str,
) -> Result<bool, String> {
    match inline_value {
        Some(value) => parse_bool(&value),
        None => {
            if let Some(next) = args.get(*idx + 1)
                && !next.starts_with('-')
            {
                *idx += 1;
                return parse_bool(next);
            }
            let _ = key;
            Ok(true)
        }
    }
}

fn parse_bool(value: &str) -> Result<bool, String> {
    match value {
        "true" | "1" | "yes" => Ok(true),
        "false" | "0" | "no" => Ok(false),
        other => Err(format!("invalid bool: {other}")),
    }
}

fn parse_duration(value: &str) -> Result<Duration, String> {
    if let Some(raw) = value.strip_suffix("ms") {
        let millis = raw.parse::<u64>().map_err(|err| err.to_string())?;
        return Ok(Duration::from_millis(millis));
    }
    if let Some(raw) = value.strip_suffix('s') {
        let secs = raw.parse::<u64>().map_err(|err| err.to_string())?;
        return Ok(Duration::from_secs(secs));
    }
    if let Some(raw) = value.strip_suffix('m') {
        let mins = raw.parse::<u64>().map_err(|err| err.to_string())?;
        return Ok(Duration::from_secs(mins * 60));
    }
    Err(format!("invalid duration: {value}"))
}
