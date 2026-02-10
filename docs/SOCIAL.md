# Social Integrations (`si social`)

`si social` provides unified command families for:
- Facebook Graph API
- Instagram Graph API
- X (Twitter API v2)
- LinkedIn API

## Command Surface
```bash
si social facebook <auth|context|doctor|profile|page|post|comment|insights|raw|report>
si social instagram <auth|context|doctor|profile|media|comment|insights|raw|report>
si social x <auth|context|doctor|user|tweet|search|raw|report>
si social linkedin <auth|context|doctor|profile|organization|post|raw|report>
```

Common platform commands:
- `auth status`: verifies credentials against a lightweight probe endpoint.
- `context list|current|use`: list/select defaults in `~/.si/settings.toml`.
- `doctor`: runs health checks for auth + runtime config.
- `raw`: direct API calls (`--method`, `--path`, `--param`, `--body`).
- `report usage|errors`: local log summaries from `~/.si/logs/social-<platform>.log`.
- `--auth-style none` allows unauthenticated/public endpoint probes.

## Credential Resolution
Per-account prefix:
- `SOCIAL_<ACCOUNT>_...` (or custom `vault_prefix` in settings)

Token keys:
- Facebook: `FACEBOOK_ACCESS_TOKEN`
- Instagram: `INSTAGRAM_ACCESS_TOKEN`
- X: `X_BEARER_TOKEN` (or `X_ACCESS_TOKEN`)
- LinkedIn: `LINKEDIN_ACCESS_TOKEN`

Global fallbacks:
- `FACEBOOK_ACCESS_TOKEN`
- `INSTAGRAM_ACCESS_TOKEN`
- `X_BEARER_TOKEN` / `TWITTER_BEARER_TOKEN`
- `LINKEDIN_ACCESS_TOKEN`

Optional default IDs:
- `FACEBOOK_PAGE_ID`
- `INSTAGRAM_BUSINESS_ID`
- `X_USER_ID`, `X_USERNAME`
- `LINKEDIN_PERSON_URN`, `LINKEDIN_ORGANIZATION_URN`

## Settings Model
Settings live under `[social]`:

```toml
[social]
default_account = "core"
default_env = "prod"

[social.facebook]
api_base_url = "https://graph.facebook.com"
api_version = "v22.0"
auth_style = "query"

[social.instagram]
api_base_url = "https://graph.facebook.com"
api_version = "v22.0"
auth_style = "query"

[social.x]
api_base_url = "https://api.twitter.com"
api_version = "2"
auth_style = "bearer"

[social.linkedin]
api_base_url = "https://api.linkedin.com"
api_version = "v2"
auth_style = "bearer"

[social.accounts.core]
facebook_access_token_env = "SOCIAL_CORE_FACEBOOK_ACCESS_TOKEN"
instagram_access_token_env = "SOCIAL_CORE_INSTAGRAM_ACCESS_TOKEN"
x_access_token_env = "SOCIAL_CORE_X_BEARER_TOKEN"
linkedin_access_token_env = "SOCIAL_CORE_LINKEDIN_ACCESS_TOKEN"
facebook_page_id = "1234567890"
instagram_business_id = "17890000000000000"
x_username = "acme"
linkedin_person_urn = "urn:li:person:abc123"
```

## Characteristics + Scalability
- Default API version/base/auth/rate lives in Go specs (`internal/providers`).
- API calls run through provider admission checks (token bucket).
- Runtime feedback (`429`, `Retry-After`, `X-RateLimit-*`) adapts pacing dynamically.
