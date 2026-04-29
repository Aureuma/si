# GitHub Command Guide (`si orbit github`)

![GitHub](/docs/images/integrations/github.svg)

`si orbit github` supports GitHub REST/GraphQL using either GitHub App auth or OAuth token auth.

Related:
- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [Providers](./PROVIDERS)

Auth policy:
- `app` mode: GitHub App installation tokens
- `oauth` mode: OAuth access token / token-based auth (including PAT-style tokens)
- Credentials should be resolved through configured Fort bindings or compatible env keys; use `si fort` for runtime secret access.

## Credential Keys (Fort/Env-Compatible)

Per account alias `<ACCOUNT>` (uppercase slug):

- `GITHUB_<ACCOUNT>_APP_ID`
- `GITHUB_<ACCOUNT>_APP_PRIVATE_KEY_PEM`
- `GITHUB_<ACCOUNT>_INSTALLATION_ID` (optional)
- `GITHUB_<ACCOUNT>_OAUTH_ACCESS_TOKEN`
- `GITHUB_<ACCOUNT>_TOKEN`

Global fallback keys:

- `GITHUB_APP_ID`
- `GITHUB_APP_PRIVATE_KEY_PEM`
- `GITHUB_INSTALLATION_ID`
- `GITHUB_OAUTH_TOKEN`
- `GITHUB_TOKEN`
- `GH_TOKEN`
- `GITHUB_API_BASE_URL`
- `GITHUB_DEFAULT_OWNER`
- `GITHUB_DEFAULT_ACCOUNT`
- `GITHUB_AUTH_MODE`
- `GITHUB_DEFAULT_AUTH_MODE`

## Context

```bash
si orbit github auth status --account core
si orbit github auth status --auth-mode oauth --token "$GITHUB_TOKEN"
si orbit github context list
si orbit github context current
si orbit github context use --account core --owner Aureuma --auth-mode app --base-url https://api.github.com
si orbit github context use --account core --auth-mode oauth --token-env GITHUB_CORE_OAUTH_ACCESS_TOKEN
```

## Git Remotes (No PAT URLs)

For host/admin Git credential-helper setup, use GitHub App tokens through `si vault run`, then normalize remotes to PAT-free HTTPS URLs:

```bash
si vault run -- si orbit github git setup --root ~/Development --account core --owner Aureuma
```

Note:
- `si vault run` usage here is host/admin-side.
- For SI runtime workers, use `si fort ...` for secret access paths.

Optional custom vault scope for helper auth:

```bash
si orbit github git setup \
  --root ~/Development \
  --account core \
  --owner Aureuma \
  --vault-file default
```

Common flags:
- `--remote <name>`: choose a remote other than `origin`
- `--helper-owner <owner>`: force a fixed owner in helper calls (default derives from remote path)
- `--no-vault`: use direct env lookup instead of wrapping helper calls with `si vault run`
- `--dry-run`: preview remote/helper changes without writing

Helper-only usage (for manual git credential helper wiring):

```bash
si orbit github git credential get
```

## Git Remotes (PAT URLs from Vault)

When you need explicit PAT-authenticated remotes (for CI/dev environments that do not use git credential helpers), use:

```bash
si orbit github git remote \
  --root ~/Development \
  --owner Aureuma \
  --vault-key GH_PAT_AUREUMA_VANGUARDA
```

This command:
- reads the PAT from `si vault` using `--vault-key`
- rewrites both fetch and push URLs for the target remote (default `origin`) to:
  - `https://<PAT>@github.com/<owner>/<repo>.git`
- sets local branch upstream tracking so plain `git push` / `git pull` work without extra remote/branch args

Useful flags:
- `--remote <name>`: remote name to rewrite (default `origin`)
- `--owner <owner>`: only apply to repos for that owner/org
- `--track-upstream=false`: skip branch tracking update
- `--dry-run`: preview changes without writing
- `--json`: structured output for automation

To clone a new repository directly with PAT URL auth sourced from vault:

```bash
si orbit github git clone Aureuma/GitHubProj \
  --root ~/Development \
  --vault-key GH_PAT_AUREUMA_VANGUARDA
```

`clone` supports either `owner/repo` or full GitHub URL input, rewrites both fetch/push URLs with PAT auth, and sets upstream tracking for plain `git push` / `git pull`.

### Troubleshooting Git App Access

If fetch/push still fails after setup:

- `Repository not found` for private repos usually means the app installation does not include that repo.
- `github app installation id is required` means owner/repo context could not map to an installation; pass `--owner`/`--helper-owner` or set `GITHUB_<ACCOUNT>_INSTALLATION_ID`.

Useful checks:

```bash
si orbit github auth status --account core --auth-mode app --json
si orbit github doctor --account core --owner Aureuma --auth-mode app
si orbit github git setup --root ~/Development --account core --owner Aureuma --dry-run
```

## Repositories

```bash
si orbit github repo list Aureuma
si orbit github repo get Aureuma/si
si orbit github repo create si-demo --owner Aureuma
si orbit github repo update Aureuma/si --param description="si substrate"
si orbit github repo archive Aureuma/si --force
si orbit github repo delete Aureuma/si-demo --force
```

## Branches and Protection

```bash
si orbit github branch list Aureuma/si
si orbit github branch get Aureuma/si main
si orbit github branch create Aureuma/si --name feature/release-train --from main
si orbit github branch delete Aureuma/si feature/release-train --force

si orbit github branch protect Aureuma/si main --required-check ci --required-check lint --required-approvals 2
si orbit github branch unprotect Aureuma/si main --force
```

## Pull Requests

```bash
si orbit github pr list Aureuma/si
si orbit github pr get Aureuma/si 123
si orbit github pr create Aureuma/si --head feature-branch --base main --title "Feature" --body "Summary"
si orbit github pr comment Aureuma/si 123 --body "Looks good"
si orbit github pr merge Aureuma/si 123 --method squash
```

## Issues

```bash
si orbit github issue list Aureuma/si
si orbit github issue get Aureuma/si 456
si orbit github issue create Aureuma/si --title "Bug" --body "Repro"
si orbit github issue comment Aureuma/si 456 --body "Investigating"
si orbit github issue close Aureuma/si 456
si orbit github issue reopen Aureuma/si 456
```

## Projects (GitHub Projects v2)

Project reference inputs accepted by project commands:

- project node ID (for example `PVT_kwDOB2x6Nc4ArlO7`)
- `org/number` (for example `Aureuma/7`)
- project URL (for example `https://github.com/orgs/Aureuma/projects/7/views/4`)
- project number (`7`) when org is available from `--owner` or current context owner

```bash
si orbit github project list Aureuma
si orbit github project get Aureuma/7
si orbit github project update Aureuma/7 --title "Q1 Delivery" --description "Shared roadmap board" --public true
si orbit github project fields Aureuma/7
si orbit github project items Aureuma/7 --include-archived

# add an existing issue to project
si orbit github project add Aureuma/7 --repo Aureuma/GHPSandbox --issue 123

# update project item status by field/option names
si orbit github project set Aureuma/7 PVTI_xxx --field Status --single-select "In Progress"

# update scalar field values
si orbit github project set Aureuma/7 PVTI_xxx --field Estimate --number 3
si orbit github project set Aureuma/7 PVTI_xxx --field DueDate --date 2026-02-28

# clear/archive/delete item state
si orbit github project clear Aureuma/7 PVTI_xxx --field Estimate
si orbit github project archive Aureuma/7 PVTI_xxx
si orbit github project unarchive Aureuma/7 PVTI_xxx
si orbit github project delete Aureuma/7 PVTI_xxx
```

Notes:

- `set` accepts exactly one value update at a time: `--text`, `--number`, `--date`, `--single-select-option-id`, `--single-select`, `--iteration-id`, or `--iteration`.
- `--single-select` and `--iteration` resolve IDs from project field metadata automatically.
- OAuth/PAT auth for Projects v2 needs project permissions (`read:project` for read/list/get/fields/items and `project` write scope for item mutations). Issue-linked operations also need repo issue permissions on the target repository.

## Workflows

```bash
si orbit github workflow list Aureuma/si
si orbit github workflow run Aureuma/si ci.yml --ref main --input run_full=true
si orbit github workflow runs Aureuma/si
si orbit github workflow run get Aureuma/si 1234567890
si orbit github workflow run cancel Aureuma/si 1234567890
si orbit github workflow run rerun Aureuma/si 1234567890
si orbit github workflow logs Aureuma/si 1234567890 --raw
```

## Releases

```bash
si orbit github release list
si orbit github release get vX.Y.0
si orbit github release create --tag vX.Y.0 --notes-file ./notes.md
si orbit github release create --tag vX.Y.0 --target "$(git rev-parse HEAD)" --draft
si orbit github release upload vX.Y.0 --asset ./dist/si-linux-amd64
si orbit github release delete vX.Y.0 --force
```

Release create behavior:

- When you run inside a GitHub checkout, release commands infer `owner/repo` from `origin`. Use `-R, --repo <owner/repo>` to override or run outside a checkout.
- For SI itself, the release tag should come from the one repo-wide version in root `Cargo.toml [workspace.package].version`.
- For SI releases, use the canonical `vX.Y.0` tag form. Do not create GitHub Releases against bare tags such as `0.50.0`.
- If `--title` is omitted, `si orbit github release create` reuses the tag as the release title.
- If the requested tag already exists remotely, `si orbit github release create` reuses it.
- If the tag is missing and `--target <sha>` is provided, SI creates `refs/tags/<tag>` first, then creates the release.
- If the tag is missing and `--target` is omitted, the command fails clearly instead of creating a broken/tagless release flow.
- For draft releases, GitHub can still report an `untagged-...` release URL until publish time. Treat `tag_name` plus the remote git ref as the source of truth.
- For SI, patch versions are not published release lines. Do not create GitHub Releases for `vX.Y.Z` where `Z > 0`.

Practical first-release example:

```bash
si orbit github release create Aureuma/releasemind \
  --auth-mode app \
  --account core \
  --owner Aureuma \
  --tag v0.1.0 \
  --title "v0.1.0" \
  --target c2eb19f59c0f71dcdc41c1d7b6c4dcad54e5f480 \
  --notes "Initial draft release." \
  --draft
git ls-remote --tags git@github.com:Aureuma/releasemind.git
```

## Secrets

`si orbit github` fetches the target public key, encrypts plaintext with sealed-box compatible encryption, then upserts the secret.

```bash
si orbit github secret repo set Aureuma/si MY_SECRET --value "..."
si orbit github secret repo delete Aureuma/si MY_SECRET --force

si orbit github secret env set Aureuma/si sandbox MY_SECRET --value "..."
si orbit github secret env delete Aureuma/si sandbox MY_SECRET --force

si orbit github secret org set Aureuma MY_SECRET --value "..." --visibility private
si orbit github secret org set Aureuma MY_SECRET --value "..." --visibility selected --repos 123,456
si orbit github secret org delete Aureuma MY_SECRET --force
```

## Raw REST / GraphQL

```bash
si orbit github raw --method GET --path /repos/Aureuma/si
si orbit github raw --method POST --path /repos/Aureuma/si/issues --body '{"title":"Hello"}'

si orbit github graphql --query 'query { viewer { login } }'
si orbit github graphql --query 'query($owner:String!,$name:String!){ repository(owner:$owner,name:$name){ id } }' --var owner='"Aureuma"' --var name='"si"'
```

## Error Reporting

On failures, `si orbit github` surfaces:

- HTTP status
- request id (`X-GitHub-Request-Id`)
- API message and documentation URL
- structured `errors` when present
- redacted raw body for debugging
