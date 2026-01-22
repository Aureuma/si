# ReleaseParty product spec (MVP)

## Mission

ReleaseParty generates high-quality release blog posts for open-source projects and opens them as pull requests in a destination repository (often a blog/docs repo), triggered by GitHub Releases or tags. The human maintainer reviews and merges.

Key constraints:
- Operates as a GitHub App (**ReleaseParty Acolyte**) using installation tokens.
- No manual copy/paste required for the normal flow.
- Produces Markdown with predictable front matter and path templates.

## Inputs

- GitHub Release metadata (tag, name, body, URL).
- Git history between releases/tags (`compare base...head`).
- Optional:
  - PR metadata for merged PRs in the range (later).
  - `CHANGELOG.md` snippet at `ref=head` (later).
  - Diff summary (later).

## Outputs

- Markdown post (front matter + sections).
- PR in destination repo:
  - branch: `release-party/<project>-<tag>-<timestamp>`
  - file path: template (default: `posts/{date}-{release_tag}.md`)

## Core flows

### 1) Installation

- User installs GitHub App.
- Webhook `installation.created` persists installation record.

### 2) Release trigger → PR

- Webhook `release.published` fires with installation + repo.
- Backend:
  1. Loads project config for `(installation_id, repo_full_name)` (DB for MVP).
  2. Detects base tag (previous release/tag) and compares commits.
  3. Generates markdown (template-first; LLM provider later).
  4. Creates branch, upserts markdown file, opens PR.

## Configuration model (MVP)

Per tracked repository:
- `blog_repo`: `owner/name` where PR should be opened (can be same repo).
- `path_template`: e.g. `posts/{date}-{release_tag}.md`
- `base_branch`: destination base branch (default `main`)
- `front_matter_format`: `yaml` (MVP) / `toml` (later)

## Non-goals (MVP)

- Billing/subscriptions.
- Multi-tenant auth (use GitHub installation as identity; add UI auth later).
- Full PR enrichment (labels/reviews/comments).

## Parity with Aureuma Python prototype

The Python prototype (`/home/shawn/Development/Aureuma/app/rp`) included:
- Projects + posts storage
- GitHub REST client helpers
- Release context builder + post processing
- Manual generate + publish flows

This repo reimplements the essential pipeline in Go.
