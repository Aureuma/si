use std::env;
use std::path::Path;
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

    println!("== Agent doctor ==");

    let required = ["bash", "git", "python3"];
    let optional = ["shfmt", "gofmt"];

    for command in required {
        if has_command(command) {
            println!("PASS required command: {command}");
        } else {
            println!("FAIL required command missing: {command}");
            return ExitCode::from(1);
        }
    }

    for command in optional {
        if has_command(command) {
            println!("PASS optional command: {command}");
        } else {
            println!("WARN optional command missing: {command}");
        }
    }

    println!();
    println!("== Syntax checks ==");
    let files = ["tools/agents/doctor.sh", "tools/agents/status.sh"];
    for file in files {
        if let Err(code) = run_bash_syntax_check(Path::new(file)) {
            return code;
        }
    }
    println!("PASS shell syntax");
    ExitCode::SUCCESS
}

fn has_command(name: &str) -> bool {
    Command::new("sh")
        .arg("-lc")
        .arg(format!("command -v {} >/dev/null 2>&1", shell_escape(name)))
        .status()
        .map(|status| status.success())
        .unwrap_or(false)
}

fn run_bash_syntax_check(path: &Path) -> Result<(), ExitCode> {
    let status = Command::new("bash").arg("-n").arg(path).status().map_err(|err| {
        eprintln!("{err}");
        ExitCode::from(1)
    })?;
    if status.success() { Ok(()) } else { Err(ExitCode::from(status.code().unwrap_or(1) as u8)) }
}

fn shell_escape(value: &str) -> String {
    value.replace('\'', "'\"'\"'")
}
