use anyhow::Result;
use chrono::TimeZone;
use clap::{ArgAction, Parser, Subcommand, ValueEnum};
use serde::{Deserialize, Serialize};
use serde_json::Value;
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
    BootstrapView, PersistedRuntimeAgentState, PersistedSessionState, RefreshOutcome,
    RefreshSuccess, SessionState, apply_refresh_outcome_to_persisted_session_state,
    build_bootstrap_view, classify_persisted_session_state, clear_persisted_runtime_agent_state,
    clear_persisted_session_state, load_persisted_runtime_agent_state,
    load_persisted_session_state, save_persisted_runtime_agent_state, save_persisted_session_state,
    teardown_persisted_session_state,
};
use si_rs_process::{ProcessRunner, RunOptions, StdinBehavior};
use si_rs_provider_apple::{
    AppleAppStoreAuthOverrides, AppleAppStoreAuthStatus, AppleAppStoreContextListEntry,
    AppleAppStoreCurrentContext, list_appstore_contexts,
    render_appstore_context_list_text,
    resolve_auth_status as resolve_apple_appstore_auth_status,
    resolve_current_context as resolve_apple_appstore_current_context,
};
use si_rs_provider_aws::{
    AWSAuthOverrides, AWSAuthStatus, AWSContextListEntry, AWSCurrentContext,
    list_contexts as list_aws_contexts, render_context_list_text as render_aws_context_list_text,
    resolve_auth_status as resolve_aws_auth_status,
    resolve_current_context as resolve_aws_current_context,
};
use si_rs_provider_catalog::{default_ids, find as find_provider, parse_id as parse_provider_id};
use si_rs_provider_cloudflare::{
    CloudflareAuthRuntime, CloudflareAuthStatus, CloudflareAuthOverrides,
    CloudflareContextListEntry, CloudflareContextOverrides, CloudflareCurrentContext,
    list_contexts as list_cloudflare_contexts,
    render_context_list_text as render_cloudflare_context_list_text,
    resolve_auth_runtime as resolve_cloudflare_auth_runtime,
    resolve_current_context as resolve_cloudflare_current_context,
    verify_auth_status as verify_cloudflare_auth_status,
};
use si_rs_provider_gcp::{
    GCPAuthOverrides, GCPAuthStatus, GCPContextListEntry, GCPCurrentContext,
    list_contexts as list_gcp_contexts, render_context_list_text as render_gcp_context_list_text,
    resolve_auth_status as resolve_gcp_auth_status,
    resolve_current_context as resolve_gcp_current_context,
};
use si_rs_provider_github::{
    GitHubAPIResponse, GitHubAuthOverrides, GitHubAuthStatus, GitHubContextListEntry,
    get_branch as github_get_branch,
    get_issue as github_get_issue, get_project as github_get_project,
    get_pull_request as github_get_pull_request, get_release as github_get_release,
    get_repo as github_get_repo, list_branches as github_list_branches, list_contexts,
    list_issues as github_list_issues,
    list_projects as github_list_projects, list_pull_requests as github_list_pull_requests,
    list_releases as github_list_releases, list_repos as github_list_repos,
    list_workflow_runs as github_list_workflow_runs, list_workflows as github_list_workflows,
    graphql_query as github_graphql_query,
    raw_get as github_raw_get,
    resolve_access_token as github_resolve_access_token,
    render_context_list_text, resolve_auth_status, resolve_current_context,
    resolve_project_id as github_resolve_project_id, resolve_runtime as resolve_github_runtime,
    get_workflow_run as github_get_workflow_run,
};
use si_rs_provider_google::{
    GooglePlacesAuthStatus, GooglePlacesContextListEntry, GooglePlacesCurrentContext,
    GooglePlacesOverrides, list_places_contexts, render_places_context_list_text,
    resolve_places_auth_status, resolve_places_current_context,
};
use si_rs_provider_oci::{
    OCIAuthOverrides, OCIAuthStatus, OCIContextListEntry, OCIContextOverrides, OCICurrentContext,
    list_contexts as list_oci_contexts, render_context_list_text as render_oci_context_list_text,
    resolve_auth_status as resolve_oci_auth_status,
    resolve_current_context as resolve_oci_current_context,
};
use si_rs_provider_openai::{
    OpenAIAPIResponse, OpenAIAuthStatus, OpenAIContextListEntry, OpenAIContextOverrides,
    OpenAICurrentContext, OpenAIRuntime, get_admin_api_key as openai_get_admin_api_key,
    get_model as openai_get_model, get_project as openai_get_project,
    get_project_api_key as openai_get_project_api_key,
    get_project_service_account as openai_get_project_service_account,
    get_usage_metric as openai_get_usage_metric, list_admin_api_keys as openai_list_admin_api_keys,
    list_contexts as list_openai_contexts, list_models as openai_list_models,
    list_project_api_keys as openai_list_project_api_keys,
    list_project_rate_limits as openai_list_project_rate_limits,
    list_project_service_accounts as openai_list_project_service_accounts,
    list_projects as openai_list_projects,
    render_api_response_text as render_openai_api_response_text,
    render_auth_status_text as render_openai_auth_status_text,
    render_context_list_text as render_openai_context_list_text,
    resolve_current_context as resolve_openai_current_context,
    resolve_runtime as resolve_openai_runtime, verify_auth_status as verify_openai_auth_status,
};
use si_rs_provider_stripe::{
    StripeAuthOverrides, StripeAuthStatus, StripeContextListEntry, StripeCurrentContext,
    list_contexts as list_stripe_contexts,
    render_context_list_text as render_stripe_context_list_text,
    resolve_auth_status as resolve_stripe_auth_status,
    resolve_current_context as resolve_stripe_current_context,
};
use si_rs_provider_workos::{
    WorkOSAuthOverrides, WorkOSAuthStatus, WorkOSContextListEntry, WorkOSCurrentContext,
    list_contexts as list_workos_contexts,
    render_context_list_text as render_workos_context_list_text,
    resolve_auth_status as resolve_workos_auth_status,
    resolve_current_context as resolve_workos_current_context,
};
use si_rs_runtime::HostMountContext;
use si_rs_vault::TrustStore;
use si_rs_warmup::{
    WarmupState, classify_autostart_request, default_autostart_marker_path,
    default_disabled_marker_path, default_state_path as default_warmup_state_path,
    load_state as load_warmup_state, read_marker_state as read_warmup_marker_state,
    render_state_text as render_warmup_state_text, save_state as save_warmup_state,
    set_disabled_marker as set_rust_warmup_disabled_marker,
    write_autostart_marker as write_rust_warmup_autostart_marker,
};
use std::collections::BTreeMap;
use std::fmt;
use std::io::{self, Read};
use std::path::PathBuf;

#[derive(Debug, Parser)]
#[command(name = "si-rs", disable_version_flag = true, disable_help_subcommand = true)]
struct Cli {
    #[command(subcommand)]
    command: Command,
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
    Cloudflare {
        #[command(subcommand)]
        command: CloudflareCommand,
    },
    Apple {
        #[command(subcommand)]
        command: AppleCommand,
    },
    Aws {
        #[command(subcommand)]
        command: AWSCommand,
    },
    #[command(name = "gcp")]
    Gcp {
        #[command(subcommand)]
        command: GCPCommand,
    },
    Google {
        #[command(subcommand)]
        command: GoogleCommand,
    },
    #[command(name = "openai")]
    OpenAI {
        #[command(subcommand)]
        command: OpenAICommand,
    },
    #[command(name = "oci")]
    Oci {
        #[command(subcommand)]
        command: OciCommand,
    },
    Stripe {
        #[command(subcommand)]
        command: StripeCommand,
    },
    #[command(name = "workos")]
    WorkOS {
        #[command(subcommand)]
        command: WorkOSCommand,
    },
    #[command(name = "github")]
    GitHub {
        #[command(subcommand)]
        command: GitHubCommand,
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
    Warmup {
        #[command(subcommand)]
        command: WarmupCommand,
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

#[derive(Debug, Subcommand)]
enum CloudflareCommand {
    Auth {
        #[command(subcommand)]
        command: CloudflareAuthCommand,
    },
    Context {
        #[command(subcommand)]
        command: CloudflareContextCommand,
    },
}

#[derive(Debug, Subcommand)]
enum CloudflareAuthCommand {
    Status {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        env: Option<String>,
        #[arg(long)]
        zone_id: Option<String>,
        #[arg(long)]
        zone: Option<String>,
        #[arg(long)]
        api_token: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        account_id: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum CloudflareContextCommand {
    List {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Current {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        env: Option<String>,
        #[arg(long)]
        zone_id: Option<String>,
        #[arg(long)]
        zone: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        account_id: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum AppleCommand {
    #[command(name = "appstore")]
    AppStore {
        #[command(subcommand)]
        command: AppleAppStoreCommand,
    },
}

#[derive(Debug, Subcommand)]
enum AppleAppStoreCommand {
    Auth {
        #[command(subcommand)]
        command: AppleAppStoreAuthCommand,
    },
    Context {
        #[command(subcommand)]
        command: AppleAppStoreContextCommand,
    },
}

#[derive(Debug, Subcommand)]
enum AppleAppStoreAuthCommand {
    Status {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        env: Option<String>,
        #[arg(long)]
        bundle_id: Option<String>,
        #[arg(long)]
        locale: Option<String>,
        #[arg(long)]
        platform: Option<String>,
        #[arg(long)]
        issuer_id: Option<String>,
        #[arg(long)]
        key_id: Option<String>,
        #[arg(long)]
        private_key: Option<String>,
        #[arg(long)]
        private_key_file: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long, default_value_t = true, action = ArgAction::Set)]
        verify: bool,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum AppleAppStoreContextCommand {
    List {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Current {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum AWSCommand {
    Auth {
        #[command(subcommand)]
        command: AWSAuthCommand,
    },
    Context {
        #[command(subcommand)]
        command: AWSContextCommand,
    },
}

#[derive(Debug, Subcommand)]
enum AWSAuthCommand {
    Status {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        region: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        access_key: Option<String>,
        #[arg(long)]
        secret_key: Option<String>,
        #[arg(long)]
        session_token: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum AWSContextCommand {
    List {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Current {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum GCPCommand {
    Auth {
        #[command(subcommand)]
        command: GCPAuthCommand,
    },
    Context {
        #[command(subcommand)]
        command: GCPContextCommand,
    },
}

#[derive(Debug, Subcommand)]
enum GCPAuthCommand {
    Status {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        env: Option<String>,
        #[arg(long)]
        project: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        access_token: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum GCPContextCommand {
    List {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Current {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum GoogleCommand {
    Places {
        #[command(subcommand)]
        command: GooglePlacesCommand,
    },
}

#[derive(Debug, Subcommand)]
enum GooglePlacesCommand {
    Auth {
        #[command(subcommand)]
        command: GooglePlacesAuthCommand,
    },
    Context {
        #[command(subcommand)]
        command: GooglePlacesContextCommand,
    },
}

#[derive(Debug, Subcommand)]
enum GooglePlacesAuthCommand {
    Status {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        env: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        language: Option<String>,
        #[arg(long)]
        region: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum GooglePlacesContextCommand {
    List {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Current {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        env: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        language: Option<String>,
        #[arg(long)]
        region: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[allow(clippy::large_enum_variant)]
#[derive(Debug, Subcommand)]
enum OpenAICommand {
    Auth {
        #[command(subcommand)]
        command: OpenAIAuthCommand,
    },
    Context {
        #[command(subcommand)]
        command: OpenAIContextCommand,
    },
    Model {
        #[command(subcommand)]
        command: OpenAIModelCommand,
    },
    Usage {
        metric: OpenAIUsageMetric,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        admin_api_key: Option<String>,
        #[arg(long)]
        org_id: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        start_time: Option<i64>,
        #[arg(long)]
        end_time: Option<i64>,
        #[arg(long)]
        bucket_width: Option<String>,
        #[arg(long)]
        limit: Option<usize>,
        #[arg(long)]
        page: Option<String>,
        #[arg(long)]
        batch: bool,
        #[arg(long = "project")]
        project_ids: Vec<String>,
        #[arg(long = "user-id")]
        user_ids: Vec<String>,
        #[arg(long = "api-key-id")]
        api_key_ids: Vec<String>,
        #[arg(long = "model")]
        models: Vec<String>,
        #[arg(long = "group-by")]
        group_by: Vec<String>,
        #[arg(long = "param")]
        extra_params: Vec<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
    Monitor {
        #[command(subcommand)]
        command: OpenAIMonitorCommand,
    },
    Codex {
        #[command(subcommand)]
        command: OpenAICodexCommand,
    },
    Key {
        #[command(subcommand)]
        command: OpenAIKeyCommand,
    },
    Project {
        #[command(subcommand)]
        command: OpenAIProjectCommand,
    },
}

#[derive(Debug, Subcommand)]
enum OpenAIAuthCommand {
    Status {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        admin_api_key: Option<String>,
        #[arg(long)]
        org_id: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum OpenAIContextCommand {
    List {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Current {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        admin_api_key: Option<String>,
        #[arg(long)]
        org_id: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum OpenAIModelCommand {
    List {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        admin_api_key: Option<String>,
        #[arg(long)]
        org_id: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        limit: Option<usize>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
    Get {
        model_id: Option<String>,
        #[arg(long)]
        id: Option<String>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        admin_api_key: Option<String>,
        #[arg(long)]
        org_id: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, ValueEnum)]
enum OpenAIUsageMetric {
    #[value(name = "completions")]
    Completions,
    #[value(name = "embeddings")]
    Embeddings,
    #[value(name = "images")]
    Images,
    #[value(name = "audio_speeches", alias = "audio-speeches", alias = "speeches")]
    AudioSpeeches,
    #[value(
        name = "audio_transcriptions",
        alias = "audio-transcriptions",
        alias = "transcriptions"
    )]
    AudioTranscriptions,
    #[value(name = "moderations")]
    Moderations,
    #[value(name = "vector_stores", alias = "vector-stores", alias = "vector-store")]
    VectorStores,
    #[value(
        name = "code_interpreter_sessions",
        alias = "code-interpreter-sessions",
        alias = "code-interpreter"
    )]
    CodeInterpreterSessions,
    #[value(name = "costs")]
    Costs,
}

#[derive(Debug, Subcommand)]
enum OpenAIMonitorCommand {
    Usage {
        metric: Option<OpenAIUsageMetric>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        admin_api_key: Option<String>,
        #[arg(long)]
        org_id: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        start_time: Option<i64>,
        #[arg(long)]
        end_time: Option<i64>,
        #[arg(long)]
        bucket_width: Option<String>,
        #[arg(long)]
        limit: Option<usize>,
        #[arg(long)]
        page: Option<String>,
        #[arg(long)]
        batch: bool,
        #[arg(long = "project")]
        project_ids: Vec<String>,
        #[arg(long = "user-id")]
        user_ids: Vec<String>,
        #[arg(long = "api-key-id")]
        api_key_ids: Vec<String>,
        #[arg(long = "model")]
        models: Vec<String>,
        #[arg(long = "group-by")]
        group_by: Vec<String>,
        #[arg(long = "param")]
        extra_params: Vec<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
    #[command(alias = "rate-limits")]
    Limits {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        admin_api_key: Option<String>,
        #[arg(long)]
        org_id: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        limit: Option<usize>,
        #[arg(long)]
        after: Option<String>,
        #[arg(long)]
        before: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
}

#[derive(Debug, Subcommand)]
enum OpenAICodexCommand {
    Usage {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        admin_api_key: Option<String>,
        #[arg(long)]
        org_id: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        start_time: Option<i64>,
        #[arg(long)]
        end_time: Option<i64>,
        #[arg(long)]
        bucket_width: Option<String>,
        #[arg(long)]
        limit: Option<usize>,
        #[arg(long = "model")]
        models: Vec<String>,
        #[arg(long = "group-by")]
        group_by: Vec<String>,
        #[arg(long = "project")]
        project_ids: Vec<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
}

#[derive(Debug, Subcommand)]
enum OpenAIKeyCommand {
    List {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        admin_api_key: Option<String>,
        #[arg(long)]
        org_id: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        limit: Option<usize>,
        #[arg(long)]
        after: Option<String>,
        #[arg(long)]
        order: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
    Get {
        key_ref: Option<String>,
        #[arg(long)]
        key_id: Option<String>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        admin_api_key: Option<String>,
        #[arg(long)]
        org_id: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
}

#[derive(Debug, Subcommand)]
enum OpenAIProjectCommand {
    List {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        admin_api_key: Option<String>,
        #[arg(long)]
        org_id: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        limit: Option<usize>,
        #[arg(long)]
        after: Option<String>,
        #[arg(long)]
        include_archived: bool,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
    Get {
        project_ref: Option<String>,
        #[arg(long)]
        id: Option<String>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        admin_api_key: Option<String>,
        #[arg(long)]
        org_id: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
    ApiKey {
        #[command(subcommand)]
        command: OpenAIProjectAPIKeyCommand,
    },
    ServiceAccount {
        #[command(subcommand)]
        command: OpenAIProjectServiceAccountCommand,
    },
    RateLimit {
        #[command(subcommand)]
        command: OpenAIProjectRateLimitCommand,
    },
}

#[derive(Debug, Subcommand)]
enum OpenAIProjectAPIKeyCommand {
    List {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        admin_api_key: Option<String>,
        #[arg(long)]
        org_id: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        limit: Option<usize>,
        #[arg(long)]
        after: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
    Get {
        key_ref: Option<String>,
        #[arg(long)]
        key_id: Option<String>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        admin_api_key: Option<String>,
        #[arg(long)]
        org_id: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
}

#[derive(Debug, Subcommand)]
enum OpenAIProjectServiceAccountCommand {
    List {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        admin_api_key: Option<String>,
        #[arg(long)]
        org_id: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        limit: Option<usize>,
        #[arg(long)]
        after: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
    Get {
        service_account_ref: Option<String>,
        #[arg(long)]
        service_account_id: Option<String>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        admin_api_key: Option<String>,
        #[arg(long)]
        org_id: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
}

#[derive(Debug, Subcommand)]
enum OpenAIProjectRateLimitCommand {
    List {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        admin_api_key: Option<String>,
        #[arg(long)]
        org_id: Option<String>,
        #[arg(long)]
        project_id: Option<String>,
        #[arg(long)]
        limit: Option<usize>,
        #[arg(long)]
        after: Option<String>,
        #[arg(long)]
        before: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
}

#[derive(Debug, Subcommand)]
enum OciCommand {
    Auth {
        #[command(subcommand)]
        command: OciAuthCommand,
    },
    Context {
        #[command(subcommand)]
        command: OciContextCommand,
    },
    Oracular {
        #[command(subcommand)]
        command: OciOracularCommand,
    },
}

#[derive(Debug, Subcommand)]
enum OciAuthCommand {
    Status {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        profile: Option<String>,
        #[arg(long)]
        config_file: Option<String>,
        #[arg(long)]
        region: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth: Option<String>,
        #[arg(long, default_value_t = true, action = ArgAction::Set)]
        verify: bool,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum OciOracularCommand {
    Tenancy {
        #[arg(long)]
        profile: Option<String>,
        #[arg(long)]
        config_file: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum OciContextCommand {
    List {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Current {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        profile: Option<String>,
        #[arg(long)]
        config_file: Option<String>,
        #[arg(long)]
        region: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum StripeCommand {
    Auth {
        #[command(subcommand)]
        command: StripeAuthCommand,
    },
    Context {
        #[command(subcommand)]
        command: StripeContextCommand,
    },
}

#[derive(Debug, Subcommand)]
enum StripeAuthCommand {
    Status {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        env: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum StripeContextCommand {
    List {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Current {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum WorkOSCommand {
    Auth {
        #[command(subcommand)]
        command: WorkOSAuthCommand,
    },
    Context {
        #[command(subcommand)]
        command: WorkOSContextCommand,
    },
}

#[derive(Debug, Subcommand)]
enum WorkOSAuthCommand {
    Status {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        env: Option<String>,
        #[arg(long)]
        api_key: Option<String>,
        #[arg(long)]
        client_id: Option<String>,
        #[arg(long)]
        org_id: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum WorkOSContextCommand {
    List {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Current {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum GitHubCommand {
    Auth {
        #[command(subcommand)]
        command: GitHubAuthCommand,
    },
    Context {
        #[command(subcommand)]
        command: GitHubContextCommand,
    },
    Branch {
        #[command(subcommand)]
        command: GitHubBranchCommand,
    },
    Git {
        #[command(subcommand)]
        command: GitHubGitCommand,
    },
    Raw {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long, default_value = "GET")]
        method: String,
        #[arg(long)]
        path: Option<String>,
        #[arg(long = "param")]
        params: Vec<String>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long = "settings-file")]
        settings_file: Option<PathBuf>,
    },
    Graphql {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long)]
        query: Option<String>,
        #[arg(long = "var")]
        vars: Vec<String>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long = "settings-file")]
        settings_file: Option<PathBuf>,
    },
    Project {
        #[command(subcommand)]
        command: GitHubProjectCommand,
    },
    Issue {
        #[command(subcommand)]
        command: GitHubIssueCommand,
    },
    #[command(name = "pr")]
    PullRequest {
        #[command(subcommand)]
        command: GitHubPullRequestCommand,
    },
    Workflow {
        #[command(subcommand)]
        command: GitHubWorkflowCommand,
    },
    Repo {
        #[command(subcommand)]
        command: GitHubRepoCommand,
    },
    Release {
        #[command(subcommand)]
        command: GitHubReleaseCommand,
    },
}

#[derive(Debug, Subcommand)]
enum GitHubBranchCommand {
    List {
        repo_ref: Option<String>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long)]
        protected: Option<String>,
        #[arg(long, default_value_t = 10)]
        max_pages: usize,
        #[arg(long = "param")]
        params: Vec<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
    Get {
        repo_ref: Option<String>,
        branch: Option<String>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long = "param")]
        params: Vec<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
}

#[derive(Debug, Subcommand)]
enum GitHubGitCommand {
    Credential {
        #[command(subcommand)]
        command: GitHubGitCredentialCommand,
    },
}

#[derive(Debug, Subcommand)]
enum GitHubGitCredentialCommand {
    Get {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
    },
    Store,
    Erase,
}

#[derive(Debug, Subcommand)]
enum GitHubAuthCommand {
    Status {
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum GitHubContextCommand {
    List {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Current {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
}

#[derive(Debug, Subcommand)]
enum GitHubReleaseCommand {
    List {
        repo_ref: Option<String>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long, default_value_t = 5)]
        max_pages: usize,
        #[arg(long = "param")]
        params: Vec<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
    Get {
        repo_ref: Option<String>,
        release_ref: Option<String>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
}

#[derive(Debug, Subcommand)]
enum GitHubRepoCommand {
    List {
        owner_ref: Option<String>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long, default_value_t = 10)]
        max_pages: usize,
        #[arg(long = "param")]
        params: Vec<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
    Get {
        repo_ref: Option<String>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
}

#[derive(Debug, Subcommand)]
enum GitHubProjectCommand {
    List {
        organization_ref: Option<String>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long, default_value_t = 30)]
        limit: usize,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
    Get {
        project_ref: Option<String>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
}

#[derive(Debug, Subcommand)]
enum GitHubWorkflowCommand {
    List {
        repo_ref: Option<String>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
    Runs {
        repo_ref: Option<String>,
        #[arg(long)]
        workflow: Option<String>,
        #[arg(long = "param")]
        params: Vec<String>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
    Run {
        #[command(subcommand)]
        command: GitHubWorkflowRunCommand,
    },
}

#[derive(Debug, Subcommand)]
enum GitHubWorkflowRunCommand {
    Get {
        repo_ref: Option<String>,
        run_id: Option<i64>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
}

#[derive(Debug, Subcommand)]
enum GitHubIssueCommand {
    List {
        repo_ref: Option<String>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long, default_value_t = 5)]
        max_pages: usize,
        #[arg(long = "param")]
        params: Vec<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
    Get {
        repo_ref: Option<String>,
        number: Option<i64>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
}

#[derive(Debug, Subcommand)]
enum GitHubPullRequestCommand {
    List {
        repo_ref: Option<String>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long, default_value_t = 5)]
        max_pages: usize,
        #[arg(long = "param")]
        params: Vec<String>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
    },
    Get {
        repo_ref: Option<String>,
        number: Option<i64>,
        #[arg(long)]
        account: Option<String>,
        #[arg(long)]
        owner: Option<String>,
        #[arg(long)]
        base_url: Option<String>,
        #[arg(long)]
        auth_mode: Option<String>,
        #[arg(long)]
        token: Option<String>,
        #[arg(long)]
        app_id: Option<i64>,
        #[arg(long)]
        app_key: Option<String>,
        #[arg(long)]
        installation_id: Option<i64>,
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long)]
        json: bool,
        #[arg(long)]
        raw: bool,
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
        #[arg(long, default_value = "text")]
        format: OutputFormat,
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
        #[arg(long, default_value = "text")]
        format: OutputFormat,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    Start {
        name: String,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
        #[arg(long)]
        docker_bin: Option<PathBuf>,
    },
    Stop {
        name: String,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
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
        #[arg(long, default_value = "text")]
        format: OutputFormat,
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
    Clear {
        #[arg(long)]
        path: PathBuf,
    },
    BootstrapView {
        #[arg(long)]
        path: PathBuf,
        #[arg(long)]
        profile_id: Option<String>,
        #[arg(long)]
        access_token_path: String,
        #[arg(long)]
        refresh_token_path: String,
        #[arg(long)]
        access_token_container_path: String,
        #[arg(long)]
        refresh_token_container_path: String,
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
    Clear {
        #[arg(long)]
        path: PathBuf,
    },
}

#[derive(Debug, Subcommand)]
enum WarmupCommand {
    AutostartDecision {
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
    WriteAutostart {
        #[arg(long)]
        path: PathBuf,
    },
    SetDisabled {
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
struct CloudflareContextListPayload {
    contexts: Vec<CloudflareContextListEntry>,
}

#[derive(Debug, Serialize)]
struct CloudflareCurrentContextPayload {
    account_alias: String,
    account_id: String,
    environment: String,
    zone_id: String,
    zone_name: String,
    base_url: String,
    source: String,
}

impl From<CloudflareCurrentContext> for CloudflareCurrentContextPayload {
    fn from(value: CloudflareCurrentContext) -> Self {
        Self {
            account_alias: value.account_alias,
            account_id: value.account_id,
            environment: value.environment,
            zone_id: value.zone_id,
            zone_name: value.zone_name,
            base_url: value.base_url,
            source: value.source,
        }
    }
}

#[derive(Debug, Serialize)]
struct CloudflareAuthStatusPayload {
    status: String,
    account_alias: String,
    account_id: String,
    environment: String,
    zone_id: String,
    zone_name: String,
    source: String,
    token_preview: String,
    base_url: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    verify: Option<serde_json::Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    verify_error: Option<String>,
}

impl From<&CloudflareAuthRuntime> for CloudflareAuthStatusPayload {
    fn from(value: &CloudflareAuthRuntime) -> Self {
        let status = CloudflareAuthStatus::from(value);
        Self {
            status: status.status,
            account_alias: status.account_alias,
            account_id: status.account_id,
            environment: status.environment,
            zone_id: status.zone_id,
            zone_name: status.zone_name,
            source: status.source,
            token_preview: status.token_preview,
            base_url: status.base_url,
            verify: None,
            verify_error: None,
        }
    }
}

#[derive(Debug, Serialize)]
struct AppleAppStoreContextListPayload {
    contexts: Vec<AppleAppStoreContextListEntry>,
}

#[derive(Debug, Serialize)]
struct AppleAppStoreCurrentContextPayload {
    account_alias: String,
    project_id: String,
    environment: String,
    source: String,
    token_source: String,
    bundle_id: String,
    locale: String,
    platform: String,
    base_url: String,
}

impl From<AppleAppStoreCurrentContext> for AppleAppStoreCurrentContextPayload {
    fn from(value: AppleAppStoreCurrentContext) -> Self {
        Self {
            account_alias: value.account_alias,
            project_id: value.project_id,
            environment: value.environment,
            source: value.source,
            token_source: value.token_source,
            bundle_id: value.bundle_id,
            locale: value.locale,
            platform: value.platform,
            base_url: value.base_url,
        }
    }
}

#[derive(Debug, Serialize)]
struct AppleAppStoreAuthStatusPayload {
    status: String,
    account_alias: String,
    project_id: String,
    environment: String,
    source: String,
    token_source: String,
    bundle_id: String,
    locale: String,
    platform: String,
    base_url: String,
}

impl From<AppleAppStoreAuthStatus> for AppleAppStoreAuthStatusPayload {
    fn from(value: AppleAppStoreAuthStatus) -> Self {
        Self {
            status: value.status,
            account_alias: value.account_alias,
            project_id: value.project_id,
            environment: value.environment,
            source: value.source,
            token_source: value.token_source,
            bundle_id: value.bundle_id,
            locale: value.locale,
            platform: value.platform,
            base_url: value.base_url,
        }
    }
}

#[derive(Debug, Serialize)]
struct AWSContextListPayload {
    contexts: Vec<AWSContextListEntry>,
}

#[derive(Debug, Serialize)]
struct AWSCurrentContextPayload {
    account_alias: String,
    region: String,
    base_url: String,
    source: String,
    access_key: String,
}

impl From<AWSCurrentContext> for AWSCurrentContextPayload {
    fn from(value: AWSCurrentContext) -> Self {
        Self {
            account_alias: value.account_alias,
            region: value.region,
            base_url: value.base_url,
            source: value.source,
            access_key: value.access_key,
        }
    }
}

#[derive(Debug, Serialize)]
struct AWSAuthStatusPayload {
    account_alias: String,
    region: String,
    base_url: String,
    source: String,
    access_key: String,
}

impl From<AWSAuthStatus> for AWSAuthStatusPayload {
    fn from(value: AWSAuthStatus) -> Self {
        Self {
            account_alias: value.account_alias,
            region: value.region,
            base_url: value.base_url,
            source: value.source,
            access_key: value.access_key,
        }
    }
}

#[derive(Debug, Serialize)]
struct GCPContextListPayload {
    contexts: Vec<GCPContextListEntry>,
}

#[derive(Debug, Serialize)]
struct GCPCurrentContextPayload {
    account_alias: String,
    environment: String,
    project_id: String,
    base_url: String,
    source: String,
}

impl From<GCPCurrentContext> for GCPCurrentContextPayload {
    fn from(value: GCPCurrentContext) -> Self {
        Self {
            account_alias: value.account_alias,
            environment: value.environment,
            project_id: value.project_id,
            base_url: value.base_url,
            source: value.source,
        }
    }
}

#[derive(Debug, Serialize)]
struct GCPAuthStatusPayload {
    account_alias: String,
    environment: String,
    project_id: String,
    base_url: String,
    source: String,
    token_preview: String,
}

impl From<GCPAuthStatus> for GCPAuthStatusPayload {
    fn from(value: GCPAuthStatus) -> Self {
        Self {
            account_alias: value.account_alias,
            environment: value.environment,
            project_id: value.project_id,
            base_url: value.base_url,
            source: value.source,
            token_preview: value.token_preview,
        }
    }
}

#[derive(Debug, Serialize)]
struct GooglePlacesContextListPayload {
    contexts: Vec<GooglePlacesContextListEntry>,
}

#[derive(Debug, Serialize)]
struct GooglePlacesCurrentContextPayload {
    account_alias: String,
    project_id: String,
    environment: String,
    language_code: String,
    region_code: String,
    source: String,
    base_url: String,
}

impl From<GooglePlacesCurrentContext> for GooglePlacesCurrentContextPayload {
    fn from(value: GooglePlacesCurrentContext) -> Self {
        Self {
            account_alias: value.account_alias,
            project_id: value.project_id,
            environment: value.environment,
            language_code: value.language_code,
            region_code: value.region_code,
            source: value.source,
            base_url: value.base_url,
        }
    }
}

#[derive(Debug, Serialize)]
struct GooglePlacesAuthStatusPayload {
    account_alias: String,
    project_id: String,
    environment: String,
    language_code: String,
    region_code: String,
    source: String,
    key_preview: String,
    base_url: String,
}

impl From<GooglePlacesAuthStatus> for GooglePlacesAuthStatusPayload {
    fn from(value: GooglePlacesAuthStatus) -> Self {
        Self {
            account_alias: value.account_alias,
            project_id: value.project_id,
            environment: value.environment,
            language_code: value.language_code,
            region_code: value.region_code,
            source: value.source,
            key_preview: value.key_preview,
            base_url: value.base_url,
        }
    }
}

#[derive(Debug, Serialize)]
struct OpenAIContextListPayload {
    contexts: Vec<OpenAIContextListEntry>,
}

#[derive(Debug, Serialize)]
struct OpenAICurrentContextPayload {
    account_alias: String,
    base_url: String,
    organization_id: String,
    project_id: String,
    source: String,
    admin_key_set: bool,
}

impl From<OpenAICurrentContext> for OpenAICurrentContextPayload {
    fn from(value: OpenAICurrentContext) -> Self {
        Self {
            account_alias: value.account_alias,
            base_url: value.base_url,
            organization_id: value.organization_id,
            project_id: value.project_id,
            source: value.source,
            admin_key_set: value.admin_key_set,
        }
    }
}

#[derive(Debug, Serialize)]
struct OCIContextListPayload {
    contexts: Vec<OCIContextListEntry>,
}

#[derive(Debug, Serialize)]
struct OCICurrentContextPayload {
    account_alias: String,
    profile: String,
    config_file: String,
    region: String,
    base_url: String,
    auth_style: String,
    source: String,
    tenancy_ocid: String,
}

impl From<OCICurrentContext> for OCICurrentContextPayload {
    fn from(value: OCICurrentContext) -> Self {
        Self {
            account_alias: value.account_alias,
            profile: value.profile,
            config_file: value.config_file,
            region: value.region,
            base_url: value.base_url,
            auth_style: value.auth_style,
            source: value.source,
            tenancy_ocid: value.tenancy_ocid,
        }
    }
}

#[derive(Debug, Serialize)]
struct OCIAuthStatusPayload {
    status: String,
    account_alias: String,
    profile: String,
    config_file: String,
    region: String,
    base_url: String,
    auth_style: String,
    tenancy_ocid: String,
    user_ocid: String,
    fingerprint: String,
    source: String,
}

impl From<OCIAuthStatus> for OCIAuthStatusPayload {
    fn from(value: OCIAuthStatus) -> Self {
        Self {
            status: value.status,
            account_alias: value.account_alias,
            profile: value.profile,
            config_file: value.config_file,
            region: value.region,
            base_url: value.base_url,
            auth_style: value.auth_style,
            tenancy_ocid: value.tenancy_ocid,
            user_ocid: value.user_ocid,
            fingerprint: value.fingerprint,
            source: value.source,
        }
    }
}

#[derive(Debug, Serialize)]
struct OCIOracularTenancyPayload {
    profile: String,
    config_file: String,
    tenancy_ocid: String,
}

#[derive(Debug, Serialize)]
struct StripeContextListPayload {
    contexts: Vec<StripeContextListEntry>,
}

#[derive(Debug, Serialize)]
struct StripeCurrentContextPayload {
    account_alias: String,
    account_id: String,
    environment: String,
    key_source: String,
}

impl From<StripeCurrentContext> for StripeCurrentContextPayload {
    fn from(value: StripeCurrentContext) -> Self {
        Self {
            account_alias: value.account_alias,
            account_id: value.account_id,
            environment: value.environment,
            key_source: value.key_source,
        }
    }
}

#[derive(Debug, Serialize)]
struct StripeAuthStatusPayload {
    account_alias: String,
    account_id: String,
    environment: String,
    key_source: String,
    key_preview: String,
}

impl From<StripeAuthStatus> for StripeAuthStatusPayload {
    fn from(value: StripeAuthStatus) -> Self {
        Self {
            account_alias: value.account_alias,
            account_id: value.account_id,
            environment: value.environment,
            key_source: value.key_source,
            key_preview: value.key_preview,
        }
    }
}

#[derive(Debug, Serialize)]
struct WorkOSContextListPayload {
    contexts: Vec<WorkOSContextListEntry>,
}

#[derive(Debug, Serialize)]
struct WorkOSCurrentContextPayload {
    account_alias: String,
    environment: String,
    base_url: String,
    organization_id: String,
    client_id: String,
    source: String,
}

impl From<WorkOSCurrentContext> for WorkOSCurrentContextPayload {
    fn from(value: WorkOSCurrentContext) -> Self {
        Self {
            account_alias: value.account_alias,
            environment: value.environment,
            base_url: value.base_url,
            organization_id: value.organization_id,
            client_id: value.client_id,
            source: value.source,
        }
    }
}

#[derive(Debug, Serialize)]
struct WorkOSAuthStatusPayload {
    account_alias: String,
    environment: String,
    organization_id: String,
    client_id: String,
    source: String,
    base_url: String,
    key_preview: String,
}

impl From<WorkOSAuthStatus> for WorkOSAuthStatusPayload {
    fn from(value: WorkOSAuthStatus) -> Self {
        Self {
            account_alias: value.account_alias,
            environment: value.environment,
            organization_id: value.organization_id,
            client_id: value.client_id,
            source: value.source,
            base_url: value.base_url,
            key_preview: value.key_preview,
        }
    }
}

#[derive(Debug, Serialize)]
struct GitHubContextListPayload {
    contexts: Vec<GitHubContextListEntry>,
}

#[derive(Debug, Serialize)]
struct GitHubCurrentContextPayload {
    account_alias: String,
    owner: String,
    auth_mode: String,
    base_url: String,
    source: String,
}

#[derive(Debug, Serialize)]
struct GitHubAuthStatusPayload {
    account_alias: String,
    owner: String,
    auth_mode: String,
    base_url: String,
    source: String,
    token_preview: String,
}

impl From<GitHubAuthStatus> for GitHubAuthStatusPayload {
    fn from(value: GitHubAuthStatus) -> Self {
        Self {
            account_alias: value.account_alias,
            owner: value.owner,
            auth_mode: value.auth_mode,
            base_url: value.base_url,
            source: value.source,
            token_preview: value.token_preview,
        }
    }
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
struct CodexRemoveResultView {
    name: String,
    container_name: String,
    profile_id: Option<String>,
    codex_volume: Option<String>,
    gh_volume: Option<String>,
    output: String,
}

#[derive(Debug, Serialize)]
struct CodexContainerActionView {
    action: String,
    name: String,
    container_name: String,
    output: String,
}

#[derive(Debug, Serialize)]
struct CodexCloneResultView {
    name: String,
    repo: String,
    container_name: String,
    output: String,
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
        Command::Cloudflare { command } => match command {
            CloudflareCommand::Auth { command } => match command {
                CloudflareAuthCommand::Status {
                    account,
                    env,
                    zone_id,
                    zone,
                    api_token,
                    base_url,
                    account_id,
                    home,
                    settings_file,
                    json,
                    format,
                } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_cloudflare_auth_status(
                        account,
                        env,
                        zone_id,
                        zone,
                        api_token,
                        base_url,
                        account_id,
                        home,
                        settings_file,
                        format,
                    )?
                }
            },
            CloudflareCommand::Context { command } => match command {
                CloudflareContextCommand::List { home, settings_file, json, format } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_cloudflare_context_list(home, settings_file, format)?
                }
                CloudflareContextCommand::Current {
                    account,
                    env,
                    zone_id,
                    zone,
                    base_url,
                    account_id,
                    home,
                    settings_file,
                    json,
                    format,
                } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_cloudflare_context_current(
                        account,
                        env,
                        zone_id,
                        zone,
                        base_url,
                        account_id,
                        home,
                        settings_file,
                        format,
                    )?
                }
            },
        },
        Command::Apple { command } => match command {
            AppleCommand::AppStore { command } => match command {
                AppleAppStoreCommand::Auth { command } => match command {
                    AppleAppStoreAuthCommand::Status {
                        account,
                        env,
                        bundle_id,
                        locale,
                        platform,
                        issuer_id,
                        key_id,
                        private_key,
                        private_key_file,
                        project_id,
                        base_url,
                        verify,
                        home,
                        settings_file,
                        json,
                        format,
                    } => {
                        let format = if json { OutputFormat::Json } else { format };
                        show_apple_appstore_auth_status(
                            account,
                            env,
                            bundle_id,
                            locale,
                            platform,
                            issuer_id,
                            key_id,
                            private_key,
                            private_key_file,
                            project_id,
                            base_url,
                            verify,
                            home,
                            settings_file,
                            format,
                        )?
                    }
                },
                AppleAppStoreCommand::Context { command } => match command {
                    AppleAppStoreContextCommand::List { home, settings_file, json, format } => {
                        let format = if json { OutputFormat::Json } else { format };
                        show_apple_appstore_context_list(home, settings_file, format)?
                    }
                    AppleAppStoreContextCommand::Current { home, settings_file, json, format } => {
                        let format = if json { OutputFormat::Json } else { format };
                        show_apple_appstore_context_current(home, settings_file, format)?
                    }
                },
            },
        },
        Command::Aws { command } => match command {
            AWSCommand::Auth { command } => match command {
                AWSAuthCommand::Status {
                    account,
                    region,
                    base_url,
                    access_key,
                    secret_key,
                    session_token,
                    home,
                    settings_file,
                    json,
                    format,
                } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_aws_auth_status(
                        account,
                        region,
                        base_url,
                        access_key,
                        secret_key,
                        session_token,
                        home,
                        settings_file,
                        format,
                    )?
                }
            },
            AWSCommand::Context { command } => match command {
                AWSContextCommand::List { home, settings_file, json, format } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_aws_context_list(home, settings_file, format)?
                }
                AWSContextCommand::Current { home, settings_file, json, format } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_aws_context_current(home, settings_file, format)?
                }
            },
        },
        Command::Gcp { command } => match command {
            GCPCommand::Auth { command } => match command {
                GCPAuthCommand::Status {
                    account,
                    env,
                    project,
                    base_url,
                    access_token,
                    home,
                    settings_file,
                    json,
                    format,
                } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_gcp_auth_status(
                        account,
                        env,
                        project,
                        base_url,
                        access_token,
                        home,
                        settings_file,
                        format,
                    )?
                }
            },
            GCPCommand::Context { command } => match command {
                GCPContextCommand::List { home, settings_file, json, format } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_gcp_context_list(home, settings_file, format)?
                }
                GCPContextCommand::Current { home, settings_file, json, format } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_gcp_context_current(home, settings_file, format)?
                }
            },
        },
        Command::Google { command } => match command {
            GoogleCommand::Places { command } => match command {
                GooglePlacesCommand::Auth { command } => match command {
                    GooglePlacesAuthCommand::Status {
                        account,
                        env,
                        api_key,
                        base_url,
                        project_id,
                        language,
                        region,
                        home,
                        settings_file,
                        json,
                        format,
                    } => {
                        let format = if json { OutputFormat::Json } else { format };
                        show_google_places_auth_status(
                            account,
                            env,
                            api_key,
                            base_url,
                            project_id,
                            language,
                            region,
                            home,
                            settings_file,
                            format,
                        )?
                    }
                },
                GooglePlacesCommand::Context { command } => match command {
                    GooglePlacesContextCommand::List { home, settings_file, json, format } => {
                        let format = if json { OutputFormat::Json } else { format };
                        show_google_places_context_list(home, settings_file, format)?
                    }
                    GooglePlacesContextCommand::Current {
                        account,
                        env,
                        api_key,
                        base_url,
                        project_id,
                        language,
                        region,
                        home,
                        settings_file,
                        json,
                        format,
                    } => {
                        let format = if json { OutputFormat::Json } else { format };
                        show_google_places_context_current(
                            account,
                            env,
                            api_key,
                            base_url,
                            project_id,
                            language,
                            region,
                            home,
                            settings_file,
                            format,
                        )?
                    }
                },
            },
        },
        Command::OpenAI { command } => match command {
            OpenAICommand::Auth { command } => match command {
                OpenAIAuthCommand::Status {
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    home,
                    settings_file,
                    json,
                    format,
                } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_openai_auth_status(
                        account,
                        base_url,
                        api_key,
                        admin_api_key,
                        org_id,
                        project_id,
                        home,
                        settings_file,
                        format,
                    )?
                }
            },
            OpenAICommand::Context { command } => match command {
                OpenAIContextCommand::List { home, settings_file, json, format } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_openai_context_list(home, settings_file, format)?
                }
                OpenAIContextCommand::Current {
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    home,
                    settings_file,
                    json,
                    format,
                } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_openai_context_current(
                        account,
                        base_url,
                        api_key,
                        admin_api_key,
                        org_id,
                        project_id,
                        home,
                        settings_file,
                        format,
                    )?
                }
            },
            OpenAICommand::Model { command } => match command {
                OpenAIModelCommand::List {
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    limit,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_openai_model_list(
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    limit,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
                OpenAIModelCommand::Get {
                    model_id,
                    id,
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_openai_model_get(
                    id.or(model_id),
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
            },
            OpenAICommand::Usage {
                metric,
                account,
                base_url,
                api_key,
                admin_api_key,
                org_id,
                project_id,
                start_time,
                end_time,
                bucket_width,
                limit,
                page,
                batch,
                project_ids,
                user_ids,
                api_key_ids,
                models,
                group_by,
                extra_params,
                home,
                settings_file,
                json,
                raw,
            } => run_openai_usage(
                metric,
                account,
                base_url,
                api_key,
                admin_api_key,
                org_id,
                project_id,
                start_time,
                end_time,
                bucket_width,
                limit,
                page,
                batch,
                project_ids,
                user_ids,
                api_key_ids,
                models,
                group_by,
                extra_params,
                home,
                settings_file,
                json,
                raw,
            )?,
            OpenAICommand::Monitor { command } => match command {
                OpenAIMonitorCommand::Usage {
                    metric,
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    start_time,
                    end_time,
                    bucket_width,
                    limit,
                    page,
                    batch,
                    project_ids,
                    user_ids,
                    api_key_ids,
                    models,
                    group_by,
                    extra_params,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_openai_monitor_usage(
                    metric,
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    start_time,
                    end_time,
                    bucket_width,
                    limit,
                    page,
                    batch,
                    project_ids,
                    user_ids,
                    api_key_ids,
                    models,
                    group_by,
                    extra_params,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
                OpenAIMonitorCommand::Limits {
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    limit,
                    after,
                    before,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_openai_project_rate_limit_list(
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    limit,
                    after,
                    before,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
            },
            OpenAICommand::Codex { command } => match command {
                OpenAICodexCommand::Usage {
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    start_time,
                    end_time,
                    bucket_width,
                    limit,
                    models,
                    group_by,
                    project_ids,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_openai_codex_usage(
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    start_time,
                    end_time,
                    bucket_width,
                    limit,
                    models,
                    group_by,
                    project_ids,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
            },
            OpenAICommand::Key { command } => match command {
                OpenAIKeyCommand::List {
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    limit,
                    after,
                    order,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_openai_key_list(
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    limit,
                    after,
                    order,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
                OpenAIKeyCommand::Get {
                    key_ref,
                    key_id,
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_openai_key_get(
                    key_id.or(key_ref),
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
            },
            OpenAICommand::Project { command } => match command {
                OpenAIProjectCommand::List {
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    limit,
                    after,
                    include_archived,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_openai_project_list(
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    limit,
                    after,
                    include_archived,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
                OpenAIProjectCommand::Get {
                    project_ref,
                    id,
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_openai_project_get(
                    id.or(project_ref),
                    account,
                    base_url,
                    api_key,
                    admin_api_key,
                    org_id,
                    project_id,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
                OpenAIProjectCommand::ApiKey { command } => match command {
                    OpenAIProjectAPIKeyCommand::List {
                        account,
                        base_url,
                        api_key,
                        admin_api_key,
                        org_id,
                        project_id,
                        limit,
                        after,
                        home,
                        settings_file,
                        json,
                        raw,
                    } => run_openai_project_api_key_list(
                        account,
                        base_url,
                        api_key,
                        admin_api_key,
                        org_id,
                        project_id,
                        limit,
                        after,
                        home,
                        settings_file,
                        json,
                        raw,
                    )?,
                    OpenAIProjectAPIKeyCommand::Get {
                        key_ref,
                        key_id,
                        account,
                        base_url,
                        api_key,
                        admin_api_key,
                        org_id,
                        project_id,
                        home,
                        settings_file,
                        json,
                        raw,
                    } => run_openai_project_api_key_get(
                        key_id.or(key_ref),
                        account,
                        base_url,
                        api_key,
                        admin_api_key,
                        org_id,
                        project_id,
                        home,
                        settings_file,
                        json,
                        raw,
                    )?,
                },
                OpenAIProjectCommand::ServiceAccount { command } => match command {
                    OpenAIProjectServiceAccountCommand::List {
                        account,
                        base_url,
                        api_key,
                        admin_api_key,
                        org_id,
                        project_id,
                        limit,
                        after,
                        home,
                        settings_file,
                        json,
                        raw,
                    } => run_openai_project_service_account_list(
                        account,
                        base_url,
                        api_key,
                        admin_api_key,
                        org_id,
                        project_id,
                        limit,
                        after,
                        home,
                        settings_file,
                        json,
                        raw,
                    )?,
                    OpenAIProjectServiceAccountCommand::Get {
                        service_account_ref,
                        service_account_id,
                        account,
                        base_url,
                        api_key,
                        admin_api_key,
                        org_id,
                        project_id,
                        home,
                        settings_file,
                        json,
                        raw,
                    } => run_openai_project_service_account_get(
                        service_account_id.or(service_account_ref),
                        account,
                        base_url,
                        api_key,
                        admin_api_key,
                        org_id,
                        project_id,
                        home,
                        settings_file,
                        json,
                        raw,
                    )?,
                },
                OpenAIProjectCommand::RateLimit { command } => match command {
                    OpenAIProjectRateLimitCommand::List {
                        account,
                        base_url,
                        api_key,
                        admin_api_key,
                        org_id,
                        project_id,
                        limit,
                        after,
                        before,
                        home,
                        settings_file,
                        json,
                        raw,
                    } => run_openai_project_rate_limit_list(
                        account,
                        base_url,
                        api_key,
                        admin_api_key,
                        org_id,
                        project_id,
                        limit,
                        after,
                        before,
                        home,
                        settings_file,
                        json,
                        raw,
                    )?,
                },
            },
        },
        Command::Oci { command } => match command {
            OciCommand::Auth { command } => match command {
                OciAuthCommand::Status {
                    account,
                    profile,
                    config_file,
                    region,
                    base_url,
                    auth,
                    verify,
                    home,
                    settings_file,
                    json,
                    format,
                } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_oci_auth_status(
                        account,
                        profile,
                        config_file,
                        region,
                        base_url,
                        auth,
                        verify,
                        home,
                        settings_file,
                        format,
                    )?
                }
            },
            OciCommand::Oracular { command } => match command {
                OciOracularCommand::Tenancy {
                    profile,
                    config_file,
                    home,
                    settings_file,
                    json,
                    format,
                } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_oci_oracular_tenancy(profile, config_file, home, settings_file, format)?
                }
            },
            OciCommand::Context { command } => match command {
                OciContextCommand::List { home, settings_file, json, format } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_oci_context_list(home, settings_file, format)?
                }
                OciContextCommand::Current {
                    account,
                    profile,
                    config_file,
                    region,
                    base_url,
                    auth,
                    home,
                    settings_file,
                    json,
                    format,
                } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_oci_context_current(
                        account,
                        profile,
                        config_file,
                        region,
                        base_url,
                        auth,
                        home,
                        settings_file,
                        format,
                    )?
                }
            },
        },
        Command::Stripe { command } => match command {
            StripeCommand::Auth { command } => match command {
                StripeAuthCommand::Status {
                    account,
                    env,
                    api_key,
                    home,
                    settings_file,
                    json,
                    format,
                } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_stripe_auth_status(account, env, api_key, home, settings_file, format)?
                }
            },
            StripeCommand::Context { command } => match command {
                StripeContextCommand::List { home, settings_file, json, format } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_stripe_context_list(home, settings_file, format)?
                }
                StripeContextCommand::Current { home, settings_file, json, format } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_stripe_context_current(home, settings_file, format)?
                }
            },
        },
        Command::WorkOS { command } => match command {
            WorkOSCommand::Auth { command } => match command {
                WorkOSAuthCommand::Status {
                    account,
                    env,
                    api_key,
                    client_id,
                    org_id,
                    base_url,
                    home,
                    settings_file,
                    json,
                    format,
                } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_workos_auth_status(
                        account,
                        env,
                        api_key,
                        client_id,
                        org_id,
                        base_url,
                        home,
                        settings_file,
                        format,
                    )?
                }
            },
            WorkOSCommand::Context { command } => match command {
                WorkOSContextCommand::List { home, settings_file, json, format } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_workos_context_list(home, settings_file, format)?
                }
                WorkOSContextCommand::Current { home, settings_file, json, format } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_workos_context_current(home, settings_file, format)?
                }
            },
        },
        Command::GitHub { command } => match command {
            GitHubCommand::Auth { command } => match command {
                GitHubAuthCommand::Status {
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    home,
                    settings_file,
                    json,
                    format,
                } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_github_auth_status(
                        account,
                        owner,
                        base_url,
                        auth_mode,
                        token,
                        app_id,
                        app_key,
                        installation_id,
                        home,
                        settings_file,
                        format,
                    )?
                }
            },
            GitHubCommand::Context { command } => match command {
                GitHubContextCommand::List { home, settings_file, json, format } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_github_context_list(home, settings_file, format)?
                }
                GitHubContextCommand::Current { home, settings_file, json, format } => {
                    let format = if json { OutputFormat::Json } else { format };
                    show_github_context_current(home, settings_file, format)?
                }
            },
            GitHubCommand::Branch { command } => match command {
                GitHubBranchCommand::List {
                    repo_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    protected,
                    max_pages,
                    params,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_github_branch_list(
                    repo_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    protected,
                    max_pages,
                    params,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
                GitHubBranchCommand::Get {
                    repo_ref,
                    branch,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    params,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_github_branch_get(
                    repo_ref,
                    branch,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    params,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
            },
            GitHubCommand::Git { command } => match command {
                GitHubGitCommand::Credential { command } => match command {
                    GitHubGitCredentialCommand::Get {
                        account,
                        owner,
                        base_url,
                        auth_mode,
                        token,
                        app_id,
                        app_key,
                        installation_id,
                        home,
                        settings_file,
                    } => run_github_git_credential_get(
                        account,
                        owner,
                        base_url,
                        auth_mode,
                        token,
                        app_id,
                        app_key,
                        installation_id,
                        home,
                        settings_file,
                    )?,
                    GitHubGitCredentialCommand::Store => {}
                    GitHubGitCredentialCommand::Erase => {}
                },
            },
            GitHubCommand::Raw {
                account,
                owner,
                base_url,
                auth_mode,
                token,
                app_id,
                app_key,
                installation_id,
                method,
                path,
                params,
                home,
                settings_file,
                json,
                raw,
            } => run_github_raw(
                account,
                owner,
                base_url,
                auth_mode,
                token,
                app_id,
                app_key,
                installation_id,
                method,
                path,
                params,
                home,
                settings_file,
                json,
                raw,
            )?,
            GitHubCommand::Graphql {
                account,
                owner,
                base_url,
                auth_mode,
                token,
                app_id,
                app_key,
                installation_id,
                query,
                vars,
                home,
                settings_file,
                json,
                raw,
            } => run_github_graphql(
                account,
                owner,
                base_url,
                auth_mode,
                token,
                app_id,
                app_key,
                installation_id,
                query,
                vars,
                home,
                settings_file,
                json,
                raw,
            )?,
            GitHubCommand::Project { command } => match command {
                GitHubProjectCommand::List {
                    organization_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    limit,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_github_project_list(
                    organization_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    limit,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
                GitHubProjectCommand::Get {
                    project_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_github_project_get(
                    project_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
            },
            GitHubCommand::Issue { command } => match command {
                GitHubIssueCommand::List {
                    repo_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    max_pages,
                    params,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_github_issue_list(
                    repo_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    max_pages,
                    params,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
                GitHubIssueCommand::Get {
                    repo_ref,
                    number,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_github_issue_get(
                    repo_ref,
                    number,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
            },
            GitHubCommand::PullRequest { command } => match command {
                GitHubPullRequestCommand::List {
                    repo_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    max_pages,
                    params,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_github_pr_list(
                    repo_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    max_pages,
                    params,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
                GitHubPullRequestCommand::Get {
                    repo_ref,
                    number,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_github_pr_get(
                    repo_ref,
                    number,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
            },
            GitHubCommand::Workflow { command } => match command {
                GitHubWorkflowCommand::List {
                    repo_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_github_workflow_list(
                    repo_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
                GitHubWorkflowCommand::Runs {
                    repo_ref,
                    workflow,
                    params,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_github_workflow_runs(
                    repo_ref,
                    workflow,
                    params,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
                GitHubWorkflowCommand::Run { command } => match command {
                    GitHubWorkflowRunCommand::Get {
                        repo_ref,
                        run_id,
                        account,
                        owner,
                        base_url,
                        auth_mode,
                        token,
                        app_id,
                        app_key,
                        installation_id,
                        home,
                        settings_file,
                        json,
                        raw,
                    } => run_github_workflow_run_get(
                        repo_ref,
                        run_id,
                        account,
                        owner,
                        base_url,
                        auth_mode,
                        token,
                        app_id,
                        app_key,
                        installation_id,
                        home,
                        settings_file,
                        json,
                        raw,
                    )?,
                },
            },
            GitHubCommand::Repo { command } => match command {
                GitHubRepoCommand::List {
                    owner_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    max_pages,
                    params,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_github_repo_list(
                    owner_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    max_pages,
                    params,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
                GitHubRepoCommand::Get {
                    repo_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_github_repo_get(
                    repo_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
            },
            GitHubCommand::Release { command } => match command {
                GitHubReleaseCommand::List {
                    repo_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    max_pages,
                    params,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_github_release_list(
                    repo_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    max_pages,
                    params,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
                GitHubReleaseCommand::Get {
                    repo_ref,
                    release_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    home,
                    settings_file,
                    json,
                    raw,
                } => run_github_release_get(
                    repo_ref,
                    release_ref,
                    account,
                    owner,
                    base_url,
                    auth_mode,
                    token,
                    app_id,
                    app_key,
                    installation_id,
                    home,
                    settings_file,
                    json,
                    raw,
                )?,
            },
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
            DyadCommand::Logs { name, member, tail, format, docker_bin } => {
                run_dyad_container_logs(&name, &member, &tail, format, docker_bin)?
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
            CodexCommand::Remove { name, volumes, format, docker_bin } => {
                run_codex_remove(&name, volumes, format, docker_bin)?
            }
            CodexCommand::Start { name, format, docker_bin } => {
                run_codex_container_action(&name, ContainerAction::Start, format, docker_bin)?
            }
            CodexCommand::Stop { name, format, docker_bin } => {
                run_codex_container_action(&name, ContainerAction::Stop, format, docker_bin)?
            }
            CodexCommand::Logs { name, tail, docker_bin } => {
                run_codex_container_logs(&name, &tail, false, docker_bin)?
            }
            CodexCommand::Tail { name, tail, docker_bin } => {
                run_codex_container_logs(&name, &tail, true, docker_bin)?
            }
            CodexCommand::Clone { name, repo, gh_pat, format, docker_bin } => {
                run_codex_clone(&name, &repo, gh_pat.as_deref(), format, docker_bin)?
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
                FortSessionStateCommand::Clear { path } => clear_fort_session_state(path)?,
                FortSessionStateCommand::BootstrapView {
                    path,
                    profile_id,
                    access_token_path,
                    refresh_token_path,
                    access_token_container_path,
                    refresh_token_container_path,
                    format,
                } => show_fort_session_bootstrap_view(
                    path,
                    profile_id,
                    &access_token_path,
                    &refresh_token_path,
                    &access_token_container_path,
                    &refresh_token_container_path,
                    format,
                )?,
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
                FortRuntimeAgentStateCommand::Clear { path } => {
                    clear_fort_runtime_agent_state(path)?
                }
            },
        },
        Command::Warmup { command } => match command {
            WarmupCommand::AutostartDecision {
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
            WarmupCommand::Status { path, home, format } => run_warmup_status(path, home, format)?,
            WarmupCommand::State { command } => match command {
                WarmupStateCommand::Write { path, state_json } => {
                    write_warmup_state(path, &state_json)?
                }
            },
            WarmupCommand::Marker { command } => match command {
                WarmupMarkerCommand::Show { autostart_path, disabled_path, home, format } => {
                    run_warmup_marker_show(autostart_path, disabled_path, home, format)?
                }
                WarmupMarkerCommand::WriteAutostart { path } => {
                    write_warmup_autostart_marker(path)?
                }
                WarmupMarkerCommand::SetDisabled { path, disabled } => {
                    set_warmup_disabled_marker(path, &disabled)?
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

fn clear_fort_session_state(path: PathBuf) -> Result<()> {
    clear_persisted_session_state(path)?;
    Ok(())
}

fn show_fort_session_bootstrap_view(
    path: PathBuf,
    profile_id: Option<String>,
    access_token_path: &str,
    refresh_token_path: &str,
    access_token_container_path: &str,
    refresh_token_container_path: &str,
    format: OutputFormat,
) -> Result<()> {
    let state = load_persisted_session_state(path)?;
    let view = build_bootstrap_view(
        &state,
        profile_id.as_deref(),
        access_token_path,
        refresh_token_path,
        access_token_container_path,
        refresh_token_container_path,
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
    println!("container_host_url={}", view.container_host_url);
    println!("access_token_path={}", view.access_token_path);
    println!("refresh_token_path={}", view.refresh_token_path);
    println!("access_token_container_path={}", view.access_token_container_path);
    println!("refresh_token_container_path={}", view.refresh_token_container_path);
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

fn show_github_context_list(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let contexts = list_contexts(&settings.github);
    match format {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&GitHubContextListPayload { contexts })?)
        }
        OutputFormat::Text => print!("{}", render_context_list_text(&contexts)),
    }
    Ok(())
}

fn show_stripe_context_list(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let contexts = list_stripe_contexts(&settings.stripe);
    match format {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&StripeContextListPayload { contexts })?)
        }
        OutputFormat::Text => print!("{}", render_stripe_context_list_text(&contexts)),
    }
    Ok(())
}

fn show_cloudflare_context_list(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let contexts = list_cloudflare_contexts(&settings.cloudflare);
    match format {
        OutputFormat::Json => {
            println!(
                "{}",
                serde_json::to_string_pretty(&CloudflareContextListPayload { contexts })?
            )
        }
        OutputFormat::Text => print!("{}", render_cloudflare_context_list_text(&contexts)),
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn show_cloudflare_auth_status(
    account: Option<String>,
    environment: Option<String>,
    zone_id: Option<String>,
    zone: Option<String>,
    api_token: Option<String>,
    base_url: Option<String>,
    account_id: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let runtime = resolve_cloudflare_auth_runtime(
        &settings.cloudflare,
        &env,
        &CloudflareAuthOverrides {
            account: account.unwrap_or_default(),
            environment: environment.unwrap_or_default(),
            zone_id: zone_id.unwrap_or_default(),
            zone_name: zone.unwrap_or_default(),
            base_url: base_url.unwrap_or_default(),
            account_id: account_id.unwrap_or_default(),
            api_token: api_token.unwrap_or_default(),
        },
    )
    .map_err(anyhow::Error::msg)?;
    let mut payload = CloudflareAuthStatusPayload::from(&runtime);
    let verify_error = match verify_cloudflare_auth_status(&runtime) {
        Ok(verify) => {
            payload.verify = Some(verify);
            None
        }
        Err(err) => {
            payload.status = "error".to_owned();
            payload.verify_error = Some(err.clone());
            Some(err)
        }
    };

    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            let account = if payload.account_alias.trim().is_empty() {
                "(default)"
            } else {
                payload.account_alias.as_str()
            };
            let zone = if !payload.zone_id.trim().is_empty() {
                payload.zone_id.as_str()
            } else {
                or_dash(&payload.zone_name)
            };
            println!("Cloudflare auth: {}", payload.status);
            println!(
                "Context: account={} ({}) env={} zone={} base={}",
                account,
                or_dash(&payload.account_id),
                payload.environment,
                zone,
                payload.base_url
            );
            println!("Source: {}", or_dash(&payload.source));
            println!("Token preview: {}", or_dash(&payload.token_preview));
        }
    }
    if let Some(err) = verify_error {
        return Err(anyhow::anyhow!(err));
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn show_apple_appstore_auth_status(
    account: Option<String>,
    environment: Option<String>,
    bundle_id: Option<String>,
    locale: Option<String>,
    platform: Option<String>,
    issuer_id: Option<String>,
    key_id: Option<String>,
    private_key: Option<String>,
    private_key_file: Option<String>,
    project_id: Option<String>,
    base_url: Option<String>,
    verify: bool,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    if verify {
        return Err(anyhow::anyhow!(
            "apple appstore auth verification is not yet implemented in Rust; rerun with --verify=false or use the Go fallback"
        ));
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let payload = AppleAppStoreAuthStatusPayload::from(
        resolve_apple_appstore_auth_status(
            &settings.apple,
            &env,
            &AppleAppStoreAuthOverrides {
                account: account.unwrap_or_default(),
                environment: environment.unwrap_or_default(),
                bundle_id: bundle_id.unwrap_or_default(),
                locale: locale.unwrap_or_default(),
                platform: platform.unwrap_or_default(),
                issuer_id: issuer_id.unwrap_or_default(),
                key_id: key_id.unwrap_or_default(),
                private_key: private_key.unwrap_or_default(),
                private_key_file: private_key_file.unwrap_or_default(),
                project_id: project_id.unwrap_or_default(),
                base_url: base_url.unwrap_or_default(),
            },
        )
        .map_err(anyhow::Error::msg)?,
    );
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            let account = if payload.account_alias.trim().is_empty() {
                "(default)"
            } else {
                payload.account_alias.as_str()
            };
            println!("Apple App Store auth: {}", payload.status);
            println!(
                "Context: account={} ({}) env={} bundle={} platform={}",
                account,
                or_dash(&payload.project_id),
                payload.environment,
                or_dash(&payload.bundle_id),
                payload.platform
            );
            println!("Source: {}", or_dash(&payload.source));
            println!("Token source: {}", or_dash(&payload.token_source));
        }
    }
    Ok(())
}

fn show_apple_appstore_context_list(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let contexts = list_appstore_contexts(&settings.apple);
    match format {
        OutputFormat::Json => {
            println!(
                "{}",
                serde_json::to_string_pretty(&AppleAppStoreContextListPayload { contexts })?
            )
        }
        OutputFormat::Text => print!("{}", render_appstore_context_list_text(&contexts)),
    }
    Ok(())
}

fn show_apple_appstore_context_current(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let payload = AppleAppStoreCurrentContextPayload::from(
        resolve_apple_appstore_current_context(&settings.apple, &env)
            .map_err(anyhow::Error::msg)?,
    );
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            let account = if payload.account_alias.trim().is_empty() {
                "(default)"
            } else {
                payload.account_alias.as_str()
            };
            println!(
                "Current apple appstore context: account={} ({}) env={} bundle={} platform={}",
                account,
                or_dash(&payload.project_id),
                payload.environment,
                or_dash(&payload.bundle_id),
                payload.platform
            );
            println!("Source: {}", or_dash(&payload.source));
            println!("Token source: {}", or_dash(&payload.token_source));
        }
    }
    Ok(())
}

fn show_aws_context_list(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let contexts = list_aws_contexts(&settings.aws);
    match format {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&AWSContextListPayload { contexts })?)
        }
        OutputFormat::Text => print!("{}", render_aws_context_list_text(&contexts)),
    }
    Ok(())
}

fn show_aws_context_current(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let payload = AWSCurrentContextPayload::from(
        resolve_aws_current_context(&settings.aws, &env).map_err(anyhow::Error::msg)?,
    );
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            let account = if payload.account_alias.trim().is_empty() {
                "(default)"
            } else {
                payload.account_alias.as_str()
            };
            println!(
                "Current aws context: account={} region={} base={}",
                account, payload.region, payload.base_url
            );
            println!("Source: {}", or_dash(&payload.source));
        }
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn show_aws_auth_status(
    account: Option<String>,
    region: Option<String>,
    base_url: Option<String>,
    access_key: Option<String>,
    secret_key: Option<String>,
    session_token: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let payload = AWSAuthStatusPayload::from(
        resolve_aws_auth_status(
            &settings.aws,
            &env,
            &AWSAuthOverrides {
                account: account.unwrap_or_default(),
                region: region.unwrap_or_default(),
                base_url: base_url.unwrap_or_default(),
                access_key: access_key.unwrap_or_default(),
                secret_key: secret_key.unwrap_or_default(),
                session_token: session_token.unwrap_or_default(),
            },
        )
        .map_err(anyhow::Error::msg)?,
    );
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            let account = if payload.account_alias.trim().is_empty() {
                "(default)"
            } else {
                payload.account_alias.as_str()
            };
            println!("AWS auth: ready");
            println!(
                "Context: account={} region={} base={}",
                account, payload.region, payload.base_url
            );
            println!("Source: {}", or_dash(&payload.source));
            println!("Access key: {}", or_dash(&payload.access_key));
        }
    }
    Ok(())
}

fn show_gcp_context_list(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let contexts = list_gcp_contexts(&settings.gcp);
    match format {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&GCPContextListPayload { contexts })?)
        }
        OutputFormat::Text => print!("{}", render_gcp_context_list_text(&contexts)),
    }
    Ok(())
}

fn show_gcp_context_current(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let payload = GCPCurrentContextPayload::from(
        resolve_gcp_current_context(&settings.gcp, &env).map_err(anyhow::Error::msg)?,
    );
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            let account = if payload.account_alias.trim().is_empty() {
                "(default)"
            } else {
                payload.account_alias.as_str()
            };
            println!(
                "Current gcp context: account={} env={} project={} base={}",
                account, payload.environment, payload.project_id, payload.base_url
            );
            println!("Source: {}", or_dash(&payload.source));
        }
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn show_gcp_auth_status(
    account: Option<String>,
    environment: Option<String>,
    project: Option<String>,
    base_url: Option<String>,
    access_token: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let payload = GCPAuthStatusPayload::from(
        resolve_gcp_auth_status(
            &settings.gcp,
            &env,
            &GCPAuthOverrides {
                account: account.unwrap_or_default(),
                environment: environment.unwrap_or_default(),
                project_id: project.unwrap_or_default(),
                base_url: base_url.unwrap_or_default(),
                access_token: access_token.unwrap_or_default(),
            },
        )
        .map_err(anyhow::Error::msg)?,
    );
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            let account = if payload.account_alias.trim().is_empty() {
                "(default)"
            } else {
                payload.account_alias.as_str()
            };
            println!("GCP auth: ready");
            println!(
                "Context: account={} env={} project={} base={}",
                account, payload.environment, payload.project_id, payload.base_url
            );
            println!("Source: {}", or_dash(&payload.source));
            println!("Token preview: {}", or_dash(&payload.token_preview));
        }
    }
    Ok(())
}

fn show_google_places_context_list(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let contexts = list_places_contexts(&settings.google);
    match format {
        OutputFormat::Json => println!(
            "{}",
            serde_json::to_string_pretty(&GooglePlacesContextListPayload { contexts })?
        ),
        OutputFormat::Text => print!("{}", render_places_context_list_text(&contexts)),
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn show_google_places_context_current(
    account: Option<String>,
    environment: Option<String>,
    api_key: Option<String>,
    base_url: Option<String>,
    project_id: Option<String>,
    language: Option<String>,
    region: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let payload = GooglePlacesCurrentContextPayload::from(
        resolve_places_current_context(
            &settings.google,
            &env,
            &GooglePlacesOverrides {
                account: account.unwrap_or_default(),
                environment: environment.unwrap_or_default(),
                api_key: api_key.unwrap_or_default(),
                base_url: base_url.unwrap_or_default(),
                project_id: project_id.unwrap_or_default(),
                language: language.unwrap_or_default(),
                region: region.unwrap_or_default(),
            },
        )
        .map_err(anyhow::Error::msg)?,
    );
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            let account = if payload.account_alias.trim().is_empty() {
                "(default)"
            } else {
                payload.account_alias.as_str()
            };
            println!(
                "Current google places context: account={} env={} project={} language={} region={} base={}",
                account,
                payload.environment,
                or_dash(&payload.project_id),
                or_dash(&payload.language_code),
                or_dash(&payload.region_code),
                payload.base_url
            );
            println!("Source: {}", or_dash(&payload.source));
        }
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn show_google_places_auth_status(
    account: Option<String>,
    environment: Option<String>,
    api_key: Option<String>,
    base_url: Option<String>,
    project_id: Option<String>,
    language: Option<String>,
    region: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let payload = GooglePlacesAuthStatusPayload::from(
        resolve_places_auth_status(
            &settings.google,
            &env,
            &GooglePlacesOverrides {
                account: account.unwrap_or_default(),
                environment: environment.unwrap_or_default(),
                api_key: api_key.unwrap_or_default(),
                base_url: base_url.unwrap_or_default(),
                project_id: project_id.unwrap_or_default(),
                language: language.unwrap_or_default(),
                region: region.unwrap_or_default(),
            },
        )
        .map_err(anyhow::Error::msg)?,
    );
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            let account = if payload.account_alias.trim().is_empty() {
                "(default)"
            } else {
                payload.account_alias.as_str()
            };
            println!("Google Places auth: ready");
            println!(
                "Context: account={} env={} project={} language={} region={} base={}",
                account,
                payload.environment,
                or_dash(&payload.project_id),
                or_dash(&payload.language_code),
                or_dash(&payload.region_code),
                payload.base_url
            );
            println!("Source: {}", or_dash(&payload.source));
            println!("Key preview: {}", or_dash(&payload.key_preview));
        }
    }
    Ok(())
}

fn show_openai_context_list(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let contexts = list_openai_contexts(&settings.openai);
    match format {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&OpenAIContextListPayload { contexts })?)
        }
        OutputFormat::Text => print!("{}", render_openai_context_list_text(&contexts)),
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn show_openai_auth_status(
    account: Option<String>,
    base_url: Option<String>,
    api_key: Option<String>,
    admin_api_key: Option<String>,
    org_id: Option<String>,
    project_id: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let payload = execute_openai_request(
        account,
        base_url,
        api_key,
        admin_api_key,
        org_id,
        project_id,
        home,
        settings_file,
        |runtime| Ok::<OpenAIAuthStatus, String>(verify_openai_auth_status(&runtime)),
    )?;
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => print!("{}", render_openai_auth_status_text(&payload)),
    }
    if payload.status != "ready" {
        anyhow::bail!(
            "{}",
            payload.verify_error.unwrap_or_else(|| "openai auth verification failed".to_owned())
        );
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn show_openai_context_current(
    account: Option<String>,
    base_url: Option<String>,
    api_key: Option<String>,
    admin_api_key: Option<String>,
    org_id: Option<String>,
    project_id: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let payload = OpenAICurrentContextPayload::from(
        resolve_openai_current_context(
            &settings.openai,
            &env,
            &OpenAIContextOverrides {
                account: account.unwrap_or_default(),
                base_url: base_url.unwrap_or_default(),
                api_key: api_key.unwrap_or_default(),
                admin_api_key: admin_api_key.unwrap_or_default(),
                org_id: org_id.unwrap_or_default(),
                project_id: project_id.unwrap_or_default(),
            },
        )
        .map_err(anyhow::Error::msg)?,
    );
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            let account = if payload.account_alias.trim().is_empty() {
                "(default)"
            } else {
                payload.account_alias.as_str()
            };
            println!(
                "Current openai context: account={} base={} org={} project={}",
                account,
                payload.base_url,
                or_dash(&payload.organization_id),
                or_dash(&payload.project_id)
            );
            println!("Source: {}", or_dash(&payload.source));
            println!("Admin key set: {}", if payload.admin_key_set { "true" } else { "false" });
        }
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn run_openai_model_list(
    account: Option<String>,
    base_url: Option<String>,
    api_key: Option<String>,
    admin_api_key: Option<String>,
    org_id: Option<String>,
    project_id: Option<String>,
    limit: Option<usize>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let response = execute_openai_request(
        account,
        base_url,
        api_key,
        admin_api_key,
        org_id,
        project_id,
        home,
        settings_file,
        |runtime| openai_list_models(&runtime, limit),
    )?;
    print_openai_api_response(response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_openai_model_get(
    id: Option<String>,
    account: Option<String>,
    base_url: Option<String>,
    api_key: Option<String>,
    admin_api_key: Option<String>,
    org_id: Option<String>,
    project_id: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let id = id.unwrap_or_default();
    if id.trim().is_empty() {
        anyhow::bail!("model id is required");
    }
    let response = execute_openai_request(
        account,
        base_url,
        api_key,
        admin_api_key,
        org_id,
        project_id,
        home,
        settings_file,
        |runtime| openai_get_model(&runtime, &id),
    )?;
    print_openai_api_response(response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_openai_usage(
    metric: OpenAIUsageMetric,
    account: Option<String>,
    base_url: Option<String>,
    api_key: Option<String>,
    admin_api_key: Option<String>,
    org_id: Option<String>,
    project_id: Option<String>,
    start_time: Option<i64>,
    end_time: Option<i64>,
    bucket_width: Option<String>,
    limit: Option<usize>,
    page: Option<String>,
    batch: bool,
    project_ids: Vec<String>,
    user_ids: Vec<String>,
    api_key_ids: Vec<String>,
    models: Vec<String>,
    group_by: Vec<String>,
    extra_params: Vec<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let metric_name = openai_usage_metric_name(metric);
    let mut params = Vec::new();
    let start_time = start_time.unwrap_or_else(|| {
        (std::time::SystemTime::now() - std::time::Duration::from_secs(7 * 24 * 60 * 60))
            .duration_since(std::time::UNIX_EPOCH)
            .map(|value| value.as_secs() as i64)
            .unwrap_or_default()
    });
    if start_time > 0 {
        params.push(("start_time".to_owned(), start_time.to_string()));
    }
    if let Some(end_time) = end_time.filter(|value| *value > 0) {
        params.push(("end_time".to_owned(), end_time.to_string()));
    }
    if let Some(bucket_width) =
        bucket_width.map(|value| value.trim().to_owned()).filter(|value| !value.is_empty())
    {
        params.push(("bucket_width".to_owned(), bucket_width));
    }
    if let Some(limit) = limit.filter(|value| *value > 0) {
        params.push(("limit".to_owned(), limit.to_string()));
    }
    if let Some(page) = page.map(|value| value.trim().to_owned()).filter(|value| !value.is_empty())
    {
        params.push(("page".to_owned(), page));
    }
    if metric_name == "completions" && batch {
        params.push(("batch".to_owned(), "true".to_owned()));
    }
    for item in project_ids {
        let value = item.trim();
        if !value.is_empty() {
            params.push(("project_ids".to_owned(), value.to_owned()));
        }
    }
    for item in user_ids {
        let value = item.trim();
        if !value.is_empty() {
            params.push(("user_ids".to_owned(), value.to_owned()));
        }
    }
    for item in api_key_ids {
        let value = item.trim();
        if !value.is_empty() {
            params.push(("api_key_ids".to_owned(), value.to_owned()));
        }
    }
    for item in models {
        let value = item.trim();
        if !value.is_empty() {
            params.push(("models".to_owned(), value.to_owned()));
        }
    }
    for item in group_by {
        let value = item.trim();
        if !value.is_empty() {
            params.push(("group_by".to_owned(), value.to_owned()));
        }
    }
    for item in extra_params {
        let value = item.trim();
        if value.is_empty() {
            continue;
        }
        if let Some((key, param_value)) = value.split_once('=') {
            let key = key.trim();
            let param_value = param_value.trim();
            if !key.is_empty() {
                params.push((key.to_owned(), param_value.to_owned()));
            }
        }
    }
    let response = execute_openai_request(
        account,
        base_url,
        api_key,
        admin_api_key,
        org_id,
        project_id,
        home,
        settings_file,
        |runtime| openai_get_usage_metric(&runtime, metric_name, &params),
    )?;
    print_openai_api_response(response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_openai_monitor_usage(
    metric: Option<OpenAIUsageMetric>,
    account: Option<String>,
    base_url: Option<String>,
    api_key: Option<String>,
    admin_api_key: Option<String>,
    org_id: Option<String>,
    project_id: Option<String>,
    start_time: Option<i64>,
    end_time: Option<i64>,
    bucket_width: Option<String>,
    limit: Option<usize>,
    page: Option<String>,
    batch: bool,
    project_ids: Vec<String>,
    user_ids: Vec<String>,
    api_key_ids: Vec<String>,
    models: Vec<String>,
    group_by: Vec<String>,
    extra_params: Vec<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    run_openai_usage(
        metric.unwrap_or(OpenAIUsageMetric::Completions),
        account,
        base_url,
        api_key,
        admin_api_key,
        org_id,
        project_id,
        start_time,
        end_time,
        bucket_width,
        limit,
        page,
        batch,
        project_ids,
        user_ids,
        api_key_ids,
        models,
        group_by,
        extra_params,
        home,
        settings_file,
        json,
        raw,
    )
}

#[allow(clippy::too_many_arguments)]
fn run_openai_codex_usage(
    account: Option<String>,
    base_url: Option<String>,
    api_key: Option<String>,
    admin_api_key: Option<String>,
    org_id: Option<String>,
    project_id: Option<String>,
    start_time: Option<i64>,
    end_time: Option<i64>,
    bucket_width: Option<String>,
    limit: Option<usize>,
    models: Vec<String>,
    group_by: Vec<String>,
    project_ids: Vec<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let models = if models.is_empty() {
        vec!["gpt-5-codex".to_owned()]
    } else {
        models
    };
    run_openai_usage(
        OpenAIUsageMetric::Completions,
        account,
        base_url,
        api_key,
        admin_api_key,
        org_id,
        project_id,
        start_time,
        end_time,
        bucket_width.or(Some("1d".to_owned())),
        limit.or(Some(7)),
        None,
        false,
        project_ids,
        Vec::new(),
        Vec::new(),
        models,
        group_by,
        Vec::new(),
        home,
        settings_file,
        json,
        raw,
    )
}

fn openai_usage_metric_name(metric: OpenAIUsageMetric) -> &'static str {
    match metric {
        OpenAIUsageMetric::Completions => "completions",
        OpenAIUsageMetric::Embeddings => "embeddings",
        OpenAIUsageMetric::Images => "images",
        OpenAIUsageMetric::AudioSpeeches => "audio_speeches",
        OpenAIUsageMetric::AudioTranscriptions => "audio_transcriptions",
        OpenAIUsageMetric::Moderations => "moderations",
        OpenAIUsageMetric::VectorStores => "vector_stores",
        OpenAIUsageMetric::CodeInterpreterSessions => "code_interpreter_sessions",
        OpenAIUsageMetric::Costs => "costs",
    }
}

#[allow(clippy::too_many_arguments)]
fn run_openai_key_list(
    account: Option<String>,
    base_url: Option<String>,
    api_key: Option<String>,
    admin_api_key: Option<String>,
    org_id: Option<String>,
    project_id: Option<String>,
    limit: Option<usize>,
    after: Option<String>,
    order: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let response = execute_openai_request(
        account,
        base_url,
        api_key,
        admin_api_key,
        org_id,
        project_id,
        home,
        settings_file,
        |runtime| {
            openai_list_admin_api_keys(
                &runtime,
                limit,
                after.as_deref().unwrap_or_default(),
                order.as_deref().unwrap_or("asc"),
            )
        },
    )?;
    print_openai_api_response(response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_openai_key_get(
    key_id: Option<String>,
    account: Option<String>,
    base_url: Option<String>,
    api_key: Option<String>,
    admin_api_key: Option<String>,
    org_id: Option<String>,
    project_id: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let key_id = key_id.unwrap_or_default();
    if key_id.trim().is_empty() {
        anyhow::bail!("key id is required");
    }
    let response = execute_openai_request(
        account,
        base_url,
        api_key,
        admin_api_key,
        org_id,
        project_id,
        home,
        settings_file,
        |runtime| openai_get_admin_api_key(&runtime, &key_id),
    )?;
    print_openai_api_response(response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_openai_project_list(
    account: Option<String>,
    base_url: Option<String>,
    api_key: Option<String>,
    admin_api_key: Option<String>,
    org_id: Option<String>,
    project_id: Option<String>,
    limit: Option<usize>,
    after: Option<String>,
    include_archived: bool,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let response = execute_openai_request(
        account,
        base_url,
        api_key,
        admin_api_key,
        org_id,
        project_id,
        home,
        settings_file,
        |runtime| {
            openai_list_projects(
                &runtime,
                limit,
                after.as_deref().unwrap_or_default(),
                include_archived,
            )
        },
    )?;
    print_openai_api_response(response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_openai_project_get(
    id: Option<String>,
    account: Option<String>,
    base_url: Option<String>,
    api_key: Option<String>,
    admin_api_key: Option<String>,
    org_id: Option<String>,
    project_id: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let id = id.unwrap_or_default();
    if id.trim().is_empty() {
        anyhow::bail!("project id is required");
    }
    let response = execute_openai_request(
        account,
        base_url,
        api_key,
        admin_api_key,
        org_id,
        project_id,
        home,
        settings_file,
        |runtime| openai_get_project(&runtime, &id),
    )?;
    print_openai_api_response(response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_openai_project_api_key_list(
    account: Option<String>,
    base_url: Option<String>,
    api_key: Option<String>,
    admin_api_key: Option<String>,
    org_id: Option<String>,
    project_id: Option<String>,
    limit: Option<usize>,
    after: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let project_id = project_id.unwrap_or_default();
    if project_id.trim().is_empty() {
        anyhow::bail!("project id is required");
    }
    let response = execute_openai_request(
        account,
        base_url,
        api_key,
        admin_api_key,
        org_id,
        Some(project_id.clone()),
        home,
        settings_file,
        |runtime| {
            openai_list_project_api_keys(
                &runtime,
                &project_id,
                limit,
                after.as_deref().unwrap_or_default(),
            )
        },
    )?;
    print_openai_api_response(response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_openai_project_api_key_get(
    key_id: Option<String>,
    account: Option<String>,
    base_url: Option<String>,
    api_key: Option<String>,
    admin_api_key: Option<String>,
    org_id: Option<String>,
    project_id: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let project_id = project_id.unwrap_or_default();
    if project_id.trim().is_empty() {
        anyhow::bail!("project id is required");
    }
    let key_id = key_id.unwrap_or_default();
    if key_id.trim().is_empty() {
        anyhow::bail!("key id is required");
    }
    let response = execute_openai_request(
        account,
        base_url,
        api_key,
        admin_api_key,
        org_id,
        Some(project_id.clone()),
        home,
        settings_file,
        |runtime| openai_get_project_api_key(&runtime, &project_id, &key_id),
    )?;
    print_openai_api_response(response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_openai_project_service_account_list(
    account: Option<String>,
    base_url: Option<String>,
    api_key: Option<String>,
    admin_api_key: Option<String>,
    org_id: Option<String>,
    project_id: Option<String>,
    limit: Option<usize>,
    after: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let project_id = project_id.unwrap_or_default();
    if project_id.trim().is_empty() {
        anyhow::bail!("project id is required");
    }
    let response = execute_openai_request(
        account,
        base_url,
        api_key,
        admin_api_key,
        org_id,
        Some(project_id.clone()),
        home,
        settings_file,
        |runtime| {
            openai_list_project_service_accounts(
                &runtime,
                &project_id,
                limit,
                after.as_deref().unwrap_or_default(),
            )
        },
    )?;
    print_openai_api_response(response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_openai_project_service_account_get(
    service_account_id: Option<String>,
    account: Option<String>,
    base_url: Option<String>,
    api_key: Option<String>,
    admin_api_key: Option<String>,
    org_id: Option<String>,
    project_id: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let project_id = project_id.unwrap_or_default();
    if project_id.trim().is_empty() {
        anyhow::bail!("project id is required");
    }
    let service_account_id = service_account_id.unwrap_or_default();
    if service_account_id.trim().is_empty() {
        anyhow::bail!("service account id is required");
    }
    let response = execute_openai_request(
        account,
        base_url,
        api_key,
        admin_api_key,
        org_id,
        Some(project_id.clone()),
        home,
        settings_file,
        |runtime| openai_get_project_service_account(&runtime, &project_id, &service_account_id),
    )?;
    print_openai_api_response(response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_openai_project_rate_limit_list(
    account: Option<String>,
    base_url: Option<String>,
    api_key: Option<String>,
    admin_api_key: Option<String>,
    org_id: Option<String>,
    project_id: Option<String>,
    limit: Option<usize>,
    after: Option<String>,
    before: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let project_id = project_id.unwrap_or_default();
    if project_id.trim().is_empty() {
        anyhow::bail!("project id is required");
    }
    let response = execute_openai_request(
        account,
        base_url,
        api_key,
        admin_api_key,
        org_id,
        Some(project_id.clone()),
        home,
        settings_file,
        |runtime| {
            openai_list_project_rate_limits(
                &runtime,
                &project_id,
                limit,
                after.as_deref().unwrap_or_default(),
                before.as_deref().unwrap_or_default(),
            )
        },
    )?;
    print_openai_api_response(response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn execute_openai_request<T, F>(
    account: Option<String>,
    base_url: Option<String>,
    api_key: Option<String>,
    admin_api_key: Option<String>,
    org_id: Option<String>,
    project_id: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    request: F,
) -> Result<T>
where
    F: FnOnce(OpenAIRuntime) -> std::result::Result<T, String>,
{
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let runtime = resolve_openai_runtime(
        &settings.openai,
        &env,
        &OpenAIContextOverrides {
            account: account.unwrap_or_default(),
            base_url: base_url.unwrap_or_default(),
            api_key: api_key.unwrap_or_default(),
            admin_api_key: admin_api_key.unwrap_or_default(),
            org_id: org_id.unwrap_or_default(),
            project_id: project_id.unwrap_or_default(),
        },
    )
    .map_err(anyhow::Error::msg)?;
    request(runtime).map_err(anyhow::Error::msg)
}

fn print_openai_api_response(response: OpenAIAPIResponse, json: bool, raw: bool) -> Result<()> {
    if json {
        println!("{}", serde_json::to_string_pretty(&response)?);
    } else {
        print!("{}", render_openai_api_response_text(&response, raw));
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn show_oci_auth_status(
    account: Option<String>,
    profile: Option<String>,
    config_file: Option<String>,
    region: Option<String>,
    base_url: Option<String>,
    auth: Option<String>,
    verify: bool,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    if verify {
        return Err(anyhow::anyhow!(
            "oci auth verification is not yet implemented in Rust; rerun with --verify=false or use the Go fallback"
        ));
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let payload = OCIAuthStatusPayload::from(
        resolve_oci_auth_status(
            &settings.oci,
            &env,
            &OCIAuthOverrides {
                account: account.unwrap_or_default(),
                profile: profile.unwrap_or_default(),
                config_file: config_file.unwrap_or_default(),
                region: region.unwrap_or_default(),
                base_url: base_url.unwrap_or_default(),
                auth_style: auth.unwrap_or_default(),
            },
        )
        .map_err(anyhow::Error::msg)?,
    );
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            let account = if payload.account_alias.trim().is_empty() {
                "(default)"
            } else {
                payload.account_alias.as_str()
            };
            println!("OCI auth: {}", payload.status);
            println!(
                "Context: account={} profile={} region={} auth={} base={}",
                account, payload.profile, payload.region, payload.auth_style, payload.base_url
            );
            println!("Source: {}", or_dash(&payload.source));
        }
    }
    Ok(())
}

fn show_oci_oracular_tenancy(
    profile: Option<String>,
    config_file: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let current = resolve_oci_current_context(
        &settings.oci,
        &env,
        &OCIContextOverrides {
            profile: profile.unwrap_or_default(),
            config_file: config_file.unwrap_or_default(),
            ..OCIContextOverrides::default()
        },
    )
    .map_err(anyhow::Error::msg)?;
    let payload = OCIOracularTenancyPayload {
        profile: current.profile,
        config_file: current.config_file,
        tenancy_ocid: current.tenancy_ocid,
    };
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            println!("OCI tenancy: {}", or_dash(&payload.tenancy_ocid));
            println!(
                "Context: profile={} config={}",
                or_dash(&payload.profile),
                or_dash(&payload.config_file)
            );
        }
    }
    Ok(())
}

fn show_oci_context_list(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let contexts = list_oci_contexts(&settings.oci);
    match format {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&OCIContextListPayload { contexts })?)
        }
        OutputFormat::Text => print!("{}", render_oci_context_list_text(&contexts)),
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn show_oci_context_current(
    account: Option<String>,
    profile: Option<String>,
    config_file: Option<String>,
    region: Option<String>,
    base_url: Option<String>,
    auth: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let payload = OCICurrentContextPayload::from(
        resolve_oci_current_context(
            &settings.oci,
            &env,
            &OCIContextOverrides {
                account: account.unwrap_or_default(),
                profile: profile.unwrap_or_default(),
                config_file: config_file.unwrap_or_default(),
                region: region.unwrap_or_default(),
                base_url: base_url.unwrap_or_default(),
                auth_style: auth.unwrap_or_default(),
            },
        )
        .map_err(anyhow::Error::msg)?,
    );
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            let account = if payload.account_alias.trim().is_empty() {
                "(default)"
            } else {
                payload.account_alias.as_str()
            };
            println!(
                "Current oci context: account={} profile={} region={} base={} auth={}",
                account, payload.profile, payload.region, payload.base_url, payload.auth_style
            );
            println!("Source: {}", or_dash(&payload.source));
            println!("Tenancy: {}", or_dash(&payload.tenancy_ocid));
        }
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn show_cloudflare_context_current(
    account: Option<String>,
    environment: Option<String>,
    zone_id: Option<String>,
    zone: Option<String>,
    base_url: Option<String>,
    account_id: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let payload = CloudflareCurrentContextPayload::from(
        resolve_cloudflare_current_context(
            &settings.cloudflare,
            &env,
            &CloudflareContextOverrides {
                account: account.unwrap_or_default(),
                environment: environment.unwrap_or_default(),
                zone_id: zone_id.unwrap_or_default(),
                zone_name: zone.unwrap_or_default(),
                base_url: base_url.unwrap_or_default(),
                account_id: account_id.unwrap_or_default(),
            },
        )
        .map_err(anyhow::Error::msg)?,
    );
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            let account = if payload.account_alias.trim().is_empty() {
                "(default)"
            } else {
                payload.account_alias.as_str()
            };
            let zone = if !payload.zone_id.trim().is_empty() {
                payload.zone_id.as_str()
            } else {
                or_dash(&payload.zone_name)
            };
            println!(
                "Current cloudflare context: account={} ({}) env={} zone={}",
                account,
                or_dash(&payload.account_id),
                payload.environment,
                zone
            );
            println!("Source: {}", or_dash(&payload.source));
        }
    }
    Ok(())
}

fn show_stripe_context_current(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let payload = StripeCurrentContextPayload::from(
        resolve_stripe_current_context(&settings.stripe, &env).map_err(anyhow::Error::msg)?,
    );
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            let account = if payload.account_alias.trim().is_empty() {
                "(default)"
            } else {
                payload.account_alias.as_str()
            };
            println!(
                "Current stripe context: account={} ({}) env={}",
                account,
                or_dash(&payload.account_id),
                payload.environment
            );
            println!("Key source: {}", or_dash(&payload.key_source));
        }
    }
    Ok(())
}

fn show_stripe_auth_status(
    account: Option<String>,
    environment: Option<String>,
    api_key: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let payload = StripeAuthStatusPayload::from(
        resolve_stripe_auth_status(
            &settings.stripe,
            &env,
            &StripeAuthOverrides {
                account: account.unwrap_or_default(),
                environment: environment.unwrap_or_default(),
                api_key: api_key.unwrap_or_default(),
            },
        )
        .map_err(anyhow::Error::msg)?,
    );
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            let account = if payload.account_alias.trim().is_empty() {
                "(default)"
            } else {
                payload.account_alias.as_str()
            };
            println!("Stripe auth: ready");
            println!(
                "Context: account={} ({}) env={}",
                account,
                or_dash(&payload.account_id),
                payload.environment
            );
            println!("Key source: {}", or_dash(&payload.key_source));
            println!("Key preview: {}", or_dash(&payload.key_preview));
        }
    }
    Ok(())
}

fn show_workos_context_list(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let contexts = list_workos_contexts(&settings.workos);
    match format {
        OutputFormat::Json => {
            println!("{}", serde_json::to_string_pretty(&WorkOSContextListPayload { contexts })?)
        }
        OutputFormat::Text => print!("{}", render_workos_context_list_text(&contexts)),
    }
    Ok(())
}

fn show_workos_context_current(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let payload = WorkOSCurrentContextPayload::from(
        resolve_workos_current_context(&settings.workos, &env).map_err(anyhow::Error::msg)?,
    );
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            let account = if payload.account_alias.trim().is_empty() {
                "(default)"
            } else {
                payload.account_alias.as_str()
            };
            println!(
                "Current workos context: account={} env={} org={} client_id={}",
                account,
                payload.environment,
                or_dash(&payload.organization_id),
                or_dash(&payload.client_id)
            );
            println!("Source: {}", or_dash(&payload.source));
        }
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn show_workos_auth_status(
    account: Option<String>,
    environment: Option<String>,
    api_key: Option<String>,
    client_id: Option<String>,
    organization_id: Option<String>,
    base_url: Option<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let payload = WorkOSAuthStatusPayload::from(
        resolve_workos_auth_status(
            &settings.workos,
            &env,
            &WorkOSAuthOverrides {
                account: account.unwrap_or_default(),
                environment: environment.unwrap_or_default(),
                base_url: base_url.unwrap_or_default(),
                api_key: api_key.unwrap_or_default(),
                client_id: client_id.unwrap_or_default(),
                organization_id: organization_id.unwrap_or_default(),
            },
        )
        .map_err(anyhow::Error::msg)?,
    );
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            let account = if payload.account_alias.trim().is_empty() {
                "(default)"
            } else {
                payload.account_alias.as_str()
            };
            println!("WorkOS auth: ready");
            println!(
                "Context: account={} env={} org={} client_id={}",
                account,
                payload.environment,
                or_dash(&payload.organization_id),
                or_dash(&payload.client_id)
            );
            println!("Source: {}", or_dash(&payload.source));
            println!("Token preview: {}", or_dash(&payload.key_preview));
        }
    }
    Ok(())
}

fn show_github_context_current(
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let current = resolve_current_context(&settings.github, &env);
    let payload = GitHubCurrentContextPayload {
        account_alias: current.account_alias,
        owner: current.owner,
        auth_mode: current.auth_mode,
        base_url: current.base_url,
        source: current.source,
    };
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            println!(
                "Current github context: account={} owner={} auth={} base={}",
                or_dash(&payload.account_alias),
                or_dash(&payload.owner),
                or_dash(&payload.auth_mode),
                or_dash(&payload.base_url)
            );
            println!("Source: {}", or_dash(&payload.source));
        }
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn show_github_auth_status(
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    format: OutputFormat,
) -> Result<()> {
    fn or_dash(value: &str) -> &str {
        if value.trim().is_empty() { "-" } else { value }
    }

    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    let status = resolve_auth_status(
        &settings.github,
        &env,
        &GitHubAuthOverrides {
            account: account.unwrap_or_default(),
            owner: owner.unwrap_or_default(),
            base_url: base_url.unwrap_or_default(),
            auth_mode: auth_mode.unwrap_or_default(),
            token: token.unwrap_or_default(),
            app_id,
            app_key: app_key.unwrap_or_default(),
            installation_id,
        },
    )
    .map_err(anyhow::Error::msg)?;
    let payload = GitHubAuthStatusPayload::from(status);
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&payload)?),
        OutputFormat::Text => {
            let account = if payload.account_alias.trim().is_empty() {
                "(default)"
            } else {
                payload.account_alias.as_str()
            };
            let base_url = if payload.base_url.trim().is_empty() {
                "https://api.github.com"
            } else {
                payload.base_url.as_str()
            };
            println!("GitHub auth: ready");
            println!(
                "Context: account={} owner={} auth={} base={}",
                account,
                or_dash(&payload.owner),
                or_dash(&payload.auth_mode),
                base_url
            );
            println!("Source: {}", or_dash(&payload.source));
            println!("Token preview: {}", or_dash(&payload.token_preview));
        }
    }
    Ok(())
}

fn parse_github_owner_repo(repo_ref: &str, default_owner: &str) -> Result<(String, String)> {
    let trimmed = repo_ref.trim();
    if trimmed.is_empty() {
        return Err(anyhow::Error::msg("github repo is required"));
    }
    if let Some((owner, repo)) = trimmed.split_once('/') {
        let owner = owner.trim();
        let repo = repo.trim();
        if owner.is_empty() || repo.is_empty() {
            return Err(anyhow::Error::msg("github repo must be <owner/repo> or <repo>"));
        }
        return Ok((owner.to_owned(), repo.to_owned()));
    }
    let owner = default_owner.trim();
    if owner.is_empty() {
        return Err(anyhow::Error::msg("github owner is required when repo is not fully qualified"));
    }
    Ok((owner.to_owned(), trimmed.to_owned()))
}

#[derive(Debug, Default)]
struct GitHubGitCredentialRequest {
    protocol: String,
    host: String,
    path: String,
}

fn read_github_git_credential_request(mut input: impl Read) -> Result<GitHubGitCredentialRequest> {
    let mut raw = String::new();
    input.read_to_string(&mut raw)?;
    let mut payload = BTreeMap::new();
    for line in raw.lines() {
        let line = line.trim();
        if line.is_empty() {
            break;
        }
        let Some((key, value)) = line.split_once('=') else {
            continue;
        };
        payload.insert(key.trim().to_ascii_lowercase(), value.trim().to_owned());
    }
    let mut request = GitHubGitCredentialRequest {
        protocol: payload.get("protocol").cloned().unwrap_or_default(),
        host: payload.get("host").cloned().unwrap_or_default(),
        path: payload.get("path").cloned().unwrap_or_default(),
    };
    if let Some(raw_url) = payload.get("url").map(String::as_str).filter(|value| !value.trim().is_empty()) {
        let parsed = url::Url::parse(raw_url).map_err(|err| anyhow::Error::msg(format!("parse credential url: {err}")))?;
        if request.protocol.trim().is_empty() {
            request.protocol = parsed.scheme().trim().to_owned();
        }
        if request.host.trim().is_empty() {
            request.host = parsed.host_str().unwrap_or_default().trim().to_owned();
        }
        if request.path.trim().is_empty() {
            request.path = parsed.path().trim().to_owned();
        }
    }
    request.host = normalize_git_host(&request.host);
    if request.host.trim().is_empty() {
        return Err(anyhow::Error::msg("git credential request is missing host"));
    }
    Ok(request)
}

fn normalize_git_host(host: &str) -> String {
    let mut host = host.trim().to_ascii_lowercase();
    for prefix in ["https://", "http://", "ssh://"] {
        if let Some(stripped) = host.strip_prefix(prefix) {
            host = stripped.to_owned();
        }
    }
    if let Some((_, right)) = host.split_once('@') {
        host = right.to_owned();
    }
    if let Some((left, _)) = host.split_once('/') {
        host = left.to_owned();
    }
    host.trim().to_owned()
}

fn git_owner_repo_from_credential_path(path: &str) -> (String, String) {
    let mut path = path.trim();
    if path.is_empty() {
        return (String::new(), String::new());
    }
    path = path.trim_start_matches('/');
    if let Some((left, _)) = path.split_once('?') {
        path = left;
    }
    let mut parts = path.split('/');
    let owner = parts.next().unwrap_or_default().trim();
    let repo = parts
        .next()
        .unwrap_or_default()
        .trim()
        .trim_end_matches(".git");
    if owner.is_empty() || repo.is_empty() {
        return (String::new(), String::new());
    }
    (owner.to_owned(), repo.to_owned())
}

fn is_git_credential_host_allowed(host: &str, base_url: &str) -> bool {
    let host = normalize_git_host(host);
    if host.is_empty() {
        return false;
    }
    let mut allowed = BTreeMap::new();
    allowed.insert("github.com".to_owned(), ());
    if let Ok(parsed) = url::Url::parse(base_url.trim()) {
        let base_host = normalize_git_host(parsed.host_str().unwrap_or_default());
        if !base_host.is_empty() {
            allowed.insert(base_host.clone(), ());
            if let Some(stripped) = base_host.strip_prefix("api.") {
                allowed.insert(stripped.to_owned(), ());
            }
        }
    }
    allowed.contains_key(&host)
}

fn parse_github_params(params: Vec<String>) -> Result<BTreeMap<String, String>> {
    let mut out = BTreeMap::new();
    for raw in params {
        let Some((key, value)) = raw.split_once('=') else {
            return Err(anyhow::Error::msg(format!(
                "invalid --param {raw:?} (expected key=value)"
            )));
        };
        let key = key.trim();
        if key.is_empty() {
            return Err(anyhow::Error::msg("github --param key cannot be empty"));
        }
        out.insert(key.to_owned(), value.trim().to_owned());
    }
    Ok(out)
}

fn parse_github_graphql_vars(params: Vec<String>) -> Result<serde_json::Map<String, serde_json::Value>> {
    let mut out = serde_json::Map::new();
    for raw in params {
        let Some((key, value)) = raw.split_once('=') else {
            return Err(anyhow::Error::msg(format!(
                "invalid --var {raw:?} (expected key=value)"
            )));
        };
        let key = key.trim();
        if key.is_empty() {
            return Err(anyhow::Error::msg("github --var key cannot be empty"));
        }
        let value = value.trim();
        let parsed = serde_json::from_str::<serde_json::Value>(value)
            .unwrap_or_else(|_| serde_json::Value::String(value.to_owned()));
        out.insert(key.to_owned(), parsed);
    }
    Ok(out)
}

fn github_graphql_query_is_read_only(query: &str) -> bool {
    !query.trim_start().to_ascii_lowercase().starts_with("mutation")
}

#[allow(clippy::too_many_arguments)]
fn load_github_runtime(
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
) -> Result<si_rs_provider_github::GitHubRuntime> {
    let home = home.unwrap_or_else(default_home_dir);
    let settings = Settings::load(&home, settings_file.as_deref())?;
    let env = std::env::vars().collect();
    resolve_github_runtime(
        &settings.github,
        &env,
        &GitHubAuthOverrides {
            account: account.unwrap_or_default(),
            owner: owner.unwrap_or_default(),
            base_url: base_url.unwrap_or_default(),
            auth_mode: auth_mode.unwrap_or_default(),
            token: token.unwrap_or_default(),
            app_id,
            app_key: app_key.unwrap_or_default(),
            installation_id,
        },
    )
    .map_err(anyhow::Error::msg)
}

fn print_github_api_response(response: &GitHubAPIResponse, json: bool, raw: bool) -> Result<()> {
    if json {
        println!("{}", serde_json::to_string_pretty(response)?);
        return Ok(());
    }

    println!("Status: {} {}", response.status_code, response.status);
    if !response.request_id.trim().is_empty() {
        println!("Request ID: {}", response.request_id);
    }
    if raw && !response.body.trim().is_empty() {
        println!("{}", response.body);
        return Ok(());
    }
    if let Some(data) = &response.data {
        println!("{}", serde_json::to_string_pretty(data)?);
    } else if !response.list.is_empty() {
        println!("{}", serde_json::to_string_pretty(&response.list)?);
    } else if !response.body.trim().is_empty() {
        println!("{}", response.body);
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn run_github_release_list(
    repo_ref: Option<String>,
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    max_pages: usize,
    params: Vec<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let runtime = load_github_runtime(
        account,
        owner,
        base_url,
        auth_mode,
        token,
        app_id,
        app_key,
        installation_id,
        home,
        settings_file,
    )?;
    let (repo_owner, repo_name) =
        parse_github_owner_repo(repo_ref.as_deref().unwrap_or_default(), &runtime.owner)?;
    let params = parse_github_params(params)?;
    let response =
        github_list_releases(&runtime, &repo_owner, &repo_name, &params, max_pages)
            .map_err(anyhow::Error::msg)?;
    print_github_api_response(&response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_github_release_get(
    repo_ref: Option<String>,
    release_ref: Option<String>,
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let runtime = load_github_runtime(
        account,
        owner,
        base_url,
        auth_mode,
        token,
        app_id,
        app_key,
        installation_id,
        home,
        settings_file,
    )?;
    let (repo_owner, repo_name) =
        parse_github_owner_repo(repo_ref.as_deref().unwrap_or_default(), &runtime.owner)?;
    let release_ref = release_ref
        .filter(|value| !value.trim().is_empty())
        .ok_or_else(|| anyhow::Error::msg("github release ref is required"))?;
    let response = github_get_release(&runtime, &repo_owner, &repo_name, &release_ref)
        .map_err(anyhow::Error::msg)?;
    print_github_api_response(&response, json, raw)
}

fn summarize_github_item(item: &Value) -> String {
    if let Some(full_name) = item.get("full_name").and_then(Value::as_str) {
        return full_name.to_owned();
    }
    if let Some(name) = item.get("name").and_then(Value::as_str) {
        return name.to_owned();
    }
    serde_json::to_string(item).unwrap_or_else(|_| "{}".to_owned())
}

#[derive(Debug, Clone)]
struct GitHubProjectRef {
    project_id: String,
    organization: String,
    number: i64,
}

fn parse_github_project_ref(raw: &str) -> Result<GitHubProjectRef> {
    let value = raw.trim();
    if value.is_empty() {
        return Err(anyhow::Error::msg("project reference is required"));
    }
    if value.starts_with("http://") || value.starts_with("https://") {
        let marker = "/orgs/";
        if let Some(idx) = value.find(marker) {
            let tail = &value[idx + marker.len()..];
            let parts = tail.split('/').collect::<Vec<_>>();
            if parts.len() >= 3 && parts[1].eq_ignore_ascii_case("projects") {
                return Ok(GitHubProjectRef {
                    project_id: String::new(),
                    organization: parts[0].trim().to_owned(),
                    number: parts[2]
                        .trim()
                        .parse::<i64>()
                        .map_err(|_| anyhow::Error::msg("project number must be an integer"))?,
                });
            }
        }
        return Err(anyhow::Error::msg(format!(
            "unsupported project url format: {value}"
        )));
    }
    if let Ok(number) = value.parse::<i64>() {
        return Ok(GitHubProjectRef {
            project_id: String::new(),
            organization: String::new(),
            number,
        });
    }
    if let Some((organization, number)) = value.split_once('/') {
        if let Ok(parsed) = number.trim().parse::<i64>() {
            let organization = organization.trim();
            if !organization.is_empty() {
                return Ok(GitHubProjectRef {
                    project_id: String::new(),
                    organization: organization.to_owned(),
                    number: parsed,
                });
            }
        }
    }
    Ok(GitHubProjectRef {
        project_id: value.to_owned(),
        organization: String::new(),
        number: 0,
    })
}

fn summarize_github_project(project: &Value) -> String {
    let number = project.get("number").and_then(Value::as_i64).unwrap_or_default();
    let title = project
        .get("title")
        .and_then(Value::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .unwrap_or("(untitled)");
    let project_id = project
        .get("id")
        .and_then(Value::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .unwrap_or("-");
    let public_text = if project.get("public").and_then(Value::as_bool).unwrap_or(false) {
        "public"
    } else {
        "private"
    };
    let closed_text = if project.get("closed").and_then(Value::as_bool).unwrap_or(false) {
        "closed"
    } else {
        "open"
    };
    if let Some(url) = project.get("url").and_then(Value::as_str).map(str::trim).filter(|value| !value.is_empty()) {
        return format!("#{number} {title} [{public_text}, {closed_text}] {project_id} ({url})");
    }
    format!("#{number} {title} [{public_text}, {closed_text}] {project_id}")
}

#[allow(clippy::too_many_arguments)]
fn run_github_project_list(
    organization_ref: Option<String>,
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    limit: usize,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    if limit == 0 {
        return Err(anyhow::Error::msg("--limit must be greater than 0"));
    }
    let runtime = load_github_runtime(
        account,
        owner.clone(),
        base_url,
        auth_mode,
        token,
        app_id,
        app_key,
        installation_id,
        home,
        settings_file,
    )?;
    let organization = organization_ref
        .filter(|value| !value.trim().is_empty())
        .or(owner)
        .unwrap_or_else(|| runtime.owner.clone());
    if organization.trim().is_empty() {
        return Err(anyhow::Error::msg(
            "organization owner is required (use positional org, --owner, or context owner)",
        ));
    }
    let response =
        github_list_projects(&runtime, &organization, limit).map_err(anyhow::Error::msg)?;
    let projects = response
        .data
        .as_ref()
        .and_then(|data| data.get("organization"))
        .and_then(|organization| organization.get("projectsV2"))
        .and_then(|projects| projects.get("nodes"))
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default();
    if json {
        println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({
                "organization": organization,
                "count": projects.len(),
                "projects": projects,
            }))?
        );
        return Ok(());
    }
    if raw {
        println!("{}", response.body);
        return Ok(());
    }
    println!("Project list: {} ({})", organization, projects.len());
    for project in &projects {
        println!("  {}", summarize_github_project(project));
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn run_github_project_get(
    project_ref: Option<String>,
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let runtime = load_github_runtime(
        account,
        owner.clone(),
        base_url,
        auth_mode,
        token,
        app_id,
        app_key,
        installation_id,
        home,
        settings_file,
    )?;
    let reference = parse_github_project_ref(project_ref.as_deref().unwrap_or_default())?;
    let mut organization = reference.organization.clone();
    if organization.trim().is_empty() {
        organization = owner.unwrap_or_else(|| runtime.owner.clone());
    }
    let project_id = if !reference.project_id.trim().is_empty() {
        reference.project_id
    } else {
        if organization.trim().is_empty() {
            return Err(anyhow::Error::msg(format!(
                "organization is required to resolve project number {}",
                reference.number
            )));
        }
        github_resolve_project_id(&runtime, &organization, reference.number)
            .map_err(anyhow::Error::msg)?
    };
    let response = github_get_project(&runtime, &project_id).map_err(anyhow::Error::msg)?;
    let project = response
        .data
        .as_ref()
        .and_then(|data| data.get("node"))
        .cloned()
        .unwrap_or(Value::Null);
    if project.is_null() {
        return Err(anyhow::Error::msg("project not found"));
    }
    if json {
        let mut payload = serde_json::Map::new();
        payload.insert("project".to_owned(), project);
        if !organization.trim().is_empty() {
            payload.insert("organization".to_owned(), Value::String(organization));
        }
        println!("{}", serde_json::to_string_pretty(&Value::Object(payload))?);
        return Ok(());
    }
    if raw {
        println!("{}", response.body);
        return Ok(());
    }
    println!("{}", serde_json::to_string_pretty(&project)?);
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn run_github_workflow_list(
    repo_ref: Option<String>,
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let runtime = load_github_runtime(
        account,
        owner,
        base_url,
        auth_mode,
        token,
        app_id,
        app_key,
        installation_id,
        home,
        settings_file,
    )?;
    let (repo_owner, repo_name) =
        parse_github_owner_repo(repo_ref.as_deref().unwrap_or_default(), &runtime.owner)?;
    let response =
        github_list_workflows(&runtime, &repo_owner, &repo_name).map_err(anyhow::Error::msg)?;
    print_github_api_response(&response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_github_workflow_runs(
    repo_ref: Option<String>,
    workflow: Option<String>,
    params: Vec<String>,
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let runtime = load_github_runtime(
        account,
        owner,
        base_url,
        auth_mode,
        token,
        app_id,
        app_key,
        installation_id,
        home,
        settings_file,
    )?;
    let (repo_owner, repo_name) =
        parse_github_owner_repo(repo_ref.as_deref().unwrap_or_default(), &runtime.owner)?;
    let params = parse_github_params(params)?;
    let response = github_list_workflow_runs(
        &runtime,
        &repo_owner,
        &repo_name,
        workflow.as_deref().unwrap_or_default(),
        &params,
    )
    .map_err(anyhow::Error::msg)?;
    print_github_api_response(&response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_github_workflow_run_get(
    repo_ref: Option<String>,
    run_id: Option<i64>,
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let runtime = load_github_runtime(
        account,
        owner,
        base_url,
        auth_mode,
        token,
        app_id,
        app_key,
        installation_id,
        home,
        settings_file,
    )?;
    let (repo_owner, repo_name) =
        parse_github_owner_repo(repo_ref.as_deref().unwrap_or_default(), &runtime.owner)?;
    let run_id = run_id.ok_or_else(|| anyhow::Error::msg("workflow run id is required"))?;
    let response =
        github_get_workflow_run(&runtime, &repo_owner, &repo_name, run_id)
            .map_err(anyhow::Error::msg)?;
    print_github_api_response(&response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_github_branch_list(
    repo_ref: Option<String>,
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    protected: Option<String>,
    max_pages: usize,
    params: Vec<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let runtime = load_github_runtime(
        account,
        owner,
        base_url,
        auth_mode,
        token,
        app_id,
        app_key,
        installation_id,
        home,
        settings_file,
    )?;
    let (repo_owner, repo_name) =
        parse_github_owner_repo(repo_ref.as_deref().unwrap_or_default(), &runtime.owner)?;
    let mut params = parse_github_params(params)?;
    if let Some(value) = protected.filter(|value| !value.trim().is_empty()) {
        let value = value.trim().to_ascii_lowercase();
        if value != "true" && value != "false" {
            return Err(anyhow::Error::msg(format!(
                "invalid --protected {value:?} (expected true|false)"
            )));
        }
        params.insert("protected".to_owned(), value);
    }
    let response =
        github_list_branches(&runtime, &repo_owner, &repo_name, &params, max_pages)
            .map_err(anyhow::Error::msg)?;
    if json {
        println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({
                "repo": format!("{repo_owner}/{repo_name}"),
                "count": response.list.len(),
                "data": response.list,
            }))?
        );
        return Ok(());
    }
    if raw {
        println!("{}", serde_json::to_string(&response.list)?);
        return Ok(());
    }
    println!("Branch list: {repo_owner}/{repo_name} ({})", response.list.len());
    println!("{}", serde_json::to_string_pretty(&response.list)?);
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn run_github_branch_get(
    repo_ref: Option<String>,
    branch: Option<String>,
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    params: Vec<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let runtime = load_github_runtime(
        account,
        owner,
        base_url,
        auth_mode,
        token,
        app_id,
        app_key,
        installation_id,
        home,
        settings_file,
    )?;
    let (repo_owner, repo_name) =
        parse_github_owner_repo(repo_ref.as_deref().unwrap_or_default(), &runtime.owner)?;
    let branch = branch
        .filter(|value| !value.trim().is_empty())
        .ok_or_else(|| anyhow::Error::msg("branch is required"))?;
    let params = parse_github_params(params)?;
    let response =
        github_get_branch(&runtime, &repo_owner, &repo_name, &branch, &params)
            .map_err(anyhow::Error::msg)?;
    print_github_api_response(&response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_github_git_credential_get(
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
) -> Result<()> {
    let request = read_github_git_credential_request(io::stdin())?;
    let (parsed_owner, parsed_repo) = git_owner_repo_from_credential_path(&request.path);
    let owner = owner.or_else(|| {
        if parsed_owner.trim().is_empty() {
            None
        } else {
            Some(parsed_owner.clone())
        }
    });
    let runtime = load_github_runtime(
        account,
        owner,
        base_url,
        auth_mode,
        token,
        app_id,
        app_key,
        installation_id,
        home,
        settings_file,
    )?;
    if !is_git_credential_host_allowed(&request.host, &runtime.base_url) {
        return Ok(());
    }
    let token = github_resolve_access_token(
        &runtime,
        if runtime.owner.trim().is_empty() { &parsed_owner } else { &runtime.owner },
        &parsed_repo,
    )
    .map_err(anyhow::Error::msg)?;
    if token.trim().is_empty() {
        return Err(anyhow::Error::msg("github auth token is empty"));
    }
    print!("username=x-access-token\npassword={token}\n\n");
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn run_github_raw(
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    method: String,
    path: Option<String>,
    params: Vec<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    if !method.trim().eq_ignore_ascii_case("GET") {
        return Err(anyhow::Error::msg(
            "github raw Rust path only supports GET",
        ));
    }
    let path = path
        .filter(|value| !value.trim().is_empty())
        .ok_or_else(|| anyhow::Error::msg("github raw requires --path"))?;
    let params = parse_github_params(params)?;
    let runtime = load_github_runtime(
        account,
        owner,
        base_url,
        auth_mode,
        token,
        app_id,
        app_key,
        installation_id,
        home,
        settings_file,
    )?;
    let response = github_raw_get(&runtime, &path, &params).map_err(anyhow::Error::msg)?;
    print_github_api_response(&response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_github_graphql(
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    query: Option<String>,
    vars: Vec<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let query = query
        .filter(|value| !value.trim().is_empty())
        .ok_or_else(|| anyhow::Error::msg("github graphql requires --query"))?;
    if !github_graphql_query_is_read_only(&query) {
        return Err(anyhow::Error::msg(
            "github graphql Rust path only supports queries",
        ));
    }
    let runtime = load_github_runtime(
        account,
        owner,
        base_url,
        auth_mode,
        token,
        app_id,
        app_key,
        installation_id,
        home,
        settings_file,
    )?;
    let variables = serde_json::Value::Object(parse_github_graphql_vars(vars)?);
    let response =
        github_graphql_query(&runtime, &query, variables).map_err(anyhow::Error::msg)?;
    print_github_api_response(&response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_github_issue_list(
    repo_ref: Option<String>,
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    max_pages: usize,
    params: Vec<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let runtime = load_github_runtime(
        account,
        owner,
        base_url,
        auth_mode,
        token,
        app_id,
        app_key,
        installation_id,
        home,
        settings_file,
    )?;
    let (repo_owner, repo_name) =
        parse_github_owner_repo(repo_ref.as_deref().unwrap_or_default(), &runtime.owner)?;
    let params = parse_github_params(params)?;
    let response = github_list_issues(&runtime, &repo_owner, &repo_name, &params, max_pages)
        .map_err(anyhow::Error::msg)?;
    print_github_api_response(&response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_github_issue_get(
    repo_ref: Option<String>,
    number: Option<i64>,
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let runtime = load_github_runtime(
        account,
        owner,
        base_url,
        auth_mode,
        token,
        app_id,
        app_key,
        installation_id,
        home,
        settings_file,
    )?;
    let (repo_owner, repo_name) =
        parse_github_owner_repo(repo_ref.as_deref().unwrap_or_default(), &runtime.owner)?;
    let number = number.ok_or_else(|| anyhow::Error::msg("issue number is required"))?;
    let response =
        github_get_issue(&runtime, &repo_owner, &repo_name, number).map_err(anyhow::Error::msg)?;
    print_github_api_response(&response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_github_pr_list(
    repo_ref: Option<String>,
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    max_pages: usize,
    params: Vec<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let runtime = load_github_runtime(
        account,
        owner,
        base_url,
        auth_mode,
        token,
        app_id,
        app_key,
        installation_id,
        home,
        settings_file,
    )?;
    let (repo_owner, repo_name) =
        parse_github_owner_repo(repo_ref.as_deref().unwrap_or_default(), &runtime.owner)?;
    let params = parse_github_params(params)?;
    let response =
        github_list_pull_requests(&runtime, &repo_owner, &repo_name, &params, max_pages)
            .map_err(anyhow::Error::msg)?;
    print_github_api_response(&response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_github_pr_get(
    repo_ref: Option<String>,
    number: Option<i64>,
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let runtime = load_github_runtime(
        account,
        owner,
        base_url,
        auth_mode,
        token,
        app_id,
        app_key,
        installation_id,
        home,
        settings_file,
    )?;
    let (repo_owner, repo_name) =
        parse_github_owner_repo(repo_ref.as_deref().unwrap_or_default(), &runtime.owner)?;
    let number = number.ok_or_else(|| anyhow::Error::msg("pull request number is required"))?;
    let response = github_get_pull_request(&runtime, &repo_owner, &repo_name, number)
        .map_err(anyhow::Error::msg)?;
    print_github_api_response(&response, json, raw)
}

#[allow(clippy::too_many_arguments)]
fn run_github_repo_list(
    owner_ref: Option<String>,
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    max_pages: usize,
    params: Vec<String>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let runtime = load_github_runtime(
        account,
        owner.clone(),
        base_url,
        auth_mode,
        token,
        app_id,
        app_key,
        installation_id,
        home,
        settings_file,
    )?;
    let selected_owner = owner_ref
        .filter(|value| !value.trim().is_empty())
        .or(owner)
        .unwrap_or_else(|| runtime.owner.clone());
    if selected_owner.trim().is_empty() {
        return Err(anyhow::Error::msg(
            "owner is required (use --owner, context owner, or positional owner)",
        ));
    }
    let params = parse_github_params(params)?;
    let response = github_list_repos(&runtime, &selected_owner, &params, max_pages)
        .map_err(anyhow::Error::msg)?;
    if json {
        println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({
                "owner": selected_owner,
                "count": response.list.len(),
                "data": response.list,
            }))?
        );
        return Ok(());
    }
    if raw {
        println!("{}", serde_json::to_string(&response.list)?);
        return Ok(());
    }
    println!("Repository list: {} ({})", selected_owner, response.list.len());
    for item in &response.list {
        println!("  {}", summarize_github_item(item));
    }
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn run_github_repo_get(
    repo_ref: Option<String>,
    account: Option<String>,
    owner: Option<String>,
    base_url: Option<String>,
    auth_mode: Option<String>,
    token: Option<String>,
    app_id: Option<i64>,
    app_key: Option<String>,
    installation_id: Option<i64>,
    home: Option<PathBuf>,
    settings_file: Option<PathBuf>,
    json: bool,
    raw: bool,
) -> Result<()> {
    let runtime = load_github_runtime(
        account,
        owner,
        base_url,
        auth_mode,
        token,
        app_id,
        app_key,
        installation_id,
        home,
        settings_file,
    )?;
    let (repo_owner, repo_name) =
        parse_github_owner_repo(repo_ref.as_deref().unwrap_or_default(), &runtime.owner)?;
    let response =
        github_get_repo(&runtime, &repo_owner, &repo_name).map_err(anyhow::Error::msg)?;
    print_github_api_response(&response, json, raw)
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
    format: OutputFormat,
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
    let logs = String::from_utf8_lossy(&output.stdout).into_owned();
    match format {
        OutputFormat::Json => {
            println!(
                "{}",
                serde_json::to_string_pretty(&serde_json::json!({
                    "dyad": dyad,
                    "member": member,
                    "tail": tail.parse::<i32>().unwrap_or(0),
                    "logs": logs,
                }))?
            );
        }
        OutputFormat::Text => print!("{logs}"),
    }
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

fn inspect_codex_profile_label(
    docker_program: &str,
    container_name: &str,
) -> Result<Option<String>> {
    let command = si_rs_process::CommandSpec::new(docker_program.to_owned()).args([
        "inspect".to_owned(),
        "--format".to_owned(),
        "{{ index .Config.Labels \"si.codex.profile\" }}".to_owned(),
        container_name.to_owned(),
    ]);
    let output = ProcessRunner.run(&command, &RunOptions::default())?;
    if !output.status.success() {
        return Ok(None);
    }
    let value = String::from_utf8_lossy(&output.stdout).trim().to_owned();
    if value.is_empty() || value == "<no value>" {
        return Ok(None);
    }
    Ok(Some(value))
}

fn run_codex_remove(
    name: &str,
    volumes: bool,
    format: OutputFormat,
    docker_bin: Option<PathBuf>,
) -> Result<()> {
    let artifacts = build_remove_artifacts(name)?;
    let docker_program =
        docker_bin.unwrap_or_else(|| si_rs_docker::docker_binary_path().to_path_buf());
    let docker_program_str = docker_program.display().to_string();
    let profile_id = inspect_codex_profile_label(&docker_program_str, &artifacts.container_name)?;
    let remove_container = docker_container_remove_command(
        docker_program_str.clone(),
        artifacts.container_name.clone(),
        true,
    )?;
    let output = ProcessRunner.run(&remove_container, &RunOptions::default())?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        anyhow::bail!("docker rm failed: {}", stderr.trim());
    }
    let mut rendered = String::from_utf8_lossy(&output.stdout).into_owned();
    if volumes {
        for volume_name in [&artifacts.codex_volume, &artifacts.gh_volume] {
            let remove_volume = docker_volume_remove_command(
                docker_program_str.clone(),
                volume_name.clone(),
                true,
            )?;
            let output = ProcessRunner.run(&remove_volume, &RunOptions::default())?;
            if !output.status.success() {
                let stderr = String::from_utf8_lossy(&output.stderr);
                anyhow::bail!("docker volume rm failed: {}", stderr.trim());
            }
            rendered.push_str(&String::from_utf8_lossy(&output.stdout));
        }
    }
    match format {
        OutputFormat::Json => {
            let view = CodexRemoveResultView {
                name: artifacts.name,
                container_name: artifacts.container_name,
                profile_id,
                codex_volume: volumes.then_some(artifacts.codex_volume),
                gh_volume: volumes.then_some(artifacts.gh_volume),
                output: rendered,
            };
            println!("{}", serde_json::to_string_pretty(&view)?);
        }
        OutputFormat::Text => print!("{rendered}"),
    }
    Ok(())
}

fn run_codex_container_action(
    name: &str,
    action: ContainerAction,
    format: OutputFormat,
    docker_bin: Option<PathBuf>,
) -> Result<()> {
    let artifacts = build_remove_artifacts(name)?;
    let docker_program =
        docker_bin.unwrap_or_else(|| si_rs_docker::docker_binary_path().to_path_buf());
    let command = docker_container_action_command(
        docker_program.display().to_string(),
        action,
        artifacts.container_name.clone(),
    )?;
    let output = ProcessRunner.run(&command, &RunOptions::default())?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        anyhow::bail!("docker {} failed: {}", action.as_str(), stderr.trim());
    }
    let rendered = String::from_utf8_lossy(&output.stdout).into_owned();
    match format {
        OutputFormat::Json => {
            let view = CodexContainerActionView {
                action: action.as_str().to_owned(),
                name: name.trim().to_owned(),
                container_name: artifacts.container_name,
                output: rendered,
            };
            println!("{}", serde_json::to_string_pretty(&view)?);
        }
        OutputFormat::Text => print!("{rendered}"),
    }
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
    format: OutputFormat,
    docker_bin: Option<PathBuf>,
) -> Result<()> {
    let artifacts = build_remove_artifacts(name)?;
    let repo = repo.trim();
    if repo.is_empty() {
        anyhow::bail!("repo is required");
    }
    let docker_program =
        docker_bin.unwrap_or_else(|| si_rs_docker::docker_binary_path().to_path_buf());
    let mut spec = ContainerExecSpec::new(artifacts.container_name.clone())
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
    let rendered = String::from_utf8_lossy(&output.stdout).into_owned();
    match format {
        OutputFormat::Json => {
            let view = CodexCloneResultView {
                name: name.trim().to_owned(),
                repo: repo.trim().to_owned(),
                container_name: artifacts.container_name,
                output: rendered,
            };
            println!("{}", serde_json::to_string_pretty(&view)?);
        }
        OutputFormat::Text => print!("{rendered}"),
    }
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
    let decision = classify_autostart_request(&marker_state, &state);
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
