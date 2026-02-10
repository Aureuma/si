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

const workosUsageText = "usage: si workos <auth|context|doctor|organization|user|membership|invitation|directory|raw>"

type workosRuntimeContext struct {
	AccountAlias   string
	Environment    string
	BaseURL        string
	APIKey         string
	ClientID       string
	OrganizationID string
	Source         string
	LogPath        string
}

type workosRuntimeContextInput struct {
	AccountFlag      string
	EnvFlag          string
	BaseURLFlag      string
	APIKeyFlag       string
	ClientIDFlag     string
	OrganizationFlag string
}

type workosRequest struct {
	Method      string
	Path        string
	Params      map[string]string
	Headers     map[string]string
	RawBody     string
	JSONBody    any
	ContentType string
}

type workosResponse struct {
	StatusCode int               `json:"status_code"`
	Status     string            `json:"status"`
	RequestID  string            `json:"request_id,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	Data       map[string]any    `json:"data,omitempty"`
	List       []map[string]any  `json:"list,omitempty"`
}

type workosAPIErrorDetails struct {
	StatusCode int    `json:"status_code,omitempty"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	RawBody    string `json:"raw_body,omitempty"`
}

func (e *workosAPIErrorDetails) Error() string {
	if e == nil {
		return "workos api error"
	}
	parts := make([]string, 0, 6)
	if e.StatusCode > 0 {
		parts = append(parts, fmt.Sprintf("status_code=%d", e.StatusCode))
	}
	if strings.TrimSpace(e.Code) != "" {
		parts = append(parts, "code="+e.Code)
	}
	if strings.TrimSpace(e.Message) != "" {
		parts = append(parts, "message="+e.Message)
	}
	if strings.TrimSpace(e.RequestID) != "" {
		parts = append(parts, "request_id="+e.RequestID)
	}
	if len(parts) == 0 {
		return "workos api error"
	}
	return "workos api error: " + strings.Join(parts, ", ")
}

func cmdWorkOS(args []string) {
	if len(args) == 0 {
		printUsage(workosUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(workosUsageText)
	case "auth":
		cmdWorkOSAuth(rest)
	case "context":
		cmdWorkOSContext(rest)
	case "doctor":
		cmdWorkOSDoctor(rest)
	case "organization", "org":
		cmdWorkOSOrganization(rest)
	case "user", "users":
		cmdWorkOSUser(rest)
	case "membership", "memberships":
		cmdWorkOSMembership(rest)
	case "invitation", "invitations", "invite":
		cmdWorkOSInvitation(rest)
	case "directory", "directories":
		cmdWorkOSDirectory(rest)
	case "raw":
		cmdWorkOSRaw(rest)
	default:
		printUnknown("workos", sub)
		printUsage(workosUsageText)
	}
}

func cmdWorkOSAuth(args []string) {
	if len(args) == 0 {
		printUsage("usage: si workos auth status [--account <alias>] [--env <prod|staging|dev>] [--json]")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "status":
		cmdWorkOSAuthStatus(args[1:])
	default:
		printUnknown("workos auth", sub)
	}
}

func cmdWorkOSAuthStatus(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("workos auth status", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	apiKey := fs.String("api-key", "", "override api key")
	clientID := fs.String("client-id", "", "override client id")
	orgID := fs.String("org-id", "", "organization id")
	baseURL := fs.String("base-url", "", "api base url")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si workos auth status [--account <alias>] [--env <prod|staging|dev>] [--json]")
		return
	}
	runtime, err := resolveWorkOSRuntimeContext(workosRuntimeContextInput{
		AccountFlag:      *account,
		EnvFlag:          *env,
		BaseURLFlag:      *baseURL,
		APIKeyFlag:       *apiKey,
		ClientIDFlag:     *clientID,
		OrganizationFlag: *orgID,
	})
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	verifyResp, verifyErr := workosDo(ctx, runtime, workosRequest{
		Method: http.MethodGet,
		Path:   "/organizations",
		Params: map[string]string{"limit": "1"},
	})
	status := "error"
	if verifyErr == nil {
		status = "ready"
	}
	payload := map[string]any{
		"status":          status,
		"account_alias":   runtime.AccountAlias,
		"environment":     runtime.Environment,
		"organization_id": runtime.OrganizationID,
		"source":          runtime.Source,
		"base_url":        runtime.BaseURL,
		"key_preview":     previewWorkOSSecret(runtime.APIKey),
		"client_id":       runtime.ClientID,
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
		fmt.Printf("%s %s\n", styleHeading("WorkOS auth:"), styleError("error"))
		fmt.Printf("%s %s\n", styleHeading("Context:"), formatWorkOSContext(runtime))
		printWorkOSError(verifyErr)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("WorkOS auth:"), styleSuccess("ready"))
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatWorkOSContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
	fmt.Printf("%s %s\n", styleHeading("Token preview:"), previewWorkOSSecret(runtime.APIKey))
}

func cmdWorkOSContext(args []string) {
	if len(args) == 0 {
		printUsage("usage: si workos context <list|current|use>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdWorkOSContextList(rest)
	case "current":
		cmdWorkOSContextCurrent(rest)
	case "use":
		cmdWorkOSContextUse(rest)
	default:
		printUnknown("workos context", sub)
	}
}

func cmdWorkOSContextList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("workos context list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si workos context list [--json]")
		return
	}
	settings := loadSettingsOrDefault()
	aliases := workosAccountAliases(settings)
	rows := make([]map[string]string, 0, len(aliases))
	for _, alias := range aliases {
		entry := settings.WorkOS.Accounts[alias]
		rows = append(rows, map[string]string{
			"alias":         alias,
			"name":          strings.TrimSpace(entry.Name),
			"default":       boolString(alias == strings.TrimSpace(settings.WorkOS.DefaultAccount)),
			"api_key_env":   workosAccountKeyEnvRef(settings.WorkOS.DefaultEnv, entry),
			"client_id_env": strings.TrimSpace(entry.ClientIDEnv),
			"org_id":        strings.TrimSpace(entry.OrganizationID),
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
		infof("no workos accounts configured in settings")
		return
	}
	fmt.Printf("%s %s %s %s %s %s\n",
		padRightANSI(styleHeading("ALIAS"), 18),
		padRightANSI(styleHeading("DEFAULT"), 8),
		padRightANSI(styleHeading("API KEY ENV"), 34),
		padRightANSI(styleHeading("CLIENT ID ENV"), 28),
		padRightANSI(styleHeading("ORG ID"), 24),
		styleHeading("NAME"),
	)
	for _, row := range rows {
		fmt.Printf("%s %s %s %s %s %s\n",
			padRightANSI(orDash(row["alias"]), 18),
			padRightANSI(orDash(row["default"]), 8),
			padRightANSI(orDash(row["api_key_env"]), 34),
			padRightANSI(orDash(row["client_id_env"]), 28),
			padRightANSI(orDash(row["org_id"]), 24),
			orDash(row["name"]),
		)
	}
}

func cmdWorkOSContextCurrent(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("workos context current", flag.ExitOnError)
	flags := bindWorkOSCommonFlags(fs)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si workos context current [--json]")
		return
	}
	runtime, err := resolveWorkOSRuntimeContext(workosRuntimeContextInput{
		AccountFlag:      strings.TrimSpace(*flags.account),
		EnvFlag:          strings.TrimSpace(*flags.env),
		BaseURLFlag:      strings.TrimSpace(*flags.baseURL),
		APIKeyFlag:       strings.TrimSpace(*flags.apiKey),
		ClientIDFlag:     strings.TrimSpace(*flags.clientID),
		OrganizationFlag: strings.TrimSpace(*flags.orgID),
	})
	if err != nil {
		fatal(err)
	}
	payload := map[string]any{
		"account_alias":   runtime.AccountAlias,
		"environment":     runtime.Environment,
		"base_url":        runtime.BaseURL,
		"organization_id": runtime.OrganizationID,
		"client_id":       runtime.ClientID,
		"source":          runtime.Source,
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Current workos context:"), formatWorkOSContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
}

func cmdWorkOSContextUse(args []string) {
	fs := flag.NewFlagSet("workos context use", flag.ExitOnError)
	account := fs.String("account", "", "default account alias")
	env := fs.String("env", "", "default environment (prod|staging|dev)")
	baseURL := fs.String("base-url", "", "api base url")
	orgID := fs.String("org-id", "", "default organization id")
	apiKeyEnv := fs.String("api-key-env", "", "api key env-var reference for selected account")
	clientIDEnv := fs.String("client-id-env", "", "client id env-var reference for selected account")
	vaultPrefix := fs.String("vault-prefix", "", "account env prefix (optional)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si workos context use [--account <alias>] [--env <prod|staging|dev>] [--base-url <url>] [--org-id <id>] [--api-key-env <env>] [--client-id-env <env>] [--vault-prefix <prefix>]")
		return
	}
	settings := loadSettingsOrDefault()
	if value := strings.TrimSpace(*account); value != "" {
		settings.WorkOS.DefaultAccount = value
	}
	if value := strings.TrimSpace(*env); value != "" {
		parsed, err := parseWorkOSEnvironment(value)
		if err != nil {
			fatal(err)
		}
		settings.WorkOS.DefaultEnv = parsed
	}
	if value := strings.TrimSpace(*baseURL); value != "" {
		settings.WorkOS.APIBaseURL = value
	}
	if value := strings.TrimSpace(*orgID); value != "" {
		settings.WorkOS.DefaultOrganizationID = value
	}

	targetAlias := strings.TrimSpace(settings.WorkOS.DefaultAccount)
	if value := strings.TrimSpace(*account); value != "" {
		targetAlias = value
	}
	if targetAlias == "" && (strings.TrimSpace(*apiKeyEnv) != "" || strings.TrimSpace(*clientIDEnv) != "" || strings.TrimSpace(*orgID) != "" || strings.TrimSpace(*vaultPrefix) != "") {
		targetAlias = "default"
		settings.WorkOS.DefaultAccount = targetAlias
	}
	if targetAlias != "" {
		if settings.WorkOS.Accounts == nil {
			settings.WorkOS.Accounts = map[string]WorkOSAccountEntry{}
		}
		entry := settings.WorkOS.Accounts[targetAlias]
		if value := strings.TrimSpace(*apiKeyEnv); value != "" {
			entry.APIKeyEnv = value
		}
		if value := strings.TrimSpace(*clientIDEnv); value != "" {
			entry.ClientIDEnv = value
		}
		if value := strings.TrimSpace(*orgID); value != "" {
			entry.OrganizationID = value
		}
		if value := strings.TrimSpace(*vaultPrefix); value != "" {
			entry.VaultPrefix = value
		}
		settings.WorkOS.Accounts[targetAlias] = entry
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	fmt.Printf("%s default_account=%s env=%s base=%s org=%s\n",
		styleHeading("Updated workos context:"),
		orDash(settings.WorkOS.DefaultAccount),
		orDash(settings.WorkOS.DefaultEnv),
		orDash(settings.WorkOS.APIBaseURL),
		orDash(settings.WorkOS.DefaultOrganizationID),
	)
}

func cmdWorkOSDoctor(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "public": true})
	fs := flag.NewFlagSet("workos doctor", flag.ExitOnError)
	flags := bindWorkOSCommonFlags(fs)
	publicProbe := fs.Bool("public", false, "run unauthenticated public probe")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si workos doctor [--account <alias>] [--env <prod|staging|dev>] [--public] [--json]")
		return
	}
	if *publicProbe {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err := runPublicProviderDoctor(ctx, providers.WorkOS, strings.TrimSpace(*flags.baseURL))
		if err != nil {
			fatal(err)
		}
		printPublicDoctorResult("workos", result, *jsonOut)
		return
	}
	runtime, err := resolveWorkOSRuntimeContext(workosRuntimeContextInput{
		AccountFlag:      strings.TrimSpace(*flags.account),
		EnvFlag:          strings.TrimSpace(*flags.env),
		BaseURLFlag:      strings.TrimSpace(*flags.baseURL),
		APIKeyFlag:       strings.TrimSpace(*flags.apiKey),
		ClientIDFlag:     strings.TrimSpace(*flags.clientID),
		OrganizationFlag: strings.TrimSpace(*flags.orgID),
	})
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, verifyErr := workosDo(ctx, runtime, workosRequest{
		Method: http.MethodGet,
		Path:   "/organizations",
		Params: map[string]string{"limit": "1"},
	})
	checks := []doctorCheck{
		{Name: "api-key", OK: strings.TrimSpace(runtime.APIKey) != "", Detail: previewWorkOSSecret(runtime.APIKey)},
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
		"provider":        "workos",
		"base_url":        runtime.BaseURL,
		"account_alias":   runtime.AccountAlias,
		"environment":     runtime.Environment,
		"organization_id": runtime.OrganizationID,
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
		fmt.Printf("%s %s\n", styleHeading("WorkOS doctor:"), styleSuccess("ok"))
	} else {
		fmt.Printf("%s %s\n", styleHeading("WorkOS doctor:"), styleError("issues found"))
	}
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatWorkOSContext(runtime))
	for _, check := range checks {
		icon := styleSuccess("OK")
		if !check.OK {
			icon = styleError("ERR")
		}
		fmt.Printf("  %s %s %s\n", padRightANSI(icon, 4), padRightANSI(check.Name, 16), strings.TrimSpace(check.Detail))
	}
	if !ok {
		os.Exit(1)
	}
}

type workosResourceSpec struct {
	Label              string
	CollectionPath     string
	SupportsCreate     bool
	SupportsUpdate     bool
	SupportsDelete     bool
	UsesOrganizationID bool
}

var (
	workosOrganizationSpec = workosResourceSpec{
		Label:          "organization",
		CollectionPath: "/organizations",
		SupportsCreate: true,
		SupportsUpdate: true,
		SupportsDelete: true,
	}
	workosUserSpec = workosResourceSpec{
		Label:              "user",
		CollectionPath:     "/user_management/users",
		SupportsCreate:     true,
		SupportsUpdate:     true,
		SupportsDelete:     true,
		UsesOrganizationID: true,
	}
	workosMembershipSpec = workosResourceSpec{
		Label:              "membership",
		CollectionPath:     "/organization_memberships",
		SupportsCreate:     true,
		SupportsUpdate:     true,
		SupportsDelete:     true,
		UsesOrganizationID: true,
	}
	workosInvitationSpec = workosResourceSpec{
		Label:              "invitation",
		CollectionPath:     "/user_management/invitations",
		SupportsCreate:     true,
		SupportsUpdate:     false,
		SupportsDelete:     false,
		UsesOrganizationID: true,
	}
	workosDirectorySpec = workosResourceSpec{
		Label:          "directory",
		CollectionPath: "/directories",
	}
)

func cmdWorkOSOrganization(args []string) {
	cmdWorkOSResource(workosOrganizationSpec, args)
}

func cmdWorkOSUser(args []string) {
	cmdWorkOSResource(workosUserSpec, args)
}

func cmdWorkOSMembership(args []string) {
	cmdWorkOSResource(workosMembershipSpec, args)
}

func cmdWorkOSInvitation(args []string) {
	if len(args) == 0 {
		printUsage("usage: si workos invitation <list|get|create|revoke|raw>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list", "get", "create":
		cmdWorkOSResource(workosInvitationSpec, args)
	case "revoke":
		cmdWorkOSInvitationRevoke(rest)
	default:
		printUnknown("workos invitation", sub)
		printUsage("usage: si workos invitation <list|get|create|revoke>")
	}
}

func cmdWorkOSInvitationRevoke(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("workos invitation revoke", flag.ExitOnError)
	flags := bindWorkOSCommonFlags(fs)
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si workos invitation revoke <invitation_id> [--account <alias>] [--env <prod|staging|dev>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromWorkOSFlags(flags)
	if err != nil {
		fatal(err)
	}
	invitationID := url.PathEscape(strings.TrimSpace(fs.Arg(0)))
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := workosDo(ctx, runtime, workosRequest{
		Method:   http.MethodPost,
		Path:     workosInvitationSpec.CollectionPath + "/" + invitationID + "/revoke",
		JSONBody: map[string]any{},
	})
	if err != nil {
		printWorkOSError(err)
		return
	}
	printWorkOSResponse(resp, *jsonOut, *raw)
}

func cmdWorkOSDirectory(args []string) {
	if len(args) == 0 {
		printUsage("usage: si workos directory <list|get|users|groups|sync>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list", "get":
		cmdWorkOSResource(workosDirectorySpec, args)
	case "users":
		cmdWorkOSDirectoryUsers(rest)
	case "groups":
		cmdWorkOSDirectoryGroups(rest)
	case "sync":
		cmdWorkOSDirectorySync(rest)
	default:
		printUnknown("workos directory", sub)
		printUsage("usage: si workos directory <list|get|users|groups|sync>")
	}
}

func cmdWorkOSDirectoryUsers(args []string) {
	cmdWorkOSDirectoryCollection("users", "/directory_users", args)
}

func cmdWorkOSDirectoryGroups(args []string) {
	cmdWorkOSDirectoryCollection("groups", "/directory_groups", args)
}

func cmdWorkOSDirectoryCollection(kind string, path string, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("workos directory "+kind, flag.ExitOnError)
	flags := bindWorkOSCommonFlags(fs)
	limit := fs.Int("limit", 50, "max items")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si workos directory " + kind + " <directory_id> [--limit N] [--param key=value] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromWorkOSFlags(flags)
	if err != nil {
		fatal(err)
	}
	directoryID := strings.TrimSpace(fs.Arg(0))
	query := parseWorkOSParams(params)
	query["directory"] = directoryID
	if *limit > 0 {
		query["limit"] = strconv.Itoa(*limit)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := workosDo(ctx, runtime, workosRequest{
		Method: http.MethodGet,
		Path:   path,
		Params: query,
	})
	if err != nil {
		printWorkOSError(err)
		return
	}
	printWorkOSResponse(resp, *jsonOut, *raw)
}

func cmdWorkOSDirectorySync(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("workos directory sync", flag.ExitOnError)
	flags := bindWorkOSCommonFlags(fs)
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si workos directory sync <directory_id> [--json]")
		return
	}
	runtime, err := resolveRuntimeFromWorkOSFlags(flags)
	if err != nil {
		fatal(err)
	}
	directoryID := url.PathEscape(strings.TrimSpace(fs.Arg(0)))
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := workosDo(ctx, runtime, workosRequest{
		Method:   http.MethodPost,
		Path:     "/directories/" + directoryID + "/sync",
		JSONBody: map[string]any{},
	})
	if err != nil {
		printWorkOSError(err)
		return
	}
	printWorkOSResponse(resp, *jsonOut, *raw)
}

func cmdWorkOSResource(spec workosResourceSpec, args []string) {
	if len(args) == 0 {
		printUsage("usage: si workos " + spec.Label + " <list|get|create|update|delete>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdWorkOSResourceList(spec, rest)
	case "get":
		cmdWorkOSResourceGet(spec, rest)
	case "create":
		if !spec.SupportsCreate {
			fatal(fmt.Errorf("%s create is not supported", spec.Label))
		}
		cmdWorkOSResourceCreate(spec, rest)
	case "update":
		if !spec.SupportsUpdate {
			fatal(fmt.Errorf("%s update is not supported", spec.Label))
		}
		cmdWorkOSResourceUpdate(spec, rest)
	case "delete":
		if !spec.SupportsDelete {
			fatal(fmt.Errorf("%s delete is not supported", spec.Label))
		}
		cmdWorkOSResourceDelete(spec, rest)
	default:
		printUnknown("workos "+spec.Label, sub)
	}
}

func cmdWorkOSResourceList(spec workosResourceSpec, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("workos "+spec.Label+" list", flag.ExitOnError)
	flags := bindWorkOSCommonFlags(fs)
	limit := fs.Int("limit", 50, "max items")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si workos " + spec.Label + " list [--limit N] [--param key=value] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromWorkOSFlags(flags)
	if err != nil {
		fatal(err)
	}
	query := parseWorkOSParams(params)
	if *limit > 0 {
		query["limit"] = strconv.Itoa(*limit)
	}
	injectWorkOSOrganizationParam(spec, runtime, query)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := workosDo(ctx, runtime, workosRequest{
		Method: http.MethodGet,
		Path:   spec.CollectionPath,
		Params: query,
	})
	if err != nil {
		printWorkOSError(err)
		return
	}
	printWorkOSResponse(resp, *jsonOut, *raw)
}

func cmdWorkOSResourceGet(spec workosResourceSpec, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("workos "+spec.Label+" get", flag.ExitOnError)
	flags := bindWorkOSCommonFlags(fs)
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si workos " + spec.Label + " get <id> [--param key=value] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromWorkOSFlags(flags)
	if err != nil {
		fatal(err)
	}
	itemID := url.PathEscape(strings.TrimSpace(fs.Arg(0)))
	query := parseWorkOSParams(params)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := workosDo(ctx, runtime, workosRequest{
		Method: http.MethodGet,
		Path:   spec.CollectionPath + "/" + itemID,
		Params: query,
	})
	if err != nil {
		printWorkOSError(err)
		return
	}
	printWorkOSResponse(resp, *jsonOut, *raw)
}

func cmdWorkOSResourceCreate(spec workosResourceSpec, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("workos "+spec.Label+" create", flag.ExitOnError)
	flags := bindWorkOSCommonFlags(fs)
	jsonBody := fs.String("json-body", "", "json request body")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si workos " + spec.Label + " create [--param key=value] [--json-body '{...}'] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromWorkOSFlags(flags)
	if err != nil {
		fatal(err)
	}
	body := parseWorkOSJSONBody(strings.TrimSpace(*jsonBody), params)
	injectWorkOSOrganizationBody(spec, runtime, body)
	if len(body) == 0 {
		fatal(fmt.Errorf("create body is empty; provide --param key=value or --json-body"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := workosDo(ctx, runtime, workosRequest{
		Method:   http.MethodPost,
		Path:     spec.CollectionPath,
		JSONBody: body,
	})
	if err != nil {
		printWorkOSError(err)
		return
	}
	printWorkOSResponse(resp, *jsonOut, *raw)
}

func cmdWorkOSResourceUpdate(spec workosResourceSpec, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("workos "+spec.Label+" update", flag.ExitOnError)
	flags := bindWorkOSCommonFlags(fs)
	jsonBody := fs.String("json-body", "", "json request body")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si workos " + spec.Label + " update <id> [--param key=value] [--json-body '{...}'] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromWorkOSFlags(flags)
	if err != nil {
		fatal(err)
	}
	itemID := url.PathEscape(strings.TrimSpace(fs.Arg(0)))
	body := parseWorkOSJSONBody(strings.TrimSpace(*jsonBody), params)
	injectWorkOSOrganizationBody(spec, runtime, body)
	if len(body) == 0 {
		fatal(fmt.Errorf("update body is empty; provide --param key=value or --json-body"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := workosDo(ctx, runtime, workosRequest{
		Method:   http.MethodPut,
		Path:     spec.CollectionPath + "/" + itemID,
		JSONBody: body,
	})
	if err != nil {
		printWorkOSError(err)
		return
	}
	printWorkOSResponse(resp, *jsonOut, *raw)
}

func cmdWorkOSResourceDelete(spec workosResourceSpec, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("workos "+spec.Label+" delete", flag.ExitOnError)
	flags := bindWorkOSCommonFlags(fs)
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	force := fs.Bool("force", false, "skip confirmation")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si workos " + spec.Label + " delete <id> [--force] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromWorkOSFlags(flags)
	if err != nil {
		fatal(err)
	}
	itemID := strings.TrimSpace(fs.Arg(0))
	if err := requireWorkOSConfirmation("delete "+spec.Label+" "+itemID, *force); err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := workosDo(ctx, runtime, workosRequest{
		Method: http.MethodDelete,
		Path:   spec.CollectionPath + "/" + url.PathEscape(itemID),
	})
	if err != nil {
		printWorkOSError(err)
		return
	}
	printWorkOSResponse(resp, *jsonOut, *raw)
}

func requireWorkOSConfirmation(action string, force bool) error {
	if force {
		return nil
	}
	ok, accepted := confirmYN("Confirm "+strings.TrimSpace(action)+"?", false)
	if !accepted {
		return fmt.Errorf("operation canceled")
	}
	if !ok {
		return fmt.Errorf("operation aborted")
	}
	return nil
}

func cmdWorkOSRaw(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("workos raw", flag.ExitOnError)
	flags := bindWorkOSCommonFlags(fs)
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
		printUsage("usage: si workos raw --method <GET|POST|PUT|PATCH|DELETE> --path <api-path> [--param key=value] [--header key=value] [--body raw|--json-body '{...}'] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromWorkOSFlags(flags)
	if err != nil {
		fatal(err)
	}
	var payload any
	rawBody := strings.TrimSpace(*body)
	if strings.TrimSpace(*jsonBody) != "" {
		payload = parseWorkOSJSONBody(strings.TrimSpace(*jsonBody), nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := workosDo(ctx, runtime, workosRequest{
		Method:   strings.ToUpper(strings.TrimSpace(*method)),
		Path:     strings.TrimSpace(*path),
		Params:   parseWorkOSParams(params),
		Headers:  parseWorkOSParams(headers),
		RawBody:  rawBody,
		JSONBody: payload,
	})
	if err != nil {
		printWorkOSError(err)
		return
	}
	printWorkOSResponse(resp, *jsonOut, *raw)
}

type workosCommonFlags struct {
	account  *string
	env      *string
	apiKey   *string
	clientID *string
	orgID    *string
	baseURL  *string
}

func bindWorkOSCommonFlags(fs *flag.FlagSet) workosCommonFlags {
	return workosCommonFlags{
		account:  fs.String("account", "", "account alias"),
		env:      fs.String("env", "", "environment (prod|staging|dev)"),
		apiKey:   fs.String("api-key", "", "override api key"),
		clientID: fs.String("client-id", "", "override client id"),
		orgID:    fs.String("org-id", "", "organization id"),
		baseURL:  fs.String("base-url", "", "api base url"),
	}
}

func resolveRuntimeFromWorkOSFlags(flags workosCommonFlags) (workosRuntimeContext, error) {
	return resolveWorkOSRuntimeContext(workosRuntimeContextInput{
		AccountFlag:      strings.TrimSpace(valueOrEmpty(flags.account)),
		EnvFlag:          strings.TrimSpace(valueOrEmpty(flags.env)),
		BaseURLFlag:      strings.TrimSpace(valueOrEmpty(flags.baseURL)),
		APIKeyFlag:       strings.TrimSpace(valueOrEmpty(flags.apiKey)),
		ClientIDFlag:     strings.TrimSpace(valueOrEmpty(flags.clientID)),
		OrganizationFlag: strings.TrimSpace(valueOrEmpty(flags.orgID)),
	})
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func resolveWorkOSRuntimeContext(input workosRuntimeContextInput) (workosRuntimeContext, error) {
	settings := loadSettingsOrDefault()
	alias, account := resolveWorkOSAccountSelection(settings, input.AccountFlag)
	env := strings.TrimSpace(input.EnvFlag)
	if env == "" {
		env = strings.TrimSpace(settings.WorkOS.DefaultEnv)
	}
	if env == "" {
		env = strings.TrimSpace(os.Getenv("WORKOS_DEFAULT_ENV"))
	}
	if env == "" {
		env = "prod"
	}
	parsedEnv, err := parseWorkOSEnvironment(env)
	if err != nil {
		return workosRuntimeContext{}, err
	}

	spec := providers.Resolve(providers.WorkOS)
	baseURL := strings.TrimSpace(input.BaseURLFlag)
	if baseURL == "" {
		baseURL = strings.TrimSpace(account.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(settings.WorkOS.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("WORKOS_API_BASE_URL"))
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(spec.BaseURL)
	}
	if baseURL == "" {
		baseURL = "https://api.workos.com"
	}

	apiKey, keySource := resolveWorkOSAPIKey(alias, account, parsedEnv, strings.TrimSpace(input.APIKeyFlag))
	if strings.TrimSpace(apiKey) == "" {
		prefix := workosAccountEnvPrefix(alias, account)
		if prefix == "" {
			prefix = "WORKOS_<ACCOUNT>_"
		}
		return workosRuntimeContext{}, fmt.Errorf("workos api key not found (set --api-key, %sAPI_KEY, or WORKOS_API_KEY)", prefix)
	}

	clientID, clientIDSource := resolveWorkOSClientID(alias, account, strings.TrimSpace(input.ClientIDFlag))
	organizationID, orgSource := resolveWorkOSOrganizationID(alias, account, settings, strings.TrimSpace(input.OrganizationFlag))
	logPath := resolveWorkOSLogPath(settings)
	source := strings.Join(nonEmpty(keySource, clientIDSource, orgSource), ",")
	return workosRuntimeContext{
		AccountAlias:   alias,
		Environment:    parsedEnv,
		BaseURL:        strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIKey:         strings.TrimSpace(apiKey),
		ClientID:       strings.TrimSpace(clientID),
		OrganizationID: strings.TrimSpace(organizationID),
		Source:         source,
		LogPath:        logPath,
	}, nil
}

func resolveWorkOSAccountSelection(settings Settings, accountFlag string) (string, WorkOSAccountEntry) {
	selected := strings.TrimSpace(accountFlag)
	if selected == "" {
		selected = strings.TrimSpace(settings.WorkOS.DefaultAccount)
	}
	if selected == "" {
		selected = strings.TrimSpace(os.Getenv("WORKOS_DEFAULT_ACCOUNT"))
	}
	if selected == "" {
		aliases := workosAccountAliases(settings)
		if len(aliases) == 1 {
			selected = aliases[0]
		}
	}
	if selected == "" {
		return "", WorkOSAccountEntry{}
	}
	if entry, ok := settings.WorkOS.Accounts[selected]; ok {
		return selected, entry
	}
	return selected, WorkOSAccountEntry{}
}

func workosAccountAliases(settings Settings) []string {
	if len(settings.WorkOS.Accounts) == 0 {
		return nil
	}
	out := make([]string, 0, len(settings.WorkOS.Accounts))
	for alias := range settings.WorkOS.Accounts {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

func parseWorkOSEnvironment(raw string) (string, error) {
	env := normalizeWorkOSEnvironment(raw)
	if env == "" {
		if strings.TrimSpace(raw) == "" {
			return "", fmt.Errorf("environment required (prod|staging|dev)")
		}
		if strings.EqualFold(strings.TrimSpace(raw), "test") {
			return "", fmt.Errorf("environment `test` is not supported; use `staging` or `dev`")
		}
		return "", fmt.Errorf("invalid environment %q (expected prod|staging|dev)", raw)
	}
	return env, nil
}

func normalizeWorkOSEnvironment(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "prod", "staging", "dev":
		return value
	default:
		return ""
	}
}

func workosAccountEnvPrefix(alias string, account WorkOSAccountEntry) string {
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
	return "WORKOS_" + alias + "_"
}

func resolveWorkOSEnv(alias string, account WorkOSAccountEntry, key string) string {
	prefix := workosAccountEnvPrefix(alias, account)
	if prefix != "" {
		if value := strings.TrimSpace(os.Getenv(prefix + key)); value != "" {
			return value
		}
	}
	return ""
}

func resolveWorkOSAPIKey(alias string, account WorkOSAccountEntry, env string, override string) (string, string) {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override), "flag:--api-key"
	}
	switch normalizeWorkOSEnvironment(env) {
	case "prod":
		if ref := strings.TrimSpace(account.ProdAPIKeyEnv); ref != "" {
			if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
				return value, "env:" + ref
			}
		}
		if value := strings.TrimSpace(resolveWorkOSEnv(alias, account, "PROD_API_KEY")); value != "" {
			return value, "env:" + workosAccountEnvPrefix(alias, account) + "PROD_API_KEY"
		}
	case "staging":
		if ref := strings.TrimSpace(account.StagingAPIKeyEnv); ref != "" {
			if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
				return value, "env:" + ref
			}
		}
		if value := strings.TrimSpace(resolveWorkOSEnv(alias, account, "STAGING_API_KEY")); value != "" {
			return value, "env:" + workosAccountEnvPrefix(alias, account) + "STAGING_API_KEY"
		}
	case "dev":
		if ref := strings.TrimSpace(account.DevAPIKeyEnv); ref != "" {
			if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
				return value, "env:" + ref
			}
		}
		if value := strings.TrimSpace(resolveWorkOSEnv(alias, account, "DEV_API_KEY")); value != "" {
			return value, "env:" + workosAccountEnvPrefix(alias, account) + "DEV_API_KEY"
		}
	}
	if ref := strings.TrimSpace(account.APIKeyEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveWorkOSEnv(alias, account, "API_KEY")); value != "" {
		return value, "env:" + workosAccountEnvPrefix(alias, account) + "API_KEY"
	}
	if value := strings.TrimSpace(os.Getenv("WORKOS_API_KEY")); value != "" {
		return value, "env:WORKOS_API_KEY"
	}
	if value := strings.TrimSpace(os.Getenv("WORKOS_MANAGEMENT_API_KEY")); value != "" {
		return value, "env:WORKOS_MANAGEMENT_API_KEY"
	}
	return "", ""
}

func resolveWorkOSClientID(alias string, account WorkOSAccountEntry, override string) (string, string) {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override), "flag:--client-id"
	}
	if ref := strings.TrimSpace(account.ClientIDEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveWorkOSEnv(alias, account, "CLIENT_ID")); value != "" {
		return value, "env:" + workosAccountEnvPrefix(alias, account) + "CLIENT_ID"
	}
	if value := strings.TrimSpace(os.Getenv("WORKOS_CLIENT_ID")); value != "" {
		return value, "env:WORKOS_CLIENT_ID"
	}
	return "", ""
}

func resolveWorkOSOrganizationID(alias string, account WorkOSAccountEntry, settings Settings, override string) (string, string) {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override), "flag:--org-id"
	}
	if value := strings.TrimSpace(account.OrganizationID); value != "" {
		return value, "settings.organization_id"
	}
	if value := strings.TrimSpace(resolveWorkOSEnv(alias, account, "ORGANIZATION_ID")); value != "" {
		return value, "env:" + workosAccountEnvPrefix(alias, account) + "ORGANIZATION_ID"
	}
	if value := strings.TrimSpace(settings.WorkOS.DefaultOrganizationID); value != "" {
		return value, "settings.default_organization_id"
	}
	if value := strings.TrimSpace(os.Getenv("WORKOS_ORGANIZATION_ID")); value != "" {
		return value, "env:WORKOS_ORGANIZATION_ID"
	}
	return "", ""
}

func workosAccountKeyEnvRef(env string, account WorkOSAccountEntry) string {
	switch normalizeWorkOSEnvironment(env) {
	case "prod":
		if value := strings.TrimSpace(account.ProdAPIKeyEnv); value != "" {
			return value
		}
	case "staging":
		if value := strings.TrimSpace(account.StagingAPIKeyEnv); value != "" {
			return value
		}
	case "dev":
		if value := strings.TrimSpace(account.DevAPIKeyEnv); value != "" {
			return value
		}
	}
	return strings.TrimSpace(account.APIKeyEnv)
}

func resolveWorkOSLogPath(settings Settings) string {
	if value := strings.TrimSpace(os.Getenv("SI_WORKOS_LOG_FILE")); value != "" {
		return value
	}
	if value := strings.TrimSpace(settings.WorkOS.LogFile); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".si", "logs", "workos.log")
}

func formatWorkOSContext(runtime workosRuntimeContext) string {
	account := strings.TrimSpace(runtime.AccountAlias)
	if account == "" {
		account = "(default)"
	}
	org := strings.TrimSpace(runtime.OrganizationID)
	if org == "" {
		org = "-"
	}
	return fmt.Sprintf("account=%s env=%s org=%s base=%s", account, runtime.Environment, org, runtime.BaseURL)
}

func previewWorkOSSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	if len(value) <= 6 {
		return strings.Repeat("*", len(value))
	}
	return value[:3] + strings.Repeat("*", len(value)-6) + value[len(value)-3:]
}

func workosDo(ctx context.Context, runtime workosRuntimeContext, req workosRequest) (workosResponse, error) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		return workosResponse{}, fmt.Errorf("request path is required")
	}
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
	}
	endpoint, err := resolveWorkOSURL(runtime.BaseURL, path, req.Params)
	if err != nil {
		return workosResponse{}, err
	}
	providerID := providers.WorkOS

	return integrationruntime.DoHTTP(ctx, integrationruntime.HTTPExecutorOptions[workosResponse]{
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
				userAgent = "si-workos/1.0"
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
			httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(runtime.APIKey))
			if bodyReader != nil {
				contentType := strings.TrimSpace(req.ContentType)
				if contentType == "" {
					contentType = "application/json"
				}
				httpReq.Header.Set("Content-Type", contentType)
			}
			return httpReq, nil
		},
		NormalizeResponse: normalizeWorkOSResponse,
		StatusCode: func(resp workosResponse) int {
			return resp.StatusCode
		},
		NormalizeHTTPError: normalizeWorkOSHTTPError,
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
		OnCacheHit: func(resp workosResponse) {
			workosLogEvent(runtime.LogPath, map[string]any{
				"event":       "cache_hit",
				"account":     runtime.AccountAlias,
				"environment": runtime.Environment,
				"method":      method,
				"path":        sanitizeURL(endpoint),
				"status_code": resp.StatusCode,
			})
		},
		OnResponse: func(_ int, resp workosResponse, _ http.Header, duration time.Duration) {
			workosLogEvent(runtime.LogPath, map[string]any{
				"event":       "response",
				"account":     runtime.AccountAlias,
				"environment": runtime.Environment,
				"method":      method,
				"path":        sanitizeURL(endpoint),
				"status_code": resp.StatusCode,
				"request_id":  resp.RequestID,
				"duration_ms": duration.Milliseconds(),
			})
		},
		OnError: func(_ int, callErr error, duration time.Duration) {
			workosLogEvent(runtime.LogPath, map[string]any{
				"event":       "error",
				"account":     runtime.AccountAlias,
				"environment": runtime.Environment,
				"method":      method,
				"path":        sanitizeURL(endpoint),
				"duration_ms": duration.Milliseconds(),
				"error":       redactWorkOSSensitive(callErr.Error()),
			})
		},
	})
}

func resolveWorkOSURL(baseURL string, path string, params map[string]string) (string, error) {
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
			q.Set(key, value)
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
		q.Set(key, value)
	}
	full.RawQuery = q.Encode()
	return strings.TrimSpace(full.String()), nil
}

func normalizeWorkOSResponse(httpResp *http.Response, body string) workosResponse {
	resp := workosResponse{
		StatusCode: httpResp.StatusCode,
		Status:     strings.TrimSpace(httpResp.Status),
		RequestID:  firstWorkOSHeader(httpResp.Header, "X-Request-ID", "X-Request-Id", "Request-Id"),
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
			if list, ok := payload["list"].([]any); ok {
				resp.List = convertAnyListToMapList(list)
			}
		}
	case []any:
		resp.List = convertAnyListToMapList(payload)
	}
	return resp
}

func normalizeWorkOSHTTPError(statusCode int, headers http.Header, body string) error {
	details := &workosAPIErrorDetails{
		StatusCode: statusCode,
		RequestID:  firstWorkOSHeader(headers, "X-Request-ID", "X-Request-Id", "Request-Id"),
		RawBody:    strings.TrimSpace(body),
		Message:    "workos request failed",
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
	if value := strings.TrimSpace(stringifyWorkOSAny(parsed["code"])); value != "" {
		details.Code = value
	}
	if nested, ok := parsed["error"].(map[string]any); ok {
		if details.Code == "" {
			details.Code = strings.TrimSpace(stringifyWorkOSAny(nested["code"]))
		}
		if value := strings.TrimSpace(stringifyWorkOSAny(nested["message"])); value != "" {
			details.Message = value
		}
	}
	if value := strings.TrimSpace(stringifyWorkOSAny(parsed["message"])); value != "" {
		details.Message = value
	}
	if details.Message == "workos request failed" {
		details.Message = trimmed
	}
	return details
}

func firstWorkOSHeader(headers http.Header, keys ...string) string {
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

func convertAnyListToMapList(items []any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		switch typed := item.(type) {
		case map[string]any:
			out = append(out, typed)
		default:
			out = append(out, map[string]any{"value": typed})
		}
	}
	return out
}

func parseWorkOSParams(values []string) map[string]string {
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

func parseWorkOSJSONBody(raw string, fallback []string) map[string]any {
	trimmed := strings.TrimSpace(raw)
	if trimmed != "" {
		var obj map[string]any
		if err := json.Unmarshal([]byte(trimmed), &obj); err == nil && obj != nil {
			return obj
		}
		return map[string]any{"body": trimmed}
	}
	out := map[string]any{}
	for key, value := range parseWorkOSParams(fallback) {
		out[key] = coerceWorkOSValue(value)
	}
	return out
}

func coerceWorkOSValue(raw string) any {
	value := strings.TrimSpace(raw)
	switch strings.ToLower(value) {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if intValue, err := strconv.ParseInt(value, 10, 64); err == nil {
		return intValue
	}
	if floatValue, err := strconv.ParseFloat(value, 64); err == nil {
		return floatValue
	}
	return value
}

func injectWorkOSOrganizationParam(spec workosResourceSpec, runtime workosRuntimeContext, query map[string]string) {
	if !spec.UsesOrganizationID || query == nil {
		return
	}
	if strings.TrimSpace(query["organization_id"]) != "" {
		return
	}
	if strings.TrimSpace(runtime.OrganizationID) == "" {
		return
	}
	query["organization_id"] = strings.TrimSpace(runtime.OrganizationID)
}

func injectWorkOSOrganizationBody(spec workosResourceSpec, runtime workosRuntimeContext, body map[string]any) {
	if !spec.UsesOrganizationID || body == nil {
		return
	}
	if _, ok := body["organization_id"]; ok {
		return
	}
	if strings.TrimSpace(runtime.OrganizationID) == "" {
		return
	}
	body["organization_id"] = strings.TrimSpace(runtime.OrganizationID)
}

func printWorkOSResponse(resp workosResponse, jsonOut bool, raw bool) {
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
			fmt.Printf("  %s %s\n", padRightANSI(orDash(workosItemID(item)), 32), orDash(workosItemLabel(item)))
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

func printWorkOSError(err error) {
	if err == nil {
		return
	}
	var details *workosAPIErrorDetails
	if errors.As(err, &details) {
		fmt.Printf("%s %s\n", styleHeading("WorkOS error:"), styleError(details.Error()))
		if details.RequestID != "" {
			fmt.Printf("%s %s\n", styleHeading("Request ID:"), details.RequestID)
		}
		if details.RawBody != "" {
			fmt.Printf("%s %s\n", styleHeading("Body:"), truncateString(details.RawBody, 600))
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("WorkOS error:"), styleError(err.Error()))
}

func workosItemID(item map[string]any) string {
	for _, key := range []string{"id", "user_id", "organization_id", "membership_id", "directory_id"} {
		if value := strings.TrimSpace(stringifyWorkOSAny(item[key])); value != "" {
			return value
		}
	}
	return ""
}

func workosItemLabel(item map[string]any) string {
	for _, key := range []string{"name", "email", "slug", "state", "domain", "status", "object"} {
		if value := strings.TrimSpace(stringifyWorkOSAny(item[key])); value != "" {
			return value
		}
	}
	return "-"
}

func stringifyWorkOSAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		v := float64(typed)
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", typed)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(raw)
	}
}

func errorOrOK(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
}

func truncateString(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max] + "..."
}

func workosLogEvent(path string, event map[string]any) {
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
			event[key] = redactWorkOSSensitive(asString)
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

func redactWorkOSSensitive(value string) string {
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
		{"api_key=", "api_key=***"},
	}
	masked := value
	for _, item := range replacements {
		if strings.Contains(strings.ToLower(masked), strings.ToLower(item.needle)) {
			masked = maskAfterToken(masked, item.needle, item.repl)
		}
	}
	return masked
}

func maskAfterToken(value string, token string, replacement string) string {
	idx := strings.Index(strings.ToLower(value), strings.ToLower(token))
	if idx < 0 {
		return value
	}
	end := idx + len(token)
	tail := value[end:]
	tail = strings.TrimLeft(tail, " ")
	for i, r := range tail {
		if r == '&' || r == ',' || r == ' ' || r == '\n' {
			return value[:idx] + replacement + tail[i:]
		}
	}
	return value[:idx] + replacement
}
