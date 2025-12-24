# Codex model + reasoning policy (Dyads)

All dyads should run the latest Codex model:
- `CODEX_MODEL=gpt-5.1-codex-max`

Reasoning level is controlled via:
- `CODEX_REASONING_EFFORT=low|medium|high|xhigh`

These values are applied in two places:
1) **Default Codex config**: `bin/codex-init.sh` writes `~/.config/codex/config.toml` with `model` and `model_reasoning_effort`.
2) **Critic-driven execution**: when a critic runs `codex exec` inside an actor, it explicitly passes `-m $CODEX_MODEL` and `-c model_reasoning_effort=$CODEX_REASONING_EFFORT`.

## Default policy

Recommended defaults (can be overridden per container via env):
- **infra dyad**
  - actor: `xhigh`
  - critic (driver): `high`
- **web dyad**
  - actor: `high`
  - critic (driver): `high`
- **research dyad**
  - actor: `xhigh`
  - critic (driver): `high`
- **pm dyad**
  - actor: `low` (planning)
  - critic (driver): `xhigh` (program-level reasoning)

## Spawned dyads

`bin/spawn-dyad.sh` sets:
- `CODEX_MODEL` (defaults to `gpt-5.1-codex-max`)
- `CODEX_REASONING_EFFORT` for actor/critic based on `ROLE`
- `CODEX_PER_DYAD=1` by default so Codex state (`~/.codex` / `~/.config/codex`) is not shared across dyads. Override to `0` only if you explicitly want a shared store.

Overrides:
- `CODEX_MODEL=...`
- `CODEX_ACTOR_EFFORT=...`
- `CODEX_CRITIC_EFFORT=...`

## Compose dyads

`docker-compose.yml` sets `CODEX_MODEL` and `CODEX_REASONING_EFFORT` per service (actor/critic).
