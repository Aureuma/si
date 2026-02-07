# si Substrate

`si` is an AI-first substrate for orchestrating multiple coding agents (Dyads) and Codex containers on vanilla Docker.

## Layout
- `agents/`: Agent-specific code and tooling.
- `tools/si`: Go-based CLI for Docker workflows (dyads, codex containers, image helpers).
- `tools/si-image`: Unified Docker image for Codex containers and dyad actor/critic runtime.

## Quickstart (Docker)
Requires Docker Engine.

Build the CLI and runtime image:

```bash
go build -o si ./tools/si
./si image build
```

This builds the unified image `aureuma/si:local` used by dyads and codex containers.
`si image build` is the only image-build command surface.

## Testing
Run all module tests from repo root:

```bash
./tools/test.sh
```

Run static analysis across go.work modules:

```bash
./si analyze
```

Scope to a module or keep CI green while reviewing findings:

```bash
./si analyze --module tools/si
./si analyze --no-fail
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
./si image build
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

Run/attach workflows:

```bash
# open shell in existing container
./si run <container-or-profile>

# attach to persistent codex tmux pane
./si run <container-or-profile> --tmux

# ensure autopoietic companion sidecar, then attach tmux codex pane
./si run <container-or-profile> --autopoietic --tmux
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

## Stripe
`si` includes a Stripe bridge command family:

```bash
./si stripe auth status
./si stripe context list
./si stripe context use --account core --env sandbox
./si stripe object list product --limit 20
./si stripe object create product --param name=Starter
./si stripe raw --method GET --path /v1/balance
./si stripe report balance-overview
./si stripe sync live-to-sandbox plan --account core
./si stripe sync live-to-sandbox apply --account core --dry-run
```

Environment policy:
- Use `live` and `sandbox`.
- `test` is intentionally rejected as a CLI environment mode.

## Self Build/Upgrade
Build or upgrade the `si` binary from the repo itself:

```bash
# dev checkout build
./si self build --output ./si

# explicit stable upgrade of installed binary
si self upgrade

# run current checkout without rebuilding a binary artifact
si self run -- version
```

## Codex CLI login flow (pattern)
1) Actor runs `codex login` (gets a local callback URL + port).
2) Human opens the URL on a browser-capable machine and completes OAuth.
3) Actor resumes once callback is received.

## Access notes
- Keep container privileges minimal; only the critic needs the Docker socket; the actor does not.

## Next steps
- Update Codex CLI (or another LLM-driven CLI) inside actor containers as needed.
- Add more dyads via `si dyad spawn`.
