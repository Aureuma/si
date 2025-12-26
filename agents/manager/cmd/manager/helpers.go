package main

import (
	"os"
	"strconv"
	"strings"

	"silexa/agents/manager/internal/state"
)

func findDyadTask(tasks []state.DyadTask, id int) (state.DyadTask, bool) {
	for _, t := range tasks {
		if t.ID == id {
			return t, true
		}
	}
	return state.DyadTask{}, false
}

func boolEnv(key string, def bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}

func intEnv(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	if v, err := strconv.Atoi(raw); err == nil {
		return v
	}
	return def
}
