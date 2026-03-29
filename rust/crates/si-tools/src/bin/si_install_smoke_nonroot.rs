use std::env;
use std::path::PathBuf;
use std::process::{Command, ExitCode};

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(err) => {
            eprintln!("ERROR: {err}");
            ExitCode::from(1)
        }
    }
}

fn run() -> Result<(), String> {
    let source_dir = PathBuf::from(
        env::var("SI_INSTALL_SOURCE_DIR").unwrap_or_else(|_| "/workspace/si".to_owned()),
    );
    if !source_dir.join("Cargo.toml").is_file() {
        return Err(format!("repo root not found at {}", source_dir.display()));
    }

    let home = PathBuf::from(env::var("HOME").map_err(|err| err.to_string())?);
    let target = home.join(".local").join("bin").join("si");

    println!("==> non-root smoke: install into user default path");
    run_checked(Command::new("cargo").current_dir(&source_dir).args([
        "run",
        "--quiet",
        "--locked",
        "-p",
        "si-rs-cli",
        "--",
        "build",
        "installer",
        "run",
        "--source-dir",
        source_dir.to_str().unwrap_or_default(),
        "--force",
        "--no-buildx",
        "--quiet",
    ]))?;

    if !target.is_file() {
        return Err(format!("expected binary at {}", target.display()));
    }
    run_checked(Command::new(&target).arg("version"))?;
    run_checked(Command::new(&target).arg("--help"))?;

    println!("==> non-root smoke: uninstall");
    run_checked(Command::new("cargo").current_dir(&source_dir).args([
        "run",
        "--quiet",
        "--locked",
        "-p",
        "si-rs-cli",
        "--",
        "build",
        "installer",
        "run",
        "--uninstall",
        "--quiet",
    ]))?;

    if target.exists() {
        return Err(format!("expected uninstall to remove {}", target.display()));
    }

    println!("OK");
    Ok(())
}

fn run_checked(command: &mut Command) -> Result<(), String> {
    let status = command.status().map_err(|err| err.to_string())?;
    if status.success() { Ok(()) } else { Err(format!("command failed: {status}")) }
}
