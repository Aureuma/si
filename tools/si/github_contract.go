package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"si/tools/si/internal/githubbridge"
)

type githubRuntimeContext struct {
	AccountAlias string
	Owner        string
	AuthMode     githubbridge.AuthMode
	Source       string
	BaseURL      string
	Provider     githubbridge.TokenProvider
}

type githubBridgeClient interface {
	Do(ctx context.Context, req githubbridge.Request) (githubbridge.Response, error)
	ListAll(ctx context.Context, req githubbridge.Request, maxPages int) ([]map[string]any, error)
}

func buildGithubClient(runtime githubRuntimeContext) (*githubbridge.Client, error) {
	settings := loadSettingsOrDefault()
	cfg := githubbridge.ClientConfig{
		BaseURL:    runtime.BaseURL,
		UserAgent:  "si-github/1.0",
		Timeout:    30 * time.Second,
		MaxRetries: 2,
		Provider:   runtime.Provider,
		LogPath:    resolveGithubLogPath(settings),
		LogContext: map[string]string{
			"account_alias": runtime.AccountAlias,
			"owner":         runtime.Owner,
			"auth_mode":     string(runtime.AuthMode),
		},
	}
	return githubbridge.NewClient(cfg)
}

func resolveGithubLogPath(settings Settings) string {
	if value := strings.TrimSpace(os.Getenv("SI_GITHUB_LOG_FILE")); value != "" {
		return value
	}
	if value := strings.TrimSpace(settings.Github.LogFile); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".si", "logs", "github.log")
}

func formatGithubContext(runtime githubRuntimeContext) string {
	account := strings.TrimSpace(runtime.AccountAlias)
	if account == "" {
		account = "(default)"
	}
	owner := strings.TrimSpace(runtime.Owner)
	if owner == "" {
		owner = "-"
	}
	baseURL := strings.TrimSpace(runtime.BaseURL)
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	return fmt.Sprintf("account=%s owner=%s auth=%s base=%s", account, owner, runtime.AuthMode, baseURL)
}
