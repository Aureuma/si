# Release Notes / Changelog Competitive Pricing (2026-02-13)

This doc captures major competitors and adjacent alternatives for ReleaseMind (RM), with emphasis on pricing models and plan limits (usage-based, per-seat, per-project, MAU/tracked-user, etc.). It also proposes RM pricing options based on the observed market bands.

Notes:
- Pricing changes frequently. This snapshot is "best effort" as of **2026-02-13**.
- Sources are linked per vendor. Prefer vendor pricing pages over third-party roundups.
- RM context: current codebase supports a `free` vs `core` usage gate (see `app/rm/src/lib/server/usage.ts`) and current Stripe pricing in the app is effectively "$19/mo or $99/yr" (`app/rm/src/routes/pricing/+page.svelte`).

## Market Patterns (What Competitors Charge For)

Common pricing models used by "changelog / release notes" products:
- Flat per workspace/org (often marketed as "unlimited")
- Per project/page (multiple products price by number of changelog pages/projects)
- MAU / tracked-user (common for in-app widgets and segmentation)
- Per seat (collaborators/admins)
- Usage-based (less common; e.g., per changelog generated)

Observed price bands:
- OSS / DIY: **$0** (but costs engineering time to set up and maintain)
- Solo/SMB changelog tools: **~$19 to $79/month**
- Business plans: **~$99 to $399/month**
- Enterprise: often **$300+/month** or "contact sales"

## Competitors (Direct + Adjacent)

### Developer-first / GitHub release automation (mostly OSS)

These are the "GitHub-native" alternatives to RM's draft generation workflow. The trade is: free licenses, but you own setup, templates, maintenance, and any AI/content workflow.

- Release Drafter (OSS)
  - Pricing: Free (open source)
  - Model: GitHub Action / config-driven draft releases
  - Source: `https://github.com/release-drafter/release-drafter`

- release-please (OSS)
  - Pricing: Free (open source)
  - Model: automated releases + changelog generation (Google)
  - Source: `https://github.com/googleapis/release-please`

- semantic-release (OSS)
  - Pricing: Free (open source)
  - Model: conventional-commit driven releases + changelog generation
  - Source: `https://github.com/semantic-release/semantic-release`

- Changesets (OSS)
  - Pricing: Free (open source)
  - Model: versioning + changelog workflow (popular in monorepos)
  - Source: `https://github.com/changesets/changesets`

### AI / automation for release notes (SaaS, smaller vendors)

- AutoChangelog
  - Pricing model: per month, multiple plans
  - Plans (published):
    - Free: **$0** (1 repo, 10 changelog entries/deployments per month, public pages, RSS)
    - Pro: **$14/month** or **$140/year** (1 repo, unlimited entries/deployments, priority support)
    - Team: **$29/month** or **$290/year** (unlimited repos, unlimited entries/deployments, team member access)
  - Sources:
    - `https://www.autochangelog.com/`
    - `https://autochangelog.com/terms`

- GitSaga (usage-based)
  - Pricing model: pay-as-you-go
  - Price: **$0.05 per changelog** (first 10 free)
  - Source: `https://gitsaga.io/pricing`

- Opensmith
  - Pricing model: per month (monthly/annual toggle)
  - Plans (published):
    - Free: **$0/month**
    - Pro: **$10/month**
  - Limits/features called out: AI release note generation (Pro), GitHub repo connection, webhooks, integrations (plan-dependent).
  - Source: `https://www.opensmith.io/pricing`

- ReleaseNotes.io
  - Pricing model: per "project"
  - Plans (published):
    - "Solo": **$39 per project/month**
    - "Team": **$99 per project/month**
    - "Enterprise": custom
  - Limits/features called out: projects, unlimited changelogs/updates, integrations (plan-dependent), etc.
  - Source: `https://releasenotes.io/pricing/`

### Customer-facing changelog / announcements tools (adjacent)

These are commonly evaluated as alternatives even when a buyer starts with a developer-centric "release notes" problem. They tend to price by MAU/tracked-users, pages/projects, or seats.

- Headway (HeadwayApp)
  - Pricing model: Free + flat monthly
  - Plans (published):
    - Free: $0, unlimited changelogs, "basic features"
    - Pro: **$29/month**
  - Feature gates called out: whitelabel, custom domain, integrations, team management, private changelog, scheduled publishing, etc.
  - Source: `https://headwayapp.co/`

- Noticeable
  - Pricing model: flat monthly, multiple tiers
  - Plans (published on pricing page):
    - Free: **$0/month**
    - Starter: **$29/month** (pricing page also shows an annual-billing equivalent of **$24** per month)
    - Growth: **$79/month** (annual equivalent shown as **$65** per month)
    - Business: **$159/month** (annual equivalent shown as **$132** per month)
    - Enterprise: **$399/month** (annual equivalent shown as **$333** per month)
  - Limits/features called out (examples): projects, collaborators, widgets.
  - Source: `https://noticeable.io/pricing`

- AnnounceKit
  - Pricing model: flat monthly, multiple tiers
  - Plans (published):
    - Essentials: **$79/month** (or **$89** billed monthly)
    - Growth: **$129/month** (or **$149** billed monthly)
    - Scale: **$339/month** (or **$399** billed monthly)
  - Limits/features called out: team members, domains/sites, announcements/month, email subscribers/month, etc.
  - Source: `https://announcekit.app/pricing`

- Beamer
  - Pricing model: MAU-based tiers
  - Plans (published):
    - Starter: **$49/month** annual (or **$59** billed monthly) for 5,000 MAU
    - Pro: **$99/month** annual (or **$119** billed monthly) for 10,000 MAU
    - Scale: **$249/month** annual (or **$299** billed monthly) for 50,000 MAU
    - Enterprise: contact
  - Source: `https://www.getbeamer.com/pricing`

- LaunchNotes
  - Pricing model: per month; higher-end "product communication" suite
  - Plans (published):
    - Growth: **$249/month** (paid yearly) or **$299/month** billed monthly
    - Scale: contact sales
  - Limits/features called out (Growth): users, pages, core communications features, integrations (varies by plan)
  - Source: `https://launchnotes.com/pricing`

- Sleekplan
  - Pricing model: multiple tiers (monthly/annual), includes free plan
  - Plans (published; annual-billing pricing shown on page):
    - Indie: **$0** (1 team seat; 500K pageviews/month shown in compare table)
    - Starter: **$13/mo billed annually** (3 team seats; unlimited pageviews)
    - Business: **$38/mo billed annually** (10 team seats; unlimited pageviews)
    - Enterprise: custom
  - Usage limits called out: announcements/month, popups/month, email credits/month, storage capacity.
  - Source: `https://sleekplan.com/pricing`

- Frill (Roadmap + feedback + changelog)
  - Pricing model: flat monthly tiers + add-ons
  - Plans (published):
    - Startup: **$25/month** (50 ideas, 1 survey)
    - Business: **$49/month** (unlimited ideas, 3 surveys)
    - Growth: **$149/month** (privacy/surveys/white labeling included)
    - Enterprise: from **$349/month**
  - Add-ons called out: privacy (+$25/mo), surveys (+$25/mo), white labeling (+$100/mo).
  - Source: `https://frill.co/pricing`

- Canny (Feedback + roadmap + changelog; tracked-users model)
  - Pricing model: tracked users + feature tiers
  - Plans (published on pricing page; billed yearly):
    - Free: **$0** (25 tracked users; 5 managers)
    - Core: **$19/mo billed yearly** (starts at 100+ tracked users)
    - Pro: **$79/mo billed yearly** (starts at 100+ tracked users; PM integrations + advanced privacy)
    - Business: custom (5,000+ tracked users; SSO + CRM integrations)
  - Source: `https://canny.io/pricing`

- changes.page
  - Pricing model: per page/month + add-ons
  - Published price: **$2/page/month**
  - Add-ons called out: email notifications priced at **$0.01/email notification**
  - Source: `https://changes.page/pricing`

- Openchangelog
  - Pricing model: flat monthly (EUR)
  - Published plans:
    - Free: **€0/month** (unlimited changelogs; GitHub integration; RSS; subdomain)
    - Pro: **€14/month** (custom domain + SSL, whitelabel, password protection, analytics, full-text search, unlimited team seats)
  - Source: `https://openchangelog.com/pricing/`

- ChangelogHQ
  - Pricing model: flat monthly with usage caps
  - Plans (published):
    - Solo: **$9/mo** (10 projects, 2 users, 300 monthly commits)
    - Startup: **$29/mo** (25 projects, 10 users, 800 monthly commits)
    - Business: **$59/mo** (50 projects, 30 users, 1,200 monthly commits)
    - Corporate: **$79/mo** (100 projects, 50 users, 2,000 monthly commits)
  - Source: `https://changeloghq.com/pricing`

- ProductLift (feedback/roadmap/changelog platform; adjacent)
  - Pricing model: per admin seat
  - Published price: **$14/month per admin (billed yearly)**; end-users unlimited
  - Source: `https://www.productlift.dev/pricing`

## "GitHub Deploy" / Marketplace Notes

The GitHub Marketplace has many "release notes" / "changelog" solutions, but a lot of them are:
- free OSS projects (e.g., Release Drafter),
- Actions with no pricing page,
- or SaaS that route you to their own billing.

Example Marketplace listing (Action):
- AI Release Notes
  - Model: GitHub Action that uses AI to generate release notes
  - Pricing: not clearly advertised on the listing page (often effectively "free" except your LLM/API costs, depending on how it works)
  - Source: `https://github.com/marketplace/actions/ai-release-notes`

When comparing RM to Marketplace options, the key differentiation levers that show up repeatedly are:
- quality of the generated narrative (not just a list of PRs),
- reviewer workflow (approval loop),
- multi-repo / org scale,
- and the maintenance burden of configs/scripts.

## RM Pricing Recommendations (Based on Competition + RM’s Current Shape)

RM’s current in-app Stripe pricing (dev/sandbox) is effectively:
- **$19/month**
- **$99/year** (displayed as ~$9/mo; very steep discount vs monthly)

Given the competitive set above:
- $19/month is at the low end of "Pro" pricing, but not out-of-market.
- $99/year is aggressively discounted compared to almost every comparable SaaS plan; it risks anchoring RM as "cheap" for teams.

### Option A: Keep One Paid Plan (Highest Simplicity, Strong Conversion)

Keep a Free plan for funnel (already exists in usage limits). Keep exactly one paid "Core" plan to reduce decision friction.

Suggested pricing:
- Core: **$29/month** and **$290/year**

Why:
- Anchors with Headway Pro ($29), Noticeable Starter ($29), AutoChangelog Team ($29).
- Still undercuts the "product comms" suite pricing band (AnnounceKit/Beamer/LaunchNotes).
- Annual discount becomes conventional (roughly 2 months free), which is easier to defend later.

Suggested RM "Core" usage fences (align to current code; adjust copy accordingly):
- active repos: keep 25 (soft)
- drafts: keep 200/month
- AI edits: keep 200/month

### Option B: Split Solo vs Team (Better Price Discrimination, More Work)

If you want to serve both ICPs (solo/OSS and teams) without underpricing teams, add a paid Solo tier below Team.

Suggested pricing (starting points):
- Solo: **$12/month** and **$120/year**
- Team: **$39/month** and **$390/year**
- Business/Org: **$99/month** and **$990/year** (optional; only if you can justify with limits/support/admin)

Suggested usage fences (uses RM’s existing meter concepts):
- Solo:
  - active repos: 5 (hard or soft)
  - drafts: 50-100/month
  - AI edits: 100/month
  - private repos: allowed
- Team:
  - active repos: 25 (soft)
  - drafts: 200-500/month
  - AI edits: 500/month
- Business/Org:
  - active repos: 100 (soft)
  - drafts: 1000+/month
  - AI edits: 2000+/month
  - include priority support + onboarding

### If You Intentionally Want Higher Pricing

If RM is positioned as a "serious engineering workflow tool" (not a generic changelog widget), price can be a positive signal for teams. A higher price tends to work best when paired with:
- clear team outcomes (time saved per release, consistency, fewer mistakes),
- multi-repo narrative quality (PR/tag/diff context),
- and admin/billing features teams expect.

A realistic "higher pricing" test band for Core/Team (without going enterprise) is:
- **$49 to $79/month** with **$490 to $790/year**

## Action Items (If You Want To Implement Changes)

1. Update pricing copy on `/` and `/pricing` to match whatever usage policy you actually enforce (especially "Unlimited repos" vs `active_repos`).
2. Standardize annual discount messaging (it is currently hard-coded as "6+ months free" in the UI).
3. If adding tiers: expand Stripe config from 2 prices (monthly/annual) to multiple products/prices and map Stripe subscription -> plan key in `resolveUsagePlan()`.
