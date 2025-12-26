# Beams

A Beam is a repeatable, registered human-in-the-loop runbook. Each Beam captures:
- Trigger: when to run it.
- Automation: what the agent does automatically.
- Human action: the exact command/message sent to operators (via Telegram).
- Exit: how to verify and close the task.

## `codex_login` Beam (Codex CLI OAuth)
Goal: get Codex CLI authenticated inside an actor without exposing extra context—humans receive only the run command.

Flow:
1) Create a Dyad Task Board item with kind `beam.codex_login` (router can auto-assign) and set `actor`/`critic` if known.
2) Critic runs `codex login --port <port>` inside the actor and captures:
   - The callback port (the `localhost:<port>` shown in output).
   - The full long OAuth URL printed by `codex login`.
3) Critic sets up a local-forward bridge because Codex binds to `127.0.0.1` inside the container (see “Forwarding nuance” below).
4) Critic sends the human a ready-to-run Telegram message (using `parse_mode="HTML"`) in this exact shape:
   - Header: `🔐 <b>Codex login</b>`
   - Body:
     - `<b>🛠 Port-forward:</b>` in a `<pre><code>…</code></pre>` block
     - `<b>🌐 URL:</b>` in a `<pre><code>…</code></pre>` block
5) Human runs the port-forward command and opens the auth URL in the browser.
6) Critic verifies success via `codex login status` inside the actor and updates the Dyad Task to `done`.

Expectations:
- No secrets in the message beyond the tunnel command + OAuth URL.
- Use a short timeout window (e.g., 20m) to avoid stale tunnel requests.

Forwarding nuance:
- Codex binds to `127.0.0.1:<port>` inside the container, so port-forwarding directly to `<port>` can fail.
- Fix: run a TCP forwarder inside the pod, then forward to that port:
  - Forward inside pod: `socat tcp-listen:<forward_port>,reuseaddr,fork tcp:127.0.0.1:<port>`
  - Port-forward: `kubectl -n silexa port-forward pod/<pod> <port>:<forward_port>`

Compatibility note:
- Some Codex CLI builds do not support `codex login --port`. In that case, run `codex login` and capture the printed `localhost:<port>` from the output; the Beam uses that port for the port-forward.

Ready-to-run defaults:
- Namespace defaults to `silexa` unless `SILEXA_NAMESPACE` is set.
- Helper script to run this Beam manually: `bin/beam-codex-login.sh <dyad> [port]`.

Recorded Telegram message examples: see `docs/beam_messages/` for the latest sent commands and URLs (e.g., `codex_login_actor_infra.txt` captures the exact tunnel + auth URL shared).

## Creating new Beams
1) Define the automation + human action split.
2) Add a helper script under `bin/beam-*.sh` that creates a Manager task and notifies Telegram with the precise human command.
3) Record the Beam in this file with trigger, automation, human command, and exit criteria.
4) Prefer minimal human-facing text—just the command to run and a short note.
