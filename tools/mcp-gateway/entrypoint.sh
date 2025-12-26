#!/bin/sh
set -e

if [ -f /run/secrets/gh_token ]; then
  GH_TOKEN=$(cat /run/secrets/gh_token)
  export GH_TOKEN
fi

if [ -f /run/secrets/stripe_api_key ]; then
  STRIPE_API_KEY=$(cat /run/secrets/stripe_api_key)
  export STRIPE_API_KEY
fi

mkdir -p /catalog
if [ ! -f /catalog/catalog.yaml ] && [ "$1" = "gateway" ]; then
  /usr/local/bin/docker-mcp catalog bootstrap /catalog/catalog.yaml || true
fi

exec /usr/local/bin/docker-mcp "$@"
