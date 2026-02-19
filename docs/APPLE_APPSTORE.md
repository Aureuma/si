---
title: Apple App Store Command Guide
description: App Store Connect workflows in SI for auth, context, app metadata, listing updates, and raw API access.
---

# Apple App Store Command Guide (`si apple appstore`)

![Apple App Store](/docs/images/integrations/apple-appstore.svg)

`si apple appstore` provides App Store Connect automation for app creation, listing metadata, and managed apply flows.

## Related docs

- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [Vault](./VAULT)
- [Providers](./PROVIDERS)

## Command surface

```bash
si apple appstore <auth|context|doctor|app|listing|raw|apply>
```

## Auth and context

```bash
si apple appstore auth status --account core --json
si apple appstore context list --json
si apple appstore context current --json
si apple appstore context use --account core --env prod --json
si apple appstore doctor --account core --public --json
```

## App and listing workflows

```bash
si apple appstore app list --json
si apple appstore app get --bundle-id com.example.app --json
si apple appstore app create --bundle-id com.example.app --bundle-name "Example" --platform IOS --app-name "Example" --sku EXAMPLE001 --primary-locale en-US --json

si apple appstore listing get --bundle-id com.example.app --locale en-US --json
si apple appstore listing update --bundle-id com.example.app --locale en-US --name "Example" --description "Release notes" --json
```

## Managed metadata apply

```bash
si apple appstore apply --bundle-id com.example.app --metadata-dir appstore --version 1.2.0 --create-version --json
```

Use this flow to keep metadata as code and apply deterministic changes.

## Raw API mode

```bash
si apple appstore raw --method GET --path /v1/apps --json
si apple appstore raw --method PATCH --path /v1/appStoreVersionLocalizations/<id> --json-body '{"data":{"type":"appStoreVersionLocalizations","id":"<id>","attributes":{"description":"Updated"}}}' --json
```

## Safety guidance

- Keep JWT issuer/key configuration in vault-managed env variables.
- Validate bundle ID and target locale before listing updates.
- Use `apply` from versioned metadata files for repeatable releases.
- Treat raw mode as escape hatch for unsupported endpoints.

## Troubleshooting

1. `si apple appstore auth status --json`
2. `si apple appstore doctor --json`
3. `si providers health --provider apple_appstore --json`
4. Verify API key, issuer, key id, and private key source.
