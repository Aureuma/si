use std::env;
use std::process::{Command, ExitCode};

fn main() -> ExitCode {
    let root = match repo_root() {
        Ok(root) => root,
        Err(err) => {
            eprintln!("{err}");
            return ExitCode::from(1);
        }
    };

    println!("[fort-spawn-matrix] delegating to Go integration test");

    let go_bin = env::var("SI_GO_BIN").unwrap_or_else(|_| "go".to_string()).trim().to_string();

    match Command::new(go_bin)
        .args([
            "test",
            "-tags=integration",
            "./tools/si",
            "-run",
            "TestFortSpawnMatrix",
            "-count=1",
        ])
        .current_dir(&root)
        .status()
    {
        Ok(status) if status.success() => ExitCode::SUCCESS,
        Ok(status) => ExitCode::from(status.code().unwrap_or(1) as u8),
        Err(err) => {
            eprintln!("{err}");
            ExitCode::from(1)
        }
    }
}

fn repo_root() -> Result<String, String> {
    let cwd = env::current_dir().map_err(|err| err.to_string())?;
    if cwd.join("go.work").is_file() {
        Ok(cwd.display().to_string())
    } else {
        Err("go.work not found; run from repo root".to_string())
    }
}
