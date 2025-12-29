package beam

import (
	"fmt"
	"strings"
)

func WorkflowID(kind string, taskID int) string {
	base := strings.ToLower(strings.TrimSpace(kind))
	if base == "" {
		base = "beam"
	}
	base = strings.ReplaceAll(base, ".", "-")
	if taskID <= 0 {
		return fmt.Sprintf("beam-%s", base)
	}
	return fmt.Sprintf("beam-%s-%d", base, taskID)
}

func normalizeKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

func parseState(notes string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(notes, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[") || !strings.Contains(line, "]=") {
			continue
		}
		end := strings.Index(line, "]=")
		if end <= 1 {
			continue
		}
		key := strings.TrimSpace(line[1:end])
		val := strings.TrimSpace(line[end+2:])
		if key != "" {
			out[key] = val
		}
	}
	return out
}

func atoiDefault(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	n := def
	_, _ = fmt.Sscanf(s, "%d", &n)
	return n
}

func parseCSVList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func defaultCodexResetPaths() []string {
	return []string{
		"/root/.codex",
		"/root/.config/openai-codex",
		"/root/.config/codex",
		"/root/.cache/openai-codex",
		"/root/.cache/codex",
	}
}
