---
title: Providers Telemetry Guide
description: Using si orbit list to inspect integration characteristics, guardrails, and API version policy coverage.
---

# Providers Telemetry Guide (`si orbit list`)

![Providers](/docs/images/integrations/providers.svg)

`si orbit list` is the control-plane view across SI's first-party provider runtimes.

## Related docs

- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [API Characteristics](./API_CHARACTERISTICS)

## Command surface

```bash
si orbit list [--provider <id>] [--json]
```

## Characteristics view

Use this to inspect static policy and capability metadata.

```bash
si orbit list --json
si orbit list --provider github --json
```

Output includes:

- base URL and API version
- auth style
- rate and burst policy
- public probe endpoint
- capability flags (pagination/bulk/idempotency/raw)

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

1. Run `si orbit list --json` after upgrading SI.
2. Run integration-specific auth status and doctor commands after context setup.
3. Investigate version warnings before production writes.
4. Pair provider inventory checks with integration-specific doctor commands.

## Troubleshooting

- Version policy errors indicate missing/invalid provider policy metadata.
- Re-run affected integration doctor command and verify context/account.
