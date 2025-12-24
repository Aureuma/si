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
- `dyad` must match the dyad name used in `bin/spawn-dyad.sh`.
- `monitor_role` defaults to `critic`.
- `spawn=true` tells `codex-monitor` to create the dyad if missing.
- `cooldown_threshold_pct` defaults to 10.

## Routing pools

Use `pool:<department>` in router rules or program configs so work can shift to another account when usage is low.

Examples:
- `pool:infra`
- `pool:web`

## Metrics emitted

- `codex.remaining_pct` (percent)
- `codex.remaining_minutes` (minutes)
- `codex.cooldown` (bool)

## Operational notes

- `codex-monitor` runs with Docker socket access to exec `codex /status` in dyad containers.
- When a dyad falls below the cooldown threshold, new tasks should route to other dyads in the same pool.
- If no alternative dyads exist, routing falls back to the original target.
