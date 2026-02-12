package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"si/tools/si/internal/vault"
)

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
	return vault.ResolveTarget(vault.ResolveOptions{
		CWD:              "",
		File:             fileFlag,
		DefaultFile:      vaultDefaultEnvFile(settings),
		AllowMissingFile: allowMissingFile,
	})
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
