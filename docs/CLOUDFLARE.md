# Cloudflare Command Guide (`si cloudflare`)

`si cloudflare` is the Cloudflare bridge for account context, operational workflows, and raw API access.

Auth policy:
- API token only.
- Credentials should be injected from `si vault` (or compatible env keys).
- Settings should store env references/pointers, not raw secrets.

## Credential Keys (Vault-Compatible)

Per account alias `<ACCOUNT>` (uppercase slug):

- `CLOUDFLARE_<ACCOUNT>_API_TOKEN`
- `CLOUDFLARE_<ACCOUNT>_ACCOUNT_ID`
- `CLOUDFLARE_<ACCOUNT>_DEFAULT_ZONE_ID`
- `CLOUDFLARE_<ACCOUNT>_DEFAULT_ZONE_NAME`
- `CLOUDFLARE_<ACCOUNT>_PROD_ZONE_ID`
- `CLOUDFLARE_<ACCOUNT>_STAGING_ZONE_ID`
- `CLOUDFLARE_<ACCOUNT>_DEV_ZONE_ID`

Global fallback keys:

- `CLOUDFLARE_API_TOKEN`
- `CLOUDFLARE_ACCOUNT_ID`
- `CLOUDFLARE_ZONE_ID`
- `CLOUDFLARE_API_BASE_URL`
- `CLOUDFLARE_DEFAULT_ACCOUNT`
- `CLOUDFLARE_DEFAULT_ENV`

Environment policy:
- `prod`, `staging`, `dev` are the supported context labels.
- `test` is intentionally not used as a standalone environment mode.

## Context + Auth + Diagnostics

```bash
si cloudflare auth status --account core
si cloudflare context list
si cloudflare context current
si cloudflare context use --account core --env prod --zone-id <zone>
si cloudflare doctor --account core
```

## Zone + DNS

```bash
si cloudflare zone list
si cloudflare zone get <zone_id>
si cloudflare zone create --param name=example.com --param account.id=<account_id>

si cloudflare dns list --zone-id <zone_id>
si cloudflare dns create --zone-id <zone_id> --param type=A --param name=api --param content=1.2.3.4 --param proxied=true
si cloudflare dns update --zone-id <zone_id> <record_id> --param ttl=120
si cloudflare dns delete --zone-id <zone_id> <record_id> --force
si cloudflare dns export --zone-id <zone_id>
si cloudflare dns import --zone-id <zone_id> --body '<BIND DATA>' --force
```

## TLS + Cache + Security

```bash
si cloudflare tls get --zone-id <zone_id> --setting min_tls_version
si cloudflare tls set --zone-id <zone_id> --setting min_tls_version --value 1.2
si cloudflare tls cert list --zone-id <zone_id>
si cloudflare tls origin-cert list

si cloudflare cache purge --zone-id <zone_id> --everything --force
si cloudflare cache settings get --zone-id <zone_id> --setting cache_level

si cloudflare waf list --zone-id <zone_id>
si cloudflare ruleset list --zone-id <zone_id>
si cloudflare firewall list --zone-id <zone_id>
si cloudflare ratelimit list --zone-id <zone_id>
```

## Workers + Pages

```bash
si cloudflare workers script list --account-id <account_id>
si cloudflare workers route list --zone-id <zone_id>
si cloudflare workers secret set --account-id <account_id> --script my-worker --name API_KEY --text '...'

si cloudflare pages project list --account-id <account_id>
si cloudflare pages deploy list --account-id <account_id> --project my-pages
si cloudflare pages deploy rollback --account-id <account_id> --project my-pages --deployment <id> --force
```

## Data Platform

```bash
si cloudflare r2 bucket list --account-id <account_id>
si cloudflare r2 object list --account-id <account_id> --bucket my-bucket

si cloudflare d1 db list --account-id <account_id>
si cloudflare d1 query --account-id <account_id> --db <db_id> --sql 'select 1'

si cloudflare kv namespace list --account-id <account_id>
si cloudflare kv key put --account-id <account_id> --namespace <ns_id> --key demo --value hello

si cloudflare queue list --account-id <account_id>
```

## Access + Tunnel + Load Balancer

```bash
si cloudflare access app list --account-id <account_id>
si cloudflare access policy list --account-id <account_id>

si cloudflare tunnel list --account-id <account_id>
si cloudflare tunnel token --account-id <account_id> --tunnel <id>

si cloudflare lb list --zone-id <zone_id>
si cloudflare lb pool list --account-id <account_id>
```

## Analytics + Logs + Reports

```bash
si cloudflare analytics http --zone-id <zone_id>
si cloudflare logs job list --zone-id <zone_id>
si cloudflare logs received --zone-id <zone_id>
si cloudflare report traffic-summary --zone-id <zone_id>
```

## Raw Escape Hatch

```bash
si cloudflare raw --method GET --path /zones
si cloudflare raw --method POST --path /zones/<zone_id>/purge_cache --body '{"purge_everything":true}'
```

## Error Reporting

On failures, `si cloudflare` surfaces:

- HTTP status
- request id (`CF-Ray` when available)
- Cloudflare error code/message
- structured `errors` payload when present
- redacted raw body for debugging
