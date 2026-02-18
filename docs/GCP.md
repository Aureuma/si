# GCP Command Guide (`si gcp`)

`si gcp` covers Google Cloud Service Usage, IAM, API keys, Gemini (Generative Language), and Vertex AI.

## Auth and context

```bash
si gcp auth status --project <project_id>
si gcp context list
si gcp context current
si gcp context use --account core --project <project_id> --token-env GOOGLE_OAUTH_ACCESS_TOKEN --api-key-env GEMINI_API_KEY
si gcp doctor --project <project_id>
```

## Service Usage

```bash
si gcp service enable --name aiplatform.googleapis.com --project <project_id>
si gcp service disable --name generativelanguage.googleapis.com --project <project_id>
si gcp service get --name serviceusage.googleapis.com --project <project_id>
si gcp service list --project <project_id> --filter state:ENABLED
```

## IAM

```bash
si gcp iam service-account list --project <project_id>
si gcp iam service-account get <email> --project <project_id>
si gcp iam service-account create --project <project_id> --account-id app-bot --display-name "App Bot"
si gcp iam service-account-key list --project <project_id> --service-account <email>
si gcp iam policy get --project <project_id>
si gcp iam role list
```

## API keys

```bash
si gcp apikey list --project <project_id>
si gcp apikey get <key_id> --project <project_id>
si gcp apikey create --project <project_id> --display-name "gemini-client"
si gcp apikey update <key_id> --project <project_id> --display-name "gemini-client-v2"
si gcp apikey delete <key_id> --project <project_id> --force
```

## Gemini text and embeddings

```bash
si gcp gemini models list --api-key $GEMINI_API_KEY
si gcp gemini models get gemini-2.5-flash --api-key $GEMINI_API_KEY
si gcp gemini generate --api-key $GEMINI_API_KEY --model gemini-2.5-flash --prompt "Draft release notes"
si gcp gemini embed --api-key $GEMINI_API_KEY --model text-embedding-004 --text "search phrase"
si gcp gemini count-tokens --api-key $GEMINI_API_KEY --model gemini-2.5-flash --text "hello world"
si gcp gemini batch-embed --api-key $GEMINI_API_KEY --model text-embedding-004 --text "one" --text "two"
```

## Gemini image generation

```bash
si gcp gemini image generate \
  --api-key $GEMINI_API_KEY \
  --model gemini-2.5-flash-image \
  --prompt "Create a transparent PNG hero illustration for si CLI" \
  --transparent \
  --output assets/images/si-hero.png
```

Notes:

- Default image model is `gemini-2.5-flash-image`.
- Auth can come from `--api-key`, account-scoped `GCP_<ACCOUNT>_API_KEY`, `GEMINI_API_KEY`, `GOOGLE_API_KEY`, or OAuth access token.

## Vertex AI

```bash
si gcp vertex model list --project <project_id> --location us-central1
si gcp vertex endpoint list --project <project_id> --location us-central1
si gcp vertex endpoint predict <endpoint_id> --project <project_id> --location us-central1 --instances-json '[{"content":"hello"}]'
si gcp vertex batch list --project <project_id> --location us-central1
si gcp vertex pipeline list --project <project_id> --location us-central1
si gcp vertex operation list --project <project_id> --location us-central1
```

## AI umbrella alias

```bash
si gcp ai gemini generate --api-key $GEMINI_API_KEY --prompt "hello"
si gcp ai vertex batch list --project <project_id> --location us-central1
```

## Raw escape hatches

```bash
si gcp raw --project <project_id> --method GET --path /v1/projects/<project_id>/services
si gcp gemini raw --api-key $GEMINI_API_KEY --method GET --path /v1beta/models
si gcp vertex raw --project <project_id> --location us-central1 --method GET --path /v1/projects/<project_id>/locations/us-central1/models
```
