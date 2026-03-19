use std::env;
use std::io::{self, BufRead, Write};
use std::process::ExitCode;
use std::thread;
use std::time::Duration;

struct Config {
    prompt_char: String,
    delay: Duration,
    long_lines: usize,
    long_if_contains: String,
    no_markers: bool,
}

fn main() -> ExitCode {
    let mut config = match load_config() {
        Ok(config) => config,
        Err(err) => {
            eprintln!("{err}");
            return ExitCode::from(1);
        }
    };

    let stdin = io::stdin();
    let mut stdout = io::stdout().lock();
    let mut turn = 0usize;

    println!("fake-codex ready");
    print_prompt(&mut stdout, &config.prompt_char);

    for line in stdin.lock().lines() {
        let line = match line {
            Ok(line) => line,
            Err(_) => return ExitCode::SUCCESS,
        };
        turn += 1;

        if config.delay > Duration::ZERO {
            thread::sleep(config.delay);
        }

        if let Some(should_exit) = handle_special(&mut stdout, &line, &config.prompt_char) {
            if should_exit {
                return ExitCode::SUCCESS;
            }
            continue;
        }

        let emit_long = if config.long_lines > 0 {
            true
        } else if !config.long_if_contains.is_empty() && line.contains(&config.long_if_contains) {
            config.long_lines = 12000;
            true
        } else {
            false
        };

        if emit_long {
            for i in 1..=config.long_lines {
                println!("line {i}");
            }
        } else {
            println!("ok");
        }

        let member = env::var("DYAD_MEMBER").unwrap_or_else(|_| "unknown".to_string());
        let sig = truncate_chars(&line, 60);
        if config.no_markers {
            emit_body(&member, turn, &sig);
        } else {
            println!("<<WORK_REPORT_BEGIN>>");
            emit_body(&member, turn, &sig);
            println!("<<WORK_REPORT_END>>");
        }
        print_prompt(&mut stdout, &config.prompt_char);
    }

    ExitCode::SUCCESS
}

fn load_config() -> Result<Config, String> {
    let prompt_char = env::var("FAKE_CODEX_PROMPT_CHAR").unwrap_or_else(|_| "›".to_string());
    let delay_seconds = env::var("FAKE_CODEX_DELAY_SECONDS").unwrap_or_else(|_| "0".to_string());
    let delay_value = delay_seconds
        .parse::<f64>()
        .map_err(|err| format!("invalid FAKE_CODEX_DELAY_SECONDS: {err}"))?;

    let long_lines = match env::var("FAKE_CODEX_LONG_LINES") {
        Ok(raw) if !raw.is_empty() => raw
            .parse::<usize>()
            .map_err(|err| format!("invalid FAKE_CODEX_LONG_LINES: {err}"))?,
        _ => 0,
    };

    let no_markers = match env::var("FAKE_CODEX_NO_MARKERS") {
        Ok(raw) => raw != "0",
        Err(_) => false,
    };

    Ok(Config {
        prompt_char,
        delay: Duration::from_secs_f64(delay_value),
        long_lines,
        long_if_contains: env::var("FAKE_CODEX_LONG_IF_CONTAINS").unwrap_or_default(),
        no_markers,
    })
}

fn print_prompt(stdout: &mut impl Write, prompt_char: &str) {
    let _ = write!(stdout, "{prompt_char} ");
    let _ = stdout.flush();
}

fn handle_special(stdout: &mut impl Write, line: &str, prompt_char: &str) -> Option<bool> {
    let response = match line {
        "/status" => Some("status: ok"),
        _ if line.starts_with("/model") => Some("model: gpt-5.2-codex"),
        _ if line.starts_with("/approval") => Some("approval: auto"),
        _ if line.starts_with("/sandbox") => Some("sandbox: workspace-write"),
        "/agents" => Some("agents: actor,critic"),
        "/prompts" => Some("prompts: available"),
        "/review" => Some("review: started"),
        "/compact" => Some("compact: done"),
        "/clear" => Some("clear: done"),
        "/help" => Some("help: commands listed"),
        "/logout" => Some("logout: skipped"),
        "/vim" => Some("vim: enabled"),
        "/" => Some("menu: /status /model /approval"),
        "1" => Some("menu-select: /status"),
        "2" => Some("menu-select: /model"),
        _ if line.starts_with("\u{1b}[A") || line.starts_with("^[[A") => Some("menu-select: /status"),
        _ if line.starts_with("\u{1b}[B") || line.starts_with("^[[B") => Some("menu-select: /model"),
        "/exit" => {
            println!("bye");
            return Some(true);
        }
        _ => None,
    };

    if let Some(response) = response {
        println!("{response}");
        print_prompt(stdout, prompt_char);
        Some(false)
    } else {
        None
    }
}

fn emit_body(member: &str, turn: usize, sig: &str) {
    if member == "critic" {
        println!("Assessment:");
        println!("- member: {member}");
        println!("- turn: {turn}");
        println!("- input_sig: {sig}");
        println!("Risks:");
        println!("- none");
        println!("Required Fixes:");
        println!("- none");
        println!("Verification Steps:");
        println!("- none");
        println!("Next Actor Prompt:");
        println!("- proceed");
        println!("Continue Loop: yes");
    } else {
        println!("Summary:");
        println!("- member: {member}");
        println!("- turn: {turn}");
        println!("- input_sig: {sig}");
        println!("Changes:");
        println!("- none");
        println!("Validation:");
        println!("- none");
        println!("Open Questions:");
        println!("- none");
        println!("Next Step for Critic:");
        println!("- proceed");
    }
}

fn truncate_chars(input: &str, max: usize) -> String {
    let chars: Vec<char> = input.chars().collect();
    if chars.len() <= max {
        input.to_string()
    } else {
        chars.into_iter().take(max).collect()
    }
}
