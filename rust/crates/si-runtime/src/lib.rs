use si_rs_docker::BindMount;
use std::env;
use std::os::unix::fs::FileTypeExt;
use std::path::{Path, PathBuf};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ContainerCoreMountPlan {
    pub workspace_host: PathBuf,
    pub workspace_primary_target: PathBuf,
    pub workspace_mirror_target: Option<PathBuf>,
    pub container_home: PathBuf,
    pub include_host_si: bool,
    pub host_vault_env_file: Option<PathBuf>,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct HostMountContext {
    pub home_dir: Option<PathBuf>,
    pub ssh_auth_sock: Option<PathBuf>,
}

impl HostMountContext {
    pub fn from_env() -> Self {
        Self {
            home_dir: env::var_os("HOME").map(PathBuf::from),
            ssh_auth_sock: env::var_os("SSH_AUTH_SOCK").map(PathBuf::from),
        }
    }
}

pub fn build_container_core_mounts(
    plan: &ContainerCoreMountPlan,
    ctx: &HostMountContext,
) -> Vec<BindMount> {
    let Some(workspace_host) = normalize_absolute(&plan.workspace_host) else {
        return Vec::new();
    };

    let primary = normalize_absolute(&plan.workspace_primary_target)
        .filter(|path| path.starts_with("/"))
        .unwrap_or_else(|| PathBuf::from("/workspace"));
    let mirror = plan
        .workspace_mirror_target
        .as_ref()
        .and_then(|path| normalize_absolute(path))
        .filter(|path| path.starts_with("/"));

    let mut mounts = Vec::new();
    append_unique_mount(&mut mounts, BindMount::new(&workspace_host, &primary));
    if let Some(mirror) = mirror {
        if mirror != primary {
            append_unique_mount(&mut mounts, BindMount::new(&workspace_host, mirror));
        }
    }
    if let Some(mount) = infer_development_mount(&workspace_host, &plan.container_home, ctx) {
        append_unique_mount(&mut mounts, mount);
    }
    if let Some(mount) = infer_host_development_mount(&workspace_host, ctx) {
        append_unique_mount(&mut mounts, mount);
    }
    if plan.include_host_si {
        for home in candidate_container_homes(&plan.container_home) {
            if let Some(mount) = host_si_mount(home, ctx) {
                append_unique_mount(&mut mounts, mount);
            }
        }
    }
    if let Some(mount) = host_docker_config_mount(&plan.container_home, ctx) {
        append_unique_mount(&mut mounts, mount);
    }
    for home in candidate_container_homes(&plan.container_home) {
        if let Some(mount) = host_ssh_dir_mount(home, ctx) {
            append_unique_mount(&mut mounts, mount);
        }
    }
    if let Some(mount) = host_ssh_auth_sock_mount(ctx) {
        append_unique_mount(&mut mounts, mount);
    }
    if let Some(mount) = host_si_toolchain_mount(&plan.container_home, ctx) {
        append_unique_mount(&mut mounts, mount);
    }
    if let Some(mount) = host_vault_env_file_mount(plan.host_vault_env_file.as_deref()) {
        append_unique_mount(&mut mounts, mount);
    }
    mounts
}

pub fn infer_development_mount(
    host_path: &Path,
    container_home: &Path,
    ctx: &HostMountContext,
) -> Option<BindMount> {
    let host_path = normalize_absolute(host_path)?;
    let container_home = normalize_absolute(container_home)?;
    let development_host = host_development_dir(&host_path, ctx)?;
    Some(BindMount::new(development_host, container_home.join("Development")))
}

pub fn infer_host_development_mount(host_path: &Path, ctx: &HostMountContext) -> Option<BindMount> {
    let host_path = normalize_absolute(host_path)?;
    let development_host = host_development_dir(&host_path, ctx)?;
    Some(BindMount::new(&development_host, &development_host))
}

fn host_si_mount(container_home: &Path, ctx: &HostMountContext) -> Option<BindMount> {
    let source = host_si_dir_source(ctx)?;
    Some(BindMount::new(source, container_home.join(".si")))
}

fn host_docker_config_mount(container_home: &Path, ctx: &HostMountContext) -> Option<BindMount> {
    let source = host_docker_config_source(ctx)?;
    Some(BindMount::new(source, container_home.join(".docker")))
}

fn host_ssh_dir_mount(container_home: &Path, ctx: &HostMountContext) -> Option<BindMount> {
    let source = host_ssh_dir_source(ctx)?;
    Some(BindMount::new(source, container_home.join(".ssh")))
}

fn host_ssh_auth_sock_mount(ctx: &HostMountContext) -> Option<BindMount> {
    let source = host_ssh_auth_sock_source(ctx)?;
    Some(BindMount::new(&source, &source))
}

fn host_si_toolchain_mount(container_home: &Path, ctx: &HostMountContext) -> Option<BindMount> {
    let source = host_si_toolchain_source(ctx)?;
    Some(
        BindMount::new(
            source,
            container_home.join(".local").join("share").join("si").join("toolchain"),
        )
            .read_only(true),
    )
}

fn host_vault_env_file_mount(host_file: Option<&Path>) -> Option<BindMount> {
    let source = host_file.and_then(normalize_absolute)?;
    if !source.is_file() {
        return None;
    }
    Some(BindMount::new(&source, &source))
}

fn host_si_dir_source(ctx: &HostMountContext) -> Option<PathBuf> {
    let home = ctx.home_dir.as_ref()?;
    let path = home.join(".si");
    path.is_dir().then_some(path)
}

fn host_docker_config_source(ctx: &HostMountContext) -> Option<PathBuf> {
    let home = ctx.home_dir.as_ref()?;
    let path = home.join(".docker");
    path.is_dir().then_some(path)
}

fn host_ssh_dir_source(ctx: &HostMountContext) -> Option<PathBuf> {
    let home = ctx.home_dir.as_ref()?;
    let path = home.join(".ssh");
    path.is_dir().then_some(path)
}

fn host_si_toolchain_source(ctx: &HostMountContext) -> Option<PathBuf> {
    let home = ctx.home_dir.as_ref()?;
    let path = home.join(".local").join("share").join("si").join("toolchain");
    path.is_dir().then_some(path)
}

fn host_ssh_auth_sock_source(ctx: &HostMountContext) -> Option<PathBuf> {
    let source = normalize_absolute(ctx.ssh_auth_sock.as_deref()?)?;
    if !is_socket(&source) {
        return None;
    }
    Some(source)
}

fn host_development_dir(host_path: &Path, ctx: &HostMountContext) -> Option<PathBuf> {
    let home = ctx.home_dir.as_ref()?;
    let development = home.join("Development");
    host_path.strip_prefix(&development).ok()?;
    Some(development)
}

fn candidate_container_homes(container_home: &Path) -> Vec<&Path> {
    if container_home == Path::new("/root") {
        vec![container_home]
    } else {
        vec![container_home, Path::new("/root")]
    }
}

fn append_unique_mount(mounts: &mut Vec<BindMount>, next: BindMount) {
    let source = next.source().to_path_buf();
    let target = next.target().to_path_buf();
    if mounts.iter().any(|mount| mount.source() == source && mount.target() == target) {
        return;
    }
    mounts.push(next);
}

fn normalize_absolute(path: &Path) -> Option<PathBuf> {
    if path.as_os_str().is_empty() {
        return None;
    }
    let path = PathBuf::from(path);
    if path.is_absolute() { Some(path) } else { None }
}

fn is_socket(path: &Path) -> bool {
    std::fs::metadata(path).map(|meta| meta.file_type().is_socket()).unwrap_or(false)
}

#[cfg(test)]
mod tests {
    use super::{
        ContainerCoreMountPlan, HostMountContext, build_container_core_mounts,
        infer_development_mount, infer_host_development_mount,
    };
    use std::os::unix::net::UnixListener;
    use std::path::Path;
    use tempfile::tempdir;

    #[test]
    fn build_container_core_mounts_includes_workspace_mirror_and_host_si() {
        let home = tempdir().expect("tempdir");
        std::fs::create_dir_all(home.path().join(".si")).expect("mkdir .si");
        let workspace = tempdir().expect("tempdir");
        let ctx =
            HostMountContext { home_dir: Some(home.path().to_path_buf()), ssh_auth_sock: None };

        let mounts = build_container_core_mounts(
            &ContainerCoreMountPlan {
                workspace_host: workspace.path().to_path_buf(),
                workspace_primary_target: Path::new("/workspace").to_path_buf(),
                workspace_mirror_target: Some(Path::new("/workspace-mirror").to_path_buf()),
                container_home: Path::new("/home/si").to_path_buf(),
                include_host_si: true,
                host_vault_env_file: None,
            },
            &ctx,
        );

        assert_eq!(mounts.len(), 4);
        assert_eq!(mounts[0].source(), workspace.path());
        assert_eq!(mounts[0].target(), Path::new("/workspace"));
        assert_eq!(mounts[1].source(), workspace.path());
        assert_eq!(mounts[1].target(), Path::new("/workspace-mirror"));
        assert_eq!(mounts[2].source(), &home.path().join(".si"));
        assert_eq!(mounts[2].target(), Path::new("/home/si/.si"));
        assert_eq!(mounts[3].source(), &home.path().join(".si"));
        assert_eq!(mounts[3].target(), Path::new("/root/.si"));
    }

    #[test]
    fn build_container_core_mounts_dedupes_mirror_target() {
        let home = tempdir().expect("tempdir");
        let workspace = tempdir().expect("tempdir");
        let ctx =
            HostMountContext { home_dir: Some(home.path().to_path_buf()), ssh_auth_sock: None };
        let mounts = build_container_core_mounts(
            &ContainerCoreMountPlan {
                workspace_host: workspace.path().to_path_buf(),
                workspace_primary_target: Path::new("/workspace").to_path_buf(),
                workspace_mirror_target: Some(Path::new("/workspace").to_path_buf()),
                container_home: Path::new("/home/si").to_path_buf(),
                include_host_si: false,
                host_vault_env_file: None,
            },
            &ctx,
        );
        assert_eq!(mounts.len(), 1);
    }

    #[test]
    fn build_container_core_mounts_rejects_empty_workspace() {
        let ctx = HostMountContext::default();
        let mounts = build_container_core_mounts(
            &ContainerCoreMountPlan {
                workspace_host: Path::new("").to_path_buf(),
                workspace_primary_target: Path::new("/workspace").to_path_buf(),
                workspace_mirror_target: None,
                container_home: Path::new("/home/si").to_path_buf(),
                include_host_si: false,
                host_vault_env_file: None,
            },
            &ctx,
        );
        assert!(mounts.is_empty());
    }

    #[test]
    fn build_container_core_mounts_includes_vault_env_file_mount() {
        let home = tempdir().expect("tempdir");
        let workspace = tempdir().expect("tempdir");
        let vault = home.path().join(".env.vault");
        std::fs::write(&vault, "KEY=value\n").expect("write vault");
        let ctx =
            HostMountContext { home_dir: Some(home.path().to_path_buf()), ssh_auth_sock: None };
        let mounts = build_container_core_mounts(
            &ContainerCoreMountPlan {
                workspace_host: workspace.path().to_path_buf(),
                workspace_primary_target: Path::new("/workspace").to_path_buf(),
                workspace_mirror_target: None,
                container_home: Path::new("/home/si").to_path_buf(),
                include_host_si: false,
                host_vault_env_file: Some(vault.clone()),
            },
            &ctx,
        );
        assert_eq!(mounts.len(), 2);
        assert_eq!(mounts[1].source(), vault.as_path());
        assert_eq!(mounts[1].target(), vault.as_path());
    }

    #[test]
    fn build_container_core_mounts_includes_development_mirror_mount() {
        let home = tempdir().expect("tempdir");
        let workspace = home.path().join("Development").join("si");
        std::fs::create_dir_all(&workspace).expect("mkdir workspace");
        let ctx =
            HostMountContext { home_dir: Some(home.path().to_path_buf()), ssh_auth_sock: None };
        let mounts = build_container_core_mounts(
            &ContainerCoreMountPlan {
                workspace_host: workspace.clone(),
                workspace_primary_target: Path::new("/workspace").to_path_buf(),
                workspace_mirror_target: Some(Path::new("/home/si/Development/si").to_path_buf()),
                container_home: Path::new("/home/si").to_path_buf(),
                include_host_si: false,
                host_vault_env_file: None,
            },
            &ctx,
        );
        assert_eq!(mounts.len(), 4);
        assert_eq!(mounts[2].source(), &home.path().join("Development"));
        assert_eq!(mounts[2].target(), Path::new("/home/si/Development"));
        assert_eq!(mounts[3].source(), &home.path().join("Development"));
        assert_eq!(mounts[3].target(), &home.path().join("Development"));
    }

    #[test]
    fn build_container_core_mounts_includes_host_docker_and_toolchain_mounts() {
        let home = tempdir().expect("tempdir");
        std::fs::create_dir_all(home.path().join(".si")).expect("mkdir .si");
        std::fs::create_dir_all(home.path().join(".docker")).expect("mkdir .docker");
        std::fs::create_dir_all(
            home.path().join(".local").join("share").join("si").join("toolchain"),
        )
        .expect("mkdir toolchain dir");
        let workspace = tempdir().expect("tempdir");
        let ctx =
            HostMountContext { home_dir: Some(home.path().to_path_buf()), ssh_auth_sock: None };
        let mounts = build_container_core_mounts(
            &ContainerCoreMountPlan {
                workspace_host: workspace.path().to_path_buf(),
                workspace_primary_target: Path::new("/workspace").to_path_buf(),
                workspace_mirror_target: None,
                container_home: Path::new("/home/si").to_path_buf(),
                include_host_si: true,
                host_vault_env_file: None,
            },
            &ctx,
        );
        assert_eq!(mounts.len(), 5);
        assert_eq!(mounts[3].source(), &home.path().join(".docker"));
        assert_eq!(mounts[3].target(), Path::new("/home/si/.docker"));
        assert_eq!(
            mounts[4].source(),
            &home.path().join(".local").join("share").join("si").join("toolchain")
        );
        assert_eq!(mounts[4].target(), Path::new("/home/si/.local/share/si/toolchain"));
        assert!(mounts[4].is_read_only());
    }

    #[test]
    fn build_container_core_mounts_includes_host_ssh_dir_and_agent_socket_mounts() {
        let home = tempdir().expect("tempdir");
        std::fs::create_dir_all(home.path().join(".ssh")).expect("mkdir .ssh");
        let sock_path = home.path().join("ssh-agent.sock");
        let listener = UnixListener::bind(&sock_path).expect("bind unix socket");
        let workspace = tempdir().expect("tempdir");
        let ctx = HostMountContext {
            home_dir: Some(home.path().to_path_buf()),
            ssh_auth_sock: Some(sock_path.clone()),
        };
        let mounts = build_container_core_mounts(
            &ContainerCoreMountPlan {
                workspace_host: workspace.path().to_path_buf(),
                workspace_primary_target: Path::new("/workspace").to_path_buf(),
                workspace_mirror_target: None,
                container_home: Path::new("/home/si").to_path_buf(),
                include_host_si: false,
                host_vault_env_file: None,
            },
            &ctx,
        );
        drop(listener);
        assert_eq!(mounts.len(), 4);
        assert_eq!(mounts[1].source(), &home.path().join(".ssh"));
        assert_eq!(mounts[1].target(), Path::new("/home/si/.ssh"));
        assert_eq!(mounts[2].source(), &home.path().join(".ssh"));
        assert_eq!(mounts[2].target(), Path::new("/root/.ssh"));
        assert_eq!(mounts[3].source(), sock_path.as_path());
        assert_eq!(mounts[3].target(), sock_path.as_path());
    }

    #[test]
    fn infer_development_mount_returns_container_home_development() {
        let home = tempdir().expect("tempdir");
        let host_path = home.path().join("Development").join("si");
        std::fs::create_dir_all(&host_path).expect("mkdir host path");
        let ctx =
            HostMountContext { home_dir: Some(home.path().to_path_buf()), ssh_auth_sock: None };

        let mount =
            infer_development_mount(&host_path, Path::new("/home/si"), &ctx).expect("mount");

        assert_eq!(mount.source(), &home.path().join("Development"));
        assert_eq!(mount.target(), Path::new("/home/si/Development"));
    }

    #[test]
    fn infer_host_development_mount_returns_host_path_target() {
        let home = tempdir().expect("tempdir");
        let host_path = home.path().join("Development").join("si");
        std::fs::create_dir_all(&host_path).expect("mkdir host path");
        let ctx =
            HostMountContext { home_dir: Some(home.path().to_path_buf()), ssh_auth_sock: None };

        let mount = infer_host_development_mount(&host_path, &ctx).expect("mount");

        assert_eq!(mount.source(), &home.path().join("Development"));
        assert_eq!(mount.target(), &home.path().join("Development"));
    }
}
