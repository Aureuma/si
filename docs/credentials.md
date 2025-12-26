# Credentials Handling and Rotation

## Telegram bot token
- Stored in `secrets/telegram_bot_token` (raw token, no quotes). Ignored by git.
- Rotation: `bin/rotate-telegram-token.sh <new_token>` (updates the file, syncs the Kubernetes secret, restarts `silexa-telegram-bot`).
- Chat ID is set via `.env` or env var `TELEGRAM_CHAT_ID`; rotate by editing `.env` and restarting services.

## General guidance
- Keep secrets in `secrets/` files, not in git or images.
- Use `*_FILE` env vars when possible (e.g., `TELEGRAM_BOT_TOKEN_FILE`).
- Limit secret mounts to the services that need them (currently only `telegram-bot`).
- For new secrets, create Kubernetes secrets and mount only into the services that need them.

## Actors/Critics
- No secrets mounted by default. Prefer OAuth-style flows where possible (e.g., `codex login` via browser).

Codex CLI credentials persistence:
- Each dyad has its own PVC (`codex-<dyad>`) mounted at `/root/.codex` for both actor and critic containers.
- OAuth does not need to be repeated after pod recreation as long as the PVC remains.

## Port-forward (OAuth callbacks)
- Codex login beams use `kubectl port-forward pod/<pod> <local>:<forward>` for OAuth callbacks.
- Do not store credentials in git; use short-lived OAuth flows and keep the port-forward alive until callback succeeds.

## Manager
- No secrets. State is stored in Temporal (no local data volume).

## Rotation playbook
1) Update secret file (e.g., `bin/rotate-telegram-token.sh <new_token>`).
2) Verify `kubectl rollout status deployment/silexa-telegram-bot -n silexa`.
3) Send a test notification if needed.
