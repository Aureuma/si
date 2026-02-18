# si browser Docker Assets

This directory contains the Docker runtime assets used by `si browser`.

Primary CLI flows:

```bash
si browser build
si browser start
si browser status
si browser logs
si browser stop
```

Optional MCP path proxy (for legacy `/mcp` SSE clients):

```bash
si browser proxy --upstream http://127.0.0.1:8932
```

Default local endpoints after `si browser start`:

- MCP: `http://127.0.0.1:8932/mcp`
- noVNC: `http://127.0.0.1:6080/vnc.html?autoconnect=1&resize=scale`
