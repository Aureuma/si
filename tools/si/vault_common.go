package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"si/tools/si/internal/vault"
)

const (
	vaultSyncBackendSun = "sun"
)

type vaultSyncBackendResolution struct {
	Mode   string
	Source string
}

func vaultRefuseNonInteractiveOSKeyring(keyCfg vault.KeyConfig) error {
	// In non-interactive environments (CI/VPS), OS keychains can block on prompts.
	// Prefer SI_VAULT_IDENTITY(_FILE) or file backend for deterministic behavior.
	if isInteractiveTerminal() {
		return nil
	}
	if strings.TrimSpace(os.Getenv("SI_VAULT_IDENTITY")) != "" ||
		strings.TrimSpace(os.Getenv("SI_VAULT_PRIVATE_KEY")) != "" ||
		strings.TrimSpace(os.Getenv("SI_VAULT_IDENTITY_FILE")) != "" {
		return nil
	}
	if vault.NormalizeKeyBackend(keyCfg.Backend) == "keyring" {
		return fmt.Errorf("non-interactive: refusing to access OS keychain/keyring (set SI_VAULT_IDENTITY/SI_VAULT_IDENTITY_FILE or use vault.key_backend=\"file\")")
	}
	return nil
}

func vaultKeyConfigFromSettings(settings Settings) vault.KeyConfig {
	backend := strings.TrimSpace(os.Getenv("SI_VAULT_KEY_BACKEND"))
	if backend == "" {
		backend = settings.Vault.KeyBackend
	}
	backend = vault.NormalizeKeyBackend(backend)
	keyFile := strings.TrimSpace(os.Getenv("SI_VAULT_KEY_FILE"))
	if keyFile == "" {
		keyFile = settings.Vault.KeyFile
	}
	return vault.KeyConfig{Backend: backend, KeyFile: keyFile}
}

func vaultRecipientsForWrite(settings Settings, doc vault.DotenvFile, source string) ([]string, error) {
	backend, err := resolveVaultSyncBackend(settings)
	if err != nil {
		return nil, err
	}
	if backend.Mode == vaultSyncBackendSun {
		identity, err := vaultEnsureStrictSunIdentity(settings, source)
		if err != nil {
			return nil, err
		}
		if identity == nil {
			return nil, fmt.Errorf("sun vault identity sync failed (%s): missing identity", source)
		}
		return []string{strings.TrimSpace(identity.Recipient().String())}, nil
	}
	return vault.ParseRecipientsFromDotenv(doc), nil
}

func normalizeVaultSyncBackend(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "sun", "cloud", "dual", "both":
		// Backward-compatible aliases all resolve to strict Sun mode.
		return vaultSyncBackendSun
	case "git", "local":
		// Legacy git/local modes now map to Sun-only mode.
		return vaultSyncBackendSun
	default:
		return ""
	}
}

func resolveVaultSyncBackend(settings Settings) (vaultSyncBackendResolution, error) {
	if envRaw := strings.TrimSpace(os.Getenv("SI_VAULT_SYNC_BACKEND")); envRaw != "" {
		mode := normalizeVaultSyncBackend(envRaw)
		if mode == "" {
			return vaultSyncBackendResolution{}, fmt.Errorf("invalid SI_VAULT_SYNC_BACKEND %q (expected sun)", envRaw)
		}
		return vaultSyncBackendResolution{Mode: mode, Source: "env"}, nil
	}
	if cfgRaw := strings.TrimSpace(settings.Vault.SyncBackend); cfgRaw != "" {
		mode := normalizeVaultSyncBackend(cfgRaw)
		if mode == "" {
			return vaultSyncBackendResolution{}, fmt.Errorf("invalid vault.sync_backend %q (expected sun)", cfgRaw)
		}
		return vaultSyncBackendResolution{Mode: mode, Source: "settings"}, nil
	}
	return vaultSyncBackendResolution{Mode: vaultSyncBackendSun, Source: "default"}, nil
}

func vaultTrustStorePath(settings Settings) string {
	path := strings.TrimSpace(os.Getenv("SI_VAULT_TRUST_STORE"))
	if path == "" {
		path = settings.Vault.TrustStore
	}
	return path
}

func vaultAuditLogPath(settings Settings) string {
	path := strings.TrimSpace(os.Getenv("SI_VAULT_AUDIT_LOG"))
	if path == "" {
		path = settings.Vault.AuditLog
	}
	return path
}

func vaultDefaultEnvFile(settings Settings) string {
	// Allows per-invocation override without changing settings.
	if path := strings.TrimSpace(os.Getenv("SI_VAULT_FILE")); path != "" {
		return path
	}
	return strings.TrimSpace(settings.Vault.File)
}

func vaultResolveTarget(settings Settings, fileFlag string, allowMissingFile bool) (vault.Target, error) {
	_, err := resolveVaultSyncBackend(settings)
	if err != nil {
		return vault.Target{}, err
	}
	// Sun-backed mode may hydrate the local file from cloud after resolution.
	resolveAllowMissing := true
	target, err := vault.ResolveTarget(vault.ResolveOptions{
		CWD:              "",
		File:             fileFlag,
		DefaultFile:      vaultDefaultEnvFile(settings),
		AllowMissingFile: resolveAllowMissing,
	})
	if err != nil {
		return vault.Target{}, err
	}
	if err := vaultHydrateFromSun(settings, target, allowMissingFile); err != nil {
		return vault.Target{}, err
	}
	if !allowMissingFile {
		if _, statErr := os.Stat(target.File); statErr != nil {
			return vault.Target{}, statErr
		}
	}
	if shouldEnforceVaultRepoScope(settings) {
		if err := vaultValidateImplicitTargetRepoScope(target); err != nil && isTruthyFlagValue(os.Getenv("SI_VAULT_STRICT_TARGET_SCOPE")) {
			return vault.Target{}, err
		}
	}
	return target, nil
}

func vaultResolveTargetStatus(settings Settings, fileFlag string) (vault.Target, error) {
	return vaultResolveTarget(settings, fileFlag, true)
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

func vaultAuditSink(settings Settings) vault.AuditSink {
	return vault.NewJSONLAudit(vaultAuditLogPath(settings))
}

func vaultAuditEvent(settings Settings, target vault.Target, typ string, fields map[string]any) {
	sink := vaultAuditSink(settings)
	if sink == nil {
		return
	}
	event := map[string]any{
		"type":     strings.TrimSpace(typ),
		"user":     strings.TrimSpace(os.Getenv("USER")),
		"uid":      os.Getuid(),
		"gid":      os.Getgid(),
		"repoRoot": strings.TrimSpace(target.RepoRoot),
		"file":     strings.TrimSpace(target.File),
	}
	for k, v := range fields {
		if strings.TrimSpace(k) == "" {
			continue
		}
		event[k] = v
	}
	sink.Log(event)
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
