package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"si/tools/si/internal/vault"
)

const (
	vaultSyncBackendFort = "fort"
)

var deprecatedSIVaultIdentityEnvVars = []string{
	"SI_VAULT_IDENTITY",
	"SI_VAULT_PRIVATE_KEY",
	"SI_VAULT_IDENTITY_FILE",
}

var windowsDrivePrefixPattern = regexp.MustCompile(`^[a-zA-Z]:[\\/]`)

type vaultSyncBackendResolution struct {
	Mode   string
	Source string
}

func normalizeVaultSyncBackend(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), vaultSyncBackendFort) {
		return vaultSyncBackendFort
	}
	return ""
}

func resolveVaultSyncBackend(settings Settings) (vaultSyncBackendResolution, error) {
	if envRaw := strings.TrimSpace(os.Getenv("SI_VAULT_SYNC_BACKEND")); envRaw != "" {
		mode := normalizeVaultSyncBackend(envRaw)
		if mode == "" {
			return vaultSyncBackendResolution{}, fmt.Errorf("invalid SI_VAULT_SYNC_BACKEND %q (expected fort)", envRaw)
		}
		return vaultSyncBackendResolution{Mode: mode, Source: "env"}, nil
	}
	if cfgRaw := strings.TrimSpace(settings.Vault.SyncBackend); cfgRaw != "" {
		mode := normalizeVaultSyncBackend(cfgRaw)
		if mode == "" {
			return vaultSyncBackendResolution{}, fmt.Errorf("invalid vault.sync_backend %q (expected fort)", cfgRaw)
		}
		return vaultSyncBackendResolution{Mode: mode, Source: "settings"}, nil
	}
	return vaultSyncBackendResolution{Mode: vaultSyncBackendFort, Source: "default"}, nil
}

func vaultTrustStorePath(settings Settings) string {
	path := strings.TrimSpace(os.Getenv("SI_VAULT_TRUST_STORE"))
	if path == "" {
		path = settings.Vault.TrustStore
	}
	return path
}

func vaultDefaultEnvFile(settings Settings) string {
	if scope := strings.TrimSpace(os.Getenv("SI_VAULT_SCOPE")); scope != "" {
		return scope
	}
	// Keep SI_VAULT_FILE for backward compatibility with existing automation.
	if file := strings.TrimSpace(os.Getenv("SI_VAULT_FILE")); file != "" {
		return file
	}

	configured := strings.TrimSpace(settings.Vault.File)
	if configured == "" {
		return defaultSIVaultDotenvFile
	}
	// Legacy scope values are not valid fort/local env-file paths.
	if !vaultLooksLikeLegacyPath(configured) &&
		!strings.Contains(configured, "/") &&
		!strings.Contains(configured, "\\") &&
		!strings.HasPrefix(strings.ToLower(configured), ".env") {
		return defaultSIVaultDotenvFile
	}
	return configured
}

func vaultLooksLikeLegacyPath(normalizedLower string) bool {
	normalizedLower = strings.TrimSpace(strings.ReplaceAll(normalizedLower, "\\", "/"))
	if normalizedLower == "" {
		return false
	}
	if strings.HasPrefix(normalizedLower, "/") || strings.HasPrefix(normalizedLower, "~") {
		return true
	}
	if windowsDrivePrefixPattern.MatchString(normalizedLower) {
		return true
	}
	base := strings.TrimSpace(strings.ToLower(filepath.Base(normalizedLower)))
	if base == ".env" || base == "default.env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".env") {
		return true
	}
	return false
}

func vaultResolveTarget(settings Settings, fileFlag string, allowMissingFile bool) (vault.Target, error) {
	fileValue := strings.TrimSpace(fileFlag)
	if fileValue == "" {
		fileValue = vaultDefaultEnvFile(settings)
	}
	target, err := vault.ResolveTarget(vault.ResolveOptions{
		CWD:              "",
		File:             fileValue,
		DefaultFile:      vaultDefaultEnvFile(settings),
		AllowMissingFile: allowMissingFile,
	})
	if err != nil {
		return vault.Target{}, err
	}
	_ = allowMissingFile
	if shouldEnforceVaultRepoScope(settings) {
		if err := vaultValidateImplicitTargetRepoScope(target); err != nil && isTruthyFlagValue(os.Getenv("SI_VAULT_STRICT_TARGET_SCOPE")) {
			return vault.Target{}, err
		}
	}
	return target, nil
}

// vaultContainerEnvFileMountPath resolves the host vault env file path to bind
// into containers. Returns empty when unresolved or missing.
func vaultContainerEnvFileMountPath(settings Settings) string {
	target, err := vaultResolveTarget(settings, "", true)
	if err != nil {
		return ""
	}
	path := filepath.Clean(strings.TrimSpace(target.File))
	if path == "" {
		return ""
	}
	info, statErr := os.Stat(path)
	if statErr != nil || !info.Mode().IsRegular() {
		return ""
	}
	return path
}

func vaultTrustFingerprint(doc vault.DotenvFile) (string, error) {
	recipients := vault.ParseRecipientsFromDotenv(doc)
	if len(recipients) == 0 {
		return "", fmt.Errorf("no recipients found (expected %q lines)", vault.VaultRecipientPrefix)
	}
	return vault.RecipientsFingerprint(recipients), nil
}

func vaultRequireTrusted(settings Settings, target vault.Target, doc vault.DotenvFile) (string, error) {
	fp, err := vaultTrustFingerprint(doc)
	if err != nil {
		return "", err
	}
	storePath := vaultTrustStorePath(settings)
	store, err := vault.LoadTrustStore(storePath)
	if err != nil {
		return "", err
	}
	entry, ok := store.Find(target.RepoRoot, target.File)
	if !ok {
		return "", fmt.Errorf("vault trust not established for %s: run `si vault trust accept --file %s`", filepath.Clean(target.File), shellSingleQuote(filepath.Clean(target.File)))
	}
	if strings.TrimSpace(entry.Fingerprint) != fp {
		return "", fmt.Errorf("vault trust fingerprint changed for %s: run `si vault trust status --file %s` and `si vault trust accept --file %s`", filepath.Clean(target.File), shellSingleQuote(filepath.Clean(target.File)), shellSingleQuote(filepath.Clean(target.File)))
	}
	return fp, nil
}

func warnIfDeprecatedSIVaultIdentityEnvSet() {
	for _, key := range deprecatedSIVaultIdentityEnvVars {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			continue
		}
		warnf("%s is deprecated and ignored; configure vault identity via settings (vault.key_backend/vault.key_file)", key)
	}
}

func vaultValidateImplicitTargetRepoScope(target vault.Target) error {
	if target.FileIsExplicit {
		return nil
	}
	if isTruthyFlagValue(os.Getenv("SI_VAULT_ALLOW_CROSS_REPO")) {
		return nil
	}
	cwdRepoRoot := ""
	if cwd, err := os.Getwd(); err == nil {
		if gitRoot, gitErr := vault.GitRoot(cwd); gitErr == nil {
			cwdRepoRoot = gitRoot
		}
	}
	if strings.TrimSpace(cwdRepoRoot) == "" {
		if siRepoRoot, err := repoRoot(); err == nil {
			cwdRepoRoot = siRepoRoot
		}
	}
	if strings.TrimSpace(cwdRepoRoot) == "" {
		// Not in a repo layout where scope can be inferred.
		return nil
	}
	targetRepoRoot := filepath.Clean(strings.TrimSpace(target.RepoRoot))
	if targetRepoRoot == "" {
		// Target file is outside a git repo, so no cross-repo ambiguity to guard.
		return nil
	}
	targetRepoRoot = absPathOrSelf(targetRepoRoot)
	cwdRepoRoot = absPathOrSelf(filepath.Clean(strings.TrimSpace(cwdRepoRoot)))
	targetFile := absPathOrSelf(filepath.Clean(strings.TrimSpace(target.File)))
	if !isPathWithin(targetFile, targetRepoRoot) {
		// Target repo root came from cwd fallback (file not inside a git repo).
		return nil
	}
	if cwdRepoRoot == targetRepoRoot {
		return nil
	}
	return fmt.Errorf(
		"vault default file %s resolves to repo %s while current repo is %s; pass --file explicitly, run `si vault use --file <path>`, or set SI_VAULT_ALLOW_CROSS_REPO=1",
		filepath.Clean(strings.TrimSpace(target.File)),
		targetRepoRoot,
		cwdRepoRoot,
	)
}

func shouldEnforceVaultRepoScope(settings Settings) bool {
	_ = settings
	return true
}

func absPathOrSelf(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(abs)
	}
	if resolved, evalErr := filepath.EvalSymlinks(abs); evalErr == nil && strings.TrimSpace(resolved) != "" {
		return filepath.Clean(resolved)
	}
	parent := filepath.Dir(abs)
	if parentResolved, evalErr := filepath.EvalSymlinks(parent); evalErr == nil && strings.TrimSpace(parentResolved) != "" {
		return filepath.Clean(filepath.Join(parentResolved, filepath.Base(abs)))
	}
	return abs
}
