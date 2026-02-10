package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"si/tools/si/internal/googleplacesbridge"
	"si/tools/si/internal/providers"
)

type googlePlacesRuntimeContextInput struct {
	AccountFlag   string
	EnvFlag       string
	APIKeyFlag    string
	BaseURLFlag   string
	ProjectIDFlag string
	LanguageFlag  string
	RegionFlag    string
}

func resolveGooglePlacesRuntimeContext(input googlePlacesRuntimeContextInput) (googlePlacesRuntimeContext, error) {
	settings := loadSettingsOrDefault()
	alias, account := resolveGoogleAccountSelection(settings, input.AccountFlag)
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
		return googlePlacesRuntimeContext{}, err
	}

	baseURL := strings.TrimSpace(input.BaseURLFlag)
	if baseURL == "" {
		baseURL = strings.TrimSpace(account.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(settings.Google.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("GOOGLE_API_BASE_URL"))
	}
	if baseURL == "" {
		baseURL = "https://places.googleapis.com"
	}

	projectID, projectSource := resolveGoogleProjectID(alias, account, strings.TrimSpace(input.ProjectIDFlag))
	apiKey, keySource := resolveGooglePlacesAPIKey(alias, account, parsedEnv, strings.TrimSpace(input.APIKeyFlag))
	if strings.TrimSpace(apiKey) == "" {
		prefix := googleAccountEnvPrefix(alias, account)
		if prefix == "" {
			prefix = "GOOGLE_<ACCOUNT>_"
		}
		return googlePlacesRuntimeContext{}, fmt.Errorf("google places api key not found (set --api-key, %sPLACES_API_KEY, or GOOGLE_PLACES_API_KEY)", prefix)
	}

	languageCode, languageSource := resolveGoogleLanguageCode(alias, account, strings.TrimSpace(input.LanguageFlag))
	regionCode, regionSource := resolveGoogleRegionCode(alias, account, strings.TrimSpace(input.RegionFlag))

	source := strings.Join(nonEmpty(keySource, projectSource, languageSource, regionSource), ",")
	return googlePlacesRuntimeContext{
		AccountAlias: alias,
		ProjectID:    projectID,
		Environment:  parsedEnv,
		APIKey:       apiKey,
		LanguageCode: languageCode,
		RegionCode:   regionCode,
		Source:       source,
		BaseURL:      baseURL,
	}, nil
}

func resolveGoogleAccountSelection(settings Settings, accountFlag string) (string, GoogleAccountEntry) {
	selected := strings.TrimSpace(accountFlag)
	if selected == "" {
		selected = strings.TrimSpace(settings.Google.DefaultAccount)
	}
	if selected == "" {
		selected = strings.TrimSpace(os.Getenv("GOOGLE_DEFAULT_ACCOUNT"))
	}
	if selected == "" {
		aliases := googleAccountAliases(settings)
		if len(aliases) == 1 {
			selected = aliases[0]
		}
	}
	if selected == "" {
		return "", GoogleAccountEntry{}
	}
	if entry, ok := settings.Google.Accounts[selected]; ok {
		return selected, entry
	}
	return selected, GoogleAccountEntry{}
}

func resolveGoogleProjectID(alias string, account GoogleAccountEntry, override string) (string, string) {
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
	if value := strings.TrimSpace(resolveGoogleEnv(alias, account, "PROJECT_ID")); value != "" {
		return value, "env:" + googleAccountEnvPrefix(alias, account) + "PROJECT_ID"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_PROJECT_ID")); value != "" {
		return value, "env:GOOGLE_PROJECT_ID"
	}
	return "", ""
}

func resolveGooglePlacesAPIKey(alias string, account GoogleAccountEntry, env string, override string) (string, string) {
	if override != "" {
		return override, "flag:--api-key"
	}
	switch normalizeGoogleEnvironment(env) {
	case "prod":
		if ref := strings.TrimSpace(account.ProdPlacesAPIKeyEnv); ref != "" {
			if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
				return value, "env:" + ref
			}
		}
		if value := strings.TrimSpace(resolveGoogleEnv(alias, account, "PROD_PLACES_API_KEY")); value != "" {
			return value, "env:" + googleAccountEnvPrefix(alias, account) + "PROD_PLACES_API_KEY"
		}
	case "staging":
		if ref := strings.TrimSpace(account.StagingPlacesAPIKeyEnv); ref != "" {
			if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
				return value, "env:" + ref
			}
		}
		if value := strings.TrimSpace(resolveGoogleEnv(alias, account, "STAGING_PLACES_API_KEY")); value != "" {
			return value, "env:" + googleAccountEnvPrefix(alias, account) + "STAGING_PLACES_API_KEY"
		}
	case "dev":
		if ref := strings.TrimSpace(account.DevPlacesAPIKeyEnv); ref != "" {
			if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
				return value, "env:" + ref
			}
		}
		if value := strings.TrimSpace(resolveGoogleEnv(alias, account, "DEV_PLACES_API_KEY")); value != "" {
			return value, "env:" + googleAccountEnvPrefix(alias, account) + "DEV_PLACES_API_KEY"
		}
	}
	if ref := strings.TrimSpace(account.PlacesAPIKeyEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveGoogleEnv(alias, account, "PLACES_API_KEY")); value != "" {
		return value, "env:" + googleAccountEnvPrefix(alias, account) + "PLACES_API_KEY"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_PLACES_API_KEY")); value != "" {
		return value, "env:GOOGLE_PLACES_API_KEY"
	}
	return "", ""
}

func resolveGoogleLanguageCode(alias string, account GoogleAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--language"
	}
	if value := strings.TrimSpace(account.DefaultLanguageCode); value != "" {
		return value, "settings.default_language_code"
	}
	if value := strings.TrimSpace(resolveGoogleEnv(alias, account, "DEFAULT_LANGUAGE_CODE")); value != "" {
		return value, "env:" + googleAccountEnvPrefix(alias, account) + "DEFAULT_LANGUAGE_CODE"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_DEFAULT_LANGUAGE_CODE")); value != "" {
		return value, "env:GOOGLE_DEFAULT_LANGUAGE_CODE"
	}
	return "", ""
}

func resolveGoogleRegionCode(alias string, account GoogleAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--region"
	}
	if value := strings.TrimSpace(account.DefaultRegionCode); value != "" {
		return value, "settings.default_region_code"
	}
	if value := strings.TrimSpace(resolveGoogleEnv(alias, account, "DEFAULT_REGION_CODE")); value != "" {
		return value, "env:" + googleAccountEnvPrefix(alias, account) + "DEFAULT_REGION_CODE"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_DEFAULT_REGION_CODE")); value != "" {
		return value, "env:GOOGLE_DEFAULT_REGION_CODE"
	}
	return "", ""
}

func cmdGooglePlacesAuth(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google places auth status [--account <alias>] [--env <prod|staging|dev>] [--json]")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "status":
		cmdGooglePlacesAuthStatus(args[1:])
	default:
		printUnknown("google places auth", args[0])
	}
}

func cmdGooglePlacesAuthStatus(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("google places auth status", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	apiKey := fs.String("api-key", "", "override google places api key")
	baseURL := fs.String("base-url", "", "google places api base url")
	projectID := fs.String("project-id", "", "google project id")
	language := fs.String("language", "", "language code (BCP-47)")
	region := fs.String("region", "", "region code (CLDR)")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google places auth status [--account <alias>] [--env <prod|staging|dev>] [--json]")
		return
	}
	runtime, err := resolveGooglePlacesRuntimeContext(googlePlacesRuntimeContextInput{
		AccountFlag:   *account,
		EnvFlag:       *env,
		APIKeyFlag:    *apiKey,
		BaseURLFlag:   *baseURL,
		ProjectIDFlag: *projectID,
		LanguageFlag:  *language,
		RegionFlag:    *region,
	})
	if err != nil {
		fatal(err)
	}
	client, err := buildGooglePlacesClient(runtime)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	fieldMask, _ := resolveGooglePlacesFieldMask(fieldMaskInput{Operation: "autocomplete", Preset: "autocomplete-basic", Required: false})
	verifyResp, verifyErr := client.Do(ctx, googleplacesbridge.Request{
		Method:    "POST",
		Path:      "/v1/places:autocomplete",
		JSONBody:  map[string]any{"input": "a"},
		FieldMask: fieldMask,
	})
	status := "error"
	if verifyErr == nil {
		status = "ready"
	}
	payload := map[string]any{
		"status":        status,
		"account_alias": runtime.AccountAlias,
		"project_id":    runtime.ProjectID,
		"environment":   runtime.Environment,
		"language_code": runtime.LanguageCode,
		"region_code":   runtime.RegionCode,
		"source":        runtime.Source,
		"key_preview":   previewGooglePlacesSecret(runtime.APIKey),
		"base_url":      runtime.BaseURL,
	}
	if verifyErr == nil {
		payload["verify"] = verifyResp.Data
	} else {
		payload["verify_error"] = verifyErr.Error()
	}
	if *jsonOut {
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
		fmt.Printf("%s %s\n", styleHeading("Google Places auth:"), styleError("error"))
		fmt.Printf("%s %s\n", styleHeading("Context:"), formatGooglePlacesContext(runtime))
		printGooglePlacesError(verifyErr)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Google Places auth:"), styleSuccess("ready"))
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatGooglePlacesContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
	fmt.Printf("%s %s\n", styleHeading("Key preview:"), previewGooglePlacesSecret(runtime.APIKey))
}

func cmdGooglePlacesContext(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google places context <list|current|use>")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdGooglePlacesContextList(args[1:])
	case "current":
		cmdGooglePlacesContextCurrent(args[1:])
	case "use":
		cmdGooglePlacesContextUse(args[1:])
	default:
		printUnknown("google places context", args[0])
	}
}

func cmdGooglePlacesContextList(args []string) {
	fs := flag.NewFlagSet("google places context list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google places context list [--json]")
		return
	}
	settings := loadSettingsOrDefault()
	aliases := googleAccountAliases(settings)
	rows := make([]map[string]string, 0, len(aliases))
	for _, alias := range aliases {
		entry := settings.Google.Accounts[alias]
		rows = append(rows, map[string]string{
			"alias":    alias,
			"name":     strings.TrimSpace(entry.Name),
			"project":  strings.TrimSpace(entry.ProjectID),
			"default":  boolString(alias == strings.TrimSpace(settings.Google.DefaultAccount)),
			"language": strings.TrimSpace(entry.DefaultLanguageCode),
			"region":   strings.TrimSpace(entry.DefaultRegionCode),
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
		infof("no google accounts configured in settings")
		return
	}
	fmt.Printf("%s %s %s %s %s %s\n",
		padRightANSI(styleHeading("ALIAS"), 18),
		padRightANSI(styleHeading("DEFAULT"), 8),
		padRightANSI(styleHeading("PROJECT"), 28),
		padRightANSI(styleHeading("LANGUAGE"), 10),
		padRightANSI(styleHeading("REGION"), 8),
		styleHeading("NAME"),
	)
	sort.Slice(rows, func(i, j int) bool { return rows[i]["alias"] < rows[j]["alias"] })
	for _, row := range rows {
		fmt.Printf("%s %s %s %s %s %s\n",
			padRightANSI(orDash(row["alias"]), 18),
			padRightANSI(orDash(row["default"]), 8),
			padRightANSI(orDash(row["project"]), 28),
			padRightANSI(orDash(row["language"]), 10),
			padRightANSI(orDash(row["region"]), 8),
			orDash(row["name"]),
		)
	}
}

func cmdGooglePlacesContextCurrent(args []string) {
	fs := flag.NewFlagSet("google places context current", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google places context current [--json]")
		return
	}
	runtime, err := resolveGooglePlacesRuntimeContext(googlePlacesRuntimeContextInput{})
	if err != nil {
		fatal(err)
	}
	payload := map[string]any{
		"account_alias": runtime.AccountAlias,
		"project_id":    runtime.ProjectID,
		"environment":   runtime.Environment,
		"language_code": runtime.LanguageCode,
		"region_code":   runtime.RegionCode,
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
	fmt.Printf("%s %s\n", styleHeading("Current google places context:"), formatGooglePlacesContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
}

func cmdGooglePlacesContextUse(args []string) {
	fs := flag.NewFlagSet("google places context use", flag.ExitOnError)
	account := fs.String("account", "", "default account alias")
	env := fs.String("env", "", "default environment (prod|staging|dev)")
	language := fs.String("language", "", "default language code")
	region := fs.String("region", "", "default region code")
	baseURL := fs.String("base-url", "", "default places api base url")
	projectID := fs.String("project-id", "", "project id")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google places context use [--account <alias>] [--env <prod|staging|dev>] [--language <lc>] [--region <rc>] [--base-url <url>] [--project-id <id>]")
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
	if value := strings.TrimSpace(*baseURL); value != "" {
		settings.Google.APIBaseURL = value
	}
	alias := strings.TrimSpace(*account)
	if alias != "" {
		if settings.Google.Accounts == nil {
			settings.Google.Accounts = map[string]GoogleAccountEntry{}
		}
		entry := settings.Google.Accounts[alias]
		if value := strings.TrimSpace(*language); value != "" {
			entry.DefaultLanguageCode = value
		}
		if value := strings.TrimSpace(*region); value != "" {
			entry.DefaultRegionCode = value
		}
		if value := strings.TrimSpace(*projectID); value != "" {
			entry.ProjectID = value
		}
		if value := strings.TrimSpace(*baseURL); value != "" {
			entry.APIBaseURL = value
		}
		settings.Google.Accounts[alias] = entry
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	successf("google places context updated")
}

func cmdGooglePlacesDoctor(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "public": true})
	fs := flag.NewFlagSet("google places doctor", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	apiKey := fs.String("api-key", "", "override google places api key")
	baseURL := fs.String("base-url", "", "google places api base url")
	projectID := fs.String("project-id", "", "google project id")
	language := fs.String("language", "", "language code (BCP-47)")
	region := fs.String("region", "", "region code (CLDR)")
	public := fs.Bool("public", false, "run unauthenticated provider public probe")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google places doctor [--account <alias>] [--env <prod|staging|dev>] [--public] [--json]")
		return
	}
	if *public {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err := runPublicProviderDoctor(ctx, providers.GooglePlaces, *baseURL)
		if err != nil {
			fatal(err)
		}
		printPublicDoctorResult("Google Places", result, *jsonOut)
		return
	}
	runtime, err := resolveGooglePlacesRuntimeContext(googlePlacesRuntimeContextInput{
		AccountFlag:   *account,
		EnvFlag:       *env,
		APIKeyFlag:    *apiKey,
		BaseURLFlag:   *baseURL,
		ProjectIDFlag: *projectID,
		LanguageFlag:  *language,
		RegionFlag:    *region,
	})
	if err != nil {
		fatal(err)
	}
	client, err := buildGooglePlacesClient(runtime)
	if err != nil {
		fatal(err)
	}

	type check struct {
		Name   string `json:"name"`
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	checks := make([]check, 0, 4)
	addCheck := func(name string, ok bool, detail string) {
		checks = append(checks, check{Name: name, OK: ok, Detail: detail})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	autocompleteMask, _ := resolveGooglePlacesFieldMask(fieldMaskInput{Operation: "autocomplete", Preset: "autocomplete-basic", Required: false})
	autocompleteResp, autocompleteErr := client.Do(ctx, googleplacesbridge.Request{
		Method:    "POST",
		Path:      "/v1/places:autocomplete",
		JSONBody:  map[string]any{"input": "cafe"},
		FieldMask: autocompleteMask,
	})
	if autocompleteErr != nil {
		addCheck("autocomplete", false, autocompleteErr.Error())
	} else {
		addCheck("autocomplete", true, summarizeGooglePlacesResponse(autocompleteResp))
	}

	textMask, _ := resolveGooglePlacesFieldMask(fieldMaskInput{Operation: "search-text", Preset: "search-basic", Required: true})
	textResp, textErr := client.Do(ctx, googleplacesbridge.Request{
		Method:    "POST",
		Path:      "/v1/places:searchText",
		JSONBody:  map[string]any{"textQuery": "coffee"},
		FieldMask: textMask,
	})
	if textErr != nil {
		addCheck("search-text", false, textErr.Error())
	} else {
		addCheck("search-text", true, summarizeGooglePlacesResponse(textResp))
	}

	ok := true
	for _, item := range checks {
		ok = ok && item.OK
	}
	payload := map[string]any{
		"ok":      ok,
		"context": runtime,
		"checks":  checks,
	}
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
		fmt.Printf("%s %s\n", styleHeading("Google Places doctor:"), styleSuccess("ok"))
	} else {
		fmt.Printf("%s %s\n", styleHeading("Google Places doctor:"), styleError("issues found"))
	}
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatGooglePlacesContext(runtime))
	for _, item := range checks {
		icon := styleSuccess("OK")
		if !item.OK {
			icon = styleError("ERR")
		}
		fmt.Printf("  %s %s %s\n", padRightANSI(icon, 4), padRightANSI(item.Name, 16), strings.TrimSpace(item.Detail))
	}
	if !ok {
		os.Exit(1)
	}
}

func summarizeGooglePlacesResponse(resp googleplacesbridge.Response) string {
	if len(resp.List) > 0 {
		return fmt.Sprintf("%d item(s)", len(resp.List))
	}
	if len(resp.Data) == 0 {
		return "ok"
	}
	for _, key := range []string{"name", "id", "displayName", "nextPageToken"} {
		if value, ok := resp.Data[key]; ok {
			return fmt.Sprintf("%s=%s", key, stringifyGooglePlacesAny(value))
		}
	}
	return "ok"
}

func previewGooglePlacesSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "-"
	}
	secret = googleplacesbridge.RedactSensitive(secret)
	if len(secret) <= 10 {
		return secret
	}
	return secret[:8] + "..."
}
