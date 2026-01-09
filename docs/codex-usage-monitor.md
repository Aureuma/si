# Codex usage monitor

This service monitors Codex plan usage per ChatGPT account and routes new work away from dyads that are close to their 5-hour limit.

## Components

- **codex-monitor**: polls `codex /status` in each account's permanent dyad, posts usage metrics to Manager, and emits cooldown warnings.
- **router**: selects a dyad from a `pool:<department>` target, avoiding dyads in cooldown when possible.

## Configuration

Accounts list (`configs/codex_accounts.json`):

```json
{
  "cooldown_threshold_pct": 10,
  "total_limit_minutes": 300,
  "poll_interval": "2m",
  "accounts": [
    {
      "name": "infra-primary",
      "dyad": "infra",
      "role": "infra",
      "department": "infra",
      "monitor_role": "critic",
      "spawn": true
    }
  ]
}
```

Fields:
- `dyad` must match the dyad name used in `silexa dyad spawn`.
- `monitor_role` defaults to `critic`.
- `spawn=true` tells `codex-monitor` to create the dyad if missing.
- `codex_home` (optional) points to the HOME directory that contains `.codex/` for that account (preferred for reliable `/status` output).
- `cooldown_threshold_pct` defaults to 10.

## Routing pools

Use `pool:<department>` in router rules or program configs so work can shift to another account when usage is low.

Examples:
- `pool:infra`
- `pool:web`

## Metrics emitted

- `codex.remaining_pct` (percent)
- `codex.remaining_minutes` (minutes)
- `codex.weekly_remaining_pct` (percent)
- `codex.weekly_remaining_minutes` (minutes)
- `codex.cooldown` (bool)

## Telegram status integration

If `CODEX_MONITOR_URL` is configured on the Telegram bot (default: `http://codex-monitor:8086/status`), the `/status` command will append the Codex usage summary from this service.

## Operational notes

- `codex-monitor` runs `codex /status` in a local pseudo-terminal and reads each account's `.codex` state from mounted volumes.
- For read-only `.codex` mounts, the monitor copies the state into a temporary HOME and answers initial prompts (approval + model selection) before issuing `/status`.
- If an account has no `auth.json`, usage shows as `n/a` until the dyad logs in (`codex login`).
- The usage summary includes the account email parsed from the `/status` output when available.
- Weekly quota data is parsed from `/status` lines that mention weekly usage.
- Model name and reasoning effort are parsed from `/status` and shown in the status summary when present.
- When `DYAD_REQUIRE_REGISTERED=true`, codex-monitor skips dyads that are not registered in the Manager registry.
- When a dyad falls below the cooldown threshold, new tasks should route to other dyads in the same pool.
- If no alternative dyads exist, routing falls back to the original target.
- When cooldown is detected, `codex-monitor` can create a `beam.codex_account_reset` task to wipe Codex state and prepare for a new login. The reset beam waits ~30s and auto-queues `beam.codex_login` unless one is already open. Disable with `CODEX_RESET_ON_COOLDOWN=0`.
