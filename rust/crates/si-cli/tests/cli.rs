use assert_cmd::Command;
use base64::{Engine as _, engine::general_purpose::URL_SAFE_NO_PAD};
use chrono::Utc;
use serde_json::{Value, json};
use si_fort::{SessionState, classify_persisted_session_state, load_persisted_session_state};
use std::fs;
use std::path::{Path, PathBuf};
use tempfile::tempdir;

fn cargo_bin() -> Command {
    Command::cargo_bin("si").expect("si binary should build")
}

#[allow(clippy::result_large_err)]
fn fake_jwt(payload: Value) -> String {
    format!(
        "header.{}.signature",
        URL_SAFE_NO_PAD.encode(serde_json::to_vec(&payload).expect("serialize jwt payload"))
    )
}

fn write_codex_auth_file(path: &Path, email: &str) {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).expect("mkdir auth dir");
    }
    let access_token = fake_jwt(json!({
        "https://api.openai.com/profile": {
            "email": email,
        }
    }));
    let id_token = fake_jwt(json!({
        "email": email,
    }));
    fs::write(
        path,
        serde_json::to_vec_pretty(&json!({
            "tokens": {
                "access_token": access_token,
                "id_token": id_token,
            }
        }))
        .expect("serialize auth json"),
    )
    .expect("write auth json");
}

fn shell_escape_for_test(path: &Path) -> String {
    format!("'{}'", path.display().to_string().replace('\'', "'\"'\"'"))
}

fn write_executable_shell_script(path: &Path, body: &str) {
    fs::write(path, body).expect("write shell script");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(path).expect("stat shell script").permissions();
        perms.set_mode(0o755);
        fs::set_permissions(path, perms).expect("chmod shell script");
    }
}

/// Fixture native `codex`: instant `--version` (spawn probes it), `-c`
/// prefix tolerance, then `behavior` shell for real invocations. Records
/// nothing secret; tests assert env/argv from `$DUMP` when set.
fn write_fixture_codex_bin(dir: &Path, behavior: &str) -> PathBuf {
    let path = dir.join("codex");
    write_executable_shell_script(
        &path,
        &format!(
            "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'codex-fixture 0.0.0'; exit 0; fi\nif [ \"$1\" = \"-c\" ]; then shift 2; fi\n{behavior}\n"
        ),
    );
    path
}

/// Blocking fixture native: dumps env/argv, prints a marker, sleeps 300s.
/// Stop interrupts it with SIGINT/C-c like a real TUI.
fn write_blocking_fixture_codex_bin(dir: &Path, marker: &str) -> PathBuf {
    write_fixture_codex_bin(
        dir,
        &format!(
            "if [ -n \"$DUMP\" ]; then echo \"NATIVE_ARGV=$*\" >> \"$DUMP\"; echo \"NATIVE_HOME=$CODEX_HOME\" >> \"$DUMP\"; echo \"NATIVE_API_KEY=${{OPENAI_API_KEY:-missing}}\" >> \"$DUMP\"; echo \"NATIVE_PWD=$PWD\" >> \"$DUMP\"; fi\necho '{marker}'\nexec sleep 300\n"
        ),
    )
}

/// PATH env with `dir` first.
fn path_with(dir: &Path) -> String {
    format!("{}:{}", dir.display(), std::env::var("PATH").unwrap_or_default())
}

/// Build raw `tmux` with optional extra env. Always scrubs `TMUX`: when tests
/// run inside a tmux pane that variable points at the developer's live server
/// and takes precedence over `TMUX_TMPDIR`, so a bare `tmux ...` would address
/// the developer's sessions instead of the test's (on 2026-10-07 a test's
/// `tmux kill-server` destroyed the developer's live sessions exactly this
/// way). Prefer an explicit `-S <owned socket>` in `args` regardless.
fn tmux_command(env: &[(&str, &Path)], args: &[&str]) -> std::process::Command {
    let mut command = std::process::Command::new("tmux");
    for (key, value) in env {
        command.env(key, value);
    }
    command.env_remove("TMUX");
    command.args(args);
    command
}

/// Run raw `tmux` with optional extra env; panic unless it succeeds.
fn tmux_ok(env: &[(&str, &Path)], args: &[&str]) {
    let output = tmux_command(env, args).output().expect("run tmux");
    assert!(
        output.status.success(),
        "tmux {} failed: {}",
        args.join(" "),
        String::from_utf8_lossy(&output.stderr)
    );
}

/// Kill the tmux server on the owned socket for `home`. Best-effort:
/// servers of finished tests may already be gone.
fn kill_owned_tmux_server(home: &Path) {
    let socket = home.join(".si").join("runtime").join("tmux.sock");
    let _ = std::process::Command::new("tmux")
        .args(["-S", &socket.display().to_string(), "kill-server"])
        .output();
}

/// Panic-safe owned-server cleanup: kills the socket server on drop so
/// failed assertions cannot leak `sleep` workers into later tests.
struct TmuxGuard<'a> {
    home: &'a Path,
}

impl Drop for TmuxGuard<'_> {
    fn drop(&mut self) {
        kill_owned_tmux_server(self.home);
    }
}

/// Whether the owned socket currently has a live server.
fn owned_tmux_server_alive(home: &Path) -> bool {
    let socket = home.join(".si").join("runtime").join("tmux.sock");
    std::process::Command::new("tmux")
        .args(["-S", &socket.display().to_string(), "ls"])
        .output()
        .map(|output| output.status.success())
        .unwrap_or(false)
}

/// Write a new-shape worker record for reconciliation tests.
fn write_worker_record_for_test(
    home: &Path,
    profile: &str,
    slot: &str,
    state: &str,
    session: &str,
    workspace: &Path,
) {
    let root = home.join(".si");
    let record = json!({
        "schema_version": 1,
        "provider": "codex",
        "profile": profile,
        "slot": slot,
        "generation": format!("test-{profile}-{slot}"),
        "owner": format!("test-{profile}-{slot}"),
        "native_root": root.join("codex").join("profiles").join(profile).display().to_string(),
        "program": "/tmp/fixture-codex",
        "program_version": "codex-fixture 0.0.0",
        "workspace": workspace.display().to_string(),
        "workdir": workspace.display().to_string(),
        "socket": root.join("runtime").join("tmux.sock").display().to_string(),
        "session_name": session,
        "pane_id": None::<String>,
        "native_pid": None::<u32>,
        "native_start_time": None::<u64>,
        "state": state,
        "exit_code": None::<i32>,
        "exit_reason": None::<String>,
        "started_at": None::<String>,
        "ended_at": None::<String>,
        "contract": "test",
        "resume_id": None::<String>,
        "native_session_id": None::<String>,
    });
    let path = root.join("codex").join("workers").join(profile).join(format!("{slot}.json"));
    fs::create_dir_all(path.parent().expect("record parent")).expect("mkdir record dir");
    fs::write(&path, serde_json::to_vec_pretty(&record).expect("encode record"))
        .expect("write record");
}

#[test]
fn fort_wrapper_forwards_native_command_with_si_settings_defaults() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    fs::write(bootstrap_dir.join("admin.token"), "bootstrap-token\n").expect("write admin token");
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"https://fort.example.test\"\n",
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let env_file = bin_dir.path().join("fort-env.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\nprintf 'FORT_HOST=%s\\nFORT_BOOTSTRAP_TOKEN_FILE=%s\\nFORT_TOKEN=%s\\nFORT_REFRESH_TOKEN=%s\\nFORT_TOKEN_PATH=%s\\nFORT_REFRESH_TOKEN_PATH=%s\\n' \"${{FORT_HOST:-}}\" \"${{FORT_BOOTSTRAP_TOKEN_FILE:-}}\" \"${{FORT_TOKEN:-}}\" \"${{FORT_REFRESH_TOKEN:-}}\" \"${{FORT_TOKEN_PATH:-}}\" \"${{FORT_REFRESH_TOKEN_PATH:-}}\" > {}\n",
            shell_escape_for_test(&args_file),
            shell_escape_for_test(&env_file),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "doctor"])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .env_remove("CODEX_HOME")
        .env("FORT_TOKEN", "legacy-token")
        .env("FORT_REFRESH_TOKEN", "legacy-refresh-token")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert_eq!(args, "--host\nhttps://fort.example.test\ndoctor\n");
    let env = fs::read_to_string(&env_file).expect("read fort env");
    assert!(env.contains("FORT_HOST="));
    assert!(env.contains("FORT_BOOTSTRAP_TOKEN_FILE="));
    assert!(env.contains("FORT_TOKEN="));
    assert!(env.contains("FORT_REFRESH_TOKEN="));
    assert!(env.contains("FORT_TOKEN_PATH="));
    assert!(env.contains("FORT_REFRESH_TOKEN_PATH="));
}

#[test]
fn fort_wrapper_preserves_explicit_native_flags() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    fs::write(home.path().join(".si/settings.toml"), "schema_version = 1\n")
        .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let env_file = bin_dir.path().join("fort-env.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\nprintf 'FORT_HOST=%s\\nFORT_TOKEN_PATH=%s\\nFORT_BOOTSTRAP_TOKEN_FILE=%s\\n' \"${{FORT_HOST:-}}\" \"${{FORT_TOKEN_PATH:-}}\" \"${{FORT_BOOTSTRAP_TOKEN_FILE:-}}\" > {}\n",
            shell_escape_for_test(&args_file),
            shell_escape_for_test(&env_file),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "--",
            "--host",
            "https://override.example.test",
            "--token-file",
            "/tmp/runtime.token",
            "doctor",
        ])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("CODEX_HOME")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert_eq!(
        args,
        "--host\nhttps://override.example.test\n--token-file\n/tmp/runtime.token\ndoctor\n"
    );
    let env = fs::read_to_string(&env_file).expect("read fort env");
    assert!(env.contains("FORT_HOST="));
    assert!(env.contains("FORT_TOKEN_PATH="));
    assert!(env.contains("FORT_BOOTSTRAP_TOKEN_FILE="));
}

#[test]
fn fort_wrapper_refreshes_bootstrap_session_from_file_paths() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    let token_path = bootstrap_dir.join("admin.token");
    let refresh_path = bootstrap_dir.join("admin.refresh.token");
    fs::write(&token_path, "stale-admin-token\n").expect("write stale admin token");
    fs::write(&refresh_path, "stale-refresh-token\n").expect("write stale refresh token");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [&token_path, &refresh_path] {
            let mut perms = fs::metadata(path).expect("stat token file").permissions();
            perms.set_mode(0o600);
            fs::set_permissions(path, perms).expect("chmod token file");
        }
    }
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"https://fort.example.test\"\n",
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$@\" >> {args}\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--json\" ] && [ \"$4\" = \"auth\" ] && [ \"$5\" = \"session\" ] && [ \"$6\" = \"refresh\" ]; then\n  refresh_out=\"${{10}}\"\n  printf '%s\\n' 'rotated-refresh-token' > \"$refresh_out\"\n  printf '%s\\n' '{{\"access_token\":\"rotated-access-token\",\"refresh_token_file\":\"'\"$refresh_out\"'\"}}'\n  exit 0\nfi\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--token-file\" ] && [ \"$4\" = \"{token}\" ] && [ \"$5\" = \"agent\" ] && [ \"$6\" = \"list\" ]; then\n  test \"$(cat \"$4\")\" = 'rotated-access-token'\n  exit 0\nfi\nprintf 'unexpected fort invocation\\n' >&2\nexit 1\n",
            args = shell_escape_for_test(&args_file),
            token = token_path.display(),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "agent", "list"])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .env_remove("CODEX_HOME")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert!(args.contains("--json\nauth\nsession\nrefresh\n--refresh-token-file\n"));
    assert!(args.contains(&format!("--token-file\n{}\nagent\nlist\n", token_path.display())));
    assert_eq!(
        fs::read_to_string(&token_path).expect("read refreshed admin token"),
        "rotated-access-token\n"
    );
    assert_eq!(
        fs::read_to_string(&refresh_path).expect("read rotated refresh token"),
        "rotated-refresh-token\n"
    );
}

#[test]
fn fort_wrapper_ignores_caller_runtime_session_paths_for_runtime_commands() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"https://fort.example.test\"\n",
    )
    .expect("write settings");

    let runtime_dir = tempdir().expect("runtime tempdir");
    let runtime_token_path = runtime_dir.path().join("access.token");
    let runtime_refresh_path = runtime_dir.path().join("refresh.token");
    fs::write(&runtime_token_path, "stale-runtime-token\n").expect("write runtime token");
    fs::write(&runtime_refresh_path, "stale-runtime-refresh\n").expect("write runtime refresh");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [&runtime_token_path, &runtime_refresh_path] {
            let mut perms = fs::metadata(path).expect("stat runtime token").permissions();
            perms.set_mode(0o600);
            fs::set_permissions(path, perms).expect("chmod runtime token");
        }
    }

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!("#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\n", shell_escape_for_test(&args_file)),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    let assert = cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "list",
            "--repo",
            "safe",
            "--env",
            "dev",
        ])
        .env("PATH", path_env)
        .env("FORT_TOKEN_PATH", runtime_token_path.to_str().expect("runtime token path"))
        .env(
            "FORT_REFRESH_TOKEN_PATH",
            runtime_refresh_path.to_str().expect("runtime refresh path"),
        )
        .env_remove("FORT_HOST")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("CODEX_HOME")
        .assert()
        .failure();

    assert!(!args_file.exists(), "fort binary should not be invoked without managed runtime");
    assert_eq!(
        fs::read_to_string(&runtime_token_path).expect("read runtime token"),
        "stale-runtime-token\n"
    );
    let stderr = String::from_utf8_lossy(&assert.get_output().stderr);
    assert!(stderr.contains("Fort runtime session is required"));
    assert!(stderr.contains("si codex shell"));
}

#[test]
fn fort_wrapper_ignores_caller_runtime_session_paths_for_doctor() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"https://fort.example.test\"\n",
    )
    .expect("write settings");

    let runtime_dir = tempdir().expect("runtime tempdir");
    let runtime_token_path = runtime_dir.path().join("access.token");
    let runtime_refresh_path = runtime_dir.path().join("refresh.token");
    fs::write(&runtime_token_path, "stale-runtime-token\n").expect("write runtime token");
    fs::write(&runtime_refresh_path, "stale-runtime-refresh\n").expect("write runtime refresh");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [&runtime_token_path, &runtime_refresh_path] {
            let mut perms = fs::metadata(path).expect("stat runtime token").permissions();
            perms.set_mode(0o600);
            fs::set_permissions(path, perms).expect("chmod runtime token");
        }
    }

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$@\" >> {args}\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"doctor\" ]; then\n  exit 0\nfi\nprintf 'unexpected fort invocation\\n' >&2\nexit 1\n",
            args = shell_escape_for_test(&args_file),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "doctor"])
        .env("PATH", path_env)
        .env("FORT_TOKEN_PATH", runtime_token_path.to_str().expect("runtime token path"))
        .env(
            "FORT_REFRESH_TOKEN_PATH",
            runtime_refresh_path.to_str().expect("runtime refresh path"),
        )
        .env_remove("FORT_HOST")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("CODEX_HOME")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert_eq!(args, "--host\nhttps://fort.example.test\ndoctor\n");
    assert_eq!(
        fs::read_to_string(&runtime_token_path).expect("read runtime token"),
        "stale-runtime-token\n"
    );
}

#[test]
fn fort_wrapper_ignores_active_profile_runtime_session_outside_managed_runtime() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si/codex")).expect("mkdir codex dir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    let bootstrap_token_path = bootstrap_dir.join("admin.token");
    let bootstrap_refresh_path = bootstrap_dir.join("admin.refresh.token");
    fs::write(&bootstrap_token_path, "stale-admin-token\n").expect("write stale admin token");
    fs::write(&bootstrap_refresh_path, "stale-admin-refresh\n").expect("write stale admin refresh");
    let profile_fort_dir = home.path().join(".si/codex/profiles/profile-delta/fort");
    fs::create_dir_all(&profile_fort_dir).expect("mkdir profile fort dir");
    let profile_token_path = profile_fort_dir.join("access.token");
    let profile_refresh_path = profile_fort_dir.join("refresh.token");
    fs::write(&profile_token_path, "stale-profile-token\n").expect("write stale profile token");
    fs::write(&profile_refresh_path, "stale-profile-refresh\n")
        .expect("write stale profile refresh");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [
            &bootstrap_token_path,
            &bootstrap_refresh_path,
            &profile_token_path,
            &profile_refresh_path,
        ] {
            let mut perms = fs::metadata(path).expect("stat token file").permissions();
            perms.set_mode(0o600);
            fs::set_permissions(path, perms).expect("chmod token file");
        }
    }
    fs::write(
        home.path().join(".si/settings.toml"),
        format!(
            "schema_version = 1\n[paths]\ncodex_profiles_dir = {:?}\n[fort]\nhost = \"https://fort.example.test\"\n[codex]\nprofile = \"profile-delta\"\n[codex.profiles]\nactive = \"profile-delta\"\n[codex.profiles.entries.profile-delta]\nname = \"Profile Delta\"\n",
            home.path().join(".si/codex/profiles")
        ),
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!("#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\n", shell_escape_for_test(&args_file)),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    let assert = cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "list",
            "--repo",
            "safe",
            "--env",
            "dev",
        ])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .env_remove("CODEX_HOME")
        .assert()
        .failure();

    assert!(!args_file.exists(), "fort binary should not be invoked without CODEX_HOME");
    assert_eq!(
        fs::read_to_string(&profile_token_path).expect("read profile token"),
        "stale-profile-token\n"
    );
    assert_eq!(
        fs::read_to_string(&profile_refresh_path).expect("read profile refresh"),
        "stale-profile-refresh\n"
    );
    let stderr = String::from_utf8_lossy(&assert.get_output().stderr);
    assert!(stderr.contains("Fort runtime session is required"));
    assert!(stderr.contains("si codex shell"));
}

#[test]
fn fort_wrapper_fails_loudly_when_codex_home_runtime_refresh_fails() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si/codex")).expect("mkdir codex dir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    let bootstrap_token_path = bootstrap_dir.join("admin.token");
    let bootstrap_refresh_path = bootstrap_dir.join("admin.refresh.token");
    fs::write(&bootstrap_token_path, "stale-admin-token\n").expect("write stale admin token");
    fs::write(&bootstrap_refresh_path, "stale-admin-refresh\n").expect("write stale admin refresh");
    let profile_home = home.path().join(".si/codex/profiles/profile-delta");
    let profile_fort_dir = profile_home.join("fort");
    fs::create_dir_all(&profile_fort_dir).expect("mkdir profile fort dir");
    let profile_token_path = profile_fort_dir.join("access.token");
    let profile_refresh_path = profile_fort_dir.join("refresh.token");
    let profile_session_path = profile_fort_dir.join("session.json");
    fs::write(&profile_token_path, "stale-profile-token\n").expect("write stale profile token");
    fs::write(&profile_refresh_path, "stale-profile-refresh\n")
        .expect("write stale profile refresh");
    fs::write(
        &profile_session_path,
        serde_json::to_vec_pretty(&json!({
            "profile_id": "profile-delta",
            "agent_id": "si-codex-profile-delta",
            "session_id": "fort-session-profile-delta",
            "host": "https://fort.example.test",
            "runtime_host": "https://fort.example.test",
            "access_token_path": profile_token_path,
            "refresh_token_path": profile_refresh_path,
            "access_expires_at": (Utc::now() - chrono::Duration::hours(1)).to_rfc3339(),
            "refresh_expires_at": (Utc::now() + chrono::Duration::days(30)).to_rfc3339(),
            "updated_at": Utc::now().to_rfc3339(),
        }))
        .expect("serialize fort session"),
    )
    .expect("write fort session state");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [
            &bootstrap_token_path,
            &bootstrap_refresh_path,
            &profile_token_path,
            &profile_refresh_path,
            &profile_session_path,
        ] {
            let mut perms = fs::metadata(path).expect("stat token file").permissions();
            perms.set_mode(0o600);
            fs::set_permissions(path, perms).expect("chmod token file");
        }
    }
    fs::write(
        home.path().join(".si/settings.toml"),
        format!(
            "schema_version = 1\n[paths]\ncodex_profiles_dir = {:?}\n[fort]\nhost = \"https://fort.example.test\"\n[codex]\nprofile = \"profile-delta\"\n[codex.profiles]\nactive = \"profile-delta\"\n[codex.profiles.entries.profile-delta]\nname = \"Profile Delta\"\n",
            home.path().join(".si/codex/profiles")
        ),
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$@\" >> {args}\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--json\" ] && [ \"$4\" = \"auth\" ] && [ \"$5\" = \"session\" ] && [ \"$6\" = \"refresh\" ] && [ \"$8\" = \"{profile_refresh}\" ]; then\n  printf '%s\\n' 'fort request failed (status=401): unauthorized' >&2\n  exit 1\nfi\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--json\" ] && [ \"$4\" = \"auth\" ] && [ \"$5\" = \"session\" ] && [ \"$6\" = \"refresh\" ] && [ \"$8\" = \"{bootstrap_refresh}\" ]; then\n  refresh_out=\"${{10}}\"\n  printf '%s\\n' 'rotated-bootstrap-refresh' > \"$refresh_out\"\n  printf '%s\\n' '{{\"access_token\":\"rotated-bootstrap-access\",\"refresh_token_file\":\"'\"$refresh_out\"'\"}}'\n  exit 0\nfi\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--token-file\" ] && [ \"$4\" = \"{bootstrap_token}\" ] && [ \"$5\" = \"list\" ] && [ \"$6\" = \"--repo\" ] && [ \"$7\" = \"safe\" ] && [ \"$8\" = \"--env\" ] && [ \"$9\" = \"dev\" ]; then\n  test \"$(cat \"$4\")\" = 'rotated-bootstrap-access'\n  exit 0\nfi\nprintf 'unexpected fort invocation\\n' >&2\nexit 1\n",
            args = shell_escape_for_test(&args_file),
            profile_refresh = profile_refresh_path.display(),
            bootstrap_refresh = bootstrap_refresh_path.display(),
            bootstrap_token = bootstrap_token_path.display(),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    let assert = cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "list",
            "--repo",
            "safe",
            "--env",
            "dev",
        ])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .env("CODEX_HOME", &profile_home)
        .assert()
        .failure();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert!(
        args.contains(&profile_refresh_path.display().to_string()),
        "fort args did not include profile refresh path; args={args}"
    );
    assert!(args.contains("auth\nsession\nrefresh"), "fort args did not refresh; args={args}");
    assert!(!args.contains(&bootstrap_refresh_path.display().to_string()));
    assert!(!args.contains(&bootstrap_token_path.display().to_string()));
    let stderr = String::from_utf8_lossy(&assert.get_output().stderr);
    assert!(stderr.contains("refresh fort session"));
    assert!(stderr.contains("unauthorized"));
    assert_eq!(
        fs::read_to_string(&bootstrap_token_path).expect("read refreshed bootstrap token"),
        "stale-admin-token\n"
    );
    assert_eq!(
        fs::read_to_string(&bootstrap_refresh_path).expect("read rotated bootstrap refresh"),
        "stale-admin-refresh\n"
    );
    let persisted = load_persisted_session_state(&profile_session_path)
        .expect("load revoked profile session state");
    assert!(persisted.session_id.trim().is_empty());
    match classify_persisted_session_state(&persisted, Utc::now().timestamp())
        .expect("classify revoked profile session state")
    {
        SessionState::Revoked { .. } => {}
        other => panic!("expected revoked profile session state, got {other:?}"),
    }
}

#[test]
fn fort_wrapper_does_not_fall_back_to_bootstrap_when_codex_home_session_is_missing() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si/fort/bootstrap")).expect("mkdir fort bootstrap");
    fs::write(home.path().join(".si/fort/bootstrap/admin.token"), "bootstrap-token\n")
        .expect("write bootstrap token");
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"https://fort.example.test\"\n",
    )
    .expect("write settings");

    let codex_home = home.path().join(".si/codex/profiles/profile-epsilon");
    fs::create_dir_all(&codex_home).expect("mkdir codex home");
    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\nexit 0\n",
            shell_escape_for_test(&args_file),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    let assert = cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "list",
            "--repo",
            "safe",
            "--env",
            "dev",
        ])
        .env("PATH", path_env)
        .env("CODEX_HOME", &codex_home)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .assert()
        .failure();

    assert!(
        !args_file.exists(),
        "fort binary should not be invoked after missing CODEX_HOME session"
    );
    let stderr = String::from_utf8_lossy(&assert.get_output().stderr);
    assert!(stderr.contains("Fort runtime session is required for CODEX_HOME"));
    assert!(stderr.contains(codex_home.join("fort").display().to_string().as_str()));
}

#[test]
fn fort_wrapper_rejects_profile_refresh_token_rotation_to_noncanonical_path() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si/codex/profiles/profile-zeta/fort"))
        .expect("mkdir profile fort dir");
    fs::write(
        home.path().join(".si/settings.toml"),
        format!(
            "schema_version = 1\n[paths]\ncodex_profiles_dir = {:?}\n[fort]\nhost = \"https://fort.example.test\"\n",
            home.path().join(".si/codex/profiles")
        ),
    )
    .expect("write settings");
    let refresh_path = home.path().join(".si/codex/profiles/profile-zeta/fort/refresh.token");
    fs::write(&refresh_path, "profile-refresh-token\n").expect("write profile refresh token");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&refresh_path).expect("stat refresh token").permissions();
        perms.set_mode(0o600);
        fs::set_permissions(&refresh_path, perms).expect("chmod refresh token");
    }

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!("#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\n", shell_escape_for_test(&args_file)),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    let out_path = home.path().join("detached-refresh.token");

    let assert = cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "auth",
            "session",
            "refresh",
            "--refresh-token-file",
            refresh_path.to_str().expect("refresh path"),
            "--refresh-token-out",
            out_path.to_str().expect("out path"),
        ])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .env_remove("CODEX_HOME")
        .assert()
        .failure();

    assert!(!args_file.exists(), "fort binary should not be invoked after guard failure");
    assert!(!out_path.exists(), "guard must not write a detached refresh token");
    let stderr = String::from_utf8_lossy(&assert.get_output().stderr);
    assert!(stderr.contains("refusing to rotate Codex profile Fort refresh token"));
    assert!(stderr.contains("refreshed in place"));
}

#[test]
fn fort_wrapper_rejects_nonprimary_profile_refresh_token_rotation_to_noncanonical_path() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si/codex/profiles/profile-zeta/workers/review/fort"))
        .expect("mkdir worker fort dir");
    fs::write(
        home.path().join(".si/settings.toml"),
        format!(
            "schema_version = 1\n[paths]\ncodex_profiles_dir = {:?}\n[fort]\nhost = \"https://fort.example.test\"\n",
            home.path().join(".si/codex/profiles")
        ),
    )
    .expect("write settings");
    let refresh_path =
        home.path().join(".si/codex/profiles/profile-zeta/workers/review/fort/refresh.token");
    fs::write(&refresh_path, "profile-refresh-token\n").expect("write profile refresh token");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&refresh_path).expect("stat refresh token").permissions();
        perms.set_mode(0o600);
        fs::set_permissions(&refresh_path, perms).expect("chmod refresh token");
    }

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!("#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\n", shell_escape_for_test(&args_file)),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());
    let out_path = home.path().join("detached-refresh.token");

    let assert = cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "auth",
            "session",
            "refresh",
            "--refresh-token-file",
            refresh_path.to_str().expect("refresh path"),
            "--refresh-token-out",
            out_path.to_str().expect("out path"),
        ])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .env_remove("CODEX_HOME")
        .assert()
        .failure();

    assert!(!args_file.exists(), "fort binary should not be invoked after guard failure");
    assert!(!out_path.exists(), "guard must not write a detached refresh token");
    let stderr = String::from_utf8_lossy(&assert.get_output().stderr);
    assert!(stderr.contains("refusing to rotate Codex profile Fort refresh token"));
    assert!(stderr.contains("refreshed in place"));
}

#[test]
fn fort_wrapper_reuses_fresh_bootstrap_token_without_refreshing() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    let token_path = bootstrap_dir.join("admin.token");
    let refresh_path = bootstrap_dir.join("admin.refresh.token");
    let payload = format!(
        "{{\"exp\":{},\"iss\":\"fortd\",\"aud\":[\"fort-api\"]}}",
        chrono::Utc::now().timestamp() + 3600
    );
    let token = format!("header.{}.signature\n", URL_SAFE_NO_PAD.encode(payload.as_bytes()));
    fs::write(&token_path, token).expect("write fresh admin token");
    fs::write(&refresh_path, "unused-refresh-token\n").expect("write refresh token");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [&token_path, &refresh_path] {
            let mut perms = fs::metadata(path).expect("stat token file").permissions();
            perms.set_mode(0o600);
            fs::set_permissions(path, perms).expect("chmod token file");
        }
    }
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"https://fort.example.test\"\n",
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$@\" >> {args}\nif [ \"$3\" = \"--json\" ] && [ \"$4\" = \"auth\" ] && [ \"$5\" = \"session\" ] && [ \"$6\" = \"refresh\" ]; then\n  printf 'unexpected refresh\\n' >&2\n  exit 1\nfi\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--token-file\" ] && [ \"$4\" = \"{token}\" ] && [ \"$5\" = \"agent\" ] && [ \"$6\" = \"list\" ]; then\n  exit 0\nfi\nprintf 'unexpected fort invocation\\n' >&2\nexit 1\n",
            args = shell_escape_for_test(&args_file),
            token = token_path.display(),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "agent", "list"])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .env_remove("CODEX_HOME")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert!(!args.contains("--json\nauth\nsession\nrefresh\n"));
    assert!(args.contains(&format!("--token-file\n{}\nagent\nlist\n", token_path.display())));
}

#[test]
fn fort_wrapper_refreshes_bootstrap_token_before_near_expiry() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    let token_path = bootstrap_dir.join("admin.token");
    let refresh_path = bootstrap_dir.join("admin.refresh.token");
    let payload = format!(
        "{{\"exp\":{},\"iss\":\"fortd\",\"aud\":[\"fort-api\"]}}",
        chrono::Utc::now().timestamp() + 120
    );
    let token = format!("header.{}.signature\n", URL_SAFE_NO_PAD.encode(payload.as_bytes()));
    fs::write(&token_path, token).expect("write near-expiry admin token");
    fs::write(&refresh_path, "near-expiry-refresh-token\n").expect("write refresh token");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [&token_path, &refresh_path] {
            let mut perms = fs::metadata(path).expect("stat token file").permissions();
            perms.set_mode(0o600);
            fs::set_permissions(path, perms).expect("chmod token file");
        }
    }
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"https://fort.example.test\"\n",
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$@\" >> {args}\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--json\" ] && [ \"$4\" = \"auth\" ] && [ \"$5\" = \"session\" ] && [ \"$6\" = \"refresh\" ]; then\n  refresh_out=\"${{10}}\"\n  printf '%s\\n' 'rotated-refresh-token' > \"$refresh_out\"\n  printf '%s\\n' '{{\"access_token\":\"rotated-access-token\",\"refresh_token_file\":\"'\"$refresh_out\"'\"}}'\n  exit 0\nfi\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"--token-file\" ] && [ \"$4\" = \"{token}\" ] && [ \"$5\" = \"agent\" ] && [ \"$6\" = \"list\" ]; then\n  test \"$(cat \"$4\")\" = 'rotated-access-token'\n  exit 0\nfi\nprintf 'unexpected fort invocation\\n' >&2\nexit 1\n",
            args = shell_escape_for_test(&args_file),
            token = token_path.display(),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "agent", "list"])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .env_remove("CODEX_HOME")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert!(args.contains("--json\nauth\nsession\nrefresh\n--refresh-token-file\n"));
    assert_eq!(
        fs::read_to_string(&token_path).expect("read refreshed admin token"),
        "rotated-access-token\n"
    );
}

#[test]
fn fort_wrapper_does_not_refresh_stale_bootstrap_session_for_doctor() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    let token_path = bootstrap_dir.join("admin.token");
    let refresh_path = bootstrap_dir.join("admin.refresh.token");
    fs::write(&token_path, "stale-admin-token\n").expect("write stale admin token");
    fs::write(&refresh_path, "stale-refresh-token\n").expect("write stale refresh token");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [&token_path, &refresh_path] {
            let mut perms = fs::metadata(path).expect("stat token file").permissions();
            perms.set_mode(0o600);
            fs::set_permissions(path, perms).expect("chmod token file");
        }
    }
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"https://fort.example.test\"\n",
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$@\" >> {args}\nif [ \"$1\" = \"--host\" ] && [ \"$2\" = \"https://fort.example.test\" ] && [ \"$3\" = \"doctor\" ]; then\n  exit 0\nfi\nif [ \"$3\" = \"--json\" ] && [ \"$4\" = \"auth\" ] && [ \"$5\" = \"session\" ] && [ \"$6\" = \"refresh\" ]; then\n  printf 'unexpected refresh\\n' >&2\n  exit 1\nfi\nprintf 'unexpected fort invocation\\n' >&2\nexit 1\n",
            args = shell_escape_for_test(&args_file),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "doctor"])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .env_remove("CODEX_HOME")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert!(!args.contains("--json\nauth\nsession\nrefresh\n"));
    assert!(!args.contains("--token-file\n"));
    assert_eq!(args, "--host\nhttps://fort.example.test\ndoctor\n");
}

#[test]
fn fort_wrapper_doctor_fails_when_codex_home_session_is_missing() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si/fort/bootstrap")).expect("mkdir fort bootstrap");
    fs::write(home.path().join(".si/fort/bootstrap/admin.token"), "bootstrap-token\n")
        .expect("write bootstrap token");
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"https://fort.example.test\"\n",
    )
    .expect("write settings");

    let codex_home = home.path().join(".si/codex/profiles/profile-epsilon");
    fs::create_dir_all(&codex_home).expect("mkdir codex home");
    let bin_dir = tempdir().expect("bin tempdir");
    let args_file = bin_dir.path().join("fort-args.txt");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(
        &fort_path,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\nexit 0\n",
            shell_escape_for_test(&args_file),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    let assert = cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "doctor"])
        .env("PATH", path_env)
        .env("CODEX_HOME", &codex_home)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("FORT_REFRESH_TOKEN_PATH")
        .assert()
        .failure();

    assert!(
        !args_file.exists(),
        "fort binary should not be invoked after missing CODEX_HOME doctor session"
    );
    let stderr = String::from_utf8_lossy(&assert.get_output().stderr);
    assert!(stderr.contains("Fort runtime session is required for CODEX_HOME"));
    assert!(stderr.contains(codex_home.join("fort").display().to_string().as_str()));
}

#[test]
fn fort_wrapper_builds_configured_repo_when_fort_missing_from_path() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    fs::write(bootstrap_dir.join("admin.token"), "bootstrap-token\n").expect("write admin token");
    let repo = tempdir().expect("fort repo");
    let args_file = repo.path().join("fort-args.txt");
    let env_file = repo.path().join("fort-env.txt");
    fs::write(
        home.path().join(".si/settings.toml"),
        format!(
            "schema_version = 1\n[fort]\nrepo = \"{}\"\nhost = \"https://fort.example.test\"\n",
            repo.path().display()
        ),
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let cargo_path = bin_dir.path().join("cargo");
    write_executable_shell_script(
        &cargo_path,
        &format!(
            "#!/bin/sh\nset -eu\nif [ \"$1\" != \"build\" ]; then\n  printf 'unexpected cargo command: %s\\n' \"$1\" >&2\n  exit 1\nfi\nmkdir -p \"$PWD/target/debug\"\ncat > \"$PWD/target/debug/fort\" <<'EOF'\n#!/bin/sh\nprintf '%s\\n' \"$@\" > {args}\nprintf 'FORT_HOST=%s\\nFORT_BOOTSTRAP_TOKEN_FILE=%s\\nFORT_TOKEN_PATH=%s\\n' \"${{FORT_HOST:-}}\" \"${{FORT_BOOTSTRAP_TOKEN_FILE:-}}\" \"${{FORT_TOKEN_PATH:-}}\" > {env}\nEOF\nchmod +x \"$PWD/target/debug/fort\"\n",
            args = shell_escape_for_test(&args_file),
            env = shell_escape_for_test(&env_file),
        ),
    );
    let path_env = format!("{}:/usr/bin:/bin", bin_dir.path().display());

    cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "doctor"])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("CODEX_HOME")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert_eq!(args, "--host\nhttps://fort.example.test\ndoctor\n");
    let env = fs::read_to_string(&env_file).expect("read fort env");
    assert!(env.contains("FORT_HOST="));
    assert!(env.contains("FORT_BOOTSTRAP_TOKEN_FILE="));
    assert!(env.contains("FORT_TOKEN_PATH="));
}

#[test]
fn fort_wrapper_builds_sibling_checkout_from_runtime_workspace_when_settings_repo_missing() {
    let workspace = tempdir().expect("workspace tempdir");
    let si_dir = workspace.path().join("si");
    let fort_dir = workspace.path().join("fort");
    fs::create_dir_all(&si_dir).expect("mkdir sibling si dir");
    fs::create_dir_all(&fort_dir).expect("mkdir sibling fort dir");
    fs::write(fort_dir.join("Cargo.toml"), "[package]\nname = \"fort\"\nversion = \"0.0.0\"\n")
        .expect("write sibling fort cargo manifest");

    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    fs::write(bootstrap_dir.join("admin.token"), "bootstrap-token\n").expect("write admin token");
    fs::write(home.path().join(".si/settings.toml"), "schema_version = 1\n")
        .expect("write settings");

    let args_file = fort_dir.join("fort-args.txt");
    let env_file = fort_dir.join("fort-env.txt");
    let bin_dir = tempdir().expect("bin tempdir");
    let cargo_path = bin_dir.path().join("cargo");
    write_executable_shell_script(
        &cargo_path,
        &format!(
            "#!/bin/sh\nset -eu\nif [ \"$1\" != \"build\" ]; then\n  printf 'unexpected cargo command: %s\\n' \"$1\" >&2\n  exit 1\nfi\nmkdir -p \"$PWD/target/debug\"\ncat > \"$PWD/target/debug/fort\" <<'EOF'\n#!/bin/sh\nprintf '%s\\n' \"$@\" > {args}\nprintf 'FORT_HOST=%s\\nFORT_BOOTSTRAP_TOKEN_FILE=%s\\nFORT_TOKEN_PATH=%s\\n' \"${{FORT_HOST:-}}\" \"${{FORT_BOOTSTRAP_TOKEN_FILE:-}}\" \"${{FORT_TOKEN_PATH:-}}\" > {env}\nEOF\nchmod +x \"$PWD/target/debug/fort\"\n",
            args = shell_escape_for_test(&args_file),
            env = shell_escape_for_test(&env_file),
        ),
    );
    let path_env = format!("{}:/usr/bin:/bin", bin_dir.path().display());

    cargo_bin()
        .current_dir(workspace.path())
        .args(["fort", "--home", home.path().to_str().expect("home path"), "doctor"])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("CODEX_HOME")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert_eq!(args, "doctor\n");
    let env = fs::read_to_string(&env_file).expect("read fort env");
    assert!(env.contains("FORT_HOST="));
    assert!(env.contains("FORT_BOOTSTRAP_TOKEN_FILE="));
    assert!(env.contains("FORT_TOKEN_PATH="));
}

#[test]
fn fort_wrapper_prefers_existing_sibling_binary_before_cargo_build_fallback() {
    let workspace = tempdir().expect("workspace tempdir");
    let fort_dir = workspace.path().join("fort");
    fs::create_dir_all(fort_dir.join("target/debug")).expect("mkdir fort target");
    fs::write(fort_dir.join("Cargo.toml"), "[package]\nname = \"fort\"\nversion = \"0.0.0\"\n")
        .expect("write sibling fort cargo manifest");

    let args_file = fort_dir.join("fort-args.txt");
    let env_file = fort_dir.join("fort-env.txt");
    let fort_binary = fort_dir.join("target/debug/fort");
    write_executable_shell_script(
        &fort_binary,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > {args}\nprintf 'FORT_HOST=%s\\nFORT_BOOTSTRAP_TOKEN_FILE=%s\\nFORT_TOKEN_PATH=%s\\n' \"${{FORT_HOST:-}}\" \"${{FORT_BOOTSTRAP_TOKEN_FILE:-}}\" \"${{FORT_TOKEN_PATH:-}}\" > {env}\n",
            args = shell_escape_for_test(&args_file),
            env = shell_escape_for_test(&env_file),
        ),
    );

    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    fs::write(bootstrap_dir.join("admin.token"), "bootstrap-token\n").expect("write admin token");
    fs::write(home.path().join(".si/settings.toml"), "schema_version = 1\n")
        .expect("write settings");

    cargo_bin()
        .current_dir(workspace.path())
        .args(["fort", "--home", home.path().to_str().expect("home path"), "doctor"])
        .env("PATH", "/usr/bin:/bin")
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("CODEX_HOME")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert_eq!(args, "doctor\n");
    let env = fs::read_to_string(&env_file).expect("read fort env");
    assert!(env.contains("FORT_HOST="));
    assert!(env.contains("FORT_BOOTSTRAP_TOKEN_FILE="));
    assert!(env.contains("FORT_TOKEN_PATH="));
}

#[test]
fn fort_wrapper_builds_configured_repo_from_custom_target_dir() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    let bootstrap_dir = home.path().join(".si/fort/bootstrap");
    fs::create_dir_all(&bootstrap_dir).expect("mkdir fort bootstrap");
    fs::write(bootstrap_dir.join("admin.token"), "bootstrap-token\n").expect("write admin token");
    let repo = tempdir().expect("fort repo");
    fs::create_dir_all(repo.path().join(".cargo")).expect("mkdir cargo dir");
    fs::write(
        repo.path().join(".cargo/config.toml"),
        "[build]\ntarget-dir = \".artifacts/cargo-target\"\n",
    )
    .expect("write cargo config");
    fs::create_dir_all(repo.path().join("target/debug")).expect("mkdir stale target dir");
    write_executable_shell_script(
        &repo.path().join("target/debug/fort"),
        "#!/bin/sh\nprintf 'stale default target fort binary should not be used\\n' >&2\nexit 1\n",
    );
    let args_file = repo.path().join("fort-args.txt");
    let env_file = repo.path().join("fort-env.txt");
    fs::write(
        home.path().join(".si/settings.toml"),
        format!(
            "schema_version = 1\n[fort]\nrepo = \"{}\"\nhost = \"https://fort.example.test\"\n",
            repo.path().display()
        ),
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let cargo_path = bin_dir.path().join("cargo");
    write_executable_shell_script(
        &cargo_path,
        &format!(
            "#!/bin/sh\nset -eu\nif [ \"$1\" != \"build\" ]; then\n  printf 'unexpected cargo command: %s\\n' \"$1\" >&2\n  exit 1\nfi\nmkdir -p \"$PWD/.artifacts/cargo-target/debug\"\ncat > \"$PWD/.artifacts/cargo-target/debug/fort\" <<'EOF'\n#!/bin/sh\nprintf '%s\\n' \"$@\" > {args}\nprintf 'FORT_HOST=%s\\nFORT_BOOTSTRAP_TOKEN_FILE=%s\\nFORT_TOKEN_PATH=%s\\n' \"${{FORT_HOST:-}}\" \"${{FORT_BOOTSTRAP_TOKEN_FILE:-}}\" \"${{FORT_TOKEN_PATH:-}}\" > {env}\nEOF\nchmod +x \"$PWD/.artifacts/cargo-target/debug/fort\"\n",
            args = shell_escape_for_test(&args_file),
            env = shell_escape_for_test(&env_file),
        ),
    );
    let path_env = format!("{}:/usr/bin:/bin", bin_dir.path().display());

    cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "doctor"])
        .env("PATH", path_env)
        .env_remove("FORT_HOST")
        .env_remove("FORT_TOKEN_PATH")
        .env_remove("FORT_BOOTSTRAP_TOKEN_FILE")
        .env_remove("CODEX_HOME")
        .assert()
        .success();

    let args = fs::read_to_string(&args_file).expect("read fort args");
    assert_eq!(args, "--host\nhttps://fort.example.test\ndoctor\n");
    let env = fs::read_to_string(&env_file).expect("read fort env");
    assert!(env.contains("FORT_HOST="));
    assert!(env.contains("FORT_BOOTSTRAP_TOKEN_FILE="));
    assert!(env.contains("FORT_TOKEN_PATH="));
}

#[test]
fn fort_config_set_and_show_round_trip_si_settings() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");

    cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "config",
            "set",
            "--repo",
            "/tmp/fort-repo",
            "--bin",
            "/tmp/fort-bin",
            "--build",
            "true",
            "--host",
            "https://fort.example.test",
            "--runtime-host",
            "https://fort-runtime.example.test",
        ])
        .assert()
        .success();

    let settings_source =
        fs::read_to_string(home.path().join(".si/settings.toml")).expect("read settings");
    let parsed: toml::Value = toml::from_str(&settings_source).expect("parse settings");
    assert_eq!(parsed["fort"]["repo"].as_str().expect("repo"), "/tmp/fort-repo");
    assert_eq!(parsed["fort"]["bin"].as_str().expect("bin"), "/tmp/fort-bin");
    assert!(parsed["fort"]["build"].as_bool().expect("build"));

    let output = cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "config",
            "show",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["repo"], "/tmp/fort-repo");
    assert_eq!(parsed["bin"], "/tmp/fort-bin");
    assert_eq!(parsed["build"], true);
    assert_eq!(parsed["host"], "https://fort.example.test");
    assert_eq!(parsed["runtime_host"], "https://fort-runtime.example.test");
}

#[test]
fn fort_config_set_rejects_persistent_local_hosts() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");

    let output = cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "config",
            "set",
            "--host",
            "http://127.0.0.1:8088",
        ])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    let stderr = String::from_utf8_lossy(&output);
    assert!(stderr.contains("fort.host must use https"));

    let output = cargo_bin()
        .args([
            "fort",
            "--home",
            home.path().to_str().expect("home path"),
            "config",
            "set",
            "--runtime-host",
            "https://fort.internal",
        ])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    let stderr = String::from_utf8_lossy(&output);
    assert!(stderr.contains("fort.runtime_host must resolve through a public Fort HTTPS endpoint"));
}

#[test]
fn fort_wrapper_rejects_persistent_local_host_from_settings() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir si home");
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[fort]\nhost = \"http://127.0.0.1:8088\"\n",
    )
    .expect("write settings");

    let bin_dir = tempdir().expect("bin tempdir");
    let fort_path = bin_dir.path().join("fort");
    write_executable_shell_script(&fort_path, "#!/bin/sh\nexit 0\n");
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    let output = cargo_bin()
        .args(["fort", "--home", home.path().to_str().expect("home path"), "doctor"])
        .env("PATH", path_env)
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    let stderr = String::from_utf8_lossy(&output);
    assert!(stderr.contains("fort.host must use https"));
}

#[test]
fn settings_show_honors_path_overrides() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    fs::write(
        &settings_path,
        r#"
[paths]
root = "~/state/si"
settings_file = "~/config/si/settings.toml"
codex_profiles_dir = "~/state/si/profiles"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["settings", "show", "--format", "json", "--home"])
        .arg(home.path())
        .args(["--settings-file"])
        .arg(&settings_path)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["paths"]["root"], path_string(home.path().join("state/si")));
    assert_eq!(
        parsed["paths"]["settings_file"],
        path_string(home.path().join("config/si/settings.toml"))
    );
    assert_eq!(
        parsed["paths"]["codex_profiles_dir"],
        path_string(home.path().join("state/si/profiles"))
    );
}

#[test]
fn fort_session_state_show_reads_and_normalizes_persisted_state() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(
        &state_path,
        r#"{
  "profile_id": " profile-zeta ",
  "agent_id": " agent-profile-zeta ",
  "session_id": " session-123 ",
  "host": " https://fort.example.test ",
  "runtime_host": " http://fort.internal:8088 ",
  "access_expires_at": " 2030-01-01T00:00:00Z ",
  "refresh_expires_at": " 2030-02-01T00:00:00Z "
}
"#,
    )
    .expect("write session state");
    #[cfg(unix)]
    fs::set_permissions(&state_path, std::os::unix::fs::PermissionsExt::from_mode(0o600))
        .expect("chmod session state");

    let output = cargo_bin()
        .args(["fort", "session", "show", "--path"])
        .arg(&state_path)
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["profile_id"], "profile-zeta");
    assert_eq!(parsed["agent_id"], "agent-profile-zeta");
    assert_eq!(parsed["session_id"], "session-123");
    assert_eq!(parsed["host"], "https://fort.example.test");
    assert_eq!(parsed["runtime_host"], "http://fort.internal:8088");
}

#[test]
fn fort_session_state_classify_reports_refreshing_state() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(
        &state_path,
        r#"{
  "profile_id": "profile-zeta",
  "agent_id": "agent-profile-zeta",
  "session_id": "session-123",
  "access_expires_at": "1970-01-01T00:01:30Z",
  "refresh_expires_at": "1970-01-01T00:06:40Z"
}
"#,
    )
    .expect("write session state");
    #[cfg(unix)]
    fs::set_permissions(&state_path, std::os::unix::fs::PermissionsExt::from_mode(0o600))
        .expect("chmod session state");

    let output = cargo_bin()
        .args(["fort", "session", "classify", "--path"])
        .arg(&state_path)
        .args(["--now-unix", "100", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["Refreshing"]["profile_id"], "profile-zeta");
    assert_eq!(parsed["Refreshing"]["agent_id"], "agent-profile-zeta");
    assert_eq!(parsed["Refreshing"]["session_id"], "session-123");
    assert_eq!(parsed["Refreshing"]["access_expires_at_unix"], 90);
    assert_eq!(parsed["Refreshing"]["refresh_expires_at_unix"], 400);
}

#[test]
fn fort_runtime_agent_state_show_reads_and_normalizes_persisted_state() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("runtime-agent.json");
    fs::write(
        &state_path,
        r#"{
  "profile_id": " profile-zeta ",
  "pid": 4242,
  "command_path": " /tmp/si ",
  "started_at": " 2030-01-01T00:00:00Z ",
  "updated_at": " 2030-01-01T00:00:01Z "
}
"#,
    )
    .expect("write runtime agent state");
    #[cfg(unix)]
    fs::set_permissions(&state_path, std::os::unix::fs::PermissionsExt::from_mode(0o600))
        .expect("chmod runtime agent state");

    let output = cargo_bin()
        .args(["fort", "runtime", "show", "--path"])
        .arg(&state_path)
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["profile_id"], "profile-zeta");
    assert_eq!(parsed["pid"], 4242);
    assert_eq!(parsed["command_path"], "/tmp/si");
}

#[test]
fn fort_session_state_write_persists_normalized_json() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");

    cargo_bin()
        .args([
            "fort",
            "session",
            "write",
            "--path",
        ])
        .arg(&state_path)
        .args([
            "--state-json",
            r#"{"profile_id":" profile-zeta ","agent_id":" agent-profile-zeta ","session_id":" session-123 ","host":" https://fort.example.test "}"#,
        ])
        .assert()
        .success();

    let raw = fs::read_to_string(&state_path).expect("read persisted state");
    let parsed: Value = serde_json::from_str(&raw).expect("json");
    assert_eq!(parsed["profile_id"], "profile-zeta");
    assert_eq!(parsed["agent_id"], "agent-profile-zeta");
    assert_eq!(parsed["session_id"], "session-123");
    assert_eq!(parsed["host"], "https://fort.example.test");
}

#[test]
fn fort_runtime_agent_state_write_persists_normalized_json() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("runtime-agent.json");

    cargo_bin()
        .args(["fort", "runtime", "write", "--path"])
        .arg(&state_path)
        .args([
            "--state-json",
            r#"{"profile_id":" profile-zeta ","pid":4242,"command_path":" /tmp/si "}"#,
        ])
        .assert()
        .success();

    let raw = fs::read_to_string(&state_path).expect("read persisted runtime state");
    let parsed: Value = serde_json::from_str(&raw).expect("json");
    assert_eq!(parsed["profile_id"], "profile-zeta");
    assert_eq!(parsed["pid"], 4242);
    assert_eq!(parsed["command_path"], "/tmp/si");
}

#[test]
fn fort_session_state_clear_removes_persisted_file() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(&state_path, "{}\n").expect("write session state");

    cargo_bin().args(["fort", "session", "clear", "--path"]).arg(&state_path).assert().success();

    assert!(!state_path.exists());
}

#[test]
fn fort_session_state_bootstrap_view_normalizes_fallbacks() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(
        &state_path,
        r#"{
  "profile_id": " profile-zeta ",
  "agent_id": "",
  "host": " http://127.0.0.1:8088 "
}
"#,
    )
    .expect("write session state");
    #[cfg(unix)]
    fs::set_permissions(&state_path, std::os::unix::fs::PermissionsExt::from_mode(0o600))
        .expect("chmod session state");

    let output = cargo_bin()
        .args(["fort", "session", "bootstrap", "--path"])
        .arg(&state_path)
        .args([
            "--access-token-path",
            "/tmp/access.token",
            "--refresh-token-path",
            "/tmp/refresh.token",
            "--access-token-runtime-path",
            "/home/si/.si/access.token",
            "--refresh-token-runtime-path",
            "/home/si/.si/refresh.token",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["profile_id"], "profile-zeta");
    assert_eq!(parsed["agent_id"], "si-codex-profile-zeta");
    assert_eq!(parsed["runtime_host_url"], "http://127.0.0.1:8088");
    assert_eq!(parsed["access_token_runtime_path"], "/home/si/.si/access.token");
}

#[test]
fn fort_runtime_agent_state_clear_removes_persisted_file() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("runtime-agent.json");
    fs::write(&state_path, "{}\n").expect("write runtime agent state");

    cargo_bin().args(["fort", "runtime", "clear", "--path"]).arg(&state_path).assert().success();

    assert!(!state_path.exists());
}

#[test]
fn fort_session_state_refresh_outcome_returns_updated_state_and_classification() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(
        &state_path,
        r#"{
  "profile_id": "profile-zeta",
  "agent_id": "agent-profile-zeta",
  "session_id": "session-123",
  "access_expires_at": "1970-01-01T00:01:30Z",
  "refresh_expires_at": "1970-01-01T00:06:40Z"
}
"#,
    )
    .expect("write session state");
    #[cfg(unix)]
    fs::set_permissions(&state_path, std::os::unix::fs::PermissionsExt::from_mode(0o600))
        .expect("chmod session state");

    let output = cargo_bin()
        .args(["fort", "session", "refresh", "--path"])
        .arg(&state_path)
        .args([
            "--outcome",
            "success",
            "--now-unix",
            "100",
            "--access-expires-at-unix",
            "500",
            "--refresh-expires-at-unix",
            "800",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["state"]["access_expires_at"], "1970-01-01T00:08:20Z");
    assert_eq!(parsed["state"]["refresh_expires_at"], "1970-01-01T00:13:20Z");
    assert_eq!(parsed["classification"]["state"], "resumable");
}

#[test]
fn fort_session_state_teardown_reports_closed_state() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(
        &state_path,
        r#"{
  "profile_id": "profile-zeta",
  "agent_id": "agent-profile-zeta",
  "session_id": "session-123",
  "access_expires_at": "1970-01-01T00:01:30Z",
  "refresh_expires_at": "1970-01-01T00:06:40Z"
}
"#,
    )
    .expect("write session state");
    #[cfg(unix)]
    fs::set_permissions(&state_path, std::os::unix::fs::PermissionsExt::from_mode(0o600))
        .expect("chmod session state");

    let output = cargo_bin()
        .args(["fort", "session", "teardown", "--path"])
        .arg(&state_path)
        .args(["--now-unix", "100", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["state"], "closed");
}

#[test]
fn fort_session_state_refresh_outcome_unauthorized_clears_session_id() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(
        &state_path,
        r#"{
  "profile_id": "profile-zeta",
  "agent_id": "agent-profile-zeta",
  "session_id": "session-123",
  "access_expires_at": "1970-01-01T00:01:30Z",
  "refresh_expires_at": "1970-01-01T00:06:40Z"
}
"#,
    )
    .expect("write session state");
    #[cfg(unix)]
    fs::set_permissions(&state_path, std::os::unix::fs::PermissionsExt::from_mode(0o600))
        .expect("chmod session state");

    let output = cargo_bin()
        .args(["fort", "session", "refresh", "--path"])
        .arg(&state_path)
        .args(["--outcome", "unauthorized", "--now-unix", "100", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["state"]["session_id"], "");
    assert_eq!(parsed["classification"]["state"], "revoked");
}

#[test]
fn codex_profile_swap_and_repair_auth_are_removed() {
    let home = tempdir().expect("tempdir");

    let output = cargo_bin()
        .args(["codex", "profile", "swap", "profile-gamma", "--home"])
        .arg(home.path())
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    let stderr = String::from_utf8(output).expect("utf8 stderr");
    assert!(stderr.contains("unrecognized subcommand 'swap'"));

    let output = cargo_bin()
        .args(["codex", "repair-auth", "--home"])
        .arg(home.path())
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    let stderr = String::from_utf8(output).expect("utf8 stderr");
    assert!(stderr.contains("unrecognized subcommand 'repair-auth'"));

    assert!(!home.path().join(".codex").exists());
    assert!(!home.path().join(".si").exists());
}

#[test]
fn codex_profile_list_reports_unverified_credential_without_live_probe() {
    let home = tempdir().expect("tempdir");
    cargo_bin()
        .args([
            "codex",
            "profile",
            "add",
            "america",
            "--name",
            "America",
            "--email",
            "america@example.test",
            "--home",
        ])
        .arg(home.path())
        .assert()
        .success();
    write_codex_auth_file(
        &home.path().join(".si/codex/profiles/america/auth.json"),
        "america@example.test",
    );

    // list/show must not invoke the native binary at all: the sentinel proves
    // the shim was never executed, and the state never claims a verified login.
    let bin_dir = tempdir().expect("bin tempdir");
    let sentinel = bin_dir.path().join("invoked");
    let codex_path = bin_dir.path().join("codex");
    write_executable_shell_script(
        &codex_path,
        &format!("#!/bin/sh\nset -eu\necho invoked >> {}\nexit 1\n", sentinel.display()),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    let output = cargo_bin()
        .env("PATH", path_env)
        .args([
            "codex",
            "profile",
            "list",
            "--home",
            home.path().to_str().expect("home path"),
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let profiles: Value = serde_json::from_slice(&output).expect("parse profile list");
    let profile = profiles
        .as_array()
        .expect("profile list array")
        .iter()
        .find(|profile| profile["profile"] == "america")
        .expect("america profile");
    assert_eq!(profile["auth_state"], "credential_present_unverified");
    assert_eq!(profile["email"], "america@example.test");
    assert!(profile["identity"].is_null());
    assert!(!sentinel.exists());
    assert!(!home.path().join(".codex").exists());
}

#[test]
fn codex_profile_add_list_select_show_remove_roundtrip() {
    let home = tempdir().expect("tempdir");
    let output = cargo_bin()
        .args([
            "codex",
            "profile",
            "add",
            "work",
            "--name",
            "Work",
            "--email",
            "work@example.test",
            "--home",
        ])
        .arg(home.path())
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let added: Value = serde_json::from_slice(&output).expect("parse add output");
    assert_eq!(added["profile"], "work");
    assert_eq!(added["auth_state"], "not_logged_in");
    assert_eq!(added["selected"], false);

    cargo_bin()
        .args(["codex", "profile", "add", "personal", "--home"])
        .arg(home.path())
        .assert()
        .success();

    let output = cargo_bin()
        .args(["codex", "profile", "add", "work", "--home"])
        .arg(home.path())
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    assert!(String::from_utf8(output).expect("utf8 stderr").contains("already exists"));

    cargo_bin()
        .args(["codex", "profile", "select", "work", "--home"])
        .arg(home.path())
        .assert()
        .success();

    let output = cargo_bin()
        .args(["codex", "profile", "show", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let shown: Value = serde_json::from_slice(&output).expect("parse show output");
    assert_eq!(shown["profile"], "work");
    assert_eq!(shown["selected"], true);

    cargo_bin()
        .args(["codex", "profile", "remove", "personal", "--home"])
        .arg(home.path())
        .assert()
        .success();
    assert!(home.path().join(".si/codex/profiles/personal").is_dir());

    let output = cargo_bin()
        .args(["codex", "profile", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let profiles: Value = serde_json::from_slice(&output).expect("parse list");
    assert_eq!(profiles.as_array().expect("profile array").len(), 1);
}

#[test]
fn codex_run_enforces_env_contract_and_passes_exit_code() {
    let home = tempdir().expect("tempdir");
    cargo_bin()
        .args(["codex", "profile", "add", "a", "--home"])
        .arg(home.path())
        .assert()
        .success();

    let bin_dir = tempdir().expect("bin tempdir");
    let dump = bin_dir.path().join("env.dump");
    let codex_path = bin_dir.path().join("codex");
    write_executable_shell_script(
        &codex_path,
        &format!(
            "#!/bin/sh\nset -eu\necho \"CODEX_HOME=$CODEX_HOME\" >> {dump}\necho \"OPENAI_API_KEY=${{OPENAI_API_KEY:-missing}}\" >> {dump}\necho \"ARGS=$*\" >> {dump}\necho \"SI=$SI_PROVIDER/$SI_PROFILE\" >> {dump}\nexit 3\n",
            dump = dump.display(),
        ),
    );
    let path_env =
        format!("{}:{}", bin_dir.path().display(), std::env::var("PATH").unwrap_or_default());

    let output = cargo_bin()
        .env("PATH", path_env)
        .env("OPENAI_API_KEY", "seeded")
        .args(["codex", "run", "--profile", "a", "--home"])
        .arg(home.path())
        .args(["--", "exec", "--json"])
        .assert()
        .failure()
        .get_output()
        .clone();
    assert_eq!(output.status.code(), Some(3));
    let dumped = fs::read_to_string(&dump).expect("read env dump");
    assert!(dumped.contains(&format!("CODEX_HOME={}/.si/codex/profiles/a", home.path().display())));
    assert!(dumped.contains("OPENAI_API_KEY=missing"));
    assert!(dumped.contains("-c cli_auth_credentials_store=\"file\" exec --json"));
    assert!(dumped.contains("SI=codex/a"));

    let output = cargo_bin()
        .args(["codex", "run", "--profile", "a", "--home"])
        .arg(home.path())
        .args(["--", "-c", "cli_auth_credentials_store=os", "exec"])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    assert!(String::from_utf8(output).expect("utf8 stderr").contains("enforced credential store"));
}

#[test]
fn codex_profile_resolution_forms_are_consistent_across_lifecycle_commands() {
    let home = tempdir().expect("home tempdir");
    cargo_bin()
        .args(["codex", "profile", "add", "america", "--home"])
        .arg(home.path())
        .assert()
        .success();

    for args in [
        vec!["codex", "remove", "america", "--slot", "primary", "--home"],
        vec!["codex", "remove", "--profile", "america", "--slot", "primary", "--home"],
        vec!["codex", "stop", "america", "--slot", "primary", "--home"],
        vec!["codex", "stop", "--profile", "america", "--slot", "primary", "--home"],
        vec!["codex", "tail", "america", "--slot", "primary", "--home"],
        vec!["codex", "tail", "--profile", "america", "--slot", "primary", "--home"],
        vec!["codex", "tmux", "america", "--slot", "primary", "--home"],
        vec!["codex", "tmux", "--profile", "america", "--slot", "primary", "--home"],
    ] {
        let mut full: Vec<String> = args.into_iter().map(str::to_owned).collect();
        full.push(home.path().to_str().expect("home path").to_owned());
        let output = cargo_bin().args(&full).assert().failure().get_output().stderr.clone();
        let stderr = String::from_utf8(output).expect("stderr utf8");
        assert!(stderr.contains("no codex worker for profile"), "unexpected stderr: {stderr}");
        assert!(!stderr.contains("unexpected argument"));
    }
}

#[test]
fn codex_lifecycle_commands_reject_dual_profile_forms_and_shell_treats_positional_as_command() {
    for args in [
        vec!["codex", "remove", "america", "--profile", "america", "--slot", "primary"],
        vec!["codex", "stop", "america", "--profile", "america", "--slot", "primary"],
        vec!["codex", "tail", "america", "--profile", "america", "--slot", "primary"],
        vec!["codex", "tmux", "america", "--profile", "america", "--slot", "primary"],
    ] {
        let output = cargo_bin().args(args).assert().failure().get_output().stderr.clone();
        let stderr = String::from_utf8(output).expect("stderr utf8");
        assert!(stderr.contains("cannot be used with"));
        assert!(stderr.contains("[PROFILE]"));
    }

    // `shell` takes no positional profile: the first word is the command.
    let home = tempdir().expect("home tempdir");
    cargo_bin()
        .args(["codex", "profile", "add", "america", "--home"])
        .arg(home.path())
        .assert()
        .success();
    cargo_bin()
        .args(["codex", "profile", "select", "america", "--home"])
        .arg(home.path())
        .assert()
        .success();
    let output = cargo_bin()
        .args(["codex", "shell", "--home"])
        .arg(home.path())
        .args(["--", "echo", "ok"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert_eq!(String::from_utf8(output).expect("utf8 stdout").trim(), "ok");

    // Flags-first: the positional word joins the command, so SI tries to
    // run a program named `america` instead of treating it as a profile.
    let output = cargo_bin()
        .args(["codex", "shell", "--home"])
        .arg(home.path())
        .args(["america", "--", "echo", "ok"])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    let stderr = String::from_utf8(output).expect("stderr utf8");
    assert!(stderr.contains("run america"), "unexpected stderr: {stderr}");
}

#[test]
fn codex_stale_record_reconciles_without_model_and_keeps_native_data() {
    let home = tempdir().expect("tempdir");
    let workspace = home.path().join("workspace");
    fs::create_dir_all(&workspace).expect("mkdir workspace");
    cargo_bin()
        .args(["codex", "profile", "add", "america", "--home"])
        .arg(home.path())
        .assert()
        .success();

    // Native sentinel data the lifecycle must never touch.
    let native_root = home.path().join(".si/codex/profiles/america");
    let auth_path = native_root.join("auth.json");
    write_codex_auth_file(&auth_path, "america@example.test");
    let auth_before = fs::read(&auth_path).expect("read auth");
    let sessions_dir = native_root.join("sessions").join("2026");
    fs::create_dir_all(&sessions_dir).expect("mkdir sessions");
    fs::write(sessions_dir.join("rollout-x.jsonl"), "{}\n").expect("write rollout");

    // A record claiming a live worker, but no tmux server exists at all.
    write_worker_record_for_test(
        home.path(),
        "america",
        "primary",
        "running",
        "si-codex-pane-america",
        &workspace,
    );
    assert!(!owned_tmux_server_alive(home.path()));

    let output = cargo_bin()
        .args(["codex", "stop", "--profile", "america", "--slot", "primary", "--home"])
        .arg(home.path())
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert!(String::from_utf8(output).expect("utf8").contains("already exited"));

    let record_path =
        home.path().join(".si").join("codex").join("workers").join("america").join("primary.json");
    let record: Value = serde_json::from_slice(&fs::read(&record_path).expect("read record"))
        .expect("parse record");
    assert_eq!(record["state"], "exited");
    assert_eq!(fs::read(&auth_path).expect("read auth"), auth_before);
    assert!(sessions_dir.join("rollout-x.jsonl").is_file());

    // Removing the settled worker drops the record, never native data.
    cargo_bin()
        .args(["codex", "remove", "--profile", "america", "--slot", "primary", "--home"])
        .arg(home.path())
        .assert()
        .success();
    assert!(!record_path.exists());
    assert!(auth_path.is_file());
    assert!(sessions_dir.join("rollout-x.jsonl").is_file());
}

fn path_string(path: impl AsRef<Path>) -> Value {
    Value::String(path.as_ref().display().to_string())
}

#[test]
fn codex_spawn_stop_tail_remove_respawn_lifecycle() {
    let home = tempdir().expect("home tempdir");
    let _guard = TmuxGuard { home: home.path() };
    let workspace = home.path().join("workspace");
    fs::create_dir_all(&workspace).expect("mkdir workspace");
    let bin_dir = tempdir().expect("bin tempdir");
    write_blocking_fixture_codex_bin(bin_dir.path(), "WORKER-A-READY");
    let dump = bin_dir.path().join("dump.txt");
    let path_env = path_with(bin_dir.path());
    let home_arg = home.path().to_str().expect("home path").to_owned();

    cargo_bin().args(["codex", "profile", "add", "a", "--home", &home_arg]).assert().success();

    let spawn = |extra: &[&str]| {
        let mut args = vec!["codex", "spawn", "--profile", "a", "--home", &home_arg, "--workspace"];
        args.push(workspace.to_str().expect("workspace"));
        args.push("--format");
        args.push("json");
        args.push("--");
        args.extend(extra.iter().copied());
        cargo_bin()
            .env("PATH", &path_env)
            .env("DUMP", &dump)
            .args(&args)
            .assert()
            .success()
            .get_output()
            .stdout
            .clone()
    };
    let first: Value =
        serde_json::from_slice(&spawn(&["exec", "one"])).expect("parse spawn output");
    assert_eq!(first["state"], "started");
    assert_eq!(first["session"], "si-codex-pane-a");
    let generation = first["generation"].as_str().expect("generation").to_owned();

    // Same contract is idempotent; a different launch conflicts.
    let second: Value =
        serde_json::from_slice(&spawn(&["exec", "one"])).expect("parse respawn output");
    assert_eq!(second["state"], "already_running");
    assert_eq!(second["generation"], generation);
    cargo_bin()
        .env("PATH", &path_env)
        .args([
            "codex",
            "spawn",
            "--profile",
            "a",
            "--home",
            &home_arg,
            "--workspace",
            workspace.to_str().expect("workspace"),
            "--",
            "exec",
            "two",
        ])
        .assert()
        .failure();

    // Foreground run is refused while the worker holds the lease.
    let output = cargo_bin()
        .env("PATH", &path_env)
        .args(["codex", "run", "--profile", "a", "--home", &home_arg, "--", "exec", "x"])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    assert!(String::from_utf8(output).expect("utf8").contains("already has an active"));

    // List shows the live worker; tail shows its output.
    let output = cargo_bin()
        .args(["codex", "list", "--home", &home_arg, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let rows: Value = serde_json::from_slice(&output).expect("parse list");
    assert_eq!(rows.as_array().expect("rows").len(), 1);
    assert_eq!(rows[0]["state"], "running");
    assert_eq!(rows[0]["program_version"], "codex-fixture 0.0.0");
    let output = cargo_bin()
        .args(["codex", "tail", "--profile", "a", "--home", &home_arg, "--tail", "10"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert!(String::from_utf8(output).expect("utf8").contains("WORKER-A-READY"));

    // The native saw the enforced store (stripped by the fixture), the
    // canonical home, and the workspace workdir.
    let dumped = fs::read_to_string(&dump).expect("read dump");
    assert!(dumped.contains("NATIVE_ARGV=exec one"));
    assert!(
        dumped.contains(&format!("NATIVE_HOME={}/.si/codex/profiles/a", home.path().display()))
    );
    assert!(dumped.contains(&format!("NATIVE_PWD={}", workspace.display())));

    // Stop retains the real exit status; tail still works after death.
    let output = cargo_bin()
        .args(["codex", "stop", "--profile", "a", "--home", &home_arg])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert!(String::from_utf8(output).expect("utf8").contains("stopped"));
    let output = cargo_bin()
        .args(["codex", "list", "--home", &home_arg, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let rows: Value = serde_json::from_slice(&output).expect("parse list");
    assert_eq!(rows[0]["state"], "stopped");
    assert!(rows[0]["exit_code"].as_i64().expect("exit") != 0);
    let output = cargo_bin()
        .args(["codex", "tail", "--profile", "a", "--home", &home_arg, "--tail", "10"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert!(String::from_utf8(output).expect("utf8").contains("WORKER-A-READY"));

    // Respawn reuses the stored contract with a new generation.
    let output = cargo_bin()
        .env("PATH", &path_env)
        .args(["codex", "respawn", "--profile", "a", "--home", &home_arg, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let respawned: Value = serde_json::from_slice(&output).expect("parse respawn");
    assert_eq!(respawned["state"], "started");
    assert_ne!(respawned["generation"], generation);

    // Remove refuses the live worker, then removes with --stop.
    cargo_bin().args(["codex", "remove", "--profile", "a", "--home", &home_arg]).assert().failure();
    cargo_bin()
        .args(["codex", "remove", "--profile", "a", "--home", &home_arg, "--stop"])
        .assert()
        .success();
    assert!(!home.path().join(".si/codex/workers/a/primary.json").exists());
    assert!(home.path().join(".si/codex/profiles/a").is_dir());
    assert!(workspace.is_dir());
    let output = cargo_bin()
        .args(["codex", "list", "--home", &home_arg, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert_eq!(serde_json::from_slice::<Value>(&output).expect("parse list"), json!([]));
}

#[test]
fn codex_ab_workers_stay_isolated_and_default_codex_untouched() {
    let home = tempdir().expect("home tempdir");
    let _guard = TmuxGuard { home: home.path() };
    // Sentinel default Codex home: SI must never create, reset, or modify it.
    let default_codex = home.path().join(".codex");
    fs::create_dir_all(&default_codex).expect("mkdir default codex");
    fs::write(default_codex.join("sentinel"), "untouched\n").expect("write sentinel");

    let workspace_a = home.path().join("ws-a");
    let workspace_b = home.path().join("ws-b with space");
    fs::create_dir_all(&workspace_a).expect("mkdir ws-a");
    fs::create_dir_all(&workspace_b).expect("mkdir ws-b");
    let bin_dir = tempdir().expect("bin tempdir");
    write_blocking_fixture_codex_bin(bin_dir.path(), "READY");
    let dump = bin_dir.path().join("dump.txt");
    let path_env = path_with(bin_dir.path());
    let home_arg = home.path().to_str().expect("home path").to_owned();

    for profile in ["a", "b"] {
        cargo_bin()
            .args(["codex", "profile", "add", profile, "--home", &home_arg])
            .assert()
            .success();
    }
    // Seed hostile inherited overrides: children must ignore/reject them.
    let spawn = |profile: &str, workspace: &Path| {
        cargo_bin()
            .env("PATH", &path_env)
            .env("DUMP", &dump)
            .env("OPENAI_API_KEY", "seeded-fake")
            .env("CODEX_HOME", "/hijack")
            .args([
                "codex",
                "spawn",
                "--profile",
                profile,
                "--home",
                &home_arg,
                "--workspace",
                workspace.to_str().expect("workspace"),
                "--",
                "exec",
                profile,
            ])
            .assert()
            .success();
    };
    spawn("a", &workspace_a);
    spawn("b", &workspace_b);

    let output = cargo_bin()
        .args(["codex", "list", "--home", &home_arg, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let rows: Value = serde_json::from_slice(&output).expect("parse list");
    assert_eq!(rows.as_array().expect("rows").len(), 2);

    let dumped = fs::read_to_string(&dump).expect("read dump");
    assert!(
        dumped.contains(&format!("NATIVE_HOME={}/.si/codex/profiles/a", home.path().display()))
    );
    assert!(
        dumped.contains(&format!("NATIVE_HOME={}/.si/codex/profiles/b", home.path().display()))
    );
    assert!(!dumped.contains("/hijack"));
    assert!(!dumped.contains("seeded-fake"));
    assert!(dumped.contains("NATIVE_API_KEY=missing"));

    // Stop A while B works; B keeps running with its own store.
    cargo_bin().args(["codex", "stop", "--profile", "a", "--home", &home_arg]).assert().success();
    let output = cargo_bin()
        .args(["codex", "list", "--home", &home_arg, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let rows: Value = serde_json::from_slice(&output).expect("parse list");
    let states: Vec<(&str, &str)> = rows
        .as_array()
        .expect("rows")
        .iter()
        .map(|row| {
            (row["profile"].as_str().expect("profile"), row["state"].as_str().expect("state"))
        })
        .collect();
    assert!(states.contains(&("a", "stopped")));
    assert!(states.contains(&("b", "running")));

    cargo_bin().args(["codex", "stop", "--profile", "b", "--home", &home_arg]).assert().success();

    assert_eq!(
        fs::read_to_string(default_codex.join("sentinel")).expect("read sentinel"),
        "untouched\n"
    );
    let mut leftovers: Vec<_> = fs::read_dir(&default_codex)
        .expect("read default codex")
        .filter_map(|entry| entry.ok().map(|entry| entry.file_name()))
        .collect();
    leftovers.sort();
    assert_eq!(leftovers, vec![std::ffi::OsString::from("sentinel")]);
}

#[test]
fn codex_spawn_rolls_back_on_native_startup_failure() {
    let home = tempdir().expect("home tempdir");
    let _guard = TmuxGuard { home: home.path() };
    let workspace = home.path().join("workspace");
    fs::create_dir_all(&workspace).expect("mkdir workspace");
    let bin_dir = tempdir().expect("bin tempdir");
    write_fixture_codex_bin(bin_dir.path(), "exit 7\n");
    let path_env = path_with(bin_dir.path());
    let home_arg = home.path().to_str().expect("home path").to_owned();

    cargo_bin().args(["codex", "profile", "add", "a", "--home", &home_arg]).assert().success();
    let output = cargo_bin()
        .env("PATH", &path_env)
        .args([
            "codex",
            "spawn",
            "--profile",
            "a",
            "--home",
            &home_arg,
            "--workspace",
            workspace.to_str().expect("workspace"),
            "--",
            "exec",
            "x",
        ])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    assert!(String::from_utf8(output).expect("utf8").contains("worker launch failed"));

    // No partial worker remains: no record, no spec, no session.
    assert!(!home.path().join(".si/codex/workers/a/primary.json").exists());
    assert!(!home.path().join(".si/codex/workers/a/primary.spec.json").exists());
    assert!(!owned_tmux_server_alive(home.path()));
    // The profile itself stays usable.
    let output = cargo_bin()
        .args(["codex", "profile", "show", "a", "--home", &home_arg, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert_eq!(serde_json::from_slice::<Value>(&output).expect("parse")["profile"], "a");
}

#[test]
fn tmux_helper_ignores_inherited_tmux_env() {
    // Regression test for the 2026-10-07 incident where a test's bare
    // `tmux kill-server` destroyed the developer's live tmux sessions: `TMUX`
    // inherited from the surrounding pane overrode `TMUX_TMPDIR`, so the
    // "isolated" server was never isolated. The helper must scrub `TMUX`.
    let dir = tempdir().expect("tmpdir");
    let canary = dir.path().join("canary.sock");
    let canary_arg = canary.to_str().expect("socket path").to_owned();
    // A live server the helper must NOT reach. Owned socket, self-cleaning:
    // `sleep` exits and the server goes away with its last session.
    assert!(
        std::process::Command::new("tmux")
            .args(["-S", &canary_arg, "new-session", "-d", "-s", "canary", "sleep 30"])
            .status()
            .expect("spawn canary server")
            .success()
    );
    let empty = dir.path().join("empty");
    fs::create_dir_all(&empty).expect("mkdir empty tmux tmpdir");
    // Poisoned inheritance: `TMUX` points at the live canary while
    // `TMUX_TMPDIR` points at an empty dir. The helper must find no server.
    let poisoned = format!("{canary_arg},99999,0");
    let output =
        tmux_command(&[("TMUX", Path::new(&poisoned)), ("TMUX_TMPDIR", empty.as_path())], &["ls"])
            .output()
            .expect("run tmux ls");
    assert!(
        !output.status.success(),
        "helper reached the TMUX server: {}",
        String::from_utf8_lossy(&output.stdout)
    );
    let _ = std::process::Command::new("tmux").args(["-S", &canary_arg, "kill-server"]).output();
}

#[test]
fn codex_unrelated_tmux_name_collision_is_a_conflict_not_a_replace() {
    let home = tempdir().expect("home tempdir");
    let _guard = TmuxGuard { home: home.path() };
    let workspace = home.path().join("workspace");
    fs::create_dir_all(&workspace).expect("mkdir workspace");
    let bin_dir = tempdir().expect("bin tempdir");
    write_blocking_fixture_codex_bin(bin_dir.path(), "READY");
    let path_env = path_with(bin_dir.path());
    let home_arg = home.path().to_str().expect("home path").to_owned();
    let socket = home.path().join(".si/runtime/tmux.sock");
    fs::create_dir_all(socket.parent().expect("socket parent")).expect("mkdir runtime");

    cargo_bin().args(["codex", "profile", "add", "a", "--home", &home_arg]).assert().success();
    // Foreign session with SI's exact name but no ownership labels.
    let socket_arg = socket.to_str().expect("socket").to_owned();
    tmux_ok(
        &[],
        &[
            "-S",
            &socket_arg,
            "new-session",
            "-d",
            "-s",
            "si-codex-pane-a",
            "-x",
            "200",
            "-y",
            "50",
            "sleep 300",
        ],
    );

    let output = cargo_bin()
        .env("PATH", &path_env)
        .args([
            "codex",
            "spawn",
            "--profile",
            "a",
            "--home",
            &home_arg,
            "--workspace",
            workspace.to_str().expect("workspace"),
            "--",
            "exec",
            "x",
        ])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    assert!(
        String::from_utf8(output).expect("utf8").contains("refusing to replace a foreign session")
    );
    // The foreign session survives untouched; SI claims nothing.
    assert!(owned_tmux_server_alive(home.path()));
    assert!(!home.path().join(".si/codex/workers/a/primary.json").exists());
}

#[test]
fn codex_tampered_ownership_label_refuses_action() {
    let home = tempdir().expect("home tempdir");
    let _guard = TmuxGuard { home: home.path() };
    let workspace = home.path().join("workspace");
    fs::create_dir_all(&workspace).expect("mkdir workspace");
    let bin_dir = tempdir().expect("bin tempdir");
    write_blocking_fixture_codex_bin(bin_dir.path(), "READY");
    let path_env = path_with(bin_dir.path());
    let home_arg = home.path().to_str().expect("home path").to_owned();
    let socket = home.path().join(".si/runtime/tmux.sock");

    cargo_bin().args(["codex", "profile", "add", "a", "--home", &home_arg]).assert().success();
    cargo_bin()
        .env("PATH", &path_env)
        .args([
            "codex",
            "spawn",
            "--profile",
            "a",
            "--home",
            &home_arg,
            "--workspace",
            workspace.to_str().expect("workspace"),
            "--",
            "exec",
            "x",
        ])
        .assert()
        .success();

    let record_path = home.path().join(".si/codex/workers/a/primary.json");
    let record: Value =
        serde_json::from_slice(&fs::read(&record_path).expect("read record")).expect("parse");
    let owner = record["owner"].as_str().expect("owner").to_owned();
    let socket_arg = socket.to_str().expect("socket").to_owned();
    tmux_ok(&[], &["-S", &socket_arg, "set-option", "-t", "si-codex-pane-a", "@si_owner", "evil"]);

    let output = cargo_bin()
        .args(["codex", "stop", "--profile", "a", "--home", &home_arg])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    assert!(String::from_utf8(output).expect("utf8").contains("not owned by this worker"));
    // The session is still alive: nothing was signaled.
    assert!(owned_tmux_server_alive(home.path()));

    // Restore the label; lifecycle works again.
    tmux_ok(&[], &["-S", &socket_arg, "set-option", "-t", "si-codex-pane-a", "@si_owner", &owner]);
    cargo_bin().args(["codex", "stop", "--profile", "a", "--home", &home_arg]).assert().success();
}

#[test]
fn codex_respawn_validates_resume_id_in_profile_store() {
    let home = tempdir().expect("home tempdir");
    let _guard = TmuxGuard { home: home.path() };
    let workspace = home.path().join("workspace");
    fs::create_dir_all(&workspace).expect("mkdir workspace");
    let bin_dir = tempdir().expect("bin tempdir");
    write_fixture_codex_bin(bin_dir.path(), "echo \"ARGS=$*\" >> \"$DUMP\"\nsleep 2\nexit 0\n");
    let dump = bin_dir.path().join("dump.txt");
    let path_env = path_with(bin_dir.path());
    let home_arg = home.path().to_str().expect("home path").to_owned();

    for profile in ["a", "b"] {
        cargo_bin()
            .args(["codex", "profile", "add", profile, "--home", &home_arg])
            .assert()
            .success();
    }
    fs::write(
        home.path().join(".si/codex/profiles/a/session_index.jsonl"),
        "{\"id\":\"uuid-aaaa\",\"thread_name\":\"A thread\",\"updated_at\":\"2026-10-07T00:00:00Z\"}\n",
    )
    .expect("write index a");
    fs::write(
        home.path().join(".si/codex/profiles/b/session_index.jsonl"),
        "{\"id\":\"uuid-bbbb\",\"thread_name\":\"B thread\",\"updated_at\":\"2026-10-07T00:00:00Z\"}\n",
    )
    .expect("write index b");

    // Quick worker that exits on its own; wait until it settles.
    cargo_bin()
        .env("PATH", &path_env)
        .env("DUMP", &dump)
        .args([
            "codex",
            "spawn",
            "--profile",
            "a",
            "--home",
            &home_arg,
            "--workspace",
            workspace.to_str().expect("workspace"),
            "--",
            "exec",
            "x",
        ])
        .assert()
        .success();
    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(15);
    loop {
        let output = cargo_bin()
            .args(["codex", "list", "--home", &home_arg, "--format", "json"])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone();
        let rows: Value = serde_json::from_slice(&output).expect("parse list");
        if rows[0]["state"] != "running" {
            break;
        }
        assert!(std::time::Instant::now() < deadline, "worker did not settle");
        std::thread::sleep(std::time::Duration::from_millis(100));
    }

    // Another profile's ID is refused without launching anything.
    let output = cargo_bin()
        .env("PATH", &path_env)
        .args([
            "codex",
            "respawn",
            "--profile",
            "a",
            "--home",
            &home_arg,
            "--resume-id",
            "uuid-bbbb",
        ])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    assert!(
        String::from_utf8(output).expect("utf8").contains("is not known in codex profile"),
        "foreign resume id must be refused"
    );
    let output = cargo_bin()
        .env("PATH", &path_env)
        .args([
            "codex",
            "respawn",
            "--profile",
            "a",
            "--home",
            &home_arg,
            "--resume-id",
            "uuid-nope",
        ])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    assert!(String::from_utf8(output).expect("utf8").contains("refusing to start a fresh"));

    // The profile's own ID resumes through the native resume command.
    cargo_bin()
        .env("PATH", &path_env)
        .env("DUMP", &dump)
        .args([
            "codex",
            "respawn",
            "--profile",
            "a",
            "--home",
            &home_arg,
            "--resume-id",
            "uuid-aaaa",
        ])
        .assert()
        .success();
    let dumped = fs::read_to_string(&dump).expect("read dump");
    assert!(dumped.contains("ARGS=resume uuid-aaaa"), "unexpected dump: {dumped}");
}

/// Legacy schema-1 settings with an ordinary, a custom-filename, and a
/// missing-auth profile, plus one legacy worker record and one slot home
/// carrying sessions, an index, an auth fork, and sqlite.
fn write_legacy_migration_fixture(home: &Path, workspace: &Path) {
    let si = home.join(".si");
    let america = si.join("codex/profiles/america");
    let custom = si.join("codex/profiles/custom");
    fs::create_dir_all(&america).expect("mkdir america");
    fs::create_dir_all(&custom).expect("mkdir custom");
    write_codex_auth_file(&america.join("auth.json"), "america@example.test");
    fs::write(custom.join("creds.json"), "{\"tokens\":{}}\n").expect("write custom creds");
    fs::write(
        si.join("settings.toml"),
        format!(
            "schema_version = 1\n[codex]\nprofile = \"america\"\n[codex.profiles]\nactive = \"america\"\n\
             [codex.profiles.entries.america]\nname = \"America\"\nemail = \"america@example.test\"\n\
             auth_path = {:?}\n[codex.profiles.entries.custom]\nname = \"Custom\"\n\
             auth_path = {:?}\n[codex.profiles.entries.missing]\nname = \"Missing\"\n",
            america.join("auth.json").display().to_string(),
            custom.join("creds.json").display().to_string(),
        ),
    )
    .expect("write legacy settings");
    let workers = si.join("codex/workers/america");
    fs::create_dir_all(&workers).expect("mkdir workers");
    fs::write(
        workers.join("primary.json"),
        serde_json::to_string_pretty(&json!({
            "schema_version": 1,
            "profile_id": "america",
            "worker_slot": "primary",
            "session_name": "si-codex-pane-america",
            "workspace": workspace.display().to_string(),
            "workdir": workspace.display().to_string(),
            "updated_at": "2026-01-01T00:00:00Z",
        }))
        .expect("encode legacy worker"),
    )
    .expect("write legacy worker");

    let slot = america.join("workers/review");
    let slot_sessions = slot.join("sessions/2026/10/07");
    fs::create_dir_all(&slot_sessions).expect("mkdir slot sessions");
    fs::write(
        slot_sessions.join("rollout-2026-10-07T00-00-00-uuid-slot1.jsonl"),
        "{\"ordinal\":0}\n",
    )
    .expect("write slot rollout");
    fs::write(
        slot.join("session_index.jsonl"),
        "{\"id\":\"uuid-slot1\",\"thread_name\":\"Slot one\",\"updated_at\":\"2026-10-07T00:00:00Z\"}\n",
    )
    .expect("write slot index");
    write_codex_auth_file(&slot.join("auth.json"), "forked@example.test");
    fs::write(slot.join("threads.sqlite"), "fake-sqlite\n").expect("write slot sqlite");
}

#[test]
fn doctor_migration_plan_dry_run_changes_nothing() {
    let home = tempdir().expect("home tempdir");
    let workspace = home.path().join("workspace");
    fs::create_dir_all(&workspace).expect("mkdir workspace");
    write_legacy_migration_fixture(home.path(), &workspace);
    let tmp = tempdir().expect("tmp tempdir");
    let settings_before = fs::read(home.path().join(".si/settings.toml")).expect("read settings");
    let home_arg = home.path().to_str().expect("home path").to_owned();

    let output = cargo_bin()
        .env("TMUX_TMPDIR", tmp.path())
        .args(["doctor", "--migration-plan", "--format", "json", "--home", &home_arg])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let inventory: Value = serde_json::from_slice(&output).expect("parse plan");
    assert_eq!(inventory["plan"]["ready"], true);
    let profiles = inventory["plan"]["profiles"].as_array().expect("profiles");
    assert_eq!(profiles.len(), 3);
    let america = profiles.iter().find(|item| item["profile"] == "america").expect("america");
    assert_eq!(america["disposition"]["kind"], "keep_canonical_home");
    assert_eq!(
        profiles.iter().filter(|item| item["disposition"]["kind"] == "needs_enrollment").count(),
        2
    );
    assert_eq!(inventory["legacy_workers"].as_array().expect("workers").len(), 1);
    assert_eq!(inventory["slot_histories"].as_array().expect("histories").len(), 1);
    let history = &inventory["slot_histories"][0];
    assert_eq!(history["session_files"], 1);
    assert_eq!(history["has_index"], true);
    assert_eq!(history["has_auth_fork"], true);
    assert_eq!(history["has_sqlite"], true);
    assert_eq!(inventory["active_blockers"].as_array().expect("blockers").len(), 0);

    assert_eq!(fs::read(home.path().join(".si/settings.toml")).expect("read"), settings_before);
    assert!(!home.path().join(".si/backups").exists());
}

#[test]
fn doctor_migrate_applies_and_second_run_changes_nothing() {
    let home = tempdir().expect("home tempdir");
    let _guard = TmuxGuard { home: home.path() };
    let workspace = home.path().join("workspace");
    fs::create_dir_all(&workspace).expect("mkdir workspace");
    write_legacy_migration_fixture(home.path(), &workspace);
    let tmp = tempdir().expect("tmp tempdir");
    let home_arg = home.path().to_str().expect("home path").to_owned();
    let migrate = || {
        cargo_bin()
            .env("TMUX_TMPDIR", tmp.path())
            .args(["doctor", "--migrate", "--format", "json", "--home", &home_arg])
            .assert()
            .success()
            .get_output()
            .stdout
            .clone()
    };

    let report: Value = serde_json::from_slice(&migrate()).expect("parse report");
    assert_eq!(report["schema_version"], 2);
    assert_eq!(report["profiles_converted"], json!(["america"]));
    assert_eq!(report["profiles_needing_enrollment"].as_array().expect("arr").len(), 2);
    assert_eq!(report["workers_converted"], json!(["america [primary]"]));
    assert_eq!(report["histories"].as_array().expect("histories").len(), 1);
    assert_eq!(report["histories"][0]["index_merged"], 1);

    // Settings converted: schema marker, native_root set, auth_path gone.
    let settings: toml::Value = toml::from_str(
        &fs::read_to_string(home.path().join(".si/settings.toml")).expect("read settings"),
    )
    .expect("parse settings");
    assert_eq!(settings["schema_version"], toml::Value::Integer(2));
    let america_entry = &settings["codex"]["profiles"]["entries"]["america"];
    assert!(america_entry.get("native_root").is_some());
    assert!(america_entry.get("auth_path").is_none());

    // Worker record converted to a legacy entry; slot history moved.
    let record: Value = serde_json::from_slice(
        &fs::read(home.path().join(".si/codex/workers/america/primary.json")).expect("read record"),
    )
    .expect("parse record");
    assert_eq!(record["state"], "legacy");
    assert_eq!(record["workspace"], workspace.display().to_string());
    let moved = home
        .path()
        .join(".si/codex/profiles/america/sessions/2026/10/07/rollout-2026-10-07T00-00-00-uuid-slot1.jsonl");
    assert_eq!(fs::read_to_string(&moved).expect("read moved"), "{\"ordinal\":0}\n");
    assert!(!home.path().join(".si/codex/profiles/america/workers/review/sessions/2026/10/07/rollout-2026-10-07T00-00-00-uuid-slot1.jsonl").exists());
    let index =
        fs::read_to_string(home.path().join(".si/codex/profiles/america/session_index.jsonl"))
            .expect("read merged index");
    assert!(index.contains("uuid-slot1"));

    // Credentials, databases, and directories stay in the slot home.
    let slot = home.path().join(".si/codex/profiles/america/workers/review");
    assert!(slot.join("auth.json").is_file());
    assert!(slot.join("threads.sqlite").is_file());
    assert!(slot.join("sessions").is_dir());

    // Backup captured settings, the legacy record, and the index baseline.
    let backup = report["backup_dir"].as_str().expect("backup").to_owned();
    assert!(Path::new(&backup).join("settings.toml").is_file());
    assert!(Path::new(&backup).join("workers/america-primary.json").is_file());

    // Second run: settled, no new backup.
    let backups_before =
        fs::read_dir(home.path().join(".si/backups")).expect("read backups").count();
    let output = cargo_bin()
        .env("TMUX_TMPDIR", tmp.path())
        .args(["doctor", "--migrate", "--home", &home_arg])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert!(String::from_utf8(output).expect("utf8").contains("no changes"));
    let backups_after =
        fs::read_dir(home.path().join(".si/backups")).expect("read backups").count();
    assert_eq!(backups_before, backups_after);
}

#[test]
fn doctor_migrate_refuses_duplicate_store_and_leaves_everything_intact() {
    let home = tempdir().expect("home tempdir");
    let si = home.path().join(".si");
    let shared = si.join("codex/profiles/shared");
    fs::create_dir_all(&shared).expect("mkdir shared");
    write_codex_auth_file(&shared.join("auth.json"), "shared@example.test");
    fs::write(
        si.join("settings.toml"),
        format!(
            "schema_version = 1\n[codex.profiles.entries.one]\nname = \"One\"\nauth_path = {:?}\n\
             [codex.profiles.entries.two]\nname = \"Two\"\nauth_path = {:?}\n",
            shared.join("auth.json").display().to_string(),
            shared.join("auth.json").display().to_string(),
        ),
    )
    .expect("write settings");
    let settings_before = fs::read(si.join("settings.toml")).expect("read settings");
    let tmp = tempdir().expect("tmp tempdir");
    let home_arg = home.path().to_str().expect("home path").to_owned();

    let output = cargo_bin()
        .env("TMUX_TMPDIR", tmp.path())
        .args(["doctor", "--migrate", "--home", &home_arg])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    assert!(String::from_utf8(output).expect("utf8").contains("not ready"));
    assert_eq!(fs::read(si.join("settings.toml")).expect("read"), settings_before);
    assert!(!si.join("backups").exists());
}

#[test]
fn doctor_migrate_leaves_history_conflicts_in_place() {
    let home = tempdir().expect("home tempdir");
    let si = home.path().join(".si");
    let america = si.join("codex/profiles/america");
    fs::create_dir_all(&america).expect("mkdir america");
    write_codex_auth_file(&america.join("auth.json"), "america@example.test");
    fs::write(
        si.join("settings.toml"),
        format!(
            "schema_version = 1\n[codex.profiles.entries.america]\nname = \"America\"\n\
             auth_path = {:?}\n",
            america.join("auth.json").display().to_string(),
        ),
    )
    .expect("write settings");
    let rel = "sessions/2026/10/07/rollout-2026-10-07T00-00-00-uuid-c.jsonl";
    fs::create_dir_all(america.join("sessions/2026/10/07")).expect("mkdir canonical sessions");
    fs::write(america.join(&rel), "{\"side\":\"canonical\"}\n").expect("write canonical");
    let slot = america.join("workers/review");
    fs::create_dir_all(slot.join("sessions/2026/10/07")).expect("mkdir slot sessions");
    fs::write(slot.join(&rel), "{\"side\":\"slot\"}\n").expect("write slot");
    fs::write(
        america.join("session_index.jsonl"),
        "{\"id\":\"uuid-c\",\"thread_name\":\"Canonical\",\"updated_at\":\"2026-10-07T00:00:00Z\"}\n",
    )
    .expect("write canonical index");
    fs::write(
        slot.join("session_index.jsonl"),
        "{\"id\":\"uuid-c\",\"thread_name\":\"Slot\",\"updated_at\":\"2026-10-07T00:00:01Z\"}\n",
    )
    .expect("write slot index");
    let tmp = tempdir().expect("tmp tempdir");
    let home_arg = home.path().to_str().expect("home path").to_owned();

    let output = cargo_bin()
        .env("TMUX_TMPDIR", tmp.path())
        .args(["doctor", "--migrate", "--format", "json", "--home", &home_arg])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let report: Value = serde_json::from_slice(&output).expect("parse report");
    let history = &report["histories"][0];
    assert_eq!(history["files"][0]["outcome"], "conflict_left_in_place");
    assert_eq!(history["index_conflicts"].as_array().expect("conflicts").len(), 1);

    // Both sides byte-identical; canonical index kept.
    assert_eq!(fs::read_to_string(america.join(&rel)).expect("read"), "{\"side\":\"canonical\"}\n");
    assert_eq!(fs::read_to_string(slot.join(&rel)).expect("read"), "{\"side\":\"slot\"}\n");
    let index = fs::read_to_string(america.join("session_index.jsonl")).expect("read index");
    assert!(index.contains("\"thread_name\":\"Canonical\""));
    assert!(!index.contains("\"thread_name\":\"Slot\""));
}

#[test]
fn codex_spawn_refuses_legacy_record_until_migrate() {
    let home = tempdir().expect("home tempdir");
    let workspace = home.path().join("workspace");
    fs::create_dir_all(&workspace).expect("mkdir workspace");
    write_legacy_migration_fixture(home.path(), &workspace);
    let tmp = tempdir().expect("tmp tempdir");
    let bin_dir = tempdir().expect("bin tempdir");
    write_blocking_fixture_codex_bin(bin_dir.path(), "READY");
    let path_env = path_with(bin_dir.path());
    let home_arg = home.path().to_str().expect("home path").to_owned();
    let legacy_before = fs::read(home.path().join(".si/codex/workers/america/primary.json"))
        .expect("read legacy record");

    let output = cargo_bin()
        .env("PATH", &path_env)
        .args([
            "codex",
            "spawn",
            "--profile",
            "america",
            "--home",
            &home_arg,
            "--workspace",
            workspace.to_str().expect("workspace"),
            "--",
            "exec",
            "x",
        ])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    assert!(String::from_utf8(output).expect("utf8").contains("si doctor --migrate"));
    assert_eq!(
        fs::read(home.path().join(".si/codex/workers/america/primary.json")).expect("read"),
        legacy_before
    );

    cargo_bin()
        .env("TMUX_TMPDIR", tmp.path())
        .args(["doctor", "--migrate", "--home", &home_arg])
        .assert()
        .success();
    // After migration the slot respawns from the converted legacy entry.
    cargo_bin()
        .env("PATH", &path_env)
        .args(["codex", "respawn", "--profile", "america", "--home", &home_arg])
        .assert()
        .success();
    kill_owned_tmux_server(home.path());
}

#[test]
fn codex_run_rejects_credential_argv_before_native_spawn() {
    let home = tempdir().expect("home tempdir");
    let home_arg = home.path().to_str().expect("home path").to_owned();
    cargo_bin().args(["codex", "profile", "add", "a", "--home", &home_arg]).assert().success();
    let bin_dir = tempdir().expect("bin tempdir");
    let dump = bin_dir.path().join("native.dump");
    write_executable_shell_script(
        &bin_dir.path().join("codex"),
        &format!("#!/bin/sh\necho SPAWNED >> {}\nexit 0\n", shell_escape_for_test(&dump)),
    );
    let path_env = path_with(bin_dir.path());
    let jwt = format!("{}.{}.{}", "a".repeat(10), "b".repeat(40), "c".repeat(43));

    let cases: Vec<(Vec<String>, &str)> = vec![
        (vec!["login".to_owned()], "profile login"),
        (vec!["logout".to_owned()], "profile logout"),
        (vec!["--with-api-key".to_owned()], "profile login"),
        (vec!["exec".to_owned(), "--with-access-token".to_owned()], "profile login"),
        (vec!["-c".to_owned(), "api_key=x".to_owned(), "exec".to_owned()], "non-auth"),
        (vec!["--oss".to_owned(), "exec".to_owned()], "enrolled"),
        (vec!["exec".to_owned(), "sk-abcdefgh12345678".to_owned()], "stdin or files"),
        (vec!["exec".to_owned(), jwt], "stdin or files"),
    ];
    for (argv, fragment) in &cases {
        let mut full = vec!["codex", "run", "--profile", "a", "--home", home_arg.as_str(), "--"];
        full.extend(argv.iter().map(String::as_str));
        let output = cargo_bin()
            .env("PATH", &path_env)
            .args(&full)
            .assert()
            .failure()
            .get_output()
            .stderr
            .clone();
        let stderr = String::from_utf8(output).expect("utf8 stderr");
        assert!(stderr.contains(fragment), "argv {argv:?}: {stderr}");
    }
    assert!(!dump.exists(), "rejected argv must never spawn the native");
}

#[test]
fn codex_run_strips_all_auth_overrides_and_delivers_metachars_verbatim() {
    let home = tempdir().expect("home tempdir");
    let home_arg = home.path().to_str().expect("home path").to_owned();
    cargo_bin().args(["codex", "profile", "add", "a", "--home", &home_arg]).assert().success();
    let bin_dir = tempdir().expect("bin tempdir");
    let dump = bin_dir.path().join("run.dump");
    write_executable_shell_script(
        &bin_dir.path().join("codex"),
        &format!(
            "#!/bin/sh\n{{\necho \"CODEX_HOME=$CODEX_HOME\"\necho \"OPENAI_API_KEY=${{OPENAI_API_KEY:-missing}}\"\necho \"CODEX_API_KEY=${{CODEX_API_KEY:-missing}}\"\necho \"CODEX_ACCESS_TOKEN=${{CODEX_ACCESS_TOKEN:-missing}}\"\nprintf 'ARG=<%s>\\n' \"$@\"\n}} >> {}\nexit 0\n",
            shell_escape_for_test(&dump)
        ),
    );
    let path_env = path_with(bin_dir.path());
    let marker = bin_dir.path().join("pwned.marker");
    // One argv token: metacharacters that a shell would execute. SI spawns
    // directly, so the native must receive it byte-identical and nothing
    // must execute.
    let prompt = format!(
        "explain 2>&1; touch {} | cat && echo $(touch {}) `touch {}` 'sq' \"dq\"",
        marker.display(),
        marker.display(),
        marker.display()
    );

    cargo_bin()
        .env("PATH", &path_env)
        .env("OPENAI_API_KEY", "seeded-openai")
        .env("CODEX_API_KEY", "seeded-codex")
        .env("CODEX_ACCESS_TOKEN", "seeded-access")
        .env("CODEX_HOME", "/hijack")
        .args(["codex", "run", "--profile", "a", "--home", &home_arg, "--"])
        .args(["-c", "model=gpt-5", "-p", "cfg", "login", "status", &prompt])
        .assert()
        .success();
    let dumped = fs::read_to_string(&dump).expect("read run dump");
    assert!(dumped.contains(&format!("CODEX_HOME={}/.si/codex/profiles/a", home.path().display())));
    assert!(!dumped.contains("/hijack"));
    for key in ["OPENAI_API_KEY=missing", "CODEX_API_KEY=missing", "CODEX_ACCESS_TOKEN=missing"] {
        assert!(dumped.contains(key), "dump:\n{dumped}");
    }
    for token in ["-c", "model=gpt-5", "-p", "cfg", "login", "status"] {
        assert!(dumped.contains(&format!("ARG=<{token}>")), "dump:\n{dumped}");
    }
    assert!(dumped.contains(&format!("ARG=<{prompt}>")), "dump:\n{dumped}");
    assert!(!marker.exists(), "metacharacters in argv must never execute");
}

#[test]
fn codex_spawn_rejects_credential_argv_without_side_effects() {
    let home = tempdir().expect("home tempdir");
    let _guard = TmuxGuard { home: home.path() };
    let home_arg = home.path().to_str().expect("home path").to_owned();
    let workspace = home.path().join("workspace");
    fs::create_dir_all(&workspace).expect("mkdir workspace");
    cargo_bin().args(["codex", "profile", "add", "a", "--home", &home_arg]).assert().success();
    let bin_dir = tempdir().expect("bin tempdir");
    write_fixture_codex_bin(bin_dir.path(), "exit 0\n");
    let path_env = path_with(bin_dir.path());

    for (argv, fragment) in [
        (vec!["login"], "profile login"),
        (vec!["--with-api-key"], "profile login"),
        (vec!["-c", "model_provider=azure", "exec"], "non-auth"),
    ] {
        let mut full = vec![
            "codex",
            "spawn",
            "--profile",
            "a",
            "--home",
            &home_arg,
            "--workspace",
            workspace.to_str().expect("workspace"),
            "--",
        ];
        full.extend(argv.iter().copied());
        let output = cargo_bin()
            .env("PATH", &path_env)
            .args(&full)
            .assert()
            .failure()
            .get_output()
            .stderr
            .clone();
        let stderr = String::from_utf8(output).expect("utf8 stderr");
        assert!(stderr.contains(fragment), "argv {argv:?}: {stderr}");
    }
    assert!(!home.path().join(".si/codex/workers/a/primary.json").exists());
    assert!(!home.path().join(".si/codex/workers/a/primary.spec.json").exists());
    assert!(!owned_tmux_server_alive(home.path()));
}
