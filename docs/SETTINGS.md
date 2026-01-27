# Settings Reference (`~/.si/settings.toml`)

Silexa reads a single TOML file for user-facing configuration. The canonical path is:

```
~/.si/settings.toml
```

This file is created automatically on first use. It is also updated when you log in with `si login` so that profile metadata (auth path/timestamps) are tracked in one place.

## Precedence
When supported by a command, values resolve in this order:

1. CLI flags
2. `~/.si/settings.toml`
3. Environment variables
4. Built-in defaults

## Schema
The file is a standard TOML document. `schema_version` is reserved for future migrations.

### Top-level
- `schema_version` (int): settings schema version. Current value: `1`.

### `[paths]`
Reference paths for the local `.si` directory layout.
- `paths.root` (string): default `~/.si`
- `paths.settings` (string): default `~/.si/settings.toml`
- `paths.codex_profiles_dir` (string): default `~/.si/codex/profiles`

### `[codex]`
Defaults for Codex container commands (spawn/respawn/login/run).
- `codex.image` (string): docker image for `si codex spawn` (default: `silexa/silexa:local`)
- `codex.network` (string): docker network name
- `codex.workspace` (string): host path for workspace bind
- `codex.workdir` (string): container working directory
- `codex.repo` (string): default repo in `Org/Repo` form
- `codex.gh_pat` (string): optional PAT (stored in settings; keep file permissions restrictive)
- `codex.codex_volume` (string): override codex volume name
- `codex.gh_volume` (string): override GitHub config volume name
- `codex.docker_socket` (bool): mount host Docker socket into codex containers (default: `true`)
- `codex.profile` (string): default profile ID/email
- `codex.detach` (bool): default detach behavior
- `codex.clean_slate` (bool): default clean-slate behavior

#### `[codex.login]`
Defaults for `si login`.
- `codex.login.device_auth` (bool): default device auth flow (`true`/`false`)

#### `[codex.exec]`
Defaults for one-off `si run` (alias `si exec`).
- `codex.exec.model` (string): default model
- `codex.exec.effort` (string): default reasoning effort

#### `[codex.profiles]`
Profile metadata tracked in settings.
- `codex.profiles.active` (string): the last profile used for login

##### `[codex.profiles.entries.<id>]`
Per-profile entry keyed by profile ID (for example `america`). These entries are updated on successful `si login`.
- `name` (string): profile display name
- `email` (string): profile email
- `auth_path` (string): path to auth.json
- `auth_updated` (string): RFC3339 timestamp of auth.json

### `[dyad]`
Defaults for dyad spawns.
- `dyad.actor_image` (string): default `silexa/silexa:local`
- `dyad.critic_image` (string): default `silexa/silexa:local`
- `dyad.codex_model` (string)
- `dyad.codex_effort_actor` (string)
- `dyad.codex_effort_critic` (string)
- `dyad.codex_model_low` (string)
- `dyad.codex_model_medium` (string)
- `dyad.codex_model_high` (string)
- `dyad.codex_effort_low` (string)
- `dyad.codex_effort_medium` (string)
- `dyad.codex_effort_high` (string)
- `dyad.workspace` (string): host path for workspace bind
- `dyad.configs` (string): host path for configs
- `dyad.forward_ports` (string): port range, e.g. `1455-1465`
- `dyad.docker_socket` (bool): mount host Docker socket into dyad containers (default: `true`)

### `[shell.prompt]`
Prompt rendering for `si run` interactive shells. This applies without modifying `.bashrc`.
- `shell.prompt.enabled` (bool): enable/disable prompt customization
- `shell.prompt.git_enabled` (bool): include git branch when available
- `shell.prompt.prefix_template` (string): template for profile prefix. Use `{profile}` placeholder.
- `shell.prompt.format` (string): layout template. Supported placeholders: `{prefix}`, `{cwd}`, `{git}`, `{symbol}`
- `shell.prompt.symbol` (string): prompt symbol (e.g. `$` or `❯`)

#### `[shell.prompt.colors]`
Color tokens for prompt components. Supported values:
- Basic names: `black`, `red`, `green`, `yellow`, `blue`, `magenta`, `cyan`, `white`
- Bright variants: `bright-black`, `bright-red`, `bright-green`, `bright-yellow`, `bright-blue`, `bright-magenta`, `bright-cyan`, `bright-white`
- `reset`
- `ansi:<code>` where `<code>` is an ANSI numeric color code (e.g. `ansi:0;36`)
- `raw:<value>` to pass a raw escape sequence

Keys:
- `shell.prompt.colors.profile`
- `shell.prompt.colors.cwd`
- `shell.prompt.colors.git`
- `shell.prompt.colors.symbol`
- `shell.prompt.colors.error`
- `shell.prompt.colors.reset`

## Example
```toml
schema_version = 1

[paths]
root = "~/.si"
settings = "~/.si/settings.toml"
codex_profiles_dir = "~/.si/codex/profiles"

[codex]
image = "silexa/silexa:local"
network = "silexa"
workspace = "/home/ubuntu/Development/Silexa"
workdir = "/workspace"
docker_socket = true
profile = "america"
detach = true
clean_slate = false

[codex.login]
device_auth = true

[codex.exec]
model = "gpt-5.2-codex"
effort = "medium"

[codex.profiles]
active = "america"

[codex.profiles.entries.america]
name = "🗽 America"
email = "example@example.com"
auth_path = "~/.si/codex/profiles/america/auth.json"
auth_updated = "2026-01-26T00:00:00Z"

[dyad]
actor_image = "silexa/silexa:local"
critic_image = "silexa/silexa:local"
codex_model = "gpt-5.2-codex"
forward_ports = "1455-1465"
docker_socket = true
workspace = "/home/ubuntu/Development/Silexa"

[shell.prompt]
enabled = true
git_enabled = true
prefix_template = "[{profile}] "
format = "{prefix}{cwd}{git} {symbol} "
symbol = "$"

[shell.prompt.colors]
profile = "cyan"
cwd = "blue"
git = "magenta"
symbol = "green"
error = "red"
reset = "reset"
```
