---
title: Publish Command Guide
description: DistributionKit-backed publishing workflows in SI for Dev.to, Hashnode, Reddit, Hacker News, and Product Hunt.
---

# Publish Command Guide (`si publish`)

![Publish](/images/integrations/publish.svg)

`si publish` provides a unified publishing surface across multiple launch channels.

## Related docs

- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [Vault](./VAULT)
- [CLI Reference](./CLI_REFERENCE)

## Command surface

```bash
si publish <catalog|devto|hashnode|reddit|hackernews|producthunt>
```

## Platform catalog

```bash
si publish catalog list --pricing free-at-least --limit 25 --json
si publish catalog list --query "product launch" --json
```

## Dev.to

```bash
si publish devto auth --json
si publish devto article --title "Release" --body "# launch" --tags go,cli --published --json
si publish devto raw --path /api/articles/me --json
```

## Hashnode

```bash
si publish hashnode auth --json
si publish hashnode post --publication-id <id> --title "Release" --content-markdown "# launch" --tags go,saas --json
si publish hashnode raw --query 'query { me { id } }' --json
```

## Reddit

```bash
si publish reddit auth --json
si publish reddit submit --subreddit startups --title "Release" --kind self --text "launch notes" --json
si publish reddit raw --path /api/v1/me --json
```

## Hacker News

```bash
si publish hackernews top --limit 20 --json
si publish hackernews item --id 123456 --json
si publish hackernews submit-url --title "Release" --url https://example.com --json
```

## Product Hunt

```bash
si publish producthunt auth --json
si publish producthunt posts --first 10 --json
si publish producthunt raw --query 'query { viewer { user { name } } }' --json
```

## Safety guidance

- Validate each provider auth status before publishing.
- Keep post bodies in files and load through CI for reviewability.
- Use `--json` in pipelines to capture request/response status.
- Stage posts in non-production communities/accounts first.

## Troubleshooting

1. Re-run provider-specific `auth` command.
2. Verify token scope and account binding for each platform.
3. Check response payload from `raw` mode for field-level errors.
