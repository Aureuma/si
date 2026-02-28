package main

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestOrbitsListCommandJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	home := t.TempDir()
	stdout, stderr, err := runSICommand(t, map[string]string{"HOME": home}, "orbits", "list", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json parse failed: %v\nstdout=%s", err, stdout)
	}
	rowsRaw, ok := payload["rows"].([]any)
	if !ok {
		t.Fatalf("expected rows array in payload: %#v", payload)
	}
	required := map[string]bool{
		"si/browser-mcp":   false,
		"openclaw/discord": false,
		"saas/linear":      false,
	}
	for _, item := range rowsRaw {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := row["id"].(string)
		if _, tracked := required[id]; tracked {
			required[id] = true
		}
	}
	for id, found := range required {
		if !found {
			t.Fatalf("expected built-in orbit %s in list output: %#v", id, payload)
		}
	}
}

func TestOrbitsLifecycleViaCatalogJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	home := t.TempDir()
	workspace := t.TempDir()
	orbitID := "acme/release-mind"

	stdout, stderr, err := runSICommand(t, map[string]string{"HOME": home}, "orbits", "scaffold", orbitID, "--dir", workspace, "--json")
	if err != nil {
		t.Fatalf("scaffold failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	orbitPath := filepath.Join(workspace, "acme", "release-mind")
	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "orbits", "register", orbitPath, "--channel", "community", "--json")
	if err != nil {
		t.Fatalf("register failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "orbits", "install", orbitID, "--json")
	if err != nil {
		t.Fatalf("install failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "orbits", "info", orbitID, "--json")
	if err != nil {
		t.Fatalf("info failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var infoPayload map[string]any
	if err := json.Unmarshal([]byte(stdout), &infoPayload); err != nil {
		t.Fatalf("info json parse failed: %v\nstdout=%s", err, stdout)
	}
	if infoPayload["id"] != orbitID {
		t.Fatalf("unexpected orbit id payload: %#v", infoPayload)
	}
	if installed, _ := infoPayload["installed"].(bool); !installed {
		t.Fatalf("expected installed=true in info payload: %#v", infoPayload)
	}
	if inCatalog, _ := infoPayload["in_catalog"].(bool); !inCatalog {
		t.Fatalf("expected in_catalog=true in info payload: %#v", infoPayload)
	}

	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "orbits", "doctor", "--json")
	if err != nil {
		t.Fatalf("doctor failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var doctorPayload map[string]any
	if err := json.Unmarshal([]byte(stdout), &doctorPayload); err != nil {
		t.Fatalf("doctor json parse failed: %v\nstdout=%s", err, stdout)
	}
	if okVal, ok := doctorPayload["ok"].(bool); !ok || !okVal {
		t.Fatalf("expected doctor ok=true: %#v", doctorPayload)
	}
}

func TestOrbitsPolicyAffectsEffectiveState(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	home := t.TempDir()
	workspace := t.TempDir()
	orbitID := "acme/release-mind"
	orbitPath := filepath.Join(workspace, "acme", "release-mind")

	stdout, stderr, err := runSICommand(t, map[string]string{"HOME": home}, "orbits", "scaffold", orbitID, "--dir", workspace, "--json")
	if err != nil {
		t.Fatalf("scaffold failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "orbits", "register", orbitPath, "--json")
	if err != nil {
		t.Fatalf("register failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "orbits", "install", orbitID, "--json")
	if err != nil {
		t.Fatalf("install failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "orbits", "policy", "set", "--deny", orbitID, "--json")
	if err != nil {
		t.Fatalf("policy set failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "orbits", "info", orbitID, "--json")
	if err != nil {
		t.Fatalf("info failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var infoPayload map[string]any
	if err := json.Unmarshal([]byte(stdout), &infoPayload); err != nil {
		t.Fatalf("info json parse failed: %v\nstdout=%s", err, stdout)
	}
	if effective, _ := infoPayload["effective_enabled"].(bool); effective {
		t.Fatalf("expected effective_enabled=false after denylist policy: %#v", infoPayload)
	}
	reason, _ := infoPayload["effective_reason"].(string)
	if reason == "" {
		t.Fatalf("expected effective_reason in info payload: %#v", infoPayload)
	}

	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "orbits", "list", "--installed", "--json")
	if err != nil {
		t.Fatalf("list failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var listPayload map[string]any
	if err := json.Unmarshal([]byte(stdout), &listPayload); err != nil {
		t.Fatalf("list json parse failed: %v\nstdout=%s", err, stdout)
	}
	rows, ok := listPayload["rows"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("expected one installed row: %#v", listPayload)
	}
	row, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected row shape: %#v", rows[0])
	}
	if row["id"] != orbitID {
		t.Fatalf("unexpected row id: %#v", row)
	}
	if effective, _ := row["effective_enabled"].(bool); effective {
		t.Fatalf("expected list effective_enabled=false: %#v", row)
	}
}

func TestOrbitsListReadsEnvCatalogPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	home := t.TempDir()
	catalogPath := filepath.Join(t.TempDir(), "external-catalog.json")
	content := `{
  "schema_version": 1,
  "entries": [
    {
      "channel": "community",
      "manifest": {
        "schema_version": 1,
        "id": "acme/release-mind",
        "namespace": "acme",
        "name": "Release Mind",
        "install": { "type": "none" }
      }
    }
  ]
}`
	if err := os.WriteFile(catalogPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	stdout, stderr, err := runSICommand(t, map[string]string{
		"HOME":                   home,
		"SI_ORBIT_CATALOG_PATHS": catalogPath,
	}, "orbits", "list", "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json parse failed: %v\nstdout=%s", err, stdout)
	}
	rows, ok := payload["rows"].([]any)
	if !ok {
		t.Fatalf("expected rows array payload, got %#v", payload)
	}
	found := false
	foundSource := false
	for _, row := range rows {
		item, ok := row.(map[string]any)
		if !ok {
			continue
		}
		if item["id"] == "acme/release-mind" {
			found = true
			if source, _ := item["catalog_source"].(string); source == catalogPath {
				foundSource = true
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected acme/release-mind row from env catalog: %#v", payload)
	}
	if !foundSource {
		t.Fatalf("expected acme/release-mind row to include catalog_source=%s: %#v", catalogPath, payload)
	}
}

func TestOrbitsInstallFromArchivePath(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	home := t.TempDir()
	archivePath := filepath.Join(t.TempDir(), "orbit.zip")
	zipFile, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	writer := zip.NewWriter(zipFile)
	manifestEntry, err := writer.Create("acme/archive-orbit/si.orbit.json")
	if err != nil {
		t.Fatalf("create manifest entry: %v", err)
	}
	manifest := `{"schema_version":1,"id":"acme/archive-orbit","namespace":"acme","install":{"type":"none"}}`
	if _, err := manifestEntry.Write([]byte(manifest)); err != nil {
		t.Fatalf("write manifest entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	stdout, stderr, err := runSICommand(t, map[string]string{"HOME": home}, "orbits", "install", archivePath, "--json")
	if err != nil {
		t.Fatalf("install failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var installPayload map[string]any
	if err := json.Unmarshal([]byte(stdout), &installPayload); err != nil {
		t.Fatalf("install json parse failed: %v\nstdout=%s", err, stdout)
	}
	recordRaw, ok := installPayload["record"].(map[string]any)
	if !ok {
		t.Fatalf("expected record payload: %#v", installPayload)
	}
	if recordRaw["id"] != "acme/archive-orbit" {
		t.Fatalf("unexpected record id: %#v", recordRaw)
	}
}

func TestOrbitsUpdateCommandJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	home := t.TempDir()
	orbitID := "si/browser-mcp"

	stdout, stderr, err := runSICommand(t, map[string]string{"HOME": home}, "orbits", "install", orbitID, "--json")
	if err != nil {
		t.Fatalf("install failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "orbits", "update", orbitID, "--json")
	if err != nil {
		t.Fatalf("update failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json parse failed: %v\nstdout=%s", err, stdout)
	}
	if ok, _ := payload["ok"].(bool); !ok {
		t.Fatalf("expected update ok=true payload: %#v", payload)
	}
	updated, ok := payload["updated"].([]any)
	if !ok || len(updated) != 1 {
		t.Fatalf("expected one updated orbit: %#v", payload)
	}
	if updated[0] != orbitID {
		t.Fatalf("unexpected updated orbit: %#v", updated)
	}
}

func TestOrbitsCatalogBuildAndValidateJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	home := t.TempDir()
	sourceRoot := t.TempDir()
	outputPath := filepath.Join(t.TempDir(), "ecosystem-catalog.json")
	discordDir := filepath.Join(sourceRoot, "openclaw", "discord")
	slackDir := filepath.Join(sourceRoot, "openclaw", "slack")
	if err := os.MkdirAll(discordDir, 0o755); err != nil {
		t.Fatalf("mkdir discord dir: %v", err)
	}
	if err := os.MkdirAll(slackDir, 0o755); err != nil {
		t.Fatalf("mkdir slack dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(discordDir, "si.orbit.json"), []byte(`{"schema_version":1,"id":"openclaw/discord","namespace":"openclaw","install":{"type":"none"}}`), 0o644); err != nil {
		t.Fatalf("write discord manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(slackDir, "si.orbit.json"), []byte(`{"schema_version":1,"id":"openclaw/slack","namespace":"openclaw","install":{"type":"none"}}`), 0o644); err != nil {
		t.Fatalf("write slack manifest: %v", err)
	}

	stdout, stderr, err := runSICommand(t, map[string]string{"HOME": home}, "orbits", "catalog", "build", "--source", sourceRoot, "--output", outputPath, "--channel", "ecosystem", "--verified", "--tag", "openclaw", "--json")
	if err != nil {
		t.Fatalf("catalog build failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var buildPayload map[string]any
	if err := json.Unmarshal([]byte(stdout), &buildPayload); err != nil {
		t.Fatalf("build json parse failed: %v\nstdout=%s", err, stdout)
	}
	if ok, _ := buildPayload["ok"].(bool); !ok {
		t.Fatalf("expected build ok=true: %#v", buildPayload)
	}
	if entries, _ := buildPayload["entries"].(float64); int(entries) != 2 {
		t.Fatalf("expected 2 build entries: %#v", buildPayload)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected output file written: %v", err)
	}

	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "orbits", "catalog", "validate", "--source", sourceRoot, "--json")
	if err != nil {
		t.Fatalf("catalog validate failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var validatePayload map[string]any
	if err := json.Unmarshal([]byte(stdout), &validatePayload); err != nil {
		t.Fatalf("validate json parse failed: %v\nstdout=%s", err, stdout)
	}
	if ok, _ := validatePayload["ok"].(bool); !ok {
		t.Fatalf("expected validate ok=true: %#v", validatePayload)
	}
	if entries, _ := validatePayload["entries"].(float64); int(entries) != 2 {
		t.Fatalf("expected 2 validate entries: %#v", validatePayload)
	}
}

func TestOrbitsPolicySetSupportsNamespaceWildcard(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	home := t.TempDir()
	workspace := t.TempDir()
	orbitID := "acme/release-mind"
	orbitPath := filepath.Join(workspace, "acme", "release-mind")

	stdout, stderr, err := runSICommand(t, map[string]string{"HOME": home}, "orbits", "scaffold", orbitID, "--dir", workspace, "--json")
	if err != nil {
		t.Fatalf("scaffold failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "orbits", "register", orbitPath, "--json")
	if err != nil {
		t.Fatalf("register failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "orbits", "install", orbitID, "--json")
	if err != nil {
		t.Fatalf("install failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	// Wildcard allow should keep acme/* orbits active.
	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "orbits", "policy", "set", "--clear-allow", "--clear-deny", "--allow", "acme/*", "--json")
	if err != nil {
		t.Fatalf("policy wildcard allow failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "orbits", "info", orbitID, "--json")
	if err != nil {
		t.Fatalf("info failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var infoAllow map[string]any
	if err := json.Unmarshal([]byte(stdout), &infoAllow); err != nil {
		t.Fatalf("info json parse failed: %v\nstdout=%s", err, stdout)
	}
	if effective, _ := infoAllow["effective_enabled"].(bool); !effective {
		t.Fatalf("expected effective_enabled=true with wildcard allow: %#v", infoAllow)
	}

	// Wildcard deny should block acme/* orbits.
	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "orbits", "policy", "set", "--clear-allow", "--clear-deny", "--deny", "acme/*", "--json")
	if err != nil {
		t.Fatalf("policy wildcard deny failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "orbits", "info", orbitID, "--json")
	if err != nil {
		t.Fatalf("info failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var infoDeny map[string]any
	if err := json.Unmarshal([]byte(stdout), &infoDeny); err != nil {
		t.Fatalf("info json parse failed: %v\nstdout=%s", err, stdout)
	}
	if effective, _ := infoDeny["effective_enabled"].(bool); effective {
		t.Fatalf("expected effective_enabled=false with wildcard deny: %#v", infoDeny)
	}
}

func TestOrbitsInfoIncludesCatalogSourceForBuiltin(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	home := t.TempDir()
	stdout, stderr, err := runSICommand(t, map[string]string{"HOME": home}, "orbits", "info", "si/browser-mcp", "--json")
	if err != nil {
		t.Fatalf("info failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("info json parse failed: %v\nstdout=%s", err, stdout)
	}
	source, _ := payload["catalog_source"].(string)
	if source != "builtin" {
		t.Fatalf("expected builtin catalog source, got %#v", payload)
	}
}

func TestOrbitsGatewayBuildWritesBundle(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	home := t.TempDir()
	sourceRoot := t.TempDir()
	outputDir := filepath.Join(t.TempDir(), "gateway-out")
	orbitDir := filepath.Join(sourceRoot, "acme", "gateway")
	if err := os.MkdirAll(orbitDir, 0o755); err != nil {
		t.Fatalf("mkdir orbit dir: %v", err)
	}
	manifest := `{"schema_version":1,"id":"acme/gateway","namespace":"acme","install":{"type":"none"},"integration":{"capabilities":["chat.send"]}}`
	if err := os.WriteFile(filepath.Join(orbitDir, "si.orbit.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	stdout, stderr, err := runSICommand(t, map[string]string{"HOME": home}, "orbits", "gateway", "build",
		"--source", sourceRoot,
		"--registry", "team",
		"--slots", "8",
		"--output-dir", outputDir,
		"--json",
	)
	if err != nil {
		t.Fatalf("gateway build failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("decode output: %v\nstdout=%s", err, stdout)
	}
	if ok, _ := payload["ok"].(bool); !ok {
		t.Fatalf("expected ok=true payload=%#v", payload)
	}
	if got, _ := payload["registry"].(string); got != "team" {
		t.Fatalf("unexpected registry payload=%#v", payload)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "index.json")); err != nil {
		t.Fatalf("missing index.json: %v", err)
	}
}

func TestOrbitsGatewayPushPullRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	home := t.TempDir()
	sourceRoot := t.TempDir()
	outPath := filepath.Join(t.TempDir(), "pulled-catalog.json")
	orbitDir := filepath.Join(sourceRoot, "acme", "chat")
	if err := os.MkdirAll(orbitDir, 0o755); err != nil {
		t.Fatalf("mkdir orbit dir: %v", err)
	}
	manifest := `{"schema_version":1,"id":"acme/chat","namespace":"acme","install":{"type":"none"},"integration":{"capabilities":["chat.send"]}}`
	if err := os.WriteFile(filepath.Join(orbitDir, "si.orbit.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	var mu sync.Mutex
	indexRaw := []byte(`{"registry":"team","shards":[]}`)
	shardRawByKey := map[string][]byte{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("Authorization")) != "Bearer token-123" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/v1/integrations/registries/team":
			var body struct {
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			indexRaw = append([]byte{}, body.Payload...)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"object":   map[string]any{"latest_revision": 1},
					"revision": map[string]any{"revision": 1},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/integrations/registries/team":
			mu.Lock()
			raw := append([]byte{}, indexRaw...)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"registry": "team",
				"index":    json.RawMessage(raw),
			})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/integrations/registries/team/shards/"):
			shardKey := strings.TrimPrefix(r.URL.Path, "/v1/integrations/registries/team/shards/")
			var body struct {
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			shardRawByKey[shardKey] = append([]byte{}, body.Payload...)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"object":   map[string]any{"latest_revision": 1},
					"revision": map[string]any{"revision": 1},
				},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/integrations/registries/team/shards/"):
			shardKey := strings.TrimPrefix(r.URL.Path, "/v1/integrations/registries/team/shards/")
			mu.Lock()
			raw := append([]byte{}, shardRawByKey[shardKey]...)
			mu.Unlock()
			if len(raw) == 0 {
				http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"registry": "team",
				"shard":    shardKey,
				"payload":  json.RawMessage(raw),
			})
		default:
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		}
	}))
	defer server.Close()

	env := map[string]string{
		"HOME":            home,
		"SI_SUN_BASE_URL": server.URL,
		"SI_SUN_TOKEN":    "token-123",
	}
	stdout, stderr, err := runSICommand(t, env, "orbits", "gateway", "push", "--source", sourceRoot, "--registry", "team", "--json")
	if err != nil {
		t.Fatalf("gateway push failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, env, "orbits", "gateway", "pull", "--registry", "team", "--out", outPath, "--json")
	if err != nil {
		t.Fatalf("gateway pull failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read pulled catalog: %v", err)
	}
	var catalog struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("decode pulled catalog: %v", err)
	}
	if len(catalog.Entries) != 1 {
		t.Fatalf("expected one pulled entry, got %d", len(catalog.Entries))
	}
	manifestRaw, _ := catalog.Entries[0]["manifest"].(map[string]any)
	if id, _ := manifestRaw["id"].(string); id != "acme/chat" {
		t.Fatalf("unexpected pulled entry: %s", string(raw))
	}
}
