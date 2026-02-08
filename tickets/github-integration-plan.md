# Ticket: `si github` Full GitHub Integration (Vault-Compatible, App-Only)

Date: 2026-02-08
Owner: Unassigned
Primary Goal: Add `si github ...` as a first-class command family for broad GitHub control, monitoring, and automation using GitHub REST/GraphQL APIs with credentials sourced from `si vault` (or strict compatibility with that architecture).

## 0. Decision Lock (Updated)

This plan is now explicitly locked to:

1. GitHub App authentication only.
2. Direct REST + GraphQL API integration (custom bridge in `tools/si/internal/githubbridge`).
3. No `go-github` SDK dependency.
4. No PAT and no OAuth command paths for `si github`.

Rationale:

- Matches unattended/automation-first security requirements.
- Keeps full control over output formatting, redaction, and error surfacing.
- Preserves raw endpoint parity without waiting on SDK coverage.
- Aligns with the requirement to use vault-compatible credentials.

## 1. Requirement Understanding (What Must Be Delivered)

This ticket introduces a new command surface:

- `si github ...`

It must support:

- Broad GitHub operations across repositories, pull requests, issues, actions/workflows, releases, and secrets.
- Clear auth model aligned with GitHub surfaces:
  - API is capability only.
  - Auth is GitHub App only.
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
4. Command context supports multiple GitHub accounts and clear default selection.
5. Core CRUD-style operations exist for key object families:
   - repo, issue, pull request, release, workflow run.
6. Actions/workflow controls exist (list runs, view run, rerun/cancel, logs/artifacts access).
7. GitHub secrets management commands exist with strong safety checks.
8. Raw fallback exists for endpoint parity:
   - REST raw
   - GraphQL query/mutation.
9. Error handling surfaces actionable detail (status, request id, message, docs URL) with secret redaction.
10. Rate-limit and abuse-limit handling is resilient (backoff/retry policy for safe operations).
11. Output is consistent with `si` styling; `--json` provides deterministic machine-readable output.
12. Unit + integration tests cover command parsing, auth, bridge behavior, and failure paths.
13. Docs are updated so an engineer can use `si github` without code diving.

## 3. Auth Mental Model (Adopted Policy)

### API

- GitHub API is capability only.
- Command handlers remain capability-focused, with auth handled by runtime resolver + provider.

### Auth mode

`si github` uses one auth mode:

1. `app`:
   - GitHub App private key + app id + installation id (or installation lookup).
   - short-lived installation token.

Resolution policy:

1. Resolve account + owner + base URL.
2. Resolve app credentials from vault-compatible key set.
3. Build App provider.
4. Fetch short-lived installation token.
5. Execute API request.

## 4. Vault Compatibility Contract (Mandatory)

`si github` follows `tickets/creds-management-integration-plan.md` principles:

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

- `GITHUB_<ACCOUNT>_APP_ID`
- `GITHUB_<ACCOUNT>_APP_PRIVATE_KEY_PEM`
- `GITHUB_<ACCOUNT>_INSTALLATION_ID` (optional if install lookup is used)

Notes:

- `<ACCOUNT>` is uppercase slug (example: `CORE`, `OPS`).
- PEM can be multiline in vault file, preserved as exact content.

### 4.3 Settings model (non-secret pointers only)

`settings.toml` additions:

- `[github]`
  - `default_account`
  - `default_auth_mode` (fixed to `app`)
  - `api_base_url`
  - `vault_env` (default `dev`)
  - `vault_file` (optional explicit override)
- `[github.accounts.<alias>]`
  - `owner` (default owner/org)
  - `api_base_url` override
  - `vault_prefix` (example: `GITHUB_CORE_`)
  - `app_id_env`
  - `app_private_key_env`
  - `installation_id_env`

Settings must not store raw token material.

## 5. Command Surface (Planned)

### 5.1 Top-level

- `si github auth status`
- `si github context list`
- `si github context current`
- `si github context use --account <alias> [--owner <owner>] [--base-url <url>]`

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

## 6. Architecture (V2 Locked)

1. Shared bridge package for REST/GraphQL, retry, rate-limit parsing, and error normalization.
2. App auth provider only:
   - JWT signer
   - installation token exchange
   - installation lookup fallback.
3. Context resolver layer:
   - account alias
   - owner defaults
   - base URL (GitHub.com vs GHES).
4. Command layer remains thin and declarative.
5. No `go-github` dependency in this architecture.

## 7. Global File Boundary Contract

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

## 8. Workstream Status Board

| Workstream | Status | Owner | Branch | PR | Last Update |
|---|---|---|---|---|---|
| WS-00 Contracts | Done | Codex | main | n/a | 2026-02-08 |
| WS-01 CLI Entry | Done | Codex | main | n/a | 2026-02-08 |
| WS-02 Vault/Auth Context | In Progress | Codex | main | n/a | 2026-02-08 |
| WS-03 App Auth Provider | In Progress | Codex | main | n/a | 2026-02-08 |
| WS-04 Legacy Auth Providers | Done (Removed) | Codex | main | n/a | 2026-02-08 |
| WS-05 Bridge Core (REST/GraphQL) | In Progress | Codex | main | n/a | 2026-02-08 |
| WS-06 Core Resource Commands | Not Started |  |  |  | 2026-02-08 |
| WS-07 Actions/Releases/Secrets | Not Started |  |  |  | 2026-02-08 |
| WS-08 Raw + Safety + Output | In Progress | Codex | main | n/a | 2026-02-08 |
| WS-09 Testing + E2E | In Progress | Codex | main | n/a | 2026-02-08 |
| WS-10 Docs + Release | Not Started |  |  |  | 2026-02-08 |

Status values: `Not Started | In Progress | Blocked | Done`

## 9. Independent Parallel Workstreams

## WS-00 Contracts

Status:
- State: Done
- Owner: Codex
- Notes: runtime contracts and bridge DTOs added.

Path ownership:
- `tools/si/github_contract.go`
- `tools/si/internal/githubbridge/types.go`

Deliverables:
1. Runtime context models (`account`, `owner`, `api base`).
2. Provider interfaces and normalized error DTO.

Acceptance:
- All other workstreams compile against these contracts.

## WS-01 CLI Entry and Help

Status:
- State: Done
- Owner: Codex
- Notes: dispatch/help wiring landed.

Path ownership:
- `tools/si/main.go`
- `tools/si/util.go`
- `tools/si/github_cmd.go`

Deliverables:
1. `si github` dispatch and subcommand tree.
2. Help text aligned with existing `si` style.

Acceptance:
- `si --help` and `si github --help` include GitHub command surface.

## WS-02 Vault/Auth Context Resolution

Status:
- State: In Progress
- Owner: Codex
- Notes: App-only resolver implemented; tests pending.

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
- State: In Progress
- Owner: Codex
- Notes: JWT signing + installation exchange + lookup implemented; tests pending.

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

## WS-04 Legacy Auth Providers (Removed)

Status:
- State: Done
- Owner: Codex
- Notes: PAT/OAuth provider files removed by design.

Path ownership:
- removed: `tools/si/internal/githubbridge/auth_pat.go`
- removed: `tools/si/internal/githubbridge/auth_oauth.go`

Deliverables:
1. App-only policy enforced in code and plan.

Acceptance:
- No PAT/OAuth path exists in `si github` runtime auth selection.

## WS-05 Bridge Core (REST/GraphQL)

Status:
- State: In Progress
- Owner: Codex
- Notes: client/errors/pagination/logging implemented; endpoint-specific wrappers pending.

Path ownership:
- `tools/si/internal/githubbridge/client.go`
- `tools/si/internal/githubbridge/errors.go`
- `tools/si/internal/githubbridge/pagination.go`
- `tools/si/internal/githubbridge/request.go`
- `tools/si/internal/githubbridge/logging.go`

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
- Core resource workflows function end-to-end with GitHub App auth.

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
- State: In Progress
- Owner: Codex
- Notes: raw/graphql + output formatter added; safety policy file still pending.

Path ownership:
- `tools/si/github_raw_cmd.go`
- `tools/si/github_output.go`
- `tools/si/github_safety.go`

Deliverables:
1. Raw REST and GraphQL escape hatches.
2. Standardized human + JSON output modes.
3. Redaction and confirmation policies.

Acceptance:
- Unknown API surfaces are reachable without shipping new typed commands.

## WS-09 Testing + E2E

Status:
- State: In Progress
- Owner: Codex
- Notes: baseline `go test ./tools/si/...` passes; github-focused tests pending.

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
1. Docs for GitHub App auth decisions and vault setup.
2. Practical command recipes for App usage.
3. Release notes and help updates.

Acceptance:
- New engineers can configure and use `si github` with vault in under 15 minutes.

## 10. Edge Case Matrix (Must Be Tested)

1. App credentials present but installation id missing.
2. App installed on multiple orgs/repos and owner is ambiguous.
3. Installation token lacks permission for requested operation.
4. Token expired mid-pagination.
5. Secondary rate limit / abuse limit from burst operations.
6. GitHub Enterprise base URL with self-signed certs.
7. Repo renamed/transferred between API calls.
8. Workflow log/artifact endpoints returning redirects/large payloads.
9. GraphQL partial success with `errors` and partial `data`.
10. Secret set commands with invalid key names or missing visibility target.
11. Non-interactive mode for destructive commands without `--force`.
12. Vault key exists but decrypt fails (wrong recipient / trust drift).
13. Vault unavailable: env fallback works only when explicitly enabled/available.
14. Installation lookup cannot resolve owner context.
15. Mixed account contexts in one session (`context use` changes account/base URL).

## 11. Self-Review and Plan Revision (Introspection)

### 11.1 Critique of initial draft

Initial draft had unnecessary auth breadth for this product objective:

- PAT and OAuth increased complexity without improving unattended automation.
- Auth matrix would have expanded testing and support burden.

### 11.2 Revisions applied

1. Removed PAT/OAuth from scope and runtime architecture.
2. Standardized on GitHub App-only credentials and token flow.
3. Removed SDK dependence discussion and locked to direct REST/GraphQL bridge.
4. Updated workstreams and statuses to reflect current in-repo implementation state.

### 11.3 Additional enhancements recommended

1. Add `si github doctor` for:
   - credential source diagnostics
   - installation lookup diagnostics
   - rate-limit visibility.
2. Add capability preflight (`si github policy check`) using endpoint probes.
3. Add lightweight per-run token cache keyed by account + owner with strict expiry.
4. Add audit log stream with command/action metadata only.
5. Add migration command when native vault integration lands:
   - `si github auth migrate-to-vault`.

## 12. Agent Update Template (Per Workstream)

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

## 13. Out Of Scope (MVP)

1. Browser OAuth flow or user-identity auth features.
2. PAT-based auth mode.
3. Organization-wide policy administration parity with every GitHub admin endpoint.
4. Full Codespaces/package/container registry management in first cut.
5. Centralized secret manager backend (HashiCorp Vault/cloud secret managers).
6. Server-side webhook receiver service inside `si` runtime.
