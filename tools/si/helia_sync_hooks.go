package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func maybeHeliaAutoSyncProfile(source string, profile codexProfile) {
	settings := loadSettingsOrDefault()
	if !settings.Helia.AutoSync {
		return
	}
	if strings.TrimSpace(profile.ID) == "" {
		return
	}
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		warnf("helia auto-sync skipped (%s): %v", source, err)
		return
	}
	authPath, err := codexProfileAuthPath(profile)
	if err != nil {
		warnf("helia auto-sync skipped (%s): %v", source, err)
		return
	}
	if err := codexAuthValidationError(authPath); err != nil {
		warnf("helia auto-sync skipped (%s): %v", source, err)
		return
	}
	// #nosec G304 -- authPath resolves from local profile location.
	authBytes, err := os.ReadFile(authPath)
	if err != nil {
		warnf("helia auto-sync skipped (%s): %v", source, err)
		return
	}
	bundle := heliaCodexProfileBundle{
		ID:       profile.ID,
		Name:     profile.Name,
		Email:    profile.Email,
		AuthJSON: authBytes,
		SyncedAt: time.Now().UTC().Format(time.RFC3339),
	}
	payload, err := json.Marshal(bundle)
	if err != nil {
		warnf("helia auto-sync skipped (%s): %v", source, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := client.putObject(ctx, heliaCodexProfileBundleKind, profile.ID, payload, "application/json", map[string]interface{}{
		"profile_id": profile.ID,
		"name":       profile.Name,
		"email":      profile.Email,
		"source":     source,
	}, nil); err != nil {
		warnf("helia auto-sync failed (%s): %v", source, err)
		return
	}
	infof("helia auto-sync complete for profile %s (%s)", profile.ID, source)
}

func maybeHeliaAutoBackupVault(source string, vaultPath string) {
	settings := loadSettingsOrDefault()
	if !settings.Helia.AutoSync {
		return
	}
	vaultPath = expandTilde(strings.TrimSpace(vaultPath))
	if vaultPath == "" {
		return
	}
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		warnf("helia vault auto-backup skipped (%s): %v", source, err)
		return
	}
	data, err := os.ReadFile(vaultPath)
	if err != nil {
		warnf("helia vault auto-backup skipped (%s): %v", source, err)
		return
	}
	name := strings.TrimSpace(settings.Helia.VaultBackup)
	if name == "" {
		name = "default"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := client.putObject(ctx, heliaVaultBackupKind, name, data, "text/plain", map[string]interface{}{
		"path":   filepath.Base(vaultPath),
		"source": source,
	}, nil); err != nil {
		warnf("helia vault auto-backup failed (%s): %v", source, err)
		return
	}
	infof("helia vault auto-backup complete (%s)", source)
}
