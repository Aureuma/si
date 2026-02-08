# Ticket: `si vault` Git-Based Credentials Management (Encrypted at Rest, Docker-Friendly at Runtime)

Date: 2026-02-07
Owner: Unassigned
Primary Goal: Add a first-class `si vault ...` command family that manages credentials encrypted at rest (committed to a separate private git repo, usually as a submodule) and injects/decrypts them in a Docker-friendly way without writing plaintext secrets to disk by default.

## 1. Requirement Understanding (What Must Be Delivered)

We need a credentials system integrated into `si` that:

1. Stores secrets encrypted at rest in the repository (git/VCS-friendly).
2. Defaults to a separate private vault repo (submodule) so secrets are not stored in the main code repo.
3. Does not produce git noise:
   - Running the encrypt command twice must not rewrite already-encrypted lines.
   - Only an explicit flag should re-encrypt existing encrypted values.
4. Supports humans on macOS and Linux.
5. Supports Docker workflows following current Docker guidance:
   - Avoid baking secrets into images.
   - Avoid long-lived plaintext secrets on disk.
   - Prefer in-memory/ephemeral secret delivery to containers.
   - Treat Docker daemon access as highly privileged; be conservative with remote Docker.
6. Has an MVP audit trail of “what keys were accessed by whom (local user), when, and in which context”, without logging secret values.
7. Is simple by default, but designed to allow future expansions:
   - approval / “break glass” flows
   - passkey/device-based authorization (out of scope for MVP)
   - external providers (Vault, cloud secret managers) (out of scope for MVP)

The user suggested adopting the `dotenvx` interface; we must evaluate it and borrow lessons, but we are not required to be wire-compatible with its encryption format unless we choose to.

## 2. Docker Guidance Snapshot (What We Must Align With)

From Docker’s docs and best practices (paraphrased):

1. Runtime secrets are ideally delivered as files in an in-memory filesystem (Docker Swarm “secrets” are mounted in memory under `/run/secrets` and are not persisted to disk).
2. Docker avoids mapping secrets into environment variables by default because env vars can leak (for example between containers or via introspection).
3. Build-time secrets should use BuildKit secret mounts (`RUN --mount=type=secret`) rather than `ARG` or embedding into the build context or layers.
4. Protect the Docker daemon endpoint: anyone with access to the Docker API (including TLS client keys) effectively has root-equivalent control.

Implication for `si vault`:
- For long-lived containers, prefer an in-memory secrets directory (`/run/secrets` tmpfs) plus an inject step.
- For one-off “run a command with secrets”, prefer exec-time injection that does not persist values into container config.
- Detect and warn on insecure remote Docker configurations before transmitting plaintext secret material.

## 3. Lessons From Similar Tools (What We Should Copy, What To Avoid)

### Lessons from `dotenvx` (high-value)

1. Keep secrets reviewable and PR-friendly:
   - Encrypt values inline in `.env`-style files rather than generating a single monolithic vault blob.
2. Allow “encrypt-only” roles:
   - If encryption can be done with a public key, you can let someone add/update secrets without the ability to decrypt existing ones.
3. Keep keys out of the repo:
   - Private key material lives outside git (keys file, CI secret store, or keychain).
4. Support multi-file / multi-environment workflows:
   - People want `dev`, `staging`, `prod` separation, with predictable precedence and override rules.

### Lessons from SOPS / age / git-crypt / blackbox

1. Don’t invent crypto unless you must:
   - Use battle-tested primitives and libraries; keep format versioned.
2. Preserve formatting / ordering:
   - Reordering keys or normalizing quotes causes noisy diffs and human distrust.
3. Rotation is mandatory:
   - There must be an intentional “rotate/reencrypt” path.
4. “Git-integrated encryption” is never a full replacement for centralized secret management:
   - It’s great for repo-scoped dev/CI secrets; it’s weak for fine-grained RBAC, revocation, and centralized audit unless paired with a server.

### Lessons from Vault-style systems (for future design)

1. Audit logs are a first-class feature.
2. Policy gates and approvals are common for “production” secret access.
3. Dynamic secrets with leases/rotation are the ideal for production (but out of scope for a git-based MVP).

## 4. Options Survey (What We Could Use, and Why/Why Not)

### Option A (Adopt `dotenvx` directly, `si` wraps it)

Pros:
- Mature interface (`run`, `set`, `get`, `encrypt`, `decrypt`) and good docs.
- Inline encrypted `.env` format; PR-friendly.
- Public-key encrypt-only workflow.

Cons:
- Introduces a Node-based dependency chain (binary distribution, platform quirks, supply chain).
- Hard to deeply integrate with our Docker flows (tmpfs mounts, exec injection, audit) without duplicating logic anyway.
- We still must solve key storage (keychain) + auditing + “no git noise” semantics in our wrapper.

Verdict:
- Great inspiration and UX reference.
- Not ideal as a hard dependency for `si`.

### Option B (Use SOPS as the underlying engine)

Pros:
- Widely trusted, supports KMS backends, great for production-style workflows.

Cons:
- Additional external dependency and operational complexity.
- Default edits/encryptions often rewrite large parts of files (diff noise).
- `.env` UX is secondary; secrets are often in YAML/JSON.

Verdict:
- Strong optional backend later, not the simplest MVP default.

### Option C (Implement a minimal `si` native encrypted env store in Go) (Recommended)

Pros:
- Single distribution story (the `si` binary).
- Can enforce idempotent “don’t rewrite encrypted lines” behavior.
- Can deeply integrate with our Docker container lifecycle and exec flows.
- We can design audit logs and future approval hooks cleanly.

Cons:
- We own the format and long-term maintenance.
- Need to be careful to not create a homegrown-crypto mess.

Verdict:
- Best fit for “simplicity + tight integration”, if we keep crypto and format minimal and versioned.

### Option D (Use a developer password manager CLI as the backend: 1Password / Bitwarden / etc.)

Pros:
- Strong UX for humans, device-bound unlock flows, and often excellent team sharing.
- Usually includes some audit capabilities.
- Often “off disk” in the sense that secrets are retrieved on demand after device auth.

Cons:
- External dependency and vendor lock-in for core workflows.
- Not git-based; secrets drift from repo state unless carefully managed.
- Docker injection becomes a wrapper around vendor CLIs (and their auth/session).

Verdict:
- Good optional backend for teams already standardized on a manager.
- Not the default for a git-first workflow.

### Option E (Use a centralized secrets server: HashiCorp Vault / cloud secret managers)

Pros:
- Best-in-class for RBAC, revocation, centralized audit, and dynamic secrets.
- Naturally supports “approval” patterns and policy enforcement.

Cons:
- Operational complexity and network dependency.
- Not git-based; requires service uptime and access provisioning.

Verdict:
- Best long-term direction for “production” credentials.
- Out of scope for MVP, but we should design interfaces so it can be added later.

## 5. Recommended Architecture (V1)

Naming note:
- Recommended canonical command: `si vault`
- Provide an alias: `si creds` (for people who prefer explicit “credentials” wording)

### Command Family

Top-level:
- `si vault ...` (alias: `si creds ...`)

Minimum V1 commands:
1. `si vault init [--submodule-url <git-url>] [--vault-dir <path>] [--ignore-dirty] [--env <name>]`
2. `si vault fmt  [--vault-dir <path>] [--env <name>] [--all] [--check]`
3. `si vault encrypt [--vault-dir <path>] [--file <path>] [--env <name>] [--format] [--reencrypt]`
4. `si vault set <KEY> <VALUE> [--vault-dir <path>] [--file <path>] [--env <name>] [--section <name>] [--stdin] [--format]`
5. `si vault unset <KEY> [--vault-dir <path>] [--file <path>] [--env <name>] [--section <name>] [--format]`
6. `si vault get <KEY> [--vault-dir <path>] [--file <path>] [--env <name>] [--reveal]`
7. `si vault list [--vault-dir <path>] [--file <path>] [--env <name>]`
8. `si vault run [--vault-dir <path>] [--file <path>] [--env <name>] -- <cmd...>` (local process)
9. `si vault docker exec [--container <name|id>] [--vault-dir <path>] [--file <path>] [--env <name>] -- <cmd...>` (exec into an existing container with secrets injected for that exec only)
10. `si vault status [--vault-dir <path>] [--env <name>]` (submodule + trust + file diagnostics)

Intentional re-encryption:
- `si vault encrypt --reencrypt` (explicitly re-encrypt all encrypted values)
- `si vault rotate [--env <name>]` (generate new keypair + re-encrypt)

Optional but high value:
- `si vault audit tail|show [--json]`
- `si vault doctor` (validates key availability, file format, warns about insecure Docker host)
- `si vault set <KEY> --stdin` (avoid shell history)
- `si vault recipients list|add|remove` (multi-device/team access without sharing private keys)
- `si vault trust status|accept|forget` (TOFU trust for recipient set changes)
- `si vault submodule add|status` (explicit submodule management when `init` isn’t used)

### Storage Model (Git-based, Separate Vault Repo) (Chosen)

V1 store is a separate private git repository ("vault repo") added into the host repo as a git submodule.

Default layout in the host repo:
- `vault/` (git submodule checkout)
- `vault/.env.dev`
- `vault/.env.prod`

How `si vault` resolves the target file:
1. `--file <path>` (explicit file path; can be outside the host repo)
2. `--vault-dir <path>` + `--env <name>` (maps to `<vault-dir>/.env.<env>`)
3. Auto-discover `vault/` at the host repo root (submodule or directory)

Submodule behavior:
- The host repo pins the submodule commit (default git behavior).
- Recommended host repo `.gitmodules` setting: `ignore = dirty` for the `vault` submodule.
  - Rationale: vault edits should not constantly make the host repo look dirty.
  - Tradeoff: `git status` in the host repo will not warn you about uncommitted vault changes.
  - Mitigation: `si vault status` must report the vault repo dirty/clean state regardless of `ignore = dirty`.
  - Example:
    ```ini
    [submodule "vault"]
        path = vault
        url = git@github.com:org/project-vault.git
        ignore = dirty
    ```

Private key material is never committed. Defaults:
- macOS: stored in Keychain (preferred)
- Linux: stored in Secret Service (preferred), else a locked-down file under `~/.si/vault/keys/` (fallback)
- CI: provided via environment variables

Multi-environment support:
- Canonical convention: one file per environment, named `.env.<env>` (e.g. `.env.dev`, `.env.prod`).
- Default environment: `dev` (configurable via settings).

### File Format Choice: Dotenv vs TOML vs YAML

We can support TOML instead of dotenv, but the “no git noise” requirement strongly favors a line-oriented format that we can patch without reserializing the whole document.

Dotenv (`.env.<env>` in the vault repo) (Recommended MVP default):
- Pros:
  - Simple 1-line-per-secret model maps directly to environment variables and Docker injection.
  - Easy to implement a line-preserving editor so “encrypt twice = no diff”.
  - Matches the `dotenvx` mental model.
- Cons:
  - No structure beyond flat keys; harder to add per-secret policy without conventions.
  - Dotenv parsing has edge cases (quotes/escapes, `export`, duplicates).

TOML (`vault.si.toml` or similar) (Viable, likely best “structured” alternative):
- Pros:
  - Clear structure for environments/tables (`[dev]`, `[prod]`, `[policies]`).
  - Already aligned with `si`’s existing config story (TOML in `~/.si/settings.toml`).
  - Less ambiguous than YAML for quoting/types.
- Cons:
  - Most TOML libraries re-encode and can cause formatting churn (git noise) unless we implement a line-preserving writer/patcher.
  - Multi-line strings and rich types add complexity and parsing hazards; for secrets we should restrict to strings.

YAML (`.yaml`) (Not recommended for MVP):
- Pros:
  - Familiar for many teams; supports nested structure.
- Cons:
  - High risk of formatting churn and subtle parse differences (indentation, implicit typing, anchors/merges).
  - Harder to make “run twice = no diff” without a bespoke line-preserving YAML patcher.

Recommendation:
- MVP uses dotenv as the canonical store.
- If we add a second format, add TOML next, but only once we can guarantee idempotent, minimal-diff edits.
- Avoid YAML unless there’s a strong external requirement; if needed, treat YAML as “read-only import” first.

### Repo Placement (Chosen: Separate Vault Repo Submodule)

Decision:
- Use a separate private "vault repo" for encrypted `.env.<env>` files.
- Add it to the host repo as a git submodule at `vault/` by default.
- Commit/push secret changes in the vault repo; only rarely update the host repo's pinned submodule commit.

Recommended host repo `.gitmodules` config:
- Set `ignore = dirty` for the `vault` submodule.
- This keeps the host repo `git status` clean even if the vault repo has uncommitted edits.
- Tradeoff: you must rely on `si vault status` (or `git -C vault status`) to notice uncommitted vault changes.

Alternatives (supported via `--file` / `--vault-dir`, but not the default):
1. Local-only vault file (not shared):
- Store secrets under `~/.si/vault/<repo-id>/vault.env` (or a gitignored file like `.env.local`).
- Benefits: nothing to commit; simplest for solo dev.
- Costs: not team-shareable; onboarding requires manual setup.

2. CI-only secrets (GitHub Actions / CI secret store) + local file for dev:
- Benefits: no repo secret file needed; good for production deploy secrets.
- Costs: secret changes are not PR-reviewable; drift between dev and CI is common.

3. External secret manager as source of truth (Vault / cloud secret managers / 1Password / Bitwarden):
- Benefits: best for RBAC, revocation, centralized audit, and approvals (depending on provider).
- Costs: operational dependency; not “git-based”; more setup.

### Initialization & Chain of Trust (First Install / First Use)

We need a clean first-run story that:
- bootstraps a vault repo (submodule) for this host repo
- creates a device keypair
- prevents “encrypting to the wrong recipients” and makes recipient changes explicit

Recommended model: TOFU (trust-on-first-use), similar to `ssh known_hosts`.

Trust store:
- Local-only, non-secret metadata under `~/.si/vault/trust.json` (or similar).
- Keyed by host repo identity + `vault-dir` + `env` (because files are per-environment).
- Records:
  - vault repo URL (from `.gitmodules` or `git -C <vault-dir> remote get-url origin`)
  - recipient-set fingerprint (normalized set of recipient public keys)

Host repo bootstrap (`si vault init` when `vault/` submodule does not exist):
1. Require `--submodule-url <git-url>` (or prompt interactively) and optionally `--vault-dir` (default: `vault/`).
2. Add the vault repo as a git submodule and (optionally) write `ignore = dirty` into `.gitmodules`.
3. Initialize/update the submodule working tree.
4. Create the target env file (default: `vault/.env.dev`, or `vault/.env.<env>`):
   - write `# si-vault:v1`
   - write one `# si-vault:recipient ...` line (this device's public key)
   - write an initial formatting skeleton (section headers) only if `--format` is requested
5. Record the recipient-set fingerprint into the local trust store for this repo.

Using an existing host repo (`si vault init` when the vault submodule exists):
1. Ensure the submodule is initialized/checked out (fail with clear instructions if not).
2. Resolve the target env file: `--file` or `<vault-dir>/.env.<env>` (default env: `dev`).
3. Parse recipients from that env file and compute the stable fingerprint.
4. If this repo has no trusted fingerprint recorded yet:
   - interactive: show fingerprint + recipients and ask user to accept
   - non-interactive: fail with instructions to run `si vault trust accept`
5. If fingerprint changed since last trust, or vault repo URL changed:
   - interactive: show added/removed recipients and the old/new repo URL and ask to accept
   - non-interactive: fail unless an explicit `--accept-trust-change`-style flag is provided

Why TOFU:
- No server required.
- Prevents silent recipient additions (a common git-based secrets footgun).
- Catches suspicious vault repo URL changes in `.gitmodules`.

CI / non-interactive guidance:
- Avoid “auto-accept trust changes” in CI.
- Pin an expected fingerprint via env var (e.g. `SI_VAULT_TRUST_FINGERPRINT=...`) or CLI flag (e.g. `--trust-fingerprint ...`) so CI fails if recipients drift unexpectedly.

### File Format (Line-Preserving, Idempotent)

We use a dotenv-compatible line format and preserve:
- ordering
- comments
- whitespace
- quoting as much as possible

Proposed conventions:

1. A metadata header block (comments), not required but recommended:
   - store format version
   - store recipient public key(s) (so encryption is possible without private key)
2. Recipients are stored as comment lines to avoid polluting runtime env:
   - `# si-vault:recipient age1...`
   - `# si-vault:recipient age1...`
3. Encrypted values use a recognizable prefix:
   - `encrypted:si:v1:<base64payload>`
4. Sections are comment-only and optional, but recommended for readability:
   - Divider line: `# ------------------------------------------------------------------------------`
   - Section header: `# [stripe]` (lowercase slug, e.g. `stripe`, `workos`, `aws`)
   - Keys belong to the most recent section header until the next section header.

Canonical formatting style (enforced only when requested):
- Header:
  - `# si-vault:v1` as the first line
  - one or more `# si-vault:recipient ...` lines next
  - exactly one blank line after the header block
- Sections:
  - each section begins with a divider line, then a section header line
  - exactly one blank line between sections
  - comments inside a section start with `# ` (hash + single space)
- Keys:
  - `KEY=value` with no spaces around `=`
  - values may be plaintext only transiently; the goal is to end in `encrypted:si:v1:...`

Formatting command:
- `si vault fmt` rewrites the file to the canonical style (whitespace, blank lines, divider/header normalization).
- `si vault fmt` does not re-encrypt values; it only changes layout and ordering.
- `--check` makes `fmt` fail if changes would be made (CI-friendly).
- `--all` formats all files matching `.env.*` in the vault dir.
- Mutating commands accept `--format` to run `fmt` after the minimal change is applied.

Section-aware inserts:
- `si vault set --section stripe ...` updates/inserts the key inside the `[stripe]` section.
- If the section does not exist, `set` appends a new section block (divider + section header) to the end of the file.
- Without `--section`, `set` should append new keys with the smallest possible diff (no new section scaffolding).

Example (canonical formatting):
```dotenv
# si-vault:v1
# si-vault:recipient age1exampleexampleexampleexampleexampleexampleexampleexampleexample

# ------------------------------------------------------------------------------
# [stripe]
STRIPE_API_KEY=encrypted:si:v1:...
STRIPE_WEBHOOK_SECRET=encrypted:si:v1:...

# ------------------------------------------------------------------------------
# [workos]
WORKOS_API_KEY=encrypted:si:v1:...
WORKOS_CLIENT_ID=encrypted:si:v1:...
```

Idempotency rule:
- If a value already matches `^encrypted:si:v\\d+:`, we DO NOT rewrite it unless `--reencrypt` is provided.
- Without `--format`, commands must preserve all unrelated lines byte-for-byte to prevent git diff noise.

Additional “no git noise” rules:
- Do not emit timestamps, random key IDs, or rewrapped headers on normal runs.
- If the file already contains recipient lines, do not reorder or normalize them.
- When updating a single key via `set`, only that line changes (plus an insertion if the key did not exist).

### Crypto Model (Keep It Boring)

Goals:
- Public-key encryption allows encrypt-only.
- Decrypt requires private key.
- Format is versioned and supports rotation.

Implementation choices for MVP:
- Use established Go crypto or a well-reviewed library (avoid novel schemes).

Recommended MVP choice:
- Use age recipients (X25519) with a per-value envelope, embedded as base64 in a single line.

Why:
- Encrypt-only is possible with public keys (recipients).
- Decrypt requires a private key (per device), which can be stored in OS keychain/keyring.
- Multi-recipient is straightforward (team sharing without distributing one shared private key).

Fallback (if we reject age as a dependency):
- X25519 + AEAD envelope implemented via Go stdlib + `x/crypto` with a versioned payload.

Notes:
- Adding a new recipient does not rewrite existing ciphertext automatically. Existing secrets become readable by the new recipient only after an explicit `--reencrypt` run (intentional git noise).

Key rotation:
- `si vault rotate` generates a new keypair and (with confirmation) re-encrypts all values.

### Integration With Existing `si` Docker Workflows

The goal is that `si` can do Docker credentials injection without users having to learn a separate ecosystem.

Planned integration points:
1. Add optional flags to container lifecycle commands:
   - `si spawn ... --vault [--vault-dir <path>] [--vault-env <env>] [--vault-secret KEY]...`
   - `si dyad spawn ... --vault [--vault-dir <path>] [--vault-env <env>] [--vault-secret KEY]...`
   - `si run <container> ... --vault ...` (inject for exec/run command only)
2. Keep default safe behavior:
   - do not set container environment at create time unless explicitly requested (`--persist-env` or similar)
   - prefer exec-time injection or `/run/secrets` tmpfs injection
3. Ensure we never accidentally pass secret values through `si` logs/printfs.

### Team / Multi-Device Workflow (V1)

Goal: avoid sharing a single private key across the team.

1. Each device generates its own private key (stored in OS keychain/keyring when possible).
2. The repo stores one or more recipient public keys (in the encrypted env header comments).
3. Encrypt operations use the recipient list; decrypt operations use a local private key.
4. When a new recipient is added:
   - commit the recipient public key
   - run `si vault encrypt --reencrypt` intentionally to make existing values decryptable by the new recipient

### Docker Integration (Runtime)

We support two patterns, with safe defaults:

1. Exec-time injection (default for `si vault docker exec`):
   - Decrypt on host only.
   - Inject as environment variables for the exec’d process only (not persisted into container config).
   - This is simplest and avoids writing plaintext secrets to host disk.

2. In-memory secret files (opt-in, aligns with Docker “secrets” ergonomics):
   - Ensure containers have a tmpfs mount at `/run/secrets` (for containers created by `si`).
   - Inject secrets by `docker exec -i` writing files into `/run/secrets/<KEY>` (umask 077).
   - Optionally delete after command completion.

Remote Docker safety:
- Before transmitting decrypted secrets, detect insecure Docker endpoints:
  - `DOCKER_HOST=tcp://...` without TLS verification, or other obvious insecure configs.
- Default behavior: refuse with an actionable error unless `--allow-insecure-docker-host` is specified.

Docker socket risk note:
- Any container with host Docker socket access can generally escalate to root on the host; do not treat “container boundary” as a security boundary in that configuration.

### Auditing (MVP)

Local-only audit log, JSONL:
- Path: `~/.si/logs/vault.log` (configurable)
- Events:
  - `encrypt` (keys added/updated)
  - `decrypt` (a secret was revealed to user or injected)
  - `inject_docker_exec` (which container, which keys)
  - `rotate` / `reencrypt`
- Fields:
  - timestamp, repo root, host user, uid/gid, command, target container, keys accessed, success/failure, error class
- Never log secret values.

### Future (Out of Scope for MVP): Approval + Passkeys

Design for a policy hook:
- A pre-decrypt authorization callback (local plugin or external service).
- Allow future enforcement like:
  - “prod env decrypt requires passkey approval”
  - “decrypt requires 2-person approval for selected keys”
  - “decrypt emits signed audit event”

Passkeys/WebAuthn path (future):
- Use a local OS prompt (macOS) or FIDO2 device for approval.
- Store key material in hardware-backed keystore when possible.

## 6. Global File Boundary Contract (For Implementation Agent)

Allowed paths (expected changes):
- `tools/si/main.go` (dispatch wiring)
- `tools/si/util.go` (help text)
- `tools/si/settings.go` (add `[vault]` paths/config)
- `tools/si/vault*.go` (new command handlers)
- `tools/si/*vault*_test.go` (tests)
- `tools/si/internal/vault/**` (new packages: parser/crypto/keychain/audit/formatter/submodule)
- `docs/VAULT.md` (new)
- `docs/SETTINGS.md` (update)
- `README.md` (light mention)
- `.gitmodules` (submodule configuration, including `ignore = dirty`)
- `tickets/creds-management-integration-plan.md` (this file)

Disallowed paths (unless truly required):
- `agents/**` (avoid unrelated runtime changes)

Secret handling rules:
- Never write decrypted values to disk by default.
- Never print secret values unless explicitly requested via flags (`--reveal`, `--stdout`).
- Ensure audit logs never include secret values.

## 7. Definition of Done

The work is complete when all are true:

1. `si vault` is present in `si --help` and dispatch (with `si creds` as an alias).
2. A host repo can be initialized with `si vault init` such that a `vault/` submodule exists and `.env.<env>` files can be created/committed in the vault repo.
3. `si vault fmt` exists and enforces one canonical `.env` style; `fmt --check` works for CI.
4. Encrypting twice is idempotent: existing encrypted lines are not rewritten.
5. `--reencrypt` intentionally rewrites all encrypted values.
6. Private key material is never committed and defaults to keychain/keyring storage when available.
7. `si vault run` and `si vault docker exec` work on macOS + Linux and do not write plaintext secrets to disk by default.
8. Docker integration has a secure-default stance for remote daemon connections.
9. Audit log exists, includes key access events, and redacts values.
10. Tests cover parsing, idempotency, encryption/decryption roundtrips, formatting, and Docker command shaping (unit-level).
11. Docs include a minimal quickstart, threat-model notes, and operational guidance.

## 8. Workstream Status Board

| Workstream | Status | Owner | Branch | PR | Last Update |
|---|---|---|---|---|---|
| WS-00 Contracts | In Progress | codex | main |  | 2026-02-08 |
| WS-01 CLI Entry & Help | Done | codex | main |  | 2026-02-08 |
| WS-02 File Format + Parser (line-preserving) | In Progress | codex | main |  | 2026-02-08 |
| WS-03 Crypto Engine | In Progress | codex | main |  | 2026-02-08 |
| WS-04 Key Storage + Trust Store (Keychain/Keyring + CI env) | In Progress | codex | main |  | 2026-02-08 |
| WS-05 Vault Repo Submodule (init, discovery, status) | In Progress | codex | main |  | 2026-02-08 |
| WS-06 Formatter (sections + canonical style) | In Progress | codex | main |  | 2026-02-08 |
| WS-07 Docker Runtime Injection | Not Started |  |  |  | 2026-02-07 |
| WS-08 Docker Build Secrets | Not Started |  |  |  | 2026-02-07 |
| WS-09 Auditing | In Progress | codex | main |  | 2026-02-08 |
| WS-10 Tests | Not Started |  |  |  | 2026-02-07 |
| WS-11 Docs | Not Started |  |  |  | 2026-02-07 |
| WS-12 Future Approvals (design only) | Not Started |  |  |  | 2026-02-07 |

Status values: `Not Started | In Progress | Blocked | Done`

## 9. Independent Parallel Workstreams (Detailed)

## WS-00 Contracts (Interface-first foundation)

Deliverables:
1. Define internal interfaces:
   - `SecretStore` (read/write dotenv-style encrypted file)
   - `KeyProvider` (keychain, env, file)
   - `Decrypter`/`Encrypter` (versioned)
   - `AuditSink` (JSONL writer)
2. Define DTOs used by CLI handlers and Docker injectors.

Notes:
- Keep these interfaces extremely small; feature creep here explodes complexity.

## WS-01 CLI Entry & Help

Deliverables:
1. Wire `si vault` (and alias `si creds`) in `tools/si/main.go`.
2. Add help text in `tools/si/util.go` consistent with existing style and colorization.

Acceptance:
- `si vault --help` is clear enough to use without docs for the common path.

## WS-02 File Format + Parser (line-preserving)

Deliverables:
1. Parser that:
   - keeps original lines and comments
   - identifies KEY=VALUE pairs and whether VALUE is encrypted
   - updates only specific keys without reformatting other lines
2. A stable writer that:
   - preserves newline style
   - only changes necessary lines
3. Idempotency tests:
   - encrypting twice yields byte-identical output (unless `--reencrypt`)

Corner cases:
- duplicate keys in file (choose last-wins but preserve earlier lines)
- multi-line values (either explicitly unsupported for MVP with clear error, or handled carefully)
- export prefixes (`export KEY=...`)
- quoted values with escapes

## WS-03 Crypto Engine

Deliverables:
1. Implement V1 encryption/decryption with format versioning.
2. Explicit re-encrypt flow.
3. Key rotation flow.

Notes:
- Keep payload ASCII-safe (base64).
- Ensure zero secret values are printed by default.
- Provide a `--reveal` / `--stdout` style flag for explicit display.

## WS-04 Key Storage + Trust Store (Keychain/Keyring + CI env)

Deliverables:
1. Key resolution precedence:
   - CLI flags (for CI)
   - env vars (CI)
   - OS keychain/keyring
   - fallback local file (explicit opt-in)
2. `si vault init` stores private key to keychain/keyring when possible.
3. Implement TOFU trust store and commands:
   - `si vault trust status`
   - `si vault trust accept`
   - `si vault trust forget`
4. `si vault doctor` indicates where the key is currently sourced from and whether the repo is trusted.

Notes:
- Even “keychain” is still “on disk”; the real goal is “no plaintext secrets written as regular files”.

## WS-05 Vault Repo Submodule (init, discovery, status)

Deliverables:
1. Submodule bootstrap:
   - `si vault init --submodule-url <git-url>` adds the vault repo as a submodule at `vault/` (or `--vault-dir <path>`).
   - When `--ignore-dirty` is set, write `ignore = dirty` into host repo `.gitmodules` for that submodule.
   - Optional explicit command: `si vault submodule add|status`.
2. Vault discovery and env mapping:
   - Auto-discover `<repoRoot>/vault` if `--vault-dir` is not provided.
   - Resolve `--env <name>` to `<vault-dir>/.env.<env>` (default env: `dev`).
3. `si vault init` creates env files when missing:
   - writes `# si-vault:v1` + `# si-vault:recipient ...` header
   - records TOFU trust fingerprint for this repo/env
4. `si vault status` provides diagnostics:
   - vault dir, vault repo URL, pinned commit, dirty state
   - trust state (trusted/untrusted + fingerprint)
   - target env file existence

Optional helper:
- `si vault scan` to detect accidental plaintext secrets (still worth having even if secrets are encrypted).

## WS-06 Formatter (sections + canonical style)

Deliverables:
1. Implement `si vault fmt`:
   - `--check` (CI mode)
   - `--all` (format all `.env.*` files in the vault dir)
   - `--env`/`--file` targeting
2. Canonical section style support:
   - `# ------------------------------------------------------------------------------`
   - `# [stripe]`, `# [workos]`, etc
   - stable spacing, blank line rules, newline at EOF
3. `fmt` must not re-encrypt values; it only rewrites layout.
4. Add `--format` to mutating commands (`set`, `unset`, `encrypt`, `rotate`) to run `fmt` after the minimal edit.

## WS-07 Docker Runtime Injection

Deliverables:
1. `si vault docker exec --container X -- <cmd...>`:
   - decrypt on host
   - inject into exec only (default)
2. Optional: `--as-files` mode that writes to `/run/secrets`.
3. Guardrails:
   - refuse or warn for insecure `DOCKER_HOST` transport before injecting secrets.

## WS-08 Docker Build Secrets

Deliverables:
1. Extend `si image build` to accept `--secret <KEY>` and pass BuildKit secrets appropriately.
2. Provide documentation snippet:
   - `RUN --mount=type=secret,id=KEY ...`

## WS-09 Auditing

Deliverables:
1. JSONL audit writer.
2. Record events for decrypt/inject/reveal.
3. Include enough context to be useful without leaking secrets.

## WS-10 Tests

Deliverables:
1. Parser stability + idempotency tests.
2. Crypto roundtrip tests (including wrong-key failure).
3. CLI flag parsing tests.
4. Docker injection command shaping tests (no actual docker required for unit tests).

## WS-11 Docs

Deliverables:
1. `docs/VAULT.md` (new) with:
   - threat model + do/don’t list
   - quickstart
   - CI usage
   - Docker usage patterns
2. Update `docs/SETTINGS.md` with `[vault]` section.
3. Add a minimal mention in `README.md`.

## WS-12 Future Approvals (design only)

Deliverables:
1. Define policy hook points:
   - before decrypt
   - before inject to docker
2. Sketch passkey-based local approval flow.
3. Decide on an extension mechanism:
   - local executable hook
   - gRPC/HTTP approval server

## 10. Security Notes / Threat Model (MVP)

Non-exhaustive risks and mitigations:

1. Secrets in terminal scrollback:
   - Default is non-revealing; require explicit `--reveal`.
2. Secrets in shell history:
   - Prefer `si vault set KEY --stdin` rather than `si vault set KEY value` when used interactively.
3. Secrets in process list:
   - Avoid passing secrets as CLI args to subprocesses.
4. Secrets in Docker inspect:
   - Prefer exec-time env injection or `/run/secrets` injection over setting container env at create time.
5. Remote Docker:
   - Refuse insecure transports by default.
6. Host compromise:
   - If host is compromised, secrets can be decrypted; git-based encryption does not solve endpoint security.

## 11. Non-goals (MVP)

Explicitly not targeting in V1:
- Centralized RBAC and revocation (Vault-like controls).
- Multi-party approvals / passkey auth (design only in WS-11).
- Automatic secret rotation with leases/dynamic credentials.
- Full dotenv feature parity (we only implement what `si` needs).
- Windows support (unless requested).

## 12. MVP Recommendation Summary

Implement `si vault` natively in Go with a dotenv-like encrypted store inspired by `dotenvx`:
- Inline encrypted values (PR-friendly).
- Public-key-based encryption supports encrypt-only workflows.
- Strict idempotency prevents git noise.
- Docker injection defaults avoid persisting secrets into container config.
- Add a local audit log and clean extension points for future approvals/providers.
