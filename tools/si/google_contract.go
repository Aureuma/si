package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"si/tools/si/internal/googleplacesbridge"
)

type googlePlacesRuntimeContext struct {
	AccountAlias  string
	ProjectID     string
	Environment   string
	APIKey        string
	LanguageCode  string
	RegionCode    string
	Source        string
	BaseURL       string
	SessionSource string
}

type googlePlacesBridgeClient interface {
	Do(ctx context.Context, req googleplacesbridge.Request) (googleplacesbridge.Response, error)
	ListAll(ctx context.Context, req googleplacesbridge.Request, maxPages int, tokenField string) ([]map[string]any, error)
}

func normalizeGoogleEnvironment(raw string) string {
	env := strings.ToLower(strings.TrimSpace(raw))
	switch env {
	case "prod", "staging", "dev":
		return env
	default:
		return ""
	}
}

func parseGoogleEnvironment(raw string) (string, error) {
	env := normalizeGoogleEnvironment(raw)
	if env == "" {
		if strings.TrimSpace(raw) == "" {
			return "", fmt.Errorf("environment required (prod|staging|dev)")
		}
		if strings.EqualFold(strings.TrimSpace(raw), "test") {
			return "", fmt.Errorf("environment `test` is not supported; use `staging` or `dev`")
		}
		return "", fmt.Errorf("invalid environment %q (expected prod|staging|dev)", raw)
	}
	return env, nil
}

func googleAccountAliases(settings Settings) []string {
	if len(settings.Google.Accounts) == 0 {
		return nil
	}
	out := make([]string, 0, len(settings.Google.Accounts))
	for alias := range settings.Google.Accounts {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

func buildGooglePlacesClient(runtime googlePlacesRuntimeContext) (*googleplacesbridge.Client, error) {
	settings := loadSettingsOrDefault()
	cfg := googleplacesbridge.ClientConfig{
		APIKey:     runtime.APIKey,
		BaseURL:    runtime.BaseURL,
		Timeout:    30 * time.Second,
		MaxRetries: 2,
		LogPath:    resolveGooglePlacesLogPath(settings),
		LogContext: map[string]string{
			"account_alias": runtime.AccountAlias,
			"project_id":    runtime.ProjectID,
			"environment":   runtime.Environment,
			"language_code": runtime.LanguageCode,
			"region_code":   runtime.RegionCode,
		},
	}
	return googleplacesbridge.NewClient(cfg)
}

func formatGooglePlacesContext(runtime googlePlacesRuntimeContext) string {
	account := strings.TrimSpace(runtime.AccountAlias)
	if account == "" {
		account = "(default)"
	}
	project := strings.TrimSpace(runtime.ProjectID)
	if project == "" {
		project = "-"
	}
	language := strings.TrimSpace(runtime.LanguageCode)
	if language == "" {
		language = "-"
	}
	region := strings.TrimSpace(runtime.RegionCode)
	if region == "" {
		region = "-"
	}
	return fmt.Sprintf("account=%s (%s), env=%s, language=%s, region=%s", account, project, runtime.Environment, language, region)
}

func resolveGooglePlacesLogPath(settings Settings) string {
	if value := strings.TrimSpace(os.Getenv("SI_GOOGLE_PLACES_LOG_FILE")); value != "" {
		return value
	}
	if value := strings.TrimSpace(settings.Google.LogFile); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".si", "logs", "google-places.log")
}

func googleAccountEnvPrefix(alias string, account GoogleAccountEntry) string {
	if prefix := strings.TrimSpace(account.VaultPrefix); prefix != "" {
		if strings.HasSuffix(prefix, "_") {
			return strings.ToUpper(prefix)
		}
		return strings.ToUpper(prefix) + "_"
	}
	alias = slugUpper(alias)
	if alias == "" {
		return ""
	}
	return "GOOGLE_" + alias + "_"
}

func resolveGoogleEnv(alias string, account GoogleAccountEntry, key string) string {
	prefix := googleAccountEnvPrefix(alias, account)
	if prefix != "" {
		if value := strings.TrimSpace(os.Getenv(prefix + key)); value != "" {
			return value
		}
	}
	return ""
}

func mustGooglePlacesClient(input googlePlacesRuntimeContextInput) (googlePlacesRuntimeContext, googlePlacesBridgeClient) {
	runtime, err := resolveGooglePlacesRuntimeContext(input)
	if err != nil {
		fatal(err)
	}
	client, err := buildGooglePlacesClient(runtime)
	if err != nil {
		fatal(err)
	}
	return runtime, client
}

func printGooglePlacesContextBanner(runtime googlePlacesRuntimeContext, jsonOut bool) {
	if jsonOut {
		return
	}
	fmt.Printf("%s %s\n", styleDim("google places context:"), formatGooglePlacesContext(runtime))
}
