# Orbitals and Integration Plan

![Orbitals](/docs/images/integrations/orbits.svg)

This document defines SI's Orbitals model and the implementation now available in `si orbits ...`.

Related:
- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [Integration Gateway Architecture](./INTEGRATION_GATEWAY_ARCHITECTURE)
- [Documentation Style Guide](./DOCS_STYLE_GUIDE)

## Goals

- Provide a fast, operator-friendly path to add integrations without modifying SI core code for every new ecosystem.
- Enforce namespaced orbit identity and safety checks before installation.
- Keep a orbitals model that can evolve from local-first to hosted registries.
- Support MCP-focused integrations (HTTP/SSE/stdio), provider metadata, and command hints.

## OpenClaw Lessons Applied

The SI design intentionally mirrors proven OpenClaw patterns:

1. Manifest-first validation.
- Like OpenClaw's `openclaw.orbit.json`, SI requires `si.orbit.json` and validates metadata before install/use.

2. Discovery precedence.
- SI merges catalogs with clear precedence: built-in catalog, then `~/.si/orbits/catalog.json`, then `~/.si/orbits/catalog.d/*.json`.

3. Safe installation boundaries.
- SI uses safe install path resolution to prevent traversal escapes from `~/.si/orbits/installed`.
- Orbit source trees are copied without following symlinks.

4. Lifecycle + diagnostics UX.
- SI exposes list/info/install/uninstall/enable/disable/doctor/register/scaffold workflows.
- `si orbits doctor` surfaces catalog and install-state problems in machine-readable JSON and human text.

5. Explicit enable-state.
- Install records track `enabled` separately from catalog metadata so operators can stage orbits safely.

## Manifest Contract (`si.orbit.json`)

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
  - `url_archive`
  - `mcp_http`
  - `oci_image`
  - `git`
- For `install.type=url_archive`, set:
  - `install.source` to an `https://` or `http://` archive URL (`.zip`, `.tgz`, `.tar.gz`, `.tar`)
  - `install.params.sha256` to pin expected artifact checksum (recommended)
- Optional legal/compliance metadata:
  - `terms_url`
  - `privacy_url`
  - `license`
- Optional integration metadata:
  - `integration.provider_ids`
  - `integration.commands`
  - `integration.mcp_servers`
  - `integration.capabilities`

## Orbitals Sources

SI now loads catalog entries from:

1. Embedded built-in catalog (core + curated OpenClaw parity + SaaS foundation integrations).
2. User catalog file: `~/.si/orbits/catalog.json`.
3. User catalog directory: `~/.si/orbits/catalog.d/*.json`.
4. Optional env overrides via `SI_ORBIT_CATALOG_PATHS` (comma/semicolon/path-list separated file/dir paths).

User sources override lower-precedence entries for the same orbit id.

## Sharded Gateway for Large Catalogs

SI now includes a sharded integration gateway model for high-cardinality catalogs (thousands to tens of thousands of integrations).

Design:
- Namespace partitioning: each orbit id (`namespace/name`) maps to a namespace partition.
- Slot sharding: each namespace is further split into deterministic slots (`namespace--NN`) using stable hashing.
- Index-first reads: a compact registry index tracks shards, counts, and capabilities so clients fetch only relevant shards.

Sun storage model:
- Registry index object kind: `integration_gateway_index`
- Registry shard object kind: `integration_gateway_shard`

Why this shape:
- Avoids monolithic catalog payloads.
- Supports targeted fetch by namespace/capability/prefix.
- Keeps orbit resolution deterministic and cache-friendly.

## Runtime State and Files

- Root: `~/.si/orbits`
- Install root: `~/.si/orbits/installed`
- Install state: `~/.si/orbits/state.json`
- User catalog: `~/.si/orbits/catalog.json`

Install state tracks:

- orbit id
- enabled flag
- source (`catalog:<id>` or `path:<absolute-path>`)
- install directory (when copied locally)
- timestamp
- normalized manifest snapshot

## CLI Workflows

- `si orbits list [--installed] [--json]`
- `si orbits info <id> [--json]`
- `si orbits install <id-or-path> [--disabled] [--json]`
- `si orbits update <id>|--all [--json]`
- `si orbits uninstall <id> [--keep-files] [--json]`
- `si orbits enable|disable <id> [--json]`
- `si orbits policy show [--json]`
- `si orbits policy set [--enabled <true|false>] [--allow <id>]... [--deny <id>]... [--clear-allow] [--clear-deny] [--json]`
- `si orbits doctor [--json]`
- `si orbits register [--manifest <path>|<path>] [--channel <name>] [--verified] [--json]`
- `si orbits scaffold <namespace/name> [--dir <path>] [--force] [--json]`
- `si orbits catalog build --source <path> [--output <path>] [--channel <name>] [--verified] [--tag <value>]... [--added-at YYYY-MM-DD] [--json]`
- `si orbits catalog validate --source <path> [--json]`
- `si orbits gateway build --source <path> [--registry <name>] [--slots <n>] [--output-dir <dir>] [--json]`
- `si orbits gateway push --source <path> [--registry <name>] [--slots <n>] [--channel <name>] [--verified] [--json]`
- `si orbits gateway pull [--registry <name>] [--namespace <ns>] [--capability <cap>] [--prefix <prefix>] [--limit <n>] [--out <file>] [--json]`
- `si orbits gateway status [--registry <name>] [--json]`

Catalog metadata now includes source provenance:
- `catalog_source` in `si orbits list --json`
- `catalog_source` in `si orbits info --json`
- override diagnostics report both new and previous source when ids collide

## External Catalog Packs

SI still supports external catalog packs for private or team-specific integrations.

Example:

```bash
SI_ORBIT_CATALOG_PATHS="/path/to/team-catalog.json" si orbits list --json
```

External catalogs overlay built-ins and can override matching ids.

## Quick Integration Onboarding Flow

1. Scaffold orbit metadata:

```bash
si orbits scaffold acme/release-mind --dir ./integrations
```

2. Fill manifest details (`terms_url`, `privacy_url`, MCP/provider metadata).

3. Register into local Orbitals catalog:

```bash
si orbits register ./integrations/acme/release-mind --channel community
```

4. Install and stage:

```bash
si orbits install acme/release-mind --disabled
si orbits enable acme/release-mind
```

Local archive install is also supported:

```bash
si orbits install ./dist/release-mind-orbit.zip
si orbits install ./dist/release-mind-orbit.tgz
```

Catalog-driven remote archive install is also supported when an orbit entry uses `install.type=url_archive`:

```json
{
  "id": "aureuma/remote-control",
  "install": {
    "type": "url_archive",
    "source": "https://github.com/Aureuma/remote-control/releases/download/v0.1.0/remote-control_linux_amd64.tar.gz",
    "params": {
      "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    }
  }
}
```

5. Validate:

```bash
si orbits doctor --json
```

## Security Baseline

- Strict namespaced IDs to avoid collisions.
- Safe install-dir resolution to prevent path escape.
- Symlink copy rejection for local installs.
- Doctor checks for manifest mismatch, missing files, and unsafe install paths.

## Future Work

- Signed catalog bundles and trust policy.
- Remote package fetch and verification pipeline.
- Policy controls (allow/deny lists and slot ownership) similar to OpenClaw's advanced orbit config.
- Optional compatibility contracts for SI command/runtime versions.
