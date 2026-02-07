# Stripe Command Guide (`si stripe`)

`si` includes a first-class Stripe bridge with account context, CRUD helpers, reporting, raw endpoint access, and live-to-sandbox sync.

## Environment Policy
- Supported CLI environments: `live`, `sandbox`
- `test` is intentionally rejected as a standalone CLI mode

## Context & Auth
```bash
si stripe auth status
si stripe auth status --account core --env sandbox

si stripe context list
si stripe context current
si stripe context use --account core --env sandbox
```

## Object CRUD
```bash
si stripe object list product --limit 50
si stripe object get product prod_123
si stripe object create product --param name=Starter --param active=true
si stripe object update product prod_123 --param metadata[tier]=pro
si stripe object delete customer cus_123 --force
```

Supported object registry includes:
- `product`, `price`, `coupon`, `promotion_code`, `tax_rate`, `shipping_rate`
- `customer`, `payment_intent`, `subscription`, `invoice`, `refund`, `charge`
- `account`, `organization`, `balance_transaction`, `payout`, `payment_method`

If an object/operation is unsupported in the curated registry, use `si stripe raw`.

## Raw Endpoint Access
```bash
si stripe raw --method GET --path /v1/balance
si stripe raw --method POST --path /v1/products --param name=Starter
```

## Reporting Presets
```bash
si stripe report revenue-summary
si stripe report payment-intent-status --from 2026-02-01T00:00:00Z --to 2026-02-07T00:00:00Z
si stripe report subscription-churn
si stripe report balance-overview
```

## Live-to-Sandbox Sync
```bash
si stripe sync live-to-sandbox plan --account core
si stripe sync live-to-sandbox apply --account core --dry-run
si stripe sync live-to-sandbox apply --account core --only products --only prices --force
```

Supported sync families:
- `products`, `prices`, `coupons`, `promotion_codes`, `tax_rates`, `shipping_rates`

Behavior:
- `plan`: detects create/update/archive drift from live to sandbox
- `apply`: applies create/update/archive actions in sandbox
- `--dry-run`: computes actions without mutation

## Error Visibility
On API failures, `si stripe` surfaces:
- HTTP status
- Stripe `type`, `code`, `decline_code`, `param`, `message`
- `request_id`, `doc_url`, `request_log_url`
- raw payload (with secret redaction)

## Observability
- Bridge events are written as JSON lines to `~/.si/logs/stripe.log` by default.
- Override with `stripe.log_file` in settings or `SI_STRIPE_LOG_FILE`.
- Logged events include context (`account`, `environment`), request path/method, status, request ID, and duration.
