package pluginmarket

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	ManifestFileName = "si.plugin.json"
	SchemaVersion    = 1
	CatalogPathsEnv  = "SI_PLUGIN_CATALOG_PATHS"
)

const (
	InstallTypeNone      = "none"
	InstallTypeLocalPath = "local_path"
	InstallTypeMCPHTTP   = "mcp_http"
	InstallTypeOCIImage  = "oci_image"
	InstallTypeGit       = "git"
)

var (
	pluginIDSegmentPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	maturityValues         = map[string]bool{"": true, "experimental": true, "beta": true, "ga": true}
	installTypeValues      = map[string]bool{
		InstallTypeNone:      true,
		InstallTypeLocalPath: true,
		InstallTypeMCPHTTP:   true,
		InstallTypeOCIImage:  true,
		InstallTypeGit:       true,
	}
)

var wildcardPolicyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/\*$`)

//go:embed builtin_catalog.json
var builtinCatalogRaw []byte

type Paths struct {
	RootDir     string
	InstallsDir string
	StateFile   string
	CatalogFile string
	CatalogDir  string
}

type Manifest struct {
	SchemaVersion int                    `json:"schema_version,omitempty"`
	ID            string                 `json:"id"`
	Namespace     string                 `json:"namespace,omitempty"`
	Name          string                 `json:"name,omitempty"`
	Version       string                 `json:"version,omitempty"`
	Summary       string                 `json:"summary,omitempty"`
	Description   string                 `json:"description,omitempty"`
	Homepage      string                 `json:"homepage,omitempty"`
	TermsURL      string                 `json:"terms_url,omitempty"`
	PrivacyURL    string                 `json:"privacy_url,omitempty"`
	License       string                 `json:"license,omitempty"`
	Maturity      string                 `json:"maturity,omitempty"`
	Kind          string                 `json:"kind,omitempty"`
	Install       InstallSpec            `json:"install,omitempty"`
	Integration   IntegrationSpec        `json:"integration,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

type InstallSpec struct {
	Type         string            `json:"type,omitempty"`
	Source       string            `json:"source,omitempty"`
	EntryCommand []string          `json:"entry_command,omitempty"`
	Env          []string          `json:"env,omitempty"`
	Params       map[string]string `json:"params,omitempty"`
}

type IntegrationSpec struct {
	ProviderIDs  []string    `json:"provider_ids,omitempty"`
	Commands     []string    `json:"commands,omitempty"`
	MCPServers   []MCPServer `json:"mcp_servers,omitempty"`
	Capabilities []string    `json:"capabilities,omitempty"`
}

type MCPServer struct {
	Name      string   `json:"name"`
	Transport string   `json:"transport"`
	Endpoint  string   `json:"endpoint,omitempty"`
	Command   []string `json:"command,omitempty"`
}

type Catalog struct {
	SchemaVersion int            `json:"schema_version,omitempty"`
	Entries       []CatalogEntry `json:"entries"`
}

type CatalogEntry struct {
	Manifest Manifest `json:"manifest"`
	Channel  string   `json:"channel,omitempty"`
	Verified bool     `json:"verified,omitempty"`
	AddedAt  string   `json:"added_at,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Source   string   `json:"-"`
}

type BuildCatalogOptions struct {
	Channel  string
	Verified bool
	AddedAt  string
	Tags     []string
}

type State struct {
	SchemaVersion int                      `json:"schema_version,omitempty"`
	Policy        Policy                   `json:"policy,omitempty"`
	Installs      map[string]InstallRecord `json:"installs,omitempty"`
}

type Policy struct {
	Enabled bool     `json:"enabled"`
	Allow   []string `json:"allow,omitempty"`
	Deny    []string `json:"deny,omitempty"`
}

type InstallRecord struct {
	ID            string   `json:"id"`
	Enabled       bool     `json:"enabled"`
	Source        string   `json:"source"`
	CatalogSource string   `json:"catalog_source,omitempty"`
	InstallDir    string   `json:"install_dir,omitempty"`
	InstalledAt   string   `json:"installed_at"`
	Manifest      Manifest `json:"manifest"`
}

type Diagnostic struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	Source  string `json:"source,omitempty"`
}

func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		if err == nil {
			err = os.ErrNotExist
		}
		return Paths{}, err
	}
	root := filepath.Join(home, ".si", "plugins")
	return Paths{
		RootDir:     root,
		InstallsDir: filepath.Join(root, "installed"),
		StateFile:   filepath.Join(root, "state.json"),
		CatalogFile: filepath.Join(root, "catalog.json"),
		CatalogDir:  filepath.Join(root, "catalog.d"),
	}, nil
}

func DefaultState() State {
	return State{
		SchemaVersion: SchemaVersion,
		Policy:        DefaultPolicy(),
		Installs:      map[string]InstallRecord{},
	}
}

func DefaultPolicy() Policy {
	return Policy{
		Enabled: true,
		Allow:   nil,
		Deny:    nil,
	}
}

func normalizeManifest(manifest *Manifest) {
	manifest.ID = strings.TrimSpace(manifest.ID)
	manifest.Namespace = strings.TrimSpace(manifest.Namespace)
	manifest.Name = strings.TrimSpace(manifest.Name)
	manifest.Version = strings.TrimSpace(manifest.Version)
	manifest.Summary = strings.TrimSpace(manifest.Summary)
	manifest.Description = strings.TrimSpace(manifest.Description)
	manifest.Homepage = strings.TrimSpace(manifest.Homepage)
	manifest.TermsURL = strings.TrimSpace(manifest.TermsURL)
	manifest.PrivacyURL = strings.TrimSpace(manifest.PrivacyURL)
	manifest.License = strings.TrimSpace(manifest.License)
	manifest.Maturity = strings.ToLower(strings.TrimSpace(manifest.Maturity))
	manifest.Kind = strings.ToLower(strings.TrimSpace(manifest.Kind))
	manifest.Install.Type = strings.ToLower(strings.TrimSpace(manifest.Install.Type))
	manifest.Install.Source = strings.TrimSpace(manifest.Install.Source)
	if manifest.Install.Type == "" {
		manifest.Install.Type = InstallTypeNone
	}
	if manifest.SchemaVersion == 0 {
		manifest.SchemaVersion = SchemaVersion
	}
	if manifest.Namespace == "" {
		manifest.Namespace = NamespaceFromID(manifest.ID)
	}
	manifest.Integration.ProviderIDs = normalizeStringList(manifest.Integration.ProviderIDs)
	manifest.Integration.Commands = normalizeStringList(manifest.Integration.Commands)
	manifest.Integration.Capabilities = normalizeStringList(manifest.Integration.Capabilities)
	if len(manifest.Integration.MCPServers) > 0 {
		servers := make([]MCPServer, 0, len(manifest.Integration.MCPServers))
		for _, server := range manifest.Integration.MCPServers {
			server.Name = strings.TrimSpace(server.Name)
			server.Transport = strings.ToLower(strings.TrimSpace(server.Transport))
			server.Endpoint = strings.TrimSpace(server.Endpoint)
			if len(server.Command) > 0 {
				server.Command = normalizeStringList(server.Command)
			}
			servers = append(servers, server)
		}
		manifest.Integration.MCPServers = servers
	}
}

func normalizeStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func normalizePolicy(policy *Policy) {
	if policy == nil {
		return
	}
	policy.Allow = normalizeStringList(policy.Allow)
	policy.Deny = normalizeStringList(policy.Deny)
}

func NamespaceFromID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	parts := strings.Split(id, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[0]
}

func ValidatePluginID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("plugin id required")
	}
	parts := strings.Split(id, "/")
	if len(parts) != 2 {
		return fmt.Errorf("plugin id must be namespaced as <namespace>/<name>")
	}
	for _, part := range parts {
		if !pluginIDSegmentPattern.MatchString(part) {
			return fmt.Errorf("invalid plugin id segment %q", part)
		}
		if part == "." || part == ".." {
			return fmt.Errorf("invalid plugin id segment %q", part)
		}
	}
	return nil
}

func ValidatePolicySelector(selector string) error {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return fmt.Errorf("policy selector required")
	}
	if wildcardPolicyPattern.MatchString(selector) {
		return nil
	}
	return ValidatePluginID(selector)
}

func ValidateManifest(manifest Manifest) error {
	normalizeManifest(&manifest)
	if err := ValidatePluginID(manifest.ID); err != nil {
		return err
	}
	ns := NamespaceFromID(manifest.ID)
	if manifest.Namespace != "" && manifest.Namespace != ns {
		return fmt.Errorf("namespace %q does not match id namespace %q", manifest.Namespace, ns)
	}
	if manifest.SchemaVersion < 1 {
		return fmt.Errorf("schema_version must be >= 1")
	}
	if !maturityValues[manifest.Maturity] {
		return fmt.Errorf("unsupported maturity %q", manifest.Maturity)
	}
	if !installTypeValues[manifest.Install.Type] {
		return fmt.Errorf("unsupported install.type %q", manifest.Install.Type)
	}
	if manifest.Install.Type == InstallTypeLocalPath && manifest.Install.Source == "" {
		return fmt.Errorf("install.source required for install.type=%s", InstallTypeLocalPath)
	}
	if manifest.Install.Type == InstallTypeMCPHTTP {
		if manifest.Install.Source == "" && len(manifest.Integration.MCPServers) == 0 {
			return fmt.Errorf("install.source or integration.mcp_servers required for install.type=%s", InstallTypeMCPHTTP)
		}
	}
	if err := validateOptionalURL(manifest.Homepage, "homepage"); err != nil {
		return err
	}
	if err := validateOptionalURL(manifest.TermsURL, "terms_url"); err != nil {
		return err
	}
	if err := validateOptionalURL(manifest.PrivacyURL, "privacy_url"); err != nil {
		return err
	}
	for _, server := range manifest.Integration.MCPServers {
		if strings.TrimSpace(server.Name) == "" {
			return fmt.Errorf("integration.mcp_servers.name required")
		}
		switch server.Transport {
		case "stdio":
			if len(server.Command) == 0 {
				return fmt.Errorf("integration.mcp_servers[%s].command required for stdio transport", server.Name)
			}
		case "http", "sse":
			if err := validateOptionalURL(server.Endpoint, "integration.mcp_servers.endpoint"); err != nil {
				return err
			}
			if strings.TrimSpace(server.Endpoint) == "" {
				return fmt.Errorf("integration.mcp_servers[%s].endpoint required for %s transport", server.Name, server.Transport)
			}
		default:
			return fmt.Errorf("unsupported integration.mcp_servers[%s].transport %q", server.Name, server.Transport)
		}
	}
	for _, providerID := range manifest.Integration.ProviderIDs {
		if providerID == "" {
			return fmt.Errorf("integration.provider_ids must not contain empty values")
		}
	}
	return nil
}

func validateOptionalURL(raw string, field string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", field, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("invalid %s: absolute URL required", field)
	}
	return nil
}

func ParseManifestBytes(raw []byte) (Manifest, error) {
	manifest := Manifest{}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, err
	}
	normalizeManifest(&manifest)
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func ReadManifestFromPath(path string) (Manifest, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Manifest{}, "", fmt.Errorf("path required")
	}
	resolved, err := cleanAbsPath(path)
	if err != nil {
		return Manifest{}, "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return Manifest{}, "", err
	}
	manifestPath := ""
	rootDir := ""
	switch {
	case info.IsDir():
		rootDir = resolved
		manifestPath = filepath.Join(rootDir, ManifestFileName)
	case info.Mode().IsRegular():
		manifestPath = resolved
		rootDir = filepath.Dir(resolved)
	default:
		return Manifest{}, "", fmt.Errorf("unsupported path type: %s", resolved)
	}
	raw, err := os.ReadFile(manifestPath) // #nosec G304 -- operator-provided path.
	if err != nil {
		return Manifest{}, "", fmt.Errorf("read manifest %s: %w", manifestPath, err)
	}
	manifest, err := ParseManifestBytes(raw)
	if err != nil {
		return Manifest{}, "", fmt.Errorf("parse manifest %s: %w", manifestPath, err)
	}
	return manifest, rootDir, nil
}

func cleanAbsPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path required")
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(cwd, path)), nil
}

func archiveKind(path string) string {
	lower := strings.ToLower(strings.TrimSpace(path))
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return "zip"
	case strings.HasSuffix(lower, ".tgz"), strings.HasSuffix(lower, ".tar.gz"):
		return "targz"
	case strings.HasSuffix(lower, ".tar"):
		return "tar"
	default:
		return ""
	}
}

func extractZIP(zipPath string, destDir string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		target, err := secureArchiveTargetPath(destDir, file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive contains symlink entry: %s", file.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.Mode().Perm())
		if err != nil {
			_ = in.Close()
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			_ = in.Close()
			return err
		}
		if err := out.Close(); err != nil {
			_ = in.Close()
			return err
		}
		if err := in.Close(); err != nil {
			return err
		}
	}
	return nil
}

func extractTarball(path string, destDir string, compressed bool) error {
	file, err := os.Open(path) // #nosec G304 -- caller passes a validated archive path.
	if err != nil {
		return err
	}
	defer file.Close()
	var reader io.Reader = file
	if compressed {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer gzReader.Close()
		reader = gzReader
	}
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target, err := secureArchiveTargetPath(destDir, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode).Perm())
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tarReader); err != nil {
				_ = out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("archive contains unsupported link entry: %s", header.Name)
		default:
			return fmt.Errorf("archive contains unsupported entry type for %s", header.Name)
		}
	}
	return nil
}

func secureArchiveTargetPath(destDir string, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("archive entry name is empty")
	}
	cleanName := filepath.Clean(name)
	if cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) || filepath.IsAbs(cleanName) {
		return "", fmt.Errorf("archive entry escapes destination: %s", name)
	}
	target := filepath.Join(destDir, cleanName)
	rel, err := filepath.Rel(filepath.Clean(destDir), filepath.Clean(target))
	if err != nil {
		return "", err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("archive entry escapes destination: %s", name)
	}
	return target, nil
}

func findManifestRoot(baseDir string) (string, Manifest, error) {
	manifestPaths := make([]string, 0)
	err := filepath.WalkDir(baseDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.EqualFold(entry.Name(), ManifestFileName) {
			manifestPaths = append(manifestPaths, path)
		}
		return nil
	})
	if err != nil {
		return "", Manifest{}, err
	}
	if len(manifestPaths) == 0 {
		return "", Manifest{}, fmt.Errorf("archive missing %s", ManifestFileName)
	}
	sort.Slice(manifestPaths, func(i, j int) bool { return len(manifestPaths[i]) < len(manifestPaths[j]) })
	manifestPath := manifestPaths[0]
	raw, err := os.ReadFile(manifestPath) // #nosec G304 -- manifest path is derived from extracted archive walk.
	if err != nil {
		return "", Manifest{}, err
	}
	manifest, err := ParseManifestBytes(raw)
	if err != nil {
		return "", Manifest{}, err
	}
	return filepath.Dir(manifestPath), manifest, nil
}

func LoadCatalog(paths Paths) (Catalog, []Diagnostic, error) {
	catalogs := make([]Catalog, 0, 4)
	diagnostics := make([]Diagnostic, 0)

	builtinCatalog, builtinDiagnostics, err := loadCatalogFromRaw("builtin", builtinCatalogRaw)
	if err != nil {
		return Catalog{}, nil, fmt.Errorf("load built-in catalog: %w", err)
	}
	diagnostics = append(diagnostics, builtinDiagnostics...)
	catalogs = append(catalogs, builtinCatalog)

	if userCatalog, userDiagnostics, err := loadOptionalCatalogFile(paths.CatalogFile); err == nil {
		catalogs = append(catalogs, userCatalog)
		diagnostics = append(diagnostics, userDiagnostics...)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Catalog{}, nil, fmt.Errorf("load catalog %s: %w", paths.CatalogFile, err)
	}

	if entries, err := os.ReadDir(paths.CatalogDir); err == nil {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := strings.TrimSpace(entry.Name())
			if strings.HasSuffix(strings.ToLower(name), ".json") {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			path := filepath.Join(paths.CatalogDir, name)
			catalog, fileDiagnostics, err := loadOptionalCatalogFile(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return Catalog{}, nil, fmt.Errorf("load catalog %s: %w", path, err)
			}
			catalogs = append(catalogs, catalog)
			diagnostics = append(diagnostics, fileDiagnostics...)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Catalog{}, nil, fmt.Errorf("read catalog dir %s: %w", paths.CatalogDir, err)
	}
	for _, envPath := range ParseCatalogPathList(os.Getenv(CatalogPathsEnv)) {
		envCatalogs, envDiagnostics, err := loadCatalogPathSource(envPath)
		if err != nil {
			return Catalog{}, nil, fmt.Errorf("load env catalog path %s: %w", envPath, err)
		}
		catalogs = append(catalogs, envCatalogs...)
		diagnostics = append(diagnostics, envDiagnostics...)
	}

	merged, mergeDiagnostics := MergeCatalogs(catalogs...)
	diagnostics = append(diagnostics, mergeDiagnostics...)
	return merged, diagnostics, nil
}

func ParseCatalogPathList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	splitter := func(r rune) bool {
		if r == ',' || r == ';' {
			return true
		}
		return r == rune(os.PathListSeparator)
	}
	parts := strings.FieldsFunc(raw, splitter)
	return normalizeStringList(parts)
}

func DiscoverManifestPaths(source string) ([]string, error) {
	resolved, err := cleanAbsPath(source)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		paths := make([]string, 0)
		err := filepath.WalkDir(resolved, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if strings.EqualFold(entry.Name(), ManifestFileName) {
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		sort.Strings(paths)
		if len(paths) == 0 {
			return nil, fmt.Errorf("no %s files found in %s", ManifestFileName, resolved)
		}
		return paths, nil
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("unsupported source type: %s", resolved)
	}
	if !strings.EqualFold(filepath.Base(resolved), ManifestFileName) {
		return nil, fmt.Errorf("source file must be %s, got %s", ManifestFileName, resolved)
	}
	return []string{resolved}, nil
}

func BuildCatalogFromSource(source string, options BuildCatalogOptions) (Catalog, []Diagnostic, error) {
	paths, err := DiscoverManifestPaths(source)
	if err != nil {
		return Catalog{}, nil, err
	}
	channel := strings.TrimSpace(options.Channel)
	if channel == "" {
		channel = "community"
	}
	addedAt := strings.TrimSpace(options.AddedAt)
	if addedAt == "" {
		addedAt = time.Now().UTC().Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", addedAt); err != nil {
		return Catalog{}, nil, fmt.Errorf("invalid added_at date %q (expected YYYY-MM-DD)", addedAt)
	}
	tags := normalizeStringList(options.Tags)
	diagnostics := make([]Diagnostic, 0)
	entries := make([]CatalogEntry, 0, len(paths))
	seen := map[string]string{}
	for _, path := range paths {
		manifest, _, err := ReadManifestFromPath(path)
		if err != nil {
			return Catalog{}, diagnostics, err
		}
		if previous, ok := seen[manifest.ID]; ok {
			diagnostics = append(diagnostics, Diagnostic{
				Level:   "warn",
				Source:  path,
				Message: fmt.Sprintf("duplicate plugin id %q skipped (already loaded from %s)", manifest.ID, previous),
			})
			continue
		}
		seen[manifest.ID] = path
		entry := CatalogEntry{
			Manifest: manifest,
			Channel:  channel,
			Verified: options.Verified,
			AddedAt:  addedAt,
			Tags:     tags,
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Manifest.ID < entries[j].Manifest.ID
	})
	return Catalog{SchemaVersion: SchemaVersion, Entries: entries}, diagnostics, nil
}

func loadCatalogPathSource(path string) ([]Catalog, []Diagnostic, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil, nil
	}
	resolved, err := cleanAbsPath(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, nil, err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(resolved)
		if err != nil {
			return nil, nil, err
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := strings.TrimSpace(entry.Name())
			if strings.HasSuffix(strings.ToLower(name), ".json") {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		catalogs := make([]Catalog, 0, len(names))
		diagnostics := make([]Diagnostic, 0)
		for _, name := range names {
			itemPath := filepath.Join(resolved, name)
			catalog, itemDiagnostics, err := loadOptionalCatalogFile(itemPath)
			if err != nil {
				return nil, nil, err
			}
			catalogs = append(catalogs, catalog)
			diagnostics = append(diagnostics, itemDiagnostics...)
		}
		return catalogs, diagnostics, nil
	}
	catalog, diagnostics, err := loadOptionalCatalogFile(resolved)
	if err != nil {
		return nil, nil, err
	}
	return []Catalog{catalog}, diagnostics, nil
}

func loadOptionalCatalogFile(path string) (Catalog, []Diagnostic, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- local operator-managed catalog file.
	if err != nil {
		return Catalog{}, nil, err
	}
	catalog, diagnostics, err := loadCatalogFromRaw(path, raw)
	if err != nil {
		return Catalog{}, nil, err
	}
	return catalog, diagnostics, nil
}

func loadCatalogFromRaw(source string, raw []byte) (Catalog, []Diagnostic, error) {
	decoded := Catalog{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Catalog{}, nil, err
	}
	if decoded.SchemaVersion == 0 {
		decoded.SchemaVersion = SchemaVersion
	}
	diagnostics := make([]Diagnostic, 0)
	entries := make([]CatalogEntry, 0, len(decoded.Entries))
	for _, entry := range decoded.Entries {
		normalizeManifest(&entry.Manifest)
		if err := ValidateManifest(entry.Manifest); err != nil {
			diagnostics = append(diagnostics, Diagnostic{
				Level:   "error",
				Source:  source,
				Message: fmt.Sprintf("catalog entry %q invalid: %v", entry.Manifest.ID, err),
			})
			continue
		}
		entry.Channel = strings.TrimSpace(entry.Channel)
		entry.AddedAt = strings.TrimSpace(entry.AddedAt)
		entry.Tags = normalizeStringList(entry.Tags)
		entry.Source = strings.TrimSpace(source)
		entries = append(entries, entry)
	}
	decoded.Entries = entries
	return decoded, diagnostics, nil
}

func MergeCatalogs(catalogs ...Catalog) (Catalog, []Diagnostic) {
	merged := Catalog{SchemaVersion: SchemaVersion, Entries: []CatalogEntry{}}
	diagnostics := make([]Diagnostic, 0)
	byID := map[string]CatalogEntry{}
	order := make([]string, 0)
	for _, catalog := range catalogs {
		for _, entry := range catalog.Entries {
			id := entry.Manifest.ID
			if id == "" {
				continue
			}
			if previous, ok := byID[id]; ok {
				prevSource := strings.TrimSpace(previous.Source)
				nextSource := strings.TrimSpace(entry.Source)
				if prevSource == "" {
					prevSource = "unknown"
				}
				if nextSource == "" {
					nextSource = "unknown"
				}
				diagnostics = append(diagnostics, Diagnostic{
					Level:   "warn",
					Source:  nextSource,
					Message: fmt.Sprintf("catalog entry %q overridden by higher precedence source (previous=%s)", id, prevSource),
				})
			} else {
				order = append(order, id)
			}
			byID[id] = entry
		}
	}
	sort.Strings(order)
	for _, id := range order {
		entry, ok := byID[id]
		if !ok {
			continue
		}
		merged.Entries = append(merged.Entries, entry)
	}
	return merged, diagnostics
}

func CatalogByID(catalog Catalog) map[string]CatalogEntry {
	result := make(map[string]CatalogEntry, len(catalog.Entries))
	for _, entry := range catalog.Entries {
		if entry.Manifest.ID == "" {
			continue
		}
		result[entry.Manifest.ID] = entry
	}
	return result
}

func LoadState(paths Paths) (State, error) {
	state := DefaultState()
	raw, err := os.ReadFile(paths.StateFile) // #nosec G304 -- local operator-managed state file.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, nil
		}
		return state, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return DefaultState(), err
	}
	if state.SchemaVersion == 0 {
		state.SchemaVersion = SchemaVersion
	}
	normalizePolicy(&state.Policy)
	if state.Installs == nil {
		state.Installs = map[string]InstallRecord{}
	}
	for id, record := range state.Installs {
		record.ID = strings.TrimSpace(record.ID)
		if record.ID == "" {
			record.ID = strings.TrimSpace(id)
		}
		if record.ID == "" {
			delete(state.Installs, id)
			continue
		}
		normalizeManifest(&record.Manifest)
		state.Installs[record.ID] = record
		if record.ID != id {
			delete(state.Installs, id)
		}
	}
	return state, nil
}

func SaveState(paths Paths, state State) error {
	if state.SchemaVersion == 0 {
		state.SchemaVersion = SchemaVersion
	}
	normalizePolicy(&state.Policy)
	if state.Installs == nil {
		state.Installs = map[string]InstallRecord{}
	}
	if err := os.MkdirAll(paths.RootDir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(paths.RootDir, "plugins-state-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, paths.StateFile)
}

func SaveUserCatalog(paths Paths, catalog Catalog) error {
	if catalog.SchemaVersion == 0 {
		catalog.SchemaVersion = SchemaVersion
	}
	if err := os.MkdirAll(paths.RootDir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(paths.RootDir, "plugins-catalog-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, paths.CatalogFile)
}

func UpsertUserCatalogEntry(paths Paths, entry CatalogEntry) error {
	normalizeManifest(&entry.Manifest)
	if err := ValidateManifest(entry.Manifest); err != nil {
		return err
	}
	catalog := Catalog{SchemaVersion: SchemaVersion, Entries: []CatalogEntry{}}
	if raw, err := os.ReadFile(paths.CatalogFile); err == nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &catalog); err != nil {
			return fmt.Errorf("parse %s: %w", paths.CatalogFile, err)
		}
	}
	if catalog.SchemaVersion == 0 {
		catalog.SchemaVersion = SchemaVersion
	}
	if catalog.Entries == nil {
		catalog.Entries = []CatalogEntry{}
	}
	found := false
	for i := range catalog.Entries {
		if catalog.Entries[i].Manifest.ID == entry.Manifest.ID {
			catalog.Entries[i] = entry
			found = true
			break
		}
	}
	if !found {
		catalog.Entries = append(catalog.Entries, entry)
	}
	sort.Slice(catalog.Entries, func(i, j int) bool {
		return catalog.Entries[i].Manifest.ID < catalog.Entries[j].Manifest.ID
	})
	return SaveUserCatalog(paths, catalog)
}

func InstallFromSource(paths Paths, sourcePath string, enabled bool, now time.Time) (InstallRecord, error) {
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return InstallRecord{}, fmt.Errorf("source path required")
	}
	resolvedSource, err := cleanAbsPath(sourcePath)
	if err != nil {
		return InstallRecord{}, err
	}
	info, err := os.Stat(resolvedSource)
	if err != nil {
		return InstallRecord{}, err
	}
	if info.IsDir() {
		return InstallFromPath(paths, resolvedSource, enabled, now)
	}
	if !info.Mode().IsRegular() {
		return InstallRecord{}, fmt.Errorf("unsupported source path type: %s", resolvedSource)
	}
	if archiveKind(resolvedSource) != "" {
		return InstallFromArchive(paths, resolvedSource, enabled, now)
	}
	return InstallFromPath(paths, resolvedSource, enabled, now)
}

func InstallFromPath(paths Paths, sourcePath string, enabled bool, now time.Time) (InstallRecord, error) {
	manifest, rootDir, err := ReadManifestFromPath(sourcePath)
	if err != nil {
		return InstallRecord{}, err
	}
	resolvedSource, err := cleanAbsPath(sourcePath)
	if err != nil {
		return InstallRecord{}, err
	}
	return installFromResolvedRoot(paths, manifest, rootDir, "path:"+resolvedSource, enabled, now)
}

func InstallFromArchive(paths Paths, archivePath string, enabled bool, now time.Time) (InstallRecord, error) {
	resolvedArchive, err := cleanAbsPath(archivePath)
	if err != nil {
		return InstallRecord{}, err
	}
	kind := archiveKind(resolvedArchive)
	if kind == "" {
		return InstallRecord{}, fmt.Errorf("unsupported archive: %s", resolvedArchive)
	}
	tmpDir, err := os.MkdirTemp("", "si-plugin-archive-*")
	if err != nil {
		return InstallRecord{}, err
	}
	defer os.RemoveAll(tmpDir)
	switch kind {
	case "zip":
		if err := extractZIP(resolvedArchive, tmpDir); err != nil {
			return InstallRecord{}, err
		}
	case "tar":
		if err := extractTarball(resolvedArchive, tmpDir, false); err != nil {
			return InstallRecord{}, err
		}
	case "targz":
		if err := extractTarball(resolvedArchive, tmpDir, true); err != nil {
			return InstallRecord{}, err
		}
	default:
		return InstallRecord{}, fmt.Errorf("unsupported archive: %s", resolvedArchive)
	}
	rootDir, manifest, err := findManifestRoot(tmpDir)
	if err != nil {
		return InstallRecord{}, err
	}
	return installFromResolvedRoot(paths, manifest, rootDir, "archive:"+resolvedArchive, enabled, now)
}

func installFromResolvedRoot(paths Paths, manifest Manifest, rootDir string, source string, enabled bool, now time.Time) (InstallRecord, error) {
	if err := os.MkdirAll(paths.InstallsDir, 0o700); err != nil {
		return InstallRecord{}, err
	}
	targetDir, err := resolveSafeInstallDir(paths.InstallsDir, manifest.ID)
	if err != nil {
		return InstallRecord{}, err
	}
	if err := os.RemoveAll(targetDir); err != nil {
		return InstallRecord{}, err
	}
	if err := copyDirectoryTree(rootDir, targetDir); err != nil {
		return InstallRecord{}, err
	}
	installedAt := now.UTC()
	if installedAt.IsZero() {
		installedAt = time.Now().UTC()
	}
	return InstallRecord{
		ID:          manifest.ID,
		Enabled:     enabled,
		Source:      source,
		InstallDir:  targetDir,
		InstalledAt: installedAt.Format(time.RFC3339),
		Manifest:    manifest,
	}, nil
}

func InstallFromCatalog(paths Paths, entry CatalogEntry, enabled bool, now time.Time) (InstallRecord, error) {
	normalizeManifest(&entry.Manifest)
	if err := ValidateManifest(entry.Manifest); err != nil {
		return InstallRecord{}, err
	}
	if entry.Manifest.Install.Type == InstallTypeLocalPath {
		record, err := InstallFromPath(paths, entry.Manifest.Install.Source, enabled, now)
		if err != nil {
			return InstallRecord{}, err
		}
		record.Source = "catalog:" + entry.Manifest.ID
		record.CatalogSource = entry.Manifest.Install.Source
		return record, nil
	}
	installedAt := now.UTC()
	if installedAt.IsZero() {
		installedAt = time.Now().UTC()
	}
	return InstallRecord{
		ID:            entry.Manifest.ID,
		Enabled:       enabled,
		Source:        "catalog:" + entry.Manifest.ID,
		CatalogSource: entry.Manifest.Install.Source,
		InstalledAt:   installedAt.Format(time.RFC3339),
		Manifest:      entry.Manifest,
	}, nil
}

func RemoveInstallDir(paths Paths, dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	resolvedBase := filepath.Clean(paths.InstallsDir)
	resolvedTarget := filepath.Clean(dir)
	rel, err := filepath.Rel(resolvedBase, resolvedTarget)
	if err != nil {
		return err
	}
	if rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("unsafe install dir removal blocked: %s", dir)
	}
	return os.RemoveAll(resolvedTarget)
}

func resolveSafeInstallDir(baseDir string, id string) (string, error) {
	resolvedBase := filepath.Clean(strings.TrimSpace(baseDir))
	if resolvedBase == "" {
		return "", fmt.Errorf("base dir required")
	}
	if err := ValidatePluginID(id); err != nil {
		return "", err
	}
	target := filepath.Join(resolvedBase, safeDirName(id))
	resolvedTarget := filepath.Clean(target)
	rel, err := filepath.Rel(resolvedBase, resolvedTarget)
	if err != nil {
		return "", err
	}
	if rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("invalid plugin name: path traversal detected")
	}
	return resolvedTarget, nil
}

func safeDirName(id string) string {
	id = strings.TrimSpace(id)
	replacer := strings.NewReplacer("/", "__", `\\`, "__")
	id = replacer.Replace(id)
	id = strings.ReplaceAll(id, " ", "-")
	id = strings.ReplaceAll(id, "..", "-")
	if id == "" {
		return "plugin"
	}
	return id
}

func copyDirectoryTree(src, dst string) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == src {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to copy symlink in plugin source: %s", path)
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported file type in plugin source: %s", path)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	in, err := os.Open(src) // #nosec G304 -- source file path from validated directory walk.
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm()) // #nosec G304 -- destination path derived from validated target dir.
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return nil
}

func ResolveEnableState(id string, record InstallRecord, policy Policy) (bool, string) {
	id = strings.TrimSpace(id)
	if !policy.Enabled {
		return false, "blocked: plugins policy disabled"
	}
	if matchPolicySelectors(policy.Deny, id) {
		return false, "blocked: denylist"
	}
	if len(policy.Allow) > 0 && !matchPolicySelectors(policy.Allow, id) {
		return false, "blocked: not in allowlist"
	}
	if !record.Enabled {
		return false, "blocked: install record disabled"
	}
	return true, "enabled"
}

func matchPolicySelectors(selectors []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, selector := range selectors {
		selector = strings.TrimSpace(selector)
		if selector == "" {
			continue
		}
		if selector == target {
			return true
		}
		if strings.HasSuffix(selector, "/*") {
			namespace := strings.TrimSuffix(selector, "/*")
			if namespace != "" && strings.HasPrefix(target, namespace+"/") {
				return true
			}
		}
	}
	return false
}

func containsString(items []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, item := range items {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}

func Doctor(catalog Catalog, state State, paths Paths) []Diagnostic {
	diagnostics := make([]Diagnostic, 0)
	catalogByID := CatalogByID(catalog)
	overlap := overlapStrings(state.Policy.Allow, state.Policy.Deny)
	for _, id := range overlap {
		diagnostics = append(diagnostics, Diagnostic{
			Level:   "warn",
			Message: fmt.Sprintf("policy overlap: %q is in both allow and deny lists; deny takes precedence", id),
		})
	}
	for _, id := range state.Policy.Allow {
		if err := ValidatePolicySelector(id); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Level: "error", Message: fmt.Sprintf("policy allow id %q invalid: %v", id, err)})
		}
	}
	for _, id := range state.Policy.Deny {
		if err := ValidatePolicySelector(id); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Level: "error", Message: fmt.Sprintf("policy deny id %q invalid: %v", id, err)})
		}
	}
	for id, record := range state.Installs {
		if err := ValidatePluginID(id); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Level: "error", Message: fmt.Sprintf("installed id %q invalid: %v", id, err)})
			continue
		}
		if err := ValidateManifest(record.Manifest); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Level: "error", Message: fmt.Sprintf("installed manifest %q invalid: %v", id, err)})
		}
		if record.Manifest.ID != "" && record.Manifest.ID != id {
			diagnostics = append(diagnostics, Diagnostic{Level: "error", Message: fmt.Sprintf("installed manifest id mismatch for %q (manifest=%q)", id, record.Manifest.ID)})
		}
		if _, ok := catalogByID[id]; !ok {
			diagnostics = append(diagnostics, Diagnostic{Level: "warn", Message: fmt.Sprintf("installed plugin %q is not present in merged catalog", id)})
		}
		if strings.TrimSpace(record.InstallDir) != "" {
			if err := verifyInstallDirWithin(paths.InstallsDir, record.InstallDir); err != nil {
				diagnostics = append(diagnostics, Diagnostic{Level: "error", Message: fmt.Sprintf("install dir for %q invalid: %v", id, err), Source: record.InstallDir})
				continue
			}
			manifestPath := filepath.Join(record.InstallDir, ManifestFileName)
			if _, err := os.Stat(manifestPath); err != nil {
				diagnostics = append(diagnostics, Diagnostic{Level: "error", Message: fmt.Sprintf("install dir for %q missing %s", id, ManifestFileName), Source: manifestPath})
			}
		}
		if enabled, reason := ResolveEnableState(id, record, state.Policy); !enabled {
			diagnostics = append(diagnostics, Diagnostic{
				Level:   "info",
				Message: fmt.Sprintf("plugin %q inactive: %s", id, reason),
			})
		}
	}
	if len(state.Installs) == 0 {
		diagnostics = append(diagnostics, Diagnostic{Level: "info", Message: "no plugins installed"})
	}
	if len(catalog.Entries) == 0 {
		diagnostics = append(diagnostics, Diagnostic{Level: "warn", Message: "catalog has no entries"})
	}
	return diagnostics
}

func verifyInstallDirWithin(baseDir, targetDir string) error {
	resolvedBase := filepath.Clean(baseDir)
	resolvedTarget := filepath.Clean(targetDir)
	rel, err := filepath.Rel(resolvedBase, resolvedTarget)
	if err != nil {
		return err
	}
	if rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("path escapes installs dir")
	}
	return nil
}

func overlapStrings(a []string, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	set := map[string]bool{}
	for _, item := range a {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		set[item] = true
	}
	overlap := make([]string, 0)
	for _, item := range b {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if set[item] {
			overlap = append(overlap, item)
		}
	}
	sort.Strings(overlap)
	return normalizeStringList(overlap)
}

func ScaffoldManifest(id string) (Manifest, error) {
	id = strings.TrimSpace(id)
	if err := ValidatePluginID(id); err != nil {
		return Manifest{}, err
	}
	namespace := NamespaceFromID(id)
	name := strings.Split(id, "/")[1]
	manifest := Manifest{
		SchemaVersion: SchemaVersion,
		ID:            id,
		Namespace:     namespace,
		Name:          strings.ReplaceAll(name, "-", " "),
		Version:       "0.1.0",
		Summary:       "Describe what this integration/plugin adds.",
		Maturity:      "experimental",
		Kind:          "integration",
		Install: InstallSpec{
			Type: InstallTypeNone,
		},
		Integration: IntegrationSpec{
			Commands: []string{"si plugins install " + id},
			MCPServers: []MCPServer{
				{
					Name:      "replace_me",
					Transport: "http",
					Endpoint:  "https://example.invalid/mcp",
				},
			},
			Capabilities: []string{"replace-with-capability"},
		},
	}
	normalizeManifest(&manifest)
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func EncodeManifest(manifest Manifest) ([]byte, error) {
	normalizeManifest(&manifest)
	if err := ValidateManifest(manifest); err != nil {
		return nil, err
	}
	return json.MarshalIndent(manifest, "", "  ")
}
