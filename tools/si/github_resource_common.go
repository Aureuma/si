package main

import (
	"fmt"
	"strconv"
	"strings"
)

func parseGitHubOwnerRepo(value string, defaultOwner string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", fmt.Errorf("owner/repo is required")
	}
	parts := strings.Split(value, "/")
	if len(parts) == 1 {
		owner := strings.TrimSpace(defaultOwner)
		repo := strings.TrimSpace(parts[0])
		if owner == "" {
			return "", "", fmt.Errorf("owner is required (use <owner/repo> or --owner)")
		}
		if repo == "" {
			return "", "", fmt.Errorf("repo is required")
		}
		return owner, repo, nil
	}
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid owner/repo %q", value)
	}
	owner := strings.TrimSpace(parts[0])
	repo := strings.TrimSpace(parts[1])
	if owner == "" || repo == "" {
		return "", "", fmt.Errorf("invalid owner/repo %q", value)
	}
	return owner, repo, nil
}

func parseGitHubNumber(raw string, name string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid %s %q", name, raw)
	}
	return value, nil
}

func parseGitHubBodyParams(values []string) map[string]any {
	out := map[string]any{}
	for key, value := range parseGitHubParams(values) {
		out[key] = value
	}
	return out
}
