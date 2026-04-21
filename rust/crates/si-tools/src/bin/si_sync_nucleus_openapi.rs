use std::env;
use std::fs;
use std::path::PathBuf;
use std::process::ExitCode;

use serde_json::Value;
use si_nucleus::{GPT_ACTIONS_OPENAPI_PUBLIC_URL, public_openapi_document};

const USAGE: &str = r#"Synchronize docs/gpt-actions-openapi.yaml from the canonical Nucleus OpenAPI document.

Usage:
  cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-sync-nucleus-openapi -- --write
  cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-sync-nucleus-openapi -- --check

Options:
  --write              Overwrite the target YAML file with the generated document.
  --check              Fail if the target YAML file does not match the generated document.
  --output <path>      Override the output path. Defaults to docs/gpt-actions-openapi.yaml.
  --public-url <url>   Override the public server URL. Defaults to https://nucleus.aureuma.ai.
  -h, --help           Show this help text.
"#;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum Mode {
    Check,
    Write,
}

#[derive(Debug)]
struct Args {
    mode: Mode,
    output: PathBuf,
    public_url: String,
}

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(message) => {
            eprintln!("si-sync-nucleus-openapi: {message}");
            ExitCode::from(1)
        }
    }
}

fn run() -> Result<(), String> {
    let args = parse_args(env::args().skip(1).collect())?;
    let generated = public_openapi_document(&args.public_url);
    let rendered = serde_yaml::to_string(&generated)
        .map_err(|err| format!("render generated OpenAPI YAML: {err}"))?;

    match args.mode {
        Mode::Write => {
            if let Some(parent) = args.output.parent() {
                fs::create_dir_all(parent)
                    .map_err(|err| format!("create {}: {err}", parent.display()))?;
            }
            fs::write(&args.output, rendered)
                .map_err(|err| format!("write {}: {err}", args.output.display()))?;
            println!("updated {}", args.output.display());
            Ok(())
        }
        Mode::Check => {
            let existing = fs::read_to_string(&args.output)
                .map_err(|err| format!("read {}: {err}", args.output.display()))?;
            let existing_value: Value = serde_yaml::from_str(&existing)
                .map_err(|err| format!("parse {}: {err}", args.output.display()))?;
            if existing_value == generated {
                println!("ok {}", args.output.display());
                Ok(())
            } else {
                Err(format!(
                    "{} does not match the generated Nucleus OpenAPI document; run with --write",
                    args.output.display()
                ))
            }
        }
    }
}

fn parse_args(args: Vec<String>) -> Result<Args, String> {
    let mut mode = None;
    let mut output = default_output_path();
    let mut public_url = GPT_ACTIONS_OPENAPI_PUBLIC_URL.to_owned();
    let mut iter = args.into_iter();
    while let Some(arg) = iter.next() {
        match arg.as_str() {
            "--write" => set_mode(&mut mode, Mode::Write)?,
            "--check" => set_mode(&mut mode, Mode::Check)?,
            "--output" => {
                let value = iter.next().ok_or_else(|| "--output requires a path".to_string())?;
                output = PathBuf::from(value);
            }
            "--public-url" => {
                let value =
                    iter.next().ok_or_else(|| "--public-url requires a value".to_string())?;
                let trimmed = value.trim();
                if trimmed.is_empty() {
                    return Err("--public-url must not be empty".to_string());
                }
                public_url = trimmed.to_string();
            }
            "-h" | "--help" => return Err(USAGE.to_string()),
            other => {
                return Err(format!(
                    "unknown argument: {other}

{USAGE}"
                ));
            }
        }
    }

    let Some(mode) = mode else {
        return Err(format!(
            "must pass exactly one of --write or --check

{USAGE}"
        ));
    };

    Ok(Args { mode, output, public_url })
}

fn set_mode(current: &mut Option<Mode>, next: Mode) -> Result<(), String> {
    match current {
        Some(existing) if *existing != next => {
            Err("cannot pass both --write and --check".to_string())
        }
        Some(_) => Err("duplicate mode flag".to_string()),
        None => {
            *current = Some(next);
            Ok(())
        }
    }
}

fn default_output_path() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../docs/gpt-actions-openapi.yaml")
}
