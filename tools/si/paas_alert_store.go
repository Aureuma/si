package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type paasAlertEntry struct {
	Timestamp string            `json:"timestamp"`
	Command   string            `json:"command"`
	Severity  string            `json:"severity"`
	Status    string            `json:"status"`
	Target    string            `json:"target,omitempty"`
	Message   string            `json:"message"`
	Guidance  string            `json:"guidance,omitempty"`
	Context   string            `json:"context"`
	Fields    map[string]string `json:"fields,omitempty"`
}

type paasTelegramNotifierConfig struct {
	BotToken  string `json:"bot_token"`
	ChatID    string `json:"chat_id"`
	UpdatedAt string `json:"updated_at"`
}

func resolvePaasAlertHistoryPath(contextName string) (string, error) {
	contextDir, err := resolvePaasContextDir(contextName)
	if err != nil {
		return "", err
	}
	return filepath.Join(contextDir, "events", "alerts.jsonl"), nil
}

func resolvePaasTelegramConfigPath(contextName string) (string, error) {
	contextDir, err := resolvePaasContextDir(contextName)
	if err != nil {
		return "", err
	}
	return filepath.Join(contextDir, "alerts", "telegram.json"), nil
}

func loadPaasTelegramConfig(contextName string) (paasTelegramNotifierConfig, string, error) {
	path, err := resolvePaasTelegramConfigPath(contextName)
	if err != nil {
		return paasTelegramNotifierConfig{}, "", err
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- context-scoped local state path.
	if err != nil {
		if os.IsNotExist(err) {
			return paasTelegramNotifierConfig{}, path, nil
		}
		return paasTelegramNotifierConfig{}, path, err
	}
	var cfg paasTelegramNotifierConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return paasTelegramNotifierConfig{}, path, fmt.Errorf("invalid telegram notifier config: %w", err)
	}
	cfg.BotToken = strings.TrimSpace(cfg.BotToken)
	cfg.ChatID = strings.TrimSpace(cfg.ChatID)
	cfg.UpdatedAt = strings.TrimSpace(cfg.UpdatedAt)
	return cfg, path, nil
}

func savePaasTelegramConfig(contextName string, cfg paasTelegramNotifierConfig) (string, error) {
	path, err := resolvePaasTelegramConfigPath(contextName)
	if err != nil {
		return "", err
	}
	row := cfg
	row.BotToken = strings.TrimSpace(row.BotToken)
	row.ChatID = strings.TrimSpace(row.ChatID)
	if row.BotToken == "" || row.ChatID == "" {
		return "", fmt.Errorf("telegram bot token and chat id are required")
	}
	row.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	raw, err := json.MarshalIndent(row, "", "  ")
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

func recordPaasAlertEntry(entry paasAlertEntry) string {
	path, err := resolvePaasAlertHistoryPath(currentPaasContext())
	if err != nil {
		return ""
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return ""
	}
	row := entry
	row.Timestamp = strings.TrimSpace(row.Timestamp)
	if row.Timestamp == "" {
		row.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	row.Context = currentPaasContext()
	row.Command = strings.TrimSpace(row.Command)
	row.Severity = strings.ToLower(strings.TrimSpace(row.Severity))
	row.Status = strings.ToLower(strings.TrimSpace(row.Status))
	row.Target = strings.TrimSpace(row.Target)
	row.Message = strings.TrimSpace(row.Message)
	row.Guidance = strings.TrimSpace(row.Guidance)
	if row.Command == "" || row.Severity == "" || row.Status == "" || row.Message == "" {
		return ""
	}
	raw, err := json.Marshal(row)
	if err != nil {
		return ""
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return ""
	}
	defer file.Close()
	if _, err := file.Write(append(raw, '\n')); err != nil {
		return ""
	}
	return path
}

func loadPaasAlertHistory(limit int, severityFilter string) ([]paasAlertEntry, string, error) {
	path, err := resolvePaasAlertHistoryPath(currentPaasContext())
	if err != nil {
		return nil, "", err
	}
	if limit < 1 {
		limit = 1
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []paasAlertEntry{}, path, nil
		}
		return nil, path, err
	}
	defer file.Close()
	filter := strings.ToLower(strings.TrimSpace(severityFilter))
	rows := []paasAlertEntry{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row paasAlertEntry
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if filter != "" && !strings.EqualFold(strings.TrimSpace(row.Severity), filter) {
			continue
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, path, fmt.Errorf("read alert history: %w", err)
	}
	if len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}
	return rows, path, nil
}
