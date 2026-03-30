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
    pub stripe: StripeSettings,
    #[serde(default)]
    pub aws: AWSSettings,
    #[serde(default)]
    pub gcp: GCPSettings,
    #[serde(default)]
    pub google: GoogleSettings,
    #[serde(default)]
    pub cloudflare: CloudflareSettings,
    #[serde(default)]
    pub apple: AppleSettings,
    #[serde(default)]
    pub openai: OpenAISettings,
    #[serde(default)]
    pub oci: OCISettings,
    #[serde(default)]
    pub github: GitHubSettings,
    #[serde(default)]
    pub workos: WorkOSSettings,
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
            stripe: StripeSettings::default(),
            aws: AWSSettings::default(),
            gcp: GCPSettings::default(),
            google: GoogleSettings::default(),
            cloudflare: CloudflareSettings::default(),
            apple: AppleSettings::default(),
            openai: OpenAISettings::default(),
            oci: OCISettings::default(),
            github: GitHubSettings::default(),
            workos: WorkOSSettings::default(),
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
                self.stripe = payload.stripe;
                self.aws = payload.aws;
                self.gcp = payload.gcp;
                self.google = payload.google;
                self.cloudflare = payload.cloudflare;
                self.apple = payload.apple;
                self.openai = payload.openai;
                self.oci = payload.oci;
                self.github = payload.github;
                self.workos = payload.workos;
            }
            SettingsModule::Surf => {
                let payload: SurfSettingsModule = toml::from_str(source)?;
                self.surf = payload.surf;
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
        self.stripe.normalize();
        self.aws.normalize();
        self.gcp.normalize();
        self.google.normalize();
        self.cloudflare.normalize();
        self.apple.normalize();
        self.openai.normalize();
        self.oci.normalize();
        self.github.normalize();
        self.workos.normalize();
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
    pub stripe: StripeSettings,
    #[serde(default)]
    pub aws: AWSSettings,
    #[serde(default)]
    pub gcp: GCPSettings,
    #[serde(default)]
    pub google: GoogleSettings,
    #[serde(default)]
    pub cloudflare: CloudflareSettings,
    #[serde(default)]
    pub apple: AppleSettings,
    #[serde(default)]
    pub openai: OpenAISettings,
    #[serde(default)]
    pub oci: OCISettings,
    #[serde(default)]
    pub github: GitHubSettings,
    #[serde(default)]
    pub workos: WorkOSSettings,
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
    pub image: Option<String>,
    pub network: Option<String>,
    pub workspace: Option<String>,
    pub workdir: Option<String>,
    pub profile: Option<String>,
    #[serde(default)]
    pub profiles: CodexProfilesSettings,
}

impl CodexSettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.image);
        normalize_option_string(&mut self.network);
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
    pub container_host: Option<String>,
}

impl FortSettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.repo);
        normalize_option_string(&mut self.bin);
        normalize_option_string(&mut self.host);
        normalize_option_string(&mut self.container_host);
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct StripeSettings {
    pub organization: Option<String>,
    pub default_account: Option<String>,
    pub default_env: Option<String>,
    pub log_file: Option<String>,
    #[serde(default)]
    pub accounts: BTreeMap<String, StripeAccountEntry>,
}

impl StripeSettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.organization);
        normalize_option_string(&mut self.default_account);
        normalize_option_string(&mut self.default_env);
        normalize_option_string(&mut self.log_file);
        let mut normalized = BTreeMap::new();
        for (key, mut entry) in std::mem::take(&mut self.accounts) {
            let key = key.trim().to_owned();
            if key.is_empty() {
                continue;
            }
            entry.normalize();
            normalized.insert(key, entry);
        }
        self.accounts = normalized;
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct StripeAccountEntry {
    pub id: Option<String>,
    pub name: Option<String>,
    pub live_key: Option<String>,
    pub sandbox_key: Option<String>,
    pub live_key_env: Option<String>,
    pub sandbox_key_env: Option<String>,
}

impl StripeAccountEntry {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.id);
        normalize_option_string(&mut self.name);
        normalize_option_string(&mut self.live_key);
        normalize_option_string(&mut self.sandbox_key);
        normalize_option_string(&mut self.live_key_env);
        normalize_option_string(&mut self.sandbox_key_env);
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct AWSSettings {
    pub default_account: Option<String>,
    pub default_region: Option<String>,
    pub api_base_url: Option<String>,
    pub log_file: Option<String>,
    #[serde(default)]
    pub accounts: BTreeMap<String, AWSAccountEntry>,
}

impl AWSSettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.default_account);
        normalize_option_string(&mut self.default_region);
        normalize_option_string(&mut self.api_base_url);
        normalize_option_string(&mut self.log_file);
        let mut normalized = BTreeMap::new();
        for (key, mut entry) in std::mem::take(&mut self.accounts) {
            let key = key.trim().to_owned();
            if key.is_empty() {
                continue;
            }
            entry.normalize();
            normalized.insert(key, entry);
        }
        self.accounts = normalized;
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct AWSAccountEntry {
    pub name: Option<String>,
    pub vault_prefix: Option<String>,
    pub region: Option<String>,
    pub access_key_id_env: Option<String>,
    pub secret_access_key_env: Option<String>,
    pub session_token_env: Option<String>,
}

impl AWSAccountEntry {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.name);
        normalize_option_string(&mut self.vault_prefix);
        normalize_option_string(&mut self.region);
        normalize_option_string(&mut self.access_key_id_env);
        normalize_option_string(&mut self.secret_access_key_env);
        normalize_option_string(&mut self.session_token_env);
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct GCPSettings {
    pub default_account: Option<String>,
    pub default_env: Option<String>,
    pub api_base_url: Option<String>,
    pub log_file: Option<String>,
    #[serde(default)]
    pub accounts: BTreeMap<String, GCPAccountEntry>,
}

impl GCPSettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.default_account);
        normalize_option_string(&mut self.default_env);
        normalize_option_string(&mut self.api_base_url);
        normalize_option_string(&mut self.log_file);
        let mut normalized = BTreeMap::new();
        for (key, mut entry) in std::mem::take(&mut self.accounts) {
            let key = key.trim().to_owned();
            if key.is_empty() {
                continue;
            }
            entry.normalize();
            normalized.insert(key, entry);
        }
        self.accounts = normalized;
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct GCPAccountEntry {
    pub name: Option<String>,
    pub vault_prefix: Option<String>,
    pub project_id: Option<String>,
    pub project_id_env: Option<String>,
    pub access_token_env: Option<String>,
    pub api_key_env: Option<String>,
    pub api_base_url: Option<String>,
}

impl GCPAccountEntry {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.name);
        normalize_option_string(&mut self.vault_prefix);
        normalize_option_string(&mut self.project_id);
        normalize_option_string(&mut self.project_id_env);
        normalize_option_string(&mut self.access_token_env);
        normalize_option_string(&mut self.api_key_env);
        normalize_option_string(&mut self.api_base_url);
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct GoogleSettings {
    pub default_account: Option<String>,
    pub default_env: Option<String>,
    pub api_base_url: Option<String>,
    pub vault_file: Option<String>,
    pub log_file: Option<String>,
    #[serde(default)]
    pub youtube: GoogleYouTubeSettings,
    #[serde(default)]
    pub play: GooglePlaySettings,
    #[serde(default)]
    pub accounts: BTreeMap<String, GoogleAccountEntry>,
}

impl GoogleSettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.default_account);
        normalize_option_string(&mut self.default_env);
        normalize_option_string(&mut self.api_base_url);
        normalize_option_string(&mut self.vault_file);
        normalize_option_string(&mut self.log_file);
        self.youtube.normalize();
        self.play.normalize();
        let mut normalized = BTreeMap::new();
        for (key, mut entry) in std::mem::take(&mut self.accounts) {
            let key = key.trim().to_owned();
            if key.is_empty() {
                continue;
            }
            entry.normalize();
            normalized.insert(key, entry);
        }
        self.accounts = normalized;
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct GooglePlaySettings {
    pub api_base_url: Option<String>,
    pub upload_base_url: Option<String>,
    pub custom_app_base_url: Option<String>,
    #[serde(default)]
    pub accounts: BTreeMap<String, GooglePlayAccountEntry>,
}

impl GooglePlaySettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.api_base_url);
        normalize_option_string(&mut self.upload_base_url);
        normalize_option_string(&mut self.custom_app_base_url);
        let mut normalized = BTreeMap::new();
        for (key, mut entry) in std::mem::take(&mut self.accounts) {
            let key = key.trim().to_owned();
            if key.is_empty() {
                continue;
            }
            entry.normalize();
            normalized.insert(key, entry);
        }
        self.accounts = normalized;
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct GooglePlayAccountEntry {
    pub name: Option<String>,
    pub project_id: Option<String>,
    pub project_id_env: Option<String>,
    pub vault_prefix: Option<String>,
    pub developer_account_id: Option<String>,
    pub default_package_name: Option<String>,
    pub default_language_code: Option<String>,
    pub service_account_json_env: Option<String>,
    pub prod_service_account_json_env: Option<String>,
    pub staging_service_account_json_env: Option<String>,
    pub dev_service_account_json_env: Option<String>,
    pub service_account_file: Option<String>,
    pub service_account_file_env: Option<String>,
}

impl GooglePlayAccountEntry {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.name);
        normalize_option_string(&mut self.project_id);
        normalize_option_string(&mut self.project_id_env);
        normalize_option_string(&mut self.vault_prefix);
        normalize_option_string(&mut self.developer_account_id);
        normalize_option_string(&mut self.default_package_name);
        normalize_option_string(&mut self.default_language_code);
        normalize_option_string(&mut self.service_account_json_env);
        normalize_option_string(&mut self.prod_service_account_json_env);
        normalize_option_string(&mut self.staging_service_account_json_env);
        normalize_option_string(&mut self.dev_service_account_json_env);
        normalize_option_string(&mut self.service_account_file);
        normalize_option_string(&mut self.service_account_file_env);
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct GoogleYouTubeSettings {
    pub api_base_url: Option<String>,
    pub upload_base_url: Option<String>,
    pub default_auth_mode: Option<String>,
    pub upload_chunk_size_mb: Option<i64>,
    #[serde(default)]
    pub accounts: BTreeMap<String, GoogleYouTubeAccountEntry>,
}

impl GoogleYouTubeSettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.api_base_url);
        normalize_option_string(&mut self.upload_base_url);
        normalize_option_string(&mut self.default_auth_mode);
        let mut normalized = BTreeMap::new();
        for (key, mut entry) in std::mem::take(&mut self.accounts) {
            let key = key.trim().to_owned();
            if key.is_empty() {
                continue;
            }
            entry.normalize();
            normalized.insert(key, entry);
        }
        self.accounts = normalized;
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct GoogleYouTubeAccountEntry {
    pub name: Option<String>,
    pub project_id: Option<String>,
    pub project_id_env: Option<String>,
    pub vault_prefix: Option<String>,
    pub youtube_api_key_env: Option<String>,
    pub prod_youtube_api_key_env: Option<String>,
    pub staging_youtube_api_key_env: Option<String>,
    pub dev_youtube_api_key_env: Option<String>,
    pub youtube_client_id_env: Option<String>,
    pub youtube_client_secret_env: Option<String>,
    pub youtube_redirect_uri_env: Option<String>,
    pub youtube_refresh_token_env: Option<String>,
    pub default_region_code: Option<String>,
    pub default_language_code: Option<String>,
}

impl GoogleYouTubeAccountEntry {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.name);
        normalize_option_string(&mut self.project_id);
        normalize_option_string(&mut self.project_id_env);
        normalize_option_string(&mut self.vault_prefix);
        normalize_option_string(&mut self.youtube_api_key_env);
        normalize_option_string(&mut self.prod_youtube_api_key_env);
        normalize_option_string(&mut self.staging_youtube_api_key_env);
        normalize_option_string(&mut self.dev_youtube_api_key_env);
        normalize_option_string(&mut self.youtube_client_id_env);
        normalize_option_string(&mut self.youtube_client_secret_env);
        normalize_option_string(&mut self.youtube_redirect_uri_env);
        normalize_option_string(&mut self.youtube_refresh_token_env);
        normalize_option_string(&mut self.default_region_code);
        normalize_option_string(&mut self.default_language_code);
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct GoogleAccountEntry {
    pub name: Option<String>,
    pub project_id: Option<String>,
    pub project_id_env: Option<String>,
    pub api_base_url: Option<String>,
    pub vault_prefix: Option<String>,
    pub places_api_key_env: Option<String>,
    pub prod_places_api_key_env: Option<String>,
    pub staging_places_api_key_env: Option<String>,
    pub dev_places_api_key_env: Option<String>,
    pub default_region_code: Option<String>,
    pub default_language_code: Option<String>,
}

impl GoogleAccountEntry {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.name);
        normalize_option_string(&mut self.project_id);
        normalize_option_string(&mut self.project_id_env);
        normalize_option_string(&mut self.api_base_url);
        normalize_option_string(&mut self.vault_prefix);
        normalize_option_string(&mut self.places_api_key_env);
        normalize_option_string(&mut self.prod_places_api_key_env);
        normalize_option_string(&mut self.staging_places_api_key_env);
        normalize_option_string(&mut self.dev_places_api_key_env);
        normalize_option_string(&mut self.default_region_code);
        normalize_option_string(&mut self.default_language_code);
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct CloudflareSettings {
    pub default_account: Option<String>,
    pub default_env: Option<String>,
    pub api_base_url: Option<String>,
    pub vault_file: Option<String>,
    pub log_file: Option<String>,
    #[serde(default)]
    pub accounts: BTreeMap<String, CloudflareAccountEntry>,
}

impl CloudflareSettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.default_account);
        normalize_option_string(&mut self.default_env);
        normalize_option_string(&mut self.api_base_url);
        normalize_option_string(&mut self.vault_file);
        normalize_option_string(&mut self.log_file);
        let mut normalized = BTreeMap::new();
        for (key, mut entry) in std::mem::take(&mut self.accounts) {
            let key = key.trim().to_owned();
            if key.is_empty() {
                continue;
            }
            entry.normalize();
            normalized.insert(key, entry);
        }
        self.accounts = normalized;
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct CloudflareAccountEntry {
    pub name: Option<String>,
    pub account_id: Option<String>,
    pub account_id_env: Option<String>,
    pub api_base_url: Option<String>,
    pub vault_prefix: Option<String>,
    pub default_zone_id: Option<String>,
    pub default_zone_name: Option<String>,
    pub prod_zone_id: Option<String>,
    pub staging_zone_id: Option<String>,
    pub dev_zone_id: Option<String>,
    pub api_token_env: Option<String>,
}

impl CloudflareAccountEntry {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.name);
        normalize_option_string(&mut self.account_id);
        normalize_option_string(&mut self.account_id_env);
        normalize_option_string(&mut self.api_base_url);
        normalize_option_string(&mut self.vault_prefix);
        normalize_option_string(&mut self.default_zone_id);
        normalize_option_string(&mut self.default_zone_name);
        normalize_option_string(&mut self.prod_zone_id);
        normalize_option_string(&mut self.staging_zone_id);
        normalize_option_string(&mut self.dev_zone_id);
        normalize_option_string(&mut self.api_token_env);
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct AppleSettings {
    pub default_account: Option<String>,
    pub default_env: Option<String>,
    pub api_base_url: Option<String>,
    pub log_file: Option<String>,
    #[serde(default)]
    pub appstore: AppleAppStoreSettings,
}

impl AppleSettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.default_account);
        normalize_option_string(&mut self.default_env);
        normalize_option_string(&mut self.api_base_url);
        normalize_option_string(&mut self.log_file);
        self.appstore.normalize();
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct OpenAISettings {
    pub default_account: Option<String>,
    pub api_base_url: Option<String>,
    pub default_organization_id: Option<String>,
    pub default_project_id: Option<String>,
    pub log_file: Option<String>,
    #[serde(default)]
    pub accounts: BTreeMap<String, OpenAIAccountEntry>,
}

impl OpenAISettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.default_account);
        normalize_option_string(&mut self.api_base_url);
        normalize_option_string(&mut self.default_organization_id);
        normalize_option_string(&mut self.default_project_id);
        normalize_option_string(&mut self.log_file);
        let mut normalized = BTreeMap::new();
        for (key, mut entry) in std::mem::take(&mut self.accounts) {
            let key = key.trim().to_owned();
            if key.is_empty() {
                continue;
            }
            entry.normalize();
            normalized.insert(key, entry);
        }
        self.accounts = normalized;
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct OpenAIAccountEntry {
    pub name: Option<String>,
    pub vault_prefix: Option<String>,
    pub api_base_url: Option<String>,
    pub api_key_env: Option<String>,
    pub admin_api_key_env: Option<String>,
    pub organization_id: Option<String>,
    pub organization_id_env: Option<String>,
    pub project_id: Option<String>,
    pub project_id_env: Option<String>,
}

impl OpenAIAccountEntry {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.name);
        normalize_option_string(&mut self.vault_prefix);
        normalize_option_string(&mut self.api_base_url);
        normalize_option_string(&mut self.api_key_env);
        normalize_option_string(&mut self.admin_api_key_env);
        normalize_option_string(&mut self.organization_id);
        normalize_option_string(&mut self.organization_id_env);
        normalize_option_string(&mut self.project_id);
        normalize_option_string(&mut self.project_id_env);
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct OCISettings {
    pub default_account: Option<String>,
    pub profile: Option<String>,
    pub config_file: Option<String>,
    pub region: Option<String>,
    pub api_base_url: Option<String>,
    pub log_file: Option<String>,
    #[serde(default)]
    pub accounts: BTreeMap<String, OCIAccountEntry>,
}

impl OCISettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.default_account);
        normalize_option_string(&mut self.profile);
        normalize_option_string(&mut self.config_file);
        normalize_option_string(&mut self.region);
        normalize_option_string(&mut self.api_base_url);
        normalize_option_string(&mut self.log_file);
        let mut normalized = BTreeMap::new();
        for (key, mut entry) in std::mem::take(&mut self.accounts) {
            let key = key.trim().to_owned();
            if key.is_empty() {
                continue;
            }
            entry.normalize();
            normalized.insert(key, entry);
        }
        self.accounts = normalized;
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct OCIAccountEntry {
    pub name: Option<String>,
    pub vault_prefix: Option<String>,
    pub profile: Option<String>,
    pub config_file: Option<String>,
    pub region: Option<String>,
    pub tenancy_ocid: Option<String>,
    pub tenancy_ocid_env: Option<String>,
    pub user_ocid: Option<String>,
    pub user_ocid_env: Option<String>,
    pub fingerprint: Option<String>,
    pub fingerprint_env: Option<String>,
    pub private_key_path: Option<String>,
    pub private_key_path_env: Option<String>,
    pub passphrase_env: Option<String>,
    pub compartment_id: Option<String>,
    pub compartment_id_env: Option<String>,
}

impl OCIAccountEntry {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.name);
        normalize_option_string(&mut self.vault_prefix);
        normalize_option_string(&mut self.profile);
        normalize_option_string(&mut self.config_file);
        normalize_option_string(&mut self.region);
        normalize_option_string(&mut self.tenancy_ocid);
        normalize_option_string(&mut self.tenancy_ocid_env);
        normalize_option_string(&mut self.user_ocid);
        normalize_option_string(&mut self.user_ocid_env);
        normalize_option_string(&mut self.fingerprint);
        normalize_option_string(&mut self.fingerprint_env);
        normalize_option_string(&mut self.private_key_path);
        normalize_option_string(&mut self.private_key_path_env);
        normalize_option_string(&mut self.passphrase_env);
        normalize_option_string(&mut self.compartment_id);
        normalize_option_string(&mut self.compartment_id_env);
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct AppleAppStoreSettings {
    pub api_base_url: Option<String>,
    #[serde(default)]
    pub accounts: BTreeMap<String, AppleAppStoreAccountEntry>,
}

impl AppleAppStoreSettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.api_base_url);
        let mut normalized = BTreeMap::new();
        for (key, mut entry) in std::mem::take(&mut self.accounts) {
            let key = key.trim().to_owned();
            if key.is_empty() {
                continue;
            }
            entry.normalize();
            normalized.insert(key, entry);
        }
        self.accounts = normalized;
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct AppleAppStoreAccountEntry {
    pub name: Option<String>,
    pub project_id: Option<String>,
    pub project_id_env: Option<String>,
    pub vault_prefix: Option<String>,
    pub issuer_id: Option<String>,
    pub issuer_id_env: Option<String>,
    pub key_id: Option<String>,
    pub key_id_env: Option<String>,
    pub private_key_pem: Option<String>,
    pub private_key_env: Option<String>,
    pub private_key_file: Option<String>,
    pub private_key_file_env: Option<String>,
    pub default_bundle_id: Option<String>,
    pub default_language: Option<String>,
    pub default_platform: Option<String>,
}

impl AppleAppStoreAccountEntry {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.name);
        normalize_option_string(&mut self.project_id);
        normalize_option_string(&mut self.project_id_env);
        normalize_option_string(&mut self.vault_prefix);
        normalize_option_string(&mut self.issuer_id);
        normalize_option_string(&mut self.issuer_id_env);
        normalize_option_string(&mut self.key_id);
        normalize_option_string(&mut self.key_id_env);
        normalize_option_string(&mut self.private_key_pem);
        normalize_option_string(&mut self.private_key_env);
        normalize_option_string(&mut self.private_key_file);
        normalize_option_string(&mut self.private_key_file_env);
        normalize_option_string(&mut self.default_bundle_id);
        normalize_option_string(&mut self.default_language);
        normalize_option_string(&mut self.default_platform);
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct GitHubSettings {
    pub default_account: Option<String>,
    pub default_auth_mode: Option<String>,
    pub api_base_url: Option<String>,
    pub default_owner: Option<String>,
    #[serde(default)]
    pub accounts: BTreeMap<String, GitHubAccountEntry>,
}

impl GitHubSettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.default_account);
        normalize_option_string(&mut self.default_auth_mode);
        normalize_option_string(&mut self.api_base_url);
        normalize_option_string(&mut self.default_owner);
        let mut normalized = BTreeMap::new();
        for (key, mut entry) in std::mem::take(&mut self.accounts) {
            let key = key.trim().to_owned();
            if key.is_empty() {
                continue;
            }
            entry.normalize();
            normalized.insert(key, entry);
        }
        self.accounts = normalized;
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct GitHubAccountEntry {
    pub name: Option<String>,
    pub owner: Option<String>,
    pub api_base_url: Option<String>,
    pub auth_mode: Option<String>,
    pub vault_prefix: Option<String>,
    pub oauth_access_token: Option<String>,
    pub oauth_token_env: Option<String>,
    pub app_id: Option<i64>,
    pub app_id_env: Option<String>,
    pub app_private_key_pem: Option<String>,
    pub app_private_key_env: Option<String>,
    pub installation_id: Option<i64>,
    pub installation_env: Option<String>,
}

impl GitHubAccountEntry {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.name);
        normalize_option_string(&mut self.owner);
        normalize_option_string(&mut self.api_base_url);
        normalize_option_string(&mut self.auth_mode);
        normalize_option_string(&mut self.vault_prefix);
        normalize_option_string(&mut self.oauth_access_token);
        normalize_option_string(&mut self.oauth_token_env);
        normalize_option_string(&mut self.app_id_env);
        normalize_option_string(&mut self.app_private_key_pem);
        normalize_option_string(&mut self.app_private_key_env);
        normalize_option_string(&mut self.installation_env);
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct WorkOSSettings {
    pub default_account: Option<String>,
    pub default_env: Option<String>,
    pub api_base_url: Option<String>,
    pub default_organization_id: Option<String>,
    pub log_file: Option<String>,
    #[serde(default)]
    pub accounts: BTreeMap<String, WorkOSAccountEntry>,
}

impl WorkOSSettings {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.default_account);
        normalize_option_string(&mut self.default_env);
        normalize_option_string(&mut self.api_base_url);
        normalize_option_string(&mut self.default_organization_id);
        normalize_option_string(&mut self.log_file);
        let mut normalized = BTreeMap::new();
        for (key, mut entry) in std::mem::take(&mut self.accounts) {
            let key = key.trim().to_owned();
            if key.is_empty() {
                continue;
            }
            entry.normalize();
            normalized.insert(key, entry);
        }
        self.accounts = normalized;
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct WorkOSAccountEntry {
    pub name: Option<String>,
    pub vault_prefix: Option<String>,
    pub api_base_url: Option<String>,
    pub api_key_env: Option<String>,
    pub prod_api_key_env: Option<String>,
    pub staging_api_key_env: Option<String>,
    pub dev_api_key_env: Option<String>,
    pub client_id_env: Option<String>,
    pub organization_id: Option<String>,
}

impl WorkOSAccountEntry {
    fn normalize(&mut self) {
        normalize_option_string(&mut self.name);
        normalize_option_string(&mut self.vault_prefix);
        normalize_option_string(&mut self.api_base_url);
        normalize_option_string(&mut self.api_key_env);
        normalize_option_string(&mut self.prod_api_key_env);
        normalize_option_string(&mut self.staging_api_key_env);
        normalize_option_string(&mut self.dev_api_key_env);
        normalize_option_string(&mut self.client_id_env);
        normalize_option_string(&mut self.organization_id);
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
    pub tunnel: SurfTunnelSettings,
}

impl SurfSettings {
    fn normalize(&mut self) {
        trim_string(&mut self.repo);
        trim_string(&mut self.bin);
        trim_string(&mut self.settings_file);
        trim_string(&mut self.state_dir);
        trim_string(&mut self.tunnel.name);
        trim_string(&mut self.tunnel.vault_key);
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
    pub vault_key: String,
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
    #[serde(default)]
    pub node: VivaNodeSettings,
}

impl VivaSettings {
    fn normalize(&mut self) {
        trim_string(&mut self.repo);
        trim_string(&mut self.bin);
        self.tunnel.normalize();
        self.node.normalize();
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
    pub container_name: String,
    #[serde(default)]
    pub tunnel_id_env_key: String,
    #[serde(default)]
    pub credentials_env_key: String,
    #[serde(default)]
    pub metrics_addr: String,
    #[serde(default)]
    pub image: String,
    #[serde(default)]
    pub network_mode: String,
    #[serde(default)]
    pub additional_networks: Vec<String>,
    pub no_autoupdate: Option<bool>,
    pub pull_image: Option<bool>,
    #[serde(default)]
    pub runtime_dir: String,
    #[serde(default)]
    pub vault_env_file: String,
    #[serde(default)]
    pub vault_repo: String,
    #[serde(default)]
    pub vault_env: String,
    #[serde(default)]
    pub routes: Vec<VivaTunnelRoute>,
}

impl VivaTunnelProfile {
    fn normalized(mut self) -> Self {
        trim_string(&mut self.name);
        trim_string(&mut self.container_name);
        trim_string(&mut self.tunnel_id_env_key);
        if self.tunnel_id_env_key.is_empty() {
            self.tunnel_id_env_key = "VIVA_CLOUDFLARE_TUNNEL_ID".to_owned();
        }
        trim_string(&mut self.credentials_env_key);
        if self.credentials_env_key.is_empty() {
            self.credentials_env_key = "CLOUDFLARE_TUNNEL_CREDENTIALS_JSON".to_owned();
        }
        trim_string(&mut self.metrics_addr);
        trim_string(&mut self.image);
        if self.image.is_empty() {
            self.image = "cloudflare/cloudflared:latest".to_owned();
        }
        trim_string(&mut self.network_mode);
        if self.network_mode.is_empty() {
            self.network_mode = "host".to_owned();
        }
        self.additional_networks =
            normalize_unique_names(&self.additional_networks, Some(&self.network_mode));
        if self.no_autoupdate.is_none() {
            self.no_autoupdate = Some(true);
        }
        if self.pull_image.is_none() {
            self.pull_image = Some(true);
        }
        trim_string(&mut self.runtime_dir);
        trim_string(&mut self.vault_env_file);
        trim_string(&mut self.vault_repo);
        if self.vault_repo.is_empty() {
            self.vault_repo = "viva".to_owned();
        }
        self.vault_env = self.vault_env.trim().to_lowercase();
        if self.vault_env.is_empty() {
            self.vault_env = "dev".to_owned();
        }
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

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct VivaNodeSettings {
    #[serde(default)]
    pub default_node: String,
    #[serde(default)]
    pub entries: BTreeMap<String, VivaNodeProfile>,
    #[serde(default)]
    pub bootstrap: VivaNodeBootstrapSettings,
}

impl VivaNodeSettings {
    fn normalize(&mut self) {
        self.default_node = self.default_node.trim().to_lowercase();
        self.bootstrap = std::mem::take(&mut self.bootstrap).normalized();
        if self.entries.is_empty() {
            return;
        }

        let mut normalized = BTreeMap::new();
        for (key, profile) in std::mem::take(&mut self.entries) {
            let key = key.trim().to_lowercase();
            if key.is_empty() {
                continue;
            }
            normalized.insert(key, profile.normalized());
        }
        self.entries = normalized;
        if self.default_node.is_empty() && self.entries.contains_key("default") {
            self.default_node = "default".to_owned();
        }
        if !self.default_node.is_empty() && !self.entries.contains_key(&self.default_node) {
            self.default_node.clear();
        }
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct VivaNodeProfile {
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub host: String,
    #[serde(default)]
    pub port: String,
    #[serde(default)]
    pub user: String,
    #[serde(default)]
    pub host_env_key: String,
    #[serde(default)]
    pub port_env_key: String,
    #[serde(default)]
    pub user_env_key: String,
    #[serde(default)]
    pub identity_file: String,
    #[serde(default)]
    pub identity_file_env_key: String,
    #[serde(default)]
    pub known_hosts_file: String,
    #[serde(default)]
    pub strict_host_key_checking: String,
    #[serde(default)]
    pub connect_timeout_seconds: i32,
    #[serde(default)]
    pub server_alive_interval_seconds: i32,
    #[serde(default)]
    pub server_alive_count_max: i32,
    pub compression: Option<bool>,
    pub multiplex: Option<bool>,
    #[serde(default)]
    pub control_persist: String,
    #[serde(default)]
    pub control_path: String,
    #[serde(default)]
    pub mosh_port: String,
    #[serde(default)]
    pub protocols: VivaNodeProtocols,
}

impl VivaNodeProfile {
    fn normalized(mut self) -> Self {
        trim_string(&mut self.name);
        trim_string(&mut self.description);
        trim_string(&mut self.host);
        trim_string(&mut self.port);
        if self.port.is_empty() {
            self.port = "22".to_owned();
        }
        trim_string(&mut self.user);
        trim_string(&mut self.host_env_key);
        trim_string(&mut self.port_env_key);
        trim_string(&mut self.user_env_key);
        trim_string(&mut self.identity_file);
        trim_string(&mut self.identity_file_env_key);
        trim_string(&mut self.known_hosts_file);
        self.strict_host_key_checking = normalize_choice_or_default(
            &self.strict_host_key_checking,
            &["yes", "accept-new", "no"],
            "yes",
        );
        if self.connect_timeout_seconds <= 0 {
            self.connect_timeout_seconds = 10;
        }
        if self.server_alive_interval_seconds <= 0 {
            self.server_alive_interval_seconds = 30;
        }
        if self.server_alive_count_max <= 0 {
            self.server_alive_count_max = 5;
        }
        if self.compression.is_none() {
            self.compression = Some(true);
        }
        if self.multiplex.is_none() {
            self.multiplex = Some(true);
        }
        trim_string(&mut self.control_persist);
        if self.control_persist.is_empty() {
            self.control_persist = "5m".to_owned();
        }
        trim_string(&mut self.control_path);
        if self.control_path.is_empty() {
            self.control_path = "~/.ssh/cm-si-viva-%C".to_owned();
        }
        trim_string(&mut self.mosh_port);
        self.protocols.normalize();
        self
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct VivaNodeProtocols {
    pub ssh: Option<bool>,
    pub mosh: Option<bool>,
    pub rsync: Option<bool>,
}

impl VivaNodeProtocols {
    fn normalize(&mut self) {
        if self.ssh.is_none() {
            self.ssh = Some(true);
        }
        if self.mosh.is_none() {
            self.mosh = Some(true);
        }
        if self.rsync.is_none() {
            self.rsync = Some(true);
        }
    }
}

#[derive(Debug, Clone, Default, Deserialize, Serialize, PartialEq, Eq)]
pub struct VivaNodeBootstrapSettings {
    #[serde(default)]
    pub source_root: String,
    #[serde(default)]
    pub workspace_dir: String,
    #[serde(default)]
    pub repos: Vec<String>,
    #[serde(default)]
    pub shell_profile: String,
    #[serde(default)]
    pub env_file: String,
    #[serde(default)]
    pub github_token_key: String,
    pub build_si: Option<bool>,
    pub pull_latest: Option<bool>,
    #[serde(default)]
    pub install_orbitals: Vec<String>,
}

impl VivaNodeBootstrapSettings {
    fn normalized(mut self) -> Self {
        trim_string(&mut self.source_root);
        trim_string(&mut self.workspace_dir);
        self.repos = normalize_unique_names(&self.repos, None);
        if self.repos.is_empty() {
            self.repos = default_viva_node_bootstrap_repos();
        }
        trim_string(&mut self.shell_profile);
        if self.shell_profile.is_empty() {
            self.shell_profile = "~/.bashrc".to_owned();
        }
        trim_string(&mut self.env_file);
        if self.env_file.is_empty() {
            self.env_file = "~/.si/node-bootstrap.env".to_owned();
        }
        trim_string(&mut self.github_token_key);
        if self.github_token_key.is_empty() {
            self.github_token_key = "GH_PAT_AUREUMA".to_owned();
        }
        if self.build_si.is_none() {
            self.build_si = Some(true);
        }
        if self.pull_latest.is_none() {
            self.pull_latest = Some(true);
        }
        self.install_orbitals = normalize_unique_names(&self.install_orbitals, None);
        self
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

fn trim_string(value: &mut String) {
    *value = value.trim().to_owned();
}

fn normalize_choice(raw: &str, allowed: &[&str]) -> String {
    let normalized = raw.trim().to_lowercase();
    if allowed.contains(&normalized.as_str()) { normalized } else { String::new() }
}

fn normalize_choice_or_default(raw: &str, allowed: &[&str], default: &str) -> String {
    let normalized = normalize_choice(raw, allowed);
    if normalized.is_empty() { default.to_owned() } else { normalized }
}

fn normalize_unique_names(items: &[String], exclude: Option<&str>) -> Vec<String> {
    let mut seen = BTreeMap::new();
    if let Some(exclude) = exclude {
        let exclude = exclude.trim();
        if !exclude.is_empty() {
            seen.insert(exclude.to_owned(), ());
        }
    }
    let mut out = Vec::new();
    for raw in items {
        let name = raw.trim();
        if name.is_empty() || seen.contains_key(name) {
            continue;
        }
        seen.insert(name.to_owned(), ());
        out.push(name.to_owned());
    }
    out
}

fn default_viva_node_bootstrap_repos() -> Vec<String> {
    vec![
        "si",
        "safe",
        "viva",
        "remote-control",
        "surf",
        "releasemind",
        "lingospeak",
        "aureuma",
        "svelta",
        "convelt",
        "core",
        "homebrew-si",
    ]
    .into_iter()
    .map(str::to_owned)
    .collect()
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
        assert_eq!(settings.surf.tunnel.mode, "token");
        assert_eq!(settings.surf.tunnel.vault_key, "SURF_CLOUDFLARE_TUNNEL_TOKEN");
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
container_name = "viva-cloudflared-dev-browser"
network_mode = "viva-shared"
additional_networks = ["si", "viva-shared", " ", "supabase_default"]
vault_env_file = "/work/safe/sampleapp/.env.dev"
vault_repo = "sampleapp"
vault_env = "dev"

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
        assert_eq!(profile.container_name, "viva-cloudflared-dev-browser");
        assert_eq!(profile.additional_networks, vec!["si", "supabase_default"]);
        assert_eq!(profile.routes.len(), 1);
        assert_eq!(profile.routes[0].hostname, "dev.example.app");
    }

    #[test]
    fn loads_viva_node_settings_module() {
        let home = tempdir().expect("tempdir");
        let settings_dir = home.path().join(".si").join("viva");
        fs::create_dir_all(&settings_dir).expect("mkdir viva settings dir");
        fs::write(
            settings_dir.join("settings.toml"),
            r#"
[viva.node]
default_node = "prod"

[viva.node.entries.prod]
host_env_key = "PROD_SSH_HOST"
user_env_key = "PROD_SSH_USER"
port_env_key = "PROD_SSH_PORT"
strict_host_key_checking = "accept-new"
control_path = "~/.ssh/cm-%C"

[viva.node.entries.prod.protocols]
ssh = true
mosh = false
rsync = true
"#,
        )
        .expect("write viva settings");

        let settings = Settings::load(home.path(), None).expect("load settings");

        assert_eq!(settings.viva.node.default_node, "prod");
        let entry = settings.viva.node.entries.get("prod").expect("prod node");
        assert_eq!(entry.port, "22");
        assert_eq!(entry.strict_host_key_checking, "accept-new");
        assert_eq!(entry.control_path, "~/.ssh/cm-%C");
        assert_eq!(entry.protocols.ssh, Some(true));
        assert_eq!(entry.protocols.mosh, Some(false));
        assert_eq!(entry.protocols.rsync, Some(true));
    }

    #[test]
    fn loads_viva_node_bootstrap_settings_module() {
        let home = tempdir().expect("tempdir");
        let settings_dir = home.path().join(".si").join("viva");
        fs::create_dir_all(&settings_dir).expect("mkdir viva settings dir");
        fs::write(
            settings_dir.join("settings.toml"),
            r#"
[viva.node.bootstrap]
source_root = "/work/src"
workspace_dir = "~/Work"
repos = ["si", "safe", "si"]
shell_profile = "~/.zshrc"
env_file = "~/.si/bootstrap.env"
github_token_key = "GH_PAT_AUREUMA"
build_si = false
pull_latest = false
install_orbitals = ["remote-control", "surf"]
"#,
        )
        .expect("write viva settings");

        let settings = Settings::load(home.path(), None).expect("load settings");
        let bootstrap = settings.viva.node.bootstrap;

        assert_eq!(bootstrap.source_root, "/work/src");
        assert_eq!(bootstrap.workspace_dir, "~/Work");
        assert_eq!(bootstrap.repos, vec!["si", "safe"]);
        assert_eq!(bootstrap.shell_profile, "~/.zshrc");
        assert_eq!(bootstrap.env_file, "~/.si/bootstrap.env");
        assert_eq!(bootstrap.github_token_key, "GH_PAT_AUREUMA");
        assert_eq!(bootstrap.build_si, Some(false));
        assert_eq!(bootstrap.pull_latest, Some(false));
        assert_eq!(bootstrap.install_orbitals, vec!["remote-control", "surf"]);
    }

    #[test]
    fn loads_github_settings_from_core_module() {
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
api_base_url = "https://api.github.com"
default_owner = "Aureuma"

[github.accounts.core]
name = "Core"
owner = "Aureuma"
api_base_url = "https://ghe.example/api/v3"
auth_mode = "oauth"
oauth_token_env = "GITHUB_CORE_TOKEN"
"#,
        )
        .expect("write settings");

        let settings = Settings::load(home.path(), None).expect("load settings");
        assert_eq!(settings.github.default_account.as_deref(), Some("core"));
        assert_eq!(settings.github.default_auth_mode.as_deref(), Some("oauth"));
        assert_eq!(settings.github.default_owner.as_deref(), Some("Aureuma"));
        let account = settings.github.accounts.get("core").expect("core account");
        assert_eq!(account.name.as_deref(), Some("Core"));
        assert_eq!(account.owner.as_deref(), Some("Aureuma"));
        assert_eq!(account.api_base_url.as_deref(), Some("https://ghe.example/api/v3"));
        assert_eq!(account.oauth_token_env.as_deref(), Some("GITHUB_CORE_TOKEN"));
    }
}
