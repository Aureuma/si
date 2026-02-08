package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type googleOAuthTokenEntry struct {
	AccountAlias string `json:"account_alias,omitempty"`
	Environment  string `json:"environment,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	Scope        string `json:"scope,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

type googleOAuthTokenStore struct {
	Tokens map[string]googleOAuthTokenEntry `json:"tokens"`
}

func googleOAuthTokenStorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		if err == nil {
			err = os.ErrNotExist
		}
		return "", err
	}
	return filepath.Join(home, ".si", "google", "youtube", "oauth_tokens.json"), nil
}

func loadGoogleOAuthTokenStore() (googleOAuthTokenStore, error) {
	path, err := googleOAuthTokenStorePath()
	if err != nil {
		return googleOAuthTokenStore{Tokens: map[string]googleOAuthTokenEntry{}}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return googleOAuthTokenStore{Tokens: map[string]googleOAuthTokenEntry{}}, nil
		}
		return googleOAuthTokenStore{Tokens: map[string]googleOAuthTokenEntry{}}, err
	}
	store := googleOAuthTokenStore{Tokens: map[string]googleOAuthTokenEntry{}}
	if len(data) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return googleOAuthTokenStore{Tokens: map[string]googleOAuthTokenEntry{}}, err
	}
	if store.Tokens == nil {
		store.Tokens = map[string]googleOAuthTokenEntry{}
	}
	return store, nil
}

func saveGoogleOAuthTokenStore(store googleOAuthTokenStore) error {
	path, err := googleOAuthTokenStorePath()
	if err != nil {
		return err
	}
	if store.Tokens == nil {
		store.Tokens = map[string]googleOAuthTokenEntry{}
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
	tmp, err := os.CreateTemp(dir, "youtube-oauth-*.json")
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

func googleOAuthTokenStoreKey(alias string, env string) string {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		alias = "_default"
	}
	env = strings.TrimSpace(env)
	if env == "" {
		env = "prod"
	}
	return alias + "|" + env
}

func loadGoogleOAuthTokenEntry(alias string, env string) (googleOAuthTokenEntry, bool) {
	store, err := loadGoogleOAuthTokenStore()
	if err != nil {
		return googleOAuthTokenEntry{}, false
	}
	entry, ok := store.Tokens[googleOAuthTokenStoreKey(alias, env)]
	if !ok {
		return googleOAuthTokenEntry{}, false
	}
	return entry, true
}

func saveGoogleOAuthTokenEntry(alias string, env string, entry googleOAuthTokenEntry) error {
	store, err := loadGoogleOAuthTokenStore()
	if err != nil {
		return err
	}
	entry.AccountAlias = strings.TrimSpace(alias)
	entry.Environment = strings.TrimSpace(env)
	entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	store.Tokens[googleOAuthTokenStoreKey(alias, env)] = entry
	return saveGoogleOAuthTokenStore(store)
}

func deleteGoogleOAuthTokenEntry(alias string, env string) error {
	store, err := loadGoogleOAuthTokenStore()
	if err != nil {
		return err
	}
	delete(store.Tokens, googleOAuthTokenStoreKey(alias, env))
	return saveGoogleOAuthTokenStore(store)
}

func parseGoogleOAuthExpiry(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}
