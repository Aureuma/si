use std::collections::BTreeMap;
use std::fmt;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{SystemTime, UNIX_EPOCH};

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use thiserror::Error;

static ID_COUNTER: AtomicU64 = AtomicU64::new(1);

fn next_opaque_id() -> String {
    let millis = SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default().as_millis();
    let counter = ID_COUNTER.fetch_add(1, Ordering::Relaxed);
    format!("{millis:016x}{counter:08x}")
}

#[derive(Debug, Error, Eq, PartialEq)]
pub enum IdError {
    #[error("id cannot be empty")]
    Empty,
    #[error("id must start with {expected}, got {actual}")]
    MissingPrefix { expected: &'static str, actual: String },
}

macro_rules! define_id {
    ($name:ident, $prefix:literal) => {
        #[derive(Clone, Debug, Eq, PartialEq, Ord, PartialOrd, Hash, Serialize, Deserialize)]
        #[serde(transparent)]
        pub struct $name(String);

        impl $name {
            pub const PREFIX: &'static str = $prefix;

            pub fn new(value: impl Into<String>) -> Result<Self, IdError> {
                let value = value.into();
                let trimmed = value.trim();
                if trimmed.is_empty() {
                    return Err(IdError::Empty);
                }
                if !trimmed.starts_with(Self::PREFIX) {
                    return Err(IdError::MissingPrefix {
                        expected: Self::PREFIX,
                        actual: trimmed.to_owned(),
                    });
                }
                Ok(Self(trimmed.to_owned()))
            }

            pub fn generate() -> Self {
                Self(format!("{}{}", Self::PREFIX, next_opaque_id()))
            }

            pub fn as_str(&self) -> &str {
                &self.0
            }
        }

        impl fmt::Display for $name {
            fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
                f.write_str(self.as_str())
            }
        }
    };
}

define_id!(TaskId, "si-task-");
define_id!(WorkerId, "si-worker-");
define_id!(SessionId, "si-session-");
define_id!(RunId, "si-run-");
define_id!(EventId, "si-event-");

#[derive(Clone, Debug, Eq, PartialEq, Ord, PartialOrd, Hash, Serialize, Deserialize)]
#[serde(transparent)]
pub struct ProfileName(String);

impl ProfileName {
    pub fn new(value: impl Into<String>) -> Result<Self, ProfileNameError> {
        let value = value.into();
        let trimmed = value.trim();
        if trimmed.is_empty() {
            return Err(ProfileNameError::Empty);
        }
        let mut chars = trimmed.chars();
        let Some(first) = chars.next() else {
            return Err(ProfileNameError::Empty);
        };
        if !first.is_ascii_lowercase() {
            return Err(ProfileNameError::Invalid(trimmed.to_owned()));
        }
        if !chars.all(|ch| ch.is_ascii_lowercase() || ch.is_ascii_digit() || ch == '-') {
            return Err(ProfileNameError::Invalid(trimmed.to_owned()));
        }
        Ok(Self(trimmed.to_owned()))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl fmt::Display for ProfileName {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Error, Eq, PartialEq)]
pub enum ProfileNameError {
    #[error("profile name cannot be empty")]
    Empty,
    #[error("profile name must match ^[a-z][a-z0-9-]*$, got {0}")]
    Invalid(String),
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum TaskSource {
    Cli,
    Websocket,
    Cron,
    Hook,
    System,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum TaskStatus {
    Queued,
    Running,
    Blocked,
    Done,
    Failed,
    Cancelled,
}

impl TaskStatus {
    pub fn can_transition_to(self, next: Self) -> bool {
        matches!(
            (self, next),
            (Self::Queued, Self::Running)
                | (Self::Queued, Self::Blocked)
                | (Self::Queued, Self::Failed)
                | (Self::Queued, Self::Cancelled)
                | (Self::Running, Self::Done)
                | (Self::Running, Self::Failed)
                | (Self::Running, Self::Blocked)
                | (Self::Running, Self::Cancelled)
                | (Self::Blocked, Self::Queued)
                | (Self::Blocked, Self::Cancelled)
        )
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum BlockedReason {
    AuthRequired,
    WorkerUnavailable,
    SessionBroken,
    ProducerError,
    OperatorHold,
    FortUnavailable,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum WorkerStatus {
    Starting,
    Ready,
    Degraded,
    Failed,
    Stopped,
}

impl WorkerStatus {
    pub fn can_transition_to(self, next: Self) -> bool {
        matches!(
            (self, next),
            (Self::Starting, Self::Ready)
                | (Self::Starting, Self::Failed)
                | (Self::Starting, Self::Stopped)
                | (Self::Ready, Self::Degraded)
                | (Self::Ready, Self::Failed)
                | (Self::Ready, Self::Stopped)
                | (Self::Degraded, Self::Ready)
                | (Self::Degraded, Self::Failed)
                | (Self::Degraded, Self::Stopped)
                | (Self::Failed, Self::Starting)
                | (Self::Failed, Self::Stopped)
                | (Self::Stopped, Self::Starting)
        )
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum SessionLifecycleState {
    Opening,
    Ready,
    Busy,
    Broken,
    Closed,
}

impl SessionLifecycleState {
    pub fn can_transition_to(self, next: Self) -> bool {
        matches!(
            (self, next),
            (Self::Opening, Self::Ready)
                | (Self::Opening, Self::Busy)
                | (Self::Opening, Self::Broken)
                | (Self::Opening, Self::Closed)
                | (Self::Ready, Self::Busy)
                | (Self::Ready, Self::Broken)
                | (Self::Ready, Self::Closed)
                | (Self::Busy, Self::Ready)
                | (Self::Busy, Self::Broken)
                | (Self::Busy, Self::Closed)
                | (Self::Broken, Self::Closed)
        )
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum RunStatus {
    Queued,
    Running,
    Blocked,
    Completed,
    Failed,
    Cancelled,
}

impl RunStatus {
    pub fn can_transition_to(self, next: Self) -> bool {
        matches!(
            (self, next),
            (Self::Queued, Self::Running)
                | (Self::Queued, Self::Blocked)
                | (Self::Queued, Self::Failed)
                | (Self::Queued, Self::Cancelled)
                | (Self::Running, Self::Completed)
                | (Self::Running, Self::Failed)
                | (Self::Running, Self::Blocked)
                | (Self::Running, Self::Cancelled)
                | (Self::Blocked, Self::Queued)
                | (Self::Blocked, Self::Cancelled)
        )
    }
}

#[derive(Debug, Error, Eq, PartialEq)]
#[error("invalid {entity} transition: {from:?} -> {to:?}")]
pub struct TransitionError<S>
where
    S: fmt::Debug + Copy,
{
    pub entity: &'static str,
    pub from: S,
    pub to: S,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct TaskRecord {
    pub task_id: TaskId,
    pub source: TaskSource,
    pub title: String,
    pub instructions: String,
    pub status: TaskStatus,
    pub profile: Option<ProfileName>,
    pub session_id: Option<SessionId>,
    pub latest_run_id: Option<RunId>,
    pub checkpoint_summary: Option<String>,
    pub checkpoint_at: Option<DateTime<Utc>>,
    pub checkpoint_seq: Option<u64>,
    pub parent_task_id: Option<TaskId>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub producer_rule_name: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub producer_dedup_key: Option<String>,
    pub blocked_reason: Option<BlockedReason>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
    pub max_retries: Option<u32>,
    pub timeout_seconds: Option<u64>,
}

impl TaskRecord {
    pub fn new(
        task_id: TaskId,
        source: TaskSource,
        title: impl Into<String>,
        instructions: impl Into<String>,
    ) -> Self {
        let now = Utc::now();
        Self {
            task_id,
            source,
            title: title.into(),
            instructions: instructions.into(),
            status: TaskStatus::Queued,
            profile: None,
            session_id: None,
            latest_run_id: None,
            checkpoint_summary: None,
            checkpoint_at: None,
            checkpoint_seq: None,
            parent_task_id: None,
            producer_rule_name: None,
            producer_dedup_key: None,
            blocked_reason: None,
            created_at: now,
            updated_at: now,
            max_retries: None,
            timeout_seconds: None,
        }
    }

    pub fn transition_to(
        &mut self,
        next: TaskStatus,
        blocked_reason: Option<BlockedReason>,
    ) -> Result<(), TransitionError<TaskStatus>> {
        if !self.status.can_transition_to(next) {
            return Err(TransitionError { entity: "task", from: self.status, to: next });
        }
        self.status = next;
        self.blocked_reason = if next == TaskStatus::Blocked { blocked_reason } else { None };
        self.updated_at = Utc::now();
        Ok(())
    }
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct WorkerRecord {
    pub worker_id: WorkerId,
    pub profile: ProfileName,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub home_dir: Option<String>,
    pub codex_home: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub workdir: Option<String>,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub extra_env: BTreeMap<String, String>,
    pub status: WorkerStatus,
    pub capability_version: Option<String>,
    pub last_heartbeat_at: Option<DateTime<Utc>>,
    pub effective_account_state: Option<String>,
}

impl WorkerRecord {
    pub fn transition_to(
        &mut self,
        next: WorkerStatus,
    ) -> Result<(), TransitionError<WorkerStatus>> {
        if !self.status.can_transition_to(next) {
            return Err(TransitionError { entity: "worker", from: self.status, to: next });
        }
        self.status = next;
        self.last_heartbeat_at = Some(Utc::now());
        Ok(())
    }
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct SessionRecord {
    pub session_id: SessionId,
    pub worker_id: WorkerId,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub profile: Option<ProfileName>,
    pub app_server_thread_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub workdir: Option<String>,
    pub lifecycle_state: SessionLifecycleState,
    pub summary_state: Option<String>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl SessionRecord {
    pub fn new(session_id: SessionId, worker_id: WorkerId) -> Self {
        let now = Utc::now();
        Self {
            session_id,
            worker_id,
            profile: None,
            app_server_thread_id: None,
            workdir: None,
            lifecycle_state: SessionLifecycleState::Opening,
            summary_state: None,
            created_at: now,
            updated_at: now,
        }
    }

    pub fn transition_to(
        &mut self,
        next: SessionLifecycleState,
    ) -> Result<(), TransitionError<SessionLifecycleState>> {
        if !self.lifecycle_state.can_transition_to(next) {
            return Err(TransitionError {
                entity: "session",
                from: self.lifecycle_state,
                to: next,
            });
        }
        self.lifecycle_state = next;
        self.updated_at = Utc::now();
        Ok(())
    }
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct RunRecord {
    pub run_id: RunId,
    pub task_id: TaskId,
    pub session_id: SessionId,
    pub status: RunStatus,
    pub started_at: Option<DateTime<Utc>>,
    pub ended_at: Option<DateTime<Utc>>,
    pub parent_run_id: Option<RunId>,
    pub app_server_turn_id: Option<String>,
}

impl RunRecord {
    pub fn new(run_id: RunId, task_id: TaskId, session_id: SessionId) -> Self {
        Self {
            run_id,
            task_id,
            session_id,
            status: RunStatus::Queued,
            started_at: None,
            ended_at: None,
            parent_run_id: None,
            app_server_turn_id: None,
        }
    }

    pub fn transition_to(&mut self, next: RunStatus) -> Result<(), TransitionError<RunStatus>> {
        if !self.status.can_transition_to(next) {
            return Err(TransitionError { entity: "run", from: self.status, to: next });
        }
        if next == RunStatus::Running && self.started_at.is_none() {
            self.started_at = Some(Utc::now());
        }
        if matches!(next, RunStatus::Completed | RunStatus::Failed | RunStatus::Cancelled) {
            self.ended_at = Some(Utc::now());
        }
        self.status = next;
        Ok(())
    }
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct ProfileRecord {
    pub profile: ProfileName,
    pub account_identity: Option<String>,
    pub codex_home: String,
    pub auth_mode: Option<String>,
    pub preferred_model: Option<String>,
    pub runtime_defaults: BTreeMap<String, String>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub enum CanonicalEventType {
    #[serde(rename = "task.created")]
    TaskCreated,
    #[serde(rename = "task.updated")]
    TaskUpdated,
    #[serde(rename = "task.blocked")]
    TaskBlocked,
    #[serde(rename = "worker.starting")]
    WorkerStarting,
    #[serde(rename = "worker.ready")]
    WorkerReady,
    #[serde(rename = "worker.failed")]
    WorkerFailed,
    #[serde(rename = "session.created")]
    SessionCreated,
    #[serde(rename = "session.reused")]
    SessionReused,
    #[serde(rename = "session.broken")]
    SessionBroken,
    #[serde(rename = "run.started")]
    RunStarted,
    #[serde(rename = "run.output_delta")]
    RunOutputDelta,
    #[serde(rename = "run.requires_auth")]
    RunRequiresAuth,
    #[serde(rename = "run.blocked")]
    RunBlocked,
    #[serde(rename = "run.completed")]
    RunCompleted,
    #[serde(rename = "run.failed")]
    RunFailed,
    #[serde(rename = "run.cancelled")]
    RunCancelled,
    #[serde(rename = "profile.loaded")]
    ProfileLoaded,
    #[serde(rename = "system.warning")]
    SystemWarning,
}

impl CanonicalEventType {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::TaskCreated => "task.created",
            Self::TaskUpdated => "task.updated",
            Self::TaskBlocked => "task.blocked",
            Self::WorkerStarting => "worker.starting",
            Self::WorkerReady => "worker.ready",
            Self::WorkerFailed => "worker.failed",
            Self::SessionCreated => "session.created",
            Self::SessionReused => "session.reused",
            Self::SessionBroken => "session.broken",
            Self::RunStarted => "run.started",
            Self::RunOutputDelta => "run.output_delta",
            Self::RunRequiresAuth => "run.requires_auth",
            Self::RunBlocked => "run.blocked",
            Self::RunCompleted => "run.completed",
            Self::RunFailed => "run.failed",
            Self::RunCancelled => "run.cancelled",
            Self::ProfileLoaded => "profile.loaded",
            Self::SystemWarning => "system.warning",
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum CanonicalEventSource {
    Nucleus,
    AppServer,
    Cli,
    Websocket,
    Cron,
    Hook,
    Fort,
    System,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct EventDataEnvelope {
    pub task_id: Option<TaskId>,
    pub worker_id: Option<WorkerId>,
    pub session_id: Option<SessionId>,
    pub run_id: Option<RunId>,
    pub profile: Option<ProfileName>,
    pub payload: Value,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct CanonicalEvent {
    pub event_id: EventId,
    pub seq: u64,
    pub ts: DateTime<Utc>,
    #[serde(rename = "type")]
    pub event_type: CanonicalEventType,
    pub source: CanonicalEventSource,
    pub data: EventDataEnvelope,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum AppServerTurnStatus {
    Pending,
    Running,
    Completed,
    Failed,
    Interrupted,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum AppServerItemKind {
    Message,
    ToolCall,
    ToolResult,
    Approval,
    System,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum AppServerAuthState {
    Ready,
    Missing,
    Expired,
    Unknown,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct AppServerThreadProjection {
    pub thread_id: String,
    pub title: Option<String>,
    pub model: Option<String>,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct AppServerTurnProjection {
    pub turn_id: String,
    pub thread_id: String,
    pub status: AppServerTurnStatus,
    pub started_at: Option<DateTime<Utc>>,
    pub completed_at: Option<DateTime<Utc>>,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct AppServerItemProjection {
    pub item_id: String,
    pub thread_id: Option<String>,
    pub turn_id: Option<String>,
    pub kind: AppServerItemKind,
    pub role: Option<String>,
    pub text: Option<String>,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct AppServerAccountProjection {
    pub account_id: Option<String>,
    pub account_label: Option<String>,
    pub auth_state: AppServerAuthState,
    pub active_profile: Option<ProfileName>,
}

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
pub struct AppServerConfigProjection {
    pub model: Option<String>,
    pub approval_policy: Option<String>,
    pub sandbox_policy: Option<String>,
    pub cwd: Option<String>,
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeMap;

    use super::{
        AppServerAuthState, AppServerConfigProjection, CanonicalEventType, ProfileName,
        ProfileNameError, RunRecord, RunStatus, SessionLifecycleState, SessionRecord, TaskId,
        TaskRecord, TaskSource, TaskStatus, WorkerId, WorkerRecord, WorkerStatus,
    };

    #[test]
    fn generated_ids_use_nucleus_prefixes() {
        assert!(super::TaskId::generate().as_str().starts_with(super::TaskId::PREFIX));
        assert!(super::WorkerId::generate().as_str().starts_with(super::WorkerId::PREFIX));
        assert!(super::SessionId::generate().as_str().starts_with(super::SessionId::PREFIX));
        assert!(super::RunId::generate().as_str().starts_with(super::RunId::PREFIX));
        assert!(super::EventId::generate().as_str().starts_with(super::EventId::PREFIX));
    }

    #[test]
    fn profile_name_accepts_lowercase_ascii_slug() {
        let profile = ProfileName::new("einsteina").expect("profile");
        assert_eq!(profile.as_str(), "einsteina");
    }

    #[test]
    fn profile_name_rejects_invalid_slug() {
        let err = ProfileName::new("Einstein").expect_err("invalid profile");
        assert_eq!(err, ProfileNameError::Invalid("Einstein".to_owned()));
    }

    #[test]
    fn task_transitions_follow_ticket_rules() {
        let mut task =
            TaskRecord::new(TaskId::generate(), TaskSource::Cli, "Title", "Instructions");
        task.transition_to(TaskStatus::Running, None).expect("queued -> running");
        task.transition_to(TaskStatus::Blocked, Some(super::BlockedReason::AuthRequired))
            .expect("running -> blocked");
        task.transition_to(TaskStatus::Queued, None).expect("blocked -> queued");
        task.transition_to(TaskStatus::Running, None).expect("queued -> running again");
        task.transition_to(TaskStatus::Done, None).expect("running -> done");
        assert!(task.transition_to(TaskStatus::Queued, None).is_err());
    }

    #[test]
    fn task_transitions_allow_reconciliation_from_queue() {
        let mut task =
            TaskRecord::new(TaskId::generate(), TaskSource::Cli, "Title", "Instructions");
        task.transition_to(TaskStatus::Blocked, Some(super::BlockedReason::WorkerUnavailable))
            .expect("queued -> blocked");

        let mut task =
            TaskRecord::new(TaskId::generate(), TaskSource::Cli, "Title", "Instructions");
        task.transition_to(TaskStatus::Failed, None).expect("queued -> failed");
    }

    #[test]
    fn worker_transitions_are_validated() {
        let profile = ProfileName::new("america").expect("profile");
        let mut worker = WorkerRecord {
            worker_id: WorkerId::generate(),
            profile,
            home_dir: None,
            codex_home: "/tmp/worker".to_owned(),
            workdir: None,
            extra_env: BTreeMap::new(),
            status: WorkerStatus::Starting,
            capability_version: None,
            last_heartbeat_at: None,
            effective_account_state: None,
        };
        worker.transition_to(WorkerStatus::Ready).expect("starting -> ready");
        worker.transition_to(WorkerStatus::Failed).expect("ready -> failed");
        worker.transition_to(WorkerStatus::Starting).expect("failed -> starting");
        assert!(worker.transition_to(WorkerStatus::Degraded).is_err());
    }

    #[test]
    fn session_transitions_are_validated() {
        let mut session = SessionRecord::new(super::SessionId::generate(), WorkerId::generate());
        session.transition_to(SessionLifecycleState::Ready).expect("opening -> ready");
        session.transition_to(SessionLifecycleState::Busy).expect("ready -> busy");
        session.transition_to(SessionLifecycleState::Ready).expect("busy -> ready");
        session.transition_to(SessionLifecycleState::Closed).expect("ready -> closed");
        assert!(session.transition_to(SessionLifecycleState::Ready).is_err());
    }

    #[test]
    fn run_transitions_are_validated() {
        let mut run = RunRecord::new(
            super::RunId::generate(),
            TaskId::generate(),
            super::SessionId::generate(),
        );
        run.transition_to(RunStatus::Running).expect("queued -> running");
        run.transition_to(RunStatus::Completed).expect("running -> completed");
        assert!(run.started_at.is_some());
        assert!(run.ended_at.is_some());
        assert!(run.transition_to(RunStatus::Running).is_err());
    }

    #[test]
    fn run_transitions_allow_reconciliation_from_queue() {
        let mut run = RunRecord::new(
            super::RunId::generate(),
            TaskId::generate(),
            super::SessionId::generate(),
        );
        run.transition_to(RunStatus::Blocked).expect("queued -> blocked");

        let mut run = RunRecord::new(
            super::RunId::generate(),
            TaskId::generate(),
            super::SessionId::generate(),
        );
        run.transition_to(RunStatus::Failed).expect("queued -> failed");
    }

    #[test]
    fn canonical_event_types_use_dot_names() {
        assert_eq!(CanonicalEventType::TaskCreated.as_str(), "task.created");
        assert_eq!(CanonicalEventType::RunRequiresAuth.as_str(), "run.requires_auth");
        assert_eq!(CanonicalEventType::RunBlocked.as_str(), "run.blocked");
        assert_eq!(CanonicalEventType::WorkerReady.as_str(), "worker.ready");
    }

    #[test]
    fn app_server_projection_types_compile_as_expected() {
        let profile = ProfileName::new("darmstada").expect("profile");
        let projection = AppServerConfigProjection {
            model: Some("gpt-5.4".to_owned()),
            approval_policy: Some("never".to_owned()),
            sandbox_policy: Some("danger-full-access".to_owned()),
            cwd: Some("/tmp/project".to_owned()),
        };
        let account = super::AppServerAccountProjection {
            account_id: Some("acct_123".to_owned()),
            account_label: Some("primary".to_owned()),
            auth_state: AppServerAuthState::Ready,
            active_profile: Some(profile),
        };
        assert_eq!(projection.model.as_deref(), Some("gpt-5.4"));
        assert_eq!(account.auth_state, AppServerAuthState::Ready);
    }
}
