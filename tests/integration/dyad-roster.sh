#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)

"${ROOT_DIR}/bin/dyad-roster-apply.sh" --dry-run >/dev/null
