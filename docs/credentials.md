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
- No secrets mounted by default. Prefer OAuth-style flows where possible (e.g., `codex login` via browser).

Codex CLI credentials persistence:
- **Compose dyads** persist per-dyad docker named volumes mounted at `/root/.codex`:
  - `codex_state_web` for `actor-web` + `critic-web`
  - `codex_state_research` for `actor-research` + `critic-research`
  OAuth does not need to be repeated after container recreation (within the same dyad volume).
- **Spawned dyads** (`bin/spawn-dyad.sh <name> ...`) default to a **shared** host directory `data/codex/shared/{actor,critic}` mounted at `/root/.codex` (override with `CODEX_PER_DYAD=1` to isolate per-dyad as `data/codex/<dyad>/{actor,critic}`).

## SSH target (tunnels)
- Beams that require SSH tunnels (e.g., Codex OAuth callbacks) use `SSH_TARGET=<user>@<public_ip>`.
- Default is recorded in `configs/ssh_target` and injected into dyad critics by `bin/spawn-dyad.sh`.
- Do not store SSH passwords in git; use SSH keys on the operator machine.

## Manager
- No secrets. Persists tasks/feedback in `data/manager` volume.

## Rotation playbook
1) Update secret file (e.g., `bin/rotate-telegram-token.sh <new_token>`).
2) Restart affected service(s) with compose (e.g., `docker compose up -d telegram-bot`).
3) Verify logs and, if applicable, send a test notification.
