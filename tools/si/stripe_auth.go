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

	"si/tools/si/internal/providers"
	"si/tools/si/internal/stripebridge"
)

func resolveStripeRuntimeContext(accountFlag string, envFlag string, apiKeyFlag string) (stripeRuntimeContext, error) {
	settings := loadSettingsOrDefault()
	accountFlag = strings.TrimSpace(accountFlag)
	envFlag = strings.TrimSpace(envFlag)
	apiKeyFlag = strings.TrimSpace(apiKeyFlag)

	alias, accountSetting, accountID := resolveStripeAccountSelection(settings, accountFlag)

	env := envFlag
	if env == "" {
		env = strings.TrimSpace(settings.Stripe.DefaultEnv)
	}
	if env == "" {
		env = strings.TrimSpace(os.Getenv("SI_STRIPE_ENV"))
	}
	if env == "" {
		env = "sandbox"
	}
	parsedEnv, err := parseStripeEnvironment(env)
	if err != nil {
		return stripeRuntimeContext{}, err
	}

	key := apiKeyFlag
	source := "flag"
	if key == "" {
		key, source = resolveStripeAPIKey(accountSetting, parsedEnv)
	}
	if strings.TrimSpace(key) == "" {
		return stripeRuntimeContext{}, fmt.Errorf("stripe api key not found for env=%s (set --api-key, [stripe.accounts.<alias>] key, or SI_STRIPE_API_KEY)", parsedEnv)
	}
	return stripeRuntimeContext{
		AccountAlias: alias,
		AccountID:    accountID,
		Environment:  parsedEnv,
		APIKey:       strings.TrimSpace(key),
		Source:       source,
		BaseURL:      strings.TrimSpace(os.Getenv("SI_STRIPE_API_BASE_URL")),
	}, nil
}

func resolveStripeAccountSelection(settings Settings, accountFlag string) (alias string, account StripeAccountSetting, accountID string) {
	selected := strings.TrimSpace(accountFlag)
	if selected == "" {
		selected = strings.TrimSpace(settings.Stripe.DefaultAccount)
	}
	if selected == "" {
		selected = strings.TrimSpace(os.Getenv("SI_STRIPE_ACCOUNT"))
	}
	if selected == "" {
		aliases := stripeAccountAliases(settings)
		if len(aliases) == 1 {
			selected = aliases[0]
		}
	}
	if selected == "" {
		return "", StripeAccountSetting{}, ""
	}
	if item, ok := settings.Stripe.Accounts[selected]; ok {
		return selected, item, strings.TrimSpace(item.ID)
	}
	if strings.HasPrefix(strings.ToLower(selected), "acct_") {
		return "", StripeAccountSetting{}, selected
	}
	return selected, StripeAccountSetting{}, ""
}

func resolveStripeAPIKey(account StripeAccountSetting, env stripebridge.Environment) (value string, source string) {
	switch env {
	case stripebridge.EnvLive:
		if strings.TrimSpace(account.LiveKey) != "" {
			return strings.TrimSpace(account.LiveKey), "settings.live_key"
		}
		if ref := strings.TrimSpace(account.LiveKeyEnv); ref != "" {
			if val := strings.TrimSpace(os.Getenv(ref)); val != "" {
				return val, "env:" + ref
			}
		}
		if val := strings.TrimSpace(os.Getenv("SI_STRIPE_LIVE_API_KEY")); val != "" {
			return val, "env:SI_STRIPE_LIVE_API_KEY"
		}
	case stripebridge.EnvSandbox:
		if strings.TrimSpace(account.SandboxKey) != "" {
			return strings.TrimSpace(account.SandboxKey), "settings.sandbox_key"
		}
		if ref := strings.TrimSpace(account.SandboxKeyEnv); ref != "" {
			if val := strings.TrimSpace(os.Getenv(ref)); val != "" {
				return val, "env:" + ref
			}
		}
		if val := strings.TrimSpace(os.Getenv("SI_STRIPE_SANDBOX_API_KEY")); val != "" {
			return val, "env:SI_STRIPE_SANDBOX_API_KEY"
		}
	}
	if val := strings.TrimSpace(os.Getenv("SI_STRIPE_API_KEY")); val != "" {
		return val, "env:SI_STRIPE_API_KEY"
	}
	return "", ""
}

func cmdStripeAuth(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si stripe auth status [--account <alias|acct_id>] [--env <live|sandbox>] [--api-key <key>] [--json]")
	if !routedOK {
		return
	}
	args = routedArgs
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "status":
		cmdStripeAuthStatus(args[1:])
	default:
		printUnknown("stripe auth", args[0])
	}
}

func cmdStripeAuthStatus(args []string) {
	fs := flag.NewFlagSet("stripe auth status", flag.ExitOnError)
	account := fs.String("account", "", "account alias or acct_ id")
	env := fs.String("env", "", "environment (live|sandbox)")
	apiKey := fs.String("api-key", "", "override api key")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si stripe auth status [--account <alias|acct_id>] [--env <live|sandbox>] [--api-key <key>] [--json]")
		return
	}
	runtime, err := resolveStripeRuntimeContext(*account, *env, *apiKey)
	if err != nil {
		fatal(err)
	}
	payload := map[string]any{
		"account_alias": runtime.AccountAlias,
		"account_id":    runtime.AccountID,
		"environment":   runtime.Environment,
		"key_source":    runtime.Source,
		"key_preview":   previewSecret(runtime.APIKey),
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Stripe auth:"), styleSuccess("ready"))
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatStripeContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Key source:"), runtime.Source)
	fmt.Printf("%s %s\n", styleHeading("Key preview:"), previewSecret(runtime.APIKey))
}

func cmdStripeDoctor(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "public": true})
	fs := flag.NewFlagSet("stripe doctor", flag.ExitOnError)
	account := fs.String("account", "", "account alias or acct_ id")
	env := fs.String("env", "", "environment (live|sandbox)")
	apiKey := fs.String("api-key", "", "override api key")
	baseURL := fs.String("base-url", "", "stripe api base url")
	public := fs.Bool("public", false, "run unauthenticated provider public probe")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si stripe doctor [--account <alias|acct_id>] [--env <live|sandbox>] [--public] [--json]")
		return
	}
	if *public {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err := runPublicProviderDoctor(ctx, providers.Stripe, *baseURL)
		if err != nil {
			fatal(err)
		}
		printPublicDoctorResult("Stripe", result, *jsonOut)
		return
	}
	runtime, err := resolveStripeRuntimeContext(*account, *env, *apiKey)
	if err != nil {
		fatal(err)
	}
	if value := strings.TrimSpace(*baseURL); value != "" {
		runtime.BaseURL = value
	}
	client, err := buildStripeClient(runtime)
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
	resp, err := client.Do(ctx, stripebridge.Request{Method: "GET", Path: "/v1/balance"})
	if err != nil {
		checks = append(checks, check{Name: "balance.read", OK: false, Detail: err.Error()})
	} else {
		checks = append(checks, check{Name: "balance.read", OK: true, Detail: summarizeStripeResponse(resp)})
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
		fmt.Printf("%s %s\n", styleHeading("Stripe doctor:"), styleSuccess("ok"))
	} else {
		fmt.Printf("%s %s\n", styleHeading("Stripe doctor:"), styleError("issues found"))
	}
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatStripeContext(runtime))
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

func cmdStripeContext(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si stripe context <list|current|use>")
	if !routedOK {
		return
	}
	args = routedArgs
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdStripeContextList(args[1:])
	case "current":
		cmdStripeContextCurrent(args[1:])
	case "use":
		cmdStripeContextUse(args[1:])
	default:
		printUnknown("stripe context", args[0])
	}
}

func cmdStripeContextList(args []string) {
	fs := flag.NewFlagSet("stripe context list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si stripe context list [--json]")
		return
	}
	settings := loadSettingsOrDefault()
	aliases := stripeAccountAliases(settings)
	rows := make([]map[string]string, 0, len(aliases))
	for _, alias := range aliases {
		account := settings.Stripe.Accounts[alias]
		rows = append(rows, map[string]string{
			"alias":              alias,
			"id":                 strings.TrimSpace(account.ID),
			"name":               strings.TrimSpace(account.Name),
			"default":            boolYesNo(alias == strings.TrimSpace(settings.Stripe.DefaultAccount)),
			"live_key_config":    boolYesNo(hasStripeEnvKey(account.LiveKey, account.LiveKeyEnv)),
			"sandbox_key_config": boolYesNo(hasStripeEnvKey(account.SandboxKey, account.SandboxKeyEnv)),
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
		infof("no stripe accounts configured in settings")
		return
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i]["alias"] < rows[j]["alias"] })
	headers := []string{
		styleHeading("ALIAS"),
		styleHeading("ACCOUNT"),
		styleHeading("DEFAULT"),
		styleHeading("LIVE"),
		styleHeading("SANDBOX"),
		styleHeading("NAME"),
	}
	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		tableRows = append(tableRows, []string{
			row["alias"],
			orDash(row["id"]),
			row["default"],
			row["live_key_config"],
			row["sandbox_key_config"],
			orDash(row["name"]),
		})
	}
	printAlignedTable(headers, tableRows, 2)
}

func cmdStripeContextCurrent(args []string) {
	fs := flag.NewFlagSet("stripe context current", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si stripe context current [--json]")
		return
	}
	runtime, err := resolveStripeRuntimeContext("", "", "")
	if err != nil {
		fatal(err)
	}
	out := map[string]any{
		"account_alias": runtime.AccountAlias,
		"account_id":    runtime.AccountID,
		"environment":   runtime.Environment,
		"key_source":    runtime.Source,
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Current stripe context:"), formatStripeContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Key source:"), runtime.Source)
}

func cmdStripeContextUse(args []string) {
	fs := flag.NewFlagSet("stripe context use", flag.ExitOnError)
	account := fs.String("account", "", "account alias or acct_ id")
	env := fs.String("env", "", "environment (live|sandbox)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si stripe context use --account <alias|acct_id> --env <live|sandbox>")
		return
	}
	if strings.TrimSpace(*account) == "" || strings.TrimSpace(*env) == "" {
		printUsage("usage: si stripe context use --account <alias|acct_id> --env <live|sandbox>")
		return
	}
	parsedEnv, err := parseStripeEnvironment(*env)
	if err != nil {
		fatal(err)
	}
	settings := loadSettingsOrDefault()
	selected := strings.TrimSpace(*account)
	if _, ok := settings.Stripe.Accounts[selected]; !ok {
		if !strings.HasPrefix(strings.ToLower(selected), "acct_") {
			aliases := stripeAccountAliases(settings)
			fatal(fmt.Errorf("unknown stripe account alias %q (available: %s)", selected, strings.Join(aliases, ", ")))
		}
	}
	settings.Stripe.DefaultAccount = selected
	settings.Stripe.DefaultEnv = string(parsedEnv)
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	successf("stripe context set: account=%s env=%s", selected, parsedEnv)
}

func boolYesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func hasStripeEnvKey(inline string, envRef string) bool {
	if strings.TrimSpace(inline) != "" {
		return true
	}
	if ref := strings.TrimSpace(envRef); ref != "" && strings.TrimSpace(os.Getenv(ref)) != "" {
		return true
	}
	return false
}

func previewSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "-"
	}
	secret = stripebridge.RedactSensitive(secret)
	if len(secret) <= 10 {
		return secret
	}
	return secret[:8] + "..."
}

func summarizeStripeResponse(resp stripebridge.Response) string {
	if len(resp.Data) == 0 {
		return firstNonEmpty(resp.Status, "ok")
	}
	for _, key := range []string{"object", "id", "livemode", "url"} {
		if value, ok := resp.Data[key]; ok {
			return fmt.Sprintf("%s=%s", key, stringifyStripeAny(value))
		}
	}
	return "ok"
}

func stringifyStripeAny(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	}
}

func orDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}
