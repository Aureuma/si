## MCP Gateway integration

We bundle Docker's MCP Gateway so actors/critics can access MCP servers through a single endpoint.

### Service
- Kubernetes Deployment `silexa-mcp-gateway` (image `silexa/mcp-gateway:local`) built from the upstream repo.
- Runs `docker-mcp gateway run --transport streaming --host 0.0.0.0 --port 8088 --catalog /catalog/catalog.yaml`.
- Cluster access via `http://silexa-mcp-gateway:8088`.

### Catalog
- Default catalog is bootstrapped during image build (Docker MCP catalog). Location: `/catalog/catalog.yaml` inside the container; mount/replace to add custom servers.
- To inspect: `bin/mcp-scout.sh` (lists catalogs and shows the default catalog snippet).

### Usage from actors/critics
- Point MCP clients/SDKs to `http://silexa-mcp-gateway:8088` (inside the cluster) or port-forward from host.
- Critics/actors can rely on the shared gateway to discover servers instead of running bespoke ones per container.
- Codex CLI: use `configs/codex-mcp-config.toml` and place it at `~/.config/codex/config.toml` inside the actor/critic container (helper: `bin/apply-codex-mcp-config.sh <dyad>`).

### Operations
- Build/start: `bin/mcp-gateway-up.sh`.
- Smoke test: `bin/test-mcp-gateway.sh` (builds image, lists catalogs, shows default catalog snippet).

### Extending
- Catalog includes GitHub CLI and Stripe CLI POCI entries (`data/mcp-gateway/catalog.yaml`). Provide `secrets/gh_token` and `secrets/stripe_api_key`, then create `mcp-gateway-secrets` in Kubernetes:
  `kubectl -n silexa create secret generic mcp-gateway-secrets --from-file=gh_token=secrets/gh_token --from-file=stripe_api_key=secrets/stripe_api_key`
- Add further catalogs by mounting a catalog file into the service (edit `infra/k8s/silexa/mcp-gateway.yaml`).
- Keep credentials out of env when possible; prefer secret files and minimal scopes per service.
