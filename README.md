# ⚛️ si

<p align="center">
  <img src="assets/images/si-hero.png" alt="si hero illustration" />
</p>

<p align="center">
  <a href="https://img.shields.io/badge/license-AGPL--3.0-0f766e?style=for-the-badge"><img src="https://img.shields.io/badge/license-AGPL--3.0-0f766e?style=for-the-badge" alt="License: AGPL-3.0"></a>
  <a href="https://img.shields.io/badge/rust-1.86-000000?logo=rust&logoColor=white&style=for-the-badge"><img src="https://img.shields.io/badge/rust-1.86-000000?logo=rust&logoColor=white&style=for-the-badge" alt="Rust 1.86"></a>
  <a href="https://img.shields.io/badge/docker-required-2496ED?logo=docker&logoColor=white&style=for-the-badge"><img src="https://img.shields.io/badge/docker-required-2496ED?logo=docker&logoColor=white&style=for-the-badge" alt="Docker required"></a>
  <a href="https://img.shields.io/badge/docs-mintlify-0f766e?style=for-the-badge"><img src="https://img.shields.io/badge/docs-mintlify-0f766e?style=for-the-badge" alt="Docs: Mintlify"></a>
  <a href="https://www.npmjs.com/package/@aureuma/si"><img src="https://img.shields.io/npm/v/%40aureuma%2Fsi?logo=npm&logoColor=white&style=for-the-badge" alt="npm: @aureuma/si"></a>
  <a href="https://github.com/Aureuma/homebrew-si"><img src="https://img.shields.io/badge/homebrew-aureuma%2Fsi%2Fsi-fbbf24?logo=homebrew&logoColor=black&style=for-the-badge" alt="Homebrew Formula: aureuma/si/si"></a>
</p>

`si` is an AI-first CLI for orchestrating coding agents, provider bridges, and secure runtime workflows.

Quick links: [`docs/index.mdx`](docs/index.mdx) · [`docs/CLI_REFERENCE.md`](docs/CLI_REFERENCE.md) · [`docs/VAULT.md`](docs/VAULT.md) · [`docs/RELEASING.md`](docs/RELEASING.md)

## What si covers

- Dyads: actor + critic paired containers, closed-loop execution, status/log/exec workflows.
- Codex containers: profile-scoped lifecycle under `si codex` (`profile`, `spawn`, `status`, `exec`, `report`, `clone`, `remove`, `respawn`).
- Vault: encrypted dotenv workflows with trust/recipient checks and secure command injection.
- Provider bridges: Stripe, GitHub, Cloudflare, Google (Places/Play/YouTube), Apple, Social, WorkOS, AWS, GCP, OpenAI, OCI.
- Orbitals: namespaced integration catalog + install/enable/doctor lifecycle (`si orbits ...`).
- Browser runtime: Dockerized Playwright MCP runtime (`si browser ...`).
- Docs workflow: Mintlify wrapper (`si mintlify ...`) to bootstrap and maintain docs locally.

## Repo layout

- `rust/`: primary Rust workspace and shipping CLI implementation.
- `tools/si-browser`: browser runtime Docker assets.
- `tools/si-image`: unified runtime image used by codex and dyad containers.
- `docs/`: Markdown + Mintlify docs content.
- `agents/`: dyad runtime components.

## Install

Use one of these install paths:

```bash
# npm (global launcher package)
npm install -g @aureuma/si

# Homebrew
brew install aureuma/si/si
```

Homebrew uses `user/repo/formula` for external taps, so `brew install aureuma/si` is not a valid formula path.

Direct installer script remains available:

```bash
curl -fsSL https://raw.githubusercontent.com/Aureuma/si/main/tools/install-si.sh | bash
```

## Quickstart

Prerequisites:

- Docker Engine available on host.
- Rust 1.86 toolchain for local source builds.
- `si-rs` is the runtime entrypoint.

Build local CLI + runtime image:

```bash
# host build (Rust-primary CLI)
cd /path/to/si
cargo build --release --locked --bin si-rs

# runtime image for dyads/codex
./.artifacts/cargo-target/release/si-rs build image
```

## Common workflows

Dyad lifecycle:

```bash
./si dyad spawn start --name <name> --workspace "$PWD"
./si dyad status <name>
./si dyad logs <name>
./si dyad exec --member actor <name> -- bash
./si dyad remove <name>
```

Browser runtime:

```bash
./si browser build
./si browser start
./si browser status
./si browser logs --follow
./si browser stop
```

When running, SI-managed codex and dyad containers auto-register MCP server `si_browser`
to the browser runtime endpoint on the shared Docker network.

Mintlify docs tooling:

```bash
./si mintlify init --repo . --docs-dir docs --site-url https://docs.si.aureuma.ai --force
./si mintlify validate
./si mintlify dev
```

## Command map

- `si dyad ...` / `si codex ...`: agent runtime operations.
- `si vault ...`: secure secret workflows.
- `si providers ...`: provider characteristics + health surfaces.
- `si orbits ...`: Orbitals and integration onboarding.
- `si browser ...`: Playwright MCP browser runtime.
- `si mintlify ...`: docs site bootstrap/validation/dev wrappers.
- `si build ...`: local image + self-build workflows.

Full command surface: run `si --help` and command-specific help (`si <command> --help`).

## Testing and quality

Run module tests:

```bash
./tools/test.sh
```

Run the staged Rust workspace checks:

```bash
cargo fmt --check
cargo clippy --workspace --all-targets -- -D warnings
cargo test --workspace
```

Run installer smoke tests:

```bash
./tools/test-install-si.sh
./tools/test-install-si-docker.sh
```

Run strict vault-focused tests:

```bash
./tools/test-vault.sh
```

Run the full local test stack in one command:

```bash
./tools/test-all.sh
```

Run the Rust host matrix for the direct `si`/`fort`/`surf` chain:

```bash
./tools/test-rust-host-matrix.sh
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
- `tools/release/npm/publish-npm-from-vault.sh -- --version vX.Y.Z` (vault key: `NPM_GAT_AUREUMA_VANGUARDA`)

## License

This repository is licensed under GNU Affero General Public License v3.0 (AGPL-3.0).
See [`LICENSE`](LICENSE).
