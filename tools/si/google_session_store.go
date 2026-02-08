package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type googlePlacesSessionEntry struct {
	Token        string `json:"token"`
	AccountAlias string `json:"account_alias,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	EndedAt      string `json:"ended_at,omitempty"`
	Note         string `json:"note,omitempty"`
}

type googlePlacesSessionStore struct {
	Sessions map[string]googlePlacesSessionEntry `json:"sessions"`
}

func googlePlacesSessionPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		if err == nil {
			err = os.ErrNotExist
		}
		return "", err
	}
	return filepath.Join(home, ".si", "google", "places", "sessions.json"), nil
}

func loadGooglePlacesSessionStore() (googlePlacesSessionStore, error) {
	path, err := googlePlacesSessionPath()
	if err != nil {
		return googlePlacesSessionStore{Sessions: map[string]googlePlacesSessionEntry{}}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return googlePlacesSessionStore{Sessions: map[string]googlePlacesSessionEntry{}}, nil
		}
		return googlePlacesSessionStore{Sessions: map[string]googlePlacesSessionEntry{}}, err
	}
	store := googlePlacesSessionStore{Sessions: map[string]googlePlacesSessionEntry{}}
	if len(data) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return googlePlacesSessionStore{Sessions: map[string]googlePlacesSessionEntry{}}, err
	}
	if store.Sessions == nil {
		store.Sessions = map[string]googlePlacesSessionEntry{}
	}
	return store, nil
}

func saveGooglePlacesSessionStore(store googlePlacesSessionStore) error {
	path, err := googlePlacesSessionPath()
	if err != nil {
		return err
	}
	if store.Sessions == nil {
		store.Sessions = map[string]googlePlacesSessionEntry{}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(dir, "sessions-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func generateGooglePlacesSessionToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	hexv := hex.EncodeToString(buf)
	if len(hexv) != 32 {
		return "", fmt.Errorf("failed to generate session token")
	}
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexv[:8], hexv[8:12], hexv[12:16], hexv[16:20], hexv[20:]), nil
}

func googlePlacesDefaultAccountAlias() string {
	settings := loadSettingsOrDefault()
	if value := strings.TrimSpace(settings.Google.DefaultAccount); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_DEFAULT_ACCOUNT")); value != "" {
		return value
	}
	return ""
}

func googlePlacesNowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
