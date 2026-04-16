# Workspace Rules

- Repositories rooted under underscore-prefixed top-level directories, such as `_paases/<repo>`, `_agentic/<repo>`, or any similar `_<name>/<repo>` pattern, are reference-only. They are not owned working repos, are outside the default scope of modifications, and must not be modified unless the user explicitly overrides this rule for a specific task.
- Those underscore-prefixed directories may be scanned, indexed, searched, or read for reference, but they must be treated as external/unowned code unless the user explicitly says otherwise.

# Release Discipline

- Use one single SI repository version across the whole system rather than separate versions for the gateway, REST API, storage schema, SDK surfaces, or other SI-owned runtime layers.
- Every commit that changes SI must bump the workspace patch version in the same commit. Do not batch multiple commits under one unchanged patch version and do not defer the bump.
- Bump the workspace minor version only when publishing a new SI release to GitHub Releases or another distribution channel such as npm or Homebrew. Reset the patch component to `0` in that release commit.
- Create git tags only for those minor release commits; do not tag patch-only commits.
- Release notes for each minor release must cover every commit and patch-version bump since the previous minor release.
- When the SI version changes, that one version change applies everywhere in SI at once.
- After bumping the version, rebuild the SI binary on this host and update the mapped installed locations that SI uses, including the repo-local binary and the host-installed binary when applicable.
- Prefer rebuild paths that reuse cached Cargo artifacts so incremental follow-up builds stay fast.

# Secrets And Credential Access

- For any credentials, secret reads, secret writes, bootstrap flows, or operator secret work, always use `si fort` rather than calling Vault directly or bypassing Fort.
- Do not jump around Fort by using raw `si vault` commands, ad-hoc local secret files, or alternate secret access paths when `si fort` is the supported workflow.
- Keep Fort's guiding principles in mind for all secret-related work, especially the public HTTPS runtime path, file-backed token handling, scoped policy enforcement, and the rule that Fort remains the auth boundary over SI Vault-backed `safe` data.
- `safe` is the literal name of the separate repository that stores encrypted secret material; it is not a generic label or an arbitrary local folder.

# Node Package Manager Discipline

- For SI-owned web or Node-based workspaces, use `pnpm` rather than `npm` for local dependency installation, script execution, lockfile generation, and release-packaging workflows.
- Prefer `corepack pnpm ...` in docs and automation when that makes the toolchain requirement clearer.
- Do not introduce or commit `package-lock.json` in those workspaces; use `pnpm-lock.yaml` instead.
- If a workspace already enforces `pnpm`, preserve and extend that enforcement rather than weakening it.
- External npm-registry references are allowed when they refer to publishing or installing packages, but repo-local development/build instructions should default to `pnpm`.

# Naming Convention Discipline

- When introducing new SI-owned names for binaries, services, IDs, environment variables, sockets, files, directories, tmux sessions, API resources, or other runtime surfaces, namespace them with the full `si` prefix rather than shortened fragments.
- Do not introduce abbreviated namespace prefixes such as `nuc`, `wrk`, `sess`, `evt`, `svc`, or similar short forms when the name is intended to represent an SI-owned concept or resource.
- Prefer full names such as `si-nucleus`, `si-worker`, `si-session`, `si-run`, `si-event`, and `SI_NUCLEUS_*` over compressed tokens.
- This rule applies both to user-facing names and internal implementation identifiers when those identifiers may surface in logs, paths, IDs, configuration, or adjacent-repo integrations.
- For Viva-managed app Docker runtime names, use `viva-<app_code>-<env>-<component>[-<slot>]`.
- Use `dev` and `prod` literally for the environment segment.
- Use real component names such as `web`, `api`, `worker`, `postgres`, `walg`, `db-init`, and `databasus`.
- Add the slot segment last, and only for blue/green swappable runtime components, such as `viva-rm-prod-api-blue` or `viva-ls-prod-web-green`.
- Do not use `blue` or `green` as component names. For example, a frontend service should use `web-blue` or `web-green`, not a component named `blue`.
