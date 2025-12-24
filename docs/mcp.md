## MCP Gateway integration

We bundle Docker's MCP Gateway so actors/critics can access MCP servers through a single endpoint.

### Service
- Compose service `mcp-gateway` (image `silexa/mcp-gateway:local`) built from upstream repo.
- Runs `docker-mcp gateway run --transport streaming --host 0.0.0.0 --port 8088 --catalog /catalog/catalog.yaml`.
- Exposed on host `localhost:8088`.

### Catalog
- Default catalog is bootstrapped during image build (Docker MCP catalog). Location: `/catalog/catalog.yaml` inside the container; mount/replace to add custom servers.
- To inspect: `docker compose run --rm mcp-gateway catalog ls` or `catalog show docker-mcp --format yaml`.

### Usage from actors/critics
- Point MCP clients/SDKs to `http://mcp-gateway:8088` (inside compose network) or `http://localhost:8088` from host.
- Critics/actors can rely on the shared gateway to discover servers instead of running bespoke ones per container.
- Codex CLI: use `configs/codex-mcp-config.toml` and place it at `~/.config/codex/config.toml` inside the actor/critic container (helper: `bin/apply-codex-mcp-config.sh <container>`).

### Operations
- Build/start: `bin/mcp-gateway-up.sh`.
- Smoke test: `bin/test-mcp-gateway.sh` (builds image, lists catalogs, shows default catalog snippet).
- Resource limits: 0.5 CPU / 512 MiB (see `docker-compose.yml`).

### Extending
- Catalog now includes GitHub CLI and Stripe CLI POCI entries (`data/mcp-gateway/catalog.yaml`). Provide `GH_TOKEN` and `STRIPE_API_KEY` in the environment (see `docker-compose.yml` for pass-through) or mount secrets before use.
- Add further catalogs by mounting a catalog file into the service (edit compose). Use `docker-mcp catalog bootstrap` or `catalog add` to bring in servers with appropriate secrets mounted via docker secrets.
- Keep credentials out of env when possible; prefer secret files and minimal scopes per service.
