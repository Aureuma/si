package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"si/tools/si/internal/cloudflarebridge"
)

type cloudflareRuntimeContext struct {
	AccountAlias string
	AccountID    string
	Environment  string
	ZoneID       string
	ZoneName     string
	APIToken     string
	Source       string
	BaseURL      string
}

type cloudflareBridgeClient interface {
	Do(ctx context.Context, req cloudflarebridge.Request) (cloudflarebridge.Response, error)
	ListAll(ctx context.Context, req cloudflarebridge.Request, maxPages int) ([]map[string]any, error)
}

func normalizeCloudflareEnvironment(raw string) string {
	env := strings.ToLower(strings.TrimSpace(raw))
	switch env {
	case "prod", "staging", "dev":
		return env
	default:
		return ""
	}
}

func parseCloudflareEnvironment(raw string) (string, error) {
	env := normalizeCloudflareEnvironment(raw)
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

func cloudflareAccountAliases(settings Settings) []string {
	if len(settings.Cloudflare.Accounts) == 0 {
		return nil
	}
	out := make([]string, 0, len(settings.Cloudflare.Accounts))
	for alias := range settings.Cloudflare.Accounts {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

func buildCloudflareClient(runtime cloudflareRuntimeContext) (*cloudflarebridge.Client, error) {
	settings := loadSettingsOrDefault()
	cfg := cloudflarebridge.ClientConfig{
		APIToken:   runtime.APIToken,
		BaseURL:    runtime.BaseURL,
		Timeout:    30 * time.Second,
		MaxRetries: 2,
		LogPath:    resolveCloudflareLogPath(settings),
		LogContext: map[string]string{
			"account_alias": runtime.AccountAlias,
			"account_id":    runtime.AccountID,
			"environment":   runtime.Environment,
			"zone_id":       runtime.ZoneID,
		},
	}
	return cloudflarebridge.NewClient(cfg)
}

func formatCloudflareContext(runtime cloudflareRuntimeContext) string {
	account := strings.TrimSpace(runtime.AccountAlias)
	if account == "" {
		account = "(default)"
	}
	accountID := strings.TrimSpace(runtime.AccountID)
	if accountID == "" {
		accountID = "-"
	}
	zone := strings.TrimSpace(runtime.ZoneID)
	if zone == "" {
		zone = strings.TrimSpace(runtime.ZoneName)
	}
	if zone == "" {
		zone = "-"
	}
	return fmt.Sprintf("account=%s (%s), env=%s, zone=%s", account, accountID, runtime.Environment, zone)
}

func resolveCloudflareLogPath(settings Settings) string {
	if value := strings.TrimSpace(os.Getenv("SI_CLOUDFLARE_LOG_FILE")); value != "" {
		return value
	}
	if value := strings.TrimSpace(settings.Cloudflare.LogFile); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".si", "logs", "cloudflare.log")
}

func cloudflareAccountEnvPrefix(alias string, account CloudflareAccountEntry) string {
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
	return "CLOUDFLARE_" + alias + "_"
}

func resolveCloudflareEnv(alias string, account CloudflareAccountEntry, key string) string {
	prefix := cloudflareAccountEnvPrefix(alias, account)
	if prefix != "" {
		if value := strings.TrimSpace(os.Getenv(prefix + key)); value != "" {
			return value
		}
	}
	return ""
}

func mustCloudflareClient(account string, env string, zone string, zoneID string, token string, baseURL string, accountID string) (cloudflareRuntimeContext, cloudflareBridgeClient) {
	runtime, err := resolveCloudflareRuntimeContext(cloudflareRuntimeContextInput{
		AccountFlag:   account,
		EnvFlag:       env,
		ZoneFlag:      zone,
		ZoneIDFlag:    zoneID,
		TokenFlag:     token,
		BaseURLFlag:   baseURL,
		AccountIDFlag: accountID,
	})
	if err != nil {
		fatal(err)
	}
	client, err := buildCloudflareClient(runtime)
	if err != nil {
		fatal(err)
	}
	return runtime, client
}

func printCloudflareContextBanner(runtime cloudflareRuntimeContext, jsonOut bool) {
	if jsonOut {
		return
	}
	fmt.Printf("%s %s\n", styleDim("cloudflare context:"), formatCloudflareContext(runtime))
}
