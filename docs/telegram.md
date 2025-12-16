## Telegram control plane

The Telegram bot provides a light-weight control plane for humans:

- `/status` — show manager health (tasks, access queue, beats, uptime).
- `/tasks` — list the first few open human tasks.
- `/task Title | command | notes` — create a human task stored in the manager and linked to the current chat. Use this for anything that needs manual action (e.g., Codex login tunnel).
- `/help` — quick usage help.
- Any non-command message is recorded as feedback in the manager (source is the chat ID). Use `/task` for actionable work.

### Deployment
- Token is supplied via docker secret `secrets/telegram_bot_token` (raw token text). Chat ID comes from `TELEGRAM_CHAT_ID` env or per-message payload.
- Service listens on `:8081`; internal notify endpoint is `http://telegram-bot:8081/notify`.
- Manager pushes human task notifications by POSTing to `/notify` when `TELEGRAM_NOTIFY_URL` is set.

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
