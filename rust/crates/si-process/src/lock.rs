//! Generic advisory file locks.
//!
//! Moved out of Fort as the surviving lock primitive. A lock file is created
//! `0600` with parents, then held with an exclusive `flock` for the guard's
//! lifetime (Unix; no-op elsewhere, matching prior behavior). Locks are
//! advisory and process-scoped: they serialize SI metadata mutations, not
//! native credential refresh inside a profile home.

use std::fs::{File, OpenOptions};
use std::path::{Path, PathBuf};
use thiserror::Error;

#[derive(Debug, Error)]
pub enum FileLockError {
    #[error("lock path required")]
    MissingPath,
    #[error("create lock directory {path}: {source}")]
    CreateDirectory {
        path: PathBuf,
        #[source]
        source: std::io::Error,
    },
    #[error("open lock file {path}: {source}")]
    Open {
        path: PathBuf,
        #[source]
        source: std::io::Error,
    },
    #[cfg(unix)]
    #[error("lock file {path}: {source}")]
    Lock {
        path: PathBuf,
        #[source]
        source: std::io::Error,
    },
}

/// RAII exclusive lock guard. Dropping releases the `flock`.
#[derive(Debug)]
pub struct FileLock {
    path: PathBuf,
    file: File,
}

impl FileLock {
    /// Acquire the lock, blocking until it is available.
    pub fn acquire(path: &Path) -> Result<Self, FileLockError> {
        let file = open_lock_file(path)?;
        lock_file(file, path.to_path_buf(), false)
    }

    /// Try to acquire the lock without blocking; `Ok(None)` when held.
    pub fn try_acquire(path: &Path) -> Result<Option<Self>, FileLockError> {
        let file = open_lock_file(path)?;
        try_lock_file(file, path.to_path_buf())
    }

    pub fn path(&self) -> &Path {
        &self.path
    }
}

fn open_lock_file(path: &Path) -> Result<File, FileLockError> {
    if path.as_os_str().is_empty() {
        return Err(FileLockError::MissingPath);
    }
    if let Some(dir) = path.parent() {
        std::fs::create_dir_all(dir)
            .map_err(|source| FileLockError::CreateDirectory { path: dir.to_path_buf(), source })?;
    }
    let file = OpenOptions::new()
        .create(true)
        .read(true)
        .write(true)
        .truncate(false)
        .open(path)
        .map_err(|source| FileLockError::Open { path: path.to_path_buf(), source })?;
    set_file_mode(path, 0o600)
        .map_err(|source| FileLockError::Open { path: path.to_path_buf(), source })?;
    Ok(file)
}

fn set_file_mode(path: &Path, mode: u32) -> std::io::Result<()> {
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(path, PermissionsExt::from_mode(mode))?;
    }
    #[cfg(not(unix))]
    {
        let _ = (path, mode);
    }
    Ok(())
}

#[cfg(unix)]
fn lock_file(file: File, path: PathBuf, non_blocking: bool) -> Result<FileLock, FileLockError> {
    use std::os::fd::AsRawFd;
    let operation = if non_blocking { libc::LOCK_EX | libc::LOCK_NB } else { libc::LOCK_EX };
    let rc = unsafe { libc::flock(file.as_raw_fd(), operation) };
    if rc != 0 {
        return Err(FileLockError::Lock { path, source: std::io::Error::last_os_error() });
    }
    Ok(FileLock { path, file })
}

#[cfg(unix)]
fn try_lock_file(file: File, path: PathBuf) -> Result<Option<FileLock>, FileLockError> {
    use std::os::fd::AsRawFd;
    let rc = unsafe { libc::flock(file.as_raw_fd(), libc::LOCK_EX | libc::LOCK_NB) };
    if rc == 0 {
        return Ok(Some(FileLock { path, file }));
    }
    let err = std::io::Error::last_os_error();
    match err.raw_os_error() {
        Some(code) if code == libc::EWOULDBLOCK => Ok(None),
        _ => Err(FileLockError::Lock { path, source: err }),
    }
}

#[cfg(not(unix))]
fn lock_file(file: File, path: PathBuf, _non_blocking: bool) -> Result<FileLock, FileLockError> {
    Ok(FileLock { path, file })
}

#[cfg(not(unix))]
fn try_lock_file(file: File, path: PathBuf) -> Result<Option<FileLock>, FileLockError> {
    Ok(Some(FileLock { path, file }))
}

impl Drop for FileLock {
    fn drop(&mut self) {
        #[cfg(unix)]
        {
            use std::os::fd::AsRawFd;
            let _ = unsafe { libc::flock(self.file.as_raw_fd(), libc::LOCK_UN) };
        }
    }
}

#[cfg(test)]
mod tests {
    use super::FileLock;
    use tempfile::tempdir;

    #[test]
    fn blocking_acquire_serializes_then_releases() {
        let temp = tempdir().expect("tempdir");
        let path = temp.path().join("nested").join("profile.lock");
        {
            let _held = FileLock::acquire(&path).expect("acquire");
            assert!(path.is_file());
            #[cfg(unix)]
            {
                use std::os::unix::fs::PermissionsExt;
                let mode = std::fs::metadata(&path).expect("stat").permissions().mode() & 0o777;
                assert_eq!(mode, 0o600);
            }
        }
        let _reacquired = FileLock::acquire(&path).expect("reacquire after drop");
    }

    #[test]
    fn try_acquire_reports_held_lock_without_blocking() {
        let temp = tempdir().expect("tempdir");
        let path = temp.path().join("busy.lock");
        let _held = FileLock::acquire(&path).expect("acquire");
        let second = FileLock::try_acquire(&path).expect("try should not error");
        #[cfg(unix)]
        assert!(second.is_none(), "held flock must report busy");
        #[cfg(not(unix))]
        assert!(second.is_some());
    }

    #[test]
    fn rejects_empty_lock_path() {
        use std::path::Path;
        assert!(FileLock::acquire(Path::new("")).is_err());
    }
}
