package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"si/tools/si/internal/googleplaybridge"
)

const (
	googlePlayOAuthScope = "https://www.googleapis.com/auth/androidpublisher"
)

type googlePlayRuntimeContextInput struct {
	AccountFlag          string
	EnvFlag              string
	PackageFlag          string
	LanguageFlag         string
	ProjectIDFlag        string
	DeveloperAccountFlag string
	ServiceAccountJSON   string
	ServiceAccountFile   string
	BaseURLFlag          string
	UploadBaseURLFlag    string
	CustomAppBaseURLFlag string
}

type googlePlayRuntimeContext struct {
	AccountAlias        string
	ProjectID           string
	Environment         string
	Source              string
	TokenSource         string
	BaseURL             string
	UploadBaseURL       string
	CustomAppBaseURL    string
	DeveloperAccountID  string
	DefaultPackageName  string
	DefaultLanguageCode string

	ServiceAccountJSON  string
	ServiceAccountEmail string
	TokenURI            string
}

type googlePlayBridgeClient interface {
	Do(ctx context.Context, req googleplaybridge.Request) (googleplaybridge.Response, error)
	ListAll(ctx context.Context, req googleplaybridge.Request, maxPages int, tokenField string) ([]map[string]any, error)
}

type googleServiceAccountCredentials struct {
	Type         string `json:"type"`
	ProjectID    string `json:"project_id"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	ClientEmail  string `json:"client_email"`
	TokenURI     string `json:"token_uri"`
}

type googleServiceAccountTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

type googlePlayServiceAccountTokenProvider struct {
	creds      googleServiceAccountCredentials
	source     string
	httpClient *http.Client
	mu         sync.Mutex
	cached     googleplaybridge.Token
}

func googlePlayAccountAliases(settings Settings) []string {
	if len(settings.Google.Play.Accounts) == 0 {
		return nil
	}
	out := make([]string, 0, len(settings.Google.Play.Accounts))
	for alias := range settings.Google.Play.Accounts {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

func resolveGooglePlayAccountSelection(settings Settings, accountFlag string) (string, GooglePlayAccountEntry) {
	selected := strings.TrimSpace(accountFlag)
	if selected == "" {
		selected = strings.TrimSpace(settings.Google.DefaultAccount)
	}
	if selected == "" {
		selected = strings.TrimSpace(os.Getenv("GOOGLE_DEFAULT_ACCOUNT"))
	}
	if selected == "" {
		aliases := googlePlayAccountAliases(settings)
		if len(aliases) == 1 {
			selected = aliases[0]
		}
	}
	if selected == "" {
		return "", GooglePlayAccountEntry{}
	}
	if entry, ok := settings.Google.Play.Accounts[selected]; ok {
		return selected, entry
	}
	return selected, GooglePlayAccountEntry{}
}

func googlePlayAccountEnvPrefix(alias string, account GooglePlayAccountEntry) string {
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

func resolveGooglePlayEnv(alias string, account GooglePlayAccountEntry, key string) string {
	prefix := googlePlayAccountEnvPrefix(alias, account)
	if prefix != "" {
		if value := strings.TrimSpace(os.Getenv(prefix + key)); value != "" {
			return value
		}
	}
	return ""
}

func resolveGooglePlayProjectID(alias string, account GooglePlayAccountEntry, fallback GoogleAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--project-id"
	}
	if value := strings.TrimSpace(account.ProjectID); value != "" {
		return value, "settings.play.project_id"
	}
	if ref := strings.TrimSpace(account.ProjectIDEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveGooglePlayEnv(alias, account, "PROJECT_ID")); value != "" {
		return value, "env:" + googlePlayAccountEnvPrefix(alias, account) + "PROJECT_ID"
	}
	if value := strings.TrimSpace(fallback.ProjectID); value != "" {
		return value, "settings.project_id"
	}
	if ref := strings.TrimSpace(fallback.ProjectIDEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_PROJECT_ID")); value != "" {
		return value, "env:GOOGLE_PROJECT_ID"
	}
	return "", ""
}

func resolveGooglePlayServiceAccountJSON(alias string, account GooglePlayAccountEntry, env string, overrideJSON string, overrideFile string) (string, string, error) {
	if value := strings.TrimSpace(overrideJSON); value != "" {
		jsonValue, err := resolveGooglePlayJSONInput(value)
		return jsonValue, "flag:--service-account-json", err
	}
	if path := strings.TrimSpace(overrideFile); path != "" {
		jsonValue, err := resolveGooglePlayJSONFile(path)
		return jsonValue, "flag:--service-account-file", err
	}
	envNorm := normalizeGoogleEnvironment(env)
	switch envNorm {
	case "prod":
		if ref := strings.TrimSpace(account.ProdServiceAccountJSONEnv); ref != "" {
			if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
				jsonValue, err := resolveGooglePlayJSONInput(value)
				return jsonValue, "env:" + ref, err
			}
		}
		if value := strings.TrimSpace(resolveGooglePlayEnv(alias, account, "PROD_PLAY_SERVICE_ACCOUNT_JSON")); value != "" {
			jsonValue, err := resolveGooglePlayJSONInput(value)
			return jsonValue, "env:" + googlePlayAccountEnvPrefix(alias, account) + "PROD_PLAY_SERVICE_ACCOUNT_JSON", err
		}
	case "staging":
		if ref := strings.TrimSpace(account.StagingServiceAccountJSONEnv); ref != "" {
			if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
				jsonValue, err := resolveGooglePlayJSONInput(value)
				return jsonValue, "env:" + ref, err
			}
		}
		if value := strings.TrimSpace(resolveGooglePlayEnv(alias, account, "STAGING_PLAY_SERVICE_ACCOUNT_JSON")); value != "" {
			jsonValue, err := resolveGooglePlayJSONInput(value)
			return jsonValue, "env:" + googlePlayAccountEnvPrefix(alias, account) + "STAGING_PLAY_SERVICE_ACCOUNT_JSON", err
		}
	case "dev":
		if ref := strings.TrimSpace(account.DevServiceAccountJSONEnv); ref != "" {
			if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
				jsonValue, err := resolveGooglePlayJSONInput(value)
				return jsonValue, "env:" + ref, err
			}
		}
		if value := strings.TrimSpace(resolveGooglePlayEnv(alias, account, "DEV_PLAY_SERVICE_ACCOUNT_JSON")); value != "" {
			jsonValue, err := resolveGooglePlayJSONInput(value)
			return jsonValue, "env:" + googlePlayAccountEnvPrefix(alias, account) + "DEV_PLAY_SERVICE_ACCOUNT_JSON", err
		}
	}
	if ref := strings.TrimSpace(account.ServiceAccountJSONEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			jsonValue, err := resolveGooglePlayJSONInput(value)
			return jsonValue, "env:" + ref, err
		}
	}
	if value := strings.TrimSpace(resolveGooglePlayEnv(alias, account, "PLAY_SERVICE_ACCOUNT_JSON")); value != "" {
		jsonValue, err := resolveGooglePlayJSONInput(value)
		return jsonValue, "env:" + googlePlayAccountEnvPrefix(alias, account) + "PLAY_SERVICE_ACCOUNT_JSON", err
	}
	if value := strings.TrimSpace(account.ServiceAccountFile); value != "" {
		jsonValue, err := resolveGooglePlayJSONFile(value)
		return jsonValue, "settings.play.service_account_file", err
	}
	if ref := strings.TrimSpace(account.ServiceAccountFileEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			jsonValue, err := resolveGooglePlayJSONFile(value)
			return jsonValue, "env:" + ref, err
		}
	}
	if value := strings.TrimSpace(resolveGooglePlayEnv(alias, account, "PLAY_SERVICE_ACCOUNT_FILE")); value != "" {
		jsonValue, err := resolveGooglePlayJSONFile(value)
		return jsonValue, "env:" + googlePlayAccountEnvPrefix(alias, account) + "PLAY_SERVICE_ACCOUNT_FILE", err
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_PLAY_SERVICE_ACCOUNT_JSON")); value != "" {
		jsonValue, err := resolveGooglePlayJSONInput(value)
		return jsonValue, "env:GOOGLE_PLAY_SERVICE_ACCOUNT_JSON", err
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_PLAY_SERVICE_ACCOUNT_FILE")); value != "" {
		jsonValue, err := resolveGooglePlayJSONFile(value)
		return jsonValue, "env:GOOGLE_PLAY_SERVICE_ACCOUNT_FILE", err
	}
	return "", "", nil
}

func resolveGooglePlayJSONInput(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "@") {
		return resolveGooglePlayJSONFile(strings.TrimPrefix(value, "@"))
	}
	if strings.HasPrefix(value, "{") {
		return value, nil
	}
	if strings.HasSuffix(strings.ToLower(value), ".json") {
		if _, err := os.Stat(value); err == nil {
			return resolveGooglePlayJSONFile(value)
		}
	}
	return value, nil
}

func resolveGooglePlayJSONFile(path string) (string, error) {
	data, err := readLocalFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func resolveGooglePlayDeveloperAccountID(alias string, account GooglePlayAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--developer-account"
	}
	if value := strings.TrimSpace(account.DeveloperAccountID); value != "" {
		return value, "settings.play.developer_account_id"
	}
	if value := strings.TrimSpace(resolveGooglePlayEnv(alias, account, "PLAY_DEVELOPER_ACCOUNT_ID")); value != "" {
		return value, "env:" + googlePlayAccountEnvPrefix(alias, account) + "PLAY_DEVELOPER_ACCOUNT_ID"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_PLAY_DEVELOPER_ACCOUNT_ID")); value != "" {
		return value, "env:GOOGLE_PLAY_DEVELOPER_ACCOUNT_ID"
	}
	return "", ""
}

func resolveGooglePlayDefaultPackage(alias string, account GooglePlayAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--package"
	}
	if value := strings.TrimSpace(account.DefaultPackageName); value != "" {
		return value, "settings.play.default_package_name"
	}
	if value := strings.TrimSpace(resolveGooglePlayEnv(alias, account, "PLAY_PACKAGE_NAME")); value != "" {
		return value, "env:" + googlePlayAccountEnvPrefix(alias, account) + "PLAY_PACKAGE_NAME"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_PLAY_PACKAGE_NAME")); value != "" {
		return value, "env:GOOGLE_PLAY_PACKAGE_NAME"
	}
	return "", ""
}

func resolveGooglePlayDefaultLanguage(alias string, account GooglePlayAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--language"
	}
	if value := strings.TrimSpace(account.DefaultLanguageCode); value != "" {
		return value, "settings.play.default_language_code"
	}
	if value := strings.TrimSpace(resolveGooglePlayEnv(alias, account, "DEFAULT_LANGUAGE_CODE")); value != "" {
		return value, "env:" + googlePlayAccountEnvPrefix(alias, account) + "DEFAULT_LANGUAGE_CODE"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_DEFAULT_LANGUAGE_CODE")); value != "" {
		return value, "env:GOOGLE_DEFAULT_LANGUAGE_CODE"
	}
	return "", ""
}

func resolveGooglePlayRuntimeContext(input googlePlayRuntimeContextInput) (googlePlayRuntimeContext, error) {
	settings := loadSettingsOrDefault()
	alias, account := resolveGooglePlayAccountSelection(settings, input.AccountFlag)
	genericAccount := settings.Google.Accounts[alias]
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
		return googlePlayRuntimeContext{}, err
	}

	baseURL := strings.TrimSpace(input.BaseURLFlag)
	if baseURL == "" {
		baseURL = strings.TrimSpace(settings.Google.Play.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = "https://androidpublisher.googleapis.com"
	}
	uploadBaseURL := strings.TrimSpace(input.UploadBaseURLFlag)
	if uploadBaseURL == "" {
		uploadBaseURL = strings.TrimSpace(settings.Google.Play.UploadBaseURL)
	}
	if uploadBaseURL == "" {
		uploadBaseURL = "https://androidpublisher.googleapis.com"
	}
	customAppBaseURL := strings.TrimSpace(input.CustomAppBaseURLFlag)
	if customAppBaseURL == "" {
		customAppBaseURL = strings.TrimSpace(settings.Google.Play.CustomAppBaseURL)
	}
	if customAppBaseURL == "" {
		customAppBaseURL = "https://playcustomapp.googleapis.com"
	}

	projectID, projectSource := resolveGooglePlayProjectID(alias, account, genericAccount, strings.TrimSpace(input.ProjectIDFlag))
	serviceAccountJSON, tokenSource, err := resolveGooglePlayServiceAccountJSON(alias, account, parsedEnv, strings.TrimSpace(input.ServiceAccountJSON), strings.TrimSpace(input.ServiceAccountFile))
	if err != nil {
		return googlePlayRuntimeContext{}, err
	}
	if strings.TrimSpace(serviceAccountJSON) == "" {
		prefix := googlePlayAccountEnvPrefix(alias, account)
		if prefix == "" {
			prefix = "GOOGLE_<ACCOUNT>_"
		}
		return googlePlayRuntimeContext{}, fmt.Errorf("google play service account not found (set --service-account-json, --service-account-file, %sPLAY_SERVICE_ACCOUNT_JSON, or GOOGLE_PLAY_SERVICE_ACCOUNT_JSON)", prefix)
	}

	creds, err := parseGoogleServiceAccountCredentials(serviceAccountJSON)
	if err != nil {
		return googlePlayRuntimeContext{}, err
	}
	if projectID == "" && strings.TrimSpace(creds.ProjectID) != "" {
		projectID = strings.TrimSpace(creds.ProjectID)
		projectSource = "service_account.project_id"
	}
	developerAccountID, developerSource := resolveGooglePlayDeveloperAccountID(alias, account, strings.TrimSpace(input.DeveloperAccountFlag))
	defaultPackage, packageSource := resolveGooglePlayDefaultPackage(alias, account, strings.TrimSpace(input.PackageFlag))
	defaultLanguage, languageSource := resolveGooglePlayDefaultLanguage(alias, account, strings.TrimSpace(input.LanguageFlag))
	source := strings.Join(nonEmpty(projectSource, developerSource, packageSource, languageSource), ",")
	return googlePlayRuntimeContext{
		AccountAlias:        alias,
		ProjectID:           projectID,
		Environment:         parsedEnv,
		Source:              source,
		TokenSource:         tokenSource,
		BaseURL:             baseURL,
		UploadBaseURL:       uploadBaseURL,
		CustomAppBaseURL:    customAppBaseURL,
		DeveloperAccountID:  developerAccountID,
		DefaultPackageName:  defaultPackage,
		DefaultLanguageCode: defaultLanguage,
		ServiceAccountJSON:  serviceAccountJSON,
		ServiceAccountEmail: strings.TrimSpace(creds.ClientEmail),
		TokenURI:            strings.TrimSpace(creds.TokenURI),
	}, nil
}

func parseGoogleServiceAccountCredentials(raw string) (googleServiceAccountCredentials, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return googleServiceAccountCredentials{}, fmt.Errorf("service account json is required")
	}
	var creds googleServiceAccountCredentials
	if err := json.Unmarshal([]byte(raw), &creds); err != nil {
		return googleServiceAccountCredentials{}, fmt.Errorf("invalid service account json: %w", err)
	}
	creds.PrivateKey = strings.TrimSpace(creds.PrivateKey)
	if strings.Contains(creds.PrivateKey, `\\n`) {
		creds.PrivateKey = strings.ReplaceAll(creds.PrivateKey, `\\n`, "\n")
	}
	if strings.TrimSpace(creds.ClientEmail) == "" {
		return googleServiceAccountCredentials{}, fmt.Errorf("service account json missing client_email")
	}
	if strings.TrimSpace(creds.PrivateKey) == "" {
		return googleServiceAccountCredentials{}, fmt.Errorf("service account json missing private_key")
	}
	if strings.TrimSpace(creds.TokenURI) == "" {
		creds.TokenURI = "https://oauth2.googleapis.com/token"
	}
	if _, err := parseGoogleRSAPrivateKey(creds.PrivateKey); err != nil {
		return googleServiceAccountCredentials{}, err
	}
	return creds, nil
}

func parseGoogleRSAPrivateKey(value string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(value)))
	if block == nil {
		return nil, fmt.Errorf("invalid service account private key pem")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse service account private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("service account private key must be RSA")
	}
	return key, nil
}

func buildGooglePlayTokenProvider(runtime googlePlayRuntimeContext) (googleplaybridge.TokenProvider, error) {
	creds, err := parseGoogleServiceAccountCredentials(runtime.ServiceAccountJSON)
	if err != nil {
		return nil, err
	}
	provider := &googlePlayServiceAccountTokenProvider{
		creds:      creds,
		source:     firstNonEmpty(runtime.TokenSource, "service_account"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	return provider, nil
}

func (p *googlePlayServiceAccountTokenProvider) Source() string {
	if p == nil {
		return ""
	}
	if strings.TrimSpace(p.source) == "" {
		return "service_account"
	}
	return p.source
}

func (p *googlePlayServiceAccountTokenProvider) Token(ctx context.Context) (googleplaybridge.Token, error) {
	if p == nil {
		return googleplaybridge.Token{}, fmt.Errorf("google play token provider is nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().UTC()
	if strings.TrimSpace(p.cached.Value) != "" && (p.cached.ExpiresAt.IsZero() || now.Before(p.cached.ExpiresAt.Add(-45*time.Second))) {
		return p.cached, nil
	}
	jwtToken, err := p.signedJWT(now)
	if err != nil {
		return googleplaybridge.Token{}, err
	}
	token, err := p.exchangeToken(ctx, jwtToken)
	if err != nil {
		return googleplaybridge.Token{}, err
	}
	p.cached = token
	return token, nil
}

func (p *googlePlayServiceAccountTokenProvider) signedJWT(now time.Time) (string, error) {
	key, err := parseGoogleRSAPrivateKey(p.creds.PrivateKey)
	if err != nil {
		return "", err
	}
	header := map[string]any{"alg": "RS256", "typ": "JWT"}
	if strings.TrimSpace(p.creds.PrivateKeyID) != "" {
		header["kid"] = strings.TrimSpace(p.creds.PrivateKeyID)
	}
	claims := map[string]any{
		"iss":   strings.TrimSpace(p.creds.ClientEmail),
		"scope": googlePlayOAuthScope,
		"aud":   strings.TrimSpace(p.creds.TokenURI),
		"iat":   now.Add(-30 * time.Second).Unix(),
		"exp":   now.Add(59 * time.Minute).Unix(),
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	signingInput := enc.EncodeToString(headerJSON) + "." + enc.EncodeToString(claimsJSON)
	hash := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + enc.EncodeToString(sig), nil
}

func (p *googlePlayServiceAccountTokenProvider) exchangeToken(ctx context.Context, assertion string) (googleplaybridge.Token, error) {
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", assertion)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(p.creds.TokenURI), strings.NewReader(form.Encode()))
	if err != nil {
		return googleplaybridge.Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return googleplaybridge.Token{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return googleplaybridge.Token{}, googleplaybridge.NormalizeHTTPError(resp.StatusCode, resp.Header, string(body))
	}
	var payload googleServiceAccountTokenResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return googleplaybridge.Token{}, err
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return googleplaybridge.Token{}, fmt.Errorf("google oauth token response missing access_token")
	}
	expiresAt := time.Time{}
	if payload.ExpiresIn > 0 {
		expiresAt = time.Now().UTC().Add(time.Duration(payload.ExpiresIn) * time.Second)
	}
	return googleplaybridge.Token{Value: strings.TrimSpace(payload.AccessToken), ExpiresAt: expiresAt}, nil
}

func buildGooglePlayClient(runtime googlePlayRuntimeContext) (*googleplaybridge.Client, error) {
	tokenProvider, err := buildGooglePlayTokenProvider(runtime)
	if err != nil {
		return nil, err
	}
	settings := loadSettingsOrDefault()
	cfg := googleplaybridge.ClientConfig{
		TokenProvider:    tokenProvider,
		BaseURL:          runtime.BaseURL,
		UploadBaseURL:    runtime.UploadBaseURL,
		CustomAppBaseURL: runtime.CustomAppBaseURL,
		Timeout:          45 * time.Second,
		MaxRetries:       2,
		LogPath:          resolveGooglePlayLogPath(settings),
		LogContext: map[string]string{
			"account_alias": runtime.AccountAlias,
			"project_id":    runtime.ProjectID,
			"environment":   runtime.Environment,
			"package_name":  runtime.DefaultPackageName,
		},
	}
	return googleplaybridge.NewClient(cfg)
}

func resolveGooglePlayLogPath(settings Settings) string {
	if value := strings.TrimSpace(os.Getenv("SI_GOOGLE_PLAY_LOG_FILE")); value != "" {
		return value
	}
	if value := strings.TrimSpace(settings.Google.LogFile); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".si", "logs", "google-play.log")
}

func mustGooglePlayClient(input googlePlayRuntimeContextInput) (googlePlayRuntimeContext, googlePlayBridgeClient) {
	runtime, err := resolveGooglePlayRuntimeContext(input)
	if err != nil {
		fatal(err)
	}
	client, err := buildGooglePlayClient(runtime)
	if err != nil {
		fatal(err)
	}
	return runtime, client
}

func formatGooglePlayContext(runtime googlePlayRuntimeContext) string {
	account := strings.TrimSpace(runtime.AccountAlias)
	if account == "" {
		account = "(default)"
	}
	project := strings.TrimSpace(runtime.ProjectID)
	if project == "" {
		project = "-"
	}
	packageName := strings.TrimSpace(runtime.DefaultPackageName)
	if packageName == "" {
		packageName = "-"
	}
	return fmt.Sprintf("account=%s (%s), env=%s, package=%s", account, project, runtime.Environment, packageName)
}

func printGooglePlayContextBanner(runtime googlePlayRuntimeContext, jsonOut bool) {
	if jsonOut {
		return
	}
	fmt.Printf("%s %s\n", styleDim("google play context:"), formatGooglePlayContext(runtime))
}

func cmdGooglePlayAuth(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google play auth status [--account <alias>] [--env <prod|staging|dev>] [--package <name>] [--json]")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "status":
		cmdGooglePlayAuthStatus(rest)
	default:
		printUnknown("google play auth", sub)
		printUsage("usage: si google play auth status [--account <alias>] [--env <prod|staging|dev>] [--package <name>] [--json]")
	}
}

func cmdGooglePlayAuthStatus(args []string) {
	fs, common := googlePlayCommonFlagSet("google play auth status", args, false)
	verifyPackage := fs.String("verify-package", "", "optional package name to verify api access via edits.insert")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google play auth status [--account <alias>] [--env <prod|staging|dev>] [--verify-package <name>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tokProvider, err := buildGooglePlayTokenProvider(runtime)
	if err != nil {
		fatal(err)
	}
	token, tokenErr := tokProvider.Token(ctx)
	status := "error"
	if tokenErr == nil {
		status = "ready"
	}
	verifyResult := map[string]any{}
	verifyErrText := ""
	pkg := strings.TrimSpace(*verifyPackage)
	if pkg == "" {
		pkg = strings.TrimSpace(runtime.DefaultPackageName)
	}
	if tokenErr == nil && pkg != "" {
		ok, verifyErr := googlePlayVerifyPackageAccess(ctx, client, pkg)
		verifyResult["package"] = pkg
		verifyResult["ok"] = ok
		if verifyErr != nil {
			verifyErrText = verifyErr.Error()
		}
	}
	payload := map[string]any{
		"status":                status,
		"account_alias":         runtime.AccountAlias,
		"project_id":            runtime.ProjectID,
		"environment":           runtime.Environment,
		"source":                runtime.Source,
		"token_source":          runtime.TokenSource,
		"service_account_email": runtime.ServiceAccountEmail,
		"token_uri":             runtime.TokenURI,
		"developer_account_id":  runtime.DeveloperAccountID,
		"default_package_name":  runtime.DefaultPackageName,
		"default_language_code": runtime.DefaultLanguageCode,
		"base_url":              runtime.BaseURL,
		"upload_base_url":       runtime.UploadBaseURL,
		"custom_app_base_url":   runtime.CustomAppBaseURL,
		"token_expires_at":      token.ExpiresAt.UTC().Format(time.RFC3339),
	}
	if tokenErr != nil {
		payload["verify_error"] = tokenErr.Error()
	} else if len(verifyResult) > 0 {
		payload["verify"] = verifyResult
		if verifyErrText != "" {
			payload["verify_error"] = verifyErrText
		}
	}
	if common.json() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		if tokenErr != nil || verifyErrText != "" {
			os.Exit(1)
		}
		return
	}
	if tokenErr != nil {
		fmt.Printf("%s %s\n", styleHeading("Google Play auth:"), styleError("error"))
		fmt.Printf("%s %s\n", styleHeading("Context:"), formatGooglePlayContext(runtime))
		fatal(tokenErr)
	}
	fmt.Printf("%s %s\n", styleHeading("Google Play auth:"), styleSuccess("ready"))
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatGooglePlayContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Service account:"), orDash(runtime.ServiceAccountEmail))
	if !token.ExpiresAt.IsZero() {
		fmt.Printf("%s %s\n", styleHeading("Token expires:"), formatDateWithGitHubRelativeNow(token.ExpiresAt))
	}
	if len(verifyResult) > 0 {
		if verifyErrText != "" {
			fmt.Printf("%s %s\n", styleHeading("Package verify:"), styleError(verifyErrText))
			os.Exit(1)
		}
		fmt.Printf("%s %s\n", styleHeading("Package verify:"), styleSuccess("ok"))
	}
}

func cmdGooglePlayContext(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google play context <list|current|use>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGooglePlayContextList(rest)
	case "current":
		cmdGooglePlayContextCurrent(rest)
	case "use":
		cmdGooglePlayContextUse(rest)
	default:
		printUnknown("google play context", sub)
		printUsage("usage: si google play context <list|current|use>")
	}
}

func cmdGooglePlayContextList(args []string) {
	fs := flag.NewFlagSet("google play context list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google play context list [--json]")
		return
	}
	settings := loadSettingsOrDefault()
	aliases := googlePlayAccountAliases(settings)
	rows := make([]map[string]string, 0, len(aliases))
	for _, alias := range aliases {
		entry := settings.Google.Play.Accounts[alias]
		rows = append(rows, map[string]string{
			"alias":             alias,
			"name":              strings.TrimSpace(entry.Name),
			"project":           strings.TrimSpace(entry.ProjectID),
			"default":           boolString(alias == strings.TrimSpace(settings.Google.DefaultAccount)),
			"developer_account": strings.TrimSpace(entry.DeveloperAccountID),
			"default_package":   strings.TrimSpace(entry.DefaultPackageName),
			"default_language":  strings.TrimSpace(entry.DefaultLanguageCode),
		})
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rows); err != nil {
			fatal(err)
		}
		return
	}
	if len(rows) == 0 {
		infof("no google play accounts configured in settings")
		return
	}
	fmt.Printf("%s %s %s %s %s %s %s\n",
		padRightANSI(styleHeading("ALIAS"), 18),
		padRightANSI(styleHeading("DEFAULT"), 8),
		padRightANSI(styleHeading("PROJECT"), 24),
		padRightANSI(styleHeading("DEV ACCOUNT"), 16),
		padRightANSI(styleHeading("PACKAGE"), 24),
		padRightANSI(styleHeading("LANG"), 8),
		styleHeading("NAME"),
	)
	for _, row := range rows {
		fmt.Printf("%s %s %s %s %s %s %s\n",
			padRightANSI(orDash(row["alias"]), 18),
			padRightANSI(orDash(row["default"]), 8),
			padRightANSI(orDash(row["project"]), 24),
			padRightANSI(orDash(row["developer_account"]), 16),
			padRightANSI(orDash(row["default_package"]), 24),
			padRightANSI(orDash(row["default_language"]), 8),
			orDash(row["name"]),
		)
	}
}

func cmdGooglePlayContextCurrent(args []string) {
	fs := flag.NewFlagSet("google play context current", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google play context current [--json]")
		return
	}
	runtime, err := resolveGooglePlayRuntimeContext(googlePlayRuntimeContextInput{})
	if err != nil {
		fatal(err)
	}
	payload := map[string]any{
		"account_alias":         runtime.AccountAlias,
		"project_id":            runtime.ProjectID,
		"environment":           runtime.Environment,
		"source":                runtime.Source,
		"token_source":          runtime.TokenSource,
		"service_account_email": runtime.ServiceAccountEmail,
		"developer_account_id":  runtime.DeveloperAccountID,
		"default_package_name":  runtime.DefaultPackageName,
		"default_language_code": runtime.DefaultLanguageCode,
		"base_url":              runtime.BaseURL,
		"upload_base_url":       runtime.UploadBaseURL,
		"custom_app_base_url":   runtime.CustomAppBaseURL,
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Current google play context:"), formatGooglePlayContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
	fmt.Printf("%s %s\n", styleHeading("Token source:"), orDash(runtime.TokenSource))
}

func cmdGooglePlayContextUse(args []string) {
	fs, common := googlePlayCommonFlagSet("google play context use", args, false)
	aliasFlag := fs.String("alias", "", "account alias (overrides --account)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google play context use [--account <alias>] [--env <prod|staging|dev>] [--project-id <id>] [--developer-account <id>] [--package <name>] [--language <code>] [--service-account-file <path>]")
		return
	}
	settings := loadSettingsOrDefault()
	alias := strings.TrimSpace(*aliasFlag)
	if alias == "" {
		alias = strings.TrimSpace(stringValue(common.account))
	}
	if alias == "" {
		alias = strings.TrimSpace(settings.Google.DefaultAccount)
	}
	if alias == "" {
		fatal(fmt.Errorf("account alias is required (use --account <alias>)"))
	}
	if settings.Google.Play.Accounts == nil {
		settings.Google.Play.Accounts = map[string]GooglePlayAccountEntry{}
	}
	entry := settings.Google.Play.Accounts[alias]
	if value := strings.TrimSpace(stringValue(common.projectID)); value != "" {
		entry.ProjectID = value
	}
	if value := strings.TrimSpace(stringValue(common.developerAccount)); value != "" {
		entry.DeveloperAccountID = value
	}
	if value := strings.TrimSpace(stringValue(common.packageName)); value != "" {
		entry.DefaultPackageName = value
	}
	if value := strings.TrimSpace(stringValue(common.language)); value != "" {
		entry.DefaultLanguageCode = value
	}
	if value := strings.TrimSpace(stringValue(common.serviceFile)); value != "" {
		entry.ServiceAccountFile = value
	}
	if value := strings.TrimSpace(stringValue(common.serviceJSON)); value != "" {
		if strings.HasPrefix(value, "@") {
			entry.ServiceAccountFile = strings.TrimSpace(strings.TrimPrefix(value, "@"))
		} else {
			warnf("--service-account-json contains secret material; not persisting raw json in settings (use --service-account-file)")
		}
	}
	settings.Google.Play.Accounts[alias] = entry
	settings.Google.DefaultAccount = alias
	if value := normalizeGoogleEnvironment(strings.TrimSpace(stringValue(common.env))); value != "" {
		settings.Google.DefaultEnv = value
	}
	if value := strings.TrimSpace(stringValue(common.baseURL)); value != "" {
		settings.Google.Play.APIBaseURL = value
	}
	if value := strings.TrimSpace(stringValue(common.uploadBaseURL)); value != "" {
		settings.Google.Play.UploadBaseURL = value
	}
	if value := strings.TrimSpace(stringValue(common.customAppBaseURL)); value != "" {
		settings.Google.Play.CustomAppBaseURL = value
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	successf("google play context updated")
	fmt.Printf("%s %s\n", styleHeading("Account:"), alias)
	fmt.Printf("%s %s\n", styleHeading("Environment:"), settings.Google.DefaultEnv)
}

func cmdGooglePlayDoctor(args []string) {
	fs, common := googlePlayCommonFlagSet("google play doctor", args, false)
	public := fs.Bool("public", false, "run unauthenticated discovery probe")
	verifyPackage := fs.String("verify-package", "", "optional package name to verify with edits.insert")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google play doctor [--account <alias>] [--env <prod|staging|dev>] [--public] [--verify-package <name>] [--json]")
		return
	}
	if *public {
		baseURL := strings.TrimSpace(stringValue(common.baseURL))
		if baseURL == "" {
			baseURL = "https://androidpublisher.googleapis.com"
		}
		probeURL := strings.TrimRight(baseURL, "/") + "/$discovery/rest?version=v3"
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
		if err != nil {
			fatal(err)
		}
		resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
		if err != nil {
			fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		ok := resp.StatusCode >= 200 && resp.StatusCode < 300
		payload := map[string]any{
			"ok":          ok,
			"probe":       probeURL,
			"status_code": resp.StatusCode,
			"status":      resp.Status,
		}
		if !ok {
			payload["body"] = strings.TrimSpace(string(body))
		}
		if common.json() {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(payload); err != nil {
				fatal(err)
			}
			if !ok {
				os.Exit(1)
			}
			return
		}
		if !ok {
			fatal(fmt.Errorf("google play public probe failed: %s", strings.TrimSpace(string(body))))
		}
		successf("google play public probe ok")
		fmt.Printf("%s %s\n", styleHeading("Probe:"), probeURL)
		return
	}
	argsForStatus := append([]string{}, args...)
	if strings.TrimSpace(*verifyPackage) == "" {
		argsForStatus = append(argsForStatus, "--verify-package", strings.TrimSpace(stringValue(common.packageName)))
	}
	cmdGooglePlayAuthStatus(argsForStatus)
}

func googlePlayVerifyPackageAccess(ctx context.Context, client googlePlayBridgeClient, packageName string) (bool, error) {
	packageName = strings.TrimSpace(packageName)
	if packageName == "" {
		return false, fmt.Errorf("package name is required")
	}
	insertPath := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits", url.PathEscape(packageName))
	insertResp, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodPost, Path: insertPath, JSONBody: map[string]any{}})
	if err != nil {
		return false, err
	}
	editID := strings.TrimSpace(anyToString(insertResp.Data["id"]))
	if editID == "" {
		return false, fmt.Errorf("edits.insert response missing id")
	}
	deletePath := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s", url.PathEscape(packageName), url.PathEscape(editID))
	_, _ = client.Do(ctx, googleplaybridge.Request{Method: http.MethodDelete, Path: deletePath})
	return true, nil
}

func anyToString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(typed, 10)
	case int:
		return strconv.Itoa(typed)
	default:
		return fmt.Sprintf("%v", typed)
	}
}
