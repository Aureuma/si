---
title: WorkOS Command Guide
description: WorkOS integration workflows in SI for organizations, users, memberships, invitations, directories, and raw API access.
---

# WorkOS Command Guide (`si workos`)

![WorkOS](/images/integrations/workos.svg)

`si workos` provides WorkOS operational APIs with account context and environment-aware auth.

## Related docs

- [Integrations Overview](./INTEGRATIONS_OVERVIEW)
- [Vault](./VAULT)
- [Providers](./PROVIDERS)

## Command surface

```bash
si workos <auth|context|doctor|organization|user|membership|invitation|directory|raw>
```

## Auth and context

```bash
si workos auth status --account core --env prod --json
si workos context list --json
si workos context current --json
si workos context use --account core --env prod --org-id org_123
si workos doctor --account core --env prod --public --json
```

## Organization and user management

```bash
si workos organization list --json
si workos organization get org_123 --json
si workos organization create --name "Aureuma" --json

si workos user list --json
si workos user get user_123 --json
si workos user create --email admin@example.com --first-name Admin --last-name User --json
```

## Memberships, invitations, directories

```bash
si workos membership list --organization-id org_123 --json
si workos membership create --organization-id org_123 --user-id user_123 --role admin --json

si workos invitation list --organization-id org_123 --json
si workos invitation create --organization-id org_123 --email ops@example.com --role member --json

si workos directory list --json
si workos directory get dir_123 --json
```

## Raw API mode

```bash
si workos raw --method GET --path /organizations --json
si workos raw --method POST --path /organizations --json-body '{"name":"Aureuma"}' --json
```

## Safety guidance

- Use environment-specific contexts (`prod|staging|dev`) for separation.
- Validate organization IDs before membership/invitation writes.
- Prefer JSON output in CI pipelines.
- Keep WorkOS keys in vault-managed env refs.

## Troubleshooting

1. `si workos auth status --json`
2. `si workos doctor --json`
3. `si providers health --provider workos --json`
4. Validate selected env/account and key source.
