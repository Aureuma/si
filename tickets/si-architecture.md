# SI Nucleus Architecture Plan

## Status

This file is the single consolidated SI architecture and research plan.

It supersedes and absorbs the useful conclusions from:
- OpenClaw architecture research
- CLI agent orchestration landscape research
- Codex CLI multi-account authentication research

## Architecture source of truth

Until implementation lands and replaces parts of this plan with accepted code and docs, this file is the architecture source of truth for SI Nucleus.

Rules:
- if README or adjacent docs disagree with this plan, this plan wins until the implementation is updated and the docs are reconciled
- implementation should update public docs once a planned contract becomes real and accepted
- avoid drifting terminology between the ticket, CLI help, and public docs

Current decision set:
- SI does not support Docker.
- SI does not support Kubernetes.
- SI does not support any image-based Codex runtime.
- SI does not support MCP as part of the SI architecture.
- SI uses local Codex App Server processes as the only Codex runtime transport.
- SI uses WebSocket as the main Nucleus control-plane transport.
- SI may add an annotated REST API as a later external integration surface after the core system is accepted.
- SI uses isolated `CODEX_HOME` roots per worker/profile/account.
- SI persists orchestration state using format-by-fit storage: Markdown, JSON, and JSONL as appropriate.
- SI treats tmux as optional operator tooling, not as part of the core runtime contract.
- SI keeps one single local control plane as the architecture center.
- That control plane is named `nucleus`.

## Non-goals

The plan should explicitly exclude these directions:
- no Docker runtime or container orchestration return
- no Kubernetes
- no MCP integration as part of the SI architecture
- no raw App Server protocol as the public SI contract
- no second orchestration model for adjacent tools
- no correctness dependency on `AGENTS.md`, skills, or `SKILL.md` files
- no separate authoritative queue object beside the durable task ledger
- no separate authoritative event streams beside the canonical SI event ledger

## Core conclusion

SI should copy OpenClaw's control-plane shape, not its sandbox/runtime stack.

What transfers:
- durable session identity
- orchestrator-owned lifecycle
- explicit worker classes and runtime defaults
- structured event flow
- control-plane-first design

What does not transfer:
- Docker as the worker boundary
- container identity as architecture
- runtime/image planning as a primary concern
- session logic inferred from shell/container state

The final SI direction is:
- local-process-first
- App-Server-first
- WebSocket-gateway-first
- REST-later-for-external-integration
- session-first
- tmux-optional
- `CODEX_HOME`-isolated
- Nucleus-centered

## Why the name is nucleus

The local SI control plane should no longer be referred to as `si-agentd`.

Use `nucleus` instead because it works better as:
- the central runtime identity
- the binary name
- the long-term API identity
- the architecture term in docs and logs

Recommended naming:
- concept: `SI nucleus`
- binary: `si-nucleus`
- local service/gateway identity: `nucleus`
- WebSocket API surface: `nucleus gateway`

Avoid using abstract architecture nouns like `framework`, `protocol`, `model`, or `dashboard` as the service name.

## Why Docker is gone

The only strong reason SI originally had for Docker was multi-account Codex isolation on one machine.

That requirement is now satisfied without Docker by:
- one local Codex App Server process per worker
- one isolated `CODEX_HOME` per worker/profile/account
- one orchestrator supervising those worker processes

So Docker is no longer needed for the core SI runtime.

## Architectural model

### 1. Control plane

`si-nucleus` is the local orchestrator.

It owns:
- worker lifecycle
- session lifecycle
- run lifecycle
- task intake and projection
- event persistence
- runtime supervision
- repair and recovery behavior

### 2. Core primitives

The core Nucleus primitives are:
- `task`
- `worker`
- `session`
- `run`
- `event`
- `profile`

This is intentionally minimal.

Why this is the right model:
- App Server already gives SI the core runtime primitives
- SI should map closely to those primitives instead of inventing a parallel object graph
- the only extra first-class SI primitive SI truly needs is `task`
- everything else can be a field, a projection, or service behavior instead of a new durable object

### 2.1 `task`

A `task` is the only extra first-class SI primitive beyond the App Server-aligned model.

Why it is needed:
- incoming work from any channel needs one canonical durable object
- operators need one place to inspect, cancel, retry, and summarize requested work
- App Server models conversation execution, not business-level requested work

A task should include:
- `task_id`
- `source`
- `title`
- `instructions`
- `status`
- `profile`
- `session_id` if already attached
- latest `run_id` if any
- optional `checkpoint_summary`
- optional `checkpoint_at`
- optional `checkpoint_seq`
- `parent_task_id`
- `created_at`
- `updated_at`
- retry and timeout fields

Suggested task `source` values:
- `cli`
- `websocket`
- `cron`
- `hook`
- `system`

App Server mapping:
- no direct App Server primitive
- a task is resolved by Nucleus into one session and one or more runs over time

Interaction summary:
- channels and internal producers create tasks
- a task targets a profile
- Nucleus resolves the task onto a worker and session
- runs execute task steps
- events and summaries project the outcome back onto the task

### 2.2 `worker`

A `worker` is one long-lived local Codex App Server process with isolated `CODEX_HOME`.

Why it is needed:
- process lifecycle, account isolation, and runtime supervision are separate concerns from task identity

A worker should include:
- `worker_id`
- `profile`
- `CODEX_HOME`
- runtime status
- capability/version data
- last heartbeat or last activity
- effective account/config state

App Server mapping:
- one App Server process or connection endpoint
- initialized runtime with account/config/capability state

Interaction summary:
- workers host sessions
- tasks are routed to workers primarily by profile
- repair and recovery act on workers

### 2.3 `session`

A `session` is SI's durable identity for ongoing work on a worker.

Why it is needed:
- tasks may span many runs
- session continuity matters more than terminal continuity

A session should include:
- `session_id`
- `worker_id`
- App Server thread id
- lifecycle state
- summary state

App Server mapping:
- primarily maps to an App Server thread/session identity

Interaction summary:
- tasks may attach to or reuse sessions
- runs always occur inside a session

### 2.4 `run`

A `run` is one submitted turn or orchestrator-triggered operation inside a session.

Why it is needed:
- one task often produces many model turns
- cancellation, retries, and observability need a unit smaller than the session

A run should include:
- `run_id`
- `task_id`
- `session_id`
- run status
- start/end timestamps
- parent run if any
- App Server turn id where applicable

App Server mapping:
- primarily maps to an App Server turn

Interaction summary:
- runs produce events
- tasks read latest run state for progress and completion

### 2.5 `event`

An `event` is the normalized durable record of runtime activity.

Why it is needed:
- event persistence is the basis for replay, debugging, UI updates, and API projections
- one canonical event ledger keeps recovery and projection simple

Event categories should include:
- worker lifecycle
- session lifecycle
- run lifecycle
- output delta
- tool or approval signal
- auth/account signal
- task signal

Event ledger rules:
- SI keeps one canonical event ledger
- App Server, Nucleus system activity, webhook inputs, and other future event sources must be normalized before entering the ledger
- SI must not keep separate authoritative event streams by source
- SI must not keep separate raw source logs or raw source ledgers as part of the architecture
- canonical events should include a `source` field so origin stays visible without splitting the ledger
- noisy low-level source traffic should be filtered or coalesced before durable append

App Server mapping:
- thread events
- turn events
- item events
- account/config/approval notifications

Interaction summary:
- events are the bridge between live runtime and durable orchestration state
- all event-producing channels feed the same canonical SI ledger after normalization

### 2.6 `profile`

A `profile` is the scheduler-facing account/runtime identity.

Why it is needed:
- SI must choose account/auth/runtime defaults without hiding them in ad hoc env state

A profile should include:
- `profile`
- account identity
- `CODEX_HOME`
- auth mode
- preferred models and runtime defaults

Profile identifier rule:
- the lowercase profile name is the identifier
- do not introduce a separate profile identifier field
- examples: `america`, `darmstada`, `einsteina`
- profile names should be lowercase ASCII slugs matching `^[a-z][a-z0-9-]*$`

App Server mapping:
- `account/read` and `config/read` are the runtime probes for effective profile state

## Naming grammar

Keep names simple and machine-safe.

Rules:
- canonical event `type` values use dot-separated SI names such as `task.created`, `worker.ready`, and `run.completed`
- do not expose raw App Server event names as canonical SI event types
- producer rule names should use the same lowercase ASCII slug grammar as profiles
- SI-owned environment variables must use full `SI_` names such as `SI_NUCLEUS_WS_ADDR`, `SI_NUCLEUS_STATE_DIR`, `SI_NUCLEUS_AUTH_TOKEN`, and `SI_NUCLEUS_LOG_LEVEL`

## Primitive interaction model

The intended execution chain is:
- a channel or internal producer creates a `task`
- Nucleus routes the task to a compatible `profile`
- Nucleus chooses or prepares a `worker`
- the worker hosts or resumes a `session`
- Nucleus submits a `run`
- App Server emits runtime signals
- Nucleus normalizes them into `events`
- Nucleus projects the latest run and summary state back onto the `task`

## Task ledger and queue-state model

Nucleus should use a durable task ledger with queue state, not a separate queue primitive.

Why this is needed:
- tasks must remain the single source of truth for requested work
- queued work must survive restart
- cron and hook producers must create work into the same durable system used by operator-created tasks
- a separate queue object would duplicate state and create reconciliation problems

Chosen mechanism:
- every task is a durable object on disk
- runnable tasks are tasks whose state is `queued`
- Nucleus performs queue-like selection over queued tasks rather than maintaining a second queue data structure
- selection is grouped primarily by `profile`
- if a task is already attached to a `session_id`, that session acts as a serialization constraint for the task

What this means in practice:
- task creation is also queue insertion
- changing a task to `queued` makes it eligible for selection
- changing a task to `running`, `done`, `failed`, `blocked`, or `cancelled` removes it from runnable selection
- operator-visible task lists are projections over the durable task ledger

Session-affine backlog rule:
- if multiple queued tasks point to the same `session_id`, Nucleus must treat them as an ordered backlog behind that session
- Nucleus must not run overlapping turns for the same session unless App Server semantics explicitly allow it and SI intentionally supports it
- session-affine backlog is a constraint on dispatch, not a separate primitive

Dispatch rule:
- choose from queued tasks
- group by `profile`
- respect session-affine backlog when `session_id` is present
- dispatch onto a healthy worker for that profile

Rejected alternatives for now:
- separate queue object or queue file
- in-memory-only queue
- external broker
- dead-letter queue
- workflow DAG scheduler

## Task producer model

Nucleus may create tasks internally through producers, but the produced object must always be a normal `task`.

Why this is needed:
- reliable scheduled work needs durable producer state
- reliable event-driven work needs explicit matching and deduplication
- SI should not create a separate job universe outside the main task model

Supported producer kinds:
- `cron`
- `hook`

Producer rules:
- producers are internal Nucleus machinery, not new first-class orchestration primitives
- producers create normal tasks with `task.source` set to `cron` or `hook`
- producers must persist enough state to resume safely after Nucleus restart
- producer failures must be visible through durable events and logs

Cron producer requirements:
- persist schedule definitions on disk
- persist next due time and last successful emission time
- on startup, reconcile missed due times deterministically
- never emit duplicate tasks for the same scheduled firing
- create tasks atomically before marking the scheduled firing complete

Minimal cron rule shape:
- `name`
- `enabled`
- `schedule_kind`
- `schedule`
- `instructions`
- `last_emitted_at`
- `next_due_at`
- `version`

Cron schedule model:
- `schedule_kind` may be `once_at`, `every`, or `cron`
- `schedule` is a timestamp, duration, or cron expression depending on `schedule_kind`

Cron task creation model:
- a due cron rule creates a normal task with `task.source = cron`
- the created task carries natural-language `instructions` derived from the cron rule
- the cron rule does not choose a `profile`; normal task routing decides that later

Cron deduplication model:
- deduplicate by durable key derived from `cron rule name + scheduled fire time`
- deduplication must survive restart and replay

Cron evaluation timing:
- cron evaluation should happen asynchronously in the producer loop
- due-time calculation must not block the main gateway path
- a scheduled firing is only marked complete after durable task creation succeeds

Cron reconciliation model:
- on startup, reconcile missed due times deterministically
- emit one task per missed scheduled fire time in v1
- do not silently skip missed scheduled firings

Hook producer requirements:
- consume normalized Nucleus and App Server events, not raw terminal text
- persist rule definitions on disk
- persist a cursor or last-processed event sequence per rule
- deduplicate task emission for the same matching event and rule
- create tasks atomically before advancing the rule cursor

Minimal hook rule shape:
- `name`
- `enabled`
- `match_event_type`
- `instructions`
- `last_processed_event_seq`
- `version`

Hook matching model:
- hooks match by canonical SI event `type`
- do not add secondary routing filters in v1
- hooks consume canonical SI events after append, not raw source events before normalization

Hook task creation model:
- a matching hook creates a normal task with `task.source = hook`
- the created task carries natural-language `instructions` derived from the hook rule
- the hook rule does not choose a `profile`; normal task routing decides that later

Hook deduplication model:
- deduplicate by durable key derived from `hook rule name + canonical event sequence`
- deduplication must survive restart and replay

Hook evaluation timing:
- hook evaluation should happen asynchronously after canonical event append
- the append path must stay fast and not block on hook task creation work
- the hook cursor may advance only after durable task creation succeeds

Reliability model:
- a producer must be restart-safe
- a producer must be idempotent across replay and restart
- task creation and producer state advancement must be ordered so crashes do not silently lose work
- when exact atomicity across files is not possible, Nucleus must prefer replayable at-least-once emission with deduplication over silent task loss
- at-least-once producer emission with deduplication is the preferred reliability model for both `cron` and `hook`
- silent task loss is worse than replay that is filtered by durable deduplication keys

## Task projection model

The operator-facing task list should be a projection, not a separate source of truth.

It should be derived from:
- task state
- latest run state
- blocked conditions
- session attachment when present

Recommended task states:
- `queued`
- `running`
- `blocked`
- `done`
- `failed`
- `cancelled`

Why this matters:
- operators need one central view of requested work
- external APIs should read task progress without replaying raw runtime streams
- the task remains the only SI-native first-class work object

## Task state transition model

Keep task transitions explicit and small.

Allowed states:
- `queued`
- `running`
- `blocked`
- `done`
- `failed`
- `cancelled`

Allowed transitions:
- `queued -> running`
- `queued -> cancelled`
- `running -> done`
- `running -> failed`
- `running -> blocked`
- `running -> cancelled`
- `blocked -> queued`
- `blocked -> cancelled`

Rules:
- only Nucleus changes task state
- producer-created tasks start as `queued`
- `done`, `failed`, and `cancelled` are terminal
- retries create a new `run`, not a new `task`

## Task checkpoint model

Keep checkpoints lightweight and derived from work already happening.

Rules:
- checkpoints are optional task metadata, not a separate primitive and not a separate task state
- a checkpoint is a durable progress marker for an in-flight or recently blocked task
- Nucleus may update a task checkpoint from normalized run or system events when that improves operator visibility
- checkpoints should stay small and human-readable

Suggested checkpoint fields:
- `checkpoint_summary`
- `checkpoint_at`
- `checkpoint_seq`

Why this is needed:
- long-running tasks need progress visibility without inventing more task states
- restart and recovery should preserve the latest known meaningful progress point
- operator inspection should not require replaying the whole event ledger to understand where a task got to

## Session attachment rules

Keep `session_id` optional on task creation.

Rules:
- if a task already has `session_id`, Nucleus must try that session first
- if a task has no `session_id`, Nucleus may reuse an existing compatible session on the chosen worker or create a new session
- once a task is attached to a session, that attachment is durable for the life of the task
- tasks sharing the same `session_id` form a backlog behind that session
- one session must not run overlapping turns unless SI explicitly supports that later

Compatibility rules for session reuse:
- same `profile`
- same worker still healthy
- session is not terminal or broken
- no conflicting active `run`

## Worker selection rules

Keep worker selection deterministic and easy to debug.

Selection order:
1. healthy worker with exact `profile` and matching attached `session_id`
2. healthy worker with exact `profile` and a reusable existing session
3. healthy idle worker with exact `profile`
4. start a new worker for that `profile`
5. if the profile is not ready, mark the task `blocked`

Rules:
- route only by `profile`
- do not introduce scoring or soft ranking in v1
- if multiple equal candidates exist, choose the oldest healthy idle worker or stable lexical `worker_id`
- one active worker per profile by default, with more only if SI explicitly allows it later

## Blocked reason model

Keep `blocked` as the task state, and carry the specific reason in metadata.

Suggested blocked reasons:
- `auth_required`
- `worker_unavailable`
- `session_broken`
- `producer_error`
- `operator_hold`

Rules:
- reason codes should be machine-readable and stable
- reason codes refine `blocked`; they do not expand the task state enum
- runtime events such as `run.requires_auth` may project to `task.status = blocked` with `blocked_reason = auth_required`

## App Server mapping model

Codex App Server should be treated as the runtime substrate, not the orchestration model.

Recommended mapping:
- SI `worker` -> one App Server process/connection
- SI `session` -> one App Server thread/session identity
- SI `run` -> one App Server turn
- SI `event` -> normalized thread/turn/item/account/config notifications
- SI `profile` -> effective account/config lane
- SI `task` -> owned by Nucleus as the only extra first-class orchestration primitive

Detailed App Server mapping guidance:
- worker readiness should perform `initialize`, then `account/read`, then `config/read`
- SI session creation or resume should map to App Server thread or session creation/resolution
- SI run submission should map to one App Server turn
- SI output and tool/approval deltas should be normalized from App Server item and turn notifications
- SI auth-required or degraded worker state should be derived from App Server account/config and turn-failure signals
- SI should project human summaries from completed turn and item outputs rather than inventing a separate artifact primitive

This means App Server is responsible for:
- model/runtime interaction
- turn execution
- account/config inspection
- native streaming notifications

And Nucleus is responsible for:
- intake
- task state
- worker selection
- persistence
- projection
- repair and recovery behavior

### 3. Runtime transport

Codex transport is:
- `codex app-server`

Not supported:
- `codex exec` oneshot mode
- raw terminal scraping as architecture
- alternate runtime backends hidden behind fallbacks

App Server is required.
If App Server is unavailable, the worker is unhealthy and SI should fail clearly.

### 4. Isolation model

Worker isolation comes from:
- separate process
- separate `CODEX_HOME`
- explicit environment shaping

Default worker contract:
- `CODEX_HOME` must be worker-specific
- `CODEX_HOME` is the isolation and override boundary for Codex state
- auth state must stay profile-scoped
- session state must not be inferred from terminal state

### 5. tmux role

tmux is optional operator tooling, not a requirement.

It is for:
- attach
- scrollback
- recovery
- manual inspection
- operator continuity

It may be absent:
- in the first daemon/runtime extraction
- in automated-only workers
- on hosts where operators do not want a persistent terminal surface

It is not:
- the system of record
- the machine protocol
- the canonical session identity

The control plane remains authoritative.

## Control-plane transport design

### `si` to `si-nucleus`

Use:
- WebSocket only
- one typed request/response and event-stream protocol over WebSocket frames

Why:
- one transport for local clients and future remote access if ever needed
- same message contract for CLI and other Nucleus clients
- aligns better with a gateway-style control plane
- avoids split transport design between local and remote use cases

WebSocket is the transport.
The message protocol is the contract.

The local default should still bind to loopback only unless explicitly configured otherwise.
Nucleus should not expose itself publicly by default.

### Protocol shape

Use typed envelopes with two modes:
- request/response
- event notification

Examples of request methods:
- `nucleus.status`
- `task.create`
- `task.list`
- `worker.list`
- `worker.inspect`
- `session.create`
- `session.show`
- `run.submit_turn`
- `run.cancel`
- `events.subscribe`

Examples of event types:
- `task.created`
- `worker.ready`
- `worker.failed`
- `session.created`
- `run.started`
- `run.output_delta`
- `run.requires_auth`
- `run.completed`
- `run.failed`

### WebSocket wire schema

Use one envelope shape for all gateway traffic.

Request shape:
- `id`
- `method`
- `params`

Response shape:
- `id`
- `ok`
- `result` or `error`

Error object shape:
- `code`
- `message`
- optional `details`

Event shape:
- `event_id`
- `seq`
- `ts`
- `type`
- `source`
- `data`

Canonical event `data` envelope:
- optional `task_id`
- optional `worker_id`
- optional `session_id`
- optional `run_id`
- optional `profile`
- event-specific `payload`

Rules:
- request ids are client-provided and echoed back in responses
- events are server-pushed and never reuse request ids
- `source` names where the event came from, while `type` names the normalized SI event
- the canonical event `data` envelope should keep common object references at the top level and place event-specific fields under `payload`
- the same wire shape is used by CLI and other gateway clients

### Canonical event granularity

Keep the durable ledger useful and low-noise.

Persist durably:
- worker lifecycle changes
- session lifecycle changes
- run lifecycle changes
- task lifecycle changes
- auth and account signals
- meaningful tool or approval signals
- output milestones such as `run.output_delta` when they are useful for replay and inspection

Do not persist every low-level runtime frame or tiny transient update as its own canonical event.
Live subscribers may still receive finer-grained updates than the durable ledger keeps.

### Event sequence allocation

Nucleus allocates one global monotonically increasing `seq` for the canonical ledger at append time.

Rules:
- there is one sequence space for all canonical events
- do not use per-source or per-worker sequences as authoritative ordering
- canonical append is the point where ordering becomes official

## Persistence format rules

Nucleus should use format-by-fit persistence rather than forcing one file format everywhere.

This is the explicit rule:
- Markdown for human-facing plans, summaries, and durable operator notes
- JSON for canonical structured object state
- JSONL for append-only event and history streams

Do not introduce:
- SQLite
- PostgreSQL
- embedded KV stores

Why this is the right balance:
- operators still get human-readable Markdown where that matters
- structured state stays easy to validate and update atomically in JSON
- high-volume event history stays append-friendly in JSONL

The durable source of truth on disk may therefore be Markdown, JSON, or JSONL depending on object type, but not a database.

## Namespacing rules

All Nucleus-owned runtime resources must be explicitly namespaced.

Do not use generic names like:
- `si`
- `daemon`
- `worker`
- `session`
- `run`
- `socket`
- `tmux`

The goal is to prevent:
- collisions with adjacent repos
- operational ambiguity in logs and tooling
- stale cross-repo coupling
- resource leaks that are hard to attribute

### Required naming prefixes

Use these prefixes consistently:
- binary/service: `si-nucleus`
- internal concept prefix: `nucleus`
- state root prefix: `nucleus-`
- runtime object prefixes:
  - `si-worker-`
  - `si-session-`
  - `si-run-`
  - `si-event-`

### Local filesystem layout

Use a dedicated state root under:
- `~/.si/nucleus/`

Recommended layout:
- `~/.si/nucleus/run/`
- `~/.si/nucleus/logs/`
- `~/.si/nucleus/workers/`
- `~/.si/nucleus/sessions/`
- `~/.si/nucleus/tmp/`
- `~/.si/nucleus/state/`
- `~/.si/nucleus/gateway/`

Recommended concrete paths:
- pid file: `~/.si/nucleus/run/nucleus.pid`
- lock file: `~/.si/nucleus/run/nucleus.lock`
- Markdown state root: `~/.si/nucleus/state/`
- gateway metadata: `~/.si/nucleus/gateway/`

Do not place Nucleus runtime files directly under:
- `~/.si/`
- `/tmp/`
- generic repo-local temp paths

unless a clearly named Nucleus subpath is used.

### Worker state paths

Each worker should get a namespaced directory:
- `~/.si/nucleus/workers/<worker-id>/`

Recommended contents:
- `codex-home/`
- `logs/`
- `state.json`
- `runtime.json`
- `summary.md`

Example:
- `~/.si/nucleus/workers/si-worker-01HXYZ.../codex-home/`

### ID shapes

IDs should be opaque and stable, but visibly namespaced:
- worker id: `si-worker-<opaque>`
- session id: `si-session-<opaque>`
- run id: `si-run-<opaque>`
- event id: `si-event-<opaque>`

Do not expose raw tmux session names, PIDs, or Codex thread ids as SI primary ids.

### Optional tmux names

If tmux is enabled, tmux resources must also be namespaced:
- tmux session name: `si:nucleus:<worker-id>`
- tmux window name: `nucleus:<profile>`

tmux names are operator-facing only.
They are not authoritative ids.

### WebSocket gateway naming

Use:
- service identity: `nucleus gateway`
- gateway path prefix: `/ws`

Recommended local default:
- `ws://127.0.0.1:<port>/ws`

Do not expose generic or unstable paths like:
- `/daemon`
- `/socket`
- `/tmux`
- `/app-server/raw`

unless they are deliberately debug-only and clearly marked as unstable.

### Environment variable ownership

Use SI-owned names for SI-specific runtime settings.

Recommended examples:
- `SI_NUCLEUS_WS_ADDR`
- `SI_NUCLEUS_STATE_DIR`
- `SI_NUCLEUS_BIND_ADDR`
- `SI_NUCLEUS_LOG_LEVEL`

Use `CODEX_HOME` only for Codex state isolation.
Do not invent SI-specific aliases for Codex's own state root.

### Service management naming

If a service unit is introduced later, use:
- `si-nucleus.service`

If timers or companion units appear later, namespace them the same way:
- `si-nucleus-reconcile.service`
- `si-nucleus-reconcile.timer`

### Boundary rule for adjacent repos

Adjacent repos may depend on:
- documented CLI commands
- documented WebSocket protocol and gateway endpoint
- documented config fields

Adjacent repos must not depend on:
- Nucleus bind address internals unless explicitly documented
- worker directory names
- tmux session names
- local file layouts beyond the stable public contract
- temporary/debug resource names

## Main gateway surface

Nucleus should expose a WebSocket gateway for external clients such as:
- future web UI surfaces
- local dashboards
- adjacent trusted tools
- internal SI clients

This gateway is the Nucleus runtime-facing API surface.
It is the main control-plane API surface.

### Gateway principles

- Nucleus remains the source of truth
- WebSocket frames carry the same typed request/response and event model used by the CLI
- local and external clients should speak the same gateway protocol
- the gateway should not bypass the store or task/runtime logic
- the gateway should remain simple enough for stable client implementations

### Why WebSocket belongs in the plan

WebSocket is the right outward-facing adapter for:
- the local CLI
- future remote or browser-facing clients
- dashboards and operator surfaces
- a unified gateway-style control plane

WebSocket is preferred because:
- it keeps one transport everywhere
- streaming runtime/event semantics fit naturally
- it mirrors the shape used by OpenClaw's gateway
- it avoids maintaining parallel local and external control APIs

### External integration REST API

After the core system is implemented, accepted, and integration-tested end to end, SI may add an annotated REST API for external clients such as ChatGPT GPT Actions.

Rules:
- this REST API is not the primary control-plane transport
- WebSocket remains the main Nucleus transport for CLI and real-time clients
- the REST API exists to replace the need for MCP integration for external tool-style consumers
- the REST API must be generated from the same canonical task, worker, session, run, event, and profile model used by Nucleus
- the REST API must not bypass Nucleus logic or create a second source of truth

Annotation requirements:
- every endpoint must include summary and description
- every endpoint must define request and response schema
- endpoints should include explicit notes about what they are meant to be used for
- the API should be OpenAPI-compatible so GPT Actions can consume it cleanly

Initial external-use operations should stay bounded:
- `GET /status`
- `POST /tasks`
- `GET /tasks`
- `GET /tasks/{task_id}`
- `POST /tasks/{task_id}/cancel`
- `GET /workers`
- `GET /workers/{worker_id}`
- `GET /sessions/{session_id}`
- `GET /runs/{run_id}`

### Gateway API scope

The first gateway should expose bounded operations like:
- `nucleus.status`
- `task.create`
- `task.list`
- `task.inspect`
- `task.cancel`
- `worker.list`
- `worker.inspect`
- `worker.restart`
- `worker.repair_auth`
- `session.create`
- `session.show`
- `session.list`
- `run.submit_turn`
- `run.cancel`
- `run.inspect`
- `events.subscribe`
- `profile.list`

The first gateway should not try to expose:
- raw App Server methods
- terminal/tmux internals
- direct transcript file access
- direct `auth.json` mutation
- low-level debug-only transport internals

## Worker model

### Worker per profile/account

Default policy:
- one worker process per profile/account
- one isolated `CODEX_HOME` per profile/account

This keeps:
- ChatGPT-account isolation
- auth cache isolation
- session transcript isolation
- worker recovery simple

## Storage model

Use file-based persistence with Markdown, JSON, and JSONL.

Do not use SQLite or any database layer for Nucleus state.

Why this is the plan:
- operators should be able to inspect and repair state directly on disk
- structured objects should still be easy to validate and update atomically
- append-heavy event streams should not be forced into awkward Markdown edits

Persistence rules:
- JSON is the canonical format for durable structured objects
- JSONL is the canonical format for append-only event and history streams
- Markdown is used for human-facing summaries, plans, projections, and operator-friendly views

Recommended state layout:
- `~/.si/nucleus/state/tasks/`
- `~/.si/nucleus/state/workers/`
- `~/.si/nucleus/state/sessions/`
- `~/.si/nucleus/state/runs/`
- `~/.si/nucleus/state/events/`
- `~/.si/nucleus/state/profiles/`
- `~/.si/nucleus/state/producers/cron/`
- `~/.si/nucleus/state/producers/hook/`

Recommended file shape by object type:
- task: `task.json`
- worker: `state.json`, `runtime.json`, optional `summary.md`
- session: `session.json`, optional `summary.md`
- run: `run.json`, optional `summary.md`
- event stream: `events.jsonl`
- profile: `profile.json`
- cron producer state: `<rule-name>.json`
- hook producer state: `<rule-name>.json`

Important persisted fields:
- task id and task status
- task source
- task instructions
- task checkpoint fields when present
- blocked reason when present
- worker id
- profile
- `CODEX_HOME`
- session id
- Codex thread id
- run id
- App Server turn id where applicable
- event sequence
- last heartbeat or last activity

Recommended conventions:
- JSON objects should be canonical, typed, and easy to rewrite atomically
- JSONL should be append-only and ordered by sequence
- `events.jsonl` is the single canonical SI event ledger
- all incoming event sources must be normalized before append to `events.jsonl`
- do not create separate raw source logs, raw source ledgers, or parallel authoritative event streams
- Markdown should be reserved for human-readable status, summaries, and dashboards
- generated summaries are optional convenience views and not the source of truth

## Codex account model

SI should treat multi-account as a first-class scheduler concern.

Recommended model:
- profile maps to one account identity
- account identity maps to one `CODEX_HOME`
- the lowercase profile name is the stable identifier everywhere
- one active worker per profile by default
- orchestrator chooses worker/profile before starting or reusing a session

Do not try to make one Codex App Server juggle many accounts.
That is not the right boundary.

Additional conclusions from the multi-account research:
- Codex CLI does not currently provide first-class named multi-account switching as a user feature
- SI should not treat `auth.json` as the system of record
- `CODEX_HOME` is the correct practical account-isolation boundary
- future host-managed auth/token injection can be added on top of Nucleus, but is not required for the first implementation

## Adjacent integration contract

Keep adjacent-tool integration explicit and narrow.

Rules:
- adjacent tools must integrate through documented Nucleus or worker-facing surfaces, not through hidden state files or internal implementation details
- adjacent integrations must not create a second orchestration model beside tasks, sessions, runs, events, and profiles
- integration results must be projected back into the canonical SI event ledger
- capability checks and failure states for adjacent tools should become normal SI events and blocked reasons where relevant

## Fort integration contract

Fort should be planned now because secret and auth access is part of real worker execution.

Rules:
- Codex workers should access secret or auth material through the documented `si fort ...` surface or Fort service contract, not by reading vault internals directly
- Nucleus should treat Fort as an adjacent capability, not as a new orchestration primitive
- Fort availability, failure, and auth-related outcomes should be normalized into canonical SI events
- Fort-related task blocking should use normal `blocked` task state plus stable blocked reasons such as `auth_required` or `fort_unavailable`

Recommended integration shape:
- Nucleus or the worker runtime determines that a task or run requires Fort-backed access
- the worker uses the documented Fort-facing surface
- the outcome is normalized back into canonical SI events and task/run projection state

Boundaries:
- do not make Fort routing a first-class scheduler primitive
- do not let adjacent repos depend on Nucleus internals to reach Fort
- do not make Fort state files part of the Nucleus source of truth

## Instruction packaging guidance

`AGENTS.md`, skills, or `SKILL.md` files may be useful for teaching Codex when and how to use Fort, but they are not the architecture contract.

Rules:
- instruction files are an optional packaging and ergonomics layer
- the actual integration contract must still be implemented in documented commands, runtime behavior, events, and error handling
- correctness of Nucleus and worker behavior must not depend on a particular skill file being present

## Additional lessons from the broader landscape

There is no single public repo that already matches SI exactly.
The right move is to compose the strongest patterns from several repos instead of copying one project wholesale.

Most useful lessons to retain:
- from OpenClaw: control-plane-first architecture
- from cli-agent-orchestrator: explicit control-plane surfaces matter
- from codex-app-server-client-sdk: App Server deserves a clean client layer
- from tmux-agent-status and opensessions: operator UX matters, but should not become runtime truth

## What SI should build next

### Phase 1: domain core

Create `si-nucleus-core`.

It should contain:
- task, worker, session, run, event, and profile types
- ids
- state transitions
- event enums
- task status enums
- App Server mapping types for thread, turn, item, account, and config projections

### Phase 2: nucleus service

Create `si-nucleus`.

It should contain:
- WebSocket gateway server
- request dispatch
- event fanout
- worker supervision
- reconciliation loop
- task intake loop
- cron producer loop
- hook producer loop
- producer state reconciliation
- file-based persistence conventions
- JSON object readers and writers
- JSONL append and rotation helpers
- Markdown projection and summary writers
- lookup and indexing helpers over file-based state
- recovery helpers

### Phase 3: runtime boundary

Create `si-nucleus-runtime`.

It should define:
- runtime trait
- worker startup contract
- App Server client contract
- session/thread mapping contract
- run/turn mapping contract
- event normalization contract
- stop/restart semantics

### Phase 4: Codex runtime implementation

Create `si-nucleus-runtime-codex`.

It should implement:
- local process launch of `codex app-server`
- worker env shaping
- `CODEX_HOME` management
- App Server request/response handling
- event normalization
- interrupt/cancel behavior

### Phase 5: CLI integration

Update `si` CLI so that:
- `si codex ...` is a worker/runtime-facing surface
- `si nucleus ...` becomes the control-plane-facing surface
- `si nucleus task ...`, `si nucleus session ...`, and `si nucleus run ...` become the main orchestration surfaces
- `si codex tmux` remains an optional operator surface if enabled
- profile commands manage auth and worker identity, not image/runtime metadata

### Phase 6: Fort integration

Only start this phase after phases 1 through 5 are accepted.

It should contain:
- documented worker-facing Fort access path
- Fort capability and failure normalization into canonical SI events
- blocked-reason projection for Fort-related failures
- tests that verify worker tasks can use Fort without depending on hidden state or manual operator steps

### Phase 7: annotated REST API for external integrations

Only start this phase after phases 1 through 6 are accepted and end-to-end integration testing is passing.

It should contain:
- REST handlers over the canonical Nucleus model
- OpenAPI-compatible schema generation
- endpoint summaries and descriptions
- explicit endpoint-use annotations for external tool consumers
- GPT Actions-oriented endpoint coverage for bounded task and inspection operations

## Implementation order discipline

Build the system in phase order.

Rules:
- do not start later phases until the earlier phase is implemented enough to satisfy its acceptance criteria
- do not start Fort integration before phases 1 through 5 are accepted
- do not start the annotated REST API before phases 1 through 6 are accepted and end-to-end integration testing is passing
- avoid speculative implementation of later integrations before the core runtime and gateway contracts are stable

## Implementation work-item tracking

Use a small status vocabulary for plan execution.

Allowed work-item statuses:
- `planned`
- `active`
- `accepted`
- `dropped`

Current planned work items:
- Phase 1 `si-nucleus-core`: `accepted`
- Phase 2 `si-nucleus`: `accepted`
- Phase 3 `si-nucleus-runtime`: `accepted`
- Phase 4 `si-nucleus-runtime-codex`: `accepted`
- Phase 5 CLI integration: `accepted`
- Phase 6 Fort integration: `accepted`
- Phase 7 annotated REST API: `accepted`

Implementation notes:
- `[implementation-note:nucleus-phase1-2026-04-05]` Phase 1 landed in `rust/crates/si-nucleus-core` with canonical task, worker, session, run, event, and profile types; explicit transition validation; and App Server thread, turn, item, account, and config projection types.
- `[implementation-note:nucleus-phase1-task-id-prefix-2026-04-05]` The implementation currently uses namespaced task ids in the form `si-task-<opaque>` for symmetry with the explicitly namespaced worker, session, run, and event ids. This is an additive implementation choice, not a replacement for the ticket contract.
- `[implementation-note:nucleus-phase2-2026-04-05]` Phase 2 is active locally with the first `si-nucleus` service crate, a file-backed state layout rooted under `~/.si/nucleus/`, canonical `events.jsonl` append/reload behavior, typed gateway envelopes, and an initial WebSocket `/ws` control-plane path for `nucleus.status`, `task.create`, `task.list`, `task.inspect`, `profile.list`, and `events.subscribe`.
- `[implementation-note:nucleus-phase3-2026-04-05]` Phase 3 is active locally with `rust/crates/si-nucleus-runtime`, which now defines the runtime launch contract, worker probe result shape, canonical event draft shape, and the trait Nucleus uses without importing Codex-specific code directly.
- `[implementation-note:nucleus-phase4-2026-04-05]` Phase 4 is active locally with `rust/crates/si-nucleus-runtime-codex`, which now shapes `codex app-server` launch commands, owns live worker processes per `worker_id`, performs `initialize`, `account/rateLimits/read`, `account/read`, and `config/read`, and drives `thread/start`, `thread/resume`, `turn/start`, and `turn/interrupt` through the shared runtime interface.
- `[implementation-note:nucleus-phase4-app-server-only-coverage-2026-04-06]` The runtime transport boundary is now pinned more explicitly too. `si-nucleus-runtime-codex` tests assert that worker launch uses exactly `codex app-server` with no extra transport verb and no hidden `codex exec` fallback argument, which keeps the Phase 4 runtime aligned with the plan’s App Server only contract.
- `[implementation-note:nucleus-phase5-2026-04-05]` Phase 5 is active locally with `si nucleus ...` CLI coverage over the WebSocket gateway for `status`, `task create|list|inspect`, `worker probe|list|inspect`, `session create|list|show`, and `run submit-turn|inspect|cancel`.
- `[implementation-note:nucleus-phase3-accepted-2026-04-05]` Phase 3 is now accepted locally. `rust/crates/si-nucleus-runtime` defines the stable runtime-owned worker/session/run contract that `si-nucleus` uses directly without importing Codex-specific code.
- `[implementation-note:nucleus-phase4-session-run-2026-04-05]` Phase 4 now includes live worker process ownership inside `rust/crates/si-nucleus-runtime-codex`, `thread/start` and `thread/resume` session handling, `turn/start` execution, `turn/interrupt` support, and normalization of `item/agentMessage/delta` plus run lifecycle notifications into SI canonical events.
- `[implementation-note:nucleus-phase5-session-run-2026-04-05]` Phase 5 now includes `si nucleus session create|list|show` and `si nucleus run submit-turn|inspect|cancel` over the WebSocket gateway. Runs are task-first: `run.submit_turn` now requires a durable `task_id` so SI does not create hidden work objects outside the task ledger.
- `[implementation-note:nucleus-phase2-dispatcher-2026-04-06]` Phase 2 now includes durable queued-task selection directly from the task ledger, one background dispatcher/supervision loop inside `si-nucleus`, profile-based worker/session reuse, and stable session-affine backlog enforcement so only the head queued task for a given `session_id` is eligible at a time.
- `[implementation-note:nucleus-phase2-recovery-2026-04-06]` Phase 2 now persists enough worker and session launch metadata to restart workers and prove reusable sessions without CLI coupling. Startup reconciliation marks ambiguous in-flight runs into explicit blocked state, while steady-state supervision only reconciles active runs when the worker/session attachment is actually broken.
- `[implementation-note:nucleus-worker-account-boundary-coverage-2026-04-06]` The one-worker-per-profile/account contract now has direct regression coverage too. `si-nucleus` tests assert that repeated `session.create` calls for the same lowercase profile reuse the same active worker, keep a single persisted worker record, and preserve the original worker `CODEX_HOME` instead of silently creating a second worker/account lane for the same profile.
- `[implementation-note:nucleus-session-conflict-reuse-coverage-2026-04-06]` The session-reuse conflict rule now has direct regression coverage and live CLI coverage too. `si-nucleus` tests assert that `session.create` does not reuse a session that still has a conflicting active run, and the live `si nucleus session create` matrix now pins the same rule against a running service: Nucleus opens a new session on the same profile lane while the original run remains active instead of silently reusing the busy session.
- `[implementation-note:nucleus-session-lane-reuse-live-coverage-2026-04-06]` The default same-profile session lane behavior now also has direct live CLI coverage. Against a running Nucleus service, two plain `si nucleus session create america ...` calls now reuse the same worker lane, preserve the first recorded `codex_home` for that worker, and create two durable sessions without silently starting a second worker for the same profile.
- `[implementation-note:nucleus-worker-selection-tiebreak-coverage-2026-04-06]` The deterministic worker-selection tiebreak rule now has both direct regression coverage and live CLI coverage. When multiple equal live worker candidates exist for the same profile, `si-nucleus` tests prove `session.create` picks the stable lexical `worker_id`, and the live `si nucleus session create` surface now pins the same rule against a running service rather than starting a third worker or selecting a nondeterministic candidate.
- `[implementation-note:nucleus-profile-not-ready-blocking-2026-04-06]` The worker-selection fallback for an unready profile now also has direct regression coverage. When worker startup fails for a queued task’s selected profile, `si-nucleus` now asserts that the task becomes `blocked` with the stable `worker_unavailable` reason instead of remaining silently queued or creating a partial run.
- `[implementation-note:nucleus-profile-routing-mismatch-2026-04-06]` Session-affine routing now rejects cross-profile reuse explicitly on both the queued-dispatch and direct-run paths. `si-nucleus` now blocks a queued task with `session_broken` when the task’s explicit profile does not match the bound profile of its referenced session, preserves the original session profile instead of silently rebinding that session into a different account lane, and regression coverage now also pins both the queued task-routing branch and the direct `run.submit_turn` rejection through the live `si nucleus ...` surfaces.
- `[implementation-note:nucleus-session-reference-failure-coverage-2026-04-06]` The simpler session-reuse failure branches now also have direct dispatcher coverage and live task-surface coverage. `si-nucleus` tests assert that a queued task becomes `blocked` with `session_broken` when it references a missing session or when it is queued behind a session that has already been marked non-reusable, and the live `si nucleus` task matrix now pins both branches against a running service so recovery does not silently attach new work to invalid session state.
- `[implementation-note:nucleus-session-thread-recovery-2026-04-06]` The session-recovery contract now also covers the missing-thread branch directly. When a referenced persisted session has lost its App Server thread id, `si-nucleus` now marks that session `broken` and blocks the queued task with `session_broken` instead of leaving the session apparently reusable, and the live `si nucleus` task-routing surface now pins the same recovery branch against a running service.
- `[implementation-note:nucleus-direct-run-thread-recovery-2026-04-06]` The same missing-thread rule now also holds on the direct `run.submit_turn` path. `si-nucleus` now refuses to claim run state when the chosen session has no App Server thread id, marks that session `broken`, blocks the task with `session_broken`, and leaves no stray run record behind. The live `si nucleus run submit-turn` surface now pins the same direct-run recovery branch against a running service too.
- `[implementation-note:nucleus-phase2-checkpoint4-2026-04-06]` Checkpoint 4 is satisfied locally: task routing, worker selection, session reuse, and session backlog rules now work together in unit coverage and through a live service-path smoke where a queued task is created over the gateway and auto-dispatched to completion through a reusable Codex session.
- `[implementation-note:nucleus-phase2-transition-gap-2026-04-06]` The plan’s listed task/run transition table did not include reconciliation from queued state, but startup/runtime failure handling needs explicit non-ambiguous outcomes before `run.started` can exist. The implementation therefore allows `queued -> blocked` and `queued -> failed` for both task and run state as a narrow recovery-only extension of the transition contract.
- `[implementation-note:nucleus-phase2-producers-2026-04-06]` Phase 2 now includes durable `cron` and `hook` producer loops inside `si-nucleus`. Rules are persisted under `state/producers/{cron,hook}/<rule>.json`, due or matching emissions create normal tasks with `task.source = cron|hook`, and replay-safe dedup is bound onto the task itself through `producer_rule_name` and `producer_dedup_key`.
- `[implementation-note:nucleus-phase2-producer-replay-2026-04-06]` Producer state advancement now follows durable task creation: cron rules advance `last_emitted_at` and `next_due_at` only after task creation is durable, and hook rules advance `last_processed_event_seq` only after durable task creation or durable duplicate detection succeeds. This preserves at-least-once replay with restart-safe dedup rather than silent task loss.
- `[implementation-note:nucleus-phase2-producer-crash-window-coverage-2026-04-06]` The producer failure window from the verification plan now has direct regression coverage too. `si-nucleus` tests simulate the crash boundary where a cron or hook producer task already exists durably but the producer rule has not yet advanced its own state; replay then suppresses duplicate effective task creation through `producer_dedup_key` and still advances `last_emitted_at` or `last_processed_event_seq` correctly on the next pass.
- `[implementation-note:nucleus-persisted-version-policy-2026-04-06]` Persisted producer-rule JSON now follows the plan’s single-version policy instead of carrying an independent numeric schema marker. Cron and hook rules write the current SI repository version string, Nucleus still reads the older numeric `1` form for compatibility, and the next producer pass rewrites that legacy state forward to the current SI version through the normal atomic file-update path.
- `[implementation-note:nucleus-naming-grammar-coverage-2026-04-06]` The plan’s lowercase-slug naming grammar is now pinned at the control-plane, live CLI, and producer-failure layers. `si-nucleus` tests assert that `task.create` and `session.create` reject non-slug profile names such as `America`, the live `si nucleus task create` and `si nucleus session create` surfaces now reject the same invalid profile names without creating durable work state, and invalid persisted cron or hook rule names emit durable `system.warning` events and do not create producer tasks.
- `[implementation-note:nucleus-rest-naming-grammar-coverage-2026-04-06]` The same lowercase-slug naming rule now also has direct live coverage through the bounded REST intake surface. Against a running Nucleus service, `POST /tasks` rejects a non-slug profile such as `America` with HTTP `400`, `error.code = invalid_params`, and a clear grammar message, while leaving durable task state empty.
- `[implementation-note:nucleus-openapi-invalid-params-coverage-2026-04-06]` The published OpenAPI contract is now pinned to that same invalid-profile behavior too. Both `si-nucleus` and the live `/openapi.json` smoke now assert that `POST /tasks` advertises a canonical `400` `RestErrorEnvelope` response alongside its existing success and auth responses, keeping the bounded REST document aligned with the real service behavior for invalid profile input.
- `[implementation-note:nucleus-public-surface-allowlist-2026-04-06]` The public `si nucleus` command surface is now pinned to the bounded first-gateway allowlist from this plan. CLI regression coverage asserts that `si nucleus --help` exposes only the documented public nouns (`status`, `profile`, `service`, `task`, `worker`, `session`, `run`, and `events`) and does not surface forbidden raw-App-Server or terminal/auth-file nouns such as `thread`, `turn`, `tmux`, `transcript`, or `auth-json`.
- `[implementation-note:nucleus-gateway-raw-method-rejection-2026-04-06]` The bounded first-gateway rule is now pinned at the service boundary too. `si-nucleus` regression coverage asserts that raw App Server verbs such as `thread.start`, `thread.resume`, `turn.start`, and `turn.interrupt` return `method_not_found` through the public gateway instead of being accepted as contract methods.
- `[implementation-note:nucleus-canonical-event-name-coverage-2026-04-06]` Canonical event-name normalization is now pinned at the persisted Nucleus boundary too. `si-nucleus` regression coverage drives a real run to completion, then asserts that the durable ledger contains SI event names such as `run.started`, `run.output_delta`, and `run.completed` and does not expose raw App Server names such as `item/agentMessage/delta`, `turn/start`, or `turn/interrupt`.
- `[implementation-note:nucleus-single-ledger-coverage-2026-04-06]` The single-ledger rule is now pinned end to end too. `si-nucleus` regression coverage drives a real run, then asserts that the canonical event stream is persisted only under `state/events/events.jsonl` for the non-rotated case and that no parallel event ledger appears under `logs/` or as an extra raw-source JSONL file.
- `[implementation-note:nucleus-primary-id-boundary-coverage-2026-04-06]` The SI primary-id boundary is now pinned at runtime-backed state too. `si-nucleus` regression coverage asserts that worker, session, run, and event ids remain namespaced (`si-worker-`, `si-session-`, `si-run-`, `si-event-`) while the persisted App Server thread and turn ids stay distinct runtime-side identifiers instead of becoming SI primary ids.
- `[implementation-note:nucleus-phase2-routing-fallback-2026-04-06]` Producer-created tasks do not choose a profile. To keep them runnable without inventing a second routing primitive, Nucleus now resolves an omitted task profile only when there is one unambiguous known profile lane from persisted profile state, live workers, or sessions; otherwise the task remains queued until routing is unambiguous or operator input changes the state.
- `[implementation-note:nucleus-phase2-markdown-summaries-2026-04-06]` Phase 2 now writes derived `summary.md` projections beside persisted `worker`, `session`, and `run` JSON state. These markdown files are operator-facing views only: JSON remains canonical, while Nucleus regenerates the summaries from durable state on each write and now projects normalized runtime output into `session.summary_state` so session/run summaries expose the latest checkpoint or final output without replaying the event ledger.
- `[implementation-note:nucleus-phase2-projection-rebuild-2026-04-06]` Startup recovery now rebuilds the derived markdown projections from canonical JSON state. If `summary.md` files are missing, Nucleus recreates them on open for workers, sessions, and runs before the service resumes, so restart recovery restores the operator-facing projections without changing the canonical ledger or object files.
- `[implementation-note:nucleus-markdown-nonauthoritative-coverage-2026-04-06]` Markdown projections are now pinned as non-authoritative convenience views too. `si-nucleus` regression coverage overwrites persisted `worker`, `session`, and `run` `summary.md` files with divergent content, then proves startup rewrites them back from canonical JSON state and preserves the real completed run state.
- `[implementation-note:nucleus-phase2-state-corruption-2026-04-06]` Startup recovery now scans the remaining canonical JSON object sets that do not produce markdown projections directly, including tasks, profiles, and persisted producer rules. Malformed objects are isolated instead of aborting startup, and Nucleus appends a durable `system.warning` event describing the corrupted path so operator repair remains explicit and restart-safe.
- `[implementation-note:nucleus-projection-corruption-coverage-2026-04-06]` Startup-corruption coverage now also pins the projection-rebuild path for persisted session state. In addition to malformed task objects under the direct state scan, `si-nucleus` tests now assert that malformed `state/sessions/.../session.json` content is isolated into a durable `system.warning` event instead of aborting startup, which proves the same operator-repair contract holds for projection-backed session objects too.
- `[implementation-note:nucleus-event-ledger-corruption-failure-2026-04-06]` The canonical event-ledger corruption branch now has direct startup coverage too. `si-nucleus` tests assert that malformed `state/events/events.jsonl` content aborts startup with a clear parse error that names `events.jsonl` and the failing line, rather than being isolated like ordinary object corruption. This pins the plan rule that unreadable canonical ledger state requires explicit operator repair.
- `[implementation-note:nucleus-phase2-worker-restart-policy-2026-04-06]` Worker supervision now owns the default idle-worker restart policy directly inside `si-nucleus`. When a persisted worker disappears from the runtime with no active runs attached, Nucleus marks it failed and then performs bounded automatic restart attempts with exponential backoff; successful restarts clear the retry state, while repeated failures stay failed and emit explicit durable warnings instead of looping forever or delegating restart policy to OS service units.
- `[implementation-note:nucleus-restart-exhaustion-repair-boundary-2026-04-06]` Exhausted automatic worker restarts now stay behind the explicit operator-repair boundary. Once bounded idle-worker restart attempts are exhausted, normal task dispatch and session reuse no longer implicitly call `start_worker` again for that failed profile lane; affected tasks instead block with `worker_unavailable` until an explicit `worker.restart` or `worker.repair_auth` action succeeds, and regression coverage now pins both the blocked pre-repair state and the successful post-restart re-queue path.
- `[implementation-note:nucleus-restart-exhaustion-repair-auth-coverage-2026-04-06]` The parallel explicit repair path is now pinned too. After bounded automatic restart attempts are exhausted for a worker lane, new `session.create` requests now fail clearly until an explicit `worker.repair_auth` succeeds; once that repair action completes, the same profile lane becomes usable again and both direct `si-nucleus` and live `si nucleus ...` coverage now pin that exhausted-boundary recovery path.
- `[implementation-note:nucleus-worker-restart-guard-coverage-2026-04-06]` The manual restart guard now has direct gateway coverage and live CLI coverage too. `si-nucleus` tests assert that `worker.restart` refuses to restart a worker that still owns an active run, and the live `si nucleus worker restart` matrix now pins the same failure path against a running service so operator-triggered restarts do not silently disrupt in-flight work before that run is cancelled or reconciled.
- `[implementation-note:nucleus-phase2-event-rotation-2026-04-06]` The canonical event ledger now supports rotation without breaking recovery. When the active `events.jsonl` grows past the internal rotation threshold, Nucleus rolls it into timestamped sibling logs under `state/events/` and continues appending to a fresh `events.jsonl`; startup replay and sequence recovery now read both rotated logs and the active ledger in order so summaries and durable object state remain reconstructable after rotation.
- `[implementation-note:nucleus-phase2-task-prune-2026-04-06]` The retention policy now has an explicit operator surface through `task.prune` and `si nucleus task prune`. Nucleus prunes only old completed or failed task records by default, using a 30-day cutoff unless the caller overrides it, and it leaves active worker/session/run state untouched so cleanup stays conservative and explicit as required by the plan.
- `[implementation-note:nucleus-phase2-accepted-2026-04-06]` Phase 2 is now accepted locally. The WebSocket gateway, durable task ledger, queued-task dispatcher, worker/session supervision, startup reconciliation, and restart-safe cron/hook producers all operate from the same file-backed state and canonical event ledger without direct CLI coupling.
- `[implementation-note:nucleus-phase4-cancel-2026-04-06]` Phase 4 now has explicit cancel coverage in both isolated runtime tests and a live Codex App Server smoke. A real `run.cancel` against a long-running turn projected both `run.status = cancelled` and `task.status = cancelled`, so the interrupt/cancel behavior is now accepted locally.
- `[implementation-note:nucleus-cancel-recovery-coverage-2026-04-06]` Cancellation now also converges through canonical Nucleus state when the runtime metadata needed for an interrupt is already gone. `si-nucleus` regression coverage now asserts that `run.cancel` cancels queued runs before turn start, that both `run.cancel` and `task.cancel` force an active run to `cancelled`, mark the task `cancelled`, and mark the session `broken` when the persisted App Server thread id has been lost instead of leaving the run active behind a runtime-RPC error, and that both cancel surfaces can also finish the documented `blocked -> cancelled` transition after reconciliation has already projected the run blocked. The live `si nucleus run cancel` and `si nucleus task cancel` surfaces now pin those missing-thread recovery branches against a running service too.
- `[implementation-note:nucleus-cancel-terminality-coverage-2026-04-06]` Cancellation terminality is now pinned directly too. `si-nucleus` regression coverage now asserts that a cancelled task is not re-queued automatically after service restart or a later reconcile pass: Nucleus does not start a worker, does not create a run, and leaves the durable task in `cancelled` until an explicit future operator action changes it.
- `[implementation-note:nucleus-cancel-state-transition-coverage-2026-04-06]` The documented cancellation state transitions are now pinned across all three non-terminal task states. In addition to the queued and running cancellation coverage, `si-nucleus` regression tests now assert that a `blocked` task can transition cleanly to `cancelled` through `task.cancel`, clearing the blocked reason without creating a run or re-queueing the work implicitly, and the live `si nucleus task cancel` surface now pins the same blocked-to-cancelled transition against a running service.
- `[implementation-note:nucleus-blocked-requeue-repair-coverage-2026-04-06]` The remaining `blocked -> queued` transition is now exercised through the existing operator repair actions instead of a new API. After a successful `worker.restart`, `si-nucleus` now re-queues profile-matching tasks blocked only by `worker_unavailable`; after a successful `worker.repair_auth`, it re-queues profile-matching tasks blocked only by `auth_required` or `fort_unavailable`, but leaves tasks blocked behind non-reusable `session_broken` sessions or on other profile lanes untouched. Regression coverage now pins the `worker_unavailable`, `auth_required`, and `fort_unavailable` recovery paths, the broken-session exclusion for both restart and auth repair, and the same-profile-only boundary directly.
- `[implementation-note:nucleus-profile-reprobe-refresh-coverage-2026-04-06]` The operator repair action to re-read account and config state now refreshes persisted profile metadata too. Successful `worker.probe` and `worker.repair_auth` calls now rewrite `state/profiles/<profile>.json` when the effective account identity, preferred model, or related profile-scoped metadata drift, and regression coverage now pins that refresh both directly in `si-nucleus` and through the live `si nucleus profile list` CLI surface after an explicit `worker repair-auth`.
- `[implementation-note:nucleus-phase5-live-repair-cli-coverage-2026-04-06]` The public CLI/live-service matrix now also exercises the operator repair loop end to end. Against a running Nucleus service, `si nucleus worker restart` now unblocks a `worker_unavailable` task into a queued retry that completes, and `si nucleus worker repair-auth` now unblocks both `auth_required` and `fort_unavailable` Fort tasks into queued retries that complete. The live matrix also now pins the negative boundaries: restarting one worker does not unblock blocked tasks on other profile lanes or behind broken sessions, exhausted automatic worker restarts do not get bypassed by a later implicit dispatch attempt, and repairing auth does not unblock tasks still blocked behind `session_broken` or on other profile lanes. The live event ledger is also asserted to contain the intermediate `task.updated` re-queue events, so the user-facing CLI surface now pins the same blocked-to-queued recovery behavior already covered in `si-nucleus`.
- `[implementation-note:nucleus-phase5-accepted-2026-04-06]` Phase 5 is now accepted locally. The CLI orchestration commands operate through Nucleus for create, inspect, submit, and cancel flows, and the explicit live cancel smoke exercised `si nucleus run cancel` against the same source of truth used by the gateway.
- `[implementation-note:nucleus-phase6-fort-2026-04-06]` Phase 6 now routes Fort checks through the documented worker-side Fort profile directory (`<codex_home>/fort` and the persisted Fort session-state contract from `si-rs-fort`) instead of inventing a second Nucleus-owned Fort model. Nucleus projects `fort.ready`, `fort.auth_required`, and `fort.unavailable` into the canonical ledger and blocks Fort-required tasks with stable `auth_required` or `fort_unavailable` reasons before run creation when Fort is not usable.
- `[implementation-note:nucleus-phase6-fort-task-detection-2026-04-06]` The current implementation infers that a task requires Fort-backed access when the durable task title/instructions, or an explicit direct-run prompt, references the documented `si fort` surface. This keeps the decision bound to the documented worker-facing contract without introducing a separate scheduler primitive.
- `[implementation-note:nucleus-phase7-rest-2026-04-06]` Phase 7 now adds a bounded REST surface directly inside `si-nucleus` alongside the websocket gateway: `GET /openapi.json`, `GET /status`, `POST /tasks`, `GET /tasks`, `GET /tasks/{task_id}`, `POST /tasks/{task_id}/cancel`, `GET /workers`, `GET /workers/{worker_id}`, `GET /sessions/{session_id}`, and `GET /runs/{run_id}`. These handlers route through the same Nucleus request logic and durable state used by the websocket control plane instead of creating a second API model.
- `[implementation-note:nucleus-phase7-mutation-status-shapes-2026-04-06]` The bounded REST mutation endpoints now also have direct live status-code and response-shape coverage. Against a running Nucleus service, `POST /tasks` is asserted to return HTTP `201 Created` with the canonical `TaskRecord` shape for a newly queued task, and `POST /tasks/{task_id}/cancel` is asserted to return HTTP `200 OK` with the canonical `TaskCancelResultView` shape while the durable task and run projections converge to `cancelled`.
- `[implementation-note:nucleus-phase7-openapi-2026-04-06]` The REST surface now publishes an OpenAPI 3.1 document with endpoint summaries, descriptions, request and response schemas, and explicit `x-si-purpose` annotations for GPT Actions-style bounded external consumers. Task cancellation is also exposed as a first-class bounded operation through both REST and the websocket method `task.cancel`.
- `[implementation-note:nucleus-service-install-2026-04-06]` The next concrete post-phase plan slice now lives in `si nucleus service ...`. The CLI can generate OS-native user-service definitions for Nucleus (`systemd --user` on Linux and `launchd` agents on macOS), point them at the current `si` binary with `nucleus service run`, and perform `install`, `uninstall`, `start`, `stop`, `restart`, and `status` actions with explicit logs hints instead of leaving service management as ad hoc shell work.
- `[implementation-note:nucleus-service-manager-coverage-2026-04-06]` Service-manager regression coverage now exercises the full documented action set instead of only installation. The CLI harness now asserts `stop`, `restart`, and `uninstall` against `systemd --user`, and it also covers `start`, `status`, `stop`, and `restart` against `launchd`, so both OS-native manager command shapes are now pinned to the accepted `si nucleus service ...` contract.
- `[implementation-note:nucleus-gateway-auth-2026-04-06]` The gateway security model is now enforced on both websocket and REST surfaces: when Nucleus binds beyond loopback, read operations remain available but mutating control-plane operations require a matching bearer token from `SI_NUCLEUS_AUTH_TOKEN`. The REST OpenAPI document now advertises bearer auth on bounded write endpoints, and the `si nucleus ...` CLI forwards the same token on websocket requests when the environment variable is set.
- `[implementation-note:nucleus-docs-2026-04-06]` Public docs are now reconciled with the accepted Nucleus contract. `README.md`, `docs/NUCLEUS.md`, `docs/CLI_REFERENCE.md`, `docs/COMMAND_REFERENCE.md`, `docs/SETTINGS.md`, and `rust/README.md` now describe `si nucleus ...`, OS-native service installation, gateway discovery via `metadata.json`, bounded REST/OpenAPI coverage, and bearer-auth behavior for non-loopback mutation paths.
- `[implementation-note:nucleus-gateway-parity-2026-04-06]` The remaining planned gateway methods are now implemented and exposed through `si nucleus ...`. This closes the earlier gap where `profile.list`, `task.cancel`, and `events.subscribe` existed only partially at the CLI layer and where `worker.restart` / `worker.repair_auth` were not yet implemented. The current control-plane surface now matches the plan’s first-gateway method set without introducing any extra orchestration primitives.
- `[implementation-note:nucleus-phase5-task-cli-coverage-2026-04-06]` CLI regression coverage now includes the foundational `si nucleus task create|list|inspect` path in addition to the newer cancellation, prune, profile, and event flows. This closes the remaining Phase 5 test gap where the basic durable-task gateway surface was implemented but not directly asserted through the CLI harness.
- `[implementation-note:nucleus-phase5-live-task-inspect-coverage-2026-04-06]` The `si nucleus task list|inspect` operator surface now also has direct live service coverage. Against a running Nucleus service, the CLI list command returns multiple persisted tasks across distinct lowercase profiles, and `task inspect` immediately reflects the selected durable task’s profile, terminal status, and checkpoint summary from that same live source of truth.
- `[implementation-note:nucleus-phase5-live-task-prune-coverage-2026-04-06]` The explicit `si nucleus task prune` operator path now also has live service coverage. Against a running Nucleus service, the CLI prune command now removes an aged completed task while leaving an equally old queued backlog task and a recent completed task intact, which pins the documented “old terminal tasks only” retention boundary through the public control-plane surface.
- `[implementation-note:nucleus-phase5-inspect-cli-coverage-2026-04-06]` CLI regression coverage now also exercises the worker, session, and run inspection commands directly over the gateway (`worker.list|inspect`, `session.list|show`, `run.inspect`). This matches the Phase 5 acceptance requirement that CLI inspection use the same source of truth as the control plane rather than relying on indirect store-level tests alone.
- `[implementation-note:nucleus-phase5-live-session-inspect-coverage-2026-04-06]` The `si nucleus session list|show` operator surface now also has direct live service coverage. Against a running Nucleus service, the CLI list command returns multiple persisted sessions across distinct lowercase profiles, and `session show` immediately reflects the same worker and thread binding for the selected live session.
- `[implementation-note:nucleus-phase5-live-profile-list-coverage-2026-04-06]` The `si nucleus profile list` operator surface now also has direct live service coverage. Against a running Nucleus service, the CLI list command reflects multiple persisted lowercase profiles and their current `account_identity` plus `codex_home` values from the same live source of truth used by worker and session routing.
- `[implementation-note:nucleus-phase5-live-run-inspect-coverage-2026-04-06]` The `si nucleus run inspect` operator surface now also has direct live service coverage. Against a running Nucleus service, the CLI inspect command now reflects the completed run selected from live durable task state, including its bound task, bound session, and final `completed` status.
- `[implementation-note:nucleus-phase5-mutation-cli-coverage-2026-04-06]` CLI regression coverage now also asserts the remaining orchestration and mutation paths directly over the gateway: `worker.probe|restart|repair_auth`, `session.create`, and `run.submit_turn|cancel`. This closes the last direct CLI coverage gap for the planned Phase 5 gateway surface without adding any new control-plane primitives.
- `[implementation-note:nucleus-phase5-live-worker-probe-coverage-2026-04-06]` The `si nucleus worker probe` operator path now also has live service coverage. Against a running Nucleus service, the CLI probe call persists a ready worker projection for the requested lowercase profile, the same worker is immediately visible through `worker.inspect`, and the durable `summary.md` projection reflects the probed profile and ready status.
- `[implementation-note:nucleus-phase5-live-worker-inspect-coverage-2026-04-06]` The `si nucleus worker list|inspect` operator surface now also has direct live service coverage. Against a running Nucleus service, the CLI list command returns multiple persisted workers across distinct lowercase profiles, and `worker inspect` immediately reflects the same ready worker projection selected from that live list.
- `[implementation-note:nucleus-phase5-live-worker-restart-success-coverage-2026-04-06]` The successful idle `si nucleus worker restart` path now also has live service coverage. Against a running Nucleus service, restarting an idle worker returns the same `worker_id` in ready state, reprobes the runtime-backed worker successfully, and increments the runtime start count instead of collapsing to a no-op.
- `[implementation-note:nucleus-phase5-live-worker-repair-success-coverage-2026-04-06]` The successful `si nucleus worker repair-auth` path now also has direct live service coverage when the persisted worker exists but its runtime process is missing. Against a running Nucleus service, `repair-auth` reprobes the worker, restarts the runtime-backed process for that same `worker_id`, and returns the worker to ready state instead of leaving the projection detached from the runtime.
- `[implementation-note:nucleus-phase5-live-events-subscribe-coverage-2026-04-06]` The public `si nucleus events subscribe` surface now also has direct live service coverage. Against a running Nucleus service, the CLI subscription command observes canonical `task.created`, `run.started`, `run.output_delta`, and `run.completed` events for a real streamed task run on the lowercase `america` profile instead of only passing through mocked websocket frames.
- `[implementation-note:nucleus-phase5-live-websocket-cli-coverage-2026-04-06]` Phase 5 now also has a live websocket-to-CLI parity smoke. A real Nucleus service is exercised by creating a task through `task.create` over the websocket gateway and then re-reading that task through `si nucleus task inspect` and `si nucleus task list`, proving CLI task inspection/listing reflects the same running gateway state rather than only mocked websocket exchanges.
- `[implementation-note:nucleus-phase5-live-cli-websocket-coverage-2026-04-06]` Phase 5 now also has the inverse live parity smoke. A real Nucleus service is exercised by creating a task through `si nucleus task create` and then re-reading that task through a raw websocket `task.inspect` request, proving CLI-created task state is immediately visible through the running gateway without any CLI-only shortcut path.
- `[implementation-note:nucleus-phase2-producer-cross-surface-coverage-2026-04-06]` Live producer acceptance coverage now exercises both producer paths against a running Nucleus service with runtime-backed dispatch. Cron- and hook-created tasks are observed through `si nucleus task ...` and raw websocket inspection from the same live service, which closes the remaining verification-plan gap where producer emission and replay were covered internally but not yet asserted across the public control-plane surfaces.
- `[implementation-note:nucleus-phase6-live-fort-coverage-2026-04-06]` Fort-backed execution now also has a live service acceptance smoke. A task that references the documented `si fort` surface is executed against a running Nucleus instance with a persisted Fort session under `<codex_home>/fort/session.json`, and the test verifies both successful task completion and durable `fort.ready` event projection in the canonical event ledger.
- `[implementation-note:nucleus-phase6-live-fort-failure-coverage-2026-04-06]` The live Fort matrix now also covers failure paths. A Fort-referencing task blocks with `auth_required` when no persisted Fort session exists, a second task blocks with `fort_unavailable` when the persisted Fort session state is malformed, and the direct `si nucleus run submit-turn` surface now pins the same malformed-session `fort_unavailable` branch while leaving the task unclaimed. All of those paths are exercised against a running Nucleus service and assert the matching durable canonical Fort events.
- `[implementation-note:nucleus-phase7-cross-surface-coverage-2026-04-06]` Phase 7 now has live cross-surface parity coverage for the bounded REST read surface, not just tasks. A single running Nucleus service is exercised by creating and inspecting task, worker, session, and run state over REST, then re-reading those same objects through the raw websocket gateway (`task.inspect`, `worker.inspect`, `session.show`, `run.inspect`) and through `si nucleus ... inspect` commands, proving the annotated REST surface matches the gateway and CLI views from the same source of truth.
- `[implementation-note:nucleus-phase7-list-parity-2026-04-06]` The bounded REST list endpoints now also have direct live parity coverage. A running Nucleus service is exercised with durable tasks and workers across multiple lowercase profiles, and `GET /tasks` plus `GET /workers` are then compared against raw websocket `task.list` and `worker.list` responses and the `si nucleus task list` plus `worker list` CLI views, proving the bounded REST list surface reflects the same source-of-truth records and statuses.
- `[implementation-note:nucleus-phase7-status-parity-2026-04-06]` The bounded REST status surface now also has direct live parity coverage. A running Nucleus service is exercised through `GET /status`, raw websocket `nucleus.status`, and `si nucleus status`, and the regression asserts that all three paths expose the same version, bind address, websocket endpoint, state directory, durable object counts, and next event sequence while matching the persisted `gateway/metadata.json` advertisement from the same service instance.
- `[implementation-note:nucleus-phase7-live-cancel-parity-2026-04-06]` The bounded REST task-cancel mutation now also has direct live parity coverage. Against a running Nucleus service, `POST /tasks/{task_id}/cancel` cancels a live running task, and the returned task/run state now matches the raw websocket `task.inspect` plus `run.inspect` views and the `si nucleus task inspect` plus `run inspect` CLI views from the same source of truth.
- `[implementation-note:nucleus-phase7-live-not-found-coverage-2026-04-06]` The bounded REST error contract now also has direct live coverage for missing-object requests. Against a running Nucleus service, `GET /tasks/{task_id}`, `GET /workers/{worker_id}`, `GET /sessions/{session_id}`, `GET /runs/{run_id}`, and `POST /tasks/{task_id}/cancel` all return `404` with the canonical `RestErrorEnvelope` shape, `error.code = not_found`, and a stable not-found message when the requested durable object does not exist.
- `[implementation-note:nucleus-phase7-live-unavailable-cancel-coverage-2026-04-06]` The bounded REST `503` cancel branch now also has direct live coverage. A real active run is snapshotted from a runtime-backed Nucleus service into a second state root and then reopened without any runtime attached; against that running snapshot service, `POST /tasks/{task_id}/cancel` returns `503` with the canonical `RestErrorEnvelope` and `error.code = unavailable`, while the persisted task and run remain active instead of being silently mutated.
- `[implementation-note:nucleus-phase2-live-backlog-coverage-2026-04-06]` The verification plan now also has a live backlog-serialization smoke against a running Nucleus service. Two tasks targeting the same ready session are created through the CLI, the second is observed remaining `queued` while the first run is active, and the canonical event ledger confirms the second `run.started` occurs only after the first `run.completed`, with both runs bound to the same persisted session.
- `[implementation-note:nucleus-phase2-live-reconnect-coverage-2026-04-06]` WebSocket reconnect behavior during an active run is now covered by a live service test. A client subscribes to the event stream, disconnects after the run becomes active, reconnects through a fresh `events.subscribe`, and still observes `run.completed` while durable CLI task inspection resolves the same task to `done` with the final checkpoint summary from canonical state.
- `[implementation-note:nucleus-phase2-live-streaming-run-coverage-2026-04-06]` The remaining live happy-path run contract now has an explicit acceptance test. A task is routed onto the lowercase `america` profile through a real persisted session and worker, the public event stream exposes `run.started`, `run.output_delta`, and `run.completed`, and the final CLI task, run, session, and worker inspections all agree on the completed projection and checkpoint summary.
- `[implementation-note:nucleus-phase2-live-session-resume-coverage-2026-04-06]` Session resume now has a live acceptance test through `si nucleus session create`. A second session is opened against an existing worker with an explicit persisted thread id, a task then runs successfully through that resumed session, and the resulting task/run/session projections confirm the worker and thread binding stayed stable.
- `[implementation-note:nucleus-phase2-live-session-init-failure-coverage-2026-04-06]` The App Server/session-initialization failure path now has live coverage. A running Nucleus service is given an existing persisted session whose runtime `ensure_session` step then fails, and the public projections converge as required: the queued task blocks with `session_broken`, no run is started, and the session becomes `broken`.
- `[implementation-note:nucleus-phase2-live-prestart-run-failure-coverage-2026-04-06]` The direct-run failure path before `run.started` now also has live CLI coverage. A running Nucleus service is exercised with a runtime that fails `execute_turn` immediately, `si nucleus run submit-turn` still returns the claimed `run_id`, and the durable projections then converge to `failed` for both the task and run while the canonical ledger records `run.failed` without a preceding `run.started`.
- `[implementation-note:nucleus-phase2-live-malformed-state-coverage-2026-04-06]` Malformed persisted task state now has live startup coverage through the public gateway. A broken `state/tasks/.../task.json` object is left on disk before Nucleus starts, startup isolates that object into a durable `system.warning` event instead of aborting, and the same running gateway still answers `si nucleus status`, creates a fresh session, and completes a new task successfully.
- `[implementation-note:nucleus-phase2-live-cancel-coverage-2026-04-06]` In-flight cancellation is now covered against a running Nucleus service. A live task bound to a real persisted session is allowed to enter `running`, `si nucleus run cancel` is issued against its live `run_id`, and both the durable task and durable run projections converge to `cancelled` through the same control plane.
- `[implementation-note:nucleus-phase2-live-worker-loss-coverage-2026-04-06]` Blocked worker-related state now has live service coverage. A runtime-backed task is started, the backing worker is removed from the test runtime mid-run, and the public projections converge consistently: the worker becomes `failed`, the run becomes `blocked` with `worker_unavailable`, and the task exposes the same blocked reason through CLI inspection.
- `[implementation-note:nucleus-worker-loss-unit-coverage-2026-04-06]` The same worker-loss reconciliation branch now also has direct `si-nucleus` coverage. A claimed in-flight run is left attached to a real persisted worker and session, the worker disappears from the runtime, and reconciliation now asserts the worker becomes `failed`, the run becomes `blocked` with `worker_unavailable`, the task exposes the same blocked reason, and the session is marked `broken`.
- `[implementation-note:nucleus-phase2-restart-reload-coverage-2026-04-06]` The restart contract now has an explicit persisted-state reload test in `si-nucleus`. A worker, session, task, and completed run are created against the real file-backed store, the service is reopened from disk, and the reloaded status counts, canonical event sequence, and durable projections all confirm that task/worker/session/run state survived restart without reset.
- `[implementation-note:nucleus-gateway-auth-live-coverage-2026-04-06]` The non-loopback gateway auth contract now also has a live CLI smoke. A running Nucleus service bound beyond loopback accepts read-only status requests without a token, rejects mutating CLI task creation without a bearer token, and then accepts the same mutation once `SI_NUCLEUS_AUTH_TOKEN` is provided to the CLI process.
- `[implementation-note:nucleus-doc-language-2026-04-06]` Public docs no longer describe SI itself as MCP-based. Browser-runtime pages now treat `si surf` as a browser automation runtime that may expose an MCP-compatible endpoint for external or legacy tooling, while the main SI architecture remains Nucleus-centered, WebSocket-based, and explicitly non-MCP.
- `[implementation-note:nucleus-plan-audit-2026-04-06]` The current `main` branch was re-audited end to end against this plan after the recent Nucleus implementation series. The owned `si` repository already satisfies the accepted Phase 1 through Phase 7 scope recorded here, the adjacent-repo boundary remains intact, and targeted verification currently passes through `cargo test -p si-nucleus` and `cargo test -p si-rs-cli`, so there is no remaining within-plan implementation gap to land before any future plan expansion.
- `[implementation-note:nucleus-repo-vault-workflow-2026-04-06]` A repo-local workflow regression discovered during the plan-conformance audit has been corrected in the shared `si` CLI: the local SI Vault command surface now again covers `check`, `hooks`, `encrypt`, `decrypt`, `restore`, `set`, `unset`, `get`, `list`, `run`, and `keypair`, matching the documented repo hook and dotenv-importer flows without changing the accepted Nucleus architecture scope above.
- `[implementation-note:nucleus-root-command-visibility-2026-04-06]` The public CLI manifest now exposes `nucleus` as a first-class root command again. This closes a drift where `si nucleus ...` was implemented and documented but was omitted from `si help`, `si commands`, and related root-command ordering checks because the shared command manifest did not list it.
- `[implementation-note:nucleus-gateway-metadata-coverage-2026-04-06]` Gateway discovery is now pinned more explicitly in regression coverage. `si-nucleus` tests now assert that service startup writes `~/.si/nucleus/gateway/metadata.json` with the documented `version`, `bind_addr`, and `ws_url` fields, so the metadata file remains a real contract rather than an incidental bootstrap detail.
- `[implementation-note:nucleus-live-status-metadata-coverage-2026-04-06]` The public `si nucleus status` path now also has direct live service coverage for the same gateway-discovery contract. Against a running Nucleus service, the CLI status command now returns the live `ws_url`, reflects durable task/worker/session/run counts after real work has completed, and matches the persisted `gateway/metadata.json` endpoint advertisement from the same service instance.
- `[implementation-note:nucleus-gateway-auth-read-coverage-2026-04-06]` The non-loopback auth contract is now pinned more explicitly for ordinary task reads as well as writes. `si-nucleus` regression coverage now asserts that `task.list` and `task.inspect`, plus the bounded REST `GET /tasks` and `GET /tasks/{task_id}` endpoints, remain readable without a bearer token when the service is bound beyond loopback, while the live CLI auth smoke now proves the same rule through `si nucleus task list|inspect` against a real authenticated service.
- `[implementation-note:nucleus-gateway-auth-read-surface-coverage-2026-04-06]` Read-only auth coverage now extends across the rest of the bounded inspect surface too. `si-nucleus` regression tests assert that `profile.list`, `worker.list|inspect`, `session.list|show`, `run.inspect`, `events.subscribe`, and the bounded REST `GET /workers`, `GET /workers/{worker_id}`, `GET /sessions/{session_id}`, and `GET /runs/{run_id}` endpoints stay readable without a bearer token when Nucleus is bound beyond loopback, and the live CLI auth smoke now also proves those same `si nucleus ...` read paths remain usable against a real authenticated service.
- `[implementation-note:nucleus-rest-auth-live-coverage-2026-04-06]` The bounded REST auth split now also has direct live service coverage. Against a running non-loopback Nucleus service with `SI_NUCLEUS_AUTH_TOKEN` set, unauthenticated `GET /openapi.json`, `GET /status`, `GET /tasks`, `GET /tasks/{task_id}`, `GET /workers`, `GET /workers/{worker_id}`, `GET /sessions/{session_id}`, and `GET /runs/{run_id}` remain readable, while unauthenticated `POST /tasks` and `POST /tasks/{task_id}/cancel` return the canonical `401` unauthorized envelope and those same writes succeed once a matching bearer token is supplied.
- `[implementation-note:nucleus-rest-auth-missing-target-coverage-2026-04-06]` The live REST auth smoke now also pins the missing-target ordering rule on authenticated non-loopback services. Unauthenticated reads of missing bounded objects still return the canonical `404 not_found` envelope because read paths remain open, while unauthenticated `POST /tasks/{task_id}/cancel` still returns `401 unauthorized` before target lookup and only switches to `404 not_found` once a matching bearer token is supplied.
- `[implementation-note:nucleus-rest-auth-validation-order-2026-04-06]` The bounded REST create path now also has direct live coverage for auth-before-validation ordering on authenticated non-loopback services. `POST /tasks` with an invalid uppercase profile still returns `401 unauthorized` when no bearer token is supplied, and only after a matching bearer is present does the same request resolve to `400 invalid_params` with the profile-slug grammar message.
- `[implementation-note:nucleus-rest-error-envelope-live-shape-2026-04-06]` The running service now also has direct live coverage for the full canonical `RestErrorEnvelope` shape across the bounded REST error branches. Live regression coverage asserts that `401 unauthorized`, `404 not_found`, and `503 unavailable` responses all return a non-empty `error.message` plus `error.details = null`, matching the documented envelope contract instead of only checking HTTP status codes or error codes in isolation.
- `[implementation-note:nucleus-openapi-live-document-coverage-2026-04-06]` The bounded OpenAPI document now also has direct live service coverage instead of being pinned only through unit tests. Against a running authenticated Nucleus service, `/openapi.json` is asserted to publish OpenAPI `3.1.0`, the canonical `bearerAuth` security scheme and `opaque token` bearer format, the documented write-endpoint auth split for `POST /tasks` and `POST /tasks/{task_id}/cancel`, the documented read-only unauthenticated status/openapi operations, and non-empty `summary`, `description`, and `x-si-purpose` annotations across every bounded REST operation.
- `[implementation-note:nucleus-openapi-live-cancel-purpose-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the exact bounded cancellation purpose string instead of only checking that the field is non-empty. Against a running service, `POST /tasks/{task_id}/cancel` is asserted to keep the canonical `x-si-purpose` text describing bounded external cancellation followed by re-reading task or run state, so the published document stays aligned with the plan’s intended cancellation semantics.
- `[implementation-note:nucleus-openapi-live-create-annotation-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the exact bounded task-create annotations instead of only checking that the fields are non-empty. Against a running service, `POST /tasks` is asserted to keep the canonical `summary`, `description`, and `x-si-purpose` text for bounded external task intake, so the published document stays aligned with the plan’s intended creation semantics and not just with its schemas.
- `[implementation-note:nucleus-openapi-live-status-annotation-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the exact bounded status annotations instead of only checking that the fields are non-empty. Against a running service, `GET /status` is asserted to keep the canonical `summary`, `description`, and `x-si-purpose` text for health and topology inspection, so the published document stays aligned with the plan’s intended status semantics and not just with its response schema.
- `[implementation-note:nucleus-openapi-live-task-list-annotation-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the exact bounded task-list annotations instead of only checking that the fields are non-empty. Against a running service, `GET /tasks` is asserted to keep the canonical `summary`, `description`, and `x-si-purpose` text for bounded task inspection and polling, so the published document stays aligned with the plan’s intended list semantics and not just with its array schema.
- `[implementation-note:nucleus-openapi-live-task-inspect-annotation-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the exact bounded task-inspect annotations instead of only checking that the fields are non-empty. Against a running service, `GET /tasks/{task_id}` is asserted to keep the canonical `summary`, `description`, and `x-si-purpose` text for bounded task inspection, so the published document stays aligned with the plan’s intended task-inspect semantics and not just with its response schema.
- `[implementation-note:nucleus-openapi-live-task-cancel-annotation-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the exact bounded task-cancel annotations instead of only partially checking that operation. Against a running service, `POST /tasks/{task_id}/cancel` is asserted to keep the canonical `summary`, `description`, and `x-si-purpose` text for bounded cancellation requests, so the published document stays aligned with the plan’s intended cancellation semantics and not just with its response schemas.
- `[implementation-note:nucleus-openapi-live-worker-list-annotation-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the exact bounded worker-list annotations instead of only checking that the fields are non-empty. Against a running service, `GET /workers` is asserted to keep the canonical `summary`, `description`, and `x-si-purpose` text for bounded worker inspection, so the published document stays aligned with the plan’s intended worker-list semantics and not just with its array schema.
- `[implementation-note:nucleus-openapi-live-worker-inspect-annotation-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the exact bounded worker-inspect annotations instead of only checking that the fields are non-empty. Against a running service, `GET /workers/{worker_id}` is asserted to keep the canonical `summary`, `description`, and `x-si-purpose` text for worker assignment and runtime attachment inspection, so the published document stays aligned with the plan’s intended inspect semantics and not just with its response schema.
- `[implementation-note:nucleus-openapi-live-session-inspect-annotation-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the exact bounded session-inspect annotations instead of only checking that the fields are non-empty. Against a running service, `GET /sessions/{session_id}` is asserted to keep the canonical `summary`, `description`, and `x-si-purpose` text for worker/session binding and reusable thread identity inspection, so the published document stays aligned with the plan’s intended session-inspect semantics and not just with its response schema.
- `[implementation-note:nucleus-openapi-live-run-inspect-annotation-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the exact bounded run-inspect annotations instead of only checking that the fields are non-empty. Against a running service, `GET /runs/{run_id}` is asserted to keep the canonical `summary`, `description`, and `x-si-purpose` text for bounded run inspection without websocket subscription, so the published document stays aligned with the plan’s intended run-inspect semantics and not just with its response schema.
- `[implementation-note:nucleus-openapi-live-document-annotation-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the exact annotations for the OpenAPI document operation itself instead of only treating it as an object-schema and auth check. Against a running service, `GET /openapi.json` is asserted to keep the canonical `summary`, `description`, and `x-si-purpose` text for bounded external client bootstrap, so the published document stays aligned with the plan’s intended discovery semantics and not just with its response shape.
- `[implementation-note:nucleus-openapi-live-parameter-response-description-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the published parameter and success/error description strings instead of only checking schemas and endpoint-level annotations. Against a running service, the OpenAPI document is asserted to keep the canonical path-parameter descriptions for task, worker, session, and run ids, plus the documented response descriptions for status, task create, task inspect/cancel not-found, worker inspect, session inspect, run inspect, and the document operation itself, so the live document stays aligned with the intended bounded API wording and not just with its shapes.
- `[implementation-note:nucleus-openapi-live-list-success-description-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the success-description strings on the bounded list operations. Against a running service, `GET /tasks` is asserted to keep `All durable tasks.` and `GET /workers` is asserted to keep `All durable workers.`, so the published live contract stays aligned with the intended list-response wording and not just with array schemas.
- `[implementation-note:nucleus-openapi-live-task-success-description-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the remaining task success-description strings directly. Against a running service, `GET /tasks/{task_id}` is asserted to keep `Task record.` and `POST /tasks/{task_id}/cancel` is asserted to keep `Cancellation result.`, so the published live contract stays aligned with the intended task-response wording and not just with schemas and endpoint annotations.
- `[implementation-note:nucleus-openapi-live-not-found-description-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the remaining inspect-path `404` description strings directly. Against a running service, the OpenAPI document is asserted to keep the canonical `Worker not found.`, `Session not found.`, and `Run not found.` descriptions on the corresponding inspect operations, so the live contract stays aligned with the intended bounded error wording and not just with the shared `RestErrorEnvelope` schema references.
- `[implementation-note:nucleus-openapi-live-error-description-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the bounded write-auth, unavailable, and generic failure description strings directly. Against a running service, the OpenAPI document is asserted to keep the canonical unauthorized wording on `POST /tasks` and `POST /tasks/{task_id}/cancel`, the canonical unavailable wording on active-run cancellation, and the shared `Request failed.` wording across the bounded task, worker, session, run, status, and document `500` responses, so the live contract stays aligned with the intended API wording and not just with status codes and schemas.
- `[implementation-note:nucleus-rest-openapi-v056-verification-2026-04-06]` After the final live annotation pass and the `v0.56.0` bump, the bounded REST/OpenAPI verification sweep is green again. `cargo test -p si-rs-cli nucleus_rest_ -- --nocapture` and `cargo test -p si-nucleus rest_openapi_document_describes_bounded_external_endpoints -- --nocapture` now pass together, confirming the live CLI/websocket/REST parity checks and the unit-level OpenAPI document contract still match after the latest exact-annotation hardening.
- `[implementation-note:nucleus-openapi-live-error-response-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the published error-response references instead of leaving them to unit coverage alone. Against a running service, the OpenAPI document is now asserted to advertise canonical `RestErrorEnvelope` schemas for `POST /tasks` `400|401`, `GET /tasks/{task_id}` `404`, `POST /tasks/{task_id}/cancel` `401|404|503`, and the generic `500` responses on `GET /status` and `GET /openapi.json`.
- `[implementation-note:nucleus-openapi-live-success-schema-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the published success-schema references across the rest of the bounded read surface. Against a running service, the OpenAPI document is asserted to keep `TaskRecord[]`, `WorkerRecord[]`, `WorkerInspectView`, `SessionRecord`, `RunRecord`, `NucleusStatusView`, and the generic object schema for `/openapi.json`, so the live document stays aligned with the canonical Nucleus models rather than only with auth and error annotations.
- `[implementation-note:nucleus-openapi-live-cancel-success-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the bounded cancellation success contract directly. Against a running service, `POST /tasks/{task_id}/cancel` is asserted to keep the canonical `TaskCancelResultView` success schema reference, so the published document stays aligned with the real cancellation result model instead of only with its error branches.
- `[implementation-note:nucleus-openapi-live-component-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins key bounded component-schema details directly. Against a running service, the OpenAPI document is asserted to keep `TaskCancelResultView` requiring `task` and `cancellation_requested`, and `WorkerInspectView.worker` referencing the canonical `WorkerRecord`, so the published component graph stays aligned with the real bounded models instead of only with top-level operation refs.
- `[implementation-note:nucleus-openapi-live-error-component-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the canonical error-envelope component shape directly. Against a running service, the OpenAPI document is asserted to keep `RestErrorEnvelope` requiring `error`, `error` requiring `code` and `message`, and the documented `details` field shape, so the published error model stays aligned with the real bounded REST envelope instead of only with operation-level schema references.
- `[implementation-note:nucleus-openapi-live-bounded-error-surface-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the rest of the bounded error surface directly. Against a running service, the OpenAPI document is asserted to keep canonical `404` `RestErrorEnvelope` references on the worker, session, and run inspect endpoints, plus canonical `500` `RestErrorEnvelope` references across the task, worker, session, run, status, and document operations, so the published live contract stays aligned with the full bounded failure surface already pinned in unit coverage.
- `[implementation-note:nucleus-openapi-live-request-body-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the bounded write request-body contract directly. Against a running service, `POST /tasks` is asserted to keep a required JSON request body with the canonical `TaskCreateParams` schema reference, so the published document stays aligned with the real bounded intake surface rather than only with its responses.
- `[implementation-note:nucleus-openapi-live-parameter-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the bounded path-parameter shapes directly. Against a running service, the OpenAPI document is asserted to keep string path parameters for `task_id`, `worker_id`, `session_id`, and `run_id` on the inspect and cancel operations, matching the unit-level OpenAPI contract and the live bounded REST surface.
- `[implementation-note:nucleus-openapi-live-read-auth-coverage-2026-04-06]` The live `/openapi.json` smoke now also pins the published auth split across the rest of the bounded read surface instead of only on `/status` and `/openapi.json`. Against a running service, the document is asserted to keep `security = null` on `GET /tasks`, `GET /tasks/{task_id}`, `GET /workers`, `GET /workers/{worker_id}`, `GET /sessions/{session_id}`, and `GET /runs/{run_id}`, matching the live non-loopback read contract.
- `[implementation-note:nucleus-openapi-auth-coverage-2026-04-06]` The generated OpenAPI document now has explicit regression coverage for the same auth split enforced by the service. Tests now pin bearer auth on the bounded write endpoints (`POST /tasks`, `POST /tasks/{task_id}/cancel`) and assert that the bounded read endpoints (`GET /status`, `GET /tasks`, `GET /tasks/{task_id}`, `GET /workers`, `GET /workers/{worker_id}`, `GET /sessions/{session_id}`, and `GET /runs/{run_id}`) do not advertise bearer auth requirements.
- `[implementation-note:nucleus-openapi-auth-responses-2026-04-06]` The OpenAPI write endpoints now also describe their auth failure shape explicitly. `POST /tasks` and `POST /tasks/{task_id}/cancel` now document `401` responses using the canonical `RestErrorEnvelope`, so external consumers see the same unauthorized contract the runtime already enforces.
- `[implementation-note:nucleus-openapi-error-envelope-coverage-2026-04-06]` The bounded REST/OpenAPI contract now documents the canonical server-failure envelope more consistently. The generated document now advertises `500` `RestErrorEnvelope` responses across the bounded task, worker, session, run, and OpenAPI endpoints, and regression coverage pins those schemas so external consumers see the same generic failure shape the service already emits.
- `[implementation-note:nucleus-openapi-success-schema-coverage-2026-04-06]` OpenAPI regression coverage now also pins the primary success-schema references for the bounded endpoints, including status, task list/create/inspect/cancel, worker list/inspect, session inspect, and run inspect. This keeps the published response-model contract aligned with the canonical Nucleus types instead of only checking auth and error annotations.
- `[implementation-note:nucleus-openapi-parameter-error-coverage-2026-04-06]` OpenAPI regression coverage now also pins the bounded path-parameter shapes and the inspect/cancel error envelopes. The generated document is now asserted to keep string path parameters for task, worker, session, and run ids, plus canonical `RestErrorEnvelope` schemas on the documented `404` and `503` error responses for the bounded inspect and cancel operations.
- `[implementation-note:nucleus-openapi-component-read-coverage-2026-04-06]` OpenAPI regression coverage now also pins the low-level component contract and the non-loopback read behavior for the document itself. Tests now assert the published `bearerFormat`, the canonical `RestErrorEnvelope` shape, the `/status` and `/openapi.json` response schemas, and that `GET /openapi.json` remains readable without a bearer token when Nucleus is bound beyond loopback.
- `[implementation-note:nucleus-openapi-operation-annotation-coverage-2026-04-06]` Phase 7 regression coverage now also pins the per-endpoint documentation contract directly. Tests assert that every bounded REST operation publishes a non-empty `summary`, `description`, and `x-si-purpose`, plus a success response schema and an explicit request surface through either documented parameters or a request body. Body-backed write operations also keep required request-body schemas. This closes the remaining gap between the plan’s “each endpoint” annotation requirement and the regression suite.
- `[implementation-note:nucleus-live-test-startup-retry-2026-04-06]` The live CLI verification harness now retries Nucleus test-service startup across fresh ephemeral ports when a just-reserved localhost port is lost before the service binds. This removes a real flake in the plan-verification path where the public `nucleus_` CLI suite could fail with `Address already in use` even though the implementation itself was healthy.
- `[implementation-note:nucleus-live-ledger-tail-retry-2026-04-06]` Steady-state hook-producer iteration now retries a transient parse on the active `events.jsonl` tail before surfacing an error. This removes a real execution flake where a hook scan could race with an in-flight event append, observe an incomplete trailing JSON line, and fail despite the canonical ledger becoming valid a few milliseconds later. Startup still fails clearly on genuinely unreadable canonical ledger state; only the live append race is retried.
- `[implementation-note:nucleus-final-verification-matrix-2026-04-06]` The current accepted plan slice now also has a clean end-state verification sweep across the owned packages and public CLI harnesses: `cargo test -p si-nucleus-core`, `cargo test -p si-nucleus-runtime`, `cargo test -p si-nucleus-runtime-codex`, `cargo test -p si-nucleus`, `cargo test -p si-rs-cli nucleus_`, and `cargo test -p si-rs-cli vault_` all pass together after the latest contract hardening and live-service startup retry fix.
- `[implementation-note:nucleus-auxiliary-verification-2026-04-06]` The adjacent plan sections around service management and release/install channels also have passing repo-local verification. `cargo test -p si-rs-cli nucleus_service_` passes for the documented `si nucleus service ...` contract, and `cargo test -p si-rs-cli build_installer_smoke_` passes for the repo-local host, npm, and Homebrew installer smoke flows described in the release/install part of this plan.
- `[implementation-note:nucleus-release-version-surface-coverage-2026-04-06]` The release/install regressions now also pin the single-version policy at the packaging surface itself. The npm package build test asserts that the staged `package.json` is rewritten to the requested SI version before `npm pack`, and the Homebrew tap-formula test asserts the rendered formula publishes the matching `version` stanza for the same SI release.
- `[implementation-note:nucleus-live-scenario-matrix-2026-04-06]` The live end-to-end scenario path now also has a clean serial verification sweep. `cargo test -p si-rs-cli nucleus_ -- --nocapture --test-threads=1` and `cargo test -p si-nucleus -- --nocapture` pass together while exercising the documented cross-surface task parity, producer emission, Fort ready/auth-required/unavailable behavior, session backlog ordering, event-stream reconnect, streamed run completion, session resume, session-init failure, malformed-state startup recovery, cancellation, blocked worker projection, gateway auth, service management, and bounded REST/OpenAPI source-of-truth contracts.

## Implementation checkpoints

Use these checkpoints to keep the build-out end-to-end and verifiable:

1. core types and state transitions compile and validate
2. canonical event ledger appends, sequences, and reloads correctly
3. worker startup and App Server initialization work through the runtime boundary
4. task routing, worker selection, session reuse, and session backlog rules work together
5. cron and hook producers emit durable tasks safely across restart and replay
6. CLI commands operate through the gateway and reflect the same source of truth
7. Fort-backed worker operations normalize correctly into canonical events and blocked reasons
8. annotated REST endpoints expose the same source of truth with OpenAPI-compatible schemas and endpoint-purpose annotations

A phase should not be marked `accepted` until its checkpoint and acceptance criteria both pass.

## Success criteria

The plan should only be considered successful if all of the following are true:
- a task can be created, persisted, routed, executed, and observed without tmux
- queued tasks are selected from the durable task ledger without needing a separate queue primitive
- cron and hook producers can create durable tasks reliably across restart and replay
- one lowercase `profile` maps cleanly to one worker and one isolated `CODEX_HOME`
- a worker can be started, initialized, and inspected through App Server only
- a session can be created or resumed using App Server thread identity
- a run can be started, interrupted, and completed using App Server turn semantics
- Nucleus can normalize App Server notifications into durable SI events
- WebSocket and CLI surfaces expose the same source of truth for task, worker, session, run, event, and profile state
- Fort-backed worker access behaves through the documented integration contract and projects into the same source of truth
- the later annotated REST API exposes that same source of truth without requiring MCP integration
- restart of Nucleus preserves durable state and allows recovery of tasks, sessions, and workers from disk
- no Docker, Kubernetes, MCP, or oneshot `codex exec` path remains in the runtime architecture

## Phase acceptance criteria

### Phase 1 acceptance: `si-nucleus-core`
- core types compile cleanly
- task, worker, session, run, event, and profile state transitions are explicitly validated
- App Server mapping types exist for thread, turn, item, account, and config projections

### Phase 2 acceptance: `si-nucleus`
- WebSocket gateway accepts requests and emits events
- task creation through the gateway persists immediately
- queued task selection operates from durable task state without a separate queue object
- cron and hook producers persist their state and resume safely after restart
- file-backed state persists and reloads correctly
- worker supervision and reconciliation loops run without direct CLI coupling

### Phase 3 acceptance: `si-nucleus-runtime`
- runtime trait can start, stop, inspect, and interrupt workers and runs through one stable interface
- Nucleus can use the runtime interface without importing Codex-specific code directly

### Phase 4 acceptance: `si-nucleus-runtime-codex`
- Codex App Server worker can be started locally with worker-specific `CODEX_HOME`
- runtime can perform `initialize`, `account/read`, and `config/read`
- runtime can create or resume a session and start a run
- runtime can normalize App Server notifications into SI events
- runtime can interrupt a run cleanly

### Phase 5 acceptance: CLI integration
- `si nucleus task ...`, `si nucleus session ...`, and `si nucleus run ...` operate through Nucleus rather than hidden direct runtime shortcuts
- CLI can inspect task, worker, session, run, and profile state from the same source of truth used by the gateway

### Phase 6 acceptance: Fort integration
- worker-side Fort access uses the documented integration path rather than hidden local state assumptions
- Fort availability and failure states are normalized into canonical SI events
- Fort-related failures project to stable blocked reasons when appropriate

### Phase 7 acceptance: annotated REST API
- REST endpoints are generated or implemented from the same canonical Nucleus model used by the gateway
- the API is OpenAPI-compatible
- each endpoint has summary, description, request schema, response schema, and explicit endpoint-purpose annotations
- GPT Actions can use the bounded external operations without requiring MCP

## Test fixture policy

Keep fixtures small, deterministic, and contract-focused.

Rules:
- use fake App Server inputs where possible to test normalization and state transitions without depending on live Codex processes
- use canonical event fixtures to test replay, projection, producer behavior, and blocked-reason handling
- use isolated file-backed state fixtures for workers, sessions, runs, tasks, and producer state
- use Fort test doubles or isolated Fort test setups for integration tests rather than hidden local-machine assumptions
- fixture data should exercise the documented contract shapes, not private implementation shortcuts

## Verification plan

### End-to-end scenarios
- create a task from the CLI and observe it through both CLI and the WebSocket gateway
- once phase 7 exists, create and inspect a task through the annotated REST API and verify it matches WebSocket and CLI state
- create a task through the WebSocket gateway and observe it through the CLI
- create a task from a cron producer firing and observe it through CLI and WebSocket
- create a task from a hook producer match and observe it through CLI and WebSocket
- verify a task that requires Fort-backed access can execute through the documented Fort integration path and project normalized events back into Nucleus
- queue multiple tasks and verify selection comes from durable queued task state
- queue multiple tasks for the same `session_id` and verify stable backlog ordering
- route a task onto the expected lowercase `profile`
- start or reuse a worker for that `profile`
- create or resume a session on that worker
- submit a run and receive streamed output events
- complete the run and observe task state change to `done`
- cancel an in-flight run and observe task and run state update consistently
- restart Nucleus and verify persisted task, worker, session, run, and event state reloads correctly
- surface a blocked worker-related state and verify it is visible through task, worker, and run projections

### Failure scenarios
- App Server fails to initialize
- worker process exits unexpectedly
- auth is missing or expired
- run is interrupted mid-stream
- persisted state file is malformed or partially missing
- producer restart occurs between task emission and producer state advancement
- WebSocket client disconnects and reconnects during an active run

### Regression guardrails
- no code path should depend on tmux to determine canonical state
- no code path should require Docker or image lifecycle management
- no code path should fall back to `codex exec`
- no public control-plane surface should expose raw App Server internals as the contract

## Delivery model

There is no migration or compatibility rollout plan.

The intended delivery model is:
- remove old paths fully
- replace them with the Nucleus architecture directly
- do not keep temporary bridges, fallback transports, or compatibility layers inside SI

## Security and auth model

Keep this simple.

Rules:
- Nucleus binds to loopback by default
- WebSocket is local-only by default
- authentication is required before non-read actions when the gateway is exposed beyond loopback
- if Nucleus is exposed beyond loopback, use one simple bearer token configured by environment variable or local config file
- profile-scoped Codex auth remains inside each worker's `CODEX_HOME`
- Nucleus must not expose raw `auth.json` mutation through public APIs

## Versioning and schema policy

Keep one versioning policy only.

Rules:
- the SI repository version is the version for Nucleus, the WebSocket gateway, and persisted state
- do not introduce separate product versions for the gateway, storage schema, or any other Nucleus-owned surface
- normal changes should bump the SI patch version
- bump the SI minor version only when the change is meaningfully larger and deserves that level of version step
- when the SI version changes, that one version change applies everywhere in the architecture at once
- persisted JSON objects should carry a simple `version` field equal to the SI version when needed for startup checks and controlled rewrites

## Failure and recovery contract

Keep failure handling explicit, restart-safe, and testable.

### Nucleus restart contract

On Nucleus startup, the recovery order is:
1. load persisted profiles
2. load persisted workers
3. load persisted sessions
4. load persisted runs
5. load persisted tasks
6. load persisted producer state
7. rebuild projections from canonical state and `events.jsonl`
8. probe worker processes and runtime readiness
9. reconcile in-flight or uncertain run state
10. resume producer loops
11. open the WebSocket gateway

Rules:
- Nucleus restart must not silently discard persisted task, session, run, worker, or producer state
- projections may be rebuilt, but canonical durable objects and the canonical event ledger remain authoritative
- if persisted state is readable, Nucleus should prefer recovery over reset

### Worker failure contract

Rules:
- if a worker exits while idle, mark the worker failed and let Nucleus decide whether to restart it
- if a worker exits during an active run, Nucleus must emit failure-related events and reconcile the affected run and task explicitly
- worker failure must never silently drop a task
- worker restart policy belongs to Nucleus, not to separate worker service units

Recommended default behavior:
- restart failed workers with bounded retry and backoff
- if repeated restart fails, keep the worker failed and project affected tasks to `blocked` or `failed` explicitly

### Run recovery contract

After Nucleus restart or worker failure, an in-flight run must end in exactly one recoverable interpretation:
- `running` again because Nucleus successfully reattached and verified it
- `failed`
- `cancelled`
- `blocked` pending operator repair or later retry

Rules:
- do not leave runs permanently ambiguous
- if Nucleus cannot prove a run is still healthy and attached, it must reconcile it to a non-ambiguous state
- task projection must follow the reconciled run state

### Session recovery contract

A session may be reused after restart only if:
- the worker is healthy
- App Server thread or session resume succeeds
- there is no conflicting active run
- the session is not explicitly marked broken

If those checks fail:
- mark the session non-reusable or broken
- do not silently attach new tasks to it
- project affected tasks through normal routing or blocked-state handling

### State corruption contract

Rules:
- if one persisted object file is malformed or unreadable, isolate that object and emit a durable system event
- Nucleus should continue running if the canonical event ledger and enough surrounding state remain usable
- do not reset the whole state directory because one object is bad
- if `events.jsonl` itself is unreadable in a way that breaks canonical recovery, fail clearly and require explicit operator repair

### Producer recovery contract

Rules:
- if a crash happens before durable task creation, producer replay may emit the task later
- if a crash happens after durable task creation but before producer state advancement, replay is allowed and dedup must suppress duplicate effective task creation
- producer recovery must prefer at-least-once emission with deduplication over silent task loss

### Operator repair boundary

Rules:
- Nucleus may restart workers, rebuild projections, re-read runtime state, and resume producers automatically
- Nucleus must not mutate Codex auth internals directly as part of hidden repair behavior
- explicit operator action is required when auth, corrupted canonical state, or repeated restart failure cannot be repaired safely by Nucleus

## Repair and recovery model

Keep repair explicit and operator-visible.

Repair cases to support:
- worker failed to start
- App Server failed `initialize`
- auth missing or expired
- run hung or interrupted
- persisted state malformed or incomplete

Required repair actions:
- restart worker
- re-read account and config state
- mark tasks as `blocked` or `failed` explicitly
- rebuild projections from canonical JSON and JSONL state

Cancellation semantics:
- cancelling an active run should cancel that `run` and mark the `task` as `cancelled`
- cancelled tasks are not re-queued automatically
- if work should resume, Nucleus or the operator should create a new task

Startup reconciliation order:
1. load profiles
2. load workers
3. load sessions
4. load runs
5. load tasks
6. load producer state
7. rebuild projections
8. start worker supervision
9. start producer loops
10. open the WebSocket gateway

## Concurrency and locking rules

Keep concurrency simple and file-safe.

Rules:
- one writer at a time per object file
- JSON object writes must be atomic via temp-file-and-rename
- JSONL appends must be serialized per stream
- conflicting writes should fail fast rather than merge implicitly
- task, session, and run mutation order must follow the event sequence observed by Nucleus
- tasks queued behind the same `session_id` must be dispatched in a stable backlog order
- producer state advancement must follow durable task creation, not precede it

## Retention and cleanup policy

Keep cleanup conservative.

Rules:
- active workers, sessions, runs, and recent tasks stay on disk
- completed and failed tasks remain inspectable until explicitly pruned
- use a conservative default retention window such as 30 days for completed and failed tasks unless explicitly configured otherwise
- event streams may be rotated, but only after summaries and current object state remain reconstructable
- cleanup must never delete active worker state or the latest session/run state silently

## Service management model

Nucleus should run as a local user service, not as a containerized service.

Rules:
- on Linux, prefer `systemd --user`
- on macOS, prefer `launchd`
- do not run Nucleus in Docker
- do not make worker processes separate OS services by default
- Nucleus should spawn and supervise worker processes itself

Why this is the right model:
- it matches the local-process-first architecture
- it keeps `CODEX_HOME`-isolated worker management inside Nucleus
- it avoids reintroducing container/runtime complexity that the plan has already removed
- it gives normal local restart and startup behavior without inventing another orchestration layer

## Release and installation channels

Keep release and installation simple and aligned with the single-version policy.

Initial supported installation channels:
- direct binary release
- `brew`
- `npm`

Rules:
- all installation channels ship the same SI version
- there is no separate package or version for Nucleus
- upgrade of `si` implies upgrade of the Nucleus service and its generated service definitions when needed
- installation channels should install the main `si` binary and any required companion files without creating a second product identity
- release artifacts should stay predictable and easy to verify

Why this belongs in the plan:
- installation channels affect how operators get and upgrade SI in practice
- service installation and update behavior must stay aligned with release behavior
- the single-version policy should apply across direct installs, `brew`, and `npm` equally

## Service installation plan

Keep service installation minimal and OS-native.

Rules:
- SI should provide generated user-service definitions rather than asking operators to write them manually
- on Linux, SI should be able to install and update a `systemd --user` unit for Nucleus
- on macOS, SI should be able to install and update a `launchd` agent for Nucleus
- service installation should point to the current `si` binary and the configured Nucleus state directory
- service installation should not create a second configuration system outside normal SI config files and environment variables

Minimum install actions to support:
- install service definition
- uninstall service definition
- start service
- stop service
- restart service
- print service status and logs hints

Why this should be planned:
- naming the service manager is not enough; operators need a repeatable install path
- service definitions must stay aligned with SI versioning and config conventions
- startup, restart, and failure behavior should be part of the architecture, not left as ad hoc shell work

## Discovery and bootstrap

Keep discovery explicit and local.

Rules:
- the CLI should know one default local Nucleus endpoint
- environment variables may override it
- a well-known gateway metadata file may advertise the bound WebSocket endpoint and current SI version
- discovery should not require external service discovery systems

## Observability

Keep observability focused on operator usefulness.

Provide:
- durable events for runtime history
- readable worker and task summaries on disk
- gateway-visible task, worker, session, run, and profile state
- producer-visible logs and state for cron and hook emission
- enough logs to diagnose startup, auth, run failure, producer behavior, and restart behavior

## What must not come back

Do not reintroduce:
- Docker support
- Kubernetes support
- MCP support
- image build/runtime planning for Codex workers
- oneshot `codex exec` mode
- raw tmux parsing as the main control path
- worker identity based on terminal/session names alone
- Unix-socket IPC as the primary Nucleus transport

## Operator guidance

The operator story should be:
- spawn or reuse a worker by profile
- inspect worker runtime state
- attach through tmux when enabled and needed
- observe session/run state through SI commands
- repair broken auth or App Server state explicitly

The machine story should be:
- talk to `si-nucleus`
- let `si-nucleus` talk to App Server
- persist events and lifecycle state centrally

## Main lessons from the research set

OpenClaw:
- copy the control-plane shape
- do not copy the sandbox/runtime stack

CLI Agent Orchestrator:
- explicit control-plane surfaces are valuable
- terminal parsing is not a durable runtime architecture

Oh My Codex:
- do not collapse runtime and task state into one undifferentiated layer

codex-acp:
- ACP is out of scope for the SI architecture

codex-app-server-client-sdk:
- App Server deserves a clean client layer with typed event handling

tmux-agent-status and opensessions:
- operator visibility matters
- tmux UX should reflect orchestrator state, not replace it

## Final one-sentence plan

SI should build a Nucleus-centered local control plane around durable Codex App Server workers with isolated `CODEX_HOME` roots, one SI-native `task` primitive above the App Server model, optional tmux-based operator tooling, and a WebSocket main control plane, with no Docker, no Kubernetes, no MCP, and no oneshot Codex execution path.
