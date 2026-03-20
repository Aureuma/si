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
        eprintln!("usage: orbits-test-runner <unit|policy|catalog|e2e|all>");
        return ExitCode::from(2);
    }

    let lane = args[0].trim().to_ascii_lowercase();
    let rest = &args[1..];

    match lane.as_str() {
        "unit" | "policy" | "catalog" | "e2e" => {
            if !rest.is_empty() {
                eprintln!("usage: orbits-test-runner {lane}");
                return ExitCode::from(2);
            }
            run_lane(&root, &lane)
        }
        "all" => run_all(&root, rest),
        _ => {
            eprintln!("unknown orbits lane: {lane}");
            ExitCode::from(2)
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

fn run_all(root: &PathBuf, args: &[String]) -> ExitCode {
    let mut skip_unit = false;
    let mut skip_policy = false;
    let mut skip_catalog = false;
    let mut skip_e2e = false;

    for arg in args {
        match arg.as_str() {
            "--skip-unit" => skip_unit = true,
            "--skip-policy" => skip_policy = true,
            "--skip-catalog" => skip_catalog = true,
            "--skip-e2e" => skip_e2e = true,
            _ => {
                eprintln!(
                    "usage: orbits-test-runner all [--skip-unit] [--skip-policy] [--skip-catalog] [--skip-e2e]"
                );
                return ExitCode::from(2);
            }
        }
    }

    if !skip_unit && run_lane(root, "unit") != ExitCode::SUCCESS {
        return ExitCode::from(1);
    }
    if !skip_policy && run_lane(root, "policy") != ExitCode::SUCCESS {
        return ExitCode::from(1);
    }
    if !skip_catalog && run_lane(root, "catalog") != ExitCode::SUCCESS {
        return ExitCode::from(1);
    }
    if !skip_e2e && run_lane(root, "e2e") != ExitCode::SUCCESS {
        return ExitCode::from(1);
    }

    eprintln!("==> all requested orbit runners passed");
    ExitCode::SUCCESS
}

fn run_lane(root: &PathBuf, lane: &str) -> ExitCode {
    eprintln!("==> orbits {lane}");
    let mut command = Command::new("cargo");
    command.current_dir(root).arg("test").arg("-p").arg("si-rs-provider-catalog");
    match command.status() {
        Ok(status) if status.success() => ExitCode::SUCCESS,
        Ok(status) => ExitCode::from(status.code().unwrap_or(1) as u8),
        Err(err) => {
            eprintln!("{err}");
            ExitCode::from(1)
        }
    }
}
