package main

import (
	"os"
	"strconv"
	"strings"
)

const (
	defaultBrowserContainer = "si-playwright-mcp-headed"
	defaultBrowserMCPPort   = 8931
)

func envOrInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}
