#![recursion_limit = "256"]

use std::collections::{BTreeMap, BTreeSet, HashMap, HashSet};
use std::env;
use std::fs::{self, File, OpenOptions};
use std::future::pending;
use std::io::{BufRead, BufReader, Write};
use std::net::SocketAddr;
#[cfg(unix)]
use std::os::fd::AsRawFd;
use std::path::{Path, PathBuf};
use std::process;
use std::str::FromStr;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;
use std::time::{SystemTime, UNIX_EPOCH};

use anyhow::{Context, Result, anyhow};
use axum::Json;
use axum::Router;
use axum::extract::ws::{Message, WebSocket, WebSocketUpgrade};
use axum::extract::{Path as AxumPath, State};
use axum::http::header::AUTHORIZATION;
use axum::http::{HeaderMap, StatusCode};
use axum::response::IntoResponse;
use axum::response::Response;
use axum::routing::{get, post};
use chrono::{DateTime, Duration as ChronoDuration, SecondsFormat, Utc};
use cron::Schedule;
use futures_util::{SinkExt, StreamExt};
use serde::de::DeserializeOwned;
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use si_nucleus_core::{
    BlockedReason, CanonicalEvent, CanonicalEventSource, CanonicalEventType, EventDataEnvelope,
    EventId, ProfileName, ProfileRecord, RunId, RunRecord, RunStatus, SessionId,
    SessionLifecycleState, SessionRecord, TaskId, TaskRecord, TaskSource, TaskStatus, WorkerId,
    WorkerRecord, WorkerStatus,
};
use si_nucleus_runtime::{
    CanonicalEventDraft, NucleusRuntime, RunInputItem, RunTurnSpec, SessionOpenSpec,
    WorkerLaunchSpec, WorkerProbeResult, WorkerRuntimeView, WorkerStartResult,
};
use si_rs_codex::codex_profile_fort_session_paths;
use si_rs_fort::{
    SessionState, SessionStateFileError, build_bootstrap_view, classify_persisted_session_state,
    load_persisted_session_state,
};
use tokio::net::TcpListener;
use tokio::sync::broadcast;

const DEFAULT_BIND_ADDR: &str = "127.0.0.1:4747";
const WS_PATH: &str = "/ws";
const OPENAPI_PATH: &str = "/openapi.json";
const REST_STATUS_PATH: &str = "/status";
const REST_TASKS_PATH: &str = "/tasks";
const REST_TASK_PATH: &str = "/tasks/{task_id}";
const REST_TASK_CANCEL_PATH: &str = "/tasks/{task_id}/cancel";
const REST_EVENTS_PATH: &str = "/events";
const REST_WORKERS_PATH: &str = "/workers";
const REST_WORKER_PATH: &str = "/workers/{worker_id}";
const REST_SESSION_PATH: &str = "/sessions/{session_id}";
const REST_RUN_PATH: &str = "/runs/{run_id}";
const REST_HOOK_RULES_PATH: &str = "/producers/hook";
const REST_HOOK_RULE_PATH: &str = "/producers/hook/{rule_name}";
const DISPATCH_LOOP_INTERVAL: Duration = Duration::from_millis(200);
const BACKGROUND_WARNING_THROTTLE_WINDOW: ChronoDuration = ChronoDuration::seconds(60);
const MAX_EVENTS_JSONL_BYTES: u64 = 1024 * 1024;
const DEFAULT_TASK_RETENTION_DAYS: u64 = 30;
const WORKER_RESTART_BACKOFF_BASE: Duration = Duration::from_millis(100);
const MAX_WORKER_RESTART_ATTEMPTS: u32 = 3;
pub const GPT_ACTIONS_OPENAPI_PUBLIC_URL: &str = "https://nucleus.aureuma.ai";

fn extract_bearer_token(headers: &HeaderMap) -> Option<String> {
    let header = headers.get(AUTHORIZATION)?.to_str().ok()?.trim();
    let token = header.strip_prefix("Bearer ")?.trim();
    (!token.is_empty()).then(|| token.to_owned())
}

fn temp_suffix() -> String {
    let millis = SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default().as_millis();
    format!("{millis:x}-{}", process::id())
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct NucleusConfig {
    pub bind_addr: SocketAddr,
    pub state_dir: PathBuf,
    pub auth_token: Option<String>,
}

impl NucleusConfig {
    pub fn from_env() -> Result<Self> {
        let bind_addr = env::var("SI_NUCLEUS_BIND_ADDR")
            .unwrap_or_else(|_| DEFAULT_BIND_ADDR.to_owned())
            .parse::<SocketAddr>()
            .context("parse SI_NUCLEUS_BIND_ADDR")?;
        let state_dir = env::var_os("SI_NUCLEUS_STATE_DIR")
            .map(PathBuf::from)
            .unwrap_or_else(default_state_dir);
        let auth_token =
            env::var("SI_NUCLEUS_AUTH_TOKEN").ok().filter(|value| !value.trim().is_empty());
        Ok(Self { bind_addr, state_dir, auth_token })
    }

    pub fn ws_url(&self) -> String {
        format!("ws://{}{}", self.bind_addr, WS_PATH)
    }
}

fn default_state_dir() -> PathBuf {
    env::var_os("HOME")
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("."))
        .join(".si")
        .join("nucleus")
}

#[derive(Clone, Debug)]
pub struct NucleusPaths {
    pub root: PathBuf,
    pub run_dir: PathBuf,
    pub logs_dir: PathBuf,
    pub workers_dir: PathBuf,
    pub sessions_dir: PathBuf,
    pub tmp_dir: PathBuf,
    pub state_dir: PathBuf,
    pub gateway_dir: PathBuf,
    pub tasks_state_dir: PathBuf,
    pub workers_state_dir: PathBuf,
    pub sessions_state_dir: PathBuf,
    pub runs_state_dir: PathBuf,
    pub events_state_dir: PathBuf,
    pub profiles_state_dir: PathBuf,
    pub cron_state_dir: PathBuf,
    pub hook_state_dir: PathBuf,
    pub events_path: PathBuf,
}

impl NucleusPaths {
    pub fn new(root: PathBuf) -> Self {
        let run_dir = root.join("run");
        let logs_dir = root.join("logs");
        let workers_dir = root.join("workers");
        let sessions_dir = root.join("sessions");
        let tmp_dir = root.join("tmp");
        let state_dir = root.join("state");
        let gateway_dir = root.join("gateway");
        let tasks_state_dir = state_dir.join("tasks");
        let workers_state_dir = state_dir.join("workers");
        let sessions_state_dir = state_dir.join("sessions");
        let runs_state_dir = state_dir.join("runs");
        let events_state_dir = state_dir.join("events");
        let profiles_state_dir = state_dir.join("profiles");
        let cron_state_dir = state_dir.join("producers").join("cron");
        let hook_state_dir = state_dir.join("producers").join("hook");
        let events_path = events_state_dir.join("events.jsonl");
        Self {
            root,
            run_dir,
            logs_dir,
            workers_dir,
            sessions_dir,
            tmp_dir,
            state_dir,
            gateway_dir,
            tasks_state_dir,
            workers_state_dir,
            sessions_state_dir,
            runs_state_dir,
            events_state_dir,
            profiles_state_dir,
            cron_state_dir,
            hook_state_dir,
            events_path,
        }
    }

    pub fn ensure_layout(&self) -> Result<()> {
        for dir in [
            &self.root,
            &self.run_dir,
            &self.logs_dir,
            &self.workers_dir,
            &self.sessions_dir,
            &self.tmp_dir,
            &self.state_dir,
            &self.gateway_dir,
            &self.tasks_state_dir,
            &self.workers_state_dir,
            &self.sessions_state_dir,
            &self.runs_state_dir,
            &self.events_state_dir,
            &self.profiles_state_dir,
            &self.cron_state_dir,
            &self.hook_state_dir,
        ] {
            fs::create_dir_all(dir).with_context(|| format!("create {}", dir.display()))?;
        }
        if !self.events_path.exists() {
            File::create(&self.events_path)
                .with_context(|| format!("create {}", self.events_path.display()))?;
        }
        Ok(())
    }

    pub fn task_dir(&self, task_id: &TaskId) -> PathBuf {
        self.tasks_state_dir.join(task_id.as_str())
    }

    pub fn task_path(&self, task_id: &TaskId) -> PathBuf {
        self.task_dir(task_id).join("task.json")
    }

    pub fn worker_dir(&self, worker_id: &WorkerId) -> PathBuf {
        self.workers_state_dir.join(worker_id.as_str())
    }

    pub fn worker_path(&self, worker_id: &WorkerId) -> PathBuf {
        self.worker_dir(worker_id).join("state.json")
    }

    pub fn worker_runtime_path(&self, worker_id: &WorkerId) -> PathBuf {
        self.worker_dir(worker_id).join("runtime.json")
    }

    pub fn worker_summary_path(&self, worker_id: &WorkerId) -> PathBuf {
        self.worker_dir(worker_id).join("summary.md")
    }

    pub fn session_dir(&self, session_id: &SessionId) -> PathBuf {
        self.sessions_state_dir.join(session_id.as_str())
    }

    pub fn session_path(&self, session_id: &SessionId) -> PathBuf {
        self.session_dir(session_id).join("session.json")
    }

    pub fn session_summary_path(&self, session_id: &SessionId) -> PathBuf {
        self.session_dir(session_id).join("summary.md")
    }

    pub fn run_dir(&self, run_id: &RunId) -> PathBuf {
        self.runs_state_dir.join(run_id.as_str())
    }

    pub fn run_path(&self, run_id: &RunId) -> PathBuf {
        self.run_dir(run_id).join("run.json")
    }

    pub fn run_summary_path(&self, run_id: &RunId) -> PathBuf {
        self.run_dir(run_id).join("summary.md")
    }

    pub fn profile_path(&self, profile: &ProfileName) -> PathBuf {
        self.profiles_state_dir.join(format!("{}.json", profile.as_str()))
    }

    pub fn cron_rule_path(&self, rule_name: &str) -> PathBuf {
        self.cron_state_dir.join(format!("{rule_name}.json"))
    }

    pub fn hook_rule_path(&self, rule_name: &str) -> PathBuf {
        self.hook_state_dir.join(format!("{rule_name}.json"))
    }
}

#[derive(Clone, Debug, Serialize)]
pub struct NucleusStatusView {
    pub version: String,
    pub bind_addr: String,
    pub ws_url: String,
    pub state_dir: String,
    pub task_count: usize,
    pub worker_count: usize,
    pub session_count: usize,
    pub run_count: usize,
    pub next_event_seq: u64,
}

#[derive(Clone, Debug, Serialize)]
struct GatewayMetadata {
    version: String,
    bind_addr: String,
    ws_url: String,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct WorkerInspectView {
    pub worker: WorkerRecord,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub runtime: Option<WorkerRuntimeView>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
enum CronScheduleKind {
    OnceAt,
    Every,
    Cron,
}

fn current_persisted_version() -> &'static str {
    env!("CARGO_PKG_VERSION")
}

fn default_persisted_version() -> String {
    current_persisted_version().to_owned()
}

fn deserialize_persisted_version<'de, D>(deserializer: D) -> std::result::Result<String, D::Error>
where
    D: serde::Deserializer<'de>,
{
    #[derive(Deserialize)]
    #[serde(untagged)]
    enum PersistedVersion {
        String(String),
        Unsigned(u64),
        Signed(i64),
    }

    Ok(match PersistedVersion::deserialize(deserializer)? {
        PersistedVersion::String(version) => version,
        PersistedVersion::Unsigned(version) => version.to_string(),
        PersistedVersion::Signed(version) => version.to_string(),
    })
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
struct CronRuleRecord {
    name: String,
    enabled: bool,
    schedule_kind: CronScheduleKind,
    schedule: String,
    instructions: String,
    last_emitted_at: Option<DateTime<Utc>>,
    next_due_at: Option<DateTime<Utc>>,
    #[serde(
        default = "default_persisted_version",
        deserialize_with = "deserialize_persisted_version"
    )]
    version: String,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
struct HookRuleRecord {
    name: String,
    enabled: bool,
    match_event_type: String,
    instructions: String,
    #[serde(default)]
    last_processed_event_seq: u64,
    #[serde(
        default = "default_persisted_version",
        deserialize_with = "deserialize_persisted_version"
    )]
    version: String,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum FortCapabilityState {
    Ready,
    AuthRequired,
    Unavailable,
}

pub struct NucleusStore {
    paths: NucleusPaths,
    next_event_seq: AtomicU64,
    write_lock: Mutex<()>,
    _instance_lock: NucleusInstanceLock,
}

struct NucleusInstanceLock {
    #[allow(dead_code)]
    file: File,
}

impl NucleusInstanceLock {
    fn acquire(paths: &NucleusPaths) -> Result<Self> {
        let path = paths.run_dir.join("nucleus.lock");
        let mut file = OpenOptions::new()
            .create(true)
            .truncate(false)
            .read(true)
            .write(true)
            .open(&path)
            .with_context(|| format!("open nucleus instance lock {}", path.display()))?;
        lock_instance_file(&file, &path)?;
        file.set_len(0)
            .with_context(|| format!("truncate nucleus instance lock {}", path.display()))?;
        writeln!(
            &mut file,
            "pid={}\nversion={}\nstate_dir={}",
            process::id(),
            env!("CARGO_PKG_VERSION"),
            paths.root.display()
        )
        .with_context(|| format!("write nucleus instance lock {}", path.display()))?;
        Ok(Self { file })
    }
}

#[cfg(unix)]
fn lock_instance_file(file: &File, path: &Path) -> Result<()> {
    let rc = unsafe { libc::flock(file.as_raw_fd(), libc::LOCK_EX | libc::LOCK_NB) };
    if rc == 0 {
        return Ok(());
    }
    let err = std::io::Error::last_os_error();
    if err.raw_os_error() == Some(libc::EWOULDBLOCK) {
        anyhow::bail!(
            "nucleus state directory is already locked by another si-nucleus process: {}",
            path.display()
        );
    }
    Err(err).with_context(|| format!("lock nucleus instance file {}", path.display()))
}

#[cfg(not(unix))]
fn lock_instance_file(_file: &File, _path: &Path) -> Result<()> {
    Ok(())
}

impl NucleusStore {
    pub fn open(state_dir: PathBuf) -> Result<Self> {
        let paths = NucleusPaths::new(state_dir);
        paths.ensure_layout()?;
        let instance_lock = NucleusInstanceLock::acquire(&paths)?;
        let recovered_events = load_canonical_events_for_startup_recovery(&paths.events_path)?;
        let store = Self {
            paths,
            next_event_seq: AtomicU64::new(recovered_events.last_seq),
            write_lock: Mutex::new(()),
            _instance_lock: instance_lock,
        };
        let mut warnings = recovered_events.warnings;
        warnings.extend(store.rebuild_markdown_projections()?);
        warnings.extend(store.scan_persisted_state_health()?);
        for warning in warnings {
            store.append_system_warning(startup_recovery_warning_message(&warning), warning)?;
        }
        Ok(store)
    }

    pub fn paths(&self) -> &NucleusPaths {
        &self.paths
    }

    pub fn next_event_seq(&self) -> u64 {
        self.next_event_seq.load(Ordering::SeqCst) + 1
    }

    pub fn task_count(&self) -> Result<usize> {
        Ok(self.list_tasks()?.len())
    }

    pub fn worker_count(&self) -> Result<usize> {
        Ok(self.list_workers()?.len())
    }

    pub fn session_count(&self) -> Result<usize> {
        Ok(self.list_sessions()?.len())
    }

    pub fn run_count(&self) -> Result<usize> {
        Ok(self.list_runs()?.len())
    }

    fn write_worker_projection_locked(
        &self,
        worker: &WorkerRecord,
        runtime: Option<&WorkerRuntimeView>,
    ) -> Result<()> {
        write_json_atomic(&self.paths.worker_path(&worker.worker_id), worker)?;
        if let Some(runtime) = runtime {
            write_json_atomic(&self.paths.worker_runtime_path(&worker.worker_id), runtime)?;
        }
        let runtime = match runtime {
            Some(runtime) => Some(runtime.clone()),
            None => self.read_worker_runtime_locked(&worker.worker_id)?,
        };
        let summary = render_worker_summary(worker, runtime.as_ref());
        write_markdown_atomic(&self.paths.worker_summary_path(&worker.worker_id), &summary)
    }

    fn write_session_projection_locked(&self, session: &SessionRecord) -> Result<()> {
        write_json_atomic(&self.paths.session_path(&session.session_id), session)?;
        let summary = render_session_summary(session);
        write_markdown_atomic(&self.paths.session_summary_path(&session.session_id), &summary)
    }

    fn write_run_projection_locked(
        &self,
        run: &RunRecord,
        task: Option<&TaskRecord>,
    ) -> Result<()> {
        write_json_atomic(&self.paths.run_path(&run.run_id), run)?;
        let owned_task = match task {
            Some(task) => Some(task.clone()),
            None => self.read_task_locked(&run.task_id)?,
        };
        let summary = render_run_summary(run, owned_task.as_ref());
        write_markdown_atomic(&self.paths.run_summary_path(&run.run_id), &summary)
    }

    fn rebuild_markdown_projections(&self) -> Result<Vec<Value>> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        let mut warnings = Vec::new();
        self.rebuild_worker_markdown_locked(&mut warnings)?;
        self.rebuild_session_markdown_locked(&mut warnings)?;
        self.rebuild_run_markdown_locked(&mut warnings)?;
        Ok(warnings)
    }

    fn rebuild_worker_markdown_locked(&self, warnings: &mut Vec<Value>) -> Result<()> {
        for entry in fs::read_dir(&self.paths.workers_state_dir)
            .with_context(|| format!("read {}", self.paths.workers_state_dir.display()))?
        {
            let entry = entry?;
            let worker_path = entry.path().join("state.json");
            if !worker_path.exists() {
                continue;
            }
            let worker = match read_json_path::<WorkerRecord>(&worker_path) {
                Ok(worker) => worker,
                Err(error) => {
                    warnings.push(malformed_state_warning(
                        "worker",
                        &worker_path,
                        &error.to_string(),
                    ));
                    continue;
                }
            };
            let runtime_path = entry.path().join("runtime.json");
            let runtime = if runtime_path.exists() {
                match read_json_path::<WorkerRuntimeView>(&runtime_path) {
                    Ok(runtime) => Some(runtime),
                    Err(error) => {
                        warnings.push(malformed_state_warning(
                            "worker_runtime",
                            &runtime_path,
                            &error.to_string(),
                        ));
                        None
                    }
                }
            } else {
                None
            };
            let summary = render_worker_summary(&worker, runtime.as_ref());
            write_markdown_atomic(&self.paths.worker_summary_path(&worker.worker_id), &summary)?;
        }
        Ok(())
    }

    fn rebuild_session_markdown_locked(&self, warnings: &mut Vec<Value>) -> Result<()> {
        for entry in fs::read_dir(&self.paths.sessions_state_dir)
            .with_context(|| format!("read {}", self.paths.sessions_state_dir.display()))?
        {
            let entry = entry?;
            let session_path = entry.path().join("session.json");
            if !session_path.exists() {
                continue;
            }
            let session = match read_json_path::<SessionRecord>(&session_path) {
                Ok(session) => session,
                Err(error) => {
                    warnings.push(malformed_state_warning(
                        "session",
                        &session_path,
                        &error.to_string(),
                    ));
                    continue;
                }
            };
            let summary = render_session_summary(&session);
            write_markdown_atomic(&self.paths.session_summary_path(&session.session_id), &summary)?;
        }
        Ok(())
    }

    fn rebuild_run_markdown_locked(&self, warnings: &mut Vec<Value>) -> Result<()> {
        for entry in fs::read_dir(&self.paths.runs_state_dir)
            .with_context(|| format!("read {}", self.paths.runs_state_dir.display()))?
        {
            let entry = entry?;
            let run_path = entry.path().join("run.json");
            if !run_path.exists() {
                continue;
            }
            let run = match read_json_path::<RunRecord>(&run_path) {
                Ok(run) => run,
                Err(error) => {
                    warnings.push(malformed_state_warning("run", &run_path, &error.to_string()));
                    continue;
                }
            };
            let task_path = self.paths.task_path(&run.task_id);
            let task = if task_path.exists() {
                match read_json_path::<TaskRecord>(&task_path) {
                    Ok(task) => Some(task),
                    Err(error) => {
                        warnings.push(malformed_state_warning(
                            "task",
                            &task_path,
                            &error.to_string(),
                        ));
                        None
                    }
                }
            } else {
                None
            };
            let summary = render_run_summary(&run, task.as_ref());
            write_markdown_atomic(&self.paths.run_summary_path(&run.run_id), &summary)?;
        }
        Ok(())
    }

    fn scan_persisted_state_health(&self) -> Result<Vec<Value>> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        let mut warnings = Vec::new();
        self.scan_tasks_locked(&mut warnings)?;
        self.scan_profiles_locked(&mut warnings)?;
        self.scan_cron_rules_locked(&mut warnings)?;
        self.scan_hook_rules_locked(&mut warnings)?;
        Ok(warnings)
    }

    fn scan_tasks_locked(&self, warnings: &mut Vec<Value>) -> Result<()> {
        for entry in fs::read_dir(&self.paths.tasks_state_dir)
            .with_context(|| format!("read {}", self.paths.tasks_state_dir.display()))?
        {
            let entry = entry?;
            let path = entry.path().join("task.json");
            if !path.exists() {
                continue;
            }
            if let Err(error) = read_json_path::<TaskRecord>(&path) {
                warnings.push(malformed_state_warning("task", &path, &error.to_string()));
            }
        }
        Ok(())
    }

    fn scan_profiles_locked(&self, warnings: &mut Vec<Value>) -> Result<()> {
        for entry in fs::read_dir(&self.paths.profiles_state_dir)
            .with_context(|| format!("read {}", self.paths.profiles_state_dir.display()))?
        {
            let entry = entry?;
            let path = entry.path();
            if !path.is_file() {
                continue;
            }
            if let Err(error) = read_json_path::<ProfileRecord>(&path) {
                warnings.push(malformed_state_warning("profile", &path, &error.to_string()));
            }
        }
        Ok(())
    }

    fn scan_cron_rules_locked(&self, warnings: &mut Vec<Value>) -> Result<()> {
        for entry in fs::read_dir(&self.paths.cron_state_dir)
            .with_context(|| format!("read {}", self.paths.cron_state_dir.display()))?
        {
            let entry = entry?;
            let path = entry.path();
            if !path.is_file() {
                continue;
            }
            if let Err(error) = read_json_path::<CronRuleRecord>(&path) {
                warnings.push(malformed_state_warning("cron_rule", &path, &error.to_string()));
            }
        }
        Ok(())
    }

    fn scan_hook_rules_locked(&self, warnings: &mut Vec<Value>) -> Result<()> {
        for entry in fs::read_dir(&self.paths.hook_state_dir)
            .with_context(|| format!("read {}", self.paths.hook_state_dir.display()))?
        {
            let entry = entry?;
            let path = entry.path();
            if !path.is_file() {
                continue;
            }
            if let Err(error) = read_json_path::<HookRuleRecord>(&path) {
                warnings.push(malformed_state_warning("hook_rule", &path, &error.to_string()));
            }
        }
        Ok(())
    }

    fn create_task(&self, input: CreateTaskInput) -> Result<Vec<CanonicalEvent>> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        let mut task =
            TaskRecord::new(TaskId::generate(), input.source, input.title, input.instructions);
        task.profile = input.profile;
        task.session_id = input.session_id;
        task.session_binding_locked = task.session_id.is_some();
        if let Some(max_retries) = input.max_retries {
            task.max_retries = Some(max_retries);
        }
        if let Some(timeout_seconds) = input.timeout_seconds {
            task.timeout_seconds = Some(timeout_seconds);
        }
        let task_path = self.paths.task_path(&task.task_id);
        write_json_atomic(&task_path, &task)?;
        let event = self.append_event_locked(
            CanonicalEventType::TaskCreated,
            source_from_task_source(task.source),
            EventDataEnvelope {
                task_id: Some(task.task_id.clone()),
                worker_id: None,
                session_id: task.session_id.clone(),
                run_id: None,
                profile: task.profile.clone(),
                payload: json!({
                    "title": task.title,
                    "status": task.status,
                }),
            },
        )?;
        Ok(vec![event])
    }

    fn prune_tasks_older_than(&self, cutoff: DateTime<Utc>) -> Result<TaskPruneOutcome> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        let mut pruned_task_ids = Vec::new();
        let mut skipped = Vec::new();
        for entry in fs::read_dir(&self.paths.tasks_state_dir)
            .with_context(|| format!("read {}", self.paths.tasks_state_dir.display()))?
        {
            let entry = entry?;
            let path = entry.path().join("task.json");
            if !path.exists() {
                continue;
            }
            let task = match read_json_path::<TaskRecord>(&path) {
                Ok(task) => task,
                Err(error) => {
                    skipped.push(TaskPruneSkipView {
                        path: path.display().to_string(),
                        error: error.to_string(),
                    });
                    continue;
                }
            };
            if !is_prunable_task_status(task.status) || task.updated_at > cutoff {
                continue;
            }
            fs::remove_dir_all(entry.path())
                .with_context(|| format!("remove {}", entry.path().display()))?;
            pruned_task_ids.push(task.task_id);
        }
        pruned_task_ids.sort();
        Ok(TaskPruneOutcome { pruned_task_ids, skipped })
    }

    fn create_producer_task(
        &self,
        input: CreateTaskInput,
        producer_rule_name: &str,
        producer_dedup_key: &str,
    ) -> Result<Option<(TaskRecord, Vec<CanonicalEvent>)>> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        if self
            .find_task_by_producer_dedup_locked(
                input.source,
                producer_rule_name,
                producer_dedup_key,
            )?
            .is_some()
        {
            return Ok(None);
        }

        let mut task =
            TaskRecord::new(TaskId::generate(), input.source, input.title, input.instructions);
        task.profile = input.profile;
        task.session_id = input.session_id;
        task.session_binding_locked = task.session_id.is_some();
        task.producer_rule_name = Some(producer_rule_name.to_owned());
        task.producer_dedup_key = Some(producer_dedup_key.to_owned());
        if let Some(max_retries) = input.max_retries {
            task.max_retries = Some(max_retries);
        }
        if let Some(timeout_seconds) = input.timeout_seconds {
            task.timeout_seconds = Some(timeout_seconds);
        }
        let task_path = self.paths.task_path(&task.task_id);
        write_json_atomic(&task_path, &task)?;
        let event = self.append_event_locked(
            CanonicalEventType::TaskCreated,
            source_from_task_source(task.source),
            EventDataEnvelope {
                task_id: Some(task.task_id.clone()),
                worker_id: None,
                session_id: task.session_id.clone(),
                run_id: None,
                profile: task.profile.clone(),
                payload: json!({
                    "title": task.title,
                    "status": task.status,
                    "producer_rule_name": task.producer_rule_name,
                    "producer_dedup_key": task.producer_dedup_key,
                }),
            },
        )?;
        Ok(Some((task, vec![event])))
    }

    pub fn list_tasks(&self) -> Result<Vec<TaskRecord>> {
        let mut tasks = load_json_records_from_child_dirs::<TaskRecord>(
            &self.paths.tasks_state_dir,
            "task.json",
        )?;
        tasks.sort_by(|left, right| left.created_at.cmp(&right.created_at));
        Ok(tasks)
    }

    pub fn inspect_task(&self, task_id: &TaskId) -> Result<Option<TaskRecord>> {
        let path = self.paths.task_path(task_id);
        if !path.exists() {
            return Ok(None);
        }
        let bytes = fs::read(&path).with_context(|| format!("read {}", path.display()))?;
        let task = serde_json::from_slice::<TaskRecord>(&bytes)
            .with_context(|| format!("parse {}", path.display()))?;
        Ok(Some(task))
    }

    pub fn list_workers(&self) -> Result<Vec<WorkerRecord>> {
        let mut workers = load_json_records_from_child_dirs::<WorkerRecord>(
            &self.paths.workers_state_dir,
            "state.json",
        )?;
        workers.sort_by(|left, right| left.worker_id.cmp(&right.worker_id));
        Ok(workers)
    }

    pub fn inspect_worker(&self, worker_id: &WorkerId) -> Result<Option<WorkerRecord>> {
        let path = self.paths.worker_path(worker_id);
        if !path.exists() {
            return Ok(None);
        }
        let bytes = fs::read(&path).with_context(|| format!("read {}", path.display()))?;
        let worker = serde_json::from_slice::<WorkerRecord>(&bytes)
            .with_context(|| format!("parse {}", path.display()))?;
        Ok(Some(worker))
    }

    pub fn inspect_worker_runtime(
        &self,
        worker_id: &WorkerId,
    ) -> Result<Option<WorkerRuntimeView>> {
        let path = self.paths.worker_runtime_path(worker_id);
        if !path.exists() {
            return Ok(None);
        }
        let bytes = fs::read(&path).with_context(|| format!("read {}", path.display()))?;
        let runtime = serde_json::from_slice::<WorkerRuntimeView>(&bytes)
            .with_context(|| format!("parse {}", path.display()))?;
        Ok(Some(runtime))
    }

    pub fn list_sessions(&self) -> Result<Vec<SessionRecord>> {
        let mut sessions = load_json_records_from_child_dirs::<SessionRecord>(
            &self.paths.sessions_state_dir,
            "session.json",
        )?;
        sessions.sort_by(|left, right| left.created_at.cmp(&right.created_at));
        Ok(sessions)
    }

    pub fn inspect_session(&self, session_id: &SessionId) -> Result<Option<SessionRecord>> {
        let path = self.paths.session_path(session_id);
        if !path.exists() {
            return Ok(None);
        }
        let bytes = fs::read(&path).with_context(|| format!("read {}", path.display()))?;
        let session = serde_json::from_slice::<SessionRecord>(&bytes)
            .with_context(|| format!("parse {}", path.display()))?;
        Ok(Some(session))
    }

    pub fn list_runs(&self) -> Result<Vec<RunRecord>> {
        let mut runs =
            load_json_records_from_child_dirs::<RunRecord>(&self.paths.runs_state_dir, "run.json")?;
        runs.sort_by(|left, right| {
            left.started_at.cmp(&right.started_at).then(left.run_id.cmp(&right.run_id))
        });
        Ok(runs)
    }

    pub fn inspect_run(&self, run_id: &RunId) -> Result<Option<RunRecord>> {
        let path = self.paths.run_path(run_id);
        if !path.exists() {
            return Ok(None);
        }
        let bytes = fs::read(&path).with_context(|| format!("read {}", path.display()))?;
        let run = serde_json::from_slice::<RunRecord>(&bytes)
            .with_context(|| format!("parse {}", path.display()))?;
        Ok(Some(run))
    }

    pub fn list_profiles(&self) -> Result<Vec<Value>> {
        let profiles = load_json_records_from_dir::<Value>(&self.paths.profiles_state_dir)?;
        Ok(profiles)
    }

    fn list_hook_rules(&self) -> Result<Vec<HookRuleRecord>> {
        let mut rules = load_json_records_from_dir::<HookRuleRecord>(&self.paths.hook_state_dir)?;
        rules.sort_by(|left, right| left.name.cmp(&right.name));
        Ok(rules)
    }

    fn inspect_hook_rule(&self, rule_name: &str) -> Result<Option<HookRuleRecord>> {
        let path = self.paths.hook_rule_path(rule_name);
        if !path.exists() {
            return Ok(None);
        }
        let bytes = fs::read(&path).with_context(|| format!("read {}", path.display()))?;
        let rule = serde_json::from_slice::<HookRuleRecord>(&bytes)
            .with_context(|| format!("parse {}", path.display()))?;
        Ok(Some(rule))
    }

    fn upsert_hook_rule(&self, rule: &HookRuleRecord) -> Result<HookRuleRecord> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        write_json_atomic(&self.paths.hook_rule_path(&rule.name), rule)?;
        Ok(rule.clone())
    }

    fn delete_hook_rule(&self, rule_name: &str) -> Result<bool> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        let path = self.paths.hook_rule_path(rule_name);
        if !path.exists() {
            return Ok(false);
        }
        fs::remove_file(&path).with_context(|| format!("remove {}", path.display()))?;
        Ok(true)
    }

    fn persist_hook_rule_progress(&self, progress: &HookRuleRecord) -> Result<bool> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        let path = self.paths.hook_rule_path(&progress.name);
        if !path.exists() {
            return Ok(false);
        }
        let bytes = fs::read(&path).with_context(|| format!("read {}", path.display()))?;
        let mut current = serde_json::from_slice::<HookRuleRecord>(&bytes)
            .with_context(|| format!("parse {}", path.display()))?;
        current.last_processed_event_seq =
            current.last_processed_event_seq.max(progress.last_processed_event_seq);
        current.version = current_persisted_version().to_owned();
        write_json_atomic(&path, &current)?;
        Ok(true)
    }

    pub fn list_profile_records(&self) -> Result<Vec<ProfileRecord>> {
        let mut profiles =
            load_json_records_from_dir::<ProfileRecord>(&self.paths.profiles_state_dir)?;
        profiles.sort_by(|left, right| left.profile.cmp(&right.profile));
        Ok(profiles)
    }

    fn append_event_locked(
        &self,
        event_type: CanonicalEventType,
        source: CanonicalEventSource,
        data: EventDataEnvelope,
    ) -> Result<CanonicalEvent> {
        let seq = self.next_event_seq.fetch_add(1, Ordering::SeqCst) + 1;
        let event = CanonicalEvent {
            event_id: EventId::generate(),
            seq,
            ts: Utc::now(),
            event_type,
            source,
            data,
        };
        append_jsonl(&self.paths.events_path, &event)?;
        Ok(event)
    }

    fn append_event_draft_locked(&self, draft: CanonicalEventDraft) -> Result<CanonicalEvent> {
        self.append_event_locked(draft.event_type, draft.source, draft.data)
    }

    fn record_worker_probe(
        &self,
        spec: &WorkerLaunchSpec,
        probe: &WorkerProbeResult,
        runtime: &dyn NucleusRuntime,
    ) -> Result<Vec<CanonicalEvent>> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        let payload = runtime.status_payload(probe);
        let worker = WorkerRecord {
            worker_id: spec.worker_id.clone(),
            profile: spec.profile.clone(),
            home_dir: Some(spec.home_dir.display().to_string()),
            codex_home: spec.codex_home.display().to_string(),
            workdir: Some(spec.workdir.display().to_string()),
            extra_env: spec.extra_env.clone(),
            status: probe.status,
            capability_version: payload.get("model").and_then(Value::as_str).map(ToOwned::to_owned),
            last_heartbeat_at: Some(probe.checked_at),
            effective_account_state: payload
                .get("account_email")
                .and_then(Value::as_str)
                .map(ToOwned::to_owned)
                .or_else(|| Some(payload.to_string())),
        };
        self.write_worker_projection_locked(&worker, None)?;
        let mut events = Vec::new();
        let profile = ProfileRecord {
            profile: spec.profile.clone(),
            account_identity: payload
                .get("account_email")
                .and_then(Value::as_str)
                .map(ToOwned::to_owned),
            codex_home: spec.codex_home.display().to_string(),
            auth_mode: None,
            preferred_model: payload.get("model").and_then(Value::as_str).map(ToOwned::to_owned),
            runtime_defaults: BTreeMap::new(),
        };
        if let Some(event) = self.persist_profile_locked(profile, CanonicalEventSource::System)? {
            events.push(event);
        }
        for draft in runtime.probe_events(spec, probe)? {
            events.push(self.append_event_draft_locked(draft)?);
        }
        Ok(events)
    }

    fn record_worker_start(
        &self,
        spec: &WorkerLaunchSpec,
        started: &WorkerStartResult,
        runtime: &dyn NucleusRuntime,
    ) -> Result<(WorkerRecord, WorkerRuntimeView, Vec<CanonicalEvent>)> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        let mut events = Vec::new();
        events.push(self.append_event_locked(
            CanonicalEventType::WorkerStarting,
            CanonicalEventSource::Nucleus,
            EventDataEnvelope {
                task_id: None,
                worker_id: Some(spec.worker_id.clone()),
                session_id: None,
                run_id: None,
                profile: Some(spec.profile.clone()),
                payload: json!({
                    "runtime": started.runtime.runtime_name,
                    "pid": started.runtime.pid,
                }),
            },
        )?);
        let payload = runtime.status_payload(&started.probe);
        let worker = WorkerRecord {
            worker_id: spec.worker_id.clone(),
            profile: spec.profile.clone(),
            home_dir: Some(spec.home_dir.display().to_string()),
            codex_home: spec.codex_home.display().to_string(),
            workdir: Some(spec.workdir.display().to_string()),
            extra_env: spec.extra_env.clone(),
            status: started.probe.status,
            capability_version: payload.get("model").and_then(Value::as_str).map(ToOwned::to_owned),
            last_heartbeat_at: Some(started.probe.checked_at),
            effective_account_state: payload
                .get("account_email")
                .and_then(Value::as_str)
                .map(ToOwned::to_owned)
                .or_else(|| Some(payload.to_string())),
        };
        self.write_worker_projection_locked(&worker, Some(&started.runtime))?;
        let profile = ProfileRecord {
            profile: spec.profile.clone(),
            account_identity: payload
                .get("account_email")
                .and_then(Value::as_str)
                .map(ToOwned::to_owned),
            codex_home: spec.codex_home.display().to_string(),
            auth_mode: None,
            preferred_model: payload.get("model").and_then(Value::as_str).map(ToOwned::to_owned),
            runtime_defaults: BTreeMap::new(),
        };
        if let Some(event) = self.persist_profile_locked(profile, CanonicalEventSource::System)? {
            events.push(event);
        }
        for draft in runtime.probe_events(spec, &started.probe)? {
            events.push(self.append_event_draft_locked(draft)?);
        }
        Ok((worker, started.runtime.clone(), events))
    }

    fn record_session_open(
        &self,
        session_id: SessionId,
        worker_id: WorkerId,
        thread_id: String,
        created: bool,
        profile: ProfileName,
        workdir: PathBuf,
    ) -> Result<(SessionRecord, CanonicalEvent)> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        let mut session = SessionRecord::new(session_id.clone(), worker_id.clone());
        session.profile = Some(profile.clone());
        session.app_server_thread_id = Some(thread_id.clone());
        session.workdir = Some(workdir.display().to_string());
        session.transition_to(SessionLifecycleState::Ready).map_err(anyhow::Error::new)?;
        self.write_session_projection_locked(&session)?;
        let event = self.append_event_locked(
            if created {
                CanonicalEventType::SessionCreated
            } else {
                CanonicalEventType::SessionReused
            },
            CanonicalEventSource::AppServer,
            EventDataEnvelope {
                task_id: None,
                worker_id: Some(worker_id),
                session_id: Some(session_id),
                run_id: None,
                profile: Some(profile),
                payload: json!({
                    "thread_id": thread_id,
                }),
            },
        )?;
        Ok((session, event))
    }

    fn claim_run_for_task(&self, run: RunRecord) -> Result<RunRecord> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        let mut task = self
            .read_task_locked(&run.task_id)?
            .ok_or_else(|| anyhow!("task not found while claiming run"))?;
        if task.status != TaskStatus::Queued {
            anyhow::bail!("task is not queued");
        }
        if let Some(latest_run_id) = task.latest_run_id.as_ref() {
            if let Some(latest_run) = self.read_run_locked(latest_run_id)? {
                if is_active_run_status(latest_run.status) {
                    anyhow::bail!("task already has an active run");
                }
            }
        }
        self.write_run_projection_locked(&run, Some(&task))?;
        if task.session_id.is_none() {
            task.session_id = Some(run.session_id.clone());
        }
        task.attempt_count = task.attempt_count.saturating_add(1);
        task.latest_run_id = Some(run.run_id.clone());
        task.updated_at = Utc::now();
        write_json_atomic(&self.paths.task_path(&run.task_id), &task)?;
        Ok(run)
    }

    fn apply_runtime_event(&self, draft: CanonicalEventDraft) -> Result<Vec<CanonicalEvent>> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        let primary = self.append_event_draft_locked(draft)?;
        let mut events = vec![primary.clone()];

        match primary.event_type {
            CanonicalEventType::RunStarted => {
                let Some(run_id) = primary.data.run_id.clone() else {
                    return Ok(events);
                };
                let turn_id = primary
                    .data
                    .payload
                    .get("turn_id")
                    .and_then(Value::as_str)
                    .map(ToOwned::to_owned);
                if let Some(mut run) = self.read_run_locked(&run_id)? {
                    if run.status == RunStatus::Queued {
                        run.transition_to(RunStatus::Running).map_err(anyhow::Error::new)?;
                    }
                    run.app_server_turn_id = turn_id;
                    self.write_run_projection_locked(&run, None)?;
                }
                if let Some(session_id) = primary.data.session_id.clone() {
                    if let Some(mut session) = self.read_session_locked(&session_id)? {
                        if session.lifecycle_state == SessionLifecycleState::Ready {
                            session
                                .transition_to(SessionLifecycleState::Busy)
                                .map_err(anyhow::Error::new)?;
                            self.write_session_projection_locked(&session)?;
                        }
                    }
                }
                if let Some(task_id) = primary.data.task_id.clone() {
                    if let Some(mut task) = self.read_task_locked(&task_id)? {
                        if matches!(task.status, TaskStatus::Queued | TaskStatus::Blocked) {
                            task.transition_to(TaskStatus::Running, None)
                                .map_err(anyhow::Error::new)?;
                            write_json_atomic(&self.paths.task_path(&task_id), &task)?;
                            events.push(self.append_event_locked(
                                CanonicalEventType::TaskUpdated,
                                CanonicalEventSource::Nucleus,
                                EventDataEnvelope {
                                    task_id: Some(task_id),
                                    worker_id: primary.data.worker_id.clone(),
                                    session_id: primary.data.session_id.clone(),
                                    run_id: Some(run_id),
                                    profile: primary.data.profile.clone(),
                                    payload: json!({
                                        "status": task.status,
                                    }),
                                },
                            )?);
                        }
                    }
                }
            }
            CanonicalEventType::RunOutputDelta => {
                if let Some(task_id) = primary.data.task_id.clone() {
                    if let Some(mut task) = self.read_task_locked(&task_id)? {
                        if let Some(delta) =
                            primary.data.payload.get("delta").and_then(Value::as_str)
                        {
                            task.checkpoint_summary = Some(delta.to_owned());
                            task.checkpoint_at = Some(primary.ts);
                            task.checkpoint_seq = Some(primary.seq);
                            task.updated_at = Utc::now();
                            write_json_atomic(&self.paths.task_path(&task_id), &task)?;
                            if let Some(session_id) = primary.data.session_id.clone() {
                                if let Some(mut session) = self.read_session_locked(&session_id)? {
                                    session.summary_state = Some(delta.to_owned());
                                    session.updated_at = Utc::now();
                                    self.write_session_projection_locked(&session)?;
                                }
                            }
                            if let Some(run_id) = primary.data.run_id.clone() {
                                if let Some(run) = self.read_run_locked(&run_id)? {
                                    self.write_run_projection_locked(&run, Some(&task))?;
                                }
                            }
                        }
                    }
                }
            }
            CanonicalEventType::RunCompleted => {
                self.finish_run_locked(
                    &primary,
                    RunStatus::Completed,
                    None,
                    TaskStatus::Done,
                    None,
                    &mut events,
                )?;
            }
            CanonicalEventType::RunFailed => {
                let retry_plan = self.task_retry_plan_for_run_failure_locked(&primary)?;
                self.finish_run_locked(
                    &primary,
                    RunStatus::Failed,
                    None,
                    TaskStatus::Failed,
                    None,
                    &mut events,
                )?;
                self.apply_run_failure_consequences_locked(&primary, &mut events)?;
                if let Some(retry_plan) = retry_plan {
                    if let Some(task_id) = primary.data.task_id.as_ref() {
                        if let Some(event) = self.retry_failed_task_locked(task_id, &retry_plan)? {
                            events.push(event);
                        }
                    }
                }
            }
            CanonicalEventType::RunCancelled => {
                self.finish_run_locked(
                    &primary,
                    RunStatus::Cancelled,
                    None,
                    TaskStatus::Cancelled,
                    None,
                    &mut events,
                )?;
            }
            CanonicalEventType::RunRequiresAuth => {
                self.finish_run_locked(
                    &primary,
                    RunStatus::Blocked,
                    Some(BlockedReason::AuthRequired),
                    TaskStatus::Blocked,
                    Some(CanonicalEventType::TaskBlocked),
                    &mut events,
                )?;
            }
            CanonicalEventType::RunBlocked => {
                self.finish_run_locked(
                    &primary,
                    RunStatus::Blocked,
                    blocked_reason_from_payload(&primary.data.payload)
                        .unwrap_or(BlockedReason::WorkerUnavailable)
                        .into(),
                    TaskStatus::Blocked,
                    Some(CanonicalEventType::TaskBlocked),
                    &mut events,
                )?;
            }
            _ => {}
        }

        Ok(events)
    }

    fn apply_run_failure_consequences_locked(
        &self,
        event: &CanonicalEvent,
        events: &mut Vec<CanonicalEvent>,
    ) -> Result<()> {
        let Some(error) = event
            .data
            .payload
            .get("error")
            .and_then(Value::as_str)
            .map(str::trim)
            .filter(|value| !value.is_empty())
        else {
            return Ok(());
        };

        if run_failure_requires_session_quarantine(error) {
            if let Some(session_id) = event.data.session_id.as_ref() {
                if let Some(appended) = self.mark_session_broken_locked(session_id, error)? {
                    events.push(appended);
                }
            }
        }
        if runtime_error_requires_worker_quarantine(error) {
            if let Some(worker_id) = event.data.worker_id.as_ref() {
                if let Some(appended) = self.mark_worker_failed_locked(worker_id, error)? {
                    events.push(appended);
                }
            }
        }
        Ok(())
    }

    fn task_retry_plan_for_run_failure_locked(
        &self,
        event: &CanonicalEvent,
    ) -> Result<Option<TaskRetryPlan>> {
        let Some(task_id) = event.data.task_id.as_ref() else {
            return Ok(None);
        };
        let Some(task) = self.read_task_locked(task_id)? else {
            return Ok(None);
        };
        let Some(max_retries) = task.max_retries else {
            return Ok(None);
        };
        if task.attempt_count == 0 || task.attempt_count > max_retries {
            return Ok(None);
        }
        let failure_breaks_session = event
            .data
            .payload
            .get("error")
            .and_then(Value::as_str)
            .map(str::trim)
            .filter(|value| !value.is_empty())
            .is_some_and(run_failure_requires_session_quarantine);
        if failure_breaks_session && task.session_binding_locked {
            return Ok(None);
        }
        Ok(Some(TaskRetryPlan {
            next_session_id: if failure_breaks_session { None } else { task.session_id.clone() },
            attempt_count: task.attempt_count,
            max_retries,
        }))
    }

    fn retry_failed_task_locked(
        &self,
        task_id: &TaskId,
        retry_plan: &TaskRetryPlan,
    ) -> Result<Option<CanonicalEvent>> {
        let Some(mut task) = self.read_task_locked(task_id)? else {
            return Ok(None);
        };
        if task.status != TaskStatus::Failed {
            return Ok(None);
        }
        task.session_id = retry_plan.next_session_id.clone();
        task.transition_to(TaskStatus::Queued, None).map_err(anyhow::Error::new)?;
        write_json_atomic(&self.paths.task_path(task_id), &task)?;
        let event = self.append_event_locked(
            CanonicalEventType::TaskUpdated,
            CanonicalEventSource::Nucleus,
            EventDataEnvelope {
                task_id: Some(task.task_id.clone()),
                worker_id: None,
                session_id: task.session_id.clone(),
                run_id: task.latest_run_id.clone(),
                profile: task.profile.clone(),
                payload: json!({
                    "status": task.status,
                    "blocked_reason": task.blocked_reason,
                    "message": format!(
                        "retrying task after failed attempt {}/{}",
                        retry_plan.attempt_count,
                        retry_plan.max_retries + 1
                    ),
                }),
            },
        )?;
        Ok(Some(event))
    }

    fn finish_run_locked(
        &self,
        event: &CanonicalEvent,
        run_status: RunStatus,
        blocked_reason: Option<BlockedReason>,
        task_status: TaskStatus,
        task_event_type: Option<CanonicalEventType>,
        events: &mut Vec<CanonicalEvent>,
    ) -> Result<()> {
        let mut refreshed_run = None;
        if let Some(run_id) = event.data.run_id.clone() {
            if let Some(mut run) = self.read_run_locked(&run_id)? {
                if run.status != run_status {
                    run.transition_to(run_status).map_err(anyhow::Error::new)?;
                }
                self.write_run_projection_locked(&run, None)?;
                refreshed_run = Some(run);
            }
        }
        if let Some(session_id) = event.data.session_id.clone() {
            if let Some(mut session) = self.read_session_locked(&session_id)? {
                if let Some(summary) = event
                    .data
                    .payload
                    .get("final_output")
                    .and_then(Value::as_str)
                    .map(str::trim)
                    .filter(|value| !value.is_empty())
                {
                    session.summary_state = Some(summary.to_owned());
                    session.updated_at = Utc::now();
                }
                if session.lifecycle_state == SessionLifecycleState::Busy {
                    session
                        .transition_to(SessionLifecycleState::Ready)
                        .map_err(anyhow::Error::new)?;
                }
                self.write_session_projection_locked(&session)?;
            }
        }
        if let Some(task_id) = event.data.task_id.clone() {
            if let Some(mut task) = self.read_task_locked(&task_id)? {
                if task.status != task_status {
                    task.transition_to(task_status, blocked_reason).map_err(anyhow::Error::new)?;
                }
                if let Some(summary) = event
                    .data
                    .payload
                    .get("final_output")
                    .and_then(Value::as_str)
                    .map(str::trim)
                    .filter(|value| !value.is_empty())
                {
                    task.checkpoint_summary = Some(summary.to_owned());
                    task.checkpoint_at = Some(event.ts);
                    task.checkpoint_seq = Some(event.seq);
                }
                write_json_atomic(&self.paths.task_path(&task_id), &task)?;
                if let Some(run) = refreshed_run.as_ref() {
                    self.write_run_projection_locked(run, Some(&task))?;
                }
                events.push(self.append_event_locked(
                    task_event_type.unwrap_or(CanonicalEventType::TaskUpdated),
                    CanonicalEventSource::Nucleus,
                    EventDataEnvelope {
                        task_id: Some(task_id),
                        worker_id: event.data.worker_id.clone(),
                        session_id: event.data.session_id.clone(),
                        run_id: event.data.run_id.clone(),
                        profile: event.data.profile.clone(),
                        payload: json!({
                            "status": task.status,
                            "blocked_reason": task.blocked_reason,
                        }),
                    },
                )?);
            }
        }
        Ok(())
    }

    fn read_task_locked(&self, task_id: &TaskId) -> Result<Option<TaskRecord>> {
        let path = self.paths.task_path(task_id);
        if !path.exists() {
            return Ok(None);
        }
        let bytes = fs::read(&path).with_context(|| format!("read {}", path.display()))?;
        let task = serde_json::from_slice::<TaskRecord>(&bytes)
            .with_context(|| format!("parse {}", path.display()))?;
        Ok(Some(task))
    }

    fn read_session_locked(&self, session_id: &SessionId) -> Result<Option<SessionRecord>> {
        let path = self.paths.session_path(session_id);
        if !path.exists() {
            return Ok(None);
        }
        let bytes = fs::read(&path).with_context(|| format!("read {}", path.display()))?;
        let session = serde_json::from_slice::<SessionRecord>(&bytes)
            .with_context(|| format!("parse {}", path.display()))?;
        Ok(Some(session))
    }

    fn read_run_locked(&self, run_id: &RunId) -> Result<Option<RunRecord>> {
        let path = self.paths.run_path(run_id);
        if !path.exists() {
            return Ok(None);
        }
        let bytes = fs::read(&path).with_context(|| format!("read {}", path.display()))?;
        let run = serde_json::from_slice::<RunRecord>(&bytes)
            .with_context(|| format!("parse {}", path.display()))?;
        Ok(Some(run))
    }

    fn read_worker_runtime_locked(
        &self,
        worker_id: &WorkerId,
    ) -> Result<Option<WorkerRuntimeView>> {
        let path = self.paths.worker_runtime_path(worker_id);
        if !path.exists() {
            return Ok(None);
        }
        let bytes = fs::read(&path).with_context(|| format!("read {}", path.display()))?;
        let runtime = serde_json::from_slice::<WorkerRuntimeView>(&bytes)
            .with_context(|| format!("parse {}", path.display()))?;
        Ok(Some(runtime))
    }

    fn find_task_by_producer_dedup_locked(
        &self,
        source: TaskSource,
        producer_rule_name: &str,
        producer_dedup_key: &str,
    ) -> Result<Option<TaskRecord>> {
        for task in load_json_records_from_child_dirs::<TaskRecord>(
            &self.paths.tasks_state_dir,
            "task.json",
        )? {
            if task.source == source
                && task.producer_rule_name.as_deref() == Some(producer_rule_name)
                && task.producer_dedup_key.as_deref() == Some(producer_dedup_key)
            {
                return Ok(Some(task));
            }
        }
        Ok(None)
    }

    fn read_worker_locked(&self, worker_id: &WorkerId) -> Result<Option<WorkerRecord>> {
        let path = self.paths.worker_path(worker_id);
        if !path.exists() {
            return Ok(None);
        }
        let bytes = fs::read(&path).with_context(|| format!("read {}", path.display()))?;
        let worker = serde_json::from_slice::<WorkerRecord>(&bytes)
            .with_context(|| format!("parse {}", path.display()))?;
        Ok(Some(worker))
    }

    fn persist_profile_locked(
        &self,
        profile: ProfileRecord,
        source: CanonicalEventSource,
    ) -> Result<Option<CanonicalEvent>> {
        let path = self.paths.profile_path(&profile.profile);
        let existing =
            if path.exists() { Some(read_json_path::<ProfileRecord>(&path)?) } else { None };
        if existing.as_ref() != Some(&profile) {
            write_json_atomic(&path, &profile)?;
        }
        let existed = existing.is_some();
        if existed {
            return Ok(None);
        }
        let event = self.append_event_locked(
            CanonicalEventType::ProfileLoaded,
            source,
            EventDataEnvelope {
                task_id: None,
                worker_id: None,
                session_id: None,
                run_id: None,
                profile: Some(profile.profile),
                payload: json!({
                    "codex_home": profile.codex_home,
                    "account_identity": profile.account_identity,
                    "auth_mode": profile.auth_mode,
                    "preferred_model": profile.preferred_model,
                }),
            },
        )?;
        Ok(Some(event))
    }

    fn append_system_warning(&self, message: &str, payload: Value) -> Result<CanonicalEvent> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        self.append_event_locked(
            CanonicalEventType::SystemWarning,
            CanonicalEventSource::System,
            EventDataEnvelope {
                task_id: None,
                worker_id: None,
                session_id: None,
                run_id: None,
                profile: None,
                payload: json!({
                    "message": message,
                    "details": payload,
                }),
            },
        )
    }

    fn append_aux_event(
        &self,
        event_type: CanonicalEventType,
        source: CanonicalEventSource,
        data: EventDataEnvelope,
    ) -> Result<CanonicalEvent> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        self.append_event_locked(event_type, source, data)
    }

    fn mark_worker_failed(
        &self,
        worker_id: &WorkerId,
        message: &str,
    ) -> Result<Option<CanonicalEvent>> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        self.mark_worker_failed_locked(worker_id, message)
    }

    fn mark_worker_failed_locked(
        &self,
        worker_id: &WorkerId,
        message: &str,
    ) -> Result<Option<CanonicalEvent>> {
        let Some(mut worker) = self.read_worker_locked(worker_id)? else {
            return Ok(None);
        };
        if worker.status != si_nucleus_core::WorkerStatus::Failed {
            worker
                .transition_to(si_nucleus_core::WorkerStatus::Failed)
                .map_err(anyhow::Error::new)?;
            self.write_worker_projection_locked(&worker, None)?;
        }
        let event = self.append_event_locked(
            CanonicalEventType::WorkerFailed,
            CanonicalEventSource::System,
            EventDataEnvelope {
                task_id: None,
                worker_id: Some(worker.worker_id.clone()),
                session_id: None,
                run_id: None,
                profile: Some(worker.profile.clone()),
                payload: json!({
                    "message": message,
                }),
            },
        )?;
        Ok(Some(event))
    }

    fn mark_session_broken(
        &self,
        session_id: &SessionId,
        message: &str,
    ) -> Result<Option<CanonicalEvent>> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        self.mark_session_broken_locked(session_id, message)
    }

    fn mark_session_broken_locked(
        &self,
        session_id: &SessionId,
        message: &str,
    ) -> Result<Option<CanonicalEvent>> {
        let Some(mut session) = self.read_session_locked(session_id)? else {
            return Ok(None);
        };
        if session.lifecycle_state != SessionLifecycleState::Broken {
            session.transition_to(SessionLifecycleState::Broken).map_err(anyhow::Error::new)?;
            self.write_session_projection_locked(&session)?;
        }
        let event = self.append_event_locked(
            CanonicalEventType::SessionBroken,
            CanonicalEventSource::System,
            EventDataEnvelope {
                task_id: None,
                worker_id: Some(session.worker_id.clone()),
                session_id: Some(session.session_id.clone()),
                run_id: None,
                profile: session.profile.clone(),
                payload: json!({
                    "message": message,
                }),
            },
        )?;
        Ok(Some(event))
    }

    fn mark_task_blocked(
        &self,
        task_id: &TaskId,
        worker_id: Option<WorkerId>,
        session_id: Option<SessionId>,
        profile: Option<ProfileName>,
        blocked_reason: BlockedReason,
        message: &str,
    ) -> Result<Option<CanonicalEvent>> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        let Some(mut task) = self.read_task_locked(task_id)? else {
            return Ok(None);
        };
        if task.status != TaskStatus::Blocked {
            task.transition_to(TaskStatus::Blocked, Some(blocked_reason))
                .map_err(anyhow::Error::new)?;
            write_json_atomic(&self.paths.task_path(task_id), &task)?;
        }
        let event = self.append_event_locked(
            CanonicalEventType::TaskBlocked,
            CanonicalEventSource::System,
            EventDataEnvelope {
                task_id: Some(task.task_id.clone()),
                worker_id,
                session_id,
                run_id: task.latest_run_id.clone(),
                profile,
                payload: json!({
                    "status": task.status,
                    "blocked_reason": task.blocked_reason,
                    "message": message,
                }),
            },
        )?;
        Ok(Some(event))
    }

    fn assign_task_profile(
        &self,
        task_id: &TaskId,
        profile: &ProfileName,
        message: &str,
    ) -> Result<Option<CanonicalEvent>> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        let Some(mut task) = self.read_task_locked(task_id)? else {
            return Ok(None);
        };
        if task.profile.as_ref() == Some(profile) {
            return Ok(None);
        }
        task.profile = Some(profile.clone());
        task.updated_at = Utc::now();
        write_json_atomic(&self.paths.task_path(task_id), &task)?;
        let event = self.append_event_locked(
            CanonicalEventType::TaskUpdated,
            CanonicalEventSource::Nucleus,
            EventDataEnvelope {
                task_id: Some(task.task_id.clone()),
                worker_id: None,
                session_id: task.session_id.clone(),
                run_id: task.latest_run_id.clone(),
                profile: Some(profile.clone()),
                payload: json!({
                    "status": task.status,
                    "blocked_reason": task.blocked_reason,
                    "message": message,
                }),
            },
        )?;
        Ok(Some(event))
    }

    fn cancel_task_without_run(
        &self,
        task_id: &TaskId,
        source: CanonicalEventSource,
        message: &str,
    ) -> Result<Option<(TaskRecord, CanonicalEvent)>> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        let Some(mut task) = self.read_task_locked(task_id)? else {
            return Ok(None);
        };
        if task.status == TaskStatus::Cancelled {
            return Ok(None);
        }
        if matches!(task.status, TaskStatus::Done | TaskStatus::Failed) {
            return Ok(None);
        }
        task.transition_to(TaskStatus::Cancelled, None).map_err(anyhow::Error::new)?;
        write_json_atomic(&self.paths.task_path(task_id), &task)?;
        let event = self.append_event_locked(
            CanonicalEventType::TaskUpdated,
            source,
            EventDataEnvelope {
                task_id: Some(task.task_id.clone()),
                worker_id: None,
                session_id: task.session_id.clone(),
                run_id: task.latest_run_id.clone(),
                profile: task.profile.clone(),
                payload: json!({
                    "status": task.status,
                    "message": message,
                }),
            },
        )?;
        Ok(Some((task, event)))
    }

    fn requeue_blocked_task(
        &self,
        task_id: &TaskId,
        profile: Option<ProfileName>,
        message: &str,
    ) -> Result<Option<CanonicalEvent>> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        let Some(mut task) = self.read_task_locked(task_id)? else {
            return Ok(None);
        };
        if task.status != TaskStatus::Blocked {
            return Ok(None);
        }
        if let Some(profile) = profile.clone() {
            task.profile = Some(profile);
        }
        task.transition_to(TaskStatus::Queued, None).map_err(anyhow::Error::new)?;
        write_json_atomic(&self.paths.task_path(task_id), &task)?;
        let event = self.append_event_locked(
            CanonicalEventType::TaskUpdated,
            CanonicalEventSource::Nucleus,
            EventDataEnvelope {
                task_id: Some(task.task_id.clone()),
                worker_id: None,
                session_id: task.session_id.clone(),
                run_id: task.latest_run_id.clone(),
                profile,
                payload: json!({
                    "status": task.status,
                    "blocked_reason": task.blocked_reason,
                    "message": message,
                }),
            },
        )?;
        Ok(Some(event))
    }

    fn record_session_ready(
        &self,
        session_id: &SessionId,
        worker_id: &WorkerId,
        profile: &ProfileName,
        thread_id: &str,
    ) -> Result<()> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        let Some(mut session) = self.read_session_locked(session_id)? else {
            return Ok(());
        };
        session.worker_id = worker_id.clone();
        session.profile = Some(profile.clone());
        session.app_server_thread_id = Some(thread_id.to_owned());
        if matches!(
            session.lifecycle_state,
            SessionLifecycleState::Opening | SessionLifecycleState::Busy
        ) {
            session.transition_to(SessionLifecycleState::Ready).map_err(anyhow::Error::new)?;
        }
        self.write_session_projection_locked(&session)?;
        Ok(())
    }
}

fn is_active_run_status(status: RunStatus) -> bool {
    matches!(status, RunStatus::Queued | RunStatus::Running)
}

fn blocked_reason_from_payload(payload: &Value) -> Option<BlockedReason> {
    payload.get("blocked_reason").cloned().and_then(|value| serde_json::from_value(value).ok())
}

fn pinned_task_profile(
    task: &TaskRecord,
    sessions: &HashMap<SessionId, SessionRecord>,
    workers: &HashMap<WorkerId, WorkerRecord>,
) -> Option<ProfileName> {
    task.profile.clone().or_else(|| {
        task.session_id.as_ref().and_then(|session_id| sessions.get(session_id)).and_then(
            |session| {
                session.profile.clone().or_else(|| {
                    workers.get(&session.worker_id).map(|worker| worker.profile.clone())
                })
            },
        )
    })
}

fn push_unique_profile(candidates: &mut Vec<ProfileName>, profile: ProfileName) {
    if !candidates.iter().any(|candidate| candidate == &profile) {
        candidates.push(profile);
    }
}

fn sorted_worker_profiles_by_status(
    workers: &HashMap<WorkerId, WorkerRecord>,
    status: WorkerStatus,
) -> Vec<ProfileName> {
    let mut entries = workers
        .values()
        .filter(|worker| worker.status == status)
        .map(|worker| (worker.profile.clone(), worker.worker_id.clone()))
        .collect::<Vec<_>>();
    entries.sort_by(|left, right| left.0.cmp(&right.0).then_with(|| left.1.cmp(&right.1)));
    entries.into_iter().map(|(profile, _)| profile).collect()
}

fn discover_local_profile_names(profiles_root: &Path) -> Vec<ProfileName> {
    let mut profiles = Vec::<ProfileName>::new();
    let Ok(entries) = fs::read_dir(profiles_root) else {
        return profiles;
    };
    for entry in entries.flatten() {
        let entry_path = entry.path();
        if !entry_path.is_dir() {
            continue;
        }
        let file_name = entry.file_name();
        let Some(name) = file_name.to_str() else {
            continue;
        };
        let Ok(profile) = ProfileName::new(name.to_owned()) else {
            continue;
        };
        push_unique_profile(&mut profiles, profile);
    }
    profiles.sort();
    profiles
}

fn default_local_profile_names() -> Vec<ProfileName> {
    discover_local_profile_names(&default_home_dir().join(".si").join("codex").join("profiles"))
}

fn is_profile_resolvable(
    profile: &ProfileName,
    sessions: &HashMap<SessionId, SessionRecord>,
    workers: &HashMap<WorkerId, WorkerRecord>,
    available_profiles: &[ProfileRecord],
    local_profiles: &[ProfileName],
) -> bool {
    workers.values().any(|worker| &worker.profile == profile)
        || sessions.values().any(|session| session.profile.as_ref() == Some(profile))
        || available_profiles.iter().any(|record| &record.profile == profile)
        || local_profiles.iter().any(|candidate| candidate == profile)
}

fn task_profile_candidates_for_binding(
    profile: Option<&ProfileName>,
    session_id: Option<&SessionId>,
    sessions: &HashMap<SessionId, SessionRecord>,
    workers: &HashMap<WorkerId, WorkerRecord>,
    available_profiles: &[ProfileRecord],
    local_profiles: &[ProfileName],
) -> Vec<ProfileName> {
    let mut candidates = Vec::<ProfileName>::new();

    if let Some(profile) = profile.cloned() {
        if is_profile_resolvable(&profile, sessions, workers, available_profiles, local_profiles) {
            push_unique_profile(&mut candidates, profile);
        }
        if session_id.is_none() {
            return candidates;
        }
    }

    if let Some(session_id) = session_id {
        if let Some(session) = sessions.get(session_id) {
            if let Some(profile) = session.profile.clone() {
                push_unique_profile(&mut candidates, profile);
            } else if let Some(worker) = workers.get(&session.worker_id) {
                push_unique_profile(&mut candidates, worker.profile.clone());
            }
        }
        return candidates;
    }

    for profile in sorted_worker_profiles_by_status(workers, WorkerStatus::Ready) {
        push_unique_profile(&mut candidates, profile);
    }
    for profile in sorted_worker_profiles_by_status(workers, WorkerStatus::Degraded) {
        push_unique_profile(&mut candidates, profile);
    }

    let mut profile_records =
        available_profiles.iter().map(|profile| profile.profile.clone()).collect::<Vec<_>>();
    profile_records.sort();
    for profile in profile_records {
        push_unique_profile(&mut candidates, profile);
    }

    for profile in local_profiles.iter().cloned() {
        push_unique_profile(&mut candidates, profile);
    }

    let mut session_profiles =
        sessions.values().filter_map(|session| session.profile.clone()).collect::<Vec<_>>();
    session_profiles.sort();
    for profile in session_profiles {
        push_unique_profile(&mut candidates, profile);
    }

    for status in [WorkerStatus::Starting, WorkerStatus::Failed, WorkerStatus::Stopped] {
        for profile in sorted_worker_profiles_by_status(workers, status) {
            push_unique_profile(&mut candidates, profile);
        }
    }

    candidates
}

fn task_profile_candidates(
    task: &TaskRecord,
    sessions: &HashMap<SessionId, SessionRecord>,
    workers: &HashMap<WorkerId, WorkerRecord>,
    available_profiles: &[ProfileRecord],
    local_profiles: &[ProfileName],
) -> Vec<ProfileName> {
    task_profile_candidates_for_binding(
        task.profile.as_ref(),
        task.session_id.as_ref(),
        sessions,
        workers,
        available_profiles,
        local_profiles,
    )
}

fn can_try_next_profile(reason: BlockedReason) -> bool {
    matches!(
        reason,
        BlockedReason::ProfileUnavailable
            | BlockedReason::WorkerUnavailable
            | BlockedReason::AuthRequired
            | BlockedReason::FortUnavailable
    )
}

fn cron_due_key(rule_name: &str, due_at: DateTime<Utc>) -> String {
    format!("{rule_name}:{}", due_at.to_rfc3339_opts(SecondsFormat::Secs, true))
}

fn hook_event_key(rule_name: &str, seq: u64) -> String {
    format!("{rule_name}:{seq}")
}

fn task_requires_fort(task: &TaskRecord, prompt_override: Option<&str>) -> bool {
    let mut combined = String::with_capacity(
        task.title.len() + task.instructions.len() + prompt_override.unwrap_or_default().len() + 2,
    );
    combined.push_str(&task.title);
    combined.push('\n');
    combined.push_str(&task.instructions);
    if let Some(prompt) = prompt_override {
        combined.push('\n');
        combined.push_str(prompt);
    }
    combined.to_ascii_lowercase().contains("si fort")
}

fn default_fort_profile_dir(home_dir: &Path, profile: &ProfileName) -> PathBuf {
    codex_profile_fort_session_paths(
        &home_dir.join(".si").join("codex").join("profiles").join(profile.as_str()),
    )
    .dir
}

fn fort_profile_dir(worker: &WorkerRecord, profile: &ProfileName) -> PathBuf {
    let codex_home = worker.codex_home.trim();
    if !codex_home.is_empty() {
        return codex_profile_fort_session_paths(&PathBuf::from(codex_home)).dir;
    }
    if let Some(home_dir) =
        worker.home_dir.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        return default_fort_profile_dir(Path::new(home_dir), profile);
    }
    codex_profile_fort_session_paths(&default_codex_home_dir(profile.as_str())).dir
}

fn fort_capability_event_type(state: FortCapabilityState) -> CanonicalEventType {
    match state {
        FortCapabilityState::Ready => CanonicalEventType::FortReady,
        FortCapabilityState::AuthRequired => CanonicalEventType::FortAuthRequired,
        FortCapabilityState::Unavailable => CanonicalEventType::FortUnavailable,
    }
}

fn fort_blocked_reason(state: FortCapabilityState) -> Option<BlockedReason> {
    match state {
        FortCapabilityState::Ready => None,
        FortCapabilityState::AuthRequired => Some(BlockedReason::AuthRequired),
        FortCapabilityState::Unavailable => Some(BlockedReason::FortUnavailable),
    }
}

fn fort_capability_label(state: FortCapabilityState) -> &'static str {
    match state {
        FortCapabilityState::Ready => "ready",
        FortCapabilityState::AuthRequired => "auth_required",
        FortCapabilityState::Unavailable => "unavailable",
    }
}

fn fort_session_state_label(state: &SessionState) -> &'static str {
    match state {
        SessionState::BootstrapRequired => "bootstrap_required",
        SessionState::Resumable(_) => "resumable",
        SessionState::Refreshing(_) => "refreshing",
        SessionState::Revoked { .. } => "revoked",
        SessionState::TeardownPending(_) => "teardown_pending",
        SessionState::Closed => "closed",
    }
}

fn classify_fort_requirement(
    task: &TaskRecord,
    worker: &WorkerRecord,
    profile: &ProfileName,
    prompt_override: Option<&str>,
) -> Result<Option<(FortCapabilityState, String, Value)>> {
    if !task_requires_fort(task, prompt_override) {
        return Ok(None);
    }

    let fort_dir = fort_profile_dir(worker, profile);
    let session_path = fort_dir.join("session.json");
    let access_token_path = fort_dir.join("access.token");
    let refresh_token_path = fort_dir.join("refresh.token");
    let payload_base = json!({
        "fort_profile_dir": fort_dir.display().to_string(),
        "session_path": session_path.display().to_string(),
        "access_token_path": access_token_path.display().to_string(),
        "refresh_token_path": refresh_token_path.display().to_string(),
    });
    if !session_path.exists() {
        let message = format!(
            "Fort authentication is required for profile {profile}: session state is missing"
        );
        return Ok(Some((
            FortCapabilityState::AuthRequired,
            message,
            json!({
                "fort_profile_dir": fort_dir.display().to_string(),
                "session_path": session_path.display().to_string(),
                "access_token_path": access_token_path.display().to_string(),
                "refresh_token_path": refresh_token_path.display().to_string(),
                "fort_state": "missing",
            }),
        )));
    }

    let persisted = match load_persisted_session_state(&session_path) {
        Ok(state) => state,
        Err(SessionStateFileError::Stat(error)) if error.kind() == std::io::ErrorKind::NotFound => {
            let message = format!(
                "Fort authentication is required for profile {profile}: session state is missing"
            );
            return Ok(Some((
                FortCapabilityState::AuthRequired,
                message,
                json!({
                    "fort_profile_dir": fort_dir.display().to_string(),
                    "session_path": session_path.display().to_string(),
                    "access_token_path": access_token_path.display().to_string(),
                    "refresh_token_path": refresh_token_path.display().to_string(),
                    "fort_state": "missing",
                }),
            )));
        }
        Err(error) => {
            let message = format!("Fort is unavailable for profile {profile}: {error}");
            return Ok(Some((
                FortCapabilityState::Unavailable,
                message,
                json!({
                    "fort_profile_dir": fort_dir.display().to_string(),
                    "session_path": session_path.display().to_string(),
                    "access_token_path": access_token_path.display().to_string(),
                    "refresh_token_path": refresh_token_path.display().to_string(),
                    "fort_state": "load_failed",
                    "error": error.to_string(),
                }),
            )));
        }
    };

    if let Err(error) = build_bootstrap_view(
        &persisted,
        Some(profile.as_str()),
        &access_token_path.display().to_string(),
        &refresh_token_path.display().to_string(),
        &access_token_path.display().to_string(),
        &refresh_token_path.display().to_string(),
    ) {
        let message = format!("Fort is unavailable for profile {profile}: {error}");
        return Ok(Some((
            FortCapabilityState::Unavailable,
            message,
            json!({
                "fort_profile_dir": fort_dir.display().to_string(),
                "session_path": session_path.display().to_string(),
                "access_token_path": access_token_path.display().to_string(),
                "refresh_token_path": refresh_token_path.display().to_string(),
                "fort_state": "bootstrap_invalid",
                "error": error.to_string(),
            }),
        )));
    }

    let now_unix = Utc::now().timestamp();
    let session_state = match classify_persisted_session_state(&persisted, now_unix) {
        Ok(state) => state,
        Err(error) => {
            let message = format!("Fort is unavailable for profile {profile}: {error}");
            return Ok(Some((
                FortCapabilityState::Unavailable,
                message,
                json!({
                    "fort_profile_dir": fort_dir.display().to_string(),
                    "session_path": session_path.display().to_string(),
                    "access_token_path": access_token_path.display().to_string(),
                    "refresh_token_path": refresh_token_path.display().to_string(),
                    "fort_state": "classification_failed",
                    "error": error.to_string(),
                }),
            )));
        }
    };

    let (state, message) = match &session_state {
        SessionState::Resumable(_) | SessionState::Refreshing(_) => {
            (FortCapabilityState::Ready, format!("Fort is ready for profile {profile}"))
        }
        SessionState::BootstrapRequired | SessionState::Revoked { .. } | SessionState::Closed => (
            FortCapabilityState::AuthRequired,
            format!("Fort authentication is required for profile {profile}"),
        ),
        SessionState::TeardownPending(_) => (
            FortCapabilityState::Unavailable,
            format!("Fort is unavailable for profile {profile}: session teardown is still pending"),
        ),
    };
    if state == FortCapabilityState::Ready && !refresh_token_path.is_file() {
        let message = format!(
            "Fort authentication is required for profile {profile}: refresh token is missing"
        );
        return Ok(Some((
            FortCapabilityState::AuthRequired,
            message,
            json!({
                "fort_profile_dir": fort_dir.display().to_string(),
                "session_path": session_path.display().to_string(),
                "access_token_path": access_token_path.display().to_string(),
                "refresh_token_path": refresh_token_path.display().to_string(),
                "fort_state": "refresh_token_missing",
            }),
        )));
    }
    let payload = merge_json_objects(
        payload_base,
        json!({
            "fort_state": fort_session_state_label(&session_state),
            "fort_capability": fort_capability_label(state),
        }),
    );
    Ok(Some((state, message, payload)))
}

fn merge_json_objects(base: Value, extra: Value) -> Value {
    match (base, extra) {
        (Value::Object(mut left), Value::Object(right)) => {
            for (key, value) in right {
                left.insert(key, value);
            }
            Value::Object(left)
        }
        (_, right) => right,
    }
}

fn parse_every_duration(schedule: &str) -> Result<ChronoDuration> {
    let schedule = schedule.trim();
    if schedule.is_empty() {
        anyhow::bail!("every schedule cannot be empty");
    }
    let digits_len = schedule.chars().take_while(|ch| ch.is_ascii_digit()).count();
    if digits_len == 0 {
        anyhow::bail!("every schedule must start with digits");
    }
    let amount: i64 = schedule[..digits_len].parse().context("parse every schedule amount")?;
    let unit = schedule[digits_len..].trim();
    let duration = match unit {
        "s" => ChronoDuration::seconds(amount),
        "m" => ChronoDuration::minutes(amount),
        "h" => ChronoDuration::hours(amount),
        "d" => ChronoDuration::days(amount),
        "" => ChronoDuration::seconds(amount),
        _ => anyhow::bail!("unsupported every schedule unit: {unit}"),
    };
    if duration <= ChronoDuration::zero() {
        anyhow::bail!("every schedule must be positive");
    }
    Ok(duration)
}

fn next_cron_due_after(
    rule: &CronRuleRecord,
    after: DateTime<Utc>,
) -> Result<Option<DateTime<Utc>>> {
    match rule.schedule_kind {
        CronScheduleKind::OnceAt => {
            let due_at = DateTime::parse_from_rfc3339(rule.schedule.trim())
                .context("parse once_at schedule")?
                .with_timezone(&Utc);
            Ok((due_at > after).then_some(due_at))
        }
        CronScheduleKind::Every => Ok(Some(after + parse_every_duration(&rule.schedule)?)),
        CronScheduleKind::Cron => {
            let schedule =
                Schedule::from_str(rule.schedule.trim()).context("parse cron schedule")?;
            Ok(schedule.after(&after).next())
        }
    }
}

fn load_json_records_from_dir<T>(dir: &Path) -> Result<Vec<T>>
where
    T: for<'de> Deserialize<'de>,
{
    let mut paths = fs::read_dir(dir)
        .with_context(|| format!("read {}", dir.display()))?
        .filter_map(|entry| entry.ok().map(|value| value.path()))
        .filter(|path| path.is_file())
        .collect::<Vec<_>>();
    paths.sort();

    let mut records = Vec::with_capacity(paths.len());
    for path in paths {
        if let Ok(record) = read_json_path::<T>(&path) {
            records.push(record);
        }
    }
    Ok(records)
}

fn load_json_records_from_child_dirs<T>(dir: &Path, file_name: &str) -> Result<Vec<T>>
where
    T: DeserializeOwned,
{
    let mut paths = fs::read_dir(dir)
        .with_context(|| format!("read {}", dir.display()))?
        .filter_map(|entry| entry.ok().map(|value| value.path().join(file_name)))
        .filter(|path| path.exists())
        .collect::<Vec<_>>();
    paths.sort();

    let mut records = Vec::with_capacity(paths.len());
    for path in paths {
        if let Ok(record) = read_json_path::<T>(&path) {
            records.push(record);
        }
    }
    Ok(records)
}

struct CanonicalEventLoadResult {
    events: Vec<CanonicalEvent>,
    last_seq: u64,
    warnings: Vec<Value>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum CanonicalEventLoadMode {
    #[cfg(test)]
    Strict,
    StartupRecovery,
    LiveRecovery,
}

#[cfg(test)]
fn load_canonical_events(path: &Path) -> Result<Vec<CanonicalEvent>> {
    Ok(load_canonical_events_with_mode(path, CanonicalEventLoadMode::Strict)?.events)
}

fn load_canonical_events_for_startup_recovery(path: &Path) -> Result<CanonicalEventLoadResult> {
    load_canonical_events_with_mode(path, CanonicalEventLoadMode::StartupRecovery)
}

fn load_canonical_events_for_live_iteration(path: &Path) -> Result<CanonicalEventLoadResult> {
    load_canonical_events_with_mode(path, CanonicalEventLoadMode::LiveRecovery)
}

fn load_canonical_events_with_mode(
    path: &Path,
    mode: CanonicalEventLoadMode,
) -> Result<CanonicalEventLoadResult> {
    let mut result =
        CanonicalEventLoadResult { events: Vec::new(), last_seq: 0, warnings: Vec::new() };
    for log_path in canonical_event_log_paths(path)? {
        match load_canonical_events_from_path_with_retry(
            &log_path,
            matches!(mode, CanonicalEventLoadMode::LiveRecovery) && log_path == path,
        ) {
            Ok(events) => {
                for event in events {
                    result.last_seq = result.last_seq.max(event.seq);
                    result.events.push(event);
                }
            }
            #[cfg(test)]
            Err(error) if matches!(mode, CanonicalEventLoadMode::Strict) => {
                return Err(error);
            }
            Err(error) => {
                result.warnings.push(quarantine_malformed_canonical_event_log(&log_path, &error)?);
            }
        }
    }
    Ok(result)
}

fn source_from_task_source(source: TaskSource) -> CanonicalEventSource {
    match source {
        TaskSource::Cli => CanonicalEventSource::Cli,
        TaskSource::Websocket => CanonicalEventSource::Websocket,
        TaskSource::Cron => CanonicalEventSource::Cron,
        TaskSource::Hook => CanonicalEventSource::Hook,
        TaskSource::System => CanonicalEventSource::System,
    }
}

#[cfg(test)]
fn load_last_event_seq(path: &Path) -> Result<u64> {
    Ok(load_canonical_events_with_mode(path, CanonicalEventLoadMode::Strict)?.last_seq)
}

fn canonical_event_log_paths(path: &Path) -> Result<Vec<PathBuf>> {
    let parent = path.parent().ok_or_else(|| anyhow!("missing parent for {}", path.display()))?;
    let mut rotated = Vec::new();
    for entry in fs::read_dir(parent).with_context(|| format!("read {}", parent.display()))? {
        let entry = entry?;
        let entry_path = entry.path();
        if !entry_path.is_file() {
            continue;
        }
        let Some(name) = entry_path.file_name().and_then(|value| value.to_str()) else {
            continue;
        };
        if name.starts_with("events-") && name.ends_with(".jsonl") {
            rotated.push(entry_path);
        }
    }
    rotated.sort();
    rotated.push(path.to_path_buf());
    Ok(rotated)
}

fn load_canonical_events_from_path(path: &Path) -> Result<Vec<CanonicalEvent>> {
    let file = File::open(path).with_context(|| format!("open {}", path.display()))?;
    let reader = BufReader::new(file);
    let mut events = Vec::new();
    for (index, line) in reader.lines().enumerate() {
        let line = line.with_context(|| format!("read {} line {}", path.display(), index + 1))?;
        if line.trim().is_empty() {
            continue;
        }
        events.push(
            serde_json::from_str::<CanonicalEvent>(&line)
                .with_context(|| format!("parse {} line {}", path.display(), index + 1))?,
        );
    }
    Ok(events)
}

fn load_canonical_events_from_path_with_retry(
    path: &Path,
    retry_eof: bool,
) -> Result<Vec<CanonicalEvent>> {
    let attempts = if retry_eof { 3 } else { 1 };
    let mut last_error = None;
    for attempt in 0..attempts {
        match load_canonical_events_from_path(path) {
            Ok(events) => return Ok(events),
            Err(error) if retry_eof && is_eof_parse_error(&error) && attempt + 1 < attempts => {
                last_error = Some(error);
                thread::sleep(Duration::from_millis(20));
            }
            Err(error) => return Err(error),
        }
    }
    Err(last_error.expect("canonical event retry should capture an error"))
}

fn is_eof_parse_error(error: &anyhow::Error) -> bool {
    let message = error.to_string();
    message.contains("parse ") && message.contains("EOF while parsing")
}

fn quarantine_malformed_canonical_event_log(path: &Path, error: &anyhow::Error) -> Result<Value> {
    let file_name = path.file_name().and_then(|value| value.to_str()).unwrap_or("events.jsonl");
    let quarantine_path = path.with_file_name(format!("{file_name}.corrupt-{}", temp_suffix()));
    fs::rename(path, &quarantine_path)
        .with_context(|| format!("rename {} -> {}", path.display(), quarantine_path.display()))?;
    Ok(json!({
        "kind": "canonical_event_log",
        "path": path.display().to_string(),
        "quarantine_path": quarantine_path.display().to_string(),
        "error": error.to_string(),
    }))
}

fn startup_recovery_warning_message(details: &Value) -> &'static str {
    if details.get("kind").and_then(Value::as_str) == Some("canonical_event_log") {
        "isolated malformed canonical event log during startup recovery"
    } else {
        "isolated malformed persisted object during startup recovery"
    }
}

fn read_json_path<T: DeserializeOwned>(path: &Path) -> Result<T> {
    let bytes = fs::read(path).with_context(|| format!("read {}", path.display()))?;
    serde_json::from_slice::<T>(&bytes).with_context(|| format!("parse {}", path.display()))
}

fn malformed_state_warning(kind: &str, path: &Path, error: &str) -> Value {
    json!({
        "kind": kind,
        "path": path.display().to_string(),
        "error": error,
    })
}

fn write_json_atomic<T: Serialize>(path: &Path, value: &T) -> Result<()> {
    let parent = path.parent().ok_or_else(|| anyhow!("missing parent for {}", path.display()))?;
    fs::create_dir_all(parent).with_context(|| format!("create {}", parent.display()))?;
    let tmp_path = parent.join(format!(".tmp-{}", temp_suffix()));
    let bytes = serde_json::to_vec_pretty(value)?;
    fs::write(&tmp_path, bytes).with_context(|| format!("write {}", tmp_path.display()))?;
    fs::rename(&tmp_path, path)
        .with_context(|| format!("rename {} -> {}", tmp_path.display(), path.display()))?;
    Ok(())
}

fn write_markdown_atomic(path: &Path, contents: &str) -> Result<()> {
    let parent = path.parent().ok_or_else(|| anyhow!("missing parent for {}", path.display()))?;
    fs::create_dir_all(parent).with_context(|| format!("create {}", parent.display()))?;
    let tmp_path = parent.join(format!(".tmp-{}", temp_suffix()));
    fs::write(&tmp_path, contents).with_context(|| format!("write {}", tmp_path.display()))?;
    fs::rename(&tmp_path, path)
        .with_context(|| format!("rename {} -> {}", tmp_path.display(), path.display()))?;
    Ok(())
}

fn render_worker_summary(worker: &WorkerRecord, runtime: Option<&WorkerRuntimeView>) -> String {
    let mut summary = format!(
        "# Worker {}\n\n- Profile: `{}`\n- Status: `{}`\n",
        worker.worker_id,
        worker.profile,
        serde_json::to_string(&worker.status)
            .unwrap_or_else(|_| "\"unknown\"".to_owned())
            .trim_matches('"'),
    );
    push_optional_markdown_line(
        &mut summary,
        "Capability version",
        worker.capability_version.as_deref(),
    );
    push_optional_markdown_line(
        &mut summary,
        "Last heartbeat",
        worker.last_heartbeat_at.as_ref().map(format_timestamp).as_deref(),
    );
    summary.push_str(&format!("- `CODEX_HOME`: `{}`\n", worker.codex_home));
    push_optional_markdown_line(&mut summary, "Home dir", worker.home_dir.as_deref());
    push_optional_markdown_line(&mut summary, "Workdir", worker.workdir.as_deref());
    push_optional_markdown_line(
        &mut summary,
        "Account state",
        worker.effective_account_state.as_deref(),
    );
    if !worker.extra_env.is_empty() {
        let keys = worker.extra_env.keys().map(String::as_str).collect::<Vec<_>>().join(", ");
        summary.push_str(&format!("- Extra env keys: `{keys}`\n"));
    }
    if let Some(runtime) = runtime {
        summary.push_str("\n## Runtime\n\n");
        summary.push_str(&format!("- Runtime: `{}`\n", runtime.runtime_name));
        summary.push_str(&format!("- PID: `{}`\n", runtime.pid));
        summary.push_str(&format!("- Started at: `{}`\n", format_timestamp(&runtime.started_at)));
        summary.push_str(&format!("- Last checked: `{}`\n", format_timestamp(&runtime.checked_at)));
    }
    summary
}

fn render_session_summary(session: &SessionRecord) -> String {
    let mut summary = format!(
        "# Session {}\n\n- Worker: `{}`\n- Lifecycle: `{}`\n",
        session.session_id,
        session.worker_id,
        serde_json::to_string(&session.lifecycle_state)
            .unwrap_or_else(|_| "\"unknown\"".to_owned())
            .trim_matches('"'),
    );
    push_optional_markdown_line(
        &mut summary,
        "Profile",
        session.profile.as_ref().map(ProfileName::as_str),
    );
    push_optional_markdown_line(&mut summary, "Thread", session.app_server_thread_id.as_deref());
    push_optional_markdown_line(&mut summary, "Workdir", session.workdir.as_deref());
    push_optional_markdown_line(&mut summary, "Summary", session.summary_state.as_deref());
    summary.push_str(&format!("- Created at: `{}`\n", format_timestamp(&session.created_at)));
    summary.push_str(&format!("- Updated at: `{}`\n", format_timestamp(&session.updated_at)));
    summary
}

fn render_run_summary(run: &RunRecord, task: Option<&TaskRecord>) -> String {
    let mut summary = format!(
        "# Run {}\n\n- Task: `{}`\n- Session: `{}`\n- Status: `{}`\n",
        run.run_id,
        run.task_id,
        run.session_id,
        serde_json::to_string(&run.status)
            .unwrap_or_else(|_| "\"unknown\"".to_owned())
            .trim_matches('"'),
    );
    push_optional_markdown_line(
        &mut summary,
        "Started at",
        run.started_at.as_ref().map(format_timestamp).as_deref(),
    );
    push_optional_markdown_line(
        &mut summary,
        "Ended at",
        run.ended_at.as_ref().map(format_timestamp).as_deref(),
    );
    push_optional_markdown_line(&mut summary, "Turn", run.app_server_turn_id.as_deref());
    if let Some(task) = task {
        summary.push_str("\n## Task Projection\n\n");
        summary.push_str(&format!("- Title: {}\n", task.title));
        summary.push_str(&format!(
            "- Task status: `{}`\n",
            serde_json::to_string(&task.status)
                .unwrap_or_else(|_| "\"unknown\"".to_owned())
                .trim_matches('"'),
        ));
        push_optional_markdown_line(&mut summary, "Checkpoint", task.checkpoint_summary.as_deref());
        push_optional_markdown_line(
            &mut summary,
            "Blocked reason",
            task.blocked_reason
                .as_ref()
                .and_then(|reason| serde_json::to_string(reason).ok())
                .as_deref()
                .map(|value| value.trim_matches('"')),
        );
    }
    summary
}

fn push_optional_markdown_line(summary: &mut String, label: &str, value: Option<&str>) {
    if let Some(value) = value.map(str::trim).filter(|value| !value.is_empty()) {
        summary.push_str(&format!("- {label}: `{value}`\n"));
    }
}

fn format_timestamp(timestamp: &DateTime<Utc>) -> String {
    timestamp.to_rfc3339_opts(SecondsFormat::Secs, true)
}

fn worker_restart_backoff(attempt: u32) -> ChronoDuration {
    let multiplier = 2_i64.saturating_pow(attempt.saturating_sub(1));
    let millis = WORKER_RESTART_BACKOFF_BASE.as_millis() as i64;
    ChronoDuration::milliseconds(millis.saturating_mul(multiplier))
}

fn is_prunable_task_status(status: TaskStatus) -> bool {
    matches!(status, TaskStatus::Done | TaskStatus::Failed | TaskStatus::Cancelled)
}

fn append_jsonl<T: Serialize>(path: &Path, value: &T) -> Result<()> {
    append_jsonl_with_rotation(path, value, MAX_EVENTS_JSONL_BYTES)
}

fn append_jsonl_with_rotation<T: Serialize>(path: &Path, value: &T, max_bytes: u64) -> Result<()> {
    rotate_jsonl_if_needed(path, max_bytes)?;
    let mut file = OpenOptions::new()
        .create(true)
        .append(true)
        .open(path)
        .with_context(|| format!("open {}", path.display()))?;
    serde_json::to_writer(&mut file, value)?;
    file.write_all(b"\n").with_context(|| format!("append {}", path.display()))?;
    file.flush().with_context(|| format!("flush {}", path.display()))?;
    Ok(())
}

fn rotate_jsonl_if_needed(path: &Path, max_bytes: u64) -> Result<()> {
    if !path.exists() {
        return Ok(());
    }
    if fs::metadata(path).with_context(|| format!("stat {}", path.display()))?.len() < max_bytes {
        return Ok(());
    }
    let parent = path.parent().ok_or_else(|| anyhow!("missing parent for {}", path.display()))?;
    let rotated_path = parent.join(format!(
        "events-{:020}-{}.jsonl",
        Utc::now().timestamp_millis(),
        process::id()
    ));
    fs::rename(path, &rotated_path)
        .with_context(|| format!("rename {} -> {}", path.display(), rotated_path.display()))?;
    File::create(path).with_context(|| format!("create {}", path.display()))?;
    Ok(())
}

fn persist_gateway_metadata(paths: &NucleusPaths, bind_addr: SocketAddr) -> Result<()> {
    let metadata_path = paths.gateway_dir.join("metadata.json");
    let metadata = GatewayMetadata {
        version: env!("CARGO_PKG_VERSION").to_owned(),
        bind_addr: bind_addr.to_string(),
        ws_url: format!("ws://{bind_addr}{WS_PATH}"),
    };
    write_json_atomic(&metadata_path, &metadata)
}

#[derive(Clone)]
pub struct NucleusService {
    config: NucleusConfig,
    store: Arc<NucleusStore>,
    events: broadcast::Sender<CanonicalEvent>,
    runtime: Option<Arc<dyn NucleusRuntime>>,
    background_started: Arc<AtomicBool>,
    worker_restart_state: Arc<Mutex<HashMap<WorkerId, WorkerRestartState>>>,
    background_warning_state: Arc<Mutex<BackgroundWarningState>>,
}

impl NucleusService {
    pub fn open(config: NucleusConfig) -> Result<Self> {
        Self::open_without_runtime(config)
    }

    pub fn open_without_runtime(config: NucleusConfig) -> Result<Self> {
        let store = Arc::new(NucleusStore::open(config.state_dir.clone())?);
        let (events, _) = broadcast::channel(256);
        Ok(Self {
            config,
            store,
            events,
            runtime: None,
            background_started: Arc::new(AtomicBool::new(false)),
            worker_restart_state: Arc::new(Mutex::new(HashMap::new())),
            background_warning_state: Arc::new(Mutex::new(BackgroundWarningState::default())),
        })
    }

    pub fn open_with_runtime(
        config: NucleusConfig,
        runtime: Arc<dyn NucleusRuntime>,
    ) -> Result<Self> {
        let store = Arc::new(NucleusStore::open(config.state_dir.clone())?);
        let (events, _) = broadcast::channel(256);
        Ok(Self {
            config,
            store,
            events,
            runtime: Some(runtime),
            background_started: Arc::new(AtomicBool::new(false)),
            worker_restart_state: Arc::new(Mutex::new(HashMap::new())),
            background_warning_state: Arc::new(Mutex::new(BackgroundWarningState::default())),
        })
    }

    pub fn status(&self) -> Result<NucleusStatusView> {
        Ok(NucleusStatusView {
            version: env!("CARGO_PKG_VERSION").to_owned(),
            bind_addr: self.config.bind_addr.to_string(),
            ws_url: self.config.ws_url(),
            state_dir: self.store.paths().root.display().to_string(),
            task_count: self.store.task_count()?,
            worker_count: self.store.worker_count()?,
            session_count: self.store.session_count()?,
            run_count: self.store.run_count()?,
            next_event_seq: self.store.next_event_seq(),
        })
    }

    pub fn router(self) -> Router {
        Router::new()
            .route(WS_PATH, get(ws_handler))
            .route(OPENAPI_PATH, get(rest_openapi_handler))
            .route(REST_STATUS_PATH, get(rest_status_handler))
            .route(REST_TASKS_PATH, get(rest_list_tasks_handler).post(rest_create_task_handler))
            .route(REST_TASK_PATH, get(rest_inspect_task_handler))
            .route(REST_TASK_CANCEL_PATH, post(rest_cancel_task_handler))
            .route(REST_EVENTS_PATH, post(rest_ingest_event_handler))
            .route(REST_WORKERS_PATH, get(rest_list_workers_handler))
            .route(REST_WORKER_PATH, get(rest_inspect_worker_handler))
            .route(REST_SESSION_PATH, get(rest_show_session_handler))
            .route(REST_RUN_PATH, get(rest_inspect_run_handler))
            .route(
                REST_HOOK_RULES_PATH,
                get(rest_list_hook_rules_handler).post(rest_upsert_hook_rule_handler),
            )
            .route(
                REST_HOOK_RULE_PATH,
                get(rest_inspect_hook_rule_handler).delete(rest_delete_hook_rule_handler),
            )
            .with_state(Arc::new(self))
    }

    pub async fn serve(self) -> Result<()> {
        let bind_addr = self.config.bind_addr;
        let listener =
            TcpListener::bind(bind_addr).await.with_context(|| format!("bind {bind_addr}"))?;
        let bound_addr = listener.local_addr().context("read bound si-nucleus address")?;
        persist_gateway_metadata(self.store.paths(), bound_addr)?;
        self.initialize_runtime_loops()?;
        axum::serve(listener, self.router()).await.context("serve si-nucleus websocket gateway")
    }

    fn initialize_runtime_loops(&self) -> Result<()> {
        if self.runtime.is_none() {
            return Ok(());
        }
        if self.background_started.swap(true, Ordering::SeqCst) {
            return Ok(());
        }
        self.reconcile_worker_runtime_state()?;
        self.reconcile_inflight_runs(true)?;
        self.process_cron_producers_at(Utc::now())?;
        self.process_hook_producers()?;
        self.dispatch_queued_tasks()?;
        let service = self.clone();
        thread::spawn(move || service.background_runtime_loop());
        Ok(())
    }

    fn background_runtime_loop(self) {
        loop {
            if let Err(error) = self.reconcile_and_dispatch_once() {
                if let Some(payload) = self.record_background_loop_warning(Utc::now(), &error) {
                    if let Ok(event) = self
                        .store
                        .append_system_warning("nucleus background loop iteration failed", payload)
                    {
                        let _ = self.events.send(event);
                    }
                }
            }
            thread::sleep(DISPATCH_LOOP_INTERVAL);
        }
    }

    fn reconcile_and_dispatch_once(&self) -> Result<()> {
        self.reconcile_worker_runtime_state()?;
        self.reconcile_inflight_runs(false)?;
        self.process_cron_producers_at(Utc::now())?;
        self.process_hook_producers()?;
        self.dispatch_queued_tasks()?;
        Ok(())
    }

    fn record_background_loop_warning(
        &self,
        now: DateTime<Utc>,
        error: &anyhow::Error,
    ) -> Option<Value> {
        let signature = error.to_string();
        let mut state = self.background_warning_state.lock().ok()?;
        let mut suppressed = 0_u64;
        if state.last_error.as_deref() == Some(signature.as_str()) {
            if let Some(last_emitted_at) = state.last_emitted_at {
                if now < last_emitted_at + BACKGROUND_WARNING_THROTTLE_WINDOW {
                    state.suppressed += 1;
                    return None;
                }
            }
            suppressed = state.suppressed;
        }
        state.last_error = Some(signature.clone());
        state.last_emitted_at = Some(now);
        state.suppressed = 0;
        Some(json!({
            "error": signature,
            "suppressed_since_last_emit": suppressed,
        }))
    }

    fn reconcile_worker_runtime_state(&self) -> Result<()> {
        let Some(runtime) = self.runtime.as_ref() else {
            return Ok(());
        };
        for worker in self.store.list_workers()? {
            if matches!(
                worker.status,
                si_nucleus_core::WorkerStatus::Failed | si_nucleus_core::WorkerStatus::Stopped
            ) {
                continue;
            }
            if runtime.inspect_worker(&worker.worker_id)?.is_none() {
                if let Some(event) = self.store.mark_worker_failed(
                    &worker.worker_id,
                    "worker process is not attached to the runtime",
                )? {
                    let _ = self.events.send(event);
                }
            }
        }
        for worker in self.store.list_workers()? {
            if !matches!(
                worker.status,
                si_nucleus_core::WorkerStatus::Failed | si_nucleus_core::WorkerStatus::Stopped
            ) {
                continue;
            }
            if runtime.inspect_worker(&worker.worker_id)?.is_some() {
                self.clear_worker_restart_state(&worker.worker_id);
                continue;
            }
            self.maybe_restart_failed_worker(runtime.as_ref(), &worker)?;
        }
        Ok(())
    }

    fn process_cron_producers_at(&self, now: DateTime<Utc>) -> Result<()> {
        for mut rule in
            load_json_records_from_dir::<CronRuleRecord>(&self.store.paths().cron_state_dir)?
        {
            if let Err(error) = self.process_single_cron_rule(&mut rule, now) {
                if let Ok(event) = self.store.append_system_warning(
                    "cron producer iteration failed",
                    json!({
                        "rule_name": rule.name,
                        "error": error.to_string(),
                    }),
                ) {
                    let _ = self.events.send(event);
                }
            }
        }
        Ok(())
    }

    fn process_single_cron_rule(
        &self,
        rule: &mut CronRuleRecord,
        now: DateTime<Utc>,
    ) -> Result<()> {
        ProfileName::new(rule.name.clone()).context("validate cron rule name")?;
        if !rule.enabled {
            return Ok(());
        }
        let mut changed = false;
        if rule.version != current_persisted_version() {
            rule.version = current_persisted_version().to_owned();
            changed = true;
        }
        if rule.next_due_at.is_none() {
            rule.next_due_at = next_cron_due_after(rule, now - ChronoDuration::seconds(1))?;
            changed = true;
            self.write_cron_rule(rule)?;
        }
        while let Some(due_at) = rule.next_due_at {
            if due_at > now {
                break;
            }
            let dedup_key = cron_due_key(&rule.name, due_at);
            let title = format!(
                "Cron {} @ {}",
                rule.name,
                due_at.to_rfc3339_opts(SecondsFormat::Secs, true)
            );
            let instructions = format!(
                "{}\n\nCron rule: {}\nScheduled fire time: {}",
                rule.instructions,
                rule.name,
                due_at.to_rfc3339_opts(SecondsFormat::Secs, true)
            );
            if let Some((_, events)) = self.store.create_producer_task(
                CreateTaskInput {
                    title,
                    instructions,
                    source: TaskSource::Cron,
                    profile: None,
                    session_id: None,
                    max_retries: None,
                    timeout_seconds: None,
                },
                &rule.name,
                &dedup_key,
            )? {
                for event in events {
                    let _ = self.events.send(event);
                }
            }
            rule.last_emitted_at = Some(due_at);
            rule.next_due_at = next_cron_due_after(rule, due_at)?;
            changed = true;
        }
        if changed {
            self.write_cron_rule(rule)?;
        }
        Ok(())
    }

    fn process_hook_producers(&self) -> Result<()> {
        let rules =
            load_json_records_from_dir::<HookRuleRecord>(&self.store.paths().hook_state_dir)?;
        if rules.is_empty() {
            return Ok(());
        }

        let loaded = load_canonical_events_for_live_iteration(&self.store.paths().events_path)?;
        for warning in loaded.warnings {
            let event = self.store.append_system_warning(
                "isolated malformed canonical event log during live iteration",
                warning,
            )?;
            let _ = self.events.send(event);
        }
        for mut rule in rules {
            if let Err(error) = self.process_single_hook_rule(&mut rule, &loaded.events) {
                if let Ok(event) = self.store.append_system_warning(
                    "hook producer iteration failed",
                    json!({
                        "rule_name": rule.name,
                        "error": error.to_string(),
                    }),
                ) {
                    let _ = self.events.send(event);
                }
            }
        }
        Ok(())
    }

    fn process_single_hook_rule(
        &self,
        rule: &mut HookRuleRecord,
        events: &[CanonicalEvent],
    ) -> Result<()> {
        ProfileName::new(rule.name.clone()).context("validate hook rule name")?;
        if !rule.enabled {
            return Ok(());
        }
        let mut changed = false;
        if rule.version != current_persisted_version() {
            rule.version = current_persisted_version().to_owned();
            changed = true;
        }
        for event in events {
            if event.seq <= rule.last_processed_event_seq {
                continue;
            }
            let self_triggered = event.source == CanonicalEventSource::Hook
                && event.data.payload.get("producer_rule_name").and_then(Value::as_str)
                    == Some(rule.name.as_str());
            if event.event_type.as_str() == rule.match_event_type && !self_triggered {
                let dedup_key = hook_event_key(&rule.name, event.seq);
                let title =
                    format!("Hook {} @ {} #{}", rule.name, event.event_type.as_str(), event.seq);
                let instructions = format!(
                    "{}\n\nCanonical event type: {}\nCanonical event sequence: {}",
                    rule.instructions,
                    event.event_type.as_str(),
                    event.seq
                );
                if let Some((_, emitted)) = self.store.create_producer_task(
                    CreateTaskInput {
                        title,
                        instructions,
                        source: TaskSource::Hook,
                        profile: None,
                        session_id: None,
                        max_retries: None,
                        timeout_seconds: None,
                    },
                    &rule.name,
                    &dedup_key,
                )? {
                    for appended in emitted {
                        let _ = self.events.send(appended);
                    }
                }
            }
            rule.last_processed_event_seq = event.seq;
            changed = true;
        }
        if changed {
            self.store.persist_hook_rule_progress(rule)?;
        }
        Ok(())
    }

    fn write_cron_rule(&self, rule: &CronRuleRecord) -> Result<()> {
        write_json_atomic(&self.store.paths().cron_rule_path(&rule.name), rule)
    }

    fn reconcile_inflight_runs(&self, block_ambiguous_healthy_runs: bool) -> Result<()> {
        for run in self.store.list_runs()? {
            if !is_active_run_status(run.status) {
                continue;
            }
            let task = self.store.inspect_task(&run.task_id)?;
            let session = self.store.inspect_session(&run.session_id)?;
            let worker_id = session.as_ref().map(|entry| entry.worker_id.clone());
            let profile = task
                .as_ref()
                .and_then(|entry| entry.profile.clone())
                .or_else(|| session.as_ref().and_then(|entry| entry.profile.clone()));
            let (blocked_reason, message, mark_session_broken) = match session.as_ref() {
                None => (
                    BlockedReason::SessionBroken,
                    "run references a missing session".to_owned(),
                    false,
                ),
                Some(session)
                    if matches!(
                        session.lifecycle_state,
                        SessionLifecycleState::Broken | SessionLifecycleState::Closed
                    ) =>
                {
                    (
                        BlockedReason::SessionBroken,
                        "run is attached to a non-reusable session".to_owned(),
                        false,
                    )
                }
                Some(session) if session.app_server_thread_id.is_none() => (
                    BlockedReason::SessionBroken,
                    "run is attached to a session without an app-server thread id".to_owned(),
                    true,
                ),
                Some(session) => {
                    let Some(runtime) = self.runtime.as_ref() else {
                        continue;
                    };
                    match runtime.inspect_worker(&session.worker_id)? {
                        Some(_) if block_ambiguous_healthy_runs => (
                            BlockedReason::SessionBroken,
                            "run could not be proven healthy after reconciliation".to_owned(),
                            true,
                        ),
                        Some(_) => continue,
                        None => (
                            BlockedReason::WorkerUnavailable,
                            "run lost its worker during reconciliation".to_owned(),
                            true,
                        ),
                    }
                }
            };
            self.reconcile_run_as_blocked(
                &run,
                BlockedRunReconciliation {
                    worker_id,
                    session_id: Some(run.session_id.clone()),
                    profile,
                    blocked_reason,
                    message,
                    mark_session_broken,
                },
            )?;
        }
        Ok(())
    }

    fn reconcile_run_as_blocked(
        &self,
        run: &RunRecord,
        blocked: BlockedRunReconciliation,
    ) -> Result<()> {
        let events = self.store.apply_runtime_event(CanonicalEventDraft {
            event_type: CanonicalEventType::RunBlocked,
            source: CanonicalEventSource::System,
            data: EventDataEnvelope {
                task_id: Some(run.task_id.clone()),
                worker_id: blocked.worker_id,
                session_id: blocked.session_id.clone(),
                run_id: Some(run.run_id.clone()),
                profile: blocked.profile,
                payload: json!({
                    "blocked_reason": blocked.blocked_reason,
                    "error": blocked.message,
                }),
            },
        })?;
        for event in events {
            let _ = self.events.send(event);
        }
        if blocked.mark_session_broken {
            if let Some(session_id) = blocked.session_id {
                if let Some(event) =
                    self.store.mark_session_broken(&session_id, &blocked.message)?
                {
                    let _ = self.events.send(event);
                }
            }
        }
        Ok(())
    }

    fn validate_task_create_input(&self, input: &CreateTaskInput) -> Result<()> {
        if input.title.trim().is_empty() {
            anyhow::bail!("task title cannot be empty");
        }
        if input.instructions.trim().is_empty() {
            anyhow::bail!("task instructions cannot be empty");
        }
        Ok(())
    }

    fn task_create_bound_session_profile(
        &self,
        session: &SessionRecord,
    ) -> Result<Option<ProfileName>> {
        if let Some(profile) = session.profile.clone() {
            return Ok(Some(profile));
        }
        Ok(self.store.inspect_worker(&session.worker_id)?.map(|worker| worker.profile))
    }

    fn evaluate_task_session_binding(
        &self,
        session_id: &SessionId,
        requested_profile: Option<&ProfileName>,
    ) -> Result<TaskSessionBindingEvaluation> {
        let Some(session) = self.store.inspect_session(session_id)? else {
            return Ok(TaskSessionBindingEvaluation::Blocked(TaskSessionBindingBlock {
                worker_id: None,
                session_id: Some(session_id.clone()),
                profile: requested_profile.cloned(),
                blocked_reason: BlockedReason::SessionBroken,
                message: "task references a missing session".to_owned(),
                mark_session_broken: false,
            }));
        };
        let bound_profile = self.task_create_bound_session_profile(&session)?;
        if let (Some(requested_profile), Some(bound_profile)) =
            (requested_profile, bound_profile.as_ref())
        {
            if requested_profile != bound_profile {
                return Ok(TaskSessionBindingEvaluation::Blocked(TaskSessionBindingBlock {
                    worker_id: Some(session.worker_id.clone()),
                    session_id: Some(session.session_id.clone()),
                    profile: Some(requested_profile.clone()),
                    blocked_reason: BlockedReason::SessionBroken,
                    message: "task profile does not match session profile".to_owned(),
                    mark_session_broken: false,
                }));
            }
        }
        if matches!(
            session.lifecycle_state,
            SessionLifecycleState::Broken | SessionLifecycleState::Closed
        ) {
            return Ok(TaskSessionBindingEvaluation::Blocked(TaskSessionBindingBlock {
                worker_id: Some(session.worker_id.clone()),
                session_id: Some(session.session_id.clone()),
                profile: requested_profile.cloned().or(bound_profile),
                blocked_reason: BlockedReason::SessionBroken,
                message: "task references a non-reusable session".to_owned(),
                mark_session_broken: false,
            }));
        }
        if session.app_server_thread_id.is_none() {
            return Ok(TaskSessionBindingEvaluation::Blocked(TaskSessionBindingBlock {
                worker_id: Some(session.worker_id.clone()),
                session_id: Some(session.session_id.clone()),
                profile: requested_profile.cloned().or(bound_profile),
                blocked_reason: BlockedReason::SessionBroken,
                message: "session missing app-server thread id".to_owned(),
                mark_session_broken: true,
            }));
        }
        Ok(TaskSessionBindingEvaluation::Ready(session))
    }

    fn apply_task_binding_block(
        &self,
        task_id: &TaskId,
        blocked: TaskSessionBindingBlock,
    ) -> Result<()> {
        if let Some(event) = self.store.mark_task_blocked(
            task_id,
            blocked.worker_id.clone(),
            blocked.session_id.clone(),
            blocked.profile,
            blocked.blocked_reason,
            &blocked.message,
        )? {
            let _ = self.events.send(event);
        }
        if blocked.mark_session_broken {
            if let Some(session_id) = blocked.session_id {
                if let Some(event) =
                    self.store.mark_session_broken(&session_id, &blocked.message)?
                {
                    let _ = self.events.send(event);
                }
            }
        }
        Ok(())
    }

    fn preflight_task_create_block(
        &self,
        input: &CreateTaskInput,
    ) -> Result<Option<TaskSessionBindingBlock>> {
        let Some(session_id) = input.session_id.as_ref() else {
            return Ok(None);
        };
        match self.evaluate_task_session_binding(session_id, input.profile.as_ref())? {
            TaskSessionBindingEvaluation::Ready(_) => Ok(None),
            TaskSessionBindingEvaluation::Blocked(blocked) => Ok(Some(blocked)),
        }
    }

    fn dispatch_queued_tasks(&self) -> Result<()> {
        let Some(runtime) = self.runtime.as_ref() else {
            return Ok(());
        };
        let mut tasks = self.store.list_tasks()?;
        let sessions = self
            .store
            .list_sessions()?
            .into_iter()
            .map(|session| (session.session_id.clone(), session))
            .collect::<HashMap<_, _>>();
        let workers = self
            .store
            .list_workers()?
            .into_iter()
            .map(|worker| (worker.worker_id.clone(), worker))
            .collect::<HashMap<_, _>>();
        let profiles = self.store.list_profile_records()?;
        let local_profiles = default_local_profile_names();
        if self.requeue_profile_unavailable_tasks(&tasks, &sessions, &workers, &profiles)? {
            tasks = self.store.list_tasks()?;
        }
        let runs = self.store.list_runs()?;
        let runs_by_id =
            runs.into_iter().map(|run| (run.run_id.clone(), run)).collect::<HashMap<_, _>>();

        let active_sessions = runs_by_id
            .values()
            .filter(|run| is_active_run_status(run.status))
            .map(|run| run.session_id.clone())
            .collect::<HashSet<_>>();
        let mut session_queue_heads = HashMap::<SessionId, TaskId>::new();
        for task in tasks.iter().filter(|task| task.status == TaskStatus::Queued) {
            if let Some(session_id) = task.session_id.as_ref() {
                session_queue_heads
                    .entry(session_id.clone())
                    .or_insert_with(|| task.task_id.clone());
            }
        }
        let mut selected_profiles = BTreeSet::<ProfileName>::new();
        for task in tasks.into_iter().filter(|task| task.status == TaskStatus::Queued) {
            if let Some(latest_run_id) = task.latest_run_id.as_ref() {
                if let Some(run) = runs_by_id.get(latest_run_id) {
                    if is_active_run_status(run.status) {
                        continue;
                    }
                }
            }
            if let Some(session_id) = task.session_id.as_ref() {
                if session_queue_heads.get(session_id) != Some(&task.task_id) {
                    continue;
                }
                if active_sessions.contains(session_id) {
                    continue;
                }
                if !sessions.contains_key(session_id) {
                    if let Some(event) = self.store.mark_task_blocked(
                        &task.task_id,
                        None,
                        Some(session_id.clone()),
                        task.profile.clone(),
                        BlockedReason::SessionBroken,
                        "task references a missing session",
                    )? {
                        let _ = self.events.send(event);
                    }
                    continue;
                }
            }
            let candidates =
                task_profile_candidates(&task, &sessions, &workers, &profiles, &local_profiles);
            if candidates.is_empty() {
                if let Some(event) = self.store.mark_task_blocked(
                    &task.task_id,
                    None,
                    task.session_id.clone(),
                    None,
                    BlockedReason::ProfileUnavailable,
                    "task has no assignable profile candidates",
                )? {
                    let _ = self.events.send(event);
                }
                continue;
            }

            let can_fallback =
                task.session_id.is_none() && task.profile.is_none() && candidates.len() > 1;
            let mut attempted_any = false;
            for (index, profile) in candidates.iter().enumerate() {
                if selected_profiles.contains(profile) {
                    continue;
                }
                attempted_any = true;
                let current_task = self
                    .store
                    .inspect_task(&task.task_id)?
                    .ok_or_else(|| anyhow!("task missing during dispatch"))?;
                if current_task.status == TaskStatus::Blocked {
                    if let Some(event) = self.store.requeue_blocked_task(
                        &current_task.task_id,
                        Some(profile.clone()),
                        "task re-queued for next profile candidate",
                    )? {
                        let _ = self.events.send(event);
                    }
                }
                match self.try_dispatch_task(runtime.as_ref(), current_task, profile.clone())? {
                    DispatchAttemptOutcome::Started => {
                        selected_profiles.insert(profile.clone());
                        break;
                    }
                    DispatchAttemptOutcome::Blocked(reason)
                        if can_fallback
                            && can_try_next_profile(reason)
                            && index + 1 < candidates.len() =>
                    {
                        continue;
                    }
                    DispatchAttemptOutcome::Blocked(_) => break,
                }
            }
            if !attempted_any {
                continue;
            }
        }
        Ok(())
    }

    fn requeue_profile_unavailable_tasks(
        &self,
        tasks: &[TaskRecord],
        sessions: &HashMap<SessionId, SessionRecord>,
        workers: &HashMap<WorkerId, WorkerRecord>,
        profiles: &[ProfileRecord],
    ) -> Result<bool> {
        let mut requeued_any = false;
        for task in tasks {
            if task.status != TaskStatus::Blocked
                || task.blocked_reason != Some(BlockedReason::ProfileUnavailable)
            {
                continue;
            }
            let Some(profile) = task_profile_candidates(
                task,
                sessions,
                workers,
                profiles,
                &default_local_profile_names(),
            )
            .into_iter()
            .next() else {
                continue;
            };
            if let Some(event) = self.store.requeue_blocked_task(
                &task.task_id,
                Some(profile),
                "task re-queued after Nucleus selected a profile candidate",
            )? {
                let _ = self.events.send(event);
                requeued_any = true;
            }
        }
        Ok(requeued_any)
    }

    fn try_dispatch_task(
        &self,
        runtime: &dyn NucleusRuntime,
        task: TaskRecord,
        profile: ProfileName,
    ) -> Result<DispatchAttemptOutcome> {
        if let Some(event) = self.store.assign_task_profile(
            &task.task_id,
            &profile,
            "task assigned to profile candidate",
        )? {
            let _ = self.events.send(event);
        }
        let Some(session) = self.ensure_dispatch_session(runtime, &task, &profile)? else {
            let blocked_reason = self
                .store
                .inspect_task(&task.task_id)?
                .and_then(|task| task.blocked_reason)
                .unwrap_or(BlockedReason::WorkerUnavailable);
            return Ok(DispatchAttemptOutcome::Blocked(blocked_reason));
        };
        let worker = self
            .store
            .inspect_worker(&session.worker_id)?
            .ok_or_else(|| anyhow!("worker not found"))?;
        if self.enforce_fort_capability(&task, &session, &worker, &profile, None)?.is_some() {
            let blocked_reason = self
                .store
                .inspect_task(&task.task_id)?
                .and_then(|task| task.blocked_reason)
                .unwrap_or(BlockedReason::AuthRequired);
            return Ok(DispatchAttemptOutcome::Blocked(blocked_reason));
        }
        let run = self.store.claim_run_for_task(RunRecord::new(
            RunId::generate(),
            task.task_id.clone(),
            session.session_id.clone(),
        ))?;
        let run_spec = RunTurnSpec {
            run_id: run.run_id.clone(),
            task_id: Some(task.task_id.clone()),
            worker_id: session.worker_id.clone(),
            session_id: session.session_id.clone(),
            profile,
            thread_id: session
                .app_server_thread_id
                .clone()
                .ok_or_else(|| anyhow!("session missing app-server thread id"))?,
            timeout_seconds: task.timeout_seconds,
            input: vec![RunInputItem::Text { text: task.instructions }],
        };
        self.spawn_run_execution(runtime, run_spec);
        Ok(DispatchAttemptOutcome::Started)
    }

    fn enforce_fort_capability(
        &self,
        task: &TaskRecord,
        session: &SessionRecord,
        worker: &WorkerRecord,
        profile: &ProfileName,
        prompt_override: Option<&str>,
    ) -> Result<Option<String>> {
        let Some((state, message, payload)) =
            classify_fort_requirement(task, worker, profile, prompt_override)?
        else {
            return Ok(None);
        };

        let fort_event = self.store.append_aux_event(
            fort_capability_event_type(state),
            CanonicalEventSource::Fort,
            EventDataEnvelope {
                task_id: Some(task.task_id.clone()),
                worker_id: Some(worker.worker_id.clone()),
                session_id: Some(session.session_id.clone()),
                run_id: task.latest_run_id.clone(),
                profile: Some(profile.clone()),
                payload: merge_json_objects(payload, json!({ "message": message.clone() })),
            },
        )?;
        let _ = self.events.send(fort_event);

        if let Some(blocked_reason) = fort_blocked_reason(state) {
            if let Some(event) = self.store.mark_task_blocked(
                &task.task_id,
                Some(worker.worker_id.clone()),
                Some(session.session_id.clone()),
                Some(profile.clone()),
                blocked_reason,
                &message,
            )? {
                let _ = self.events.send(event);
            }
            return Ok(Some(message));
        }

        Ok(None)
    }

    fn ensure_dispatch_session(
        &self,
        runtime: &dyn NucleusRuntime,
        task: &TaskRecord,
        profile: &ProfileName,
    ) -> Result<Option<SessionRecord>> {
        if let Some(session_id) = task.session_id.as_ref() {
            let session = match self.evaluate_task_session_binding(session_id, Some(profile))? {
                TaskSessionBindingEvaluation::Ready(session) => session,
                TaskSessionBindingEvaluation::Blocked(blocked) => {
                    self.apply_task_binding_block(&task.task_id, blocked)?;
                    return Ok(None);
                }
            };
            let worker = self.ensure_worker_started(
                runtime,
                profile,
                EnsureWorkerRequest {
                    allow_exhausted_restart: false,
                    requested_worker_id: Some(session.worker_id.as_str()),
                    home_dir: None,
                    codex_home: None,
                    workdir: session.workdir.as_ref().map(PathBuf::from),
                    env: None,
                },
            )?;
            let thread_id = session
                .app_server_thread_id
                .clone()
                .ok_or_else(|| anyhow!("session missing app-server thread id"))?;
            let workdir =
                session.workdir.as_ref().map(PathBuf::from).unwrap_or_else(default_workdir);
            match runtime.ensure_session(&SessionOpenSpec {
                session_id: session.session_id.clone(),
                worker_id: worker.worker.worker_id.clone(),
                profile: profile.clone(),
                workdir,
                resume_thread_id: Some(thread_id.clone()),
            }) {
                Ok(_) => {
                    self.store.record_session_ready(
                        &session.session_id,
                        &worker.worker.worker_id,
                        profile,
                        &thread_id,
                    )?;
                    return self.store.inspect_session(&session.session_id);
                }
                Err(error) => {
                    self.apply_task_binding_block(
                        &task.task_id,
                        TaskSessionBindingBlock {
                            worker_id: Some(worker.worker.worker_id.clone()),
                            session_id: Some(session.session_id.clone()),
                            profile: Some(profile.clone()),
                            blocked_reason: BlockedReason::SessionBroken,
                            message: error.to_string(),
                            mark_session_broken: true,
                        },
                    )?;
                    return Ok(None);
                }
            }
        }

        if let Some(session) = self.find_reusable_session(runtime, profile)? {
            return Ok(Some(session));
        }

        let worker = match self.ensure_worker_started(
            runtime,
            profile,
            EnsureWorkerRequest {
                allow_exhausted_restart: false,
                requested_worker_id: None,
                home_dir: None,
                codex_home: None,
                workdir: None,
                env: None,
            },
        ) {
            Ok(worker) => worker,
            Err(error) => {
                if let Some(event) = self.store.mark_task_blocked(
                    &task.task_id,
                    None,
                    None,
                    Some(profile.clone()),
                    BlockedReason::WorkerUnavailable,
                    &error.to_string(),
                )? {
                    let _ = self.events.send(event);
                }
                return Ok(None);
            }
        };
        let workdir =
            worker.worker.workdir.as_ref().map(PathBuf::from).unwrap_or_else(default_workdir);
        let session_id = SessionId::generate();
        let opened = runtime.ensure_session(&SessionOpenSpec {
            session_id: session_id.clone(),
            worker_id: worker.worker.worker_id.clone(),
            profile: profile.clone(),
            workdir: workdir.clone(),
            resume_thread_id: None,
        })?;
        let (session, event) = self.store.record_session_open(
            session_id,
            worker.worker.worker_id.clone(),
            opened.thread_id,
            opened.created,
            profile.clone(),
            workdir,
        )?;
        let _ = self.events.send(event);
        Ok(Some(session))
    }

    fn cancel_task(&self, task_id: &TaskId) -> Result<TaskCancelResultView> {
        let task = self.store.inspect_task(task_id)?.ok_or_else(|| anyhow!("task not found"))?;
        let current_run = task
            .latest_run_id
            .as_ref()
            .map(|run_id| self.store.inspect_run(run_id))
            .transpose()?
            .flatten();

        if matches!(task.status, TaskStatus::Done | TaskStatus::Failed | TaskStatus::Cancelled) {
            return Ok(TaskCancelResultView {
                task,
                run: current_run,
                cancellation_requested: false,
            });
        }

        if let Some(run) = current_run.clone() {
            if run.status == RunStatus::Blocked {
                let session = self
                    .store
                    .inspect_session(&run.session_id)?
                    .ok_or_else(|| anyhow!("session not found"))?;
                let (task, run) = self.cancel_active_run_without_interrupt(
                    &task,
                    &run,
                    &session,
                    "run cancelled after becoming blocked",
                    false,
                )?;
                return Ok(TaskCancelResultView {
                    task,
                    run: Some(run),
                    cancellation_requested: false,
                });
            }
            if is_active_run_status(run.status) {
                let session = self
                    .store
                    .inspect_session(&run.session_id)?
                    .ok_or_else(|| anyhow!("session not found"))?;
                if let Some(turn_id) = run.app_server_turn_id.as_deref() {
                    let Some(thread_id) = session.app_server_thread_id.clone() else {
                        let (task, run) = self.cancel_active_run_without_interrupt(
                            &task,
                            &run,
                            &session,
                            "run cancelled after session lost app-server thread id",
                            true,
                        )?;
                        return Ok(TaskCancelResultView {
                            task,
                            run: Some(run),
                            cancellation_requested: false,
                        });
                    };
                    let runtime =
                        self.runtime.as_ref().ok_or_else(|| anyhow!("runtime unavailable"))?;
                    runtime.interrupt_turn(&session.worker_id, &thread_id, turn_id)?;
                    let task = self
                        .store
                        .inspect_task(task_id)?
                        .ok_or_else(|| anyhow!("task not found"))?;
                    let run = self.store.inspect_run(&run.run_id)?;
                    return Ok(TaskCancelResultView { task, run, cancellation_requested: true });
                }

                let (task, run) = self.cancel_active_run_without_interrupt(
                    &task,
                    &run,
                    &session,
                    "run cancelled before the worker reported turn start",
                    false,
                )?;
                return Ok(TaskCancelResultView {
                    task,
                    run: Some(run),
                    cancellation_requested: false,
                });
            }
        }

        if let Some((task, event)) = self.store.cancel_task_without_run(
            task_id,
            CanonicalEventSource::Nucleus,
            "task cancelled before run execution",
        )? {
            let _ = self.events.send(event);
            let run = task
                .latest_run_id
                .as_ref()
                .map(|run_id| self.store.inspect_run(run_id))
                .transpose()?
                .flatten();
            return Ok(TaskCancelResultView { task, run, cancellation_requested: false });
        }

        let task = self.store.inspect_task(task_id)?.ok_or_else(|| anyhow!("task not found"))?;
        let run = task
            .latest_run_id
            .as_ref()
            .map(|run_id| self.store.inspect_run(run_id))
            .transpose()?
            .flatten();
        Ok(TaskCancelResultView { task, run, cancellation_requested: false })
    }

    fn cancel_active_run_without_interrupt(
        &self,
        task: &TaskRecord,
        run: &RunRecord,
        session: &SessionRecord,
        message: &str,
        mark_session_broken: bool,
    ) -> Result<(TaskRecord, RunRecord)> {
        if mark_session_broken {
            if let Some(event) = self.store.mark_session_broken(&session.session_id, message)? {
                let _ = self.events.send(event);
            }
        }
        let events = self.store.apply_runtime_event(CanonicalEventDraft {
            event_type: CanonicalEventType::RunCancelled,
            source: CanonicalEventSource::Nucleus,
            data: EventDataEnvelope {
                task_id: Some(task.task_id.clone()),
                worker_id: Some(session.worker_id.clone()),
                session_id: Some(session.session_id.clone()),
                run_id: Some(run.run_id.clone()),
                profile: task.profile.clone().or_else(|| session.profile.clone()),
                payload: json!({
                    "message": message,
                    "thread_id": session.app_server_thread_id,
                    "turn_id": run.app_server_turn_id,
                }),
            },
        })?;
        for event in events {
            let _ = self.events.send(event);
        }
        let task =
            self.store.inspect_task(&task.task_id)?.ok_or_else(|| anyhow!("task not found"))?;
        let run = self.store.inspect_run(&run.run_id)?.ok_or_else(|| anyhow!("run not found"))?;
        Ok((task, run))
    }

    fn find_reusable_session(
        &self,
        runtime: &dyn NucleusRuntime,
        profile: &ProfileName,
    ) -> Result<Option<SessionRecord>> {
        let active_sessions = self
            .store
            .list_runs()?
            .into_iter()
            .filter(|run| is_active_run_status(run.status))
            .map(|run| run.session_id)
            .collect::<HashSet<_>>();
        for session in self.store.list_sessions()? {
            if session.profile.as_ref() != Some(profile) {
                continue;
            }
            if session.lifecycle_state != SessionLifecycleState::Ready {
                continue;
            }
            if active_sessions.contains(&session.session_id) {
                continue;
            }
            if session.app_server_thread_id.is_none() {
                continue;
            }
            let worker = match self.ensure_worker_started(
                runtime,
                profile,
                EnsureWorkerRequest {
                    allow_exhausted_restart: false,
                    requested_worker_id: Some(session.worker_id.as_str()),
                    home_dir: None,
                    codex_home: None,
                    workdir: session.workdir.as_ref().map(PathBuf::from),
                    env: None,
                },
            ) {
                Ok(worker) => worker,
                Err(_) => continue,
            };
            let workdir =
                session.workdir.as_ref().map(PathBuf::from).unwrap_or_else(default_workdir);
            let thread_id = session.app_server_thread_id.clone().expect("checked thread id");
            if runtime
                .ensure_session(&SessionOpenSpec {
                    session_id: session.session_id.clone(),
                    worker_id: worker.worker.worker_id.clone(),
                    profile: profile.clone(),
                    workdir,
                    resume_thread_id: Some(thread_id.clone()),
                })
                .is_ok()
            {
                self.store.record_session_ready(
                    &session.session_id,
                    &worker.worker.worker_id,
                    profile,
                    &thread_id,
                )?;
                return self.store.inspect_session(&session.session_id);
            }
        }
        Ok(None)
    }

    fn requeue_recoverable_blocked_tasks_for_profile(
        &self,
        profile: &ProfileName,
        allowed_reasons: &[BlockedReason],
        message: &str,
    ) -> Result<Vec<TaskId>> {
        let tasks = self.store.list_tasks()?;
        let sessions = self
            .store
            .list_sessions()?
            .into_iter()
            .map(|session| (session.session_id.clone(), session))
            .collect::<HashMap<_, _>>();
        let workers = self
            .store
            .list_workers()?
            .into_iter()
            .map(|worker| (worker.worker_id.clone(), worker))
            .collect::<HashMap<_, _>>();
        let mut requeued = Vec::new();
        for task in tasks {
            if task.status != TaskStatus::Blocked {
                continue;
            }
            let Some(reason) = task.blocked_reason else {
                continue;
            };
            if !allowed_reasons.contains(&reason) {
                continue;
            }
            if let Some(latest_run_id) = task.latest_run_id.as_ref() {
                if let Some(run) = self.store.inspect_run(latest_run_id)? {
                    if is_active_run_status(run.status) {
                        continue;
                    }
                }
            }
            if let Some(session_id) = task.session_id.as_ref() {
                let Some(session) = sessions.get(session_id) else {
                    continue;
                };
                if matches!(
                    session.lifecycle_state,
                    SessionLifecycleState::Broken | SessionLifecycleState::Closed
                ) || session.app_server_thread_id.is_none()
                {
                    continue;
                }
            }
            let Some(task_profile) = pinned_task_profile(&task, &sessions, &workers) else {
                continue;
            };
            if &task_profile != profile {
                continue;
            }
            if let Some(event) = self.store.requeue_blocked_task(
                &task.task_id,
                Some(task_profile.clone()),
                message,
            )? {
                let _ = self.events.send(event);
                requeued.push(task.task_id);
            }
        }

        Ok(requeued)
    }

    fn worker_launch_spec_from_record(&self, worker: &WorkerRecord) -> WorkerLaunchSpec {
        WorkerLaunchSpec {
            worker_id: worker.worker_id.clone(),
            profile: worker.profile.clone(),
            home_dir: worker.home_dir.as_ref().map(PathBuf::from).unwrap_or_else(default_home_dir),
            codex_home: PathBuf::from(worker.codex_home.clone()),
            workdir: worker.workdir.as_ref().map(PathBuf::from).unwrap_or_else(default_workdir),
            extra_env: worker.extra_env.clone(),
        }
    }

    fn worker_has_active_runs(&self, worker_id: &WorkerId) -> Result<bool> {
        for run in self.store.list_runs()? {
            if !is_active_run_status(run.status) {
                continue;
            }
            let Some(session) = self.store.inspect_session(&run.session_id)? else {
                continue;
            };
            if session.worker_id == *worker_id {
                return Ok(true);
            }
        }
        Ok(false)
    }

    fn restart_worker(&self, worker_id: &WorkerId) -> Result<WorkerInspectView> {
        let runtime = self.runtime.as_ref().ok_or_else(|| anyhow!("runtime unavailable"))?;
        let worker =
            self.store.inspect_worker(worker_id)?.ok_or_else(|| anyhow!("worker not found"))?;
        if self.worker_has_active_runs(worker_id)? {
            anyhow::bail!("worker has active runs; cancel or reconcile them before restart");
        }
        let spec = self.worker_launch_spec_from_record(&worker);
        if runtime.inspect_worker(worker_id)?.is_some() {
            runtime.stop_worker(worker_id)?;
        }
        let started = runtime.start_worker(&spec)?;
        let (worker, runtime_view, events) =
            self.store.record_worker_start(&spec, &started, runtime.as_ref())?;
        self.clear_worker_restart_state(worker_id);
        for event in events {
            let _ = self.events.send(event);
        }
        let _ = self.requeue_recoverable_blocked_tasks_for_profile(
            &worker.profile,
            &[BlockedReason::WorkerUnavailable],
            "task re-queued after worker restart",
        )?;
        Ok(WorkerInspectView { worker, runtime: Some(runtime_view) })
    }

    fn repair_worker_auth(&self, worker_id: &WorkerId) -> Result<WorkerInspectView> {
        let runtime = self.runtime.as_ref().ok_or_else(|| anyhow!("runtime unavailable"))?;
        let worker =
            self.store.inspect_worker(worker_id)?.ok_or_else(|| anyhow!("worker not found"))?;
        let spec = self.worker_launch_spec_from_record(&worker);
        let _ = self.ensure_worker_started(
            runtime.as_ref(),
            &worker.profile,
            EnsureWorkerRequest {
                allow_exhausted_restart: true,
                requested_worker_id: Some(worker.worker_id.as_str()),
                home_dir: Some(spec.home_dir.clone()),
                codex_home: Some(spec.codex_home.clone()),
                workdir: Some(spec.workdir.clone()),
                env: Some(spec.extra_env.clone()),
            },
        )?;
        let probe = runtime.probe_worker(&spec)?;
        let events = self.store.record_worker_probe(&spec, &probe, runtime.as_ref())?;
        self.clear_worker_restart_state(worker_id);
        for event in events {
            let _ = self.events.send(event);
        }
        let _ = self.requeue_recoverable_blocked_tasks_for_profile(
            &worker.profile,
            &[BlockedReason::AuthRequired, BlockedReason::FortUnavailable],
            "task re-queued after worker auth repair",
        )?;
        let worker = self
            .store
            .inspect_worker(worker_id)?
            .ok_or_else(|| anyhow!("worker missing after auth repair"))?;
        Ok(WorkerInspectView { runtime: self.store.inspect_worker_runtime(worker_id)?, worker })
    }

    fn spawn_run_execution(&self, _runtime: &dyn NucleusRuntime, run_spec: RunTurnSpec) {
        let store = Arc::clone(&self.store);
        let runtime = Arc::clone(self.runtime.as_ref().expect("runtime must exist for execution"));
        let events = self.events.clone();
        thread::spawn(move || {
            let mut sink = |draft: CanonicalEventDraft| -> Result<()> {
                let appended = store.apply_runtime_event(draft)?;
                for event in appended {
                    let _ = events.send(event);
                }
                Ok(())
            };
            if let Err(error) = runtime.execute_turn(&run_spec, &mut sink) {
                let error_message = error.to_string();
                let failure = CanonicalEventDraft {
                    event_type: CanonicalEventType::RunFailed,
                    source: CanonicalEventSource::System,
                    data: EventDataEnvelope {
                        task_id: run_spec.task_id.clone(),
                        worker_id: Some(run_spec.worker_id.clone()),
                        session_id: Some(run_spec.session_id.clone()),
                        run_id: Some(run_spec.run_id.clone()),
                        profile: Some(run_spec.profile.clone()),
                        payload: json!({
                            "thread_id": run_spec.thread_id,
                            "turn_id": Value::Null,
                            "error": error_message,
                        }),
                    },
                };
                if let Ok(appended) = store.apply_runtime_event(failure) {
                    for event in appended {
                        let _ = events.send(event);
                    }
                }
                if let Ok(Some(event)) =
                    store.mark_session_broken(&run_spec.session_id, &error_message)
                {
                    let _ = events.send(event);
                }
                if runtime_error_requires_worker_quarantine(&error_message) {
                    if let Ok(Some(event)) =
                        store.mark_worker_failed(&run_spec.worker_id, &error_message)
                    {
                        let _ = events.send(event);
                    }
                }
            }
        });
    }

    pub async fn dispatch_request(&self, request: GatewayRequest) -> GatewayResponse {
        self.dispatch_request_authorized(request, None).await
    }

    async fn dispatch_request_authorized(
        &self,
        request: GatewayRequest,
        bearer_token: Option<&str>,
    ) -> GatewayResponse {
        if let Err(error) = self.authorize_request_method(request.method.as_str(), bearer_token) {
            return GatewayResponse::err(
                request.id,
                infer_error_code(&error),
                error.to_string(),
                None,
            );
        }
        match self.handle_request(request.method.as_str(), request.params.clone()).await {
            Ok(result) => GatewayResponse::ok(request.id, result),
            Err(error) => {
                GatewayResponse::err(request.id, infer_error_code(&error), error.to_string(), None)
            }
        }
    }

    fn authorize_request_method(&self, _method: &str, bearer_token: Option<&str>) -> Result<()> {
        if !self.requires_gateway_auth() {
            return Ok(());
        }
        let expected = self.config.auth_token.as_deref().ok_or_else(|| {
            anyhow!(
                "unauthorized: SI_NUCLEUS_AUTH_TOKEN must be set when authentication is required"
            )
        })?;
        let provided = bearer_token
            .map(str::trim)
            .filter(|value| !value.is_empty())
            .ok_or_else(|| anyhow!("unauthorized: missing bearer token"))?;
        if provided != expected {
            anyhow::bail!("unauthorized: invalid bearer token");
        }
        Ok(())
    }

    fn requires_gateway_auth(&self) -> bool {
        self.config.auth_token.is_some() || !self.config.bind_addr.ip().is_loopback()
    }

    async fn handle_request(&self, method: &str, params: Value) -> Result<Value> {
        match method {
            "nucleus.status" => Ok(serde_json::to_value(self.status()?)?),
            "task.create" => {
                let params: TaskCreateParams =
                    serde_json::from_value(params).context("parse task.create params")?;
                let profile = match params.profile {
                    Some(value) => Some(ProfileName::new(value)?),
                    None => None,
                };
                let session_id = match params.session_id {
                    Some(value) => Some(SessionId::new(value)?),
                    None => None,
                };
                let input = CreateTaskInput {
                    title: params.title,
                    instructions: params.instructions,
                    source: params.source,
                    profile,
                    session_id,
                    max_retries: params.max_retries,
                    timeout_seconds: params.timeout_seconds,
                };
                self.validate_task_create_input(&input)?;
                let preflight_block = self.preflight_task_create_block(&input)?;
                let events = self.store.create_task(input)?;
                let task_id = events[0]
                    .data
                    .task_id
                    .clone()
                    .ok_or_else(|| anyhow!("task id missing after create"))?;
                for event in events {
                    let _ = self.events.send(event);
                }
                if let Some(blocked) = preflight_block {
                    self.apply_task_binding_block(&task_id, blocked)?;
                }
                let task = self
                    .store
                    .inspect_task(&task_id)?
                    .ok_or_else(|| anyhow!("task missing after create"))?;
                Ok(serde_json::to_value(task)?)
            }
            "task.list" => Ok(serde_json::to_value(self.store.list_tasks()?)?),
            "task.inspect" => {
                let params: TaskInspectParams =
                    serde_json::from_value(params).context("parse task.inspect params")?;
                let task_id = TaskId::new(params.task_id)?;
                match self.store.inspect_task(&task_id)? {
                    Some(task) => Ok(serde_json::to_value(task)?),
                    None => Err(anyhow!("task not found")),
                }
            }
            "task.cancel" => {
                let params: TaskCancelParams =
                    serde_json::from_value(params).context("parse task.cancel params")?;
                let task_id = TaskId::new(params.task_id)?;
                Ok(serde_json::to_value(self.cancel_task(&task_id)?)?)
            }
            "task.prune" => {
                let params: TaskPruneParams =
                    serde_json::from_value(params).context("parse task.prune params")?;
                let older_than_days = params.older_than_days.unwrap_or(DEFAULT_TASK_RETENTION_DAYS);
                let cutoff_at = Utc::now()
                    - ChronoDuration::days(
                        i64::try_from(older_than_days).context("task prune cutoff")?,
                    );
                let result = self.store.prune_tasks_older_than(cutoff_at)?;
                for skipped in &result.skipped {
                    let event = self.store.append_system_warning(
                        "skipped malformed task during explicit prune",
                        json!({
                            "path": skipped.path,
                            "error": skipped.error,
                        }),
                    )?;
                    let _ = self.events.send(event);
                }
                Ok(serde_json::to_value(TaskPruneResultView {
                    older_than_days,
                    cutoff_at,
                    pruned_task_ids: result.pruned_task_ids,
                    skipped: result.skipped,
                })?)
            }
            "profile.list" => Ok(serde_json::to_value(self.store.list_profiles()?)?),
            "producer.hook.list" => Ok(serde_json::to_value(self.store.list_hook_rules()?)?),
            "producer.hook.inspect" => {
                let params: HookRuleInspectParams =
                    serde_json::from_value(params).context("parse producer.hook.inspect params")?;
                let rule_name = normalize_hook_rule_name(&params.rule_name)?;
                match self.store.inspect_hook_rule(&rule_name)? {
                    Some(rule) => Ok(serde_json::to_value(rule)?),
                    None => Err(anyhow!("hook rule not found")),
                }
            }
            "producer.hook.upsert" => {
                let params: HookRuleUpsertParams =
                    serde_json::from_value(params).context("parse producer.hook.upsert params")?;
                let rule_name = normalize_hook_rule_name(&params.name)?;
                let match_event_type =
                    normalize_canonical_event_type_string(&params.match_event_type)?;
                let existing = self.store.inspect_hook_rule(&rule_name)?;
                let last_processed_event_seq = existing
                    .as_ref()
                    .map(|rule| rule.last_processed_event_seq)
                    .unwrap_or_else(|| self.store.next_event_seq().saturating_sub(1));
                let rule = HookRuleRecord {
                    name: rule_name,
                    enabled: params.enabled.unwrap_or_else(|| {
                        existing.as_ref().map(|rule| rule.enabled).unwrap_or(true)
                    }),
                    match_event_type,
                    instructions: params.instructions,
                    last_processed_event_seq,
                    version: current_persisted_version().to_owned(),
                };
                let rule = self.store.upsert_hook_rule(&rule)?;
                self.process_hook_producers()?;
                Ok(serde_json::to_value(rule)?)
            }
            "producer.hook.delete" => {
                let params: HookRuleDeleteParams =
                    serde_json::from_value(params).context("parse producer.hook.delete params")?;
                let rule_name = normalize_hook_rule_name(&params.rule_name)?;
                let deleted = self.store.delete_hook_rule(&rule_name)?;
                Ok(serde_json::to_value(HookRuleDeleteResultView { rule_name, deleted })?)
            }
            "worker.probe" => {
                let runtime =
                    self.runtime.as_ref().ok_or_else(|| anyhow!("runtime unavailable"))?;
                let params: WorkerProbeParams =
                    serde_json::from_value(params).context("parse worker.probe params")?;
                let profile_slug = params.profile_slug().to_owned();
                let profile = ProfileName::new(params.profile)?;
                let worker_id = match params.worker_id {
                    Some(value) => WorkerId::new(value)?,
                    None => WorkerId::generate(),
                };
                let spec = WorkerLaunchSpec {
                    worker_id,
                    profile,
                    home_dir: params.home_dir.unwrap_or_else(default_home_dir),
                    codex_home: params
                        .codex_home
                        .unwrap_or_else(|| default_codex_home_dir(&profile_slug)),
                    workdir: params.workdir.unwrap_or_else(default_workdir),
                    extra_env: params.env.unwrap_or_default(),
                };
                let probe = runtime.probe_worker(&spec)?;
                let events = self.store.record_worker_probe(&spec, &probe, runtime.as_ref())?;
                for event in events {
                    let _ = self.events.send(event);
                }
                let worker = self
                    .store
                    .inspect_worker(&spec.worker_id)?
                    .ok_or_else(|| anyhow!("worker missing after probe"))?;
                Ok(json!({
                    "worker": worker,
                    "probe": probe,
                    "command": runtime.build_worker_command(&spec),
                }))
            }
            "worker.list" => Ok(serde_json::to_value(self.store.list_workers()?)?),
            "worker.inspect" => {
                let params: WorkerInspectParams =
                    serde_json::from_value(params).context("parse worker.inspect params")?;
                let worker_id = WorkerId::new(params.worker_id)?;
                match self.store.inspect_worker(&worker_id)? {
                    Some(worker) => Ok(serde_json::to_value(WorkerInspectView {
                        runtime: self.store.inspect_worker_runtime(&worker_id)?,
                        worker,
                    })?),
                    None => Err(anyhow!("worker not found")),
                }
            }
            "worker.restart" => {
                let params: WorkerRestartParams =
                    serde_json::from_value(params).context("parse worker.restart params")?;
                let worker_id = WorkerId::new(params.worker_id)?;
                Ok(serde_json::to_value(self.restart_worker(&worker_id)?)?)
            }
            "worker.repair_auth" => {
                let params: WorkerRepairAuthParams =
                    serde_json::from_value(params).context("parse worker.repair_auth params")?;
                let worker_id = WorkerId::new(params.worker_id)?;
                Ok(serde_json::to_value(self.repair_worker_auth(&worker_id)?)?)
            }
            "session.create" => {
                let runtime =
                    self.runtime.as_ref().ok_or_else(|| anyhow!("runtime unavailable"))?;
                let params: SessionCreateParams =
                    serde_json::from_value(params).context("parse session.create params")?;
                let profile = ProfileName::new(params.profile)?;
                let workdir = params.workdir.unwrap_or_else(default_workdir);
                let worker = self.ensure_worker_started(
                    runtime.as_ref(),
                    &profile,
                    EnsureWorkerRequest {
                        allow_exhausted_restart: false,
                        requested_worker_id: params.worker_id.as_deref(),
                        home_dir: params.home_dir,
                        codex_home: params.codex_home,
                        workdir: Some(workdir.clone()),
                        env: params.env,
                    },
                )?;
                let session_id = SessionId::generate();
                let opened = runtime.ensure_session(&SessionOpenSpec {
                    session_id: session_id.clone(),
                    worker_id: worker.worker.worker_id.clone(),
                    profile: profile.clone(),
                    workdir: workdir.clone(),
                    resume_thread_id: params.thread_id,
                })?;
                let (session, event) = self.store.record_session_open(
                    session_id,
                    worker.worker.worker_id.clone(),
                    opened.thread_id,
                    opened.created,
                    profile,
                    workdir,
                )?;
                let _ = self.events.send(event);
                Ok(json!({
                    "worker": worker.worker,
                    "session": session,
                    "runtime": worker.runtime,
                }))
            }
            "session.list" => Ok(serde_json::to_value(self.store.list_sessions()?)?),
            "session.show" => {
                let params: SessionShowParams =
                    serde_json::from_value(params).context("parse session.show params")?;
                let session_id = SessionId::new(params.session_id)?;
                match self.store.inspect_session(&session_id)? {
                    Some(session) => Ok(serde_json::to_value(session)?),
                    None => Err(anyhow!("session not found")),
                }
            }
            "run.submit_turn" => {
                let runtime =
                    self.runtime.as_ref().ok_or_else(|| anyhow!("runtime unavailable"))?;
                let params: RunSubmitTurnParams =
                    serde_json::from_value(params).context("parse run.submit_turn params")?;
                let session_id = SessionId::new(params.session_id)?;
                let task_id = TaskId::new(params.task_id)?;
                let task =
                    self.store.inspect_task(&task_id)?.ok_or_else(|| anyhow!("task not found"))?;
                if let Some(bound_session_id) = task.session_id.as_ref() {
                    if bound_session_id != &session_id {
                        anyhow::bail!("task is bound to a different session");
                    }
                }
                let session =
                    match self.evaluate_task_session_binding(&session_id, task.profile.as_ref())? {
                        TaskSessionBindingEvaluation::Ready(session) => session,
                        TaskSessionBindingEvaluation::Blocked(blocked) => {
                            let message = blocked.message.clone();
                            self.apply_task_binding_block(&task.task_id, blocked)?;
                            anyhow::bail!(message);
                        }
                    };
                let worker = self
                    .store
                    .inspect_worker(&session.worker_id)?
                    .ok_or_else(|| anyhow!("worker not found"))?;
                let profile = worker.profile.clone();
                if let Some(latest_run_id) = task.latest_run_id.as_ref() {
                    if let Some(latest_run) = self.store.inspect_run(latest_run_id)? {
                        if is_active_run_status(latest_run.status) {
                            anyhow::bail!("task already has an active run");
                        }
                    }
                }
                if let Some(message) = self.enforce_fort_capability(
                    &task,
                    &session,
                    &worker,
                    &profile,
                    Some(&params.prompt),
                )? {
                    anyhow::bail!(message);
                }
                let thread_id = session
                    .app_server_thread_id
                    .clone()
                    .ok_or_else(|| anyhow!("session missing app-server thread id"))?;
                let run = RunRecord::new(RunId::generate(), task_id.clone(), session_id.clone());
                let run = self.store.claim_run_for_task(run)?;
                let run_spec = RunTurnSpec {
                    run_id: run.run_id.clone(),
                    task_id: Some(task_id),
                    worker_id: worker.worker_id.clone(),
                    session_id: session_id.clone(),
                    profile,
                    thread_id,
                    timeout_seconds: params.timeout_seconds,
                    input: vec![RunInputItem::Text { text: params.prompt }],
                };
                self.spawn_run_execution(runtime.as_ref(), run_spec);
                Ok(serde_json::to_value(run)?)
            }
            "run.inspect" => {
                let params: RunInspectParams =
                    serde_json::from_value(params).context("parse run.inspect params")?;
                let run_id = RunId::new(params.run_id)?;
                match self.store.inspect_run(&run_id)? {
                    Some(run) => Ok(serde_json::to_value(run)?),
                    None => Err(anyhow!("run not found")),
                }
            }
            "run.cancel" => {
                let params: RunCancelParams =
                    serde_json::from_value(params).context("parse run.cancel params")?;
                let run_id = RunId::new(params.run_id)?;
                let run =
                    self.store.inspect_run(&run_id)?.ok_or_else(|| anyhow!("run not found"))?;
                if run.status == RunStatus::Blocked {
                    let session = self
                        .store
                        .inspect_session(&run.session_id)?
                        .ok_or_else(|| anyhow!("session not found"))?;
                    let task = self
                        .store
                        .inspect_task(&run.task_id)?
                        .ok_or_else(|| anyhow!("task not found"))?;
                    let (_, run) = self.cancel_active_run_without_interrupt(
                        &task,
                        &run,
                        &session,
                        "run cancelled after becoming blocked",
                        false,
                    )?;
                    return Ok(serde_json::to_value(run)?);
                }
                if !is_active_run_status(run.status) {
                    return Ok(serde_json::to_value(run)?);
                }
                let session = self
                    .store
                    .inspect_session(&run.session_id)?
                    .ok_or_else(|| anyhow!("session not found"))?;
                let task = self
                    .store
                    .inspect_task(&run.task_id)?
                    .ok_or_else(|| anyhow!("task not found"))?;
                let Some(turn_id) = run.app_server_turn_id.clone() else {
                    let (_, run) = self.cancel_active_run_without_interrupt(
                        &task,
                        &run,
                        &session,
                        "run cancelled before the worker reported turn start",
                        false,
                    )?;
                    return Ok(serde_json::to_value(run)?);
                };
                let Some(thread_id) = session.app_server_thread_id.clone() else {
                    let (_, run) = self.cancel_active_run_without_interrupt(
                        &task,
                        &run,
                        &session,
                        "run cancelled after session lost app-server thread id",
                        true,
                    )?;
                    return Ok(serde_json::to_value(run)?);
                };
                let runtime =
                    self.runtime.as_ref().ok_or_else(|| anyhow!("runtime unavailable"))?;
                runtime.interrupt_turn(&session.worker_id, &thread_id, &turn_id)?;
                let run =
                    self.store.inspect_run(&run_id)?.ok_or_else(|| anyhow!("run not found"))?;
                Ok(serde_json::to_value(run)?)
            }
            "events.ingest" => {
                let params: EventIngestParams =
                    serde_json::from_value(params).context("parse events.ingest params")?;
                let event_type = parse_canonical_event_type(&params.event_type)?;
                let source = parse_canonical_event_source(&params.source)?;
                if event_type != CanonicalEventType::GithubNotification
                    || source != CanonicalEventSource::Github
                {
                    anyhow::bail!(
                        "events.ingest currently supports only type=github.notification with source=github"
                    );
                }
                let profile = match params.profile {
                    Some(value) => Some(ProfileName::new(value)?),
                    None => None,
                };
                let event = self.store.append_aux_event(
                    event_type,
                    source,
                    EventDataEnvelope {
                        task_id: None,
                        worker_id: None,
                        session_id: None,
                        run_id: None,
                        profile,
                        payload: params.payload,
                    },
                )?;
                let _ = self.events.send(event.clone());
                self.process_hook_producers()?;
                Ok(serde_json::to_value(event)?)
            }
            "events.subscribe" => Ok(json!({ "subscribed": true })),
            _ => Err(anyhow!("method not found: {method}")),
        }
    }

    fn ensure_worker_started(
        &self,
        runtime: &dyn NucleusRuntime,
        profile: &ProfileName,
        request: EnsureWorkerRequest<'_>,
    ) -> Result<EnsuredWorker> {
        let existing = if let Some(worker_id) = request.requested_worker_id {
            let worker_id = WorkerId::new(worker_id.to_owned())?;
            self.store.inspect_worker(&worker_id)?
        } else {
            self.store
                .list_workers()?
                .into_iter()
                .filter(|worker| worker.profile == *profile)
                .min_by(|left, right| left.worker_id.cmp(&right.worker_id))
        };
        let worker_id = existing
            .as_ref()
            .map(|worker| worker.worker_id.clone())
            .unwrap_or_else(WorkerId::generate);
        let profile_codex_home = self
            .store
            .list_profile_records()?
            .into_iter()
            .find(|record| record.profile == *profile)
            .and_then(|record| {
                let codex_home = record.codex_home.trim();
                (!codex_home.is_empty()).then(|| PathBuf::from(codex_home))
            });
        let home_dir = request
            .home_dir
            .or_else(|| {
                existing.as_ref().and_then(|worker| worker.home_dir.as_ref().map(PathBuf::from))
            })
            .unwrap_or_else(default_home_dir);
        let codex_home = request
            .codex_home
            .or_else(|| existing.as_ref().map(|worker| PathBuf::from(worker.codex_home.clone())))
            .or(profile_codex_home)
            .unwrap_or_else(|| default_codex_home_dir(profile.as_str()));
        let workdir = request
            .workdir
            .or_else(|| {
                existing.as_ref().and_then(|worker| worker.workdir.as_ref().map(PathBuf::from))
            })
            .unwrap_or_else(default_workdir);
        let extra_env = request
            .env
            .filter(|value| !value.is_empty())
            .or_else(|| existing.as_ref().map(|worker| worker.extra_env.clone()))
            .unwrap_or_default();
        let spec = WorkerLaunchSpec {
            worker_id: worker_id.clone(),
            profile: profile.clone(),
            home_dir,
            codex_home,
            workdir,
            extra_env,
        };

        let live_runtime = runtime.inspect_worker(&worker_id)?;
        if let (Some(worker), Some(runtime_view)) = (existing.clone(), live_runtime) {
            return Ok(EnsuredWorker { worker, runtime: Some(runtime_view) });
        }
        if let Some(worker) = existing.as_ref() {
            if !request.allow_exhausted_restart
                && self.worker_restart_exhausted(&worker.worker_id)?
            {
                anyhow::bail!(
                    "worker restart attempts exhausted; explicit worker.restart or worker.repair_auth required"
                );
            }
        }

        let started = runtime.start_worker(&spec)?;
        let (worker, runtime_view, events) =
            self.store.record_worker_start(&spec, &started, runtime)?;
        self.clear_worker_restart_state(&worker.worker_id);
        for event in events {
            let _ = self.events.send(event);
        }
        Ok(EnsuredWorker { worker, runtime: Some(runtime_view) })
    }

    fn maybe_restart_failed_worker(
        &self,
        runtime: &dyn NucleusRuntime,
        worker: &WorkerRecord,
    ) -> Result<()> {
        if self.worker_has_active_runs(&worker.worker_id)? {
            return Ok(());
        }
        let now = Utc::now();
        {
            let state = self
                .worker_restart_state
                .lock()
                .map_err(|_| anyhow!("worker restart state lock poisoned"))?;
            if let Some(restart) = state.get(&worker.worker_id) {
                if restart.exhausted {
                    return Ok(());
                }
                if let Some(next_retry_at) = restart.next_retry_at {
                    if now < next_retry_at {
                        return Ok(());
                    }
                }
            }
        }

        let spec = self.worker_launch_spec_from_record(worker);
        match runtime.start_worker(&spec) {
            Ok(started) => {
                let (worker, _, events) =
                    self.store.record_worker_start(&spec, &started, runtime)?;
                self.clear_worker_restart_state(&worker.worker_id);
                for event in events {
                    let _ = self.events.send(event);
                }
                Ok(())
            }
            Err(error) => {
                let restart = self.record_worker_restart_failure(&worker.worker_id, now)?;
                let next_retry_at = restart
                    .next_retry_at
                    .map(|timestamp| timestamp.to_rfc3339_opts(SecondsFormat::Secs, true));
                let event = self.store.append_system_warning(
                    if restart.exhausted {
                        "worker restart attempts exhausted"
                    } else {
                        "worker auto-restart attempt failed"
                    },
                    json!({
                        "worker_id": worker.worker_id,
                        "attempt": restart.attempts,
                        "max_attempts": MAX_WORKER_RESTART_ATTEMPTS,
                        "next_retry_at": next_retry_at,
                        "error": error.to_string(),
                    }),
                )?;
                let _ = self.events.send(event);
                Ok(())
            }
        }
    }

    fn clear_worker_restart_state(&self, worker_id: &WorkerId) {
        if let Ok(mut state) = self.worker_restart_state.lock() {
            state.remove(worker_id);
        }
    }

    fn worker_restart_exhausted(&self, worker_id: &WorkerId) -> Result<bool> {
        let state = self
            .worker_restart_state
            .lock()
            .map_err(|_| anyhow!("worker restart state lock poisoned"))?;
        Ok(state.get(worker_id).is_some_and(|restart| restart.exhausted))
    }

    fn record_worker_restart_failure(
        &self,
        worker_id: &WorkerId,
        now: DateTime<Utc>,
    ) -> Result<WorkerRestartState> {
        let mut state = self
            .worker_restart_state
            .lock()
            .map_err(|_| anyhow!("worker restart state lock poisoned"))?;
        let restart = state.entry(worker_id.clone()).or_default();
        restart.attempts += 1;
        if restart.attempts >= MAX_WORKER_RESTART_ATTEMPTS {
            restart.next_retry_at = None;
            restart.exhausted = true;
        } else {
            restart.next_retry_at = Some(now + worker_restart_backoff(restart.attempts));
        }
        Ok(restart.clone())
    }
}

#[derive(Clone, Debug)]
struct EnsuredWorker {
    worker: WorkerRecord,
    runtime: Option<WorkerRuntimeView>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum DispatchAttemptOutcome {
    Started,
    Blocked(BlockedReason),
}

struct BlockedRunReconciliation {
    worker_id: Option<WorkerId>,
    session_id: Option<SessionId>,
    profile: Option<ProfileName>,
    blocked_reason: BlockedReason,
    message: String,
    mark_session_broken: bool,
}

enum TaskSessionBindingEvaluation {
    Ready(SessionRecord),
    Blocked(TaskSessionBindingBlock),
}

struct TaskSessionBindingBlock {
    worker_id: Option<WorkerId>,
    session_id: Option<SessionId>,
    profile: Option<ProfileName>,
    blocked_reason: BlockedReason,
    message: String,
    mark_session_broken: bool,
}

struct EnsureWorkerRequest<'a> {
    allow_exhausted_restart: bool,
    requested_worker_id: Option<&'a str>,
    home_dir: Option<PathBuf>,
    codex_home: Option<PathBuf>,
    workdir: Option<PathBuf>,
    env: Option<BTreeMap<String, String>>,
}

#[derive(Clone, Debug, Serialize)]
struct TaskPruneOutcome {
    pruned_task_ids: Vec<TaskId>,
    skipped: Vec<TaskPruneSkipView>,
}

#[derive(Clone, Debug, Serialize)]
struct TaskPruneResultView {
    older_than_days: u64,
    cutoff_at: DateTime<Utc>,
    pruned_task_ids: Vec<TaskId>,
    skipped: Vec<TaskPruneSkipView>,
}

#[derive(Clone, Debug, Serialize)]
struct TaskPruneSkipView {
    path: String,
    error: String,
}

#[derive(Clone, Debug, Default)]
struct WorkerRestartState {
    attempts: u32,
    next_retry_at: Option<DateTime<Utc>>,
    exhausted: bool,
}

#[derive(Clone, Debug, Default)]
struct BackgroundWarningState {
    last_error: Option<String>,
    last_emitted_at: Option<DateTime<Utc>>,
    suppressed: u64,
}

async fn ws_handler(
    ws: WebSocketUpgrade,
    State(service): State<Arc<NucleusService>>,
    headers: HeaderMap,
) -> impl IntoResponse {
    let bearer_token = extract_bearer_token(&headers);
    ws.on_upgrade(move |socket| async move {
        let _ = handle_socket(service, socket, bearer_token).await;
    })
}

async fn handle_socket(
    service: Arc<NucleusService>,
    socket: WebSocket,
    bearer_token: Option<String>,
) -> Result<()> {
    let (mut sender, mut receiver) = socket.split();
    let mut subscription: Option<broadcast::Receiver<CanonicalEvent>> = None;

    loop {
        tokio::select! {
            incoming = receiver.next() => {
                let Some(message) = incoming else {
                    break;
                };
                match message? {
                    Message::Text(text) => {
                        let request: GatewayRequest =
                            serde_json::from_str(&text).context("parse websocket gateway request")?;
                        let subscribe_requested = request.method == "events.subscribe";
                        let response =
                            service.dispatch_request_authorized(request, bearer_token.as_deref()).await;
                        sender
                            .send(Message::Text(serde_json::to_string(&response)?.into()))
                            .await
                            .context("send websocket gateway response")?;
                        if subscribe_requested {
                            subscription = Some(service.events.subscribe());
                        }
                    }
                    Message::Close(_) => break,
                    Message::Ping(bytes) => {
                        sender.send(Message::Pong(bytes)).await.context("send websocket pong")?;
                    }
                    Message::Binary(_) | Message::Pong(_) => {}
                }
            }
            event = recv_event(&mut subscription), if subscription.is_some() => {
                if let Some(event) = event {
                    sender
                        .send(Message::Text(serde_json::to_string(&event)?.into()))
                        .await
                        .context("send websocket event")?;
                }
            }
        }
    }

    Ok(())
}

async fn recv_event(
    subscription: &mut Option<broadcast::Receiver<CanonicalEvent>>,
) -> Option<CanonicalEvent> {
    match subscription.as_mut() {
        Some(receiver) => loop {
            match receiver.recv().await {
                Ok(event) => return Some(event),
                Err(broadcast::error::RecvError::Lagged(_)) => continue,
                Err(broadcast::error::RecvError::Closed) => return None,
            }
        },
        None => pending::<Option<CanonicalEvent>>().await,
    }
}

fn parse_canonical_event_type(value: &str) -> Result<CanonicalEventType> {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        anyhow::bail!("canonical event type cannot be empty");
    }
    serde_json::from_value(json!(trimmed))
        .with_context(|| format!("unknown canonical event type: {trimmed}"))
}

fn normalize_canonical_event_type_string(value: &str) -> Result<String> {
    Ok(parse_canonical_event_type(value)?.as_str().to_owned())
}

fn parse_canonical_event_source(value: &str) -> Result<CanonicalEventSource> {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        anyhow::bail!("canonical event source cannot be empty");
    }
    serde_json::from_value(json!(trimmed))
        .with_context(|| format!("unknown canonical event source: {trimmed}"))
}

fn normalize_hook_rule_name(value: &str) -> Result<String> {
    Ok(ProfileName::new(value.trim().to_owned())?.to_string())
}

fn rest_request(method: &str, params: Value) -> GatewayRequest {
    GatewayRequest { id: json!(method), method: method.to_owned(), params }
}

fn rest_status_from_gateway_code(code: &str) -> StatusCode {
    match code {
        "unauthorized" => StatusCode::UNAUTHORIZED,
        "invalid_params" => StatusCode::BAD_REQUEST,
        "not_found" | "method_not_found" => StatusCode::NOT_FOUND,
        "unavailable" => StatusCode::SERVICE_UNAVAILABLE,
        _ => StatusCode::INTERNAL_SERVER_ERROR,
    }
}

fn rest_gateway_response(response: GatewayResponse, success_status: StatusCode) -> Response {
    if response.ok {
        return (success_status, Json(response.result.unwrap_or_else(|| json!({}))))
            .into_response();
    }

    let error = response.error.unwrap_or(GatewayErrorObject {
        code: "internal_error".to_owned(),
        message: "request failed".to_owned(),
        details: None,
    });
    let status = rest_status_from_gateway_code(&error.code);
    (
        status,
        Json(json!({
            "error": {
                "code": error.code,
                "message": error.message,
                "details": error.details,
            }
        })),
    )
        .into_response()
}

fn schema_ref(name: &str) -> Value {
    json!({ "$ref": format!("#/components/schemas/{name}") })
}

fn openapi_json_content(schema: Value) -> Value {
    json!({ "application/json": { "schema": schema } })
}

fn openapi_json_content_example(schema: Value, example: Value) -> Value {
    json!({ "application/json": { "schema": schema, "example": example } })
}

fn openapi_id_schema(prefix: &str, example: &str) -> Value {
    json!({
        "type": "string",
        "pattern": format!("^{prefix}.+"),
        "example": example
    })
}

fn openapi_path_id_parameter(name: &str, description: &str, prefix: &str, example: &str) -> Value {
    json!({
        "name": name,
        "in": "path",
        "required": true,
        "schema": openapi_id_schema(prefix, example),
        "description": description,
        "example": example
    })
}

fn task_record_example() -> Value {
    json!({
        "task_id": "si-task-0123456789abcdef00000001",
        "source": "websocket",
        "title": "Audit Nucleus tasks",
        "instructions": "Inspect queued and running tasks and report any items that are stuck.",
        "status": "queued",
        "profile": "darmstada",
        "session_id": null,
        "latest_run_id": null,
        "checkpoint_summary": null,
        "checkpoint_at": null,
        "checkpoint_seq": null,
        "parent_task_id": null,
        "producer_rule_name": null,
        "producer_dedup_key": null,
        "blocked_reason": null,
        "created_at": "2026-04-17T00:00:00Z",
        "updated_at": "2026-04-17T00:00:00Z",
        "attempt_count": 1,
        "max_retries": 0,
        "session_binding_locked": false,
        "timeout_seconds": 900
    })
}

fn run_record_example() -> Value {
    json!({
        "run_id": "si-run-0123456789abcdef00000001",
        "task_id": "si-task-0123456789abcdef00000001",
        "session_id": "si-session-0123456789abcdef00000001",
        "status": "running",
        "started_at": "2026-04-17T00:00:05Z",
        "ended_at": null,
        "parent_run_id": null,
        "app_server_turn_id": null
    })
}

fn worker_record_example() -> Value {
    json!({
        "worker_id": "si-worker-0123456789abcdef00000001",
        "profile": "darmstada",
        "home_dir": "/home/si",
        "codex_home": "/home/si/.codex",
        "workdir": "/home/si/Development/si",
        "extra_env": {},
        "status": "ready",
        "capability_version": env!("CARGO_PKG_VERSION"),
        "last_heartbeat_at": "2026-04-17T00:00:00Z",
        "effective_account_state": "authenticated"
    })
}

fn session_record_example() -> Value {
    json!({
        "session_id": "si-session-0123456789abcdef00000001",
        "worker_id": "si-worker-0123456789abcdef00000001",
        "profile": "darmstada",
        "app_server_thread_id": "thread_0123456789abcdef",
        "workdir": "/home/si/Development/si",
        "lifecycle_state": "ready",
        "summary_state": "idle",
        "created_at": "2026-04-17T00:00:00Z",
        "updated_at": "2026-04-17T00:00:00Z"
    })
}

fn openapi_id_property(description: &str, prefix: &str, example: &str) -> Value {
    let mut schema = openapi_id_schema(prefix, example);
    if let Some(object) = schema.as_object_mut() {
        object.insert("description".to_owned(), json!(description));
        object.insert("readOnly".to_owned(), json!(true));
    }
    schema
}

fn openapi_nullable_id_property(description: &str, prefix: &str, example: Value) -> Value {
    json!({
        "type": ["string", "null"],
        "description": description,
        "pattern": format!("^{prefix}.+"),
        "example": example,
        "readOnly": true
    })
}

fn openapi_document_schema() -> Value {
    json!({
        "type": "object",
        "description": "Public OpenAPI document returned by this endpoint.",
        "additionalProperties": true,
        "required": ["openapi", "info", "servers", "paths"],
        "properties": {
            "openapi": {
                "type": "string",
                "description": "OpenAPI specification version.",
                "example": "3.1.0"
            },
            "info": {
                "type": "object",
                "description": "API metadata.",
                "additionalProperties": true,
                "required": ["title", "version"],
                "properties": {
                    "title": {
                        "type": "string",
                        "description": "API title.",
                        "example": "SI Nucleus REST API"
                    },
                    "version": {
                        "type": "string",
                        "description": "API package version.",
                        "example": env!("CARGO_PKG_VERSION")
                    },
                    "description": {
                        "type": "string",
                        "description": "API description."
                    }
                }
            },
            "servers": {
                "type": "array",
                "description": "Absolute server URLs clients can call.",
                "items": {
                    "type": "object",
                    "additionalProperties": true,
                    "required": ["url"],
                    "properties": {
                        "url": {
                            "type": "string",
                            "description": "Absolute API base URL.",
                            "format": "uri",
                            "example": "https://nucleus.aureuma.ai"
                        }
                    }
                }
            },
            "paths": {
                "type": "object",
                "description": "OpenAPI path item map.",
                "additionalProperties": true,
                "properties": {}
            },
            "components": {
                "type": "object",
                "description": "OpenAPI reusable components.",
                "additionalProperties": true,
                "properties": {}
            },
            "security": {
                "type": "array",
                "description": "Default security requirements.",
                "items": {
                    "type": "object",
                    "additionalProperties": true,
                    "properties": {}
                }
            }
        }
    })
}

fn openapi_header_value<'a>(headers: &'a HeaderMap, name: &str) -> Option<&'a str> {
    headers
        .get(name)?
        .to_str()
        .ok()?
        .split(',')
        .next()
        .map(str::trim)
        .filter(|value| !value.is_empty())
}

fn openapi_server_url(headers: &HeaderMap) -> String {
    if let Ok(value) = env::var("SI_NUCLEUS_PUBLIC_URL") {
        let trimmed = value.trim().trim_end_matches('/');
        if !trimmed.is_empty() {
            return trimmed.to_owned();
        }
    }

    let Some(host) = openapi_header_value(headers, "x-forwarded-host")
        .or_else(|| openapi_header_value(headers, "host"))
    else {
        return "/".to_owned();
    };
    let proto = openapi_header_value(headers, "x-forwarded-proto").unwrap_or_else(|| {
        if host.starts_with("127.0.0.1")
            || host.starts_with("localhost")
            || host.starts_with("[::1]")
        {
            "http"
        } else {
            "https"
        }
    });
    format!("{}://{}", proto.trim_end_matches(':'), host)
}

fn openapi_document_with_server_url(server_url: &str) -> Value {
    let task_example = task_record_example();
    let run_example = run_record_example();
    let worker_example = worker_record_example();
    let session_example = session_record_example();

    json!({
        "openapi": "3.1.0",
        "info": {
            "title": "SI Nucleus REST API",
            "version": env!("CARGO_PKG_VERSION"),
            "description": "Bounded external integration API over the canonical SI Nucleus task, worker, session, and run model."
        },
        "servers": [
            { "url": server_url }
        ],
        "security": [{ "bearerAuth": [] }],
        "paths": {
            "/status": {
                "get": {
                    "operationId": "getNucleusStatus",
                    "summary": "Inspect Nucleus status",
                    "description": "Read the current Nucleus status projection, including bind address, state directory, and durable object counts.",
                    "x-si-purpose": "Use this for bounded external health and topology inspection without opening the websocket control plane.",
                    "security": [{ "bearerAuth": [] }],
                    "responses": {
                        "200": {
                            "description": "Current nucleus status.",
                            "content": openapi_json_content_example(schema_ref("NucleusStatusView"), json!({
                                "version": env!("CARGO_PKG_VERSION"),
                                "bind_addr": "0.0.0.0:4747",
                                "ws_url": "ws://0.0.0.0:4747/ws",
                                "state_dir": "/home/si/.local/state/si/nucleus",
                                "task_count": 12,
                                "worker_count": 2,
                                "session_count": 2,
                                "run_count": 8,
                                "next_event_seq": 128
                            }))
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        }
                    }
                }
            },
            "/tasks": {
                "get": {
                    "operationId": "listTasks",
                    "summary": "List tasks",
                    "description": "List durable tasks from the same source of truth used by the websocket gateway and CLI.",
                    "x-si-purpose": "Use this for bounded task inspection and polling from external tools such as GPT Actions.",
                    "security": [{ "bearerAuth": [] }],
                    "responses": {
                        "200": {
                            "description": "All durable tasks.",
                            "content": openapi_json_content_example(
                                json!({ "type": "array", "items": schema_ref("TaskRecord") }),
                                json!([task_example.clone()])
                            )
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        }
                    }
                },
                "post": {
                    "operationId": "createTask",
                    "summary": "Create a task",
                    "description": "Create a durable task through Nucleus so it can be routed, executed, and observed through the canonical control plane.",
                    "x-si-purpose": "Use this to create bounded external work without bypassing Nucleus task intake rules.",
                    "x-openai-isConsequential": true,
                    "security": [{ "bearerAuth": [] }],
                    "requestBody": {
                        "required": true,
                        "description": "Task intake request. Provide a concise title and complete operator instructions; optionally provide a preferred profile, but Nucleus can assign or fall back to another available profile when no session is pinned.",
                        "content": {
                            "application/json": {
                                "schema": schema_ref("TaskCreateParams"),
                                "example": {
                                    "title": "Audit Nucleus tasks",
                                    "instructions": "Inspect queued and running tasks and report any items that are stuck.",
                                    "source": "websocket",
                                    "profile": "darmstada",
                                    "session_id": null,
                                    "max_retries": 0,
                                    "timeout_seconds": 900
                                }
                            }
                        }
                    },
                    "responses": {
                        "201": {
                            "description": "Created task.",
                            "content": openapi_json_content_example(schema_ref("TaskRecord"), task_example.clone())
                        },
                        "401": {
                            "description": "Bearer token missing or invalid for an authenticated request.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        },
                        "400": {
                            "description": "Invalid request.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        }
                    }
                }
            },
            "/tasks/{task_id}": {
                "get": {
                    "operationId": "inspectTask",
                    "summary": "Inspect one task",
                    "description": "Read one durable task projection by task id.",
                    "x-si-purpose": "Use this to inspect bounded task state from external tooling.",
                    "security": [{ "bearerAuth": [] }],
                    "parameters": [
                        openapi_path_id_parameter(
                            "task_id",
                            "Canonical SI task id returned by task creation or task listing.",
                            "si-task-",
                            "si-task-0123456789abcdef00000001"
                        )
                    ],
                    "responses": {
                        "200": {
                            "description": "Task record.",
                            "content": openapi_json_content_example(schema_ref("TaskRecord"), task_example.clone())
                        },
                        "404": {
                            "description": "Task not found.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        }
                    }
                }
            },
            "/tasks/{task_id}/cancel": {
                "post": {
                    "operationId": "cancelTask",
                    "summary": "Cancel one task",
                    "description": "Request cancellation for a task through Nucleus. Queued tasks cancel immediately; active runs are interrupted through the runtime when needed.",
                    "x-si-purpose": "Use this for bounded external cancellation requests and then re-read the task or run to observe final state.",
                    "x-openai-isConsequential": true,
                    "security": [{ "bearerAuth": [] }],
                    "parameters": [
                        openapi_path_id_parameter(
                            "task_id",
                            "Canonical SI task id returned by task creation or task listing.",
                            "si-task-",
                            "si-task-0123456789abcdef00000001"
                        )
                    ],
                    "responses": {
                        "200": {
                            "description": "Cancellation result.",
                            "content": openapi_json_content_example(schema_ref("TaskCancelResultView"), json!({
                                "task": task_example.clone(),
                                "run": run_example.clone(),
                                "cancellation_requested": true
                            }))
                        },
                        "401": {
                            "description": "Bearer token missing or invalid for an authenticated request.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        },
                        "404": {
                            "description": "Task not found.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        },
                        "503": {
                            "description": "Runtime unavailable for active-run cancellation.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        }
                    }
                }
            },
            "/workers": {
                "get": {
                    "operationId": "listWorkers",
                    "summary": "List workers",
                    "description": "List durable worker records tracked by Nucleus.",
                    "x-si-purpose": "Use this for bounded worker inspection without relying on tmux or direct runtime internals.",
                    "security": [{ "bearerAuth": [] }],
                    "responses": {
                        "200": {
                            "description": "All durable workers.",
                            "content": openapi_json_content_example(
                                json!({ "type": "array", "items": schema_ref("WorkerRecord") }),
                                json!([worker_example.clone()])
                            )
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        }
                    }
                }
            },
            "/workers/{worker_id}": {
                "get": {
                    "operationId": "inspectWorker",
                    "summary": "Inspect one worker",
                    "description": "Read one worker projection, including persisted runtime view when available.",
                    "x-si-purpose": "Use this to inspect worker assignment and runtime attachment through the Nucleus model.",
                    "security": [{ "bearerAuth": [] }],
                    "parameters": [
                        openapi_path_id_parameter(
                            "worker_id",
                            "Canonical SI worker id returned by worker listing.",
                            "si-worker-",
                            "si-worker-0123456789abcdef00000001"
                        )
                    ],
                    "responses": {
                        "200": {
                            "description": "Worker inspect view.",
                            "content": openapi_json_content_example(schema_ref("WorkerInspectView"), json!({
                                "worker": worker_example.clone(),
                                "runtime": {
                                    "worker_id": "si-worker-0123456789abcdef00000001",
                                    "runtime_name": "codex",
                                    "pid": 12345,
                                    "started_at": "2026-04-17T00:00:00Z",
                                    "checked_at": "2026-04-17T00:00:10Z"
                                }
                            }))
                        },
                        "404": {
                            "description": "Worker not found.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        }
                    }
                }
            },
            "/sessions/{session_id}": {
                "get": {
                    "operationId": "inspectSession",
                    "summary": "Inspect one session",
                    "description": "Read one durable session projection by session id.",
                    "x-si-purpose": "Use this to inspect worker/session binding and reusable thread identity from external tooling.",
                    "security": [{ "bearerAuth": [] }],
                    "parameters": [
                        openapi_path_id_parameter(
                            "session_id",
                            "Canonical SI session id returned by session or run inspection.",
                            "si-session-",
                            "si-session-0123456789abcdef00000001"
                        )
                    ],
                    "responses": {
                        "200": {
                            "description": "Session record.",
                            "content": openapi_json_content_example(schema_ref("SessionRecord"), session_example.clone())
                        },
                        "404": {
                            "description": "Session not found.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        }
                    }
                }
            },
            "/runs/{run_id}": {
                "get": {
                    "operationId": "inspectRun",
                    "summary": "Inspect one run",
                    "description": "Read one durable run projection by run id.",
                    "x-si-purpose": "Use this to inspect bounded run state from external tools without subscribing to websocket events.",
                    "security": [{ "bearerAuth": [] }],
                    "parameters": [
                        openapi_path_id_parameter(
                            "run_id",
                            "Canonical SI run id returned by task or session inspection.",
                            "si-run-",
                            "si-run-0123456789abcdef00000001"
                        )
                    ],
                    "responses": {
                        "200": {
                            "description": "Run record.",
                            "content": openapi_json_content_example(schema_ref("RunRecord"), run_example.clone())
                        },
                        "404": {
                            "description": "Run not found.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        }
                    }
                }
            },
            "/openapi.json": {
                "get": {
                    "operationId": "getOpenApiDocument",
                    "summary": "Fetch the OpenAPI document",
                    "description": "Read the public OpenAPI-compatible REST description for bounded external integrations.",
                    "x-si-purpose": "Use this unauthenticated endpoint to bootstrap GPT Actions or other external tool clients against the bounded REST surface.",
                    "security": [],
                    "responses": {
                        "200": {
                            "description": "Public OpenAPI document.",
                            "content": openapi_json_content(schema_ref("OpenApiDocumentView"))
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": openapi_json_content(schema_ref("RestErrorEnvelope"))
                        }
                    }
                }
            }
        },
        "components": {
            "securitySchemes": {
                "bearerAuth": {
                    "type": "http",
                    "scheme": "bearer",
                    "bearerFormat": "opaque token",
                    "description": "Opaque bearer token provided by SI Fort for Nucleus REST access."
                }
            },
            "schemas": {
                "OpenApiDocumentView": openapi_document_schema(),
                "RestErrorEnvelope": {
                    "type": "object",
                    "description": "Standard REST error envelope returned when a Nucleus request fails.",
                    "additionalProperties": false,
                    "required": ["error"],
                    "properties": {
                        "error": {
                            "type": "object",
                            "description": "Machine-readable error details.",
                            "additionalProperties": false,
                            "required": ["code", "message"],
                            "properties": {
                                "code": { "type": "string", "description": "Stable Nucleus error code.", "example": "not_found" },
                                "message": { "type": "string", "description": "Human-readable error summary.", "example": "task not found" },
                                "details": { "description": "Optional structured details for debugging the request." }
                            }
                        }
                    }
                },
                "NucleusStatusView": {
                    "type": "object",
                    "description": "Current Nucleus status projection and durable object counts.",
                    "additionalProperties": false,
                    "required": ["version", "bind_addr", "ws_url", "state_dir", "task_count", "worker_count", "session_count", "run_count", "next_event_seq"],
                    "properties": {
                        "version": { "type": "string", "description": "Running SI Nucleus package version.", "example": env!("CARGO_PKG_VERSION"), "readOnly": true },
                        "bind_addr": { "type": "string", "description": "TCP address where Nucleus listens.", "example": "0.0.0.0:4747", "readOnly": true },
                        "ws_url": { "type": "string", "description": "Websocket URL exposed by the same Nucleus service.", "example": "ws://0.0.0.0:4747/ws", "readOnly": true },
                        "state_dir": { "type": "string", "description": "Filesystem directory that stores durable Nucleus state.", "example": "/home/si/.local/state/si/nucleus", "readOnly": true },
                        "task_count": { "type": "integer", "description": "Number of durable task records.", "minimum": 0, "example": 12, "readOnly": true },
                        "worker_count": { "type": "integer", "description": "Number of durable worker records.", "minimum": 0, "example": 2, "readOnly": true },
                        "session_count": { "type": "integer", "description": "Number of durable session records.", "minimum": 0, "example": 2, "readOnly": true },
                        "run_count": { "type": "integer", "description": "Number of durable run records.", "minimum": 0, "example": 8, "readOnly": true },
                        "next_event_seq": { "type": "integer", "description": "Next canonical event sequence number that will be written.", "minimum": 0, "example": 128, "readOnly": true }
                    }
                },
                "TaskCreateParams": {
                    "type": "object",
                    "description": "Request body for creating durable Nucleus work.",
                    "additionalProperties": false,
                    "required": ["title", "instructions"],
                    "properties": {
                        "title": { "type": "string", "description": "Short operator-facing summary of the work to perform.", "minLength": 1, "example": "Audit Nucleus tasks" },
                        "instructions": { "type": "string", "description": "Complete task instructions for the assigned worker.", "minLength": 1, "example": "Inspect queued and running tasks and report any items that are stuck." },
                        "source": { "type": "string", "description": "Source category for the task intake request. External GPT Actions should normally use websocket.", "enum": ["cli", "websocket", "cron", "hook", "system"], "default": "websocket", "example": "websocket" },
                        "profile": { "type": ["string", "null"], "description": "Preferred SI worker profile. If omitted or temporarily unavailable, Nucleus assigns the first available profile by priority: requested profile, ready workers, configured profiles, reusable sessions, then non-ready workers.", "pattern": "^[a-z][a-z0-9-]*$", "default": null, "example": "darmstada" },
                        "session_id": { "type": ["string", "null"], "description": "Optional existing SI session id to reuse for continuity.", "pattern": "^si-session-.+", "default": null, "example": "si-session-0123456789abcdef00000001" },
                        "max_retries": { "type": ["integer", "null"], "description": "Optional maximum retry attempts after failed execution.", "minimum": 0, "default": null, "example": 0 },
                        "timeout_seconds": { "type": ["integer", "null"], "description": "Optional execution timeout in seconds for this task.", "minimum": 0, "default": null, "example": 900 }
                    }
                },
                "TaskRecord": {
                    "type": "object",
                    "description": "Durable task projection stored by Nucleus.",
                    "additionalProperties": false,
                    "required": ["task_id", "source", "title", "instructions", "status", "created_at", "updated_at"],
                    "properties": {
                        "task_id": openapi_id_property("Canonical SI task id.", "si-task-", "si-task-0123456789abcdef00000001"),
                        "source": { "type": "string", "description": "Source category that created the task.", "enum": ["cli", "websocket", "cron", "hook", "system"], "example": "websocket", "readOnly": true },
                        "title": { "type": "string", "description": "Short operator-facing summary of the task.", "example": "Audit Nucleus tasks" },
                        "instructions": { "type": "string", "description": "Full worker instructions captured at intake.", "example": "Inspect queued and running tasks and report any items that are stuck." },
                        "status": { "type": "string", "description": "Current task lifecycle status.", "enum": ["queued", "running", "blocked", "done", "failed", "cancelled"], "example": "queued", "readOnly": true },
                        "profile": { "type": ["string", "null"], "description": "Preferred SI worker profile for this task.", "pattern": "^[a-z][a-z0-9-]*$", "example": "darmstada" },
                        "session_id": openapi_nullable_id_property("Session currently associated with this task, if any.", "si-session-", json!("si-session-0123456789abcdef00000001")),
                        "latest_run_id": openapi_nullable_id_property("Most recent run created for this task.", "si-run-", json!("si-run-0123456789abcdef00000001")),
                        "checkpoint_summary": { "type": ["string", "null"], "description": "Latest durable progress summary reported by the worker.", "example": "Checked all queued tasks." },
                        "checkpoint_at": { "type": ["string", "null"], "description": "Timestamp of the latest checkpoint.", "format": "date-time", "example": "2026-04-17T00:00:00Z" },
                        "checkpoint_seq": { "type": ["integer", "null"], "description": "Canonical event sequence for the latest checkpoint.", "minimum": 0, "example": 128 },
                        "parent_task_id": { "type": ["string", "null"], "description": "Parent task id when this task was produced by another task.", "pattern": "^si-task-.+", "example": "si-task-0123456789abcdef00000000" },
                        "producer_rule_name": { "type": ["string", "null"], "description": "Hook or producer rule that created this task.", "example": "nightly-audit" },
                        "producer_dedup_key": { "type": ["string", "null"], "description": "Producer deduplication key used to avoid duplicate task intake.", "example": "nightly-audit:2026-04-17" },
                        "blocked_reason": {
                            "type": ["string", "null"],
                            "description": "Reason the task is blocked when status is blocked.",
                            "enum": [null, "auth_required", "worker_unavailable", "profile_unavailable", "session_broken", "producer_error", "operator_hold", "fort_unavailable"],
                            "example": null
                        },
                        "created_at": { "type": "string", "description": "Task creation timestamp.", "format": "date-time", "example": "2026-04-17T00:00:00Z", "readOnly": true },
                        "updated_at": { "type": "string", "description": "Last task update timestamp.", "format": "date-time", "example": "2026-04-17T00:00:00Z", "readOnly": true },
                        "attempt_count": { "type": "integer", "description": "How many execution attempts Nucleus has already started for this task.", "minimum": 0, "example": 1, "readOnly": true },
                        "max_retries": { "type": ["integer", "null"], "description": "Maximum retry attempts configured for this task after a failed run.", "minimum": 0, "example": 0 },
                        "session_binding_locked": { "type": "boolean", "description": "True when the task was created against an explicit existing session and retries must preserve that session binding.", "example": false, "readOnly": true },
                        "timeout_seconds": { "type": ["integer", "null"], "description": "Execution timeout configured for this task.", "minimum": 0, "example": 900 }
                    }
                },
                "WorkerRecord": {
                    "type": "object",
                    "description": "Durable worker projection tracked by Nucleus.",
                    "additionalProperties": false,
                    "required": ["worker_id", "profile", "codex_home", "status"],
                    "properties": {
                        "worker_id": openapi_id_property("Canonical SI worker id.", "si-worker-", "si-worker-0123456789abcdef00000001"),
                        "profile": { "type": "string", "description": "SI profile this worker runs as.", "pattern": "^[a-z][a-z0-9-]*$", "example": "darmstada" },
                        "home_dir": { "type": ["string", "null"], "description": "Home directory used by the worker process.", "example": "/home/si" },
                        "codex_home": { "type": "string", "description": "Codex home directory used by this worker.", "example": "/home/si/.codex" },
                        "workdir": { "type": ["string", "null"], "description": "Default workspace directory for worker execution.", "example": "/home/si/Development/si" },
                        "extra_env": { "type": "object", "description": "Non-secret extra environment values configured for the worker.", "properties": {}, "additionalProperties": { "type": "string" }, "example": {} },
                        "status": { "type": "string", "description": "Worker lifecycle status.", "enum": ["starting", "ready", "degraded", "failed", "stopped"], "example": "ready", "readOnly": true },
                        "capability_version": { "type": ["string", "null"], "description": "Worker capability or package version when reported.", "example": env!("CARGO_PKG_VERSION") },
                        "last_heartbeat_at": { "type": ["string", "null"], "description": "Most recent heartbeat timestamp.", "format": "date-time", "example": "2026-04-17T00:00:00Z", "readOnly": true },
                        "effective_account_state": { "type": ["string", "null"], "description": "Observed account/authentication state for this worker.", "example": "authenticated", "readOnly": true }
                    }
                },
                "WorkerRuntimeView": {
                    "type": "object",
                    "description": "Observed runtime process attached to a worker.",
                    "additionalProperties": false,
                    "required": ["worker_id", "runtime_name", "pid", "started_at", "checked_at"],
                    "properties": {
                        "worker_id": openapi_id_property("Canonical SI worker id for the runtime process.", "si-worker-", "si-worker-0123456789abcdef00000001"),
                        "runtime_name": { "type": "string", "description": "Runtime adapter name.", "example": "codex", "readOnly": true },
                        "pid": { "type": "integer", "description": "Operating system process id.", "minimum": 0, "example": 12345, "readOnly": true },
                        "started_at": { "type": "string", "description": "Runtime process start timestamp.", "format": "date-time", "example": "2026-04-17T00:00:00Z", "readOnly": true },
                        "checked_at": { "type": "string", "description": "Timestamp when runtime process state was checked.", "format": "date-time", "example": "2026-04-17T00:00:10Z", "readOnly": true }
                    }
                },
                "WorkerInspectView": {
                    "type": "object",
                    "description": "Worker record plus optional live runtime observation.",
                    "additionalProperties": false,
                    "required": ["worker"],
                    "properties": {
                        "worker": { "description": "Durable worker projection.", "allOf": [schema_ref("WorkerRecord")] },
                        "runtime": {
                            "description": "Live runtime process details when the worker process can be observed.",
                            "anyOf": [schema_ref("WorkerRuntimeView"), { "type": "null" }]
                        }
                    }
                },
                "SessionRecord": {
                    "type": "object",
                    "description": "Durable worker session projection tracked by Nucleus.",
                    "additionalProperties": false,
                    "required": ["session_id", "worker_id", "lifecycle_state", "created_at", "updated_at"],
                    "properties": {
                        "session_id": openapi_id_property("Canonical SI session id.", "si-session-", "si-session-0123456789abcdef00000001"),
                        "worker_id": openapi_id_property("Worker currently associated with the session.", "si-worker-", "si-worker-0123456789abcdef00000001"),
                        "profile": { "type": ["string", "null"], "description": "SI profile associated with the session.", "pattern": "^[a-z][a-z0-9-]*$", "example": "darmstada" },
                        "app_server_thread_id": { "type": ["string", "null"], "description": "Underlying app server thread id for continuity.", "example": "thread_0123456789abcdef", "readOnly": true },
                        "workdir": { "type": ["string", "null"], "description": "Workspace directory associated with the session.", "example": "/home/si/Development/si" },
                        "lifecycle_state": { "type": "string", "description": "Current session lifecycle state.", "enum": ["opening", "ready", "busy", "broken", "closed"], "example": "ready", "readOnly": true },
                        "summary_state": { "type": ["string", "null"], "description": "Short durable summary of reusable session context.", "example": "idle" },
                        "created_at": { "type": "string", "description": "Session creation timestamp.", "format": "date-time", "example": "2026-04-17T00:00:00Z", "readOnly": true },
                        "updated_at": { "type": "string", "description": "Last session update timestamp.", "format": "date-time", "example": "2026-04-17T00:00:00Z", "readOnly": true }
                    }
                },
                "RunRecord": {
                    "type": "object",
                    "description": "Durable execution attempt for a task.",
                    "additionalProperties": false,
                    "required": ["run_id", "task_id", "session_id", "status"],
                    "properties": {
                        "run_id": openapi_id_property("Canonical SI run id.", "si-run-", "si-run-0123456789abcdef00000001"),
                        "task_id": openapi_id_property("Task executed by this run.", "si-task-", "si-task-0123456789abcdef00000001"),
                        "session_id": openapi_id_property("Session used by this run.", "si-session-", "si-session-0123456789abcdef00000001"),
                        "status": { "type": "string", "description": "Current run lifecycle status.", "enum": ["queued", "running", "blocked", "completed", "failed", "cancelled"], "example": "running", "readOnly": true },
                        "started_at": { "type": ["string", "null"], "description": "Timestamp when execution started.", "format": "date-time", "example": "2026-04-17T00:00:05Z", "readOnly": true },
                        "ended_at": { "type": ["string", "null"], "description": "Timestamp when execution reached a terminal state.", "format": "date-time", "example": null, "readOnly": true },
                        "parent_run_id": openapi_nullable_id_property("Parent run id for nested execution, if any.", "si-run-", json!(null)),
                        "app_server_turn_id": { "type": ["string", "null"], "description": "Underlying app server turn id associated with this run.", "example": null, "readOnly": true }
                    }
                },
                "TaskCancelResultView": {
                    "type": "object",
                    "description": "Result returned after requesting cancellation for a task.",
                    "additionalProperties": false,
                    "required": ["task", "cancellation_requested"],
                    "properties": {
                        "task": { "description": "Task projection after the cancellation request was handled.", "allOf": [schema_ref("TaskRecord")] },
                        "run": {
                            "description": "Run affected by the cancellation request, when the task had an active or latest run.",
                            "anyOf": [schema_ref("RunRecord"), { "type": "null" }]
                        },
                        "cancellation_requested": { "type": "boolean", "description": "True when Nucleus sent a live runtime interrupt for an active run; false when cancellation completed without a runtime interrupt or the task was already terminal.", "example": true, "readOnly": true }
                    }
                }
            }
        }
    })
}

pub fn public_openapi_document(public_url: &str) -> Value {
    openapi_document_with_server_url(public_url.trim())
}

fn openapi_document(_config: &NucleusConfig, headers: &HeaderMap) -> Value {
    openapi_document_with_server_url(&openapi_server_url(headers))
}

async fn rest_openapi_handler(
    State(service): State<Arc<NucleusService>>,
    headers: HeaderMap,
) -> impl IntoResponse {
    (StatusCode::OK, Json(openapi_document(&service.config, &headers))).into_response()
}

async fn rest_status_handler(
    State(service): State<Arc<NucleusService>>,
    headers: HeaderMap,
) -> Response {
    rest_gateway_response(
        service
            .dispatch_request_authorized(
                rest_request("nucleus.status", json!({})),
                extract_bearer_token(&headers).as_deref(),
            )
            .await,
        StatusCode::OK,
    )
}

async fn rest_create_task_handler(
    State(service): State<Arc<NucleusService>>,
    headers: HeaderMap,
    Json(request): Json<TaskCreateParams>,
) -> Response {
    rest_gateway_response(
        service
            .dispatch_request_authorized(
                rest_request(
                    "task.create",
                    serde_json::to_value(request).unwrap_or_else(|_| json!({})),
                ),
                extract_bearer_token(&headers).as_deref(),
            )
            .await,
        StatusCode::CREATED,
    )
}

async fn rest_list_tasks_handler(
    State(service): State<Arc<NucleusService>>,
    headers: HeaderMap,
) -> Response {
    rest_gateway_response(
        service
            .dispatch_request_authorized(
                rest_request("task.list", json!({})),
                extract_bearer_token(&headers).as_deref(),
            )
            .await,
        StatusCode::OK,
    )
}

async fn rest_inspect_task_handler(
    State(service): State<Arc<NucleusService>>,
    headers: HeaderMap,
    AxumPath(task_id): AxumPath<String>,
) -> Response {
    rest_gateway_response(
        service
            .dispatch_request_authorized(
                rest_request("task.inspect", json!({ "task_id": task_id })),
                extract_bearer_token(&headers).as_deref(),
            )
            .await,
        StatusCode::OK,
    )
}

async fn rest_cancel_task_handler(
    State(service): State<Arc<NucleusService>>,
    headers: HeaderMap,
    AxumPath(task_id): AxumPath<String>,
) -> Response {
    rest_gateway_response(
        service
            .dispatch_request_authorized(
                rest_request("task.cancel", json!({ "task_id": task_id })),
                extract_bearer_token(&headers).as_deref(),
            )
            .await,
        StatusCode::OK,
    )
}

async fn rest_ingest_event_handler(
    State(service): State<Arc<NucleusService>>,
    headers: HeaderMap,
    Json(request): Json<EventIngestParams>,
) -> Response {
    rest_gateway_response(
        service
            .dispatch_request_authorized(
                rest_request(
                    "events.ingest",
                    serde_json::to_value(request).unwrap_or_else(|_| json!({})),
                ),
                extract_bearer_token(&headers).as_deref(),
            )
            .await,
        StatusCode::CREATED,
    )
}

async fn rest_list_workers_handler(
    State(service): State<Arc<NucleusService>>,
    headers: HeaderMap,
) -> Response {
    rest_gateway_response(
        service
            .dispatch_request_authorized(
                rest_request("worker.list", json!({})),
                extract_bearer_token(&headers).as_deref(),
            )
            .await,
        StatusCode::OK,
    )
}

async fn rest_list_hook_rules_handler(
    State(service): State<Arc<NucleusService>>,
    headers: HeaderMap,
) -> Response {
    rest_gateway_response(
        service
            .dispatch_request_authorized(
                rest_request("producer.hook.list", json!({})),
                extract_bearer_token(&headers).as_deref(),
            )
            .await,
        StatusCode::OK,
    )
}

async fn rest_upsert_hook_rule_handler(
    State(service): State<Arc<NucleusService>>,
    headers: HeaderMap,
    Json(request): Json<HookRuleUpsertParams>,
) -> Response {
    rest_gateway_response(
        service
            .dispatch_request_authorized(
                rest_request(
                    "producer.hook.upsert",
                    serde_json::to_value(request).unwrap_or_else(|_| json!({})),
                ),
                extract_bearer_token(&headers).as_deref(),
            )
            .await,
        StatusCode::OK,
    )
}

async fn rest_inspect_hook_rule_handler(
    State(service): State<Arc<NucleusService>>,
    headers: HeaderMap,
    AxumPath(rule_name): AxumPath<String>,
) -> Response {
    rest_gateway_response(
        service
            .dispatch_request_authorized(
                rest_request("producer.hook.inspect", json!({ "rule_name": rule_name })),
                extract_bearer_token(&headers).as_deref(),
            )
            .await,
        StatusCode::OK,
    )
}

async fn rest_delete_hook_rule_handler(
    State(service): State<Arc<NucleusService>>,
    headers: HeaderMap,
    AxumPath(rule_name): AxumPath<String>,
) -> Response {
    rest_gateway_response(
        service
            .dispatch_request_authorized(
                rest_request("producer.hook.delete", json!({ "rule_name": rule_name })),
                extract_bearer_token(&headers).as_deref(),
            )
            .await,
        StatusCode::OK,
    )
}

async fn rest_inspect_worker_handler(
    State(service): State<Arc<NucleusService>>,
    headers: HeaderMap,
    AxumPath(worker_id): AxumPath<String>,
) -> Response {
    rest_gateway_response(
        service
            .dispatch_request_authorized(
                rest_request("worker.inspect", json!({ "worker_id": worker_id })),
                extract_bearer_token(&headers).as_deref(),
            )
            .await,
        StatusCode::OK,
    )
}

async fn rest_show_session_handler(
    State(service): State<Arc<NucleusService>>,
    headers: HeaderMap,
    AxumPath(session_id): AxumPath<String>,
) -> Response {
    rest_gateway_response(
        service
            .dispatch_request_authorized(
                rest_request("session.show", json!({ "session_id": session_id })),
                extract_bearer_token(&headers).as_deref(),
            )
            .await,
        StatusCode::OK,
    )
}

async fn rest_inspect_run_handler(
    State(service): State<Arc<NucleusService>>,
    headers: HeaderMap,
    AxumPath(run_id): AxumPath<String>,
) -> Response {
    rest_gateway_response(
        service
            .dispatch_request_authorized(
                rest_request("run.inspect", json!({ "run_id": run_id })),
                extract_bearer_token(&headers).as_deref(),
            )
            .await,
        StatusCode::OK,
    )
}

fn infer_error_code(error: &anyhow::Error) -> &'static str {
    let message = error.to_string();
    if message.contains("unauthorized") {
        "unauthorized"
    } else if message.contains("must match")
        || message.contains("parse ")
        || message.contains("cannot be empty")
    {
        "invalid_params"
    } else if message.contains("unavailable") {
        "unavailable"
    } else if message.contains("method not found") {
        "method_not_found"
    } else if message.contains("not found") {
        "not_found"
    } else {
        "internal_error"
    }
}

fn default_home_dir() -> PathBuf {
    env::var_os("HOME").map(PathBuf::from).unwrap_or_else(|| PathBuf::from("."))
}

fn default_codex_home_dir(profile: &str) -> PathBuf {
    default_home_dir().join(".si").join("codex").join("profiles").join(profile)
}

fn stable_workdir_from(current_dir: Option<PathBuf>, home_dir: PathBuf) -> PathBuf {
    if let Some(path) = current_dir.filter(|path| path.is_absolute() && path.is_dir()) {
        return path;
    }
    if home_dir.is_absolute() && home_dir.is_dir() {
        return home_dir;
    }
    PathBuf::from("/")
}

fn default_workdir() -> PathBuf {
    stable_workdir_from(env::current_dir().ok(), default_home_dir())
}

fn run_failure_requires_session_quarantine(message: &str) -> bool {
    let normalized = message.trim().to_ascii_lowercase();
    normalized.contains("turn timed out")
        || normalized.contains("turn became idle")
        || normalized.contains("turn exceeded max duration")
        || runtime_error_requires_worker_quarantine(&normalized)
}

fn runtime_error_requires_worker_quarantine(message: &str) -> bool {
    let normalized = message.trim().to_ascii_lowercase();
    normalized.contains("worker notification stream closed")
        || normalized.contains("worker response channel closed")
        || normalized.contains("worker request channel closed")
        || normalized.contains("worker request timed out")
        || normalized.contains("worker not running")
}

#[derive(Clone, Debug)]
struct CreateTaskInput {
    title: String,
    instructions: String,
    source: TaskSource,
    profile: Option<ProfileName>,
    session_id: Option<SessionId>,
    max_retries: Option<u32>,
    timeout_seconds: Option<u64>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
struct TaskRetryPlan {
    next_session_id: Option<SessionId>,
    attempt_count: u32,
    max_retries: u32,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
struct TaskCreateParams {
    title: String,
    instructions: String,
    #[serde(default = "default_request_task_source")]
    source: TaskSource,
    profile: Option<String>,
    session_id: Option<String>,
    max_retries: Option<u32>,
    timeout_seconds: Option<u64>,
}

fn default_request_task_source() -> TaskSource {
    TaskSource::Websocket
}

#[derive(Clone, Debug, Serialize, Deserialize)]
struct TaskCancelResultView {
    task: TaskRecord,
    #[serde(skip_serializing_if = "Option::is_none")]
    run: Option<RunRecord>,
    cancellation_requested: bool,
}

#[derive(Clone, Debug, Deserialize)]
struct TaskInspectParams {
    task_id: String,
}

#[derive(Clone, Debug, Deserialize)]
struct TaskCancelParams {
    task_id: String,
}

#[derive(Clone, Debug, Deserialize)]
struct TaskPruneParams {
    older_than_days: Option<u64>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
struct EventIngestParams {
    #[serde(rename = "type")]
    event_type: String,
    source: String,
    profile: Option<String>,
    payload: Value,
}

#[derive(Clone, Debug, Deserialize)]
struct WorkerProbeParams {
    worker_id: Option<String>,
    profile: String,
    home_dir: Option<PathBuf>,
    codex_home: Option<PathBuf>,
    workdir: Option<PathBuf>,
    env: Option<std::collections::BTreeMap<String, String>>,
}

impl WorkerProbeParams {
    fn profile_slug(&self) -> &str {
        self.profile.trim()
    }
}

#[derive(Clone, Debug, Deserialize)]
struct WorkerInspectParams {
    worker_id: String,
}

#[derive(Clone, Debug, Deserialize)]
struct WorkerRestartParams {
    worker_id: String,
}

#[derive(Clone, Debug, Deserialize)]
struct WorkerRepairAuthParams {
    worker_id: String,
}

#[derive(Clone, Debug, Deserialize)]
struct SessionCreateParams {
    profile: String,
    worker_id: Option<String>,
    thread_id: Option<String>,
    home_dir: Option<PathBuf>,
    codex_home: Option<PathBuf>,
    workdir: Option<PathBuf>,
    env: Option<std::collections::BTreeMap<String, String>>,
}

#[derive(Clone, Debug, Deserialize)]
struct SessionShowParams {
    session_id: String,
}

#[derive(Clone, Debug, Deserialize)]
struct RunSubmitTurnParams {
    session_id: String,
    task_id: String,
    prompt: String,
    timeout_seconds: Option<u64>,
}

#[derive(Clone, Debug, Deserialize)]
struct RunInspectParams {
    run_id: String,
}

#[derive(Clone, Debug, Deserialize)]
struct RunCancelParams {
    run_id: String,
}

#[derive(Clone, Debug, Deserialize)]
struct HookRuleInspectParams {
    rule_name: String,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
struct HookRuleUpsertParams {
    name: String,
    enabled: Option<bool>,
    match_event_type: String,
    instructions: String,
}

#[derive(Clone, Debug, Deserialize)]
struct HookRuleDeleteParams {
    rule_name: String,
}

#[derive(Clone, Debug, Serialize)]
struct HookRuleDeleteResultView {
    rule_name: String,
    deleted: bool,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct GatewayRequest {
    pub id: Value,
    pub method: String,
    #[serde(default = "default_gateway_params")]
    pub params: Value,
}

fn default_gateway_params() -> Value {
    Value::Object(Default::default())
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct GatewayErrorObject {
    pub code: String,
    pub message: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub details: Option<Value>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct GatewayResponse {
    pub id: Value,
    pub ok: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub result: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<GatewayErrorObject>,
}

impl GatewayResponse {
    fn ok(id: Value, result: Value) -> Self {
        Self { id, ok: true, result: Some(result), error: None }
    }

    fn err(id: Value, code: &str, message: String, details: Option<Value>) -> Self {
        Self {
            id,
            ok: false,
            result: None,
            error: Some(GatewayErrorObject { code: code.to_owned(), message, details }),
        }
    }
}

#[cfg(test)]
mod tests {
    use std::collections::{BTreeMap, HashMap, HashSet};
    use std::fs;
    use std::path::{Path, PathBuf};
    use std::sync::{Arc, Mutex};
    use std::thread;
    use std::time::Duration;

    use super::{
        BACKGROUND_WARNING_THROTTLE_WINDOW, CreateTaskInput, CronRuleRecord, CronScheduleKind,
        GPT_ACTIONS_OPENAPI_PUBLIC_URL, GatewayRequest, HookRuleRecord,
        MAX_WORKER_RESTART_ATTEMPTS, NucleusConfig, NucleusPaths, NucleusService, NucleusStore,
        append_jsonl_with_rotation, cron_due_key, current_persisted_version, hook_event_key,
        load_canonical_events, load_canonical_events_for_live_iteration, load_last_event_seq,
        public_openapi_document, run_failure_requires_session_quarantine,
        runtime_error_requires_worker_quarantine, stable_workdir_from, write_json_atomic,
    };
    use anyhow::{Result, anyhow};
    use axum::body::{Body, to_bytes};
    use axum::http::{Request, StatusCode};
    use chrono::{Duration as ChronoDuration, Utc};
    use serde_json::{Value, json};
    use si_nucleus_core::{
        BlockedReason, CanonicalEvent, CanonicalEventSource, CanonicalEventType, EventDataEnvelope,
        EventId, ProfileName, ProfileRecord, RunId, RunRecord, RunStatus, SessionId,
        SessionLifecycleState, SessionRecord, TaskId, TaskRecord, TaskSource, TaskStatus, WorkerId,
        WorkerStatus,
    };
    use si_nucleus_runtime::{
        CanonicalEventDraft, NucleusRuntime, RunTurnSpec, RuntimeCommand, RuntimeRunOutcome,
        RuntimeStatusSnapshot, SessionOpenResult, SessionOpenSpec, WorkerLaunchSpec,
        WorkerProbeResult, WorkerRuntimeView, WorkerStartResult,
    };
    use si_rs_fort::{PersistedSessionState, save_persisted_session_state};
    use tempfile::tempdir;
    use tower::util::ServiceExt;

    #[derive(Default)]
    struct FakeState {
        workers: HashMap<String, WorkerRuntimeView>,
        run_delay: Duration,
        fail_execute: bool,
        remaining_start_failures: usize,
        start_calls: usize,
        interrupted_turns: HashSet<String>,
        last_timeout_seconds: Option<u64>,
        run_failure_error: Option<String>,
        remaining_run_failures: usize,
    }

    #[derive(Clone, Default)]
    struct FakeRuntime {
        state: Arc<Mutex<FakeState>>,
    }

    impl FakeRuntime {
        fn with_run_delay(run_delay: Duration) -> Self {
            let runtime = Self::default();
            runtime.state.lock().expect("state").run_delay = run_delay;
            runtime
        }

        fn with_execute_failure() -> Self {
            let runtime = Self::default();
            runtime.state.lock().expect("state").fail_execute = true;
            runtime
        }

        fn with_run_failure(error: &str) -> Self {
            Self::with_run_failures(error, 1)
        }

        fn with_run_failures(error: &str, count: usize) -> Self {
            let runtime = Self::default();
            let mut state = runtime.state.lock().expect("state");
            state.run_failure_error = Some(error.to_owned());
            state.remaining_run_failures = count;
            drop(state);
            runtime
        }

        fn fail_next_starts(&self, count: usize) {
            self.state.lock().expect("state").remaining_start_failures = count;
        }

        fn start_call_count(&self) -> usize {
            self.state.lock().expect("state").start_calls
        }

        fn last_timeout_seconds(&self) -> Option<u64> {
            self.state.lock().expect("state").last_timeout_seconds
        }
    }

    impl NucleusRuntime for FakeRuntime {
        fn runtime_name(&self) -> &'static str {
            "fake-runtime"
        }

        fn build_worker_command(&self, spec: &WorkerLaunchSpec) -> RuntimeCommand {
            RuntimeCommand {
                program: "fake-runtime".to_owned(),
                args: vec![spec.profile.to_string()],
                current_dir: spec.workdir.clone(),
                env: Default::default(),
            }
        }

        fn probe_worker(&self, spec: &WorkerLaunchSpec) -> Result<WorkerProbeResult> {
            Ok(WorkerProbeResult {
                status: WorkerStatus::Ready,
                snapshot: RuntimeStatusSnapshot {
                    source: "fake-runtime".to_owned(),
                    model: Some("gpt-5.4".to_owned()),
                    reasoning_effort: Some("medium".to_owned()),
                    account_email: Some(format!("{}@example.com", spec.profile)),
                    account_plan: Some("pro".to_owned()),
                    five_hour_left_pct: Some(80.0),
                    five_hour_reset: Some("Apr 5, 2026 4:00 PM".to_owned()),
                    five_hour_remaining_minutes: Some(240),
                    weekly_left_pct: Some(90.0),
                    weekly_reset: Some("Apr 12, 2026 4:00 PM".to_owned()),
                    weekly_remaining_minutes: Some(9000),
                },
                checked_at: Utc::now(),
            })
        }

        fn start_worker(&self, spec: &WorkerLaunchSpec) -> Result<WorkerStartResult> {
            {
                let mut state = self.state.lock().expect("state");
                state.start_calls += 1;
                if state.remaining_start_failures > 0 {
                    state.remaining_start_failures -= 1;
                    anyhow::bail!("fake-runtime start_worker failed");
                }
            }
            let probe = self.probe_worker(spec)?;
            let runtime = WorkerRuntimeView {
                worker_id: spec.worker_id.clone(),
                runtime_name: "fake-runtime".to_owned(),
                pid: 4242,
                started_at: Utc::now(),
                checked_at: probe.checked_at,
            };
            let mut state = self.state.lock().expect("state");
            state.workers.insert(spec.worker_id.to_string(), runtime.clone());
            Ok(WorkerStartResult { runtime, probe })
        }

        fn stop_worker(&self, worker_id: &WorkerId) -> Result<()> {
            let mut state = self.state.lock().expect("state");
            state.workers.remove(worker_id.as_str());
            Ok(())
        }

        fn inspect_worker(&self, worker_id: &WorkerId) -> Result<Option<WorkerRuntimeView>> {
            let state = self.state.lock().expect("state");
            Ok(state.workers.get(worker_id.as_str()).cloned())
        }

        fn ensure_session(&self, spec: &SessionOpenSpec) -> Result<SessionOpenResult> {
            Ok(SessionOpenResult {
                thread_id: spec
                    .resume_thread_id
                    .clone()
                    .unwrap_or_else(|| format!("thread-{}", spec.session_id)),
                created: spec.resume_thread_id.is_none(),
                opened_at: Utc::now(),
            })
        }

        fn execute_turn(
            &self,
            spec: &RunTurnSpec,
            on_event: &mut dyn FnMut(CanonicalEventDraft) -> Result<()>,
        ) -> Result<RuntimeRunOutcome> {
            let (run_delay, fail_execute, run_failure_error) = {
                let mut state = self.state.lock().expect("state");
                state.last_timeout_seconds = spec.timeout_seconds;
                let run_failure_error = if state.remaining_run_failures > 0 {
                    state.remaining_run_failures -= 1;
                    state.run_failure_error.clone()
                } else {
                    None
                };
                (state.run_delay, state.fail_execute, run_failure_error)
            };
            if fail_execute {
                anyhow::bail!("fake-runtime execute_turn failed before run.started");
            }
            let turn_id = format!("turn-{}", spec.run_id);
            on_event(CanonicalEventDraft {
                event_type: CanonicalEventType::RunStarted,
                source: CanonicalEventSource::System,
                data: EventDataEnvelope {
                    task_id: spec.task_id.clone(),
                    worker_id: Some(spec.worker_id.clone()),
                    session_id: Some(spec.session_id.clone()),
                    run_id: Some(spec.run_id.clone()),
                    profile: Some(spec.profile.clone()),
                    payload: json!({
                        "thread_id": spec.thread_id,
                        "turn_id": turn_id,
                    }),
                },
            })?;
            if !run_delay.is_zero() {
                let start = std::time::Instant::now();
                while start.elapsed() < run_delay {
                    {
                        let mut state = self.state.lock().expect("state");
                        if state.interrupted_turns.remove(&turn_id) {
                            on_event(CanonicalEventDraft {
                                event_type: CanonicalEventType::RunCancelled,
                                source: CanonicalEventSource::System,
                                data: EventDataEnvelope {
                                    task_id: spec.task_id.clone(),
                                    worker_id: Some(spec.worker_id.clone()),
                                    session_id: Some(spec.session_id.clone()),
                                    run_id: Some(spec.run_id.clone()),
                                    profile: Some(spec.profile.clone()),
                                    payload: json!({
                                        "thread_id": spec.thread_id,
                                        "turn_id": turn_id,
                                        "error": "interrupted",
                                    }),
                                },
                            })?;
                            return Ok(RuntimeRunOutcome {
                                turn_id,
                                status: RunStatus::Cancelled,
                                completed_at: Utc::now(),
                                final_output: None,
                            });
                        }
                    }
                    thread::sleep(Duration::from_millis(20));
                }
            }
            if let Some(error) = run_failure_error {
                on_event(CanonicalEventDraft {
                    event_type: CanonicalEventType::RunFailed,
                    source: CanonicalEventSource::System,
                    data: EventDataEnvelope {
                        task_id: spec.task_id.clone(),
                        worker_id: Some(spec.worker_id.clone()),
                        session_id: Some(spec.session_id.clone()),
                        run_id: Some(spec.run_id.clone()),
                        profile: Some(spec.profile.clone()),
                        payload: json!({
                            "thread_id": spec.thread_id,
                            "turn_id": turn_id,
                            "error": error,
                        }),
                    },
                })?;
                return Ok(RuntimeRunOutcome {
                    turn_id,
                    status: RunStatus::Failed,
                    completed_at: Utc::now(),
                    final_output: None,
                });
            }
            on_event(CanonicalEventDraft {
                event_type: CanonicalEventType::RunOutputDelta,
                source: CanonicalEventSource::System,
                data: EventDataEnvelope {
                    task_id: spec.task_id.clone(),
                    worker_id: Some(spec.worker_id.clone()),
                    session_id: Some(spec.session_id.clone()),
                    run_id: Some(spec.run_id.clone()),
                    profile: Some(spec.profile.clone()),
                    payload: json!({
                        "thread_id": spec.thread_id,
                        "turn_id": turn_id,
                        "delta": "nucleus-smoke",
                    }),
                },
            })?;
            on_event(CanonicalEventDraft {
                event_type: CanonicalEventType::RunCompleted,
                source: CanonicalEventSource::System,
                data: EventDataEnvelope {
                    task_id: spec.task_id.clone(),
                    worker_id: Some(spec.worker_id.clone()),
                    session_id: Some(spec.session_id.clone()),
                    run_id: Some(spec.run_id.clone()),
                    profile: Some(spec.profile.clone()),
                    payload: json!({
                        "thread_id": spec.thread_id,
                        "turn_id": turn_id,
                        "final_output": "nucleus-smoke",
                    }),
                },
            })?;
            Ok(RuntimeRunOutcome {
                turn_id,
                status: RunStatus::Completed,
                completed_at: Utc::now(),
                final_output: Some("nucleus-smoke".to_owned()),
            })
        }

        fn interrupt_turn(
            &self,
            _worker_id: &WorkerId,
            _thread_id: &str,
            turn_id: &str,
        ) -> Result<()> {
            self.state.lock().expect("state").interrupted_turns.insert(turn_id.to_owned());
            Ok(())
        }

        fn probe_events(
            &self,
            spec: &WorkerLaunchSpec,
            probe: &WorkerProbeResult,
        ) -> Result<Vec<CanonicalEventDraft>> {
            Ok(vec![CanonicalEventDraft {
                event_type: CanonicalEventType::WorkerReady,
                source: CanonicalEventSource::System,
                data: EventDataEnvelope {
                    task_id: None,
                    worker_id: Some(spec.worker_id.clone()),
                    session_id: None,
                    run_id: None,
                    profile: Some(spec.profile.clone()),
                    payload: json!({
                        "source": probe.snapshot.source,
                        "model": probe.snapshot.model,
                    }),
                },
            }])
        }

        fn status_payload(&self, probe: &WorkerProbeResult) -> serde_json::Value {
            json!({
                "source": probe.snapshot.source,
                "model": probe.snapshot.model,
                "account_email": probe.snapshot.account_email,
            })
        }
    }

    fn wait_for_task_status(service: &NucleusService, task_id: &str, expected: TaskStatus) {
        let task_id = TaskId::new(task_id).expect("task id");
        for _ in 0..40 {
            let task =
                service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
            if task.status == expected {
                return;
            }
            thread::sleep(Duration::from_millis(25));
        }
        let task =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        panic!("expected task {} status {:?}, got {:?}", task_id, expected, task.status);
    }

    fn wait_for_task_state(
        service: &NucleusService,
        task_id: &str,
        predicate: impl Fn(&TaskRecord) -> bool,
    ) -> TaskRecord {
        let task_id = TaskId::new(task_id).expect("task id");
        for _ in 0..120 {
            let task =
                service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
            if predicate(&task) {
                return task;
            }
            thread::sleep(Duration::from_millis(25));
        }
        service.store.inspect_task(&task_id).expect("inspect task").expect("task exists")
    }

    fn wait_for_session_state(
        service: &NucleusService,
        session_id: &str,
        expected: SessionLifecycleState,
    ) -> SessionRecord {
        let session_id = SessionId::new(session_id).expect("session id");
        for _ in 0..120 {
            let session = service
                .store
                .inspect_session(&session_id)
                .expect("inspect session")
                .expect("session exists");
            if session.lifecycle_state == expected {
                return session;
            }
            thread::sleep(Duration::from_millis(25));
        }
        service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists")
    }

    fn wait_for_run_started(service: &NucleusService, run_id: &str) -> RunRecord {
        let run_id = RunId::new(run_id).expect("run id");
        for _ in 0..80 {
            let run = service.store.inspect_run(&run_id).expect("inspect run").expect("run exists");
            if run.status == RunStatus::Running && run.app_server_turn_id.is_some() {
                return run;
            }
            thread::sleep(Duration::from_millis(25));
        }
        service.store.inspect_run(&run_id).expect("inspect run").expect("run exists")
    }

    fn write_cron_rule(state_root: &Path, rule: &CronRuleRecord) {
        let path = state_root
            .join("state")
            .join("producers")
            .join("cron")
            .join(format!("{}.json", rule.name));
        write_json_atomic(&path, rule).expect("write cron rule");
    }

    fn write_hook_rule(state_root: &Path, rule: &HookRuleRecord) {
        let path = state_root
            .join("state")
            .join("producers")
            .join("hook")
            .join(format!("{}.json", rule.name));
        write_json_atomic(&path, rule).expect("write hook rule");
    }

    fn read_cron_rule(state_root: &Path, rule_name: &str) -> CronRuleRecord {
        let path = state_root
            .join("state")
            .join("producers")
            .join("cron")
            .join(format!("{rule_name}.json"));
        serde_json::from_slice(&fs::read(path).expect("read cron rule")).expect("parse cron rule")
    }

    fn read_hook_rule(state_root: &Path, rule_name: &str) -> HookRuleRecord {
        let path = state_root
            .join("state")
            .join("producers")
            .join("hook")
            .join(format!("{rule_name}.json"));
        serde_json::from_slice(&fs::read(path).expect("read hook rule")).expect("parse hook rule")
    }

    fn write_fort_session_state(
        codex_home: &Path,
        profile: &str,
        state: PersistedSessionState,
    ) -> PathBuf {
        let fort_dir = codex_home.join("fort");
        let session_path = fort_dir.join("session.json");
        save_persisted_session_state(
            &session_path,
            &PersistedSessionState { profile_id: profile.to_owned(), ..state },
        )
        .expect("write fort session state");
        fs::write(fort_dir.join("access.token"), "access-token\n")
            .expect("write fort access token");
        fs::write(fort_dir.join("refresh.token"), "refresh-token\n")
            .expect("write fort refresh token");
        session_path
    }

    fn fort_events_for_task(service: &NucleusService, task_id: &str) -> Vec<CanonicalEvent> {
        let task_id = TaskId::new(task_id).expect("task id");
        for _ in 0..10 {
            match load_canonical_events(&service.store.paths().events_path) {
                Ok(events) => {
                    return events
                        .into_iter()
                        .filter(|event| event.data.task_id.as_ref() == Some(&task_id))
                        .collect();
                }
                Err(_) => thread::sleep(Duration::from_millis(20)),
            }
        }
        load_canonical_events(&service.store.paths().events_path)
            .expect("load events")
            .into_iter()
            .filter(|event| event.data.task_id.as_ref() == Some(&task_id))
            .collect()
    }

    async fn response_json(response: axum::response::Response) -> serde_json::Value {
        let body = to_bytes(response.into_body(), usize::MAX).await.expect("read body");
        serde_json::from_slice(&body).expect("parse json body")
    }

    fn assert_object_schemas_have_properties(value: &Value, path: &str) {
        match value {
            Value::Object(map) => {
                let type_is_object = match map.get("type") {
                    Some(Value::String(value)) => value == "object",
                    Some(Value::Array(values)) => values.iter().any(|value| value == "object"),
                    _ => false,
                };
                let uses_schema_composition =
                    ["$ref", "allOf", "anyOf", "oneOf"].iter().any(|key| map.contains_key(*key));
                if type_is_object && !uses_schema_composition {
                    assert!(
                        map.contains_key("properties"),
                        "object schema missing properties at {path}"
                    );
                }
                for (key, child) in map {
                    let child_path =
                        if path.is_empty() { key.to_owned() } else { format!("{path}.{key}") };
                    assert_object_schemas_have_properties(child, &child_path);
                }
            }
            Value::Array(values) => {
                for (index, child) in values.iter().enumerate() {
                    assert_object_schemas_have_properties(child, &format!("{path}[{index}]"));
                }
            }
            _ => {}
        }
    }

    #[test]
    fn store_reloads_event_sequence() {
        let temp = tempdir().expect("tempdir");
        let config = NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: None,
        };
        let service = NucleusService::open(config.clone()).expect("open service");
        let created = tokio::runtime::Runtime::new().expect("runtime").block_on(
            service.dispatch_request(GatewayRequest {
                id: json!(1),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Build gateway",
                    "instructions": "Create the first websocket request path",
                }),
            }),
        );
        assert!(created.ok);

        drop(service);

        let reopened = NucleusService::open(config).expect("reopen");
        let status = reopened.status().expect("status");
        assert_eq!(status.task_count, 1);
        assert_eq!(status.next_event_seq, 2);
    }

    #[test]
    fn store_rejects_second_open_for_same_state_directory() {
        let temp = tempdir().expect("tempdir");
        let state_dir = temp.path().join("nucleus");
        let _store = NucleusStore::open(state_dir.clone()).expect("open store");

        let error = match NucleusStore::open(state_dir) {
            Ok(_) => panic!("second open should fail"),
            Err(error) => error,
        };
        let message = format!("{error:#}");
        assert!(message.contains("already locked"));
        assert!(message.contains("nucleus.lock"));
    }

    #[test]
    fn rotated_event_logs_preserve_replay_and_last_sequence() {
        let temp = tempdir().expect("tempdir");
        let events_path = temp.path().join("events.jsonl");
        let event_one = CanonicalEvent {
            event_id: EventId::generate(),
            seq: 1,
            ts: Utc::now(),
            event_type: CanonicalEventType::TaskCreated,
            source: CanonicalEventSource::System,
            data: EventDataEnvelope {
                task_id: None,
                worker_id: None,
                session_id: None,
                run_id: None,
                profile: None,
                payload: json!({ "title": "first" }),
            },
        };
        let event_two = CanonicalEvent {
            event_id: EventId::generate(),
            seq: 2,
            ts: Utc::now(),
            event_type: CanonicalEventType::TaskUpdated,
            source: CanonicalEventSource::System,
            data: EventDataEnvelope {
                task_id: None,
                worker_id: None,
                session_id: None,
                run_id: None,
                profile: None,
                payload: json!({ "title": "second" }),
            },
        };

        append_jsonl_with_rotation(&events_path, &event_one, 1).expect("append first event");
        append_jsonl_with_rotation(&events_path, &event_two, 1).expect("append rotated event");

        let rotated = fs::read_dir(temp.path())
            .expect("read temp dir")
            .filter_map(|entry| entry.ok())
            .map(|entry| entry.path())
            .filter(|path| {
                path.file_name()
                    .and_then(|value| value.to_str())
                    .map(|name| name.starts_with("events-") && name.ends_with(".jsonl"))
                    .unwrap_or(false)
            })
            .collect::<Vec<_>>();
        assert_eq!(rotated.len(), 1);

        let events = load_canonical_events(&events_path).expect("load canonical events");
        assert_eq!(events.iter().map(|event| event.seq).collect::<Vec<_>>(), vec![1, 2]);
        assert_eq!(load_last_event_seq(&events_path).expect("load last seq"), 2);
    }

    #[test]
    fn startup_isolates_malformed_task_state_and_emits_system_warning() {
        let temp = tempdir().expect("tempdir");
        let state_dir = temp.path().join("nucleus");
        let store = NucleusStore::open(state_dir.clone()).expect("open store");
        let broken_task_dir = store.paths().tasks_state_dir.join("broken-task");
        fs::create_dir_all(&broken_task_dir).expect("create broken task dir");
        fs::write(broken_task_dir.join("task.json"), b"{\"task_id\":")
            .expect("write broken task file");
        drop(store);

        let reopened = NucleusStore::open(state_dir).expect("reopen store");
        let warning = load_canonical_events(&reopened.paths().events_path)
            .expect("load events")
            .into_iter()
            .find(|event| event.event_type == CanonicalEventType::SystemWarning)
            .expect("system warning event");
        assert_eq!(warning.source, CanonicalEventSource::System);
        assert_eq!(
            warning.data.payload["message"],
            json!("isolated malformed persisted object during startup recovery"),
        );
        assert_eq!(warning.data.payload["details"]["kind"], json!("task"));
        assert!(
            warning.data.payload["details"]["path"]
                .as_str()
                .expect("warning path")
                .ends_with("task.json")
        );
    }

    #[test]
    fn startup_isolates_malformed_session_state_and_emits_system_warning() {
        let temp = tempdir().expect("tempdir");
        let state_dir = temp.path().join("nucleus");
        let store = NucleusStore::open(state_dir.clone()).expect("open store");
        let broken_session_dir = store.paths().sessions_state_dir.join("broken-session");
        fs::create_dir_all(&broken_session_dir).expect("create broken session dir");
        fs::write(broken_session_dir.join("session.json"), b"{\"session_id\":")
            .expect("write broken session file");
        drop(store);

        let reopened = NucleusStore::open(state_dir).expect("reopen store");
        let warning = load_canonical_events(&reopened.paths().events_path)
            .expect("load events")
            .into_iter()
            .find(|event| {
                event.event_type == CanonicalEventType::SystemWarning
                    && event.data.payload["details"]["kind"] == json!("session")
            })
            .expect("session warning event");
        assert_eq!(warning.source, CanonicalEventSource::System);
        assert_eq!(
            warning.data.payload["message"],
            json!("isolated malformed persisted object during startup recovery"),
        );
        assert!(
            warning.data.payload["details"]["path"]
                .as_str()
                .expect("warning path")
                .ends_with("session.json")
        );
    }

    #[test]
    fn startup_quarantines_malformed_canonical_event_ledger_and_emits_system_warning() {
        let temp = tempdir().expect("tempdir");
        let state_dir = temp.path().join("nucleus");
        let paths = NucleusPaths::new(state_dir.clone());
        paths.ensure_layout().expect("ensure layout");
        fs::write(&paths.events_path, b"{\"seq\":").expect("write malformed events ledger");

        let store = NucleusStore::open(state_dir).expect("open store");
        let warning = load_canonical_events(&store.paths().events_path)
            .expect("load events")
            .into_iter()
            .find(|event| {
                event.event_type == CanonicalEventType::SystemWarning
                    && event.data.payload["details"]["kind"] == json!("canonical_event_log")
            })
            .expect("canonical event log warning");
        assert_eq!(warning.source, CanonicalEventSource::System);
        assert_eq!(
            warning.data.payload["message"],
            json!("isolated malformed canonical event log during startup recovery"),
        );
        assert_eq!(
            warning.data.payload["details"]["path"],
            json!(store.paths().events_path.display().to_string()),
        );
        let quarantine_path =
            warning.data.payload["details"]["quarantine_path"].as_str().expect("quarantine path");
        assert!(Path::new(quarantine_path).exists());
    }

    #[test]
    fn background_loop_warning_suppresses_repeated_errors_within_window() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_without_runtime(NucleusConfig {
            bind_addr: "127.0.0.1:9898".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: None,
        })
        .expect("service");
        let now = Utc::now();

        let first = service
            .record_background_loop_warning(now, &anyhow!("failed to load configuration"))
            .expect("first warning");
        assert_eq!(first["suppressed_since_last_emit"], json!(0));

        assert!(
            service
                .record_background_loop_warning(
                    now + ChronoDuration::seconds(10),
                    &anyhow!("failed to load configuration"),
                )
                .is_none()
        );

        let third = service
            .record_background_loop_warning(
                now + BACKGROUND_WARNING_THROTTLE_WINDOW + ChronoDuration::seconds(1),
                &anyhow!("failed to load configuration"),
            )
            .expect("throttled warning re-emits");
        assert_eq!(third["suppressed_since_last_emit"], json!(1));
    }

    #[test]
    fn background_loop_warning_emits_new_errors_immediately() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_without_runtime(NucleusConfig {
            bind_addr: "127.0.0.1:9898".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: None,
        })
        .expect("service");
        let now = Utc::now();

        assert!(
            service
                .record_background_loop_warning(now, &anyhow!("failed to load configuration"))
                .is_some()
        );
        let second = service
            .record_background_loop_warning(
                now + ChronoDuration::seconds(1),
                &anyhow!("worker not found"),
            )
            .expect("different error emits immediately");
        assert_eq!(second["error"], json!("worker not found"));
        assert_eq!(second["suppressed_since_last_emit"], json!(0));
    }

    #[tokio::test]
    async fn gateway_dispatch_creates_and_lists_tasks() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: None,
        })
        .expect("service");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("create-1"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Persist task",
                    "instructions": "Write the task to disk and emit an event",
                    "profile": "america"
                }),
            })
            .await;
        assert!(created.ok);

        let listed = service
            .dispatch_request(GatewayRequest {
                id: json!("list-1"),
                method: "task.list".to_owned(),
                params: json!({}),
            })
            .await;
        assert!(listed.ok);
        let tasks = listed.result.expect("task list");
        assert_eq!(tasks.as_array().map(|items| items.len()), Some(1));
        assert_eq!(tasks[0]["profile"], json!("america"));
    }

    #[tokio::test]
    async fn gateway_rejects_raw_app_server_methods_from_public_surface() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: None,
        })
        .expect("service");

        for method in ["thread.start", "thread.resume", "turn.start", "turn.interrupt"] {
            let response = service
                .dispatch_request(GatewayRequest {
                    id: json!(method),
                    method: method.to_owned(),
                    params: json!({}),
                })
                .await;
            assert!(!response.ok, "{method} should not be exposed publicly");
            let error = response.error.expect("gateway error");
            assert_eq!(error.code, "method_not_found");
            assert!(error.message.contains("method not found"));
        }
    }

    #[tokio::test]
    async fn task_create_rejects_non_slug_profile_names() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: None,
        })
        .expect("service");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("create-invalid-profile"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Persist task",
                    "instructions": "Write the task to disk and emit an event",
                    "profile": "America"
                }),
            })
            .await;
        assert!(!created.ok);
        assert!(
            created
                .error
                .as_ref()
                .map(|error| error.message.contains("profile name must match"))
                .unwrap_or(false)
        );
        assert_eq!(service.store.list_tasks().expect("list tasks").len(), 0);
    }

    #[tokio::test]
    async fn gateway_ingests_github_notification_and_emits_hook_task() {
        let temp = tempdir().expect("tempdir");
        let state_root = temp.path().join("nucleus");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: state_root.clone(),
            auth_token: None,
        })
        .expect("service");

        let upsert = service
            .dispatch_request(GatewayRequest {
                id: json!("hook-upsert"),
                method: "producer.hook.upsert".to_owned(),
                params: json!({
                    "name": "github-notify",
                    "match_event_type": "github.notification",
                    "instructions": "Triage the GitHub notification",
                }),
            })
            .await;
        assert!(upsert.ok);
        assert_eq!(
            upsert.result.expect("hook rule")["match_event_type"],
            json!("github.notification")
        );

        let ingested = service
            .dispatch_request(GatewayRequest {
                id: json!("event-ingest"),
                method: "events.ingest".to_owned(),
                params: json!({
                    "type": "github.notification",
                    "source": "github",
                    "payload": {
                        "repository": "Aureuma/si",
                        "reason": "mention",
                        "subject": {
                            "type": "PullRequest",
                            "title": "Stabilize nucleus ingress"
                        }
                    }
                }),
            })
            .await;
        assert!(ingested.ok);
        let ingested_event = ingested.result.expect("ingested event");
        assert_eq!(ingested_event["type"], json!("github.notification"));
        assert_eq!(ingested_event["source"], json!("github"));

        let events =
            load_canonical_events(&service.store.paths().events_path).expect("load events");
        assert!(events.iter().any(|event| {
            event.event_type == CanonicalEventType::GithubNotification
                && event.source == CanonicalEventSource::Github
                && event.data.payload["repository"] == json!("Aureuma/si")
        }));

        let tasks = service.store.list_tasks().expect("list tasks");
        let hook_task =
            tasks.iter().find(|task| task.source == TaskSource::Hook).expect("hook task");
        assert_eq!(hook_task.producer_rule_name.as_deref(), Some("github-notify"));
        assert!(hook_task.instructions.contains("Canonical event type: github.notification"));

        let stored_rule = read_hook_rule(&state_root, "github-notify");
        assert_eq!(stored_rule.last_processed_event_seq, 1);
    }

    #[tokio::test]
    async fn gateway_rejects_non_github_external_event_ingest() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: None,
        })
        .expect("service");

        let ingested = service
            .dispatch_request(GatewayRequest {
                id: json!("event-ingest-invalid"),
                method: "events.ingest".to_owned(),
                params: json!({
                    "type": "task.created",
                    "source": "websocket",
                    "payload": {}
                }),
            })
            .await;
        assert!(!ingested.ok);
        assert!(
            ingested
                .error
                .as_ref()
                .map(|error| error.message.contains("github.notification"))
                .unwrap_or(false)
        );
    }

    #[tokio::test]
    async fn new_hook_rule_upsert_starts_after_existing_event_history() {
        let temp = tempdir().expect("tempdir");
        let state_root = temp.path().join("nucleus");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: state_root.clone(),
            auth_token: None,
        })
        .expect("service");

        let ingested = service
            .dispatch_request(GatewayRequest {
                id: json!("event-ingest-before-upsert"),
                method: "events.ingest".to_owned(),
                params: json!({
                    "type": "github.notification",
                    "source": "github",
                    "payload": { "repository": "Aureuma/si", "reason": "mention" }
                }),
            })
            .await;
        assert!(ingested.ok);
        let first_seq = ingested.result.expect("first event")["seq"].as_u64().expect("first seq");

        let upsert = service
            .dispatch_request(GatewayRequest {
                id: json!("hook-upsert-after-history"),
                method: "producer.hook.upsert".to_owned(),
                params: json!({
                    "name": "github-notify",
                    "match_event_type": "github.notification",
                    "instructions": "Triage the GitHub notification",
                }),
            })
            .await;
        assert!(upsert.ok);
        assert_eq!(upsert.result.expect("hook rule")["last_processed_event_seq"], json!(first_seq));
        assert!(service.store.list_tasks().expect("list tasks").is_empty());
        assert_eq!(
            read_hook_rule(&state_root, "github-notify").last_processed_event_seq,
            first_seq
        );

        let ingested = service
            .dispatch_request(GatewayRequest {
                id: json!("event-ingest-after-upsert"),
                method: "events.ingest".to_owned(),
                params: json!({
                    "type": "github.notification",
                    "source": "github",
                    "payload": { "repository": "Aureuma/si", "reason": "assign" }
                }),
            })
            .await;
        assert!(ingested.ok);
        let second_seq =
            ingested.result.expect("second event")["seq"].as_u64().expect("second seq");
        let tasks = service.store.list_tasks().expect("list tasks");
        let hook_task =
            tasks.iter().find(|task| task.source == TaskSource::Hook).expect("hook task");
        let expected_dedup = format!("github-notify:{second_seq}");
        assert_eq!(hook_task.producer_dedup_key.as_deref(), Some(expected_dedup.as_str()));
    }

    #[test]
    fn persist_hook_rule_progress_preserves_concurrent_operator_updates() {
        let temp = tempdir().expect("tempdir");
        let state_root = temp.path().join("nucleus");
        let store = NucleusStore::open(state_root.clone()).expect("open store");

        write_hook_rule(
            &state_root,
            &HookRuleRecord {
                name: "github-notify".to_owned(),
                enabled: true,
                match_event_type: "github.notification".to_owned(),
                instructions: "old instructions".to_owned(),
                last_processed_event_seq: 0,
                version: current_persisted_version().to_owned(),
            },
        );
        let mut stale = read_hook_rule(&state_root, "github-notify");
        stale.last_processed_event_seq = 9;
        stale.instructions = "stale instructions".to_owned();

        store
            .upsert_hook_rule(&HookRuleRecord {
                name: "github-notify".to_owned(),
                enabled: false,
                match_event_type: "github.notification".to_owned(),
                instructions: "new instructions".to_owned(),
                last_processed_event_seq: 3,
                version: current_persisted_version().to_owned(),
            })
            .expect("upsert hook rule");

        assert!(store.persist_hook_rule_progress(&stale).expect("persist hook progress"));

        let stored = read_hook_rule(&state_root, "github-notify");
        assert!(!stored.enabled);
        assert_eq!(stored.instructions, "new instructions");
        assert_eq!(stored.last_processed_event_seq, 9);
    }

    #[test]
    fn persist_hook_rule_progress_does_not_recreate_deleted_rule() {
        let temp = tempdir().expect("tempdir");
        let state_root = temp.path().join("nucleus");
        let store = NucleusStore::open(state_root.clone()).expect("open store");

        write_hook_rule(
            &state_root,
            &HookRuleRecord {
                name: "github-notify".to_owned(),
                enabled: true,
                match_event_type: "github.notification".to_owned(),
                instructions: "old instructions".to_owned(),
                last_processed_event_seq: 0,
                version: current_persisted_version().to_owned(),
            },
        );
        let mut stale = read_hook_rule(&state_root, "github-notify");
        stale.last_processed_event_seq = 9;
        assert!(store.delete_hook_rule("github-notify").expect("delete hook rule"));

        assert!(!store.persist_hook_rule_progress(&stale).expect("skip deleted rule progress"));
        assert!(store.inspect_hook_rule("github-notify").expect("inspect hook rule").is_none());
    }

    #[tokio::test]
    async fn gateway_status_reports_ws_endpoint() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:9898".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: None,
        })
        .expect("service");

        let response = service
            .dispatch_request(GatewayRequest {
                id: json!(1),
                method: "nucleus.status".to_owned(),
                params: json!({}),
            })
            .await;
        assert!(response.ok);
        assert_eq!(response.result.expect("status")["ws_url"], json!("ws://127.0.0.1:9898/ws"));
    }

    #[tokio::test]
    async fn session_create_rejects_non_slug_profile_names() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-invalid-profile"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "America",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/America"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(!session.ok);
        assert!(
            session
                .error
                .as_ref()
                .map(|error| error.message.contains("profile name must match"))
                .unwrap_or(false)
        );
        assert_eq!(service.store.list_sessions().expect("list sessions").len(), 0);
        assert_eq!(service.store.list_workers().expect("list workers").len(), 0);
    }

    #[tokio::test]
    async fn service_open_waits_for_successful_bind_before_writing_gateway_metadata() {
        let temp = tempdir().expect("tempdir");
        let state_dir = temp.path().join("nucleus");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:0".parse().expect("addr"),
            state_dir: state_dir.clone(),
            auth_token: None,
        })
        .expect("service");
        let metadata_path = state_dir.join("gateway").join("metadata.json");
        assert!(!metadata_path.exists());

        let handle = tokio::spawn(service.serve());
        let metadata = loop {
            if metadata_path.exists() {
                let raw = fs::read(&metadata_path).expect("read metadata");
                break serde_json::from_slice::<serde_json::Value>(&raw).expect("parse metadata");
            }
            tokio::time::sleep(Duration::from_millis(10)).await;
        };
        handle.abort();

        assert_eq!(metadata["version"], json!(env!("CARGO_PKG_VERSION")));
        let bind_addr = metadata["bind_addr"].as_str().expect("bind addr");
        assert!(bind_addr.starts_with("127.0.0.1:"));
        assert_ne!(bind_addr, "127.0.0.1:0");
        assert_eq!(metadata["ws_url"], json!(format!("ws://{bind_addr}/ws")));
    }

    #[tokio::test]
    async fn serve_does_not_start_runtime_loops_when_bind_fails() {
        let temp = tempdir().expect("tempdir");
        let occupied = std::net::TcpListener::bind("127.0.0.1:0").expect("bind occupied addr");
        let bind_addr = occupied.local_addr().expect("occupied addr");
        let runtime = FakeRuntime::default();
        let service = NucleusService::open_with_runtime(
            NucleusConfig { bind_addr, state_dir: temp.path().join("nucleus"), auth_token: None },
            Arc::new(runtime.clone()),
        )
        .expect("service");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-before-bind-failure"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Do not dispatch without listener",
                    "instructions": "This task must not start when the gateway cannot bind",
                    "profile": "america",
                }),
            })
            .await;
        assert!(created.ok);
        let task_id = TaskId::new(
            created
                .result
                .as_ref()
                .and_then(|task| task["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        let error = service.clone().serve().await.expect_err("serve should fail to bind");
        assert!(error.to_string().contains("bind"));
        assert_eq!(runtime.start_call_count(), 0);
        let task = service.store.inspect_task(&task_id).expect("inspect task").expect("task");
        assert_eq!(task.status, TaskStatus::Queued);
        assert!(task.latest_run_id.is_none());
    }

    #[tokio::test]
    async fn rest_openapi_document_describes_bounded_external_endpoints() {
        let temp = tempdir().expect("tempdir");
        let app = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:9898".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: None,
        })
        .expect("service")
        .router();

        let response = app
            .clone()
            .oneshot(Request::builder().uri("/openapi.json").body(Body::empty()).expect("request"))
            .await
            .expect("openapi response");
        assert_eq!(response.status(), StatusCode::OK);
        let body = response_json(response).await;
        assert_eq!(body["openapi"], json!("3.1.0"));
        assert_eq!(body["info"]["title"], json!("SI Nucleus REST API"));
        assert_eq!(body["info"]["version"], json!(env!("CARGO_PKG_VERSION")));
        assert_eq!(
            body["info"]["description"],
            json!(
                "Bounded external integration API over the canonical SI Nucleus task, worker, session, and run model."
            )
        );
        assert_eq!(body["servers"][0]["url"], json!("/"));

        let forwarded_response = app
            .clone()
            .oneshot(
                Request::builder()
                    .uri("/openapi.json")
                    .header("host", "nucleus.aureuma.ai")
                    .header("x-forwarded-proto", "https")
                    .body(Body::empty())
                    .expect("request"),
            )
            .await
            .expect("forwarded openapi response");
        assert_eq!(forwarded_response.status(), StatusCode::OK);
        let forwarded_body = response_json(forwarded_response).await;
        assert_eq!(forwarded_body["servers"][0]["url"], json!("https://nucleus.aureuma.ai"));

        assert_eq!(body["components"]["securitySchemes"]["bearerAuth"]["scheme"], json!("bearer"));
        assert_eq!(
            body["components"]["securitySchemes"]["bearerAuth"]["bearerFormat"],
            json!("opaque token")
        );
        for (path, method, expected_response) in [
            ("/status", "get", "200"),
            ("/tasks", "get", "200"),
            ("/tasks", "post", "201"),
            ("/tasks/{task_id}", "get", "200"),
            ("/tasks/{task_id}/cancel", "post", "200"),
            ("/workers", "get", "200"),
            ("/workers/{worker_id}", "get", "200"),
            ("/sessions/{session_id}", "get", "200"),
            ("/runs/{run_id}", "get", "200"),
            ("/openapi.json", "get", "200"),
        ] {
            let operation = &body["paths"][path][method];
            assert!(
                operation["summary"].as_str().map(|value| !value.is_empty()).unwrap_or(false),
                "missing summary for {method} {path}"
            );
            assert!(
                operation["description"].as_str().map(|value| !value.is_empty()).unwrap_or(false),
                "missing description for {method} {path}"
            );
            assert!(
                operation["x-si-purpose"].as_str().map(|value| !value.is_empty()).unwrap_or(false),
                "missing x-si-purpose for {method} {path}"
            );
            assert!(
                operation["responses"][expected_response]["content"]["application/json"]["schema"]
                    .is_object(),
                "missing success schema for {method} {path}"
            );
            assert!(
                operation.get("requestBody").map(|value| value.is_object()).unwrap_or(false)
                    || operation.get("parameters").map(|value| value.is_array()).unwrap_or(false)
                    || matches!(
                        (path, method),
                        ("/status", "get")
                            | ("/tasks", "get")
                            | ("/workers", "get")
                            | ("/openapi.json", "get")
                    ),
                "missing request surface for {method} {path}"
            );
        }
        for (path, method, operation_id) in [
            ("/status", "get", "getNucleusStatus"),
            ("/tasks", "get", "listTasks"),
            ("/tasks", "post", "createTask"),
            ("/tasks/{task_id}", "get", "inspectTask"),
            ("/tasks/{task_id}/cancel", "post", "cancelTask"),
            ("/workers", "get", "listWorkers"),
            ("/workers/{worker_id}", "get", "inspectWorker"),
            ("/sessions/{session_id}", "get", "inspectSession"),
            ("/runs/{run_id}", "get", "inspectRun"),
            ("/openapi.json", "get", "getOpenApiDocument"),
        ] {
            assert_eq!(
                body["paths"][path][method]["operationId"],
                json!(operation_id),
                "missing stable operationId for {method} {path}"
            );
        }
        for (path, method, expected_pattern, expected_example) in [
            ("/tasks/{task_id}", "get", "^si-task-.+", "si-task-0123456789abcdef00000001"),
            ("/tasks/{task_id}/cancel", "post", "^si-task-.+", "si-task-0123456789abcdef00000001"),
            ("/workers/{worker_id}", "get", "^si-worker-.+", "si-worker-0123456789abcdef00000001"),
            (
                "/sessions/{session_id}",
                "get",
                "^si-session-.+",
                "si-session-0123456789abcdef00000001",
            ),
            ("/runs/{run_id}", "get", "^si-run-.+", "si-run-0123456789abcdef00000001"),
        ] {
            let parameter = &body["paths"][path][method]["parameters"][0];
            assert_eq!(parameter["schema"]["pattern"], json!(expected_pattern));
            assert_eq!(parameter["schema"]["example"], json!(expected_example));
            assert!(
                parameter["description"].as_str().map(|value| !value.is_empty()).unwrap_or(false),
                "missing path parameter description for {method} {path}"
            );
        }
        let create_params = &body["components"]["schemas"]["TaskCreateParams"];
        assert_eq!(create_params["additionalProperties"], json!(false));
        assert_eq!(create_params["properties"]["source"]["default"], json!("websocket"));
        assert_eq!(create_params["properties"]["session_id"]["pattern"], json!("^si-session-.+"));
        assert!(create_params["properties"]["instructions"]["description"].is_string());
        assert_eq!(
            body["paths"]["/tasks"]["post"]["requestBody"]["content"]["application/json"]["example"]
                ["title"],
            json!("Audit Nucleus tasks")
        );
        assert_eq!(
            body["paths"]["/tasks"]["post"]["responses"]["201"]["content"]["application/json"]["example"]
                ["task_id"],
            json!("si-task-0123456789abcdef00000001")
        );
        assert!(
            body["components"]["schemas"]["OpenApiDocumentView"]["description"]
                .as_str()
                .map(|value| !value.is_empty())
                .unwrap_or(false),
            "missing component schema description for OpenApiDocumentView"
        );
        assert_eq!(
            body["components"]["schemas"]["OpenApiDocumentView"]["additionalProperties"],
            json!(true)
        );
        for schema_name in [
            "RestErrorEnvelope",
            "NucleusStatusView",
            "TaskCreateParams",
            "TaskRecord",
            "WorkerRecord",
            "WorkerRuntimeView",
            "WorkerInspectView",
            "SessionRecord",
            "RunRecord",
            "TaskCancelResultView",
        ] {
            let schema = &body["components"]["schemas"][schema_name];
            assert!(
                schema["description"].as_str().map(|value| !value.is_empty()).unwrap_or(false),
                "missing component schema description for {schema_name}"
            );
            assert_eq!(
                schema["additionalProperties"],
                json!(false),
                "component schema should be closed for {schema_name}"
            );
        }
        assert_eq!(
            body["components"]["schemas"]["TaskRecord"]["properties"]["task_id"]["pattern"],
            json!("^si-task-.+")
        );
        assert_eq!(
            body["components"]["schemas"]["RunRecord"]["properties"]["run_id"]["example"],
            json!("si-run-0123456789abcdef00000001")
        );
        let gpt_actions_openapi = include_str!("../../../../docs/gpt-actions-openapi.yaml");
        let rendered_openapi: Value = serde_yaml::from_str(gpt_actions_openapi)
            .expect("parse checked-in GPT Actions OpenAPI");
        assert_eq!(
            rendered_openapi,
            public_openapi_document(GPT_ACTIONS_OPENAPI_PUBLIC_URL),
            "docs/gpt-actions-openapi.yaml must be generated from the canonical Nucleus OpenAPI document"
        );
        for required_text in [
            "operationId: listWorkers",
            "operationId: inspectWorker",
            "operationId: inspectSession",
            "TaskCancelResultView:",
            "pattern: ^si-task-.+",
            "additionalProperties: false",
        ] {
            assert!(
                gpt_actions_openapi.contains(required_text),
                "GPT Actions profile missing {required_text}"
            );
        }
        {
            let (path, method) = ("/tasks", "post");
            let operation = &body["paths"][path][method];
            assert_eq!(
                operation["requestBody"]["required"],
                json!(true),
                "requestBody must be required for {method} {path}"
            );
            assert!(
                operation["requestBody"]["content"]["application/json"]["schema"].is_object(),
                "missing request schema for {method} {path}"
            );
        }
        assert_eq!(
            body["components"]["schemas"]["RestErrorEnvelope"]["required"],
            json!(["error"])
        );
        assert_eq!(
            body["components"]["schemas"]["RestErrorEnvelope"]["properties"]["error"]["required"],
            json!(["code", "message"])
        );
        assert!(
            body["components"]["schemas"]["RestErrorEnvelope"]["properties"]["error"]["properties"]
                ["details"]
                .is_object()
        );
        assert_eq!(
            body["paths"]["/status"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]
                ["$ref"],
            json!("#/components/schemas/NucleusStatusView")
        );
        assert_eq!(
            body["paths"]["/status"]["get"]["responses"]["500"]["content"]["application/json"]["schema"]
                ["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/tasks"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]
                ["type"],
            json!("array")
        );
        assert_eq!(
            body["paths"]["/tasks"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]
                ["items"]["$ref"],
            json!("#/components/schemas/TaskRecord")
        );
        assert_eq!(
            body["paths"]["/tasks"]["post"]["requestBody"]["content"]["application/json"]["schema"]
                ["$ref"],
            json!("#/components/schemas/TaskCreateParams")
        );
        assert_eq!(
            body["paths"]["/tasks"]["post"]["responses"]["201"]["content"]["application/json"]["schema"]
                ["$ref"],
            json!("#/components/schemas/TaskRecord")
        );
        assert_eq!(
            body["paths"]["/tasks"]["post"]["responses"]["400"]["content"]["application/json"]["schema"]
                ["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}"]["get"]["parameters"][0]["schema"]["type"],
            json!("string")
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}"]["get"]["responses"]["200"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/TaskRecord")
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}"]["get"]["responses"]["404"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}/cancel"]["post"]["x-si-purpose"],
            json!(
                "Use this for bounded external cancellation requests and then re-read the task or run to observe final state."
            )
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}/cancel"]["post"]["parameters"][0]["schema"]["type"],
            json!("string")
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["200"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/TaskCancelResultView")
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["404"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["503"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/workers"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]
                ["type"],
            json!("array")
        );
        assert_eq!(
            body["paths"]["/workers"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]
                ["items"]["$ref"],
            json!("#/components/schemas/WorkerRecord")
        );
        assert_eq!(
            body["paths"]["/workers/{worker_id}"]["get"]["responses"]["200"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/WorkerInspectView")
        );
        assert_eq!(
            body["paths"]["/workers/{worker_id}"]["get"]["parameters"][0]["schema"]["type"],
            json!("string")
        );
        assert_eq!(
            body["paths"]["/workers/{worker_id}"]["get"]["responses"]["404"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/sessions/{session_id}"]["get"]["responses"]["200"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/SessionRecord")
        );
        assert_eq!(
            body["paths"]["/sessions/{session_id}"]["get"]["parameters"][0]["schema"]["type"],
            json!("string")
        );
        assert_eq!(
            body["paths"]["/sessions/{session_id}"]["get"]["responses"]["404"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/runs/{run_id}"]["get"]["responses"]["200"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/RunRecord")
        );
        assert_eq!(
            body["paths"]["/runs/{run_id}"]["get"]["parameters"][0]["schema"]["type"],
            json!("string")
        );
        assert_eq!(
            body["paths"]["/runs/{run_id}"]["get"]["responses"]["404"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(body["paths"]["/tasks"]["post"]["security"][0]["bearerAuth"], json!([]));
        assert_eq!(
            body["paths"]["/tasks/{task_id}/cancel"]["post"]["security"][0]["bearerAuth"],
            json!([])
        );
        assert_eq!(
            body["paths"]["/tasks"]["post"]["responses"]["401"]["content"]["application/json"]["schema"]
                ["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["401"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/tasks"]["get"]["responses"]["500"]["content"]["application/json"]["schema"]
                ["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/tasks"]["post"]["responses"]["500"]["content"]["application/json"]["schema"]
                ["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}"]["get"]["responses"]["500"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["500"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/workers"]["get"]["responses"]["500"]["content"]["application/json"]["schema"]
                ["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/workers/{worker_id}"]["get"]["responses"]["500"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/sessions/{session_id}"]["get"]["responses"]["500"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/runs/{run_id}"]["get"]["responses"]["500"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/openapi.json"]["get"]["responses"]["500"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/openapi.json"]["get"]["responses"]["200"]["content"]["application/json"]
                ["schema"]["$ref"],
            json!("#/components/schemas/OpenApiDocumentView")
        );
        assert!(
            body["components"]["schemas"]["OpenApiDocumentView"]["properties"]["paths"]
                ["properties"]
                .is_object()
        );
        assert_object_schemas_have_properties(&body["components"]["schemas"], "components.schemas");
        assert_object_schemas_have_properties(&body["paths"], "paths");
        for (path, method) in [
            ("/status", "get"),
            ("/tasks", "get"),
            ("/tasks", "post"),
            ("/tasks/{task_id}", "get"),
            ("/tasks/{task_id}/cancel", "post"),
            ("/workers", "get"),
            ("/workers/{worker_id}", "get"),
            ("/sessions/{session_id}", "get"),
            ("/runs/{run_id}", "get"),
        ] {
            assert_eq!(
                body["paths"][path][method]["security"],
                json!([{ "bearerAuth": [] }]),
                "OpenAPI security must require bearer auth for {method} {path}"
            );
        }
        assert_eq!(body["paths"]["/openapi.json"]["get"]["security"], json!([]));
        assert_eq!(
            body["components"]["schemas"]["TaskCancelResultView"]["required"],
            json!(["task", "cancellation_requested"])
        );
        assert_eq!(
            body["components"]["schemas"]["WorkerInspectView"]["properties"]["worker"]["allOf"][0]
                ["$ref"],
            json!("#/components/schemas/WorkerRecord")
        );
    }

    #[tokio::test]
    async fn rest_all_requests_require_bearer_token_when_auth_is_configured() {
        let temp = tempdir().expect("tempdir");
        let app = NucleusService::open(NucleusConfig {
            bind_addr: "0.0.0.0:9898".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: Some("secret-token".to_owned()),
        })
        .expect("service")
        .router();

        let create_response = app
            .clone()
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/tasks")
                    .header("content-type", "application/json")
                    .body(Body::from(
                        serde_json::to_vec(&json!({
                            "title": "Auth gated task",
                            "instructions": "Should require auth",
                        }))
                        .expect("serialize request"),
                    ))
                    .expect("request"),
            )
            .await
            .expect("create response");
        assert_eq!(create_response.status(), StatusCode::UNAUTHORIZED);
        let create_body = response_json(create_response).await;
        assert_eq!(create_body["error"]["code"], json!("unauthorized"));

        let status_response = app
            .oneshot(Request::builder().uri("/status").body(Body::empty()).expect("request"))
            .await
            .expect("status response");
        assert_eq!(status_response.status(), StatusCode::UNAUTHORIZED);
    }

    #[tokio::test]
    async fn rest_write_requests_accept_matching_bearer_token_when_bound_beyond_loopback() {
        let temp = tempdir().expect("tempdir");
        let app = NucleusService::open(NucleusConfig {
            bind_addr: "0.0.0.0:9898".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: Some("secret-token".to_owned()),
        })
        .expect("service")
        .router();

        let create_response = app
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/tasks")
                    .header("content-type", "application/json")
                    .header("authorization", "Bearer secret-token")
                    .body(Body::from(
                        serde_json::to_vec(&json!({
                            "title": "Auth gated task",
                            "instructions": "Should pass with auth",
                        }))
                        .expect("serialize request"),
                    ))
                    .expect("request"),
            )
            .await
            .expect("create response");
        assert_eq!(create_response.status(), StatusCode::CREATED);
        let body = response_json(create_response).await;
        assert_eq!(body["title"], json!("Auth gated task"));
    }

    #[tokio::test]
    async fn rest_read_requests_accept_matching_bearer_token_when_auth_is_configured() {
        let temp = tempdir().expect("tempdir");
        let app = NucleusService::open(NucleusConfig {
            bind_addr: "0.0.0.0:9898".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: Some("secret-token".to_owned()),
        })
        .expect("service")
        .router();

        let create_response = app
            .clone()
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/tasks")
                    .header("content-type", "application/json")
                    .header("authorization", "Bearer secret-token")
                    .body(Body::from(
                        serde_json::to_vec(&json!({
                            "title": "Readable task",
                            "instructions": "Read should stay available",
                            "profile": "america",
                        }))
                        .expect("serialize request"),
                    ))
                    .expect("request"),
            )
            .await
            .expect("create response");
        assert_eq!(create_response.status(), StatusCode::CREATED);
        let created = response_json(create_response).await;
        let task_id = created["task_id"].as_str().expect("task id").to_owned();

        let list_response = app
            .clone()
            .oneshot(
                Request::builder()
                    .uri("/tasks")
                    .header("authorization", "Bearer secret-token")
                    .body(Body::empty())
                    .expect("request"),
            )
            .await
            .expect("list response");
        assert_eq!(list_response.status(), StatusCode::OK);
        let listed = response_json(list_response).await;
        assert!(
            listed
                .as_array()
                .expect("task list array")
                .iter()
                .any(|task| task["task_id"] == json!(task_id.clone()))
        );

        let inspect_response = app
            .oneshot(
                Request::builder()
                    .uri(format!("/tasks/{task_id}"))
                    .header("authorization", "Bearer secret-token")
                    .body(Body::empty())
                    .expect("request"),
            )
            .await
            .expect("inspect response");
        assert_eq!(inspect_response.status(), StatusCode::OK);
        let inspected = response_json(inspect_response).await;
        assert_eq!(inspected["task_id"], json!(task_id));
    }

    #[tokio::test]
    async fn rest_openapi_remains_public_when_auth_is_configured() {
        let temp = tempdir().expect("tempdir");
        let app = NucleusService::open(NucleusConfig {
            bind_addr: "0.0.0.0:9898".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: Some("secret-token".to_owned()),
        })
        .expect("service")
        .router();

        let response = app
            .oneshot(Request::builder().uri("/openapi.json").body(Body::empty()).expect("request"))
            .await
            .expect("openapi response");
        assert_eq!(response.status(), StatusCode::OK);
        let body = response_json(response).await;
        assert_eq!(body["openapi"], json!("3.1.0"));
    }

    #[tokio::test]
    async fn rest_openapi_accepts_matching_bearer_token_when_auth_is_configured() {
        let temp = tempdir().expect("tempdir");
        let app = NucleusService::open(NucleusConfig {
            bind_addr: "0.0.0.0:9898".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: Some("secret-token".to_owned()),
        })
        .expect("service")
        .router();

        let response = app
            .oneshot(
                Request::builder()
                    .uri("/openapi.json")
                    .header("authorization", "Bearer secret-token")
                    .body(Body::empty())
                    .expect("request"),
            )
            .await
            .expect("openapi response");
        assert_eq!(response.status(), StatusCode::OK);
        let body = response_json(response).await;
        assert_eq!(body["openapi"], json!("3.1.0"));
    }

    #[tokio::test]
    async fn gateway_all_requests_require_bearer_token_when_auth_is_configured() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "0.0.0.0:9898".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: Some("secret-token".to_owned()),
        })
        .expect("service");

        let write = service
            .dispatch_request_authorized(
                GatewayRequest {
                    id: json!("create"),
                    method: "task.create".to_owned(),
                    params: json!({
                        "title": "Blocked write",
                        "instructions": "No auth header",
                    }),
                },
                None,
            )
            .await;
        assert!(!write.ok);
        assert_eq!(write.error.as_ref().map(|error| error.code.as_str()), Some("unauthorized"));

        let read = service
            .dispatch_request_authorized(
                GatewayRequest {
                    id: json!("status"),
                    method: "nucleus.status".to_owned(),
                    params: json!({}),
                },
                None,
            )
            .await;
        assert!(!read.ok);
        assert_eq!(read.error.as_ref().map(|error| error.code.as_str()), Some("unauthorized"));
    }

    #[tokio::test]
    async fn gateway_reads_accept_matching_bearer_token_when_auth_is_configured() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "0.0.0.0:9898".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: Some("secret-token".to_owned()),
        })
        .expect("service");

        let created = service
            .dispatch_request_authorized(
                GatewayRequest {
                    id: json!("create"),
                    method: "task.create".to_owned(),
                    params: json!({
                        "title": "Readable task",
                        "instructions": "Read should stay available",
                        "profile": "america",
                    }),
                },
                Some("secret-token"),
            )
            .await;
        assert!(created.ok);
        let task_id =
            created.result.expect("created")["task_id"].as_str().expect("task id").to_owned();

        let listed = service
            .dispatch_request_authorized(
                GatewayRequest {
                    id: json!("list"),
                    method: "task.list".to_owned(),
                    params: json!({}),
                },
                Some("secret-token"),
            )
            .await;
        assert!(listed.ok);
        assert!(
            listed
                .result
                .expect("task list")
                .as_array()
                .expect("task list array")
                .iter()
                .any(|task| task["task_id"] == json!(task_id.clone()))
        );

        let inspected = service
            .dispatch_request_authorized(
                GatewayRequest {
                    id: json!("inspect"),
                    method: "task.inspect".to_owned(),
                    params: json!({ "task_id": task_id }),
                },
                Some("secret-token"),
            )
            .await;
        assert!(inspected.ok);
    }

    #[tokio::test]
    async fn read_only_gateway_and_rest_inspect_surfaces_require_bearer_token_when_auth_is_configured()
     {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "0.0.0.0:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: Some("secret-token".to_owned()),
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let session = service
            .dispatch_request_authorized(
                GatewayRequest {
                    id: json!("session-1"),
                    method: "session.create".to_owned(),
                    params: json!({
                        "profile": "america",
                        "home_dir": temp.path().join("home"),
                        "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                        "workdir": temp.path(),
                    }),
                },
                Some("secret-token"),
            )
            .await;
        assert!(session.ok);
        let session_payload = session.result.expect("session payload");
        let worker_id =
            session_payload["worker"]["worker_id"].as_str().expect("worker id").to_owned();
        let session_id =
            session_payload["session"]["session_id"].as_str().expect("session id").to_owned();

        let task = service
            .dispatch_request_authorized(
                GatewayRequest {
                    id: json!("task-1"),
                    method: "task.create".to_owned(),
                    params: json!({
                        "title": "Run a turn",
                        "instructions": "Drive one fake runtime turn",
                        "profile": "america",
                        "session_id": session_id,
                    }),
                },
                Some("secret-token"),
            )
            .await;
        assert!(task.ok);
        let task_id =
            task.result.expect("task payload")["task_id"].as_str().expect("task id").to_owned();

        let run = service
            .dispatch_request_authorized(
                GatewayRequest {
                    id: json!("run-1"),
                    method: "run.submit_turn".to_owned(),
                    params: json!({
                        "session_id": session_id,
                        "task_id": task_id,
                        "prompt": "Reply with nucleus-smoke",
                    }),
                },
                Some("secret-token"),
            )
            .await;
        assert!(run.ok);
        let run_id =
            run.result.expect("run payload")["run_id"].as_str().expect("run id").to_owned();

        thread::sleep(Duration::from_millis(150));

        let profile_list = service
            .dispatch_request_authorized(
                GatewayRequest {
                    id: json!("profile-list"),
                    method: "profile.list".to_owned(),
                    params: json!({}),
                },
                None,
            )
            .await;
        assert!(!profile_list.ok);
        assert_eq!(
            profile_list.error.as_ref().map(|error| error.code.as_str()),
            Some("unauthorized")
        );

        let worker_list = service
            .dispatch_request_authorized(
                GatewayRequest {
                    id: json!("worker-list"),
                    method: "worker.list".to_owned(),
                    params: json!({}),
                },
                None,
            )
            .await;
        assert!(!worker_list.ok);

        let worker_inspect = service
            .dispatch_request_authorized(
                GatewayRequest {
                    id: json!("worker-inspect"),
                    method: "worker.inspect".to_owned(),
                    params: json!({ "worker_id": worker_id }),
                },
                None,
            )
            .await;
        assert!(!worker_inspect.ok);

        let session_list = service
            .dispatch_request_authorized(
                GatewayRequest {
                    id: json!("session-list"),
                    method: "session.list".to_owned(),
                    params: json!({}),
                },
                None,
            )
            .await;
        assert!(!session_list.ok);

        let session_show = service
            .dispatch_request_authorized(
                GatewayRequest {
                    id: json!("session-show"),
                    method: "session.show".to_owned(),
                    params: json!({ "session_id": session_id.clone() }),
                },
                None,
            )
            .await;
        assert!(!session_show.ok);

        let run_inspect = service
            .dispatch_request_authorized(
                GatewayRequest {
                    id: json!("run-inspect"),
                    method: "run.inspect".to_owned(),
                    params: json!({ "run_id": run_id.clone() }),
                },
                None,
            )
            .await;
        assert!(!run_inspect.ok);

        let subscribed = service
            .dispatch_request_authorized(
                GatewayRequest {
                    id: json!("events-subscribe"),
                    method: "events.subscribe".to_owned(),
                    params: json!({}),
                },
                None,
            )
            .await;
        assert!(!subscribed.ok);

        let app = service.clone().router();

        let workers_response = app
            .clone()
            .oneshot(Request::builder().uri("/workers").body(Body::empty()).expect("request"))
            .await
            .expect("workers response");
        assert_eq!(workers_response.status(), StatusCode::UNAUTHORIZED);

        let worker_response = app
            .clone()
            .oneshot(
                Request::builder()
                    .uri(format!("/workers/{worker_id}"))
                    .body(Body::empty())
                    .expect("request"),
            )
            .await
            .expect("worker response");
        assert_eq!(worker_response.status(), StatusCode::UNAUTHORIZED);

        let session_response = app
            .clone()
            .oneshot(
                Request::builder()
                    .uri(format!("/sessions/{session_id}"))
                    .body(Body::empty())
                    .expect("request"),
            )
            .await
            .expect("session response");
        assert_eq!(session_response.status(), StatusCode::UNAUTHORIZED);

        let run_response = app
            .oneshot(
                Request::builder()
                    .uri(format!("/runs/{run_id}"))
                    .body(Body::empty())
                    .expect("request"),
            )
            .await
            .expect("run response");
        assert_eq!(run_response.status(), StatusCode::UNAUTHORIZED);
    }

    #[tokio::test]
    async fn gateway_mutations_accept_matching_bearer_token_when_bound_beyond_loopback() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "0.0.0.0:9898".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: Some("secret-token".to_owned()),
        })
        .expect("service");

        let write = service
            .dispatch_request_authorized(
                GatewayRequest {
                    id: json!("create"),
                    method: "task.create".to_owned(),
                    params: json!({
                        "title": "Authorized write",
                        "instructions": "Bearer token provided",
                    }),
                },
                Some("secret-token"),
            )
            .await;
        assert!(write.ok);
        assert_eq!(
            write.result.as_ref().and_then(|value| value["title"].as_str()),
            Some("Authorized write")
        );
    }

    #[tokio::test]
    async fn rest_task_endpoints_share_nucleus_source_of_truth() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:9898".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: None,
        })
        .expect("service");
        let app = service.clone().router();

        let create_response = app
            .clone()
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/tasks")
                    .header("content-type", "application/json")
                    .body(Body::from(
                        serde_json::to_vec(&json!({
                            "title": "REST task",
                            "instructions": "Create a durable task through REST",
                            "profile": "america",
                        }))
                        .expect("serialize request"),
                    ))
                    .expect("request"),
            )
            .await
            .expect("create response");
        assert_eq!(create_response.status(), StatusCode::CREATED);
        let created = response_json(create_response).await;
        let task_id = created["task_id"].as_str().expect("task id");

        let status_response = app
            .clone()
            .oneshot(Request::builder().uri("/status").body(Body::empty()).expect("request"))
            .await
            .expect("status response");
        assert_eq!(status_response.status(), StatusCode::OK);
        let status = response_json(status_response).await;
        assert_eq!(status["task_count"], json!(1));

        let list_response = app
            .clone()
            .oneshot(Request::builder().uri("/tasks").body(Body::empty()).expect("request"))
            .await
            .expect("list response");
        assert_eq!(list_response.status(), StatusCode::OK);
        let listed = response_json(list_response).await;
        assert_eq!(listed.as_array().map(|items| items.len()), Some(1));

        let inspect_response = app
            .clone()
            .oneshot(
                Request::builder()
                    .uri(format!("/tasks/{task_id}"))
                    .body(Body::empty())
                    .expect("request"),
            )
            .await
            .expect("inspect response");
        assert_eq!(inspect_response.status(), StatusCode::OK);
        let inspected = response_json(inspect_response).await;
        assert_eq!(inspected["status"], json!("queued"));

        let cancel_response = app
            .clone()
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri(format!("/tasks/{task_id}/cancel"))
                    .body(Body::empty())
                    .expect("request"),
            )
            .await
            .expect("cancel response");
        assert_eq!(cancel_response.status(), StatusCode::OK);
        let cancelled = response_json(cancel_response).await;
        assert_eq!(cancelled["task"]["status"], json!("cancelled"));
        assert_eq!(cancelled["cancellation_requested"], json!(false));

        let stored = service
            .store
            .inspect_task(&TaskId::new(task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(stored.status, TaskStatus::Cancelled);
    }

    #[tokio::test]
    async fn rest_task_create_rejects_non_slug_profile_names() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:9898".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: None,
        })
        .expect("service");
        let app = service.clone().router();

        let response = app
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/tasks")
                    .header("content-type", "application/json")
                    .body(Body::from(
                        serde_json::to_vec(&json!({
                            "title": "REST invalid profile task",
                            "instructions": "Reject uppercase profile names",
                            "profile": "America",
                        }))
                        .expect("serialize request"),
                    ))
                    .expect("request"),
            )
            .await
            .expect("create response");
        assert_eq!(response.status(), StatusCode::BAD_REQUEST);
        let body = response_json(response).await;
        assert_eq!(body["error"]["code"], json!("invalid_params"));
        assert!(
            body["error"]["message"]
                .as_str()
                .map(|value| value.contains("profile name must match"))
                .unwrap_or(false)
        );
        assert_eq!(service.store.list_tasks().expect("list tasks").len(), 0);
    }

    #[tokio::test]
    async fn rest_task_create_rejects_blank_title_and_instructions() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:9898".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: None,
        })
        .expect("service");
        let app = service.clone().router();

        let response = app
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/tasks")
                    .header("content-type", "application/json")
                    .body(Body::from(
                        serde_json::to_vec(&json!({
                            "title": "   ",
                            "instructions": "",
                        }))
                        .expect("serialize request"),
                    ))
                    .expect("request"),
            )
            .await
            .expect("create response");
        assert_eq!(response.status(), StatusCode::BAD_REQUEST);
        let body = response_json(response).await;
        assert_eq!(body["error"]["code"], json!("invalid_params"));
        assert!(
            body["error"]["message"]
                .as_str()
                .map(|value| value.contains("cannot be empty"))
                .unwrap_or(false)
        );
        assert_eq!(service.store.list_tasks().expect("list tasks").len(), 0);
    }

    #[tokio::test]
    async fn rest_task_create_blocks_missing_session_immediately() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:9898".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: None,
        })
        .expect("service");
        let app = service.clone().router();

        let response = app
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/tasks")
                    .header("content-type", "application/json")
                    .body(Body::from(
                        serde_json::to_vec(&json!({
                            "title": "REST missing session task",
                            "instructions": "Reject impossible session binding at intake",
                            "profile": "america",
                            "session_id": "si-session-missing",
                        }))
                        .expect("serialize request"),
                    ))
                    .expect("request"),
            )
            .await
            .expect("create response");
        assert_eq!(response.status(), StatusCode::CREATED);
        let created = response_json(response).await;
        assert_eq!(created["status"], json!("blocked"));
        assert_eq!(created["blocked_reason"], json!("session_broken"));
        assert_eq!(created["latest_run_id"], json!(null));
    }

    #[tokio::test]
    async fn rest_task_create_blocks_session_profile_mismatch_immediately() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");
        let app = service.clone().router();

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-rest-mismatch"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = session
            .result
            .as_ref()
            .and_then(|item| item["session"]["session_id"].as_str())
            .expect("session id");

        let response = app
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/tasks")
                    .header("content-type", "application/json")
                    .body(Body::from(
                        serde_json::to_vec(&json!({
                            "title": "REST mismatched session task",
                            "instructions": "Reject cross-profile session reuse at intake",
                            "profile": "europe",
                            "session_id": session_id,
                        }))
                        .expect("serialize request"),
                    ))
                    .expect("request"),
            )
            .await
            .expect("create response");
        assert_eq!(response.status(), StatusCode::CREATED);
        let created = response_json(response).await;
        assert_eq!(created["status"], json!("blocked"));
        assert_eq!(created["blocked_reason"], json!("session_broken"));
        assert_eq!(created["latest_run_id"], json!(null));
    }

    #[tokio::test]
    async fn rest_task_create_marks_session_broken_when_referenced_session_lacks_thread_id() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");
        let app = service.clone().router();

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-rest-missing-thread"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = SessionId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["session"]["session_id"].as_str())
                .expect("session id")
                .to_owned(),
        )
        .expect("session id");

        let mut persisted_session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        persisted_session.app_server_thread_id = None;
        service
            .store
            .write_session_projection_locked(&persisted_session)
            .expect("persist session without thread id");

        let response = app
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/tasks")
                    .header("content-type", "application/json")
                    .body(Body::from(
                        serde_json::to_vec(&json!({
                            "title": "REST missing thread task",
                            "instructions": "Reject impossible session reuse without a thread id",
                            "profile": "america",
                            "session_id": session_id.as_str(),
                        }))
                        .expect("serialize request"),
                    ))
                    .expect("request"),
            )
            .await
            .expect("create response");
        assert_eq!(response.status(), StatusCode::CREATED);
        let created = response_json(response).await;
        assert_eq!(created["status"], json!("blocked"));
        assert_eq!(created["blocked_reason"], json!("session_broken"));
        assert_eq!(created["latest_run_id"], json!(null));

        let session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        assert_eq!(session.lifecycle_state, SessionLifecycleState::Broken);
    }

    #[tokio::test]
    async fn rest_worker_session_and_run_endpoints_reflect_gateway_state() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");
        let app = service.clone().router();

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-rest"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let worker_id = session
            .result
            .as_ref()
            .and_then(|item| item["worker"]["worker_id"].as_str())
            .expect("worker id");
        let session_id = session
            .result
            .as_ref()
            .and_then(|item| item["session"]["session_id"].as_str())
            .expect("session id");

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-rest"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "REST inspect run",
                    "instructions": "Reply with nucleus-smoke",
                    "profile": "america",
                    "session_id": session_id,
                }),
            })
            .await;
        assert!(task.ok);
        let task_id =
            task.result.as_ref().and_then(|item| item["task_id"].as_str()).expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-rest"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id,
                    "task_id": task_id,
                    "prompt": "Reply with nucleus-smoke",
                }),
            })
            .await;
        assert!(run.ok);
        let run_id = run.result.as_ref().and_then(|item| item["run_id"].as_str()).expect("run id");

        thread::sleep(Duration::from_millis(150));

        let workers_response = app
            .clone()
            .oneshot(Request::builder().uri("/workers").body(Body::empty()).expect("request"))
            .await
            .expect("workers response");
        assert_eq!(workers_response.status(), StatusCode::OK);
        let workers = response_json(workers_response).await;
        assert_eq!(workers.as_array().map(|items| items.len()), Some(1));

        let worker_response = app
            .clone()
            .oneshot(
                Request::builder()
                    .uri(format!("/workers/{worker_id}"))
                    .body(Body::empty())
                    .expect("request"),
            )
            .await
            .expect("worker response");
        assert_eq!(worker_response.status(), StatusCode::OK);
        let worker = response_json(worker_response).await;
        assert_eq!(worker["worker"]["worker_id"], json!(worker_id));

        let session_response = app
            .clone()
            .oneshot(
                Request::builder()
                    .uri(format!("/sessions/{session_id}"))
                    .body(Body::empty())
                    .expect("request"),
            )
            .await
            .expect("session response");
        assert_eq!(session_response.status(), StatusCode::OK);
        let session_body = response_json(session_response).await;
        assert_eq!(session_body["session_id"], json!(session_id));

        let run_response = app
            .oneshot(
                Request::builder()
                    .uri(format!("/runs/{run_id}"))
                    .body(Body::empty())
                    .expect("request"),
            )
            .await
            .expect("run response");
        assert_eq!(run_response.status(), StatusCode::OK);
        let run_body = response_json(run_response).await;
        assert_eq!(run_body["run_id"], json!(run_id));
        assert_eq!(run_body["status"], json!("completed"));
    }

    #[tokio::test]
    async fn worker_probe_uses_runtime_and_persists_worker_state() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let response = service
            .dispatch_request(GatewayRequest {
                id: json!("probe-1"),
                method: "worker.probe".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(response.ok);
        let payload = response.result.expect("probe result");
        let worker_id = payload["worker"]["worker_id"].as_str().expect("worker id").to_owned();
        let inspected = service
            .dispatch_request(GatewayRequest {
                id: json!("inspect-1"),
                method: "worker.inspect".to_owned(),
                params: json!({ "worker_id": worker_id }),
            })
            .await;
        assert!(inspected.ok);
        assert_eq!(inspected.result.expect("worker")["worker"]["profile"], json!("america"));
        assert_eq!(service.status().expect("status").worker_count, 1);
        let profile = ProfileName::new("america").expect("profile");
        assert_eq!(payload["worker"]["profile"], json!(profile.as_str()));
        let worker_id = WorkerId::new(worker_id).expect("worker id");
        let worker_summary =
            fs::read_to_string(service.store.paths().worker_summary_path(&worker_id))
                .expect("read worker summary");
        assert!(worker_summary.contains("# Worker"));
        assert!(worker_summary.contains("Profile: `america`"));
        assert!(worker_summary.contains("Status: `ready`"));
    }

    #[tokio::test]
    async fn worker_restart_restarts_idle_worker_through_gateway() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-restart"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let worker_id = session
            .result
            .as_ref()
            .and_then(|item| item["worker"]["worker_id"].as_str())
            .expect("worker id");

        let restarted = service
            .dispatch_request(GatewayRequest {
                id: json!("worker-restart"),
                method: "worker.restart".to_owned(),
                params: json!({ "worker_id": worker_id }),
            })
            .await;
        assert!(restarted.ok);
        let payload = restarted.result.expect("restart payload");
        assert_eq!(payload["worker"]["worker_id"], json!(worker_id));
        assert_eq!(payload["worker"]["status"], json!("ready"));
        assert_eq!(payload["runtime"]["worker_id"], json!(worker_id));
    }

    #[tokio::test]
    async fn worker_restart_rejects_workers_with_active_runs() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::with_run_delay(Duration::from_millis(150))),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-restart-active"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let worker_id = session
            .result
            .as_ref()
            .and_then(|item| item["worker"]["worker_id"].as_str())
            .expect("worker id")
            .to_owned();
        let session_id = session
            .result
            .as_ref()
            .and_then(|item| item["session"]["session_id"].as_str())
            .expect("session id")
            .to_owned();

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-restart-active"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Worker restart guard",
                    "instructions": "Keep the run active while restart is requested",
                    "profile": "america",
                    "session_id": session_id,
                }),
            })
            .await;
        assert!(task.ok);
        let task_id =
            task.result.as_ref().and_then(|item| item["task_id"].as_str()).expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-restart-active"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id,
                    "task_id": task_id,
                    "prompt": "hold this run open briefly",
                }),
            })
            .await;
        assert!(run.ok);

        thread::sleep(Duration::from_millis(40));

        let restarted = service
            .dispatch_request(GatewayRequest {
                id: json!("worker-restart-active"),
                method: "worker.restart".to_owned(),
                params: json!({ "worker_id": worker_id }),
            })
            .await;
        assert!(!restarted.ok);
        assert!(
            restarted
                .error
                .as_ref()
                .map(|error| error.message.contains("worker has active runs"))
                .unwrap_or(false)
        );

        wait_for_task_status(&service, task_id, TaskStatus::Done);
    }

    #[tokio::test]
    async fn supervision_restarts_failed_idle_worker_within_retry_budget() {
        let temp = tempdir().expect("tempdir");
        let runtime = FakeRuntime::default();
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime.clone()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-auto-restart"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let worker_id = WorkerId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["worker"]["worker_id"].as_str())
                .expect("worker id")
                .to_owned(),
        )
        .expect("worker id");

        runtime.stop_worker(&worker_id).expect("stop worker");
        service.reconcile_and_dispatch_once().expect("reconcile failed worker");

        let worker = service
            .store
            .inspect_worker(&worker_id)
            .expect("inspect worker")
            .expect("worker exists");
        assert_eq!(worker.status, WorkerStatus::Ready);
        assert!(runtime.inspect_worker(&worker_id).expect("inspect live runtime").is_some());
    }

    #[tokio::test]
    async fn supervision_stops_retrying_worker_after_bounded_failures() {
        let temp = tempdir().expect("tempdir");
        let runtime = FakeRuntime::default();
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime.clone()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-auto-restart-fail"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let worker_id = WorkerId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["worker"]["worker_id"].as_str())
                .expect("worker id")
                .to_owned(),
        )
        .expect("worker id");

        runtime.stop_worker(&worker_id).expect("stop worker");
        runtime.fail_next_starts(MAX_WORKER_RESTART_ATTEMPTS as usize);

        for _ in 0..(MAX_WORKER_RESTART_ATTEMPTS + 2) {
            service.reconcile_and_dispatch_once().expect("reconcile failed worker");
            thread::sleep(Duration::from_millis(125));
        }

        let worker = service
            .store
            .inspect_worker(&worker_id)
            .expect("inspect worker")
            .expect("worker exists");
        assert_eq!(worker.status, WorkerStatus::Failed);
        assert_eq!(runtime.start_call_count(), (MAX_WORKER_RESTART_ATTEMPTS + 1) as usize);

        let warnings = load_canonical_events(&service.store.paths().events_path)
            .expect("load events")
            .into_iter()
            .filter(|event| event.event_type == CanonicalEventType::SystemWarning)
            .collect::<Vec<_>>();
        assert!(warnings.iter().any(|event| {
            event.data.payload["message"] == json!("worker restart attempts exhausted")
                && event.data.payload["details"]["worker_id"] == json!(worker_id.as_str())
        }));
    }

    #[tokio::test]
    async fn dispatch_blocks_tasks_until_operator_restart_after_exhausted_auto_restart() {
        let temp = tempdir().expect("tempdir");
        let runtime = FakeRuntime::default();
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime.clone()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-auto-restart-exhausted-block"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let worker_id = WorkerId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["worker"]["worker_id"].as_str())
                .expect("worker id")
                .to_owned(),
        )
        .expect("worker id");

        runtime.stop_worker(&worker_id).expect("stop worker");
        runtime.fail_next_starts(MAX_WORKER_RESTART_ATTEMPTS as usize);
        for _ in 0..(MAX_WORKER_RESTART_ATTEMPTS + 2) {
            service.reconcile_and_dispatch_once().expect("reconcile failed worker");
            thread::sleep(Duration::from_millis(125));
        }

        let exhausted_worker = service
            .store
            .inspect_worker(&worker_id)
            .expect("inspect worker")
            .expect("worker exists");
        assert_eq!(exhausted_worker.status, WorkerStatus::Failed);
        assert_eq!(runtime.start_call_count(), (MAX_WORKER_RESTART_ATTEMPTS + 1) as usize);

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-auto-restart-exhausted-block"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Stay blocked until operator restart",
                    "instructions": "Do not bypass exhausted auto-restart state implicitly",
                    "profile": "america",
                }),
            })
            .await;
        assert!(created.ok);
        let task_id = TaskId::new(
            created
                .result
                .as_ref()
                .and_then(|item| item["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        service.reconcile_and_dispatch_once().expect("dispatch exhausted task");

        let blocked =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(blocked.status, TaskStatus::Blocked);
        assert_eq!(blocked.blocked_reason, Some(BlockedReason::WorkerUnavailable));
        assert_eq!(runtime.start_call_count(), (MAX_WORKER_RESTART_ATTEMPTS + 1) as usize);

        let restarted = service
            .dispatch_request(GatewayRequest {
                id: json!("worker-restart-after-exhausted-auto-restart"),
                method: "worker.restart".to_owned(),
                params: json!({ "worker_id": worker_id.as_str() }),
            })
            .await;
        assert!(restarted.ok);

        let requeued =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(requeued.status, TaskStatus::Queued);
        assert_eq!(requeued.blocked_reason, None);

        service.reconcile_and_dispatch_once().expect("dispatch requeued task");
        wait_for_task_status(&service, task_id.as_str(), TaskStatus::Done);
    }

    #[tokio::test]
    async fn worker_repair_auth_reprobes_worker_and_starts_runtime_if_missing() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let response = service
            .dispatch_request(GatewayRequest {
                id: json!("probe-repair-auth"),
                method: "worker.probe".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(response.ok);
        let worker_id = response
            .result
            .as_ref()
            .and_then(|item| item["worker"]["worker_id"].as_str())
            .expect("worker id");

        let repaired = service
            .dispatch_request(GatewayRequest {
                id: json!("worker-repair-auth"),
                method: "worker.repair_auth".to_owned(),
                params: json!({ "worker_id": worker_id }),
            })
            .await;
        assert!(repaired.ok);
        let payload = repaired.result.expect("repair payload");
        assert_eq!(payload["worker"]["worker_id"], json!(worker_id));
        assert_eq!(payload["worker"]["status"], json!("ready"));
        assert_eq!(payload["runtime"]["worker_id"], json!(worker_id));
    }

    #[tokio::test]
    async fn worker_repair_auth_clears_exhausted_auto_restart_boundary() {
        let temp = tempdir().expect("tempdir");
        let runtime = FakeRuntime::default();
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime.clone()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-repair-auth-after-exhausted-restart"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let worker_id = WorkerId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["worker"]["worker_id"].as_str())
                .expect("worker id")
                .to_owned(),
        )
        .expect("worker id");

        runtime.stop_worker(&worker_id).expect("stop worker");
        runtime.fail_next_starts(MAX_WORKER_RESTART_ATTEMPTS as usize);
        for _ in 0..(MAX_WORKER_RESTART_ATTEMPTS + 2) {
            service.reconcile_and_dispatch_once().expect("reconcile failed worker");
            thread::sleep(Duration::from_millis(125));
        }

        let failed_session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-create-exhausted-before-repair-auth"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(!failed_session.ok);
        assert!(
            failed_session
                .error
                .as_ref()
                .expect("session create error")
                .message
                .contains("worker restart attempts exhausted")
        );

        let repaired = service
            .dispatch_request(GatewayRequest {
                id: json!("worker-repair-auth-after-exhausted-restart"),
                method: "worker.repair_auth".to_owned(),
                params: json!({ "worker_id": worker_id.as_str() }),
            })
            .await;
        assert!(repaired.ok);

        let resumed_session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-create-after-repair-auth"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(resumed_session.ok);
    }

    #[tokio::test]
    async fn worker_repair_auth_refreshes_persisted_profile_state() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let response = service
            .dispatch_request(GatewayRequest {
                id: json!("probe-repair-auth-refresh-profile"),
                method: "worker.probe".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(response.ok);
        let worker_id = response
            .result
            .as_ref()
            .and_then(|item| item["worker"]["worker_id"].as_str())
            .expect("worker id");

        let profile_path = service
            .store
            .paths()
            .profile_path(&ProfileName::new("america".to_owned()).expect("profile name"));
        write_json_atomic(
            &profile_path,
            &ProfileRecord {
                profile: ProfileName::new("america".to_owned()).expect("profile name"),
                account_identity: Some("stale@example.com".to_owned()),
                codex_home: temp
                    .path()
                    .join("stale/.si/codex/profiles/america")
                    .display()
                    .to_string(),
                auth_mode: Some("stale-auth".to_owned()),
                preferred_model: Some("stale-model".to_owned()),
                runtime_defaults: BTreeMap::from([("model".to_owned(), "stale-model".to_owned())]),
            },
        )
        .expect("persist stale profile");

        let repaired = service
            .dispatch_request(GatewayRequest {
                id: json!("worker-repair-auth-refresh-profile"),
                method: "worker.repair_auth".to_owned(),
                params: json!({ "worker_id": worker_id }),
            })
            .await;
        assert!(repaired.ok);

        let profiles = service.store.list_profile_records().expect("list profiles");
        let profile = profiles
            .into_iter()
            .find(|profile| profile.profile.as_str() == "america")
            .expect("america profile");
        assert_eq!(profile.account_identity.as_deref(), Some("america@example.com"));
        assert_eq!(profile.preferred_model.as_deref(), Some("gpt-5.4"));
        assert_eq!(
            profile.codex_home,
            temp.path().join("home/.si/codex/profiles/america").display().to_string()
        );
        assert!(profile.auth_mode.is_none());
        assert!(profile.runtime_defaults.is_empty());
    }

    #[tokio::test]
    async fn worker_restart_requeues_worker_unavailable_tasks_for_profile() {
        let temp = tempdir().expect("tempdir");
        let runtime = FakeRuntime::default();
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime.clone()),
        )
        .expect("service");

        let probed = service
            .dispatch_request(GatewayRequest {
                id: json!("probe-restart-requeue"),
                method: "worker.probe".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(probed.ok);
        let worker_id = WorkerId::new(
            probed
                .result
                .as_ref()
                .and_then(|item| item["worker"]["worker_id"].as_str())
                .expect("worker id")
                .to_owned(),
        )
        .expect("worker id");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-restart-requeue"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Restart should requeue blocked task",
                    "instructions": "Run after the worker restart succeeds",
                    "profile": "america",
                }),
            })
            .await;
        assert!(created.ok);
        let task_id = TaskId::new(
            created
                .result
                .as_ref()
                .and_then(|item| item["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        runtime.fail_next_starts(2);
        service.reconcile_and_dispatch_once().expect("dispatch blocked task");

        let blocked =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(blocked.status, TaskStatus::Blocked);
        assert_eq!(blocked.blocked_reason, Some(BlockedReason::WorkerUnavailable));

        let restarted = service
            .dispatch_request(GatewayRequest {
                id: json!("worker-restart-requeue"),
                method: "worker.restart".to_owned(),
                params: json!({ "worker_id": worker_id.as_str() }),
            })
            .await;
        assert!(restarted.ok);

        let requeued =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(requeued.status, TaskStatus::Queued);
        assert_eq!(requeued.blocked_reason, None);

        service.reconcile_and_dispatch_once().expect("dispatch requeued task");
        wait_for_task_status(&service, task_id.as_str(), TaskStatus::Done);
    }

    #[tokio::test]
    async fn worker_restart_does_not_requeue_worker_unavailable_task_with_broken_session() {
        let temp = tempdir().expect("tempdir");
        let runtime = FakeRuntime::with_run_delay(Duration::from_millis(400));
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime.clone()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("worker-restart-broken-session"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let worker_id = WorkerId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["worker"]["worker_id"].as_str())
                .expect("worker id")
                .to_owned(),
        )
        .expect("worker id");
        let session_id = SessionId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["session"]["session_id"].as_str())
                .expect("session id")
                .to_owned(),
        )
        .expect("session id");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-worker-restart-broken-session"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Do not requeue broken-session worker task",
                    "instructions": "Keep running until the worker disappears",
                    "profile": "america",
                    "session_id": session_id.as_str(),
                }),
            })
            .await;
        assert!(created.ok);
        let task_id = TaskId::new(
            created
                .result
                .as_ref()
                .and_then(|item| item["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-worker-restart-broken-session"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id.as_str(),
                    "task_id": task_id.as_str(),
                    "prompt": "Generate a long response",
                }),
            })
            .await;
        assert!(run.ok);

        let runtime_worker_id = worker_id.clone();
        runtime.stop_worker(&runtime_worker_id).expect("stop worker");
        service.reconcile_and_dispatch_once().expect("reconcile worker loss");

        let blocked =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(blocked.status, TaskStatus::Blocked);
        assert_eq!(blocked.blocked_reason, Some(BlockedReason::WorkerUnavailable));
        let broken_session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        assert_eq!(broken_session.lifecycle_state, SessionLifecycleState::Broken);

        let restarted = service
            .dispatch_request(GatewayRequest {
                id: json!("worker-restart-after-broken-session"),
                method: "worker.restart".to_owned(),
                params: json!({ "worker_id": worker_id.as_str() }),
            })
            .await;
        assert!(restarted.ok);

        let still_blocked =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(still_blocked.status, TaskStatus::Blocked);
        assert_eq!(still_blocked.blocked_reason, Some(BlockedReason::WorkerUnavailable));
        assert_eq!(
            still_blocked.latest_run_id.as_ref().map(|id| id.as_str()),
            blocked.latest_run_id.as_ref().map(|id| id.as_str())
        );
    }

    #[tokio::test]
    async fn worker_restart_does_not_requeue_other_profile_assignment() {
        let temp = tempdir().expect("tempdir");
        let runtime = FakeRuntime::default();
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime.clone()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-america-restart-scope"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let america_worker_id = WorkerId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["session"]["worker_id"].as_str())
                .expect("worker id")
                .to_owned(),
        )
        .expect("worker id");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-britain-restart-scope"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Do not requeue other profile task",
                    "instructions": "Stay blocked until a britain worker is repaired or restarted",
                    "profile": "britain",
                }),
            })
            .await;
        assert!(created.ok);
        let britain_task_id = TaskId::new(
            created
                .result
                .as_ref()
                .and_then(|item| item["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        runtime.fail_next_starts(1);
        service.reconcile_and_dispatch_once().expect("dispatch britain blocked task");

        let blocked_task = service
            .store
            .inspect_task(&britain_task_id)
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(blocked_task.status, TaskStatus::Blocked);
        assert_eq!(blocked_task.profile.as_ref().map(ProfileName::as_str), Some("britain"));
        assert_eq!(blocked_task.blocked_reason, Some(BlockedReason::WorkerUnavailable));
        assert!(blocked_task.latest_run_id.is_none());

        let restarted = service
            .dispatch_request(GatewayRequest {
                id: json!("worker-restart-scope"),
                method: "worker.restart".to_owned(),
                params: json!({ "worker_id": america_worker_id.as_str() }),
            })
            .await;
        assert!(restarted.ok);

        let still_blocked = service
            .store
            .inspect_task(&britain_task_id)
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(still_blocked.status, TaskStatus::Blocked);
        assert_eq!(still_blocked.profile.as_ref().map(ProfileName::as_str), Some("britain"));
        assert_eq!(still_blocked.blocked_reason, Some(BlockedReason::WorkerUnavailable));
        assert!(still_blocked.latest_run_id.is_none());
    }

    #[tokio::test]
    async fn worker_repair_auth_requeues_auth_required_tasks_for_profile() {
        let temp = tempdir().expect("tempdir");
        let codex_home = temp.path().join("home/.si/codex/profiles/america");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-auth-requeue"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": codex_home.clone(),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let worker_id = WorkerId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["session"]["worker_id"].as_str())
                .expect("worker id")
                .to_owned(),
        )
        .expect("worker id");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-auth-requeue"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Fort auth retry task",
                    "instructions": "Use si fort status before continuing",
                    "profile": "america",
                }),
            })
            .await;
        assert!(created.ok);
        let task_id = TaskId::new(
            created
                .result
                .as_ref()
                .and_then(|item| item["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        service.reconcile_and_dispatch_once().expect("dispatch auth-blocked task");

        let blocked =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(blocked.status, TaskStatus::Blocked);
        assert_eq!(blocked.blocked_reason, Some(BlockedReason::AuthRequired));

        write_fort_session_state(
            &codex_home,
            "america",
            PersistedSessionState {
                agent_id: "si-codex-america".to_owned(),
                session_id: "fort-session".to_owned(),
                host: "https://fort.example.invalid".to_owned(),
                runtime_host: "https://fort-runtime.example.invalid".to_owned(),
                access_expires_at: (Utc::now() + ChronoDuration::hours(1)).to_rfc3339(),
                refresh_expires_at: (Utc::now() + ChronoDuration::hours(12)).to_rfc3339(),
                ..PersistedSessionState::default()
            },
        );

        let repaired = service
            .dispatch_request(GatewayRequest {
                id: json!("worker-auth-requeue"),
                method: "worker.repair_auth".to_owned(),
                params: json!({ "worker_id": worker_id.as_str() }),
            })
            .await;
        assert!(repaired.ok);

        let requeued =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(requeued.status, TaskStatus::Queued);
        assert_eq!(requeued.blocked_reason, None);

        service.reconcile_and_dispatch_once().expect("dispatch requeued auth task");
        wait_for_task_status(&service, task_id.as_str(), TaskStatus::Done);
    }

    #[tokio::test]
    async fn worker_repair_auth_requeues_fort_unavailable_tasks_for_profile() {
        let temp = tempdir().expect("tempdir");
        let codex_home = temp.path().join("home/.si/codex/profiles/america");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-fort-unavailable-requeue"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": codex_home.clone(),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let worker_id = WorkerId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["session"]["worker_id"].as_str())
                .expect("worker id")
                .to_owned(),
        )
        .expect("worker id");

        write_fort_session_state(
            &codex_home,
            "america",
            PersistedSessionState {
                agent_id: "si-codex-america".to_owned(),
                session_id: "fort-session".to_owned(),
                access_expires_at: (Utc::now() + ChronoDuration::hours(1)).to_rfc3339(),
                refresh_expires_at: (Utc::now() + ChronoDuration::hours(12)).to_rfc3339(),
                ..PersistedSessionState::default()
            },
        );

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-fort-unavailable-requeue"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Fort unavailable retry task",
                    "instructions": "Use si fort refresh before continuing",
                    "profile": "america",
                }),
            })
            .await;
        assert!(created.ok);
        let task_id = TaskId::new(
            created
                .result
                .as_ref()
                .and_then(|item| item["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        service.reconcile_and_dispatch_once().expect("dispatch fort-unavailable task");

        let blocked =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(blocked.status, TaskStatus::Blocked);
        assert_eq!(blocked.blocked_reason, Some(BlockedReason::FortUnavailable));

        write_fort_session_state(
            &codex_home,
            "america",
            PersistedSessionState {
                agent_id: "si-codex-america".to_owned(),
                session_id: "fort-session".to_owned(),
                host: "https://fort.example.invalid".to_owned(),
                runtime_host: "https://fort-runtime.example.invalid".to_owned(),
                access_expires_at: (Utc::now() + ChronoDuration::hours(1)).to_rfc3339(),
                refresh_expires_at: (Utc::now() + ChronoDuration::hours(12)).to_rfc3339(),
                ..PersistedSessionState::default()
            },
        );

        let repaired = service
            .dispatch_request(GatewayRequest {
                id: json!("worker-fort-unavailable-requeue"),
                method: "worker.repair_auth".to_owned(),
                params: json!({ "worker_id": worker_id.as_str() }),
            })
            .await;
        assert!(repaired.ok);

        let requeued =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(requeued.status, TaskStatus::Queued);
        assert_eq!(requeued.blocked_reason, None);

        service.reconcile_and_dispatch_once().expect("dispatch requeued fort task");
        wait_for_task_status(&service, task_id.as_str(), TaskStatus::Done);
    }

    #[tokio::test]
    async fn worker_repair_auth_does_not_requeue_tasks_for_broken_sessions() {
        let temp = tempdir().expect("tempdir");
        let codex_home = temp.path().join("home/.si/codex/profiles/america");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-broken-auth-requeue"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": codex_home.clone(),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = SessionId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["session"]["session_id"].as_str())
                .expect("session id")
                .to_owned(),
        )
        .expect("session id");
        let worker_id = WorkerId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["session"]["worker_id"].as_str())
                .expect("worker id")
                .to_owned(),
        )
        .expect("worker id");

        let mut persisted_session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        persisted_session.app_server_thread_id = None;
        service
            .store
            .write_session_projection_locked(&persisted_session)
            .expect("persist session without thread id");

        write_fort_session_state(
            &codex_home,
            "america",
            PersistedSessionState {
                agent_id: "si-codex-america".to_owned(),
                session_id: "fort-session".to_owned(),
                host: "https://fort.example.invalid".to_owned(),
                runtime_host: "https://fort-runtime.example.invalid".to_owned(),
                access_expires_at: (Utc::now() + ChronoDuration::hours(1)).to_rfc3339(),
                refresh_expires_at: (Utc::now() + ChronoDuration::hours(12)).to_rfc3339(),
                ..PersistedSessionState::default()
            },
        );

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-broken-auth-requeue"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Do not requeue broken-session task",
                    "instructions": "Use si fort status before continuing",
                    "profile": "america",
                    "session_id": session_id.as_str(),
                }),
            })
            .await;
        assert!(created.ok);
        let task_id = TaskId::new(
            created
                .result
                .as_ref()
                .and_then(|item| item["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        service.reconcile_and_dispatch_once().expect("dispatch broken-session task");

        let blocked =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(blocked.status, TaskStatus::Blocked);
        assert_eq!(blocked.blocked_reason, Some(BlockedReason::SessionBroken));

        let repaired = service
            .dispatch_request(GatewayRequest {
                id: json!("worker-broken-auth-requeue"),
                method: "worker.repair_auth".to_owned(),
                params: json!({ "worker_id": worker_id.as_str() }),
            })
            .await;
        assert!(repaired.ok);

        let still_blocked =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(still_blocked.status, TaskStatus::Blocked);
        assert_eq!(still_blocked.blocked_reason, Some(BlockedReason::SessionBroken));
        assert!(still_blocked.latest_run_id.is_none());
    }

    #[tokio::test]
    async fn worker_repair_auth_does_not_requeue_other_profile_assignment() {
        let temp = tempdir().expect("tempdir");
        let america_codex_home = temp.path().join("home/.si/codex/profiles/america");
        let britain_codex_home = temp.path().join("home/.si/codex/profiles/britain");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let america_session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-america-auth-scope"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": america_codex_home.clone(),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(america_session.ok);
        let america_worker_id = WorkerId::new(
            america_session
                .result
                .as_ref()
                .and_then(|item| item["session"]["worker_id"].as_str())
                .expect("worker id")
                .to_owned(),
        )
        .expect("worker id");

        let britain_session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-britain-auth-scope"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "britain",
                    "home_dir": temp.path().join("home"),
                    "codex_home": britain_codex_home.clone(),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(britain_session.ok);

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-britain-auth-scope"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Do not requeue other profile auth task",
                    "instructions": "Use si fort status before continuing",
                    "profile": "britain",
                }),
            })
            .await;
        assert!(created.ok);
        let britain_task_id = TaskId::new(
            created
                .result
                .as_ref()
                .and_then(|item| item["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        service.reconcile_and_dispatch_once().expect("dispatch britain auth-blocked task");

        let blocked = service
            .store
            .inspect_task(&britain_task_id)
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(blocked.status, TaskStatus::Blocked);
        assert_eq!(blocked.profile.as_ref().map(ProfileName::as_str), Some("britain"));
        assert_eq!(blocked.blocked_reason, Some(BlockedReason::AuthRequired));
        assert!(blocked.latest_run_id.is_none());

        write_fort_session_state(
            &america_codex_home,
            "america",
            PersistedSessionState {
                agent_id: "si-codex-america".to_owned(),
                session_id: "fort-session".to_owned(),
                host: "https://fort.example.invalid".to_owned(),
                runtime_host: "https://fort-runtime.example.invalid".to_owned(),
                access_expires_at: (Utc::now() + ChronoDuration::hours(1)).to_rfc3339(),
                refresh_expires_at: (Utc::now() + ChronoDuration::hours(12)).to_rfc3339(),
                ..PersistedSessionState::default()
            },
        );

        let repaired = service
            .dispatch_request(GatewayRequest {
                id: json!("worker-auth-scope"),
                method: "worker.repair_auth".to_owned(),
                params: json!({ "worker_id": america_worker_id.as_str() }),
            })
            .await;
        assert!(repaired.ok);
        service.reconcile_and_dispatch_once().expect("dispatch after repairing other profile");

        let still_blocked = service
            .store
            .inspect_task(&britain_task_id)
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(still_blocked.status, TaskStatus::Blocked);
        assert_eq!(still_blocked.profile.as_ref().map(ProfileName::as_str), Some("britain"));
        assert_eq!(still_blocked.blocked_reason, Some(BlockedReason::AuthRequired));
        assert!(still_blocked.latest_run_id.is_none());
    }

    #[tokio::test]
    async fn session_and_run_commands_persist_state() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-1"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = session
            .result
            .as_ref()
            .and_then(|item| item["session"]["session_id"].as_str())
            .expect("session id");

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-1"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Run a turn",
                    "instructions": "Drive one fake runtime turn",
                    "profile": "america",
                    "session_id": session_id,
                }),
            })
            .await;
        assert!(task.ok);
        let task_id =
            task.result.as_ref().and_then(|item| item["task_id"].as_str()).expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-1"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id,
                    "task_id": task_id,
                    "prompt": "Reply with nucleus-smoke",
                }),
            })
            .await;
        assert!(run.ok);
        let run_id = run.result.as_ref().and_then(|item| item["run_id"].as_str()).expect("run id");

        wait_for_task_status(&service, task_id, TaskStatus::Done);

        let inspected_run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-inspect"),
                method: "run.inspect".to_owned(),
                params: json!({ "run_id": run_id }),
            })
            .await;
        assert!(inspected_run.ok);
        assert_eq!(inspected_run.result.expect("run")["status"], json!("completed"));

        let inspected_task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-inspect"),
                method: "task.inspect".to_owned(),
                params: json!({ "task_id": task_id }),
            })
            .await;
        assert!(inspected_task.ok);
        let task = inspected_task.result.expect("task");
        assert_eq!(task["status"], json!("done"));
        assert_eq!(task["checkpoint_summary"], json!("nucleus-smoke"));

        let session_id = SessionId::new(session_id).expect("session id");
        let run_id = RunId::new(run_id).expect("run id");
        let session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        assert_eq!(session.summary_state.as_deref(), Some("nucleus-smoke"));

        let session_summary =
            fs::read_to_string(service.store.paths().session_summary_path(&session_id))
                .expect("read session summary");
        assert!(session_summary.contains("# Session"));
        assert!(session_summary.contains("Summary: `nucleus-smoke`"));

        let run_summary = fs::read_to_string(service.store.paths().run_summary_path(&run_id))
            .expect("read run summary");
        assert!(run_summary.contains("# Run"));
        assert!(run_summary.contains("Task status: `done`"));
        assert!(run_summary.contains("Checkpoint: `nucleus-smoke`"));
    }

    #[tokio::test]
    async fn restart_reloads_persisted_task_worker_session_run_and_event_state() {
        let temp = tempdir().expect("tempdir");
        let config = NucleusConfig {
            bind_addr: "127.0.0.1:9898".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: None,
        };

        let (worker_id, session_id, task_id, run_id) = {
            let service =
                NucleusService::open_with_runtime(config.clone(), Arc::new(FakeRuntime::default()))
                    .expect("service");

            let session = service
                .dispatch_request(GatewayRequest {
                    id: json!("session-restart-reload"),
                    method: "session.create".to_owned(),
                    params: json!({
                        "profile": "america",
                        "home_dir": temp.path().join("home"),
                        "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                        "workdir": temp.path(),
                    }),
                })
                .await;
            assert!(session.ok);
            let worker_id = WorkerId::new(
                session
                    .result
                    .as_ref()
                    .and_then(|item| item["worker"]["worker_id"].as_str())
                    .expect("worker id")
                    .to_owned(),
            )
            .expect("worker id");
            let session_id = SessionId::new(
                session
                    .result
                    .as_ref()
                    .and_then(|item| item["session"]["session_id"].as_str())
                    .expect("session id")
                    .to_owned(),
            )
            .expect("session id");

            let task = service
                .dispatch_request(GatewayRequest {
                    id: json!("task-restart-reload"),
                    method: "task.create".to_owned(),
                    params: json!({
                        "title": "Restart reload task",
                        "instructions": "Reply with nucleus-smoke",
                        "profile": "america",
                        "session_id": session_id.as_str(),
                    }),
                })
                .await;
            assert!(task.ok);
            let task_id = TaskId::new(
                task.result
                    .as_ref()
                    .and_then(|item| item["task_id"].as_str())
                    .expect("task id")
                    .to_owned(),
            )
            .expect("task id");

            let run = service
                .dispatch_request(GatewayRequest {
                    id: json!("run-restart-reload"),
                    method: "run.submit_turn".to_owned(),
                    params: json!({
                        "session_id": session_id.as_str(),
                        "task_id": task_id.as_str(),
                        "prompt": "Reply with nucleus-smoke",
                    }),
                })
                .await;
            assert!(run.ok);
            let run_id = RunId::new(
                run.result
                    .as_ref()
                    .and_then(|item| item["run_id"].as_str())
                    .expect("run id")
                    .to_owned(),
            )
            .expect("run id");

            wait_for_task_status(&service, task_id.as_str(), TaskStatus::Done);
            (worker_id, session_id, task_id, run_id)
        };

        let reopened = NucleusService::open(config).expect("reopen service");
        let status = reopened.status().expect("status");
        assert_eq!(status.worker_count, 1);
        assert_eq!(status.session_count, 1);
        assert_eq!(status.task_count, 1);
        assert_eq!(status.run_count, 1);
        assert!(status.next_event_seq > 1);

        let worker = reopened
            .store
            .inspect_worker(&worker_id)
            .expect("inspect worker")
            .expect("worker exists");
        let session = reopened
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        let task =
            reopened.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        let run = reopened.store.inspect_run(&run_id).expect("inspect run").expect("run exists");
        assert_eq!(worker.status, WorkerStatus::Ready);
        assert_eq!(session.lifecycle_state, SessionLifecycleState::Ready);
        assert_eq!(session.summary_state.as_deref(), Some("nucleus-smoke"));
        assert_eq!(task.status, TaskStatus::Done);
        assert_eq!(task.checkpoint_summary.as_deref(), Some("nucleus-smoke"));
        assert_eq!(run.status, RunStatus::Completed);

        let events =
            load_canonical_events(&reopened.store.paths().events_path).expect("load events");
        assert!(events.iter().any(|event| {
            event.event_type == CanonicalEventType::RunCompleted
                && event.data.run_id.as_ref() == Some(&run_id)
        }));
    }

    #[tokio::test]
    async fn startup_rebuilds_markdown_projections_from_canonical_state() {
        let temp = tempdir().expect("tempdir");
        let state_dir = temp.path().join("nucleus");

        let (worker_id, session_id, run_id) = {
            let service = NucleusService::open_with_runtime(
                NucleusConfig {
                    bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                    state_dir: state_dir.clone(),
                    auth_token: None,
                },
                Arc::new(FakeRuntime::default()),
            )
            .expect("service");

            let session = service
                .dispatch_request(GatewayRequest {
                    id: json!("session-rebuild"),
                    method: "session.create".to_owned(),
                    params: json!({
                        "profile": "america",
                        "home_dir": temp.path().join("home"),
                        "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                        "workdir": temp.path(),
                    }),
                })
                .await;
            assert!(session.ok);
            let worker_id = session
                .result
                .as_ref()
                .and_then(|item| item["worker"]["worker_id"].as_str())
                .expect("worker id")
                .to_owned();
            let session_id = session
                .result
                .as_ref()
                .and_then(|item| item["session"]["session_id"].as_str())
                .expect("session id")
                .to_owned();

            let task = service
                .dispatch_request(GatewayRequest {
                    id: json!("task-rebuild"),
                    method: "task.create".to_owned(),
                    params: json!({
                        "title": "Rebuild summaries",
                        "instructions": "Reply with nucleus-smoke",
                        "profile": "america",
                        "session_id": session_id,
                    }),
                })
                .await;
            assert!(task.ok);
            let task_id =
                task.result.as_ref().and_then(|item| item["task_id"].as_str()).expect("task id");

            let run = service
                .dispatch_request(GatewayRequest {
                    id: json!("run-rebuild"),
                    method: "run.submit_turn".to_owned(),
                    params: json!({
                        "session_id": session_id,
                        "task_id": task_id,
                        "prompt": "Reply with nucleus-smoke",
                    }),
                })
                .await;
            assert!(run.ok);
            let run_id = run
                .result
                .as_ref()
                .and_then(|item| item["run_id"].as_str())
                .expect("run id")
                .to_owned();

            thread::sleep(Duration::from_millis(150));
            (worker_id, session_id, run_id)
        };

        let paths = NucleusPaths::new(state_dir.clone());
        let worker_id = WorkerId::new(worker_id).expect("worker id");
        let session_id = SessionId::new(session_id).expect("session id");
        let run_id = RunId::new(run_id).expect("run id");

        fs::remove_file(paths.worker_summary_path(&worker_id)).expect("remove worker summary");
        fs::remove_file(paths.session_summary_path(&session_id)).expect("remove session summary");
        fs::remove_file(paths.run_summary_path(&run_id)).expect("remove run summary");

        let reopened = NucleusStore::open(state_dir).expect("reopen store");
        let worker_summary = fs::read_to_string(reopened.paths().worker_summary_path(&worker_id))
            .expect("read rebuilt worker summary");
        let session_summary =
            fs::read_to_string(reopened.paths().session_summary_path(&session_id))
                .expect("read rebuilt session summary");
        let run_summary = fs::read_to_string(reopened.paths().run_summary_path(&run_id))
            .expect("read rebuilt run summary");

        assert!(worker_summary.contains("Status: `ready`"));
        assert!(session_summary.contains("Summary: `nucleus-smoke`"));
        assert!(run_summary.contains("Checkpoint: `nucleus-smoke`"));
    }

    #[tokio::test]
    async fn startup_rewrites_diverged_markdown_summaries_from_canonical_state() {
        let temp = tempdir().expect("tempdir");
        let state_dir = temp.path().join("nucleus");

        let (worker_id, session_id, run_id) = {
            let service = NucleusService::open_with_runtime(
                NucleusConfig {
                    bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                    state_dir: state_dir.clone(),
                    auth_token: None,
                },
                Arc::new(FakeRuntime::default()),
            )
            .expect("service");

            let session = service
                .dispatch_request(GatewayRequest {
                    id: json!("session-diverged-summary"),
                    method: "session.create".to_owned(),
                    params: json!({
                        "profile": "america",
                        "home_dir": temp.path().join("home"),
                        "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                        "workdir": temp.path(),
                    }),
                })
                .await;
            assert!(session.ok);
            let worker_id = session
                .result
                .as_ref()
                .and_then(|item| item["worker"]["worker_id"].as_str())
                .expect("worker id")
                .to_owned();
            let session_id = session
                .result
                .as_ref()
                .and_then(|item| item["session"]["session_id"].as_str())
                .expect("session id")
                .to_owned();

            let task = service
                .dispatch_request(GatewayRequest {
                    id: json!("task-diverged-summary"),
                    method: "task.create".to_owned(),
                    params: json!({
                        "title": "Diverged summaries",
                        "instructions": "Reply with nucleus-smoke",
                        "profile": "america",
                        "session_id": session_id,
                    }),
                })
                .await;
            assert!(task.ok);
            let task_id =
                task.result.as_ref().and_then(|item| item["task_id"].as_str()).expect("task id");

            let run = service
                .dispatch_request(GatewayRequest {
                    id: json!("run-diverged-summary"),
                    method: "run.submit_turn".to_owned(),
                    params: json!({
                        "session_id": session_id,
                        "task_id": task_id,
                        "prompt": "Reply with nucleus-smoke",
                    }),
                })
                .await;
            assert!(run.ok);
            let run_id = run
                .result
                .as_ref()
                .and_then(|item| item["run_id"].as_str())
                .expect("run id")
                .to_owned();

            thread::sleep(Duration::from_millis(150));
            (worker_id, session_id, run_id)
        };

        let paths = NucleusPaths::new(state_dir.clone());
        let worker_id = WorkerId::new(worker_id).expect("worker id");
        let session_id = SessionId::new(session_id).expect("session id");
        let run_id = RunId::new(run_id).expect("run id");

        fs::write(paths.worker_summary_path(&worker_id), "Status: `failed`\n")
            .expect("write divergent worker summary");
        fs::write(paths.session_summary_path(&session_id), "Summary: `stale`\n")
            .expect("write divergent session summary");
        fs::write(paths.run_summary_path(&run_id), "Checkpoint: `stale`\n")
            .expect("write divergent run summary");

        let reopened = NucleusStore::open(state_dir).expect("reopen store");
        let worker_summary = fs::read_to_string(reopened.paths().worker_summary_path(&worker_id))
            .expect("read rebuilt worker summary");
        let session_summary =
            fs::read_to_string(reopened.paths().session_summary_path(&session_id))
                .expect("read rebuilt session summary");
        let run_summary = fs::read_to_string(reopened.paths().run_summary_path(&run_id))
            .expect("read rebuilt run summary");

        assert!(worker_summary.contains("Status: `ready`"));
        assert!(!worker_summary.contains("Status: `failed`"));
        assert!(session_summary.contains("Summary: `nucleus-smoke`"));
        assert!(!session_summary.contains("Summary: `stale`"));
        assert!(run_summary.contains("Checkpoint: `nucleus-smoke`"));
        assert!(!run_summary.contains("Checkpoint: `stale`"));

        let persisted_run =
            reopened.inspect_run(&run_id).expect("inspect run").expect("run exists");
        assert_eq!(persisted_run.status, RunStatus::Completed);
    }

    #[tokio::test]
    async fn persisted_run_events_use_canonical_si_event_names() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-canonical-events"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = session
            .result
            .as_ref()
            .and_then(|item| item["session"]["session_id"].as_str())
            .expect("session id");

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-canonical-events"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Canonical event names",
                    "instructions": "Emit canonical run events",
                    "profile": "america",
                    "session_id": session_id,
                }),
            })
            .await;
        assert!(task.ok);
        let task_id =
            task.result.as_ref().and_then(|item| item["task_id"].as_str()).expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-canonical-events"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id,
                    "task_id": task_id,
                    "prompt": "emit canonical event names",
                }),
            })
            .await;
        assert!(run.ok);

        wait_for_task_status(&service, task_id, TaskStatus::Done);

        let names = load_canonical_events(&service.store.paths().events_path)
            .expect("load events")
            .into_iter()
            .map(|event| event.event_type.as_str().to_owned())
            .collect::<Vec<_>>();
        assert!(names.contains(&"run.started".to_owned()));
        assert!(names.contains(&"run.output_delta".to_owned()));
        assert!(names.contains(&"run.completed".to_owned()));
        for forbidden in ["item/agentMessage/delta", "turn/start", "turn/interrupt"] {
            assert!(
                !names.iter().any(|name| name == forbidden),
                "canonical event ledger must not expose {forbidden}"
            );
        }
    }

    #[tokio::test]
    async fn persisted_run_events_use_single_canonical_event_ledger() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-single-ledger"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = session
            .result
            .as_ref()
            .and_then(|item| item["session"]["session_id"].as_str())
            .expect("session id");

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-single-ledger"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Single ledger task",
                    "instructions": "Verify one canonical event stream",
                    "profile": "america",
                    "session_id": session_id,
                }),
            })
            .await;
        assert!(task.ok);
        let task_id =
            task.result.as_ref().and_then(|item| item["task_id"].as_str()).expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-single-ledger"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id,
                    "task_id": task_id,
                    "prompt": "verify a single canonical event ledger",
                }),
            })
            .await;
        assert!(run.ok);
        wait_for_task_status(&service, task_id, TaskStatus::Done);

        let event_files = fs::read_dir(&service.store.paths().events_state_dir)
            .expect("read events dir")
            .filter_map(|entry| entry.ok())
            .map(|entry| entry.path())
            .filter(|path| path.extension().and_then(|value| value.to_str()) == Some("jsonl"))
            .collect::<Vec<_>>();
        assert_eq!(event_files, vec![service.store.paths().events_path.clone()]);

        let log_entries = fs::read_dir(&service.store.paths().logs_dir)
            .expect("read logs dir")
            .filter_map(|entry| entry.ok())
            .collect::<Vec<_>>();
        assert!(log_entries.is_empty(), "logs dir should not hold a parallel event ledger");
    }

    #[tokio::test]
    async fn si_primary_ids_remain_namespaced_and_distinct_from_runtime_ids() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-id-boundary"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let worker_id = session.result.as_ref().expect("session result")["worker"]["worker_id"]
            .as_str()
            .expect("worker id")
            .to_owned();
        let session_id = session.result.as_ref().expect("session result")["session"]["session_id"]
            .as_str()
            .expect("session id")
            .to_owned();
        let thread_id =
            session.result.as_ref().expect("session result")["session"]["app_server_thread_id"]
                .as_str()
                .expect("thread id")
                .to_owned();
        assert!(worker_id.starts_with("si-worker-"));
        assert!(session_id.starts_with("si-session-"));
        assert_ne!(session_id, thread_id);
        assert!(!thread_id.starts_with("si-session-"));

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-id-boundary"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "ID boundary task",
                    "instructions": "Verify SI ids stay distinct",
                    "profile": "america",
                    "session_id": session_id,
                }),
            })
            .await;
        assert!(task.ok);
        let task_id =
            task.result.as_ref().and_then(|item| item["task_id"].as_str()).expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-id-boundary"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id,
                    "task_id": task_id,
                    "prompt": "verify id boundary",
                }),
            })
            .await;
        assert!(run.ok);
        let run_id = run.result.as_ref().and_then(|item| item["run_id"].as_str()).expect("run id");
        wait_for_task_status(&service, task_id, TaskStatus::Done);

        let persisted_run = service
            .store
            .inspect_run(&RunId::new(run_id).expect("run id"))
            .expect("inspect run")
            .expect("run exists");
        let turn_id = persisted_run.app_server_turn_id.as_deref().expect("app server turn id");
        assert!(run_id.starts_with("si-run-"));
        assert_ne!(run_id, turn_id);
        assert!(!turn_id.starts_with("si-run-"));

        let loaded = load_canonical_events_for_live_iteration(&service.store.paths().events_path)
            .expect("load events");
        assert!(!loaded.events.is_empty());
        assert!(
            loaded.events.iter().all(|event| event.event_id.as_str().starts_with("si-event-")),
            "canonical event ids must keep the si-event namespace"
        );
    }

    #[test]
    fn live_iteration_quarantines_malformed_canonical_event_logs() {
        let temp = tempdir().expect("tempdir");
        let state_dir = temp.path().join("nucleus");
        let store = NucleusStore::open(state_dir).expect("open store");
        store
            .append_system_warning("seed event", json!({ "kind": "seed" }))
            .expect("append seed event");
        let rotated_path = store
            .paths()
            .events_path
            .parent()
            .expect("events parent")
            .join("events-00000000000000000001-test.jsonl");
        fs::write(&rotated_path, b"{\"seq\":").expect("write malformed rotated events ledger");

        let loaded = load_canonical_events_for_live_iteration(&store.paths().events_path)
            .expect("load live events");
        assert_eq!(loaded.warnings.len(), 1);
        assert_eq!(loaded.warnings[0]["kind"], json!("canonical_event_log"));
        assert_eq!(loaded.warnings[0]["path"], json!(rotated_path.display().to_string()),);
        let quarantine_path =
            loaded.warnings[0]["quarantine_path"].as_str().expect("quarantine path");
        assert!(Path::new(quarantine_path).exists());
        assert_eq!(loaded.events.len(), 1);
        assert_eq!(loaded.events[0].event_type, CanonicalEventType::SystemWarning);
    }

    #[tokio::test]
    async fn task_prune_removes_only_old_terminal_tasks() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: None,
        })
        .expect("service");

        let old_done_id = TaskId::generate();
        let old_cancelled_id = TaskId::generate();
        let old_queued_id = TaskId::generate();
        let recent_done_id = TaskId::generate();

        let mut old_done =
            TaskRecord::new(old_done_id.clone(), TaskSource::System, "old done", "done task");
        old_done.transition_to(TaskStatus::Running, None).expect("run old done");
        old_done.transition_to(TaskStatus::Done, None).expect("finish old done");
        old_done.updated_at = Utc::now() - ChronoDuration::days(45);
        write_json_atomic(&service.store.paths().task_path(&old_done_id), &old_done)
            .expect("write old done");

        let mut old_cancelled = TaskRecord::new(
            old_cancelled_id.clone(),
            TaskSource::System,
            "old cancelled",
            "cancelled task",
        );
        old_cancelled.transition_to(TaskStatus::Cancelled, None).expect("cancel old task");
        old_cancelled.updated_at = Utc::now() - ChronoDuration::days(45);
        write_json_atomic(&service.store.paths().task_path(&old_cancelled_id), &old_cancelled)
            .expect("write old cancelled");

        let mut old_queued =
            TaskRecord::new(old_queued_id.clone(), TaskSource::System, "old queued", "queued task");
        old_queued.updated_at = Utc::now() - ChronoDuration::days(45);
        write_json_atomic(&service.store.paths().task_path(&old_queued_id), &old_queued)
            .expect("write old queued");

        let mut recent_done = TaskRecord::new(
            recent_done_id.clone(),
            TaskSource::System,
            "recent done",
            "recent task",
        );
        recent_done.transition_to(TaskStatus::Running, None).expect("run recent done");
        recent_done.transition_to(TaskStatus::Done, None).expect("finish recent done");
        write_json_atomic(&service.store.paths().task_path(&recent_done_id), &recent_done)
            .expect("write recent done");

        let response = service
            .dispatch_request(GatewayRequest {
                id: json!("task-prune"),
                method: "task.prune".to_owned(),
                params: json!({ "older_than_days": 30 }),
            })
            .await;
        assert!(response.ok);
        let payload = response.result.expect("prune payload");
        assert_eq!(
            payload["pruned_task_ids"],
            json!([old_done_id.as_str(), old_cancelled_id.as_str()])
        );

        assert!(!service.store.paths().task_path(&old_done_id).exists());
        assert!(!service.store.paths().task_path(&old_cancelled_id).exists());
        assert!(service.store.paths().task_path(&old_queued_id).exists());
        assert!(service.store.paths().task_path(&recent_done_id).exists());
    }

    #[tokio::test]
    async fn dispatcher_selects_and_executes_queued_tasks_from_durable_task_state() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-queued"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Dispatch queued task",
                    "instructions": "Drive the dispatcher path",
                    "profile": "america",
                }),
            })
            .await;
        assert!(created.ok);
        let task_id =
            created.result.as_ref().and_then(|task| task["task_id"].as_str()).expect("task id");

        service.reconcile_and_dispatch_once().expect("dispatch queued work");
        wait_for_task_status(&service, task_id, TaskStatus::Done);

        let task = service
            .store
            .inspect_task(&TaskId::new(task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(task.status, TaskStatus::Done);
        assert!(task.session_id.is_some());
        assert!(task.latest_run_id.is_some());
        assert_eq!(service.status().expect("status").worker_count, 1);
        assert_eq!(service.status().expect("status").session_count, 1);
        assert_eq!(service.status().expect("status").run_count, 1);
    }

    #[tokio::test]
    async fn session_create_reuses_single_worker_and_codex_home_per_profile() {
        let temp = tempdir().expect("tempdir");
        let runtime = FakeRuntime::default();
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime.clone()),
        )
        .expect("service");
        let first_codex_home = temp.path().join("home/.si/codex/profiles/america");
        let second_codex_home = temp.path().join("other/.si/codex/profiles/america-shadow");

        let first = service
            .dispatch_request(GatewayRequest {
                id: json!("session-first-worker"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": first_codex_home,
                    "workdir": temp.path().join("work-a"),
                }),
            })
            .await;
        assert!(first.ok);

        let second = service
            .dispatch_request(GatewayRequest {
                id: json!("session-second-worker"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("other"),
                    "codex_home": second_codex_home,
                    "workdir": temp.path().join("work-b"),
                }),
            })
            .await;
        assert!(second.ok);

        assert_eq!(runtime.start_call_count(), 1);
        assert_eq!(
            first.result.as_ref().expect("first result")["worker"]["worker_id"],
            second.result.as_ref().expect("second result")["worker"]["worker_id"]
        );
        assert_eq!(
            second.result.as_ref().expect("second result")["worker"]["codex_home"],
            json!(first_codex_home.display().to_string())
        );
        assert_eq!(service.store.list_workers().expect("workers").len(), 1);
        assert_eq!(service.store.list_sessions().expect("sessions").len(), 2);
    }

    #[tokio::test]
    async fn session_create_does_not_reuse_session_with_conflicting_active_run() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::with_run_delay(Duration::from_millis(150))),
        )
        .expect("service");

        let first_session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-first-active"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(first_session.ok);
        let first_session_id = first_session
            .result
            .as_ref()
            .and_then(|item| item["session"]["session_id"].as_str())
            .expect("first session id")
            .to_owned();
        let worker_id = first_session
            .result
            .as_ref()
            .and_then(|item| item["worker"]["worker_id"].as_str())
            .expect("worker id")
            .to_owned();

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-active-session"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Conflicting active run",
                    "instructions": "Keep the first session busy",
                    "profile": "america",
                    "session_id": first_session_id,
                }),
            })
            .await;
        assert!(task.ok);
        let task_id =
            task.result.as_ref().and_then(|item| item["task_id"].as_str()).expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-active-session"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": first_session_id,
                    "task_id": task_id,
                    "prompt": "hold this run briefly open",
                }),
            })
            .await;
        assert!(run.ok);

        thread::sleep(Duration::from_millis(40));

        let second_session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-second-active"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(second_session.ok);
        let second_session_id = second_session
            .result
            .as_ref()
            .and_then(|item| item["session"]["session_id"].as_str())
            .expect("second session id")
            .to_owned();
        let second_worker_id = second_session
            .result
            .as_ref()
            .and_then(|item| item["worker"]["worker_id"].as_str())
            .expect("second worker id")
            .to_owned();

        assert_ne!(second_session_id, first_session_id);
        assert_eq!(second_worker_id, worker_id);

        wait_for_task_status(&service, task_id, TaskStatus::Done);
    }

    #[tokio::test]
    async fn explicit_profile_tasks_do_not_fall_back_when_requested_profile_is_busy() {
        let temp = tempdir().expect("tempdir");
        let runtime = FakeRuntime::with_run_delay(Duration::from_millis(250));
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime.clone()),
        )
        .expect("service");

        for profile_name in ["america", "berylla"] {
            let profile = ProfileName::new(profile_name).expect("profile");
            let home_dir = temp.path().join(format!("home-{profile_name}"));
            let spec = WorkerLaunchSpec {
                worker_id: WorkerId::generate(),
                profile: profile.clone(),
                home_dir: home_dir.clone(),
                codex_home: home_dir.join(format!(".si/codex/profiles/{profile_name}")),
                workdir: temp.path().to_path_buf(),
                extra_env: BTreeMap::new(),
            };
            let started = runtime.start_worker(&spec).expect("start worker");
            service.store.record_worker_start(&spec, &started, &runtime).expect("record worker");
        }

        let first = service
            .dispatch_request(GatewayRequest {
                id: json!("task-explicit-busy-first"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "First pinned task",
                    "instructions": "Hold america busy",
                    "profile": "america",
                }),
            })
            .await;
        assert!(first.ok);
        let first_task_id = first.result.as_ref().expect("first result")["task_id"]
            .as_str()
            .expect("first task id")
            .to_owned();

        let second = service
            .dispatch_request(GatewayRequest {
                id: json!("task-explicit-busy-second"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Second pinned task",
                    "instructions": "Wait for america instead of falling back",
                    "profile": "america",
                }),
            })
            .await;
        assert!(second.ok);
        let second_task_id = second.result.as_ref().expect("second result")["task_id"]
            .as_str()
            .expect("second task id")
            .to_owned();

        service.reconcile_and_dispatch_once().expect("dispatch first wave");
        thread::sleep(Duration::from_millis(40));

        let second_task = service
            .store
            .inspect_task(&TaskId::new(second_task_id.clone()).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(second_task.status, TaskStatus::Queued);
        assert_eq!(second_task.profile.as_ref().map(ProfileName::as_str), Some("america"));
        assert!(second_task.latest_run_id.is_none());

        wait_for_task_status(&service, &first_task_id, TaskStatus::Done);
        service.reconcile_and_dispatch_once().expect("dispatch second wave");
        wait_for_task_status(&service, &second_task_id, TaskStatus::Done);

        let second_task = service
            .store
            .inspect_task(&TaskId::new(second_task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(second_task.profile.as_ref().map(ProfileName::as_str), Some("america"));
    }

    #[tokio::test]
    async fn session_create_prefers_stable_lexical_worker_id_when_candidates_tie() {
        let temp = tempdir().expect("tempdir");
        let runtime = FakeRuntime::default();
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime.clone()),
        )
        .expect("service");
        let profile = ProfileName::new("america".to_owned()).expect("profile");
        let base_home = temp.path().join("home");

        for worker_suffix in ["b", "a"] {
            let worker_id = WorkerId::new(format!("si-worker-{worker_suffix}")).expect("worker id");
            let spec = WorkerLaunchSpec {
                worker_id,
                profile: profile.clone(),
                home_dir: base_home.clone(),
                codex_home: base_home.join(format!(".si/codex/profiles/america-{worker_suffix}")),
                workdir: temp.path().to_path_buf(),
                extra_env: BTreeMap::new(),
            };
            let started = runtime.start_worker(&spec).expect("start worker");
            let _ = service
                .store
                .record_worker_start(&spec, &started, &runtime)
                .expect("record worker");
        }

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-lexical-worker"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": base_home,
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        assert_eq!(runtime.start_call_count(), 2);
        assert_eq!(
            session.result.as_ref().expect("session result")["worker"]["worker_id"],
            json!("si-worker-a")
        );
    }

    #[tokio::test]
    async fn dispatcher_assigns_unprofiled_task_to_first_ready_profile() {
        let temp = tempdir().expect("tempdir");
        let runtime = FakeRuntime::default();
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime),
        )
        .expect("service");

        for profile in ["zulu", "america"] {
            let session = service
                .dispatch_request(GatewayRequest {
                    id: json!(format!("session-{profile}")),
                    method: "session.create".to_owned(),
                    params: json!({
                        "profile": profile,
                        "home_dir": temp.path().join("home"),
                        "codex_home": temp.path().join(format!("home/.si/codex/profiles/{profile}")),
                        "workdir": temp.path(),
                    }),
                })
                .await;
            assert!(session.ok, "create session for {profile}");
        }

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-auto-profile"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Auto profile task",
                    "instructions": "Use the first ready profile deterministically",
                }),
            })
            .await;
        assert!(created.ok);
        let task_id =
            created.result.as_ref().and_then(|task| task["task_id"].as_str()).expect("task id");

        service.reconcile_and_dispatch_once().expect("dispatch queued work");
        wait_for_task_status(&service, task_id, TaskStatus::Done);

        let task = service
            .store
            .inspect_task(&TaskId::new(task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(task.profile.as_ref().map(ProfileName::as_str), Some("america"));
        assert!(task.latest_run_id.is_some());
    }

    #[tokio::test]
    async fn dispatcher_blocks_explicit_profile_when_worker_is_unavailable() {
        let temp = tempdir().expect("tempdir");
        let runtime = FakeRuntime::default();
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime.clone()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-america-fallback"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        runtime.fail_next_starts(1);

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-fallback-profile"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Explicit profile task",
                    "instructions": "Stay on the requested profile when its worker cannot start",
                    "profile": "zulu",
                }),
            })
            .await;
        assert!(created.ok);
        let task_id =
            created.result.as_ref().and_then(|task| task["task_id"].as_str()).expect("task id");

        service.reconcile_and_dispatch_once().expect("dispatch queued work");
        wait_for_task_status(&service, task_id, TaskStatus::Blocked);

        let task = service
            .store
            .inspect_task(&TaskId::new(task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(task.profile.as_ref().map(ProfileName::as_str), Some("zulu"));
        assert_eq!(task.blocked_reason, Some(BlockedReason::ProfileUnavailable));
        assert!(task.latest_run_id.is_none());
    }

    #[tokio::test]
    async fn dispatcher_blocks_task_when_profile_worker_is_not_ready() {
        let temp = tempdir().expect("tempdir");
        let runtime = FakeRuntime::default();
        runtime.fail_next_starts(1);
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime),
        )
        .expect("service");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-blocked-profile"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Blocked profile task",
                    "instructions": "Run when the profile is ready",
                    "profile": "america",
                }),
            })
            .await;
        assert!(created.ok);
        let task_id =
            created.result.as_ref().and_then(|task| task["task_id"].as_str()).expect("task id");

        service.reconcile_and_dispatch_once().expect("dispatch queued work");

        let task = service
            .store
            .inspect_task(&TaskId::new(task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(task.status, TaskStatus::Blocked);
        assert_eq!(task.blocked_reason, Some(BlockedReason::WorkerUnavailable));
        assert!(task.latest_run_id.is_none());
    }

    #[test]
    fn task_profile_candidates_stay_pinned_to_explicit_profile() {
        let task = TaskRecord {
            profile: Some(ProfileName::new("america").expect("profile")),
            ..TaskRecord::new(
                TaskId::generate(),
                TaskSource::Websocket,
                "Pinned profile".to_owned(),
                "Pinned profile".to_owned(),
            )
        };
        let candidates = super::task_profile_candidates(
            &task,
            &HashMap::new(),
            &HashMap::new(),
            &[],
            &[
                ProfileName::new("america").expect("profile"),
                ProfileName::new("berylla").expect("profile"),
                ProfileName::new("cadma").expect("profile"),
            ],
        );
        assert_eq!(candidates, vec![ProfileName::new("america").expect("profile")]);
    }

    #[test]
    fn task_profile_candidates_include_discovered_local_profiles() {
        let task = TaskRecord::new(TaskId::generate(), TaskSource::System, "local", "profile");
        let mut local_profiles = vec![
            ProfileName::new("darmstada").expect("profile"),
            ProfileName::new("berylla").expect("profile"),
        ];
        local_profiles.sort();

        let candidates = super::task_profile_candidates(
            &task,
            &HashMap::new(),
            &HashMap::new(),
            &[],
            &local_profiles,
        );

        assert_eq!(
            candidates.iter().map(ProfileName::as_str).collect::<Vec<_>>(),
            vec!["berylla", "darmstada"]
        );
    }

    #[test]
    fn task_profile_candidates_are_empty_when_nothing_is_resolvable() {
        let task = TaskRecord::new(TaskId::generate(), TaskSource::System, "empty", "profiles");

        let candidates =
            super::task_profile_candidates(&task, &HashMap::new(), &HashMap::new(), &[], &[]);

        assert!(candidates.is_empty());
    }

    #[tokio::test]
    async fn dispatcher_requeues_profile_unavailable_task_when_explicit_profile_becomes_resolvable()
    {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-profile-later"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Routable later",
                    "instructions": "Run after one profile is known",
                    "profile": "zulu",
                }),
            })
            .await;
        assert!(created.ok);
        let task_id =
            created.result.as_ref().and_then(|task| task["task_id"].as_str()).expect("task id");

        service.reconcile_and_dispatch_once().expect("block unresolved task");
        let blocked = service
            .store
            .inspect_task(&TaskId::new(task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(blocked.status, TaskStatus::Blocked);
        assert_eq!(blocked.blocked_reason, Some(BlockedReason::ProfileUnavailable));

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-profile-later"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "zulu",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/zulu"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);

        service.reconcile_and_dispatch_once().expect("requeue and dispatch resolved task");
        wait_for_task_status(&service, task_id, TaskStatus::Done);

        let task = service
            .store
            .inspect_task(&TaskId::new(task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(task.status, TaskStatus::Done);
        assert_eq!(task.profile.as_ref().map(ProfileName::as_str), Some("zulu"));
    }

    #[tokio::test]
    async fn dispatcher_blocks_unprofiled_task_when_referenced_session_is_missing() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-no-profile-missing-session"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Missing session without profile",
                    "instructions": "Do not wait on profile inference before surfacing session loss",
                    "session_id": "si-session-missing",
                }),
            })
            .await;
        assert!(created.ok);
        assert_eq!(
            created.result.as_ref().and_then(|task| task["status"].as_str()),
            Some("blocked")
        );
        let task_id =
            created.result.as_ref().and_then(|task| task["task_id"].as_str()).expect("task id");

        let task = service
            .store
            .inspect_task(&TaskId::new(task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(task.status, TaskStatus::Blocked);
        assert_eq!(task.blocked_reason, Some(BlockedReason::SessionBroken));
        assert!(task.latest_run_id.is_none());
    }

    #[tokio::test]
    async fn dispatcher_blocks_task_when_session_profile_mismatches_task_profile() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-america"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = session
            .result
            .as_ref()
            .and_then(|item| item["session"]["session_id"].as_str())
            .expect("session id");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-session-mismatch"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Mismatched task profile",
                    "instructions": "Attempt cross-profile session reuse",
                    "profile": "europe",
                    "session_id": session_id,
                }),
            })
            .await;
        assert!(created.ok);
        assert_eq!(
            created.result.as_ref().and_then(|task| task["status"].as_str()),
            Some("blocked")
        );
        let task_id =
            created.result.as_ref().and_then(|task| task["task_id"].as_str()).expect("task id");

        let task = service
            .store
            .inspect_task(&TaskId::new(task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(task.status, TaskStatus::Blocked);
        assert_eq!(task.blocked_reason, Some(BlockedReason::SessionBroken));
        assert!(task.latest_run_id.is_none());

        let session = service
            .store
            .inspect_session(&SessionId::new(session_id).expect("session id"))
            .expect("inspect session")
            .expect("session exists");
        assert_eq!(session.profile.as_ref().map(ProfileName::as_str), Some("america"));
    }

    #[tokio::test]
    async fn run_submit_turn_rejects_session_profile_mismatch() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-america-run"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = session
            .result
            .as_ref()
            .and_then(|item| item["session"]["session_id"].as_str())
            .expect("session id");

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-europe-run"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Direct run mismatch",
                    "instructions": "Attempt cross-profile run submission",
                    "profile": "europe",
                    "session_id": session_id,
                }),
            })
            .await;
        assert!(task.ok);
        let task_id =
            task.result.as_ref().and_then(|item| item["task_id"].as_str()).expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-mismatch"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id,
                    "task_id": task_id,
                    "prompt": "Submit a mismatched direct run",
                }),
            })
            .await;
        assert!(!run.ok);
        assert!(
            run.error
                .as_ref()
                .map(|error| error.message.contains("task profile does not match session profile"))
                .unwrap_or(false)
        );
        assert_eq!(service.store.list_runs().expect("runs").len(), 0);
    }

    #[tokio::test]
    async fn run_submit_turn_marks_session_broken_when_thread_id_is_missing() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-missing-run-thread"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = SessionId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["session"]["session_id"].as_str())
                .expect("session id")
                .to_owned(),
        )
        .expect("session id");

        let mut persisted_session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        persisted_session.app_server_thread_id = None;
        service
            .store
            .write_session_projection_locked(&persisted_session)
            .expect("persist session without thread id");

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-missing-run-thread"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Direct run missing thread",
                    "instructions": "Attempt direct run with no app server thread id",
                    "profile": "america",
                    "session_id": session_id.as_str(),
                }),
            })
            .await;
        assert!(task.ok);
        let task_id = TaskId::new(
            task.result
                .as_ref()
                .and_then(|item| item["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-missing-thread"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id.as_str(),
                    "task_id": task_id.as_str(),
                    "prompt": "attempt direct run without thread id",
                }),
            })
            .await;
        assert!(!run.ok);
        assert!(
            run.error
                .as_ref()
                .map(|error| {
                    error.message.contains("session missing app-server thread id")
                        || error.message.contains("task references a non-reusable session")
                })
                .unwrap_or(false)
        );
        assert_eq!(service.store.list_runs().expect("runs").len(), 0);

        let task =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(task.status, TaskStatus::Blocked);
        assert_eq!(task.blocked_reason, Some(BlockedReason::SessionBroken));

        let session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        assert_eq!(session.lifecycle_state, SessionLifecycleState::Broken);
    }

    #[tokio::test]
    async fn run_submit_turn_rejects_different_bound_session() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let first_session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-direct-bound-a"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(first_session.ok);
        let first_session_id = first_session
            .result
            .as_ref()
            .and_then(|item| item["session"]["session_id"].as_str())
            .expect("first session id");

        let second_session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-direct-bound-b"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home-b"),
                    "codex_home": temp.path().join("home-b/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(second_session.ok);
        let second_session_id = second_session
            .result
            .as_ref()
            .and_then(|item| item["session"]["session_id"].as_str())
            .expect("second session id");

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-direct-bound"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Direct run wrong session",
                    "instructions": "Attempt direct run with a different bound session",
                    "profile": "america",
                    "session_id": first_session_id,
                }),
            })
            .await;
        assert!(task.ok);
        let task_id = TaskId::new(
            task.result
                .as_ref()
                .and_then(|item| item["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-direct-bound-mismatch"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": second_session_id,
                    "task_id": task_id.as_str(),
                    "prompt": "attempt direct run with wrong session",
                }),
            })
            .await;
        assert!(!run.ok);
        assert!(
            run.error
                .as_ref()
                .map(|error| error.message.contains("task is bound to a different session"))
                .unwrap_or(false)
        );
        assert_eq!(service.store.list_runs().expect("runs").len(), 0);

        let task =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(task.status, TaskStatus::Queued);
        assert_eq!(task.session_id.as_ref().map(SessionId::as_str), Some(first_session_id));
    }

    #[tokio::test]
    async fn run_submit_turn_rejects_non_reusable_session_binding() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-broken-run"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = SessionId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["session"]["session_id"].as_str())
                .expect("session id")
                .to_owned(),
        )
        .expect("session id");
        service
            .store
            .mark_session_broken(&session_id, "marked broken for direct-run test")
            .expect("mark broken");

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-broken-run"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Direct run broken session",
                    "instructions": "Attempt direct run through a broken session",
                    "profile": "america",
                    "session_id": session_id.as_str(),
                }),
            })
            .await;
        assert!(task.ok);
        let task_id =
            task.result.as_ref().and_then(|item| item["task_id"].as_str()).expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-broken-session"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id.as_str(),
                    "task_id": task_id,
                    "prompt": "attempt direct run with broken session",
                }),
            })
            .await;
        assert!(!run.ok);
        assert!(
            run.error
                .as_ref()
                .map(|error| error.message.contains("task references a non-reusable session"))
                .unwrap_or(false)
        );
        assert_eq!(service.store.list_runs().expect("runs").len(), 0);
    }

    #[tokio::test]
    async fn dispatcher_blocks_task_when_referenced_session_is_missing() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-missing-session"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Missing session task",
                    "instructions": "Attempt to route through a missing session",
                    "profile": "america",
                    "session_id": "si-session-missing",
                }),
            })
            .await;
        assert!(created.ok);
        assert_eq!(
            created.result.as_ref().and_then(|task| task["status"].as_str()),
            Some("blocked")
        );
        let task_id =
            created.result.as_ref().and_then(|task| task["task_id"].as_str()).expect("task id");

        let task = service
            .store
            .inspect_task(&TaskId::new(task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(task.status, TaskStatus::Blocked);
        assert_eq!(task.blocked_reason, Some(BlockedReason::SessionBroken));
        assert!(task.latest_run_id.is_none());
    }

    #[tokio::test]
    async fn dispatcher_blocks_task_behind_non_reusable_session() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-broken"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = SessionId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["session"]["session_id"].as_str())
                .expect("session id")
                .to_owned(),
        )
        .expect("session id");
        service
            .store
            .mark_session_broken(&session_id, "marked broken for test")
            .expect("mark broken");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-broken-session"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Broken session task",
                    "instructions": "Attempt to route through a broken session",
                    "profile": "america",
                    "session_id": session_id.as_str(),
                }),
            })
            .await;
        assert!(created.ok);
        assert_eq!(
            created.result.as_ref().and_then(|task| task["status"].as_str()),
            Some("blocked")
        );
        let task_id =
            created.result.as_ref().and_then(|task| task["task_id"].as_str()).expect("task id");

        let task = service
            .store
            .inspect_task(&TaskId::new(task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(task.status, TaskStatus::Blocked);
        assert_eq!(task.blocked_reason, Some(BlockedReason::SessionBroken));
        assert!(task.latest_run_id.is_none());
    }

    #[tokio::test]
    async fn task_create_marks_session_broken_when_referenced_session_lacks_thread_id() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let created_session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-missing-thread"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(created_session.ok);
        let session_id = SessionId::new(
            created_session
                .result
                .as_ref()
                .and_then(|item| item["session"]["session_id"].as_str())
                .expect("session id")
                .to_owned(),
        )
        .expect("session id");

        let mut session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        session.app_server_thread_id = None;
        service
            .store
            .write_session_projection_locked(&session)
            .expect("persist session without thread id");

        let created_task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-missing-thread"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Missing thread id task",
                    "instructions": "Route through a session with no app server thread",
                    "profile": "america",
                    "session_id": session_id.as_str(),
                }),
            })
            .await;
        assert!(created_task.ok);
        assert_eq!(
            created_task.result.as_ref().and_then(|item| item["status"].as_str()),
            Some("blocked")
        );
        let task_id = TaskId::new(
            created_task
                .result
                .as_ref()
                .and_then(|item| item["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        let task =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(task.status, TaskStatus::Blocked);
        assert_eq!(task.blocked_reason, Some(BlockedReason::SessionBroken));
        assert!(task.latest_run_id.is_none());

        let session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        assert_eq!(session.lifecycle_state, SessionLifecycleState::Broken);
        assert!(session.app_server_thread_id.is_none());
    }

    #[tokio::test]
    async fn dispatcher_respects_session_affine_backlog_order() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::with_run_delay(Duration::from_millis(150))),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-queue"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = session
            .result
            .as_ref()
            .and_then(|item| item["session"]["session_id"].as_str())
            .expect("session id");

        let first = service
            .dispatch_request(GatewayRequest {
                id: json!("task-first"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "First queued task",
                    "instructions": "Run first",
                    "profile": "america",
                    "session_id": session_id,
                }),
            })
            .await;
        assert!(first.ok);
        let first_task_id =
            first.result.as_ref().and_then(|task| task["task_id"].as_str()).expect("first task id");

        let second = service
            .dispatch_request(GatewayRequest {
                id: json!("task-second"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Second queued task",
                    "instructions": "Run second",
                    "profile": "america",
                    "session_id": session_id,
                }),
            })
            .await;
        assert!(second.ok);
        let second_task_id = second
            .result
            .as_ref()
            .and_then(|task| task["task_id"].as_str())
            .expect("second task id");

        service.reconcile_and_dispatch_once().expect("first dispatch");
        thread::sleep(Duration::from_millis(40));
        let second_task = service
            .store
            .inspect_task(&TaskId::new(second_task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(second_task.status, TaskStatus::Queued);
        assert!(second_task.latest_run_id.is_none());

        service.reconcile_and_dispatch_once().expect("backlog still serialized");
        let second_task = service
            .store
            .inspect_task(&TaskId::new(second_task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(second_task.status, TaskStatus::Queued);
        assert!(second_task.latest_run_id.is_none());

        wait_for_task_status(&service, first_task_id, TaskStatus::Done);
        service.reconcile_and_dispatch_once().expect("second dispatch");
        wait_for_task_status(&service, second_task_id, TaskStatus::Done);
    }

    #[tokio::test]
    async fn reconciliation_blocks_ambiguous_active_runs_after_restart() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let session_response = service
            .dispatch_request(GatewayRequest {
                id: json!("session-recovery"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session_response.ok);
        let session_id = SessionId::new(
            session_response
                .result
                .as_ref()
                .and_then(|item| item["session"]["session_id"].as_str())
                .expect("session id"),
        )
        .expect("session id");

        let task_response = service
            .dispatch_request(GatewayRequest {
                id: json!("task-recovery"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Recover active run",
                    "instructions": "Recover active run",
                    "profile": "america",
                    "session_id": session_id.as_str(),
                }),
            })
            .await;
        assert!(task_response.ok);
        let task_id = TaskId::new(
            task_response
                .result
                .as_ref()
                .and_then(|task| task["task_id"].as_str())
                .expect("task id"),
        )
        .expect("task id");

        let run = service
            .store
            .claim_run_for_task(RunRecord::new(
                RunId::generate(),
                task_id.clone(),
                session_id.clone(),
            ))
            .expect("claim run");
        service.reconcile_inflight_runs(true).expect("reconcile in-flight run");

        let run = service.store.inspect_run(&run.run_id).expect("inspect run").expect("run exists");
        assert_eq!(run.status, RunStatus::Blocked);
        let task =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(task.status, TaskStatus::Blocked);
        assert_eq!(task.blocked_reason, Some(si_nucleus_core::BlockedReason::SessionBroken));
        let session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        assert_eq!(session.lifecycle_state, SessionLifecycleState::Broken);
    }

    #[tokio::test]
    async fn reconciliation_blocks_active_run_when_worker_disappears() {
        let temp = tempdir().expect("tempdir");
        let runtime = FakeRuntime::default();
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime.clone()),
        )
        .expect("service");

        let session_response = service
            .dispatch_request(GatewayRequest {
                id: json!("session-worker-loss"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session_response.ok);
        let worker_id = WorkerId::new(
            session_response
                .result
                .as_ref()
                .and_then(|item| item["worker"]["worker_id"].as_str())
                .expect("worker id")
                .to_owned(),
        )
        .expect("worker id");
        let session_id = SessionId::new(
            session_response
                .result
                .as_ref()
                .and_then(|item| item["session"]["session_id"].as_str())
                .expect("session id")
                .to_owned(),
        )
        .expect("session id");

        let task_response = service
            .dispatch_request(GatewayRequest {
                id: json!("task-worker-loss"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Worker loss run",
                    "instructions": "Block when the worker disappears",
                    "profile": "america",
                    "session_id": session_id.as_str(),
                }),
            })
            .await;
        assert!(task_response.ok);
        let task_id = TaskId::new(
            task_response
                .result
                .as_ref()
                .and_then(|task| task["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        let run = service
            .store
            .claim_run_for_task(RunRecord::new(
                RunId::generate(),
                task_id.clone(),
                session_id.clone(),
            ))
            .expect("claim run");

        runtime.stop_worker(&worker_id).expect("stop worker");
        service.reconcile_and_dispatch_once().expect("reconcile worker loss");

        let run = service.store.inspect_run(&run.run_id).expect("inspect run").expect("run exists");
        assert_eq!(run.status, RunStatus::Blocked);
        let task =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(task.status, TaskStatus::Blocked);
        assert_eq!(task.blocked_reason, Some(BlockedReason::WorkerUnavailable));
        let session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        assert_eq!(session.lifecycle_state, SessionLifecycleState::Broken);
        let worker = service
            .store
            .inspect_worker(&worker_id)
            .expect("inspect worker")
            .expect("worker exists");
        assert_eq!(worker.status, WorkerStatus::Failed);
    }

    #[tokio::test]
    async fn run_cancel_interrupts_active_run_and_projects_cancelled_state() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::with_run_delay(Duration::from_millis(400))),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-cancel"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = session
            .result
            .as_ref()
            .and_then(|item| item["session"]["session_id"].as_str())
            .expect("session id");

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-cancel"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Cancel direct run",
                    "instructions": "Generate enough output to be cancellable",
                    "profile": "america",
                    "session_id": session_id,
                }),
            })
            .await;
        assert!(task.ok);
        let task_id =
            task.result.as_ref().and_then(|item| item["task_id"].as_str()).expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-cancel"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id,
                    "task_id": task_id,
                    "prompt": "Generate a long response",
                }),
            })
            .await;
        assert!(run.ok);
        let run_id = run.result.as_ref().and_then(|item| item["run_id"].as_str()).expect("run id");

        let started = wait_for_run_started(&service, run_id);
        assert!(started.app_server_turn_id.is_some());

        let cancelled = service
            .dispatch_request(GatewayRequest {
                id: json!("cancel"),
                method: "run.cancel".to_owned(),
                params: json!({ "run_id": run_id }),
            })
            .await;
        assert!(cancelled.ok);

        wait_for_task_status(&service, task_id, TaskStatus::Cancelled);
        let run = service
            .store
            .inspect_run(&RunId::new(run_id).expect("run id"))
            .expect("inspect run")
            .expect("run exists");
        assert_eq!(run.status, RunStatus::Cancelled);
        let session = service
            .store
            .inspect_session(&SessionId::new(session_id).expect("session id"))
            .expect("inspect session")
            .expect("session exists");
        assert_eq!(session.lifecycle_state, SessionLifecycleState::Ready);
    }

    #[tokio::test]
    async fn run_cancel_cancels_queued_run_before_turn_start() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-cancel-before-turn"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = SessionId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["session"]["session_id"].as_str())
                .expect("session id")
                .to_owned(),
        )
        .expect("session id");

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-cancel-before-turn"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Cancel queued run",
                    "instructions": "Create a queued run before turn start",
                    "profile": "america",
                    "session_id": session_id.as_str(),
                }),
            })
            .await;
        assert!(task.ok);
        let task_id = TaskId::new(
            task.result
                .as_ref()
                .and_then(|item| item["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        let run = service
            .store
            .claim_run_for_task(RunRecord::new(
                RunId::generate(),
                task_id.clone(),
                session_id.clone(),
            ))
            .expect("claim queued run");

        let cancelled = service
            .dispatch_request(GatewayRequest {
                id: json!("cancel-before-turn"),
                method: "run.cancel".to_owned(),
                params: json!({ "run_id": run.run_id }),
            })
            .await;
        assert!(cancelled.ok);
        assert_eq!(
            cancelled.result.as_ref().and_then(|item| item["status"].as_str()),
            Some("cancelled")
        );

        let task =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(task.status, TaskStatus::Cancelled);
        let run = service.store.inspect_run(&run.run_id).expect("inspect run").expect("run exists");
        assert_eq!(run.status, RunStatus::Cancelled);
        assert!(run.app_server_turn_id.is_none());
        let session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        assert_eq!(session.lifecycle_state, SessionLifecycleState::Ready);
    }

    #[tokio::test]
    async fn run_cancel_marks_session_broken_when_thread_id_is_missing() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::with_run_delay(Duration::from_millis(400))),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-cancel-missing-thread"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = SessionId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["session"]["session_id"].as_str())
                .expect("session id")
                .to_owned(),
        )
        .expect("session id");

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-cancel-missing-thread"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Cancel run after thread loss",
                    "instructions": "Generate enough output to be cancellable",
                    "profile": "america",
                    "session_id": session_id.as_str(),
                }),
            })
            .await;
        assert!(task.ok);
        let task_id = TaskId::new(
            task.result
                .as_ref()
                .and_then(|item| item["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-cancel-missing-thread"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id.as_str(),
                    "task_id": task_id.as_str(),
                    "prompt": "Generate a long response",
                }),
            })
            .await;
        assert!(run.ok);
        let run_id = RunId::new(
            run.result
                .as_ref()
                .and_then(|item| item["run_id"].as_str())
                .expect("run id")
                .to_owned(),
        )
        .expect("run id");

        let started = wait_for_run_started(&service, run_id.as_str());
        assert!(started.app_server_turn_id.is_some());

        let mut persisted_session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        persisted_session.app_server_thread_id = None;
        service
            .store
            .write_session_projection_locked(&persisted_session)
            .expect("persist session without thread id");

        let cancelled = service
            .dispatch_request(GatewayRequest {
                id: json!("cancel-missing-thread"),
                method: "run.cancel".to_owned(),
                params: json!({ "run_id": run_id.as_str() }),
            })
            .await;
        assert!(cancelled.ok);
        assert_eq!(
            cancelled.result.as_ref().and_then(|item| item["status"].as_str()),
            Some("cancelled")
        );

        let task =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(task.status, TaskStatus::Cancelled);
        let run = service.store.inspect_run(&run_id).expect("inspect run").expect("run exists");
        assert_eq!(run.status, RunStatus::Cancelled);
        let session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        assert_eq!(session.lifecycle_state, SessionLifecycleState::Broken);
    }

    #[tokio::test]
    async fn run_cancel_transitions_blocked_run_to_cancelled() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::with_run_delay(Duration::from_millis(400))),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-cancel-blocked-run"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = SessionId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["session"]["session_id"].as_str())
                .expect("session id")
                .to_owned(),
        )
        .expect("session id");

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-cancel-blocked-run"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Cancel blocked run",
                    "instructions": "Generate enough output to become cancellable after blocking",
                    "profile": "america",
                    "session_id": session_id.as_str(),
                }),
            })
            .await;
        assert!(task.ok);
        let task_id = TaskId::new(
            task.result
                .as_ref()
                .and_then(|item| item["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-cancel-blocked-run"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id.as_str(),
                    "task_id": task_id.as_str(),
                    "prompt": "Generate a long response",
                }),
            })
            .await;
        assert!(run.ok);
        let run_id = RunId::new(
            run.result
                .as_ref()
                .and_then(|item| item["run_id"].as_str())
                .expect("run id")
                .to_owned(),
        )
        .expect("run id");

        let started = wait_for_run_started(&service, run_id.as_str());
        assert!(started.app_server_turn_id.is_some());

        let mut persisted_session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        persisted_session.app_server_thread_id = None;
        service
            .store
            .write_session_projection_locked(&persisted_session)
            .expect("persist session without thread id");

        service.reconcile_and_dispatch_once().expect("reconcile blocked run");
        let blocked_run =
            service.store.inspect_run(&run_id).expect("inspect run").expect("run exists");
        assert_eq!(blocked_run.status, RunStatus::Blocked);

        let cancelled = service
            .dispatch_request(GatewayRequest {
                id: json!("cancel-blocked-run"),
                method: "run.cancel".to_owned(),
                params: json!({ "run_id": run_id.as_str() }),
            })
            .await;
        assert!(cancelled.ok);
        assert_eq!(
            cancelled.result.as_ref().and_then(|item| item["status"].as_str()),
            Some("cancelled")
        );

        let task =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(task.status, TaskStatus::Cancelled);
        let run = service.store.inspect_run(&run_id).expect("inspect run").expect("run exists");
        assert_eq!(run.status, RunStatus::Cancelled);
    }

    #[tokio::test]
    async fn task_cancel_marks_session_broken_when_thread_id_is_missing() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::with_run_delay(Duration::from_millis(400))),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-task-cancel-missing-thread"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = SessionId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["session"]["session_id"].as_str())
                .expect("session id")
                .to_owned(),
        )
        .expect("session id");

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-task-cancel-missing-thread"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Cancel task after thread loss",
                    "instructions": "Generate enough output to be cancellable",
                    "profile": "america",
                    "session_id": session_id.as_str(),
                }),
            })
            .await;
        assert!(task.ok);
        let task_id = TaskId::new(
            task.result
                .as_ref()
                .and_then(|item| item["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-task-cancel-missing-thread"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id.as_str(),
                    "task_id": task_id.as_str(),
                    "prompt": "Generate a long response",
                }),
            })
            .await;
        assert!(run.ok);
        let run_id = RunId::new(
            run.result
                .as_ref()
                .and_then(|item| item["run_id"].as_str())
                .expect("run id")
                .to_owned(),
        )
        .expect("run id");

        let started = wait_for_run_started(&service, run_id.as_str());
        assert!(started.app_server_turn_id.is_some());

        let mut persisted_session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        persisted_session.app_server_thread_id = None;
        service
            .store
            .write_session_projection_locked(&persisted_session)
            .expect("persist session without thread id");

        let cancelled = service
            .dispatch_request(GatewayRequest {
                id: json!("task-cancel-missing-thread"),
                method: "task.cancel".to_owned(),
                params: json!({ "task_id": task_id.as_str() }),
            })
            .await;
        assert!(cancelled.ok);
        assert_eq!(
            cancelled.result.as_ref().and_then(|item| item["task"]["status"].as_str()),
            Some("cancelled")
        );
        assert_eq!(
            cancelled.result.as_ref().and_then(|item| item["cancellation_requested"].as_bool()),
            Some(false)
        );

        let task =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(task.status, TaskStatus::Cancelled);
        let run = service.store.inspect_run(&run_id).expect("inspect run").expect("run exists");
        assert_eq!(run.status, RunStatus::Cancelled);
        let session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        assert_eq!(session.lifecycle_state, SessionLifecycleState::Broken);
    }

    #[tokio::test]
    async fn task_cancel_transitions_blocked_run_to_cancelled() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::with_run_delay(Duration::from_millis(400))),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-task-cancel-blocked-run"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = SessionId::new(
            session
                .result
                .as_ref()
                .and_then(|item| item["session"]["session_id"].as_str())
                .expect("session id")
                .to_owned(),
        )
        .expect("session id");

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-task-cancel-blocked-run"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Cancel blocked run through task surface",
                    "instructions": "Generate enough output to be cancellable",
                    "profile": "america",
                    "session_id": session_id.as_str(),
                }),
            })
            .await;
        assert!(task.ok);
        let task_id = TaskId::new(
            task.result
                .as_ref()
                .and_then(|item| item["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-task-cancel-blocked-run"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id.as_str(),
                    "task_id": task_id.as_str(),
                    "prompt": "Generate a long response",
                }),
            })
            .await;
        assert!(run.ok);
        let run_id = RunId::new(
            run.result
                .as_ref()
                .and_then(|item| item["run_id"].as_str())
                .expect("run id")
                .to_owned(),
        )
        .expect("run id");

        let started = wait_for_run_started(&service, run_id.as_str());
        assert!(started.app_server_turn_id.is_some());

        let mut persisted_session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        persisted_session.app_server_thread_id = None;
        service
            .store
            .write_session_projection_locked(&persisted_session)
            .expect("persist session without thread id");

        service.reconcile_and_dispatch_once().expect("reconcile blocked run");
        let blocked_run =
            service.store.inspect_run(&run_id).expect("inspect run").expect("run exists");
        assert_eq!(blocked_run.status, RunStatus::Blocked);

        let cancelled = service
            .dispatch_request(GatewayRequest {
                id: json!("task-cancel-blocked-run"),
                method: "task.cancel".to_owned(),
                params: json!({ "task_id": task_id.as_str() }),
            })
            .await;
        assert!(cancelled.ok);
        assert_eq!(
            cancelled.result.as_ref().and_then(|item| item["task"]["status"].as_str()),
            Some("cancelled")
        );
        assert_eq!(
            cancelled.result.as_ref().and_then(|item| item["run"]["status"].as_str()),
            Some("cancelled")
        );

        let task =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(task.status, TaskStatus::Cancelled);
        let run = service.store.inspect_run(&run_id).expect("inspect run").expect("run exists");
        assert_eq!(run.status, RunStatus::Cancelled);
    }

    #[tokio::test]
    async fn cancelled_tasks_do_not_requeue_after_restart_or_reconcile() {
        let temp = tempdir().expect("tempdir");
        let runtime = Arc::new(FakeRuntime::default());
        let state_dir = temp.path().join("nucleus");
        let config = NucleusConfig {
            bind_addr: "127.0.0.1:9898".parse().expect("addr"),
            state_dir: state_dir.clone(),
            auth_token: None,
        };
        let service =
            NucleusService::open_with_runtime(config.clone(), runtime.clone()).expect("service");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("cancelled-no-requeue-task"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Do not requeue cancelled task",
                    "instructions": "This task should stay terminal after cancellation",
                    "profile": "america",
                }),
            })
            .await;
        assert!(created.ok);
        let task_id = TaskId::new(
            created
                .result
                .as_ref()
                .and_then(|item| item["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        let cancelled = service
            .dispatch_request(GatewayRequest {
                id: json!("cancelled-no-requeue-cancel"),
                method: "task.cancel".to_owned(),
                params: json!({ "task_id": task_id.as_str() }),
            })
            .await;
        assert!(cancelled.ok);
        assert_eq!(
            cancelled.result.as_ref().and_then(|item| item["task"]["status"].as_str()),
            Some("cancelled")
        );

        drop(service);

        let reopened =
            NucleusService::open_with_runtime(config, runtime.clone()).expect("reopen service");
        reopened.reconcile_and_dispatch_once().expect("reconcile restarted service");

        let task =
            reopened.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(task.status, TaskStatus::Cancelled);
        assert!(task.latest_run_id.is_none());
        assert_eq!(runtime.start_call_count(), 0);
        assert!(reopened.store.list_workers().expect("workers").is_empty());
        assert!(reopened.store.list_runs().expect("runs").is_empty());
    }

    #[tokio::test]
    async fn task_cancel_transitions_blocked_task_to_cancelled() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let created_session = service
            .dispatch_request(GatewayRequest {
                id: json!("blocked-cancel-session"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(created_session.ok);
        let session_id = SessionId::new(
            created_session
                .result
                .as_ref()
                .and_then(|item| item["session"]["session_id"].as_str())
                .expect("session id")
                .to_owned(),
        )
        .expect("session id");

        let mut session = service
            .store
            .inspect_session(&session_id)
            .expect("inspect session")
            .expect("session exists");
        session.app_server_thread_id = None;
        service
            .store
            .write_session_projection_locked(&session)
            .expect("persist session without thread id");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("blocked-cancel-task"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Cancel blocked task",
                    "instructions": "Create a blocked task before cancellation",
                    "profile": "america",
                    "session_id": session_id.as_str(),
                }),
            })
            .await;
        assert!(created.ok);
        let task_id = TaskId::new(
            created
                .result
                .as_ref()
                .and_then(|item| item["task_id"].as_str())
                .expect("task id")
                .to_owned(),
        )
        .expect("task id");

        service.reconcile_and_dispatch_once().expect("block queued task");

        let blocked =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(blocked.status, TaskStatus::Blocked);
        assert_eq!(blocked.blocked_reason, Some(BlockedReason::SessionBroken));
        assert!(blocked.latest_run_id.is_none());

        let cancelled = service
            .dispatch_request(GatewayRequest {
                id: json!("blocked-cancel"),
                method: "task.cancel".to_owned(),
                params: json!({ "task_id": task_id.as_str() }),
            })
            .await;
        assert!(cancelled.ok);
        assert_eq!(
            cancelled.result.as_ref().and_then(|item| item["task"]["status"].as_str()),
            Some("cancelled")
        );
        assert_eq!(
            cancelled.result.as_ref().and_then(|item| item["cancellation_requested"].as_bool()),
            Some(false)
        );

        let task =
            service.store.inspect_task(&task_id).expect("inspect task").expect("task exists");
        assert_eq!(task.status, TaskStatus::Cancelled);
        assert_eq!(task.blocked_reason, None);
        assert!(task.latest_run_id.is_none());
    }

    #[test]
    fn cron_producer_emits_due_task_once_across_replay() {
        let temp = tempdir().expect("tempdir");
        let state_root = temp.path().join("nucleus");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: state_root.clone(),
            auth_token: None,
        })
        .expect("service");
        let now = Utc::now();
        write_cron_rule(
            &state_root,
            &CronRuleRecord {
                name: "nightly".to_owned(),
                enabled: true,
                schedule_kind: CronScheduleKind::Every,
                schedule: "60s".to_owned(),
                instructions: "Run nightly maintenance".to_owned(),
                last_emitted_at: None,
                next_due_at: Some(now - ChronoDuration::seconds(30)),
                version: current_persisted_version().to_owned(),
            },
        );

        service.process_cron_producers_at(now).expect("process cron");
        let tasks = service.store.list_tasks().expect("list tasks");
        let expected_dedup = cron_due_key("nightly", now - ChronoDuration::seconds(30));
        assert_eq!(tasks.len(), 1);
        assert_eq!(tasks[0].source, si_nucleus_core::TaskSource::Cron);
        assert_eq!(tasks[0].producer_rule_name.as_deref(), Some("nightly"));
        assert_eq!(tasks[0].producer_dedup_key.as_deref(), Some(expected_dedup.as_str()));

        let stored = read_cron_rule(&state_root, "nightly");
        assert_eq!(stored.last_emitted_at, Some(now - ChronoDuration::seconds(30)));
        assert!(stored.next_due_at.is_some_and(|value| value > now));

        service.process_cron_producers_at(now).expect("replay cron");
        drop(service);

        let reopened = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: state_root,
            auth_token: None,
        })
        .expect("reopen service");
        reopened.process_cron_producers_at(now).expect("replay cron after restart");
        assert_eq!(reopened.store.list_tasks().expect("list tasks").len(), 1);
    }

    #[test]
    fn cron_producer_replay_advances_rule_after_durable_task_exists() {
        let temp = tempdir().expect("tempdir");
        let state_root = temp.path().join("nucleus");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: state_root.clone(),
            auth_token: None,
        })
        .expect("service");
        let now = Utc::now();
        let due_at = now - ChronoDuration::seconds(30);
        let dedup_key = cron_due_key("nightly", due_at);
        write_cron_rule(
            &state_root,
            &CronRuleRecord {
                name: "nightly".to_owned(),
                enabled: true,
                schedule_kind: CronScheduleKind::Every,
                schedule: "60s".to_owned(),
                instructions: "Run nightly maintenance".to_owned(),
                last_emitted_at: None,
                next_due_at: Some(due_at),
                version: current_persisted_version().to_owned(),
            },
        );
        service
            .store
            .create_producer_task(
                CreateTaskInput {
                    title: "Cron nightly @ precreated".to_owned(),
                    instructions: "Simulate crash after durable task creation".to_owned(),
                    source: TaskSource::Cron,
                    profile: None,
                    session_id: None,
                    max_retries: None,
                    timeout_seconds: None,
                },
                "nightly",
                &dedup_key,
            )
            .expect("create durable producer task");

        let before = read_cron_rule(&state_root, "nightly");
        assert_eq!(before.last_emitted_at, None);
        assert_eq!(before.next_due_at, Some(due_at));

        service.process_cron_producers_at(now).expect("replay cron");

        let tasks = service.store.list_tasks().expect("list tasks");
        assert_eq!(tasks.len(), 1);
        assert_eq!(tasks[0].producer_dedup_key.as_deref(), Some(dedup_key.as_str()));
        let stored = read_cron_rule(&state_root, "nightly");
        assert_eq!(stored.last_emitted_at, Some(due_at));
        assert!(stored.next_due_at.is_some_and(|value| value > now));
    }

    #[test]
    fn cron_producer_rewrites_legacy_numeric_version_to_current_si_version() {
        let temp = tempdir().expect("tempdir");
        let state_root = temp.path().join("nucleus");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: state_root.clone(),
            auth_token: None,
        })
        .expect("service");
        let now = Utc::now();
        let rule_path =
            state_root.join("state").join("producers").join("cron").join("nightly.json");
        fs::create_dir_all(rule_path.parent().expect("cron dir")).expect("create cron dir");
        fs::write(
            &rule_path,
            serde_json::to_vec(&json!({
                "name": "nightly",
                "enabled": true,
                "schedule_kind": "every",
                "schedule": "60s",
                "instructions": "Run nightly maintenance",
                "last_emitted_at": null,
                "next_due_at": now.to_rfc3339(),
                "version": 1
            }))
            .expect("serialize legacy rule"),
        )
        .expect("write legacy rule");

        service.process_cron_producers_at(now).expect("process cron");

        let stored = read_cron_rule(&state_root, "nightly");
        assert_eq!(stored.version, current_persisted_version());
    }

    #[test]
    fn cron_producer_invalid_rule_name_emits_system_warning() {
        let temp = tempdir().expect("tempdir");
        let state_root = temp.path().join("nucleus");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: state_root.clone(),
            auth_token: None,
        })
        .expect("service");
        let now = Utc::now();
        write_cron_rule(
            &state_root,
            &CronRuleRecord {
                name: "Nightly".to_owned(),
                enabled: true,
                schedule_kind: CronScheduleKind::Every,
                schedule: "60s".to_owned(),
                instructions: "Run nightly maintenance".to_owned(),
                last_emitted_at: None,
                next_due_at: Some(now),
                version: current_persisted_version().to_owned(),
            },
        );

        service.process_cron_producers_at(now).expect("process cron");

        assert_eq!(service.store.list_tasks().expect("list tasks").len(), 0);
        let warning = load_canonical_events(&service.store.paths().events_path)
            .expect("load events")
            .into_iter()
            .find(|event| {
                event.event_type == CanonicalEventType::SystemWarning
                    && event.data.payload["message"] == json!("cron producer iteration failed")
            })
            .expect("warning event");
        assert_eq!(warning.data.payload["details"]["rule_name"], json!("Nightly"));
        assert!(
            warning.data.payload["details"]["error"]
                .as_str()
                .expect("warning error")
                .contains("validate cron rule name")
        );
    }

    #[tokio::test]
    async fn hook_processing_skips_event_replay_when_no_rules_exist() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: temp.path().join("nucleus"),
            auth_token: None,
        })
        .expect("service");

        fs::write(&service.store.paths().events_path, b"{\"seq\":")
            .expect("write malformed event ledger");

        service.process_hook_producers().expect("skip hook processing without rules");
    }

    #[tokio::test]
    async fn hook_producer_emits_task_once_for_matching_event_and_advances_cursor() {
        let temp = tempdir().expect("tempdir");
        let state_root = temp.path().join("nucleus");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: state_root.clone(),
            auth_token: None,
        })
        .expect("service");
        write_hook_rule(
            &state_root,
            &HookRuleRecord {
                name: "task-created".to_owned(),
                enabled: true,
                match_event_type: "task.created".to_owned(),
                instructions: "Investigate newly created task".to_owned(),
                last_processed_event_seq: 0,
                version: current_persisted_version().to_owned(),
            },
        );

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!(1),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Primary task",
                    "instructions": "Create hook input",
                }),
            })
            .await;
        assert!(created.ok);

        service.process_hook_producers().expect("process hooks");
        service.process_hook_producers().expect("process hooks replay");
        let tasks = service.store.list_tasks().expect("list tasks");
        assert_eq!(tasks.len(), 2);
        let hook_task = tasks
            .iter()
            .find(|task| task.source == si_nucleus_core::TaskSource::Hook)
            .expect("hook task");
        assert_eq!(hook_task.producer_rule_name.as_deref(), Some("task-created"));
        let stored = read_hook_rule(&state_root, "task-created");
        assert_eq!(stored.last_processed_event_seq, 2);

        drop(service);

        let reopened = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: state_root,
            auth_token: None,
        })
        .expect("reopen service");
        reopened.process_hook_producers().expect("process hooks after restart");
        assert_eq!(reopened.store.list_tasks().expect("list tasks").len(), 2);
    }

    #[tokio::test]
    async fn hook_producer_replay_advances_cursor_after_durable_task_exists() {
        let temp = tempdir().expect("tempdir");
        let state_root = temp.path().join("nucleus");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: state_root.clone(),
            auth_token: None,
        })
        .expect("service");
        write_hook_rule(
            &state_root,
            &HookRuleRecord {
                name: "task-created".to_owned(),
                enabled: true,
                match_event_type: "task.created".to_owned(),
                instructions: "Investigate newly created task".to_owned(),
                last_processed_event_seq: 0,
                version: current_persisted_version().to_owned(),
            },
        );

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!(1),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Primary task",
                    "instructions": "Create hook input",
                }),
            })
            .await;
        assert!(created.ok);

        let task_created_event = load_canonical_events(&service.store.paths().events_path)
            .expect("load events")
            .into_iter()
            .find(|event| event.event_type == CanonicalEventType::TaskCreated)
            .expect("task.created event");
        let dedup_key = hook_event_key("task-created", task_created_event.seq);
        service
            .store
            .create_producer_task(
                CreateTaskInput {
                    title: "Hook task-created @ precreated".to_owned(),
                    instructions: "Simulate crash after durable hook task creation".to_owned(),
                    source: TaskSource::Hook,
                    profile: None,
                    session_id: None,
                    max_retries: None,
                    timeout_seconds: None,
                },
                "task-created",
                &dedup_key,
            )
            .expect("create durable hook task");
        let max_seq_before_replay = load_canonical_events(&service.store.paths().events_path)
            .expect("load events after precreate")
            .into_iter()
            .map(|event| event.seq)
            .max()
            .expect("max event seq");

        let before = read_hook_rule(&state_root, "task-created");
        assert_eq!(before.last_processed_event_seq, 0);

        service.process_hook_producers().expect("replay hooks");

        let tasks = service.store.list_tasks().expect("list tasks");
        assert_eq!(tasks.len(), 2);
        let hook_task = tasks
            .iter()
            .find(|task| task.source == si_nucleus_core::TaskSource::Hook)
            .expect("hook task");
        assert_eq!(hook_task.producer_dedup_key.as_deref(), Some(dedup_key.as_str()));
        let stored = read_hook_rule(&state_root, "task-created");
        assert_eq!(stored.last_processed_event_seq, max_seq_before_replay);
    }

    #[tokio::test]
    async fn hook_producer_rewrites_legacy_numeric_version_to_current_si_version() {
        let temp = tempdir().expect("tempdir");
        let state_root = temp.path().join("nucleus");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: state_root.clone(),
            auth_token: None,
        })
        .expect("service");
        let rule_path =
            state_root.join("state").join("producers").join("hook").join("task-created.json");
        fs::create_dir_all(rule_path.parent().expect("hook dir")).expect("create hook dir");
        fs::write(
            &rule_path,
            serde_json::to_vec(&json!({
                "name": "task-created",
                "enabled": true,
                "match_event_type": "task.created",
                "instructions": "Investigate newly created task",
                "last_processed_event_seq": 0,
                "version": 1
            }))
            .expect("serialize legacy rule"),
        )
        .expect("write legacy rule");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!(1),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Primary task",
                    "instructions": "Create hook input",
                }),
            })
            .await;
        assert!(created.ok);

        service.process_hook_producers().expect("process hooks");

        let stored = read_hook_rule(&state_root, "task-created");
        assert_eq!(stored.version, current_persisted_version());
    }

    #[tokio::test]
    async fn hook_producer_invalid_rule_name_emits_system_warning() {
        let temp = tempdir().expect("tempdir");
        let state_root = temp.path().join("nucleus");
        let service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:4747".parse().expect("addr"),
            state_dir: state_root.clone(),
            auth_token: None,
        })
        .expect("service");
        write_hook_rule(
            &state_root,
            &HookRuleRecord {
                name: "Task-Created".to_owned(),
                enabled: true,
                match_event_type: "task.created".to_owned(),
                instructions: "Investigate newly created task".to_owned(),
                last_processed_event_seq: 0,
                version: current_persisted_version().to_owned(),
            },
        );

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!(1),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Primary task",
                    "instructions": "Create hook input",
                }),
            })
            .await;
        assert!(created.ok);

        service.process_hook_producers().expect("process hooks");

        let tasks = service.store.list_tasks().expect("list tasks");
        assert_eq!(tasks.len(), 1);
        let warning = load_canonical_events(&service.store.paths().events_path)
            .expect("load events")
            .into_iter()
            .find(|event| {
                event.event_type == CanonicalEventType::SystemWarning
                    && event.data.payload["message"] == json!("hook producer iteration failed")
            })
            .expect("warning event");
        assert_eq!(warning.data.payload["details"]["rule_name"], json!("Task-Created"));
        assert!(
            warning.data.payload["details"]["error"]
                .as_str()
                .expect("warning error")
                .contains("validate hook rule name")
        );
    }

    #[tokio::test]
    async fn producer_created_task_can_route_when_one_profile_is_available() {
        let temp = tempdir().expect("tempdir");
        let state_root = temp.path().join("nucleus");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: state_root.clone(),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-producer"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);

        let now = Utc::now();
        write_cron_rule(
            &state_root,
            &CronRuleRecord {
                name: "maintenance".to_owned(),
                enabled: true,
                schedule_kind: CronScheduleKind::OnceAt,
                schedule: (now - ChronoDuration::seconds(5)).to_rfc3339(),
                instructions: "Reply with nucleus-smoke".to_owned(),
                last_emitted_at: None,
                next_due_at: Some(now - ChronoDuration::seconds(5)),
                version: current_persisted_version().to_owned(),
            },
        );

        service.process_cron_producers_at(now).expect("process cron");
        service.reconcile_and_dispatch_once().expect("dispatch producer task");

        let cron_task = service
            .store
            .list_tasks()
            .expect("list tasks")
            .into_iter()
            .find(|task| task.source == si_nucleus_core::TaskSource::Cron)
            .expect("cron task");
        wait_for_task_status(&service, cron_task.task_id.as_str(), TaskStatus::Done);
    }

    #[tokio::test]
    async fn run_submit_turn_failure_before_run_started_marks_run_and_task_failed() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::with_execute_failure()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-fail"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = session
            .result
            .as_ref()
            .and_then(|item| item["session"]["session_id"].as_str())
            .expect("session id");

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-fail"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Fail direct run",
                    "instructions": "Fail direct run",
                    "profile": "america",
                    "session_id": session_id,
                }),
            })
            .await;
        assert!(task.ok);
        let task_id =
            task.result.as_ref().and_then(|item| item["task_id"].as_str()).expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-fail"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id,
                    "task_id": task_id,
                    "prompt": "fail before run.started",
                }),
            })
            .await;
        assert!(run.ok);
        let run_id = RunId::new(
            run.result.as_ref().and_then(|item| item["run_id"].as_str()).expect("run id"),
        )
        .expect("run id");

        wait_for_task_status(&service, task_id, TaskStatus::Failed);
        let run = service.store.inspect_run(&run_id).expect("inspect run").expect("run exists");
        assert_eq!(run.status, RunStatus::Failed);
        let session = wait_for_session_state(&service, &session_id, SessionLifecycleState::Broken);
        assert_eq!(session.lifecycle_state, SessionLifecycleState::Broken);
    }

    #[tokio::test]
    async fn task_create_blocks_reuse_after_execute_turn_failure_breaks_session() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::with_execute_failure()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-fail-reuse"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = session
            .result
            .as_ref()
            .and_then(|item| item["session"]["session_id"].as_str())
            .expect("session id")
            .to_owned();

        let first_task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-fail-reuse-first"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "First run fails",
                    "instructions": "First run fails",
                    "profile": "america",
                    "session_id": session_id,
                }),
            })
            .await;
        assert!(first_task.ok);
        let first_task_id = first_task
            .result
            .as_ref()
            .and_then(|item| item["task_id"].as_str())
            .expect("task id")
            .to_owned();

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-fail-reuse-first"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id,
                    "task_id": first_task_id,
                    "prompt": "fail before run.started",
                }),
            })
            .await;
        assert!(run.ok);

        wait_for_task_status(&service, &first_task_id, TaskStatus::Failed);

        let second_task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-fail-reuse-second"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Reuse broken session",
                    "instructions": "Reuse broken session",
                    "profile": "america",
                    "session_id": session_id,
                }),
            })
            .await;
        assert!(second_task.ok);
        let created = second_task.result.expect("created task");
        assert_eq!(created["status"], json!("blocked"));
        assert_eq!(created["blocked_reason"], json!("session_broken"));
    }

    #[test]
    fn stable_workdir_prefers_existing_current_dir() {
        let temp = tempdir().expect("tempdir");
        let home = temp.path().join("home");
        fs::create_dir_all(&home).expect("create home");
        let workdir = stable_workdir_from(Some(temp.path().to_path_buf()), home);
        assert_eq!(workdir, temp.path());
    }

    #[test]
    fn stable_workdir_falls_back_to_home_when_current_dir_is_missing() {
        let temp = tempdir().expect("tempdir");
        let home = temp.path().join("home");
        fs::create_dir_all(&home).expect("create home");
        let workdir = stable_workdir_from(Some(temp.path().join("missing")), home.clone());
        assert_eq!(workdir, home);
    }

    #[test]
    fn runtime_error_requires_worker_quarantine_matches_worker_channel_failures() {
        assert!(runtime_error_requires_worker_quarantine("worker notification stream closed"));
        assert!(runtime_error_requires_worker_quarantine("worker request timed out: turn/start"));
        assert!(!runtime_error_requires_worker_quarantine("turn became idle for 7200 seconds"));
    }

    #[test]
    fn run_failure_requires_session_quarantine_matches_timeout_failures() {
        assert!(run_failure_requires_session_quarantine("turn timed out"));
        assert!(run_failure_requires_session_quarantine("turn became idle for 7200 seconds"));
        assert!(run_failure_requires_session_quarantine(
            "turn exceeded max duration of 7200 seconds"
        ));
        assert!(!run_failure_requires_session_quarantine("agent refused to answer"));
    }

    #[tokio::test]
    async fn run_submit_turn_runtime_failure_marks_session_broken() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::with_run_failure("turn exceeded max duration of 7200 seconds")),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-runtime-fail"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let worker_id = WorkerId::new(
            session.result.as_ref().expect("session result")["worker"]["worker_id"]
                .as_str()
                .expect("worker id"),
        )
        .expect("worker id");
        let session_id = session.result.as_ref().expect("session result")["session"]["session_id"]
            .as_str()
            .expect("session id")
            .to_owned();

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-runtime-fail"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Runtime failure",
                    "instructions": "Runtime failure",
                    "profile": "america",
                    "session_id": session_id,
                }),
            })
            .await;
        assert!(task.ok);
        let task_id =
            task.result.as_ref().expect("task result")["task_id"].as_str().expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-runtime-fail"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id,
                    "task_id": task_id,
                    "prompt": "fail via run.failed event",
                }),
            })
            .await;
        assert!(run.ok);
        let run_id = RunId::new(
            run.result.as_ref().expect("run result")["run_id"].as_str().expect("run id"),
        )
        .expect("run id");

        wait_for_task_status(&service, task_id, TaskStatus::Failed);
        let run = service.store.inspect_run(&run_id).expect("inspect run").expect("run exists");
        assert_eq!(run.status, RunStatus::Failed);
        let session = service
            .store
            .inspect_session(&SessionId::new(session_id).expect("session id"))
            .expect("inspect session")
            .expect("session exists");
        assert_eq!(session.lifecycle_state, SessionLifecycleState::Broken);
        let worker = service
            .store
            .inspect_worker(&worker_id)
            .expect("inspect worker")
            .expect("worker exists");
        assert_eq!(worker.status, WorkerStatus::Ready);
    }

    #[tokio::test]
    async fn run_submit_turn_worker_runtime_failure_marks_worker_failed() {
        let temp = tempdir().expect("tempdir");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::with_run_failure("worker notification stream closed")),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-worker-runtime-fail"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let worker_id = WorkerId::new(
            session.result.as_ref().expect("session result")["worker"]["worker_id"]
                .as_str()
                .expect("worker id"),
        )
        .expect("worker id");
        let session_id = session.result.as_ref().expect("session result")["session"]["session_id"]
            .as_str()
            .expect("session id")
            .to_owned();

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-worker-runtime-fail"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Worker runtime failure",
                    "instructions": "Worker runtime failure",
                    "profile": "america",
                    "session_id": session_id,
                }),
            })
            .await;
        assert!(task.ok);
        let task_id =
            task.result.as_ref().expect("task result")["task_id"].as_str().expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-worker-runtime-fail"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id,
                    "task_id": task_id,
                    "prompt": "fail via worker transport",
                }),
            })
            .await;
        assert!(run.ok);

        wait_for_task_status(&service, task_id, TaskStatus::Failed);
        let session = service
            .store
            .inspect_session(&SessionId::new(session_id).expect("session id"))
            .expect("inspect session")
            .expect("session exists");
        assert_eq!(session.lifecycle_state, SessionLifecycleState::Broken);
        let worker = service
            .store
            .inspect_worker(&worker_id)
            .expect("inspect worker")
            .expect("worker exists");
        assert_eq!(worker.status, WorkerStatus::Failed);
    }

    #[tokio::test]
    async fn dispatched_task_retries_after_non_quarantining_failure() {
        let temp = tempdir().expect("tempdir");
        let runtime = FakeRuntime::with_run_failures("agent refused to answer", 1);
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime),
        )
        .expect("service");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-retry-soft-fail"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Retry soft failure",
                    "instructions": "Retry soft failure",
                    "profile": "america",
                    "max_retries": 1,
                }),
            })
            .await;
        assert!(created.ok);
        let task_id =
            created.result.expect("task result")["task_id"].as_str().expect("task id").to_owned();

        service.reconcile_and_dispatch_once().expect("dispatch first attempt");

        let queued = wait_for_task_state(&service, &task_id, |task| {
            task.status == TaskStatus::Queued && task.attempt_count == 1
        });
        assert_eq!(queued.max_retries, Some(1));
        assert!(!queued.session_binding_locked);
        let first_run_id = queued.latest_run_id.clone().expect("first run id");
        let first_session_id = queued.session_id.clone().expect("reused session id");

        for _ in 0..20 {
            service.reconcile_and_dispatch_once().expect("dispatch retry attempt");
            let task = service
                .store
                .inspect_task(&TaskId::new(&task_id).expect("task id"))
                .expect("inspect task")
                .expect("task exists");
            if task.status == TaskStatus::Done {
                break;
            }
            thread::sleep(Duration::from_millis(25));
        }
        wait_for_task_status(&service, &task_id, TaskStatus::Done);

        let final_task = service
            .store
            .inspect_task(&TaskId::new(&task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(final_task.attempt_count, 2);
        assert_eq!(final_task.status, TaskStatus::Done);
        assert_eq!(final_task.max_retries, Some(1));
        assert_eq!(final_task.session_id, Some(first_session_id));

        let runs = service.store.list_runs().expect("list runs");
        let task_runs =
            runs.into_iter().filter(|run| run.task_id.as_str() == task_id).collect::<Vec<_>>();
        assert_eq!(task_runs.len(), 2);
        assert_eq!(task_runs[0].run_id, first_run_id);
        assert_eq!(task_runs[0].status, RunStatus::Failed);
        assert_eq!(task_runs[1].status, RunStatus::Completed);
    }

    #[tokio::test]
    async fn dispatched_task_retries_after_session_breaking_failure_with_new_session() {
        let temp = tempdir().expect("tempdir");
        let runtime =
            FakeRuntime::with_run_failures("turn exceeded max duration of 7200 seconds", 1);
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime),
        )
        .expect("service");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-retry-broken-session"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Retry broken session",
                    "instructions": "Retry broken session",
                    "profile": "america",
                    "max_retries": 1,
                }),
            })
            .await;
        assert!(created.ok);
        let task_id =
            created.result.expect("task result")["task_id"].as_str().expect("task id").to_owned();

        service.reconcile_and_dispatch_once().expect("dispatch first attempt");

        let queued = wait_for_task_state(&service, &task_id, |task| {
            task.status == TaskStatus::Queued
                && task.attempt_count == 1
                && task.session_id.is_none()
        });
        let first_run_id = queued.latest_run_id.clone().expect("first run id");
        let first_run =
            service.store.inspect_run(&first_run_id).expect("inspect run").expect("run exists");
        let first_session_id = first_run.session_id.clone();
        let broken_session = service
            .store
            .inspect_session(&first_session_id)
            .expect("inspect session")
            .expect("session exists");
        assert_eq!(broken_session.lifecycle_state, SessionLifecycleState::Broken);

        for _ in 0..20 {
            service.reconcile_and_dispatch_once().expect("dispatch retry attempt");
            let task = service
                .store
                .inspect_task(&TaskId::new(&task_id).expect("task id"))
                .expect("inspect task")
                .expect("task exists");
            if task.status == TaskStatus::Done {
                break;
            }
            thread::sleep(Duration::from_millis(25));
        }
        wait_for_task_status(&service, &task_id, TaskStatus::Done);

        let final_task = service
            .store
            .inspect_task(&TaskId::new(&task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(final_task.attempt_count, 2);
        let final_session_id = final_task.session_id.clone().expect("retry session id");
        assert_ne!(final_session_id, first_session_id);

        let final_run_id = final_task.latest_run_id.clone().expect("final run id");
        let final_run =
            service.store.inspect_run(&final_run_id).expect("inspect run").expect("run exists");
        assert_eq!(final_run.status, RunStatus::Completed);
        assert_eq!(final_run.session_id, final_session_id);
    }

    #[tokio::test]
    async fn dispatched_task_does_not_retry_when_explicit_session_breaks() {
        let temp = tempdir().expect("tempdir");
        let runtime =
            FakeRuntime::with_run_failures("turn exceeded max duration of 7200 seconds", 1);
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-retry-locked"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = session.result.expect("session")["session"]["session_id"]
            .as_str()
            .expect("session id")
            .to_owned();

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-retry-locked"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Retry locked session",
                    "instructions": "Retry locked session",
                    "profile": "america",
                    "session_id": session_id,
                    "max_retries": 2,
                }),
            })
            .await;
        assert!(created.ok);
        let task_id =
            created.result.expect("task result")["task_id"].as_str().expect("task id").to_owned();

        service.reconcile_and_dispatch_once().expect("dispatch explicit session attempt");
        wait_for_task_status(&service, &task_id, TaskStatus::Failed);

        let task = service
            .store
            .inspect_task(&TaskId::new(&task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(task.attempt_count, 1);
        assert_eq!(task.status, TaskStatus::Failed);
        assert!(task.session_binding_locked);
        assert_eq!(task.session_id.as_ref().map(SessionId::as_str), Some(session_id.as_str()));

        let runs = service.store.list_runs().expect("list runs");
        let task_runs =
            runs.into_iter().filter(|run| run.task_id.as_str() == task_id).collect::<Vec<_>>();
        assert_eq!(task_runs.len(), 1);
        assert_eq!(task_runs[0].status, RunStatus::Failed);
    }

    #[tokio::test]
    async fn dispatcher_blocks_fort_task_when_auth_is_required() {
        let temp = tempdir().expect("tempdir");
        let codex_home = temp.path().join("home/.si/codex/profiles/america");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-fort-auth"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Check Fort bootstrap",
                    "instructions": "Use si fort status before continuing",
                    "profile": "america",
                }),
            })
            .await;
        assert!(created.ok);
        let task_id =
            created.result.as_ref().and_then(|task| task["task_id"].as_str()).expect("task id");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-fort-auth"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": codex_home,
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);

        service.reconcile_and_dispatch_once().expect("dispatch fort task");

        let task = service
            .store
            .inspect_task(&TaskId::new(task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(task.status, TaskStatus::Blocked);
        assert_eq!(task.blocked_reason, Some(BlockedReason::AuthRequired));
        assert!(task.latest_run_id.is_none());
        let events = fort_events_for_task(&service, task_id);
        assert!(
            events.iter().any(|event| event.event_type == CanonicalEventType::FortAuthRequired)
        );
    }

    #[tokio::test]
    async fn dispatcher_blocks_fort_task_when_refresh_token_file_is_missing() {
        let temp = tempdir().expect("tempdir");
        let codex_home = temp.path().join("home/.si/codex/profiles/america");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        write_fort_session_state(
            &codex_home,
            "america",
            PersistedSessionState {
                agent_id: "si-codex-america".to_owned(),
                session_id: "fort-session".to_owned(),
                host: "https://fort.example.invalid".to_owned(),
                runtime_host: "https://fort-runtime.example.invalid".to_owned(),
                access_expires_at: (Utc::now() + ChronoDuration::hours(1)).to_rfc3339(),
                refresh_expires_at: (Utc::now() + ChronoDuration::hours(12)).to_rfc3339(),
                ..PersistedSessionState::default()
            },
        );
        fs::remove_file(codex_home.join("fort/refresh.token")).expect("remove refresh token");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-fort-refresh-missing"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": codex_home,
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-fort-refresh-missing"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Check Fort refresh",
                    "instructions": "Use si fort status before continuing",
                    "profile": "america",
                }),
            })
            .await;
        assert!(created.ok);
        let task_id =
            created.result.as_ref().and_then(|task| task["task_id"].as_str()).expect("task id");

        service.reconcile_and_dispatch_once().expect("dispatch fort task");

        let task = service
            .store
            .inspect_task(&TaskId::new(task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(task.status, TaskStatus::Blocked);
        assert_eq!(task.blocked_reason, Some(BlockedReason::AuthRequired));
        let events = fort_events_for_task(&service, task_id);
        assert!(events.iter().any(|event| {
            event.event_type == CanonicalEventType::FortAuthRequired
                && event.data.payload["fort_state"] == json!("refresh_token_missing")
        }));
    }

    #[tokio::test]
    async fn dispatched_tasks_pass_timeout_seconds_to_runtime() {
        let temp = tempdir().expect("tempdir");
        let runtime = FakeRuntime::default();
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:0".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime.clone()),
        )
        .expect("service");

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("create-timeout-task"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Long-running task",
                    "instructions": "stay alive",
                    "profile": "america",
                    "timeout_seconds": 7200,
                }),
            })
            .await;
        assert!(created.ok);

        service.reconcile_and_dispatch_once().expect("dispatch task");

        for _ in 0..50 {
            if runtime.last_timeout_seconds() == Some(7200) {
                break;
            }
            thread::sleep(Duration::from_millis(20));
        }

        assert_eq!(runtime.last_timeout_seconds(), Some(7200));
    }

    #[tokio::test]
    async fn run_submit_turn_passes_timeout_seconds_to_runtime() {
        let temp = tempdir().expect("tempdir");
        let runtime = FakeRuntime::default();
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:0".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(runtime.clone()),
        )
        .expect("service");

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-timeout"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": temp.path().join("home/.si/codex/profiles/america"),
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = session.result.expect("session")["session"]["session_id"]
            .as_str()
            .expect("session id")
            .to_owned();

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-timeout"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Direct timeout run",
                    "instructions": "stay alive",
                    "profile": "america",
                    "session_id": session_id,
                }),
            })
            .await;
        assert!(task.ok);
        let task_id = task.result.expect("task")["task_id"].as_str().expect("task id").to_owned();

        let response = service
            .dispatch_request(GatewayRequest {
                id: json!("run-submit-timeout"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id,
                    "task_id": task_id,
                    "prompt": "stay alive",
                    "timeout_seconds": 1800,
                }),
            })
            .await;
        assert!(response.ok);

        for _ in 0..50 {
            if runtime.last_timeout_seconds() == Some(1800) {
                break;
            }
            thread::sleep(Duration::from_millis(20));
        }

        assert_eq!(runtime.last_timeout_seconds(), Some(1800));
    }

    #[tokio::test]
    async fn run_submit_turn_blocks_fort_task_when_fort_is_unavailable() {
        let temp = tempdir().expect("tempdir");
        let codex_home = temp.path().join("home/.si/codex/profiles/america");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        write_fort_session_state(
            &codex_home,
            "america",
            PersistedSessionState {
                agent_id: "si-codex-america".to_owned(),
                session_id: "fort-session".to_owned(),
                access_expires_at: (Utc::now() + ChronoDuration::hours(1)).to_rfc3339(),
                refresh_expires_at: (Utc::now() + ChronoDuration::hours(12)).to_rfc3339(),
                ..PersistedSessionState::default()
            },
        );

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-fort-unavailable"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": codex_home,
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);
        let session_id = session
            .result
            .as_ref()
            .and_then(|item| item["session"]["session_id"].as_str())
            .expect("session id");

        let task = service
            .dispatch_request(GatewayRequest {
                id: json!("task-fort-unavailable"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Use Fort in a run",
                    "instructions": "Use si fort refresh",
                    "profile": "america",
                    "session_id": session_id,
                }),
            })
            .await;
        assert!(task.ok);
        let task_id =
            task.result.as_ref().and_then(|item| item["task_id"].as_str()).expect("task id");

        let run = service
            .dispatch_request(GatewayRequest {
                id: json!("run-fort-unavailable"),
                method: "run.submit_turn".to_owned(),
                params: json!({
                    "session_id": session_id,
                    "task_id": task_id,
                    "prompt": "Use si fort refresh now",
                }),
            })
            .await;
        assert!(!run.ok);
        assert!(
            run.error
                .as_ref()
                .map(|error| error.message.contains("Fort is unavailable"))
                .unwrap_or(false)
        );

        let task = service
            .store
            .inspect_task(&TaskId::new(task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(task.status, TaskStatus::Blocked);
        assert_eq!(task.blocked_reason, Some(BlockedReason::FortUnavailable));
        assert_eq!(service.store.list_runs().expect("list runs").len(), 0);
        let events = fort_events_for_task(&service, task_id);
        assert!(events.iter().any(|event| event.event_type == CanonicalEventType::FortUnavailable));
    }

    #[tokio::test]
    async fn dispatcher_executes_fort_task_when_fort_is_ready() {
        let temp = tempdir().expect("tempdir");
        let codex_home = temp.path().join("home/.si/codex/profiles/america");
        let service = NucleusService::open_with_runtime(
            NucleusConfig {
                bind_addr: "127.0.0.1:9898".parse().expect("addr"),
                state_dir: temp.path().join("nucleus"),
                auth_token: None,
            },
            Arc::new(FakeRuntime::default()),
        )
        .expect("service");

        write_fort_session_state(
            &codex_home,
            "america",
            PersistedSessionState {
                agent_id: "si-codex-america".to_owned(),
                session_id: "fort-session".to_owned(),
                host: "https://fort.example.invalid".to_owned(),
                runtime_host: "https://fort-runtime.example.invalid".to_owned(),
                access_expires_at: (Utc::now() + ChronoDuration::hours(1)).to_rfc3339(),
                refresh_expires_at: (Utc::now() + ChronoDuration::hours(12)).to_rfc3339(),
                ..PersistedSessionState::default()
            },
        );

        let session = service
            .dispatch_request(GatewayRequest {
                id: json!("session-fort-ready"),
                method: "session.create".to_owned(),
                params: json!({
                    "profile": "america",
                    "home_dir": temp.path().join("home"),
                    "codex_home": codex_home,
                    "workdir": temp.path(),
                }),
            })
            .await;
        assert!(session.ok);

        let created = service
            .dispatch_request(GatewayRequest {
                id: json!("task-fort-ready"),
                method: "task.create".to_owned(),
                params: json!({
                    "title": "Fort-backed task",
                    "instructions": "Use si fort bootstrap and then reply with nucleus-smoke",
                    "profile": "america",
                }),
            })
            .await;
        assert!(created.ok);
        let task_id =
            created.result.as_ref().and_then(|task| task["task_id"].as_str()).expect("task id");

        service.reconcile_and_dispatch_once().expect("dispatch fort-ready task");
        wait_for_task_status(&service, task_id, TaskStatus::Done);

        let task = service
            .store
            .inspect_task(&TaskId::new(task_id).expect("task id"))
            .expect("inspect task")
            .expect("task exists");
        assert_eq!(task.status, TaskStatus::Done);
        let events = fort_events_for_task(&service, task_id);
        assert!(events.iter().any(|event| event.event_type == CanonicalEventType::FortReady));
    }
}
