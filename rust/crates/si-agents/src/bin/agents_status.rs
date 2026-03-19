use std::env;
use std::process::ExitCode;

use si_agents::{latest_run_dirs, log_root_from_env, repo_root};

fn main() -> ExitCode {
    let root = match repo_root() {
        Ok(root) => root,
        Err(err) => {
            eprintln!("{err}");
            return ExitCode::from(1);
        }
    };
    if let Err(err) = env::set_current_dir(&root) {
        eprintln!("{err}");
        return ExitCode::from(1);
    }

    let log_root = log_root_from_env();
    print_latest(&log_root, "pr-guardian");
    print_latest(&log_root, "website-sentry");
    ExitCode::SUCCESS
}

fn print_latest(log_root: &std::path::Path, agent: &str) {
    let run_dirs = latest_run_dirs(&log_root.join(agent));
    if run_dirs.is_empty() {
        println!("{agent}: no runs");
        return;
    }

    let latest = &run_dirs[run_dirs.len() - 1];
    println!("{agent}: {}", latest.display());

    let summary = latest.join("summary.md");
    if let Ok(raw) = std::fs::read_to_string(summary) {
        for line in raw.lines().take(20) {
            println!("{line}");
        }
    }
    println!();
}
