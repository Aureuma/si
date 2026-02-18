package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"si/tools/si/internal/githubbridge"
	"si/tools/si/internal/providers"
)

type githubAuthOverrides struct {
	AppID          int64
	AppKey         string
	InstallationID int64
	AccessToken    string
	AuthMode       string
}

func resolveGithubRuntimeContext(accountFlag string, ownerFlag string, baseURLFlag string, overrides githubAuthOverrides) (githubRuntimeContext, error) {
	settings := loadSettingsOrDefault()
	alias, account := resolveGitHubAccountSelection(settings, accountFlag)

	owner := strings.TrimSpace(ownerFlag)
	if owner == "" {
		owner = strings.TrimSpace(account.Owner)
	}
	if owner == "" {
		owner = strings.TrimSpace(settings.Github.DefaultOwner)
	}
	if owner == "" {
		owner = strings.TrimSpace(os.Getenv("GITHUB_DEFAULT_OWNER"))
	}

	baseURL := strings.TrimSpace(baseURLFlag)
	if baseURL == "" {
		baseURL = strings.TrimSpace(account.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(settings.Github.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("GITHUB_API_BASE_URL"))
	}
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}

	mode, modeSource, err := resolveGitHubAuthMode(settings, alias, account, overrides)
	if err != nil {
		return githubRuntimeContext{}, err
	}
	if mode == "" {
		mode = githubbridge.AuthModeApp
	}

	sourceParts := nonEmpty(modeSource)
	if mode == githubbridge.AuthModeOAuth {
		accessToken, tokenSource := resolveGitHubOAuthAccessToken(alias, account, overrides)
		if strings.TrimSpace(accessToken) == "" {
			prefix := githubAccountEnvPrefix(alias, account)
			if prefix == "" {
				prefix = "GITHUB_<ACCOUNT>_"
			}
			return githubRuntimeContext{}, fmt.Errorf("github oauth token not found (set --token, %sOAUTH_ACCESS_TOKEN, %sTOKEN, GITHUB_TOKEN, GH_TOKEN, GITHUB_PAT, or GH_PAT)", prefix, prefix)
		}
		provider, providerErr := githubbridge.NewOAuthProvider(githubbridge.OAuthProviderConfig{
			AccessToken: accessToken,
			TokenSource: strings.Join(nonEmpty(modeSource, tokenSource), ","),
		})
		if providerErr != nil {
			return githubRuntimeContext{}, providerErr
		}
		sourceParts = append(sourceParts, tokenSource)
		return githubRuntimeContext{
			AccountAlias: alias,
			Owner:        owner,
			AuthMode:     githubbridge.AuthModeOAuth,
			Source:       strings.Join(nonEmpty(sourceParts...), ","),
			BaseURL:      baseURL,
			Provider:     provider,
		}, nil
	}

	appID, appIDSource := resolveGitHubAppID(alias, account, overrides)
	appKey, appKeySource := resolveGitHubAppKey(alias, account, overrides)
	installationID, installationSource := resolveGitHubInstallationID(alias, account, overrides)
	if appID <= 0 || strings.TrimSpace(appKey) == "" {
		return githubRuntimeContext{}, fmt.Errorf("github app auth requires app id and private key (keys: %sAPP_ID, %sAPP_PRIVATE_KEY_PEM)", githubAccountEnvPrefix(alias, account), githubAccountEnvPrefix(alias, account))
	}

	provider, err := githubbridge.NewAppProvider(githubbridge.AppProviderConfig{
		AppID:          appID,
		InstallationID: installationID,
		PrivateKeyPEM:  appKey,
		BaseURL:        baseURL,
		Owner:          owner,
		TokenSource:    strings.Join(nonEmpty(appIDSource, appKeySource, installationSource), ","),
	})
	if err != nil {
		return githubRuntimeContext{}, err
	}
	source := strings.Join(nonEmpty(append(sourceParts, appIDSource, appKeySource, installationSource)...), ",")

	return githubRuntimeContext{
		AccountAlias: alias,
		Owner:        owner,
		AuthMode:     githubbridge.AuthModeApp,
		Source:       source,
		BaseURL:      baseURL,
		Provider:     provider,
	}, nil
}

func resolveGitHubAuthMode(settings Settings, alias string, account GitHubAccountEntry, overrides githubAuthOverrides) (githubbridge.AuthMode, string, error) {
	if value := strings.TrimSpace(overrides.AuthMode); value != "" {
		mode, err := githubbridge.ParseAuthMode(value)
		if err != nil {
			return "", "", err
		}
		return mode, "flag:--auth-mode", nil
	}
	if value := strings.TrimSpace(account.AuthMode); value != "" {
		mode, err := githubbridge.ParseAuthMode(value)
		if err != nil {
			return "", "", fmt.Errorf("invalid github account auth_mode for %q: %w", firstNonEmpty(alias, "default"), err)
		}
		return mode, "settings.auth_mode", nil
	}
	if value := strings.TrimSpace(os.Getenv("GITHUB_AUTH_MODE")); value != "" {
		mode, err := githubbridge.ParseAuthMode(value)
		if err != nil {
			return "", "", err
		}
		return mode, "env:GITHUB_AUTH_MODE", nil
	}
	if value := strings.TrimSpace(os.Getenv("GITHUB_DEFAULT_AUTH_MODE")); value != "" {
		mode, err := githubbridge.ParseAuthMode(value)
		if err != nil {
			return "", "", err
		}
		return mode, "env:GITHUB_DEFAULT_AUTH_MODE", nil
	}
	if value := strings.TrimSpace(settings.Github.DefaultAuthMode); value != "" {
		mode, err := githubbridge.ParseAuthMode(value)
		if err != nil {
			return "", "", fmt.Errorf("invalid github default_auth_mode: %w", err)
		}
		return mode, "settings.default_auth_mode", nil
	}
	return githubbridge.AuthModeApp, "", nil
}

func resolveGitHubAccountSelection(settings Settings, accountFlag string) (string, GitHubAccountEntry) {
	selected := strings.TrimSpace(accountFlag)
	if selected == "" {
		selected = strings.TrimSpace(settings.Github.DefaultAccount)
	}
	if selected == "" {
		selected = strings.TrimSpace(os.Getenv("GITHUB_DEFAULT_ACCOUNT"))
	}
	if selected == "" {
		aliases := githubAccountAliases(settings)
		if len(aliases) == 1 {
			selected = aliases[0]
		}
	}
	if selected == "" {
		return "", GitHubAccountEntry{}
	}
	if entry, ok := settings.Github.Accounts[selected]; ok {
		return selected, entry
	}
	return selected, GitHubAccountEntry{}
}

func githubAccountAliases(settings Settings) []string {
	if len(settings.Github.Accounts) == 0 {
		return nil
	}
	out := make([]string, 0, len(settings.Github.Accounts))
	for alias := range settings.Github.Accounts {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

func resolveGitHubAppID(alias string, account GitHubAccountEntry, overrides githubAuthOverrides) (int64, string) {
	if overrides.AppID > 0 {
		return overrides.AppID, "flag:--app-id"
	}
	if account.AppID > 0 {
		return account.AppID, "settings.app_id"
	}
	if ref := strings.TrimSpace(account.AppIDEnv); ref != "" {
		if parsed := parseInt64(os.Getenv(ref)); parsed > 0 {
			return parsed, "env:" + ref
		}
	}
	if parsed := parseInt64(resolveGitHubEnv(alias, account, "APP_ID")); parsed > 0 {
		return parsed, "env:" + githubAccountEnvPrefix(alias, account) + "APP_ID"
	}
	if parsed := parseInt64(os.Getenv("GITHUB_APP_ID")); parsed > 0 {
		return parsed, "env:GITHUB_APP_ID"
	}
	return 0, ""
}

func resolveGitHubAppKey(alias string, account GitHubAccountEntry, overrides githubAuthOverrides) (string, string) {
	if strings.TrimSpace(overrides.AppKey) != "" {
		return strings.TrimSpace(overrides.AppKey), "flag:--app-key"
	}
	if strings.TrimSpace(account.AppPrivateKeyPEM) != "" {
		return strings.TrimSpace(account.AppPrivateKeyPEM), "settings.app_private_key_pem"
	}
	if ref := strings.TrimSpace(account.AppPrivateKeyEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveGitHubEnv(alias, account, "APP_PRIVATE_KEY_PEM")); value != "" {
		return value, "env:" + githubAccountEnvPrefix(alias, account) + "APP_PRIVATE_KEY_PEM"
	}
	if value := strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY_PEM")); value != "" {
		return value, "env:GITHUB_APP_PRIVATE_KEY_PEM"
	}
	return "", ""
}

func resolveGitHubInstallationID(alias string, account GitHubAccountEntry, overrides githubAuthOverrides) (int64, string) {
	if overrides.InstallationID > 0 {
		return overrides.InstallationID, "flag:--installation-id"
	}
	if account.InstallationID > 0 {
		return account.InstallationID, "settings.installation_id"
	}
	if ref := strings.TrimSpace(account.InstallationEnv); ref != "" {
		if parsed := parseInt64(os.Getenv(ref)); parsed > 0 {
			return parsed, "env:" + ref
		}
	}
	if parsed := parseInt64(resolveGitHubEnv(alias, account, "INSTALLATION_ID")); parsed > 0 {
		return parsed, "env:" + githubAccountEnvPrefix(alias, account) + "INSTALLATION_ID"
	}
	if parsed := parseInt64(os.Getenv("GITHUB_INSTALLATION_ID")); parsed > 0 {
		return parsed, "env:GITHUB_INSTALLATION_ID"
	}
	return 0, ""
}

func resolveGitHubOAuthAccessToken(alias string, account GitHubAccountEntry, overrides githubAuthOverrides) (string, string) {
	if value := strings.TrimSpace(overrides.AccessToken); value != "" {
		return value, "flag:--token"
	}
	if value := strings.TrimSpace(account.OAuthAccessToken); value != "" {
		return value, "settings.oauth_access_token"
	}
	if ref := strings.TrimSpace(account.OAuthTokenEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	prefix := githubAccountEnvPrefix(alias, account)
	if value := strings.TrimSpace(resolveGitHubEnv(alias, account, "OAUTH_ACCESS_TOKEN")); value != "" {
		return value, "env:" + prefix + "OAUTH_ACCESS_TOKEN"
	}
	if value := strings.TrimSpace(resolveGitHubEnv(alias, account, "TOKEN")); value != "" {
		return value, "env:" + prefix + "TOKEN"
	}
	if value := strings.TrimSpace(os.Getenv("GITHUB_OAUTH_TOKEN")); value != "" {
		return value, "env:GITHUB_OAUTH_TOKEN"
	}
	if value := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); value != "" {
		return value, "env:GITHUB_TOKEN"
	}
	if value := strings.TrimSpace(os.Getenv("GH_TOKEN")); value != "" {
		return value, "env:GH_TOKEN"
	}
	if value := strings.TrimSpace(os.Getenv("GITHUB_PAT")); value != "" {
		return value, "env:GITHUB_PAT"
	}
	if value := strings.TrimSpace(os.Getenv("GH_PAT")); value != "" {
		return value, "env:GH_PAT"
	}
	return "", ""
}

func resolveGitHubEnv(alias string, account GitHubAccountEntry, key string) string {
	prefix := githubAccountEnvPrefix(alias, account)
	if prefix != "" {
		if value := strings.TrimSpace(os.Getenv(prefix + key)); value != "" {
			return value
		}
	}
	return ""
}

func githubAccountEnvPrefix(alias string, account GitHubAccountEntry) string {
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
	return "GITHUB_" + alias + "_"
}

func slugUpper(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return strings.Trim(strings.ReplaceAll(b.String(), "__", "_"), "_")
}

func parseInt64(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func cmdGithubAuth(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si github auth status [--account <alias>] [--owner <owner>] [--auth-mode <app|oauth>] [--json]")
	if !routedOK {
		return
	}
	args = routedArgs
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "status":
		cmdGithubAuthStatus(args[1:])
	default:
		printUnknown("github auth", args[0])
	}
}

func cmdGithubAuthStatus(args []string) {
	fs := flag.NewFlagSet("github auth status", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si github auth status [--account <alias>] [--owner <owner>] [--auth-mode <app|oauth>] [--json]")
		return
	}
	runtime, err := resolveGithubRuntimeContext(*account, *owner, *baseURL, githubAuthOverrides{
		AuthMode:       *authMode,
		AccessToken:    *token,
		AppID:          *appID,
		AppKey:         *appKey,
		InstallationID: *installationID,
	})
	if err != nil {
		fatal(err)
	}
	tokenPreview := "-"
	source := strings.TrimSpace(runtime.Source)
	if provider := runtime.Provider; provider != nil {
		token, tokenErr := provider.Token(context.Background(), githubbridge.TokenRequest{Owner: runtime.Owner})
		if tokenErr == nil {
			tokenPreview = previewGitHubSecret(token.Value)
		}
	}
	payload := map[string]any{
		"account_alias": runtime.AccountAlias,
		"owner":         runtime.Owner,
		"auth_mode":     runtime.AuthMode,
		"base_url":      runtime.BaseURL,
		"source":        source,
		"token_preview": tokenPreview,
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("GitHub auth:"), styleSuccess("ready"))
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatGithubContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(source))
	fmt.Printf("%s %s\n", styleHeading("Token preview:"), tokenPreview)
}

func cmdGithubDoctor(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "public": true})
	fs := flag.NewFlagSet("github doctor", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	public := fs.Bool("public", false, "run unauthenticated provider public probe")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si github doctor [--account <alias>] [--owner <owner>] [--auth-mode <app|oauth>] [--public] [--json]")
		return
	}
	if *public {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err := runPublicProviderDoctor(ctx, providers.GitHub, *baseURL)
		if err != nil {
			fatal(err)
		}
		printPublicDoctorResult("GitHub", result, *jsonOut)
		return
	}
	runtime, err := resolveGithubRuntimeContext(*account, *owner, *baseURL, githubAuthOverrides{
		AuthMode:       *authMode,
		AccessToken:    *token,
		AppID:          *appID,
		AppKey:         *appKey,
		InstallationID: *installationID,
	})
	if err != nil {
		fatal(err)
	}
	client, err := buildGithubClient(runtime)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	type check struct {
		Name   string `json:"name"`
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	checks := make([]check, 0, 2)
	resp, err := client.Do(ctx, githubbridge.Request{Method: "GET", Path: "/rate_limit"})
	if err != nil {
		checks = append(checks, check{Name: "rate_limit", OK: false, Detail: err.Error()})
	} else {
		checks = append(checks, check{Name: "rate_limit", OK: true, Detail: summarizeGitHubResponse(resp)})
	}
	ok := true
	for _, item := range checks {
		ok = ok && item.OK
	}
	payload := map[string]any{"ok": ok, "context": runtime, "checks": checks}
	if *jsonOut {
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
	if ok {
		fmt.Printf("%s %s\n", styleHeading("GitHub doctor:"), styleSuccess("ok"))
	} else {
		fmt.Printf("%s %s\n", styleHeading("GitHub doctor:"), styleError("issues found"))
	}
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatGithubContext(runtime))
	rows := make([][]string, 0, len(checks))
	for _, item := range checks {
		icon := styleSuccess("OK")
		if !item.OK {
			icon = styleError("ERR")
		}
		rows = append(rows, []string{icon, item.Name, strings.TrimSpace(item.Detail)})
	}
	printAlignedRows(rows, 2, "  ")
	if !ok {
		os.Exit(1)
	}
}

func cmdGithubContext(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si github context <list|current|use>")
	if !routedOK {
		return
	}
	args = routedArgs
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdGithubContextList(args[1:])
	case "current":
		cmdGithubContextCurrent(args[1:])
	case "use":
		cmdGithubContextUse(args[1:])
	default:
		printUnknown("github context", args[0])
	}
}

func cmdGithubContextList(args []string) {
	fs := flag.NewFlagSet("github context list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si github context list [--json]")
		return
	}
	settings := loadSettingsOrDefault()
	aliases := githubAccountAliases(settings)
	rows := make([]map[string]string, 0, len(aliases))
	for _, alias := range aliases {
		entry := settings.Github.Accounts[alias]
		authMode := strings.TrimSpace(entry.AuthMode)
		if authMode == "" {
			authMode = strings.TrimSpace(settings.Github.DefaultAuthMode)
		}
		if authMode == "" {
			authMode = string(githubbridge.AuthModeApp)
		}
		rows = append(rows, map[string]string{
			"alias":        alias,
			"name":         strings.TrimSpace(entry.Name),
			"owner":        strings.TrimSpace(entry.Owner),
			"auth_mode":    authMode,
			"default":      boolString(alias == strings.TrimSpace(settings.Github.DefaultAccount)),
			"api_base_url": strings.TrimSpace(entry.APIBaseURL),
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
		infof("no github accounts configured in settings")
		return
	}
	headers := []string{
		styleHeading("ALIAS"),
		styleHeading("DEFAULT"),
		styleHeading("AUTH"),
		styleHeading("OWNER"),
		styleHeading("BASE URL"),
		styleHeading("NAME"),
	}
	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		tableRows = append(tableRows, []string{
			row["alias"],
			row["default"],
			orDash(row["auth_mode"]),
			orDash(row["owner"]),
			orDash(row["api_base_url"]),
			orDash(row["name"]),
		})
	}
	printAlignedTable(headers, tableRows, 2)
}

func cmdGithubContextCurrent(args []string) {
	fs := flag.NewFlagSet("github context current", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si github context current [--json]")
		return
	}
	runtime, err := resolveGithubRuntimeContext("", "", "", githubAuthOverrides{})
	if err != nil {
		fatal(err)
	}
	payload := map[string]any{
		"account_alias": runtime.AccountAlias,
		"owner":         runtime.Owner,
		"auth_mode":     runtime.AuthMode,
		"base_url":      runtime.BaseURL,
		"source":        runtime.Source,
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Current github context:"), formatGithubContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
}

func cmdGithubContextUse(args []string) {
	fs := flag.NewFlagSet("github context use", flag.ExitOnError)
	account := fs.String("account", "", "default account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "default github api base url")
	authMode := fs.String("auth-mode", "", "default auth mode (app|oauth)")
	tokenEnv := fs.String("token-env", "", "oauth token env-var reference for selected account")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si github context use [--account <alias>] [--owner <owner>] [--base-url <url>] [--auth-mode <app|oauth>] [--token-env <env_key>]")
		return
	}
	settings := loadSettingsOrDefault()
	if value := strings.TrimSpace(*account); value != "" {
		settings.Github.DefaultAccount = value
	}
	if value := strings.TrimSpace(*owner); value != "" {
		settings.Github.DefaultOwner = value
	}
	if value := strings.TrimSpace(*baseURL); value != "" {
		settings.Github.APIBaseURL = value
	}
	if value := strings.TrimSpace(*authMode); value != "" {
		mode, err := githubbridge.ParseAuthMode(value)
		if err != nil {
			fatal(err)
		}
		settings.Github.DefaultAuthMode = string(mode)
	}
	if alias := strings.TrimSpace(*account); alias != "" {
		if settings.Github.Accounts == nil {
			settings.Github.Accounts = map[string]GitHubAccountEntry{}
		}
		entry := settings.Github.Accounts[alias]
		if value := strings.TrimSpace(*tokenEnv); value != "" {
			entry.OAuthTokenEnv = value
		}
		if value := strings.TrimSpace(*authMode); value != "" {
			mode, err := githubbridge.ParseAuthMode(value)
			if err != nil {
				fatal(err)
			}
			entry.AuthMode = string(mode)
		}
		settings.Github.Accounts[alias] = entry
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	successf("github context updated")
}

func previewGitHubSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "-"
	}
	secret = githubbridge.RedactSensitive(secret)
	if len(secret) <= 10 {
		return secret
	}
	return secret[:8] + "..."
}

func summarizeGitHubResponse(resp githubbridge.Response) string {
	if len(resp.List) > 0 {
		return fmt.Sprintf("%d item(s)", len(resp.List))
	}
	if len(resp.Data) == 0 {
		return "ok"
	}
	for _, key := range []string{"id", "name", "full_name", "message"} {
		if value, ok := resp.Data[key]; ok {
			return fmt.Sprintf("%s=%s", key, stringifyGitHubAny(value))
		}
	}
	return "ok"
}
