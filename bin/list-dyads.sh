#!/usr/bin/env bash
set -euo pipefail

docker ps --filter "label=silexa.dyad" --format '{{.Label "com.docker.swarm.service.name"}}\t{{.Label "silexa.dyad"}}\t{{.Label "silexa.department"}}\t{{.Label "silexa.role"}}\t{{.Image}}' \
  | awk 'BEGIN { printf "%-36s %-12s %-14s %-12s %s\n", "SERVICE","DYAD","DEPT","ROLE","IMAGE" } { printf "%-36s %-12s %-14s %-12s %s\n", $1,$2,$3,$4,$5 }'
