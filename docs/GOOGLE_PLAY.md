# Google Play (`si google play`)

![Google Play](/docs/images/integrations/google-play.svg)

`si google play` provides direct Google Play automation through the Google Play Developer API (and Play Custom App API for app creation).

Related:
- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [Providers](./PROVIDERS)

## What is automated
- Service-account auth + context management.
- Private custom app creation (`playcustomapp` API).
- Play Store listing updates (title, short/full description, video).
- App details updates (contact email/phone/website, default language).
- Listing asset management (screenshots, icon, feature graphic, TV banner).
- Release upload + track orchestration (AAB/APK upload, promote, halt/resume).
- End-to-end metadata apply flow from a repository folder.

## What still requires manual console setup
- Initial Play Developer account setup and billing/profile onboarding.
- First-time policy acknowledgements and compliance forms that are not fully API-driven.

## Credentials
Use a Google Cloud service account with Android Publisher permissions for your app/developer account.

Supported credential sources (highest priority first):
- `--service-account-json '<json>'`
- `--service-account-json @/path/to/service-account.json`
- `--service-account-file /path/to/service-account.json`
- `GOOGLE_<ACCOUNT>_PLAY_SERVICE_ACCOUNT_JSON`
- `GOOGLE_PLAY_SERVICE_ACCOUNT_JSON`
- `GOOGLE_<ACCOUNT>_PLAY_SERVICE_ACCOUNT_FILE`
- `GOOGLE_PLAY_SERVICE_ACCOUNT_FILE`

## Quickstart

```bash
# auth + context
./si google play auth status --account core --verify-package com.example.app
./si google play context use --account core --env prod --package com.example.app --service-account-file ~/.secrets/play-sa.json

# listing + details
./si google play listing update --account core --package com.example.app --language en-US \
  --title "Example App" --short-description "Short summary" --full-description "Long description"
./si google play details update --account core --package com.example.app \
  --contact-email support@example.com --contact-website https://example.com/support

# assets
./si google play asset upload --account core --package com.example.app --language en-US \
  --type phoneScreenshots --clear-first --file ./play-store/images/en-US/phoneScreenshots/01.png

# releases
./si google play release upload --account core --package com.example.app --aab ./app-release.aab --track internal
./si google play release promote --account core --package com.example.app --from internal --to production --status completed
./si google play release set-status --account core --package com.example.app --track production --status halted

# raw fallback
./si google play raw --account core --method GET \
  --path /androidpublisher/v3/applications/com.example.app/edits

# release metadata planning (ReleaseMind namespace)
./si releasemind play plan --repo-path /path/to/mobile-repo --planner-repo /path/to/releasemind --write /tmp/play-plan.json
```

## Metadata apply flow

```bash
./si google play apply --account core --package com.example.app --metadata-dir ./play-store --aab ./app-release.aab --track internal
```

`--metadata-dir` structure:
- `details.json`
- `listings/<language>.json` (for example `listings/en-US.json`)
- `images/<language>/<imageType>/*.{png,jpg,jpeg,webp}`

Supported `imageType` values:
- `phoneScreenshots`
- `sevenInchScreenshots`
- `tenInchScreenshots`
- `tvScreenshots`
- `wearScreenshots`
- `icon`
- `featureGraphic`
- `tvBanner`

## Command surface
- `si google play auth status`
- `si google play context list|current|use`
- `si google play doctor`
- `si google play app create`
- `si google play listing get|list|update`
- `si google play details get|update`
- `si google play asset list|upload|clear`
- `si google play release upload|status|promote|set-status`
- `si google play raw`
- `si google play apply`
