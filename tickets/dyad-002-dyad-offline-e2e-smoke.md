# DYAD-002 Add a Repeatable Offline Dyad E2E Smoke

## Goal

Make it easy to validate the dyad loop invariants without Codex auth by using `tools/dyad/fake-codex.sh`.

## Acceptance Criteria

- Document a single copy-paste command (or short script) that:
  - Builds the image (`si build image`)
  - Spawns a dyad with `DYAD_CODEX_START_CMD=/workspace/tools/dyad/fake-codex.sh`
  - Runs at least 2 turns (`DYAD_LOOP_MAX_TURNS=2`, `sleep_seconds=0`, `startup_delay=0`)
  - Produces artifacts under `.si/dyad/<name>/reports/`
  - Demonstrates the invariants:
    - actor prompt == previous critic message
    - critic prompt == actor report only
- Include how to use `si dyad peek --detached --member both <name>` to inspect panes/titles.

## Suggested Location

- Add a short section to `docs/testing.md` or a dedicated `docs/DYAD_TESTING.md`.

