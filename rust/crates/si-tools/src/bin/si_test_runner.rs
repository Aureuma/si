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

    let args: Vec<String> = env::args().skip(1).collect();
    if args.is_empty() {
        eprintln!("usage: si-test-runner <workspace|vault|all> [args]");
        return ExitCode::from(2);
    }

    match args[0].trim() {
        "workspace" => run_workspace(&root, &args[1..]),
        "vault" => run_vault(&root, &args[1..]),
        "all" => run_all(&root, &args[1..]),
        _ => {
            eprintln!("usage: si-test-runner <workspace|vault|all> [args]");
            ExitCode::from(2)
        }
    }
}

fn repo_root() -> Result<PathBuf, String> {
    let cwd = env::current_dir().map_err(|err| err.to_string())?;
    if cwd.join("Cargo.toml").is_file() && cwd.join("rust").join("crates").join("si-cli").is_dir() {
        Ok(cwd)
    } else {
        Err("repo root not found. Run this command from the si workspace root.".to_string())
    }
}

fn run_workspace(root: &PathBuf, args: &[String]) -> ExitCode {
    let mut list = false;
    for arg in args {
        match arg.as_str() {
            "--list" => list = true,
            "--help" | "-h" => {
                println!("usage: si-test-runner workspace [--list]");
                return ExitCode::SUCCESS;
            }
            _ => {
                eprintln!("usage: si-test-runner workspace [--list]");
                return ExitCode::from(2);
            }
        }
    }

    if list {
        println!("cargo test --workspace");
        return ExitCode::SUCCESS;
    }

    println!("Running cargo test --workspace");
    run_command(Command::new("cargo").current_dir(root).arg("test").arg("--workspace"))
}

fn run_vault(root: &PathBuf, args: &[String]) -> ExitCode {
    let mut quick = false;
    for arg in args {
        match arg.as_str() {
            "--quick" => quick = true,
            "--help" | "-h" => {
                println!("usage: si-test-runner vault");
                return ExitCode::SUCCESS;
            }
            _ => {
                eprintln!("usage: si-test-runner vault");
                return ExitCode::from(2);
            }
        }
    }

    if quick {
        println!("Running cargo test -p si-rs-vault (--quick is a compatibility no-op)");
    } else {
        println!("Running cargo test -p si-rs-vault");
    }
    run_command(Command::new("cargo").current_dir(root).arg("test").arg("-p").arg("si-rs-vault"))
}

fn run_all(root: &PathBuf, args: &[String]) -> ExitCode {
    let mut skip_workspace = false;
    let mut skip_vault = false;
    let mut skip_installer = false;
    let mut skip_npm = false;

    for arg in args {
        match arg.as_str() {
            "--skip-workspace" => skip_workspace = true,
            "--skip-vault" => skip_vault = true,
            "--skip-installer" => skip_installer = true,
            "--skip-npm" => skip_npm = true,
            "--help" | "-h" => {
                println!(
                    "usage: si-test-runner all [--skip-workspace] [--skip-vault] [--skip-installer] [--skip-npm]"
                );
                return ExitCode::SUCCESS;
            }
            _ => {
                eprintln!(
                    "usage: si-test-runner all [--skip-workspace] [--skip-vault] [--skip-installer] [--skip-npm]"
                );
                return ExitCode::from(2);
            }
        }
    }

    if !skip_workspace {
        eprintln!("==> Rust workspace tests");
        if run_workspace(root, &[]) != ExitCode::SUCCESS {
            return ExitCode::from(1);
        }
    }
    if !skip_vault {
        eprintln!("==> Vault package tests");
        if run_vault(root, &[]) != ExitCode::SUCCESS {
            return ExitCode::from(1);
        }
    }
    if !skip_installer {
        eprintln!("==> Installer host smoke");
        if run_command(Command::new("cargo").current_dir(root).args([
            "run",
            "--quiet",
            "--locked",
            "-p",
            "si-rs-cli",
            "--",
            "build",
            "installer",
            "smokehost",
        ])) != ExitCode::SUCCESS
        {
            return ExitCode::from(1);
        }
    }
    if !skip_npm {
        eprintln!("==> npm installer smoke");
        if run_command(Command::new("cargo").current_dir(root).args([
            "run",
            "--quiet",
            "--locked",
            "-p",
            "si-rs-cli",
            "--",
            "build",
            "installer",
            "smokenpm",
        ])) != ExitCode::SUCCESS
        {
            return ExitCode::from(1);
        }
    }
    ExitCode::SUCCESS
}

fn run_command(command: &mut Command) -> ExitCode {
    match command.status() {
        Ok(status) if status.success() => ExitCode::SUCCESS,
        Ok(status) => ExitCode::from(status.code().unwrap_or(1) as u8),
        Err(err) => {
            eprintln!("{err}");
            ExitCode::from(1)
        }
    }
}
