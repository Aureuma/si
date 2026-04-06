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
- `SI_NUCLEUS_SERVICE_PLATFORM`: force `systemd-user` or `launchd-agent`

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
- request/response methods such as `nucleus.status`, `profile.list`, `task.create`, `task.list`, `task.inspect`, `task.cancel`, `task.prune`, `worker.list`, `worker.inspect`, `worker.restart`, `worker.repair_auth`, `session.list`, `session.show`, `run.inspect`, and `run.cancel`
- server-pushed canonical events through `events.subscribe`

The bounded REST surface is exposed by the same Nucleus service and source of truth:

- `GET /openapi.json`
- `GET /status`
- `POST /tasks`
- `GET /tasks`
- `GET /tasks/{task_id}`
- `POST /tasks/{task_id}/cancel`
- `GET /workers`
- `GET /workers/{worker_id}`
- `GET /sessions/{session_id}`
- `GET /runs/{run_id}`

`/openapi.json` is OpenAPI 3.1 and includes summaries, descriptions, schemas, and `x-si-purpose` annotations for bounded external consumers.

## Security and auth

Default behavior:

- Nucleus binds to loopback only
- local reads and writes work without extra auth on loopback

When the gateway binds beyond loopback:

- read operations remain available
- mutating WebSocket and REST operations require bearer auth from `SI_NUCLEUS_AUTH_TOKEN`
- the `si nucleus ...` CLI forwards that token automatically when the env var is set

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

- use `si nucleus task prune --older-than-days 30` to explicitly remove old completed or failed task records from the durable task ledger
- pruning is conservative: it removes only old completed or failed task records and does not silently delete active worker, session, or run state

## Related docs

- [CLI Reference](./CLI_REFERENCE)
- [Command Reference](./COMMAND_REFERENCE)
- [Settings](./SETTINGS)
- [Vault](./VAULT)
