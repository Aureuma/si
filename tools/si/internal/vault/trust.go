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
	File        string `json:"file"`
	Fingerprint string `json:"fingerprint"`
	TrustedAt   string `json:"trusted_at,omitempty"`
}

func LoadTrustStore(path string) (*TrustStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return &TrustStore{SchemaVersion: 3}, nil
	}
	path, err := ExpandHome(path)
	if err != nil {
		return nil, err
	}
	data, err := readFileScoped(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &TrustStore{SchemaVersion: 3}, nil
		}
		return nil, err
	}
	var store TrustStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	if store.SchemaVersion < 3 {
		store.SchemaVersion = 3
	}
	return &store, nil
}

func (s *TrustStore) Find(repoRoot, file string) (*TrustEntry, bool) {
	if s == nil {
		return nil, false
	}
	repoRoot = filepath.Clean(strings.TrimSpace(repoRoot))
	file = filepath.Clean(strings.TrimSpace(file))
	for i := range s.Entries {
		e := &s.Entries[i]
		if filepath.Clean(strings.TrimSpace(e.RepoRoot)) == repoRoot &&
			filepath.Clean(strings.TrimSpace(e.File)) == file {
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
	entry.File = filepath.Clean(strings.TrimSpace(entry.File))
	entry.Fingerprint = strings.TrimSpace(entry.Fingerprint)
	if entry.TrustedAt == "" {
		entry.TrustedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	for i := range s.Entries {
		e := &s.Entries[i]
		if filepath.Clean(strings.TrimSpace(e.RepoRoot)) == entry.RepoRoot &&
			filepath.Clean(strings.TrimSpace(e.File)) == entry.File {
			*e = entry
			return
		}
	}
	s.Entries = append(s.Entries, entry)
}

func (s *TrustStore) Delete(repoRoot, file string) bool {
	if s == nil {
		return false
	}
	repoRoot = filepath.Clean(strings.TrimSpace(repoRoot))
	file = filepath.Clean(strings.TrimSpace(file))
	out := s.Entries[:0]
	removed := false
	for _, e := range s.Entries {
		if filepath.Clean(strings.TrimSpace(e.RepoRoot)) == repoRoot &&
			filepath.Clean(strings.TrimSpace(e.File)) == file {
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
