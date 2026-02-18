package main

import (
	"encoding/json"
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
