---
title: Apple App Store Command Guide
description: App Store Connect workflows in SI for auth, context, app metadata, listing updates, and raw API access.
---

# Apple App Store Command Guide (`si orbit apple store`)

![Apple App Store](/docs/images/integrations/apple-appstore.svg)

`si orbit apple store` provides App Store Connect automation for app creation, listing metadata, and managed apply flows.

## Related docs

- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [Vault](./VAULT)
- [Providers](./PROVIDERS)

## Command surface

```bash
si orbit apple store <auth|context|doctor|app|listing|raw|apply>
```

## Auth and context

```bash
si orbit apple store auth status --account core --json
si orbit apple store context list --json
si orbit apple store context current --json
si orbit apple store context use --account core --env prod --json
si orbit apple store doctor --account core --public --json
```

## App and listing workflows

```bash
si orbit apple store app list --json
si orbit apple store app get --bundle-id com.example.app --json
si orbit apple store app create --bundle-id com.example.app --bundle-name "Example" --platform IOS --app-name "Example" --sku EXAMPLE001 --primary-locale en-US --json

si orbit apple store listing get --bundle-id com.example.app --locale en-US --json
si orbit apple store listing update --bundle-id com.example.app --locale en-US --name "Example" --description "Release notes" --json
```

## Managed metadata apply

```bash
si orbit apple store apply --bundle-id com.example.app --metadata-dir appstore --version 1.2.0 --create-version --json
```

Use this flow to keep metadata as code and apply deterministic changes.

## Raw API mode

```bash
si orbit apple store raw --method GET --path /v1/apps --json
si orbit apple store raw --method PATCH --path /v1/appStoreVersionLocalizations/<id> --json-body '{"data":{"type":"appStoreVersionLocalizations","id":"<id>","attributes":{"description":"Updated"}}}' --json
```

## Safety guidance

- Keep JWT issuer/key configuration in vault-managed env variables.
- Validate bundle ID and target locale before listing updates.
- Use `apply` from versioned metadata files for repeatable releases.
- Treat raw mode as escape hatch for unsupported endpoints.

## Troubleshooting

1. `si orbit apple store auth status --json`
2. `si orbit apple store doctor --json`
3. `si orbit list --provider apple_appstore --json`
4. Verify API key, issuer, key id, and private key source.
