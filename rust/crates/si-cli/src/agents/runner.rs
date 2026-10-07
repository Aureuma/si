//! Owned tmux workers: reservation, runner launch, lifecycle operations.
//!
//! Provider-generic over [`ProviderLaunch`]; Codex implements it in
//! [`crate::agents::codex`], Muse follows in C9. Every command carries the
//! SI-owned socket (`<root>/runtime/tmux.sock`) via `tmux -S`; the user's
//! normal tmux server is never touched.
//!
//! Launch protocol: reserve under the slot lock, start a detached session
//! running `si __worker-run --spec <spec>`, then commit the running record
//! only after the runner confirms native startup. The runner holds the
//! profile worker lease for the whole native lifetime, records the real exit
//! status, and releases the lease on exit. Failures roll back the
//! reservation and the newly created session only.

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use si_config::ids::{Provider, SlotId};
use si_config::io::{create_private_dir_all, write_private_file};
use si_config::paths::SiPaths;
use si_process::adapter::validate_env_name;
use si_process::lease::{Lease, LeaseError};
use si_process::{CommandSpec, ProcessRunner};
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

/// Hidden runner entry point: `si __worker-run --spec <path>`.
pub const RUNNER_SUBCOMMAND: &str = "__worker-run";

/// Worker record/spec schema version (lifecycle format, not SI version).
pub const WORKER_RECORD_SCHEMA_VERSION: u32 = 1;

/// tmux user options carrying ownership. Verified before every action.
pub const OPT_OWNER: &str = "@si_owner";
pub const OPT_GENERATION: &str = "@si_generation";
pub const OPT_PROVIDER: &str = "@si_provider";
pub const OPT_PROFILE: &str = "@si_profile";
pub const OPT_SLOT: &str = "@si_slot";

/// How long spawn waits for the runner to confirm native startup.
const SPAWN_CONFIRM_TIMEOUT: Duration = Duration::from_secs(15);
/// Grace period for `stop` before reporting still-running (no `--force`).
const STOP_GRACE_PERIOD: Duration = Duration::from_secs(5);
/// Extra wait after SIGINT to the native PID under `--force`.
const STOP_FORCE_PID_WAIT: Duration = Duration::from_secs(2);

/// Provider-specific launch behavior. Implemented once per provider.
pub trait ProviderLaunch {
    fn provider(&self) -> Provider;
    /// tmux session name for a worker.
    fn session_name(&self, profile: &str, slot: &SlotId) -> String;
    /// tmux window label (non-secret identity display).
    fn window_name(&self, profile: &str, display: Option<&str>, slot: &SlotId) -> String;
    /// Full native [`CommandSpec`] (store forcing, env, workdir).
    fn build_command(
        &self,
        program: &Path,
        native_root: &Path,
        workdir: &Path,
        argv: &[String],
    ) -> Result<CommandSpec, String>;
    /// Default native argv when spawn carries none (bare interactive native).
    fn default_argv(&self) -> Vec<String> {
        Vec::new()
    }
    /// Managed-resume argv for a validated native session ID.
    fn resume_argv(&self, session_id: &str) -> Vec<String>;
    /// Whether `id` names a session known in this profile's native store.
    fn session_known(&self, native_root: &Path, id: &str) -> bool;
    /// Best-effort native version string for the record.
    fn probe_version(&self, program: &Path) -> Option<String>;
}

/// Private launch specification. Written 0600 at reservation, read once by
/// the runner. Carries non-secret argv only (credential switches are
/// rejected before the spec is written).
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct WorkerSpec {
    pub schema_version: u32,
    pub provider: String,
    pub profile: String,
    pub slot: String,
    pub si_root: PathBuf,
    pub native_root: PathBuf,
    pub program: PathBuf,
    pub program_version: Option<String>,
    pub argv: Vec<String>,
    pub env_set: BTreeMap<String, String>,
    pub env_unset: Vec<String>,
    pub workdir: PathBuf,
    pub workspace: PathBuf,
    pub socket: PathBuf,
    pub session_name: String,
    pub owner: String,
    pub generation: String,
    pub record_path: PathBuf,
    pub lease_path: PathBuf,
    pub resume_id: Option<String>,
    pub contract: String,
}

/// Lifecycle state of one worker slot.
#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum WorkerState {
    /// Reserved under the slot lock; runner has not confirmed startup yet.
    Reserved,
    /// Runner confirmed the native child is alive.
    Running,
    /// Native exited (code recorded); pane kept via remain-on-exit.
    Exited,
    /// Stopped through `stop`/`remove --stop`.
    Stopped,
    /// Launch failed or was interrupted; nothing live.
    Failed,
    /// Converted legacy entry; never owned by the dedicated socket.
    Legacy,
}

impl WorkerState {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Reserved => "reserved",
            Self::Running => "running",
            Self::Exited => "exited",
            Self::Stopped => "stopped",
            Self::Failed => "failed",
            Self::Legacy => "legacy",
        }
    }

    /// States in which the slot may still own a live tmux object.
    pub fn maybe_live(self) -> bool {
        matches!(self, Self::Reserved | Self::Running)
    }
}

/// Durable worker record: `<provider>/workers/<profile>/<slot>.json`.
///
/// No complete process environment, no token-bearing arguments.
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct WorkerRecord {
    pub schema_version: u32,
    pub provider: String,
    pub profile: String,
    pub slot: String,
    pub generation: String,
    pub owner: String,
    pub native_root: PathBuf,
    pub program: PathBuf,
    pub program_version: Option<String>,
    pub workspace: PathBuf,
    pub workdir: PathBuf,
    pub socket: PathBuf,
    pub session_name: String,
    pub pane_id: Option<String>,
    pub native_pid: Option<u32>,
    pub native_start_time: Option<u64>,
    pub state: WorkerState,
    pub exit_code: Option<i32>,
    pub exit_reason: Option<String>,
    pub started_at: Option<String>,
    pub ended_at: Option<String>,
    pub contract: String,
    pub resume_id: Option<String>,
    pub native_session_id: Option<String>,
}

/// Unique run generation: `<pid>-<unix-nanos>`. The owner token equals the
/// generation; both are verified against the tmux labels before acting.
pub fn new_generation() -> String {
    let nanos =
        SystemTime::now().duration_since(UNIX_EPOCH).map(|elapsed| elapsed.as_nanos()).unwrap_or(0);
    format!("{}-{nanos}", std::process::id())
}

/// Canonical launch contract for spawn idempotency. Same bytes in, same
/// worker; anything else is a conflicting launch, never a silent replace.
/// `argv` is pre-forcing user argv (respawn reuses it verbatim).
pub fn launch_contract(
    program: &Path,
    argv: &[String],
    workdir: &Path,
    workspace: &Path,
    native_root: &Path,
    resume_id: Option<&str>,
) -> String {
    serde_json::json!({
        "program": program.display().to_string(),
        "argv": argv,
        "workdir": workdir.display().to_string(),
        "workspace": workspace.display().to_string(),
        "native_root": native_root.display().to_string(),
        "resume_id": resume_id,
    })
    .to_string()
}

/// POSIX single-quote one argv element for the tmux shell-string command.
pub fn shell_quote(arg: &str) -> String {
    let mut rebuilt = String::with_capacity(arg.len() + 2);
    rebuilt.push('\'');
    let mut first = true;
    for chunk in arg.split('\'') {
        if !first {
            rebuilt.push_str("'\\''");
        }
        first = false;
        rebuilt.push_str(chunk);
    }
    rebuilt.push('\'');
    rebuilt
}

/// Live pane observation. `dead_status` is the retained exit code of a dead
/// pane (remain-on-exit); `None` while the pane runs.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PaneInfo {
    pub pane_id: String,
    pub dead: bool,
    pub dead_status: Option<i32>,
}

/// Minimal tmux client bound to one owned socket. Every invocation carries
/// `-S <socket>`; the default server is never addressed.
#[derive(Clone, Debug)]
pub struct Tmux {
    socket: PathBuf,
    term: String,
    program: PathBuf,
}

impl Tmux {
    pub fn new(socket: PathBuf) -> Self {
        let term = std::env::var("TERM").unwrap_or_else(|_| "xterm-256color".to_owned());
        Self { socket, term, program: PathBuf::from("tmux") }
    }

    fn command(&self) -> Command {
        let mut command = Command::new(&self.program);
        command.arg("-S").arg(&self.socket).env("TERM", &self.term);
        command
    }

    fn run_capture(&self, args: &[&str]) -> Result<std::process::Output> {
        self.command().args(args).output().with_context(|| format!("run tmux {}", args.join(" ")))
    }

    /// Create a detached session running `shell_command` via the pane shell.
    /// Uses a fixed 200x50 size so headless servers render deterministically.
    pub fn new_session(&self, session: &str, window: &str, shell_command: &str) -> Result<()> {
        let output = self.run_capture(&[
            "new-session",
            "-d",
            "-s",
            session,
            "-n",
            window,
            "-x",
            "200",
            "-y",
            "50",
            shell_command,
        ])?;
        if !output.status.success() {
            anyhow::bail!(
                "tmux new-session {session} failed: {}",
                String::from_utf8_lossy(&output.stderr).trim()
            );
        }
        // Failures must stay inspectable; never silently start a fresh shell.
        let output = self.run_capture(&["set-option", "-t", session, "remain-on-exit", "on"])?;
        if !output.status.success() {
            anyhow::bail!(
                "tmux remain-on-exit for {session} failed: {}",
                String::from_utf8_lossy(&output.stderr).trim()
            );
        }
        Ok(())
    }

    pub fn set_option(&self, session: &str, key: &str, value: &str) -> Result<()> {
        let output = self.run_capture(&["set-option", "-t", session, key, value])?;
        if !output.status.success() {
            anyhow::bail!(
                "tmux set-option {key} failed: {}",
                String::from_utf8_lossy(&output.stderr).trim()
            );
        }
        Ok(())
    }

    /// Read a session user option. Missing sessions/options yield `None`
    /// rather than an error so callers can report conflicts precisely.
    pub fn show_option(&self, session: &str, key: &str) -> Option<String> {
        let output =
            self.command().args(["show-options", "-v", "-t", session, key]).output().ok()?;
        if !output.status.success() {
            return None;
        }
        Some(String::from_utf8_lossy(&output.stdout).trim().to_owned())
    }

    /// Whether the session exists on this socket (any owner).
    pub fn session_exists(&self, session: &str) -> bool {
        self.command()
            .args(["has-session", "-t", session])
            .output()
            .map(|output| output.status.success())
            .unwrap_or(false)
    }

    /// First pane of the session with liveness + retained exit status.
    pub fn pane_info(&self, session: &str) -> Option<PaneInfo> {
        let output = self
            .command()
            .args([
                "list-panes",
                "-t",
                session,
                "-F",
                "#{pane_id} #{pane_dead} #{pane_dead_status}",
            ])
            .output()
            .ok()?;
        if !output.status.success() {
            return None;
        }
        let line = String::from_utf8_lossy(&output.stdout).lines().next()?.to_owned();
        parse_pane_line(&line)
    }

    pub fn send_keys(&self, target: &str, keys: &[&str]) -> Result<()> {
        let mut args = vec!["send-keys", "-t", target];
        args.extend(keys.iter().copied());
        let output = self.run_capture(&args)?;
        if !output.status.success() {
            anyhow::bail!(
                "tmux send-keys to {target} failed: {}",
                String::from_utf8_lossy(&output.stderr).trim()
            );
        }
        Ok(())
    }

    /// Last `lines` lines of the pane (works on dead panes under
    /// remain-on-exit, so failures stay inspectable).
    pub fn capture_pane(&self, target: &str, lines: usize) -> Result<String> {
        let start = format!("-{}", lines.max(1));
        let output = self.run_capture(&["capture-pane", "-J", "-p", "-t", target, "-S", &start])?;
        if !output.status.success() {
            anyhow::bail!(
                "tmux capture-pane {target} failed: {}",
                String::from_utf8_lossy(&output.stderr).trim()
            );
        }
        Ok(String::from_utf8_lossy(&output.stdout).into_owned())
    }

    /// Kill an ownership-verified session. Callers verify first; this helper
    /// never guesses which session is ours.
    pub fn kill_session(&self, session: &str) -> Result<()> {
        let output = self.run_capture(&["kill-session", "-t", session])?;
        if !output.status.success() {
            anyhow::bail!(
                "tmux kill-session {session} failed: {}",
                String::from_utf8_lossy(&output.stderr).trim()
            );
        }
        Ok(())
    }

    /// Replace this process with an interactive attach. Never returns on
    /// success. Callers drop locks before calling: attach holds no lease.
    #[cfg(unix)]
    pub fn attach_exec(&self, session: &str) -> std::io::Error {
        use std::os::unix::process::CommandExt;
        self.command().args(["attach-session", "-t", session]).exec()
    }
}

/// Parse one `list-panes -F '#{pane_id} #{pane_dead} #{pane_dead_status}'`
/// line. Live panes expand the status empty, so the third field exists
/// only for dead panes under remain-on-exit.
fn parse_pane_line(line: &str) -> Option<PaneInfo> {
    let mut parts = line.split_whitespace();
    let pane_id = parts.next()?.to_owned();
    let dead = parts.next()? == "1";
    let dead_status = parts.next().and_then(|field| field.parse::<i32>().ok()).filter(|_| dead);
    Some(PaneInfo { pane_id, dead, dead_status })
}

/// Verify socket liveness, session presence, and all five ownership labels.
/// Returns the live pane info. Anything else is a precise conflict error;
/// an unrelated session with a matching name is never acted on.
pub fn verify_owned_session(tmux: &Tmux, record: &WorkerRecord) -> Result<PaneInfo> {
    if !tmux.session_exists(&record.session_name) {
        anyhow::bail!(
            "tmux session {} for {} profile {:?} slot {:?} is gone; run `tail` for retained output or `respawn` to restart",
            record.session_name,
            record.provider,
            record.profile,
            record.slot
        );
    }
    let labels = [
        (OPT_OWNER, &record.owner),
        (OPT_GENERATION, &record.generation),
        (OPT_SLOT, &record.slot),
    ];
    for (key, expected) in labels {
        match tmux.show_option(&record.session_name, key) {
            Some(actual) if &actual == expected => {}
            Some(actual) => anyhow::bail!(
                "tmux session {} is not owned by this worker ({key} is {actual:?}, want {expected:?}); refusing to touch a foreign session",
                record.session_name
            ),
            None => anyhow::bail!(
                "tmux session {} carries no SI ownership label {key}; refusing to touch a foreign session",
                record.session_name
            ),
        }
    }
    for (key, expected) in [(OPT_PROVIDER, &record.provider), (OPT_PROFILE, &record.profile)] {
        match tmux.show_option(&record.session_name, key) {
            Some(actual) if &actual == expected => {}
            _ => anyhow::bail!(
                "tmux session {} is not owned by this worker; refusing to touch a foreign session",
                record.session_name
            ),
        }
    }
    tmux.pane_info(&record.session_name)
        .ok_or_else(|| anyhow::anyhow!("tmux session {} has no readable pane", record.session_name))
}

/// Load a worker record, if present. Legacy pre-migration records refuse
/// with a migrate hint (never a silent overwrite); corrupt records are an
/// explicit error, never silently ignored.
pub fn load_record(path: &Path) -> Result<Option<WorkerRecord>> {
    match std::fs::read(path) {
        Err(source) if source.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(source) => Err(anyhow::anyhow!("read {}: {source}", path.display())),
        Ok(raw) => match serde_json::from_slice::<WorkerRecord>(&raw) {
            Ok(record) => Ok(Some(record)),
            Err(_) if super::migrate::is_legacy_record_bytes(&raw) => {
                anyhow::bail!(
                    "worker record {} is a legacy pre-migration entry; run `si doctor --migrate` first",
                    path.display()
                )
            }
            Err(source) => Err(anyhow::anyhow!("parse worker record {}: {source}", path.display())),
        },
    }
}

/// Atomically persist a worker record (0600, parent 0700).
pub fn save_record(path: &Path, record: &WorkerRecord) -> Result<()> {
    if let Some(parent) = path.parent() {
        create_private_dir_all(parent)?;
    }
    let raw = serde_json::to_vec_pretty(record).context("encode worker record")?;
    write_private_file(path, &raw)?;
    Ok(())
}

/// Current UTC time in RFC 3339 (seconds precision is enough for records).
pub fn now_rfc3339() -> String {
    use chrono::{SecondsFormat, Utc};
    Utc::now().to_rfc3339_opts(SecondsFormat::Secs, true)
}

/// Linux process start time (field 22 of `/proc/<pid>/stat`) as a PID-reuse
/// discriminator. `None` where unavailable; callers treat that as "cannot
/// verify", never as a match.
#[cfg(target_os = "linux")]
pub fn process_start_time(pid: u32) -> Option<u64> {
    let stat = std::fs::read_to_string(format!("/proc/{pid}/stat")).ok()?;
    // The comm field may contain spaces/parens; the start time is field 22
    // overall, i.e. the 20th whitespace field after the final ')'.
    let after_comm = stat.rsplit_once(')').map(|(_, tail)| tail)?;
    after_comm.split_whitespace().nth(19)?.parse::<u64>().ok()
}

/// PID-reuse discriminator where `/proc` is unavailable.
#[cfg(not(target_os = "linux"))]
pub fn process_start_time(_pid: u32) -> Option<u64> {
    None
}

/// Send SIGINT to a recorded native PID only when its start time still
/// matches. Returns whether a signal was sent.
#[cfg(unix)]
pub fn interrupt_native_pid(record: &WorkerRecord) -> bool {
    let (Some(pid), Some(started)) = (record.native_pid, record.native_start_time) else {
        return false;
    };
    if process_start_time(pid) != Some(started) {
        return false;
    }
    // SAFETY: kill with a validated live PID is the documented use; ESRCH or
    // EPERM just mean "not signaled".
    unsafe { libc::kill(pid as libc::pid_t, libc::SIGINT) == 0 }
}

/// Non-unix platforms signal through tmux only.
#[cfg(not(unix))]
pub fn interrupt_native_pid(_record: &WorkerRecord) -> bool {
    false
}

/// Outcome of reconciling one record against live tmux state.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Reconciled {
    /// Worker owns a live pane; ownership labels verified.
    Live(PaneInfo),
    /// Nothing live; record already terminal (or was finalized: `changed`).
    Settled { changed: bool },
    /// Session exists but labels do not match: foreign/tampered. The record
    /// is left untouched; only the diagnosis is returned.
    Conflict(String),
}

/// Reconcile one record without calling a model: exited/missing tmux state
/// finalizes the record, live state is ownership-verified. Returns whether
/// the record was modified (caller persists).
pub fn reconcile_record(tmux: &Tmux, record: &mut WorkerRecord) -> Reconciled {
    if !record.state.maybe_live() {
        return Reconciled::Settled { changed: false };
    }
    if !tmux.session_exists(&record.session_name) {
        record.state = WorkerState::Exited;
        record.exit_reason = Some("tmux session gone".to_owned());
        record.ended_at = Some(now_rfc3339());
        return Reconciled::Settled { changed: true };
    }
    let pane = match verify_owned_session(tmux, record) {
        Ok(pane) => pane,
        Err(conflict) => return Reconciled::Conflict(conflict.to_string()),
    };
    if pane.dead {
        record.state = WorkerState::Exited;
        record.exit_code = pane.dead_status;
        record.exit_reason = Some(match pane.dead_status {
            Some(code) => format!("native exited with status {code}"),
            None => "pane dead".to_owned(),
        });
        record.ended_at = Some(now_rfc3339());
        return Reconciled::Settled { changed: true };
    }
    if record.pane_id.as_deref() != Some(pane.pane_id.as_str()) {
        record.pane_id = Some(pane.pane_id.clone());
        return Reconciled::Live(pane);
    }
    Reconciled::Live(pane)
}

/// Finalize a record from a dead pane observation.
fn finalize_from_pane(
    record: &mut WorkerRecord,
    pane: &PaneInfo,
    state: WorkerState,
    action: &str,
) {
    record.state = state;
    record.exit_code = pane.dead_status;
    record.exit_reason = Some(match pane.dead_status {
        Some(code) => format!("{action}; native exit status {code}"),
        None => format!("{action}; pane dead"),
    });
    record.ended_at = Some(now_rfc3339());
}

/// Resolved launch inputs. The provider command layer (`agents::codex`)
/// resolves profile/entry/home; the runner owns reservation and launch.
pub struct LaunchRequest<'a> {
    pub paths: &'a SiPaths,
    pub launch: &'a dyn ProviderLaunch,
    pub profile: &'a str,
    pub slot: &'a SlotId,
    pub display_name: Option<&'a str>,
    pub native_root: &'a Path,
    pub program: &'a Path,
    pub program_version: Option<String>,
    pub argv: Vec<String>,
    pub workdir: PathBuf,
    pub workspace: PathBuf,
    pub resume_id: Option<String>,
}

/// Outcome of a spawn/respawn launch.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum LaunchOutcome {
    Started { session: String, pane: String, generation: String },
    AlreadyRunning { session: String, pane: String, generation: String },
}

/// Reserve the slot, launch the runner session, and commit the running
/// record after the runner confirms native startup. A launch failure rolls
/// back the reservation and the newly created session only.
pub fn launch_worker(request: LaunchRequest<'_>) -> Result<LaunchOutcome> {
    let provider = request.launch.provider();
    let record_path = request.paths.worker_record_path(provider, request.profile, request.slot);
    let spec_path = request.paths.worker_spec_path(provider, request.profile, request.slot);
    let slot_lock = request.paths.slot_lock_path(provider, request.profile, request.slot);
    let _slot = Lease::acquire(&slot_lock, "worker reservation")
        .with_context(|| format!("acquire slot lock {}", slot_lock.display()))?;

    create_private_dir_all(&request.paths.runtime_dir()).context("create runtime dir")?;
    let tmux = Tmux::new(request.paths.tmux_socket_path());
    let session_name = request.launch.session_name(request.profile, request.slot);
    let lease_path = request.paths.worker_lease_path(provider, request.profile);

    let full_argv =
        if request.argv.is_empty() { request.launch.default_argv() } else { request.argv.clone() };
    let command = request
        .launch
        .build_command(request.program, request.native_root, &request.workdir, &full_argv)
        .map_err(|detail| anyhow::anyhow!("{detail}"))?;
    // The contract records pre-forcing user argv (the effective command is a
    // pure function of it), so respawn can reuse it without double-forcing.
    let contract = launch_contract(
        request.program,
        &full_argv,
        &request.workdir,
        &request.workspace,
        request.native_root,
        request.resume_id.as_deref(),
    );

    // Existing record: idempotent iff the live contract matches, else error.
    if let Some(mut record) = load_record(&record_path)? {
        match reconcile_record(&tmux, &mut record) {
            Reconciled::Live(pane) => {
                if record.contract == contract {
                    save_record(&record_path, &record)?;
                    return Ok(LaunchOutcome::AlreadyRunning {
                        session: record.session_name.clone(),
                        pane: pane.pane_id.clone(),
                        generation: record.generation.clone(),
                    });
                }
                anyhow::bail!(
                    "slot {:?} of {} profile {:?} already runs a different launch (generation {}); stop it first",
                    request.slot.as_str(),
                    provider.as_str(),
                    request.profile,
                    record.generation
                );
            }
            Reconciled::Settled { changed } => {
                if changed {
                    save_record(&record_path, &record)?;
                }
            }
            Reconciled::Conflict(diagnosis) => anyhow::bail!("{diagnosis}"),
        }
    }

    // Admission, checked after idempotency so a re-spawn of the live
    // contract reports already-running instead of busy: one
    // credential-using worker per profile until same-profile concurrency is
    // proven. `run` and the runner hold this lease.
    match Lease::try_acquire(&lease_path, "worker admission") {
        Ok(Some(_free)) => {}
        Ok(None) => anyhow::bail!(
            "{} profile {:?} already has an active credential-using worker; same-profile concurrency is disabled until native refresh safety is proven",
            provider.as_str(),
            request.profile
        ),
        Err(source) => return Err(source.into()),
    }

    // A session with our name but no live record is a conflict unless it is
    // a dead leftover already carrying our provider/profile/slot labels.
    if tmux.session_exists(&session_name) {
        let ours = [OPT_PROVIDER, OPT_PROFILE, OPT_SLOT]
            .iter()
            .zip([provider.as_str(), request.profile, request.slot.as_str()])
            .all(|(key, want)| tmux.show_option(&session_name, key).as_deref() == Some(want));
        let dead_leftover = ours && tmux.pane_info(&session_name).is_some_and(|pane| pane.dead);
        if !dead_leftover {
            anyhow::bail!(
                "tmux session {session_name} already exists and is not a dead leftover of this worker; refusing to replace a foreign session"
            );
        }
        tmux.kill_session(&session_name)?;
    }

    let generation = new_generation();
    let spec = WorkerSpec {
        schema_version: WORKER_RECORD_SCHEMA_VERSION,
        provider: provider.as_str().to_owned(),
        profile: request.profile.to_owned(),
        slot: request.slot.as_str().to_owned(),
        si_root: request.paths.root.clone(),
        native_root: request.native_root.to_path_buf(),
        program: request.program.to_path_buf(),
        program_version: request.program_version.clone(),
        argv: command.args_slice().to_vec(),
        env_set: command.env_map().clone(),
        env_unset: command.env_remove_set().iter().cloned().collect(),
        workdir: request.workdir.clone(),
        workspace: request.workspace.clone(),
        socket: request.paths.tmux_socket_path(),
        session_name: session_name.clone(),
        owner: generation.clone(),
        generation: generation.clone(),
        record_path: record_path.clone(),
        lease_path: lease_path.clone(),
        resume_id: request.resume_id.clone(),
        contract: contract.clone(),
    };
    if let Some(parent) = spec_path.parent() {
        create_private_dir_all(parent)?;
    }
    let raw = serde_json::to_vec_pretty(&spec).context("encode launch spec")?;
    write_private_file(&spec_path, &raw)?;

    let mut record = WorkerRecord {
        schema_version: WORKER_RECORD_SCHEMA_VERSION,
        provider: provider.as_str().to_owned(),
        profile: request.profile.to_owned(),
        slot: request.slot.as_str().to_owned(),
        generation: generation.clone(),
        owner: generation.clone(),
        native_root: request.native_root.to_path_buf(),
        program: request.program.to_path_buf(),
        program_version: request.program_version.clone(),
        workspace: request.workspace.clone(),
        workdir: request.workdir.clone(),
        socket: request.paths.tmux_socket_path(),
        session_name: session_name.clone(),
        pane_id: None,
        native_pid: None,
        native_start_time: None,
        state: WorkerState::Reserved,
        exit_code: None,
        exit_reason: None,
        started_at: None,
        ended_at: None,
        contract,
        resume_id: request.resume_id.clone(),
        native_session_id: None,
    };
    save_record(&record_path, &record)?;

    let rollback = |tmux: &Tmux| {
        let _ = tmux.kill_session(&session_name);
        let _ = std::fs::remove_file(&spec_path);
        let _ = std::fs::remove_file(&record_path);
    };
    let si_exe = std::env::current_exe().context("locate SI executable for runner")?;
    let runner_command = format!(
        "{} {} --spec {}",
        shell_quote(&si_exe.display().to_string()),
        RUNNER_SUBCOMMAND,
        shell_quote(&spec_path.display().to_string())
    );
    let window = request.launch.window_name(request.profile, request.display_name, request.slot);
    if let Err(source) = tmux.new_session(&session_name, &window, &runner_command) {
        rollback(&tmux);
        return Err(source);
    }
    for (key, value) in [
        (OPT_OWNER, record.owner.as_str()),
        (OPT_GENERATION, record.generation.as_str()),
        (OPT_PROVIDER, record.provider.as_str()),
        (OPT_PROFILE, record.profile.as_str()),
        (OPT_SLOT, record.slot.as_str()),
    ] {
        if let Err(source) = tmux.set_option(&session_name, key, value) {
            rollback(&tmux);
            return Err(source);
        }
    }

    // Commit only after the runner confirms native startup.
    let deadline = std::time::Instant::now() + SPAWN_CONFIRM_TIMEOUT;
    loop {
        let observed = load_record(&record_path)?
            .ok_or_else(|| anyhow::anyhow!("worker record vanished during launch; rolled back"))?;
        match observed.state {
            WorkerState::Running => {
                let pane = verify_owned_session(&tmux, &observed)?;
                if pane.dead {
                    // The native exited before confirmation: keep polling so
                    // the runner's exit record lands, then fail the launch.
                    std::thread::sleep(Duration::from_millis(100));
                    continue;
                }
                // Stability: a native that survives 250ms past confirmation
                // counts as started; anything quicker is a startup failure
                // with rollback, never a stale running record.
                std::thread::sleep(Duration::from_millis(250));
                let stable = load_record(&record_path)?.ok_or_else(|| {
                    anyhow::anyhow!("worker record vanished during launch; rolled back")
                })?;
                if stable.state != WorkerState::Running {
                    continue;
                }
                let pane = verify_owned_session(&tmux, &stable)?;
                if pane.dead {
                    continue;
                }
                return Ok(LaunchOutcome::Started {
                    session: stable.session_name.clone(),
                    pane: pane.pane_id.clone(),
                    generation: stable.generation.clone(),
                });
            }
            WorkerState::Failed | WorkerState::Exited => {
                let reason =
                    observed.exit_reason.clone().unwrap_or_else(|| "runner failed".to_owned());
                rollback(&tmux);
                anyhow::bail!("worker launch failed: {reason}");
            }
            _ => {
                if std::time::Instant::now() >= deadline {
                    // Leave the session for inspection but drop our claim: the
                    // record keeps the reservation details for diagnosis.
                    record.state = WorkerState::Failed;
                    record.exit_reason = Some("runner did not confirm startup in time".to_owned());
                    record.ended_at = Some(now_rfc3339());
                    save_record(&record_path, &record)?;
                    tmux.kill_session(&session_name)?;
                    anyhow::bail!(
                        "worker launch timed out waiting for native startup; rolled back"
                    );
                }
                std::thread::sleep(Duration::from_millis(100));
            }
        }
    }
}

/// Hidden `__worker-run` entry: execute one private launch spec.
///
/// Validates the spec against the SI root layout, takes the profile worker
/// lease (fail-fast), applies the profile environment immediately before
/// native execution, records PID/startup/exit, and releases the lease.
/// Exits with the native exit code so remain-on-exit panes retain it.
/// Refuse specs whose exec fields a hand-made or tampered spec could use to
/// panic the runner: invalid env names, or interior NUL bytes (which panic
/// `std::process::Command` at spawn). Runs before the record is touched, so a
/// refusal leaves the reservation for the normal launch-timeout rollback.
fn validate_spec_exec_fields(spec: &WorkerSpec) -> Result<()> {
    for key in spec.env_set.keys().chain(spec.env_unset.iter()) {
        validate_env_name(key)
            .map_err(|source| anyhow::anyhow!("runner: bad spec env name: {source}"))?;
    }
    for (key, value) in &spec.env_set {
        if value.bytes().any(|byte| byte == 0) {
            anyhow::bail!("runner: spec env value for {key:?} contains NUL");
        }
    }
    if spec.argv.iter().any(|arg| arg.bytes().any(|byte| byte == 0)) {
        anyhow::bail!("runner: spec argv contains NUL");
    }
    for (label, path) in [("program", &spec.program), ("workdir", &spec.workdir)] {
        if path.as_os_str().to_string_lossy().contains('\0') {
            anyhow::bail!("runner: spec {label} contains NUL");
        }
    }
    Ok(())
}

pub fn run_worker_entry(spec_path: &Path) -> Result<()> {
    let raw = std::fs::read(spec_path)
        .with_context(|| format!("runner: read spec {}", spec_path.display()))?;
    let spec: WorkerSpec = serde_json::from_slice(&raw).context("runner: parse spec")?;
    if spec.schema_version != WORKER_RECORD_SCHEMA_VERSION {
        anyhow::bail!("runner: unsupported spec schema {}", spec.schema_version);
    }
    let provider = Provider::parse(&spec.provider)
        .map_err(|_| anyhow::anyhow!("runner: unknown provider {:?}", spec.provider))?;
    let slot = SlotId::parse(&spec.slot)
        .map_err(|detail| anyhow::anyhow!("runner: bad slot: {detail}"))?;
    let paths = SiPaths::from_si_root(spec.si_root.clone());
    // The spec must describe exactly the paths this root derives. Anything
    // else is a stale/hand-made spec and is refused before spawning.
    if spec.record_path != paths.worker_record_path(provider, &spec.profile, &slot)
        || spec.lease_path != paths.worker_lease_path(provider, &spec.profile)
        || spec.socket != paths.tmux_socket_path()
    {
        anyhow::bail!("runner: spec paths do not match SI root {}", spec.si_root.display());
    }
    let spec_canonical = spec_path.canonicalize().unwrap_or_else(|_| spec_path.to_path_buf());
    let root_canonical = spec.si_root.canonicalize().unwrap_or_else(|_| spec.si_root.clone());
    if !spec_canonical.starts_with(&root_canonical) {
        anyhow::bail!("runner: spec {} escapes SI root", spec_path.display());
    }
    validate_spec_exec_fields(&spec)?;

    let mut record = load_record(&spec.record_path)?.ok_or_else(|| {
        anyhow::anyhow!("runner: reservation for {} slot {:?} is gone", spec.profile, spec.slot)
    })?;
    if record.state != WorkerState::Reserved
        || record.generation != spec.generation
        || record.owner != spec.owner
    {
        anyhow::bail!(
            "runner: stale spec for {} slot {:?} (record is {:?})",
            spec.profile,
            spec.slot,
            record.state
        );
    }
    let fail = |record: &mut WorkerRecord, reason: &str| {
        record.state = WorkerState::Failed;
        record.exit_reason = Some(reason.to_owned());
        record.ended_at = Some(now_rfc3339());
        save_record(&spec.record_path, record)
    };

    // Fail fast: a second credential-using worker must error, never queue
    // behind the lease while the slot lock serializes the spawn side.
    let _lease = match Lease::try_acquire(&spec.lease_path, "worker run") {
        Ok(Some(lease)) => lease,
        Ok(None) => {
            let reason = "profile already has an active credential-using worker";
            let _ = fail(&mut record, reason);
            anyhow::bail!("runner: {reason}");
        }
        Err(LeaseError::Busy { .. }) => {
            let reason = "profile already has an active credential-using worker";
            let _ = fail(&mut record, reason);
            anyhow::bail!("runner: {reason}");
        }
        Err(other) => {
            let _ = fail(&mut record, &other.to_string());
            return Err(other.into());
        }
    };

    let mut command = CommandSpec::new(spec.program.display().to_string())
        .args(spec.argv.clone())
        .current_dir(spec.workdir.clone());
    for (key, value) in &spec.env_set {
        command = command.env(key.clone(), value.clone());
    }
    for key in &spec.env_unset {
        command = command.env_remove(key.clone());
    }
    // Non-secret worker labels, applied immediately before native execution.
    command = command
        .env("SI_PROVIDER", spec.provider.clone())
        .env("SI_PROFILE", spec.profile.clone())
        .env("SI_WORKER_SLOT", spec.slot.clone());
    let mut child = match ProcessRunner.spawn_inherit(&command) {
        Ok(child) => child,
        Err(source) => {
            let _ = fail(&mut record, &format!("spawn native: {source}"));
            anyhow::bail!("runner: spawn native: {source}");
        }
    };
    record.native_pid = Some(child.id());
    record.native_start_time = process_start_time(child.id());
    record.state = WorkerState::Running;
    record.started_at = Some(now_rfc3339());
    if record.pane_id.is_none() {
        record.pane_id = std::env::var("TMUX_PANE").ok().filter(|pane| !pane.is_empty());
    }
    if let Err(source) = save_record(&spec.record_path, &record) {
        let _ = child.kill();
        let _ = fail(&mut record, &format!("record startup: {source}"));
        return Err(source);
    }

    let status = child.wait().with_context(|| "runner: wait for native")?;
    let code = exit_code_of(&status);
    record.state = WorkerState::Exited;
    record.exit_code = Some(code);
    record.exit_reason = Some(format!("native exited with status {code}"));
    record.ended_at = Some(now_rfc3339());
    save_record(&spec.record_path, &record)?;
    drop(_lease);
    std::process::exit(code);
}

/// Portable exit code: real code, or 128+signal on unix, else 1.
#[cfg(unix)]
fn exit_code_of(status: &std::process::ExitStatus) -> i32 {
    use std::os::unix::process::ExitStatusExt;
    status.code().unwrap_or_else(|| status.signal().map(|signal| 128 + signal).unwrap_or(1))
}

/// Portable exit code where signals do not exist.
#[cfg(not(unix))]
fn exit_code_of(status: &std::process::ExitStatus) -> i32 {
    status.code().unwrap_or(1)
}

/// Outcome of `stop`.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum StopOutcome {
    Stopped { exit: Option<i32> },
    AlreadyExited { exit: Option<i32> },
}

/// Stop a worker: graceful interrupt to the owned native process, bounded
/// wait, explicit `--force` for the owned session. Only the verified owned
/// session is ever signaled.
pub fn stop_worker(
    paths: &SiPaths,
    launch: &dyn ProviderLaunch,
    profile: &str,
    slot: &SlotId,
    force: bool,
) -> Result<StopOutcome> {
    let provider = launch.provider();
    let record_path = paths.worker_record_path(provider, profile, slot);
    let slot_lock = paths.slot_lock_path(provider, profile, slot);
    let _slot = Lease::acquire(&slot_lock, "worker stop")?;
    let tmux = Tmux::new(paths.tmux_socket_path());

    let mut record = load_record(&record_path)?.ok_or_else(|| {
        anyhow::anyhow!(
            "no {} worker for profile {:?} slot {:?}; nothing to stop",
            provider.as_str(),
            profile,
            slot.as_str()
        )
    })?;
    match reconcile_record(&tmux, &mut record) {
        Reconciled::Settled { changed } => {
            if changed {
                save_record(&record_path, &record)?;
            }
            return Ok(StopOutcome::AlreadyExited { exit: record.exit_code });
        }
        Reconciled::Conflict(diagnosis) => anyhow::bail!("{diagnosis}"),
        Reconciled::Live(_) => {}
    }

    // Graceful interrupt: SIGINT the verified native PID when possible, and
    // always C-c the owned pane (covers TUI foreground groups).
    interrupt_native_pid(&record);
    let target = record.pane_id.clone().unwrap_or_else(|| record.session_name.clone());
    tmux.send_keys(&target, &["C-c"])?;
    let deadline = std::time::Instant::now() + STOP_GRACE_PERIOD;
    let pane = loop {
        let pane = verify_owned_session(&tmux, &record)?;
        if pane.dead || std::time::Instant::now() >= deadline {
            break pane;
        }
        std::thread::sleep(Duration::from_millis(100));
    };
    if pane.dead {
        finalize_from_pane(&mut record, &pane, WorkerState::Stopped, "stopped");
        save_record(&record_path, &record)?;
        return Ok(StopOutcome::Stopped { exit: record.exit_code });
    }
    if !force {
        anyhow::bail!(
            "worker for {} profile {:?} slot {:?} is still running; pass --force to kill the owned session",
            provider.as_str(),
            profile,
            slot.as_str()
        );
    }
    // Forced: re-interrupt the verified PID, then kill the verified session.
    interrupt_native_pid(&record);
    std::thread::sleep(STOP_FORCE_PID_WAIT);
    if let Ok(pane) = verify_owned_session(&tmux, &record)
        && !pane.dead
    {
        tmux.kill_session(&record.session_name)?;
    }
    match tmux.pane_info(&record.session_name) {
        Some(pane) if pane.dead => {
            finalize_from_pane(&mut record, &pane, WorkerState::Stopped, "force-stopped");
        }
        _ => {
            record.state = WorkerState::Stopped;
            record.exit_reason = Some("force-stopped; session killed".to_owned());
            record.ended_at = Some(now_rfc3339());
        }
    }
    save_record(&record_path, &record)?;
    Ok(StopOutcome::Stopped { exit: record.exit_code })
}

/// Outcome of `remove`.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum RemoveOutcome {
    Removed { native_root: PathBuf, workspace: PathBuf },
}

/// Remove idle lifecycle metadata and the owned pane/session. Stops only
/// when explicitly requested (`stop_first`). Native history, credentials,
/// and the workspace are never touched.
pub fn remove_worker(
    paths: &SiPaths,
    launch: &dyn ProviderLaunch,
    profile: &str,
    slot: &SlotId,
    stop_first: bool,
) -> Result<RemoveOutcome> {
    let provider = launch.provider();
    let record_path = paths.worker_record_path(provider, profile, slot);
    let spec_path = paths.worker_spec_path(provider, profile, slot);
    let slot_lock = paths.slot_lock_path(provider, profile, slot);
    let _slot = Lease::acquire(&slot_lock, "worker remove")?;
    let tmux = Tmux::new(paths.tmux_socket_path());

    let mut record = load_record(&record_path)?.ok_or_else(|| {
        anyhow::anyhow!(
            "no {} worker for profile {:?} slot {:?}; nothing to remove",
            provider.as_str(),
            profile,
            slot.as_str()
        )
    })?;
    match reconcile_record(&tmux, &mut record) {
        Reconciled::Live(_) if !stop_first => {
            anyhow::bail!(
                "worker for {} profile {:?} slot {:?} is active; stop it first or pass --stop",
                provider.as_str(),
                profile,
                slot.as_str()
            );
        }
        Reconciled::Live(_) => {
            drop(_slot);
            stop_worker(paths, launch, profile, slot, false).with_context(
                || "remove --stop could not stop the worker gracefully; stop it explicitly first",
            )?;
            let _slot = Lease::acquire(&slot_lock, "worker remove")?;
            record = load_record(&record_path)?
                .ok_or_else(|| anyhow::anyhow!("worker record vanished during remove --stop"))?;
        }
        Reconciled::Conflict(diagnosis) => anyhow::bail!("{diagnosis}"),
        Reconciled::Settled { .. } => {}
    }
    // Kill a dead remnant session only when ownership still verifies.
    if tmux.session_exists(&record.session_name) && verify_owned_session(&tmux, &record).is_ok() {
        tmux.kill_session(&record.session_name)?;
    }
    std::fs::remove_file(&record_path)
        .with_context(|| format!("remove worker record {}", record_path.display()))?;
    let _ = std::fs::remove_file(&spec_path);
    Ok(RemoveOutcome::Removed { native_root: record.native_root, workspace: record.workspace })
}

/// Explicit local tail of the owned pane (live or remain-on-exit dead).
/// Never uploads; never touches a foreign session.
pub fn tail_worker(
    paths: &SiPaths,
    launch: &dyn ProviderLaunch,
    profile: &str,
    slot: &SlotId,
    lines: usize,
) -> Result<String> {
    let provider = launch.provider();
    let record_path = paths.worker_record_path(provider, profile, slot);
    let slot_lock = paths.slot_lock_path(provider, profile, slot);
    let _slot = Lease::acquire(&slot_lock, "worker tail")?;
    let tmux = Tmux::new(paths.tmux_socket_path());

    let record = load_record(&record_path)?.ok_or_else(|| {
        anyhow::anyhow!(
            "no {} worker for profile {:?} slot {:?}",
            provider.as_str(),
            profile,
            slot.as_str()
        )
    })?;
    if record.state == WorkerState::Legacy {
        anyhow::bail!(
            "worker for {} profile {:?} slot {:?} is a migrated legacy entry with no owned pane",
            provider.as_str(),
            profile,
            slot.as_str()
        );
    }
    let pane = verify_owned_session(&tmux, &record)?;
    tmux.capture_pane(&pane.pane_id, lines)
}

/// Attach to the owned worker. Drops all locks, then replaces this process
/// with the tmux client. Refuses without a terminal (use `tail` instead).
#[cfg(unix)]
pub fn attach_worker(
    paths: &SiPaths,
    launch: &dyn ProviderLaunch,
    profile: &str,
    slot: &SlotId,
) -> Result<()> {
    use std::io::IsTerminal;
    let provider = launch.provider();
    let record_path = paths.worker_record_path(provider, profile, slot);
    let slot_lock = paths.slot_lock_path(provider, profile, slot);
    let session = {
        let _slot = Lease::acquire(&slot_lock, "worker attach")?;
        let tmux = Tmux::new(paths.tmux_socket_path());
        let mut record = load_record(&record_path)?.ok_or_else(|| {
            anyhow::anyhow!(
                "no {} worker for profile {:?} slot {:?}",
                provider.as_str(),
                profile,
                slot.as_str()
            )
        })?;
        match reconcile_record(&tmux, &mut record) {
            Reconciled::Live(_) => {
                save_record(&record_path, &record)?;
                record.session_name.clone()
            }
            Reconciled::Settled { .. } => anyhow::bail!(
                "worker for {} profile {:?} slot {:?} is not running; use `tail` for retained output or `respawn` to restart",
                provider.as_str(),
                profile,
                slot.as_str()
            ),
            Reconciled::Conflict(diagnosis) => anyhow::bail!("{diagnosis}"),
        }
    };
    if !std::io::stdin().is_terminal() {
        anyhow::bail!("attach needs a terminal; use `tail` for non-interactive output");
    }
    let tmux = Tmux::new(paths.tmux_socket_path());
    Err(tmux.attach_exec(&session).into())
}

/// Attach where `exec` is unavailable: not supported (use `tail`).
#[cfg(not(unix))]
pub fn attach_worker(
    _paths: &SiPaths,
    launch: &dyn ProviderLaunch,
    profile: &str,
    slot: &SlotId,
) -> Result<()> {
    let _ = (launch.provider(), profile, slot.as_str());
    anyhow::bail!("attach is only supported on unix; use `tail` for pane output")
}

/// One reconciled row for `list`.
#[derive(Clone, Debug, Serialize)]
pub struct WorkerRow {
    pub profile: String,
    pub slot: String,
    pub state: String,
    pub session: String,
    pub pane: Option<String>,
    pub pid: Option<u32>,
    pub exit_code: Option<i32>,
    pub generation: String,
    pub workspace: String,
    pub program_version: Option<String>,
    pub conflict: Option<String>,
}

/// Reconcile every record of a provider against the owned socket.
/// Persists records that changed; never calls a model.
pub fn list_workers(paths: &SiPaths, launch: &dyn ProviderLaunch) -> Result<Vec<WorkerRow>> {
    let provider = launch.provider();
    let tmux = Tmux::new(paths.tmux_socket_path());
    let mut rows = Vec::new();
    let workers = paths.workers_dir(provider);
    let mut record_paths = Vec::new();
    if let Ok(profiles) = std::fs::read_dir(&workers) {
        for profile in profiles.flatten() {
            if !profile.file_type().map(|kind| kind.is_dir()).unwrap_or(false) {
                continue;
            }
            if let Ok(slots) = std::fs::read_dir(profile.path()) {
                for slot in slots.flatten() {
                    let path = slot.path();
                    let name = path.file_name().and_then(|name| name.to_str()).unwrap_or("");
                    // Records are `<slot>.json`; skip transient `<slot>.spec.json`.
                    if name.ends_with(".json") && !name.ends_with(".spec.json") {
                        record_paths.push(path);
                    }
                }
            }
        }
    }
    record_paths.sort();
    for record_path in record_paths {
        // Legacy pre-migration entries are migration's subject, not live
        // workers: skip with a hint instead of failing the listing (this
        // keeps `list` and the migration blocker scan working pre-migrate;
        // spawn/stop/tail still refuse legacy slots via `load_record`).
        if let Ok(raw) = std::fs::read(&record_path) {
            if serde_json::from_slice::<WorkerRecord>(&raw).is_err()
                && super::migrate::is_legacy_record_bytes(&raw)
            {
                eprintln!(
                    "warning: {} is a legacy pre-migration entry; run `si doctor --migrate`",
                    record_path.display()
                );
                continue;
            }
        }
        let Some(mut record) = load_record(&record_path)? else {
            continue;
        };
        if record.provider != provider.as_str() {
            continue;
        }
        let slot = match SlotId::parse(&record.slot) {
            Ok(slot) => slot,
            Err(_) => continue,
        };
        let slot_lock = paths.slot_lock_path(provider, &record.profile, &slot);
        let _held = Lease::acquire(&slot_lock, "worker list")?;
        let (state, conflict) = match reconcile_record(&tmux, &mut record) {
            Reconciled::Live(_) => {
                save_record(&record_path, &record)?;
                (record.state.as_str().to_owned(), None)
            }
            Reconciled::Settled { changed } => {
                if changed {
                    save_record(&record_path, &record)?;
                }
                (record.state.as_str().to_owned(), None)
            }
            Reconciled::Conflict(diagnosis) => (record.state.as_str().to_owned(), Some(diagnosis)),
        };
        rows.push(WorkerRow {
            profile: record.profile.clone(),
            slot: record.slot.clone(),
            state,
            session: record.session_name.clone(),
            pane: record.pane_id.clone(),
            pid: record.native_pid,
            exit_code: record.exit_code,
            generation: record.generation.clone(),
            workspace: record.workspace.display().to_string(),
            program_version: record.program_version.clone(),
            conflict,
        });
    }
    Ok(rows)
}

/// Respawn inputs: reuse the stored workspace/workdir/argv, optionally with
/// a validated native resume ID. Never creates a fresh conversation when an
/// explicit resume ID is unknown.
pub struct RespawnRequest<'a> {
    pub paths: &'a SiPaths,
    pub launch: &'a dyn ProviderLaunch,
    pub profile: &'a str,
    pub slot: &'a SlotId,
    pub display_name: Option<&'a str>,
    pub native_root: &'a Path,
    pub program: &'a Path,
    pub program_version: Option<String>,
    pub resume_id: Option<String>,
}

pub fn respawn_worker(request: RespawnRequest<'_>) -> Result<LaunchOutcome> {
    let provider = request.launch.provider();
    let record_path = request.paths.worker_record_path(provider, request.profile, request.slot);
    let slot_lock = request.paths.slot_lock_path(provider, request.profile, request.slot);
    let tmux = Tmux::new(request.paths.tmux_socket_path());

    let (workspace, workdir, argv) = {
        let _slot = Lease::acquire(&slot_lock, "worker respawn check")?;
        let mut record = load_record(&record_path)?.ok_or_else(|| {
            anyhow::anyhow!(
                "no {} worker for profile {:?} slot {:?}; use `spawn` to start one",
                provider.as_str(),
                request.profile,
                request.slot.as_str()
            )
        })?;
        match reconcile_record(&tmux, &mut record) {
            Reconciled::Live(_) => anyhow::bail!(
                "worker for {} profile {:?} slot {:?} is already running; stop it first",
                provider.as_str(),
                request.profile,
                request.slot.as_str()
            ),
            Reconciled::Settled { changed } => {
                if changed {
                    save_record(&record_path, &record)?;
                }
            }
            Reconciled::Conflict(diagnosis) => anyhow::bail!("{diagnosis}"),
        }
        if record.provider != provider.as_str() {
            anyhow::bail!("worker record provider mismatch; refusing to respawn across providers");
        }
        let argv = match request.resume_id.as_deref() {
            Some(id) => {
                let id = id.trim();
                if !request.launch.session_known(request.native_root, id) {
                    anyhow::bail!(
                        "native session {id:?} is not known in {} profile {:?}; refusing to start a fresh conversation in its place",
                        provider.as_str(),
                        request.profile
                    );
                }
                request.launch.resume_argv(id)
            }
            None => contract_argv(&record.contract).unwrap_or_default(),
        };
        (record.workspace.clone(), record.workdir.clone(), argv)
    };

    // The slot lock is released before launch: launch_worker re-acquires it
    // and re-validates, so a concurrent spawn cannot interleave a launch.
    launch_worker(LaunchRequest {
        paths: request.paths,
        launch: request.launch,
        profile: request.profile,
        slot: request.slot,
        display_name: request.display_name,
        native_root: request.native_root,
        program: request.program,
        program_version: request.program_version,
        argv,
        workdir,
        workspace,
        resume_id: request.resume_id,
    })
}

/// Recover the stored native argv from a launch contract. `None` when the
/// contract is unreadable (respawn then uses the provider default).
fn contract_argv(contract: &str) -> Option<Vec<String>> {
    let value: serde_json::Value = serde_json::from_str(contract).ok()?;
    value
        .get("argv")?
        .as_array()?
        .iter()
        .map(|item| item.as_str().map(str::to_owned))
        .collect::<Option<Vec<_>>>()
}

#[cfg(test)]
mod tests {
    use super::{
        PaneInfo, WORKER_RECORD_SCHEMA_VERSION, WorkerSpec, WorkerState, contract_argv,
        launch_contract, new_generation, parse_pane_line, shell_quote, validate_spec_exec_fields,
    };
    use std::collections::BTreeMap;
    use std::path::{Path, PathBuf};

    fn valid_spec() -> WorkerSpec {
        WorkerSpec {
            schema_version: WORKER_RECORD_SCHEMA_VERSION,
            provider: "codex".to_owned(),
            profile: "a".to_owned(),
            slot: "main".to_owned(),
            si_root: PathBuf::from("/si"),
            native_root: PathBuf::from("/si/codex/profiles/a"),
            program: PathBuf::from("/bin/codex"),
            program_version: None,
            argv: vec!["-c".to_owned(), "cli_auth_credentials_store=\"file\"".to_owned()],
            env_set: BTreeMap::from([("CODEX_HOME".to_owned(), "/si/codex/profiles/a".to_owned())]),
            env_unset: vec!["OPENAI_API_KEY".to_owned()],
            workdir: PathBuf::from("/work"),
            workspace: PathBuf::from("/ws"),
            socket: PathBuf::from("/si/runtime/tmux.sock"),
            session_name: "si-codex-a-main".to_owned(),
            owner: "owner".to_owned(),
            generation: "gen".to_owned(),
            record_path: PathBuf::from("/si/record.json"),
            lease_path: PathBuf::from("/si/lease"),
            resume_id: None,
            contract: "{}".to_owned(),
        }
    }

    #[test]
    fn spec_exec_fields_reject_bad_names_and_nul_bytes() {
        assert!(validate_spec_exec_fields(&valid_spec()).is_ok());
        let mut bad_name = valid_spec();
        bad_name.env_set.insert("EV;IL".to_owned(), "x".to_owned());
        assert!(validate_spec_exec_fields(&bad_name).is_err());
        let mut bad_unset = valid_spec();
        bad_unset.env_unset.push("A=B".to_owned());
        assert!(validate_spec_exec_fields(&bad_unset).is_err());
        let mut bad_value = valid_spec();
        bad_value.env_set.insert("OK".to_owned(), "a\0b".to_owned());
        let err = validate_spec_exec_fields(&bad_value).expect_err("NUL value must fail");
        assert!(err.to_string().contains("NUL"), "{err}");
        let mut bad_argv = valid_spec();
        bad_argv.argv.push("a\0b".to_owned());
        assert!(validate_spec_exec_fields(&bad_argv).is_err());
        let mut bad_program = valid_spec();
        bad_program.program = PathBuf::from("/bin/co\0dex");
        assert!(validate_spec_exec_fields(&bad_program).is_err());
    }

    #[test]
    fn pane_line_parses_live_and_dead_panes() {
        assert_eq!(
            parse_pane_line("%0 0 "),
            Some(PaneInfo { pane_id: "%0".to_owned(), dead: false, dead_status: None })
        );
        assert_eq!(
            parse_pane_line("%3 1 130"),
            Some(PaneInfo { pane_id: "%3".to_owned(), dead: true, dead_status: Some(130) })
        );
        assert_eq!(parse_pane_line(""), None);
        assert_eq!(parse_pane_line("%0"), None);
    }

    #[test]
    fn shell_quote_handles_spaces_and_single_quotes() {
        assert_eq!(shell_quote("plain"), "'plain'");
        assert_eq!(shell_quote("/tmp/a b/c.spec.json"), "'/tmp/a b/c.spec.json'");
        assert_eq!(shell_quote("o'clock"), "'o'\\''clock'");
        assert_eq!(shell_quote(""), "''");
    }

    #[test]
    fn launch_contract_roundtrips_argv_and_distinguishes_launches() {
        let argv = vec!["exec".to_owned(), "--json".to_owned()];
        let contract = launch_contract(
            Path::new("/bin/codex"),
            &argv,
            Path::new("/work"),
            Path::new("/ws"),
            Path::new("/native"),
            None,
        );
        assert_eq!(contract_argv(&contract), Some(argv));
        let other = launch_contract(
            Path::new("/bin/codex"),
            &["exec".to_owned()],
            Path::new("/work"),
            Path::new("/ws"),
            Path::new("/native"),
            None,
        );
        assert_ne!(contract, other);
        assert_eq!(contract_argv("not json"), None);
    }

    #[test]
    fn generations_are_unique_and_worker_states_classify_liveness() {
        assert_ne!(new_generation(), new_generation());
        assert!(WorkerState::Reserved.maybe_live());
        assert!(WorkerState::Running.maybe_live());
        for state in
            [WorkerState::Exited, WorkerState::Stopped, WorkerState::Failed, WorkerState::Legacy]
        {
            assert!(!state.maybe_live());
            assert_eq!(state.as_str(), format!("{state:?}").to_lowercase());
        }
    }
}
