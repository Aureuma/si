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

fn path_string(path: impl AsRef<Path>) -> Value {
    Value::String(path.as_ref().display().to_string())
}
