//! Development-only SI release and packaging tooling.
//!
//! Replaces the former public `si build` surface. This binary is never shipped
//! to users; CI and developers invoke it via `cargo run -p si-tools --bin si-release`.

use clap::Parser;
use si_tools::release::{BuildCommand, run};

#[derive(Debug, Parser)]
#[command(
    name = "si-release",
    about = "Development-only SI release and packaging tooling.",
    arg_required_else_help = true
)]
struct Cli {
    #[command(subcommand)]
    command: BuildCommand,
}

fn main() -> anyhow::Result<()> {
    let cli = Cli::parse();
    run(cli.command)
}
