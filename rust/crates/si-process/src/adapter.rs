//! Provider adapter interface.
//!
//! The native CLI owns login, credentials, renewal, model requests, and
//! conversation persistence. SI owns profile metadata, process environment,
//! tmux ownership, and lifecycle records. Adapters are the narrow bridge:
//! native environment, login/logout, invocation assembly, and qualified local
//! auth status. They perform no network IO and store no credentials.

use crate::CommandSpec;
use serde::{Deserialize, Serialize};
use std::collections::{BTreeMap, BTreeSet};
use std::path::Path;
use thiserror::Error;

/// Environment delta for one native child process.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct ChildEnv {
    /// Variables to set (profile paths, SI labels).
    pub set: BTreeMap<String, String>,
    /// Inherited variables to remove (auth overrides, conflicting paths).
    pub unset: BTreeSet<String>,
}

impl ChildEnv {
    pub fn from_parts(set: BTreeMap<String, String>, unset: BTreeSet<String>) -> Self {
        debug_assert!(
            set.keys().all(|key| !unset.contains(key)),
            "child env must not set and unset the same key"
        );
        Self { set, unset }
    }

    /// Apply the delta to a typed command spec.
    pub fn apply_to(&self, spec: CommandSpec) -> CommandSpec {
        let mut spec = spec;
        for key in &self.unset {
            spec = spec.env_remove(key);
        }
        for (key, value) in &self.set {
            spec = spec.env(key, value);
        }
        spec
    }
}

/// Locally qualified auth state. `Authenticated` requires a native result, not
/// mere file existence; see each adapter for its verification source.
#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(tag = "state", rename_all = "snake_case")]
pub enum AuthStatus {
    NotLoggedIn,
    CredentialPresentUnverified,
    Authenticated { account: Option<String>, identity_verified: bool },
    ReauthRequired { reason: String },
    NetworkFailure { detail: String },
    QuotaExhausted { detail: String },
    UnsupportedBackend { detail: String },
    Unknown { detail: String },
}

impl AuthStatus {
    /// Stable machine-readable state name for text rendering.
    pub fn state_name(&self) -> &'static str {
        match self {
            Self::NotLoggedIn => "not_logged_in",
            Self::CredentialPresentUnverified => "credential_present_unverified",
            Self::Authenticated { .. } => "authenticated",
            Self::ReauthRequired { .. } => "reauth_required",
            Self::NetworkFailure { .. } => "network_failure",
            Self::QuotaExhausted { .. } => "quota_exhausted",
            Self::UnsupportedBackend { .. } => "unsupported_backend",
            Self::Unknown { .. } => "unknown",
        }
    }
}

#[derive(Debug, Error, Eq, PartialEq)]
pub enum AdapterError {
    #[error("native program path is required")]
    EmptyProgram,
    #[error("native argv is empty")]
    EmptyArgv,
}

/// Native command assembly and local status for one provider.
///
/// Implementations live in the provider crates (`si-codex`, `si-muse`) and
/// stay limited to these capabilities: environment, login/logout, invocation,
/// and qualified status. Object-safe so runners can hold boxed adapters.
pub trait ProviderAdapter {
    /// Stable provider id: `"codex"` or `"muse"`.
    fn provider_id(&self) -> &'static str;

    /// Child environment for `native_root`: profile paths to set plus
    /// inherited auth/path overrides to remove.
    fn profile_env(&self, native_root: &Path) -> ChildEnv;

    /// Native login scoped to `native_root` (same env as run/spawn).
    fn login_command(&self, program: &Path, native_root: &Path) -> CommandSpec;

    /// Native logout scoped to `native_root`.
    fn logout_command(&self, program: &Path, native_root: &Path) -> CommandSpec;

    /// Managed model invocation: profile env, controlled workdir, typed argv.
    fn run_command(
        &self,
        program: &Path,
        native_root: &Path,
        workdir: &Path,
        argv: &[String],
    ) -> Result<CommandSpec, AdapterError>;

    /// Local auth-state qualification from the profile store. No network, no
    /// model prompt, no login side effects.
    fn qualified_status(&self, native_root: &Path) -> AuthStatus;
}

#[cfg(test)]
mod tests {
    use super::{AuthStatus, ChildEnv};
    use crate::{CommandSpec, ProcessRunner, RunOptions};
    use std::collections::{BTreeMap, BTreeSet};

    #[test]
    fn child_env_sets_profile_paths_and_removes_overrides() {
        let env = ChildEnv::from_parts(
            BTreeMap::from([("CODEX_HOME".to_owned(), "/tmp/profiles/a".to_owned())]),
            BTreeSet::from(["OPENAI_API_KEY".to_owned()]),
        );
        let spec = env.apply_to(
            CommandSpec::new("sh")
                .args(["-c", "printf '%s|%s' \"$CODEX_HOME\" \"${OPENAI_API_KEY:-missing}\""])
                .env("OPENAI_API_KEY", "seeded-override"),
        );
        let output = ProcessRunner.run(&spec, &RunOptions::default()).expect("run probe");
        assert!(output.status.success());
        assert_eq!(String::from_utf8_lossy(&output.stdout), "/tmp/profiles/a|missing");
    }

    #[test]
    fn auth_status_names_and_json_are_stable() {
        assert_eq!(AuthStatus::NotLoggedIn.state_name(), "not_logged_in");
        assert_eq!(
            AuthStatus::CredentialPresentUnverified.state_name(),
            "credential_present_unverified"
        );
        let authenticated = AuthStatus::Authenticated {
            account: Some("personal@example.invalid".to_owned()),
            identity_verified: false,
        };
        assert_eq!(authenticated.state_name(), "authenticated");
        let json = serde_json::to_value(&authenticated).expect("serialize status");
        assert_eq!(json["state"], "authenticated");
        assert_eq!(json["identity_verified"], false);
        let roundtrip: AuthStatus = serde_json::from_value(json).expect("deserialize status");
        assert_eq!(roundtrip, authenticated);
    }
}
