# GitHub Command Guide (`si github`)

`si github` supports GitHub REST/GraphQL using either GitHub App auth or OAuth token auth.

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
