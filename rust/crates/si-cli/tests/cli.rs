use assert_cmd::Command;
use chrono::Local;
use serde_json::Value;
use std::fs;
use std::fs::File;
use std::io::Write;
use std::path::Path;
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
