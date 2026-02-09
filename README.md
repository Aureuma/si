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

## Vault (Secrets)
`si vault` manages encrypted `.env.<env>` files stored in a pinned vault submodule and injects secrets into local processes or `docker exec` at runtime.

See `docs/VAULT.md`.

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

### Dyad closed-loop (critic ↔ actor)
- The critic now runs an automatic turn loop by default when spawned as a dyad member.
- Each turn:
  - critic sends prompt to actor Codex interactive session (tmux in actor container), parses actor work report
  - critic sends prompt to critic Codex interactive session (tmux in critic container), produces next instructions
  - critic feeds the generated critic report back into the next actor turn
- Loop artifacts are written to `.si/dyad/<dyad>/reports` and state to `.si/dyad/<dyad>/loop-state.json`.
- Loop is lightweight-resilient:
  - actor container is auto-started if it was stopped
  - actor/critic tmux sessions are recreated on recoverable turn failures/timeouts
  - retry/backoff remains bounded by existing loop retry settings

Useful environment knobs (set before `si dyad spawn`):
- `DYAD_LOOP_ENABLED` (`1|0`)
- `DYAD_LOOP_MAX_TURNS` (`0` = infinite; default)
- `DYAD_LOOP_SLEEP_SECONDS`
- `DYAD_LOOP_STARTUP_DELAY_SECONDS`
- `DYAD_LOOP_TURN_TIMEOUT_SECONDS`
- `DYAD_LOOP_RETRY_MAX`
- `DYAD_LOOP_RETRY_BASE_SECONDS`
- `DYAD_LOOP_GOAL`
- `DYAD_LOOP_SEED_CRITIC_PROMPT` (bootstrap turn-0 critic instruction)
- `DYAD_LOOP_PROMPT_LINES` (prompt readiness detector window)
- `DYAD_LOOP_ALLOW_MCP_STARTUP` (`1|0`)
- `DYAD_LOOP_TMUX_CAPTURE` (`main|alt`)
- `DYAD_LOOP_PAUSE_POLL_SECONDS` (poll interval while paused via control file)

Preferred persistent config:
- Set dyad loop defaults in `~/.si/settings.toml` under `[dyad.loop]`.
- `si dyad spawn` loads these settings and forwards them to the critic loop.
- Environment variables above still work as one-off overrides.

Example:
```toml
[dyad.loop]
goal = "Continuously improve implementation quality in the current repo."
seed_critic_prompt = "Start with one concrete improvement task for the actor and require a work report."
max_turns = 0
sleep_seconds = 3
startup_delay_seconds = 2
turn_timeout_seconds = 900
retry_max = 3
retry_base_seconds = 2
prompt_lines = 3
tmux_capture = "main"
pause_poll_seconds = 5
```

Runtime control files (optional):
- `touch .si/dyad/<dyad>/control.pause` to pause the loop
- `rm .si/dyad/<dyad>/control.pause` to resume
- `touch .si/dyad/<dyad>/control.stop` to stop loop gracefully

Stop behavior:
- Loop runs continuously by default.
- Critic can stop the loop by emitting one of:
  - `Continue Loop: no`
  - `Stop Loop: yes`
  - `#STOP_LOOP`

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

## GitHub
`si` includes an App-auth GitHub bridge:

```bash
# auth/context
./si github auth status --account core
./si github context list
./si github context use --account core --owner Aureuma

# repositories
./si github repo list Aureuma
./si github repo get Aureuma/si
./si github repo create si-sandbox --owner Aureuma

# pull requests + issues
./si github pr list Aureuma/si
./si github pr get Aureuma/si 123
./si github issue list Aureuma/si
./si github issue create Aureuma/si --title "Bug report"

# workflows + releases
./si github workflow runs Aureuma/si
./si github workflow run Aureuma/si ci.yml --ref main
./si github release list Aureuma/si
./si github release create Aureuma/si --tag v0.44.0 --title "v0.44.0"

# secrets
./si github secret repo set Aureuma/si MY_SECRET --value "..."
./si github secret env set Aureuma/si sandbox MY_SECRET --value "..."
./si github secret org set Aureuma MY_SECRET --value "..." --visibility private

# raw escape hatches
./si github raw --method GET --path /repos/Aureuma/si
./si github graphql --query 'query { viewer { login } }'
```

Auth policy:
- GitHub App only.
- Configure app credentials via vault-compatible env keys (`GITHUB_<ACCOUNT>_APP_ID`, `GITHUB_<ACCOUNT>_APP_PRIVATE_KEY_PEM`, optional `GITHUB_<ACCOUNT>_INSTALLATION_ID`).

## Cloudflare
`si` includes a token-auth Cloudflare bridge with multi-account context:

```bash
# auth/context/diagnostics
./si cloudflare auth status --account core
./si cloudflare context list
./si cloudflare context use --account core --env prod --zone-id <zone_id>
./si cloudflare doctor --account core

# common resources
./si cloudflare zone list
./si cloudflare dns list --zone-id <zone_id>
./si cloudflare workers script list --account-id <account_id>
./si cloudflare pages project list --account-id <account_id>
./si cloudflare r2 bucket list --account-id <account_id>
./si cloudflare d1 query --account-id <account_id> --db <db_id> --sql "select 1"

# security/cache/tls
./si cloudflare cache purge --zone-id <zone_id> --everything --force
./si cloudflare tls get --zone-id <zone_id> --setting min_tls_version
./si cloudflare waf list --zone-id <zone_id>

# analytics/logging/reporting
./si cloudflare analytics http --zone-id <zone_id>
./si cloudflare logs job list --zone-id <zone_id>
./si cloudflare report traffic-summary --zone-id <zone_id>

# raw fallback
./si cloudflare raw --method GET --path /zones
```

Environment policy:
- Use `prod`, `staging`, and `dev` context labels.
- `test` is intentionally not used as a standalone environment mode.

## Google Places
`si` includes a Google Places API (New) bridge under `si google places`:

```bash
# auth/context/diagnostics
./si google places auth status --account core
./si google places context list
./si google places context current
./si google places context use --account core --env prod --language en --region US
./si google places doctor --account core

# session lifecycle
./si google places session new
./si google places session list
./si google places session inspect <token>
./si google places session end <token>

# search/details/photos
./si google places autocomplete --input "coffee" --session <token>
./si google places search-text --query "coffee near sf" --field-mask places.id,places.displayName,places.formattedAddress
./si google places search-nearby --center 37.7749,-122.4194 --radius 1200 --included-type cafe --field-mask places.id,places.displayName,places.formattedAddress
./si google places details ChIJN1t_tDeuEmsRUsoyG83frY4 --session <token> --field-mask id,name,displayName,formattedAddress
./si google places photo get places/<place_id>/photos/<photo_id>
./si google places photo download places/<place_id>/photos/<photo_id> --output ./photo.jpg

# helpers/reporting/raw
./si google places types list --group food
./si google places types validate cafe
./si google places report usage --since 2026-02-08T00:00:00Z
./si google places report sessions
./si google places raw --method GET --path /v1/places/<place_id> --field-mask id,name
```

Environment policy:
- Use `prod`, `staging`, and `dev` context labels.
- `test` is intentionally not used as a standalone environment mode.

## Google YouTube
`si` includes a YouTube Data API v3 bridge under `si google youtube`:

```bash
# auth/context/diagnostics
./si google youtube auth status --account core --mode api-key
./si google youtube auth login --account core --mode oauth
./si google youtube context list
./si google youtube context current
./si google youtube context use --account core --env prod --mode oauth
./si google youtube doctor --account core --mode oauth

# discovery/read
./si google youtube search list --query "engineering vlog" --type video
./si google youtube support languages
./si google youtube support regions
./si google youtube support categories --region US
./si google youtube channel mine --mode oauth
./si google youtube video list --id <video_id>
./si google youtube playlist list --mine --mode oauth

# mutations/assets/live (oauth)
./si google youtube video upload --mode oauth --file ./clip.mp4 --title "Demo" --privacy unlisted
./si google youtube video rate --mode oauth --id <video_id> --rating like
./si google youtube playlist-item add --mode oauth --playlist-id <playlist_id> --video-id <video_id>
./si google youtube caption upload --mode oauth --video-id <video_id> --file ./captions.vtt --language en
./si google youtube caption download --mode oauth --id <caption_id> --output ./captions.vtt
./si google youtube thumbnail set --mode oauth --video-id <video_id> --file ./thumb.jpg
./si google youtube live broadcast list --mode oauth
./si google youtube live stream list --mode oauth
./si google youtube live chat list --mode oauth --live-chat-id <chat_id>

# observability/raw
./si google youtube report usage --since 2026-02-08T00:00:00Z
./si google youtube report quota
./si google youtube raw --method GET --path /youtube/v3/search --param part=id --param q=music
```

Environment policy:
- Use `prod`, `staging`, and `dev` context labels.
- `test` is intentionally not used as a standalone environment mode.

## Self Build/Upgrade
Build or upgrade the `si` binary from the repo itself:

```bash
# installer (builds + installs to ~/.local/bin by default)
./tools/install-si.sh

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
