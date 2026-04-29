# Cloudflare Command Guide (`si orbit cloudflare`)

![Cloudflare](/docs/images/integrations/cloudflare.svg)

`si orbit cloudflare` is the Cloudflare bridge for account context, operational workflows, and raw API access.

Related:
- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [Providers](./PROVIDERS)

Auth policy:
- API token only.
- Credentials should be resolved through configured Fort bindings or compatible env keys; use `si fort` for runtime secret access.
- Settings should store env references/pointers, not raw secrets.

## Credential Keys (Fort/Env-Compatible)

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
si orbit cloudflare auth status --account core
si orbit cloudflare status --account core
si orbit cloudflare smoke --account core
si orbit cloudflare context list
si orbit cloudflare context current
si orbit cloudflare context use --account core --env prod --zone-id <zone>
si orbit cloudflare doctor --account core
```

## Zone + DNS

```bash
si orbit cloudflare zone list
si orbit cloudflare zone get <zone_id>
si orbit cloudflare zone create --param name=example.com --param account.id=<account_id>

si orbit cloudflare dns list --zone-id <zone_id>
si orbit cloudflare dns create --zone-id <zone_id> --param type=A --param name=api --param content=1.2.3.4 --param proxied=true
si orbit cloudflare dns update --zone-id <zone_id> <record_id> --param ttl=120
si orbit cloudflare dns delete --zone-id <zone_id> <record_id> --force
si orbit cloudflare dns export --zone-id <zone_id>
si orbit cloudflare dns import --zone-id <zone_id> --body '<BIND DATA>' --force
```

## TLS + Cache + Security

```bash
si orbit cloudflare tls get --zone-id <zone_id> --setting min_tls_version
si orbit cloudflare tls set --zone-id <zone_id> --setting min_tls_version --value 1.2
si orbit cloudflare tls get --zone-id <zone_id> --setting ssl
si orbit cloudflare tls cert list --zone-id <zone_id>
si orbit cloudflare cert list --zone-id <zone_id>
si orbit cloudflare tls origin list
si orbit cloudflare origin list

si orbit cloudflare cache purge --zone-id <zone_id> --everything --force
si orbit cloudflare cache settings get --zone-id <zone_id> --setting cache_level
si orbit cloudflare cache tiered get --zone-id <zone_id>
si orbit cloudflare cache tiered set --zone-id <zone_id> --value on

si orbit cloudflare waf list --zone-id <zone_id>
si orbit cloudflare ruleset list --zone-id <zone_id>
si orbit cloudflare firewall list --zone-id <zone_id>
si orbit cloudflare limits list --zone-id <zone_id>
```

## Workers + Pages

```bash
si orbit cloudflare workers script list --account-id <account_id>
si orbit cloudflare workers route list --zone-id <zone_id>
si orbit cloudflare workers secret set --account-id <account_id> --script my-worker --name API_KEY --text '...'

si orbit cloudflare pages project list --account-id <account_id>
si orbit cloudflare pages deploy list --account-id <account_id> --project my-pages
si orbit cloudflare pages deploy rollback --account-id <account_id> --project my-pages --deployment <id> --force
```

## Data Platform

```bash
si orbit cloudflare r2 bucket list --account-id <account_id>
si orbit cloudflare r2 object list --account-id <account_id> --bucket my-bucket

si orbit cloudflare d1 db list --account-id <account_id>
si orbit cloudflare d1 query --account-id <account_id> --db <db_id> --sql 'select 1'

si orbit cloudflare kv namespace list --account-id <account_id>
si orbit cloudflare kv key put --account-id <account_id> --namespace <ns_id> --key demo --value hello

si orbit cloudflare queue list --account-id <account_id>
```

## Access + Tunnel + Load Balancer

```bash
si orbit cloudflare access app list --account-id <account_id>
si orbit cloudflare access policy list --account-id <account_id>

si orbit cloudflare tunnel list --account-id <account_id>
si orbit cloudflare tunnels list --account-id <account_id>
si orbit cloudflare tunnel token --account-id <account_id> --tunnel <id>

si orbit cloudflare lb list --zone-id <zone_id>
si orbit cloudflare lb pool list --account-id <account_id>
```

## Email + Tokens

```bash
si orbit cloudflare email rule list --zone-id <zone_id>
si orbit cloudflare email rule create --zone-id <zone_id> --param name=forward-inbox --param enabled=true
si orbit cloudflare email address list --account-id <account_id>
si orbit cloudflare email settings get --zone-id <zone_id>
si orbit cloudflare email settings enable --zone-id <zone_id> --force

si orbit cloudflare token verify
si orbit cloudflare token list
si orbit cloudflare token permissions
```

## Analytics + Logs + Reports

```bash
si orbit cloudflare analytics http --zone-id <zone_id>
si orbit cloudflare logs job list --zone-id <zone_id>
si orbit cloudflare logs received --zone-id <zone_id>
si orbit cloudflare report traffic-summary --zone-id <zone_id>
```

## Raw Escape Hatch

```bash
si orbit cloudflare raw --method GET --path /zones
si orbit cloudflare api --method GET --path /zones
si orbit cloudflare raw --method POST --path /zones/<zone_id>/purge_cache --body '{"purge_everything":true}'
```

For routine state audits, prefer direct commands over `raw`:

```bash
si orbit cloudflare dns list --zone-id <zone_id>
si orbit cloudflare r2 bucket list --account-id <account_id>
si orbit cloudflare tls origin list
si orbit cloudflare tls get --zone-id <zone_id> --setting ssl
si orbit cloudflare cache tiered get --zone-id <zone_id>
```

## Error Reporting

On failures, `si orbit cloudflare` surfaces:

- HTTP status
- request id (`CF-Ray` when available)
- Cloudflare error code/message
- structured `errors` payload when present
- redacted raw body for debugging
