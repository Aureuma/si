package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type paasAgentConfig struct {
	Name         string   `json:"name"`
	Enabled      bool     `json:"enabled"`
	Targets      []string `json:"targets,omitempty"`
	Profile      string   `json:"profile,omitempty"`
	CreatedAt    string   `json:"created_at,omitempty"`
	UpdatedAt    string   `json:"updated_at,omitempty"`
	LastRunAt    string   `json:"last_run_at,omitempty"`
	LastRunID    string   `json:"last_run_id,omitempty"`
	LastRunState string   `json:"last_run_state,omitempty"`
}

type paasAgentStore struct {
	Agents []paasAgentConfig `json:"agents,omitempty"`
}

type paasAgentRunRecord struct {
	Timestamp      string `json:"timestamp"`
	Agent          string `json:"agent"`
	RunID          string `json:"run_id"`
	Status         string `json:"status"`
	IncidentID     string `json:"incident_id,omitempty"`
	Collected      int    `json:"collected"`
	Inserted       int    `json:"inserted"`
	Updated        int    `json:"updated"`
	Pruned         int    `json:"pruned"`
	QueueTotal     int    `json:"queue_total"`
	QueuePath      string `json:"queue_path,omitempty"`
	CollectorCount int    `json:"collector_count,omitempty"`
	Message        string `json:"message,omitempty"`
}

func resolvePaasAgentStorePath(contextName string) (string, error) {
	contextDir, err := resolvePaasContextDir(contextName)
	if err != nil {
		return "", err
	}
	return filepath.Join(contextDir, "agents", "agents.json"), nil
}

func loadPaasAgentStore(contextName string) (paasAgentStore, string, error) {
	path, err := resolvePaasAgentStorePath(contextName)
	if err != nil {
		return paasAgentStore{}, "", err
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- context-scoped path.
	if err != nil {
		if os.IsNotExist(err) {
			return paasAgentStore{}, path, nil
		}
		return paasAgentStore{}, path, err
	}
	var store paasAgentStore
	if err := json.Unmarshal(raw, &store); err != nil {
		return paasAgentStore{}, path, fmt.Errorf("invalid agent store: %w", err)
	}
	for i := range store.Agents {
		store.Agents[i] = normalizePaasAgentConfig(store.Agents[i])
	}
	sortPaasAgentConfigs(store.Agents)
	return store, path, nil
}

func savePaasAgentStore(contextName string, store paasAgentStore) (string, error) {
	path, err := resolvePaasAgentStorePath(contextName)
	if err != nil {
		return "", err
	}
	for i := range store.Agents {
		store.Agents[i] = normalizePaasAgentConfig(store.Agents[i])
	}
	sortPaasAgentConfigs(store.Agents)
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func normalizePaasAgentConfig(cfg paasAgentConfig) paasAgentConfig {
	cfg.Name = strings.TrimSpace(cfg.Name)
	cfg.Targets = parseCSV(strings.Join(cfg.Targets, ","))
	cfg.Profile = strings.TrimSpace(cfg.Profile)
	cfg.CreatedAt = strings.TrimSpace(cfg.CreatedAt)
	cfg.UpdatedAt = strings.TrimSpace(cfg.UpdatedAt)
	cfg.LastRunAt = strings.TrimSpace(cfg.LastRunAt)
	cfg.LastRunID = strings.TrimSpace(cfg.LastRunID)
	cfg.LastRunState = strings.TrimSpace(cfg.LastRunState)
	return cfg
}

func sortPaasAgentConfigs(rows []paasAgentConfig) {
	sort.SliceStable(rows, func(i, j int) bool {
		return strings.ToLower(strings.TrimSpace(rows[i].Name)) < strings.ToLower(strings.TrimSpace(rows[j].Name))
	})
}

func findPaasAgentIndex(store paasAgentStore, name string) int {
	needle := strings.ToLower(strings.TrimSpace(name))
	if needle == "" {
		return -1
	}
	for i, row := range store.Agents {
		if strings.ToLower(strings.TrimSpace(row.Name)) == needle {
			return i
		}
	}
	return -1
}

func resolvePaasAgentRunLogPath(contextName string) (string, error) {
	contextDir, err := resolvePaasContextDir(contextName)
	if err != nil {
		return "", err
	}
	return filepath.Join(contextDir, "events", "agent-runs.jsonl"), nil
}

func appendPaasAgentRunRecord(record paasAgentRunRecord) (string, error) {
	path, err := resolvePaasAgentRunLogPath(currentPaasContext())
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	row := record
	if strings.TrimSpace(row.Timestamp) == "" {
		row.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	row.Agent = strings.TrimSpace(row.Agent)
	row.RunID = strings.TrimSpace(row.RunID)
	row.Status = strings.ToLower(strings.TrimSpace(row.Status))
	row.IncidentID = strings.TrimSpace(row.IncidentID)
	row.QueuePath = strings.TrimSpace(row.QueuePath)
	row.Message = strings.TrimSpace(row.Message)
	if row.Agent == "" || row.RunID == "" || row.Status == "" {
		return "", fmt.Errorf("agent, run_id, and status are required")
	}
	raw, err := json.Marshal(row)
	if err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := file.Write(append(raw, '\n')); err != nil {
		return "", err
	}
	return path, nil
}

func loadPaasAgentRunRecords(name string, tail int) ([]paasAgentRunRecord, string, error) {
	if tail < 1 {
		tail = 1
	}
	path, err := resolvePaasAgentRunLogPath(currentPaasContext())
	if err != nil {
		return nil, "", err
	}
	file, err := os.Open(path) // #nosec G304 -- context-scoped path.
	if err != nil {
		if os.IsNotExist(err) {
			return []paasAgentRunRecord{}, path, nil
		}
		return nil, path, err
	}
	defer file.Close()

	selectedName := strings.ToLower(strings.TrimSpace(name))
	rows := make([]paasAgentRunRecord, 0, tail)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row paasAgentRunRecord
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		row.Agent = strings.TrimSpace(row.Agent)
		if selectedName != "" && strings.ToLower(row.Agent) != selectedName {
			continue
		}
		row.Timestamp = strings.TrimSpace(row.Timestamp)
		row.RunID = strings.TrimSpace(row.RunID)
		row.Status = strings.ToLower(strings.TrimSpace(row.Status))
		row.IncidentID = strings.TrimSpace(row.IncidentID)
		row.QueuePath = strings.TrimSpace(row.QueuePath)
		row.Message = strings.TrimSpace(row.Message)
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, path, err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left := parsePaasIncidentQueueTimestamp(rows[i].Timestamp)
		right := parsePaasIncidentQueueTimestamp(rows[j].Timestamp)
		return left.After(right)
	})
	if len(rows) > tail {
		rows = rows[:tail]
	}
	return rows, path, nil
}

