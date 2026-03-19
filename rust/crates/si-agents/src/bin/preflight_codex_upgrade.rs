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
    if let Err(err) = env::set_current_dir(&root) {
        eprintln!("{err}");
        return ExitCode::from(1);
    }

    let go_timeout = env::var("SI_CODEX_UPGRADE_TEST_TIMEOUT")
        .unwrap_or_else(|_| "15m".to_string())
        .trim()
        .to_string();
    let cargo_timeout = env::var("SI_CODEX_UPGRADE_CARGO_TEST_TIMEOUT")
        .unwrap_or_else(|_| "15m".to_string())
        .trim()
        .to_string();
    let go_bin = env::var("SI_GO_BIN").unwrap_or_else(|_| "go".to_string()).trim().to_string();

    let tests = [(
        "./tools/si",
        "Test(CodexBrowserMCPURL|CodexContainerWorkspaceMatches|CodexContainerWorkspaceSource|SplitNameAndFlags|CodexRespawnBoolFlags|SplitDyadSpawnArgs|DyadProfileArg|DyadSkipAuthArg|DyadLoopBoolEnv|DyadLoopIntSetting|DockerBuildArgsIncludesSecret|RunDockerBuild|ShouldRetryLegacyBuild|CmdBuildImage)",
    )];

    println!("[preflight] codex image compatibility checks (spawn + dyad + container runtime)");
    for (pkg, pattern) in tests {
        println!("[preflight] {go_bin} test {pkg} -run '{pattern}'");
        let status = Command::new(&go_bin)
            .args(["test", pkg, "-run", pattern, "-count=1", "-timeout", &go_timeout])
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

    println!("[preflight] cargo test -p si-tools --lib -- --nocapture");
    let cargo_status = Command::new("cargo")
        .args(["test", "-p", "si-tools", "--lib", "--", "--nocapture"])
        .env("CARGO_TERM_COLOR", "always")
        .env("CARGO_BUILD_JOBS", "1")
        .env("CARGO_NET_RETRY", "2")
        .env("CARGO_HTTP_TIMEOUT", cargo_timeout.as_str())
        .status();
    match cargo_status {
        Ok(status) if status.success() => {}
        Ok(status) => return ExitCode::from(status.code().unwrap_or(1) as u8),
        Err(err) => {
            eprintln!("{err}");
            return ExitCode::from(1);
        }
    }

    println!("[preflight] codex image compatibility checks passed");
    ExitCode::SUCCESS
}
