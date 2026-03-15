use si_rs_docker::BindMount;
use si_rs_runtime::{ContainerCoreMountPlan, HostMountContext, build_container_core_mounts};
use std::path::PathBuf;
use thiserror::Error;

pub const DEFAULT_IMAGE: &str = "aureuma/si:local";
pub const DEFAULT_NETWORK: &str = "si";
pub const DEFAULT_WORKSPACE_PRIMARY: &str = "/workspace";
pub const DEFAULT_CONTAINER_HOME: &str = "/home/si";
pub const DEFAULT_SKILLS_VOLUME: &str = "si-codex-skills";

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct SpawnRequest {
    pub name: Option<String>,
    pub profile_id: Option<String>,
    pub image: Option<String>,
    pub network_name: Option<String>,
    pub workspace_host: PathBuf,
    pub workdir: Option<String>,
    pub codex_volume: Option<String>,
    pub skills_volume: Option<String>,
    pub gh_volume: Option<String>,
    pub repo: Option<String>,
    pub gh_pat: Option<String>,
    pub docker_socket: bool,
    pub clean_slate: bool,
    pub detach: bool,
    pub container_home: Option<PathBuf>,
    pub host_vault_env_file: Option<PathBuf>,
    pub include_host_si: bool,
    pub additional_env: Vec<String>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SpawnPlan {
    pub name: String,
    pub container_name: String,
    pub image: String,
    pub network_name: String,
    pub workspace_host: PathBuf,
    pub workspace_primary_target: PathBuf,
    pub workspace_mirror_target: PathBuf,
    pub workdir: PathBuf,
    pub codex_volume: String,
    pub skills_volume: String,
    pub gh_volume: String,
    pub docker_socket: bool,
    pub clean_slate: bool,
    pub detach: bool,
    pub env: Vec<String>,
    pub mounts: Vec<BindMount>,
}

#[derive(Debug, Error, Eq, PartialEq)]
pub enum SpawnPlanError {
    #[error("spawn name or profile is required")]
    MissingName,
    #[error("workspace host must be an absolute directory")]
    InvalidWorkspace,
}

pub fn build_spawn_plan(
    request: &SpawnRequest,
    host_ctx: &HostMountContext,
) -> Result<SpawnPlan, SpawnPlanError> {
    let Some(mut name) = request
        .profile_id
        .as_deref()
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(str::to_owned)
        .or_else(|| {
            request
                .name
                .as_deref()
                .map(str::trim)
                .filter(|value| !value.is_empty())
                .map(str::to_owned)
        })
    else {
        return Err(SpawnPlanError::MissingName);
    };
    if let Some(profile_id) = request.profile_id.as_deref().map(str::trim).filter(|v| !v.is_empty())
    {
        name = profile_id.to_owned();
    }

    let workspace_host = request.workspace_host.clone();
    if !workspace_host.is_absolute() || !workspace_host.is_dir() {
        return Err(SpawnPlanError::InvalidWorkspace);
    }

    let workspace_primary_target = PathBuf::from(DEFAULT_WORKSPACE_PRIMARY);
    let workspace_mirror_target = workspace_host.clone();
    let requested_workdir = request.workdir.as_deref().map(str::trim).unwrap_or("");
    let workdir = if requested_workdir.is_empty() || requested_workdir == DEFAULT_WORKSPACE_PRIMARY
    {
        workspace_mirror_target.clone()
    } else {
        PathBuf::from(requested_workdir)
    };
    let codex_volume =
        default_named_value(request.codex_volume.as_deref(), &format!("si-codex-{name}"));
    let skills_volume =
        default_named_value(request.skills_volume.as_deref(), DEFAULT_SKILLS_VOLUME);
    let gh_volume = default_named_value(request.gh_volume.as_deref(), &format!("si-gh-{name}"));
    let container_home =
        request.container_home.clone().unwrap_or_else(|| PathBuf::from(DEFAULT_CONTAINER_HOME));

    let mut env = vec![
        format!("HOME={}", container_home.display()),
        format!("CODEX_HOME={}/.codex", container_home.display()),
        format!("SI_WORKSPACE_PRIMARY={}", workspace_primary_target.display()),
        format!("SI_WORKSPACE_MIRROR={}", workspace_mirror_target.display()),
        format!("SI_WORKSPACE_HOST={}", workspace_host.display()),
    ];
    if let Some(repo) = request.repo.as_deref().map(str::trim).filter(|v| !v.is_empty()) {
        env.push(format!("SI_REPO={repo}"));
    }
    if let Some(gh_pat) = request.gh_pat.as_deref().map(str::trim).filter(|v| !v.is_empty()) {
        env.push(format!("SI_GH_PAT={gh_pat}"));
        env.push(format!("GH_TOKEN={gh_pat}"));
        env.push(format!("GITHUB_TOKEN={gh_pat}"));
    }
    env.extend(
        request
            .additional_env
            .iter()
            .map(|value| value.trim())
            .filter(|value| !value.is_empty())
            .map(str::to_owned),
    );

    let mounts = build_container_core_mounts(
        &ContainerCoreMountPlan {
            workspace_host: workspace_host.clone(),
            workspace_primary_target: workspace_primary_target.clone(),
            workspace_mirror_target: Some(workspace_mirror_target.clone()),
            container_home: container_home.clone(),
            include_host_si: request.include_host_si,
            host_vault_env_file: request.host_vault_env_file.clone(),
        },
        host_ctx,
    );

    Ok(SpawnPlan {
        name: name.clone(),
        container_name: codex_container_name(&name),
        image: default_named_value(request.image.as_deref(), DEFAULT_IMAGE),
        network_name: default_named_value(request.network_name.as_deref(), DEFAULT_NETWORK),
        workspace_host,
        workspace_primary_target,
        workspace_mirror_target,
        workdir,
        codex_volume,
        skills_volume,
        gh_volume,
        docker_socket: request.docker_socket,
        clean_slate: request.clean_slate,
        detach: request.detach,
        env,
        mounts,
    })
}

pub fn codex_container_name(name: &str) -> String {
    let name = name.trim();
    if name.is_empty() {
        return String::new();
    }
    if name.starts_with("si-codex-") {
        return name.to_owned();
    }
    format!("si-codex-{name}")
}

fn default_named_value(value: Option<&str>, fallback: &str) -> String {
    value.map(str::trim).filter(|value| !value.is_empty()).unwrap_or(fallback).to_owned()
}

#[cfg(test)]
mod tests {
    use super::{
        DEFAULT_SKILLS_VOLUME, SpawnPlanError, SpawnRequest, build_spawn_plan, codex_container_name,
    };
    use si_rs_runtime::HostMountContext;
    use std::path::{Path, PathBuf};
    use tempfile::tempdir;

    #[test]
    fn build_spawn_plan_prefers_profile_id_for_name_and_container() {
        let workspace = tempdir().expect("tempdir");
        let plan = build_spawn_plan(
            &SpawnRequest {
                name: Some("custom".to_owned()),
                profile_id: Some("ferma".to_owned()),
                workspace_host: workspace.path().to_path_buf(),
                detach: true,
                docker_socket: true,
                include_host_si: false,
                ..SpawnRequest::default()
            },
            &HostMountContext::default(),
        )
        .expect("spawn plan");

        assert_eq!(plan.name, "ferma");
        assert_eq!(plan.container_name, "si-codex-ferma");
        assert_eq!(plan.codex_volume, "si-codex-ferma");
        assert_eq!(plan.gh_volume, "si-gh-ferma");
        assert_eq!(plan.skills_volume, DEFAULT_SKILLS_VOLUME);
    }

    #[test]
    fn build_spawn_plan_defaults_workdir_to_workspace_mirror() {
        let workspace = tempdir().expect("tempdir");
        let plan = build_spawn_plan(
            &SpawnRequest {
                name: Some("ferma".to_owned()),
                workspace_host: workspace.path().to_path_buf(),
                workdir: Some("/workspace".to_owned()),
                detach: true,
                docker_socket: true,
                include_host_si: false,
                ..SpawnRequest::default()
            },
            &HostMountContext::default(),
        )
        .expect("spawn plan");

        assert_eq!(plan.workspace_primary_target, PathBuf::from("/workspace"));
        assert_eq!(plan.workspace_mirror_target, workspace.path());
        assert_eq!(plan.workdir, workspace.path());
    }

    #[test]
    fn build_spawn_plan_keeps_explicit_non_default_workdir() {
        let workspace = tempdir().expect("tempdir");
        let plan = build_spawn_plan(
            &SpawnRequest {
                name: Some("ferma".to_owned()),
                workspace_host: workspace.path().to_path_buf(),
                workdir: Some("/custom".to_owned()),
                detach: false,
                docker_socket: false,
                include_host_si: false,
                ..SpawnRequest::default()
            },
            &HostMountContext::default(),
        )
        .expect("spawn plan");

        assert_eq!(plan.workdir, PathBuf::from("/custom"));
        assert!(!plan.detach);
        assert!(!plan.docker_socket);
    }

    #[test]
    fn build_spawn_plan_assembles_workspace_and_repo_env() {
        let workspace = tempdir().expect("tempdir");
        let plan = build_spawn_plan(
            &SpawnRequest {
                name: Some("ferma".to_owned()),
                workspace_host: workspace.path().to_path_buf(),
                repo: Some("acme/repo".to_owned()),
                gh_pat: Some("token-123".to_owned()),
                additional_env: vec!["EXTRA=1".to_owned()],
                detach: true,
                docker_socket: true,
                include_host_si: false,
                ..SpawnRequest::default()
            },
            &HostMountContext::default(),
        )
        .expect("spawn plan");

        assert!(plan.env.contains(&format!("SI_WORKSPACE_HOST={}", workspace.path().display())));
        assert!(plan.env.contains(&format!("SI_WORKSPACE_MIRROR={}", workspace.path().display())));
        assert!(plan.env.contains(&"SI_REPO=acme/repo".to_owned()));
        assert!(plan.env.contains(&"SI_GH_PAT=token-123".to_owned()));
        assert!(plan.env.contains(&"GH_TOKEN=token-123".to_owned()));
        assert!(plan.env.contains(&"GITHUB_TOKEN=token-123".to_owned()));
        assert!(plan.env.contains(&"EXTRA=1".to_owned()));
    }

    #[test]
    fn build_spawn_plan_reuses_runtime_core_mounts() {
        let home = tempdir().expect("tempdir");
        std::fs::create_dir_all(home.path().join(".si")).expect("mkdir .si");
        let workspace = tempdir().expect("tempdir");
        let ctx =
            HostMountContext { home_dir: Some(home.path().to_path_buf()), ssh_auth_sock: None };
        let plan = build_spawn_plan(
            &SpawnRequest {
                name: Some("ferma".to_owned()),
                workspace_host: workspace.path().to_path_buf(),
                include_host_si: true,
                detach: true,
                docker_socket: true,
                ..SpawnRequest::default()
            },
            &ctx,
        )
        .expect("spawn plan");

        assert_eq!(plan.mounts[0].source(), workspace.path());
        assert_eq!(plan.mounts[0].target(), Path::new("/workspace"));
        assert_eq!(plan.mounts[1].source(), workspace.path());
        assert_eq!(plan.mounts[1].target(), workspace.path());
        assert!(plan.mounts.iter().any(|mount| mount.target() == Path::new("/home/si/.si")));
    }

    #[test]
    fn build_spawn_plan_rejects_missing_name() {
        let workspace = tempdir().expect("tempdir");
        let err = build_spawn_plan(
            &SpawnRequest {
                workspace_host: workspace.path().to_path_buf(),
                ..SpawnRequest::default()
            },
            &HostMountContext::default(),
        )
        .expect_err("missing name");

        assert_eq!(err, SpawnPlanError::MissingName);
    }

    #[test]
    fn build_spawn_plan_rejects_invalid_workspace() {
        let err = build_spawn_plan(
            &SpawnRequest {
                name: Some("ferma".to_owned()),
                workspace_host: PathBuf::from("relative"),
                ..SpawnRequest::default()
            },
            &HostMountContext::default(),
        )
        .expect_err("invalid workspace");

        assert_eq!(err, SpawnPlanError::InvalidWorkspace);
    }

    #[test]
    fn codex_container_name_preserves_existing_prefix() {
        assert_eq!(codex_container_name("si-codex-ferma"), "si-codex-ferma");
        assert_eq!(codex_container_name("ferma"), "si-codex-ferma");
    }
}
