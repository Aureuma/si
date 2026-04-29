---
title: Integrations Overview
description: Complete map of SI integration families, capabilities, and command entry points.
---

# Integrations Overview

![SI Integrations](/docs/images/integrations/integrations-overview.svg)

This page is the canonical map of SI integration families.

## Command families

| Integration | Primary command | Guide |
| --- | --- | --- |
| GitHub | `si orbit github ...` | [GitHub](./GITHUB) |
| Cloudflare | `si orbit cloudflare ...` | [Cloudflare](./CLOUDFLARE) |
| Stripe | `si orbit stripe ...` | [Stripe](./STRIPE) |
| Google Cloud (Gemini/Vertex/Service Usage) | `si orbit gcp ...` | [GCP](./GCP) |
| Google Places | `si orbit google places ...` | [Google Places](./GOOGLE_PLACES) |
| Google Play | `si orbit google play ...` | [Google Play](./GOOGLE_PLAY) |
| YouTube | `si orbit google youtube ...` | [Google YouTube](./GOOGLE_YOUTUBE) |
| Social (Facebook/Instagram/X/LinkedIn/Reddit) | `si social ...` | [Social](./SOCIAL) |
| AWS | `si orbit aws ...` | [AWS](./AWS) |
| OpenAI | `si orbit openai ...` | [OpenAI](./OPENAI) |
| Oracle Cloud Infrastructure | `si orbit oci ...` | [OCI](./OCI) |
| WorkOS | `si orbit workos ...` | [WorkOS](./WORKOS) |
| Apple App Store | `si orbit apple store ...` | [Apple App Store](./APPLE_APPSTORE) |
| ReleaseMind runbooks | `si orbit releasemind runbook ...` | [Release Runbook](./RELEASE_RUNBOOK) |
| Publish bridge | `si publish ...` | [Publish](./PUBLISH) |
| Provider orbit inventory | `si orbit list` | [Providers](./PROVIDERS) |
| Surf browser runtime | `si surf ...` | [Browser Runtime](./BROWSER) |

## Integration capability matrix

| Integration | Auth diagnostics | Context selection | Structured resources | Raw API mode | Doctor/health path |
| --- | --- | --- | --- | --- | --- |
| GitHub | Yes | Yes | Yes | Yes | `si orbit github doctor` |
| Cloudflare | Yes | Yes | Yes | Yes | `si orbit cloudflare doctor` |
| Stripe | Yes | Yes | Yes | Yes | `si orbit stripe auth status` |
| GCP | Yes | Yes | Yes | Yes | `si orbit gcp doctor` |
| Google Places | Yes | via `si orbit google` | Yes | Yes | provider health + auth |
| Google Play | Yes | via `si orbit google` | Yes | Yes | auth + release checks |
| YouTube | Yes | via `si orbit google` | Yes | Yes | auth + upload checks |
| Social | Yes | Yes | Yes | Yes | platform auth status |
| AWS | Yes | Yes | Yes | Yes | `si orbit aws doctor` |
| OpenAI | Yes | Yes | Yes | Yes | `si orbit openai doctor` |
| OCI | Yes | Yes | Yes | Yes | `si orbit oci doctor` |
| WorkOS | Yes | Yes | Yes | Yes | `si orbit workos doctor` |
| Apple App Store | Yes | Yes | Yes | Yes | auth + API checks |
| Publish | N/A (depends on target) | target-specific | Curated publishing flows | N/A | pre-publish checks |
| Provider inventory | N/A | N/A | Aggregated capability metadata | N/A | `si orbit list` |
| Surf runtime | runtime status | runtime profile dir | browser automation runtime, optional MCP-compatible endpoint | N/A | `si surf status` |

## Integration visuals

| Integration | Visual |
| --- | --- |
| GitHub | ![GitHub](/docs/images/integrations/github.svg) |
| Cloudflare | ![Cloudflare](/docs/images/integrations/cloudflare.svg) |
| Stripe | ![Stripe](/docs/images/integrations/stripe.svg) |
| Google Cloud | ![GCP](/docs/images/integrations/gcp.svg) |
| AWS | ![AWS](/docs/images/integrations/aws.svg) |
| OpenAI | ![OpenAI](/docs/images/integrations/openai.svg) |
| OCI | ![OCI](/docs/images/integrations/oci.svg) |
| WorkOS | ![WorkOS](/docs/images/integrations/workos.svg) |
| Apple App Store | ![Apple App Store](/docs/images/integrations/apple-appstore.svg) |
| Publish | ![Publish](/docs/images/integrations/publish.svg) |
| Providers | ![Providers](/docs/images/integrations/providers.svg) |
| Surf runtime | ![Browser](/docs/images/integrations/browser.svg) |

## Operator checklist before production writes

1. Confirm credentials with integration-specific auth status command.
2. Confirm context/account/environment target.
3. Run integration doctor/health command where available.
4. Use `--json` mode for auditable outputs in automation.
5. For host/admin flows, prefer `si vault run -- <cmd>` when injecting secrets. For SI runtime workers, use `si fort ...`.

## Related pages

- [CLI Reference](./CLI_REFERENCE)
- [Settings](./SETTINGS)
- [Vault](./VAULT)
- [Documentation Style Guide](./DOCS_STYLE_GUIDE)
