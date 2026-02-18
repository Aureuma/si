package pluginmarket

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidatePluginIDRequiresNamespace(t *testing.T) {
	if err := ValidatePluginID("acme/release-mind"); err != nil {
		t.Fatalf("expected namespaced id to be valid: %v", err)
	}
	for _, invalid := range []string{"", "release-mind", "acme/../oops", "ACME/release"} {
		if err := ValidatePluginID(invalid); err == nil {
			t.Fatalf("expected invalid plugin id error for %q", invalid)
		}
	}
}

func TestResolveSafeInstallDirRejectsTraversal(t *testing.T) {
	base := t.TempDir()
	resolved, err := resolveSafeInstallDir(base, "acme/release-mind")
	if err != nil {
		t.Fatalf("resolve safe install dir: %v", err)
	}
	if !strings.HasPrefix(resolved, filepath.Clean(base)+string(filepath.Separator)) {
		t.Fatalf("resolved path escaped base: base=%s resolved=%s", base, resolved)
	}
	if _, err := resolveSafeInstallDir(base, "acme/../../escape"); err == nil {
		t.Fatalf("expected traversal id to be rejected")
	}
}

func TestInstallFromPathCopiesPluginDir(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		RootDir:     root,
		InstallsDir: filepath.Join(root, "installed"),
		StateFile:   filepath.Join(root, "state.json"),
		CatalogFile: filepath.Join(root, "catalog.json"),
		CatalogDir:  filepath.Join(root, "catalog.d"),
	}
	sourceDir := filepath.Join(root, "source-plugin")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	manifest := Manifest{
		SchemaVersion: 1,
		ID:            "acme/release-mind",
		Namespace:     "acme",
		Name:          "Release Mind",
		Version:       "0.1.0",
		Maturity:      "experimental",
		Install: InstallSpec{
			Type: InstallTypeNone,
		},
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, ManifestFileName), raw, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "README.md"), []byte("test"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}

	record, err := InstallFromPath(paths, sourceDir, true, time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("install from path: %v", err)
	}
	if record.ID != "acme/release-mind" {
		t.Fatalf("unexpected record id: %s", record.ID)
	}
	if !record.Enabled {
		t.Fatalf("expected installed plugin to be enabled")
	}
	if !strings.Contains(record.Source, sourceDir) {
		t.Fatalf("expected source path reference, got: %s", record.Source)
	}
	if _, err := os.Stat(filepath.Join(record.InstallDir, ManifestFileName)); err != nil {
		t.Fatalf("copied manifest missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(record.InstallDir, "README.md")); err != nil {
		t.Fatalf("copied readme missing: %v", err)
	}
}

func TestInstallFromPathRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		RootDir:     root,
		InstallsDir: filepath.Join(root, "installed"),
		StateFile:   filepath.Join(root, "state.json"),
		CatalogFile: filepath.Join(root, "catalog.json"),
		CatalogDir:  filepath.Join(root, "catalog.d"),
	}
	sourceDir := filepath.Join(root, "source-plugin")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	manifest := `{"schema_version":1,"id":"acme/symlink-test","namespace":"acme","install":{"type":"none"}}`
	if err := os.WriteFile(filepath.Join(sourceDir, ManifestFileName), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(sourceDir, "passwd-link")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if _, err := InstallFromPath(paths, sourceDir, true, time.Time{}); err == nil {
		t.Fatalf("expected symlink install to fail")
	} else if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink failure, got: %v", err)
	}
}

func TestDoctorReportsEscapingInstallDir(t *testing.T) {
	paths := Paths{InstallsDir: filepath.Join(t.TempDir(), "installed")}
	state := State{
		SchemaVersion: 1,
		Installs: map[string]InstallRecord{
			"acme/release": {
				ID:         "acme/release",
				Enabled:    true,
				Source:     "catalog:acme/release",
				InstallDir: "/tmp",
				Manifest: Manifest{
					SchemaVersion: 1,
					ID:            "acme/release",
					Namespace:     "acme",
					Install: InstallSpec{
						Type: InstallTypeNone,
					},
				},
			},
		},
	}
	diagnostics := Doctor(Catalog{}, state, paths)
	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Level == "error" && strings.Contains(diagnostic.Message, "install dir") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected doctor error for escaping install dir, got: %#v", diagnostics)
	}
}
