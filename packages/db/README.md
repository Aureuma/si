# @silexa/db

Shared database helper for SvelteKit apps using Drizzle ORM + Postgres.

## Usage
- Provide `DATABASE_URL` per app.
- Import and pass your app schema:

```ts
import * as schema from "$lib/db/schema";
import { createDb } from "@silexa/db";

const db = createDb({ connectionString: process.env.DATABASE_URL!, schema });
```

## Recommended stack
- `drizzle-orm` + `drizzle-kit`
- `postgres` driver

Each app owns its schema and migrations under `apps/<app>/migrations`.
