use std::sync::Arc;

use si_nucleus::{NucleusConfig, NucleusService};
use si_nucleus_runtime_codex::CodexNucleusRuntime;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let service = NucleusService::open_with_runtime(
        NucleusConfig::from_env()?,
        Arc::new(CodexNucleusRuntime::new()),
    )?;
    service.serve().await
}
