# si Substrate

`si` is an AI-first substrate for orchestrating multiple coding agents (Dyads) and Codex containers on vanilla Docker.

## Layout
- `agents/`: Agent-specific code and tooling.
- `tools/si`: Go-based CLI for Docker workflows (dyads, codex containers, image helpers).
- `tools/si-image`: Unified Docker image for Codex containers and dyad actor/critic runtime.

## Quickstart (Docker)
Requires Docker Engine.

Build the CLI and images:

```bash
go build -o si ./tools/si
./si images build
```

This builds the unified image `aureuma/si:local` used by dyads and codex containers.

## Testing
Run all module tests from repo root:

```bash
./tools/test.sh
```
## Dyads (Actor + Critic)
- Spawn a dyad (actor+critic):

```bash
./si dyad spawn <name> [role] [department]
```

- Teardown a dyad:

```bash
./si dyad remove <name>
```

- Run commands in an actor:

```bash
./si dyad exec --member actor <dyad> -- bash
```

- List running dyads:

```bash
./si dyad list
```

By default, dyads mount the current directory; when run from the repo root they share the repo workspace. Use `--workspace` to override.

## Codex containers (on-demand)
Spawn standalone Codex containers with isolated auth:

```bash
./si images build
./si spawn <name> --repo Org/Repo --gh-pat <token>
```

Clone later into an existing container:
```bash
./si clone <name> Org/Repo --gh-pat <token>
```

Each container uses its own persistent `~/.codex` volume so multiple Codex accounts can coexist on the same host.
By default, `si spawn` mounts the current directory as `/workspace`; use `--workspace` to override.

## Warm Weekly Limits
`si` can auto-bootstrap weekly usage timers for logged-in Codex profiles:

```bash
./si warm-weekly enable
./si warm-weekly reconcile
./si warm-weekly status
./si warm-weekly disable
```

Behavior:
- `enable` installs an hourly scheduler and triggers immediate reconcile.
- `reconcile` warms profiles that are logged in but still at fresh weekly usage.
- `status` shows per-profile warm state and next due time.

## Codex CLI login flow (pattern)
1) Actor runs `codex login` (gets a local callback URL + port).
2) Human opens the URL on a browser-capable machine and completes OAuth.
3) Actor resumes once callback is received.

## Access notes
- Keep container privileges minimal; only the critic needs the Docker socket; the actor does not.

## Next steps
- Update Codex CLI (or another LLM-driven CLI) inside actor containers as needed.
- Add more dyads via `si dyad spawn`.
