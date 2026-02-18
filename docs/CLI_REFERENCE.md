# CLI Reference

Use `si --help` for the full command and flag surface.

## Top-level command groups

- `si dyad ...`: actor/critic dyad lifecycle and loop operations.
- codex lifecycle: `si spawn|respawn|list|status|report|login|logout|swap|ps|run|logs|tail|clone|remove|stop|start|warmup`.
- `si vault ...`: encrypted dotenv management + secure process/container injection.
- `si stripe|github|cloudflare|google|apple|social|workos|publish|aws|gcp|openai|oci ...`: provider bridge families.
- `si providers ...`: provider capability and health views.
- `si plugins ...`: plugin marketplace, catalog, and integration lifecycle.
- `si browser ...`: Dockerized Playwright MCP runtime.
- `si paas ...`: Docker-native deployment and operations workflows.
- `si mintlify ...`: docs bootstrap/validate/dev wrappers via Mintlify CLI.
- `si build ...`: local runtime image and self-build workflows.
- `si analyze|lint`: static analysis for `go.work` modules.
- `si docker ...`: raw Docker passthrough.

## Help conventions

- `si <command> --help` prints command-level usage.
- `si <command> <subcommand> --help` prints subcommand flags.
- Most command families support `help`, `-h`, and `--help` aliases.

Examples:

```bash
si --help
si paas --help
si paas backup --help
si gcp gemini image generate --help
si mintlify --help
```

## High-signal operations

```bash
# Build runtime image
si build image

# Spawn dyad
si dyad spawn app-hardening --profile main

# Run PaaS doctor checks
si paas doctor --json

# Trigger Supabase WAL-G backup
si paas backup run --app <slug> --json

# Run browser runtime for MCP clients
si browser start

# Scaffold and register a plugin integration
si plugins scaffold acme/release-mind --dir ./integrations
si plugins register ./integrations/acme/release-mind --channel community
si plugins policy set --deny acme/release-mind
```

## Security notes

- Prefer `si vault run -- <cmd>` for sensitive commands.
- Avoid plaintext secret files in repo workspaces.
- Use `si paas doctor` before production writes.
