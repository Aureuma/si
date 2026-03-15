use anyhow::Result;
use clap::{Parser, Subcommand, ValueEnum};
use serde::Serialize;
use si_rs_config::paths::SiPaths;
use std::fmt;
use std::path::PathBuf;

#[derive(Debug, Parser)]
#[command(name = "si-rs", disable_version_flag = true)]
struct Cli {
    #[command(subcommand)]
    command: Command,
}

#[derive(Debug, Subcommand)]
enum Command {
    Version,
    Paths {
        #[command(subcommand)]
        command: PathsCommand,
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

fn main() -> Result<()> {
    let cli = Cli::parse();

    match cli.command {
        Command::Version => {
            println!("{}", si_rs_core::version::current_version());
        }
        Command::Paths { command } => match command {
            PathsCommand::Show { home, settings_file, format } => {
                show_paths(home, settings_file, format)?
            }
        },
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

fn default_home_dir() -> PathBuf {
    std::env::var_os("HOME")
        .map(PathBuf::from)
        .filter(|path| !path.as_os_str().is_empty())
        .unwrap_or_else(|| PathBuf::from("/"))
}
