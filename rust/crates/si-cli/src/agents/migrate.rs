//! Schema migration: legacy settings (schema 1) to schema 2.
//!
//! `si doctor --migration-plan` renders the dry-run; `si doctor --migrate`
//! applies it. Validation runs fully before any write, so a failed
//! migration leaves every metadata, auth, and history file intact. Native
//! session files move byte-identical into the canonical tree (proven
//! discoverable by `codex migrate-rollouts` on fixtures); SQLite databases,
//! credentials, and conflicting files are left read-only in place with an
//! exact report. Legacy worker records become stopped/legacy entries; SI
//! never claims an old default-socket pane as its own.

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use si_config::ids::{Provider, SlotId};
use si_config::io::{create_private_dir_all, write_private_file};
use si_config::migration::{MigrationPlan, ProfileDisposition, plan_migration};
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};

use super::runner::{self, WorkerRecord, WorkerState};
use super::store::{AgentContext, SettingsDoc};
use crate::{OutputFormat, ensure_toml_table, set_toml_string};

const PROVIDER: Provider = Provider::Codex;

/// Legacy worker record shape (pre-refocus `<root>/codex/workers/`).
#[derive(Clone, Debug, Deserialize)]
struct LegacyCodexWorkerState {
    #[serde(default)]
    profile_id: String,
    #[serde(default = "legacy_default_slot")]
    worker_slot: String,
    #[serde(default)]
    session_name: String,
    #[serde(default)]
    workspace: String,
    #[serde(default)]
    workdir: String,
    #[serde(default)]
    updated_at: String,
}

fn legacy_default_slot() -> String {
    "primary".to_owned()
}

/// Whether raw record bytes parse as a legacy worker record. Used by the
/// lifecycle to refuse pre-migration state with a migrate hint instead of
/// a corruption error or a silent overwrite.
pub(crate) fn is_legacy_record_bytes(raw: &[u8]) -> bool {
    serde_json::from_slice::<LegacyCodexWorkerState>(raw)
        .is_ok_and(|state| !state.profile_id.trim().is_empty())
}

/// One legacy worker record found during inventory.
#[derive(Clone, Debug, Serialize)]
pub struct LegacyWorkerEntry {
    pub path: String,
    pub profile: String,
    pub slot: String,
    pub session: String,
    pub workspace: String,
    pub target_record: String,
    pub target_exists: bool,
}

/// One legacy per-slot native home (`<canonical>/workers/<slot>/`).
#[derive(Clone, Debug, Serialize)]
pub struct SlotHistoryEntry {
    pub profile: String,
    pub slot: String,
    pub home: String,
    pub session_files: usize,
    pub has_index: bool,
    pub has_auth_fork: bool,
    pub has_sqlite: bool,
}

/// Full dry-run: profile plan plus worker/history inventory.
#[derive(Clone, Debug, Serialize)]
pub struct MigrationInventory {
    pub plan: MigrationPlan,
    pub legacy_workers: Vec<LegacyWorkerEntry>,
    pub slot_histories: Vec<SlotHistoryEntry>,
    pub active_blockers: Vec<String>,
}

/// Record candidates end in `.json` but are never transient `.spec.json`.
fn is_record_candidate(path: &Path) -> bool {
    let name = path.file_name().and_then(|name| name.to_str()).unwrap_or("");
    name.ends_with(".json") && !name.ends_with(".spec.json")
}

/// Scan legacy worker records (nested and flat pre-refocus layouts).
fn inventory_legacy_workers(ctx: &AgentContext) -> Result<Vec<LegacyWorkerEntry>> {
    let dir = ctx.paths.provider_dir(PROVIDER).join("workers");
    let mut entries = Vec::new();
    let mut files = Vec::new();
    if let Ok(top) = std::fs::read_dir(&dir) {
        for item in top.flatten() {
            let path = item.path();
            if path.is_file() && is_record_candidate(&path) {
                files.push(path);
            } else if path.is_dir() {
                if let Ok(slots) = std::fs::read_dir(&path) {
                    for slot in slots.flatten() {
                        let slot_path = slot.path();
                        if is_record_candidate(&slot_path) {
                            files.push(slot_path);
                        }
                    }
                }
            }
        }
    }
    files.sort();
    for path in files {
        let raw = match std::fs::read(&path) {
            Ok(raw) => raw,
            Err(_) => continue,
        };
        // New-shape records are not legacy; leave them alone.
        if serde_json::from_slice::<WorkerRecord>(&raw).is_ok() {
            continue;
        }
        let Ok(state) = serde_json::from_slice::<LegacyCodexWorkerState>(&raw) else {
            continue;
        };
        if state.profile_id.trim().is_empty() {
            continue;
        }
        let slot_name = si_codex::codex_worker_slot_name(Some(state.worker_slot.as_str()));
        let Ok(slot) = SlotId::parse(&slot_name) else {
            continue;
        };
        let target = ctx.paths.worker_record_path(PROVIDER, &state.profile_id, &slot);
        // Converted only when a new-shape record already owns the target; a
        // legacy file sitting at the same path still needs conversion.
        let target_exists = std::fs::read(&target)
            .is_ok_and(|raw| serde_json::from_slice::<WorkerRecord>(&raw).is_ok());
        entries.push(LegacyWorkerEntry {
            path: path.display().to_string(),
            profile: state.profile_id.clone(),
            slot: slot.as_str().to_owned(),
            session: state.session_name.clone(),
            workspace: state.workspace.clone(),
            target_record: target.display().to_string(),
            target_exists,
        });
    }
    Ok(entries)
}

/// Scan legacy per-slot native homes for migratable session data.
fn inventory_slot_histories(ctx: &AgentContext) -> Vec<SlotHistoryEntry> {
    let mut out = Vec::new();
    for (profile, entry) in &ctx.settings.codex.profiles.entries {
        let canonical = match super::codex::canonical_native_home(&ctx.paths, profile, entry) {
            Ok(root) => root,
            Err(_) => continue,
        };
        let slots_dir = canonical.join("workers");
        let Ok(slots) = std::fs::read_dir(&slots_dir) else {
            continue;
        };
        let mut homes: Vec<PathBuf> =
            slots.flatten().map(|item| item.path()).filter(|path| path.is_dir()).collect();
        homes.sort();
        for home in homes {
            let sessions = home.join(si_codex::adapter::CODEX_SESSIONS_DIR_NAME);
            let mut session_files = 0_usize;
            if sessions.is_dir() {
                let mut stack = vec![sessions];
                while let Some(dir) = stack.pop() {
                    if let Ok(items) = std::fs::read_dir(&dir) {
                        for item in items.flatten() {
                            let path = item.path();
                            if path.is_dir() {
                                stack.push(path);
                            } else {
                                session_files += 1;
                            }
                        }
                    }
                }
            }
            let slot = home.file_name().and_then(|name| name.to_str()).unwrap_or("?").to_owned();
            out.push(SlotHistoryEntry {
                profile: profile.clone(),
                slot,
                home: home.display().to_string(),
                session_files,
                has_index: home.join(si_codex::adapter::CODEX_SESSION_INDEX_FILE_NAME).is_file(),
                has_auth_fork: home.join(si_codex::adapter::CODEX_AUTH_FILE_NAME).is_file(),
                has_sqlite: has_sqlite_files(&home),
            });
        }
    }
    out
}

fn has_sqlite_files(dir: &Path) -> bool {
    let mut stack = vec![dir.to_path_buf()];
    let mut seen = 0_usize;
    while let Some(current) = stack.pop() {
        let Ok(items) = std::fs::read_dir(&current) else {
            continue;
        };
        for item in items.flatten() {
            seen += 1;
            if seen > 20_000 {
                return true;
            }
            let path = item.path();
            if path.is_dir() {
                stack.push(path);
            } else if path.extension().and_then(|ext| ext.to_str()).is_some_and(|ext| {
                ext.starts_with("sqlite") || ext == "db" || ext == "shm" || ext == "wal"
            }) {
                return true;
            }
        }
    }
    false
}

/// Old SI-managed workers on the default tmux server (`si-codex-pane-*`
/// sessions SI used to own). Read-only; never kills.
fn legacy_default_server_workers() -> Vec<String> {
    let output = std::process::Command::new("tmux").args(["ls", "-F", "#{session_name}"]).output();
    let Ok(output) = output else {
        return Vec::new();
    };
    if !output.status.success() {
        return Vec::new();
    }
    String::from_utf8_lossy(&output.stdout)
        .lines()
        .map(str::trim)
        .filter(|name| name.starts_with(si_codex::TMUX_SESSION_PREFIX))
        .map(str::to_owned)
        .collect()
}

/// Blockers that refuse `--migrate`: legacy default-server sessions plus
/// live workers on the owned socket.
fn migration_blockers(ctx: &AgentContext) -> Result<Vec<String>> {
    let mut blockers = Vec::new();
    for session in legacy_default_server_workers() {
        blockers.push(format!(
            "legacy worker session {session:?} is live on the default tmux server; stop it first with `tmux kill-session -t {session}` (SI will not kill it for you)"
        ));
    }
    let launch = super::codex::CodexLaunch;
    for row in runner::list_workers(&ctx.paths, &launch)? {
        if row.state == "running" || row.state == "reserved" {
            blockers.push(format!(
                "worker for {} profile {:?} slot {:?} is active; stop it first with `si codex stop --profile {} --slot {}`",
                row.profile, row.profile, row.slot, row.profile, row.slot
            ));
        }
    }
    Ok(blockers)
}

pub fn full_inventory(ctx: &AgentContext) -> Result<MigrationInventory> {
    Ok(MigrationInventory {
        plan: plan_migration(&ctx.settings, &ctx.paths),
        legacy_workers: inventory_legacy_workers(ctx)?,
        slot_histories: inventory_slot_histories(ctx),
        active_blockers: migration_blockers(ctx)?,
    })
}

pub fn render_migration_plan(ctx: &AgentContext, format: OutputFormat) -> Result<()> {
    let inventory = full_inventory(ctx)?;
    match format {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&inventory)?);
        }
        OutputFormat::Text => {
            println!("migration target schema: {}", inventory.plan.target_schema_version);
            println!("ready: {}", inventory.plan.ready);
            for profile in &inventory.plan.profiles {
                match &profile.disposition {
                    ProfileDisposition::KeepCanonicalHome { native_root } => {
                        println!(
                            "  profile {:?}: keep canonical home {}",
                            profile.profile,
                            native_root.display()
                        );
                    }
                    ProfileDisposition::NeedsEnrollment { reason } => {
                        println!("  profile {:?}: needs enrollment ({reason})", profile.profile);
                    }
                }
            }
            for error in &inventory.plan.errors {
                println!("  error: {}", serde_json::to_string(error)?);
            }
            for worker in &inventory.legacy_workers {
                println!(
                    "  legacy worker {} [{}] -> {}{}",
                    worker.profile,
                    worker.slot,
                    worker.target_record,
                    if worker.target_exists { " (already converted; skipped)" } else { "" }
                );
            }
            for history in &inventory.slot_histories {
                println!(
                    "  slot history {} [{}]: {} session files{}{}{}",
                    history.profile,
                    history.slot,
                    history.session_files,
                    if history.has_index { ", index" } else { "" },
                    if history.has_auth_fork { ", AUTH FORK (left untouched)" } else { "" },
                    if history.has_sqlite { ", sqlite (left in place)" } else { "" }
                );
            }
            for blocker in &inventory.active_blockers {
                println!("  blocker: {blocker}");
            }
        }
    }
    Ok(())
}

/// One migrated-or-skipped history file.
#[derive(Clone, Debug, Serialize)]
pub struct HistoryFileOutcome {
    pub source: String,
    pub dest: String,
    pub outcome: String,
    pub detail: Option<String>,
}

/// Per-profile history result.
#[derive(Clone, Debug, Serialize)]
pub struct HistoryMigrationOutcome {
    pub profile: String,
    pub slot: String,
    pub files: Vec<HistoryFileOutcome>,
    pub index_merged: usize,
    pub index_conflicts: Vec<String>,
    pub left_in_place: Vec<String>,
}

/// What `--migrate` changed.
#[derive(Clone, Debug, Serialize)]
pub struct MigrationReport {
    pub backup_dir: Option<String>,
    pub schema_version: u32,
    pub profiles_converted: Vec<String>,
    pub profiles_needing_enrollment: Vec<String>,
    pub workers_converted: Vec<String>,
    pub workers_skipped: Vec<String>,
    pub histories: Vec<HistoryMigrationOutcome>,
    pub notices: Vec<String>,
}

/// Apply the migration. Validation (plan errors, active workers) runs
/// before any write; a refusal changes nothing.
pub fn apply_migration(ctx: &mut AgentContext, format: OutputFormat) -> Result<()> {
    let inventory = full_inventory(ctx)?;
    if !inventory.plan.ready {
        anyhow::bail!("migration is not ready: {}", serde_json::to_string(&inventory.plan.errors)?);
    }
    if !inventory.active_blockers.is_empty() {
        anyhow::bail!(
            "refusing migration while workers run:\n  {}",
            inventory.active_blockers.join("\n  ")
        );
    }

    let schema_done =
        ctx.settings.schema_version >= si_config::migration::MIGRATION_TARGET_SCHEMA_VERSION;
    let workers_pending = inventory.legacy_workers.iter().any(|worker| !worker.target_exists);
    let histories_pending =
        inventory.slot_histories.iter().any(|history| slot_history_has_pending_work(ctx, history));
    if schema_done && !workers_pending && !histories_pending {
        return render_report(
            format,
            &MigrationReport {
                backup_dir: None,
                schema_version: ctx.settings.schema_version,
                profiles_converted: Vec::new(),
                profiles_needing_enrollment: Vec::new(),
                workers_converted: Vec::new(),
                workers_skipped: Vec::new(),
                histories: Vec::new(),
                notices: vec!["migration already applied; no changes".to_owned()],
            },
        );
    }

    // Backup first: full settings (obsolete sections retained here), legacy
    // worker records, and pre-migration session indexes. Never auth files.
    let backup = backup_migration_inputs(ctx, &inventory)?;
    let mut report = MigrationReport {
        backup_dir: Some(backup.display().to_string()),
        schema_version: si_config::migration::MIGRATION_TARGET_SCHEMA_VERSION,
        profiles_converted: Vec::new(),
        profiles_needing_enrollment: Vec::new(),
        workers_converted: Vec::new(),
        workers_skipped: Vec::new(),
        histories: Vec::new(),
        notices: Vec::new(),
    };

    // Settings: schema marker, auth_path -> native_root conversion. Needs-
    // enrollment entries keep their shape; use-time errors enroll them.
    {
        let _settings_lock = si_process::lease::Lease::acquire(
            &ctx.paths.settings_lock_path(),
            "migration settings rewrite",
        )?;
        let mut doc = SettingsDoc::load(&ctx.settings_path)?;
        doc.document.insert(
            "schema_version".to_owned(),
            toml::Value::Integer(si_config::migration::MIGRATION_TARGET_SCHEMA_VERSION as i64),
        );
        for profile in &inventory.plan.profiles {
            match &profile.disposition {
                ProfileDisposition::KeepCanonicalHome { native_root } => {
                    let codex = ensure_toml_table(&mut doc.document, "codex")?;
                    let profiles = ensure_toml_table(codex, "profiles")?;
                    let entries = ensure_toml_table(profiles, "entries")?;
                    if let Some(table) =
                        entries.get_mut(&profile.profile).and_then(toml::Value::as_table_mut)
                    {
                        set_toml_string(
                            table,
                            "native_root",
                            Some(native_root.display().to_string()),
                        );
                        table.remove("auth_path");
                    }
                    report.profiles_converted.push(profile.profile.clone());
                }
                ProfileDisposition::NeedsEnrollment { .. } => {
                    report.profiles_needing_enrollment.push(profile.profile.clone());
                }
            }
        }
        ctx.save_doc(&doc)?;
        ctx.reload()?;
    }

    // Worker records: same-profile histories first (they live under the
    // canonical homes the settings step just confirmed), then records.
    for history in &inventory.slot_histories {
        report.histories.push(migrate_slot_history(ctx, history)?);
    }
    for worker in &inventory.legacy_workers {
        if worker.target_exists {
            report.workers_skipped.push(format!("{} [{}]", worker.profile, worker.slot));
            continue;
        }
        convert_legacy_worker(ctx, worker)?;
        report.workers_converted.push(format!("{} [{}]", worker.profile, worker.slot));
    }

    report.notices.push(
        "future plain `codex` invocations still use their normal defaults; SI installs no PATH \
         interceptors, writes no shell startup files, and keeps no global active token"
            .to_owned(),
    );
    if !report.profiles_needing_enrollment.is_empty() {
        report.notices.push(
            "some profiles need native enrollment: run `si codex profile login <id>` for each"
                .to_owned(),
        );
    }
    render_report(format, &report)
}

fn render_report(format: OutputFormat, report: &MigrationReport) -> Result<()> {
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(report)?),
        OutputFormat::Text => {
            if let Some(backup) = report.backup_dir.as_deref() {
                println!("backup: {backup}");
            }
            println!("schema_version: {}", report.schema_version);
            for profile in &report.profiles_converted {
                println!("  converted profile {profile:?}");
            }
            for profile in &report.profiles_needing_enrollment {
                println!("  profile {profile:?} needs native enrollment");
            }
            for worker in &report.workers_converted {
                println!("  converted worker {worker}");
            }
            for worker in &report.workers_skipped {
                println!("  skipped worker {worker} (already converted)");
            }
            for history in &report.histories {
                println!(
                    "  history {} [{}]: {} files, {} index lines merged, {} conflicts",
                    history.profile,
                    history.slot,
                    history.files.len(),
                    history.index_merged,
                    history.index_conflicts.len()
                );
                for file in &history.files {
                    println!("    {} -> {}: {}", file.source, file.dest, file.outcome);
                }
                for conflict in &history.index_conflicts {
                    println!("    index conflict (kept canonical): {conflict}");
                }
                for left in &history.left_in_place {
                    println!("    left in place: {left}");
                }
            }
            for notice in &report.notices {
                println!("  note: {notice}");
            }
        }
    }
    Ok(())
}

/// Private metadata backup. Settings, legacy records, and pre-migration
/// indexes only; never credentials or full history trees.
fn backup_migration_inputs(ctx: &AgentContext, inventory: &MigrationInventory) -> Result<PathBuf> {
    let stamp = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|elapsed| elapsed.as_secs())
        .unwrap_or(0);
    let backup = ctx.paths.migration_backup_dir().join(format!("migration-{stamp}"));
    create_private_dir_all(&backup)?;
    if ctx.settings_path.is_file() {
        let raw = std::fs::read(&ctx.settings_path).context("read settings for backup")?;
        write_private_file(&backup.join("settings.toml"), &raw)?;
    }
    let workers_backup = backup.join("workers");
    for worker in &inventory.legacy_workers {
        if worker.target_exists {
            continue;
        }
        let source = Path::new(&worker.path);
        if let Ok(raw) = std::fs::read(source) {
            let name = format!("{}-{}.json", worker.profile, worker.slot);
            create_private_dir_all(&workers_backup)?;
            write_private_file(&workers_backup.join(name), &raw)?;
        }
    }
    let index_backup = backup.join("index");
    for history in &inventory.slot_histories {
        let canonical = canonical_home_for(ctx, &history.profile);
        let Some(canonical) = canonical else {
            continue;
        };
        let index = canonical.join(si_codex::adapter::CODEX_SESSION_INDEX_FILE_NAME);
        if index.is_file() {
            if let Ok(raw) = std::fs::read(&index) {
                create_private_dir_all(&index_backup)?;
                write_private_file(&index_backup.join(format!("{}.jsonl", history.profile)), &raw)?;
            }
        }
    }
    Ok(backup)
}

fn canonical_home_for(ctx: &AgentContext, profile: &str) -> Option<PathBuf> {
    let entry = ctx.settings.codex.profiles.entries.get(profile)?;
    super::codex::canonical_native_home(&ctx.paths, profile, entry).ok()
}

/// Convert one legacy record into a stopped/legacy provider-scoped record.
/// Never claims the old default-socket pane as an owned worker.
fn convert_legacy_worker(ctx: &AgentContext, worker: &LegacyWorkerEntry) -> Result<()> {
    let raw = std::fs::read(&worker.path)
        .with_context(|| format!("read legacy worker {}", worker.path))?;
    let state: LegacyCodexWorkerState =
        serde_json::from_slice(&raw).context("parse legacy worker record")?;
    let slot = SlotId::parse(&worker.slot).context("parse legacy worker slot")?;
    let native_root = canonical_home_for(ctx, &worker.profile)
        .unwrap_or_else(|| ctx.paths.default_native_root(PROVIDER, &worker.profile));
    let record = WorkerRecord {
        schema_version: runner::WORKER_RECORD_SCHEMA_VERSION,
        provider: PROVIDER.as_str().to_owned(),
        profile: worker.profile.clone(),
        slot: worker.slot.clone(),
        generation: format!("legacy-{}", state.updated_at),
        owner: "legacy".to_owned(),
        native_root,
        program: PathBuf::from("codex"),
        program_version: None,
        workspace: PathBuf::from(state.workspace.clone()),
        workdir: if state.workdir.trim().is_empty() {
            PathBuf::from(state.workspace.clone())
        } else {
            PathBuf::from(state.workdir.clone())
        },
        socket: ctx.paths.tmux_socket_path(),
        session_name: if state.session_name.trim().is_empty() {
            si_codex::codex_tmux_session_name_for_slot(&worker.profile, &worker.slot)
        } else {
            state.session_name.clone()
        },
        pane_id: None,
        native_pid: None,
        native_start_time: None,
        state: WorkerState::Legacy,
        exit_code: None,
        exit_reason: Some("migrated legacy entry; never ran on the owned socket".to_owned()),
        started_at: None,
        ended_at: None,
        contract: "legacy".to_owned(),
        resume_id: None,
        native_session_id: None,
    };
    let target = ctx.paths.worker_record_path(PROVIDER, &worker.profile, &slot);
    runner::save_record(&target, &record)?;
    // Flat legacy files (`workers/<profile>.json`) move aside: the backup
    // preserves them and the converted record replaces them.
    let source = Path::new(&worker.path);
    if source != target {
        std::fs::remove_file(source)
            .with_context(|| format!("remove converted legacy worker {}", worker.path))?;
    }
    Ok(())
}

/// Whether a slot history still has moves, dedupes, or index merges
/// pending. Pure conflicts and leftovers are settled: re-running apply
/// changes nothing for them, so they do not retrigger migration.
fn slot_history_has_pending_work(ctx: &AgentContext, history: &SlotHistoryEntry) -> bool {
    let Some(canonical) = canonical_home_for(ctx, &history.profile) else {
        return false;
    };
    let sessions = Path::new(&history.home).join(si_codex::adapter::CODEX_SESSIONS_DIR_NAME);
    if sessions.is_dir() {
        let mut stack = vec![sessions.clone()];
        while let Some(dir) = stack.pop() {
            let Ok(items) = std::fs::read_dir(&dir) else {
                continue;
            };
            for item in items.flatten() {
                let path = item.path();
                if path.is_dir() {
                    stack.push(path);
                    continue;
                }
                if !is_migratable_session_file(&path) {
                    continue;
                }
                let relative = path.strip_prefix(&sessions).unwrap_or(&path);
                let dest =
                    canonical.join(si_codex::adapter::CODEX_SESSIONS_DIR_NAME).join(relative);
                if !dest.is_file() {
                    return true;
                }
                let same =
                    std::fs::read(&path).is_ok_and(|a| std::fs::read(&dest).is_ok_and(|b| a == b));
                if same {
                    return true;
                }
            }
        }
    }
    // Unknown index IDs pending merge.
    let source_index =
        Path::new(&history.home).join(si_codex::adapter::CODEX_SESSION_INDEX_FILE_NAME);
    if source_index.is_file() {
        let dest_index = canonical.join(si_codex::adapter::CODEX_SESSION_INDEX_FILE_NAME);
        let dest_raw = std::fs::read_to_string(&dest_index).unwrap_or_default();
        let known: std::collections::BTreeSet<String> = dest_raw
            .lines()
            .filter_map(|line| {
                serde_json::from_str::<serde_json::Value>(line.trim())
                    .ok()?
                    .get("id")?
                    .as_str()
                    .map(str::to_owned)
            })
            .collect();
        if let Ok(source_raw) = std::fs::read_to_string(&source_index) {
            for line in source_raw.lines() {
                let line = line.trim();
                if line.is_empty() {
                    continue;
                }
                if let Ok(entry) = serde_json::from_str::<serde_json::Value>(line) {
                    if let Some(id) = entry.get("id").and_then(serde_json::Value::as_str) {
                        if !known.contains(id) {
                            return true;
                        }
                    }
                }
            }
        }
    }
    false
}

/// Move one slot's native session logs into the canonical session tree.
///
/// Same verified profile only (the slot home lives under the canonical
/// root). Files move byte-identical with relative paths preserved; the
/// native CLI discovers them at the canonical location (proven on
/// fixtures). Credentials, SQLite databases, config, and conflicting files
/// stay read-only in place with an exact report. Directories are never
/// deleted.
fn migrate_slot_history(
    ctx: &AgentContext,
    history: &SlotHistoryEntry,
) -> Result<HistoryMigrationOutcome> {
    let mut outcome = HistoryMigrationOutcome {
        profile: history.profile.clone(),
        slot: history.slot.clone(),
        files: Vec::new(),
        index_merged: 0,
        index_conflicts: Vec::new(),
        left_in_place: Vec::new(),
    };
    let canonical = canonical_home_for(ctx, &history.profile).ok_or_else(|| {
        anyhow::anyhow!("cannot resolve canonical home for profile {:?}", history.profile)
    })?;
    let home = Path::new(&history.home);
    // Refuse cross-profile moves: the slot home must live under this
    // profile's canonical root.
    if home != canonical.join("workers").join(&history.slot) {
        outcome.left_in_place.push(format!(
            "{}: not under the canonical workers dir; refusing cross-profile move",
            history.home
        ));
        return Ok(outcome);
    }

    // Session files: copy, verify bytes, then delete the source copy.
    let mut sources = Vec::new();
    let sessions = home.join(si_codex::adapter::CODEX_SESSIONS_DIR_NAME);
    if sessions.is_dir() {
        let mut stack = vec![sessions.clone()];
        while let Some(dir) = stack.pop() {
            let Ok(items) = std::fs::read_dir(&dir) else {
                continue;
            };
            for item in items.flatten() {
                let path = item.path();
                if path.is_dir() {
                    stack.push(path);
                } else {
                    sources.push(path);
                }
            }
        }
    }
    sources.sort();
    for source in sources {
        let relative = source.strip_prefix(&sessions).unwrap_or(&source);
        // Never move databases, credentials, or non-log files by suffix.
        let name = source.file_name().and_then(|name| name.to_str()).unwrap_or("");
        if !is_migratable_session_file(&source) {
            outcome
                .left_in_place
                .push(format!("{}: unsupported file type; left read-only", source.display()));
            outcome.files.push(HistoryFileOutcome {
                source: source.display().to_string(),
                dest: String::new(),
                outcome: "left_in_place".to_owned(),
                detail: Some(format!("{name} is not a supported native log file")),
            });
            continue;
        }
        let dest = canonical.join(si_codex::adapter::CODEX_SESSIONS_DIR_NAME).join(relative);
        if dest.is_file() {
            let same =
                std::fs::read(&source).is_ok_and(|a| std::fs::read(&dest).is_ok_and(|b| a == b));
            if same {
                std::fs::remove_file(&source)?;
                outcome.files.push(HistoryFileOutcome {
                    source: source.display().to_string(),
                    dest: dest.display().to_string(),
                    outcome: "duplicate_content_removed".to_owned(),
                    detail: None,
                });
            } else {
                outcome.left_in_place.push(format!(
                    "{}: same relative path with different bytes already exists; left read-only",
                    source.display()
                ));
                outcome.files.push(HistoryFileOutcome {
                    source: source.display().to_string(),
                    dest: dest.display().to_string(),
                    outcome: "conflict_left_in_place".to_owned(),
                    detail: Some("same path, different bytes".to_owned()),
                });
            }
            continue;
        }
        if let Some(parent) = dest.parent() {
            std::fs::create_dir_all(parent)
                .with_context(|| format!("create {}", parent.display()))?;
        }
        let bytes = std::fs::read(&source).with_context(|| format!("read {}", source.display()))?;
        std::fs::write(&dest, &bytes).with_context(|| format!("write {}", dest.display()))?;
        let check = std::fs::read(&dest).with_context(|| format!("verify {}", dest.display()))?;
        if check != bytes {
            std::fs::remove_file(&dest).ok();
            anyhow::bail!("verify failed for {}", dest.display());
        }
        std::fs::remove_file(&source)
            .with_context(|| format!("remove migrated {}", source.display()))?;
        outcome.files.push(HistoryFileOutcome {
            source: source.display().to_string(),
            dest: dest.display().to_string(),
            outcome: "migrated".to_owned(),
            detail: None,
        });
    }

    // Session index: append unknown IDs, keep canonical on conflict.
    let source_index = home.join(si_codex::adapter::CODEX_SESSION_INDEX_FILE_NAME);
    if source_index.is_file() {
        let dest_index = canonical.join(si_codex::adapter::CODEX_SESSION_INDEX_FILE_NAME);
        merge_session_index(&source_index, &dest_index, &mut outcome)?;
    }

    // Everything else in the slot home stays: credentials, databases,
    // config, and the directories themselves.
    for name in [si_codex::adapter::CODEX_AUTH_FILE_NAME, "config.toml"] {
        if home.join(name).is_file() {
            outcome.left_in_place.push(format!(
                "{}: {} left untouched in the slot home",
                home.join(name).display(),
                name
            ));
        }
    }
    if has_sqlite_files(home) {
        outcome.left_in_place.push(format!(
            "{}: native SQLite databases are not merged; left read-only",
            home.display()
        ));
    }
    Ok(outcome)
}

/// Only native rollout/event logs move. Databases, credentials, and
/// anything else stay where they are.
fn is_migratable_session_file(path: &Path) -> bool {
    let name = path.file_name().and_then(|name| name.to_str()).unwrap_or("");
    path.extension().and_then(|ext| ext.to_str()) == Some("jsonl") && name.starts_with("rollout-")
}

/// Append source index lines with unknown IDs to the canonical index.
/// Conflicting IDs keep the canonical line and are reported.
fn merge_session_index(
    source: &Path,
    dest: &Path,
    outcome: &mut HistoryMigrationOutcome,
) -> Result<()> {
    let source_raw =
        std::fs::read_to_string(source).with_context(|| format!("read {}", source.display()))?;
    let dest_raw = std::fs::read_to_string(dest).unwrap_or_default();
    let mut known: BTreeMap<String, String> = BTreeMap::new();
    for line in dest_raw.lines() {
        let line = line.trim();
        if line.is_empty() {
            continue;
        }
        if let Ok(entry) = serde_json::from_str::<serde_json::Value>(line) {
            if let Some(id) = entry.get("id").and_then(serde_json::Value::as_str) {
                known.insert(id.to_owned(), line.to_owned());
            }
        }
    }
    let mut merged = dest_raw;
    if !merged.is_empty() && !merged.ends_with('\n') {
        merged.push('\n');
    }
    for line in source_raw.lines() {
        let line = line.trim();
        if line.is_empty() {
            continue;
        }
        let Ok(entry) = serde_json::from_str::<serde_json::Value>(line) else {
            continue;
        };
        let Some(id) = entry.get("id").and_then(serde_json::Value::as_str) else {
            continue;
        };
        match known.get(id) {
            None => {
                known.insert(id.to_owned(), line.to_owned());
                merged.push_str(line);
                merged.push('\n');
                outcome.index_merged += 1;
            }
            Some(existing) if existing != line => {
                outcome
                    .index_conflicts
                    .push(format!("session {id}: canonical and slot lines differ; kept canonical"));
            }
            Some(_) => {}
        }
    }
    if outcome.index_merged > 0 {
        if let Some(parent) = dest.parent() {
            std::fs::create_dir_all(parent)
                .with_context(|| format!("create {}", parent.display()))?;
        }
        // Atomic replace via the private writer (0600 is fine for an index
        // SI already owns; native files keep their own modes otherwise).
        write_private_file(dest, merged.as_bytes())
            .with_context(|| format!("write merged {}", dest.display()))?;
    }
    Ok(())
}
