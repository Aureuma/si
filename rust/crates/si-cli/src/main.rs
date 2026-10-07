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
use clap::builder::styling::{AnsiColor, Effects, Styles};
use clap::{
    ArgAction, Args, ColorChoice, CommandFactory, FromArgMatches, Parser, Subcommand, ValueEnum,
};
use serde::Serialize;
use serde_json::Value;
use si_codex::codex_worker_slot_name;
use si_command_manifest::{CommandCategory, CommandSpec, find_root_command, visible_root_commands};
use si_config::paths::SiPaths;
use si_config::settings::Settings;
use si_muse::muse_worker_slot_name;
use std::env;
use std::fmt;
use std::fs;
use std::io::{self, IsTerminal};
#[cfg(unix)]
use std::path::{Path, PathBuf};

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
    Codex {
        #[command(subcommand)]
        command: Box<CodexCommand>,
    },
    Muse {
        #[command(subcommand)]
        command: Box<MuseCommand>,
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
    #[command(name = "respawn")]
    Respawn(CodexRespawnArgs),
}
#[derive(Debug, Args)]
struct MuseSpawnStartArgs {
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
    muse_bin: Option<PathBuf>,
    #[arg(long, default_value = "text")]
    format: OutputFormat,
    #[arg(last = true)]
    native: Vec<String>,
}

#[derive(Debug, Args)]
struct MuseRespawnArgs {
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
    muse_bin: Option<PathBuf>,
    #[arg(long)]
    resume_id: Option<String>,
    #[arg(long, default_value = "text")]
    format: OutputFormat,
}

#[derive(Debug, Args)]
struct MuseProfileListArgs {
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long, default_value = "text")]
    format: OutputFormat,
}

#[derive(Debug, Args)]
struct MuseProfileShowArgs {
    profile: Option<String>,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long, default_value = "text")]
    format: OutputFormat,
}

#[derive(Debug, Args)]
struct MuseProfileAddArgs {
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
struct MuseProfileRemoveArgs {
    profile: Option<String>,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct MuseProfileLoginArgs {
    profile: Option<String>,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long)]
    muse_bin: Option<PathBuf>,
    #[arg(long, default_value = "text")]
    format: OutputFormat,
}

#[derive(Debug, Args)]
struct MuseProfileLogoutArgs {
    profile: Option<String>,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long)]
    muse_bin: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct MuseProfileSelectArgs {
    profile: String,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
}

#[derive(Debug, Args)]
struct MuseRunArgs {
    profile: Option<String>,
    #[arg(long = "profile", conflicts_with = "profile")]
    profile_flag: Option<String>,
    #[arg(long)]
    home: Option<PathBuf>,
    #[arg(long)]
    settings_file: Option<PathBuf>,
    #[arg(long)]
    muse_bin: Option<PathBuf>,
    #[arg(last = true, required = true)]
    native: Vec<String>,
}

#[derive(Debug, Subcommand)]
enum MuseProfileCommand {
    List(MuseProfileListArgs),
    Show(MuseProfileShowArgs),
    Add(MuseProfileAddArgs),
    Remove(MuseProfileRemoveArgs),
    Login(MuseProfileLoginArgs),
    Logout(MuseProfileLogoutArgs),
    Select(MuseProfileSelectArgs),
}

#[derive(Debug, Args)]
struct MuseTmuxArgs {
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
struct MuseRemoveArgs {
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
struct MuseStopArgs {
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
struct MuseTailArgs {
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
struct MuseShellArgs {
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
enum MuseCommand {
    #[command(name = "profile", alias = "profiles")]
    Profile {
        #[command(subcommand)]
        command: MuseProfileCommand,
    },
    Spawn(MuseSpawnStartArgs),
    Remove(MuseRemoveArgs),
    Stop(MuseStopArgs),
    Tail(MuseTailArgs),
    Shell(MuseShellArgs),
    Run(MuseRunArgs),
    List {
        #[arg(long)]
        home: Option<PathBuf>,
        #[arg(long)]
        settings_file: Option<PathBuf>,
        #[arg(long, default_value = "text")]
        format: OutputFormat,
    },
    Tmux(MuseTmuxArgs),
    #[command(name = "respawn")]
    Respawn(MuseRespawnArgs),
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
struct DistributionDoctorView {
    version: String,
    binary: String,
    checks: Vec<DistributionDoctorCheck>,
    native: Vec<NativeDoctorEntry>,
}

#[derive(Debug, Serialize)]
struct NativeDoctorEntry {
    provider: String,
    program: String,
    version: String,
    tested_version: String,
    support: String,
    platform: String,
    detail: String,
}

fn codex_native_doctor_entry() -> NativeDoctorEntry {
    let blank = |detail: &str| NativeDoctorEntry {
        provider: "codex".to_owned(),
        program: String::new(),
        version: String::new(),
        tested_version: String::new(),
        support: "missing".to_owned(),
        platform: "supported".to_owned(),
        detail: detail.to_owned(),
    };
    match resolve_program_on_path("codex") {
        None => blank("native codex not found on PATH"),
        Some(program) => {
            let version = si_codex::adapter::probe_native_version(&program).unwrap_or_default();
            NativeDoctorEntry {
                provider: "codex".to_owned(),
                program: program.display().to_string(),
                version,
                tested_version: String::new(),
                support: "managed".to_owned(),
                platform: "supported".to_owned(),
                detail: "file-store forcing applies to any native version".to_owned(),
            }
        }
    }
}

fn muse_native_doctor_entry() -> NativeDoctorEntry {
    let blank = |detail: &str| NativeDoctorEntry {
        provider: "muse".to_owned(),
        program: String::new(),
        version: String::new(),
        tested_version: si_muse::adapter::MUSE_TESTED_VERSION.to_owned(),
        support: "missing".to_owned(),
        platform: match si_muse::adapter::ensure_muse_platform_supported() {
            Ok(()) => "supported".to_owned(),
            Err(detail) => detail.to_string(),
        },
        detail: detail.to_owned(),
    };
    let program = match resolve_program_on_path("muse") {
        None => return blank("native muse not found on PATH"),
        Some(program) => program,
    };
    let version = si_muse::adapter::probe_native_version(&program).unwrap_or_default();
    let (support, detail) = match si_muse::adapter::muse_version_support(&version) {
        si_muse::adapter::MuseVersionSupport::Supported => {
            ("supported".to_owned(), "auth-path contract verified against this native".to_owned())
        }
        si_muse::adapter::MuseVersionSupport::Unverified => (
            "unverified".to_owned(),
            format!(
                "re-run the auth-destination checks before trusting isolation on this native (verified: {})",
                si_muse::adapter::MUSE_TESTED_VERSION
            ),
        ),
    };
    NativeDoctorEntry {
        provider: "muse".to_owned(),
        program: program.display().to_string(),
        version,
        tested_version: si_muse::adapter::MUSE_TESTED_VERSION.to_owned(),
        support,
        platform: match si_muse::adapter::ensure_muse_platform_supported() {
            Ok(()) => "supported".to_owned(),
            Err(detail) => detail.to_string(),
        },
        detail,
    }
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
        distribution_doctor_check("muse", "Muse tmux operator sessions"),
        distribution_doctor_check("tmux", "operator sessions"),
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
        native: vec![codex_native_doctor_entry(), muse_native_doctor_entry()],
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
            println!("native providers:");
            for native in &view.native {
                let program =
                    if native.program.is_empty() { "(not found)" } else { native.program.as_str() };
                let version =
                    if native.version.is_empty() { "(unprobed)" } else { native.version.as_str() };
                println!(
                    "  {:<8} {} {} [{}; platform: {}]",
                    native.provider, program, version, native.support, native.platform
                );
                println!("    {}", native.detail);
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
        "fort" | "surf" | "viva" | "image" | "build" | "warmup" => Some("si --help"),
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
        Command::Muse { command } => match *command {
            MuseCommand::Profile { command } => match command {
                MuseProfileCommand::List(MuseProfileListArgs { home, settings_file, format }) => {
                    let ctx = agents::store::AgentContext::load(home, settings_file)?;
                    agents::muse::profile_list(&ctx, format)?
                }
                MuseProfileCommand::Show(MuseProfileShowArgs {
                    profile,
                    home,
                    settings_file,
                    format,
                }) => {
                    let ctx = agents::store::AgentContext::load(home, settings_file)?;
                    agents::muse::profile_show(&ctx, profile.as_deref(), format)?
                }
                MuseProfileCommand::Add(MuseProfileAddArgs {
                    profile,
                    name,
                    email,
                    home,
                    settings_file,
                    format,
                }) => {
                    let mut ctx = agents::store::AgentContext::load(home, settings_file)?;
                    agents::muse::profile_add(&mut ctx, &profile, name, email, format)?
                }
                MuseProfileCommand::Remove(MuseProfileRemoveArgs {
                    profile,
                    home,
                    settings_file,
                }) => {
                    let mut ctx = agents::store::AgentContext::load(home, settings_file)?;
                    agents::muse::profile_remove(&mut ctx, profile.as_deref())?
                }
                MuseProfileCommand::Login(MuseProfileLoginArgs {
                    profile,
                    home,
                    settings_file,
                    muse_bin,
                    format,
                }) => {
                    let mut ctx = agents::store::AgentContext::load(home, settings_file)?;
                    agents::muse::profile_login(&mut ctx, profile.as_deref(), muse_bin, format)?
                }
                MuseProfileCommand::Logout(MuseProfileLogoutArgs {
                    profile,
                    home,
                    settings_file,
                    muse_bin,
                }) => {
                    let mut ctx = agents::store::AgentContext::load(home, settings_file)?;
                    agents::muse::profile_logout(&mut ctx, profile.as_deref(), muse_bin)?
                }
                MuseProfileCommand::Select(MuseProfileSelectArgs {
                    profile,
                    home,
                    settings_file,
                }) => {
                    let mut ctx = agents::store::AgentContext::load(home, settings_file)?;
                    agents::muse::profile_select(&mut ctx, &profile)?
                }
            },
            MuseCommand::Spawn(MuseSpawnStartArgs {
                profile,
                profile_flag,
                worker_slot,
                home,
                settings_file,
                workspace,
                workdir,
                muse_bin,
                format,
                native,
            }) => {
                let requested = resolve_muse_cli_profile_arg(profile, profile_flag);
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                agents::muse::spawn(
                    &ctx,
                    requested.as_deref(),
                    worker_slot.as_deref(),
                    workspace,
                    workdir.as_deref(),
                    muse_bin,
                    native,
                    format,
                )?
            }
            MuseCommand::Remove(MuseRemoveArgs {
                profile,
                profile_flag,
                worker_slot,
                home,
                settings_file,
                stop,
                all,
                format,
            }) => {
                let requested = resolve_muse_cli_profile_arg(profile, profile_flag);
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                if all {
                    agents::muse::remove_all(&ctx, stop, format)?
                } else {
                    agents::muse::worker_remove(
                        &ctx,
                        requested.as_deref(),
                        worker_slot.as_deref(),
                        stop,
                        format,
                    )?
                }
            }
            MuseCommand::Stop(MuseStopArgs {
                profile,
                profile_flag,
                worker_slot,
                home,
                settings_file,
                force,
                all,
                format,
            }) => {
                let requested = resolve_muse_cli_profile_arg(profile, profile_flag);
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                if all {
                    agents::muse::stop_all(&ctx, force, format)?
                } else {
                    agents::muse::stop(
                        &ctx,
                        requested.as_deref(),
                        worker_slot.as_deref(),
                        force,
                        format,
                    )?
                }
            }
            MuseCommand::Tail(MuseTailArgs {
                profile,
                profile_flag,
                worker_slot,
                home,
                settings_file,
                tail,
            }) => {
                let requested = resolve_muse_cli_profile_arg(profile, profile_flag);
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                let lines: usize = tail
                    .parse()
                    .with_context(|| format!("invalid --tail {tail:?}; want a line count"))?;
                agents::muse::tail(&ctx, requested.as_deref(), worker_slot.as_deref(), lines)?
            }
            MuseCommand::Shell(MuseShellArgs {
                profile,
                worker_slot,
                home,
                settings_file,
                workspace,
                workdir,
                command,
            }) => {
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                agents::muse::shell(
                    &ctx,
                    profile.as_deref(),
                    worker_slot.as_deref(),
                    workspace,
                    workdir.as_deref(),
                    command,
                )?
            }
            MuseCommand::Run(MuseRunArgs {
                profile,
                profile_flag,
                home,
                settings_file,
                muse_bin,
                native,
            }) => {
                let requested = resolve_muse_cli_profile_arg(profile, profile_flag);
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                agents::muse::run(&ctx, requested.as_deref(), muse_bin, &native)?
            }
            MuseCommand::List { home, settings_file, format } => {
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                agents::muse::list(&ctx, format)?
            }
            MuseCommand::Tmux(MuseTmuxArgs {
                profile,
                profile_flag,
                worker_slot,
                home,
                settings_file,
            }) => {
                let requested = resolve_muse_cli_profile_arg(profile, profile_flag);
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                agents::muse::attach(&ctx, requested.as_deref(), worker_slot.as_deref())?
            }
            MuseCommand::Respawn(MuseRespawnArgs {
                profile,
                profile_flag,
                worker_slot,
                home,
                settings_file,
                muse_bin,
                resume_id,
                format,
            }) => {
                let requested = resolve_muse_cli_profile_arg(profile, profile_flag);
                let ctx = agents::store::AgentContext::load(home, settings_file)?;
                agents::muse::respawn(
                    &ctx,
                    requested.as_deref(),
                    worker_slot.as_deref(),
                    muse_bin,
                    resume_id,
                    format,
                )?
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
        ["codex", "respawn"] => {
            Some("Restart a Codex worker on the same profile (reuse or resume).")
        }
        ["muse"] => Some("Manage Muse profiles and worker sessions."),
        ["muse", "profile"] => Some("Manage Muse profiles."),
        ["muse", "spawn"] => Some("Start a Muse worker for a chosen profile."),
        ["muse", "remove"] => Some("Remove a Muse worker session."),
        ["muse", "stop"] => Some("Stop a Muse worker session."),
        ["muse", "tail"] => Some("Tail Muse worker session output."),
        ["muse", "shell"] => Some("Run a shell command with a Muse worker environment."),
        ["muse", "run"] => Some("Run a native Muse command in the foreground."),
        ["muse", "profile", "select"] => Some("Select the default Muse profile."),
        ["muse", "profile", "logout"] => Some("Log a Muse profile out natively."),
        ["muse", "list"] => Some("List Muse worker sessions."),
        ["muse", "tmux"] => Some("Attach to a Muse tmux session."),
        ["muse", "respawn"] => Some("Restart a Muse worker on the same profile (reuse or resume)."),
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

#[derive(Clone, Copy)]
enum CliStream {
    Stdout,
}

#[derive(Clone, Copy)]
enum CliTone {
    Command,
    Label,
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
        },
    }
}

fn cli_ansi_code(tone: CliTone) -> &'static str {
    match tone {
        CliTone::Command => "1;35",
        CliTone::Label => "1;34",
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

fn resolve_codex_cli_profile_arg(
    positional: Option<String>,
    flag: Option<String>,
) -> Option<String> {
    flag.or(positional).map(|value| value.trim().to_owned()).filter(|value| !value.is_empty())
}

fn resolve_muse_cli_profile_arg(
    positional: Option<String>,
    flag: Option<String>,
) -> Option<String> {
    flag.or(positional).map(|value| value.trim().to_owned()).filter(|value| !value.is_empty())
}

fn sanitize_tmux_profile_label(raw: &str) -> Option<String> {
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
        .and_then(sanitize_tmux_profile_label)
        .unwrap_or_else(|| profile_id.trim().to_owned());
    let slot = codex_worker_slot_name(Some(worker_slot));
    if let Some(emoji) = codex_repo_emoji_for_slot(&slot) {
        format!("{profile_label} {emoji} [{slot}]")
    } else {
        format!("{profile_label} [{slot}]")
    }
}

pub(crate) fn muse_tmux_window_name(
    profile_id: &str,
    profile_display_name: Option<&str>,
    worker_slot: &str,
) -> String {
    let profile_label = profile_display_name
        .and_then(sanitize_tmux_profile_label)
        .unwrap_or_else(|| profile_id.trim().to_owned());
    let slot = muse_worker_slot_name(Some(worker_slot));
    format!("{profile_label} [{slot}]")
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
        CommandCategory::Muse => "muse",
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
    fn muse_tmux_window_name_includes_profile_and_slot() {
        assert_eq!(
            muse_tmux_window_name("personal", Some(" Ada :\nLovelace  "), " ReView "),
            "Ada Lovelace [review]"
        );
        assert_eq!(muse_tmux_window_name("work", None, "primary"), "work [primary]");
    }
}
