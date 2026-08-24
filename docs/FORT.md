# `si fort`

`si fort` wraps the native `fort` binary (github.com/Aureuma/fort) and keeps
runtime auth file-based. The dotenv encryption tool it brokers now lives in its
own repo: github.com/Aureuma/vault.

## Wrapper Contract

- `si fort` wraps the native `fort` binary and keeps runtime auth file-based.
- `si codex spawn ...` and `si codex shell ...` provision or reuse a slot-scoped Fort session under `CODEX_HOME/fort/` (`~/.si/codex/profiles/<profile>/fort/` for `primary`, `~/.si/codex/profiles/<profile>/workers/<slot>/fort/` for non-primary slots).
- Host bootstrap/admin auth for provisioning uses the bootstrap token files at `~/.si/fort/bootstrap/admin.token` and `~/.si/fort/bootstrap/admin.refresh.token`.
- Runtime worker sessions use profile-local file-backed token paths for the short-lived access token and rotating refresh token.
- Wrapper behavior:
  - prefers the Fort session under `CODEX_HOME/fort/` when `CODEX_HOME` is set by a managed Codex profile runtime
  - does not accept caller-supplied `FORT_TOKEN_PATH` / `FORT_REFRESH_TOKEN_PATH` as a normal `si fort` runtime fallback
  - does not fall back to the active Codex profile outside a managed `si codex spawn` / `si codex shell` runtime
  - fails loudly for runtime secret commands (`get`, `set`, `list`, `batch-get`, `run`) when no usable runtime session exists or runtime refresh fails
  - uses bootstrap/admin auth only for explicit provisioning and admin commands (`agent ...`, `auth issue|login|list|revoke`, `auth session open`)
  - runtime refresh is owned by the profile-scoped Fort refresher
  - refuses to rotate a Codex profile `refresh.token` into a different output path; profile refresh tokens must be refreshed in place
  - passes explicit token-file auth to native `fort` when default files are available (no bearer token argv injection)
  - rejects deprecated token-value env vars (`FORT_TOKEN`, `FORT_REFRESH_TOKEN`)
  - strips legacy token env entries from child process env if present
- Operational guidance:
  - keep `~/.si/fort/bootstrap/*` for break-glass recovery only
  - keep routine Fort access in `~/.si/codex/profiles/<profile>/fort/access.token` and `refresh.token`
  - invoke routine Fort commands through `si codex shell --profile <profile> --slot <slot> -- si fort ...` when outside that profile runtime
  - Codex profile provisioning explicitly requests a `30d` refresh-session TTL; Fort's general default may be shorter for non-Codex sessions
  - Fort runtime agent IDs are slot-aware: `si-codex-<profile>` for `primary` and `si-codex-<profile>--<slot>` for non-primary slots
- For flags that belong to native `fort` global options, pass through after `--`:
  - `si fort -- --host https://fort.aureuma.ai doctor`

## Core Model

