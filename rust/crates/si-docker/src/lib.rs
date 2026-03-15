use std::path::{Path, PathBuf};

use si_rs_process::CommandSpec;
use thiserror::Error;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ContainerAction {
    Start,
    Stop,
}

impl ContainerAction {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Start => "start",
            Self::Stop => "stop",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BindMount {
    source: PathBuf,
    target: PathBuf,
    read_only: bool,
}

impl BindMount {
    pub fn new(source: impl Into<PathBuf>, target: impl Into<PathBuf>) -> Self {
        Self { source: source.into(), target: target.into(), read_only: false }
    }

    pub fn read_only(mut self, read_only: bool) -> Self {
        self.read_only = read_only;
        self
    }

    pub fn validate(&self) -> Result<(), BindMountError> {
        if !self.source.is_absolute() {
            return Err(BindMountError::SourceNotAbsolute { path: self.source.clone() });
        }
        if !self.source.exists() {
            return Err(BindMountError::SourceMissing { path: self.source.clone() });
        }
        if !self.target.is_absolute() {
            return Err(BindMountError::TargetNotAbsolute { path: self.target.clone() });
        }
        Ok(())
    }

    pub fn source(&self) -> &Path {
        &self.source
    }

    pub fn target(&self) -> &Path {
        &self.target
    }

    pub fn is_read_only(&self) -> bool {
        self.read_only
    }

    pub fn docker_mount_arg(&self) -> Result<String, BindMountError> {
        self.validate()?;
        let mut options = vec![
            "type=bind".to_owned(),
            format!("src={}", self.source.display()),
            format!("dst={}", self.target.display()),
        ];
        if self.read_only {
            options.push("readonly".to_owned());
        }
        Ok(options.join(","))
    }
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ContainerSpec {
    image: String,
    name: Option<String>,
    bind_mounts: Vec<BindMount>,
    volume_mounts: Vec<VolumeMount>,
    published_ports: Vec<PublishedPort>,
    labels: Vec<(String, String)>,
    env: Vec<(String, String)>,
    working_dir: Option<PathBuf>,
    command: Vec<String>,
    restart_policy: Option<String>,
    network: Option<String>,
    user: Option<String>,
    detach: bool,
    auto_remove: bool,
}

impl ContainerSpec {
    pub fn new(image: impl Into<String>) -> Self {
        Self { image: image.into(), ..Self::default() }
    }

    pub fn name(mut self, name: impl Into<String>) -> Self {
        self.name = Some(name.into());
        self
    }

    pub fn mount(mut self, mount: BindMount) -> Self {
        self.bind_mounts.push(mount);
        self
    }

    pub fn volume_mount(mut self, mount: VolumeMount) -> Self {
        self.volume_mounts.push(mount);
        self
    }

    pub fn env(mut self, key: impl Into<String>, value: impl Into<String>) -> Self {
        self.env.push((key.into(), value.into()));
        self
    }

    pub fn label(mut self, key: impl Into<String>, value: impl Into<String>) -> Self {
        self.labels.push((key.into(), value.into()));
        self
    }

    pub fn workdir(mut self, path: impl Into<PathBuf>) -> Self {
        self.working_dir = Some(path.into());
        self
    }

    pub fn command<I, S>(mut self, values: I) -> Self
    where
        I: IntoIterator<Item = S>,
        S: Into<String>,
    {
        self.command = values.into_iter().map(Into::into).collect();
        self
    }

    pub fn restart_policy(mut self, value: impl Into<String>) -> Self {
        self.restart_policy = Some(value.into());
        self
    }

    pub fn network(mut self, value: impl Into<String>) -> Self {
        self.network = Some(value.into());
        self
    }

    pub fn user(mut self, value: impl Into<String>) -> Self {
        self.user = Some(value.into());
        self
    }

    pub fn detach(mut self, value: bool) -> Self {
        self.detach = value;
        self
    }

    pub fn auto_remove(mut self, value: bool) -> Self {
        self.auto_remove = value;
        self
    }

    pub fn published_port(mut self, port: PublishedPort) -> Self {
        self.published_ports.push(port);
        self
    }

    pub fn image(&self) -> &str {
        &self.image
    }

    pub fn name_ref(&self) -> Option<&str> {
        self.name.as_deref()
    }

    pub fn bind_mounts(&self) -> &[BindMount] {
        &self.bind_mounts
    }

    pub fn volume_mounts(&self) -> &[VolumeMount] {
        &self.volume_mounts
    }

    pub fn env_vars(&self) -> &[(String, String)] {
        &self.env
    }

    pub fn labels(&self) -> &[(String, String)] {
        &self.labels
    }

    pub fn working_dir(&self) -> Option<&Path> {
        self.working_dir.as_deref()
    }

    pub fn command_args(&self) -> &[String] {
        &self.command
    }

    pub fn restart_policy_ref(&self) -> Option<&str> {
        self.restart_policy.as_deref()
    }

    pub fn network_ref(&self) -> Option<&str> {
        self.network.as_deref()
    }

    pub fn user_ref(&self) -> Option<&str> {
        self.user.as_deref()
    }

    pub fn published_ports(&self) -> &[PublishedPort] {
        &self.published_ports
    }

    pub fn detach_enabled(&self) -> bool {
        self.detach
    }

    pub fn auto_remove_enabled(&self) -> bool {
        self.auto_remove
    }

    pub fn validate(&self) -> Result<(), ContainerSpecError> {
        if self.image.trim().is_empty() {
            return Err(ContainerSpecError::MissingImage);
        }
        if let Some(name) = &self.name {
            if name.trim().is_empty() {
                return Err(ContainerSpecError::InvalidName);
            }
        }
        for mount in &self.bind_mounts {
            mount.validate().map_err(ContainerSpecError::BindMount)?;
        }
        for mount in &self.volume_mounts {
            mount.validate().map_err(ContainerSpecError::VolumeMount)?;
        }
        for port in &self.published_ports {
            port.validate().map_err(ContainerSpecError::PublishedPort)?;
        }
        if let Some(path) = &self.working_dir {
            if !path.is_absolute() {
                return Err(ContainerSpecError::InvalidWorkingDir { path: path.clone() });
            }
        }
        Ok(())
    }

    pub fn docker_run_args(&self) -> Result<Vec<String>, ContainerSpecError> {
        self.validate()?;

        let mut args = vec!["run".to_owned()];
        if self.auto_remove {
            args.push("--rm".to_owned());
        }
        if self.detach {
            args.push("-d".to_owned());
        }
        if let Some(name) = &self.name {
            args.push("--name".to_owned());
            args.push(name.clone());
        }
        if let Some(restart) = &self.restart_policy {
            args.push("--restart".to_owned());
            args.push(restart.clone());
        }
        if let Some(network) = &self.network {
            args.push("--network".to_owned());
            args.push(network.clone());
        }
        if let Some(path) = &self.working_dir {
            args.push("-w".to_owned());
            args.push(path.display().to_string());
        }
        if let Some(user) = &self.user {
            args.push("--user".to_owned());
            args.push(user.clone());
        }
        for (key, value) in &self.labels {
            args.push("--label".to_owned());
            args.push(format!("{key}={value}"));
        }
        for (key, value) in &self.env {
            args.push("-e".to_owned());
            args.push(format!("{key}={value}"));
        }
        for port in &self.published_ports {
            args.push("-p".to_owned());
            args.push(port.docker_publish_arg().map_err(ContainerSpecError::PublishedPort)?);
        }
        for mount in &self.bind_mounts {
            args.push("--mount".to_owned());
            args.push(mount.docker_mount_arg().map_err(ContainerSpecError::BindMount)?);
        }
        for mount in &self.volume_mounts {
            args.push("--mount".to_owned());
            args.push(mount.docker_mount_arg().map_err(ContainerSpecError::VolumeMount)?);
        }
        args.push(self.image.trim().to_owned());
        args.extend(self.command.iter().cloned());
        Ok(args)
    }

    pub fn docker_run_command(
        &self,
        docker_program: impl Into<String>,
    ) -> Result<CommandSpec, ContainerSpecError> {
        Ok(CommandSpec::new(docker_program).args(self.docker_run_args()?))
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PublishedPort {
    host_ip: String,
    host_port: String,
    container_port: u16,
}

impl PublishedPort {
    pub fn new(host_port: impl Into<String>, container_port: u16) -> Self {
        Self { host_ip: "127.0.0.1".to_owned(), host_port: host_port.into(), container_port }
    }

    pub fn host_ip(mut self, value: impl Into<String>) -> Self {
        self.host_ip = value.into();
        self
    }

    pub fn validate(&self) -> Result<(), PublishedPortError> {
        if self.host_port.trim().is_empty() {
            return Err(PublishedPortError::MissingHostPort);
        }
        Ok(())
    }

    pub fn host_ip_ref(&self) -> &str {
        &self.host_ip
    }

    pub fn host_port(&self) -> &str {
        &self.host_port
    }

    pub fn container_port(&self) -> u16 {
        self.container_port
    }

    pub fn docker_publish_arg(&self) -> Result<String, PublishedPortError> {
        self.validate()?;
        Ok(format!("{}:{}:{}", self.host_ip.trim(), self.host_port.trim(), self.container_port))
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VolumeMount {
    source: String,
    target: PathBuf,
    read_only: bool,
}

impl VolumeMount {
    pub fn new(source: impl Into<String>, target: impl Into<PathBuf>) -> Self {
        Self { source: source.into(), target: target.into(), read_only: false }
    }

    pub fn read_only(mut self, read_only: bool) -> Self {
        self.read_only = read_only;
        self
    }

    pub fn validate(&self) -> Result<(), VolumeMountError> {
        if self.source.trim().is_empty() {
            return Err(VolumeMountError::MissingSource);
        }
        if !self.target.is_absolute() {
            return Err(VolumeMountError::TargetNotAbsolute { path: self.target.clone() });
        }
        Ok(())
    }

    pub fn source(&self) -> &str {
        &self.source
    }

    pub fn target(&self) -> &Path {
        &self.target
    }

    pub fn is_read_only(&self) -> bool {
        self.read_only
    }

    pub fn docker_mount_arg(&self) -> Result<String, VolumeMountError> {
        self.validate()?;
        let mut options = vec![
            "type=volume".to_owned(),
            format!("src={}", self.source.trim()),
            format!("dst={}", self.target.display()),
        ];
        if self.read_only {
            options.push("readonly".to_owned());
        }
        Ok(options.join(","))
    }
}

#[derive(Debug, Error, PartialEq, Eq)]
pub enum BindMountError {
    #[error("bind source path must be absolute: {path}")]
    SourceNotAbsolute { path: PathBuf },
    #[error("bind source path does not exist: {path}")]
    SourceMissing { path: PathBuf },
    #[error("bind target path must be absolute: {path}")]
    TargetNotAbsolute { path: PathBuf },
}

#[derive(Debug, Error, PartialEq, Eq)]
pub enum ContainerSpecError {
    #[error("container image is required")]
    MissingImage,
    #[error("container name must not be empty")]
    InvalidName,
    #[error(transparent)]
    BindMount(#[from] BindMountError),
    #[error(transparent)]
    VolumeMount(#[from] VolumeMountError),
    #[error(transparent)]
    PublishedPort(#[from] PublishedPortError),
    #[error("container working directory must be absolute: {path}")]
    InvalidWorkingDir { path: PathBuf },
}

#[derive(Debug, Error, PartialEq, Eq)]
pub enum VolumeMountError {
    #[error("volume source name is required")]
    MissingSource,
    #[error("volume target path must be absolute: {path}")]
    TargetNotAbsolute { path: PathBuf },
}

#[derive(Debug, Error, PartialEq, Eq)]
pub enum PublishedPortError {
    #[error("published host port is required")]
    MissingHostPort,
}

#[derive(Debug, Error, PartialEq, Eq)]
pub enum ContainerActionError {
    #[error("container name is required")]
    MissingContainerName,
}

pub fn docker_binary_path() -> &'static Path {
    Path::new("docker")
}

pub fn docker_container_action_command(
    docker_program: impl Into<String>,
    action: ContainerAction,
    container_name: impl Into<String>,
) -> Result<CommandSpec, ContainerActionError> {
    let container_name = container_name.into();
    if container_name.trim().is_empty() {
        return Err(ContainerActionError::MissingContainerName);
    }
    Ok(CommandSpec::new(docker_program).args([action.as_str().to_owned(), container_name]))
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[test]
    fn renders_docker_run_args_with_bind_mounts() {
        let temp_dir = tempdir().expect("tempdir");
        let spec = ContainerSpec::new("ghcr.io/aureuma/si:latest")
            .name("si-codex-ferma")
            .auto_remove(false)
            .detach(true)
            .restart_policy("unless-stopped")
            .network("si")
            .workdir("/workspace")
            .user("root")
            .label("si.component", "codex")
            .env("PROFILE", "ferma")
            .published_port(PublishedPort::new("3000", 3000))
            .mount(BindMount::new(temp_dir.path(), "/workspace").read_only(true))
            .volume_mount(VolumeMount::new("si-codex-ferma", "/home/si/.codex"))
            .command(["bash", "-lc", "sleep infinity"]);

        let args = spec.docker_run_args().expect("docker args");

        assert_eq!(args[0], "run");
        assert!(args.contains(&"-d".to_owned()));
        assert!(args.contains(&"--name".to_owned()));
        assert!(args.contains(&"--restart".to_owned()));
        assert!(args.contains(&"unless-stopped".to_owned()));
        assert!(args.contains(&"--network".to_owned()));
        assert!(args.contains(&"si".to_owned()));
        assert!(args.contains(&"-w".to_owned()));
        assert!(args.contains(&"/workspace".to_owned()));
        assert!(args.contains(&"--user".to_owned()));
        assert!(args.contains(&"root".to_owned()));
        assert!(args.contains(&"--label".to_owned()));
        assert!(args.contains(&"si.component=codex".to_owned()));
        assert!(args.contains(&"-p".to_owned()));
        assert!(args.contains(&"127.0.0.1:3000:3000".to_owned()));
        assert!(args.contains(&"si-codex-ferma".to_owned()));
        assert!(args.contains(&"-e".to_owned()));
        assert!(args.contains(&"PROFILE=ferma".to_owned()));
        assert!(args.contains(&"--mount".to_owned()));
        assert!(args.iter().any(|arg| arg.contains("type=bind")));
        assert!(args.iter().any(|arg| arg.contains("type=volume")));
        assert_eq!(args.last().map(String::as_str), Some("sleep infinity"));
    }

    #[test]
    fn rejects_missing_bind_source() {
        let mount = BindMount::new("/missing/workspace", "/workspace");

        let err = mount.validate().expect_err("missing bind source");

        assert_eq!(
            err,
            BindMountError::SourceMissing { path: PathBuf::from("/missing/workspace") }
        );
    }

    #[test]
    fn rejects_relative_bind_target() {
        let temp_dir = tempdir().expect("tempdir");
        let mount = BindMount::new(temp_dir.path(), "workspace");

        let err = mount.validate().expect_err("relative bind target");

        assert_eq!(err, BindMountError::TargetNotAbsolute { path: PathBuf::from("workspace") });
    }

    #[test]
    fn rejects_empty_container_image() {
        let err = ContainerSpec::new("   ").docker_run_args().expect_err("missing image");

        assert_eq!(err, ContainerSpecError::MissingImage);
    }

    #[test]
    fn rejects_relative_working_dir() {
        let err = ContainerSpec::new("ghcr.io/aureuma/si:latest")
            .workdir("workspace")
            .docker_run_args()
            .expect_err("relative workdir");

        assert_eq!(err, ContainerSpecError::InvalidWorkingDir { path: PathBuf::from("workspace") });
    }

    #[test]
    fn rejects_missing_volume_name() {
        let err =
            VolumeMount::new("   ", "/workspace").docker_mount_arg().expect_err("missing volume");

        assert_eq!(err, VolumeMountError::MissingSource);
    }

    #[test]
    fn rejects_missing_published_host_port() {
        let err = PublishedPort::new("   ", 3000).docker_publish_arg().expect_err("missing port");

        assert_eq!(err, PublishedPortError::MissingHostPort);
    }

    #[test]
    fn exposes_docker_binary_name() {
        assert_eq!(docker_binary_path(), Path::new("docker"));
    }

    #[test]
    fn builds_docker_run_command_spec() {
        let temp_dir = tempdir().expect("tempdir");
        let spec = ContainerSpec::new("ghcr.io/aureuma/si:latest")
            .name("si-codex-ferma")
            .auto_remove(false)
            .detach(true)
            .mount(BindMount::new(temp_dir.path(), "/workspace"))
            .command(["bash", "-lc", "sleep infinity"]);

        let command = spec.docker_run_command("docker").expect("command spec");

        assert_eq!(command.program(), "docker");
        assert!(command.args_slice().contains(&"run".to_owned()));
        assert!(command.args_slice().contains(&"-d".to_owned()));
    }

    #[test]
    fn builds_docker_start_command() {
        let command =
            docker_container_action_command("docker", ContainerAction::Start, "si-codex-ferma")
                .expect("start command");

        assert_eq!(command.program(), "docker");
        assert_eq!(command.args_slice(), ["start", "si-codex-ferma"]);
    }

    #[test]
    fn rejects_empty_container_name_for_container_action() {
        let err = docker_container_action_command("docker", ContainerAction::Stop, "   ")
            .expect_err("missing container name");

        assert_eq!(err, ContainerActionError::MissingContainerName);
    }
}
