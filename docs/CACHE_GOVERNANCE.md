---
title: Cache Governance
description: Workspace cache and storage control without sacrificing Rust and Docker build performance.
---

# Cache Governance

Use `tools/si-cache-governor` to keep workspace storage under control while protecting warm build paths.

## Why this exists

- preserve fast Rust incremental builds (`.artifacts/cargo-target`, `sccache`)
- keep Docker build cache useful while removing stale entries
- avoid destructive cleanup of active runtime volumes

## Audit first

Always start with a read-only snapshot:

```bash
tools/si-cache-governor audit
```

The report includes:

- per-repo total size
- per-repo `.artifacts`, `target`, and `node_modules` footprint
- dirty repo count signal
- global cache sizes (`sccache`, Cargo registry/git, npm/pnpm)
- Docker aggregate cache/image/volume usage

## Safe prune order

1. Preview local prune candidates:

```bash
tools/si-cache-governor prune
```

2. Apply local prune candidates:

```bash
tools/si-cache-governor prune --apply
```

3. Preview + apply Docker cache/image pruning:

```bash
tools/si-cache-governor prune --include-docker
tools/si-cache-governor prune --include-docker --apply
```

## Retention defaults

- `si` repo `.artifacts/cargo-target`: prune only when older than `14` days
- other repos `.artifacts/cargo-target`: prune only when older than `30` days
- node modules pruning is opt-in with `--include-node-modules`

## Guardrails

- dry-run is the default for prune
- underscore-prefixed top-level directories are always skipped
- Docker volume pruning is intentionally excluded from automatic cleanup
- `sccache` is audited but never auto-cleared

## Optional tuning

- set alternate root:

```bash
tools/si-cache-governor audit --workspace-root /path/to/workspace
```

- tune Docker age thresholds:

```bash
tools/si-cache-governor prune --include-docker --docker-buildx-max-age-days 21 --docker-image-max-age-days 45
```
