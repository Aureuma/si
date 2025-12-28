# Secrets (SOPS + age)

Use SOPS with age to store encrypted app secrets in git and decrypt them at deploy time.

## One-time setup
1) Install `sops` and `age` (or `age-keygen`) on your workstation/runner.
2) Run `bin/sops-init.sh` to generate `secrets/age.key` and update `.sops.yaml` with the public key.
3) Export the key when decrypting:
   - `export SOPS_AGE_KEY_FILE=/opt/silexa/secrets/age.key`

## Encrypted app env files
- Store encrypted env files as `secrets/app-<app>.env.sops` (or `secrets/app-<app>.sops.env`).
- Create or edit:
  - `sops secrets/app-<app>.env.sops`

## Apply to Kubernetes
- `bin/app-secrets.sh <app>` detects the encrypted file and decrypts it before applying.

## Notes
- Plaintext env files (`secrets/app-<app>.env`) still work for local dev.
- Encrypted files are allowed in git; plaintext files remain ignored.
