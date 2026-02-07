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
./si dyad spawn <name> [role] [department] --profile <profile>
```

- Teardown a dyad:

```bash
./si dyad remove <name>
```

- Run commands in an actor:

```bash
./si dyad exec --member actor <dyad> -- bash
```

- Stop/start dyad containers:

```bash
./si dyad stop <name>
./si dyad start <name>
```

- List running dyads:

```bash
./si dyad list
```

Dyads use existing `si login` profiles for Codex auth (no separate dyad login flow).  
By default, dyad spawn uses the active/default profile when available, or lets you choose interactively. Use `--profile` to force a specific profile.  
If no logged-in profile is available, run `si login` first.
By default, dyads mount the current directory; when run from the repo root they share the repo workspace. Use `--workspace` to override.

## Codex containers (on-demand)
Spawn standalone Codex containers with isolated auth:

```bash
./si images build
./si spawn --profile america --repo Org/Repo --gh-pat <token>
```

Clone later into an existing container:
```bash
./si clone america Org/Repo --gh-pat <token>
```

Each container uses its own persistent `~/.codex` volume so multiple Codex accounts can coexist on the same host.
By default, `si spawn` mounts the current directory as `/workspace`; use `--workspace` to override.
When a profile is selected, `si` uses that profile ID as the container name and enforces one container per profile.

Inspect profiles and usage:

```bash
./si status
./si status --no-status
./si status <profile>
```

## Warmup
`si` can auto-bootstrap weekly usage timers for logged-in Codex profiles:

```bash
./si warmup enable
./si warmup reconcile
./si warmup status
./si warmup disable
```

Behavior:
- `enable` installs an hourly scheduler and triggers immediate reconcile.
- `reconcile` warms profiles that are logged in but still at fresh weekly usage.
- `status` shows per-profile warm state and next due time.
- `si login` triggers `warmup enable --profile <profile>` automatically after successful auth cache write.
- `--quiet` suppresses warmup command output.

## Codex CLI login flow (pattern)
1) Actor runs `codex login` (gets a local callback URL + port).
2) Human opens the URL on a browser-capable machine and completes OAuth.
3) Actor resumes once callback is received.

## Access notes
- Keep container privileges minimal; only the critic needs the Docker socket; the actor does not.

## Next steps
- Update Codex CLI (or another LLM-driven CLI) inside actor containers as needed.
- Add more dyads via `si dyad spawn`.
