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
| GitHub | `si github ...` | [GitHub](./GITHUB) |
| Cloudflare | `si cloudflare ...` | [Cloudflare](./CLOUDFLARE) |
| Stripe | `si stripe ...` | [Stripe](./STRIPE) |
| Google Cloud (Gemini/Vertex/Service Usage) | `si gcp ...` | [GCP](./GCP) |
| Google Places | `si google places ...` | [Google Places](./GOOGLE_PLACES) |
| Google Play | `si google play ...` | [Google Play](./GOOGLE_PLAY) |
| YouTube | `si google youtube ...` | [Google YouTube](./GOOGLE_YOUTUBE) |
| Social (Facebook/Instagram/X/LinkedIn/Reddit) | `si social ...` | [Social](./SOCIAL) |
| AWS | `si aws ...` | [AWS](./AWS) |
| OpenAI | `si openai ...` | [OpenAI](./OPENAI) |
| Oracle Cloud Infrastructure | `si oci ...` | [OCI](./OCI) |
| WorkOS | `si workos ...` | [WorkOS](./WORKOS) |
| Apple App Store | `si apple appstore ...` | [Apple App Store](./APPLE_APPSTORE) |
| Publish bridge | `si publish ...` | [Publish](./PUBLISH) |
| Provider meta-health | `si providers ...` | [Providers](./PROVIDERS) |
| Browser MCP runtime | `si browser ...` | [Browser Runtime](./BROWSER) |
| Plugin marketplace | `si plugins ...` | [Plugin Marketplace](./PLUGIN_MARKETPLACE) |

## Integration capability matrix

| Integration | Auth diagnostics | Context selection | Structured resources | Raw API mode | Doctor/health path |
| --- | --- | --- | --- | --- | --- |
| GitHub | Yes | Yes | Yes | Yes | `si github doctor` |
| Cloudflare | Yes | Yes | Yes | Yes | `si cloudflare doctor` |
| Stripe | Yes | Yes | Yes | Yes | `si stripe auth status` |
| GCP | Yes | Yes | Yes | Yes | `si gcp doctor` |
| Google Places | Yes | via `si google` | Yes | Yes | provider health + auth |
| Google Play | Yes | via `si google` | Yes | Yes | auth + release checks |
| YouTube | Yes | via `si google` | Yes | Yes | auth + upload checks |
| Social | Yes | Yes | Yes | Yes | platform auth status |
| AWS | Yes | Yes | Yes | Yes | `si aws doctor` |
| OpenAI | Yes | Yes | Yes | Yes | `si openai doctor` |
| OCI | Yes | Yes | Yes | Yes | `si oci doctor` |
| WorkOS | Yes | Yes | Yes | Yes | `si workos doctor` |
| Apple App Store | Yes | Yes | Yes | Yes | auth + API checks |
| Publish | N/A (depends on target) | target-specific | Curated publishing flows | N/A | pre-publish checks |
| Providers | N/A | N/A | Aggregated telemetry | N/A | `si providers health` |
| Browser MCP | runtime status | runtime profile dir | browser actions through MCP | N/A | `si browser status` |
| Plugin marketplace | policy + install diagnostics | N/A | catalog/install records | N/A | `si plugins doctor` |

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
| Browser MCP | ![Browser](/docs/images/integrations/browser.svg) |
| Plugin Marketplace | ![Plugin Marketplace](/docs/images/integrations/plugins.svg) |

## Operator checklist before production writes

1. Confirm credentials with integration-specific auth status command.
2. Confirm context/account/environment target.
3. Run integration doctor/health command where available.
4. Use `--json` mode for auditable outputs in automation.
5. Prefer `si vault run -- <cmd>` when injecting secrets.

## Related pages

- [CLI Reference](./CLI_REFERENCE)
- [Settings](./SETTINGS)
- [Vault](./VAULT)
- [Plugin Marketplace](./PLUGIN_MARKETPLACE)
- [Documentation Style Guide](./DOCS_STYLE_GUIDE)
