package vault

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type TrustStore struct {
	SchemaVersion int          `json:"schema_version"`
	Entries       []TrustEntry `json:"entries,omitempty"`
}

type TrustEntry struct {
	RepoRoot    string `json:"repo_root"`
	VaultDir    string `json:"vault_dir"`
	Env         string `json:"env"`
	VaultRepo   string `json:"vault_repo_url,omitempty"`
	Fingerprint string `json:"fingerprint"`
	TrustedAt   string `json:"trusted_at,omitempty"`
}

func LoadTrustStore(path string) (*TrustStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return &TrustStore{SchemaVersion: 1}, nil
	}
	path, err := ExpandHome(path)
	if err != nil {
		return nil, err
	}
	data, err := readFileScoped(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &TrustStore{SchemaVersion: 1}, nil
		}
		return nil, err
	}
	var store TrustStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	if store.SchemaVersion == 0 {
		store.SchemaVersion = 1
	}
	return &store, nil
}

func (s *TrustStore) Find(repoRoot, vaultDir, env string) (*TrustEntry, bool) {
	if s == nil {
		return nil, false
	}
	repoRoot = filepath.Clean(strings.TrimSpace(repoRoot))
	vaultDir = filepath.Clean(strings.TrimSpace(vaultDir))
	env = strings.TrimSpace(env)
	for i := range s.Entries {
		e := &s.Entries[i]
		if filepath.Clean(strings.TrimSpace(e.RepoRoot)) == repoRoot &&
			filepath.Clean(strings.TrimSpace(e.VaultDir)) == vaultDir &&
			strings.TrimSpace(e.Env) == env {
			return e, true
		}
	}
	return nil, false
}

func (s *TrustStore) Upsert(entry TrustEntry) {
	if s == nil {
		return
	}
	entry.RepoRoot = filepath.Clean(strings.TrimSpace(entry.RepoRoot))
	entry.VaultDir = filepath.Clean(strings.TrimSpace(entry.VaultDir))
	entry.Env = strings.TrimSpace(entry.Env)
	entry.VaultRepo = strings.TrimSpace(entry.VaultRepo)
	entry.Fingerprint = strings.TrimSpace(entry.Fingerprint)
	if entry.TrustedAt == "" {
		entry.TrustedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	for i := range s.Entries {
		e := &s.Entries[i]
		if filepath.Clean(strings.TrimSpace(e.RepoRoot)) == entry.RepoRoot &&
			filepath.Clean(strings.TrimSpace(e.VaultDir)) == entry.VaultDir &&
			strings.TrimSpace(e.Env) == entry.Env {
			*e = entry
			return
		}
	}
	s.Entries = append(s.Entries, entry)
}

func (s *TrustStore) Delete(repoRoot, vaultDir, env string) bool {
	if s == nil {
		return false
	}
	repoRoot = filepath.Clean(strings.TrimSpace(repoRoot))
	vaultDir = filepath.Clean(strings.TrimSpace(vaultDir))
	env = strings.TrimSpace(env)
	out := s.Entries[:0]
	removed := false
	for _, e := range s.Entries {
		if filepath.Clean(strings.TrimSpace(e.RepoRoot)) == repoRoot &&
			filepath.Clean(strings.TrimSpace(e.VaultDir)) == vaultDir &&
			strings.TrimSpace(e.Env) == env {
			removed = true
			continue
		}
		out = append(out, e)
	}
	s.Entries = out
	return removed
}

func (s *TrustStore) Save(path string) error {
	if s == nil {
		return nil
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	path, err := ExpandHome(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, "trust-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
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
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
