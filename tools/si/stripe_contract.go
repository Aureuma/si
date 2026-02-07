package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"si/tools/si/internal/stripebridge"
)

type stripeRuntimeContext struct {
	AccountAlias string
	AccountID    string
	Environment  stripebridge.Environment
	APIKey       string
	Source       string
	BaseURL      string
}

type stripeCredentialProvider interface {
	Resolve(account string, env string, apiKey string) (stripeRuntimeContext, error)
}

type stripeBridgeClient interface {
	Do(ctx context.Context, req stripebridge.Request) (stripebridge.Response, error)
	ListAll(ctx context.Context, path string, params map[string]string, limit int) ([]map[string]any, error)
	ExecuteCRUD(ctx context.Context, spec stripebridge.ObjectSpec, op stripebridge.CRUDOp, id string, params map[string]string, idempotencyKey string) (stripebridge.Response, error)
}

func normalizeStripeEnvironment(raw string) string {
	env := strings.ToLower(strings.TrimSpace(raw))
	switch env {
	case "live", "sandbox":
		return env
	default:
		return ""
	}
}

func parseStripeEnvironment(raw string) (stripebridge.Environment, error) {
	return stripebridge.ParseEnvironment(raw)
}

func stripeAccountAliases(settings Settings) []string {
	if len(settings.Stripe.Accounts) == 0 {
		return nil
	}
	out := make([]string, 0, len(settings.Stripe.Accounts))
	for alias := range settings.Stripe.Accounts {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

func buildStripeClient(runtime stripeRuntimeContext) (*stripebridge.Client, error) {
	settings := loadSettingsOrDefault()
	cfg := stripebridge.ClientConfig{
		APIKey:            runtime.APIKey,
		AccountID:         runtime.AccountID,
		BaseURL:           runtime.BaseURL,
		Timeout:           30 * time.Second,
		MaxNetworkRetries: 2,
		LogPath:           resolveStripeLogPath(settings),
		LogContext: map[string]string{
			"account_alias": runtime.AccountAlias,
			"account_id":    runtime.AccountID,
			"environment":   string(runtime.Environment),
		},
	}
	client, err := stripebridge.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func formatStripeContext(runtime stripeRuntimeContext) string {
	account := strings.TrimSpace(runtime.AccountAlias)
	if account == "" {
		account = "(default)"
	}
	accountID := strings.TrimSpace(runtime.AccountID)
	if accountID == "" {
		accountID = "-"
	}
	return fmt.Sprintf("account=%s (%s), env=%s", account, accountID, runtime.Environment)
}

func resolveStripeLogPath(settings Settings) string {
	if value := strings.TrimSpace(os.Getenv("SI_STRIPE_LOG_FILE")); value != "" {
		return value
	}
	if value := strings.TrimSpace(settings.Stripe.LogFile); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".si", "logs", "stripe.log")
}
