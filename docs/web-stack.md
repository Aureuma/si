# SvelteKit Web Stack (Standard)

This is the default stack for new web apps in Silexa.

## Core
- **Framework**: SvelteKit (TypeScript, full-stack)
- **UI**: shadcn-svelte + Tailwind CSS
- **Routing/SSR**: SvelteKit built-ins
- **Adapter**: adapter-node for container deploys (required by `silexa app build`)

## Data + Auth
- **DB**: Postgres (or SQLite for local-only)
- **ORM**: Drizzle
- **Auth**: @auth/sveltekit (+ Drizzle adapter)

## Common libraries
- **Validation**: Zod
- **Forms**: sveltekit-superforms (optional)
- **Icons**: lucide-svelte
- **HTTP**: fetch + SvelteKit endpoints
 - **Package manager**: pnpm (workspace-friendly; required for shared packages)

## Shared packages
- `@silexa/ui` for UI components
- `@silexa/db` for Drizzle helpers
- `@silexa/auth` for auth adapters
