package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr string

	GitHubAppID        int64
	GitHubAppSlug      string
	GitHubWebhookSecret string
	GitHubPrivateKeyPEM string

	DatabasePath string

	BaseURL string
}

func Load() (Config, error) {
	cfg := Config{
		Addr:         env("RP_ADDR", ":8080"),
		BaseURL:      strings.TrimRight(env("RP_BASE_URL", ""), "/"),
		DatabasePath: env("RP_DB_PATH", "data/releaseparty.sqlite"),
		GitHubAppSlug: env("GITHUB_APP_SLUG", ""),
		GitHubWebhookSecret: env("GITHUB_APP_WEBHOOK_SECRET", ""),
		GitHubPrivateKeyPEM: env("GITHUB_APP_PRIVATE_KEY_PEM", ""),
	}

	if v := strings.TrimSpace(env("GITHUB_APP_ID", "")); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Config{}, err
		}
		cfg.GitHubAppID = n
	}
	if cfg.GitHubPrivateKeyPEM == "" {
		if path := strings.TrimSpace(env("GITHUB_APP_PRIVATE_KEY_PATH", "")); path != "" {
			b, err := os.ReadFile(path)
			if err != nil {
				return Config{}, err
			}
			cfg.GitHubPrivateKeyPEM = string(b)
		}
	}

	if cfg.GitHubAppID == 0 {
		return Config{}, errors.New("missing GITHUB_APP_ID")
	}
	if strings.TrimSpace(cfg.GitHubPrivateKeyPEM) == "" {
		return Config{}, errors.New("missing GITHUB_APP_PRIVATE_KEY_PEM or GITHUB_APP_PRIVATE_KEY_PATH")
	}
	if strings.TrimSpace(cfg.GitHubWebhookSecret) == "" {
		return Config{}, errors.New("missing GITHUB_APP_WEBHOOK_SECRET")
	}
	if strings.TrimSpace(cfg.GitHubAppSlug) == "" {
		return Config{}, errors.New("missing GITHUB_APP_SLUG")
	}
	if cfg.BaseURL == "" {
		return Config{}, errors.New("missing RP_BASE_URL (public https base url for GitHub webhook delivery + UI links)")
	}

	return cfg, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

