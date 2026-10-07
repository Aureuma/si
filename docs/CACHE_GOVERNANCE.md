---
title: Cache Governance
description: Workspace cache and storage control without sacrificing Rust build performance.
---

# Cache Governance

SI no longer ships a cache-governor tool. Apply these retention rules manually
when disk pressure requires cleanup.

## Retention defaults

- `si` repo `.artifacts/cargo-target`: prune only when older than `14` days
- other repos `.artifacts/cargo-target`: prune only when older than `30` days
- clear repo `target` directories before clearing `sccache` so cross-repo Rust
  rebuilds stay fast

## Guardrails

- preview before deleting; never auto-clear `sccache`
- never prune Docker volumes automatically
- skip underscore-prefixed top-level directories
