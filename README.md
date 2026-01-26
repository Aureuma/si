# Silexa Substrate

Silexa is an AI-first substrate for orchestrating multiple coding agents (Dyads) and Codex containers on vanilla Docker.

## Layout
- `agents/`: Agent-specific code and tooling.
- `tools/silexa`: Go-based CLI for Docker workflows (dyads, codex containers, image helpers).
- `actor`: Node base image for interactive CLI agents (Codex CLI preinstalled; update inside container as needed).
- `critic`: Go helper that prepares Codex config and idles.

## Quickstart (Docker)
Requires Docker Engine.

Build the CLI and images:

```bash
go build -o si ./tools/silexa
./si images build
```


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
./si dyad exec <dyad> --member actor -- bash
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

## Codex CLI login flow (pattern)
1) Actor runs `codex login` (gets a local callback URL + port).
2) Human opens the URL on a browser-capable machine and completes OAuth.
3) Actor resumes once callback is received.

## Access notes
- Keep container privileges minimal; actors/critics do not need a container runtime socket.

## Next steps
- Update Codex CLI (or another LLM-driven CLI) inside actor containers as needed.
- Add more dyads via `si dyad spawn`.
