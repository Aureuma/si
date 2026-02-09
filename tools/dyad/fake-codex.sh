#!/usr/bin/env bash
set -euo pipefail

# Minimal interactive REPL that mimics Codex's `›` prompt and emits delimited work reports.
# Useful for offline dyad/tmux/parsing smoke tests.

prompt_char="${FAKE_CODEX_PROMPT_CHAR:-›}"
delay="${FAKE_CODEX_DELAY_SECONDS:-0}"
long_lines="${FAKE_CODEX_LONG_LINES:-0}"
long_if_contains="${FAKE_CODEX_LONG_IF_CONTAINS:-}"
no_markers="${FAKE_CODEX_NO_MARKERS:-0}"

echo "fake-codex ready"
printf "%s " "${prompt_char}"
while IFS= read -r line; do
  if [[ "${delay}" != "0" ]]; then
    sleep "${delay}" || true
  fi

  emit_long="0"
  if [[ "${long_lines}" != "0" ]]; then
    emit_long="1"
  elif [[ -n "${long_if_contains}" && "${line}" == *"${long_if_contains}"* ]]; then
    emit_long="1"
    long_lines="12000"
  fi

  if [[ "${emit_long}" == "1" ]]; then
    for i in $(seq 1 "${long_lines}"); do
      echo "line ${i}"
    done
  else
    echo "ok"
  fi

  member="${DYAD_MEMBER:-unknown}"

  if [[ "${no_markers}" == "1" ]]; then
    if [[ "${member}" == "critic" ]]; then
      echo "Assessment:"
      echo "- member: ${member}"
      echo "Risks:"
      echo "- none"
      echo "Required Fixes:"
      echo "- none"
      echo "Verification Steps:"
      echo "- none"
      echo "Next Actor Prompt:"
      echo "- proceed"
      echo "Continue Loop: yes"
    else
      echo "Summary:"
      echo "- member: ${member}"
      echo "Changes:"
      echo "- none"
      echo "Validation:"
      echo "- none"
      echo "Open Questions:"
      echo "- none"
      echo "Next Step for Critic:"
      echo "- proceed"
    fi
  else
    echo "<<WORK_REPORT_BEGIN>>"
    if [[ "${member}" == "critic" ]]; then
      echo "Assessment:"
      echo "- member: ${member}"
      echo "Risks:"
      echo "- none"
      echo "Required Fixes:"
      echo "- none"
      echo "Verification Steps:"
      echo "- none"
      echo "Next Actor Prompt:"
      echo "- proceed"
      echo "Continue Loop: yes"
    else
      echo "Summary:"
      echo "- member: ${member}"
      echo "Changes:"
      echo "- none"
      echo "Validation:"
      echo "- none"
      echo "Open Questions:"
      echo "- none"
      echo "Next Step for Critic:"
      echo "- proceed"
    fi
    echo "<<WORK_REPORT_END>>"
  fi

  printf "%s " "${prompt_char}"
done
