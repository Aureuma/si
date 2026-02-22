package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"time"

	"si/tools/si/internal/httpx"
	"si/tools/si/internal/integrationruntime"
	"si/tools/si/internal/netpolicy"
	"si/tools/si/internal/providers"
)

const openAIUsageText = "usage: si openai <auth|context|doctor|model|project|key|usage|monitor|codex|raw>"
const openAIAuthUsageText = "usage: si openai auth <status|codex-status> [--account <alias>] [--auth-mode <api|codex>] [--profile <profile>] [--json]"

type openaiRuntimeContext struct {
	AccountAlias string
	BaseURL      string
	APIKey       string
	AdminAPIKey  string
	OrgID        string
	ProjectID    string
	Source       string
	LogPath      string
}

type openaiRuntimeContextInput struct {
	AccountFlag     string
	BaseURLFlag     string
	APIKeyFlag      string
	AdminAPIKeyFlag string
	OrgIDFlag       string
	ProjectIDFlag   string
}

type openaiRequest struct {
	Method      string
	Path        string
	Params      url.Values
	Headers     map[string]string
	RawBody     string
	JSONBody    any
	ContentType string
	UseAdminKey bool
}

type openaiResponse struct {
	StatusCode int               `json:"status_code"`
	Status     string            `json:"status"`
	RequestID  string            `json:"request_id,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	Data       map[string]any    `json:"data,omitempty"`
	List       []map[string]any  `json:"list,omitempty"`
}

type openaiAPIErrorDetails struct {
	StatusCode int    `json:"status_code,omitempty"`
	Code       string `json:"code,omitempty"`
	Type       string `json:"type,omitempty"`
	Message    string `json:"message,omitempty"`
	Param      string `json:"param,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	RawBody    string `json:"raw_body,omitempty"`
}

func (e *openaiAPIErrorDetails) Error() string {
	if e == nil {
		return "openai api error"
	}
	parts := make([]string, 0, 8)
	if e.StatusCode > 0 {
		parts = append(parts, fmt.Sprintf("status_code=%d", e.StatusCode))
	}
	if strings.TrimSpace(e.Code) != "" {
		parts = append(parts, "code="+e.Code)
	}
	if strings.TrimSpace(e.Type) != "" {
		parts = append(parts, "type="+e.Type)
	}
	if strings.TrimSpace(e.Param) != "" {
		parts = append(parts, "param="+e.Param)
	}
	if strings.TrimSpace(e.Message) != "" {
		parts = append(parts, "message="+e.Message)
	}
	if strings.TrimSpace(e.RequestID) != "" {
		parts = append(parts, "request_id="+e.RequestID)
	}
	if len(parts) == 0 {
		return "openai api error"
	}
	return "openai api error: " + strings.Join(parts, ", ")
}

func cmdOpenAI(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, openAIUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(openAIUsageText)
	case "auth":
		cmdOpenAIAuth(rest)
	case "context":
		cmdOpenAIContext(rest)
	case "doctor":
		cmdOpenAIDoctor(rest)
	case "model", "models":
		cmdOpenAIModel(rest)
	case "project", "projects":
		cmdOpenAIProject(rest)
	case "key", "keys", "admin-key", "admin-keys":
		cmdOpenAIKey(rest)
	case "usage":
		cmdOpenAIUsage(rest)
	case "monitor", "monitoring":
		cmdOpenAIMonitor(rest)
	case "codex":
		cmdOpenAICodex(rest)
	case "raw":
		cmdOpenAIRaw(rest)
	default:
		printUnknown("openai", sub)
		printUsage(openAIUsageText)
	}
}

func cmdOpenAIAuth(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, openAIAuthUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "status":
		cmdOpenAIAuthStatus(args[1:])
	case "codex-status":
		cmdOpenAICodexAuthStatus(args[1:])
	default:
		printUnknown("openai auth", sub)
		printUsage(openAIAuthUsageText)
	}
}

func cmdOpenAIAuthStatus(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("openai auth status", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	authMode := fs.String("auth-mode", "api", "auth mode (api|codex)")
	codexProfile := fs.String("profile", "", "codex profile id/name/email (for auth-mode=codex)")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si openai auth status [--account <alias>] [--auth-mode <api|codex>] [--profile <profile>] [--json]")
		return
	}
	switch normalizeOpenAIAuthMode(*authMode) {
	case "codex":
		runOpenAICodexAuthStatus(strings.TrimSpace(*codexProfile), *jsonOut)
		return
	case "api":
	default:
		fatal(fmt.Errorf("invalid --auth-mode %q (expected api or codex)", strings.TrimSpace(*authMode)))
	}
	runtime, err := resolveRuntimeFromOpenAIFlags(flags)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	verifyResp, verifyErr := openaiDo(ctx, runtime, openaiRequest{
		Method: http.MethodGet,
		Path:   "/v1/models",
		Params: url.Values{"limit": []string{"1"}},
	})
	status := "error"
	if verifyErr == nil {
		status = "ready"
	}
	payload := map[string]any{
		"status":          status,
		"account_alias":   runtime.AccountAlias,
		"organization_id": runtime.OrgID,
		"project_id":      runtime.ProjectID,
		"source":          runtime.Source,
		"base_url":        runtime.BaseURL,
		"api_key_preview": previewOpenAISecret(runtime.APIKey),
		"admin_key_set":   strings.TrimSpace(runtime.AdminAPIKey) != "",
	}
	if verifyErr == nil {
		payload["verify_status"] = verifyResp.StatusCode
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
		fmt.Printf("%s %s\n", styleHeading("OpenAI auth:"), styleError("error"))
		fmt.Printf("%s %s\n", styleHeading("Context:"), formatOpenAIContext(runtime))
		printOpenAIError(verifyErr)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("OpenAI auth:"), styleSuccess("ready"))
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatOpenAIContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
	fmt.Printf("%s %s\n", styleHeading("API key preview:"), previewOpenAISecret(runtime.APIKey))
}

func cmdOpenAICodexAuthStatus(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("openai auth codex-status", flag.ExitOnError)
	profile := fs.String("profile", "", "codex profile id/name/email")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si openai auth codex-status [--profile <profile>] [--json]")
		return
	}
	runOpenAICodexAuthStatus(strings.TrimSpace(*profile), *jsonOut)
}

func normalizeOpenAIAuthMode(value string) string {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "", "api", "apikey", "api-key":
		return "api"
	case "codex", "chatgpt", "plan":
		return "codex"
	default:
		return mode
	}
}

func runOpenAICodexAuthStatus(profileKey string, jsonOut bool) {
	profile, err := resolveOpenAICodexProfile(profileKey)
	if err != nil {
		fatal(err)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	auth, authErr := loadProfileAuthTokens(profile)
	if authErr == nil && strings.TrimSpace(auth.AccessToken) == "" && strings.TrimSpace(auth.RefreshToken) != "" {
		if refreshed, refreshErr := refreshProfileAuthTokens(ctx, client, profile, auth); refreshErr == nil {
			auth = refreshed
		}
	}

	verifyStatus := codexStatus{}
	verifyErr := authErr
	if verifyErr == nil {
		verifyStatus, verifyErr = fetchUsageStatus(ctx, client, profileUsageURL(), auth)
	}

	status := "error"
	if verifyErr == nil {
		status = "ready"
	}
	payload := map[string]any{
		"status":               status,
		"auth_mode":            "codex",
		"profile_id":           profile.ID,
		"profile_name":         strings.TrimSpace(profile.Name),
		"profile_email":        strings.TrimSpace(profile.Email),
		"usage_url":            profileUsageURL(),
		"source":               "codex.auth.json",
		"access_token_preview": previewOpenAISecret(auth.AccessToken),
		"account_id_set":       strings.TrimSpace(auth.AccountID) != "",
	}
	if verifyErr == nil {
		payload["verify"] = map[string]any{
			"plan_type":                   strings.TrimSpace(verifyStatus.AccountPlan),
			"account_email":               strings.TrimSpace(verifyStatus.AccountEmail),
			"five_hour_left_pct":          verifyStatus.FiveHourLeftPct,
			"five_hour_reset":             strings.TrimSpace(verifyStatus.FiveHourReset),
			"five_hour_remaining_minutes": verifyStatus.FiveHourRemaining,
			"weekly_left_pct":             verifyStatus.WeeklyLeftPct,
			"weekly_reset":                strings.TrimSpace(verifyStatus.WeeklyReset),
			"weekly_remaining_minutes":    verifyStatus.WeeklyRemaining,
		}
	} else {
		payload["verify_error"] = verifyErr.Error()
	}

	if jsonOut {
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
		fmt.Printf("%s %s\n", styleHeading("OpenAI auth (codex):"), styleError("error"))
		fmt.Printf("%s %s\n", styleHeading("Profile:"), formatCodexProfileSummary(profile))
		printOpenAIError(verifyErr)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("OpenAI auth (codex):"), styleSuccess("ready"))
	fmt.Printf("%s %s\n", styleHeading("Profile:"), formatCodexProfileSummary(profile))
	fmt.Printf("%s %s\n", styleHeading("Plan:"), orDash(verifyStatus.AccountPlan))
	fmt.Printf("%s %s\n", styleHeading("5h window:"), formatLimitDetail(verifyStatus.FiveHourLeftPct, verifyStatus.FiveHourReset, verifyStatus.FiveHourRemaining))
	fmt.Printf("%s %s\n", styleHeading("Weekly window:"), formatLimitDetail(verifyStatus.WeeklyLeftPct, verifyStatus.WeeklyReset, verifyStatus.WeeklyRemaining))
}

func resolveOpenAICodexProfile(profileKey string) (codexProfile, error) {
	key := strings.TrimSpace(profileKey)
	if key != "" {
		return requireCodexProfile(key)
	}
	settings := loadSettingsOrDefault()
	defaultKey := strings.TrimSpace(codexDefaultProfileKey(settings))
	if defaultKey != "" {
		if profile, ok := codexProfileByKey(defaultKey); ok {
			return profile, nil
		}
	}
	profiles := codexProfiles()
	switch len(profiles) {
	case 0:
		return codexProfile{}, errors.New("no codex profiles configured; run `si login`")
	case 1:
		return profiles[0], nil
	default:
		return codexProfile{}, fmt.Errorf("multiple codex profiles configured (%s); set --profile", strings.Join(codexProfileIDs(), ", "))
	}
}

func formatCodexProfileSummary(profile codexProfile) string {
	parts := make([]string, 0, 3)
	if id := strings.TrimSpace(profile.ID); id != "" {
		parts = append(parts, "id="+id)
	}
	if name := strings.TrimSpace(profile.Name); name != "" {
		parts = append(parts, "name="+name)
	}
	if email := strings.TrimSpace(profile.Email); email != "" {
		parts = append(parts, "email="+email)
	}
	if len(parts) == 0 {
		return "(unknown)"
	}
	return strings.Join(parts, " ")
}

func cmdOpenAIContext(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si openai context <list|current|use>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdOpenAIContextList(rest)
	case "current":
		cmdOpenAIContextCurrent(rest)
	case "use":
		cmdOpenAIContextUse(rest)
	default:
		printUnknown("openai context", sub)
	}
}

func cmdOpenAIContextList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("openai context list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si openai context list [--json]")
		return
	}
	settings := loadSettingsOrDefault()
	aliases := openaiAccountAliases(settings)
	rows := make([]map[string]string, 0, len(aliases))
	for _, alias := range aliases {
		entry := settings.OpenAI.Accounts[alias]
		rows = append(rows, map[string]string{
			"alias":             alias,
			"name":              strings.TrimSpace(entry.Name),
			"default":           boolString(alias == strings.TrimSpace(settings.OpenAI.DefaultAccount)),
			"api_key_env":       strings.TrimSpace(entry.APIKeyEnv),
			"admin_api_key_env": strings.TrimSpace(entry.AdminAPIKeyEnv),
			"org_id":            strings.TrimSpace(firstNonEmpty(entry.OrganizationID, entry.OrganizationIDEnv)),
			"project_id":        strings.TrimSpace(firstNonEmpty(entry.ProjectID, entry.ProjectIDEnv)),
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
		infof("no openai accounts configured in settings")
		return
	}
	headers := []string{
		styleHeading("ALIAS"),
		styleHeading("DEFAULT"),
		styleHeading("API KEY ENV"),
		styleHeading("ADMIN KEY ENV"),
		styleHeading("ORG"),
		styleHeading("PROJECT"),
		styleHeading("NAME"),
	}
	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		tableRows = append(tableRows, []string{
			orDash(row["alias"]),
			orDash(row["default"]),
			orDash(row["api_key_env"]),
			orDash(row["admin_api_key_env"]),
			orDash(row["org_id"]),
			orDash(row["project_id"]),
			orDash(row["name"]),
		})
	}
	printAlignedTable(headers, tableRows, 2)
}

func cmdOpenAIContextCurrent(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("openai context current", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si openai context current [--json]")
		return
	}
	runtime, err := resolveRuntimeFromOpenAIFlags(flags)
	if err != nil {
		fatal(err)
	}
	payload := map[string]any{
		"account_alias":   runtime.AccountAlias,
		"base_url":        runtime.BaseURL,
		"organization_id": runtime.OrgID,
		"project_id":      runtime.ProjectID,
		"source":          runtime.Source,
		"admin_key_set":   strings.TrimSpace(runtime.AdminAPIKey) != "",
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Current openai context:"), formatOpenAIContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
}

func cmdOpenAIContextUse(args []string) {
	fs := flag.NewFlagSet("openai context use", flag.ExitOnError)
	account := fs.String("account", "", "default account alias")
	baseURL := fs.String("base-url", "", "api base url")
	orgID := fs.String("org-id", "", "organization id")
	projectID := fs.String("project-id", "", "project id")
	apiKeyEnv := fs.String("api-key-env", "", "api key env-var reference for selected account")
	adminAPIKeyEnv := fs.String("admin-api-key-env", "", "admin api key env-var reference for selected account")
	orgIDEnv := fs.String("org-id-env", "", "organization id env-var reference for selected account")
	projectIDEnv := fs.String("project-id-env", "", "project id env-var reference for selected account")
	vaultPrefix := fs.String("vault-prefix", "", "account env prefix (optional)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si openai context use [--account <alias>] [--base-url <url>] [--org-id <id>] [--project-id <id>] [--api-key-env <env>] [--admin-api-key-env <env>] [--org-id-env <env>] [--project-id-env <env>] [--vault-prefix <prefix>]")
		return
	}
	settings := loadSettingsOrDefault()
	if value := strings.TrimSpace(*account); value != "" {
		settings.OpenAI.DefaultAccount = value
	}
	if value := strings.TrimSpace(*baseURL); value != "" {
		settings.OpenAI.APIBaseURL = value
	}
	if value := strings.TrimSpace(*orgID); value != "" {
		settings.OpenAI.DefaultOrganizationID = value
	}
	if value := strings.TrimSpace(*projectID); value != "" {
		settings.OpenAI.DefaultProjectID = value
	}

	targetAlias := strings.TrimSpace(settings.OpenAI.DefaultAccount)
	if value := strings.TrimSpace(*account); value != "" {
		targetAlias = value
	}
	if targetAlias == "" && (strings.TrimSpace(*apiKeyEnv) != "" || strings.TrimSpace(*adminAPIKeyEnv) != "" || strings.TrimSpace(*orgIDEnv) != "" || strings.TrimSpace(*projectIDEnv) != "" || strings.TrimSpace(*vaultPrefix) != "") {
		targetAlias = "default"
		settings.OpenAI.DefaultAccount = targetAlias
	}
	if targetAlias != "" {
		if settings.OpenAI.Accounts == nil {
			settings.OpenAI.Accounts = map[string]OpenAIAccountEntry{}
		}
		entry := settings.OpenAI.Accounts[targetAlias]
		if value := strings.TrimSpace(*apiKeyEnv); value != "" {
			entry.APIKeyEnv = value
		}
		if value := strings.TrimSpace(*adminAPIKeyEnv); value != "" {
			entry.AdminAPIKeyEnv = value
		}
		if value := strings.TrimSpace(*orgID); value != "" {
			entry.OrganizationID = value
		}
		if value := strings.TrimSpace(*orgIDEnv); value != "" {
			entry.OrganizationIDEnv = value
		}
		if value := strings.TrimSpace(*projectID); value != "" {
			entry.ProjectID = value
		}
		if value := strings.TrimSpace(*projectIDEnv); value != "" {
			entry.ProjectIDEnv = value
		}
		if value := strings.TrimSpace(*vaultPrefix); value != "" {
			entry.VaultPrefix = value
		}
		settings.OpenAI.Accounts[targetAlias] = entry
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	fmt.Printf("%s default_account=%s base=%s org=%s project=%s\n",
		styleHeading("Updated openai context:"),
		orDash(settings.OpenAI.DefaultAccount),
		orDash(settings.OpenAI.APIBaseURL),
		orDash(settings.OpenAI.DefaultOrganizationID),
		orDash(settings.OpenAI.DefaultProjectID),
	)
}

func cmdOpenAIDoctor(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "public": true})
	fs := flag.NewFlagSet("openai doctor", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	publicProbe := fs.Bool("public", false, "run unauthenticated public probe")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si openai doctor [--account <alias>] [--public] [--json]")
		return
	}
	if *publicProbe {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err := runPublicProviderDoctor(ctx, providers.OpenAI, strings.TrimSpace(*flags.baseURL))
		if err != nil {
			fatal(err)
		}
		printPublicDoctorResult("openai", result, *jsonOut)
		return
	}
	runtime, err := resolveRuntimeFromOpenAIFlags(flags)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, verifyErr := openaiDo(ctx, runtime, openaiRequest{
		Method: http.MethodGet,
		Path:   "/v1/models",
		Params: url.Values{"limit": []string{"1"}},
	})
	checks := []doctorCheck{
		{Name: "api-key", OK: strings.TrimSpace(runtime.APIKey) != "", Detail: previewOpenAISecret(runtime.APIKey)},
		{Name: "base-url", OK: strings.TrimSpace(runtime.BaseURL) != "", Detail: runtime.BaseURL},
		{Name: "request", OK: verifyErr == nil, Detail: errorOrOK(verifyErr)},
	}
	ok := true
	for _, check := range checks {
		if !check.OK {
			ok = false
		}
	}
	payload := map[string]any{
		"ok":              ok,
		"provider":        "openai",
		"base_url":        runtime.BaseURL,
		"account_alias":   runtime.AccountAlias,
		"organization_id": runtime.OrgID,
		"project_id":      runtime.ProjectID,
		"checks":          checks,
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
		fmt.Printf("%s %s\n", styleHeading("OpenAI doctor:"), styleSuccess("ok"))
	} else {
		fmt.Printf("%s %s\n", styleHeading("OpenAI doctor:"), styleError("issues found"))
	}
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatOpenAIContext(runtime))
	rows := make([][]string, 0, len(checks))
	for _, check := range checks {
		icon := styleSuccess("OK")
		if !check.OK {
			icon = styleError("ERR")
		}
		rows = append(rows, []string{icon, check.Name, strings.TrimSpace(check.Detail)})
	}
	printAlignedRows(rows, 2, "  ")
	if !ok {
		os.Exit(1)
	}
}

func cmdOpenAIModel(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si openai model <list|get> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdOpenAIModelList(rest)
	case "get", "retrieve":
		cmdOpenAIModelGet(rest)
	default:
		printUnknown("openai model", sub)
	}
}

func cmdOpenAIModelList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("openai model list", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	limit := fs.Int("limit", 20, "max models")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si openai model list [--limit N] [--json]")
		return
	}
	params := url.Values{}
	if *limit > 0 {
		params.Set("limit", strconv.Itoa(*limit))
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodGet, Path: "/v1/models", Params: params}, *jsonOut, *raw)
}

func cmdOpenAIModelGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("openai model get", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	modelID := fs.String("id", "", "model id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*modelID)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si openai model get <model-id> [--json]")
		return
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodGet, Path: "/v1/models/" + url.PathEscape(id)}, *jsonOut, *raw)
}

func cmdOpenAIProject(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si openai project <list|get|create|update|archive|rate-limit|api-key|service-account> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdOpenAIProjectList(rest)
	case "get", "retrieve":
		cmdOpenAIProjectGet(rest)
	case "create":
		cmdOpenAIProjectCreate(rest)
	case "update", "modify":
		cmdOpenAIProjectUpdate(rest)
	case "archive":
		cmdOpenAIProjectArchive(rest)
	case "rate-limit", "rate-limits":
		cmdOpenAIProjectRateLimit(rest)
	case "api-key", "api-keys":
		cmdOpenAIProjectAPIKey(rest)
	case "service-account", "service-accounts":
		cmdOpenAIProjectServiceAccount(rest)
	default:
		printUnknown("openai project", sub)
	}
}

func cmdOpenAIProjectList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "include-archived": true})
	fs := flag.NewFlagSet("openai project list", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	limit := fs.Int("limit", 20, "max projects")
	after := fs.String("after", "", "pagination cursor")
	includeArchived := fs.Bool("include-archived", false, "include archived projects")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si openai project list [--limit N] [--after <id>] [--include-archived] [--json]")
		return
	}
	params := url.Values{}
	if *limit > 0 {
		params.Set("limit", strconv.Itoa(*limit))
	}
	if value := strings.TrimSpace(*after); value != "" {
		params.Set("after", value)
	}
	if *includeArchived {
		params.Set("include_archived", "true")
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodGet, Path: "/v1/organization/projects", Params: params, UseAdminKey: true}, *jsonOut, *raw)
}

func cmdOpenAIProjectGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("openai project get", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	idFlag := fs.String("id", "", "project id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*idFlag)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si openai project get <project-id> [--json]")
		return
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodGet, Path: "/v1/organization/projects/" + url.PathEscape(id), UseAdminKey: true}, *jsonOut, *raw)
}

func cmdOpenAIProjectCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("openai project create", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	name := fs.String("name", "", "project name")
	geography := fs.String("geography", "", "data residency geography")
	body := fs.String("body", "", "raw json body")
	bodyFile := fs.String("body-file", "", "json body file path")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si openai project create --name <name> [--geography <region>] [--json]")
		return
	}
	payload, err := openaiResolveJSONBody(strings.TrimSpace(*body), strings.TrimSpace(*bodyFile), nil)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(payload) == "" {
		if strings.TrimSpace(*name) == "" {
			fatal(fmt.Errorf("--name or --body/--body-file is required"))
		}
		request := map[string]any{"name": strings.TrimSpace(*name)}
		if value := strings.TrimSpace(*geography); value != "" {
			request["geography"] = value
		}
		encoded, err := json.Marshal(request)
		if err != nil {
			fatal(err)
		}
		payload = string(encoded)
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodPost, Path: "/v1/organization/projects", RawBody: payload, ContentType: "application/json", UseAdminKey: true}, *jsonOut, *raw)
}

func cmdOpenAIProjectUpdate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("openai project update", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	idFlag := fs.String("id", "", "project id")
	name := fs.String("name", "", "project name")
	body := fs.String("body", "", "raw json body")
	bodyFile := fs.String("body-file", "", "json body file path")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*idFlag)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si openai project update <project-id> [--name <name>|--body <json>|--body-file <path>] [--json]")
		return
	}
	payload, err := openaiResolveJSONBody(strings.TrimSpace(*body), strings.TrimSpace(*bodyFile), nil)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(payload) == "" {
		if strings.TrimSpace(*name) == "" {
			fatal(fmt.Errorf("--name or --body/--body-file is required"))
		}
		request := map[string]any{"name": strings.TrimSpace(*name)}
		encoded, err := json.Marshal(request)
		if err != nil {
			fatal(err)
		}
		payload = string(encoded)
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodPost, Path: "/v1/organization/projects/" + url.PathEscape(id), RawBody: payload, ContentType: "application/json", UseAdminKey: true}, *jsonOut, *raw)
}

func cmdOpenAIProjectArchive(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("openai project archive", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	idFlag := fs.String("id", "", "project id")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*idFlag)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si openai project archive <project-id> [--force] [--json]")
		return
	}
	if err := requireConfirmation("archive openai project "+id, *force); err != nil {
		fatal(err)
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodPost, Path: "/v1/organization/projects/" + url.PathEscape(id) + "/archive", JSONBody: map[string]any{}, UseAdminKey: true}, *jsonOut, *raw)
}

func cmdOpenAIProjectRateLimit(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si openai project rate-limit <list|update> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdOpenAIProjectRateLimitList(rest)
	case "update", "set":
		cmdOpenAIProjectRateLimitUpdate(rest)
	default:
		printUnknown("openai project rate-limit", sub)
	}
}

func cmdOpenAIProjectRateLimitList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("openai project rate-limit list", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	projectID := fs.String("project-id", "", "project id")
	limit := fs.Int("limit", 100, "max items")
	after := fs.String("after", "", "pagination cursor")
	before := fs.String("before", "", "pagination cursor")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	pid := strings.TrimSpace(*projectID)
	if pid == "" {
		pid = strings.TrimSpace(valueOrEmpty(flags.projectID))
	}
	if pid == "" || fs.NArg() > 0 {
		printUsage("usage: si openai project rate-limit list --project-id <id> [--limit N] [--json]")
		return
	}
	params := url.Values{}
	if *limit > 0 {
		params.Set("limit", strconv.Itoa(*limit))
	}
	if value := strings.TrimSpace(*after); value != "" {
		params.Set("after", value)
	}
	if value := strings.TrimSpace(*before); value != "" {
		params.Set("before", value)
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodGet, Path: "/v1/organization/projects/" + url.PathEscape(pid) + "/rate_limits", Params: params, UseAdminKey: true}, *jsonOut, *raw)
}

func cmdOpenAIProjectRateLimitUpdate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("openai project rate-limit update", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	projectID := fs.String("project-id", "", "project id")
	rateLimitID := fs.String("rate-limit-id", "", "rate limit id")
	body := fs.String("body", "", "raw json body")
	bodyFile := fs.String("body-file", "", "json body file path")
	maxReqMinute := fs.Int("max-requests-per-1-minute", -1, "max requests per minute")
	maxReqDay := fs.Int("max-requests-per-1-day", -1, "max requests per day")
	maxTokensMinute := fs.Int("max-tokens-per-1-minute", -1, "max tokens per minute")
	maxImagesMinute := fs.Int("max-images-per-1-minute", -1, "max images per minute")
	maxAudioMBMinute := fs.Int("max-audio-megabytes-per-1-minute", -1, "max audio MB per minute")
	batchMaxInputPerDay := fs.Int("batch-1-day-max-input-tokens", -1, "batch max input tokens per day")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	pid := strings.TrimSpace(*projectID)
	if pid == "" {
		pid = strings.TrimSpace(valueOrEmpty(flags.projectID))
	}
	if pid == "" || strings.TrimSpace(*rateLimitID) == "" || fs.NArg() > 0 {
		printUsage("usage: si openai project rate-limit update --project-id <id> --rate-limit-id <id> [limit fields|--body <json>|--body-file <path>] [--json]")
		return
	}
	payload, err := openaiResolveJSONBody(strings.TrimSpace(*body), strings.TrimSpace(*bodyFile), nil)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(payload) == "" {
		request := map[string]any{}
		if *maxReqMinute >= 0 {
			request["max_requests_per_1_minute"] = *maxReqMinute
		}
		if *maxReqDay >= 0 {
			request["max_requests_per_1_day"] = *maxReqDay
		}
		if *maxTokensMinute >= 0 {
			request["max_tokens_per_1_minute"] = *maxTokensMinute
		}
		if *maxImagesMinute >= 0 {
			request["max_images_per_1_minute"] = *maxImagesMinute
		}
		if *maxAudioMBMinute >= 0 {
			request["max_audio_megabytes_per_1_minute"] = *maxAudioMBMinute
		}
		if *batchMaxInputPerDay >= 0 {
			request["batch_1_day_max_input_tokens"] = *batchMaxInputPerDay
		}
		if len(request) == 0 {
			fatal(fmt.Errorf("provide at least one limit field or --body/--body-file"))
		}
		encoded, err := json.Marshal(request)
		if err != nil {
			fatal(err)
		}
		payload = string(encoded)
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodPost, Path: "/v1/organization/projects/" + url.PathEscape(pid) + "/rate_limits/" + url.PathEscape(strings.TrimSpace(*rateLimitID)), RawBody: payload, ContentType: "application/json", UseAdminKey: true}, *jsonOut, *raw)
}

func cmdOpenAIProjectAPIKey(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si openai project api-key <list|get|delete> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdOpenAIProjectAPIKeyList(rest)
	case "get", "retrieve":
		cmdOpenAIProjectAPIKeyGet(rest)
	case "delete", "remove", "rm":
		cmdOpenAIProjectAPIKeyDelete(rest)
	default:
		printUnknown("openai project api-key", sub)
	}
}

func cmdOpenAIProjectAPIKeyList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("openai project api-key list", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	projectID := fs.String("project-id", "", "project id")
	limit := fs.Int("limit", 20, "max keys")
	after := fs.String("after", "", "pagination cursor")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	pid := strings.TrimSpace(*projectID)
	if pid == "" {
		pid = strings.TrimSpace(valueOrEmpty(flags.projectID))
	}
	if pid == "" || fs.NArg() > 0 {
		printUsage("usage: si openai project api-key list --project-id <id> [--limit N] [--json]")
		return
	}
	params := url.Values{}
	if *limit > 0 {
		params.Set("limit", strconv.Itoa(*limit))
	}
	if value := strings.TrimSpace(*after); value != "" {
		params.Set("after", value)
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodGet, Path: "/v1/organization/projects/" + url.PathEscape(pid) + "/api_keys", Params: params, UseAdminKey: true}, *jsonOut, *raw)
}

func cmdOpenAIProjectAPIKeyGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("openai project api-key get", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	projectID := fs.String("project-id", "", "project id")
	keyID := fs.String("key-id", "", "key id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	pid := strings.TrimSpace(*projectID)
	if pid == "" {
		pid = strings.TrimSpace(valueOrEmpty(flags.projectID))
	}
	kid := strings.TrimSpace(*keyID)
	if kid == "" && fs.NArg() == 1 {
		kid = strings.TrimSpace(fs.Arg(0))
	}
	if pid == "" || kid == "" || fs.NArg() > 1 {
		printUsage("usage: si openai project api-key get --project-id <id> <key-id> [--json]")
		return
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodGet, Path: "/v1/organization/projects/" + url.PathEscape(pid) + "/api_keys/" + url.PathEscape(kid), UseAdminKey: true}, *jsonOut, *raw)
}

func cmdOpenAIProjectAPIKeyDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("openai project api-key delete", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	projectID := fs.String("project-id", "", "project id")
	keyID := fs.String("key-id", "", "key id")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	pid := strings.TrimSpace(*projectID)
	if pid == "" {
		pid = strings.TrimSpace(valueOrEmpty(flags.projectID))
	}
	kid := strings.TrimSpace(*keyID)
	if kid == "" && fs.NArg() == 1 {
		kid = strings.TrimSpace(fs.Arg(0))
	}
	if pid == "" || kid == "" || fs.NArg() > 1 {
		printUsage("usage: si openai project api-key delete --project-id <id> <key-id> [--force] [--json]")
		return
	}
	if err := requireConfirmation("delete openai project api key "+kid, *force); err != nil {
		fatal(err)
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodDelete, Path: "/v1/organization/projects/" + url.PathEscape(pid) + "/api_keys/" + url.PathEscape(kid), UseAdminKey: true}, *jsonOut, *raw)
}

func cmdOpenAIProjectServiceAccount(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si openai project service-account <list|create|get|delete> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdOpenAIProjectServiceAccountList(rest)
	case "create":
		cmdOpenAIProjectServiceAccountCreate(rest)
	case "get", "retrieve":
		cmdOpenAIProjectServiceAccountGet(rest)
	case "delete", "remove", "rm":
		cmdOpenAIProjectServiceAccountDelete(rest)
	default:
		printUnknown("openai project service-account", sub)
	}
}

func cmdOpenAIProjectServiceAccountList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("openai project service-account list", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	projectID := fs.String("project-id", "", "project id")
	limit := fs.Int("limit", 20, "max service accounts")
	after := fs.String("after", "", "pagination cursor")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	pid := strings.TrimSpace(*projectID)
	if pid == "" {
		pid = strings.TrimSpace(valueOrEmpty(flags.projectID))
	}
	if pid == "" || fs.NArg() > 0 {
		printUsage("usage: si openai project service-account list --project-id <id> [--limit N] [--json]")
		return
	}
	params := url.Values{}
	if *limit > 0 {
		params.Set("limit", strconv.Itoa(*limit))
	}
	if value := strings.TrimSpace(*after); value != "" {
		params.Set("after", value)
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodGet, Path: "/v1/organization/projects/" + url.PathEscape(pid) + "/service_accounts", Params: params, UseAdminKey: true}, *jsonOut, *raw)
}

func cmdOpenAIProjectServiceAccountCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("openai project service-account create", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	projectID := fs.String("project-id", "", "project id")
	name := fs.String("name", "", "service account name")
	body := fs.String("body", "", "raw json body")
	bodyFile := fs.String("body-file", "", "json body file path")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	pid := strings.TrimSpace(*projectID)
	if pid == "" {
		pid = strings.TrimSpace(valueOrEmpty(flags.projectID))
	}
	if pid == "" || fs.NArg() > 0 {
		printUsage("usage: si openai project service-account create --project-id <id> --name <name> [--json]")
		return
	}
	payload, err := openaiResolveJSONBody(strings.TrimSpace(*body), strings.TrimSpace(*bodyFile), nil)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(payload) == "" {
		if strings.TrimSpace(*name) == "" {
			fatal(fmt.Errorf("--name or --body/--body-file is required"))
		}
		encoded, err := json.Marshal(map[string]any{"name": strings.TrimSpace(*name)})
		if err != nil {
			fatal(err)
		}
		payload = string(encoded)
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodPost, Path: "/v1/organization/projects/" + url.PathEscape(pid) + "/service_accounts", RawBody: payload, ContentType: "application/json", UseAdminKey: true}, *jsonOut, *raw)
}

func cmdOpenAIProjectServiceAccountGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("openai project service-account get", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	projectID := fs.String("project-id", "", "project id")
	serviceAccountID := fs.String("service-account-id", "", "service account id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	pid := strings.TrimSpace(*projectID)
	if pid == "" {
		pid = strings.TrimSpace(valueOrEmpty(flags.projectID))
	}
	said := strings.TrimSpace(*serviceAccountID)
	if said == "" && fs.NArg() == 1 {
		said = strings.TrimSpace(fs.Arg(0))
	}
	if pid == "" || said == "" || fs.NArg() > 1 {
		printUsage("usage: si openai project service-account get --project-id <id> <service-account-id> [--json]")
		return
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodGet, Path: "/v1/organization/projects/" + url.PathEscape(pid) + "/service_accounts/" + url.PathEscape(said), UseAdminKey: true}, *jsonOut, *raw)
}

func cmdOpenAIProjectServiceAccountDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("openai project service-account delete", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	projectID := fs.String("project-id", "", "project id")
	serviceAccountID := fs.String("service-account-id", "", "service account id")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	pid := strings.TrimSpace(*projectID)
	if pid == "" {
		pid = strings.TrimSpace(valueOrEmpty(flags.projectID))
	}
	said := strings.TrimSpace(*serviceAccountID)
	if said == "" && fs.NArg() == 1 {
		said = strings.TrimSpace(fs.Arg(0))
	}
	if pid == "" || said == "" || fs.NArg() > 1 {
		printUsage("usage: si openai project service-account delete --project-id <id> <service-account-id> [--force] [--json]")
		return
	}
	if err := requireConfirmation("delete openai service account "+said, *force); err != nil {
		fatal(err)
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodDelete, Path: "/v1/organization/projects/" + url.PathEscape(pid) + "/service_accounts/" + url.PathEscape(said), UseAdminKey: true}, *jsonOut, *raw)
}

func cmdOpenAIKey(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si openai key <list|get|create|delete> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdOpenAIKeyList(rest)
	case "get", "retrieve":
		cmdOpenAIKeyGet(rest)
	case "create":
		cmdOpenAIKeyCreate(rest)
	case "delete", "remove", "rm":
		cmdOpenAIKeyDelete(rest)
	default:
		printUnknown("openai key", sub)
	}
}

func cmdOpenAIKeyList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("openai key list", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	limit := fs.Int("limit", 20, "max keys")
	after := fs.String("after", "", "pagination cursor")
	order := fs.String("order", "asc", "sort order asc|desc")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si openai key list [--limit N] [--after <id>] [--order asc|desc] [--json]")
		return
	}
	params := url.Values{}
	if *limit > 0 {
		params.Set("limit", strconv.Itoa(*limit))
	}
	if value := strings.TrimSpace(*after); value != "" {
		params.Set("after", value)
	}
	if value := strings.TrimSpace(*order); value != "" {
		params.Set("order", strings.ToLower(value))
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodGet, Path: "/v1/organization/admin_api_keys", Params: params, UseAdminKey: true}, *jsonOut, *raw)
}

func cmdOpenAIKeyGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("openai key get", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	keyID := fs.String("key-id", "", "admin key id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	kid := strings.TrimSpace(*keyID)
	if kid == "" && fs.NArg() == 1 {
		kid = strings.TrimSpace(fs.Arg(0))
	}
	if kid == "" || fs.NArg() > 1 {
		printUsage("usage: si openai key get <key-id> [--json]")
		return
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodGet, Path: "/v1/organization/admin_api_keys/" + url.PathEscape(kid), UseAdminKey: true}, *jsonOut, *raw)
}

func cmdOpenAIKeyCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("openai key create", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	name := fs.String("name", "", "admin key name")
	body := fs.String("body", "", "raw json body")
	bodyFile := fs.String("body-file", "", "json body file path")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si openai key create --name <name> [--json]")
		return
	}
	payload, err := openaiResolveJSONBody(strings.TrimSpace(*body), strings.TrimSpace(*bodyFile), nil)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(payload) == "" {
		if strings.TrimSpace(*name) == "" {
			fatal(fmt.Errorf("--name or --body/--body-file is required"))
		}
		encoded, err := json.Marshal(map[string]any{"name": strings.TrimSpace(*name)})
		if err != nil {
			fatal(err)
		}
		payload = string(encoded)
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodPost, Path: "/v1/organization/admin_api_keys", RawBody: payload, ContentType: "application/json", UseAdminKey: true}, *jsonOut, *raw)
}

func cmdOpenAIKeyDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("openai key delete", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	keyID := fs.String("key-id", "", "admin key id")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	kid := strings.TrimSpace(*keyID)
	if kid == "" && fs.NArg() == 1 {
		kid = strings.TrimSpace(fs.Arg(0))
	}
	if kid == "" || fs.NArg() > 1 {
		printUsage("usage: si openai key delete <key-id> [--force] [--json]")
		return
	}
	if err := requireConfirmation("delete openai admin api key "+kid, *force); err != nil {
		fatal(err)
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodDelete, Path: "/v1/organization/admin_api_keys/" + url.PathEscape(kid), UseAdminKey: true}, *jsonOut, *raw)
}

func cmdOpenAIUsage(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si openai usage <completions|embeddings|images|audio_speeches|audio_transcriptions|moderations|vector_stores|code_interpreter_sessions|costs> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	metric := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	cmdOpenAIUsageMetric(metric, rest)
}

func cmdOpenAIMonitor(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si openai monitor <usage|limits> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "usage":
		if len(rest) == 0 {
			cmdOpenAIUsageMetric("completions", rest)
			return
		}
		cmdOpenAIUsageMetric(strings.ToLower(strings.TrimSpace(rest[0])), rest[1:])
	case "limits", "rate-limits":
		cmdOpenAIProjectRateLimitList(rest)
	default:
		printUnknown("openai monitor", sub)
	}
}

func cmdOpenAICodex(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si openai codex usage [--model <name>] ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "usage":
		cmdOpenAICodexUsage(rest)
	default:
		printUnknown("openai codex", sub)
	}
}

func cmdOpenAICodexUsage(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("openai codex usage", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	startTime := fs.Int64("start-time", 0, "start time (unix seconds)")
	endTime := fs.Int64("end-time", 0, "end time (unix seconds)")
	bucketWidth := fs.String("bucket-width", "1d", "bucket width (1m|1h|1d)")
	limit := fs.Int("limit", 7, "max buckets")
	model := multiFlag{}
	fs.Var(&model, "model", "model filter (repeatable, defaults to codex family)")
	groupBy := multiFlag{}
	fs.Var(&groupBy, "group-by", "group_by field (repeatable)")
	projectIDs := multiFlag{}
	fs.Var(&projectIDs, "project", "project id filter (repeatable)")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si openai codex usage [--start-time <unix>] [--end-time <unix>] [--model gpt-5-codex] [--json]")
		return
	}
	metricArgs := []string{"completions"}
	if *startTime > 0 {
		metricArgs = append(metricArgs, "--start-time", strconv.FormatInt(*startTime, 10))
	}
	if *endTime > 0 {
		metricArgs = append(metricArgs, "--end-time", strconv.FormatInt(*endTime, 10))
	}
	if value := strings.TrimSpace(*bucketWidth); value != "" {
		metricArgs = append(metricArgs, "--bucket-width", value)
	}
	if *limit > 0 {
		metricArgs = append(metricArgs, "--limit", strconv.Itoa(*limit))
	}
	if len(model) == 0 {
		metricArgs = append(metricArgs, "--model", "gpt-5-codex")
	} else {
		for _, item := range model {
			metricArgs = append(metricArgs, "--model", strings.TrimSpace(item))
		}
	}
	for _, item := range groupBy {
		metricArgs = append(metricArgs, "--group-by", strings.TrimSpace(item))
	}
	for _, item := range projectIDs {
		metricArgs = append(metricArgs, "--project", strings.TrimSpace(item))
	}
	if *jsonOut {
		metricArgs = append(metricArgs, "--json")
	}
	if *raw {
		metricArgs = append(metricArgs, "--raw")
	}
	appendOpenAIFlagsFromCommon(&metricArgs, flags)
	cmdOpenAIUsage(metricArgs)
}

func cmdOpenAIUsageMetric(metric string, args []string) {
	metric = normalizeOpenAIUsageMetric(metric)
	if metric == "" {
		fatal(fmt.Errorf("unsupported usage metric"))
	}
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "batch": true})
	fs := flag.NewFlagSet("openai usage "+metric, flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	startTime := fs.Int64("start-time", 0, "start time (unix seconds)")
	endTime := fs.Int64("end-time", 0, "end time (unix seconds)")
	bucketWidth := fs.String("bucket-width", "1d", "bucket width (1m|1h|1d)")
	limit := fs.Int("limit", 7, "max buckets")
	page := fs.String("page", "", "pagination cursor")
	batch := fs.Bool("batch", false, "batch only (supported by completions)")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	projectIDs := multiFlag{}
	fs.Var(&projectIDs, "project", "project id filter (repeatable)")
	userIDs := multiFlag{}
	fs.Var(&userIDs, "user-id", "user id filter (repeatable)")
	apiKeyIDs := multiFlag{}
	fs.Var(&apiKeyIDs, "api-key-id", "api key id filter (repeatable)")
	models := multiFlag{}
	fs.Var(&models, "model", "model filter (repeatable)")
	groupBy := multiFlag{}
	fs.Var(&groupBy, "group-by", "group_by field (repeatable)")
	extraParams := multiFlag{}
	fs.Var(&extraParams, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si openai usage <metric> [--start-time <unix>] [--end-time <unix>] [--bucket-width <1m|1h|1d>] [--json]")
		return
	}
	params := url.Values{}
	if *startTime > 0 {
		params.Set("start_time", strconv.FormatInt(*startTime, 10))
	}
	if *endTime > 0 {
		params.Set("end_time", strconv.FormatInt(*endTime, 10))
	}
	if value := strings.TrimSpace(*bucketWidth); value != "" {
		params.Set("bucket_width", value)
	}
	if *limit > 0 {
		params.Set("limit", strconv.Itoa(*limit))
	}
	if value := strings.TrimSpace(*page); value != "" {
		params.Set("page", value)
	}
	if metric == "completions" && *batch {
		params.Set("batch", "true")
	}
	for _, item := range projectIDs {
		value := strings.TrimSpace(item)
		if value != "" {
			params.Add("project_ids", value)
		}
	}
	for _, item := range userIDs {
		value := strings.TrimSpace(item)
		if value != "" {
			params.Add("user_ids", value)
		}
	}
	for _, item := range apiKeyIDs {
		value := strings.TrimSpace(item)
		if value != "" {
			params.Add("api_key_ids", value)
		}
	}
	for _, item := range models {
		value := strings.TrimSpace(item)
		if value != "" {
			params.Add("models", value)
		}
	}
	for _, item := range groupBy {
		value := strings.TrimSpace(item)
		if value != "" {
			params.Add("group_by", value)
		}
	}
	for key, values := range parseOpenAIParamsToValues(extraParams) {
		for _, value := range values {
			params.Add(key, value)
		}
	}
	if strings.TrimSpace(params.Get("start_time")) == "" {
		params.Set("start_time", strconv.FormatInt(time.Now().UTC().Add(-7*24*time.Hour).Unix(), 10))
	}
	path := "/v1/organization/usage/" + metric
	if metric == "costs" {
		path = "/v1/organization/costs"
	}
	runOpenAIRequest(flags, openaiRequest{Method: http.MethodGet, Path: path, Params: params, UseAdminKey: true}, *jsonOut, *raw)
}

func normalizeOpenAIUsageMetric(metric string) string {
	metric = strings.ToLower(strings.TrimSpace(metric))
	switch metric {
	case "completion", "completions":
		return "completions"
	case "embedding", "embeddings":
		return "embeddings"
	case "image", "images":
		return "images"
	case "audio-speeches", "audio_speeches", "speeches":
		return "audio_speeches"
	case "audio-transcriptions", "audio_transcriptions", "transcriptions":
		return "audio_transcriptions"
	case "moderation", "moderations":
		return "moderations"
	case "vector-store", "vector-stores", "vector_stores":
		return "vector_stores"
	case "code-interpreter-sessions", "code_interpreter_sessions", "code-interpreter", "code_interpreter":
		return "code_interpreter_sessions"
	case "cost", "costs", "spend":
		return "costs"
	default:
		return ""
	}
}

func cmdOpenAIRaw(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "admin": true})
	fs := flag.NewFlagSet("openai raw", flag.ExitOnError)
	flags := bindOpenAICommonFlags(fs)
	method := fs.String("method", http.MethodGet, "http method")
	path := fs.String("path", "/v1/models", "api path")
	body := fs.String("body", "", "raw request body")
	bodyFile := fs.String("body-file", "", "request body file")
	jsonBody := fs.String("json-body", "", "json request body")
	contentType := fs.String("content-type", "application/json", "request content type")
	admin := fs.Bool("admin", false, "use admin api key")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	headers := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	fs.Var(&headers, "header", "header key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si openai raw --method <GET|POST|PATCH|DELETE> --path <api-path> [--param key=value] [--body raw|--json-body '{...}'] [--admin] [--json]")
		return
	}
	resolvedBody := strings.TrimSpace(*body)
	if value := strings.TrimSpace(*bodyFile); value != "" {
		data, err := os.ReadFile(value)
		if err != nil {
			fatal(err)
		}
		resolvedBody = strings.TrimSpace(string(data))
	}
	var payload any
	if rawJSON := strings.TrimSpace(*jsonBody); rawJSON != "" {
		if err := json.Unmarshal([]byte(rawJSON), &payload); err != nil {
			fatal(fmt.Errorf("invalid --json-body: %w", err))
		}
	}
	runOpenAIRequest(flags, openaiRequest{
		Method:      strings.ToUpper(strings.TrimSpace(*method)),
		Path:        strings.TrimSpace(*path),
		Params:      parseOpenAIParamsToValues(params),
		Headers:     parseOpenAISimpleParams(headers),
		RawBody:     resolvedBody,
		JSONBody:    payload,
		ContentType: strings.TrimSpace(*contentType),
		UseAdminKey: *admin,
	}, *jsonOut, *raw)
}

type openaiCommonFlags struct {
	account     *string
	baseURL     *string
	apiKey      *string
	adminAPIKey *string
	orgID       *string
	projectID   *string
}

func bindOpenAICommonFlags(fs *flag.FlagSet) openaiCommonFlags {
	return openaiCommonFlags{
		account:     fs.String("account", "", "account alias"),
		baseURL:     fs.String("base-url", "", "api base url"),
		apiKey:      fs.String("api-key", "", "override api key"),
		adminAPIKey: fs.String("admin-api-key", "", "override admin api key"),
		orgID:       fs.String("org-id", "", "organization id"),
		projectID:   fs.String("project-id", "", "project id"),
	}
}

func resolveRuntimeFromOpenAIFlags(flags openaiCommonFlags) (openaiRuntimeContext, error) {
	return resolveOpenAIRuntimeContext(openaiRuntimeContextInput{
		AccountFlag:     strings.TrimSpace(valueOrEmpty(flags.account)),
		BaseURLFlag:     strings.TrimSpace(valueOrEmpty(flags.baseURL)),
		APIKeyFlag:      strings.TrimSpace(valueOrEmpty(flags.apiKey)),
		AdminAPIKeyFlag: strings.TrimSpace(valueOrEmpty(flags.adminAPIKey)),
		OrgIDFlag:       strings.TrimSpace(valueOrEmpty(flags.orgID)),
		ProjectIDFlag:   strings.TrimSpace(valueOrEmpty(flags.projectID)),
	})
}

func resolveOpenAIRuntimeContext(input openaiRuntimeContextInput) (openaiRuntimeContext, error) {
	settings := loadSettingsOrDefault()
	alias, account := resolveOpenAIAccountSelection(settings, input.AccountFlag)
	spec := providers.Resolve(providers.OpenAI)
	baseURL := strings.TrimSpace(input.BaseURLFlag)
	if baseURL == "" {
		baseURL = strings.TrimSpace(account.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(settings.OpenAI.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("OPENAI_API_BASE_URL"))
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(spec.BaseURL)
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	apiKey, apiKeySource := resolveOpenAIAPIKey(alias, account, strings.TrimSpace(input.APIKeyFlag))
	if strings.TrimSpace(apiKey) == "" {
		prefix := openaiAccountEnvPrefix(alias, account)
		if prefix == "" {
			prefix = "OPENAI_<ACCOUNT>_"
		}
		return openaiRuntimeContext{}, fmt.Errorf("openai api key not found (set --api-key, %sAPI_KEY, or OPENAI_API_KEY)", prefix)
	}
	adminAPIKey, adminSource := resolveOpenAIAdminAPIKey(alias, account, strings.TrimSpace(input.AdminAPIKeyFlag))
	orgID, orgSource := resolveOpenAIOrgID(alias, account, settings, strings.TrimSpace(input.OrgIDFlag))
	projectID, projectSource := resolveOpenAIProjectID(alias, account, settings, strings.TrimSpace(input.ProjectIDFlag))
	logPath := resolveOpenAILogPath(settings)
	source := strings.Join(nonEmpty(apiKeySource, adminSource, orgSource, projectSource), ",")
	return openaiRuntimeContext{
		AccountAlias: alias,
		BaseURL:      strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIKey:       strings.TrimSpace(apiKey),
		AdminAPIKey:  strings.TrimSpace(adminAPIKey),
		OrgID:        strings.TrimSpace(orgID),
		ProjectID:    strings.TrimSpace(projectID),
		Source:       source,
		LogPath:      logPath,
	}, nil
}

func resolveOpenAIAccountSelection(settings Settings, accountFlag string) (string, OpenAIAccountEntry) {
	selected := strings.TrimSpace(accountFlag)
	if selected == "" {
		selected = strings.TrimSpace(settings.OpenAI.DefaultAccount)
	}
	if selected == "" {
		selected = strings.TrimSpace(os.Getenv("OPENAI_DEFAULT_ACCOUNT"))
	}
	if selected == "" {
		aliases := openaiAccountAliases(settings)
		if len(aliases) == 1 {
			selected = aliases[0]
		}
	}
	if selected == "" {
		return "", OpenAIAccountEntry{}
	}
	if entry, ok := settings.OpenAI.Accounts[selected]; ok {
		return selected, entry
	}
	return selected, OpenAIAccountEntry{}
}

func openaiAccountAliases(settings Settings) []string {
	if len(settings.OpenAI.Accounts) == 0 {
		return nil
	}
	out := make([]string, 0, len(settings.OpenAI.Accounts))
	for alias := range settings.OpenAI.Accounts {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

func openaiAccountEnvPrefix(alias string, account OpenAIAccountEntry) string {
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
	return "OPENAI_" + alias + "_"
}

func resolveOpenAIEnv(alias string, account OpenAIAccountEntry, key string) string {
	prefix := openaiAccountEnvPrefix(alias, account)
	if prefix != "" {
		if value := strings.TrimSpace(os.Getenv(prefix + key)); value != "" {
			return value
		}
	}
	return ""
}

func resolveOpenAIAPIKey(alias string, account OpenAIAccountEntry, override string) (string, string) {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override), "flag:--api-key"
	}
	if ref := strings.TrimSpace(account.APIKeyEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveOpenAIEnv(alias, account, "API_KEY")); value != "" {
		return value, "env:" + openaiAccountEnvPrefix(alias, account) + "API_KEY"
	}
	if value := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); value != "" {
		return value, "env:OPENAI_API_KEY"
	}
	return "", ""
}

func resolveOpenAIAdminAPIKey(alias string, account OpenAIAccountEntry, override string) (string, string) {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override), "flag:--admin-api-key"
	}
	if ref := strings.TrimSpace(account.AdminAPIKeyEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveOpenAIEnv(alias, account, "ADMIN_API_KEY")); value != "" {
		return value, "env:" + openaiAccountEnvPrefix(alias, account) + "ADMIN_API_KEY"
	}
	if value := strings.TrimSpace(os.Getenv("OPENAI_ADMIN_API_KEY")); value != "" {
		return value, "env:OPENAI_ADMIN_API_KEY"
	}
	if value := strings.TrimSpace(os.Getenv("OPENAI_ADMIN_KEY")); value != "" {
		return value, "env:OPENAI_ADMIN_KEY"
	}
	return "", ""
}

func resolveOpenAIOrgID(alias string, account OpenAIAccountEntry, settings Settings, override string) (string, string) {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override), "flag:--org-id"
	}
	if value := strings.TrimSpace(account.OrganizationID); value != "" {
		return value, "settings.organization_id"
	}
	if ref := strings.TrimSpace(account.OrganizationIDEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveOpenAIEnv(alias, account, "ORG_ID")); value != "" {
		return value, "env:" + openaiAccountEnvPrefix(alias, account) + "ORG_ID"
	}
	if value := strings.TrimSpace(settings.OpenAI.DefaultOrganizationID); value != "" {
		return value, "settings.default_organization_id"
	}
	if value := strings.TrimSpace(os.Getenv("OPENAI_ORG_ID")); value != "" {
		return value, "env:OPENAI_ORG_ID"
	}
	if value := strings.TrimSpace(os.Getenv("OPENAI_ORGANIZATION")); value != "" {
		return value, "env:OPENAI_ORGANIZATION"
	}
	return "", ""
}

func resolveOpenAIProjectID(alias string, account OpenAIAccountEntry, settings Settings, override string) (string, string) {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override), "flag:--project-id"
	}
	if value := strings.TrimSpace(account.ProjectID); value != "" {
		return value, "settings.project_id"
	}
	if ref := strings.TrimSpace(account.ProjectIDEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveOpenAIEnv(alias, account, "PROJECT_ID")); value != "" {
		return value, "env:" + openaiAccountEnvPrefix(alias, account) + "PROJECT_ID"
	}
	if value := strings.TrimSpace(settings.OpenAI.DefaultProjectID); value != "" {
		return value, "settings.default_project_id"
	}
	if value := strings.TrimSpace(os.Getenv("OPENAI_PROJECT_ID")); value != "" {
		return value, "env:OPENAI_PROJECT_ID"
	}
	return "", ""
}

func resolveOpenAILogPath(settings Settings) string {
	if value := strings.TrimSpace(os.Getenv("SI_OPENAI_LOG_FILE")); value != "" {
		return value
	}
	if value := strings.TrimSpace(settings.OpenAI.LogFile); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".si", "logs", "openai.log")
}

func formatOpenAIContext(runtime openaiRuntimeContext) string {
	account := strings.TrimSpace(runtime.AccountAlias)
	if account == "" {
		account = "(default)"
	}
	org := strings.TrimSpace(runtime.OrgID)
	if org == "" {
		org = "-"
	}
	project := strings.TrimSpace(runtime.ProjectID)
	if project == "" {
		project = "-"
	}
	return fmt.Sprintf("account=%s org=%s project=%s base=%s", account, org, project, runtime.BaseURL)
}

func previewOpenAISecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	if len(value) <= 8 {
		return strings.Repeat("*", len(value))
	}
	return value[:4] + strings.Repeat("*", len(value)-8) + value[len(value)-4:]
}

func parseOpenAISimpleParams(entries []string) map[string]string {
	out := map[string]string{}
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(parts[1])
	}
	return out
}

func parseOpenAIParamsToValues(entries []string) url.Values {
	out := url.Values{}
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if key == "" {
			continue
		}
		out.Add(key, strings.TrimSpace(parts[1]))
	}
	return out
}

func openaiResolveJSONBody(body string, bodyFile string, fallback any) (string, error) {
	body = strings.TrimSpace(body)
	bodyFile = strings.TrimSpace(bodyFile)
	if body != "" && bodyFile != "" {
		return "", fmt.Errorf("--body and --body-file cannot both be set")
	}
	if bodyFile != "" {
		raw, err := os.ReadFile(bodyFile)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(raw)), nil
	}
	if body != "" {
		return body, nil
	}
	if fallback == nil {
		return "", nil
	}
	raw, err := json.Marshal(fallback)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func openaiDo(ctx context.Context, runtime openaiRuntimeContext, req openaiRequest) (openaiResponse, error) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		return openaiResponse{}, fmt.Errorf("request path is required")
	}
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
	}
	endpoint, err := resolveOpenAIURL(runtime.BaseURL, path, req.Params)
	if err != nil {
		return openaiResponse{}, err
	}
	providerID := providers.OpenAI
	token, tokenSource, err := resolveOpenAIAuthToken(runtime, req.UseAdminKey)
	if err != nil {
		return openaiResponse{}, err
	}

	return integrationruntime.DoHTTP(ctx, integrationruntime.HTTPExecutorOptions[openaiResponse]{
		Provider:    providerID,
		Subject:     runtime.AccountAlias,
		Method:      method,
		RequestPath: path,
		Endpoint:    endpoint,
		MaxRetries:  2,
		Client:      httpx.SharedClient(45 * time.Second),
		BuildRequest: func(callCtx context.Context, callMethod string, callEndpoint string) (*http.Request, error) {
			bodyReader := io.Reader(nil)
			if strings.TrimSpace(req.RawBody) != "" {
				bodyReader = strings.NewReader(req.RawBody)
			} else if req.JSONBody != nil {
				rawBody, marshalErr := json.Marshal(req.JSONBody)
				if marshalErr != nil {
					return nil, marshalErr
				}
				bodyReader = bytes.NewReader(rawBody)
			}
			httpReq, reqErr := http.NewRequestWithContext(callCtx, callMethod, callEndpoint, bodyReader)
			if reqErr != nil {
				return nil, reqErr
			}
			spec := providers.Resolve(providerID)
			accept := strings.TrimSpace(spec.Accept)
			if accept == "" {
				accept = "application/json"
			}
			httpReq.Header.Set("Accept", accept)
			userAgent := strings.TrimSpace(spec.UserAgent)
			if userAgent == "" {
				userAgent = "si-openai/1.0"
			}
			httpReq.Header.Set("User-Agent", userAgent)
			for key, value := range spec.DefaultHeaders {
				key = strings.TrimSpace(key)
				if key == "" {
					continue
				}
				httpReq.Header.Set(key, strings.TrimSpace(value))
			}
			for key, value := range req.Headers {
				key = strings.TrimSpace(key)
				if key == "" {
					continue
				}
				httpReq.Header.Set(key, strings.TrimSpace(value))
			}
			httpReq.Header.Set("Authorization", "Bearer "+token)
			if value := strings.TrimSpace(runtime.OrgID); value != "" {
				httpReq.Header.Set("OpenAI-Organization", value)
			}
			if value := strings.TrimSpace(runtime.ProjectID); value != "" {
				httpReq.Header.Set("OpenAI-Project", value)
			}
			if bodyReader != nil {
				contentType := strings.TrimSpace(req.ContentType)
				if contentType == "" {
					contentType = "application/json"
				}
				httpReq.Header.Set("Content-Type", contentType)
			}
			return httpReq, nil
		},
		NormalizeResponse: normalizeOpenAIResponse,
		StatusCode: func(resp openaiResponse) int {
			return resp.StatusCode
		},
		NormalizeHTTPError: normalizeOpenAIHTTPError,
		IsRetryableNetwork: func(method string, _ error) bool {
			return netpolicy.IsSafeMethod(method)
		},
		IsRetryableHTTP: func(method string, statusCode int, _ http.Header, _ string) bool {
			if !netpolicy.IsSafeMethod(method) {
				return false
			}
			switch statusCode {
			case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
				return true
			}
			return statusCode >= 500
		},
		OnCacheHit: func(resp openaiResponse) {
			openaiLogEvent(runtime.LogPath, map[string]any{
				"event":       "cache_hit",
				"account":     runtime.AccountAlias,
				"method":      method,
				"path":        sanitizeURL(endpoint),
				"status_code": resp.StatusCode,
			})
		},
		OnResponse: func(_ int, resp openaiResponse, _ http.Header, duration time.Duration) {
			openaiLogEvent(runtime.LogPath, map[string]any{
				"event":         "response",
				"account":       runtime.AccountAlias,
				"method":        method,
				"path":          sanitizeURL(endpoint),
				"status_code":   resp.StatusCode,
				"request_id":    resp.RequestID,
				"duration_ms":   duration.Milliseconds(),
				"auth_source":   tokenSource,
				"auth_is_admin": req.UseAdminKey,
			})
		},
		OnError: func(_ int, callErr error, duration time.Duration) {
			openaiLogEvent(runtime.LogPath, map[string]any{
				"event":         "error",
				"account":       runtime.AccountAlias,
				"method":        method,
				"path":          sanitizeURL(endpoint),
				"duration_ms":   duration.Milliseconds(),
				"error":         redactOpenAISensitive(callErr.Error()),
				"auth_source":   tokenSource,
				"auth_is_admin": req.UseAdminKey,
			})
		},
	})
}

func resolveOpenAIAuthToken(runtime openaiRuntimeContext, preferAdmin bool) (string, string, error) {
	if preferAdmin {
		if value := strings.TrimSpace(runtime.AdminAPIKey); value != "" {
			return value, "admin_api_key", nil
		}
		return "", "", fmt.Errorf("admin api key is required for this command (set --admin-api-key or OPENAI_ADMIN_API_KEY)")
	}
	if value := strings.TrimSpace(runtime.APIKey); value != "" {
		return value, "api_key", nil
	}
	if value := strings.TrimSpace(runtime.AdminAPIKey); value != "" {
		return value, "admin_api_key", nil
	}
	return "", "", fmt.Errorf("api key is required")
}

func resolveOpenAIURL(baseURL string, path string, params url.Values) (string, error) {
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		u, err := url.Parse(path)
		if err != nil {
			return "", err
		}
		q := u.Query()
		for key, values := range params {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			for _, value := range values {
				q.Add(key, strings.TrimSpace(value))
			}
		}
		u.RawQuery = q.Encode()
		return strings.TrimSpace(u.String()), nil
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	if !base.IsAbs() {
		return "", fmt.Errorf("base url must be absolute: %q", baseURL)
	}
	ref := &url.URL{Path: path}
	full := base.ResolveReference(ref)
	q := full.Query()
	for key, values := range params {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		for _, value := range values {
			q.Add(key, strings.TrimSpace(value))
		}
	}
	full.RawQuery = q.Encode()
	return strings.TrimSpace(full.String()), nil
}

func normalizeOpenAIResponse(httpResp *http.Response, body string) openaiResponse {
	resp := openaiResponse{
		StatusCode: httpResp.StatusCode,
		Status:     strings.TrimSpace(httpResp.Status),
		RequestID:  firstOpenAIHeader(httpResp.Header, "x-request-id", "openai-processing-ms"),
		Headers:    map[string]string{},
		Body:       strings.TrimSpace(body),
	}
	for key, values := range httpResp.Header {
		if len(values) == 0 {
			continue
		}
		resp.Headers[key] = strings.Join(values, ",")
	}
	if resp.Body == "" {
		return resp
	}
	var parsed any
	if err := json.Unmarshal([]byte(resp.Body), &parsed); err != nil {
		return resp
	}
	switch payload := parsed.(type) {
	case map[string]any:
		resp.Data = payload
		if list, ok := payload["data"].([]any); ok {
			resp.List = convertAnyListToMapList(list)
		}
		if len(resp.List) == 0 {
			if list, ok := payload["results"].([]any); ok {
				resp.List = convertAnyListToMapList(list)
			}
		}
	case []any:
		resp.List = convertAnyListToMapList(payload)
	}
	return resp
}

func normalizeOpenAIHTTPError(statusCode int, headers http.Header, body string) error {
	details := &openaiAPIErrorDetails{
		StatusCode: statusCode,
		RequestID:  firstOpenAIHeader(headers, "x-request-id"),
		RawBody:    strings.TrimSpace(body),
		Message:    "openai request failed",
	}
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		details.Message = http.StatusText(statusCode)
		return details
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		details.Message = trimmed
		return details
	}
	candidate := parsed
	if nested, ok := parsed["error"].(map[string]any); ok {
		candidate = nested
	}
	if value := strings.TrimSpace(stringifyOpenAIAny(candidate["code"])); value != "" {
		details.Code = value
	}
	if value := strings.TrimSpace(stringifyOpenAIAny(candidate["type"])); value != "" {
		details.Type = value
	}
	if value := strings.TrimSpace(stringifyOpenAIAny(candidate["param"])); value != "" {
		details.Param = value
	}
	if value := strings.TrimSpace(stringifyOpenAIAny(candidate["message"])); value != "" {
		details.Message = value
	}
	if details.Message == "openai request failed" {
		details.Message = trimmed
	}
	return details
}

func firstOpenAIHeader(headers http.Header, keys ...string) string {
	if headers == nil {
		return ""
	}
	for _, key := range keys {
		value := strings.TrimSpace(headers.Get(strings.TrimSpace(key)))
		if value != "" {
			return value
		}
	}
	return ""
}

func stringifyOpenAIAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case fmt.Stringer:
		return typed.String()
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		if value == nil {
			return ""
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return ""
		}
		return string(raw)
	}
}

func printOpenAIResponse(resp openaiResponse, jsonOut bool, raw bool) {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(resp); err != nil {
			fatal(err)
		}
		return
	}
	if raw {
		fmt.Println(resp.Body)
		return
	}
	fmt.Printf("%s %d %s\n", styleHeading("Status:"), resp.StatusCode, orDash(resp.Status))
	if resp.RequestID != "" {
		fmt.Printf("%s %s\n", styleHeading("Request ID:"), resp.RequestID)
	}
	if len(resp.Data) > 0 {
		pretty, err := json.MarshalIndent(resp.Data, "", "  ")
		if err == nil {
			fmt.Println(string(pretty))
			return
		}
	}
	if strings.TrimSpace(resp.Body) != "" {
		fmt.Println(resp.Body)
	}
}

func printOpenAIError(err error) {
	if err == nil {
		return
	}
	var details *openaiAPIErrorDetails
	if errors.As(err, &details) {
		fmt.Printf("%s %s\n", styleHeading("OpenAI error:"), styleError(details.Error()))
		if details.RequestID != "" {
			fmt.Printf("%s %s\n", styleHeading("Request ID:"), details.RequestID)
		}
		if details.RawBody != "" {
			fmt.Printf("%s %s\n", styleHeading("Body:"), truncateString(details.RawBody, 800))
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("OpenAI error:"), styleError(err.Error()))
}

func runOpenAIRequest(flags openaiCommonFlags, req openaiRequest, jsonOut bool, raw bool) {
	runtime, err := resolveRuntimeFromOpenAIFlags(flags)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := openaiDo(ctx, runtime, req)
	if err != nil {
		printOpenAIError(err)
		return
	}
	printOpenAIResponse(resp, jsonOut, raw)
}

func appendOpenAIFlagsFromCommon(args *[]string, flags openaiCommonFlags) {
	if flags.account != nil && strings.TrimSpace(*flags.account) != "" {
		*args = append(*args, "--account", strings.TrimSpace(*flags.account))
	}
	if flags.baseURL != nil && strings.TrimSpace(*flags.baseURL) != "" {
		*args = append(*args, "--base-url", strings.TrimSpace(*flags.baseURL))
	}
	if flags.apiKey != nil && strings.TrimSpace(*flags.apiKey) != "" {
		*args = append(*args, "--api-key", strings.TrimSpace(*flags.apiKey))
	}
	if flags.adminAPIKey != nil && strings.TrimSpace(*flags.adminAPIKey) != "" {
		*args = append(*args, "--admin-api-key", strings.TrimSpace(*flags.adminAPIKey))
	}
	if flags.orgID != nil && strings.TrimSpace(*flags.orgID) != "" {
		*args = append(*args, "--org-id", strings.TrimSpace(*flags.orgID))
	}
	if flags.projectID != nil && strings.TrimSpace(*flags.projectID) != "" {
		*args = append(*args, "--project-id", strings.TrimSpace(*flags.projectID))
	}
}

func openaiLogEvent(path string, event map[string]any) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	if event == nil {
		event = map[string]any{}
	}
	if _, ok := event["ts"]; !ok {
		event["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	for key, value := range event {
		if asString, ok := value.(string); ok {
			event[key] = redactOpenAISensitive(asString)
		}
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = file.Write(raw)
	_ = file.Close()
}

func redactOpenAISensitive(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	replacements := []struct {
		needle string
		repl   string
	}{
		{"Authorization: Bearer ", "Authorization: Bearer ***"},
		{"api_key=", "api_key=***"},
		{"admin_api_key=", "admin_api_key=***"},
	}
	masked := value
	for _, item := range replacements {
		if strings.Contains(strings.ToLower(masked), strings.ToLower(item.needle)) {
			masked = maskAfterToken(masked, item.needle, item.repl)
		}
	}
	return masked
}

func requireConfirmation(action string, force bool) error {
	if force {
		return nil
	}
	confirmed, ok := confirmYN("Confirm "+strings.TrimSpace(action)+"?", false)
	if !ok {
		return fmt.Errorf("operation canceled")
	}
	if !confirmed {
		return fmt.Errorf("operation canceled")
	}
	return nil
}
