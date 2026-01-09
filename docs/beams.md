# Beams

A Beam is a repeatable, registered human-in-the-loop runbook. Each Beam captures:
- Trigger: when to run it.
- Automation: what the agent does automatically.
- Human action: the exact command/message sent to operators (via Telegram).
- Exit: how to verify and close the task.

All Beams are executed by Temporal workflows (dyad critics do not run Beam logic).
If a Beam kind has no workflow implementation, the task is marked `blocked` with a note.

## `codex_login` Beam (Codex CLI OAuth)
Goal: get Codex CLI authenticated inside an actor without exposing extra context—humans receive only the run command.

Flow:
1) Create a Dyad Task Board item with kind `beam.codex_login` (router can auto-assign) and set `actor`/`critic` if known.
2) Temporal Beam workflow runs `codex login --port <port>` inside the actor and captures:
   - The callback port (the `localhost:<port>` shown in output).
   - The full long OAuth URL printed by `codex login`.
3) Workflow sets up a local-forward bridge because Codex binds to `127.0.0.1` inside the container (see “Forwarding nuance” below).
4) Workflow sends the human a ready-to-run Telegram message (using `parse_mode="HTML"`) in this exact shape:
   - Header: `🔐 <b>Codex login</b>`
   - Body:
     - `<b>🛠 Tunnel:</b>` in a `<pre><code>…</code></pre>` block (optional when running remotely)
     - `<b>🌐 URL:</b>` in a `<pre><code>…</code></pre>` block
5) Human opens the auth URL in the browser (and runs the tunnel command if provided).
6) Workflow verifies success via `codex login status` inside the actor, stops the forwarder, and updates the Dyad Task to `done`.

Expectations:
- No secrets in the message beyond the tunnel command + OAuth URL.
- Use a short timeout window (e.g., 20m) to avoid stale tunnel requests.

Forwarding nuance:
- Codex binds to `127.0.0.1:<port>` inside the container, so port-forwarding directly to `<port>` can fail.
- Fix: run a TCP forwarder inside the container, then forward to that port:
  - Forward inside container: `socat tcp-listen:<forward_port>,reuseaddr,fork tcp:127.0.0.1:<port>`
  - Expose via Docker host port; Beam includes the host port in the Telegram message.

Compatibility note:
- Some Codex CLI builds do not support `codex login --port`. In that case, run `codex login` and capture the printed `localhost:<port>` from the output; the Beam uses that port for the port-forward.

Ready-to-run defaults:
- Helper script to create the Beam task manually: `silexa beam codex-login <dyad> [port]` (Temporal workflow will pick it up).

Recorded Telegram message examples: see `docs/beam_messages/` for the latest sent commands and URLs (e.g., `codex_login_actor_infra.txt` captures the exact tunnel + auth URL shared).

## `codex_account_reset` Beam (Account switch cleanup)
Goal: clear Codex CLI state so the dyad can log in with a different account.

Flow:
1) Create a Dyad Task Board item with kind `beam.codex_account_reset`.
2) Optional notes:
   - `[beam.codex_account_reset.targets]=actor,critic`
   - `[beam.codex_account_reset.paths]=/root/.codex,/root/.config/openai-codex,/root/.config/codex,/root/.cache/openai-codex,/root/.cache/codex`
   - `[beam.codex_account_reset.reason]=<why>`
3) Temporal workflow deletes Codex state in each target container and re-runs `silexa-codex-init` to restore baseline config.
4) Workflow waits ~30s, then auto-queues a `beam.codex_login` task (if one isn’t already open).
5) Task marked `done`; follow the login beam if OAuth is required.

Ready-to-run helper:
- `silexa beam codex-reset <dyad> [targets] [paths] [reason]`

## `dyad_bootstrap` Beam (Dyad provisioning)
Goal: create the dyad containers and volumes in a deterministic sequence.

Flow:
1) Create a Dyad Task Board item with kind `beam.dyad_bootstrap` and set `dyad`, `actor`, `critic`.
2) Include notes:
   - `[beam.dyad_bootstrap.role]=<role>`
   - `[beam.dyad_bootstrap.department]=<department>`
3) Temporal workflow applies the dyad resources (containers + volumes) and waits for readiness.
4) Workflow updates the dyad task to `done` and marks the dyad as active in the registry.

Ready-to-run helper:
- `silexa beam dyad-bootstrap <dyad> [role] [department]`

## Creating new Beams
1) Define the automation + human action split.
2) Add a helper CLI command (under `silexa beam`) that creates a Manager task (Temporal Beam workflow sends the Telegram message).
3) Record the Beam in this file with trigger, automation, human command, and exit criteria.
4) Prefer minimal human-facing text—just the command to run and a short note.
