//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	ecies "github.com/ecies/go/v2"
)

const fortSeedProgram = `package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"fort/internal/auth"
	"fort/internal/model"
	"fort/internal/store"
	ecies "github.com/ecies/go/v2"
)

func writeEntry(entries map[string]any, repo string, env string, publicKey string, privateKey string) error {
	if publicKey == "" || privateKey == "" {
		return fmt.Errorf("public/private key are required")
	}
	entries[repo+"/"+env] = map[string]any{
		"repo":        repo,
		"env":         env,
		"public_key":  publicKey,
		"private_key": privateKey,
	}
	return nil
}

func generateCanonicalKeypair() (string, string, error) {
	key, err := ecies.GenerateKey()
	if err != nil {
		return "", "", err
	}
	return key.PublicKey.Hex(true), key.Hex(), nil
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
	profileIDs := []string{}
	if raw := strings.TrimSpace(os.Getenv("PROFILE_IDS")); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			id := strings.TrimSpace(part)
			if id != "" {
				profileIDs = append(profileIDs, id)
			}
		}
	}
	for _, profileID := range profileIDs {
		agentID := "si-codex-" + profileID
		workload, err := st.CreateAgent(agentID, model.AgentTypeWorkload, model.AgentStatusActive, now)
		if err != nil {
			panic(err)
		}
		if _, err := st.SetPolicy(workload.ID, []model.Binding{{Repo: "*", Env: "*", Ops: []model.Operation{model.OpAny}}}, now); err != nil {
			panic(err)
		}
	}

	entries := map[string]any{}
	publicKey, privateKey, err := generateCanonicalKeypair()
	if err != nil {
		panic(err)
	}
	if err := writeEntry(entries, "safe", "dev", publicKey, privateKey); err != nil {
		panic(err)
	}
	if err := writeEntry(entries, "safe", "prod", publicKey, privateKey); err != nil {
		panic(err)
	}
	if err := writeEntry(entries, "core", "dev", publicKey, privateKey); err != nil {
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
`

func TestFortSpawnMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("skip integration test")
	}

	requireCommand(t, "docker")
	requireCommand(t, "go")

	ctx := setupFortMatrixContext(t)
	defer ctx.cleanup()

	ctx.seedFortState()
	ctx.startFortServer()
	ctx.seedBaseVaultValues()
	for _, profile := range ctx.profiles {
		ctx.spawnProfile(profile)
		ctx.copyFortIntoContainer(profile)
		ctx.waitForProfile(profile)
		ctx.verifyContainerEnvHygiene(profile)
		ctx.verifyTokenFiles(profile)
	}

	expectingAdminPolicy(ctx, t)
	ctx.respawnContinuity(ctx.profiles[0])

	safeDevFile := ctx.readSafeFile("safe", "dev", "MATRIX_PROFILE_A_WRITE")
	manual, err := decryptFortCiphertext(safeDevFile, ctx.keyringFile, "safe", "dev", "MATRIX_PROFILE_A_WRITE")
	if err != nil {
		t.Fatalf("manual decrypt: %v", err)
	}
	fortValue, err := ctx.fortGet(ctx.profiles[0], "safe", "dev", "MATRIX_PROFILE_A_WRITE")
	if err != nil {
		t.Fatalf("fort get profile value: %v", err)
	}
	if manual != strings.TrimSpace(fortValue) {
		t.Fatalf("manual decrypt mismatch: got %q want %q", fortValue, manual)
	}
}

func setupFortMatrixContext(t *testing.T) *fortMatrixContext {
	siRepo := strings.TrimSpace(os.Getenv("SI_REPO"))
	if siRepo == "" {
		_, callerPath, _, ok := runtime.Caller(0)
		if !ok {
			t.Fatalf("runtime.Caller failed to resolve test file location")
		}
		siRepo = filepath.Clean(filepath.Join(filepath.Dir(callerPath), "..", ".."))
	}
	siRepo = filepath.Clean(siRepo)
	if !isExistingDir(siRepo) {
		root, err := repoRootFrom(".")
		if err != nil {
			t.Fatalf("resolve SI repo: %v", err)
		}
		siRepo = root
	}
	fortRepo := strings.TrimSpace(os.Getenv("FORT_REPO"))
	if fortRepo == "" {
		fortRepo = filepath.Clean(filepath.Join(siRepo, "..", "fort"))
	}
	fortRepo = filepath.Clean(fortRepo)

	if !isExistingDir(siRepo) {
		t.Fatalf("si repo not found: %s", siRepo)
	}
	if !isExistingDir(fortRepo) {
		t.Fatalf("fort repo not found: %s", fortRepo)
	}
	if !isExistingDir(filepath.Join(fortRepo, "cmd", "fort")) {
		t.Fatalf("fort cmd directory missing: %s", filepath.Join(fortRepo, "cmd", "fort"))
	}

	networkName := strings.TrimSpace(os.Getenv("SI_E2E_NETWORK"))
	if networkName == "" {
		networkName = "si-fort-spawn-matrix-" + randomSuffix()
	}

	port := 18090
	if raw := strings.TrimSpace(os.Getenv("SI_E2E_FORT_PORT")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 65535 {
			t.Fatalf("invalid SI_E2E_FORT_PORT: %q", raw)
		}
		port = parsed
	}
	if !isPortAvailable(port) {
		t.Fatalf("SI_E2E_FORT_PORT %d is not available", port)
	}

	profileA := strings.TrimSpace(os.Getenv("SI_E2E_PROFILE_A"))
	if profileA == "" {
		profileA = "matrix-a-" + randomSuffix()
	}
	profileB := strings.TrimSpace(os.Getenv("SI_E2E_PROFILE_B"))
	if profileB == "" {
		profileB = "matrix-b-" + randomSuffix()
	}
	fortImageTag := strings.TrimSpace(os.Getenv("SI_E2E_FORT_IMAGE_TAG"))
	if fortImageTag == "" {
		fortImageTag = "fort:e2e-spawn-matrix"
	}

	tmpRoot := t.TempDir()
	seedDir, seedErr := createTempSeedWorkspace(t, fortRepo)
	if seedErr != nil {
		t.Fatalf("prepare seed workspace: %v", seedErr)
	}
	ctx := &fortMatrixContext{
		t:                t,
		siRepo:           siRepo,
		fortRepo:         fortRepo,
		profiles:         []string{profileA, profileB},
		network:          networkName,
		fortHostURL:      fmt.Sprintf("http://127.0.0.1:%d", port),
		fortContainerURL: "",
		fortContainer:    "fort-spawn-matrix-" + randomSuffix(),
		tmpRoot:          tmpRoot,
		stateDir:         filepath.Join(tmpRoot, "state"),
		safeRoot:         filepath.Join(tmpRoot, "safe-root"),
		keyringFile:      filepath.Join(tmpRoot, "state", "si-vault-keyring.json"),
		stateFile:        filepath.Join(tmpRoot, "state", "state.json"),
		jwtSigningKey:    filepath.Join(tmpRoot, "state", "jwt-signing.key"),
		seedFile:         filepath.Join(seedDir, "seed.go"),
		seedWorkspace:    seedDir,
		binDir:           filepath.Join(tmpRoot, "bin"),
		profileTestHome:  mustTempDir(t, "si-fort-matrix-home"),
		fortImageTag:     fortImageTag,
		uid:              os.Getuid(),
		gid:              os.Getgid(),
		hostUID:          strconv.Itoa(os.Getuid()),
		hostGID:          strconv.Itoa(os.Getgid()),
		fortPort:         port,
		fortHost:         "",
	}
	ctx.siBinary = siTestBinaryPath(t)
	ctx.fortBinary = filepath.Join(ctx.binDir, "fort")
	ctx.fortContainerURL = "http://" + ctx.fortContainer + ":8088"
	ctx.fortHostURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	ctx.fortHost = ctx.fortHostURL
	t.Setenv("SI_SETTINGS_HOME", ctx.profileTestHome)
	t.Setenv("HOME", ctx.profileTestHome)
	if err := seedTestCodexProfiles(t, ctx.profiles); err != nil {
		t.Fatalf("seed test codex profiles: %v", err)
	}

	if err := os.WriteFile(ctx.seedFile, []byte(fortSeedProgram), 0o600); err != nil {
		t.Fatalf("write seed helper: %v", err)
	}
	for _, p := range []string{ctx.stateDir, ctx.safeRoot, filepath.Join(ctx.safeRoot, "safe"), filepath.Join(ctx.safeRoot, "core"), ctx.binDir} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatalf("create dir %s: %v", p, err)
		}
	}

	if err := buildFortBinary(ctx.fortRepo, ctx.fortBinary); err != nil {
		t.Fatalf("build fort binary: %v", err)
	}
	if err := runCommand(t, ctx.siRepo, nil, "docker", "build", "-t", ctx.fortImageTag, ctx.fortRepo); err != nil {
		t.Fatalf("build fort image %s: %v", ctx.fortImageTag, err)
	}
	ctx.imageBuilt = true

	if err := runCommand(t, "", nil, "go", "version"); err != nil {
		t.Fatalf("go toolchain check failed")
	}

	if err := ensureDockerNetwork(ctx.network); err != nil {
		t.Fatalf("prepare docker network %s: %v", ctx.network, err)
	}
	ctx.networkCreated = true

	ctx.cleanupFns = append(ctx.cleanupFns, func() {
		runCommandIgnore("docker", "rm", "-f", profileContainerName(ctx.profiles[0]))
		runCommandIgnore("docker", "rm", "-f", profileContainerName(ctx.profiles[1]))
		runCommandIgnore("docker", "rm", "-f", ctx.fortContainer)
		if ctx.seedWorkspace != "" {
			_ = os.RemoveAll(ctx.seedWorkspace)
		}
		if ctx.networkCreated {
			runCommandIgnore("docker", "network", "rm", ctx.network)
		}
	})

	return ctx
}

func (ctx *fortMatrixContext) cleanup() {
	for _, fn := range ctx.cleanupFns {
		fn()
	}
}

func createTempSeedWorkspace(t *testing.T, fortRepo string) (string, error) {
	t.Helper()
	seedDir := filepath.Join(fortRepo, ".e2e-seed-"+randomSuffix())
	if err := os.MkdirAll(seedDir, 0o700); err != nil {
		return "", err
	}
	return seedDir, nil
}

func seedTestCodexProfiles(t *testing.T, profileIDs []string) error {
	t.Helper()
	if len(profileIDs) == 0 {
		return fmt.Errorf("at least one profile id is required")
	}
	baseSettings := defaultSettings()
	baseSettings.Codex.Profiles.Entries = map[string]CodexProfileEntry{}
	for _, id := range profileIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return fmt.Errorf("profile id must not be empty")
		}
		name := id
		if len(id) > 1 {
			name = strings.ToUpper(id[:1]) + id[1:]
		} else {
			name = strings.ToUpper(id)
		}
		baseSettings.Codex.Profiles.Entries[id] = CodexProfileEntry{
			Name:  name,
			Email: id + "@example.com",
		}
	}
	baseSettings.Codex.Profiles.Active = strings.TrimSpace(profileIDs[0])
	return saveSettings(baseSettings)
}

func (ctx *fortMatrixContext) commonEnvMap(extra map[string]string) map[string]string {
	base := map[string]string{
		"HOME":                        ctx.profileTestHome,
		"SI_SETTINGS_HOME":            ctx.profileTestHome,
		"SI_HOST_UID":                 ctx.hostUID,
		"SI_HOST_GID":                 ctx.hostGID,
		"SI_FORT_ALLOW_INSECURE_HOST": "1",
	}
	for k, v := range extra {
		base[k] = v
	}
	return base
}

func (ctx *fortMatrixContext) seedFortState() {
	t := ctx.t
	env := map[string]string{
		"STATE_PATH":       ctx.stateFile,
		"KEYRING_PATH":     ctx.keyringFile,
		"SIGNING_KEY_PATH": ctx.jwtSigningKey,
		"GOWORK":           "off",
		"PROFILE_IDS":      strings.Join(ctx.profiles, ","),
	}
	out, stderr, err := runCommandWithOutput(t, ctx.fortRepo, env, "go", "run", ctx.seedFile)
	if err != nil {
		t.Fatalf("seed fort state: %v (stdout=%q stderr=%q)", err, out, stderr)
	}
	ctx.adminToken = strings.TrimSpace(out)
	if ctx.adminToken == "" {
		t.Fatalf("empty admin token from seed helper")
	}
}

func (ctx *fortMatrixContext) runFortAdmin(args ...string) (string, error) {
	tokenPath := filepath.Join(ctx.profileTestHome, ".si", "fort", "bootstrap", "admin.token")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(tokenPath, []byte(ctx.adminToken), 0o600); err != nil {
		return "", err
	}
	env := map[string]string{}
	cmdArgs := []string{ctx.fortBinary, "--host", ctx.fortHost}
	if ctx.fortSupportsTokenFileFlag() {
		cmdArgs = append(cmdArgs, "--token-file", tokenPath)
	} else {
		cmdArgs = append(cmdArgs, "--token", ctx.adminToken)
	}
	cmdArgs = append(cmdArgs, args...)
	out, stderr, err := runCommandWithOutput(ctx.t, ctx.fortRepo, env, cmdArgs...)
	if err != nil {
		return out, fmt.Errorf("%w (stdout=%q stderr=%q)", err, out, stderr)
	}
	return out, nil
}

func (ctx *fortMatrixContext) startFortServer() {
	t := ctx.t
	if _, _, err := runCommandWithOutput(t, "", nil,
		"docker", "run", "-d", "--rm",
		"--name", ctx.fortContainer,
		"--network", ctx.network,
		"-p", fmt.Sprintf("%d:8088", ctx.fortPort),
		"-e", "FORT_ADDR=:8088",
		"-e", "FORT_STATE_PATH=/var/lib/fort/state.json",
		"-e", "FORT_JWT_SIGNING_KEY_FILE=/var/lib/fort/jwt-signing.key",
		"-e", "FORT_SAFE_ROOT=/safe",
		"-e", "FORT_VAULT_KEYRING_FILE=/var/lib/fort/si-vault-keyring.json",
		"-e", "FORT_TOKEN_TTL=3m",
		"-e", "FORT_REFRESH_SESSION_TTL=1h",
		"-v", ctx.stateDir+":/var/lib/fort",
		"-v", ctx.safeRoot+":/safe",
		ctx.fortImageTag,
	); err != nil {
		t.Fatalf("start fort container: %v", err)
	}
	if !waitUntil(time.Now().Add(180*time.Second), 250*time.Millisecond, func() bool {
		return ctx.fortHTTPReady()
	}) {
		logs, _, _ := runCommandWithOutput(ctx.t, "", nil, "docker", "logs", ctx.fortContainer)
		t.Fatalf("fort did not become ready (logs=%q)", logs)
	}
	if out, err := ctx.fortDoctor(); err != nil {
		logs, _, _ := runCommandWithOutput(ctx.t, "", nil, "docker", "logs", ctx.fortContainer)
		t.Fatalf("fort doctor failed after readiness: %v (doctor=%q logs=%q)", err, out, logs)
	}
}

func (ctx *fortMatrixContext) fortDoctor() (string, error) {
	return ctx.runFortAdmin("doctor")
}

func (ctx *fortMatrixContext) fortHTTPReady() bool {
	base := strings.TrimRight(strings.TrimSpace(ctx.fortHostURL), "/")
	if base == "" {
		return false
	}
	for _, path := range []string{"/v1/health", "/v1/ready"} {
		resp, err := http.Get(base + path)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return false
		}
	}
	return true
}

func (ctx *fortMatrixContext) fortSupportsTokenFileFlag() bool {
	if ctx.fortTokenFileFlagProbed {
		return ctx.fortTokenFileFlagOK
	}
	ctx.fortTokenFileFlagProbed = true
	stdout, stderr, err := runCommandWithOutput(ctx.t, ctx.fortRepo, nil, ctx.fortBinary, "-h")
	combined := strings.TrimSpace(stdout + "\n" + stderr)
	if err != nil && combined == "" {
		ctx.fortTokenFileFlagOK = false
		return false
	}
	ctx.fortTokenFileFlagOK = strings.Contains(combined, "--token-file")
	return ctx.fortTokenFileFlagOK
}

func (ctx *fortMatrixContext) seedBaseVaultValues() {
	for _, cmd := range [][]string{
		{"set", "--repo", "safe", "--env", "dev", "--key", "MATRIX_SAFE_DEV", "--value", "safe-dev-value"},
		{"set", "--repo", "safe", "--env", "prod", "--key", "MATRIX_SAFE_PROD", "--value", "safe-prod-value"},
		{"set", "--repo", "core", "--env", "dev", "--key", "MATRIX_CORE_DEV", "--value", "core-dev-value"},
	} {
		if _, err := ctx.runFortAdmin(cmd...); err != nil {
			ctx.t.Fatalf("fort set failed: %v", err)
		}
	}
}

func (ctx *fortMatrixContext) runProfileCommand(profile, shellCmd string) (string, error) {
	out, stderr, err := runCommandWithOutput(ctx.t, ctx.siRepo, ctx.commonEnvMap(nil), ctx.siBinary, "run", profile, "--no-tmux", "bash", "-lc", shellCmd)
	if err != nil {
		return out, fmt.Errorf("%w (stdout=%q stderr=%q)", err, out, stderr)
	}
	return out, nil
}

func (ctx *fortMatrixContext) spawnProfile(profile string) {
	tokenPath := filepath.Join(ctx.profileTestHome, ".si", "fort", "bootstrap", "admin.token")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		ctx.t.Fatalf("mkdir bootstrap token dir: %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte(ctx.adminToken), 0o600); err != nil {
		ctx.t.Fatalf("write bootstrap token: %v", err)
	}
	env := ctx.commonEnvMap(map[string]string{
		"FORT_HOST":                 ctx.fortHost,
		"FORT_BOOTSTRAP_TOKEN_FILE": tokenPath,
		"SI_FORT_CONTAINER_HOST":    ctx.fortContainerURL,
	})
	out, stderr, err := runCommandWithOutput(ctx.t, ctx.siRepo, env, ctx.siBinary, "spawn", profile, "--profile", profile, "--network", ctx.network, "--workspace", ctx.siRepo, "--detach")
	if err != nil {
		ctx.t.Fatalf("spawn %s: %v\nstdout=%s\nstderr=%s", profile, err, out, stderr)
	}
}

func (ctx *fortMatrixContext) respawnProfile(profile string) {
	tokenPath := filepath.Join(ctx.profileTestHome, ".si", "fort", "bootstrap", "admin.token")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		ctx.t.Fatalf("mkdir bootstrap token dir: %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte(ctx.adminToken), 0o600); err != nil {
		ctx.t.Fatalf("write bootstrap token: %v", err)
	}
	env := ctx.commonEnvMap(map[string]string{
		"FORT_HOST":                 ctx.fortHost,
		"FORT_BOOTSTRAP_TOKEN_FILE": tokenPath,
		"SI_FORT_CONTAINER_HOST":    ctx.fortContainerURL,
	})
	out, stderr, err := runCommandWithOutput(ctx.t, ctx.siRepo, env, ctx.siBinary, "respawn", profile, "--profile", profile, "--network", ctx.network, "--workspace", ctx.siRepo, "--volumes")
	if err != nil {
		ctx.t.Fatalf("respawn %s: %v\nstdout=%s\nstderr=%s", profile, err, out, stderr)
	}
}

func (ctx *fortMatrixContext) waitForProfile(profile string) {
	ok := waitUntil(time.Now().Add(60*time.Second), 500*time.Millisecond, func() bool {
		_, err := ctx.runProfileCommand(profile, "true")
		return err == nil
	})
	if !ok {
		ctx.t.Fatalf("profile %s did not become ready", profile)
	}
}

func (ctx *fortMatrixContext) copyFortIntoContainer(profile string) {
	container := profileContainerName(profile)
	if err := runCommandIgnore("docker", "cp", ctx.fortBinary, container+":/tmp/fort"); err != nil {
		ctx.t.Fatalf("docker cp to %s: %v", container, err)
	}
	if err := runCommandIgnore("docker", "cp", ctx.siBinary, container+":/tmp/si"); err != nil {
		ctx.t.Fatalf("docker cp si binary to %s: %v", container, err)
	}
	if _, _, err := runCommandWithOutput(ctx.t, "", nil, "docker", "exec", container, "sh", "-lc", "chown si:si /tmp/fort /tmp/si && chmod 0755 /tmp/fort /tmp/si"); err != nil {
		ctx.t.Fatalf("container %s fort/si binary chmod/chown: %v", container, err)
	}
}

func (ctx *fortMatrixContext) containerEnv(profile string) map[string]string {
	container := profileContainerName(profile)
	out, _, err := runCommandWithOutput(ctx.t, "", nil, "docker", "inspect", "--format", "{{range .Config.Env}}{{println .}}{{end}}", container)
	if err != nil {
		ctx.t.Fatalf("docker inspect %s: %v", container, err)
	}
	parsed := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if eq := strings.Index(line, "="); eq >= 0 {
			parsed[line[:eq]] = line[eq+1:]
		}
	}
	return parsed
}

func (ctx *fortMatrixContext) verifyContainerEnvHygiene(profile string) {
	env := ctx.containerEnv(profile)
	if env["FORT_TOKEN"] != "" {
		ctx.t.Fatalf("container %s leaked FORT_TOKEN in env", profile)
	}
	if env["FORT_REFRESH_TOKEN"] != "" {
		ctx.t.Fatalf("container %s leaked FORT_REFRESH_TOKEN in env", profile)
	}
	if env["FORT_TOKEN_PATH"] == "" {
		ctx.t.Fatalf("container %s missing FORT_TOKEN_PATH", profile)
	}
	if env["FORT_REFRESH_TOKEN_PATH"] == "" {
		ctx.t.Fatalf("container %s missing FORT_REFRESH_TOKEN_PATH", profile)
	}
	if strings.TrimSpace(env["FORT_HOST"]) != ctx.fortContainerURL {
		ctx.t.Fatalf("container %s missing expected FORT_HOST %s, got %q", profile, ctx.fortContainerURL, env["FORT_HOST"])
	}

	envOut, err := ctx.runProfileCommand(profile, "env | sort")
	if err != nil {
		ctx.t.Fatalf("read runtime env for %s: %v", profile, err)
	}
	if strings.Contains(envOut, "FORT_TOKEN=") {
		ctx.t.Fatalf("runtime env leaked FORT_TOKEN for %s", profile)
	}
	if strings.Contains(envOut, "FORT_REFRESH_TOKEN=") {
		ctx.t.Fatalf("runtime env leaked FORT_REFRESH_TOKEN for %s", profile)
	}
}

func (ctx *fortMatrixContext) assertModeAndOwner(path string, expectedMode string, expectedUID, expectedGID int) {
	info, err := os.Stat(path)
	if err != nil {
		ctx.t.Fatalf("stat %s: %v", path, err)
	}
	if fmt.Sprintf("%03o", info.Mode().Perm()) != expectedMode {
		ctx.t.Fatalf("unexpected mode for %s: got %s expected %s", path, fmt.Sprintf("%03o", info.Mode().Perm()), expectedMode)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		ctx.t.Fatalf("unsupported stat type for %s", path)
	}
	if int(st.Uid) != expectedUID || int(st.Gid) != expectedGID {
		ctx.t.Fatalf("unexpected ownership for %s: got %d:%d expected %d:%d", path, st.Uid, st.Gid, expectedUID, expectedGID)
	}
}

func (ctx *fortMatrixContext) verifyTokenFiles(profile string) {
	expected := fmt.Sprintf("%d:%d", ctx.uid, ctx.gid)
	ownerCheck := fmt.Sprintf("%d:%d", ctx.uid, ctx.gid)

	if got, err := ctx.runProfileCommand(profile, "stat -c \"%a\" \"$FORT_TOKEN_PATH\""); err != nil || got != "600" {
		if err != nil {
			ctx.t.Fatalf("read token file mode for %s: %v", profile, err)
		}
		ctx.t.Fatalf("%s access token mode expected 600, got %q", profile, got)
	}
	if got, err := ctx.runProfileCommand(profile, "stat -c \"%u:%g\" \"$FORT_TOKEN_PATH\""); err != nil || got != ownerCheck {
		if err != nil {
			ctx.t.Fatalf("read token owner for %s: %v", profile, err)
		}
		ctx.t.Fatalf("%s access token owner expected %s, got %q", profile, expected, got)
	}
	if got, err := ctx.runProfileCommand(profile, "stat -c \"%a\" \"$FORT_REFRESH_TOKEN_PATH\""); err != nil || got != "600" {
		if err != nil {
			ctx.t.Fatalf("read refresh token mode for %s: %v", profile, err)
		}
		ctx.t.Fatalf("%s refresh token mode expected 600, got %q", profile, got)
	}
	if got, err := ctx.runProfileCommand(profile, "stat -c \"%u:%g\" \"$FORT_REFRESH_TOKEN_PATH\""); err != nil || got != ownerCheck {
		if err != nil {
			ctx.t.Fatalf("read refresh token owner for %s: %v", profile, err)
		}
		ctx.t.Fatalf("%s refresh token owner expected %s, got %q", profile, expected, got)
	}
	if got, err := ctx.runProfileCommand(profile, "stat -c \"%a\" \"$(dirname \"$FORT_TOKEN_PATH\")\""); err != nil || got != "700" {
		if err != nil {
			ctx.t.Fatalf("read fort dir mode for %s: %v", profile, err)
		}
		ctx.t.Fatalf("%s fort dir mode expected 700, got %q", profile, got)
	}
	if got, err := ctx.runProfileCommand(profile, "stat -c \"%u:%g\" \"$(dirname \"$FORT_TOKEN_PATH\")\""); err != nil || got != ownerCheck {
		if err != nil {
			ctx.t.Fatalf("read fort dir owner for %s: %v", profile, err)
		}
		ctx.t.Fatalf("%s fort dir owner expected %s, got %q", profile, expected, got)
	}

	fortDir := filepath.Join(ctx.profileTestHome, ".si", "codex", "profiles", profile, "fort")
	ctx.assertModeAndOwner(filepath.Join(fortDir, "access.token"), "600", ctx.uid, ctx.gid)
	ctx.assertModeAndOwner(filepath.Join(fortDir, "refresh.token"), "600", ctx.uid, ctx.gid)
	ctx.assertModeAndOwner(fortDir, "700", ctx.uid, ctx.gid)
}

func (ctx *fortMatrixContext) fortGet(profile, repo, env, key string) (string, error) {
	command := fmt.Sprintf("%s get --repo %s --env %s --key %s --format raw", ctx.containerFortCommandPrefix(), repo, env, key)
	raw, err := ctx.runProfileCommand(profile, command)
	if err != nil {
		return "", err
	}
	return lastNonEmptyLine(raw), nil
}

func (ctx *fortMatrixContext) fortSetAndGet(profile, repo, env, key, value string) (string, error) {
	command := fmt.Sprintf("%s set --repo %s --env %s --key %s --value %s && %s get --repo %s --env %s --key %s --format raw", ctx.containerFortCommandPrefix(), repo, env, key, value, ctx.containerFortCommandPrefix(), repo, env, key)
	raw, err := ctx.runProfileCommand(profile, command)
	if err != nil {
		return "", err
	}
	return lastNonEmptyLine(raw), nil
}

func (ctx *fortMatrixContext) containerFortCommandPrefix() string {
	if ctx.fortSupportsTokenFileFlag() {
		return `/tmp/fort --host "$FORT_HOST" --token-file "$FORT_TOKEN_PATH"`
	}
	return `/tmp/fort --host "$FORT_HOST" --token "$(cat "$FORT_TOKEN_PATH")"`
}

func lastNonEmptyLine(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}

func expectingAdminPolicy(ctx *fortMatrixContext, t *testing.T) {
	if got, err := ctx.fortGet(ctx.profiles[0], "safe", "dev", "MATRIX_SAFE_DEV"); err != nil || strings.TrimSpace(got) != "safe-dev-value" {
		if err != nil {
			t.Fatalf("%s safe/dev read: %v", ctx.profiles[0], err)
		}
		t.Fatalf("%s safe/dev read expected safe-dev-value, got %q", ctx.profiles[0], got)
	}
	if got, err := ctx.fortSetAndGet(ctx.profiles[0], "safe", "dev", "MATRIX_PROFILE_A_WRITE", "from-profile-a"); err != nil || strings.TrimSpace(got) != "from-profile-a" {
		if err != nil {
			t.Fatalf("%s set/get safe/dev: %v", ctx.profiles[0], err)
		}
		t.Fatalf("%s set/get safe/dev expected from-profile-a, got %q", ctx.profiles[0], got)
	}
	if got, err := ctx.fortGet(ctx.profiles[0], "core", "dev", "MATRIX_CORE_DEV"); err != nil || strings.TrimSpace(got) != "core-dev-value" {
		if err != nil {
			t.Fatalf("%s core/dev read: %v", ctx.profiles[0], err)
		}
		t.Fatalf("%s core/dev read expected core-dev-value, got %q", ctx.profiles[0], got)
	}
	if got, err := ctx.fortGet(ctx.profiles[1], "core", "dev", "MATRIX_CORE_DEV"); err != nil || strings.TrimSpace(got) != "core-dev-value" {
		if err != nil {
			t.Fatalf("%s core/dev read: %v", ctx.profiles[1], err)
		}
		t.Fatalf("%s core/dev read expected core-dev-value, got %q", ctx.profiles[1], got)
	}
	if got, err := ctx.fortGet(ctx.profiles[1], "safe", "dev", "MATRIX_SAFE_DEV"); err != nil || strings.TrimSpace(got) != "safe-dev-value" {
		if err != nil {
			t.Fatalf("%s safe/dev read: %v", ctx.profiles[1], err)
		}
		t.Fatalf("%s safe/dev read expected safe-dev-value, got %q", ctx.profiles[1], got)
	}
	if got, err := ctx.fortSetAndGet(ctx.profiles[1], "core", "dev", "MATRIX_PROFILE_B_WRITE", "from-profile-b"); err != nil || strings.TrimSpace(got) != "from-profile-b" {
		if err != nil {
			t.Fatalf("%s set/get core/dev: %v", ctx.profiles[1], err)
		}
		t.Fatalf("%s set/get core/dev expected from-profile-b, got %q", ctx.profiles[1], got)
	}
}

func (ctx *fortMatrixContext) respawnContinuity(profile string) {
	for i := 1; i <= 2; i++ {
		ctx.respawnProfile(profile)
		ctx.waitForProfile(profile)
		ctx.copyFortIntoContainer(profile)
		ctx.verifyContainerEnvHygiene(profile)
		ctx.verifyTokenFiles(profile)
		if got, err := ctx.fortGet(profile, "safe", "dev", "MATRIX_PROFILE_A_WRITE"); err != nil || strings.TrimSpace(got) != "from-profile-a" {
			if err != nil {
				ctx.t.Fatalf("%s profile value after respawn #%d: %v", profile, i, err)
			}
			ctx.t.Fatalf("%s expected from-profile-a after respawn #%d, got %q", profile, i, got)
		}
		if got, err := ctx.fortGet(profile, "core", "dev", "MATRIX_CORE_DEV"); err != nil || strings.TrimSpace(got) != "core-dev-value" {
			if err != nil {
				ctx.t.Fatalf("%s core/dev value after respawn #%d: %v", profile, i, err)
			}
			ctx.t.Fatalf("%s expected core-dev-value after respawn #%d, got %q", profile, i, got)
		}
	}
}

func (ctx *fortMatrixContext) readSafeFile(repo, env, key string) string {
	filePath := fmt.Sprintf("/safe/%s/.env.%s", repo, env)
	content, err := runDockerExec(ctx.t, ctx.fortContainer, "cat", filePath)
	if err != nil {
		ctx.t.Fatalf("read %s: %v", filePath, err)
	}
	if strings.TrimSpace(content) == "" {
		ctx.t.Fatalf("empty %s file", filePath)
	}
	if !strings.Contains(content, key+"=encrypted:si-vault:") {
		ctx.t.Fatalf("expected encrypted payload for %s in %s", key, filePath)
	}
	return content
}

func runDockerExec(t *testing.T, container, command, arg string) (string, error) {
	out, _, err := runCommandWithOutput(t, "", nil, "docker", "exec", container, command, arg)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func decryptFortCiphertext(envFileContents, keyringPath, repo, env, key string) (string, error) {
	prefix := "encrypted:si-vault:"
	var ciphertext string
	for _, line := range strings.Split(envFileContents, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, key+"=") {
			continue
		}
		ciphertext = strings.TrimPrefix(line, key+"=")
		break
	}
	if ciphertext == "" {
		return "", fmt.Errorf("missing key %s", key)
	}
	if !strings.HasPrefix(ciphertext, prefix) {
		return "", fmt.Errorf("missing si-vault prefix for %s", key)
	}
	payload := strings.TrimPrefix(ciphertext, prefix)
	cipher, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	raw, err := os.ReadFile(keyringPath)
	if err != nil {
		return "", fmt.Errorf("read keyring: %w", err)
	}
	var parsed struct {
		Entries map[string]struct {
			PrivateKey string `json:"private_key"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("parse keyring: %w", err)
	}
	entry, ok := parsed.Entries[repo+"/"+env]
	if !ok {
		return "", fmt.Errorf("missing keyring entry for %s/%s", repo, env)
	}
	priv, err := ecies.NewPrivateKeyFromHex(strings.TrimSpace(entry.PrivateKey))
	if err != nil {
		return "", fmt.Errorf("private key: %w", err)
	}
	plain, err := ecies.Decrypt(priv, cipher)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plain), nil
}

type fortMatrixContext struct {
	t                       *testing.T
	siRepo                  string
	fortRepo                string
	siBinary                string
	fortBinary              string
	profiles                []string
	network                 string
	fortHost                string
	fortHostURL             string
	fortContainerURL        string
	fortContainer           string
	fortImageTag            string
	fortPort                int
	stateDir                string
	safeRoot                string
	keyringFile             string
	stateFile               string
	jwtSigningKey           string
	seedFile                string
	seedWorkspace           string
	binDir                  string
	tmpRoot                 string
	profileTestHome         string
	adminToken              string
	uid                     int
	gid                     int
	hostUID                 string
	hostGID                 string
	cleanupFns              []func()
	imageBuilt              bool
	networkCreated          bool
	fortTokenFileFlagProbed bool
	fortTokenFileFlagOK     bool
}

func isExistingDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func mustTempDir(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	return dir
}

func ensureDockerNetwork(name string) error {
	_, _, err := runCommandWithOutput(nil, "", nil, "docker", "network", "inspect", name)
	if err == nil {
		return nil
	}
	if _, _, err := runCommandWithOutput(nil, "", nil, "docker", "network", "create", name); err != nil {
		return err
	}
	return nil
}

func waitUntil(deadline time.Time, interval time.Duration, check func() bool) bool {
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(interval)
	}
	return false
}

func runCommandWithOutput(t *testing.T, dir string, env map[string]string, args ...string) (string, string, error) {
	if len(args) == 0 {
		if t != nil {
			t.Fatalf("run command called with no args")
		}
		return "", "", fmt.Errorf("no args")
	}
	if t != nil {
		t.Helper()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append([]string{}, os.Environ()...)
	for k, v := range env {
		if k == "" {
			continue
		}
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil && t == nil {
		return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
	}
	if err != nil && t != nil {
		return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

func runCommand(t *testing.T, dir string, env map[string]string, args ...string) error {
	_, _, err := runCommandWithOutput(t, dir, env, args...)
	return err
}

func runCommandIgnore(args ...string) error {
	_, _, err := runCommandWithOutput(nil, "", nil, args...)
	return err
}

func profileContainerName(profile string) string {
	return fmt.Sprintf("si-codex-%s", profile)
}

func isPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func randomSuffix() string {
	data := make([]byte, 4)
	if _, err := rand.Read(data); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano()%100000)
	}
	return hex.EncodeToString(data)
}

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Fatalf("required command not found: %s", name)
	}
}
