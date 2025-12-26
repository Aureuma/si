#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)

"${ROOT_DIR}/bin/list-apps.sh" >/dev/null
"${ROOT_DIR}/bin/app-audit.sh"
