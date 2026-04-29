# Google YouTube (`si orbit google youtube` / `si orbit google youtube-data`)

![YouTube](/docs/images/integrations/youtube.svg)

`si orbit google youtube` provides a YouTube Data API v3 command family with Fort-backed and env-compatible credential resolution, multi-account context, and both API-key and OAuth modes.
`si orbit google youtube-data` is an alias with identical behavior.

Related:
- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [Providers](./PROVIDERS)

## Auth Modes
- `api-key`: public/read operations
- `oauth`: private/mutation operations (channel/video edits, uploads, subscriptions, comments, live, captions, thumbnails)

## Environment Model
- Supported environments: `prod`, `staging`, `dev`
- `test` is intentionally unsupported

## Credential Keys
Per-account defaults:
- `GOOGLE_<ACCOUNT>_YOUTUBE_API_KEY`
- `GOOGLE_<ACCOUNT>_YOUTUBE_CLIENT_ID`
- `GOOGLE_<ACCOUNT>_YOUTUBE_CLIENT_SECRET`
- `GOOGLE_<ACCOUNT>_YOUTUBE_REDIRECT_URI`
- `GOOGLE_<ACCOUNT>_YOUTUBE_ACCESS_TOKEN`
- `GOOGLE_<ACCOUNT>_YOUTUBE_REFRESH_TOKEN`

Per-account per-env overrides:
- `GOOGLE_<ACCOUNT>_PROD_YOUTUBE_API_KEY`
- `GOOGLE_<ACCOUNT>_STAGING_YOUTUBE_API_KEY`
- `GOOGLE_<ACCOUNT>_DEV_YOUTUBE_API_KEY`
- `GOOGLE_<ACCOUNT>_PROD_YOUTUBE_REFRESH_TOKEN`
- `GOOGLE_<ACCOUNT>_STAGING_YOUTUBE_REFRESH_TOKEN`
- `GOOGLE_<ACCOUNT>_DEV_YOUTUBE_REFRESH_TOKEN`

Global fallbacks:
- `GOOGLE_YOUTUBE_API_KEY`
- `GOOGLE_YOUTUBE_CLIENT_ID`
- `GOOGLE_YOUTUBE_CLIENT_SECRET`
- `GOOGLE_YOUTUBE_REDIRECT_URI`
- `GOOGLE_YOUTUBE_ACCESS_TOKEN`
- `GOOGLE_YOUTUBE_REFRESH_TOKEN`

OAuth login cache file:
- `~/.si/google/youtube/oauth_tokens.json`

## Core Commands
```bash
# auth/context
si orbit google youtube auth status --account core --mode api-key
si orbit google youtube auth login --account core --mode oauth
si orbit google youtube auth logout --account core --mode oauth
si orbit google youtube context list
si orbit google youtube context current
si orbit google youtube context use --account core --env prod --mode oauth
si orbit google youtube doctor --account core --mode oauth

# discovery
si orbit google youtube search list --query "engineering vlog" --type video
si orbit google youtube support languages
si orbit google youtube support regions
si orbit google youtube support categories --support-region US

# resources
si orbit google youtube channel list --id <channel_id>
si orbit google youtube channel mine --mode oauth
si orbit google youtube video list --id <video_id>
si orbit google youtube video upload --mode oauth --file ./clip.mp4 --title "Demo" --privacy unlisted
si orbit google youtube video rate --mode oauth --id <video_id> --rating like
si orbit google youtube playlist create --mode oauth --title "Sandbox"
si orbit google youtube items add --mode oauth --playlist-id <playlist_id> --video-id <video_id>
si orbit google youtube subscription list --mode oauth --mine
si orbit google youtube comment thread create --mode oauth --video-id <video_id> --text "Nice work"
si orbit google youtube caption upload --mode oauth --video-id <video_id> --file ./captions.vtt --language en
si orbit google youtube caption download --mode oauth --id <caption_id> --output ./captions.vtt
si orbit google youtube thumbnail set --mode oauth --video-id <video_id> --file ./thumb.jpg
si orbit google youtube live broadcast list --mode oauth
si orbit google youtube live stream list --mode oauth
si orbit google youtube live chat list --mode oauth --live-chat-id <chat_id>

# observability and fallback
si orbit google youtube report usage --since 2026-02-08T00:00:00Z
si orbit google youtube raw --method GET --path /youtube/v3/search --param part=id --param q=music
```

## Notes
- `--json` is strict JSON output.
- `--raw` prints raw response body.
- API and OAuth errors are normalized and redacted before printing.
- Video upload supports resumable mode by default (`--resumable=true`).
