use thiserror::Error;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SessionSnapshot {
    pub profile_id: String,
    pub agent_id: String,
    pub session_id: Option<String>,
    pub access_expires_at_unix: Option<i64>,
    pub refresh_expires_at_unix: Option<i64>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum SessionState {
    BootstrapRequired,
    Resumable(SessionSnapshot),
    Refreshing(SessionSnapshot),
    Revoked { snapshot: Option<SessionSnapshot>, reason: RevocationReason },
    TeardownPending(SessionSnapshot),
    Closed,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum RevocationReason {
    MissingSession,
    RefreshExpired,
    RefreshUnauthorized,
    InvalidRefreshResult,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RefreshSuccess {
    pub access_expires_at_unix: i64,
    pub refresh_expires_at_unix: Option<i64>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
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

#[cfg(test)]
mod tests {
    use super::{
        RefreshOutcome, RefreshSuccess, RevocationReason, SessionSnapshot, SessionState,
        SessionTransitionError, apply_refresh_outcome, begin_refresh, begin_teardown,
        classify_session, complete_teardown,
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
}
