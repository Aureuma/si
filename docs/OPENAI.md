---
title: OpenAI Command Guide
description: OpenAI integration workflows in SI for auth, projects, keys, usage, monitoring, codex usage, and raw API calls.
---

# OpenAI Command Guide (`si orbit openai`)

![OpenAI](/docs/images/integrations/openai.svg)

`si orbit openai` provides authenticated OpenAI operations with context management, project administration, usage analytics, and raw access.

## Related docs

- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [Vault](./VAULT)
- [Providers](./PROVIDERS)

## Command surface

```bash
si orbit openai <auth|context|doctor|model|project|key|usage|monitor|codex|raw>
```

## Auth and context

```bash
si orbit openai auth status --account core --json
si orbit openai auth status --auth-mode codex --profile main --json
si orbit openai auth codex-status --profile main --json
si orbit openai context list --json
si orbit openai context current --json
si orbit openai context use --account core --org-id org_xxx --project-id proj_xxx
si orbit openai doctor --account core --public --json
```

`si orbit openai auth` supports two modes:
- `api` (default): validates OpenAI API-key auth against `api.openai.com`.
- `codex`: validates ChatGPT/Codex plan-token auth from SI codex profiles (OpenClaw-style non-API-key flow).

## Model and project administration

```bash
si orbit openai model list --limit 20 --json
si orbit openai model get gpt-5 --json

si orbit openai project list --json
si orbit openai project create --name "release-mind" --json
si orbit openai project get proj_xxx --json
si orbit openai project archive proj_xxx --force --json
```

## Key management

```bash
si orbit openai key list --json
si orbit openai key create --name "ci-automation" --json
si orbit openai key delete key_xxx --force --json

si orbit openai project keys list --project-id proj_xxx --json
si orbit openai project accounts list --project-id proj_xxx --json
```

## Usage and monitoring

```bash
si orbit openai usage costs --start-time 1738368000 --end-time 1738972800 --json
si orbit openai monitor usage --start-time 1738368000 --end-time 1738972800 --json
si orbit openai codex usage --model gpt-5-codex --json
```

## Raw API mode

```bash
si orbit openai raw --method GET --path /v1/models --json
si orbit openai raw --method POST --path /v1/responses --json-body '{"model":"gpt-5","input":"hello"}' --json
```

Use `--admin` when explicitly targeting admin APIs.

## Safety guidance

- Separate runtime API key and admin API key usage.
- Prefer account-specific context over global env defaults in CI.
- Treat usage/cost reports as operational telemetry and store artifacts.
- Keep raw mode payloads in files for reviewable change history.

## Troubleshooting

1. `si orbit openai auth status --json`
2. `si orbit openai doctor --json`
3. `si orbit list --provider openai --json`
4. Verify org/project context values and key source precedence.
