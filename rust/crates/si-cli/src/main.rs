use anyhow::Result;
use chrono::TimeZone;
use clap::{Parser, Subcommand, ValueEnum};
use serde::{Deserialize, Serialize};
use si_rs_codex::{
    RespawnRequest, SpawnContainerOptions, SpawnRequest, build_container_spec,
    build_remove_artifacts, build_respawn_plan, build_spawn_plan,
};
use si_rs_command_manifest::{
    CommandCategory, CommandSpec, find_root_command, visible_root_commands,
};
use si_rs_config::paths::SiPaths;
use si_rs_config::settings::Settings;
use si_rs_docker::{
    ContainerAction, ContainerExecSpec, docker_container_action_command,
    docker_container_exec_command, docker_container_list_command, docker_container_logs_command,
};
use si_rs_process::{ProcessRunner, RunOptions, StdinBehavior};
use si_rs_provider_catalog::{default_ids, find as find_provider, parse_id as parse_provider_id};
use si_rs_runtime::HostMountContext;
use std::fmt;
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
    Codex {
        #[command(subcommand)]
        command: Box<CodexCommand>,
    },
    Paths {
        #[command(subcommand)]
        command: PathsCommand,
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
            CodexCommand::RespawnPlan { name, profile_id, profile_containers, format } => {
                run_codex_respawn_plan(&name, profile_id, profile_containers, format)?
            }
        },
        Command::Paths { command } => match command {
            PathsCommand::Show { home, settings_file, format } => {
                show_paths(home, settings_file, format)?
            }
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
