package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"si/tools/si/internal/providers"
	"si/tools/si/internal/youtubebridge"
)

func cmdGoogleYouTubeAuth(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si google youtube auth <status|login|logout>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "status":
		cmdGoogleYouTubeAuthStatus(rest)
	case "login":
		cmdGoogleYouTubeAuthLogin(rest)
	case "logout":
		cmdGoogleYouTubeAuthLogout(rest)
	default:
		printUnknown("google youtube auth", sub)
		printUsage("usage: si google youtube auth <status|login|logout>")
	}
}

func cmdGoogleYouTubeAuthStatus(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube auth status", args, false)
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google youtube auth status [--account <alias>] [--env <prod|staging|dev>] [--mode <api-key|oauth>] [--json]")
		return
	}
	runtime, err := resolveGoogleYouTubeRuntimeContext(common.runtimeInput())
	if err != nil {
		fatal(err)
	}
	client, err := buildGoogleYouTubeClient(runtime)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	verifyReq := youtubebridge.Request{
		Method: http.MethodGet,
		Path:   "/youtube/v3/search",
		Params: map[string]string{"part": "id", "maxResults": "1", "type": "video", "q": "music"},
	}
	if runtime.AuthMode == "oauth" {
		verifyReq = youtubebridge.Request{
			Method: http.MethodGet,
			Path:   "/youtube/v3/channels",
			Params: map[string]string{"part": "id", "mine": "true", "maxResults": "1"},
		}
	}
	resp, verifyErr := client.Do(ctx, verifyReq)
	status := "error"
	if verifyErr == nil {
		status = "ready"
	}
	payload := map[string]any{
		"status":          status,
		"account_alias":   runtime.AccountAlias,
		"project_id":      runtime.ProjectID,
		"environment":     runtime.Environment,
		"auth_mode":       runtime.AuthMode,
		"language_code":   runtime.LanguageCode,
		"region_code":     runtime.RegionCode,
		"source":          runtime.Source,
		"token_source":    runtime.TokenSource,
		"session_source":  runtime.SessionSource,
		"api_key_preview": previewGoogleYouTubeSecret(runtime.APIKey),
		"access_preview":  previewGoogleYouTubeSecret(runtime.OAuth.AccessToken),
		"refresh_present": strings.TrimSpace(runtime.OAuth.RefreshToken) != "",
		"base_url":        runtime.BaseURL,
		"upload_base_url": runtime.UploadBaseURL,
	}
	if verifyErr == nil {
		payload["verify"] = resp.Data
	} else {
		payload["verify_error"] = verifyErr.Error()
	}
	if common.json() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		if verifyErr != nil {
			os.Exit(1)
		}
		return
	}
	if verifyErr != nil {
		fmt.Printf("%s %s\n", styleHeading("Google YouTube auth:"), styleError("error"))
		fmt.Printf("%s %s\n", styleHeading("Context:"), formatGoogleYouTubeContext(runtime))
		printGoogleYouTubeError(verifyErr)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Google YouTube auth:"), styleSuccess("ready"))
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatGoogleYouTubeContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
	fmt.Printf("%s %s\n", styleHeading("Token source:"), orDash(runtime.TokenSource))
	if runtime.AuthMode == "api-key" {
		fmt.Printf("%s %s\n", styleHeading("Key preview:"), previewGoogleYouTubeSecret(runtime.APIKey))
	} else {
		fmt.Printf("%s %s\n", styleHeading("Access preview:"), previewGoogleYouTubeSecret(runtime.OAuth.AccessToken))
		fmt.Printf("%s %s\n", styleHeading("Refresh token:"), boolString(strings.TrimSpace(runtime.OAuth.RefreshToken) != ""))
	}
}

func cmdGoogleYouTubeAuthLogin(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube auth login", args, false)
	scopesRaw := fs.String("scopes", "", "comma-separated oauth scopes")
	deviceOnly := fs.Bool("device", true, "use device authorization flow")
	timeout := fs.Duration("timeout", 18*time.Minute, "device flow timeout")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google youtube auth login [--account <alias>] [--env <prod|staging|dev>] [--scopes <csv>] [--device] [--json]")
		return
	}
	if !*deviceOnly {
		warnf("only device flow is currently implemented; continuing with --device")
	}
	input := common.runtimeInput()
	if strings.TrimSpace(input.AuthModeFlag) == "" {
		input.AuthModeFlag = "oauth"
	}
	runtime, err := resolveGoogleYouTubeRuntimeContext(input)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(runtime.AuthMode) != "oauth" {
		fatal(fmt.Errorf("auth login requires oauth mode"))
	}
	if strings.TrimSpace(runtime.OAuth.ClientID) == "" {
		fatal(fmt.Errorf("oauth client id is required (set GOOGLE_<ACCOUNT>_YOUTUBE_CLIENT_ID or --client-id)"))
	}
	scopes := parseGoogleCSVList(*scopesRaw)
	if len(scopes) == 0 {
		scopes = defaultGoogleYouTubeScopes()
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	device, err := startGoogleOAuthDeviceFlow(ctx, runtime.OAuth.ClientID, scopes)
	if err != nil {
		fatal(err)
	}
	if common.json() {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"account_alias":      runtime.AccountAlias,
			"environment":        runtime.Environment,
			"verification_url":   device.VerificationURL,
			"user_code":          device.UserCode,
			"expires_in_seconds": device.ExpiresIn,
			"interval_seconds":   device.Interval,
			"scopes":             scopes,
		})
	} else {
		fmt.Printf("%s %s\n", styleHeading("Google YouTube device login:"), formatGoogleYouTubeContext(runtime))
		fmt.Printf("%s %s\n", styleHeading("Open URL:"), device.VerificationURL)
		fmt.Printf("%s %s\n", styleHeading("User code:"), styleWarn(device.UserCode))
		fmt.Printf("%s %d seconds\n", styleHeading("Timeout:"), int(timeout.Seconds()))
	}
	token, err := pollGoogleOAuthDeviceToken(ctx, runtime.OAuth.ClientID, runtime.OAuth.ClientSecret, device.DeviceCode, device.Interval, *timeout)
	if err != nil {
		fatal(err)
	}
	expiresAt := googleOAuthExpiresAt(token.ExpiresIn)
	entry := googleOAuthTokenEntry{
		AccountAlias: runtime.AccountAlias,
		Environment:  runtime.Environment,
		AccessToken:  strings.TrimSpace(token.AccessToken),
		RefreshToken: strings.TrimSpace(token.RefreshToken),
		TokenType:    strings.TrimSpace(token.TokenType),
		Scope:        strings.TrimSpace(token.Scope),
		ExpiresAt:    expiresAt.UTC().Format(time.RFC3339),
	}
	if err := saveGoogleOAuthTokenEntry(runtime.AccountAlias, runtime.Environment, entry); err != nil {
		fatal(err)
	}
	if common.json() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"status":                "logged-in",
			"account_alias":         runtime.AccountAlias,
			"environment":           runtime.Environment,
			"scope":                 entry.Scope,
			"expires_at":            entry.ExpiresAt,
			"refresh_token_present": strings.TrimSpace(entry.RefreshToken) != "",
		})
		return
	}
	successf("youtube oauth login stored for account=%s env=%s", orDash(runtime.AccountAlias), runtime.Environment)
	fmt.Printf("%s %s\n", styleHeading("Scope:"), orDash(entry.Scope))
	fmt.Printf("%s %s\n", styleHeading("Expires:"), formatISODateWithGitHubRelativeNow(entry.ExpiresAt))
	if strings.TrimSpace(entry.RefreshToken) == "" {
		warnf("refresh token was not returned; future auth refresh may require re-login")
	}
}

func cmdGoogleYouTubeAuthLogout(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube auth logout", args, false)
	revoke := fs.Bool("revoke", false, "revoke stored oauth token with Google before logout")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google youtube auth logout [--account <alias>] [--env <prod|staging|dev>] [--revoke] [--json]")
		return
	}
	input := common.runtimeInput()
	if strings.TrimSpace(input.AuthModeFlag) == "" {
		input.AuthModeFlag = "oauth"
	}
	runtime, err := resolveGoogleYouTubeRuntimeContext(input)
	if err != nil {
		fatal(err)
	}
	entry, ok := loadGoogleOAuthTokenEntry(runtime.AccountAlias, runtime.Environment)
	if !ok {
		payload := map[string]any{"status": "already-logged-out", "account_alias": runtime.AccountAlias, "environment": runtime.Environment}
		if common.json() {
			_ = json.NewEncoder(os.Stdout).Encode(payload)
			return
		}
		infof("youtube oauth token already absent for account=%s env=%s", orDash(runtime.AccountAlias), runtime.Environment)
		return
	}
	if *revoke {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err = revokeGoogleOAuthToken(ctx, firstNonEmpty(entry.RefreshToken, entry.AccessToken))
		cancel()
		if err != nil {
			warnf("token revoke failed: %v", err)
		}
	}
	if err := deleteGoogleOAuthTokenEntry(runtime.AccountAlias, runtime.Environment); err != nil {
		fatal(err)
	}
	payload := map[string]any{"status": "logged-out", "account_alias": runtime.AccountAlias, "environment": runtime.Environment}
	if common.json() {
		_ = json.NewEncoder(os.Stdout).Encode(payload)
		return
	}
	successf("youtube oauth logout complete for account=%s env=%s", orDash(runtime.AccountAlias), runtime.Environment)
}

func cmdGoogleYouTubeContext(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si google youtube context <list|current|use>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGoogleYouTubeContextList(rest)
	case "current":
		cmdGoogleYouTubeContextCurrent(rest)
	case "use":
		cmdGoogleYouTubeContextUse(rest)
	default:
		printUnknown("google youtube context", sub)
		printUsage("usage: si google youtube context <list|current|use>")
	}
}

func cmdGoogleYouTubeContextList(args []string) {
	fs := flag.NewFlagSet("google youtube context list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google youtube context list [--json]")
		return
	}
	settings := loadSettingsOrDefault()
	aliases := googleYouTubeAllAccountAliases(settings)
	rows := make([]map[string]string, 0, len(aliases))
	for _, alias := range aliases {
		entry := settings.Google.YouTube.Accounts[alias]
		rows = append(rows, map[string]string{
			"alias":        alias,
			"name":         strings.TrimSpace(entry.Name),
			"project":      strings.TrimSpace(entry.ProjectID),
			"default":      boolString(alias == strings.TrimSpace(settings.Google.DefaultAccount)),
			"auth_mode":    strings.TrimSpace(settings.Google.YouTube.DefaultAuthMode),
			"language":     strings.TrimSpace(entry.DefaultLanguageCode),
			"region":       strings.TrimSpace(entry.DefaultRegionCode),
			"vault_prefix": strings.TrimSpace(entry.VaultPrefix),
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
		infof("no google youtube accounts configured in settings")
		return
	}
	fmt.Printf("%s %s %s %s %s %s %s\n",
		padRightANSI(styleHeading("ALIAS"), 18),
		padRightANSI(styleHeading("DEFAULT"), 8),
		padRightANSI(styleHeading("PROJECT"), 28),
		padRightANSI(styleHeading("AUTH"), 8),
		padRightANSI(styleHeading("LANG"), 8),
		padRightANSI(styleHeading("REGION"), 8),
		styleHeading("NAME"),
	)
	sort.Slice(rows, func(i, j int) bool { return rows[i]["alias"] < rows[j]["alias"] })
	for _, row := range rows {
		fmt.Printf("%s %s %s %s %s %s %s\n",
			padRightANSI(orDash(row["alias"]), 18),
			padRightANSI(orDash(row["default"]), 8),
			padRightANSI(orDash(row["project"]), 28),
			padRightANSI(orDash(row["auth_mode"]), 8),
			padRightANSI(orDash(row["language"]), 8),
			padRightANSI(orDash(row["region"]), 8),
			orDash(row["name"]),
		)
	}
}

func cmdGoogleYouTubeContextCurrent(args []string) {
	fs := flag.NewFlagSet("google youtube context current", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google youtube context current [--json]")
		return
	}
	runtime, err := resolveGoogleYouTubeRuntimeContext(googleYouTubeRuntimeContextInput{})
	if err != nil {
		fatal(err)
	}
	payload := map[string]any{
		"account_alias":   runtime.AccountAlias,
		"project_id":      runtime.ProjectID,
		"environment":     runtime.Environment,
		"auth_mode":       runtime.AuthMode,
		"language_code":   runtime.LanguageCode,
		"region_code":     runtime.RegionCode,
		"base_url":        runtime.BaseURL,
		"upload_base_url": runtime.UploadBaseURL,
		"source":          runtime.Source,
		"token_source":    runtime.TokenSource,
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Current google youtube context:"), formatGoogleYouTubeContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
}

func cmdGoogleYouTubeContextUse(args []string) {
	fs := flag.NewFlagSet("google youtube context use", flag.ExitOnError)
	account := fs.String("account", "", "default account alias")
	env := fs.String("env", "", "default environment (prod|staging|dev)")
	authMode := fs.String("mode", "", "default auth mode (api-key|oauth)")
	language := fs.String("language", "", "default language code")
	region := fs.String("region", "", "default region code")
	baseURL := fs.String("base-url", "", "default youtube api base url")
	uploadBaseURL := fs.String("upload-base-url", "", "default youtube upload base url")
	projectID := fs.String("project-id", "", "default project id for account")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google youtube context use [--account <alias>] [--env <prod|staging|dev>] [--mode <api-key|oauth>] [--language <lc>] [--region <rc>] [--base-url <url>] [--upload-base-url <url>] [--project-id <id>]")
		return
	}
	settings := loadSettingsOrDefault()
	if value := strings.TrimSpace(*account); value != "" {
		settings.Google.DefaultAccount = value
	}
	if value := strings.TrimSpace(*env); value != "" {
		parsed, err := parseGoogleEnvironment(value)
		if err != nil {
			fatal(err)
		}
		settings.Google.DefaultEnv = parsed
	}
	if value := strings.TrimSpace(*authMode); value != "" {
		parsed, err := parseGoogleYouTubeAuthMode(value)
		if err != nil {
			fatal(err)
		}
		settings.Google.YouTube.DefaultAuthMode = parsed
	}
	if value := strings.TrimSpace(*baseURL); value != "" {
		settings.Google.YouTube.APIBaseURL = value
	}
	if value := strings.TrimSpace(*uploadBaseURL); value != "" {
		settings.Google.YouTube.UploadBaseURL = value
	}
	alias := strings.TrimSpace(*account)
	if alias != "" {
		if settings.Google.YouTube.Accounts == nil {
			settings.Google.YouTube.Accounts = map[string]GoogleYouTubeAccountEntry{}
		}
		entry := settings.Google.YouTube.Accounts[alias]
		if value := strings.TrimSpace(*language); value != "" {
			entry.DefaultLanguageCode = value
		}
		if value := strings.TrimSpace(*region); value != "" {
			entry.DefaultRegionCode = value
		}
		if value := strings.TrimSpace(*projectID); value != "" {
			entry.ProjectID = value
		}
		settings.Google.YouTube.Accounts[alias] = entry
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	successf("google youtube context updated")
}

func cmdGoogleYouTubeDoctor(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube doctor", args, false)
	public := fs.Bool("public", false, "run unauthenticated provider public probe")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google youtube doctor [--account <alias>] [--env <prod|staging|dev>] [--mode <api-key|oauth>] [--public] [--json]")
		return
	}
	if *public {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err := runPublicProviderDoctor(ctx, providers.YouTube, stringValue(common.baseURL))
		if err != nil {
			fatal(err)
		}
		printPublicDoctorResult("Google YouTube", result, common.json())
		return
	}
	runtime, err := resolveGoogleYouTubeRuntimeContext(common.runtimeInput())
	if err != nil {
		fatal(err)
	}
	client, err := buildGoogleYouTubeClient(runtime)
	if err != nil {
		fatal(err)
	}
	type check struct {
		Name   string `json:"name"`
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	checks := make([]check, 0, 6)
	addCheck := func(name string, ok bool, detail string) {
		checks = append(checks, check{Name: name, OK: ok, Detail: detail})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	searchResp, searchErr := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/search", Params: map[string]string{"part": "id", "q": "music", "type": "video", "maxResults": "1"}})
	if searchErr != nil {
		addCheck("search.list", false, searchErr.Error())
	} else {
		addCheck("search.list", true, summarizeGoogleYouTubeResponse(searchResp))
	}

	langResp, langErr := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/i18nLanguages", Params: map[string]string{"part": "snippet", "hl": firstNonEmpty(runtime.LanguageCode, "en_US")}})
	if langErr != nil {
		addCheck("i18nLanguages.list", false, langErr.Error())
	} else {
		addCheck("i18nLanguages.list", true, summarizeGoogleYouTubeResponse(langResp))
	}

	if runtime.AuthMode == "oauth" {
		mineResp, mineErr := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/channels", Params: map[string]string{"part": "id,snippet", "mine": "true", "maxResults": "1"}})
		if mineErr != nil {
			addCheck("channels.list(mine)", false, mineErr.Error())
		} else {
			addCheck("channels.list(mine)", true, summarizeGoogleYouTubeResponse(mineResp))
		}
	} else {
		addCheck("channels.list(mine)", true, "skipped (oauth only)")
	}

	ok := true
	for _, item := range checks {
		ok = ok && item.OK
	}
	payload := map[string]any{"ok": ok, "context": runtime, "checks": checks}
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
	if ok {
		fmt.Printf("%s %s\n", styleHeading("Google YouTube doctor:"), styleSuccess("ok"))
	} else {
		fmt.Printf("%s %s\n", styleHeading("Google YouTube doctor:"), styleError("issues found"))
	}
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatGoogleYouTubeContext(runtime))
	for _, item := range checks {
		icon := styleSuccess("OK")
		if !item.OK {
			icon = styleError("ERR")
		}
		fmt.Printf("  %s %s %s\n", padRightANSI(icon, 4), padRightANSI(item.Name, 20), strings.TrimSpace(item.Detail))
	}
	if !ok {
		os.Exit(1)
	}
}

func summarizeGoogleYouTubeResponse(resp youtubebridge.Response) string {
	if len(resp.List) > 0 {
		return fmt.Sprintf("%d item(s)", len(resp.List))
	}
	if len(resp.Data) == 0 {
		return "ok"
	}
	for _, key := range []string{"kind", "etag", "nextPageToken", "pageInfo"} {
		if value, ok := resp.Data[key]; ok {
			return fmt.Sprintf("%s=%s", key, stringifyGoogleYouTubeAny(value))
		}
	}
	return "ok"
}

func revokeGoogleOAuthToken(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	form := url.Values{}
	form.Set("token", token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/revoke", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return youtubebridge.NormalizeHTTPError(resp.StatusCode, resp.Header, string(body))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
