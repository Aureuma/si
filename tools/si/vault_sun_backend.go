package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"si/tools/si/internal/vault"
)

func sunVaultObjectName(settings Settings) string {
	name := strings.TrimSpace(settings.Sun.VaultBackup)
	if name == "" {
		name = "default"
	}
	return name
}

func isSunNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(err.Error())), "status 404")
}

func vaultSyncBackendStrictSun(settings Settings, resolution vaultSyncBackendResolution) bool {
	if resolution.Mode != vaultSyncBackendSun {
		return false
	}
	if resolution.Source == "env" || resolution.Source == "settings" {
		return true
	}
	token := firstNonEmpty(envSunToken(), strings.TrimSpace(settings.Sun.Token))
	return strings.TrimSpace(token) != ""
}

func vaultLocalFileExists(path string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

func vaultEnsureSunIdentityEnv(settings Settings, source string) error {
	if strings.TrimSpace(os.Getenv("SI_VAULT_IDENTITY")) != "" ||
		strings.TrimSpace(os.Getenv("SI_VAULT_PRIVATE_KEY")) != "" ||
		strings.TrimSpace(os.Getenv("SI_VAULT_IDENTITY_FILE")) != "" {
		return nil
	}
	backend, err := resolveVaultSyncBackend(settings)
	if err != nil {
		return err
	}
	strict := vaultSyncBackendStrictSun(settings, backend)

	client, err := sunClientFromSettings(settings)
	if err != nil {
		if strict {
			return fmt.Errorf("sun vault identity sync failed (%s): %w", source, err)
		}
		warnf("sun vault identity sync skipped (%s): %v", source, err)
		return nil
	}

	name := sunVaultObjectName(settings)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	payload, err := client.getPayload(ctx, sunVaultIdentityKind, name)
	if err != nil {
		// Missing remote identity is valid during first-time bootstrap.
		if isSunNotFoundError(err) {
			return nil
		}
		if strict {
			return fmt.Errorf("sun vault identity sync failed (%s): %w", source, err)
		}
		warnf("sun vault identity sync skipped (%s): %v", source, err)
		return nil
	}

	secret := strings.TrimSpace(string(payload))
	if secret == "" {
		if strict {
			return fmt.Errorf("sun vault identity sync failed (%s): empty identity payload", source)
		}
		warnf("sun vault identity sync skipped (%s): empty identity payload", source)
		return nil
	}
	if _, err := age.ParseX25519Identity(secret); err != nil {
		if strict {
			return fmt.Errorf("sun vault identity sync failed (%s): invalid age identity: %w", source, err)
		}
		warnf("sun vault identity sync skipped (%s): invalid age identity: %v", source, err)
		return nil
	}
	if err := os.Setenv("SI_VAULT_IDENTITY", secret); err != nil {
		if strict {
			return fmt.Errorf("sun vault identity sync failed (%s): %w", source, err)
		}
		warnf("sun vault identity sync skipped (%s): %v", source, err)
	}
	return nil
}

func vaultPersistIdentityToSun(settings Settings, identity *age.X25519Identity, source string) error {
	if identity == nil {
		return nil
	}
	backend, err := resolveVaultSyncBackend(settings)
	if err != nil {
		return err
	}
	strict := vaultSyncBackendStrictSun(settings, backend)

	client, err := sunClientFromSettings(settings)
	if err != nil {
		if strict {
			return fmt.Errorf("sun vault identity upload failed (%s): %w", source, err)
		}
		warnf("sun vault identity upload skipped (%s): %v", source, err)
		return nil
	}

	name := sunVaultObjectName(settings)
	payload := []byte(strings.TrimSpace(identity.String()) + "\n")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err = client.putObject(ctx, sunVaultIdentityKind, name, payload, "text/plain", map[string]interface{}{
		"source":    source,
		"recipient": strings.TrimSpace(identity.Recipient().String()),
	}, nil)
	if err != nil {
		if strict {
			return fmt.Errorf("sun vault identity upload failed (%s): %w", source, err)
		}
		warnf("sun vault identity upload skipped (%s): %v", source, err)
		return nil
	}
	return nil
}

func vaultEnsureStrictSunIdentity(settings Settings, source string) (*age.X25519Identity, error) {
	backend, err := resolveVaultSyncBackend(settings)
	if err != nil {
		return nil, err
	}
	if backend.Mode != vaultSyncBackendSun {
		return nil, nil
	}
	if !vaultSyncBackendStrictSun(settings, backend) {
		info, _, err := vault.EnsureIdentity(vaultKeyConfigFromSettings(settings))
		if err != nil {
			return nil, err
		}
		if info == nil || info.Identity == nil {
			return nil, fmt.Errorf("sun vault identity sync failed (%s): missing identity", source)
		}
		_ = os.Setenv("SI_VAULT_IDENTITY", strings.TrimSpace(info.Identity.String()))
		return info.Identity, nil
	}

	client, err := sunClientFromSettings(settings)
	if err != nil {
		return nil, fmt.Errorf("sun vault identity sync failed (%s): %w", source, err)
	}

	name := sunVaultObjectName(settings)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	payload, err := client.getPayload(ctx, sunVaultIdentityKind, name)
	if err != nil {
		if !isSunNotFoundError(err) {
			return nil, fmt.Errorf("sun vault identity sync failed (%s): %w", source, err)
		}
		identity, genErr := vault.GenerateIdentity()
		if genErr != nil {
			return nil, fmt.Errorf("sun vault identity sync failed (%s): generate identity: %w", source, genErr)
		}
		if setErr := os.Setenv("SI_VAULT_IDENTITY", strings.TrimSpace(identity.String())); setErr != nil {
			return nil, fmt.Errorf("sun vault identity sync failed (%s): %w", source, setErr)
		}
		if persistErr := vaultPersistIdentityToSun(settings, identity, source); persistErr != nil {
			return nil, persistErr
		}
		return identity, nil
	}

	secret := strings.TrimSpace(string(payload))
	if secret == "" {
		return nil, fmt.Errorf("sun vault identity sync failed (%s): empty identity payload", source)
	}
	identity, err := age.ParseX25519Identity(secret)
	if err != nil {
		return nil, fmt.Errorf("sun vault identity sync failed (%s): invalid age identity: %w", source, err)
	}
	if err := os.Setenv("SI_VAULT_IDENTITY", secret); err != nil {
		return nil, fmt.Errorf("sun vault identity sync failed (%s): %w", source, err)
	}
	return identity, nil
}

func vaultHydrateFromSun(settings Settings, target vault.Target, allowMissingFile bool) error {
	backend, err := resolveVaultSyncBackend(settings)
	if err != nil {
		return err
	}
	strict := vaultSyncBackendStrictSun(settings, backend)
	path := filepath.Clean(strings.TrimSpace(target.File))
	localExists := vaultLocalFileExists(path)
	if err := vaultEnsureSunIdentityEnv(settings, "vault_target_resolve"); err != nil {
		if strict {
			if localExists {
				return nil
			}
			return err
		}
		warnf("%v", err)
	}

	client, err := sunClientFromSettings(settings)
	if err != nil {
		if strict {
			if localExists {
				return nil
			}
			return fmt.Errorf("sun vault sync failed: %w", err)
		}
		warnf("sun vault sync skipped: %v", err)
		return nil
	}

	name := sunVaultObjectName(settings)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	payload, err := client.getPayload(ctx, sunVaultBackupKind, name)
	if err != nil {
		if isSunNotFoundError(err) {
			return nil
		}
		if strict {
			return fmt.Errorf("sun vault sync failed: %w", err)
		}
		warnf("sun vault sync skipped: %v", err)
		return nil
	}

	if path == "" {
		return fmt.Errorf("sun vault sync failed: empty local vault path")
	}
	if err := writeFileAtomic0600(path, payload); err != nil {
		return fmt.Errorf("sun vault sync failed: write local vault file: %w", err)
	}
	if allowMissingFile {
		return nil
	}
	doc, err := vault.ReadDotenvFile(path)
	if err != nil {
		return fmt.Errorf("sun vault sync failed: read hydrated vault file: %w", err)
	}
	fp, err := vaultTrustFingerprint(doc)
	if err != nil {
		return nil
	}
	storePath := vaultTrustStorePath(settings)
	store, err := vault.LoadTrustStore(storePath)
	if err != nil {
		if strict {
			return err
		}
		warnf("sun vault trust sync skipped: %v", err)
		return nil
	}
	store.Upsert(vault.TrustEntry{
		RepoRoot:    target.RepoRoot,
		File:        target.File,
		Fingerprint: fp,
	})
	if err := store.Save(storePath); err != nil {
		if strict {
			return err
		}
		warnf("sun vault trust sync skipped: %v", err)
	}
	return nil
}
