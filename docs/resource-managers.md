## Third-party resource managers (low-level building blocks)

Goal: each external service gets a small, composable layer with credentials, API/CLI wiring, and queueing so dyads can operate semi-autonomously.

### Credentials
- Store tokens/keys as docker secrets and mount only into the resource broker (or dedicated service). Environment variables are allowed for short-lived dev.
- Supported keys (expected by resource-broker):
  - GitHub: `GITHUB_TOKEN_FILE` (fine-grained PAT) or `GITHUB_TOKEN`
  - Stripe: `STRIPE_API_KEY_FILE` or `STRIPE_API_KEY` (restricted key)
  - Telegram: `TELEGRAM_BOT_TOKEN_FILE` or `TELEGRAM_BOT_TOKEN`
- Add secrets under `secrets/` and wire them into the broker service when ready.

### Capabilities probe
- `resource-broker` now exposes `/capabilities` showing which services are credential-ready:
  - name, credential, credential_ok, interface (rest/cli), notes.
- Example: `curl -s http://localhost:9091/capabilities | jq`.

### Request flow (semi-autonomous)
1) Actor/critic submits a resource request: `bin/request-resource.sh github repo-create '{"name":"myapp","private":true}'`.
2) Resource broker queues it (visible via `/requests`); when a credential is present and executor is enabled, it can fulfill automatically, otherwise a human/security dyad approves.
3) Result/decision is posted to Telegram and manager feedback.

### Implementation notes
- Execution of API calls is intentionally gated; attach small executors per service (GitHub/Stripe) that read the same creds and act on queued items. Keep scopes minimal.
- For CLI-based services, wrap the CLI in a thin handler invoked by the broker to process requests.
- All actions should log payload, requester, and resolution for audit; store only non-sensitive payloads in the queue.

### Next wiring steps
- Mount secrets into `resource-broker` and extend it with service-specific executors.
- Define allowed actions per service (e.g., GitHub: repo-create, add-collaborator; Stripe: create-product/price).
- Add RBAC checks (requester/department) before executing; use manager `/access-requests` for higher-risk operations.
