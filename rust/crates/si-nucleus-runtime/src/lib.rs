use std::collections::BTreeMap;
use std::path::PathBuf;

use anyhow::Result;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use si_nucleus_core::{
    CanonicalEventSource, CanonicalEventType, EventDataEnvelope, ProfileName, RunId, RunStatus,
    SessionId, TaskId, WorkerId, WorkerStatus,
};

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct RuntimeCommand {
    pub program: String,
    pub args: Vec<String>,
    pub current_dir: PathBuf,
    pub env: BTreeMap<String, String>,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct WorkerLaunchSpec {
    pub worker_id: WorkerId,
    pub profile: ProfileName,
    pub home_dir: PathBuf,
    pub codex_home: PathBuf,
    pub workdir: PathBuf,
    pub extra_env: BTreeMap<String, String>,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct WorkerRuntimeView {
    pub worker_id: WorkerId,
    pub runtime_name: String,
    pub pid: u32,
    pub started_at: DateTime<Utc>,
    pub checked_at: DateTime<Utc>,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct RuntimeStatusSnapshot {
    pub source: String,
    pub model: Option<String>,
    pub reasoning_effort: Option<String>,
    pub account_email: Option<String>,
    pub account_plan: Option<String>,
    pub five_hour_left_pct: Option<f64>,
    pub five_hour_reset: Option<String>,
    pub five_hour_remaining_minutes: Option<i32>,
    pub weekly_left_pct: Option<f64>,
    pub weekly_reset: Option<String>,
    pub weekly_remaining_minutes: Option<i32>,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct WorkerProbeResult {
    pub status: WorkerStatus,
    pub snapshot: RuntimeStatusSnapshot,
    pub checked_at: DateTime<Utc>,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct WorkerStartResult {
    pub runtime: WorkerRuntimeView,
    pub probe: WorkerProbeResult,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct SessionOpenSpec {
    pub session_id: SessionId,
    pub worker_id: WorkerId,
    pub profile: ProfileName,
    pub workdir: PathBuf,
    pub resume_thread_id: Option<String>,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct SessionOpenResult {
    pub thread_id: String,
    pub created: bool,
    pub opened_at: DateTime<Utc>,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case", tag = "type")]
pub enum RunInputItem {
    Text { text: String },
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct RunTurnSpec {
    pub run_id: RunId,
    pub task_id: Option<TaskId>,
    pub worker_id: WorkerId,
    pub session_id: SessionId,
    pub profile: ProfileName,
    pub thread_id: String,
    pub timeout_seconds: Option<u64>,
    pub input: Vec<RunInputItem>,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct RuntimeRunOutcome {
    pub turn_id: String,
    pub status: RunStatus,
    pub completed_at: DateTime<Utc>,
    pub final_output: Option<String>,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct CanonicalEventDraft {
    #[serde(rename = "type")]
    pub event_type: CanonicalEventType,
    pub source: CanonicalEventSource,
    pub data: EventDataEnvelope,
}

pub trait NucleusRuntime: Send + Sync {
    fn runtime_name(&self) -> &'static str;
    fn build_worker_command(&self, spec: &WorkerLaunchSpec) -> RuntimeCommand;
    fn probe_worker(&self, spec: &WorkerLaunchSpec) -> Result<WorkerProbeResult>;
    fn start_worker(&self, spec: &WorkerLaunchSpec) -> Result<WorkerStartResult>;
    fn stop_worker(&self, worker_id: &WorkerId) -> Result<()>;
    fn inspect_worker(&self, worker_id: &WorkerId) -> Result<Option<WorkerRuntimeView>>;
    fn ensure_session(&self, spec: &SessionOpenSpec) -> Result<SessionOpenResult>;
    fn execute_turn(
        &self,
        spec: &RunTurnSpec,
        on_event: &mut dyn FnMut(CanonicalEventDraft) -> Result<()>,
    ) -> Result<RuntimeRunOutcome>;
    fn interrupt_turn(&self, worker_id: &WorkerId, thread_id: &str, turn_id: &str) -> Result<()>;
    fn probe_events(
        &self,
        spec: &WorkerLaunchSpec,
        probe: &WorkerProbeResult,
    ) -> Result<Vec<CanonicalEventDraft>>;
    fn status_payload(&self, probe: &WorkerProbeResult) -> Value;
}
