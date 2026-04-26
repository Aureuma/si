# GCP Command Guide (`si orbit gcp`)

![Google Cloud](/docs/images/integrations/gcp.svg)

`si orbit gcp` covers Google Cloud Service Usage, IAM, API keys, Gemini (Generative Language), and Vertex AI.

Related:
- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [Providers](./PROVIDERS)

## Auth and context

```bash
si orbit gcp auth status --project <project_id>
si orbit gcp context list
si orbit gcp context current
si orbit gcp context use --account core --project <project_id> --token-env GOOGLE_OAUTH_ACCESS_TOKEN --api-key-env GEMINI_API_KEY
si orbit gcp doctor --project <project_id>
```

## Service Usage

```bash
si orbit gcp service enable --name aiplatform.googleapis.com --project <project_id>
si orbit gcp service disable --name generativelanguage.googleapis.com --project <project_id>
si orbit gcp service get --name serviceusage.googleapis.com --project <project_id>
si orbit gcp service list --project <project_id> --filter state:ENABLED
```

## IAM

```bash
si orbit gcp iam account list --project <project_id>
si orbit gcp iam account get <email> --project <project_id>
si orbit gcp iam account create --project <project_id> --account-id app-bot --display-name "App Bot"
si orbit gcp iam keys list --project <project_id> --service-account <email>
si orbit gcp iam policy get --project <project_id>
si orbit gcp iam role list
```

## API keys

```bash
si orbit gcp keys list --project <project_id>
si orbit gcp keys get <key_id> --project <project_id>
si orbit gcp keys create --project <project_id> --display-name "gemini-client"
si orbit gcp keys update <key_id> --project <project_id> --display-name "gemini-client-v2"
si orbit gcp keys delete <key_id> --project <project_id> --force
```

## Gemini text and embeddings

```bash
si orbit gcp gemini models list --api-key $GEMINI_API_KEY
si orbit gcp gemini models get gemini-2.5-flash --api-key $GEMINI_API_KEY
si orbit gcp gemini generate --api-key $GEMINI_API_KEY --model gemini-2.5-flash --prompt "Draft release notes"
si orbit gcp gemini embed --api-key $GEMINI_API_KEY --model text-embedding-004 --text "search phrase"
si orbit gcp gemini count --api-key $GEMINI_API_KEY --model gemini-2.5-flash --text "hello world"
si orbit gcp gemini batch --api-key $GEMINI_API_KEY --model text-embedding-004 --text "one" --text "two"
```

## Gemini image generation

```bash
si orbit gcp gemini image generate \
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
si orbit gcp vertex model list --project <project_id> --location us-central1
si orbit gcp vertex generate --project <project_id> --location us-central1 --model gemini-2.5-pro --prompt "Summarize this paper"
si orbit gcp vertex endpoint list --project <project_id> --location us-central1
si orbit gcp vertex endpoint predict <endpoint_id> --project <project_id> --location us-central1 --instances-json '[{"content":"hello"}]'
si orbit gcp vertex batch list --project <project_id> --location us-central1
si orbit gcp vertex pipeline list --project <project_id> --location us-central1
si orbit gcp vertex operation list --project <project_id> --location us-central1
```

`si orbit gcp vertex generate` targets Vertex AI publisher models and accepts either a shorthand Gemini model id such as `gemini-2.5-pro` or a fully qualified publisher-model resource name.

## AI umbrella alias

```bash
si orbit gcp ai gemini generate --api-key $GEMINI_API_KEY --prompt "hello"
si orbit gcp ai vertex batch list --project <project_id> --location us-central1
```

## Raw escape hatches

```bash
si orbit gcp raw --project <project_id> --method GET --path /v1/projects/<project_id>/services
si orbit gcp gemini raw --api-key $GEMINI_API_KEY --method GET --path /v1beta/models
si orbit gcp vertex raw --project <project_id> --location us-central1 --method GET --path /v1/projects/<project_id>/locations/us-central1/models
```
