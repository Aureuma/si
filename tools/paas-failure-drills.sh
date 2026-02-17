#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

run_drill() {
  local name="$1"
  local pattern="$2"
  echo "[drill] $name"
  docker run --rm -v "$ROOT_DIR:/work" -w /work golang:1.25 \
    go test ./tools/si -run "$pattern" -count=1
}

run_drill "DRILL-01 canary fanout failure gating" "TestApplyPaasReleaseToTargetsCanaryStopsAfterCanaryFailure"
run_drill "DRILL-02 deploy health rollback regression path" "TestPaasRegressionUpgradeDeployRollbackPath"
run_drill "DRILL-03 bluegreen post-cutover rollback" "TestRunPaasBlueGreenDeployOnTargetRollsBackOnPostCutoverHealthFailure"

echo "[drill] all failure-injection rollback drills passed"
