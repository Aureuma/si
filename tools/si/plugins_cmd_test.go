package main

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginsListCommandJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	home := t.TempDir()
	stdout, stderr, err := runSICommand(t, map[string]string{"HOME": home}, "plugins", "list", "--json")
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
	found := false
	for _, item := range rowsRaw {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if row["id"] == "si/browser-mcp" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected built-in plugin si/browser-mcp in list output: %#v", payload)
	}
}

func TestPluginsLifecycleViaCatalogJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	home := t.TempDir()
	workspace := t.TempDir()
	pluginID := "acme/release-mind"

	stdout, stderr, err := runSICommand(t, map[string]string{"HOME": home}, "plugins", "scaffold", pluginID, "--dir", workspace, "--json")
	if err != nil {
		t.Fatalf("scaffold failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	pluginPath := filepath.Join(workspace, "acme", "release-mind")
	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "plugins", "register", pluginPath, "--channel", "community", "--json")
	if err != nil {
		t.Fatalf("register failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "plugins", "install", pluginID, "--json")
	if err != nil {
		t.Fatalf("install failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "plugins", "info", pluginID, "--json")
	if err != nil {
		t.Fatalf("info failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var infoPayload map[string]any
	if err := json.Unmarshal([]byte(stdout), &infoPayload); err != nil {
		t.Fatalf("info json parse failed: %v\nstdout=%s", err, stdout)
	}
	if infoPayload["id"] != pluginID {
		t.Fatalf("unexpected plugin id payload: %#v", infoPayload)
	}
	if installed, _ := infoPayload["installed"].(bool); !installed {
		t.Fatalf("expected installed=true in info payload: %#v", infoPayload)
	}
	if inCatalog, _ := infoPayload["in_catalog"].(bool); !inCatalog {
		t.Fatalf("expected in_catalog=true in info payload: %#v", infoPayload)
	}

	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "plugins", "doctor", "--json")
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

func TestPluginsPolicyAffectsEffectiveState(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	home := t.TempDir()
	workspace := t.TempDir()
	pluginID := "acme/release-mind"
	pluginPath := filepath.Join(workspace, "acme", "release-mind")

	stdout, stderr, err := runSICommand(t, map[string]string{"HOME": home}, "plugins", "scaffold", pluginID, "--dir", workspace, "--json")
	if err != nil {
		t.Fatalf("scaffold failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "plugins", "register", pluginPath, "--json")
	if err != nil {
		t.Fatalf("register failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "plugins", "install", pluginID, "--json")
	if err != nil {
		t.Fatalf("install failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "plugins", "policy", "set", "--deny", pluginID, "--json")
	if err != nil {
		t.Fatalf("policy set failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "plugins", "info", pluginID, "--json")
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

	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "plugins", "list", "--installed", "--json")
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
	if row["id"] != pluginID {
		t.Fatalf("unexpected row id: %#v", row)
	}
	if effective, _ := row["effective_enabled"].(bool); effective {
		t.Fatalf("expected list effective_enabled=false: %#v", row)
	}
}

func TestPluginsListReadsEnvCatalogPaths(t *testing.T) {
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
		"HOME":                    home,
		"SI_PLUGIN_CATALOG_PATHS": catalogPath,
	}, "plugins", "list", "--json")
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
	for _, row := range rows {
		item, ok := row.(map[string]any)
		if !ok {
			continue
		}
		if item["id"] == "acme/release-mind" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected acme/release-mind row from env catalog: %#v", payload)
	}
}

func TestPluginsInstallFromArchivePath(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	home := t.TempDir()
	archivePath := filepath.Join(t.TempDir(), "plugin.zip")
	zipFile, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	writer := zip.NewWriter(zipFile)
	manifestEntry, err := writer.Create("acme/archive-plugin/si.plugin.json")
	if err != nil {
		t.Fatalf("create manifest entry: %v", err)
	}
	manifest := `{"schema_version":1,"id":"acme/archive-plugin","namespace":"acme","install":{"type":"none"}}`
	if _, err := manifestEntry.Write([]byte(manifest)); err != nil {
		t.Fatalf("write manifest entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	stdout, stderr, err := runSICommand(t, map[string]string{"HOME": home}, "plugins", "install", archivePath, "--json")
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
	if recordRaw["id"] != "acme/archive-plugin" {
		t.Fatalf("unexpected record id: %#v", recordRaw)
	}
}

func TestPluginsUpdateCommandJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	home := t.TempDir()
	pluginID := "si/browser-mcp"

	stdout, stderr, err := runSICommand(t, map[string]string{"HOME": home}, "plugins", "install", pluginID, "--json")
	if err != nil {
		t.Fatalf("install failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	stdout, stderr, err = runSICommand(t, map[string]string{"HOME": home}, "plugins", "update", pluginID, "--json")
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
		t.Fatalf("expected one updated plugin: %#v", payload)
	}
	if updated[0] != pluginID {
		t.Fatalf("unexpected updated plugin: %#v", updated)
	}
}
