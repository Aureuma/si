use std::env;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::process::{Command, ExitCode, Stdio};

use si_tools::{USAGE_TEXT, infer_target_env, list_env_files, parse_dotenv};

struct Config {
    src: PathBuf,
    section: String,
    identity_file: PathBuf,
    dry_run: bool,
}

fn main() -> ExitCode {
    match run(env::args().skip(1).collect()) {
        Ok(code) => code,
        Err((message, code)) => {
            if !message.is_empty() {
                eprintln!("{message}");
            }
            code
        }
    }
}

fn run(args: Vec<String>) -> Result<ExitCode, (String, ExitCode)> {
    let (config, show_help) = parse_args(&args)?;
    if show_help {
        println!("{USAGE_TEXT}");
        return Ok(ExitCode::SUCCESS);
    }

    if !config.src.exists() {
        return Err((
            format!("source directory not found: {}", config.src.display()),
            ExitCode::from(1),
        ));
    }
    if !has_si_on_path() {
        return Err(("si not found on PATH".to_string(), ExitCode::from(1)));
    }
    if !config.identity_file.exists() {
        return Err((
            format!(
                "vault identity file not found: {}\nhint: pass --identity-file",
                config.identity_file.display()
            ),
            ExitCode::from(1),
        ));
    }

    let env_files = list_env_files(&config.src)
        .map_err(|err| (format!("find env files: {err}"), ExitCode::from(1)))?;
    if env_files.is_empty() {
        return Err((
            format!("no .env* files found in: {}", config.src.display()),
            ExitCode::from(1),
        ));
    }

    for path in env_files {
        let base = path
            .file_name()
            .and_then(|name| name.to_str())
            .unwrap_or_default()
            .to_string();
        let target_env = infer_target_env(&base);
        println!(
            "import: {} -> si vault env={} section={}",
            path.display(),
            target_env,
            config.section
        );

        let raw = std::fs::read_to_string(&path)
            .map_err(|err| (format!("read {}: {err}", path.display()), ExitCode::from(1)))?;
        let values = parse_dotenv(&raw);
        for (key, value) in values {
            if config.dry_run {
                println!("dry-run: {}:{}:{}", target_env, config.section, key);
                continue;
            }
            set_vault_value(&config.identity_file, target_env, &config.section, &key, &value)?;
            println!("imported: {}:{}:{}", target_env, config.section, key);
        }
    }

    Ok(ExitCode::SUCCESS)
}

fn parse_args(args: &[String]) -> Result<(Config, bool), (String, ExitCode)> {
    let default_identity = default_identity_file();
    let mut src = PathBuf::from(".");
    let mut section = "default".to_string();
    let mut identity_file = default_identity;
    let mut dry_run = false;
    let mut i = 0;
    while i < args.len() {
        match args[i].as_str() {
            "--help" | "-h" => return Ok((
                Config {
                    src,
                    section,
                    identity_file,
                    dry_run,
                },
                true,
            )),
            "--src" => {
                i += 1;
                let Some(value) = args.get(i) else {
                    return usage_error("missing value for --src");
                };
                src = PathBuf::from(value.trim());
            }
            "--section" => {
                i += 1;
                let Some(value) = args.get(i) else {
                    return usage_error("missing value for --section");
                };
                section = value.trim().to_string();
            }
            "--identity-file" => {
                i += 1;
                let Some(value) = args.get(i) else {
                    return usage_error("missing value for --identity-file");
                };
                identity_file = PathBuf::from(value.trim());
            }
            "--dry-run" => {
                dry_run = true;
            }
            other if other.starts_with('-') => {
                return usage_error(&format!("unknown arg: {other}"));
            }
            other => {
                return usage_error(&format!("unknown arg: {other}"));
            }
        }
        i += 1;
    }

    if identity_file.as_os_str().is_empty() {
        return usage_error("identity file required");
    }

    Ok((
        Config {
            src,
            section,
            identity_file,
            dry_run,
        },
        false,
    ))
}

fn usage_error(message: &str) -> Result<(Config, bool), (String, ExitCode)> {
    Err((format!("{message}\n{USAGE_TEXT}"), ExitCode::from(2)))
}

fn default_identity_file() -> PathBuf {
    match env::var("HOME") {
        Ok(home) if !home.trim().is_empty() => Path::new(home.trim())
            .join(".si")
            .join("vault")
            .join("keys")
            .join("age.key"),
        _ => PathBuf::from(".si/vault/keys/age.key"),
    }
}

fn has_si_on_path() -> bool {
    Command::new("sh")
        .arg("-lc")
        .arg("command -v si >/dev/null 2>&1")
        .status()
        .map(|status| status.success())
        .unwrap_or(false)
}

fn set_vault_value(
    identity_file: &Path,
    target_env: &str,
    section: &str,
    key: &str,
    value: &str,
) -> Result<(), (String, ExitCode)> {
    let mut args = vec!["vault", "set", "--stdin", "--env", target_env, "--format"];
    if !section.trim().is_empty() {
        args.extend(["--section", section.trim()]);
    }
    args.push(key);

    let mut command = Command::new("si");
    command.args(args);
    command.stdin(Stdio::piped());
    command.stdout(Stdio::null());
    command.stderr(Stdio::piped());
    command.env("SI_VAULT_KEY_BACKEND", "file");
    command.env("SI_VAULT_KEY_FILE", identity_file);
    let mut child = command
        .spawn()
        .map_err(|err| (err.to_string(), ExitCode::from(1)))?;
    if let Some(stdin) = child.stdin.as_mut() {
        stdin
            .write_all(value.as_bytes())
            .map_err(|err| (err.to_string(), ExitCode::from(1)))?;
    }
    let output = child
        .wait_with_output()
        .map_err(|err| (err.to_string(), ExitCode::from(1)))?;
    if output.status.success() {
        Ok(())
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        if stderr.is_empty() {
            Err(("si vault set failed".to_string(), ExitCode::from(1)))
        } else {
            Err((stderr, ExitCode::from(1)))
        }
    }
}
