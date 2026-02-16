package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"si/tools/si/internal/appstorebridge"
)

type appleAppStoreRuntimeContextInput struct {
	AccountFlag    string
	EnvFlag        string
	BundleIDFlag   string
	LocaleFlag     string
	PlatformFlag   string
	IssuerIDFlag   string
	KeyIDFlag      string
	PrivateKeyFlag string
	PrivateKeyFile string
	ProjectIDFlag  string
	BaseURLFlag    string
}

type appleAppStoreRuntimeContext struct {
	AccountAlias string
	ProjectID    string
	Environment  string
	Source       string
	TokenSource  string
	BaseURL      string

	BundleID string
	Locale   string
	Platform string

	IssuerID      string
	KeyID         string
	PrivateKeyPEM string
}

type appleAppStoreBridgeClient interface {
	Do(ctx context.Context, req appstorebridge.Request) (appstorebridge.Response, error)
}

type appleAppStoreJWTProvider struct {
	issuerID   string
	keyID      string
	privateKey *ecdsa.PrivateKey
	source     string

	mu     sync.Mutex
	cached appstorebridge.Token
}

func appleAppStoreAccountAliases(settings Settings) []string {
	if len(settings.Apple.AppStore.Accounts) == 0 {
		return nil
	}
	out := make([]string, 0, len(settings.Apple.AppStore.Accounts))
	for alias := range settings.Apple.AppStore.Accounts {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

func resolveAppleAppStoreAccountSelection(settings Settings, accountFlag string) (string, AppleAppStoreAccountEntry) {
	selected := strings.TrimSpace(accountFlag)
	if selected == "" {
		selected = strings.TrimSpace(settings.Apple.DefaultAccount)
	}
	if selected == "" {
		selected = strings.TrimSpace(os.Getenv("APPLE_DEFAULT_ACCOUNT"))
	}
	if selected == "" {
		aliases := appleAppStoreAccountAliases(settings)
		if len(aliases) == 1 {
			selected = aliases[0]
		}
	}
	if selected == "" {
		return "", AppleAppStoreAccountEntry{}
	}
	if entry, ok := settings.Apple.AppStore.Accounts[selected]; ok {
		return selected, entry
	}
	return selected, AppleAppStoreAccountEntry{}
}

func appleAppStoreAccountEnvPrefix(alias string, account AppleAppStoreAccountEntry) string {
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
	return "APPLE_" + alias + "_"
}

func resolveAppleAppStoreEnv(alias string, account AppleAppStoreAccountEntry, key string) string {
	prefix := appleAppStoreAccountEnvPrefix(alias, account)
	if prefix != "" {
		if value := strings.TrimSpace(os.Getenv(prefix + key)); value != "" {
			return value
		}
	}
	return ""
}

func resolveAppleAppStoreRuntimeContext(input appleAppStoreRuntimeContextInput) (appleAppStoreRuntimeContext, error) {
	settings := loadSettingsOrDefault()
	alias, account := resolveAppleAppStoreAccountSelection(settings, input.AccountFlag)
	env := strings.TrimSpace(input.EnvFlag)
	if env == "" {
		env = strings.TrimSpace(settings.Apple.DefaultEnv)
	}
	if env == "" {
		env = strings.TrimSpace(os.Getenv("APPLE_DEFAULT_ENV"))
	}
	if env == "" {
		env = "prod"
	}
	env = normalizeIntegrationEnvironment(env)
	if env == "" {
		return appleAppStoreRuntimeContext{}, fmt.Errorf("environment required (prod|staging|dev)")
	}
	baseURL := strings.TrimSpace(input.BaseURLFlag)
	if baseURL == "" {
		baseURL = strings.TrimSpace(settings.Apple.AppStore.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(settings.Apple.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("APPLE_APPSTORE_API_BASE_URL"))
	}
	if baseURL == "" {
		baseURL = "https://api.appstoreconnect.apple.com"
	}
	projectID, projectSource := resolveAppleProjectID(alias, account, strings.TrimSpace(input.ProjectIDFlag))
	bundleID, bundleSource := resolveAppleBundleID(alias, account, strings.TrimSpace(input.BundleIDFlag))
	locale, localeSource := resolveAppleLocale(alias, account, strings.TrimSpace(input.LocaleFlag))
	platform, platformSource, err := resolveApplePlatform(alias, account, strings.TrimSpace(input.PlatformFlag))
	if err != nil {
		return appleAppStoreRuntimeContext{}, err
	}
	issuerID, issuerSource := resolveAppleIssuerID(alias, account, strings.TrimSpace(input.IssuerIDFlag))
	if issuerID == "" {
		return appleAppStoreRuntimeContext{}, fmt.Errorf("apple appstore issuer id not found (set --issuer-id, APPLE_<ACCOUNT>_APPSTORE_ISSUER_ID, or APPLE_APPSTORE_ISSUER_ID)")
	}
	keyID, keySource := resolveAppleKeyID(alias, account, strings.TrimSpace(input.KeyIDFlag))
	if keyID == "" {
		return appleAppStoreRuntimeContext{}, fmt.Errorf("apple appstore key id not found (set --key-id, APPLE_<ACCOUNT>_APPSTORE_KEY_ID, or APPLE_APPSTORE_KEY_ID)")
	}
	privateKey, tokenSource, err := resolveApplePrivateKey(alias, account, strings.TrimSpace(input.PrivateKeyFlag), strings.TrimSpace(input.PrivateKeyFile))
	if err != nil {
		return appleAppStoreRuntimeContext{}, err
	}
	if privateKey == "" {
		return appleAppStoreRuntimeContext{}, fmt.Errorf("apple appstore private key not found (set --private-key, --private-key-file, APPLE_<ACCOUNT>_APPSTORE_PRIVATE_KEY_PEM, or APPLE_APPSTORE_PRIVATE_KEY_PEM)")
	}
	source := strings.Join(nonEmpty(projectSource, bundleSource, localeSource, platformSource, issuerSource, keySource), ",")
	return appleAppStoreRuntimeContext{
		AccountAlias:  alias,
		ProjectID:     projectID,
		Environment:   env,
		Source:        source,
		TokenSource:   tokenSource,
		BaseURL:       baseURL,
		BundleID:      bundleID,
		Locale:        locale,
		Platform:      platform,
		IssuerID:      issuerID,
		KeyID:         keyID,
		PrivateKeyPEM: privateKey,
	}, nil
}

func resolveAppleProjectID(alias string, account AppleAppStoreAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--project-id"
	}
	if value := strings.TrimSpace(account.ProjectID); value != "" {
		return value, "settings.apple.project_id"
	}
	if ref := strings.TrimSpace(account.ProjectIDEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveAppleAppStoreEnv(alias, account, "PROJECT_ID")); value != "" {
		return value, "env:" + appleAppStoreAccountEnvPrefix(alias, account) + "PROJECT_ID"
	}
	if value := strings.TrimSpace(os.Getenv("APPLE_PROJECT_ID")); value != "" {
		return value, "env:APPLE_PROJECT_ID"
	}
	return "", ""
}

func resolveAppleBundleID(alias string, account AppleAppStoreAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--bundle-id"
	}
	if value := strings.TrimSpace(account.DefaultBundleID); value != "" {
		return value, "settings.apple.default_bundle_id"
	}
	if value := strings.TrimSpace(resolveAppleAppStoreEnv(alias, account, "APPSTORE_BUNDLE_ID")); value != "" {
		return value, "env:" + appleAppStoreAccountEnvPrefix(alias, account) + "APPSTORE_BUNDLE_ID"
	}
	if value := strings.TrimSpace(os.Getenv("APPLE_APPSTORE_BUNDLE_ID")); value != "" {
		return value, "env:APPLE_APPSTORE_BUNDLE_ID"
	}
	return "", ""
}

func resolveAppleLocale(alias string, account AppleAppStoreAccountEntry, override string) (string, string) {
	if value := normalizeAppleLocale(override); value != "" {
		return value, "flag:--locale"
	}
	if value := normalizeAppleLocale(account.DefaultLanguage); value != "" {
		return value, "settings.apple.default_language"
	}
	if value := normalizeAppleLocale(resolveAppleAppStoreEnv(alias, account, "APPSTORE_LOCALE")); value != "" {
		return value, "env:" + appleAppStoreAccountEnvPrefix(alias, account) + "APPSTORE_LOCALE"
	}
	if value := normalizeAppleLocale(os.Getenv("APPLE_APPSTORE_LOCALE")); value != "" {
		return value, "env:APPLE_APPSTORE_LOCALE"
	}
	return "en-US", "default"
}

func resolveApplePlatform(alias string, account AppleAppStoreAccountEntry, override string) (string, string, error) {
	if override != "" {
		platform := normalizeApplePlatform(override)
		if platform == "" {
			return "", "", fmt.Errorf("invalid --platform %q (expected IOS|MAC_OS|TV_OS|VISION_OS)", override)
		}
		return platform, "flag:--platform", nil
	}
	if value := normalizeApplePlatform(account.DefaultPlatform); value != "" {
		return value, "settings.apple.default_platform", nil
	}
	if value := normalizeApplePlatform(resolveAppleAppStoreEnv(alias, account, "APPSTORE_PLATFORM")); value != "" {
		return value, "env:" + appleAppStoreAccountEnvPrefix(alias, account) + "APPSTORE_PLATFORM", nil
	}
	if value := normalizeApplePlatform(os.Getenv("APPLE_APPSTORE_PLATFORM")); value != "" {
		return value, "env:APPLE_APPSTORE_PLATFORM", nil
	}
	return "IOS", "default", nil
}

func resolveAppleIssuerID(alias string, account AppleAppStoreAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--issuer-id"
	}
	if value := strings.TrimSpace(account.IssuerID); value != "" {
		return value, "settings.apple.issuer_id"
	}
	if ref := strings.TrimSpace(account.IssuerIDEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveAppleAppStoreEnv(alias, account, "APPSTORE_ISSUER_ID")); value != "" {
		return value, "env:" + appleAppStoreAccountEnvPrefix(alias, account) + "APPSTORE_ISSUER_ID"
	}
	if value := strings.TrimSpace(os.Getenv("APPLE_APPSTORE_ISSUER_ID")); value != "" {
		return value, "env:APPLE_APPSTORE_ISSUER_ID"
	}
	return "", ""
}

func resolveAppleKeyID(alias string, account AppleAppStoreAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--key-id"
	}
	if value := strings.TrimSpace(account.KeyID); value != "" {
		return value, "settings.apple.key_id"
	}
	if ref := strings.TrimSpace(account.KeyIDEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveAppleAppStoreEnv(alias, account, "APPSTORE_KEY_ID")); value != "" {
		return value, "env:" + appleAppStoreAccountEnvPrefix(alias, account) + "APPSTORE_KEY_ID"
	}
	if value := strings.TrimSpace(os.Getenv("APPLE_APPSTORE_KEY_ID")); value != "" {
		return value, "env:APPLE_APPSTORE_KEY_ID"
	}
	return "", ""
}

func resolveApplePrivateKey(alias string, account AppleAppStoreAccountEntry, overrideValue string, overrideFile string) (string, string, error) {
	if overrideValue != "" {
		value, err := resolveApplePrivateKeyInput(overrideValue)
		return value, "flag:--private-key", err
	}
	if overrideFile != "" {
		value, err := resolveApplePrivateKeyFile(overrideFile)
		return value, "flag:--private-key-file", err
	}
	if value := strings.TrimSpace(account.PrivateKeyPEM); value != "" {
		resolved, err := resolveApplePrivateKeyInput(value)
		return resolved, "settings.apple.private_key_pem", err
	}
	if ref := strings.TrimSpace(account.PrivateKeyEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			resolved, err := resolveApplePrivateKeyInput(value)
			return resolved, "env:" + ref, err
		}
	}
	if value := strings.TrimSpace(resolveAppleAppStoreEnv(alias, account, "APPSTORE_PRIVATE_KEY_PEM")); value != "" {
		resolved, err := resolveApplePrivateKeyInput(value)
		return resolved, "env:" + appleAppStoreAccountEnvPrefix(alias, account) + "APPSTORE_PRIVATE_KEY_PEM", err
	}
	if value := strings.TrimSpace(account.PrivateKeyFile); value != "" {
		resolved, err := resolveApplePrivateKeyFile(value)
		return resolved, "settings.apple.private_key_file", err
	}
	if ref := strings.TrimSpace(account.PrivateKeyFileEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			resolved, err := resolveApplePrivateKeyFile(value)
			return resolved, "env:" + ref, err
		}
	}
	if value := strings.TrimSpace(resolveAppleAppStoreEnv(alias, account, "APPSTORE_PRIVATE_KEY_FILE")); value != "" {
		resolved, err := resolveApplePrivateKeyFile(value)
		return resolved, "env:" + appleAppStoreAccountEnvPrefix(alias, account) + "APPSTORE_PRIVATE_KEY_FILE", err
	}
	if value := strings.TrimSpace(os.Getenv("APPLE_APPSTORE_PRIVATE_KEY_PEM")); value != "" {
		resolved, err := resolveApplePrivateKeyInput(value)
		return resolved, "env:APPLE_APPSTORE_PRIVATE_KEY_PEM", err
	}
	if value := strings.TrimSpace(os.Getenv("APPLE_APPSTORE_PRIVATE_KEY_FILE")); value != "" {
		resolved, err := resolveApplePrivateKeyFile(value)
		return resolved, "env:APPLE_APPSTORE_PRIVATE_KEY_FILE", err
	}
	return "", "", nil
}

func resolveApplePrivateKeyInput(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if strings.HasPrefix(raw, "@") {
		return resolveApplePrivateKeyFile(strings.TrimSpace(strings.TrimPrefix(raw, "@")))
	}
	if strings.HasSuffix(strings.ToLower(raw), ".p8") || strings.HasSuffix(strings.ToLower(raw), ".pem") {
		if _, err := os.Stat(raw); err == nil {
			return resolveApplePrivateKeyFile(raw)
		}
	}
	if strings.Contains(raw, `\n`) {
		raw = strings.ReplaceAll(raw, `\n`, "\n")
	}
	return raw, nil
}

func resolveApplePrivateKeyFile(path string) (string, error) {
	data, err := readLocalFile(path)
	if err != nil {
		return "", err
	}
	content := strings.TrimSpace(string(data))
	if strings.Contains(content, `\n`) {
		content = strings.ReplaceAll(content, `\n`, "\n")
	}
	return content, nil
}

func parseAppleECPrivateKey(value string) (*ecdsa.PrivateKey, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, `\n`) {
		value = strings.ReplaceAll(value, `\n`, "\n")
	}
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, fmt.Errorf("invalid apple appstore private key pem")
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse apple appstore private key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("apple appstore private key must be ECDSA")
	}
	return key, nil
}

func buildAppleAppStoreTokenProvider(runtime appleAppStoreRuntimeContext) (appstorebridge.TokenProvider, error) {
	privateKey, err := parseAppleECPrivateKey(runtime.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}
	provider := &appleAppStoreJWTProvider{
		issuerID:   strings.TrimSpace(runtime.IssuerID),
		keyID:      strings.TrimSpace(runtime.KeyID),
		privateKey: privateKey,
		source:     firstNonEmpty(runtime.TokenSource, "appstore_jwt"),
	}
	if provider.issuerID == "" {
		return nil, fmt.Errorf("issuer id is required")
	}
	if provider.keyID == "" {
		return nil, fmt.Errorf("key id is required")
	}
	return provider, nil
}

func (p *appleAppStoreJWTProvider) Source() string {
	if p == nil {
		return ""
	}
	if strings.TrimSpace(p.source) == "" {
		return "appstore_jwt"
	}
	return p.source
}

func (p *appleAppStoreJWTProvider) Token(_ context.Context) (appstorebridge.Token, error) {
	if p == nil {
		return appstorebridge.Token{}, fmt.Errorf("apple appstore jwt provider is nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().UTC()
	if strings.TrimSpace(p.cached.Value) != "" && !p.cached.ExpiresAt.IsZero() && now.Before(p.cached.ExpiresAt.Add(-60*time.Second)) {
		return p.cached, nil
	}
	token, expiresAt, err := p.signedJWT(now)
	if err != nil {
		return appstorebridge.Token{}, err
	}
	p.cached = appstorebridge.Token{Value: token, ExpiresAt: expiresAt}
	return p.cached, nil
}

func (p *appleAppStoreJWTProvider) signedJWT(now time.Time) (string, time.Time, error) {
	expiresAt := now.Add(19 * time.Minute)
	header := map[string]any{
		"alg": "ES256",
		"kid": p.keyID,
		"typ": "JWT",
	}
	claims := map[string]any{
		"iss": p.issuerID,
		"aud": "appstoreconnect-v1",
		"iat": now.Add(-30 * time.Second).Unix(),
		"exp": expiresAt.Unix(),
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", time.Time{}, err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, err
	}
	enc := base64.RawURLEncoding
	signingInput := enc.EncodeToString(headerJSON) + "." + enc.EncodeToString(claimsJSON)
	h := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, p.privateKey, h[:])
	if err != nil {
		return "", time.Time{}, err
	}
	sig, err := joseEcdsaSignature(r, s, p.privateKey)
	if err != nil {
		return "", time.Time{}, err
	}
	jwt := signingInput + "." + enc.EncodeToString(sig)
	return jwt, expiresAt, nil
}

func joseEcdsaSignature(r *big.Int, s *big.Int, key *ecdsa.PrivateKey) ([]byte, error) {
	if r == nil || s == nil || key == nil || key.Curve == nil {
		return nil, fmt.Errorf("invalid ecdsa signature inputs")
	}
	keyBytes := (key.Curve.Params().BitSize + 7) / 8
	raw := make([]byte, keyBytes*2)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(raw[keyBytes-len(rBytes):keyBytes], rBytes)
	copy(raw[2*keyBytes-len(sBytes):], sBytes)
	return raw, nil
}

func buildAppleAppStoreClient(runtime appleAppStoreRuntimeContext) (*appstorebridge.Client, error) {
	tokenProvider, err := buildAppleAppStoreTokenProvider(runtime)
	if err != nil {
		return nil, err
	}
	settings := loadSettingsOrDefault()
	cfg := appstorebridge.ClientConfig{
		TokenProvider: tokenProvider,
		BaseURL:       runtime.BaseURL,
		Timeout:       30 * time.Second,
		MaxRetries:    2,
		LogPath:       resolveAppleAppStoreLogPath(settings),
		LogContext: map[string]string{
			"account_alias": runtime.AccountAlias,
			"project_id":    runtime.ProjectID,
			"environment":   runtime.Environment,
			"bundle_id":     runtime.BundleID,
		},
	}
	return appstorebridge.NewClient(cfg)
}

func resolveAppleAppStoreLogPath(settings Settings) string {
	if value := strings.TrimSpace(os.Getenv("SI_APPLE_APPSTORE_LOG_FILE")); value != "" {
		return value
	}
	if value := strings.TrimSpace(settings.Apple.LogFile); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".si", "logs", "apple-appstore.log")
}

func mustAppleAppStoreClient(input appleAppStoreRuntimeContextInput) (appleAppStoreRuntimeContext, appleAppStoreBridgeClient) {
	runtime, err := resolveAppleAppStoreRuntimeContext(input)
	if err != nil {
		fatal(err)
	}
	client, err := buildAppleAppStoreClient(runtime)
	if err != nil {
		fatal(err)
	}
	return runtime, client
}

func formatAppleAppStoreContext(runtime appleAppStoreRuntimeContext) string {
	account := strings.TrimSpace(runtime.AccountAlias)
	if account == "" {
		account = "(default)"
	}
	project := strings.TrimSpace(runtime.ProjectID)
	if project == "" {
		project = "-"
	}
	bundleID := strings.TrimSpace(runtime.BundleID)
	if bundleID == "" {
		bundleID = "-"
	}
	return fmt.Sprintf("account=%s (%s), env=%s, bundle=%s, platform=%s", account, project, runtime.Environment, bundleID, runtime.Platform)
}

func printAppleAppStoreContextBanner(runtime appleAppStoreRuntimeContext, jsonOut bool) {
	if jsonOut {
		return
	}
	fmt.Printf("%s %s\n", styleDim("apple appstore context:"), formatAppleAppStoreContext(runtime))
}

func cmdAppleAppStoreAuth(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si apple appstore auth status [--account <alias>] [--env <prod|staging|dev>] [--json]")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "status":
		cmdAppleAppStoreAuthStatus(rest)
	default:
		printUnknown("apple appstore auth", sub)
		printUsage("usage: si apple appstore auth status [--account <alias>] [--env <prod|staging|dev>] [--json]")
	}
}

func cmdAppleAppStoreAuthStatus(args []string) {
	fs, common := appleAppStoreCommonFlagSet("apple appstore auth status", args, false)
	verify := fs.Bool("verify", true, "verify token against /v1/apps?limit=1")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si apple appstore auth status [--account <alias>] [--env <prod|staging|dev>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	tokenProvider, err := buildAppleAppStoreTokenProvider(runtime)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	tok, tokenErr := tokenProvider.Token(ctx)
	status := "error"
	if tokenErr == nil {
		status = "ready"
	}
	verifyStatus := map[string]any{}
	verifyErrText := ""
	if tokenErr == nil && *verify {
		resp, err := client.Do(ctx, appstorebridge.Request{Method: http.MethodGet, Path: "/v1/apps", Params: map[string]string{"limit": "1"}})
		if err != nil {
			verifyErrText = err.Error()
		} else {
			verifyStatus["ok"] = true
			verifyStatus["status_code"] = resp.StatusCode
			verifyStatus["items"] = len(resp.List)
		}
	}
	payload := map[string]any{
		"status":        status,
		"account_alias": runtime.AccountAlias,
		"project_id":    runtime.ProjectID,
		"environment":   runtime.Environment,
		"source":        runtime.Source,
		"token_source":  runtime.TokenSource,
		"bundle_id":     runtime.BundleID,
		"locale":        runtime.Locale,
		"platform":      runtime.Platform,
		"base_url":      runtime.BaseURL,
	}
	if tokenErr == nil {
		payload["token_expires_at"] = tok.ExpiresAt.UTC().Format(time.RFC3339)
	} else {
		payload["token_error"] = tokenErr.Error()
	}
	if len(verifyStatus) > 0 {
		payload["verify"] = verifyStatus
	}
	if verifyErrText != "" {
		payload["verify_error"] = verifyErrText
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
		fmt.Printf("%s %s\n", styleHeading("Apple App Store auth:"), styleError("error"))
		fmt.Printf("%s %s\n", styleHeading("Context:"), formatAppleAppStoreContext(runtime))
		fatal(tokenErr)
	}
	fmt.Printf("%s %s\n", styleHeading("Apple App Store auth:"), styleSuccess("ready"))
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatAppleAppStoreContext(runtime))
	if !tok.ExpiresAt.IsZero() {
		fmt.Printf("%s %s\n", styleHeading("Token expires:"), formatDateWithGitHubRelativeNow(tok.ExpiresAt))
	}
	if verifyErrText != "" {
		fmt.Printf("%s %s\n", styleHeading("Verify:"), styleError(verifyErrText))
		os.Exit(1)
	}
	if len(verifyStatus) > 0 {
		fmt.Printf("%s %s\n", styleHeading("Verify:"), styleSuccess("ok"))
	}
}

func cmdAppleAppStoreContext(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si apple appstore context <list|current|use>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdAppleAppStoreContextList(rest)
	case "current":
		cmdAppleAppStoreContextCurrent(rest)
	case "use":
		cmdAppleAppStoreContextUse(rest)
	default:
		printUnknown("apple appstore context", sub)
		printUsage("usage: si apple appstore context <list|current|use>")
	}
}

func cmdAppleAppStoreContextList(args []string) {
	fs := flag.NewFlagSet("apple appstore context list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si apple appstore context list [--json]")
		return
	}
	settings := loadSettingsOrDefault()
	aliases := appleAppStoreAccountAliases(settings)
	rows := make([]map[string]string, 0, len(aliases))
	for _, alias := range aliases {
		entry := settings.Apple.AppStore.Accounts[alias]
		rows = append(rows, map[string]string{
			"alias":     alias,
			"name":      strings.TrimSpace(entry.Name),
			"project":   strings.TrimSpace(entry.ProjectID),
			"default":   boolString(alias == strings.TrimSpace(settings.Apple.DefaultAccount)),
			"bundle_id": strings.TrimSpace(entry.DefaultBundleID),
			"platform":  strings.TrimSpace(entry.DefaultPlatform),
			"language":  strings.TrimSpace(entry.DefaultLanguage),
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
		infof("no apple appstore accounts configured in settings")
		return
	}
	headers := []string{
		styleHeading("ALIAS"),
		styleHeading("DEFAULT"),
		styleHeading("PROJECT"),
		styleHeading("BUNDLE ID"),
		styleHeading("PLATFORM"),
		styleHeading("LANG"),
		styleHeading("NAME"),
	}
	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		tableRows = append(tableRows, []string{
			orDash(row["alias"]),
			orDash(row["default"]),
			orDash(row["project"]),
			orDash(row["bundle_id"]),
			orDash(row["platform"]),
			orDash(row["language"]),
			orDash(row["name"]),
		})
	}
	printAlignedTable(headers, tableRows, 2)
}

func cmdAppleAppStoreContextCurrent(args []string) {
	fs := flag.NewFlagSet("apple appstore context current", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si apple appstore context current [--json]")
		return
	}
	runtime, err := resolveAppleAppStoreRuntimeContext(appleAppStoreRuntimeContextInput{})
	if err != nil {
		fatal(err)
	}
	payload := map[string]any{
		"account_alias": runtime.AccountAlias,
		"project_id":    runtime.ProjectID,
		"environment":   runtime.Environment,
		"source":        runtime.Source,
		"token_source":  runtime.TokenSource,
		"bundle_id":     runtime.BundleID,
		"locale":        runtime.Locale,
		"platform":      runtime.Platform,
		"base_url":      runtime.BaseURL,
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Current apple appstore context:"), formatAppleAppStoreContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
	fmt.Printf("%s %s\n", styleHeading("Token source:"), orDash(runtime.TokenSource))
}

func cmdAppleAppStoreContextUse(args []string) {
	fs, common := appleAppStoreCommonFlagSet("apple appstore context use", args, false)
	aliasFlag := fs.String("alias", "", "account alias (overrides --account)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si apple appstore context use [--account <alias>] [--env <prod|staging|dev>] [--bundle-id <id>] [--platform <IOS|MAC_OS|TV_OS|VISION_OS>] [--locale <code>] [--issuer-id <id>] [--key-id <id>] [--private-key-file <path>]")
		return
	}
	settings := loadSettingsOrDefault()
	alias := strings.TrimSpace(*aliasFlag)
	if alias == "" {
		alias = strings.TrimSpace(stringValue(common.account))
	}
	if alias == "" {
		alias = strings.TrimSpace(settings.Apple.DefaultAccount)
	}
	if alias == "" {
		fatal(fmt.Errorf("account alias is required (use --account <alias>)"))
	}
	if settings.Apple.AppStore.Accounts == nil {
		settings.Apple.AppStore.Accounts = map[string]AppleAppStoreAccountEntry{}
	}
	entry := settings.Apple.AppStore.Accounts[alias]
	if value := strings.TrimSpace(stringValue(common.projectID)); value != "" {
		entry.ProjectID = value
	}
	if value := strings.TrimSpace(stringValue(common.bundleID)); value != "" {
		entry.DefaultBundleID = value
	}
	if value := normalizeAppleLocale(strings.TrimSpace(stringValue(common.locale))); value != "" {
		entry.DefaultLanguage = value
	}
	if value := normalizeApplePlatform(strings.TrimSpace(stringValue(common.platform))); value != "" {
		entry.DefaultPlatform = value
	}
	if value := strings.TrimSpace(stringValue(common.issuerID)); value != "" {
		entry.IssuerID = value
	}
	if value := strings.TrimSpace(stringValue(common.keyID)); value != "" {
		entry.KeyID = value
	}
	if value := strings.TrimSpace(stringValue(common.privateKeyFile)); value != "" {
		entry.PrivateKeyFile = value
	}
	if value := strings.TrimSpace(stringValue(common.privateKey)); value != "" {
		if strings.HasPrefix(value, "@") {
			entry.PrivateKeyFile = strings.TrimSpace(strings.TrimPrefix(value, "@"))
		} else {
			warnf("--private-key contains secret material; not persisting raw key in settings (use --private-key-file)")
		}
	}
	settings.Apple.AppStore.Accounts[alias] = entry
	settings.Apple.DefaultAccount = alias
	if value := normalizeIntegrationEnvironment(strings.TrimSpace(stringValue(common.env))); value != "" {
		settings.Apple.DefaultEnv = value
	}
	if value := strings.TrimSpace(stringValue(common.baseURL)); value != "" {
		settings.Apple.AppStore.APIBaseURL = value
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	successf("apple appstore context updated")
	fmt.Printf("%s %s\n", styleHeading("Account:"), alias)
	fmt.Printf("%s %s\n", styleHeading("Environment:"), settings.Apple.DefaultEnv)
}

func cmdAppleAppStoreDoctor(args []string) {
	fs, common := appleAppStoreCommonFlagSet("apple appstore doctor", args, false)
	public := fs.Bool("public", false, "run unauthenticated connectivity probe")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si apple appstore doctor [--account <alias>] [--env <prod|staging|dev>] [--public] [--json]")
		return
	}
	if *public {
		probeURL := "https://developer.apple.com/sample-code/app-store-connect/app-store-connect-openapi-specification.zip"
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
		ok := resp.StatusCode >= 200 && resp.StatusCode < 300
		payload := map[string]any{
			"ok":          ok,
			"probe":       probeURL,
			"status_code": resp.StatusCode,
			"status":      resp.Status,
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
			fatal(fmt.Errorf("apple appstore public probe failed: %s", resp.Status))
		}
		successf("apple appstore public probe ok")
		fmt.Printf("%s %s\n", styleHeading("Probe:"), probeURL)
		return
	}
	argsForStatus := append([]string{"status"}, args...)
	cmdAppleAppStoreAuth(argsForStatus)
}
