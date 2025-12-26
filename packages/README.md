# Shared Packages

Shared packages keep dependencies and patterns consistent across apps. Keep them small, well-documented, and focused on reuse.

## Packages
- `@silexa/ui`: shadcn-svelte component library (SvelteKit)
- `@silexa/db`: database client helper for Drizzle + Postgres
- `@silexa/auth`: shared auth adapters/config for @auth/sveltekit

## Guidelines
- Prefer well-known ecosystem libraries over custom code.
- Keep app-specific logic in the app, not the package.
- Export stable APIs and document required env vars.
