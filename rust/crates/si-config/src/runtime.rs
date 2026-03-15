use crate::settings::Settings;
use std::fs;
use std::path::{Path, PathBuf};
use thiserror::Error;

const BUNDLED_DYAD_CONFIG_TEMPLATE: &str = r#"# managed by si-codex-init
#
# Shared Codex defaults for si dyads.
# Seeded from /opt/si/data/codex/shared/actor/config.toml

model = "__CODEX_MODEL__"
model_reasoning_effort = "__CODEX_REASONING_EFFORT__"

# Codex deprecated [features].web_search_request; configure web search at the top level.
# Values: "live" | "cached" | "disabled"
web_search = "live"

[sandbox_workspace_write]
network_access = true

[si]
dyad = "__DYAD_NAME__"
member = "__DYAD_MEMBER__"
role = "__ROLE__"
initialized_utc = "__INITIALIZED_UTC__"
"#;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum WorkspaceScope {
    Codex,
    Dyad,
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
    #[error("create bundled dyad configs dir {path}: {source}")]
    CreateBundledConfigs {
        path: PathBuf,
        #[source]
        source: std::io::Error,
    },
    #[error("write bundled dyad template {path}: {source}")]
    WriteBundledTemplate {
        path: PathBuf,
        #[source]
        source: std::io::Error,
    },
    #[error("read bundled dyad template {path}: {source}")]
    ReadBundledTemplate {
        path: PathBuf,
        #[source]
        source: std::io::Error,
    },
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
    if let Some(settings) = settings {
        if let Some(configured) = workspace_default_value(settings, scope) {
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
    if let Some(settings) = settings {
        if let Some(configured) = settings.paths.workspace_root.as_deref() {
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
    }

    Ok(ResolvedDirectory {
        path: infer_workspace_root_from_cwd(cwd)?,
        source: ResolvedPathSource::Cwd,
        stale_settings,
    })
}

pub fn resolve_dyad_configs_directory(
    flag_value: Option<&Path>,
    env_value: Option<&Path>,
    settings: Option<&Settings>,
    workspace_host: &Path,
    home: &Path,
) -> Result<ResolvedDirectory, RuntimePathError> {
    let current_dir = std::env::current_dir().ok();
    let exe_dir = std::env::current_exe().ok().and_then(|exe| exe.parent().map(Path::to_path_buf));
    resolve_dyad_configs_directory_with_sources(
        flag_value,
        env_value,
        settings,
        workspace_host,
        home,
        current_dir.as_deref(),
        exe_dir.as_deref(),
    )
}

fn resolve_dyad_configs_directory_with_sources(
    flag_value: Option<&Path>,
    env_value: Option<&Path>,
    settings: Option<&Settings>,
    workspace_host: &Path,
    home: &Path,
    current_dir: Option<&Path>,
    exe_dir: Option<&Path>,
) -> Result<ResolvedDirectory, RuntimePathError> {
    if let Some(path) = flag_value {
        return Ok(ResolvedDirectory {
            path: resolve_directory_path(path, "dyad configs")?,
            source: ResolvedPathSource::Flag,
            stale_settings: false,
        });
    }
    if let Some(path) = env_value {
        return Ok(ResolvedDirectory {
            path: resolve_directory_path(path, "dyad configs")?,
            source: ResolvedPathSource::Env,
            stale_settings: false,
        });
    }

    let mut stale_settings = false;
    if let Some(settings) = settings {
        if let Some(configured) = settings.dyad.configs.as_deref() {
            match resolve_directory_path(Path::new(configured), "dyad configs") {
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
    }

    for candidate in repo_config_candidates(workspace_host, current_dir, exe_dir) {
        if let Ok(path) = resolve_directory_path(&candidate, "dyad configs") {
            return Ok(ResolvedDirectory {
                path,
                source: ResolvedPathSource::Repo,
                stale_settings,
            });
        }
    }

    Ok(ResolvedDirectory {
        path: ensure_bundled_dyad_configs_dir(home)?,
        source: ResolvedPathSource::Bundled,
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
        WorkspaceScope::Dyad => "dyad",
    }
}

fn workspace_default_value(settings: &Settings, scope: WorkspaceScope) -> Option<&str> {
    match scope {
        WorkspaceScope::Codex => settings.codex.workspace.as_deref(),
        WorkspaceScope::Dyad => settings.dyad.workspace.as_deref(),
    }
}

fn repo_config_candidates(
    workspace_host: &Path,
    current_dir: Option<&Path>,
    exe_dir: Option<&Path>,
) -> Vec<PathBuf> {
    let mut candidates = Vec::new();
    if let Ok(root) = si_repo_root_from(workspace_host) {
        candidates.push(root.join("configs"));
    }
    if let Some(cwd) = current_dir {
        if let Ok(root) = si_repo_root_from(cwd) {
            let candidate = root.join("configs");
            if !candidates.contains(&candidate) {
                candidates.push(candidate);
            }
        }
    }
    if let Some(parent) = exe_dir {
        if let Ok(root) = si_repo_root_from(parent) {
            let candidate = root.join("configs");
            if !candidates.contains(&candidate) {
                candidates.push(candidate);
            }
        }
    }
    candidates
}

fn ensure_bundled_dyad_configs_dir(home: &Path) -> Result<PathBuf, RuntimePathError> {
    let dir = home.join(".si").join("configs");
    fs::create_dir_all(&dir)
        .map_err(|source| RuntimePathError::CreateBundledConfigs { path: dir.clone(), source })?;
    let template_path = dir.join("codex-config.template.toml");
    ensure_file_content(&template_path, BUNDLED_DYAD_CONFIG_TEMPLATE)?;
    Ok(dir)
}

fn si_repo_root_from(start: &Path) -> Result<PathBuf, RuntimePathError> {
    let start = resolve_directory_path(start, "repo root")?;
    let mut current = start.clone();
    loop {
        if current.join("configs").is_dir() && current.join("agents").is_dir() {
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

fn ensure_file_content(path: &Path, content: &str) -> Result<(), RuntimePathError> {
    match fs::read_to_string(path) {
        Ok(current) if current == content => return Ok(()),
        Ok(_) | Err(_) => {}
    }
    fs::write(path, content).map_err(|source| RuntimePathError::WriteBundledTemplate {
        path: path.to_path_buf(),
        source,
    })
}

#[cfg(test)]
mod tests {
    use super::{
        ResolvedPathSource, WorkspaceScope, infer_workspace_root_from_cwd,
        resolve_dyad_configs_directory, resolve_dyad_configs_directory_with_sources,
        resolve_workspace_directory, resolve_workspace_root_directory,
    };
    use crate::settings::Settings;
    use std::fs;
    use std::path::PathBuf;
    use tempfile::tempdir;

    struct CwdGuard(PathBuf);

    impl Drop for CwdGuard {
        fn drop(&mut self) {
            let _ = std::env::set_current_dir(&self.0);
        }
    }

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
    fn resolve_dyad_configs_directory_falls_back_to_bundled_configs() {
        let home = tempdir().expect("tempdir");
        let settings = Settings::with_home_defaults(home.path());
        let original_cwd = std::env::current_dir().expect("current dir");
        let _guard = CwdGuard(original_cwd);
        std::env::set_current_dir(home.path()).expect("chdir temp home");

        let resolved = resolve_dyad_configs_directory_with_sources(
            None,
            None,
            Some(&settings),
            home.path(),
            home.path(),
            Some(home.path()),
            None,
        )
        .expect("resolve dyad configs");

        assert_eq!(resolved.source, ResolvedPathSource::Bundled);
        assert!(resolved.path.starts_with(home.path().join(".si")));
        assert!(resolved.path.join("codex-config.template.toml").exists());
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

    #[test]
    fn resolve_dyad_configs_directory_prefers_repo_configs() {
        let workspace = tempdir().expect("tempdir");
        let repo = workspace.path().join("si");
        let cwd = repo.join("tools").join("si");
        let configs = repo.join("configs");
        let agents = repo.join("agents");
        fs::create_dir_all(repo.join(".git")).expect("mkdir .git");
        fs::create_dir_all(&cwd).expect("mkdir cwd");
        fs::create_dir_all(&configs).expect("mkdir configs");
        fs::create_dir_all(&agents).expect("mkdir agents");

        let settings = Settings::with_home_defaults(workspace.path());
        let resolved =
            resolve_dyad_configs_directory(None, None, Some(&settings), &cwd, workspace.path())
                .expect("resolve dyad configs");

        assert_eq!(resolved.path, configs);
        assert_eq!(resolved.source, ResolvedPathSource::Repo);
    }
}
