use std::collections::{BTreeMap, BTreeSet, HashMap, HashSet};
use std::env;
use std::fs::{self, File, OpenOptions};
use std::future::pending;
use std::io::{BufRead, BufReader, Write};
use std::net::SocketAddr;
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
    WorkerRecord,
};
use si_nucleus_runtime::{
    CanonicalEventDraft, NucleusRuntime, RunInputItem, RunTurnSpec, SessionOpenSpec,
    WorkerLaunchSpec, WorkerProbeResult, WorkerRuntimeView, WorkerStartResult,
};
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
const REST_WORKERS_PATH: &str = "/workers";
const REST_WORKER_PATH: &str = "/workers/{worker_id}";
const REST_SESSION_PATH: &str = "/sessions/{session_id}";
const REST_RUN_PATH: &str = "/runs/{run_id}";
const DISPATCH_LOOP_INTERVAL: Duration = Duration::from_millis(200);
const MAX_EVENTS_JSONL_BYTES: u64 = 1024 * 1024;
const DEFAULT_TASK_RETENTION_DAYS: u64 = 30;
const WORKER_RESTART_BACKOFF_BASE: Duration = Duration::from_millis(100);
const MAX_WORKER_RESTART_ATTEMPTS: u32 = 3;

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
}

impl NucleusStore {
    pub fn open(state_dir: PathBuf) -> Result<Self> {
        let paths = NucleusPaths::new(state_dir);
        paths.ensure_layout()?;
        let last_seq = load_last_event_seq(&paths.events_path)?;
        let store =
            Self { paths, next_event_seq: AtomicU64::new(last_seq), write_lock: Mutex::new(()) };
        let mut warnings = store.rebuild_markdown_projections()?;
        warnings.extend(store.scan_persisted_state_health()?);
        for warning in warnings {
            store.append_system_warning(
                "isolated malformed persisted object during startup recovery",
                warning,
            )?;
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
                self.finish_run_locked(
                    &primary,
                    RunStatus::Failed,
                    None,
                    TaskStatus::Failed,
                    None,
                    &mut events,
                )?;
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
        let existed = path.exists();
        write_json_atomic(&path, &profile)?;
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

fn derive_task_profile(
    task: &TaskRecord,
    sessions: &HashMap<SessionId, SessionRecord>,
    workers: &HashMap<WorkerId, WorkerRecord>,
    available_profiles: &[ProfileRecord],
) -> Option<ProfileName> {
    task.profile.clone().or_else(|| {
        task.session_id
            .as_ref()
            .and_then(|session_id| sessions.get(session_id))
            .and_then(|session| {
                session.profile.clone().or_else(|| {
                    workers.get(&session.worker_id).map(|worker| worker.profile.clone())
                })
            })
            .or_else(|| choose_single_profile_candidate(sessions, workers, available_profiles))
    })
}

fn choose_single_profile_candidate(
    sessions: &HashMap<SessionId, SessionRecord>,
    workers: &HashMap<WorkerId, WorkerRecord>,
    available_profiles: &[ProfileRecord],
) -> Option<ProfileName> {
    let mut candidates = BTreeSet::<ProfileName>::new();
    for worker in workers.values() {
        candidates.insert(worker.profile.clone());
    }
    for session in sessions.values() {
        if let Some(profile) = session.profile.as_ref() {
            candidates.insert(profile.clone());
        }
    }
    for profile in available_profiles {
        candidates.insert(profile.profile.clone());
    }
    if candidates.len() == 1 { candidates.into_iter().next() } else { None }
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
    home_dir.join(".si").join("codex").join("profiles").join(profile.as_str()).join("fort")
}

fn fort_profile_dir(worker: &WorkerRecord, profile: &ProfileName) -> PathBuf {
    let codex_home = worker.codex_home.trim();
    if !codex_home.is_empty() {
        return PathBuf::from(codex_home).join("fort");
    }
    if let Some(home_dir) =
        worker.home_dir.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        return default_fort_profile_dir(Path::new(home_dir), profile);
    }
    default_codex_home_dir(profile.as_str()).join("fort")
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
            "Fort authentication is required for profile {}: session state is missing",
            profile
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
                "Fort authentication is required for profile {}: session state is missing",
                profile
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
            let message = format!("Fort is unavailable for profile {}: {}", profile, error);
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
        let message = format!("Fort is unavailable for profile {}: {}", profile, error);
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
            let message = format!("Fort is unavailable for profile {}: {}", profile, error);
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
            (FortCapabilityState::Ready, format!("Fort is ready for profile {}", profile))
        }
        SessionState::BootstrapRequired | SessionState::Revoked { .. } | SessionState::Closed => (
            FortCapabilityState::AuthRequired,
            format!("Fort authentication is required for profile {}", profile),
        ),
        SessionState::TeardownPending(_) => (
            FortCapabilityState::Unavailable,
            format!(
                "Fort is unavailable for profile {}: session teardown is still pending",
                profile
            ),
        ),
    };
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

fn load_canonical_events(path: &Path) -> Result<Vec<CanonicalEvent>> {
    let mut events = Vec::new();
    for log_path in canonical_event_log_paths(path)? {
        events.extend(load_canonical_events_from_path(&log_path)?);
    }
    Ok(events)
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

fn load_last_event_seq(path: &Path) -> Result<u64> {
    let mut last_seq = 0_u64;
    for log_path in canonical_event_log_paths(path)? {
        for event in load_canonical_events_from_path(&log_path)? {
            last_seq = last_seq.max(event.seq);
        }
    }
    Ok(last_seq)
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
    matches!(status, TaskStatus::Done | TaskStatus::Failed)
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
        ws_url: format!("ws://{}{}", bind_addr, WS_PATH),
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
}

impl NucleusService {
    pub fn open(config: NucleusConfig) -> Result<Self> {
        Self::open_without_runtime(config)
    }

    pub fn open_without_runtime(config: NucleusConfig) -> Result<Self> {
        let store = Arc::new(NucleusStore::open(config.state_dir.clone())?);
        persist_gateway_metadata(store.paths(), config.bind_addr)?;
        let (events, _) = broadcast::channel(256);
        Ok(Self {
            config,
            store,
            events,
            runtime: None,
            background_started: Arc::new(AtomicBool::new(false)),
            worker_restart_state: Arc::new(Mutex::new(HashMap::new())),
        })
    }

    pub fn open_with_runtime(
        config: NucleusConfig,
        runtime: Arc<dyn NucleusRuntime>,
    ) -> Result<Self> {
        let store = Arc::new(NucleusStore::open(config.state_dir.clone())?);
        persist_gateway_metadata(store.paths(), config.bind_addr)?;
        let (events, _) = broadcast::channel(256);
        Ok(Self {
            config,
            store,
            events,
            runtime: Some(runtime),
            background_started: Arc::new(AtomicBool::new(false)),
            worker_restart_state: Arc::new(Mutex::new(HashMap::new())),
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
            .route(REST_WORKERS_PATH, get(rest_list_workers_handler))
            .route(REST_WORKER_PATH, get(rest_inspect_worker_handler))
            .route(REST_SESSION_PATH, get(rest_show_session_handler))
            .route(REST_RUN_PATH, get(rest_inspect_run_handler))
            .with_state(Arc::new(self))
    }

    pub async fn serve(self) -> Result<()> {
        self.initialize_runtime_loops()?;
        let bind_addr = self.config.bind_addr;
        let listener =
            TcpListener::bind(bind_addr).await.with_context(|| format!("bind {}", bind_addr))?;
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
                if let Ok(event) = self.store.append_system_warning(
                    "nucleus background loop iteration failed",
                    json!({ "error": error.to_string() }),
                ) {
                    let _ = self.events.send(event);
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
        let events = load_canonical_events(&self.store.paths().events_path)?;
        for mut rule in
            load_json_records_from_dir::<HookRuleRecord>(&self.store.paths().hook_state_dir)?
        {
            if let Err(error) = self.process_single_hook_rule(&mut rule, &events) {
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
            self.write_hook_rule(rule)?;
        }
        Ok(())
    }

    fn write_cron_rule(&self, rule: &CronRuleRecord) -> Result<()> {
        write_json_atomic(&self.store.paths().cron_rule_path(&rule.name), rule)
    }

    fn write_hook_rule(&self, rule: &HookRuleRecord) -> Result<()> {
        write_json_atomic(&self.store.paths().hook_rule_path(&rule.name), rule)
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

    fn dispatch_queued_tasks(&self) -> Result<()> {
        let Some(runtime) = self.runtime.as_ref() else {
            return Ok(());
        };
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
        let profiles = self.store.list_profile_records()?;
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
            }
            let Some(profile) = derive_task_profile(&task, &sessions, &workers, &profiles) else {
                continue;
            };
            if !selected_profiles.insert(profile.clone()) {
                continue;
            }
            self.try_dispatch_task(runtime.as_ref(), task, profile)?;
        }
        Ok(())
    }

    fn try_dispatch_task(
        &self,
        runtime: &dyn NucleusRuntime,
        task: TaskRecord,
        profile: ProfileName,
    ) -> Result<()> {
        let Some(session) = self.ensure_dispatch_session(runtime, &task, &profile)? else {
            return Ok(());
        };
        let worker = self
            .store
            .inspect_worker(&session.worker_id)?
            .ok_or_else(|| anyhow!("worker not found"))?;
        if self.enforce_fort_capability(&task, &session, &worker, &profile, None)?.is_some() {
            return Ok(());
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
            input: vec![RunInputItem::Text { text: task.instructions }],
        };
        self.spawn_run_execution(runtime, run_spec);
        Ok(())
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
            let Some(session) = self.store.inspect_session(session_id)? else {
                if let Some(event) = self.store.mark_task_blocked(
                    &task.task_id,
                    None,
                    Some(session_id.clone()),
                    Some(profile.clone()),
                    BlockedReason::SessionBroken,
                    "task references a missing session",
                )? {
                    let _ = self.events.send(event);
                }
                return Ok(None);
            };
            if matches!(
                session.lifecycle_state,
                SessionLifecycleState::Broken | SessionLifecycleState::Closed
            ) {
                if let Some(event) = self.store.mark_task_blocked(
                    &task.task_id,
                    Some(session.worker_id.clone()),
                    Some(session.session_id.clone()),
                    Some(profile.clone()),
                    BlockedReason::SessionBroken,
                    "task is queued behind a non-reusable session",
                )? {
                    let _ = self.events.send(event);
                }
                return Ok(None);
            }
            let worker = self.ensure_worker_started(
                runtime,
                profile,
                EnsureWorkerRequest {
                    requested_worker_id: Some(session.worker_id.as_str()),
                    home_dir: None,
                    codex_home: None,
                    workdir: session.workdir.as_ref().map(PathBuf::from),
                    env: None,
                },
            )?;
            let Some(thread_id) = session.app_server_thread_id.clone() else {
                if let Some(event) = self.store.mark_task_blocked(
                    &task.task_id,
                    Some(worker.worker.worker_id.clone()),
                    Some(session.session_id.clone()),
                    Some(profile.clone()),
                    BlockedReason::SessionBroken,
                    "session is missing an app-server thread id",
                )? {
                    let _ = self.events.send(event);
                }
                return Ok(None);
            };
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
                    if let Some(event) =
                        self.store.mark_session_broken(&session.session_id, &error.to_string())?
                    {
                        let _ = self.events.send(event);
                    }
                    if let Some(event) = self.store.mark_task_blocked(
                        &task.task_id,
                        Some(worker.worker.worker_id.clone()),
                        Some(session.session_id.clone()),
                        Some(profile.clone()),
                        BlockedReason::SessionBroken,
                        &error.to_string(),
                    )? {
                        let _ = self.events.send(event);
                    }
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
            if is_active_run_status(run.status) {
                let session = self
                    .store
                    .inspect_session(&run.session_id)?
                    .ok_or_else(|| anyhow!("session not found"))?;
                let profile = task.profile.clone().or_else(|| session.profile.clone());
                if let Some(turn_id) = run.app_server_turn_id.as_deref() {
                    let runtime =
                        self.runtime.as_ref().ok_or_else(|| anyhow!("runtime unavailable"))?;
                    let thread_id = session
                        .app_server_thread_id
                        .clone()
                        .ok_or_else(|| anyhow!("session missing app-server thread id"))?;
                    runtime.interrupt_turn(&session.worker_id, &thread_id, turn_id)?;
                    let task = self
                        .store
                        .inspect_task(task_id)?
                        .ok_or_else(|| anyhow!("task not found"))?;
                    let run = self.store.inspect_run(&run.run_id)?;
                    return Ok(TaskCancelResultView { task, run, cancellation_requested: true });
                }

                let events = self.store.apply_runtime_event(CanonicalEventDraft {
                    event_type: CanonicalEventType::RunCancelled,
                    source: CanonicalEventSource::Nucleus,
                    data: EventDataEnvelope {
                        task_id: Some(task.task_id.clone()),
                        worker_id: Some(session.worker_id.clone()),
                        session_id: Some(session.session_id.clone()),
                        run_id: Some(run.run_id.clone()),
                        profile,
                        payload: json!({
                            "message": "run cancelled before the worker reported turn start",
                        }),
                    },
                })?;
                for event in events {
                    let _ = self.events.send(event);
                }
                let task =
                    self.store.inspect_task(task_id)?.ok_or_else(|| anyhow!("task not found"))?;
                let run = self.store.inspect_run(&run.run_id)?;
                return Ok(TaskCancelResultView { task, run, cancellation_requested: false });
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
                            "error": error.to_string(),
                        }),
                    },
                };
                if let Ok(appended) = store.apply_runtime_event(failure) {
                    for event in appended {
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

    fn authorize_request_method(&self, method: &str, bearer_token: Option<&str>) -> Result<()> {
        if !self.requires_gateway_auth() || is_read_gateway_method(method) {
            return Ok(());
        }
        let expected = self.config.auth_token.as_deref().ok_or_else(|| {
            anyhow!("unauthorized: SI_NUCLEUS_AUTH_TOKEN must be set when bound beyond loopback")
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
        !self.config.bind_addr.ip().is_loopback()
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
                let events = self.store.create_task(CreateTaskInput {
                    title: params.title,
                    instructions: params.instructions,
                    source: params.source,
                    profile,
                    session_id,
                    max_retries: params.max_retries,
                    timeout_seconds: params.timeout_seconds,
                })?;
                let task = self
                    .store
                    .inspect_task(
                        &events[0]
                            .data
                            .task_id
                            .clone()
                            .ok_or_else(|| anyhow!("task id missing after create"))?,
                    )?
                    .ok_or_else(|| anyhow!("task missing after create"))?;
                for event in events {
                    let _ = self.events.send(event);
                }
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
                let session = self
                    .store
                    .inspect_session(&session_id)?
                    .ok_or_else(|| anyhow!("session not found"))?;
                let worker = self
                    .store
                    .inspect_worker(&session.worker_id)?
                    .ok_or_else(|| anyhow!("worker not found"))?;
                let profile = worker.profile.clone();
                let task_id = TaskId::new(params.task_id)?;
                let task =
                    self.store.inspect_task(&task_id)?.ok_or_else(|| anyhow!("task not found"))?;
                if let Some(task_profile) = task.profile.as_ref() {
                    if task_profile != &profile {
                        anyhow::bail!("task profile does not match session profile");
                    }
                }
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
                let run = RunRecord::new(RunId::generate(), task_id.clone(), session_id.clone());
                let run = self.store.claim_run_for_task(run)?;
                let run_spec = RunTurnSpec {
                    run_id: run.run_id.clone(),
                    task_id: Some(task_id),
                    worker_id: worker.worker_id.clone(),
                    session_id: session_id.clone(),
                    profile,
                    thread_id: session
                        .app_server_thread_id
                        .clone()
                        .ok_or_else(|| anyhow!("session missing app-server thread id"))?,
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
                let runtime =
                    self.runtime.as_ref().ok_or_else(|| anyhow!("runtime unavailable"))?;
                let params: RunCancelParams =
                    serde_json::from_value(params).context("parse run.cancel params")?;
                let run_id = RunId::new(params.run_id)?;
                let run =
                    self.store.inspect_run(&run_id)?.ok_or_else(|| anyhow!("run not found"))?;
                let session = self
                    .store
                    .inspect_session(&run.session_id)?
                    .ok_or_else(|| anyhow!("session not found"))?;
                let thread_id = session
                    .app_server_thread_id
                    .clone()
                    .ok_or_else(|| anyhow!("session missing app-server thread id"))?;
                let turn_id = run
                    .app_server_turn_id
                    .clone()
                    .ok_or_else(|| anyhow!("run has not started a turn yet"))?;
                runtime.interrupt_turn(&session.worker_id, &thread_id, &turn_id)?;
                Ok(serde_json::to_value(run)?)
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
        let home_dir = request
            .home_dir
            .or_else(|| {
                existing.as_ref().and_then(|worker| worker.home_dir.as_ref().map(PathBuf::from))
            })
            .unwrap_or_else(default_home_dir);
        let codex_home = request
            .codex_home
            .or_else(|| existing.as_ref().map(|worker| PathBuf::from(worker.codex_home.clone())))
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

struct BlockedRunReconciliation {
    worker_id: Option<WorkerId>,
    session_id: Option<SessionId>,
    profile: Option<ProfileName>,
    blocked_reason: BlockedReason,
    message: String,
    mark_session_broken: bool,
}

struct EnsureWorkerRequest<'a> {
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

fn is_read_gateway_method(method: &str) -> bool {
    matches!(
        method,
        "nucleus.status"
            | "task.list"
            | "task.inspect"
            | "profile.list"
            | "worker.list"
            | "worker.inspect"
            | "session.list"
            | "session.show"
            | "run.inspect"
            | "events.subscribe"
    )
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

fn openapi_document(config: &NucleusConfig) -> Value {
    json!({
        "openapi": "3.1.0",
        "info": {
            "title": "SI Nucleus REST API",
            "version": env!("CARGO_PKG_VERSION"),
            "description": "Bounded external integration API over the canonical SI Nucleus task, worker, session, and run model."
        },
        "servers": [
            { "url": format!("http://{}", config.bind_addr) }
        ],
        "paths": {
            "/status": {
                "get": {
                    "summary": "Inspect Nucleus status",
                    "description": "Read the current Nucleus status projection, including bind address, state directory, and durable object counts.",
                    "x-si-purpose": "Use this for bounded external health and topology inspection without opening the websocket control plane.",
                    "responses": {
                        "200": {
                            "description": "Current nucleus status.",
                            "content": { "application/json": { "schema": schema_ref("NucleusStatusView") } }
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
                        }
                    }
                }
            },
            "/tasks": {
                "get": {
                    "summary": "List tasks",
                    "description": "List durable tasks from the same source of truth used by the websocket gateway and CLI.",
                    "x-si-purpose": "Use this for bounded task inspection and polling from external tools such as GPT Actions.",
                    "responses": {
                        "200": {
                            "description": "All durable tasks.",
                            "content": {
                                "application/json": {
                                    "schema": { "type": "array", "items": schema_ref("TaskRecord") }
                                }
                            }
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
                        }
                    }
                },
                "post": {
                    "summary": "Create a task",
                    "description": "Create a durable task through Nucleus so it can be routed, executed, and observed through the canonical control plane.",
                    "x-si-purpose": "Use this to create bounded external work without bypassing Nucleus task intake rules.",
                    "security": [{ "bearerAuth": [] }],
                    "requestBody": {
                        "required": true,
                        "content": {
                            "application/json": {
                                "schema": schema_ref("TaskCreateParams")
                            }
                        }
                    },
                    "responses": {
                        "201": {
                            "description": "Created task.",
                            "content": { "application/json": { "schema": schema_ref("TaskRecord") } }
                        },
                        "401": {
                            "description": "Bearer token missing or invalid for a non-loopback write request.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
                        },
                        "400": {
                            "description": "Invalid request.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
                        }
                    }
                }
            },
            "/tasks/{task_id}": {
                "get": {
                    "summary": "Inspect one task",
                    "description": "Read one durable task projection by task id.",
                    "x-si-purpose": "Use this to inspect bounded task state from external tooling.",
                    "parameters": [
                        {
                            "name": "task_id",
                            "in": "path",
                            "required": true,
                            "schema": { "type": "string" },
                            "description": "Canonical SI task id."
                        }
                    ],
                    "responses": {
                        "200": {
                            "description": "Task record.",
                            "content": { "application/json": { "schema": schema_ref("TaskRecord") } }
                        },
                        "404": {
                            "description": "Task not found.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
                        }
                    }
                }
            },
            "/tasks/{task_id}/cancel": {
                "post": {
                    "summary": "Cancel one task",
                    "description": "Request cancellation for a task through Nucleus. Queued tasks cancel immediately; active runs are interrupted through the runtime when needed.",
                    "x-si-purpose": "Use this for bounded external cancellation requests and then re-read the task or run to observe final state.",
                    "security": [{ "bearerAuth": [] }],
                    "parameters": [
                        {
                            "name": "task_id",
                            "in": "path",
                            "required": true,
                            "schema": { "type": "string" },
                            "description": "Canonical SI task id."
                        }
                    ],
                    "responses": {
                        "200": {
                            "description": "Cancellation result.",
                            "content": { "application/json": { "schema": schema_ref("TaskCancelResultView") } }
                        },
                        "401": {
                            "description": "Bearer token missing or invalid for a non-loopback write request.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
                        },
                        "404": {
                            "description": "Task not found.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
                        },
                        "503": {
                            "description": "Runtime unavailable for active-run cancellation.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
                        }
                    }
                }
            },
            "/workers": {
                "get": {
                    "summary": "List workers",
                    "description": "List durable worker records tracked by Nucleus.",
                    "x-si-purpose": "Use this for bounded worker inspection without relying on tmux or direct runtime internals.",
                    "responses": {
                        "200": {
                            "description": "All durable workers.",
                            "content": {
                                "application/json": {
                                    "schema": { "type": "array", "items": schema_ref("WorkerRecord") }
                                }
                            }
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
                        }
                    }
                }
            },
            "/workers/{worker_id}": {
                "get": {
                    "summary": "Inspect one worker",
                    "description": "Read one worker projection, including persisted runtime view when available.",
                    "x-si-purpose": "Use this to inspect worker assignment and runtime attachment through the Nucleus model.",
                    "parameters": [
                        {
                            "name": "worker_id",
                            "in": "path",
                            "required": true,
                            "schema": { "type": "string" },
                            "description": "Canonical SI worker id."
                        }
                    ],
                    "responses": {
                        "200": {
                            "description": "Worker inspect view.",
                            "content": { "application/json": { "schema": schema_ref("WorkerInspectView") } }
                        },
                        "404": {
                            "description": "Worker not found.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
                        }
                    }
                }
            },
            "/sessions/{session_id}": {
                "get": {
                    "summary": "Inspect one session",
                    "description": "Read one durable session projection by session id.",
                    "x-si-purpose": "Use this to inspect worker/session binding and reusable thread identity from external tooling.",
                    "parameters": [
                        {
                            "name": "session_id",
                            "in": "path",
                            "required": true,
                            "schema": { "type": "string" },
                            "description": "Canonical SI session id."
                        }
                    ],
                    "responses": {
                        "200": {
                            "description": "Session record.",
                            "content": { "application/json": { "schema": schema_ref("SessionRecord") } }
                        },
                        "404": {
                            "description": "Session not found.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
                        }
                    }
                }
            },
            "/runs/{run_id}": {
                "get": {
                    "summary": "Inspect one run",
                    "description": "Read one durable run projection by run id.",
                    "x-si-purpose": "Use this to inspect bounded run state from external tools without subscribing to websocket events.",
                    "parameters": [
                        {
                            "name": "run_id",
                            "in": "path",
                            "required": true,
                            "schema": { "type": "string" },
                            "description": "Canonical SI run id."
                        }
                    ],
                    "responses": {
                        "200": {
                            "description": "Run record.",
                            "content": { "application/json": { "schema": schema_ref("RunRecord") } }
                        },
                        "404": {
                            "description": "Run not found.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
                        }
                    }
                }
            },
            "/openapi.json": {
                "get": {
                    "summary": "Fetch the OpenAPI document",
                    "description": "Read the OpenAPI-compatible REST description for bounded external integrations.",
                    "x-si-purpose": "Use this to bootstrap GPT Actions or other external tool clients against the bounded REST surface.",
                    "responses": {
                        "200": {
                            "description": "OpenAPI document.",
                            "content": { "application/json": { "schema": { "type": "object" } } }
                        },
                        "500": {
                            "description": "Request failed.",
                            "content": { "application/json": { "schema": schema_ref("RestErrorEnvelope") } }
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
                    "bearerFormat": "opaque token"
                }
            },
            "schemas": {
                "RestErrorEnvelope": {
                    "type": "object",
                    "required": ["error"],
                    "properties": {
                        "error": {
                            "type": "object",
                            "required": ["code", "message"],
                            "properties": {
                                "code": { "type": "string" },
                                "message": { "type": "string" },
                                "details": {}
                            }
                        }
                    }
                },
                "NucleusStatusView": {
                    "type": "object",
                    "required": ["version", "bind_addr", "ws_url", "state_dir", "task_count", "worker_count", "session_count", "run_count", "next_event_seq"],
                    "properties": {
                        "version": { "type": "string" },
                        "bind_addr": { "type": "string" },
                        "ws_url": { "type": "string" },
                        "state_dir": { "type": "string" },
                        "task_count": { "type": "integer", "minimum": 0 },
                        "worker_count": { "type": "integer", "minimum": 0 },
                        "session_count": { "type": "integer", "minimum": 0 },
                        "run_count": { "type": "integer", "minimum": 0 },
                        "next_event_seq": { "type": "integer", "minimum": 0 }
                    }
                },
                "TaskCreateParams": {
                    "type": "object",
                    "required": ["title", "instructions"],
                    "properties": {
                        "title": { "type": "string" },
                        "instructions": { "type": "string" },
                        "source": { "type": "string", "enum": ["cli", "websocket", "cron", "hook", "system"] },
                        "profile": { "type": ["string", "null"] },
                        "session_id": { "type": ["string", "null"] },
                        "max_retries": { "type": ["integer", "null"], "minimum": 0 },
                        "timeout_seconds": { "type": ["integer", "null"], "minimum": 0 }
                    }
                },
                "TaskRecord": {
                    "type": "object",
                    "required": ["task_id", "source", "title", "instructions", "status", "created_at", "updated_at"],
                    "properties": {
                        "task_id": { "type": "string" },
                        "source": { "type": "string", "enum": ["cli", "websocket", "cron", "hook", "system"] },
                        "title": { "type": "string" },
                        "instructions": { "type": "string" },
                        "status": { "type": "string", "enum": ["queued", "running", "blocked", "done", "failed", "cancelled"] },
                        "profile": { "type": ["string", "null"] },
                        "session_id": { "type": ["string", "null"] },
                        "latest_run_id": { "type": ["string", "null"] },
                        "checkpoint_summary": { "type": ["string", "null"] },
                        "checkpoint_at": { "type": ["string", "null"], "format": "date-time" },
                        "checkpoint_seq": { "type": ["integer", "null"], "minimum": 0 },
                        "parent_task_id": { "type": ["string", "null"] },
                        "producer_rule_name": { "type": ["string", "null"] },
                        "producer_dedup_key": { "type": ["string", "null"] },
                        "blocked_reason": {
                            "type": ["string", "null"],
                            "enum": [null, "auth_required", "worker_unavailable", "session_broken", "producer_error", "operator_hold", "fort_unavailable"]
                        },
                        "created_at": { "type": "string", "format": "date-time" },
                        "updated_at": { "type": "string", "format": "date-time" },
                        "max_retries": { "type": ["integer", "null"], "minimum": 0 },
                        "timeout_seconds": { "type": ["integer", "null"], "minimum": 0 }
                    }
                },
                "WorkerRecord": {
                    "type": "object",
                    "required": ["worker_id", "profile", "codex_home", "status"],
                    "properties": {
                        "worker_id": { "type": "string" },
                        "profile": { "type": "string" },
                        "home_dir": { "type": ["string", "null"] },
                        "codex_home": { "type": "string" },
                        "workdir": { "type": ["string", "null"] },
                        "extra_env": { "type": "object", "additionalProperties": { "type": "string" } },
                        "status": { "type": "string", "enum": ["starting", "ready", "degraded", "failed", "stopped"] },
                        "capability_version": { "type": ["string", "null"] },
                        "last_heartbeat_at": { "type": ["string", "null"], "format": "date-time" },
                        "effective_account_state": { "type": ["string", "null"] }
                    }
                },
                "WorkerRuntimeView": {
                    "type": "object",
                    "required": ["worker_id", "runtime_name", "pid", "started_at", "checked_at"],
                    "properties": {
                        "worker_id": { "type": "string" },
                        "runtime_name": { "type": "string" },
                        "pid": { "type": "integer", "minimum": 0 },
                        "started_at": { "type": "string", "format": "date-time" },
                        "checked_at": { "type": "string", "format": "date-time" }
                    }
                },
                "WorkerInspectView": {
                    "type": "object",
                    "required": ["worker"],
                    "properties": {
                        "worker": schema_ref("WorkerRecord"),
                        "runtime": {
                            "anyOf": [schema_ref("WorkerRuntimeView"), { "type": "null" }]
                        }
                    }
                },
                "SessionRecord": {
                    "type": "object",
                    "required": ["session_id", "worker_id", "lifecycle_state", "created_at", "updated_at"],
                    "properties": {
                        "session_id": { "type": "string" },
                        "worker_id": { "type": "string" },
                        "profile": { "type": ["string", "null"] },
                        "app_server_thread_id": { "type": ["string", "null"] },
                        "workdir": { "type": ["string", "null"] },
                        "lifecycle_state": { "type": "string", "enum": ["opening", "ready", "busy", "broken", "closed"] },
                        "summary_state": { "type": ["string", "null"] },
                        "created_at": { "type": "string", "format": "date-time" },
                        "updated_at": { "type": "string", "format": "date-time" }
                    }
                },
                "RunRecord": {
                    "type": "object",
                    "required": ["run_id", "task_id", "session_id", "status"],
                    "properties": {
                        "run_id": { "type": "string" },
                        "task_id": { "type": "string" },
                        "session_id": { "type": "string" },
                        "status": { "type": "string", "enum": ["queued", "running", "blocked", "completed", "failed", "cancelled"] },
                        "started_at": { "type": ["string", "null"], "format": "date-time" },
                        "ended_at": { "type": ["string", "null"], "format": "date-time" },
                        "parent_run_id": { "type": ["string", "null"] },
                        "app_server_turn_id": { "type": ["string", "null"] }
                    }
                },
                "TaskCancelResultView": {
                    "type": "object",
                    "required": ["task", "cancellation_requested"],
                    "properties": {
                        "task": schema_ref("TaskRecord"),
                        "run": {
                            "anyOf": [schema_ref("RunRecord"), { "type": "null" }]
                        },
                        "cancellation_requested": { "type": "boolean" }
                    }
                }
            }
        }
    })
}

async fn rest_openapi_handler(State(service): State<Arc<NucleusService>>) -> impl IntoResponse {
    (StatusCode::OK, Json(openapi_document(&service.config)))
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
    } else if message.contains("parse ") {
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

fn default_workdir() -> PathBuf {
    env::current_dir().unwrap_or_else(|_| PathBuf::from("."))
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
}

#[derive(Clone, Debug, Deserialize)]
struct RunInspectParams {
    run_id: String,
}

#[derive(Clone, Debug, Deserialize)]
struct RunCancelParams {
    run_id: String,
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
        CronRuleRecord, CronScheduleKind, GatewayRequest, HookRuleRecord,
        MAX_WORKER_RESTART_ATTEMPTS, NucleusConfig, NucleusPaths, NucleusService, NucleusStore,
        CreateTaskInput, append_jsonl_with_rotation, cron_due_key, current_persisted_version,
        hook_event_key, load_canonical_events, load_last_event_seq, write_json_atomic,
    };
    use anyhow::Result;
    use axum::body::{Body, to_bytes};
    use axum::http::{Request, StatusCode};
    use chrono::{Duration as ChronoDuration, Utc};
    use serde_json::json;
    use si_nucleus_core::{
        BlockedReason, CanonicalEvent, CanonicalEventSource, CanonicalEventType, EventDataEnvelope,
        EventId, ProfileName, RunId, RunRecord, RunStatus, SessionId, SessionLifecycleState,
        TaskId, TaskRecord, TaskSource, TaskStatus, WorkerId, WorkerStatus,
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

        fn fail_next_starts(&self, count: usize) {
            self.state.lock().expect("state").remaining_start_failures = count;
        }

        fn start_call_count(&self) -> usize {
            self.state.lock().expect("state").start_calls
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
            let (run_delay, fail_execute) = {
                let state = self.state.lock().expect("state");
                (state.run_delay, state.fail_execute)
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

        let reopened = NucleusService::open(config).expect("reopen");
        let status = reopened.status().expect("status");
        assert_eq!(status.task_count, 1);
        assert_eq!(status.next_event_seq, 2);
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
    async fn service_open_writes_gateway_metadata_with_version_and_bind_address() {
        let temp = tempdir().expect("tempdir");
        let state_dir = temp.path().join("nucleus");
        let _service = NucleusService::open(NucleusConfig {
            bind_addr: "127.0.0.1:9898".parse().expect("addr"),
            state_dir: state_dir.clone(),
            auth_token: None,
        })
        .expect("service");

        let raw = fs::read(state_dir.join("gateway").join("metadata.json")).expect("read metadata");
        let metadata: serde_json::Value = serde_json::from_slice(&raw).expect("parse metadata");
        assert_eq!(metadata["version"], json!(env!("CARGO_PKG_VERSION")));
        assert_eq!(metadata["bind_addr"], json!("127.0.0.1:9898"));
        assert_eq!(metadata["ws_url"], json!("ws://127.0.0.1:9898/ws"));
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
            .oneshot(Request::builder().uri("/openapi.json").body(Body::empty()).expect("request"))
            .await
            .expect("openapi response");
        assert_eq!(response.status(), StatusCode::OK);
        let body = response_json(response).await;
        assert_eq!(body["openapi"], json!("3.1.0"));
        assert_eq!(body["components"]["securitySchemes"]["bearerAuth"]["scheme"], json!("bearer"));
        assert_eq!(body["components"]["securitySchemes"]["bearerAuth"]["bearerFormat"], json!("opaque token"));
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
                operation["x-si-purpose"]
                    .as_str()
                    .map(|value| !value.is_empty())
                    .unwrap_or(false),
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
                    || matches!((path, method), ("/status", "get") | ("/tasks", "get") | ("/workers", "get") | ("/openapi.json", "get")),
                "missing request surface for {method} {path}"
            );
        }
        for (path, method) in [("/tasks", "post")] {
            let operation = &body["paths"][path][method];
            assert_eq!(operation["requestBody"]["required"], json!(true), "requestBody must be required for {method} {path}");
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
        assert!(body["components"]["schemas"]["RestErrorEnvelope"]["properties"]["error"]["properties"]["details"].is_object());
        assert_eq!(
            body["paths"]["/status"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/NucleusStatusView")
        );
        assert_eq!(
            body["paths"]["/status"]["get"]["responses"]["500"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/tasks"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]["type"],
            json!("array")
        );
        assert_eq!(
            body["paths"]["/tasks"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]["items"]["$ref"],
            json!("#/components/schemas/TaskRecord")
        );
        assert_eq!(
            body["paths"]["/tasks"]["post"]["requestBody"]["content"]["application/json"]["schema"]
                ["$ref"],
            json!("#/components/schemas/TaskCreateParams")
        );
        assert_eq!(
            body["paths"]["/tasks"]["post"]["responses"]["201"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/TaskRecord")
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}"]["get"]["parameters"][0]["schema"]["type"],
            json!("string")
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/TaskRecord")
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}"]["get"]["responses"]["404"]["content"]["application/json"]["schema"]["$ref"],
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
            body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["200"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/TaskCancelResultView")
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["404"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["503"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/workers"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]["type"],
            json!("array")
        );
        assert_eq!(
            body["paths"]["/workers"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]["items"]["$ref"],
            json!("#/components/schemas/WorkerRecord")
        );
        assert_eq!(
            body["paths"]["/workers/{worker_id}"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/WorkerInspectView")
        );
        assert_eq!(
            body["paths"]["/workers/{worker_id}"]["get"]["parameters"][0]["schema"]["type"],
            json!("string")
        );
        assert_eq!(
            body["paths"]["/workers/{worker_id}"]["get"]["responses"]["404"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/sessions/{session_id}"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/SessionRecord")
        );
        assert_eq!(
            body["paths"]["/sessions/{session_id}"]["get"]["parameters"][0]["schema"]["type"],
            json!("string")
        );
        assert_eq!(
            body["paths"]["/sessions/{session_id}"]["get"]["responses"]["404"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/runs/{run_id}"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/RunRecord")
        );
        assert_eq!(
            body["paths"]["/runs/{run_id}"]["get"]["parameters"][0]["schema"]["type"],
            json!("string")
        );
        assert_eq!(
            body["paths"]["/runs/{run_id}"]["get"]["responses"]["404"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(body["paths"]["/tasks"]["post"]["security"][0]["bearerAuth"], json!([]));
        assert_eq!(body["paths"]["/tasks/{task_id}/cancel"]["post"]["security"][0]["bearerAuth"], json!([]));
        assert_eq!(
            body["paths"]["/tasks"]["post"]["responses"]["401"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["401"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/tasks"]["get"]["responses"]["500"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/tasks"]["post"]["responses"]["500"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}"]["get"]["responses"]["500"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/tasks/{task_id}/cancel"]["post"]["responses"]["500"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/workers"]["get"]["responses"]["500"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/workers/{worker_id}"]["get"]["responses"]["500"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/sessions/{session_id}"]["get"]["responses"]["500"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/runs/{run_id}"]["get"]["responses"]["500"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/openapi.json"]["get"]["responses"]["500"]["content"]["application/json"]["schema"]["$ref"],
            json!("#/components/schemas/RestErrorEnvelope")
        );
        assert_eq!(
            body["paths"]["/openapi.json"]["get"]["responses"]["200"]["content"]["application/json"]["schema"]["type"],
            json!("object")
        );
        assert!(body["paths"]["/status"]["get"]["security"].is_null());
        assert!(body["paths"]["/tasks"]["get"]["security"].is_null());
        assert!(body["paths"]["/tasks/{task_id}"]["get"]["security"].is_null());
        assert!(body["paths"]["/workers"]["get"]["security"].is_null());
        assert!(body["paths"]["/workers/{worker_id}"]["get"]["security"].is_null());
        assert!(body["paths"]["/sessions/{session_id}"]["get"]["security"].is_null());
        assert!(body["paths"]["/runs/{run_id}"]["get"]["security"].is_null());
        assert!(body["paths"]["/openapi.json"]["get"]["security"].is_null());
        assert_eq!(
            body["components"]["schemas"]["TaskCancelResultView"]["required"],
            json!(["task", "cancellation_requested"])
        );
        assert_eq!(
            body["components"]["schemas"]["WorkerInspectView"]["properties"]["worker"]["$ref"],
            json!("#/components/schemas/WorkerRecord")
        );
    }

    #[tokio::test]
    async fn rest_write_requests_require_bearer_token_when_bound_beyond_loopback() {
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
        assert_eq!(status_response.status(), StatusCode::OK);
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
    async fn rest_read_requests_remain_available_without_bearer_token_when_bound_beyond_loopback() {
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
            .oneshot(Request::builder().uri("/tasks").body(Body::empty()).expect("request"))
            .await
            .expect("list response");
        assert_eq!(list_response.status(), StatusCode::OK);
        let listed = response_json(list_response).await;
        assert!(listed
            .as_array()
            .expect("task list array")
            .iter()
            .any(|task| task["task_id"] == json!(task_id.clone())));

        let inspect_response = app
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
        assert_eq!(inspected["task_id"], json!(task_id));
    }

    #[tokio::test]
    async fn rest_openapi_remains_available_without_bearer_token_when_bound_beyond_loopback() {
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
    async fn gateway_mutations_require_bearer_token_when_bound_beyond_loopback() {
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
        assert!(read.ok);
    }

    #[tokio::test]
    async fn gateway_reads_remain_available_without_bearer_token_when_bound_beyond_loopback() {
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
        let task_id = created.result.expect("created")["task_id"]
            .as_str()
            .expect("task id")
            .to_owned();

        let listed = service
            .dispatch_request_authorized(
                GatewayRequest {
                    id: json!("list"),
                    method: "task.list".to_owned(),
                    params: json!({}),
                },
                None,
            )
            .await;
        assert!(listed.ok);
        assert!(listed
            .result
            .expect("task list")
            .as_array()
            .expect("task list array")
            .iter()
            .any(|task| task["task_id"] == json!(task_id.clone())));

        let inspected = service
            .dispatch_request_authorized(
                GatewayRequest {
                    id: json!("inspect"),
                    method: "task.inspect".to_owned(),
                    params: json!({ "task_id": task_id }),
                },
                None,
            )
            .await;
        assert!(inspected.ok);
    }

    #[tokio::test]
    async fn read_only_gateway_and_rest_inspect_surfaces_remain_available_without_bearer_token_when_bound_beyond_loopback(
    ) {
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
        let worker_id = session_payload["worker"]["worker_id"].as_str().expect("worker id").to_owned();
        let session_id = session_payload["session"]["session_id"].as_str().expect("session id").to_owned();

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
        let task_id = task.result.expect("task payload")["task_id"].as_str().expect("task id").to_owned();

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
        let run_id = run.result.expect("run payload")["run_id"].as_str().expect("run id").to_owned();

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
        assert!(profile_list.ok);

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
        assert!(worker_list.ok);
        assert!(worker_list
            .result
            .expect("worker list")
            .as_array()
            .expect("worker list array")
            .iter()
            .any(|worker| worker["worker_id"] == json!(worker_id.clone())));

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
        assert!(worker_inspect.ok);

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
        assert!(session_list.ok);
        assert!(session_list
            .result
            .expect("session list")
            .as_array()
            .expect("session list array")
            .iter()
            .any(|session| session["session_id"] == json!(session_id.clone())));

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
        assert!(session_show.ok);

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
        assert!(run_inspect.ok);
        assert_eq!(run_inspect.result.expect("run inspect")["status"], json!("completed"));

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
        assert!(subscribed.ok);
        assert_eq!(subscribed.result.expect("subscribe")["subscribed"], json!(true));

        let app = service.clone().router();

        let workers_response = app
            .clone()
            .oneshot(Request::builder().uri("/workers").body(Body::empty()).expect("request"))
            .await
            .expect("workers response");
        assert_eq!(workers_response.status(), StatusCode::OK);

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

        thread::sleep(Duration::from_millis(150));

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
        assert_eq!(payload["pruned_task_ids"], json!([old_done_id.as_str()]));

        assert!(!service.store.paths().task_path(&old_done_id).exists());
        assert!(service.store.paths().task_path(&old_cancelled_id).exists());
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
            let worker_id =
                WorkerId::new(format!("si-worker-{worker_suffix}")).expect("worker id");
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
        let rule_path = state_root.join("state").join("producers").join("cron").join("nightly.json");
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
        let rule_path = state_root.join("state").join("producers").join("hook").join("task-created.json");
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
