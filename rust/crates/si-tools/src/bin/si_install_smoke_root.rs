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

    let work = tempfile::tempdir().map_err(|err| err.to_string())?;
    let install_dir = work.path().join("bin");
    std::fs::create_dir_all(&install_dir).map_err(|err| err.to_string())?;

    println!("==> root smoke: install from source checkout");
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
        "--install-dir",
        install_dir.to_str().unwrap_or_default(),
        "--force",
        "--no-buildx",
        "--quiet",
    ]))?;

    let target = install_dir.join("si");
    if !target.is_file() {
        return Err(format!("expected binary at {}", target.display()));
    }
    run_checked(Command::new(&target).arg("version"))?;
    run_checked(Command::new(&target).arg("--help"))?;

    println!("==> root smoke: uninstall");
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
        "--install-dir",
        install_dir.to_str().unwrap_or_default(),
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
