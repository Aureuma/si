# Nucleus Transition Hardening Plan

## Status

- Completed the shared task/session binding evaluator in `si-nucleus`; no generic state-machine framework was introduced.
- Task intake, queued dispatch, and direct `run.submit_turn` now use the same deterministic session-binding rules.
- Added coverage for missing sessions, profile mismatches, non-reusable sessions, missing app-server thread ids, and direct-run session-id mismatches.
- Live REST verification confirmed `400 invalid_params`, immediate blocked task projection for impossible session bindings, stable inspect responses, and the remaining cancel-task contract wording gap.
- Closed the cancel-task wording gap by tightening the OpenAPI description for `cancellation_requested`.

## Goal

Make Nucleus lifecycle behavior systematic by moving deterministic cross-entity binding checks behind one shared transition evaluator, then use that evaluator consistently across task intake, dispatch, and direct run submission.

## Why this is needed

Nucleus already has per-entity transition guards in `si-nucleus-core`:
- `TaskStatus::can_transition_to`
- `RunStatus::can_transition_to`
- `SessionLifecycleState::can_transition_to`
- `WorkerStatus::can_transition_to`

So the missing piece is not a generic state-machine library.

The real problem is that cross-entity lifecycle rules are still distributed through ad hoc service logic in `si-nucleus`:
- task intake decides some things up front
- dispatch decides some things later
- direct `run.submit_turn` decides some things independently
- runtime reconciliation decides others after the fact

That makes it easy for one path to return a transient lie like `queued` while another path would immediately know the task is already blocked.

## Decision

Do **not** introduce a generic state-machine framework.

Instead, implement a small domain-specific transition layer inside Nucleus that:
- classifies the binding state between `task`, `session`, `worker`, and `profile`
- returns an explicit typed outcome
- can be reused by task intake, dispatch, and direct run submission
- carries the exact blocked reason and message that should be projected
- carries whether the referenced session should itself be marked broken

This is the right level of abstraction because it reduces inconsistency without replacing the existing durable model or event model.

## Problems to fix systematically

### 1. Deterministic invalid task bindings are not handled from one place

Examples:
- missing referenced session
- session profile mismatch
- non-reusable session (`broken` or `closed`)
- session missing app-server thread id

These should all be derived from one shared evaluator instead of re-implemented in multiple request paths.

### 2. Intake, dispatch, and direct run submission can disagree about the same state

Examples:
- `task.create` may create a task first and let dispatch discover that it is impossible
- `run.submit_turn` may reject a task/session combination that intake accepted as if it were runnable
- dispatch may mark a task blocked using a different branch than direct-run submission would have used

The same binding state should produce the same classification everywhere.

### 3. Public API contract enforcement is incomplete unless it is tied to the same transition layer

The OpenAPI contract already constrains request bodies. The service must enforce those inputs before durable state is created, and it must also apply deterministic lifecycle blocking before returning the created task.

## Implementation plan

### Phase 1. Introduce a typed binding evaluator

Add a domain-specific evaluator in `si-nucleus` for session-bound task/run execution.

Suggested shape:
- `TaskSessionBindingState` or similar enum
- variants for:
  - `MissingSession`
  - `ProfileMismatch`
  - `SessionNotReusable`
  - `MissingThreadId`
  - `Ready`

Associated output should carry:
- `worker_id`
- `session_id`
- resolved `profile` where relevant
- `blocked_reason`
- operator-facing message
- whether to call `mark_session_broken`

This is the systematic core. Everything else should call into this.

### Phase 2. Use the evaluator at task intake

At `task.create`:
- validate request fields before durable writes
- create the task
- if the binding evaluator says the task is already impossible, immediately project the blocked task before returning the response
- if the evaluator says the referenced session is structurally broken, also mark the session broken before returning

This keeps create responses honest.

### Phase 3. Use the same evaluator in dispatch

Replace duplicated session-binding checks in dispatch/ensure-session paths with the same evaluator.

That keeps late-path behavior aligned with intake.

### Phase 4. Use the same evaluator in direct run submission

If `run.submit_turn` is asked to operate on a task/session pair, it should use the same binding evaluator before trying to open or resume a turn.

That ensures direct runs and queued tasks share the same lifecycle interpretation.

### Phase 5. Test at the right layers

Add or update tests for:
- gateway `task.create`
- REST `POST /tasks`
- dispatcher behavior
- direct `run.submit_turn`

Required scenarios:
- missing session
- session/profile mismatch
- broken or closed session
- session missing app-server thread id
- blank task fields

### Phase 6. Live REST verification

Run a local Nucleus instance and verify via REST that:
- invalid input returns `400 invalid_params`
- deterministic invalid session-bound creates return blocked tasks immediately
- inspect endpoints preserve the projected blocked state
- no live path still returns a transient `queued` result for a deterministically invalid binding

## Risks and controls

### Risk: over-generalizing into a framework

Control:
- keep the new abstraction domain-specific
- avoid macros, generated tables, or a new engine dependency
- reuse existing durable record types and canonical events

### Risk: changing semantics for dynamic worker/profile availability

Control:
- only preflight **deterministic** invalid states
- keep dynamic availability decisions in dispatch/runtime paths
- do not pre-block requests that depend on worker start timing or temporary capacity

### Risk: diverging blocked-reason vocabulary

Control:
- reuse existing `BlockedReason` values unless a new one is clearly necessary
- if existing vocabulary is insufficient, add it deliberately with matching OpenAPI and tests

## Acceptance criteria

- One shared evaluator owns deterministic session-binding classification.
- `task.create`, dispatch, and direct run submission use the same classification rules.
- No deterministic invalid session-bound task returns as `queued` from create.
- Live REST tests confirm the new behavior.
- No unrelated repo changes are pulled into the commit.
