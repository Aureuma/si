# Credentials Handling and Rotation

## Telegram bot token
- Stored in docker secret file: `secrets/telegram_bot_token` (raw token, no quotes). Ignored by git.
- Rotation: `bin/rotate-telegram-token.sh <new_token>` (updates the secret file and restarts `telegram-bot` via compose).
- Chat ID is set via `.env` or env var `TELEGRAM_CHAT_ID`; rotate by editing `.env` and restarting services.

## General guidance
- Keep secrets in `secrets/` files, not in git or images.
- Use `*_FILE` env vars when possible (e.g., `TELEGRAM_BOT_TOKEN_FILE`).
- Limit secret mounts to the services that need them (currently only `telegram-bot`).
- For new secrets, add under `secrets:` in `docker-compose.yml` and mount into the specific service.

## Actors/Critics
- No secrets mounted by default. Provide task-specific credentials via env when exec’ing an actor (e.g., `OPENAI_API_KEY` during `codex login`).

## Manager
- No secrets. Persists tasks/feedback in `data/manager` volume.

## Rotation playbook
1) Update secret file (e.g., `bin/rotate-telegram-token.sh <new_token>`).
2) Restart affected service(s) with compose (e.g., `docker compose up -d telegram-bot`).
3) Verify logs and, if applicable, send a test notification.
