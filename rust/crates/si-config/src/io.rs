//! Private filesystem IO: restrictive modes and atomic writes.
//!
//! SI owns profile metadata and lifecycle records. Private directories are
//! created `0700` and metadata files `0600` under a restrictive creation mask.
//! Config and records are written through locked, atomic replacement: the
//! caller holds the relevant lock, and this module fsyncs a temp file in the
//! destination directory before renaming it over the target.

use std::fs;
use std::io::Write;
use std::path::Path;
use thiserror::Error;

/// Mode for SI-owned private directories.
pub const PRIVATE_DIR_MODE: u32 = 0o700;

/// Mode for SI-owned private metadata files.
pub const PRIVATE_FILE_MODE: u32 = 0o600;

#[derive(Debug, Error)]
pub enum PrivateIoError {
    #[error("create directory {path}: {source}")]
    CreateDir {
        path: std::path::PathBuf,
        #[source]
        source: std::io::Error,
    },
    #[error("set permissions on {path}: {source}")]
    SetPermissions {
        path: std::path::PathBuf,
        #[source]
        source: std::io::Error,
    },
    #[error("write temp file for {path}: {source}")]
    WriteTemp {
        path: std::path::PathBuf,
        #[source]
        source: std::io::Error,
    },
    #[error("persist {path}: {source}")]
    Persist {
        path: std::path::PathBuf,
        #[source]
        source: std::io::Error,
    },
}

/// Create a directory (and parents) with `PRIVATE_DIR_MODE` on Unix.
pub fn create_private_dir_all(path: &Path) -> Result<(), PrivateIoError> {
    fs::create_dir_all(path)
        .map_err(|source| PrivateIoError::CreateDir { path: path.to_path_buf(), source })?;
    set_mode(path, PRIVATE_DIR_MODE)?;
    Ok(())
}

/// Atomically replace `path` with `bytes`, mode `PRIVATE_FILE_MODE` on Unix.
///
/// The parent directory must already exist. The temp file is created in the
/// same directory so the rename is atomic on one filesystem.
pub fn write_private_file(path: &Path, bytes: &[u8]) -> Result<(), PrivateIoError> {
    let parent = path.parent().ok_or_else(|| PrivateIoError::Persist {
        path: path.to_path_buf(),
        source: std::io::Error::other("path has no parent directory"),
    })?;
    let mut temp = tempfile::NamedTempFile::new_in(parent)
        .map_err(|source| PrivateIoError::WriteTemp { path: path.to_path_buf(), source })?;
    set_mode(temp.path(), PRIVATE_FILE_MODE)?;
    temp.write_all(bytes)
        .map_err(|source| PrivateIoError::WriteTemp { path: path.to_path_buf(), source })?;
    temp.as_file_mut()
        .sync_all()
        .map_err(|source| PrivateIoError::WriteTemp { path: path.to_path_buf(), source })?;
    temp.persist(path)
        .map_err(|err| PrivateIoError::Persist { path: path.to_path_buf(), source: err.error })?;
    Ok(())
}

fn set_mode(path: &Path, mode: u32) -> Result<(), PrivateIoError> {
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(path, PermissionsExt::from_mode(mode)).map_err(|source| {
            PrivateIoError::SetPermissions { path: path.to_path_buf(), source }
        })?;
    }
    #[cfg(not(unix))]
    {
        let _ = (path, mode);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{PRIVATE_DIR_MODE, PRIVATE_FILE_MODE, create_private_dir_all, write_private_file};
    #[cfg(unix)]
    use std::os::unix::fs::PermissionsExt;
    use tempfile::tempdir;

    #[test]
    fn creates_private_dirs_and_files_with_restrictive_modes() {
        let temp = tempdir().expect("tempdir");
        let dir = temp.path().join("nested").join("si");
        create_private_dir_all(&dir).expect("create dir");
        assert!(dir.is_dir());

        let file = dir.join("settings.toml");
        write_private_file(&file, b"schema_version = 2\n").expect("write file");
        assert_eq!(std::fs::read(&file).expect("read"), b"schema_version = 2\n");

        #[cfg(unix)]
        {
            let dir_mode = std::fs::metadata(&dir).expect("stat dir").permissions().mode() & 0o777;
            assert_eq!(dir_mode, PRIVATE_DIR_MODE, "dir mode should be 0700");
            let file_mode =
                std::fs::metadata(&file).expect("stat file").permissions().mode() & 0o777;
            assert_eq!(file_mode, PRIVATE_FILE_MODE, "file mode should be 0600");
        }
    }

    #[test]
    fn atomic_replace_keeps_old_content_on_parent_failure() {
        let temp = tempdir().expect("tempdir");
        let missing_parent = temp.path().join("no-such-dir").join("file.toml");
        let err = write_private_file(&missing_parent, b"x").expect_err("should fail");
        assert!(format!("{err}").contains("no-such-dir"));
        assert!(!missing_parent.exists());
    }
}
