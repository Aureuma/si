use assert_cmd::Command;
use base64::{Engine as _, engine::general_purpose::URL_SAFE_NO_PAD};
use chrono::Utc;
use serde_json::{Value, json};
use si_fort::{SessionState, classify_persisted_session_state, load_persisted_session_state};
use std::fs;
use std::io::{Read, Write};
use std::net::TcpListener;
use std::path::Path;
use std::sync::{Arc, Mutex};
use std::thread;
use tempfile::tempdir;

fn cargo_bin() -> Command {
    Command::cargo_bin("si").expect("si binary should build")
}

#[allow(clippy::result_large_err)]
fn write_named_codex_profile_settings(
    home: &Path,
    active_profile: &str,
    profiles: &[(&str, &str, &str)],
) {
    let settings_dir = home.join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let mut source = String::from("schema_version = 1\n[codex]\n");
    source.push_str(&format!("profile = {active_profile:?}\n\n"));
    source.push_str("[codex.profiles]\n");
    source.push_str(&format!("active = {active_profile:?}\n\n"));
    for (profile, name, email) in profiles {
        source.push_str(&format!("[codex.profiles.entries.{profile}]\n"));
        source.push_str(&format!("name = \"{name}\"\n"));
        source.push_str(&format!("email = \"{email}\"\n"));
        source.push_str(&format!(
            "auth_path = {:?}\n\n",
            home.join(".si").join("codex").join("profiles").join(profile).join("auth.json")
        ));
    }
    fs::write(settings_dir.join("settings.toml"), source).expect("write named codex settings");
}

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

fn write_reusable_codex_fort_session(codex_home: &Path, profile_id: &str) {
    let fort_dir = codex_home.join("fort");
    fs::create_dir_all(&fort_dir).expect("mkdir fort session dir");
    let access_token_path = fort_dir.join("access.token");
    let refresh_token_path = fort_dir.join("refresh.token");
    fs::write(&access_token_path, "access-token\n").expect("write fort access token");
    fs::write(&refresh_token_path, "refresh-token\n").expect("write fort refresh token");
    let access_expires_at = (Utc::now() + chrono::Duration::hours(1)).to_rfc3339();
    let refresh_expires_at = (Utc::now() + chrono::Duration::days(30)).to_rfc3339();
    let session_path = fort_dir.join("session.json");
    fs::write(
        &session_path,
        serde_json::to_vec_pretty(&json!({
            "profile_id": profile_id,
            "agent_id": format!("si-codex-{profile_id}"),
            "session_id": format!("fort-session-{profile_id}"),
            "host": "https://fort.example.test",
            "runtime_host": "https://fort.example.test",
            "access_token_path": access_token_path,
            "refresh_token_path": refresh_token_path,
            "access_expires_at": access_expires_at,
            "refresh_expires_at": refresh_expires_at,
            "updated_at": Utc::now().to_rfc3339(),
        }))
        .expect("serialize fort session"),
    )
    .expect("write fort session state");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in [&session_path, &access_token_path, &refresh_token_path] {
            fs::set_permissions(path, fs::Permissions::from_mode(0o600))
                .expect("chmod fort session file");
        }
    }
}

fn write_codex_worker_state_for_test(
    home: &Path,
    profile_id: &str,
    worker_slot: &str,
    session_name: &str,
    workspace: &Path,
    workdir: &Path,
) {
    let path = home
        .join(".si")
        .join("codex")
        .join("workers")
        .join(profile_id)
        .join(format!("{worker_slot}.json"));
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).expect("mkdir worker state dir");
    }
    fs::write(
        &path,
        serde_json::to_vec_pretty(&json!({
            "schema_version": 1,
            "profile_id": profile_id,
            "worker_slot": worker_slot,
            "profile_name": "America",
            "session_name": session_name,
            "workspace": workspace.display().to_string(),
            "workdir": workdir.display().to_string(),
            "updated_at": Utc::now().to_rfc3339(),
        }))
        .expect("serialize worker state"),
    )
    .expect("write worker state");
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
fn codex_profile_swap_requires_logged_in_profile() {
    let home = tempdir().expect("tempdir");
    write_named_codex_profile_settings(
        home.path(),
        "profile-gamma",
        &[("profile-gamma", "🛰 Profile Gamma", "gamma@example.test")],
    );
    let host_codex_home = home.path().join(".codex");
    fs::create_dir_all(&host_codex_home).expect("mkdir host codex home");
    fs::write(host_codex_home.join("config.toml"), "model = \"gpt-5\"\n").expect("write config");

    let output = cargo_bin()
        .args(["codex", "profile", "swap", "profile-gamma", "--home"])
        .arg(home.path())
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    let stderr = String::from_utf8(output).expect("utf8 stderr");
    assert!(stderr.contains("is not Logged-In"));
    assert_eq!(
        fs::read_to_string(host_codex_home.join("config.toml")).expect("read config"),
        "model = \"gpt-5\"\n"
    );
    assert!(!host_codex_home.join("auth.json").exists());
}

#[test]
fn codex_profile_list_preserves_logged_in_state_when_live_probe_fails() {
    let home = tempdir().expect("tempdir");
    write_named_codex_profile_settings(
        home.path(),
        "america",
        &[("america", "America", "america@example.test")],
    );
    write_codex_auth_file(
        &home.path().join(".si/codex/profiles/america/auth.json"),
        "america@example.test",
    );

    let bin_dir = tempdir().expect("bin tempdir");
    let codex_path = bin_dir.path().join("codex");
    write_executable_shell_script(
        &codex_path,
        "#!/bin/sh\nset -eu\nif [ \"${1:-}\" = \"app-server\" ]; then\n  printf 'temporary app-server failure\\n' >&2\n  exit 1\nfi\nprintf 'unexpected codex invocation\\n' >&2\nexit 1\n",
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
    assert_eq!(profile["state"], "Logged-In");
    assert!(profile["five_hour_left_pct"].is_null());
    assert!(profile["weekly_left_pct"].is_null());
}

#[test]
fn codex_repair_auth_all_provisions_slot_specific_agents_with_30d_ttl() {
    let home = tempdir().expect("home tempdir");
    let workspace = home.path().join("workspace");
    fs::create_dir_all(&workspace).expect("mkdir workspace");
    let codex_profiles_dir = home.path().join(".si/codex/profiles");
    fs::create_dir_all(&codex_profiles_dir).expect("mkdir codex profiles");

    write_codex_worker_state_for_test(
        home.path(),
        "america",
        "primary",
        "si-codex-pane-america",
        &workspace,
        &workspace,
    );
    write_codex_worker_state_for_test(
        home.path(),
        "america",
        "review",
        "si-codex-pane-america-review",
        &workspace,
        &workspace,
    );

    let requests_seen = Arc::new(Mutex::new(Vec::<String>::new()));
    let seen_clone = Arc::clone(&requests_seen);
    let call_index = Arc::new(std::sync::atomic::AtomicUsize::new(0));
    let call_index_clone = Arc::clone(&call_index);
    let server = start_http_server_with_body(8, move |request| {
        seen_clone.lock().expect("requests lock").push(request.clone());
        let call = call_index_clone.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        if request.starts_with("GET /v1/agents/si-codex-america HTTP/1.1\r\n")
            || request.starts_with("GET /v1/agents/si-codex-america--review HTTP/1.1\r\n")
        {
            return http_json_response("404 Not Found", &[], r#"{"error":"not found"}"#);
        }
        if request.starts_with("POST /v1/agents HTTP/1.1\r\n") {
            return http_json_response("201 Created", &[], r#"{"ok":true}"#);
        }
        if request.starts_with("PUT /v1/agents/si-codex-america/policy HTTP/1.1\r\n")
            || request.starts_with("PUT /v1/agents/si-codex-america--review/policy HTTP/1.1\r\n")
        {
            return http_json_response("200 OK", &[], r#"{"ok":true}"#);
        }
        if request.starts_with("POST /v1/auth/session/open HTTP/1.1\r\n") {
            let body = format!(
                r#"{{"access_token":"{}","refresh_token":"rft-{call}","session_id":"session-{call}","access_expires_at":"2030-01-01T00:00:00Z","refresh_expires_at":"2030-01-31T00:00:00Z"}}"#,
                fake_jwt(json!({"exp": Utc::now().timestamp() + 3600}))
            );
            return http_json_response("200 OK", &[], &body);
        }
        panic!("unexpected request: {}", request.lines().next().unwrap_or("<empty>"));
    });

    fs::create_dir_all(home.path().join(".si/fort/bootstrap")).expect("mkdir fort bootstrap");
    fs::write(
        home.path().join(".si/fort/bootstrap/admin.token"),
        format!("{}\n", fake_jwt(json!({"exp": Utc::now().timestamp() + 3600}))),
    )
    .expect("write bootstrap token");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(
            home.path().join(".si/fort/bootstrap/admin.token"),
            fs::Permissions::from_mode(0o600),
        )
        .expect("chmod bootstrap token");
    }
    fs::write(
        home.path().join(".si/settings.toml"),
        format!(
            "schema_version = 1\n[paths]\ncodex_profiles_dir = {:?}\n[codex]\nprofile = \"america\"\n[codex.profiles]\nactive = \"america\"\n[codex.profiles.entries.america]\nname = \"America\"\nemail = \"america@example.test\"\nauth_path = {:?}\n[fort]\nhost = {:?}\n",
            codex_profiles_dir,
            codex_profiles_dir.join("america").join("auth.json"),
            server.base_url,
        ),
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("HOME", home.path())
        .env("SI_FORT_ALLOW_INSECURE_HOST", "1")
        .args(["codex", "repair-auth", "--all", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("parse repair output");
    let repaired = parsed["repaired"].as_array().expect("repaired array");
    assert_eq!(repaired.len(), 2);
    assert!(repaired.iter().any(|item| item["agent_id"] == "si-codex-america"));
    assert!(repaired.iter().any(|item| item["agent_id"] == "si-codex-america--review"));

    let requests = requests_seen.lock().expect("requests lock");
    let open_calls = requests
        .iter()
        .filter(|request| request.starts_with("POST /v1/auth/session/open HTTP/1.1\r\n"))
        .collect::<Vec<_>>();
    assert_eq!(open_calls.len(), 2, "expected two Fort session open calls");
    assert!(open_calls.iter().any(|request| request.contains(r#""agent_id":"si-codex-america""#)));
    assert!(
        open_calls
            .iter()
            .any(|request| request.contains(r#""agent_id":"si-codex-america--review""#))
    );
    assert!(open_calls.iter().all(|request| request.contains(r#""refresh_ttl":"30d""#)));
    server.join();
}

#[test]
fn codex_profile_resolution_forms_are_consistent_across_lifecycle_commands() {
    let home = tempdir().expect("home tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir .si");
    fs::write(
        home.path().join(".si/settings.toml"),
        "schema_version = 1\n[codex]\nprofile = \"america\"\n[codex.profiles]\nactive = \"america\"\n[codex.profiles.entries.america]\nname = \"America\"\nemail = \"america@example.test\"\nauth_path = \"/tmp/nonexistent\"\n",
    )
    .expect("write settings");

    for args in [
        vec!["codex", "remove", "america", "--slot", "primary"],
        vec!["codex", "remove", "--profile", "america", "--slot", "primary"],
        vec!["codex", "stop", "america", "--slot", "primary"],
        vec!["codex", "stop", "--profile", "america", "--slot", "primary"],
        vec!["codex", "tail", "america", "--slot", "primary"],
        vec!["codex", "tail", "--profile", "america", "--slot", "primary"],
        vec!["codex", "tmux", "america", "--slot", "primary", "--format", "json"],
        vec!["codex", "tmux", "--profile", "america", "--slot", "primary", "--format", "json"],
    ] {
        let output = cargo_bin()
            .env("HOME", home.path())
            .args(args)
            .assert()
            .failure()
            .get_output()
            .stderr
            .clone();
        let stderr = String::from_utf8(output).expect("stderr utf8");
        assert!(stderr.contains("no codex worker session found for profile"));
        assert!(!stderr.contains("unexpected argument"));
    }
}

#[test]
fn codex_lifecycle_commands_reject_dual_profile_forms_and_shell_legacy_positional_profile() {
    for args in [
        vec!["codex", "remove", "america", "--profile", "america", "--slot", "primary"],
        vec!["codex", "stop", "america", "--profile", "america", "--slot", "primary"],
        vec!["codex", "tail", "america", "--profile", "america", "--slot", "primary"],
        vec![
            "codex",
            "tmux",
            "america",
            "--profile",
            "america",
            "--slot",
            "primary",
            "--format",
            "json",
        ],
    ] {
        let output = cargo_bin().args(args).assert().failure().get_output().stderr.clone();
        let stderr = String::from_utf8(output).expect("stderr utf8");
        assert!(stderr.contains("cannot be used with"));
        assert!(stderr.contains("[PROFILE]"));
    }

    let output = cargo_bin()
        .args(["codex", "shell", "america", "--", "echo", "ok"])
        .assert()
        .failure()
        .get_output()
        .stderr
        .clone();
    let stderr = String::from_utf8(output).expect("stderr utf8");
    assert!(stderr.contains("legacy positional profile form"));
    assert!(stderr.contains("use `si codex shell --profile <profile> --slot <slot> -- <command>`"));
}

#[test]
fn codex_stop_keeps_worker_state_and_fort_auth_files() {
    let home = tempdir().expect("tempdir");
    let workspace = home.path().join("workspace");
    fs::create_dir_all(&workspace).expect("mkdir workspace");
    write_named_codex_profile_settings(
        home.path(),
        "america",
        &[("america", "America", "america@example.test")],
    );

    let codex_home = home.path().join(".si/codex/profiles/america");
    write_reusable_codex_fort_session(&codex_home, "america");
    write_codex_worker_state_for_test(
        home.path(),
        "america",
        "primary",
        "si-codex-pane-america",
        &workspace,
        &workspace,
    );

    let state_path =
        home.path().join(".si").join("codex").join("workers").join("america").join("primary.json");
    let access_token_path = codex_home.join("fort/access.token");
    let refresh_token_path = codex_home.join("fort/refresh.token");
    let session_path = codex_home.join("fort/session.json");

    let output = cargo_bin()
        .env("HOME", home.path())
        .args(["codex", "stop", "--profile", "america", "--slot", "primary"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    assert!(String::from_utf8(output).expect("utf8").contains("stopped si-codex-pane-america"));

    assert!(state_path.exists());
    assert!(access_token_path.exists());
    assert!(refresh_token_path.exists());
    assert!(session_path.exists());
}

fn path_string(path: impl AsRef<Path>) -> Value {
    Value::String(path.as_ref().display().to_string())
}

struct TestHttpServer {
    base_url: String,
    handle: Option<thread::JoinHandle<()>>,
}

impl TestHttpServer {
    fn join(mut self) {
        if let Some(handle) = self.handle.take() {
            handle.join().expect("server thread should join");
        }
    }
}

fn start_http_server_with_body<F>(requests: usize, handler: F) -> TestHttpServer
where
    F: Fn(String) -> String + Send + Sync + 'static,
{
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind test server");
    let addr = listener.local_addr().expect("local addr");
    let handler = std::sync::Arc::new(handler);
    let handle = thread::spawn(move || {
        for _ in 0..requests {
            let (mut stream, _) = listener.accept().expect("accept");
            let mut request = Vec::new();
            let mut buffer = [0_u8; 4096];
            let mut content_length = 0_usize;
            let mut header_end = None;
            loop {
                let read = stream.read(&mut buffer).expect("read request");
                if read == 0 {
                    break;
                }
                request.extend_from_slice(&buffer[..read]);
                if header_end.is_none()
                    && let Some(pos) = request.windows(4).position(|window| window == b"\r\n\r\n")
                {
                    header_end = Some(pos + 4);
                    let headers = String::from_utf8_lossy(&request[..pos + 4]).to_ascii_lowercase();
                    for line in headers.lines() {
                        if let Some(value) = line.strip_prefix("content-length:") {
                            content_length = value.trim().parse::<usize>().unwrap_or(0);
                            break;
                        }
                    }
                }
                if let Some(end) = header_end
                    && request.len() >= end + content_length
                {
                    break;
                }
            }
            let request = String::from_utf8(request).expect("request utf8");
            let response = handler(request);
            stream.write_all(response.as_bytes()).expect("write response");
            stream.flush().expect("flush response");
        }
    });
    TestHttpServer { base_url: format!("http://{addr}"), handle: Some(handle) }
}

fn http_json_response(status: &str, headers: &[(&str, &str)], body: &str) -> String {
    let mut response = format!(
        "HTTP/1.1 {status}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n",
        body.len()
    );
    for (key, value) in headers {
        response.push_str(&format!("{key}: {value}\r\n"));
    }
    response.push_str("\r\n");
    response.push_str(body);
    response
}

