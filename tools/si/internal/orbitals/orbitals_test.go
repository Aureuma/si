package orbitals

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateOrbitIDRequiresNamespace(t *testing.T) {
	if err := ValidateOrbitID("acme/release-mind"); err != nil {
		t.Fatalf("expected namespaced id to be valid: %v", err)
	}
	for _, invalid := range []string{"", "release-mind", "acme/../oops", "ACME/release"} {
		if err := ValidateOrbitID(invalid); err == nil {
			t.Fatalf("expected invalid orbit id error for %q", invalid)
		}
	}
}

func TestValidatePolicySelector(t *testing.T) {
	valid := []string{"acme/release-mind", "openclaw/*", "saas/*"}
	for _, selector := range valid {
		if err := ValidatePolicySelector(selector); err != nil {
			t.Fatalf("expected valid selector %q, got error: %v", selector, err)
		}
	}
	invalid := []string{"*", "openclaw*", "openclaw/", "/release", "OpenClaw/*"}
	for _, selector := range invalid {
		if err := ValidatePolicySelector(selector); err == nil {
			t.Fatalf("expected invalid selector %q to fail", selector)
		}
	}
}

func TestBuiltinCatalogIncludesCuratedIntegrations(t *testing.T) {
	catalog, diagnostics, err := loadCatalogFromRaw("builtin", builtinCatalogRaw)
	if err != nil {
		t.Fatalf("load builtin catalog: %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("expected builtin catalog diagnostics to be empty, got %#v", diagnostics)
	}
	byID := CatalogByID(catalog)
	if len(byID) != len(catalog.Entries) {
		t.Fatalf("expected unique builtin ids, got entries=%d unique=%d", len(catalog.Entries), len(byID))
	}
	if len(catalog.Entries) < 70 {
		t.Fatalf("expected expanded builtin catalog, got %d entries", len(catalog.Entries))
	}
	for _, id := range []string{"si/browser-mcp", "openclaw/discord", "saas/linear"} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("expected builtin catalog to contain %s", id)
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

func TestInstallFromPathCopiesOrbitDir(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		RootDir:     root,
		InstallsDir: filepath.Join(root, "installed"),
		StateFile:   filepath.Join(root, "state.json"),
		CatalogFile: filepath.Join(root, "catalog.json"),
		CatalogDir:  filepath.Join(root, "catalog.d"),
	}
	sourceDir := filepath.Join(root, "source-orbit")
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
		t.Fatalf("expected installed orbit to be enabled")
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
	sourceDir := filepath.Join(root, "source-orbit")
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
	policy.Allow = []string{"acme/*"}
	enabled, reason = ResolveEnableState(record.ID, record, policy)
	if !enabled || reason != "enabled" {
		t.Fatalf("expected wildcard allow to enable orbit, got enabled=%v reason=%q", enabled, reason)
	}

	policy = DefaultPolicy()
	policy.Deny = []string{"acme/*"}
	enabled, reason = ResolveEnableState(record.ID, record, policy)
	if enabled || !strings.Contains(reason, "denylist") {
		t.Fatalf("expected wildcard deny to block orbit, got enabled=%v reason=%q", enabled, reason)
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
	entry := CatalogByID(catalog)["acme/release-mind"]
	if strings.TrimSpace(entry.Source) == "" {
		t.Fatalf("expected catalog entry source to be populated")
	}
}

func TestMergeCatalogsDiagnosticsIncludePreviousSource(t *testing.T) {
	first := Catalog{
		SchemaVersion: 1,
		Entries: []CatalogEntry{
			{
				Source: "builtin",
				Manifest: Manifest{
					SchemaVersion: 1,
					ID:            "acme/release-mind",
					Namespace:     "acme",
					Install: InstallSpec{
						Type: InstallTypeNone,
					},
				},
			},
		},
	}
	second := Catalog{
		SchemaVersion: 1,
		Entries: []CatalogEntry{
			{
				Source: "/tmp/catalog.json",
				Manifest: Manifest{
					SchemaVersion: 1,
					ID:            "acme/release-mind",
					Namespace:     "acme",
					Install: InstallSpec{
						Type: InstallTypeNone,
					},
				},
			},
		},
	}
	_, diagnostics := MergeCatalogs(first, second)
	if len(diagnostics) != 1 {
		t.Fatalf("expected one override diagnostic, got %#v", diagnostics)
	}
	if diagnostics[0].Source != "/tmp/catalog.json" {
		t.Fatalf("expected overriding source in diagnostic, got %#v", diagnostics[0])
	}
	if !strings.Contains(diagnostics[0].Message, "previous=builtin") {
		t.Fatalf("expected diagnostic message to include previous source, got %#v", diagnostics[0])
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
	archivePath := filepath.Join(root, "orbit.zip")
	zipFile, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	writer := zip.NewWriter(zipFile)
	manifestRaw := `{"schema_version":1,"id":"acme/archive-orbit","namespace":"acme","install":{"type":"none"}}`
	manifestEntry, err := writer.Create("acme/archive-orbit/si.orbit.json")
	if err != nil {
		t.Fatalf("create manifest entry: %v", err)
	}
	if _, err := manifestEntry.Write([]byte(manifestRaw)); err != nil {
		t.Fatalf("write manifest entry: %v", err)
	}
	readmeEntry, err := writer.Create("acme/archive-orbit/README.md")
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
	if record.ID != "acme/archive-orbit" {
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
	entry, err := writer.Create("../si.orbit.json")
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
	source := filepath.Join(root, "orbit.bin")
	if err := os.WriteFile(source, []byte("not-a-orbit"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if _, err := InstallFromSource(paths, source, true, time.Time{}); err == nil {
		t.Fatalf("expected unsupported source to fail")
	}
}

func TestInstallFromCatalogURLArchive(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		RootDir:     root,
		InstallsDir: filepath.Join(root, "installed"),
		StateFile:   filepath.Join(root, "state.json"),
		CatalogFile: filepath.Join(root, "catalog.json"),
		CatalogDir:  filepath.Join(root, "catalog.d"),
	}
	archiveBytes := buildOrbitArchiveBytes(t, "acme/remote-orbit")
	sum := sha256.Sum256(archiveBytes)
	sha := hex.EncodeToString(sum[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orbit.zip" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archiveBytes)
	}))
	defer server.Close()

	entry := CatalogEntry{
		Manifest: Manifest{
			SchemaVersion: 1,
			ID:            "acme/remote-orbit",
			Namespace:     "acme",
			Install: InstallSpec{
				Type:   InstallTypeURLArchive,
				Source: server.URL + "/orbit.zip",
				Params: map[string]string{"sha256": sha},
			},
		},
	}

	record, err := InstallFromCatalog(paths, entry, true, time.Time{})
	if err != nil {
		t.Fatalf("install from URL archive: %v", err)
	}
	if record.Source != "catalog:acme/remote-orbit" {
		t.Fatalf("unexpected source: %s", record.Source)
	}
	if record.CatalogSource != server.URL+"/orbit.zip" {
		t.Fatalf("unexpected catalog source: %s", record.CatalogSource)
	}
	if _, err := os.Stat(filepath.Join(record.InstallDir, "README.md")); err != nil {
		t.Fatalf("installed archive file missing: %v", err)
	}
}

func TestInstallFromCatalogURLArchiveChecksumMismatch(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		RootDir:     root,
		InstallsDir: filepath.Join(root, "installed"),
		StateFile:   filepath.Join(root, "state.json"),
		CatalogFile: filepath.Join(root, "catalog.json"),
		CatalogDir:  filepath.Join(root, "catalog.d"),
	}
	archiveBytes := buildOrbitArchiveBytes(t, "acme/remote-orbit")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archiveBytes)
	}))
	defer server.Close()

	entry := CatalogEntry{
		Manifest: Manifest{
			SchemaVersion: 1,
			ID:            "acme/remote-orbit",
			Namespace:     "acme",
			Install: InstallSpec{
				Type:   InstallTypeURLArchive,
				Source: server.URL + "/orbit.zip",
				Params: map[string]string{"sha256": strings.Repeat("0", 64)},
			},
		},
	}

	if _, err := InstallFromCatalog(paths, entry, true, time.Time{}); err == nil {
		t.Fatalf("expected checksum mismatch error")
	} else if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func buildOrbitArchiveBytes(t *testing.T, id string) []byte {
	t.Helper()
	buf := bytes.Buffer{}
	writer := zip.NewWriter(&buf)
	manifestPath := id + "/" + ManifestFileName
	manifestEntry, err := writer.Create(manifestPath)
	if err != nil {
		t.Fatalf("create manifest entry: %v", err)
	}
	namespace := strings.Split(id, "/")[0]
	manifestRaw := `{"schema_version":1,"id":"` + id + `","namespace":"` + namespace + `","install":{"type":"none"}}`
	if _, err := manifestEntry.Write([]byte(manifestRaw)); err != nil {
		t.Fatalf("write manifest entry: %v", err)
	}
	readmeEntry, err := writer.Create(id + "/README.md")
	if err != nil {
		t.Fatalf("create readme entry: %v", err)
	}
	if _, err := readmeEntry.Write([]byte("remote orbit archive")); err != nil {
		t.Fatalf("write readme entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return buf.Bytes()
}

func TestDiscoverManifestPathsFromTree(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "openclaw", "discord")
	second := filepath.Join(root, "openclaw", "slack")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(first, ManifestFileName), []byte(`{"schema_version":1,"id":"openclaw/discord","namespace":"openclaw","install":{"type":"none"}}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(second, ManifestFileName), []byte(`{"schema_version":1,"id":"openclaw/slack","namespace":"openclaw","install":{"type":"none"}}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	paths, err := DiscoverManifestPaths(root)
	if err != nil {
		t.Fatalf("discover manifests: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected two manifest paths, got %#v", paths)
	}
}

func TestBuildCatalogFromSource(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "openclaw", "discord")
	second := filepath.Join(root, "openclaw", "slack")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(first, ManifestFileName), []byte(`{"schema_version":1,"id":"openclaw/discord","namespace":"openclaw","install":{"type":"none"}}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(second, ManifestFileName), []byte(`{"schema_version":1,"id":"openclaw/slack","namespace":"openclaw","install":{"type":"none"}}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	catalog, diagnostics, err := BuildCatalogFromSource(root, BuildCatalogOptions{
		Channel:  "ecosystem",
		Verified: true,
		AddedAt:  "2026-02-18",
		Tags:     []string{"openclaw", "channel"},
	})
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
	if len(catalog.Entries) != 2 {
		t.Fatalf("expected 2 catalog entries, got %#v", catalog.Entries)
	}
	if catalog.Entries[0].Channel != "ecosystem" || !catalog.Entries[0].Verified {
		t.Fatalf("unexpected entry metadata: %#v", catalog.Entries[0])
	}
	if catalog.Entries[0].AddedAt != "2026-02-18" {
		t.Fatalf("expected added_at propagated, got %#v", catalog.Entries[0])
	}
}

func TestBuildCatalogFromSourceSkipsDuplicateIDs(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "a")
	second := filepath.Join(root, "b")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	manifest := `{"schema_version":1,"id":"openclaw/discord","namespace":"openclaw","install":{"type":"none"}}`
	if err := os.WriteFile(filepath.Join(first, ManifestFileName), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write first manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(second, ManifestFileName), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write second manifest: %v", err)
	}
	catalog, diagnostics, err := BuildCatalogFromSource(root, BuildCatalogOptions{AddedAt: "2026-02-18"})
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	if len(catalog.Entries) != 1 {
		t.Fatalf("expected duplicate to be skipped, got %#v", catalog.Entries)
	}
	if len(diagnostics) != 1 || diagnostics[0].Level != "warn" {
		t.Fatalf("expected one warn diagnostic, got %#v", diagnostics)
	}
}
