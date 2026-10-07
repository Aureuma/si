# Testing

## Rust workspace layout
This repo is Rust-only for build, test, and runtime flows.
Run commands from the repo root so the workspace `Cargo.toml` and Rust helper binaries resolve correctly.

## Running tests
Use the repo test runner from the root:

```bash
cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-test-runner -- workspace
```

That runner executes `cargo test --workspace`.
No secondary language toolchain is required.
Use `cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-test-runner -- workspace --help` for a quick usage reminder.
Use `cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-test-runner -- workspace --list` to print the active test lane without running it.

For one-command local coverage of the standard test stack, run:

```bash
cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-test-runner -- all
```

Scenario coverage for the remaining suites is documented in [HOST_TEST_MATRIX.md](./HOST_TEST_MATRIX.md).

## tmux isolation in tests
Tests that spawn tmux must never address the developer's live server.
On 2026-10-07 a test's bare `tmux kill-server` destroyed the developer's
sessions because `TMUX` inherited from the surrounding pane takes precedence
over `TMUX_TMPDIR` — the "isolated" server was never isolated.

Rules:
- Always pass `-S <owned socket>` (usually `<home>/.si/runtime/tmux.sock`).
  `TMUX_TMPDIR` alone does not isolate: any inherited `TMUX` overrides it.
- Never run bare `tmux new-session`, `kill-session`, or `kill-server` in a
  test. `kill-server` is only acceptable with `-S` against an owned socket,
  and owned-server cleanup belongs in `TmuxGuard`.
- Use the `tmux_ok`/`tmux_command` helpers in `si-cli/tests/cli.rs`, which
  scrub `TMUX` from the child environment. Do not add new raw
  `Command::new("tmux")` call sites.
- Production `legacy_default_server_workers` intentionally reads the ambient
  server (read-only `ls`) to find pre-migration workers; tests must not
  depend on ambient-server state. Helper isolation is locked by
  `tmux_helper_ignores_inherited_tmux_env`.

## Orbit validation

Third-party Orbit integration coverage moved to the standalone `Aureuma/orbit` repo. Run `cargo test --workspace` from that repo for provider integration tests.

## Installer smoke tests
To validate the `si` installer script end-to-end, run:

```bash
cargo run --quiet --locked -p si-tools --bin si-release -- installer host
```

To validate the pnpm launcher package end-to-end, run:

```bash
cargo run --quiet --locked -p si-tools --bin si-release -- installer npm
```

To validate the Homebrew tap install path end-to-end, run:

```bash
cargo run --quiet --locked -p si-tools --bin si-release -- installer homebrew
```

## CI notes
GitHub Actions workflows use docs-only change detection to skip heavy test jobs
when only docs/markdown files are modified.

## CLI help smoke checks
After CLI command-surface changes, run targeted help checks:

```bash
./si --help
./si codex --help
./si muse --help
```

## Codex upgrade compatibility check
Run the Codex-facing test suites directly from the repo root:

```bash
cargo test -p si-codex
cargo test -p si-tools
```

Use these suites as the compatibility gate before upgrading Codex-facing flows.

Manual runtime smoke:

```bash
si codex spawn --profile <profile> --slot primary --workspace "$PWD"
si codex spawn --profile <profile> --slot review --workspace "$PWD"
si codex list
si codex shell --profile <profile> --slot primary -- env | rg 'CODEX_HOME|SI_CODEX_WORKER_SLOT'
si codex shell --profile <profile> --slot review -- env | rg 'CODEX_HOME|SI_CODEX_WORKER_SLOT'
si codex stop --profile <profile> --slot review
si codex remove --profile <profile> --slot review
```

## Fresh-machine doctor
Run the non-invasive distribution doctor after installing SI on another machine:

```bash
si doctor
si doctor --format json
```

The doctor checks the installed SI binary path and the commands needed by common
runtime paths, including Codex workers, tmux operator sessions, npm archive
extraction, and source installs. It does not prompt for auth, write state, or
make network calls.
