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

func sunVaultObjectName(_ Settings) string {
	return "default"
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
	_ = settings
	return true
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

// vaultHydrateFromSun is intentionally a no-op in Sun remote vault mode.
// Legacy implementations attempted to materialize a local vault file from
// backup objects, which reintroduced local disk dependency and permission
// failures. Sun mode now reads/writes keys directly from/to Sun KV only.
func vaultHydrateFromSun(settings Settings, target vault.Target, allowMissingFile bool) error {
	backend, err := resolveVaultSyncBackend(settings)
	if err != nil {
		return err
	}
	strict := vaultSyncBackendStrictSun(settings, backend)
	_ = target
	_ = allowMissingFile
	if err := vaultEnsureSunIdentityEnv(settings, "vault_target_resolve"); err != nil {
		if strict {
			return err
		}
		warnf("%v", err)
	}
	return nil
}
