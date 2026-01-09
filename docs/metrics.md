## Steering metrics

Use these metrics to steer dyads and departments. All can be posted to the manager via `/metrics` (see `silexa metric post`).

- **Throughput**: completed tasks per department per day (e.g., `webdev.tasks_done`, `infra.changes_applied`).
- **Lead time**: median time from task open to close per department (`lead_time_minutes` with unit `minutes`).
- **Blocker pressure**: count of open human tasks + pending access requests (`blockers_open`). Track trend day over day.
- **Reliability**: failed vs successful runs of QA smoke or deployments (`qa_fail_rate`, `deploy_fail_rate` as percentages).
- **Visual quality**: count of visual diffs per run and time-to-approval (`visual_regressions`, `visual_baseline_age_days`).
- **Utilization**: active dyads per department vs planned (`dyads_active`).
- **Cost guardrail**: estimated cloud spend deltas pre-deploy (`deploy_cost_delta`), and number of cost vetoes (`cost_veto_count`).
- **Security**: credentials rotations per week, pending secret requests (`creds_rotated`, `creds_requests_pending`).
- **Autonomy**: fraction of tasks closed without human intervention (`autonomy_ratio`).
- **Feedback quality**: count of actionable feedback items (`feedback_actionable`).
- **Codex usage**: per-dyad remaining quota percent (`codex.remaining_pct`), remaining minutes (`codex.remaining_minutes`), and cooldown flag (`codex.cooldown`).

Recommended aggregation:
- Record raw events (task closed, deploy attempted, QA run) into `/metrics` with timestamp.
- Summaries can be rendered by manager or upstream reporting dyad and surfaced via Telegram status reports.

Example metric payload:

```json
{
  "dyad": "web-builder",
  "department": "webdev",
  "name": "tasks_done",
  "value": 3,
  "unit": "count",
  "recorded_by": "critic-web"
}
```
