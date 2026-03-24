#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

FORT_ROOT="$(cd "${ROOT}/.." && pwd)/fort"
SURF_ROOT="$(cd "${ROOT}/.." && pwd)/surf"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'missing required command: %s\n' "$1" >&2
    exit 1
  fi
}

need_dir() {
  if [ ! -d "$1" ]; then
    printf 'missing required repo: %s\n' "$1" >&2
    exit 1
  fi
}

run_step() {
  local name="$1"
  shift
  printf '\n==> %s\n' "$name"
  "$@"
}

run_fort_wrapper_smoke() {
  local tmpdir port output
  tmpdir="$(mktemp -d)"
  port="$(
    python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
  )"
  cat >"${tmpdir}/server.py" <<'PY'
from http.server import BaseHTTPRequestHandler, HTTPServer
import json
import sys

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        status = 200
        body = {}
        if self.path == "/v1/health":
            body = {"status": "ok"}
        elif self.path == "/v1/ready":
            body = {"status": "ready"}
        elif self.path == "/v1/whoami":
            body = {"actor": "matrix"}
        else:
            status = 404
            body = {"error": "not found", "path": self.path}
        data = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, *args):
        pass

HTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
PY
  python3 "${tmpdir}/server.py" "${port}" >/dev/null 2>&1 &
  local server_pid="$!"
  trap 'kill "${server_pid}" >/dev/null 2>&1 || true; rm -rf "${tmpdir}"' RETURN

  output="$(
    FORT_HOST="http://127.0.0.1:${port}" \
      cargo run --quiet --bin si-rs -- fort --home "${tmpdir}/home" -- --json doctor
  )"
  printf '%s\n' "${output}"
  python3 - <<'PY' "${output}"
import json
import sys

payload = json.loads(sys.argv[1])
assert payload["health_status"] == 200, payload
assert payload["ready_status"] == 200, payload
PY

  trap - RETURN
  kill "${server_pid}" >/dev/null 2>&1 || true
  rm -rf "${tmpdir}"
}

run_dyad_smoke() {
  local tmpdir workspace configs home_dir script_dir state docker_bin status stopped final
  tmpdir="$(mktemp -d)"
  workspace="${tmpdir}/workspace"
  configs="${tmpdir}/configs"
  home_dir="${tmpdir}/home"
  script_dir="${tmpdir}/bin"
  state="${tmpdir}/state.txt"
  docker_bin="${script_dir}/docker"
  mkdir -p "${workspace}" "${configs}" "${home_dir}/.si" "${script_dir}"
  cat >"${docker_bin}" <<'SH'
#!/bin/sh
STATE_FILE="__STATE__"
cmd="$1"
shift
case "$cmd" in
  run)
    printf '%s\n' 'running' > "${STATE_FILE}"
    printf '%s\n' 'container-id'
    ;;
  ps)
    state='missing'
    if [ -f "${STATE_FILE}" ]; then state=$(tr -d '\n' < "${STATE_FILE}"); fi
    if [ "${state}" = 'removed' ] || [ "${state}" = 'missing' ]; then exit 0; fi
    printf 'si-actor-alpha\t%s\tactor-id\talpha\tios\tactor\n' "${state}"
    printf 'si-critic-alpha\t%s\tcritic-id\talpha\tios\tcritic\n' "${state}"
    ;;
  logs)
    printf '%s\n' 'critic logs'
    ;;
  start)
    printf '%s\n' 'running' > "${STATE_FILE}"
    printf '%s\n' 'started'
    ;;
  stop)
    printf '%s\n' 'exited' > "${STATE_FILE}"
    printf '%s\n' 'stopped'
    ;;
  rm)
    printf '%s\n' 'removed' > "${STATE_FILE}"
    printf '%s\n' 'removed'
    ;;
  exec)
    printf '%s\n' 'exec-ok'
    ;;
  *)
    printf 'unexpected docker command: %s\n' "$cmd" >&2
    exit 1
    ;;
esac
SH
  python3 - <<'PY' "${docker_bin}" "${state}"
from pathlib import Path
import sys

path = Path(sys.argv[1])
state = sys.argv[2]
path.write_text(path.read_text().replace("__STATE__", state))
PY
  chmod +x "${docker_bin}"

  cargo run --quiet --bin si-rs -- dyad spawn start \
    --name alpha \
    --workspace "${workspace}" \
    --configs "${configs}" \
    --home "${home_dir}" \
    --docker-bin "${docker_bin}" >/dev/null

  status="$(
    cargo run --quiet --bin si-rs -- dyad status alpha --format json --docker-bin "${docker_bin}"
  )"
  printf '%s\n' "${status}"
  python3 - <<'PY' "${status}"
import json
import sys

payload = json.loads(sys.argv[1])
assert payload["found"] is True, payload
assert payload["actor"]["status"] == "running", payload
assert payload["critic"]["status"] == "running", payload
PY

  cargo run --quiet --bin si-rs -- dyad logs alpha --member critic --tail 10 --docker-bin "${docker_bin}" >/dev/null
  cargo run --quiet --bin si-rs -- dyad exec alpha --member critic --tty=true --docker-bin "${docker_bin}" -- bash -lc 'echo hi' >/dev/null
  cargo run --quiet --bin si-rs -- dyad stop alpha --docker-bin "${docker_bin}" >/dev/null

  stopped="$(
    cargo run --quiet --bin si-rs -- dyad status alpha --format json --docker-bin "${docker_bin}"
  )"
  python3 - <<'PY' "${stopped}"
import json
import sys

payload = json.loads(sys.argv[1])
assert payload["actor"]["status"] == "exited", payload
assert payload["critic"]["status"] == "exited", payload
PY

  cargo run --quiet --bin si-rs -- dyad start alpha --docker-bin "${docker_bin}" >/dev/null
  cargo run --quiet --bin si-rs -- dyad remove alpha --docker-bin "${docker_bin}" >/dev/null

  final="$(
    cargo run --quiet --bin si-rs -- dyad status alpha --format json --docker-bin "${docker_bin}"
  )"
  printf '%s\n' "${final}"
  python3 - <<'PY' "${final}"
import json
import sys

payload = json.loads(sys.argv[1])
assert payload["found"] is False, payload
PY

  rm -rf "${tmpdir}"
}

need_cmd cargo
need_cmd docker
need_cmd python3
need_dir "${FORT_ROOT}"
need_dir "${SURF_ROOT}"

run_step "installer smoke-host" ./tools/test-install-si.sh
run_step "si cli integration" cargo test -p si-rs-cli --test cli --quiet
run_step "si vault package" cargo test -p si-rs-vault --quiet
run_step "fort workspace" cargo test --quiet --manifest-path "${FORT_ROOT}/Cargo.toml"
run_step "surf workspace" cargo test --workspace --quiet --manifest-path "${SURF_ROOT}/Cargo.toml"
run_step "si fort live wrapper smoke" run_fort_wrapper_smoke
run_step "si dyad lifecycle smoke" run_dyad_smoke

printf '\n==> rust host matrix: ok\n'
