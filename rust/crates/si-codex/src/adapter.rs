//! Codex provider adapter: native environment, login/logout, invocation.
//!
//! Verified against Codex CLI 0.160.0 (`codex-cli 0.160.0`): global `-c` config
//! overrides, `login --device-auth`, `login status`, `logout`, and `exec` all
//! behave as used here. Every credential-using invocation forces
//! `cli_auth_credentials_store="file"` via CLI override and rejects user `-c`
//! values that conflict with it, so SI never depends on argument-order folklore
//! or on ambient config for the auth-store contract. Managed-launch argv is
//! further validated: credential switches, credential-mutating subcommands
//! (except read-only `login status`), auth `-c` keys, provider changes, and
//! secret-shaped bare tokens are rejected; user models, permissions,
//! `-p` config profiles, and prompts pass through verbatim.

use serde_json::Value;
use si_process::adapter::{
    AdapterError, AuthStatus, ChildEnv, ProviderAdapter, detect_subcommand, long_flag_name,
    looks_like_bare_secret, validate_child_env,
};
use si_process::{CommandSpec, ProcessRunner, RunOptions, StdinBehavior};
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use thiserror::Error;

/// Native credential filename inside a Codex home.
pub const CODEX_AUTH_FILE_NAME: &str = "auth.json";

/// Config key SI enforces for the supported credential store.
pub const CODEX_STORE_CONFIG_KEY: &str = "cli_auth_credentials_store";

/// CLI override value forcing file credential storage.
pub const CODEX_FILE_STORE_OVERRIDE: &str = "cli_auth_credentials_store=\"file\"";

/// Inherited variables that take precedence over profile login and must be
/// removed from every managed Codex child.
pub const CODEX_AUTH_OVERRIDE_VARS: &[&str] =
    &["OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_ACCESS_TOKEN"];

/// How `codex login` should authenticate the terminal session.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum CodexLoginMode {
    /// Device-code flow (default; works over plain terminal/stdout).
    Device,
    /// Normal browser flow, for hosts where device auth is unavailable.
    Browser,
}

#[derive(Debug, Default)]
pub struct CodexAdapter;

impl CodexAdapter {
    /// `codex -c <file-store> login [--device-auth]` with the profile env.
    pub fn login_command_with_mode(
        &self,
        program: &Path,
        native_root: &Path,
        mode: CodexLoginMode,
    ) -> CommandSpec {
        let mut argv = file_store_argv();
        argv.push("login".to_owned());
        if mode == CodexLoginMode::Device {
            argv.push("--device-auth".to_owned());
        }
        self.profile_env(native_root).apply_to(
            CommandSpec::new(program.display().to_string()).args(argv).current_dir(native_root),
        )
    }

    /// Read-only `codex -c <file-store> login status` for login validation.
    pub fn login_status_command(&self, program: &Path, native_root: &Path) -> CommandSpec {
        let mut argv = file_store_argv();
        argv.push("login".to_owned());
        argv.push("status".to_owned());
        self.profile_env(native_root)
            .apply_to(CommandSpec::new(program.display().to_string()).args(argv))
    }
}

impl ProviderAdapter for CodexAdapter {
    fn provider_id(&self) -> &'static str {
        "codex"
    }

    fn profile_env(&self, native_root: &Path) -> ChildEnv {
        ChildEnv::from_parts(
            BTreeMap::from([("CODEX_HOME".to_owned(), native_root.display().to_string())]),
            CODEX_AUTH_OVERRIDE_VARS.iter().map(|key| key.to_string()).collect(),
        )
    }

    fn login_command(&self, program: &Path, native_root: &Path) -> CommandSpec {
        self.login_command_with_mode(program, native_root, CodexLoginMode::Device)
    }

    fn logout_command(&self, program: &Path, native_root: &Path) -> CommandSpec {
        let mut argv = file_store_argv();
        argv.push("logout".to_owned());
        self.profile_env(native_root)
            .apply_to(CommandSpec::new(program.display().to_string()).args(argv))
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
        let mut full_argv = file_store_argv();
        full_argv.extend(argv.iter().cloned());
        Ok(env.apply_to(
            CommandSpec::new(program.display().to_string()).args(full_argv).current_dir(workdir),
        ))
    }

    fn qualified_status(&self, native_root: &Path) -> AuthStatus {
        let auth_path = native_root.join(CODEX_AUTH_FILE_NAME);
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
        match serde_json::from_slice::<Value>(&raw) {
            Ok(Value::Object(map)) if !map.is_empty() => AuthStatus::CredentialPresentUnverified,
            Ok(Value::Object(_)) => AuthStatus::NotLoggedIn,
            Ok(_) => AuthStatus::Unknown {
                detail: format!("{} is not a JSON object", auth_path.display()),
            },
            Err(_) => {
                AuthStatus::Unknown { detail: format!("{} is not valid JSON", auth_path.display()) }
            }
        }
    }
}

fn file_store_argv() -> Vec<String> {
    vec!["-c".to_owned(), CODEX_FILE_STORE_OVERRIDE.to_owned()]
}

/// Long flags (normalized: lowercase, `--` stripped, `_` unified to `-`,
/// value cut at `=`) that inject credentials into a launch. Verified against
/// `codex --help` and `codex login --help` on Codex CLI 0.160.0; the extra
/// names are fail-closed against native aliases and future additions.
const CODEX_CREDENTIAL_SWITCHES: &[&str] = &[
    "with-api-key",
    "with-access-token",
    "api-key-stdin",
    "api-key",
    "access-token",
    "auth-token",
    "refresh-token",
    "id-token",
    "client-secret",
    "remote-auth-token-env",
];

/// Long flags that move the launch off the enrolled account's provider.
const CODEX_PROVIDER_SWITCHES: &[&str] = &["oss", "local-provider"];

/// Case-insensitive infixes that mark a `-c` config key as credential/auth
/// material. Only the KEY is scanned, never the value, so model names or
/// prompts containing these substrings pass through untouched.
const CODEX_AUTH_KEY_INFIXES: &[&str] =
    &["auth", "token", "key", "secret", "credential", "password", "provider"];

/// Global long flags that consume the following argv token as their value
/// (normalized names). Used only to locate the subcommand position; unknown
/// flags are skipped singly (fail-open) so future native flags keep working.
const CODEX_VALUE_FLAGS: &[&str] = &[
    "config",
    "profile",
    "cd",
    "local-provider",
    "color",
    "enable",
    "disable",
    "remote-auth-token-env",
];

/// Global short flags that consume a value (`-c`, `-p`, `-C`).
const CODEX_SHORT_VALUE_FLAGS: &[u8; 3] = b"cpC";

/// Validate managed-launch argv: credential switches, credential-mutating
/// subcommands, auth `-c` keys, provider changes, and secret-shaped bare
/// tokens are rejected. Everything else — subcommands, `-p` config profiles,
/// non-auth `-c` keys, prompts — passes through verbatim, including shell
/// metacharacters: SI spawns the native directly (CLI argv, or spec JSON to
/// `__worker-run`, then `exec`), so metacharacters in argv values are inert
/// by construction and rejecting them would only break legitimate prompts.
fn validate_native_argv(argv: &[String]) -> Result<(), AdapterError> {
    reject_credential_switches(argv)?;
    reject_credential_subcommand(argv)?;
    reject_credential_config_keys(argv)?;
    reject_secret_shaped_values(argv)?;
    Ok(())
}

fn reject_credential_switches(argv: &[String]) -> Result<(), AdapterError> {
    for arg in argv {
        let Some(name) = long_flag_name(arg) else { continue };
        // Echo the flag only, never anything after `=`: a value there could
        // itself be secret material.
        let flag = arg.split('=').next().unwrap_or(arg.as_str());
        if CODEX_CREDENTIAL_SWITCHES.contains(&name.as_str()) {
            return Err(AdapterError::CredentialSwitch {
                provider: "codex",
                flag: flag.to_owned(),
            });
        }
        if CODEX_PROVIDER_SWITCHES.contains(&name.as_str()) {
            return Err(AdapterError::ProviderOverride {
                provider: "codex",
                flag: flag.to_owned(),
            });
        }
    }
    Ok(())
}

/// Route credential-mutating subcommands through profile login/logout.
/// `login status` is read-only and stays allowed.
fn reject_credential_subcommand(argv: &[String]) -> Result<(), AdapterError> {
    let Some(at) = detect_subcommand(argv, CODEX_VALUE_FLAGS, CODEX_SHORT_VALUE_FLAGS) else {
        return Ok(());
    };
    match argv[at].as_str() {
        "login" => {
            if argv.get(at + 1).map(String::as_str) == Some("status") {
                Ok(())
            } else {
                Err(AdapterError::CredentialMutatingCommand {
                    provider: "codex",
                    command: "login".to_owned(),
                })
            }
        }
        "logout" => Err(AdapterError::CredentialMutatingCommand {
            provider: "codex",
            command: "logout".to_owned(),
        }),
        _ => Ok(()),
    }
}

/// Collect `-c`/`--config` overrides as (flag token, value) pairs. Accepts
/// `-c value`, `-cvalue`, `--config value`, and `--config=value` spellings.
fn config_override_values(argv: &[String]) -> Vec<(&str, &str)> {
    let mut out = Vec::new();
    let mut index = 0;
    while index < argv.len() {
        let arg = argv[index].as_str();
        let value = if arg == "-c" || arg == "--config" {
            index += 1;
            argv.get(index).map(String::as_str)
        } else if let Some(rest) = arg.strip_prefix("--config=") {
            Some(rest)
        } else {
            arg.strip_prefix("-c").filter(|rest| !rest.is_empty())
        };
        if let Some(value) = value {
            out.push((arg, value));
        }
        index += 1;
    }
    out
}

/// Reject `-c` overrides of the enforced store key and of any
/// credential/auth/provider config key. Non-auth keys (model, sandbox,
/// permissions, profiles) pass through.
fn reject_credential_config_keys(argv: &[String]) -> Result<(), AdapterError> {
    for (flag, value) in config_override_values(argv) {
        let key = value.split('=').next().unwrap_or("").trim();
        if key.eq_ignore_ascii_case(CODEX_STORE_CONFIG_KEY) {
            return Err(AdapterError::ConflictingStoreOverride { flag: flag.to_owned() });
        }
        let lowered = key.to_ascii_lowercase();
        if CODEX_AUTH_KEY_INFIXES.iter().any(|infix| lowered.contains(infix)) {
            return Err(AdapterError::CredentialConfigKey {
                provider: "codex",
                key: key.to_owned(),
            });
        }
    }
    Ok(())
}

fn reject_secret_shaped_values(argv: &[String]) -> Result<(), AdapterError> {
    for arg in argv {
        if looks_like_bare_secret(arg) {
            return Err(AdapterError::SecretShapedArgument { provider: "codex" });
        }
    }
    Ok(())
}

/// Best-effort local account hint parsed from the profile's own auth file.
///
/// This is a display hint, never independently server-verified identity.
pub fn local_email_from_auth_file(path: &Path) -> Option<String> {
    let raw = std::fs::read_to_string(path).ok()?;
    let auth = serde_json::from_str::<Value>(&raw).ok()?;
    let tokens = auth.get("tokens")?;

    let access_email = tokens
        .get("access_token")
        .and_then(Value::as_str)
        .and_then(decode_jwt_payload_claims)
        .and_then(|claims| {
            claims.get("https://api.openai.com/profile")?.get("email")?.as_str().map(str::to_owned)
        });
    if let Some(email) =
        access_email.map(|value| value.trim().to_owned()).filter(|value| !value.is_empty())
    {
        return Some(email);
    }

    tokens
        .get("id_token")
        .and_then(Value::as_str)
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

#[derive(Debug, Error)]
pub enum CodexIdentityError {
    #[error("run codex app-server: {0}")]
    Run(#[from] si_process::ProcessError),
    #[error("codex app-server exited with {status}")]
    Exit { status: String },
    #[error("parse codex app-server identity: {0}")]
    Parse(#[from] crate::CodexAppServerStatusError),
}

/// Server-confirmed account email via read-only `app-server` requests.
///
/// Returns `Ok(None)` when the native flow succeeds but reports no ChatGPT
/// account email. Uses the same requests as status parsing (`account/read`
/// with `refreshToken: false` plus rate-limit/config context).
pub fn query_app_server_account_email(
    program: &Path,
    env: &ChildEnv,
    workdir: &Path,
    client_name: &str,
    client_version: &str,
) -> Result<Option<String>, CodexIdentityError> {
    let spec = env.apply_to(
        CommandSpec::new(program.display().to_string()).args(["app-server"]).current_dir(workdir),
    );
    let output = ProcessRunner.run(
        &spec,
        &RunOptions {
            stdin: StdinBehavior::Bytes(crate::build_codex_app_server_status_input(
                client_name,
                client_version,
                Some(workdir.display().to_string()),
            )),
            timeout: Some(std::time::Duration::from_secs(30)),
            ..RunOptions::default()
        },
    )?;
    if !output.status.success() {
        return Err(CodexIdentityError::Exit { status: output.status.to_string() });
    }
    let status = crate::parse_codex_app_server_status(&String::from_utf8_lossy(&output.stdout))?;
    Ok(status.account_email)
}

/// Canonical auth file for a native home.
pub fn auth_file_path(native_root: &Path) -> PathBuf {
    native_root.join(CODEX_AUTH_FILE_NAME)
}

/// Native per-account session index (JSONL with `id`/`thread_name`).
pub const CODEX_SESSION_INDEX_FILE_NAME: &str = "session_index.jsonl";

/// Native per-account session tree root (`sessions/YYYY/MM/DD/...`).
pub const CODEX_SESSIONS_DIR_NAME: &str = "sessions";

/// `resume <session-id>` argv fragment for managed resume (file-store `-c`
/// prefix is added by [`ProviderAdapter::run_command`]).
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

/// Whether `id` names a session known in this profile's native store.
///
/// Accepts an `id` or `thread_name` entry from the profile's
/// `session_index.jsonl`, or a rollout filename under its `sessions/` tree.
/// Scans are bounded; unreadable stores report unknown rather than failing.
/// This answers "known here" only; the native CLI resolves UUID-vs-name
/// precedence itself.
pub fn session_known(native_root: &Path, id: &str) -> bool {
    const MAX_INDEX_LINES: usize = 4_000;
    const MAX_SESSION_FILES: usize = 8_000;
    const MAX_SCAN_DEPTH: usize = 6;

    let id = id.trim();
    if id.is_empty() || id.contains('/') || id.contains('\\') || id == ".." {
        return false;
    }
    let index_path = native_root.join(CODEX_SESSION_INDEX_FILE_NAME);
    if let Ok(raw) = std::fs::read_to_string(&index_path) {
        for line in raw.lines().take(MAX_INDEX_LINES) {
            let line = line.trim();
            if line.is_empty() {
                continue;
            }
            let Ok(entry) = serde_json::from_str::<Value>(line) else {
                continue;
            };
            if entry.get("id").and_then(Value::as_str) == Some(id)
                || entry.get("thread_name").and_then(Value::as_str) == Some(id)
            {
                return true;
            }
        }
    }
    let sessions_root = native_root.join(CODEX_SESSIONS_DIR_NAME);
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
            if seen > MAX_SESSION_FILES {
                return false;
            }
            let name = entry.file_name().to_string_lossy().into_owned();
            if entry.file_type().map(|kind| kind.is_dir()).unwrap_or(false) {
                stack.push((entry.path(), depth + 1));
            } else if name.contains(id) {
                return true;
            }
        }
    }
    false
}

#[cfg(test)]
mod tests {
    use super::{
        CODEX_AUTH_FILE_NAME, CODEX_AUTH_OVERRIDE_VARS, CODEX_FILE_STORE_OVERRIDE, CodexAdapter,
        CodexLoginMode, local_email_from_auth_file, validate_native_argv,
    };
    use si_process::adapter::{AdapterError, AuthStatus, ProviderAdapter, validate_child_env};
    use std::path::Path;

    fn adapter() -> CodexAdapter {
        CodexAdapter
    }

    #[test]
    fn profile_env_scopes_home_and_removes_auth_overrides() {
        let env = adapter().profile_env(Path::new("/si/codex/profiles/a"));
        assert_eq!(env.set.get("CODEX_HOME").map(String::as_str), Some("/si/codex/profiles/a"));
        for key in ["OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_ACCESS_TOKEN"] {
            assert!(env.unset.contains(key), "{key} must be removed");
        }
    }

    #[test]
    fn login_uses_device_auth_by_default_and_browser_on_request() {
        let device = adapter().login_command(Path::new("codex"), Path::new("/si/codex/profiles/a"));
        assert_eq!(
            device.args_slice(),
            &[
                "-c".to_owned(),
                CODEX_FILE_STORE_OVERRIDE.to_owned(),
                "login".to_owned(),
                "--device-auth".to_owned()
            ]
        );
        let browser = adapter().login_command_with_mode(
            Path::new("codex"),
            Path::new("/si/codex/profiles/a"),
            CodexLoginMode::Browser,
        );
        assert!(!browser.args_slice().contains(&"--device-auth".to_owned()));
        assert!(browser.args_slice().contains(&"login".to_owned()));
    }

    #[test]
    fn run_forces_file_store_ahead_of_native_argv() {
        let spec = adapter()
            .run_command(
                Path::new("codex"),
                Path::new("/si/codex/profiles/a"),
                Path::new("/work"),
                &["exec".to_owned(), "--json".to_owned()],
            )
            .expect("run spec");
        let argv = spec.args_slice();
        assert_eq!(argv[0], "-c");
        assert_eq!(argv[1], CODEX_FILE_STORE_OVERRIDE);
        assert_eq!(&argv[2..], &["exec".to_owned(), "--json".to_owned()]);
        assert_eq!(spec.current_dir_path(), Some(Path::new("/work")));
    }

    #[test]
    fn run_accepts_empty_argv_as_bare_interactive_native() {
        let spec = adapter()
            .run_command(
                Path::new("codex"),
                Path::new("/si/codex/profiles/a"),
                Path::new("/work"),
                &[],
            )
            .expect("bare run spec");
        let argv = spec.args_slice();
        assert_eq!(argv, &["-c".to_owned(), CODEX_FILE_STORE_OVERRIDE.to_owned()]);
    }

    #[test]
    fn run_rejects_conflicting_store_overrides_in_every_spelling() {
        for argv in [
            vec!["-c", "cli_auth_credentials_store=\"os\""],
            vec!["--config", "cli_auth_credentials_store=os"],
            vec!["--config=cli_auth_credentials_store=os"],
            vec!["-ccli_auth_credentials_store=os"],
            vec!["exec", "-c", "cli_auth_credentials_store = \"os\""],
        ] {
            let argv: Vec<String> = argv.into_iter().map(str::to_owned).collect();
            let err = adapter()
                .run_command(Path::new("codex"), Path::new("/h"), Path::new("/w"), &argv)
                .expect_err("conflicting override must be rejected");
            assert!(matches!(err, AdapterError::ConflictingStoreOverride { .. }), "{argv:?}");
        }
        for argv in [
            vec!["-c", "model=\"o3\""],
            vec!["--config=sandbox_mode=\"read-only\""],
            vec!["exec", "--profile", "work"],
        ] {
            let argv: Vec<String> = argv.into_iter().map(str::to_owned).collect();
            adapter()
                .run_command(Path::new("codex"), Path::new("/h"), Path::new("/w"), &argv)
                .expect("unrelated flags must pass");
        }
    }

    fn argv(words: &[&str]) -> Vec<String> {
        words.iter().map(|word| word.to_string()).collect()
    }

    #[test]
    fn argv_rejects_credential_switches_in_every_spelling() {
        for words in [
            vec!["exec", "--with-api-key", "prompt"],
            vec!["--with-api-key"],
            vec!["--with-api-key=true"],
            vec!["--with_api_key"],
            vec!["--WITH-API-KEY"],
            vec!["exec", "--with-access-token"],
            vec!["--api-key-stdin"],
            vec!["--api-key", "sk-test"],
            vec!["exec", "--remote-auth-token-env", "MY_TOKEN"],
            vec!["--remote-auth-token-env=MY_TOKEN"],
            vec!["--refresh-token", "x"],
        ] {
            let args = argv(&words);
            let err = validate_native_argv(&args).expect_err("credential switch must fail");
            assert!(matches!(err, AdapterError::CredentialSwitch { .. }), "{args:?} gave {err:?}");
            let detail = err.to_string();
            assert!(detail.contains("si codex profile login"), "{args:?}: {detail}");
            assert!(!detail.contains("sk-test"), "{args:?}: error must not echo values");
        }
    }

    #[test]
    fn argv_rejects_provider_changes() {
        for words in [
            vec!["--oss"],
            vec!["exec", "--oss", "prompt"],
            vec!["--local-provider", "ollama"],
            vec!["--local-provider=ollama"],
        ] {
            let args = argv(&words);
            let err = validate_native_argv(&args).expect_err("provider change must fail");
            assert!(matches!(err, AdapterError::ProviderOverride { .. }), "{args:?} gave {err:?}");
            assert!(err.to_string().contains("enrolled"), "{args:?}");
        }
    }

    #[test]
    fn argv_routes_login_and_logout_but_allows_login_status() {
        for words in [
            vec!["login"],
            vec!["login", "--device-auth"],
            vec!["-c", "model=x", "login"],
            vec!["-p", "cfg", "logout"],
            vec!["logout"],
        ] {
            let args = argv(&words);
            let err = validate_native_argv(&args).expect_err("mutating command must fail");
            assert!(
                matches!(err, AdapterError::CredentialMutatingCommand { .. }),
                "{args:?} gave {err:?}"
            );
        }
    }

    #[test]
    fn argv_allows_read_only_status_and_prompt_positions() {
        for words in [
            vec!["login", "status"],
            vec!["login", "status", "--json"],
            vec!["-c", "model=x", "login", "status"],
            vec!["exec", "login"],
            vec!["exec", "logout"],
            vec!["exec", "please login to staging and check"],
            vec!["exec", "logout of everything now"],
            vec!["resume", "0192abcd-0000-4000-8000-000000000000"],
            vec!["exec", "--output-last-message", "login"],
            vec!["--", "login", "status"],
        ] {
            let args = argv(&words);
            assert!(validate_native_argv(&args).is_ok(), "{args:?} must pass");
        }
    }

    #[test]
    fn argv_rejects_auth_config_keys_but_keeps_model_and_permissions() {
        for words in [
            vec!["-c", "api_key=x"],
            vec!["-c", "model_provider=azure"],
            vec!["-c", "model_providers.azure.api_key=x"],
            vec!["--config=auth.json=y"],
            vec!["-csecret=z"],
            vec!["-c", "credentials_store=os"],
            vec!["exec", "-c", "sandbox_auth=bypass"],
        ] {
            let args = argv(&words);
            let err = validate_native_argv(&args).expect_err("auth key must fail");
            assert!(
                matches!(err, AdapterError::CredentialConfigKey { .. }),
                "{args:?} gave {err:?}"
            );
        }
        for words in [
            vec!["-c", "model=gpt-5"],
            vec!["-c", "model=gpt-key"],
            vec!["-c", "sandbox_mode=read-only"],
            vec!["-c", "approval_policy=never"],
            vec!["-c", "shell_environment_policy.inherit=all"],
            vec!["-p", "myprofile"],
            vec!["--ignore-user-config"],
            vec!["exec", "--json", "prompt"],
            vec!["--max-tokens", "5"],
            vec!["-C", "/tmp/work"],
        ] {
            let args = argv(&words);
            assert!(validate_native_argv(&args).is_ok(), "{args:?} must pass");
        }
    }

    #[test]
    fn argv_rejects_secret_shaped_bare_tokens_without_echoing_them() {
        let jwt = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dGVzdHNpZ25hdHVyZXhhbXBsZQ";
        for secret in [
            "sk-abcdefgh12345678",
            "sk-proj-abcdefgh12345678",
            "ghp_abcdefgh12345678",
            "github_pat_abcdefgh12345678",
            "xoxb-12345678-abcdefgh",
            "AKIAIOSFODNN7EXAMPLE",
            jwt,
        ] {
            let args = argv(&["exec", secret]);
            let err = validate_native_argv(&args).expect_err("secret shape must fail");
            assert!(
                matches!(err, AdapterError::SecretShapedArgument { .. }),
                "{secret} gave {err:?}"
            );
            assert!(!err.to_string().contains(secret), "error must not echo the secret");
        }
        for innocent in [
            "hello world, use the api key from stdin",
            "1.2.3",
            "archive.tar.gz",
            "v1.2.3-rc.1",
            "model=gpt-key",
            "sk-",
            "sk-short",
            "AKIA123",
            "not-a-secret",
        ] {
            let args = argv(&["exec", innocent]);
            assert!(validate_native_argv(&args).is_ok(), "{innocent:?} must pass");
        }
    }

    #[test]
    fn argv_passes_shell_metacharacters_through_verbatim() {
        // Metacharacters are inert: SI spawns the native directly, never via
        // a shell. Rejecting them would only break legitimate prompts.
        let prompt = "explain 2>&1; echo a|b && c$(d)`e`'f'\"g\"\\h";
        let args = argv(&["exec", prompt]);
        assert!(validate_native_argv(&args).is_ok());
        let spec = adapter()
            .run_command(Path::new("codex"), Path::new("/h"), Path::new("/w"), &args)
            .expect("metachar prompt must pass");
        assert!(spec.args_slice().contains(&prompt.to_owned()));
    }

    #[test]
    fn run_validates_child_env_and_strips_every_auth_override() {
        let env = adapter().profile_env(Path::new("/si/codex/profiles/a"));
        validate_child_env(&env).expect("profile env names must validate");
        let spec = adapter()
            .run_command(
                Path::new("codex"),
                Path::new("/si/codex/profiles/a"),
                Path::new("/work"),
                &argv(&["exec", "hi"]),
            )
            .expect("run spec");
        assert_eq!(
            spec.env_map().get("CODEX_HOME").map(String::as_str),
            Some("/si/codex/profiles/a")
        );
        for key in CODEX_AUTH_OVERRIDE_VARS {
            assert!(spec.env_remove_set().contains(*key), "{key} must be removed");
            assert!(!spec.env_map().contains_key(*key), "{key} must not be set");
        }
    }

    #[test]
    fn qualified_status_never_claims_login_from_files_alone() {
        let temp = tempfile::tempdir().expect("tempdir");
        let root = temp.path().join("a");
        std::fs::create_dir_all(&root).expect("mkdir");
        assert_eq!(adapter().qualified_status(&root), AuthStatus::NotLoggedIn);

        std::fs::write(root.join(CODEX_AUTH_FILE_NAME), b"{\"tokens\":{}}").expect("write auth");
        assert_eq!(adapter().qualified_status(&root), AuthStatus::CredentialPresentUnverified);

        std::fs::write(root.join(CODEX_AUTH_FILE_NAME), b"{}").expect("write empty");
        assert_eq!(adapter().qualified_status(&root), AuthStatus::NotLoggedIn);

        std::fs::write(root.join(CODEX_AUTH_FILE_NAME), b"not json").expect("write bad");
        assert!(matches!(adapter().qualified_status(&root), AuthStatus::Unknown { .. }));
    }

    #[test]
    fn local_email_parses_id_token_hint() {
        use base64::Engine as _;
        let temp = tempfile::tempdir().expect("tempdir");
        let claims = base64::engine::general_purpose::URL_SAFE_NO_PAD
            .encode(r#"{"email":"a@example.invalid"}"#);
        let token = format!("header.{claims}.signature");
        let path = temp.path().join("auth.json");
        std::fs::write(&path, format!(r#"{{"tokens":{{"id_token":"{token}"}}}}"#)).expect("write");
        assert_eq!(local_email_from_auth_file(&path).as_deref(), Some("a@example.invalid"));
        std::fs::write(&path, r#"{"tokens":{}}"#).expect("write empty tokens");
        assert_eq!(local_email_from_auth_file(&path), None);
    }

    #[test]
    fn resume_argv_names_session_id() {
        assert_eq!(super::resume_argv("abc-123"), vec!["resume".to_owned(), "abc-123".to_owned()]);
    }

    #[test]
    fn probe_native_version_reads_first_line() {
        let temp = tempfile::tempdir().expect("tempdir");
        let program = temp.path().join("codex");
        std::fs::write(&program, "#!/bin/sh\necho 'codex-cli 0.160.0'\n").expect("write");
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&program, std::fs::Permissions::from_mode(0o755))
                .expect("chmod");
        }
        assert_eq!(super::probe_native_version(&program).as_deref(), Some("codex-cli 0.160.0"));
        assert_eq!(super::probe_native_version(temp.path().join("missing").as_path()), None);
    }

    #[test]
    fn session_known_matches_index_and_rollout_names_only() {
        let temp = tempfile::tempdir().expect("tempdir");
        let root = temp.path().join("a");
        std::fs::create_dir_all(root.join("sessions").join("2026").join("10")).expect("mkdir");
        std::fs::write(
            root.join(super::CODEX_SESSION_INDEX_FILE_NAME),
            "{\"id\":\"uuid-1\",\"thread_name\":\"Fix bug\",\"updated_at\":\"2026-10-07T00:00:00Z\"}\n\
             not-json\n\
             {\"id\":\"uuid-2\",\"thread_name\":\"Other\",\"updated_at\":\"2026-10-07T00:00:00Z\"}\n",
        )
        .expect("write index");
        std::fs::write(
            root.join("sessions").join("2026").join("10").join("rollout-uuid-3.jsonl"),
            "{}\n",
        )
        .expect("write rollout");

        assert!(super::session_known(&root, "uuid-1"));
        assert!(super::session_known(&root, "Fix bug"));
        assert!(super::session_known(&root, "uuid-3"));
        assert!(!super::session_known(&root, "uuid-9"));
        assert!(!super::session_known(&root, ""));
        assert!(!super::session_known(&root, "../escape"));
        assert!(!super::session_known(temp.path().join("missing").as_path(), "uuid-1"));
    }
}
