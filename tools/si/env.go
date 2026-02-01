package main

import "strings"

func filterEnv(env []string) []string {
	if len(env) == 0 {
		return nil
	}
	filtered := make([]string, 0, len(env))
	seen := map[string]int{}
	for _, entry := range env {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		key := entry
		if idx := strings.Index(entry, "="); idx >= 0 {
			key = entry[:idx]
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if existing, ok := seen[key]; ok {
			filtered[existing] = entry
			continue
		}
		seen[key] = len(filtered)
		filtered = append(filtered, entry)
	}
	return filtered
}
