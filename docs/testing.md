# Testing

## Rust workspace layout
This repo is Rust-only for build, test, and runtime flows.
Run commands from the repo root so the workspace `Cargo.toml` and helper scripts resolve correctly.

## Running tests
Use the repo test runner from the root:

```bash
si test workspace
# compatibility alias:
./tools/test.sh
```

That runner executes `cargo test --workspace`.
No secondary language toolchain is required.
Use `./tools/test.sh --help` for a quick usage reminder.
Use `./tools/test.sh --list` to print the active test lane without running it.

For one-command local coverage of the standard test stack, run:

```bash
si test all
# compatibility alias:
./tools/test-all.sh
```

For the direct Rust host matrix across `si`, sibling `fort`, and sibling `surf`, run:

```bash
./tools/test-rust-host-matrix.sh
```

That matrix is documented in [HOST_TEST_MATRIX.md](./HOST_TEST_MATRIX.md) and is the best local gate after wrapper/runtime changes.

## Orbit runner matrix
For orbit-system specific regression lanes, use:

```bash
si test orbits unit
si test orbits policy
si test orbits catalog
si test orbits e2e
# compatibility aliases:
./tools/test-runners/orbits-unit.sh
./tools/test-runners/orbits-policy.sh
./tools/test-runners/orbits-catalog.sh
./tools/test-runners/orbits-e2e.sh
```

Run the full orbit runner stack:

```bash
si test orbits all
# compatibility alias:
./tools/test-runners/orbits-all.sh
```

CI coverage for these lanes is defined in:

```bash
.github/workflows/orbits-runners.yml
```

## Installer smoke tests
To validate the `si` installer script end-to-end, run:

```bash
./tools/test-install-si.sh
```

Use `./tools/test-install-si.sh --help` for a quick usage reminder.

To validate the npm launcher package end-to-end, run:

```bash
./tools/test-install-si-npm.sh
```

To validate the Homebrew tap install path end-to-end, run:

```bash
./tools/test-install-si-homebrew.sh
```

For containerized smoke coverage (root + non-root installer paths), run:

```bash
./tools/test-install-si-docker.sh
```

Use `SI_INSTALL_SMOKE_SKIP_NONROOT=1 ./tools/test-install-si-docker.sh` to skip
the non-root leg during local iteration.

## Vault strict suite
Run the dedicated vault suite:

```bash
si test vault
# compatibility alias:
./tools/test-vault.sh
```

Compatibility flag:

```bash
si test vault --quick
# compatibility alias:
./tools/test-vault.sh --quick
```

`--quick` is retained as a compatibility no-op; the Rust vault lane already runs as a single package suite.

## Fort spawn/respawn security matrix
Run the Fort integration matrix:

```bash
./tools/test-fort-spawn-matrix.sh
```

This matrix validates:
- profile-scoped Fort agent auth bootstrap in `si spawn`
- hosted Fort endpoint flow (configured via `~/.si/fort/settings.toml` `[fort].host`) as the default runtime target
- host-side bootstrap admin token resolved from `FORT_BOOTSTRAP_TOKEN_FILE` (default `~/.si/fort/bootstrap/admin.token`)
- runtime token-path flow in containers via `FORT_TOKEN_PATH` + `FORT_REFRESH_TOKEN_PATH`
- in-container access through `si run` with no `FORT_TOKEN`/`FORT_REFRESH_TOKEN` secret env leakage
- strict token file modes/ownership (`0600` files, `0700` fort state dir)
- policy allow/deny behavior across multiple profiles and repo/env bindings
- `si respawn --volumes` auth continuity
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
$FORT_TOKEN_PATH
$FORT_REFRESH_TOKEN_PATH

stat -c "%a %n" "$FORT_TOKEN_PATH" "$FORT_REFRESH_TOKEN_PATH"
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
./si gcp gemini image generate --help
./si surf --help
```

## Image build smoke check
`si build image` runs a Codex compatibility preflight before building the image.

Run the preflight directly:

```bash
./tools/si-image/preflight-codex-upgrade.sh
```

Skip preflight only when you explicitly need a fast local image iteration:

```bash
./si build image --skip-preflight
```

The canonical local image build command is:

```bash
./si build image
```

Build mode behavior:
- If `docker buildx` is available, SI runs `docker buildx build --load` directly.
- If `docker buildx` is unavailable or probe fails, SI uses classic `docker build`.
- SI no longer retries/falls back mid-build after a buildx start; mode is selected once up front.
