# Mintlify Docs Workflow (`si mintlify`)

`si mintlify` wraps common Mintlify CLI operations so docs can be managed from the same `si` command surface.

## Commands

```bash
si mintlify init [--repo <path>] [--docs-dir <path>] [--name <site>] [--site-url <url>] [--force]
si mintlify dev [--repo <path>] [-- mint args...]
si mintlify validate [--repo <path>] [-- mint args...]
si mintlify broken-links [--repo <path>] [-- mint args...]
si mintlify openapi-check [--repo <path>] [-- mint args...]
si mintlify a11y [--repo <path>] [-- mint args...]
si mintlify rename [--repo <path>] [-- mint args...]
si mintlify update [--repo <path>] [-- mint args...]
si mintlify upgrade [--repo <path>] [-- mint args...]
si mintlify migrate-mdx [--repo <path>] [-- mint args...]
si mintlify version [--repo <path>] [-- mint args...]
si mintlify raw [--repo <path>] -- <mint args...>
```

## Typical usage

Initialize config and homepage:

```bash
si mintlify init --repo . --docs-dir docs --name si --site-url https://docs.si.aureuma.ai --force
```

Validate docs:

```bash
si mintlify validate
si mintlify broken-links
si mintlify openapi-check
```

Run local docs site:

```bash
si mintlify dev
```

## Notes

- `si mintlify ...` shells out to `npx -y mint ...`.
- Use `si mintlify raw` for Mintlify subcommands not yet wrapped directly.
- Keep docs assets under `docs/` (for example `docs/images/si-hero.png`) so Mintlify can serve them.
