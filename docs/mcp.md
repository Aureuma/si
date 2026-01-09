## MCP Gateway integration

We bundle Docker's MCP Gateway so actors/critics can access MCP servers through a single endpoint.

### Service
- Docker container `silexa-mcp-gateway` (image `silexa/mcp-gateway:local`) built from the upstream repo.
- Runs `docker-mcp gateway run --transport streaming --host 0.0.0.0 --port 8088 --catalog /catalog/catalog.yaml`.
- Internal access via `http://silexa-mcp-gateway:8088` on the Docker network.

### Catalog
- Default catalog is bootstrapped during image build (Docker MCP catalog). Location: `/catalog/catalog.yaml` inside the container; mount/replace to add custom servers.
- To inspect: `silexa mcp scout` (lists catalogs and shows the default catalog snippet).
- Local catalog override lives at `data/mcp-gateway/catalog.yaml`; sync it into the gateway volume when updated.

### Usage from actors/critics
- Point MCP clients/SDKs to `http://silexa-mcp-gateway:8088` (inside the cluster) or port-forward from host.
- Critics/actors can rely on the shared gateway to discover servers instead of running bespoke ones per container.
- Codex CLI: use `configs/codex-mcp-config.toml` and place it at `~/.codex/config.toml` inside the actor/critic container (helper: `silexa mcp apply-config <dyad>`).

### Operations
- Build/start: `silexa stack up`.
- Smoke test: `silexa mcp scout`.

### Extending
- Catalog includes GitHub CLI and Stripe CLI POCI entries (`data/mcp-gateway/catalog.yaml`). Provide `secrets/gh_token` and `secrets/stripe_api_key` on the host; the stack mounts them into `/run/secrets`.
- Add further catalogs by editing `data/mcp-gateway/catalog.yaml` and running `silexa mcp sync`.
- Keep credentials out of env when possible; prefer secret files and minimal scopes per service.

### Credentials broker
- `silexa-credentials` MCP server is registered as a remote server in `data/mcp-gateway/catalog.yaml`.
- Workflow: request access via `credentials.request_secret`, review/approve via `silexa-credentials`, then decrypt via `credentials.reveal_secret`.
