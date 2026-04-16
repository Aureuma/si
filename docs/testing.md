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

For the direct Rust host matrix across `si`, sibling `fort`, and sibling `surf`, run:

```bash
cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-rust-host-matrix --
```

That matrix is documented in [HOST_TEST_MATRIX.md](./HOST_TEST_MATRIX.md) and is the best local gate after wrapper/runtime changes.

## Provider orbit validation

Provider-orbit coverage now lives in the normal Rust CLI and provider test suites.

Use focused command tests such as:

```bash
cargo test -p si-rs-cli orbit
cargo test -p si-rs-provider-github
cargo test -p si-rs-provider-aws
```

## Installer smoke tests
To validate the `si` installer script end-to-end, run:

```bash
cargo run --quiet --locked -p si-rs-cli -- build installer smoke-host
```

Use `si build installer host --help` for a quick usage reminder.

To validate the npm launcher package end-to-end, run:

```bash
cargo run --quiet --locked -p si-rs-cli -- build installer smoke-npm
```

To validate the Homebrew tap install path end-to-end, run:

```bash
cargo run --quiet --locked -p si-rs-cli -- build installer smoke-homebrew
```

## Vault strict suite
Run the dedicated vault suite:

```bash
cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-test-runner -- vault
```

Compatibility flag:

```bash
cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-test-runner -- vault --quick
```

`--quick` is retained as a compatibility no-op; the Rust vault lane already runs as a single package suite.

## Fort codex runtime security matrix
Run the Fort integration matrix:

```bash
cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin test-fort-spawn-matrix --
```

This matrix validates:
- profile-scoped Fort agent auth bootstrap in `si codex spawn`
- hosted Fort endpoint flow (configured via `~/.si/fort/settings.toml` `[fort].host`) as the default runtime target
- host-side bootstrap admin token files are used for provisioning/admin flows only
- runtime token-path flow remains file-backed under `CODEX_HOME/fort/`; use `si codex shell <profile> -- si fort ...` for profile runtime auth
- runtime secret commands fail loudly when profile-scoped Fort token files are missing or cannot refresh
- worker-shell access through `si codex shell` with no `FORT_TOKEN`/`FORT_REFRESH_TOKEN` secret env leakage
- strict token file modes/ownership (`0600` files, `0700` fort state dir)
- policy allow/deny behavior across multiple profiles and repo/env bindings
- `si codex respawn` auth continuity
- ciphertext-at-rest plus manual ECIES decrypt parity with `fort get`

For local-only integration harnesses that use HTTP Fort endpoints, set:

```bash
SI_FORT_ALLOW_INSECURE_HOST=1
```

Bootstrap token file requirements:

```bash
~/.si/fort/bootstrap/admin.token

chmod 600 ~/.si/fort/bootstrap/admin.token
chmod 700 ~/.si/fort/bootstrap
```

Runtime session token file requirements:

```bash
/path/to/access.token
/path/to/refresh.token

stat -c "%a %n" /path/to/access.token /path/to/refresh.token
```

Wrapper reminder:
- `si fort` is a wrapper around `fort`.
- If `fort` is not already on `PATH`, the wrapper can build and run the sibling `../fort` checkout when build fallback is allowed.
- If a flag belongs to `fort` itself, pass it after `--` (for example: `si fort -- --host https://fort.aureuma.ai doctor`).

## CI notes
GitHub Actions workflows use docs-only change detection to skip heavy test jobs
when only docs/markdown files are modified.

## Static analysis
Run static analysis from the repo root:

```bash
./si analyze
```

Use non-failing mode for local iteration while keeping CI strict with default `./si analyze`:

```bash
./si analyze --no-fail
```

## CLI help smoke checks
After CLI command-surface changes, run targeted help checks:

```bash
./si --help
./si mintlify --help
./si orbit gcp gemini image generate --help
./si surf --help
```

## Codex upgrade compatibility check
Run the Codex-facing test suites directly from the repo root:

```bash
cargo test -p si-rs-codex
cargo test -p si-tools
```

Use these suites as the compatibility gate before upgrading Codex-facing flows.
