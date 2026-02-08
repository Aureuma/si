# Ticket: `si github` Full GitHub Integration (Vault-Compatible, App-First)

Date: 2026-02-08  
Owner: Unassigned  
Primary Goal: Add `si github ...` as a first-class command family for broad GitHub control, monitoring, and automation using GitHub REST/GraphQL APIs with credentials sourced from `si vault` (or strict compatibility with that architecture).

## 1. Requirement Understanding (What Must Be Delivered)

This ticket introduces a new command surface:

- `si github ...`

It must support:

- Broad GitHub operations across repositories, pull requests, issues, actions/workflows, releases, and secrets.
- Clear auth model aligned with GitHub surfaces:
  - API is capability only.
  - Auth choices are GitHub App, OAuth App, PAT.
- Secure unattended automation defaults:
  - GitHub App is default for service/automation flows.
- Developer convenience fallback:
  - PAT support for local and one-off usage.
- Optional user-identity flow:
  - OAuth App support designed, but not required for MVP unless needed by specific commands.
- Credentials must come from `si vault` or be fully compatible with the architecture in `tickets/creds-management-integration-plan.md`.
- Multi-account context support.
- Consistent `si` UX:
  - interactive selectors where appropriate
  - colorized status output
  - strict JSON mode for machine consumption.

## 2. Definition Of Done

Implementation is complete when all are true:

1. `si github` is wired in command dispatch and help.
2. Credential resolution is vault-first and compatible with `si vault` storage and execution model.
3. GitHub App auth flow is implemented for unattended operations (JWT -> installation token).
4. PAT auth flow is implemented for local/manual usage.
5. Command context supports multiple GitHub accounts and clear default selection.
6. Core CRUD-style operations exist for key object families:
   - repo, issue, pull request, release, workflow run.
7. Actions/workflow controls exist (list runs, view run, rerun/cancel, logs/artifacts access).
8. GitHub secrets management commands exist with strong safety checks.
9. A raw fallback exists for endpoint parity:
   - REST raw
   - GraphQL query/mutation.
10. Error handling surfaces actionable detail (status, request id, message, docs URL) with secret redaction.
11. Rate-limit and abuse-limit handling is resilient (backoff/retry policy for safe operations).
12. Output is consistent with `si` styling; `--json` provides deterministic machine-readable output.
13. Unit + integration tests cover command parsing, auth, bridge behavior, and failure paths.
14. Docs are updated so an engineer can use `si github` without code diving.

## 3. Auth Mental Model (Adopted Policy)

### API

- GitHub API is capability only.
- All command behavior must be independent of auth mode at handler level.

### Auth modes

`si github` supports three auth modes:

1. `app` (default for automation):
   - GitHub App private key + app id + installation id (or installation lookup).
   - short-lived installation token.
2. `pat` (local convenience):
   - PAT from vault/env compatibility path.
3. `oauth` (user identity mode):
   - kept as a designed extension path; not required for MVP command coverage unless explicitly needed.

Default resolution order:

1. explicit `--auth-mode`
2. context default auth mode
3. `app` if app credentials exist
4. else `pat` if PAT exists
5. else fail with actionable credential guidance.

## 4. Vault Compatibility Contract (Mandatory)

`si github` must follow `tickets/creds-management-integration-plan.md` principles:

- Secrets encrypted at rest in vault repo (`vault/.env.<env>` pattern).
- No plaintext secret persistence in repo or settings.
- Runtime decryption in-memory only where possible.
- Compatible with `si vault run` and future `si vault docker exec` injection model.

### 4.1 Credential source contract

Primary source:

- internal vault resolver (future/native): read decrypted values via vault runtime integration.

Compatibility source (until native vault integration is fully available):

- environment variables with identical key names, so the same keys can be delivered by `si vault run -- ...`.

### 4.2 Canonical secret key names (vault/env compatible)

Global default keys:

- `GITHUB_API_BASE_URL` (optional, default `https://api.github.com`; supports GHES)
- `GITHUB_DEFAULT_OWNER` (optional)
- `GITHUB_DEFAULT_ACCOUNT` (optional context alias)

Per-account key pattern:

- `GITHUB_<ACCOUNT>_AUTH_MODE` (`app|pat|oauth`)
- `GITHUB_<ACCOUNT>_APP_ID`
- `GITHUB_<ACCOUNT>_APP_PRIVATE_KEY_PEM`
- `GITHUB_<ACCOUNT>_INSTALLATION_ID` (optional if install lookup is used)
- `GITHUB_<ACCOUNT>_PAT`
- `GITHUB_<ACCOUNT>_OAUTH_CLIENT_ID` (future)
- `GITHUB_<ACCOUNT>_OAUTH_CLIENT_SECRET` (future)
- `GITHUB_<ACCOUNT>_OAUTH_REFRESH_TOKEN` (future)

Notes:

- `<ACCOUNT>` is uppercase slug (example: `CORE`, `OPS`).
- PEM can be multiline in vault file, preserved as exact content.

### 4.3 Settings model (non-secret pointers only)

`settings.toml` additions:

- `[github]`
  - `default_account`
  - `default_auth_mode`
  - `api_base_url`
  - `vault_env` (default `dev`)
  - `vault_file` (optional explicit override)
- `[github.accounts.<alias>]`
  - `owner` (default owner/org)
  - `api_base_url` override
  - `auth_mode` default override
  - `vault_prefix` (example: `GITHUB_CORE_`)

Settings must not store raw PAT/private keys.

## 5. Command Surface (Planned)

### 5.1 Top-level

- `si github auth status`
- `si github context list`
- `si github context current`
- `si github context use --account <alias> [--auth-mode app|pat|oauth]`

### 5.2 Repository operations

- `si github repo list [--owner <org|user>] [--visibility ...]`
- `si github repo get <owner/repo>`
- `si github repo create <name> [flags]`
- `si github repo update <owner/repo> [flags]`
- `si github repo archive <owner/repo>`
- `si github repo delete <owner/repo> --force`

### 5.3 Pull request operations

- `si github pr list <owner/repo> [--state ...]`
- `si github pr get <owner/repo> <number>`
- `si github pr create <owner/repo> --head ... --base ... --title ...`
- `si github pr comment <owner/repo> <number> --body ...`
- `si github pr merge <owner/repo> <number> [--method merge|squash|rebase]`

### 5.4 Issue operations

- `si github issue list <owner/repo> [--state ...]`
- `si github issue get <owner/repo> <number>`
- `si github issue create <owner/repo> --title ... [--body ...]`
- `si github issue comment <owner/repo> <number> --body ...`
- `si github issue close <owner/repo> <number>`
- `si github issue reopen <owner/repo> <number>`

### 5.5 Actions/workflow operations

- `si github workflow list <owner/repo>`
- `si github workflow run <owner/repo> <workflow> [--ref ...] [--input k=v ...]`
- `si github workflow runs <owner/repo> [--workflow ...]`
- `si github workflow run get <owner/repo> <run-id>`
- `si github workflow run cancel <owner/repo> <run-id>`
- `si github workflow run rerun <owner/repo> <run-id>`
- `si github workflow logs <owner/repo> <run-id>`

### 5.6 Release operations

- `si github release list <owner/repo>`
- `si github release get <owner/repo> <tag|id>`
- `si github release create <owner/repo> --tag ... --title ... [--notes-file ...]`
- `si github release upload <owner/repo> <tag|id> --asset <path>`
- `si github release delete <owner/repo> <tag|id> --force`

### 5.7 Secrets operations

- `si github secret repo set <owner/repo> <name> --value ...`
- `si github secret repo delete <owner/repo> <name> --force`
- `si github secret env set <owner/repo> <env> <name> --value ...`
- `si github secret org set <org> <name> --value ... [--repos ...]`

### 5.8 Raw escape hatches

- `si github raw --method <GET|POST|PATCH|PUT|DELETE> --path <api-path> [--param ...]`
- `si github graphql --query <q> [--var k=json ...]`

## 6. Architecture Draft (V1)

Initial simple design:

1. Command handlers directly call GitHub REST helpers.
2. Auth mode resolved in each command.
3. Minimal shared client logic.

### V1 Weaknesses

- Duplicate auth/token lifecycle logic across commands.
- Inconsistent retry/rate-limit behavior.
- Harder to enforce uniform redaction/output/safety rules.
- Difficult to parallelize implementation safely.

## 7. Architecture Revision (V2, Recommended)

Revised design:

1. Shared bridge package for REST/GraphQL, retry, rate-limit parsing, and error normalization.
2. Auth provider abstraction:
   - AppProvider
   - PATProvider
   - OAuthProvider (stub/extension).
3. Context resolver layer:
   - account alias
   - owner defaults
   - base URL (GitHub.com vs GHES)
   - auth mode selection.
4. Command layer is thin and declarative.
5. Capability preflight:
   - optional `--check-permissions` mode to detect missing scopes/permissions early.

Why V2:

- Cleanly separates capability surface from auth model.
- Enforces GitHub App-first unattended design without breaking PAT workflows.
- Keeps command UX stable while backend providers evolve.

## 8. Global File Boundary Contract

### Allowed paths

- `tools/si/main.go`
- `tools/si/util.go`
- `tools/si/settings.go`
- `tools/si/github*.go` (new/updated)
- `tools/si/*github*_test.go`
- `tools/si/internal/githubbridge/**` (new)
- `docs/GITHUB.md` (new)
- `docs/SETTINGS.md`
- `README.md`
- `tickets/github-integration-plan.md` (this file)

### Disallowed paths

- `agents/**` (unrelated runtime changes)
- `tools/codex-init/**`
- `tools/codex-stdout-parser/**`
- unrelated existing command behavior unless required for command registration/help consistency

### Secret handling rules

- Never log raw token/private key material.
- Never persist ephemeral installation tokens to git-tracked files.
- Redact auth headers and known token formats in all errors and debug output.

## 9. Workstream Status Board

| Workstream | Status | Owner | Branch | PR | Last Update |
|---|---|---|---|---|---|
| WS-00 Contracts | Not Started |  |  |  | 2026-02-08 |
| WS-01 CLI Entry | Not Started |  |  |  | 2026-02-08 |
| WS-02 Vault/Auth Context | Not Started |  |  |  | 2026-02-08 |
| WS-03 App Auth Provider | Not Started |  |  |  | 2026-02-08 |
| WS-04 PAT/OAuth Provider | Not Started |  |  |  | 2026-02-08 |
| WS-05 Bridge Core (REST/GraphQL) | Not Started |  |  |  | 2026-02-08 |
| WS-06 Core Resource Commands | Not Started |  |  |  | 2026-02-08 |
| WS-07 Actions/Releases/Secrets | Not Started |  |  |  | 2026-02-08 |
| WS-08 Raw + Safety + Output | Not Started |  |  |  | 2026-02-08 |
| WS-09 Testing + E2E | Not Started |  |  |  | 2026-02-08 |
| WS-10 Docs + Release | Not Started |  |  |  | 2026-02-08 |

Status values: `Not Started | In Progress | Blocked | Done`

## 10. Independent Parallel Workstreams

## WS-00 Contracts

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/github_contract.go`
- `tools/si/internal/githubbridge/types.go`

Deliverables:
1. Runtime context models (`account`, `owner`, `auth mode`, `api base`).
2. Provider interfaces and normalized error DTO.

Acceptance:
- All other workstreams compile against these contracts.

## WS-01 CLI Entry and Help

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/main.go`
- `tools/si/util.go`
- `tools/si/github_cmd.go`

Deliverables:
1. `si github` dispatch and subcommand tree.
2. Help text aligned with existing `si` style.

Acceptance:
- `si --help` and `si github --help` are complete and accurate.

## WS-02 Vault/Auth Context Resolution

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/settings.go` (`[github]` non-secret config)
- `tools/si/github_auth.go`
- `tools/si/github_auth_test.go`

Deliverables:
1. Vault-first credential resolution contract.
2. Env compatibility fallback using vault-compatible key names.
3. Context commands (`auth status`, `context list/current/use`).

Acceptance:
- Missing credential errors instruct exactly which vault/env keys are required.

## WS-03 GitHub App Provider

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/internal/githubbridge/auth_app.go`
- `tools/si/internal/githubbridge/auth_app_test.go`

Deliverables:
1. App JWT signing.
2. Installation token exchange and expiration handling.
3. Installation selection strategy:
   - explicit installation id
   - owner/repo lookup fallback.

Acceptance:
- Unattended commands can run with short-lived installation tokens only.

## WS-04 PAT/OAuth Provider

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/internal/githubbridge/auth_pat.go`
- `tools/si/internal/githubbridge/auth_oauth.go` (stub or implementation)

Deliverables:
1. PAT provider with scope diagnostics where available.
2. OAuth provider scaffolding (or explicit deferred stub).

Acceptance:
- PAT flows are reliable for local workflows.
- OAuth status is explicit (implemented vs deferred).

## WS-05 Bridge Core (REST/GraphQL)

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/internal/githubbridge/client.go`
- `tools/si/internal/githubbridge/rest.go`
- `tools/si/internal/githubbridge/graphql.go`
- `tools/si/internal/githubbridge/errors.go`
- `tools/si/internal/githubbridge/pagination.go`

Deliverables:
1. Unified request execution and response normalization.
2. Pagination helpers.
3. Retry/backoff strategy for transient failures and rate-limits.
4. Error model preserving:
   - status
   - request id
   - docs URL/message
   - GraphQL error details.

Acceptance:
- Deterministic behavior under 403 rate limit / abuse detection / 5xx retries.

## WS-06 Core Resource Commands

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/github_repo_cmd.go`
- `tools/si/github_pr_cmd.go`
- `tools/si/github_issue_cmd.go`
- `tools/si/*github*_test.go`

Deliverables:
1. Repo, PR, and issue command handlers.
2. Safe defaults for mutating operations.

Acceptance:
- Core resource workflows function end-to-end for app and pat auth modes.

## WS-07 Actions / Releases / Secrets

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/github_workflow_cmd.go`
- `tools/si/github_release_cmd.go`
- `tools/si/github_secret_cmd.go`

Deliverables:
1. Workflow run controls and logs access.
2. Release lifecycle commands.
3. Secrets commands with explicit confirmations for destructive operations.

Acceptance:
- Critical CI and release operations are usable from CLI with clear safety rails.

## WS-08 Raw + Output + Safety

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/github_raw_cmd.go`
- `tools/si/github_output.go`
- `tools/si/github_safety.go`

Deliverables:
1. Raw REST and GraphQL escape hatches.
2. Standardized human + JSON output modes.
3. Redaction and confirmation policies.

Acceptance:
- Unknown API surfaces are still reachable without shipping new typed commands.

## WS-09 Testing + E2E

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/*github*_test.go`
- `tools/si/internal/githubbridge/*_test.go`
- `tools/si/testdata/github/**`

Deliverables:
1. Unit tests for parsers, auth resolution, redaction, and error mapping.
2. Integration tests with mocked GitHub API.
3. Optional gated live tests (`SI_GITHUB_E2E=1`).

Acceptance:
- New command set is regression-protected for main flows and edge cases.

## WS-10 Docs + Release

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `README.md`
- `docs/SETTINGS.md`
- `docs/GITHUB.md` (new)
- `CHANGELOG.md`

Deliverables:
1. Docs for auth mode decisions and vault setup.
2. Practical command recipes for app and pat usage.
3. Release notes and help updates.

Acceptance:
- New engineers can configure and use `si github` with vault in under 15 minutes.

## 11. Edge Case Matrix (Must Be Tested)

1. App credentials present but installation id missing.
2. App installed on multiple orgs/repos and owner is ambiguous.
3. Installation token lacks permission for requested operation.
4. PAT lacks required scopes or is SSO-restricted.
5. Token expired mid-pagination.
6. Secondary rate limit / abuse limit from burst operations.
7. GitHub Enterprise base URL with self-signed certs.
8. Repo renamed/transferred between API calls.
9. Workflow log/artifact endpoints returning redirects/large payloads.
10. GraphQL partial success with `errors` and partial `data`.
11. Secret set commands with invalid key names or missing visibility target.
12. Non-interactive mode for destructive commands without `--force`.
13. Vault key exists but decrypt fails (wrong recipient / trust drift).
14. Vault unavailable: env fallback works only when explicitly enabled/available.
15. Mixed auth modes in one session (`context use` changes mode/account).

## 12. Self-Review and Plan Revision (Introspection)

### 12.1 Critique of initial draft

Initial draft risked:

- overloading MVP with full OAuth login UX
- under-specifying vault integration detail
- not explicitly addressing GHES and multi-install ambiguity
- not separating bridge/auth/context enough for parallel work.

### 12.2 Revisions applied

1. OAuth moved to optional/stub path unless required by concrete flows.
2. Vault compatibility upgraded to a strict contract with canonical key naming.
3. Added explicit App-first auth policy and fallback chain.
4. Added dedicated context and provider layers to isolate responsibilities.
5. Added edge-case matrix with rate-limit, GHES, and installation ambiguity cases.
6. Added precise path boundaries and parallel workstreams to reduce merge conflicts.

### 12.3 Additional enhancements recommended

1. Add `si github doctor` for:
   - credential source diagnostics
   - permission preflight
   - rate-limit visibility.
2. Add `si github policy check` to validate command against current auth mode permissions before execution.
3. Add lightweight per-run token cache keyed by account+auth mode with strict expiry.
4. Add audit log stream:
   - command/action metadata only
   - no secret values.
5. Add migration command when native vault integration lands:
   - `si github auth migrate-to-vault`.

## 13. Agent Update Template (Per Workstream)

Use this template for each workstream update:

```md
### WS-XX <Name>
- Status: Not Started | In Progress | Blocked | Done
- Owner:
- Branch:
- PR:
- Changed paths:
- Tests run:
- Open risks/blockers:
- Next step:
- Last updated: YYYY-MM-DD
```

## 14. Out Of Scope (MVP)

1. Full browser OAuth dance and hosted callback UX.
2. Organization-wide policy administration parity with every GitHub admin endpoint.
3. Full Codespaces/package/container registry management in first cut.
4. Centralized secret manager backend (HashiCorp Vault/cloud secret managers).
5. Server-side webhook receiver service inside `si` runtime.

