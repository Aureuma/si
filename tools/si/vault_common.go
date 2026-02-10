package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"si/tools/si/internal/vault"
)

func vaultKeyConfigFromSettings(settings Settings) vault.KeyConfig {
	backend := strings.TrimSpace(os.Getenv("SI_VAULT_KEY_BACKEND"))
	if backend == "" {
		backend = settings.Vault.KeyBackend
	}
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

func vaultResolveTarget(settings Settings, fileFlag, vaultDirFlag string, allowMissingVaultDir, allowMissingFile bool) (vault.Target, error) {
	return vault.ResolveTarget(vault.ResolveOptions{
		CWD:                  "",
		File:                 fileFlag,
		VaultDir:             vaultDirFlag,
		DefaultVaultDir:      settings.Vault.Dir,
		AllowMissingVaultDir: allowMissingVaultDir,
		AllowMissingFile:     allowMissingFile,
	})
}

func vaultRepoURL(target vault.Target) string {
	if target.VaultDir == "" || !vault.IsDir(target.VaultDir) {
		return ""
	}
	url, err := vault.GitRemoteOriginURL(target.VaultDir)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(url)
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
	currentURL := vaultRepoURL(target)
	if entry.VaultRepo != "" && currentURL != "" && strings.TrimSpace(entry.VaultRepo) != strings.TrimSpace(currentURL) {
		return "", fmt.Errorf("vault repo URL changed for %s: run `si vault trust status --file %s` and `si vault trust accept --file %s`", filepath.Clean(target.File), shellSingleQuote(filepath.Clean(target.File)), shellSingleQuote(filepath.Clean(target.File)))
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
		"vaultDir": strings.TrimSpace(target.VaultDir),
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
