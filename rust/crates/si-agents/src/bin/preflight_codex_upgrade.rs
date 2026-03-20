use std::env;
use std::process::{Command, ExitCode};

use si_agents::repo_root;

fn main() -> ExitCode {
    let root = match repo_root() {
        Ok(root) => root,
        Err(err) => {
            eprintln!("{err}");
            return ExitCode::from(1);
        }
    };

    let cargo_timeout = env::var("SI_CODEX_UPGRADE_CARGO_TEST_TIMEOUT")
        .unwrap_or_else(|_| "15m".to_string())
        .trim()
        .to_string();
    let test_groups = [
        ("si-rs-codex", "[preflight] cargo test -p si-rs-codex"),
        ("si-rs-dyad", "[preflight] cargo test -p si-rs-dyad"),
        ("si-rs-docker", "[preflight] cargo test -p si-rs-docker"),
        ("si-tools", "[preflight] cargo test -p si-tools"),
    ];

    println!("[preflight] codex upgrade compatibility checks");
    for (pkg, label) in test_groups {
        println!("{label}");
        let status = Command::new("cargo")
            .current_dir(&root)
            .args(["test", "-p", pkg])
            .env("CARGO_TERM_COLOR", "always")
            .env("CARGO_BUILD_JOBS", "1")
            .env("CARGO_NET_RETRY", "2")
            .env("CARGO_HTTP_TIMEOUT", cargo_timeout.as_str())
            .status();
        match status {
            Ok(status) if status.success() => {}
            Ok(status) => return ExitCode::from(status.code().unwrap_or(1) as u8),
            Err(err) => {
                eprintln!("{err}");
                return ExitCode::from(1);
            }
        }
    }

    println!("[preflight] codex upgrade compatibility checks passed");
    ExitCode::SUCCESS
}
