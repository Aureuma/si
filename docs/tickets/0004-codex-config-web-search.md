# Ticket 0004: Codex Config Deprecation Cleanup (web_search_request -> web_search)

## Problem

Codex emits a warning that `[features].web_search_request` is deprecated and should be replaced by `web_search` at the top level (or under a profile).

This warning appears in dyad tmux sessions and makes output noisier than it needs to be.

## Acceptance Criteria

- `tools/codex-init` generated `config.toml` no longer includes `[features].web_search_request`.
- Generated config uses the replacement top-level setting:
  - `web_search = "live"` (to preserve the previous enabled behavior).
- `si run --no-mcp` (one-off exec config) disables web search via:
  - `web_search = "disabled"`
- Update any tests that asserted the deprecated key.
- Add tests for `tools/codex-init` config generation so regressions are caught.

## Implementation Notes

- Keep the rest of the generated config stable (model, reasoning effort, `[sandbox_workspace_write]`, `[si]` metadata).
- Keep diffs minimal and focused.
- Run Go tests for affected modules using Docker (host may not have Go installed).

## Definition of Done

- Tests pass.
- Commit created with a clear subject and body explaining the behavior change.

