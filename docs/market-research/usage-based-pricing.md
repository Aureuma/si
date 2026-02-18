# Usage-Based Pricing Research (2026-01-22)

Collected for ReleaseMind usage-based billing design. Pricing and limits can
change; verify before launch.

## 1) LaunchDarkly
- Sources:
  - https://launchdarkly.com/pricing/
- Model: hybrid usage pricing driven by **service connections** and **client-side
  MAU**, plus add-on observability usage (sessions, errors, logs, traces).
- Pricing highlights (public list):
  - Developer: Free.
  - Foundation: **$12 per service connection/mo** and **$10 per 1k client-side
    MAU/mo**.
  - Observability add-ons: session replays start at **$3.50/1k sessions**, errors
    at **$0.30/1k**, logs/traces at **$1.50/1M**.
- Usage metrics: service connections, client-side MAU, experimentation MAU,
  session replays, errors, logs, traces.
- Takeaways:
  - Clearly separates core usage (MAU, service connections) from metered add-ons.
  - MAU-based pricing aligns spend with end-user scale.

## 2) CircleCI
- Sources:
  - https://circleci.com/pricing/
- Model: **credits** consumed by compute time/resources; optional overage credit
  purchases.
- Pricing highlights:
  - Free: **6,000 build minutes**, **5 active users/mo**.
  - Performance: **$15/mo** with **30,000 credits**, additional credits
    **$15 per 25,000 credits**.
- Usage metrics: credits per minute (resource-class based), add-ons, network/
  storage.
- Takeaways:
  - Credit model abstracts multiple resource costs into one meter.
  - Paid credits roll over (helpful for spiky usage).

## 3) GitHub Copilot
- Sources:
  - https://github.com/features/copilot/plans
  - https://docs.github.com/en/copilot/concepts/copilot-billing/about-billing-for-individual-copilot-plans
- Model: seat plans with **monthly premium-request quotas**; paid overage per
  request.
- Pricing highlights:
  - Free: **50 premium requests/month**.
  - Pro: **$10/mo** with **300 premium requests/month**.
  - Pro+: **$39/mo** with **1,500 premium requests/month**.
  - Overages: **$0.04 per premium request**.
- Usage metrics: premium requests, chat/agent requests, completions.
- Takeaways:
  - LLM usage is metered per “premium request.”
  - Overage pricing is simple and transparent per request.

## 4) Codecov
- Sources:
  - https://about.codecov.io/pricing/
- Model: per-user pricing with **upload limits** on lower tiers.
- Pricing highlights:
  - Team: **$5/user/mo**.
  - Pro: **$12/user/mo**.
  - Private repo uploads: **250** (Developer), **2,500** (Team), **Unlimited**
    (Pro).
- Usage metrics: private repo uploads, seats.
- Takeaways:
  - Upload caps keep ingestion costs bounded.
  - Users and uploads are easy to explain to orgs.

## 5) Coveralls
- Sources:
  - https://coveralls.io/pricing
- Model: tiered by **private repo count**; unlimited users; optional upload
  add-on.
- Pricing highlights:
  - Starter: **$10/mo** for 1 private repo.
  - Org: **$50/mo** for 10 private repos.
  - Unlimited: **$400/mo**.
  - Add-on: **$10/mo** for **+3,000 uploads**.
- Usage metrics: private repo count, uploads/month.
- Takeaways:
  - Repo count is a simple, predictable usage unit.
  - Upload add-on monetizes bursts without complex metering.

## 6) SonarQube Cloud (SonarCloud)
- Sources:
  - https://www.sonarsource.com/plans-and-pricing/sonarcloud/
  - https://docs.sonarsource.com/sonarqube-cloud/administering-sonarcloud/managing-subscription/subscription-plans
- Model: **LOC-based** pricing for private projects.
- Pricing highlights:
  - Free: **up to 50k LOC**.
  - Team: starts around **€30/mo** for **100k LOC**, with LOC bands up to 1.9M.
- Usage metrics: lines of code (private projects).
- Takeaways:
  - LOC is a predictable metric for static analysis cost.
  - Paid tiers scale on LOC bands rather than seats.

## 7) Snyk
- Sources:
  - https://snyk.io/plans/
  - https://docs.snyk.io/snyk-admin/managing-settings/usage-page-details
- Model: per **contributing developer**, with **test limits** per product on
  free tiers.
- Pricing highlights:
  - Team starts at **$25/month per contributing developer**.
  - Free tier test limits by product (e.g., Open Source, Code, IaC, Container).
- Usage metrics: test counts per product; contributing developers (commits to
  private repos in last 90 days).
- Takeaways:
  - “Contributing developer” is a practical seat definition.
  - Tests-per-month maps to actual compute usage.

## 8) DeepSource
- Sources:
  - https://deepsource.com/pricing
- Model: per-seat pricing with **usage-limited features** on lower tiers.
- Pricing highlights:
  - Starter: **$8/seat/mo**.
  - Business: **$24/seat/mo**.
  - Free tier: limited runs, 1 private repo, 3 team members.
- Usage metrics: analysis runs; formatter/autofix usage limits.
- Takeaways:
  - Feature-specific usage caps keep limits easy to message.

## 9) Codacy
- Sources:
  - https://www.codacy.com/pricing
- Model: per-dev pricing; seats tied to Git contributors on private repos.
- Pricing highlights:
  - Team: **$18/dev/mo billed yearly** (or **$21** monthly).
  - Developer plan: **$0**.
- Usage metrics: seats (commit authors) and private repo/project caps by plan.
- Takeaways:
  - Seat definition tied to commit authors is consistent for GitHub apps.

## 10) Code Climate Quality
- Sources:
  - https://marketingapi.codeclimate.com/quality/pricing
- Model: per-seat pricing; unlimited repos.
- Pricing highlights:
  - Team: **$16.67/seat/mo billed annually** (**$20** monthly).
  - Startup: **$0** up to 4 seats.
- Usage metrics: seats; analysis limited to organization members.
- Takeaways:
  - Unlimited repos reduces friction; seats meter cost.

## 11) LinearB
- Sources:
  - https://linearb.io/pricing
  - https://linearb.io/how-credits-work
- Model: **seat + credits** hybrid; credits consumed by automated PRs.
- Pricing highlights:
  - Essentials: **$29/mo per contributor**, **1,000 credits**.
  - Enterprise: **$59/mo per contributor**, **1,500 credits**.
  - One PR automation consumes **100 credits**.
- Usage metrics: contributors + credits.
- Takeaways:
  - Credits bundle AI-heavy operations without per-action billing complexity.

## 12) Mergify
- Sources:
  - https://mergify.com/pricing
  - https://docs.mergify.com/billing/
- Model: per-seat pricing with **user caps**; focuses on **active contributors**.
- Pricing highlights:
  - Free: **$0** up to 5 users on private repos.
  - Max: **$21/seat/mo** up to 100 users.
  - Enterprise: custom.
- Usage metrics: active contributors/users.
- Takeaways:
  - “Only active contributors are billed” reduces seat anxiety.
