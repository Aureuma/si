# Rust Transition Workspace

This workspace is the staged Rust rewrite for `si`.

Current scope:

- shared version metadata
- `.si` path defaults and staged modular settings parsing
- runtime path resolution for workspace/config discovery
- typed process execution foundations with capture, cwd/env overrides, and timeout handling
- typed Docker run specs with early bind-mount validation
- shared runtime core-mount planning for codex/dyad containers
- initial codex spawn planning for names, volumes, env, workdir, and mount assembly
- initial dyad spawn planning for actor/critic names, env, labels, volumes, configs mount, and core mount assembly
- Rust CLI exposure for codex spawn planning via `si-rs codex spawn-plan`
- Rust CLI exposure for dyad spawn planning via `si-rs dyad spawn-plan`
- the primary Rust CLI entrypoint used for local and shipped runtime flows

Current entrypoint:

```bash
cargo run -p si-rs-cli -- version
cargo run -p si-rs-cli -- help --format json
cargo run -p si-rs-cli -- settings show --format json
cargo run -p si-rs-cli -- providers characteristics --provider github --format json
cargo run -p si-rs-cli -- paths show --format json
cargo run -p si-rs-cli -- dyad spawn-plan --name alpha --workspace "$PWD" --format json
```

The repository now treats Rust as the primary CLI/runtime path. The remaining Go code exists as compatibility and helper surface while the last subsystem internals are retired according to `tickets/2026-03-15-si-rust-transition-plan.md`.
