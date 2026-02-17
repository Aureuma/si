# Ticket: Monetization Model (Solo-dev / Solopreneur ICP)

Date: 2026-02-17
Owner: Unassigned
Status: Planned
Priority: High

## 1. Objective

Define a monetization model that is:

1. Simple for users to understand.
2. Simple for engineering to implement.
3. Simple for operations/support to maintain.

## 2. ICP

Primary paid ICP:

1. Solo developers.
2. Solopreneurs and micro-startups.

Key buying behavior:

1. Wants predictable monthly cost.
2. Wants low operational burden.
3. Dislikes complex usage-based surprises.

## 3. Product Packaging

1. Self-hosted OSS:
- Free software.
- User manages infrastructure.

2. Managed cloud:
- Paid subscription plans.
- Controlled by entitlements, not granular metered overages.

## 4. Pricing and Entitlement Principle

Primary billable metric:

1. Active app slots.
- One managed app with at least one active deployment = one slot.
- Compose internals do not change slot count.

Secondary limits:

1. Managed target count.
2. Log retention days.
3. Concurrent deploy operations.

Policy:

1. Flat monthly plans.
2. No usage overages in initial monetized release.
3. Optional annual billing later.

## 5. Recommended Initial Plan Shape

Use these as launch placeholders and validate during beta:

| Plan | Active App Slots | Managed Targets | Log Retention | Concurrent Deploys | Notes |
| --- | --- | --- | --- | --- | --- |
| `launch` | 3 | 1 | 7 days | 1 | solo starter |
| `build` | 10 | 3 | 30 days | 2 | growing solo app portfolio |
| `grow` | 30 | 10 | 90 days | 4 | advanced solo/pro operator |

## 6. Billing Lifecycle

States:

1. `active`
2. `grace`
3. `restricted`

Grace timeout (MVP):

1. Fixed at 7 calendar days from first payment failure event.

Transitions:

1. `active -> grace` on `invoice.payment_failed`.
2. `grace -> active` on `invoice.paid` or `customer.subscription.updated` with active status.
3. `grace -> restricted` after 7-day grace timeout without successful recovery.
4. `active -> restricted` on `customer.subscription.deleted`.
5. `restricted -> active` on successful recovery checkout/payment webhook.

Behavior:

1. `active`: all operations allowed within entitlements.
2. `grace`: allow read operations, billing recovery actions, and rollback to last known-good release.
3. `grace`: block mutating growth operations (`deploy`, `scale`, `app init/remove`, `target add/remove`, `secret set/unset`).
4. `restricted`: allow read + billing recovery actions only; block all mutating operations until billing is restored.

## 7. Stripe Scope (Minimal)

Implement only:

1. Checkout session create.
2. Customer billing portal link.
3. Webhook handling for:
- `checkout.session.completed`
- `customer.subscription.updated`
- `customer.subscription.deleted`
- `invoice.paid`
- `invoice.payment_failed`

Out of scope initially:

1. Complex metered billing.
2. Coupon/promo orchestration.
3. Multi-currency edge optimization.

## 8. CLI UX Contract

Required commands:

1. `si paas cloud plan`
2. `si paas cloud usage`
3. `si paas cloud billing portal`

Behavior:

1. Always show current plan, limits, and remaining headroom.
2. On entitlement failure, output clear upgrade path.
3. No ambiguous billing states.

## 9. Engineering Acceptance Criteria

1. Entitlements are enforced at deploy boundaries.
2. Plan state changes propagate within one billing webhook cycle.
3. Billing failures move tenant through `active -> grace -> restricted` predictably on timeout.
4. CLI clearly reports why operations are blocked and which state policy is active.
5. End-to-end tests cover checkout -> active -> payment failure -> grace -> restricted -> recovery.
6. Operation policy tests verify state gating for deploy/scale/app/target/secret mutations.

## 10. Risks and Mitigation

1. Risk: Too many plan variants confuse ICP.
Mitigation: launch with minimal tiers and simple slot-based limits.

2. Risk: Overly strict grace policy causes churn.
Mitigation: clear warning windows and easy recovery path.

3. Risk: Under-priced plans harm sustainability.
Mitigation: quarterly review of slot consumption and support load.

## 11. Implementation Tasks

| ID | Task | Status | Owner | Notes |
| --- | --- | --- | --- | --- |
| MON-01 | Finalize plan matrix and internal pricing assumptions | Not Started | Unassigned | Linked WS: WS08-01, WS08-07 |
| MON-02 | Implement entitlement policy engine (active app slots + secondary limits) | Not Started | Unassigned | Linked WS: WS08-02, WS08-03 |
| MON-03 | Implement Stripe checkout and portal handlers | Not Started | Unassigned | Linked WS: WS08-04 |
| MON-04 | Implement webhook ingestion and billing state transitions | Not Started | Unassigned | Linked WS: WS08-04, WS08-05 |
| MON-05 | Implement CLI usage/plan visibility commands | Not Started | Unassigned | Linked WS: WS08-06 |
| MON-06 | Add end-to-end billing lifecycle tests | Not Started | Unassigned | Linked WS: WS08-05 |
| MON-07 | Publish public pricing and billing FAQ copy | Not Started | Unassigned | Linked WS: WS08-09 |
