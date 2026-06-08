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
    pub fort: FortSettings,
    #[serde(default)]
    pub surf: SurfSettings,
    #[serde(default)]
    pub viva: VivaSettings,
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
        settings.merge_module_if_exists(SettingsModule::Surf, &SettingsModule::Surf.path(home))?;
        settings.merge_module_if_exists(SettingsModule::Viva, &SettingsModule::Viva.path(home))?;
        settings.apply_home_defaults(home);
        settings.apply_runtime_defaults();
        Ok(settings)
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
            fort: FortSettings::default(),
            surf: SurfSettings::default(),
            viva: VivaSettings::default(),
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
                self.fort = payload.fort;
                self.surf = payload.surf;
            }
            SettingsModule::Surf => {
                let payload: SurfSettingsModule = toml::from_str(source)?;
                self.surf.merge(payload.surf);
            }
            SettingsModule::Viva => {
                let payload: VivaSettingsModule = toml::from_str(source)?;
                self.viva = payload.viva;
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
    }

    fn apply_runtime_defaults(&mut self) {
        normalize_option_string(&mut self.paths.workspace_root);
        self.codex.normalize();
        self.fort.normalize();
        self.surf.normalize();
        self.viva.normalize();
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
    pub fort: FortSettings,
    #[serde(default)]
    pub surf: SurfSettings,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
struct SurfSettingsModule {
    #[serde(default)]
    pub surf: SurfSettings,
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
struct VivaSettingsModule {
    #[serde(default)]
    pub viva: VivaSettings,
}

#[derive(Clone, Copy, Debug)]
enum SettingsModule {
    Core,
    Surf,
    Viva,
}

impl SettingsModule {
    fn as_str(self) -> &'static str {
        match self {
            Self::Core => "core",
            Self::Surf => "surf",
            Self::Viva => "viva",
        }
    }

    fn path(self, home: &Path) -> PathBuf {
        let root = home.join(".si");
        match self {
            Self::Core => root.join("settings.toml"),
            Self::Surf => root.join("surf").join("si.settings.toml"),
            Self::Viva => root.join("viva").join("settings.toml"),
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
}

impl CodexProfileEntry {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.name);
        normalize_option_string(&mut self.email);
        normalize_option_string(&mut self.auth_path);
        normalize_option_string(&mut self.auth_updated);
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct FortSettings {
    pub repo: Option<String>,
    pub bin: Option<String>,
    pub build: Option<bool>,
    pub host: Option<String>,
    pub runtime_host: Option<String>,
}

impl FortSettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.repo);
        normalize_option_string(&mut self.bin);
        normalize_option_string(&mut self.host);
        normalize_option_string(&mut self.runtime_host);
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct SurfSettings {
    #[serde(default)]
    pub repo: String,
    #[serde(default)]
    pub bin: String,
    pub build: Option<bool>,
    #[serde(default)]
    pub settings_file: String,
    #[serde(default)]
    pub state_dir: String,
    #[serde(default)]
    pub vnc_password_fort_key: String,
    #[serde(default)]
    pub vnc_password_fort_repo: String,
    #[serde(default)]
    pub vnc_password_fort_env: String,
    #[serde(default)]
    pub tunnel: SurfTunnelSettings,
}

impl SurfSettings {
    fn merge(&mut self, other: Self) {
        if !other.repo.is_empty() {
            self.repo = other.repo;
        }
        if !other.bin.is_empty() {
            self.bin = other.bin;
        }
        if other.build.is_some() {
            self.build = other.build;
        }
        if !other.settings_file.is_empty() {
            self.settings_file = other.settings_file;
        }
        if !other.state_dir.is_empty() {
            self.state_dir = other.state_dir;
        }
        if !other.vnc_password_fort_key.is_empty() {
            self.vnc_password_fort_key = other.vnc_password_fort_key;
        }
        if !other.vnc_password_fort_repo.is_empty() {
            self.vnc_password_fort_repo = other.vnc_password_fort_repo;
        }
        if !other.vnc_password_fort_env.is_empty() {
            self.vnc_password_fort_env = other.vnc_password_fort_env;
        }
        self.tunnel.merge(other.tunnel);
    }

    fn normalize(&mut self) {
        trim_string(&mut self.repo);
        trim_string(&mut self.bin);
        trim_string(&mut self.settings_file);
        trim_string(&mut self.state_dir);
        trim_string(&mut self.vnc_password_fort_key);
        trim_string(&mut self.vnc_password_fort_repo);
        if !self.vnc_password_fort_key.is_empty() && self.vnc_password_fort_repo.is_empty() {
            self.vnc_password_fort_repo = "surf".to_owned();
        }
        self.vnc_password_fort_env = self.vnc_password_fort_env.trim().to_lowercase();
        if !self.vnc_password_fort_key.is_empty() && self.vnc_password_fort_env.is_empty() {
            self.vnc_password_fort_env = "dev".to_owned();
        }
        trim_string(&mut self.tunnel.name);
        trim_string(&mut self.tunnel.fort_key);
        trim_string(&mut self.tunnel.vault_key);
        if self.tunnel.fort_key.is_empty() && !self.tunnel.vault_key.is_empty() {
            self.tunnel.fort_key = self.tunnel.vault_key.clone();
        }
        self.tunnel.mode = normalize_choice(&self.tunnel.mode, &["quick", "token"]);
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct SurfTunnelSettings {
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub mode: String,
    #[serde(default)]
    pub fort_key: String,
    #[serde(default)]
    pub vault_key: String,
}

impl SurfTunnelSettings {
    fn merge(&mut self, other: Self) {
        if !other.name.is_empty() {
            self.name = other.name;
        }
        if !other.mode.is_empty() {
            self.mode = other.mode;
        }
        if !other.fort_key.is_empty() {
            self.fort_key = other.fort_key;
        }
        if !other.vault_key.is_empty() {
            self.vault_key = other.vault_key;
        }
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct VivaSettings {
    #[serde(default)]
    pub repo: String,
    #[serde(default)]
    pub bin: String,
    pub build: Option<bool>,
    #[serde(default)]
    pub tunnel: VivaTunnelSettings,
}

impl VivaSettings {
    fn normalize(&mut self) {
        trim_string(&mut self.repo);
        trim_string(&mut self.bin);
        self.tunnel.normalize();
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct VivaTunnelSettings {
    #[serde(default)]
    pub default_profile: String,
    #[serde(default)]
    pub profiles: BTreeMap<String, VivaTunnelProfile>,
}

impl VivaTunnelSettings {
    fn normalize(&mut self) {
        self.default_profile = self.default_profile.trim().to_lowercase();
        if self.profiles.is_empty() {
            return;
        }

        let mut normalized = BTreeMap::new();
        for (key, profile) in std::mem::take(&mut self.profiles) {
            let key = key.trim().to_lowercase();
            if key.is_empty() {
                continue;
            }
            normalized.insert(key, profile.normalized());
        }
        self.profiles = normalized;
        if self.default_profile.is_empty() && self.profiles.contains_key("dev") {
            self.default_profile = "dev".to_owned();
        }
        if !self.default_profile.is_empty() && !self.profiles.contains_key(&self.default_profile) {
            self.default_profile.clear();
        }
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct VivaTunnelProfile {
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub runtime_name: String,
    #[serde(default)]
    pub tunnel_id_env_key: String,
    #[serde(default)]
    pub credentials_env_key: String,
    #[serde(default)]
    pub metrics_addr: String,
    pub no_autoupdate: Option<bool>,
    #[serde(default)]
    pub runtime_dir: String,
    #[serde(default)]
    pub fort_env_file: String,
    #[serde(default)]
    pub routes: Vec<VivaTunnelRoute>,
}

impl VivaTunnelProfile {
    fn normalized(mut self) -> Self {
        trim_string(&mut self.name);
        trim_string(&mut self.runtime_name);
        trim_string(&mut self.tunnel_id_env_key);
        if self.tunnel_id_env_key.is_empty() {
            self.tunnel_id_env_key = "VIVA_CLOUDFLARE_TUNNEL_ID".to_owned();
        }
        trim_string(&mut self.credentials_env_key);
        if self.credentials_env_key.is_empty() {
            self.credentials_env_key = "CLOUDFLARE_TUNNEL_CREDENTIALS_JSON".to_owned();
        }
        trim_string(&mut self.metrics_addr);
        if self.no_autoupdate.is_none() {
            self.no_autoupdate = Some(true);
        }
        trim_string(&mut self.runtime_dir);
        trim_string(&mut self.fort_env_file);
        self.routes = self
            .routes
            .into_iter()
            .filter_map(|mut route| {
                trim_string(&mut route.hostname);
                trim_string(&mut route.service);
                if route.service.is_empty() {
                    return None;
                }
                Some(route)
            })
            .collect();
        self
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct VivaTunnelRoute {
    #[serde(default)]
    pub hostname: String,
    #[serde(default)]
    pub service: String,
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

fn trim_string(value: &mut String) {
    *value = value.trim().to_owned();
}

fn normalize_choice(raw: &str, allowed: &[&str]) -> String {
    let normalized = raw.trim().to_lowercase();
    if allowed.contains(&normalized.as_str()) { normalized } else { String::new() }
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

    #[test]
    fn metadata_only_surf_settings_module_does_not_wipe_core_surf_config() {
        let home = tempdir().expect("tempdir");
        let root = home.path().join(".si");
        let surf_dir = root.join("surf");
        fs::create_dir_all(&surf_dir).expect("mkdir surf settings dir");
        fs::write(
            root.join("settings.toml"),
            r#"
schema_version = 1

[surf]
vnc_password_fort_key = "SURF_VNC_PASSWORD"
vnc_password_fort_repo = "surf"
vnc_password_fort_env = "dev"
"#,
        )
        .expect("write core settings");
        fs::write(
            surf_dir.join("si.settings.toml"),
            "schema_version = 1

[metadata]
updated_at = '2026-03-18T03:19:46Z'
",
        )
        .expect("write metadata-only surf settings");

        let settings = Settings::load(home.path(), None).expect("load settings");

        assert_eq!(settings.surf.vnc_password_fort_key, "SURF_VNC_PASSWORD");
        assert_eq!(settings.surf.vnc_password_fort_repo, "surf");
        assert_eq!(settings.surf.vnc_password_fort_env, "dev");
    }

    #[test]
    fn loads_surf_settings_module() {
        let home = tempdir().expect("tempdir");
        let settings_dir = home.path().join(".si").join("surf");
        fs::create_dir_all(&settings_dir).expect("mkdir surf settings dir");
        fs::write(
            settings_dir.join("si.settings.toml"),
            r#"
[surf]
repo = "/work/surf"
bin = "/work/surf/bin/surf"
build = true
settings_file = "/home/user/.si/surf/settings.toml"
state_dir = "/home/user/.surf-state"
vnc_password_fort_key = "SURF_VNC_PASSWORD"
vnc_password_fort_repo = "surf"
vnc_password_fort_env = "dev"

[surf.tunnel]
name = "surf-cloudflared"
mode = "token"
vault_key = "SURF_CLOUDFLARE_TUNNEL_TOKEN"
"#,
        )
        .expect("write surf settings");

        let settings = Settings::load(home.path(), None).expect("load settings");

        assert_eq!(settings.surf.repo, "/work/surf");
        assert_eq!(settings.surf.bin, "/work/surf/bin/surf");
        assert_eq!(settings.surf.build, Some(true));
        assert_eq!(settings.surf.vnc_password_fort_key, "SURF_VNC_PASSWORD");
        assert_eq!(settings.surf.vnc_password_fort_repo, "surf");
        assert_eq!(settings.surf.vnc_password_fort_env, "dev");
        assert_eq!(settings.surf.tunnel.mode, "token");
        assert_eq!(settings.surf.tunnel.fort_key, "SURF_CLOUDFLARE_TUNNEL_TOKEN");
        assert_eq!(settings.surf.tunnel.vault_key, "SURF_CLOUDFLARE_TUNNEL_TOKEN");
    }

    #[test]
    fn loads_surf_tunnel_fort_key_without_legacy_vault_key() {
        let settings = Settings::from_toml(
            r#"
[surf.tunnel]
mode = "token"
fort_key = "SURF_CLOUDFLARE_TUNNEL_TOKEN"
"#,
        )
        .expect("settings should parse");

        assert_eq!(settings.surf.tunnel.mode, "token");
        assert_eq!(settings.surf.tunnel.fort_key, "SURF_CLOUDFLARE_TUNNEL_TOKEN");
        assert_eq!(settings.surf.tunnel.vault_key, "");
    }

    #[test]
    fn loads_viva_tunnel_settings_module() {
        let home = tempdir().expect("tempdir");
        let settings_dir = home.path().join(".si").join("viva");
        fs::create_dir_all(&settings_dir).expect("mkdir viva settings dir");
        fs::write(
            settings_dir.join("settings.toml"),
            r#"
[viva]
repo = "/work/viva"
bin = "/work/viva/bin/viva"

[viva.tunnel]
default_profile = "dev"

[viva.tunnel.profiles.dev]
runtime_name = "viva-cloudflared-dev-browser"
fort_env_file = "/work/safe/sampleapp/.env.dev"

[[viva.tunnel.profiles.dev.routes]]
hostname = "dev.example.app"
service = "http://127.0.0.1:3000"
"#,
        )
        .expect("write viva settings");

        let settings = Settings::load(home.path(), None).expect("load settings");

        assert_eq!(settings.viva.repo, "/work/viva");
        assert_eq!(settings.viva.bin, "/work/viva/bin/viva");
        assert_eq!(settings.viva.tunnel.default_profile, "dev");
        let profile = settings.viva.tunnel.profiles.get("dev").expect("dev profile");
        assert_eq!(profile.runtime_name, "viva-cloudflared-dev-browser");
        assert_eq!(profile.routes.len(), 1);
        assert_eq!(profile.routes[0].hostname, "dev.example.app");
    }
}
