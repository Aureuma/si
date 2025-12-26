import postgres from "postgres";
import { drizzle } from "drizzle-orm/postgres-js";
import type { PostgresJsDatabase } from "drizzle-orm/postgres-js";

type Schema = Record<string, unknown>;

type DbOptions<TSchema extends Schema> = {
  connectionString: string;
  schema?: TSchema;
  max?: number;
};

export function createDb<TSchema extends Schema>(
  options: DbOptions<TSchema>
): PostgresJsDatabase<TSchema> {
  const client = postgres(options.connectionString, {
    max: options.max ?? 10
  });
  return drizzle(client, { schema: options.schema });
}
