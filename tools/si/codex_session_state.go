package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	shared "si/agents/shared/docker"
)

const codexProfileSessionRecordFilename = "last_session.json"

const codexLatestSessionMetaLineScript = `latest="$(ls -1t /home/si/.codex/sessions/*/*/*/*.jsonl 2>/dev/null | head -n 1 || true)"
if [ -z "$latest" ]; then
  exit 0
fi
sed -n '/"type":"session_meta"/{p;q;}' "$latest"`

type codexSessionRecord struct {
	SessionID  string `json:"session_id"`
	Cwd        string `json:"cwd,omitempty"`
	RecordedAt string `json:"recorded_at,omitempty"`
}

type codexSessionMetaEvent struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		ID        string `json:"id"`
		Cwd       string `json:"cwd"`
		Timestamp string `json:"timestamp"`
	} `json:"payload"`
}

func codexResumeProfileKey(profileID string, containerName string) string {
	profileID = strings.TrimSpace(profileID)
	if profileID != "" {
		return profileID
	}
	return strings.TrimSpace(codexContainerSlug(containerName))
}

func codexProfileSessionRecordPath(profileKey string) (string, error) {
	profileKey = strings.TrimSpace(profileKey)
	if profileKey == "" {
		return "", fmt.Errorf("profile key required")
	}
	if !isValidSlug(profileKey) {
		return "", fmt.Errorf("invalid profile key %q", profileKey)
	}
	root, err := codexProfilesRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, profileKey, codexProfileSessionRecordFilename), nil
}

func loadCodexProfileSessionRecord(profileKey string) (codexSessionRecord, error) {
	path, err := codexProfileSessionRecordPath(profileKey)
	if err != nil {
		return codexSessionRecord{}, err
	}
	// #nosec G304 -- path is derived from local profile state under ~/.si/codex/profiles.
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return codexSessionRecord{}, nil
		}
		return codexSessionRecord{}, err
	}
	var record codexSessionRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return codexSessionRecord{}, fmt.Errorf("invalid codex session record: %w", err)
	}
	record.SessionID = strings.TrimSpace(record.SessionID)
	record.Cwd = strings.TrimSpace(record.Cwd)
	record.RecordedAt = strings.TrimSpace(record.RecordedAt)
	return record, nil
}

func saveCodexProfileSessionRecord(profileKey string, record codexSessionRecord) error {
	record.SessionID = strings.TrimSpace(record.SessionID)
	record.Cwd = strings.TrimSpace(record.Cwd)
	record.RecordedAt = strings.TrimSpace(record.RecordedAt)
	if record.SessionID == "" {
		return nil
	}
	if record.RecordedAt == "" {
		record.RecordedAt = time.Now().UTC().Format(time.RFC3339)
	}
	path, err := codexProfileSessionRecordPath(profileKey)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return err
	}
	return nil
}

func parseCodexSessionMetaLine(line string) (codexSessionRecord, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return codexSessionRecord{}, false
	}
	var event codexSessionMetaEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return codexSessionRecord{}, false
	}
	if strings.TrimSpace(event.Type) != "session_meta" {
		return codexSessionRecord{}, false
	}
	sessionID := strings.TrimSpace(event.Payload.ID)
	if sessionID == "" {
		return codexSessionRecord{}, false
	}
	recordedAt := firstNonEmpty(strings.TrimSpace(event.Payload.Timestamp), strings.TrimSpace(event.Timestamp))
	return codexSessionRecord{
		SessionID:  sessionID,
		Cwd:        strings.TrimSpace(event.Payload.Cwd),
		RecordedAt: recordedAt,
	}, true
}

func captureLatestCodexSessionRecord(ctx context.Context, client *shared.Client, containerID string) (codexSessionRecord, error) {
	if client == nil {
		return codexSessionRecord{}, fmt.Errorf("docker client required")
	}
	containerID = strings.TrimSpace(containerID)
	if containerID == "" {
		return codexSessionRecord{}, fmt.Errorf("container id required")
	}
	var stdout bytes.Buffer
	if err := client.Exec(ctx, containerID, []string{"bash", "-lc", codexLatestSessionMetaLineScript}, shared.ExecOptions{}, nil, &stdout, io.Discard); err != nil {
		return codexSessionRecord{}, err
	}
	line := strings.TrimSpace(stdout.String())
	if line == "" {
		return codexSessionRecord{}, nil
	}
	record, ok := parseCodexSessionMetaLine(line)
	if !ok {
		return codexSessionRecord{}, fmt.Errorf("failed to parse codex session metadata")
	}
	return record, nil
}

func syncCodexProfileSessionRecordFromContainer(ctx context.Context, client *shared.Client, containerID string, profileKey string) (codexSessionRecord, error) {
	profileKey = strings.TrimSpace(profileKey)
	if profileKey == "" {
		return codexSessionRecord{}, nil
	}
	record, err := captureLatestCodexSessionRecord(ctx, client, containerID)
	if err != nil {
		return codexSessionRecord{}, err
	}
	if strings.TrimSpace(record.SessionID) == "" {
		return codexSessionRecord{}, nil
	}
	if err := saveCodexProfileSessionRecord(profileKey, record); err != nil {
		return codexSessionRecord{}, err
	}
	return record, nil
}
