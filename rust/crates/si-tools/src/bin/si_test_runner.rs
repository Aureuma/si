use std::env;
use std::path::PathBuf;
use std::process::{Command, ExitCode};

const TEST_MODULE_PATTERNS: &[&str] = &["./..."];

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

    let go_bin = env::var("SI_GO_BIN").unwrap_or_else(|_| "go".to_string()).trim().to_string();

    match args[0].trim() {
        "workspace" => run_workspace(&root, &go_bin, &args[1..]),
        "vault" => run_vault(&root, &go_bin, &args[1..]),
        "all" => run_all(&root, &go_bin, &args[1..]),
        _ => {
            eprintln!("usage: si-test-runner <workspace|vault|all> [args]");
            ExitCode::from(2)
        }
    }
}

fn repo_root() -> Result<PathBuf, String> {
    let cwd = env::current_dir().map_err(|err| err.to_string())?;
    if cwd.join("tools/si/go.mod").is_file() {
        Ok(cwd)
    } else {
        Err("tools/si/go.mod not found. Run this command from the repo root.".to_string())
    }
}

fn go_module_dir(root: &PathBuf) -> PathBuf {
    root.join("tools/si")
}

fn run_workspace(root: &PathBuf, go_bin: &str, args: &[String]) -> ExitCode {
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

    let timeout = env::var("SI_GO_TEST_TIMEOUT").unwrap_or_else(|_| "15m".to_string());
    if !print_go_version(root, go_bin) {
        return ExitCode::from(1);
    }
    println!("go test timeout: {}", timeout.trim());
    if list {
        for module in TEST_MODULE_PATTERNS {
            println!("{module}");
        }
        return ExitCode::SUCCESS;
    }

    println!("Running go test on: ./...");
    let mut cmd = Command::new(go_bin);
    cmd.current_dir(go_module_dir(root));
    cmd.arg("test").arg("-timeout").arg(timeout.trim());
    cmd.arg("./...");
    run_command(cmd)
}

fn run_vault(root: &PathBuf, go_bin: &str, args: &[String]) -> ExitCode {
    let mut quick = false;
    for arg in args {
        match arg.as_str() {
            "--quick" => quick = true,
            "--help" | "-h" => {
                println!("usage: si-test-runner vault [--quick]");
                return ExitCode::SUCCESS;
            }
            _ => {
                eprintln!("usage: si-test-runner vault [--quick]");
                return ExitCode::from(2);
            }
        }
    }

    let timeout = env::var("SI_GO_TEST_TIMEOUT").unwrap_or_else(|_| "20m".to_string());
    if !print_go_version(root, go_bin) {
        return ExitCode::from(1);
    }
    println!("go test timeout: {}", timeout.trim());

    println!("[1/3] vault command wiring + guardrail unit tests");
    let mut wiring = Command::new(go_bin);
    wiring.current_dir(go_module_dir(root));
    wiring.args([
        "test",
        "-timeout",
        timeout.trim(),
        "-count=1",
        "-shuffle=on",
        "-run",
        "^(TestVaultCommandActionSetsArePopulated|TestVaultActionNamesMatchDispatchSwitches|TestVaultValidateImplicitTargetRepoScope.*)$",
        ".",
    ]);
    if run_command(wiring) != ExitCode::SUCCESS {
        return ExitCode::from(1);
    }

    println!("[2/3] vault internal package tests");
    let mut internal = Command::new(go_bin);
    internal.current_dir(go_module_dir(root));
    internal.args([
        "test",
        "-timeout",
        timeout.trim(),
        "-count=1",
        "-shuffle=on",
        "./internal/vault/...",
    ]);
    if run_command(internal) != ExitCode::SUCCESS {
        return ExitCode::from(1);
    }

    if quick {
        println!("[3/3] skipped vault e2e subprocess tests (--quick)");
    } else {
        println!("[3/3] vault e2e subprocess tests");
        let mut e2e = Command::new(go_bin);
        e2e.current_dir(go_module_dir(root));
        e2e.args([
            "test",
            "-timeout",
            timeout.trim(),
            "-count=1",
            "-shuffle=on",
            "-run",
            "^TestVaultE2E_",
            ".",
        ]);
        if run_command(e2e) != ExitCode::SUCCESS {
            return ExitCode::from(1);
        }
    }

    println!("vault strict test suite: ok");
    ExitCode::SUCCESS
}

fn run_all(root: &PathBuf, go_bin: &str, args: &[String]) -> ExitCode {
    let mut skip_go = false;
    let mut skip_vault = false;
    let mut skip_installer = false;
    let mut skip_npm = false;
    let mut skip_docker = false;

    for arg in args {
        match arg.as_str() {
            "--skip-go" => skip_go = true,
            "--skip-vault" => skip_vault = true,
            "--skip-installer" => skip_installer = true,
            "--skip-npm" => skip_npm = true,
            "--skip-docker" => skip_docker = true,
            "--help" | "-h" => {
                println!(
                    "usage: si-test-runner all [--skip-go] [--skip-vault] [--skip-installer] [--skip-npm] [--skip-docker]"
                );
                return ExitCode::SUCCESS;
            }
            _ => {
                eprintln!(
                    "usage: si-test-runner all [--skip-go] [--skip-vault] [--skip-installer] [--skip-npm] [--skip-docker]"
                );
                return ExitCode::from(2);
            }
        }
    }

    if !skip_go {
        eprintln!("==> Go module tests");
        if run_workspace(root, go_bin, &[]) != ExitCode::SUCCESS {
            return ExitCode::from(1);
        }
    }
    if !skip_vault {
        eprintln!("==> Vault strict suite");
        if run_vault(root, go_bin, &[]) != ExitCode::SUCCESS {
            return ExitCode::from(1);
        }
    }
    if !skip_installer {
        eprintln!("==> Installer host smoke");
        if run_script(root, "./tools/test-install-si.sh") != ExitCode::SUCCESS {
            return ExitCode::from(1);
        }
    }
    if !skip_npm {
        eprintln!("==> npm installer smoke");
        if run_script(root, "./tools/test-install-si-npm.sh") != ExitCode::SUCCESS {
            return ExitCode::from(1);
        }
    }
    if !skip_docker {
        eprintln!("==> Installer docker smoke");
        if run_script(root, "./tools/test-install-si-docker.sh") != ExitCode::SUCCESS {
            return ExitCode::from(1);
        }
    }
    eprintln!("==> all requested tests passed");
    ExitCode::SUCCESS
}

fn print_go_version(root: &PathBuf, go_bin: &str) -> bool {
    let output = Command::new(go_bin).arg("version").current_dir(root).output();
    match output {
        Ok(output) if output.status.success() => {
            println!("go version: {}", String::from_utf8_lossy(&output.stdout).trim());
            true
        }
        Ok(output) => {
            eprintln!("{}", String::from_utf8_lossy(&output.stderr).trim());
            false
        }
        Err(err) => {
            eprintln!("{err}");
            false
        }
    }
}

fn run_script(root: &PathBuf, rel_path: &str) -> ExitCode {
    let path = root.join(rel_path);
    let mut command = Command::new(path);
    command.current_dir(root);
    run_command(command)
}

fn run_command(mut command: Command) -> ExitCode {
    match command.status() {
        Ok(status) if status.success() => ExitCode::SUCCESS,
        Ok(status) => ExitCode::from(status.code().unwrap_or(1) as u8),
        Err(err) => {
            eprintln!("{err}");
            ExitCode::from(1)
        }
    }
}
