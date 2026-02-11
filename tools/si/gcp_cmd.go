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
	"strings"
	"time"

	"si/tools/si/internal/httpx"
	"si/tools/si/internal/integrationruntime"
	"si/tools/si/internal/netpolicy"
	"si/tools/si/internal/providers"
)

const gcpUsageText = "usage: si gcp <auth|context|doctor|service|iam|apikey|gemini|generativelanguage|vertex|ai|raw>"

type gcpRuntimeContext struct {
	AccountAlias string
	Environment  string
	ProjectID    string
	BaseURL      string
	AccessToken  string
	Source       string
	LogPath      string
}

type gcpRuntimeContextInput struct {
	AccountFlag    string
	EnvFlag        string
	ProjectFlag    string
	BaseURLFlag    string
	TokenFlag      string
	RequireToken   bool
	RequireProject bool
}

type gcpResponse struct {
	StatusCode int               `json:"status_code"`
	Status     string            `json:"status"`
	RequestID  string            `json:"request_id,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	Data       map[string]any    `json:"data,omitempty"`
	List       []map[string]any  `json:"list,omitempty"`
}

type gcpAPIErrorDetails struct {
	StatusCode int    `json:"status_code,omitempty"`
	Code       int    `json:"code,omitempty"`
	Status     string `json:"status,omitempty"`
	Message    string `json:"message,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	RawBody    string `json:"raw_body,omitempty"`
}

func (e *gcpAPIErrorDetails) Error() string {
	if e == nil {
		return "gcp api error"
	}
	parts := make([]string, 0, 6)
	if e.StatusCode > 0 {
		parts = append(parts, fmt.Sprintf("status_code=%d", e.StatusCode))
	}
	if e.Code > 0 {
		parts = append(parts, fmt.Sprintf("code=%d", e.Code))
	}
	if strings.TrimSpace(e.Status) != "" {
		parts = append(parts, "status="+e.Status)
	}
	if strings.TrimSpace(e.Message) != "" {
		parts = append(parts, "message="+e.Message)
	}
	if strings.TrimSpace(e.RequestID) != "" {
		parts = append(parts, "request_id="+e.RequestID)
	}
	if len(parts) == 0 {
		return "gcp api error"
	}
	return "gcp api error: " + strings.Join(parts, ", ")
}

func cmdGCP(args []string) {
	if len(args) == 0 {
		printUsage(gcpUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(gcpUsageText)
	case "auth":
		cmdGCPAuth(rest)
	case "context":
		cmdGCPContext(rest)
	case "doctor":
		cmdGCPDoctor(rest)
	case "service", "services":
		cmdGCPService(rest)
	case "iam":
		cmdGCPIAM(rest)
	case "apikey", "api-key", "apikeys":
		cmdGCPAPIKey(rest)
	case "gemini", "generativelanguage":
		cmdGCPGemini(rest)
	case "vertex":
		cmdGCPVertex(rest)
	case "ai":
		cmdGCPAI(rest)
	case "raw":
		cmdGCPRaw(rest)
	default:
		printUnknown("gcp", sub)
		printUsage(gcpUsageText)
	}
}

func cmdGCPAuth(args []string) {
	if len(args) == 0 {
		printUsage("usage: si gcp auth status [--account <alias>] [--env <prod|staging|dev>] [--project <project>] [--json]")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "status":
		cmdGCPAuthStatus(args[1:])
	default:
		printUnknown("gcp auth", sub)
	}
}

func cmdGCPAuthStatus(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("gcp auth status", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	service := fs.String("service", "serviceusage.googleapis.com", "service name to probe")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si gcp auth status [--account <alias>] [--env <prod|staging|dev>] [--project <project>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlags(flags, true)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	serviceName := strings.TrimSpace(*service)
	path := "/v1/projects/" + url.PathEscape(runtime.ProjectID) + "/services/" + url.PathEscape(serviceName)
	resp, verifyErr := gcpDo(ctx, runtime, gcpRequest{Method: http.MethodGet, Path: path})
	status := "error"
	if verifyErr == nil {
		status = "ready"
	}
	payload := map[string]any{
		"status":        status,
		"account_alias": runtime.AccountAlias,
		"environment":   runtime.Environment,
		"project_id":    runtime.ProjectID,
		"base_url":      runtime.BaseURL,
		"source":        runtime.Source,
		"token_preview": previewGCPSecret(runtime.AccessToken),
	}
	if verifyErr == nil {
		payload["verify_status"] = resp.StatusCode
		payload["verify"] = resp.Data
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
		fmt.Printf("%s %s\n", styleHeading("GCP auth:"), styleError("error"))
		fmt.Printf("%s %s\n", styleHeading("Context:"), formatGCPContext(runtime))
		printGCPError(verifyErr)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("GCP auth:"), styleSuccess("ready"))
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatGCPContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
	fmt.Printf("%s %s\n", styleHeading("Token preview:"), previewGCPSecret(runtime.AccessToken))
}

func cmdGCPContext(args []string) {
	if len(args) == 0 {
		printUsage("usage: si gcp context <list|current|use>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGCPContextList(rest)
	case "current":
		cmdGCPContextCurrent(rest)
	case "use":
		cmdGCPContextUse(rest)
	default:
		printUnknown("gcp context", sub)
	}
}

func cmdGCPContextList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("gcp context list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si gcp context list [--json]")
		return
	}
	settings := loadSettingsOrDefault()
	aliases := gcpAccountAliases(settings)
	rows := make([]map[string]string, 0, len(aliases))
	for _, alias := range aliases {
		entry := settings.GCP.Accounts[alias]
		rows = append(rows, map[string]string{
			"alias":          alias,
			"name":           strings.TrimSpace(entry.Name),
			"default":        boolString(alias == strings.TrimSpace(settings.GCP.DefaultAccount)),
			"project_id":     strings.TrimSpace(entry.ProjectID),
			"project_id_env": strings.TrimSpace(entry.ProjectIDEnv),
			"token_env":      strings.TrimSpace(entry.AccessTokenEnv),
			"api_key_env":    strings.TrimSpace(entry.APIKeyEnv),
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
		infof("no gcp accounts configured in settings")
		return
	}
	fmt.Printf("%s %s %s %s %s %s %s\n",
		padRightANSI(styleHeading("ALIAS"), 18),
		padRightANSI(styleHeading("DEFAULT"), 8),
		padRightANSI(styleHeading("PROJECT ID"), 18),
		padRightANSI(styleHeading("PROJECT ENV"), 24),
		padRightANSI(styleHeading("TOKEN ENV"), 24),
		padRightANSI(styleHeading("API KEY ENV"), 24),
		styleHeading("NAME"),
	)
	for _, row := range rows {
		fmt.Printf("%s %s %s %s %s %s %s\n",
			padRightANSI(orDash(row["alias"]), 18),
			padRightANSI(orDash(row["default"]), 8),
			padRightANSI(orDash(row["project_id"]), 18),
			padRightANSI(orDash(row["project_id_env"]), 24),
			padRightANSI(orDash(row["token_env"]), 24),
			padRightANSI(orDash(row["api_key_env"]), 24),
			orDash(row["name"]),
		)
	}
}

func cmdGCPContextCurrent(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("gcp context current", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si gcp context current [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlags(flags, false)
	if err != nil {
		fatal(err)
	}
	payload := map[string]any{
		"account_alias": runtime.AccountAlias,
		"environment":   runtime.Environment,
		"project_id":    runtime.ProjectID,
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
	fmt.Printf("%s %s\n", styleHeading("Current gcp context:"), formatGCPContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
}

func cmdGCPContextUse(args []string) {
	fs := flag.NewFlagSet("gcp context use", flag.ExitOnError)
	account := fs.String("account", "", "default account alias")
	env := fs.String("env", "", "default environment (prod|staging|dev)")
	project := fs.String("project", "", "default gcp project id")
	baseURL := fs.String("base-url", "", "serviceusage api base url")
	projectEnv := fs.String("project-env", "", "project id env-var reference")
	tokenEnv := fs.String("token-env", "", "access token env-var reference")
	apiKeyEnv := fs.String("api-key-env", "", "api key env-var reference (for gemini api-key mode)")
	vaultPrefix := fs.String("vault-prefix", "", "account env prefix")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si gcp context use [--account <alias>] [--env <prod|staging|dev>] [--project <id>] [--base-url <url>] [--project-env <env>] [--token-env <env>] [--api-key-env <env>] [--vault-prefix <prefix>]")
		return
	}
	settings := loadSettingsOrDefault()
	if value := strings.TrimSpace(*account); value != "" {
		settings.GCP.DefaultAccount = value
	}
	if value := strings.TrimSpace(*env); value != "" {
		normalized := normalizeIntegrationEnvironment(value)
		if normalized == "" {
			fatal(fmt.Errorf("invalid environment %q (expected prod|staging|dev)", value))
		}
		settings.GCP.DefaultEnv = normalized
	}
	if value := strings.TrimSpace(*baseURL); value != "" {
		settings.GCP.APIBaseURL = value
	}
	targetAlias := strings.TrimSpace(settings.GCP.DefaultAccount)
	if value := strings.TrimSpace(*account); value != "" {
		targetAlias = value
	}
	if targetAlias == "" && (strings.TrimSpace(*project) != "" || strings.TrimSpace(*projectEnv) != "" || strings.TrimSpace(*tokenEnv) != "" || strings.TrimSpace(*apiKeyEnv) != "" || strings.TrimSpace(*vaultPrefix) != "") {
		targetAlias = "default"
		settings.GCP.DefaultAccount = targetAlias
	}
	if targetAlias != "" {
		if settings.GCP.Accounts == nil {
			settings.GCP.Accounts = map[string]GCPAccountEntry{}
		}
		entry := settings.GCP.Accounts[targetAlias]
		if value := strings.TrimSpace(*project); value != "" {
			entry.ProjectID = value
		}
		if value := strings.TrimSpace(*projectEnv); value != "" {
			entry.ProjectIDEnv = value
		}
		if value := strings.TrimSpace(*tokenEnv); value != "" {
			entry.AccessTokenEnv = value
		}
		if value := strings.TrimSpace(*apiKeyEnv); value != "" {
			entry.APIKeyEnv = value
		}
		if value := strings.TrimSpace(*vaultPrefix); value != "" {
			entry.VaultPrefix = value
		}
		settings.GCP.Accounts[targetAlias] = entry
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	fmt.Printf("%s default_account=%s env=%s project=%s base=%s\n",
		styleHeading("Updated gcp context:"),
		orDash(settings.GCP.DefaultAccount),
		orDash(settings.GCP.DefaultEnv),
		orDash(firstNonEmpty(*project, settings.GCP.Accounts[settings.GCP.DefaultAccount].ProjectID)),
		orDash(settings.GCP.APIBaseURL),
	)
}

func cmdGCPDoctor(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "public": true})
	fs := flag.NewFlagSet("gcp doctor", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	publicProbe := fs.Bool("public", false, "run unauthenticated public probe")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si gcp doctor [--account <alias>] [--env <prod|staging|dev>] [--project <project>] [--public] [--json]")
		return
	}
	if *publicProbe {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err := runPublicProviderDoctor(ctx, providers.GCPServiceUsage, strings.TrimSpace(*flags.baseURL))
		if err != nil {
			fatal(err)
		}
		printPublicDoctorResult("gcp", result, *jsonOut)
		return
	}
	runtime, err := resolveRuntimeFromGCPFlags(flags, true)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	path := "/v1/projects/" + url.PathEscape(runtime.ProjectID) + "/services/serviceusage.googleapis.com"
	_, verifyErr := gcpDo(ctx, runtime, gcpRequest{Method: http.MethodGet, Path: path})
	checks := []doctorCheck{
		{Name: "project", OK: strings.TrimSpace(runtime.ProjectID) != "", Detail: orDash(runtime.ProjectID)},
		{Name: "token", OK: strings.TrimSpace(runtime.AccessToken) != "", Detail: previewGCPSecret(runtime.AccessToken)},
		{Name: "request", OK: verifyErr == nil, Detail: errorOrOK(verifyErr)},
	}
	ok := true
	for _, check := range checks {
		if !check.OK {
			ok = false
		}
	}
	payload := map[string]any{
		"ok":            ok,
		"provider":      "gcp_serviceusage",
		"base_url":      runtime.BaseURL,
		"account_alias": runtime.AccountAlias,
		"environment":   runtime.Environment,
		"project_id":    runtime.ProjectID,
		"checks":        checks,
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
		fmt.Printf("%s %s\n", styleHeading("GCP doctor:"), styleSuccess("ok"))
	} else {
		fmt.Printf("%s %s\n", styleHeading("GCP doctor:"), styleError("issues found"))
	}
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatGCPContext(runtime))
	for _, check := range checks {
		icon := styleSuccess("OK")
		if !check.OK {
			icon = styleError("ERR")
		}
		fmt.Printf("  %s %s %s\n", padRightANSI(icon, 4), padRightANSI(check.Name, 14), strings.TrimSpace(check.Detail))
	}
	if !ok {
		os.Exit(1)
	}
}

func cmdGCPService(args []string) {
	if len(args) == 0 {
		printUsage("usage: si gcp service <enable|disable|get|list>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "enable":
		cmdGCPServiceEnable(rest)
	case "disable":
		cmdGCPServiceDisable(rest)
	case "get":
		cmdGCPServiceGet(rest)
	case "list":
		cmdGCPServiceList(rest)
	default:
		printUnknown("gcp service", sub)
	}
}

func cmdGCPServiceEnable(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp service enable", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	service := fs.String("name", "", "service name (for example generativelanguage.googleapis.com)")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*service) == "" {
		printUsage("usage: si gcp service enable --name <service.googleapis.com> [--project <id>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlags(flags, true)
	if err != nil {
		fatal(err)
	}
	path := "/v1/projects/" + url.PathEscape(runtime.ProjectID) + "/services/" + url.PathEscape(strings.TrimSpace(*service)) + ":enable"
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := gcpDo(ctx, runtime, gcpRequest{Method: http.MethodPost, Path: path, JSONBody: map[string]any{}})
	if err != nil {
		printGCPError(err)
		return
	}
	printGCPResponse(resp, *jsonOut, *raw)
}

func cmdGCPServiceDisable(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp service disable", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	service := fs.String("name", "", "service name (for example generativelanguage.googleapis.com)")
	checkUsage := fs.Bool("check-usage", false, "if true, disable request fails when service has usage")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*service) == "" {
		printUsage("usage: si gcp service disable --name <service.googleapis.com> [--check-usage] [--project <id>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlags(flags, true)
	if err != nil {
		fatal(err)
	}
	path := "/v1/projects/" + url.PathEscape(runtime.ProjectID) + "/services/" + url.PathEscape(strings.TrimSpace(*service)) + ":disable"
	body := map[string]any{"checkIfServiceHasUsage": *checkUsage}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := gcpDo(ctx, runtime, gcpRequest{Method: http.MethodPost, Path: path, JSONBody: body})
	if err != nil {
		printGCPError(err)
		return
	}
	printGCPResponse(resp, *jsonOut, *raw)
}

func cmdGCPServiceGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp service get", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	service := fs.String("name", "", "service name")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*service) == "" {
		printUsage("usage: si gcp service get --name <service.googleapis.com> [--project <id>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlags(flags, true)
	if err != nil {
		fatal(err)
	}
	path := "/v1/projects/" + url.PathEscape(runtime.ProjectID) + "/services/" + url.PathEscape(strings.TrimSpace(*service))
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := gcpDo(ctx, runtime, gcpRequest{Method: http.MethodGet, Path: path})
	if err != nil {
		printGCPError(err)
		return
	}
	printGCPResponse(resp, *jsonOut, *raw)
}

func cmdGCPServiceList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp service list", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	limit := fs.Int("limit", 50, "maximum services")
	filter := fs.String("filter", "", "filter expression")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si gcp service list [--limit N] [--filter expr] [--param key=value] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlags(flags, true)
	if err != nil {
		fatal(err)
	}
	query := parseGCPParams(params)
	if *limit > 0 {
		query["pageSize"] = fmt.Sprintf("%d", *limit)
	}
	if value := strings.TrimSpace(*filter); value != "" {
		query["filter"] = value
	}
	path := "/v1/projects/" + url.PathEscape(runtime.ProjectID) + "/services"
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := gcpDo(ctx, runtime, gcpRequest{Method: http.MethodGet, Path: path, Params: query})
	if err != nil {
		printGCPError(err)
		return
	}
	printGCPResponse(resp, *jsonOut, *raw)
}

func cmdGCPRaw(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp raw", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	method := fs.String("method", http.MethodGet, "http method")
	path := fs.String("path", "", "api path")
	body := fs.String("body", "", "raw request body")
	jsonBody := fs.String("json-body", "", "json request body")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	headers := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	fs.Var(&headers, "header", "header key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*path) == "" {
		printUsage("usage: si gcp raw --method <GET|POST|PATCH|DELETE> --path <api-path> [--param key=value] [--body raw|--json-body '{...}'] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlags(flags, true)
	if err != nil {
		fatal(err)
	}
	var payload any
	if strings.TrimSpace(*jsonBody) != "" {
		payload = parseGCPJSONBody(strings.TrimSpace(*jsonBody), nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := gcpDo(ctx, runtime, gcpRequest{
		Method:   strings.ToUpper(strings.TrimSpace(*method)),
		Path:     strings.TrimSpace(*path),
		Params:   parseGCPParams(params),
		Headers:  parseGCPParams(headers),
		RawBody:  strings.TrimSpace(*body),
		JSONBody: payload,
	})
	if err != nil {
		printGCPError(err)
		return
	}
	printGCPResponse(resp, *jsonOut, *raw)
}

type gcpCommonFlags struct {
	account *string
	env     *string
	project *string
	token   *string
	baseURL *string
}

func bindGCPCommonFlags(fs *flag.FlagSet) gcpCommonFlags {
	return gcpCommonFlags{
		account: fs.String("account", "", "account alias"),
		env:     fs.String("env", "", "environment (prod|staging|dev)"),
		project: fs.String("project", "", "gcp project id"),
		token:   fs.String("access-token", "", "override oauth access token"),
		baseURL: fs.String("base-url", "", "api base url"),
	}
}

func resolveRuntimeFromGCPFlags(flags gcpCommonFlags, requireToken bool) (gcpRuntimeContext, error) {
	return resolveRuntimeFromGCPFlagsWithBase(flags, requireToken, "", true)
}

func resolveRuntimeFromGCPFlagsWithBase(flags gcpCommonFlags, requireToken bool, defaultBaseURL string, requireProject bool) (gcpRuntimeContext, error) {
	base := strings.TrimSpace(valueOrEmpty(flags.baseURL))
	if base == "" {
		base = strings.TrimSpace(defaultBaseURL)
	}
	return resolveGCPRuntimeContext(gcpRuntimeContextInput{
		AccountFlag:    strings.TrimSpace(valueOrEmpty(flags.account)),
		EnvFlag:        strings.TrimSpace(valueOrEmpty(flags.env)),
		ProjectFlag:    strings.TrimSpace(valueOrEmpty(flags.project)),
		BaseURLFlag:    base,
		TokenFlag:      strings.TrimSpace(valueOrEmpty(flags.token)),
		RequireToken:   requireToken,
		RequireProject: requireProject,
	})
}

func resolveGCPRuntimeContext(input gcpRuntimeContextInput) (gcpRuntimeContext, error) {
	settings := loadSettingsOrDefault()
	alias, account := resolveGCPAccountSelection(settings, input.AccountFlag)
	env := strings.TrimSpace(input.EnvFlag)
	if env == "" {
		env = strings.TrimSpace(settings.GCP.DefaultEnv)
	}
	if env == "" {
		env = strings.TrimSpace(os.Getenv("GCP_DEFAULT_ENV"))
	}
	if env == "" {
		env = "prod"
	}
	env = normalizeIntegrationEnvironment(env)
	if env == "" {
		return gcpRuntimeContext{}, fmt.Errorf("invalid environment %q (expected prod|staging|dev)", input.EnvFlag)
	}

	spec := providers.Resolve(providers.GCPServiceUsage)
	baseURL := strings.TrimSpace(input.BaseURLFlag)
	if baseURL == "" {
		baseURL = strings.TrimSpace(account.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(settings.GCP.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("GCP_API_BASE_URL"))
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(spec.BaseURL)
	}
	if baseURL == "" {
		baseURL = "https://serviceusage.googleapis.com"
	}

	projectID, projectSource := resolveGCPProjectID(alias, account, strings.TrimSpace(input.ProjectFlag))
	if input.RequireProject && projectID == "" {
		return gcpRuntimeContext{}, fmt.Errorf("gcp project id not found (set --project, GCP_PROJECT_ID, or account project settings)")
	}
	token, tokenSource := resolveGCPAccessToken(alias, account, strings.TrimSpace(input.TokenFlag))
	if input.RequireToken && strings.TrimSpace(token) == "" {
		prefix := gcpAccountEnvPrefix(alias, account)
		if prefix == "" {
			prefix = "GCP_<ACCOUNT>_"
		}
		return gcpRuntimeContext{}, fmt.Errorf("gcp access token not found (set --access-token, %sACCESS_TOKEN, or GOOGLE_OAUTH_ACCESS_TOKEN)", prefix)
	}
	source := strings.Join(nonEmpty(projectSource, tokenSource), ",")
	return gcpRuntimeContext{
		AccountAlias: strings.TrimSpace(alias),
		Environment:  env,
		ProjectID:    strings.TrimSpace(projectID),
		BaseURL:      strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		AccessToken:  strings.TrimSpace(token),
		Source:       source,
		LogPath:      resolveGCPLogPath(settings),
	}, nil
}

func resolveGCPAccountSelection(settings Settings, accountFlag string) (string, GCPAccountEntry) {
	selected := strings.TrimSpace(accountFlag)
	if selected == "" {
		selected = strings.TrimSpace(settings.GCP.DefaultAccount)
	}
	if selected == "" {
		selected = strings.TrimSpace(os.Getenv("GCP_DEFAULT_ACCOUNT"))
	}
	if selected == "" {
		aliases := gcpAccountAliases(settings)
		if len(aliases) == 1 {
			selected = aliases[0]
		}
	}
	if selected == "" {
		return "", GCPAccountEntry{}
	}
	if entry, ok := settings.GCP.Accounts[selected]; ok {
		return selected, entry
	}
	return selected, GCPAccountEntry{}
}

func gcpAccountAliases(settings Settings) []string {
	if len(settings.GCP.Accounts) == 0 {
		return nil
	}
	out := make([]string, 0, len(settings.GCP.Accounts))
	for alias := range settings.GCP.Accounts {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

func gcpAccountEnvPrefix(alias string, account GCPAccountEntry) string {
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
	return "GCP_" + alias + "_"
}

func resolveGCPEnv(alias string, account GCPAccountEntry, key string) string {
	prefix := gcpAccountEnvPrefix(alias, account)
	if prefix != "" {
		if value := strings.TrimSpace(os.Getenv(prefix + key)); value != "" {
			return value
		}
	}
	return ""
}

func resolveGCPProjectID(alias string, account GCPAccountEntry, override string) (string, string) {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override), "flag:--project"
	}
	if value := strings.TrimSpace(account.ProjectID); value != "" {
		return value, "settings.project_id"
	}
	if ref := strings.TrimSpace(account.ProjectIDEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveGCPEnv(alias, account, "PROJECT_ID")); value != "" {
		return value, "env:" + gcpAccountEnvPrefix(alias, account) + "PROJECT_ID"
	}
	if value := strings.TrimSpace(os.Getenv("GCP_PROJECT_ID")); value != "" {
		return value, "env:GCP_PROJECT_ID"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT")); value != "" {
		return value, "env:GOOGLE_CLOUD_PROJECT"
	}
	return "", ""
}

func resolveGCPAccessToken(alias string, account GCPAccountEntry, override string) (string, string) {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override), "flag:--access-token"
	}
	if ref := strings.TrimSpace(account.AccessTokenEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveGCPEnv(alias, account, "ACCESS_TOKEN")); value != "" {
		return value, "env:" + gcpAccountEnvPrefix(alias, account) + "ACCESS_TOKEN"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_OAUTH_ACCESS_TOKEN")); value != "" {
		return value, "env:GOOGLE_OAUTH_ACCESS_TOKEN"
	}
	if value := strings.TrimSpace(os.Getenv("GCP_ACCESS_TOKEN")); value != "" {
		return value, "env:GCP_ACCESS_TOKEN"
	}
	return "", ""
}

func resolveGCPLogPath(settings Settings) string {
	if value := strings.TrimSpace(os.Getenv("SI_GCP_LOG_FILE")); value != "" {
		return value
	}
	if value := strings.TrimSpace(settings.GCP.LogFile); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".si", "logs", "gcp-serviceusage.log")
}

func formatGCPContext(runtime gcpRuntimeContext) string {
	account := strings.TrimSpace(runtime.AccountAlias)
	if account == "" {
		account = "(default)"
	}
	return fmt.Sprintf("account=%s env=%s project=%s base=%s", account, runtime.Environment, runtime.ProjectID, runtime.BaseURL)
}

func previewGCPSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	if len(value) <= 6 {
		return strings.Repeat("*", len(value))
	}
	return value[:3] + strings.Repeat("*", len(value)-6) + value[len(value)-3:]
}

type gcpRequest struct {
	Method   string
	Path     string
	Params   map[string]string
	Headers  map[string]string
	RawBody  string
	JSONBody any
}

func gcpDo(ctx context.Context, runtime gcpRuntimeContext, req gcpRequest) (gcpResponse, error) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		return gcpResponse{}, fmt.Errorf("request path is required")
	}
	endpoint, err := resolveGCPURL(runtime.BaseURL, path, req.Params)
	if err != nil {
		return gcpResponse{}, err
	}
	providerID := providers.GCPServiceUsage
	return integrationruntime.DoHTTP(ctx, integrationruntime.HTTPExecutorOptions[gcpResponse]{
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
			httpReq.Header.Set("Accept", firstNonEmpty(spec.Accept, "application/json"))
			httpReq.Header.Set("User-Agent", firstNonEmpty(spec.UserAgent, "si-gcp-serviceusage/1.0"))
			if strings.TrimSpace(runtime.AccessToken) != "" {
				httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(runtime.AccessToken))
			}
			for key, value := range req.Headers {
				key = strings.TrimSpace(key)
				if key == "" {
					continue
				}
				httpReq.Header.Set(key, strings.TrimSpace(value))
			}
			if bodyReader != nil {
				httpReq.Header.Set("Content-Type", "application/json")
			}
			return httpReq, nil
		},
		NormalizeResponse:  normalizeGCPResponse,
		StatusCode:         func(resp gcpResponse) int { return resp.StatusCode },
		NormalizeHTTPError: normalizeGCPHTTPError,
		IsRetryableNetwork: func(method string, _ error) bool { return netpolicy.IsSafeMethod(method) },
		IsRetryableHTTP: func(method string, statusCode int, _ http.Header, _ string) bool {
			if !netpolicy.IsSafeMethod(method) {
				return false
			}
			return statusCode == http.StatusTooManyRequests || statusCode >= 500
		},
		OnError: func(_ int, callErr error, duration time.Duration) {
			gcpLogEvent(runtime.LogPath, map[string]any{
				"event":       "error",
				"account":     runtime.AccountAlias,
				"project_id":  runtime.ProjectID,
				"method":      method,
				"path":        sanitizeURL(endpoint),
				"duration_ms": duration.Milliseconds(),
				"error":       redactGCPSensitive(callErr.Error()),
			})
		},
	})
}

func resolveGCPURL(baseURL string, path string, params map[string]string) (string, error) {
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		u, err := url.Parse(path)
		if err != nil {
			return "", err
		}
		q := u.Query()
		for key, value := range params {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			q.Set(key, strings.TrimSpace(value))
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
	for key, value := range params {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		q.Set(key, strings.TrimSpace(value))
	}
	full.RawQuery = q.Encode()
	return strings.TrimSpace(full.String()), nil
}

func normalizeGCPResponse(httpResp *http.Response, body string) gcpResponse {
	resp := gcpResponse{
		StatusCode: httpResp.StatusCode,
		Status:     strings.TrimSpace(httpResp.Status),
		RequestID:  firstGCPHeader(httpResp.Header, "X-Request-Id", "X-Google-Request-Id"),
		Headers:    map[string]string{},
		Body:       strings.TrimSpace(body),
	}
	for key, values := range httpResp.Header {
		if len(values) == 0 {
			continue
		}
		resp.Headers[key] = strings.Join(values, ",")
	}
	if strings.TrimSpace(resp.Body) == "" {
		return resp
	}
	var parsed any
	if err := json.Unmarshal([]byte(resp.Body), &parsed); err != nil {
		return resp
	}
	switch payload := parsed.(type) {
	case map[string]any:
		resp.Data = payload
		if list, ok := payload["services"].([]any); ok {
			resp.List = convertAnyListToMapList(list)
		}
	case []any:
		resp.List = convertAnyListToMapList(payload)
	}
	return resp
}

func normalizeGCPHTTPError(statusCode int, headers http.Header, body string) error {
	details := &gcpAPIErrorDetails{
		StatusCode: statusCode,
		RequestID:  firstGCPHeader(headers, "X-Request-Id", "X-Google-Request-Id"),
		RawBody:    strings.TrimSpace(body),
		Message:    "gcp request failed",
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &parsed); err == nil {
		if errObj, ok := parsed["error"].(map[string]any); ok {
			if value, ok := readSocialIntLike(errObj["code"]); ok {
				details.Code = int(value)
			}
			if value := strings.TrimSpace(stringifyWorkOSAny(errObj["status"])); value != "" {
				details.Status = value
			}
			if value := strings.TrimSpace(stringifyWorkOSAny(errObj["message"])); value != "" {
				details.Message = value
			}
		} else if value := strings.TrimSpace(stringifyWorkOSAny(parsed["message"])); value != "" {
			details.Message = value
		}
	}
	if details.Message == "gcp request failed" {
		details.Message = firstNonEmpty(strings.TrimSpace(body), http.StatusText(statusCode))
	}
	return details
}

func firstGCPHeader(headers http.Header, keys ...string) string {
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

func parseGCPParams(values []string) map[string]string {
	out := map[string]string{}
	for _, entry := range values {
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

func parseGCPJSONBody(raw string, fallback []string) map[string]any {
	trimmed := strings.TrimSpace(raw)
	if trimmed != "" {
		var obj map[string]any
		if err := json.Unmarshal([]byte(trimmed), &obj); err == nil && obj != nil {
			return obj
		}
		return map[string]any{"body": trimmed}
	}
	out := map[string]any{}
	for key, value := range parseGCPParams(fallback) {
		out[key] = coerceWorkOSValue(value)
	}
	return out
}

func printGCPResponse(resp gcpResponse, jsonOut bool, raw bool) {
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
	if len(resp.List) > 0 {
		fmt.Printf("%s %d\n", styleHeading("Items:"), len(resp.List))
		limit := len(resp.List)
		if limit > 20 {
			limit = 20
		}
		for i := 0; i < limit; i++ {
			item := resp.List[i]
			fmt.Printf("  %s %s\n", padRightANSI(orDash(stringifyWorkOSAny(item["name"])), 48), orDash(stringifyWorkOSAny(item["state"])))
		}
		if len(resp.List) > limit {
			fmt.Printf("  %s\n", styleDim(fmt.Sprintf("... %d more", len(resp.List)-limit)))
		}
		return
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

func printGCPError(err error) {
	if err == nil {
		return
	}
	var details *gcpAPIErrorDetails
	if errors.As(err, &details) {
		fmt.Printf("%s %s\n", styleHeading("GCP error:"), styleError(details.Error()))
		if details.RequestID != "" {
			fmt.Printf("%s %s\n", styleHeading("Request ID:"), details.RequestID)
		}
		if details.RawBody != "" {
			fmt.Printf("%s %s\n", styleHeading("Body:"), truncateString(details.RawBody, 600))
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("GCP error:"), styleError(err.Error()))
}

func gcpLogEvent(path string, event map[string]any) {
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
			event[key] = redactGCPSensitive(asString)
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

func redactGCPSensitive(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	replacements := []struct {
		needle string
		repl   string
	}{
		{"Authorization: Bearer ", "Authorization: Bearer ***"},
		{"access_token=", "access_token=***"},
	}
	masked := value
	for _, item := range replacements {
		if strings.Contains(strings.ToLower(masked), strings.ToLower(item.needle)) {
			masked = maskAfterToken(masked, item.needle, item.repl)
		}
	}
	return masked
}
