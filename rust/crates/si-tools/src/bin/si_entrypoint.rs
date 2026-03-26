use std::env;
use std::fs;
use std::os::unix::process::CommandExt;
use std::path::{Path, PathBuf};
use std::process::{Command, ExitCode};

use si_tools::{
    collect_git_safe_directories, ensure_git_safe_directories, shell_escape, sync_codex_auth,
    sync_codex_skills, write_codex_config,
};

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(message) => {
            eprintln!("si-entrypoint: {message}");
            ExitCode::from(1)
        }
    }
}

fn run() -> Result<(), String> {
    ensure_runtime_directories().map_err(|err| format!("ensure runtime dirs: {err}"))?;
    let args: Vec<String> = env::args().skip(1).collect();
    let command = if args.is_empty() {
        vec!["bash".to_string(), "-lc".to_string(), "sleep infinity".to_string()]
    } else {
        args
    };

    if is_root() {
        apply_host_ids()?;
        maybe_clone_repo()?;
        sync_codex_auth(
            env::var("SI_CODEX_PROFILE_ID").ok().as_deref(),
            Path::new("/home/si/.si"),
            Path::new("/home/si/.codex"),
        )
        .map_err(|err| format!("sync auth: {err}"))?;
        sync_codex_skills(Path::new("/opt/si/codex-skills"), Path::new("/home/si/.codex/skills"))
            .map_err(|err| format!("sync skills: {err}"))?;
        let template_path = env::var("CODEX_CONFIG_TEMPLATE")
            .ok()
            .filter(|value| !value.trim().is_empty())
            .map(PathBuf::from);
        write_codex_config(Path::new("/home/si/.codex/config.toml"), template_path.as_deref())
            .map_err(|err| format!("write config: {err}"))?;
        chown_paths(&["/home/si/.codex", "/home/si/.config/gh", "/workspace"])?;
        let safe_dirs = collect_git_safe_directories(Some(Path::new("/workspace")))
            .map_err(|err| format!("collect git safe directories: {err}"))?;
        ensure_git_safe_directories(Path::new("/root"), &safe_dirs)
            .map_err(|err| format!("configure root git safe dirs: {err}"))?;
        ensure_git_safe_directories(Path::new("/home/si"), &safe_dirs)
            .map_err(|err| format!("configure si git safe dirs: {err}"))?;
        let quoted = command.iter().map(|arg| shell_escape(arg)).collect::<Vec<_>>().join(" ");
        let err =
            Command::new("su").arg("-s").arg("/bin/bash").arg("si").arg("-c").arg(quoted).exec();
        return Err(format!("exec su: {err}"));
    }

    let mut child = Command::new(&command[0]);
    child.args(&command[1..]);
    let err = child.exec();
    Err(format!("exec {}: {err}", command[0]))
}

fn ensure_runtime_directories() -> std::io::Result<()> {
    for path in ["/home/si/.codex", "/home/si/.config/gh", "/workspace"] {
        fs::create_dir_all(path)?;
    }
    Ok(())
}

fn is_root() -> bool {
    unsafe { libc::geteuid() == 0 }
}

fn apply_host_ids() -> Result<(), String> {
    let host_gid = parse_id_env(&["SI_HOST_GID", "HOST_GID", "LOCAL_GID"]);
    let host_uid = parse_id_env(&["SI_HOST_UID", "HOST_UID", "LOCAL_UID"]);

    if let Some(gid) = host_gid {
        run_status(
            Command::new("groupmod").arg("-o").arg("-g").arg(gid.to_string()).arg("si"),
            "groupmod",
        )?;
    }
    if let Some(uid) = host_uid {
        let mut command = Command::new("usermod");
        command.arg("-o").arg("-u").arg(uid.to_string());
        if let Some(gid) = host_gid {
            command.arg("-g").arg(gid.to_string());
        }
        command.arg("si");
        run_status(&mut command, "usermod")?;
    }
    Ok(())
}

fn maybe_clone_repo() -> Result<(), String> {
    let Some(url) = env_value(&["SI_REPO_URL", "GIT_REPO_URL", "REPO_URL"]) else {
        return Ok(());
    };
    let dest = env_value(&["SI_REPO_DEST", "GIT_REPO_DEST", "REPO_DEST"])
        .unwrap_or_else(|| "/workspace".to_string());
    let dest_path = PathBuf::from(dest);
    if dest_path.exists() && !directory_is_empty(&dest_path).map_err(|err| err.to_string())? {
        return Ok(());
    }
    fs::create_dir_all(&dest_path).map_err(|err| err.to_string())?;
    let mut command = Command::new("git");
    command.arg("clone").arg("--depth").arg("1");
    if let Some(reference) = env_value(&["SI_REPO_REF", "GIT_REPO_REF", "REPO_REF"]) {
        command.arg("--branch").arg(reference);
    }
    command.arg(url).arg(&dest_path);
    run_status(&mut command, "git clone")
}

fn directory_is_empty(path: &Path) -> std::io::Result<bool> {
    Ok(fs::read_dir(path)?.next().is_none())
}

fn chown_paths(paths: &[&str]) -> Result<(), String> {
    for path in paths {
        run_status(Command::new("chown").arg("-R").arg("si:si").arg(path), "chown")?;
    }
    Ok(())
}

fn env_value(keys: &[&str]) -> Option<String> {
    keys.iter().find_map(|key| {
        env::var(key).ok().map(|value| value.trim().to_string()).filter(|value| !value.is_empty())
    })
}

fn parse_id_env(keys: &[&str]) -> Option<u32> {
    env_value(keys).and_then(|value| value.parse::<u32>().ok())
}

fn run_status(command: &mut Command, label: &str) -> Result<(), String> {
    let status = command.status().map_err(|err| format!("{label}: {err}"))?;
    if status.success() { Ok(()) } else { Err(format!("{label}: exit status {status}")) }
}
