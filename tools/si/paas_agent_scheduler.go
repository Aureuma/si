package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const paasAgentLockTTL = 15 * time.Minute

type paasAgentLockState struct {
	Agent       string `json:"agent"`
	Owner       string `json:"owner"`
	PID         int    `json:"pid"`
	AcquiredAt  string `json:"acquired_at"`
	HeartbeatAt string `json:"heartbeat_at"`
}

type paasAgentLockResult struct {
	Acquired  bool
	Recovered bool
	Path      string
	Reason    string
}

func resolvePaasAgentLockPath(contextName, agentName string) (string, error) {
	contextDir, err := resolvePaasContextDir(contextName)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(agentName)
	if name == "" {
		return "", fmt.Errorf("agent name is required")
	}
	return filepath.Join(contextDir, "agents", "locks", name+".lock.json"), nil
}

func acquirePaasAgentLock(agentName, owner string, now time.Time) (paasAgentLockResult, error) {
	path, err := resolvePaasAgentLockPath(currentPaasContext(), agentName)
	if err != nil {
		return paasAgentLockResult{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return paasAgentLockResult{}, err
	}

	existing, exists, err := loadPaasAgentLockState(path)
	if err != nil {
		return paasAgentLockResult{}, err
	}
	if exists {
		last := parsePaasAgentLockTime(existing.HeartbeatAt)
		if last.IsZero() {
			last = parsePaasAgentLockTime(existing.AcquiredAt)
		}
		if !last.IsZero() && now.Sub(last) <= paasAgentLockTTL {
			return paasAgentLockResult{
				Acquired: false,
				Path:     path,
				Reason:   fmt.Sprintf("lock is active (owner=%s heartbeat=%s)", strings.TrimSpace(existing.Owner), last.Format(time.RFC3339)),
			}, nil
		}
	}
	state := paasAgentLockState{
		Agent:       strings.TrimSpace(agentName),
		Owner:       strings.TrimSpace(owner),
		PID:         os.Getpid(),
		AcquiredAt:  now.Format(time.RFC3339Nano),
		HeartbeatAt: now.Format(time.RFC3339Nano),
	}
	if err := savePaasAgentLockState(path, state); err != nil {
		return paasAgentLockResult{}, err
	}
	return paasAgentLockResult{
		Acquired:  true,
		Recovered: exists,
		Path:      path,
	}, nil
}

func releasePaasAgentLock(agentName string) error {
	path, err := resolvePaasAgentLockPath(currentPaasContext(), agentName)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func loadPaasAgentLockState(path string) (paasAgentLockState, bool, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- context-scoped path.
	if err != nil {
		if os.IsNotExist(err) {
			return paasAgentLockState{}, false, nil
		}
		return paasAgentLockState{}, false, err
	}
	var row paasAgentLockState
	if err := json.Unmarshal(raw, &row); err != nil {
		return paasAgentLockState{}, false, err
	}
	row.Agent = strings.TrimSpace(row.Agent)
	row.Owner = strings.TrimSpace(row.Owner)
	row.AcquiredAt = strings.TrimSpace(row.AcquiredAt)
	row.HeartbeatAt = strings.TrimSpace(row.HeartbeatAt)
	return row, true, nil
}

func savePaasAgentLockState(path string, state paasAgentLockState) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o600)
}

func parsePaasAgentLockTime(value string) time.Time {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return time.Time{}
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

