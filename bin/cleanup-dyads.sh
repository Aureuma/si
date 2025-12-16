#!/usr/bin/env bash
set -euo pipefail

# Remove stopped/unused actor/critic containers and prune dangling images.

# Remove stopped actor/critic containers labeled as silexa dyads.
stoplist=$(sudo docker ps -a --filter "label=silexa.dyad" --filter "status=exited" --format '{{.ID}}')
if [[ -n "$stoplist" ]]; then
  echo "$stoplist" | xargs sudo docker rm >/dev/null
  echo "Removed stopped dyad containers"
fi

# Prune dangling images (quietly)
sudo docker image prune -f >/dev/null && echo "Pruned dangling images"
