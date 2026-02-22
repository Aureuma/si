package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const codexProfileLabelKey = "si.codex.profile"

type codexProfile struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type codexProfileSummary struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Email             string  `json:"email"`
	AuthCached        bool    `json:"auth_cached"`
	AuthPath          string  `json:"auth_path,omitempty"`
	AuthUpdated       string  `json:"auth_updated,omitempty"`
	StatusError       string  `json:"status_error,omitempty"`
	FiveHourLeftPct   float64 `json:"five_hour_left_pct,omitempty"`
	FiveHourReset     string  `json:"five_hour_reset,omitempty"`
	FiveHourRemaining int     `json:"five_hour_remaining_minutes,omitempty"`
	WeeklyLeftPct     float64 `json:"weekly_left_pct,omitempty"`
	WeeklyReset       string  `json:"weekly_reset,omitempty"`
	WeeklyRemaining   int     `json:"weekly_remaining_minutes,omitempty"`
}

func codexProfiles() []codexProfile {
	settings := loadSettingsOrDefault()
	entries := settings.Codex.Profiles.Entries
	if len(entries) == 0 {
		return nil
	}
	items := make([]codexProfile, 0, len(entries))
	for id, entry := range entries {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(entry.Name)
		email := strings.TrimSpace(entry.Email)
		items = append(items, codexProfile{
			ID:    id,
			Name:  name,
			Email: email,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
	return items
}

func codexProfileByKey(key string) (codexProfile, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return codexProfile{}, false
	}
	lower := strings.ToLower(key)
	normalized := normalizeCodexProfileKey(key)
	for _, profile := range codexProfiles() {
		if strings.EqualFold(profile.ID, key) || strings.EqualFold(profile.Email, key) {
			return profile, true
		}
		if normalized != "" && normalized == normalizeCodexProfileKey(profile.Name) {
			return profile, true
		}
		if lower == strings.ToLower(profile.ID) {
			return profile, true
		}
	}
	return codexProfile{}, false
}

func requireCodexProfile(key string) (codexProfile, error) {
	profile, ok := codexProfileByKey(key)
	if ok {
		return profile, nil
	}
	available := codexProfileIDs()
	if len(available) == 0 {
		return codexProfile{}, fmt.Errorf("unknown codex profile %q", key)
	}
	return codexProfile{}, fmt.Errorf("unknown codex profile %q (available: %s)", key, strings.Join(available, ", "))
}

func codexProfileIDs() []string {
	profiles := codexProfiles()
	out := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, profile.ID)
	}
	sort.Strings(out)
	return out
}

func normalizeCodexProfileKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.TrimLeftFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	value = strings.TrimSpace(value)
	return strings.ToLower(value)
}

func codexProfilesRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		if err == nil {
			err = fmt.Errorf("home dir not available")
		}
		return "", err
	}
	return filepath.Join(home, ".si", "codex", "profiles"), nil
}

func codexProfileDir(profile codexProfile) (string, error) {
	root, err := codexProfilesRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, profile.ID), nil
}

func codexProfileAuthPath(profile codexProfile) (string, error) {
	dir, err := codexProfileDir(profile)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "auth.json"), nil
}

func ensureCodexProfileDir(profile codexProfile) (string, error) {
	dir, err := codexProfileDir(profile)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

type codexAuthCacheStatus struct {
	Path     string
	Exists   bool
	Modified time.Time
	Reason   string
}

var (
	codexAuthSyncAttempts                sync.Map
	syncProfileAuthFromContainerStatusFn = syncProfileAuthFromContainer
)

func codexProfileAuthStatus(profile codexProfile) codexAuthCacheStatus {
	path, err := codexProfileAuthPath(profile)
	if err != nil {
		return codexAuthCacheStatus{}
	}
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() && isValidCodexAuthFile(path, time.Now()) {
		return codexAuthCacheStatus{Path: path, Exists: true, Modified: info.ModTime()}
	}
	if attemptRecoverCodexAuthCache(profile) {
		info, err = os.Stat(path)
		if err == nil && !info.IsDir() && isValidCodexAuthFile(path, time.Now()) {
			return codexAuthCacheStatus{Path: path, Exists: true, Modified: info.ModTime()}
		}
	}
	reason := codexAuthMissingReason(path, info, err)
	return codexAuthCacheStatus{Path: path, Reason: reason}
}

func isValidCodexAuthFile(path string, now time.Time) bool {
	_ = now
	return codexAuthValidationError(path) == nil
}

func codexAuthValidationError(path string) error {
	// #nosec G304 -- path is derived from local profile auth location.
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var parsed struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
			IDToken     string `json:"id_token"`
			Refresh     string `json:"refresh_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("invalid auth json: %w", err)
	}
	accessToken := strings.TrimSpace(parsed.Tokens.AccessToken)
	refreshToken := strings.TrimSpace(parsed.Tokens.Refresh)
	if accessToken == "" && refreshToken == "" {
		return errors.New("auth tokens missing (need access_token or refresh_token)")
	}
	return nil
}

func codexAuthMissingReason(path string, info os.FileInfo, statErr error) string {
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return fmt.Sprintf("auth cache not found at %s; run `si login`", path)
		}
		return fmt.Sprintf("auth cache read failed at %s: %v", path, statErr)
	}
	if info != nil && info.IsDir() {
		return fmt.Sprintf("auth cache path is a directory: %s", path)
	}
	if err := codexAuthValidationError(path); err != nil {
		return fmt.Sprintf("invalid auth cache at %s: %v", path, err)
	}
	return fmt.Sprintf("auth cache unavailable at %s", path)
}

func attemptRecoverCodexAuthCache(profile codexProfile) bool {
	profileID := strings.TrimSpace(profile.ID)
	if profileID == "" || syncProfileAuthFromContainerStatusFn == nil {
		return false
	}
	if codexProfileRecoveryBlocked(profileID) {
		return false
	}
	if _, loaded := codexAuthSyncAttempts.LoadOrStore(strings.ToLower(profileID), struct{}{}); loaded {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, err := syncProfileAuthFromContainerStatusFn(ctx, profile)
	return err == nil
}

func codexProfileSummaries() []codexProfileSummary {
	profiles := codexProfiles()
	if len(profiles) == 0 {
		return nil
	}
	items := make([]codexProfileSummary, 0, len(profiles))
	for _, profile := range profiles {
		status := codexProfileAuthStatus(profile)
		item := codexProfileSummary{
			ID:                profile.ID,
			Name:              profile.Name,
			Email:             profile.Email,
			AuthCached:        status.Exists,
			AuthPath:          status.Path,
			StatusError:       status.Reason,
			FiveHourLeftPct:   -1,
			FiveHourRemaining: -1,
			WeeklyLeftPct:     -1,
			WeeklyRemaining:   -1,
		}
		if status.Exists {
			item.AuthUpdated = status.Modified.UTC().Format(time.RFC3339)
		}
		items = append(items, item)
	}
	return items
}
