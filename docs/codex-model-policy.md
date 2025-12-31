# Codex model + reasoning policy (Dyads)

All dyads should run the latest Codex model:
- `CODEX_MODEL=gpt-5.2-codex`

Reasoning level is controlled via:
- `CODEX_REASONING_EFFORT=medium|high|xhigh` (policy avoids `low`)

These values are applied in two places:
1) **Default Codex config**: `bin/codex-init.sh` writes `~/.codex/config.toml` with `model` and `model_reasoning_effort`.
2) **Critic-driven execution**: when a critic runs interactive `codex`/`codex resume` inside an actor, it explicitly passes `-m $CODEX_MODEL` and `-c model_reasoning_effort=$CODEX_REASONING_EFFORT`.

## Complexity-based overrides

When a dyad task includes `complexity=low|medium|high`, critics map complexity to model/effort:
- Model: `CODEX_MODEL_LOW|MEDIUM|HIGH` if set, otherwise `CODEX_MODEL`.
- Reasoning: `CODEX_REASONING_EFFORT_LOW|MEDIUM|HIGH` if set, otherwise the complexity level itself (`low|medium|high`), with `low` clamped to `medium`.

If `complexity` is empty, critics fall back to `priority` and then to the base env defaults.

## Default policy

Recommended defaults (can be overridden per container via env):
- **infra dyad**
  - actor: `xhigh`
  - critic (driver): `xhigh`
- **web dyad**
  - actor: `medium`
  - critic (driver): `high`
- **research dyad**
  - actor: `high`
  - critic (driver): `high`
- **pm dyad**
  - actor: `high` (planning)
  - critic (driver): `xhigh` (program-level reasoning)
- **default dyad**
  - actor: `medium`
  - critic (driver): `medium`

## Spawned dyads

`bin/spawn-dyad.sh` sets:
- `CODEX_MODEL` (defaults to `gpt-5.2-codex`)
- `CODEX_REASONING_EFFORT` for actor/critic based on `ROLE`
- `CODEX_PER_DYAD=1` by default so Codex state (`~/.codex`) is not shared across dyads. Override to `0` only if you explicitly want a shared store.

Overrides:
- `CODEX_MODEL=...`
- `CODEX_ACTOR_EFFORT=...`
- `CODEX_CRITIC_EFFORT=...`

## Kubernetes defaults

`infra/k8s/silexa/*.yaml` and `bin/spawn-dyad.sh` set `CODEX_MODEL` and `CODEX_REASONING_EFFORT` per deployment (actor/critic).
