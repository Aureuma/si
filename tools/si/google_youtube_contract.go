package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"si/tools/si/internal/youtubebridge"
)

type googleYouTubeRuntimeContext struct {
	AccountAlias  string
	ProjectID     string
	Environment   string
	AuthMode      string
	APIKey        string
	BaseURL       string
	UploadBaseURL string
	LanguageCode  string
	RegionCode    string
	OAuth         googleYouTubeOAuthRuntime
	Source        string
	TokenSource   string
	SessionSource string
}

type googleYouTubeOAuthRuntime struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	AccessToken  string
	RefreshToken string
}

type googleYouTubeRuntimeContextInput struct {
	AccountFlag       string
	EnvFlag           string
	AuthModeFlag      string
	APIKeyFlag        string
	BaseURLFlag       string
	UploadBaseURLFlag string
	ProjectIDFlag     string
	LanguageFlag      string
	RegionFlag        string
	ClientIDFlag      string
	ClientSecretFlag  string
	RedirectURIFlag   string
	AccessTokenFlag   string
	RefreshTokenFlag  string
}

type googleYouTubeBridgeClient interface {
	Do(ctx context.Context, req youtubebridge.Request) (youtubebridge.Response, error)
	ListAll(ctx context.Context, req youtubebridge.Request, maxPages int) ([]map[string]any, error)
}

func normalizeGoogleYouTubeAuthMode(raw string) string {
	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "api-key", "apikey", "key":
		return "api-key"
	case "oauth", "oauth2", "bearer":
		return "oauth"
	default:
		return ""
	}
}

func parseGoogleYouTubeAuthMode(raw string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	switch trimmed {
	case "service-account", "service", "sa":
		return "", fmt.Errorf("service-account auth is not supported for youtube user/channel workflows; use api-key or oauth")
	}
	mode := normalizeGoogleYouTubeAuthMode(raw)
	if mode == "" {
		if strings.TrimSpace(raw) == "" {
			return "", fmt.Errorf("auth mode required (api-key|oauth)")
		}
		return "", fmt.Errorf("invalid auth mode %q (expected api-key|oauth)", raw)
	}
	return mode, nil
}

func googleYouTubeAccountAliases(settings Settings) []string {
	if len(settings.Google.YouTube.Accounts) == 0 {
		return nil
	}
	out := make([]string, 0, len(settings.Google.YouTube.Accounts))
	for alias := range settings.Google.YouTube.Accounts {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

func resolveGoogleYouTubeRuntimeContext(input googleYouTubeRuntimeContextInput) (googleYouTubeRuntimeContext, error) {
	settings := loadSettingsOrDefault()
	alias, account := resolveGoogleYouTubeAccountSelection(settings, input.AccountFlag)

	env := strings.TrimSpace(input.EnvFlag)
	if env == "" {
		env = strings.TrimSpace(settings.Google.DefaultEnv)
	}
	if env == "" {
		env = strings.TrimSpace(os.Getenv("GOOGLE_DEFAULT_ENV"))
	}
	if env == "" {
		env = "prod"
	}
	parsedEnv, err := parseGoogleEnvironment(env)
	if err != nil {
		return googleYouTubeRuntimeContext{}, err
	}

	authMode := strings.TrimSpace(input.AuthModeFlag)
	if authMode == "" {
		authMode = strings.TrimSpace(settings.Google.YouTube.DefaultAuthMode)
	}
	if authMode == "" {
		authMode = strings.TrimSpace(os.Getenv("GOOGLE_YOUTUBE_DEFAULT_AUTH_MODE"))
	}
	if authMode == "" {
		authMode = "api-key"
	}
	authMode = normalizeGoogleYouTubeAuthMode(authMode)
	if authMode == "" {
		return googleYouTubeRuntimeContext{}, fmt.Errorf("invalid auth mode %q (expected api-key|oauth)", input.AuthModeFlag)
	}

	baseURL := strings.TrimSpace(input.BaseURLFlag)
	if baseURL == "" {
		baseURL = strings.TrimSpace(settings.Google.YouTube.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(settings.Google.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("GOOGLE_YOUTUBE_API_BASE_URL"))
	}
	if baseURL == "" {
		baseURL = "https://www.googleapis.com"
	}

	uploadBaseURL := strings.TrimSpace(input.UploadBaseURLFlag)
	if uploadBaseURL == "" {
		uploadBaseURL = strings.TrimSpace(settings.Google.YouTube.UploadBaseURL)
	}
	if uploadBaseURL == "" {
		uploadBaseURL = strings.TrimSpace(os.Getenv("GOOGLE_YOUTUBE_UPLOAD_BASE_URL"))
	}
	if uploadBaseURL == "" {
		uploadBaseURL = "https://www.googleapis.com/upload"
	}

	projectID, projectSource := resolveGoogleYouTubeProjectID(alias, account, strings.TrimSpace(input.ProjectIDFlag))
	apiKey, keySource := resolveGoogleYouTubeAPIKey(alias, account, parsedEnv, strings.TrimSpace(input.APIKeyFlag))

	clientID, clientIDSource := resolveGoogleYouTubeClientID(alias, account, strings.TrimSpace(input.ClientIDFlag))
	clientSecret, clientSecretSource := resolveGoogleYouTubeClientSecret(alias, account, strings.TrimSpace(input.ClientSecretFlag))
	redirectURI, redirectSource := resolveGoogleYouTubeRedirectURI(alias, account, strings.TrimSpace(input.RedirectURIFlag))
	accessToken, accessSource := resolveGoogleYouTubeAccessToken(alias, account, strings.TrimSpace(input.AccessTokenFlag))
	refreshToken, refreshSource := resolveGoogleYouTubeRefreshToken(alias, account, parsedEnv, strings.TrimSpace(input.RefreshTokenFlag))

	if authMode == "api-key" && strings.TrimSpace(apiKey) == "" {
		prefix := googleYouTubeAccountEnvPrefix(alias, account)
		if prefix == "" {
			prefix = "GOOGLE_<ACCOUNT>_"
		}
		return googleYouTubeRuntimeContext{}, fmt.Errorf("youtube api key not found (set --api-key, %sYOUTUBE_API_KEY, or GOOGLE_YOUTUBE_API_KEY)", prefix)
	}
	if authMode == "oauth" && strings.TrimSpace(clientID) == "" {
		prefix := googleYouTubeAccountEnvPrefix(alias, account)
		if prefix == "" {
			prefix = "GOOGLE_<ACCOUNT>_"
		}
		return googleYouTubeRuntimeContext{}, fmt.Errorf("youtube oauth client id not found (set --client-id, %sYOUTUBE_CLIENT_ID, or GOOGLE_YOUTUBE_CLIENT_ID)", prefix)
	}

	languageCode, languageSource := resolveGoogleYouTubeLanguageCode(alias, account, strings.TrimSpace(input.LanguageFlag))
	regionCode, regionSource := resolveGoogleYouTubeRegionCode(alias, account, strings.TrimSpace(input.RegionFlag))

	source := strings.Join(nonEmpty(keySource, projectSource, languageSource, regionSource, clientIDSource, clientSecretSource, redirectSource), ",")
	tokenSource := strings.Join(nonEmpty(accessSource, refreshSource), ",")

	runtime := googleYouTubeRuntimeContext{
		AccountAlias:  alias,
		ProjectID:     projectID,
		Environment:   parsedEnv,
		AuthMode:      authMode,
		APIKey:        apiKey,
		BaseURL:       baseURL,
		UploadBaseURL: uploadBaseURL,
		LanguageCode:  languageCode,
		RegionCode:    regionCode,
		OAuth: googleYouTubeOAuthRuntime{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURI:  redirectURI,
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
		},
		Source:      source,
		TokenSource: tokenSource,
	}

	if authMode == "oauth" && strings.TrimSpace(runtime.OAuth.AccessToken) == "" && strings.TrimSpace(runtime.OAuth.RefreshToken) == "" {
		if entry, ok := loadGoogleOAuthTokenEntry(alias, parsedEnv); ok {
			runtime.OAuth.AccessToken = strings.TrimSpace(entry.AccessToken)
			if strings.TrimSpace(runtime.OAuth.RefreshToken) == "" {
				runtime.OAuth.RefreshToken = strings.TrimSpace(entry.RefreshToken)
			}
			runtime.SessionSource = "store"
		}
	}

	return runtime, nil
}

func resolveGoogleYouTubeAccountSelection(settings Settings, accountFlag string) (string, GoogleYouTubeAccountEntry) {
	selected := strings.TrimSpace(accountFlag)
	if selected == "" {
		selected = strings.TrimSpace(settings.Google.DefaultAccount)
	}
	if selected == "" {
		selected = strings.TrimSpace(os.Getenv("GOOGLE_DEFAULT_ACCOUNT"))
	}
	if selected == "" {
		aliases := googleYouTubeAccountAliases(settings)
		if len(aliases) == 1 {
			selected = aliases[0]
		}
	}
	if selected == "" {
		return "", GoogleYouTubeAccountEntry{}
	}
	if entry, ok := settings.Google.YouTube.Accounts[selected]; ok {
		return selected, entry
	}
	return selected, GoogleYouTubeAccountEntry{}
}

func googleYouTubeAccountEnvPrefix(alias string, account GoogleYouTubeAccountEntry) string {
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

func resolveGoogleYouTubeEnv(alias string, account GoogleYouTubeAccountEntry, key string) string {
	prefix := googleYouTubeAccountEnvPrefix(alias, account)
	if prefix != "" {
		if value := strings.TrimSpace(os.Getenv(prefix + key)); value != "" {
			return value
		}
	}
	return ""
}

func resolveGoogleYouTubeProjectID(alias string, account GoogleYouTubeAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--project-id"
	}
	if value := strings.TrimSpace(account.ProjectID); value != "" {
		return value, "settings.project_id"
	}
	if ref := strings.TrimSpace(account.ProjectIDEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveGoogleYouTubeEnv(alias, account, "PROJECT_ID")); value != "" {
		return value, "env:" + googleYouTubeAccountEnvPrefix(alias, account) + "PROJECT_ID"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_PROJECT_ID")); value != "" {
		return value, "env:GOOGLE_PROJECT_ID"
	}
	return "", ""
}

func resolveGoogleYouTubeAPIKey(alias string, account GoogleYouTubeAccountEntry, env string, override string) (string, string) {
	if override != "" {
		return override, "flag:--api-key"
	}
	switch normalizeGoogleEnvironment(env) {
	case "prod":
		if ref := strings.TrimSpace(account.ProdYouTubeAPIKeyEnv); ref != "" {
			if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
				return value, "env:" + ref
			}
		}
		if value := strings.TrimSpace(resolveGoogleYouTubeEnv(alias, account, "PROD_YOUTUBE_API_KEY")); value != "" {
			return value, "env:" + googleYouTubeAccountEnvPrefix(alias, account) + "PROD_YOUTUBE_API_KEY"
		}
	case "staging":
		if ref := strings.TrimSpace(account.StagingYouTubeAPIKeyEnv); ref != "" {
			if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
				return value, "env:" + ref
			}
		}
		if value := strings.TrimSpace(resolveGoogleYouTubeEnv(alias, account, "STAGING_YOUTUBE_API_KEY")); value != "" {
			return value, "env:" + googleYouTubeAccountEnvPrefix(alias, account) + "STAGING_YOUTUBE_API_KEY"
		}
	case "dev":
		if ref := strings.TrimSpace(account.DevYouTubeAPIKeyEnv); ref != "" {
			if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
				return value, "env:" + ref
			}
		}
		if value := strings.TrimSpace(resolveGoogleYouTubeEnv(alias, account, "DEV_YOUTUBE_API_KEY")); value != "" {
			return value, "env:" + googleYouTubeAccountEnvPrefix(alias, account) + "DEV_YOUTUBE_API_KEY"
		}
	}
	if ref := strings.TrimSpace(account.YouTubeAPIKeyEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveGoogleYouTubeEnv(alias, account, "YOUTUBE_API_KEY")); value != "" {
		return value, "env:" + googleYouTubeAccountEnvPrefix(alias, account) + "YOUTUBE_API_KEY"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_YOUTUBE_API_KEY")); value != "" {
		return value, "env:GOOGLE_YOUTUBE_API_KEY"
	}
	return "", ""
}

func resolveGoogleYouTubeClientID(alias string, account GoogleYouTubeAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--client-id"
	}
	if ref := strings.TrimSpace(account.YouTubeClientIDEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveGoogleYouTubeEnv(alias, account, "YOUTUBE_CLIENT_ID")); value != "" {
		return value, "env:" + googleYouTubeAccountEnvPrefix(alias, account) + "YOUTUBE_CLIENT_ID"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_YOUTUBE_CLIENT_ID")); value != "" {
		return value, "env:GOOGLE_YOUTUBE_CLIENT_ID"
	}
	return "", ""
}

func resolveGoogleYouTubeClientSecret(alias string, account GoogleYouTubeAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--client-secret"
	}
	if ref := strings.TrimSpace(account.YouTubeClientSecretEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveGoogleYouTubeEnv(alias, account, "YOUTUBE_CLIENT_SECRET")); value != "" {
		return value, "env:" + googleYouTubeAccountEnvPrefix(alias, account) + "YOUTUBE_CLIENT_SECRET"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_YOUTUBE_CLIENT_SECRET")); value != "" {
		return value, "env:GOOGLE_YOUTUBE_CLIENT_SECRET"
	}
	return "", ""
}

func resolveGoogleYouTubeRedirectURI(alias string, account GoogleYouTubeAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--redirect-uri"
	}
	if ref := strings.TrimSpace(account.YouTubeRedirectURIEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveGoogleYouTubeEnv(alias, account, "YOUTUBE_REDIRECT_URI")); value != "" {
		return value, "env:" + googleYouTubeAccountEnvPrefix(alias, account) + "YOUTUBE_REDIRECT_URI"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_YOUTUBE_REDIRECT_URI")); value != "" {
		return value, "env:GOOGLE_YOUTUBE_REDIRECT_URI"
	}
	return "", ""
}

func resolveGoogleYouTubeRefreshToken(alias string, account GoogleYouTubeAccountEntry, env string, override string) (string, string) {
	if override != "" {
		return override, "flag:--refresh-token"
	}
	switch normalizeGoogleEnvironment(env) {
	case "prod":
		if value := strings.TrimSpace(resolveGoogleYouTubeEnv(alias, account, "PROD_YOUTUBE_REFRESH_TOKEN")); value != "" {
			return value, "env:" + googleYouTubeAccountEnvPrefix(alias, account) + "PROD_YOUTUBE_REFRESH_TOKEN"
		}
	case "staging":
		if value := strings.TrimSpace(resolveGoogleYouTubeEnv(alias, account, "STAGING_YOUTUBE_REFRESH_TOKEN")); value != "" {
			return value, "env:" + googleYouTubeAccountEnvPrefix(alias, account) + "STAGING_YOUTUBE_REFRESH_TOKEN"
		}
	case "dev":
		if value := strings.TrimSpace(resolveGoogleYouTubeEnv(alias, account, "DEV_YOUTUBE_REFRESH_TOKEN")); value != "" {
			return value, "env:" + googleYouTubeAccountEnvPrefix(alias, account) + "DEV_YOUTUBE_REFRESH_TOKEN"
		}
	}
	if ref := strings.TrimSpace(account.YouTubeRefreshTokenEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveGoogleYouTubeEnv(alias, account, "YOUTUBE_REFRESH_TOKEN")); value != "" {
		return value, "env:" + googleYouTubeAccountEnvPrefix(alias, account) + "YOUTUBE_REFRESH_TOKEN"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_YOUTUBE_REFRESH_TOKEN")); value != "" {
		return value, "env:GOOGLE_YOUTUBE_REFRESH_TOKEN"
	}
	return "", ""
}

func resolveGoogleYouTubeAccessToken(alias string, account GoogleYouTubeAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--access-token"
	}
	if value := strings.TrimSpace(resolveGoogleYouTubeEnv(alias, account, "YOUTUBE_ACCESS_TOKEN")); value != "" {
		return value, "env:" + googleYouTubeAccountEnvPrefix(alias, account) + "YOUTUBE_ACCESS_TOKEN"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_YOUTUBE_ACCESS_TOKEN")); value != "" {
		return value, "env:GOOGLE_YOUTUBE_ACCESS_TOKEN"
	}
	return "", ""
}

func resolveGoogleYouTubeLanguageCode(alias string, account GoogleYouTubeAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--language"
	}
	if value := strings.TrimSpace(account.DefaultLanguageCode); value != "" {
		return value, "settings.default_language_code"
	}
	if value := strings.TrimSpace(resolveGoogleYouTubeEnv(alias, account, "DEFAULT_LANGUAGE_CODE")); value != "" {
		return value, "env:" + googleYouTubeAccountEnvPrefix(alias, account) + "DEFAULT_LANGUAGE_CODE"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_DEFAULT_LANGUAGE_CODE")); value != "" {
		return value, "env:GOOGLE_DEFAULT_LANGUAGE_CODE"
	}
	return "", ""
}

func resolveGoogleYouTubeRegionCode(alias string, account GoogleYouTubeAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--region"
	}
	if value := strings.TrimSpace(account.DefaultRegionCode); value != "" {
		return value, "settings.default_region_code"
	}
	if value := strings.TrimSpace(resolveGoogleYouTubeEnv(alias, account, "DEFAULT_REGION_CODE")); value != "" {
		return value, "env:" + googleYouTubeAccountEnvPrefix(alias, account) + "DEFAULT_REGION_CODE"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_DEFAULT_REGION_CODE")); value != "" {
		return value, "env:GOOGLE_DEFAULT_REGION_CODE"
	}
	return "", ""
}

func buildGoogleYouTubeClient(runtime googleYouTubeRuntimeContext) (*youtubebridge.Client, error) {
	settings := loadSettingsOrDefault()
	cfg := youtubebridge.ClientConfig{
		AuthMode:      youtubebridge.AuthMode(runtime.AuthMode),
		APIKey:        runtime.APIKey,
		BaseURL:       runtime.BaseURL,
		UploadBaseURL: runtime.UploadBaseURL,
		Timeout:       30 * time.Second,
		MaxRetries:    2,
		LogPath:       resolveGoogleYouTubeLogPath(settings),
		LogContext: map[string]string{
			"account_alias": runtime.AccountAlias,
			"project_id":    runtime.ProjectID,
			"environment":   runtime.Environment,
			"auth_mode":     runtime.AuthMode,
		},
	}
	if runtime.AuthMode == "oauth" {
		provider, err := buildGoogleYouTubeTokenProvider(runtime)
		if err != nil {
			return nil, err
		}
		cfg.TokenProvider = provider
	}
	return youtubebridge.NewClient(cfg)
}

func resolveGoogleYouTubeLogPath(settings Settings) string {
	if value := strings.TrimSpace(os.Getenv("SI_GOOGLE_YOUTUBE_LOG_FILE")); value != "" {
		return value
	}
	if value := strings.TrimSpace(settings.Google.LogFile); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".si", "logs", "google-youtube.log")
}

func formatGoogleYouTubeContext(runtime googleYouTubeRuntimeContext) string {
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
	return fmt.Sprintf("account=%s (%s), env=%s, auth=%s, language=%s, region=%s", account, project, runtime.Environment, runtime.AuthMode, language, region)
}

func mustGoogleYouTubeClient(input googleYouTubeRuntimeContextInput) (googleYouTubeRuntimeContext, googleYouTubeBridgeClient) {
	runtime, err := resolveGoogleYouTubeRuntimeContext(input)
	if err != nil {
		fatal(err)
	}
	client, err := buildGoogleYouTubeClient(runtime)
	if err != nil {
		fatal(err)
	}
	return runtime, client
}

func printGoogleYouTubeContextBanner(runtime googleYouTubeRuntimeContext, jsonOut bool) {
	if jsonOut {
		return
	}
	fmt.Printf("%s %s\n", styleDim("google youtube context:"), formatGoogleYouTubeContext(runtime))
}
