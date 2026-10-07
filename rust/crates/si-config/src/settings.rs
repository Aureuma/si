use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
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
    pub muse: MuseSettings,
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
        settings.apply_runtime_defaults();
        Ok(settings)
    }

    pub fn load(home: &Path, settings_file: Option<&Path>) -> Result<Self, LoadSettingsError> {
        let mut settings = Self::with_home_defaults(home);
        let core_path =
            settings_file.map(Path::to_path_buf).unwrap_or_else(|| SettingsModule::Core.path(home));
        settings.merge_module_if_exists(SettingsModule::Core, &core_path)?;
        settings.apply_home_defaults(home);
        settings.apply_runtime_defaults();
        Ok(settings)
    }

    pub fn with_home_defaults(home: &Path) -> Self {
        let root = crate::paths::resolve_si_root(home);
        let settings_file = root.join("settings.toml");
        let codex_profiles_dir = root.join("codex").join("profiles");
        let muse_profiles_dir = root.join("muse").join("profiles");

        Self {
            schema_version: default_schema_version(),
            paths: SettingsPaths {
                root: Some(path_string(&root)),
                settings_file: Some(path_string(&settings_file)),
                codex_profiles_dir: Some(path_string(&codex_profiles_dir)),
                muse_profiles_dir: Some(path_string(&muse_profiles_dir)),
                workspace_root: None,
            },
            codex: CodexSettings::default(),
            muse: MuseSettings::default(),
        }
    }

    fn merge_module_if_exists(
        &mut self,
        module: SettingsModule,
        path: &Path,
    ) -> Result<(), LoadSettingsError> {
        if !path.exists() {
            return Ok(());
        }

        let source =
            fs::read_to_string(path).map_err(|source| LoadSettingsError::ReadSettings {
                module: module.as_str(),
                path: path.to_path_buf(),
                source,
            })?;

        self.merge_module_from_toml(module, &source).map_err(|source| {
            LoadSettingsError::ParseSettings {
                module: module.as_str(),
                path: path.to_path_buf(),
                source: Box::new(source),
            }
        })
    }

    fn merge_module_from_toml(
        &mut self,
        module: SettingsModule,
        source: &str,
    ) -> Result<(), toml::de::Error> {
        match module {
            SettingsModule::Core => {
                let payload: CoreSettingsModule = toml::from_str(source)?;
                if let Some(schema_version) = payload.schema_version {
                    self.schema_version = schema_version;
                }
                self.paths = payload.paths;
                self.codex = payload.codex;
                self.muse = payload.muse;
            }
        }

        Ok(())
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
        if self.paths.muse_profiles_dir.is_none() {
            self.paths.muse_profiles_dir = defaults.paths.muse_profiles_dir;
        }
    }

    fn apply_runtime_defaults(&mut self) {
        normalize_option_string(&mut self.paths.workspace_root);
        self.codex.normalize();
        self.muse.normalize();
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
struct CoreSettingsModule {
    pub schema_version: Option<u32>,
    #[serde(default)]
    pub paths: SettingsPaths,
    #[serde(default)]
    pub codex: CodexSettings,
    #[serde(default)]
    pub muse: MuseSettings,
}

#[derive(Clone, Copy, Debug)]
enum SettingsModule {
    Core,
}

impl SettingsModule {
    fn as_str(self) -> &'static str {
        match self {
            Self::Core => "core",
        }
    }

    fn path(self, home: &Path) -> PathBuf {
        let root = crate::paths::resolve_si_root(home);
        match self {
            Self::Core => root.join("settings.toml"),
        }
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct SettingsPaths {
    pub root: Option<String>,
    pub settings_file: Option<String>,
    pub codex_profiles_dir: Option<String>,
    pub muse_profiles_dir: Option<String>,
    pub workspace_root: Option<String>,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct CodexSettings {
    pub workspace: Option<String>,
    pub workdir: Option<String>,
    pub profile: Option<String>,
    #[serde(default)]
    pub profiles: CodexProfilesSettings,
}

impl CodexSettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.workspace);
        normalize_option_string(&mut self.workdir);
        normalize_option_string(&mut self.profile);
        self.profiles.normalize();
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct CodexProfilesSettings {
    pub active: Option<String>,
    #[serde(default)]
    pub entries: BTreeMap<String, CodexProfileEntry>,
}

impl CodexProfilesSettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.active);
        let mut normalized = BTreeMap::new();
        for (key, mut entry) in std::mem::take(&mut self.entries) {
            let key = key.trim().to_owned();
            if key.is_empty() {
                continue;
            }
            entry.normalize();
            normalized.insert(key, entry);
        }
        self.entries = normalized;
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct CodexProfileEntry {
    pub name: Option<String>,
    pub email: Option<String>,
    pub auth_path: Option<String>,
    pub auth_updated: Option<String>,
    /// Canonical native home for this account (post-migration shape).
    pub native_root: Option<String>,
    /// Short identity summary bound at enrollment (display hint, not a secret).
    pub identity: Option<String>,
    /// How `identity` was verified, or `identity_unverified`.
    pub identity_source: Option<String>,
    /// RFC3339 enrollment timestamp.
    pub enrolled_at: Option<String>,
    /// Set with a reason when launch is disabled for this profile.
    pub disabled: Option<String>,
}

impl CodexProfileEntry {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.name);
        normalize_option_string(&mut self.email);
        normalize_option_string(&mut self.auth_path);
        normalize_option_string(&mut self.auth_updated);
        normalize_option_string(&mut self.native_root);
        normalize_option_string(&mut self.identity);
        normalize_option_string(&mut self.identity_source);
        normalize_option_string(&mut self.enrolled_at);
        normalize_option_string(&mut self.disabled);
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct MuseSettings {
    pub workspace: Option<String>,
    pub workdir: Option<String>,
    pub profile: Option<String>,
    #[serde(default)]
    pub profiles: MuseProfilesSettings,
}

impl MuseSettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.workspace);
        normalize_option_string(&mut self.workdir);
        normalize_option_string(&mut self.profile);
        self.profiles.normalize();
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct MuseProfilesSettings {
    pub active: Option<String>,
    #[serde(default)]
    pub entries: BTreeMap<String, MuseProfileEntry>,
}

impl MuseProfilesSettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.active);
        let mut normalized = BTreeMap::new();
        for (key, mut entry) in std::mem::take(&mut self.entries) {
            let key = key.trim().to_owned();
            if key.is_empty() {
                continue;
            }
            entry.normalize();
            normalized.insert(key, entry);
        }
        self.entries = normalized;
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct MuseProfileEntry {
    pub name: Option<String>,
    pub email: Option<String>,
    /// Canonical native root for this account (holds config/data/cache/state).
    pub native_root: Option<String>,
    /// Short identity summary bound at enrollment (display hint, not a secret).
    pub identity: Option<String>,
    /// How `identity` was verified, or `identity_unverified`.
    pub identity_source: Option<String>,
    /// RFC3339 enrollment timestamp.
    pub enrolled_at: Option<String>,
    /// Set with a reason when launch is disabled for this profile.
    pub disabled: Option<String>,
}

impl MuseProfileEntry {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.name);
        normalize_option_string(&mut self.email);
        normalize_option_string(&mut self.native_root);
        normalize_option_string(&mut self.identity);
        normalize_option_string(&mut self.identity_source);
        normalize_option_string(&mut self.enrolled_at);
        normalize_option_string(&mut self.disabled);
    }
}

#[derive(Debug, Error)]
pub enum LoadSettingsError {
    #[error("read {module} settings {path}: {source}")]
    ReadSettings {
        module: &'static str,
        path: PathBuf,
        #[source]
        source: std::io::Error,
    },
    #[error("parse {module} settings {path}: {source}")]
    ParseSettings {
        module: &'static str,
        path: PathBuf,
        #[source]
        source: Box<toml::de::Error>,
    },
}

fn default_schema_version() -> u32 {
    1
}

fn path_string(path: &Path) -> String {
    path.display().to_string()
}

fn normalize_option_string(value: &mut Option<String>) {
    if let Some(current) = value {
        let trimmed = current.trim();
        if trimmed.is_empty() {
            *value = None;
        } else if trimmed.len() != current.len() {
            *current = trimmed.to_owned();
        }
    }
}

#[cfg(test)]
mod tests {
    use super::Settings;
    use std::fs;
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
            "[codex]\nprofile = \"profile-zeta\"\n",
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
        assert_eq!(settings.codex.profile.as_deref(), Some("profile-zeta"));
    }

    #[test]
    fn loads_runtime_fields_from_settings_file() {
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
profile = "profile-delta"

"#,
        )
        .expect("write settings");

        let settings =
            Settings::load(home.path(), Some(&settings_path)).expect("settings should load");

        assert_eq!(settings.paths.workspace_root.as_deref(), Some("~/Development"));
        assert_eq!(settings.codex.workspace.as_deref(), Some("~/Development/si"));
        assert_eq!(settings.codex.workdir.as_deref(), Some("/workspace"));
        assert_eq!(settings.codex.profile.as_deref(), Some("profile-delta"));
    }

    #[test]
    fn loads_codex_profiles_from_settings_file() {
        let home = tempdir().expect("tempdir");
        let settings_dir = home.path().join(".si");
        fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
        let settings_path = settings_dir.join("settings.toml");
        fs::write(
            &settings_path,
            r#"
[codex]
profile = "legacy"

[codex.profiles]
active = "profile-delta"

[codex.profiles.entries.profile-delta]
name = "Profile Delta"
email = "profile-delta@example.com"
auth_path = "~/.si/codex/profiles/profile-delta/auth.json"
auth_updated = "2026-03-23T00:00:00Z"
"#,
        )
        .expect("write settings");

        let settings =
            Settings::load(home.path(), Some(&settings_path)).expect("settings should load");

        assert_eq!(settings.codex.profile.as_deref(), Some("legacy"));
        assert_eq!(settings.codex.profiles.active.as_deref(), Some("profile-delta"));
        let entry = settings.codex.profiles.entries.get("profile-delta").expect("profile entry");
        assert_eq!(entry.name.as_deref(), Some("Profile Delta"));
        assert_eq!(entry.email.as_deref(), Some("profile-delta@example.com"));
        assert_eq!(
            entry.auth_path.as_deref(),
            Some("~/.si/codex/profiles/profile-delta/auth.json")
        );
        assert_eq!(entry.auth_updated.as_deref(), Some("2026-03-23T00:00:00Z"));
    }
}
