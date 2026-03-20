use std::env;
use std::path::{Path, PathBuf};

pub fn repo_root() -> Result<PathBuf, String> {
    let cwd = env::current_dir().map_err(|err| err.to_string())?;
    if cwd.join("Cargo.toml").is_file() && cwd.join("rust").join("crates").join("si-cli").is_dir() {
        Ok(cwd)
    } else {
        Err("repo root not found; run from the si workspace root".to_string())
    }
}

pub fn log_root_from_env() -> PathBuf {
    let raw = env::var("AGENT_LOG_ROOT").unwrap_or_default();
    let trimmed = raw.trim();
    if trimmed.is_empty() { PathBuf::from(".artifacts/agent-logs") } else { PathBuf::from(trimmed) }
}

pub fn latest_run_dirs(dir: &Path) -> Vec<PathBuf> {
    let mut run_dirs = match std::fs::read_dir(dir) {
        Ok(entries) => entries
            .filter_map(Result::ok)
            .filter_map(|entry| {
                let path = entry.path();
                if path.is_dir() { Some(path) } else { None }
            })
            .collect::<Vec<_>>(),
        Err(_) => Vec::new(),
    };
    run_dirs.sort();
    run_dirs
}
