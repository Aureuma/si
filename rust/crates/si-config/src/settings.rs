use serde::Deserialize;

#[derive(Debug, Clone, Default, Deserialize, PartialEq, Eq)]
pub struct Settings {
    #[serde(default)]
    pub paths: SettingsPaths,
}

impl Settings {
    pub fn from_toml(source: &str) -> Result<Self, toml::de::Error> {
        toml::from_str(source)
    }
}

#[derive(Debug, Clone, Default, Deserialize, PartialEq, Eq)]
pub struct SettingsPaths {
    pub root: Option<String>,
    pub settings_file: Option<String>,
    pub codex_profiles_dir: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::Settings;

    #[test]
    fn parses_paths_subset() {
        let parsed = Settings::from_toml(
            r#"
[paths]
root = "~/.si"
settings_file = "~/.si/settings.toml"
codex_profiles_dir = "~/.si/codex/profiles"
"#,
        )
        .expect("settings should parse");

        assert_eq!(parsed.paths.root.as_deref(), Some("~/.si"));
        assert_eq!(parsed.paths.settings_file.as_deref(), Some("~/.si/settings.toml"));
        assert_eq!(parsed.paths.codex_profiles_dir.as_deref(), Some("~/.si/codex/profiles"));
    }
}
