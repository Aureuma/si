# SI Distribution Readiness Plan

## Status

In progress.

This ticket tracks the work needed to make SI distributable on fresh Linux and
macOS machines through GitHub Releases, npm, Homebrew, and source install paths.

## Why This Exists

The existing release docs advertise multi-platform archives, but the asset
builder packages host-built Rust binaries under every platform label. That can
publish archives whose filenames say `darwin_arm64` or `linux_arm64` while the
contained binary is actually for the release runner host.

Distribution must be boring and verifiable:

- each archive name must match the contained binary platform and architecture
- release workflows must build binaries on GitHub Actions runners or explicit
  Rust target triples, not through Go-style environment variables
- npm and Homebrew must consume the same verified release archives
- macOS service installs must not depend on an interactive shell environment
- fresh-machine diagnostics must explain missing runtime dependencies

## Scope

In scope:

- Release archive generation
- Release archive verification
- GitHub Actions release workflow
- npm launcher target support and extraction checks
- Homebrew tap formula rendering
- Nucleus service portability on Linux and macOS
- Fresh-machine doctor checks for runtime dependencies
- Documentation updates for supported platforms and release validation

Out of scope for this pass:

- Windows support
- Homebrew bottles
- macOS code signing and notarization automation
- Docker, Kubernetes, or image-based SI runtime distribution

## Platform Decision

Supported binary release targets for this pass:

- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`

`linux/armv7` is removed from advertised binary distribution until it has a
real build runner, target toolchain, and smoke test. Keeping an unsupported
target label is worse than a smaller honest matrix.

## Execution Plan

### 1. Make the Plan Durable

Status: completed.

Steps:

- Add this ticket under `tickets/`.
- Bump the SI patch version in the same commit.
- Keep this ticket updated as implementation lands.

Reason:

The release-hardening work spans CLI code, JavaScript launcher code, docs, and
GitHub Actions. A durable ticket keeps the intent and ordering visible.

### 2. Replace Go-Style Release Target Inputs

Status: completed.

Steps:

- Replace `--goos`, `--goarch`, and `--goarm` with Rust/platform terms.
- Add canonical release target IDs:
  - `linux-amd64`
  - `linux-arm64`
  - `darwin-amd64`
  - `darwin-arm64`
- Map each target ID to:
  - artifact OS label
  - artifact architecture label
  - Rust target triple
- Pass `--target <triple>` to `cargo build`.
- Keep compatibility aliases only where useful, but do not keep Go terminology
  in the primary command surface or docs.

Reason:

Rust release assets must be driven by Rust targets. Go environment variables do
not select Rust compilation targets and made the previous workflow misleading.

### 3. Make Asset Verification Inspect Binaries

Status: completed.

Steps:

- Verify that every expected archive exists and has a checksum entry.
- Verify archive contents still include `si`, `README.md`, and `LICENSE`.
- Extract the `si` binary from each archive during verification.
- Run `file` against the binary when available.
- Reject binaries whose detected format does not match the expected OS and
  architecture.
- Keep verification usable on local machines by making the `file` dependency a
  checked runtime requirement with a clear error.

Reason:

Name and checksum checks prove only packaging integrity. They do not prove the
binary inside the archive can run on the labeled platform.

### 4. Build Release Assets in GitHub Actions by Platform

Status: pending.

Steps:

- Change `.github/workflows/cli-release-assets.yml` to use a build matrix.
- Build Linux AMD64 on Ubuntu AMD64.
- Build Linux ARM64 on an ARM64 Ubuntu runner.
- Build macOS ARM64 on macOS ARM64.
- Build macOS AMD64 on macOS with the x86_64 Rust target installed.
- Upload per-target artifacts from build jobs.
- Add a package job that downloads all target archives, writes `checksums.txt`,
  and runs `si build self verify`.
- Keep npm publish and Homebrew tap update downstream of verified release
  archives.

Reason:

GitHub Actions is the right place to build distributable binaries. CI runner
selection and target triples make the binary provenance explicit.

### 5. Update npm and Homebrew Consumers

Status: pending.

Steps:

- Remove npm Linux ARMv7 resolution.
- Keep npm support for macOS x64/arm64 and Linux x64/arm64.
- Keep npm checksum verification.
- Improve npm first-run failure messages for missing `tar`.
- Update Homebrew tap formula generation to use only the supported target set.
- Refresh stale Homebrew core metadata or mark it as generated legacy metadata.

Reason:

Installers should only reference artifacts that the release workflow really
builds and verifies.

### 6. Harden Nucleus Service Portability

Status: pending.

Steps:

- Include deterministic service environment values for:
  - `PATH`
  - `SI_NUCLEUS_AUTH_TOKEN`
  - `SI_NUCLEUS_PUBLIC_URL`
- Preserve `SI_NUCLEUS_BIND_ADDR` and state-dir behavior through explicit
  service arguments.
- Document that service install records the current `si` executable path.
- Add a warning or status note when the service points at a versioned npm cache
  binary.

Reason:

macOS launchd and Linux user services do not run under the same environment as
an interactive shell. A fresh machine needs service definitions that find
`codex` and preserve the intended Nucleus public URL.

### 7. Add Fresh-Machine Doctor Checks

Status: pending.

Steps:

- Add a focused distribution doctor command.
- Check the current SI binary path and version.
- Check required commands by feature:
  - `codex` for Nucleus workers
  - `tmux` for operator Codex sessions
  - `tar` for npm launcher extraction
  - `git`, `cargo`, and `rustc` for source install
  - `brew` for Homebrew install validation on macOS
- Print text and JSON output.
- Keep checks non-invasive: no auth prompts, no writes, no network calls.

Reason:

Another machine can have the binary installed but still fail at runtime because
its worker dependencies or shell-independent service environment are missing.

### 8. Update Docs and Tests

Status: pending.

Steps:

- Update README and release docs to the honest supported target set.
- Update npm package README.
- Update command help tests.
- Update release asset tests.
- Add tests for target mapping and binary verification behavior where practical.
- Run focused tests before each commit and broader validation at the end.

Reason:

Distribution behavior is only useful if docs, help output, tests, and workflows
describe the same contract.

## Completion Criteria

- No primary release command or docs use Go-style target naming.
- Release archives are built by explicit Rust target triples.
- Release verification rejects mislabeled binaries.
- GitHub Actions builds and packages release assets by target.
- npm and Homebrew reference only verified supported artifacts.
- Nucleus service install is shell-environment aware.
- A fresh-machine doctor command exists.
- Relevant docs and tests are updated.
- All implementation commits include patch version bumps.
