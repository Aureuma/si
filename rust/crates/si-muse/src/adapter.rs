//! Muse provider adapter: native environment, login/logout, invocation.
//!
//! Verified against Muse Code 1.4.3 (`Muse Code 1.4.3 (1.4.3-R5018.1)`).
//! The binary reads and writes its credential store at
//! `$XDG_CONFIG_HOME/muse/auth.json` and its sessions under
//! `$XDG_DATA_HOME/muse/`; `MUSE_AUTH_PATH` is honored only by the launcher
//! script for its own update-download auth, never by the binary's
//! `auth set`/`logout` paths (probed with divergent paths: the binary
//! ignores `MUSE_AUTH_PATH` in both directions). SI therefore aligns every
//! XDG variable plus `MUSE_AUTH_PATH` to the profile tree on every managed
//! invocation, so the launcher and the binary agree.
//!
//! Managed-launch argv passes through verbatim with no SI-added flags: unlike
//! Codex there is no store-forcing override, and SI must never prepend
//! root-only options (the audit found that a root option before `exec` can
//! route the invocation into TUI parsing). Credential-input switches,
//! credential-mutating subcommands, provider moves off the enrolled account,
//! and secret-shaped bare tokens are rejected instead.

use serde_json::Value;
use si_process::adapter::{
    AdapterError, AuthStatus, ChildEnv, ProviderAdapter, detect_subcommand, long_flag_name,
    looks_like_bare_secret, validate_child_env,
};
use si_process::{CommandSpec, ProcessRunner, RunOptions};
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use thiserror::Error;

/// Native Muse version this SI release verified its path contract against
/// (`muse --version` first line, verbatim). Any other installed version
/// reports `unverified` until the destination checks are re-run against it.
pub const MUSE_TESTED_VERSION: &str = "Muse Code 1.4.3 (1.4.3-R5018.1)";

/// Credential filename inside `<root>/config/muse/`.
pub const MUSE_AUTH_FILE_NAME: &str = "auth.json";

/// Inherited variables that take precedence over profile login and must be
/// removed from every managed Muse child. `META_API_KEY` priority over
/// account login is documented in native `muse login --help`.
pub const MUSE_AUTH_OVERRIDE_VARS: &[&str] = &["META_API_KEY"];

#[derive(Debug, Default)]
pub struct MuseAdapter;

impl ProviderAdapter for MuseAdapter {
    fn provider_id(&self) -> &'static str {
        "muse"
    }

    fn profile_env(&self, native_root: &Path) -> ChildEnv {
        let root = native_root.display().to_string();
        ChildEnv::from_parts(
            BTreeMap::from([
                ("XDG_CONFIG_HOME".to_owned(), format!("{root}/config")),
                ("XDG_DATA_HOME".to_owned(), format!("{root}/data")),
                ("XDG_CACHE_HOME".to_owned(), format!("{root}/cache")),
                ("XDG_STATE_HOME".to_owned(), format!("{root}/state")),
                ("MUSE_AUTH_PATH".to_owned(), auth_file_path(native_root).display().to_string()),
            ]),
            MUSE_AUTH_OVERRIDE_VARS.iter().map(|key| key.to_string()).collect(),
        )
    }

    fn login_command(&self, program: &Path, native_root: &Path) -> CommandSpec {
        self.profile_env(native_root).apply_to(
            CommandSpec::new(program.display().to_string())
                .args(["login"])
                .current_dir(native_root),
        )
    }

    fn logout_command(&self, program: &Path, native_root: &Path) -> CommandSpec {
        self.profile_env(native_root)
            .apply_to(CommandSpec::new(program.display().to_string()).args(["logout"]))
    }

    fn run_command(
        &self,
        program: &Path,
        native_root: &Path,
        workdir: &Path,
        argv: &[String],
    ) -> Result<CommandSpec, AdapterError> {
        if program.as_os_str().is_empty() {
            return Err(AdapterError::EmptyProgram);
        }
        // Empty argv is the bare interactive native (operator TUI sessions).
        // Headless `run` requires argv at the CLI layer instead.
        validate_native_argv(argv)?;
        let env = self.profile_env(native_root);
        validate_child_env(&env)?;
        Ok(env.apply_to(
            CommandSpec::new(program.display().to_string())
                .args(argv.iter().cloned())
                .current_dir(workdir),
        ))
    }

    fn qualified_status(&self, native_root: &Path) -> AuthStatus {
        let auth_path = auth_file_path(native_root);
        if !auth_path.is_file() {
            return AuthStatus::NotLoggedIn;
        }
        let raw = match std::fs::read(&auth_path) {
            Ok(raw) => raw,
            Err(source) => {
                return AuthStatus::Unknown {
                    detail: format!("read {}: {source}", auth_path.display()),
                };
            }
        };
        match credential_present(&raw) {
            Ok(true) => AuthStatus::CredentialPresentUnverified,
            Ok(false) => AuthStatus::NotLoggedIn,
            Err(detail) => AuthStatus::Unknown { detail },
        }
    }
}

/// Whether the auth file holds a Meta credential: `providers.meta` with a
/// non-empty `api_key` or `access_token`. `Authenticated` is unreachable
/// offline: 1.4.3 offers no read-only native identity facility, so SI never
/// claims it from file presence.
fn credential_present(raw: &[u8]) -> Result<bool, String> {
    let auth: Value =
        serde_json::from_slice(raw).map_err(|_| "auth.json is not valid JSON".to_owned())?;
    let meta = auth.get("providers").and_then(|providers| providers.get("meta"));
    let Some(meta) = meta else { return Ok(false) };
    for field in ["api_key", "access_token"] {
        if meta.get(field).and_then(Value::as_str).is_some_and(|value| !value.trim().is_empty()) {
            return Ok(true);
        }
    }
    Ok(false)
}

/// Canonical auth file for a native root: `<root>/config/muse/auth.json`.
pub fn auth_file_path(native_root: &Path) -> PathBuf {
    native_root.join("config").join("muse").join(MUSE_AUTH_FILE_NAME)
}

/// Long flags (normalized like Codex) that inject credentials into a
/// launch. Verified against `muse auth set --help` and `muse exec --help`
/// on 1.4.3: `--api-key-stdin` is the only credential-input switch, valid
/// in both positions.
const MUSE_CREDENTIAL_SWITCHES: &[&str] = &["api-key-stdin"];

/// Global long flags that consume the following argv token as their value
/// (normalized names; root options plus subcommand options so mixed-order
/// invocations still locate the subcommand). Unknown flags are skipped
/// singly (fail-open).
const MUSE_VALUE_FLAGS: &[&str] = &[
    "provider",
    "preset",
    "model",
    "reasoning-effort",
    "base-url",
    "image",
    "workspace",
    "worktree-base",
    "worktree-existing",
    "approval-mode",
    "permission-profile",
    "approval-judge",
    "echo-delay-ms",
    "sandbox-network",
    "agents",
    "prompt-file",
    "output-schema",
    "session-id",
    "context-compaction-strategy",
    "context-compaction-soft-threshold",
    "context-compaction-hard-threshold",
    "max-model-steps",
    "max-tool-output-bytes",
    "callback-port",
    "oauth-client-id",
    "scope",
    "out",
    "session",
];

/// Global short flags that consume a value. `-w/--worktree` takes an
/// OPTIONAL value and is deliberately absent: skipping one token there is
/// fail-closed (a smuggled `login` is still caught) while skipping two
/// could hide a real subcommand.
const MUSE_SHORT_VALUE_FLAGS: &[u8] = &[];

/// Validate managed-launch argv: the credential-input switch,
/// credential-mutating subcommands, provider moves off the enrolled
/// account, and secret-shaped bare tokens are rejected. Everything else —
/// `exec`, `resume`, `mcp`, prompts, models, permissions, sandbox flags —
/// passes through verbatim.
fn validate_native_argv(argv: &[String]) -> Result<(), AdapterError> {
    reject_credential_switches(argv)?;
    reject_credential_subcommand(argv)?;
    reject_provider_override(argv)?;
    reject_secret_shaped_values(argv)?;
    Ok(())
}

fn reject_credential_switches(argv: &[String]) -> Result<(), AdapterError> {
    for arg in argv {
        let Some(name) = long_flag_name(arg) else { continue };
        if MUSE_CREDENTIAL_SWITCHES.contains(&name.as_str()) {
            let flag = arg.split('=').next().unwrap_or(arg.as_str());
            return Err(AdapterError::CredentialSwitch { provider: "muse", flag: flag.to_owned() });
        }
    }
    Ok(())
}

/// Route credential-mutating subcommands through profile login/logout.
/// 1.4.3 offers no read-only `auth`/`login` form, so all three route.
/// `mcp login/logout` stays allowed: MCP OAuth credentials are auxiliary
/// per-server secrets scoped to the profile store, not the bound Meta
/// account identity, so they cannot confuse accounts.
fn reject_credential_subcommand(argv: &[String]) -> Result<(), AdapterError> {
    let Some(at) = detect_subcommand(argv, MUSE_VALUE_FLAGS, MUSE_SHORT_VALUE_FLAGS) else {
        return Ok(());
    };
    match argv[at].as_str() {
        "auth" | "login" | "logout" => Err(AdapterError::CredentialMutatingCommand {
            provider: "muse",
            command: argv[at].clone(),
        }),
        _ => Ok(()),
    }
}

/// Reject `--provider` values that move the launch off the enrolled Meta
/// account. `echo` (credentialless, used by offline isolation tests) and
/// `meta` (the enrolled provider) pass; `local` reroutes to local models
/// outside the enrolled account and is refused. Unknown values pass to
/// the native, which validates them.
fn reject_provider_override(argv: &[String]) -> Result<(), AdapterError> {
    let mut index = 0;
    while index < argv.len() {
        let arg = argv[index].as_str();
        let value = if arg == "--provider" {
            index += 1;
            argv.get(index).map(String::as_str)
        } else {
            arg.strip_prefix("--provider=")
        };
        if let Some(value) = value
            && value.trim().eq_ignore_ascii_case("local")
        {
            return Err(AdapterError::ProviderOverride {
                provider: "muse",
                flag: "--provider".to_owned(),
            });
        }
        index += 1;
    }
    Ok(())
}

fn reject_secret_shaped_values(argv: &[String]) -> Result<(), AdapterError> {
    for arg in argv {
        if looks_like_bare_secret(arg) {
            return Err(AdapterError::SecretShapedArgument { provider: "muse" });
        }
    }
    Ok(())
}

/// Native version support verdict for one probed `muse --version` line.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum MuseVersionSupport {
    /// Exactly the verified version: the path contract holds.
    Supported,
    /// Anything else: re-run the auth-destination checks before trusting
    /// isolation on this native. SI does not guess newer-vs-older from the
    /// string; any mismatch needs re-verification.
    Unverified,
}

/// Classify a probed version line against [`MUSE_TESTED_VERSION`].
pub fn muse_version_support(version: &str) -> MuseVersionSupport {
    if version.trim() == MUSE_TESTED_VERSION {
        MuseVersionSupport::Supported
    } else {
        MuseVersionSupport::Unverified
    }
}

#[derive(Debug, Error, Eq, PartialEq)]
pub enum MusePlatformError {
    #[error(
        "muse multi-account isolation is not supported on macOS yet: the native \
         credential backend has no documented, tested account scoping there \
         (Keychain isolation is unproven). Codex remains available on Darwin; \
         run `si muse` on Linux"
    )]
    UnsupportedIsolation,
    #[error("muse multi-account isolation supports Linux only; refusing on {os:?}")]
    UnsupportedPlatform { os: String },
}

/// Platform gate for every Muse native launch. Linux is the initial
/// supported platform; macOS fails the documented isolation gate; anything
/// else fails closed. Takes the OS name so tests can pin every branch.
pub fn muse_platform_support(os: &str) -> Result<(), MusePlatformError> {
    match os {
        "linux" => Ok(()),
        "macos" => Err(MusePlatformError::UnsupportedIsolation),
        other => Err(MusePlatformError::UnsupportedPlatform { os: other.to_owned() }),
    }
}

/// Enforce the platform gate on the real host OS.
pub fn ensure_muse_platform_supported() -> Result<(), MusePlatformError> {
    muse_platform_support(std::env::consts::OS)
}

/// Best-effort local account hint parsed from the profile's own auth file:
/// the `email` claim of the Meta `access_token` JWT payload, when present.
///
/// This is a display hint, never independently server-verified identity.
/// API-key entries carry no email; unknown OAuth claim shapes (to be
/// grounded by the live phase) yield `None` rather than a guess.
pub fn local_email_from_auth_file(path: &Path) -> Option<String> {
    let raw = std::fs::read_to_string(path).ok()?;
    let auth = serde_json::from_str::<Value>(&raw).ok()?;
    auth.get("providers")?
        .get("meta")?
        .get("access_token")?
        .as_str()
        .and_then(decode_jwt_payload_claims)
        .and_then(|claims| claims.get("email")?.as_str().map(str::to_owned))
        .map(|value| value.trim().to_owned())
        .filter(|value| !value.is_empty())
}

fn decode_jwt_payload_claims(token: &str) -> Option<Value> {
    use base64::Engine as _;
    let payload = token.trim().split('.').nth(1)?;
    let decoded =
        base64::engine::general_purpose::URL_SAFE_NO_PAD.decode(payload.as_bytes()).ok()?;
    serde_json::from_slice::<Value>(&decoded).ok()
}

/// `resume <session-id>` argv fragment for managed resume.
pub fn resume_argv(session_id: &str) -> Vec<String> {
    vec!["resume".to_owned(), session_id.to_owned()]
}

/// Best-effort native version string: first line of `<program> --version`,
/// trimmed and capped. `None` when the probe fails or prints nothing.
pub fn probe_native_version(program: &Path) -> Option<String> {
    let output = ProcessRunner
        .run(
            &CommandSpec::new(program.display().to_string()).args(["--version"]),
            &RunOptions {
                timeout: Some(std::time::Duration::from_secs(10)),
                ..RunOptions::default()
            },
        )
        .ok()?;
    if !output.status.success() {
        return None;
    }
    let line =
        String::from_utf8_lossy(&output.stdout).lines().next().unwrap_or("").trim().to_owned();
    if line.is_empty() {
        return None;
    }
    let mut capped = line;
    capped.truncate(128);
    Some(capped)
}

/// Native per-account session tree: `<data>/muse/sessions/…`.
pub const MUSE_SESSIONS_DIR_NAME: &str = "sessions";

/// Whether `id` names a session known in this profile's native store.
///
/// Accepts a session UUID whose directory under `<root>/data/muse/sessions/`
/// holds a `session.jsonl` log. Session *names* are refused (unknown here):
/// they resolve through the native sqlite index, which SI does not read,
/// so callers must pass the UUID (`muse export` prints it). Scans are
/// bounded; unreadable stores report unknown rather than failing.
pub fn session_known(native_root: &Path, id: &str) -> bool {
    const MAX_ENTRIES: usize = 8_000;
    const MAX_SCAN_DEPTH: usize = 8;

    let id = id.trim();
    if !is_uuid_shape(id) {
        return false;
    }
    let sessions_root = native_root.join("data").join("muse").join(MUSE_SESSIONS_DIR_NAME);
    let mut stack = vec![(sessions_root, 0_usize)];
    let mut seen = 0_usize;
    while let Some((dir, depth)) = stack.pop() {
        if depth > MAX_SCAN_DEPTH {
            continue;
        }
        let Ok(entries) = std::fs::read_dir(&dir) else {
            continue;
        };
        for entry in entries.flatten() {
            seen += 1;
            if seen > MAX_ENTRIES {
                return false;
            }
            let name = entry.file_name().to_string_lossy().into_owned();
            if entry.file_type().map(|kind| kind.is_dir()).unwrap_or(false) {
                if name == id && entry.path().join("session.jsonl").is_file() {
                    return true;
                }
                stack.push((entry.path(), depth + 1));
            }
        }
    }
    false
}

/// Strict `8-4-4-4-12` lowercase-or-upper hex UUID shape.
fn is_uuid_shape(id: &str) -> bool {
    let parts: Vec<&str> = id.split('-').collect();
    parts.len() == 5
        && [8, 4, 4, 4, 12]
            .into_iter()
            .zip(parts)
            .all(|(len, part)| part.len() == len && part.bytes().all(|b| b.is_ascii_hexdigit()))
}

#[cfg(test)]
mod tests {
    use super::{
        MUSE_AUTH_OVERRIDE_VARS, MUSE_TESTED_VERSION, MuseAdapter, MusePlatformError,
        MuseVersionSupport, auth_file_path, local_email_from_auth_file, muse_platform_support,
        muse_version_support, session_known, validate_native_argv,
    };
    use si_process::adapter::{AdapterError, AuthStatus, ProviderAdapter, validate_child_env};
    use std::path::Path;

    fn adapter() -> MuseAdapter {
        MuseAdapter
    }

    fn argv(words: &[&str]) -> Vec<String> {
        words.iter().map(|word| word.to_string()).collect()
    }

    #[test]
    fn profile_env_aligns_xdg_and_auth_path_and_strips_meta_key() {
        let env = adapter().profile_env(Path::new("/si/muse/profiles/a"));
        for (key, want) in [
            ("XDG_CONFIG_HOME", "/si/muse/profiles/a/config"),
            ("XDG_DATA_HOME", "/si/muse/profiles/a/data"),
            ("XDG_CACHE_HOME", "/si/muse/profiles/a/cache"),
            ("XDG_STATE_HOME", "/si/muse/profiles/a/state"),
            ("MUSE_AUTH_PATH", "/si/muse/profiles/a/config/muse/auth.json"),
        ] {
            assert_eq!(env.set.get(key).map(String::as_str), Some(want), "{key}");
        }
        for key in MUSE_AUTH_OVERRIDE_VARS {
            assert!(env.unset.contains(*key), "{key} must be removed");
        }
        validate_child_env(&env).expect("profile env names must validate");
    }

    #[test]
    fn run_passes_argv_verbatim_with_no_si_added_flags() {
        // Unlike Codex there is no store-forcing override, and SI must never
        // prepend root-only options (a root option before `exec` can route
        // the invocation into TUI parsing instead).
        let args = argv(&["exec", "--provider", "echo", "hello"]);
        let spec = adapter()
            .run_command(Path::new("muse"), Path::new("/h"), Path::new("/w"), &args)
            .expect("run spec");
        assert_eq!(spec.args_slice(), &args);
        assert_eq!(spec.current_dir_path(), Some(Path::new("/w")));
        assert!(spec.env_remove_set().contains("META_API_KEY"));
    }

    #[test]
    fn run_accepts_empty_argv_as_bare_interactive_native() {
        let spec = adapter()
            .run_command(Path::new("muse"), Path::new("/h"), Path::new("/w"), &[])
            .expect("bare run spec");
        assert!(spec.args_slice().is_empty());
    }

    #[test]
    fn argv_rejects_api_key_stdin_in_every_spelling() {
        for words in [
            vec!["auth", "set", "--api-key-stdin"],
            vec!["exec", "--api-key-stdin"],
            vec!["--api-key-stdin"],
            vec!["--api_key_stdin"],
            vec!["exec", "--api-key-stdin=true"],
        ] {
            let args = argv(&words);
            let err = validate_native_argv(&args).expect_err("credential switch must fail");
            assert!(matches!(err, AdapterError::CredentialSwitch { .. }), "{args:?} gave {err:?}");
            assert!(err.to_string().contains("si muse profile login"), "{args:?}");
        }
    }

    #[test]
    fn argv_routes_auth_login_logout_but_allows_mcp_and_prompts() {
        for words in [
            vec!["auth"],
            vec!["auth", "set", "--provider", "meta"],
            vec!["login"],
            vec!["logout"],
            vec!["--provider", "meta", "login"],
        ] {
            let args = argv(&words);
            let err = validate_native_argv(&args).expect_err("mutating command must fail");
            assert!(
                matches!(err, AdapterError::CredentialMutatingCommand { .. }),
                "{args:?} gave {err:?}"
            );
        }
        for words in [
            vec!["mcp", "login", "server"],
            vec!["mcp", "logout", "server"],
            vec!["exec", "login"],
            vec!["exec", "please logout of everything"],
            vec!["resume", "--last"],
            vec!["resume", "0192abcd-0000-4000-8000-000000000000"],
            vec!["serve"],
            vec!["export", "--last", "--out", "t.json"],
            vec!["hello world (bare TUI prompt)"],
        ] {
            let args = argv(&words);
            assert!(validate_native_argv(&args).is_ok(), "{args:?} must pass");
        }
    }

    #[test]
    fn argv_rejects_local_provider_but_keeps_echo_and_meta() {
        for words in [
            vec!["--provider", "local"],
            vec!["--provider=local"],
            vec!["exec", "--provider", "LOCAL"],
        ] {
            let args = argv(&words);
            let err = validate_native_argv(&args).expect_err("provider change must fail");
            assert!(matches!(err, AdapterError::ProviderOverride { .. }), "{args:?} gave {err:?}");
        }
        for words in [
            vec!["exec", "--provider", "echo", "hi"],
            vec!["exec", "--provider=meta", "hi"],
            vec!["--provider", "meta", "exec", "hi"],
            vec!["exec", "--model", "x", "--base-url", "https://proxy.invalid", "hi"],
            vec!["-w", "exec", "hi"],
        ] {
            let args = argv(&words);
            assert!(validate_native_argv(&args).is_ok(), "{args:?} must pass");
        }
    }

    #[test]
    fn argv_rejects_secret_shaped_bare_tokens_without_echoing_them() {
        let jwt = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dGVzdHNpZ25hdHVyZXhhbXBsZQ";
        for secret in ["sk-abcdefgh12345678", jwt] {
            let args = argv(&["exec", secret]);
            let err = validate_native_argv(&args).expect_err("secret shape must fail");
            assert!(
                matches!(err, AdapterError::SecretShapedArgument { .. }),
                "{secret} gave {err:?}"
            );
            assert!(!err.to_string().contains(secret), "error must not echo the secret");
        }
        let args = argv(&["exec", "use the api key from stdin please"]);
        assert!(validate_native_argv(&args).is_ok());
    }

    #[test]
    fn qualified_status_reads_providers_meta_not_file_existence() {
        let temp = tempfile::tempdir().expect("tempdir");
        let root = temp.path().join("a");
        assert_eq!(adapter().qualified_status(&root), AuthStatus::NotLoggedIn);

        let auth_path = auth_file_path(&root);
        std::fs::create_dir_all(auth_path.parent().expect("parent")).expect("mkdir");
        // Post-logout skeleton: file exists, no credential.
        std::fs::write(&auth_path, br#"{"schema_version":1,"providers":{}}"#).expect("write");
        assert_eq!(adapter().qualified_status(&root), AuthStatus::NotLoggedIn);

        std::fs::write(&auth_path, br#"{"schema_version":1,"providers":{"meta":{"api_key":"x"}}}"#)
            .expect("write");
        assert_eq!(adapter().qualified_status(&root), AuthStatus::CredentialPresentUnverified);

        std::fs::write(
            &auth_path,
            br#"{"schema_version":1,"providers":{"meta":{"mechanism":"oauth","access_token":"y"}}}"#,
        )
        .expect("write");
        assert_eq!(adapter().qualified_status(&root), AuthStatus::CredentialPresentUnverified);

        std::fs::write(&auth_path, b"not json").expect("write");
        assert!(matches!(adapter().qualified_status(&root), AuthStatus::Unknown { .. }));
    }

    #[test]
    fn version_support_matches_tested_version_exactly() {
        assert_eq!(muse_version_support(MUSE_TESTED_VERSION), MuseVersionSupport::Supported);
        assert_eq!(
            muse_version_support("Muse Code 1.4.3 (1.4.3-R5018.1)\n"),
            MuseVersionSupport::Supported
        );
        assert_eq!(
            muse_version_support("Muse Code 1.5.0 (1.5.0-R9999.9)"),
            MuseVersionSupport::Unverified
        );
        assert_eq!(muse_version_support(""), MuseVersionSupport::Unverified);
    }

    #[test]
    fn platform_gate_supports_linux_and_fails_macos_and_unknown() {
        assert_eq!(muse_platform_support("linux"), Ok(()));
        assert_eq!(muse_platform_support("macos"), Err(MusePlatformError::UnsupportedIsolation));
        let err = muse_platform_support("macos").expect_err("macos must fail");
        assert!(err.to_string().contains("Keychain"), "{err}");
        assert!(matches!(
            muse_platform_support("freebsd"),
            Err(MusePlatformError::UnsupportedPlatform { .. })
        ));
    }

    #[test]
    fn email_hint_reads_access_token_claims_only() {
        use base64::Engine as _;
        let temp = tempfile::tempdir().expect("tempdir");
        let auth_path = temp.path().join("auth.json");
        let payload = base64::engine::general_purpose::URL_SAFE_NO_PAD
            .encode(br#"{"email":"agent@example.test"}"#);
        let token = format!("header.{payload}.signature");
        std::fs::write(
            &auth_path,
            serde_json::json!({"providers": {"meta": {"access_token": token}}}).to_string(),
        )
        .expect("write");
        assert_eq!(local_email_from_auth_file(&auth_path).as_deref(), Some("agent@example.test"));

        std::fs::write(
            &auth_path,
            serde_json::json!({"providers": {"meta": {"api_key": "not-a-jwt"}}}).to_string(),
        )
        .expect("write");
        assert_eq!(local_email_from_auth_file(&auth_path), None);
    }

    #[test]
    fn session_known_matches_uuid_dirs_with_logs_and_refuses_names() {
        let temp = tempfile::tempdir().expect("tempdir");
        let root = temp.path().join("a");
        let id = "0192abcd-0000-4000-8000-000000000000";
        assert!(!session_known(&root, id));
        assert!(!session_known(&root, "my session name"));
        assert!(!session_known(&root, "../escape"));
        let dir = root.join("data/muse/sessions/2026/10/07").join(id);
        std::fs::create_dir_all(&dir).expect("mkdir");
        assert!(!session_known(&root, id), "dir without session.jsonl is unknown");
        std::fs::write(dir.join("session.jsonl"), "{}\n").expect("write");
        assert!(session_known(&root, id));
        assert!(!session_known(&root, "0192abcd-0000-4000-8000-000000000001"));
    }
}
