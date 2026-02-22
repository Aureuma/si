# GitHub Command Guide (`si github`)

![GitHub](/docs/images/integrations/github.svg)

`si github` supports GitHub REST/GraphQL using either GitHub App auth or OAuth token auth.

Related:
- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [Providers](./PROVIDERS)

Auth policy:
- `app` mode: GitHub App installation tokens
- `oauth` mode: OAuth access token / token-based auth (including PAT-style tokens)
- Credentials should be injected from `si vault` (or compatible env keys).

## Credential Keys (Vault-Compatible)

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
si github auth status --account core
si github auth status --auth-mode oauth --token "$GITHUB_TOKEN"
si github context list
si github context current
si github context use --account core --owner Aureuma --auth-mode app --base-url https://api.github.com
si github context use --account core --auth-mode oauth --token-env GITHUB_CORE_OAUTH_ACCESS_TOKEN
```

## Git Remotes (No PAT URLs)

Use GitHub App tokens through `si vault` as a Git credential helper, then normalize remotes to PAT-free HTTPS URLs:

```bash
si vault run -- si github git setup --root ~/Development --account core --owner Aureuma
```

Recommended for file-key vault backends (non-interactive git credential helper):

```bash
si github git setup \
  --root ~/Development \
  --account core \
  --owner Aureuma \
  --vault-file ~/.si/vault/dev.env \
  --vault-identity-file ~/.si/vault/keys/age.key
```

Common flags:
- `--remote <name>`: choose a remote other than `origin`
- `--helper-owner <owner>`: force a fixed owner in helper calls (default derives from remote path)
- `--no-vault`: use direct env lookup instead of wrapping helper calls with `si vault run`
- `--dry-run`: preview remote/helper changes without writing

Helper-only usage (for manual git credential helper wiring):

```bash
si github git credential get
```

## Git Remotes (PAT URLs from Vault)

When you need explicit PAT-authenticated remotes (for CI/dev environments that do not use git credential helpers), use:

```bash
si github git remote-auth \
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
si github git clone-auth Aureuma/GitHubProj \
  --root ~/Development \
  --vault-key GH_PAT_AUREUMA_VANGUARDA
```

`clone-auth` supports either `owner/repo` or full GitHub URL input, rewrites both fetch/push URLs with PAT auth, and sets upstream tracking for plain `git push` / `git pull`.

### Troubleshooting Git App Access

If fetch/push still fails after setup:

- `Repository not found` for private repos usually means the app installation does not include that repo.
- `github app installation id is required` means owner/repo context could not map to an installation; pass `--owner`/`--helper-owner` or set `GITHUB_<ACCOUNT>_INSTALLATION_ID`.

Useful checks:

```bash
si github auth status --account core --auth-mode app --json
si github doctor --account core --owner Aureuma --auth-mode app
si github git setup --root ~/Development --account core --owner Aureuma --dry-run
```

## Repositories

```bash
si github repo list Aureuma
si github repo get Aureuma/si
si github repo create si-demo --owner Aureuma
si github repo update Aureuma/si --param description="si substrate"
si github repo archive Aureuma/si --force
si github repo delete Aureuma/si-demo --force
```

## Branches and Protection

```bash
si github branch list Aureuma/si
si github branch get Aureuma/si main
si github branch create Aureuma/si --name feature/release-train --from main
si github branch delete Aureuma/si feature/release-train --force

si github branch protect Aureuma/si main --required-check ci --required-check lint --required-approvals 2
si github branch unprotect Aureuma/si main --force
```

## Pull Requests

```bash
si github pr list Aureuma/si
si github pr get Aureuma/si 123
si github pr create Aureuma/si --head feature-branch --base main --title "Feature" --body "Summary"
si github pr comment Aureuma/si 123 --body "Looks good"
si github pr merge Aureuma/si 123 --method squash
```

## Issues

```bash
si github issue list Aureuma/si
si github issue get Aureuma/si 456
si github issue create Aureuma/si --title "Bug" --body "Repro"
si github issue comment Aureuma/si 456 --body "Investigating"
si github issue close Aureuma/si 456
si github issue reopen Aureuma/si 456
```

## Workflows

```bash
si github workflow list Aureuma/si
si github workflow run Aureuma/si ci.yml --ref main --input run_full=true
si github workflow runs Aureuma/si
si github workflow run get Aureuma/si 1234567890
si github workflow run cancel Aureuma/si 1234567890
si github workflow run rerun Aureuma/si 1234567890
si github workflow logs Aureuma/si 1234567890 --raw
```

## Releases

```bash
si github release list Aureuma/si
si github release get Aureuma/si v0.44.0
si github release create Aureuma/si --tag v0.44.0 --title "v0.44.0" --notes-file ./notes.md
si github release upload Aureuma/si v0.44.0 --asset ./dist/si-linux-amd64
si github release delete Aureuma/si v0.44.0 --force
```

## Secrets

`si github` fetches the target public key, encrypts plaintext with sealed-box compatible encryption, then upserts the secret.

```bash
si github secret repo set Aureuma/si MY_SECRET --value "..."
si github secret repo delete Aureuma/si MY_SECRET --force

si github secret env set Aureuma/si sandbox MY_SECRET --value "..."
si github secret env delete Aureuma/si sandbox MY_SECRET --force

si github secret org set Aureuma MY_SECRET --value "..." --visibility private
si github secret org set Aureuma MY_SECRET --value "..." --visibility selected --repos 123,456
si github secret org delete Aureuma MY_SECRET --force
```

## Raw REST / GraphQL

```bash
si github raw --method GET --path /repos/Aureuma/si
si github raw --method POST --path /repos/Aureuma/si/issues --body '{"title":"Hello"}'

si github graphql --query 'query { viewer { login } }'
si github graphql --query 'query($owner:String!,$name:String!){ repository(owner:$owner,name:$name){ id } }' --var owner='"Aureuma"' --var name='"si"'
```

## Error Reporting

On failures, `si github` surfaces:

- HTTP status
- request id (`X-GitHub-Request-Id`)
- API message and documentation URL
- structured `errors` when present
- redacted raw body for debugging
