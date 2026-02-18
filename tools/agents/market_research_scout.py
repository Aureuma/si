#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from email.utils import parsedate_to_datetime
from pathlib import Path
from typing import Any
from urllib.request import Request, urlopen
import xml.etree.ElementTree as ET

DEFAULT_FEEDS = [
    "https://news.ycombinator.com/rss",
    "https://techcrunch.com/category/startups/feed/",
    "https://github.blog/changelog/feed/",
    "https://www.producthunt.com/feed",
    "https://www.saastr.com/feed/"
]

KEYWORD_WEIGHTS: dict[str, tuple[list[str], int]] = {
    "pricing": (["pricing", "price", "billing", "subscription", "usage-based", "seat"], 4),
    "ai_automation": (["ai", "agent", "automation", "copilot", "llm", "assistant"], 4),
    "developer_workflow": (["release", "changelog", "github", "workflow", "developer", "devops"], 3),
    "compliance": (["security", "compliance", "soc2", "audit", "gdpr", "hipaa"], 3),
    "go_to_market": (["growth", "distribution", "marketplace", "partnership", "channel"], 2),
}

PLAYBOOK_BY_CATEGORY = {
    "pricing": {
        "opportunity": "Pricing and packaging differentiation",
        "plan": "Evaluate a usage-metered or annual-commit package variant for ReleaseMind PaaS.",
        "experiment": "Run a pricing-page A/B copy test with value metric framing and monitor conversion intent."
    },
    "ai_automation": {
        "opportunity": "AI-first release operations lane",
        "plan": "Package AI-assisted release generation and post-publish QA checks as a premium workflow.",
        "experiment": "Build one AI workflow add-on and test adoption with existing active users."
    },
    "developer_workflow": {
        "opportunity": "Developer workflow integration expansion",
        "plan": "Prioritize tighter GitHub/CI integration with faster release-note draft loops.",
        "experiment": "Ship one integration enhancement and track week-over-week usage lift."
    },
    "compliance": {
        "opportunity": "Compliance-ready release communications",
        "plan": "Productize audit-grade release evidence and governance controls for enterprise buyers.",
        "experiment": "Prototype a compliance export view and collect design-partner feedback."
    },
    "go_to_market": {
        "opportunity": "Channel and distribution growth",
        "plan": "Expand marketplace/distribution footprints and partner-aligned onboarding assets.",
        "experiment": "Launch one distribution channel experiment and measure qualified signup rate."
    },
    "general": {
        "opportunity": "Emerging market signal",
        "plan": "Capture this signal in backlog and validate impact against current roadmap themes.",
        "experiment": "Interview 3 target users to validate urgency and willingness to pay."
    },
}


@dataclass
class FeedItem:
    title: str
    link: str
    published_at: datetime | None
    source_feed: str


@dataclass
class Opportunity:
    item: FeedItem
    score: int
    category: str
    matched_terms: list[str]


def now_utc() -> datetime:
    return datetime.now(UTC)


def parse_feed_items(url: str, timeout_seconds: int = 15) -> list[FeedItem]:
    req = Request(url, headers={"User-Agent": "si-market-research-agent/1.0"})
    with urlopen(req, timeout=timeout_seconds) as resp:
        data = resp.read()

    root = ET.fromstring(data)
    items: list[FeedItem] = []

    rss_items = root.findall(".//item")
    if rss_items:
        for item in rss_items:
            title = (item.findtext("title") or "").strip()
            link = (item.findtext("link") or "").strip()
            pub_raw = (item.findtext("pubDate") or item.findtext("date") or "").strip()
            pub_at = parse_any_date(pub_raw)
            if title and link:
                items.append(
                    FeedItem(
                        title=title,
                        link=link,
                        published_at=pub_at,
                        source_feed=url,
                    )
                )
        return items

    atom_entries = root.findall(".//{http://www.w3.org/2005/Atom}entry")
    for entry in atom_entries:
        title = (entry.findtext("{http://www.w3.org/2005/Atom}title") or "").strip()
        link = ""
        for link_node in entry.findall("{http://www.w3.org/2005/Atom}link"):
            href = (link_node.attrib.get("href") or "").strip()
            rel = (link_node.attrib.get("rel") or "alternate").strip()
            if href and rel in {"alternate", ""}:
                link = href
                break
            if href and not link:
                link = href
        pub_raw = (
            entry.findtext("{http://www.w3.org/2005/Atom}updated")
            or entry.findtext("{http://www.w3.org/2005/Atom}published")
            or ""
        ).strip()
        pub_at = parse_any_date(pub_raw)
        if title and link:
            items.append(
                FeedItem(
                    title=title,
                    link=link,
                    published_at=pub_at,
                    source_feed=url,
                )
            )

    return items


def parse_any_date(raw: str) -> datetime | None:
    if not raw:
        return None
    try:
        dt = parsedate_to_datetime(raw)
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=UTC)
        return dt.astimezone(UTC)
    except Exception:
        pass

    for fmt in (
        "%Y-%m-%dT%H:%M:%S%z",
        "%Y-%m-%dT%H:%M:%SZ",
        "%Y-%m-%d",
    ):
        try:
            dt = datetime.strptime(raw, fmt)
            if dt.tzinfo is None:
                dt = dt.replace(tzinfo=UTC)
            return dt.astimezone(UTC)
        except Exception:
            continue
    return None


def score_item(item: FeedItem) -> Opportunity | None:
    text = f"{item.title} {item.link}".lower()
    total = 0
    category_scores: dict[str, int] = {}
    matched_terms: list[str] = []

    for category, (terms, weight) in KEYWORD_WEIGHTS.items():
        cat_score = 0
        for term in terms:
            if term in text:
                cat_score += weight
                matched_terms.append(term)
        if cat_score > 0:
            category_scores[category] = cat_score
            total += cat_score

    if item.published_at:
        age = now_utc() - item.published_at
        if age <= timedelta(days=7):
            total += 3
        elif age <= timedelta(days=30):
            total += 1

    if total < 4:
        return None

    category = max(category_scores.items(), key=lambda x: x[1])[0] if category_scores else "general"
    return Opportunity(item=item, score=total, category=category, matched_terms=sorted(set(matched_terms)))


def ensure_parent(path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)


def load_json_or_default(path: Path, default: dict[str, Any]) -> dict[str, Any]:
    if not path.exists():
        return default
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except Exception:
        return default


def stable_id(link: str) -> str:
    digest = hashlib.sha1(link.encode("utf-8")).hexdigest()[:10]
    return f"mr-{digest}"


def slugify(value: str) -> str:
    slug = re.sub(r"[^a-z0-9]+", "-", value.lower()).strip("-")
    return slug[:72] or "market-opportunity"


def build_action_plan(op: Opportunity) -> dict[str, str]:
    template = PLAYBOOK_BY_CATEGORY.get(op.category, PLAYBOOK_BY_CATEGORY["general"])
    return {
        "opportunity": template["opportunity"],
        "plan": template["plan"],
        "experiment": template["experiment"],
    }


def write_if_changed(path: Path, content: str) -> bool:
    existing = path.read_text(encoding="utf-8") if path.exists() else None
    if existing == content:
        return False
    ensure_parent(path)
    path.write_text(content, encoding="utf-8")
    return True


def render_board_markdown(board: dict[str, Any]) -> str:
    lines: list[str] = []
    lines.append("# Shared Market Taskboard")
    lines.append("")
    lines.append(f"Updated: {board['updated_at']}")
    lines.append("")

    columns = board.get("columns", [])
    tasks = board.get("tasks", [])

    for col in columns:
        cid = col["id"]
        cname = col["name"]
        lines.append(f"## {cname}")
        col_tasks = [t for t in tasks if t.get("status") == cid]
        if not col_tasks:
            lines.append("- _(none)_")
            lines.append("")
            continue

        col_tasks.sort(key=lambda t: (t.get("priority", "P3"), t.get("created_at", "")))
        for task in col_tasks:
            title = task.get("title", "Untitled")
            prio = task.get("priority", "P3")
            ticket = task.get("ticket_path")
            owner = task.get("owner", "market-agent")
            plan = task.get("action_plan", {})
            plan_text = plan.get("plan", "")
            lines.append(f"- **[{prio}] {title}**")
            lines.append(f"  - Owner: `{owner}`")
            lines.append(f"  - Workstream: `{task.get('workstream', 'paas')}`")
            if ticket:
                lines.append(f"  - Ticket: `{ticket}`")
            if plan_text:
                lines.append(f"  - Plan: {plan_text}")
        lines.append("")

    return "\n".join(lines).rstrip() + "\n"


def create_ticket_content(op: Opportunity, task: dict[str, Any]) -> str:
    published = op.item.published_at.isoformat() if op.item.published_at else "unknown"
    plan = task["action_plan"]
    lines = [
        f"# Market Opportunity: {op.item.title}",
        "",
        f"- Source: {op.item.link}",
        f"- Feed: {op.item.source_feed}",
        f"- Published: {published}",
        f"- Score: {op.score}",
        f"- Category: `{op.category}`",
        f"- Workstream: `{task['workstream']}`",
        "",
        "## Why this matters",
        f"{plan['opportunity']}.",
        "",
        "## Action plan",
        f"{plan['plan']}",
        "",
        "## 7-day experiment",
        f"{plan['experiment']}",
        "",
        "## Task checklist",
        "- [ ] Validate urgency with at least 3 target users",
        "- [ ] Define success metric and baseline",
        "- [ ] Prototype scope and estimate implementation effort",
        "- [ ] Decide go/no-go for PaaS backlog",
        "",
    ]
    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser(description="Market research scout for SI")
    parser.add_argument("--repo-root", required=True)
    parser.add_argument("--board-json", required=True)
    parser.add_argument("--board-md", required=True)
    parser.add_argument("--report-dir", required=True)
    parser.add_argument("--tickets-dir", required=True)
    parser.add_argument("--summary-json", required=True)
    parser.add_argument("--max-opportunities", type=int, default=6)
    parser.add_argument("--max-new-tickets", type=int, default=3)
    parser.add_argument("--feeds", default="")
    args = parser.parse_args()

    root = Path(args.repo_root)
    board_json_path = root / args.board_json
    board_md_path = root / args.board_md
    report_dir = root / args.report_dir
    tickets_dir = root / args.tickets_dir
    summary_json_path = root / args.summary_json

    feeds = [f.strip() for f in args.feeds.split(",") if f.strip()] if args.feeds else DEFAULT_FEEDS

    fetched_items: list[FeedItem] = []
    feed_errors: list[str] = []

    for feed_url in feeds:
        try:
            items = parse_feed_items(feed_url)
            fetched_items.extend(items)
        except Exception as exc:
            feed_errors.append(f"{feed_url}: {exc}")

    seen_links: set[str] = set()
    deduped: list[FeedItem] = []
    for item in fetched_items:
        if item.link in seen_links:
            continue
        seen_links.add(item.link)
        deduped.append(item)

    opportunities: list[Opportunity] = []
    for item in deduped:
        scored = score_item(item)
        if scored:
            opportunities.append(scored)

    opportunities.sort(
        key=lambda op: (
            -op.score,
            -(int(op.item.published_at.timestamp()) if op.item.published_at else 0),
        )
    )
    top = opportunities[: max(0, args.max_opportunities)]

    default_board = {
        "version": 1,
        "updated_at": now_utc().isoformat(),
        "columns": [
            {"id": "market-intel", "name": "Market Intel"},
            {"id": "paas-backlog", "name": "PaaS Backlog"},
            {"id": "paas-build", "name": "PaaS Build"},
            {"id": "validate", "name": "Validate"},
            {"id": "done", "name": "Done"},
        ],
        "tasks": [],
        "history": [],
    }
    board = load_json_or_default(board_json_path, default_board)
    existing_tasks = board.get("tasks", [])

    existing_by_source = {
        task.get("source", {}).get("link", ""): task for task in existing_tasks if task.get("source")
    }

    created_tasks = 0
    created_ticket_paths: list[str] = []

    for op in top:
        key = op.item.link
        action_plan = build_action_plan(op)
        if key in existing_by_source:
            task = existing_by_source[key]
            task["updated_at"] = now_utc().isoformat()
            task["score"] = op.score
            task["matched_terms"] = op.matched_terms
            task["action_plan"] = action_plan
            continue

        if created_tasks >= max(0, args.max_new_tickets):
            continue

        task_id = stable_id(op.item.link)
        ticket_name = f"{now_utc().strftime('%Y%m%d')}-{slugify(op.item.title)}.md"
        ticket_path = tickets_dir / ticket_name
        rel_ticket_path = str(ticket_path.relative_to(root))

        task = {
            "id": task_id,
            "title": op.item.title,
            "status": "paas-backlog",
            "priority": "P1" if op.score >= 10 else "P2",
            "workstream": "paas",
            "owner": "market-research-agent",
            "score": op.score,
            "matched_terms": op.matched_terms,
            "action_plan": action_plan,
            "source": {
                "link": op.item.link,
                "feed": op.item.source_feed,
                "published_at": op.item.published_at.isoformat() if op.item.published_at else None,
            },
            "ticket_path": rel_ticket_path,
            "created_at": now_utc().isoformat(),
            "updated_at": now_utc().isoformat(),
            "tags": ["market-research", "opportunity", "paas"],
        }
        existing_tasks.append(task)
        existing_by_source[key] = task

        ensure_parent(ticket_path)
        write_if_changed(ticket_path, create_ticket_content(op, task))
        created_ticket_paths.append(rel_ticket_path)
        created_tasks += 1

    board["tasks"] = existing_tasks
    board["updated_at"] = now_utc().isoformat()
    board.setdefault("history", []).append(
        {
            "ran_at": now_utc().isoformat(),
            "signals_scanned": len(deduped),
            "top_opportunities": len(top),
            "new_tasks": created_tasks,
        }
    )
    board["history"] = board["history"][-50:]

    board_json_content = json.dumps(board, indent=2, sort_keys=False) + "\n"
    board_json_changed = write_if_changed(board_json_path, board_json_content)

    board_md_content = render_board_markdown(board)
    board_md_changed = write_if_changed(board_md_path, board_md_content)

    report_lines = [
        f"# Market Opportunities Scan ({now_utc().date().isoformat()})",
        "",
        f"- Signals scanned: {len(deduped)}",
        f"- Scored opportunities: {len(opportunities)}",
        f"- Top opportunities considered: {len(top)}",
        f"- New tasks created: {created_tasks}",
        "",
        "## Top opportunities",
    ]

    if not top:
        report_lines.append("- No high-signal opportunities found in this run.")
    else:
        for op in top:
            plan = build_action_plan(op)
            report_lines.extend(
                [
                    f"### {op.item.title}",
                    f"- Link: {op.item.link}",
                    f"- Score: {op.score}",
                    f"- Category: `{op.category}`",
                    f"- Matched terms: {', '.join(op.matched_terms) if op.matched_terms else 'none'}",
                    f"- Action: {plan['plan']}",
                    f"- 7-day experiment: {plan['experiment']}",
                    "",
                ]
            )

    if feed_errors:
        report_lines.append("## Feed errors")
        for err in feed_errors:
            report_lines.append(f"- {err}")
        report_lines.append("")

    report_dir.mkdir(parents=True, exist_ok=True)
    report_path = report_dir / f"{now_utc().date().isoformat()}-market-opportunities.md"
    report_changed = write_if_changed(report_path, "\n".join(report_lines).rstrip() + "\n")

    summary = {
        "status": "ok",
        "signals_scanned": len(deduped),
        "scored_opportunities": len(opportunities),
        "top_opportunities": len(top),
        "new_tasks": created_tasks,
        "board_json_changed": board_json_changed,
        "board_md_changed": board_md_changed,
        "report_changed": report_changed,
        "report_path": str(report_path.relative_to(root)),
        "created_ticket_paths": created_ticket_paths,
        "feed_errors": feed_errors,
    }

    ensure_parent(summary_json_path)
    summary_json_path.write_text(json.dumps(summary, indent=2) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
