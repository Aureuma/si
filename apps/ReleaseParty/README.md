# ReleaseParty

ReleaseParty is a GitHub App (registered as **ReleaseParty Acolyte**) that helps open-source maintainers ship polished release blog posts.

On every release/tag, ReleaseParty:
1. Collects release context (release metadata, commit history between tags, PRs, changelog, diff).
2. Generates a Markdown post (front matter + structured sections).
3. Opens a PR to the configured destination repo/path so maintainers can review, tweak, and merge.

This repository is a **fresh reimplementation** of the earlier Python prototype located at:
`/home/shawn/Development/Aureuma/app/rp` (FastAPI/SQLModel).

## Repo layout (planned)

- `backend/` — Go API + GitHub App webhook receiver + workers.
- `frontend/` — SvelteKit UI for configuration and previews.
- `docs/` — product spec, webhook/event contracts, operational runbooks.

## MVP scope

- GitHub App webhook endpoint (`release`, `installation`, `installation_repositories`).
- Store installations + project configs in SQLite.
- Generate a Markdown post from GitHub context (template-based; LLM provider pluggable).
- Create/update file + open PR in target repo.

