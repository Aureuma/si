use assert_cmd::Command;
use chrono::Local;
use serde_json::Value;
use std::fs;
use std::fs::File;
use std::io::{Read, Write};
use std::net::TcpListener;
use std::path::Path;
use std::thread;
use tempfile::tempdir;

fn cargo_bin() -> Command {
    Command::cargo_bin("si-rs").expect("si-rs binary should build")
}

#[test]
fn version_matches_go_repo_version() {
    cargo_bin().arg("version").assert().success().stdout("v0.54.0\n");
}

#[test]
fn help_json_lists_known_root_commands() {
    let output = cargo_bin()
        .args(["help", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let commands = parsed["commands"].as_array().expect("commands array should be present");

    assert!(commands.iter().any(|entry| entry["name"] == "github"));
    assert!(commands.iter().any(|entry| entry["name"] == "spawn"));
    assert!(!commands.iter().any(|entry| entry["name"] == "__fort-runtime-agent"));
}

#[test]
fn help_json_for_specific_command_includes_aliases() {
    let output = cargo_bin()
        .args(["help", "remote-control", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let commands = parsed["commands"].as_array().expect("commands array should be present");

    assert_eq!(commands.len(), 1);
    assert_eq!(commands[0]["name"], "remote-control");
    assert_eq!(commands[0]["aliases"][0], "rc");
}

#[test]
fn settings_show_defaults_to_home_scoped_core_settings() {
    let home = tempdir().expect("tempdir");
    let output = cargo_bin()
        .args(["settings", "show", "--format", "json", "--home"])
        .arg(home.path())
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["schema_version"], 1);
    assert_eq!(parsed["paths"]["root"], path_string(home.path().join(".si")));
    assert_eq!(
        parsed["paths"]["codex_profiles_dir"],
        path_string(home.path().join(".si/codex/profiles"))
    );
}

#[test]
fn settings_show_reads_core_runtime_fields() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    let settings_path = settings_dir.join("settings.toml");
    fs::write(
        &settings_path,
        r#"
[paths]
workspace_root = "~/Development"

[codex]
workspace = "~/Development/si"
workdir = "/workspace"
profile = "darmstada"

[dyad]
workspace = "~/Development"
configs = "~/Development/si/configs"
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
    assert_eq!(parsed["paths"]["workspace_root"], "~/Development");
    assert_eq!(parsed["codex"]["workspace"], "~/Development/si");
    assert_eq!(parsed["codex"]["workdir"], "/workspace");
    assert_eq!(parsed["codex"]["profile"], "darmstada");
    assert_eq!(parsed["dyad"]["workspace"], "~/Development");
    assert_eq!(parsed["dyad"]["configs"], "~/Development/si/configs");
}

#[test]
fn warmup_status_json_reads_and_upgrades_legacy_state() {
    let home = tempdir().expect("tempdir");
    let warmup_dir = home.path().join(".si/warmup");
    fs::create_dir_all(&warmup_dir).expect("mkdir warmup dir");
    fs::write(
        warmup_dir.join("state.json"),
        r#"{
  "version": 2,
  "profiles": {
    "ferma": {
      "profile_id": " ferma ",
      "last_result": "warmed",
      "last_weekly_used_pct": 12.5,
      "last_weekly_reset": "2030-03-20T00:00:00Z"
    }
  }
}
"#,
    )
    .expect("write state");

    let output = cargo_bin()
        .args(["warmup", "status", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["version"], 3);
    assert_eq!(parsed["profiles"]["ferma"]["profile_id"], "ferma");
    assert_eq!(parsed["profiles"]["ferma"]["last_warmed_reset"], "2030-03-20T00:00:00Z");
    assert_eq!(parsed["profiles"]["ferma"]["last_weekly_used_ok"], true);
}

#[test]
fn warmup_status_text_reports_empty_state() {
    let home = tempdir().expect("tempdir");

    let output = cargo_bin()
        .args(["warmup", "status", "--home"])
        .arg(home.path())
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    assert_eq!(String::from_utf8_lossy(&output), "warmup state is empty\n");
}

#[test]
fn warmup_state_write_persists_normalized_json() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("state.json");

    cargo_bin()
        .args(["warmup", "state", "write", "--path"])
        .arg(&state_path)
        .args([
            "--state-json",
            r#"{"version":0,"updated_at":" 2030-03-19T12:00:00Z ","profiles":{" ferma ":{"profile_id":" ferma ","last_result":" ready "}}}"#,
        ])
        .assert()
        .success();

    let raw = fs::read_to_string(&state_path).expect("read persisted state");
    let parsed: Value = serde_json::from_str(&raw).expect("json");
    assert_eq!(parsed["version"], 3);
    assert_eq!(parsed["updated_at"], "2030-03-19T12:00:00Z");
    assert_eq!(parsed["profiles"]["ferma"]["profile_id"], "ferma");
    assert_eq!(parsed["profiles"]["ferma"]["last_result"], "ready");
}

#[test]
fn warmup_marker_show_reports_disabled_and_autostart() {
    let home = tempdir().expect("tempdir");
    let warmup_dir = home.path().join(".si/warmup");
    fs::create_dir_all(&warmup_dir).expect("mkdir warmup dir");
    fs::write(warmup_dir.join("autostart.v1"), "2030-03-19T12:00:00Z\n").expect("write autostart");
    fs::write(warmup_dir.join("disabled.v1"), "disabled\n").expect("write disabled");

    let output = cargo_bin()
        .args(["warmup", "marker", "show", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["disabled"], true);
    assert_eq!(parsed["autostart_present"], true);
}

#[test]
fn warmup_marker_write_and_set_disabled_update_files() {
    let state_dir = tempdir().expect("tempdir");
    let autostart_path = state_dir.path().join("autostart.v1");
    let disabled_path = state_dir.path().join("disabled.v1");

    cargo_bin()
        .args(["warmup", "marker", "write-autostart", "--path"])
        .arg(&autostart_path)
        .assert()
        .success();
    cargo_bin()
        .args(["warmup", "marker", "set-disabled", "--path"])
        .arg(&disabled_path)
        .args(["--disabled=true"])
        .assert()
        .success();
    assert!(autostart_path.exists());
    assert!(disabled_path.exists());

    cargo_bin()
        .args(["warmup", "marker", "set-disabled", "--path"])
        .arg(&disabled_path)
        .args(["--disabled=false"])
        .assert()
        .success();
    assert!(!disabled_path.exists());
}

#[test]
fn warmup_autostart_decision_prefers_disabled_and_legacy_state() {
    let home = tempdir().expect("tempdir");
    let warmup_dir = home.path().join(".si/warmup");
    fs::create_dir_all(&warmup_dir).expect("mkdir warmup dir");
    fs::write(
        warmup_dir.join("state.json"),
        r#"{"version":3,"profiles":{"ferma":{"profile_id":"ferma","last_result":"ready"}}}"#,
    )
    .expect("write state");

    let output = cargo_bin()
        .args(["warmup", "autostart-decision", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["requested"], true);
    assert_eq!(parsed["reason"], "legacy_state");

    fs::write(warmup_dir.join("disabled.v1"), "disabled\n").expect("write disabled");
    let output = cargo_bin()
        .args(["warmup", "autostart-decision", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["requested"], false);
    assert_eq!(parsed["reason"], "disabled");
}

#[test]
fn providers_characteristics_json_matches_expected_shape() {
    let output = cargo_bin()
        .args(["providers", "characteristics", "--provider", "github", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["policy"]["defaults"], "built_in_go");
    let providers = parsed["providers"].as_array().expect("providers array");
    assert_eq!(providers.len(), 1);
    assert_eq!(providers[0]["provider"], "github");
    assert_eq!(providers[0]["base_url"], "https://api.github.com");
    assert_eq!(providers[0]["api_version"], "2022-11-28");
    assert_eq!(providers[0]["public_probe"]["path"], "/zen");
}

#[test]
fn providers_characteristics_supports_alias_ids() {
    let output = cargo_bin()
        .args(["providers", "characteristics", "--provider", "twitter", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let providers = parsed["providers"].as_array().expect("providers array");
    assert_eq!(providers[0]["provider"], "social_x");
}

#[test]
fn github_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[github]
default_account = "core"
default_auth_mode = "oauth"

[github.accounts.core]
name = "Core"
owner = "Aureuma"
api_base_url = "https://ghe.example/api/v3"
auth_mode = "oauth"

[github.accounts.ops]
name = "Ops"
owner = "OpsOrg"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["github", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts.len(), 2);
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "true");
    assert_eq!(contexts[0]["auth_mode"], "oauth");
    assert_eq!(contexts[1]["alias"], "ops");
    assert_eq!(contexts[1]["auth_mode"], "oauth");
}

#[test]
fn github_context_current_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[github]
default_account = "core"
default_auth_mode = "app"

[github.accounts.core]
owner = "Aureuma"
api_base_url = "https://ghe.example/api/v3"
auth_mode = "oauth"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["github", "context", "current", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["owner"], "Aureuma");
    assert_eq!(parsed["auth_mode"], "oauth");
    assert_eq!(parsed["base_url"], "https://ghe.example/api/v3");
    assert_eq!(parsed["source"], "settings.default_account,settings.auth_mode");
}

#[test]
fn github_auth_status_json_resolves_oauth_token() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[github]
default_account = "core"

[github.accounts.core]
owner = "Aureuma"
auth_mode = "oauth"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args(["github", "auth", "status", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["owner"], "Aureuma");
    assert_eq!(parsed["auth_mode"], "oauth");
    assert_eq!(parsed["base_url"], "https://api.github.com");
    assert_eq!(parsed["source"], "settings.default_account,settings.auth_mode,env:GITHUB_TOKEN");
    assert_eq!(parsed["token_preview"], "gho_exam...");
}

#[test]
fn github_auth_status_json_resolves_app_credentials() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[github]
default_account = "core"
default_auth_mode = "app"

[github.accounts.core]
owner = "Aureuma"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("GITHUB_CORE_APP_ID", "42")
        .env("GITHUB_CORE_APP_PRIVATE_KEY_PEM", "-----BEGIN PRIVATE KEY-----abc")
        .env("GITHUB_CORE_INSTALLATION_ID", "99")
        .args(["github", "auth", "status", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["owner"], "Aureuma");
    assert_eq!(parsed["auth_mode"], "app");
    assert_eq!(parsed["base_url"], "https://api.github.com");
    assert_eq!(
        parsed["source"],
        "settings.default_account,settings.default_auth_mode,env:GITHUB_CORE_APP_ID,env:GITHUB_CORE_APP_PRIVATE_KEY_PEM,env:GITHUB_CORE_INSTALLATION_ID"
    );
    assert_eq!(parsed["token_preview"], "-");
}

#[test]
fn github_release_list_json_fetches_from_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /repos/Aureuma/si/releases?page=1&per_page=100 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_release_list")],
            r#"[{"id":101,"tag_name":"v1.2.3"}]"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "github",
            "release",
            "list",
            "Aureuma/si",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_release_list");
    assert_eq!(parsed["list"][0]["tag_name"], "v1.2.3");
    server.join();
}

#[test]
fn github_release_get_json_fetches_tag_with_app_auth() {
    let call_count = std::sync::Arc::new(std::sync::atomic::AtomicUsize::new(0));
    let calls = std::sync::Arc::clone(&call_count);
    let server = start_http_server(2, move |request| {
        let call = calls.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
        match call {
            0 => {
                assert!(
                    request.starts_with("POST /app/installations/321/access_tokens HTTP/1.1\r\n")
                );
                assert!(request.contains("authorization: Bearer "));
                http_json_response(
                    "201 Created",
                    &[("x-github-request-id", "req_gh_install")],
                    r#"{"token":"ghs_install_token","expires_at":"2030-01-01T00:00:00Z"}"#,
                )
            }
            1 => {
                assert!(
                    request.starts_with(
                        "GET /repos/Aureuma/si/releases/tags/v1.2.3 HTTP/1.1\r\n"
                    )
                );
                assert!(request.contains("authorization: Bearer ghs_install_token\r\n"));
                http_json_response(
                    "200 OK",
                    &[("x-github-request-id", "req_gh_release_get")],
                    r#"{"id":101,"tag_name":"v1.2.3"}"#,
                )
            }
            _ => panic!("unexpected request"),
        }
    });

    let app_key = "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQCaJZkLuu/uJGz1\n4cxlZ3d7H5b88tcXH0qPZmkCUPWHA4aumx36BErkorXukYD0IRhRaJe8shsgRC4c\nw5TkjrXcG9Kigh3HvifRnA1kCbmwceANdww6J8ggtFDFO026VIEx2R8tjtYLs+pU\n+Xb6llxixE+QWSXQVHqHy67KvWDeRu6es8OZb8klxFejwdTBC0UDxNLwdr+hDV3b\nEDduxm+pnnmTi7ciwDbrO8D/GXkYi7YLXwqcfHLhVqZeVXrs5JPc+7pOHJCf1fZO\n9BBUOVO9qDUqfQk7CWBF3MKyNtx/wv+Mzg5ztl4VMPRgdnbnU8B2en+rPYZg7KTF\nN2n0ORH/AgMBAAECggEAAfNDfkZVXnN1Mh/duKi4S8VTYTbnVBe6we60mb68JIL9\nvhF2AyGbxaHDYIB/G6zxhFIo8qO5kSJxB5R35UkNnE/OeJeMgz2bflzq6cmaYP+d\nKz5xgqjZ24QR2N+jtPL4bCYy7UjMhNBiwMQj5mQRnimdV2uxUp3xq5cpn89ekuFY\n1C48pXicl8OLgdzhNAROk2edrYo+DJl+5VaSPSN5L+dz67pBqAZ4gcUj4ZdmofmB\ninHw83zTvQfSFaykC98TJEpQppaC8gK+mxQF6bWotfxq/Gd2MBhNwJAF1WnJ2cq/\np2vuDCqliKbt40M33qUVIavhY6C50dUQ3VeERxmvyQKBgQDSlBBZJ2auZHgJeR/U\nIYUPOypo8mBBVMh6axbRR5yrpTfGDHqc4Zx4nC3kxRjqnA+sfdZBESOgvj7FdWUj\nf3fEM+RPQLW0zu2F+wmJ2w28kncOFVxHrrrxJToKtBSfR3YIjCnZmy6pxn8WOimM\nabOm5hmSRLgMcRSvptw6crOOtwKBgQC7ZXCuTgnod+Cf25PvKNxSLJOy9lephPYO\nqU7LWywilQEgj7VWrmVKP+6HC3L615++cLlKxoozlvT0dxjfhzgdZxXKLOUf4x3d\n72FXx/sKFFtOCgeDeR2Ln+hSLbGsCLkyOo5zFFCidmE4z0DitiPmSRtJdHt1VthO\n8KW10yTO+QKBgCBZhrlriCa6YIZ0CSO5kotod3dv5MGkmLfVw8eazMLBuvO97wgy\n0Krms1Y1wUIpf27sVgHg9Cw5jcMf6c2uQ2Ps5OIX+tIwB+VRT4HSGSYjCg8r0OVi\nPm3VXjlOuOxPOh7OCY/Yey6xw8xSWxerFWJKbxs9W1jt9lOVurdv7425AoGBAKIU\nQ5hOoN0yydIZjWK92YktSvXvgLR67oKRxze1fH/Qlm/+O55kKfFFSF3+9gyk8GI7\nhtd4ztF+EBFc7ONwRYWQwlTh7a5dtlhdEbllmugF4U6m+Aare3Vm8f4ZzWD5Doy1\n/rzj5jYN41rKTtmHJZeoxXQLzjgXy/DCzOBtZZmpAoGABacst96WKng6XE5MkZpo\nacIEMOPpPYnyc4VgqHPft4D45ARP4wFZryxZ58Ya6194Z9PUzL5N7yKgsQZlnGR8\nL6W4ulLYfyhkWfi592cIKS7eDjWijbcIUzgvuIzCWvme08KQSPkgYNFXomlg4EZv\n9HrWPhpFaH+jHJsVKmD/Qyo=\n-----END PRIVATE KEY-----";

    let output = cargo_bin()
        .env("GITHUB_APP_PRIVATE_KEY_PEM", app_key)
        .args([
            "github",
            "release",
            "get",
            "Aureuma/si",
            "v1.2.3",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "app",
            "--app-id",
            "123",
            "--installation-id",
            "321",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_release_get");
    assert_eq!(parsed["data"]["tag_name"], "v1.2.3");
    server.join();
}

#[test]
fn github_repo_list_json_fetches_from_api_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /users/Aureuma/repos?page=1&per_page=100 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_repo_list")],
            r#"[{"id":101,"full_name":"Aureuma/si"}]"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "github",
            "repo",
            "list",
            "Aureuma",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["owner"], "Aureuma");
    assert_eq!(parsed["count"], 1);
    assert_eq!(parsed["data"][0]["full_name"], "Aureuma/si");
    server.join();
}

#[test]
fn github_repo_get_json_fetches_repo_with_oauth() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /repos/Aureuma/si HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer gho_example_token\r\n"));
        http_json_response(
            "200 OK",
            &[("x-github-request-id", "req_gh_repo_get")],
            r#"{"id":101,"full_name":"Aureuma/si"}"#,
        )
    });

    let output = cargo_bin()
        .env("GITHUB_TOKEN", "gho_example_token")
        .args([
            "github",
            "repo",
            "get",
            "Aureuma/si",
            "--base-url",
            &server.base_url,
            "--auth-mode",
            "oauth",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_gh_repo_get");
    assert_eq!(parsed["data"]["full_name"], "Aureuma/si");
    server.join();
}

#[test]
fn stripe_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[stripe]
default_account = "core"

[stripe.accounts.core]
id = "acct_core"
name = "Core"
live_key_env = "CORE_LIVE"

[stripe.accounts.ops]
id = "acct_ops"
sandbox_key = "sk_test_ops"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["stripe", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts.len(), 2);
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "yes");
    assert_eq!(contexts[0]["live_key_config"], "yes");
    assert_eq!(contexts[1]["alias"], "ops");
    assert_eq!(contexts[1]["sandbox_key_config"], "yes");
}

#[test]
fn stripe_context_current_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[stripe]
default_account = "core"
default_env = "sandbox"

[stripe.accounts.core]
id = "acct_core"
sandbox_key = "sk_test_core"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["stripe", "context", "current", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["account_id"], "acct_core");
    assert_eq!(parsed["environment"], "sandbox");
    assert_eq!(parsed["key_source"], "settings.sandbox_key");
}

#[test]
fn stripe_auth_status_json_resolves_env_key() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[stripe]
default_env = "sandbox"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("SI_STRIPE_API_KEY", "sk_test_shared")
        .args(["stripe", "auth", "status", "--account", "acct_123", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "");
    assert_eq!(parsed["account_id"], "acct_123");
    assert_eq!(parsed["environment"], "sandbox");
    assert_eq!(parsed["key_source"], "env:SI_STRIPE_API_KEY");
    assert_eq!(parsed["key_preview"], "sk_test_...");
}

#[test]
fn workos_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[workos]
default_account = "core"
default_env = "prod"

[workos.accounts.core]
name = "Core"
prod_api_key_env = "CORE_PROD"
client_id_env = "CORE_CLIENT"
organization_id = "org_123"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["workos", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "true");
    assert_eq!(contexts[0]["api_key_env"], "CORE_PROD");
}

#[test]
fn workos_context_current_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[workos]
default_account = "core"
default_env = "prod"

[workos.accounts.core]
prod_api_key_env = "CORE_PROD"
organization_id = "org_123"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_PROD", "sk_workos_prod")
        .args(["workos", "context", "current", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["environment"], "prod");
    assert_eq!(parsed["organization_id"], "org_123");
    assert_eq!(parsed["source"], "env:CORE_PROD,settings.organization_id");
}

#[test]
fn workos_auth_status_json_resolves_env_sources() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[workos]
default_account = "core"

[workos.accounts.core]
prod_api_key_env = "CORE_PROD"
client_id_env = "CORE_CLIENT"
organization_id = "org_123"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_PROD", "sk_workos_prod")
        .env("CORE_CLIENT", "client_123")
        .args(["workos", "auth", "status", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["environment"], "prod");
    assert_eq!(parsed["source"], "env:CORE_PROD,env:CORE_CLIENT,settings.organization_id");
    assert_eq!(parsed["key_preview"], "sk_worko...");
}

#[test]
fn cloudflare_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[cloudflare]
default_account = "core"

[cloudflare.accounts.core]
name = "Core"
account_id = "acc_core"
prod_zone_id = "zone_prod"

[cloudflare.accounts.ops]
account_id = "acc_ops"
staging_zone_id = "zone_staging"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["cloudflare", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "true");
    assert_eq!(contexts[1]["staging_zone"], "zone_staging");
}

#[test]
fn cloudflare_context_current_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[cloudflare]
default_account = "core"
default_env = "prod"

[cloudflare.accounts.core]
account_id = "acc_core"
default_zone_name = "example.com"
prod_zone_id = "zone_prod"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["cloudflare", "context", "current", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["account_id"], "acc_core");
    assert_eq!(parsed["environment"], "prod");
    assert_eq!(parsed["zone_id"], "zone_prod");
    assert_eq!(parsed["zone_name"], "example.com");
    assert_eq!(parsed["source"], "settings.account_id,settings.prod_zone_id");
}

#[test]
fn cloudflare_auth_status_json_verifies_token() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /user/tokens/verify HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer cf-test-token\r\n"));
        http_json_response(
            "200 OK",
            &[],
            r#"{"success":true,"result":{"id":"verify_123","status":"active"}}"#,
        )
    });

    let output = cargo_bin()
        .args(["cloudflare", "auth", "status"])
        .args(["--account", "core"])
        .args(["--env", "prod"])
        .args(["--zone-id", "zone_prod"])
        .args(["--account-id", "acc_core"])
        .args(["--api-token", "cf-test-token"])
        .args(["--base-url", &server.base_url, "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status"], "ready");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["account_id"], "acc_core");
    assert_eq!(parsed["zone_id"], "zone_prod");
    assert_eq!(parsed["token_preview"], "cf-tes...");
    assert_eq!(parsed["verify"]["result"]["id"], "verify_123");
    server.join();
}

#[test]
fn apple_appstore_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[apple]
default_account = "core"

[apple.appstore.accounts.core]
name = "Core"
project_id = "proj_core"
default_bundle_id = "com.example.core"
default_platform = "IOS"
default_language = "en-US"

[apple.appstore.accounts.ops]
project_id = "proj_ops"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["apple", "appstore", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "true");
    assert_eq!(contexts[0]["bundle_id"], "com.example.core");
    assert_eq!(contexts[1]["alias"], "ops");
}

#[test]
fn apple_appstore_context_current_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[apple]
default_account = "core"
default_env = "prod"

[apple.appstore.accounts.core]
project_id = "proj_core"
issuer_id_env = "CORE_ISSUER"
key_id_env = "CORE_KEY"
private_key_env = "CORE_PRIVATE_KEY"
default_bundle_id = "com.example.core"
default_language = "en-US"
default_platform = "IOS"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_ISSUER", "issuer_123")
        .env("CORE_KEY", "key_123")
        .env("CORE_PRIVATE_KEY", "-----BEGIN PRIVATE KEY-----")
        .args(["apple", "appstore", "context", "current", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["project_id"], "proj_core");
    assert_eq!(parsed["environment"], "prod");
    assert_eq!(parsed["bundle_id"], "com.example.core");
    assert_eq!(parsed["platform"], "IOS");
    assert_eq!(parsed["token_source"], "env:CORE_PRIVATE_KEY");
    assert_eq!(
        parsed["source"],
        "settings.apple.project_id,settings.apple.default_bundle_id,settings.apple.default_language,settings.apple.default_platform,env:CORE_ISSUER,env:CORE_KEY"
    );
}

#[test]
fn aws_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[aws]
default_account = "core"
default_region = "us-east-1"

[aws.accounts.core]
name = "Core"
access_key_id_env = "CORE_AWS_ACCESS_KEY_ID"

[aws.accounts.ops]
region = "us-west-2"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["aws", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "true");
    assert_eq!(contexts[0]["region"], "us-east-1");
    assert_eq!(contexts[1]["region"], "us-west-2");
}

#[test]
fn aws_context_current_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[aws]
default_account = "core"
default_region = "us-east-1"

[aws.accounts.core]
access_key_id_env = "CORE_AWS_ACCESS_KEY_ID"
secret_access_key_env = "CORE_AWS_SECRET_ACCESS_KEY"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_AWS_ACCESS_KEY_ID", "AKIA1234567890ABCD")
        .env("CORE_AWS_SECRET_ACCESS_KEY", "secret")
        .args(["aws", "context", "current", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["region"], "us-east-1");
    assert_eq!(parsed["base_url"], "https://iam.amazonaws.com");
    assert_eq!(parsed["source"], "env:CORE_AWS_ACCESS_KEY_ID,env:CORE_AWS_SECRET_ACCESS_KEY");
    assert_eq!(parsed["access_key"], "AKIA**********ABCD");
}

#[test]
fn aws_auth_status_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[aws]
default_account = "core"

[aws.accounts.core]
region = "us-west-2"
access_key_id_env = "CORE_AWS_ACCESS_KEY_ID"
secret_access_key_env = "CORE_AWS_SECRET_ACCESS_KEY"
session_token_env = "CORE_AWS_SESSION_TOKEN"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_AWS_ACCESS_KEY_ID", "AKIA1234567890ABCD")
        .env("CORE_AWS_SECRET_ACCESS_KEY", "secret")
        .env("CORE_AWS_SESSION_TOKEN", "session")
        .args(["aws", "auth", "status", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["region"], "us-west-2");
    assert_eq!(
        parsed["source"],
        "env:CORE_AWS_ACCESS_KEY_ID,env:CORE_AWS_SECRET_ACCESS_KEY,env:CORE_AWS_SESSION_TOKEN"
    );
    assert_eq!(parsed["access_key"], "AKIA**********ABCD");
}

#[test]
fn gcp_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[gcp]
default_account = "core"

[gcp.accounts.core]
project_id = "proj_core"
access_token_env = "CORE_GCP_ACCESS_TOKEN"

[gcp.accounts.ops]
project_id_env = "OPS_GCP_PROJECT"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["gcp", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "true");
    assert_eq!(contexts[0]["project_id"], "proj_core");
    assert_eq!(contexts[0]["token_env"], "CORE_GCP_ACCESS_TOKEN");
    assert_eq!(contexts[1]["alias"], "ops");
    assert_eq!(contexts[1]["project_id_env"], "OPS_GCP_PROJECT");
}

#[test]
fn gcp_context_current_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[gcp]
default_account = "core"
default_env = "prod"

[gcp.accounts.core]
project_id_env = "CORE_PROJECT"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_PROJECT", "proj_core")
        .args(["gcp", "context", "current", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["environment"], "prod");
    assert_eq!(parsed["project_id"], "proj_core");
    assert_eq!(parsed["base_url"], "https://serviceusage.googleapis.com");
    assert_eq!(parsed["source"], "env:CORE_PROJECT");
}

#[test]
fn gcp_auth_status_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[gcp]
default_account = "core"
default_env = "staging"

[gcp.accounts.core]
project_id_env = "CORE_PROJECT"
access_token_env = "CORE_TOKEN"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_PROJECT", "proj_core")
        .env("CORE_TOKEN", "ya29.token-core-xyz")
        .args(["gcp", "auth", "status", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["environment"], "staging");
    assert_eq!(parsed["project_id"], "proj_core");
    assert_eq!(parsed["base_url"], "https://serviceusage.googleapis.com");
    assert_eq!(parsed["source"], "env:CORE_PROJECT,env:CORE_TOKEN");
    assert_eq!(parsed["token_preview"], "ya2*************xyz");
}

#[test]
fn google_places_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[google]
default_account = "core"

[google.accounts.core]
project_id = "proj_core"
default_language_code = "en"
default_region_code = "US"

[google.accounts.ops]
project_id = "proj_ops"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["google", "places", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "true");
    assert_eq!(contexts[0]["project"], "proj_core");
    assert_eq!(contexts[0]["language"], "en");
    assert_eq!(contexts[0]["region"], "US");
    assert_eq!(contexts[1]["alias"], "ops");
}

#[test]
fn google_places_context_current_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[google]
default_account = "core"
default_env = "prod"

[google.accounts.core]
project_id_env = "CORE_PROJECT"
places_api_key_env = "CORE_API_KEY"
default_language_code = "en"
default_region_code = "US"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_PROJECT", "proj_core")
        .env("CORE_API_KEY", "AIza.token.core.xyz")
        .args(["google", "places", "context", "current", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["project_id"], "proj_core");
    assert_eq!(parsed["environment"], "prod");
    assert_eq!(parsed["language_code"], "en");
    assert_eq!(parsed["region_code"], "US");
    assert_eq!(parsed["base_url"], "https://places.googleapis.com");
    assert_eq!(
        parsed["source"],
        "env:CORE_API_KEY,env:CORE_PROJECT,settings.default_language_code,settings.default_region_code"
    );
}

#[test]
fn google_places_auth_status_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[google]
default_account = "core"
default_env = "staging"

[google.accounts.core]
project_id_env = "CORE_PROJECT"
staging_places_api_key_env = "CORE_STAGING_KEY"
default_language_code = "en"
default_region_code = "GB"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_PROJECT", "proj_core")
        .env("CORE_STAGING_KEY", "AIza.staging-token-xyz")
        .args(["google", "places", "auth", "status", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["project_id"], "proj_core");
    assert_eq!(parsed["environment"], "staging");
    assert_eq!(parsed["language_code"], "en");
    assert_eq!(parsed["region_code"], "GB");
    assert_eq!(parsed["base_url"], "https://places.googleapis.com");
    assert_eq!(
        parsed["source"],
        "env:CORE_STAGING_KEY,env:CORE_PROJECT,settings.default_language_code,settings.default_region_code"
    );
    assert_eq!(parsed["key_preview"], "AIz****************xyz");
}

#[test]
fn openai_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[openai]
default_account = "core"

[openai.accounts.core]
api_key_env = "CORE_OPENAI_API_KEY"
admin_api_key_env = "CORE_OPENAI_ADMIN_API_KEY"
organization_id = "org_core"
project_id = "proj_core"

[openai.accounts.ops]
project_id_env = "OPS_OPENAI_PROJECT_ID"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["openai", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "true");
    assert_eq!(contexts[0]["api_key_env"], "CORE_OPENAI_API_KEY");
    assert_eq!(contexts[0]["admin_api_key_env"], "CORE_OPENAI_ADMIN_API_KEY");
    assert_eq!(contexts[0]["org_id"], "org_core");
    assert_eq!(contexts[0]["project_id"], "proj_core");
    assert_eq!(contexts[1]["alias"], "ops");
}

#[test]
fn openai_context_current_json_resolves_selected_account() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[openai]
default_account = "core"
default_project_id = "proj_default"

[openai.accounts.core]
api_key_env = "CORE_OPENAI_API_KEY"
organization_id_env = "CORE_OPENAI_ORG"
admin_api_key_env = "CORE_OPENAI_ADMIN_KEY"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .env("CORE_OPENAI_API_KEY", "sk-test")
        .env("CORE_OPENAI_ORG", "org_core")
        .env("CORE_OPENAI_ADMIN_KEY", "sk-admin")
        .args(["openai", "context", "current", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["account_alias"], "core");
    assert_eq!(parsed["base_url"], "https://api.openai.com");
    assert_eq!(parsed["organization_id"], "org_core");
    assert_eq!(parsed["project_id"], "proj_default");
    assert_eq!(parsed["admin_key_set"], true);
    assert_eq!(
        parsed["source"],
        "env:CORE_OPENAI_API_KEY,env:CORE_OPENAI_ADMIN_KEY,env:CORE_OPENAI_ORG,settings.default_project_id"
    );
}

#[test]
fn apple_appstore_auth_status_json_reads_local_inputs() {
    let key_file = tempdir().expect("tempdir");
    let key_path = key_file.path().join("AuthKey_TEST.p8");
    fs::write(&key_path, "-----BEGIN PRIVATE KEY-----").expect("write key");

    let output = cargo_bin()
        .args(["apple", "appstore", "auth", "status"])
        .args(["--account", "mobile"])
        .args(["--env", "staging"])
        .args(["--project-id", "proj_mobile"])
        .args(["--bundle-id", "com.example.mobile"])
        .args(["--locale", "fr-FR"])
        .args(["--platform", "MAC_OS"])
        .args(["--issuer-id", "issuer_123"])
        .args(["--key-id", "key_123"])
        .args(["--private-key-file", key_path.to_str().expect("utf8")])
        .args(["--verify=false", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status"], "ready");
    assert_eq!(parsed["account_alias"], "mobile");
    assert_eq!(parsed["environment"], "staging");
    assert_eq!(parsed["project_id"], "proj_mobile");
    assert_eq!(parsed["bundle_id"], "com.example.mobile");
    assert_eq!(parsed["locale"], "fr-FR");
    assert_eq!(parsed["platform"], "MAC_OS");
    assert_eq!(parsed["token_source"], "flag:--private-key-file");
}

#[test]
fn openai_auth_status_json_verifies_request() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/models?limit=1 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk-test\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_auth")],
            r#"{"data":[{"id":"gpt-4.1-mini","object":"model"}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "openai",
            "auth",
            "status",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status"], "ready");
    assert_eq!(parsed["verify_status"], 200);
    assert_eq!(parsed["verify"]["data"][0]["id"], "gpt-4.1-mini");
    server.join();
}

#[test]
fn openai_model_list_json_fetches_from_api() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/models?limit=1 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk-test\r\n"));
        assert!(request.contains("openai-organization: org_core\r\n"));
        assert!(request.contains("openai-project: proj_core\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_models")],
            r#"{"data":[{"id":"gpt-4.1-mini","object":"model"}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "openai",
            "model",
            "list",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--org-id",
            "org_core",
            "--project-id",
            "proj_core",
            "--limit",
            "1",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_models");
    assert_eq!(parsed["data"]["data"][0]["id"], "gpt-4.1-mini");
    server.join();
}

#[test]
fn openai_model_get_text_formats_response() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/models/gpt-test HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk-test\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_model")],
            r#"{"id":"gpt-test","object":"model"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "openai",
            "model",
            "get",
            "gpt-test",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let rendered = String::from_utf8_lossy(&output);
    assert!(rendered.contains("Status: 200 200 OK"));
    assert!(rendered.contains("Request ID: req_model"));
    assert!(rendered.contains("\"id\": \"gpt-test\""));
    server.join();
}

#[test]
fn openai_project_list_json_fetches_from_api_with_admin_key() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /v1/organization/projects?limit=1&include_archived=true HTTP/1.1\r\n"
        ));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_projects")],
            r#"{"data":[{"id":"proj_123","name":"Core"}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "openai",
            "project",
            "list",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--limit",
            "1",
            "--include-archived",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_projects");
    assert_eq!(parsed["data"]["data"][0]["id"], "proj_123");
    server.join();
}

#[test]
fn openai_project_get_text_formats_response() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/organization/projects/proj_123 HTTP/1.1\r\n"));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_project")],
            r#"{"id":"proj_123","name":"Core"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "openai",
            "project",
            "get",
            "proj_123",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let rendered = String::from_utf8_lossy(&output);
    assert!(rendered.contains("Status: 200 200 OK"));
    assert!(rendered.contains("Request ID: req_project"));
    assert!(rendered.contains("\"id\": \"proj_123\""));
    server.join();
}

#[test]
fn openai_project_api_key_list_json_fetches_from_api_with_admin_key() {
    let server =
        start_one_shot_http_server(|request| {
            assert!(request.starts_with(
                "GET /v1/organization/projects/proj_123/api_keys?limit=1 HTTP/1.1\r\n"
            ));
            assert!(request.contains("authorization: Bearer sk-admin\r\n"));
            http_json_response(
                "200 OK",
                &[("x-request-id", "req_keys")],
                r#"{"data":[{"id":"key_123","name":"Deploy"}]}"#,
            )
        });

    let output = cargo_bin()
        .args([
            "openai",
            "project",
            "api-key",
            "list",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--project-id",
            "proj_123",
            "--limit",
            "1",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_keys");
    assert_eq!(parsed["data"]["data"][0]["id"], "key_123");
    server.join();
}

#[test]
fn openai_project_api_key_get_text_formats_response() {
    let server =
        start_one_shot_http_server(|request| {
            assert!(request.starts_with(
                "GET /v1/organization/projects/proj_123/api_keys/key_123 HTTP/1.1\r\n"
            ));
            assert!(request.contains("authorization: Bearer sk-admin\r\n"));
            http_json_response(
                "200 OK",
                &[("x-request-id", "req_key")],
                r#"{"id":"key_123","name":"Deploy"}"#,
            )
        });

    let output = cargo_bin()
        .args([
            "openai",
            "project",
            "api-key",
            "get",
            "key_123",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--project-id",
            "proj_123",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let rendered = String::from_utf8_lossy(&output);
    assert!(rendered.contains("Status: 200 200 OK"));
    assert!(rendered.contains("Request ID: req_key"));
    assert!(rendered.contains("\"id\": \"key_123\""));
    server.join();
}

#[test]
fn openai_project_service_account_list_json_fetches_from_api_with_admin_key() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /v1/organization/projects/proj_123/service_accounts?limit=1 HTTP/1.1\r\n"
        ));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_service_accounts")],
            r#"{"data":[{"id":"sa_123","name":"Deploy"}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "openai",
            "project",
            "service-account",
            "list",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--project-id",
            "proj_123",
            "--limit",
            "1",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_service_accounts");
    assert_eq!(parsed["data"]["data"][0]["id"], "sa_123");
    server.join();
}

#[test]
fn openai_project_service_account_get_text_formats_response() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /v1/organization/projects/proj_123/service_accounts/sa_123 HTTP/1.1\r\n"
        ));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_service_account")],
            r#"{"id":"sa_123","name":"Deploy"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "openai",
            "project",
            "service-account",
            "get",
            "sa_123",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--project-id",
            "proj_123",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let rendered = String::from_utf8_lossy(&output);
    assert!(rendered.contains("Status: 200 200 OK"));
    assert!(rendered.contains("Request ID: req_service_account"));
    assert!(rendered.contains("\"id\": \"sa_123\""));
    server.join();
}

#[test]
fn openai_project_rate_limit_list_json_fetches_from_api_with_admin_key() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /v1/organization/projects/proj_123/rate_limits?limit=1&after=cursor HTTP/1.1\r\n"
        ));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_rate_limits")],
            r#"{"data":[{"id":"rl_123","max_requests_per_1_minute":60}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "openai",
            "project",
            "rate-limit",
            "list",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--project-id",
            "proj_123",
            "--limit",
            "1",
            "--after",
            "cursor",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_rate_limits");
    assert_eq!(parsed["data"]["data"][0]["id"], "rl_123");
    server.join();
}

#[test]
fn openai_key_list_json_fetches_from_api_with_admin_key() {
    let server = start_one_shot_http_server(|request| {
        assert!(
            request
                .starts_with("GET /v1/organization/admin_api_keys?limit=1&order=desc HTTP/1.1\r\n")
        );
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_admin_keys")],
            r#"{"data":[{"id":"adminkey_123","name":"Core"}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "openai",
            "key",
            "list",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--limit",
            "1",
            "--order",
            "desc",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_admin_keys");
    assert_eq!(parsed["data"]["data"][0]["id"], "adminkey_123");
    server.join();
}

#[test]
fn openai_key_get_text_formats_response() {
    let server = start_one_shot_http_server(|request| {
        assert!(
            request.starts_with("GET /v1/organization/admin_api_keys/adminkey_123 HTTP/1.1\r\n")
        );
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_admin_key")],
            r#"{"id":"adminkey_123","name":"Core"}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "openai",
            "key",
            "get",
            "adminkey_123",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let rendered = String::from_utf8_lossy(&output);
    assert!(rendered.contains("Status: 200 200 OK"));
    assert!(rendered.contains("Request ID: req_admin_key"));
    assert!(rendered.contains("\"id\": \"adminkey_123\""));
    server.join();
}

#[test]
fn openai_usage_completions_json_fetches_from_api_with_admin_key() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/organization/usage/completions?start_time="));
        assert!(request.contains("&limit=1"));
        assert!(request.contains("&models=gpt-5-codex"));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_usage")],
            r#"{"data":[{"object":"bucket","results":[{"input_tokens":42}]}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "openai",
            "usage",
            "completions",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--limit",
            "1",
            "--model",
            "gpt-5-codex",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_usage");
    assert_eq!(parsed["data"]["data"][0]["object"], "bucket");
    server.join();
}

#[test]
fn openai_codex_usage_json_defaults_codex_model() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/organization/usage/completions?start_time="));
        assert!(request.contains("&limit=1"));
        assert!(request.contains("&models=gpt-5-codex"));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_codex_usage")],
            r#"{"data":[{"object":"bucket","results":[{"input_tokens":7}]}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "openai",
            "codex",
            "usage",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--limit",
            "1",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_codex_usage");
    assert_eq!(parsed["data"]["data"][0]["object"], "bucket");
    server.join();
}

#[test]
fn openai_monitor_usage_json_defaults_to_completions() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with("GET /v1/organization/usage/completions?start_time="));
        assert!(request.contains("&limit=1"));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_monitor_usage")],
            r#"{"data":[{"object":"bucket","results":[{"input_tokens":11}]}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "openai",
            "monitor",
            "usage",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--limit",
            "1",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_monitor_usage");
    assert_eq!(parsed["data"]["data"][0]["object"], "bucket");
    server.join();
}

#[test]
fn openai_monitor_limits_json_fetches_project_rate_limits() {
    let server = start_one_shot_http_server(|request| {
        assert!(request.starts_with(
            "GET /v1/organization/projects/proj_123/rate_limits?limit=1 HTTP/1.1\r\n"
        ));
        assert!(request.contains("authorization: Bearer sk-admin\r\n"));
        http_json_response(
            "200 OK",
            &[("x-request-id", "req_monitor_limits")],
            r#"{"data":[{"id":"rl_456","max_requests_per_1_minute":120}]}"#,
        )
    });

    let output = cargo_bin()
        .args([
            "openai",
            "monitor",
            "limits",
            "--base-url",
            &server.base_url,
            "--api-key",
            "sk-test",
            "--admin-api-key",
            "sk-admin",
            "--project-id",
            "proj_123",
            "--limit",
            "1",
            "--json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status_code"], 200);
    assert_eq!(parsed["request_id"], "req_monitor_limits");
    assert_eq!(parsed["data"]["data"][0]["id"], "rl_456");
    server.join();
}

#[test]
fn oci_context_list_json_reads_settings_accounts() {
    let home = tempdir().expect("tempdir");
    let settings_dir = home.path().join(".si");
    fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
    fs::write(
        settings_dir.join("settings.toml"),
        r#"
schema_version = 1

[oci]
default_account = "core"
profile = "DEFAULT"

[oci.accounts.core]
region = "us-phoenix-1"
config_file = "/tmp/core-config"

[oci.accounts.ops]
profile = "OPS"
"#,
    )
    .expect("write settings");

    let output = cargo_bin()
        .args(["oci", "context", "list", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let contexts = parsed["contexts"].as_array().expect("contexts array");
    assert_eq!(contexts[0]["alias"], "core");
    assert_eq!(contexts[0]["default"], "true");
    assert_eq!(contexts[0]["profile"], "DEFAULT");
    assert_eq!(contexts[0]["region"], "us-phoenix-1");
    assert_eq!(contexts[0]["config_file"], "/tmp/core-config");
    assert_eq!(contexts[1]["alias"], "ops");
}

#[test]
fn oci_context_current_json_reads_profile_config() {
    let config_dir = tempdir().expect("tempdir");
    let config_file = config_dir.path().join("config");
    fs::write(&config_file, "[DEFAULT]\ntenancy=ocid1.tenancy.oc1..example\nregion=us-phoenix-1\n")
        .expect("write config");

    let output = cargo_bin()
        .args(["oci", "context", "current"])
        .args(["--config-file", config_file.to_str().expect("utf8")])
        .arg("--format")
        .arg("json")
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["profile"], "DEFAULT");
    assert_eq!(parsed["region"], "us-phoenix-1");
    assert_eq!(parsed["auth_style"], "signature");
    assert_eq!(parsed["tenancy_ocid"], "ocid1.tenancy.oc1..example");
    assert_eq!(parsed["source"], "profile:DEFAULT");
}

#[test]
fn oci_auth_status_json_reads_local_signature_context() {
    let config_dir = tempdir().expect("tempdir");
    let config_file = config_dir.path().join("config");
    let key_dir = config_dir.path().join("keys");
    let key_file = key_dir.join("oci.pem");
    fs::create_dir_all(&key_dir).expect("mkdir key dir");
    fs::write(
        &config_file,
        "[DEFAULT]\ntenancy=ocid1.tenancy.oc1..example\nuser=ocid1.user.oc1..example\nfingerprint=aa:bb:cc\nkey_file=keys/oci.pem\nregion=us-phoenix-1\n",
    )
    .expect("write config");
    fs::write(&key_file, "dummy-private-key").expect("write key");

    let output = cargo_bin()
        .args(["oci", "auth", "status"])
        .args(["--config-file", config_file.to_str().expect("utf8")])
        .args(["--verify=false", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["status"], "ready");
    assert_eq!(parsed["profile"], "DEFAULT");
    assert_eq!(parsed["region"], "us-phoenix-1");
    assert_eq!(parsed["auth_style"], "signature");
    assert_eq!(parsed["tenancy_ocid"], "ocid1.tenancy.oc1..example");
    assert_eq!(parsed["user_ocid"], "ocid1.user.oc1..example");
    assert_eq!(parsed["fingerprint"], "aa:bb:cc");
    assert_eq!(parsed["source"], "profile:DEFAULT");
}

#[test]
fn oci_oracular_tenancy_json_reads_profile_config() {
    let config_dir = tempdir().expect("tempdir");
    let config_file = config_dir.path().join("config");
    fs::write(
        &config_file,
        "[DEFAULT]\ntenancy=ocid1.tenancy.oc1..example\nregion=us-phoenix-1\n",
    )
    .expect("write config");

    let output = cargo_bin()
        .args(["oci", "oracular", "tenancy"])
        .args(["--config-file", config_file.to_str().expect("utf8")])
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["profile"], "DEFAULT");
    assert_eq!(parsed["config_file"], config_file.to_str().expect("utf8"));
    assert_eq!(parsed["tenancy_ocid"], "ocid1.tenancy.oc1..example");
}

#[test]
fn dyad_spawn_plan_json_defaults_names_and_volumes() {
    let workspace = tempdir().expect("tempdir");
    let home = tempdir().expect("tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir .si");

    let output = cargo_bin()
        .args(["dyad", "spawn-plan", "--name", "alpha", "--workspace"])
        .arg(workspace.path())
        .args(["--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["dyad"], "alpha");
    assert_eq!(parsed["role"], "generic");
    assert_eq!(parsed["codex_volume"], "si-codex-alpha");
    assert_eq!(parsed["skills_volume"], "si-codex-skills");
    assert_eq!(parsed["actor"]["container_name"], "si-actor-alpha");
    assert_eq!(parsed["critic"]["container_name"], "si-critic-alpha");
}

#[test]
fn dyad_spawn_plan_json_includes_critic_configs_and_loop_env() {
    let workspace = tempdir().expect("tempdir");
    let configs = tempdir().expect("tempdir");
    let home = tempdir().expect("tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir .si");

    let output = cargo_bin()
        .args(["dyad", "spawn-plan", "--name", "alpha", "--role", "ios", "--workspace"])
        .arg(workspace.path())
        .args(["--configs"])
        .arg(configs.path())
        .args(["--profile-id", "ferma", "--loop-enabled", "true", "--loop-goal", "ship", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["role"], "ios");
    assert!(
        parsed["actor"]["env"]
            .as_array()
            .expect("actor env")
            .iter()
            .any(|value| value == "SI_CODEX_PROFILE_ID=ferma")
    );
    assert!(
        parsed["critic"]["env"]
            .as_array()
            .expect("critic env")
            .iter()
            .any(|value| value == "DYAD_LOOP_ENABLED=true")
    );
    assert!(
        parsed["critic"]["bind_mounts"]
            .as_array()
            .expect("critic bind mounts")
            .iter()
            .any(|mount| mount["target"] == "/configs")
    );
}

#[test]
fn dyad_spawn_spec_json_includes_actor_ports_and_critic_configs_mount() {
    let workspace = tempdir().expect("tempdir");
    let configs = tempdir().expect("tempdir");
    let home = tempdir().expect("tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir .si");

    let output = cargo_bin()
        .args(["dyad", "spawn-spec", "--name", "alpha", "--workspace"])
        .arg(workspace.path())
        .args(["--configs"])
        .arg(configs.path())
        .args(["--forward-ports", "1455-1456", "--home"])
        .arg(home.path())
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["actor"]["name"], "si-actor-alpha");
    assert_eq!(parsed["critic"]["name"], "si-critic-alpha");
    assert_eq!(parsed["actor"]["published_ports"].as_array().expect("ports").len(), 2);
    assert!(
        parsed["critic"]["bind_mounts"]
            .as_array()
            .expect("critic bind mounts")
            .iter()
            .any(|mount| mount["target"] == "/configs")
    );
}

#[test]
fn dyad_spawn_start_executes_actor_and_critic_docker_commands() {
    let workspace = tempdir().expect("tempdir");
    let configs = tempdir().expect("tempdir");
    let home = tempdir().expect("tempdir");
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir .si");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" >> '{}'\nprintf '%s\\n' '--' >> '{}'\nprintf '%s\\n' 'container-id'\n",
            args_path.display(),
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["dyad", "spawn-start", "--name", "alpha", "--workspace"])
        .arg(workspace.path())
        .args(["--configs"])
        .arg(configs.path())
        .args(["--forward-ports", "1455-1456", "--home"])
        .arg(home.path())
        .args(["--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert!(text.contains("container-id"));
    let args = fs::read_to_string(args_path).expect("args file");
    assert!(args.contains("si-actor-alpha"));
    assert!(args.contains("si-critic-alpha"));
    assert!(args.contains("127.0.0.1::1455"));
    assert!(args.contains("--label"));
    assert!(args.contains("si.dyad=alpha"));
}

#[test]
fn dyad_start_executes_actor_and_critic_docker_start() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" >> '{}'\nprintf '%s\\n' '--' >> '{}'\nprintf '%s\\n' 'started'\n",
            args_path.display(),
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["dyad", "start", "alpha", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert!(text.contains("started"));
    let args = fs::read_to_string(args_path).expect("args file");
    assert!(args.contains("start"));
    assert!(args.contains("si-actor-alpha"));
    assert!(args.contains("si-critic-alpha"));
}

#[test]
fn dyad_stop_executes_actor_and_critic_docker_stop() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" >> '{}'\nprintf '%s\\n' '--' >> '{}'\nprintf '%s\\n' 'stopped'\n",
            args_path.display(),
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["dyad", "stop", "alpha", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert!(text.contains("stopped"));
    let args = fs::read_to_string(args_path).expect("args file");
    assert!(args.contains("stop"));
    assert!(args.contains("si-actor-alpha"));
    assert!(args.contains("si-critic-alpha"));
}

#[test]
fn dyad_logs_executes_docker_logs_for_selected_member() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > '{}'\nprintf '%s\\n' 'critic logs'\n",
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["dyad", "logs", "alpha", "--member", "critic", "--tail", "50", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert!(text.contains("critic logs"));
    let args = fs::read_to_string(args_path).expect("args file");
    assert!(args.contains("logs"));
    assert!(args.contains("--tail"));
    assert!(args.contains("50"));
    assert!(args.contains("si-critic-alpha"));
}

#[test]
fn dyad_logs_json_wraps_selected_member_output() {
    let script_dir = tempdir().expect("tempdir");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(&docker_bin, "#!/bin/sh\nprintf '%s\\n' 'critic logs'\n");

    let output = cargo_bin()
        .args([
            "dyad",
            "logs",
            "alpha",
            "--member",
            "critic",
            "--tail",
            "50",
            "--format",
            "json",
            "--docker-bin",
        ])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["dyad"], "alpha");
    assert_eq!(parsed["member"], "critic");
    assert_eq!(parsed["tail"], 50);
    assert_eq!(parsed["logs"], "critic logs\n");
}

#[test]
fn dyad_list_json_groups_actor_and_critic_rows() {
    let script_dir = tempdir().expect("tempdir");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        "#!/bin/sh\nprintf '%s\\n' 'si-actor-alpha\trunning\tactor-id\talpha\tios\tactor'\nprintf '%s\\n' 'si-critic-alpha\texited\tcritic-id\talpha\tios\tcritic'\n",
    );

    let output = cargo_bin()
        .args(["dyad", "list", "--format", "json", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let rows = parsed.as_array().expect("rows");
    assert_eq!(rows.len(), 1);
    assert_eq!(rows[0]["dyad"], "alpha");
    assert_eq!(rows[0]["role"], "ios");
    assert_eq!(rows[0]["actor"], "running");
    assert_eq!(rows[0]["critic"], "exited");
}

#[test]
fn dyad_status_json_reports_member_states() {
    let script_dir = tempdir().expect("tempdir");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        "#!/bin/sh\nprintf '%s\\n' 'si-actor-alpha\trunning\tactor-id\talpha\tios\tactor'\nprintf '%s\\n' 'si-critic-alpha\texited\tcritic-id\talpha\tios\tcritic'\n",
    );

    let output = cargo_bin()
        .args(["dyad", "status", "alpha", "--format", "json", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["dyad"], "alpha");
    assert_eq!(parsed["found"], true);
    assert_eq!(parsed["actor"]["name"], "si-actor-alpha");
    assert_eq!(parsed["actor"]["status"], "running");
    assert_eq!(parsed["critic"]["name"], "si-critic-alpha");
    assert_eq!(parsed["critic"]["status"], "exited");
}

#[test]
fn dyad_peek_plan_json_defaults_session_and_attach_commands() {
    let output = cargo_bin()
        .args(["dyad", "peek-plan", "alpha", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["dyad"], "alpha");
    assert_eq!(parsed["member"], "both");
    assert_eq!(parsed["actor_container_name"], "si-actor-alpha");
    assert_eq!(parsed["critic_container_name"], "si-critic-alpha");
    assert_eq!(parsed["peek_session_name"], "si-dyad-peek-alpha");
    assert!(
        parsed["actor_attach_command"]
            .as_str()
            .unwrap_or("")
            .contains("docker exec si-actor-alpha tmux has-session -t si-dyad-alpha-actor")
    );
    assert!(
        parsed["critic_attach_command"]
            .as_str()
            .unwrap_or("")
            .contains("docker exec -it si-critic-alpha tmux attach -t si-dyad-alpha-critic")
    );
}

#[test]
fn dyad_peek_plan_json_honors_member_and_session_override() {
    let output = cargo_bin()
        .args([
            "dyad",
            "peek-plan",
            "alpha",
            "--member",
            "critic",
            "--session",
            "peek-main",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["member"], "critic");
    assert_eq!(parsed["peek_session_name"], "peek-main");
    assert_eq!(parsed["critic_session_name"], "si-dyad-alpha-critic");
}

#[test]
fn dyad_restart_executes_actor_and_critic_docker_restart() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" >> '{}'\nprintf '%s\\n' '--' >> '{}'\nprintf '%s\\n' 'restarted'\n",
            args_path.display(),
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["dyad", "restart", "alpha", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert!(text.contains("restarted"));
    let args = fs::read_to_string(args_path).expect("args file");
    assert!(args.contains("restart"));
    assert!(args.contains("si-actor-alpha"));
    assert!(args.contains("si-critic-alpha"));
}

#[test]
fn dyad_remove_executes_actor_and_critic_docker_rm() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" >> '{}'\nprintf '%s\\n' '--' >> '{}'\nprintf '%s\\n' 'removed'\n",
            args_path.display(),
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["dyad", "remove", "alpha", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert!(text.contains("removed"));
    let args = fs::read_to_string(args_path).expect("args file");
    assert!(args.contains("rm"));
    assert!(args.contains("-f"));
    assert!(args.contains("si-actor-alpha"));
    assert!(args.contains("si-critic-alpha"));
}

#[test]
fn dyad_lifecycle_smoke_works_with_fake_docker() {
    let workspace = tempdir().expect("tempdir");
    let configs = tempdir().expect("tempdir");
    let home = tempdir().expect("tempdir");
    let script_dir = tempdir().expect("tempdir");
    let state_path = script_dir.path().join("state.txt");
    let docker_bin = script_dir.path().join("docker");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir .si");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nSTATE='{}'\ncmd=\"$1\"\nshift\ncase \"$cmd\" in\n  run)\n    printf '%s\\n' 'running' > \"$STATE\"\n    printf '%s\\n' 'container-id'\n    ;;\n  ps)\n    state='missing'\n    if [ -f \"$STATE\" ]; then state=$(tr -d '\\n' < \"$STATE\"); fi\n    if [ \"$state\" = 'removed' ] || [ \"$state\" = 'missing' ]; then exit 0; fi\n    actor_state=\"$state\"\n    critic_state=\"$state\"\n    printf '%s\\n' 'si-actor-alpha\t'\"$actor_state\"'\tactor-id\talpha\tios\tactor'\n    printf '%s\\n' 'si-critic-alpha\t'\"$critic_state\"'\tcritic-id\talpha\tios\tcritic'\n    ;;\n  logs)\n    printf '%s\\n' 'critic logs'\n    ;;\n  start)\n    printf '%s\\n' 'running' > \"$STATE\"\n    printf '%s\\n' 'started'\n    ;;\n  stop)\n    printf '%s\\n' 'exited' > \"$STATE\"\n    printf '%s\\n' 'stopped'\n    ;;\n  rm)\n    printf '%s\\n' 'removed' > \"$STATE\"\n    printf '%s\\n' 'removed'\n    ;;\n  *)\n    printf 'unexpected docker command: %s\\n' \"$cmd\" >&2\n    exit 1\n    ;;\nesac\n",
            state_path.display()
        ),
    );

    cargo_bin()
        .args(["dyad", "spawn-start", "--name", "alpha", "--workspace"])
        .arg(workspace.path())
        .args(["--configs"])
        .arg(configs.path())
        .args(["--home"])
        .arg(home.path())
        .args(["--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success();
    assert_eq!(fs::read_to_string(&state_path).expect("state"), "running\n");

    let status_output = cargo_bin()
        .args(["dyad", "status", "alpha", "--format", "json", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let status: Value = serde_json::from_slice(&status_output).expect("json output");
    assert_eq!(status["found"], true);
    assert_eq!(status["actor"]["status"], "running");
    assert_eq!(status["critic"]["status"], "running");

    let logs_output = cargo_bin()
        .args(["dyad", "logs", "alpha", "--member", "critic", "--tail", "10", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert!(String::from_utf8(logs_output).expect("utf8 output").contains("critic logs"));

    cargo_bin().args(["dyad", "stop", "alpha", "--docker-bin"]).arg(&docker_bin).assert().success();
    assert_eq!(fs::read_to_string(&state_path).expect("state"), "exited\n");

    let stopped_status_output = cargo_bin()
        .args(["dyad", "status", "alpha", "--format", "json", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let stopped_status: Value =
        serde_json::from_slice(&stopped_status_output).expect("json output");
    assert_eq!(stopped_status["actor"]["status"], "exited");
    assert_eq!(stopped_status["critic"]["status"], "exited");

    cargo_bin()
        .args(["dyad", "start", "alpha", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success();
    assert_eq!(fs::read_to_string(&state_path).expect("state"), "running\n");

    cargo_bin()
        .args(["dyad", "remove", "alpha", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success();
    assert_eq!(fs::read_to_string(&state_path).expect("state"), "removed\n");
}

#[test]
fn dyad_exec_executes_docker_exec_for_selected_member() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > '{}'\nprintf '%s\\n' 'exec-ok'\n",
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["dyad", "exec", "alpha", "--member", "critic", "--tty=true", "--docker-bin"])
        .arg(&docker_bin)
        .args(["--", "bash", "-lc", "echo hi"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert!(text.contains("exec-ok"));
    let args = fs::read_to_string(args_path).expect("args file");
    assert!(args.contains("exec"));
    assert!(args.contains("-it"));
    assert!(args.contains("si-critic-alpha"));
    assert!(args.contains("bash"));
}

#[test]
fn dyad_cleanup_removes_only_stopped_members() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nif [ \"$1\" = \"ps\" ]; then\n  printf '%s\\n' 'si-actor-alpha\trunning\tactor-id\talpha\tios\tactor'\n  printf '%s\\n' 'si-critic-alpha\texited\tcritic-id\talpha\tios\tcritic'\n  printf '%s\\n' 'si-actor-beta\tdead\tactor2-id\tbeta\tgeneric\tactor'\n  exit 0\nfi\nprintf '%s\\n' \"$@\" >> '{}'\nprintf '%s\\n' '--' >> '{}'\n",
            args_path.display(),
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["dyad", "cleanup", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert!(text.contains("removed=2"));
    let args = fs::read_to_string(args_path).expect("args file");
    assert!(args.contains("rm"));
    assert!(args.contains("si-critic-alpha"));
    assert!(args.contains("si-actor-beta"));
    assert!(!args.contains("si-actor-alpha"));
}

#[test]
fn paths_show_uses_home_defaults() {
    let home = tempdir().expect("tempdir");
    let output = cargo_bin()
        .args(["paths", "show", "--format", "json", "--home"])
        .arg(home.path())
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["root"], path_string(home.path().join(".si")));
    assert_eq!(parsed["settings_file"], path_string(home.path().join(".si/settings.toml")));
    assert_eq!(parsed["codex_profiles_dir"], path_string(home.path().join(".si/codex/profiles")));
}

#[test]
fn paths_show_honors_settings_override() {
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
        .args(["paths", "show", "--format", "json", "--home"])
        .arg(home.path())
        .args(["--settings-file"])
        .arg(&settings_path)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["root"], path_string(home.path().join("state/si")));
    assert_eq!(parsed["settings_file"], path_string(home.path().join("config/si/settings.toml")));
    assert_eq!(parsed["codex_profiles_dir"], path_string(home.path().join("state/si/profiles")));
}

#[test]
fn fort_session_state_show_reads_and_normalizes_persisted_state() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(
        &state_path,
        r#"{
  "profile_id": " ferma ",
  "agent_id": " agent-ferma ",
  "session_id": " session-123 ",
  "host": " https://fort.example.test ",
  "container_host": " http://fort.internal:8088 ",
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
        .args(["fort", "session-state", "show", "--path"])
        .arg(&state_path)
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["profile_id"], "ferma");
    assert_eq!(parsed["agent_id"], "agent-ferma");
    assert_eq!(parsed["session_id"], "session-123");
    assert_eq!(parsed["host"], "https://fort.example.test");
    assert_eq!(parsed["container_host"], "http://fort.internal:8088");
}

#[test]
fn fort_session_state_classify_reports_refreshing_state() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(
        &state_path,
        r#"{
  "profile_id": "ferma",
  "agent_id": "agent-ferma",
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
        .args(["fort", "session-state", "classify", "--path"])
        .arg(&state_path)
        .args(["--now-unix", "100", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["Refreshing"]["profile_id"], "ferma");
    assert_eq!(parsed["Refreshing"]["agent_id"], "agent-ferma");
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
  "profile_id": " ferma ",
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
        .args(["fort", "runtime-agent-state", "show", "--path"])
        .arg(&state_path)
        .args(["--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["profile_id"], "ferma");
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
            "session-state",
            "write",
            "--path",
        ])
        .arg(&state_path)
        .args([
            "--state-json",
            r#"{"profile_id":" ferma ","agent_id":" agent-ferma ","session_id":" session-123 ","host":" https://fort.example.test "}"#,
        ])
        .assert()
        .success();

    let raw = fs::read_to_string(&state_path).expect("read persisted state");
    let parsed: Value = serde_json::from_str(&raw).expect("json");
    assert_eq!(parsed["profile_id"], "ferma");
    assert_eq!(parsed["agent_id"], "agent-ferma");
    assert_eq!(parsed["session_id"], "session-123");
    assert_eq!(parsed["host"], "https://fort.example.test");
}

#[test]
fn fort_runtime_agent_state_write_persists_normalized_json() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("runtime-agent.json");

    cargo_bin()
        .args(["fort", "runtime-agent-state", "write", "--path"])
        .arg(&state_path)
        .args(["--state-json", r#"{"profile_id":" ferma ","pid":4242,"command_path":" /tmp/si "}"#])
        .assert()
        .success();

    let raw = fs::read_to_string(&state_path).expect("read persisted runtime state");
    let parsed: Value = serde_json::from_str(&raw).expect("json");
    assert_eq!(parsed["profile_id"], "ferma");
    assert_eq!(parsed["pid"], 4242);
    assert_eq!(parsed["command_path"], "/tmp/si");
}

#[test]
fn fort_session_state_clear_removes_persisted_file() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(&state_path, "{}\n").expect("write session state");

    cargo_bin()
        .args(["fort", "session-state", "clear", "--path"])
        .arg(&state_path)
        .assert()
        .success();

    assert!(!state_path.exists());
}

#[test]
fn fort_session_state_bootstrap_view_normalizes_fallbacks() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(
        &state_path,
        r#"{
  "profile_id": " ferma ",
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
        .args(["fort", "session-state", "bootstrap-view", "--path"])
        .arg(&state_path)
        .args([
            "--access-token-path",
            "/tmp/access.token",
            "--refresh-token-path",
            "/tmp/refresh.token",
            "--access-token-container-path",
            "/home/si/.si/access.token",
            "--refresh-token-container-path",
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
    assert_eq!(parsed["profile_id"], "ferma");
    assert_eq!(parsed["agent_id"], "si-codex-ferma");
    assert_eq!(parsed["container_host_url"], "http://host.docker.internal:8088/");
    assert_eq!(parsed["access_token_container_path"], "/home/si/.si/access.token");
}

#[test]
fn fort_runtime_agent_state_clear_removes_persisted_file() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("runtime-agent.json");
    fs::write(&state_path, "{}\n").expect("write runtime agent state");

    cargo_bin()
        .args(["fort", "runtime-agent-state", "clear", "--path"])
        .arg(&state_path)
        .assert()
        .success();

    assert!(!state_path.exists());
}

#[test]
fn fort_session_state_refresh_outcome_returns_updated_state_and_classification() {
    let state_dir = tempdir().expect("tempdir");
    let state_path = state_dir.path().join("session.json");
    fs::write(
        &state_path,
        r#"{
  "profile_id": "ferma",
  "agent_id": "agent-ferma",
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
        .args(["fort", "session-state", "refresh-outcome", "--path"])
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
  "profile_id": "ferma",
  "agent_id": "agent-ferma",
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
        .args(["fort", "session-state", "teardown", "--path"])
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
  "profile_id": "ferma",
  "agent_id": "agent-ferma",
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
        .args(["fort", "session-state", "refresh-outcome", "--path"])
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
fn vault_trust_lookup_reports_matching_entry() {
    let store_dir = tempdir().expect("tempdir");
    let store_path = store_dir.path().join("trust.json");
    fs::write(
        &store_path,
        r#"{
  "schema_version": 3,
  "entries": [
    {
      "repo_root": "/repo",
      "file": "/repo/.env",
      "fingerprint": "deadbeef",
      "trusted_at": "2030-01-01T00:00:00Z"
    }
  ]
}
"#,
    )
    .expect("write trust store");

    let output = cargo_bin()
        .args(["vault", "trust", "lookup", "--path"])
        .arg(&store_path)
        .args([
            "--repo-root",
            "/repo",
            "--file",
            "/repo/.env",
            "--fingerprint",
            "deadbeef",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["found"], true);
    assert_eq!(parsed["matches"], true);
    assert_eq!(parsed["stored_fingerprint"], "deadbeef");
    assert_eq!(parsed["trusted_at"], "2030-01-01T00:00:00Z");
}

#[test]
fn vault_trust_lookup_reports_missing_entry() {
    let store_dir = tempdir().expect("tempdir");
    let store_path = store_dir.path().join("missing.json");

    let output = cargo_bin()
        .args(["vault", "trust", "lookup", "--path"])
        .arg(&store_path)
        .args([
            "--repo-root",
            "/repo",
            "--file",
            "/repo/.env",
            "--fingerprint",
            "deadbeef",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["found"], false);
    assert_eq!(parsed["matches"], false);
}

#[test]
fn codex_spawn_plan_json_defaults_profile_name_and_workdir() {
    let workspace = tempdir().expect("tempdir");
    let output = cargo_bin()
        .args(["codex", "spawn-plan", "--profile-id", "ferma", "--workspace"])
        .arg(workspace.path())
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["name"], "ferma");
    assert_eq!(parsed["container_name"], "si-codex-ferma");
    assert_eq!(parsed["workspace_mirror_target"], path_string(workspace.path()));
    assert_eq!(parsed["workdir"], path_string(workspace.path()));
    assert_eq!(parsed["codex_volume"], "si-codex-ferma");
    assert_eq!(parsed["skills_volume"], "si-codex-skills");
    assert_eq!(parsed["gh_volume"], "si-gh-ferma");
}

#[test]
fn codex_spawn_plan_json_includes_repo_pat_env_and_host_mounts() {
    let home = tempdir().expect("tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir .si");
    let workspace = tempdir().expect("tempdir");
    let output = cargo_bin()
        .args(["codex", "spawn-plan", "--name", "darmstada", "--workspace"])
        .arg(workspace.path())
        .args(["--repo", "acme/repo", "--gh-pat", "token-123", "--home"])
        .arg(home.path())
        .args(["--env", "EXTRA=1"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let env = parsed["env"].as_array().expect("env array");
    assert!(env.iter().any(|value| value == "SI_REPO=acme/repo"));
    assert!(env.iter().any(|value| value == "SI_GH_PAT=token-123"));
    assert!(env.iter().any(|value| value == "GH_TOKEN=token-123"));
    assert!(env.iter().any(|value| value == "GITHUB_TOKEN=token-123"));
    assert!(env.iter().any(|value| value == "EXTRA=1"));

    let mounts = parsed["mounts"].as_array().expect("mounts array");
    assert!(mounts.iter().any(|mount| mount["target"] == "/workspace"));
    assert!(mounts.iter().any(|mount| mount["target"] == "/home/si/.si"));
}

#[test]
fn codex_spawn_plan_uses_env_host_context_when_flags_are_omitted() {
    let home = tempdir().expect("tempdir");
    fs::create_dir_all(home.path().join(".si")).expect("mkdir .si");
    let workspace = tempdir().expect("tempdir");
    let output = cargo_bin()
        .env("HOME", home.path())
        .args(["codex", "spawn-plan", "--name", "einsteina", "--workspace"])
        .arg(workspace.path())
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let mounts = parsed["mounts"].as_array().expect("mounts array");
    assert!(mounts.iter().any(|mount| mount["target"] == "/home/si/.si"));
}

#[test]
fn codex_spawn_spec_json_includes_named_volumes_and_command() {
    let workspace = tempdir().expect("tempdir");
    let output = cargo_bin()
        .args(["codex", "spawn-spec", "--name", "ferma", "--workspace"])
        .arg(workspace.path())
        .args(["--cmd", "echo hello", "--port", "3000:3000", "--label", "si.codex.profile=ferma"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let command = parsed["command"].as_array().expect("command array");
    assert_eq!(command[0], "bash");
    assert_eq!(command[2], "echo hello");
    assert_eq!(parsed["user"], "root");
    assert_eq!(parsed["detach"], true);
    assert_eq!(parsed["auto_remove"], false);
    let labels = parsed["labels"].as_array().expect("labels array");
    assert!(labels.iter().any(|label| label["key"] == "si.component" && label["value"] == "codex"));
    assert!(
        labels.iter().any(|label| label["key"] == "si.codex.profile" && label["value"] == "ferma")
    );
    let published_ports = parsed["published_ports"].as_array().expect("published ports");
    assert_eq!(published_ports[0]["host_ip"], "127.0.0.1");
    assert_eq!(published_ports[0]["host_port"], "3000");
    assert_eq!(published_ports[0]["container_port"], 3000);
    let volume_mounts = parsed["volume_mounts"].as_array().expect("volume mounts");
    assert_eq!(volume_mounts.len(), 3);
    assert!(volume_mounts.iter().any(|mount| mount["target"] == "/home/si/.codex"));
    assert_eq!(parsed["restart_policy"], "unless-stopped");
}

#[test]
fn codex_spawn_run_args_text_renders_persistent_docker_invocation() {
    let workspace = tempdir().expect("tempdir");
    let output = cargo_bin()
        .args(["codex", "spawn-run-args", "--name", "ferma", "--workspace"])
        .arg(workspace.path())
        .args([
            "--cmd",
            "echo hello",
            "--port",
            "3000:3000",
            "--label",
            "si.codex.profile=ferma",
            "--format",
            "text",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert!(text.contains("run"));
    assert!(text.contains("-d"));
    assert!(text.contains("--user root"));
    assert!(text.contains("--label si.component=codex"));
    assert!(text.contains("--label si.codex.profile=ferma"));
    assert!(text.contains("-p 127.0.0.1:3000:3000"));
    assert!(text.contains("bash -lc echo hello"));
}

#[test]
fn codex_spawn_start_executes_docker_command_from_generated_spec() {
    let workspace = tempdir().expect("tempdir");
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > '{}'\nprintf '%s\\n' 'container-id-123'\n",
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["codex", "spawn-start", "--name", "ferma", "--workspace"])
        .arg(workspace.path())
        .args([
            "--cmd",
            "echo hello",
            "--port",
            "3000:3000",
            "--label",
            "si.codex.profile=ferma",
            "--docker-bin",
        ])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert!(text.contains("container-id-123"));
    let args = fs::read_to_string(args_path).expect("args file");
    assert!(args.contains("run"));
    assert!(args.contains("-d"));
    assert!(args.contains("--user"));
    assert!(args.contains("root"));
    assert!(args.contains("--label"));
    assert!(args.contains("si.component=codex"));
    assert!(args.contains("si.codex.profile=ferma"));
}

#[test]
fn codex_remove_plan_json_returns_container_and_volume_names() {
    let output = cargo_bin()
        .args(["codex", "remove-plan", "ferma", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["container_name"], "si-codex-ferma");
    assert_eq!(parsed["slug"], "ferma");
    assert_eq!(parsed["codex_volume"], "si-codex-ferma");
    assert_eq!(parsed["gh_volume"], "si-gh-ferma");
}

#[test]
fn codex_remove_executes_container_and_volume_removal() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" >> '{}'\nprintf '%s\\n' '--' >> '{}'\nprintf '%s\\n' 'removed'\n",
            args_path.display(),
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["codex", "remove", "ferma", "--volumes", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert!(text.contains("removed"));
    let args = fs::read_to_string(args_path).expect("args file");
    assert!(args.contains("rm"));
    assert!(args.contains("si-codex-ferma"));
    assert!(args.contains("volume"));
    assert!(args.contains("si-gh-ferma"));
}

#[test]
fn codex_remove_json_reports_profile_and_removed_artifacts() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" >> '{}'\nprintf '%s\\n' '--' >> '{}'\nif [ \"$1\" = \"inspect\" ]; then\n  printf '%s\\n' 'ferma'\nelse\n  printf '%s\\n' 'removed'\nfi\n",
            args_path.display(),
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["codex", "remove", "ferma", "--volumes", "--format", "json", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["container_name"], "si-codex-ferma");
    assert_eq!(parsed["profile_id"], "ferma");
    assert_eq!(parsed["codex_volume"], "si-codex-ferma");
    assert_eq!(parsed["gh_volume"], "si-gh-ferma");
    assert!(parsed["output"].as_str().expect("output string").contains("removed"));
}

#[test]
fn codex_start_executes_docker_start_for_container_name() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > '{}'\nprintf '%s\\n' 'si-codex-ferma'\n",
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["codex", "start", "ferma", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert!(text.contains("si-codex-ferma"));
    let args = fs::read_to_string(args_path).expect("args file");
    assert_eq!(args.lines().collect::<Vec<_>>(), ["start", "si-codex-ferma"]);
}

#[test]
fn codex_start_json_reports_container_name() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > '{}'\nprintf '%s\\n' 'started'\n",
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["codex", "start", "ferma", "--format", "json", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["action"], "start");
    assert_eq!(parsed["container_name"], "si-codex-ferma");
    assert!(parsed["output"].as_str().expect("output string").contains("started"));
}

#[test]
fn codex_stop_executes_docker_stop_for_container_name() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > '{}'\nprintf '%s\\n' 'si-codex-ferma'\n",
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["codex", "stop", "ferma", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert!(text.contains("si-codex-ferma"));
    let args = fs::read_to_string(args_path).expect("args file");
    assert_eq!(args.lines().collect::<Vec<_>>(), ["stop", "si-codex-ferma"]);
}

#[test]
fn codex_stop_json_reports_container_name() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > '{}'\nprintf '%s\\n' 'stopped'\n",
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["codex", "stop", "ferma", "--format", "json", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["action"], "stop");
    assert_eq!(parsed["container_name"], "si-codex-ferma");
    assert!(parsed["output"].as_str().expect("output string").contains("stopped"));
}

#[test]
fn codex_clone_json_reports_container_name() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > '{}'\nprintf '%s\\n' 'cloned'\n",
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["codex", "clone", "ferma", "acme/repo", "--format", "json", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["repo"], "acme/repo");
    assert_eq!(parsed["container_name"], "si-codex-ferma");
    assert!(parsed["output"].as_str().expect("output string").contains("cloned"));
}

#[test]
fn codex_logs_executes_docker_logs_for_container_name() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > '{}'\nprintf '%s\\n' 'log line'\n",
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["codex", "logs", "ferma", "--tail", "25", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert!(text.contains("log line"));
    let args = fs::read_to_string(args_path).expect("args file");
    assert_eq!(args.lines().collect::<Vec<_>>(), ["logs", "--tail", "25", "si-codex-ferma"]);
}

#[test]
fn codex_tail_executes_following_docker_logs_for_container_name() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > '{}'\nprintf '%s\\n' 'tail line'\n",
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["codex", "tail", "ferma", "--tail", "25", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert!(text.contains("tail line"));
    let args = fs::read_to_string(args_path).expect("args file");
    assert_eq!(args.lines().collect::<Vec<_>>(), ["logs", "-f", "--tail", "25", "si-codex-ferma"]);
}

#[test]
fn codex_clone_executes_docker_exec_for_container_name() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > '{}'\nprintf '%s\\n' 'cloned'\n",
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args(["codex", "clone", "ferma", "acme/repo", "--gh-pat", "token-123", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert!(text.contains("cloned"));
    let args = fs::read_to_string(args_path).expect("args file");
    assert_eq!(
        args.lines().collect::<Vec<_>>(),
        [
            "exec",
            "--user",
            "si",
            "-e",
            "SI_REPO=acme/repo",
            "-e",
            "SI_GH_PAT=token-123",
            "-e",
            "GH_TOKEN=token-123",
            "-e",
            "GITHUB_TOKEN=token-123",
            "si-codex-ferma",
            "/usr/local/bin/si-entrypoint",
            "bash",
            "-lc",
            "true",
        ]
    );
}

#[test]
fn codex_exec_executes_docker_exec_for_container_name() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > '{}'\nprintf '%s\\n' 'exec-output'\n",
            args_path.display()
        ),
    );

    let output = cargo_bin()
        .args([
            "codex",
            "exec",
            "ferma",
            "--interactive=false",
            "--tty=false",
            "--workdir",
            "/workspace/project",
            "--env",
            "A=1",
            "--env",
            "B=2",
            "--docker-bin",
        ])
        .arg(&docker_bin)
        .arg("--")
        .args(["git", "status"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let text = String::from_utf8(output).expect("utf8 output");
    assert!(text.contains("exec-output"));
    let args = fs::read_to_string(args_path).expect("args file");
    assert_eq!(
        args.lines().collect::<Vec<_>>(),
        [
            "exec",
            "--user",
            "si",
            "-w",
            "/workspace/project",
            "-e",
            "A=1",
            "-e",
            "B=2",
            "si-codex-ferma",
            "git",
            "status",
        ]
    );
}

#[test]
fn codex_status_read_returns_parsed_app_server_usage() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let input_path = script_dir.path().join("input.txt");
    let docker_bin = script_dir.path().join("docker");
    let primary_reset = Local::now().timestamp() + 3600;
    let secondary_reset = Local::now().timestamp() + 7200;
    let rate_json = serde_json::json!({
        "id": 2,
        "result": {
            "rateLimits": {
                "primary": {
                    "usedPercent": 25,
                    "windowDurationMins": 300,
                    "resetsAt": primary_reset,
                },
                "secondary": {
                    "usedPercent": 12,
                    "windowDurationMins": 10080,
                    "resetsAt": secondary_reset,
                }
            }
        }
    })
    .to_string();
    let account_json = serde_json::json!({
        "id": 3,
        "result": {
            "account": {
                "type": "chatgpt",
                "email": "ferma@example.com",
                "planType": "pro"
            }
        }
    })
    .to_string();
    let config_json = serde_json::json!({
        "id": 4,
        "result": {
            "config": {
                "model": "gpt-5.2-codex",
                "model_reasoning_effort": "medium"
            }
        }
    })
    .to_string();
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\ncat > '{}'\nprintf '%s\\n' \"$@\" > '{}'\nprintf '%s\\n' '{}' '{}' '{}'\n",
            input_path.display(),
            args_path.display(),
            rate_json,
            account_json,
            config_json,
        ),
    );

    let output = cargo_bin()
        .args(["codex", "status-read", "ferma", "--raw", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["source"], "app-server");
    assert_eq!(parsed["account_email"], "ferma@example.com");
    assert_eq!(parsed["account_plan"], "pro");
    assert_eq!(parsed["model"], "gpt-5.2-codex");
    assert_eq!(parsed["reasoning_effort"], "medium");
    assert_eq!(parsed["five_hour_left_pct"], 75.0);
    assert_eq!(parsed["weekly_left_pct"], 88.0);
    assert!(parsed["raw"].as_str().unwrap_or("").contains("\"id\":2"));

    let args = fs::read_to_string(args_path).expect("args file");
    assert_eq!(
        args.lines().collect::<Vec<_>>(),
        [
            "exec",
            "-i",
            "--user",
            "si",
            "-e",
            "HOME=/home/si",
            "-e",
            "CODEX_HOME=/home/si/.codex",
            "-e",
            "TERM=xterm-256color",
            "si-codex-ferma",
            "codex",
            "app-server",
        ]
    );
    let input = fs::read_to_string(input_path).expect("input file");
    assert!(input.contains("\"method\":\"account/rateLimits/read\""));
}

#[test]
fn codex_lifecycle_smoke_works_with_fake_docker() {
    let workspace = tempdir().expect("tempdir");
    let script_dir = tempdir().expect("tempdir");
    let state_path = script_dir.path().join("state.txt");
    let docker_bin = script_dir.path().join("docker");
    let primary_reset = Local::now().timestamp() + 3600;
    let secondary_reset = Local::now().timestamp() + 7200;
    let rate_json = serde_json::json!({
        "id": 2,
        "result": {
            "rateLimits": {
                "primary": {
                    "usedPercent": 25,
                    "windowDurationMins": 300,
                    "resetsAt": primary_reset,
                },
                "secondary": {
                    "usedPercent": 12,
                    "windowDurationMins": 10080,
                    "resetsAt": secondary_reset,
                }
            }
        }
    })
    .to_string();
    let account_json = serde_json::json!({
        "id": 3,
        "result": {
            "account": {
                "type": "chatgpt",
                "email": "ferma@example.com",
                "planType": "pro"
            }
        }
    })
    .to_string();
    let config_json = serde_json::json!({
        "id": 4,
        "result": {
            "config": {
                "model": "gpt-5.2-codex",
                "model_reasoning_effort": "medium"
            }
        }
    })
    .to_string();
    write_executable_script(
        &docker_bin,
        &format!(
            "#!/bin/sh\nSTATE='{}'\ncmd=\"$1\"\nshift\ncase \"$cmd\" in\n  run)\n    printf '%s\\n' 'running' > \"$STATE\"\n    printf '%s\\n' 'container-id-123'\n    ;;\n  start)\n    printf '%s\\n' 'running' > \"$STATE\"\n    printf '%s\\n' 'si-codex-ferma'\n    ;;\n  stop)\n    printf '%s\\n' 'stopped' > \"$STATE\"\n    printf '%s\\n' 'si-codex-ferma'\n    ;;\n  logs)\n    printf '%s\\n' 'log line'\n    ;;\n  inspect)\n    printf '%s\\n' 'ferma'\n    ;;\n  rm)\n    printf '%s\\n' 'removed' > \"$STATE\"\n    printf '%s\\n' 'si-codex-ferma'\n    ;;\n  volume)\n    shift\n    printf '%s\\n' 'removed-volume'\n    ;;\n  exec)\n    cat >/dev/null\n    if [ \"$1\" = \"--user\" ]; then shift 2; fi\n    while [ $# -gt 0 ]; do\n      case \"$1\" in\n        -e|-w) shift 2 ;;\n        -i|-t) shift ;;\n        si-codex-*) shift; break ;;\n        *) shift ;;\n      esac\n    done\n    if [ \"$1\" = \"/usr/local/bin/si-entrypoint\" ]; then\n      printf '%s\\n' 'cloned'\n    else\n      printf '%s\\n' '{}' '{}' '{}'\n    fi\n    ;;\n  *)\n    printf 'unexpected docker command: %s\\n' \"$cmd\" >&2\n    exit 1\n    ;;\nesac\n",
            state_path.display(),
            rate_json,
            account_json,
            config_json
        ),
    );

    cargo_bin()
        .args(["codex", "spawn-start", "--name", "ferma", "--workspace"])
        .arg(workspace.path())
        .args(["--cmd", "echo hello", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success();
    assert_eq!(fs::read_to_string(&state_path).expect("state"), "running\n");

    let status_output = cargo_bin()
        .args(["codex", "status-read", "ferma", "--format", "json", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    let status: Value = serde_json::from_slice(&status_output).expect("json output");
    assert_eq!(status["account_email"], "ferma@example.com");
    assert_eq!(status["model"], "gpt-5.2-codex");

    let logs_output = cargo_bin()
        .args(["codex", "logs", "ferma", "--tail", "10", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert!(String::from_utf8(logs_output).expect("utf8 output").contains("log line"));

    cargo_bin()
        .args(["codex", "stop", "ferma", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success();
    assert_eq!(fs::read_to_string(&state_path).expect("state"), "stopped\n");

    cargo_bin()
        .args(["codex", "start", "ferma", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success();
    assert_eq!(fs::read_to_string(&state_path).expect("state"), "running\n");

    let clone_output = cargo_bin()
        .args(["codex", "clone", "ferma", "acme/repo", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();
    assert!(String::from_utf8(clone_output).expect("utf8 output").contains("cloned"));

    cargo_bin()
        .args(["codex", "remove", "ferma", "--volumes", "--docker-bin"])
        .arg(&docker_bin)
        .assert()
        .success();
    assert_eq!(fs::read_to_string(&state_path).expect("state"), "removed\n");
}

#[test]
fn codex_respawn_plan_returns_sorted_unique_remove_targets() {
    let output = cargo_bin()
        .args([
            "codex",
            "respawn-plan",
            "ferma",
            "--profile-id",
            "ferma",
            "--profile-container",
            "si-codex-alpha",
            "--profile-container",
            "ferma",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["effective_name"], "ferma");
    assert_eq!(parsed["profile_id"], "ferma");
    assert_eq!(parsed["remove_targets"], serde_json::json!(["alpha", "ferma"]));
}

#[test]
fn codex_tmux_plan_json_uses_bypass_flag_and_start_dir() {
    let output = cargo_bin()
        .args([
            "codex",
            "tmux-plan",
            "profile-beta",
            "--start-dir",
            "/home/ubuntu/Development/si",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["session_name"], "si-codex-pane-profile-beta");
    assert_eq!(parsed["target"], "si-codex-pane-profile-beta:0.0");
    assert!(
        parsed["launch_command"]
            .as_str()
            .unwrap_or("")
            .contains("codex --dangerously-bypass-approvals-and-sandbox")
    );
    assert!(parsed["launch_command"].as_str().unwrap_or("").contains("--user 'si'"));
    assert!(
        parsed["launch_command"].as_str().unwrap_or("").contains("/home/ubuntu/Development/si")
    );
}

#[test]
fn codex_tmux_plan_json_includes_resume_command_when_present() {
    let output = cargo_bin()
        .args([
            "codex",
            "tmux-plan",
            "profile-beta",
            "--start-dir",
            "/workspace/app",
            "--resume-session-id",
            "sess-123",
            "--resume-profile",
            "profile-beta",
            "--format",
            "json",
        ])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert!(parsed["resume_command"].as_str().unwrap_or("").contains("codex resume"));
    assert!(parsed["resume_command"].as_str().unwrap_or("").contains("sess-123"));
    assert!(
        parsed["resume_command"]
            .as_str()
            .unwrap_or("")
            .contains("tmux session unavailable; attempting codex resume")
    );
}

#[test]
fn codex_tmux_command_json_uses_bypass_flag() {
    let output = cargo_bin()
        .args(["codex", "tmux-command", "--container", "abc123", "--format", "json"])
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    assert_eq!(parsed["container"], "abc123");
    assert!(
        parsed["launch_command"]
            .as_str()
            .unwrap_or("")
            .contains("codex --dangerously-bypass-approvals-and-sandbox")
    );
    assert!(parsed["launch_command"].as_str().unwrap_or("").contains("--user 'si'"));
}

#[test]
fn codex_report_parse_json_extracts_report_for_prompt_index() {
    let output = cargo_bin()
        .args(["codex", "report-parse", "--format", "json"])
        .write_stdin(
            r#"{
  "clean": "› first\n• Working…\n\n› second\n• Finished task\n  with detail\nWorked for 5s\n",
  "raw": "› first\n• Working…\n\n› second\n• Finished task\n  with detail\nWorked for 5s\n",
  "prompt_index": 1,
  "ansi": false
}"#,
        )
        .assert()
        .success()
        .get_output()
        .stdout
        .clone();

    let parsed: Value = serde_json::from_slice(&output).expect("json output");
    let segments = parsed["segments"].as_array().expect("segments array");
    assert_eq!(segments.len(), 2);
    assert_eq!(segments[0]["prompt"], "first");
    assert_eq!(segments[1]["prompt"], "second");
    assert_eq!(parsed["report"], "• Finished task\n  with detail\nWorked for 5s");
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

fn start_one_shot_http_server<F>(handler: F) -> TestHttpServer
where
    F: FnOnce(String) -> String + Send + 'static,
{
    let listener = TcpListener::bind("127.0.0.1:0").expect("bind test server");
    let addr = listener.local_addr().expect("local addr");
    let handle = thread::spawn(move || {
        let (mut stream, _) = listener.accept().expect("accept");
        let mut request = Vec::new();
        let mut buffer = [0_u8; 4096];
        loop {
            let read = stream.read(&mut buffer).expect("read request");
            if read == 0 {
                break;
            }
            request.extend_from_slice(&buffer[..read]);
            if request.windows(4).any(|window| window == b"\r\n\r\n") {
                break;
            }
        }
        let request = String::from_utf8(request).expect("request utf8");
        let response = handler(request);
        stream.write_all(response.as_bytes()).expect("write response");
        stream.flush().expect("flush response");
    });
    TestHttpServer { base_url: format!("http://{addr}"), handle: Some(handle) }
}

fn start_http_server<F>(requests: usize, handler: F) -> TestHttpServer
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
            loop {
                let read = stream.read(&mut buffer).expect("read request");
                if read == 0 {
                    break;
                }
                request.extend_from_slice(&buffer[..read]);
                if request.windows(4).any(|window| window == b"\r\n\r\n") {
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
        "HTTP/1.1 {status}\r\nContent-Type: application/json\r\nContent-Length: {}\r\n",
        body.len()
    );
    for (key, value) in headers {
        response.push_str(&format!("{key}: {value}\r\n"));
    }
    response.push_str("\r\n");
    response.push_str(body);
    response
}

fn write_executable_script(path: &Path, content: &str) {
    let temp_path = path.with_extension("tmp");
    let mut file = File::create(&temp_path).expect("create temp script");
    file.write_all(content.as_bytes()).expect("write temp script");
    file.sync_all().expect("sync temp script");
    drop(file);
    fs::rename(&temp_path, path).expect("rename temp script");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(path).expect("metadata").permissions();
        perms.set_mode(0o700);
        fs::set_permissions(path, perms).expect("chmod");
    }
}
