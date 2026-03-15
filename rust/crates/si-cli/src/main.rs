use anyhow::Result;
use chrono::TimeZone;
use clap::{Parser, Subcommand, ValueEnum};
use serde::{Deserialize, Serialize};
use si_rs_codex::{
    RespawnRequest, SpawnContainerOptions, SpawnRequest, build_container_spec,
    build_remove_artifacts, build_respawn_plan, build_spawn_plan, build_tmux_command_for_container,
    build_tmux_plan, parse_report_capture,
};
use si_rs_command_manifest::{
    CommandCategory, CommandSpec, find_root_command, visible_root_commands,
};
use si_rs_config::paths::SiPaths;
use si_rs_config::settings::Settings;
use si_rs_docker::{
    ContainerAction, ContainerExecSpec, docker_container_action_command,
    docker_container_exec_command, docker_container_list_command,
    docker_container_list_with_format_command, docker_container_logs_command,
    docker_container_remove_command, docker_volume_remove_command,
};
use si_rs_dyad::{
    SpawnRequest as DyadSpawnRequest, build_container_specs as build_dyad_container_specs,
    build_peek_plan as build_dyad_peek_plan, build_spawn_plan as build_dyad_spawn_plan,
};
use si_rs_fort::{
    PersistedRuntimeAgentState, PersistedSessionState, RefreshOutcome, RefreshSuccess,
    SessionState, apply_refresh_outcome_to_persisted_session_state,
    classify_persisted_session_state, load_persisted_runtime_agent_state,
    load_persisted_session_state, save_persisted_runtime_agent_state, save_persisted_session_state,
    teardown_persisted_session_state,
};
use si_rs_process::{ProcessRunner, RunOptions, StdinBehavior};
use si_rs_provider_catalog::{default_ids, find as find_provider, parse_id as parse_provider_id};
use si_rs_runtime::HostMountContext;
use si_rs_vault::TrustStore;
use std::fmt;
use std::io::{self, Read};
use std::path::PathBuf;

#[derive(Debug, Parser)]
#[command(name = "si-rs", disable_version_flag = true, disable_help_subcommand = true)]
struct Cli {
    #[command(subcommand)]
    command: Command,
}

#[derive(Debug, Subcommand)]
enum Command {
    Version,
    Help {
        command: Option<String>,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Commands {
        #[command(subcommand)]
        command: CommandsCommand,
    },
    Settings {
        #[command(subcommand)]
        command: SettingsCommand,
    },
    Providers {
        #[command(subcommand)]
        command: ProvidersCommand,
    },
    Dyad {
        #[command(subcommand)]
        command: Box<DyadCommand>,
    },
    Codex {
        #[command(subcommand)]
        command: Box<CodexCommand>,
    },
    Paths {
        #[command(subcommand)]
        command: PathsCommand,
    },
    Fort {
        #[command(subcommand)]
        command: FortCommand,
    },
    Vault {
        #[command(subcommand)]
        command: VaultCommand,
    },
}

#[derive(Debug, Subcommand)]
enum CommandsCommand {
    List {
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum SettingsCommand {
    Show {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum ProvidersCommand {
    #[command(visible_aliases = ["chars", "status", "list"])]
    Characteristics {
        #[arg(long)]
        provider: Option<String>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[allow(clippy::enum_variant_names)]
#[derive(Debug, Subcommand)]
enum DyadCommand {
    SpawnPlan {
        #[arg(long)]
        name: String,
        #[arg(long)]
        role: Option<String>,
        #[arg(long)]
        actor_image: Option<String>,
        #[arg(long)]
        critic_image: Option<String>,
        #[arg(long)]
        codex_model: Option<String>,
        #[arg(long)]
        codex_effort_actor: Option<String>,
        #[arg(long)]
        codex_effort_critic: Option<String>,
        #[arg(long)]
        codex_model_low: Option<String>,
        #[arg(long)]
        codex_model_medium: Option<String>,
        #[arg(long)]
        codex_model_high: Option<String>,
        #[arg(long)]
        codex_effort_low: Option<String>,
        #[arg(long)]
        codex_effort_medium: Option<String>,
        #[arg(long)]
        codex_effort_high: Option<String>,
        #[arg(long)]
        workspace: PathBuf,
        #[arg(long)]
        configs: Option<PathBuf>,
        #[arg(long)]
        vault_env_file: Option<PathBuf>,
        #[arg(long)]
        codex_volume: Option<String>,
        #[arg(long)]
        skills_volume: Option<String>,
        #[arg(long)]
        network: Option<String>,
        #[arg(long)]
        forward_ports: Option<String>,
        #[arg(long, default_value_t = true)]
        docker_socket: bool,
        #[arg(long)]
        profile_id: Option<String>,
        #[arg(long)]
        profile_name: Option<String>,
        #[arg(long)]
        loop_enabled: Option<bool>,
        #[arg(long)]
        loop_goal: Option<String>,
        #[arg(long)]
        loop_seed_prompt: Option<String>,
        #[arg(long)]
        loop_max_turns: Option<i32>,
        #[arg(long)]
        loop_sleep_seconds: Option<i32>,
        #[arg(long)]
        loop_startup_delay_seconds: Option<i32>,
        #[arg(long)]
        loop_turn_timeout_seconds: Option<i32>,
        #[arg(long)]
        loop_retry_max: Option<i32>,
        #[arg(long)]
        loop_retry_base_seconds: Option<i32>,
        #[arg(long)]
        loop_prompt_lines: Option<i32>,
        #[arg(long)]
        loop_allow_mcp_startup: Option<bool>,
        #[arg(long)]
        loop_tmux_capture: Option<String>,
        #[arg(long)]
        loop_pause_poll_seconds: Option<i32>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        ssh_auth_sock: Option<PathBuf>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    SpawnSpec {
        #[arg(long)]
        name: String,
        #[arg(long)]
        role: Option<String>,
        #[arg(long)]
        actor_image: Option<String>,
        #[arg(long)]
        critic_image: Option<String>,
        #[arg(long)]
        codex_model: Option<String>,
        #[arg(long)]
        codex_effort_actor: Option<String>,
        #[arg(long)]
        codex_effort_critic: Option<String>,
        #[arg(long)]
        codex_model_low: Option<String>,
        #[arg(long)]
        codex_model_medium: Option<String>,
        #[arg(long)]
        codex_model_high: Option<String>,
        #[arg(long)]
        codex_effort_low: Option<String>,
        #[arg(long)]
        codex_effort_medium: Option<String>,
        #[arg(long)]
        codex_effort_high: Option<String>,
        #[arg(long)]
        workspace: PathBuf,
        #[arg(long)]
        configs: Option<PathBuf>,
        #[arg(long)]
        vault_env_file: Option<PathBuf>,
        #[arg(long)]
        codex_volume: Option<String>,
        #[arg(long)]
        skills_volume: Option<String>,
        #[arg(long)]
        network: Option<String>,
        #[arg(long)]
        forward_ports: Option<String>,
        #[arg(long, default_value_t = true)]
        docker_socket: bool,
        #[arg(long)]
        profile_id: Option<String>,
        #[arg(long)]
        profile_name: Option<String>,
        #[arg(long)]
        loop_enabled: Option<bool>,
        #[arg(long)]
        loop_goal: Option<String>,
        #[arg(long)]
        loop_seed_prompt: Option<String>,
        #[arg(long)]
        loop_max_turns: Option<i32>,
        #[arg(long)]
        loop_sleep_seconds: Option<i32>,
        #[arg(long)]
        loop_startup_delay_seconds: Option<i32>,
        #[arg(long)]
        loop_turn_timeout_seconds: Option<i32>,
        #[arg(long)]
        loop_retry_max: Option<i32>,
        #[arg(long)]
        loop_retry_base_seconds: Option<i32>,
        #[arg(long)]
        loop_prompt_lines: Option<i32>,
        #[arg(long)]
        loop_allow_mcp_startup: Option<bool>,
        #[arg(long)]
        loop_tmux_capture: Option<String>,
        #[arg(long)]
        loop_pause_poll_seconds: Option<i32>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        ssh_auth_sock: Option<PathBuf>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    SpawnStart {
        #[arg(long)]
        name: String,
        #[arg(long)]
        role: Option<String>,
        #[arg(long)]
        actor_image: Option<String>,
        #[arg(long)]
        critic_image: Option<String>,
        #[arg(long)]
        codex_model: Option<String>,
        #[arg(long)]
        codex_effort_actor: Option<String>,
        #[arg(long)]
        codex_effort_critic: Option<String>,
        #[arg(long)]
        codex_model_low: Option<String>,
        #[arg(long)]
        codex_model_medium: Option<String>,
        #[arg(long)]
        codex_model_high: Option<String>,
        #[arg(long)]
        codex_effort_low: Option<String>,
        #[arg(long)]
        codex_effort_medium: Option<String>,
        #[arg(long)]
        codex_effort_high: Option<String>,
        #[arg(long)]
        workspace: PathBuf,
        #[arg(long)]
        configs: Option<PathBuf>,
        #[arg(long)]
        vault_env_file: Option<PathBuf>,
        #[arg(long)]
        codex_volume: Option<String>,
        #[arg(long)]
        skills_volume: Option<String>,
        #[arg(long)]
        network: Option<String>,
        #[arg(long)]
        forward_ports: Option<String>,
        #[arg(long, default_value_t = true)]
        docker_socket: bool,
        #[arg(long)]
        profile_id: Option<String>,
        #[arg(long)]
        profile_name: Option<String>,
        #[arg(long)]
        loop_enabled: Option<bool>,
        #[arg(long)]
        loop_goal: Option<String>,
        #[arg(long)]
        loop_seed_prompt: Option<String>,
        #[arg(long)]
        loop_max_turns: Option<i32>,
        #[arg(long)]
        loop_sleep_seconds: Option<i32>,
        #[arg(long)]
        loop_startup_delay_seconds: Option<i32>,
        #[arg(long)]
        loop_turn_timeout_seconds: Option<i32>,
        #[arg(long)]
        loop_retry_max: Option<i32>,
        #[arg(long)]
        loop_retry_base_seconds: Option<i32>,
        #[arg(long)]
        loop_prompt_lines: Option<i32>,
        #[arg(long)]
        loop_allow_mcp_startup: Option<bool>,
        #[arg(long)]
        loop_tmux_capture: Option<String>,
        #[arg(long)]
        loop_pause_poll_seconds: Option<i32>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        ssh_auth_sock: Option<PathBuf>,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    Start {
        name: String,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    Stop {
        name: String,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    Logs {
        name: String,
        #[arg(long, default_value = "critic")]
        member: String,
        #[arg(long, default_value = "200")]
        tail: String,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    List {
        #[arg(long, default_value = "text")]
        format: OutputFormat,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    Status {
        name: String,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    PeekPlan {
        name: String,
        #[arg(long, default_value = "both")]
        member: String,
        #[arg(long)]
        session: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Restart {
        name: String,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    Remove {
        name: String,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    Exec {
        name: String,
        #[arg(long, default_value = "actor")]
        member: String,
        #[arg(long, num_args = 1, default_value = "false", value_parser = clap::value_parser!(bool))]
        tty: bool,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
        #[arg(trailing_var_arg = true, allow_hyphen_values = true)]
        command: Vec<String>,
    },
    Cleanup {
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
}

#[allow(clippy::enum_variant_names)]
#[derive(Debug, Subcommand)]
enum CodexCommand {
    SpawnPlan {
        #[arg(long)]
        name: Option<String>,
        #[arg(long)]
        profile_id: Option<String>,
        #[arg(long)]
        workspace: PathBuf,
        #[arg(long)]
        workdir: Option<String>,
        #[arg(long)]
        codex_volume: Option<String>,
        #[arg(long)]
        skills_volume: Option<String>,
        #[arg(long)]
        gh_volume: Option<String>,
        #[arg(long)]
        repo: Option<String>,
        #[arg(long)]
        gh_pat: Option<String>,
        #[arg(long, default_value_t = true)]
        docker_socket: bool,
        #[arg(long, default_value_t = true)]
        detach: bool,
        #[arg(long, default_value_t = false)]
        clean_slate: bool,
        #[arg(long)]
        image: Option<String>,
        #[arg(long)]
        network: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        ssh_auth_sock: Option<PathBuf>,
        #[arg(long)]
        vault_env_file: Option<PathBuf>,
        #[arg(long, default_value_t = true)]
        include_host_si: bool,
        #[arg(long = "env")]
        env: Vec<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    SpawnSpec {
        #[arg(long)]
        name: Option<String>,
        #[arg(long)]
        profile_id: Option<String>,
        #[arg(long)]
        workspace: PathBuf,
        #[arg(long)]
        workdir: Option<String>,
        #[arg(long)]
        codex_volume: Option<String>,
        #[arg(long)]
        skills_volume: Option<String>,
        #[arg(long)]
        gh_volume: Option<String>,
        #[arg(long)]
        repo: Option<String>,
        #[arg(long)]
        gh_pat: Option<String>,
        #[arg(long, default_value_t = true)]
        docker_socket: bool,
        #[arg(long, default_value_t = true)]
        detach: bool,
        #[arg(long, default_value_t = false)]
        clean_slate: bool,
        #[arg(long)]
        image: Option<String>,
        #[arg(long)]
        network: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        ssh_auth_sock: Option<PathBuf>,
        #[arg(long)]
        vault_env_file: Option<PathBuf>,
        #[arg(long, default_value_t = true)]
        include_host_si: bool,
        #[arg(long = "env")]
        env: Vec<String>,
        #[arg(long = "label")]
        labels: Vec<String>,
        #[arg(long = "port")]
        ports: Vec<String>,
        #[arg(long)]
        cmd: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    SpawnRunArgs {
        #[arg(long)]
        name: Option<String>,
        #[arg(long)]
        profile_id: Option<String>,
        #[arg(long)]
        workspace: PathBuf,
        #[arg(long)]
        workdir: Option<String>,
        #[arg(long)]
        codex_volume: Option<String>,
        #[arg(long)]
        skills_volume: Option<String>,
        #[arg(long)]
        gh_volume: Option<String>,
        #[arg(long)]
        repo: Option<String>,
        #[arg(long)]
        gh_pat: Option<String>,
        #[arg(long, default_value_t = true)]
        docker_socket: bool,
        #[arg(long, default_value_t = true)]
        detach: bool,
        #[arg(long, default_value_t = false)]
        clean_slate: bool,
        #[arg(long)]
        image: Option<String>,
        #[arg(long)]
        network: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        ssh_auth_sock: Option<PathBuf>,
        #[arg(long)]
        vault_env_file: Option<PathBuf>,
        #[arg(long, default_value_t = true)]
        include_host_si: bool,
        #[arg(long = "env")]
        env: Vec<String>,
        #[arg(long = "label")]
        labels: Vec<String>,
        #[arg(long = "port")]
        ports: Vec<String>,
        #[arg(long)]
        cmd: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    SpawnStart {
        #[arg(long)]
        name: Option<String>,
        #[arg(long)]
        profile_id: Option<String>,
        #[arg(long)]
        workspace: PathBuf,
        #[arg(long)]
        workdir: Option<String>,
        #[arg(long)]
        codex_volume: Option<String>,
        #[arg(long)]
        skills_volume: Option<String>,
        #[arg(long)]
        gh_volume: Option<String>,
        #[arg(long)]
        repo: Option<String>,
        #[arg(long)]
        gh_pat: Option<String>,
        #[arg(long, default_value_t = true)]
        docker_socket: bool,
        #[arg(long, default_value_t = true)]
        detach: bool,
        #[arg(long, default_value_t = false)]
        clean_slate: bool,
        #[arg(long)]
        image: Option<String>,
        #[arg(long)]
        network: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        ssh_auth_sock: Option<PathBuf>,
        #[arg(long)]
        vault_env_file: Option<PathBuf>,
        #[arg(long, default_value_t = true)]
        include_host_si: bool,
        #[arg(long = "env")]
        env: Vec<String>,
        #[arg(long = "label")]
        labels: Vec<String>,
        #[arg(long = "port")]
        ports: Vec<String>,
        #[arg(long)]
        cmd: Option<String>,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    RemovePlan {
        name: String,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    Remove {
        name: String,
        #[arg(long, default_value_t = false)]
        volumes: bool,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    Start {
        name: String,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    Stop {
        name: String,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    Logs {
        name: String,
        #[arg(long, default_value = "200")]
        tail: String,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    Tail {
        name: String,
        #[arg(long, default_value = "200")]
        tail: String,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    Clone {
        name: String,
        repo: String,
        #[arg(long)]
        gh_pat: Option<String>,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    Exec {
        name: String,
        #[arg(long)]
        workdir: Option<PathBuf>,
        #[arg(long, num_args = 1, default_value = "true", value_parser = clap::value_parser!(bool))]
        interactive: bool,
        #[arg(long, num_args = 1, default_value = "false", value_parser = clap::value_parser!(bool))]
        tty: bool,
        #[arg(long = "env")]
        env: Vec<String>,
        #[arg(long, default_value = "si")]
        user: String,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
        #[arg(trailing_var_arg = true, allow_hyphen_values = true)]
        command: Vec<String>,
    },
    List {
        #[arg(long, default_value = "text")]
        format: OutputFormat,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    StatusRead {
        name: String,
        #[arg(long, default_value_t = false)]
        raw: bool,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    TmuxPlan {
        name: String,
        #[arg(long)]
        start_dir: Option<String>,
        #[arg(long)]
        resume_session_id: Option<String>,
        #[arg(long)]
        resume_profile: Option<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    TmuxCommand {
        #[arg(long)]
        container: String,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    ReportParse {
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    RespawnPlan {
        name: String,
        #[arg(long)]
        profile_id: Option<String>,
        #[arg(long = "profile-container")]
        profile_containers: Vec<String>,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum PathsCommand {
    Show {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum FortCommand {
    SessionState {
        #[command(subcommand)]
        command: FortSessionStateCommand,
    },
    RuntimeAgentState {
        #[command(subcommand)]
        command: FortRuntimeAgentStateCommand,
    },
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
    Classify {
        #[arg(long)]
        path: PathBuf,
        #[arg(long)]
        now_unix: i64,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
    RefreshOutcome {
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
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, ValueEnum)]
enum FortRefreshOutcomeArg {
    Success,
    Unauthorized,
    Retryable,
}

#[derive(Debug, Subcommand)]
enum VaultCommand {
    Trust {
        #[command(subcommand)]
        command: VaultTrustCommand,
    },
}

#[derive(Debug, Subcommand)]
enum VaultTrustCommand {
    Lookup {
        #[arg(long)]
        path: PathBuf,
        #[arg(long)]
        repo_root: String,
        #[arg(long)]
        file: String,
        #[arg(long)]
        fingerprint: String,
        #[arg(long, default_value = "json")]
        format: OutputFormat,
    },
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

#[derive(Debug, Serialize)]
struct PathView {
    root: String,
    settings_file: String,
    codex_profiles_dir: String,
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

#[derive(Debug, Serialize)]
struct ProvidersCharacteristicsPayload {
    policy: ProvidersPolicyView,
    providers: Vec<ProviderCharacteristicsView>,
}

#[derive(Debug, Serialize)]
struct ProvidersPolicyView {
    defaults: &'static str,
    admission: &'static str,
    adaptive_feedback: bool,
}

#[derive(Debug, Serialize)]
struct ProviderCharacteristicsView {
    provider: String,
    base_url: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    upload_base_url: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    api_version: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    auth_style: Option<String>,
    rate_limit_per_second: f64,
    rate_limit_burst: i32,
    #[serde(skip_serializing_if = "Option::is_none")]
    public_probe: Option<ProviderPublicProbeView>,
    capabilities: ProviderCapabilitiesView,
}

#[derive(Debug, Serialize)]
struct ProviderPublicProbeView {
    method: String,
    path: String,
}

#[derive(Debug, Serialize)]
struct ProviderCapabilitiesView {
    supports_pagination: bool,
    supports_bulk: bool,
    supports_idempotency: bool,
    supports_raw: bool,
}

#[derive(Debug, Serialize)]
struct DyadSpawnPlanView {
    dyad: String,
    role: String,
    network_name: String,
    workspace_host: String,
    configs_host: String,
    codex_volume: String,
    skills_volume: String,
    forward_ports: String,
    docker_socket: bool,
    actor: DyadMemberPlanView,
    critic: DyadMemberPlanView,
}

#[derive(Debug, Serialize)]
struct DyadSpawnSpecView {
    actor: DyadContainerSpecView,
    critic: DyadContainerSpecView,
}

#[derive(Debug, Serialize)]
struct DyadListEntryView {
    dyad: String,
    role: String,
    actor: String,
    critic: String,
}

#[derive(Debug, Serialize)]
struct DyadStatusView {
    dyad: String,
    found: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    actor: Option<DyadContainerStatusView>,
    #[serde(skip_serializing_if = "Option::is_none")]
    critic: Option<DyadContainerStatusView>,
}

#[derive(Debug, Serialize)]
struct DyadPeekPlanView {
    dyad: String,
    member: String,
    actor_container_name: String,
    critic_container_name: String,
    actor_session_name: String,
    critic_session_name: String,
    peek_session_name: String,
    actor_attach_command: String,
    critic_attach_command: String,
}

#[derive(Debug, Serialize)]
struct DyadContainerStatusView {
    name: String,
    id: String,
    status: String,
}

#[derive(Debug, Serialize)]
struct DyadMemberPlanView {
    member: String,
    container_name: String,
    image: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    workdir: Option<String>,
    env: Vec<String>,
    bind_mounts: Vec<DyadBindMountView>,
    volume_mounts: Vec<DyadVolumeMountView>,
    labels: Vec<DyadLabelView>,
    command: Vec<String>,
}

#[derive(Debug, Serialize)]
struct DyadBindMountView {
    source: String,
    target: String,
    read_only: bool,
}

#[derive(Debug, Serialize)]
struct DyadVolumeMountView {
    source: String,
    target: String,
    read_only: bool,
}

#[derive(Debug, Serialize)]
struct DyadContainerSpecView {
    image: String,
    name: Option<String>,
    network: Option<String>,
    restart_policy: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    working_dir: Option<String>,
    command: Vec<String>,
    env: Vec<CodexEnvVarView>,
    bind_mounts: Vec<DyadBindMountView>,
    volume_mounts: Vec<DyadVolumeMountView>,
    labels: Vec<DyadLabelView>,
    published_ports: Vec<CodexPublishedPortView>,
    user: Option<String>,
    detach: bool,
    auto_remove: bool,
}

#[derive(Debug, Serialize)]
struct DyadLabelView {
    key: String,
    value: String,
}

#[derive(Debug, Serialize)]
struct CodexSpawnPlanView {
    name: String,
    container_name: String,
    image: String,
    network_name: String,
    workspace_host: String,
    workspace_primary_target: String,
    workspace_mirror_target: String,
    workdir: String,
    codex_volume: String,
    skills_volume: String,
    gh_volume: String,
    docker_socket: bool,
    clean_slate: bool,
    detach: bool,
    env: Vec<String>,
    mounts: Vec<CodexBindMountView>,
}

#[derive(Debug, Serialize)]
struct CodexBindMountView {
    source: String,
    target: String,
    read_only: bool,
}

#[derive(Debug, Serialize)]
struct CodexVolumeMountView {
    source: String,
    target: String,
    read_only: bool,
}

#[derive(Debug, Serialize)]
struct CodexEnvVarView {
    key: String,
    value: String,
}

#[derive(Debug, Serialize)]
struct CodexSpawnSpecView {
    image: String,
    name: Option<String>,
    network: Option<String>,
    restart_policy: Option<String>,
    working_dir: Option<String>,
    command: Vec<String>,
    env: Vec<CodexEnvVarView>,
    bind_mounts: Vec<CodexBindMountView>,
    volume_mounts: Vec<CodexVolumeMountView>,
    labels: Vec<CodexEnvVarView>,
    published_ports: Vec<CodexPublishedPortView>,
    user: Option<String>,
    detach: bool,
    auto_remove: bool,
}

#[derive(Debug, Serialize)]
struct CodexRemovePlanView {
    name: String,
    container_name: String,
    slug: String,
    codex_volume: String,
    gh_volume: String,
}

#[derive(Debug, Serialize)]
struct CodexPublishedPortView {
    host_ip: String,
    host_port: String,
    container_port: u16,
}

#[derive(Debug, Serialize)]
struct CodexListEntryView {
    name: String,
    state: String,
    image: String,
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
struct CodexRespawnPlanView {
    effective_name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    profile_id: Option<String>,
    remove_targets: Vec<String>,
}

#[derive(Debug, Serialize)]
struct CodexTmuxPlanView {
    session_name: String,
    target: String,
    launch_command: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    resume_command: String,
}

#[derive(Debug, Serialize)]
struct CodexTmuxCommandView {
    container: String,
    launch_command: String,
}

#[derive(Debug, Deserialize)]
struct CodexReportParseInput {
    clean: String,
    raw: String,
    prompt_index: usize,
    ansi: bool,
}

#[derive(Debug, Serialize)]
struct CodexPromptSegmentView {
    prompt: String,
    lines: Vec<String>,
    raw: Vec<String>,
}

#[derive(Debug, Serialize)]
struct CodexReportParseView {
    segments: Vec<CodexPromptSegmentView>,
    report: String,
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

#[derive(Debug, Deserialize)]
struct AppServerEnvelope {
    id: serde_json::Value,
    #[serde(default)]
    result: serde_json::Value,
    error: Option<AppServerError>,
}

#[derive(Debug, Deserialize)]
struct AppServerError {
    message: String,
}

#[derive(Debug, Deserialize)]
struct AppRateLimitsResponse {
    #[serde(rename = "rateLimits")]
    rate_limits: AppRateLimitSnapshot,
}

#[derive(Debug, Deserialize)]
struct AppRateLimitSnapshot {
    primary: Option<AppRateLimitWindow>,
    secondary: Option<AppRateLimitWindow>,
}

#[derive(Debug, Deserialize)]
struct AppRateLimitWindow {
    #[serde(rename = "usedPercent")]
    used_percent: i32,
    #[serde(rename = "windowDurationMins")]
    window_duration_mins: Option<i64>,
    #[serde(rename = "resetsAt")]
    resets_at: Option<i64>,
}

#[derive(Debug, Deserialize)]
struct AppAccountResponse {
    account: Option<AppAccount>,
}

#[derive(Debug, Deserialize)]
struct AppAccount {
    #[serde(rename = "type")]
    account_type: String,
    email: String,
    #[serde(rename = "planType")]
    plan_type: String,
}

#[derive(Debug, Deserialize)]
struct AppConfigResponse {
    config: AppConfig,
}

#[derive(Debug, Deserialize)]
struct AppConfig {
    model: Option<String>,
    #[serde(rename = "model_reasoning_effort")]
    model_reasoning_effort: Option<String>,
}

fn main() -> Result<()> {
    let cli = Cli::parse();

    match cli.command {
        Command::Version => {
            println!("{}", si_rs_core::version::current_version());
        }
        Command::Help { command, format } => show_help(command.as_deref(), format)?,
        Command::Commands { command } => match command {
            CommandsCommand::List { format } => show_help(None, format)?,
        },
        Command::Settings { command } => match command {
            SettingsCommand::Show { home, settings_file, format } => {
                show_settings(home, settings_file, format)?
            }
        },
        Command::Providers { command } => match command {
            ProvidersCommand::Characteristics { provider, json, format } => {
                let format = if json { OutputFormat::Json } else { format };
                show_provider_characteristics(provider.as_deref(), format)?
            }
        },
        Command::Dyad { command } => match *command {
            DyadCommand::SpawnPlan {
                name,
                role,
                actor_image,
                critic_image,
                codex_model,
                codex_effort_actor,
                codex_effort_critic,
                codex_model_low,
                codex_model_medium,
                codex_model_high,
                codex_effort_low,
                codex_effort_medium,
                codex_effort_high,
                workspace,
                configs,
                vault_env_file,
                codex_volume,
                skills_volume,
                network,
                forward_ports,
                docker_socket,
                profile_id,
                profile_name,
                loop_enabled,
                loop_goal,
                loop_seed_prompt,
                loop_max_turns,
                loop_sleep_seconds,
                loop_startup_delay_seconds,
                loop_turn_timeout_seconds,
                loop_retry_max,
                loop_retry_base_seconds,
                loop_prompt_lines,
                loop_allow_mcp_startup,
                loop_tmux_capture,
                loop_pause_poll_seconds,
                home,
                ssh_auth_sock,
                format,
            } => run_dyad_spawn_plan(
                &name,
                role,
                actor_image,
                critic_image,
                codex_model,
                codex_effort_actor,
                codex_effort_critic,
                codex_model_low,
                codex_model_medium,
                codex_model_high,
                codex_effort_low,
                codex_effort_medium,
                codex_effort_high,
                workspace,
                configs,
                vault_env_file,
                codex_volume,
                skills_volume,
                network,
                forward_ports,
                docker_socket,
                profile_id,
                profile_name,
                loop_enabled,
                loop_goal,
                loop_seed_prompt,
                loop_max_turns,
                loop_sleep_seconds,
                loop_startup_delay_seconds,
                loop_turn_timeout_seconds,
                loop_retry_max,
                loop_retry_base_seconds,
                loop_prompt_lines,
                loop_allow_mcp_startup,
                loop_tmux_capture,
                loop_pause_poll_seconds,
                home,
                ssh_auth_sock,
                format,
            )?,
            DyadCommand::SpawnSpec {
                name,
                role,
                actor_image,
                critic_image,
                codex_model,
                codex_effort_actor,
                codex_effort_critic,
                codex_model_low,
                codex_model_medium,
                codex_model_high,
                codex_effort_low,
                codex_effort_medium,
                codex_effort_high,
                workspace,
                configs,
                vault_env_file,
                codex_volume,
                skills_volume,
                network,
                forward_ports,
                docker_socket,
                profile_id,
                profile_name,
                loop_enabled,
                loop_goal,
                loop_seed_prompt,
                loop_max_turns,
                loop_sleep_seconds,
                loop_startup_delay_seconds,
                loop_turn_timeout_seconds,
                loop_retry_max,
                loop_retry_base_seconds,
                loop_prompt_lines,
                loop_allow_mcp_startup,
                loop_tmux_capture,
                loop_pause_poll_seconds,
                home,
                ssh_auth_sock,
                format,
            } => run_dyad_spawn_spec(
                &name,
                role,
                actor_image,
                critic_image,
                codex_model,
                codex_effort_actor,
                codex_effort_critic,
                codex_model_low,
                codex_model_medium,
                codex_model_high,
                codex_effort_low,
                codex_effort_medium,
                codex_effort_high,
                workspace,
                configs,
                vault_env_file,
                codex_volume,
                skills_volume,
                network,
                forward_ports,
                docker_socket,
                profile_id,
                profile_name,
                loop_enabled,
                loop_goal,
                loop_seed_prompt,
                loop_max_turns,
                loop_sleep_seconds,
                loop_startup_delay_seconds,
                loop_turn_timeout_seconds,
                loop_retry_max,
                loop_retry_base_seconds,
                loop_prompt_lines,
                loop_allow_mcp_startup,
                loop_tmux_capture,
                loop_pause_poll_seconds,
                home,
                ssh_auth_sock,
                format,
            )?,
            DyadCommand::SpawnStart {
                name,
                role,
                actor_image,
                critic_image,
                codex_model,
                codex_effort_actor,
                codex_effort_critic,
                codex_model_low,
                codex_model_medium,
                codex_model_high,
                codex_effort_low,
                codex_effort_medium,
                codex_effort_high,
                workspace,
                configs,
                vault_env_file,
                codex_volume,
                skills_volume,
                network,
                forward_ports,
                docker_socket,
                profile_id,
                profile_name,
                loop_enabled,
                loop_goal,
                loop_seed_prompt,
                loop_max_turns,
                loop_sleep_seconds,
                loop_startup_delay_seconds,
                loop_turn_timeout_seconds,
                loop_retry_max,
                loop_retry_base_seconds,
                loop_prompt_lines,
                loop_allow_mcp_startup,
                loop_tmux_capture,
                loop_pause_poll_seconds,
                home,
                ssh_auth_sock,
                docker_bin,
            } => run_dyad_spawn_start(
                &name,
                role,
                actor_image,
                critic_image,
                codex_model,
                codex_effort_actor,
                codex_effort_critic,
                codex_model_low,
                codex_model_medium,
                codex_model_high,
                codex_effort_low,
                codex_effort_medium,
                codex_effort_high,
                workspace,
                configs,
                vault_env_file,
                codex_volume,
                skills_volume,
                network,
                forward_ports,
                docker_socket,
                profile_id,
                profile_name,
                loop_enabled,
                loop_goal,
                loop_seed_prompt,
                loop_max_turns,
                loop_sleep_seconds,
                loop_startup_delay_seconds,
                loop_turn_timeout_seconds,
                loop_retry_max,
                loop_retry_base_seconds,
                loop_prompt_lines,
                loop_allow_mcp_startup,
                loop_tmux_capture,
                loop_pause_poll_seconds,
                home,
                ssh_auth_sock,
                docker_bin,
            )?,
            DyadCommand::Start { name, docker_bin } => {
                run_dyad_container_action(&name, ContainerAction::Start, docker_bin)?
            }
            DyadCommand::Stop { name, docker_bin } => {
                run_dyad_container_action(&name, ContainerAction::Stop, docker_bin)?
            }
            DyadCommand::Logs { name, member, tail, docker_bin } => {
                run_dyad_container_logs(&name, &member, &tail, docker_bin)?
            }
            DyadCommand::List { format, docker_bin } => run_dyad_list(format, docker_bin)?,
            DyadCommand::Status { name, format, docker_bin } => {
                run_dyad_status(&name, format, docker_bin)?
            }
            DyadCommand::PeekPlan { name, member, session, format } => {
                run_dyad_peek_plan(&name, &member, session, format)?
            }
            DyadCommand::Restart { name, docker_bin } => {
                run_dyad_container_action(&name, ContainerAction::Restart, docker_bin)?
            }
            DyadCommand::Remove { name, docker_bin } => run_dyad_remove(&name, docker_bin)?,
            DyadCommand::Exec { name, member, tty, docker_bin, command } => {
                run_dyad_exec(&name, &member, tty, docker_bin, command)?
            }
            DyadCommand::Cleanup { docker_bin } => run_dyad_cleanup(docker_bin)?,
        },
        Command::Codex { command } => match *command {
            CodexCommand::SpawnPlan {
                name,
                profile_id,
                workspace,
                workdir,
                codex_volume,
                skills_volume,
                gh_volume,
                repo,
                gh_pat,
                docker_socket,
                detach,
                clean_slate,
                image,
                network,
                home,
                ssh_auth_sock,
                vault_env_file,
                include_host_si,
                env,
                format,
            } => show_codex_spawn_plan(
                name,
                profile_id,
                workspace,
                workdir,
                codex_volume,
                skills_volume,
                gh_volume,
                repo,
                gh_pat,
                docker_socket,
                detach,
                clean_slate,
                image,
                network,
                home,
                ssh_auth_sock,
                vault_env_file,
                include_host_si,
                env,
                format,
            )?,
            CodexCommand::SpawnSpec {
                name,
                profile_id,
                workspace,
                workdir,
                codex_volume,
                skills_volume,
                gh_volume,
                repo,
                gh_pat,
                docker_socket,
                detach,
                clean_slate,
                image,
                network,
                home,
                ssh_auth_sock,
                vault_env_file,
                include_host_si,
                env,
                labels,
                ports,
                cmd,
                format,
            } => show_codex_spawn_spec(
                name,
                profile_id,
                workspace,
                workdir,
                codex_volume,
                skills_volume,
                gh_volume,
                repo,
                gh_pat,
                docker_socket,
                detach,
                clean_slate,
                image,
                network,
                home,
                ssh_auth_sock,
                vault_env_file,
                include_host_si,
                env,
                labels,
                ports,
                cmd,
                format,
            )?,
            CodexCommand::SpawnRunArgs {
                name,
                profile_id,
                workspace,
                workdir,
                codex_volume,
                skills_volume,
                gh_volume,
                repo,
                gh_pat,
                docker_socket,
                detach,
                clean_slate,
                image,
                network,
                home,
                ssh_auth_sock,
                vault_env_file,
                include_host_si,
                env,
                labels,
                ports,
                cmd,
                format,
            } => show_codex_spawn_run_args(
                name,
                profile_id,
                workspace,
                workdir,
                codex_volume,
                skills_volume,
                gh_volume,
                repo,
                gh_pat,
                docker_socket,
                detach,
                clean_slate,
                image,
                network,
                home,
                ssh_auth_sock,
                vault_env_file,
                include_host_si,
                env,
                labels,
                ports,
                cmd,
                format,
            )?,
            CodexCommand::SpawnStart {
                name,
                profile_id,
                workspace,
                workdir,
                codex_volume,
                skills_volume,
                gh_volume,
                repo,
                gh_pat,
                docker_socket,
                detach,
                clean_slate,
                image,
                network,
                home,
                ssh_auth_sock,
                vault_env_file,
                include_host_si,
                env,
                labels,
                ports,
                cmd,
                docker_bin,
            } => show_codex_spawn_start(
                name,
                profile_id,
                workspace,
                workdir,
                codex_volume,
                skills_volume,
                gh_volume,
                repo,
                gh_pat,
                docker_socket,
                detach,
                clean_slate,
                image,
                network,
                home,
                ssh_auth_sock,
                vault_env_file,
                include_host_si,
                env,
                labels,
                ports,
                cmd,
                docker_bin,
            )?,
            CodexCommand::RemovePlan { name, format } => show_codex_remove_plan(&name, format)?,
            CodexCommand::Remove { name, volumes, docker_bin } => {
                run_codex_remove(&name, volumes, docker_bin)?
            }
            CodexCommand::Start { name, docker_bin } => {
                run_codex_container_action(&name, ContainerAction::Start, docker_bin)?
            }
            CodexCommand::Stop { name, docker_bin } => {
                run_codex_container_action(&name, ContainerAction::Stop, docker_bin)?
            }
            CodexCommand::Logs { name, tail, docker_bin } => {
                run_codex_container_logs(&name, &tail, false, docker_bin)?
            }
            CodexCommand::Tail { name, tail, docker_bin } => {
                run_codex_container_logs(&name, &tail, true, docker_bin)?
            }
            CodexCommand::Clone { name, repo, gh_pat, docker_bin } => {
                run_codex_clone(&name, &repo, gh_pat.as_deref(), docker_bin)?
            }
            CodexCommand::Exec {
                name,
                workdir,
                interactive,
                tty,
                env,
                user,
                docker_bin,
                command,
            } => run_codex_exec(&name, workdir, interactive, tty, env, &user, docker_bin, command)?,
            CodexCommand::List { format, docker_bin } => run_codex_list(format, docker_bin)?,
            CodexCommand::StatusRead { name, raw, format, docker_bin } => {
                run_codex_status_read(&name, raw, format, docker_bin)?
            }
            CodexCommand::TmuxPlan {
                name,
                start_dir,
                resume_session_id,
                resume_profile,
                format,
            } => run_codex_tmux_plan(
                &name,
                start_dir.as_deref(),
                resume_session_id.as_deref(),
                resume_profile.as_deref(),
                format,
            )?,
            CodexCommand::TmuxCommand { container, format } => {
                run_codex_tmux_command(&container, format)?
            }
            CodexCommand::ReportParse { format } => run_codex_report_parse(format)?,
            CodexCommand::RespawnPlan { name, profile_id, profile_containers, format } => {
                run_codex_respawn_plan(&name, profile_id, profile_containers, format)?
            }
        },
        Command::Paths { command } => match command {
            PathsCommand::Show { home, settings_file, format } => {
                show_paths(home, settings_file, format)?
            }
        },
        Command::Fort { command } => match command {
            FortCommand::SessionState { command } => match command {
                FortSessionStateCommand::Show { path, format } => {
                    show_fort_session_state(path, format)?
                }
                FortSessionStateCommand::Write { path, state_json } => {
                    write_fort_session_state(path, &state_json)?
                }
                FortSessionStateCommand::Classify { path, now_unix, format } => {
                    show_fort_session_state_classification(path, now_unix, format)?
                }
                FortSessionStateCommand::RefreshOutcome {
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
                )?,
                FortSessionStateCommand::Teardown { path, now_unix, format } => {
                    show_fort_session_state_teardown(path, now_unix, format)?
                }
            },
            FortCommand::RuntimeAgentState { command } => match command {
                FortRuntimeAgentStateCommand::Show { path, format } => {
                    show_fort_runtime_agent_state(path, format)?
                }
                FortRuntimeAgentStateCommand::Write { path, state_json } => {
                    write_fort_runtime_agent_state(path, &state_json)?
                }
            },
        },
        Command::Vault { command } => match command {
            VaultCommand::Trust { command } => match command {
                VaultTrustCommand::Lookup { path, repo_root, file, fingerprint, format } => {
                    show_vault_trust_lookup(path, &repo_root, &file, &fingerprint, format)?
                }
            },
        },
    }

    Ok(())
}

fn show_help(command: Option<&str>, format: OutputFormat) -> Result<()> {
    let view = match command {
        Some(name) => {
            let spec = find_root_command(name)
                .ok_or_else(|| anyhow::anyhow!("unknown root command: {name}"))?;
            HelpView { commands: vec![command_view(spec)] }
        }
        None => HelpView { commands: visible_root_commands().map(command_view).collect() },
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
                println!("{}", command.name);
                println!("  category={}", format_category(command.category));
                if command.aliases.is_empty() {
                    println!("  aliases=(none)");
                } else {
                    println!("  aliases={}", command.aliases.join(", "));
                }
                println!("  summary={}", command.summary);
            }
        }
    }

    Ok(())
}

fn show_paths(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let paths = SiPaths::load(&home, settings_file.as_deref())?;
    let view = PathView {
        root: paths.root.display().to_string(),
        settings_file: paths.settings_file.display().to_string(),
        codex_profiles_dir: paths.codex_profiles_dir.display().to_string(),
    };

    match format {
        OutputFormat::Text => {
            println!("root={}", view.root);
            println!("settings_file={}", view.settings_file);
            println!("codex_profiles_dir={}", view.codex_profiles_dir);
        }
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&view)?);
        }
    }

    Ok(())
}

fn show_settings(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;

    match format {
        OutputFormat::Text => {
            println!("schema_version={}", settings.schema_version);
            println!("paths.root={}", settings.paths.root.as_deref().unwrap_or("(none)"));
            println!(
                "paths.settings_file={}",
                settings.paths.settings_file.as_deref().unwrap_or("(none)")
            );
            println!(
                "paths.codex_profiles_dir={}",
                settings.paths.codex_profiles_dir.as_deref().unwrap_or("(none)")
            );
            println!(
                "paths.workspace_root={}",
                settings.paths.workspace_root.as_deref().unwrap_or("(none)")
            );
            println!("codex.workspace={}", settings.codex.workspace.as_deref().unwrap_or("(none)"));
            println!("codex.workdir={}", settings.codex.workdir.as_deref().unwrap_or("(none)"));
            println!("codex.profile={}", settings.codex.profile.as_deref().unwrap_or("(none)"));
            println!("dyad.workspace={}", settings.dyad.workspace.as_deref().unwrap_or("(none)"));
            println!("dyad.configs={}", settings.dyad.configs.as_deref().unwrap_or("(none)"));
        }
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&settings)?);
        }
    }

    Ok(())
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
    println!("container_host={}", render_text_value(&state.container_host));
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

fn render_fort_snapshot_text(snapshot: &si_rs_fort::SessionSnapshot) {
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

fn show_provider_characteristics(provider: Option<&str>, format: OutputFormat) -> Result<()> {
    let ids = match provider {
        Some(raw) => {
            let id = parse_provider_id(raw)
                .ok_or_else(|| anyhow::anyhow!("unknown provider id: {raw}"))?;
            vec![id]
        }
        None => default_ids(),
    };

    let providers = ids
        .into_iter()
        .map(|id| {
            let entry = find_provider(id).expect("provider entry should exist");
            ProviderCharacteristicsView {
                provider: entry.id.as_str().to_owned(),
                base_url: entry.spec.base_url.to_owned(),
                upload_base_url: entry.spec.upload_base_url.map(str::to_owned),
                api_version: entry.spec.api_version.map(str::to_owned),
                auth_style: entry.spec.auth_style.map(str::to_owned),
                rate_limit_per_second: entry.spec.rate_limit_per_second,
                rate_limit_burst: entry.spec.rate_limit_burst,
                public_probe: entry.spec.public_probe.map(|probe| ProviderPublicProbeView {
                    method: probe.method.to_owned(),
                    path: probe.path.to_owned(),
                }),
                capabilities: ProviderCapabilitiesView {
                    supports_pagination: entry.capabilities.supports_pagination,
                    supports_bulk: entry.capabilities.supports_bulk,
                    supports_idempotency: entry.capabilities.supports_idempotency,
                    supports_raw: entry.capabilities.supports_raw,
                },
            }
        })
        .collect::<Vec<_>>();

    let payload = ProvidersCharacteristicsPayload {
        policy: ProvidersPolicyView {
            defaults: "built_in_go",
            admission: "token_bucket",
            adaptive_feedback: true,
        },
        providers,
    };

    match format {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&payload)?);
        }
        OutputFormat::Text => {
            println!("Policy: built-in defaults + runtime adaptive feedback");
            for provider in payload.providers {
                println!("{}", provider.provider);
                println!("  rate={}", provider.rate_limit_per_second);
                println!("  burst={}", provider.rate_limit_burst);
                println!("  auth={}", provider.auth_style.as_deref().unwrap_or("-"));
                let caps = format_provider_caps(&provider.capabilities);
                println!("  caps={}", if caps.is_empty() { "-" } else { &caps });
                if let Some(probe) = provider.public_probe {
                    println!("  public_probe={} {}", probe.method, probe.path);
                } else {
                    println!("  public_probe=-");
                }
            }
        }
    }

    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn run_dyad_spawn_plan(
    name: &str,
    role: Option<String>,
    actor_image: Option<String>,
    critic_image: Option<String>,
    codex_model: Option<String>,
    codex_effort_actor: Option<String>,
    codex_effort_critic: Option<String>,
    codex_model_low: Option<String>,
    codex_model_medium: Option<String>,
    codex_model_high: Option<String>,
    codex_effort_low: Option<String>,
    codex_effort_medium: Option<String>,
    codex_effort_high: Option<String>,
    workspace: PathBuf,
    configs: Option<PathBuf>,
    vault_env_file: Option<PathBuf>,
    codex_volume: Option<String>,
    skills_volume: Option<String>,
    network: Option<String>,
    forward_ports: Option<String>,
    docker_socket: bool,
    profile_id: Option<String>,
    profile_name: Option<String>,
    loop_enabled: Option<bool>,
    loop_goal: Option<String>,
    loop_seed_prompt: Option<String>,
    loop_max_turns: Option<i32>,
    loop_sleep_seconds: Option<i32>,
    loop_startup_delay_seconds: Option<i32>,
    loop_turn_timeout_seconds: Option<i32>,
    loop_retry_max: Option<i32>,
    loop_retry_base_seconds: Option<i32>,
    loop_prompt_lines: Option<i32>,
    loop_allow_mcp_startup: Option<bool>,
    loop_tmux_capture: Option<String>,
    loop_pause_poll_seconds: Option<i32>,
    home: Option<PathBuf>,
    ssh_auth_sock: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let plan = build_dyad_plan(
        name,
        role,
        actor_image,
        critic_image,
        codex_model,
        codex_effort_actor,
        codex_effort_critic,
        codex_model_low,
        codex_model_medium,
        codex_model_high,
        codex_effort_low,
        codex_effort_medium,
        codex_effort_high,
        workspace,
        configs,
        vault_env_file,
        codex_volume,
        skills_volume,
        network,
        forward_ports,
        docker_socket,
        profile_id,
        profile_name,
        loop_enabled,
        loop_goal,
        loop_seed_prompt,
        loop_max_turns,
        loop_sleep_seconds,
        loop_startup_delay_seconds,
        loop_turn_timeout_seconds,
        loop_retry_max,
        loop_retry_base_seconds,
        loop_prompt_lines,
        loop_allow_mcp_startup,
        loop_tmux_capture,
        loop_pause_poll_seconds,
        home,
        ssh_auth_sock,
    )?;
    let view = DyadSpawnPlanView {
        dyad: plan.dyad,
        role: plan.role,
        network_name: plan.network_name,
        workspace_host: plan.workspace_host.display().to_string(),
        configs_host: plan.configs_host.display().to_string(),
        codex_volume: plan.codex_volume,
        skills_volume: plan.skills_volume,
        forward_ports: plan.forward_ports,
        docker_socket: plan.docker_socket,
        actor: dyad_member_plan_view(plan.actor),
        critic: dyad_member_plan_view(plan.critic),
    };

    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            println!("dyad={}", view.dyad);
            println!("role={}", view.role);
            println!("network_name={}", view.network_name);
            println!("actor.container_name={}", view.actor.container_name);
            println!("critic.container_name={}", view.critic.container_name);
        }
    }

    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn run_dyad_spawn_spec(
    name: &str,
    role: Option<String>,
    actor_image: Option<String>,
    critic_image: Option<String>,
    codex_model: Option<String>,
    codex_effort_actor: Option<String>,
    codex_effort_critic: Option<String>,
    codex_model_low: Option<String>,
    codex_model_medium: Option<String>,
    codex_model_high: Option<String>,
    codex_effort_low: Option<String>,
    codex_effort_medium: Option<String>,
    codex_effort_high: Option<String>,
    workspace: PathBuf,
    configs: Option<PathBuf>,
    vault_env_file: Option<PathBuf>,
    codex_volume: Option<String>,
    skills_volume: Option<String>,
    network: Option<String>,
    forward_ports: Option<String>,
    docker_socket: bool,
    profile_id: Option<String>,
    profile_name: Option<String>,
    loop_enabled: Option<bool>,
    loop_goal: Option<String>,
    loop_seed_prompt: Option<String>,
    loop_max_turns: Option<i32>,
    loop_sleep_seconds: Option<i32>,
    loop_startup_delay_seconds: Option<i32>,
    loop_turn_timeout_seconds: Option<i32>,
    loop_retry_max: Option<i32>,
    loop_retry_base_seconds: Option<i32>,
    loop_prompt_lines: Option<i32>,
    loop_allow_mcp_startup: Option<bool>,
    loop_tmux_capture: Option<String>,
    loop_pause_poll_seconds: Option<i32>,
    home: Option<PathBuf>,
    ssh_auth_sock: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let plan = build_dyad_plan(
        name,
        role,
        actor_image,
        critic_image,
        codex_model,
        codex_effort_actor,
        codex_effort_critic,
        codex_model_low,
        codex_model_medium,
        codex_model_high,
        codex_effort_low,
        codex_effort_medium,
        codex_effort_high,
        workspace,
        configs,
        vault_env_file,
        codex_volume,
        skills_volume,
        network,
        forward_ports,
        docker_socket,
        profile_id,
        profile_name,
        loop_enabled,
        loop_goal,
        loop_seed_prompt,
        loop_max_turns,
        loop_sleep_seconds,
        loop_startup_delay_seconds,
        loop_turn_timeout_seconds,
        loop_retry_max,
        loop_retry_base_seconds,
        loop_prompt_lines,
        loop_allow_mcp_startup,
        loop_tmux_capture,
        loop_pause_poll_seconds,
        home,
        ssh_auth_sock,
    )?;
    let (actor, critic) = build_dyad_container_specs(&plan)?;
    let view = DyadSpawnSpecView {
        actor: dyad_container_spec_view(&actor),
        critic: dyad_container_spec_view(&critic),
    };

    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            println!("actor.name={}", view.actor.name.as_deref().unwrap_or("-"));
            println!("critic.name={}", view.critic.name.as_deref().unwrap_or("-"));
            println!("actor.bind_mounts={}", view.actor.bind_mounts.len());
            println!("critic.bind_mounts={}", view.critic.bind_mounts.len());
            println!("actor.published_ports={}", view.actor.published_ports.len());
        }
    }

    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn run_dyad_spawn_start(
    name: &str,
    role: Option<String>,
    actor_image: Option<String>,
    critic_image: Option<String>,
    codex_model: Option<String>,
    codex_effort_actor: Option<String>,
    codex_effort_critic: Option<String>,
    codex_model_low: Option<String>,
    codex_model_medium: Option<String>,
    codex_model_high: Option<String>,
    codex_effort_low: Option<String>,
    codex_effort_medium: Option<String>,
    codex_effort_high: Option<String>,
    workspace: PathBuf,
    configs: Option<PathBuf>,
    vault_env_file: Option<PathBuf>,
    codex_volume: Option<String>,
    skills_volume: Option<String>,
    network: Option<String>,
    forward_ports: Option<String>,
    docker_socket: bool,
    profile_id: Option<String>,
    profile_name: Option<String>,
    loop_enabled: Option<bool>,
    loop_goal: Option<String>,
    loop_seed_prompt: Option<String>,
    loop_max_turns: Option<i32>,
    loop_sleep_seconds: Option<i32>,
    loop_startup_delay_seconds: Option<i32>,
    loop_turn_timeout_seconds: Option<i32>,
    loop_retry_max: Option<i32>,
    loop_retry_base_seconds: Option<i32>,
    loop_prompt_lines: Option<i32>,
    loop_allow_mcp_startup: Option<bool>,
    loop_tmux_capture: Option<String>,
    loop_pause_poll_seconds: Option<i32>,
    home: Option<PathBuf>,
    ssh_auth_sock: Option<PathBuf>,
    docker_bin: Option<PathBuf>,
) -> Result<()> {
    let plan = build_dyad_plan(
        name,
        role,
        actor_image,
        critic_image,
        codex_model,
        codex_effort_actor,
        codex_effort_critic,
        codex_model_low,
        codex_model_medium,
        codex_model_high,
        codex_effort_low,
        codex_effort_medium,
        codex_effort_high,
        workspace,
        configs,
        vault_env_file,
        codex_volume,
        skills_volume,
        network,
        forward_ports,
        docker_socket,
        profile_id,
        profile_name,
        loop_enabled,
        loop_goal,
        loop_seed_prompt,
        loop_max_turns,
        loop_sleep_seconds,
        loop_startup_delay_seconds,
        loop_turn_timeout_seconds,
        loop_retry_max,
        loop_retry_base_seconds,
        loop_prompt_lines,
        loop_allow_mcp_startup,
        loop_tmux_capture,
        loop_pause_poll_seconds,
        home,
        ssh_auth_sock,
    )?;
    let (actor, critic) = build_dyad_container_specs(&plan)?;
    let docker_program =
        docker_bin.unwrap_or_else(|| si_rs_docker::docker_binary_path().to_path_buf());
    for spec in [&actor, &critic] {
        let command = spec.docker_run_command(docker_program.display().to_string())?;
        let output = ProcessRunner.run(&command, &RunOptions::default())?;
        if !output.status.success() {
            let stderr = String::from_utf8_lossy(&output.stderr);
            anyhow::bail!("docker run failed: {}", stderr.trim());
        }
        print!("{}", String::from_utf8_lossy(&output.stdout));
    }
    Ok(())
}

fn run_dyad_container_action(
    dyad: &str,
    action: ContainerAction,
    docker_bin: Option<PathBuf>,
) -> Result<()> {
    let docker_program =
        docker_bin.unwrap_or_else(|| si_rs_docker::docker_binary_path().to_path_buf());
    for member in ["actor", "critic"] {
        let container_name = si_rs_dyad::dyad_container_name(dyad, member);
        let command = docker_container_action_command(
            docker_program.display().to_string(),
            action,
            container_name,
        )?;
        let output = ProcessRunner.run(&command, &RunOptions::default())?;
        if !output.status.success() {
            let stderr = String::from_utf8_lossy(&output.stderr);
            anyhow::bail!("docker {} failed: {}", action.as_str(), stderr.trim());
        }
        print!("{}", String::from_utf8_lossy(&output.stdout));
    }
    Ok(())
}

fn run_dyad_container_logs(
    dyad: &str,
    member: &str,
    tail: &str,
    docker_bin: Option<PathBuf>,
) -> Result<()> {
    let docker_program =
        docker_bin.unwrap_or_else(|| si_rs_docker::docker_binary_path().to_path_buf());
    let container_name = si_rs_dyad::dyad_container_name(dyad, member);
    let command = docker_container_logs_command(
        docker_program.display().to_string(),
        container_name,
        tail,
        false,
    )?;
    let output = ProcessRunner.run(&command, &RunOptions::default())?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        anyhow::bail!("docker logs failed: {}", stderr.trim());
    }
    print!("{}", String::from_utf8_lossy(&output.stdout));
    Ok(())
}

fn run_dyad_remove(dyad: &str, docker_bin: Option<PathBuf>) -> Result<()> {
    let docker_program =
        docker_bin.unwrap_or_else(|| si_rs_docker::docker_binary_path().to_path_buf());
    for member in ["actor", "critic"] {
        let container_name = si_rs_dyad::dyad_container_name(dyad, member);
        let command = docker_container_remove_command(
            docker_program.display().to_string(),
            container_name,
            true,
        )?;
        let output = ProcessRunner.run(&command, &RunOptions::default())?;
        if !output.status.success() {
            let stderr = String::from_utf8_lossy(&output.stderr);
            anyhow::bail!("docker rm failed: {}", stderr.trim());
        }
        print!("{}", String::from_utf8_lossy(&output.stdout));
    }
    Ok(())
}

fn run_dyad_exec(
    dyad: &str,
    member: &str,
    tty: bool,
    docker_bin: Option<PathBuf>,
    command: Vec<String>,
) -> Result<()> {
    if command.is_empty() {
        anyhow::bail!("exec command is required");
    }
    let member = member.trim();
    if member.is_empty() {
        anyhow::bail!("member is required");
    }
    let docker_program =
        docker_bin.unwrap_or_else(|| si_rs_docker::docker_binary_path().to_path_buf());
    let container_name = si_rs_dyad::dyad_container_name(dyad, member);
    let spec = ContainerExecSpec::new(container_name)
        .user("si")
        .interactive(true)
        .tty(tty)
        .command(command);
    let command = docker_container_exec_command(docker_program.display().to_string(), &spec)?;
    let output = ProcessRunner
        .run(&command, &RunOptions { stdin: StdinBehavior::Inherit, ..RunOptions::default() })?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        anyhow::bail!("docker exec failed: {}", stderr.trim());
    }
    print!("{}", String::from_utf8_lossy(&output.stdout));
    Ok(())
}

fn run_dyad_cleanup(docker_bin: Option<PathBuf>) -> Result<()> {
    let items = read_dyad_containers(docker_bin.clone())?;
    let docker_program =
        docker_bin.unwrap_or_else(|| si_rs_docker::docker_binary_path().to_path_buf());
    let mut removed = 0;
    for item in items {
        if item.state == "running" {
            continue;
        }
        let command =
            docker_container_remove_command(docker_program.display().to_string(), item.name, true)?;
        let output = ProcessRunner.run(&command, &RunOptions::default())?;
        if output.status.success() {
            removed += 1;
        }
    }
    println!("removed={removed}");
    Ok(())
}

fn run_dyad_list(format: OutputFormat, docker_bin: Option<PathBuf>) -> Result<()> {
    let entries = read_dyad_rows(docker_bin)?;
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&entries)?),
        OutputFormat::Text => {
            for item in entries {
                println!("{}\t{}\t{}\t{}", item.dyad, item.role, item.actor, item.critic);
            }
        }
    }
    Ok(())
}

fn run_dyad_status(name: &str, format: OutputFormat, docker_bin: Option<PathBuf>) -> Result<()> {
    let items = read_dyad_containers(docker_bin)?;
    let name = name.trim();
    let mut actor = None;
    let mut critic = None;
    for item in items {
        if item.dyad != name {
            continue;
        }
        let status = DyadContainerStatusView {
            name: item.name.clone(),
            id: item.id.clone(),
            status: item.state.clone(),
        };
        match item.member.as_str() {
            "actor" => actor = Some(status),
            "critic" => critic = Some(status),
            _ => {}
        }
    }
    let view = DyadStatusView {
        dyad: name.to_owned(),
        found: actor.is_some() || critic.is_some(),
        actor,
        critic,
    };
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            println!("dyad={}", view.dyad);
            println!("found={}", view.found);
            println!(
                "actor={}",
                view.actor.as_ref().map(|item| item.status.as_str()).unwrap_or("(none)")
            );
            println!(
                "critic={}",
                view.critic.as_ref().map(|item| item.status.as_str()).unwrap_or("(none)")
            );
        }
    }
    Ok(())
}

fn run_dyad_peek_plan(
    name: &str,
    member: &str,
    session: Option<String>,
    format: OutputFormat,
) -> Result<()> {
    let plan = build_dyad_peek_plan(name, member, session.as_deref())?;
    let view = DyadPeekPlanView {
        dyad: plan.dyad,
        member: plan.member,
        actor_container_name: plan.actor_container_name,
        critic_container_name: plan.critic_container_name,
        actor_session_name: plan.actor_session_name,
        critic_session_name: plan.critic_session_name,
        peek_session_name: plan.peek_session_name,
        actor_attach_command: plan.actor_attach_command,
        critic_attach_command: plan.critic_attach_command,
    };
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            println!("dyad={}", view.dyad);
            println!("member={}", view.member);
            println!("peek_session_name={}", view.peek_session_name);
            println!("actor_container_name={}", view.actor_container_name);
            println!("critic_container_name={}", view.critic_container_name);
            println!("actor_session_name={}", view.actor_session_name);
            println!("critic_session_name={}", view.critic_session_name);
        }
    }
    Ok(())
}

#[derive(Debug)]
struct DyadContainerListItem {
    name: String,
    state: String,
    id: String,
    dyad: String,
    role: String,
    member: String,
}

fn read_dyad_rows(docker_bin: Option<PathBuf>) -> Result<Vec<DyadListEntryView>> {
    let items = read_dyad_containers(docker_bin)?;
    let mut rows = std::collections::BTreeMap::<String, DyadListEntryView>::new();
    for item in items {
        let entry = rows.entry(item.dyad.clone()).or_insert_with(|| DyadListEntryView {
            dyad: item.dyad.clone(),
            role: item.role.clone(),
            actor: String::new(),
            critic: String::new(),
        });
        if entry.role.trim().is_empty() && !item.role.trim().is_empty() {
            entry.role = item.role.clone();
        }
        match item.member.as_str() {
            "actor" => entry.actor = item.state.clone(),
            "critic" => entry.critic = item.state.clone(),
            _ => {}
        }
    }
    Ok(rows.into_values().collect())
}

fn read_dyad_containers(docker_bin: Option<PathBuf>) -> Result<Vec<DyadContainerListItem>> {
    let docker_program =
        docker_bin.unwrap_or_else(|| si_rs_docker::docker_binary_path().to_path_buf());
    let command = docker_container_list_with_format_command(
        docker_program.display().to_string(),
        "app=si-dyad",
        true,
        "{{.Names}}\t{{.State}}\t{{.ID}}\t{{.Label \"si.dyad\"}}\t{{.Label \"si.role\"}}\t{{.Label \"si.member\"}}",
    )?;
    let output = ProcessRunner.run(&command, &RunOptions::default())?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        anyhow::bail!("docker ps failed: {}", stderr.trim());
    }
    let stdout = String::from_utf8_lossy(&output.stdout);
    let mut items = Vec::new();
    for line in stdout.lines() {
        let line = line.trim();
        if line.is_empty() {
            continue;
        }
        let mut parts = line.splitn(6, '\t');
        let name = parts.next().unwrap_or("").trim();
        let state = parts.next().unwrap_or("").trim();
        let id = parts.next().unwrap_or("").trim();
        let dyad = parts.next().unwrap_or("").trim();
        let role = parts.next().unwrap_or("").trim();
        let member = parts.next().unwrap_or("").trim();
        if name.is_empty() || dyad.is_empty() {
            continue;
        }
        items.push(DyadContainerListItem {
            name: name.to_owned(),
            state: state.to_owned(),
            id: id.to_owned(),
            dyad: dyad.to_owned(),
            role: role.to_owned(),
            member: member.to_owned(),
        });
    }
    Ok(items)
}

#[allow(clippy::too_many_arguments)]
fn build_dyad_plan(
    name: &str,
    role: Option<String>,
    actor_image: Option<String>,
    critic_image: Option<String>,
    codex_model: Option<String>,
    codex_effort_actor: Option<String>,
    codex_effort_critic: Option<String>,
    codex_model_low: Option<String>,
    codex_model_medium: Option<String>,
    codex_model_high: Option<String>,
    codex_effort_low: Option<String>,
    codex_effort_medium: Option<String>,
    codex_effort_high: Option<String>,
    workspace: PathBuf,
    configs: Option<PathBuf>,
    vault_env_file: Option<PathBuf>,
    codex_volume: Option<String>,
    skills_volume: Option<String>,
    network: Option<String>,
    forward_ports: Option<String>,
    docker_socket: bool,
    profile_id: Option<String>,
    profile_name: Option<String>,
    loop_enabled: Option<bool>,
    loop_goal: Option<String>,
    loop_seed_prompt: Option<String>,
    loop_max_turns: Option<i32>,
    loop_sleep_seconds: Option<i32>,
    loop_startup_delay_seconds: Option<i32>,
    loop_turn_timeout_seconds: Option<i32>,
    loop_retry_max: Option<i32>,
    loop_retry_base_seconds: Option<i32>,
    loop_prompt_lines: Option<i32>,
    loop_allow_mcp_startup: Option<bool>,
    loop_tmux_capture: Option<String>,
    loop_pause_poll_seconds: Option<i32>,
    home: Option<PathBuf>,
    ssh_auth_sock: Option<PathBuf>,
) -> Result<si_rs_dyad::SpawnPlan> {
    let host_ctx = HostMountContext {
        home_dir: home.or_else(|| std::env::var_os("HOME").map(PathBuf::from)),
        ssh_auth_sock: ssh_auth_sock
            .or_else(|| std::env::var_os("SSH_AUTH_SOCK").map(PathBuf::from)),
    };
    Ok(build_dyad_spawn_plan(
        &DyadSpawnRequest {
            name: name.trim().to_owned(),
            role,
            actor_image,
            critic_image,
            codex_model,
            codex_effort_actor,
            codex_effort_critic,
            codex_model_low,
            codex_model_medium,
            codex_model_high,
            codex_effort_low,
            codex_effort_medium,
            codex_effort_high,
            workspace_host: workspace,
            configs_host: configs,
            vault_env_file,
            codex_volume,
            skills_volume,
            network_name: network,
            forward_ports,
            docker_socket,
            profile_id,
            profile_name,
            loop_enabled,
            loop_goal,
            loop_seed_prompt,
            loop_max_turns,
            loop_sleep_seconds,
            loop_startup_delay_seconds,
            loop_turn_timeout_seconds,
            loop_retry_max,
            loop_retry_base_seconds,
            loop_prompt_lines,
            loop_allow_mcp_startup,
            loop_tmux_capture,
            loop_pause_poll_seconds,
        },
        &host_ctx,
    )?)
}

fn dyad_member_plan_view(plan: si_rs_dyad::MemberPlan) -> DyadMemberPlanView {
    DyadMemberPlanView {
        member: plan.member,
        container_name: plan.container_name,
        image: plan.image,
        workdir: plan.workdir.map(|value| value.display().to_string()),
        env: plan.env,
        bind_mounts: plan
            .bind_mounts
            .into_iter()
            .map(|mount| DyadBindMountView {
                source: mount.source.display().to_string(),
                target: mount.target.display().to_string(),
                read_only: mount.read_only,
            })
            .collect(),
        volume_mounts: plan
            .volume_mounts
            .into_iter()
            .map(|mount| DyadVolumeMountView {
                source: mount.source,
                target: mount.target.display().to_string(),
                read_only: mount.read_only,
            })
            .collect(),
        labels: plan.labels.into_iter().map(|(key, value)| DyadLabelView { key, value }).collect(),
        command: plan.command,
    }
}

fn dyad_container_spec_view(spec: &si_rs_docker::ContainerSpec) -> DyadContainerSpecView {
    DyadContainerSpecView {
        image: spec.image().to_owned(),
        name: spec.name_ref().map(str::to_owned),
        network: spec.network_ref().map(str::to_owned),
        restart_policy: spec.restart_policy_ref().map(str::to_owned),
        working_dir: spec.working_dir().map(|path| path.display().to_string()),
        command: spec.command_args().to_vec(),
        env: spec
            .env_vars()
            .iter()
            .map(|(key, value)| CodexEnvVarView { key: key.clone(), value: value.clone() })
            .collect(),
        bind_mounts: spec
            .bind_mounts()
            .iter()
            .map(|mount| DyadBindMountView {
                source: mount.source().display().to_string(),
                target: mount.target().display().to_string(),
                read_only: mount.is_read_only(),
            })
            .collect(),
        volume_mounts: spec
            .volume_mounts()
            .iter()
            .map(|mount| DyadVolumeMountView {
                source: mount.source().to_owned(),
                target: mount.target().display().to_string(),
                read_only: mount.is_read_only(),
            })
            .collect(),
        labels: spec
            .labels()
            .iter()
            .map(|(key, value)| DyadLabelView { key: key.clone(), value: value.clone() })
            .collect(),
        published_ports: spec
            .published_ports()
            .iter()
            .map(|port| CodexPublishedPortView {
                host_ip: port.host_ip_ref().to_owned(),
                host_port: port.host_port().to_owned(),
                container_port: port.container_port(),
            })
            .collect(),
        user: spec.user_ref().map(str::to_owned),
        detach: spec.detach_enabled(),
        auto_remove: spec.auto_remove_enabled(),
    }
}

#[allow(clippy::too_many_arguments)]
fn show_codex_spawn_plan(
    name: Option<String>,
    profile_id: Option<String>,
    workspace: PathBuf,
    workdir: Option<String>,
    codex_volume: Option<String>,
    skills_volume: Option<String>,
    gh_volume: Option<String>,
    repo: Option<String>,
    gh_pat: Option<String>,
    docker_socket: bool,
    detach: bool,
    clean_slate: bool,
    image: Option<String>,
    network: Option<String>,
    home: Option<PathBuf>,
    ssh_auth_sock: Option<PathBuf>,
    vault_env_file: Option<PathBuf>,
    include_host_si: bool,
    env: Vec<String>,
    format: OutputFormat,
) -> Result<()> {
    let mut host_ctx = HostMountContext::from_env();
    if home.is_some() {
        host_ctx.home_dir = home;
    }
    if ssh_auth_sock.is_some() {
        host_ctx.ssh_auth_sock = ssh_auth_sock;
    }
    let plan = build_spawn_plan(
        &SpawnRequest {
            name,
            profile_id,
            image,
            network_name: network,
            workspace_host: workspace,
            workdir,
            codex_volume,
            skills_volume,
            gh_volume,
            repo,
            gh_pat,
            docker_socket,
            clean_slate,
            detach,
            container_home: None,
            host_vault_env_file: vault_env_file,
            include_host_si,
            additional_env: env,
        },
        &host_ctx,
    )?;

    let view = CodexSpawnPlanView {
        name: plan.name,
        container_name: plan.container_name,
        image: plan.image,
        network_name: plan.network_name,
        workspace_host: plan.workspace_host.display().to_string(),
        workspace_primary_target: plan.workspace_primary_target.display().to_string(),
        workspace_mirror_target: plan.workspace_mirror_target.display().to_string(),
        workdir: plan.workdir.display().to_string(),
        codex_volume: plan.codex_volume,
        skills_volume: plan.skills_volume,
        gh_volume: plan.gh_volume,
        docker_socket: plan.docker_socket,
        clean_slate: plan.clean_slate,
        detach: plan.detach,
        env: plan.env,
        mounts: plan
            .mounts
            .into_iter()
            .map(|mount| CodexBindMountView {
                source: mount.source().display().to_string(),
                target: mount.target().display().to_string(),
                read_only: mount.is_read_only(),
            })
            .collect(),
    };

    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            println!("name={}", view.name);
            println!("container_name={}", view.container_name);
            println!("image={}", view.image);
            println!("network_name={}", view.network_name);
            println!("workspace_host={}", view.workspace_host);
            println!("workspace_primary_target={}", view.workspace_primary_target);
            println!("workspace_mirror_target={}", view.workspace_mirror_target);
            println!("workdir={}", view.workdir);
            println!("codex_volume={}", view.codex_volume);
            println!("skills_volume={}", view.skills_volume);
            println!("gh_volume={}", view.gh_volume);
            println!("docker_socket={}", view.docker_socket);
            println!("clean_slate={}", view.clean_slate);
            println!("detach={}", view.detach);
            println!("env={}", view.env.join(","));
            println!("mounts={}", view.mounts.len());
        }
    }

    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn show_codex_spawn_spec(
    name: Option<String>,
    profile_id: Option<String>,
    workspace: PathBuf,
    workdir: Option<String>,
    codex_volume: Option<String>,
    skills_volume: Option<String>,
    gh_volume: Option<String>,
    repo: Option<String>,
    gh_pat: Option<String>,
    docker_socket: bool,
    detach: bool,
    clean_slate: bool,
    image: Option<String>,
    network: Option<String>,
    home: Option<PathBuf>,
    ssh_auth_sock: Option<PathBuf>,
    vault_env_file: Option<PathBuf>,
    include_host_si: bool,
    env: Vec<String>,
    labels: Vec<String>,
    ports: Vec<String>,
    cmd: Option<String>,
    format: OutputFormat,
) -> Result<()> {
    let mut host_ctx = HostMountContext::from_env();
    if home.is_some() {
        host_ctx.home_dir = home;
    }
    if ssh_auth_sock.is_some() {
        host_ctx.ssh_auth_sock = ssh_auth_sock;
    }
    let plan = build_spawn_plan(
        &SpawnRequest {
            name,
            profile_id,
            image,
            network_name: network,
            workspace_host: workspace,
            workdir,
            codex_volume,
            skills_volume,
            gh_volume,
            repo,
            gh_pat,
            docker_socket,
            clean_slate,
            detach,
            container_home: None,
            host_vault_env_file: vault_env_file,
            include_host_si,
            additional_env: env,
        },
        &host_ctx,
    )?;
    let spec = build_container_spec(&plan, &SpawnContainerOptions { command: cmd, labels, ports })?;
    let view = CodexSpawnSpecView {
        image: spec.image().to_owned(),
        name: spec.name_ref().map(str::to_owned),
        network: spec.network_ref().map(str::to_owned),
        restart_policy: spec.restart_policy_ref().map(str::to_owned),
        working_dir: spec.working_dir().map(|path| path.display().to_string()),
        command: spec.command_args().to_vec(),
        env: spec
            .env_vars()
            .iter()
            .map(|(key, value)| CodexEnvVarView { key: key.clone(), value: value.clone() })
            .collect(),
        labels: spec
            .labels()
            .iter()
            .map(|(key, value)| CodexEnvVarView { key: key.clone(), value: value.clone() })
            .collect(),
        published_ports: spec
            .published_ports()
            .iter()
            .map(|port| CodexPublishedPortView {
                host_ip: port.host_ip_ref().to_owned(),
                host_port: port.host_port().to_owned(),
                container_port: port.container_port(),
            })
            .collect(),
        bind_mounts: spec
            .bind_mounts()
            .iter()
            .map(|mount| CodexBindMountView {
                source: mount.source().display().to_string(),
                target: mount.target().display().to_string(),
                read_only: mount.is_read_only(),
            })
            .collect(),
        volume_mounts: spec
            .volume_mounts()
            .iter()
            .map(|mount| CodexVolumeMountView {
                source: mount.source().to_owned(),
                target: mount.target().display().to_string(),
                read_only: mount.is_read_only(),
            })
            .collect(),
        user: spec.user_ref().map(str::to_owned),
        detach: spec.detach_enabled(),
        auto_remove: spec.auto_remove_enabled(),
    };

    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            println!("image={}", view.image);
            println!("name={}", view.name.as_deref().unwrap_or("-"));
            println!("network={}", view.network.as_deref().unwrap_or("-"));
            println!("restart_policy={}", view.restart_policy.as_deref().unwrap_or("-"));
            println!("working_dir={}", view.working_dir.as_deref().unwrap_or("-"));
            println!("command={}", view.command.join(" "));
            println!("env={}", view.env.len());
            println!("labels={}", view.labels.len());
            println!("published_ports={}", view.published_ports.len());
            println!("bind_mounts={}", view.bind_mounts.len());
            println!("volume_mounts={}", view.volume_mounts.len());
        }
    }

    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn show_codex_spawn_run_args(
    name: Option<String>,
    profile_id: Option<String>,
    workspace: PathBuf,
    workdir: Option<String>,
    codex_volume: Option<String>,
    skills_volume: Option<String>,
    gh_volume: Option<String>,
    repo: Option<String>,
    gh_pat: Option<String>,
    docker_socket: bool,
    detach: bool,
    clean_slate: bool,
    image: Option<String>,
    network: Option<String>,
    home: Option<PathBuf>,
    ssh_auth_sock: Option<PathBuf>,
    vault_env_file: Option<PathBuf>,
    include_host_si: bool,
    env: Vec<String>,
    labels: Vec<String>,
    ports: Vec<String>,
    cmd: Option<String>,
    format: OutputFormat,
) -> Result<()> {
    let mut host_ctx = HostMountContext::from_env();
    if home.is_some() {
        host_ctx.home_dir = home;
    }
    if ssh_auth_sock.is_some() {
        host_ctx.ssh_auth_sock = ssh_auth_sock;
    }
    let plan = build_spawn_plan(
        &SpawnRequest {
            name,
            profile_id,
            image,
            network_name: network,
            workspace_host: workspace,
            workdir,
            codex_volume,
            skills_volume,
            gh_volume,
            repo,
            gh_pat,
            docker_socket,
            clean_slate,
            detach,
            container_home: None,
            host_vault_env_file: vault_env_file,
            include_host_si,
            additional_env: env,
        },
        &host_ctx,
    )?;
    let spec = build_container_spec(&plan, &SpawnContainerOptions { command: cmd, labels, ports })?;
    let args = spec.docker_run_args()?;
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&args)?),
        OutputFormat::Text => println!("{}", args.join(" ")),
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn show_codex_spawn_start(
    name: Option<String>,
    profile_id: Option<String>,
    workspace: PathBuf,
    workdir: Option<String>,
    codex_volume: Option<String>,
    skills_volume: Option<String>,
    gh_volume: Option<String>,
    repo: Option<String>,
    gh_pat: Option<String>,
    docker_socket: bool,
    detach: bool,
    clean_slate: bool,
    image: Option<String>,
    network: Option<String>,
    home: Option<PathBuf>,
    ssh_auth_sock: Option<PathBuf>,
    vault_env_file: Option<PathBuf>,
    include_host_si: bool,
    env: Vec<String>,
    labels: Vec<String>,
    ports: Vec<String>,
    cmd: Option<String>,
    docker_bin: Option<PathBuf>,
) -> Result<()> {
    let mut host_ctx = HostMountContext::from_env();
    if home.is_some() {
        host_ctx.home_dir = home;
    }
    if ssh_auth_sock.is_some() {
        host_ctx.ssh_auth_sock = ssh_auth_sock;
    }
    let plan = build_spawn_plan(
        &SpawnRequest {
            name,
            profile_id,
            image,
            network_name: network,
            workspace_host: workspace,
            workdir,
            codex_volume,
            skills_volume,
            gh_volume,
            repo,
            gh_pat,
            docker_socket,
            clean_slate,
            detach,
            container_home: None,
            host_vault_env_file: vault_env_file,
            include_host_si,
            additional_env: env,
        },
        &host_ctx,
    )?;
    let spec = build_container_spec(&plan, &SpawnContainerOptions { command: cmd, labels, ports })?;
    let docker_program =
        docker_bin.unwrap_or_else(|| si_rs_docker::docker_binary_path().to_path_buf());
    let command = spec.docker_run_command(docker_program.display().to_string())?;
    let output = ProcessRunner.run(&command, &RunOptions::default())?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        anyhow::bail!("docker run failed: {}", stderr.trim());
    }
    print!("{}", String::from_utf8_lossy(&output.stdout));
    Ok(())
}

fn show_codex_remove_plan(name: &str, format: OutputFormat) -> Result<()> {
    let artifacts = build_remove_artifacts(name)?;
    let view = CodexRemovePlanView {
        name: artifacts.name,
        container_name: artifacts.container_name,
        slug: artifacts.slug,
        codex_volume: artifacts.codex_volume,
        gh_volume: artifacts.gh_volume,
    };
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            println!("name={}", view.name);
            println!("container_name={}", view.container_name);
            println!("slug={}", view.slug);
            println!("codex_volume={}", view.codex_volume);
            println!("gh_volume={}", view.gh_volume);
        }
    }
    Ok(())
}

fn run_codex_remove(name: &str, volumes: bool, docker_bin: Option<PathBuf>) -> Result<()> {
    let artifacts = build_remove_artifacts(name)?;
    let docker_program =
        docker_bin.unwrap_or_else(|| si_rs_docker::docker_binary_path().to_path_buf());
    let remove_container = docker_container_remove_command(
        docker_program.display().to_string(),
        artifacts.container_name.clone(),
        true,
    )?;
    let output = ProcessRunner.run(&remove_container, &RunOptions::default())?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        anyhow::bail!("docker rm failed: {}", stderr.trim());
    }
    print!("{}", String::from_utf8_lossy(&output.stdout));
    if volumes {
        for volume_name in [&artifacts.codex_volume, &artifacts.gh_volume] {
            let remove_volume = docker_volume_remove_command(
                docker_program.display().to_string(),
                volume_name.clone(),
                true,
            )?;
            let output = ProcessRunner.run(&remove_volume, &RunOptions::default())?;
            if !output.status.success() {
                let stderr = String::from_utf8_lossy(&output.stderr);
                anyhow::bail!("docker volume rm failed: {}", stderr.trim());
            }
            print!("{}", String::from_utf8_lossy(&output.stdout));
        }
    }
    Ok(())
}

fn run_codex_container_action(
    name: &str,
    action: ContainerAction,
    docker_bin: Option<PathBuf>,
) -> Result<()> {
    let artifacts = build_remove_artifacts(name)?;
    let docker_program =
        docker_bin.unwrap_or_else(|| si_rs_docker::docker_binary_path().to_path_buf());
    let command = docker_container_action_command(
        docker_program.display().to_string(),
        action,
        artifacts.container_name,
    )?;
    let output = ProcessRunner.run(&command, &RunOptions::default())?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        anyhow::bail!("docker {} failed: {}", action.as_str(), stderr.trim());
    }
    print!("{}", String::from_utf8_lossy(&output.stdout));
    Ok(())
}

fn run_codex_container_logs(
    name: &str,
    tail: &str,
    follow: bool,
    docker_bin: Option<PathBuf>,
) -> Result<()> {
    let artifacts = build_remove_artifacts(name)?;
    let docker_program =
        docker_bin.unwrap_or_else(|| si_rs_docker::docker_binary_path().to_path_buf());
    let command = docker_container_logs_command(
        docker_program.display().to_string(),
        artifacts.container_name,
        tail,
        follow,
    )?;
    let output = ProcessRunner.run(&command, &RunOptions::default())?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        anyhow::bail!("docker logs failed: {}", stderr.trim());
    }
    print!("{}", String::from_utf8_lossy(&output.stdout));
    Ok(())
}

fn run_codex_clone(
    name: &str,
    repo: &str,
    gh_pat: Option<&str>,
    docker_bin: Option<PathBuf>,
) -> Result<()> {
    let artifacts = build_remove_artifacts(name)?;
    let repo = repo.trim();
    if repo.is_empty() {
        anyhow::bail!("repo is required");
    }
    let docker_program =
        docker_bin.unwrap_or_else(|| si_rs_docker::docker_binary_path().to_path_buf());
    let mut spec = ContainerExecSpec::new(artifacts.container_name)
        .user("si")
        .env("SI_REPO", repo)
        .command(["/usr/local/bin/si-entrypoint", "bash", "-lc", "true"]);
    if let Some(gh_pat) = gh_pat.map(str::trim).filter(|value| !value.is_empty()) {
        spec = spec.env("SI_GH_PAT", gh_pat).env("GH_TOKEN", gh_pat).env("GITHUB_TOKEN", gh_pat);
    }
    let command = docker_container_exec_command(docker_program.display().to_string(), &spec)?;
    let output = ProcessRunner.run(&command, &RunOptions::default())?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        anyhow::bail!("docker exec failed: {}", stderr.trim());
    }
    print!("{}", String::from_utf8_lossy(&output.stdout));
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn run_codex_exec(
    name: &str,
    workdir: Option<PathBuf>,
    interactive: bool,
    tty: bool,
    env: Vec<String>,
    user: &str,
    docker_bin: Option<PathBuf>,
    command: Vec<String>,
) -> Result<()> {
    let artifacts = build_remove_artifacts(name)?;
    if command.is_empty() {
        anyhow::bail!("exec command is required");
    }
    let docker_program =
        docker_bin.unwrap_or_else(|| si_rs_docker::docker_binary_path().to_path_buf());
    let mut spec = ContainerExecSpec::new(artifacts.container_name)
        .user(user.trim())
        .interactive(interactive)
        .tty(tty)
        .command(command);
    if let Some(workdir) = workdir {
        spec = spec.workdir(workdir);
    }
    for item in env {
        let item = item.trim();
        if item.is_empty() {
            continue;
        }
        let (key, value) = item.split_once('=').unwrap_or((item, ""));
        spec = spec.env(key.trim(), value);
    }
    let command = docker_container_exec_command(docker_program.display().to_string(), &spec)?;
    let output = ProcessRunner.run(
        &command,
        &RunOptions {
            stdin: if interactive { StdinBehavior::Inherit } else { StdinBehavior::Null },
            ..RunOptions::default()
        },
    )?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        anyhow::bail!("docker exec failed: {}", stderr.trim());
    }
    print!("{}", String::from_utf8_lossy(&output.stdout));
    Ok(())
}

fn run_codex_list(format: OutputFormat, docker_bin: Option<PathBuf>) -> Result<()> {
    let docker_program =
        docker_bin.unwrap_or_else(|| si_rs_docker::docker_binary_path().to_path_buf());
    let command = docker_container_list_command(
        docker_program.display().to_string(),
        "si.component=codex",
        true,
    )?;
    let output = ProcessRunner.run(&command, &RunOptions::default())?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        anyhow::bail!("docker ps failed: {}", stderr.trim());
    }
    let stdout = String::from_utf8_lossy(&output.stdout);
    let mut items = Vec::new();
    for line in stdout.lines() {
        let line = line.trim();
        if line.is_empty() {
            continue;
        }
        let mut parts = line.splitn(3, '\t');
        let name = parts.next().unwrap_or("").trim();
        let state = parts.next().unwrap_or("").trim();
        let image = parts.next().unwrap_or("").trim();
        if name.is_empty() {
            continue;
        }
        items.push(CodexListEntryView {
            name: name.to_owned(),
            state: state.to_owned(),
            image: image.to_owned(),
        });
    }
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&items)?),
        OutputFormat::Text => {
            for item in items {
                println!("{}\t{}\t{}", item.name, item.state, item.image);
            }
        }
    }
    Ok(())
}

fn run_codex_status_read(
    name: &str,
    raw: bool,
    format: OutputFormat,
    docker_bin: Option<PathBuf>,
) -> Result<()> {
    let artifacts = build_remove_artifacts(name)?;
    let docker_program =
        docker_bin.unwrap_or_else(|| si_rs_docker::docker_binary_path().to_path_buf());
    let command = docker_container_exec_command(
        docker_program.display().to_string(),
        &ContainerExecSpec::new(artifacts.container_name)
            .user("si")
            .interactive(true)
            .env("HOME", "/home/si")
            .env("CODEX_HOME", "/home/si/.codex")
            .env("TERM", "xterm-256color")
            .command(["codex", "app-server"]),
    )?;
    let output = ProcessRunner.run(
        &command,
        &RunOptions {
            stdin: StdinBehavior::Bytes(build_app_server_input()),
            ..RunOptions::default()
        },
    )?;
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
        anyhow::bail!(if combined.is_empty() { "docker exec failed".to_owned() } else { combined });
    }
    let mut status = parse_app_server_status(&combined)?;
    if raw {
        status.raw = Some(combined);
    }
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&status)?),
        OutputFormat::Text => println!("{}", serde_json::to_string_pretty(&status)?),
    }
    Ok(())
}

fn run_codex_tmux_plan(
    name: &str,
    start_dir: Option<&str>,
    resume_session_id: Option<&str>,
    resume_profile: Option<&str>,
    format: OutputFormat,
) -> Result<()> {
    let artifacts = build_remove_artifacts(name)?;
    let plan = build_tmux_plan(
        &artifacts.container_name,
        start_dir.unwrap_or(""),
        resume_session_id.unwrap_or(""),
        resume_profile.unwrap_or(""),
    )?;
    let view = CodexTmuxPlanView {
        session_name: plan.session_name,
        target: plan.target,
        launch_command: plan.launch_command,
        resume_command: plan.resume_command,
    };
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            println!("session_name={}", view.session_name);
            println!("target={}", view.target);
            println!("launch_command={}", view.launch_command);
            if !view.resume_command.is_empty() {
                println!("resume_command={}", view.resume_command);
            }
        }
    }
    Ok(())
}

fn run_codex_tmux_command(container: &str, format: OutputFormat) -> Result<()> {
    let container = container.trim();
    let launch_command = build_tmux_command_for_container(container)?;
    let view = CodexTmuxCommandView { container: container.to_owned(), launch_command };
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            println!("container={}", view.container);
            println!("launch_command={}", view.launch_command);
        }
    }
    Ok(())
}

fn run_codex_report_parse(format: OutputFormat) -> Result<()> {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input)?;
    let payload: CodexReportParseInput = serde_json::from_str(&input)?;
    let parsed =
        parse_report_capture(&payload.clean, &payload.raw, payload.prompt_index, payload.ansi);
    let view = CodexReportParseView {
        segments: parsed
            .segments
            .into_iter()
            .map(|segment| CodexPromptSegmentView {
                prompt: segment.prompt,
                lines: segment.lines,
                raw: segment.raw,
            })
            .collect(),
        report: parsed.report,
    };
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            println!("report={}", view.report);
            for segment in view.segments {
                println!("prompt={}", segment.prompt);
                for line in segment.lines {
                    println!("line={line}");
                }
            }
        }
    }
    Ok(())
}

fn run_codex_respawn_plan(
    name: &str,
    profile_id: Option<String>,
    profile_containers: Vec<String>,
    format: OutputFormat,
) -> Result<()> {
    let plan = build_respawn_plan(&RespawnRequest {
        name: name.trim().to_owned(),
        profile_id,
        profile_container_names: profile_containers,
    })?;
    let view = CodexRespawnPlanView {
        effective_name: plan.effective_name,
        profile_id: plan.profile_id,
        remove_targets: plan.remove_targets,
    };
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&view)?),
        OutputFormat::Text => {
            println!("effective_name={}", view.effective_name);
            if let Some(profile_id) = view.profile_id {
                println!("profile_id={profile_id}");
            }
            for target in view.remove_targets {
                println!("remove_target={target}");
            }
        }
    }
    Ok(())
}

fn build_app_server_input() -> Vec<u8> {
    let requests = [
        serde_json::json!({
            "jsonrpc": "2.0",
            "id": 1,
            "method": "initialize",
            "params": {
                "clientInfo": {
                    "name": "si",
                    "version": si_rs_core::version::current_version(),
                }
            }
        }),
        serde_json::json!({"jsonrpc": "2.0", "id": 2, "method": "account/rateLimits/read", "params": null}),
        serde_json::json!({"jsonrpc": "2.0", "id": 3, "method": "account/read", "params": {}}),
        serde_json::json!({"jsonrpc": "2.0", "id": 4, "method": "config/read", "params": {}}),
    ];
    let mut payload = Vec::new();
    for request in requests {
        payload.extend(serde_json::to_vec(&request).expect("app server request json"));
        payload.push(b'\n');
    }
    payload
}

fn parse_app_server_status(raw: &str) -> Result<CodexStatusView> {
    let raw = raw.trim();
    if raw.is_empty() {
        anyhow::bail!("empty app-server output");
    }
    let mut rate_resp: Option<AppRateLimitsResponse> = None;
    let mut account_resp: Option<AppAccountResponse> = None;
    let mut config_resp: Option<AppConfigResponse> = None;
    let mut rate_err: Option<String> = None;

    for line in raw.lines() {
        let line = line.trim();
        if line.is_empty() {
            continue;
        }
        let Ok(envelope) = serde_json::from_str::<AppServerEnvelope>(line) else {
            continue;
        };
        let Some(id) = parse_app_server_id(&envelope.id) else {
            continue;
        };
        if let Some(error) = envelope.error {
            if id == 2 {
                let message = error.message.trim();
                rate_err = Some(if message.is_empty() {
                    "rate limits request failed".to_owned()
                } else {
                    message.to_owned()
                });
            }
            continue;
        }
        match id {
            2 => {
                if let Ok(parsed) = serde_json::from_value::<AppRateLimitsResponse>(envelope.result)
                {
                    rate_resp = Some(parsed);
                }
            }
            3 => {
                if let Ok(parsed) = serde_json::from_value::<AppAccountResponse>(envelope.result) {
                    account_resp = Some(parsed);
                }
            }
            4 => {
                if let Ok(parsed) = serde_json::from_value::<AppConfigResponse>(envelope.result) {
                    config_resp = Some(parsed);
                }
            }
            _ => {}
        }
    }
    if let Some(rate_err) = rate_err {
        anyhow::bail!(rate_err);
    }
    let Some(rate_resp) = rate_resp else {
        anyhow::bail!("rate limits missing");
    };
    let total_limit_min = std::env::var("CODEX_PLAN_LIMIT_MINUTES")
        .ok()
        .and_then(|value| value.trim().parse::<i64>().ok())
        .filter(|value| *value > 0)
        .unwrap_or(300);
    let now = chrono::Local::now();
    let (five_hour_left_pct, five_hour_remaining_minutes, five_hour_reset) = rate_resp
        .rate_limits
        .primary
        .as_ref()
        .map(|window| window_usage(window, total_limit_min, now))
        .unwrap_or((None, None, None));
    let (weekly_left_pct, weekly_remaining_minutes, weekly_reset) = rate_resp
        .rate_limits
        .secondary
        .as_ref()
        .map(|window| window_usage(window, 0, now))
        .unwrap_or((None, None, None));

    Ok(CodexStatusView {
        source: Some("app-server".to_owned()),
        raw: None,
        model: config_resp
            .as_ref()
            .and_then(|resp| resp.config.model.clone())
            .filter(|v| !v.trim().is_empty()),
        reasoning_effort: config_resp
            .as_ref()
            .and_then(|resp| resp.config.model_reasoning_effort.clone())
            .filter(|v| !v.trim().is_empty()),
        account_email: account_resp
            .as_ref()
            .and_then(|resp| resp.account.as_ref())
            .filter(|account| account.account_type.eq_ignore_ascii_case("chatgpt"))
            .map(|account| account.email.trim().to_owned())
            .filter(|v| !v.is_empty()),
        account_plan: account_resp
            .as_ref()
            .and_then(|resp| resp.account.as_ref())
            .filter(|account| account.account_type.eq_ignore_ascii_case("chatgpt"))
            .map(|account| account.plan_type.trim().to_owned())
            .filter(|v| !v.is_empty()),
        five_hour_left_pct,
        five_hour_reset,
        five_hour_remaining_minutes,
        weekly_left_pct,
        weekly_reset,
        weekly_remaining_minutes,
    })
}

fn parse_app_server_id(value: &serde_json::Value) -> Option<i64> {
    match value {
        serde_json::Value::Number(number) => number.as_i64(),
        serde_json::Value::String(value) => value.trim().parse::<i64>().ok(),
        _ => None,
    }
}

fn window_usage(
    window: &AppRateLimitWindow,
    fallback_minutes: i64,
    now: chrono::DateTime<chrono::Local>,
) -> (Option<f64>, Option<i32>, Option<String>) {
    let used = window.used_percent as f64;
    if !(0.0..=100.0).contains(&used) {
        return (None, None, None);
    }
    let remaining_pct = 100.0 - used;
    let window_minutes = window.window_duration_mins.unwrap_or(fallback_minutes);
    let remaining_minutes = window
        .resets_at
        .and_then(|timestamp| chrono::Local.timestamp_opt(timestamp, 0).single())
        .filter(|reset_at| *reset_at > now)
        .map(|reset_at| ((reset_at - now).num_seconds() as f64 / 60.0).ceil() as i32)
        .filter(|value| *value > 0)
        .or_else(|| {
            if window_minutes > 0 {
                Some(((window_minutes as f64) * remaining_pct / 100.0).round() as i32)
            } else {
                None
            }
        });
    let reset = window
        .resets_at
        .and_then(|timestamp| chrono::Local.timestamp_opt(timestamp, 0).single())
        .map(format_reset_at);
    (Some(remaining_pct), remaining_minutes, reset)
}

fn format_reset_at(time: chrono::DateTime<chrono::Local>) -> String {
    time.format("%b %-d, %Y %-I:%M %p").to_string()
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

fn format_provider_caps(caps: &ProviderCapabilitiesView) -> String {
    let mut value = String::new();
    if caps.supports_pagination {
        value.push('p');
    }
    if caps.supports_bulk {
        value.push('b');
    }
    if caps.supports_idempotency {
        value.push('i');
    }
    if caps.supports_raw {
        value.push('r');
    }
    value
}
