use chrono::{DateTime, SecondsFormat, TimeZone, Utc};
use serde::{Deserialize, Serialize};
use std::fs::{self, File, OpenOptions};
#[cfg(unix)]
use std::os::fd::AsRawFd;
#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use tempfile::NamedTempFile;
use thiserror::Error;

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct SessionSnapshot {
    pub profile_id: String,
    pub agent_id: String,
    pub session_id: Option<String>,
    pub access_expires_at_unix: Option<i64>,
    pub refresh_expires_at_unix: Option<i64>,
}

#[derive(Clone, Debug, Default, Deserialize, Eq, PartialEq, Serialize)]
pub struct PersistedSessionState {
    pub profile_id: String,
    pub agent_id: String,
    #[serde(default)]
    pub session_id: String,
    #[serde(default)]
    pub host: String,
    #[serde(default)]
    pub container_host: String,
    #[serde(default)]
    pub access_token_path: String,
    #[serde(default)]
    pub refresh_token_path: String,
    #[serde(default)]
    pub access_expires_at: String,
    #[serde(default)]
    pub refresh_expires_at: String,
    #[serde(default)]
    pub updated_at: String,
}

#[derive(Clone, Debug, Default, Deserialize, Eq, PartialEq, Serialize)]
pub struct PersistedRuntimeAgentState {
    pub profile_id: String,
    pub pid: i32,
    #[serde(default)]
    pub command_path: String,
    #[serde(default)]
    pub started_at: String,
    #[serde(default)]
    pub updated_at: String,
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct PersistedSessionTransition {
    pub state: PersistedSessionState,
    pub classification: SessionState,
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub enum SessionState {
    BootstrapRequired,
    Resumable(SessionSnapshot),
    Refreshing(SessionSnapshot),
    Revoked { snapshot: Option<SessionSnapshot>, reason: RevocationReason },
    TeardownPending(SessionSnapshot),
    Closed,
}

#[derive(Clone, Copy, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub enum RevocationReason {
    MissingSession,
    RefreshExpired,
    RefreshUnauthorized,
    InvalidRefreshResult,
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct RefreshSuccess {
    pub access_expires_at_unix: i64,
    pub refresh_expires_at_unix: Option<i64>,
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub enum RefreshOutcome {
    Success(RefreshSuccess),
    Unauthorized,
    Retryable,
}

#[derive(Debug, Error, Eq, PartialEq)]
pub enum SessionTransitionError {
    #[error("session snapshot is required")]
    MissingSnapshot,
    #[error("refresh can only start from a resumable or refreshing state")]
    InvalidRefreshStart,
    #[error("refresh result requires a refreshing state")]
    InvalidRefreshResult,
    #[error("teardown requires an active session snapshot")]
    InvalidTeardown,
}

#[derive(Debug, Error)]
pub enum SessionStateFileError {
    #[error("state path required")]
    MissingPath,
    #[error("state file must be a regular file")]
    NotRegularFile,
    #[error("insecure permissions {0:o} (require 0600 or stricter)")]
    InsecurePermissions(u32),
    #[error("create state directory: {0}")]
    CreateDirectory(#[source] std::io::Error),
    #[error("stat state file: {0}")]
    Stat(#[source] std::io::Error),
    #[error("read state file: {0}")]
    Read(#[source] std::io::Error),
    #[error("serialize state file: {0}")]
    Serialize(#[source] serde_json::Error),
    #[error("parse state file: {0}")]
    Parse(#[source] serde_json::Error),
    #[error("create temp state file: {0}")]
    CreateTemp(#[source] std::io::Error),
    #[error("write temp state file: {0}")]
    WriteTemp(#[source] std::io::Error),
    #[error("persist temp state file: {0}")]
    Persist(#[source] std::io::Error),
    #[error("set state file permissions: {0}")]
    SetPermissions(#[source] std::io::Error),
}

#[derive(Debug, Error, Eq, PartialEq)]
pub enum PersistedSessionError {
    #[error("invalid access expiry timestamp {value:?}")]
    InvalidAccessExpiry { value: String },
    #[error("invalid refresh expiry timestamp {value:?}")]
    InvalidRefreshExpiry { value: String },
    #[error("invalid access expiry unix timestamp {value}")]
    InvalidAccessExpiryUnix { value: i64 },
    #[error("invalid refresh expiry unix timestamp {value}")]
    InvalidRefreshExpiryUnix { value: i64 },
}

#[derive(Debug, Error)]
pub enum SessionLockError {
    #[error("lock path required")]
    MissingPath,
    #[error("create lock directory: {0}")]
    CreateDirectory(#[source] std::io::Error),
    #[error("open lock file: {0}")]
    Open(#[source] std::io::Error),
    #[cfg(unix)]
    #[error("lock session file: {0}")]
    Lock(#[source] std::io::Error),
    #[cfg(unix)]
    #[error("unlock session file: {0}")]
    Unlock(#[source] std::io::Error),
}

#[derive(Debug)]
pub struct SessionMutationLock {
    path: PathBuf,
    file: File,
}

impl SessionMutationLock {
    pub fn path(&self) -> &Path {
        &self.path
    }
}

impl PersistedSessionState {
    pub fn normalized(&self) -> Self {
        Self {
            profile_id: self.profile_id.trim().to_owned(),
            agent_id: self.agent_id.trim().to_owned(),
            session_id: self.session_id.trim().to_owned(),
            host: self.host.trim().to_owned(),
            container_host: self.container_host.trim().to_owned(),
            access_token_path: self.access_token_path.trim().to_owned(),
            refresh_token_path: self.refresh_token_path.trim().to_owned(),
            access_expires_at: self.access_expires_at.trim().to_owned(),
            refresh_expires_at: self.refresh_expires_at.trim().to_owned(),
            updated_at: self.updated_at.trim().to_owned(),
        }
    }

    pub fn to_snapshot(&self) -> Result<SessionSnapshot, PersistedSessionError> {
        let normalized = self.normalized();
        Ok(SessionSnapshot {
            profile_id: normalized.profile_id,
            agent_id: normalized.agent_id,
            session_id: non_empty_string(normalized.session_id),
            access_expires_at_unix: parse_optional_rfc3339(
                &normalized.access_expires_at,
                |value| PersistedSessionError::InvalidAccessExpiry { value },
            )?,
            refresh_expires_at_unix: parse_optional_rfc3339(
                &normalized.refresh_expires_at,
                |value| PersistedSessionError::InvalidRefreshExpiry { value },
            )?,
        })
    }
}

impl PersistedRuntimeAgentState {
    pub fn normalized(&self) -> Self {
        Self {
            profile_id: self.profile_id.trim().to_owned(),
            pid: self.pid,
            command_path: self.command_path.trim().to_owned(),
            started_at: self.started_at.trim().to_owned(),
            updated_at: self.updated_at.trim().to_owned(),
        }
    }
}

pub fn classify_session(snapshot: Option<SessionSnapshot>, now_unix: i64) -> SessionState {
    let Some(snapshot) = snapshot else {
        return SessionState::BootstrapRequired;
    };
    let session_id = snapshot.session_id.as_deref().map(str::trim).unwrap_or("");
    if session_id.is_empty() {
        return SessionState::Revoked {
            snapshot: Some(snapshot),
            reason: RevocationReason::MissingSession,
        };
    }
    let refresh_expiry = snapshot.refresh_expires_at_unix.unwrap_or_default();
    if refresh_expiry <= now_unix {
        return SessionState::Revoked {
            snapshot: Some(snapshot),
            reason: RevocationReason::RefreshExpired,
        };
    }
    if snapshot.access_expires_at_unix.unwrap_or_default() <= now_unix {
        return SessionState::Refreshing(snapshot);
    }
    SessionState::Resumable(snapshot)
}

pub fn begin_refresh(state: SessionState) -> Result<SessionState, SessionTransitionError> {
    match state {
        SessionState::Resumable(snapshot) | SessionState::Refreshing(snapshot) => {
            Ok(SessionState::Refreshing(snapshot))
        }
        _ => Err(SessionTransitionError::InvalidRefreshStart),
    }
}

pub fn apply_refresh_outcome(
    state: SessionState,
    outcome: RefreshOutcome,
    now_unix: i64,
) -> Result<SessionState, SessionTransitionError> {
    let SessionState::Refreshing(mut snapshot) = state else {
        return Err(SessionTransitionError::InvalidRefreshResult);
    };
    Ok(match outcome {
        RefreshOutcome::Success(success) => {
            snapshot.access_expires_at_unix = Some(success.access_expires_at_unix);
            if let Some(refresh_expires_at_unix) = success.refresh_expires_at_unix {
                snapshot.refresh_expires_at_unix = Some(refresh_expires_at_unix);
            }
            classify_session(Some(snapshot), now_unix)
        }
        RefreshOutcome::Unauthorized => SessionState::Revoked {
            snapshot: Some(snapshot),
            reason: RevocationReason::RefreshUnauthorized,
        },
        RefreshOutcome::Retryable => SessionState::Refreshing(snapshot),
    })
}

pub fn begin_teardown(state: SessionState) -> Result<SessionState, SessionTransitionError> {
    match state {
        SessionState::Resumable(snapshot)
        | SessionState::Refreshing(snapshot)
        | SessionState::TeardownPending(snapshot) => Ok(SessionState::TeardownPending(snapshot)),
        SessionState::Revoked { snapshot: Some(snapshot), .. } => {
            Ok(SessionState::TeardownPending(snapshot))
        }
        _ => Err(SessionTransitionError::InvalidTeardown),
    }
}

pub fn complete_teardown(state: SessionState) -> Result<SessionState, SessionTransitionError> {
    match state {
        SessionState::TeardownPending(_) => Ok(SessionState::Closed),
        _ => Err(SessionTransitionError::InvalidTeardown),
    }
}

pub fn save_persisted_session_state(
    path: impl AsRef<Path>,
    state: &PersistedSessionState,
) -> Result<(), SessionStateFileError> {
    let path = clean_state_path(path.as_ref())?;
    let dir = path.parent().ok_or(SessionStateFileError::MissingPath)?;
    fs::create_dir_all(dir).map_err(SessionStateFileError::CreateDirectory)?;
    let raw =
        serde_json::to_vec_pretty(&state.normalized()).map_err(SessionStateFileError::Serialize)?;
    let mut raw = raw;
    raw.push(b'\n');

    let mut tmp = NamedTempFile::new_in(dir).map_err(SessionStateFileError::CreateTemp)?;
    set_file_mode(tmp.path(), 0o600)?;
    use std::io::Write as _;
    tmp.write_all(&raw).map_err(SessionStateFileError::WriteTemp)?;
    tmp.flush().map_err(SessionStateFileError::WriteTemp)?;
    tmp.persist(path).map_err(|err| SessionStateFileError::Persist(err.error))?;
    set_file_mode(path, 0o600)?;
    Ok(())
}

pub fn load_persisted_session_state(
    path: impl AsRef<Path>,
) -> Result<PersistedSessionState, SessionStateFileError> {
    let path = clean_state_path(path.as_ref())?;
    let metadata = fs::metadata(path).map_err(SessionStateFileError::Stat)?;
    if !metadata.is_file() {
        return Err(SessionStateFileError::NotRegularFile);
    }
    #[cfg(unix)]
    {
        let mode = metadata.permissions().mode() & 0o777;
        if mode & 0o077 != 0 {
            return Err(SessionStateFileError::InsecurePermissions(mode));
        }
    }
    let raw = fs::read(path).map_err(SessionStateFileError::Read)?;
    let state: PersistedSessionState =
        serde_json::from_slice(&raw).map_err(SessionStateFileError::Parse)?;
    Ok(state.normalized())
}

pub fn classify_persisted_session_state(
    state: &PersistedSessionState,
    now_unix: i64,
) -> Result<SessionState, PersistedSessionError> {
    Ok(classify_session(Some(state.to_snapshot()?), now_unix))
}

pub fn apply_refresh_outcome_to_persisted_session_state(
    state: &PersistedSessionState,
    outcome: RefreshOutcome,
    now_unix: i64,
) -> Result<PersistedSessionTransition, PersistedSessionError> {
    let normalized = state.normalized();
    let classification = apply_refresh_outcome(
        SessionState::Refreshing(normalized.to_snapshot()?),
        outcome.clone(),
        now_unix,
    )
    .map_err(|_| PersistedSessionError::InvalidRefreshExpiry {
        value: normalized.refresh_expires_at.clone(),
    })?;
    let mut next = normalized;
    if let RefreshOutcome::Success(success) = outcome {
        next.access_expires_at = format_unix_rfc3339(success.access_expires_at_unix, |value| {
            PersistedSessionError::InvalidAccessExpiryUnix { value }
        })?;
        if let Some(refresh_expires_at_unix) = success.refresh_expires_at_unix {
            next.refresh_expires_at = format_unix_rfc3339(refresh_expires_at_unix, |value| {
                PersistedSessionError::InvalidRefreshExpiryUnix { value }
            })?;
        }
    }
    Ok(PersistedSessionTransition { state: next, classification })
}

pub fn save_persisted_runtime_agent_state(
    path: impl AsRef<Path>,
    state: &PersistedRuntimeAgentState,
) -> Result<(), SessionStateFileError> {
    let path = clean_state_path(path.as_ref())?;
    let dir = path.parent().ok_or(SessionStateFileError::MissingPath)?;
    fs::create_dir_all(dir).map_err(SessionStateFileError::CreateDirectory)?;
    let raw =
        serde_json::to_vec_pretty(&state.normalized()).map_err(SessionStateFileError::Serialize)?;
    let mut raw = raw;
    raw.push(b'\n');

    let mut tmp = NamedTempFile::new_in(dir).map_err(SessionStateFileError::CreateTemp)?;
    set_file_mode(tmp.path(), 0o600)?;
    use std::io::Write as _;
    tmp.write_all(&raw).map_err(SessionStateFileError::WriteTemp)?;
    tmp.flush().map_err(SessionStateFileError::WriteTemp)?;
    tmp.persist(path).map_err(|err| SessionStateFileError::Persist(err.error))?;
    set_file_mode(path, 0o600)?;
    Ok(())
}

pub fn load_persisted_runtime_agent_state(
    path: impl AsRef<Path>,
) -> Result<PersistedRuntimeAgentState, SessionStateFileError> {
    let path = clean_state_path(path.as_ref())?;
    let metadata = fs::metadata(path).map_err(SessionStateFileError::Stat)?;
    if !metadata.is_file() {
        return Err(SessionStateFileError::NotRegularFile);
    }
    #[cfg(unix)]
    {
        let mode = metadata.permissions().mode() & 0o777;
        if mode & 0o077 != 0 {
            return Err(SessionStateFileError::InsecurePermissions(mode));
        }
    }
    let raw = fs::read(path).map_err(SessionStateFileError::Read)?;
    let state: PersistedRuntimeAgentState =
        serde_json::from_slice(&raw).map_err(SessionStateFileError::Parse)?;
    Ok(state.normalized())
}

pub fn acquire_session_lock(
    path: impl AsRef<Path>,
) -> Result<SessionMutationLock, SessionLockError> {
    let path = clean_lock_path(path.as_ref())?;
    let dir = path.parent().ok_or(SessionLockError::MissingPath)?;
    fs::create_dir_all(dir).map_err(SessionLockError::CreateDirectory)?;
    let file = OpenOptions::new()
        .create(true)
        .read(true)
        .write(true)
        .truncate(false)
        .open(path)
        .map_err(SessionLockError::Open)?;
    set_file_mode(path, 0o600).map_err(lock_mode_error)?;
    lock_file(file, path.to_path_buf(), false)
}

pub fn try_acquire_session_lock(
    path: impl AsRef<Path>,
) -> Result<Option<SessionMutationLock>, SessionLockError> {
    let path = clean_lock_path(path.as_ref())?;
    let dir = path.parent().ok_or(SessionLockError::MissingPath)?;
    fs::create_dir_all(dir).map_err(SessionLockError::CreateDirectory)?;
    let file = OpenOptions::new()
        .create(true)
        .read(true)
        .write(true)
        .truncate(false)
        .open(path)
        .map_err(SessionLockError::Open)?;
    set_file_mode(path, 0o600).map_err(lock_mode_error)?;
    try_lock_file(file, path.to_path_buf())
}

fn clean_state_path(path: &Path) -> Result<&Path, SessionStateFileError> {
    if path.as_os_str().is_empty() {
        return Err(SessionStateFileError::MissingPath);
    }
    Ok(path)
}

fn non_empty_string(value: String) -> Option<String> {
    if value.trim().is_empty() { None } else { Some(value) }
}

fn parse_optional_rfc3339<F>(raw: &str, err: F) -> Result<Option<i64>, PersistedSessionError>
where
    F: Fn(String) -> PersistedSessionError,
{
    let raw = raw.trim();
    if raw.is_empty() {
        return Ok(None);
    }
    let parsed = DateTime::parse_from_rfc3339(raw).map_err(|_| err(raw.to_owned()))?;
    Ok(Some(parsed.timestamp()))
}

fn format_unix_rfc3339<F>(value: i64, err: F) -> Result<String, PersistedSessionError>
where
    F: Fn(i64) -> PersistedSessionError,
{
    let parsed = Utc.timestamp_opt(value, 0).single().ok_or_else(|| err(value))?;
    Ok(parsed.to_rfc3339_opts(SecondsFormat::Secs, true))
}

fn set_file_mode(path: &Path, mode: u32) -> Result<(), SessionStateFileError> {
    #[cfg(unix)]
    {
        let permissions = PermissionsExt::from_mode(mode);
        fs::set_permissions(path, permissions).map_err(SessionStateFileError::SetPermissions)?;
    }
    #[cfg(not(unix))]
    {
        let _ = (path, mode);
    }
    Ok(())
}

fn clean_lock_path(path: &Path) -> Result<&Path, SessionLockError> {
    if path.as_os_str().is_empty() {
        return Err(SessionLockError::MissingPath);
    }
    Ok(path)
}

fn lock_mode_error(err: SessionStateFileError) -> SessionLockError {
    match err {
        SessionStateFileError::SetPermissions(source) => SessionLockError::Open(source),
        other => SessionLockError::Open(std::io::Error::other(other.to_string())),
    }
}

#[cfg(unix)]
fn lock_file(
    file: File,
    path: PathBuf,
    non_blocking: bool,
) -> Result<SessionMutationLock, SessionLockError> {
    let operation = if non_blocking { libc::LOCK_EX | libc::LOCK_NB } else { libc::LOCK_EX };
    let rc = unsafe { libc::flock(file.as_raw_fd(), operation) };
    if rc != 0 {
        return Err(SessionLockError::Lock(std::io::Error::last_os_error()));
    }
    Ok(SessionMutationLock { path, file })
}

#[cfg(unix)]
fn try_lock_file(
    file: File,
    path: PathBuf,
) -> Result<Option<SessionMutationLock>, SessionLockError> {
    let rc = unsafe { libc::flock(file.as_raw_fd(), libc::LOCK_EX | libc::LOCK_NB) };
    if rc == 0 {
        return Ok(Some(SessionMutationLock { path, file }));
    }
    let err = std::io::Error::last_os_error();
    match err.raw_os_error() {
        Some(libc::EWOULDBLOCK) => Ok(None),
        _ => Err(SessionLockError::Lock(err)),
    }
}

#[cfg(not(unix))]
fn lock_file(
    file: File,
    path: PathBuf,
    _non_blocking: bool,
) -> Result<SessionMutationLock, SessionLockError> {
    Ok(SessionMutationLock { path, file })
}

#[cfg(not(unix))]
fn try_lock_file(
    file: File,
    path: PathBuf,
) -> Result<Option<SessionMutationLock>, SessionLockError> {
    Ok(Some(SessionMutationLock { path, file }))
}

impl Drop for SessionMutationLock {
    fn drop(&mut self) {
        #[cfg(unix)]
        {
            let _ = unsafe { libc::flock(self.file.as_raw_fd(), libc::LOCK_UN) };
        }
    }
}

#[cfg(test)]
mod tests {
    use std::fs;
    #[cfg(unix)]
    use std::os::unix::fs::PermissionsExt;
    use tempfile::tempdir;

    use super::{
        PersistedRuntimeAgentState, PersistedSessionError, PersistedSessionState,
        PersistedSessionTransition, RefreshOutcome, RefreshSuccess, RevocationReason,
        SessionSnapshot, SessionState, SessionStateFileError, SessionTransitionError,
        acquire_session_lock, apply_refresh_outcome,
        apply_refresh_outcome_to_persisted_session_state, begin_refresh, begin_teardown,
        classify_persisted_session_state, classify_session, complete_teardown,
        load_persisted_runtime_agent_state, load_persisted_session_state,
        save_persisted_runtime_agent_state, save_persisted_session_state, try_acquire_session_lock,
    };

    fn snapshot() -> SessionSnapshot {
        SessionSnapshot {
            profile_id: "ferma".to_owned(),
            agent_id: "agent-ferma".to_owned(),
            session_id: Some("session-123".to_owned()),
            access_expires_at_unix: Some(200),
            refresh_expires_at_unix: Some(400),
        }
    }

    #[test]
    fn classify_requires_bootstrap_without_snapshot() {
        assert_eq!(classify_session(None, 100), SessionState::BootstrapRequired);
    }

    #[test]
    fn classify_marks_access_expired_session_as_refreshing() {
        let mut state = snapshot();
        state.access_expires_at_unix = Some(90);

        assert_eq!(classify_session(Some(state.clone()), 100), SessionState::Refreshing(state));
    }

    #[test]
    fn classify_marks_refresh_expired_session_as_revoked() {
        let mut state = snapshot();
        state.refresh_expires_at_unix = Some(99);

        assert_eq!(
            classify_session(Some(state.clone()), 100),
            SessionState::Revoked {
                snapshot: Some(state),
                reason: RevocationReason::RefreshExpired,
            }
        );
    }

    #[test]
    fn refresh_success_returns_resumable_state() {
        let state = begin_refresh(SessionState::Resumable(snapshot())).expect("start refresh");

        let refreshed = apply_refresh_outcome(
            state,
            RefreshOutcome::Success(RefreshSuccess {
                access_expires_at_unix: 300,
                refresh_expires_at_unix: Some(500),
            }),
            100,
        )
        .expect("apply refresh");

        match refreshed {
            SessionState::Resumable(snapshot) => {
                assert_eq!(snapshot.access_expires_at_unix, Some(300));
                assert_eq!(snapshot.refresh_expires_at_unix, Some(500));
            }
            other => panic!("unexpected state {other:?}"),
        }
    }

    #[test]
    fn refresh_unauthorized_revokes_session() {
        let state = begin_refresh(SessionState::Resumable(snapshot())).expect("start refresh");

        let refreshed =
            apply_refresh_outcome(state, RefreshOutcome::Unauthorized, 100).expect("apply refresh");

        assert_eq!(
            refreshed,
            SessionState::Revoked {
                snapshot: Some(snapshot()),
                reason: RevocationReason::RefreshUnauthorized,
            }
        );
    }

    #[test]
    fn teardown_transitions_to_closed() {
        let teardown = begin_teardown(SessionState::Resumable(snapshot())).expect("begin teardown");

        assert_eq!(complete_teardown(teardown).expect("complete teardown"), SessionState::Closed);
    }

    #[test]
    fn refresh_result_requires_refreshing_state() {
        let err =
            apply_refresh_outcome(SessionState::BootstrapRequired, RefreshOutcome::Retryable, 100)
                .expect_err("invalid refresh result");

        assert_eq!(err, SessionTransitionError::InvalidRefreshResult);
    }

    #[test]
    fn persisted_state_round_trip_normalizes_whitespace_and_mode() {
        let dir = tempdir().expect("tempdir");
        let path = dir.path().join("session.json");
        let state = PersistedSessionState {
            profile_id: " ferma ".to_owned(),
            agent_id: " agent-ferma ".to_owned(),
            session_id: " session-123 ".to_owned(),
            host: " https://fort.example.test ".to_owned(),
            container_host: " http://fort.internal:8088 ".to_owned(),
            access_token_path: " /tmp/access.token ".to_owned(),
            refresh_token_path: " /tmp/refresh.token ".to_owned(),
            access_expires_at: " 2030-01-01T00:00:00Z ".to_owned(),
            refresh_expires_at: " 2030-02-01T00:00:00Z ".to_owned(),
            updated_at: " 2030-01-01T00:00:00Z ".to_owned(),
        };

        save_persisted_session_state(&path, &state).expect("save session state");
        let loaded = load_persisted_session_state(&path).expect("load session state");

        assert_eq!(loaded.profile_id, "ferma");
        assert_eq!(loaded.agent_id, "agent-ferma");
        assert_eq!(loaded.session_id, "session-123");
        assert_eq!(loaded.host, "https://fort.example.test");
        assert_eq!(loaded.container_host, "http://fort.internal:8088");
        #[cfg(unix)]
        {
            let mode =
                fs::metadata(&path).expect("stat session state").permissions().mode() & 0o777;
            assert_eq!(mode, 0o600);
        }
    }

    #[test]
    fn load_persisted_session_state_rejects_insecure_permissions() {
        let dir = tempdir().expect("tempdir");
        let path = dir.path().join("session.json");
        fs::write(&path, br#"{"profile_id":"ferma","agent_id":"agent-ferma"}"#)
            .expect("write session state");
        #[cfg(unix)]
        fs::set_permissions(&path, PermissionsExt::from_mode(0o644)).expect("chmod session state");

        let err = load_persisted_session_state(&path).expect_err("reject insecure permissions");

        #[cfg(unix)]
        assert_eq!(err.to_string(), SessionStateFileError::InsecurePermissions(0o644).to_string());
    }

    #[test]
    fn classify_persisted_session_state_parses_rfc3339_expiries() {
        let state = PersistedSessionState {
            profile_id: "ferma".to_owned(),
            agent_id: "agent-ferma".to_owned(),
            session_id: "session-123".to_owned(),
            access_expires_at: "1970-01-01T00:01:30Z".to_owned(),
            refresh_expires_at: "1970-01-01T00:06:40Z".to_owned(),
            ..PersistedSessionState::default()
        };

        let classified =
            classify_persisted_session_state(&state, 100).expect("classify persisted session");

        assert_eq!(
            classified,
            SessionState::Refreshing(SessionSnapshot {
                profile_id: "ferma".to_owned(),
                agent_id: "agent-ferma".to_owned(),
                session_id: Some("session-123".to_owned()),
                access_expires_at_unix: Some(90),
                refresh_expires_at_unix: Some(400),
            })
        );
    }

    #[test]
    fn classify_persisted_session_state_rejects_invalid_expiry() {
        let state = PersistedSessionState {
            profile_id: "ferma".to_owned(),
            agent_id: "agent-ferma".to_owned(),
            session_id: "session-123".to_owned(),
            access_expires_at: "not-a-timestamp".to_owned(),
            refresh_expires_at: "1970-01-01T00:06:40Z".to_owned(),
            ..PersistedSessionState::default()
        };

        let err =
            classify_persisted_session_state(&state, 100).expect_err("reject invalid timestamp");

        assert_eq!(
            err,
            PersistedSessionError::InvalidAccessExpiry { value: "not-a-timestamp".to_owned() }
        );
    }

    #[test]
    fn apply_refresh_outcome_to_persisted_session_state_updates_expiries() {
        let state = PersistedSessionState {
            profile_id: "ferma".to_owned(),
            agent_id: "agent-ferma".to_owned(),
            session_id: "session-123".to_owned(),
            access_expires_at: "1970-01-01T00:01:30Z".to_owned(),
            refresh_expires_at: "1970-01-01T00:06:40Z".to_owned(),
            ..PersistedSessionState::default()
        };

        let transitioned = apply_refresh_outcome_to_persisted_session_state(
            &state,
            RefreshOutcome::Success(RefreshSuccess {
                access_expires_at_unix: 500,
                refresh_expires_at_unix: Some(800),
            }),
            100,
        )
        .expect("transition refresh outcome");

        assert_eq!(
            transitioned,
            PersistedSessionTransition {
                state: PersistedSessionState {
                    access_expires_at: "1970-01-01T00:08:20Z".to_owned(),
                    refresh_expires_at: "1970-01-01T00:13:20Z".to_owned(),
                    ..state
                },
                classification: SessionState::Resumable(SessionSnapshot {
                    profile_id: "ferma".to_owned(),
                    agent_id: "agent-ferma".to_owned(),
                    session_id: Some("session-123".to_owned()),
                    access_expires_at_unix: Some(500),
                    refresh_expires_at_unix: Some(800),
                }),
            }
        );
    }

    #[test]
    fn apply_refresh_outcome_to_persisted_session_state_revokes_on_unauthorized() {
        let state = PersistedSessionState {
            profile_id: "ferma".to_owned(),
            agent_id: "agent-ferma".to_owned(),
            session_id: "session-123".to_owned(),
            access_expires_at: "1970-01-01T00:01:30Z".to_owned(),
            refresh_expires_at: "1970-01-01T00:06:40Z".to_owned(),
            ..PersistedSessionState::default()
        };

        let transitioned = apply_refresh_outcome_to_persisted_session_state(
            &state,
            RefreshOutcome::Unauthorized,
            100,
        )
        .expect("transition unauthorized outcome");

        assert_eq!(transitioned.state, state);
        assert_eq!(
            transitioned.classification,
            SessionState::Revoked {
                snapshot: Some(SessionSnapshot {
                    profile_id: "ferma".to_owned(),
                    agent_id: "agent-ferma".to_owned(),
                    session_id: Some("session-123".to_owned()),
                    access_expires_at_unix: Some(90),
                    refresh_expires_at_unix: Some(400),
                }),
                reason: RevocationReason::RefreshUnauthorized,
            }
        );
    }

    #[test]
    fn acquire_session_lock_sets_strict_file_mode() {
        let dir = tempdir().expect("tempdir");
        let path = dir.path().join("runtime.lock");

        let lock = acquire_session_lock(&path).expect("acquire session lock");
        assert_eq!(lock.path(), path.as_path());
        #[cfg(unix)]
        {
            let mode = fs::metadata(&path).expect("stat lock").permissions().mode() & 0o777;
            assert_eq!(mode, 0o600);
        }
    }

    #[test]
    fn try_acquire_session_lock_returns_none_while_lock_is_held() {
        let dir = tempdir().expect("tempdir");
        let path = dir.path().join("runtime.lock");

        let _lock = acquire_session_lock(&path).expect("acquire first lock");
        let second = try_acquire_session_lock(&path).expect("try acquire second lock");

        assert!(second.is_none());
    }

    #[test]
    fn persisted_runtime_agent_state_round_trip_normalizes_fields() {
        let dir = tempdir().expect("tempdir");
        let path = dir.path().join("runtime-agent.json");
        let state = PersistedRuntimeAgentState {
            profile_id: " ferma ".to_owned(),
            pid: 4242,
            command_path: " /tmp/si ".to_owned(),
            started_at: " 2030-01-01T00:00:00Z ".to_owned(),
            updated_at: " 2030-01-01T00:00:01Z ".to_owned(),
        };

        save_persisted_runtime_agent_state(&path, &state).expect("save runtime agent state");
        let loaded = load_persisted_runtime_agent_state(&path).expect("load runtime agent state");

        assert_eq!(loaded.profile_id, "ferma");
        assert_eq!(loaded.pid, 4242);
        assert_eq!(loaded.command_path, "/tmp/si");
        assert_eq!(loaded.started_at, "2030-01-01T00:00:00Z");
        assert_eq!(loaded.updated_at, "2030-01-01T00:00:01Z");
    }
}
