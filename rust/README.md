# Rust Workspace

This workspace is the source of truth for `si`.

Current scope:

- shared version metadata
- `.si` path defaults and staged modular settings parsing
- runtime path resolution for workspace/config discovery
- typed process execution foundations with capture, cwd/env overrides, and timeout handling
- typed runtime launch specs with early path validation
- shared Codex worker-path and workspace binding for local sessions
- local Codex worker spawn assembly for names, environment, workdir, and process launch inputs
- Rust CLI exposure for Codex worker lifecycle commands under `si-rs codex ...`
- the primary Rust CLI entrypoint used for local and shipped runtime flows

Current entrypoint:

```bash
cargo run -p si-rs-cli -- version
cargo run -p si-rs-cli -- help --format json
cargo run -p si-rs-cli -- settings show --format json
cargo run -p si-rs-cli -- orbit list --provider github --json
cargo run -p si-rs-cli -- paths show --format json
```

The repository is now Rust-only for build, test, and runtime flows.
