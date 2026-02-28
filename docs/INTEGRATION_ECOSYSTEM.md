# Integration Ecosystem

SI supports a two-layer integration model:

1. Core integrations in the SI codebase.
2. External orbit-pack catalogs maintained independently and loaded at runtime.

## Why This Model

- Faster integration iteration without waiting for SI core releases.
- Namespace ownership (`openclaw/*`, `saas/*`, etc.) to avoid ID collisions.
- Stable operator workflow using catalog artifacts (`catalog/*.json`) and `SI_ORBIT_CATALOG_PATHS`.

## External Orbit-Pack Repository

A sibling repository (`../si-integrations`) now contains:

- OpenClaw parity integrations (all current OpenClaw extension orbit IDs).
 - OpenClaw parity integrations (all current OpenClaw integration orbit IDs).
- SaaS-first integrations for startup/company workflows.
- Deterministic catalog generation scripts and validation scripts.

## Operational Workflow

1. Add or update orbit manifests in the external pack.
2. Build catalogs (`npm run build:catalogs`).
3. Validate catalogs (`npm test`).
4. Load into SI:

```bash
SI_ORBIT_CATALOG_PATHS="/absolute/path/to/si-integrations/catalog/all.json" si orbits list --json
```

## SI Catalog Tooling

SI now provides native catalog build/validate workflows for manifest trees:

```bash
si orbits catalog build --source ./orbits --output ./catalog/all.json --channel ecosystem --tag integrations
si orbits catalog validate --source ./orbits --json
```

These commands make large-scale integration packs maintainable and testable with a single interface.

## Recommended Governance

- Keep each orbit namespaced and owner-scoped.
- Use `install.type=none` for metadata-only catalog entries.
- Attach channel/tags metadata in generated catalogs for discoverability.
- Add policy controls in SI runtime (`si orbits policy`) before enabling at scale.
