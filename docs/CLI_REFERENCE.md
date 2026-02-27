---
title: CLI Reference
description: Practical SI CLI orientation with command discovery, top-level families, and high-signal workflows.
---

# CLI Reference

This page is the fast orientation guide for `si`.

For a full categorized list, use [Command Reference](./COMMAND_REFERENCE).

## Command discovery pattern

```bash
si --help
si <command> --help
si <command> <subcommand> --help
```

## Top-level command families

| Domain | Commands |
| --- | --- |
| Runtime and orchestration | `si dyad`, codex lifecycle (`si spawn`, `si run`, `si status`, `si report`) |
| Secrets and context | `si vault` (`si creds`) |
| Integration bridges | `si github`, `si cloudflare`, `si gcp`, `si aws`, `si openai`, `si oci`, `si google`, `si social`, `si workos`, `si apple appstore`, `si stripe`, `si publish`, `si sun` |
| Provider telemetry | `si providers` |
| Platform operations | `si paas` |
| Surf browser runtime | `si surf` |
| Plugin ecosystem | `si plugins` |
| Build and quality | `si build`, `si analyze` (`si lint`), `si docker` |
| Docs workflow | `si mintlify` |
| Profiles and skills | `si persona`, `si skill` |

## High-signal workflows

### Runtime setup

```bash
si build image
si dyad spawn app-hardening --profile main
si dyad status app-hardening
```

### Viva tunnel via SI wrapper

```bash
si viva config set --repo ~/Development/viva --build true
si viva config tunnel show --json
si viva -- tunnel up --profile dev
si viva -- tunnel status --profile dev
si viva -- tunnel down --profile dev
```

### Shared dyad taskboard

```bash
si sun taskboard add --name shared --title "Triage flaky test" --prompt "Reproduce and fix the flaky test in CI" --priority P1
si dyad spawn ci-triage --autopilot --profile main
```

### Cross-machine SI control

```bash
si sun machine register --machine controller-a --operator op:controller@local --can-control-others --can-be-controlled=false
si sun machine run --machine worker-a --source-machine controller-a --operator op:controller@local --wait -- version
```

`--wait` exits non-zero for remote `failed`/`denied` jobs, so it is safe for CI gating.

### Integration readiness

```bash
si providers characteristics --json
si github doctor --json
si cloudflare doctor --json
si gcp doctor --json
```

### PaaS readiness

```bash
si paas doctor --json
si paas backup run --app <slug> --json
si paas events tail --app <slug> --json
```

### Docs quality

```bash
si mintlify validate
si mintlify broken-links
```

### Release preflight

```bash
si build self release-assets --version vX.Y.Z --out-dir .artifacts/release-preflight
```

## Safety guidance

- Use `si vault run -- <command>` when secrets are required.
- Prefer `--json` for automation and auditability.
- Run `doctor` commands before mutating production systems.
- Keep docs and `docs.json` navigation in sync.
