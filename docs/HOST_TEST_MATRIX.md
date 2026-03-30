# Host Test Matrix

This is the strong local validation baseline for the Rust-only `si` chain on a real host.
Run it from the `si` repo root:

```bash
cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-rust-host-matrix --
```

The matrix covers the direct runtime dependency chain:

- `si`
- sibling `fort`
- sibling `surf`

It is intentionally host-shaped instead of unit-test-only. The goal is to catch wrapper, path, process, and repo-to-repo integration regressions that pure package tests can miss.

## Scenario matrix

| Scenario | Command | Expected behavior |
| --- | --- | --- |
| Installer smoke | `cargo run --quiet --locked -p si-rs-cli -- build installer smoke-host` | local installer build/install/uninstall completes successfully |
| SI CLI integration | `cargo test -p si-rs-cli --test cli --quiet` | command-surface and integration tests pass |
| SI vault package | `cargo test -p si-rs-vault --quiet` | vault package tests pass |
| Fort repo validation | `cargo test --quiet --manifest-path ../fort/Cargo.toml` | sibling `fort` workspace tests pass |
| Surf repo validation | `cargo test --workspace --quiet --manifest-path ../surf/Cargo.toml` | sibling `surf` workspace tests pass |
| Live fort wrapper smoke | included in script | `si fort -- --json doctor` reaches a Fort-shaped HTTP stub and returns `health_status=200`, `ready_status=200` |

## Why this exists

- `cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-test-runner -- all` is still the broad local stack.
- This matrix is the narrower Rust-host gate for the operational chain that must work together on a developer machine.
- It verifies behavior that previously regressed in practice:
  - stale `.artifacts` binaries shadowing current Rust source
  - `si fort` failing on hosts without a preinstalled `fort` binary
  - env-sensitive `surf` tests breaking under real host overrides

## Requirements

- `cargo`
- `docker`
- `python3`
- sibling checkouts at `../fort` and `../surf`

## Failure interpretation

- Installer failures usually mean packaging or wrapper issues.
- `si fort` smoke failures usually mean wrapper resolution, repo discovery, or Fort request-path regressions.
- Dependent repo failures should be fixed in the owning repo, then the full matrix rerun.
