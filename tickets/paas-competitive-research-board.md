# AI PaaS Competitive Research Board

Date created: 2026-02-17
Owner: Unassigned
Status: In Progress

Purpose:
- Track deep competitor discovery across repo analysis and web user sentiment.
- Provide a single update surface for multiple concurrent agents.

Status legend:
- `Not Started`
- `In Progress`
- `Blocked`
- `Done`

## 1. Platform Tracking Matrix

| Platform | Repo Cloned | Docs Reviewed | User Feedback Collected | Architecture Analyzed | Strengths Synthesized | Weaknesses Synthesized | Status | Owner | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Coolify | No | No | No | No | No | No | Not Started | Unassigned | |
| CapRover | No | No | No | No | No | No | Not Started | Unassigned | |
| Dokploy | No | No | No | No | No | No | Not Started | Unassigned | |
| Dokku | No | No | No | No | No | No | Not Started | Unassigned | |
| Easypanel | No | No | No | No | No | No | Not Started | Unassigned | |
| Portainer | No | No | No | No | No | No | Not Started | Unassigned | |
| Tsuru | No | No | No | No | No | No | Not Started | Unassigned | |
| SwiftWave | No | No | No | No | No | No | Not Started | Unassigned | Canonical upstream: `https://github.com/swiftwave-org/swiftwave` |
| Kamal | No | No | No | No | No | No | Not Started | Unassigned | Canonical upstream: `https://github.com/basecamp/kamal` |
| OpenClaw patterns | Yes | Yes | Partial | Partial | Partial | Partial | In Progress | Codex | Local repo context already loaded |

## 2. Evidence Collection Rules

Every evidence entry must include:

1. URL
2. Source type (`github_issue`, `github_discussion`, `reddit`, `hn`, `blog`, `docs`, `code`)
3. Date (absolute date)
4. Platform
5. Sentiment (`positive`, `negative`, `mixed`, `neutral`)
6. Key quote summary (paraphrased)
7. Feature/topic tag (`deploy`, `rollback`, `secrets`, `ux`, `pricing`, `stability`, `scale`, `alerts`, `ai`)
8. Confidence (`high`, `medium`, `low`)

Hard rule:
- Do not add simulated findings without a direct source URL and date.

## 3. Findings Template Per Platform

Use this block for each platform once research begins:

### Platform: <name>

Repo:
- <url>

Docs:
- <url>

Strengths users repeatedly mention:
1. TBD
2. TBD
3. TBD

Weaknesses users repeatedly mention:
1. TBD
2. TBD
3. TBD

Missing features users request:
1. TBD
2. TBD
3. TBD

Architecture lessons we should copy:
1. TBD
2. TBD
3. TBD

Architecture lessons we should avoid:
1. TBD
2. TBD
3. TBD

## 4. Evidence Log

| Date | Platform | Source Type | URL | Sentiment | Topic Tag | Insight Summary | Confidence | Added By |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 2026-02-17 | OpenClaw patterns | code | ../openclaw/docs/concepts/architecture.md | positive | architecture | Single gateway control-plane model is clearly documented and operationally focused. | medium | Codex |
| 2026-02-17 | OpenClaw patterns | code | ../openclaw/docs/concepts/session-tool.md | positive | ai | Session tooling gives strong patterns for multi-agent orchestration and scoped access. | medium | Codex |

## 5. Synthesis Summary (Update after each batch)

Current top themes:

1. TBD
2. TBD
3. TBD

Current top gaps in market:

1. TBD
2. TBD
3. TBD

Implications for `si paas` priorities:

1. TBD
2. TBD
3. TBD
