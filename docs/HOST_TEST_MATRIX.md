# Host Test Matrix

This is the strong local validation baseline for the Rust-only `si` chain on a real host.
Run it from the `si` repo root:

```bash
cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-test-runner -- all
```

The matrix covers the direct runtime dependency chain:

- `si` (Codex and Muse multi-account workers)
- release/installer packaging via the dev-only `si-release` tool

It is intentionally host-shaped instead of unit-test-only. The goal is to catch path, process, and packaging regressions that pure package tests can miss.

## Scenario matrix

| Scenario | Command | Expected behavior |
| --- | --- | --- |
| Rust workspace | `cargo test --workspace` | all package tests pass |
| Installer smoke | `cargo run --quiet --locked -p si-tools --bin si-release -- installer host` | local installer build/install/uninstall completes successfully |
| npm smoke | `cargo run --quiet --locked -p si-tools --bin si-release -- installer npm` | launcher package smoke passes |
| SI CLI integration | `cargo test -p si-cli --test cli` | command-surface and integration tests pass |
| Distribution doctor | `si doctor` | prerequisites report healthy |

## Why this exists

- `si-test-runner -- all` is the broad local stack.
- This matrix is the narrower Rust-host gate for the operational chain that must work together on a developer machine.
- It verifies behavior that previously regressed in practice:
  - stale `.artifacts` binaries shadowing current Rust source
  - installer/packaging drift against the workspace version

## Requirements

- `cargo`
- `tmux` (for worker lifecycle tests)

## Failure interpretation

- Installer failures usually mean packaging issues.
- Worker lifecycle failures usually mean tmux socket, lease, or native-adapter regressions.
