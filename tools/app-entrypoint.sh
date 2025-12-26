#!/usr/bin/env sh
set -e

if [ -f /run/secrets/app_env ]; then
  while IFS='=' read -r key value; do
    case "$key" in
      ""|\#*) continue ;;
    esac
    export "$key=$value"
  done < /run/secrets/app_env
fi

exec "$@"
