package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"si/tools/si/internal/vault"
)

type paasTestEnvelope struct {
	OK      bool              `json:"ok"`
	Command string            `json:"command"`
	Context string            `json:"context"`
	Mode    string            `json:"mode"`
	Fields  map[string]string `json:"fields"`
}

type paasTargetListPayload struct {
	OK            bool         `json:"ok"`
	Command       string       `json:"command"`
	Context       string       `json:"context"`
	Mode          string       `json:"mode"`
	CurrentTarget string       `json:"current_target"`
	Count         int          `json:"count"`
	Data          []paasTarget `json:"data"`
}

type paasContextListPayload struct {
	OK      bool                `json:"ok"`
	Command string              `json:"command"`
	Context string              `json:"context"`
	Mode    string              `json:"mode"`
	Current string              `json:"current"`
	Count   int                 `json:"count"`
	Data    []paasContextConfig `json:"data"`
}

type paasDoctorPayload struct {
	OK      bool          `json:"ok"`
	Command string        `json:"command"`
	Context string        `json:"context"`
	Mode    string        `json:"mode"`
	Count   int           `json:"count"`
	Checks  []doctorCheck `json:"checks"`
}

func setupPaasMockSunVault(t *testing.T, vaultFile string, token string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_SETTINGS_HOME", home)
	t.Setenv("SI_SUN_BASE_URL", "")
	t.Setenv("SI_SUN_TOKEN", "")

	server, store := newSunTestServer(t, "acme", token)
	t.Cleanup(server.Close)

	settings := defaultSettings()
	applySettingsDefaults(&settings)
	settings.Sun.BaseURL = server.URL
	settings.Sun.Token = token
	settings.Sun.Account = "acme"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	target, err := vaultResolveTarget(settings, vaultFile, false)
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	store.mu.Lock()
	objectKey := store.key(vaultSunKVKind(target), "APP_ENV")
	store.payloads[objectKey] = []byte("prod\n")
	store.revs[objectKey] = 1
	store.metadata[objectKey] = map[string]any{"deleted": false}
	store.created[objectKey] = "2026-01-01T00:00:00Z"
	store.updated[objectKey] = "2026-01-02T00:00:00Z"
	store.mu.Unlock()
}

func TestPaasNoArgsShowsUsageInNonInteractiveMode(t *testing.T) {
	out := captureStdout(t, func() {
		cmdPaas(nil)
	})
	if !strings.Contains(out, paasUsageText) {
		t.Fatalf("expected usage output, got %q", out)
	}
}

func TestPaasSubcommandNoArgsShowsUsageInNonInteractiveMode(t *testing.T) {
	tests := []struct {
		name   string
		invoke func()
		usage  string
	}{
		{name: "target", invoke: func() { cmdPaasTarget(nil) }, usage: paasTargetUsageText},
		{name: "app", invoke: func() { cmdPaasApp(nil) }, usage: paasAppUsageText},
		{name: "alert", invoke: func() { cmdPaasAlert(nil) }, usage: paasAlertUsageText},
		{name: "ai", invoke: func() { cmdPaasAI(nil) }, usage: paasAIUsageText},
		{name: "context", invoke: func() { cmdPaasContext(nil) }, usage: paasContextUsageText},
		{name: "agent", invoke: func() { cmdPaasAgent(nil) }, usage: paasAgentUsageText},
		{name: "events", invoke: func() { cmdPaasEvents(nil) }, usage: paasEventsUsageText},
		{name: "backup", invoke: func() { cmdPaasBackup(nil) }, usage: paasBackupUsageText},
		{name: "taskboard", invoke: func() { cmdPaasTaskboard(nil) }, usage: paasTaskboardUsageText},
		{name: "cloud", invoke: func() { cmdPaasCloud(nil) }, usage: paasCloudUsageText},
	}
	for _, tc := range tests {
		out := captureStdout(t, tc.invoke)
		if !strings.Contains(out, tc.usage) {
			t.Fatalf("%s expected usage output, got %q", tc.name, out)
		}
	}
}

func TestPaasJSONOutputContractTargetAdd(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	out := captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-1", "--host", "10.0.0.4", "--user", "root", "--json"})
	})
	env := parsePaasEnvelope(t, out)
	if !env.OK {
		t.Fatalf("expected ok=true envelope: %#v", env)
	}
	if env.Command != "target add" {
		t.Fatalf("expected command=target add, got %q", env.Command)
	}
	if env.Mode != "scaffold" {
		t.Fatalf("expected mode=scaffold, got %q", env.Mode)
	}
	if env.Context != defaultPaasContext {
		t.Fatalf("expected default context, got %q", env.Context)
	}
	if env.Fields["name"] != "edge-1" {
		t.Fatalf("expected name field, got %#v", env.Fields)
	}
	if env.Fields["host"] != "10.0.0.4" {
		t.Fatalf("expected host field, got %#v", env.Fields)
	}
	if env.Fields["user"] != "root" {
		t.Fatalf("expected user field, got %#v", env.Fields)
	}
}

func TestPaasContextFlagPropagatesAndResets(t *testing.T) {
	withContext := captureStdout(t, func() {
		cmdPaas([]string{"--context", "internal-dogfood", "app", "list", "--json"})
	})
	withEnv := parsePaasEnvelope(t, withContext)
	if withEnv.Context != "internal-dogfood" {
		t.Fatalf("expected context=internal-dogfood, got %q", withEnv.Context)
	}

	defaultContext := captureStdout(t, func() {
		cmdPaas([]string{"app", "list", "--json"})
	})
	defaultEnv := parsePaasEnvelope(t, defaultContext)
	if defaultEnv.Context != defaultPaasContext {
		t.Fatalf("expected context reset to default, got %q", defaultEnv.Context)
	}
}

func TestPaasContextCreateListUseShowRemove(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	createRaw := captureStdout(t, func() {
		cmdPaas([]string{
			"context", "create",
			"--name", "internal-dogfood",
			"--type", "internal-dogfood",
			"--json",
		})
	})
	createEnv := parsePaasEnvelope(t, createRaw)
	if createEnv.Command != "context create" {
		t.Fatalf("expected context create envelope, got %#v", createEnv)
	}
	contextDir := filepath.Join(stateRoot, "contexts", "internal-dogfood")
	required := []string{
		filepath.Join(contextDir, "events"),
		filepath.Join(contextDir, "cache"),
		filepath.Join(contextDir, "vault"),
		filepath.Join(contextDir, "releases"),
		filepath.Join(contextDir, "addons"),
		filepath.Join(contextDir, "config.json"),
	}
	for _, path := range required {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected context layout path %s: %v", path, err)
		}
	}

	listRaw := captureStdout(t, func() {
		cmdPaas([]string{"context", "list", "--json"})
	})
	var listPayload paasContextListPayload
	if err := json.Unmarshal([]byte(listRaw), &listPayload); err != nil {
		t.Fatalf("decode context list payload: %v output=%q", err, listRaw)
	}
	if listPayload.Command != "context list" || listPayload.Count == 0 || len(listPayload.Data) == 0 {
		t.Fatalf("unexpected context list payload: %#v", listPayload)
	}
	found := false
	for _, row := range listPayload.Data {
		if strings.EqualFold(strings.TrimSpace(row.Name), "internal-dogfood") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected context list to include internal-dogfood, got %#v", listPayload.Data)
	}

	useRaw := captureStdout(t, func() {
		cmdPaas([]string{"context", "use", "--name", "internal-dogfood", "--json"})
	})
	useEnv := parsePaasEnvelope(t, useRaw)
	if useEnv.Command != "context use" || useEnv.Fields["name"] != "internal-dogfood" {
		t.Fatalf("unexpected context use output: %#v", useEnv)
	}

	afterUseRaw := captureStdout(t, func() {
		cmdPaas([]string{"app", "list", "--json"})
	})
	afterUseEnv := parsePaasEnvelope(t, afterUseRaw)
	if afterUseEnv.Context != "internal-dogfood" {
		t.Fatalf("expected persisted context selection, got %#v", afterUseEnv)
	}

	showRaw := captureStdout(t, func() {
		cmdPaas([]string{"context", "show", "--name", "internal-dogfood", "--json"})
	})
	showEnv := parsePaasEnvelope(t, showRaw)
	if showEnv.Command != "context show" || showEnv.Fields["name"] != "internal-dogfood" {
		t.Fatalf("unexpected context show output: %#v", showEnv)
	}
	if strings.TrimSpace(showEnv.Fields["vault_file"]) == "" {
		t.Fatalf("expected vault_file in context show output, got %#v", showEnv.Fields)
	}

	removeRaw := captureStdout(t, func() {
		cmdPaas([]string{"context", "remove", "--name", "internal-dogfood", "--force", "--json"})
	})
	removeEnv := parsePaasEnvelope(t, removeRaw)
	if removeEnv.Command != "context remove" || removeEnv.Fields["name"] != "internal-dogfood" {
		t.Fatalf("unexpected context remove output: %#v", removeEnv)
	}
	if _, err := os.Stat(contextDir); !os.IsNotExist(err) {
		t.Fatalf("expected removed context dir to not exist, stat err=%v", err)
	}
}

func TestPaasContextInitUsesCurrentContextWhenNameOmitted(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	raw := captureStdout(t, func() {
		cmdPaas([]string{"--context", "customer-1", "context", "init", "--json"})
	})
	env := parsePaasEnvelope(t, raw)
	if env.Command != "context init" {
		t.Fatalf("expected context init envelope, got %#v", env)
	}
	if env.Fields["name"] != "customer-1" {
		t.Fatalf("expected context init to use current context customer-1, got %#v", env.Fields)
	}
	configPath := filepath.Join(stateRoot, "contexts", "customer-1", "config.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected customer-1 config path: %v", err)
	}
}

func TestPaasDoctorJSONHealthy(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)
	t.Setenv(paasAllowRepoStateEnvKey, "")

	raw := captureStdout(t, func() {
		cmdPaas([]string{"doctor", "--json"})
	})
	var payload paasDoctorPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode doctor payload: %v output=%q", err, raw)
	}
	if !payload.OK || payload.Command != "doctor" || payload.Mode != "live" {
		t.Fatalf("unexpected doctor payload: %#v", payload)
	}
	if payload.Count == 0 || len(payload.Checks) == 0 {
		t.Fatalf("expected non-empty doctor checks, got %#v", payload)
	}
}

func TestRunPaasDoctorChecksDetectsRepoStateAndSecretExposure(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o700); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	stateRoot := filepath.Join(repoRoot, ".state")
	t.Setenv(paasStateRootEnvKey, stateRoot)
	t.Setenv(paasAllowRepoStateEnvKey, "")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	if _, err := initializePaasContextLayout("internal-dogfood", "internal-dogfood", "", ""); err != nil {
		t.Fatalf("initialize context layout: %v", err)
	}
	secretFile := filepath.Join(stateRoot, "contexts", "internal-dogfood", "vault", "secrets.env")
	if err := os.WriteFile(secretFile, []byte("API_TOKEN=super-secret\n"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	checks, failureCount := runPaasDoctorChecks()
	if failureCount == 0 {
		t.Fatalf("expected doctor failures, got checks=%#v", checks)
	}
	statusByName := make(map[string]bool, len(checks))
	for _, check := range checks {
		statusByName[strings.TrimSpace(check.Name)] = check.OK
	}
	requiredFailures := []string{
		"state_root_outside_repo",
		"context_vault_outside_repo",
		"repo_private_state_artifacts",
		"repo_plaintext_secret_exposure",
	}
	for _, name := range requiredFailures {
		ok, exists := statusByName[name]
		if !exists {
			t.Fatalf("expected doctor check %q to exist in %#v", name, checks)
		}
		if ok {
			t.Fatalf("expected doctor check %q to fail in %#v", name, checks)
		}
	}
}

func TestPaasAgentEnableStatusRunOnceLogsDisable(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	contextDir := filepath.Join(stateRoot, "contexts", defaultPaasContext)
	eventsDir := filepath.Join(contextDir, "events")
	if err := os.MkdirAll(eventsDir, 0o700); err != nil {
		t.Fatalf("mkdir events dir: %v", err)
	}
	deployEvent := []byte("{\"timestamp\":\"2026-02-17T16:00:00Z\",\"source\":\"deploy\",\"command\":\"deploy apply\",\"status\":\"failed\",\"target\":\"edge-a\",\"message\":\"deploy failed\"}\n")
	if err := os.WriteFile(filepath.Join(eventsDir, "deployments.jsonl"), deployEvent, 0o600); err != nil {
		t.Fatalf("write deployments event: %v", err)
	}

	enableRaw := captureStdout(t, func() {
		cmdPaas([]string{"agent", "enable", "--name", "ops-agent", "--targets", "all", "--profile", "default", "--json"})
	})
	enableEnv := parsePaasEnvelope(t, enableRaw)
	if enableEnv.Command != "agent enable" || enableEnv.Mode != "live" {
		t.Fatalf("unexpected agent enable envelope: %#v", enableEnv)
	}
	if enableEnv.Fields["name"] != "ops-agent" || enableEnv.Fields["enabled"] != "true" {
		t.Fatalf("unexpected agent enable fields: %#v", enableEnv.Fields)
	}

	statusRaw := captureStdout(t, func() {
		cmdPaas([]string{"agent", "status", "--name", "ops-agent", "--json"})
	})
	var statusPayload struct {
		Command string            `json:"command"`
		Mode    string            `json:"mode"`
		Count   int               `json:"count"`
		Data    []paasAgentConfig `json:"data"`
	}
	if err := json.Unmarshal([]byte(statusRaw), &statusPayload); err != nil {
		t.Fatalf("decode agent status payload: %v output=%q", err, statusRaw)
	}
	if statusPayload.Command != "agent status" || statusPayload.Mode != "live" || statusPayload.Count != 1 || len(statusPayload.Data) != 1 {
		t.Fatalf("unexpected agent status payload: %#v", statusPayload)
	}
	if !statusPayload.Data[0].Enabled || statusPayload.Data[0].Name != "ops-agent" {
		t.Fatalf("unexpected agent row: %#v", statusPayload.Data[0])
	}

	runRaw := captureStdout(t, func() {
		cmdPaas([]string{"agent", "run-once", "--name", "ops-agent", "--json"})
	})
	runEnv := parsePaasEnvelope(t, runRaw)
	if runEnv.Command != "agent run-once" || runEnv.Mode != "live" {
		t.Fatalf("unexpected agent run-once envelope: %#v", runEnv)
	}
	if strings.TrimSpace(runEnv.Fields["run_id"]) == "" {
		t.Fatalf("expected non-empty run_id in run-once output: %#v", runEnv.Fields)
	}
	if runEnv.Fields["status"] != "queued" && runEnv.Fields["status"] != "noop" && runEnv.Fields["status"] != "blocked" {
		t.Fatalf("unexpected run status %q in %#v", runEnv.Fields["status"], runEnv.Fields)
	}

	logsRaw := captureStdout(t, func() {
		cmdPaas([]string{"agent", "logs", "--name", "ops-agent", "--tail", "20", "--json"})
	})
	var logsPayload struct {
		Command string               `json:"command"`
		Mode    string               `json:"mode"`
		Count   int                  `json:"count"`
		Data    []paasAgentRunRecord `json:"data"`
	}
	if err := json.Unmarshal([]byte(logsRaw), &logsPayload); err != nil {
		t.Fatalf("decode agent logs payload: %v output=%q", err, logsRaw)
	}
	if logsPayload.Command != "agent logs" || logsPayload.Mode != "live" || logsPayload.Count < 1 || len(logsPayload.Data) < 1 {
		t.Fatalf("unexpected logs payload: %#v", logsPayload)
	}
	if logsPayload.Data[0].Agent != "ops-agent" {
		t.Fatalf("expected logs for ops-agent, got %#v", logsPayload.Data[0])
	}

	disableRaw := captureStdout(t, func() {
		cmdPaas([]string{"agent", "disable", "--name", "ops-agent", "--json"})
	})
	disableEnv := parsePaasEnvelope(t, disableRaw)
	if disableEnv.Command != "agent disable" || disableEnv.Fields["enabled"] != "false" {
		t.Fatalf("unexpected disable envelope: %#v", disableEnv)
	}
}

func TestPaasAgentRunOnceOfflineFakeCodexDeterministicSmoke(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	t.Setenv(paasAgentOfflineFakeCodexEnvKey, "true")
	t.Setenv(paasAgentOfflineFakeCodexCmdEnvKey, quoteSingle(filepath.Join(root, "tools", "dyad", "fake-codex.sh")))

	prevRequire := requirePaasAgentCodexProfileFn
	prevAuth := codexProfileAuthStatusFn
	t.Cleanup(func() {
		requirePaasAgentCodexProfileFn = prevRequire
		codexProfileAuthStatusFn = prevAuth
	})
	requirePaasAgentCodexProfileFn = func(key string) (codexProfile, error) {
		return codexProfile{ID: "weekly", Email: "ops@example.com"}, nil
	}
	codexProfileAuthStatusFn = func(profile codexProfile) codexAuthCacheStatus {
		return codexAuthCacheStatus{Path: "/tmp/codex-auth.json", Exists: true}
	}

	_, err = savePaasRemediationPolicy(currentPaasContext(), paasRemediationPolicy{
		DefaultAction: paasRemediationActionAutoAllow,
		Severity: map[string]string{
			paasIncidentSeverityInfo:     paasRemediationActionAutoAllow,
			paasIncidentSeverityWarning:  paasRemediationActionAutoAllow,
			paasIncidentSeverityCritical: paasRemediationActionAutoAllow,
		},
	})
	if err != nil {
		t.Fatalf("save remediation policy: %v", err)
	}

	contextDir := filepath.Join(stateRoot, "contexts", defaultPaasContext)
	eventsDir := filepath.Join(contextDir, "events")
	if err := os.MkdirAll(eventsDir, 0o700); err != nil {
		t.Fatalf("mkdir events dir: %v", err)
	}
	deployEvent := []byte("{\"timestamp\":\"2026-02-17T17:00:00Z\",\"source\":\"deploy\",\"command\":\"deploy apply\",\"status\":\"failed\",\"target\":\"edge-a\",\"message\":\"deploy failed smoke\"}\n")
	if err := os.WriteFile(filepath.Join(eventsDir, "deployments.jsonl"), deployEvent, 0o600); err != nil {
		t.Fatalf("write deployments event: %v", err)
	}

	captureStdout(t, func() {
		cmdPaas([]string{"agent", "enable", "--name", "ops-agent", "--profile", "weekly", "--json"})
	})
	runRaw := captureStdout(t, func() {
		cmdPaas([]string{"agent", "run-once", "--name", "ops-agent", "--json"})
	})
	runEnv := parsePaasEnvelope(t, runRaw)
	if runEnv.Command != "agent run-once" {
		t.Fatalf("unexpected run-once envelope: %#v", runEnv)
	}
	if runEnv.Fields["status"] != "queued" {
		t.Fatalf("expected queued status, got %#v", runEnv.Fields)
	}
	if runEnv.Fields["execution_mode"] != paasAgentExecutionModeOfflineFakeCodex {
		t.Fatalf("expected offline fake-codex execution mode, got %#v", runEnv.Fields)
	}
	if !strings.Contains(runEnv.Fields["execution_note"], "member: actor") {
		t.Fatalf("expected deterministic fake-codex note, got %#v", runEnv.Fields)
	}
	if strings.TrimSpace(runEnv.Fields["incident_correlation_id"]) == "" {
		t.Fatalf("expected incident correlation id, got %#v", runEnv.Fields)
	}
	if strings.TrimSpace(runEnv.Fields["artifact_path"]) == "" {
		t.Fatalf("expected artifact path, got %#v", runEnv.Fields)
	}
	if _, err := os.Stat(runEnv.Fields["artifact_path"]); err != nil {
		t.Fatalf("expected run artifact file: %v", err)
	}

	logsRaw := captureStdout(t, func() {
		cmdPaas([]string{"agent", "logs", "--name", "ops-agent", "--tail", "1", "--json"})
	})
	var logsPayload struct {
		Count int                  `json:"count"`
		Data  []paasAgentRunRecord `json:"data"`
	}
	if err := json.Unmarshal([]byte(logsRaw), &logsPayload); err != nil {
		t.Fatalf("decode logs payload: %v output=%q", err, logsRaw)
	}
	if logsPayload.Count != 1 || len(logsPayload.Data) != 1 {
		t.Fatalf("expected one run log row, got %#v", logsPayload)
	}
	if logsPayload.Data[0].ExecutionMode != paasAgentExecutionModeOfflineFakeCodex {
		t.Fatalf("expected run log execution mode, got %#v", logsPayload.Data[0])
	}
	if strings.TrimSpace(logsPayload.Data[0].IncidentCorrID) == "" {
		t.Fatalf("expected run log incident correlation id, got %#v", logsPayload.Data[0])
	}
	if strings.TrimSpace(logsPayload.Data[0].ArtifactPath) == "" {
		t.Fatalf("expected run log artifact path, got %#v", logsPayload.Data[0])
	}
}

func TestPaasAgentApproveDenyFlowPersistsDecision(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	contextDir := filepath.Join(stateRoot, "contexts", defaultPaasContext)
	eventsDir := filepath.Join(contextDir, "events")
	if err := os.MkdirAll(eventsDir, 0o700); err != nil {
		t.Fatalf("mkdir events dir: %v", err)
	}
	deployEvent := []byte("{\"timestamp\":\"2026-02-17T16:00:00Z\",\"source\":\"deploy\",\"command\":\"deploy apply\",\"status\":\"failed\",\"target\":\"edge-a\",\"message\":\"deploy failed\"}\n")
	if err := os.WriteFile(filepath.Join(eventsDir, "deployments.jsonl"), deployEvent, 0o600); err != nil {
		t.Fatalf("write deployments event: %v", err)
	}

	captureStdout(t, func() {
		cmdPaas([]string{"agent", "enable", "--name", "ops-agent", "--json"})
	})
	runRaw := captureStdout(t, func() {
		cmdPaas([]string{"agent", "run-once", "--name", "ops-agent", "--json"})
	})
	runEnv := parsePaasEnvelope(t, runRaw)
	runID := strings.TrimSpace(runEnv.Fields["run_id"])
	if runID == "" {
		t.Fatalf("expected run_id from run-once, got %#v", runEnv.Fields)
	}

	approveRaw := captureStdout(t, func() {
		cmdPaas([]string{"agent", "approve", "--run", runID, "--note", "manual approval", "--json"})
	})
	approveEnv := parsePaasEnvelope(t, approveRaw)
	if approveEnv.Command != "agent approve" || approveEnv.Fields["decision"] != paasApprovalDecisionApproved {
		t.Fatalf("unexpected approve envelope: %#v", approveEnv)
	}

	denyRaw := captureStdout(t, func() {
		cmdPaas([]string{"agent", "deny", "--run", runID, "--note", "rejecting action", "--json"})
	})
	denyEnv := parsePaasEnvelope(t, denyRaw)
	if denyEnv.Command != "agent deny" || denyEnv.Fields["decision"] != paasApprovalDecisionDenied {
		t.Fatalf("unexpected deny envelope: %#v", denyEnv)
	}

	store, _, err := loadPaasAgentApprovalStore(currentPaasContext())
	if err != nil {
		t.Fatalf("load approval store: %v", err)
	}
	if len(store.Decisions) == 0 {
		t.Fatalf("expected approval decisions in store")
	}
	if strings.TrimSpace(store.Decisions[0].RunID) != runID || store.Decisions[0].Decision != paasApprovalDecisionDenied {
		t.Fatalf("expected latest decision deny for run, got %#v", store.Decisions[0])
	}
}

func TestPaasAgentRunOnceBlockedByActiveLock(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	captureStdout(t, func() {
		cmdPaas([]string{"agent", "enable", "--name", "ops-agent", "--json"})
	})
	lockPath, err := resolvePaasAgentLockPath(currentPaasContext(), "ops-agent")
	if err != nil {
		t.Fatalf("resolve lock path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}
	now := time.Now().UTC()
	if err := savePaasAgentLockState(lockPath, paasAgentLockState{
		Agent:       "ops-agent",
		Owner:       "lock-owner",
		PID:         1234,
		AcquiredAt:  now.Format(time.RFC3339Nano),
		HeartbeatAt: now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("seed active lock: %v", err)
	}

	raw := captureStdout(t, func() {
		cmdPaas([]string{"agent", "run-once", "--name", "ops-agent", "--json"})
	})
	env := parsePaasEnvelope(t, raw)
	if env.Command != "agent run-once" {
		t.Fatalf("unexpected run-once envelope: %#v", env)
	}
	if env.Fields["status"] != "blocked" {
		t.Fatalf("expected blocked status for active lock, got %#v", env.Fields)
	}
	if !strings.Contains(env.Fields["message"], "lock unavailable") {
		t.Fatalf("expected lock unavailable message, got %#v", env.Fields)
	}
}

func TestPaasStateRootGuardrailRejectsRepoState(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Setenv(paasStateRootEnvKey, filepath.Join(cwd, ".tmp", "paas-guardrail-state"))
	t.Setenv(paasAllowRepoStateEnvKey, "")
	if err := enforcePaasStateRootIsolationGuardrail(); err == nil {
		t.Fatalf("expected guardrail failure for repo-local state root")
	}
	t.Setenv(paasAllowRepoStateEnvKey, "true")
	if err := enforcePaasStateRootIsolationGuardrail(); err != nil {
		t.Fatalf("expected override to bypass repo-state guardrail: %v", err)
	}
}

func TestRedactPaasSensitiveFields(t *testing.T) {
	fields := map[string]string{
		"api_token":                "abc",
		"db_password":              "super-secret",
		"compose_secret_guardrail": "ok",
		"compose_secret_findings":  "2",
		"vault_file":               "/tmp/vault.env",
	}
	redacted := redactPaasSensitiveFields(fields)
	if redacted["api_token"] != "<redacted>" {
		t.Fatalf("expected api_token redaction, got %#v", redacted)
	}
	if redacted["db_password"] != "<redacted>" {
		t.Fatalf("expected db_password redaction, got %#v", redacted)
	}
	if redacted["compose_secret_guardrail"] != "ok" || redacted["compose_secret_findings"] != "2" {
		t.Fatalf("expected compose guardrail fields to remain visible, got %#v", redacted)
	}
	if redacted["vault_file"] != "/tmp/vault.env" {
		t.Fatalf("expected non-sensitive field unchanged, got %#v", redacted)
	}
}

func TestPaasTargetCRUDWithLocalStore(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-1", "--host", "10.0.0.4", "--user", "root"})
	})
	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-2", "--host", "10.0.0.5", "--user", "admin"})
	})

	listRaw := captureStdout(t, func() {
		cmdPaas([]string{"target", "list", "--json"})
	})
	listPayload := parseTargetListPayload(t, listRaw)
	if listPayload.Command != "target list" {
		t.Fatalf("expected command=target list, got %q", listPayload.Command)
	}
	if listPayload.Mode != "live" {
		t.Fatalf("expected mode=live, got %q", listPayload.Mode)
	}
	if listPayload.Count != 2 {
		t.Fatalf("expected count=2, got %d", listPayload.Count)
	}
	if listPayload.CurrentTarget != "edge-1" {
		t.Fatalf("expected current target edge-1, got %q", listPayload.CurrentTarget)
	}

	captureStdout(t, func() {
		cmdPaas([]string{"target", "use", "--target", "edge-2"})
	})
	afterUseRaw := captureStdout(t, func() {
		cmdPaas([]string{"target", "list", "--json"})
	})
	afterUse := parseTargetListPayload(t, afterUseRaw)
	if afterUse.CurrentTarget != "edge-2" {
		t.Fatalf("expected current target edge-2, got %q", afterUse.CurrentTarget)
	}

	captureStdout(t, func() {
		cmdPaas([]string{"target", "remove", "--target", "edge-1"})
	})
	afterRemoveRaw := captureStdout(t, func() {
		cmdPaas([]string{"target", "list", "--json"})
	})
	afterRemove := parseTargetListPayload(t, afterRemoveRaw)
	if afterRemove.Count != 1 {
		t.Fatalf("expected count=1 after remove, got %d", afterRemove.Count)
	}
	if len(afterRemove.Data) != 1 || afterRemove.Data[0].Name != "edge-2" {
		t.Fatalf("expected edge-2 remaining, got %#v", afterRemove.Data)
	}
}

func TestPaasTargetIngressBaselineRendersArtifactsAndPersistsMetadata(t *testing.T) {
	stateRoot := t.TempDir()
	artifactsDir := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-ingress", "--host", "10.0.0.9", "--user", "root"})
	})

	captureStdout(t, func() {
		cmdPaas([]string{
			"target", "ingress-baseline",
			"--target", "edge-ingress",
			"--domain", "apps.example.com",
			"--acme-email", "ops@example.com",
			"--lb-mode", "dns",
			"--output-dir", artifactsDir,
			"--json",
		})
	})

	required := []string{
		"docker-compose.traefik.yaml",
		"traefik.yaml",
		"dynamic.yaml",
		"README.md",
		"acme.json",
	}
	for _, name := range required {
		path := filepath.Join(artifactsDir, name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected artifact %s to exist: %v", path, err)
		}
	}

	listRaw := captureStdout(t, func() {
		cmdPaas([]string{"target", "list", "--json"})
	})
	listPayload := parseTargetListPayload(t, listRaw)
	if len(listPayload.Data) != 1 {
		t.Fatalf("expected 1 target in list payload, got %#v", listPayload.Data)
	}
	row := listPayload.Data[0]
	if row.IngressProvider != paasIngressProviderTraefik {
		t.Fatalf("expected ingress provider %q, got %q", paasIngressProviderTraefik, row.IngressProvider)
	}
	if row.IngressDomain != "apps.example.com" {
		t.Fatalf("expected ingress domain apps.example.com, got %q", row.IngressDomain)
	}
	if row.IngressLBMode != "dns" {
		t.Fatalf("expected ingress lb mode dns, got %q", row.IngressLBMode)
	}
}

func TestPaasAppAddonContractEnableListDisable(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	contractRaw := captureStdout(t, func() {
		cmdPaas([]string{"app", "addon", "contract", "--json"})
	})
	var contractPayload struct {
		Command string                  `json:"command"`
		Count   int                     `json:"count"`
		Data    []paasAddonPackContract `json:"data"`
	}
	if err := json.Unmarshal([]byte(contractRaw), &contractPayload); err != nil {
		t.Fatalf("decode addon contract payload: %v output=%q", err, contractRaw)
	}
	if contractPayload.Command != "app addon contract" || contractPayload.Count < 5 {
		t.Fatalf("unexpected addon contract payload: %#v", contractPayload)
	}
	if len(contractPayload.Data) == 0 || contractPayload.Data[0].MergeStrategy != paasAddonMergeStrategyAdditiveNoOverride {
		t.Fatalf("expected merge strategy contract in response, got %#v", contractPayload.Data)
	}

	enableRaw := captureStdout(t, func() {
		cmdPaas([]string{
			"app", "addon", "enable",
			"--app", "billing-api",
			"--pack", "postgres",
			"--name", "db-main",
			"--set", "POSTGRES_DB=billing,POSTGRES_USER=svc",
			"--json",
		})
	})
	enableEnv := parsePaasEnvelope(t, enableRaw)
	if enableEnv.Command != "app addon enable" {
		t.Fatalf("expected addon enable envelope, got %#v", enableEnv)
	}
	if enableEnv.Fields["merge_strategy"] != paasAddonMergeStrategyAdditiveNoOverride {
		t.Fatalf("expected merge strategy in addon enable output, got %#v", enableEnv.Fields)
	}
	fragmentPath := strings.TrimSpace(enableEnv.Fields["fragment_path"])
	if fragmentPath == "" {
		t.Fatalf("expected fragment path in enable output, got %#v", enableEnv.Fields)
	}
	fragmentRaw, err := os.ReadFile(fragmentPath)
	if err != nil {
		t.Fatalf("read addon fragment: %v", err)
	}
	fragment := string(fragmentRaw)
	if !strings.Contains(fragment, "image: postgres:16") || !strings.Contains(fragment, "POSTGRES_DB: \"billing\"") {
		t.Fatalf("expected rendered postgres fragment content, got %q", fragment)
	}

	listRaw := captureStdout(t, func() {
		cmdPaas([]string{"app", "addon", "list", "--app", "billing-api", "--json"})
	})
	var listPayload struct {
		Command string            `json:"command"`
		Count   int               `json:"count"`
		Data    []paasAddonRecord `json:"data"`
	}
	if err := json.Unmarshal([]byte(listRaw), &listPayload); err != nil {
		t.Fatalf("decode addon list payload: %v output=%q", err, listRaw)
	}
	if listPayload.Command != "app addon list" || listPayload.Count != 1 || len(listPayload.Data) != 1 {
		t.Fatalf("unexpected addon list payload: %#v", listPayload)
	}
	if listPayload.Data[0].App != "billing-api" || listPayload.Data[0].Pack != "postgres" || listPayload.Data[0].Name != "db-main" {
		t.Fatalf("unexpected addon row: %#v", listPayload.Data[0])
	}

	disableRaw := captureStdout(t, func() {
		cmdPaas([]string{
			"app", "addon", "disable",
			"--app", "billing-api",
			"--name", "db-main",
			"--json",
		})
	})
	disableEnv := parsePaasEnvelope(t, disableRaw)
	if disableEnv.Command != "app addon disable" || disableEnv.Fields["removed"] != "true" {
		t.Fatalf("unexpected addon disable output: %#v", disableEnv)
	}
	if _, err := os.Stat(fragmentPath); !os.IsNotExist(err) {
		t.Fatalf("expected fragment file removal for disabled add-on, stat err=%v", err)
	}

	afterRaw := captureStdout(t, func() {
		cmdPaas([]string{"app", "addon", "list", "--app", "billing-api", "--json"})
	})
	if err := json.Unmarshal([]byte(afterRaw), &listPayload); err != nil {
		t.Fatalf("decode addon list after disable: %v output=%q", err, afterRaw)
	}
	if listPayload.Count != 0 || len(listPayload.Data) != 0 {
		t.Fatalf("expected empty addon list after disable, got %#v", listPayload)
	}
}

func TestPaasAppAddonEnableSupabaseWalgAndDatabasus(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	walgRaw := captureStdout(t, func() {
		cmdPaas([]string{
			"app", "addon", "enable",
			"--app", "billing-api",
			"--pack", "supabase-walg",
			"--name", "backup-main",
			"--service", "supabase",
			"--json",
		})
	})
	walgEnv := parsePaasEnvelope(t, walgRaw)
	if walgEnv.Command != "app addon enable" || walgEnv.Fields["pack"] != "supabase-walg" {
		t.Fatalf("unexpected supabase-walg enable output: %#v", walgEnv)
	}
	walgFragmentPath := strings.TrimSpace(walgEnv.Fields["fragment_path"])
	walgFragment, err := os.ReadFile(walgFragmentPath)
	if err != nil {
		t.Fatalf("read supabase-walg fragment: %v", err)
	}
	walgText := string(walgFragment)
	if !strings.Contains(walgText, "supabase-backup:") || !strings.Contains(walgText, "supabase-restore:") {
		t.Fatalf("expected backup and restore services, got %q", walgText)
	}
	if !strings.Contains(walgText, "WALG_S3_PREFIX") {
		t.Fatalf("expected WAL-G env placeholders, got %q", walgText)
	}

	databasusRaw := captureStdout(t, func() {
		cmdPaas([]string{
			"app", "addon", "enable",
			"--app", "billing-api",
			"--pack", "databasus",
			"--name", "dbmeta-main",
			"--service", "dbmeta",
			"--json",
		})
	})
	databasusEnv := parsePaasEnvelope(t, databasusRaw)
	if databasusEnv.Command != "app addon enable" || databasusEnv.Fields["pack"] != "databasus" {
		t.Fatalf("unexpected databasus enable output: %#v", databasusEnv)
	}
	databasusFragmentPath := strings.TrimSpace(databasusEnv.Fields["fragment_path"])
	databasusFragment, err := os.ReadFile(databasusFragmentPath)
	if err != nil {
		t.Fatalf("read databasus fragment: %v", err)
	}
	databasusText := string(databasusFragment)
	if !strings.Contains(databasusText, "image: databasus/databasus:latest") {
		t.Fatalf("expected databasus image reference, got %q", databasusText)
	}
	if strings.Contains(databasusText, "ports:") {
		t.Fatalf("databasus add-on must not expose host ports, got %q", databasusText)
	}
}

func TestPaasDeployInvalidStrategyShowsUsage(t *testing.T) {
	out := captureStdout(t, func() {
		cmdPaas([]string{"deploy", "--strategy", "invalid"})
	})
	if !strings.Contains(out, paasDeployUsageText) {
		t.Fatalf("expected deploy usage for invalid strategy, got %q", out)
	}
}

func TestPaasCommandActionSetsArePopulated(t *testing.T) {
	tests := []struct {
		name    string
		actions []subcommandAction
	}{
		{name: "paas", actions: paasActions},
		{name: "paas target", actions: paasTargetActions},
		{name: "paas app", actions: paasAppActions},
		{name: "paas alert", actions: paasAlertActions},
		{name: "paas secret", actions: paasSecretActions},
		{name: "paas ai", actions: paasAIActions},
		{name: "paas context", actions: paasContextActions},
		{name: "paas agent", actions: paasAgentActions},
		{name: "paas events", actions: paasEventsActions},
		{name: "paas backup", actions: paasBackupActions},
		{name: "paas cloud", actions: paasCloudActions},
	}
	for _, tc := range tests {
		if len(tc.actions) == 0 {
			t.Fatalf("%s actions should not be empty", tc.name)
		}
		for _, action := range tc.actions {
			if action.Name == "" {
				t.Fatalf("%s action name should not be empty", tc.name)
			}
			if action.Description == "" {
				t.Fatalf("%s action description should not be empty", tc.name)
			}
		}
	}
}

func TestPaasActionNamesMatchDispatchSwitches(t *testing.T) {
	expectActionNames(t, "paas", paasActions, []string{"target", "app", "deploy", "rollback", "logs", "alert", "secret", "ai", "context", "doctor", "agent", "events", "backup", "taskboard", "cloud"})
	expectActionNames(t, "paas target", paasTargetActions, []string{"add", "list", "check", "use", "remove", "bootstrap", "ingress-baseline"})
	expectActionNames(t, "paas app", paasAppActions, []string{"init", "list", "status", "remove", "addon"})
	expectActionNames(t, "paas app addon", paasAppAddonActions, []string{"contract", "enable", "list", "disable"})
	expectActionNames(t, "paas alert", paasAlertActions, []string{"setup-telegram", "test", "history", "acknowledge", "policy", "ingress-tls"})
	expectActionNames(t, "paas secret", paasSecretActions, []string{"set", "get", "unset", "list", "key"})
	expectActionNames(t, "paas ai", paasAIActions, []string{"plan", "inspect", "fix"})
	expectActionNames(t, "paas context", paasContextActions, []string{"create", "init", "list", "use", "show", "remove", "export", "import"})
	expectActionNames(t, "paas agent", paasAgentActions, []string{"enable", "disable", "status", "logs", "run-once", "approve", "deny"})
	expectActionNames(t, "paas events", paasEventsActions, []string{"list"})
	expectActionNames(t, "paas backup", paasBackupActions, []string{"contract", "run", "restore", "status"})
	expectActionNames(t, "paas cloud", paasCloudActions, []string{"status", "use", "push", "pull"})
}

func TestNormalizeImagePlatformArch(t *testing.T) {
	tests := map[string]string{
		"":              "",
		"linux/amd64":   "amd64",
		"amd64":         "amd64",
		"linux/aarch64": "arm64",
		"arm64":         "arm64",
	}
	for input, expected := range tests {
		got := normalizeImagePlatformArch(input)
		if got != expected {
			t.Fatalf("normalizeImagePlatformArch(%q) = %q, expected %q", input, got, expected)
		}
	}
}

func TestPaasSecretKeyConvention(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)
	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-a", "--host", "10.0.0.7", "--user", "root"})
	})
	out := captureStdout(t, func() {
		cmdPaas([]string{"secret", "key", "--app", "billing-api", "--name", "stripe_api_key"})
	})
	got := strings.TrimSpace(out)
	want := "PAAS__CTX_DEFAULT__NS_DEFAULT__APP_BILLING_API__TARGET_EDGE_A__VAR_STRIPE_API_KEY"
	if got != want {
		t.Fatalf("unexpected vault key convention: got=%q want=%q", got, want)
	}
}

func TestPaasSecretKeyNamespaceSegment(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)
	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-a", "--host", "10.0.0.7", "--user", "root"})
	})
	out := captureStdout(t, func() {
		cmdPaas([]string{"secret", "key", "--app", "billing-api", "--namespace", "production", "--name", "stripe_api_key"})
	})
	got := strings.TrimSpace(out)
	want := "PAAS__CTX_DEFAULT__NS_PRODUCTION__APP_BILLING_API__TARGET_EDGE_A__VAR_STRIPE_API_KEY"
	if got != want {
		t.Fatalf("unexpected namespaced vault key convention: got=%q want=%q", got, want)
	}
}

func TestResolvePaasContextVaultFile(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)
	t.Setenv("SI_VAULT_FILE", "")
	got := resolvePaasContextVaultFile("")
	want := filepath.Join(stateRoot, "contexts", "default", "vault", "secrets.env")
	if got != want {
		t.Fatalf("expected context-scoped default vault file: got=%q want=%q", got, want)
	}
	explicit := resolvePaasContextVaultFile("/tmp/custom.env")
	if explicit != "/tmp/custom.env" {
		t.Fatalf("expected explicit vault file to win, got=%q", explicit)
	}
	t.Setenv("SI_VAULT_FILE", "/tmp/global.env")
	if resolved := resolvePaasContextVaultFile(""); resolved != "" {
		t.Fatalf("expected env-driven vault resolution passthrough, got=%q", resolved)
	}
}

func TestPaasContextMetadataExportImportRoundTrip(t *testing.T) {
	stateRoot := t.TempDir()
	exportPath := filepath.Join(t.TempDir(), "default-context-export.json")
	t.Setenv(paasStateRootEnvKey, stateRoot)

	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-a", "--host", "203.0.113.7", "--user", "root"})
	})
	if err := recordPaasSuccessfulRelease("billing-api", "rel-roundtrip-001"); err != nil {
		t.Fatalf("record deploy history: %v", err)
	}
	captureStdout(t, func() {
		cmdPaas([]string{
			"deploy", "webhook", "map", "add",
			"--provider", "github",
			"--repo", "acme/billing-api",
			"--branch", "main",
			"--app", "billing-api",
			"--strategy", "rolling",
		})
	})

	captureStdout(t, func() {
		cmdPaas([]string{"context", "export", "--output", exportPath, "--json"})
	})
	raw, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read export file: %v", err)
	}
	if key, sensitive := detectPaasSensitiveMetadataKey(raw); sensitive {
		t.Fatalf("expected scrubbed export metadata, found sensitive key %q", key)
	}

	captureStdout(t, func() {
		cmdPaas([]string{"context", "import", "--name", "customer-42", "--input", exportPath, "--json"})
	})

	importedTargets, err := loadPaasTargetStore("customer-42")
	if err != nil {
		t.Fatalf("load imported target store: %v", err)
	}
	if len(importedTargets.Targets) != 1 || importedTargets.Targets[0].Name != "edge-a" {
		t.Fatalf("unexpected imported targets: %#v", importedTargets.Targets)
	}

	importedDeploys, err := loadPaasDeployHistoryStoreForContext("customer-42")
	if err != nil {
		t.Fatalf("load imported deploy history: %v", err)
	}
	appHistory, ok := importedDeploys.Apps["billing-api"]
	if !ok || appHistory.CurrentRelease != "rel-roundtrip-001" {
		t.Fatalf("expected imported deploy history for billing-api, got %#v", importedDeploys.Apps)
	}

	importedMappings, err := loadPaasWebhookMappingStore("customer-42")
	if err != nil {
		t.Fatalf("load imported webhook mappings: %v", err)
	}
	if len(importedMappings.Mappings) != 1 {
		t.Fatalf("expected one imported webhook mapping, got %#v", importedMappings.Mappings)
	}
	if importedMappings.Mappings[0].Repo != "acme/billing-api" || importedMappings.Mappings[0].App != "billing-api" {
		t.Fatalf("unexpected imported webhook mapping: %#v", importedMappings.Mappings[0])
	}
}

func TestPaasContextImportRejectsSecretKeys(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)
	importPath := filepath.Join(t.TempDir(), "bad-import.json")
	body := []byte("{\"schema_version\":1,\"context\":\"default\",\"metadata\":{\"secrets\":{\"db_password\":\"top-secret\"}}}\n")
	if err := os.WriteFile(importPath, body, 0o600); err != nil {
		t.Fatalf("write import payload: %v", err)
	}
	if _, err := importPaasContextMetadata("", importPath, false); err == nil {
		t.Fatalf("expected import rejection for secret-like keys")
	}
}

func TestResolvePaasComposePlaintextFindings(t *testing.T) {
	composePath := filepath.Join(t.TempDir(), "compose.yaml")
	content := strings.Join([]string{
		"services:",
		"  api:",
		"    environment:",
		"      - DB_PASSWORD=super-secret",
		"      - API_TOKEN=${API_TOKEN}",
		"      SECRET_TOKEN: ${SECRET_TOKEN}",
		"",
	}, "\n")
	if err := os.WriteFile(composePath, []byte(content), 0o600); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	findings, err := resolvePaasComposePlaintextFindings(composePath)
	if err != nil {
		t.Fatalf("resolve findings: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected exactly one plaintext finding, got %#v", findings)
	}
	if findings[0].Key != "DB_PASSWORD" {
		t.Fatalf("expected DB_PASSWORD finding, got %#v", findings[0])
	}
}

func TestEnforcePaasPlaintextSecretGuardrailDoesNotLeakValue(t *testing.T) {
	composePath := filepath.Join(t.TempDir(), "compose.yaml")
	secretValue := "super-secret-value-123"
	content := "services:\n  api:\n    environment:\n      - DB_PASSWORD=" + secretValue + "\n"
	if err := os.WriteFile(composePath, []byte(content), 0o600); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	_, err := enforcePaasPlaintextSecretGuardrail(composePath, false)
	if err == nil {
		t.Fatalf("expected guardrail error for plaintext secret value")
	}
	if strings.Contains(err.Error(), secretValue) {
		t.Fatalf("guardrail error leaked secret value: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("expected redaction marker in guardrail error: %q", err.Error())
	}
}

func TestRunPaasVaultDeployGuardrailTrusted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_SETTINGS_HOME", home)
	t.Setenv("SI_SUN_BASE_URL", "")
	t.Setenv("SI_SUN_TOKEN", "")
	server, store := newSunTestServer(t, "acme", "token-paas-vault-guardrail")
	defer server.Close()

	settings := defaultSettings()
	applySettingsDefaults(&settings)
	settings.Sun.BaseURL = server.URL
	settings.Sun.Token = "token-paas-vault-guardrail"
	settings.Sun.Account = "acme"
	settings.Vault.File = "paas-guardrail"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	target, err := vaultResolveTarget(settings, "", true)
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	store.mu.Lock()
	objectKey := store.key(vaultSunKVKind(target), "APP_ENV")
	store.payloads[objectKey] = []byte("prod\n")
	store.revs[objectKey] = 1
	store.metadata[objectKey] = map[string]any{"deleted": false}
	store.created[objectKey] = "2026-01-01T00:00:00Z"
	store.updated[objectKey] = "2026-01-02T00:00:00Z"
	store.mu.Unlock()

	result, err := runPaasVaultDeployGuardrail("", false)
	if err != nil {
		t.Fatalf("run vault guardrail: %v", err)
	}
	if !result.Trusted {
		t.Fatalf("expected trusted vault guardrail result, got %#v", result)
	}
	if result.RecipientCount != 1 {
		t.Fatalf("expected one recipient, got %#v", result)
	}
}

func TestRunPaasVaultDeployGuardrailAllowUntrusted(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_SETTINGS_HOME", home)
	vaultFile := filepath.Join(root, ".env")
	trustStore := filepath.Join(root, "trust.json")
	t.Setenv("SI_SUN_BASE_URL", "")
	t.Setenv("SI_SUN_TOKEN", "")
	doc := fmt.Sprintf("%s%s\nAPP_ENV=prod\n", vault.VaultRecipientPrefix, "age1examplerecipient000000000000000000000000000000000000000000000000")
	if err := os.WriteFile(vaultFile, []byte(doc), 0o600); err != nil {
		t.Fatalf("write vault file: %v", err)
	}
	t.Setenv("SI_VAULT_FILE", vaultFile)
	t.Setenv("SI_VAULT_TRUST_STORE", trustStore)

	_, err := runPaasVaultDeployGuardrail("", false)
	if err == nil {
		t.Fatalf("expected sun auth error without configured token")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "sun token is required") {
		t.Fatalf("expected sun token error, got: %v", err)
	}
	_, err = runPaasVaultDeployGuardrail("", true)
	if err == nil {
		t.Fatalf("expected sun auth error even with allow-untrusted override")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "sun token is required") {
		t.Fatalf("expected sun token error with override, got: %v", err)
	}
}

func TestEnforcePaasSecretRevealGuardrail(t *testing.T) {
	if err := enforcePaasSecretRevealGuardrail(false, false); err != nil {
		t.Fatalf("unexpected error when reveal=false: %v", err)
	}
	if err := enforcePaasSecretRevealGuardrail(true, false); err == nil {
		t.Fatalf("expected error when reveal=true without allow-plaintext")
	}
	if err := enforcePaasSecretRevealGuardrail(true, true); err != nil {
		t.Fatalf("unexpected error when reveal allowed: %v", err)
	}
}

func TestPaasDeployPruneRemovesOldReleasesAndOldEvents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_SETTINGS_HOME", home)
	t.Setenv("SI_SUN_BASE_URL", "")
	t.Setenv("SI_SUN_TOKEN", "")
	server, store := newSunTestServer(t, "acme", "token-paas-prune")
	defer server.Close()

	stateRoot := t.TempDir()
	composePath := filepath.Join(t.TempDir(), "compose.yaml")
	vaultFile := filepath.Join(t.TempDir(), ".env")
	trustStore := filepath.Join(t.TempDir(), "trust.json")
	t.Setenv(paasStateRootEnvKey, stateRoot)
	t.Setenv("SI_VAULT_FILE", vaultFile)
	t.Setenv("SI_VAULT_TRUST_STORE", trustStore)

	settings := defaultSettings()
	applySettingsDefaults(&settings)
	settings.Sun.BaseURL = server.URL
	settings.Sun.Token = "token-paas-prune"
	settings.Sun.Account = "acme"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	target, err := vaultResolveTarget(settings, vaultFile, false)
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	store.mu.Lock()
	objectKey := store.key(vaultSunKVKind(target), "APP_ENV")
	store.payloads[objectKey] = []byte("prod\n")
	store.revs[objectKey] = 1
	store.metadata[objectKey] = map[string]any{"deleted": false}
	store.created[objectKey] = "2026-01-01T00:00:00Z"
	store.updated[objectKey] = "2026-01-02T00:00:00Z"
	store.mu.Unlock()

	composeBody := strings.Join([]string{
		"services:",
		"  api:",
		"    image: nginx:latest",
		"    environment:",
		"      - API_TOKEN=${API_TOKEN}",
		"",
	}, "\n")
	if err := os.WriteFile(composePath, []byte(composeBody), 0o600); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	vaultBody := fmt.Sprintf("%s%s\nAPP_ENV=prod\n", vault.VaultRecipientPrefix, "age1examplerecipient000000000000000000000000000000000000000000000000")
	if err := os.WriteFile(vaultFile, []byte(vaultBody), 0o600); err != nil {
		t.Fatalf("write vault: %v", err)
	}
	// Create multiple releases.
	captureStdout(t, func() {
		cmdPaas([]string{"deploy", "--app", "billing-api", "--compose-file", composePath, "--allow-untrusted-vault"})
	})
	time.Sleep(3 * time.Millisecond)
	captureStdout(t, func() {
		cmdPaas([]string{"deploy", "--app", "billing-api", "--compose-file", composePath, "--allow-untrusted-vault"})
	})
	time.Sleep(3 * time.Millisecond)
	captureStdout(t, func() {
		cmdPaas([]string{"deploy", "--app", "billing-api", "--compose-file", composePath, "--allow-untrusted-vault"})
	})

	eventsPath, err := resolvePaasContextDir(currentPaasContext())
	if err != nil {
		t.Fatalf("resolve context dir: %v", err)
	}
	eventsFile := filepath.Join(eventsPath, "events", "deployments.jsonl")
	oldEvent := fmt.Sprintf("{\"timestamp\":\"%s\",\"command\":\"deploy\",\"status\":\"succeeded\"}\n", time.Now().UTC().Add(-48*time.Hour).Format(time.RFC3339Nano))
	newEvent := fmt.Sprintf("{\"timestamp\":\"%s\",\"command\":\"deploy\",\"status\":\"succeeded\"}\n", time.Now().UTC().Format(time.RFC3339Nano))
	if err := os.WriteFile(eventsFile, []byte(oldEvent+newEvent), 0o600); err != nil {
		t.Fatalf("seed events file: %v", err)
	}

	out := captureStdout(t, func() {
		cmdPaas([]string{"deploy", "prune", "--app", "billing-api", "--keep", "1", "--events-max-age", "24h", "--json"})
	})
	env := parsePaasEnvelope(t, out)
	if env.Command != "deploy prune" {
		t.Fatalf("expected deploy prune command output, got %#v", env)
	}
	if env.Fields["releases_removed"] != "2" {
		t.Fatalf("expected two releases removed, got %#v", env.Fields)
	}
	if env.Fields["events_removed"] != "1" {
		t.Fatalf("expected one old event removed, got %#v", env.Fields)
	}
	root, err := resolvePaasReleaseBundleRoot("")
	if err != nil {
		t.Fatalf("resolve release root: %v", err)
	}
	releasesDir := filepath.Join(root, "billing-api")
	entries, err := os.ReadDir(releasesDir)
	if err != nil {
		t.Fatalf("read releases dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one release directory after prune, got %d", len(entries))
	}
	eventRaw, err := os.ReadFile(eventsFile)
	if err != nil {
		t.Fatalf("read pruned events: %v", err)
	}
	eventText := string(eventRaw)
	if strings.Contains(eventText, "2025-") {
		t.Fatalf("expected old event to be pruned, got %q", eventText)
	}
	if strings.Count(strings.TrimSpace(eventText), "\n")+1 != 2 {
		t.Fatalf("expected two event lines (kept deploy + prune event), got %q", eventText)
	}
}

func TestPaasDeployReconcileDetectsDriftAndOrphans(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SI_SETTINGS_HOME", home)
	t.Setenv("SI_SUN_BASE_URL", "")
	t.Setenv("SI_SUN_TOKEN", "")
	server, store := newSunTestServer(t, "acme", "token-paas-reconcile")
	defer server.Close()

	stateRoot := t.TempDir()
	composePath := filepath.Join(t.TempDir(), "compose.yaml")
	vaultFile := filepath.Join(t.TempDir(), ".env")
	trustStore := filepath.Join(t.TempDir(), "trust.json")
	fakeBinDir := t.TempDir()
	sshLog := filepath.Join(fakeBinDir, "ssh.log")
	t.Setenv(paasStateRootEnvKey, stateRoot)
	t.Setenv("SI_VAULT_FILE", vaultFile)
	t.Setenv("SI_VAULT_TRUST_STORE", trustStore)

	settings := defaultSettings()
	applySettingsDefaults(&settings)
	settings.Sun.BaseURL = server.URL
	settings.Sun.Token = "token-paas-reconcile"
	settings.Sun.Account = "acme"
	if err := saveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	target, err := vaultResolveTarget(settings, vaultFile, false)
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	store.mu.Lock()
	objectKey := store.key(vaultSunKVKind(target), "APP_ENV")
	store.payloads[objectKey] = []byte("prod\n")
	store.revs[objectKey] = 1
	store.metadata[objectKey] = map[string]any{"deleted": false}
	store.created[objectKey] = "2026-01-01T00:00:00Z"
	store.updated[objectKey] = "2026-01-02T00:00:00Z"
	store.mu.Unlock()

	composeBody := strings.Join([]string{
		"services:",
		"  api:",
		"    image: nginx:latest",
		"    environment:",
		"      - API_TOKEN=${API_TOKEN}",
		"",
	}, "\n")
	if err := os.WriteFile(composePath, []byte(composeBody), 0o600); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	vaultBody := fmt.Sprintf("%s%s\nAPP_ENV=prod\n", vault.VaultRecipientPrefix, "age1examplerecipient000000000000000000000000000000000000000000000000")
	if err := os.WriteFile(vaultFile, []byte(vaultBody), 0o600); err != nil {
		t.Fatalf("write vault: %v", err)
	}
	deployRaw := captureStdout(t, func() {
		cmdPaas([]string{"deploy", "--app", "billing-api", "--compose-file", composePath, "--allow-untrusted-vault", "--json"})
	})
	deployEnv := parsePaasEnvelope(t, deployRaw)
	currentRelease := strings.TrimSpace(deployEnv.Fields["release"])
	if currentRelease == "" {
		t.Fatalf("expected release identifier after deploy: %#v", deployEnv.Fields)
	}

	sshScript := filepath.Join(fakeBinDir, "fake-ssh")
	sshContent := strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"printf '%s\\n' \"$*\" >> " + shellSingleQuote(sshLog),
		"cmd=\"$*\"",
		"if [[ \"$cmd\" == *\"ls -1\"* ]]; then",
		"  echo " + shellSingleQuote(sanitizePaasReleasePathSegment(currentRelease)),
		"  echo rel-old-orphan",
		"  exit 0",
		"fi",
		"if [[ \"$cmd\" == *\"test -d\"* ]]; then",
		"  echo present",
		"  exit 0",
		"fi",
		"if [[ \"$cmd\" == *\"ps --status running --services\"* ]]; then",
		"  echo simulated-unhealthy >&2",
		"  exit 1",
		"fi",
		"exit 0",
		"",
	}, "\n")
	if err := os.WriteFile(sshScript, []byte(sshContent), 0o700); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv(paasSSHBinEnvKey, sshScript)

	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-a", "--host", "203.0.113.10", "--user", "root"})
	})
	out := captureStdout(t, func() {
		cmdPaas([]string{"deploy", "reconcile", "--app", "billing-api", "--target", "edge-a", "--json"})
	})
	var payload struct {
		Command string                `json:"command"`
		Data    []paasReconcileResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode reconcile output: %v output=%q", err, out)
	}
	if payload.Command != "deploy reconcile" || len(payload.Data) != 1 {
		t.Fatalf("unexpected reconcile payload: %#v", payload)
	}
	row := payload.Data[0]
	if row.Status != "drifted" {
		t.Fatalf("expected drifted status, got %#v", row)
	}
	if len(row.Orphaned) == 0 || row.Orphaned[0] != "rel-old-orphan" {
		t.Fatalf("expected orphan detection, got %#v", row.Orphaned)
	}
}

func TestPaasDeployCreatesReleaseBundleMetadata(t *testing.T) {
	stateRoot := t.TempDir()
	composePath := filepath.Join(t.TempDir(), "compose.yaml")
	vaultFile := filepath.Join(t.TempDir(), ".env")
	trustStore := filepath.Join(t.TempDir(), "trust.json")
	t.Setenv(paasStateRootEnvKey, stateRoot)
	t.Setenv("SI_VAULT_FILE", vaultFile)
	t.Setenv("SI_VAULT_TRUST_STORE", trustStore)
	setupPaasMockSunVault(t, vaultFile, "token-paas-bundle")

	composeBody := strings.Join([]string{
		"services:",
		"  api:",
		"    image: nginx:latest",
		"    environment:",
		"      - API_TOKEN=${API_TOKEN}",
		"",
	}, "\n")
	if err := os.WriteFile(composePath, []byte(composeBody), 0o600); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	vaultBody := fmt.Sprintf("%s%s\nAPP_ENV=prod\n", vault.VaultRecipientPrefix, "age1examplerecipient000000000000000000000000000000000000000000000000")
	if err := os.WriteFile(vaultFile, []byte(vaultBody), 0o600); err != nil {
		t.Fatalf("write vault: %v", err)
	}

	out := captureStdout(t, func() {
		cmdPaas([]string{
			"deploy",
			"--app", "billing-api",
			"--target", "edge-a",
			"--compose-file", composePath,
			"--allow-untrusted-vault",
			"--json",
		})
	})
	env := parsePaasEnvelope(t, out)
	if env.Command != "deploy" {
		t.Fatalf("expected deploy command, got %#v", env)
	}
	bundleDir := strings.TrimSpace(env.Fields["bundle_dir"])
	metaPath := strings.TrimSpace(env.Fields["bundle_metadata"])
	if bundleDir == "" || metaPath == "" {
		t.Fatalf("expected bundle fields in deploy output, got %#v", env.Fields)
	}
	if _, err := os.Stat(filepath.Join(bundleDir, "compose.yaml")); err != nil {
		t.Fatalf("expected bundled compose file: %v", err)
	}
	rawMeta, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	var meta paasReleaseBundleMetadata
	if err := json.Unmarshal(rawMeta, &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if meta.App != "billing-api" {
		t.Fatalf("unexpected bundle app: %#v", meta)
	}
	if meta.ReleaseID != env.Fields["release"] {
		t.Fatalf("release mismatch between output and metadata: output=%q meta=%q", env.Fields["release"], meta.ReleaseID)
	}
	if strings.TrimSpace(meta.ComposeSHA256) == "" {
		t.Fatalf("expected compose digest in metadata: %#v", meta)
	}
}

func TestPaasDeployApplyUsesRemoteTransport(t *testing.T) {
	stateRoot := t.TempDir()
	composePath := filepath.Join(t.TempDir(), "compose.yaml")
	vaultFile := filepath.Join(t.TempDir(), ".env")
	trustStore := filepath.Join(t.TempDir(), "trust.json")
	fakeBinDir := t.TempDir()
	sshLog := filepath.Join(fakeBinDir, "ssh.log")
	scpLog := filepath.Join(fakeBinDir, "scp.log")
	t.Setenv(paasStateRootEnvKey, stateRoot)
	t.Setenv("SI_VAULT_FILE", vaultFile)
	t.Setenv("SI_VAULT_TRUST_STORE", trustStore)
	setupPaasMockSunVault(t, vaultFile, "token-paas-apply")

	composeBody := strings.Join([]string{
		"services:",
		"  api:",
		"    image: nginx:latest",
		"    environment:",
		"      - API_TOKEN=${API_TOKEN}",
		"",
	}, "\n")
	if err := os.WriteFile(composePath, []byte(composeBody), 0o600); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	vaultBody := fmt.Sprintf("%s%s\nAPP_ENV=prod\n", vault.VaultRecipientPrefix, "age1examplerecipient000000000000000000000000000000000000000000000000")
	if err := os.WriteFile(vaultFile, []byte(vaultBody), 0o600); err != nil {
		t.Fatalf("write vault: %v", err)
	}
	sshScript := filepath.Join(fakeBinDir, "fake-ssh")
	scpScript := filepath.Join(fakeBinDir, "fake-scp")
	sshContent := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> " + shellSingleQuote(sshLog) + "\n"
	scpContent := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> " + shellSingleQuote(scpLog) + "\n"
	if err := os.WriteFile(sshScript, []byte(sshContent), 0o700); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	if err := os.WriteFile(scpScript, []byte(scpContent), 0o700); err != nil {
		t.Fatalf("write fake scp: %v", err)
	}
	t.Setenv(paasSSHBinEnvKey, sshScript)
	t.Setenv(paasSCPBinEnvKey, scpScript)

	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-a", "--host", "203.0.113.10", "--user", "root"})
	})
	out := captureStdout(t, func() {
		cmdPaas([]string{
			"deploy",
			"--app", "billing-api",
			"--target", "edge-a",
			"--compose-file", composePath,
			"--apply",
			"--allow-untrusted-vault",
			"--json",
		})
	})
	env := parsePaasEnvelope(t, out)
	if env.Fields["apply"] != "true" {
		t.Fatalf("expected apply=true field, got %#v", env.Fields)
	}
	if env.Fields["applied_targets"] != "edge-a" {
		t.Fatalf("expected applied target edge-a, got %#v", env.Fields)
	}
	if env.Fields["target_statuses"] != "edge-a:ok" {
		t.Fatalf("expected deterministic target status summary, got %#v", env.Fields)
	}
	if env.Fields["fanout_plan"] != "serial(edge-a)" {
		t.Fatalf("expected serial fanout plan, got %#v", env.Fields)
	}
	eventPath := strings.TrimSpace(env.Fields["event_log"])
	if eventPath == "" {
		t.Fatalf("expected event log path in deploy fields, got %#v", env.Fields)
	}
	eventRaw, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatalf("read deploy event log: %v", err)
	}
	if !strings.Contains(string(eventRaw), "\"command\":\"deploy\"") {
		t.Fatalf("expected deploy command event entry, got %q", string(eventRaw))
	}
	sshRaw, err := os.ReadFile(sshLog)
	if err != nil {
		t.Fatalf("read ssh log: %v", err)
	}
	scpRaw, err := os.ReadFile(scpLog)
	if err != nil {
		t.Fatalf("read scp log: %v", err)
	}
	sshText := string(sshRaw)
	scpText := string(scpRaw)
	if !strings.Contains(sshText, "docker compose -f 'compose.yaml' up -d --remove-orphans") {
		t.Fatalf("expected compose apply command in ssh log, got %q", sshText)
	}
	if strings.Count(scpText, "root@203.0.113.10") < 2 {
		t.Fatalf("expected compose and metadata uploads in scp log, got %q", scpText)
	}
}

func TestPaasDeployResolvesMagicVariablesAndAddonComposeManifest(t *testing.T) {
	stateRoot := t.TempDir()
	composePath := filepath.Join(t.TempDir(), "compose.yaml")
	vaultFile := filepath.Join(t.TempDir(), ".env")
	trustStore := filepath.Join(t.TempDir(), "trust.json")
	t.Setenv(paasStateRootEnvKey, stateRoot)
	t.Setenv("SI_VAULT_FILE", vaultFile)
	t.Setenv("SI_VAULT_TRUST_STORE", trustStore)
	setupPaasMockSunVault(t, vaultFile, "token-paas-magic")

	composeBody := strings.Join([]string{
		"services:",
		"  api:",
		"    image: nginx:latest",
		"    labels:",
		"      - app={{paas.app}}",
		"      - context={{paas.context}}",
		"      - release=${SI_PAAS_RELEASE}",
		"",
	}, "\n")
	if err := os.WriteFile(composePath, []byte(composeBody), 0o600); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	vaultBody := fmt.Sprintf("%s%s\nAPP_ENV=prod\n", vault.VaultRecipientPrefix, "age1examplerecipient000000000000000000000000000000000000000000000000")
	if err := os.WriteFile(vaultFile, []byte(vaultBody), 0o600); err != nil {
		t.Fatalf("write vault: %v", err)
	}

	captureStdout(t, func() {
		cmdPaas([]string{
			"app", "addon", "enable",
			"--app", "billing-api",
			"--pack", "redis",
			"--name", "cache-main",
			"--json",
		})
	})

	raw := captureStdout(t, func() {
		cmdPaas([]string{
			"deploy",
			"--app", "billing-api",
			"--release", "rel-magic-001",
			"--compose-file", composePath,
			"--target", "edge-a",
			"--allow-untrusted-vault",
			"--json",
		})
	})
	env := parsePaasEnvelope(t, raw)
	if env.Command != "deploy" {
		t.Fatalf("expected deploy command output, got %#v", env)
	}
	if env.Fields["addon_fragments"] != "1" {
		t.Fatalf("expected one addon fragment in deploy output, got %#v", env.Fields)
	}
	if !strings.Contains(env.Fields["compose_files"], "compose.addon.cache-main.yaml") {
		t.Fatalf("expected addon compose file in deploy output, got %#v", env.Fields)
	}
	bundleDir := strings.TrimSpace(env.Fields["bundle_dir"])
	manifestPath := filepath.Join(bundleDir, paasComposeFilesManifestName)
	manifestRaw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read compose manifest: %v", err)
	}
	manifest := string(manifestRaw)
	if !strings.Contains(manifest, "compose.yaml") || !strings.Contains(manifest, "compose.addon.cache-main.yaml") {
		t.Fatalf("expected compose manifest entries, got %q", manifest)
	}
	resolvedComposeRaw, err := os.ReadFile(filepath.Join(bundleDir, "compose.yaml"))
	if err != nil {
		t.Fatalf("read resolved compose bundle: %v", err)
	}
	resolvedCompose := string(resolvedComposeRaw)
	if strings.Contains(resolvedCompose, "{{paas.") || strings.Contains(resolvedCompose, "${SI_PAAS_RELEASE}") {
		t.Fatalf("expected magic variables resolved in bundled compose, got %q", resolvedCompose)
	}
	if !strings.Contains(resolvedCompose, "app=billing-api") || !strings.Contains(resolvedCompose, "context="+defaultPaasContext) || !strings.Contains(resolvedCompose, "release=rel-magic-001") {
		t.Fatalf("expected resolved magic values in compose output, got %q", resolvedCompose)
	}
	addonRaw, err := os.ReadFile(filepath.Join(bundleDir, "compose.addon.cache-main.yaml"))
	if err != nil {
		t.Fatalf("read addon compose artifact: %v", err)
	}
	if !strings.Contains(string(addonRaw), "image: redis:7") {
		t.Fatalf("expected redis addon artifact, got %q", string(addonRaw))
	}
}

func TestPreparePaasComposeForDeployRejectsAddonMergeConflicts(t *testing.T) {
	stateRoot := t.TempDir()
	composePath := filepath.Join(t.TempDir(), "compose.yaml")
	t.Setenv(paasStateRootEnvKey, stateRoot)

	baseCompose := strings.Join([]string{
		"services:",
		"  redis-cache-main:",
		"    image: nginx:latest",
		"",
	}, "\n")
	if err := os.WriteFile(composePath, []byte(baseCompose), 0o600); err != nil {
		t.Fatalf("write base compose: %v", err)
	}
	captureStdout(t, func() {
		cmdPaas([]string{
			"app", "addon", "enable",
			"--app", "billing-api",
			"--pack", "redis",
			"--name", "cache-main",
			"--json",
		})
	})

	_, err := preparePaasComposeForDeploy(paasComposePrepareOptions{
		App:         "billing-api",
		ReleaseID:   "rel-conflict-001",
		ComposeFile: composePath,
		Strategy:    "serial",
		Targets:     []string{"edge-a"},
	})
	if err == nil {
		t.Fatalf("expected merge conflict validation error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "merge conflict") {
		t.Fatalf("expected merge conflict message, got %v", err)
	}
}

func TestPreparePaasComposeForDeployRejectsUnknownMagicVariable(t *testing.T) {
	stateRoot := t.TempDir()
	composePath := filepath.Join(t.TempDir(), "compose.yaml")
	t.Setenv(paasStateRootEnvKey, stateRoot)

	body := strings.Join([]string{
		"services:",
		"  api:",
		"    image: nginx:latest",
		"    labels:",
		"      - unknown={{paas.unknown}}",
		"",
	}, "\n")
	if err := os.WriteFile(composePath, []byte(body), 0o600); err != nil {
		t.Fatalf("write compose: %v", err)
	}

	_, err := preparePaasComposeForDeploy(paasComposePrepareOptions{
		App:         "billing-api",
		ReleaseID:   "rel-magic-invalid-1",
		ComposeFile: composePath,
		Strategy:    "serial",
		Targets:     []string{"edge-a"},
	})
	if err == nil {
		t.Fatalf("expected unresolved magic variable validation error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unresolved magic variable") {
		t.Fatalf("expected unresolved magic variable failure, got %v", err)
	}
}

func TestPaasDeployBlueGreenApplyUsesComposeOnlyCutoverPolicy(t *testing.T) {
	stateRoot := t.TempDir()
	composePath := filepath.Join(t.TempDir(), "compose.yaml")
	vaultFile := filepath.Join(t.TempDir(), ".env")
	trustStore := filepath.Join(t.TempDir(), "trust.json")
	fakeBinDir := t.TempDir()
	sshLog := filepath.Join(fakeBinDir, "ssh.log")
	scpLog := filepath.Join(fakeBinDir, "scp.log")
	t.Setenv(paasStateRootEnvKey, stateRoot)
	t.Setenv("SI_VAULT_FILE", vaultFile)
	t.Setenv("SI_VAULT_TRUST_STORE", trustStore)
	setupPaasMockSunVault(t, vaultFile, "token-paas-bluegreen")

	composeBody := strings.Join([]string{
		"services:",
		"  api:",
		"    image: nginx:latest",
		"    environment:",
		"      - API_TOKEN=${API_TOKEN}",
		"",
	}, "\n")
	if err := os.WriteFile(composePath, []byte(composeBody), 0o600); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	vaultBody := fmt.Sprintf("%s%s\nAPP_ENV=prod\n", vault.VaultRecipientPrefix, "age1examplerecipient000000000000000000000000000000000000000000000000")
	if err := os.WriteFile(vaultFile, []byte(vaultBody), 0o600); err != nil {
		t.Fatalf("write vault: %v", err)
	}

	sshScript := filepath.Join(fakeBinDir, "fake-ssh")
	scpScript := filepath.Join(fakeBinDir, "fake-scp")
	sshContent := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> " + shellSingleQuote(sshLog) + "\n"
	scpContent := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> " + shellSingleQuote(scpLog) + "\n"
	if err := os.WriteFile(sshScript, []byte(sshContent), 0o700); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	if err := os.WriteFile(scpScript, []byte(scpContent), 0o700); err != nil {
		t.Fatalf("write fake scp: %v", err)
	}
	t.Setenv(paasSSHBinEnvKey, sshScript)
	t.Setenv(paasSCPBinEnvKey, scpScript)

	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-a", "--host", "203.0.113.10", "--user", "root"})
	})

	firstRaw := captureStdout(t, func() {
		cmdPaas([]string{
			"deploy", "bluegreen",
			"--app", "billing-api",
			"--target", "edge-a",
			"--compose-file", composePath,
			"--apply",
			"--allow-untrusted-vault",
			"--json",
		})
	})
	firstEnv := parsePaasEnvelope(t, firstRaw)
	if firstEnv.Command != "deploy bluegreen" {
		t.Fatalf("expected deploy bluegreen command, got %#v", firstEnv)
	}
	if firstEnv.Fields["target_statuses"] != "edge-a:ok" {
		t.Fatalf("expected edge-a successful blue/green status, got %#v", firstEnv.Fields)
	}
	if firstEnv.Fields["active_slots"] != "edge-a:green" || firstEnv.Fields["previous_slots"] != "edge-a:blue" {
		t.Fatalf("expected slot transition blue->green, got %#v", firstEnv.Fields)
	}
	if firstEnv.Fields["rollback_policy"] == "" {
		t.Fatalf("expected rollback policy field, got %#v", firstEnv.Fields)
	}

	secondRaw := captureStdout(t, func() {
		cmdPaas([]string{
			"deploy", "bluegreen",
			"--app", "billing-api",
			"--target", "edge-a",
			"--compose-file", composePath,
			"--apply",
			"--allow-untrusted-vault",
			"--json",
		})
	})
	secondEnv := parsePaasEnvelope(t, secondRaw)
	if secondEnv.Fields["active_slots"] != "edge-a:blue" || secondEnv.Fields["previous_slots"] != "edge-a:green" {
		t.Fatalf("expected slot transition green->blue on second rollout, got %#v", secondEnv.Fields)
	}

	store, err := loadPaasBlueGreenPolicyStore()
	if err != nil {
		t.Fatalf("load bluegreen policy store: %v", err)
	}
	appPolicy := store.Apps[sanitizePaasReleasePathSegment("billing-api")]
	targetPolicy := appPolicy.Targets[sanitizePaasReleasePathSegment("edge-a")]
	if targetPolicy.ActiveSlot != "blue" {
		t.Fatalf("expected active slot blue after second rollout, got %#v", targetPolicy)
	}

	sshRaw, err := os.ReadFile(sshLog)
	if err != nil {
		t.Fatalf("read ssh log: %v", err)
	}
	scpRaw, err := os.ReadFile(scpLog)
	if err != nil {
		t.Fatalf("read scp log: %v", err)
	}
	sshText := string(sshRaw)
	scpText := string(scpRaw)
	if !strings.Contains(sshText, "docker compose -p 'billing-api-edge-a-green' -f 'compose.yaml' up -d --remove-orphans --build") {
		t.Fatalf("expected green project apply command in ssh log, got %q", sshText)
	}
	if !strings.Contains(sshText, "docker compose -p 'billing-api-edge-a-blue' -f 'compose.yaml' up -d --remove-orphans --build") {
		t.Fatalf("expected blue project apply command in ssh log, got %q", sshText)
	}
	if strings.Count(scpText, "root@203.0.113.10") < 4 {
		t.Fatalf("expected two deploy uploads (compose + metadata) per run in scp log, got %q", scpText)
	}
}

func TestRunPaasBlueGreenDeployOnTargetRollsBackOnPostCutoverHealthFailure(t *testing.T) {
	fakeBinDir := t.TempDir()
	sshLog := filepath.Join(fakeBinDir, "ssh.log")
	healthCountPath := filepath.Join(fakeBinDir, "health.count")
	sshScript := filepath.Join(fakeBinDir, "fake-ssh")
	scpScript := filepath.Join(fakeBinDir, "fake-scp")
	sshBody := strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"printf '%s\\n' \"$*\" >> " + shellSingleQuote(sshLog),
		"cmd=\"$*\"",
		"if [[ \"$cmd\" == *\"docker compose -p 'billing-api-edge-a-green' -f 'compose.yaml' ps --status running --services | grep -q .\"* ]]; then",
		"  count=0",
		"  if [[ -f " + shellSingleQuote(healthCountPath) + " ]]; then count=$(cat " + shellSingleQuote(healthCountPath) + "); fi",
		"  count=$((count+1))",
		"  printf '%s' \"$count\" > " + shellSingleQuote(healthCountPath),
		"  if [[ \"$count\" -ge 2 ]]; then",
		"    echo \"simulated post-cutover health failure\" >&2",
		"    exit 1",
		"  fi",
		"fi",
		"exit 0",
		"",
	}, "\n")
	if err := os.WriteFile(sshScript, []byte(sshBody), 0o700); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	if err := os.WriteFile(scpScript, []byte("#!/usr/bin/env bash\nset -euo pipefail\n"), 0o700); err != nil {
		t.Fatalf("write fake scp: %v", err)
	}
	t.Setenv(paasSSHBinEnvKey, sshScript)
	t.Setenv(paasSCPBinEnvKey, scpScript)

	bundleDir := seedPaasTestBundleDir(t)
	outcome := runPaasBlueGreenDeployOnTarget(paasBlueGreenDeployTargetOptions{
		Target:          paasTarget{Name: "edge-a", Host: "203.0.113.10", Port: 22, User: "root"},
		App:             "billing-api",
		ReleaseID:       "rel-bluegreen-1",
		BundleDir:       bundleDir,
		RemoteRoot:      "/opt/si/paas/releases",
		FromSlot:        "blue",
		ToSlot:          "green",
		PreviousRelease: "rel-bluegood-1",
		ApplyTimeout:    2 * time.Second,
		HealthTimeout:   2 * time.Second,
		CutoverTimeout:  2 * time.Second,
		HealthCommand:   "",
		CutoverCommand:  "",
		KeepStandby:     true,
	})
	if outcome.Err == nil {
		t.Fatalf("expected post-cutover health failure to return error")
	}
	if !outcome.RolledBack {
		t.Fatalf("expected rollback to previous slot on post-cutover failure, got %#v", outcome)
	}
	failure := asPaasOperationFailure(outcome.Err)
	if failure.Stage != "bluegreen_post_cutover_health" {
		t.Fatalf("expected bluegreen_post_cutover_health stage, got %#v", failure)
	}
	sshRaw, err := os.ReadFile(sshLog)
	if err != nil {
		t.Fatalf("read ssh log: %v", err)
	}
	sshText := string(sshRaw)
	if !strings.Contains(sshText, "printf '%s\\n' 'green' > '/opt/si/paas/releases/bluegreen/billing-api/edge-a/state/active_slot'") {
		t.Fatalf("expected cutover-to-green command in ssh log, got %q", sshText)
	}
	if !strings.Contains(sshText, "printf '%s\\n' 'blue' > '/opt/si/paas/releases/bluegreen/billing-api/edge-a/state/active_slot'") {
		t.Fatalf("expected rollback-to-blue command in ssh log, got %q", sshText)
	}
}

func TestApplyPaasReleaseToTargetsRollingPlanAndOrder(t *testing.T) {
	stateRoot := t.TempDir()
	fakeBinDir := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-a", "--host", "203.0.113.10", "--user", "root"})
	})
	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-b", "--host", "203.0.113.11", "--user", "root"})
	})
	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-c", "--host", "203.0.113.12", "--user", "root"})
	})

	sshScript := filepath.Join(fakeBinDir, "fake-ssh")
	scpScript := filepath.Join(fakeBinDir, "fake-scp")
	if err := os.WriteFile(sshScript, []byte("#!/usr/bin/env bash\nset -euo pipefail\n"), 0o700); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	if err := os.WriteFile(scpScript, []byte("#!/usr/bin/env bash\nset -euo pipefail\n"), 0o700); err != nil {
		t.Fatalf("write fake scp: %v", err)
	}
	t.Setenv(paasSSHBinEnvKey, sshScript)
	t.Setenv(paasSCPBinEnvKey, scpScript)

	bundleDir := seedPaasTestBundleDir(t)
	result, err := applyPaasReleaseToTargets(paasApplyOptions{
		Enabled:              true,
		SelectedTargets:      []string{"all"},
		Strategy:             "rolling",
		MaxParallel:          2,
		ContinueOnError:      false,
		BundleDir:            bundleDir,
		ReleaseID:            "rel-rolling",
		RemoteRoot:           "/opt/si/paas/releases",
		ApplyTimeout:         2 * time.Second,
		HealthTimeout:        2 * time.Second,
		HealthCommand:        "",
		RollbackOnFailure:    false,
		RollbackBundleDir:    "",
		RollbackReleaseID:    "",
		RollbackApplyTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("apply rolling strategy: %v", err)
	}
	if result.FanoutPlan != "rolling(edge-a+edge-b);rolling(edge-c)" {
		t.Fatalf("unexpected rolling fanout plan: %#v", result.FanoutPlan)
	}
	if formatPaasTargetStatuses(result.TargetStatuses) != "edge-a:ok,edge-b:ok,edge-c:ok" {
		t.Fatalf("unexpected rolling target statuses: %#v", result.TargetStatuses)
	}
}

func TestApplyPaasReleaseToTargetsCanaryStopsAfterCanaryFailure(t *testing.T) {
	stateRoot := t.TempDir()
	fakeBinDir := t.TempDir()
	sshLog := filepath.Join(fakeBinDir, "ssh.log")
	t.Setenv(paasStateRootEnvKey, stateRoot)

	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-a", "--host", "203.0.113.10", "--user", "root"})
	})
	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-b", "--host", "203.0.113.11", "--user", "root"})
	})
	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-c", "--host", "203.0.113.12", "--user", "root"})
	})

	sshScript := filepath.Join(fakeBinDir, "fake-ssh")
	scpScript := filepath.Join(fakeBinDir, "fake-scp")
	sshBody := strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"printf '%s\\n' \"$*\" >> " + shellSingleQuote(sshLog),
		"cmd=\"$*\"",
		"if [[ \"$cmd\" == *\"root@203.0.113.10\"* && \"$cmd\" == *\"up -d --remove-orphans\"* ]]; then",
		"  echo \"simulated canary apply failure\" >&2",
		"  exit 1",
		"fi",
		"exit 0",
		"",
	}, "\n")
	if err := os.WriteFile(sshScript, []byte(sshBody), 0o700); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	if err := os.WriteFile(scpScript, []byte("#!/usr/bin/env bash\nset -euo pipefail\n"), 0o700); err != nil {
		t.Fatalf("write fake scp: %v", err)
	}
	t.Setenv(paasSSHBinEnvKey, sshScript)
	t.Setenv(paasSCPBinEnvKey, scpScript)

	bundleDir := seedPaasTestBundleDir(t)
	result, err := applyPaasReleaseToTargets(paasApplyOptions{
		Enabled:              true,
		SelectedTargets:      []string{"all"},
		Strategy:             "canary",
		MaxParallel:          2,
		ContinueOnError:      true,
		BundleDir:            bundleDir,
		ReleaseID:            "rel-canary",
		RemoteRoot:           "/opt/si/paas/releases",
		ApplyTimeout:         2 * time.Second,
		HealthTimeout:        2 * time.Second,
		HealthCommand:        "",
		RollbackOnFailure:    false,
		RollbackBundleDir:    "",
		RollbackReleaseID:    "",
		RollbackApplyTimeout: 2 * time.Second,
	})
	if err == nil {
		t.Fatalf("expected canary failure")
	}
	if formatTargets(result.FailedTargets) != "edge-a" {
		t.Fatalf("expected canary failure target edge-a, got %#v", result.FailedTargets)
	}
	if formatTargets(result.SkippedTargets) != "edge-b,edge-c" {
		t.Fatalf("expected remaining targets skipped after canary failure, got %#v", result.SkippedTargets)
	}
	if formatPaasTargetStatuses(result.TargetStatuses) != "edge-a:failed,edge-b:skipped,edge-c:skipped" {
		t.Fatalf("unexpected canary status summary: %#v", result.TargetStatuses)
	}
	sshRaw, err := os.ReadFile(sshLog)
	if err != nil {
		t.Fatalf("read ssh log: %v", err)
	}
	sshText := string(sshRaw)
	if strings.Contains(sshText, "root@203.0.113.11") || strings.Contains(sshText, "root@203.0.113.12") {
		t.Fatalf("expected canary failure to block later targets, log=%q", sshText)
	}
}

func TestApplyPaasReleaseToTargetsParallelContinuesOnError(t *testing.T) {
	stateRoot := t.TempDir()
	fakeBinDir := t.TempDir()
	sshLog := filepath.Join(fakeBinDir, "ssh.log")
	t.Setenv(paasStateRootEnvKey, stateRoot)

	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-a", "--host", "203.0.113.10", "--user", "root"})
	})
	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-b", "--host", "203.0.113.11", "--user", "root"})
	})

	sshScript := filepath.Join(fakeBinDir, "fake-ssh")
	scpScript := filepath.Join(fakeBinDir, "fake-scp")
	sshBody := strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"printf '%s\\n' \"$*\" >> " + shellSingleQuote(sshLog),
		"cmd=\"$*\"",
		"if [[ \"$cmd\" == *\"root@203.0.113.11\"* && \"$cmd\" == *\"up -d --remove-orphans\"* ]]; then",
		"  echo \"simulated parallel apply failure\" >&2",
		"  exit 1",
		"fi",
		"exit 0",
		"",
	}, "\n")
	if err := os.WriteFile(sshScript, []byte(sshBody), 0o700); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	if err := os.WriteFile(scpScript, []byte("#!/usr/bin/env bash\nset -euo pipefail\n"), 0o700); err != nil {
		t.Fatalf("write fake scp: %v", err)
	}
	t.Setenv(paasSSHBinEnvKey, sshScript)
	t.Setenv(paasSCPBinEnvKey, scpScript)

	bundleDir := seedPaasTestBundleDir(t)
	result, err := applyPaasReleaseToTargets(paasApplyOptions{
		Enabled:              true,
		SelectedTargets:      []string{"all"},
		Strategy:             "parallel",
		MaxParallel:          2,
		ContinueOnError:      true,
		BundleDir:            bundleDir,
		ReleaseID:            "rel-parallel",
		RemoteRoot:           "/opt/si/paas/releases",
		ApplyTimeout:         2 * time.Second,
		HealthTimeout:        2 * time.Second,
		HealthCommand:        "",
		RollbackOnFailure:    false,
		RollbackBundleDir:    "",
		RollbackReleaseID:    "",
		RollbackApplyTimeout: 2 * time.Second,
	})
	if err == nil {
		t.Fatalf("expected aggregate error for partial parallel failure")
	}
	if result.FanoutPlan != "parallel(edge-a+edge-b)" {
		t.Fatalf("unexpected parallel fanout plan: %#v", result.FanoutPlan)
	}
	if formatTargets(result.FailedTargets) != "edge-b" {
		t.Fatalf("expected failed target edge-b, got %#v", result.FailedTargets)
	}
	if formatTargets(result.SkippedTargets) != "" {
		t.Fatalf("expected no skipped targets for parallel strategy, got %#v", result.SkippedTargets)
	}
	if formatPaasTargetStatuses(result.TargetStatuses) != "edge-a:ok,edge-b:failed" {
		t.Fatalf("unexpected parallel status summary: %#v", result.TargetStatuses)
	}
	sshRaw, err := os.ReadFile(sshLog)
	if err != nil {
		t.Fatalf("read ssh log: %v", err)
	}
	sshText := string(sshRaw)
	if !strings.Contains(sshText, "root@203.0.113.10") || !strings.Contains(sshText, "root@203.0.113.11") {
		t.Fatalf("expected parallel attempt on both targets, log=%q", sshText)
	}
}

func TestPaasDeployWebhookMapAndIngest(t *testing.T) {
	stateRoot := t.TempDir()
	payloadFile := filepath.Join(t.TempDir(), "payload.json")
	t.Setenv(paasStateRootEnvKey, stateRoot)

	addRaw := captureStdout(t, func() {
		cmdPaas([]string{
			"deploy", "webhook", "map", "add",
			"--provider", "github",
			"--repo", "https://github.com/acme/billing-api.git",
			"--branch", "main",
			"--app", "billing-api",
			"--targets", "all",
			"--strategy", "rolling",
			"--max-parallel", "2",
			"--json",
		})
	})
	addEnv := parsePaasEnvelope(t, addRaw)
	if addEnv.Command != "deploy webhook map add" {
		t.Fatalf("expected mapping add command envelope, got %#v", addEnv)
	}
	if addEnv.Fields["repo"] != "acme/billing-api" {
		t.Fatalf("expected normalized repo in mapping output, got %#v", addEnv.Fields)
	}

	listRaw := captureStdout(t, func() {
		cmdPaas([]string{"deploy", "webhook", "map", "list", "--json"})
	})
	var listPayload struct {
		Command string               `json:"command"`
		Count   int                  `json:"count"`
		Data    []paasWebhookMapping `json:"data"`
	}
	if err := json.Unmarshal([]byte(listRaw), &listPayload); err != nil {
		t.Fatalf("decode webhook map list payload: %v output=%q", err, listRaw)
	}
	if listPayload.Command != "deploy webhook map list" || listPayload.Count != 1 || len(listPayload.Data) != 1 {
		t.Fatalf("unexpected map list payload: %#v", listPayload)
	}
	if listPayload.Data[0].Branch != "main" || listPayload.Data[0].App != "billing-api" {
		t.Fatalf("unexpected mapping row: %#v", listPayload.Data[0])
	}

	payload := []byte("{\"ref\":\"refs/heads/main\",\"repository\":{\"full_name\":\"acme/billing-api\"}}\n")
	if err := os.WriteFile(payloadFile, payload, 0o600); err != nil {
		t.Fatalf("write webhook payload: %v", err)
	}
	secret := "webhook-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	ingestRaw := captureStdout(t, func() {
		cmdPaas([]string{
			"deploy", "webhook", "ingest",
			"--provider", "github",
			"--event", "push",
			"--payload-file", payloadFile,
			"--signature", signature,
			"--secret", secret,
			"--json",
		})
	})
	ingestEnv := parsePaasEnvelope(t, ingestRaw)
	if ingestEnv.Command != "deploy webhook ingest" {
		t.Fatalf("expected webhook ingest envelope, got %#v", ingestEnv)
	}
	if ingestEnv.Fields["mapped_app"] != "billing-api" {
		t.Fatalf("expected mapping app in ingest output, got %#v", ingestEnv.Fields)
	}
	if ingestEnv.Fields["repo"] != "acme/billing-api" || ingestEnv.Fields["branch"] != "main" {
		t.Fatalf("expected repo/branch from payload, got %#v", ingestEnv.Fields)
	}
	if !strings.Contains(ingestEnv.Fields["trigger_command"], "--strategy rolling") {
		t.Fatalf("expected mapped deploy strategy in trigger command, got %#v", ingestEnv.Fields)
	}

	captureStdout(t, func() {
		cmdPaas([]string{
			"deploy", "webhook", "map", "remove",
			"--provider", "github",
			"--repo", "acme/billing-api",
			"--branch", "main",
			"--json",
		})
	})
	afterRemoveRaw := captureStdout(t, func() {
		cmdPaas([]string{"deploy", "webhook", "map", "list", "--json"})
	})
	if err := json.Unmarshal([]byte(afterRemoveRaw), &listPayload); err != nil {
		t.Fatalf("decode webhook map list after remove: %v output=%q", err, afterRemoveRaw)
	}
	if listPayload.Count != 0 || len(listPayload.Data) != 0 {
		t.Fatalf("expected empty mapping list after remove, got %#v", listPayload)
	}
}

func TestVerifyPaasWebhookSignature(t *testing.T) {
	payload := []byte("{\"status\":\"ok\"}")
	secret := "abc123"
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if err := verifyPaasWebhookSignature(payload, secret, signature); err != nil {
		t.Fatalf("expected valid signature: %v", err)
	}
	if err := verifyPaasWebhookSignature(payload, secret, "sha256=0000"); err == nil {
		t.Fatalf("expected signature mismatch failure")
	}
}

func TestPaasAlertIngressTLSRecordsRetryAlert(t *testing.T) {
	stateRoot := t.TempDir()
	artifactsDir := t.TempDir()
	fakeBinDir := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-a", "--host", "203.0.113.10", "--user", "root"})
	})
	captureStdout(t, func() {
		cmdPaas([]string{
			"target", "ingress-baseline",
			"--target", "edge-a",
			"--domain", "apps.example.com",
			"--acme-email", "ops@example.com",
			"--output-dir", artifactsDir,
		})
	})

	sshScript := filepath.Join(fakeBinDir, "fake-ssh")
	sshBody := strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"cmd=\"$*\"",
		"if [[ \"$cmd\" == *\"docker ps --format\"* ]]; then",
		"  echo si-traefik-edge-a",
		"  exit 0",
		"fi",
		"if [[ \"$cmd\" == *\"test -s /var/lib/traefik/acme.json\"* ]]; then",
		"  echo ready",
		"  exit 0",
		"fi",
		"if [[ \"$cmd\" == *\"docker logs --tail\"* ]]; then",
		"  echo \"acme challenge failed for domain; retrying in 30s\"",
		"  exit 0",
		"fi",
		"exit 0",
		"",
	}, "\n")
	if err := os.WriteFile(sshScript, []byte(sshBody), 0o700); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv(paasSSHBinEnvKey, sshScript)

	alertRaw := captureStdout(t, func() {
		cmdPaas([]string{"alert", "ingress-tls", "--target", "edge-a", "--json"})
	})
	var alertPayload struct {
		Command string                 `json:"command"`
		Count   int                    `json:"count"`
		Data    []paasIngressTLSResult `json:"data"`
		Fields  map[string]string      `json:"fields"`
	}
	if err := json.Unmarshal([]byte(alertRaw), &alertPayload); err != nil {
		t.Fatalf("decode ingress tls alert payload: %v output=%q", err, alertRaw)
	}
	if alertPayload.Command != "alert ingress-tls" || alertPayload.Count != 1 || len(alertPayload.Data) != 1 {
		t.Fatalf("unexpected ingress tls alert payload: %#v", alertPayload)
	}
	if alertPayload.Data[0].Status != "retrying" || alertPayload.Data[0].Severity != "warning" {
		t.Fatalf("expected retrying warning status, got %#v", alertPayload.Data[0])
	}
	if alertPayload.Fields["alerts_emitted"] != "1" {
		t.Fatalf("expected one alert emitted, got %#v", alertPayload.Fields)
	}

	historyRaw := captureStdout(t, func() {
		cmdPaas([]string{"alert", "history", "--json"})
	})
	var historyPayload struct {
		Command string           `json:"command"`
		Count   int              `json:"count"`
		Data    []paasAlertEntry `json:"data"`
	}
	if err := json.Unmarshal([]byte(historyRaw), &historyPayload); err != nil {
		t.Fatalf("decode alert history payload: %v output=%q", err, historyRaw)
	}
	if historyPayload.Command != "alert history" || historyPayload.Count < 1 || len(historyPayload.Data) < 1 {
		t.Fatalf("expected alert history rows, got %#v", historyPayload)
	}
	found := false
	for _, row := range historyPayload.Data {
		if row.Command == "alert ingress-tls" && row.Target == "edge-a" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected ingress-tls alert record in history, got %#v", historyPayload.Data)
	}
}

func TestPaasAlertSetupTelegramPersistsConfig(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	raw := captureStdout(t, func() {
		cmdPaas([]string{
			"alert", "setup-telegram",
			"--bot-token", "bot-token-123",
			"--chat-id", "987654",
			"--json",
		})
	})
	env := parsePaasEnvelope(t, raw)
	if env.Command != "alert setup-telegram" {
		t.Fatalf("expected setup-telegram command envelope, got %#v", env)
	}
	if env.Fields["configured"] != "true" {
		t.Fatalf("expected configured=true, got %#v", env.Fields)
	}
	config, path, err := loadPaasTelegramConfig(currentPaasContext())
	if err != nil {
		t.Fatalf("load telegram config: %v", err)
	}
	if path == "" || config.ChatID != "987654" || config.BotToken != "bot-token-123" {
		t.Fatalf("unexpected persisted telegram config: path=%q config=%#v", path, config)
	}
}

func TestPaasAlertTestSendsTelegramMessage(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method != http.MethodPost {
			t.Errorf("expected POST request, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/bottest-token/sendMessage") {
			t.Errorf("unexpected telegram API path: %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if got := r.Form.Get("chat_id"); got != "12345" {
			t.Errorf("expected chat_id=12345, got %q", got)
		}
		if got := r.Form.Get("text"); !strings.Contains(got, "ws06-02 test alert") {
			t.Errorf("expected telegram text to include alert message, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	t.Setenv(paasTelegramAPIBaseEnvKey, server.URL)

	captureStdout(t, func() {
		cmdPaas([]string{
			"alert", "setup-telegram",
			"--bot-token", "test-token",
			"--chat-id", "12345",
			"--json",
		})
	})

	raw := captureStdout(t, func() {
		cmdPaas([]string{
			"alert", "test",
			"--severity", "critical",
			"--message", "ws06-02 test alert",
			"--json",
		})
	})
	env := parsePaasEnvelope(t, raw)
	if env.Command != "alert test" {
		t.Fatalf("expected alert test envelope, got %#v", env)
	}
	if env.Fields["status"] != "sent" || env.Fields["channel"] != "telegram" {
		t.Fatalf("expected sent telegram alert fields, got %#v", env.Fields)
	}
	if callCount != 1 {
		t.Fatalf("expected one telegram API call, got %d", callCount)
	}
}

func TestPaasAlertPolicySetAndSuppressedRouting(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	setRaw := captureStdout(t, func() {
		cmdPaas([]string{
			"alert", "policy", "set",
			"--critical", "disabled",
			"--warning", "telegram",
			"--info", "telegram",
			"--json",
		})
	})
	setEnv := parsePaasEnvelope(t, setRaw)
	if setEnv.Command != "alert policy set" {
		t.Fatalf("expected alert policy set envelope, got %#v", setEnv)
	}
	if setEnv.Fields["critical"] != "disabled" {
		t.Fatalf("expected critical route disabled, got %#v", setEnv.Fields)
	}

	showRaw := captureStdout(t, func() {
		cmdPaas([]string{"alert", "policy", "show", "--json"})
	})
	var showPayload struct {
		Command string                 `json:"command"`
		Data    paasAlertRoutingPolicy `json:"data"`
	}
	if err := json.Unmarshal([]byte(showRaw), &showPayload); err != nil {
		t.Fatalf("decode alert policy show payload: %v output=%q", err, showRaw)
	}
	if showPayload.Command != "alert policy show" || showPayload.Data.Severity["critical"] != "disabled" {
		t.Fatalf("unexpected alert policy show payload: %#v", showPayload)
	}

	alertRaw := captureStdout(t, func() {
		cmdPaas([]string{
			"alert", "test",
			"--severity", "critical",
			"--message", "suppressed by policy",
			"--json",
		})
	})
	alertEnv := parsePaasEnvelope(t, alertRaw)
	if alertEnv.Fields["status"] != "suppressed" || alertEnv.Fields["channel"] != "disabled" {
		t.Fatalf("expected suppressed status for disabled route, got %#v", alertEnv.Fields)
	}
}

func TestEmitPaasOperationalAlertSuppressedByPolicy(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	if _, err := savePaasAlertRoutingPolicy(currentPaasContext(), paasAlertRoutingPolicy{
		DefaultChannel: "telegram",
		Severity: map[string]string{
			"critical": "disabled",
		},
	}); err != nil {
		t.Fatalf("save alert policy: %v", err)
	}
	historyPath := emitPaasOperationalAlert(
		"deploy failure",
		"critical",
		"edge-a",
		"health check failed",
		"inspect remote service logs",
		nil,
	)
	if strings.TrimSpace(historyPath) == "" {
		t.Fatalf("expected alert history path")
	}
	rows, _, err := loadPaasAlertHistory(10, "critical")
	if err != nil {
		t.Fatalf("load alert history: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("expected suppressed alert history row")
	}
	last := rows[len(rows)-1]
	if last.Command != "deploy failure" || last.Status != "suppressed" || last.Fields["channel"] != "disabled" {
		t.Fatalf("unexpected suppressed alert row: %#v", last)
	}
}

func TestEmitPaasOperationalAlertSendsTelegram(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	t.Setenv(paasTelegramAPIBaseEnvKey, server.URL)
	if _, err := savePaasTelegramConfig(currentPaasContext(), paasTelegramNotifierConfig{
		BotToken: "token-send",
		ChatID:   "111",
	}); err != nil {
		t.Fatalf("save telegram config: %v", err)
	}

	historyPath := emitPaasOperationalAlert(
		"deploy reconcile",
		"warning",
		"edge-b",
		"runtime unhealthy",
		"re-run deploy reconcile",
		nil,
	)
	if strings.TrimSpace(historyPath) == "" {
		t.Fatalf("expected alert history path")
	}
	if callCount != 1 {
		t.Fatalf("expected one telegram send call, got %d", callCount)
	}
	rows, _, err := loadPaasAlertHistory(10, "warning")
	if err != nil {
		t.Fatalf("load alert history: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("expected warning alert history row")
	}
	last := rows[len(rows)-1]
	if last.Status != "sent" || last.Fields["channel"] != "telegram" {
		t.Fatalf("unexpected sent alert row: %#v", last)
	}
	if strings.TrimSpace(last.Fields["callback_acknowledge"]) == "" {
		t.Fatalf("expected callback acknowledge hint in alert fields, got %#v", last.Fields)
	}
}

func TestPaasAlertAcknowledgeRecordsHistory(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	raw := captureStdout(t, func() {
		cmdPaas([]string{
			"alert", "acknowledge",
			"--target", "edge-a",
			"--command", "deploy failure",
			"--note", "acked by operator",
			"--json",
		})
	})
	env := parsePaasEnvelope(t, raw)
	if env.Command != "alert acknowledge" {
		t.Fatalf("expected alert acknowledge envelope, got %#v", env)
	}
	if env.Fields["target"] != "edge-a" || env.Fields["note"] != "acked by operator" {
		t.Fatalf("unexpected acknowledge fields: %#v", env.Fields)
	}
	historyRaw := captureStdout(t, func() {
		cmdPaas([]string{"alert", "history", "--json"})
	})
	var payload struct {
		Data []paasAlertEntry `json:"data"`
	}
	if err := json.Unmarshal([]byte(historyRaw), &payload); err != nil {
		t.Fatalf("decode alert history payload: %v output=%q", err, historyRaw)
	}
	found := false
	for _, row := range payload.Data {
		if row.Command == "alert acknowledge" && row.Status == "acknowledged" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected acknowledged history entry, got %#v", payload.Data)
	}
}

func TestPaasLogsLiveJSONContract(t *testing.T) {
	stateRoot := t.TempDir()
	fakeBinDir := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-a", "--host", "203.0.113.50", "--user", "root"})
	})
	if err := recordPaasSuccessfulRelease("billing-api", "rel-20260217T020304"); err != nil {
		t.Fatalf("record release history: %v", err)
	}

	sshScript := filepath.Join(fakeBinDir, "fake-ssh")
	sshBody := strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"echo 'api log line 1'",
		"echo 'api log line 2'",
		"",
	}, "\n")
	if err := os.WriteFile(sshScript, []byte(sshBody), 0o700); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv(paasSSHBinEnvKey, sshScript)

	raw := captureStdout(t, func() {
		cmdPaas([]string{
			"logs",
			"--app", "billing-api",
			"--target", "edge-a",
			"--service", "api",
			"--tail", "20",
			"--since", "15m",
			"--json",
		})
	})
	var payload struct {
		OK      bool            `json:"ok"`
		Command string          `json:"command"`
		Mode    string          `json:"mode"`
		Count   int             `json:"count"`
		Data    []paasLogResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode logs payload: %v output=%q", err, raw)
	}
	if !payload.OK || payload.Command != "logs" || payload.Mode != "live" {
		t.Fatalf("unexpected logs payload envelope: %#v", payload)
	}
	if payload.Count != 1 || len(payload.Data) != 1 {
		t.Fatalf("expected exactly one log row: %#v", payload)
	}
	row := payload.Data[0]
	if row.Target != "edge-a" || row.Status != "ok" {
		t.Fatalf("unexpected log row target/status: %#v", row)
	}
	if row.Release != "rel-20260217T020304" {
		t.Fatalf("expected resolved release in log row, got %#v", row)
	}
	if !strings.Contains(row.Command, "docker") || !strings.Contains(row.Command, "compose") || !strings.Contains(row.Command, "logs") {
		t.Fatalf("expected compose logs command in row, got %#v", row)
	}
	if row.LineCount != 2 || !strings.Contains(row.Output, "api log line 1") {
		t.Fatalf("expected collected logs output, got %#v", row)
	}
}

func TestPaasLogsLiveUsesRecordedRemoteDir(t *testing.T) {
	stateRoot := t.TempDir()
	fakeBinDir := t.TempDir()
	sshLog := filepath.Join(fakeBinDir, "ssh.log")
	t.Setenv(paasStateRootEnvKey, stateRoot)

	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-a", "--host", "203.0.113.51", "--user", "root"})
	})
	customRemoteDir := "/srv/si/releases"
	releaseID := "rel-20260219T010203"
	if err := recordPaasSuccessfulReleaseWithRemoteDir("billing-api", releaseID, customRemoteDir); err != nil {
		t.Fatalf("record release history with remote dir: %v", err)
	}

	sshScript := filepath.Join(fakeBinDir, "fake-ssh")
	sshBody := strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"echo \"$*\" >> " + quoteSingle(sshLog),
		"echo 'api log line 1'",
		"",
	}, "\n")
	if err := os.WriteFile(sshScript, []byte(sshBody), 0o700); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv(paasSSHBinEnvKey, sshScript)

	captureStdout(t, func() {
		cmdPaas([]string{
			"logs",
			"--app", "billing-api",
			"--target", "edge-a",
			"--service", "api",
			"--tail", "10",
			"--json",
		})
	})
	sshRaw, err := os.ReadFile(sshLog)
	if err != nil {
		t.Fatalf("read ssh log: %v", err)
	}
	expectedReleaseDir := path.Join(customRemoteDir, releaseID)
	if !strings.Contains(string(sshRaw), expectedReleaseDir) {
		t.Fatalf("expected logs command to use recorded remote dir %q, got %q", expectedReleaseDir, string(sshRaw))
	}
}

func TestPaasLogsLiveRemoteDirOverride(t *testing.T) {
	stateRoot := t.TempDir()
	fakeBinDir := t.TempDir()
	sshLog := filepath.Join(fakeBinDir, "ssh.log")
	t.Setenv(paasStateRootEnvKey, stateRoot)

	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-a", "--host", "203.0.113.52", "--user", "root"})
	})
	recordedRemoteDir := "/srv/si/releases-recorded"
	overrideRemoteDir := "/srv/si/releases-override"
	releaseID := "rel-20260219T040506"
	if err := recordPaasSuccessfulReleaseWithRemoteDir("billing-api", releaseID, recordedRemoteDir); err != nil {
		t.Fatalf("record release history with remote dir: %v", err)
	}

	sshScript := filepath.Join(fakeBinDir, "fake-ssh")
	sshBody := strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"echo \"$*\" >> " + quoteSingle(sshLog),
		"echo 'api log line override'",
		"",
	}, "\n")
	if err := os.WriteFile(sshScript, []byte(sshBody), 0o700); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv(paasSSHBinEnvKey, sshScript)

	captureStdout(t, func() {
		cmdPaas([]string{
			"logs",
			"--app", "billing-api",
			"--target", "edge-a",
			"--service", "api",
			"--tail", "10",
			"--remote-dir", overrideRemoteDir,
			"--json",
		})
	})
	sshRaw, err := os.ReadFile(sshLog)
	if err != nil {
		t.Fatalf("read ssh log: %v", err)
	}
	text := string(sshRaw)
	overrideReleaseDir := path.Join(overrideRemoteDir, releaseID)
	recordedReleaseDir := path.Join(recordedRemoteDir, releaseID)
	if !strings.Contains(text, overrideReleaseDir) {
		t.Fatalf("expected logs command to use override remote dir %q, got %q", overrideReleaseDir, text)
	}
	if strings.Contains(text, recordedReleaseDir) {
		t.Fatalf("expected logs command to ignore recorded remote dir %q when override is provided, got %q", recordedReleaseDir, text)
	}
}

func TestPaasBackupRunJSONContract(t *testing.T) {
	stateRoot := t.TempDir()
	fakeBinDir := t.TempDir()
	sshLog := filepath.Join(fakeBinDir, "ssh.log")
	t.Setenv(paasStateRootEnvKey, stateRoot)

	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-a", "--host", "203.0.113.60", "--user", "root"})
	})
	if err := recordPaasSuccessfulRelease("billing-api", "rel-20260218T010203"); err != nil {
		t.Fatalf("record release history: %v", err)
	}

	sshScript := filepath.Join(fakeBinDir, "fake-ssh")
	sshBody := strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"echo \"$*\" >> " + quoteSingle(sshLog),
		"echo 'wal-g backup completed'",
		"",
	}, "\n")
	if err := os.WriteFile(sshScript, []byte(sshBody), 0o700); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv(paasSSHBinEnvKey, sshScript)

	raw := captureStdout(t, func() {
		cmdPaas([]string{
			"backup", "run",
			"--app", "billing-api",
			"--target", "edge-a",
			"--service", "supabase-walg-backup",
			"--json",
		})
	})
	var payload struct {
		OK      bool               `json:"ok"`
		Command string             `json:"command"`
		Count   int                `json:"count"`
		Data    []paasBackupResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode backup run payload: %v output=%q", err, raw)
	}
	if !payload.OK || payload.Command != "backup run" {
		t.Fatalf("unexpected backup payload envelope: %#v", payload)
	}
	if payload.Count != 1 || len(payload.Data) != 1 {
		t.Fatalf("expected single backup row, got %#v", payload)
	}
	row := payload.Data[0]
	if row.Status != "ok" || row.Release != "rel-20260218T010203" || row.Service != "supabase-walg-backup" {
		t.Fatalf("unexpected backup row fields: %#v", row)
	}
	if !strings.Contains(row.Command, "docker compose -f compose.yaml") || !strings.Contains(row.Command, "run --rm") {
		t.Fatalf("expected compose run command in backup row, got %#v", row)
	}
	if !strings.Contains(row.Output, "wal-g backup completed") {
		t.Fatalf("expected backup output capture, got %#v", row)
	}

	sshRaw, err := os.ReadFile(sshLog)
	if err != nil {
		t.Fatalf("read ssh log: %v", err)
	}
	sshText := string(sshRaw)
	if !strings.Contains(sshText, "compose.addon.*.yaml") {
		t.Fatalf("expected addon compose glob in remote command, got %q", sshText)
	}
}

func TestPaasEventsListMergesDeployAndAlertSources(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	deployFields := map[string]string{
		"app":    "billing-api",
		"target": "edge-a",
	}
	if eventPath := recordPaasDeployEvent("deploy", "failed", deployFields, fmt.Errorf("deploy timeout")); strings.TrimSpace(eventPath) == "" {
		t.Fatalf("expected deploy event to be recorded")
	}
	if alertPath := recordPaasAlertEntry(paasAlertEntry{
		Command:  "alert test",
		Severity: "warning",
		Status:   "sent",
		Target:   "edge-a",
		Message:  "high restart rate",
	}); strings.TrimSpace(alertPath) == "" {
		t.Fatalf("expected alert event to be recorded")
	}

	raw := captureStdout(t, func() {
		cmdPaas([]string{"events", "list", "--limit", "10", "--json"})
	})
	var payload struct {
		OK      bool              `json:"ok"`
		Command string            `json:"command"`
		Mode    string            `json:"mode"`
		Count   int               `json:"count"`
		Data    []paasEventRecord `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode events list payload: %v output=%q", err, raw)
	}
	if !payload.OK || payload.Command != "events list" || payload.Mode != "live" {
		t.Fatalf("unexpected events list payload envelope: %#v", payload)
	}
	if payload.Count < 2 || len(payload.Data) < 2 {
		t.Fatalf("expected merged deploy+alert rows, got %#v", payload)
	}
	foundDeployFailure := false
	foundAlert := false
	for _, row := range payload.Data {
		if row.Command == "deploy" && row.Status == "failed" && row.Severity == "critical" {
			foundDeployFailure = true
		}
		if row.Command == "alert test" && row.Status == "sent" && row.Severity == "warning" {
			foundAlert = true
		}
	}
	if !foundDeployFailure || !foundAlert {
		t.Fatalf("expected both deploy failure and alert rows, got %#v", payload.Data)
	}

	criticalRaw := captureStdout(t, func() {
		cmdPaas([]string{"events", "list", "--severity", "critical", "--limit", "10", "--json"})
	})
	var criticalPayload struct {
		Count int               `json:"count"`
		Data  []paasEventRecord `json:"data"`
	}
	if err := json.Unmarshal([]byte(criticalRaw), &criticalPayload); err != nil {
		t.Fatalf("decode critical events payload: %v output=%q", err, criticalRaw)
	}
	if criticalPayload.Count < 1 || len(criticalPayload.Data) < 1 {
		t.Fatalf("expected at least one critical row, got %#v", criticalPayload)
	}
	for _, row := range criticalPayload.Data {
		if row.Severity != "critical" {
			t.Fatalf("expected critical-only rows, got %#v", criticalPayload.Data)
		}
	}
}

func TestPaasAuditEventsRecordedAndQueryable(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	captureStdout(t, func() {
		cmdPaas([]string{"app", "list", "--json"})
	})
	if path := recordPaasAuditEvent("deploy", "failed", "live", map[string]string{"app": "billing-api"}, fmt.Errorf("boom")); strings.TrimSpace(path) == "" {
		t.Fatalf("expected audit log path")
	}
	rows, _, err := loadPaasEventRecords(20, "", "")
	if err != nil {
		t.Fatalf("load event records: %v", err)
	}
	foundScaffoldAudit := false
	foundFailureAudit := false
	for _, row := range rows {
		if row.Source != "audit" {
			continue
		}
		if row.Command == "app list" && row.Status == "succeeded" {
			foundScaffoldAudit = true
		}
		if row.Command == "deploy" && row.Status == "failed" && row.Severity == "critical" {
			foundFailureAudit = true
		}
	}
	if !foundScaffoldAudit || !foundFailureAudit {
		t.Fatalf("expected scaffold and failure audit rows, got %#v", rows)
	}
}

func TestPaasRegressionUpgradeDeployRollbackPath(t *testing.T) {
	stateRoot := t.TempDir()
	composePath := filepath.Join(t.TempDir(), "compose.yaml")
	vaultFile := filepath.Join(t.TempDir(), ".env")
	trustStore := filepath.Join(t.TempDir(), "trust.json")
	fakeBinDir := t.TempDir()
	sshLog := filepath.Join(fakeBinDir, "ssh.log")
	scpLog := filepath.Join(fakeBinDir, "scp.log")
	t.Setenv(paasStateRootEnvKey, stateRoot)
	t.Setenv("SI_VAULT_FILE", vaultFile)
	t.Setenv("SI_VAULT_TRUST_STORE", trustStore)
	setupPaasMockSunVault(t, vaultFile, "token-paas-regression")

	composeBody := strings.Join([]string{
		"services:",
		"  api:",
		"    image: nginx:latest",
		"    environment:",
		"      - API_TOKEN=${API_TOKEN}",
		"",
	}, "\n")
	if err := os.WriteFile(composePath, []byte(composeBody), 0o600); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	vaultBody := fmt.Sprintf("%s%s\nAPP_ENV=prod\n", vault.VaultRecipientPrefix, "age1examplerecipient000000000000000000000000000000000000000000000000")
	if err := os.WriteFile(vaultFile, []byte(vaultBody), 0o600); err != nil {
		t.Fatalf("write vault: %v", err)
	}
	sshScript := filepath.Join(fakeBinDir, "fake-ssh")
	scpScript := filepath.Join(fakeBinDir, "fake-scp")
	sshContent := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> " + shellSingleQuote(sshLog) + "\n"
	scpContent := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$*\" >> " + shellSingleQuote(scpLog) + "\n"
	if err := os.WriteFile(sshScript, []byte(sshContent), 0o700); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	if err := os.WriteFile(scpScript, []byte(scpContent), 0o700); err != nil {
		t.Fatalf("write fake scp: %v", err)
	}
	t.Setenv(paasSSHBinEnvKey, sshScript)
	t.Setenv(paasSCPBinEnvKey, scpScript)

	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-a", "--host", "203.0.113.10", "--user", "root"})
	})

	deploy1Raw := captureStdout(t, func() {
		cmdPaas([]string{
			"deploy",
			"--app", "billing-api",
			"--target", "edge-a",
			"--compose-file", composePath,
			"--apply",
			"--allow-untrusted-vault",
			"--json",
		})
	})
	deploy1 := parsePaasEnvelope(t, deploy1Raw)
	release1 := strings.TrimSpace(deploy1.Fields["release"])
	if release1 == "" {
		t.Fatalf("expected first release id: %#v", deploy1.Fields)
	}

	time.Sleep(3 * time.Millisecond)
	deploy2Raw := captureStdout(t, func() {
		cmdPaas([]string{
			"deploy",
			"--app", "billing-api",
			"--target", "edge-a",
			"--compose-file", composePath,
			"--apply",
			"--allow-untrusted-vault",
			"--json",
		})
	})
	deploy2 := parsePaasEnvelope(t, deploy2Raw)
	release2 := strings.TrimSpace(deploy2.Fields["release"])
	if release2 == "" || release2 == release1 {
		t.Fatalf("expected second distinct release id: first=%q second=%q", release1, release2)
	}

	rollbackRaw := captureStdout(t, func() {
		cmdPaas([]string{
			"rollback",
			"--app", "billing-api",
			"--target", "edge-a",
			"--to-release", release1,
			"--apply",
			"--allow-untrusted-vault",
			"--json",
		})
	})
	rollbackEnv := parsePaasEnvelope(t, rollbackRaw)
	if rollbackEnv.Command != "rollback" {
		t.Fatalf("expected rollback command envelope, got %#v", rollbackEnv)
	}
	if rollbackEnv.Fields["to_release"] != release1 {
		t.Fatalf("expected rollback to first release %q, got %#v", release1, rollbackEnv.Fields)
	}
	if rollbackEnv.Fields["applied_targets"] != "edge-a" {
		t.Fatalf("expected rollback apply on edge-a, got %#v", rollbackEnv.Fields)
	}
	if rollbackEnv.Fields["health_checked_targets"] != "edge-a" {
		t.Fatalf("expected rollback health check target edge-a, got %#v", rollbackEnv.Fields)
	}
}

func TestPaasRegressionStateIsolationContextBoundaries(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	captureStdout(t, func() {
		cmdPaas([]string{"--context", "alpha", "target", "add", "--name", "edge-shared", "--host", "10.0.0.1", "--user", "root"})
	})
	captureStdout(t, func() {
		cmdPaas([]string{"--context", "beta", "target", "add", "--name", "edge-shared", "--host", "10.0.0.2", "--user", "root"})
	})

	alphaTargetsRaw := captureStdout(t, func() {
		cmdPaas([]string{"--context", "alpha", "target", "list", "--json"})
	})
	betaTargetsRaw := captureStdout(t, func() {
		cmdPaas([]string{"--context", "beta", "target", "list", "--json"})
	})
	alphaTargets := parseTargetListPayload(t, alphaTargetsRaw)
	betaTargets := parseTargetListPayload(t, betaTargetsRaw)
	if len(alphaTargets.Data) != 1 || len(betaTargets.Data) != 1 {
		t.Fatalf("expected one target per context: alpha=%#v beta=%#v", alphaTargets.Data, betaTargets.Data)
	}
	if alphaTargets.Data[0].Host != "10.0.0.1" {
		t.Fatalf("expected alpha target host 10.0.0.1, got %#v", alphaTargets.Data[0])
	}
	if betaTargets.Data[0].Host != "10.0.0.2" {
		t.Fatalf("expected beta target host 10.0.0.2, got %#v", betaTargets.Data[0])
	}

	captureStdout(t, func() {
		cmdPaas([]string{
			"--context", "alpha",
			"app", "addon", "enable",
			"--app", "billing-api",
			"--pack", "redis",
			"--name", "cache-alpha",
			"--json",
		})
	})
	alphaAddonRaw := captureStdout(t, func() {
		cmdPaas([]string{"--context", "alpha", "app", "addon", "list", "--app", "billing-api", "--json"})
	})
	betaAddonRaw := captureStdout(t, func() {
		cmdPaas([]string{"--context", "beta", "app", "addon", "list", "--app", "billing-api", "--json"})
	})
	var alphaAddons struct {
		Count int               `json:"count"`
		Data  []paasAddonRecord `json:"data"`
	}
	var betaAddons struct {
		Count int               `json:"count"`
		Data  []paasAddonRecord `json:"data"`
	}
	if err := json.Unmarshal([]byte(alphaAddonRaw), &alphaAddons); err != nil {
		t.Fatalf("decode alpha addon list: %v output=%q", err, alphaAddonRaw)
	}
	if err := json.Unmarshal([]byte(betaAddonRaw), &betaAddons); err != nil {
		t.Fatalf("decode beta addon list: %v output=%q", err, betaAddonRaw)
	}
	if alphaAddons.Count != 1 || len(alphaAddons.Data) != 1 {
		t.Fatalf("expected one addon in alpha context, got %#v", alphaAddons)
	}
	if betaAddons.Count != 0 || len(betaAddons.Data) != 0 {
		t.Fatalf("expected no addons in beta context, got %#v", betaAddons)
	}

	captureStdout(t, func() {
		cmdPaas([]string{"--context", "alpha", "app", "list", "--json"})
	})
	alphaEventsRaw := captureStdout(t, func() {
		cmdPaas([]string{"--context", "alpha", "events", "list", "--limit", "20", "--json"})
	})
	betaEventsRaw := captureStdout(t, func() {
		cmdPaas([]string{"--context", "beta", "events", "list", "--limit", "20", "--json"})
	})
	var alphaEvents struct {
		Count int               `json:"count"`
		Data  []paasEventRecord `json:"data"`
	}
	var betaEvents struct {
		Count int               `json:"count"`
		Data  []paasEventRecord `json:"data"`
	}
	if err := json.Unmarshal([]byte(alphaEventsRaw), &alphaEvents); err != nil {
		t.Fatalf("decode alpha events list: %v output=%q", err, alphaEventsRaw)
	}
	if err := json.Unmarshal([]byte(betaEventsRaw), &betaEvents); err != nil {
		t.Fatalf("decode beta events list: %v output=%q", err, betaEventsRaw)
	}
	if alphaEvents.Count < 1 || len(alphaEvents.Data) < 1 {
		t.Fatalf("expected alpha context to contain events, got %#v", alphaEvents)
	}
	for _, row := range betaEvents.Data {
		if row.Fields["host"] == "10.0.0.1" {
			t.Fatalf("unexpected alpha host leaked into beta event row: %#v", row)
		}
		if row.Fields["name"] == "cache-alpha" {
			t.Fatalf("unexpected alpha addon leaked into beta event row: %#v", row)
		}
	}
}

func TestPaasTaskboardShowReturnsDefaultWhenMissing(t *testing.T) {
	boardPath := filepath.Join(t.TempDir(), "shared-taskboard.json")
	t.Setenv(paasTaskboardPathEnvKey, boardPath)

	raw := captureStdout(t, func() {
		cmdPaas([]string{"taskboard", "show", "--json"})
	})
	var payload struct {
		OK      bool          `json:"ok"`
		Command string        `json:"command"`
		Count   int           `json:"count"`
		Data    paasTaskboard `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode taskboard show payload: %v output=%q", err, raw)
	}
	if !payload.OK || payload.Command != "taskboard show" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if payload.Count != 0 || len(payload.Data.Tasks) != 0 {
		t.Fatalf("expected empty default board, got %#v", payload)
	}
	if len(payload.Data.Columns) == 0 {
		t.Fatalf("expected default columns, got %#v", payload.Data)
	}
}

func TestPaasTaskboardAddListMoveFlow(t *testing.T) {
	tempDir := t.TempDir()
	boardPath := filepath.Join(tempDir, "shared-taskboard.json")
	t.Setenv(paasTaskboardPathEnvKey, boardPath)

	addRaw := captureStdout(t, func() {
		cmdPaas([]string{
			"taskboard", "add",
			"--title", "Evaluate enterprise billing controls",
			"--status", "paas-backlog",
			"--priority", "P1",
			"--owner", "automation-agent",
			"--workstream", "paas",
			"--ticket", "tickets/opportunities/20260218-enterprise-billing-controls.md",
			"--tags", "opportunity,paas",
			"--json",
		})
	})
	var addPayload struct {
		OK      bool              `json:"ok"`
		Command string            `json:"command"`
		Data    paasTaskboardTask `json:"data"`
	}
	if err := json.Unmarshal([]byte(addRaw), &addPayload); err != nil {
		t.Fatalf("decode taskboard add payload: %v output=%q", err, addRaw)
	}
	if !addPayload.OK || addPayload.Command != "taskboard add" {
		t.Fatalf("unexpected add payload: %#v", addPayload)
	}
	if strings.TrimSpace(addPayload.Data.ID) == "" {
		t.Fatalf("expected generated task id, got %#v", addPayload.Data)
	}
	if addPayload.Data.Status != "paas-backlog" || addPayload.Data.Priority != "P1" {
		t.Fatalf("unexpected add task fields: %#v", addPayload.Data)
	}

	listRaw := captureStdout(t, func() {
		cmdPaas([]string{"taskboard", "list", "--status", "paas-backlog", "--json"})
	})
	var listPayload struct {
		OK      bool                `json:"ok"`
		Command string              `json:"command"`
		Count   int                 `json:"count"`
		Data    []paasTaskboardTask `json:"data"`
	}
	if err := json.Unmarshal([]byte(listRaw), &listPayload); err != nil {
		t.Fatalf("decode taskboard list payload: %v output=%q", err, listRaw)
	}
	if !listPayload.OK || listPayload.Command != "taskboard list" {
		t.Fatalf("unexpected list payload: %#v", listPayload)
	}
	if listPayload.Count != 1 || len(listPayload.Data) != 1 {
		t.Fatalf("expected one task in backlog, got %#v", listPayload)
	}

	moveRaw := captureStdout(t, func() {
		cmdPaas([]string{
			"taskboard", "move",
			"--id", addPayload.Data.ID,
			"--status", "validate",
			"--json",
		})
	})
	var movePayload struct {
		OK      bool              `json:"ok"`
		Command string            `json:"command"`
		Data    paasTaskboardTask `json:"data"`
	}
	if err := json.Unmarshal([]byte(moveRaw), &movePayload); err != nil {
		t.Fatalf("decode taskboard move payload: %v output=%q", err, moveRaw)
	}
	if !movePayload.OK || movePayload.Command != "taskboard move" {
		t.Fatalf("unexpected move payload: %#v", movePayload)
	}
	if movePayload.Data.Status != "validate" {
		t.Fatalf("expected moved status=validate, got %#v", movePayload.Data)
	}

	boardRaw, err := os.ReadFile(boardPath)
	if err != nil {
		t.Fatalf("read saved taskboard: %v", err)
	}
	var board paasTaskboard
	if err := json.Unmarshal(boardRaw, &board); err != nil {
		t.Fatalf("decode saved taskboard: %v", err)
	}
	index := findPaasTaskboardTaskIndex(board.Tasks, addPayload.Data.ID)
	if index < 0 {
		t.Fatalf("expected saved task %q in board", addPayload.Data.ID)
	}
	if board.Tasks[index].Status != "validate" {
		t.Fatalf("expected saved task to be moved to validate, got %#v", board.Tasks[index])
	}
	mdPath := filepath.Join(filepath.Dir(boardPath), "SHARED_TASKBOARD.md")
	if _, err := os.Stat(mdPath); err != nil {
		t.Fatalf("expected markdown board at %s: %v", mdPath, err)
	}
}

func seedPaasTestBundleDir(t *testing.T) string {
	t.Helper()
	bundleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundleDir, "compose.yaml"), []byte("services:\n  api:\n    image: nginx:latest\n"), 0o600); err != nil {
		t.Fatalf("write test compose bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "release.json"), []byte("{\"release_id\":\"rel-test\"}\n"), 0o600); err != nil {
		t.Fatalf("write test release metadata: %v", err)
	}
	return bundleDir
}

func parsePaasEnvelope(t *testing.T, raw string) paasTestEnvelope {
	t.Helper()
	var env paasTestEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("decode envelope: %v output=%q", err, raw)
	}
	return env
}

func parseTargetListPayload(t *testing.T, raw string) paasTargetListPayload {
	t.Helper()
	var payload paasTargetListPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode target list payload: %v output=%q", err, raw)
	}
	return payload
}
