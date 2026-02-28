---
title: SI Documentation Style Guide
description: Writing, structure, formatting, and visual rules for SI documentation contributors.
---

# SI Documentation Style Guide

This guide defines how SI docs should be written and maintained for Mintlify.

## Why this guide exists

SI has broad scope: dyads, codex runtime, provider bridges, browser MCP, Orbitals, and Docker-native PaaS. Without consistent docs standards, operators lose time and trust.

Use this guide as the default contract for every docs PR.

## SI documentation voice

- Be operational and specific.
- Prefer direct statements over marketing language.
- Explain exact commands, flags, and expected behavior.
- Name tradeoffs and safety implications explicitly.
- Keep examples realistic for production workflows.

## Page architecture

Every command or integration page should follow this order:

1. What it is and when to use it.
2. Prerequisites and auth/secret model.
3. Context and setup flow.
4. Core workflows (copy-paste commands).
5. Safety guardrails and failure modes.
6. Diagnostics and troubleshooting.
7. Links to adjacent docs.

Mintlify-specific expectations:

- Include frontmatter with at least `title` and `description`.
- Keep page paths stable; update `docs.json` when files are moved or renamed.
- Prefer the `navigation.tabs -> groups -> pages` pattern in `docs.json` for large doc sets.

## Heading and section conventions

- One H1 per file.
- Use H2 for major lifecycle sections.
- Use H3 only for tightly scoped subflows.
- Avoid deep nesting beyond H3.
- Keep headings action-oriented (for example: `Context Setup`, `Run Diagnostics`).

## Typography and readability

Mintlify controls base typography through theme settings; contributors control readability through structure.

- Paragraphs: short, 1-4 lines when possible.
- Lists: flat and scannable.
- Commands: fenced `bash` blocks.
- JSON payloads: fenced `json` blocks.
- Config examples: fenced `toml` or `yaml` matching actual format.
- Use callouts sparingly for warnings and irreversible actions.

## Code and command examples

Rules:

- Commands must be executable as shown.
- Prefer explicit flags over hidden defaults.
- Include `--json` examples when machine output matters.
- Show destructive operations with `--force` so intent is obvious.
- Never include real secrets or production identifiers.

## Visual assets and logos

- Use integration icons under `docs/images/integrations/`.
- Keep hero visuals lightweight SVG when possible.
- Use one visual near the top of major integration pages.
- Do not depend on third-party hotlinked images for required docs rendering.

## Cross-linking policy

- Link each integration page back to the integration hub.
- Link command guides to related operational docs (Vault, Settings, Browser, Orbitals).
- Avoid orphan pages.
- Prefer relative Mintlify paths (`./PAGE_NAME`).

## Security and compliance documentation rules

For any integration that touches credentials, billing, or production mutation:

- Document credential source precedence.
- Document safe defaults and rejected modes.
- Include at least one "before write" diagnostic command.
- Explain logs/audit behavior when applicable.

## Contributor workflow

1. Propose IA changes in a ticket under `tickets/`.
2. Update docs page(s) and navigation in `docs.json` together.
3. Run `si mintlify validate` and `si mintlify broken-links`.
4. Keep commits scoped (foundation vs provider docs vs polish).

## Definition of done for docs PRs

- New/changed behavior documented.
- Navigation updated.
- Examples validated syntactically.
- Safety sections included for risky workflows.
- Cross-links to adjacent docs added.

## Anti-patterns

- Vague descriptions without commands.
- Long narrative blocks before practical usage.
- Undocumented destructive actions.
- Inconsistent naming between CLI and docs.
- Copying external docs voice verbatim.
