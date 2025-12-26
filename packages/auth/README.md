# @silexa/auth

Shared auth adapter exports for SvelteKit apps.

## Usage
Use `@auth/sveltekit` with the Drizzle adapter and app-specific providers:

```ts
import { SvelteKitAuth, DrizzleAdapter } from "@silexa/auth";
import type { AuthConfig } from "@silexa/auth";
import { db } from "$lib/db";

const config: AuthConfig = {
  adapter: DrizzleAdapter(db),
  providers: []
};

export const handle = SvelteKitAuth(config);
```

Prefer OAuth providers that have official @auth support.
