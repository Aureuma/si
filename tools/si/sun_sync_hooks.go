package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"
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
