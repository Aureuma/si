# Silexa CLI Plan (Go)

Goal: a single Go CLI that can fully operate the Silexa stack and dyads without any JS tooling. The CLI should be stable, scriptable, and safe for automation.

## Objectives
- One binary (`silexa`) covering stack, dyad lifecycle, tasking, and reporting.
- Deterministic outputs for scripting (plain text + JSON modes where helpful).
- Docker-only workflows (no cluster schedulers, no external control planes).
- Clear error messages and actionable remediation hints.

## Command surface (breadth)
Core control
- `stack up|down|status`: start/stop core services and report container health.
- `dyad spawn|list|remove|recreate|status|exec|logs|restart|register|cleanup`: full dyad lifecycle.
- `task add|add-dyad|update`: create and manage dyad tasks.
- `human add|complete`: human-in-the-loop tasks.
- `feedback add|broadcast`: feedback + alerts.
- `access request|resolve`: access and secret requests.
- `metric post`: structured metrics.
- `notify`: Telegram notification helper.

Build/app
- `images build`: build all core images.
- `image build`: build a single image.
- `app init|adopt|list|build|deploy|remove|status|secrets`: app lifecycle helpers.

Supporting
- `report status|escalate|review|dyad`: management summaries.
- `roster apply|status`: dyad roster sync.
- `mcp scout|sync|apply-config`: MCP gateway helpers.
- `profile` and `capability`: role context and guardrails.

## UX and interaction
Flags and defaults
- All commands accept `--help` with usage, flags, and examples.
- Environment variables mirror flags with clear precedence: flag > env > default.
- Provide `--json` on read/reporting commands for scripting.

Output conventions
- Human readable output by default, aligned columns for tables.
- Errors to stderr with a short hint for recovery (e.g., missing Docker socket).
- Success paths return minimal output to keep logs clean.

Safety and idempotency
- `stack up` should be idempotent: create missing resources, leave running ones untouched.
- `dyad spawn` should be idempotent (or refuse with a clear message).
- `cleanup` only prunes stopped containers/images; never remove active containers.

## State, config, and persistence
- Manager state is persisted on disk (default `/data/manager_state.json`).
- CLI should never write state directly; always go through manager APIs.
- Configs live in `configs/` and should be mounted read-only into services.

## Coverage gaps to close
- Add `--json` to roster/status/report commands.
- Provide `--dry-run` variants for stack/dyad operations.
- Add `silexa stack logs` to tail core services.
- Emit structured exit codes for common failure cases (Docker unreachable, bad input, etc.).

## Error handling and resilience
- Retry transient HTTP calls to Manager with bounded backoff.
- If Manager is down, commands should surface a single clear error and exit non-zero.
- For network failures, show target URL and hint to check `MANAGER_URL`.

## Test plan
- Unit tests for CLI parsing and validation.
- Integration tests using Docker: `stack up`, `stack status`, `dyad spawn`, `task add`.
- Smoke tests for HTTP endpoints (`/healthz`, `/beats`, `/dyad-tasks`).
- Golden output tests for table formatting.

## Roadmap phases
Phase 1: Hardening
- Improve input validation, add `--json` output.
- Tighten error messages; add retry wrappers for HTTP.

Phase 2: Observability
- `stack logs`, `dyad tail`, structured errors/exit codes.

Phase 3: Automation
- Config-driven workflows (e.g., scripted program runs).
- Subcommand autocompletion and manpage generation.
