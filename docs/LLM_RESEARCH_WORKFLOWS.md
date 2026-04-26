# LLM Research Workflows

This guide covers the narrow path SI already supports for internal research loops that need Gemini through GCP/Vertex and Anthropic-class model access through AWS Bedrock.

Related:
- [GCP Command Guide](./GCP.md)
- [AWS Command Guide](./AWS.md)
- [Settings](./SETTINGS.md)

## Working assumptions

- Use SI Orbit for provider access instead of ad hoc SDK scripts when you only need model discovery, prompt execution, or provider diagnostics.
- Use `si fort` as the secret boundary. Do not introduce local plaintext credential files for research helpers.
- Orbit commands already integrate with the current Fort-backed runtime session and provider context. Do not wrap orbit calls in `si fort run`.

## Fast provider checks

Verify that the provider surfaces you need are reachable before building a research harness:

```bash
si orbit gcp auth status --project <project_id>
si orbit gcp doctor --project <project_id>
si orbit aws auth status --account <account> --region us-east-1 --json
si orbit aws doctor --account <account> --region us-east-1 --json
```

## Discover the current model surface

Avoid hard-coding a specific frontier model name into the workflow before checking what the provider currently exposes:

```bash
si orbit gcp gemini models list --project <project_id>
si orbit gcp vertex model list --project <project_id> --location us-central1
si orbit aws bedrock models list --region us-east-1
```

## Quick text-generation loops

Use these for literature triage, prompt checks, and controller-idea smoke tests:

```bash
si orbit gcp gemini generate \
  --project <project_id> \
  --model <gemini_model> \
  --prompt "Summarize the memory-control idea in this abstract in 5 bullets."

si orbit aws bedrock runtime converse \
  --account <account> \
  --region us-east-1 \
  --model-id <bedrock_model_id> \
  --prompt "Compare recursive context traversal against memory-bank controllers."
```

## Structured request escape hatches

When a research experiment needs a provider feature not yet wrapped by a higher-level command, prefer Orbit's raw/JSON entry points over bespoke credential handling:

```bash
si orbit gcp gemini generate \
  --project <project_id> \
  --model <gemini_model> \
  --json-body '{"contents":[{"role":"user","parts":[{"text":"Return JSON only."}]}]}'

si orbit gcp vertex endpoint predict \
  --project <project_id> \
  --location us-central1 \
  --endpoint <endpoint_id> \
  --instances-json '[{"content":"hello"}]'

si orbit aws bedrock runtime converse \
  --account <account> \
  --region us-east-1 \
  --model-id <bedrock_model_id> \
  --body-file request.json
```

## Research-harness posture

- Keep provider selection late-bound by listing models first and passing model ids through config.
- Store only experiment configuration and outputs in the research repo; keep credentials in Fort-backed flows.
- Start with Orbit CLI commands for corpus scouting and prompt iteration, then move to SI-owned code only when the experiment pattern is stable enough to deserve a reusable harness.
