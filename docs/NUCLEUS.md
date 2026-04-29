---
title: Nucleus
description: Local Nucleus control plane, service install workflow, gateway discovery, and bounded API surfaces.
---

# Nucleus

`si nucleus` is the SI local control plane.

It owns:

- the durable task ledger
- worker, session, and run orchestration
- the local WebSocket gateway
- the bounded REST inspection/mutation surface
- OS-native user-service install helpers

## Main CLI surfaces

Use `si nucleus ...` for control-plane operations:

```bash
si nucleus status
si nucleus profile list
si nucleus task create "Review nightly failures" "Summarize the last failed run."
si nucleus task list
si nucleus task cancel <task-id>
si nucleus task inspect <task-id>
si nucleus task prune --older-than-days 30
si nucleus producer cron upsert svelte-docs-nightly --schedule-kind cron --schedule "0 0 6 * * *" --instructions "Audit Svelte docs and blog readiness."
si nucleus producer hook upsert github-notify --match-event-type github.notification --instructions "Triage the GitHub notification and create follow-up tasks."
si nucleus worker list
si nucleus worker restart <worker-id>
si nucleus worker repair-auth <worker-id>
si nucleus session list
si nucleus run inspect <run-id>
si nucleus events subscribe --count 1
```

`si codex ...` remains the worker/runtime-facing surface.

## Service management

Nucleus is intended to run as a local user service.

Supported flows:

```bash
si nucleus service install
si nucleus service start
si nucleus service status --format json
si nucleus service restart
si nucleus service stop
si nucleus service uninstall
```

Platform behavior:

- Linux: generates `systemd --user` unit `si-nucleus.service`
- macOS: generates launchd agent `com.aureuma.si.nucleus`

The generated service definition points at the current `si` binary and runs the hidden service entrypoint:

```bash
si nucleus service run
```

Relevant env vars:

- `SI_NUCLEUS_STATE_DIR`: override the Nucleus state root
- `SI_NUCLEUS_BIND_ADDR`: override the local bind address
- `SI_NUCLEUS_BIN`: override the concrete `si-nucleus` runtime binary used by `si nucleus service run`
- `SI_NUCLEUS_PUBLIC_URL`: override the absolute OpenAPI `servers[0].url` value for GPT Actions import
- `SI_NUCLEUS_SERVICE_PLATFORM`: force `systemd-user` or `launchd-agent`

Service install records the current `PATH`, `SI_NUCLEUS_STATE_DIR`,
`SI_NUCLEUS_BIND_ADDR`, `SI_NUCLEUS_BIN`, `SI_NUCLEUS_AUTH_TOKEN`, and
`SI_NUCLEUS_PUBLIC_URL` values in the generated user service so
launchd/systemd can run Nucleus without inheriting an interactive shell.
When the current `si` executable lives under a transient build path such as
`target/` or `.artifacts/`, install prefers a stable `si` from `PATH` for the
service launcher when available. Re-run `si nucleus service install` after
changing those values or after installing SI through a different binary path.
If you omit `--state-dir` or `--bind-addr`, install now takes those defaults
from `SI_NUCLEUS_STATE_DIR` and `SI_NUCLEUS_BIND_ADDR` before falling back to
the built-in loopback defaults.

## Gateway discovery

CLI discovery order for the local WebSocket endpoint:

1. `--endpoint`
2. `SI_NUCLEUS_WS_ADDR`
3. `~/.si/nucleus/gateway/metadata.json`
4. default `ws://127.0.0.1:4747/ws`

The metadata file is written by `si-nucleus` and includes the bound websocket URL and current SI version.

## Gateway and API surfaces

The main control-plane transport is WebSocket:

- default local endpoint: `ws://127.0.0.1:4747/ws`
- request/response methods such as `nucleus.status`, `profile.list`, `task.create`, `task.list`, `task.inspect`, `task.cancel`, `task.prune`, `producer.cron.*`, `producer.hook.*`, `worker.list`, `worker.inspect`, `worker.restart`, `worker.repair_auth`, `session.create`, `session.list`, `session.show`, `run.submit_turn`, `run.inspect`, and `run.cancel`
- server-pushed canonical events through `events.subscribe`

The external REST contract is the generated `docs/gpt-actions-openapi.yaml` artifact and the live `GET /openapi.json` response from the same Nucleus source of truth. Treat those generated OpenAPI surfaces as canonical for GPT Actions and other bounded external clients.

Use `GET /capabilities` as the public compatibility probe for external clients. It reports whether REST task creation is available, whether text-mode GPT Actions are expected to work, and whether ChatGPT Voice can invoke custom GPT Actions. OpenAI's ChatGPT Voice mode does not currently invoke custom GPT Actions, so voice workflows that need Nucleus task creation must use a custom voice client, such as an OpenAI Realtime API client with function calling that calls `POST /tasks`.

Additional local service routes such as `/events`, `/webhooks/github`, `/producers/cron...`, and `/producers/hook...` may exist for trusted SI automation, but they are intentionally excluded from the external GPT Actions contract and should not be treated as public integration surfaces.

## Task Producers

Nucleus task producers turn durable schedules and canonical events into normal queued tasks. Producer tasks use the same task ledger, dispatcher, retry, blocking, and inspection paths as directly created tasks.

Cron producers:

- Create or update with `si nucleus producer cron upsert <name> --schedule-kind <once_at|every|cron> --schedule <value> --instructions <prompt>`.
- `once_at` uses an RFC3339 timestamp, `every` accepts positive `s`, `m`, `h`, or `d` durations such as `30m`, and `cron` accepts the six-field cron format used by the runtime parser.
- Upserts preserve the next due time when the schedule is unchanged; pass `--reset` to recompute it from now.
- Each due fire creates at most one task using a producer dedup key, so replay after restart advances the rule without duplicating already-created tasks.
- Cron producers do not overlap by default: if a previous task for the same rule is still queued, running, or blocked, Nucleus keeps the due time pending instead of creating another task.

Hook producers:

- Create or update with `si nucleus producer hook upsert <name> --match-event-type <canonical.event> --instructions <prompt>`.
- Hooks only process events after the rule was created, so a new rule does not backfill all existing event history.
- Progress is persisted with `last_processed_event_seq`; replay after restart resumes from the last processed canonical event.
- Self-triggered hook tasks are ignored to avoid event loops.

GitHub webhooks:

- `POST /webhooks/github` accepts normal GitHub webhook JSON plus `X-GitHub-Event` and `X-GitHub-Delivery` headers.
- The route normalizes the delivery into a canonical `github.notification` event whose payload includes the GitHub event name, delivery id, action, repository full name, sender login, ref, and original `github_payload`.
- If `SI_NUCLEUS_GITHUB_WEBHOOK_SECRET` is set, the route requires a valid `X-Hub-Signature-256` HMAC. Without that secret, it uses the same bearer-token policy as other local REST routes.
- A hook rule matching `github.notification` can turn meaningful GitHub deliveries such as `push`, `pull_request`, `issues`, `workflow_run`, and `release` into durable Nucleus tasks.

## Task Profile Assignment

Nucleus assigns every dispatched task to one worker profile. Task creation may include a preferred `profile`, but the dispatcher can select a fallback when the task is not pinned to an existing session.

Priority order:

1. the task's requested `profile`, when present
2. profiles with ready workers, sorted deterministically by profile and worker id
3. configured profile records
4. profiles attached to reusable sessions
5. profiles with non-ready workers, used only as a last candidate

If a candidate is unavailable because its worker cannot start, Fort authentication is unavailable, or the profile is otherwise not usable, Nucleus tries the next candidate. Tasks pinned to a `session_id` do not cross profile boundaries; a session mismatch remains blocked as `session_broken`.

When Nucleus selects a candidate, it writes that profile back to the task record so later inspection shows which profile actually owns execution.

## External Task Failure Modes

For GPT Actions and other REST clients, the main failure and blocking scenarios are:

1. Invalid intake requests:
   blank `title` or `instructions`, malformed JSON bodies, unsupported `source` values, non-slug profile names, invalid session ids, or `timeout_seconds: 0` are rejected as `400 invalid_params`.
2. Missing or unusable profile resolution:
   if Nucleus cannot resolve any usable profile candidate, the task is blocked as `profile_unavailable`.
3. Worker or auth unavailability:
   if the selected worker cannot start, loses its runtime attachment, or lacks required Fort auth, the task is blocked as `worker_unavailable`, `auth_required`, or `fort_unavailable`.
4. Broken session affinity:
   if a task references a missing, mismatched, closed, or threadless session, the task is blocked as `session_broken`.
5. Runtime execution failures:
   worker channel loss, request timeout, or worker shutdown can fail a run and may quarantine the worker or session.
6. Oversized bounded work:
   broad repository audits or long-form reporting can exceed the runtime turn budget and fail with messages such as `turn exceeded max duration ...`.

External client guidance:

1. REST task creation defaults `source` to `rest` and `timeout_seconds` to `900` when callers omit them.
2. Large audits should be split into smaller repo- or subsystem-scoped tasks instead of one repo-wide pass.
3. If continuity matters, inspect the task's `profile`, `session_id`, `latest_run_id`, and `blocked_reason` before retrying.
4. Treat `blocked` tasks as operator-actionable state, not silent transient failure.
5. Treat ChatGPT Voice failures differently from REST failures: ChatGPT Voice may never call Nucleus because custom GPT Actions are unavailable in Voice mode. Check `GET /capabilities` and the Nucleus access logs before debugging task dispatch.

## GPT Actions OpenAPI rules

For any Nucleus REST endpoint that is exposed through `/openapi.json` or `docs/gpt-actions-openapi.yaml`:

- `servers[0].url` must be an absolute HTTPS URL for public imports. Use `SI_NUCLEUS_PUBLIC_URL` in deployed environments.
- Every operation must have a stable `operationId`, `summary`, `description`, success response schema, and `x-si-purpose`.
- Every JSON request or response body must define a concrete schema. Do not use generic object schemas such as `type: object` with only `additionalProperties`.
- Field descriptions must match live semantics exactly. If a field only reports a runtime-side action, describe that narrow behavior instead of a broader workflow outcome.
- Every object schema must include a `properties` key. Map-like objects may use `properties: {}` plus `additionalProperties`, but the `properties` key must still be present for GPT Actions compatibility.
- Public bootstrap endpoints such as `GET /openapi.json` must set operation `security: []`; operational endpoints must explicitly require `bearerAuth`.
- Generate `docs/gpt-actions-openapi.yaml` from the canonical runtime model instead of editing version strings or endpoint lists by hand:

  ```bash
  cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-sync-nucleus-openapi -- --write
  ```

- Use `--check` in automation or before a commit to catch drift:

  ```bash
  cargo run --quiet --locked --manifest-path rust/crates/si-tools/Cargo.toml --bin si-sync-nucleus-openapi -- --check
  ```

- Run the Nucleus OpenAPI tests before committing schema changes so importer compatibility failures are caught locally.

## Public route ownership

The public `https://nucleus.aureuma.ai` route is currently exposed by Viva shared Traefik, but the runtime is owned by this `si` repository.

Current same-host route targets:

- `https://nucleus.aureuma.ai` forwards to the `si-nucleus` user service on port `4747`.
- `https://nucleus.aureuma.ai/gpt-actions-openapi.yaml` and `https://nucleus.aureuma.ai/privacy` forward to the static docs server on port `8092`, rooted at `docs/`.

This is intentionally not modeled as a Viva Docker container yet. Nucleus owns durable local state under `~/.si/nucleus/`, uses the `si nucleus service ...` OS-native lifecycle, and writes gateway metadata consumed by local CLI discovery. Moving it into Docker requires a separate migration plan for state, auth token delivery, public bind policy, and gateway discovery.

Route ownership rules while it remains external:

- Viva owns only the Traefik route entry.
- SI owns the `si-nucleus` process, docs server, auth policy, OpenAPI document, and service lifecycle.
- Set `SI_NUCLEUS_AUTH_TOKEN` whenever the gateway binds beyond loopback.
- Set `SI_NUCLEUS_PUBLIC_URL=https://nucleus.aureuma.ai` for public OpenAPI generation.
- Keep Viva route ownership docs in sync when ports, paths, or lifecycle change.

## Security and auth

Default behavior:

- Nucleus binds to loopback only
- local reads and writes work without extra auth on loopback

When `SI_NUCLEUS_AUTH_TOKEN` is set:

- all WebSocket and REST operations require bearer auth from `SI_NUCLEUS_AUTH_TOKEN`
- the `si nucleus ...` CLI forwards that token automatically when the env var is set

When the gateway binds beyond loopback:

- set `SI_NUCLEUS_AUTH_TOKEN` so public reads and writes are both protected

## State layout

Default state root:

```text
~/.si/nucleus/
```

Important paths:

- runtime state: `~/.si/nucleus/state/`
- canonical event ledger: `~/.si/nucleus/state/events/events.jsonl`
- gateway metadata: `~/.si/nucleus/gateway/metadata.json`
- worker directories: `~/.si/nucleus/workers/<worker-id>/`

Retention and cleanup:

- use `si nucleus task prune --older-than-days 30` to explicitly remove old completed, failed, or cancelled task records from the durable task ledger
- pruning is conservative: it removes only old terminal task records and does not silently delete active worker, session, or run state

## Related docs

- [CLI Reference](./CLI_REFERENCE)
- [Command Reference](./COMMAND_REFERENCE)
- [Settings](./SETTINGS)
- [Vault](./VAULT)
