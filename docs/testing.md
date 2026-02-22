# Testing

## Go workspace
This repo uses a `go.work` workspace that aggregates the modules under `agents/` and `tools/`.
Running `go test ./...` from the repo root will fail because the root directory is not itself a module.

## Running tests
Use the repo test runner from the root:

```bash
./tools/test.sh
```

That script runs `go test` across the workspace modules listed in `go.work`.
Make sure the Go toolchain is installed and on your `PATH` before running tests.
The script expects to be run from the repo root so it can find `go.work` and will
error with a short message if prerequisites are missing.
Use `./tools/test.sh --help` for a quick usage reminder.
Use `./tools/test.sh --list` to print the module list without running tests.
Use `SI_GO_TEST_TIMEOUT=20m ./tools/test.sh` to adjust the go-test timeout when needed.

For one-command local coverage of the standard test stack, run:

```bash
./tools/test-all.sh
```

## Plugin runner matrix
For plugin-system specific regression lanes (inspired by OpenClaw's segmented CI), use:

```bash
./tools/test-runners/plugins-unit.sh
./tools/test-runners/plugins-policy.sh
./tools/test-runners/plugins-catalog.sh
./tools/test-runners/plugins-e2e.sh
```

Run the full plugin runner stack:

```bash
./tools/test-runners/plugins-all.sh
```

CI coverage for these lanes is defined in:

```bash
.github/workflows/plugins-runners.yml
```

## Installer smoke tests
To validate the `si` installer script end-to-end, run:

```bash
./tools/test-install-si.sh
```

Use `./tools/test-install-si.sh --help` for a quick usage reminder.

## Vault strict suite
Run the dedicated strict vault suite:

```bash
./tools/test-vault.sh
```

Quick mode (skip subprocess e2e vault tests):

```bash
./tools/test-vault.sh --quick
```

For containerized smoke coverage (root + non-root installer paths), run:

```bash
./tools/test-install-si-docker.sh
```

Use `SI_INSTALL_SMOKE_SKIP_NONROOT=1 ./tools/test-install-si-docker.sh` to skip
the non-root leg during local iteration.

## CI notes
GitHub Actions workflows use docs-only change detection to skip heavy test jobs
when only docs/markdown files are modified.

## PaaS matrix
For `si paas` quality-gate coverage (unit/integration/e2e regression matrix), use:

```bash
cat docs/PAAS_TEST_MATRIX.md
```

For failure-injection and rollback drill procedures, use:

```bash
cat docs/PAAS_FAILURE_DRILLS.md
./tools/paas-failure-drills.sh
```

For PaaS security review checklist and threat model guidance, use:

```bash
cat docs/PAAS_SECURITY_THREAT_MODEL.md
```

For incident-response operational procedures, use:

```bash
cat docs/PAAS_INCIDENT_RUNBOOK.md
```

For state-isolation data classification policy, use:

```bash
cat docs/PAAS_STATE_CLASSIFICATION_POLICY.md
```

For private state backup/restore policy, use:

```bash
cat docs/PAAS_BACKUP_RESTORE_POLICY.md
```

For context operating model and day-2 procedures, use:

```bash
cat docs/PAAS_CONTEXT_OPERATIONS_RUNBOOK.md
```

For incident event schema and dedupe taxonomy, use:

```bash
cat docs/PAAS_INCIDENT_EVENT_SCHEMA.md
```

For event bridge collector behavior and mapping rules, use:

```bash
cat docs/PAAS_EVENT_BRIDGE_COLLECTORS.md
```

For incident queue storage and retention policy, use:

```bash
cat docs/PAAS_INCIDENT_QUEUE_POLICY.md
```

For live agent command runtime behavior, use:

```bash
cat docs/PAAS_AGENT_RUNTIME_COMMANDS.md
```

For Codex-profile runtime adapter behavior, use:

```bash
cat docs/PAAS_AGENT_RUNTIME_ADAPTER.md
```

For remediation policy action evaluation rules, use:

```bash
cat docs/PAAS_REMEDIATION_POLICY_ENGINE.md
```

For approval decision workflow and Telegram callback linkage, use:

```bash
cat docs/PAAS_AGENT_APPROVAL_FLOW.md
```

For scheduler lock/self-heal behavior, use:

```bash
cat docs/PAAS_AGENT_SCHEDULER_SELF_HEAL.md
```

For offline fake-codex deterministic event-to-action smoke coverage, use:

```bash
cat docs/PAAS_AGENT_OFFLINE_SMOKE_TESTS.md
```

For per-run agent audit artifact persistence and correlation linkage, use:

```bash
cat docs/PAAS_AGENT_AUDIT_ARTIFACTS.md
```

## Static analysis
Run static analysis from the repo root:

```bash
./si analyze
```

Use non-failing mode for local iteration while keeping CI strict with default `./si analyze`:

```bash
./si analyze --no-fail
```

If you only changed the CLI, this is the fastest local scope:

```bash
./si analyze --module tools/si
```

## CLI help smoke checks
After CLI command-surface changes, run targeted help checks:

```bash
./si --help
./si mintlify --help
./si paas --help
./si paas backup --help
./si gcp gemini image generate --help
./si browser --help
```

## Image build smoke check
`si build image` now runs a Codex compatibility preflight (dyad/spawn/mount/MCP
lanes) before building the image.

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
- If `docker buildx` is available, SI runs `docker buildx build --load` directly (native buildx progress UI in interactive terminals).
- If `docker buildx` is unavailable or probe fails, SI uses classic `docker build`.
- SI no longer retries/falls back mid-build after a buildx start; mode is selected once up front.
