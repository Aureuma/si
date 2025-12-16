#!/bin/sh
set -e

mkdir -p /catalog
if [ ! -f /catalog/catalog.yaml ] && [ "$1" = "gateway" ]; then
  /usr/local/bin/docker-mcp catalog bootstrap /catalog/catalog.yaml || true
fi

exec /usr/local/bin/docker-mcp "$@"
