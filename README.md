# ⚛️ si

<p align="center">
  <img src="assets/images/si-hero.png" alt="si hero illustration" />
</p>

<p align="center">
  <a href="https://img.shields.io/badge/license-AGPL--3.0-0f766e?style=for-the-badge"><img src="https://img.shields.io/badge/license-AGPL--3.0-0f766e?style=for-the-badge" alt="License: AGPL-3.0"></a>
  <a href="https://img.shields.io/badge/rust-1.86-000000?logo=rust&logoColor=white&style=for-the-badge"><img src="https://img.shields.io/badge/rust-1.86-000000?logo=rust&logoColor=white&style=for-the-badge" alt="Rust 1.86"></a>
  <a href="https://img.shields.io/badge/docs-mintlify-0f766e?style=for-the-badge"><img src="https://img.shields.io/badge/docs-mintlify-0f766e?style=for-the-badge" alt="Docs: Mintlify"></a>
  <a href="https://www.npmjs.com/package/@aureuma/si"><img src="https://img.shields.io/npm/v/%40aureuma%2Fsi?logo=npm&logoColor=white&style=for-the-badge" alt="npm: @aureuma/si"></a>
  <a href="https://github.com/Aureuma/homebrew-si"><img src="https://img.shields.io/badge/homebrew-aureuma%2Fsi%2Fsi-fbbf24?logo=homebrew&logoColor=black&style=for-the-badge" alt="Homebrew Formula: aureuma/si/si"></a>
</p>

`si` is an AI-first CLI for orchestrating coding agents, provider bridges, and secure runtime workflows.

Quick links: [`docs/index.mdx`](docs/index.mdx) · [`docs/CLI_REFERENCE.md`](docs/CLI_REFERENCE.md) · [`docs/VAULT.md`](docs/VAULT.md) · [`docs/RELEASING.md`](docs/RELEASING.md)

## What si covers

- Codex workers: profile-scoped tmux/App Server lifecycle under `si codex` (`profile`, `spawn`, `shell`, `tail`, `list`, `remove`, `respawn`, `tmux`, `warmup`).
- Vault: encrypted dotenv workflows with trust/recipient checks and secure command injection.
- Provider orbits: first-party integrations under `si orbit <provider> ...` for Stripe, GitHub, Cloudflare, Google (Places/Play/YouTube), Apple, WorkOS, AWS, GCP, OpenAI, and OCI.
- Browser runtime: local Playwright MCP runtime (`si browser ...`).
- Docs workflow: Mintlify wrapper (`si mintlify ...`) to bootstrap and maintain docs locally.

## Repo layout

- `rust/`: primary Rust workspace and shipping CLI implementation.
- `tools/si-browser`: browser runtime helpers.
- `docs/`: Markdown + Mintlify docs content.

## Install

Use one of these install paths:

```bash
# npm (global launcher package)
npm install -g @aureuma/si

# Homebrew
brew install aureuma/si/si
```

Homebrew uses `user/repo/formula` for external taps, so `brew install aureuma/si` is not a valid formula path.

Direct source install remains available:

```bash
cargo run --quiet --locked -p si-rs-cli -- build installer run --force
```

## Quickstart

Prerequisites:

- Latest stable Rust toolchain for local source builds.
- `si-rs` is the runtime entrypoint.

Build local CLI:

```bash
cd /path/to/si
cargo build --release --locked --bin si-rs
```

Fast local iteration:

```bash
si build self check --timings
si build self --timings
```

## Common workflows

Codex lifecycle:

```bash
./si codex spawn --profile <profile> --workspace "$PWD"
./si codex list
./si codex shell --profile <profile> -- bash
./si codex tail --profile <profile>
./si codex remove --profile <profile>
```

Browser runtime:

```bash
./si browser build
./si browser start
./si browser status
./si browser logs --follow
./si browser stop
```

When running, SI-managed codex workers can auto-register MCP server `si_browser`
to the configured browser runtime endpoint.

Mintlify docs tooling:

```bash
./si mintlify init --repo . --docs-dir docs --site-url https://docs.si.aureuma.ai --force
./si mintlify validate
./si mintlify dev
```

## Command map

- `si codex ...`: agent runtime operations.
- `si vault ...`: secure secret workflows.
- `si orbit ...`: provider bridges and provider capability inventory.
- `si browser ...`: Playwright MCP browser runtime.
- `si mintlify ...`: docs site bootstrap/validation/dev wrappers.
- `si build ...`: self-build and release workflows.

Full command surface: run `si --help` and command-specific help (`si <command> --help`).

## Testing and quality

Run module tests:

```bash
cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-test-runner -- workspace
```

Run the staged Rust workspace checks:

```bash
cargo fmt --check
cargo clippy --workspace --all-targets -- -D warnings
cargo test --workspace
```

Run installer smoke tests:

```bash
cargo run --quiet --locked -p si-rs-cli -- build installer smoke-host
```

Run strict vault-focused tests:

```bash
cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-test-runner -- vault
```

Run the full local test stack in one command:

```bash
cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-test-runner -- all
```

Run the Rust host matrix for the direct `si`/`fort`/`surf` chain:

```bash
cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-rust-host-matrix --
```

Scenario coverage and expected behavior are documented in [`docs/HOST_TEST_MATRIX.md`](docs/HOST_TEST_MATRIX.md).

Run static analysis:

```bash
./si analyze
```

## Releases

Release process and runbook:

- [`docs/RELEASING.md`](docs/RELEASING.md)
- [`docs/RELEASE_RUNBOOK.md`](docs/RELEASE_RUNBOOK.md)
- [`CHANGELOG.md`](CHANGELOG.md)

Published GitHub Releases automatically include multi-arch CLI archives for:
- Linux (`amd64`, `arm64`, `armv7`)
- macOS (`amd64`, `arm64`)

Local preflight command:
- `./.artifacts/cargo-target/release/si-rs build self assets --version vX.Y.Z --out-dir .artifacts/release-preflight`
- `./.artifacts/cargo-target/release/si-rs build npm vault --version vX.Y.Z` (vault key: `NPM_GAT_AUREUMA_VANGUARDA`)

## License

This repository is licensed under GNU Affero General Public License v3.0 (AGPL-3.0).
See [`LICENSE`](LICENSE).
