package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"si/tools/si/internal/vault"
)

func maybeSunAutoSyncProfile(source string, profile codexProfile) {
	settings := loadSettingsOrDefault()
	if !settings.Sun.AutoSync {
		return
	}
	if strings.TrimSpace(profile.ID) == "" {
		return
	}
	client, err := sunClientFromSettings(settings)
	if err != nil {
		warnf("sun auto-sync skipped (%s): %v", source, err)
		return
	}
	authPath, err := codexProfileAuthPath(profile)
	if err != nil {
		warnf("sun auto-sync skipped (%s): %v", source, err)
		return
	}
	if err := codexAuthValidationError(authPath); err != nil {
		warnf("sun auto-sync skipped (%s): %v", source, err)
		return
	}
	// #nosec G304 -- authPath resolves from local profile location.
	authBytes, err := os.ReadFile(authPath)
	if err != nil {
		warnf("sun auto-sync skipped (%s): %v", source, err)
		return
	}
	bundle := sunCodexProfileBundle{
		ID:       profile.ID,
		Name:     profile.Name,
		Email:    profile.Email,
		AuthJSON: authBytes,
		SyncedAt: time.Now().UTC().Format(time.RFC3339),
	}
	payload, err := json.Marshal(bundle)
	if err != nil {
		warnf("sun auto-sync skipped (%s): %v", source, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := client.putObject(ctx, sunCodexProfileBundleKind, profile.ID, payload, "application/json", map[string]interface{}{
		"profile_id": profile.ID,
		"name":       profile.Name,
		"email":      profile.Email,
		"source":     source,
	}, nil); err != nil {
		warnf("sun auto-sync failed (%s): %v", source, err)
		return
	}
	infof("sun auto-sync complete for profile %s (%s)", profile.ID, source)
}

func maybeSunAutoBackupVault(source string, vaultPath string) error {
	settings := loadSettingsOrDefault()
	backend, err := resolveVaultSyncBackend(settings)
	if err != nil {
		return err
	}
	if backend.Mode == vaultSyncBackendGit {
		return nil
	}
	requireSunBackend := backend.Mode == vaultSyncBackendSun
	vaultPath = expandTilde(strings.TrimSpace(vaultPath))
	if vaultPath == "" {
		if requireSunBackend {
			return fmt.Errorf("sun vault auto-backup failed (%s): vault file path required in sun backend mode", source)
		}
		return nil
	}
	client, err := sunClientFromSettings(settings)
	if err != nil {
		if requireSunBackend {
			return fmt.Errorf("sun vault auto-backup failed (%s): %w", source, err)
		} else {
			warnf("sun vault auto-backup skipped (%s): %v", source, err)
		}
		return nil
	}
	doc, err := vault.ReadDotenvFile(vaultPath)
	if err != nil {
		if requireSunBackend {
			return fmt.Errorf("sun vault auto-backup failed (%s): %w", source, err)
		} else {
			warnf("sun vault auto-backup skipped (%s): %v", source, err)
		}
		return nil
	}
	scan, err := vault.ScanDotenvEncryption(doc)
	if err != nil {
		if requireSunBackend {
			return fmt.Errorf("sun vault auto-backup failed (%s): %w", source, err)
		} else {
			warnf("sun vault auto-backup skipped (%s): %v", source, err)
		}
		return nil
	}
	if len(scan.PlaintextKeys) > 0 {
		if requireSunBackend {
			return fmt.Errorf("sun vault auto-backup failed (%s): plaintext keys detected (%d); run `si vault encrypt`", source, len(scan.PlaintextKeys))
		} else {
			warnf("sun vault auto-backup skipped (%s): plaintext keys detected (%d); run `si vault encrypt`", source, len(scan.PlaintextKeys))
		}
		return nil
	}
	data := doc.Bytes()
	name := strings.TrimSpace(settings.Sun.VaultBackup)
	if name == "" {
		name = "default"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := client.putObject(ctx, sunVaultBackupKind, name, data, "text/plain", map[string]interface{}{
		"path":   filepath.Base(vaultPath),
		"source": source,
	}, nil); err != nil {
		if requireSunBackend {
			return fmt.Errorf("sun vault auto-backup failed (%s): %w", source, err)
		} else {
			warnf("sun vault auto-backup skipped (%s): %v", source, err)
		}
		return nil
	}
	infof("sun vault auto-backup complete (%s)", source)
	return nil
}
