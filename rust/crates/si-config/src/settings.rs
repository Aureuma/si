use serde::{Deserialize, Serialize};
use std::fs;
use std::path::{Path, PathBuf};
use thiserror::Error;

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct Settings {
    #[serde(default = "default_schema_version")]
    pub schema_version: u32,
    #[serde(default)]
    pub paths: SettingsPaths,
    #[serde(default)]
    pub codex: CodexSettings,
    #[serde(default)]
    pub dyad: DyadSettings,
}

impl Settings {
    pub fn from_toml(source: &str) -> Result<Self, toml::de::Error> {
        toml::from_str(source)
    }

    pub fn from_toml_with_home_defaults(
        source: &str,
        home: &Path,
    ) -> Result<Self, toml::de::Error> {
        let mut settings = Self::from_toml(source)?;
        settings.apply_home_defaults(home);
        Ok(settings)
    }

    pub fn load(home: &Path, settings_file: Option<&Path>) -> Result<Self, LoadSettingsError> {
        let defaults = Self::with_home_defaults(home);
        let default_settings_path =
            defaults.paths.settings_file.as_deref().unwrap_or_default().to_owned();
        let path = settings_file.unwrap_or_else(|| Path::new(&default_settings_path));
        if !path.exists() {
            return Ok(defaults);
        }

        let source = fs::read_to_string(path).map_err(|source| {
            LoadSettingsError::ReadSettings { path: path.to_path_buf(), source }
        })?;

        Self::from_toml_with_home_defaults(&source, home)
            .map_err(|source| LoadSettingsError::ParseSettings { path: path.to_path_buf(), source })
    }

    pub fn with_home_defaults(home: &Path) -> Self {
        let root = home.join(".si");
        let settings_file = root.join("settings.toml");
        let codex_profiles_dir = root.join("codex").join("profiles");

        Self {
            schema_version: default_schema_version(),
            paths: SettingsPaths {
                root: Some(path_string(&root)),
                settings_file: Some(path_string(&settings_file)),
                codex_profiles_dir: Some(path_string(&codex_profiles_dir)),
                workspace_root: None,
            },
            codex: CodexSettings::default(),
            dyad: DyadSettings::default(),
        }
    }

    fn apply_home_defaults(&mut self, home: &Path) {
        let defaults = Self::with_home_defaults(home);
        if self.schema_version == 0 {
            self.schema_version = defaults.schema_version;
        }
        if self.paths.root.is_none() {
            self.paths.root = defaults.paths.root;
        }
        if self.paths.settings_file.is_none() {
            self.paths.settings_file = defaults.paths.settings_file;
        }
        if self.paths.codex_profiles_dir.is_none() {
            self.paths.codex_profiles_dir = defaults.paths.codex_profiles_dir;
        }
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct SettingsPaths {
    pub root: Option<String>,
    pub settings_file: Option<String>,
    pub codex_profiles_dir: Option<String>,
    pub workspace_root: Option<String>,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct CodexSettings {
    pub image: Option<String>,
    pub network: Option<String>,
    pub workspace: Option<String>,
    pub workdir: Option<String>,
    pub profile: Option<String>,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct DyadSettings {
    pub actor_image: Option<String>,
    pub critic_image: Option<String>,
    pub workspace: Option<String>,
    pub configs: Option<String>,
}

#[derive(Debug, Error)]
pub enum LoadSettingsError {
    #[error("read settings {path}: {source}")]
    ReadSettings {
        path: PathBuf,
        #[source]
        source: std::io::Error,
    },
    #[error("parse settings {path}: {source}")]
    ParseSettings {
        path: PathBuf,
        #[source]
        source: toml::de::Error,
    },
}

fn default_schema_version() -> u32 {
    1
}

fn path_string(path: &Path) -> String {
    path.display().to_string()
}

#[cfg(test)]
mod tests {
    use super::Settings;
    use std::path::Path;
    use tempfile::tempdir;

    #[test]
    fn parses_paths_subset() {
        let parsed = Settings::from_toml(
            r#"
schema_version = 1

[paths]
root = "~/.si"
settings_file = "~/.si/settings.toml"
codex_profiles_dir = "~/.si/codex/profiles"
"#,
        )
        .expect("settings should parse");

        assert_eq!(parsed.schema_version, 1);
        assert_eq!(parsed.paths.root.as_deref(), Some("~/.si"));
        assert_eq!(parsed.paths.settings_file.as_deref(), Some("~/.si/settings.toml"));
        assert_eq!(parsed.paths.codex_profiles_dir.as_deref(), Some("~/.si/codex/profiles"));
    }

    #[test]
    fn applies_home_defaults_for_core_state_paths() {
        let settings = Settings::from_toml_with_home_defaults(
            "[codex]\nprofile = \"ferma\"\n",
            Path::new("/Users/dev"),
        )
        .expect("settings should parse");

        assert_eq!(settings.schema_version, 1);
        assert_eq!(settings.paths.root.as_deref(), Some("/Users/dev/.si"));
        assert_eq!(settings.paths.settings_file.as_deref(), Some("/Users/dev/.si/settings.toml"));
        assert_eq!(
            settings.paths.codex_profiles_dir.as_deref(),
            Some("/Users/dev/.si/codex/profiles")
        );
        assert_eq!(settings.codex.profile.as_deref(), Some("ferma"));
    }

    #[test]
    fn loads_runtime_fields_from_settings_file() {
        let home = tempdir().expect("tempdir");
        let settings_dir = home.path().join(".si");
        std::fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
        let settings_path = settings_dir.join("settings.toml");
        std::fs::write(
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

        let settings =
            Settings::load(home.path(), Some(&settings_path)).expect("settings should load");

        assert_eq!(settings.paths.workspace_root.as_deref(), Some("~/Development"));
        assert_eq!(settings.codex.workspace.as_deref(), Some("~/Development/si"));
        assert_eq!(settings.codex.workdir.as_deref(), Some("/workspace"));
        assert_eq!(settings.codex.profile.as_deref(), Some("darmstada"));
        assert_eq!(settings.dyad.workspace.as_deref(), Some("~/Development"));
        assert_eq!(settings.dyad.configs.as_deref(), Some("~/Development/si/configs"));
    }
}
