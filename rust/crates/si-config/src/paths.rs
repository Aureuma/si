use crate::ids::{Provider, SlotId};
use crate::settings::Settings;
use std::fs;
use std::path::{Path, PathBuf};
use thiserror::Error;

/// Environment override for SI's own root (hermetic tests, portable installs).
///
/// It controls SI's root only and never changes the real user `HOME`.
pub const SI_HOME_ENV: &str = "SI_HOME";

/// Resolve SI's root: `$SI_HOME` when set and non-empty, else `~/.si`.
pub fn resolve_si_root(home: &Path) -> PathBuf {
    resolve_si_root_with(home, std::env::var(SI_HOME_ENV).ok())
}

/// Pure helper behind [`resolve_si_root`]: `si_home` is the raw env value.
pub fn resolve_si_root_with(home: &Path, si_home: Option<String>) -> PathBuf {
    match si_home.map(|raw| raw.trim().to_owned()) {
        Some(raw) if !raw.is_empty() => expand_home(&raw, home),
        _ => home.join(".si"),
    }
}

/// Reject profile keys that cannot be embedded in a path (separators, `..`).
/// Entry points report this as a command error; path helpers panic on it.
pub fn check_profile_key_for_paths(key: &str) -> Result<(), String> {
    let trimmed = key.trim();
    if trimmed.is_empty() {
        return Err("profile key is empty".to_owned());
    }
    if trimmed.contains('/') || trimmed.contains('\\') || trimmed == ".." {
        return Err(format!("profile key {key:?} cannot be used in a path"));
    }
    Ok(())
}

fn guard_profile_key(key: &str) {
    if let Err(detail) = check_profile_key_for_paths(key) {
        panic!("{detail}");
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SiPaths {
    pub root: PathBuf,
    pub settings_file: PathBuf,
    pub codex_profiles_dir: PathBuf,
    pub muse_profiles_dir: PathBuf,
}

impl SiPaths {
    pub fn from_home(home: &Path) -> Self {
        Self::from_si_root(home.join(".si"))
    }

    pub fn from_si_root(root: PathBuf) -> Self {
        let settings_file = root.join("settings.toml");
        let codex_profiles_dir = root.join("codex").join("profiles");
        let muse_profiles_dir = root.join("muse").join("profiles");
        Self { root, settings_file, codex_profiles_dir, muse_profiles_dir }
    }

    /// Resolve paths honoring the `SI_HOME` override.
    pub fn discover(home: &Path) -> Self {
        Self::from_si_root(resolve_si_root(home))
    }

    pub fn from_settings(home: &Path, settings: &Settings) -> Self {
        // A set SI_HOME wins over stored path overrides so hermetic roots and
        // portable installations move as one tree. Otherwise stored settings
        // win over defaults. (An explicitly passed settings *file* still
        // selects which file is read; this only resolves derived paths.)
        let si_home_override = std::env::var(SI_HOME_ENV).is_ok_and(|raw| !raw.trim().is_empty());
        let mut resolved = Self::discover(home);

        if !si_home_override && let Some(root) = settings.paths.root.as_deref() {
            resolved.root = expand_home(root, home);
        }
        if let Some(settings_file) = settings.paths.settings_file.as_deref()
            && !si_home_override
        {
            resolved.settings_file = expand_home(settings_file, home);
        } else {
            resolved.settings_file = resolved.root.join("settings.toml");
        }
        if let Some(codex_profiles_dir) = settings.paths.codex_profiles_dir.as_deref()
            && !si_home_override
        {
            resolved.codex_profiles_dir = expand_home(codex_profiles_dir, home);
        } else {
            resolved.codex_profiles_dir = resolved.root.join("codex").join("profiles");
        }
        if let Some(muse_profiles_dir) = settings.paths.muse_profiles_dir.as_deref()
            && !si_home_override
        {
            resolved.muse_profiles_dir = expand_home(muse_profiles_dir, home);
        } else {
            resolved.muse_profiles_dir = resolved.root.join("muse").join("profiles");
        }

        resolved
    }

    /// Provider state directory: `<root>/<provider>/`.
    pub fn provider_dir(&self, provider: Provider) -> PathBuf {
        self.root.join(provider.as_str())
    }

    /// Canonical profiles directory for a provider.
    pub fn profiles_dir(&self, provider: Provider) -> PathBuf {
        match provider {
            Provider::Codex => self.codex_profiles_dir.clone(),
            Provider::Muse => self.muse_profiles_dir.clone(),
        }
    }

    /// Default canonical native root for a (new) profile: `<profiles>/<id>/`.
    ///
    /// New IDs are validated slugs; legacy keys pass through after a
    /// traversal guard so pre-refocus profiles keep working.
    pub fn default_native_root(&self, provider: Provider, profile: &str) -> PathBuf {
        guard_profile_key(profile);
        self.profiles_dir(provider).join(profile.trim())
    }

    /// Worker lifecycle records: `<root>/<provider>/workers/`.
    pub fn workers_dir(&self, provider: Provider) -> PathBuf {
        self.provider_dir(provider).join("workers")
    }

    /// One worker record: `<root>/<provider>/workers/<profile>/<slot>.json`.
    pub fn worker_record_path(&self, provider: Provider, profile: &str, slot: &SlotId) -> PathBuf {
        guard_profile_key(profile);
        self.workers_dir(provider).join(profile.trim()).join(format!("{}.json", slot.as_str()))
    }

    /// Profile locks: `<root>/locks/<provider>/`.
    pub fn locks_dir(&self, provider: Provider) -> PathBuf {
        self.root.join("locks").join(provider.as_str())
    }

    /// Management lock for one profile: `<root>/locks/<provider>/<profile>.lock`.
    pub fn profile_lock_path(&self, provider: Provider, profile: &str) -> PathBuf {
        guard_profile_key(profile);
        self.locks_dir(provider).join(format!("{}.lock", profile.trim()))
    }

    /// Worker-admission lease for one profile:
    /// `<root>/locks/<provider>/<profile>.worker.lock`. Held for the whole
    /// duration of a credential-using worker; one holder per profile until
    /// same-profile concurrency is proven for the native version.
    pub fn worker_lease_path(&self, provider: Provider, profile: &str) -> PathBuf {
        guard_profile_key(profile);
        self.locks_dir(provider).join(format!("{}.worker.lock", profile.trim()))
    }

    /// Global settings write lock: `<root>/locks/settings.lock`.
    pub fn settings_lock_path(&self) -> PathBuf {
        self.root.join("locks").join("settings.lock")
    }

    /// Slot reservation lock for one worker:
    /// `<root>/locks/<provider>/<profile>.<slot>.lock`. Unambiguous because
    /// slugs never contain `.`.
    pub fn slot_lock_path(&self, provider: Provider, profile: &str, slot: &SlotId) -> PathBuf {
        guard_profile_key(profile);
        self.locks_dir(provider).join(format!("{}.{}.lock", profile.trim(), slot.as_str()))
    }

    /// Private runtime directory for owned sockets: `<root>/runtime/` (0700).
    pub fn runtime_dir(&self) -> PathBuf {
        self.root.join("runtime")
    }

    /// SI-owned tmux socket: `<root>/runtime/tmux.sock`. One per SI root;
    /// every lifecycle command carries this exact path via `tmux -S`.
    pub fn tmux_socket_path(&self) -> PathBuf {
        self.runtime_dir().join("tmux.sock")
    }

    /// Transient private launch spec for one worker:
    /// `<root>/<provider>/workers/<profile>/<slot>.spec.json` (0600). Written
    /// at reservation, read once by the runner, removed with the record.
    pub fn worker_spec_path(&self, provider: Provider, profile: &str, slot: &SlotId) -> PathBuf {
        guard_profile_key(profile);
        self.workers_dir(provider).join(profile.trim()).join(format!("{}.spec.json", slot.as_str()))
    }

    /// Migration metadata backups: `<root>/backups/`. Metadata only; never
    /// auth files or native history.
    pub fn migration_backup_dir(&self) -> PathBuf {
        self.root.join("backups")
    }

    pub fn load(home: &Path, settings_file: Option<&Path>) -> Result<Self, LoadPathsError> {
        let default_paths = Self::discover(home);
        let path = settings_file.unwrap_or(default_paths.settings_file.as_path());
        if !path.exists() {
            return Ok(default_paths);
        }

        let source = fs::read_to_string(path)
            .map_err(|source| LoadPathsError::ReadSettings { path: path.to_path_buf(), source })?;
        let settings = Settings::from_toml_with_home_defaults(&source, home)
            .map_err(|source| LoadPathsError::ParseSettings { path: path.to_path_buf(), source })?;

        Ok(Self::from_settings(home, &settings))
    }
}

#[derive(Debug, Error)]
pub enum LoadPathsError {
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

fn expand_home(value: &str, home: &Path) -> PathBuf {
    if value == "~" {
        return home.to_path_buf();
    }
    if let Some(stripped) = value.strip_prefix("~/") {
        return home.join(stripped);
    }
    PathBuf::from(value)
}

#[cfg(test)]
mod tests {
    use super::{LoadPathsError, SiPaths};
    use crate::settings::Settings;
    use std::path::Path;
    use tempfile::tempdir;

    #[test]
    fn defaults_to_home_scoped_paths() {
        let paths = SiPaths::from_home(Path::new("/tmp/test-home"));
        assert_eq!(paths.root, Path::new("/tmp/test-home/.si"));
        assert_eq!(paths.settings_file, Path::new("/tmp/test-home/.si/settings.toml"));
        assert_eq!(paths.codex_profiles_dir, Path::new("/tmp/test-home/.si/codex/profiles"));
        assert_eq!(paths.muse_profiles_dir, Path::new("/tmp/test-home/.si/muse/profiles"));
    }

    #[test]
    fn applies_settings_overrides_with_home_expansion() {
        let settings = Settings::from_toml(
            r#"
[paths]
root = "~/Library/Application Support/si"
codex_profiles_dir = "~/profiles/codex"
"#,
        )
        .expect("settings should parse");

        let paths = SiPaths::from_settings(Path::new("/Users/alex"), &settings);

        assert_eq!(paths.root, Path::new("/Users/alex/Library/Application Support/si"));
        assert_eq!(
            paths.settings_file,
            Path::new("/Users/alex/Library/Application Support/si/settings.toml")
        );
        assert_eq!(paths.codex_profiles_dir, Path::new("/Users/alex/profiles/codex"));
    }

    #[test]
    fn falls_back_to_defaults_when_settings_file_is_missing() {
        let home = tempdir().expect("tempdir");
        let paths = SiPaths::load(home.path(), None).expect("defaults should resolve");

        assert_eq!(paths.root, home.path().join(".si"));
    }

    #[test]
    fn loads_paths_from_explicit_settings_file() {
        let home = tempdir().expect("tempdir");
        let settings_dir = home.path().join(".si");
        std::fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
        let settings_path = settings_dir.join("settings.toml");
        std::fs::write(
            &settings_path,
            r#"
[paths]
root = "~/state/si"
settings_file = "~/config/si/settings.toml"
codex_profiles_dir = "~/state/si/codex-profiles"
"#,
        )
        .expect("write settings");

        let paths = SiPaths::load(home.path(), Some(&settings_path)).expect("paths should load");

        assert_eq!(paths.root, home.path().join("state/si"));
        assert_eq!(paths.settings_file, home.path().join("config/si/settings.toml"));
        assert_eq!(paths.codex_profiles_dir, home.path().join("state/si/codex-profiles"));
    }

    #[test]
    fn returns_parse_error_for_invalid_settings() {
        let home = tempdir().expect("tempdir");
        let settings_dir = home.path().join(".si");
        std::fs::create_dir_all(&settings_dir).expect("mkdir settings dir");
        let settings_path = settings_dir.join("settings.toml");
        std::fs::write(&settings_path, "[paths\nroot = 1").expect("write settings");

        let err = SiPaths::load(home.path(), Some(&settings_path)).expect_err("load should fail");

        match err {
            LoadPathsError::ParseSettings { path, .. } => assert_eq!(path, settings_path),
            other => panic!("unexpected error: {other:?}"),
        }
    }

    #[test]
    fn si_home_override_controls_root_only() {
        use super::resolve_si_root_with;
        let home = Path::new("/home/user");
        assert_eq!(
            resolve_si_root_with(home, Some("/tmp/isolated-si".to_owned())),
            Path::new("/tmp/isolated-si")
        );
        assert_eq!(resolve_si_root_with(home, Some("  ".to_owned())), home.join(".si"));
        assert_eq!(resolve_si_root_with(home, None), home.join(".si"));
        assert_eq!(
            resolve_si_root_with(home, Some("~/portable/si".to_owned())),
            home.join("portable/si")
        );
    }

    #[test]
    fn derives_provider_lock_and_worker_paths() {
        use crate::ids::{Provider, SlotId};
        let paths = SiPaths::from_home(Path::new("/home/user"));
        let slot = SlotId::or_primary(None).expect("slot");
        assert_eq!(
            paths.default_native_root(Provider::Muse, "personal"),
            Path::new("/home/user/.si/muse/profiles/personal")
        );
        assert_eq!(
            paths.worker_record_path(Provider::Codex, "personal", &slot),
            Path::new("/home/user/.si/codex/workers/personal/primary.json")
        );
        assert_eq!(
            paths.profile_lock_path(Provider::Muse, "personal"),
            Path::new("/home/user/.si/locks/muse/personal.lock")
        );
        assert_eq!(
            paths.worker_lease_path(Provider::Codex, "personal"),
            Path::new("/home/user/.si/locks/codex/personal.worker.lock")
        );
        assert_eq!(paths.settings_lock_path(), Path::new("/home/user/.si/locks/settings.lock"));
        assert_eq!(
            paths.slot_lock_path(Provider::Codex, "personal", &slot),
            Path::new("/home/user/.si/locks/codex/personal.primary.lock")
        );
        assert_eq!(paths.tmux_socket_path(), Path::new("/home/user/.si/runtime/tmux.sock"));
        assert_eq!(
            paths.worker_spec_path(Provider::Codex, "personal", &slot),
            Path::new("/home/user/.si/codex/workers/personal/primary.spec.json")
        );
        assert_eq!(paths.migration_backup_dir(), Path::new("/home/user/.si/backups"));
    }

    #[test]
    fn profile_key_guard_rejects_traversal() {
        use super::check_profile_key_for_paths;
        assert!(check_profile_key_for_paths("personal").is_ok());
        assert!(check_profile_key_for_paths("legacy display name").is_ok());
        assert!(check_profile_key_for_paths("a/b").is_err());
        assert!(check_profile_key_for_paths("..").is_err());
        assert!(check_profile_key_for_paths("  ").is_err());
    }
}
