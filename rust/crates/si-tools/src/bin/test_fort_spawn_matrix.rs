use std::env;
use std::path::PathBuf;
use std::process::{Command, ExitCode};

fn main() -> ExitCode {
    let root = match repo_root() {
        Ok(root) => root,
        Err(err) => {
            eprintln!("{err}");
            return ExitCode::from(1);
        }
    };

    println!("[fort-spawn-matrix] running Rust fort package tests");

    match Command::new("cargo").current_dir(&root).arg("test").arg("-p").arg("si-rs-fort").status()
    {
        Ok(status) if status.success() => ExitCode::SUCCESS,
        Ok(status) => ExitCode::from(status.code().unwrap_or(1) as u8),
        Err(err) => {
            eprintln!("{err}");
            ExitCode::from(1)
        }
    }
}

fn repo_root() -> Result<PathBuf, String> {
    let cwd = env::current_dir().map_err(|err| err.to_string())?;
    if cwd.join("Cargo.toml").is_file() {
        Ok(cwd)
    } else {
        Err("repo root not found; run from the si workspace root".to_string())
    }
}
