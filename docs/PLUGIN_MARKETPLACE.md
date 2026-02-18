# Plugin Marketplace and Integration Plan

This document defines SI's plugin marketplace model and the implementation now available in `si plugins ...`.

## Goals

- Provide a fast, operator-friendly path to add integrations without modifying SI core code for every new ecosystem.
- Enforce namespaced plugin identity and safety checks before installation.
- Keep a catalog/marketplace model that can evolve from local-first to hosted registries.
- Support MCP-focused integrations (HTTP/SSE/stdio), provider metadata, and command hints.

## OpenClaw Lessons Applied

The SI design intentionally mirrors proven OpenClaw patterns:

1. Manifest-first validation.
- Like OpenClaw's `openclaw.plugin.json`, SI requires `si.plugin.json` and validates metadata before install/use.

2. Discovery precedence.
- SI merges catalogs with clear precedence: built-in catalog, then `~/.si/plugins/catalog.json`, then `~/.si/plugins/catalog.d/*.json`.

3. Safe installation boundaries.
- SI uses safe install path resolution to prevent traversal escapes from `~/.si/plugins/installed`.
- Plugin source trees are copied without following symlinks.

4. Lifecycle + diagnostics UX.
- SI exposes list/info/install/uninstall/enable/disable/doctor/register/scaffold workflows.
- `si plugins doctor` surfaces catalog and install-state problems in machine-readable JSON and human text.

5. Explicit enable-state.
- Install records track `enabled` separately from catalog metadata so operators can stage plugins safely.

## Manifest Contract (`si.plugin.json`)

Minimum required shape:

```json
{
  "schema_version": 1,
  "id": "acme/release-mind",
  "namespace": "acme",
  "install": { "type": "none" }
}
```

Important rules:

- `id` must be namespaced as `<namespace>/<name>` using lowercase segments.
- `namespace` must match `id` prefix.
- `install.type` allowed values:
  - `none`
  - `local_path`
  - `mcp_http`
  - `oci_image`
  - `git`
- Optional legal/compliance metadata:
  - `terms_url`
  - `privacy_url`
  - `license`
- Optional integration metadata:
  - `integration.provider_ids`
  - `integration.commands`
  - `integration.mcp_servers`
  - `integration.capabilities`

## Marketplace Sources

SI now loads catalog entries from:

1. Embedded built-in catalog (`si/browser-mcp` seeded as core).
2. User catalog file: `~/.si/plugins/catalog.json`.
3. User catalog directory: `~/.si/plugins/catalog.d/*.json`.
4. Optional env overrides via `SI_PLUGIN_CATALOG_PATHS` (comma/semicolon/path-list separated file/dir paths).

User sources override lower-precedence entries for the same plugin id.

## Runtime State and Files

- Root: `~/.si/plugins`
- Install root: `~/.si/plugins/installed`
- Install state: `~/.si/plugins/state.json`
- User catalog: `~/.si/plugins/catalog.json`

Install state tracks:

- plugin id
- enabled flag
- source (`catalog:<id>` or `path:<absolute-path>`)
- install directory (when copied locally)
- timestamp
- normalized manifest snapshot

## CLI Workflows

- `si plugins list [--installed] [--json]`
- `si plugins info <id> [--json]`
- `si plugins install <id-or-path> [--disabled] [--json]`
- `si plugins uninstall <id> [--keep-files] [--json]`
- `si plugins enable|disable <id> [--json]`
- `si plugins policy show [--json]`
- `si plugins policy set [--enabled <true|false>] [--allow <id>]... [--deny <id>]... [--clear-allow] [--clear-deny] [--json]`
- `si plugins doctor [--json]`
- `si plugins register [--manifest <path>|<path>] [--channel <name>] [--verified] [--json]`
- `si plugins scaffold <namespace/name> [--dir <path>] [--force] [--json]`

## Quick Integration Onboarding Flow

1. Scaffold plugin metadata:

```bash
si plugins scaffold acme/release-mind --dir ./integrations
```

2. Fill manifest details (`terms_url`, `privacy_url`, MCP/provider metadata).

3. Register into local marketplace catalog:

```bash
si plugins register ./integrations/acme/release-mind --channel community
```

4. Install and stage:

```bash
si plugins install acme/release-mind --disabled
si plugins enable acme/release-mind
```

5. Validate:

```bash
si plugins doctor --json
```

## Security Baseline

- Strict namespaced IDs to avoid collisions.
- Safe install-dir resolution to prevent path escape.
- Symlink copy rejection for local installs.
- Doctor checks for manifest mismatch, missing files, and unsafe install paths.

## Future Work

- Signed catalog bundles and trust policy.
- Remote package fetch and verification pipeline.
- Policy controls (allow/deny lists and slot ownership) similar to OpenClaw's advanced plugin config.
- Optional compatibility contracts for SI command/runtime versions.
