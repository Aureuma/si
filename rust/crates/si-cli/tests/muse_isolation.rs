//! Muse echo + UUID isolation through SI itself (audited behavior, SI-driven).
//!
//! These run the REAL `muse` native (resolved from `MUSE_TEST_NATIVE` or
//! PATH) through `si muse` commands on isolated `--home` trees: two
//! profiles keep independent echo session stores, the same session UUID in
//! both profiles keeps isolated transcripts, and respawn refuses a foreign
//! profile's session id while attempting a known one (the native's own
//! login requirement then surfaces unmodified — SI gates, the native
//! decides).
//!
//! Tests SKIP (pass with a notice) when no native is installed.
//! `MUSE_NO_AUTO_UPDATE=1` is set on every SI invocation. A sentinel
//! asserts the real `~/.config/muse` store is never touched.

use assert_cmd::Command;
use std::collections::BTreeSet;
use std::path::{Path, PathBuf};
use tempfile::tempdir;

fn cargo_bin() -> Command {
    Command::cargo_bin("si").expect("si binary should build")
}

fn resolve_native() -> Option<PathBuf> {
    if let Some(explicit) = std::env::var_os("MUSE_TEST_NATIVE") {
        let path = PathBuf::from(explicit);
        if path.is_file() {
            return Some(path);
        }
        eprintln!("MUSE_TEST_NATIVE does not point at a file; skipping muse-isolation tests");
        return None;
    }
    std::env::var_os("PATH").and_then(|paths| {
        std::env::split_paths(&paths)
            .map(|dir| dir.join("muse"))
            .find(|candidate| candidate.is_file())
    })
}

fn require_native(test: &str) -> Option<PathBuf> {
    match resolve_native() {
        Some(program) => Some(program),
        None => {
            eprintln!("SKIP {test}: no `muse` native on PATH (set MUSE_TEST_NATIVE to run)");
            None
        }
    }
}

fn path_with_native(native: &Path) -> String {
    let dir = native.parent().expect("native parent").display().to_string();
    format!("{dir}:{}", std::env::var("PATH").unwrap_or_default())
}

fn kill_owned_tmux_server(home: &Path) {
    let socket = home.join(".si").join("runtime").join("tmux.sock");
    let _ = std::process::Command::new("tmux")
        .args(["-S", &socket.display().to_string(), "kill-server"])
        .output();
}

struct TmuxGuard<'a> {
    home: &'a Path,
}

impl Drop for TmuxGuard<'_> {
    fn drop(&mut self) {
        kill_owned_tmux_server(self.home);
    }
}

/// Snapshot of the real default Muse store: must be byte-identical after.
struct RealStoreSentinel {
    auth_bytes: Option<Vec<u8>>,
}

impl RealStoreSentinel {
    fn capture() -> Self {
        let path = std::env::var_os("HOME")
            .map(PathBuf::from)
            .unwrap_or_else(|| PathBuf::from("/"))
            .join(".config")
            .join("muse")
            .join("auth.json");
        Self { auth_bytes: std::fs::read(&path).ok() }
    }

    fn verify_untouched(&self) {
        let after = Self::capture();
        assert_eq!(
            self.auth_bytes, after.auth_bytes,
            "the real ~/.config/muse/auth.json must never be touched by isolation tests"
        );
    }
}

fn si_muse_run(
    home_arg: &str,
    path_env: &str,
    profile: &str,
    argv: &[&str],
) -> std::process::Output {
    let mut args = vec!["muse", "run", "--profile", profile, "--home", home_arg, "--"];
    args.extend(argv.iter().copied());
    cargo_bin()
        .env("PATH", path_env)
        .env("MUSE_NO_AUTO_UPDATE", "1")
        .args(&args)
        .assert()
        .success()
        .get_output()
        .clone()
}

fn session_logs(data_root: &Path) -> Vec<PathBuf> {
    let sessions = data_root.join("muse").join("sessions");
    let mut logs = Vec::new();
    let mut stack = vec![sessions];
    while let Some(dir) = stack.pop() {
        let Ok(entries) = std::fs::read_dir(&dir) else { continue };
        for entry in entries.flatten() {
            if entry.file_type().map(|kind| kind.is_dir()).unwrap_or(false) {
                stack.push(entry.path());
            } else if entry.file_name().to_string_lossy() == "session.jsonl" {
                logs.push(entry.path());
            }
        }
    }
    logs
}

/// Top-level session UUIDs: `<uuid>/session.jsonl` outside any `subagent/` subtree.
fn top_level_session_ids(data_root: &Path) -> BTreeSet<String> {
    session_logs(data_root)
        .into_iter()
        .filter(|log| !log.components().any(|c| c.as_os_str() == "subagent"))
        .filter_map(|log| log.parent()?.file_name()?.to_str().map(str::to_owned))
        .collect()
}

fn profile_data_dir(home: &Path, profile: &str) -> PathBuf {
    home.join(".si").join("muse").join("profiles").join(profile).join("data")
}

#[test]
fn echo_profiles_keep_independent_stores() {
    let Some(native) = require_native("echo_profiles_keep_independent_stores") else { return };
    let sentinel = RealStoreSentinel::capture();
    let home = tempdir().expect("home tempdir");
    let home_arg = home.path().to_str().expect("home path").to_owned();
    let path_env = path_with_native(&native);
    for profile in ["alpha", "beta"] {
        cargo_bin()
            .args(["muse", "profile", "add", profile, "--home", &home_arg])
            .assert()
            .success();
    }

    let out_a =
        si_muse_run(&home_arg, &path_env, "alpha", &["exec", "--provider", "echo", "prompt-AAAA"]);
    assert!(
        String::from_utf8_lossy(&out_a.stdout).contains("prompt-AAAA"),
        "echo must repeat the alpha prompt"
    );
    let out_b =
        si_muse_run(&home_arg, &path_env, "beta", &["exec", "--provider", "echo", "prompt-BBBB"]);
    assert!(
        String::from_utf8_lossy(&out_b.stdout).contains("prompt-BBBB"),
        "echo must repeat the beta prompt"
    );

    let logs_a = session_logs(&profile_data_dir(home.path(), "alpha"));
    let logs_b = session_logs(&profile_data_dir(home.path(), "beta"));
    assert!(!logs_a.is_empty(), "alpha must persist session logs in its own store");
    assert!(!logs_b.is_empty(), "beta must persist session logs in its own store");
    for log in &logs_a {
        let text = std::fs::read_to_string(log).expect("read alpha log");
        assert!(text.contains("prompt-AAAA"), "alpha log must hold the alpha prompt");
        assert!(!text.contains("prompt-BBBB"), "alpha log must never see the beta prompt");
    }
    for log in &logs_b {
        let text = std::fs::read_to_string(log).expect("read beta log");
        assert!(text.contains("prompt-BBBB"), "beta log must hold the beta prompt");
        assert!(!text.contains("prompt-AAAA"), "beta log must never see the alpha prompt");
    }
    let ids_a = top_level_session_ids(&profile_data_dir(home.path(), "alpha"));
    let ids_b = top_level_session_ids(&profile_data_dir(home.path(), "beta"));
    assert!(ids_a.intersection(&ids_b).next().is_none(), "profiles must share no sessions");
    sentinel.verify_untouched();
}

#[test]
fn same_uuid_keeps_isolated_transcripts_across_profiles() {
    let Some(native) = require_native("same_uuid_keeps_isolated_transcripts") else { return };
    let sentinel = RealStoreSentinel::capture();
    let home = tempdir().expect("home tempdir");
    let home_arg = home.path().to_str().expect("home path").to_owned();
    let path_env = path_with_native(&native);
    for profile in ["alpha", "beta"] {
        cargo_bin()
            .args(["muse", "profile", "add", profile, "--home", &home_arg])
            .assert()
            .success();
    }

    let shared = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee";
    si_muse_run(
        &home_arg,
        &path_env,
        "alpha",
        &["exec", "--provider", "echo", "--session-id", shared, "alpha-sees-only-AAAA"],
    );
    si_muse_run(
        &home_arg,
        &path_env,
        "beta",
        &["exec", "--provider", "echo", "--session-id", shared, "beta-sees-only-BBBB"],
    );

    for (profile, own, foreign) in [
        ("alpha", "alpha-sees-only-AAAA", "beta-sees-only-BBBB"),
        ("beta", "beta-sees-only-BBBB", "alpha-sees-only-AAAA"),
    ] {
        let logs = session_logs(&profile_data_dir(home.path(), profile));
        let mine: Vec<&PathBuf> = logs
            .iter()
            .filter(|log| {
                log.parent().and_then(|p| p.file_name()).and_then(|n| n.to_str()) == Some(shared)
            })
            .collect();
        assert_eq!(mine.len(), 1, "{profile} must hold exactly the shared session log");
        let text = std::fs::read_to_string(mine[0]).expect("read session log");
        assert!(text.contains(own), "{profile} transcript must hold its own prompt");
        assert!(!text.contains(foreign), "{profile} transcript must never see the other prompt");
    }
    sentinel.verify_untouched();
}

#[test]
fn respawn_refuses_foreign_session_id_but_attempts_known_one() {
    let Some(native) = require_native("respawn_refuses_foreign_session_id") else { return };
    let sentinel = RealStoreSentinel::capture();
    let home = tempdir().expect("home tempdir");
    let _guard = TmuxGuard { home: home.path() };
    let home_arg = home.path().to_str().expect("home path").to_owned();
    let workspace = home.path().join("workspace");
    std::fs::create_dir_all(&workspace).expect("mkdir workspace");
    let path_env = path_with_native(&native);
    for profile in ["alpha", "beta"] {
        cargo_bin()
            .args(["muse", "profile", "add", profile, "--home", &home_arg])
            .assert()
            .success();
    }

    // One real echo session per profile; collect their UUIDs.
    si_muse_run(&home_arg, &path_env, "alpha", &["exec", "--provider", "echo", "seed-alpha"]);
    si_muse_run(&home_arg, &path_env, "beta", &["exec", "--provider", "echo", "seed-beta"]);
    let own = top_level_session_ids(&profile_data_dir(home.path(), "alpha"))
        .into_iter()
        .next()
        .expect("alpha session");
    let foreign = top_level_session_ids(&profile_data_dir(home.path(), "beta"))
        .into_iter()
        .next()
        .expect("beta session");
    assert_ne!(own, foreign);

    // A blocking worker: `serve` waits for an MSP client on stdio, which a
    // pty never EOFs — no login, no network, no model.
    cargo_bin()
        .env("PATH", &path_env)
        .env("MUSE_NO_AUTO_UPDATE", "1")
        .args([
            "muse",
            "spawn",
            "--profile",
            "alpha",
            "--home",
            &home_arg,
            "--workspace",
            workspace.to_str().expect("workspace"),
            "--",
            "serve",
        ])
        .assert()
        .success();

    // Respawn restarts a stopped worker: stop the serve worker first.
    cargo_bin()
        .env("PATH", &path_env)
        .args(["muse", "stop", "--profile", "alpha", "--home", &home_arg])
        .assert()
        .success();

    // Foreign id: SI refuses before any native resume starts.
    let refused = cargo_bin()
        .env("PATH", &path_env)
        .env("MUSE_NO_AUTO_UPDATE", "1")
        .args([
            "muse",
            "respawn",
            "--profile",
            "alpha",
            "--home",
            &home_arg,
            "--resume-id",
            &foreign,
        ])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    let refused = String::from_utf8(refused).expect("utf8 stderr");
    assert!(
        refused.contains("not known") && refused.contains("refusing"),
        "respawn must refuse the foreign session id, got: {refused}"
    );

    // Known id: SI resumes the worker on the recorded session; the pane
    // stays up under the owned socket like any other worker.
    cargo_bin()
        .env("PATH", &path_env)
        .env("MUSE_NO_AUTO_UPDATE", "1")
        .args(["muse", "respawn", "--profile", "alpha", "--home", &home_arg, "--resume-id", &own])
        .assert()
        .success();

    cargo_bin()
        .env("PATH", &path_env)
        .args(["muse", "remove", "--profile", "alpha", "--home", &home_arg, "--stop"])
        .assert()
        .success();
    kill_owned_tmux_server(home.path());
    sentinel.verify_untouched();
}
