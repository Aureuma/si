## MCP Gateway integration

We bundle Docker's MCP Gateway so actors/critics can access MCP servers through a single endpoint.

### Service
- Swarm service `mcp-gateway` (image `silexa/mcp-gateway:local`) built from upstream repo.
- Runs `docker-mcp gateway run --transport streaming --host 0.0.0.0 --port 8088 --catalog /catalog/catalog.yaml`.
- Exposed on host `localhost:8088`.

### Catalog
- Default catalog is bootstrapped during image build (Docker MCP catalog). Location: `/catalog/catalog.yaml` inside the container; mount/replace to add custom servers.
- To inspect: `bin/mcp-scout.sh` (lists catalogs and shows the default catalog snippet).

### Usage from actors/critics
- Point MCP clients/SDKs to `http://mcp-gateway:8088` (inside Swarm network) or `http://localhost:8088` from host.
- Critics/actors can rely on the shared gateway to discover servers instead of running bespoke ones per container.
- Codex CLI: use `configs/codex-mcp-config.toml` and place it at `~/.config/codex/config.toml` inside the actor/critic container (helper: `bin/apply-codex-mcp-config.sh <container>`).

### Operations
- Build/start: `bin/mcp-gateway-up.sh`.
- Smoke test: `bin/test-mcp-gateway.sh` (builds image, lists catalogs, shows default catalog snippet).
- Resource limits: 0.5 CPU / 512 MiB (see `docker-stack.yml`).

### Extending
- Catalog now includes GitHub CLI and Stripe CLI POCI entries (`data/mcp-gateway/catalog.yaml`). Provide `secrets/gh_token` and `secrets/stripe_api_key` and run `bin/swarm-secrets.sh`.
- Add further catalogs by mounting a catalog file into the service (edit `docker-stack.yml`). Use `docker-mcp catalog bootstrap` or `catalog add` to bring in servers with appropriate secrets mounted via docker secrets.
- Keep credentials out of env when possible; prefer secret files and minimal scopes per service.
