# Tests

This folder houses the test harness for Silexa. Scripts are grouped by scope and use existing tools (bash + curl, Go test, Playwright) instead of custom frameworks.

## Layout
- smoke/: fast checks for stack health and core services
- integration/: API-level checks for dyad workflows, roster parsing, and app management
- go/: module unit tests
- visual/: Playwright-based visual regression checks
- run.sh: scope runner (smoke/go/integration/visual)

## Usage
- Default suite (smoke + go + integration): `tests/run.sh`
- Specific scope: `tests/run.sh --scope smoke`
- Visual tests: `tests/run.sh --scope visual --visual-app <app>`

Legacy entrypoints remain in `bin/` (wrappers) for compatibility:
- `bin/qa-smoke.sh`
- `bin/qa-visual.sh`
- `bin/test-mcp-gateway.sh`
