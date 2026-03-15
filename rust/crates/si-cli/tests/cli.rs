use assert_cmd::Command;
use serde_json::Value;
use std::fs;
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
    fs::write(
        &docker_bin,
        format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > '{}'\nprintf '%s\\n' 'container-id-123'\n",
            args_path.display()
        ),
    )
    .expect("write docker script");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&docker_bin).expect("metadata").permissions();
        perms.set_mode(0o700);
        fs::set_permissions(&docker_bin, perms).expect("chmod");
    }

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
fn codex_start_executes_docker_start_for_container_name() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    fs::write(
        &docker_bin,
        format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > '{}'\nprintf '%s\\n' 'si-codex-ferma'\n",
            args_path.display()
        ),
    )
    .expect("write docker script");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&docker_bin).expect("metadata").permissions();
        perms.set_mode(0o700);
        fs::set_permissions(&docker_bin, perms).expect("chmod");
    }

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
fn codex_stop_executes_docker_stop_for_container_name() {
    let script_dir = tempdir().expect("tempdir");
    let args_path = script_dir.path().join("args.txt");
    let docker_bin = script_dir.path().join("docker");
    fs::write(
        &docker_bin,
        format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > '{}'\nprintf '%s\\n' 'si-codex-ferma'\n",
            args_path.display()
        ),
    )
    .expect("write docker script");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&docker_bin).expect("metadata").permissions();
        perms.set_mode(0o700);
        fs::set_permissions(&docker_bin, perms).expect("chmod");
    }

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
    fs::write(
        &docker_bin,
        format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > '{}'\nprintf '%s\\n' 'log line'\n",
            args_path.display()
        ),
    )
    .expect("write docker script");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&docker_bin).expect("metadata").permissions();
        perms.set_mode(0o700);
        fs::set_permissions(&docker_bin, perms).expect("chmod");
    }

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
    fs::write(
        &docker_bin,
        format!(
            "#!/bin/sh\nprintf '%s\\n' \"$@\" > '{}'\nprintf '%s\\n' 'tail line'\n",
            args_path.display()
        ),
    )
    .expect("write docker script");
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = fs::metadata(&docker_bin).expect("metadata").permissions();
        perms.set_mode(0o700);
        fs::set_permissions(&docker_bin, perms).expect("chmod");
    }

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

fn path_string(path: impl AsRef<Path>) -> Value {
    Value::String(path.as_ref().display().to_string())
}
