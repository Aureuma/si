//! Schema migration dry-run: legacy settings (schema 1) to schema 2.
//!
//! Pure inventory and plan computation over `~/.si/settings.toml`. This module
//! never writes: `si doctor --migration-plan` renders [`MigrationPlan`], and a
//! later step applies it (worker-record conversion included) after refusing to
//! run while managed workers are active. Failed validation leaves every native
//! auth/history file intact.

use crate::paths::SiPaths;
use crate::settings::Settings;
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::path::{Component, Path, PathBuf};

/// Schema version written by pre-refocus SI.
pub const LEGACY_SCHEMA_VERSION: u32 = 1;

/// Schema version produced by the migration.
pub const MIGRATION_TARGET_SCHEMA_VERSION: u32 = 2;

/// Dry-run migration plan. `ready` is false when `errors` is non-empty; apply
/// must refuse in that case.
#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct MigrationPlan {
    pub target_schema_version: u32,
    pub selected_codex_profile: Option<String>,
    pub profiles: Vec<ProfileMigration>,
    pub errors: Vec<MigrationError>,
    pub ready: bool,
}

/// One legacy Codex profile and its disposition.
#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct ProfileMigration {
    pub profile: String,
    pub disposition: ProfileDisposition,
}

/// What apply will do with one profile. No disposition copies, merges, or
/// deletes native credentials or history.
#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum ProfileDisposition {
    /// Ordinary `auth.json` store becomes the canonical native home as-is.
    KeepCanonicalHome { native_root: PathBuf },
    /// Ambiguous or missing store: enroll the intended account natively into
    /// a single canonical store instead of guessing.
    NeedsEnrollment { reason: String },
}

/// Blocking migration problems. Apply refuses while any are present.
#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum MigrationError {
    /// Two profiles resolve to one canonical store; SI cannot choose.
    DuplicateStore { store: PathBuf, profiles: Vec<String> },
}

/// Compute the migration plan for legacy settings. Read-only.
pub fn plan_migration(settings: &Settings, paths: &SiPaths) -> MigrationPlan {
    if settings.schema_version >= MIGRATION_TARGET_SCHEMA_VERSION {
        return MigrationPlan {
            target_schema_version: MIGRATION_TARGET_SCHEMA_VERSION,
            selected_codex_profile: settings.codex.profile.clone(),
            profiles: Vec::new(),
            errors: Vec::new(),
            ready: true,
        };
    }

    let mut profiles = Vec::new();
    for (id, entry) in &settings.codex.profiles.entries {
        let disposition = if let Some(native_root) = entry.native_root.as_deref() {
            ProfileDisposition::KeepCanonicalHome {
                native_root: absolutize(paths, Path::new(native_root)),
            }
        } else {
            match entry.auth_path.as_deref() {
                None => ProfileDisposition::NeedsEnrollment {
                    reason: "no auth_path configured; enroll the account with native login"
                        .to_owned(),
                },
                Some(auth_path) => {
                    let auth_path = absolutize(paths, Path::new(auth_path));
                    match auth_path.file_name().and_then(|name| name.to_str()) {
                        Some("auth.json") => match auth_path.parent() {
                            Some(home) if !home.as_os_str().is_empty() => {
                                ProfileDisposition::KeepCanonicalHome {
                                    native_root: home.to_path_buf(),
                                }
                            }
                            _ => ProfileDisposition::NeedsEnrollment {
                                reason: format!(
                                    "auth_path {} has no parent directory",
                                    auth_path.display()
                                ),
                            },
                        },
                        _ => ProfileDisposition::NeedsEnrollment {
                            reason: format!(
                                "custom credential filename {}; enroll the intended account \
                                 into a single canonical store",
                                auth_path.display()
                            ),
                        },
                    }
                }
            }
        };
        profiles.push(ProfileMigration { profile: id.clone(), disposition });
    }

    let mut errors = Vec::new();
    let mut by_store: BTreeMap<PathBuf, Vec<String>> = BTreeMap::new();
    for profile in &profiles {
        if let ProfileDisposition::KeepCanonicalHome { native_root } = &profile.disposition {
            by_store
                .entry(canonicalize_store(native_root))
                .or_default()
                .push(profile.profile.clone());
        }
    }
    for (store, mut owners) in by_store {
        if owners.len() > 1 {
            owners.sort();
            errors.push(MigrationError::DuplicateStore { store, profiles: owners });
        }
    }

    let ready = errors.is_empty();
    MigrationPlan {
        target_schema_version: MIGRATION_TARGET_SCHEMA_VERSION,
        selected_codex_profile: settings.codex.profile.clone(),
        profiles,
        errors,
        ready,
    }
}

/// Best-effort canonical store identity: real canonicalization when the path
/// exists (resolves symlinks), lexical normalization otherwise.
pub fn canonicalize_store(path: &Path) -> PathBuf {
    if let Ok(canonical) = std::fs::canonicalize(path) {
        return canonical;
    }
    normalize_lexical(path)
}

fn absolutize(paths: &SiPaths, path: &Path) -> PathBuf {
    if path.is_absolute() {
        normalize_lexical(path)
    } else {
        normalize_lexical(&paths.root.join(path))
    }
}

fn normalize_lexical(path: &Path) -> PathBuf {
    let mut out = PathBuf::new();
    for component in path.components() {
        match component {
            Component::CurDir => {}
            Component::ParentDir => {
                out.pop();
            }
            other => out.push(other.as_os_str()),
        }
    }
    if out.as_os_str().is_empty() { PathBuf::from(".") } else { out }
}

#[cfg(test)]
mod tests {
    use super::{
        MIGRATION_TARGET_SCHEMA_VERSION, MigrationError, ProfileDisposition, plan_migration,
    };
    use crate::paths::SiPaths;
    use crate::settings::Settings;
    use std::path::Path;

    fn legacy_settings(source: &str) -> Settings {
        Settings::from_toml_with_home_defaults(source, Path::new("/home/user"))
            .expect("settings should parse")
    }

    fn paths() -> SiPaths {
        SiPaths::from_home(Path::new("/home/user"))
    }

    #[test]
    fn ordinary_auth_path_becomes_canonical_home() {
        let settings = legacy_settings(
            r#"
schema_version = 1
[codex]
profile = "personal"
[codex.profiles.entries.personal]
email = "personal@example.invalid"
auth_path = "/home/user/.si/codex/profiles/personal/auth.json"
"#,
        );
        let plan = plan_migration(&settings, &paths());
        assert!(plan.ready);
        assert_eq!(plan.target_schema_version, MIGRATION_TARGET_SCHEMA_VERSION);
        assert_eq!(plan.selected_codex_profile.as_deref(), Some("personal"));
        assert_eq!(plan.profiles.len(), 1);
        assert_eq!(
            plan.profiles[0].disposition,
            ProfileDisposition::KeepCanonicalHome {
                native_root: Path::new("/home/user/.si/codex/profiles/personal").to_path_buf(),
            }
        );
    }

    #[test]
    fn custom_filename_and_missing_auth_need_enrollment() {
        let settings = legacy_settings(
            r#"
[codex.profiles.entries.custom]
auth_path = "/home/user/.si/codex/profiles/custom/creds.json"
[codex.profiles.entries.empty]
"#,
        );
        let plan = plan_migration(&settings, &paths());
        assert!(plan.ready);
        assert!(plan.errors.is_empty());
        for profile in &plan.profiles {
            assert!(
                matches!(profile.disposition, ProfileDisposition::NeedsEnrollment { .. }),
                "{} should need enrollment",
                profile.profile
            );
        }
    }

    #[test]
    fn duplicate_canonical_stores_block_the_plan() {
        let settings = legacy_settings(
            r#"
[codex.profiles.entries.a]
auth_path = "/home/user/.si/codex/profiles/shared/auth.json"
[codex.profiles.entries.b]
auth_path = "/home/user/.si/codex/profiles/shared/./auth.json"
"#,
        );
        let plan = plan_migration(&settings, &paths());
        assert!(!plan.ready);
        assert_eq!(plan.errors.len(), 1);
        match &plan.errors[0] {
            MigrationError::DuplicateStore { store, profiles } => {
                assert_eq!(store, Path::new("/home/user/.si/codex/profiles/shared"));
                assert_eq!(profiles, &["a".to_owned(), "b".to_owned()]);
            }
        }
    }

    #[test]
    fn migrated_schema_is_a_noop() {
        let settings = legacy_settings(
            r#"
schema_version = 2
[codex.profiles.entries.personal]
native_root = "/home/user/.si/codex/profiles/personal"
"#,
        );
        let plan = plan_migration(&settings, &paths());
        assert!(plan.ready && plan.profiles.is_empty() && plan.errors.is_empty());
    }

    #[test]
    fn relative_paths_resolve_against_si_root() {
        let settings = legacy_settings(
            r#"
[codex.profiles.entries.rel]
auth_path = "codex/profiles/rel/auth.json"
"#,
        );
        let plan = plan_migration(&settings, &paths());
        assert!(plan.ready);
        assert_eq!(
            plan.profiles[0].disposition,
            ProfileDisposition::KeepCanonicalHome {
                native_root: Path::new("/home/user/.si/codex/profiles/rel").to_path_buf(),
            }
        );
    }
}
