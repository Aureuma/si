# Codex Skills + Interactive Driver Plan (SI)

Date: 2026-02-11

## Official Codex Documentation Findings

Sources consulted:
- Codex configuration and skills docs: https://developers.openai.com/codex/configuration
- Skills section details (`/skills` command, file layout): https://developers.openai.com/codex/configuration#skills
- Slash commands list: https://developers.openai.com/codex/cli#slash-commands
- Config basics and `CODEX_HOME` path: https://developers.openai.com/codex/configuration#config-basics

Key points:
- Codex skills are discovered from `$CODEX_HOME/skills`.
- Each skill is a folder containing `SKILL.md` with YAML frontmatter (`name`, `description`).
- Skills can be managed from the interactive CLI (`/skills`) and can be disabled per skill.
- Slash commands include `/status`, `/model`, `/approval`, `/sandbox`, `/agents`, `/prompts`, `/review`, `/compact`, `/clear`, `/help`, `/logout`, `/exit`, `/vim`.

## SI Architecture Mapping

Current SI behavior before this change:
- Regular codex containers mount per-container codex volume at `/home/si/.codex`.
- DYAD actor/critic mount a shared per-dyad codex volume at `/root/.codex`.
- This means skills were not guaranteed to be globally shared across all codex containers.

Design goals:
1. Make a baseline skill set available in all codex containers (regular + dyad).
2. Keep skills shared across container instances.
3. Preserve override flexibility (settings/flags).
4. Add deterministic interactive driving for slash-command automation and regression tests.

## Proposed Design

### Skills distribution

1. Bundle baseline skills in image at `/opt/si/codex-skills`.
2. Introduce shared skills Docker volume default: `si-codex-skills`.
3. Mount shared skills volume at `$CODEX_HOME/skills` for:
   - regular containers: `/home/si/.codex/skills`
   - dyad containers: `/root/.codex/skills`
4. During container init, copy bundled skills into mounted skills path (idempotent merge).

### Settings and flags

Add settings:
- `[codex].skills_volume`
- `[dyad].skills_volume`

Add CLI flag:
- `--skills-volume` for `si spawn`, `si respawn`, `si run --one-off`, and `si dyad spawn`.

### Interactive driver

Add `codex-interactive-driver` tool with PTY control and scripted actions:
- `wait_prompt[:duration]`
- `send:<line>`
- `type:<text>`
- `key:<enter|tab|esc|up|down|left|right|ctrl-c>`
- `sleep:<duration>`
- `wait_contains:<substring>[|duration]`

This allows deterministic scripted interaction with Codex-compatible REPLs, including slash command sequences and menu navigation.

## Critical Review

### Risk 1: nested mount behavior at `$CODEX_HOME/skills`
- Risk: mounting a subpath under an existing codex volume can behave differently across environments.
- Mitigation: keep initialization idempotent and test both regular and dyad containers.

### Risk 2: skill drift between image bundle and runtime volume
- Risk: stale volume content after image updates.
- Mitigation: init path always copies bundled skills into mounted skills dir.

### Risk 3: brittle prompt detection in automation driver
- Risk: prompt shape can vary by Codex version/theme.
- Mitigation: configurable prompt regex and explicit timeout-based failure with output tail.

### Risk 4: false confidence from fake harness only
- Risk: fake REPL diverges from real Codex behavior.
- Mitigation: keep harness deterministic for CI; pair with smoke checks in real containers when auth/session exists.

## Revised Implementation Streams

1. Skills plumbing (mounts/settings/flags)
2. Skill pack definition (initial SI skills)
3. Runtime init synchronization (entrypoint + codex-init)
4. Interactive driver implementation (tool + tests)
5. Container parity validation (regular + dyad smoke)

## Validation Matrix

1. Unit tests
- docker spec tests include skills volume mounts.
- interactive driver parsing/execution tests.

2. Regular container smoke
- spawn container, verify `/home/si/.codex/skills` has skill directories.

3. DYAD smoke
- spawn dyad (`--skip-auth` + fake codex), verify actor/critic both see `/root/.codex/skills`.

4. Interactive command automation
- run scripted slash commands against fake codex harness and verify no crash/timeout except intentional `/exit` termination.

## Initial Baseline Skills

- `si-vault-ops`
- `si-dyad-ops`
- `si-provider-debug`

All are intentionally concise and operational for daily SI workflows.
