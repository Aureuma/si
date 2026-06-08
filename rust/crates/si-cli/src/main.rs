#![allow(clippy::large_enum_variant)]
#![allow(clippy::upper_case_acronyms)]
#![allow(
    clippy::too_many_arguments,
    clippy::type_complexity,
    clippy::manual_ignore_case_cmp,
    clippy::redundant_locals,
    clippy::manual_map,
    clippy::unnecessary_lazy_evaluations,
    clippy::never_loop,
    clippy::ptr_arg,
    clippy::collapsible_if
)]

mod vault_compat;

use anyhow::{Context, Result, anyhow};
use base64::Engine as _;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use chrono::{DateTime, Utc};
use clap::builder::styling::{AnsiColor, Effects, Styles};
use clap::{
    ArgAction, Args, ColorChoice, CommandFactory, FromArgMatches, Parser, Subcommand, ValueEnum,
};
use comfy_table::modifiers::UTF8_ROUND_CORNERS;
use comfy_table::presets::UTF8_FULL;
use comfy_table::{Attribute, Cell, Color, ColumnConstraint, ContentArrangement, Table, Width};
use flate2::Compression;
use flate2::write::GzEncoder;
use regex::Regex;
use reqwest::Method;
use reqwest::blocking::Client as BlockingHttpClient;
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use si_codex::{
    CodexProfileFortSessionPaths, DEFAULT_CODEX_WORKER_SLOT, build_codex_app_server_status_input,
    codex_fort_agent_id, codex_profile_fort_session_paths, codex_tmux_session_name_for_slot,
    codex_worker_name, codex_worker_slot_name, parse_codex_app_server_status,
};
use si_command_manifest::{CommandCategory, CommandSpec, find_root_command, visible_root_commands};
use si_config::paths::SiPaths;
use si_config::runtime::git_repo_root_from;
use si_config::settings::{CodexProfileEntry, FortSettings, Settings, SurfSettings, VivaSettings};
use si_fort::{
    BootstrapView, PersistedRuntimeAgentState, PersistedSessionState, RefreshOutcome,
    RefreshSuccess, SessionState, acquire_session_lock,
    apply_refresh_outcome_to_persisted_session_state, build_bootstrap_view,
    classify_persisted_session_state, clear_persisted_runtime_agent_state,
    clear_persisted_session_state, load_persisted_runtime_agent_state,
    load_persisted_session_state, save_persisted_runtime_agent_state, save_persisted_session_state,
    teardown_persisted_session_state,
};
use si_nucleus_core::{ProfileName, RunId, RunStatus, SessionId, WorkerId};
use si_nucleus_runtime::{
    NucleusRuntime, RunInputItem, RunTurnSpec, RuntimeStatusSnapshot, SessionOpenSpec,
    WorkerLaunchSpec,
};
use si_nucleus_runtime_codex::CodexNucleusRuntime;
use si_vault::TrustStore;
use si_warmup::{
    WarmupState, classify_autostart_request, default_autostart_marker_path,
    default_disabled_marker_path, default_state_path as default_warmup_state_path,
    load_state as load_warmup_state, read_marker_state as read_warmup_marker_state,
    render_state_text as render_warmup_state_text, save_state as save_warmup_state,
    set_disabled_marker as set_rust_warmup_disabled_marker,
    write_autostart_marker as write_rust_warmup_autostart_marker,
};
use std::collections::BTreeMap;
use std::env;
use std::fmt;
use std::fs;
use std::fs::File;
use std::io::{self, IsTerminal, Read, Write};
use std::net::IpAddr;
#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;
#[cfg(unix)]
use std::path::{Path, PathBuf};
use std::process::{Command as StdCommand, ExitStatus};
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::thread;
use std::time::Duration;
use tar::Builder as TarBuilder;
use tungstenite::client::IntoClientRequest;
use tungstenite::http::header::{AUTHORIZATION, HeaderValue};
use tungstenite::{Message as WsMessage, connect as ws_connect};
use vault_compat::{VaultCommand, run_vault_command};

#[derive(Debug, Parser)]
#[command(
    name = "si",
    disable_version_flag = true,
    disable_help_subcommand = true,
    arg_required_else_help = true
)]
struct Cli {
    #[arg(short = 'v', long = "version", action = ArgAction::SetTrue)]
    version_flag: bool,

    #[command(subcommand)]
    command: Option<Command>,
}

#[allow(clippy::large_enum_variant)]
#[derive(Debug, Subcommand)]
enum Command {
    Version,
    Help {
        command: Option<String>,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Build {
        #[command(subcommand)]
        command: BuildCommand,
    },
    Commands(CommandsArgs),
    Doctor {
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Settings(SettingsArgs),
    Image {
        #[command(subcommand)]
        command: ImageCommand,
    },
    Codex {
        #[command(subcommand)]
        command: Box<CodexCommand>,
    },
    Nucleus {
        #[command(subcommand)]
        command: NucleusCommand,
    },
    Surf {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        repo: Option<PathBuf>,
        #[arg(long, action = ArgAction::SetTrue)]
        build: bool,
        #[arg(long, action = ArgAction::SetTrue)]
        no_build: bool,
        #[arg(long)]
        bin: Option<PathBuf>,
        #[arg(long)]
        vnc_password_fort_key: Option<String>,
        #[arg(long)]
        vnc_password_fort_repo: Option<String>,
        #[arg(long)]
        vnc_password_fort_env: Option<String>,
        #[arg(trailing_var_arg = true, allow_hyphen_values = true)]
        args: Vec<String>,
    },
    Viva {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        repo: Option<PathBuf>,
        #[arg(long, action = ArgAction::SetTrue)]
        build: bool,
        #[arg(long, action = ArgAction::SetTrue)]
        no_build: bool,
        #[arg(long)]
        bin: Option<PathBuf>,
        #[arg(trailing_var_arg = true, allow_hyphen_values = true)]
        args: Vec<String>,
    },
    Fort {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        repo: Option<PathBuf>,
        #[arg(long, action = ArgAction::SetTrue)]
        build: bool,
        #[arg(long, action = ArgAction::SetTrue)]
        no_build: bool,
        #[arg(long)]
        bin: Option<PathBuf>,
        #[arg(trailing_var_arg = true, allow_hyphen_values = true)]
        args: Vec<String>,
    },
    Vault {
        #[command(subcommand)]
        command: VaultCommand,
    },
}

#[derive(Debug, Subcommand)]
enum NucleusCommand {
    Status {
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Profile {
        #[command(subcommand)]
        command: NucleusProfileCommand,
    },
    Producer {
        #[command(subcommand)]
        command: NucleusProducerCommand,
    },
    Service {
        #[command(subcommand)]
        command: NucleusServiceCommand,
    },
    Task {
        #[command(subcommand)]
        command: NucleusTaskCommand,
    },
    Worker {
        #[command(subcommand)]
        command: NucleusWorkerCommand,
    },
    Session {
        #[command(subcommand)]
        command: NucleusSessionCommand,
    },
    Run {
        #[command(subcommand)]
        command: NucleusRunCommand,
    },
    Events {
        #[command(subcommand)]
        command: NucleusEventsCommand,
    },
}

#[derive(Debug, Subcommand)]
enum NucleusProfileCommand {
    List {
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum NucleusProducerCommand {
    Cron {
        #[command(subcommand)]
        command: NucleusCronCommand,
    },
    Hook {
        #[command(subcommand)]
        command: NucleusHookCommand,
    },
}

#[derive(Debug, Subcommand)]
enum NucleusCronCommand {
    List {
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Inspect {
        rule_name: String,
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Upsert {
        name: String,
        #[arg(long)]
        schedule_kind: String,
        #[arg(long)]
        schedule: String,
        #[arg(long)]
        instructions: String,
        #[arg(long)]
        enabled: Option<bool>,
        #[arg(long, action = ArgAction::SetTrue)]
        reset: bool,
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Delete {
        rule_name: String,
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum NucleusHookCommand {
    List {
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Inspect {
        rule_name: String,
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Upsert {
        name: String,
        #[arg(long)]
        match_event_type: String,
        #[arg(long)]
        instructions: String,
        #[arg(long)]
        enabled: Option<bool>,
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Delete {
        rule_name: String,
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum NucleusServiceCommand {
    Install {
        #[arg(long)]
        state_dir: Option<PathBuf>,
        #[arg(long)]
        bind_addr: Option<String>,
        #[arg(long)]
        service_dir: Option<PathBuf>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Uninstall {
        #[arg(long)]
        service_dir: Option<PathBuf>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Start {
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Stop {
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Restart {
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Status {
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    #[command(hide = true)]
    Run {
        #[arg(long)]
        state_dir: Option<PathBuf>,
        #[arg(long)]
        bind_addr: Option<String>,
        #[arg(long)]
        nucleus_bin: Option<PathBuf>,
    },
}

#[derive(Debug, Subcommand)]
enum NucleusTaskCommand {
    Create {
        title: String,
        instructions: String,
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long)]
        profile: Option<String>,
        #[arg(long)]
        requires_fort: Option<bool>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    List {
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Inspect {
        task_id: String,
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Cancel {
        task_id: String,
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Prune {
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value_t = 30)]
        older_than_days: u64,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum NucleusWorkerCommand {
    Probe {
        profile: String,
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long)]
        worker_id: Option<String>,
        #[arg(long)]
        home_dir: Option<PathBuf>,
        #[arg(long)]
        codex_home: Option<PathBuf>,
        #[arg(long)]
        workdir: Option<PathBuf>,
        #[arg(long = "env")]
        env: Vec<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    List {
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Inspect {
        worker_id: String,
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Restart {
        worker_id: String,
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    RepairAuth {
        worker_id: String,
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum NucleusSessionCommand {
    Create {
        profile: String,
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long)]
        worker_id: Option<String>,
        #[arg(long)]
        thread_id: Option<String>,
        #[arg(long)]
        home_dir: Option<PathBuf>,
        #[arg(long)]
        codex_home: Option<PathBuf>,
        #[arg(long)]
        workdir: Option<PathBuf>,
        #[arg(long = "env")]
        env: Vec<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    List {
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Show {
        session_id: String,
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum NucleusRunCommand {
    SubmitTurn {
        session_id: String,
        prompt: String,
        #[arg(long)]
        task_id: String,
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Inspect {
        run_id: String,
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Cancel {
        run_id: String,
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum NucleusEventsCommand {
    Subscribe {
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long)]
        count: Option<usize>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Ingest {
        #[arg(long)]
        endpoint: Option<String>,
        #[arg(long = "type")]
        event_type: String,
        #[arg(long)]
        source: String,
        #[arg(long)]
        profile: Option<String>,
        #[arg(long)]
        payload: String,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum CommandsCommand {
    List(CommandsListArgs),
}

#[derive(Debug, Args, Clone)]
struct CommandsListArgs {
    #[arg(long, default_value = "text")]
    format: OutputFormat,
}

#[derive(Debug, Args)]
struct CommandsArgs {
    #[command(subcommand)]
    command: Option<CommandsCommand>,
    #[command(flatten)]
    default_list: CommandsListArgs,
}

#[derive(Debug, Subcommand)]
enum ImageCommand {
    Unsplash {
        #[command(subcommand)]
        command: ImageProviderCommand,
    },
    Pexels {
        #[command(subcommand)]
        command: ImageProviderCommand,
    },
    Pixabay {
        #[command(subcommand)]
        command: ImageProviderCommand,
    },
}

#[derive(Debug, Subcommand)]
enum ImageProviderCommand {
    Auth {
        #[command(subcommand)]
        command: ImageAuthCommand,
    },
    Search {
        #[arg(long)]
        query: String,
        #[arg(long, default_value_t = 1)]
        page: i32,
        #[arg(long = "per-page", default_value_t = 10)]
        per_page: i32,
        #[arg(long)]
        orientation: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        json: bool,
    },
}

#[derive(Debug, Subcommand)]
enum ImageAuthCommand {
    Status {
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        json: bool,
    },
}

#[derive(Debug, Subcommand)]
enum SettingsCommand {
    Show(SettingsShowArgs),
}

#[derive(Debug, Args, Clone)]
struct SettingsShowArgs {
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long, default_value = "text")]
    format: OutputFormat,
}

#[derive(Debug, Args)]
struct SettingsArgs {
    #[command(subcommand)]
    command: Option<SettingsCommand>,
    #[command(flatten)]
    default_show: SettingsShowArgs,
}

#[derive(Debug, Subcommand)]
enum BuildCommand {
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
struct BuildCargoOptions {
    #[arg(long = "target-dir")]
    target_dir: Option<PathBuf>,
    #[arg(long, action = ArgAction::SetTrue)]
    timings: bool,
}

#[derive(Debug, Args)]
struct BuildSelfBuildArgs {
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
struct BuildSelfUpgradeArgs {
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
struct BuildSelfCheckArgs {
    #[arg(long)]
    repo: Option<PathBuf>,
    #[arg(long)]
    quiet: bool,
    #[command(flatten)]
    cargo: BuildCargoOptions,
}

#[derive(Debug, Args)]
struct BuildSelfRunArgs {
    #[arg(long)]
    repo: Option<PathBuf>,
    #[command(flatten)]
    cargo: BuildCargoOptions,
    #[arg(trailing_var_arg = true, allow_hyphen_values = true)]
    args: Vec<String>,
}

#[derive(Debug, Args)]
struct BuildSelfArgs {
    #[command(subcommand)]
    command: Option<BuildSelfCommand>,
    #[command(flatten)]
    default_build: BuildSelfBuildArgs,
}

#[derive(Debug, Subcommand)]
enum BuildSelfCommand {
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
enum BuildInstallerCommand {
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
enum BuildNpmCommand {
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
    #[command(name = "vault", alias = "publishfromvault", alias = "publish-from-vault")]
    PublishFromVault {
        #[arg(long = "repo-root")]
        repo_root: Option<PathBuf>,
        #[arg(long)]
        version: Option<String>,
        #[arg(long = "out-dir")]
        out_dir: Option<PathBuf>,
        #[arg(long = "token-env", default_value = "NPM_GAT_AUREUMA_VANGUARDA")]
        token_env: String,
        #[arg(long = "file")]
        file: Option<PathBuf>,
        #[arg(long = "dry-run", action = ArgAction::SetTrue)]
        dry_run: bool,
    },
}

#[derive(Debug, Subcommand)]
enum BuildHomebrewCommand {
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

#[derive(Debug, Args)]
struct CodexSpawnStartArgs {
    profile: Option<String>,
    #[arg(long = "profile", conflicts_with = "profile")]
    profile_flag: Option<String>,
    #[arg(long = "slot")]
    worker_slot: Option<String>,
    #[arg(long)]
    workspace: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct CodexProfileListArgs {
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long, default_value = "text")]
    format: OutputFormat,
}

#[derive(Debug, Args)]
struct CodexProfileShowArgs {
    profile: Option<String>,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long, default_value = "text")]
    format: OutputFormat,
}

#[derive(Debug, Args)]
struct CodexProfileAddArgs {
    profile: String,
    #[arg(long)]
    name: Option<String>,
    #[arg(long)]
    email: Option<String>,
    #[arg(long)]
    auth_path: Option<String>,
    #[arg(long, default_value_t = false)]
    activate: bool,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long, default_value = "json")]
    format: OutputFormat,
}

#[derive(Debug, Args)]
struct CodexProfileRemoveArgs {
    profile: Option<String>,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct CodexProfileLoginArgs {
    profile: Option<String>,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long)]
    codex_bin: Option<PathBuf>,
    #[arg(long, default_value = "text")]
    format: OutputFormat,
}

#[derive(Debug, Args)]
struct CodexProfileSwapArgs {
    profile: Option<String>,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long, default_value = "json")]
    format: OutputFormat,
}

#[derive(Debug, Subcommand)]
enum CodexProfileCommand {
    List(CodexProfileListArgs),
    Show(CodexProfileShowArgs),
    Add(CodexProfileAddArgs),
    Remove(CodexProfileRemoveArgs),
    Login(CodexProfileLoginArgs),
    Swap(CodexProfileSwapArgs),
}

#[derive(Debug, Args)]
struct CodexTmuxArgs {
    profile: Option<String>,
    #[arg(long = "profile", conflicts_with = "profile")]
    profile_flag: Option<String>,
    #[arg(long = "slot")]
    worker_slot: Option<String>,
    #[arg(long)]
    format: Option<OutputFormat>,
}

#[derive(Debug, Args)]
struct CodexRemoveArgs {
    profile: Option<String>,
    #[arg(long = "profile", conflicts_with = "profile")]
    profile_flag: Option<String>,
    #[arg(long = "slot")]
    worker_slot: Option<String>,
    #[arg(long, default_value_t = false, conflicts_with_all = ["profile", "profile_flag", "worker_slot"])]
    all: bool,
    #[arg(long, default_value = "text")]
    format: OutputFormat,
}

#[derive(Debug, Args)]
struct CodexStopArgs {
    profile: Option<String>,
    #[arg(long = "profile", conflicts_with = "profile")]
    profile_flag: Option<String>,
    #[arg(long = "slot")]
    worker_slot: Option<String>,
    #[arg(long, default_value_t = false, conflicts_with_all = ["profile", "profile_flag", "worker_slot"])]
    all: bool,
    #[arg(long, default_value = "text")]
    format: OutputFormat,
}

#[derive(Debug, Args)]
struct CodexTailArgs {
    profile: Option<String>,
    #[arg(long = "profile", conflicts_with = "profile")]
    profile_flag: Option<String>,
    #[arg(long = "slot")]
    worker_slot: Option<String>,
    #[arg(long, default_value = "200")]
    tail: String,
}

#[derive(Debug, Args)]
struct CodexShellArgs {
    #[arg(long = "profile")]
    profile: Option<String>,
    #[arg(long = "slot")]
    worker_slot: Option<String>,
    #[arg(trailing_var_arg = true, allow_hyphen_values = true)]
    command: Vec<String>,
}

#[derive(Debug, Args)]
struct CodexRepairAuthArgs {
    profile: Option<String>,
    #[arg(long = "profile", conflicts_with = "profile")]
    profile_flag: Option<String>,
    #[arg(long = "slot")]
    worker_slot: Option<String>,
    #[arg(long, default_value_t = false, conflicts_with_all = ["profile", "profile_flag", "worker_slot"])]
    all: bool,
    #[arg(long, default_value = "text")]
    format: OutputFormat,
}

#[allow(clippy::enum_variant_names)]
#[derive(Debug, Subcommand)]
enum CodexCommand {
    #[command(name = "profile", alias = "profiles")]
    Profile {
        #[command(subcommand)]
        command: CodexProfileCommand,
    },
    Spawn(CodexSpawnStartArgs),
    Remove(CodexRemoveArgs),
    Stop(CodexStopArgs),
    Tail(CodexTailArgs),
    Shell(CodexShellArgs),
    List {
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Tmux(CodexTmuxArgs),
    #[command(name = "repair-auth")]
    RepairAuth(CodexRepairAuthArgs),
    Warmup {
        #[command(subcommand)]
        command: WarmupCommand,
    },
    #[command(name = "respawn")]
    Respawn(CodexSpawnStartArgs),
}

#[derive(Debug, Parser)]
struct FortSessionStateCli {
    #[command(subcommand)]
    command: FortSessionStateCommand,
}

#[derive(Debug, Subcommand)]
enum FortSessionStateCommand {
    Show {
        #[arg(long)]
        path: PathBuf,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Write {
        #[arg(long)]
        path: PathBuf,
        #[arg(long)]
        state_json: String,
    },
    Clear {
        #[arg(long)]
        path: PathBuf,
    },
    #[command(name = "bootstrap", alias = "bootstrapview", alias = "bootstrap-view")]
    Bootstrap {
        #[arg(long)]
        path: PathBuf,
        #[arg(long)]
        profile_id: Option<String>,
        #[arg(long)]
        access_token_path: String,
        #[arg(long)]
        refresh_token_path: String,
        #[arg(long)]
        access_token_runtime_path: String,
        #[arg(long)]
        refresh_token_runtime_path: String,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Classify {
        #[arg(long)]
        path: PathBuf,
        #[arg(long)]
        now_unix: i64,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    #[command(name = "refresh", alias = "refreshoutcome", alias = "refresh-outcome")]
    Refresh {
        #[arg(long)]
        path: PathBuf,
        #[arg(long)]
        outcome: FortRefreshOutcomeArg,
        #[arg(long)]
        now_unix: i64,
        #[arg(long)]
        access_expires_at_unix: Option<i64>,
        #[arg(long)]
        refresh_expires_at_unix: Option<i64>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Teardown {
        #[arg(long)]
        path: PathBuf,
        #[arg(long)]
        now_unix: i64,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
}

#[derive(Debug, Parser)]
struct FortRuntimeAgentStateCli {
    #[command(subcommand)]
    command: FortRuntimeAgentStateCommand,
}

#[derive(Debug, Subcommand)]
enum FortRuntimeAgentStateCommand {
    Show {
        #[arg(long)]
        path: PathBuf,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Write {
        #[arg(long)]
        path: PathBuf,
        #[arg(long)]
        state_json: String,
    },
    Clear {
        #[arg(long)]
        path: PathBuf,
    },
}

#[derive(Debug, Parser)]
struct FortConfigCli {
    #[command(subcommand)]
    command: FortConfigCommand,
}

#[derive(Debug, Subcommand)]
enum FortConfigCommand {
    Show {
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Set {
        #[arg(long)]
        repo: Option<String>,
        #[arg(long)]
        bin: Option<String>,
        #[arg(long)]
        build: Option<bool>,
        #[arg(long)]
        host: Option<String>,
        #[arg(long)]
        runtime_host: Option<String>,
    },
}

#[derive(Debug, Parser)]
struct VivaConfigCli {
    #[command(subcommand)]
    command: VivaConfigCommand,
}

#[derive(Debug, Subcommand)]
enum VivaConfigCommand {
    Show {
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Set {
        #[arg(long)]
        repo: Option<String>,
        #[arg(long)]
        bin: Option<String>,
        #[arg(long)]
        build: Option<bool>,
    },
    Tunnel {
        #[command(subcommand)]
        command: VivaTunnelConfigCommand,
    },
}

#[derive(Debug, Subcommand)]
enum VivaTunnelConfigCommand {
    Show {
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Import {
        #[arg(long)]
        profile: String,
        #[arg(long)]
        file: PathBuf,
        #[arg(long, action = ArgAction::SetTrue)]
        set_default: bool,
    },
    Default {
        #[arg(long)]
        profile: String,
    },
}

#[derive(Debug, Parser)]
struct SurfWrapperCli {
    #[command(subcommand)]
    command: SurfWrapperCommand,
}

#[derive(Debug, Subcommand)]
enum SurfWrapperCommand {
    Config {
        #[command(subcommand)]
        command: SurfWrapperConfigCommand,
    },
}

#[derive(Debug, Subcommand)]
enum SurfWrapperConfigCommand {
    Show {
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Set {
        #[arg(long)]
        repo: Option<String>,
        #[arg(long)]
        bin: Option<String>,
        #[arg(long)]
        build: Option<bool>,
    },
}

#[derive(Debug, Subcommand)]
enum WarmupCommand {
    Run(WarmupRunArgs),
    #[command(name = "decision", alias = "autostartdecision", alias = "autostart-decision")]
    Decision {
        #[arg(long)]
        state_path: Option<PathBuf>,
        #[arg(long)]
        autostart_path: Option<PathBuf>,
        #[arg(long)]
        disabled_path: Option<PathBuf>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Status {
        #[arg(long)]
        path: Option<PathBuf>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    State {
        #[command(subcommand)]
        command: WarmupStateCommand,
    },
    Marker {
        #[command(subcommand)]
        command: WarmupMarkerCommand,
    },
}

#[derive(Debug, Args)]
struct WarmupRunArgs {
    profile: Option<String>,
    #[arg(long, default_value_t = false, conflicts_with = "profile")]
    all: bool,
    #[arg(long)]
    path: Option<PathBuf>,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long)]
    workspace: Option<PathBuf>,
    #[arg(long, default_value_t = CODEX_WARMUP_DEFAULT_MAX_TURNS)]
    max_turns: u32,
    #[arg(long, default_value_t = CODEX_WARMUP_DEFAULT_TURN_TIMEOUT_SECONDS)]
    turn_timeout_seconds: u64,
    #[arg(long, default_value = "json")]
    format: OutputFormat,
}

#[derive(Debug, Subcommand)]
enum WarmupStateCommand {
    Write {
        #[arg(long)]
        path: PathBuf,
        #[arg(long)]
        state_json: String,
    },
}

#[derive(Debug, Subcommand)]
enum WarmupMarkerCommand {
    Show {
        #[arg(long)]
        autostart_path: Option<PathBuf>,
        #[arg(long)]
        disabled_path: Option<PathBuf>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    #[command(name = "enable", alias = "writeautostart", alias = "write-autostart")]
    Enable {
        #[arg(long)]
        path: PathBuf,
    },
    #[command(name = "disable", alias = "setdisabled", alias = "set-disabled")]
    Disable {
        #[arg(long)]
        path: PathBuf,
        #[arg(long)]
        disabled: String,
    },
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, ValueEnum)]
enum FortRefreshOutcomeArg {
    Success,
    Unauthorized,
    Retryable,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, ValueEnum)]
enum OutputFormat {
    Text,
    Json,
}

impl fmt::Display for OutputFormat {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let value = match self {
            Self::Text => "text",
            Self::Json => "json",
        };
        f.write_str(value)
    }
}

#[derive(Debug, Deserialize)]
struct NucleusGatewayMetadata {
    ws_url: String,
}

#[derive(Debug, Serialize)]
struct NucleusGatewayRequest {
    id: Value,
    method: String,
    params: Value,
}

#[derive(Debug, Deserialize)]
struct NucleusGatewayError {
    code: String,
    message: String,
    #[allow(dead_code)]
    details: Option<Value>,
}

#[derive(Debug, Deserialize)]
struct NucleusGatewayResponse {
    id: Value,
    ok: bool,
    result: Option<Value>,
    error: Option<NucleusGatewayError>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum NucleusServicePlatform {
    SystemdUser,
    LaunchdAgent,
}

impl NucleusServicePlatform {
    fn as_str(self) -> &'static str {
        match self {
            Self::SystemdUser => "systemd-user",
            Self::LaunchdAgent => "launchd-agent",
        }
    }
}

#[derive(Debug, Serialize)]
struct NucleusServiceActionView {
    platform: String,
    action: String,
    definition_path: String,
    service_name: String,
    logs_hint: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    manager_command: Option<Vec<String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    manager_stdout: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    manager_stderr: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    state_dir: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    bind_addr: Option<String>,
}

fn default_nucleus_metadata_path() -> PathBuf {
    default_home_dir().join(".si").join("nucleus").join("gateway").join("metadata.json")
}

fn resolve_nucleus_ws_endpoint(endpoint: Option<&str>) -> Result<String> {
    if let Some(value) = endpoint.map(str::trim).filter(|value| !value.is_empty()) {
        return Ok(value.to_owned());
    }
    if let Ok(value) = env::var("SI_NUCLEUS_WS_ADDR") {
        let trimmed = value.trim();
        if !trimmed.is_empty() {
            return Ok(trimmed.to_owned());
        }
    }
    let metadata_path = default_nucleus_metadata_path();
    if metadata_path.is_file() {
        let raw = fs::read(&metadata_path)
            .with_context(|| format!("read {}", metadata_path.display()))?;
        let metadata: NucleusGatewayMetadata = serde_json::from_slice(&raw)
            .with_context(|| format!("parse {}", metadata_path.display()))?;
        let trimmed = metadata.ws_url.trim();
        if !trimmed.is_empty() {
            return Ok(trimmed.to_owned());
        }
    }
    Ok("ws://127.0.0.1:4747/ws".to_owned())
}

fn resolve_nucleus_gateway_auth_token() -> Option<String> {
    env::var("SI_NUCLEUS_AUTH_TOKEN")
        .ok()
        .map(|value| value.trim().to_owned())
        .filter(|value| !value.is_empty())
}

fn run_nucleus_gateway_request(
    endpoint: Option<&str>,
    method: &str,
    params: Value,
) -> Result<Value> {
    let endpoint = resolve_nucleus_ws_endpoint(endpoint)?;
    let request = NucleusGatewayRequest {
        id: json!(Utc::now().timestamp_millis()),
        method: method.to_owned(),
        params,
    };
    let mut websocket_request =
        endpoint.as_str().into_client_request().context("build nucleus websocket request")?;
    if let Some(token) = resolve_nucleus_gateway_auth_token() {
        let header = HeaderValue::from_str(format!("Bearer {token}").as_str())
            .context("build auth header")?;
        websocket_request.headers_mut().insert(AUTHORIZATION, header);
    }
    let (mut socket, _) = ws_connect(websocket_request)
        .with_context(|| format!("connect nucleus websocket {endpoint}"))?;
    socket
        .send(WsMessage::Text(serde_json::to_string(&request)?.into()))
        .context("send nucleus websocket request")?;

    loop {
        match socket.read().context("read nucleus websocket response")? {
            WsMessage::Text(text) => {
                let response: NucleusGatewayResponse =
                    serde_json::from_str(&text).context("parse nucleus websocket response")?;
                if !response.ok {
                    let error = response
                        .error
                        .map(|item| format!("{}: {}", item.code, item.message))
                        .unwrap_or_else(|| "unknown nucleus error".to_owned());
                    anyhow::bail!(error);
                }
                let _ = response.id;
                return Ok(response.result.unwrap_or(Value::Null));
            }
            WsMessage::Ping(payload) => {
                socket.send(WsMessage::Pong(payload)).context("send nucleus websocket pong")?;
            }
            WsMessage::Pong(_) | WsMessage::Binary(_) | WsMessage::Frame(_) => {}
            WsMessage::Close(_) => anyhow::bail!("nucleus websocket closed before response"),
        }
    }
}

fn print_nucleus_output(format: OutputFormat, payload: &Value) -> Result<()> {
    match format {
        OutputFormat::Json | OutputFormat::Text => {
            println!("{}", serde_json::to_string_pretty(payload)?);
        }
    }
    Ok(())
}

fn parse_env_assignments(entries: &[String]) -> Result<BTreeMap<String, String>> {
    let mut map = BTreeMap::new();
    for entry in entries {
        let trimmed = entry.trim();
        if trimmed.is_empty() {
            continue;
        }
        let Some((key, value)) = trimmed.split_once('=') else {
            anyhow::bail!("invalid env assignment: {trimmed}");
        };
        let key = key.trim();
        if key.is_empty() {
            anyhow::bail!("invalid env assignment: {trimmed}");
        }
        map.insert(key.to_owned(), value.trim().to_owned());
    }
    Ok(map)
}

fn validate_nucleus_profile_slug(profile: &str) -> Result<()> {
    let trimmed = profile.trim();
    let Some(first) = trimmed.chars().next() else {
        anyhow::bail!("profile name cannot be empty");
    };
    if !first.is_ascii_lowercase()
        || !trimmed
            .chars()
            .skip(1)
            .all(|ch| ch.is_ascii_lowercase() || ch.is_ascii_digit() || ch == '-')
    {
        anyhow::bail!("profile name must match ^[a-z][a-z0-9-]*$, got {trimmed}");
    }
    Ok(())
}

fn prepare_nucleus_codex_profile_runtime(
    profile: &str,
    home_dir: Option<PathBuf>,
    codex_home: Option<PathBuf>,
    env_entries: Vec<String>,
) -> Result<(PathBuf, PathBuf, BTreeMap<String, String>)> {
    validate_nucleus_profile_slug(profile)?;
    let (settings_home, settings) = load_codex_runtime_settings(home_dir.clone(), None)?;
    let paths = SiPaths::from_settings(&settings_home, &settings);
    let prepared = prepare_codex_profile_runtime(
        &settings_home,
        &paths,
        &settings,
        profile,
        DEFAULT_CODEX_WORKER_SLOT,
        codex_home,
    )?;
    let mut env = parse_env_assignments(&env_entries)?;
    insert_codex_profile_fort_env(&mut env, &prepared.fort_paths);
    Ok((home_dir.unwrap_or(settings_home), prepared.codex_home, env))
}

fn run_nucleus_status(endpoint: Option<String>, format: OutputFormat) -> Result<()> {
    let payload = run_nucleus_gateway_request(endpoint.as_deref(), "nucleus.status", json!({}))?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_profile_list(endpoint: Option<String>, format: OutputFormat) -> Result<()> {
    let payload = run_nucleus_gateway_request(endpoint.as_deref(), "profile.list", json!({}))?;
    print_nucleus_output(format, &payload)
}

fn parse_nucleus_json_arg(value: &str, flag: &str) -> Result<Value> {
    serde_json::from_str(value).with_context(|| format!("parse {flag} as JSON"))
}

fn run_nucleus_cron_list(endpoint: Option<String>, format: OutputFormat) -> Result<()> {
    let payload =
        run_nucleus_gateway_request(endpoint.as_deref(), "producer.cron.list", json!({}))?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_cron_inspect(
    endpoint: Option<String>,
    rule_name: String,
    format: OutputFormat,
) -> Result<()> {
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "producer.cron.inspect",
        json!({ "rule_name": rule_name }),
    )?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_cron_upsert(
    endpoint: Option<String>,
    name: String,
    schedule_kind: String,
    schedule: String,
    instructions: String,
    enabled: Option<bool>,
    reset: bool,
    format: OutputFormat,
) -> Result<()> {
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "producer.cron.upsert",
        json!({
            "name": name,
            "schedule_kind": schedule_kind,
            "schedule": schedule,
            "instructions": instructions,
            "enabled": enabled,
            "reset": reset,
        }),
    )?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_cron_delete(
    endpoint: Option<String>,
    rule_name: String,
    format: OutputFormat,
) -> Result<()> {
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "producer.cron.delete",
        json!({ "rule_name": rule_name }),
    )?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_hook_list(endpoint: Option<String>, format: OutputFormat) -> Result<()> {
    let payload =
        run_nucleus_gateway_request(endpoint.as_deref(), "producer.hook.list", json!({}))?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_hook_inspect(
    endpoint: Option<String>,
    rule_name: String,
    format: OutputFormat,
) -> Result<()> {
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "producer.hook.inspect",
        json!({ "rule_name": rule_name }),
    )?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_hook_upsert(
    endpoint: Option<String>,
    name: String,
    match_event_type: String,
    instructions: String,
    enabled: Option<bool>,
    format: OutputFormat,
) -> Result<()> {
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "producer.hook.upsert",
        json!({
            "name": name,
            "match_event_type": match_event_type,
            "instructions": instructions,
            "enabled": enabled,
        }),
    )?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_hook_delete(
    endpoint: Option<String>,
    rule_name: String,
    format: OutputFormat,
) -> Result<()> {
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "producer.hook.delete",
        json!({ "rule_name": rule_name }),
    )?;
    print_nucleus_output(format, &payload)
}

fn default_nucleus_state_dir() -> PathBuf {
    default_home_dir().join(".si").join("nucleus")
}

fn default_nucleus_bind_addr() -> &'static str {
    "127.0.0.1:4747"
}

fn resolve_nucleus_service_platform() -> Result<NucleusServicePlatform> {
    if let Ok(value) = env::var("SI_NUCLEUS_SERVICE_PLATFORM") {
        match value.trim() {
            "linux" | "systemd" | "systemd-user" => {
                return Ok(NucleusServicePlatform::SystemdUser);
            }
            "macos" | "darwin" | "launchd" | "launchd-agent" => {
                return Ok(NucleusServicePlatform::LaunchdAgent);
            }
            other if !other.is_empty() => {
                anyhow::bail!("unsupported nucleus service platform: {other}")
            }
            _ => {}
        }
    }
    #[cfg(target_os = "linux")]
    {
        return Ok(NucleusServicePlatform::SystemdUser);
    }
    #[cfg(target_os = "macos")]
    {
        return Ok(NucleusServicePlatform::LaunchdAgent);
    }
    #[allow(unreachable_code)]
    Err(anyhow!("nucleus service management is only implemented for Linux and macOS"))
}

fn default_nucleus_service_dir(platform: NucleusServicePlatform) -> PathBuf {
    match platform {
        NucleusServicePlatform::SystemdUser => {
            default_home_dir().join(".config").join("systemd").join("user")
        }
        NucleusServicePlatform::LaunchdAgent => {
            default_home_dir().join("Library").join("LaunchAgents")
        }
    }
}

fn nucleus_service_name(platform: NucleusServicePlatform) -> &'static str {
    match platform {
        NucleusServicePlatform::SystemdUser => "si-nucleus.service",
        NucleusServicePlatform::LaunchdAgent => "com.aureuma.si.nucleus",
    }
}

fn nucleus_service_definition_path(
    platform: NucleusServicePlatform,
    service_dir: Option<PathBuf>,
) -> PathBuf {
    let dir = service_dir.unwrap_or_else(|| default_nucleus_service_dir(platform));
    match platform {
        NucleusServicePlatform::SystemdUser => dir.join(nucleus_service_name(platform)),
        NucleusServicePlatform::LaunchdAgent => {
            dir.join(format!("{}.plist", nucleus_service_name(platform)))
        }
    }
}

fn nucleus_service_logs_hint(platform: NucleusServicePlatform) -> String {
    match platform {
        NucleusServicePlatform::SystemdUser => {
            format!("journalctl --user-unit {} -f", nucleus_service_name(platform))
        }
        NucleusServicePlatform::LaunchdAgent => {
            "log stream --style compact --predicate 'process == \"si-nucleus\" || process == \"si\"'".to_owned()
        }
    }
}

fn nucleus_service_manager_exec(platform: NucleusServicePlatform) -> String {
    match platform {
        NucleusServicePlatform::SystemdUser => env::var("SI_NUCLEUS_SYSTEMCTL_EXEC")
            .ok()
            .filter(|value| !value.trim().is_empty())
            .unwrap_or_else(|| "systemctl".to_owned()),
        NucleusServicePlatform::LaunchdAgent => env::var("SI_NUCLEUS_LAUNCHCTL_EXEC")
            .ok()
            .filter(|value| !value.trim().is_empty())
            .unwrap_or_else(|| "launchctl".to_owned()),
    }
}

fn nucleus_service_program_args(
    si_binary: &Path,
    state_dir: &Path,
    bind_addr: &str,
) -> Vec<String> {
    vec![
        si_binary.display().to_string(),
        "nucleus".to_owned(),
        "service".to_owned(),
        "run".to_owned(),
        "--state-dir".to_owned(),
        state_dir.display().to_string(),
        "--bind-addr".to_owned(),
        bind_addr.to_owned(),
    ]
}

fn systemd_quote_arg(arg: &str) -> String {
    let escaped = arg.replace('\\', "\\\\").replace('"', "\\\"");
    format!("\"{escaped}\"")
}

fn xml_escape(value: &str) -> String {
    value
        .replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
        .replace('"', "&quot;")
        .replace('\'', "&apos;")
}

fn render_nucleus_systemd_unit(
    si_binary: &Path,
    state_dir: &Path,
    bind_addr: &str,
    env_vars: &BTreeMap<String, String>,
) -> String {
    let exec_start = nucleus_service_program_args(si_binary, state_dir, bind_addr)
        .into_iter()
        .map(|arg| systemd_quote_arg(&arg))
        .collect::<Vec<_>>()
        .join(" ");
    let environment = env_vars
        .iter()
        .map(|(key, value)| {
            format!("Environment={}\n", systemd_quote_arg(&format!("{key}={value}")))
        })
        .collect::<String>();
    format!(
        "[Unit]\nDescription=SI Nucleus\nAfter=default.target\n\n[Service]\nType=simple\n{environment}ExecStart={exec_start}\nRestart=on-failure\nRestartSec=5\n\n[Install]\nWantedBy=default.target\n"
    )
}

fn render_nucleus_launchd_plist(
    si_binary: &Path,
    state_dir: &Path,
    bind_addr: &str,
    env_vars: &BTreeMap<String, String>,
) -> String {
    let program_arguments = nucleus_service_program_args(si_binary, state_dir, bind_addr)
        .into_iter()
        .map(|arg| format!("    <string>{}</string>\n", xml_escape(&arg)))
        .collect::<String>();
    let environment_variables = if env_vars.is_empty() {
        String::new()
    } else {
        let entries = env_vars
            .iter()
            .map(|(key, value)| {
                format!(
                    "    <key>{}</key>\n    <string>{}</string>\n",
                    xml_escape(key),
                    xml_escape(value)
                )
            })
            .collect::<String>();
        format!("  <key>EnvironmentVariables</key>\n  <dict>\n{entries}  </dict>\n")
    };
    format!(
        "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\">\n<dict>\n  <key>Label</key>\n  <string>{}</string>\n  <key>ProgramArguments</key>\n  <array>\n{}  </array>\n{}  <key>RunAtLoad</key>\n  <true/>\n  <key>KeepAlive</key>\n  <dict>\n    <key>SuccessfulExit</key>\n    <false/>\n  </dict>\n</dict>\n</plist>\n",
        nucleus_service_name(NucleusServicePlatform::LaunchdAgent),
        program_arguments,
        environment_variables
    )
}

fn default_nucleus_service_path() -> &'static str {
    if cfg!(target_os = "macos") {
        "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
    } else {
        "/usr/local/bin:/usr/bin:/bin"
    }
}

fn unstable_service_path_entry(path: &Path) -> bool {
    let value = path.to_string_lossy();
    value.contains("/tmp/arg0/") || value.contains("\\tmp\\arg0\\")
}

fn sanitized_nucleus_service_path() -> String {
    let raw_path = env::var_os("PATH").unwrap_or_default();
    let mut sanitized = Vec::new();
    for dir in env::split_paths(&raw_path) {
        if dir.as_os_str().is_empty() || !dir.is_dir() || unstable_service_path_entry(&dir) {
            continue;
        }
        if !sanitized.iter().any(|existing| existing == &dir) {
            sanitized.push(dir);
        }
    }
    if sanitized.is_empty() {
        return default_nucleus_service_path().to_owned();
    }
    env::join_paths(sanitized)
        .ok()
        .and_then(|joined| {
            let value = joined.to_string_lossy().trim().to_owned();
            (!value.is_empty()).then_some(value)
        })
        .unwrap_or_else(|| default_nucleus_service_path().to_owned())
}

fn find_executable_in_path(program: &str) -> Option<PathBuf> {
    if program.trim().is_empty() {
        return None;
    }
    let candidate = PathBuf::from(program);
    if candidate.components().count() > 1 {
        return candidate.is_file().then_some(candidate);
    }
    let path = env::var_os("PATH")?;
    for dir in env::split_paths(&path) {
        let full = dir.join(program);
        if full.is_file() {
            return Some(full);
        }
    }
    None
}

fn unstable_service_binary_path(path: &Path) -> bool {
    path.components().any(|component| {
        let value = component.as_os_str().to_string_lossy();
        value == "target" || value == ".artifacts"
    })
}

fn resolve_nucleus_service_launcher() -> Result<PathBuf> {
    if let Ok(value) = env::var("SI_NUCLEUS_SERVICE_SI_BIN") {
        let trimmed = value.trim();
        if !trimmed.is_empty() {
            return Ok(PathBuf::from(trimmed));
        }
    }
    let current = env::current_exe().context("resolve current si executable")?;
    if unstable_service_binary_path(&current)
        && let Some(path_binary) = find_executable_in_path("si")
        && path_binary != current
    {
        return Ok(path_binary);
    }
    Ok(current)
}

fn nucleus_service_environment(
    state_dir: &Path,
    bind_addr: &str,
    nucleus_bin: Option<&Path>,
) -> BTreeMap<String, String> {
    let mut env_vars = BTreeMap::new();
    env_vars.insert("PATH".to_owned(), sanitized_nucleus_service_path());
    env_vars.insert("SI_NUCLEUS_STATE_DIR".to_owned(), state_dir.display().to_string());
    env_vars.insert("SI_NUCLEUS_BIND_ADDR".to_owned(), bind_addr.to_owned());
    if let Some(nucleus_bin) = nucleus_bin {
        env_vars.insert("SI_NUCLEUS_BIN".to_owned(), nucleus_bin.display().to_string());
    }
    for key in ["SI_NUCLEUS_AUTH_TOKEN", "SI_NUCLEUS_PUBLIC_URL"] {
        if let Ok(value) = env::var(key) {
            let value = value.trim().to_owned();
            if !value.is_empty() {
                env_vars.insert(key.to_owned(), value);
            }
        }
    }
    env_vars
}

fn launchd_domain_target() -> String {
    format!("gui/{}", unsafe { libc::geteuid() })
}

fn run_nucleus_service_manager_command(
    platform: NucleusServicePlatform,
    args: &[String],
    capture_output: bool,
) -> Result<(Vec<String>, Option<String>, Option<String>)> {
    let program = nucleus_service_manager_exec(platform);
    let mut command = StdCommand::new(&program);
    command.args(args);
    if capture_output {
        let output =
            command.output().with_context(|| format!("run {program} {}", args.join(" ")))?;
        if !output.status.success() {
            anyhow::bail!(
                "{}",
                String::from_utf8_lossy(&output.stderr).trim().to_owned().if_empty_then(
                    || format!("{program} {} failed with {}", args.join(" "), output.status)
                )
            );
        }
        let stdout = String::from_utf8_lossy(&output.stdout).trim().to_owned();
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_owned();
        return Ok((
            std::iter::once(program).chain(args.iter().cloned()).collect(),
            (!stdout.is_empty()).then_some(stdout),
            (!stderr.is_empty()).then_some(stderr),
        ));
    }
    let status = command.status().with_context(|| format!("run {program} {}", args.join(" ")))?;
    if !status.success() {
        anyhow::bail!("{program} {} failed with {}", args.join(" "), status);
    }
    Ok((std::iter::once(program).chain(args.iter().cloned()).collect(), None, None))
}

trait IfEmptyThen {
    fn if_empty_then(self, fallback: impl FnOnce() -> String) -> String;
}

impl IfEmptyThen for String {
    fn if_empty_then(self, fallback: impl FnOnce() -> String) -> String {
        if self.trim().is_empty() { fallback() } else { self }
    }
}

fn print_nucleus_service_view(format: OutputFormat, view: &NucleusServiceActionView) -> Result<()> {
    print_nucleus_output(format, &serde_json::to_value(view)?)
}

fn resolve_nucleus_service_state_dir(state_dir: Option<PathBuf>) -> PathBuf {
    state_dir
        .or_else(|| {
            env::var("SI_NUCLEUS_STATE_DIR")
                .ok()
                .map(|value| value.trim().to_owned())
                .filter(|value| !value.is_empty())
                .map(PathBuf::from)
        })
        .unwrap_or_else(default_nucleus_state_dir)
}

fn resolve_nucleus_service_bind_addr(bind_addr: Option<String>) -> String {
    bind_addr
        .or_else(|| {
            env::var("SI_NUCLEUS_BIND_ADDR")
                .ok()
                .map(|value| value.trim().to_owned())
                .filter(|value| !value.is_empty())
        })
        .unwrap_or_else(|| default_nucleus_bind_addr().to_owned())
}

fn run_nucleus_service_install(
    state_dir: Option<PathBuf>,
    bind_addr: Option<String>,
    service_dir: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let platform = resolve_nucleus_service_platform()?;
    let state_dir = resolve_nucleus_service_state_dir(state_dir);
    let bind_addr = resolve_nucleus_service_bind_addr(bind_addr);
    let nucleus_bin = resolve_nucleus_service_binary(None).ok();
    let service_env = nucleus_service_environment(&state_dir, &bind_addr, nucleus_bin.as_deref());
    let definition_path = nucleus_service_definition_path(platform, service_dir);
    let si_binary = resolve_nucleus_service_launcher()?;
    let definition = match platform {
        NucleusServicePlatform::SystemdUser => {
            render_nucleus_systemd_unit(&si_binary, &state_dir, &bind_addr, &service_env)
        }
        NucleusServicePlatform::LaunchdAgent => {
            render_nucleus_launchd_plist(&si_binary, &state_dir, &bind_addr, &service_env)
        }
    };
    let parent = definition_path
        .parent()
        .ok_or_else(|| anyhow!("missing parent for {}", definition_path.display()))?;
    fs::create_dir_all(parent).with_context(|| format!("create {}", parent.display()))?;
    fs::write(&definition_path, definition)
        .with_context(|| format!("write {}", definition_path.display()))?;

    let manager_command = if platform == NucleusServicePlatform::SystemdUser {
        Some(
            run_nucleus_service_manager_command(
                platform,
                &["--user".to_owned(), "daemon-reload".to_owned()],
                false,
            )?
            .0,
        )
    } else {
        None
    };

    print_nucleus_service_view(
        format,
        &NucleusServiceActionView {
            platform: platform.as_str().to_owned(),
            action: "install".to_owned(),
            definition_path: definition_path.display().to_string(),
            service_name: nucleus_service_name(platform).to_owned(),
            logs_hint: nucleus_service_logs_hint(platform),
            manager_command,
            manager_stdout: None,
            manager_stderr: None,
            state_dir: Some(state_dir.display().to_string()),
            bind_addr: Some(bind_addr),
        },
    )
}

fn run_nucleus_service_uninstall(service_dir: Option<PathBuf>, format: OutputFormat) -> Result<()> {
    let platform = resolve_nucleus_service_platform()?;
    let definition_path = nucleus_service_definition_path(platform, service_dir);
    if definition_path.exists() {
        fs::remove_file(&definition_path)
            .with_context(|| format!("remove {}", definition_path.display()))?;
    }
    let manager_command = if platform == NucleusServicePlatform::SystemdUser {
        Some(
            run_nucleus_service_manager_command(
                platform,
                &["--user".to_owned(), "daemon-reload".to_owned()],
                false,
            )?
            .0,
        )
    } else {
        None
    };
    print_nucleus_service_view(
        format,
        &NucleusServiceActionView {
            platform: platform.as_str().to_owned(),
            action: "uninstall".to_owned(),
            definition_path: definition_path.display().to_string(),
            service_name: nucleus_service_name(platform).to_owned(),
            logs_hint: nucleus_service_logs_hint(platform),
            manager_command,
            manager_stdout: None,
            manager_stderr: None,
            state_dir: None,
            bind_addr: None,
        },
    )
}

fn run_nucleus_service_action(action: &str, format: OutputFormat) -> Result<()> {
    let platform = resolve_nucleus_service_platform()?;
    let definition_path = nucleus_service_definition_path(platform, None);
    let args = match (platform, action) {
        (NucleusServicePlatform::SystemdUser, "start") => {
            vec!["--user".to_owned(), "start".to_owned(), nucleus_service_name(platform).to_owned()]
        }
        (NucleusServicePlatform::SystemdUser, "stop") => {
            vec!["--user".to_owned(), "stop".to_owned(), nucleus_service_name(platform).to_owned()]
        }
        (NucleusServicePlatform::SystemdUser, "restart") => vec![
            "--user".to_owned(),
            "restart".to_owned(),
            nucleus_service_name(platform).to_owned(),
        ],
        (NucleusServicePlatform::LaunchdAgent, "start") => vec![
            "bootstrap".to_owned(),
            launchd_domain_target(),
            definition_path.display().to_string(),
        ],
        (NucleusServicePlatform::LaunchdAgent, "stop") => vec![
            "bootout".to_owned(),
            format!("{}/{}", launchd_domain_target(), nucleus_service_name(platform)),
        ],
        (NucleusServicePlatform::LaunchdAgent, "restart") => vec![
            "kickstart".to_owned(),
            "-k".to_owned(),
            format!("{}/{}", launchd_domain_target(), nucleus_service_name(platform)),
        ],
        _ => anyhow::bail!("unsupported nucleus service action: {action}"),
    };
    let (manager_command, manager_stdout, manager_stderr) =
        run_nucleus_service_manager_command(platform, &args, false)?;
    print_nucleus_service_view(
        format,
        &NucleusServiceActionView {
            platform: platform.as_str().to_owned(),
            action: action.to_owned(),
            definition_path: definition_path.display().to_string(),
            service_name: nucleus_service_name(platform).to_owned(),
            logs_hint: nucleus_service_logs_hint(platform),
            manager_command: Some(manager_command),
            manager_stdout,
            manager_stderr,
            state_dir: None,
            bind_addr: None,
        },
    )
}

fn run_nucleus_service_status(format: OutputFormat) -> Result<()> {
    let platform = resolve_nucleus_service_platform()?;
    let definition_path = nucleus_service_definition_path(platform, None);
    let args = match platform {
        NucleusServicePlatform::SystemdUser => vec![
            "--user".to_owned(),
            "status".to_owned(),
            "--no-pager".to_owned(),
            nucleus_service_name(platform).to_owned(),
        ],
        NucleusServicePlatform::LaunchdAgent => vec![
            "print".to_owned(),
            format!("{}/{}", launchd_domain_target(), nucleus_service_name(platform)),
        ],
    };
    let (manager_command, manager_stdout, manager_stderr) =
        run_nucleus_service_manager_command(platform, &args, true)?;
    print_nucleus_service_view(
        format,
        &NucleusServiceActionView {
            platform: platform.as_str().to_owned(),
            action: "status".to_owned(),
            definition_path: definition_path.display().to_string(),
            service_name: nucleus_service_name(platform).to_owned(),
            logs_hint: nucleus_service_logs_hint(platform),
            manager_command: Some(manager_command),
            manager_stdout,
            manager_stderr,
            state_dir: None,
            bind_addr: None,
        },
    )
}

fn resolve_nucleus_service_binary(explicit: Option<PathBuf>) -> Result<PathBuf> {
    if let Some(path) = explicit {
        return Ok(path);
    }
    if let Ok(value) = env::var("SI_NUCLEUS_BIN") {
        let trimmed = value.trim();
        if !trimmed.is_empty() {
            return Ok(PathBuf::from(trimmed));
        }
    }
    let current = env::current_exe().context("resolve current si executable")?;
    if let Some(parent) = current.parent() {
        let sibling = parent.join("si-nucleus");
        if sibling.is_file() {
            return Ok(sibling);
        }
        for candidate in [
            parent.join("cargo-target-nucleus-public").join("release").join("si-nucleus"),
            parent.join("cargo-target").join("release").join("si-nucleus"),
        ] {
            if candidate.is_file() {
                return Ok(candidate);
            }
        }
    }
    if let Ok(current_dir) = env::current_dir() {
        for ancestor in current_dir.ancestors() {
            let candidate = ancestor
                .join(".artifacts")
                .join("cargo-target-nucleus-public")
                .join("release")
                .join("si-nucleus");
            if candidate.is_file() {
                return Ok(candidate);
            }
            let candidate =
                ancestor.join(".artifacts").join("cargo-target").join("release").join("si-nucleus");
            if candidate.is_file() {
                return Ok(candidate);
            }
        }
    }
    if let Some(path_binary) = find_executable_in_path("si-nucleus") {
        return Ok(path_binary);
    }
    Ok(PathBuf::from("si-nucleus"))
}

fn run_nucleus_service_run(
    state_dir: Option<PathBuf>,
    bind_addr: Option<String>,
    nucleus_bin: Option<PathBuf>,
) -> Result<()> {
    let nucleus_bin = resolve_nucleus_service_binary(nucleus_bin)?;
    let mut command = StdCommand::new(&nucleus_bin);
    if let Some(state_dir) = state_dir {
        command.env("SI_NUCLEUS_STATE_DIR", state_dir);
    }
    if let Some(bind_addr) = bind_addr {
        command.env("SI_NUCLEUS_BIND_ADDR", bind_addr);
    }
    let status = command
        .status()
        .with_context(|| format!("run nucleus service binary {}", nucleus_bin.display()))?;
    if !status.success() {
        anyhow::bail!("nucleus service binary exited with {status}");
    }
    Ok(())
}

fn run_nucleus_task_create(
    endpoint: Option<String>,
    title: String,
    instructions: String,
    profile: Option<String>,
    requires_fort: Option<bool>,
    format: OutputFormat,
) -> Result<()> {
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "task.create",
        json!({
            "title": title,
            "instructions": instructions,
            "profile": profile,
            "requires_fort": requires_fort,
        }),
    )?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_task_list(endpoint: Option<String>, format: OutputFormat) -> Result<()> {
    let payload = run_nucleus_gateway_request(endpoint.as_deref(), "task.list", json!({}))?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_task_inspect(
    endpoint: Option<String>,
    task_id: String,
    format: OutputFormat,
) -> Result<()> {
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "task.inspect",
        json!({ "task_id": task_id }),
    )?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_task_cancel(
    endpoint: Option<String>,
    task_id: String,
    format: OutputFormat,
) -> Result<()> {
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "task.cancel",
        json!({ "task_id": task_id }),
    )?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_task_prune(
    endpoint: Option<String>,
    older_than_days: u64,
    format: OutputFormat,
) -> Result<()> {
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "task.prune",
        json!({ "older_than_days": older_than_days }),
    )?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_worker_probe(
    endpoint: Option<String>,
    profile: String,
    worker_id: Option<String>,
    home_dir: Option<PathBuf>,
    codex_home: Option<PathBuf>,
    workdir: Option<PathBuf>,
    env_entries: Vec<String>,
    format: OutputFormat,
) -> Result<()> {
    let (home_dir, codex_home, env) =
        prepare_nucleus_codex_profile_runtime(&profile, home_dir, codex_home, env_entries)?;
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "worker.probe",
        json!({
            "profile": profile,
            "worker_id": worker_id,
            "home_dir": home_dir,
            "codex_home": codex_home,
            "workdir": workdir,
            "env": env,
        }),
    )?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_worker_list(endpoint: Option<String>, format: OutputFormat) -> Result<()> {
    let payload = run_nucleus_gateway_request(endpoint.as_deref(), "worker.list", json!({}))?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_worker_inspect(
    endpoint: Option<String>,
    worker_id: String,
    format: OutputFormat,
) -> Result<()> {
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "worker.inspect",
        json!({ "worker_id": worker_id }),
    )?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_worker_restart(
    endpoint: Option<String>,
    worker_id: String,
    format: OutputFormat,
) -> Result<()> {
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "worker.restart",
        json!({ "worker_id": worker_id }),
    )?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_worker_repair_auth(
    endpoint: Option<String>,
    worker_id: String,
    format: OutputFormat,
) -> Result<()> {
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "worker.repair_auth",
        json!({ "worker_id": worker_id }),
    )?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_session_create(
    endpoint: Option<String>,
    profile: String,
    worker_id: Option<String>,
    thread_id: Option<String>,
    home_dir: Option<PathBuf>,
    codex_home: Option<PathBuf>,
    workdir: Option<PathBuf>,
    env_entries: Vec<String>,
    format: OutputFormat,
) -> Result<()> {
    let (home_dir, codex_home, env) =
        prepare_nucleus_codex_profile_runtime(&profile, home_dir, codex_home, env_entries)?;
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "session.create",
        json!({
            "profile": profile,
            "worker_id": worker_id,
            "thread_id": thread_id,
            "home_dir": home_dir,
            "codex_home": codex_home,
            "workdir": workdir,
            "env": env,
        }),
    )?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_session_list(endpoint: Option<String>, format: OutputFormat) -> Result<()> {
    let payload = run_nucleus_gateway_request(endpoint.as_deref(), "session.list", json!({}))?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_session_show(
    endpoint: Option<String>,
    session_id: String,
    format: OutputFormat,
) -> Result<()> {
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "session.show",
        json!({ "session_id": session_id }),
    )?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_run_submit_turn(
    endpoint: Option<String>,
    session_id: String,
    prompt: String,
    task_id: String,
    format: OutputFormat,
) -> Result<()> {
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "run.submit_turn",
        json!({
            "session_id": session_id,
            "prompt": prompt,
            "task_id": task_id,
        }),
    )?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_run_inspect(
    endpoint: Option<String>,
    run_id: String,
    format: OutputFormat,
) -> Result<()> {
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "run.inspect",
        json!({ "run_id": run_id }),
    )?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_run_cancel(
    endpoint: Option<String>,
    run_id: String,
    format: OutputFormat,
) -> Result<()> {
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "run.cancel",
        json!({ "run_id": run_id }),
    )?;
    print_nucleus_output(format, &payload)
}

fn run_nucleus_events_subscribe(
    endpoint: Option<String>,
    count: Option<usize>,
    format: OutputFormat,
) -> Result<()> {
    let endpoint = resolve_nucleus_ws_endpoint(endpoint.as_deref())?;
    let request = NucleusGatewayRequest {
        id: json!(Utc::now().timestamp_millis()),
        method: "events.subscribe".to_owned(),
        params: json!({}),
    };
    let mut websocket_request =
        endpoint.as_str().into_client_request().context("build nucleus websocket request")?;
    if let Some(token) = resolve_nucleus_gateway_auth_token() {
        let header = HeaderValue::from_str(format!("Bearer {token}").as_str())
            .context("build auth header")?;
        websocket_request.headers_mut().insert(AUTHORIZATION, header);
    }
    let (mut socket, _) = ws_connect(websocket_request)
        .with_context(|| format!("connect nucleus websocket {endpoint}"))?;
    socket
        .send(WsMessage::Text(serde_json::to_string(&request)?.into()))
        .context("send nucleus websocket request")?;

    let mut subscribed = false;
    let mut seen = 0usize;
    loop {
        match socket.read().context("read nucleus websocket response")? {
            WsMessage::Text(text) => {
                if !subscribed {
                    let response: NucleusGatewayResponse =
                        serde_json::from_str(&text).context("parse nucleus websocket response")?;
                    if !response.ok {
                        let error = response
                            .error
                            .map(|item| format!("{}: {}", item.code, item.message))
                            .unwrap_or_else(|| "unknown nucleus error".to_owned());
                        anyhow::bail!(error);
                    }
                    subscribed = true;
                    if count == Some(0) {
                        return Ok(());
                    }
                    continue;
                }
                let payload: Value =
                    serde_json::from_str(&text).context("parse nucleus websocket event")?;
                print_nucleus_output(format, &payload)?;
                seen += 1;
                if count.is_some_and(|limit| seen >= limit) {
                    return Ok(());
                }
            }
            WsMessage::Ping(payload) => {
                socket.send(WsMessage::Pong(payload)).context("send nucleus websocket pong")?;
            }
            WsMessage::Pong(_) | WsMessage::Binary(_) | WsMessage::Frame(_) => {}
            WsMessage::Close(_) => {
                if subscribed {
                    return Ok(());
                }
                anyhow::bail!("nucleus websocket closed before subscribe acknowledgement");
            }
        }
    }
}

fn run_nucleus_events_ingest(
    endpoint: Option<String>,
    event_type: String,
    source: String,
    profile: Option<String>,
    payload: String,
    format: OutputFormat,
) -> Result<()> {
    let payload = run_nucleus_gateway_request(
        endpoint.as_deref(),
        "events.ingest",
        json!({
            "type": event_type,
            "source": source,
            "profile": profile,
            "payload": parse_nucleus_json_arg(&payload, "--payload")?,
        }),
    )?;
    print_nucleus_output(format, &payload)
}

#[derive(Debug, Serialize)]
struct FortSessionClassificationView {
    state: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    reason: Option<String>,
}

#[derive(Debug, Serialize)]
struct FortSessionTransitionView {
    state: PersistedSessionState,
    classification: FortSessionClassificationView,
}

#[derive(Debug, Serialize)]
struct HelpView {
    commands: Vec<CommandView>,
}

#[derive(Debug, Serialize)]
struct CommandView {
    name: String,
    aliases: Vec<String>,
    category: CommandCategory,
    summary: String,
}

#[derive(Clone, Debug, Serialize)]
struct CodexProfileView {
    profile: String,
    active: bool,
    state: String,
    name: Option<String>,
    email: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    account_plan: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    five_hour_left_pct: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    five_hour_reset: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    five_hour_remaining_minutes: Option<i32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    weekly_left_pct: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    weekly_reset: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    weekly_remaining_minutes: Option<i32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    quota_sampled_at: Option<DateTime<Utc>>,
    auth_path: Option<String>,
    auth_updated: Option<String>,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
struct CodexWorkerState {
    #[serde(default = "default_codex_worker_state_schema_version")]
    schema_version: u32,
    profile_id: String,
    #[serde(default = "default_codex_worker_slot_name")]
    worker_slot: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    profile_name: Option<String>,
    session_name: String,
    workspace: String,
    workdir: String,
    updated_at: String,
}

#[derive(Debug, Serialize)]
struct CodexRemoveResultView {
    name: String,
    session_name: String,
    profile_id: Option<String>,
    output: String,
}

#[derive(Debug, Serialize)]
struct CodexStopResultView {
    name: String,
    session_name: String,
    profile_id: String,
    output: String,
}

#[derive(Debug, Serialize)]
struct CodexListEntryView {
    profile_id: String,
    worker_slot: String,
    session_name: String,
    tmux_window_name: String,
    state: String,
    workspace: String,
    workdir: String,
}

#[derive(Debug, Serialize)]
struct CodexRemoveAllResultView {
    aborted: bool,
    removed: Vec<CodexRemoveResultView>,
}

#[derive(Debug, Serialize)]
struct CodexStopAllResultView {
    aborted: bool,
    stopped: Vec<CodexStopResultView>,
}

#[derive(Debug, Serialize)]
struct CodexRepairAuthResultView {
    profile_id: String,
    worker_slot: String,
    agent_id: String,
    status: String,
    detail: String,
}

#[derive(Debug, Serialize)]
struct CodexRepairAuthAllResultView {
    repaired: Vec<CodexRepairAuthResultView>,
}

#[derive(Debug, Deserialize, Serialize)]
struct CodexStatusView {
    #[serde(skip_serializing_if = "Option::is_none")]
    source: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    raw: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    model: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    reasoning_effort: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    account_email: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    account_plan: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    five_hour_left_pct: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    five_hour_reset: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    five_hour_remaining_minutes: Option<i32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    weekly_left_pct: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    weekly_reset: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    weekly_remaining_minutes: Option<i32>,
}

#[derive(Debug, Serialize)]
struct CodexTmuxCommandView {
    profile_id: String,
    worker_slot: String,
    session_name: String,
    window_name: String,
    launch_command: String,
    workspace: String,
}

#[derive(Debug, Serialize)]
struct CodexWarmupRunView {
    updated_at: String,
    state_path: String,
    profiles: Vec<CodexWarmupRunProfileView>,
}

#[derive(Debug, Serialize)]
struct CodexWarmupRunProfileView {
    profile_id: String,
    action: String,
    result: String,
    turn_count: u32,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    account_email: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    account_plan: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    five_hour_left_pct: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    five_hour_reset: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    weekly_left_pct: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    weekly_used_pct: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    weekly_reset: Option<String>,
}

#[derive(Debug, Serialize)]
struct VaultTrustLookupView {
    found: bool,
    matches: bool,
    repo_root: String,
    file: String,
    expected_fingerprint: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    stored_fingerprint: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    trusted_at: Option<String>,
}

#[derive(Clone, Copy, Debug)]
struct ReleaseTarget {
    id: &'static str,
    os_label: &'static str,
    arch_label: &'static str,
    rust_triple: &'static str,
}

const SUPPORTED_RELEASE_TARGETS: &[ReleaseTarget] = &[
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

fn release_target_by_id(raw: &str) -> Result<ReleaseTarget> {
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

fn current_host_release_target() -> Result<ReleaseTarget> {
    let id = match (std::env::consts::OS, std::env::consts::ARCH) {
        ("linux", "x86_64") => "linux-amd64",
        ("linux", "aarch64") => "linux-arm64",
        ("macos", "x86_64") => "darwin-amd64",
        ("macos", "aarch64") => "darwin-arm64",
        (os, arch) => anyhow::bail!("unsupported host release target {os}/{arch}"),
    };
    release_target_by_id(id)
}

fn release_asset_name(version: &str, target: ReleaseTarget) -> String {
    let version_no_v = version.trim_start_matches('v');
    format!("si_{version_no_v}_{}_{}.tar.gz", target.os_label, target.arch_label)
}

fn run_build_self_release_assets(
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

fn run_build_self_release_asset(
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
struct SelfBuildTarget {
    target: PathBuf,
    upgrade: bool,
}

fn resolve_self_repo_root(repo: Option<PathBuf>) -> Result<PathBuf> {
    resolve_release_repo_root(repo)
}

fn resolve_self_absolute_path(path: &Path) -> Result<PathBuf> {
    if path.is_absolute() {
        Ok(path.to_path_buf())
    } else {
        Ok(std::env::current_dir().context("read current dir")?.join(path))
    }
}

fn resolve_self_install_path(path: Option<PathBuf>) -> Result<PathBuf> {
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
    if let Ok(exe) = std::env::current_exe() {
        if !exe.as_os_str().is_empty() {
            return Ok(exe);
        }
    }
    let home = std::env::var("HOME")
        .map(PathBuf::from)
        .map_err(|_| anyhow!("cannot determine install path; set --install-path"))?;
    Ok(home.join(".local").join("bin").join("si"))
}

struct PreparedCargoTargetDir {
    path: PathBuf,
    _temp: Option<tempfile::TempDir>,
}

#[derive(Debug, Serialize)]
struct DistributionDoctorView {
    version: String,
    binary: String,
    checks: Vec<DistributionDoctorCheck>,
}

#[derive(Debug, Serialize)]
struct DistributionDoctorCheck {
    name: String,
    required_for: String,
    ok: bool,
    path: String,
    detail: String,
}

fn distribution_doctor_check(name: &str, required_for: &str) -> DistributionDoctorCheck {
    match resolve_program_on_path(name) {
        Some(path) => DistributionDoctorCheck {
            name: name.to_owned(),
            required_for: required_for.to_owned(),
            ok: true,
            path: path.display().to_string(),
            detail: "found on PATH".to_owned(),
        },
        None => DistributionDoctorCheck {
            name: name.to_owned(),
            required_for: required_for.to_owned(),
            ok: false,
            path: String::new(),
            detail: "missing from PATH".to_owned(),
        },
    }
}

fn run_distribution_doctor(format: OutputFormat) -> Result<()> {
    let binary =
        env::current_exe().map(|path| path.display().to_string()).unwrap_or_else(|_| String::new());
    let mut checks = vec![
        distribution_doctor_check("codex", "Nucleus worker runtime"),
        distribution_doctor_check("tmux", "operator Codex sessions"),
        distribution_doctor_check("tar", "npm launcher archive extraction"),
        distribution_doctor_check("git", "source install and repo workflows"),
        distribution_doctor_check("cargo", "source install"),
        distribution_doctor_check("rustc", "source install"),
    ];
    if cfg!(target_os = "macos") {
        checks.push(distribution_doctor_check("brew", "Homebrew install validation"));
    }
    let view = DistributionDoctorView {
        version: si_core::version::current_version().to_owned(),
        binary,
        checks,
    };
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            println!("SI distribution doctor");
            print_cli_kv("version", &view.version);
            print_cli_kv("binary", &view.binary);
            for check in &view.checks {
                let state = if check.ok { "OK" } else { "MISS" };
                let detail = if check.path.is_empty() { &check.detail } else { &check.path };
                println!("  {state:<4} {:<8} {:<34} {detail}", check.name, check.required_for);
            }
        }
    }
    Ok(())
}

fn resolve_program_on_path(name: &str) -> Option<PathBuf> {
    let path_env = std::env::var_os("PATH")?;
    for dir in std::env::split_paths(&path_env) {
        let candidate = dir.join(name);
        if candidate.is_file() {
            return Some(candidate);
        }
    }
    None
}

fn ensure_directory_is_writable(path: &Path) -> Result<()> {
    fs::create_dir_all(path).with_context(|| format!("create {}", path.display()))?;
    let probe = path.join(format!(".si-write-probe-{}", std::process::id()));
    fs::write(&probe, "").with_context(|| format!("write {}", probe.display()))?;
    fs::remove_file(&probe).with_context(|| format!("remove {}", probe.display()))?;
    Ok(())
}

fn prepare_self_cargo_target_dir(
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

fn configure_self_cargo_command(
    command: &mut StdCommand,
    repo_root: &Path,
    cargo_target_dir: &Path,
) {
    command.current_dir(repo_root).env("CARGO_TARGET_DIR", cargo_target_dir);
    if std::env::var_os("RUSTC_WRAPPER").is_none() {
        if let Some(sccache) = resolve_program_on_path("sccache") {
            command.env("RUSTC_WRAPPER", sccache);
        }
    }
}

fn print_self_build_runtime_notes(cargo_target_dir: &Path, timings: bool, quiet: bool) {
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

fn resolve_self_build_target(
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

fn build_self_binary(
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

fn run_build_self_build(
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

fn run_build_self_upgrade(
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

fn run_build_self_check(
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

fn run_build_self_run(
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

fn resolve_release_repo_root(repo: Option<PathBuf>) -> Result<PathBuf> {
    let start = match repo {
        Some(path) if path.is_absolute() => path,
        Some(path) => std::env::current_dir().context("read current dir")?.join(path),
        None => std::env::current_dir().context("read current dir")?,
    };
    git_repo_root_from(&start)
        .with_context(|| format!("resolve repo root from {}", start.display()))
}

fn resolve_release_version(repo_root: &Path, version: Option<String>) -> Result<String> {
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

fn resolve_release_output_dir(out_dir: Option<PathBuf>, repo_root: &Path) -> Result<PathBuf> {
    match out_dir {
        Some(path) if path.is_absolute() => Ok(path),
        Some(path) => Ok(std::env::current_dir().context("read current dir")?.join(path)),
        None => Ok(repo_root.join("dist")),
    }
}

fn read_si_version(repo_root: &Path) -> Result<String> {
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

fn build_release_asset(
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

fn sha256_file(path: &Path) -> Result<String> {
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

fn run_build_npm_package(
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

fn run_publish_npm_package(
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

fn build_npm_package(repo_root: &Path, version: &str, out_dir: &Path) -> Result<PathBuf> {
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

fn ensure_command_exists(command: &str) -> Result<()> {
    match StdCommand::new(command).arg("--version").output() {
        Ok(_) => Ok(()),
        Err(err) if err.kind() == io::ErrorKind::NotFound => {
            Err(anyhow!("missing required command: {command}"))
        }
        Err(err) => Err(err).with_context(|| format!("check command {command}")),
    }
}

fn copy_dir_recursive(src: &Path, dst: &Path) -> Result<()> {
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

fn write_staged_npm_package_json(
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

fn move_file(src: &Path, dst: &Path) -> Result<()> {
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

fn npm_package_exists(package_version: &str) -> Result<bool> {
    let output = StdCommand::new("npm")
        .arg("view")
        .arg(package_version)
        .arg("version")
        .output()
        .with_context(|| format!("run corepack pnpm view for {package_version}"))?;
    Ok(output.status.success() && !String::from_utf8_lossy(&output.stdout).trim().is_empty())
}

fn wait_for_npm_package(
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
struct InstallerRunConfig {
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

fn run_installer(cfg: InstallerRunConfig) -> Result<()> {
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

fn validate_installer_config(cfg: &InstallerRunConfig) -> Result<()> {
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

fn resolve_installer_install_path(cfg: &InstallerRunConfig) -> Result<PathBuf> {
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

fn is_effective_root() -> bool {
    #[cfg(unix)]
    {
        unsafe { libc::geteuid() == 0 }
    }
    #[cfg(not(unix))]
    {
        false
    }
}

fn resolve_installer_source_dir(
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

fn validate_installer_source_dir(path: &Path) -> Result<()> {
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

fn ensure_installer_cargo_toolchain(toolchain_mode: &str) -> Result<String> {
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

fn ensure_installer_install_writable(install_path: &Path, force: bool) -> Result<()> {
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

fn warn_if_installer_path_missing(dir: &Path) {
    let path_env = std::env::var_os("PATH").unwrap_or_default();
    let on_path = std::env::split_paths(&path_env).any(|entry| entry == dir);
    if !on_path {
        eprintln!("WARNING: install dir is not on PATH for this shell: {}", dir.display());
    }
}

fn run_installer_settings_helper(
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

fn render_installer_settings(path: &Path, browser: &str) -> Result<(String, Option<Vec<u8>>)> {
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

fn render_installer_settings_doc(current: &str, browser: &str) -> String {
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

fn write_atomic_file(path: &Path, data: &[u8], mode: u32) -> Result<()> {
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

fn run_installer_smoke_host() -> Result<()> {
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
            ["test", "-p", "si-cli", "build_installer_settings_helper_"],
        )?;
    }

    eprintln!("==> help output");
    run_path_command_checked(&root, &installer_runner, &["build", "installer", "run", "--help"])?;

    let tmp = tempfile::tempdir().context("create temp dir")?;
    eprintln!("==> dry-run: linux/amd64 install-dir with spaces");
    let spaced_dir = tmp.path().join("bin dir");
    fs::create_dir_all(&spaced_dir).with_context(|| format!("create {}", spaced_dir.display()))?;
    run_path_command_checked(
        &root,
        &installer_runner,
        &[
            "build",
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
            "build",
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
            "build",
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
            "build",
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
            "build",
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
            "build",
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
            "build",
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
            "build",
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
            "build",
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
            "build",
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

fn run_installer_smoke_npm() -> Result<()> {
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
                    "build",
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
                "build",
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

fn run_installer_smoke_homebrew() -> Result<()> {
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
                        "build",
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
                    "build",
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

fn find_npm_package_tarball(dir: &Path) -> Result<PathBuf> {
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

fn run_path_command_checked(dir: &Path, path: &Path, args: &[&str]) -> Result<()> {
    run_path_command_with_env_checked(dir, path, &[], args)
}

fn run_path_command_with_env_checked(
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

fn run_path_command_expect_fail(dir: &Path, path: &Path, args: &[&str]) -> Result<()> {
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

fn override_executable(var: &str) -> Option<PathBuf> {
    std::env::var(var)
        .ok()
        .map(|value| value.trim().to_owned())
        .filter(|value| !value.is_empty())
        .map(PathBuf::from)
}

fn run_publish_npm_from_vault(
    repo_root: Option<PathBuf>,
    version: Option<String>,
    out_dir: Option<PathBuf>,
    token_env: String,
    vault_file: Option<PathBuf>,
    dry_run: bool,
) -> Result<()> {
    let repo_root = resolve_release_repo_root(repo_root)?;
    let si_cmd = resolve_si_command(&repo_root)?;
    let token_env = token_env.trim().to_owned();
    if token_env.is_empty() {
        return Err(anyhow!("--token-env must not be empty"));
    }

    let mut vault_args = Vec::new();
    if let Some(file) = vault_file {
        vault_args.push("--file".to_owned());
        vault_args.push(file.display().to_string());
    }

    let status = StdCommand::new(&si_cmd)
        .current_dir(&repo_root)
        .arg("vault")
        .arg("check")
        .args(&vault_args)
        .status()
        .with_context(|| format!("run {} vault check", si_cmd.display()))?;
    if !status.success() {
        return Err(anyhow!("vault check failed"));
    }

    let output = StdCommand::new(&si_cmd)
        .current_dir(&repo_root)
        .arg("vault")
        .arg("list")
        .args(&vault_args)
        .output()
        .with_context(|| format!("run {} vault list", si_cmd.display()))?;
    if !output.status.success() {
        return Err(anyhow!("vault list failed"));
    }
    if !vault_key_exists(&String::from_utf8_lossy(&output.stdout), &token_env) {
        return Err(anyhow!("vault key {token_env} not found"));
    }

    let current_exe = std::env::current_exe().context("resolve current executable")?;
    let mut nested_args = vec![
        "build".to_owned(),
        "npm".to_owned(),
        "publish".to_owned(),
        "--repo-root".to_owned(),
        repo_root.display().to_string(),
        "--token-env".to_owned(),
        token_env.clone(),
    ];
    if let Some(version) = version {
        nested_args.push("--version".to_owned());
        nested_args.push(version);
    }
    if let Some(out_dir) = out_dir {
        nested_args.push("--out-dir".to_owned());
        nested_args.push(out_dir.display().to_string());
    }
    if dry_run {
        nested_args.push("--dry-run".to_owned());
    }

    let status = StdCommand::new(&si_cmd)
        .current_dir(&repo_root)
        .arg("vault")
        .arg("run")
        .args(&vault_args)
        .arg("--")
        .arg(&current_exe)
        .args(&nested_args)
        .status()
        .with_context(|| format!("run {} vault run", si_cmd.display()))?;
    if !status.success() {
        return Err(anyhow!("vault-run corepack pnpm publish failed"));
    }
    Ok(())
}

fn resolve_si_command(repo_root: &Path) -> Result<PathBuf> {
    let local = repo_root.join("si");
    if local.exists() {
        return Ok(local);
    }
    let path = std::env::var_os("PATH").ok_or_else(|| anyhow!("PATH is not set"))?;
    for dir in std::env::split_paths(&path) {
        let candidate = dir.join("si");
        if candidate.exists() {
            return Ok(candidate);
        }
    }
    Err(anyhow!("si CLI not found (expected <repo>/si or si in PATH)"))
}

fn vault_key_exists(output: &str, key: &str) -> bool {
    output
        .lines()
        .any(|line| line.split_whitespace().next().map(|value| value == key).unwrap_or(false))
}

fn run_homebrew_render_core_formula(
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

fn run_homebrew_render_tap_formula(
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

fn run_homebrew_update_tap_repo(
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

fn validate_release_version(version: &str) -> Result<()> {
    if !version.trim().starts_with('v') {
        return Err(anyhow!("version must include v prefix, got: {}", version.trim()));
    }
    if version.trim() == "v" {
        return Err(anyhow!("invalid version"));
    }
    Ok(())
}

fn validate_release_tag_format(tag: &str) -> Result<()> {
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

fn run_validate_release_version(tag: String) -> Result<()> {
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

fn verify_release_archive_contents(path: &Path, target: ReleaseTarget) -> Result<()> {
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

fn find_release_binary(root: &Path) -> Option<PathBuf> {
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

fn verify_release_binary_format(binary: &Path, target: ReleaseTarget) -> Result<()> {
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

fn run_build_self_verify_release_assets(version: String, out_dir: PathBuf) -> Result<()> {
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

fn download_file(url: &str, output: &Path) -> Result<()> {
    let response =
        BlockingHttpClient::new().get(url).send().with_context(|| format!("download {url}"))?;
    let response = response.error_for_status().with_context(|| format!("download {url}"))?;
    let bytes = response.bytes().context("read download body")?;
    fs::write(output, &bytes).with_context(|| format!("write {}", output.display()))?;
    Ok(())
}

fn render_tap_formula_with_base_url(
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

fn parse_checksums_file(path: &Path) -> Result<std::collections::HashMap<String, String>> {
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

fn run_command_checked<const N: usize>(dir: &Path, name: &str, args: [&str; N]) -> Result<()> {
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

fn removed_root_command_replacement(root: &str) -> Option<&'static str> {
    match root {
        "paths" => Some("si settings"),
        _ => None,
    }
}

fn reject_removed_root_command(root: &str) -> Result<()> {
    if let Some(replacement) = removed_root_command_replacement(root) {
        anyhow::bail!("`si {root}` was removed; use `{replacement}` instead");
    }
    Ok(())
}

fn is_public_root_command(name: &str) -> bool {
    find_root_command(name).is_some_and(|spec| !spec.hidden)
}

fn main() -> Result<()> {
    configure_sigpipe();
    let raw_args = env::args().skip(1).collect::<Vec<_>>();
    if let Some(root) = raw_args.first().map(String::as_str) {
        match root {
            "help" => {
                if let Some(target) = raw_args.get(1).map(String::as_str) {
                    reject_removed_root_command(target)?;
                }
            }
            _ => reject_removed_root_command(root)?,
        }
    }
    let cli = parse_cli();

    if cli.version_flag {
        println!("{}", si_core::version::current_version());
        return Ok(());
    }

    let Some(command) = cli.command else {
        return Ok(());
    };
    match command {
        Command::Version => {
            println!("{}", si_core::version::current_version());
        }
        Command::Help { command, format } => show_help(command.as_deref(), format)?,
        Command::Doctor { format } => run_distribution_doctor(format)?,
        Command::Build { command } => match command {
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
                BuildInstallerCommand::SettingsHelper {
                    settings,
                    default_browser,
                    print,
                    check,
                } => run_installer_settings_helper(settings, default_browser, print, check)?,
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
                BuildNpmCommand::PublishPackage {
                    repo_root,
                    version,
                    out_dir,
                    token_env,
                    dry_run,
                } => run_publish_npm_package(repo_root, version, out_dir, token_env, dry_run)?,
                BuildNpmCommand::PublishFromVault {
                    repo_root,
                    version,
                    out_dir,
                    token_env,
                    file,
                    dry_run,
                } => run_publish_npm_from_vault(
                    repo_root, version, out_dir, token_env, file, dry_run,
                )?,
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
        },
        Command::Commands(args) => match args.command {
            Some(CommandsCommand::List(args)) => show_help(None, args.format)?,
            None => show_help(None, args.default_list.format)?,
        },
        Command::Settings(args) => match args.command {
            Some(SettingsCommand::Show(args)) => {
                show_settings(args.home, args.settings_file, args.format)?
            }
            None => show_settings(
                args.default_show.home,
                args.default_show.settings_file,
                args.default_show.format,
            )?,
        },
        Command::Codex { command } => match *command {
            CodexCommand::Profile { command } => match command {
                CodexProfileCommand::List(CodexProfileListArgs { home, settings_file, format }) => {
                    show_codex_profile_list(home, settings_file, format)?
                }
                CodexProfileCommand::Show(CodexProfileShowArgs {
                    profile,
                    home,
                    settings_file,
                    format,
                }) => show_codex_profile(profile, home, settings_file, format)?,
                CodexProfileCommand::Add(CodexProfileAddArgs {
                    profile,
                    name,
                    email,
                    auth_path,
                    activate,
                    home,
                    settings_file,
                    format,
                }) => add_codex_profile(
                    profile,
                    name,
                    email,
                    auth_path,
                    activate,
                    home,
                    settings_file,
                    format,
                )?,
                CodexProfileCommand::Remove(CodexProfileRemoveArgs {
                    profile,
                    home,
                    settings_file,
                }) => remove_codex_profile(profile, home, settings_file)?,
                CodexProfileCommand::Login(CodexProfileLoginArgs {
                    profile,
                    home,
                    settings_file,
                    codex_bin,
                    format,
                }) => login_codex_profile(profile, home, settings_file, codex_bin, format)?,
                CodexProfileCommand::Swap(CodexProfileSwapArgs {
                    profile,
                    home,
                    settings_file,
                    format,
                }) => swap_codex_profile(profile, home, settings_file, format)?,
            },
            CodexCommand::Spawn(CodexSpawnStartArgs {
                profile,
                profile_flag,
                worker_slot,
                workspace,
            }) => {
                let profile = resolve_codex_cli_profile_arg(profile, profile_flag);
                show_codex_spawn_start(profile, worker_slot, workspace)?
            }
            CodexCommand::Remove(CodexRemoveArgs {
                profile,
                profile_flag,
                worker_slot,
                all,
                format,
            }) => {
                let profile = resolve_codex_cli_profile_arg(profile, profile_flag);
                run_codex_remove(profile.as_deref(), worker_slot.as_deref(), all, format)?
            }
            CodexCommand::Stop(CodexStopArgs {
                profile,
                profile_flag,
                worker_slot,
                all,
                format,
            }) => {
                let profile = resolve_codex_cli_profile_arg(profile, profile_flag);
                run_codex_stop(profile.as_deref(), worker_slot.as_deref(), all, format)?
            }
            CodexCommand::Tail(CodexTailArgs { profile, profile_flag, worker_slot, tail }) => {
                let profile = resolve_codex_cli_profile_arg(profile, profile_flag);
                run_codex_tail(profile.as_deref(), worker_slot.as_deref(), &tail)?
            }
            CodexCommand::Shell(CodexShellArgs { profile, worker_slot, command }) => {
                run_codex_shell(profile.as_deref(), worker_slot.as_deref(), command)?
            }
            CodexCommand::List { format } => run_codex_list(format)?,
            CodexCommand::Tmux(CodexTmuxArgs { profile, profile_flag, worker_slot, format }) => {
                let profile = resolve_codex_cli_profile_arg(profile, profile_flag);
                run_codex_tmux_command(profile.as_deref(), worker_slot.as_deref(), format)?
            }
            CodexCommand::RepairAuth(CodexRepairAuthArgs {
                profile,
                profile_flag,
                worker_slot,
                all,
                format,
            }) => {
                let profile = resolve_codex_cli_profile_arg(profile, profile_flag);
                run_codex_repair_auth(profile.as_deref(), worker_slot.as_deref(), all, format)?
            }
            CodexCommand::Warmup { command } => match command {
                WarmupCommand::Decision {
                    state_path,
                    autostart_path,
                    disabled_path,
                    home,
                    format,
                } => run_warmup_autostart_decision(
                    state_path,
                    autostart_path,
                    disabled_path,
                    home,
                    format,
                )?,
                WarmupCommand::Run(WarmupRunArgs {
                    profile,
                    all,
                    path,
                    home,
                    settings_file,
                    workspace,
                    max_turns,
                    turn_timeout_seconds,
                    format,
                }) => run_codex_warmup(
                    profile,
                    all,
                    path,
                    home,
                    settings_file,
                    workspace,
                    max_turns,
                    turn_timeout_seconds,
                    format,
                )?,
                WarmupCommand::Status { path, home, format } => {
                    run_warmup_status(path, home, format)?
                }
                WarmupCommand::State { command } => match command {
                    WarmupStateCommand::Write { path, state_json } => {
                        write_warmup_state(path, &state_json)?
                    }
                },
                WarmupCommand::Marker { command } => match command {
                    WarmupMarkerCommand::Show { autostart_path, disabled_path, home, format } => {
                        run_warmup_marker_show(autostart_path, disabled_path, home, format)?
                    }
                    WarmupMarkerCommand::Enable { path } => write_warmup_autostart_marker(path)?,
                    WarmupMarkerCommand::Disable { path, disabled } => {
                        set_warmup_disabled_marker(path, &disabled)?
                    }
                },
            },
            CodexCommand::Respawn(CodexSpawnStartArgs {
                profile,
                profile_flag,
                worker_slot,
                workspace,
            }) => {
                let profile = resolve_codex_cli_profile_arg(profile, profile_flag);
                run_codex_respawn(profile, worker_slot, workspace)?
            }
        },
        Command::Nucleus { command } => match command {
            NucleusCommand::Status { endpoint, format } => run_nucleus_status(endpoint, format)?,
            NucleusCommand::Profile { command } => match command {
                NucleusProfileCommand::List { endpoint, format } => {
                    run_nucleus_profile_list(endpoint, format)?
                }
            },
            NucleusCommand::Producer { command } => match command {
                NucleusProducerCommand::Cron { command } => match command {
                    NucleusCronCommand::List { endpoint, format } => {
                        run_nucleus_cron_list(endpoint, format)?
                    }
                    NucleusCronCommand::Inspect { rule_name, endpoint, format } => {
                        run_nucleus_cron_inspect(endpoint, rule_name, format)?
                    }
                    NucleusCronCommand::Upsert {
                        name,
                        schedule_kind,
                        schedule,
                        instructions,
                        enabled,
                        reset,
                        endpoint,
                        format,
                    } => run_nucleus_cron_upsert(
                        endpoint,
                        name,
                        schedule_kind,
                        schedule,
                        instructions,
                        enabled,
                        reset,
                        format,
                    )?,
                    NucleusCronCommand::Delete { rule_name, endpoint, format } => {
                        run_nucleus_cron_delete(endpoint, rule_name, format)?
                    }
                },
                NucleusProducerCommand::Hook { command } => match command {
                    NucleusHookCommand::List { endpoint, format } => {
                        run_nucleus_hook_list(endpoint, format)?
                    }
                    NucleusHookCommand::Inspect { rule_name, endpoint, format } => {
                        run_nucleus_hook_inspect(endpoint, rule_name, format)?
                    }
                    NucleusHookCommand::Upsert {
                        name,
                        match_event_type,
                        instructions,
                        enabled,
                        endpoint,
                        format,
                    } => run_nucleus_hook_upsert(
                        endpoint,
                        name,
                        match_event_type,
                        instructions,
                        enabled,
                        format,
                    )?,
                    NucleusHookCommand::Delete { rule_name, endpoint, format } => {
                        run_nucleus_hook_delete(endpoint, rule_name, format)?
                    }
                },
            },
            NucleusCommand::Service { command } => match command {
                NucleusServiceCommand::Install { state_dir, bind_addr, service_dir, format } => {
                    run_nucleus_service_install(state_dir, bind_addr, service_dir, format)?
                }
                NucleusServiceCommand::Uninstall { service_dir, format } => {
                    run_nucleus_service_uninstall(service_dir, format)?
                }
                NucleusServiceCommand::Start { format } => {
                    run_nucleus_service_action("start", format)?
                }
                NucleusServiceCommand::Stop { format } => {
                    run_nucleus_service_action("stop", format)?
                }
                NucleusServiceCommand::Restart { format } => {
                    run_nucleus_service_action("restart", format)?
                }
                NucleusServiceCommand::Status { format } => run_nucleus_service_status(format)?,
                NucleusServiceCommand::Run { state_dir, bind_addr, nucleus_bin } => {
                    run_nucleus_service_run(state_dir, bind_addr, nucleus_bin)?
                }
            },
            NucleusCommand::Task { command } => match command {
                NucleusTaskCommand::Create {
                    title,
                    instructions,
                    endpoint,
                    profile,
                    requires_fort,
                    format,
                } => run_nucleus_task_create(
                    endpoint,
                    title,
                    instructions,
                    profile,
                    requires_fort,
                    format,
                )?,
                NucleusTaskCommand::List { endpoint, format } => {
                    run_nucleus_task_list(endpoint, format)?
                }
                NucleusTaskCommand::Inspect { task_id, endpoint, format } => {
                    run_nucleus_task_inspect(endpoint, task_id, format)?
                }
                NucleusTaskCommand::Cancel { task_id, endpoint, format } => {
                    run_nucleus_task_cancel(endpoint, task_id, format)?
                }
                NucleusTaskCommand::Prune { endpoint, older_than_days, format } => {
                    run_nucleus_task_prune(endpoint, older_than_days, format)?
                }
            },
            NucleusCommand::Worker { command } => match command {
                NucleusWorkerCommand::Probe {
                    profile,
                    endpoint,
                    worker_id,
                    home_dir,
                    codex_home,
                    workdir,
                    env,
                    format,
                } => run_nucleus_worker_probe(
                    endpoint, profile, worker_id, home_dir, codex_home, workdir, env, format,
                )?,
                NucleusWorkerCommand::List { endpoint, format } => {
                    run_nucleus_worker_list(endpoint, format)?
                }
                NucleusWorkerCommand::Inspect { worker_id, endpoint, format } => {
                    run_nucleus_worker_inspect(endpoint, worker_id, format)?
                }
                NucleusWorkerCommand::Restart { worker_id, endpoint, format } => {
                    run_nucleus_worker_restart(endpoint, worker_id, format)?
                }
                NucleusWorkerCommand::RepairAuth { worker_id, endpoint, format } => {
                    run_nucleus_worker_repair_auth(endpoint, worker_id, format)?
                }
            },
            NucleusCommand::Session { command } => match command {
                NucleusSessionCommand::Create {
                    profile,
                    endpoint,
                    worker_id,
                    thread_id,
                    home_dir,
                    codex_home,
                    workdir,
                    env,
                    format,
                } => run_nucleus_session_create(
                    endpoint, profile, worker_id, thread_id, home_dir, codex_home, workdir, env,
                    format,
                )?,
                NucleusSessionCommand::List { endpoint, format } => {
                    run_nucleus_session_list(endpoint, format)?
                }
                NucleusSessionCommand::Show { session_id, endpoint, format } => {
                    run_nucleus_session_show(endpoint, session_id, format)?
                }
            },
            NucleusCommand::Run { command } => match command {
                NucleusRunCommand::SubmitTurn { session_id, prompt, task_id, endpoint, format } => {
                    run_nucleus_run_submit_turn(endpoint, session_id, prompt, task_id, format)?
                }
                NucleusRunCommand::Inspect { run_id, endpoint, format } => {
                    run_nucleus_run_inspect(endpoint, run_id, format)?
                }
                NucleusRunCommand::Cancel { run_id, endpoint, format } => {
                    run_nucleus_run_cancel(endpoint, run_id, format)?
                }
            },
            NucleusCommand::Events { command } => match command {
                NucleusEventsCommand::Subscribe { endpoint, count, format } => {
                    run_nucleus_events_subscribe(endpoint, count, format)?
                }
                NucleusEventsCommand::Ingest {
                    endpoint,
                    event_type,
                    source,
                    profile,
                    payload,
                    format,
                } => run_nucleus_events_ingest(
                    endpoint, event_type, source, profile, payload, format,
                )?,
            },
        },
        Command::Surf {
            home,
            settings_file,
            repo,
            build,
            no_build,
            bin,
            vnc_password_fort_key,
            vnc_password_fort_repo,
            vnc_password_fort_env,
            args,
        } => run_surf_wrapper(
            home,
            settings_file,
            repo,
            build,
            no_build,
            bin,
            vnc_password_fort_key,
            vnc_password_fort_repo,
            vnc_password_fort_env,
            args,
        )?,
        Command::Viva { home, settings_file, repo, build, no_build, bin, args } => {
            run_viva_wrapper(home, settings_file, repo, build, no_build, bin, args)?
        }
        Command::Fort { home, settings_file, repo, build, no_build, bin, args } => {
            run_fort_wrapper(home, settings_file, repo, build, no_build, bin, args)?
        }
        Command::Vault { command } => run_vault_command(command)?,
        Command::Image { command } => match command {
            ImageCommand::Unsplash { command } => {
                run_image_command(ImageProvider::Unsplash, command)?
            }
            ImageCommand::Pexels { command } => run_image_command(ImageProvider::Pexels, command)?,
            ImageCommand::Pixabay { command } => {
                run_image_command(ImageProvider::Pixabay, command)?
            }
        },
    }

    Ok(())
}

fn parse_cli() -> Cli {
    let mut command = build_cli_help_command();
    let mut matches = command.try_get_matches_from_mut(std::env::args_os()).unwrap_or_else(|err| {
        err.exit();
    });
    Cli::from_arg_matches_mut(&mut matches).unwrap_or_else(|err| {
        err.exit();
    })
}

fn build_cli_help_command() -> clap::Command {
    annotate_command_help(Cli::command().styles(cli_help_styles()).color(cli_color_choice()), &[])
}

fn annotate_command_help(mut command: clap::Command, parent_path: &[String]) -> clap::Command {
    let name = command.get_name().to_owned();
    let mut path = parent_path.to_vec();
    if name != "si" {
        path.push(name);
    }

    let summary = command_help_summary(&path, command.has_subcommands());
    if command.get_about().is_none() {
        command = command.about(summary.clone());
    }
    if command.get_long_about().is_none() {
        command = command.long_about(summary);
    }

    command.mut_subcommands(|subcommand| annotate_command_help(subcommand, &path))
}

fn command_help_summary(path: &[String], has_subcommands: bool) -> String {
    let path_refs: Vec<&str> = path.iter().map(String::as_str).collect();
    if let Some(summary) = command_help_override(&path_refs) {
        return summary.to_owned();
    }

    if path.is_empty() {
        return "SI CLI for coding-agent runtimes, secure operations, and build flows.".to_owned();
    }

    if path.len() == 1 {
        if let Some(spec) = find_root_command(path[0].as_str()) {
            return ensure_sentence_period(spec.summary);
        }
    }

    if has_subcommands {
        return format!("{} commands.", command_subject(path));
    }

    leaf_command_help_summary(path)
}

fn command_help_override(path: &[&str]) -> Option<&'static str> {
    match path {
        [] => Some("SI CLI for coding-agent runtimes, secure operations, and build flows."),
        ["help"] => Some("Show SI command help."),
        ["version"] => Some("Print the current SI version."),
        ["commands"] => Some("List visible SI root commands."),
        ["commands", "list"] => Some("List visible SI root commands."),
        ["settings"] => Some("Show resolved SI settings."),
        ["settings", "show"] => Some("Show resolved SI settings."),
        ["build"] => Some("Build binaries and release assets."),
        ["build", "image"] => Some("Build the local SI runtime image."),
        ["build", "self"] => Some("Build, upgrade, or run the SI CLI."),
        ["build", "self", "build"] => Some("Build the SI CLI."),
        ["build", "self", "check"] => Some("Check the SI CLI without linking."),
        ["build", "self", "upgrade"] => Some("Install the freshly built SI CLI."),
        ["build", "self", "run"] => Some("Build and run the SI CLI."),
        ["build", "self", "asset"] => Some("Build one release asset."),
        ["build", "self", "assets"] => Some("Build all release assets."),
        ["build", "self", "validate"] => Some("Validate a release version tag."),
        ["build", "self", "verify"] => Some("Verify release assets."),
        ["nucleus"] => Some("Manage the SI Nucleus control plane."),
        ["nucleus", "service"] => Some("Manage the local SI Nucleus user service."),
        ["nucleus", "service", "install"] => {
            Some("Install the local SI Nucleus user-service definition.")
        }
        ["nucleus", "service", "uninstall"] => {
            Some("Remove the local SI Nucleus user-service definition.")
        }
        ["nucleus", "service", "start"] => Some("Start the local SI Nucleus user service."),
        ["nucleus", "service", "stop"] => Some("Stop the local SI Nucleus user service."),
        ["nucleus", "service", "restart"] => Some("Restart the local SI Nucleus user service."),
        ["nucleus", "service", "status"] => Some("Show SI Nucleus service status and log hints."),
        ["nucleus", "producer"] => Some("Manage Nucleus task producers."),
        ["nucleus", "producer", "cron"] => Some("Manage Nucleus cron task producers."),
        ["nucleus", "producer", "hook"] => Some("Manage Nucleus event hook task producers."),
        ["codex"] => Some("Manage Codex profiles and worker sessions."),
        ["codex", "profile"] => Some("Manage Codex profiles."),
        ["codex", "spawn"] => Some("Start a Codex worker for a chosen profile."),
        ["codex", "remove"] => Some("Remove a Codex worker session."),
        ["codex", "stop"] => Some("Stop a Codex worker session."),
        ["codex", "tail"] => Some("Tail Codex worker session output."),
        ["codex", "shell"] => Some("Run a shell command with a Codex worker environment."),
        ["codex", "list"] => Some("List Codex worker sessions."),
        ["codex", "tmux"] => Some("Attach to a Codex tmux session."),
        ["codex", "warmup"] => Some("Inspect Codex warmup state."),
        ["codex", "warmup", "run"] => Some("Warm configured Codex profiles."),
        ["codex", "respawn"] => Some("Remove and recreate a Codex worker session."),
        ["codex", "warmup", "decision"] => Some("Decide whether warmup should run."),
        ["codex", "warmup", "status"] => Some("Show warmup status."),
        ["codex", "warmup", "state"] => Some("Warmup state file commands."),
        ["codex", "warmup", "marker"] => Some("Warmup marker file commands."),
        ["codex", "warmup", "marker", "show"] => Some("Show warmup marker state."),
        ["codex", "warmup", "marker", "enable"] => Some("Write the autostart marker."),
        ["codex", "warmup", "marker", "disable"] => Some("Set the disabled marker."),
        ["vault"] => Some("Vault secret and trust commands."),
        ["vault", "trust"] => Some("Verify trusted vault inputs."),
        ["image"] => Some("Image provider commands."),
        _ => None,
    }
}

fn leaf_command_help_summary(path: &[String]) -> String {
    let name = path.last().expect("leaf command path");
    if let Some((verb, remainder)) = parse_action_name(name) {
        return render_action_summary(verb, remainder.as_deref(), &path[..path.len() - 1]);
    }

    match name.as_str() {
        "raw" => format!("Run raw {} requests.", command_subject(&path[..path.len() - 1])),
        "doctor" => format!("Check {}.", command_subject(&path[..path.len() - 1])),
        "report" => format!("Generate {}.", command_subject(&path[..path.len() - 1])),
        _ => {
            let current = command_subject_sentence(path);
            if path.len() > 1 {
                format!("Manage {} for {}.", current, command_subject(&path[..path.len() - 1]))
            } else {
                format!("{current} command.")
            }
        }
    }
}

fn parse_action_name(name: &str) -> Option<(&str, Option<String>)> {
    let mut parts = name.split('-');
    let first = parts.next()?;
    if !is_action_word(first) {
        return None;
    }
    let remainder = parts.collect::<Vec<_>>();
    if remainder.is_empty() {
        Some((first, None))
    } else {
        Some((first, Some(humanize_identifier(&remainder.join("-"), WordStyle::Sentence))))
    }
}

fn render_action_summary(verb: &str, remainder: Option<&str>, parent_path: &[String]) -> String {
    let subject = command_subject(parent_path);
    let object = remainder
        .filter(|value| !value.is_empty())
        .map(str::to_owned)
        .unwrap_or_else(|| default_action_object(verb, &subject));

    let phrase = match verb {
        "add" => "Add",
        "apply" => "Apply",
        "archive" => "Archive",
        "bootstrap" => "Bootstrap",
        "build" => "Build",
        "cancel" => "Cancel",
        "classify" => "Classify",
        "cleanup" => "Clean up",
        "clear" => "Clear",
        "comment" => "Comment on",
        "copy" => "Copy",
        "create" => "Create",
        "delete" => "Delete",
        "disable" => "Disable",
        "dispatch" => "Dispatch",
        "download" => "Download",
        "enable" => "Enable",
        "exec" => "Run",
        "generate" => "Generate",
        "get" => "Get",
        "list" => "List",
        "login" => "Log in to",
        "lookup" => "Look up",
        "logs" => "Show",
        "move" => "Move",
        "peek" => "Preview",
        "protect" => "Protect",
        "publish" => "Publish",
        "refresh" => "Refresh",
        "remove" => "Remove",
        "report" => "Generate",
        "restart" => "Restart",
        "respawn" => "Respawn",
        "run" => "Run",
        "search" => "Search",
        "set" => "Set",
        "show" => "Show",
        "start" => "Start",
        "status" => "Show",
        "stop" => "Stop",
        "swap" => "Switch",
        "tail" => "Tail",
        "teardown" => "Tear down",
        "trust" => "Trust",
        "unarchive" => "Unarchive",
        "unprotect" => "Unprotect",
        "update" => "Update",
        "upload" => "Upload",
        "validate" => "Validate",
        "verify" => "Verify",
        "write" => "Write",
        _ => "Run",
    };

    format!("{phrase} {object}.")
}

fn default_action_object(verb: &str, subject: &str) -> String {
    match verb {
        "list" | "search" => pluralize_phrase(subject),
        "logs" => format!("{subject} logs"),
        "status" => format!("{subject} status"),
        "tail" => format!("{subject} logs"),
        _ => subject.to_owned(),
    }
}

fn is_action_word(word: &str) -> bool {
    matches!(
        word,
        "add"
            | "apply"
            | "archive"
            | "bootstrap"
            | "build"
            | "cancel"
            | "classify"
            | "cleanup"
            | "clear"
            | "comment"
            | "copy"
            | "create"
            | "delete"
            | "disable"
            | "dispatch"
            | "download"
            | "enable"
            | "exec"
            | "generate"
            | "get"
            | "list"
            | "login"
            | "lookup"
            | "logs"
            | "move"
            | "peek"
            | "protect"
            | "publish"
            | "refresh"
            | "remove"
            | "report"
            | "restart"
            | "respawn"
            | "run"
            | "search"
            | "set"
            | "show"
            | "start"
            | "status"
            | "stop"
            | "swap"
            | "tail"
            | "teardown"
            | "trust"
            | "unarchive"
            | "unprotect"
            | "update"
            | "upload"
            | "validate"
            | "verify"
            | "write"
    )
}

fn command_subject(path: &[String]) -> String {
    command_subject_with_style(path, WordStyle::Title)
}

fn command_subject_sentence(path: &[String]) -> String {
    command_subject_with_style(path, WordStyle::Sentence)
}

fn command_subject_with_style(path: &[String], style: WordStyle) -> String {
    if path.is_empty() {
        return "SI".to_owned();
    }

    if path.first().is_some_and(|segment| segment == "build") && path.len() == 2 {
        return match path[1].as_str() {
            "self" => "SI CLI".to_owned(),
            "image" => "runtime image".to_owned(),
            other => format_command_segment(other, style),
        };
    }

    let start = if path.len() <= 2 {
        0
    } else if is_contextual_segment(path.last().expect("command path")) {
        path.len().saturating_sub(3)
    } else {
        path.len().saturating_sub(2)
    };

    path[start..]
        .iter()
        .map(|segment| format_command_segment(segment, style))
        .collect::<Vec<_>>()
        .join(" ")
}

fn is_contextual_segment(segment: &str) -> bool {
    matches!(
        segment,
        "access"
            | "agent"
            | "alias"
            | "api-key"
            | "app"
            | "asset"
            | "auth"
            | "batch"
            | "branch"
            | "broadcast"
            | "bucket"
            | "cache"
            | "caption"
            | "chat"
            | "comment"
            | "context"
            | "details"
            | "directory"
            | "domain"
            | "email"
            | "endpoint"
            | "env"
            | "function"
            | "git"
            | "group"
            | "guardrail"
            | "image"
            | "instance"
            | "invitation"
            | "issue"
            | "item"
            | "job"
            | "key"
            | "knowledge-base"
            | "listing"
            | "live"
            | "marker"
            | "migration"
            | "model"
            | "network"
            | "object"
            | "operation"
            | "parameter"
            | "pages"
            | "pipeline"
            | "playlist"
            | "policy"
            | "profile"
            | "project"
            | "pull-request"
            | "release"
            | "repo"
            | "report"
            | "repository"
            | "resource"
            | "role"
            | "run"
            | "runtime"
            | "secret"
            | "service"
            | "service-account"
            | "session"
            | "settings"
            | "state"
            | "stream"
            | "subscription"
            | "support"
            | "table"
            | "thumbnail"
            | "token"
            | "trust"
            | "tunnel"
            | "types"
            | "video"
            | "workflow"
            | "workers"
    )
}

fn format_command_segment(segment: &str, style: WordStyle) -> String {
    match segment {
        "api-key" | "apikey" => "API key".to_owned(),
        "appstore" => "App Store".to_owned(),
        "aws" => "AWS".to_owned(),
        "cloudwatch" => "CloudWatch".to_owned(),
        "codex" => "Codex".to_owned(),
        "d1" => "D1".to_owned(),
        "dynamodb" => "DynamoDB".to_owned(),
        "ecr" => "ECR".to_owned(),
        "ec2" => "EC2".to_owned(),
        "gcp" => "GCP".to_owned(),
        "github" => "GitHub".to_owned(),
        "iam" => "IAM".to_owned(),
        "json" => "JSON".to_owned(),
        "kms" => "KMS".to_owned(),
        "kv" => "KV".to_owned(),
        "lb" => "load balancer".to_owned(),
        "oci" => "OCI".to_owned(),
        "openai" => "OpenAI".to_owned(),
        "r2" => "R2".to_owned(),
        "s3" => "S3".to_owned(),
        "service-account" => "service account".to_owned(),
        "ssh" => "SSH".to_owned(),
        "ssm" => "SSM".to_owned(),
        "sts" => "STS".to_owned(),
        "tls" => "TLS".to_owned(),
        "tmux" => "tmux".to_owned(),
        "ui" => "UI".to_owned(),
        "url" => "URL".to_owned(),
        "workos" => "WorkOS".to_owned(),
        "youtube" => "YouTube".to_owned(),
        other => humanize_identifier(other, style),
    }
}

#[derive(Clone, Copy)]
enum WordStyle {
    Title,
    Sentence,
}

fn humanize_identifier(identifier: &str, style: WordStyle) -> String {
    identifier
        .split(['-', '_'])
        .filter(|word| !word.is_empty())
        .enumerate()
        .map(|(index, word)| format_help_word(word, style, index))
        .collect::<Vec<_>>()
        .join(" ")
}

fn format_help_word(word: &str, style: WordStyle, index: usize) -> String {
    match word {
        "api" => "API".to_owned(),
        "aws" => "AWS".to_owned(),
        "cli" => "CLI".to_owned(),
        "codex" => "Codex".to_owned(),
        "gcp" => "GCP".to_owned(),
        "gh" => "GitHub".to_owned(),
        "github" => "GitHub".to_owned(),
        "iam" => "IAM".to_owned(),
        "id" => "ID".to_owned(),
        "json" => "JSON".to_owned(),
        "oci" => "OCI".to_owned(),
        "openai" => "OpenAI".to_owned(),
        "ssh" => "SSH".to_owned(),
        "tmux" => "tmux".to_owned(),
        "ui" => "UI".to_owned(),
        "url" => "URL".to_owned(),
        "workos" => "WorkOS".to_owned(),
        "youtube" => "YouTube".to_owned(),
        value => match style {
            WordStyle::Title => capitalize_ascii(value),
            WordStyle::Sentence if index == 0 => value.to_owned(),
            WordStyle::Sentence => value.to_owned(),
        },
    }
}

fn capitalize_ascii(value: &str) -> String {
    let mut chars = value.chars();
    let Some(first) = chars.next() else {
        return String::new();
    };
    let mut output = String::new();
    output.push(first.to_ascii_uppercase());
    output.push_str(chars.as_str());
    output
}

fn pluralize_phrase(phrase: &str) -> String {
    if let Some((prefix, last)) = phrase.rsplit_once(' ') {
        format!("{prefix} {}", pluralize_word(last))
    } else {
        pluralize_word(phrase)
    }
}

fn pluralize_word(word: &str) -> String {
    let lower = word.to_ascii_lowercase();
    if lower.ends_with('y')
        && !matches!(lower.chars().rev().nth(1), Some('a' | 'e' | 'i' | 'o' | 'u'))
    {
        return format!("{}ies", &word[..word.len() - 1]);
    }
    if lower.ends_with('s')
        || lower.ends_with('x')
        || lower.ends_with('z')
        || lower.ends_with("ch")
        || lower.ends_with("sh")
    {
        return format!("{word}es");
    }
    format!("{word}s")
}

fn ensure_sentence_period(summary: &str) -> String {
    let trimmed = summary.trim();
    if trimmed.ends_with('.') { trimmed.to_owned() } else { format!("{trimmed}.") }
}

#[cfg(unix)]
fn configure_sigpipe() {
    unsafe {
        libc::signal(libc::SIGPIPE, libc::SIG_DFL);
    }
}

#[cfg(not(unix))]
fn configure_sigpipe() {}

fn show_help(command: Option<&str>, format: OutputFormat) -> Result<()> {
    let view = match command {
        Some(name) => {
            reject_removed_root_command(name)?;
            if !is_public_root_command(name) {
                anyhow::bail!("unknown root command: {name}");
            }
            let spec = find_root_command(name)
                .ok_or_else(|| anyhow::anyhow!("unknown root command: {name}"))?;
            HelpView { commands: vec![command_view(spec)] }
        }
        None => HelpView {
            commands: visible_root_commands()
                .filter(|spec| is_public_root_command(spec.name))
                .map(command_view)
                .collect(),
        },
    };

    render_help(view, format)
}

fn render_help(view: HelpView, format: OutputFormat) -> Result<()> {
    match format {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&view)?);
        }
        OutputFormat::Text => {
            for command in view.commands {
                println!("{}", stdout_text(&command.name, CliTone::Command));
                print_cli_kv("category", format_category(command.category));
                if command.aliases.is_empty() {
                    print_cli_kv("aliases", "(none)");
                } else {
                    print_cli_kv("aliases", command.aliases.join(", "));
                }
                print_cli_kv("summary", command.summary);
            }
        }
    }

    Ok(())
}

#[derive(Debug, Clone, Copy)]
enum ImageProvider {
    Unsplash,
    Pexels,
    Pixabay,
}

#[derive(Clone, Copy)]
enum CliStream {
    Stdout,
    Stderr,
}

#[derive(Clone, Copy)]
enum CliTone {
    Heading,
    Section,
    Command,
    Flag,
    Label,
    Success,
    Warning,
    Danger,
    Muted,
    Info,
}

fn cli_color_choice() -> ColorChoice {
    if let Ok(value) = env::var("SI_CLI_COLOR") {
        match value.trim().to_ascii_lowercase().as_str() {
            "always" => return ColorChoice::Always,
            "never" => return ColorChoice::Never,
            "auto" => {}
            _ => {}
        }
    }
    if env::var_os("NO_COLOR").is_some() { ColorChoice::Never } else { ColorChoice::Auto }
}

fn cli_stream_supports_color(stream: CliStream) -> bool {
    match cli_color_choice() {
        ColorChoice::Always => true,
        ColorChoice::Never => false,
        ColorChoice::Auto => match stream {
            CliStream::Stdout => io::stdout().is_terminal(),
            CliStream::Stderr => io::stderr().is_terminal(),
        },
    }
}

fn cli_ansi_code(tone: CliTone) -> &'static str {
    match tone {
        CliTone::Heading | CliTone::Section => "1;36",
        CliTone::Command => "1;35",
        CliTone::Flag | CliTone::Warning => "1;33",
        CliTone::Label => "1;34",
        CliTone::Success => "1;32",
        CliTone::Danger => "1;31",
        CliTone::Muted => "0;90",
        CliTone::Info => "0;36",
    }
}

fn style_cli_text(text: &str, tone: CliTone, stream: CliStream) -> String {
    if text.is_empty() || !cli_stream_supports_color(stream) {
        text.to_owned()
    } else {
        format!("\u{1b}[{}m{}\u{1b}[0m", cli_ansi_code(tone), text)
    }
}

fn stdout_text(text: &str, tone: CliTone) -> String {
    style_cli_text(text, tone, CliStream::Stdout)
}

fn stderr_text(text: &str, tone: CliTone) -> String {
    style_cli_text(text, tone, CliStream::Stderr)
}

fn print_cli_kv(key: &str, value: impl fmt::Display) {
    println!("{}={value}", stdout_text(key, CliTone::Label));
}

fn print_help_usage(command_line: &str) {
    println!(
        "{} {}",
        stdout_text("Usage:", CliTone::Heading),
        stdout_text(command_line, CliTone::Command)
    );
}

fn print_help_section(title: &str) {
    println!("{}", stdout_text(title, CliTone::Section));
}

fn print_help_item(text: &str, tone: CliTone) {
    println!("  {}", stdout_text(text, tone));
}

struct CliSpinnerHandle {
    done: Arc<AtomicUsize>,
    stop: Arc<AtomicBool>,
    thread: Option<thread::JoinHandle<()>>,
}

impl CliSpinnerHandle {
    fn progress_counter(&self) -> Arc<AtomicUsize> {
        Arc::clone(&self.done)
    }

    fn finish(mut self) {
        self.stop.store(true, Ordering::Relaxed);
        if let Some(thread) = self.thread.take() {
            let _ = thread.join();
        }
        eprint!("\r\x1b[2K");
        let _ = io::stderr().flush();
    }
}

fn codex_profile_list_spinner_frame(frame_idx: usize, done: usize, total: usize) -> String {
    let width = 10;
    let head = frame_idx % width;
    let mut rail = ['.'; 10];
    rail[head] = '=';
    let rail = rail.into_iter().collect::<String>();
    format!(
        "{} {} {}",
        stderr_text(&format!("si radar [{rail}]"), CliTone::Command),
        stderr_text(&format!("{done}/{total}"), CliTone::Info),
        stderr_text("sampling codex profiles", CliTone::Muted),
    )
}

fn start_codex_profile_list_spinner(total: usize) -> Option<CliSpinnerHandle> {
    if total == 0 || !io::stderr().is_terminal() {
        return None;
    }

    let done = Arc::new(AtomicUsize::new(0));
    let stop = Arc::new(AtomicBool::new(false));
    let spinner_done = Arc::clone(&done);
    let spinner_stop = Arc::clone(&stop);
    let thread = thread::spawn(move || {
        let mut frame_idx = 0usize;
        while !spinner_stop.load(Ordering::Relaxed) {
            let frame = codex_profile_list_spinner_frame(
                frame_idx,
                spinner_done.load(Ordering::Relaxed).min(total),
                total,
            );
            eprint!("\r\x1b[2K{frame}");
            let _ = io::stderr().flush();
            frame_idx = frame_idx.wrapping_add(1);
            thread::sleep(Duration::from_millis(90));
        }
    });

    Some(CliSpinnerHandle { done, stop, thread: Some(thread) })
}

fn cli_table_color(tone: CliTone) -> Color {
    match tone {
        CliTone::Heading | CliTone::Section | CliTone::Info => Color::Cyan,
        CliTone::Command => Color::Magenta,
        CliTone::Flag | CliTone::Warning => Color::DarkYellow,
        CliTone::Label => Color::Blue,
        CliTone::Success => Color::Green,
        CliTone::Danger => Color::Red,
        CliTone::Muted => Color::DarkGrey,
    }
}

fn cli_help_styles() -> Styles {
    Styles::styled()
        .header(AnsiColor::Cyan.on_default().effects(Effects::BOLD))
        .usage(AnsiColor::Cyan.on_default().effects(Effects::BOLD))
        .literal(AnsiColor::Magenta.on_default().effects(Effects::BOLD))
        .placeholder(AnsiColor::Yellow.on_default())
        .valid(AnsiColor::Green.on_default().effects(Effects::BOLD))
        .invalid(AnsiColor::Red.on_default().effects(Effects::BOLD))
        .error(AnsiColor::Red.on_default().effects(Effects::BOLD))
}

fn run_image_command(provider: ImageProvider, command: ImageProviderCommand) -> Result<()> {
    match command {
        ImageProviderCommand::Auth { command } => match command {
            ImageAuthCommand::Status { api_key, base_url, json } => {
                run_image_auth_status(provider, api_key, base_url, json)
            }
        },
        ImageProviderCommand::Search {
            query,
            page,
            per_page,
            orientation,
            api_key,
            base_url,
            json,
        } => run_image_search(
            provider,
            &query,
            page,
            per_page,
            orientation.as_deref(),
            api_key,
            base_url,
            json,
        ),
    }
}

fn run_image_auth_status(
    provider: ImageProvider,
    api_key: Option<String>,
    base_url: Option<String>,
    json: bool,
) -> Result<()> {
    let base_url = image_base_url(provider, base_url.as_deref());
    match image_api_key(provider, api_key.as_deref()) {
        Ok((_, source)) => {
            if json {
                println!(
                    "{}",
                    serde_json::to_string_pretty(&serde_json::json!({
                        "ok": true,
                        "provider": image_provider_name(provider),
                        "base_url": base_url,
                        "source": source,
                    }))?
                );
            } else {
                println!("{} auth configured ({source})", image_provider_name(provider));
            }
            Ok(())
        }
        Err(err) => {
            if json {
                println!(
                    "{}",
                    serde_json::to_string_pretty(&serde_json::json!({
                        "ok": false,
                        "provider": image_provider_name(provider),
                        "base_url": base_url,
                        "error": err.to_string(),
                    }))?
                );
            }
            Err(err)
        }
    }
}

fn run_image_search(
    provider: ImageProvider,
    query: &str,
    page: i32,
    per_page: i32,
    orientation: Option<&str>,
    api_key: Option<String>,
    base_url: Option<String>,
    json: bool,
) -> Result<()> {
    let (api_key, _) = image_api_key(provider, api_key.as_deref())?;
    let base_url = image_base_url(provider, base_url.as_deref());
    let path = match provider {
        ImageProvider::Unsplash => "/search/photos",
        ImageProvider::Pexels => "/v1/search",
        ImageProvider::Pixabay => "/api/",
    };
    let mut url = format!("{}{}", base_url.trim_end_matches('/'), path);
    let mut params = vec![
        (
            match provider {
                ImageProvider::Pixabay => "q",
                _ => "query",
            },
            query.to_owned(),
        ),
        ("page", page.max(1).to_string()),
        ("per_page", per_page.clamp(1, 80).to_string()),
    ];
    if let Some(value) = orientation.map(str::trim).filter(|value| !value.is_empty()) {
        if matches!(provider, ImageProvider::Unsplash) {
            params.push(("orientation", value.to_owned()));
        }
    }
    if matches!(provider, ImageProvider::Pixabay) {
        params.push(("key", api_key.clone()));
    }
    let client = BlockingHttpClient::new();
    let mut request = client.get(&url).query(&params);
    request = match provider {
        ImageProvider::Unsplash => request.header("Authorization", format!("Client-ID {api_key}")),
        ImageProvider::Pexels => request.header("Authorization", api_key),
        ImageProvider::Pixabay => request,
    };
    let response = request.send().context("run image search request")?;
    let status = response.status();
    let headers = response.headers().clone();
    let body = response.text().context("read image search response body")?;
    let parsed =
        serde_json::from_str::<Value>(&body).unwrap_or_else(|_| Value::String(body.clone()));
    if json {
        println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({
                "status_code": status.as_u16(),
                "status": status.to_string(),
                "request_id": image_request_id(provider, &headers),
                "data": parsed,
            }))?
        );
    } else {
        url.clear();
        println!("{body}");
    }
    if !status.is_success() {
        return Err(anyhow!(
            "image request failed: provider={} status={}",
            image_provider_name(provider),
            status
        ));
    }
    Ok(())
}

fn image_provider_name(provider: ImageProvider) -> &'static str {
    match provider {
        ImageProvider::Unsplash => "unsplash",
        ImageProvider::Pexels => "pexels",
        ImageProvider::Pixabay => "pixabay",
    }
}

fn image_base_url(provider: ImageProvider, override_base: Option<&str>) -> String {
    if let Some(value) = override_base.map(str::trim).filter(|value| !value.is_empty()) {
        return value.trim_end_matches('/').to_owned();
    }
    match provider {
        ImageProvider::Unsplash => "https://api.unsplash.com".to_owned(),
        ImageProvider::Pexels => "https://api.pexels.com".to_owned(),
        ImageProvider::Pixabay => "https://pixabay.com".to_owned(),
    }
}

fn image_api_key(
    provider: ImageProvider,
    override_key: Option<&str>,
) -> Result<(String, &'static str)> {
    if let Some(value) = override_key.map(str::trim).filter(|value| !value.is_empty()) {
        return Ok((value.to_owned(), "flag:--api-key"));
    }
    let env_key = match provider {
        ImageProvider::Unsplash => "UNSPLASH_ACCESS_KEY",
        ImageProvider::Pexels => "PEXELS_API_KEY",
        ImageProvider::Pixabay => "PIXABAY_API_KEY",
    };
    if let Ok(value) = std::env::var(env_key) {
        if !value.trim().is_empty() {
            return Ok((value.trim().to_owned(), "env"));
        }
    }
    Err(anyhow!(
        "missing {} api key (set {} or pass --api-key)",
        image_provider_name(provider),
        env_key
    ))
}

fn image_request_id(
    provider: ImageProvider,
    headers: &reqwest::header::HeaderMap,
) -> Option<String> {
    let key = match provider {
        ImageProvider::Unsplash => "x-request-id",
        ImageProvider::Pexels => "x-request-id",
        ImageProvider::Pixabay => "x-request-id",
    };
    headers.get(key).and_then(|value| value.to_str().ok()).map(str::to_owned)
}

fn show_settings(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let resolved_paths = SiPaths::from_settings(&home, &settings);

    match format {
        OutputFormat::Text => {
            print_cli_kv("schema_version", settings.schema_version);
            print_cli_kv("paths.root", resolved_paths.root.display());
            print_cli_kv("paths.settings_file", resolved_paths.settings_file.display());
            print_cli_kv("paths.codex_profiles_dir", resolved_paths.codex_profiles_dir.display());
            print_cli_kv(
                "paths.workspace_root",
                settings.paths.workspace_root.as_deref().unwrap_or("(none)"),
            );
            print_cli_kv(
                "codex.workspace",
                settings.codex.workspace.as_deref().unwrap_or("(none)"),
            );
            print_cli_kv("codex.workdir", settings.codex.workdir.as_deref().unwrap_or("(none)"));
            print_cli_kv("codex.profile", settings.codex.profile.as_deref().unwrap_or("(none)"));
            print_cli_kv("fort.repo", settings.fort.repo.as_deref().unwrap_or("(none)"));
            print_cli_kv("fort.bin", settings.fort.bin.as_deref().unwrap_or("(none)"));
            let fort_build = settings
                .fort
                .build
                .map(|value| value.to_string())
                .unwrap_or_else(|| "(none)".to_owned());
            print_cli_kv("fort.build", fort_build);
            print_cli_kv("fort.host", settings.fort.host.as_deref().unwrap_or("(none)"));
            print_cli_kv(
                "fort.runtime_host",
                settings.fort.runtime_host.as_deref().unwrap_or("(none)"),
            );
        }
        OutputFormat::Json => {
            let mut value = serde_json::to_value(&settings)?;
            if let Some(paths) = value.get_mut("paths").and_then(Value::as_object_mut) {
                paths.insert(
                    "root".to_owned(),
                    Value::String(resolved_paths.root.display().to_string()),
                );
                paths.insert(
                    "settings_file".to_owned(),
                    Value::String(resolved_paths.settings_file.display().to_string()),
                );
                paths.insert(
                    "codex_profiles_dir".to_owned(),
                    Value::String(resolved_paths.codex_profiles_dir.display().to_string()),
                );
            }
            println!("{}", serde_json::to_string_pretty(&value)?);
        }
    }

    Ok(())
}

#[derive(Debug, Serialize)]
struct FortConfigView {
    repo: Option<String>,
    bin: Option<String>,
    build: Option<bool>,
    host: Option<String>,
    runtime_host: Option<String>,
}

#[derive(Debug, Serialize)]
struct VivaConfigView {
    repo: Option<String>,
    bin: Option<String>,
    build: Option<bool>,
    tunnel_default_profile: String,
}

#[derive(Debug, Serialize)]
struct SurfWrapperConfigView {
    repo: Option<String>,
    bin: Option<String>,
    build: Option<bool>,
    settings_file: Option<String>,
    state_dir: Option<String>,
    vnc_password_fort_key: Option<String>,
    vnc_password_fort_repo: Option<String>,
    vnc_password_fort_env: Option<String>,
}

#[derive(Debug, Clone)]
struct SurfVncPasswordFortSource {
    key: String,
    repo: String,
    env_name: String,
}

fn run_surf_wrapper(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    repo: Option<PathBuf>,
    build: bool,
    no_build: bool,
    bin: Option<PathBuf>,
    vnc_password_fort_key: Option<String>,
    vnc_password_fort_repo: Option<String>,
    vnc_password_fort_env: Option<String>,
    args: Vec<String>,
) -> Result<()> {
    let args = strip_wrapper_passthrough_marker(args);
    if args.is_empty() || matches!(args[0].as_str(), "-h" | "--help" | "help") {
        render_surf_wrapper_help();
        return Ok(());
    }
    if args[0] == "wrapper" {
        return run_surf_wrapper_command(home, settings_file, args.into_iter().skip(1).collect());
    }
    run_native_surf_command(
        home,
        settings_file,
        repo,
        build,
        no_build,
        bin,
        vnc_password_fort_key,
        vnc_password_fort_repo,
        vnc_password_fort_env,
        &args,
    )
}

fn render_surf_wrapper_help() {
    print_help_usage("si surf [WRAPPER_OPTIONS] <COMMAND> [ARGS...]");
    println!();
    print_help_section("Wrapper commands");
    print_help_item("wrapper config show|set", CliTone::Command);
    println!();
    print_help_section("Native surf passthrough");
    print_help_item(
        "build | start | status | logs | stop | proxy | config ... | session ...",
        CliTone::Command,
    );
    println!();
    print_help_section("Wrapper options");
    print_help_item("--home <PATH>", CliTone::Flag);
    print_help_item("--settings-file <PATH>", CliTone::Flag);
    print_help_item("--repo <PATH>", CliTone::Flag);
    print_help_item("--build", CliTone::Flag);
    print_help_item("--no-build", CliTone::Flag);
    print_help_item("--bin <PATH>", CliTone::Flag);
    print_help_item("--vnc-password-fort-key <KEY>", CliTone::Flag);
    print_help_item("--vnc-password-fort-repo <REPO>", CliTone::Flag);
    print_help_item("--vnc-password-fort-env <ENV>", CliTone::Flag);
    println!();
    print_help_section("Examples");
    print_help_item("si surf build", CliTone::Command);
    print_help_item("si surf start", CliTone::Command);
    print_help_item("si surf --vnc-password-fort-key SURF_VNC_PASSWORD start", CliTone::Command);
    print_help_item(
        "si surf wrapper config set --repo ~/Development/surf --build true",
        CliTone::Command,
    );
    print_help_item("si surf config show", CliTone::Command);
}

fn run_surf_wrapper_command(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    args: Vec<String>,
) -> Result<()> {
    let SurfWrapperCli { command } =
        parse_fort_parser(std::iter::once("wrapper".to_owned()).chain(args).collect());
    match command {
        SurfWrapperCommand::Config { command } => match command {
            SurfWrapperConfigCommand::Show { format } => {
                show_surf_wrapper_config(home, settings_file, format)
            }
            SurfWrapperConfigCommand::Set { repo, bin, build } => {
                set_surf_wrapper_config(home, settings_file, repo, bin, build)
            }
        },
    }
}

fn show_surf_wrapper_config(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let view = SurfWrapperConfigView {
        repo: non_empty_string(settings.surf.repo),
        bin: non_empty_string(settings.surf.bin),
        build: settings.surf.build,
        settings_file: non_empty_string(settings.surf.settings_file),
        state_dir: non_empty_string(settings.surf.state_dir),
        vnc_password_fort_key: non_empty_string(settings.surf.vnc_password_fort_key),
        vnc_password_fort_repo: non_empty_string(settings.surf.vnc_password_fort_repo),
        vnc_password_fort_env: non_empty_string(settings.surf.vnc_password_fort_env),
    };
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            print_cli_kv("repo", render_option_text_value(view.repo.as_deref()));
            print_cli_kv("bin", render_option_text_value(view.bin.as_deref()));
            print_cli_kv(
                "build",
                view.build.map(|value| value.to_string()).unwrap_or_else(|| "(none)".to_owned()),
            );
            print_cli_kv("settings_file", render_option_text_value(view.settings_file.as_deref()));
            print_cli_kv("state_dir", render_option_text_value(view.state_dir.as_deref()));
            print_cli_kv(
                "vnc_password_fort_key",
                render_option_text_value(view.vnc_password_fort_key.as_deref()),
            );
            print_cli_kv(
                "vnc_password_fort_repo",
                render_option_text_value(view.vnc_password_fort_repo.as_deref()),
            );
            print_cli_kv(
                "vnc_password_fort_env",
                render_option_text_value(view.vnc_password_fort_env.as_deref()),
            );
        }
    }
    Ok(())
}

fn set_surf_wrapper_config(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    repo: Option<String>,
    bin: Option<String>,
    build: Option<bool>,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings_path =
        settings_file.unwrap_or_else(|| home.join(".si").join("surf").join("si.settings.toml"));
    let mut document = load_settings_document(&settings_path)?;
    if !document.contains_key("schema_version") {
        document.insert("schema_version".to_owned(), toml::Value::Integer(1));
    }
    let surf = ensure_toml_table(&mut document, "surf")?;
    set_toml_string(surf, "repo", repo);
    set_toml_string(surf, "bin", bin);
    set_toml_bool(surf, "build", build);
    if surf.is_empty() {
        document.remove("surf");
    }
    write_settings_document(&settings_path, &document)?;
    Ok(())
}

fn run_native_surf_command(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    repo: Option<PathBuf>,
    build: bool,
    no_build: bool,
    bin: Option<PathBuf>,
    vnc_password_fort_key: Option<String>,
    vnc_password_fort_repo: Option<String>,
    vnc_password_fort_env: Option<String>,
    args: &[String],
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let explicit_program = bin.is_some() || repo.is_some() || !settings.surf.bin.trim().is_empty();
    let program = resolve_surf_program(&settings.surf, repo.clone(), build, no_build, bin.clone())?;
    let vnc_password_source = resolve_surf_vnc_password_fort_source(
        &settings.surf,
        vnc_password_fort_key.as_deref(),
        vnc_password_fort_repo.as_deref(),
        vnc_password_fort_env.as_deref(),
    );
    let status = match run_surf_program(&program, args, &settings, &home, &vnc_password_source) {
        Ok(status) => status,
        Err(error)
            if error
                .downcast_ref::<std::io::Error>()
                .is_some_and(|io| io.kind() == std::io::ErrorKind::NotFound)
                && !build
                && !no_build
                && !explicit_program =>
        {
            let fallback = resolve_surf_build_fallback(&settings.surf)?;
            run_surf_program(&fallback, args, &settings, &home, &vnc_password_source)
                .with_context(|| format!("run surf wrapper command via {}", fallback.display()))?
        }
        Err(error) => {
            return Err(error)
                .with_context(|| format!("run surf wrapper command via {}", program.display()));
        }
    };
    if status.success() {
        return Ok(());
    }
    std::process::exit(status.code().unwrap_or(1));
}

fn run_surf_program(
    program: &Path,
    args: &[String],
    settings: &Settings,
    home: &Path,
    vnc_password_source: &Option<SurfVncPasswordFortSource>,
) -> Result<ExitStatus> {
    let mut command = StdCommand::new(program);
    command.env("SI_SURF_WRAPPED", "1").args(args);
    if let Some(password) = resolve_surf_vnc_password(settings, home, vnc_password_source, args)? {
        command.env("SURF_VNC_PASSWORD", password);
    }
    Ok(command.status()?)
}

fn resolve_surf_vnc_password(
    settings: &Settings,
    home: &Path,
    source: &Option<SurfVncPasswordFortSource>,
    args: &[String],
) -> Result<Option<String>> {
    if !surf_command_starts_browser(args)
        || surf_args_include_option(args, "--vnc-password")
        || std::env::var_os("SURF_VNC_PASSWORD").is_some()
    {
        return Ok(None);
    }
    let Some(source) = source.as_ref() else {
        return Ok(None);
    };
    let secret = run_fort_get_secret(settings, home, &source.repo, &source.env_name, &source.key)?;
    let secret = secret.trim();
    if secret.is_empty() {
        anyhow::bail!(
            "Fort secret {} for si surf noVNC password resolved empty; set a non-empty value",
            source.key
        );
    }
    Ok(Some(secret.to_owned()))
}

fn resolve_surf_vnc_password_fort_source(
    settings: &SurfSettings,
    key: Option<&str>,
    repo: Option<&str>,
    env_name: Option<&str>,
) -> Option<SurfVncPasswordFortSource> {
    let key = key
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .or_else(|| non_empty_str(&settings.vnc_password_fort_key))?;
    let repo = repo
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .or_else(|| non_empty_str(&settings.vnc_password_fort_repo))
        .unwrap_or("surf");
    let env_name = env_name
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .or_else(|| non_empty_str(&settings.vnc_password_fort_env))
        .unwrap_or("dev");
    Some(SurfVncPasswordFortSource {
        key: key.to_owned(),
        repo: repo.to_owned(),
        env_name: env_name.to_owned(),
    })
}

fn surf_command_starts_browser(args: &[String]) -> bool {
    matches!(args.first().map(String::as_str), Some("start"))
}

fn surf_args_include_option(args: &[String], flag: &str) -> bool {
    args.iter().any(|arg| arg == flag || arg.starts_with(&format!("{flag}=")))
}

fn run_fort_get_secret(
    settings: &Settings,
    home: &Path,
    repo: &str,
    env_name: &str,
    key: &str,
) -> Result<String> {
    let args = vec![
        "get".to_owned(),
        "--repo".to_owned(),
        repo.to_owned(),
        "--env".to_owned(),
        env_name.to_owned(),
        "--key".to_owned(),
        key.to_owned(),
        "--format".to_owned(),
        "raw".to_owned(),
    ];
    let explicit_program = settings.fort.bin.is_some();
    let program = resolve_fort_program(&settings.fort, None, false, false, None)?;
    match run_fort_capture_stdout(&program, &args, settings, home) {
        Ok(stdout) => Ok(stdout),
        Err(error)
            if error
                .downcast_ref::<std::io::Error>()
                .is_some_and(|io| io.kind() == std::io::ErrorKind::NotFound)
                && !settings.fort.build.unwrap_or(false)
                && !explicit_program =>
        {
            let fallback = resolve_fort_build_fallback(&settings.fort)?;
            run_fort_capture_stdout(&fallback, &args, settings, home)
                .with_context(|| format!("run fort get via {}", fallback.display()))
        }
        Err(error) => Err(error).with_context(|| format!("run fort get via {}", program.display())),
    }
}

fn run_fort_capture_stdout(
    program: &Path,
    args: &[String],
    settings: &Settings,
    home: &Path,
) -> Result<String> {
    let mut command = StdCommand::new(program);
    command.args(build_fort_command_args(program, args, settings, home)?);
    command.env_remove("FORT_HOST");
    command.env_remove("FORT_SETTINGS_FILE");
    command.env_remove("FORT_TOKEN_PATH");
    command.env_remove("FORT_BOOTSTRAP_TOKEN_FILE");
    command.env_remove("FORT_REFRESH_TOKEN_PATH");
    command.env_remove("FORT_TOKEN");
    command.env_remove("FORT_REFRESH_TOKEN");
    let output = command.output()?;
    if output.status.success() {
        return String::from_utf8(output.stdout).context("decode fort stdout");
    }
    let stderr = String::from_utf8_lossy(&output.stderr).trim().to_owned();
    if stderr.is_empty() {
        anyhow::bail!("fort command failed with status {}", output.status);
    }
    anyhow::bail!(stderr);
}

fn run_viva_wrapper(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    repo: Option<PathBuf>,
    build: bool,
    no_build: bool,
    bin: Option<PathBuf>,
    args: Vec<String>,
) -> Result<()> {
    let args = strip_wrapper_passthrough_marker(args);
    if args.is_empty() || matches!(args[0].as_str(), "-h" | "--help" | "help") {
        render_viva_wrapper_help();
        return Ok(());
    }
    match args[0].as_str() {
        "config" => {
            run_viva_config_command(home, settings_file, args.into_iter().skip(1).collect())
        }
        _ => run_native_viva_command(home, settings_file, repo, build, no_build, bin, &args),
    }
}

fn render_viva_wrapper_help() {
    print_help_usage("si viva [WRAPPER_OPTIONS] <COMMAND> [ARGS...]");
    println!();
    print_help_section("Wrapper commands");
    print_help_item("config show|set", CliTone::Command);
    print_help_item("config tunnel show|import|default", CliTone::Command);
    println!();
    print_help_section("Native viva passthrough");
    print_help_item(
        "doctor | deploy | backup ... | notify ... | rollback | status | history | serve | tunnel ...",
        CliTone::Command,
    );
    println!();
    print_help_section("Wrapper options");
    print_help_item("--home <PATH>", CliTone::Flag);
    print_help_item("--settings-file <PATH>", CliTone::Flag);
    print_help_item("--repo <PATH>", CliTone::Flag);
    print_help_item("--build", CliTone::Flag);
    print_help_item("--no-build", CliTone::Flag);
    print_help_item("--bin <PATH>", CliTone::Flag);
    println!();
    print_help_section("Examples");
    print_help_item("si viva config set --repo ~/Development/viva --build true", CliTone::Command);
    print_help_item(
        "si viva config tunnel import --profile dev --file ~/Development/safe/viva/cloudflare.tunnel.dev.toml --set-default",
        CliTone::Command,
    );
    print_help_item("si viva config tunnel show --format json", CliTone::Command);
    print_help_item("si viva -- tunnel up --profile dev", CliTone::Command);
}

fn run_viva_config_command(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    args: Vec<String>,
) -> Result<()> {
    let VivaConfigCli { command } =
        parse_fort_parser(std::iter::once("config".to_owned()).chain(args).collect());
    match command {
        VivaConfigCommand::Show { format } => show_viva_config(home, settings_file, format),
        VivaConfigCommand::Set { repo, bin, build } => {
            set_viva_config(home, settings_file, repo, bin, build)
        }
        VivaConfigCommand::Tunnel { command } => match command {
            VivaTunnelConfigCommand::Show { format } => {
                show_viva_tunnel_config(home, settings_file, format)
            }
            VivaTunnelConfigCommand::Import { profile, file, set_default } => {
                import_viva_tunnel_config(home, settings_file, profile, file, set_default)
            }
            VivaTunnelConfigCommand::Default { profile } => {
                set_viva_tunnel_default_profile(home, settings_file, profile)
            }
        },
    }
}

fn show_viva_config(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let view = VivaConfigView {
        repo: non_empty_string(settings.viva.repo),
        bin: non_empty_string(settings.viva.bin),
        build: settings.viva.build,
        tunnel_default_profile: settings.viva.tunnel.default_profile,
    };
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            print_cli_kv("repo", render_option_text_value(view.repo.as_deref()));
            print_cli_kv("bin", render_option_text_value(view.bin.as_deref()));
            print_cli_kv(
                "build",
                view.build.map(|value| value.to_string()).unwrap_or_else(|| "(none)".to_owned()),
            );
            print_cli_kv(
                "tunnel.default_profile",
                render_option_text_value(non_empty_str(&view.tunnel_default_profile)),
            );
        }
    }
    Ok(())
}

fn set_viva_config(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    repo: Option<String>,
    bin: Option<String>,
    build: Option<bool>,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings_path =
        settings_file.unwrap_or_else(|| home.join(".si").join("viva").join("settings.toml"));
    let mut document = load_settings_document(&settings_path)?;
    let viva = ensure_toml_table(&mut document, "viva")?;
    set_toml_string(viva, "repo", repo);
    set_toml_string(viva, "bin", bin);
    set_toml_bool(viva, "build", build);
    if viva.is_empty() {
        document.remove("viva");
    }
    write_settings_document(&settings_path, &document)?;
    Ok(())
}

fn show_viva_tunnel_config(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&settings.viva.tunnel)?),
        OutputFormat::Text => {
            print_cli_kv(
                "default_profile",
                render_option_text_value(non_empty_str(&settings.viva.tunnel.default_profile)),
            );
            if settings.viva.tunnel.profiles.is_empty() {
                print_cli_kv("profiles", "(none)");
            } else {
                print_cli_kv(
                    "profiles",
                    settings.viva.tunnel.profiles.keys().cloned().collect::<Vec<_>>().join(", "),
                );
            }
        }
    }
    Ok(())
}

fn import_viva_tunnel_config(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    profile: String,
    file: PathBuf,
    set_default: bool,
) -> Result<()> {
    let profile = normalize_viva_tunnel_profile_name(&profile)?;
    let source = fs::read_to_string(&file)
        .with_context(|| format!("read Viva tunnel profile {}", file.display()))?;
    let source_document = toml::from_str::<toml::Value>(&source)
        .with_context(|| format!("parse Viva tunnel profile {}", file.display()))?;
    let profile_value = extract_viva_tunnel_profile_value(&source_document, &profile)
        .with_context(|| format!("find Viva tunnel profile {profile} in {}", file.display()))?;

    let home = home.unwrap_or_else(default_home_dir);
    let settings_path =
        settings_file.unwrap_or_else(|| home.join(".si").join("viva").join("settings.toml"));
    let mut document = load_settings_document(&settings_path)?;
    if !document.contains_key("schema_version") {
        document.insert("schema_version".to_owned(), toml::Value::Integer(1));
    }

    let profiles = ensure_nested_toml_table(&mut document, &["viva", "tunnel", "profiles"])?;
    profiles.insert(profile.clone(), profile_value);
    if set_default {
        let tunnel = ensure_nested_toml_table(&mut document, &["viva", "tunnel"])?;
        tunnel.insert("default_profile".to_owned(), toml::Value::String(profile));
    }

    write_settings_document(&settings_path, &document)?;
    Ok(())
}

fn set_viva_tunnel_default_profile(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    profile: String,
) -> Result<()> {
    let profile = normalize_viva_tunnel_profile_name(&profile)?;
    let home = home.unwrap_or_else(default_home_dir);
    let settings_path =
        settings_file.unwrap_or_else(|| home.join(".si").join("viva").join("settings.toml"));
    let mut document = load_settings_document(&settings_path)?;
    let Some(profiles) = toml_table_at(&document, &["viva", "tunnel", "profiles"]) else {
        anyhow::bail!(
            "cannot set Viva tunnel default profile {profile}: no profiles are configured"
        );
    };
    if !profiles.contains_key(&profile) {
        anyhow::bail!("cannot set Viva tunnel default profile {profile}: profile does not exist");
    }
    let tunnel = ensure_nested_toml_table(&mut document, &["viva", "tunnel"])?;
    tunnel.insert("default_profile".to_owned(), toml::Value::String(profile));
    write_settings_document(&settings_path, &document)?;
    Ok(())
}

fn normalize_viva_tunnel_profile_name(profile: &str) -> Result<String> {
    let normalized = profile.trim().to_lowercase();
    if normalized.is_empty() {
        anyhow::bail!("Viva tunnel profile name cannot be empty");
    }
    Ok(normalized)
}

fn extract_viva_tunnel_profile_value(document: &toml::Value, profile: &str) -> Result<toml::Value> {
    if let Some(profile_table) =
        toml_value_table_at(document, &["viva", "tunnel", "profiles", profile])
    {
        return Ok(toml::Value::Table(profile_table.clone()));
    }
    if let Some(profiles) = toml_value_table_at(document, &["viva", "tunnel", "profiles"]) {
        if profiles.len() == 1 {
            let (_, value) = profiles.iter().next().expect("one profile");
            if value.as_table().is_some() {
                return Ok(value.clone());
            }
        }
    }
    if let Some(root) = document.as_table()
        && looks_like_viva_tunnel_profile(root)
    {
        return Ok(toml::Value::Table(root.clone()));
    }
    anyhow::bail!("expected [viva.tunnel.profiles.{profile}] or a single Viva tunnel profile table")
}

fn looks_like_viva_tunnel_profile(table: &toml::map::Map<String, toml::Value>) -> bool {
    table.contains_key("routes")
        || table.contains_key("runtime_name")
        || table.contains_key("container_name")
        || table.contains_key("tunnel_id_env_key")
        || table.contains_key("credentials_env_key")
        || table.contains_key("fort_env_file")
}

fn run_native_viva_command(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    repo: Option<PathBuf>,
    build: bool,
    no_build: bool,
    bin: Option<PathBuf>,
    args: &[String],
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let explicit_program = bin.is_some() || repo.is_some() || !settings.viva.bin.trim().is_empty();
    let program = resolve_viva_program(&settings.viva, repo.clone(), build, no_build, bin.clone())?;
    let command_args = build_viva_command_args(args, &settings.viva);
    let status = match StdCommand::new(&program).args(&command_args).status() {
        Ok(status) => status,
        Err(error)
            if error.kind() == std::io::ErrorKind::NotFound
                && !build
                && !no_build
                && !explicit_program =>
        {
            let fallback = resolve_viva_build_fallback(&settings.viva)?;
            StdCommand::new(&fallback)
                .args(&command_args)
                .status()
                .with_context(|| format!("run viva wrapper command via {}", fallback.display()))?
        }
        Err(error) => {
            return Err(error)
                .with_context(|| format!("run viva wrapper command via {}", program.display()));
        }
    };
    if status.success() {
        return Ok(());
    }
    std::process::exit(status.code().unwrap_or(1));
}

fn build_viva_command_args(args: &[String], settings: &VivaSettings) -> Vec<String> {
    let mut command_args = args.to_vec();
    if command_args.first().is_some_and(|value| value == "tunnel")
        && !command_args.iter().any(|value| value == "--profile")
        && let Some(default_profile) = non_empty_str(&settings.tunnel.default_profile)
    {
        command_args.push("--profile".to_owned());
        command_args.push(default_profile.to_owned());
    }
    command_args
}

fn resolve_surf_program(
    settings: &SurfSettings,
    repo: Option<PathBuf>,
    build: bool,
    no_build: bool,
    bin: Option<PathBuf>,
) -> Result<PathBuf> {
    resolve_external_tool_program(
        "surf",
        repo,
        non_empty_str(&settings.repo),
        bin,
        non_empty_str(&settings.bin),
        settings.build,
        build,
        no_build,
    )
}

fn resolve_surf_build_fallback(settings: &SurfSettings) -> Result<PathBuf> {
    let repo = resolve_external_tool_repo("surf", None, non_empty_str(&settings.repo))?;
    existing_checkout_binary(&repo, "surf")
        .map(Ok)
        .unwrap_or_else(|| build_external_tool_binary(repo, "surf"))
}

fn resolve_viva_program(
    settings: &VivaSettings,
    repo: Option<PathBuf>,
    build: bool,
    no_build: bool,
    bin: Option<PathBuf>,
) -> Result<PathBuf> {
    resolve_external_tool_program(
        "viva",
        repo,
        non_empty_str(&settings.repo),
        bin,
        non_empty_str(&settings.bin),
        settings.build,
        build,
        no_build,
    )
}

fn resolve_viva_build_fallback(settings: &VivaSettings) -> Result<PathBuf> {
    let repo = resolve_external_tool_repo("viva", None, non_empty_str(&settings.repo))?;
    existing_checkout_binary(&repo, "viva")
        .map(Ok)
        .unwrap_or_else(|| build_external_tool_binary(repo, "viva"))
}

fn resolve_external_tool_program(
    name: &str,
    explicit_repo: Option<PathBuf>,
    configured_repo: Option<&str>,
    explicit_bin: Option<PathBuf>,
    configured_bin: Option<&str>,
    configured_build: Option<bool>,
    build: bool,
    no_build: bool,
) -> Result<PathBuf> {
    if let Some(bin) = explicit_bin {
        return Ok(bin);
    }
    if let Some(bin) = configured_bin {
        return Ok(PathBuf::from(bin));
    }

    let repo = explicit_repo.or_else(|| configured_repo.map(PathBuf::from));
    let should_build = !no_build && (build || configured_build.unwrap_or(false));
    if should_build {
        return build_external_tool_binary(resolve_external_tool_repo(name, repo, None)?, name);
    }

    if let Some(repo) = repo {
        if let Some(binary) = existing_checkout_binary(&repo, name) {
            return Ok(binary);
        }
        if no_build {
            anyhow::bail!(
                "si {name} --no-build could not find {name} binary in {}",
                repo.display()
            );
        }
        return build_external_tool_binary(repo, name);
    }

    Ok(PathBuf::from(name))
}

fn resolve_external_tool_repo(
    name: &str,
    explicit_repo: Option<PathBuf>,
    configured_repo: Option<&str>,
) -> Result<PathBuf> {
    explicit_repo
        .or_else(|| configured_repo.map(PathBuf::from))
        .or_else(|| discover_checkout_repo(name))
        .ok_or_else(|| {
            anyhow!(
                "si {name} build requires [{name}].repo in settings or a sibling {name} checkout"
            )
        })
}

fn discover_checkout_repo(name: &str) -> Option<PathBuf> {
    if let Ok(current_dir) = std::env::current_dir() {
        if let Some(repo) = discover_checkout_repo_from_base(&current_dir, name) {
            return Some(repo);
        }
    }

    if let Ok(current_exe) = std::env::current_exe() {
        if let Some(repo) = discover_checkout_repo_from_base(&current_exe, name) {
            return Some(repo);
        }
    }

    discover_checkout_repo_from_base(Path::new(env!("CARGO_MANIFEST_DIR")), name)
}

fn discover_checkout_repo_from_base(base: &Path, name: &str) -> Option<PathBuf> {
    for anchor in base.ancestors() {
        if anchor.file_name().map(|part| part == name).unwrap_or(false)
            && anchor.join("Cargo.toml").exists()
        {
            return Some(anchor.to_path_buf());
        }

        let sibling = anchor.join(name);
        if sibling.join("Cargo.toml").exists() {
            return Some(sibling);
        }
    }

    None
}

fn existing_checkout_binary(repo: &Path, name: &str) -> Option<PathBuf> {
    let binary_name = if cfg!(windows) { format!("{name}.exe") } else { name.to_owned() };
    for target_dir in checkout_target_dirs(repo) {
        for profile in ["debug", "release"] {
            let candidate = target_dir.join(profile).join(&binary_name);
            if candidate.is_file() {
                return Some(candidate);
            }
        }
    }
    None
}

fn checkout_target_dirs(repo: &Path) -> Vec<PathBuf> {
    if let Some(configured) = configured_checkout_target_dir(repo) {
        return vec![configured];
    }
    vec![repo.join("target")]
}

fn configured_checkout_target_dir(repo: &Path) -> Option<PathBuf> {
    let config_path = repo.join(".cargo").join("config.toml");
    let raw = fs::read_to_string(config_path).ok()?;
    let document = raw.parse::<toml::Table>().ok()?;
    let build = document.get("build")?.as_table()?;
    let value = build.get("target-dir")?.as_str()?.trim();
    if value.is_empty() {
        return None;
    }
    let path = PathBuf::from(value);
    Some(if path.is_absolute() { path } else { repo.join(path) })
}

fn build_external_tool_binary(repo: PathBuf, name: &str) -> Result<PathBuf> {
    let status = StdCommand::new("cargo")
        .arg("build")
        .arg("--quiet")
        .arg("--bin")
        .arg(name)
        .current_dir(&repo)
        .status()
        .with_context(|| format!("build {name} binary in {}", repo.display()))?;
    if !status.success() {
        anyhow::bail!("cargo build --bin {name} failed in {}", repo.display());
    }
    existing_checkout_binary(&repo, name)
        .ok_or_else(|| anyhow!("built {name} binary not found in {}", repo.display()))
}

fn run_fort_wrapper(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    repo: Option<PathBuf>,
    build: bool,
    no_build: bool,
    bin: Option<PathBuf>,
    args: Vec<String>,
) -> Result<()> {
    if args.is_empty() || matches!(args[0].as_str(), "-h" | "--help" | "help") {
        render_fort_wrapper_help();
        return Ok(());
    }

    match args[0].as_str() {
        "session" | "sessionstate" | "session-state" => {
            run_fort_session_state_command(args.into_iter().skip(1).collect())
        }
        "runtime" | "runtimeagentstate" | "runtime-agent-state" => {
            run_fort_runtime_agent_state_command(args.into_iter().skip(1).collect())
        }
        "config" => {
            run_fort_config_command(home, settings_file, args.into_iter().skip(1).collect())
        }
        _ => run_native_fort_command(home, settings_file, repo, build, no_build, bin, &args),
    }
}

fn render_fort_wrapper_help() {
    print_help_usage("si fort [WRAPPER_OPTIONS] <COMMAND> [ARGS...]");
    println!();
    print_help_section("Wrapper commands");
    print_help_item("config show|set", CliTone::Command);
    print_help_item(
        "session <show|write|clear|bootstrap|classify|refresh|teardown>",
        CliTone::Command,
    );
    print_help_item("runtime <show|write|clear>", CliTone::Command);
    println!();
    print_help_section("Native fort passthrough");
    print_help_item(
        "doctor | auth ... | get | set | list | batch-get | run | agent ...",
        CliTone::Command,
    );
    println!();
    print_help_section("Wrapper options");
    print_help_item("--home <PATH>", CliTone::Flag);
    print_help_item("--settings-file <PATH>", CliTone::Flag);
    print_help_item("--repo <PATH>", CliTone::Flag);
    print_help_item("--build", CliTone::Flag);
    print_help_item("--no-build", CliTone::Flag);
    print_help_item("--bin <PATH>", CliTone::Flag);
    println!();
    print_help_section("Examples");
    print_help_item("si fort doctor", CliTone::Command);
    print_help_item("si fort -- --host https://fort.aureuma.ai doctor", CliTone::Command);
    print_help_item("si fort session show --path /tmp/session.json", CliTone::Command);
}

fn strip_wrapper_passthrough_marker(mut args: Vec<String>) -> Vec<String> {
    if args.first().is_some_and(|value| value == "--") {
        args.remove(0);
    }
    args
}

fn non_empty_string(value: String) -> Option<String> {
    let trimmed = value.trim();
    if trimmed.is_empty() { None } else { Some(trimmed.to_owned()) }
}

fn non_empty_str(value: &str) -> Option<&str> {
    let trimmed = value.trim();
    if trimmed.is_empty() { None } else { Some(trimmed) }
}

fn parse_fort_parser<T: Parser>(args: Vec<String>) -> T {
    match T::try_parse_from(args) {
        Ok(parsed) => parsed,
        Err(error) => error.exit(),
    }
}

fn run_fort_session_state_command(args: Vec<String>) -> Result<()> {
    let FortSessionStateCli { command } =
        parse_fort_parser(std::iter::once("session".to_owned()).chain(args).collect());
    match command {
        FortSessionStateCommand::Show { path, format } => show_fort_session_state(path, format),
        FortSessionStateCommand::Write { path, state_json } => {
            write_fort_session_state(path, &state_json)
        }
        FortSessionStateCommand::Clear { path } => clear_fort_session_state(path),
        FortSessionStateCommand::Bootstrap {
            path,
            profile_id,
            access_token_path,
            refresh_token_path,
            access_token_runtime_path,
            refresh_token_runtime_path,
            format,
        } => show_fort_session_bootstrap_view(
            path,
            profile_id,
            &access_token_path,
            &refresh_token_path,
            &access_token_runtime_path,
            &refresh_token_runtime_path,
            format,
        ),
        FortSessionStateCommand::Classify { path, now_unix, format } => {
            show_fort_session_state_classification(path, now_unix, format)
        }
        FortSessionStateCommand::Refresh {
            path,
            outcome,
            now_unix,
            access_expires_at_unix,
            refresh_expires_at_unix,
            format,
        } => show_fort_session_state_refresh_outcome(
            path,
            outcome,
            now_unix,
            access_expires_at_unix,
            refresh_expires_at_unix,
            format,
        ),
        FortSessionStateCommand::Teardown { path, now_unix, format } => {
            show_fort_session_state_teardown(path, now_unix, format)
        }
    }
}

fn run_fort_runtime_agent_state_command(args: Vec<String>) -> Result<()> {
    let FortRuntimeAgentStateCli { command } =
        parse_fort_parser(std::iter::once("runtime".to_owned()).chain(args).collect());
    match command {
        FortRuntimeAgentStateCommand::Show { path, format } => {
            show_fort_runtime_agent_state(path, format)
        }
        FortRuntimeAgentStateCommand::Write { path, state_json } => {
            write_fort_runtime_agent_state(path, &state_json)
        }
        FortRuntimeAgentStateCommand::Clear { path } => clear_fort_runtime_agent_state(path),
    }
}

fn run_fort_config_command(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    args: Vec<String>,
) -> Result<()> {
    let FortConfigCli { command } =
        parse_fort_parser(std::iter::once("config".to_owned()).chain(args).collect());
    match command {
        FortConfigCommand::Show { format } => show_fort_config(home, settings_file, format),
        FortConfigCommand::Set { repo, bin, build, host, runtime_host } => {
            set_fort_config(home, settings_file, repo, bin, build, host, runtime_host)
        }
    }
}

fn show_fort_config(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let view = FortConfigView {
        repo: settings.fort.repo,
        bin: settings.fort.bin,
        build: settings.fort.build,
        host: settings.fort.host,
        runtime_host: settings.fort.runtime_host,
    };

    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            println!("repo={}", render_option_text_value(view.repo.as_deref()));
            println!("bin={}", render_option_text_value(view.bin.as_deref()));
            println!(
                "build={}",
                view.build.map(|value| value.to_string()).unwrap_or_else(|| "(none)".to_owned())
            );
            println!("host={}", render_option_text_value(view.host.as_deref()));
            println!("runtime_host={}", render_option_text_value(view.runtime_host.as_deref()));
        }
    }

    Ok(())
}

fn set_fort_config(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    repo: Option<String>,
    bin: Option<String>,
    build: Option<bool>,
    host: Option<String>,
    runtime_host: Option<String>,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings_path = settings_file.unwrap_or_else(|| home.join(".si").join("settings.toml"));
    let mut document = load_settings_document(&settings_path)?;
    if !document.contains_key("schema_version") {
        document.insert("schema_version".to_owned(), toml::Value::Integer(1));
    }
    let host = normalize_fort_persistent_host_option(host, "fort.host")?;
    let runtime_host = normalize_fort_persistent_host_option(runtime_host, "fort.runtime_host")?;
    let fort = ensure_toml_table(&mut document, "fort")?;
    set_toml_string(fort, "repo", repo);
    set_toml_string(fort, "bin", bin);
    set_toml_bool(fort, "build", build);
    set_toml_string(fort, "host", host);
    set_toml_string(fort, "runtime_host", runtime_host);
    if fort.is_empty() {
        document.remove("fort");
    }
    write_settings_document(&settings_path, &document)?;
    Ok(())
}

fn run_native_fort_command(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    repo: Option<PathBuf>,
    build: bool,
    no_build: bool,
    bin: Option<PathBuf>,
    args: &[String],
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let explicit_program = bin.is_some() || repo.is_some() || settings.fort.bin.is_some();
    let program = resolve_fort_program(&settings.fort, repo.clone(), build, no_build, bin.clone())?;
    let status = match run_fort_program(&program, args, &settings, &home) {
        Ok(status) => status,
        Err(error)
            if error
                .downcast_ref::<std::io::Error>()
                .is_some_and(|io| io.kind() == std::io::ErrorKind::NotFound)
                && !build
                && !no_build
                && !explicit_program =>
        {
            let fallback = resolve_fort_build_fallback(&settings.fort)?;
            run_fort_program(&fallback, args, &settings, &home)
                .with_context(|| format!("run fort wrapper command via {}", fallback.display()))?
        }
        Err(error) => {
            return Err(
                error.context(format!("run fort wrapper command via {}", program.display()))
            );
        }
    };
    if status.success() {
        return Ok(());
    }
    std::process::exit(status.code().unwrap_or(1));
}

fn run_fort_program(
    program: &Path,
    args: &[String],
    settings: &Settings,
    home: &Path,
) -> Result<ExitStatus> {
    let mut command = StdCommand::new(program);
    command.args(build_fort_command_args(program, args, settings, home)?);
    command.env_remove("FORT_HOST");
    command.env_remove("FORT_SETTINGS_FILE");
    command.env_remove("FORT_TOKEN_PATH");
    command.env_remove("FORT_BOOTSTRAP_TOKEN_FILE");
    command.env_remove("FORT_REFRESH_TOKEN_PATH");
    command.env_remove("FORT_TOKEN");
    command.env_remove("FORT_REFRESH_TOKEN");
    Ok(command.status()?)
}

fn build_fort_command_args(
    program: &Path,
    args: &[String],
    settings: &Settings,
    home: &Path,
) -> Result<Vec<String>> {
    let mut command_args = Vec::new();
    guard_fort_profile_refresh_token_rotation(args, settings, home)?;
    let explicit_host = fort_option_value(args, "--host").map(ToOwned::to_owned);
    let resolved_host = if explicit_host.is_some() {
        explicit_host
    } else {
        resolve_configured_fort_public_host(settings, home)?
    };
    if !fort_args_include_option(args, "--host")
        && let Some(host) = resolved_host.as_deref()
    {
        command_args.push("--host".to_owned());
        command_args.push(host.to_owned());
    }
    if !fort_args_include_option(args, "--token-file") {
        if let Some(token_file) =
            resolve_fort_default_token_file(program, args, resolved_host.as_deref(), home)?
        {
            command_args.push("--token-file".to_owned());
            command_args.push(token_file.display().to_string());
        }
    }
    command_args.extend(args.iter().cloned());
    Ok(command_args)
}

#[derive(Clone, Debug)]
struct FortTokenFileAuth {
    token_path: PathBuf,
    refresh_token_path: Option<PathBuf>,
    refresh_lock_path: PathBuf,
    label: String,
    scope: FortAuthScope,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum FortAuthScope {
    Runtime,
    BootstrapAdmin,
}

#[derive(Debug)]
struct FortApiClient {
    host: String,
    http: BlockingHttpClient,
}

impl FortApiClient {
    fn new(host: String) -> Result<Self> {
        Ok(Self {
            host,
            http: BlockingHttpClient::builder()
                .timeout(std::time::Duration::from_secs(15))
                .build()
                .context("build Fort HTTP client")?,
        })
    }

    fn request(
        &self,
        method: Method,
        path: &str,
        body: Option<Value>,
        bearer_token: Option<&str>,
    ) -> Result<(u16, Value)> {
        let mut request = self.http.request(method, format!("{}{}", self.host, path));
        if let Some(token) = bearer_token.map(str::trim).filter(|value| !value.is_empty()) {
            request = request.bearer_auth(token);
        }
        if let Some(body) = body {
            request = request.json(&body);
        }
        let response = request.send().context("send Fort API request")?;
        let status = response.status().as_u16();
        let body = response.json::<Value>().unwrap_or_else(|_| json!({}));
        Ok((status, body))
    }
}

const FORT_ACCESS_TOKEN_REFRESH_SKEW_SECONDS: i64 = 300;
const FORT_REFRESH_LOCK_STALE_AFTER_SECONDS: u64 = 120;
const CODEX_PROFILE_FORT_REFRESH_TTL: &str = "30d";

fn fort_args_include_option(args: &[String], flag: &str) -> bool {
    args.iter().any(|arg| arg == flag || arg.starts_with(&format!("{flag}=")))
}

fn fort_option_value<'a>(args: &'a [String], flag: &str) -> Option<&'a str> {
    let mut iter = args.iter();
    while let Some(arg) = iter.next() {
        if arg == flag {
            return iter.next().map(String::as_str);
        }
        let prefix = format!("{flag}=");
        if let Some(value) = arg.strip_prefix(&prefix) {
            return Some(value);
        }
    }
    None
}

fn fort_command_tokens(args: &[String]) -> Vec<&str> {
    let mut tokens = Vec::new();
    let mut index = 0usize;
    while index < args.len() {
        let arg = args[index].as_str();
        if arg == "--" {
            index += 1;
            continue;
        }
        if arg == "--host" || arg == "--token-file" {
            index += 2;
            continue;
        }
        if arg.starts_with("--host=") || arg.starts_with("--token-file=") || arg == "--json" {
            index += 1;
            continue;
        }
        if arg.starts_with('-') {
            index += 1;
            continue;
        }
        tokens.push(arg);
        index += 1;
    }
    tokens
}

fn should_auto_refresh_fort_access_token(args: &[String], scope: FortAuthScope) -> bool {
    let tokens = fort_command_tokens(args);
    if tokens.is_empty() {
        return false;
    }
    if matches!(tokens.first(), Some(&"version")) {
        return false;
    }
    if tokens.first() == Some(&"auth") && tokens.get(1) == Some(&"session") {
        return false;
    }
    if scope == FortAuthScope::BootstrapAdmin && tokens.first() == Some(&"doctor") {
        return false;
    }
    true
}

fn resolve_fort_default_token_file(
    program: &Path,
    args: &[String],
    resolved_host: Option<&str>,
    home: &Path,
) -> Result<Option<PathBuf>> {
    if let Some(codex_home) = std::env::var_os("CODEX_HOME")
        .map(PathBuf::from)
        .filter(|path| !path.as_os_str().is_empty())
    {
        let profile_dir = codex_home.join("fort");
        if let Some(auth) =
            resolve_fort_runtime_auth_from_dir(profile_dir.clone(), "CODEX_HOME Fort session")
        {
            return resolve_required_fort_auth(program, args, resolved_host, &auth);
        }
        if fort_command_prefers_runtime_auth(args) {
            anyhow::bail!(
                "Fort runtime session is required for CODEX_HOME={}, but no usable session files were found at {}; run `si codex spawn --profile <profile>` to provision it",
                codex_home.display(),
                profile_dir.display()
            );
        }
    }
    if fort_command_requires_runtime_auth(args) {
        anyhow::bail!(
            "Fort runtime session is required; run through `si codex shell <profile> -- si fort ...` or set CODEX_HOME to a managed Codex profile home"
        );
    }
    if fort_command_allows_bootstrap_auth(args) {
        let auth = resolve_fort_bootstrap_auth(home).ok_or_else(|| {
            anyhow!(
                "Fort bootstrap admin session is required for this admin command; provision ~/.si/fort/bootstrap/admin.token and admin.refresh.token explicitly"
            )
        })?;
        return resolve_required_fort_auth(program, args, resolved_host, &auth);
    }
    Ok(None)
}

fn guard_fort_profile_refresh_token_rotation(
    args: &[String],
    settings: &Settings,
    home: &Path,
) -> Result<()> {
    let tokens = fort_command_tokens(args);
    if tokens.first() != Some(&"auth")
        || tokens.get(1) != Some(&"session")
        || tokens.get(2) != Some(&"refresh")
    {
        return Ok(());
    }
    let Some(refresh_token_file) = fort_option_value(args, "--refresh-token-file") else {
        return Ok(());
    };
    let refresh_path = normalize_fort_path_for_compare(home, Path::new(refresh_token_file));
    if !is_codex_profile_refresh_token_path(&refresh_path, settings, home) {
        return Ok(());
    }
    let out_path = fort_option_value(args, "--refresh-token-out")
        .map(|value| normalize_fort_path_for_compare(home, Path::new(value)))
        .unwrap_or_else(|| refresh_path.clone());
    if out_path != refresh_path {
        anyhow::bail!(
            "refusing to rotate Codex profile Fort refresh token {} into {}; profile refresh tokens must be refreshed in place",
            refresh_path.display(),
            out_path.display()
        );
    }
    Ok(())
}

fn normalize_fort_path_for_compare(home: &Path, path: &Path) -> PathBuf {
    let base = if path.is_absolute() { PathBuf::new() } else { home.to_path_buf() };
    normalize_path_components(base.join(path))
}

fn normalize_path_components(path: PathBuf) -> PathBuf {
    use std::path::Component;
    let mut normalized = PathBuf::new();
    for component in path.components() {
        match component {
            Component::CurDir => {}
            Component::ParentDir => {
                normalized.pop();
            }
            _ => normalized.push(component.as_os_str()),
        }
    }
    normalized
}

fn is_codex_profile_refresh_token_path(path: &Path, settings: &Settings, home: &Path) -> bool {
    let profiles_dir =
        normalize_path_components(SiPaths::from_settings(home, settings).codex_profiles_dir);
    let Ok(relative_path) = path.strip_prefix(&profiles_dir) else {
        return false;
    };
    let parts = relative_path
        .components()
        .filter_map(|component| match component {
            std::path::Component::Normal(value) => value.to_str(),
            _ => None,
        })
        .collect::<Vec<_>>();
    (parts.len() == 3 && parts[1] == "fort" && parts[2] == "refresh.token")
        || (parts.len() == 5
            && parts[1] == "workers"
            && parts[3] == "fort"
            && parts[4] == "refresh.token")
}

fn resolve_required_fort_auth(
    program: &Path,
    args: &[String],
    resolved_host: Option<&str>,
    auth: &FortTokenFileAuth,
) -> Result<Option<PathBuf>> {
    match refresh_fort_access_token(program, args, resolved_host, auth)? {
        Some(path) => Ok(Some(path)),
        None => Err(anyhow!(
            "{} is configured, but neither {} nor its refresh token is usable",
            auth.label,
            auth.token_path.display()
        )),
    }
}

fn fort_command_requires_runtime_auth(args: &[String]) -> bool {
    let tokens = fort_command_tokens(args);
    matches!(
        tokens.as_slice(),
        ["get" | "set" | "list" | "batch-get" | "run", ..] | ["auth", "whoami", ..]
    )
}

fn fort_command_prefers_runtime_auth(args: &[String]) -> bool {
    let tokens = fort_command_tokens(args);
    match tokens.as_slice() {
        ["doctor"] => true,
        _ => fort_command_requires_runtime_auth(args),
    }
}

fn fort_command_allows_bootstrap_auth(args: &[String]) -> bool {
    let tokens = fort_command_tokens(args);
    matches!(
        tokens.as_slice(),
        ["agent", ..]
            | ["auth", "issue" | "login" | "list" | "revoke", ..]
            | ["auth", "session", "open", ..]
    )
}

fn resolve_fort_bootstrap_auth(home: &Path) -> Option<FortTokenFileAuth> {
    let token_path = default_fort_bootstrap_token_path(home);
    let refresh_token_path = default_fort_bootstrap_refresh_token_path(home);
    if token_path.is_file() || refresh_token_path.is_file() {
        return Some(FortTokenFileAuth {
            token_path,
            refresh_token_path: refresh_token_path.is_file().then_some(refresh_token_path.clone()),
            refresh_lock_path: default_fort_bootstrap_refresh_lock_path(home),
            label: "bootstrap admin Fort session".to_owned(),
            scope: FortAuthScope::BootstrapAdmin,
        });
    }
    None
}

fn resolve_fort_runtime_auth_from_dir(
    profile_dir: PathBuf,
    label: impl Into<String>,
) -> Option<FortTokenFileAuth> {
    let session_path = profile_dir.join("session.json");
    let default_token_path = profile_dir.join("access.token");
    let default_refresh_path = profile_dir.join("refresh.token");
    let state = fs::read_to_string(&session_path)
        .ok()
        .and_then(|raw| serde_json::from_str::<PersistedSessionState>(&raw).ok())
        .map(|value| value.normalized());
    let token_path = state
        .as_ref()
        .and_then(|value| {
            let path = value.access_token_path.trim();
            (!path.is_empty()).then(|| PathBuf::from(path))
        })
        .unwrap_or(default_token_path);
    let refresh_token_path = state
        .as_ref()
        .and_then(|value| {
            let path = value.refresh_token_path.trim();
            (!path.is_empty()).then(|| PathBuf::from(path))
        })
        .unwrap_or(default_refresh_path);
    if token_path.is_file() || refresh_token_path.is_file() {
        return Some(FortTokenFileAuth {
            token_path,
            refresh_token_path: refresh_token_path.is_file().then_some(refresh_token_path),
            refresh_lock_path: profile_dir.join("runtime.lock"),
            label: label.into(),
            scope: FortAuthScope::Runtime,
        });
    }
    None
}

fn ensure_fort_secret_dir(dir: &Path) -> Result<()> {
    fs::create_dir_all(dir)
        .with_context(|| format!("create Fort profile dir {}", dir.display()))?;
    #[cfg(unix)]
    fs::set_permissions(dir, fs::Permissions::from_mode(0o700))
        .with_context(|| format!("chmod {}", dir.display()))?;
    Ok(())
}

fn resolve_configured_fort_runtime_host(
    settings: &Settings,
    fallback_host: &str,
) -> Result<String> {
    if let Some(host) =
        settings.fort.runtime_host.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        return normalize_fort_public_https_host(host, "fort.runtime_host");
    }
    Ok(fallback_host.trim().to_owned())
}

fn fort_api_status_error(status: u16, body: &Value, action: &str) -> anyhow::Error {
    let message = body.get("error").and_then(Value::as_str).unwrap_or("Fort request failed");
    anyhow!("{action} failed (status={status}): {message}")
}

fn ensure_fort_api_status(status: u16, expected: u16, body: &Value, action: &str) -> Result<()> {
    if status == expected { Ok(()) } else { Err(fort_api_status_error(status, body, action)) }
}

fn fort_bootstrap_admin_refresh_repair_message(
    token_path: &Path,
    refresh_token_path: &Path,
    detail: &str,
) -> String {
    format!(
        "{detail}. Fort bootstrap admin refresh token {} is not accepted by Fort, so bootstrap admin access token {} cannot be refreshed. This usually means the host Fort bootstrap admin session expired, was rotated, or was revoked by a break-glass reissue. Reissue Fort break-glass/admin bootstrap auth on the Fort host using production Fort state/signing material, then open a new file-backed admin session and replace both bootstrap files. SI did not fall back to any local bypass.",
        refresh_token_path.display(),
        token_path.display(),
    )
}

fn fort_bootstrap_admin_refresh_error(
    status: u16,
    body: &Value,
    token_path: &Path,
    refresh_token_path: &Path,
) -> anyhow::Error {
    let detail =
        fort_api_status_error(status, body, "refresh Fort bootstrap admin session").to_string();
    if matches!(status, 401 | 403) {
        anyhow!(fort_bootstrap_admin_refresh_repair_message(
            token_path,
            refresh_token_path,
            &detail,
        ))
    } else {
        anyhow!(detail)
    }
}

fn fort_path_escape(value: &str) -> String {
    url::form_urlencoded::byte_serialize(value.as_bytes()).collect::<String>()
}

fn read_fort_token_file(path: &Path, label: &str) -> Result<String> {
    let token =
        fs::read_to_string(path).with_context(|| format!("read {label} {}", path.display()))?;
    let token = token.trim().to_owned();
    if token.is_empty() {
        anyhow::bail!("{label} {} is empty", path.display());
    }
    Ok(token)
}

fn refresh_bootstrap_admin_token_for_fort_provisioning(
    client: &FortApiClient,
    home: &Path,
) -> Result<String> {
    let refresh_lock_path = default_fort_bootstrap_refresh_lock_path(home);
    with_fort_bootstrap_refresh_lock(&refresh_lock_path, || {
        let token_path = default_fort_bootstrap_token_path(home);
        if fort_access_token_is_fresh(&token_path) {
            return read_fort_token_file(&token_path, "Fort bootstrap admin access token");
        }

        let refresh_token_path = default_fort_bootstrap_refresh_token_path(home);
        if !refresh_token_path.is_file() {
            anyhow::bail!(
                "Fort profile provisioning requires a bootstrap admin session, but {} is missing or stale and {} is missing; rebootstrap Fort admin auth first",
                token_path.display(),
                refresh_token_path.display()
            );
        }
        let refresh_token =
            read_fort_token_file(&refresh_token_path, "Fort bootstrap admin refresh token")?;
        let (status, response) = client.request(
            Method::POST,
            "/v1/auth/session/refresh",
            Some(json!({ "refresh_token": refresh_token })),
            None,
        )?;
        if status != 200 {
            return Err(fort_bootstrap_admin_refresh_error(
                status,
                &response,
                &token_path,
                &refresh_token_path,
            ));
        }
        let access_token = response["access_token"].as_str().unwrap_or_default().trim();
        let next_refresh_token = response["refresh_token"].as_str().unwrap_or_default().trim();
        if access_token.is_empty() || next_refresh_token.is_empty() {
            anyhow::bail!(
                "Fort bootstrap refresh response is missing access_token or refresh_token"
            );
        }
        write_secret_text_file(&token_path, access_token)?;
        write_secret_text_file(&refresh_token_path, next_refresh_token)?;
        Ok(access_token.to_owned())
    })
}

fn fort_profile_session_is_reusable(
    paths: &CodexProfileFortSessionPaths,
    profile_id: &str,
    agent_id: &str,
) -> Result<bool> {
    if !paths.session_path.is_file()
        || !paths.access_token_path.is_file()
        || !paths.refresh_token_path.is_file()
    {
        return Ok(false);
    }
    let state = load_persisted_session_state(&paths.session_path)
        .with_context(|| format!("load Fort session state {}", paths.session_path.display()))?;
    let state = state.normalized();
    if state.profile_id != profile_id || state.agent_id != agent_id {
        return Ok(false);
    }
    match classify_persisted_session_state(&state, Utc::now().timestamp())
        .with_context(|| format!("classify Fort session state {}", paths.session_path.display()))?
    {
        SessionState::Resumable(_) | SessionState::Refreshing(_) => Ok(true),
        SessionState::BootstrapRequired
        | SessionState::Revoked { .. }
        | SessionState::TeardownPending(_)
        | SessionState::Closed => Ok(false),
    }
}

fn ensure_fort_profile_agent(
    client: &FortApiClient,
    admin_token: &str,
    agent_id: &str,
) -> Result<()> {
    let escaped = fort_path_escape(agent_id);
    let (status, response) =
        client.request(Method::GET, &format!("/v1/agents/{escaped}"), None, Some(admin_token))?;
    match status {
        200 => {
            if response["status"].as_str() == Some("disabled") {
                let (status, response) = client.request(
                    Method::POST,
                    &format!("/v1/agents/{escaped}/enable"),
                    Some(json!({})),
                    Some(admin_token),
                )?;
                ensure_fort_api_status(status, 200, &response, "enable Fort profile agent")?;
            }
            Ok(())
        }
        404 => {
            let (status, response) = client.request(
                Method::POST,
                "/v1/agents",
                Some(json!({ "id": agent_id, "type": "workload", "status": "active" })),
                Some(admin_token),
            )?;
            ensure_fort_api_status(status, 201, &response, "create Fort profile agent")
        }
        _ => Err(fort_api_status_error(status, &response, "load Fort profile agent")),
    }
}

fn ensure_fort_profile_policy(
    client: &FortApiClient,
    admin_token: &str,
    agent_id: &str,
) -> Result<()> {
    let escaped = fort_path_escape(agent_id);
    let (status, response) = client.request(
        Method::PUT,
        &format!("/v1/agents/{escaped}/policy"),
        Some(json!({
            "bindings": [{
                "repo": "*",
                "env": "*",
                "ops": ["*"]
            }]
        })),
        Some(admin_token),
    )?;
    ensure_fort_api_status(status, 200, &response, "set Fort profile policy")
}

fn open_fort_profile_session(
    client: &FortApiClient,
    admin_token: &str,
    profile_id: &str,
    agent_id: &str,
    host: &str,
    runtime_host: &str,
    paths: &CodexProfileFortSessionPaths,
) -> Result<()> {
    let (status, response) = client.request(
        Method::POST,
        "/v1/auth/session/open",
        Some(json!({
            "agent_id": agent_id,
            "aud": "fort-api",
            "refresh_ttl": CODEX_PROFILE_FORT_REFRESH_TTL
        })),
        Some(admin_token),
    )?;
    ensure_fort_api_status(status, 200, &response, "open Fort profile session")?;
    let access_token = response["access_token"].as_str().unwrap_or_default().trim();
    let refresh_token = response["refresh_token"].as_str().unwrap_or_default().trim();
    let session_id = response["session_id"].as_str().unwrap_or_default().trim();
    if access_token.is_empty() || refresh_token.is_empty() || session_id.is_empty() {
        anyhow::bail!(
            "Fort session open response is missing access_token, refresh_token, or session_id"
        );
    }
    write_secret_text_file(&paths.access_token_path, access_token)?;
    write_secret_text_file(&paths.refresh_token_path, refresh_token)?;
    save_persisted_session_state(
        &paths.session_path,
        &PersistedSessionState {
            profile_id: profile_id.to_owned(),
            agent_id: agent_id.to_owned(),
            session_id: session_id.to_owned(),
            host: host.to_owned(),
            runtime_host: runtime_host.to_owned(),
            access_token_path: paths.access_token_path.display().to_string(),
            refresh_token_path: paths.refresh_token_path.display().to_string(),
            access_expires_at: response["access_expires_at"]
                .as_str()
                .unwrap_or_default()
                .trim()
                .to_owned(),
            refresh_expires_at: response["refresh_expires_at"]
                .as_str()
                .unwrap_or_default()
                .trim()
                .to_owned(),
            updated_at: Utc::now().to_rfc3339_opts(chrono::SecondsFormat::Secs, true),
        },
    )
    .with_context(|| format!("write Fort session state {}", paths.session_path.display()))?;
    Ok(())
}

fn ensure_codex_profile_fort_session(
    home: &Path,
    settings: &Settings,
    codex_home: &Path,
    profile_id: &str,
    worker_slot: &str,
) -> Result<CodexProfileFortSessionPaths> {
    let paths = codex_profile_fort_session_paths(codex_home);
    ensure_fort_secret_dir(&paths.dir)?;
    let _lock = acquire_session_lock(&paths.lock_path)
        .with_context(|| format!("lock Fort profile session {}", paths.lock_path.display()))?;
    let agent_id = codex_fort_agent_id(profile_id, worker_slot);
    if fort_profile_session_is_reusable(&paths, profile_id, &agent_id)? {
        ensure_fort_profile_policy_best_effort(home, settings, profile_id, &agent_id);
        return Ok(paths);
    }

    let host = resolve_configured_fort_public_host(settings, home)?.ok_or_else(|| {
        anyhow!("fort.host must be configured to provision Codex profile Fort sessions")
    })?;
    let runtime_host = resolve_configured_fort_runtime_host(settings, &host)?;
    let client = FortApiClient::new(host.clone())?;
    let admin_token = refresh_bootstrap_admin_token_for_fort_provisioning(&client, home)?;
    ensure_fort_profile_agent(&client, &admin_token, &agent_id)?;
    ensure_fort_profile_policy(&client, &admin_token, &agent_id)?;
    open_fort_profile_session(
        &client,
        &admin_token,
        profile_id,
        &agent_id,
        &host,
        &runtime_host,
        &paths,
    )?;
    Ok(paths)
}

fn ensure_fort_profile_policy_best_effort(
    home: &Path,
    settings: &Settings,
    profile_id: &str,
    agent_id: &str,
) {
    let host = match resolve_configured_fort_public_host(settings, home) {
        Ok(Some(host)) => host,
        Ok(None) => return,
        Err(error) => {
            eprintln!(
                "WARNING: skipping Fort policy sync for Codex profile {profile_id}: resolve fort host failed: {error}"
            );
            return;
        }
    };

    let client = match FortApiClient::new(host) {
        Ok(client) => client,
        Err(error) => {
            eprintln!(
                "WARNING: skipping Fort policy sync for Codex profile {profile_id}: build Fort API client failed: {error}"
            );
            return;
        }
    };

    let admin_token = match refresh_bootstrap_admin_token_for_fort_provisioning(&client, home) {
        Ok(token) => token,
        Err(error) => {
            eprintln!(
                "WARNING: skipping Fort policy sync for Codex profile {profile_id}: bootstrap admin token unavailable: {error}"
            );
            return;
        }
    };

    if let Err(error) = ensure_fort_profile_agent(&client, &admin_token, agent_id) {
        eprintln!(
            "WARNING: skipping Fort policy sync for Codex profile {profile_id}: ensure agent {agent_id} failed: {error}"
        );
        return;
    }
    if let Err(error) = ensure_fort_profile_policy(&client, &admin_token, agent_id) {
        eprintln!(
            "WARNING: skipping Fort policy sync for Codex profile {profile_id}: ensure policy for agent {agent_id} failed: {error}"
        );
    }
}

fn refresh_fort_access_token(
    program: &Path,
    args: &[String],
    resolved_host: Option<&str>,
    auth: &FortTokenFileAuth,
) -> Result<Option<PathBuf>> {
    if !should_auto_refresh_fort_access_token(args, auth.scope) {
        return Ok(auth.token_path.is_file().then_some(auth.token_path.clone()));
    }
    let token_path = &auth.token_path;
    let Some(refresh_token_path) = auth.refresh_token_path.as_ref() else {
        return Ok(token_path.is_file().then_some(token_path.clone()));
    };
    if !refresh_token_path.is_file() {
        return Ok(token_path.is_file().then_some(token_path.clone()));
    }
    if fort_access_token_is_fresh(token_path) {
        return Ok(Some(token_path.clone()));
    }
    with_fort_bootstrap_refresh_lock(&auth.refresh_lock_path, || {
        if fort_access_token_is_fresh(token_path) {
            return Ok(());
        }
        let mut refresh_command = StdCommand::new(program);
        if let Some(host) = resolved_host {
            refresh_command.arg("--host").arg(host);
        }
        refresh_command
            .arg("--json")
            .arg("auth")
            .arg("session")
            .arg("refresh")
            .arg("--refresh-token-file")
            .arg(refresh_token_path)
            .arg("--refresh-token-out")
            .arg(refresh_token_path);
        refresh_command.env_remove("FORT_HOST");
        refresh_command.env_remove("FORT_SETTINGS_FILE");
        refresh_command.env_remove("FORT_TOKEN_PATH");
        refresh_command.env_remove("FORT_BOOTSTRAP_TOKEN_FILE");
        refresh_command.env_remove("FORT_REFRESH_TOKEN_PATH");
        refresh_command.env_remove("FORT_TOKEN");
        refresh_command.env_remove("FORT_REFRESH_TOKEN");
        let output = refresh_command
            .output()
            .with_context(|| format!("refresh fort session via {}", program.display()))?;
        if !output.status.success() {
            let stderr = String::from_utf8_lossy(&output.stderr).trim().to_owned();
            let stdout = String::from_utf8_lossy(&output.stdout).trim().to_owned();
            let detail = if !stderr.is_empty() {
                stderr
            } else if !stdout.is_empty() {
                stdout
            } else {
                format!("exit status {}", output.status)
            };
            if auth.label == "bootstrap admin Fort session"
                && (detail.contains("status=401") || detail.contains("status=403"))
            {
                anyhow::bail!(
                    "{}",
                    fort_bootstrap_admin_refresh_repair_message(
                        token_path,
                        refresh_token_path,
                        &detail,
                    )
                );
            }
            if auth.scope == FortAuthScope::Runtime
                && (detail.contains("status=401") || detail.contains("status=403"))
            {
                persist_revoked_fort_runtime_session(auth)
                    .context("persist revoked Fort runtime session state")?;
            }
            anyhow::bail!("refresh fort session: {detail}");
        }
        let payload: Value = serde_json::from_slice(&output.stdout)
            .context("parse fort session refresh response")?;
        let access_token = payload["access_token"].as_str().unwrap_or_default().trim();
        if access_token.is_empty() {
            anyhow::bail!("fort session refresh response missing access_token");
        }
        write_secret_text_file(token_path, access_token)?;
        Ok(())
    })?;
    Ok(Some(token_path.clone()))
}

fn persist_revoked_fort_runtime_session(auth: &FortTokenFileAuth) -> Result<()> {
    let Some(session_dir) = auth.token_path.parent() else {
        return Ok(());
    };
    let session_path = session_dir.join("session.json");
    if !session_path.is_file() {
        return Ok(());
    }
    let state = load_persisted_session_state(&session_path)
        .with_context(|| format!("load Fort session state {}", session_path.display()))?;
    let transition = apply_refresh_outcome_to_persisted_session_state(
        &state,
        RefreshOutcome::Unauthorized,
        Utc::now().timestamp(),
    )
    .with_context(|| format!("update Fort session state {}", session_path.display()))?;
    save_persisted_session_state(&session_path, &transition.state)
        .with_context(|| format!("write Fort session state {}", session_path.display()))?;
    Ok(())
}

fn fort_access_token_is_fresh(path: &Path) -> bool {
    let Ok(raw) = fs::read_to_string(path) else {
        return false;
    };
    let token = raw.trim();
    let Some(payload) = token.split('.').nth(1) else {
        return false;
    };
    let Ok(decoded) = URL_SAFE_NO_PAD.decode(payload.as_bytes()) else {
        return false;
    };
    let Ok(claims) = serde_json::from_slice::<Value>(&decoded) else {
        return false;
    };
    let Some(exp) = claims["exp"].as_i64() else {
        return false;
    };
    exp > Utc::now().timestamp() + FORT_ACCESS_TOKEN_REFRESH_SKEW_SECONDS
}

fn with_fort_bootstrap_refresh_lock<T>(
    lock_path: &Path,
    action: impl FnOnce() -> Result<T>,
) -> Result<T> {
    let parent = lock_path
        .parent()
        .ok_or_else(|| anyhow!("refresh lock path {} has no parent", lock_path.display()))?;
    fs::create_dir_all(parent).with_context(|| format!("create dir {}", parent.display()))?;
    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(15);
    loop {
        match std::fs::OpenOptions::new().write(true).create_new(true).open(lock_path) {
            Ok(_) => break,
            Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => {
                if remove_stale_fort_refresh_lock(lock_path)? {
                    continue;
                }
                if std::time::Instant::now() >= deadline {
                    return Err(anyhow!(
                        "timed out waiting for fort bootstrap refresh lock {}",
                        lock_path.display()
                    ));
                }
                std::thread::sleep(std::time::Duration::from_millis(100));
            }
            Err(error) => {
                return Err(error)
                    .with_context(|| format!("create fort refresh lock {}", lock_path.display()));
            }
        }
    }
    struct RefreshLockGuard(PathBuf);
    impl Drop for RefreshLockGuard {
        fn drop(&mut self) {
            let _ = fs::remove_file(&self.0);
        }
    }
    let _guard = RefreshLockGuard(lock_path.to_path_buf());
    action()
}

fn remove_stale_fort_refresh_lock(lock_path: &Path) -> Result<bool> {
    let Ok(metadata) = fs::metadata(lock_path) else {
        return Ok(false);
    };
    let Ok(modified) = metadata.modified() else {
        return Ok(false);
    };
    let Ok(age) = modified.elapsed() else {
        return Ok(false);
    };
    if age < std::time::Duration::from_secs(FORT_REFRESH_LOCK_STALE_AFTER_SECONDS) {
        return Ok(false);
    }
    fs::remove_file(lock_path)
        .with_context(|| format!("remove stale fort refresh lock {}", lock_path.display()))?;
    Ok(true)
}

fn resolve_fort_program(
    settings: &FortSettings,
    repo: Option<PathBuf>,
    build: bool,
    no_build: bool,
    bin: Option<PathBuf>,
) -> Result<PathBuf> {
    resolve_external_tool_program(
        "fort",
        repo,
        settings.repo.as_deref(),
        bin,
        settings.bin.as_deref(),
        settings.build,
        build,
        no_build,
    )
}

fn resolve_fort_build_fallback(settings: &FortSettings) -> Result<PathBuf> {
    let repo = resolve_external_tool_repo("fort", None, settings.repo.as_deref())?;
    existing_checkout_binary(&repo, "fort")
        .map(Ok)
        .unwrap_or_else(|| build_external_tool_binary(repo, "fort"))
}

fn default_fort_bootstrap_token_path(home: &Path) -> PathBuf {
    home.join(".si").join("fort").join("bootstrap").join("admin.token")
}

fn default_fort_bootstrap_refresh_token_path(home: &Path) -> PathBuf {
    home.join(".si").join("fort").join("bootstrap").join("admin.refresh.token")
}

fn default_fort_bootstrap_refresh_lock_path(home: &Path) -> PathBuf {
    home.join(".si").join("fort").join("bootstrap").join("admin.refresh.lock")
}

fn normalize_fort_persistent_host_option(
    value: Option<String>,
    label: &str,
) -> Result<Option<String>> {
    let Some(value) = value else {
        return Ok(None);
    };
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return Ok(Some(String::new()));
    }
    Ok(Some(normalize_fort_public_https_host(trimmed, label)?))
}

fn resolve_configured_fort_public_host(settings: &Settings, home: &Path) -> Result<Option<String>> {
    if let Some(host) =
        settings.fort.host.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        return normalize_fort_public_https_host(host, "fort.host").map(Some);
    }
    for path in [home.join(".si/fort/settings.toml"), home.join(".si/settings.toml")] {
        if let Some(host) = read_fort_host_from_settings_file(&path)? {
            return normalize_fort_public_https_host(
                &host,
                &format!("{} [fort].host", path.display()),
            )
            .map(Some);
        }
    }
    Ok(None)
}

fn read_fort_host_from_settings_file(path: &Path) -> Result<Option<String>> {
    if !path.is_file() {
        return Ok(None);
    }
    let source = fs::read_to_string(path)
        .with_context(|| format!("read fort settings {}", path.display()))?;
    let parsed = toml::from_str::<toml::Value>(&source)
        .with_context(|| format!("parse fort settings {}", path.display()))?;
    let host = parsed
        .get("fort")
        .and_then(toml::Value::as_table)
        .and_then(|fort| fort.get("host").or_else(|| fort.get("host_url")))
        .and_then(toml::Value::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(ToOwned::to_owned);
    Ok(host)
}

fn normalize_fort_public_https_host(value: &str, label: &str) -> Result<String> {
    let parsed = url::Url::parse(value.trim())
        .with_context(|| format!("{label} must be a valid Fort endpoint URL"))?;
    let insecure_override = fort_allow_insecure_host_override();
    if parsed.scheme() != "https" && !insecure_override {
        anyhow::bail!("{label} must use https for persistent Fort configuration");
    }
    if parsed.scheme() != "https" && parsed.scheme() != "http" {
        anyhow::bail!("{label} must use https (or http when SI_FORT_ALLOW_INSECURE_HOST=1)");
    }
    if !parsed.username().is_empty() || parsed.password().is_some() {
        anyhow::bail!("{label} must not include credentials");
    }
    if parsed.query().is_some() || parsed.fragment().is_some() {
        anyhow::bail!("{label} must not include query or fragment components");
    }
    if parsed.path() != "/" {
        anyhow::bail!("{label} must be an origin URL without a path");
    }
    let host = parsed
        .host_str()
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .ok_or_else(|| anyhow!("{label} must include a host"))?;
    if fort_host_is_local_or_private(host) && !insecure_override {
        anyhow::bail!(
            "{label} must resolve through a public Fort HTTPS endpoint; use an explicit native --host only for temporary local development or break/fix debugging"
        );
    }
    Ok(parsed.to_string().trim_end_matches('/').to_owned())
}

fn fort_allow_insecure_host_override() -> bool {
    std::env::var("SI_FORT_ALLOW_INSECURE_HOST")
        .ok()
        .map(|value| value.trim().to_ascii_lowercase())
        .is_some_and(|value| matches!(value.as_str(), "1" | "true" | "yes"))
}

fn fort_host_is_local_or_private(host: &str) -> bool {
    let host = host.trim().trim_end_matches('.').to_ascii_lowercase();
    if host.is_empty()
        || host == "localhost"
        || host.ends_with(".localhost")
        || host.ends_with(".local")
        || host.ends_with(".internal")
        || host.ends_with(".lan")
    {
        return true;
    }
    if let Ok(ip) = host.parse::<IpAddr>() {
        return match ip {
            IpAddr::V4(value) => {
                value.is_private()
                    || value.is_loopback()
                    || value.is_link_local()
                    || value.is_unspecified()
            }
            IpAddr::V6(value) => {
                value.is_loopback()
                    || value.is_unspecified()
                    || value.is_unique_local()
                    || value.is_unicast_link_local()
            }
        };
    }
    false
}

fn write_secret_text_file(path: &Path, value: &str) -> Result<()> {
    let parent = path
        .parent()
        .ok_or_else(|| anyhow!("secret file path {} has no parent", path.display()))?;
    fs::create_dir_all(parent).with_context(|| format!("create dir {}", parent.display()))?;
    let prefix =
        format!(".{}.", path.file_name().and_then(|name| name.to_str()).unwrap_or("secret"));
    let mut tmp = tempfile::Builder::new()
        .prefix(&prefix)
        .tempfile_in(parent)
        .with_context(|| format!("create temp secret file in {}", parent.display()))?;
    tmp.write_all(format!("{value}\n").as_bytes())
        .with_context(|| format!("write {}", tmp.path().display()))?;
    #[cfg(unix)]
    fs::set_permissions(tmp.path(), fs::Permissions::from_mode(0o600))
        .with_context(|| format!("chmod {}", tmp.path().display()))?;
    tmp.into_temp_path()
        .persist(path)
        .map_err(|error| error.error)
        .with_context(|| format!("rename {}", path.display()))?;
    Ok(())
}

fn load_settings_document(path: &Path) -> Result<toml::map::Map<String, toml::Value>> {
    if !path.exists() {
        return Ok(toml::map::Map::new());
    }
    let source = fs::read_to_string(path)
        .with_context(|| format!("read settings file {}", path.display()))?;
    let parsed = toml::from_str::<toml::Value>(&source)
        .with_context(|| format!("parse settings file {}", path.display()))?;
    Ok(parsed.as_table().cloned().unwrap_or_default())
}

fn write_settings_document(
    path: &Path,
    document: &toml::map::Map<String, toml::Value>,
) -> Result<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)
            .with_context(|| format!("create settings dir {}", parent.display()))?;
    }
    let source = toml::to_string_pretty(&toml::Value::Table(document.clone()))?;
    fs::write(path, source).with_context(|| format!("write settings file {}", path.display()))?;
    Ok(())
}

fn ensure_toml_table<'a>(
    root: &'a mut toml::map::Map<String, toml::Value>,
    key: &str,
) -> Result<&'a mut toml::map::Map<String, toml::Value>> {
    if !root.contains_key(key) {
        root.insert(key.to_owned(), toml::Value::Table(toml::map::Map::new()));
    }
    root.get_mut(key)
        .and_then(toml::Value::as_table_mut)
        .ok_or_else(|| anyhow!("settings key {key} must be a table"))
}

fn ensure_nested_toml_table<'a>(
    root: &'a mut toml::map::Map<String, toml::Value>,
    path: &[&str],
) -> Result<&'a mut toml::map::Map<String, toml::Value>> {
    let mut current = root;
    for key in path {
        if !current.contains_key(*key) {
            current.insert((*key).to_owned(), toml::Value::Table(toml::map::Map::new()));
        }
        current = current
            .get_mut(*key)
            .and_then(toml::Value::as_table_mut)
            .ok_or_else(|| anyhow!("settings key {} must be a table", path.join(".")))?;
    }
    Ok(current)
}

fn toml_table_at<'a>(
    root: &'a toml::map::Map<String, toml::Value>,
    path: &[&str],
) -> Option<&'a toml::map::Map<String, toml::Value>> {
    let mut current = root;
    for key in path {
        current = current.get(*key)?.as_table()?;
    }
    Some(current)
}

fn toml_value_table_at<'a>(
    root: &'a toml::Value,
    path: &[&str],
) -> Option<&'a toml::map::Map<String, toml::Value>> {
    let mut current = root;
    for key in path {
        current = current.as_table()?.get(*key)?;
    }
    current.as_table()
}

fn set_toml_string(
    table: &mut toml::map::Map<String, toml::Value>,
    key: &str,
    value: Option<String>,
) {
    match value.map(|item| item.trim().to_owned()) {
        Some(value) if !value.is_empty() => {
            table.insert(key.to_owned(), toml::Value::String(value));
        }
        Some(_) => {
            table.remove(key);
        }
        None => {}
    }
}

fn set_toml_bool(table: &mut toml::map::Map<String, toml::Value>, key: &str, value: Option<bool>) {
    if let Some(value) = value {
        table.insert(key.to_owned(), toml::Value::Boolean(value));
    }
}

fn resolve_codex_active_profile_id(settings: &Settings) -> Option<String> {
    settings
        .codex
        .profiles
        .active
        .as_deref()
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(str::to_owned)
        .or_else(|| {
            settings
                .codex
                .profile
                .as_deref()
                .map(str::trim)
                .filter(|value| !value.is_empty())
                .map(str::to_owned)
        })
}

fn codex_profile_display_name(profile: &CodexProfileView) -> &str {
    profile
        .name
        .as_deref()
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .unwrap_or(profile.profile.as_str())
}

fn codex_profile_matches_exact(profile: &CodexProfileView, query: &str) -> bool {
    let lowered = query.trim().to_lowercase();
    if lowered.is_empty() {
        return false;
    }
    profile.profile.eq_ignore_ascii_case(&lowered)
        || codex_profile_display_name(profile).to_lowercase() == lowered
        || profile
            .email
            .as_deref()
            .map(str::trim)
            .filter(|value| !value.is_empty())
            .is_some_and(|value| value.to_lowercase() == lowered)
}

fn codex_profile_matches_fuzzy(profile: &CodexProfileView, query: &str) -> bool {
    let lowered = query.trim().to_lowercase();
    if lowered.is_empty() {
        return false;
    }
    profile.profile.to_lowercase().contains(&lowered)
        || codex_profile_display_name(profile).to_lowercase().contains(&lowered)
        || profile
            .email
            .as_deref()
            .map(str::trim)
            .filter(|value| !value.is_empty())
            .is_some_and(|value| value.to_lowercase().contains(&lowered))
}

fn find_codex_profile_candidates(profiles: &[CodexProfileView], query: &str) -> Vec<usize> {
    let exact = profiles
        .iter()
        .enumerate()
        .filter_map(|(index, profile)| codex_profile_matches_exact(profile, query).then_some(index))
        .collect::<Vec<_>>();
    if !exact.is_empty() {
        return exact;
    }
    profiles
        .iter()
        .enumerate()
        .filter_map(|(index, profile)| codex_profile_matches_fuzzy(profile, query).then_some(index))
        .collect()
}

fn codex_profile_email_table_value(email: Option<&str>) -> String {
    let Some(email) = email.map(str::trim).filter(|value| !value.is_empty()) else {
        return "-".to_owned();
    };
    let Some((local, domain)) = email.split_once('@') else {
        return email.to_owned();
    };
    let local = local.trim();
    let domain = domain.trim();
    if local.is_empty() || domain.is_empty() {
        return email.to_owned();
    }
    let mut chars = domain.chars();
    let first = chars.next().unwrap_or_default();
    format!("{local}@{first}…")
}

fn render_codex_profile_table(profiles: &[CodexProfileView], include_index: bool) -> String {
    let snapshot_now = Utc::now();
    let mut table = Table::new();
    table
        .load_preset(UTF8_FULL)
        .apply_modifier(UTF8_ROUND_CORNERS)
        .set_content_arrangement(ContentArrangement::Dynamic)
        .set_truncation_indicator("…");
    if let Some(width) = env::var("COLUMNS")
        .ok()
        .and_then(|value| value.trim().parse::<u16>().ok())
        .filter(|width| *width > 20)
    {
        table.set_width(width);
    }

    let mut header = Vec::new();
    if include_index {
        header.push(
            Cell::new("#").fg(cli_table_color(CliTone::Muted)).add_attribute(Attribute::Bold),
        );
    }
    header.push(
        Cell::new("Profile").fg(cli_table_color(CliTone::Section)).add_attribute(Attribute::Bold),
    );
    header.push(
        Cell::new("Name").fg(cli_table_color(CliTone::Command)).add_attribute(Attribute::Bold),
    );
    header.push(
        Cell::new("Email").fg(cli_table_color(CliTone::Label)).add_attribute(Attribute::Bold),
    );
    header.push(Cell::new("5H").fg(cli_table_color(CliTone::Info)).add_attribute(Attribute::Bold));
    header.push(
        Cell::new("Weekly").fg(cli_table_color(CliTone::Info)).add_attribute(Attribute::Bold),
    );
    table.set_header(header);
    let mut constraints = Vec::new();
    if include_index {
        constraints.push(ColumnConstraint::Absolute(Width::Fixed(3)));
    }
    constraints.push(ColumnConstraint::UpperBoundary(Width::Fixed(12)));
    constraints.push(ColumnConstraint::UpperBoundary(Width::Fixed(16)));
    constraints.push(ColumnConstraint::UpperBoundary(Width::Fixed(30)));
    constraints.push(ColumnConstraint::UpperBoundary(Width::Fixed(22)));
    constraints.push(ColumnConstraint::UpperBoundary(Width::Fixed(22)));
    table.set_constraints(constraints);

    for (index, profile) in profiles.iter().enumerate() {
        let mut row = Vec::new();
        if include_index {
            row.push(Cell::new((index + 1).to_string()).fg(cli_table_color(CliTone::Muted)));
        }
        row.push(Cell::new(&profile.profile).fg(cli_table_color(CliTone::Section)));
        row.push(
            Cell::new(codex_profile_display_name(profile)).fg(cli_table_color(CliTone::Command)),
        );
        row.push(
            Cell::new(codex_profile_email_table_value(profile.email.as_deref()))
                .fg(cli_table_color(CliTone::Label)),
        );
        let missing_auth = profile.state != "Logged-In";
        let five_hour = render_codex_quota_cell(
            profile.five_hour_left_pct,
            profile.five_hour_remaining_minutes,
            profile.quota_sampled_at.as_ref(),
            snapshot_now,
            missing_auth,
        );
        row.push(Cell::new(five_hour.0).fg(five_hour.1));
        let weekly = render_codex_quota_cell(
            profile.weekly_left_pct,
            profile.weekly_remaining_minutes,
            profile.quota_sampled_at.as_ref(),
            snapshot_now,
            missing_auth,
        );
        row.push(Cell::new(weekly.0).fg(weekly.1));
        table.add_row(row);
    }

    table.to_string()
}

fn render_option_percent_value(value: Option<f64>) -> String {
    value.map(|value| format!("{value:.1}%")).unwrap_or_else(|| "-".to_owned())
}

fn format_relative_minutes_compact(remaining_minutes: i32) -> String {
    if remaining_minutes <= 0 {
        return "now".to_owned();
    }
    let mut minutes = remaining_minutes;
    let days = minutes / (24 * 60);
    minutes %= 24 * 60;
    let hours = minutes / 60;
    minutes %= 60;
    if days > 0 {
        if hours > 0 { format!("in {days}d{hours}h") } else { format!("in {days}d") }
    } else if hours > 0 {
        if minutes > 0 { format!("in {hours}h{minutes}m") } else { format!("in {hours}h") }
    } else {
        format!("in {minutes}m")
    }
}

fn codex_quota_color(pct: Option<f64>, missing_auth: bool) -> Color {
    if missing_auth {
        return cli_table_color(CliTone::Warning);
    }
    match pct {
        Some(value) if value < 20.0 => cli_table_color(CliTone::Command),
        Some(value) if value < 50.0 => cli_table_color(CliTone::Warning),
        Some(_) => cli_table_color(CliTone::Success),
        None => cli_table_color(CliTone::Muted),
    }
}

fn render_codex_quota_cell(
    pct: Option<f64>,
    remaining_minutes: Option<i32>,
    sampled_at: Option<&DateTime<Utc>>,
    snapshot_now: DateTime<Utc>,
    missing_auth: bool,
) -> (String, Color) {
    if missing_auth {
        return ("Missing".to_owned(), cli_table_color(CliTone::Warning));
    }
    let mut text = render_option_percent_value(pct);
    if let Some(minutes) = remaining_minutes {
        let minutes = normalize_quota_remaining_minutes(minutes, sampled_at, snapshot_now);
        text.push_str(" · ");
        text.push_str(&format_relative_minutes_compact(minutes));
    }
    (text, codex_quota_color(pct, false))
}

fn normalize_quota_remaining_minutes(
    remaining_minutes: i32,
    sampled_at: Option<&DateTime<Utc>>,
    snapshot_now: DateTime<Utc>,
) -> i32 {
    let Some(sampled_at) = sampled_at else {
        return remaining_minutes;
    };
    let elapsed = snapshot_now.signed_duration_since(*sampled_at).num_minutes();
    if elapsed <= 0 {
        return remaining_minutes;
    }
    remaining_minutes.saturating_sub(elapsed as i32)
}

fn codex_profile_prompt_available() -> bool {
    io::stdin().is_terminal() && io::stdout().is_terminal()
}

fn prompt_for_codex_profile(purpose: &str, profiles: &[CodexProfileView]) -> Result<String> {
    if profiles.is_empty() {
        anyhow::bail!("no codex profiles are configured");
    }

    println!(
        "{} {}:",
        stdout_text("Select codex profile for", CliTone::Section),
        stdout_text(purpose, CliTone::Command),
    );
    println!("{}", render_codex_profile_table(profiles, true));
    print!("{} ", stdout_text("Enter number or profile:", CliTone::Heading));
    io::stdout().flush().context("flush codex profile prompt")?;

    let mut input = String::new();
    io::stdin().read_line(&mut input).context("read codex profile selection")?;
    let trimmed = input.trim();
    if trimmed.is_empty() {
        anyhow::bail!("codex profile selection is required");
    }
    if let Ok(index) = trimmed.parse::<usize>() {
        if let Some(profile) = profiles.get(index.saturating_sub(1)) {
            return Ok(profile.profile.clone());
        }
        anyhow::bail!("invalid profile number {index}");
    }
    let candidates = find_codex_profile_candidates(profiles, trimmed);
    if candidates.len() == 1 {
        return Ok(profiles[candidates[0]].profile.clone());
    }
    anyhow::bail!("could not resolve codex profile from {trimmed:?}")
}

fn codex_profile_resolution_error(query: &str, profiles: &[CodexProfileView]) -> anyhow::Error {
    let available = profiles.iter().map(|profile| profile.profile.as_str()).collect::<Vec<_>>();
    anyhow!(
        "codex profile {:?} is not configured; available profiles: {}",
        query,
        if available.is_empty() { "(none)".to_owned() } else { available.join(", ") }
    )
}

fn load_codex_profiles_from_settings(
    settings: &Settings,
    paths: &SiPaths,
) -> Vec<CodexProfileView> {
    let active = resolve_codex_active_profile_id(settings);
    let mut profiles = settings
        .codex
        .profiles
        .entries
        .iter()
        .map(|(profile_id, entry)| codex_profile_view(paths, active.as_deref(), profile_id, entry))
        .collect::<Vec<_>>();
    profiles.sort_by(|left, right| left.profile.cmp(&right.profile));
    profiles
}

fn resolve_codex_profile(
    _home: &Path,
    settings: &Settings,
    paths: &SiPaths,
    profile: Option<&str>,
    purpose: &str,
) -> Result<String> {
    let profiles = load_codex_profiles_from_settings(settings, paths);

    if let Some(query) = profile.map(str::trim).filter(|value| !value.is_empty()) {
        let candidates = find_codex_profile_candidates(&profiles, query);
        return match candidates.as_slice() {
            [index] => Ok(profiles[*index].profile.clone()),
            [] => Err(codex_profile_resolution_error(query, &profiles)),
            _ => Err(anyhow!("codex profile {query:?} matched multiple configured profiles")),
        };
    }

    if codex_profile_prompt_available() {
        return prompt_for_codex_profile(purpose, &profiles);
    }

    anyhow::bail!(
        "codex profile is required; pass one explicitly or run in a TTY to choose from the configured profiles"
    )
}

fn resolve_codex_requested_profile(
    home: &Path,
    settings: &Settings,
    paths: &SiPaths,
    profile: Option<&str>,
) -> Result<String> {
    resolve_codex_profile(home, settings, paths, profile, "codex")
}

fn default_codex_profile_auth_path(paths: &SiPaths, profile: &str) -> String {
    paths.codex_profiles_dir.join(profile).join("auth.json").display().to_string()
}

fn codex_profile_home_dir(paths: &SiPaths, settings: &Settings, profile: &str) -> PathBuf {
    codex_profile_auth_path_from_settings(paths, settings, profile)
        .and_then(|path| path.parent().map(Path::to_path_buf))
        .filter(|path| !path.as_os_str().is_empty())
        .unwrap_or_else(|| paths.codex_profiles_dir.join(profile))
}

fn host_codex_home_dir(home: &Path) -> PathBuf {
    home.join(".codex")
}

fn decode_jwt_payload_claims(token: &str) -> Option<Value> {
    let payload = token.trim().split('.').nth(1)?;
    let decoded = URL_SAFE_NO_PAD.decode(payload.as_bytes()).ok()?;
    serde_json::from_slice::<Value>(&decoded).ok()
}

fn codex_auth_email_from_file(path: &Path) -> Option<String> {
    let raw = fs::read_to_string(path).ok()?;
    let auth = serde_json::from_str::<Value>(&raw).ok()?;
    let tokens = auth.get("tokens")?;

    let access_email = tokens
        .get("access_token")
        .and_then(Value::as_str)
        .and_then(decode_jwt_payload_claims)
        .and_then(|claims| {
            claims.get("https://api.openai.com/profile")?.get("email")?.as_str().map(str::to_owned)
        });
    if let Some(email) =
        access_email.map(|value| value.trim().to_owned()).filter(|value| !value.is_empty())
    {
        return Some(email);
    }

    tokens
        .get("id_token")
        .and_then(Value::as_str)
        .and_then(decode_jwt_payload_claims)
        .and_then(|claims| claims.get("email")?.as_str().map(str::to_owned))
        .map(|value| value.trim().to_owned())
        .filter(|value| !value.is_empty())
}

fn codex_profile_auth_state_label(auth_path: &str, expected_email: Option<&str>) -> &'static str {
    let path = Path::new(auth_path);
    if !path.is_file() {
        return "Missing";
    }
    let Some(actual_email) = codex_auth_email_from_file(path) else {
        return "Missing";
    };
    let Some(expected_email) = expected_email.map(str::trim).filter(|value| !value.is_empty())
    else {
        return "Logged-In";
    };
    if actual_email.eq_ignore_ascii_case(expected_email) { "Logged-In" } else { "Missing" }
}

fn reset_host_codex_home(codex_home: &Path) -> Result<()> {
    let mut preserved = Vec::new();
    for file_name in ["config.toml", "configs.toml"] {
        let path = codex_home.join(file_name);
        if path.is_file() {
            preserved.push((file_name.to_owned(), fs::read(&path)?));
        }
    }

    fs::create_dir_all(codex_home)
        .with_context(|| format!("create codex home {}", codex_home.display()))?;
    for entry in
        fs::read_dir(codex_home).with_context(|| format!("read {}", codex_home.display()))?
    {
        let entry = entry?;
        let path = entry.path();
        let file_name = entry.file_name();
        if preserved.iter().any(|(name, _)| file_name == name.as_str()) {
            continue;
        }
        let file_type = entry.file_type()?;
        if file_type.is_dir() && !file_type.is_symlink() {
            fs::remove_dir_all(&path).with_context(|| format!("remove dir {}", path.display()))?;
        } else {
            fs::remove_file(&path).with_context(|| format!("remove file {}", path.display()))?;
        }
    }

    for (file_name, contents) in preserved {
        let path = codex_home.join(file_name);
        fs::write(&path, contents).with_context(|| format!("restore {}", path.display()))?;
        #[cfg(unix)]
        fs::set_permissions(&path, fs::Permissions::from_mode(0o600))
            .with_context(|| format!("chmod {}", path.display()))?;
    }

    Ok(())
}

fn codex_profile_view(
    paths: &SiPaths,
    active_profile: Option<&str>,
    profile_id: &str,
    entry: &CodexProfileEntry,
) -> CodexProfileView {
    let auth_path = entry
        .auth_path
        .clone()
        .unwrap_or_else(|| default_codex_profile_auth_path(paths, profile_id));
    CodexProfileView {
        profile: profile_id.to_owned(),
        active: active_profile.is_some_and(|value| value == profile_id),
        state: codex_profile_auth_state_label(&auth_path, entry.email.as_deref()).to_owned(),
        name: entry.name.clone(),
        email: entry.email.clone(),
        account_plan: None,
        five_hour_left_pct: None,
        five_hour_reset: None,
        five_hour_remaining_minutes: None,
        weekly_left_pct: None,
        weekly_reset: None,
        weekly_remaining_minutes: None,
        quota_sampled_at: None,
        auth_path: Some(auth_path),
        auth_updated: entry.auth_updated.clone(),
    }
}

fn apply_codex_status_to_profile(view: &mut CodexProfileView, status: &CodexStatusView) {
    view.state = "Logged-In".to_owned();
    if view.email.as_deref().is_none_or(|value| value.trim().is_empty()) {
        view.email = status.account_email.clone();
    }
    view.account_plan = status.account_plan.clone();
    view.five_hour_left_pct = status.five_hour_left_pct;
    view.five_hour_reset = status.five_hour_reset.clone();
    view.five_hour_remaining_minutes = status.five_hour_remaining_minutes;
    view.weekly_left_pct = status.weekly_left_pct;
    view.weekly_reset = status.weekly_reset.clone();
    view.weekly_remaining_minutes = status.weekly_remaining_minutes;
    view.quota_sampled_at = Some(Utc::now());
}

fn clear_codex_profile_live_status(view: &mut CodexProfileView) {
    view.account_plan = None;
    view.five_hour_left_pct = None;
    view.five_hour_reset = None;
    view.five_hour_remaining_minutes = None;
    view.weekly_left_pct = None;
    view.weekly_reset = None;
    view.weekly_remaining_minutes = None;
    view.quota_sampled_at = None;
}

fn codex_status_has_live_quota(status: &CodexStatusView) -> bool {
    status.five_hour_left_pct.is_some()
        || status.weekly_left_pct.is_some()
        || status.five_hour_remaining_minutes.is_some()
        || status.weekly_remaining_minutes.is_some()
}

fn resolve_codex_profile_live_view(
    mut profile: CodexProfileView,
    home: PathBuf,
    paths: SiPaths,
    settings: Settings,
) -> CodexProfileView {
    let mut last_status: Option<CodexStatusView> = None;
    for wait_ms in [0_u64, 350] {
        if wait_ms > 0 {
            thread::sleep(Duration::from_millis(wait_ms));
        }
        match read_codex_status_for_profile(
            profile.profile.as_str(),
            &home,
            &paths,
            &settings,
            None,
            false,
        ) {
            Ok(status) if codex_status_has_live_quota(&status) => {
                apply_codex_status_to_profile(&mut profile, &status);
                return profile;
            }
            Ok(status) => {
                last_status = Some(status);
            }
            Err(_) => {}
        }
    }
    if let Some(status) = last_status {
        apply_codex_status_to_profile(&mut profile, &status);
    } else {
        clear_codex_profile_live_status(&mut profile);
    }
    profile
}

fn codex_profile_has_live_quota(profile: &CodexProfileView) -> bool {
    profile.five_hour_left_pct.is_some()
        || profile.weekly_left_pct.is_some()
        || profile.five_hour_remaining_minutes.is_some()
        || profile.weekly_remaining_minutes.is_some()
}

fn enrich_codex_profiles_with_live_status(
    profiles: Vec<CodexProfileView>,
    home: &Path,
    paths: &SiPaths,
    settings: &Settings,
) -> Vec<CodexProfileView> {
    let progress = Arc::new(AtomicUsize::new(0));
    enrich_codex_profiles_with_live_status_parallel(profiles, home, paths, settings, Some(progress))
}

fn enrich_codex_profiles_with_live_status_parallel(
    profiles: Vec<CodexProfileView>,
    home: &Path,
    paths: &SiPaths,
    settings: &Settings,
    progress: Option<Arc<AtomicUsize>>,
) -> Vec<CodexProfileView> {
    let home = home.to_path_buf();
    let paths = paths.clone();
    let settings = settings.clone();
    let max_parallel = thread::available_parallelism().map_or(2, |value| value.get().min(4)).max(1);
    let mut pending = profiles.into_iter().enumerate().collect::<Vec<_>>();
    let mut ordered = Vec::with_capacity(pending.len());
    while !pending.is_empty() {
        let batch_size = pending.len().min(max_parallel);
        let batch = pending.drain(..batch_size).collect::<Vec<_>>();
        thread::scope(|scope| {
            let mut handles = Vec::with_capacity(batch.len());
            for (index, profile) in batch {
                let home = home.clone();
                let paths = paths.clone();
                let settings = settings.clone();
                let progress = progress.clone();
                handles.push(scope.spawn(move || {
                    let resolved = if profile.state == "Logged-In" {
                        resolve_codex_profile_live_view(profile, home, paths, settings)
                    } else {
                        profile
                    };
                    if let Some(progress) = progress {
                        progress.fetch_add(1, Ordering::Relaxed);
                    }
                    (index, resolved)
                }));
            }
            for handle in handles {
                if let Ok(item) = handle.join() {
                    ordered.push(item);
                }
            }
        });
    }
    ordered.sort_by_key(|(index, _)| *index);
    let mut profiles = ordered.into_iter().map(|(_, profile)| profile).collect::<Vec<_>>();
    for profile in &mut profiles {
        if profile.state == "Logged-In" && !codex_profile_has_live_quota(profile) {
            *profile = resolve_codex_profile_live_view(
                profile.clone(),
                home.clone(),
                paths.clone(),
                settings.clone(),
            );
        }
    }
    profiles
}

fn show_codex_profile_list(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let paths = SiPaths::from_settings(&home, &settings);
    let profiles = load_codex_profiles_from_settings(&settings, &paths);
    let spinner = match format {
        OutputFormat::Text => start_codex_profile_list_spinner(profiles.len()),
        OutputFormat::Json => None,
    };
    let progress = spinner.as_ref().map(CliSpinnerHandle::progress_counter);
    let profiles = enrich_codex_profiles_with_live_status_parallel(
        profiles, &home, &paths, &settings, progress,
    );
    if let Some(spinner) = spinner {
        spinner.finish();
    }

    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&profiles)?),
        OutputFormat::Text => {
            if profiles.is_empty() {
                println!("No codex profiles configured.");
            } else {
                println!("{}", render_codex_profile_table(&profiles, false));
            }
        }
    }

    Ok(())
}

fn show_codex_profile(
    profile: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let paths = SiPaths::from_settings(&home, &settings);
    let profile_id = resolve_codex_profile(&home, &settings, &paths, profile.as_deref(), "show")?;
    let entry = settings
        .codex
        .profiles
        .entries
        .get(profile_id.as_str())
        .cloned()
        .ok_or_else(|| anyhow!("missing codex profile entry for {profile_id}"))?;
    let view = codex_profile_view(
        &paths,
        resolve_codex_active_profile_id(&settings).as_deref(),
        &profile_id,
        &entry,
    );
    let mut profiles = enrich_codex_profiles_with_live_status(vec![view], &home, &paths, &settings);
    let view = profiles.pop().expect("single codex profile view");

    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => println!("{}", render_codex_profile_table(&[view], false)),
    }

    Ok(())
}

fn add_codex_profile(
    profile: String,
    name: Option<String>,
    email: Option<String>,
    auth_path: Option<String>,
    activate: bool,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings_path = settings_file.unwrap_or_else(|| home.join(".si").join("settings.toml"));
    let settings = Settings::load(&home, Some(&settings_path))?;
    let paths = SiPaths::from_settings(&home, &settings);
    let profile_id = profile.trim();
    if profile_id.is_empty() {
        anyhow::bail!("profile is required");
    }

    let resolved_auth_path = auth_path
        .as_deref()
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(str::to_owned)
        .unwrap_or_else(|| default_codex_profile_auth_path(&paths, profile_id));
    if let Some(parent) = Path::new(&resolved_auth_path).parent() {
        fs::create_dir_all(parent)
            .with_context(|| format!("create codex profile dir {}", parent.display()))?;
    }

    let mut document = load_settings_document(&settings_path)?;
    if !document.contains_key("schema_version") {
        document.insert("schema_version".to_owned(), toml::Value::Integer(1));
    }
    let codex = ensure_toml_table(&mut document, "codex")?;
    let profiles = ensure_toml_table(codex, "profiles")?;
    let entries = ensure_toml_table(profiles, "entries")?;
    let entry_table = ensure_toml_table(entries, profile_id)?;
    set_toml_string(entry_table, "name", name);
    set_toml_string(entry_table, "email", email);
    set_toml_string(entry_table, "auth_path", Some(resolved_auth_path.clone()));
    if activate {
        set_toml_string(profiles, "active", Some(profile_id.to_owned()));
        set_toml_string(codex, "profile", Some(profile_id.to_owned()));
    }
    write_settings_document(&settings_path, &document)?;

    show_codex_profile(Some(profile_id.to_owned()), Some(home), Some(settings_path), format)
}

fn remove_codex_profile(
    profile: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings_path = settings_file.unwrap_or_else(|| home.join(".si").join("settings.toml"));
    let settings = Settings::load(&home, Some(&settings_path))?;
    let paths = SiPaths::from_settings(&home, &settings);
    let profile_id = resolve_codex_profile(&home, &settings, &paths, profile.as_deref(), "remove")?;
    let mut document = load_settings_document(&settings_path)?;
    let codex = ensure_toml_table(&mut document, "codex")?;
    if let Some(profiles) = codex.get_mut("profiles").and_then(toml::Value::as_table_mut) {
        if let Some(entries) = profiles.get_mut("entries").and_then(toml::Value::as_table_mut) {
            entries.remove(profile_id.as_str());
            if entries.is_empty() {
                profiles.remove("entries");
            }
        }
        if profiles.get("active").and_then(toml::Value::as_str) == Some(profile_id.as_str()) {
            profiles.remove("active");
        }
        if profiles.is_empty() {
            codex.remove("profiles");
        }
    }
    if codex.get("profile").and_then(toml::Value::as_str) == Some(profile_id.as_str()) {
        codex.remove("profile");
    }
    if codex.is_empty() {
        document.remove("codex");
    }
    write_settings_document(&settings_path, &document)?;
    Ok(())
}

fn login_codex_profile(
    profile: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    codex_bin: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings_path = settings_file.unwrap_or_else(|| home.join(".si").join("settings.toml"));
    let settings = Settings::load(&home, Some(&settings_path))?;
    let paths = SiPaths::from_settings(&home, &settings);
    let profile_id = resolve_codex_profile(&home, &settings, &paths, profile.as_deref(), "login")?;
    let entry = settings
        .codex
        .profiles
        .entries
        .get(profile_id.as_str())
        .cloned()
        .ok_or_else(|| anyhow!("missing codex profile entry for {profile_id}"))?;
    let resolved_auth_path = entry
        .auth_path
        .clone()
        .unwrap_or_else(|| default_codex_profile_auth_path(&paths, &profile_id));
    if let Some(parent) = Path::new(&resolved_auth_path).parent() {
        fs::create_dir_all(parent)
            .with_context(|| format!("create codex profile dir {}", parent.display()))?;
    }
    let codex_bin = codex_bin.unwrap_or_else(|| PathBuf::from("codex"));
    let codex_home = Path::new(&resolved_auth_path)
        .parent()
        .map(Path::to_path_buf)
        .unwrap_or_else(|| paths.codex_profiles_dir.join(&profile_id));
    fs::create_dir_all(&codex_home)
        .with_context(|| format!("create codex home {}", codex_home.display()))?;
    let host_auth_path = codex_home.join("auth.json");

    let mut command = StdCommand::new(&codex_bin);
    command.env("HOME", &home).env("CODEX_HOME", &codex_home).arg("login").arg("--device-auth");
    let status = command.status().with_context(|| format!("run {} login", codex_bin.display()))?;
    if !status.success() {
        anyhow::bail!("codex login failed: {status}");
    }

    if !host_auth_path.exists() {
        anyhow::bail!("expected codex auth at {} after login", host_auth_path.display());
    }
    if let Some(expected_email) =
        entry.email.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        let actual_email = codex_auth_email_from_file(&host_auth_path);
        if !actual_email.as_deref().is_some_and(|value| value.eq_ignore_ascii_case(expected_email))
        {
            let _ = fs::remove_file(&host_auth_path);
            anyhow::bail!(
                "codex login saved auth for {:?}, expected {:?} for profile {}",
                actual_email.as_deref().unwrap_or("unknown"),
                expected_email,
                profile_id
            );
        }
    }
    if Path::new(&resolved_auth_path) != host_auth_path {
        fs::copy(&host_auth_path, &resolved_auth_path).with_context(|| {
            format!("copy codex auth from {} to {}", host_auth_path.display(), resolved_auth_path)
        })?;
    }
    #[cfg(unix)]
    fs::set_permissions(&resolved_auth_path, fs::Permissions::from_mode(0o600))
        .with_context(|| format!("chmod {resolved_auth_path}"))?;

    let mut document = load_settings_document(&settings_path)?;
    if !document.contains_key("schema_version") {
        document.insert("schema_version".to_owned(), toml::Value::Integer(1));
    }
    let codex = ensure_toml_table(&mut document, "codex")?;
    let profiles = ensure_toml_table(codex, "profiles")?;
    let entries = ensure_toml_table(profiles, "entries")?;
    let entry_table = ensure_toml_table(entries, profile_id.as_str())?;
    set_toml_string(entry_table, "auth_path", Some(resolved_auth_path.clone()));
    set_toml_string(
        entry_table,
        "auth_updated",
        Some(Utc::now().to_rfc3339_opts(chrono::SecondsFormat::Secs, true)),
    );
    write_settings_document(&settings_path, &document)?;
    match format {
        OutputFormat::Text => show_codex_profile_list(Some(home), Some(settings_path), format),
        OutputFormat::Json => {
            show_codex_profile(Some(profile_id), Some(home), Some(settings_path), format)
        }
    }
}

fn swap_codex_profile(
    profile: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings_path = settings_file.unwrap_or_else(|| home.join(".si").join("settings.toml"));
    let settings = Settings::load(&home, Some(&settings_path))?;
    let paths = SiPaths::from_settings(&home, &settings);
    let profile_id = resolve_codex_profile(&home, &settings, &paths, profile.as_deref(), "swap")?;
    let entry = settings
        .codex
        .profiles
        .entries
        .get(profile_id.as_str())
        .cloned()
        .ok_or_else(|| anyhow!("missing codex profile entry for {profile_id}"))?;
    let resolved_auth_path = entry
        .auth_path
        .clone()
        .unwrap_or_else(|| default_codex_profile_auth_path(&paths, &profile_id));
    if codex_profile_auth_state_label(&resolved_auth_path, entry.email.as_deref()) != "Logged-In" {
        anyhow::bail!(
            "codex profile {profile_id} is not Logged-In; run `si codex profile login {profile_id}` first"
        );
    }

    let host_codex_home = host_codex_home_dir(&home);
    reset_host_codex_home(&host_codex_home)?;
    let host_auth_path = host_codex_home.join("auth.json");
    fs::copy(&resolved_auth_path, &host_auth_path).with_context(|| {
        format!("copy codex auth from {} to {}", resolved_auth_path, host_auth_path.display())
    })?;
    #[cfg(unix)]
    fs::set_permissions(&host_auth_path, fs::Permissions::from_mode(0o600))
        .with_context(|| format!("chmod {}", host_auth_path.display()))?;

    let mut document = load_settings_document(&settings_path)?;
    if !document.contains_key("schema_version") {
        document.insert("schema_version".to_owned(), toml::Value::Integer(1));
    }
    let codex = ensure_toml_table(&mut document, "codex")?;
    let profiles = ensure_toml_table(codex, "profiles")?;
    set_toml_string(profiles, "active", Some(profile_id.clone()));
    set_toml_string(codex, "profile", Some(profile_id.clone()));
    write_settings_document(&settings_path, &document)?;

    show_codex_profile(Some(profile_id), Some(home), Some(settings_path), format)
}

fn load_codex_runtime_settings(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
) -> Result<(PathBuf, Settings)> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    Ok((home, settings))
}

fn show_fort_session_state(path: PathBuf, format: OutputFormat) -> Result<()> {
    let state = load_persisted_session_state(path)?;

    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&state)?),
        OutputFormat::Text => render_fort_session_state_text(&state),
    }

    Ok(())
}

fn write_fort_session_state(path: PathBuf, state_json: &str) -> Result<()> {
    let state: PersistedSessionState = serde_json::from_str(state_json)?;
    save_persisted_session_state(path, &state)?;
    Ok(())
}

fn clear_fort_session_state(path: PathBuf) -> Result<()> {
    clear_persisted_session_state(path)?;
    Ok(())
}

fn show_fort_session_bootstrap_view(
    path: PathBuf,
    profile_id: Option<String>,
    access_token_path: &str,
    refresh_token_path: &str,
    access_token_runtime_path: &str,
    refresh_token_runtime_path: &str,
    format: OutputFormat,
) -> Result<()> {
    let state = load_persisted_session_state(path)?;
    let view = build_bootstrap_view(
        &state,
        profile_id.as_deref(),
        access_token_path,
        refresh_token_path,
        access_token_runtime_path,
        refresh_token_runtime_path,
    )?;

    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => render_fort_bootstrap_view_text(&view),
    }

    Ok(())
}

fn show_fort_runtime_agent_state(path: PathBuf, format: OutputFormat) -> Result<()> {
    let state = load_persisted_runtime_agent_state(path)?;

    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&state)?),
        OutputFormat::Text => render_fort_runtime_agent_state_text(&state),
    }

    Ok(())
}

fn write_fort_runtime_agent_state(path: PathBuf, state_json: &str) -> Result<()> {
    let state: PersistedRuntimeAgentState = serde_json::from_str(state_json)?;
    save_persisted_runtime_agent_state(path, &state)?;
    Ok(())
}

fn clear_fort_runtime_agent_state(path: PathBuf) -> Result<()> {
    clear_persisted_runtime_agent_state(path)?;
    Ok(())
}

fn show_fort_session_state_classification(
    path: PathBuf,
    now_unix: i64,
    format: OutputFormat,
) -> Result<()> {
    let state = load_persisted_session_state(path)?;
    let classified = classify_persisted_session_state(&state, now_unix)?;

    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&classified)?),
        OutputFormat::Text => render_fort_session_classification_text(&classified),
    }

    Ok(())
}

fn show_fort_session_state_refresh_outcome(
    path: PathBuf,
    outcome: FortRefreshOutcomeArg,
    now_unix: i64,
    access_expires_at_unix: Option<i64>,
    refresh_expires_at_unix: Option<i64>,
    format: OutputFormat,
) -> Result<()> {
    let state = load_persisted_session_state(path)?;
    let transition = apply_refresh_outcome_to_persisted_session_state(
        &state,
        build_fort_refresh_outcome(outcome, access_expires_at_unix, refresh_expires_at_unix)?,
        now_unix,
    )?;
    let view = FortSessionTransitionView {
        state: transition.state,
        classification: fort_session_classification_view(&transition.classification),
    };

    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            render_fort_session_state_text(&view.state);
            println!("classification.state={}", view.classification.state);
            if let Some(reason) = view.classification.reason {
                println!("classification.reason={reason}");
            }
        }
    }

    Ok(())
}

fn show_fort_session_state_teardown(
    path: PathBuf,
    now_unix: i64,
    format: OutputFormat,
) -> Result<()> {
    let state = load_persisted_session_state(path)?;
    let classification = teardown_persisted_session_state(&state, now_unix)?;
    let view = fort_session_classification_view(&classification);

    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            println!("classification.state={}", view.state);
            if let Some(reason) = view.reason {
                println!("classification.reason={reason}");
            }
        }
    }

    Ok(())
}

fn render_fort_bootstrap_view_text(view: &BootstrapView) {
    println!("profile_id={}", view.profile_id);
    println!("agent_id={}", view.agent_id);
    println!("session_id={}", view.session_id);
    println!("host_url={}", view.host_url);
    println!("runtime_host_url={}", view.runtime_host_url);
    println!("access_token_path={}", view.access_token_path);
    println!("refresh_token_path={}", view.refresh_token_path);
    println!("access_token_runtime_path={}", view.access_token_runtime_path);
    println!("refresh_token_runtime_path={}", view.refresh_token_runtime_path);
}

fn build_fort_refresh_outcome(
    outcome: FortRefreshOutcomeArg,
    access_expires_at_unix: Option<i64>,
    refresh_expires_at_unix: Option<i64>,
) -> Result<RefreshOutcome> {
    Ok(match outcome {
        FortRefreshOutcomeArg::Success => RefreshOutcome::Success(RefreshSuccess {
            access_expires_at_unix: access_expires_at_unix.ok_or_else(|| {
                anyhow::anyhow!("--access-expires-at-unix is required for success outcomes")
            })?,
            refresh_expires_at_unix,
        }),
        FortRefreshOutcomeArg::Unauthorized => RefreshOutcome::Unauthorized,
        FortRefreshOutcomeArg::Retryable => RefreshOutcome::Retryable,
    })
}

fn fort_session_classification_view(
    classification: &SessionState,
) -> FortSessionClassificationView {
    match classification {
        SessionState::BootstrapRequired => {
            FortSessionClassificationView { state: "bootstrap_required".to_owned(), reason: None }
        }
        SessionState::Resumable(_) => {
            FortSessionClassificationView { state: "resumable".to_owned(), reason: None }
        }
        SessionState::Refreshing(_) => {
            FortSessionClassificationView { state: "refreshing".to_owned(), reason: None }
        }
        SessionState::Revoked { reason, .. } => FortSessionClassificationView {
            state: "revoked".to_owned(),
            reason: Some(format!("{reason:?}")),
        },
        SessionState::TeardownPending(_) => {
            FortSessionClassificationView { state: "teardown_pending".to_owned(), reason: None }
        }
        SessionState::Closed => {
            FortSessionClassificationView { state: "closed".to_owned(), reason: None }
        }
    }
}

fn show_vault_trust_lookup(
    path: PathBuf,
    repo_root: &str,
    file: &str,
    fingerprint: &str,
    format: OutputFormat,
) -> Result<()> {
    let store = TrustStore::load(path)?;
    let entry = store.find(repo_root, file);
    let view = VaultTrustLookupView {
        found: entry.is_some(),
        matches: entry.map(|entry| entry.fingerprint.trim() == fingerprint.trim()).unwrap_or(false),
        repo_root: repo_root.trim().to_owned(),
        file: file.trim().to_owned(),
        expected_fingerprint: fingerprint.trim().to_owned(),
        stored_fingerprint: entry.map(|entry| entry.fingerprint.clone()),
        trusted_at: entry.and_then(|entry| {
            if entry.trusted_at.trim().is_empty() { None } else { Some(entry.trusted_at.clone()) }
        }),
    };

    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            println!("found={}", view.found);
            println!("matches={}", view.matches);
            println!("repo_root={}", render_text_value(&view.repo_root));
            println!("file={}", render_text_value(&view.file));
            println!("expected_fingerprint={}", render_text_value(&view.expected_fingerprint));
            println!(
                "stored_fingerprint={}",
                render_option_text_value(view.stored_fingerprint.as_deref())
            );
            println!("trusted_at={}", render_option_text_value(view.trusted_at.as_deref()));
        }
    }

    Ok(())
}

fn render_fort_session_state_text(state: &PersistedSessionState) {
    println!("profile_id={}", render_text_value(&state.profile_id));
    println!("agent_id={}", render_text_value(&state.agent_id));
    println!("session_id={}", render_text_value(&state.session_id));
    println!("host={}", render_text_value(&state.host));
    println!("runtime_host={}", render_text_value(&state.runtime_host));
    println!("access_token_path={}", render_text_value(&state.access_token_path));
    println!("refresh_token_path={}", render_text_value(&state.refresh_token_path));
    println!("access_expires_at={}", render_text_value(&state.access_expires_at));
    println!("refresh_expires_at={}", render_text_value(&state.refresh_expires_at));
    println!("updated_at={}", render_text_value(&state.updated_at));
}

fn render_fort_runtime_agent_state_text(state: &PersistedRuntimeAgentState) {
    println!("profile_id={}", render_text_value(&state.profile_id));
    println!("pid={}", state.pid);
    println!("command_path={}", render_text_value(&state.command_path));
    println!("started_at={}", render_text_value(&state.started_at));
    println!("updated_at={}", render_text_value(&state.updated_at));
}

fn render_fort_session_classification_text(state: &SessionState) {
    match state {
        SessionState::BootstrapRequired => println!("state=bootstrap_required"),
        SessionState::Closed => println!("state=closed"),
        SessionState::Resumable(snapshot) => {
            println!("state=resumable");
            render_fort_snapshot_text(snapshot);
        }
        SessionState::Refreshing(snapshot) => {
            println!("state=refreshing");
            render_fort_snapshot_text(snapshot);
        }
        SessionState::TeardownPending(snapshot) => {
            println!("state=teardown_pending");
            render_fort_snapshot_text(snapshot);
        }
        SessionState::Revoked { snapshot, reason } => {
            println!("state=revoked");
            println!("reason={reason:?}");
            if let Some(snapshot) = snapshot {
                render_fort_snapshot_text(snapshot);
            }
        }
    }
}

fn render_fort_snapshot_text(snapshot: &si_fort::SessionSnapshot) {
    println!("profile_id={}", render_option_text_value(Some(&snapshot.profile_id)));
    println!("agent_id={}", render_option_text_value(Some(&snapshot.agent_id)));
    println!("session_id={}", render_option_text_value(snapshot.session_id.as_deref()));
    println!("access_expires_at_unix={}", render_option_number(snapshot.access_expires_at_unix));
    println!("refresh_expires_at_unix={}", render_option_number(snapshot.refresh_expires_at_unix));
}

fn render_text_value(value: &str) -> &str {
    if value.trim().is_empty() { "(none)" } else { value }
}

fn render_option_text_value(value: Option<&str>) -> &str {
    match value {
        Some(value) if !value.trim().is_empty() => value,
        _ => "(none)",
    }
}

fn render_option_number(value: Option<i64>) -> String {
    value.map(|item| item.to_string()).unwrap_or_else(|| "(none)".to_owned())
}

fn show_codex_spawn_start(
    profile: Option<String>,
    worker_slot: Option<String>,
    workspace: Option<PathBuf>,
) -> Result<()> {
    let (settings_home, settings) = load_codex_runtime_settings(None, None)?;
    let paths = SiPaths::from_settings(&settings_home, &settings);
    let profile_id =
        resolve_codex_profile(&settings_home, &settings, &paths, profile.as_deref(), "spawn")?;
    let workspace = resolve_codex_workspace(workspace, &settings)?;
    let workdir = resolve_codex_workdir(None, &workspace)?;
    let slot = normalize_codex_worker_slot(worker_slot.as_deref())?;
    let state = ensure_codex_worker_session(
        &settings_home,
        &paths,
        &settings,
        &profile_id,
        &slot,
        workspace,
        workdir,
        &[],
    )?;
    println!(
        "{}",
        serde_json::to_string_pretty(&serde_json::json!({
            "profile_id": state.profile_id,
            "worker_slot": state.worker_slot,
            "session_name": state.session_name,
            "workspace": state.workspace,
            "workdir": state.workdir,
            "updated_at": state.updated_at,
        }))?
    );
    Ok(())
}

fn run_codex_respawn(
    profile: Option<String>,
    worker_slot: Option<String>,
    workspace: Option<PathBuf>,
) -> Result<()> {
    let slot = normalize_codex_worker_slot(worker_slot.as_deref())?;
    let remove_result = run_codex_remove_with_settings(
        profile.as_deref(),
        Some(slot.as_str()),
        false,
        OutputFormat::Text,
        None,
        None,
    );
    if let Err(error) = remove_result {
        let detail = error.to_string();
        let not_found = detail.contains("no codex worker session found for profile");
        if !not_found {
            return Err(error).context("respawn cleanup failed");
        }
    }
    show_codex_spawn_start(profile, Some(slot), workspace)
}

fn resolve_codex_cli_profile_arg(
    positional: Option<String>,
    flag: Option<String>,
) -> Option<String> {
    flag.or(positional).map(|value| value.trim().to_owned()).filter(|value| !value.is_empty())
}

fn resolve_codex_workdir(workdir: Option<String>, workspace: &Path) -> Result<PathBuf> {
    let Some(workdir) = workdir.as_deref().map(str::trim).filter(|value| !value.is_empty()) else {
        return Ok(workspace.to_path_buf());
    };
    let path = expand_home_path(PathBuf::from(workdir));
    if path.is_absolute() { Ok(path) } else { Ok(workspace.join(path)) }
}

fn resolve_codex_workspace(workspace: Option<PathBuf>, settings: &Settings) -> Result<PathBuf> {
    if let Some(path) = workspace {
        return resolve_codex_workspace_path(path);
    }
    if let Some(configured) = settings.codex.workspace.as_deref() {
        let trimmed = configured.trim();
        if !trimmed.is_empty() {
            return resolve_codex_workspace_path(PathBuf::from(trimmed));
        }
    }
    std::env::current_dir().context("read current dir for codex workspace")
}

fn resolve_codex_workspace_path(path: PathBuf) -> Result<PathBuf> {
    let path = expand_home_path(path);
    if path.is_absolute() {
        Ok(path)
    } else {
        Ok(std::env::current_dir().context("read current dir for codex workspace")?.join(path))
    }
}

fn expand_home_path(path: PathBuf) -> PathBuf {
    let path_str = path.to_string_lossy();
    if path_str == "~" {
        return default_home_dir();
    }
    if let Some(suffix) = path_str.strip_prefix("~/") {
        return default_home_dir().join(suffix);
    }
    path
}

fn codex_workers_dir(paths: &SiPaths) -> PathBuf {
    paths.root.join("codex").join("workers")
}

fn default_codex_worker_slot_name() -> String {
    DEFAULT_CODEX_WORKER_SLOT.to_owned()
}

fn default_codex_worker_state_schema_version() -> u32 {
    1
}

fn normalize_codex_worker_slot(slot: Option<&str>) -> Result<String> {
    let slot = codex_worker_slot_name(slot);
    if slot == DEFAULT_CODEX_WORKER_SLOT {
        return Ok(slot);
    }
    let valid = slot.chars().enumerate().all(|(index, ch)| {
        ch.is_ascii_lowercase() || ch.is_ascii_digit() || (ch == '-' && index > 0)
    });
    if !valid || slot.starts_with('-') || slot.ends_with('-') {
        anyhow::bail!(
            "invalid worker slot {slot:?}; use lowercase letters, digits, and dashes (example: review-2)"
        );
    }
    Ok(slot)
}

fn codex_worker_legacy_state_path(paths: &SiPaths, profile_id: &str) -> PathBuf {
    codex_workers_dir(paths).join(format!("{}.json", codex_worker_name(profile_id)))
}

fn codex_worker_state_path(paths: &SiPaths, profile_id: &str, worker_slot: &str) -> PathBuf {
    let slot = codex_worker_slot_name(Some(worker_slot));
    codex_workers_dir(paths).join(codex_worker_name(profile_id)).join(format!("{slot}.json"))
}

fn codex_profile_worker_slot_home_dir(
    paths: &SiPaths,
    settings: &Settings,
    profile_id: &str,
    worker_slot: &str,
) -> PathBuf {
    let slot = codex_worker_slot_name(Some(worker_slot));
    let profile_home = codex_profile_home_dir(paths, settings, profile_id);
    if slot == DEFAULT_CODEX_WORKER_SLOT {
        profile_home
    } else {
        profile_home.join("workers").join(slot)
    }
}

fn ensure_codex_workers_dir(paths: &SiPaths) -> Result<PathBuf> {
    let dir = codex_workers_dir(paths);
    fs::create_dir_all(&dir).with_context(|| format!("create {}", dir.display()))?;
    Ok(dir)
}

fn save_codex_worker_state(path: &Path, state: &CodexWorkerState) -> Result<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).with_context(|| format!("create {}", parent.display()))?;
    }
    fs::write(path, serde_json::to_vec_pretty(state)?)
        .with_context(|| format!("write {}", path.display()))?;
    #[cfg(unix)]
    fs::set_permissions(path, fs::Permissions::from_mode(0o600))
        .with_context(|| format!("chmod {}", path.display()))?;
    Ok(())
}

fn load_codex_worker_state(path: &Path) -> Result<CodexWorkerState> {
    let raw = fs::read(path).with_context(|| format!("read {}", path.display()))?;
    let mut state: CodexWorkerState =
        serde_json::from_slice(&raw).with_context(|| format!("parse {}", path.display()))?;
    state.worker_slot = normalize_codex_worker_slot(Some(state.worker_slot.as_str()))?;
    Ok(state)
}

fn find_codex_worker_state(
    paths: &SiPaths,
    profile_id: &str,
    worker_slot: Option<&str>,
) -> Result<Option<CodexWorkerState>> {
    let slot = normalize_codex_worker_slot(worker_slot)?;
    let slot_path = codex_worker_state_path(paths, profile_id, &slot);
    if slot_path.is_file() {
        return load_codex_worker_state(&slot_path).map(Some);
    }
    if slot == DEFAULT_CODEX_WORKER_SLOT {
        let legacy_path = codex_worker_legacy_state_path(paths, profile_id);
        if legacy_path.is_file() {
            return load_codex_worker_state(&legacy_path).map(Some);
        }
    }
    Ok(None)
}

fn read_codex_worker_states(paths: &SiPaths) -> Result<Vec<CodexWorkerState>> {
    let dir = codex_workers_dir(paths);
    if !dir.is_dir() {
        return Ok(Vec::new());
    }
    let items = fs::read_dir(&dir)
        .with_context(|| format!("read {}", dir.display()))?
        .filter_map(|entry| entry.ok().map(|value| value.path()))
        .flat_map(|path| {
            if path.is_file() {
                vec![path]
            } else if path.is_dir() {
                fs::read_dir(path)
                    .ok()
                    .into_iter()
                    .flat_map(|iter| iter.filter_map(|entry| entry.ok().map(|value| value.path())))
                    .collect::<Vec<_>>()
            } else {
                Vec::new()
            }
        })
        .filter(|path| path.extension().and_then(|value| value.to_str()) == Some("json"))
        .filter_map(|path| load_codex_worker_state(&path).ok())
        .collect::<Vec<_>>();
    let mut merged = BTreeMap::<(String, String), CodexWorkerState>::new();
    for item in items {
        let key = (item.profile_id.clone(), item.worker_slot.clone());
        let replace = merged
            .get(&key)
            .is_none_or(|current| item.updated_at.as_str() >= current.updated_at.as_str());
        if replace {
            merged.insert(key, item);
        }
    }
    let mut items = merged.into_values().collect::<Vec<_>>();
    items.sort_by(|left, right| {
        left.profile_id.cmp(&right.profile_id).then(left.worker_slot.cmp(&right.worker_slot))
    });
    Ok(items)
}

fn codex_profile_display_name_from_settings(
    settings: &Settings,
    profile_id: &str,
) -> Option<String> {
    settings
        .codex
        .profiles
        .entries
        .get(profile_id)
        .and_then(|entry| entry.name.clone().or_else(|| entry.email.clone()))
        .map(|value| value.trim().to_owned())
        .filter(|value| !value.is_empty())
}

fn shell_single_quote(value: &str) -> String {
    if value.is_empty() {
        return "''".to_owned();
    }
    format!("'{}'", value.replace('\'', "'\"'\"'"))
}

fn codex_env_key_protected(key: &str) -> bool {
    matches!(
        key,
        "HOME"
            | "CODEX_HOME"
            | "FORT_TOKEN_PATH"
            | "FORT_REFRESH_TOKEN_PATH"
            | "SI_CODEX_PROFILE"
            | "SI_CODEX_WORKER_SLOT"
    )
}

fn build_codex_launch_command(
    home: &Path,
    codex_home: &Path,
    workdir: &Path,
    profile_id: &str,
    worker_slot: &str,
    env_entries: &[String],
    fort_paths: &CodexProfileFortSessionPaths,
) -> String {
    let mut exports = format!(
        "export COLORTERM=truecolor HOME={} CODEX_HOME={} FORT_TOKEN_PATH={} FORT_REFRESH_TOKEN_PATH={} SI_CODEX_PROFILE={} SI_CODEX_WORKER_SLOT={}; ",
        shell_single_quote(&home.display().to_string()),
        shell_single_quote(&codex_home.display().to_string()),
        shell_single_quote(&fort_paths.access_token_path.display().to_string()),
        shell_single_quote(&fort_paths.refresh_token_path.display().to_string()),
        shell_single_quote(profile_id),
        shell_single_quote(worker_slot),
    );
    for entry in env_entries {
        let Some((key, value)) = entry.split_once('=') else {
            continue;
        };
        let key = key.trim();
        if key.is_empty() || codex_env_key_protected(key) {
            continue;
        }
        exports.push_str(&format!("export {}={}; ", key, shell_single_quote(value.trim())));
    }
    format!(
        "bash -lc {}",
        shell_single_quote(&format!(
            "{exports}cd {} 2>/dev/null || exit 1; codex --dangerously-bypass-approvals-and-sandbox; status=$?; printf '\n[si] codex exited (status %s). Run codex again, or exit to close this pane.\n' \"$status\"; exec bash -il",
            shell_single_quote(&workdir.display().to_string())
        ))
    )
}

fn sync_codex_profile_auth_to_home(
    paths: &SiPaths,
    settings: &Settings,
    profile_id: &str,
    codex_home: &Path,
) -> Result<()> {
    let auth_path = codex_profile_auth_source_path(paths, settings, profile_id);
    if !auth_path.is_file() {
        return Ok(());
    }
    fs::create_dir_all(codex_home).with_context(|| format!("create {}", codex_home.display()))?;
    let target_path = codex_home.join("auth.json");
    if auth_path != target_path {
        fs::copy(&auth_path, &target_path).with_context(|| {
            format!("copy codex auth {} -> {}", auth_path.display(), target_path.display())
        })?;
    }
    #[cfg(unix)]
    fs::set_permissions(&target_path, fs::Permissions::from_mode(0o600))
        .with_context(|| format!("chmod {}", target_path.display()))?;
    Ok(())
}

#[derive(Debug)]
struct PreparedCodexProfileRuntime {
    codex_home: PathBuf,
    fort_paths: CodexProfileFortSessionPaths,
}

fn prepare_codex_profile_runtime(
    home: &Path,
    paths: &SiPaths,
    settings: &Settings,
    profile_id: &str,
    worker_slot: &str,
    codex_home_override: Option<PathBuf>,
) -> Result<PreparedCodexProfileRuntime> {
    let codex_home =
        codex_home_override.unwrap_or_else(|| codex_profile_home_dir(paths, settings, profile_id));
    fs::create_dir_all(&codex_home).with_context(|| format!("create {}", codex_home.display()))?;
    sync_codex_profile_auth_to_home(paths, settings, profile_id, &codex_home)?;
    let fort_paths =
        ensure_codex_profile_fort_session(home, settings, &codex_home, profile_id, worker_slot)?;
    Ok(PreparedCodexProfileRuntime { codex_home, fort_paths })
}

fn insert_codex_profile_fort_env(
    env: &mut BTreeMap<String, String>,
    fort_paths: &CodexProfileFortSessionPaths,
) {
    env.insert("FORT_TOKEN_PATH".to_owned(), fort_paths.access_token_path.display().to_string());
    env.insert(
        "FORT_REFRESH_TOKEN_PATH".to_owned(),
        fort_paths.refresh_token_path.display().to_string(),
    );
}

fn ensure_codex_worker_session(
    home: &Path,
    paths: &SiPaths,
    settings: &Settings,
    profile_id: &str,
    worker_slot: &str,
    workspace: PathBuf,
    workdir: PathBuf,
    env_entries: &[String],
) -> Result<CodexWorkerState> {
    let _ = ensure_codex_workers_dir(paths)?;
    let worker_slot = normalize_codex_worker_slot(Some(worker_slot))?;
    let profile_name = codex_profile_display_name_from_settings(settings, profile_id);
    let session_name = codex_tmux_session_name_for_slot(profile_id, &worker_slot);
    let state_path = codex_worker_state_path(paths, profile_id, &worker_slot);
    let lock_path = state_path.with_extension("lock");
    let _state_lock = acquire_session_lock(&lock_path)
        .with_context(|| format!("acquire worker-state lock {}", lock_path.display()))?;
    let codex_home = codex_profile_worker_slot_home_dir(paths, settings, profile_id, &worker_slot);
    let prepared = prepare_codex_profile_runtime(
        home,
        paths,
        settings,
        profile_id,
        &worker_slot,
        Some(codex_home),
    )?;

    let existing = find_codex_worker_state(paths, profile_id, Some(&worker_slot))?;
    let restart_needed = existing.as_ref().is_some_and(|state| {
        state.workspace != workspace.display().to_string()
            || state.workdir != workdir.display().to_string()
    });
    let tmux_bin = "tmux";
    let tmux_term = env::var("TERM").unwrap_or_else(|_| "xterm-256color".to_owned());
    if restart_needed {
        kill_tmux_session_if_present(tmux_bin, &tmux_term, &session_name);
    }
    if !tmux_session_exists(tmux_bin, &tmux_term, &session_name)? {
        let window_name = codex_tmux_window_name(profile_id, profile_name.as_deref(), &worker_slot);
        let launch_command = build_codex_launch_command(
            home,
            &prepared.codex_home,
            &workdir,
            profile_id,
            &worker_slot,
            env_entries,
            &prepared.fort_paths,
        );
        let status = StdCommand::new(tmux_bin)
            .env("TERM", &tmux_term)
            .args(["new-session", "-d", "-s", &session_name, "-n", &window_name, &launch_command])
            .status()
            .with_context(|| format!("create tmux session {session_name}"))?;
        if !status.success() {
            anyhow::bail!("tmux failed to create session {session_name}");
        }
    }

    let state = CodexWorkerState {
        schema_version: default_codex_worker_state_schema_version(),
        profile_id: profile_id.to_owned(),
        worker_slot,
        profile_name,
        session_name,
        workspace: workspace.display().to_string(),
        workdir: workdir.display().to_string(),
        updated_at: Utc::now().to_rfc3339(),
    };
    save_codex_worker_state(&state_path, &state)?;
    Ok(state)
}

fn kill_tmux_session_if_present(tmux_bin: &str, tmux_term: &str, session_name: &str) {
    if session_name.trim().is_empty() {
        return;
    }
    let Ok(true) = tmux_session_exists(tmux_bin, tmux_term, session_name) else {
        return;
    };
    let _ = std::process::Command::new(tmux_bin)
        .env("TERM", tmux_term)
        .args(["kill-session", "-t", session_name])
        .status();
}

fn prompt_codex_remove_all(states: &[CodexWorkerState]) -> Result<bool> {
    eprintln!(
        "{}",
        stderr_text(
            &format!("Remove all {} codex worker sessions?", states.len()),
            CliTone::Warning,
        )
    );
    for item in states {
        eprintln!(
            "{} {} [{}] ({})",
            stderr_text("-", CliTone::Muted),
            stderr_text(&item.profile_id, CliTone::Command),
            stderr_text(&item.worker_slot, CliTone::Label),
            item.session_name,
        );
    }
    eprint!("{} ", stderr_text("Type 'remove all' to confirm:", CliTone::Heading));
    io::stderr().flush().context("flush codex remove-all prompt")?;
    let mut input = String::new();
    io::stdin().read_line(&mut input).context("read codex remove-all confirmation")?;
    Ok(input.trim() == "remove all")
}

fn stop_codex_worker_state(state: &CodexWorkerState) -> Result<CodexStopResultView> {
    let tmux_bin = "tmux";
    let tmux_term = env::var("TERM").unwrap_or_else(|_| "xterm-256color".to_owned());
    kill_tmux_session_if_present(tmux_bin, &tmux_term, &state.session_name);
    Ok(CodexStopResultView {
        name: state.profile_id.clone(),
        session_name: state.session_name.clone(),
        profile_id: state.profile_id.clone(),
        output: format!("stopped {}\n", state.session_name),
    })
}

fn prompt_codex_stop_all(states: &[CodexWorkerState]) -> Result<bool> {
    eprintln!(
        "{}",
        stderr_text(&format!("Stop all {} codex worker sessions?", states.len()), CliTone::Warning,)
    );
    for item in states {
        eprintln!(
            "{} {} [{}] ({})",
            stderr_text("-", CliTone::Muted),
            stderr_text(&item.profile_id, CliTone::Command),
            stderr_text(&item.worker_slot, CliTone::Label),
            item.session_name,
        );
    }
    eprint!("{} ", stderr_text("Type 'stop all' to confirm:", CliTone::Heading));
    io::stderr().flush().context("flush codex stop-all prompt")?;
    let mut input = String::new();
    io::stdin().read_line(&mut input).context("read codex stop-all confirmation")?;
    Ok(input.trim() == "stop all")
}

fn resolve_codex_worker_for_profile(
    profile: Option<&str>,
    worker_slot: Option<&str>,
    purpose: &str,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
) -> Result<CodexWorkerState> {
    let (home, settings) = load_codex_runtime_settings(home, settings_file)?;
    let paths = SiPaths::from_settings(&home, &settings);
    let states = read_codex_worker_states(&paths)?;
    let requested_slot = worker_slot.map(str::trim).filter(|value| !value.is_empty());
    let requested_slot = match requested_slot {
        Some(slot) => Some(normalize_codex_worker_slot(Some(slot))?),
        None => None,
    };

    if let Some(query) = profile.map(str::trim).filter(|value| !value.is_empty()) {
        if let Some(state) = states.iter().find(|item| {
            item.session_name.eq_ignore_ascii_case(query)
                && requested_slot
                    .as_ref()
                    .is_none_or(|slot| item.worker_slot.eq_ignore_ascii_case(slot))
        }) {
            return Ok(state.clone());
        }
    }

    let profile_id = resolve_codex_requested_profile(&home, &settings, &paths, profile)?;
    let mut matches = states
        .into_iter()
        .filter(|state| state.profile_id == profile_id)
        .filter(|state| {
            requested_slot.as_ref().is_none_or(|slot| state.worker_slot.eq_ignore_ascii_case(slot))
        })
        .collect::<Vec<_>>();
    matches.sort_by(|left, right| left.worker_slot.cmp(&right.worker_slot));
    match matches.as_slice() {
        [state] => Ok(state.clone()),
        [] => {
            if let Some(slot) = requested_slot {
                Err(anyhow!(
                    "no codex worker session found for profile {profile_id:?} slot {slot:?}; run `si codex spawn --profile {profile_id} --slot {slot}` first"
                ))
            } else {
                Err(anyhow!(
                    "no codex worker session found for profile {profile_id:?}; run `si codex spawn --profile {profile_id}` first"
                ))
            }
        }
        _ => {
            let slots = matches
                .iter()
                .map(|state| state.worker_slot.clone())
                .collect::<Vec<_>>()
                .join(", ");
            Err(anyhow!(
                "multiple codex worker sessions found for profile {profile_id:?} ({slots}); pass --slot"
            ))
        }
    }
    .with_context(|| format!("resolve codex worker for {purpose}"))
}

fn remove_dir_if_empty(path: &Path) -> Result<()> {
    if !path.is_dir() {
        return Ok(());
    }
    let mut entries =
        fs::read_dir(path).with_context(|| format!("read directory {}", path.display()))?;
    if entries.next().is_none() {
        fs::remove_dir(path).with_context(|| format!("remove empty dir {}", path.display()))?;
    }
    Ok(())
}

fn clear_codex_worker_fort_state(
    paths: &SiPaths,
    settings: &Settings,
    home: &Path,
    state: &CodexWorkerState,
) -> Result<()> {
    let codex_home =
        codex_profile_worker_slot_home_dir(paths, settings, &state.profile_id, &state.worker_slot);
    let fort_paths = codex_profile_fort_session_paths(&codex_home);
    close_codex_worker_fort_session(home, &fort_paths)?;
    for path in [
        fort_paths.access_token_path.as_path(),
        fort_paths.refresh_token_path.as_path(),
        fort_paths.session_path.as_path(),
        fort_paths.lock_path.as_path(),
    ] {
        if path.exists() {
            fs::remove_file(path).with_context(|| format!("remove {}", path.display()))?;
        }
    }
    remove_dir_if_empty(&fort_paths.dir)?;
    if state.worker_slot != DEFAULT_CODEX_WORKER_SLOT {
        remove_dir_if_empty(&codex_home)?;
    }
    Ok(())
}

fn close_codex_worker_fort_session(
    home: &Path,
    fort_paths: &CodexProfileFortSessionPaths,
) -> Result<()> {
    if !fort_paths.refresh_token_path.is_file() && !fort_paths.session_path.is_file() {
        return Ok(());
    }
    let session_id = if fort_paths.session_path.is_file() {
        load_persisted_session_state(&fort_paths.session_path)
            .ok()
            .map(|value| value.normalized().session_id)
            .filter(|value| !value.trim().is_empty())
    } else {
        None
    };
    let program =
        std::env::current_exe().context("resolve si executable for Fort session close")?;
    let mut command = StdCommand::new(program);
    command.arg("fort").arg("--home").arg(home).arg("auth").arg("session").arg("close");
    if let Some(session_id) = session_id.as_deref() {
        command.arg("--session-id").arg(session_id);
    }
    if fort_paths.refresh_token_path.is_file() {
        command.arg("--refresh-token-file").arg(&fort_paths.refresh_token_path);
    }
    command.env_remove("FORT_TOKEN");
    command.env_remove("FORT_REFRESH_TOKEN");
    command.env_remove("FORT_TOKEN_PATH");
    command.env_remove("FORT_REFRESH_TOKEN_PATH");
    command.env_remove("FORT_BOOTSTRAP_TOKEN_FILE");
    let output = command.output().context("run Fort session close")?;
    if output.status.success() {
        return Ok(());
    }
    let stderr = String::from_utf8_lossy(&output.stderr).trim().to_owned();
    let stdout = String::from_utf8_lossy(&output.stdout).trim().to_owned();
    let detail = if !stderr.is_empty() {
        stderr
    } else if !stdout.is_empty() {
        stdout
    } else {
        format!("exit status {}", output.status)
    };
    if is_non_fatal_fort_session_close_error(&detail) {
        return Ok(());
    }
    Err(anyhow!("close Fort session before cleanup failed: {detail}"))
}

fn is_non_fatal_fort_session_close_error(detail: &str) -> bool {
    let detail = detail.to_ascii_lowercase();
    detail.contains("status=401")
        || detail.contains("status=403")
        || detail.contains("status=404")
        || detail.contains("token file auth required")
}

fn remove_codex_worker_state(
    paths: &SiPaths,
    settings: &Settings,
    home: &Path,
    state: &CodexWorkerState,
) -> Result<CodexRemoveResultView> {
    let tmux_bin = "tmux";
    let tmux_term = env::var("TERM").unwrap_or_else(|_| "xterm-256color".to_owned());
    kill_tmux_session_if_present(tmux_bin, &tmux_term, &state.session_name);
    let state_path = codex_worker_state_path(paths, &state.profile_id, &state.worker_slot);
    if state_path.exists() {
        fs::remove_file(&state_path).with_context(|| format!("remove {}", state_path.display()))?;
    }
    if state.worker_slot == DEFAULT_CODEX_WORKER_SLOT {
        let legacy_path = codex_worker_legacy_state_path(paths, &state.profile_id);
        if legacy_path.exists() {
            fs::remove_file(&legacy_path)
                .with_context(|| format!("remove {}", legacy_path.display()))?;
        }
    }
    clear_codex_worker_fort_state(paths, settings, home, state)?;
    Ok(CodexRemoveResultView {
        name: state.profile_id.clone(),
        session_name: state.session_name.clone(),
        profile_id: Some(state.profile_id.clone()),
        output: format!("removed {}\n", state.session_name),
    })
}

fn run_codex_remove_with_settings(
    profile: Option<&str>,
    worker_slot: Option<&str>,
    all: bool,
    format: OutputFormat,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
) -> Result<()> {
    let (home, settings) = load_codex_runtime_settings(home, settings_file)?;
    let paths = SiPaths::from_settings(&home, &settings);
    if all {
        let states = read_codex_worker_states(&paths)?;
        if states.is_empty() {
            match format {
                OutputFormat::Json => println!(
                    "{}",
                    serde_json::to_string_pretty(&CodexRemoveAllResultView {
                        aborted: false,
                        removed: Vec::new(),
                    })?
                ),
                OutputFormat::Text => println!("no codex worker sessions found"),
            }
            return Ok(());
        }
        let confirmed = prompt_codex_remove_all(&states)?;
        if !confirmed {
            match format {
                OutputFormat::Json => println!(
                    "{}",
                    serde_json::to_string_pretty(&CodexRemoveAllResultView {
                        aborted: true,
                        removed: Vec::new(),
                    })?
                ),
                OutputFormat::Text => println!("aborted"),
            }
            return Ok(());
        }
        let removed = states
            .iter()
            .map(|state| remove_codex_worker_state(&paths, &settings, &home, state))
            .collect::<Result<Vec<_>>>()?;
        match format {
            OutputFormat::Json => println!(
                "{}",
                serde_json::to_string_pretty(&CodexRemoveAllResultView {
                    aborted: false,
                    removed,
                })?
            ),
            OutputFormat::Text => {
                for item in removed {
                    print!("{}", item.output);
                }
            }
        }
        return Ok(());
    }

    let target =
        resolve_codex_worker_for_profile(profile, worker_slot, "remove", Some(home.clone()), None)?;
    let view = remove_codex_worker_state(&paths, &settings, &home, &target)?;
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => print!("{}", view.output),
    }
    Ok(())
}

fn run_codex_stop_with_settings(
    profile: Option<&str>,
    worker_slot: Option<&str>,
    all: bool,
    format: OutputFormat,
    home: Option<PathBuf>,
) -> Result<()> {
    let (home, settings) = load_codex_runtime_settings(home, None)?;
    let paths = SiPaths::from_settings(&home, &settings);
    if all {
        let states = read_codex_worker_states(&paths)?;
        if states.is_empty() {
            match format {
                OutputFormat::Json => println!(
                    "{}",
                    serde_json::to_string_pretty(&CodexStopAllResultView {
                        aborted: false,
                        stopped: Vec::new(),
                    })?
                ),
                OutputFormat::Text => println!("no codex worker sessions found"),
            }
            return Ok(());
        }
        let confirmed = prompt_codex_stop_all(&states)?;
        if !confirmed {
            match format {
                OutputFormat::Json => println!(
                    "{}",
                    serde_json::to_string_pretty(&CodexStopAllResultView {
                        aborted: true,
                        stopped: Vec::new(),
                    })?
                ),
                OutputFormat::Text => println!("aborted"),
            }
            return Ok(());
        }
        let stopped = states.iter().map(stop_codex_worker_state).collect::<Result<Vec<_>>>()?;
        match format {
            OutputFormat::Json => println!(
                "{}",
                serde_json::to_string_pretty(&CodexStopAllResultView { aborted: false, stopped })?
            ),
            OutputFormat::Text => {
                for item in stopped {
                    print!("{}", item.output);
                }
            }
        }
        return Ok(());
    }

    let target = resolve_codex_worker_for_profile(profile, worker_slot, "stop", None, None)?;
    let view = stop_codex_worker_state(&target)?;
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => print!("{}", view.output),
    }
    Ok(())
}

fn run_codex_stop(
    profile: Option<&str>,
    worker_slot: Option<&str>,
    all: bool,
    format: OutputFormat,
) -> Result<()> {
    run_codex_stop_with_settings(profile, worker_slot, all, format, None)
}

fn run_codex_remove(
    profile: Option<&str>,
    worker_slot: Option<&str>,
    all: bool,
    format: OutputFormat,
) -> Result<()> {
    run_codex_remove_with_settings(profile, worker_slot, all, format, None, None)
}

fn repair_codex_worker_fort_auth(
    home: &Path,
    paths: &SiPaths,
    settings: &Settings,
    state: &CodexWorkerState,
) -> Result<CodexRepairAuthResultView> {
    let worker_slot = normalize_codex_worker_slot(Some(&state.worker_slot))?;
    let codex_home =
        codex_profile_worker_slot_home_dir(paths, settings, &state.profile_id, &worker_slot);
    let fort_paths = codex_profile_fort_session_paths(&codex_home);
    let expected_agent_id = codex_fort_agent_id(&state.profile_id, &worker_slot);

    let previous_agent_id = if fort_paths.session_path.is_file() {
        Some(load_persisted_session_state(&fort_paths.session_path)?.normalized().agent_id)
    } else {
        None
    };

    let _prepared = prepare_codex_profile_runtime(
        home,
        paths,
        settings,
        &state.profile_id,
        &worker_slot,
        Some(codex_home),
    )?;

    let persisted = load_persisted_session_state(&fort_paths.session_path)
        .with_context(|| format!("load Fort session state {}", fort_paths.session_path.display()))?
        .normalized();
    if persisted.agent_id != expected_agent_id {
        anyhow::bail!(
            "Fort session repair wrote unexpected agent id {} for profile={} slot={}, expected {}",
            persisted.agent_id,
            state.profile_id,
            worker_slot,
            expected_agent_id
        );
    }
    let detail = match previous_agent_id {
        Some(previous) if previous == expected_agent_id => {
            format!("verified Fort session agent {expected_agent_id}")
        }
        Some(previous) => {
            format!("repaired Fort session agent {previous} -> {expected_agent_id}")
        }
        None => format!("provisioned Fort session agent {expected_agent_id}"),
    };
    let status = if detail.starts_with("verified") { "verified" } else { "repaired" }.to_owned();
    Ok(CodexRepairAuthResultView {
        profile_id: state.profile_id.clone(),
        worker_slot,
        agent_id: expected_agent_id,
        status,
        detail,
    })
}

fn run_codex_repair_auth(
    profile: Option<&str>,
    worker_slot: Option<&str>,
    all: bool,
    format: OutputFormat,
) -> Result<()> {
    let (home, settings) = load_codex_runtime_settings(None, None)?;
    let paths = SiPaths::from_settings(&home, &settings);

    if all {
        let states = read_codex_worker_states(&paths)?;
        if states.is_empty() {
            match format {
                OutputFormat::Json => println!(
                    "{}",
                    serde_json::to_string_pretty(&CodexRepairAuthAllResultView {
                        repaired: Vec::new(),
                    })?
                ),
                OutputFormat::Text => println!("no codex worker sessions found"),
            }
            return Ok(());
        }
        let repaired = states
            .iter()
            .map(|state| repair_codex_worker_fort_auth(&home, &paths, &settings, state))
            .collect::<Result<Vec<_>>>()?;
        match format {
            OutputFormat::Json => {
                println!(
                    "{}",
                    serde_json::to_string_pretty(&CodexRepairAuthAllResultView { repaired })?
                );
            }
            OutputFormat::Text => {
                for item in repaired {
                    println!(
                        "{} [{}] {}",
                        item.profile_id,
                        item.worker_slot,
                        stdout_text(&item.detail, CliTone::Info)
                    );
                }
            }
        }
        return Ok(());
    }

    let target = resolve_codex_worker_for_profile(profile, worker_slot, "repair-auth", None, None)?;
    let result = repair_codex_worker_fort_auth(&home, &paths, &settings, &target)?;
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&result)?),
        OutputFormat::Text => {
            println!(
                "{} [{}] {}",
                result.profile_id,
                result.worker_slot,
                stdout_text(&result.detail, CliTone::Info)
            );
        }
    }
    Ok(())
}

fn capture_tmux_session_output(session_name: &str, tail: &str) -> Result<String> {
    let tmux_bin = "tmux";
    let tmux_term = env::var("TERM").unwrap_or_else(|_| "xterm-256color".to_owned());
    if !tmux_session_exists(tmux_bin, &tmux_term, session_name)? {
        anyhow::bail!("tmux session {session_name:?} is not running");
    }
    let line_count = tail.trim().parse::<i32>().unwrap_or(200).max(1);
    let start = format!("-{line_count}");
    let output = StdCommand::new(tmux_bin)
        .env("TERM", &tmux_term)
        .args(["capture-pane", "-p", "-t", &format!("{session_name}:0.0"), "-S", &start])
        .output()
        .with_context(|| format!("capture tmux output for {session_name}"))?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        anyhow::bail!("tmux capture-pane failed: {}", stderr.trim());
    }
    Ok(String::from_utf8_lossy(&output.stdout).into_owned())
}

fn run_codex_tail(profile: Option<&str>, worker_slot: Option<&str>, tail: &str) -> Result<()> {
    let target = resolve_codex_worker_for_profile(profile, worker_slot, "tail", None, None)?;
    print!("{}", capture_tmux_session_output(&target.session_name, tail)?);
    Ok(())
}

fn run_codex_shell(
    profile: Option<&str>,
    worker_slot: Option<&str>,
    command: Vec<String>,
) -> Result<()> {
    if profile.is_none() && command.len() >= 2 && command[1] == "--" {
        anyhow::bail!(
            "legacy positional profile form for `si codex shell` is no longer supported; use `si codex shell --profile <profile> --slot <slot> -- <command>`"
        );
    }
    let (home, settings) = load_codex_runtime_settings(None, None)?;
    let paths = SiPaths::from_settings(&home, &settings);
    let target =
        resolve_codex_worker_for_profile(profile, worker_slot, "shell", Some(home.clone()), None)?;
    if command.is_empty() {
        anyhow::bail!("shell command is required");
    }
    let prepared = prepare_codex_profile_runtime(
        &home,
        &paths,
        &settings,
        &target.profile_id,
        &target.worker_slot,
        Some(codex_profile_worker_slot_home_dir(
            &paths,
            &settings,
            &target.profile_id,
            &target.worker_slot,
        )),
    )?;
    let resolved_workdir = PathBuf::from(target.workdir.clone());
    let mut process = StdCommand::new(&command[0]);
    process
        .args(&command[1..])
        .current_dir(&resolved_workdir)
        .env("HOME", &home)
        .env("CODEX_HOME", &prepared.codex_home)
        .env("FORT_TOKEN_PATH", &prepared.fort_paths.access_token_path)
        .env("FORT_REFRESH_TOKEN_PATH", &prepared.fort_paths.refresh_token_path)
        .env("SI_CODEX_PROFILE", &target.profile_id)
        .env("SI_CODEX_WORKER_SLOT", &target.worker_slot)
        .env("TERM", env::var("TERM").unwrap_or_else(|_| "xterm-256color".to_owned()));
    let status = process
        .stdin(std::process::Stdio::inherit())
        .stdout(std::process::Stdio::inherit())
        .stderr(std::process::Stdio::inherit())
        .status()
        .with_context(|| format!("run {}", command[0]))?;
    if !status.success() {
        anyhow::bail!("command failed: {status}");
    }
    Ok(())
}

fn run_codex_list(format: OutputFormat) -> Result<()> {
    let (home, settings) = load_codex_runtime_settings(None, None)?;
    let paths = SiPaths::from_settings(&home, &settings);
    let tmux_bin = "tmux";
    let tmux_term = env::var("TERM").unwrap_or_else(|_| "xterm-256color".to_owned());
    let items = read_codex_worker_states(&paths)?
        .into_iter()
        .map(|item| {
            let state = match tmux_session_exists(tmux_bin, &tmux_term, &item.session_name) {
                Ok(true) => "running",
                Ok(false) => "stopped",
                Err(_) => "unknown",
            };
            let tmux_window_name = codex_tmux_window_name(
                &item.profile_id,
                item.profile_name.as_deref(),
                &item.worker_slot,
            );
            CodexListEntryView {
                profile_id: item.profile_id,
                worker_slot: item.worker_slot,
                session_name: item.session_name,
                tmux_window_name,
                state: state.to_owned(),
                workspace: item.workspace,
                workdir: item.workdir,
            }
        })
        .collect::<Vec<_>>();
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&items)?),
        OutputFormat::Text => {
            if items.is_empty() {
                println!("no codex worker sessions found");
            } else {
                let mut table = Table::new();
                table
                    .load_preset(UTF8_FULL)
                    .apply_modifier(UTF8_ROUND_CORNERS)
                    .set_content_arrangement(ContentArrangement::Dynamic)
                    .set_truncation_indicator("…");
                if let Some(width) = env::var("COLUMNS")
                    .ok()
                    .and_then(|value| value.trim().parse::<u16>().ok())
                    .filter(|width| *width > 20)
                {
                    table.set_width(width);
                }
                table.set_header(vec![
                    Cell::new("Profile")
                        .fg(cli_table_color(CliTone::Section))
                        .add_attribute(Attribute::Bold),
                    Cell::new("Slot")
                        .fg(cli_table_color(CliTone::Label))
                        .add_attribute(Attribute::Bold),
                    Cell::new("State")
                        .fg(cli_table_color(CliTone::Success))
                        .add_attribute(Attribute::Bold),
                ]);
                table.set_constraints(vec![
                    ColumnConstraint::UpperBoundary(Width::Fixed(24)),
                    ColumnConstraint::UpperBoundary(Width::Fixed(20)),
                    ColumnConstraint::Absolute(Width::Fixed(12)),
                ]);
                for item in items {
                    let state_tone =
                        if item.state == "running" { CliTone::Success } else { CliTone::Warning };
                    table.add_row(vec![
                        Cell::new(item.profile_id).fg(cli_table_color(CliTone::Section)),
                        Cell::new(item.worker_slot).fg(cli_table_color(CliTone::Label)),
                        Cell::new(item.state).fg(cli_table_color(state_tone)),
                    ]);
                }
                println!("{table}");
            }
        }
    }
    Ok(())
}

fn run_local_codex_app_server_status(
    home: &Path,
    codex_home: &Path,
    fort_paths: &CodexProfileFortSessionPaths,
    workdir: &Path,
    profile_id: &str,
    worker_slot: &str,
) -> Result<CodexStatusView> {
    let probe = |stdin_grace: Duration| -> Result<CodexStatusView> {
        let mut child = StdCommand::new("codex")
            .arg("--dangerously-bypass-approvals-and-sandbox")
            .arg("app-server")
            .current_dir(workdir)
            .env("HOME", home)
            .env("CODEX_HOME", codex_home)
            .env("FORT_TOKEN_PATH", &fort_paths.access_token_path)
            .env("FORT_REFRESH_TOKEN_PATH", &fort_paths.refresh_token_path)
            .env("SI_CODEX_PROFILE", profile_id)
            .env("SI_CODEX_WORKER_SLOT", worker_slot)
            .env("TERM", "xterm-256color")
            .stdin(std::process::Stdio::piped())
            .stdout(std::process::Stdio::piped())
            .stderr(std::process::Stdio::piped())
            .spawn()
            .context("run codex app-server")?;
        {
            let stdin =
                child.stdin.as_mut().ok_or_else(|| anyhow!("missing codex app-server stdin"))?;
            stdin
                .write_all(&build_app_server_status_input(Some(workdir.display().to_string())))
                .context("write codex app-server input")?;
            stdin.flush().context("flush codex app-server input")?;
            if !stdin_grace.is_zero() {
                thread::sleep(stdin_grace);
            }
        }
        let output = child.wait_with_output().context("wait for codex app-server")?;
        let stdout = String::from_utf8_lossy(&output.stdout);
        let stderr = String::from_utf8_lossy(&output.stderr);
        let mut combined = stdout.trim().to_owned();
        if !stderr.trim().is_empty() {
            if !combined.is_empty() {
                combined.push('\n');
            }
            combined.push_str(stderr.trim());
        }
        if !output.status.success() {
            anyhow::bail!(if combined.is_empty() {
                "codex app-server failed".to_owned()
            } else {
                combined
            });
        }
        parse_app_server_status(&combined).map(|mut status| {
            status.raw = Some(combined);
            status
        })
    };

    match probe(Duration::ZERO) {
        Ok(status) => Ok(status),
        Err(_) => probe(Duration::from_secs(3)),
    }
}

fn read_codex_status_for_profile(
    profile_id: &str,
    home: &Path,
    paths: &SiPaths,
    settings: &Settings,
    workspace: Option<PathBuf>,
    raw: bool,
) -> Result<CodexStatusView> {
    let selected = find_codex_worker_state(paths, profile_id, Some(DEFAULT_CODEX_WORKER_SLOT))?;
    let (worker_slot, workdir) = if let Some(state) = selected {
        (state.worker_slot, PathBuf::from(state.workdir))
    } else {
        (
            DEFAULT_CODEX_WORKER_SLOT.to_owned(),
            if let Some(workspace) = workspace {
                resolve_codex_workdir(None, &workspace)?
            } else if let Some(configured) = settings.codex.workspace.as_deref() {
                resolve_codex_workspace_path(PathBuf::from(configured))?
            } else {
                std::env::current_dir().context("read current dir for codex status")?
            },
        )
    };
    let prepared = prepare_codex_profile_runtime(
        home,
        paths,
        settings,
        profile_id,
        &worker_slot,
        Some(codex_profile_worker_slot_home_dir(paths, settings, profile_id, &worker_slot)),
    )?;
    let mut status = run_local_codex_app_server_status(
        home,
        &prepared.codex_home,
        &prepared.fort_paths,
        &workdir,
        profile_id,
        &worker_slot,
    )?;
    if !raw {
        status.raw = None;
    }
    Ok(status)
}

const CODEX_WARMUP_WEEKLY_JITTER_MAX_SECS: i64 = 300;
const CODEX_WARMUP_DEFAULT_MAX_TURNS: u32 = 4;
const CODEX_WARMUP_DEFAULT_TURN_TIMEOUT_SECONDS: u64 = 180;
const CODEX_WARMUP_PROMPT: &str = "SI Codex warmup. Reply with exactly: si-codex-warmup-ok. Do not inspect files, read files, write files, run commands, or modify anything.";

fn codex_warmup_weekly_quota_reached(status: &CodexStatusView) -> bool {
    status.weekly_left_pct.is_some_and(|value| value < 100.0)
}

fn codex_warmup_weekly_used_pct(status: &CodexStatusView) -> Option<f64> {
    status.weekly_left_pct.map(|value| 100.0 - value)
}

fn codex_status_from_runtime_snapshot(snapshot: RuntimeStatusSnapshot) -> CodexStatusView {
    CodexStatusView {
        source: Some(snapshot.source),
        raw: None,
        model: snapshot.model,
        reasoning_effort: snapshot.reasoning_effort,
        account_email: snapshot.account_email,
        account_plan: snapshot.account_plan,
        five_hour_left_pct: snapshot.five_hour_left_pct,
        five_hour_reset: snapshot.five_hour_reset,
        five_hour_remaining_minutes: snapshot.five_hour_remaining_minutes,
        weekly_left_pct: snapshot.weekly_left_pct,
        weekly_reset: snapshot.weekly_reset,
        weekly_remaining_minutes: snapshot.weekly_remaining_minutes,
    }
}

#[derive(Debug)]
struct CodexWarmupProfileOutcome {
    action: String,
    result: String,
    error: Option<String>,
    turn_count: u32,
    status: CodexStatusView,
}

fn run_nucleus_codex_warmup_profile(
    profile_id: &str,
    home: &Path,
    paths: &SiPaths,
    settings: &Settings,
    workspace: Option<PathBuf>,
    max_turns: u32,
    turn_timeout_seconds: u64,
) -> Result<CodexWarmupProfileOutcome> {
    if max_turns == 0 {
        anyhow::bail!("max_turns must be greater than zero");
    }
    if turn_timeout_seconds == 0 {
        anyhow::bail!("turn_timeout_seconds must be greater than zero");
    }

    let prepared = prepare_codex_profile_runtime(
        home,
        paths,
        settings,
        profile_id,
        DEFAULT_CODEX_WORKER_SLOT,
        None,
    )?;
    let resolved_workspace = resolve_codex_workspace(workspace, settings)?;
    let workdir = resolve_codex_workdir(None, &resolved_workspace)?;
    let worker_id = WorkerId::generate();
    let profile = ProfileName::new(profile_id.to_owned())?;
    let launch_spec = WorkerLaunchSpec {
        worker_id: worker_id.clone(),
        profile: profile.clone(),
        worker_slot: DEFAULT_CODEX_WORKER_SLOT.to_owned(),
        home_dir: home.to_path_buf(),
        codex_home: prepared.codex_home,
        workdir: workdir.clone(),
        extra_env: BTreeMap::new(),
    };
    let runtime = CodexNucleusRuntime::new();

    let result = (|| {
        let started = runtime.start_worker(&launch_spec)?;
        let mut status = codex_status_from_runtime_snapshot(started.probe.snapshot);
        if codex_warmup_weekly_quota_reached(&status) {
            return Ok(CodexWarmupProfileOutcome {
                action: "already-warm".to_owned(),
                result: "warmed".to_owned(),
                error: None,
                turn_count: 0,
                status,
            });
        }

        let session_id = SessionId::generate();
        let session = runtime.ensure_session(&SessionOpenSpec {
            session_id: session_id.clone(),
            worker_id: worker_id.clone(),
            profile: profile.clone(),
            workdir,
            resume_thread_id: None,
        })?;

        let mut turn_count = 0;
        for attempt in 1..=max_turns {
            let input_text = if attempt == 1 {
                CODEX_WARMUP_PROMPT.to_owned()
            } else {
                format!("{CODEX_WARMUP_PROMPT} Warmup attempt {attempt} of {max_turns}.")
            };
            let outcome = runtime.execute_turn(
                &RunTurnSpec {
                    run_id: RunId::generate(),
                    task_id: None,
                    worker_id: worker_id.clone(),
                    session_id: session_id.clone(),
                    profile: profile.clone(),
                    thread_id: session.thread_id.clone(),
                    timeout_seconds: Some(turn_timeout_seconds),
                    input: vec![RunInputItem::Text { text: input_text }],
                },
                &mut |_| Ok(()),
            )?;
            turn_count += 1;
            if outcome.status != RunStatus::Completed {
                anyhow::bail!("warmup turn ended with status {:?}", outcome.status);
            }

            let probe = runtime.probe_worker(&launch_spec)?;
            status = codex_status_from_runtime_snapshot(probe.snapshot);
            if codex_warmup_weekly_quota_reached(&status) {
                return Ok(CodexWarmupProfileOutcome {
                    action: "nucleus-burned".to_owned(),
                    result: "warmed".to_owned(),
                    error: None,
                    turn_count,
                    status,
                });
            }
        }

        let error = match status.weekly_left_pct {
            Some(value) if value >= 100.0 => {
                format!("weekly quota is still 100% left after {turn_count} Nucleus warmup turn(s)")
            }
            Some(_) => format!(
                "weekly quota did not drop below 100% after {turn_count} Nucleus warmup turn(s); weekly quota is still {} left",
                render_option_percent_value(status.weekly_left_pct),
            ),
            None => format!(
                "live weekly quota was not reported after {turn_count} Nucleus warmup turn(s)"
            ),
        };
        Ok(CodexWarmupProfileOutcome {
            action: "quota-unchanged".to_owned(),
            result: "failed".to_owned(),
            error: Some(error),
            turn_count,
            status,
        })
    })();

    let stop_result = runtime.stop_worker(&worker_id);
    match (result, stop_result) {
        (Ok(outcome), Ok(())) => Ok(outcome),
        (Ok(_), Err(err)) => Err(err.context("stop Nucleus warmup worker")),
        (Err(err), Ok(())) => Err(err),
        (Err(err), Err(stop_err)) => {
            Err(err.context(format!("also failed to stop Nucleus warmup worker: {stop_err}")))
        }
    }
}

fn codex_profile_auth_path_from_settings(
    paths: &SiPaths,
    settings: &Settings,
    profile_id: &str,
) -> Option<PathBuf> {
    settings.codex.profiles.entries.get(profile_id).map(|entry| {
        entry
            .auth_path
            .as_deref()
            .map(PathBuf::from)
            .unwrap_or_else(|| PathBuf::from(default_codex_profile_auth_path(paths, profile_id)))
    })
}

fn codex_profile_auth_source_path(
    paths: &SiPaths,
    settings: &Settings,
    profile_id: &str,
) -> PathBuf {
    codex_profile_auth_path_from_settings(paths, settings, profile_id)
        .unwrap_or_else(|| PathBuf::from(default_codex_profile_auth_path(paths, profile_id)))
}

fn codex_warmup_profile_jitter_seconds(profile_id: &str) -> i64 {
    if profile_id.trim().is_empty() {
        return 0;
    }
    profile_id
        .bytes()
        .fold(0_i64, |acc, byte| acc.wrapping_add(i64::from(byte)))
        .rem_euclid(CODEX_WARMUP_WEEKLY_JITTER_MAX_SECS + 1)
}

fn codex_warmup_next_due(status: &CodexStatusView, profile_id: &str, now: DateTime<Utc>) -> String {
    let remaining_minutes =
        status.weekly_remaining_minutes.or(status.five_hour_remaining_minutes).map(i64::from);
    let Some(remaining_minutes) = remaining_minutes else {
        return String::new();
    };
    let due_at = now
        + chrono::Duration::minutes(remaining_minutes)
        + chrono::Duration::seconds(codex_warmup_profile_jitter_seconds(profile_id));
    due_at.to_rfc3339_opts(chrono::SecondsFormat::Secs, true)
}

fn run_codex_warmup(
    profile: Option<String>,
    all: bool,
    path: Option<PathBuf>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    workspace: Option<PathBuf>,
    max_turns: u32,
    turn_timeout_seconds: u64,
    format: OutputFormat,
) -> Result<()> {
    let (settings_home, settings) =
        load_codex_runtime_settings(home.clone(), settings_file.clone())?;
    let paths = SiPaths::from_settings(&settings_home, &settings);
    let state_path = match path {
        Some(path) => path,
        None => default_warmup_state_path(Some(settings_home.as_path()))?,
    };
    let mut state = load_warmup_state(&state_path)?;
    let updated_at = chrono::Utc::now();
    let profile_ids = if let Some(profile) =
        profile.as_deref().map(str::trim).filter(|value| !value.is_empty())
    {
        vec![resolve_codex_requested_profile(&settings_home, &settings, &paths, Some(profile))?]
    } else if all || profile.is_none() {
        let mut items = settings.codex.profiles.entries.keys().cloned().collect::<Vec<_>>();
        items.sort();
        items
    } else {
        Vec::new()
    };
    if profile_ids.is_empty() {
        anyhow::bail!("no codex profiles are configured");
    }

    let mut results = Vec::with_capacity(profile_ids.len());

    for profile_id in profile_ids {
        let previous = state.profiles.get(profile_id.as_str()).cloned();
        let last_weekly_used_pct =
            previous.as_ref().map(|row| row.last_weekly_used_pct).unwrap_or(0.0);
        let last_weekly_used_ok = previous.as_ref().is_some_and(|row| row.last_weekly_used_ok);
        let entry = state.profiles.entry(profile_id.clone()).or_default();
        entry.profile_id = profile_id.clone();
        entry.last_attempt = updated_at.to_rfc3339();

        match run_nucleus_codex_warmup_profile(
            &profile_id,
            &settings_home,
            &paths,
            &settings,
            workspace.clone(),
            max_turns,
            turn_timeout_seconds,
        ) {
            Ok(outcome) => {
                let status = outcome.status;
                let weekly_used_pct_view = codex_warmup_weekly_used_pct(&status);
                let weekly_used_pct = weekly_used_pct_view.unwrap_or(0.0);
                entry.last_result = outcome.result.clone();
                entry.last_error = outcome.error.clone().unwrap_or_default();
                entry.last_weekly_used_pct = weekly_used_pct;
                entry.last_weekly_used_ok = status.weekly_left_pct.is_some();
                entry.last_weekly_reset = status.weekly_reset.clone().unwrap_or_default();
                if outcome.result == "warmed" {
                    entry.last_warmed_reset = status.weekly_reset.clone().unwrap_or_default();
                } else {
                    entry.last_warmed_reset.clear();
                }
                entry.last_usage_delta =
                    if last_weekly_used_ok { weekly_used_pct - last_weekly_used_pct } else { 0.0 };
                if outcome.result == "warmed" {
                    entry.next_due = codex_warmup_next_due(&status, &profile_id, updated_at);
                    entry.failure_count = 0;
                    entry.paused = false;
                } else {
                    entry.next_due.clear();
                    entry.failure_count += 1;
                }
                results.push(CodexWarmupRunProfileView {
                    profile_id,
                    action: outcome.action,
                    result: outcome.result,
                    turn_count: outcome.turn_count,
                    error: outcome.error,
                    account_email: status.account_email,
                    account_plan: status.account_plan,
                    five_hour_left_pct: status.five_hour_left_pct,
                    five_hour_reset: status.five_hour_reset,
                    weekly_left_pct: status.weekly_left_pct,
                    weekly_used_pct: weekly_used_pct_view,
                    weekly_reset: status.weekly_reset,
                });
            }
            Err(err) => {
                entry.last_result = "failed".to_owned();
                entry.last_error = err.to_string();
                entry.last_warmed_reset.clear();
                entry.next_due.clear();
                entry.failure_count += 1;
                results.push(CodexWarmupRunProfileView {
                    profile_id,
                    action: "nucleus-failed".to_owned(),
                    result: "failed".to_owned(),
                    turn_count: 0,
                    error: Some(entry.last_error.clone()),
                    account_email: None,
                    account_plan: None,
                    five_hour_left_pct: None,
                    five_hour_reset: None,
                    weekly_left_pct: None,
                    weekly_used_pct: None,
                    weekly_reset: None,
                });
            }
        }
    }

    state.version = si_warmup::WARMUP_STATE_VERSION;
    state.updated_at = updated_at.to_rfc3339();
    save_warmup_state(&state_path, &state)?;

    let view = CodexWarmupRunView {
        updated_at: state.updated_at,
        state_path: state_path.display().to_string(),
        profiles: results,
    };
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            print_cli_kv("updated_at", &view.updated_at);
            print_cli_kv("state_path", &view.state_path);
            for profile in view.profiles {
                println!(
                    "{}\taction={}\tresult={}\tturns={}\tfive_hour_left={}\tweekly_left={}\tweekly_used={}\terror={}",
                    stdout_text(&profile.profile_id, CliTone::Command),
                    stdout_text(&profile.action, CliTone::Info),
                    stdout_text(
                        &profile.result,
                        if profile.result == "warmed" { CliTone::Success } else { CliTone::Danger },
                    ),
                    profile.turn_count,
                    render_option_percent_value(profile.five_hour_left_pct),
                    render_option_percent_value(profile.weekly_left_pct),
                    render_option_percent_value(profile.weekly_used_pct),
                    profile
                        .error
                        .map(|value| stdout_text(&value, CliTone::Danger))
                        .unwrap_or_else(|| stdout_text("-", CliTone::Muted))
                );
            }
        }
    }
    Ok(())
}

fn run_codex_tmux_command(
    profile: Option<&str>,
    worker_slot: Option<&str>,
    format: Option<OutputFormat>,
) -> Result<()> {
    let (home, settings) = load_codex_runtime_settings(None, None)?;
    let paths = SiPaths::from_settings(&home, &settings);
    let target =
        resolve_codex_worker_for_profile(profile, worker_slot, "tmux", Some(home.clone()), None)?;
    let profile_display_name = target.profile_name.as_deref();
    let session_name = target.session_name.clone();
    let window_name =
        codex_tmux_window_name(&target.profile_id, profile_display_name, &target.worker_slot);
    let view = CodexTmuxCommandView {
        profile_id: target.profile_id.clone(),
        worker_slot: target.worker_slot.clone(),
        session_name: session_name.clone(),
        window_name: window_name.clone(),
        launch_command: format!("tmux attach-session -t {}", shell_single_quote(&session_name)),
        workspace: target.workspace.clone(),
    };
    if let Some(format) = format {
        match format {
            OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
            OutputFormat::Text => {
                print_cli_kv("profile_id", &view.profile_id);
                print_cli_kv("worker_slot", &view.worker_slot);
                print_cli_kv("session_name", &view.session_name);
                print_cli_kv("window_name", &view.window_name);
                print_cli_kv("workspace", &view.workspace);
                print_cli_kv("launch_command", &view.launch_command);
            }
        }
        return Ok(());
    }

    let tmux_bin = "tmux";
    let term = env::var("TERM").unwrap_or_default();
    let tmux_term = if term.trim().is_empty() || term.trim() == "dumb" {
        "xterm-256color"
    } else {
        term.trim()
    };
    if !tmux_session_exists(tmux_bin, tmux_term, &session_name)? {
        let workspace = PathBuf::from(target.workspace.clone());
        let workdir = PathBuf::from(target.workdir.clone());
        let _ = ensure_codex_worker_session(
            &home,
            &paths,
            &settings,
            &target.profile_id,
            &target.worker_slot,
            workspace,
            workdir,
            &[],
        )?;
    }
    apply_codex_tmux_repository_config(tmux_bin, tmux_term);
    apply_codex_tmux_window_identity(tmux_bin, tmux_term, &session_name, &window_name);

    let tmux_env = env::var_os("TMUX");
    let attach_args = if tmux_env.is_some() {
        vec!["switch-client", "-t", session_name.as_str()]
    } else {
        vec!["attach-session", "-t", session_name.as_str()]
    };
    let status = std::process::Command::new(tmux_bin)
        .env("TERM", tmux_term)
        .args(&attach_args)
        .status()
        .with_context(|| format!("attach tmux session {session_name}"))?;
    if !status.success() {
        anyhow::bail!("tmux failed to attach to session {session_name}");
    }
    Ok(())
}

fn sanitize_codex_tmux_profile_label(raw: &str) -> Option<String> {
    let mut out = String::new();
    let mut last_was_space = false;
    for ch in raw.chars() {
        if matches!(ch, ':' | '\n' | '\r' | '\0') {
            continue;
        }
        if ch.is_whitespace() {
            if !out.is_empty() && !last_was_space {
                out.push(' ');
                last_was_space = true;
            }
            continue;
        }
        out.push(ch);
        last_was_space = false;
    }
    let trimmed = out.trim();
    if trimmed.is_empty() { None } else { Some(trimmed.to_owned()) }
}

fn codex_repo_emoji_for_slot(worker_slot: &str) -> Option<&'static str> {
    let slot = codex_worker_slot_name(Some(worker_slot));
    match slot.as_str() {
        "si" => Some("⚛️"),
        "si-nucleus" => Some("⚛️"),
        "lingospeak" => Some("💬"),
        _ => None,
    }
}

fn codex_tmux_window_name(
    profile_id: &str,
    profile_display_name: Option<&str>,
    worker_slot: &str,
) -> String {
    let profile_label = profile_display_name
        .and_then(sanitize_codex_tmux_profile_label)
        .unwrap_or_else(|| profile_id.trim().to_owned());
    let slot = codex_worker_slot_name(Some(worker_slot));
    if let Some(emoji) = codex_repo_emoji_for_slot(&slot) {
        format!("{profile_label} {emoji} [{slot}]")
    } else {
        format!("{profile_label} [{slot}]")
    }
}

fn tmux_session_exists(tmux_bin: &str, tmux_term: &str, session_name: &str) -> Result<bool> {
    let mut command = std::process::Command::new(tmux_bin);
    command
        .env("TERM", tmux_term)
        .args(["has-session", "-t", session_name])
        .stderr(std::process::Stdio::null());
    let status = command.status().with_context(|| format!("check tmux session {session_name}"))?;
    Ok(status.success())
}

fn codex_tmux_config_path() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .ancestors()
        .nth(3)
        .expect("repo root from crate manifest")
        .join("tools")
        .join("tmux")
        .join("codex-session.tmux.conf")
}

fn apply_codex_tmux_repository_config(tmux_bin: &str, tmux_term: &str) {
    let config_path = codex_tmux_config_path();
    if !config_path.is_file() {
        return;
    }
    let _ = std::process::Command::new(tmux_bin)
        .env("TERM", tmux_term)
        .args(["source-file", &config_path.display().to_string()])
        .status();
}

fn apply_codex_tmux_window_identity(
    tmux_bin: &str,
    tmux_term: &str,
    session_name: &str,
    window_name: &str,
) {
    if session_name.trim().is_empty() || window_name.trim().is_empty() {
        return;
    }
    let target_window = format!("{session_name}:0");
    let target_pane = format!("{session_name}:0.0");
    let _ = std::process::Command::new(tmux_bin)
        .env("TERM", tmux_term)
        .args(["rename-window", "-t", &target_window, window_name])
        .status();
    let _ = std::process::Command::new(tmux_bin)
        .env("TERM", tmux_term)
        .args(["select-pane", "-t", &target_pane, "-T", window_name])
        .status();
}

fn build_app_server_status_input(cwd: Option<String>) -> Vec<u8> {
    build_codex_app_server_status_input("si", si_core::version::current_version(), cwd)
}

fn parse_app_server_status(raw: &str) -> Result<CodexStatusView> {
    let status = parse_codex_app_server_status(raw)?;
    Ok(CodexStatusView {
        source: Some(status.source),
        raw: None,
        model: status.model,
        reasoning_effort: status.reasoning_effort,
        account_email: status.account_email,
        account_plan: status.account_plan,
        five_hour_left_pct: status.five_hour_left_pct,
        five_hour_reset: status.five_hour_reset,
        five_hour_remaining_minutes: status.five_hour_remaining_minutes,
        weekly_left_pct: status.weekly_left_pct,
        weekly_reset: status.weekly_reset,
        weekly_remaining_minutes: status.weekly_remaining_minutes,
    })
}

fn run_warmup_status(
    path: Option<PathBuf>,
    home: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let path = match path {
        Some(path) => path,
        None => default_warmup_state_path(home.as_deref())?,
    };
    let state = load_warmup_state(path)?;
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&state)?),
        OutputFormat::Text => print!("{}", render_warmup_state_text(&state, chrono::Utc::now())),
    }
    Ok(())
}

fn run_warmup_autostart_decision(
    state_path: Option<PathBuf>,
    autostart_path: Option<PathBuf>,
    disabled_path: Option<PathBuf>,
    home: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let state_path = match state_path {
        Some(path) => path,
        None => default_warmup_state_path(home.as_deref())?,
    };
    let autostart_path = match autostart_path {
        Some(path) => path,
        None => default_autostart_marker_path(home.as_deref())?,
    };
    let disabled_path = match disabled_path {
        Some(path) => path,
        None => default_disabled_marker_path(home.as_deref())?,
    };
    let marker_state = read_warmup_marker_state(autostart_path, disabled_path)?;
    let state = load_warmup_state(state_path)?;
    let decision = classify_autostart_request(&marker_state, &state, chrono::Utc::now());
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&decision)?),
        OutputFormat::Text => {
            println!("requested={} reason={}", decision.requested, decision.reason)
        }
    }
    Ok(())
}

fn write_warmup_state(path: PathBuf, state_json: &str) -> Result<()> {
    let state: WarmupState = serde_json::from_str(state_json)?;
    save_warmup_state(path, &state)?;
    Ok(())
}

fn run_warmup_marker_show(
    autostart_path: Option<PathBuf>,
    disabled_path: Option<PathBuf>,
    home: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let autostart_path = match autostart_path {
        Some(path) => path,
        None => default_autostart_marker_path(home.as_deref())?,
    };
    let disabled_path = match disabled_path {
        Some(path) => path,
        None => default_disabled_marker_path(home.as_deref())?,
    };
    let state = read_warmup_marker_state(autostart_path, disabled_path)?;
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&state)?),
        OutputFormat::Text => {
            println!("disabled={} autostart_present={}", state.disabled, state.autostart_present)
        }
    }
    Ok(())
}

fn write_warmup_autostart_marker(path: PathBuf) -> Result<()> {
    write_rust_warmup_autostart_marker(path, chrono::Utc::now())?;
    Ok(())
}

fn set_warmup_disabled_marker(path: PathBuf, disabled: &str) -> Result<()> {
    let disabled = disabled
        .trim()
        .parse::<bool>()
        .map_err(|_| anyhow::anyhow!("invalid bool for --disabled: {disabled}"))?;
    set_rust_warmup_disabled_marker(path, disabled)?;
    Ok(())
}

fn default_home_dir() -> PathBuf {
    std::env::var_os("HOME")
        .map(PathBuf::from)
        .filter(|path| !path.as_os_str().is_empty())
        .unwrap_or_else(|| PathBuf::from("/"))
}

fn command_view(spec: &CommandSpec) -> CommandView {
    CommandView {
        name: spec.name.to_owned(),
        aliases: spec.aliases.iter().map(|alias| (*alias).to_owned()).collect(),
        category: spec.category,
        summary: spec.summary.to_owned(),
    }
}

fn format_category(category: CommandCategory) -> &'static str {
    match category {
        CommandCategory::Meta => "meta",
        CommandCategory::Codex => "codex",
        CommandCategory::Provider => "provider",
        CommandCategory::Runtime => "runtime",
        CommandCategory::Build => "build",
        CommandCategory::Developer => "developer",
        CommandCategory::Profile => "profile",
        CommandCategory::Internal => "internal",
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[test]
    fn configured_checkout_target_dir_resolves_relative_path() {
        let repo = tempdir().expect("repo tempdir");
        fs::create_dir_all(repo.path().join(".cargo")).expect("mkdir cargo dir");
        fs::write(
            repo.path().join(".cargo/config.toml"),
            "[build]\ntarget-dir = \".artifacts/cargo-target\"\n",
        )
        .expect("write cargo config");

        assert_eq!(
            configured_checkout_target_dir(repo.path()),
            Some(repo.path().join(".artifacts").join("cargo-target"))
        );
    }

    #[test]
    fn existing_checkout_binary_prefers_configured_target_dir() {
        let repo = tempdir().expect("repo tempdir");
        fs::create_dir_all(repo.path().join(".cargo")).expect("mkdir cargo dir");
        fs::write(
            repo.path().join(".cargo/config.toml"),
            "[build]\ntarget-dir = \".artifacts/cargo-target\"\n",
        )
        .expect("write cargo config");
        let binary = repo
            .path()
            .join(".artifacts")
            .join("cargo-target")
            .join("debug")
            .join(if cfg!(windows) { "viva.exe" } else { "viva" });
        fs::create_dir_all(binary.parent().expect("binary parent")).expect("mkdir binary parent");
        fs::write(&binary, "#!/bin/sh\n").expect("write binary");

        assert_eq!(existing_checkout_binary(repo.path(), "viva"), Some(binary));
    }

    #[test]
    fn external_tool_resolver_uses_explicit_repo_binary() {
        let repo = tempdir().expect("repo tempdir");
        let binary = repo.path().join("target").join("debug").join(if cfg!(windows) {
            "fort.exe"
        } else {
            "fort"
        });
        fs::create_dir_all(binary.parent().expect("binary parent")).expect("mkdir binary parent");
        fs::write(&binary, "#!/bin/sh\n").expect("write binary");

        let resolved = resolve_external_tool_program(
            "fort",
            Some(repo.path().to_path_buf()),
            None,
            None,
            None,
            None,
            false,
            true,
        )
        .expect("resolve explicit repo binary");

        assert_eq!(resolved, binary);
    }

    #[test]
    fn external_tool_resolver_rejects_missing_repo_binary_when_no_build() {
        let repo = tempdir().expect("repo tempdir");

        let error = resolve_external_tool_program(
            "surf",
            Some(repo.path().to_path_buf()),
            None,
            None,
            None,
            None,
            false,
            true,
        )
        .expect_err("missing binary should fail with no-build")
        .to_string();

        assert!(error.contains("si surf --no-build could not find surf binary"));
    }

    #[test]
    fn external_tool_resolver_prefers_explicit_bin_over_repo() {
        let repo = tempdir().expect("repo tempdir");
        let explicit_bin = repo.path().join("custom").join("viva");

        let resolved = resolve_external_tool_program(
            "viva",
            Some(repo.path().to_path_buf()),
            None,
            Some(explicit_bin.clone()),
            None,
            Some(true),
            true,
            false,
        )
        .expect("resolve explicit bin");

        assert_eq!(resolved, explicit_bin);
    }

    #[test]
    fn viva_tunnel_import_extracts_named_profile_without_dropping_native_fields() {
        let document = toml::from_str::<toml::Value>(
            r#"
[viva.tunnel.profiles.dev]
runtime_name = "viva-shared-dev-cloudflared"
container_name = "viva-shared-dev-cloudflared"
network_mode = "viva-shared"
additional_networks = ["viva-ls-dev_default"]
fort_env_file = "/work/safe/viva/.env.dev"

[[viva.tunnel.profiles.dev.routes]]
hostname = "dev.example.com"
service = "http://viva-dev-web:3000"
"#,
        )
        .expect("parse document");

        let profile = extract_viva_tunnel_profile_value(&document, "dev").expect("extract profile");
        let table = profile.as_table().expect("profile table");

        assert_eq!(
            table.get("container_name").and_then(toml::Value::as_str),
            Some("viva-shared-dev-cloudflared")
        );
        assert!(table.contains_key("additional_networks"));
        assert!(table.contains_key("routes"));
    }

    #[test]
    fn viva_tunnel_import_accepts_direct_profile_table() {
        let document = toml::from_str::<toml::Value>(
            r#"
runtime_name = "viva-shared-prod-cloudflared"
fort_env_file = "/work/safe/viva/.env.prod"

[[routes]]
hostname = "example.com"
service = "http://viva-prod-web:3000"
"#,
        )
        .expect("parse document");

        let profile =
            extract_viva_tunnel_profile_value(&document, "prod").expect("extract profile");
        let table = profile.as_table().expect("profile table");

        assert_eq!(
            table.get("runtime_name").and_then(toml::Value::as_str),
            Some("viva-shared-prod-cloudflared")
        );
        assert!(table.contains_key("routes"));
    }

    #[test]
    fn build_codex_launch_command_includes_bypass_flag() {
        let fort_paths =
            codex_profile_fort_session_paths(Path::new("/tmp/home/.si/codex/profiles/america"));
        let command = build_codex_launch_command(
            Path::new("/tmp/home"),
            Path::new("/tmp/home/.si/codex/profiles/america"),
            Path::new("/tmp/workspace"),
            "america",
            "primary",
            &[],
            &fort_paths,
        );

        assert!(command.contains("codex --dangerously-bypass-approvals-and-sandbox"));
        assert!(command.contains("FORT_TOKEN_PATH="));
        assert!(command.contains("/tmp/home/.si/codex/profiles/america/fort/access.token"));
        assert!(command.contains("FORT_REFRESH_TOKEN_PATH="));
        assert!(command.contains("/tmp/home/.si/codex/profiles/america/fort/refresh.token"));
        assert!(!command.contains("TERM=xterm-256color"));
        assert!(!command.contains("COLUMNS=160"));
        assert!(!command.contains("LINES=60"));
    }

    #[test]
    fn codex_tmux_window_name_includes_profile_and_slot() {
        assert_eq!(
            codex_tmux_window_name("america", Some("Ada Lovelace"), "primary"),
            "Ada Lovelace [primary]"
        );
        assert_eq!(codex_tmux_window_name("america", None, "review"), "america [review]");
        assert_eq!(codex_tmux_window_name("america", Some("America"), "si"), "America ⚛️ [si]");
        assert_eq!(
            codex_tmux_window_name("america", Some("America"), "lingospeak"),
            "America 💬 [lingospeak]"
        );
        assert_eq!(
            codex_tmux_window_name("america", Some("America"), "si-nucleus"),
            "America ⚛️ [si-nucleus]"
        );
    }

    #[test]
    fn codex_tmux_window_name_sanitizes_profile_label_and_normalizes_slot() {
        assert_eq!(
            codex_tmux_window_name("america", Some(" Ada :\nLovelace  "), " ReView "),
            "Ada Lovelace [review]"
        );
    }

    #[test]
    fn format_relative_minutes_compact_renders_compact_relative_values() {
        assert_eq!(format_relative_minutes_compact(0), "now");
        assert_eq!(format_relative_minutes_compact(45), "in 45m");
        assert_eq!(format_relative_minutes_compact(120), "in 2h");
        assert_eq!(format_relative_minutes_compact(185), "in 3h5m");
        assert_eq!(format_relative_minutes_compact(60 * 24), "in 1d");
        assert_eq!(format_relative_minutes_compact((60 * 24) + 130), "in 1d2h");
    }

    #[test]
    fn render_codex_quota_cell_marks_missing_auth_and_formats_quota() {
        let now = Utc::now();
        let missing = render_codex_quota_cell(Some(80.0), Some(30), None, now, true);
        assert_eq!(missing.0, "Missing");
        assert_eq!(missing.1, cli_table_color(CliTone::Warning));

        let low = render_codex_quota_cell(Some(19.9), Some(75), None, now, false);
        assert_eq!(low.0, "19.9% · in 1h15m");
        assert_eq!(low.1, cli_table_color(CliTone::Command));
    }

    #[test]
    fn fort_bootstrap_admin_refresh_unauthorized_error_explains_host_rebootstrap() {
        let token_path = Path::new("/home/test/.si/fort/bootstrap/admin.token");
        let refresh_token_path = Path::new("/home/test/.si/fort/bootstrap/admin.refresh.token");
        let error = fort_bootstrap_admin_refresh_error(
            401,
            &json!({ "error": "unauthorized" }),
            token_path,
            refresh_token_path,
        )
        .to_string();

        assert!(
            error
                .contains("refresh Fort bootstrap admin session failed (status=401): unauthorized")
        );
        assert!(error.contains("/home/test/.si/fort/bootstrap/admin.refresh.token"));
        assert!(error.contains("/home/test/.si/fort/bootstrap/admin.token"));
        assert!(error.contains("Reissue Fort break-glass/admin bootstrap auth on the Fort host"));
        assert!(error.contains("SI did not fall back to any local bypass"));
    }

    #[test]
    fn fort_session_close_non_fatal_errors_are_ignored() {
        assert!(is_non_fatal_fort_session_close_error("status=401: unauthorized"));
        assert!(is_non_fatal_fort_session_close_error("status=403 forbidden"));
        assert!(is_non_fatal_fort_session_close_error("status=404 missing"));
        assert!(is_non_fatal_fort_session_close_error("token file auth required"));

        assert!(!is_non_fatal_fort_session_close_error("status=500: internal server error"));
        assert!(!is_non_fatal_fort_session_close_error("network transport failed"));
    }

    #[test]
    fn codex_warmup_weekly_quota_requires_less_than_one_hundred_percent_left() {
        let mut status = CodexStatusView {
            source: None,
            raw: None,
            model: None,
            reasoning_effort: None,
            account_email: None,
            account_plan: None,
            five_hour_left_pct: None,
            five_hour_reset: None,
            five_hour_remaining_minutes: None,
            weekly_left_pct: Some(100.0),
            weekly_reset: None,
            weekly_remaining_minutes: None,
        };

        assert!(!codex_warmup_weekly_quota_reached(&status));

        status.weekly_left_pct = Some(99.0);
        assert!(codex_warmup_weekly_quota_reached(&status));

        status.weekly_left_pct = None;
        assert!(!codex_warmup_weekly_quota_reached(&status));
    }

    #[test]
    fn codex_warmup_weekly_quota_ignores_reset_timer_without_usage() {
        let mut status = CodexStatusView {
            source: None,
            raw: None,
            model: None,
            reasoning_effort: None,
            account_email: None,
            account_plan: None,
            five_hour_left_pct: None,
            five_hour_reset: None,
            five_hour_remaining_minutes: None,
            weekly_left_pct: Some(100.0),
            weekly_reset: Some("May 5, 2026 6:26 AM".to_owned()),
            weekly_remaining_minutes: None,
        };

        assert!(!codex_warmup_weekly_quota_reached(&status));

        status.weekly_reset = None;
        assert!(!codex_warmup_weekly_quota_reached(&status));

        status.weekly_remaining_minutes = Some(10080);
        assert!(!codex_warmup_weekly_quota_reached(&status));

        status.weekly_left_pct = Some(99.0);
        assert!(codex_warmup_weekly_quota_reached(&status));
    }

    #[test]
    fn codex_status_input_uses_low_refresh_live_probe_shape() {
        let payload =
            String::from_utf8(build_app_server_status_input(Some("/tmp/si-work".to_owned())))
                .expect("utf8");

        assert!(payload.contains("\"account/rateLimits/read\""));
        assert!(payload.contains("\"account/read\""));
        assert!(payload.contains("\"refreshToken\":false"));
        assert!(payload.contains("\"config/read\""));
        assert!(payload.contains("\"includeLayers\":false"));
        assert!(payload.contains("\"cwd\":\"/tmp/si-work\""));
    }
}
