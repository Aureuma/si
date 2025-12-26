# Dyad Task Board (Router -> Dyads)

Purpose: structured task intake and allocation to dyads, with notifications and status tracking in Manager.

## Data model (Manager)
- Endpoints:
  - `GET/POST /dyad-tasks`
  - `POST /dyad-tasks/update`
  - `POST /dyad-tasks/claim` (atomic claim + heartbeat; prevents multi-critic contention)
- Fields: `id`, `title`, `description`, `kind`, `status (todo|in_progress|review|blocked|done)`, `priority`, `dyad`, `actor`, `critic`, `requested_by`, `notes`, `link`, `claimed_by`, `claimed_at`, `heartbeat_at`, `created_at`, `updated_at`.
- Notifications: Manager posts a formatted message to Telegram on create/update (uses `TELEGRAM_NOTIFY_URL` and optional `TELEGRAM_CHAT_ID`).

## Scripts
- Create (unassigned; router will pick): `bin/add-task.sh <title> [kind] [priority] [description] [link] [notes]`
- Create: `bin/add-dyad-task.sh <title> <dyad> [actor] [critic] [priority] [description] [link] [notes]` (optional: set `DYAD_TASK_KIND=...`)
- Update: `bin/update-dyad-task.sh <id> <status> [notes] [actor] [critic]`
- Report: `bin/dyad-report.sh <dyad> [chat_id]` (posts a feedback entry summarizing beats and open dyad tasks; use cron or critic hook)

## Flow (router → dyad)
1) Router logs a task via `bin/add-task.sh` (or directly with dyad via `bin/add-dyad-task.sh`). Manager notifies Telegram.
2) Router assigns `dyad`/`actor`/`critic` (service: `router`), or tasks can be created already assigned.
3) Critic claims work via `POST /dyad-tasks/claim` and maintains a heartbeat by re-claiming periodically.
4) Critic drives prompts; Actor executes. Critic updates status to `review` or `blocked` with notes. When done, set `done` (and include notes/link to deliverable).
4) Anyone can list tasks: `curl -fsSL http://localhost:9090/dyad-tasks` (or via Manager service name inside the Swarm network).
5) Periodic reporting: run `bin/dyad-report.sh <dyad> [chat_id]` to log status to Manager feedback (can be scheduled).

## Assignment enforcement
Manager enforces dyad assignment rules on create/update/claim.
Defaults (see `docker-stack.yml`):
- `DYAD_REQUIRE_ASSIGNMENT=true`: non-`todo` statuses require a dyad.
- `DYAD_ALLOW_UNASSIGNED=true`: `todo` can be unassigned.
- `DYAD_ALLOW_POOL=true`: `pool:<department>` is allowed for `todo` routing.
- `DYAD_REQUIRE_REGISTERED=true`: dyads must be registered (via roster or `bin/register-dyad.sh`).
- `DYAD_ENFORCE_AVAILABLE=true`: dyads must be marked available.
- `DYAD_MAX_OPEN_PER_DYAD=10`: cap open tasks per dyad.

## Best practices
- Keep descriptions short; include a `link` to the spec/issue for details.
- Use `notes` on updates to surface blockers or review instructions.
- Always update status transitions: `todo → in_progress → review/done` (use `blocked` with a short reason).
- For usage-aware routing, set `dyad` to `pool:<department>` (e.g., `pool:infra`), or use router rules that return a `pool:` target so the router can pick a healthy dyad.
- For login/OAuth or other human-in-loop steps, pair a dyad task with a Beam entry (see `docs/beams.md`) and link it in the task `notes` or `link`.
- For Critic-driven multi-turn Codex execution, use the Codex Loop mechanism (see `docs/dyad-codex-loop.md`).
- Keep `docs/beam_messages/` updated when sending human-facing commands/URLs.
- Set `CRITIC_ID` to a stable identifier (container name) so `claimed_by` stays consistent across container recreation (prevents temporary claim conflicts).

## Status inquiry & reporting
- Critic responds to inquiries by reading `GET /dyad-tasks` and posts concise status in Telegram if asked.
- On completion, update status `done` and include final link/notes; optional feedback via `POST /feedback` for lessons learned.
