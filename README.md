# Silexa Substrate

Silexa is an AI-first substrate for orchestrating multiple coding agents (Dyads) and Codex containers on vanilla Docker.

## Layout
- `apps/`: Application repos built by agents (one repo per app).
- `agents/`: Agent-specific code and tooling.
- `tools/silexa`: Go-based CLI for Docker workflows (dyads, codex containers, app helpers).
- `actor`: Node base image for interactive CLI agents (Codex CLI preinstalled; update inside container as needed).
- `critic`: Go helper that prepares Codex config and idles.

## Quickstart (Docker)
Build the CLI and images:

```bash
go build -o si ./tools/silexa
./si images build
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

All dyads share the repo workspace.

## Codex containers (on-demand)
Spawn standalone Codex containers with isolated auth:

```bash
./si images build
./si codex spawn <name> --repo Org/Repo --gh-pat <token>
```

Clone later into an existing container:
```bash
./si codex clone <name> Org/Repo --gh-pat <token>
```

Each container uses its own persistent `~/.codex` volume so multiple Codex accounts can coexist on the same host.

## Codex CLI login flow (pattern)
1) Actor runs `codex login` (gets a local callback URL + port).
2) Human opens the URL on a browser-capable machine and completes OAuth.
3) Actor resumes once callback is received.

## Secrets and access notes
- Tokens live in `secrets/` (mounted into the relevant containers).
- Keep container privileges minimal; actors/critics do not need a container runtime socket.

## Next steps
- Update Codex CLI (or another LLM-driven CLI) inside actor containers as needed.
- Add more dyads via `si dyad spawn`.
