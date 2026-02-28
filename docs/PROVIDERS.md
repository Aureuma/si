---
title: Providers Telemetry Guide
description: Using si providers to inspect integration characteristics, runtime health, guardrails, and API version policy coverage.
---

# Providers Telemetry Guide (`si providers`)

![Providers](/docs/images/integrations/providers.svg)

`si providers` is the control-plane view across SI integration runtimes.

## Related docs

- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [API Characteristics](./API_CHARACTERISTICS)
- [Orbit Marketplace](./ORBITS)

## Command surface

```bash
si providers <characteristics|health> [--provider <id>] [--json]
```

Aliases:

```bash
si integrations ...
si apis ...
```

## Characteristics view

Use this to inspect static policy and capability metadata.

```bash
si providers characteristics --json
si providers characteristics --provider github --json
```

Output includes:

- base URL and API version
- auth style
- rate and burst policy
- public probe endpoint
- capability flags (pagination/bulk/idempotency/raw)

## Health view

Use this for runtime traffic and guardrail telemetry.

```bash
si providers health --json
si providers health --provider openai --json
```

Output includes:

- per-provider request counts and latency percentiles
- `429` and `5xx` surfaces
- circuit state snapshots
- guardrail warnings
- API version warnings/errors and coverage gaps

## Provider IDs

Common IDs:

- `github`
- `cloudflare`
- `google_places`
- `google_play`
- `apple_appstore`
- `youtube`
- `stripe`
- `social_facebook`, `social_instagram`, `social_x`, `social_linkedin`, `social_reddit`
- `workos`
- `aws_iam`
- `gcp_serviceusage`
- `openai`
- `oci_core`

## Operational playbook

1. Run `si providers characteristics --json` after upgrading SI.
2. Run `si providers health --json` after auth/context setup.
3. Investigate guardrail or version warnings before production writes.
4. Pair provider health checks with integration-specific doctor commands.

## Troubleshooting

- No entries in health output usually means no integration traffic yet.
- Version policy errors indicate missing/invalid provider policy metadata.
- Re-run affected integration doctor command and verify context/account.
