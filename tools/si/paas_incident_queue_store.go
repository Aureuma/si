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

const (
	paasIncidentQueueDefaultCollectLimit = 200
	paasIncidentQueueDefaultMaxEntries   = 1000
	paasIncidentQueueDefaultMaxAge       = 14 * 24 * time.Hour
)

type paasIncidentQueueEntry struct {
	Key       string            `json:"key"`
	Incident  paasIncidentEvent `json:"incident"`
	Status    string            `json:"status"`
	FirstSeen string            `json:"first_seen"`
	LastSeen  string            `json:"last_seen"`
	SeenCount int               `json:"seen_count"`
}

type paasIncidentQueueSyncResult struct {
	CollectorStats []paasIncidentCollectorStat `json:"collector_stats,omitempty"`
	Collected      int                         `json:"collected"`
	Inserted       int                         `json:"inserted"`
	Updated        int                         `json:"updated"`
	Pruned         int                         `json:"pruned"`
	Total          int                         `json:"total"`
	Path           string                      `json:"path"`
}

func resolvePaasIncidentQueuePath(contextName string) (string, error) {
	contextDir, err := resolvePaasContextDir(contextName)
	if err != nil {
		return "", err
	}
	return filepath.Join(contextDir, "events", "incidents.jsonl"), nil
}

func loadPaasIncidentQueueEntries(contextName string) ([]paasIncidentQueueEntry, string, error) {
	path, err := resolvePaasIncidentQueuePath(contextName)
	if err != nil {
		return nil, "", err
	}
	file, err := os.Open(path) // #nosec G304 -- context-scoped state path.
	if err != nil {
		if os.IsNotExist(err) {
			return []paasIncidentQueueEntry{}, path, nil
		}
		return nil, path, err
	}
	defer file.Close()

	rows := make([]paasIncidentQueueEntry, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row paasIncidentQueueEntry
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		row.Key = strings.TrimSpace(row.Key)
		if row.Key == "" {
			row.Key = resolvePaasIncidentQueueEntryKey(row.Incident)
		}
		row.Status = normalizePaasIncidentStatus(row.Status)
		if row.Status == "" {
			row.Status = normalizePaasIncidentStatus(row.Incident.Status)
		}
		row.FirstSeen = strings.TrimSpace(row.FirstSeen)
		row.LastSeen = strings.TrimSpace(row.LastSeen)
		if row.FirstSeen == "" {
			row.FirstSeen = strings.TrimSpace(row.Incident.TriggeredAt)
		}
		if row.LastSeen == "" {
			row.LastSeen = strings.TrimSpace(row.Incident.TriggeredAt)
		}
		if row.SeenCount < 1 {
			row.SeenCount = 1
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, path, err
	}
	sortPaasIncidentQueueEntries(rows)
	return rows, path, nil
}

func savePaasIncidentQueueEntries(contextName string, rows []paasIncidentQueueEntry) (string, error) {
	path, err := resolvePaasIncidentQueuePath(contextName)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	sortPaasIncidentQueueEntries(rows)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer file.Close()
	for _, row := range rows {
		row.Key = resolvePaasIncidentQueueEntryKey(row.Incident)
		row.Status = normalizePaasIncidentStatus(row.Status)
		if row.Status == "" {
			row.Status = normalizePaasIncidentStatus(row.Incident.Status)
		}
		row.FirstSeen = strings.TrimSpace(row.FirstSeen)
		row.LastSeen = strings.TrimSpace(row.LastSeen)
		if row.FirstSeen == "" {
			row.FirstSeen = strings.TrimSpace(row.Incident.TriggeredAt)
		}
		if row.LastSeen == "" {
			row.LastSeen = strings.TrimSpace(row.Incident.TriggeredAt)
		}
		if row.SeenCount < 1 {
			row.SeenCount = 1
		}
		raw, err := json.Marshal(row)
		if err != nil {
			return "", err
		}
		if _, err := file.Write(append(raw, '\n')); err != nil {
			return "", err
		}
	}
	return path, nil
}

func syncPaasIncidentQueueFromCollectors(limit, maxEntries int, maxAge time.Duration) (paasIncidentQueueSyncResult, error) {
	if limit < 1 {
		limit = paasIncidentQueueDefaultCollectLimit
	}
	if maxEntries < 1 {
		maxEntries = paasIncidentQueueDefaultMaxEntries
	}
	if maxAge <= 0 {
		maxAge = paasIncidentQueueDefaultMaxAge
	}
	collected, collectorStats, err := collectPaasIncidentEvents(limit)
	if err != nil {
		return paasIncidentQueueSyncResult{}, err
	}
	current, _, err := loadPaasIncidentQueueEntries(currentPaasContext())
	if err != nil {
		return paasIncidentQueueSyncResult{}, err
	}
	now := time.Now().UTC()
	next, inserted, updated := upsertPaasIncidentQueueEntries(current, collected, now)
	next, pruned := applyPaasIncidentQueueRetention(next, maxEntries, maxAge, now)
	path, err := savePaasIncidentQueueEntries(currentPaasContext(), next)
	if err != nil {
		return paasIncidentQueueSyncResult{}, err
	}
	return paasIncidentQueueSyncResult{
		CollectorStats: collectorStats,
		Collected:      len(collected),
		Inserted:       inserted,
		Updated:        updated,
		Pruned:         pruned,
		Total:          len(next),
		Path:           path,
	}, nil
}

func upsertPaasIncidentQueueEntries(existing []paasIncidentQueueEntry, incidents []paasIncidentEvent, now time.Time) ([]paasIncidentQueueEntry, int, int) {
	rows := make([]paasIncidentQueueEntry, 0, len(existing)+len(incidents))
	byKey := map[string]*paasIncidentQueueEntry{}
	for i := range existing {
		row := existing[i]
		key := strings.TrimSpace(row.Key)
		if key == "" {
			key = resolvePaasIncidentQueueEntryKey(row.Incident)
		}
		if key == "" {
			continue
		}
		row.Key = key
		copyRow := row
		byKey[key] = &copyRow
	}
	inserted := 0
	updated := 0
	for _, incident := range incidents {
		key := resolvePaasIncidentQueueEntryKey(incident)
		if key == "" {
			continue
		}
		existingRow, ok := byKey[key]
		if !ok {
			byKey[key] = &paasIncidentQueueEntry{
				Key:       key,
				Incident:  incident,
				Status:    normalizePaasIncidentStatus(incident.Status),
				FirstSeen: strings.TrimSpace(incident.TriggeredAt),
				LastSeen:  strings.TrimSpace(incident.TriggeredAt),
				SeenCount: 1,
			}
			inserted++
			continue
		}
		updated++
		existingRow.Incident = mergePaasIncidentEvent(existingRow.Incident, incident)
		existingRow.Status = normalizePaasIncidentStatus(existingRow.Status)
		if existingRow.Status == "" {
			existingRow.Status = normalizePaasIncidentStatus(incident.Status)
		}
		if existingRow.Status == "" {
			existingRow.Status = paasIncidentStatusOpen
		}
		existingRow.SeenCount++
		if strings.TrimSpace(existingRow.FirstSeen) == "" {
			existingRow.FirstSeen = strings.TrimSpace(incident.TriggeredAt)
		}
		existingRow.LastSeen = now.UTC().Format(time.RFC3339Nano)
	}
	for _, row := range byKey {
		rows = append(rows, *row)
	}
	sortPaasIncidentQueueEntries(rows)
	return rows, inserted, updated
}

func applyPaasIncidentQueueRetention(entries []paasIncidentQueueEntry, maxEntries int, maxAge time.Duration, now time.Time) ([]paasIncidentQueueEntry, int) {
	if maxEntries < 1 {
		maxEntries = paasIncidentQueueDefaultMaxEntries
	}
	if maxAge <= 0 {
		maxAge = paasIncidentQueueDefaultMaxAge
	}
	cutoff := now.UTC().Add(-maxAge)
	filtered := make([]paasIncidentQueueEntry, 0, len(entries))
	pruned := 0
	for _, row := range entries {
		last := parsePaasIncidentQueueTimestamp(row.LastSeen)
		if last.IsZero() {
			last = parsePaasIncidentQueueTimestamp(row.Incident.TriggeredAt)
		}
		if !last.IsZero() && last.Before(cutoff) {
			pruned++
			continue
		}
		filtered = append(filtered, row)
	}
	sortPaasIncidentQueueEntries(filtered)
	if len(filtered) > maxEntries {
		pruned += len(filtered) - maxEntries
		filtered = filtered[:maxEntries]
	}
	return filtered, pruned
}

func resolvePaasIncidentQueueEntryKey(incident paasIncidentEvent) string {
	key := strings.TrimSpace(incident.DedupeKey)
	window := strings.TrimSpace(incident.WindowStart)
	if key == "" {
		return ""
	}
	if window == "" {
		return key
	}
	return key + "|" + window
}

func mergePaasIncidentEvent(current, incoming paasIncidentEvent) paasIncidentEvent {
	out := current
	if severityRank(normalizePaasIncidentSeverity(incoming.Severity)) > severityRank(normalizePaasIncidentSeverity(out.Severity)) {
		out.Severity = normalizePaasIncidentSeverity(incoming.Severity)
	}
	if strings.TrimSpace(incoming.Message) != "" {
		out.Message = strings.TrimSpace(incoming.Message)
	}
	if strings.TrimSpace(incoming.Target) != "" {
		out.Target = strings.TrimSpace(incoming.Target)
	}
	if strings.TrimSpace(incoming.Signal) != "" {
		out.Signal = strings.TrimSpace(incoming.Signal)
	}
	if strings.TrimSpace(incoming.TriggeredAt) != "" {
		out.TriggeredAt = strings.TrimSpace(incoming.TriggeredAt)
	}
	if len(incoming.Metadata) > 0 {
		if out.Metadata == nil {
			out.Metadata = map[string]string{}
		}
		for key, value := range redactPaasSensitiveFields(incoming.Metadata) {
			if strings.TrimSpace(key) == "" {
				continue
			}
			out.Metadata[key] = value
		}
	}
	return out
}

func severityRank(severity string) int {
	switch normalizePaasIncidentSeverity(severity) {
	case paasIncidentSeverityCritical:
		return 3
	case paasIncidentSeverityWarning:
		return 2
	default:
		return 1
	}
}

func sortPaasIncidentQueueEntries(rows []paasIncidentQueueEntry) {
	sort.SliceStable(rows, func(i, j int) bool {
		left := parsePaasIncidentQueueTimestamp(rows[i].LastSeen)
		right := parsePaasIncidentQueueTimestamp(rows[j].LastSeen)
		if left.Equal(right) {
			return rows[i].Key < rows[j].Key
		}
		return left.After(right)
	})
}

func parsePaasIncidentQueueTimestamp(value string) time.Time {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func loadPaasIncidentQueueSummary(limit int) ([]paasIncidentQueueEntry, string, error) {
	if limit < 1 {
		limit = 1
	}
	rows, path, err := loadPaasIncidentQueueEntries(currentPaasContext())
	if err != nil {
		return nil, "", err
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, path, nil
}

func mustSyncPaasIncidentQueueFromCollectors(limit, maxEntries int, maxAge time.Duration) paasIncidentQueueSyncResult {
	result, err := syncPaasIncidentQueueFromCollectors(limit, maxEntries, maxAge)
	if err != nil {
		fatal(fmt.Errorf("sync incident queue: %w", err))
	}
	return result
}

