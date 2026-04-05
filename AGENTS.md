# Workspace Rules

- Repositories rooted under underscore-prefixed top-level directories, such as `_paases/<repo>`, `_agentic/<repo>`, or any similar `_<name>/<repo>` pattern, are reference-only. They are not owned working repos, are outside the default scope of modifications, and must not be modified unless the user explicitly overrides this rule for a specific task.
- Those underscore-prefixed directories may be scanned, indexed, searched, or read for reference, but they must be treated as external/unowned code unless the user explicitly says otherwise.

# Release Discipline

- After each minor SI improvement or fix that should result in a fresh usable SI binary, bump the workspace patch version.
- After bumping the patch version, rebuild the SI binary on this host and update the mapped installed locations that SI uses, including the repo-local binary and the host-installed binary when applicable.
- Prefer rebuild paths that reuse cached Cargo artifacts so incremental follow-up builds stay fast.
- Patch versions may be bumped sequentially as often as needed; do not avoid a patch bump merely because another recent patch bump already happened.

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
