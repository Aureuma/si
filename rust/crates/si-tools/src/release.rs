//! Development-only release, packaging, and installer tooling.
//!
//! Extracted verbatim from the former `si build` implementation (Phase A3).
//! This module backs the `si-release` binary and is not part of the shipped
//! `si` product surface.

use anyhow::{Context, Result, anyhow};
use clap::{ArgAction, Args, Subcommand};
use flate2::Compression;
use flate2::write::GzEncoder;
use regex::Regex;
use reqwest::blocking::Client as BlockingHttpClient;
use serde_json::Value;
use sha2::{Digest, Sha256};
use si_config::runtime::git_repo_root_from;
use std::fs;
use std::fs::File;
use std::io::{self, Read, Write};
#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::process::Command as StdCommand;
use tar::Builder as TarBuilder;

#[derive(Debug, Subcommand)]
pub enum BuildCommand {
    #[command(name = "self")]
    Self_ {
        #[command(flatten)]
        args: BuildSelfArgs,
    },
    #[command(name = "installer")]
    Installer {
        #[command(subcommand)]
        command: BuildInstallerCommand,
    },
    #[command(name = "npm")]
    Npm {
        #[command(subcommand)]
        command: BuildNpmCommand,
    },
    #[command(name = "homebrew")]
    Homebrew {
        #[command(subcommand)]
        command: BuildHomebrewCommand,
    },
}

#[derive(Debug, Args)]
pub struct BuildCargoOptions {
    #[arg(long = "target-dir")]
    target_dir: Option<PathBuf>,
    #[arg(long, action = ArgAction::SetTrue)]
    timings: bool,
}

#[derive(Debug, Args)]
pub struct BuildSelfBuildArgs {
    #[arg(long)]
    repo: Option<PathBuf>,
    #[arg(long = "no-upgrade")]
    no_upgrade: bool,
    #[arg(long = "output")]
    output: Option<PathBuf>,
    #[arg(long = "install-path")]
    install_path: Option<PathBuf>,
    #[arg(long)]
    quiet: bool,
    #[command(flatten)]
    cargo: BuildCargoOptions,
}

#[derive(Debug, Args)]
pub struct BuildSelfUpgradeArgs {
    #[arg(long)]
    repo: Option<PathBuf>,
    #[arg(long = "install-path")]
    install_path: Option<PathBuf>,
    #[arg(long)]
    quiet: bool,
    #[command(flatten)]
    cargo: BuildCargoOptions,
}

#[derive(Debug, Args)]
pub struct BuildSelfCheckArgs {
    #[arg(long)]
    repo: Option<PathBuf>,
    #[arg(long)]
    quiet: bool,
    #[command(flatten)]
    cargo: BuildCargoOptions,
}

#[derive(Debug, Args)]
pub struct BuildSelfRunArgs {
    #[arg(long)]
    repo: Option<PathBuf>,
    #[command(flatten)]
    cargo: BuildCargoOptions,
    #[arg(trailing_var_arg = true, allow_hyphen_values = true)]
    args: Vec<String>,
}

#[derive(Debug, Args)]
pub struct BuildSelfArgs {
    #[command(subcommand)]
    command: Option<BuildSelfCommand>,
    #[command(flatten)]
    default_build: BuildSelfBuildArgs,
}

#[derive(Debug, Subcommand)]
pub enum BuildSelfCommand {
    #[command(name = "build")]
    Build(BuildSelfBuildArgs),
    #[command(name = "check")]
    Check(BuildSelfCheckArgs),
    #[command(name = "upgrade")]
    Upgrade(BuildSelfUpgradeArgs),
    #[command(name = "run")]
    Run(BuildSelfRunArgs),
    #[command(name = "asset", alias = "releaseasset", alias = "release-asset")]
    ReleaseAsset {
        #[arg(long)]
        repo_root: Option<PathBuf>,
        #[arg(long)]
        version: String,
        #[arg(
            long = "target",
            value_name = "TARGET",
            help = "Release target id: linux-amd64, linux-arm64, darwin-amd64, or darwin-arm64."
        )]
        target: String,
        #[arg(long = "out-dir")]
        out_dir: Option<PathBuf>,
    },
    #[command(
        name = "assets",
        alias = "releaseassets",
        alias = "release-assets",
        alias = "release"
    )]
    ReleaseAssets {
        #[arg(long)]
        repo: Option<PathBuf>,
        #[arg(long)]
        version: Option<String>,
        #[arg(long = "out-dir")]
        out_dir: Option<PathBuf>,
    },
    #[command(
        name = "validate",
        alias = "validatereleaseversion",
        alias = "validate-release-version"
    )]
    ValidateReleaseVersion {
        #[arg(long)]
        tag: String,
    },
    #[command(name = "verify", alias = "verifyreleaseassets", alias = "verify-release-assets")]
    VerifyReleaseAssets {
        #[arg(long)]
        version: String,
        #[arg(long = "out-dir")]
        out_dir: PathBuf,
    },
}

#[derive(Debug, Subcommand)]
#[allow(clippy::large_enum_variant)]
pub enum BuildInstallerCommand {
    #[command(name = "settings", alias = "settingshelper", alias = "settings-helper")]
    SettingsHelper {
        #[arg(long = "settings")]
        settings: PathBuf,
        #[arg(long = "default-browser")]
        default_browser: String,
        #[arg(long = "print", action = ArgAction::SetTrue)]
        print: bool,
        #[arg(long = "check", action = ArgAction::SetTrue)]
        check: bool,
    },
    #[command(name = "run")]
    Run {
        #[arg(long, default_value = "local")]
        backend: String,
        #[arg(long = "source-dir")]
        source_dir: Option<PathBuf>,
        #[arg(long, default_value = "Aureuma/si")]
        repo: String,
        #[arg(long = "repo-url")]
        repo_url: Option<String>,
        #[arg(long, default_value = "main")]
        ref_: String,
        #[arg(long)]
        version: Option<String>,
        #[arg(long = "install-dir")]
        install_dir: Option<PathBuf>,
        #[arg(long = "install-path")]
        install_path: Option<PathBuf>,
        #[arg(long, action = ArgAction::SetTrue)]
        force: bool,
        #[arg(long, action = ArgAction::SetTrue)]
        uninstall: bool,
        #[arg(long = "toolchain-mode", default_value = "auto")]
        toolchain_mode: String,
        #[arg(long = "with-buildx", action = ArgAction::SetTrue)]
        with_buildx: bool,
        #[arg(long = "no-buildx", action = ArgAction::SetTrue)]
        no_buildx: bool,
        #[arg(long = "os")]
        os_override: Option<String>,
        #[arg(long = "arch")]
        arch_override: Option<String>,
        #[arg(long = "tmp-dir")]
        tmp_dir: Option<PathBuf>,
        #[arg(short = 'y', long = "yes", action = ArgAction::SetTrue)]
        yes: bool,
        #[arg(long = "dry-run", action = ArgAction::SetTrue)]
        dry_run: bool,
        #[arg(long, action = ArgAction::SetTrue)]
        quiet: bool,
        #[arg(long = "no-path-hint", action = ArgAction::SetTrue)]
        no_path_hint: bool,
    },
    #[command(name = "host", alias = "smokehost", alias = "smoke-host")]
    SmokeHost,
    #[command(name = "npm", alias = "smokenpm", alias = "smoke-npm")]
    SmokeNpm,
    #[command(name = "homebrew", alias = "smokehomebrew", alias = "smoke-homebrew")]
    SmokeHomebrew,
}

#[derive(Debug, Subcommand)]
pub enum BuildNpmCommand {
    #[command(name = "package", alias = "buildpackage", alias = "build-package")]
    BuildPackage {
        #[arg(long = "repo-root")]
        repo_root: Option<PathBuf>,
        #[arg(long)]
        version: Option<String>,
        #[arg(long = "out-dir")]
        out_dir: Option<PathBuf>,
    },
    #[command(name = "publish", alias = "publishpackage", alias = "publish-package")]
    PublishPackage {
        #[arg(long = "repo-root")]
        repo_root: Option<PathBuf>,
        #[arg(long)]
        version: Option<String>,
        #[arg(long = "out-dir")]
        out_dir: Option<PathBuf>,
        #[arg(long = "token-env", default_value = "NPM_TOKEN")]
        token_env: String,
        #[arg(long = "dry-run", action = ArgAction::SetTrue)]
        dry_run: bool,
    },
}

#[derive(Debug, Subcommand)]
pub enum BuildHomebrewCommand {
    #[command(name = "core", alias = "rendercoreformula", alias = "render-core-formula")]
    RenderCoreFormula {
        #[arg(long = "repo-root")]
        repo_root: Option<PathBuf>,
        #[arg(long)]
        version: Option<String>,
        #[arg(long)]
        output: PathBuf,
        #[arg(long, default_value = "Aureuma/si")]
        repo: String,
    },
    #[command(name = "tap", alias = "rendertapformula", alias = "render-tap-formula")]
    RenderTapFormula {
        #[arg(long = "repo-root")]
        repo_root: Option<PathBuf>,
        #[arg(long)]
        version: Option<String>,
        #[arg(long)]
        checksums: PathBuf,
        #[arg(long)]
        output: PathBuf,
        #[arg(long, default_value = "Aureuma/si")]
        repo: String,
    },
    #[command(name = "update", alias = "updatetaprepo", alias = "update-tap-repo")]
    UpdateTapRepo {
        #[arg(long = "repo-root")]
        repo_root: Option<PathBuf>,
        #[arg(long)]
        version: Option<String>,
        #[arg(long)]
        checksums: PathBuf,
        #[arg(long = "tap-dir")]
        tap_dir: PathBuf,
        #[arg(long, default_value = "Aureuma/si")]
        repo: String,
        #[arg(long = "commit", action = ArgAction::SetTrue)]
        commit: bool,
        #[arg(long = "push", action = ArgAction::SetTrue)]
        push: bool,
    },
}

#[derive(Clone, Copy, Debug)]
pub struct ReleaseTarget {
    id: &'static str,
    os_label: &'static str,
    arch_label: &'static str,
    rust_triple: &'static str,
}

pub const SUPPORTED_RELEASE_TARGETS: &[ReleaseTarget] = &[
    ReleaseTarget {
        id: "linux-amd64",
        os_label: "linux",
        arch_label: "amd64",
        rust_triple: "x86_64-unknown-linux-gnu",
    },
    ReleaseTarget {
        id: "linux-arm64",
        os_label: "linux",
        arch_label: "arm64",
        rust_triple: "aarch64-unknown-linux-gnu",
    },
    ReleaseTarget {
        id: "darwin-amd64",
        os_label: "darwin",
        arch_label: "amd64",
        rust_triple: "x86_64-apple-darwin",
    },
    ReleaseTarget {
        id: "darwin-arm64",
        os_label: "darwin",
        arch_label: "arm64",
        rust_triple: "aarch64-apple-darwin",
    },
];

pub fn release_target_by_id(raw: &str) -> Result<ReleaseTarget> {
    let normalized = raw.trim().replace(['_', '/'], "-").to_ascii_lowercase();
    SUPPORTED_RELEASE_TARGETS.iter().copied().find(|target| target.id == normalized).ok_or_else(
        || {
            let expected = SUPPORTED_RELEASE_TARGETS
                .iter()
                .map(|target| target.id)
                .collect::<Vec<_>>()
                .join(", ");
            anyhow!("unsupported release target {raw:?}; expected one of: {expected}")
        },
    )
}

pub fn current_host_release_target() -> Result<ReleaseTarget> {
    let id = match (std::env::consts::OS, std::env::consts::ARCH) {
        ("linux", "x86_64") => "linux-amd64",
        ("linux", "aarch64") => "linux-arm64",
        ("macos", "x86_64") => "darwin-amd64",
        ("macos", "aarch64") => "darwin-arm64",
        (os, arch) => anyhow::bail!("unsupported host release target {os}/{arch}"),
    };
    release_target_by_id(id)
}

pub fn release_asset_name(version: &str, target: ReleaseTarget) -> String {
    let version_no_v = version.trim_start_matches('v');
    format!("si_{version_no_v}_{}_{}.tar.gz", target.os_label, target.arch_label)
}

pub fn run_build_self_release_assets(
    repo: Option<PathBuf>,
    version: Option<String>,
    out_dir: Option<PathBuf>,
) -> Result<()> {
    let repo_root = resolve_release_repo_root(repo)?;
    let resolved_version = resolve_release_version(&repo_root, version)?;
    let resolved_out_dir = resolve_release_output_dir(out_dir, &repo_root)?;
    fs::create_dir_all(&resolved_out_dir)
        .with_context(|| format!("create release output dir {}", resolved_out_dir.display()))?;

    let mut archive_names = Vec::new();
    for target in SUPPORTED_RELEASE_TARGETS {
        println!("building release archive for {} ({})", target.id, target.rust_triple);
        let archive_path =
            build_release_asset(&repo_root, &resolved_out_dir, &resolved_version, *target)?;
        let archive_name = archive_path
            .file_name()
            .and_then(|value| value.to_str())
            .ok_or_else(|| anyhow!("invalid release archive path {}", archive_path.display()))?;
        archive_names.push(archive_name.to_owned());
    }

    let checksums_path = resolved_out_dir.join("checksums.txt");
    let mut checksums = File::create(&checksums_path)
        .with_context(|| format!("create {}", checksums_path.display()))?;
    for name in &archive_names {
        let digest = sha256_file(&resolved_out_dir.join(name))?;
        writeln!(checksums, "{digest}  {name}")
            .with_context(|| format!("write {}", checksums_path.display()))?;
    }

    println!("created release archives:");
    for name in &archive_names {
        println!("  - {}", resolved_out_dir.join(name).display());
    }
    println!("created checksums:");
    println!("  - {}", checksums_path.display());
    Ok(())
}

pub fn run_build_self_release_asset(
    repo_root: Option<PathBuf>,
    version: String,
    target: String,
    out_dir: Option<PathBuf>,
) -> Result<()> {
    let repo_root = resolve_release_repo_root(repo_root)?;
    let version = resolve_release_version(&repo_root, Some(version))?;
    let out_dir = resolve_release_output_dir(out_dir, &repo_root)?;
    fs::create_dir_all(&out_dir).with_context(|| format!("create {}", out_dir.display()))?;
    let target = release_target_by_id(&target)?;
    let archive = build_release_asset(&repo_root, &out_dir, &version, target)?;
    println!("created {}", archive.display());
    Ok(())
}

#[derive(Debug)]
pub struct SelfBuildTarget {
    target: PathBuf,
    upgrade: bool,
}

pub fn resolve_self_repo_root(repo: Option<PathBuf>) -> Result<PathBuf> {
    resolve_release_repo_root(repo)
}

pub fn resolve_self_absolute_path(path: &Path) -> Result<PathBuf> {
    if path.is_absolute() {
        Ok(path.to_path_buf())
    } else {
        Ok(std::env::current_dir().context("read current dir")?.join(path))
    }
}

pub fn resolve_self_install_path(path: Option<PathBuf>) -> Result<PathBuf> {
    if let Some(path) = path {
        return resolve_self_absolute_path(&path);
    }
    if let Some(path_env) = std::env::var_os("PATH") {
        for dir in std::env::split_paths(&path_env) {
            let candidate = dir.join("si");
            if candidate.exists() {
                return Ok(candidate);
            }
        }
    }
    if let Ok(exe) = std::env::current_exe()
        && !exe.as_os_str().is_empty()
    {
        return Ok(exe);
    }
    let home = std::env::var("HOME")
        .map(PathBuf::from)
        .map_err(|_| anyhow!("cannot determine install path; set --install-path"))?;
    Ok(home.join(".local").join("bin").join("si"))
}

pub struct PreparedCargoTargetDir {
    path: PathBuf,
    _temp: Option<tempfile::TempDir>,
}

pub fn resolve_program_on_path(name: &str) -> Option<PathBuf> {
    let path_env = std::env::var_os("PATH")?;
    for dir in std::env::split_paths(&path_env) {
        let candidate = dir.join(name);
        if candidate.is_file() {
            return Some(candidate);
        }
    }
    None
}

pub fn ensure_directory_is_writable(path: &Path) -> Result<()> {
    fs::create_dir_all(path).with_context(|| format!("create {}", path.display()))?;
    let probe = path.join(format!(".si-write-probe-{}", std::process::id()));
    fs::write(&probe, "").with_context(|| format!("write {}", probe.display()))?;
    fs::remove_file(&probe).with_context(|| format!("remove {}", probe.display()))?;
    Ok(())
}

pub fn prepare_self_cargo_target_dir(
    repo_root: &Path,
    target_dir: Option<PathBuf>,
) -> Result<PreparedCargoTargetDir> {
    if let Some(target_dir) = target_dir {
        let resolved = resolve_self_absolute_path(&target_dir)?;
        ensure_directory_is_writable(&resolved)?;
        return Ok(PreparedCargoTargetDir { path: resolved, _temp: None });
    }

    let default_dir = repo_root.join(".artifacts").join("cargo-target").join("self-build");
    if ensure_directory_is_writable(&default_dir).is_ok() {
        return Ok(PreparedCargoTargetDir { path: default_dir, _temp: None });
    }

    let temp = tempfile::tempdir().context("create temp cargo target dir")?;
    Ok(PreparedCargoTargetDir { path: temp.path().to_path_buf(), _temp: Some(temp) })
}

pub fn configure_self_cargo_command(
    command: &mut StdCommand,
    repo_root: &Path,
    cargo_target_dir: &Path,
) {
    command.current_dir(repo_root).env("CARGO_TARGET_DIR", cargo_target_dir);
    if std::env::var_os("RUSTC_WRAPPER").is_none()
        && let Some(sccache) = resolve_program_on_path("sccache")
    {
        command.env("RUSTC_WRAPPER", sccache);
    }
}

pub fn print_self_build_runtime_notes(cargo_target_dir: &Path, timings: bool, quiet: bool) {
    if quiet {
        return;
    }
    println!("cargo target dir: {}", cargo_target_dir.display());
    if std::env::var_os("RUSTC_WRAPPER").is_some() {
        println!("rustc wrapper: {}", std::env::var("RUSTC_WRAPPER").unwrap_or_default());
    } else if let Some(sccache) = resolve_program_on_path("sccache") {
        println!("rustc wrapper: {}", sccache.display());
    } else {
        println!("rustc wrapper: unavailable (install sccache for faster cold builds)");
    }
    if timings {
        println!("cargo timings: {}", cargo_target_dir.join("cargo-timings").display());
    }
}

pub fn resolve_self_build_target(
    repo_root: &Path,
    install_path: Option<PathBuf>,
    output: Option<PathBuf>,
    no_upgrade: bool,
) -> Result<SelfBuildTarget> {
    if install_path.is_some() && (no_upgrade || output.is_some()) {
        return Err(anyhow!("--install-path cannot be used with --no-upgrade or --output"));
    }
    if no_upgrade || output.is_some() {
        let target = if let Some(path) = output {
            resolve_self_absolute_path(&path)?
        } else {
            repo_root.join("si")
        };
        return Ok(SelfBuildTarget { target, upgrade: false });
    }
    Ok(SelfBuildTarget { target: resolve_self_install_path(install_path)?, upgrade: true })
}

pub fn build_self_binary(
    repo_root: &Path,
    output: &Path,
    quiet: bool,
    cargo_options: &BuildCargoOptions,
) -> Result<()> {
    let dir = output.parent().ok_or_else(|| anyhow!("invalid output path {}", output.display()))?;
    fs::create_dir_all(dir).with_context(|| format!("create {}", dir.display()))?;
    let cargo_target_dir =
        prepare_self_cargo_target_dir(repo_root, cargo_options.target_dir.clone())?;
    if !quiet {
        println!(
            "running: cargo build --release --locked --manifest-path rust/crates/si-cli/Cargo.toml --bin si"
        );
    }
    print_self_build_runtime_notes(&cargo_target_dir.path, cargo_options.timings, quiet);
    let mut command = StdCommand::new("cargo");
    configure_self_cargo_command(&mut command, repo_root, &cargo_target_dir.path);
    let status = command
        .arg("build")
        .args(cargo_options.timings.then_some("--timings"))
        .arg("--release")
        .arg("--locked")
        .arg("--manifest-path")
        .arg("rust/crates/si-cli/Cargo.toml")
        .arg("--bin")
        .arg("si")
        .status()
        .context("run cargo build for self build")?;
    if !status.success() {
        return Err(anyhow!("build failed: {status}"));
    }
    let built_binary =
        cargo_target_dir.path.join("release").join(if cfg!(windows) { "si.exe" } else { "si" });
    let tmp = tempfile::Builder::new()
        .prefix("si-self-build-")
        .tempfile_in(dir)
        .with_context(|| format!("create temp output in {}", dir.display()))?;
    let tmp_path = tmp.path().to_path_buf();
    drop(tmp);
    fs::copy(&built_binary, &tmp_path).with_context(|| {
        format!("copy built binary {} to {}", built_binary.display(), tmp_path.display())
    })?;
    #[cfg(unix)]
    {
        let mut perms = fs::metadata(&tmp_path)
            .with_context(|| format!("stat {}", tmp_path.display()))?
            .permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&tmp_path, perms)
            .with_context(|| format!("chmod {}", tmp_path.display()))?;
    }
    fs::rename(&tmp_path, output)
        .or_else(|err| {
            if err.kind() == io::ErrorKind::AlreadyExists {
                let _ = fs::remove_file(output);
                fs::rename(&tmp_path, output)
            } else {
                Err(err)
            }
        })
        .with_context(|| format!("install {}", output.display()))?;
    Ok(())
}

pub fn run_build_self_build(
    repo: Option<PathBuf>,
    install_path: Option<PathBuf>,
    output: Option<PathBuf>,
    no_upgrade: bool,
    quiet: bool,
    cargo_options: BuildCargoOptions,
) -> Result<()> {
    let repo_root = resolve_self_repo_root(repo)?;
    let target = resolve_self_build_target(&repo_root, install_path, output, no_upgrade)?;
    build_self_binary(&repo_root, &target.target, quiet, &cargo_options)?;
    if target.upgrade {
        println!("upgraded si binary: {}", target.target.display());
    } else {
        println!("built si binary: {}", target.target.display());
    }
    Ok(())
}

pub fn run_build_self_upgrade(
    repo: Option<PathBuf>,
    install_path: Option<PathBuf>,
    quiet: bool,
    cargo_options: BuildCargoOptions,
) -> Result<()> {
    let repo_root = resolve_self_repo_root(repo)?;
    let target = resolve_self_install_path(install_path)?;
    build_self_binary(&repo_root, &target, quiet, &cargo_options)?;
    println!("upgraded si binary: {}", target.display());
    Ok(())
}

pub fn run_build_self_check(
    repo: Option<PathBuf>,
    quiet: bool,
    cargo_options: BuildCargoOptions,
) -> Result<()> {
    let repo_root = resolve_self_repo_root(repo)?;
    let cargo_target_dir =
        prepare_self_cargo_target_dir(&repo_root, cargo_options.target_dir.clone())?;
    if !quiet {
        println!(
            "running: cargo check --locked --manifest-path rust/crates/si-cli/Cargo.toml --bin si"
        );
    }
    print_self_build_runtime_notes(&cargo_target_dir.path, cargo_options.timings, quiet);
    let mut command = StdCommand::new("cargo");
    configure_self_cargo_command(&mut command, &repo_root, &cargo_target_dir.path);
    let status = command
        .arg("check")
        .args(cargo_options.timings.then_some("--timings"))
        .arg("--locked")
        .arg("--manifest-path")
        .arg("rust/crates/si-cli/Cargo.toml")
        .arg("--bin")
        .arg("si")
        .status()
        .context("run cargo check for self build")?;
    if !status.success() {
        return Err(anyhow!("cargo check failed: {status}"));
    }
    println!("checked si binary manifest: rust/crates/si-cli/Cargo.toml");
    Ok(())
}

pub fn run_build_self_run(
    repo: Option<PathBuf>,
    cargo_options: BuildCargoOptions,
    args: Vec<String>,
) -> Result<()> {
    let repo_root = resolve_self_repo_root(repo)?;
    let cargo_target_dir =
        prepare_self_cargo_target_dir(&repo_root, cargo_options.target_dir.clone())?;
    let mut command = StdCommand::new("cargo");
    configure_self_cargo_command(&mut command, &repo_root, &cargo_target_dir.path);
    command
        .arg("run")
        .arg("--locked")
        .arg("--manifest-path")
        .arg("rust/crates/si-cli/Cargo.toml")
        .arg("--bin")
        .arg("si")
        .arg("--");
    for arg in args {
        command.arg(arg);
    }
    let status = command.status().context("run cargo run for self run")?;
    if !status.success() {
        return Err(anyhow!("cargo run failed: {status}"));
    }
    Ok(())
}

pub fn resolve_release_repo_root(repo: Option<PathBuf>) -> Result<PathBuf> {
    let start = match repo {
        Some(path) if path.is_absolute() => path,
        Some(path) => std::env::current_dir().context("read current dir")?.join(path),
        None => std::env::current_dir().context("read current dir")?,
    };
    git_repo_root_from(&start)
        .with_context(|| format!("resolve repo root from {}", start.display()))
}

pub fn resolve_release_version(repo_root: &Path, version: Option<String>) -> Result<String> {
    let value = if let Some(version) = version {
        let trimmed = version.trim();
        if trimmed.is_empty() { read_si_version(repo_root)? } else { trimmed.to_owned() }
    } else {
        read_si_version(repo_root)?
    };
    if !value.starts_with('v') {
        return Err(anyhow!("version must include v prefix, got: {value}"));
    }
    if value == "v" {
        return Err(anyhow!("invalid version"));
    }
    Ok(value)
}

pub fn resolve_release_output_dir(out_dir: Option<PathBuf>, repo_root: &Path) -> Result<PathBuf> {
    match out_dir {
        Some(path) if path.is_absolute() => Ok(path),
        Some(path) => Ok(std::env::current_dir().context("read current dir")?.join(path)),
        None => Ok(repo_root.join("dist")),
    }
}

pub fn read_si_version(repo_root: &Path) -> Result<String> {
    let cargo_toml_path = repo_root.join("Cargo.toml");
    let raw = fs::read_to_string(&cargo_toml_path)
        .with_context(|| format!("read {}", cargo_toml_path.display()))?;
    let parsed: toml::Value =
        toml::from_str(&raw).with_context(|| format!("parse {}", cargo_toml_path.display()))?;
    let version = parsed
        .get("workspace")
        .and_then(|workspace| workspace.get("package"))
        .and_then(|package| package.get("version"))
        .and_then(toml::Value::as_str)
        .ok_or_else(|| {
            anyhow!("workspace.package.version not found in {}", cargo_toml_path.display())
        })?;
    if version.starts_with('v') { Ok(version.to_owned()) } else { Ok(format!("v{version}")) }
}

pub fn build_release_asset(
    repo_root: &Path,
    out_dir: &Path,
    version: &str,
    target: ReleaseTarget,
) -> Result<PathBuf> {
    for required in ["README.md", "LICENSE"] {
        let path = repo_root.join(required);
        if !path.exists() {
            return Err(anyhow!("{required} not found in repo root"));
        }
    }

    let temp = tempfile::tempdir().context("create release temp dir")?;
    let version_no_v = version.trim_start_matches('v');
    let artifact_stem = format!("si_{version_no_v}_{}_{}", target.os_label, target.arch_label);
    let staging_dir = temp.path().join(&artifact_stem);
    fs::create_dir_all(&staging_dir)
        .with_context(|| format!("create {}", staging_dir.display()))?;

    let cargo_target_dir = temp.path().join("cargo-target");
    let output_path = staging_dir.join("si");
    let mut cargo_command = StdCommand::new("cargo");
    configure_self_cargo_command(&mut cargo_command, repo_root, &cargo_target_dir);
    cargo_command
        .arg("build")
        .arg("--release")
        .arg("--locked")
        .arg("--target")
        .arg(target.rust_triple)
        .arg("--manifest-path")
        .arg("rust/crates/si-cli/Cargo.toml")
        .arg("--bin")
        .arg("si");
    let output = cargo_command.output().context("run cargo build for release asset")?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        let stdout = String::from_utf8_lossy(&output.stdout);
        return Err(anyhow!(
            "cargo build failed for {} ({}): {}{}{}",
            target.id,
            target.rust_triple,
            output.status,
            if stdout.trim().is_empty() { "" } else { "\nstdout:\n" },
            if stdout.trim().is_empty() {
                stderr.trim().to_owned()
            } else {
                format!("{}\nstderr:\n{}", stdout.trim(), stderr.trim())
            },
        ));
    }
    let built_binary = cargo_target_dir
        .join(target.rust_triple)
        .join("release")
        .join(if cfg!(windows) { "si.exe" } else { "si" });
    fs::copy(&built_binary, &output_path).with_context(|| {
        format!("copy built release binary {} to {}", built_binary.display(), output_path.display())
    })?;

    #[cfg(unix)]
    {
        let mut perms = fs::metadata(&output_path)
            .with_context(|| format!("stat {}", output_path.display()))?
            .permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&output_path, perms)
            .with_context(|| format!("chmod {}", output_path.display()))?;
    }

    for required in ["README.md", "LICENSE"] {
        let src = repo_root.join(required);
        let dst = staging_dir.join(required);
        fs::copy(&src, &dst)
            .with_context(|| format!("copy {} to {}", src.display(), dst.display()))?;
    }

    let archive_path = out_dir.join(format!("{artifact_stem}.tar.gz"));
    let archive_file = File::create(&archive_path)
        .with_context(|| format!("create {}", archive_path.display()))?;
    let encoder = GzEncoder::new(archive_file, Compression::default());
    let mut builder = TarBuilder::new(encoder);
    builder
        .append_dir_all(&artifact_stem, &staging_dir)
        .with_context(|| format!("archive {}", archive_path.display()))?;
    builder.finish().context("finish tar archive")?;
    Ok(archive_path)
}

pub fn sha256_file(path: &Path) -> Result<String> {
    let mut file = File::open(path).with_context(|| format!("open {}", path.display()))?;
    let mut hasher = Sha256::new();
    let mut buffer = [0_u8; 8192];
    loop {
        let read = file.read(&mut buffer).with_context(|| format!("read {}", path.display()))?;
        if read == 0 {
            break;
        }
        hasher.update(&buffer[..read]);
    }
    Ok(format!("{:x}", hasher.finalize()))
}

pub fn run_build_npm_package(
    repo_root: Option<PathBuf>,
    version: Option<String>,
    out_dir: Option<PathBuf>,
) -> Result<()> {
    let repo_root = resolve_release_repo_root(repo_root)?;
    let version = resolve_release_version(&repo_root, version)?;
    let out_dir = match out_dir {
        Some(path) if path.is_absolute() => path,
        Some(path) => std::env::current_dir().context("read current dir")?.join(path),
        None => repo_root.join("dist").join("npm"),
    };
    let package = build_npm_package(&repo_root, &version, &out_dir)?;
    println!("created npm package: {}", package.display());
    Ok(())
}

pub fn run_publish_npm_package(
    repo_root: Option<PathBuf>,
    version: Option<String>,
    out_dir: Option<PathBuf>,
    token_env: String,
    dry_run: bool,
) -> Result<()> {
    let repo_root = resolve_release_repo_root(repo_root)?;
    let version = resolve_release_version(&repo_root, version)?;
    let out_dir = match out_dir {
        Some(path) if path.is_absolute() => path,
        Some(path) => std::env::current_dir().context("read current dir")?.join(path),
        None => repo_root.join("dist").join("npm"),
    };

    let token_env = if token_env.trim() == "NPM_TOKEN"
        && std::env::var("NPM_TOKEN").unwrap_or_default().trim().is_empty()
        && !std::env::var("NPM_GAT_AUREUMA_VANGUARDA").unwrap_or_default().trim().is_empty()
    {
        "NPM_GAT_AUREUMA_VANGUARDA".to_owned()
    } else {
        token_env.trim().to_owned()
    };

    let npm_version = version.trim_start_matches('v');
    let package_version = format!("@aureuma/si@{npm_version}");
    if npm_package_exists(&package_version)? {
        println!("{package_version} already published; skipping");
        return Ok(());
    }

    let package = build_npm_package(&repo_root, &version, &out_dir)?;
    let token = std::env::var(&token_env).unwrap_or_default();
    if token.trim().is_empty() {
        return Err(anyhow!("token environment variable {token_env} is required"));
    }

    let npmrc = tempfile::NamedTempFile::new().context("create npmrc temp file")?;
    fs::write(
        npmrc.path(),
        format!("//registry.npmjs.org/:_authToken={}\nalways-auth=true\n", token.trim()),
    )
    .with_context(|| format!("write {}", npmrc.path().display()))?;

    let mut command = StdCommand::new("npm");
    command
        .current_dir(&repo_root)
        .env("NPM_CONFIG_USERCONFIG", npmrc.path())
        .arg("publish")
        .arg(&package)
        .arg("--access")
        .arg("public");
    if dry_run {
        command.arg("--dry-run");
    }
    let output = command
        .output()
        .with_context(|| format!("run corepack pnpm publish for {}", package.display()))?;
    if !output.status.success() {
        return Err(anyhow!(
            "corepack pnpm publish failed: {}",
            String::from_utf8_lossy(&output.stderr).trim()
        ));
    }
    if dry_run {
        println!("dry-run complete: {}", package.display());
        return Ok(());
    }

    if !wait_for_npm_package(&package_version, 18, std::time::Duration::from_secs(10))? {
        return Err(anyhow!("package publish appears to have failed verification"));
    }
    println!("published {package_version}");
    Ok(())
}

pub fn build_npm_package(repo_root: &Path, version: &str, out_dir: &Path) -> Result<PathBuf> {
    let package_root = repo_root.join("npm").join("si");
    if !package_root.exists() {
        return Err(anyhow!("npm/si not found"));
    }
    if !repo_root.join("LICENSE").exists() {
        return Err(anyhow!("LICENSE not found"));
    }
    ensure_command_exists("node")?;
    ensure_command_exists("npm")?;

    fs::create_dir_all(out_dir).with_context(|| format!("create {}", out_dir.display()))?;
    let stage = tempfile::tempdir().context("create npm package temp dir")?;
    copy_dir_recursive(&package_root, stage.path())?;
    fs::copy(repo_root.join("LICENSE"), stage.path().join("LICENSE"))
        .with_context(|| format!("copy LICENSE into {}", stage.path().display()))?;
    write_staged_npm_package_json(
        &package_root.join("package.template.json"),
        &stage.path().join("package.json"),
        version.trim_start_matches('v'),
    )?;

    let output = StdCommand::new("npm")
        .current_dir(stage.path())
        .arg("pack")
        .arg("--silent")
        .output()
        .context("run corepack pnpm pack")?;
    if !output.status.success() {
        return Err(anyhow!(
            "corepack pnpm pack failed: {}",
            String::from_utf8_lossy(&output.stderr).trim()
        ));
    }

    let mut matches = fs::read_dir(stage.path())
        .with_context(|| format!("read {}", stage.path().display()))?
        .filter_map(|entry| entry.ok().map(|item| item.path()))
        .filter(|path| path.extension().and_then(|value| value.to_str()) == Some("tgz"))
        .collect::<Vec<_>>();
    matches.sort();
    let src =
        matches.pop().ok_or_else(|| anyhow!("corepack pnpm pack did not produce a tarball"))?;
    let dst = out_dir.join(
        src.file_name().ok_or_else(|| anyhow!("invalid npm tarball path {}", src.display()))?,
    );
    move_file(&src, &dst)?;
    Ok(dst)
}

pub fn ensure_command_exists(command: &str) -> Result<()> {
    match StdCommand::new(command).arg("--version").output() {
        Ok(_) => Ok(()),
        Err(err) if err.kind() == io::ErrorKind::NotFound => {
            Err(anyhow!("missing required command: {command}"))
        }
        Err(err) => Err(err).with_context(|| format!("check command {command}")),
    }
}

pub fn copy_dir_recursive(src: &Path, dst: &Path) -> Result<()> {
    fs::create_dir_all(dst).with_context(|| format!("create {}", dst.display()))?;
    for entry in fs::read_dir(src).with_context(|| format!("read {}", src.display()))? {
        let entry = entry.with_context(|| format!("read entry in {}", src.display()))?;
        let path = entry.path();
        let target = dst.join(entry.file_name());
        if path.is_dir() {
            copy_dir_recursive(&path, &target)?;
        } else {
            fs::copy(&path, &target)
                .with_context(|| format!("copy {} to {}", path.display(), target.display()))?;
        }
    }
    Ok(())
}

pub fn write_staged_npm_package_json(
    template_path: &Path,
    output_path: &Path,
    version: &str,
) -> Result<()> {
    let raw = fs::read_to_string(template_path)
        .with_context(|| format!("read {}", template_path.display()))?;
    let mut value: Value =
        serde_json::from_str(&raw).with_context(|| format!("parse {}", template_path.display()))?;
    let object =
        value.as_object_mut().ok_or_else(|| anyhow!("package template must be a JSON object"))?;
    object.insert("version".to_owned(), Value::String(version.to_owned()));
    fs::write(
        output_path,
        format!("{}\n", serde_json::to_string_pretty(&value).context("render package.json")?),
    )
    .with_context(|| format!("write {}", output_path.display()))?;
    Ok(())
}

pub fn move_file(src: &Path, dst: &Path) -> Result<()> {
    match fs::rename(src, dst) {
        Ok(_) => Ok(()),
        Err(err) if err.kind() == io::ErrorKind::CrossesDevices => {
            fs::copy(src, dst)
                .with_context(|| format!("copy {} to {}", src.display(), dst.display()))?;
            fs::remove_file(src).with_context(|| format!("remove {}", src.display()))?;
            Ok(())
        }
        Err(err) => {
            Err(err).with_context(|| format!("move {} to {}", src.display(), dst.display()))
        }
    }
}

pub fn npm_package_exists(package_version: &str) -> Result<bool> {
    let output = StdCommand::new("npm")
        .arg("view")
        .arg(package_version)
        .arg("version")
        .output()
        .with_context(|| format!("run corepack pnpm view for {package_version}"))?;
    Ok(output.status.success() && !String::from_utf8_lossy(&output.stdout).trim().is_empty())
}

pub fn wait_for_npm_package(
    package_version: &str,
    attempts: usize,
    delay: std::time::Duration,
) -> Result<bool> {
    for _ in 0..attempts {
        if npm_package_exists(package_version)? {
            return Ok(true);
        }
        std::thread::sleep(delay);
    }
    Ok(false)
}

#[derive(Debug)]
pub struct InstallerRunConfig {
    backend: String,
    source_dir: Option<PathBuf>,
    repo_url: Option<String>,
    ref_name: String,
    install_dir: Option<PathBuf>,
    install_path: Option<PathBuf>,
    force: bool,
    uninstall: bool,
    toolchain_mode: String,
    os_override: Option<String>,
    arch_override: Option<String>,
    dry_run: bool,
    quiet: bool,
    no_path_hint: bool,
}

pub fn run_installer(cfg: InstallerRunConfig) -> Result<()> {
    validate_installer_config(&cfg)?;
    let install_path = resolve_installer_install_path(&cfg)?;
    if cfg.uninstall {
        if cfg.dry_run {
            if !cfg.quiet {
                println!("dry-run: uninstall {}", install_path.display());
            }
            return Ok(());
        }
        match fs::remove_file(&install_path) {
            Ok(_) => {}
            Err(err) if err.kind() == io::ErrorKind::NotFound => {}
            Err(err) => {
                return Err(err).with_context(|| format!("uninstall {}", install_path.display()));
            }
        }
        return Ok(());
    }

    if cfg.backend.trim() != "local" {
        return Err(anyhow!("invalid --backend {} (expected local)", cfg.backend.trim()));
    }

    let (source_dir, _cleanup) = resolve_installer_source_dir(&cfg)?;
    let cargo_bin = ensure_installer_cargo_toolchain(&cfg.toolchain_mode)?;
    if cfg.dry_run {
        if !cfg.quiet {
            println!("rust: using system cargo ({cargo_bin})");
        }
        return Ok(());
    }

    ensure_installer_install_writable(&install_path, cfg.force)?;
    let install_dir = install_path
        .parent()
        .ok_or_else(|| anyhow!("invalid install path {}", install_path.display()))?;
    let cargo_target_dir = tempfile::tempdir().context("create temp cargo target dir")?;
    let tmp_output = tempfile::Builder::new()
        .prefix("si-build-")
        .tempfile_in(install_dir)
        .with_context(|| format!("create temp output in {}", install_dir.display()))?;
    let tmp_output_path = tmp_output.path().to_path_buf();
    drop(tmp_output);

    if std::env::var("SI_INSTALLER_USE_PREBUILT").map(|value| value.trim() == "1").unwrap_or(false)
    {
        let prebuilt = source_dir
            .join(".artifacts")
            .join("cargo-target")
            .join("release")
            .join(if cfg!(windows) { "si.exe" } else { "si" });
        if prebuilt.exists() {
            if !cfg.quiet {
                println!("rust: using prebuilt installer binary {}", prebuilt.display());
            }
            fs::copy(&prebuilt, &tmp_output_path).with_context(|| {
                format!(
                    "copy installer binary {} to {}",
                    prebuilt.display(),
                    tmp_output_path.display()
                )
            })?;
            #[cfg(unix)]
            {
                let mut perms = fs::metadata(&tmp_output_path)
                    .with_context(|| format!("stat {}", tmp_output_path.display()))?
                    .permissions();
                perms.set_mode(0o755);
                fs::set_permissions(&tmp_output_path, perms)
                    .with_context(|| format!("chmod {}", tmp_output_path.display()))?;
            }
            fs::rename(&tmp_output_path, &install_path).or_else(|err| {
                if err.kind() == io::ErrorKind::AlreadyExists {
                    let _ = fs::remove_file(&install_path);
                    fs::rename(&tmp_output_path, &install_path)
                } else {
                    Err(err)
                }
            })?;
            if !cfg.no_path_hint {
                warn_if_installer_path_missing(install_dir);
            }
            return Ok(());
        }
    }

    let mut command = StdCommand::new(&cargo_bin);
    command
        .current_dir(&source_dir)
        .env("CARGO_TARGET_DIR", cargo_target_dir.path())
        .arg("build")
        .arg("--release")
        .arg("--locked")
        .arg("--manifest-path")
        .arg("rust/crates/si-cli/Cargo.toml")
        .arg("--bin")
        .arg("si");
    let status = command.status().context("run cargo build for installer")?;
    if !status.success() {
        return Err(anyhow!("build failed: {status}"));
    }
    let built_binary =
        cargo_target_dir.path().join("release").join(if cfg!(windows) { "si.exe" } else { "si" });
    fs::copy(&built_binary, &tmp_output_path).with_context(|| {
        format!(
            "copy built installer binary {} to {}",
            built_binary.display(),
            tmp_output_path.display()
        )
    })?;

    #[cfg(unix)]
    {
        let mut perms = fs::metadata(&tmp_output_path)
            .with_context(|| format!("stat {}", tmp_output_path.display()))?
            .permissions();
        perms.set_mode(0o755);
        fs::set_permissions(&tmp_output_path, perms)
            .with_context(|| format!("chmod {}", tmp_output_path.display()))?;
    }
    fs::rename(&tmp_output_path, &install_path)
        .or_else(|err| {
            if err.kind() == io::ErrorKind::AlreadyExists {
                let _ = fs::remove_file(&install_path);
                fs::rename(&tmp_output_path, &install_path)
            } else {
                Err(err)
            }
        })
        .with_context(|| format!("install {}", install_path.display()))?;

    if !cfg.no_path_hint {
        warn_if_installer_path_missing(install_dir);
    }
    Ok(())
}

pub fn validate_installer_config(cfg: &InstallerRunConfig) -> Result<()> {
    if cfg.install_dir.is_some() && cfg.install_path.is_some() {
        return Err(anyhow!("--install-dir and --install-path are mutually exclusive"));
    }
    match cfg.toolchain_mode.trim() {
        "auto" | "system" => {}
        value => {
            return Err(anyhow!("invalid --toolchain-mode {value} (expected auto or system)"));
        }
    }
    if (cfg.os_override.is_some() || cfg.arch_override.is_some()) && !cfg.dry_run {
        return Err(anyhow!("--os/--arch overrides require --dry-run"));
    }
    if let Some(value) = cfg.os_override.as_deref() {
        match value.trim() {
            "linux" | "darwin" => {}
            other => return Err(anyhow!("invalid --os {other} (expected linux or darwin)")),
        }
    }
    if let Some(value) = cfg.arch_override.as_deref() {
        match value.trim() {
            "amd64" | "x86_64" | "arm64" | "aarch64" => {}
            other => return Err(anyhow!("invalid --arch {other} (expected amd64 or arm64)")),
        }
    }
    Ok(())
}

pub fn resolve_installer_install_path(cfg: &InstallerRunConfig) -> Result<PathBuf> {
    if let Some(path) = cfg.install_path.as_ref() {
        return Ok(path.clone());
    }
    let install_dir = if let Some(path) = cfg.install_dir.as_ref() {
        path.clone()
    } else if is_effective_root() {
        PathBuf::from("/usr/local/bin")
    } else {
        let home = std::env::var("HOME")
            .map(PathBuf::from)
            .map_err(|_| anyhow!("unable to resolve home directory for default install path"))?;
        home.join(".local").join("bin")
    };
    Ok(install_dir.join("si"))
}

pub fn is_effective_root() -> bool {
    #[cfg(unix)]
    {
        unsafe { libc::geteuid() == 0 }
    }
    #[cfg(not(unix))]
    {
        false
    }
}

pub fn resolve_installer_source_dir(
    cfg: &InstallerRunConfig,
) -> Result<(PathBuf, Option<tempfile::TempDir>)> {
    if let Some(path) = cfg.source_dir.as_ref() {
        validate_installer_source_dir(path)?;
        return Ok((path.clone(), None));
    }
    if let Some(repo_url) = cfg.repo_url.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        let path = PathBuf::from(repo_url);
        if path.is_dir() {
            validate_installer_source_dir(&path)?;
            return Ok((path, None));
        }
        ensure_command_exists("git")?;
        let temp = tempfile::tempdir().context("create temp clone dir")?;
        let status = StdCommand::new("git")
            .arg("clone")
            .arg(repo_url)
            .arg(temp.path())
            .status()
            .context("run git clone")?;
        if !status.success() {
            return Err(anyhow!("git clone failed: {status}"));
        }
        if !cfg.ref_name.trim().is_empty() {
            let status = StdCommand::new("git")
                .arg("-C")
                .arg(temp.path())
                .arg("checkout")
                .arg(cfg.ref_name.trim())
                .status()
                .context("run git checkout")?;
            if !status.success() {
                return Err(anyhow!("git checkout failed: {status}"));
            }
        }
        validate_installer_source_dir(temp.path())?;
        return Ok((temp.path().to_path_buf(), Some(temp)));
    }
    let cwd = std::env::current_dir().context("read current dir")?;
    if validate_installer_source_dir(&cwd).is_ok() {
        return Ok((cwd, None));
    }
    Err(anyhow!("source directory required; pass --source-dir"))
}

pub fn validate_installer_source_dir(path: &Path) -> Result<()> {
    if !path.is_dir() {
        return Err(anyhow!("source directory not found: {}", path.display()));
    }
    if !path.join("Cargo.toml").exists()
        || !path.join("rust").join("crates").join("si-cli").is_dir()
    {
        return Err(anyhow!("source directory is not an si checkout: {}", path.display()));
    }
    Ok(())
}

pub fn ensure_installer_cargo_toolchain(toolchain_mode: &str) -> Result<String> {
    let output = StdCommand::new("cargo").arg("--version").output();
    match output {
        Ok(output) if output.status.success() => Ok("cargo".to_owned()),
        Ok(_) | Err(_) if toolchain_mode.trim() == "system" => {
            Err(anyhow!("cargo is required for --toolchain-mode system"))
        }
        Ok(_) => Err(anyhow!("cargo toolchain probe failed")),
        Err(err) if err.kind() == io::ErrorKind::NotFound => {
            Err(anyhow!("cargo toolchain not found on PATH"))
        }
        Err(err) => Err(err).context("probe cargo toolchain"),
    }
}

pub fn ensure_installer_install_writable(install_path: &Path, force: bool) -> Result<()> {
    if let Ok(metadata) = fs::metadata(install_path) {
        if metadata.is_dir() {
            return Err(anyhow!("install path is a directory: {}", install_path.display()));
        }
        if !force {
            return Err(anyhow!(
                "install target already exists: {} (use --force)",
                install_path.display()
            ));
        }
    }
    let dir = install_path
        .parent()
        .ok_or_else(|| anyhow!("invalid install path {}", install_path.display()))?;
    fs::create_dir_all(dir).with_context(|| format!("create {}", dir.display()))?;
    let probe = dir.join(".si-write-test");
    fs::write(&probe, "ok").with_context(|| format!("write {}", probe.display()))?;
    let _ = fs::remove_file(&probe);
    Ok(())
}

pub fn warn_if_installer_path_missing(dir: &Path) {
    let path_env = std::env::var_os("PATH").unwrap_or_default();
    let on_path = std::env::split_paths(&path_env).any(|entry| entry == dir);
    if !on_path {
        eprintln!("WARNING: install dir is not on PATH for this shell: {}", dir.display());
    }
}

pub fn run_installer_settings_helper(
    settings: PathBuf,
    default_browser: String,
    print: bool,
    check: bool,
) -> Result<()> {
    let browser = default_browser.trim().to_lowercase();
    if browser != "safari" && browser != "chrome" {
        return Err(anyhow!("--default-browser must be safari or chrome"));
    }
    let mode = if print {
        "print"
    } else if check {
        "check"
    } else {
        "write"
    };
    let (rendered, existing) = render_installer_settings(&settings, &browser)?;
    match mode {
        "print" => {
            print!("{rendered}");
            Ok(())
        }
        "check" => {
            if existing.as_deref() == Some(rendered.as_bytes()) {
                Ok(())
            } else {
                Err(anyhow!("settings file does not match expected installer helper output"))
            }
        }
        _ => {
            write_atomic_file(&settings, rendered.as_bytes(), 0o644)?;
            Ok(())
        }
    }
}

pub fn render_installer_settings(path: &Path, browser: &str) -> Result<(String, Option<Vec<u8>>)> {
    match fs::read(path) {
        Ok(existing) => {
            let current = String::from_utf8_lossy(&existing);
            Ok((render_installer_settings_doc(&current, browser), Some(existing)))
        }
        Err(err) if err.kind() == io::ErrorKind::NotFound => {
            Ok((format!("[codex.login]\ndefault_browser = \"{browser}\"\n"), None))
        }
        Err(err) => Err(err).with_context(|| format!("read {}", path.display())),
    }
}

pub fn render_installer_settings_doc(current: &str, browser: &str) -> String {
    let lines = current.replace("\r\n", "\n");
    let mut lines = lines.split('\n').map(str::to_owned).collect::<Vec<_>>();
    if lines.last().map(|line| line.is_empty()).unwrap_or(false) {
        lines.pop();
    }
    let header_pattern =
        Regex::new(r"^[[:space:]]*\[[^]]+\][[:space:]]*$").expect("valid header regex");
    let login_header_pattern =
        Regex::new(r"^[[:space:]]*\[codex\.login\][[:space:]]*$").expect("valid login regex");
    let default_browser_line =
        Regex::new(r"^[[:space:]]*default_browser[[:space:]]*=").expect("valid browser regex");
    let replacement = format!("default_browser = \"{browser}\"");
    let mut out = Vec::with_capacity(lines.len() + 4);
    let mut in_login = false;
    let mut saw_login = false;
    let mut wrote = false;
    for line in lines {
        if header_pattern.is_match(&line) {
            if in_login && !wrote {
                out.push(replacement.clone());
                wrote = true;
            }
            if login_header_pattern.is_match(&line) {
                in_login = true;
                saw_login = true;
            } else {
                in_login = false;
            }
            out.push(line);
            continue;
        }
        if in_login && default_browser_line.is_match(&line) {
            if !wrote {
                out.push(replacement.clone());
                wrote = true;
            }
            continue;
        }
        out.push(line);
    }
    if saw_login && !wrote {
        out.push(replacement.clone());
    }
    if !saw_login {
        out.push(String::new());
        out.push("[codex.login]".to_owned());
        out.push(replacement);
    }
    format!("{}\n", out.join("\n"))
}

pub fn write_atomic_file(path: &Path, data: &[u8], mode: u32) -> Result<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).with_context(|| format!("create {}", parent.display()))?;
    }
    let dir = path.parent().unwrap_or_else(|| Path::new("."));
    let temp = tempfile::Builder::new()
        .prefix(".install-si-settings-")
        .suffix(".tmp")
        .tempfile_in(dir)
        .with_context(|| format!("create temp file in {}", dir.display()))?;
    fs::write(temp.path(), data).with_context(|| format!("write {}", temp.path().display()))?;
    #[cfg(unix)]
    {
        let mut perms = fs::metadata(temp.path())
            .with_context(|| format!("stat {}", temp.path().display()))?
            .permissions();
        perms.set_mode(mode);
        fs::set_permissions(temp.path(), perms)
            .with_context(|| format!("chmod {}", temp.path().display()))?;
    }
    fs::rename(temp.path(), path)
        .with_context(|| format!("rename {} to {}", temp.path().display(), path.display()))?;
    Ok(())
}

pub fn run_installer_smoke_host() -> Result<()> {
    let root = std::env::current_dir().context("resolve repo root")?;
    let installer_runner = override_executable("SI_INSTALLER_RUNNER")
        .unwrap_or(std::env::current_exe().context("resolve current si binary")?);
    let settings_test_runner = override_executable("SI_INSTALLER_SETTINGS_TEST");
    ensure_command_exists("git")?;
    if !root.join("Cargo.toml").exists() {
        return Err(anyhow!("FAIL: expected Cargo.toml at repo root: {}", root.display()));
    }

    eprintln!("==> installer settings helper tests");
    if let Some(settings_test_runner) = settings_test_runner {
        run_path_command_checked(&root, &settings_test_runner, &[])?;
    } else {
        run_command_checked(
            &root,
            "cargo",
            ["test", "-p", "si-tools", "--test", "si_release", "build_installer_settings_helper_"],
        )?;
    }

    eprintln!("==> help output");
    run_path_command_checked(&root, &installer_runner, &["installer", "run", "--help"])?;

    let tmp = tempfile::tempdir().context("create temp dir")?;
    eprintln!("==> dry-run: linux/amd64 install-dir with spaces");
    let spaced_dir = tmp.path().join("bin dir");
    fs::create_dir_all(&spaced_dir).with_context(|| format!("create {}", spaced_dir.display()))?;
    run_path_command_checked(
        &root,
        &installer_runner,
        &[
            "installer",
            "run",
            "--dry-run",
            "--source-dir",
            root.to_str().unwrap_or_default(),
            "--install-dir",
            spaced_dir.to_str().unwrap_or_default(),
            "--force",
        ],
    )?;

    eprintln!("==> dry-run: darwin/arm64 toolchain resolution");
    run_path_command_checked(
        &root,
        &installer_runner,
        &[
            "installer",
            "run",
            "--dry-run",
            "--source-dir",
            root.to_str().unwrap_or_default(),
            "--os",
            "darwin",
            "--arch",
            "arm64",
            "--toolchain-mode",
            "auto",
            "--force",
        ],
    )?;

    eprintln!("==> dry-run: no-path-hint flag");
    run_path_command_checked(
        &root,
        &installer_runner,
        &[
            "installer",
            "run",
            "--dry-run",
            "--no-path-hint",
            "--source-dir",
            root.to_str().unwrap_or_default(),
            "--force",
        ],
    )?;

    eprintln!("==> dry-run: --yes accepted");
    run_path_command_checked(
        &root,
        &installer_runner,
        &[
            "installer",
            "run",
            "--dry-run",
            "--yes",
            "--source-dir",
            root.to_str().unwrap_or_default(),
            "--force",
        ],
    )?;

    eprintln!("==> dry-run: backend local accepted");
    run_path_command_checked(
        &root,
        &installer_runner,
        &[
            "installer",
            "run",
            "--dry-run",
            "--backend",
            "local",
            "--source-dir",
            root.to_str().unwrap_or_default(),
            "--force",
        ],
    )?;

    eprintln!("==> edge: invalid backend rejected");
    run_path_command_expect_fail(
        &root,
        &installer_runner,
        &[
            "installer",
            "run",
            "--dry-run",
            "--backend",
            "bad-backend",
            "--source-dir",
            root.to_str().unwrap_or_default(),
            "--force",
        ],
    )?;

    eprintln!("==> edge: install-dir and install-path are mutually exclusive");
    run_path_command_expect_fail(
        &root,
        &installer_runner,
        &[
            "installer",
            "run",
            "--dry-run",
            "--source-dir",
            root.to_str().unwrap_or_default(),
            "--install-dir",
            tmp.path().join("x").to_str().unwrap_or_default(),
            "--install-path",
            tmp.path().join("y").join("si").to_str().unwrap_or_default(),
            "--force",
        ],
    )?;

    eprintln!("==> edge: invalid source-dir rejected");
    run_path_command_expect_fail(
        &root,
        &installer_runner,
        &[
            "installer",
            "run",
            "--dry-run",
            "--source-dir",
            tmp.path().join("missing-source").to_str().unwrap_or_default(),
            "--force",
        ],
    )?;

    eprintln!("==> e2e: install from local checkout into temp bin");
    let install_dir = tmp.path().join("bin");
    fs::create_dir_all(&install_dir)
        .with_context(|| format!("create {}", install_dir.display()))?;
    run_path_command_checked(
        &root,
        &installer_runner,
        &[
            "installer",
            "run",
            "--source-dir",
            root.to_str().unwrap_or_default(),
            "--install-dir",
            install_dir.to_str().unwrap_or_default(),
            "--force",
            "--quiet",
        ],
    )?;
    let installed = install_dir.join("si");
    if !installed.exists() {
        return Err(anyhow!("FAIL: expected installed binary at {}", installed.display()));
    }
    run_path_command_checked(&root, &installed, &["version"])?;
    run_path_command_checked(&root, &installed, &["--help"])?;

    eprintln!("==> e2e: uninstall");
    run_path_command_checked(
        &root,
        &installer_runner,
        &[
            "installer",
            "run",
            "--install-dir",
            install_dir.to_str().unwrap_or_default(),
            "--uninstall",
            "--quiet",
        ],
    )?;
    if installed.exists() {
        return Err(anyhow!("FAIL: expected {} to be removed", installed.display()));
    }
    eprintln!("==> ok");
    Ok(())
}

pub fn run_installer_smoke_npm() -> Result<()> {
    let root = std::env::current_dir().context("resolve repo root")?;
    let self_bin = std::env::current_exe().context("resolve current si binary")?;
    let assets_builder = override_executable("SI_BUILD_ASSETS_EXEC");
    let npm_builder = override_executable("SI_BUILD_NPM_PACKAGE_EXEC");
    let version = read_si_version(&root)?;
    let tmp = tempfile::tempdir().context("create temp dir")?;
    let mut assets_dir = tmp.path().join("assets");
    let npm_out = tmp.path().join("npm");
    let prefix_dir = tmp.path().join("prefix");
    let mut build_assets = true;
    if let Ok(provided_assets_dir) = std::env::var("SI_INSTALL_SMOKE_ASSETS_DIR") {
        let provided = Path::new(&provided_assets_dir).to_path_buf();
        if provided.join("checksums.txt").exists() {
            assets_dir = provided;
            build_assets = false;
        }
    }
    if build_assets {
        fs::create_dir_all(&assets_dir)
            .with_context(|| format!("create {}", assets_dir.display()))?;
    }
    for dir in [&npm_out, &prefix_dir] {
        fs::create_dir_all(dir).with_context(|| format!("create {}", dir.display()))?;
    }

    if build_assets {
        if let Some(assets_builder) = &assets_builder {
            run_path_command_checked(
                &root,
                assets_builder,
                &["--version", &version, "--out-dir", assets_dir.to_str().unwrap_or_default()],
            )?;
        } else {
            let target = current_host_release_target()?;
            run_path_command_checked(
                &root,
                &self_bin,
                &[
                    "self",
                    "asset",
                    "--version",
                    &version,
                    "--target",
                    target.id,
                    "--out-dir",
                    assets_dir.to_str().unwrap_or_default(),
                ],
            )?;
            let archive_name = release_asset_name(&version, target);
            let archive_path = assets_dir.join(&archive_name);
            let digest = sha256_file(&archive_path)?;
            fs::write(assets_dir.join("checksums.txt"), format!("{digest}  {archive_name}\n"))
                .with_context(|| format!("write {}", assets_dir.join("checksums.txt").display()))?;
        }
    }
    if let Some(npm_builder) = &npm_builder {
        run_path_command_checked(
            &root,
            npm_builder,
            &["--version", &version, "--out-dir", npm_out.to_str().unwrap_or_default()],
        )?;
    } else {
        run_path_command_checked(
            &root,
            &self_bin,
            &[
                "npm",
                "package",
                "--version",
                &version,
                "--out-dir",
                npm_out.to_str().unwrap_or_default(),
            ],
        )?;
    }

    let tarball = find_npm_package_tarball(&npm_out)?;
    run_command_checked(
        &root,
        "npm",
        [
            "install",
            "--silent",
            "--global",
            "--prefix",
            prefix_dir.to_str().unwrap_or_default(),
            tarball.to_str().unwrap_or_default(),
        ],
    )?;
    let launcher = prefix_dir.join("bin").join("si");
    if !launcher.exists() {
        return Err(anyhow!("si launcher not installed at {}", launcher.display()));
    }
    run_path_command_with_env_checked(
        &root,
        &launcher,
        &[("SI_NPM_LOCAL_ARCHIVE_DIR", assets_dir.to_str().unwrap_or_default())],
        &["version"],
    )?;
    println!("corepack pnpm install smoke passed");
    Ok(())
}

pub fn run_installer_smoke_homebrew() -> Result<()> {
    let root = std::env::current_dir().context("resolve repo root")?;
    let self_bin = std::env::current_exe().context("resolve current si binary")?;
    let assets_builder = override_executable("SI_BUILD_ASSETS_EXEC");
    if StdCommand::new("brew").arg("--version").output().is_err() {
        eprintln!("SKIP: brew is not available; skipping Homebrew installer smoke");
        return Ok(());
    }

    let version = read_si_version(&root)?;
    let tmp = tempfile::tempdir().context("create temp dir")?;
    let assets_dir = tmp.path().join("assets");
    let tap_dir = tmp.path().join("homebrew-si-smoke");
    let formula_dir = tap_dir.join("Formula");
    let formula_path = formula_dir.join("si-smoke.rb");
    let cache_dir = tmp.path().join("homebrew-cache");
    let keg_prefix = tmp.path().join("si-smoke-prefix");
    fs::create_dir_all(&assets_dir).with_context(|| format!("create {}", assets_dir.display()))?;
    fs::create_dir_all(&formula_dir)
        .with_context(|| format!("create {}", formula_dir.display()))?;
    fs::create_dir_all(&cache_dir).with_context(|| format!("create {}", cache_dir.display()))?;

    let formula_assets_dir =
        if let Ok(provided_assets_dir) = std::env::var("SI_INSTALL_SMOKE_ASSETS_DIR") {
            let provided = if Path::new(&provided_assets_dir).is_absolute() {
                Path::new(&provided_assets_dir).to_path_buf()
            } else {
                root.join(&provided_assets_dir)
            };
            if provided.join("checksums.txt").exists() {
                fs::canonicalize(&provided)
                    .with_context(|| format!("canonicalize {}", provided.display()))?
            } else if let Some(assets_builder) = &assets_builder {
                run_path_command_checked(
                    &root,
                    assets_builder,
                    &["--version", &version, "--out-dir", assets_dir.to_str().unwrap_or_default()],
                )?;
                assets_dir.clone()
            } else {
                run_path_command_checked(
                    &root,
                    &self_bin,
                    &[
                        "self",
                        "assets",
                        "--version",
                        &version,
                        "--out-dir",
                        assets_dir.to_str().unwrap_or_default(),
                    ],
                )?;
                assets_dir.clone()
            }
        } else if let Some(assets_builder) = &assets_builder {
            run_path_command_checked(
                &root,
                assets_builder,
                &["--version", &version, "--out-dir", assets_dir.to_str().unwrap_or_default()],
            )?;
            assets_dir.clone()
        } else {
            run_path_command_checked(
                &root,
                &self_bin,
                &[
                    "self",
                    "assets",
                    "--version",
                    &version,
                    "--out-dir",
                    assets_dir.to_str().unwrap_or_default(),
                ],
            )?;
            assets_dir.clone()
        };

    let checksums_path = formula_assets_dir.join("checksums.txt");
    let base_url = format!("file://{}", formula_assets_dir.display());
    render_tap_formula_with_base_url(
        &version,
        &checksums_path,
        &formula_path,
        "Aureuma/si",
        Some(&base_url),
    )?;
    let rendered = fs::read_to_string(&formula_path)
        .with_context(|| format!("read {}", formula_path.display()))?
        .replacen("class Si < Formula", "class SiSmoke < Formula", 1);
    fs::write(&formula_path, rendered)
        .with_context(|| format!("write {}", formula_path.display()))?;

    run_command_checked(&tap_dir, "git", ["init"])?;
    run_command_checked(&tap_dir, "git", ["config", "user.name", "SI Smoke"])?;
    run_command_checked(&tap_dir, "git", ["config", "user.email", "si-smoke@example.invalid"])?;
    run_command_checked(&tap_dir, "git", ["add", "Formula/si-smoke.rb"])?;
    run_command_checked(&tap_dir, "git", ["commit", "-m", "Add smoke formula"])?;

    let tap_name = "si/homebrew-si-smoke".to_owned();
    let formula_ref = format!("{tap_name}/si-smoke");
    let cache_dir_value = cache_dir.display().to_string();
    let keg_prefix_value = keg_prefix.display().to_string();
    let brew_env = [
        ("HOMEBREW_NO_AUTO_UPDATE", "1"),
        ("HOMEBREW_NO_INSTALL_CLEANUP", "1"),
        ("HOMEBREW_NO_ENV_HINTS", "1"),
        ("HOMEBREW_NO_INSTALL_FROM_API", "1"),
        ("HOMEBREW_CACHE", cache_dir_value.as_str()),
        ("SI_HOMEBREW_SMOKE_PREFIX", keg_prefix_value.as_str()),
    ];

    let mut tap = StdCommand::new("brew");
    tap.current_dir(&root).args(["tap", tap_name.as_str(), tap_dir.to_str().unwrap_or_default()]);
    for (key, value) in &brew_env {
        tap.env(key, value);
    }
    let tap_status = tap.status().context("run brew tap")?;
    if !tap_status.success() {
        return Err(anyhow!("brew tap failed: {tap_status}"));
    }

    let mut install = StdCommand::new("brew");
    install.current_dir(&root).args(["install", formula_ref.as_str()]);
    for (key, value) in &brew_env {
        install.env(key, value);
    }
    let install_status = install.status().context("run brew install")?;
    if !install_status.success() {
        return Err(anyhow!("brew install failed: {install_status}"));
    }

    let mut prefix_cmd = StdCommand::new("brew");
    prefix_cmd.current_dir(&root).args(["--prefix", formula_ref.as_str()]);
    for (key, value) in &brew_env {
        prefix_cmd.env(key, value);
    }
    let prefix_output = prefix_cmd.output().context("run brew --prefix")?;
    if !prefix_output.status.success() {
        return Err(anyhow!("brew --prefix {formula_ref} failed: {}", prefix_output.status));
    }
    let prefix = String::from_utf8_lossy(&prefix_output.stdout).trim().to_owned();
    if prefix.is_empty() {
        return Err(anyhow!("brew --prefix {formula_ref} returned empty output"));
    }

    let installed = PathBuf::from(&prefix).join("bin").join("si");
    if !installed.exists() {
        return Err(anyhow!("expected installed binary at {}", installed.display()));
    }
    run_path_command_checked(&root, &installed, &["version"])?;

    let mut uninstall = StdCommand::new("brew");
    uninstall.current_dir(&root).args(["uninstall", "--force", formula_ref.as_str()]);
    for (key, value) in &brew_env {
        uninstall.env(key, value);
    }
    let uninstall_status = uninstall.status().context("run brew uninstall")?;
    if !uninstall_status.success() {
        return Err(anyhow!("brew uninstall failed: {uninstall_status}"));
    }

    let mut untap = StdCommand::new("brew");
    untap.current_dir(&root).args(["untap", tap_name.as_str()]);
    for (key, value) in &brew_env {
        untap.env(key, value);
    }
    let untap_status = untap.status().context("run brew untap")?;
    if !untap_status.success() {
        return Err(anyhow!("brew untap failed: {untap_status}"));
    }

    println!("homebrew install smoke passed");
    Ok(())
}

pub fn find_npm_package_tarball(dir: &Path) -> Result<PathBuf> {
    let mut matches = fs::read_dir(dir)
        .with_context(|| format!("read {}", dir.display()))?
        .filter_map(|entry| entry.ok().map(|value| value.path()))
        .filter(|path| {
            path.file_name()
                .and_then(|value| value.to_str())
                .map(|value| value.starts_with("aureuma-si-") && value.ends_with(".tgz"))
                .unwrap_or(false)
        })
        .collect::<Vec<_>>();
    matches.sort();
    matches.pop().ok_or_else(|| anyhow!("npm package tarball not found"))
}

pub fn run_path_command_checked(dir: &Path, path: &Path, args: &[&str]) -> Result<()> {
    run_path_command_with_env_checked(dir, path, &[], args)
}

pub fn run_path_command_with_env_checked(
    dir: &Path,
    path: &Path,
    env: &[(&str, &str)],
    args: &[&str],
) -> Result<()> {
    let mut command = StdCommand::new(path);
    command.current_dir(dir).args(args);
    for (key, value) in env {
        command.env(key, value);
    }
    let status = command.status().with_context(|| format!("run {}", path.display()))?;
    if !status.success() {
        return Err(anyhow!("{} failed: {}", path.display(), status));
    }
    Ok(())
}

pub fn run_path_command_expect_fail(dir: &Path, path: &Path, args: &[&str]) -> Result<()> {
    let status = StdCommand::new(path)
        .current_dir(dir)
        .args(args)
        .status()
        .with_context(|| format!("run {}", path.display()))?;
    if status.success() {
        return Err(anyhow!("expected command to fail: {}", path.display()));
    }
    Ok(())
}

pub fn override_executable(var: &str) -> Option<PathBuf> {
    std::env::var(var)
        .ok()
        .map(|value| value.trim().to_owned())
        .filter(|value| !value.is_empty())
        .map(PathBuf::from)
}

pub fn run_homebrew_render_core_formula(
    repo_root: Option<PathBuf>,
    version: Option<String>,
    output: PathBuf,
    repo: String,
) -> Result<()> {
    let repo_root = resolve_release_repo_root(repo_root)?;
    let version = resolve_release_version(&repo_root, version)?;
    validate_release_version(&version)?;
    let source_base = std::env::var("SI_RUST_HOMEBREW_SOURCE_BASE_URL")
        .unwrap_or_else(|_| "https://github.com".to_owned());
    let source_url = format!(
        "{}/{}/archive/refs/tags/{}.tar.gz",
        source_base.trim_end_matches('/'),
        repo.trim(),
        version.trim()
    );
    let temp = tempfile::NamedTempFile::new().context("create temp homebrew source archive")?;
    download_file(&source_url, temp.path())?;
    let digest = sha256_file(temp.path())?;
    let content = format!(
        "class Si < Formula\n  desc \"AI-first CLI for orchestrating coding agents and coding-agent runtime operations\"\n  homepage \"https://github.com/{repo}\"\n  url \"{source_url}\"\n  sha256 \"{digest}\"\n  license \"AGPL-3.0-only\"\n  head \"https://github.com/{repo}.git\", branch: \"main\"\n\n  depends_on \"rust\" => :build\n\n  def install\n    system \"cargo\", \"install\", \"--locked\", *std_cargo_args(path: \"rust/crates/si-cli\"), \"--bin\", \"si\"\n  end\n\n  test do\n    output = shell_output(\"#{{bin}}/si version\")\n    assert_match \"si version\", output\n  end\nend\n"
    );
    if let Some(parent) = output.parent() {
        fs::create_dir_all(parent).with_context(|| format!("create {}", parent.display()))?;
    }
    fs::write(&output, content).with_context(|| format!("write {}", output.display()))?;
    println!("rendered {}", output.display());
    Ok(())
}

pub fn run_homebrew_render_tap_formula(
    repo_root: Option<PathBuf>,
    version: Option<String>,
    checksums: PathBuf,
    output: PathBuf,
    repo: String,
) -> Result<()> {
    let repo_root = resolve_release_repo_root(repo_root)?;
    let version = resolve_release_version(&repo_root, version)?;
    render_tap_formula_with_base_url(&version, &checksums, &output, &repo, None)
}

pub fn run_homebrew_update_tap_repo(
    repo_root: Option<PathBuf>,
    version: Option<String>,
    checksums: PathBuf,
    tap_dir: PathBuf,
    repo: String,
    do_commit: bool,
    do_push: bool,
) -> Result<()> {
    let repo_root = resolve_release_repo_root(repo_root)?;
    let version = resolve_release_version(&repo_root, version)?;
    validate_release_version(&version)?;
    if !tap_dir.is_dir() {
        return Err(anyhow!("tap dir does not exist: {}", tap_dir.display()));
    }
    let formula_dir = tap_dir.join("Formula");
    fs::create_dir_all(&formula_dir)
        .with_context(|| format!("create {}", formula_dir.display()))?;
    let formula_path = formula_dir.join("si.rb");
    render_tap_formula_with_base_url(&version, &checksums, &formula_path, &repo, None)?;
    if !do_commit {
        return Ok(());
    }
    run_command_checked(&tap_dir, "git", ["add", "Formula/si.rb"])?;
    let status = StdCommand::new("git")
        .current_dir(&tap_dir)
        .args(["diff", "--cached", "--quiet"])
        .status()
        .context("run git diff --cached --quiet")?;
    if status.success() {
        println!("no formula changes to commit");
        return Ok(());
    }
    run_command_checked(
        &tap_dir,
        "git",
        ["commit", "-m", &format!("chore: update si formula to {}", version.trim())],
    )?;
    if do_push {
        run_command_checked(&tap_dir, "git", ["push"])?;
    }
    Ok(())
}

pub fn validate_release_version(version: &str) -> Result<()> {
    if !version.trim().starts_with('v') {
        return Err(anyhow!("version must include v prefix, got: {}", version.trim()));
    }
    if version.trim() == "v" {
        return Err(anyhow!("invalid version"));
    }
    Ok(())
}

pub fn validate_release_tag_format(tag: &str) -> Result<()> {
    let pattern = Regex::new(r"^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$")
        .context("compile release tag regex")?;
    if !pattern.is_match(tag.trim()) {
        return Err(anyhow!(
            "tag must match vX.Y.Z (optionally with a prerelease/build suffix), got: {}",
            tag.trim()
        ));
    }
    Ok(())
}

pub fn run_validate_release_version(tag: String) -> Result<()> {
    let tag = tag.trim().to_owned();
    if tag.is_empty() {
        return Err(anyhow!("--tag is required"));
    }
    validate_release_tag_format(&tag)?;
    let cwd = std::env::current_dir().context("read current dir")?;
    let actual = read_si_version(&cwd)?;
    if actual != tag {
        return Err(anyhow!("workspace Cargo.toml has {actual}, but release tag is {tag}"));
    }
    println!("release tag and workspace Cargo.toml are aligned ({tag})");
    Ok(())
}

pub fn verify_release_archive_contents(path: &Path, target: ReleaseTarget) -> Result<()> {
    let file = File::open(path).with_context(|| format!("open {}", path.display()))?;
    let decoder = flate2::read::GzDecoder::new(file);
    let mut archive = tar::Archive::new(decoder);
    let mut names = archive
        .entries()
        .with_context(|| format!("read archive entries from {}", path.display()))?
        .map(|entry| {
            entry.with_context(|| format!("read archive entry from {}", path.display())).and_then(
                |entry| {
                    entry
                        .path()
                        .map(|value| value.display().to_string())
                        .with_context(|| format!("read archive path from {}", path.display()))
                },
            )
        })
        .collect::<Result<Vec<_>>>()?;
    names.sort();

    let has_si = names.iter().any(|name| name == "si" || name.ends_with("/si"));
    let has_readme = names.iter().any(|name| name == "README.md" || name.ends_with("/README.md"));
    let has_license = names.iter().any(|name| name == "LICENSE" || name.ends_with("/LICENSE"));

    if !has_si {
        return Err(anyhow!("archive missing si binary: {}", path.display()));
    }
    if !has_readme {
        return Err(anyhow!("archive missing README.md: {}", path.display()));
    }
    if !has_license {
        return Err(anyhow!("archive missing LICENSE: {}", path.display()));
    }

    let temp = tempfile::tempdir().context("create release verification temp dir")?;
    let file = File::open(path).with_context(|| format!("open {}", path.display()))?;
    let decoder = flate2::read::GzDecoder::new(file);
    tar::Archive::new(decoder)
        .unpack(temp.path())
        .with_context(|| format!("extract {} for binary verification", path.display()))?;
    let binary = find_release_binary(temp.path())
        .ok_or_else(|| anyhow!("archive missing extracted si binary: {}", path.display()))?;
    verify_release_binary_format(&binary, target)
        .with_context(|| format!("verify binary format for {}", path.display()))?;
    Ok(())
}

pub fn find_release_binary(root: &Path) -> Option<PathBuf> {
    let entries = fs::read_dir(root).ok()?;
    for entry in entries.flatten() {
        let path = entry.path();
        if path.is_dir() {
            if let Some(found) = find_release_binary(&path) {
                return Some(found);
            }
            continue;
        }
        if path.file_name().and_then(|value| value.to_str()) == Some("si") {
            return Some(path);
        }
    }
    None
}

pub fn verify_release_binary_format(binary: &Path, target: ReleaseTarget) -> Result<()> {
    let output = StdCommand::new("file")
        .arg(binary)
        .output()
        .context("run file for release binary verification")?;
    if !output.status.success() {
        return Err(anyhow!(
            "file failed for {}: {}",
            binary.display(),
            String::from_utf8_lossy(&output.stderr).trim()
        ));
    }
    let stdout = String::from_utf8_lossy(&output.stdout).to_ascii_lowercase();
    let matches = match target.id {
        "linux-amd64" => {
            stdout.contains("elf") && (stdout.contains("x86-64") || stdout.contains("x86_64"))
        }
        "linux-arm64" => {
            stdout.contains("elf") && (stdout.contains("aarch64") || stdout.contains("arm64"))
        }
        "darwin-amd64" => stdout.contains("mach-o") && stdout.contains("x86_64"),
        "darwin-arm64" => stdout.contains("mach-o") && stdout.contains("arm64"),
        _ => false,
    };
    if !matches {
        return Err(anyhow!(
            "release binary format mismatch for {}: target {} expected {}, got {}",
            binary.display(),
            target.id,
            target.rust_triple,
            stdout.trim()
        ));
    }
    Ok(())
}

pub fn run_build_self_verify_release_assets(version: String, out_dir: PathBuf) -> Result<()> {
    validate_release_version(&version)?;
    if !out_dir.is_dir() {
        return Err(anyhow!("release output dir does not exist: {}", out_dir.display()));
    }
    let checksums_path = out_dir.join("checksums.txt");
    let checksums = parse_checksums_file(&checksums_path)?;
    for target in SUPPORTED_RELEASE_TARGETS {
        let asset_name = release_asset_name(&version, *target);
        let asset_path = out_dir.join(&asset_name);
        if !asset_path.exists() {
            return Err(anyhow!("missing release asset: {}", asset_path.display()));
        }
        let expected_sha = checksums
            .get(&asset_name)
            .ok_or_else(|| anyhow!("checksum missing for {asset_name}"))?;
        let actual_sha = sha256_file(&asset_path)?;
        if actual_sha != *expected_sha {
            return Err(anyhow!(
                "checksum mismatch for {asset_name}: expected {expected_sha}, got {actual_sha}"
            ));
        }
        verify_release_archive_contents(&asset_path, *target)?;
    }
    println!("verified release assets in {}", out_dir.display());
    Ok(())
}

pub fn download_file(url: &str, output: &Path) -> Result<()> {
    let response =
        BlockingHttpClient::new().get(url).send().with_context(|| format!("download {url}"))?;
    let response = response.error_for_status().with_context(|| format!("download {url}"))?;
    let bytes = response.bytes().context("read download body")?;
    fs::write(output, &bytes).with_context(|| format!("write {}", output.display()))?;
    Ok(())
}

pub fn render_tap_formula_with_base_url(
    version: &str,
    checksums_path: &Path,
    output_path: &Path,
    repo: &str,
    base_url_override: Option<&str>,
) -> Result<()> {
    validate_release_version(version)?;
    if !checksums_path.exists() {
        return Err(anyhow!("checksums file not found: {}", checksums_path.display()));
    }
    let version_no_v = version.trim_start_matches('v');
    let sha_by_asset = parse_checksums_file(checksums_path)?;
    let lookup = |name: &str| -> Result<String> {
        sha_by_asset
            .get(name)
            .filter(|value| !value.trim().is_empty())
            .cloned()
            .ok_or_else(|| anyhow!("checksum not found for {name}"))
    };
    let asset_darwin_arm64 = format!("si_{version_no_v}_darwin_arm64.tar.gz");
    let asset_darwin_amd64 = format!("si_{version_no_v}_darwin_amd64.tar.gz");
    let asset_linux_arm64 = format!("si_{version_no_v}_linux_arm64.tar.gz");
    let asset_linux_amd64 = format!("si_{version_no_v}_linux_amd64.tar.gz");
    let base_url = base_url_override
        .map(|value| value.trim_end_matches('/').to_owned())
        .unwrap_or_else(|| format!("https://github.com/{repo}/releases/download/{version}"));
    let content = format!(
        "class Si < Formula\n  desc \"AI-first CLI for orchestrating coding agents and coding-agent runtime operations\"\n  homepage \"https://github.com/{repo}\"\n  version \"{version_no_v}\"\n  license \"AGPL-3.0-only\"\n\n  on_macos do\n    if Hardware::CPU.arm?\n      url \"{base_url}/{asset_darwin_arm64}\"\n      sha256 \"{}\"\n    else\n      url \"{base_url}/{asset_darwin_amd64}\"\n      sha256 \"{}\"\n    end\n  end\n\n  on_linux do\n    if Hardware::CPU.arm?\n      url \"{base_url}/{asset_linux_arm64}\"\n      sha256 \"{}\"\n    elsif Hardware::CPU.intel?\n      url \"{base_url}/{asset_linux_amd64}\"\n      sha256 \"{}\"\n    end\n  end\n\n  def install\n    stage = buildpath/\"si-stage\"\n    stage.mkpath\n    system \"tar\", \"-xzf\", cached_download, \"-C\", stage\n\n    binary = Dir[\"#{{stage}}/si_*/si\"].first\n    binary = (stage/\"si\").to_s if binary.nil? && (stage/\"si\").exist?\n    raise \"si binary not found in release archive\" if binary.nil? || binary.empty?\n\n    bin.install binary => \"si\"\n    chmod 0o755, bin/\"si\"\n  end\n\n  test do\n    output = shell_output(\"#{{bin}}/si version\")\n    assert_match \"si version\", output\n  end\nend\n",
        lookup(&asset_darwin_arm64)?,
        lookup(&asset_darwin_amd64)?,
        lookup(&asset_linux_arm64)?,
        lookup(&asset_linux_amd64)?,
    );
    if let Some(parent) = output_path.parent() {
        fs::create_dir_all(parent).with_context(|| format!("create {}", parent.display()))?;
    }
    fs::write(output_path, content).with_context(|| format!("write {}", output_path.display()))?;
    println!("rendered {}", output_path.display());
    Ok(())
}

pub fn parse_checksums_file(path: &Path) -> Result<std::collections::HashMap<String, String>> {
    let raw = fs::read_to_string(path).with_context(|| format!("read {}", path.display()))?;
    let mut out = std::collections::HashMap::new();
    for line in raw.lines() {
        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }
        let mut parts = trimmed.split_whitespace();
        let sha = parts.next().ok_or_else(|| anyhow!("invalid checksum line"))?;
        let name = parts.next().ok_or_else(|| anyhow!("invalid checksum line"))?;
        out.insert(name.to_owned(), sha.to_owned());
    }
    Ok(out)
}

pub fn run_command_checked<const N: usize>(dir: &Path, name: &str, args: [&str; N]) -> Result<()> {
    let output = StdCommand::new(name)
        .current_dir(dir)
        .args(args)
        .output()
        .with_context(|| format!("run {name}"))?;
    if !output.status.success() {
        let stdout = String::from_utf8_lossy(&output.stdout).trim().to_owned();
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_owned();
        let mut details = Vec::new();
        if !stdout.is_empty() {
            details.push(format!("stdout:\n{stdout}"));
        }
        if !stderr.is_empty() {
            details.push(format!("stderr:\n{stderr}"));
        }
        return Err(anyhow!(
            "{} failed: {}{}{}",
            name,
            output.status,
            if details.is_empty() { "" } else { "\n" },
            details.join("\n")
        ));
    }
    Ok(())
}

/// Execute one `si-release` invocation (mirrors the former `si build` dispatch).
pub fn run(command: BuildCommand) -> Result<()> {
    match command {
        BuildCommand::Self_ { args } => match args.command {
            Some(BuildSelfCommand::Build(BuildSelfBuildArgs {
                repo,
                no_upgrade,
                output,
                install_path,
                quiet,
                cargo,
            })) => run_build_self_build(repo, install_path, output, no_upgrade, quiet, cargo)?,
            Some(BuildSelfCommand::Check(BuildSelfCheckArgs { repo, quiet, cargo })) => {
                run_build_self_check(repo, quiet, cargo)?
            }
            Some(BuildSelfCommand::Upgrade(BuildSelfUpgradeArgs {
                repo,
                install_path,
                quiet,
                cargo,
            })) => run_build_self_upgrade(repo, install_path, quiet, cargo)?,
            Some(BuildSelfCommand::Run(BuildSelfRunArgs { repo, cargo, args })) => {
                run_build_self_run(repo, cargo, args)?
            }
            Some(BuildSelfCommand::ReleaseAsset { repo_root, version, target, out_dir }) => {
                run_build_self_release_asset(repo_root, version, target, out_dir)?
            }
            Some(BuildSelfCommand::ReleaseAssets { repo, version, out_dir }) => {
                run_build_self_release_assets(repo, version, out_dir)?
            }
            Some(BuildSelfCommand::ValidateReleaseVersion { tag }) => {
                run_validate_release_version(tag)?
            }
            Some(BuildSelfCommand::VerifyReleaseAssets { version, out_dir }) => {
                run_build_self_verify_release_assets(version, out_dir)?
            }
            None => run_build_self_build(
                args.default_build.repo,
                args.default_build.install_path,
                args.default_build.output,
                args.default_build.no_upgrade,
                args.default_build.quiet,
                args.default_build.cargo,
            )?,
        },
        BuildCommand::Installer { command } => match command {
            BuildInstallerCommand::SettingsHelper { settings, default_browser, print, check } => {
                run_installer_settings_helper(settings, default_browser, print, check)?
            }
            BuildInstallerCommand::Run {
                backend,
                source_dir,
                repo: _repo,
                repo_url,
                ref_,
                version: _version,
                install_dir,
                install_path,
                force,
                uninstall,
                toolchain_mode,
                with_buildx: _with_buildx,
                no_buildx: _no_buildx,
                os_override,
                arch_override,
                tmp_dir: _tmp_dir,
                yes: _yes,
                dry_run,
                quiet,
                no_path_hint,
            } => run_installer(InstallerRunConfig {
                backend,
                source_dir,
                repo_url,
                ref_name: ref_,
                install_dir,
                install_path,
                force,
                uninstall,
                toolchain_mode,
                os_override,
                arch_override,
                dry_run,
                quiet,
                no_path_hint,
            })?,
            BuildInstallerCommand::SmokeHost => run_installer_smoke_host()?,
            BuildInstallerCommand::SmokeNpm => run_installer_smoke_npm()?,
            BuildInstallerCommand::SmokeHomebrew => run_installer_smoke_homebrew()?,
        },
        BuildCommand::Npm { command } => match command {
            BuildNpmCommand::BuildPackage { repo_root, version, out_dir } => {
                run_build_npm_package(repo_root, version, out_dir)?
            }
            BuildNpmCommand::PublishPackage { repo_root, version, out_dir, token_env, dry_run } => {
                run_publish_npm_package(repo_root, version, out_dir, token_env, dry_run)?
            }
        },
        BuildCommand::Homebrew { command } => match command {
            BuildHomebrewCommand::RenderCoreFormula { repo_root, version, output, repo } => {
                run_homebrew_render_core_formula(repo_root, version, output, repo)?
            }
            BuildHomebrewCommand::RenderTapFormula {
                repo_root,
                version,
                checksums,
                output,
                repo,
            } => run_homebrew_render_tap_formula(repo_root, version, checksums, output, repo)?,
            BuildHomebrewCommand::UpdateTapRepo {
                repo_root,
                version,
                checksums,
                tap_dir,
                repo,
                commit,
                push,
            } => run_homebrew_update_tap_repo(
                repo_root, version, checksums, tap_dir, repo, commit, push,
            )?,
        },
    }
    Ok(())
}
