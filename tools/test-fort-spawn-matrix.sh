#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SI_REPO="${SI_REPO:-$ROOT_DIR}"
FORT_REPO="${FORT_REPO:-$SI_REPO/../fort}"
NETWORK_NAME="${SI_E2E_NETWORK:-si}"
PROFILE_A="${SI_E2E_PROFILE_A:-ferma}"
PROFILE_B="${SI_E2E_PROFILE_B:-berylla}"
FORT_IMAGE_TAG="${SI_E2E_FORT_IMAGE_TAG:-fort:e2e-spawn-matrix}"
FORT_HOST_PORT="${SI_E2E_FORT_PORT:-18090}"
KEEP_ARTIFACTS="${E2E_KEEP_ARTIFACTS:-0}"

if [[ ! -d "$FORT_REPO/cmd/fortd" ]]; then
  echo "fort repo not found at $FORT_REPO (set FORT_REPO to override)" >&2
  exit 1
fi
if [[ ! -d "$SI_REPO/tools/si" ]]; then
  echo "si repo not found at $SI_REPO (set SI_REPO to override)" >&2
  exit 1
fi

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "required command not found: $1" >&2
    exit 1
  fi
}

require_cmd docker
require_cmd go
require_cmd stat
require_cmd grep
require_cmd sed
require_cmd awk

TMP_ROOT="$(mktemp -d)"
STATE_DIR="$TMP_ROOT/state"
SAFE_ROOT="$TMP_ROOT/safe-root"
BIN_DIR="$TMP_ROOT/bin"
BACKUP_DIR="$TMP_ROOT/profile-backup"
mkdir -p "$STATE_DIR" "$SAFE_ROOT" "$SAFE_ROOT/safe" "$SAFE_ROOT/core" "$BIN_DIR" "$BACKUP_DIR"

FORT_BIN="$BIN_DIR/fort"
SI_BIN="$BIN_DIR/si"
SEED_FILE="$FORT_REPO/tmp_spawn_matrix_seed.go"
DECRYPT_FILE="$FORT_REPO/tmp_spawn_matrix_manual_decrypt.go"
KEYRING_FILE="$STATE_DIR/si-vault-keyring.json"
SIGNING_KEY_FILE="$STATE_DIR/jwt-signing.key"
STATE_FILE="$STATE_DIR/state.json"
FORTD_CONTAINER="fort-spawn-matrix-$RANDOM"
FORT_HOST_URL="http://127.0.0.1:${FORT_HOST_PORT}"
FORT_CONTAINER_URL="http://${FORTD_CONTAINER}:8088"
PROFILES=("$PROFILE_A" "$PROFILE_B")

log() {
  echo "[fort-spawn-matrix] $*"
}

fail() {
  echo "[fort-spawn-matrix] ERROR: $*" >&2
  exit 1
}

profile_container_name() {
  printf 'si-codex-%s' "$1"
}

restore_profile_fort_state() {
  local profile="$1"
  local profile_dir="$HOME/.si/codex/profiles/$profile"
  local fort_dir="$profile_dir/fort"
  local backup_profile_dir="$BACKUP_DIR/$profile"
  if [[ -d "$backup_profile_dir/fort" ]]; then
    rm -rf "$fort_dir"
    mkdir -p "$profile_dir"
    cp -a "$backup_profile_dir/fort" "$fort_dir"
  elif [[ -e "$backup_profile_dir/.missing" ]]; then
    rm -rf "$fort_dir"
  fi
}

cleanup() {
  rm -f "$SEED_FILE" "$DECRYPT_FILE"
  if [[ "$KEEP_ARTIFACTS" != "1" ]]; then
    docker rm -f "$FORTD_CONTAINER" >/dev/null 2>&1 || true
    for profile in "${PROFILES[@]}"; do
      docker rm -f "$(profile_container_name "$profile")" >/dev/null 2>&1 || true
    done
    rm -rf "$TMP_ROOT"
  else
    log "keeping artifacts at $TMP_ROOT"
  fi
  for profile in "${PROFILES[@]}"; do
    restore_profile_fort_state "$profile"
  done
}
trap cleanup EXIT

backup_profile_fort_state() {
  local profile="$1"
  local fort_dir="$HOME/.si/codex/profiles/$profile/fort"
  local dst="$BACKUP_DIR/$profile"
  mkdir -p "$dst"
  if [[ -d "$fort_dir" ]]; then
    cp -a "$fort_dir" "$dst/fort"
  else
    touch "$dst/.missing"
  fi
}

docker_env_dump() {
  local container="$1"
  docker inspect "$container" --format '{{range .Config.Env}}{{println .}}{{end}}'
}

si_run() {
  local profile="$1"
  shift
  "$SI_BIN" run "$profile" --no-tmux "$@"
}

assert_equals() {
  local got="$1"
  local want="$2"
  local what="$3"
  if [[ "$got" != "$want" ]]; then
    fail "$what: expected '$want', got '$got'"
  fi
}

expect_denied() {
  local what="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    fail "expected deny for $what"
  fi
}

fort_admin() {
  "$FORT_BIN" --host "$FORT_HOST_URL" --token "$ADMIN_TOKEN" "$@"
}

wait_for_fort() {
  local attempt
  for attempt in $(seq 1 80); do
    if "$FORT_BIN" --host "$FORT_HOST_URL" doctor >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.25
  done
  return 1
}

wait_for_si_run() {
  local profile="$1"
  local attempt
  for attempt in $(seq 1 60); do
    if si_run "$profile" true >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.5
  done
  return 1
}

verify_container_env_hygiene() {
  local profile="$1"
  local container
  container="$(profile_container_name "$profile")"
  local env_cfg
  env_cfg="$(docker_env_dump "$container")"
  if echo "$env_cfg" | grep -q '^FORT_TOKEN='; then
    fail "$container leaked FORT_TOKEN in docker env"
  fi
  if echo "$env_cfg" | grep -q '^FORT_REFRESH_TOKEN='; then
    fail "$container leaked FORT_REFRESH_TOKEN in docker env"
  fi
  echo "$env_cfg" | grep -q '^FORT_TOKEN_PATH=' || fail "$container missing FORT_TOKEN_PATH"
  echo "$env_cfg" | grep -q '^FORT_REFRESH_TOKEN_PATH=' || fail "$container missing FORT_REFRESH_TOKEN_PATH"
  echo "$env_cfg" | grep -q "^FORT_HOST=${FORT_CONTAINER_URL}$" || fail "$container missing expected FORT_HOST=${FORT_CONTAINER_URL}"

  local env_runtime
  env_runtime="$(si_run "$profile" bash -lc 'env | sort')"
  if echo "$env_runtime" | grep -q '^FORT_TOKEN='; then
    fail "$container runtime env leaked FORT_TOKEN"
  fi
  if echo "$env_runtime" | grep -q '^FORT_REFRESH_TOKEN='; then
    fail "$container runtime env leaked FORT_REFRESH_TOKEN"
  fi
}

verify_token_file_permissions() {
  local profile="$1"
  local token_mode owner_group refresh_mode refresh_owner_group dir_mode dir_owner_group
  token_mode="$(si_run "$profile" bash -lc 'stat -c "%a" "$FORT_TOKEN_PATH"')"
  owner_group="$(si_run "$profile" bash -lc 'stat -c "%U:%G" "$FORT_TOKEN_PATH"')"
  refresh_mode="$(si_run "$profile" bash -lc 'stat -c "%a" "$FORT_REFRESH_TOKEN_PATH"')"
  refresh_owner_group="$(si_run "$profile" bash -lc 'stat -c "%U:%G" "$FORT_REFRESH_TOKEN_PATH"')"
  dir_mode="$(si_run "$profile" bash -lc 'stat -c "%a" "$(dirname "$FORT_TOKEN_PATH")"')"
  dir_owner_group="$(si_run "$profile" bash -lc 'stat -c "%U:%G" "$(dirname "$FORT_TOKEN_PATH")"')"

  assert_equals "$token_mode" "600" "$profile access token file mode"
  assert_equals "$owner_group" "si:si" "$profile access token owner"
  assert_equals "$refresh_mode" "600" "$profile refresh token file mode"
  assert_equals "$refresh_owner_group" "si:si" "$profile refresh token owner"
  assert_equals "$dir_mode" "700" "$profile fort state dir mode"
  assert_equals "$dir_owner_group" "si:si" "$profile fort state dir owner"

  local host_fort_dir="$HOME/.si/codex/profiles/$profile/fort"
  local host_access="$host_fort_dir/access.token"
  local host_refresh="$host_fort_dir/refresh.token"
  [[ -f "$host_access" ]] || fail "host access token missing at $host_access"
  [[ -f "$host_refresh" ]] || fail "host refresh token missing at $host_refresh"
  assert_equals "$(stat -c "%a" "$host_access")" "600" "$profile host access token mode"
  assert_equals "$(stat -c "%a" "$host_refresh")" "600" "$profile host refresh token mode"
  assert_equals "$(stat -c "%a" "$host_fort_dir")" "700" "$profile host fort dir mode"
}

copy_fort_into_container() {
  local profile="$1"
  local container
  container="$(profile_container_name "$profile")"
  docker cp "$FORT_BIN" "${container}:/tmp/fort"
  docker exec "$container" sh -lc 'chown si:si /tmp/fort && chmod 0755 /tmp/fort'
}

spawn_profile() {
  local profile="$1"
  local container
  container="$(profile_container_name "$profile")"
  docker rm -f "$container" >/dev/null 2>&1 || true
  log "spawning profile ${profile}"
  FORT_HOST="$FORT_HOST_URL" \
  FORT_TOKEN="$ADMIN_TOKEN" \
  SI_FORT_CONTAINER_HOST="$FORT_CONTAINER_URL" \
  "$SI_BIN" spawn "$profile" --profile "$profile" --network "$NETWORK_NAME" --workspace "$SI_REPO" --detach >/dev/null
  wait_for_si_run "$profile" || fail "si run not ready for profile $profile"
  copy_fort_into_container "$profile"
}

respawn_profile() {
  local profile="$1"
  log "respawning profile ${profile}"
  FORT_HOST="$FORT_HOST_URL" \
  FORT_TOKEN="$ADMIN_TOKEN" \
  SI_FORT_CONTAINER_HOST="$FORT_CONTAINER_URL" \
  "$SI_BIN" respawn "$profile" --profile "$profile" --network "$NETWORK_NAME" --workspace "$SI_REPO" --volumes >/dev/null
  wait_for_si_run "$profile" || fail "si run not ready after respawn for profile $profile"
  copy_fort_into_container "$profile"
}

manual_decrypt_value() {
  local repo="$1"
  local env="$2"
  local key="$3"
  local env_file_host="$TMP_ROOT/${repo}.${env}.env"
  docker exec "$FORTD_CONTAINER" cat "/safe/${repo}/.env.${env}" >"$env_file_host"
  (
    cd "$FORT_REPO"
    ENV_FILE="$env_file_host" \
    KEYRING_FILE="$KEYRING_FILE" \
    TARGET_REPO="$repo" \
    TARGET_ENV="$env" \
    TARGET_KEY="$key" \
    go run "$DECRYPT_FILE"
  )
}

for profile in "${PROFILES[@]}"; do
  backup_profile_fort_state "$profile"
done

log "temp root: $TMP_ROOT"
log "building fort + si binaries"
(
  cd "$FORT_REPO"
  go build -o "$FORT_BIN" ./cmd/fort
)
(
  cd "$SI_REPO"
  go build -o "$SI_BIN" ./tools/si
)

log "building fort docker image ${FORT_IMAGE_TAG}"
docker build -t "$FORT_IMAGE_TAG" "$FORT_REPO" >/dev/null

log "seeding temporary fort state and keyring"
cat > "$SEED_FILE" <<'GOEOF'
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"fort/internal/auth"
	"fort/internal/model"
	"fort/internal/store"
	ecies "github.com/ecies/go/v2"
)

func writeEntry(entries map[string]any, repo string, env string) error {
	key, err := ecies.GenerateKey()
	if err != nil {
		return err
	}
	entries[repo+"/"+env] = map[string]any{
		"repo":        repo,
		"env":         env,
		"public_key":  key.PublicKey.Hex(true),
		"private_key": key.Hex(),
	}
	return nil
}

func main() {
	statePath := os.Getenv("STATE_PATH")
	keyringPath := os.Getenv("KEYRING_PATH")
	signingKeyPath := os.Getenv("SIGNING_KEY_PATH")
	signingKey := "spawn-matrix-signing-key-012345678901"

	st, err := store.New(statePath)
	if err != nil {
		panic(err)
	}
	now := time.Now().UTC()
	admin, err := st.CreateAgent("admin", model.AgentTypeUser, model.AgentStatusActive, now)
	if err != nil {
		panic(err)
	}
	if _, err := st.SetPolicy(admin.ID, []model.Binding{{Repo: "*", Env: "*", Ops: []model.Operation{model.OpAny}}}, now); err != nil {
		panic(err)
	}
	mgr := &auth.Manager{SigningKey: []byte(signingKey), Store: st}
	token, _, err := mgr.Issue(admin, 24*time.Hour, "fort-api")
	if err != nil {
		panic(err)
	}

	entries := map[string]any{}
	if err := writeEntry(entries, "safe", "dev"); err != nil {
		panic(err)
	}
	if err := writeEntry(entries, "safe", "prod"); err != nil {
		panic(err)
	}
	if err := writeEntry(entries, "core", "dev"); err != nil {
		panic(err)
	}
	doc := map[string]any{"entries": entries}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		panic(err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(keyringPath, raw, 0o600); err != nil {
		panic(err)
	}
	if err := os.WriteFile(signingKeyPath, []byte(signingKey), 0o600); err != nil {
		panic(err)
	}
	fmt.Print(token)
}
GOEOF

ADMIN_TOKEN="$(
  cd "$FORT_REPO"
  STATE_PATH="$STATE_FILE" KEYRING_PATH="$KEYRING_FILE" SIGNING_KEY_PATH="$SIGNING_KEY_FILE" go run "$SEED_FILE"
)"
[[ -n "$ADMIN_TOKEN" ]] || fail "failed to seed admin token"

log "starting temporary fortd container on ${FORT_HOST_URL}"
docker rm -f "$FORTD_CONTAINER" >/dev/null 2>&1 || true
docker run -d --rm \
  --name "$FORTD_CONTAINER" \
  --network "$NETWORK_NAME" \
  -p "${FORT_HOST_PORT}:8088" \
  -e FORT_ADDR=":8088" \
  -e FORT_STATE_PATH="/var/lib/fort/state.json" \
  -e FORT_JWT_SIGNING_KEY_FILE="/var/lib/fort/jwt-signing.key" \
  -e FORT_SAFE_ROOT="/safe" \
  -e FORT_VAULT_KEYRING_FILE="/var/lib/fort/si-vault-keyring.json" \
  -e FORT_TOKEN_TTL="3m" \
  -e FORT_REFRESH_SESSION_TTL="1h" \
  -v "$STATE_DIR:/var/lib/fort" \
  -v "$SAFE_ROOT:/safe" \
  "$FORT_IMAGE_TAG" >/dev/null

wait_for_fort || fail "fortd did not become ready"

log "creating baseline encrypted entries through fort"
fort_admin set --repo safe --env dev --key MATRIX_SAFE_DEV --value "safe-dev-value" >/dev/null
fort_admin set --repo safe --env prod --key MATRIX_SAFE_PROD --value "safe-prod-value" >/dev/null
fort_admin set --repo core --env dev --key MATRIX_CORE_DEV --value "core-dev-value" >/dev/null

for profile in "${PROFILES[@]}"; do
  spawn_profile "$profile"
  verify_container_env_hygiene "$profile"
  verify_token_file_permissions "$profile"
done

log "validating default read-only policy behavior"
assert_equals "$(si_run "$PROFILE_A" bash -lc 'si fort --bin /tmp/fort get --repo safe --env dev --key MATRIX_SAFE_DEV --format raw')" "safe-dev-value" "${PROFILE_A} safe/dev read"
expect_denied "${PROFILE_A} default write" si_run "$PROFILE_A" bash -lc 'si fort --bin /tmp/fort set --repo safe --env dev --key MATRIX_WRITE_DENY --value denied'

assert_equals "$(si_run "$PROFILE_B" bash -lc 'si fort --bin /tmp/fort get --repo core --env dev --key MATRIX_CORE_DEV --format raw')" "core-dev-value" "${PROFILE_B} core/dev read"
expect_denied "${PROFILE_B} default write" si_run "$PROFILE_B" bash -lc 'si fort --bin /tmp/fort set --repo core --env dev --key MATRIX_WRITE_DENY --value denied'

log "applying per-agent restricted policies"
fort_admin agent policy set --id "si-codex-${PROFILE_A}" --bind 'safe:dev:get,set,list,batch-get,run' >/dev/null
fort_admin agent policy set --id "si-codex-${PROFILE_B}" --bind 'core:dev:get,list,batch-get,run' >/dev/null

assert_equals "$(si_run "$PROFILE_A" bash -lc 'si fort --bin /tmp/fort set --repo safe --env dev --key MATRIX_PROFILE_A_WRITE --value from-profile-a >/dev/null && si fort --bin /tmp/fort get --repo safe --env dev --key MATRIX_PROFILE_A_WRITE --format raw')" "from-profile-a" "${PROFILE_A} safe/dev set+get after policy"
expect_denied "${PROFILE_A} read core/dev after policy" si_run "$PROFILE_A" bash -lc 'si fort --bin /tmp/fort get --repo core --env dev --key MATRIX_CORE_DEV --format raw'

assert_equals "$(si_run "$PROFILE_B" bash -lc 'si fort --bin /tmp/fort get --repo core --env dev --key MATRIX_CORE_DEV --format raw')" "core-dev-value" "${PROFILE_B} core/dev read after policy"
expect_denied "${PROFILE_B} read safe/dev after policy" si_run "$PROFILE_B" bash -lc 'si fort --bin /tmp/fort get --repo safe --env dev --key MATRIX_SAFE_DEV --format raw'
expect_denied "${PROFILE_B} write core/dev after policy" si_run "$PROFILE_B" bash -lc 'si fort --bin /tmp/fort set --repo core --env dev --key MATRIX_PROFILE_B_WRITE --value nope'

log "respawning ${PROFILE_A} twice with volumes and re-validating auth continuity"
respawn_profile "$PROFILE_A"
verify_container_env_hygiene "$PROFILE_A"
verify_token_file_permissions "$PROFILE_A"
assert_equals "$(si_run "$PROFILE_A" bash -lc 'si fort --bin /tmp/fort get --repo safe --env dev --key MATRIX_PROFILE_A_WRITE --format raw')" "from-profile-a" "${PROFILE_A} read after respawn #1"
expect_denied "${PROFILE_A} still blocked core/dev after respawn #1" si_run "$PROFILE_A" bash -lc 'si fort --bin /tmp/fort get --repo core --env dev --key MATRIX_CORE_DEV --format raw'

respawn_profile "$PROFILE_A"
verify_container_env_hygiene "$PROFILE_A"
verify_token_file_permissions "$PROFILE_A"
assert_equals "$(si_run "$PROFILE_A" bash -lc 'si fort --bin /tmp/fort get --repo safe --env dev --key MATRIX_PROFILE_A_WRITE --format raw')" "from-profile-a" "${PROFILE_A} read after respawn #2"
expect_denied "${PROFILE_A} still blocked core/dev after respawn #2" si_run "$PROFILE_A" bash -lc 'si fort --bin /tmp/fort get --repo core --env dev --key MATRIX_CORE_DEV --format raw'

log "verifying ciphertext-at-rest and manual decrypt parity"
SAFE_DEV_FILE="$TMP_ROOT/safe.dev.env"
docker exec "$FORTD_CONTAINER" cat "/safe/safe/.env.dev" >"$SAFE_DEV_FILE"
[[ -s "$SAFE_DEV_FILE" ]] || fail "expected env file not found via fort container: /safe/safe/.env.dev"
if ! grep -q '^MATRIX_PROFILE_A_WRITE=encrypted:si-vault:' "$SAFE_DEV_FILE"; then
  fail "expected encrypted vault payload prefix in $SAFE_DEV_FILE"
fi

cat > "$DECRYPT_FILE" <<'GOEOF'
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	ecies "github.com/ecies/go/v2"
)

func main() {
	envFile := strings.TrimSpace(os.Getenv("ENV_FILE"))
	keyringFile := strings.TrimSpace(os.Getenv("KEYRING_FILE"))
	targetRepo := strings.TrimSpace(os.Getenv("TARGET_REPO"))
	targetEnv := strings.TrimSpace(os.Getenv("TARGET_ENV"))
	targetKey := strings.TrimSpace(os.Getenv("TARGET_KEY"))
	if envFile == "" || keyringFile == "" || targetRepo == "" || targetEnv == "" || targetKey == "" {
		panic("missing required env vars")
	}
	raw, err := os.ReadFile(envFile)
	if err != nil {
		panic(err)
	}
	cipher := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, targetKey+"=") {
			cipher = strings.TrimPrefix(line, targetKey+"=")
			break
		}
	}
	if !strings.HasPrefix(cipher, "encrypted:si-vault:") {
		panic("ciphertext missing encrypted:si-vault prefix")
	}
	payload := strings.TrimPrefix(cipher, "encrypted:si-vault:")
	blob, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		panic(err)
	}
	keyringRaw, err := os.ReadFile(keyringFile)
	if err != nil {
		panic(err)
	}
	var doc struct {
		Entries map[string]struct {
			PrivateKey string `json:"private_key"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(keyringRaw, &doc); err != nil {
		panic(err)
	}
	entry, ok := doc.Entries[targetRepo+"/"+targetEnv]
	if !ok {
		panic("missing keyring entry")
	}
	priv, err := ecies.NewPrivateKeyFromHex(strings.TrimSpace(entry.PrivateKey))
	if err != nil {
		panic(err)
	}
	plain, err := ecies.Decrypt(priv, blob)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(plain))
}
GOEOF

manual_plain="$(manual_decrypt_value safe dev MATRIX_PROFILE_A_WRITE)"
fort_plain="$(si_run "$PROFILE_A" bash -lc 'si fort --bin /tmp/fort get --repo safe --env dev --key MATRIX_PROFILE_A_WRITE --format raw')"
assert_equals "$manual_plain" "$fort_plain" "manual decrypt parity with fort get"

log "all spawn/respawn fort matrix checks passed"
