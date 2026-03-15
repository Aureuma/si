use std::path::{Path, PathBuf};

use thiserror::Error;

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
    mounts: Vec<BindMount>,
    env: Vec<(String, String)>,
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
        self.mounts.push(mount);
        self
    }

    pub fn env(mut self, key: impl Into<String>, value: impl Into<String>) -> Self {
        self.env.push((key.into(), value.into()));
        self
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
        for mount in &self.mounts {
            mount.validate().map_err(ContainerSpecError::BindMount)?;
        }
        Ok(())
    }

    pub fn docker_run_args(&self) -> Result<Vec<String>, ContainerSpecError> {
        self.validate()?;

        let mut args = vec!["run".to_owned(), "--rm".to_owned()];
        if let Some(name) = &self.name {
            args.push("--name".to_owned());
            args.push(name.clone());
        }
        for (key, value) in &self.env {
            args.push("-e".to_owned());
            args.push(format!("{key}={value}"));
        }
        for mount in &self.mounts {
            args.push("--mount".to_owned());
            args.push(mount.docker_mount_arg().map_err(ContainerSpecError::BindMount)?);
        }
        args.push(self.image.trim().to_owned());
        Ok(args)
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
}

pub fn docker_binary_path() -> &'static Path {
    Path::new("docker")
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
            .env("PROFILE", "ferma")
            .mount(BindMount::new(temp_dir.path(), "/workspace").read_only(true));

        let args = spec.docker_run_args().expect("docker args");

        assert_eq!(args[0], "run");
        assert!(args.contains(&"--rm".to_owned()));
        assert!(args.contains(&"--name".to_owned()));
        assert!(args.contains(&"si-codex-ferma".to_owned()));
        assert!(args.contains(&"-e".to_owned()));
        assert!(args.contains(&"PROFILE=ferma".to_owned()));
        assert!(args.contains(&"--mount".to_owned()));
        assert!(args.iter().any(|arg| arg.contains("type=bind")));
        assert_eq!(args.last().map(String::as_str), Some("ghcr.io/aureuma/si:latest"));
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
    fn exposes_docker_binary_name() {
        assert_eq!(docker_binary_path(), Path::new("docker"));
    }
}
