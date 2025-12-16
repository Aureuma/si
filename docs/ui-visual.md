## Visual QA workflow (disciplined & low-cost)

We use a generic Playwright harness in Docker to take screenshots, compare to baselines, and surface actionable diffs to dyads.

### Per-app contract
Each web app should add `apps/<app>/ui-tests/targets.json`:

```json
{
  "baseURL": "http://localhost:3000",
  "routes": [
    { "path": "/", "name": "home", "waitFor": "header" },
    { "path": "/login", "name": "login", "waitFor": "#login-form" }
  ],
  "viewports": [
    { "width": 1280, "height": 720, "name": "desktop" },
    { "width": 375, "height": 667, "name": "mobile" }
  ]
}
```

Notes:
- `baseURL` should point to a running instance (local dev server or preview URL).
- Optional `waitMs` or `waitFor` per route to let UI settle.
- Defaults: two viewports (desktop/mobile) and a pixel threshold of 100.

### Running tests

```bash
# From repo root
bin/qa-visual.sh myapp           # generates baseline on first run
bin/qa-visual.sh myapp --notify  # posts summary via TELEGRAM_NOTIFY_URL
```

Artifacts (per app):
- `.artifacts/visual/baseline`: canonical screenshots.
- `.artifacts/visual/current`: latest run.
- `.artifacts/visual/diff`: pixel diffs when mismatches occur.

Set `PIXEL_THRESHOLD=0` for stricter checks, or higher if intentional noise exists.

### Notifications & feedback
- If `--notify` is used and `TELEGRAM_NOTIFY_URL` is set, a summary goes to Telegram (optionally with `TELEGRAM_CHAT_ID`).
- Critics should post failures to manager `/feedback` with pointers to the diff directory and `/metrics` with `visual_regressions` counts.
- Designer/developer dyads review `.artifacts/visual/diff/*.png` to see exactly what changed.

### Approval & baseline updates
- First run creates baselines.
- Intentional UI changes: rerun `bin/qa-visual.sh myapp` and commit updated baselines (if the app repo tracks them). Otherwise store baselines in build artifacts.
- Keep the route list small and focused on high-value flows to stay fast and cheap.

### Discipline points
- Run visual QA on every main-branch change and before releases.
- Keep test targets up to date with new pages/components.
- Enforce failures on unexpected diffs; require explicit approvals to update baselines.
-
