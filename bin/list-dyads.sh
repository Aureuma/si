#!/usr/bin/env bash
set -euo pipefail

docker ps --format '{{.Names}}\t{{.Label "silexa.dyad"}}\t{{.Label "silexa.department"}}\t{{.Label "silexa.role"}}\t{{.Image}}' | awk 'BEGIN { printf "%-28s %-12s %-14s %-10s %s\n", "NAME","DYAD","DEPT","ROLE","IMAGE" } { printf "%-28s %-12s %-14s %-10s %s\n", $1,$2,$3,$4,$5 }'
