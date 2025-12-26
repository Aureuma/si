#!/usr/bin/env bash
set -euo pipefail

swarm_stack_name() {
  echo "${SILEXA_STACK:-silexa}"
}

swarm_network_name() {
  echo "${SILEXA_NETWORK:-silexa_net}"
}

normalize_dyad_service() {
  local name="$1"
  if [[ "$name" == silexa-actor-* ]]; then
    echo "actor-${name#silexa-actor-}"
    return
  fi
  if [[ "$name" == silexa-critic-* ]]; then
    echo "critic-${name#silexa-critic-}"
    return
  fi
  echo "$name"
}

service_candidates() {
  local name
  name="$(normalize_dyad_service "$1")"
  local stack
  stack="$(swarm_stack_name)"
  local with_prefix="${stack}_${name}"

  if [[ "$name" == "${stack}_"* ]]; then
    printf '%s\n' "$name"
    return
  fi

  if [[ "$name" == "$with_prefix" ]]; then
    printf '%s\n' "$name"
    return
  fi

  printf '%s\n' "$name" "$with_prefix"
}

resolve_container_id() {
  local target="$1"
  local id=""

  if [[ -z "$target" ]]; then
    echo "missing target" >&2
    return 1
  fi

  if id=$(docker inspect -f '{{.Id}}' "$target" 2>/dev/null); then
    if [[ -n "$id" ]]; then
      echo "$id"
      return 0
    fi
  fi

  local svc
  while read -r svc; do
    if [[ -z "$svc" ]]; then
      continue
    fi
    id=$(docker ps --filter "label=com.docker.swarm.service.name=${svc}" --format '{{.ID}}' | head -n1)
    if [[ -n "$id" ]]; then
      echo "$id"
      return 0
    fi
  done < <(service_candidates "$target")

  echo "unable to resolve container for: $target" >&2
  return 1
}
