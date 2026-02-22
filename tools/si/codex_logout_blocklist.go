package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const codexLogoutBlockedProfilesFile = "codex-logout-blocked-profiles.json"

func codexLogoutBlockedProfilesPath(home string) (string, error) {
	home = filepath.Clean(strings.TrimSpace(home))
	if home == "" {
		return "", errors.New("home required")
	}
	return filepath.Join(home, ".si", codexLogoutBlockedProfilesFile), nil
}

func normalizeCodexProfileID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

func loadCodexLogoutBlockedProfiles(home string) (map[string]struct{}, error) {
	path, err := codexLogoutBlockedProfilesPath(home)
	if err != nil {
		return nil, err
	}
	// #nosec G304 -- path is derived from local user home path.
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]struct{}{}, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return map[string]struct{}{}, nil
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if norm := normalizeCodexProfileID(id); norm != "" {
			out[norm] = struct{}{}
		}
	}
	return out, nil
}

func saveCodexLogoutBlockedProfiles(home string, blocked map[string]struct{}) error {
	path, err := codexLogoutBlockedProfilesPath(home)
	if err != nil {
		return err
	}
	if len(blocked) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	ids := make([]string, 0, len(blocked))
	for id := range blocked {
		if norm := normalizeCodexProfileID(id); norm != "" {
			ids = append(ids, norm)
		}
	}
	sort.Strings(ids)
	data, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "codex-logout-blocked-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func addCodexLogoutBlockedProfiles(home string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	blocked, err := loadCodexLogoutBlockedProfiles(home)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if norm := normalizeCodexProfileID(id); norm != "" {
			blocked[norm] = struct{}{}
		}
	}
	return saveCodexLogoutBlockedProfiles(home, blocked)
}

func removeCodexLogoutBlockedProfiles(home string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	blocked, err := loadCodexLogoutBlockedProfiles(home)
	if err != nil {
		return err
	}
	for _, id := range ids {
		delete(blocked, normalizeCodexProfileID(id))
	}
	return saveCodexLogoutBlockedProfiles(home, blocked)
}

func codexProfileRecoveryBlocked(profileID string) bool {
	profileID = normalizeCodexProfileID(profileID)
	if profileID == "" {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return false
	}
	blocked, err := loadCodexLogoutBlockedProfiles(home)
	if err != nil {
		return false
	}
	_, ok := blocked[profileID]
	return ok
}

func clearCodexProfileRecoveryBlock(profileID string) error {
	profileID = normalizeCodexProfileID(profileID)
	if profileID == "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		if err == nil {
			err = errors.New("home dir not available")
		}
		return err
	}
	return removeCodexLogoutBlockedProfiles(home, []string{profileID})
}

func discoverCachedCodexProfileIDs(home string) []string {
	home = filepath.Clean(strings.TrimSpace(home))
	if home == "" {
		return nil
	}
	root := filepath.Join(home, ".si", "codex", "profiles")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	out := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if id := normalizeCodexProfileID(entry.Name()); id != "" {
			out[id] = struct{}{}
		}
	}
	ids := make([]string, 0, len(out))
	for id := range out {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
