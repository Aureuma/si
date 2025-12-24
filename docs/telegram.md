## Telegram control plane

The Telegram bot provides a light-weight control plane for humans:

- `/status` — show manager health (tasks, access queue, beats, uptime).
- `/tasks` — list the first few open human tasks.
- `/board` — list open dyad tasks (task board snapshot).
- `/task Title | command | notes` — create a human task stored in the manager and linked to the current chat. Use this for anything that needs manual action (e.g., Codex login tunnel).
- `/help` — quick usage help.
- Any non-command message is recorded as feedback in the manager (source is the chat ID). Use `/task` for actionable work.
- Replies to bot messages are also recorded as feedback with context about the original message, keeping discussion threads tied to the notification.

### Deployment
- Token is supplied via docker secret `secrets/telegram_bot_token` (raw token text). Chat ID comes from `TELEGRAM_CHAT_ID` env or per-message payload.
- Service listens on `:8081`; internal notify endpoint is `http://telegram-bot:8081/notify`.
- Manager pushes human task notifications by POSTing to `/notify` when `TELEGRAM_NOTIFY_URL` is set.

### Notification payload (`POST /notify`)

Supported fields:
- `message` (required): text body.
- `chat_id` (optional): override default chat destination.
- `message_id` (optional): if provided, bot edits that existing message instead of sending a new one.
- `parse_mode` (optional): `"HTML"` (recommended for system messages) or `"Markdown"` / `"MarkdownV2"`.
- `disable_web_page_preview` (optional): boolean.
- `disable_notification` (optional): boolean.
- `buttons` (optional): inline URL buttons.

Recommended default for system notifications (dyad tasks, beams): `parse_mode="HTML"` and `disable_web_page_preview=true`.

Response (JSON):
- `{ "ok": true, "edited": <bool>, "message_id": <int> }`

### Editing instead of spamming

For dyad tasks, Manager stores `telegram_message_id` on the task record and uses it to edit the same Telegram message on later updates (instead of posting a new message for every status change).

If you clear chat history (or delete the bot’s messages), Telegram can no longer edit those old messages. The next task update will automatically fall back to sending a new message and re-anchor `telegram_message_id` to the new one.

### Automatic dyad board digest

Manager periodically publishes a single “Dyad Task Board” digest message to Telegram and edits it in place, so you always have a current snapshot without spam.

- Config: `DYAD_TASK_DIGEST_INTERVAL` (default `10m`)
- Persisted anchor: `meta.dyad_digest_telegram_message_id` in Manager’s data file (`/data/tasks.json`)

### Telegram message template (system notifications)

Goals:
- Minimal, operationally useful fields (no actor/critic container names unless needed).
- Emojis reflect classification: status / priority / kind.
- A bold header + bold labels (with colons).
- Times shown in human-readable UTC with weekday.

Template shape (HTML parse mode):
- First line: `<status_emoji> <kind_emoji> <b>Title</b>`
- Then short labeled fields (1 per line), e.g.:
  - `<b>Status:</b> …`
  - `<b>Priority:</b> …`
  - `<b>Kind:</b> …`
  - `<b>Dyad:</b> …`
  - `<b>When (UTC):</b> Wed 2025-12-17 07:30 UTC`
  - Optional `<b>Notes:</b>` / `<b>Command:</b>` in `<pre><code>…</code></pre>`

### Adding a human task from outside Telegram

```bash
curl -X POST http://localhost:9090/human-tasks \
  -H "Content-Type: application/json" \
  -d '{"title":"Codex login","commands":"ssh -N -L 127.0.0.1:47123:ACTOR_IP:PORT user@bastion","requested_by":"infra-dyad","notes":"Open browser to http://127.0.0.1:47123 after tunnel","chat_id":-1003507771562}'
```

### Status checks
- Manager health endpoint: `http://localhost:9090/healthz`.
- Telegram bot health endpoint: `http://localhost:8081/healthz`.

### Rotation & RBAC
- Rotate the token with `bin/rotate-telegram-token.sh <new_token>`; restart only the bot service (`docker compose up -d telegram-bot`).
- Only the Telegram bot mounts the token secret. No containers other than actors/critics/coder-agent receive the docker socket.
