---
name: si-provider-debug
description: Use this skill when debugging provider integrations in SI (OpenAI, GitHub, Cloudflare, Google, Stripe, GCP, AWS) with reproducible CLI checks.
---

# SI Provider Debug

Use this workflow to isolate provider integration issues quickly.

## Triage sequence

1. Confirm resolved settings and auth context:
```bash
si self doctor
si orbit list
```

2. Run the smallest read-only probe for the target provider:
```bash
si orbit openai models list --limit 1
si orbit github repos list --limit 1
si orbit cloudflare zones list --limit 1
```

3. If probe fails, run raw request form to inspect HTTP status/body:
```bash
si orbit <provider> raw --method GET --path <path>
```

## Root-cause checklist

- Wrong account/env selection.
- Missing token or wrong vault/env key.
- Base URL mismatch (prod vs staging / GHES).
- Permission scope mismatch.
- Rate limit or quota exhaustion.

## Guardrails

- Prefer least-privilege probes first.
- Avoid mutating operations until auth/context is verified.
- Redact tokens and sensitive headers in outputs and logs.
