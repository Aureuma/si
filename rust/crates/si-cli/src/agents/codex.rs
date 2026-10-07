//! Codex account operations: profiles, login/logout, foreground run.
//!
//! One SI profile is one Codex account with its own canonical `CODEX_HOME`.
//! The native `codex` CLI owns credentials and renewal; SI owns metadata,
//! child environment, and lifecycle records. There is no global auth swap,
//! no per-worker credential copy, no Fort coupling, and no forced unsafe mode.

use anyhow::{Context, Result};
use chrono::Utc;
use si_codex::adapter::{
    CodexAdapter, CodexLoginMode, auth_file_path, local_email_from_auth_file,
    query_app_server_account_email,
};
use si_config::ids::{ProfileId, Provider};
use si_config::io::create_private_dir_all;
use si_config::paths::SiPaths;
use si_config::settings::CodexProfileEntry;
use si_process::adapter::{ChildEnv, ProviderAdapter};
use si_process::lease::{Lease, LeaseError};
use si_process::{ProcessRunner, RunOptions};
use std::path::{Path, PathBuf};

use super::store::{AgentContext, SettingsDoc};
use crate::{OutputFormat, ensure_toml_table, set_toml_string};

const PROVIDER: Provider = Provider::Codex;
const IDENTITY_UNVERIFIED: &str = "identity_unverified";
const APP_SERVER_IDENTITY_SOURCE: &str = "codex app-server account/read";

fn adapter() -> CodexAdapter {
    CodexAdapter
}

/// Canonical native home for a profile: stored `native_root`, else the parent
/// of an ordinary `auth.json` path, else the default profile directory.
///
/// A custom credential filename refuses to guess: the account must be
/// enrolled into a single canonical store instead.
pub fn canonical_native_home(
    paths: &SiPaths,
    key: &str,
    entry: &CodexProfileEntry,
) -> Result<PathBuf> {
    if let Some(root) =
        entry.native_root.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        let path = PathBuf::from(root);
        return Ok(if path.is_absolute() { path } else { paths.root.join(path) });
    }
    if let Some(auth) = entry.auth_path.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        let auth = PathBuf::from(auth);
        let auth = if auth.is_absolute() { auth } else { paths.root.join(auth) };
        if auth.file_name().and_then(|name| name.to_str()) == Some("auth.json") {
            if let Some(home) = auth.parent().filter(|home| !home.as_os_str().is_empty()) {
                return Ok(home.to_path_buf());
            }
        }
        anyhow::bail!(
            "codex profile {key:?} has a custom credential filename ({}); enroll the intended \
             account into a single canonical store instead of guessing",
            auth.display()
        );
    }
    si_config::paths::check_profile_key_for_paths(key)
        .map_err(|detail| anyhow::anyhow!("{detail}; rename the key in settings.toml"))?;
    Ok(paths.default_native_root(PROVIDER, key))
}

/// Resolve the native `codex` program: explicit `--codex-bin` wins, else PATH.
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

/// One active credential-using worker of a profile.
pub struct ActiveWorker {
    pub slot: String,
    pub session: String,
}

/// Workers to stop before login/logout/remove: live tmux records plus any
/// holder of the worker-admission lease (foreground `run` leaves no record).
pub fn active_workers(ctx: &AgentContext, key: &str) -> Result<Vec<ActiveWorker>> {
    let mut active = Vec::new();
    for state in crate::read_codex_worker_states(&ctx.paths)? {
        if state.profile_id != key {
            continue;
        }
        let term = std::env::var("TERM").unwrap_or_else(|_| "xterm-256color".to_owned());
        if crate::tmux_session_exists("tmux", &term, &state.session_name)? {
            active.push(ActiveWorker {
                slot: state.worker_slot.clone(),
                session: state.session_name.clone(),
            });
        }
    }
    match Lease::try_acquire(&ctx.paths.worker_lease_path(PROVIDER, key), "probe")? {
        Some(_free) => {}
        None => active.push(ActiveWorker {
            slot: "(active lease)".to_owned(),
            session: "foreground run or untracked holder".to_owned(),
        }),
    }
    Ok(active)
}

fn refuse_if_workers_active(ctx: &AgentContext, key: &str, action: &str) -> Result<()> {
    let active = active_workers(ctx, key)?;
    if active.is_empty() {
        return Ok(());
    }
    let mut message =
        format!("cannot {action} codex profile {key:?} while credential-using workers are active:");
    for worker in &active {
        message.push_str(&format!("\n  slot {} ({})", worker.slot, worker.session));
    }
    message.push_str(&format!(
        "\nstop them first, e.g. `si codex stop --profile {key} --slot <slot>`; other profiles stay usable"
    ));
    anyhow::bail!(message);
}

fn acquire_management_lease(ctx: &AgentContext, key: &str, action: &str) -> Result<Lease> {
    match Lease::acquire_or_busy(&ctx.paths.profile_lock_path(PROVIDER, key), action) {
        Ok(lease) => Ok(lease),
        Err(LeaseError::Busy { .. }) => {
            anyhow::bail!(
                "codex profile {key:?} is busy with another login/logout/enrollment change; wait for it and retry"
            )
        }
        Err(other) => Err(other.into()),
    }
}

fn labeled_env(native_root: &Path, key: &str) -> ChildEnv {
    let mut env = adapter().profile_env(native_root);
    env.set.insert("SI_PROVIDER".to_owned(), PROVIDER.as_str().to_owned());
    env.set.insert("SI_PROFILE".to_owned(), key.to_owned());
    env
}

fn now_rfc3339() -> String {
    Utc::now().to_rfc3339_opts(chrono::SecondsFormat::Secs, true)
}

/// Create an empty profile: validated ID, empty native tree, metadata only.
/// Never imports, copies, or touches the default account.
pub fn profile_add(
    ctx: &mut AgentContext,
    id: &str,
    name: Option<String>,
    email: Option<String>,
    format: OutputFormat,
) -> Result<()> {
    let pid = ProfileId::parse(id)?;
    if ctx.settings.codex.profiles.entries.contains_key(pid.as_str()) {
        anyhow::bail!("codex profile {:?} already exists", pid.as_str());
    }
    let native_root = ctx.paths.default_native_root(PROVIDER, pid.as_str());
    create_private_dir_all(&native_root)?;

    let _settings_lock = Lease::acquire(&ctx.paths.settings_lock_path(), "codex profile add")?;
    let mut doc = SettingsDoc::load(&ctx.settings_path)?;
    if !doc.document.contains_key("schema_version") {
        doc.document.insert("schema_version".to_owned(), toml::Value::Integer(1));
    }
    let codex = ensure_toml_table(&mut doc.document, "codex")?;
    let profiles = ensure_toml_table(codex, "profiles")?;
    let entries = ensure_toml_table(profiles, "entries")?;
    let entry = ensure_toml_table(entries, pid.as_str())?;
    set_toml_string(entry, "name", name);
    set_toml_string(entry, "email", email);
    set_toml_string(entry, "native_root", Some(native_root.display().to_string()));
    ctx.save_doc(&doc)?;
    ctx.reload()?;
    profile_show(ctx, Some(pid.as_str()), format)
}

struct ProfileRow {
    key: String,
    selected: bool,
    name: Option<String>,
    email: Option<String>,
    native_root: Option<String>,
    auth_state: String,
    identity: Option<String>,
    identity_source: Option<String>,
    enrolled_at: Option<String>,
    disabled: Option<String>,
}

fn collect_rows(ctx: &AgentContext) -> Vec<ProfileRow> {
    let selected = ctx.selected_profile(PROVIDER);
    let mut rows = Vec::new();
    for key in ctx.known_profiles(PROVIDER) {
        let entry = ctx.settings.codex.profiles.entries.get(&key);
        let native_root =
            entry.and_then(|entry| canonical_native_home(&ctx.paths, &key, entry).ok());
        let auth_state = match entry.and_then(|entry| entry.disabled.as_deref()) {
            Some(reason) if !reason.trim().is_empty() => "disabled".to_owned(),
            _ => match native_root.as_deref() {
                Some(root) => adapter().qualified_status(root).state_name().to_owned(),
                None => "unknown".to_owned(),
            },
        };
        let is_selected = selected.as_deref() == Some(key.as_str());
        rows.push(ProfileRow {
            key,
            selected: is_selected,
            name: entry.and_then(|entry| entry.name.clone()),
            email: entry.and_then(|entry| entry.email.clone()),
            native_root: native_root.map(|root| root.display().to_string()),
            auth_state,
            identity: entry.and_then(|entry| entry.identity.clone()),
            identity_source: entry.and_then(|entry| entry.identity_source.clone()),
            enrolled_at: entry.and_then(|entry| entry.enrolled_at.clone()),
            disabled: entry.and_then(|entry| entry.disabled.clone()),
        });
    }
    rows
}

fn row_json(row: &ProfileRow) -> serde_json::Value {
    serde_json::json!({
        "profile": row.key,
        "selected": row.selected,
        "name": row.name,
        "email": row.email,
        "native_root": row.native_root,
        "auth_state": row.auth_state,
        "identity": row.identity,
        "identity_source": row.identity_source,
        "enrolled_at": row.enrolled_at,
        "disabled": row.disabled,
    })
}

/// Local metadata, selection, native location, qualified auth state.
/// No tokens, no network request.
pub fn profile_list(ctx: &AgentContext, format: OutputFormat) -> Result<()> {
    let rows = collect_rows(ctx);
    match format {
        OutputFormat::Json => {
            let values: Vec<serde_json::Value> = rows.iter().map(row_json).collect();
            println!("{}", serde_json::to_string_pretty(&values)?);
        }
        OutputFormat::Text => {
            let mut table = comfy_table::Table::new();
            table.set_header(["PROFILE", "SEL", "NAME", "EMAIL", "AUTH", "NATIVE ROOT"]);
            for row in &rows {
                table.add_row([
                    row.key.as_str(),
                    if row.selected { "*" } else { "" },
                    row.name.as_deref().unwrap_or("-"),
                    row.email.as_deref().unwrap_or("-"),
                    row.auth_state.as_str(),
                    row.native_root.as_deref().unwrap_or("(ambiguous store)"),
                ]);
            }
            println!("{table}");
        }
    }
    Ok(())
}

pub fn profile_show(ctx: &AgentContext, profile: Option<&str>, format: OutputFormat) -> Result<()> {
    let key = ctx.resolve_profile(PROVIDER, profile)?;
    let row = collect_rows(ctx)
        .into_iter()
        .find(|row| row.key == key)
        .ok_or_else(|| anyhow::anyhow!("codex profile {key:?} is not registered"))?;
    match format {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&row_json(&row))?);
        }
        OutputFormat::Text => {
            println!("profile:      {}", row.key);
            println!("selected:     {}", if row.selected { "yes" } else { "no" });
            println!("name:         {}", row.name.as_deref().unwrap_or("-"));
            println!("email:        {}", row.email.as_deref().unwrap_or("-"));
            println!("native_root:  {}", row.native_root.as_deref().unwrap_or("(ambiguous store)"));
            println!("auth_state:   {}", row.auth_state);
            println!("identity:     {}", row.identity.as_deref().unwrap_or("-"));
            println!("id_source:    {}", row.identity_source.as_deref().unwrap_or("-"));
            println!("enrolled_at:  {}", row.enrolled_at.as_deref().unwrap_or("-"));
            println!("disabled:     {}", row.disabled.as_deref().unwrap_or("-"));
        }
    }
    Ok(())
}

/// Native login scoped to the profile, with identity binding.
///
/// Refuses while credential-using workers are active. A mismatch against the
/// expected or already-bound identity disables launch and errors; native
/// state is never deleted or retargeted by SI.
pub fn profile_login(
    ctx: &mut AgentContext,
    profile: Option<&str>,
    codex_bin: Option<PathBuf>,
    browser: bool,
    format: OutputFormat,
) -> Result<()> {
    let key = ctx.resolve_profile(PROVIDER, profile)?;
    let entry = ctx
        .settings
        .codex
        .profiles
        .entries
        .get(&key)
        .cloned()
        .ok_or_else(|| anyhow::anyhow!("codex profile {key:?} is not registered"))?;
    let native_root = canonical_native_home(&ctx.paths, &key, &entry)?;
    create_private_dir_all(&native_root)?;
    let _lease = acquire_management_lease(ctx, &key, "login")?;
    refuse_if_workers_active(ctx, &key, "log in")?;

    let program = resolve_native_program(codex_bin, "codex")?;
    let mode = if browser { CodexLoginMode::Browser } else { CodexLoginMode::Device };
    let spec = adapter()
        .login_command_with_mode(&program, &native_root, mode)
        .env("SI_PROVIDER", PROVIDER.as_str())
        .env("SI_PROFILE", &key);
    let status = ProcessRunner.status_inherit(&spec)?;
    if !status.success() {
        anyhow::bail!("codex login failed: {status}");
    }

    let auth_path = auth_file_path(&native_root);
    if !auth_path.is_file() {
        anyhow::bail!(
            "native login succeeded but no credentials appeared at {}; retry the login",
            auth_path.display()
        );
    }
    // A native "not logged in" verdict after a zero exit means the credentials
    // did not take; anything else defers to exit status plus the store check.
    let status_spec = adapter().login_status_command(&program, &native_root);
    if let Ok(output) = ProcessRunner.run(&status_spec, &RunOptions::default()) {
        let combined = format!(
            "{} {}",
            String::from_utf8_lossy(&output.stdout),
            String::from_utf8_lossy(&output.stderr)
        );
        if output.status.success() && combined.to_lowercase().contains("not logged in") {
            anyhow::bail!(
                "native login reported success but `codex login status` says not logged in; retry the login"
            );
        }
    }

    let server_identity = match query_app_server_account_email(
        &program,
        &labeled_env(&native_root, &key),
        &native_root,
        "si",
        si_core::version::current_version(),
    ) {
        Ok(email) => email,
        Err(detail) => {
            eprintln!("warning: codex identity read failed ({detail}); using local hint");
            None
        }
    };
    let observed: Option<(String, bool, &'static str)> =
        server_identity.map(|email| (email, true, APP_SERVER_IDENTITY_SOURCE)).or_else(|| {
            local_email_from_auth_file(&auth_path).map(|email| (email, false, IDENTITY_UNVERIFIED))
        });

    let bound = entry.identity.as_deref().map(str::trim).filter(|value| !value.is_empty());
    let expected = entry.email.as_deref().map(str::trim).filter(|value| !value.is_empty());
    let matches =
        |want: &str| observed.as_ref().is_some_and(|(got, _, _)| got.eq_ignore_ascii_case(want));
    if let Some(bound) = bound {
        if !matches(bound) {
            let detail = match observed.as_ref() {
                Some((got, _, _)) => {
                    format!("login returned {got:?}, profile is bound to {bound:?}")
                }
                None => {
                    format!("could not confirm the login account for profile bound to {bound:?}")
                }
            };
            disable_profile(ctx, &key, &format!("identity mismatch: {detail}"))?;
            anyhow::bail!("{detail}; not retargeting profile {key:?} (launch disabled)");
        }
    }
    if let Some(expected) = expected {
        if !matches(expected) {
            let detail = match observed.as_ref() {
                Some((got, _, _)) => format!("login returned {got:?}, expected {expected:?}"),
                None => format!("could not confirm the login account, expected {expected:?}"),
            };
            disable_profile(ctx, &key, &format!("identity mismatch: {detail}"))?;
            anyhow::bail!("{detail} for profile {key:?} (launch disabled; credentials preserved)");
        }
    }

    let _settings_lock = Lease::acquire(&ctx.paths.settings_lock_path(), "codex profile login")?;
    let mut doc = SettingsDoc::load(&ctx.settings_path)?;
    let codex = ensure_toml_table(&mut doc.document, "codex")?;
    let profiles = ensure_toml_table(codex, "profiles")?;
    let entries = ensure_toml_table(profiles, "entries")?;
    let table = ensure_toml_table(entries, &key)?;
    set_toml_string(table, "native_root", Some(native_root.display().to_string()));
    match observed.as_ref() {
        Some((identity, _, source)) => {
            set_toml_string(table, "identity", Some(identity.clone()));
            set_toml_string(table, "identity_source", Some(source.to_string()));
        }
        None => {
            table.remove("identity");
            table.remove("identity_source");
        }
    }
    set_toml_string(table, "enrolled_at", Some(now_rfc3339()));
    table.remove("disabled");
    ctx.save_doc(&doc)?;
    ctx.reload()?;
    match observed.as_ref() {
        Some((identity, verified, _)) if *verified => {
            println!("codex profile {key:?} logged in as {identity}");
        }
        Some((identity, _, _)) => {
            println!("codex profile {key:?} logged in as {identity} (identity_unverified)");
        }
        None => println!("codex profile {key:?} logged in (identity_unverified)"),
    }
    profile_show(ctx, Some(&key), format)
}

/// Native logout scoped to the profile. Refuses while workers are active.
/// Metadata and the identity binding stay; only credentials go.
pub fn profile_logout(
    ctx: &mut AgentContext,
    profile: Option<&str>,
    codex_bin: Option<PathBuf>,
) -> Result<()> {
    let key = ctx.resolve_profile(PROVIDER, profile)?;
    let entry = ctx
        .settings
        .codex
        .profiles
        .entries
        .get(&key)
        .cloned()
        .ok_or_else(|| anyhow::anyhow!("codex profile {key:?} is not registered"))?;
    let native_root = canonical_native_home(&ctx.paths, &key, &entry)?;
    create_private_dir_all(&native_root)?;
    let _lease = acquire_management_lease(ctx, &key, "logout")?;
    refuse_if_workers_active(ctx, &key, "log out")?;

    let program = resolve_native_program(codex_bin, "codex")?;
    let spec = adapter()
        .logout_command(&program, &native_root)
        .env("SI_PROVIDER", PROVIDER.as_str())
        .env("SI_PROFILE", &key);
    let status = ProcessRunner
        .status_inherit(&spec)
        .with_context(|| format!("run {} logout", program.display()))?;

    let auth_path = auth_file_path(&native_root);
    if auth_path.is_file()
        && !matches!(
            adapter().qualified_status(&native_root),
            si_process::adapter::AuthStatus::NotLoggedIn
        )
    {
        anyhow::bail!(
            "codex logout did not clear credentials at {} (exit {status}); profile {key:?} unchanged",
            auth_path.display()
        );
    }
    if !status.success() {
        eprintln!("warning: codex logout exited {status}; credentials are cleared");
    }
    println!("codex profile {key:?} logged out; metadata preserved");
    Ok(())
}

/// Change only the future-launch default. Running workers are untouched.
pub fn profile_select(ctx: &mut AgentContext, id: &str) -> Result<()> {
    let key = ctx.resolve_profile(PROVIDER, Some(id))?;
    let _settings_lock = Lease::acquire(&ctx.paths.settings_lock_path(), "codex profile select")?;
    let mut doc = SettingsDoc::load(&ctx.settings_path)?;
    let codex = ensure_toml_table(&mut doc.document, "codex")?;
    let profiles = ensure_toml_table(codex, "profiles")?;
    set_toml_string(profiles, "active", Some(key.clone()));
    set_toml_string(codex, "profile", Some(key.clone()));
    ctx.save_doc(&doc)?;
    ctx.reload()?;
    println!("selected codex profile {key:?} for future launches");
    Ok(())
}

/// Unregister an idle profile. Native data and credentials stay on disk and
/// their path is printed; running workers block the removal.
pub fn profile_remove(ctx: &mut AgentContext, profile: Option<&str>) -> Result<()> {
    let key = ctx.resolve_profile(PROVIDER, profile)?;
    let entry = ctx.settings.codex.profiles.entries.get(&key).cloned();
    let preserved = match entry.as_ref() {
        Some(entry) => match canonical_native_home(&ctx.paths, &key, entry) {
            Ok(root) => root.display().to_string(),
            Err(_) => entry.auth_path.clone().unwrap_or_else(|| "(unknown location)".to_owned()),
        },
        None => "(unknown location)".to_owned(),
    };
    let _lease = acquire_management_lease(ctx, &key, "remove")?;
    refuse_if_workers_active(ctx, &key, "remove")?;

    let _settings_lock = Lease::acquire(&ctx.paths.settings_lock_path(), "codex profile remove")?;
    let mut doc = SettingsDoc::load(&ctx.settings_path)?;
    let codex = ensure_toml_table(&mut doc.document, "codex")?;
    if let Some(profiles) = codex.get_mut("profiles").and_then(toml::Value::as_table_mut) {
        if let Some(entries) = profiles.get_mut("entries").and_then(toml::Value::as_table_mut) {
            entries.remove(&key);
            if entries.is_empty() {
                profiles.remove("entries");
            }
        }
        if profiles.get("active").and_then(toml::Value::as_str) == Some(key.as_str()) {
            profiles.remove("active");
        }
        if profiles.is_empty() {
            codex.remove("profiles");
        }
    }
    if codex.get("profile").and_then(toml::Value::as_str) == Some(key.as_str()) {
        codex.remove("profile");
    }
    if codex.is_empty() {
        doc.document.remove("codex");
    }
    ctx.save_doc(&doc)?;
    ctx.reload()?;
    println!("removed codex profile {key:?}; native data preserved at {preserved}");
    Ok(())
}

fn disable_profile(ctx: &mut AgentContext, key: &str, reason: &str) -> Result<()> {
    let _settings_lock = Lease::acquire(&ctx.paths.settings_lock_path(), "codex profile disable")?;
    let mut doc = SettingsDoc::load(&ctx.settings_path)?;
    let codex = ensure_toml_table(&mut doc.document, "codex")?;
    let profiles = ensure_toml_table(codex, "profiles")?;
    let entries = ensure_toml_table(profiles, "entries")?;
    let table = ensure_toml_table(entries, key)?;
    set_toml_string(table, "disabled", Some(reason.to_owned()));
    ctx.save_doc(&doc)?;
    ctx.reload()?;
    Ok(())
}

/// Foreground native invocation with a tracked worker lease and inherited
/// terminal/exit status. Exits the process with the native exit code.
pub fn run(
    ctx: &AgentContext,
    profile: Option<&str>,
    codex_bin: Option<PathBuf>,
    argv: &[String],
) -> Result<()> {
    let key = ctx.resolve_profile(PROVIDER, profile)?;
    let entry = ctx
        .settings
        .codex
        .profiles
        .entries
        .get(&key)
        .cloned()
        .ok_or_else(|| anyhow::anyhow!("codex profile {key:?} is not registered"))?;
    if let Some(reason) = entry.disabled.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        anyhow::bail!("codex profile {key:?} is disabled: {reason}");
    }
    let native_root = canonical_native_home(&ctx.paths, &key, &entry)?;
    create_private_dir_all(&native_root)?;
    match Lease::acquire_or_busy(&ctx.paths.worker_lease_path(PROVIDER, &key), "codex run") {
        Ok(_lease) => run_with_lease(&key, &native_root, codex_bin, argv),
        Err(LeaseError::Busy { .. }) => {
            anyhow::bail!(
                "codex profile {key:?} already has an active credential-using worker; same-profile \
                 concurrency is disabled until native refresh safety is proven"
            )
        }
        Err(other) => Err(other.into()),
    }
}

fn run_with_lease(
    key: &str,
    native_root: &Path,
    codex_bin: Option<PathBuf>,
    argv: &[String],
) -> Result<()> {
    // NOTE: the caller holds the worker-admission lease across this call; the
    // flock releases on process exit even though `exit` skips destructors.
    let program = resolve_native_program(codex_bin, "codex")?;
    let workdir = std::env::current_dir().context("read current dir")?;
    let spec = adapter()
        .run_command(&program, native_root, &workdir, argv)
        .map_err(|detail| anyhow::anyhow!("{detail}"))?
        .env("SI_PROVIDER", PROVIDER.as_str())
        .env("SI_PROFILE", key);
    let status = ProcessRunner.status_inherit(&spec)?;
    std::process::exit(status.code().unwrap_or(1));
}
