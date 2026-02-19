---
title: OpenAI Command Guide
description: OpenAI integration workflows in SI for auth, projects, keys, usage, monitoring, codex usage, and raw API calls.
---

# OpenAI Command Guide (`si openai`)

![OpenAI](/docs/images/integrations/openai.svg)

`si openai` provides authenticated OpenAI operations with context management, project administration, usage analytics, and raw access.

## Related docs

- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [Vault](./VAULT)
- [Providers](./PROVIDERS)

## Command surface

```bash
si openai <auth|context|doctor|model|project|key|usage|monitor|codex|raw>
```

## Auth and context

```bash
si openai auth status --account core --json
si openai context list --json
si openai context current --json
si openai context use --account core --org-id org_xxx --project-id proj_xxx
si openai doctor --account core --public --json
```

## Model and project administration

```bash
si openai model list --limit 20 --json
si openai model get gpt-5 --json

si openai project list --json
si openai project create --name "release-mind" --json
si openai project get proj_xxx --json
si openai project archive proj_xxx --force --json
```

## Key management

```bash
si openai key list --json
si openai key create --name "ci-automation" --json
si openai key delete key_xxx --force --json

si openai project api-key list --project-id proj_xxx --json
si openai project service-account list --project-id proj_xxx --json
```

## Usage and monitoring

```bash
si openai usage costs --start-time 1738368000 --end-time 1738972800 --json
si openai monitor usage --start-time 1738368000 --end-time 1738972800 --json
si openai codex usage --model gpt-5-codex --json
```

## Raw API mode

```bash
si openai raw --method GET --path /v1/models --json
si openai raw --method POST --path /v1/responses --json-body '{"model":"gpt-5","input":"hello"}' --json
```

Use `--admin` when explicitly targeting admin APIs.

## Safety guidance

- Separate runtime API key and admin API key usage.
- Prefer account-specific context over global env defaults in CI.
- Treat usage/cost reports as operational telemetry and store artifacts.
- Keep raw mode payloads in files for reviewable change history.

## Troubleshooting

1. `si openai auth status --json`
2. `si openai doctor --json`
3. `si providers health --provider openai --json`
4. Verify org/project context values and key source precedence.
