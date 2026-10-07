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

mod agents;

use crate::agents::store::default_home_dir;
use anyhow::{Context, Result, anyhow};
use base64::Engine as _;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use chrono::{DateTime, Utc};
use clap::builder::styling::{AnsiColor, Effects, Styles};
use clap::{
    ArgAction, Args, ColorChoice, CommandFactory, FromArgMatches, Parser, Subcommand, ValueEnum,
};
use reqwest::blocking::Client as BlockingHttpClient;
use serde::{Deserialize, Serialize};
use serde_json::Value;
use si_codex::codex_worker_slot_name;
use si_command_manifest::{CommandCategory, CommandSpec, find_root_command, visible_root_commands};
use si_config::paths::SiPaths;
use si_config::settings::{FortSettings, Settings, SurfSettings, VivaSettings};
use si_fort::{
    BootstrapView, PersistedRuntimeAgentState, PersistedSessionState, RefreshOutcome,
    RefreshSuccess, SessionState, apply_refresh_outcome_to_persisted_session_state,
    build_bootstrap_view, classify_persisted_session_state, clear_persisted_runtime_agent_state,
    clear_persisted_session_state, load_persisted_runtime_agent_state,
    load_persisted_session_state, save_persisted_runtime_agent_state, save_persisted_session_state,
    teardown_persisted_session_state,
};
use si_warmup::{
    WarmupState, classify_autostart_request, default_autostart_marker_path,
    default_disabled_marker_path, default_state_path as default_warmup_state_path,
    load_state as load_warmup_state, read_marker_state as read_warmup_marker_state,
    render_state_text as render_warmup_state_text, save_state as save_warmup_state,
    set_disabled_marker as set_rust_warmup_disabled_marker,
    write_autostart_marker as write_rust_warmup_autostart_marker,
};
use std::env;
use std::fmt;
use std::fs;
use std::io::{self, IsTerminal, Write};
use std::net::IpAddr;
#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;
#[cfg(unix)]
use std::path::{Path, PathBuf};
use std::process::{Command as StdCommand, ExitStatus};

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
    Commands(CommandsArgs),
    Doctor {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
        #[arg(long, default_value_t = false)]
        migration_plan: bool,
        #[arg(long, default_value_t = false, conflicts_with = "migration_plan")]
        migrate: bool,
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
    /// Internal worker runner. Not a public command: launched by SI-owned
    /// tmux sessions only, hidden from help.
    #[command(name = "__worker-run", hide = true)]
    WorkerRun {
        #[arg(long)]
        spec: PathBuf,
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

#[derive(Debug, Args)]
struct CodexSpawnStartArgs {
    profile: Option<String>,
    #[arg(long = "profile", conflicts_with = "profile")]
    profile_flag: Option<String>,
    #[arg(long = "slot")]
    worker_slot: Option<String>,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long)]
    workspace: Option<PathBuf>,
    #[arg(long)]
    workdir: Option<String>,
    #[arg(long)]
    codex_bin: Option<PathBuf>,
    #[arg(long, default_value = "text")]
    format: OutputFormat,
    #[arg(last = true)]
    native: Vec<String>,
}

#[derive(Debug, Args)]
struct CodexRespawnArgs {
    profile: Option<String>,
    #[arg(long = "profile", conflicts_with = "profile")]
    profile_flag: Option<String>,
    #[arg(long = "slot")]
    worker_slot: Option<String>,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long)]
    codex_bin: Option<PathBuf>,
    #[arg(long)]
    resume_id: Option<String>,
    #[arg(long, default_value = "text")]
    format: OutputFormat,
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
    #[arg(long, default_value_t = false)]
    browser: bool,
    #[arg(long, default_value = "text")]
    format: OutputFormat,
}

#[derive(Debug, Args)]
struct CodexProfileLogoutArgs {
    profile: Option<String>,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long)]
    codex_bin: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct CodexProfileSelectArgs {
    profile: String,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct CodexRunArgs {
    profile: Option<String>,
    #[arg(long = "profile", conflicts_with = "profile")]
    profile_flag: Option<String>,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long)]
    codex_bin: Option<PathBuf>,
    #[arg(last = true, required = true)]
    native: Vec<String>,
}

#[derive(Debug, Subcommand)]
enum CodexProfileCommand {
    List(CodexProfileListArgs),
    Show(CodexProfileShowArgs),
    Add(CodexProfileAddArgs),
    Remove(CodexProfileRemoveArgs),
    Login(CodexProfileLoginArgs),
    Logout(CodexProfileLogoutArgs),
    Select(CodexProfileSelectArgs),
}

#[derive(Debug, Args)]
struct CodexTmuxArgs {
    profile: Option<String>,
    #[arg(long = "profile", conflicts_with = "profile")]
    profile_flag: Option<String>,
    #[arg(long = "slot")]
    worker_slot: Option<String>,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct CodexRemoveArgs {
    profile: Option<String>,
    #[arg(long = "profile", conflicts_with = "profile")]
    profile_flag: Option<String>,
    #[arg(long = "slot")]
    worker_slot: Option<String>,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long, default_value_t = false)]
    stop: bool,
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
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long, default_value_t = false)]
    force: bool,
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
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long, default_value = "200")]
    tail: String,
}

#[derive(Debug, Args)]
struct CodexShellArgs {
    #[arg(long = "profile")]
    profile: Option<String>,
    #[arg(long = "slot")]
    worker_slot: Option<String>,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long)]
    workspace: Option<PathBuf>,
    #[arg(long)]
    workdir: Option<String>,
    #[arg(trailing_var_arg = true, allow_hyphen_values = true)]
    command: Vec<String>,
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
    Run(CodexRunArgs),
    List {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Tmux(CodexTmuxArgs),
    Warmup {
        #[command(subcommand)]
        command: WarmupCommand,
    },
    #[command(name = "respawn")]
    Respawn(CodexRespawnArgs),
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
pub(crate) enum OutputFormat {
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
        distribution_doctor_check("codex", "Codex tmux operator sessions"),
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

pub(crate) fn resolve_program_on_path(name: &str) -> Option<PathBuf> {
    let path_env = std::env::var_os("PATH")?;
    for dir in std::env::split_paths(&path_env) {
        let candidate = dir.join(name);
        if candidate.is_file() {
            return Some(candidate);
        }
    }
    None
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
        Command::Doctor { home, settings_file, format, migration_plan, migrate } => {
            if migration_plan {
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                agents::migrate::render_migration_plan(&ctx, format)?
            } else if migrate {
                let mut ctx = agents::store::AgentContext::load(home, settings_file)?;
                agents::migrate::apply_migration(&mut ctx, format)?
            } else {
                run_distribution_doctor(format)?
            }
        }
        Command::WorkerRun { spec } => agents::runner::run_worker_entry(&spec)?,
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
                    let ctx = agents::store::AgentContext::load(home, settings_file)?;
                    agents::codex::profile_list(&ctx, format)?
                }
                CodexProfileCommand::Show(CodexProfileShowArgs {
                    profile,
                    home,
                    settings_file,
                    format,
                }) => {
                    let ctx = agents::store::AgentContext::load(home, settings_file)?;
                    agents::codex::profile_show(&ctx, profile.as_deref(), format)?
                }
                CodexProfileCommand::Add(CodexProfileAddArgs {
                    profile,
                    name,
                    email,
                    home,
                    settings_file,
                    format,
                }) => {
                    let mut ctx = agents::store::AgentContext::load(home, settings_file)?;
                    agents::codex::profile_add(&mut ctx, &profile, name, email, format)?
                }
                CodexProfileCommand::Remove(CodexProfileRemoveArgs {
                    profile,
                    home,
                    settings_file,
                }) => {
                    let mut ctx = agents::store::AgentContext::load(home, settings_file)?;
                    agents::codex::profile_remove(&mut ctx, profile.as_deref())?
                }
                CodexProfileCommand::Login(CodexProfileLoginArgs {
                    profile,
                    home,
                    settings_file,
                    codex_bin,
                    browser,
                    format,
                }) => {
                    let mut ctx = agents::store::AgentContext::load(home, settings_file)?;
                    agents::codex::profile_login(
                        &mut ctx,
                        profile.as_deref(),
                        codex_bin,
                        browser,
                        format,
                    )?
                }
                CodexProfileCommand::Logout(CodexProfileLogoutArgs {
                    profile,
                    home,
                    settings_file,
                    codex_bin,
                }) => {
                    let mut ctx = agents::store::AgentContext::load(home, settings_file)?;
                    agents::codex::profile_logout(&mut ctx, profile.as_deref(), codex_bin)?
                }
                CodexProfileCommand::Select(CodexProfileSelectArgs {
                    profile,
                    home,
                    settings_file,
                }) => {
                    let mut ctx = agents::store::AgentContext::load(home, settings_file)?;
                    agents::codex::profile_select(&mut ctx, &profile)?
                }
            },
            CodexCommand::Spawn(CodexSpawnStartArgs {
                profile,
                profile_flag,
                worker_slot,
                home,
                settings_file,
                workspace,
                workdir,
                codex_bin,
                format,
                native,
            }) => {
                let requested = resolve_codex_cli_profile_arg(profile, profile_flag);
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                agents::codex::spawn(
                    &ctx,
                    requested.as_deref(),
                    worker_slot.as_deref(),
                    workspace,
                    workdir.as_deref(),
                    codex_bin,
                    native,
                    format,
                )?
            }
            CodexCommand::Remove(CodexRemoveArgs {
                profile,
                profile_flag,
                worker_slot,
                home,
                settings_file,
                stop,
                all,
                format,
            }) => {
                let requested = resolve_codex_cli_profile_arg(profile, profile_flag);
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                if all {
                    agents::codex::remove_all(&ctx, stop, format)?
                } else {
                    agents::codex::worker_remove(
                        &ctx,
                        requested.as_deref(),
                        worker_slot.as_deref(),
                        stop,
                        format,
                    )?
                }
            }
            CodexCommand::Stop(CodexStopArgs {
                profile,
                profile_flag,
                worker_slot,
                home,
                settings_file,
                force,
                all,
                format,
            }) => {
                let requested = resolve_codex_cli_profile_arg(profile, profile_flag);
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                if all {
                    agents::codex::stop_all(&ctx, force, format)?
                } else {
                    agents::codex::stop(
                        &ctx,
                        requested.as_deref(),
                        worker_slot.as_deref(),
                        force,
                        format,
                    )?
                }
            }
            CodexCommand::Tail(CodexTailArgs {
                profile,
                profile_flag,
                worker_slot,
                home,
                settings_file,
                tail,
            }) => {
                let requested = resolve_codex_cli_profile_arg(profile, profile_flag);
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                let lines: usize = tail
                    .parse()
                    .with_context(|| format!("invalid --tail {tail:?}; want a line count"))?;
                agents::codex::tail(&ctx, requested.as_deref(), worker_slot.as_deref(), lines)?
            }
            CodexCommand::Shell(CodexShellArgs {
                profile,
                worker_slot,
                home,
                settings_file,
                workspace,
                workdir,
                command,
            }) => {
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                agents::codex::shell(
                    &ctx,
                    profile.as_deref(),
                    worker_slot.as_deref(),
                    workspace,
                    workdir.as_deref(),
                    command,
                )?
            }
            CodexCommand::Run(CodexRunArgs {
                profile,
                profile_flag,
                home,
                settings_file,
                codex_bin,
                native,
            }) => {
                let requested = resolve_codex_cli_profile_arg(profile, profile_flag);
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                agents::codex::run(&ctx, requested.as_deref(), codex_bin, &native)?
            }
            CodexCommand::List { home, settings_file, format } => {
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                agents::codex::list(&ctx, format)?
            }
            CodexCommand::Tmux(CodexTmuxArgs {
                profile,
                profile_flag,
                worker_slot,
                home,
                settings_file,
            }) => {
                let requested = resolve_codex_cli_profile_arg(profile, profile_flag);
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                agents::codex::attach(&ctx, requested.as_deref(), worker_slot.as_deref())?
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
            CodexCommand::Respawn(CodexRespawnArgs {
                profile,
                profile_flag,
                worker_slot,
                home,
                settings_file,
                codex_bin,
                resume_id,
                format,
            }) => {
                let requested = resolve_codex_cli_profile_arg(profile, profile_flag);
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                agents::codex::respawn(
                    &ctx,
                    requested.as_deref(),
                    worker_slot.as_deref(),
                    codex_bin,
                    resume_id,
                    format,
                )?
            }
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
        ["codex"] => Some("Manage Codex profiles and worker sessions."),
        ["codex", "profile"] => Some("Manage Codex profiles."),
        ["codex", "spawn"] => Some("Start a Codex worker for a chosen profile."),
        ["codex", "remove"] => Some("Remove a Codex worker session."),
        ["codex", "stop"] => Some("Stop a Codex worker session."),
        ["codex", "tail"] => Some("Tail Codex worker session output."),
        ["codex", "shell"] => Some("Run a shell command with a Codex worker environment."),
        ["codex", "run"] => Some("Run a native Codex command in the foreground."),
        ["codex", "profile", "select"] => Some("Select the default Codex profile."),
        ["codex", "profile", "logout"] => Some("Log a Codex profile out natively."),
        ["codex", "list"] => Some("List Codex worker sessions."),
        ["codex", "tmux"] => Some("Attach to a Codex tmux session."),
        ["codex", "warmup"] => Some("Inspect Codex warmup state."),
        ["codex", "warmup", "run"] => Some("Warm configured Codex profiles."),
        ["codex", "respawn"] => {
            Some("Restart a Codex worker on the same profile (reuse or resume).")
        }
        ["codex", "warmup", "decision"] => Some("Decide whether warmup should run."),
        ["codex", "warmup", "status"] => Some("Show warmup status."),
        ["codex", "warmup", "state"] => Some("Warmup state file commands."),
        ["codex", "warmup", "marker"] => Some("Warmup marker file commands."),
        ["codex", "warmup", "marker", "show"] => Some("Show warmup marker state."),
        ["codex", "warmup", "marker", "enable"] => Some("Write the autostart marker."),
        ["codex", "warmup", "marker", "disable"] => Some("Set the disabled marker."),
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
    trace_external_tool_program("surf", program);
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
    trace_external_tool_program("fort", program);
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
    trace_external_tool_program("viva", &program);
    let status = match StdCommand::new(&program).args(&command_args).status() {
        Ok(status) => status,
        Err(error)
            if error.kind() == std::io::ErrorKind::NotFound
                && !build
                && !no_build
                && !explicit_program =>
        {
            let fallback = resolve_viva_build_fallback(&settings.viva)?;
            trace_external_tool_program("viva", &fallback);
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

fn trace_external_tool_program(name: &str, program: &Path) {
    let enabled = std::env::var("SI_WRAPPER_TRACE")
        .map(|value| matches!(value.trim().to_ascii_lowercase().as_str(), "1" | "true" | "yes"))
        .unwrap_or(false);
    if enabled {
        eprintln!("si wrapper trace: {name} binary={}", program.display());
    }
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
    trace_external_tool_program("fort", program);
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

const FORT_ACCESS_TOKEN_REFRESH_SKEW_SECONDS: i64 = 300;
const FORT_REFRESH_LOCK_STALE_AFTER_SECONDS: u64 = 120;
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

pub(crate) fn load_settings_document(path: &Path) -> Result<toml::map::Map<String, toml::Value>> {
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

pub(crate) fn ensure_toml_table<'a>(
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

pub(crate) fn set_toml_string(
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

fn resolve_codex_cli_profile_arg(
    positional: Option<String>,
    flag: Option<String>,
) -> Option<String> {
    flag.or(positional).map(|value| value.trim().to_owned()).filter(|value| !value.is_empty())
}

fn run_codex_warmup(
    profile: Option<String>,
    _all: bool,
    path: Option<PathBuf>,
    _home: Option<PathBuf>,
    _settings_file: Option<PathBuf>,
    _workspace: Option<PathBuf>,
    _max_turns: u32,
    _turn_timeout_seconds: u64,
    _format: OutputFormat,
) -> Result<()> {
    if let Some(profile) = profile.as_deref() {
        if profile.trim().is_empty() {
            return Err(anyhow!("si codex warmup run is not available while codex runtime is unavailable"));
        }
    }
    let _ = path;
    Err(anyhow!("si codex warmup run is not available while codex runtime is unavailable"))
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
        "lingospeak" => Some("💬"),
        _ => None,
    }
}

pub(crate) fn codex_tmux_window_name(
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

const CODEX_WARMUP_WEEKLY_JITTER_MAX_SECS: i64 = 300;
const CODEX_WARMUP_DEFAULT_MAX_TURNS: u32 = 4;
const CODEX_WARMUP_DEFAULT_TURN_TIMEOUT_SECONDS: u64 = 180;

fn codex_warmup_weekly_quota_reached(status: &CodexStatusView) -> bool {
    status.weekly_left_pct.is_some_and(|value| value < 100.0)
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
            codex_tmux_window_name("america", Some("America"), "si-codex"),
            "America [si-codex]"
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

}
