package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

func cmdSocialPlatformAuth(platform socialPlatform, args []string) {
	if len(args) == 0 {
		printUsage(fmt.Sprintf("usage: si social %s auth status [--account <alias>] [--env <prod|staging|dev>] [--json]", socialPlatformLabel(platform)))
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "status":
		cmdSocialPlatformAuthStatus(platform, args[1:])
	default:
		printUnknown("social "+socialPlatformLabel(platform)+" auth", args[0])
	}
}

func cmdSocialPlatformAuthStatus(platform socialPlatform, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("social auth status", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	authStyle := fs.String("auth-style", "", "auth style (bearer|query|none)")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(fmt.Sprintf("usage: si social %s auth status [--account <alias>] [--env <prod|staging|dev>] [--json]", socialPlatformLabel(platform)))
		return
	}

	runtime, err := resolveSocialRuntimeContext(socialRuntimeContextInput{
		Platform:    platform,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		AuthFlag:    *authStyle,
		TokenFlag:   *token,
	})
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	resp, verifyErr := socialDo(ctx, runtime, socialRequest{
		Method: "GET",
		Path:   socialPlatformDefaultProbePath(platform, runtime.AuthStyle),
		Params: socialPlatformDefaultProbeParams(platform, runtime.AuthStyle),
	})
	status := "error"
	if verifyErr == nil {
		status = "ready"
	}
	payload := map[string]any{
		"status":        status,
		"platform":      socialPlatformLabel(platform),
		"account_alias": runtime.AccountAlias,
		"environment":   runtime.Environment,
		"base_url":      runtime.BaseURL,
		"api_version":   runtime.APIVersion,
		"auth_style":    runtime.AuthStyle,
		"source":        runtime.Source,
		"token_preview": previewSocialSecret(runtime.Token),
	}
	if verifyErr == nil {
		payload["verify"] = resp.Data
		payload["verify_status"] = resp.StatusCode
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
		fmt.Printf("%s %s\n", styleHeading(strings.Title(socialPlatformLabel(platform))+" auth:"), styleError("error"))
		fmt.Printf("%s %s\n", styleHeading("Context:"), formatSocialContext(runtime))
		printSocialError(verifyErr)
		return
	}
	fmt.Printf("%s %s\n", styleHeading(strings.Title(socialPlatformLabel(platform))+" auth:"), styleSuccess("ready"))
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatSocialContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
	fmt.Printf("%s %s\n", styleHeading("Token preview:"), previewSocialSecret(runtime.Token))
}

func cmdSocialPlatformContext(platform socialPlatform, args []string) {
	if len(args) == 0 {
		printUsage(fmt.Sprintf("usage: si social %s context <list|current|use>", socialPlatformLabel(platform)))
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdSocialPlatformContextList(platform, args[1:])
	case "current":
		cmdSocialPlatformContextCurrent(platform, args[1:])
	case "use":
		cmdSocialPlatformContextUse(platform, args[1:])
	default:
		printUnknown("social "+socialPlatformLabel(platform)+" context", args[0])
	}
}

func cmdSocialPlatformContextList(platform socialPlatform, args []string) {
	fs := flag.NewFlagSet("social context list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(fmt.Sprintf("usage: si social %s context list [--json]", socialPlatformLabel(platform)))
		return
	}
	settings := loadSettingsOrDefault()
	aliases := socialAccountAliases(settings)
	rows := make([]map[string]string, 0, len(aliases))
	for _, alias := range aliases {
		entry := settings.Social.Accounts[alias]
		rows = append(rows, map[string]string{
			"alias":      alias,
			"name":       strings.TrimSpace(entry.Name),
			"default":    boolString(alias == strings.TrimSpace(settings.Social.DefaultAccount)),
			"token_env":  socialAccountTokenEnvRef(platform, entry),
			"default_id": socialAccountDefaultID(platform, entry),
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
		infof("no social accounts configured in settings")
		return
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i]["alias"] < rows[j]["alias"] })
	fmt.Printf("%s %s %s %s %s\n",
		padRightANSI(styleHeading("ALIAS"), 18),
		padRightANSI(styleHeading("DEFAULT"), 8),
		padRightANSI(styleHeading("TOKEN ENV"), 34),
		padRightANSI(styleHeading("DEFAULT ID"), 28),
		styleHeading("NAME"),
	)
	for _, row := range rows {
		fmt.Printf("%s %s %s %s %s\n",
			padRightANSI(orDash(row["alias"]), 18),
			padRightANSI(orDash(row["default"]), 8),
			padRightANSI(orDash(row["token_env"]), 34),
			padRightANSI(orDash(row["default_id"]), 28),
			orDash(row["name"]),
		)
	}
}

func cmdSocialPlatformContextCurrent(platform socialPlatform, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("social context current", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	authStyle := fs.String("auth-style", "", "auth style (bearer|query|none)")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(fmt.Sprintf("usage: si social %s context current [--json]", socialPlatformLabel(platform)))
		return
	}
	runtime, err := resolveSocialRuntimeContext(socialRuntimeContextInput{
		Platform:    platform,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		AuthFlag:    *authStyle,
		TokenFlag:   *token,
	})
	if err != nil {
		fatal(err)
	}
	payload := map[string]any{
		"platform":      socialPlatformLabel(platform),
		"account_alias": runtime.AccountAlias,
		"environment":   runtime.Environment,
		"base_url":      runtime.BaseURL,
		"api_version":   runtime.APIVersion,
		"auth_style":    runtime.AuthStyle,
		"default_id":    socialPlatformContextID(platform, runtime),
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
	fmt.Printf("%s %s\n", styleHeading("Current social context:"), formatSocialContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Default ID:"), orDash(socialPlatformContextID(platform, runtime)))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
}

func cmdSocialPlatformContextUse(platform socialPlatform, args []string) {
	fs := flag.NewFlagSet("social context use", flag.ExitOnError)
	account := fs.String("account", "", "default account alias")
	env := fs.String("env", "", "default environment (prod|staging|dev)")
	baseURL := fs.String("base-url", "", "platform api base url")
	apiVersion := fs.String("api-version", "", "platform api version")
	authStyle := fs.String("auth-style", "", "platform auth style (bearer|query|none)")
	tokenEnv := fs.String("token-env", "", "platform token env-var reference for selected account")
	defaultID := fs.String("default-id", "", "default entity id/urn/username for selected platform")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(fmt.Sprintf("usage: si social %s context use [--account <alias>] [--env <prod|staging|dev>] [--base-url <url>] [--api-version <ver>] [--auth-style <bearer|query|none>] [--token-env <env_key>] [--default-id <id>]", socialPlatformLabel(platform)))
		return
	}
	settings := loadSettingsOrDefault()
	if value := strings.TrimSpace(*account); value != "" {
		settings.Social.DefaultAccount = value
	}
	if value := strings.TrimSpace(*env); value != "" {
		parsed, err := parseSocialEnvironment(value)
		if err != nil {
			fatal(err)
		}
		settings.Social.DefaultEnv = parsed
	}
	cfg := socialPlatformSettings(settings, platform)
	if value := strings.TrimSpace(*baseURL); value != "" {
		cfg.APIBaseURL = value
	}
	if value := strings.TrimSpace(*apiVersion); value != "" {
		cfg.APIVersion = value
	}
	if value := strings.TrimSpace(*authStyle); value != "" {
		normalized := normalizeSocialAuthStyle(value)
		if normalized == "" {
			fatal(fmt.Errorf("invalid --auth-style %q (expected bearer|query|none)", value))
		}
		cfg.AuthStyle = normalized
	}
	applySocialPlatformSettings(&settings, platform, cfg)

	alias := strings.TrimSpace(*account)
	if alias != "" {
		if settings.Social.Accounts == nil {
			settings.Social.Accounts = map[string]SocialAccountSetting{}
		}
		entry := settings.Social.Accounts[alias]
		if value := strings.TrimSpace(*tokenEnv); value != "" {
			setSocialAccountTokenEnv(platform, &entry, value)
		}
		if value := strings.TrimSpace(*defaultID); value != "" {
			setSocialAccountDefaultID(platform, &entry, value)
		}
		settings.Social.Accounts[alias] = entry
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	successf("%s context updated", socialPlatformLabel(platform))
}

func cmdSocialPlatformDoctor(platform socialPlatform, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("social doctor", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	authStyle := fs.String("auth-style", "", "auth style (bearer|query|none)")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(fmt.Sprintf("usage: si social %s doctor [--account <alias>] [--json]", socialPlatformLabel(platform)))
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    platform,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		AuthFlag:    *authStyle,
		TokenFlag:   *token,
	})
	type check struct {
		Name   string `json:"name"`
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	checks := make([]check, 0, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	probeResp, probeErr := socialDo(ctx, runtime, socialRequest{
		Method: "GET",
		Path:   socialPlatformDefaultProbePath(platform, runtime.AuthStyle),
		Params: socialPlatformDefaultProbeParams(platform, runtime.AuthStyle),
	})
	if probeErr != nil {
		if runtime.AuthStyle == "none" && socialProbeReachableWithoutAuth(probeErr) {
			checks = append(checks, check{Name: "auth-probe", OK: true, Detail: probeErr.Error()})
		} else {
			checks = append(checks, check{Name: "auth-probe", OK: false, Detail: probeErr.Error()})
		}
	} else {
		checks = append(checks, check{Name: "auth-probe", OK: true, Detail: fmt.Sprintf("status=%d", probeResp.StatusCode)})
	}
	checks = append(checks, check{Name: "log-path", OK: strings.TrimSpace(runtime.LogPath) != "", Detail: orDash(runtime.LogPath)})
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
		fmt.Printf("%s %s\n", styleHeading(strings.Title(socialPlatformLabel(platform))+" doctor:"), styleSuccess("ok"))
	} else {
		fmt.Printf("%s %s\n", styleHeading(strings.Title(socialPlatformLabel(platform))+" doctor:"), styleError("issues found"))
	}
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatSocialContext(runtime))
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

func socialProbeReachableWithoutAuth(err error) bool {
	if err == nil {
		return true
	}
	var apiErr *socialAPIErrorDetails
	if !errors.As(err, &apiErr) || apiErr == nil {
		return false
	}
	if apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
		return true
	}
	return false
}

func cmdSocialPlatformRaw(platform socialPlatform, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social raw", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	authStyle := fs.String("auth-style", "", "auth style (bearer|query|none)")
	method := fs.String("method", "GET", "http method")
	path := fs.String("path", "", "api path")
	body := fs.String("body", "", "raw request body")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query/body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*path) == "" {
		printUsage(fmt.Sprintf("usage: si social %s raw --method <GET|POST|PATCH|PUT|DELETE> --path <api-path> [--param key=value] [--body raw] [--json]", socialPlatformLabel(platform)))
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    platform,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		AuthFlag:    *authStyle,
		TokenFlag:   *token,
	})
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	req := socialRequest{
		Method: strings.ToUpper(strings.TrimSpace(*method)),
		Path:   strings.TrimSpace(*path),
		Params: parseSocialParams(params),
	}
	if strings.TrimSpace(*body) != "" {
		req.RawBody = strings.TrimSpace(*body)
	}
	resp, err := socialDo(ctx, runtime, req)
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialPlatformReport(platform socialPlatform, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("social report", flag.ExitOnError)
	account := fs.String("account", "", "filter account alias")
	env := fs.String("env", "", "filter environment (prod|staging|dev)")
	sinceRaw := fs.String("since", "", "start timestamp (unix seconds or RFC3339)")
	untilRaw := fs.String("until", "", "end timestamp (unix seconds or RFC3339)")
	limit := fs.Int("limit", 20, "max error rows for `errors` report")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage(fmt.Sprintf("usage: si social %s report <usage|errors> [--since <time>] [--until <time>] [--json]", socialPlatformLabel(platform)))
		return
	}
	since, err := parseReportTime(*sinceRaw)
	if err != nil {
		fatal(fmt.Errorf("invalid --since: %w", err))
	}
	until, err := parseReportTime(*untilRaw)
	if err != nil {
		fatal(fmt.Errorf("invalid --until: %w", err))
	}
	if since != nil && until != nil && since.After(*until) {
		fatal(fmt.Errorf("--since must be <= --until"))
	}
	settings := loadSettingsOrDefault()
	logPath := resolveSocialLogPath(settings, platform)
	events, err := loadSocialLogEvents(logPath)
	if err != nil {
		fatal(err)
	}
	filtered := make([]map[string]any, 0, len(events))
	for _, event := range events {
		if strings.TrimSpace(stringifySocialAny(event["platform"])) != socialPlatformLabel(platform) {
			continue
		}
		if value := strings.TrimSpace(*account); value != "" {
			if strings.TrimSpace(stringifySocialAny(event["account"])) != value {
				continue
			}
		}
		if value := normalizeSocialEnvironment(*env); value != "" {
			if strings.TrimSpace(stringifySocialAny(event["environment"])) != value {
				continue
			}
		}
		if !socialEventInRange(event, since, until) {
			continue
		}
		filtered = append(filtered, event)
	}
	reportType := strings.ToLower(strings.TrimSpace(fs.Arg(0)))
	switch reportType {
	case "usage":
		cmdSocialReportUsage(platform, filtered, *jsonOut, logPath)
	case "errors":
		cmdSocialReportErrors(platform, filtered, *limit, *jsonOut, logPath)
	default:
		fatal(fmt.Errorf("unknown social report %q", reportType))
	}
}

func cmdSocialReportUsage(platform socialPlatform, events []map[string]any, jsonOut bool, logPath string) {
	var total int64
	var errorsCount int64
	durations := make([]int64, 0, len(events))
	byPath := map[string]map[string]int64{}
	for _, event := range events {
		if strings.TrimSpace(stringifySocialAny(event["event"])) != "response" {
			continue
		}
		total++
		path := strings.TrimSpace(stringifySocialAny(event["path"]))
		if path == "" {
			path = "-"
		}
		row := byPath[path]
		if row == nil {
			row = map[string]int64{"count": 0, "errors": 0}
			byPath[path] = row
		}
		row["count"]++
		status, _ := readSocialIntLike(event["status_code"])
		if status >= 400 || status == 0 {
			errorsCount++
			row["errors"]++
		}
		if ms, ok := readSocialIntLike(event["duration_ms"]); ok && ms >= 0 {
			durations = append(durations, ms)
		}
	}
	avg := int64(0)
	p95 := int64(0)
	if len(durations) > 0 {
		var sum int64
		for _, item := range durations {
			sum += item
		}
		avg = sum / int64(len(durations))
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		idx := int(float64(len(durations)-1) * 0.95)
		if idx < 0 {
			idx = 0
		}
		p95 = durations[idx]
	}
	payload := map[string]any{
		"platform":        socialPlatformLabel(platform),
		"log_path":        logPath,
		"total_requests":  total,
		"error_requests":  errorsCount,
		"avg_duration_ms": avg,
		"p95_duration_ms": p95,
		"by_path":         byPath,
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Social usage report:"), socialPlatformLabel(platform))
	fmt.Printf("%s %d\n", styleHeading("Requests:"), total)
	fmt.Printf("%s %d\n", styleHeading("Errors:"), errorsCount)
	fmt.Printf("%s %dms\n", styleHeading("Avg latency:"), avg)
	fmt.Printf("%s %dms\n", styleHeading("P95 latency:"), p95)
	if len(byPath) == 0 {
		return
	}
	fmt.Printf("%s\n", styleHeading("By path:"))
	keys := make([]string, 0, len(byPath))
	for key := range byPath {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, key := range keys {
		row := byPath[key]
		fmt.Printf("  %s count=%d errors=%d\n", key, row["count"], row["errors"])
	}
}

func cmdSocialReportErrors(platform socialPlatform, events []map[string]any, limit int, jsonOut bool, logPath string) {
	rows := make([]map[string]any, 0, len(events))
	for _, event := range events {
		switch strings.TrimSpace(stringifySocialAny(event["event"])) {
		case "error":
			rows = append(rows, event)
		case "response":
			status, _ := readSocialIntLike(event["status_code"])
			if status >= 400 {
				rows = append(rows, event)
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		return strings.TrimSpace(stringifySocialAny(rows[i]["ts"])) > strings.TrimSpace(stringifySocialAny(rows[j]["ts"]))
	})
	if limit <= 0 {
		limit = 20
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	payload := map[string]any{
		"platform": socialPlatformLabel(platform),
		"log_path": logPath,
		"count":    len(rows),
		"errors":   rows,
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Social error report:"), socialPlatformLabel(platform))
	fmt.Printf("%s %d\n", styleHeading("Rows:"), len(rows))
	for _, row := range rows {
		fmt.Printf("  %s %s %s %s\n",
			padRightANSI(orDash(stringifySocialAny(row["ts"])), 32),
			padRightANSI(orDash(stringifySocialAny(row["method"])), 8),
			padRightANSI(orDash(stringifySocialAny(row["path"])), 36),
			orDash(stringifySocialAny(row["error"])),
		)
	}
}

func socialEventInRange(event map[string]any, since *time.Time, until *time.Time) bool {
	tsRaw := strings.TrimSpace(stringifySocialAny(event["ts"]))
	if tsRaw == "" {
		return since == nil && until == nil
	}
	ts, err := time.Parse(time.RFC3339Nano, tsRaw)
	if err != nil {
		if alt, altErr := time.Parse(time.RFC3339, tsRaw); altErr == nil {
			ts = alt
		} else {
			return false
		}
	}
	ts = ts.UTC()
	if since != nil && ts.Before(*since) {
		return false
	}
	if until != nil && ts.After(*until) {
		return false
	}
	return true
}

func socialAccountDefaultID(platform socialPlatform, account SocialAccountSetting) string {
	switch platform {
	case socialPlatformFacebook:
		return strings.TrimSpace(account.FacebookPageID)
	case socialPlatformInstagram:
		return strings.TrimSpace(account.InstagramBusinessID)
	case socialPlatformX:
		if value := strings.TrimSpace(account.XUserID); value != "" {
			return value
		}
		return strings.TrimSpace(account.XUsername)
	case socialPlatformLinkedIn:
		if value := strings.TrimSpace(account.LinkedInPersonURN); value != "" {
			return value
		}
		return strings.TrimSpace(account.LinkedInOrganizationURN)
	default:
		return ""
	}
}

func setSocialAccountTokenEnv(platform socialPlatform, account *SocialAccountSetting, value string) {
	if account == nil {
		return
	}
	switch platform {
	case socialPlatformFacebook:
		account.FacebookAccessTokenEnv = value
	case socialPlatformInstagram:
		account.InstagramAccessTokenEnv = value
	case socialPlatformX:
		account.XAccessTokenEnv = value
	case socialPlatformLinkedIn:
		account.LinkedInAccessTokenEnv = value
	}
}

func setSocialAccountDefaultID(platform socialPlatform, account *SocialAccountSetting, value string) {
	if account == nil {
		return
	}
	switch platform {
	case socialPlatformFacebook:
		account.FacebookPageID = value
	case socialPlatformInstagram:
		account.InstagramBusinessID = value
	case socialPlatformX:
		if strings.HasPrefix(value, "@") {
			account.XUsername = strings.TrimPrefix(value, "@")
		} else if strings.Contains(value, " ") || strings.Contains(value, ":") {
			account.XUsername = value
		} else {
			account.XUserID = value
		}
	case socialPlatformLinkedIn:
		if strings.HasPrefix(value, "urn:li:person:") {
			account.LinkedInPersonURN = value
		} else if strings.HasPrefix(value, "urn:li:organization:") {
			account.LinkedInOrganizationURN = value
		} else {
			account.LinkedInPersonURN = value
		}
	}
}

func applySocialPlatformSettings(settings *Settings, platform socialPlatform, cfg SocialPlatformSettings) {
	if settings == nil {
		return
	}
	switch platform {
	case socialPlatformFacebook:
		settings.Social.Facebook = cfg
	case socialPlatformInstagram:
		settings.Social.Instagram = cfg
	case socialPlatformX:
		settings.Social.X = cfg
	case socialPlatformLinkedIn:
		settings.Social.LinkedIn = cfg
	}
}
