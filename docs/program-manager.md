# Program Manager

The Program Manager is a high-level reconciler that creates and maintains a set of dyad tasks for a specific goal (“program”).

In Silexa, this role is implemented as a dedicated **PM dyad**:
- `actor-pm` (actor container; optional Codex use for planning)
- `critic-pm` (critic container; runs the reconciliation loop and creates tasks via Manager API)

Core properties:
- **Idempotent**: tasks are created once and then left to dyads/humans; re-running the reconciler won’t spam duplicates.
- **Task-board native**: uses Manager `/dyad-tasks` API; tasks are routed/claimed by dyads via the existing router + critic loops.
- **Config-driven**: programs live under `configs/programs/*.json`.
- **Backpressure-aware**: applies per-dyad and global watermarks + per-tick rate limits so it feeds the board without choking Telegram or starving dyads.

## How it works

On each reconcile tick:
1) Load a program config (JSON).
2) List current dyad tasks from the Manager.
3) Detect whether each program task already exists by looking for state lines:
   - `[pm.program]=<program>`
   - `[pm.key]=<task_key>`
4) Create any missing dyad tasks with:
   - `requested_by=program-manager`
   - `notes` containing the program state lines for dedupe.
5) Apply limits before creating:
   - cap new tasks created per tick
   - cap open tasks per dyad
   - cap open tasks globally
   - prioritize dyads with too few open tasks (avoid starvation)

## Web hosting program

Default program config:
- `configs/programs/web_hosting.json`

This program is the roadmap for “host web app repos and serve them”, including:
- inventory + hosting contract
- compose scaffold
- reverse proxy + TLS plan
- healthchecks/monitoring
- deployment flow + secrets strategy
- one end-to-end hosted app

## Service (Swarm)

Service names:
- `actor-pm` (Swarm service: `silexa_actor-pm`)
- `critic-pm` (Swarm service: `silexa_critic-pm`)

Environment variables:
- `PROGRAM_MANAGER=1` enables program reconciliation mode.
- `PROGRAM_CONFIG_DIR` (default `/configs/programs`) or `PROGRAM_CONFIG_FILE` for single-file mode.
- `MANAGER_URL` (default `http://manager:9090`)
- Backpressure knobs (safe defaults):
  - `PM_MAX_NEW_PER_TICK` (default `3`)
  - `PM_MAX_OPEN_PER_DYAD` (default `5`)
  - `PM_MIN_OPEN_PER_DYAD` (default `1`)
  - `PM_MAX_OPEN_GLOBAL` (default `30`)
- Hygiene:
  - `PM_GC_ORPHAN_DUPES` (default `true`) closes old `requested_by=program-manager` tasks that lost their `[pm.key]` notes and are clearly duplicates of an existing keyed task.
- Codex state isolation: `CODEX_PER_DYAD` defaults to `1` (per-dyad codex state) so GitHub/Codex tokens aren’t shared across containers. Set to `0` only if you explicitly want shared state.

## Notes

- Program tasks can set `dyad` explicitly, or leave it empty and let the router assign based on `kind/title/description` matching.
- To route through usage-aware pools, set `route_hint` to `pool:<department>` (e.g., `pool:infra`). The router will pick a healthy dyad from that pool.
- To avoid noisy Telegram, keep most tasks `priority=normal`; reserve `high` for items where humans should pay attention.
