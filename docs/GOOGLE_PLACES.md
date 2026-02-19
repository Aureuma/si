# Google Places Command Guide (`si google places`)

![Google Places](/docs/images/integrations/google-places.svg)

`si google places` is the Google Places API (New) bridge for autocomplete, search, details, photos, local reporting, and raw API access.

Related:
- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [Providers](./PROVIDERS)

Auth policy:
- API key only (Places API New web service usage).
- Credentials should be injected from `si vault` (or compatible env keys).
- Settings should store env references/pointers, not raw keys.

## Credential Keys (Vault-Compatible)

Per account alias `<ACCOUNT>` (uppercase slug):

- `GOOGLE_<ACCOUNT>_PLACES_API_KEY`
- `GOOGLE_<ACCOUNT>_PROD_PLACES_API_KEY`
- `GOOGLE_<ACCOUNT>_STAGING_PLACES_API_KEY`
- `GOOGLE_<ACCOUNT>_DEV_PLACES_API_KEY`
- `GOOGLE_<ACCOUNT>_PROJECT_ID`
- `GOOGLE_<ACCOUNT>_DEFAULT_REGION_CODE`
- `GOOGLE_<ACCOUNT>_DEFAULT_LANGUAGE_CODE`
- `GOOGLE_<ACCOUNT>_API_BASE_URL`

Global fallback keys:

- `GOOGLE_PLACES_API_KEY`
- `GOOGLE_PROJECT_ID`
- `GOOGLE_API_BASE_URL`
- `GOOGLE_DEFAULT_ACCOUNT`
- `GOOGLE_DEFAULT_ENV`
- `GOOGLE_DEFAULT_REGION_CODE`
- `GOOGLE_DEFAULT_LANGUAGE_CODE`

Environment policy:
- `prod`, `staging`, `dev` are the supported context labels.
- `test` is intentionally not used as a standalone environment mode.

## Context + Auth + Diagnostics

```bash
si google places auth status --account core
si google places context list
si google places context current
si google places context use --account core --env prod --language en --region US
si google places doctor --account core
```

## Session Tokens

Use sessions for autocomplete -> details flows:

```bash
si google places session new
si google places session inspect <token>
si google places session list
si google places session end <token>
```

## Search + Details

```bash
si google places autocomplete --input "coffee" --session <token>
si google places search-text --query "coffee near downtown" --field-mask places.id,places.displayName,places.formattedAddress
si google places search-nearby --center 37.7749,-122.4194 --radius 1200 --included-type cafe --field-mask places.id,places.displayName,places.formattedAddress
si google places details <place_id_or_name> --session <token> --field-mask id,name,displayName,formattedAddress
```

Pagination helpers:

```bash
si google places search-text --query "coffee" --all --max-pages 4 --field-mask places.id,places.displayName
si google places search-nearby --center 37.77,-122.41 --radius 2000 --all --field-mask places.id,places.displayName
```

## Photos

```bash
si google places photo get places/<place_id>/photos/<photo_id>
si google places photo download places/<place_id>/photos/<photo_id> --output ./photo.jpg --max-width 1200
```

## Types + Reports

```bash
si google places types list --group food
si google places types validate cafe

si google places report usage --since 2026-02-08T00:00:00Z
si google places report sessions
```

`report usage` is local-log-based and reads the bridge log (`~/.si/logs/google-places.log` by default).

## Raw Escape Hatch

```bash
si google places raw --method GET --path /v1/places/<place_id> --field-mask id,name
si google places raw --method POST --path /v1/places:searchText --body '{"textQuery":"coffee"}' --field-mask places.id,places.displayName
```

## Field Mask Policy

- `search-text`, `search-nearby`, and `details` require field masks.
- Presets are available via `--field-preset` (`search-basic`, `details-basic`, etc.).
- Wildcard `*` is blocked unless `--allow-wildcard-mask` is explicitly set.
- Human mode prints a field-mask cost hint (`low|medium|high`).

## Error Reporting

On failures, `si google places` surfaces:

- HTTP status and Google status/code
- request id (when present)
- error message and structured details payload
- redacted raw body for debugging
