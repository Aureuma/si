# GCP Command Guide (`si gcp`)

`si gcp` covers common Google Cloud operations across Service Usage, IAM, API key management, Gemini (Generative Language), and Vertex AI.

## Auth + Context

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
si gcp iam service-account delete <email> --project <project_id> --force

si gcp iam service-account-key list --project <project_id> --service-account <email>
si gcp iam service-account-key create --project <project_id> --service-account <email>
si gcp iam service-account-key delete --name projects/<project>/serviceAccounts/<email>/keys/<key_id> --force

si gcp iam policy get --project <project_id>
si gcp iam policy set --project <project_id> --policy-json '{"bindings":[]}'
si gcp iam policy test-permissions --project <project_id> --permission resourcemanager.projects.get

si gcp iam role list
si gcp iam role get roles/viewer
```

## API Keys

```bash
si gcp apikey list --project <project_id>
si gcp apikey get <key_id> --project <project_id>
si gcp apikey create --project <project_id> --display-name "gemini-client"
si gcp apikey update <key_id> --project <project_id> --display-name "gemini-client-v2"
si gcp apikey delete <key_id> --project <project_id> --force
si gcp apikey undelete <key_id> --project <project_id> --force
si gcp apikey lookup --key-string <api_key_string>
```

## Gemini (API Key or OAuth)

```bash
si gcp gemini models list --api-key $GEMINI_API_KEY
si gcp gemini models get gemini-2.0-flash --api-key $GEMINI_API_KEY
si gcp gemini generate --api-key $GEMINI_API_KEY --model gemini-2.0-flash --prompt "Draft release notes"
si gcp gemini embed --api-key $GEMINI_API_KEY --model text-embedding-004 --text "search phrase"
si gcp gemini count-tokens --api-key $GEMINI_API_KEY --model gemini-2.0-flash --text "hello world"
si gcp gemini batch-embed --api-key $GEMINI_API_KEY --model text-embedding-004 --text "one" --text "two"
```

## Vertex AI

```bash
si gcp vertex model list --project <project_id> --location us-central1
si gcp vertex endpoint list --project <project_id> --location us-central1
si gcp vertex endpoint predict <endpoint_id> --project <project_id> --location us-central1 --instances-json '[{"content":"hello"}]'
si gcp vertex batch list --project <project_id> --location us-central1
si gcp vertex batch create --project <project_id> --location us-central1 --json-body '{"displayName":"batch-job"}'
si gcp vertex pipeline list --project <project_id> --location us-central1
si gcp vertex operation list --project <project_id> --location us-central1
```

## AI umbrella alias

```bash
si gcp ai gemini generate --api-key $GEMINI_API_KEY --prompt "hello"
si gcp ai vertex batch list --project <project_id> --location us-central1
```

## Raw escape hatch

```bash
si gcp raw --project <project_id> --method GET --path /v1/projects/<project_id>/services
si gcp gemini raw --api-key $GEMINI_API_KEY --method GET --path /v1beta/models
si gcp vertex raw --project <project_id> --location us-central1 --method GET --path /v1/projects/<project_id>/locations/us-central1/models
```
