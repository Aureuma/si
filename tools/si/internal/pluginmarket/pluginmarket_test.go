package pluginmarket

import (
	"archive/zip"
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

func TestResolveEnableStatePolicy(t *testing.T) {
	record := InstallRecord{
		ID:      "acme/release-mind",
		Enabled: true,
	}

	enabled, reason := ResolveEnableState(record.ID, record, DefaultPolicy())
	if !enabled || reason != "enabled" {
		t.Fatalf("expected enabled default state, got enabled=%v reason=%q", enabled, reason)
	}

	policy := DefaultPolicy()
	policy.Enabled = false
	enabled, reason = ResolveEnableState(record.ID, record, policy)
	if enabled || !strings.Contains(reason, "disabled") {
		t.Fatalf("expected policy disabled block, got enabled=%v reason=%q", enabled, reason)
	}

	policy = DefaultPolicy()
	policy.Deny = []string{"acme/release-mind"}
	enabled, reason = ResolveEnableState(record.ID, record, policy)
	if enabled || !strings.Contains(reason, "denylist") {
		t.Fatalf("expected denylist block, got enabled=%v reason=%q", enabled, reason)
	}

	policy = DefaultPolicy()
	policy.Allow = []string{"acme/other"}
	enabled, reason = ResolveEnableState(record.ID, record, policy)
	if enabled || !strings.Contains(reason, "allowlist") {
		t.Fatalf("expected allowlist block, got enabled=%v reason=%q", enabled, reason)
	}

	policy = DefaultPolicy()
	record.Enabled = false
	enabled, reason = ResolveEnableState(record.ID, record, policy)
	if enabled || !strings.Contains(reason, "install record disabled") {
		t.Fatalf("expected record disabled block, got enabled=%v reason=%q", enabled, reason)
	}
}

func TestParseCatalogPathList(t *testing.T) {
	raw := " /tmp/a.json, /tmp/b.json;/tmp/c.json "
	paths := ParseCatalogPathList(raw)
	if len(paths) != 3 {
		t.Fatalf("expected 3 parsed paths, got %#v", paths)
	}
}

func TestLoadCatalogIncludesEnvPaths(t *testing.T) {
	base := t.TempDir()
	paths := Paths{
		RootDir:     base,
		InstallsDir: filepath.Join(base, "installed"),
		StateFile:   filepath.Join(base, "state.json"),
		CatalogFile: filepath.Join(base, "catalog.json"),
		CatalogDir:  filepath.Join(base, "catalog.d"),
	}
	externalCatalog := filepath.Join(base, "external.json")
	content := `{
  "schema_version": 1,
  "entries": [
    {
      "channel": "community",
      "verified": false,
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
	if err := os.WriteFile(externalCatalog, []byte(content), 0o644); err != nil {
		t.Fatalf("write external catalog: %v", err)
	}
	t.Setenv(CatalogPathsEnv, externalCatalog)
	catalog, diagnostics, err := LoadCatalog(paths)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
	if _, ok := CatalogByID(catalog)["acme/release-mind"]; !ok {
		t.Fatalf("expected env-loaded catalog entry, got %#v", catalog.Entries)
	}
}

func TestInstallFromArchiveZIP(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		RootDir:     root,
		InstallsDir: filepath.Join(root, "installed"),
		StateFile:   filepath.Join(root, "state.json"),
		CatalogFile: filepath.Join(root, "catalog.json"),
		CatalogDir:  filepath.Join(root, "catalog.d"),
	}
	archivePath := filepath.Join(root, "plugin.zip")
	zipFile, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	writer := zip.NewWriter(zipFile)
	manifestRaw := `{"schema_version":1,"id":"acme/archive-plugin","namespace":"acme","install":{"type":"none"}}`
	manifestEntry, err := writer.Create("acme/archive-plugin/si.plugin.json")
	if err != nil {
		t.Fatalf("create manifest entry: %v", err)
	}
	if _, err := manifestEntry.Write([]byte(manifestRaw)); err != nil {
		t.Fatalf("write manifest entry: %v", err)
	}
	readmeEntry, err := writer.Create("acme/archive-plugin/README.md")
	if err != nil {
		t.Fatalf("create readme entry: %v", err)
	}
	if _, err := readmeEntry.Write([]byte("archive test")); err != nil {
		t.Fatalf("write readme entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	record, err := InstallFromSource(paths, archivePath, true, time.Time{})
	if err != nil {
		t.Fatalf("install from archive: %v", err)
	}
	if record.ID != "acme/archive-plugin" {
		t.Fatalf("unexpected record id: %s", record.ID)
	}
	if !strings.HasPrefix(record.Source, "archive:") {
		t.Fatalf("expected archive source marker, got: %s", record.Source)
	}
	if _, err := os.Stat(filepath.Join(record.InstallDir, "README.md")); err != nil {
		t.Fatalf("installed readme missing: %v", err)
	}
}

func TestInstallFromArchiveRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		RootDir:     root,
		InstallsDir: filepath.Join(root, "installed"),
		StateFile:   filepath.Join(root, "state.json"),
		CatalogFile: filepath.Join(root, "catalog.json"),
		CatalogDir:  filepath.Join(root, "catalog.d"),
	}
	archivePath := filepath.Join(root, "traversal.zip")
	zipFile, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	writer := zip.NewWriter(zipFile)
	entry, err := writer.Create("../si.plugin.json")
	if err != nil {
		t.Fatalf("create traversal entry: %v", err)
	}
	if _, err := entry.Write([]byte(`{"schema_version":1,"id":"acme/bad","namespace":"acme","install":{"type":"none"}}`)); err != nil {
		t.Fatalf("write traversal entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	if _, err := InstallFromSource(paths, archivePath, true, time.Time{}); err == nil {
		t.Fatalf("expected traversal archive install failure")
	} else if !strings.Contains(err.Error(), "escapes destination") {
		t.Fatalf("expected traversal error, got: %v", err)
	}
}

func TestInstallFromSourceRejectsUnsupportedFile(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		RootDir:     root,
		InstallsDir: filepath.Join(root, "installed"),
		StateFile:   filepath.Join(root, "state.json"),
		CatalogFile: filepath.Join(root, "catalog.json"),
		CatalogDir:  filepath.Join(root, "catalog.d"),
	}
	source := filepath.Join(root, "plugin.bin")
	if err := os.WriteFile(source, []byte("not-a-plugin"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if _, err := InstallFromSource(paths, source, true, time.Time{}); err == nil {
		t.Fatalf("expected unsupported source to fail")
	}
}
