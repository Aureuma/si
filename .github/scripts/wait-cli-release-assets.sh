#!/usr/bin/env bash
set -euo pipefail

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Required command not found: $1" >&2
    exit 1
  fi
}

require_cmd gh
require_cmd jq

tag="${1:-}"
repo="${2:-${GITHUB_REPOSITORY:-}}"
workflow="${3:-cli-release-assets.yml}"
attempts="${WAIT_RELEASE_WORKFLOW_ATTEMPTS:-30}"
delay="${WAIT_RELEASE_WORKFLOW_DELAY_SECONDS:-10}"
lookback_minutes="${WAIT_RELEASE_WORKFLOW_LOOKBACK_MINUTES:-20}"

if [[ -z "${tag}" ]]; then
  echo "tag is required" >&2
  exit 1
fi

if [[ -z "${repo}" ]]; then
  echo "repo is required" >&2
  exit 1
fi

cutoff="$(date -u -d "${lookback_minutes} minutes ago" +"%Y-%m-%dT%H:%M:%SZ")"
run_id=""

for (( attempt=1; attempt<=attempts; attempt++ )); do
  runs_json="$(
    gh api \
      --method GET \
      "repos/${repo}/actions/workflows/${workflow}/runs?event=release&per_page=10"
  )"

  run_id="$(
    printf '%s' "${runs_json}" | jq -r --arg cutoff "${cutoff}" '
      .workflow_runs
      | map(select(.created_at >= $cutoff))
      | first
      | .id // empty
    '
  )"

  if [[ -n "${run_id}" ]]; then
    echo "Watching ${workflow} run ${run_id} for ${repo} (${tag})"
    gh run watch "${run_id}" --repo "${repo}" --exit-status
    exit 0
  fi

  if (( attempt < attempts )); then
    sleep "${delay}"
  fi
done

echo "Timed out waiting for ${workflow} release run for ${repo} (${tag})" >&2
exit 1
