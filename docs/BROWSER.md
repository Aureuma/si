# Surf Runtime (`si surf`)

![Surf runtime](/docs/images/integrations/browser.svg)

`si surf` manages the Dockerized Playwright MCP runtime used by SI agents.

Related:
- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [Plugin Marketplace](./PLUGIN_MARKETPLACE)

## Commands

```bash
si surf build
si surf start
si surf status
si surf logs
si surf stop
si surf proxy
```

## Quickstart

```bash
si surf build
si surf start
si surf status
```

Default endpoints after start:

- MCP endpoint: `http://127.0.0.1:8932/mcp`
- noVNC endpoint: `http://127.0.0.1:6080/vnc.html?autoconnect=1&resize=scale`
- Internal Docker endpoint for SI containers: `http://si-playwright-mcp-headed:8931/mcp`

## Proxy mode (optional)

Use proxy mode for legacy `/mcp` SSE clients:

```bash
si surf proxy --upstream http://127.0.0.1:8932
```

## Operational notes

- `si surf start` can build the image automatically unless `--skip-build` is set.
- `si surf start` attaches the browser runtime to the SI Docker network (`si` by default, override with `--network` / `SI_BROWSER_NETWORK`).
- SI-managed codex/dyad containers auto-register MCP server `si_browser` to the browser MCP endpoint.
- Keep profile directories isolated per environment.
- Use `si surf logs --follow` during smoke tests and rollout checks.
