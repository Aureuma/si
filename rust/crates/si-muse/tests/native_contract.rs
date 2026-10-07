//! Installed-native contract tests for the Muse adapter.
//!
//! These run the REAL `muse` binary (resolved from `MUSE_TEST_NATIVE` or
//! PATH) against isolated temp XDG trees with FAKE keys only, and pin the
//! destination behavior SI's isolation relies on: `auth set` writes
//! `$XDG_CONFIG_HOME/muse/auth.json` (ignoring a divergent `MUSE_AUTH_PATH`),
//! `logout` empties that store (ignoring a divergent `MUSE_AUTH_PATH`),
//! unapproved `login` writes nothing, and `exec --provider echo` runs
//! headless with sessions under the isolated data dir.
//!
//! Tests SKIP (pass with a notice) when no native is installed, so ordinary
//! CI without the Muse CLI stays green. `MUSE_NO_AUTO_UPDATE=1` is set on
//! every invocation. Nothing here touches the real `~/.config/muse` store:
//! `XDG_CONFIG_HOME` always points at a temp dir.

use std::io::Write;
use std::path::PathBuf;
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

fn resolve_native() -> Option<PathBuf> {
    if let Some(explicit) = std::env::var_os("MUSE_TEST_NATIVE") {
        let path = PathBuf::from(explicit);
        if path.is_file() {
            return Some(path);
        }
        eprintln!("MUSE_TEST_NATIVE does not point at a file; skipping native-contract tests");
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

struct XdgTree {
    _temp: tempfile::TempDir,
    config: PathBuf,
    data: PathBuf,
    cache: PathBuf,
    state: PathBuf,
}

impl XdgTree {
    fn fresh() -> Self {
        let temp = tempfile::tempdir().expect("tempdir");
        let config = temp.path().join("config");
        let data = temp.path().join("data");
        let cache = temp.path().join("cache");
        let state = temp.path().join("state");
        for dir in [&config, &data, &cache, &state] {
            std::fs::create_dir_all(dir).expect("mkdir xdg dir");
        }
        Self { _temp: temp, config, data, cache, state }
    }

    fn apply_to(&self, command: &mut Command, auth_path: Option<&std::path::Path>) {
        command
            .env("MUSE_NO_AUTO_UPDATE", "1")
            .env("XDG_CONFIG_HOME", &self.config)
            .env("XDG_DATA_HOME", &self.data)
            .env("XDG_CACHE_HOME", &self.cache)
            .env("XDG_STATE_HOME", &self.state)
            .env_remove("META_API_KEY");
        if let Some(path) = auth_path {
            command.env("MUSE_AUTH_PATH", path);
        } else {
            command.env_remove("MUSE_AUTH_PATH");
        }
    }
}

fn auth_set(program: &std::path::Path, tree: &XdgTree, auth_path: Option<&PathBuf>) -> bool {
    let mut command = Command::new(program);
    tree.apply_to(&mut command, auth_path.map(PathBuf::as_path));
    command.args(["auth", "set", "--provider", "meta", "--api-key-stdin"]);
    command.stdin(Stdio::piped()).stdout(Stdio::piped()).stderr(Stdio::piped());
    let mut child = command.spawn().expect("spawn auth set");
    child
        .stdin
        .as_mut()
        .expect("stdin")
        .write_all(b"fake-key-not-a-credential")
        .expect("write fake key");
    child.wait().expect("wait auth set").success()
}

#[test]
fn version_output_parses_and_records_tested_release() {
    let Some(program) = require_native("version_output_parses") else { return };
    let output = Command::new(&program).arg("--version").output().expect("run --version");
    assert!(output.status.success(), "native --version must exit 0");
    let line =
        String::from_utf8_lossy(&output.stdout).lines().next().unwrap_or("").trim().to_owned();
    assert!(!line.is_empty(), "native --version must print a version line");
    eprintln!("installed native version: {line}");
    if line != si_muse::adapter::MUSE_TESTED_VERSION {
        eprintln!(
            "WARNING: installed native differs from tested {} — destination checks below re-verify the contract live",
            si_muse::adapter::MUSE_TESTED_VERSION
        );
    }
}

#[test]
fn auth_set_writes_xdg_path_and_ignores_divergent_auth_path() {
    let Some(program) = require_native("auth_set_destination") else { return };
    let tree = XdgTree::fresh();
    let elsewhere = tree._temp.path().join("elsewhere").join("auth.json");
    std::fs::create_dir_all(elsewhere.parent().expect("parent")).expect("mkdir elsewhere");
    assert!(auth_set(&program, &tree, Some(&elsewhere)), "auth set must exit 0");
    let auth_path = tree.config.join("muse").join("auth.json");
    assert!(auth_path.is_file(), "auth set must write $XDG_CONFIG_HOME/muse/auth.json");
    assert!(!elsewhere.is_file(), "auth set must ignore a divergent MUSE_AUTH_PATH");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mode = std::fs::metadata(&auth_path).expect("stat").permissions().mode() & 0o777;
        assert_eq!(mode, 0o600, "auth file must be owner-only");
    }
    let raw = std::fs::read_to_string(&auth_path).expect("read auth file");
    assert!(raw.contains("\"meta\""), "auth file must carry the meta provider entry");
}

#[test]
fn logout_empties_xdg_store_and_ignores_divergent_auth_path() {
    let Some(program) = require_native("logout_destination") else { return };
    let tree = XdgTree::fresh();
    // Case 1: credential at the XDG path is removed.
    assert!(auth_set(&program, &tree, None), "auth set must exit 0");
    let auth_path = tree.config.join("muse").join("auth.json");
    assert!(auth_path.is_file());
    let mut command = Command::new(&program);
    tree.apply_to(&mut command, None);
    let output = command.arg("logout").output().expect("run logout");
    assert!(output.status.success(), "logout must exit 0");
    let raw = std::fs::read_to_string(&auth_path).expect("read auth file after logout");
    assert!(!raw.contains("\"meta\""), "logout must empty the meta provider entry, got: {raw}");
    // Case 2: with a credential ONLY at a divergent MUSE_AUTH_PATH, logout
    // reports nothing to remove and leaves that file alone.
    let monly = tree._temp.path().join("monly").join("auth.json");
    std::fs::create_dir_all(monly.parent().expect("parent")).expect("mkdir monly");
    assert!(auth_set(&program, &tree, None), "auth set must exit 0");
    std::fs::copy(&auth_path, &monly).expect("copy auth aside");
    std::fs::remove_file(&auth_path).expect("remove xdg auth");
    let mut command = Command::new(&program);
    tree.apply_to(&mut command, Some(&monly));
    let output = command.arg("logout").output().expect("run logout");
    assert!(output.status.success(), "logout must exit 0");
    assert!(monly.is_file(), "logout must not touch a divergent MUSE_AUTH_PATH file");
    assert!(!auth_path.is_file(), "logout must not create an XDG auth file");
}

#[test]
fn login_without_approval_writes_no_credentials() {
    let Some(program) = require_native("login_no_write_without_approval") else { return };
    let tree = XdgTree::fresh();
    let auth_path = tree.config.join("muse").join("auth.json");
    let mut command = Command::new(&program);
    tree.apply_to(&mut command, None);
    command.arg("login").stdin(Stdio::null()).stdout(Stdio::piped()).stderr(Stdio::piped());
    let mut child = command.spawn().expect("spawn login");
    // Wait for approval that never comes, then stop the attempt. Both an
    // approval wait (killed by us) and a fast network failure prove the
    // invariant: no approval, no credential write.
    let deadline = Instant::now() + Duration::from_secs(25);
    let status = loop {
        match child.try_wait().expect("poll login") {
            Some(status) => break Some(status),
            None if Instant::now() >= deadline => break None,
            None => std::thread::sleep(Duration::from_millis(100)),
        }
    };
    if status.is_none() {
        child.kill().expect("kill login");
        child.wait().expect("reap login");
    }
    assert!(
        !auth_path.is_file(),
        "login without approval must not write credentials anywhere in the profile store"
    );
}

#[test]
fn exec_first_headless_echo_runs_offline_with_isolated_sessions() {
    let Some(program) = require_native("exec_echo_isolation") else { return };
    let tree = XdgTree::fresh();
    let mut command = Command::new(&program);
    tree.apply_to(&mut command, None);
    // `exec` first: a root-only option before `exec` can route the
    // invocation into TUI parsing instead of headless execution.
    let output =
        command.args(["exec", "--provider", "echo", "hello isolation"]).output().expect("run echo");
    assert!(output.status.success(), "echo exec must exit 0");
    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(stdout.contains("hello isolation"), "echo must repeat the prompt, got: {stdout}");
    let sessions = tree.data.join("muse").join("sessions");
    assert!(sessions.is_dir(), "echo sessions must land under the isolated data dir");
    let mut logs = Vec::new();
    let mut stack = vec![sessions];
    while let Some(dir) = stack.pop() {
        for entry in std::fs::read_dir(&dir).expect("read sessions").flatten() {
            if entry.file_type().map(|kind| kind.is_dir()).unwrap_or(false) {
                stack.push(entry.path());
            } else if entry.file_name().to_string_lossy() == "session.jsonl" {
                logs.push(entry.path());
            }
        }
    }
    assert!(!logs.is_empty(), "echo run must persist a session log under the isolated data dir");
}
