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

	"si/tools/si/internal/cloudflarebridge"
	"si/tools/si/internal/providers"
)

type cloudflareRuntimeContextInput struct {
	AccountFlag   string
	EnvFlag       string
	ZoneFlag      string
	ZoneIDFlag    string
	TokenFlag     string
	BaseURLFlag   string
	AccountIDFlag string
}

func resolveCloudflareRuntimeContext(input cloudflareRuntimeContextInput) (cloudflareRuntimeContext, error) {
	settings := loadSettingsOrDefault()
	alias, account := resolveCloudflareAccountSelection(settings, input.AccountFlag)
	env := strings.TrimSpace(input.EnvFlag)
	if env == "" {
		env = strings.TrimSpace(settings.Cloudflare.DefaultEnv)
	}
	if env == "" {
		env = strings.TrimSpace(os.Getenv("CLOUDFLARE_DEFAULT_ENV"))
	}
	if env == "" {
		env = "prod"
	}
	parsedEnv, err := parseCloudflareEnvironment(env)
	if err != nil {
		return cloudflareRuntimeContext{}, err
	}

	baseURL := strings.TrimSpace(input.BaseURLFlag)
	if baseURL == "" {
		baseURL = strings.TrimSpace(account.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(settings.Cloudflare.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("CLOUDFLARE_API_BASE_URL"))
	}
	if baseURL == "" {
		baseURL = "https://api.cloudflare.com/client/v4"
	}

	accountID, accountSource := resolveCloudflareAccountID(alias, account, strings.TrimSpace(input.AccountIDFlag))
	token, tokenSource := resolveCloudflareAPIToken(alias, account, strings.TrimSpace(input.TokenFlag))
	if strings.TrimSpace(token) == "" {
		prefix := cloudflareAccountEnvPrefix(alias, account)
		if prefix == "" {
			prefix = "CLOUDFLARE_<ACCOUNT>_"
		}
		return cloudflareRuntimeContext{}, fmt.Errorf("cloudflare api token not found (set --api-token, %sAPI_TOKEN, or CLOUDFLARE_API_TOKEN)", prefix)
	}

	zoneName := strings.TrimSpace(input.ZoneFlag)
	if zoneName == "" {
		zoneName = strings.TrimSpace(account.DefaultZoneName)
	}
	if zoneName == "" {
		zoneName = strings.TrimSpace(resolveCloudflareEnv(alias, account, "DEFAULT_ZONE_NAME"))
	}

	zoneID, zoneSource := resolveCloudflareZoneID(alias, account, parsedEnv, strings.TrimSpace(input.ZoneIDFlag))
	if zoneID == "" {
		if value := strings.TrimSpace(os.Getenv("CLOUDFLARE_ZONE_ID")); value != "" {
			zoneID = value
			zoneSource = "env:CLOUDFLARE_ZONE_ID"
		}
	}

	source := strings.Join(nonEmpty(tokenSource, accountSource, zoneSource), ",")
	return cloudflareRuntimeContext{
		AccountAlias: alias,
		AccountID:    accountID,
		Environment:  parsedEnv,
		ZoneID:       zoneID,
		ZoneName:     zoneName,
		APIToken:     token,
		Source:       source,
		BaseURL:      baseURL,
	}, nil
}

func resolveCloudflareAccountSelection(settings Settings, accountFlag string) (string, CloudflareAccountEntry) {
	selected := strings.TrimSpace(accountFlag)
	if selected == "" {
		selected = strings.TrimSpace(settings.Cloudflare.DefaultAccount)
	}
	if selected == "" {
		selected = strings.TrimSpace(os.Getenv("CLOUDFLARE_DEFAULT_ACCOUNT"))
	}
	if selected == "" {
		aliases := cloudflareAccountAliases(settings)
		if len(aliases) == 1 {
			selected = aliases[0]
		}
	}
	if selected == "" {
		return "", CloudflareAccountEntry{}
	}
	if entry, ok := settings.Cloudflare.Accounts[selected]; ok {
		return selected, entry
	}
	return selected, CloudflareAccountEntry{}
}

func resolveCloudflareAccountID(alias string, account CloudflareAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--account-id"
	}
	if value := strings.TrimSpace(account.AccountID); value != "" {
		return value, "settings.account_id"
	}
	if ref := strings.TrimSpace(account.AccountIDEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveCloudflareEnv(alias, account, "ACCOUNT_ID")); value != "" {
		return value, "env:" + cloudflareAccountEnvPrefix(alias, account) + "ACCOUNT_ID"
	}
	if value := strings.TrimSpace(os.Getenv("CLOUDFLARE_ACCOUNT_ID")); value != "" {
		return value, "env:CLOUDFLARE_ACCOUNT_ID"
	}
	return "", ""
}

func resolveCloudflareAPIToken(alias string, account CloudflareAccountEntry, override string) (string, string) {
	if override != "" {
		return override, "flag:--api-token"
	}
	if ref := strings.TrimSpace(account.APITokenEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveCloudflareEnv(alias, account, "API_TOKEN")); value != "" {
		return value, "env:" + cloudflareAccountEnvPrefix(alias, account) + "API_TOKEN"
	}
	if value := strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN")); value != "" {
		return value, "env:CLOUDFLARE_API_TOKEN"
	}
	return "", ""
}

func resolveCloudflareZoneID(alias string, account CloudflareAccountEntry, env string, override string) (string, string) {
	if override != "" {
		return override, "flag:--zone-id"
	}
	env = normalizeCloudflareEnvironment(env)
	if env != "" {
		switch env {
		case "prod":
			if value := strings.TrimSpace(account.ProdZoneID); value != "" {
				return value, "settings.prod_zone_id"
			}
			if value := strings.TrimSpace(resolveCloudflareEnv(alias, account, "PROD_ZONE_ID")); value != "" {
				return value, "env:" + cloudflareAccountEnvPrefix(alias, account) + "PROD_ZONE_ID"
			}
		case "staging":
			if value := strings.TrimSpace(account.StagingZoneID); value != "" {
				return value, "settings.staging_zone_id"
			}
			if value := strings.TrimSpace(resolveCloudflareEnv(alias, account, "STAGING_ZONE_ID")); value != "" {
				return value, "env:" + cloudflareAccountEnvPrefix(alias, account) + "STAGING_ZONE_ID"
			}
		case "dev":
			if value := strings.TrimSpace(account.DevZoneID); value != "" {
				return value, "settings.dev_zone_id"
			}
			if value := strings.TrimSpace(resolveCloudflareEnv(alias, account, "DEV_ZONE_ID")); value != "" {
				return value, "env:" + cloudflareAccountEnvPrefix(alias, account) + "DEV_ZONE_ID"
			}
		}
	}
	if value := strings.TrimSpace(account.DefaultZoneID); value != "" {
		return value, "settings.default_zone_id"
	}
	if value := strings.TrimSpace(resolveCloudflareEnv(alias, account, "DEFAULT_ZONE_ID")); value != "" {
		return value, "env:" + cloudflareAccountEnvPrefix(alias, account) + "DEFAULT_ZONE_ID"
	}
	return "", ""
}

func cmdCloudflareAuth(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare auth status|status [--account <alias>] [--env <prod|staging|dev>] [--zone-id <zone>] [--json]")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "status":
		cmdCloudflareAuthStatus(args[1:])
	default:
		printUnknown("cloudflare auth", args[0])
	}
}

func cmdCloudflareAuthStatus(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("cloudflare auth status", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	zoneID := fs.String("zone-id", "", "zone id")
	zone := fs.String("zone", "", "zone name")
	apiToken := fs.String("api-token", "", "override cloudflare api token")
	baseURL := fs.String("base-url", "", "cloudflare api base url")
	accountID := fs.String("account-id", "", "cloudflare account id")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si cloudflare auth status|status [--account <alias>] [--env <prod|staging|dev>] [--zone-id <zone>] [--json]")
		return
	}
	runtime, err := resolveCloudflareRuntimeContext(cloudflareRuntimeContextInput{
		AccountFlag:   *account,
		EnvFlag:       *env,
		ZoneFlag:      *zone,
		ZoneIDFlag:    *zoneID,
		TokenFlag:     *apiToken,
		BaseURLFlag:   *baseURL,
		AccountIDFlag: *accountID,
	})
	if err != nil {
		fatal(err)
	}
	client, err := buildCloudflareClient(runtime)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	verifyResp, verifyErr := client.Do(ctx, cloudflarebridge.Request{Method: "GET", Path: "/user/tokens/verify"})
	status := "error"
	if verifyErr == nil {
		status = "ready"
	}
	payload := map[string]any{
		"status":        status,
		"account_alias": runtime.AccountAlias,
		"account_id":    runtime.AccountID,
		"environment":   runtime.Environment,
		"zone_id":       runtime.ZoneID,
		"zone_name":     runtime.ZoneName,
		"source":        runtime.Source,
		"token_preview": previewCloudflareSecret(runtime.APIToken),
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
		fmt.Printf("%s %s\n", styleHeading("Cloudflare auth:"), styleError("error"))
		fmt.Printf("%s %s\n", styleHeading("Context:"), formatCloudflareContext(runtime))
		printCloudflareError(verifyErr)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Cloudflare auth:"), styleSuccess("ready"))
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatCloudflareContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
	fmt.Printf("%s %s\n", styleHeading("Token preview:"), previewCloudflareSecret(runtime.APIToken))
}

func cmdCloudflareContext(args []string) {
	if len(args) == 0 {
		printUsage("usage: si cloudflare context <list|current|use>")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdCloudflareContextList(args[1:])
	case "current":
		cmdCloudflareContextCurrent(args[1:])
	case "use":
		cmdCloudflareContextUse(args[1:])
	default:
		printUnknown("cloudflare context", args[0])
	}
}

func cmdCloudflareContextList(args []string) {
	fs := flag.NewFlagSet("cloudflare context list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si cloudflare context list [--json]")
		return
	}
	settings := loadSettingsOrDefault()
	aliases := cloudflareAccountAliases(settings)
	rows := make([]map[string]string, 0, len(aliases))
	for _, alias := range aliases {
		entry := settings.Cloudflare.Accounts[alias]
		rows = append(rows, map[string]string{
			"alias":        alias,
			"name":         strings.TrimSpace(entry.Name),
			"account_id":   strings.TrimSpace(entry.AccountID),
			"default":      boolString(alias == strings.TrimSpace(settings.Cloudflare.DefaultAccount)),
			"prod_zone":    orDash(strings.TrimSpace(entry.ProdZoneID)),
			"staging_zone": orDash(strings.TrimSpace(entry.StagingZoneID)),
			"dev_zone":     orDash(strings.TrimSpace(entry.DevZoneID)),
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
		infof("no cloudflare accounts configured in settings")
		return
	}
	fmt.Printf("%s %s %s %s %s %s %s\n",
		padRightANSI(styleHeading("ALIAS"), 18),
		padRightANSI(styleHeading("DEFAULT"), 8),
		padRightANSI(styleHeading("ACCOUNT"), 24),
		padRightANSI(styleHeading("PROD"), 16),
		padRightANSI(styleHeading("STAGING"), 16),
		padRightANSI(styleHeading("DEV"), 16),
		styleHeading("NAME"),
	)
	sort.Slice(rows, func(i, j int) bool { return rows[i]["alias"] < rows[j]["alias"] })
	for _, row := range rows {
		fmt.Printf("%s %s %s %s %s %s %s\n",
			padRightANSI(orDash(row["alias"]), 18),
			padRightANSI(orDash(row["default"]), 8),
			padRightANSI(orDash(row["account_id"]), 24),
			padRightANSI(orDash(row["prod_zone"]), 16),
			padRightANSI(orDash(row["staging_zone"]), 16),
			padRightANSI(orDash(row["dev_zone"]), 16),
			orDash(row["name"]),
		)
	}
}

func cmdCloudflareContextCurrent(args []string) {
	fs := flag.NewFlagSet("cloudflare context current", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si cloudflare context current [--json]")
		return
	}
	runtime, err := resolveCloudflareRuntimeContext(cloudflareRuntimeContextInput{})
	if err != nil {
		fatal(err)
	}
	payload := map[string]any{
		"account_alias": runtime.AccountAlias,
		"account_id":    runtime.AccountID,
		"environment":   runtime.Environment,
		"zone_id":       runtime.ZoneID,
		"zone_name":     runtime.ZoneName,
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
	fmt.Printf("%s %s\n", styleHeading("Current cloudflare context:"), formatCloudflareContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
}

func cmdCloudflareContextUse(args []string) {
	fs := flag.NewFlagSet("cloudflare context use", flag.ExitOnError)
	account := fs.String("account", "", "default account alias")
	env := fs.String("env", "", "default environment (prod|staging|dev)")
	zoneID := fs.String("zone-id", "", "default zone id")
	baseURL := fs.String("base-url", "", "default cloudflare api base url")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si cloudflare context use [--account <alias>] [--env <prod|staging|dev>] [--zone-id <zone>] [--base-url <url>]")
		return
	}
	settings := loadSettingsOrDefault()
	if value := strings.TrimSpace(*account); value != "" {
		settings.Cloudflare.DefaultAccount = value
	}
	if value := strings.TrimSpace(*env); value != "" {
		parsed, err := parseCloudflareEnvironment(value)
		if err != nil {
			fatal(err)
		}
		settings.Cloudflare.DefaultEnv = parsed
	}
	if value := strings.TrimSpace(*baseURL); value != "" {
		settings.Cloudflare.APIBaseURL = value
	}
	if alias := strings.TrimSpace(*account); alias != "" && strings.TrimSpace(*zoneID) != "" {
		if settings.Cloudflare.Accounts == nil {
			settings.Cloudflare.Accounts = map[string]CloudflareAccountEntry{}
		}
		entry := settings.Cloudflare.Accounts[alias]
		envName := normalizeCloudflareEnvironment(settings.Cloudflare.DefaultEnv)
		if envName == "" {
			envName = "prod"
		}
		switch envName {
		case "prod":
			entry.ProdZoneID = strings.TrimSpace(*zoneID)
		case "staging":
			entry.StagingZoneID = strings.TrimSpace(*zoneID)
		case "dev":
			entry.DevZoneID = strings.TrimSpace(*zoneID)
		}
		settings.Cloudflare.Accounts[alias] = entry
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	successf("cloudflare context updated")
}

func cmdCloudflareDoctor(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "public": true})
	fs := flag.NewFlagSet("cloudflare doctor", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	zoneID := fs.String("zone-id", "", "zone id")
	zone := fs.String("zone", "", "zone name")
	apiToken := fs.String("api-token", "", "override cloudflare api token")
	baseURL := fs.String("base-url", "", "cloudflare api base url")
	accountID := fs.String("account-id", "", "cloudflare account id")
	public := fs.Bool("public", false, "run unauthenticated provider public probe")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si cloudflare doctor [--account <alias>] [--env <prod|staging|dev>] [--public] [--json]")
		return
	}
	if *public {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err := runPublicProviderDoctor(ctx, providers.Cloudflare, *baseURL)
		if err != nil {
			fatal(err)
		}
		printPublicDoctorResult("Cloudflare", result, *jsonOut)
		return
	}
	runtime, err := resolveCloudflareRuntimeContext(cloudflareRuntimeContextInput{
		AccountFlag:   *account,
		EnvFlag:       *env,
		ZoneFlag:      *zone,
		ZoneIDFlag:    *zoneID,
		TokenFlag:     *apiToken,
		BaseURLFlag:   *baseURL,
		AccountIDFlag: *accountID,
	})
	if err != nil {
		fatal(err)
	}
	client, err := buildCloudflareClient(runtime)
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
	checks := make([]check, 0, 5)
	addCheck := func(name string, ok bool, detail string) {
		checks = append(checks, check{Name: name, OK: ok, Detail: detail})
	}

	verifyResp, verifyErr := client.Do(ctx, cloudflarebridge.Request{Method: "GET", Path: "/user/tokens/verify"})
	if verifyErr != nil {
		addCheck("token.verify", false, verifyErr.Error())
	} else {
		addCheck("token.verify", true, summarizeCloudflareResponse(verifyResp))
	}
	if strings.TrimSpace(runtime.AccountID) != "" {
		accountResp, accountErr := client.Do(ctx, cloudflarebridge.Request{Method: "GET", Path: "/accounts/" + runtime.AccountID})
		if accountErr != nil {
			addCheck("account.read", false, accountErr.Error())
		} else {
			addCheck("account.read", true, summarizeCloudflareResponse(accountResp))
		}
	}
	if strings.TrimSpace(runtime.ZoneID) != "" {
		zoneResp, zoneErr := client.Do(ctx, cloudflarebridge.Request{Method: "GET", Path: "/zones/" + runtime.ZoneID})
		if zoneErr != nil {
			addCheck("zone.read", false, zoneErr.Error())
		} else {
			addCheck("zone.read", true, summarizeCloudflareResponse(zoneResp))
		}
	}
	ok := true
	for _, entry := range checks {
		ok = ok && entry.OK
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
		fmt.Printf("%s %s\n", styleHeading("Cloudflare doctor:"), styleSuccess("ok"))
	} else {
		fmt.Printf("%s %s\n", styleHeading("Cloudflare doctor:"), styleError("issues found"))
	}
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatCloudflareContext(runtime))
	for _, entry := range checks {
		icon := styleSuccess("OK")
		if !entry.OK {
			icon = styleError("ERR")
		}
		fmt.Printf("  %s %s %s\n", padRightANSI(icon, 4), padRightANSI(entry.Name, 16), strings.TrimSpace(entry.Detail))
	}
	if !ok {
		os.Exit(1)
	}
}

func summarizeCloudflareResponse(resp cloudflarebridge.Response) string {
	if len(resp.List) > 0 {
		return fmt.Sprintf("%d item(s)", len(resp.List))
	}
	if len(resp.Data) == 0 {
		return "ok"
	}
	for _, key := range []string{"id", "name", "status", "value"} {
		if value, ok := resp.Data[key]; ok {
			return fmt.Sprintf("%s=%s", key, stringifyCloudflareAny(value))
		}
	}
	return "ok"
}

func previewCloudflareSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "-"
	}
	secret = cloudflarebridge.RedactSensitive(secret)
	if len(secret) <= 10 {
		return secret
	}
	return secret[:8] + "..."
}
