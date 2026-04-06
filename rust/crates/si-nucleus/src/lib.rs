use std::env;
use std::fs::{self, File, OpenOptions};
use std::future::pending;
use std::io::{BufRead, BufReader, Write};
use std::net::SocketAddr;
use std::path::{Path, PathBuf};
use std::process;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{SystemTime, UNIX_EPOCH};

use anyhow::{Context, Result, anyhow};
use axum::Router;
use axum::extract::State;
use axum::extract::ws::{Message, WebSocket, WebSocketUpgrade};
use axum::response::IntoResponse;
use axum::routing::get;
use chrono::Utc;
use futures_util::{SinkExt, StreamExt};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use si_nucleus_core::{
    BlockedReason, CanonicalEvent, CanonicalEventSource, CanonicalEventType, EventDataEnvelope,
    EventId, ProfileName, RunId, RunRecord, RunStatus, SessionId, SessionLifecycleState,
    SessionRecord, TaskId, TaskRecord, TaskSource, TaskStatus, WorkerId, WorkerRecord,
};
use si_nucleus_runtime::{
    CanonicalEventDraft, NucleusRuntime, RunInputItem, RunTurnSpec, SessionOpenSpec,
    WorkerLaunchSpec, WorkerProbeResult, WorkerRuntimeView, WorkerStartResult,
};
use tokio::net::TcpListener;
use tokio::sync::broadcast;

const DEFAULT_BIND_ADDR: &str = "127.0.0.1:4747";
const WS_PATH: &str = "/ws";

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

    pub fn session_dir(&self, session_id: &SessionId) -> PathBuf {
        self.sessions_state_dir.join(session_id.as_str())
    }

    pub fn session_path(&self, session_id: &SessionId) -> PathBuf {
        self.session_dir(session_id).join("session.json")
    }

    pub fn run_dir(&self, run_id: &RunId) -> PathBuf {
        self.runs_state_dir.join(run_id.as_str())
    }

    pub fn run_path(&self, run_id: &RunId) -> PathBuf {
        self.run_dir(run_id).join("run.json")
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
        Ok(Self { paths, next_event_seq: AtomicU64::new(last_seq), write_lock: Mutex::new(()) })
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

    pub fn list_tasks(&self) -> Result<Vec<TaskRecord>> {
        let mut tasks = Vec::new();
        for entry in fs::read_dir(&self.paths.tasks_state_dir)
            .with_context(|| format!("read {}", self.paths.tasks_state_dir.display()))?
        {
            let entry = entry?;
            let path = entry.path().join("task.json");
            if !path.exists() {
                continue;
            }
            let bytes = fs::read(&path).with_context(|| format!("read {}", path.display()))?;
            let task = serde_json::from_slice::<TaskRecord>(&bytes)
                .with_context(|| format!("parse {}", path.display()))?;
            tasks.push(task);
        }
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
        let mut workers = Vec::new();
        for entry in fs::read_dir(&self.paths.workers_state_dir)
            .with_context(|| format!("read {}", self.paths.workers_state_dir.display()))?
        {
            let entry = entry?;
            let path = entry.path().join("state.json");
            if !path.exists() {
                continue;
            }
            let bytes = fs::read(&path).with_context(|| format!("read {}", path.display()))?;
            let worker = serde_json::from_slice::<WorkerRecord>(&bytes)
                .with_context(|| format!("parse {}", path.display()))?;
            workers.push(worker);
        }
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
        let mut sessions = Vec::new();
        for entry in fs::read_dir(&self.paths.sessions_state_dir)
            .with_context(|| format!("read {}", self.paths.sessions_state_dir.display()))?
        {
            let entry = entry?;
            let path = entry.path().join("session.json");
            if !path.exists() {
                continue;
            }
            let bytes = fs::read(&path).with_context(|| format!("read {}", path.display()))?;
            let session = serde_json::from_slice::<SessionRecord>(&bytes)
                .with_context(|| format!("parse {}", path.display()))?;
            sessions.push(session);
        }
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
        let mut runs = Vec::new();
        for entry in fs::read_dir(&self.paths.runs_state_dir)
            .with_context(|| format!("read {}", self.paths.runs_state_dir.display()))?
        {
            let entry = entry?;
            let path = entry.path().join("run.json");
            if !path.exists() {
                continue;
            }
            let bytes = fs::read(&path).with_context(|| format!("read {}", path.display()))?;
            let run = serde_json::from_slice::<RunRecord>(&bytes)
                .with_context(|| format!("parse {}", path.display()))?;
            runs.push(run);
        }
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
        let mut profiles = Vec::new();
        for entry in fs::read_dir(&self.paths.profiles_state_dir)
            .with_context(|| format!("read {}", self.paths.profiles_state_dir.display()))?
        {
            let entry = entry?;
            if !entry.path().is_file() {
                continue;
            }
            let bytes = fs::read(entry.path())?;
            profiles.push(serde_json::from_slice::<Value>(&bytes)?);
        }
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
            codex_home: spec.codex_home.display().to_string(),
            status: probe.status,
            capability_version: payload.get("model").and_then(Value::as_str).map(ToOwned::to_owned),
            last_heartbeat_at: Some(probe.checked_at),
            effective_account_state: payload
                .get("account_email")
                .and_then(Value::as_str)
                .map(ToOwned::to_owned)
                .or_else(|| Some(payload.to_string())),
        };
        let worker_path = self.paths.worker_path(&worker.worker_id);
        write_json_atomic(&worker_path, &worker)?;
        let mut events = Vec::new();
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
            codex_home: spec.codex_home.display().to_string(),
            status: started.probe.status,
            capability_version: payload.get("model").and_then(Value::as_str).map(ToOwned::to_owned),
            last_heartbeat_at: Some(started.probe.checked_at),
            effective_account_state: payload
                .get("account_email")
                .and_then(Value::as_str)
                .map(ToOwned::to_owned)
                .or_else(|| Some(payload.to_string())),
        };
        write_json_atomic(&self.paths.worker_path(&worker.worker_id), &worker)?;
        write_json_atomic(&self.paths.worker_runtime_path(&worker.worker_id), &started.runtime)?;
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
    ) -> Result<(SessionRecord, CanonicalEvent)> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        let mut session = SessionRecord::new(session_id.clone(), worker_id.clone());
        session.app_server_thread_id = Some(thread_id.clone());
        session.transition_to(SessionLifecycleState::Ready).map_err(anyhow::Error::new)?;
        write_json_atomic(&self.paths.session_path(&session_id), &session)?;
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

    fn create_run(&self, run: RunRecord, task_id: Option<&TaskId>) -> Result<RunRecord> {
        let _guard = self.write_lock.lock().map_err(|_| anyhow!("nucleus store lock poisoned"))?;
        write_json_atomic(&self.paths.run_path(&run.run_id), &run)?;
        if let Some(task_id) = task_id {
            if let Some(mut task) = self.read_task_locked(task_id)? {
                if task.session_id.is_none() {
                    task.session_id = Some(run.session_id.clone());
                }
                task.latest_run_id = Some(run.run_id.clone());
                task.updated_at = Utc::now();
                write_json_atomic(&self.paths.task_path(task_id), &task)?;
            }
        }
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
                    write_json_atomic(&self.paths.run_path(&run_id), &run)?;
                }
                if let Some(session_id) = primary.data.session_id.clone() {
                    if let Some(mut session) = self.read_session_locked(&session_id)? {
                        if session.lifecycle_state == SessionLifecycleState::Ready {
                            session
                                .transition_to(SessionLifecycleState::Busy)
                                .map_err(anyhow::Error::new)?;
                            write_json_atomic(&self.paths.session_path(&session_id), &session)?;
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
        if let Some(run_id) = event.data.run_id.clone() {
            if let Some(mut run) = self.read_run_locked(&run_id)? {
                if run.status != run_status {
                    run.transition_to(run_status).map_err(anyhow::Error::new)?;
                }
                write_json_atomic(&self.paths.run_path(&run_id), &run)?;
            }
        }
        if let Some(session_id) = event.data.session_id.clone() {
            if let Some(mut session) = self.read_session_locked(&session_id)? {
                if session.lifecycle_state == SessionLifecycleState::Busy {
                    session
                        .transition_to(SessionLifecycleState::Ready)
                        .map_err(anyhow::Error::new)?;
                    write_json_atomic(&self.paths.session_path(&session_id), &session)?;
                }
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
    let file = File::open(path).with_context(|| format!("open {}", path.display()))?;
    let reader = BufReader::new(file);
    let mut last_seq = 0_u64;
    for (index, line) in reader.lines().enumerate() {
        let line = line.with_context(|| format!("read {} line {}", path.display(), index + 1))?;
        if line.trim().is_empty() {
            continue;
        }
        let event = serde_json::from_str::<CanonicalEvent>(&line)
            .with_context(|| format!("parse {} line {}", path.display(), index + 1))?;
        last_seq = last_seq.max(event.seq);
    }
    Ok(last_seq)
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

fn append_jsonl<T: Serialize>(path: &Path, value: &T) -> Result<()> {
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
}

impl NucleusService {
    pub fn open(config: NucleusConfig) -> Result<Self> {
        Self::open_without_runtime(config)
    }

    pub fn open_without_runtime(config: NucleusConfig) -> Result<Self> {
        let store = Arc::new(NucleusStore::open(config.state_dir.clone())?);
        persist_gateway_metadata(store.paths(), config.bind_addr)?;
        let (events, _) = broadcast::channel(256);
        Ok(Self { config, store, events, runtime: None })
    }

    pub fn open_with_runtime(
        config: NucleusConfig,
        runtime: Arc<dyn NucleusRuntime>,
    ) -> Result<Self> {
        let store = Arc::new(NucleusStore::open(config.state_dir.clone())?);
        persist_gateway_metadata(store.paths(), config.bind_addr)?;
        let (events, _) = broadcast::channel(256);
        Ok(Self { config, store, events, runtime: Some(runtime) })
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
        Router::new().route(WS_PATH, get(ws_handler)).with_state(Arc::new(self))
    }

    pub async fn serve(self) -> Result<()> {
        let bind_addr = self.config.bind_addr;
        let listener =
            TcpListener::bind(bind_addr).await.with_context(|| format!("bind {}", bind_addr))?;
        axum::serve(listener, self.router()).await.context("serve si-nucleus websocket gateway")
    }

    pub async fn dispatch_request(&self, request: GatewayRequest) -> GatewayResponse {
        match self.handle_request(request.method.as_str(), request.params.clone()).await {
            Ok(result) => GatewayResponse::ok(request.id, result),
            Err(error) => {
                GatewayResponse::err(request.id, infer_error_code(&error), error.to_string(), None)
            }
        }
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
                    params.worker_id.as_deref(),
                    params.home_dir,
                    params.codex_home,
                    workdir.clone(),
                    params.env.unwrap_or_default(),
                )?;
                let session_id = SessionId::generate();
                let opened = runtime.ensure_session(&SessionOpenSpec {
                    session_id: session_id.clone(),
                    worker_id: worker.worker.worker_id.clone(),
                    profile: profile.clone(),
                    workdir,
                    resume_thread_id: params.thread_id,
                })?;
                let (session, event) = self.store.record_session_open(
                    session_id,
                    worker.worker.worker_id.clone(),
                    opened.thread_id,
                    opened.created,
                    profile,
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
                let run = RunRecord::new(RunId::generate(), task_id.clone(), session_id.clone());
                let run = self.store.create_run(run, Some(&task_id))?;
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
                let store = Arc::clone(&self.store);
                let runtime = Arc::clone(runtime);
                let events = self.events.clone();
                tokio::task::spawn_blocking(move || {
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
        requested_worker_id: Option<&str>,
        home_dir: Option<PathBuf>,
        codex_home: Option<PathBuf>,
        workdir: PathBuf,
        env: std::collections::BTreeMap<String, String>,
    ) -> Result<EnsuredWorker> {
        let existing = if let Some(worker_id) = requested_worker_id {
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
        let spec = WorkerLaunchSpec {
            worker_id: worker_id.clone(),
            profile: profile.clone(),
            home_dir: home_dir.unwrap_or_else(default_home_dir),
            codex_home: codex_home.unwrap_or_else(|| default_codex_home_dir(profile.as_str())),
            workdir,
            extra_env: env,
        };

        let live_runtime = runtime.inspect_worker(&worker_id)?;
        if let (Some(worker), Some(runtime_view)) = (existing.clone(), live_runtime) {
            return Ok(EnsuredWorker { worker, runtime: Some(runtime_view) });
        }

        let started = runtime.start_worker(&spec)?;
        let (worker, runtime_view, events) =
            self.store.record_worker_start(&spec, &started, runtime)?;
        for event in events {
            let _ = self.events.send(event);
        }
        Ok(EnsuredWorker { worker, runtime: Some(runtime_view) })
    }
}

#[derive(Clone, Debug)]
struct EnsuredWorker {
    worker: WorkerRecord,
    runtime: Option<WorkerRuntimeView>,
}

async fn ws_handler(
    ws: WebSocketUpgrade,
    State(service): State<Arc<NucleusService>>,
) -> impl IntoResponse {
    ws.on_upgrade(move |socket| async move {
        let _ = handle_socket(service, socket).await;
    })
}

async fn handle_socket(service: Arc<NucleusService>, socket: WebSocket) -> Result<()> {
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
                        let response = service.dispatch_request(request).await;
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

fn infer_error_code(error: &anyhow::Error) -> &'static str {
    let message = error.to_string();
    if message.contains("parse ") {
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

#[derive(Clone, Debug, Deserialize)]
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

#[derive(Clone, Debug, Deserialize)]
struct TaskInspectParams {
    task_id: String,
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
    use std::collections::HashMap;
    use std::sync::{Arc, Mutex};
    use std::thread;
    use std::time::Duration;

    use super::{GatewayRequest, NucleusConfig, NucleusService};
    use anyhow::Result;
    use chrono::Utc;
    use serde_json::json;
    use si_nucleus_core::{
        CanonicalEventSource, CanonicalEventType, EventDataEnvelope, ProfileName, RunStatus,
        WorkerId, WorkerStatus,
    };
    use si_nucleus_runtime::{
        CanonicalEventDraft, NucleusRuntime, RunTurnSpec, RuntimeCommand, RuntimeRunOutcome,
        RuntimeStatusSnapshot, SessionOpenResult, SessionOpenSpec, WorkerLaunchSpec,
        WorkerProbeResult, WorkerRuntimeView, WorkerStartResult,
    };
    use tempfile::tempdir;

    #[derive(Default)]
    struct FakeState {
        workers: HashMap<String, WorkerRuntimeView>,
    }

    #[derive(Clone, Default)]
    struct FakeRuntime {
        state: Arc<Mutex<FakeState>>,
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
            _turn_id: &str,
        ) -> Result<()> {
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
    }
}
