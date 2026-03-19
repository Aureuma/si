use std::env;
use std::os::unix::process::CommandExt;
use std::path::{Path, PathBuf};
use std::process::{Command, ExitCode};

use si_tools::{
    browser_mcp_url_from_env, collect_git_safe_directories, ensure_git_safe_directories,
    sync_codex_skills, write_codex_config,
};

const USAGE: &str = r#"Usage: si-codex-init [--quiet] [--exec <cmd> [args...]]

Initializes Codex config and skills inside the SI image, then optionally execs a command.
"#;

struct Config {
    quiet: bool,
    exec: Vec<String>,
}

fn main() -> ExitCode {
    match run() {
        Ok(code) => code,
        Err(message) => {
            eprintln!("si-codex-init: {message}");
            ExitCode::from(1)
        }
    }
}

fn run() -> Result<ExitCode, String> {
    let args: Vec<String> = env::args().skip(1).collect();
    if args.iter().any(|arg| arg == "--help" || arg == "-h") {
        println!("{USAGE}");
        return Ok(ExitCode::SUCCESS);
    }
    let config = parse_args(args)?;
    let home = env::var("HOME")
        .ok()
        .filter(|value| !value.trim().is_empty())
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("/home/si"));
    let codex_home = env::var("CODEX_HOME")
        .ok()
        .filter(|value| !value.trim().is_empty())
        .map(PathBuf::from)
        .unwrap_or_else(|| home.join(".codex"));
    let config_dir = env::var("CODEX_CONFIG_DIR")
        .ok()
        .filter(|value| !value.trim().is_empty())
        .map(PathBuf::from)
        .unwrap_or_else(|| codex_home.clone());
    let template_path = env::var("CODEX_CONFIG_TEMPLATE")
        .ok()
        .filter(|value| !value.trim().is_empty())
        .map(PathBuf::from);

    sync_codex_skills(Path::new("/opt/si/codex-skills"), &codex_home.join("skills"))
        .map_err(|err| format!("sync skills: {err}"))?;
    write_codex_config(&config_dir.join("config.toml"), template_path.as_deref())
        .map_err(|err| format!("write config: {err}"))?;

    let cwd = env::current_dir().ok();
    let safe_dirs = collect_git_safe_directories(cwd.as_deref())
        .map_err(|err| format!("collect git safe directories: {err}"))?;
    ensure_git_safe_directories(&home, &safe_dirs)
        .map_err(|err| format!("configure git safe directories: {err}"))?;

    if let Some(url) = browser_mcp_url_from_env() {
        let status = Command::new("codex")
            .arg("mcp")
            .arg("add")
            .arg("browser")
            .arg(url)
            .status();
        if !config.quiet {
            match status {
                Ok(result) if result.success() => {
                    eprintln!("si-codex-init: browser MCP configured");
                }
                Ok(_) => {
                    eprintln!("si-codex-init: browser MCP configuration skipped");
                }
                Err(err) => {
                    eprintln!("si-codex-init: browser MCP configuration failed: {err}");
                }
            }
        }
    }

    if config.exec.is_empty() {
        return Ok(ExitCode::SUCCESS);
    }

    let mut command = Command::new(&config.exec[0]);
    command.args(&config.exec[1..]);
    let err = command.exec();
    Err(format!("exec {}: {err}", config.exec[0]))
}

fn parse_args(args: Vec<String>) -> Result<Config, String> {
    let mut quiet = false;
    let mut exec = Vec::new();
    let mut idx = 0;
    while idx < args.len() {
        match args[idx].as_str() {
            "--quiet" => quiet = true,
            "--exec" => {
                exec = args[idx + 1..].to_vec();
                break;
            }
            other => return Err(format!("unknown arg: {other}\n{USAGE}")),
        }
        idx += 1;
    }
    Ok(Config { quiet, exec })
}
