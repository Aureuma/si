## MCP Exploration Unit (server scouting)

Purpose: continually scan MCP catalogs for servers that fit current tasks and notify dyads/management to adopt them.

### Workflow
- Discover: list catalogs and servers from the MCP Gateway (`bin/mcp-scout.sh`).
- Filter: pick servers relevant to active departments (web, infra, research, QA) and note required credentials.
- Recommend: post findings to manager `/feedback` with the server name, fit, and any secret requirements; ping Telegram if urgent.
- Adopt: infra or relevant dyad mounts catalog entries into `/data/mcp-gateway/catalog.yaml` (or adds via `docker-mcp catalog add`) and restarts gateway (`bin/mcp-gateway-up.sh`).

### Tooling
- `bin/mcp-scout.sh`: quick explorer to show catalogs and top entries from the default docker-mcp catalog. Intended for exploratory runs by the exploration unit.

### Guards
- Do not enable servers that require broad secrets without security approval.
- Note runtime impact (memory, CPU) if a server is heavy; apply Swarm limits when adding.
- Keep a short list of “approved” servers per department and refresh monthly.
