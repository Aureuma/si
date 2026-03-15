# Rust Transition Workspace

This workspace is the staged Rust rewrite for `si`.

Current scope:

- shared version metadata
- `.si` path defaults and staged modular settings parsing
- typed process execution foundations with capture, cwd/env overrides, and timeout handling
- typed Docker run specs with early bind-mount validation
- a small Rust CLI entrypoint for read-only diagnostics

Current entrypoint:

```bash
cargo run -p si-rs-cli -- version
cargo run -p si-rs-cli -- help --format json
cargo run -p si-rs-cli -- settings show --format json
cargo run -p si-rs-cli -- providers characteristics --provider github --format json
cargo run -p si-rs-cli -- paths show --format json
```

Experimental Go-to-Rust delegation path for the first migrated read-only command:

```bash
SI_EXPERIMENTAL_RUST_CLI=1 SI_RUST_CLI_BIN="$(pwd)/.artifacts/cargo-target/debug/si-rs" ./si version
SI_EXPERIMENTAL_RUST_CLI=1 SI_RUST_CLI_BIN="$(pwd)/.artifacts/cargo-target/debug/si-rs" ./si help remote-control
SI_EXPERIMENTAL_RUST_CLI=1 SI_RUST_CLI_BIN="$(pwd)/.artifacts/cargo-target/debug/si-rs" ./si providers characteristics --provider github --json
```

The shipping Go `si` CLI remains the source of truth until a command family reaches the cutover criteria in `tickets/2026-03-15-si-rust-transition-plan.md`.
