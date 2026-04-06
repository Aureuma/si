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
use axum::Router;
use axum::extract::State;
use axum::extract::ws::{Message, WebSocket, WebSocketUpgrade};
use axum::response::IntoResponse;
use axum::routing::get;
use chrono::{DateTime, Duration as ChronoDuration, SecondsFormat, Utc};
use cron::Schedule;
use futures_util::{SinkExt, StreamExt};
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
use tokio::net::TcpListener;
use tokio::sync::broadcast;

const DEFAULT_BIND_ADDR: &str = "127.0.0.1:4747";
const WS_PATH: &str = "/ws";
const DISPATCH_LOOP_INTERVAL: Duration = Duration::from_millis(200);

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

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
struct CronRuleRecord {
    name: String,
    enabled: bool,
    schedule_kind: CronScheduleKind,
    schedule: String,
    instructions: String,
    last_emitted_at: Option<DateTime<Utc>>,
    next_due_at: Option<DateTime<Utc>>,
    version: u32,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
struct HookRuleRecord {
    name: String,
    enabled: bool,
    match_event_type: String,
    instructions: String,
    #[serde(default)]
    last_processed_event_seq: u64,
    version: u32,
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

    pub fn list_profile_records(&self) -> Result<Vec<ProfileRecord>> {
        let mut profiles = Vec::new();
        for entry in fs::read_dir(&self.paths.profiles_state_dir)
            .with_context(|| format!("read {}", self.paths.profiles_state_dir.display()))?
        {
            let entry = entry?;
            if !entry.path().is_file() {
                continue;
            }
            let bytes = fs::read(entry.path())?;
            profiles.push(serde_json::from_slice::<ProfileRecord>(&bytes)?);
        }
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
        let worker_path = self.paths.worker_path(&worker.worker_id);
        write_json_atomic(&worker_path, &worker)?;
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
        write_json_atomic(&self.paths.worker_path(&worker.worker_id), &worker)?;
        write_json_atomic(&self.paths.worker_runtime_path(&worker.worker_id), &started.runtime)?;
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
        write_json_atomic(&self.paths.run_path(&run.run_id), &run)?;
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

    fn find_task_by_producer_dedup_locked(
        &self,
        source: TaskSource,
        producer_rule_name: &str,
        producer_dedup_key: &str,
    ) -> Result<Option<TaskRecord>> {
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
            write_json_atomic(&self.paths.worker_path(worker_id), &worker)?;
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
            write_json_atomic(&self.paths.session_path(session_id), &session)?;
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
        write_json_atomic(&self.paths.session_path(session_id), &session)?;
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
        let bytes = fs::read(&path).with_context(|| format!("read {}", path.display()))?;
        records.push(
            serde_json::from_slice::<T>(&bytes)
                .with_context(|| format!("parse {}", path.display()))?,
        );
    }
    Ok(records)
}

fn load_canonical_events(path: &Path) -> Result<Vec<CanonicalEvent>> {
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
    background_started: Arc<AtomicBool>,
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
        Router::new().route(WS_PATH, get(ws_handler)).with_state(Arc::new(self))
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
        if rule.next_due_at.is_none() {
            rule.next_due_at = next_cron_due_after(rule, now - ChronoDuration::seconds(1))?;
            self.write_cron_rule(rule)?;
        }

        let mut changed = false;
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
                worker_id,
                Some(run.session_id.clone()),
                profile,
                blocked_reason,
                &message,
                mark_session_broken,
            )?;
        }
        Ok(())
    }

    fn reconcile_run_as_blocked(
        &self,
        run: &RunRecord,
        worker_id: Option<WorkerId>,
        session_id: Option<SessionId>,
        profile: Option<ProfileName>,
        blocked_reason: BlockedReason,
        message: &str,
        mark_session_broken: bool,
    ) -> Result<()> {
        let events = self.store.apply_runtime_event(CanonicalEventDraft {
            event_type: CanonicalEventType::RunBlocked,
            source: CanonicalEventSource::System,
            data: EventDataEnvelope {
                task_id: Some(run.task_id.clone()),
                worker_id,
                session_id: session_id.clone(),
                run_id: Some(run.run_id.clone()),
                profile,
                payload: json!({
                    "blocked_reason": blocked_reason,
                    "error": message,
                }),
            },
        })?;
        for event in events {
            let _ = self.events.send(event);
        }
        if mark_session_broken {
            if let Some(session_id) = session_id {
                if let Some(event) = self.store.mark_session_broken(&session_id, message)? {
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
                Some(session.worker_id.as_str()),
                None,
                None,
                session.workdir.as_ref().map(PathBuf::from),
                None,
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
                    return Ok(self.store.inspect_session(&session.session_id)?);
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

        let worker =
            match self.ensure_worker_started(runtime, profile, None, None, None, None, None) {
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
                Some(session.worker_id.as_str()),
                None,
                None,
                session.workdir.as_ref().map(PathBuf::from),
                None,
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
                    Some(workdir.clone()),
                    params.env,
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
        requested_worker_id: Option<&str>,
        home_dir: Option<PathBuf>,
        codex_home: Option<PathBuf>,
        workdir: Option<PathBuf>,
        env: Option<BTreeMap<String, String>>,
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
        let home_dir = home_dir
            .or_else(|| {
                existing.as_ref().and_then(|worker| worker.home_dir.as_ref().map(PathBuf::from))
            })
            .unwrap_or_else(default_home_dir);
        let codex_home = codex_home
            .or_else(|| existing.as_ref().map(|worker| PathBuf::from(worker.codex_home.clone())))
            .unwrap_or_else(|| default_codex_home_dir(profile.as_str()));
        let workdir = workdir
            .or_else(|| {
                existing.as_ref().and_then(|worker| worker.workdir.as_ref().map(PathBuf::from))
            })
            .unwrap_or_else(default_workdir);
        let extra_env = env
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
    use std::collections::{HashMap, HashSet};
    use std::fs;
    use std::path::Path;
    use std::sync::{Arc, Mutex};
    use std::thread;
    use std::time::Duration;

    use super::{
        CronRuleRecord, CronScheduleKind, GatewayRequest, HookRuleRecord, NucleusConfig,
        NucleusService, cron_due_key, write_json_atomic,
    };
    use anyhow::Result;
    use chrono::{Duration as ChronoDuration, Utc};
    use serde_json::json;
    use si_nucleus_core::{
        CanonicalEventSource, CanonicalEventType, EventDataEnvelope, ProfileName, RunId, RunRecord,
        RunStatus, SessionId, SessionLifecycleState, TaskId, TaskStatus, WorkerId, WorkerStatus,
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
        run_delay: Duration,
        fail_execute: bool,
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
        let task_id = task
            .result
            .as_ref()
            .and_then(|item| item["task_id"].as_str())
            .expect("task id");

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
        let run_id = run
            .result
            .as_ref()
            .and_then(|item| item["run_id"].as_str())
            .expect("run id");

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
                version: 1,
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
                version: 1,
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
                version: 1,
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
}
