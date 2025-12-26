#!/bin/sh
set -e

if [ -f /run/secrets/gh_token ]; then
  GH_TOKEN=$(cat /run/secrets/gh_token | tr -d '\r\n')
  if [ -n "$GH_TOKEN" ] && [ "$GH_TOKEN" != "unset" ] && [ "$GH_TOKEN" != "UNSET" ]; then
    export GH_TOKEN
  fi
fi

if [ -f /run/secrets/stripe_api_key ]; then
  STRIPE_API_KEY=$(cat /run/secrets/stripe_api_key | tr -d '\r\n')
  if [ -n "$STRIPE_API_KEY" ] && [ "$STRIPE_API_KEY" != "unset" ] && [ "$STRIPE_API_KEY" != "UNSET" ]; then
    export STRIPE_API_KEY
  fi
fi

mkdir -p /catalog
if [ ! -f /catalog/catalog.yaml ] && [ "$1" = "gateway" ]; then
  /usr/local/bin/docker-mcp catalog bootstrap /catalog/catalog.yaml || true
fi

exec /usr/local/bin/docker-mcp "$@"
