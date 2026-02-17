package main

import (
	"encoding/json"
	"fmt"
	"os"
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
	}
	for _, tc := range tests {
		out := captureStdout(t, tc.invoke)
		if !strings.Contains(out, tc.usage) {
			t.Fatalf("%s expected usage output, got %q", tc.name, out)
		}
	}
}

func TestPaasJSONOutputContractTargetAdd(t *testing.T) {
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
	expectActionNames(t, "paas", paasActions, []string{"target", "app", "deploy", "rollback", "logs", "alert", "secret", "ai", "context", "agent", "events"})
	expectActionNames(t, "paas target", paasTargetActions, []string{"add", "list", "check", "use", "remove", "bootstrap", "ingress-baseline"})
	expectActionNames(t, "paas app", paasAppActions, []string{"init", "list", "status", "remove"})
	expectActionNames(t, "paas alert", paasAlertActions, []string{"setup-telegram", "test", "history"})
	expectActionNames(t, "paas secret", paasSecretActions, []string{"set", "get", "unset", "list", "key"})
	expectActionNames(t, "paas ai", paasAIActions, []string{"plan", "inspect", "fix"})
	expectActionNames(t, "paas context", paasContextActions, []string{"create", "list", "use", "show", "remove"})
	expectActionNames(t, "paas agent", paasAgentActions, []string{"enable", "disable", "status", "logs", "run-once", "approve", "deny"})
	expectActionNames(t, "paas events", paasEventsActions, []string{"list"})
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
	want := "PAAS__CTX_DEFAULT__APP_BILLING_API__TARGET_EDGE_A__VAR_STRIPE_API_KEY"
	if got != want {
		t.Fatalf("unexpected vault key convention: got=%q want=%q", got, want)
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
	root := t.TempDir()
	vaultFile := filepath.Join(root, ".env")
	trustStore := filepath.Join(root, "trust.json")
	doc := fmt.Sprintf("%s%s\nAPP_ENV=prod\n", vault.VaultRecipientPrefix, "age1examplerecipient000000000000000000000000000000000000000000000000")
	if err := os.WriteFile(vaultFile, []byte(doc), 0o600); err != nil {
		t.Fatalf("write vault file: %v", err)
	}
	t.Setenv("SI_VAULT_FILE", vaultFile)
	t.Setenv("SI_VAULT_TRUST_STORE", trustStore)

	settings := loadSettingsOrDefault()
	target, err := vaultResolveTarget(settings, "", false)
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	parsedDoc, err := vault.ReadDotenvFile(target.File)
	if err != nil {
		t.Fatalf("read dotenv: %v", err)
	}
	fp, err := vaultTrustFingerprint(parsedDoc)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	store, err := vault.LoadTrustStore(trustStore)
	if err != nil {
		t.Fatalf("load trust store: %v", err)
	}
	store.Upsert(vault.TrustEntry{
		RepoRoot:    target.RepoRoot,
		File:        target.File,
		Fingerprint: fp,
	})
	if err := store.Save(trustStore); err != nil {
		t.Fatalf("save trust store: %v", err)
	}

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
	vaultFile := filepath.Join(root, ".env")
	trustStore := filepath.Join(root, "trust.json")
	doc := fmt.Sprintf("%s%s\nAPP_ENV=prod\n", vault.VaultRecipientPrefix, "age1examplerecipient000000000000000000000000000000000000000000000000")
	if err := os.WriteFile(vaultFile, []byte(doc), 0o600); err != nil {
		t.Fatalf("write vault file: %v", err)
	}
	t.Setenv("SI_VAULT_FILE", vaultFile)
	t.Setenv("SI_VAULT_TRUST_STORE", trustStore)

	if _, err := runPaasVaultDeployGuardrail("", false); err == nil {
		t.Fatalf("expected trust error without allow-untrusted override")
	}
	result, err := runPaasVaultDeployGuardrail("", true)
	if err != nil {
		t.Fatalf("expected allow-untrusted to bypass trust mismatch: %v", err)
	}
	if result.Trusted {
		t.Fatalf("expected untrusted status with override, got %#v", result)
	}
	if strings.TrimSpace(result.TrustWarning) == "" {
		t.Fatalf("expected trust warning message, got %#v", result)
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
	stateRoot := t.TempDir()
	composePath := filepath.Join(t.TempDir(), "compose.yaml")
	vaultFile := filepath.Join(t.TempDir(), ".env")
	trustStore := filepath.Join(t.TempDir(), "trust.json")
	t.Setenv(paasStateRootEnvKey, stateRoot)
	t.Setenv("SI_VAULT_FILE", vaultFile)
	t.Setenv("SI_VAULT_TRUST_STORE", trustStore)

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

func TestPaasDeployCreatesReleaseBundleMetadata(t *testing.T) {
	stateRoot := t.TempDir()
	composePath := filepath.Join(t.TempDir(), "compose.yaml")
	vaultFile := filepath.Join(t.TempDir(), ".env")
	trustStore := filepath.Join(t.TempDir(), "trust.json")
	t.Setenv(paasStateRootEnvKey, stateRoot)
	t.Setenv("SI_VAULT_FILE", vaultFile)
	t.Setenv("SI_VAULT_TRUST_STORE", trustStore)

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
	if !strings.Contains(sshText, "docker compose -f compose.yaml up -d --remove-orphans") {
		t.Fatalf("expected compose apply command in ssh log, got %q", sshText)
	}
	if strings.Count(scpText, "root@203.0.113.10") < 2 {
		t.Fatalf("expected compose and metadata uploads in scp log, got %q", scpText)
	}
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
