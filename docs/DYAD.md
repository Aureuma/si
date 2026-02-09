# Dyads

This repo supports running a paired **actor** + **critic** "dyad" in Docker. The critic runs a closed loop that:

- starts (or recovers) an interactive Codex session for the critic and the actor inside `tmux`
- sends prompts via `tmux send-keys`
- waits for a delimited work report between `<<WORK_REPORT_BEGIN>>` and `<<WORK_REPORT_END>>`
- persists artifacts under `.si/dyad/<dyad>/reports/` in the workspace

## Requirements

- Docker available on the host
- `tmux` installed in both containers (included in `aureuma/si:local`)
- A logged-in `si login` profile for real Codex runs (or use the offline fake Codex flow below)
- If Docker is root-only on your host, run `si dyad ...` as root and set `SI_HOST_UID`/`SI_HOST_GID` so artifacts are owned by your user.

## Spawn + Inspect

Spawn a dyad:

```bash
si dyad spawn <name> [role] [department]
```

Check status:

```bash
si dyad status <name>
si dyad logs --member critic <name> --tail 200
si dyad logs --member actor <name> --tail 200
```

## Peek Into Running Sessions (tmux)

To "peek" into the running interactive Codex sessions (even mid-turn):

```bash
si dyad peek <name>
```

Flags:

- `--member actor|critic|both` (default `both`)
- `--detached` to create the host `tmux` session without attaching
- `--session <name>` to override the host peek session name

Under the hood, dyads use tmux session names:

- actor (inside actor container): `si-dyad-<dyad>-actor`
- critic (inside critic container): `si-dyad-<dyad>-critic`

You can also attach manually:

```bash
docker exec -it si-actor-<dyad> tmux attach -t si-dyad-<dyad>-actor
docker exec -it si-critic-<dyad> tmux attach -t si-dyad-<dyad>-critic
```

## Enforcement: Interactive Only + Strict Parsing

The critic loop is designed to drive **interactive** Codex sessions in `tmux` (not `codex exec`).

By default, it enforces **strict** work-report parsing:

- `DYAD_LOOP_STRICT_REPORT=1` (default)
- reports must be delimited by the markers above
- if Codex returns to a ready prompt without a delimited report, the turn fails fast and retries

To override the interactive command used to start Codex (mainly for offline testing), set `DYAD_CODEX_START_CMD`. It rejects `codex exec` to keep dyads interactive-only.

## Tuning

Useful environment variables (set on the host before `si dyad spawn`, or passed into the critic container):

- `DYAD_LOOP_TURN_TIMEOUT_SECONDS`: per-turn timeout (default `900`)
- `DYAD_LOOP_RETRY_MAX`: retries per actor/critic turn (default `3`)
- `DYAD_LOOP_TMUX_CAPTURE`: `main` or `alt` (default `main`)
- `DYAD_LOOP_TMUX_CAPTURE_LINES`: capture last N lines from tmux (default `8000`)
- `DYAD_LOOP_PROMPT_LINES`: prompt readiness scan depth (default `3`)

## Offline Smoke Tests (No Codex Auth)

If you want to validate tmux + parsing + turn-taking without Codex auth, you can run the dyad loop against `tools/dyad/fake-codex.sh`.

Example:

```bash
export DYAD_CODEX_START_CMD='cd /workspace && exec /workspace/tools/dyad/fake-codex.sh'
export SI_HOST_UID=1000
export SI_HOST_GID=1000
export DYAD_LOOP_ENABLED=1
export DYAD_LOOP_MAX_TURNS=1
export DYAD_LOOP_STRICT_REPORT=1

# Optional: make turns "long" so you can peek mid-run.
export FAKE_CODEX_DELAY_SECONDS=10
export FAKE_CODEX_LONG_LINES=12000

si dyad spawn dyad-offline-test --skip-auth
si dyad peek dyad-offline-test
```
