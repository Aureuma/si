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
| Integration bridges | `si github`, `si cloudflare`, `si gcp`, `si aws`, `si openai`, `si oci`, `si google`, `si social`, `si workos`, `si apple appstore`, `si stripe`, `si publish` |
| Provider telemetry | `si providers` |
| Platform operations | `si paas` |
| Browser MCP runtime | `si browser` |
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

## Safety guidance

- Use `si vault run -- <command>` when secrets are required.
- Prefer `--json` for automation and auditability.
- Run `doctor` commands before mutating production systems.
- Keep docs and `docs.json` navigation in sync.
