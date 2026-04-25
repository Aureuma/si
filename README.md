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

`si` is an AI-first CLI for orchestrating coding agents, provider bridges, and secure runtime workflows.

Quick links: [`docs/index.mdx`](docs/index.mdx) · [`docs/NUCLEUS.md`](docs/NUCLEUS.md) · [`docs/CLI_REFERENCE.md`](docs/CLI_REFERENCE.md) · [`docs/VAULT.md`](docs/VAULT.md) · [`docs/RELEASING.md`](docs/RELEASING.md)

## What si covers

- Nucleus control plane: durable local orchestration under `si nucleus ...` for tasks, workers, sessions, runs, the local WebSocket gateway, and OS-native service management.
- Codex workers: profile-scoped tmux/App Server lifecycle under `si codex` (`profile`, `spawn`, `shell`, `tail`, `list`, `remove`, `respawn`, `tmux`, `warmup`).
- Vault: encrypted dotenv workflows with trust/recipient checks and secure command injection.
- Provider orbits: first-party integrations under `si orbit <provider> ...` for Stripe, GitHub, Cloudflare, Google (Places/Play/YouTube), Apple, WorkOS, AWS, GCP, OpenAI, and OCI.
- Browser runtime: local Playwright browser runtime under `si surf ...`, including optional Fort-backed injection for a stable noVNC viewer password on `si surf start`.
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

Direct source install remains available and installs the `si` launcher on this host:

```bash
cargo run --quiet --locked -p si-rs-cli -- build installer run --force
```

## Quickstart

Prerequisites:

- Rust `1.88.0` for local source builds (see `rust-toolchain.toml`).
- Installed `si` for normal usage, or `target/release/si-rs` if you want to run the just-built binary directly from source.

Build the local source binary:

```bash
cd /path/to/si
cargo build --release --locked --bin si-rs
```

Run the built binary directly without installing it:

```bash
target/release/si-rs --help
```

Fast local iteration:

```bash
si build self check --timings
si build self --timings
```

## Common workflows

Nucleus control plane:

```bash
si nucleus status
si nucleus task create "Investigate release drift" "Summarize the last failed release attempt."
si nucleus task list
si nucleus service install
si nucleus service start
```

Codex lifecycle:

```bash
si codex spawn --profile <profile> --workspace "$PWD"
si codex list
si codex shell --profile <profile> -- bash
si codex tail --profile <profile>
si codex remove --profile <profile>
```

Browser runtime:

```bash
si surf build
si surf start --profile default
si surf status
si surf logs
si surf stop
```

If you want a stable noVNC viewer password without storing it in `~/.si/surf/settings.toml`,
set the wrapper source in `~/.si/settings.toml` and let `si surf start` fetch it from Fort:

```toml
[surf]
vnc_password_fort_key = "SURF_VNC_PASSWORD"
vnc_password_fort_repo = "surf"
vnc_password_fort_env = "dev"
```

When the Fort-backed wrapper path is configured, keep `browser.vnc_password` empty in the Surf
runtime settings so the viewer secret only enters the container at start time.

By default, `si nucleus ...` discovers the local gateway via `SI_NUCLEUS_WS_ADDR`,
then `~/.si/nucleus/gateway/metadata.json`, then `ws://127.0.0.1:4747/ws`.

Mintlify docs tooling:

```bash
si mintlify init --repo . --docs-dir docs --site-url https://docs.si.aureuma.ai --force
si mintlify validate
si mintlify dev
```

## Command map

- `si nucleus ...`: local control-plane operations, gateway-facing orchestration, and service management.
- `si codex ...`: agent runtime operations.
- `si vault ...`: secure secret workflows.
- `si orbit ...`: provider bridges and provider capability inventory.
- `si surf ...`: Playwright browser runtime.
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
si analyze
```

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
- `./.artifacts/cargo-target/release/si-rs build self assets --out-dir .artifacts/release-preflight`
- `./.artifacts/cargo-target/release/si-rs build npm vault` (vault key: `NPM_GAT_AUREUMA_VANGUARDA`)

These commands default to the current SI workspace version from root `Cargo.toml`; pass `--version` only when you intentionally need a detached tag/version target.

ReleaseMind automation stays in the ReleaseMind repo and API. SI consumes it as
an orbit client:

```bash
si orbit releasemind auth login
si orbit releasemind repo ensure-link Aureuma/si --json
si orbit releasemind token list --json
si orbit releasemind release create vX.Y.0 --draft --json
si orbit releasemind release view post_123 --json
si orbit releasemind release publish post_123 --json
si orbit releasemind play plan -R Aureuma/si --base-tag vX.Y.0 --json
```

Use `si orbit releasemind auth login` for interactive operator work. Use
dashboard-linked automation tokens only for CI or unattended flows, and inject
`RELEASEMIND_AUTOMATION_TOKEN` with `si fort` when you need those lower-level
automation endpoints. The repo, release, and play commands infer `owner/repo` from the current
Git checkout when possible, so `--repo` is usually unnecessary.

## License

This repository is licensed under GNU Affero General Public License v3.0 (AGPL-3.0).
See [`LICENSE`](LICENSE).
