use crate::settings::Settings;
use std::fs;
use std::path::{Path, PathBuf};
use thiserror::Error;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SiPaths {
    pub root: PathBuf,
    pub settings_file: PathBuf,
    pub codex_profiles_dir: PathBuf,
}

impl SiPaths {
    pub fn from_home(home: &Path) -> Self {
        let root = home.join(".si");
        let settings_file = root.join("settings.toml");
        let codex_profiles_dir = root.join("codex").join("profiles");
        Self { root, settings_file, codex_profiles_dir }
    }

    pub fn from_settings(home: &Path, settings: &Settings) -> Self {
        let mut resolved = Self::from_home(home);

        if let Some(root) = settings.paths.root.as_deref() {
            resolved.root = expand_home(root, home);
        }
        if let Some(settings_file) = settings.paths.settings_file.as_deref() {
            resolved.settings_file = expand_home(settings_file, home);
        } else {
            resolved.settings_file = resolved.root.join("settings.toml");
        }
        if let Some(codex_profiles_dir) = settings.paths.codex_profiles_dir.as_deref() {
            resolved.codex_profiles_dir = expand_home(codex_profiles_dir, home);
        } else {
            resolved.codex_profiles_dir = resolved.root.join("codex").join("profiles");
        }

        resolved
    }

    pub fn load(home: &Path, settings_file: Option<&Path>) -> Result<Self, LoadPathsError> {
        let default_paths = Self::from_home(home);
        let path = settings_file.unwrap_or(default_paths.settings_file.as_path());
        if !path.exists() {
            return Ok(default_paths);
        }

        let source = fs::read_to_string(path)
            .map_err(|source| LoadPathsError::ReadSettings { path: path.to_path_buf(), source })?;
        let settings = Settings::from_toml(&source)
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
}
