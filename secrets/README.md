Place secret files here (ignored by git).
- app-<app>.env: per-app environment variables for `bin/app-secrets.sh` (examples: DATABASE_URL, AUTH_SECRET).
- app-<app>.env.sops: encrypted env files (SOPS + age) allowed in git.
