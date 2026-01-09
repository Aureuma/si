# Credentials Handling and Rotation

## Telegram bot token
- Stored in `secrets/telegram_bot_token` (raw token, no quotes). Ignored by git.
- Rotation: update the file and restart `silexa-telegram-bot` (`docker restart silexa-telegram-bot`).
- Chat ID is set via `.env` or env var `TELEGRAM_CHAT_ID`; rotate by editing `.env` and restarting services.

## General guidance
- Keep secrets in `secrets/` files, not in git or images.
- Use `*_FILE` env vars when possible (e.g., `TELEGRAM_BOT_TOKEN_FILE`).
- Limit secret mounts to the services that need them (currently only `telegram-bot`).
- For new secrets, add files under `secrets/` and mount only into the services that need them.
- Prefer SOPS + age for encrypted secrets in git (see `docs/secrets.md`).

## Credentials dyad (silexa-credentials)
- `silexa-credentials` is the only dyad authorized to decrypt/apply secrets.
- Other dyads must request secrets via MCP (`credentials.request_secret`) with a clear justification.
- The credentials dyad reviews and resolves access requests (`credentials.list_requests` / `credentials.resolve_request`), then decrypts and shares only the specific key (`credentials.reveal_secret`).
- Registry: `configs/credentials-registry.json` (controls which secrets/keys are eligible).
- Encrypted secret files live in `secrets/*.sops.*`.
- The credentials dyad receives `CREDENTIALS_APPROVER_TOKEN` via `secrets/credentials_mcp_token` so only it can approve/reveal secrets.

## Actors/Critics
- No secrets mounted by default. Prefer OAuth-style flows where possible (e.g., `codex login` via browser).

Codex CLI credentials persistence:
- Each dyad has its own volume (`silexa-codex-<dyad>`) mounted at `/root/.codex` for both actor and critic containers.
- OAuth does not need to be repeated after container recreation as long as the volume remains.

## OAuth callbacks
- Codex login beams provide a host port for OAuth callbacks.
- Do not store credentials in git; use short-lived OAuth flows and keep the tunnel alive until callback succeeds.

## Manager
- No secrets. State is stored in Temporal (no local data volume).

## Rotation playbook
1) Update secret file (e.g., `secrets/telegram_bot_token`).
2) Restart the bot (`docker restart silexa-telegram-bot`).
3) Send a test notification if needed.
