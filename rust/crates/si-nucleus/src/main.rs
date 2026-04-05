use si_nucleus::{NucleusConfig, NucleusService};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let service = NucleusService::open(NucleusConfig::from_env()?)?;
    service.serve().await
}
