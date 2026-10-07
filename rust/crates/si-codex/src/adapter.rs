//! Codex provider adapter: native environment, login/logout, invocation.
//!
//! Verified against Codex CLI 0.160.0 (`codex-cli 0.160.0`): global `-c` config
//! overrides, `login --device-auth`, `login status`, `logout`, and `exec` all
//! behave as used here. Every credential-using invocation forces
//! `cli_auth_credentials_store="file"` via CLI override and rejects user `-c`
//! values that conflict with it, so SI never depends on argument-order folklore
//! or on ambient config for the auth-store contract.

use serde_json::Value;
use si_process::adapter::{AdapterError, AuthStatus, ChildEnv, ProviderAdapter};
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
        if argv.is_empty() {
            return Err(AdapterError::EmptyArgv);
        }
        reject_conflicting_store_override(argv)?;
        let mut full_argv = file_store_argv();
        full_argv.extend(argv.iter().cloned());
        Ok(self.profile_env(native_root).apply_to(
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

/// Reject user `-c`/`--config` values that override the enforced store key.
///
/// Accepts `-c value`, `-cvalue`, `--config value`, and `--config=value`
/// spellings; anything else passes through untouched.
fn reject_conflicting_store_override(argv: &[String]) -> Result<(), AdapterError> {
    let mut index = 0;
    while index < argv.len() {
        let arg = argv[index].as_str();
        let value = if arg == "-c" || arg == "--config" {
            index += 1;
            argv.get(index).map(String::as_str)
        } else if let Some(rest) = arg.strip_prefix("--config=") {
            Some(rest)
        } else if let Some(rest) = arg.strip_prefix("-c") {
            if rest.is_empty() { None } else { Some(rest) }
        } else {
            None
        };
        if let Some(value) = value {
            let key = value.split('=').next().unwrap_or("").trim();
            if key == CODEX_STORE_CONFIG_KEY {
                return Err(AdapterError::ConflictingStoreOverride { flag: arg.to_owned() });
            }
        }
        index += 1;
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

#[cfg(test)]
mod tests {
    use super::{
        CODEX_AUTH_FILE_NAME, CODEX_FILE_STORE_OVERRIDE, CodexAdapter, CodexLoginMode,
        local_email_from_auth_file,
    };
    use si_process::adapter::{AdapterError, AuthStatus, ProviderAdapter};
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
}
