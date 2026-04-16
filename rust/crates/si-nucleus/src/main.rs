use std::sync::Arc;

use si_nucleus::{NucleusConfig, NucleusService};
use si_nucleus_runtime_codex::CodexNucleusRuntime;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let args = std::env::args().skip(1).collect::<Vec<_>>();
    if args.iter().any(|arg| arg == "--version" || arg == "-V") {
        println!("v{}", env!("CARGO_PKG_VERSION"));
        return Ok(());
    }
    if args.iter().any(|arg| arg == "--help" || arg == "-h") {
        println!(
            "Usage: si-nucleus [--version]

Run the SI Nucleus gateway service."
        );
        return Ok(());
    }
    if !args.is_empty() {
        anyhow::bail!("unsupported si-nucleus arguments: {}", args.join(" "));
    }

    let service = NucleusService::open_with_runtime(
        NucleusConfig::from_env()?,
        Arc::new(CodexNucleusRuntime::new()),
    )?;
    service.serve().await
}
