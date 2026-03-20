use std::env;
use std::fs::File;
use std::io::{BufRead, BufReader, Read, Write};
use std::process::{Child, ChildStdin, Command, ExitCode, Stdio};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};

use regex::Regex;

const USAGE: &str = r#"usage: codex-interactive-driver -command <cmd> [flags]

flags:
  -script <path>
  -step <action>            repeatable
  -prompt-regex <regex>
  -wait <duration>
  -final-wait <duration>
  -max-bytes <n>
  -print-output
  -no-initial-wait
"#;

#[derive(Clone)]
struct Config {
    command: String,
    script_path: Option<String>,
    steps: Vec<String>,
    prompt_regex: String,
    default_wait: Duration,
    final_wait: Duration,
    max_bytes: usize,
    print_output: bool,
    no_initial_wait: bool,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum ActionKind {
    WaitPrompt,
    Send,
    Type,
    Key,
    Sleep,
    WaitContains,
}

struct Action {
    kind: ActionKind,
    arg: String,
    timeout: Duration,
}

struct Runner {
    child: Child,
    stdin: ChildStdin,
    output: Arc<Mutex<Vec<u8>>>,
    max_bytes: usize,
    prompt: Regex,
    poll: Duration,
    default_wait: Duration,
}

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(message) => {
            eprintln!("{message}");
            ExitCode::from(1)
        }
    }
}

fn run() -> Result<(), String> {
    let args: Vec<String> = env::args().skip(1).collect();
    if args.iter().any(|arg| arg == "-h" || arg == "--help") {
        println!("{USAGE}");
        return Ok(());
    }

    let config = parse_args(args)?;
    let prompt =
        Regex::new(&config.prompt_regex).map_err(|err| format!("invalid prompt regex: {err}"))?;

    let mut raw = Vec::new();
    if !config.no_initial_wait {
        raw.push("wait_prompt".to_string());
    }
    if let Some(path) = &config.script_path {
        raw.extend(read_script(path)?);
    }
    raw.extend(config.steps.clone());
    let actions = normalize_actions(&raw, config.default_wait)?;

    let mut runner = Runner::new(&config.command, prompt, config.max_bytes, config.default_wait)?;

    let result = (|| {
        run_plan(&mut runner, &actions)?;
        runner.wait_exit(config.final_wait).map_err(|err| format!("wait exit: {err}"))?;
        if config.print_output {
            print!("{}", runner.output_string());
        }
        Ok(())
    })();

    let _ = runner.close();
    if result.is_err() && config.print_output {
        print!("{}", runner.output_string());
    }
    result
}

impl Runner {
    fn new(
        command: &str,
        prompt: Regex,
        max_bytes: usize,
        default_wait: Duration,
    ) -> Result<Self, String> {
        if command.trim().is_empty() {
            return Err("-command is required".to_string());
        }
        let max_bytes = if max_bytes == 0 { 1 << 20 } else { max_bytes };
        let default_wait =
            if default_wait.is_zero() { Duration::from_secs(20) } else { default_wait };

        let mut child = Command::new("script");
        child
            .arg("-qefc")
            .arg(command)
            .arg("/dev/null")
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        let mut child = child.spawn().map_err(|err| format!("start: {err}"))?;
        let stdin = child.stdin.take().ok_or("start: missing stdin pipe")?;
        let stdout = child.stdout.take().ok_or("start: missing stdout pipe")?;
        let stderr = child.stderr.take().ok_or("start: missing stderr pipe")?;
        let output = Arc::new(Mutex::new(Vec::new()));
        spawn_reader(stdout, output.clone(), max_bytes);
        spawn_reader(stderr, output.clone(), max_bytes);
        Ok(Self {
            child,
            stdin,
            output,
            max_bytes,
            prompt,
            poll: Duration::from_millis(50),
            default_wait,
        })
    }

    fn append_output(output: &Arc<Mutex<Vec<u8>>>, chunk: &[u8], max_bytes: usize) {
        let Ok(mut buffer) = output.lock() else {
            return;
        };
        if chunk.len() >= max_bytes {
            *buffer = chunk[chunk.len() - max_bytes..].to_vec();
            return;
        }
        let need = buffer.len().saturating_add(chunk.len()).saturating_sub(max_bytes);
        if need > 0 {
            buffer.drain(0..need);
        }
        buffer.extend_from_slice(chunk);
    }

    fn output_string(&self) -> String {
        match self.output.lock() {
            Ok(buffer) => String::from_utf8_lossy(&buffer).into_owned(),
            Err(_) => String::new(),
        }
    }

    fn tail(&self, lines: usize) -> String {
        let limit = if lines == 0 { 80 } else { lines };
        let text = self.output_string().replace('\r', "");
        let parts: Vec<&str> = text.split('\n').collect();
        if parts.len() <= limit {
            return parts.join("\n");
        }
        parts[parts.len() - limit..].join("\n")
    }

    fn send(&mut self, text: &str) -> Result<(), String> {
        if text.trim().is_empty() {
            return Ok(());
        }
        self.stdin.write_all(text.as_bytes()).map_err(|err| err.to_string())
    }

    fn send_line(&mut self, line: &str) -> Result<(), String> {
        self.stdin
            .write_all(line.as_bytes())
            .and_then(|_| self.stdin.write_all(b"\n"))
            .and_then(|_| self.stdin.flush())
            .map_err(|err| err.to_string())
    }

    fn wait_prompt(&mut self, timeout: Duration) -> Result<(), String> {
        let timeout = if timeout.is_zero() { self.default_wait } else { timeout };
        let deadline = Instant::now() + timeout;
        loop {
            if self.prompt.is_match(&last_line(&strip_ansi(&self.output_string()))) {
                return Ok(());
            }
            if let Some(status) = self.child.try_wait().map_err(|err| err.to_string())? {
                return Err(format!("process exited while waiting for prompt: {status}"));
            }
            if Instant::now() >= deadline {
                return Err("timeout waiting for prompt".to_string());
            }
            thread::sleep(self.poll);
        }
    }

    fn wait_contains(&mut self, needle: &str, timeout: Duration) -> Result<(), String> {
        let needle = needle.trim();
        if needle.is_empty() {
            return Ok(());
        }
        let timeout = if timeout.is_zero() { self.default_wait } else { timeout };
        let deadline = Instant::now() + timeout;
        loop {
            if strip_ansi(&self.output_string()).contains(needle) {
                return Ok(());
            }
            if let Some(status) = self.child.try_wait().map_err(|err| err.to_string())? {
                return Err(format!("process exited while waiting for {needle:?}: {status}"));
            }
            if Instant::now() >= deadline {
                return Err(format!("timeout waiting for {needle:?}"));
            }
            thread::sleep(self.poll);
        }
    }

    fn wait_exit(&mut self, timeout: Duration) -> Result<(), String> {
        let timeout = if timeout.is_zero() { Duration::from_secs(2) } else { timeout };
        let deadline = Instant::now() + timeout;
        loop {
            if let Some(status) = self.child.try_wait().map_err(|err| err.to_string())? {
                return if status.success() {
                    Ok(())
                } else {
                    Err(format!("exit status {status}"))
                };
            }
            if Instant::now() >= deadline {
                return Err("timeout waiting for process exit".to_string());
            }
            thread::sleep(self.poll);
        }
    }

    fn close(&mut self) -> Result<(), String> {
        if self.child.try_wait().map_err(|err| err.to_string())?.is_none() {
            let _ = self.child.kill();
            let _ = self.child.wait();
        }
        let _ = self.max_bytes;
        Ok(())
    }
}

fn spawn_reader<R>(mut reader: R, output: Arc<Mutex<Vec<u8>>>, max_bytes: usize)
where
    R: Read + Send + 'static,
{
    thread::spawn(move || {
        let mut buffer = [0u8; 4096];
        loop {
            match reader.read(&mut buffer) {
                Ok(0) => break,
                Ok(n) => Runner::append_output(&output, &buffer[..n], max_bytes),
                Err(_) => break,
            }
        }
    });
}

fn parse_args(args: Vec<String>) -> Result<Config, String> {
    let mut config = Config {
        command: String::new(),
        script_path: None,
        steps: Vec::new(),
        prompt_regex: r"^›\s*$".to_string(),
        default_wait: Duration::from_secs(20),
        final_wait: Duration::from_secs(2),
        max_bytes: 1 << 20,
        print_output: false,
        no_initial_wait: false,
    };

    let mut idx = 0;
    while idx < args.len() {
        let (name, inline) = split_arg(&args[idx]);
        let key = name.trim_start_matches('-');
        match key {
            "command" => config.command = take_value(&args, &mut idx, inline, key)?,
            "script" => config.script_path = Some(take_value(&args, &mut idx, inline, key)?),
            "step" => config.steps.push(take_value(&args, &mut idx, inline, key)?),
            "prompt-regex" => config.prompt_regex = take_value(&args, &mut idx, inline, key)?,
            "wait" => {
                config.default_wait = parse_duration(&take_value(&args, &mut idx, inline, key)?)?
            }
            "final-wait" => {
                config.final_wait = parse_duration(&take_value(&args, &mut idx, inline, key)?)?
            }
            "max-bytes" => {
                config.max_bytes = take_value(&args, &mut idx, inline, key)?
                    .parse::<usize>()
                    .map_err(|err| err.to_string())?
            }
            "print-output" => config.print_output = take_bool(&args, &mut idx, inline)?,
            "no-initial-wait" => config.no_initial_wait = take_bool(&args, &mut idx, inline)?,
            other => return Err(format!("unknown arg: {other}\n{USAGE}")),
        }
        idx += 1;
    }

    if config.command.trim().is_empty() {
        return Err("-command is required".to_string());
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
    inline: Option<String>,
    key: &str,
) -> Result<String, String> {
    if let Some(value) = inline {
        return Ok(value);
    }
    *idx += 1;
    args.get(*idx).cloned().ok_or_else(|| format!("missing value for -{key}"))
}

fn take_bool(args: &[String], idx: &mut usize, inline: Option<String>) -> Result<bool, String> {
    if let Some(value) = inline {
        return parse_bool(&value);
    }
    if let Some(next) = args.get(*idx + 1)
        && !next.starts_with('-')
    {
        *idx += 1;
        return parse_bool(next);
    }
    Ok(true)
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
        return raw.parse::<u64>().map(Duration::from_millis).map_err(|err| err.to_string());
    }
    if let Some(raw) = value.strip_suffix('s') {
        return raw.parse::<u64>().map(Duration::from_secs).map_err(|err| err.to_string());
    }
    if let Some(raw) = value.strip_suffix('m') {
        return raw
            .parse::<u64>()
            .map(|mins| Duration::from_secs(mins * 60))
            .map_err(|err| err.to_string());
    }
    Err(format!("invalid duration: {value}"))
}

fn read_script(path: &str) -> Result<Vec<String>, String> {
    let file = File::open(path).map_err(|err| format!("read script: {err}"))?;
    let reader = BufReader::new(file);
    let mut steps = Vec::new();
    for line in reader.lines() {
        let line = line.map_err(|err| err.to_string())?;
        let trimmed = line.trim();
        if trimmed.is_empty() || trimmed.starts_with('#') {
            continue;
        }
        steps.push(trimmed.to_string());
    }
    Ok(steps)
}

fn normalize_actions(raw: &[String], default_wait: Duration) -> Result<Vec<Action>, String> {
    raw.iter().map(|spec| parse_action(spec, default_wait)).collect()
}

fn parse_action(spec: &str, default_wait: Duration) -> Result<Action, String> {
    let spec = spec.trim();
    if spec.is_empty() {
        return Err("empty action".to_string());
    }
    if spec.eq_ignore_ascii_case("wait_prompt") {
        return Ok(Action {
            kind: ActionKind::WaitPrompt,
            arg: String::new(),
            timeout: default_wait,
        });
    }
    if let Some(value) =
        spec.strip_prefix("wait_prompt:").or_else(|| spec.strip_prefix("wait_prompt="))
    {
        return Ok(Action {
            kind: ActionKind::WaitPrompt,
            arg: String::new(),
            timeout: parse_duration_or(value, default_wait),
        });
    }

    if let Some(arg) = spec.strip_prefix("send:").or_else(|| spec.strip_prefix("send ")) {
        return Ok(Action {
            kind: ActionKind::Send,
            arg: arg.trim().to_string(),
            timeout: default_wait,
        });
    }
    if let Some(arg) = spec.strip_prefix("type:").or_else(|| spec.strip_prefix("type ")) {
        return Ok(Action {
            kind: ActionKind::Type,
            arg: arg.trim().to_string(),
            timeout: default_wait,
        });
    }
    if let Some(arg) = spec.strip_prefix("key:").or_else(|| spec.strip_prefix("key ")) {
        return Ok(Action {
            kind: ActionKind::Key,
            arg: arg.trim().to_string(),
            timeout: default_wait,
        });
    }
    if let Some(arg) = spec.strip_prefix("sleep:").or_else(|| spec.strip_prefix("sleep ")) {
        return Ok(Action {
            kind: ActionKind::Sleep,
            arg: String::new(),
            timeout: parse_duration_or(arg.trim(), default_wait),
        });
    }
    if let Some(arg) =
        spec.strip_prefix("wait_contains:").or_else(|| spec.strip_prefix("wait_contains "))
    {
        let (text, timeout) = match arg.split_once('|') {
            Some((text, timeout)) => {
                (text.trim().to_string(), parse_duration_or(timeout, default_wait))
            }
            None => (arg.trim().to_string(), default_wait),
        };
        return Ok(Action { kind: ActionKind::WaitContains, arg: text, timeout });
    }
    Err(format!("unsupported action {spec:?}"))
}

fn parse_duration_or(value: &str, fallback: Duration) -> Duration {
    parse_duration(value.trim()).unwrap_or(fallback)
}

fn run_plan(runner: &mut Runner, actions: &[Action]) -> Result<(), String> {
    for (index, action) in actions.iter().enumerate() {
        let step = index + 1;
        match action.kind {
            ActionKind::WaitPrompt => runner.wait_prompt(action.timeout).map_err(|err| {
                format!("step {step} wait_prompt: {err}\n--- tail ---\n{}", runner.tail(80))
            })?,
            ActionKind::Send => {
                runner.send_line(&action.arg).map_err(|err| format!("step {step} send: {err}"))?
            }
            ActionKind::Type => {
                runner.send(&action.arg).map_err(|err| format!("step {step} type: {err}"))?
            }
            ActionKind::Key => {
                let key = decode_key(&action.arg)?;
                runner.send(&key).map_err(|err| format!("step {step} key send: {err}"))?;
            }
            ActionKind::Sleep => thread::sleep(action.timeout),
            ActionKind::WaitContains => {
                runner.wait_contains(&action.arg, action.timeout).map_err(|err| {
                    format!("step {step} wait_contains: {err}\n--- tail ---\n{}", runner.tail(80))
                })?
            }
        }
    }
    Ok(())
}

fn decode_key(name: &str) -> Result<String, String> {
    match name.trim().to_ascii_lowercase().as_str() {
        "enter" | "return" => Ok("\r".to_string()),
        "tab" => Ok("\t".to_string()),
        "esc" | "escape" => Ok("\u{1b}".to_string()),
        "up" | "arrowup" => Ok("\u{1b}[A".to_string()),
        "down" | "arrowdown" => Ok("\u{1b}[B".to_string()),
        "left" | "arrowleft" => Ok("\u{1b}[D".to_string()),
        "right" | "arrowright" => Ok("\u{1b}[C".to_string()),
        "ctrl-c" => Ok("\u{3}".to_string()),
        _ => Err(format!("unsupported key {name:?}")),
    }
}

fn last_line(text: &str) -> String {
    text.replace('\r', "").lines().last().unwrap_or_default().trim().to_string()
}

fn strip_ansi(input: &str) -> String {
    let mut out = String::with_capacity(input.len());
    let bytes = input.as_bytes();
    let mut idx = 0;
    while idx < bytes.len() {
        if bytes[idx] == 0x1b && idx + 1 < bytes.len() {
            match bytes[idx + 1] {
                b'[' => {
                    idx += 2;
                    while idx < bytes.len() {
                        let ch = bytes[idx];
                        idx += 1;
                        if ch.is_ascii_alphabetic() {
                            break;
                        }
                    }
                    continue;
                }
                b']' => {
                    idx += 2;
                    while idx < bytes.len() {
                        if bytes[idx] == 0x07 {
                            idx += 1;
                            break;
                        }
                        if bytes[idx] == 0x1b && idx + 1 < bytes.len() && bytes[idx + 1] == b'\\' {
                            idx += 2;
                            break;
                        }
                        idx += 1;
                    }
                    continue;
                }
                _ => {}
            }
        }
        out.push(bytes[idx] as char);
        idx += 1;
    }
    out
}
