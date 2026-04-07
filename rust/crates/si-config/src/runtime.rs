use crate::settings::Settings;
use std::fs;
use std::path::{Path, PathBuf};
use thiserror::Error;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum WorkspaceScope {
    Codex,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ResolvedPathSource {
    Flag,
    Env,
    Settings,
    Cwd,
    Repo,
    Bundled,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ResolvedDirectory {
    pub path: PathBuf,
    pub source: ResolvedPathSource,
    pub stale_settings: bool,
}

#[derive(Debug, Error)]
pub enum RuntimePathError {
    #[error("{label} path is empty")]
    EmptyPath { label: String },
    #[error("resolve {label} path {path}: {source}")]
    ResolvePath {
        label: String,
        path: String,
        #[source]
        source: std::io::Error,
    },
    #[error("{label} path {path} does not exist")]
    MissingDirectory { label: String, path: PathBuf },
    #[error("{label} path {path} is not a directory")]
    NotDirectory { label: String, path: PathBuf },
    #[error(
        "unable to determine workspace root from {cwd}; pass --root or set [paths].workspace_root"
    )]
    WorkspaceRootInference { cwd: PathBuf },
}

pub fn resolve_workspace_directory(
    scope: WorkspaceScope,
    flag_value: Option<&Path>,
    env_value: Option<&Path>,
    settings: Option<&Settings>,
    cwd: &Path,
) -> Result<ResolvedDirectory, RuntimePathError> {
    let label = format!("{} workspace", workspace_scope_label(scope));
    if let Some(path) = flag_value {
        return Ok(ResolvedDirectory {
            path: resolve_directory_path(path, &label)?,
            source: ResolvedPathSource::Flag,
            stale_settings: false,
        });
    }
    if let Some(path) = env_value {
        return Ok(ResolvedDirectory {
            path: resolve_directory_path(path, &label)?,
            source: ResolvedPathSource::Env,
            stale_settings: false,
        });
    }

    let mut stale_settings = false;
    if let Some(settings) = settings
        && let Some(configured) = workspace_default_value(settings, scope)
    {
        match resolve_directory_path(Path::new(configured), &label) {
            Ok(path) => {
                return Ok(ResolvedDirectory {
                    path,
                    source: ResolvedPathSource::Settings,
                    stale_settings: false,
                });
            }
            Err(_) => stale_settings = true,
        }
    }

    if let Ok(path) = infer_workspace_root_from_cwd(cwd) {
        return Ok(ResolvedDirectory { path, source: ResolvedPathSource::Cwd, stale_settings });
    }

    Ok(ResolvedDirectory {
        path: resolve_directory_path(cwd, &label)?,
        source: ResolvedPathSource::Cwd,
        stale_settings,
    })
}

pub fn resolve_workspace_root_directory(
    flag_value: Option<&Path>,
    env_value: Option<&Path>,
    settings: Option<&Settings>,
    cwd: &Path,
) -> Result<ResolvedDirectory, RuntimePathError> {
    if let Some(path) = flag_value {
        return Ok(ResolvedDirectory {
            path: resolve_directory_path(path, "workspace root")?,
            source: ResolvedPathSource::Flag,
            stale_settings: false,
        });
    }
    if let Some(path) = env_value {
        return Ok(ResolvedDirectory {
            path: resolve_directory_path(path, "workspace root")?,
            source: ResolvedPathSource::Env,
            stale_settings: false,
        });
    }

    let mut stale_settings = false;
    if let Some(settings) = settings
        && let Some(configured) = settings.paths.workspace_root.as_deref()
    {
        match resolve_directory_path(Path::new(configured), "workspace root") {
            Ok(path) => {
                return Ok(ResolvedDirectory {
                    path,
                    source: ResolvedPathSource::Settings,
                    stale_settings: false,
                });
            }
            Err(_) => stale_settings = true,
        }
    }

    Ok(ResolvedDirectory {
        path: infer_workspace_root_from_cwd(cwd)?,
        source: ResolvedPathSource::Cwd,
        stale_settings,
    })
}

pub fn infer_workspace_root_from_cwd(cwd: &Path) -> Result<PathBuf, RuntimePathError> {
    let cwd = resolve_directory_path(cwd, "workspace root")?;
    if has_direct_git_children(&cwd) {
        return Ok(cwd);
    }
    let repo_root =
        git_repo_root_from(&cwd).map_err(|_| RuntimePathError::WorkspaceRootInference { cwd })?;
    let parent = repo_root
        .parent()
        .ok_or(RuntimePathError::WorkspaceRootInference { cwd: repo_root.clone() })?;
    if parent == repo_root {
        return Err(RuntimePathError::WorkspaceRootInference { cwd: repo_root });
    }
    resolve_directory_path(parent, "workspace root")
}

pub fn git_repo_root_from(start: &Path) -> Result<PathBuf, RuntimePathError> {
    let start = resolve_directory_path(start, "git repo root")?;
    let mut current = start.clone();
    loop {
        if is_git_repo_dir(&current) {
            return Ok(current);
        }
        let Some(parent) = current.parent() else {
            break;
        };
        if parent == current {
            break;
        }
        current = parent.to_path_buf();
    }
    Err(RuntimePathError::WorkspaceRootInference { cwd: start })
}

pub fn has_direct_git_children(dir: &Path) -> bool {
    let Ok(entries) = fs::read_dir(dir) else {
        return false;
    };
    for entry in entries.flatten() {
        let path = entry.path();
        if path.is_dir() && is_git_repo_dir(&path) {
            return true;
        }
    }
    false
}

fn resolve_directory_path(path: &Path, label: &str) -> Result<PathBuf, RuntimePathError> {
    if path.as_os_str().is_empty() {
        return Err(RuntimePathError::EmptyPath { label: label.to_owned() });
    }
    let abs = path
        .canonicalize()
        .or_else(|err| {
            if err.kind() == std::io::ErrorKind::NotFound {
                Ok(path_absolutize(path))
            } else {
                Err(err)
            }
        })
        .map_err(|source| RuntimePathError::ResolvePath {
            label: label.to_owned(),
            path: path.display().to_string(),
            source,
        })?;
    if !abs.exists() {
        return Err(RuntimePathError::MissingDirectory { label: label.to_owned(), path: abs });
    }
    if !abs.is_dir() {
        return Err(RuntimePathError::NotDirectory { label: label.to_owned(), path: abs });
    }
    Ok(abs)
}

fn path_absolutize(path: &Path) -> PathBuf {
    if path.is_absolute() {
        path.to_path_buf()
    } else {
        std::env::current_dir().unwrap_or_else(|_| PathBuf::from(".")).join(path)
    }
}

fn is_git_repo_dir(dir: &Path) -> bool {
    dir.join(".git").exists()
}

fn workspace_scope_label(scope: WorkspaceScope) -> &'static str {
    match scope {
        WorkspaceScope::Codex => "codex",
    }
}

fn workspace_default_value(settings: &Settings, scope: WorkspaceScope) -> Option<&str> {
    match scope {
        WorkspaceScope::Codex => settings.codex.workspace.as_deref(),
    }
}

#[cfg(test)]
mod tests {
    use super::{
        ResolvedPathSource, WorkspaceScope, infer_workspace_root_from_cwd,
        resolve_workspace_directory, resolve_workspace_root_directory,
    };
    use crate::settings::Settings;
    use std::fs;
    use std::path::PathBuf;
    use tempfile::tempdir;

    #[test]
    fn resolve_workspace_directory_falls_back_from_stale_settings_to_workspace_root() {
        let root = tempdir().expect("tempdir");
        let repo = root.path().join("si");
        let cwd = repo.join("tools").join("si");
        fs::create_dir_all(repo.join(".git")).expect("mkdir .git");
        fs::create_dir_all(&cwd).expect("mkdir cwd");
        let mut settings = Settings::with_home_defaults(root.path());
        settings.codex.workspace = Some(root.path().join("missing").display().to_string());

        let resolved =
            resolve_workspace_directory(WorkspaceScope::Codex, None, None, Some(&settings), &cwd)
                .expect("resolve workspace");

        assert_eq!(resolved.path, root.path());
        assert!(resolved.stale_settings);
        assert_eq!(resolved.source, ResolvedPathSource::Cwd);
    }

    #[test]
    fn resolve_workspace_directory_falls_back_to_cwd_outside_repo_workspace() {
        let cwd = tempdir().expect("tempdir");

        let resolved =
            resolve_workspace_directory(WorkspaceScope::Codex, None, None, None, cwd.path())
                .expect("resolve workspace");

        assert_eq!(resolved.path, cwd.path());
        assert!(!resolved.stale_settings);
        assert_eq!(resolved.source, ResolvedPathSource::Cwd);
    }

    #[test]
    fn resolve_workspace_root_directory_falls_back_from_stale_settings_to_repo_parent() {
        let root = tempdir().expect("tempdir");
        let repo = root.path().join("si");
        let cwd = repo.join("tools").join("si");
        fs::create_dir_all(repo.join(".git")).expect("mkdir .git");
        fs::create_dir_all(&cwd).expect("mkdir cwd");

        let mut settings = Settings::with_home_defaults(root.path());
        settings.paths.workspace_root = Some(root.path().join("missing").display().to_string());

        let resolved =
            resolve_workspace_root_directory(None, None, Some(&settings), &cwd).expect("root");

        assert_eq!(resolved.path, root.path());
        assert!(resolved.stale_settings);
        assert_eq!(resolved.source, ResolvedPathSource::Cwd);
    }

    #[test]
    fn infer_workspace_root_from_direct_git_children() {
        let root = tempdir().expect("tempdir");
        fs::create_dir_all(root.path().join("si").join(".git")).expect("mkdir .git");

        let resolved = infer_workspace_root_from_cwd(root.path()).expect("infer workspace root");

        assert_eq!(resolved, PathBuf::from(root.path()));
    }
}
