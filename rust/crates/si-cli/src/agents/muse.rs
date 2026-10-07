//! Muse account operations: profiles, login/logout, foreground run.
//!
//! One SI profile is one Muse account with its own canonical XDG tree
//! (`<root>/{config,data,cache,state}`, auth at `config/muse/auth.json`).
//! The native `Muse` CLI owns credentials and renewal; SI owns metadata,
//! child environment, and lifecycle records. There is no global auth swap,
//! no per-worker credential copy, no Fort coupling, and no forced unsafe mode.

use anyhow::{Context, Result};
use chrono::Utc;
use si_config::ids::{ProfileId, Provider};
use si_config::io::create_private_dir_all;
use si_config::paths::SiPaths;
use si_config::settings::MuseProfileEntry;
use si_muse::adapter::{
    MUSE_TESTED_VERSION, MuseAdapter, MuseVersionSupport, auth_file_path,
    ensure_muse_platform_supported, local_email_from_auth_file, muse_version_support,
};
use si_process::ProcessRunner;
use si_process::adapter::ProviderAdapter;
use si_process::lease::{Lease, LeaseError};
use std::path::{Path, PathBuf};

use super::runner::ProviderLaunch as _;
use super::store::{AgentContext, SettingsDoc};
use crate::{OutputFormat, ensure_toml_table, set_toml_string};

const PROVIDER: Provider = Provider::Muse;
const IDENTITY_UNVERIFIED: &str = "identity_unverified";

fn adapter() -> MuseAdapter {
    MuseAdapter
}

/// Canonical native root for a profile: stored `native_root`, else the
/// default profile directory. Muse has no legacy auth-path shape.
pub fn canonical_native_home(
    paths: &SiPaths,
    key: &str,
    entry: &MuseProfileEntry,
) -> Result<PathBuf> {
    if let Some(root) =
        entry.native_root.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        let path = PathBuf::from(root);
        return Ok(if path.is_absolute() { path } else { paths.root.join(path) });
    }
    si_config::paths::check_profile_key_for_paths(key)
        .map_err(|detail| anyhow::anyhow!("{detail}; rename the key in settings.toml"))?;
    Ok(paths.default_native_root(PROVIDER, key))
}

/// Gate every native Muse launch on the supported platform. Linux is the
/// initial supported multi-account platform; macOS fails the documented
/// isolation gate until Keychain account scoping is proven.
fn ensure_supported_platform() -> Result<()> {
    ensure_muse_platform_supported().map_err(|detail| anyhow::anyhow!("{detail}"))
}

/// One active credential-using worker of a profile.
pub struct ActiveWorker {
    pub slot: String,
    pub session: String,
}

/// Workers to stop before login/logout/remove: reconciled live records on
/// the owned socket, plus any holder of the worker-admission lease
/// (foreground `run` leaves no record).
pub fn active_workers(ctx: &AgentContext, key: &str) -> Result<Vec<ActiveWorker>> {
    let mut active = Vec::new();
    for row in super::runner::list_workers(&ctx.paths, &MuseLaunch)? {
        if row.profile != key {
            continue;
        }
        if row.state == "running" || row.state == "reserved" {
            active.push(ActiveWorker { slot: row.slot.clone(), session: row.session.clone() });
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
        format!("cannot {action} Muse profile {key:?} while credential-using workers are active:");
    for worker in &active {
        message.push_str(&format!("\n  slot {} ({})", worker.slot, worker.session));
    }
    message.push_str(&format!(
        "\nstop them first, e.g. `si muse stop --profile {key} --slot <slot>`; other profiles stay usable"
    ));
    anyhow::bail!(message);
}

fn acquire_management_lease(ctx: &AgentContext, key: &str, action: &str) -> Result<Lease> {
    match Lease::acquire_or_busy(&ctx.paths.profile_lock_path(PROVIDER, key), action) {
        Ok(lease) => Ok(lease),
        Err(LeaseError::Busy { .. }) => {
            anyhow::bail!(
                "muse profile {key:?} is busy with another login/logout/enrollment change; wait for it and retry"
            )
        }
        Err(other) => Err(other.into()),
    }
}

fn now_rfc3339() -> String {
    Utc::now().to_rfc3339_opts(chrono::SecondsFormat::Secs, true)
}

/// Create an empty profile: validated ID, empty XDG tree, metadata only.
/// Never imports, copies, or touches the default account.
pub fn profile_add(
    ctx: &mut AgentContext,
    id: &str,
    name: Option<String>,
    email: Option<String>,
    format: OutputFormat,
) -> Result<()> {
    let pid = ProfileId::parse(id)?;
    if ctx.settings.muse.profiles.entries.contains_key(pid.as_str()) {
        anyhow::bail!("muse profile {:?} already exists", pid.as_str());
    }
    let native_root = ctx.paths.default_native_root(PROVIDER, pid.as_str());
    create_private_dir_all(&native_root)?;
    for dir in ["config", "data", "cache", "state"] {
        create_private_dir_all(&native_root.join(dir))?;
    }

    let _settings_lock = Lease::acquire(&ctx.paths.settings_lock_path(), "muse profile add")?;
    let mut doc = SettingsDoc::load(&ctx.settings_path)?;
    if !doc.document.contains_key("schema_version") {
        doc.document.insert(
            "schema_version".to_owned(),
            toml::Value::Integer(si_config::migration::MIGRATION_TARGET_SCHEMA_VERSION as i64),
        );
    }
    let muse = ensure_toml_table(&mut doc.document, "muse")?;
    let profiles = ensure_toml_table(muse, "profiles")?;
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
        let entry = ctx.settings.muse.profiles.entries.get(&key);
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
        .ok_or_else(|| anyhow::anyhow!("muse profile {key:?} is not registered"))?;
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
    muse_bin: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    ensure_supported_platform()?;
    let key = ctx.resolve_profile(PROVIDER, profile)?;
    let entry = ctx
        .settings
        .muse
        .profiles
        .entries
        .get(&key)
        .cloned()
        .ok_or_else(|| anyhow::anyhow!("muse profile {key:?} is not registered"))?;
    let native_root = canonical_native_home(&ctx.paths, &key, &entry)?;
    create_private_dir_all(&native_root)?;
    let _lease = acquire_management_lease(ctx, &key, "login")?;
    refuse_if_workers_active(ctx, &key, "log in")?;

    let program = super::store::resolve_native_program(muse_bin, "muse")?;
    if let Some(version) = si_muse::adapter::probe_native_version(&program)
        && muse_version_support(&version) == MuseVersionSupport::Unverified
    {
        eprintln!(
            "warning: native Muse {version:?} is not the verified {MUSE_TESTED_VERSION:?}; \
             auth-path isolation was verified against the latter — re-run the destination \
             checks before trusting multi-account isolation on this native"
        );
    }
    let spec = adapter()
        .login_command(&program, &native_root)
        .env("SI_PROVIDER", PROVIDER.as_str())
        .env("SI_PROFILE", &key);
    let status = ProcessRunner.status_inherit(&spec)?;
    if !status.success() {
        anyhow::bail!("muse login failed: {status}");
    }

    // Destination validation: exit 0 plus a credential in the profile store.
    // 1.4.3 offers no read-only identity facility, so the binding below is
    // always a local hint until the live phase grounds it.
    let auth_path = auth_file_path(&native_root);
    if !matches!(
        adapter().qualified_status(&native_root),
        si_process::adapter::AuthStatus::CredentialPresentUnverified
    ) {
        anyhow::bail!(
            "native login succeeded but no credentials appeared at {}; retry the login",
            auth_path.display()
        );
    }

    let observed: Option<(String, bool, &'static str)> =
        local_email_from_auth_file(&auth_path).map(|email| (email, false, IDENTITY_UNVERIFIED));

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

    let _settings_lock = Lease::acquire(&ctx.paths.settings_lock_path(), "muse profile login")?;
    let mut doc = SettingsDoc::load(&ctx.settings_path)?;
    let muse = ensure_toml_table(&mut doc.document, "muse")?;
    let profiles = ensure_toml_table(muse, "profiles")?;
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
            println!("muse profile {key:?} logged in as {identity}");
        }
        Some((identity, _, _)) => {
            println!("muse profile {key:?} logged in as {identity} (identity_unverified)");
        }
        None => println!("muse profile {key:?} logged in (identity_unverified)"),
    }
    profile_show(ctx, Some(&key), format)
}

/// Native logout scoped to the profile. Refuses while workers are active.
/// Metadata and the identity binding stay; only credentials go.
pub fn profile_logout(
    ctx: &mut AgentContext,
    profile: Option<&str>,
    muse_bin: Option<PathBuf>,
) -> Result<()> {
    ensure_supported_platform()?;
    let key = ctx.resolve_profile(PROVIDER, profile)?;
    let entry = ctx
        .settings
        .muse
        .profiles
        .entries
        .get(&key)
        .cloned()
        .ok_or_else(|| anyhow::anyhow!("muse profile {key:?} is not registered"))?;
    let native_root = canonical_native_home(&ctx.paths, &key, &entry)?;
    create_private_dir_all(&native_root)?;
    let _lease = acquire_management_lease(ctx, &key, "logout")?;
    refuse_if_workers_active(ctx, &key, "log out")?;

    let program = super::store::resolve_native_program(muse_bin, "muse")?;
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
            "muse logout did not clear credentials at {} (exit {status}); profile {key:?} unchanged",
            auth_path.display()
        );
    }
    if !status.success() {
        eprintln!("warning: Muse logout exited {status}; credentials are cleared");
    }
    println!("muse profile {key:?} logged out; metadata preserved");
    Ok(())
}

/// Change only the future-launch default. Running workers are untouched.
pub fn profile_select(ctx: &mut AgentContext, id: &str) -> Result<()> {
    let key = ctx.resolve_profile(PROVIDER, Some(id))?;
    let _settings_lock = Lease::acquire(&ctx.paths.settings_lock_path(), "muse profile select")?;
    let mut doc = SettingsDoc::load(&ctx.settings_path)?;
    let muse = ensure_toml_table(&mut doc.document, "muse")?;
    let profiles = ensure_toml_table(muse, "profiles")?;
    set_toml_string(profiles, "active", Some(key.clone()));
    set_toml_string(muse, "profile", Some(key.clone()));
    ctx.save_doc(&doc)?;
    ctx.reload()?;
    println!("selected Muse profile {key:?} for future launches");
    Ok(())
}

/// Unregister an idle profile. Native data and credentials stay on disk and
/// their path is printed; running workers block the removal.
pub fn profile_remove(ctx: &mut AgentContext, profile: Option<&str>) -> Result<()> {
    let key = ctx.resolve_profile(PROVIDER, profile)?;
    let entry = ctx.settings.muse.profiles.entries.get(&key).cloned();
    let preserved = match entry.as_ref() {
        Some(entry) => match canonical_native_home(&ctx.paths, &key, entry) {
            Ok(root) => root.display().to_string(),
            Err(_) => entry.native_root.clone().unwrap_or_else(|| "(unknown location)".to_owned()),
        },
        None => "(unknown location)".to_owned(),
    };
    let _lease = acquire_management_lease(ctx, &key, "remove")?;
    refuse_if_workers_active(ctx, &key, "remove")?;

    let _settings_lock = Lease::acquire(&ctx.paths.settings_lock_path(), "muse profile remove")?;
    let mut doc = SettingsDoc::load(&ctx.settings_path)?;
    let muse = ensure_toml_table(&mut doc.document, "muse")?;
    if let Some(profiles) = muse.get_mut("profiles").and_then(toml::Value::as_table_mut) {
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
            muse.remove("profiles");
        }
    }
    if muse.get("profile").and_then(toml::Value::as_str) == Some(key.as_str()) {
        muse.remove("profile");
    }
    if muse.is_empty() {
        doc.document.remove("muse");
    }
    ctx.save_doc(&doc)?;
    ctx.reload()?;
    println!("removed Muse profile {key:?}; native data preserved at {preserved}");
    Ok(())
}

fn disable_profile(ctx: &mut AgentContext, key: &str, reason: &str) -> Result<()> {
    let _settings_lock = Lease::acquire(&ctx.paths.settings_lock_path(), "muse profile disable")?;
    let mut doc = SettingsDoc::load(&ctx.settings_path)?;
    let muse = ensure_toml_table(&mut doc.document, "muse")?;
    let profiles = ensure_toml_table(muse, "profiles")?;
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
    muse_bin: Option<PathBuf>,
    argv: &[String],
) -> Result<()> {
    ensure_supported_platform()?;
    let key = ctx.resolve_profile(PROVIDER, profile)?;
    let entry = ctx
        .settings
        .muse
        .profiles
        .entries
        .get(&key)
        .cloned()
        .ok_or_else(|| anyhow::anyhow!("muse profile {key:?} is not registered"))?;
    if let Some(reason) = entry.disabled.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        anyhow::bail!("muse profile {key:?} is disabled: {reason}");
    }
    let native_root = canonical_native_home(&ctx.paths, &key, &entry)?;
    create_private_dir_all(&native_root)?;
    match Lease::acquire_or_busy(&ctx.paths.worker_lease_path(PROVIDER, &key), "muse run") {
        Ok(_lease) => run_with_lease(&key, &native_root, muse_bin, argv),
        Err(LeaseError::Busy { .. }) => {
            anyhow::bail!(
                "muse profile {key:?} already has an active credential-using worker; same-profile \
                 concurrency is disabled until native refresh safety is proven"
            )
        }
        Err(other) => Err(other.into()),
    }
}

fn run_with_lease(
    key: &str,
    native_root: &Path,
    muse_bin: Option<PathBuf>,
    argv: &[String],
) -> Result<()> {
    // NOTE: the caller holds the worker-admission lease across this call; the
    // flock releases on process exit even though `exit` skips destructors.
    let program = super::store::resolve_native_program(muse_bin, "muse")?;
    let workdir = std::env::current_dir().context("read current dir")?;
    let spec = adapter()
        .run_command(&program, native_root, &workdir, argv)
        .map_err(|detail| anyhow::anyhow!("{detail}"))?
        .env("SI_PROVIDER", PROVIDER.as_str())
        .env("SI_PROFILE", key);
    let status = ProcessRunner.status_inherit(&spec)?;
    std::process::exit(status.code().unwrap_or(1));
}

/// Muse [`ProviderLaunch`]: canonical homes, file-store forcing, native
/// resume argv, session-index resume validation.
pub struct MuseLaunch;

impl super::runner::ProviderLaunch for MuseLaunch {
    fn provider(&self) -> Provider {
        PROVIDER
    }

    fn session_name(&self, profile: &str, slot: &si_config::ids::SlotId) -> String {
        si_muse::muse_tmux_session_name_for_slot(profile, slot.as_str())
    }

    fn window_name(
        &self,
        profile: &str,
        display: Option<&str>,
        slot: &si_config::ids::SlotId,
    ) -> String {
        crate::muse_tmux_window_name(profile, display, slot.as_str())
    }

    fn build_command(
        &self,
        program: &Path,
        native_root: &Path,
        workdir: &Path,
        argv: &[String],
    ) -> Result<si_process::CommandSpec, String> {
        adapter()
            .run_command(program, native_root, workdir, argv)
            .map_err(|detail| detail.to_string())
    }

    fn resume_argv(&self, session_id: &str) -> Vec<String> {
        si_muse::adapter::resume_argv(session_id)
    }

    fn session_known(&self, native_root: &Path, id: &str) -> bool {
        si_muse::adapter::session_known(native_root, id)
    }

    fn probe_version(&self, program: &Path) -> Option<String> {
        si_muse::adapter::probe_native_version(program)
    }
}

/// Normalize (trim/lowercase/default-primary) then strictly validate a slot.
pub fn resolve_slot(raw: Option<&str>) -> Result<si_config::ids::SlotId> {
    let normalized = si_muse::muse_worker_slot_name(raw);
    si_config::ids::SlotId::parse(&normalized)
        .map_err(|detail| anyhow::anyhow!("invalid slot: {detail}"))
}

/// Workspace: explicit flag, else configured default, else current dir.
/// Relative paths resolve against the current dir. Workspaces live outside
/// the SI root by design; only profile IDs are root-confined.
pub fn resolve_workspace(workspace: Option<PathBuf>, ctx: &AgentContext) -> Result<PathBuf> {
    let path = match workspace {
        Some(path) => path,
        None => match ctx.settings.muse.workspace.as_deref().map(str::trim) {
            Some(configured) if !configured.is_empty() => PathBuf::from(configured),
            _ => std::env::current_dir().context("read current dir for Muse workspace")?,
        },
    };
    let path = super::store::expand_home(path);
    if path.is_absolute() {
        Ok(path)
    } else {
        Ok(std::env::current_dir().context("read current dir for Muse workspace")?.join(path))
    }
}

fn resolve_launch_target(
    ctx: &AgentContext,
    profile: Option<&str>,
) -> Result<(String, MuseProfileEntry, PathBuf)> {
    let key = ctx.resolve_profile(PROVIDER, profile)?;
    let entry = ctx
        .settings
        .muse
        .profiles
        .entries
        .get(&key)
        .cloned()
        .ok_or_else(|| anyhow::anyhow!("muse profile {key:?} is not registered"))?;
    if let Some(reason) = entry.disabled.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        anyhow::bail!("muse profile {key:?} is disabled: {reason}");
    }
    let native_root = canonical_native_home(&ctx.paths, &key, &entry)?;
    create_private_dir_all(&native_root)?;
    Ok((key, entry, native_root))
}

#[allow(clippy::too_many_arguments)]
pub fn spawn(
    ctx: &AgentContext,
    profile: Option<&str>,
    slot: Option<&str>,
    workspace: Option<PathBuf>,
    workdir: Option<&str>,
    muse_bin: Option<PathBuf>,
    argv: Vec<String>,
    format: OutputFormat,
) -> Result<()> {
    ensure_supported_platform()?;
    let (key, entry, native_root) = resolve_launch_target(ctx, profile)?;
    let slot = resolve_slot(slot)?;
    let workspace = resolve_workspace(workspace, ctx)?;
    let workdir = super::store::resolve_workdir(workdir, &workspace)?;
    let program = super::store::resolve_native_program(muse_bin, "muse")?;
    let launch = MuseLaunch;
    let version = launch.probe_version(&program);
    let outcome = super::runner::launch_worker(super::runner::LaunchRequest {
        paths: &ctx.paths,
        launch: &launch,
        profile: &key,
        slot: &slot,
        display_name: entry.name.as_deref(),
        native_root: &native_root,
        program: &program,
        program_version: version,
        argv,
        workdir,
        workspace,
        resume_id: None,
    })?;
    match format {
        OutputFormat::Json => {
            let (state, session, pane, generation) = match outcome {
                super::runner::LaunchOutcome::Started { session, pane, generation } => {
                    ("started", session, pane, generation)
                }
                super::runner::LaunchOutcome::AlreadyRunning { session, pane, generation } => {
                    ("already_running", session, pane, generation)
                }
            };
            println!(
                "{}",
                serde_json::json!({
                    "profile": key,
                    "state": state,
                    "session": session,
                    "pane": pane,
                    "generation": generation,
                })
            );
        }
        OutputFormat::Text => match outcome {
            super::runner::LaunchOutcome::Started { session, pane, generation } => {
                println!(
                    "started Muse worker for profile {key:?} (session {session}, pane {pane}, generation {generation})"
                );
            }
            super::runner::LaunchOutcome::AlreadyRunning { session, pane, generation } => {
                println!(
                    "muse worker for profile {key:?} already running (session {session}, pane {pane}, generation {generation})"
                );
            }
        },
    }
    Ok(())
}

pub fn stop(
    ctx: &AgentContext,
    profile: Option<&str>,
    slot: Option<&str>,
    force: bool,
    format: OutputFormat,
) -> Result<()> {
    let (key, _, _) = resolve_launch_target(ctx, profile)?;
    let slot = resolve_slot(slot)?;
    let outcome = super::runner::stop_worker(&ctx.paths, &MuseLaunch, &key, &slot, force)?;
    match format {
        OutputFormat::Json => println!(
            "{}",
            serde_json::json!({
                "profile": key,
                "slot": slot.as_str(),
                "outcome": match outcome {
                    super::runner::StopOutcome::Stopped { exit } => serde_json::json!({"state": "stopped", "exit": exit}),
                    super::runner::StopOutcome::AlreadyExited { exit } => serde_json::json!({"state": "already_exited", "exit": exit}),
                },
            })
        ),
        OutputFormat::Text => match outcome {
            super::runner::StopOutcome::Stopped { exit } => {
                println!(
                    "stopped Muse worker for profile {key:?} slot {:?} (exit {})",
                    slot.as_str(),
                    exit.map_or("(unknown)".to_owned(), |code| code.to_string())
                );
            }
            super::runner::StopOutcome::AlreadyExited { exit } => {
                println!(
                    "muse worker for profile {key:?} slot {:?} already exited (exit {})",
                    slot.as_str(),
                    exit.map_or("(unknown)".to_owned(), |code| code.to_string())
                );
            }
        },
    }
    Ok(())
}

pub fn worker_remove(
    ctx: &AgentContext,
    profile: Option<&str>,
    slot: Option<&str>,
    stop_first: bool,
    format: OutputFormat,
) -> Result<()> {
    let (key, _, _) = resolve_launch_target(ctx, profile)?;
    let slot = resolve_slot(slot)?;
    let outcome = super::runner::remove_worker(&ctx.paths, &MuseLaunch, &key, &slot, stop_first)?;
    match format {
        OutputFormat::Json => println!(
            "{}",
            serde_json::json!({
                "profile": key,
                "slot": slot.as_str(),
                "outcome": match &outcome {
                    super::runner::RemoveOutcome::Removed { native_root, workspace } => serde_json::json!({
                        "state": "removed",
                        "native_root": native_root.display().to_string(),
                        "workspace": workspace.display().to_string(),
                    }),
                },
            })
        ),
        OutputFormat::Text => match outcome {
            super::runner::RemoveOutcome::Removed { native_root, workspace } => {
                println!(
                    "removed Muse worker for profile {key:?} slot {:?}; native data preserved at {}, workspace {} untouched",
                    slot.as_str(),
                    native_root.display(),
                    workspace.display()
                );
            }
        },
    }
    Ok(())
}

pub fn respawn(
    ctx: &AgentContext,
    profile: Option<&str>,
    slot: Option<&str>,
    muse_bin: Option<PathBuf>,
    resume_id: Option<String>,
    format: OutputFormat,
) -> Result<()> {
    ensure_supported_platform()?;
    let (key, entry, native_root) = resolve_launch_target(ctx, profile)?;
    let slot = resolve_slot(slot)?;
    let program = super::store::resolve_native_program(muse_bin, "muse")?;
    let launch = MuseLaunch;
    let version = launch.probe_version(&program);
    let outcome = super::runner::respawn_worker(super::runner::RespawnRequest {
        paths: &ctx.paths,
        launch: &launch,
        profile: &key,
        slot: &slot,
        display_name: entry.name.as_deref(),
        native_root: &native_root,
        program: &program,
        program_version: version,
        resume_id,
    })?;
    match format {
        OutputFormat::Json => {
            let (state, session, pane, generation) = match outcome {
                super::runner::LaunchOutcome::Started { session, pane, generation } => {
                    ("started", session, pane, generation)
                }
                super::runner::LaunchOutcome::AlreadyRunning { session, pane, generation } => {
                    ("already_running", session, pane, generation)
                }
            };
            println!(
                "{}",
                serde_json::json!({
                    "profile": key,
                    "slot": slot.as_str(),
                    "state": state,
                    "session": session,
                    "pane": pane,
                    "generation": generation,
                })
            );
        }
        OutputFormat::Text => match outcome {
            super::runner::LaunchOutcome::Started { session, pane, generation } => {
                println!(
                    "respawned Muse worker for profile {key:?} slot {:?} (session {session}, pane {pane}, generation {generation})",
                    slot.as_str()
                );
            }
            super::runner::LaunchOutcome::AlreadyRunning { session, pane, generation } => {
                println!(
                    "muse worker for profile {key:?} slot {:?} already running (session {session}, pane {pane}, generation {generation})",
                    slot.as_str()
                );
            }
        },
    }
    Ok(())
}

pub fn tail(
    ctx: &AgentContext,
    profile: Option<&str>,
    slot: Option<&str>,
    lines: usize,
) -> Result<()> {
    let (key, _, _) = resolve_launch_target(ctx, profile)?;
    let slot = resolve_slot(slot)?;
    print!("{}", super::runner::tail_worker(&ctx.paths, &MuseLaunch, &key, &slot, lines)?);
    Ok(())
}

pub fn attach(ctx: &AgentContext, profile: Option<&str>, slot: Option<&str>) -> Result<()> {
    let (key, _, _) = resolve_launch_target(ctx, profile)?;
    let slot = resolve_slot(slot)?;
    super::runner::attach_worker(&ctx.paths, &MuseLaunch, &key, &slot)
}

pub fn list(ctx: &AgentContext, format: OutputFormat) -> Result<()> {
    let rows = super::runner::list_workers(&ctx.paths, &MuseLaunch)?;
    match format {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&rows)?);
        }
        OutputFormat::Text => {
            let mut table = comfy_table::Table::new();
            table.set_header(["PROFILE", "SLOT", "STATE", "SESSION", "PID", "EXIT", "WORKSPACE"]);
            for row in &rows {
                let pid = row.pid.map_or("-".to_owned(), |pid| pid.to_string());
                let exit = row.exit_code.map_or("-".to_owned(), |code| code.to_string());
                table.add_row([
                    row.profile.as_str(),
                    row.slot.as_str(),
                    row.state.as_str(),
                    row.session.as_str(),
                    pid.as_str(),
                    exit.as_str(),
                    row.workspace.as_str(),
                ]);
            }
            println!("{table}");
            for row in rows.iter().filter(|row| row.conflict.is_some()) {
                eprintln!(
                    "warning: {} [{}]: {}",
                    row.profile,
                    row.slot,
                    row.conflict.as_deref().unwrap_or("conflict")
                );
            }
        }
    }
    Ok(())
}

/// Stop every worker of the provider. Continues past per-worker errors
/// and reports them all; exits nonzero when anything failed.
pub fn stop_all(ctx: &AgentContext, force: bool, format: OutputFormat) -> Result<()> {
    let rows = super::runner::list_workers(&ctx.paths, &MuseLaunch)?;
    let live: Vec<_> =
        rows.iter().filter(|row| row.state == "running" || row.state == "reserved").collect();
    if live.is_empty() {
        match format {
            OutputFormat::Json => println!("{}", serde_json::json!([])),
            OutputFormat::Text => println!("no running Muse workers"),
        }
        return Ok(());
    }
    let mut outcomes = Vec::new();
    let mut failed = false;
    for row in live {
        let outcome = super::runner::stop_worker(
            &ctx.paths,
            &MuseLaunch,
            &row.profile,
            &resolve_slot(Some(&row.slot))?,
            force,
        );
        match (&outcome, format) {
            (Ok(super::runner::StopOutcome::Stopped { exit }), OutputFormat::Text) => {
                println!(
                    "stopped {} [{}] (exit {})",
                    row.profile,
                    row.slot,
                    exit.map_or("(unknown)".to_owned(), |code| code.to_string())
                );
            }
            (Ok(super::runner::StopOutcome::AlreadyExited { .. }), OutputFormat::Text) => {
                println!("{} [{}] already exited", row.profile, row.slot);
            }
            (Err(source), OutputFormat::Text) => {
                failed = true;
                eprintln!("warning: stop {} [{}] failed: {source:#}", row.profile, row.slot);
            }
            _ => {}
        }
        outcomes.push(serde_json::json!({
            "profile": row.profile,
            "slot": row.slot,
            "result": match &outcome {
                Ok(super::runner::StopOutcome::Stopped { exit }) => serde_json::json!({"state": "stopped", "exit": exit}),
                Ok(super::runner::StopOutcome::AlreadyExited { exit }) => serde_json::json!({"state": "already_exited", "exit": exit}),
                Err(source) => {
                    failed = true;
                    serde_json::json!({"state": "error", "detail": format!("{source:#}")})
                }
            },
        }));
    }
    if matches!(format, OutputFormat::Json) {
        println!("{}", serde_json::to_string_pretty(&outcomes)?);
    }
    if failed {
        anyhow::bail!("one or more workers failed to stop");
    }
    Ok(())
}

/// Remove every worker record after an explicit typed confirmation.
/// Active workers are removed only with `stop_first`.
pub fn remove_all(ctx: &AgentContext, stop_first: bool, format: OutputFormat) -> Result<()> {
    let rows = super::runner::list_workers(&ctx.paths, &MuseLaunch)?;
    if rows.is_empty() {
        match format {
            OutputFormat::Json => println!("{}", serde_json::json!([])),
            OutputFormat::Text => println!("no Muse worker sessions"),
        }
        return Ok(());
    }
    eprintln!("Remove all {} Muse worker sessions?", rows.len());
    for row in &rows {
        eprintln!("  - {} [{}] ({})", row.profile, row.slot, row.session);
    }
    eprint!("Type 'remove all' to confirm: ");
    use std::io::Write as _;
    std::io::stderr().flush()?;
    let mut input = String::new();
    std::io::stdin().read_line(&mut input)?;
    if input.trim() != "remove all" {
        anyhow::bail!("remove --all not confirmed");
    }
    let mut outcomes = Vec::new();
    let mut failed = false;
    for row in &rows {
        let outcome = super::runner::remove_worker(
            &ctx.paths,
            &MuseLaunch,
            &row.profile,
            &resolve_slot(Some(&row.slot))?,
            stop_first,
        );
        match (&outcome, format) {
            (Ok(_), OutputFormat::Text) => {
                println!("removed {} [{}]", row.profile, row.slot);
            }
            (Err(source), OutputFormat::Text) => {
                failed = true;
                eprintln!("warning: remove {} [{}] failed: {source:#}", row.profile, row.slot);
            }
            _ => {}
        }
        outcomes.push(serde_json::json!({
            "profile": row.profile,
            "slot": row.slot,
            "result": match &outcome {
                Ok(_) => serde_json::json!({"state": "removed"}),
                Err(source) => {
                    failed = true;
                    serde_json::json!({"state": "error", "detail": format!("{source:#}")})
                }
            },
        }));
    }
    if matches!(format, OutputFormat::Json) {
        println!("{}", serde_json::to_string_pretty(&outcomes)?);
    }
    if failed {
        anyhow::bail!("one or more workers failed to remove");
    }
    Ok(())
}

/// Diagnostic/helper command with the profile's controlled environment.
///
/// Runs the command directly (never through a login shell, so shell startup
/// files cannot silently overwrite profile paths) and labels the profile on
/// stderr. Credential-using helpers belong in `run`, not here.
pub fn shell(
    ctx: &AgentContext,
    profile: Option<&str>,
    slot: Option<&str>,
    workspace: Option<PathBuf>,
    workdir: Option<&str>,
    command: Vec<String>,
) -> Result<()> {
    if command.is_empty() {
        anyhow::bail!("shell command is required");
    }
    let (key, _, native_root) = resolve_launch_target(ctx, profile)?;
    let slot = resolve_slot(slot)?;
    let workspace = resolve_workspace(workspace, ctx)?;
    let workdir = super::store::resolve_workdir(workdir, &workspace)?;
    eprintln!(
        "[Muse profile {key:?} slot {:?}] native root {}",
        slot.as_str(),
        native_root.display()
    );
    let child_env = adapter().profile_env(&native_root);
    let mut process = std::process::Command::new(&command[0]);
    process
        .args(&command[1..])
        .current_dir(&workdir)
        .env("HOME", &ctx.home)
        .env("SI_PROVIDER", PROVIDER.as_str())
        .env("SI_PROFILE", &key)
        .env("SI_WORKER_SLOT", slot.as_str())
        .env("TERM", std::env::var("TERM").unwrap_or_else(|_| "xterm-256color".to_owned()));
    for (key, value) in &child_env.set {
        process.env(key, value);
    }
    for key in &child_env.unset {
        process.env_remove(key);
    }
    let status = process
        .stdin(std::process::Stdio::inherit())
        .stdout(std::process::Stdio::inherit())
        .stderr(std::process::Stdio::inherit())
        .status()
        .with_context(|| format!("run {}", command[0]))?;
    if !status.success() {
        anyhow::bail!("command failed: {status}");
    }
    Ok(())
}
