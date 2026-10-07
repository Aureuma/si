# ⚛️ si

<p align="center">
  <img src="assets/images/si-hero.png" alt="si hero illustration" />
</p>

<p align="center">
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-AGPL--3.0-0f766e?style=for-the-badge" alt="License: AGPL-3.0"></a>
  <a href="./docs/index.mdx"><img src="https://img.shields.io/badge/docs-reference-0f766e?style=for-the-badge" alt="Docs Reference"></a>
  <a href="https://github.com/Aureuma/si/releases"><img src="https://img.shields.io/github/v/release/Aureuma/si?display_name=tag&style=for-the-badge" alt="GitHub Release"></a>
  <a href="https://www.npmjs.com/package/@aureuma/si"><img src="https://img.shields.io/npm/v/%40aureuma%2Fsi?logo=npm&logoColor=white&style=for-the-badge" alt="npm: @aureuma/si"></a>
  <a href="https://github.com/Aureuma/homebrew-si"><img src="https://img.shields.io/badge/homebrew-aureuma%2Fsi%2Fsi-fbbf24?logo=homebrew&logoColor=black&style=for-the-badge" alt="Homebrew Formula: aureuma/si/si"></a>
</p>

`si` is an AI-first CLI for orchestrating coding agents across multiple Codex and Muse accounts.

Quick links: [`docs/index.mdx`](docs/index.mdx) · [`docs/CLI_REFERENCE.md`](docs/CLI_REFERENCE.md) · [`docs/RELEASING.md`](docs/RELEASING.md)

## What si covers

- Codex workers: profile-scoped tmux lifecycle under `si codex` (`profile`, `spawn`, `stop`, `remove`, `shell`, `tail`, `list`, `respawn`, `tmux`).
- Muse workers: the same profile-scoped lifecycle under `si muse`.
- Third-party API integrations now live in the standalone `orbit` repo and CLI: `orbit <provider> ...`.
- Docs workflow: Mintlify content under `docs/`, validated with the external Mintlify CLI.

## Repo layout

- `rust/`: primary Rust workspace and shipping CLI implementation.
- `docs/`: Markdown + Mintlify docs content.

## Install

Use one of these install paths:

```bash
# npm (global launcher package)
corepack pnpm install -g @aureuma/si

# Homebrew
brew install aureuma/si/si
```

Homebrew uses `user/repo/formula` for external taps, so `brew install aureuma/si` is not a valid formula path.

Direct source install remains available and installs the `si` launcher on this host:

```bash
cargo run --quiet --locked -p si-tools --bin si-release -- installer run --force
```

## Quickstart

Prerequisites:

- Rust `1.94.0` for local source builds (see `rust-toolchain.toml`).
- Installed `si` for normal usage, or `target/release/si` if you want to run the just-built binary directly from source.

Build the local source binary:

```bash
cd /path/to/si
cargo build --release --locked --bin si
```

Run the built binary directly without installing it:

```bash
target/release/si --help
```

Fast local iteration (rebuilds only when Rust sources change, then execs the fresh binary):

```bash
./tools/si-dev --help
```

## Common workflows

Codex lifecycle:

```bash
si codex spawn --profile <profile> --workspace "$PWD"
si codex spawn --profile <profile> --slot review --workspace "$PWD"
si codex spawn --profile <profile> --slot release --workspace "$PWD"
si codex respawn --profile <profile> --slot review --workspace "$PWD"
si codex list
si codex shell --profile <profile> --slot review -- bash
si codex tail --profile <profile> --slot review
si codex stop --profile <profile> --slot review
si codex remove --profile <profile> --slot review
```

Mintlify docs tooling:

```bash
mintlify validate
mintlify dev
```

## Command map

- `si codex ...`: Codex agent runtime operations.
- `si muse ...`: Muse agent runtime operations.
- `orbit ...`: third-party API integrations in the standalone `Aureuma/orbit` repo.

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
cargo run --quiet --locked -p si-tools --bin si-release -- installer host
```

Run the full local test stack in one command:

```bash
cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-test-runner -- all
```

Scenario coverage and expected behavior are documented in [`docs/HOST_TEST_MATRIX.md`](docs/HOST_TEST_MATRIX.md).

## Releases

Release process:

- [`docs/RELEASING.md`](docs/RELEASING.md)
- [`CHANGELOG.md`](CHANGELOG.md)

Versioning rules:
- SI uses one repo-wide version.
- The canonical hard-coded source is root `Cargo.toml` under `[workspace.package].version`.
- Every commit bumps PATCH in that one place; minor releases reset PATCH to `0` and are the only tagged releases.

Published GitHub Releases automatically include multi-arch CLI archives for:
- Linux (`amd64`, `arm64`)
- macOS (`amd64`, `arm64`)

Local preflight command:
- `cargo run --quiet --locked -p si-tools --bin si-release -- self assets --out-dir .artifacts/release-preflight`
- `cargo run --quiet --locked -p si-tools --bin si-release -- npm package --out-dir .artifacts/release-preflight`

These commands default to the current SI workspace version from root `Cargo.toml`; pass `--version` only when you intentionally need a detached tag/version target.

## License

This repository is licensed under GNU Affero General Public License v3.0 (AGPL-3.0).
See [`LICENSE`](LICENSE).
