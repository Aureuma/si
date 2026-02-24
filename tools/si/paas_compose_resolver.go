package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"si/tools/si/internal/vault"

	"gopkg.in/yaml.v3"
)

const paasComposeFilesManifestName = "compose.files"
const paasComposeRuntimeEnvFileName = ".env"

var paasMagicVariablePattern = regexp.MustCompile(`\{\{\s*paas\.[a-zA-Z0-9_.-]+\s*\}\}`)
var paasComposeInterpolationVariablePattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)[^}]*\}`)
var paasNamespacedSecretEnvPattern = regexp.MustCompile(`^PAAS__CTX_[A-Z0-9_]+__NS_[A-Z0-9_]+__APP_[A-Z0-9_]+__TARGET_[A-Z0-9_]+__VAR_([A-Z0-9_]+)$`)

type paasComposePrepareOptions struct {
	App         string
	ReleaseID   string
	ComposeFile string
	Strategy    string
	Targets     []string
}

type paasComposeAddonArtifact struct {
	Name    string
	Pack    string
	Content []byte
}

type paasComposePrepareResult struct {
	ResolvedComposePath string
	ComposeFiles        []string
	MagicVariables      map[string]string
	AddonArtifacts      []paasComposeAddonArtifact
}

func preparePaasComposeForDeploy(opts paasComposePrepareOptions) (paasComposePrepareResult, error) {
	composePath := filepath.Clean(strings.TrimSpace(opts.ComposeFile))
	if composePath == "" {
		return paasComposePrepareResult{}, fmt.Errorf("compose file path is required")
	}
	rawCompose, err := os.ReadFile(composePath) // #nosec G304 -- CLI operator input path.
	if err != nil {
		return paasComposePrepareResult{}, err
	}
	magicVars := buildPaasMagicVariableMap(opts.App, opts.ReleaseID, opts.Strategy, opts.Targets)
	resolvedBase, err := resolvePaasMagicVariables(rawCompose, magicVars)
	if err != nil {
		return paasComposePrepareResult{}, err
	}
	baseSections, err := collectPaasComposeSectionKeys(resolvedBase)
	if err != nil {
		return paasComposePrepareResult{}, fmt.Errorf("invalid base compose YAML: %w", err)
	}
	aggregateSections := clonePaasComposeSectionKeys(baseSections)

	addons, err := resolvePaasAddonsForApp(opts.App)
	if err != nil {
		return paasComposePrepareResult{}, err
	}
	artifacts := make([]paasComposeAddonArtifact, 0, len(addons))
	composeFiles := []string{"compose.yaml"}
	for _, addon := range addons {
		if strings.TrimSpace(addon.FragmentPath) == "" {
			continue
		}
		rawFragment, err := os.ReadFile(addon.FragmentPath) // #nosec G304 -- path loaded from context-scoped addon store.
		if err != nil {
			return paasComposePrepareResult{}, fmt.Errorf("read addon fragment %q: %w", addon.Name, err)
		}
		resolvedFragment, err := resolvePaasMagicVariables(rawFragment, magicVars)
		if err != nil {
			return paasComposePrepareResult{}, fmt.Errorf("resolve addon fragment %q: %w", addon.Name, err)
		}
		fragmentSections, err := collectPaasComposeSectionKeys(resolvedFragment)
		if err != nil {
			return paasComposePrepareResult{}, fmt.Errorf("invalid addon fragment %q: %w", addon.Name, err)
		}
		if err := validatePaasComposeAdditiveMerge(aggregateSections, fragmentSections, addon); err != nil {
			return paasComposePrepareResult{}, err
		}
		mergePaasComposeSectionKeys(aggregateSections, fragmentSections)
		fileName := "compose.addon." + sanitizePaasReleasePathSegment(addon.Name) + ".yaml"
		composeFiles = append(composeFiles, fileName)
		artifacts = append(artifacts, paasComposeAddonArtifact{
			Name:    fileName,
			Pack:    addon.Pack,
			Content: resolvedFragment,
		})
	}
	resolvedPath, err := writePaasResolvedComposeTempFile(resolvedBase)
	if err != nil {
		return paasComposePrepareResult{}, err
	}
	return paasComposePrepareResult{
		ResolvedComposePath: resolvedPath,
		ComposeFiles:        composeFiles,
		MagicVariables:      magicVars,
		AddonArtifacts:      artifacts,
	}, nil
}

func materializePaasComposeBundleArtifacts(bundleDir string, prepared paasComposePrepareResult) error {
	composeFiles := prepared.ComposeFiles
	if len(composeFiles) == 0 {
		composeFiles = []string{"compose.yaml"}
	}
	for _, artifact := range prepared.AddonArtifacts {
		if strings.TrimSpace(artifact.Name) == "" {
			continue
		}
		targetPath := filepath.Join(strings.TrimSpace(bundleDir), strings.TrimSpace(artifact.Name))
		if err := os.WriteFile(targetPath, artifact.Content, 0o600); err != nil {
			return err
		}
	}
	manifest := strings.Join(composeFiles, "\n") + "\n"
	manifestPath := filepath.Join(strings.TrimSpace(bundleDir), paasComposeFilesManifestName)
	return os.WriteFile(manifestPath, []byte(manifest), 0o600)
}

func readPaasComposeFilesManifest(bundleDir string) ([]string, error) {
	manifestPath := filepath.Join(strings.TrimSpace(bundleDir), paasComposeFilesManifestName)
	raw, err := os.ReadFile(manifestPath) // #nosec G304 -- bundle path derived from local state root.
	if err != nil {
		if os.IsNotExist(err) {
			return []string{"compose.yaml"}, nil
		}
		return nil, err
	}
	lines := strings.Split(string(raw), "\n")
	files := make([]string, 0, len(lines))
	seen := map[string]struct{}{}
	for _, line := range lines {
		item := filepath.Clean(strings.TrimSpace(line))
		if item == "" || item == "." {
			continue
		}
		if strings.Contains(item, string(filepath.Separator)) || strings.Contains(item, "/") || strings.Contains(item, "\\") {
			return nil, fmt.Errorf("invalid compose file entry %q in %s", item, manifestPath)
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		files = append(files, item)
	}
	if len(files) == 0 {
		files = []string{"compose.yaml"}
	}
	if _, ok := seen["compose.yaml"]; !ok {
		files = append([]string{"compose.yaml"}, files...)
	}
	return files, nil
}

func resolvePaasBundleUploadFiles(bundleDir string) ([]string, error) {
	entries, err := os.ReadDir(strings.TrimSpace(bundleDir)) // #nosec G304 -- bundle path derived from local state root.
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" {
			continue
		}
		files = append(files, name)
	}
	sort.Strings(files)
	return files, nil
}

func materializePaasComposeRuntimeEnv(bundleDir string, composeFiles []string, env []string) (int, error) {
	referenced, err := collectPaasComposeInterpolationVariables(bundleDir, composeFiles)
	if err != nil {
		return 0, err
	}
	if len(referenced) == 0 {
		return 0, nil
	}
	available := map[string]string{}
	projected := map[string]string{}
	for _, pair := range env {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		available[key] = value
		if match := paasNamespacedSecretEnvPattern.FindStringSubmatch(key); len(match) == 2 {
			candidate := strings.TrimSpace(match[1])
			if candidate != "" {
				if _, exists := projected[candidate]; !exists {
					projected[candidate] = value
				}
			}
		}
	}
	// Direct env keys win over projected namespaced aliases.
	for key, value := range projected {
		if _, ok := available[key]; !ok {
			available[key] = value
		}
	}
	lines := make([]string, 0, len(referenced))
	for _, key := range referenced {
		value, ok := available[key]
		if !ok {
			continue
		}
		lines = append(lines, key+"="+vault.RenderDotenvValuePlain(value))
	}
	envPath := filepath.Join(strings.TrimSpace(bundleDir), paasComposeRuntimeEnvFileName)
	if len(lines) == 0 {
		if err := os.Remove(envPath); err != nil && !os.IsNotExist(err) {
			return 0, err
		}
		return 0, nil
	}
	payload := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(envPath, []byte(payload), 0o600); err != nil {
		return 0, err
	}
	return len(lines), nil
}

func collectPaasComposeInterpolationVariables(bundleDir string, composeFiles []string) ([]string, error) {
	files := composeFiles
	if len(files) == 0 {
		files = []string{"compose.yaml"}
	}
	seen := map[string]struct{}{}
	for _, file := range files {
		item := strings.TrimSpace(file)
		if item == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(strings.TrimSpace(bundleDir), item)) // #nosec G304 -- bundle path derived from local state root.
		if err != nil {
			return nil, err
		}
		matches := paasComposeInterpolationVariablePattern.FindAllStringSubmatch(string(raw), -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			key := strings.TrimSpace(match[1])
			if key == "" {
				continue
			}
			seen[key] = struct{}{}
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

func buildPaasComposeFileArgs(composeFiles []string) string {
	files := composeFiles
	if len(files) == 0 {
		files = []string{"compose.yaml"}
	}
	parts := make([]string, 0, len(files)*2)
	for _, file := range files {
		item := strings.TrimSpace(file)
		if item == "" {
			continue
		}
		parts = append(parts, "-f", quoteSingle(item))
	}
	return strings.Join(parts, " ")
}

func buildPaasComposeExecCommand(composeFiles []string, tail string) string {
	args := buildPaasComposeFileArgs(composeFiles)
	suffix := strings.TrimSpace(tail)
	if suffix != "" {
		suffix = " " + suffix
	}
	return fmt.Sprintf("if docker compose version >/dev/null 2>&1; then docker compose %s%s; elif command -v docker-compose >/dev/null 2>&1; then docker-compose %s%s; else echo 'docker compose and docker-compose are unavailable' >&2; exit 127; fi", args, suffix, args, suffix)
}

func buildPaasComposePullCommand(composeFiles []string) string {
	args := buildPaasComposeFileArgs(composeFiles)
	return fmt.Sprintf("if docker compose version >/dev/null 2>&1; then (docker compose %s pull --ignore-buildable 2>/dev/null || docker compose %s pull) || echo 'WARNING: compose pull failed; continuing with local/build images' >&2; elif command -v docker-compose >/dev/null 2>&1; then docker-compose %s pull || echo 'WARNING: compose pull failed; continuing with local/build images' >&2; else echo 'docker compose and docker-compose are unavailable' >&2; exit 127; fi", args, args, args)
}

func buildPaasDefaultHealthCheckCommand(composeFiles []string) string {
	return fmt.Sprintf("%s | grep -q .", buildPaasComposeExecCommand(composeFiles, "ps --status running --services"))
}

func buildPaasMagicVariableMap(app, releaseID, strategy string, targets []string) map[string]string {
	resolvedTargets := append([]string(nil), targets...)
	sort.Strings(resolvedTargets)
	return map[string]string{
		"SI_PAAS_APP":       strings.TrimSpace(app),
		"SI_PAAS_CONTEXT":   currentPaasContext(),
		"SI_PAAS_RELEASE":   strings.TrimSpace(releaseID),
		"SI_PAAS_TARGETS":   strings.Join(resolvedTargets, ","),
		"SI_PAAS_STRATEGY":  strings.TrimSpace(strategy),
		"SI_PAAS_TIMESTAMP": time.Now().UTC().Format(time.RFC3339),
	}
}

func resolvePaasMagicVariables(content []byte, vars map[string]string) ([]byte, error) {
	text := string(content)
	replacerPairs := []string{
		"${SI_PAAS_APP}", vars["SI_PAAS_APP"],
		"${SI_PAAS_CONTEXT}", vars["SI_PAAS_CONTEXT"],
		"${SI_PAAS_RELEASE}", vars["SI_PAAS_RELEASE"],
		"${SI_PAAS_TARGETS}", vars["SI_PAAS_TARGETS"],
		"${SI_PAAS_STRATEGY}", vars["SI_PAAS_STRATEGY"],
		"${SI_PAAS_TIMESTAMP}", vars["SI_PAAS_TIMESTAMP"],
		"{{paas.app}}", vars["SI_PAAS_APP"],
		"{{paas.context}}", vars["SI_PAAS_CONTEXT"],
		"{{paas.release}}", vars["SI_PAAS_RELEASE"],
		"{{paas.targets}}", vars["SI_PAAS_TARGETS"],
		"{{paas.strategy}}", vars["SI_PAAS_STRATEGY"],
		"{{paas.timestamp}}", vars["SI_PAAS_TIMESTAMP"],
	}
	replacer := strings.NewReplacer(replacerPairs...)
	resolved := replacer.Replace(text)
	if remaining := paasMagicVariablePattern.FindString(resolved); strings.TrimSpace(remaining) != "" {
		return nil, fmt.Errorf("unresolved magic variable placeholder %q", strings.TrimSpace(remaining))
	}
	return []byte(resolved), nil
}

func resolvePaasAddonsForApp(app string) ([]paasAddonRecord, error) {
	store, err := loadPaasAddonStore()
	if err != nil {
		return nil, err
	}
	appKey := sanitizePaasReleasePathSegment(app)
	rows := append([]paasAddonRecord(nil), store.Apps[appKey]...)
	sort.SliceStable(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
	return rows, nil
}

func writePaasResolvedComposeTempFile(content []byte) (string, error) {
	file, err := os.CreateTemp("", "si-paas-compose-*.yaml")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func collectPaasComposeSectionKeys(content []byte) (map[string]map[string]struct{}, error) {
	var root map[string]any
	if err := yaml.Unmarshal(content, &root); err != nil {
		return nil, err
	}
	sections := map[string]map[string]struct{}{}
	for _, section := range []string{"services", "volumes", "networks", "configs", "secrets"} {
		sectionMap := map[string]struct{}{}
		if raw, ok := root[section]; ok && raw != nil {
			typed, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("compose section %q must be a map", section)
			}
			for key := range typed {
				name := strings.TrimSpace(key)
				if name == "" {
					continue
				}
				sectionMap[name] = struct{}{}
			}
		}
		sections[section] = sectionMap
	}
	return sections, nil
}

func clonePaasComposeSectionKeys(in map[string]map[string]struct{}) map[string]map[string]struct{} {
	out := map[string]map[string]struct{}{}
	for section, keys := range in {
		copyKeys := map[string]struct{}{}
		for key := range keys {
			copyKeys[key] = struct{}{}
		}
		out[section] = copyKeys
	}
	return out
}

func mergePaasComposeSectionKeys(dst, src map[string]map[string]struct{}) {
	for section, keys := range src {
		target := dst[section]
		if target == nil {
			target = map[string]struct{}{}
			dst[section] = target
		}
		for key := range keys {
			target[key] = struct{}{}
		}
	}
}

func validatePaasComposeAdditiveMerge(baseSections, fragmentSections map[string]map[string]struct{}, addon paasAddonRecord) error {
	for _, section := range []string{"services", "volumes", "networks", "configs", "secrets"} {
		base := baseSections[section]
		frag := fragmentSections[section]
		for key := range frag {
			if _, exists := base[key]; !exists {
				continue
			}
			return fmt.Errorf("addon %q merge conflict in section %q for key %q (merge strategy %s forbids overriding existing keys)",
				strings.TrimSpace(addon.Name),
				section,
				key,
				firstNonEmptyString(strings.TrimSpace(addon.MergeStrategy), paasAddonMergeStrategyAdditiveNoOverride),
			)
		}
	}
	return nil
}
