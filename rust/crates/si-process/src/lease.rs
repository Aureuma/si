//! Named profile/worker leases over advisory file locks.
//!
//! A lease serializes SI-side mutations for one profile (enrollment changes,
//! worker admission). It never serializes native credential refresh inside a
//! profile home: native renewal happens in the profile's own process while the
//! SI worker-admission lease is held, and same-profile concurrency stays
//! disabled until native locking behavior is proven.

use crate::lock::{FileLock, FileLockError};
use std::path::{Path, PathBuf};
use thiserror::Error;

#[derive(Debug, Error)]
pub enum LeaseError {
    #[error("profile is busy for {purpose} (lock {lock_path})")]
    Busy { lock_path: PathBuf, purpose: String },
    #[error(transparent)]
    Lock(#[from] FileLockError),
}

/// RAII lease guard. Dropping releases the underlying file lock.
#[derive(Debug)]
pub struct Lease {
    lock: FileLock,
    purpose: String,
}

impl Lease {
    /// Acquire the lease, blocking until it is available.
    pub fn acquire(lock_path: &Path, purpose: &str) -> Result<Self, LeaseError> {
        Ok(Self { lock: FileLock::acquire(lock_path)?, purpose: purpose.to_owned() })
    }

    /// Try once; `Ok(None)` when the lease is held elsewhere.
    pub fn try_acquire(lock_path: &Path, purpose: &str) -> Result<Option<Self>, LeaseError> {
        Ok(FileLock::try_acquire(lock_path)?.map(|lock| Self { lock, purpose: purpose.to_owned() }))
    }

    /// Try once; fail with a structured busy error when held elsewhere.
    pub fn acquire_or_busy(lock_path: &Path, purpose: &str) -> Result<Self, LeaseError> {
        match Self::try_acquire(lock_path, purpose)? {
            Some(lease) => Ok(lease),
            None => Err(LeaseError::Busy {
                lock_path: lock_path.to_path_buf(),
                purpose: purpose.to_owned(),
            }),
        }
    }

    pub fn lock_path(&self) -> &Path {
        self.lock.path()
    }

    pub fn purpose(&self) -> &str {
        &self.purpose
    }
}

#[cfg(test)]
mod tests {
    use super::{Lease, LeaseError};
    use tempfile::tempdir;

    #[test]
    fn busy_lease_reports_lock_path_and_purpose() {
        let temp = tempdir().expect("tempdir");
        let path = temp.path().join("codex").join("personal.lock");
        let _held = Lease::acquire(&path, "login").expect("acquire");
        match Lease::acquire_or_busy(&path, "logout") {
            Err(LeaseError::Busy { lock_path, purpose }) => {
                assert_eq!(lock_path, path);
                assert_eq!(purpose, "logout");
            }
            #[cfg(not(unix))]
            Ok(_) => {}
            other => panic!("unexpected outcome: {other:?}"),
        }
    }

    #[test]
    fn released_lease_can_be_reacquired() {
        let temp = tempdir().expect("tempdir");
        let path = temp.path().join("personal.lock");
        drop(Lease::acquire(&path, "login").expect("acquire"));
        let lease = Lease::acquire_or_busy(&path, "login").expect("reacquire");
        assert_eq!(lease.purpose(), "login");
    }
}
