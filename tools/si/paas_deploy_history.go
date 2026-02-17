package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type paasDeployHistoryStore struct {
	Apps map[string]paasAppDeployHistory `json:"apps,omitempty"`
}

type paasAppDeployHistory struct {
	CurrentRelease string   `json:"current_release,omitempty"`
	History        []string `json:"history,omitempty"`
	UpdatedAt      string   `json:"updated_at,omitempty"`
}

func resolvePaasDeployHistoryPath() (string, error) {
	return resolvePaasDeployHistoryPathForContext(currentPaasContext())
}

func resolvePaasDeployHistoryPathForContext(contextName string) (string, error) {
	contextDir, err := resolvePaasContextDir(contextName)
	if err != nil {
		return "", err
	}
	return filepath.Join(contextDir, "deployments.json"), nil
}

func loadPaasDeployHistoryStore() (paasDeployHistoryStore, error) {
	return loadPaasDeployHistoryStoreForContext(currentPaasContext())
}

func loadPaasDeployHistoryStoreForContext(contextName string) (paasDeployHistoryStore, error) {
	path, err := resolvePaasDeployHistoryPathForContext(contextName)
	if err != nil {
		return paasDeployHistoryStore{}, err
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- local state path derived from context root.
	if err != nil {
		if os.IsNotExist(err) {
			return paasDeployHistoryStore{Apps: map[string]paasAppDeployHistory{}}, nil
		}
		return paasDeployHistoryStore{}, err
	}
	var store paasDeployHistoryStore
	if err := json.Unmarshal(raw, &store); err != nil {
		return paasDeployHistoryStore{}, fmt.Errorf("invalid deploy history: %w", err)
	}
	if store.Apps == nil {
		store.Apps = map[string]paasAppDeployHistory{}
	}
	return store, nil
}

func savePaasDeployHistoryStore(store paasDeployHistoryStore) error {
	return savePaasDeployHistoryStoreForContext(currentPaasContext(), store)
}

func savePaasDeployHistoryStoreForContext(contextName string, store paasDeployHistoryStore) error {
	path, err := resolvePaasDeployHistoryPathForContext(contextName)
	if err != nil {
		return err
	}
	if store.Apps == nil {
		store.Apps = map[string]paasAppDeployHistory{}
	}
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func recordPaasSuccessfulRelease(app, releaseID string) error {
	store, err := loadPaasDeployHistoryStore()
	if err != nil {
		return err
	}
	key := sanitizePaasReleasePathSegment(app)
	item := store.Apps[key]
	item.CurrentRelease = strings.TrimSpace(releaseID)
	item.History = appendUniquePaasRelease(item.History, strings.TrimSpace(releaseID))
	item.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	store.Apps[key] = item
	return savePaasDeployHistoryStore(store)
}

func resolvePaasCurrentRelease(app string) (string, error) {
	store, err := loadPaasDeployHistoryStore()
	if err != nil {
		return "", err
	}
	item := store.Apps[sanitizePaasReleasePathSegment(app)]
	return strings.TrimSpace(item.CurrentRelease), nil
}

func resolvePaasPreviousRelease(app string) (string, error) {
	store, err := loadPaasDeployHistoryStore()
	if err != nil {
		return "", err
	}
	item := store.Apps[sanitizePaasReleasePathSegment(app)]
	current := strings.TrimSpace(item.CurrentRelease)
	for i := len(item.History) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(item.History[i])
		if candidate == "" || strings.EqualFold(candidate, current) {
			continue
		}
		return candidate, nil
	}
	return "", nil
}

func appendUniquePaasRelease(history []string, releaseID string) []string {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return history
	}
	out := make([]string, 0, len(history)+1)
	for _, row := range history {
		value := strings.TrimSpace(row)
		if value == "" || strings.EqualFold(value, releaseID) {
			continue
		}
		out = append(out, value)
	}
	out = append(out, releaseID)
	if len(out) > 40 {
		out = out[len(out)-40:]
	}
	return out
}
