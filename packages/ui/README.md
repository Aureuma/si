# @silexa/ui

Shared UI components for SvelteKit apps, built on shadcn-svelte and Tailwind.

## Setup
From repo root:
- `pnpm --filter @silexa/ui dlx shadcn-svelte@latest init`
- Add components with `pnpm --filter @silexa/ui dlx shadcn-svelte@latest add <component>`

## Usage in apps
Import from the package:
- `import { Button } from "@silexa/ui"`

Keep app-specific styling in the app; shared components should be generic and reusable.
