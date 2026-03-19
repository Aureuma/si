use std::env;
use std::os::unix::process::CommandExt;
use std::path::{Path, PathBuf};
use std::process::{Command, ExitCode};

fn main() -> ExitCode {
    match run() {
        Ok(code) => code,
        Err(message) => {
            eprintln!("critic: {message}");
            ExitCode::from(1)
        }
    }
}

fn run() -> Result<ExitCode, String> {
    let target = resolve_target().ok_or_else(|| {
        "critic-go helper not found (set SI_CRITIC_GO_BIN or ship /usr/local/bin/critic-go)"
            .to_string()
    })?;
    let args: Vec<String> = env::args().skip(1).collect();
    let mut command = Command::new(&target);
    command.args(args);
    let err = command.exec();
    Err(format!("exec {}: {err}", target.display()))
}

fn resolve_target() -> Option<PathBuf> {
    if let Some(explicit) = env::var("SI_CRITIC_GO_BIN")
        .ok()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
    {
        return resolve_path(&explicit);
    }

    for candidate in ["/usr/local/bin/critic-go", "critic-go"] {
        if let Some(path) = resolve_path(candidate) {
            return Some(path);
        }
    }
    None
}

fn resolve_path(candidate: &str) -> Option<PathBuf> {
    let path = PathBuf::from(candidate);
    if path.components().count() > 1 {
        return path.exists().then_some(path);
    }
    let path_env = env::var_os("PATH")?;
    env::split_paths(&path_env)
        .map(|dir| dir.join(candidate))
        .find(|full| is_executable(full))
}

fn is_executable(path: &Path) -> bool {
    path.is_file()
}
