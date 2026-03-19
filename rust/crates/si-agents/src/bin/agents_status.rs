use std::env;
use std::process::ExitCode;

use si_agents::{log_root_from_env, repo_root};

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

    let _ = log_root_from_env();
    println!("automation agents removed");
    ExitCode::SUCCESS
}
