//! Shared agent-store context: home, settings, paths, profile resolution.
//!
//! Reads go through [`AgentContext`]; mutations go through [`SettingsDoc`]
//! with locked, atomic replacement that preserves unknown TOML keys.

use anyhow::{Context, Result};
use si_config::ids::Provider;
use si_config::io::{create_private_dir_all, write_private_file};
use si_config::paths::SiPaths;
use si_config::settings::Settings;
use std::fs;
use std::path::{Path, PathBuf};

/// Real user home for SI's own paths. Never the profile-overridden value.
pub fn default_home_dir() -> PathBuf {
    std::env::var_os("HOME")
        .map(PathBuf::from)
        .filter(|path| !path.as_os_str().is_empty())
        .unwrap_or_else(|| PathBuf::from("/"))
}

/// Loaded SI root state for one CLI invocation.
pub struct AgentContext {
    pub home: PathBuf,
    pub settings_path: PathBuf,
    pub settings: Settings,
    pub paths: SiPaths,
}

impl AgentContext {
    /// Load settings and paths. Honors `--home`, an explicit settings file,
    /// and the `SI_HOME` root override (in that file-selection order).
    pub fn load(home: Option<PathBuf>, settings_file: Option<PathBuf>) -> Result<Self> {
        let home = home.unwrap_or_else(default_home_dir);
        let settings_path = match settings_file {
            Some(path) => path,
            None => SiPaths::discover(&home).settings_file,
        };
        let settings = Settings::load(&home, Some(&settings_path))?;
        let paths = SiPaths::from_settings(&home, &settings);
        Ok(Self { home, settings_path, settings, paths })
    }

    /// Re-read settings from disk after another writer may have changed them.
    pub fn reload(&mut self) -> Result<()> {
        self.settings = Settings::load(&self.home, Some(&self.settings_path))?;
        self.paths = SiPaths::from_settings(&self.home, &self.settings);
        Ok(())
    }

    /// Atomically persist a settings document edit.
    ///
    /// Parent directories under the SI root are created `0700`; an explicit
    /// settings file outside the root keeps its own parent modes. The file
    /// itself is always written `0600`.
    pub fn save_doc(&self, doc: &SettingsDoc) -> Result<()> {
        let parent = doc
            .path
            .parent()
            .ok_or_else(|| anyhow::anyhow!("settings path {} has no parent", doc.path.display()))?;
        if parent == self.paths.root || parent.starts_with(&self.paths.root) {
            create_private_dir_all(parent)?;
        } else if !parent.is_dir() {
            fs::create_dir_all(parent)
                .with_context(|| format!("create settings dir {}", parent.display()))?;
        }
        let source = toml::to_string_pretty(&toml::Value::Table(doc.document.clone()))?;
        write_private_file(&doc.path, source.as_bytes())?;
        Ok(())
    }

    /// Known profile keys for a provider, sorted.
    pub fn known_profiles(&self, provider: Provider) -> Vec<String> {
        let mut keys: Vec<String> = match provider {
            Provider::Codex => self.settings.codex.profiles.entries.keys().cloned().collect(),
            Provider::Muse => self.settings.muse.profiles.entries.keys().cloned().collect(),
        };
        keys.sort();
        keys
    }

    /// Selected (default) profile for future launches, if any.
    pub fn selected_profile(&self, provider: Provider) -> Option<String> {
        let (profile, active) = match provider {
            Provider::Codex => (&self.settings.codex.profile, &self.settings.codex.profiles.active),
            Provider::Muse => (&self.settings.muse.profile, &self.settings.muse.profiles.active),
        };
        active
            .as_deref()
            .map(str::trim)
            .filter(|value| !value.is_empty())
            .map(str::to_owned)
            .or_else(|| {
                profile
                    .as_deref()
                    .map(str::trim)
                    .filter(|value| !value.is_empty())
                    .map(str::to_owned)
            })
    }

    /// Resolve an explicit-or-default profile key. Unknown names are refused
    /// with the known list; nothing is guessed, prompted, or fuzzy-matched,
    /// because selecting the wrong account is the failure this design removes.
    pub fn resolve_profile(&self, provider: Provider, requested: Option<&str>) -> Result<String> {
        let known = self.known_profiles(provider);
        match requested.map(str::trim).filter(|value| !value.is_empty()) {
            Some(id) => {
                if known.iter().any(|key| key == id) {
                    si_config::paths::check_profile_key_for_paths(id).map_err(|detail| {
                        anyhow::anyhow!("{detail}; rename the key in settings.toml")
                    })?;
                    Ok(id.to_owned())
                } else {
                    anyhow::bail!(
                        "unknown {} profile {id:?}; known profiles: {}",
                        provider.as_str(),
                        if known.is_empty() { "(none)".to_owned() } else { known.join(", ") }
                    )
                }
            }
            None => {
                let selected = self.selected_profile(provider).ok_or_else(|| {
                    if known.is_empty() {
                        anyhow::anyhow!(
                            "no {} profile given and none selected; add one with `si {} profile add <id>`",
                            provider.as_str(),
                            provider.as_str()
                        )
                    } else {
                        anyhow::anyhow!(
                            "no {} profile given and none selected; pass one of {} or run `si {} profile select <id>`",
                            provider.as_str(),
                            known.join(", "),
                            provider.as_str()
                        )
                    }
                })?;
                if !known.iter().any(|key| key == &selected) {
                    anyhow::bail!(
                        "selected {} profile {selected:?} is not registered; known profiles: {}",
                        provider.as_str(),
                        if known.is_empty() { "(none)".to_owned() } else { known.join(", ") }
                    );
                }
                si_config::paths::check_profile_key_for_paths(&selected).map_err(|detail| {
                    anyhow::anyhow!("{detail}; rename the key in settings.toml")
                })?;
                Ok(selected)
            }
        }
    }
}

/// Surgical TOML settings editor. Unknown keys survive the round-trip.
pub struct SettingsDoc {
    pub path: PathBuf,
    pub document: toml::map::Map<String, toml::Value>,
}

impl SettingsDoc {
    pub fn load(path: &Path) -> Result<Self> {
        Ok(Self { path: path.to_path_buf(), document: crate::load_settings_document(path)? })
    }
}

/// Resolve a native provider program: explicit `--<name>-bin` wins, else PATH.
/// Refuses to run SI itself as the native CLI.
pub fn resolve_native_program(explicit: Option<PathBuf>, name: &str) -> Result<PathBuf> {
    let program = match explicit {
        Some(path) => {
            if !path.is_file() {
                anyhow::bail!("native {name} not found at {}", path.display());
            }
            path
        }
        None => crate::resolve_program_on_path(name)
            .ok_or_else(|| anyhow::anyhow!("native {name} not found on PATH; pass --{name}-bin"))?,
    };
    if let (Ok(candidate), Ok(current)) =
        (program.canonicalize(), std::env::current_exe().and_then(|exe| exe.canonicalize()))
    {
        if candidate == current {
            anyhow::bail!(
                "refusing to run SI itself ({}) as the native {name} CLI",
                program.display()
            );
        }
    }
    Ok(program)
}

/// Expand a leading `~` against the real user home.
pub fn expand_home(path: PathBuf) -> PathBuf {
    let text = path.to_string_lossy();
    if text == "~" {
        return default_home_dir();
    }
    if let Some(suffix) = text.strip_prefix("~/") {
        return default_home_dir().join(suffix);
    }
    path
}

/// Workdir: explicit flag (relative joins the workspace), else workspace.
pub fn resolve_workdir(workdir: Option<&str>, workspace: &Path) -> Result<PathBuf> {
    let Some(workdir) = workdir.map(str::trim).filter(|value| !value.is_empty()) else {
        return Ok(workspace.to_path_buf());
    };
    let path = expand_home(PathBuf::from(workdir));
    if path.is_absolute() { Ok(path) } else { Ok(workspace.join(path)) }
}
