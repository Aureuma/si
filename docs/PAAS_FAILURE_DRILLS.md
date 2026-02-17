# PaaS Failure-Injection and Rollback Drills

Last updated: 2026-02-17  
Scope: `si paas` deployment failure paths and rollback behavior

## Drill Set

| Drill ID | Failure Injected | Expected Safety Behavior | Validation Command |
| --- | --- | --- | --- |
| DRILL-01 | Canary target apply failure in fan-out strategy | Remaining targets are skipped, deterministic status and failure contract returned | `docker run --rm -v /home/shawn/Development/si:/work -w /work golang:1.25 go test ./tools/si -run TestApplyPaasReleaseToTargetsCanaryStopsAfterCanaryFailure -count=1` |
| DRILL-02 | Deploy health check failure after apply | Automatic rollback to last known-good release candidate, failure envelope preserves remediation hints | `docker run --rm -v /home/shawn/Development/si:/work -w /work golang:1.25 go test ./tools/si -run TestPaasRegressionUpgradeDeployRollbackPath -count=1` |
| DRILL-03 | Blue/green post-cutover health failure | Cutover rollback command restores previous slot and reports `bluegreen_post_cutover_health` failure stage | `docker run --rm -v /home/shawn/Development/si:/work -w /work golang:1.25 go test ./tools/si -run TestRunPaasBlueGreenDeployOnTargetRollsBackOnPostCutoverHealthFailure -count=1` |

## Drill Runner

Use the committed runner to execute the full drill sequence:

```bash
./tools/paas-failure-drills.sh
```

## Operator Policy

1. Run these drills before release cuts that alter deploy/apply/rollback behavior.
2. Treat any failing drill as release-blocking until fixed or explicitly waived in ticket notes.
3. Record drill execution date and commit SHA in the status board weekly log when run manually.
