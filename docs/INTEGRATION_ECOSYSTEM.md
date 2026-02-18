# Integration Ecosystem

SI supports a two-layer integration model:

1. Core integrations in the SI codebase.
2. External plugin-pack catalogs maintained independently and loaded at runtime.

## Why This Model

- Faster integration iteration without waiting for SI core releases.
- Namespace ownership (`openclaw/*`, `saas/*`, etc.) to avoid ID collisions.
- Stable operator workflow using catalog artifacts (`catalog/*.json`) and `SI_PLUGIN_CATALOG_PATHS`.

## External Plugin-Pack Repository

A sibling repository (`../si-integrations`) now contains:

- OpenClaw parity integrations (all current OpenClaw extension plugin IDs).
- SaaS-first integrations for startup/company workflows.
- Deterministic catalog generation scripts and validation scripts.

## Operational Workflow

1. Add or update plugin manifests in the external pack.
2. Build catalogs (`npm run build:catalogs`).
3. Validate catalogs (`npm test`).
4. Load into SI:

```bash
SI_PLUGIN_CATALOG_PATHS="/absolute/path/to/si-integrations/catalog/all.json" si plugins list --json
```

## SI Catalog Tooling

SI now provides native catalog build/validate workflows for manifest trees:

```bash
si plugins catalog build --source ./plugins --output ./catalog/all.json --channel ecosystem --tag integrations
si plugins catalog validate --source ./plugins --json
```

These commands make large-scale integration packs maintainable and testable with a single interface.

## Recommended Governance

- Keep each plugin namespaced and owner-scoped.
- Use `install.type=none` for metadata-only catalog entries.
- Attach channel/tags metadata in generated catalogs for discoverability.
- Add policy controls in SI runtime (`si plugins policy`) before enabling at scale.
