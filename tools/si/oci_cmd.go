package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
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

const ociUsageText = "usage: si oci <auth|context|doctor|identity|network|compute|oracular|raw>"

type ociRuntimeContext struct {
	AccountAlias   string
	Profile        string
	ConfigFile     string
	Region         string
	BaseURL        string
	AuthStyle      string
	TenancyOCID    string
	UserOCID       string
	Fingerprint    string
	PrivateKey     *rsa.PrivateKey
	PrivateKeyPath string
	Source         string
	LogPath        string
}

type ociRuntimeContextInput struct {
	AccountFlag    string
	ProfileFlag    string
	ConfigFileFlag string
	RegionFlag     string
	BaseURLFlag    string
	AuthStyleFlag  string
	RequireAuth    bool
}

type ociRequest struct {
	Method   string
	Path     string
	Params   map[string]string
	Headers  map[string]string
	RawBody  string
	JSONBody any
}

type ociResponse struct {
	StatusCode int               `json:"status_code"`
	Status     string            `json:"status"`
	RequestID  string            `json:"request_id,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	Data       map[string]any    `json:"data,omitempty"`
	List       []map[string]any  `json:"list,omitempty"`
}

type ociAPIErrorDetails struct {
	StatusCode int    `json:"status_code,omitempty"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	RawBody    string `json:"raw_body,omitempty"`
}

func (e *ociAPIErrorDetails) Error() string {
	if e == nil {
		return "oci api error"
	}
	parts := make([]string, 0, 5)
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
		return "oci api error"
	}
	return "oci api error: " + strings.Join(parts, ", ")
}

func cmdOCI(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, ociUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(ociUsageText)
	case "auth":
		cmdOCIAuth(rest)
	case "context":
		cmdOCIContext(rest)
	case "doctor":
		cmdOCIDoctor(rest)
	case "identity":
		cmdOCIIdentity(rest)
	case "network":
		cmdOCINetwork(rest)
	case "compute":
		cmdOCICompute(rest)
	case "oracular":
		cmdOCIOracular(rest)
	case "raw":
		cmdOCIRaw(rest)
	default:
		printUnknown("oci", sub)
		printUsage(ociUsageText)
	}
}

func cmdOCIAuth(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si oci auth status [--profile <name>] [--config-file <path>] [--region <region>] [--json]")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "status":
		cmdOCIAuthStatus(args[1:])
	default:
		printUnknown("oci auth", sub)
	}
}

func cmdOCIAuthStatus(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("oci auth status", flag.ExitOnError)
	flags := bindOCICommonFlags(fs)
	verify := fs.Bool("verify", true, "verify with OCI identity availability-domains call")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si oci auth status [--profile <name>] [--config-file <path>] [--region <region>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromOCIFlags(flags, true)
	if err != nil {
		fatal(err)
	}
	status := "ready"
	var verifyErr error
	var verifyResp ociResponse
	if *verify {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		verifyResp, verifyErr = ociDo(ctx, runtime, ociRequest{
			Method: http.MethodGet,
			Path:   ociIdentityURL(runtime.Region) + "/20160918/availabilityDomains",
			Params: map[string]string{"compartmentId": runtime.TenancyOCID},
		})
		if verifyErr != nil {
			status = "error"
		}
	}
	payload := map[string]any{
		"status":        status,
		"account_alias": runtime.AccountAlias,
		"profile":       runtime.Profile,
		"config_file":   runtime.ConfigFile,
		"region":        runtime.Region,
		"base_url":      runtime.BaseURL,
		"auth_style":    runtime.AuthStyle,
		"tenancy_ocid":  runtime.TenancyOCID,
		"user_ocid":     runtime.UserOCID,
		"fingerprint":   runtime.Fingerprint,
		"source":        runtime.Source,
	}
	if *verify {
		if verifyErr == nil {
			payload["verify_status"] = verifyResp.StatusCode
			payload["verify"] = verifyResp.Data
		} else {
			payload["verify_error"] = verifyErr.Error()
		}
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
		fmt.Printf("%s %s\n", styleHeading("OCI auth:"), styleError("error"))
		fmt.Printf("%s %s\n", styleHeading("Context:"), formatOCIContext(runtime))
		printOCIError(verifyErr)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("OCI auth:"), styleSuccess("ready"))
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatOCIContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
}

func cmdOCIContext(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si oci context <list|current|use>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdOCIContextList(rest)
	case "current":
		cmdOCIContextCurrent(rest)
	case "use":
		cmdOCIContextUse(rest)
	default:
		printUnknown("oci context", sub)
	}
}

func cmdOCIContextList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("oci context list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si oci context list [--json]")
		return
	}
	settings := loadSettingsOrDefault()
	aliases := ociAccountAliases(settings)
	rows := make([]map[string]string, 0, len(aliases))
	for _, alias := range aliases {
		entry := settings.OCI.Accounts[alias]
		rows = append(rows, map[string]string{
			"alias":       alias,
			"name":        strings.TrimSpace(entry.Name),
			"default":     boolString(alias == strings.TrimSpace(settings.OCI.DefaultAccount)),
			"profile":     firstNonEmpty(strings.TrimSpace(entry.Profile), strings.TrimSpace(settings.OCI.Profile)),
			"region":      firstNonEmpty(strings.TrimSpace(entry.Region), strings.TrimSpace(settings.OCI.Region)),
			"config_file": firstNonEmpty(strings.TrimSpace(entry.ConfigFile), strings.TrimSpace(settings.OCI.ConfigFile)),
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
		infof("no oci accounts configured in settings")
		return
	}
	headers := []string{
		styleHeading("ALIAS"),
		styleHeading("DEFAULT"),
		styleHeading("PROFILE"),
		styleHeading("REGION"),
		styleHeading("CONFIG FILE"),
		styleHeading("NAME"),
	}
	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		tableRows = append(tableRows, []string{
			orDash(row["alias"]),
			orDash(row["default"]),
			orDash(row["profile"]),
			orDash(row["region"]),
			orDash(row["config_file"]),
			orDash(row["name"]),
		})
	}
	printAlignedTable(headers, tableRows, 2)
}

func cmdOCIContextCurrent(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("oci context current", flag.ExitOnError)
	flags := bindOCICommonFlags(fs)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si oci context current [--json]")
		return
	}
	runtime, err := resolveRuntimeFromOCIFlags(flags, false)
	if err != nil {
		fatal(err)
	}
	payload := map[string]any{
		"account_alias": runtime.AccountAlias,
		"profile":       runtime.Profile,
		"config_file":   runtime.ConfigFile,
		"region":        runtime.Region,
		"base_url":      runtime.BaseURL,
		"auth_style":    runtime.AuthStyle,
		"source":        runtime.Source,
		"tenancy_ocid":  runtime.TenancyOCID,
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Current oci context:"), formatOCIContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Source:"), orDash(runtime.Source))
}

func cmdOCIContextUse(args []string) {
	fs := flag.NewFlagSet("oci context use", flag.ExitOnError)
	account := fs.String("account", "", "default account alias")
	profile := fs.String("profile", "", "oci profile")
	configFile := fs.String("config-file", "", "oci config file path")
	region := fs.String("region", "", "oci region")
	baseURL := fs.String("base-url", "", "core api base url")
	vaultPrefix := fs.String("vault-prefix", "", "account env prefix")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si oci context use [--account <alias>] [--profile <name>] [--config-file <path>] [--region <region>] [--base-url <url>] [--vault-prefix <prefix>]")
		return
	}
	settings := loadSettingsOrDefault()
	if value := strings.TrimSpace(*account); value != "" {
		settings.OCI.DefaultAccount = value
	}
	if value := strings.TrimSpace(*profile); value != "" {
		settings.OCI.Profile = value
	}
	if value := strings.TrimSpace(*configFile); value != "" {
		settings.OCI.ConfigFile = value
	}
	if value := strings.TrimSpace(*region); value != "" {
		settings.OCI.Region = value
	}
	if value := strings.TrimSpace(*baseURL); value != "" {
		settings.OCI.APIBaseURL = value
	}
	targetAlias := strings.TrimSpace(settings.OCI.DefaultAccount)
	if value := strings.TrimSpace(*account); value != "" {
		targetAlias = value
	}
	if targetAlias == "" && (strings.TrimSpace(*profile) != "" || strings.TrimSpace(*configFile) != "" || strings.TrimSpace(*region) != "" || strings.TrimSpace(*vaultPrefix) != "") {
		targetAlias = "default"
		settings.OCI.DefaultAccount = targetAlias
	}
	if targetAlias != "" {
		if settings.OCI.Accounts == nil {
			settings.OCI.Accounts = map[string]OCIAccountEntry{}
		}
		entry := settings.OCI.Accounts[targetAlias]
		if value := strings.TrimSpace(*profile); value != "" {
			entry.Profile = value
		}
		if value := strings.TrimSpace(*configFile); value != "" {
			entry.ConfigFile = value
		}
		if value := strings.TrimSpace(*region); value != "" {
			entry.Region = value
		}
		if value := strings.TrimSpace(*vaultPrefix); value != "" {
			entry.VaultPrefix = value
		}
		settings.OCI.Accounts[targetAlias] = entry
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	fmt.Printf("%s default_account=%s profile=%s region=%s base=%s\n",
		styleHeading("Updated oci context:"),
		orDash(settings.OCI.DefaultAccount),
		orDash(settings.OCI.Profile),
		orDash(settings.OCI.Region),
		orDash(settings.OCI.APIBaseURL),
	)
}

func cmdOCIDoctor(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "public": true})
	fs := flag.NewFlagSet("oci doctor", flag.ExitOnError)
	flags := bindOCICommonFlags(fs)
	publicProbe := fs.Bool("public", false, "run unauthenticated public probe")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si oci doctor [--profile <name>] [--config-file <path>] [--region <region>] [--public] [--json]")
		return
	}
	if *publicProbe {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err := runPublicProviderDoctor(ctx, providers.OCICore, strings.TrimSpace(*flags.baseURL))
		if err != nil {
			fatal(err)
		}
		printPublicDoctorResult("oci", result, *jsonOut)
		return
	}
	runtime, err := resolveRuntimeFromOCIFlags(flags, true)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, verifyErr := ociDo(ctx, runtime, ociRequest{
		Method: http.MethodGet,
		Path:   ociIdentityURL(runtime.Region) + "/20160918/availabilityDomains",
		Params: map[string]string{"compartmentId": runtime.TenancyOCID},
	})
	checks := []doctorCheck{
		{Name: "profile", OK: strings.TrimSpace(runtime.Profile) != "", Detail: runtime.Profile},
		{Name: "region", OK: strings.TrimSpace(runtime.Region) != "", Detail: runtime.Region},
		{Name: "tenancy", OK: strings.TrimSpace(runtime.TenancyOCID) != "", Detail: orDash(runtime.TenancyOCID)},
		{Name: "request", OK: verifyErr == nil, Detail: errorOrOK(verifyErr)},
	}
	ok := true
	for _, check := range checks {
		if !check.OK {
			ok = false
		}
	}
	payload := map[string]any{
		"ok":           ok,
		"provider":     "oci_core",
		"base_url":     runtime.BaseURL,
		"profile":      runtime.Profile,
		"region":       runtime.Region,
		"tenancy_ocid": runtime.TenancyOCID,
		"checks":       checks,
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
		fmt.Printf("%s %s\n", styleHeading("OCI doctor:"), styleSuccess("ok"))
	} else {
		fmt.Printf("%s %s\n", styleHeading("OCI doctor:"), styleError("issues found"))
	}
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatOCIContext(runtime))
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

func cmdOCIIdentity(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si oci identity <availability-domains|compartment> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "availability-domains", "ads":
		cmdOCIIdentityAvailabilityDomains(rest)
	case "compartment", "compartments":
		cmdOCIIdentityCompartment(rest)
	default:
		printUnknown("oci identity", sub)
	}
}

func cmdOCIIdentityAvailabilityDomains(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si oci identity availability-domains list [--tenancy <ocid>] [--json]")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	if sub != "list" {
		printUnknown("oci identity availability-domains", sub)
		return
	}
	args = stripeFlagsFirst(args[1:], map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("oci identity availability-domains list", flag.ExitOnError)
	flags := bindOCICommonFlags(fs)
	tenancy := fs.String("tenancy", "", "tenancy ocid")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si oci identity availability-domains list [--tenancy <ocid>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromOCIFlags(flags, true)
	if err != nil {
		fatal(err)
	}
	compartmentID := strings.TrimSpace(*tenancy)
	if compartmentID == "" {
		compartmentID = strings.TrimSpace(runtime.TenancyOCID)
	}
	if compartmentID == "" {
		fatal(fmt.Errorf("tenancy ocid is required (use --tenancy or configure oci profile)"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := ociDo(ctx, runtime, ociRequest{
		Method: http.MethodGet,
		Path:   ociIdentityURL(runtime.Region) + "/20160918/availabilityDomains",
		Params: map[string]string{"compartmentId": compartmentID},
	})
	if err != nil {
		printOCIError(err)
		return
	}
	printOCIResponse(resp, *jsonOut, *raw)
}

func cmdOCIIdentityCompartment(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si oci identity compartment <create>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	if sub != "create" {
		printUnknown("oci identity compartment", sub)
		return
	}
	args = stripeFlagsFirst(args[1:], map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("oci identity compartment create", flag.ExitOnError)
	flags := bindOCICommonFlags(fs)
	parent := fs.String("parent", "", "parent compartment ocid")
	name := fs.String("name", "", "compartment name")
	description := fs.String("description", "", "compartment description")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*parent) == "" || strings.TrimSpace(*name) == "" {
		printUsage("usage: si oci identity compartment create --parent <ocid> --name <name> [--description <text>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromOCIFlags(flags, true)
	if err != nil {
		fatal(err)
	}
	body := map[string]any{
		"compartmentId": strings.TrimSpace(*parent),
		"name":          strings.TrimSpace(*name),
		"description":   firstNonEmpty(strings.TrimSpace(*description), "Managed by si"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := ociDo(ctx, runtime, ociRequest{
		Method:   http.MethodPost,
		Path:     ociIdentityURL(runtime.Region) + "/20160918/compartments",
		JSONBody: body,
	})
	if err != nil {
		printOCIError(err)
		return
	}
	printOCIResponse(resp, *jsonOut, *raw)
}

func cmdOCINetwork(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si oci network <vcn|internet-gateway|route-table|security-list|subnet> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "vcn":
		cmdOCINetworkVCN(rest)
	case "internet-gateway", "igw":
		cmdOCINetworkInternetGateway(rest)
	case "route-table", "rt":
		cmdOCINetworkRouteTable(rest)
	case "security-list", "seclist":
		cmdOCINetworkSecurityList(rest)
	case "subnet":
		cmdOCINetworkSubnet(rest)
	default:
		printUnknown("oci network", sub)
	}
}

func cmdOCINetworkVCN(args []string) {
	if len(args) == 0 || strings.ToLower(strings.TrimSpace(args[0])) != "create" {
		printUsage("usage: si oci network vcn create --compartment <ocid> --cidr <x.x.x.x/nn> --display-name <name> [--dns-label <label>] [--json]")
		return
	}
	args = args[1:]
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("oci network vcn create", flag.ExitOnError)
	flags := bindOCICommonFlags(fs)
	compartment := fs.String("compartment", "", "compartment ocid")
	cidr := fs.String("cidr", "10.0.0.0/16", "cidr block")
	displayName := fs.String("display-name", "oracular-vcn", "display name")
	dnsLabel := fs.String("dns-label", "oracularvcn", "dns label")
	jsonBody := fs.String("json-body", "", "json request body override")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*compartment) == "" {
		printUsage("usage: si oci network vcn create --compartment <ocid> --cidr <x.x.x.x/nn> --display-name <name> [--dns-label <label>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromOCIFlags(flags, true)
	if err != nil {
		fatal(err)
	}
	body := map[string]any{
		"cidrBlocks":    []string{strings.TrimSpace(*cidr)},
		"compartmentId": strings.TrimSpace(*compartment),
		"displayName":   strings.TrimSpace(*displayName),
		"dnsLabel":      strings.TrimSpace(*dnsLabel),
	}
	if strings.TrimSpace(*jsonBody) != "" {
		if err := json.Unmarshal([]byte(strings.TrimSpace(*jsonBody)), &body); err != nil {
			fatal(fmt.Errorf("invalid --json-body: %w", err))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := ociDo(ctx, runtime, ociRequest{Method: http.MethodPost, Path: "/20160918/vcns", JSONBody: body})
	if err != nil {
		printOCIError(err)
		return
	}
	printOCIResponse(resp, *jsonOut, *raw)
}

func cmdOCINetworkInternetGateway(args []string) {
	if len(args) == 0 || strings.ToLower(strings.TrimSpace(args[0])) != "create" {
		printUsage("usage: si oci network internet-gateway create --compartment <ocid> --vcn-id <ocid> [--display-name <name>] [--enabled] [--json]")
		return
	}
	args = args[1:]
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("oci network internet-gateway create", flag.ExitOnError)
	flags := bindOCICommonFlags(fs)
	compartment := fs.String("compartment", "", "compartment ocid")
	vcnID := fs.String("vcn-id", "", "vcn ocid")
	displayName := fs.String("display-name", "oracular-igw", "display name")
	enabled := fs.Bool("enabled", true, "enable internet gateway")
	jsonBody := fs.String("json-body", "", "json request body override")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*compartment) == "" || strings.TrimSpace(*vcnID) == "" {
		printUsage("usage: si oci network internet-gateway create --compartment <ocid> --vcn-id <ocid> [--display-name <name>] [--enabled] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromOCIFlags(flags, true)
	if err != nil {
		fatal(err)
	}
	body := map[string]any{
		"compartmentId": strings.TrimSpace(*compartment),
		"vcnId":         strings.TrimSpace(*vcnID),
		"displayName":   strings.TrimSpace(*displayName),
		"isEnabled":     *enabled,
	}
	if strings.TrimSpace(*jsonBody) != "" {
		if err := json.Unmarshal([]byte(strings.TrimSpace(*jsonBody)), &body); err != nil {
			fatal(fmt.Errorf("invalid --json-body: %w", err))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := ociDo(ctx, runtime, ociRequest{Method: http.MethodPost, Path: "/20160918/internetGateways", JSONBody: body})
	if err != nil {
		printOCIError(err)
		return
	}
	printOCIResponse(resp, *jsonOut, *raw)
}

func cmdOCINetworkRouteTable(args []string) {
	if len(args) == 0 || strings.ToLower(strings.TrimSpace(args[0])) != "create" {
		printUsage("usage: si oci network route-table create --compartment <ocid> --vcn-id <ocid> --target <network_entity_ocid> [--display-name <name>] [--json]")
		return
	}
	args = args[1:]
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("oci network route-table create", flag.ExitOnError)
	flags := bindOCICommonFlags(fs)
	compartment := fs.String("compartment", "", "compartment ocid")
	vcnID := fs.String("vcn-id", "", "vcn ocid")
	target := fs.String("target", "", "network entity ocid for 0.0.0.0/0 route")
	displayName := fs.String("display-name", "oracular-rt", "display name")
	jsonBody := fs.String("json-body", "", "json request body override")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*compartment) == "" || strings.TrimSpace(*vcnID) == "" || strings.TrimSpace(*target) == "" {
		printUsage("usage: si oci network route-table create --compartment <ocid> --vcn-id <ocid> --target <network_entity_ocid> [--display-name <name>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromOCIFlags(flags, true)
	if err != nil {
		fatal(err)
	}
	body := map[string]any{
		"compartmentId": strings.TrimSpace(*compartment),
		"vcnId":         strings.TrimSpace(*vcnID),
		"displayName":   strings.TrimSpace(*displayName),
		"routeRules": []map[string]any{{
			"destination":     "0.0.0.0/0",
			"destinationType": "CIDR_BLOCK",
			"networkEntityId": strings.TrimSpace(*target),
		}},
	}
	if strings.TrimSpace(*jsonBody) != "" {
		if err := json.Unmarshal([]byte(strings.TrimSpace(*jsonBody)), &body); err != nil {
			fatal(fmt.Errorf("invalid --json-body: %w", err))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := ociDo(ctx, runtime, ociRequest{Method: http.MethodPost, Path: "/20160918/routeTables", JSONBody: body})
	if err != nil {
		printOCIError(err)
		return
	}
	printOCIResponse(resp, *jsonOut, *raw)
}

func cmdOCINetworkSecurityList(args []string) {
	if len(args) == 0 || strings.ToLower(strings.TrimSpace(args[0])) != "create" {
		printUsage("usage: si oci network security-list create --compartment <ocid> --vcn-id <ocid> [--ssh-port <port>] [--display-name <name>] [--json]")
		return
	}
	args = args[1:]
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("oci network security-list create", flag.ExitOnError)
	flags := bindOCICommonFlags(fs)
	compartment := fs.String("compartment", "", "compartment ocid")
	vcnID := fs.String("vcn-id", "", "vcn ocid")
	sshPort := fs.Int("ssh-port", 22, "ssh port")
	displayName := fs.String("display-name", "oracular-sec", "display name")
	jsonBody := fs.String("json-body", "", "json request body override")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*compartment) == "" || strings.TrimSpace(*vcnID) == "" {
		printUsage("usage: si oci network security-list create --compartment <ocid> --vcn-id <ocid> [--ssh-port <port>] [--display-name <name>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromOCIFlags(flags, true)
	if err != nil {
		fatal(err)
	}
	body := map[string]any{
		"compartmentId": strings.TrimSpace(*compartment),
		"vcnId":         strings.TrimSpace(*vcnID),
		"displayName":   strings.TrimSpace(*displayName),
		"egressSecurityRules": []map[string]any{{
			"destination":     "0.0.0.0/0",
			"destinationType": "CIDR_BLOCK",
			"protocol":        "all",
		}},
		"ingressSecurityRules": []map[string]any{
			{
				"description": "SSH",
				"protocol":    "6",
				"source":      "0.0.0.0/0",
				"sourceType":  "CIDR_BLOCK",
				"tcpOptions": map[string]any{
					"min": *sshPort,
					"max": *sshPort,
				},
			},
			{
				"description": "HTTP",
				"protocol":    "6",
				"source":      "0.0.0.0/0",
				"sourceType":  "CIDR_BLOCK",
				"tcpOptions": map[string]any{
					"min": 80,
					"max": 80,
				},
			},
			{
				"description": "HTTPS",
				"protocol":    "6",
				"source":      "0.0.0.0/0",
				"sourceType":  "CIDR_BLOCK",
				"tcpOptions": map[string]any{
					"min": 443,
					"max": 443,
				},
			},
		},
	}
	if strings.TrimSpace(*jsonBody) != "" {
		if err := json.Unmarshal([]byte(strings.TrimSpace(*jsonBody)), &body); err != nil {
			fatal(fmt.Errorf("invalid --json-body: %w", err))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := ociDo(ctx, runtime, ociRequest{Method: http.MethodPost, Path: "/20160918/securityLists", JSONBody: body})
	if err != nil {
		printOCIError(err)
		return
	}
	printOCIResponse(resp, *jsonOut, *raw)
}

func cmdOCINetworkSubnet(args []string) {
	if len(args) == 0 || strings.ToLower(strings.TrimSpace(args[0])) != "create" {
		printUsage("usage: si oci network subnet create --compartment <ocid> --vcn-id <ocid> --route-table-id <ocid> --security-list-id <ocid> --dhcp-options-id <ocid> [--cidr 10.0.1.0/24] [--display-name <name>] [--dns-label <label>] [--public-ip] [--json]")
		return
	}
	args = args[1:]
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("oci network subnet create", flag.ExitOnError)
	flags := bindOCICommonFlags(fs)
	compartment := fs.String("compartment", "", "compartment ocid")
	vcnID := fs.String("vcn-id", "", "vcn ocid")
	routeTableID := fs.String("route-table-id", "", "route table ocid")
	securityListID := fs.String("security-list-id", "", "security list ocid")
	dhcpOptionsID := fs.String("dhcp-options-id", "", "dhcp options ocid")
	cidr := fs.String("cidr", "10.0.1.0/24", "cidr block")
	displayName := fs.String("display-name", "oracular-subnet", "display name")
	dnsLabel := fs.String("dns-label", "oracularsub", "dns label")
	publicIP := fs.Bool("public-ip", true, "assign public ip on vnic")
	jsonBody := fs.String("json-body", "", "json request body override")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*compartment) == "" || strings.TrimSpace(*vcnID) == "" || strings.TrimSpace(*routeTableID) == "" || strings.TrimSpace(*securityListID) == "" || strings.TrimSpace(*dhcpOptionsID) == "" {
		printUsage("usage: si oci network subnet create --compartment <ocid> --vcn-id <ocid> --route-table-id <ocid> --security-list-id <ocid> --dhcp-options-id <ocid> [--cidr 10.0.1.0/24] [--display-name <name>] [--dns-label <label>] [--public-ip] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromOCIFlags(flags, true)
	if err != nil {
		fatal(err)
	}
	body := map[string]any{
		"cidrBlock":              strings.TrimSpace(*cidr),
		"compartmentId":          strings.TrimSpace(*compartment),
		"vcnId":                  strings.TrimSpace(*vcnID),
		"displayName":            strings.TrimSpace(*displayName),
		"dnsLabel":               strings.TrimSpace(*dnsLabel),
		"prohibitPublicIpOnVnic": !*publicIP,
		"routeTableId":           strings.TrimSpace(*routeTableID),
		"securityListIds":        []string{strings.TrimSpace(*securityListID)},
		"dhcpOptionsId":          strings.TrimSpace(*dhcpOptionsID),
	}
	if strings.TrimSpace(*jsonBody) != "" {
		if err := json.Unmarshal([]byte(strings.TrimSpace(*jsonBody)), &body); err != nil {
			fatal(fmt.Errorf("invalid --json-body: %w", err))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := ociDo(ctx, runtime, ociRequest{Method: http.MethodPost, Path: "/20160918/subnets", JSONBody: body})
	if err != nil {
		printOCIError(err)
		return
	}
	printOCIResponse(resp, *jsonOut, *raw)
}

func cmdOCICompute(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si oci compute <image|instance|availability-domains> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "availability-domains", "ads":
		cmdOCIIdentityAvailabilityDomains(append([]string{"list"}, rest...))
	case "image", "images":
		cmdOCIComputeImage(rest)
	case "instance", "instances":
		cmdOCIComputeInstance(rest)
	default:
		printUnknown("oci compute", sub)
	}
}

func cmdOCIComputeImage(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si oci compute image latest-ubuntu --tenancy <ocid> --shape <shape> [--os-version 24.04] [--json]")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	if sub != "latest-ubuntu" {
		printUnknown("oci compute image", sub)
		return
	}
	args = stripeFlagsFirst(args[1:], map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("oci compute image latest-ubuntu", flag.ExitOnError)
	flags := bindOCICommonFlags(fs)
	tenancy := fs.String("tenancy", "", "tenancy ocid")
	shape := fs.String("shape", "VM.Standard.A1.Flex", "compute shape")
	osVersion := fs.String("os-version", "24.04", "operating system version")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si oci compute image latest-ubuntu --tenancy <ocid> --shape <shape> [--os-version 24.04] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromOCIFlags(flags, true)
	if err != nil {
		fatal(err)
	}
	compartmentID := strings.TrimSpace(*tenancy)
	if compartmentID == "" {
		compartmentID = runtime.TenancyOCID
	}
	if compartmentID == "" {
		fatal(fmt.Errorf("tenancy ocid is required"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := ociDo(ctx, runtime, ociRequest{
		Method: http.MethodGet,
		Path:   "/20160918/images",
		Params: map[string]string{
			"compartmentId":          compartmentID,
			"operatingSystem":        "Canonical Ubuntu",
			"operatingSystemVersion": strings.TrimSpace(*osVersion),
			"shape":                  strings.TrimSpace(*shape),
			"sortBy":                 "TIMECREATED",
			"sortOrder":              "DESC",
		},
	})
	if err != nil {
		printOCIError(err)
		return
	}
	printOCIResponse(resp, *jsonOut, *raw)
}

func cmdOCIComputeInstance(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si oci compute instance create ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	if sub != "create" {
		printUnknown("oci compute instance", sub)
		return
	}
	args = stripeFlagsFirst(args[1:], map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("oci compute instance create", flag.ExitOnError)
	flags := bindOCICommonFlags(fs)
	compartment := fs.String("compartment", "", "compartment ocid")
	ad := fs.String("ad", "", "availability domain")
	subnetID := fs.String("subnet-id", "", "subnet ocid")
	displayName := fs.String("display-name", "oracular-vps", "display name")
	shape := fs.String("shape", "VM.Standard.A1.Flex", "shape")
	ocpus := fs.Int("ocpus", 4, "ocpus")
	memoryGB := fs.Int("memory-gb", 20, "memory in GB")
	imageID := fs.String("image-id", "", "image ocid")
	bootVolumeGB := fs.Int("boot-volume-gb", 150, "boot volume size in GB")
	sshPublicKey := fs.String("ssh-public-key", "", "ssh public key content")
	userDataB64 := fs.String("user-data-b64", "", "base64 cloud-init user data")
	assignPublicIP := fs.Bool("assign-public-ip", true, "assign public ip")
	jsonBody := fs.String("json-body", "", "json request body override")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si oci compute instance create --compartment <ocid> --ad <ad> --subnet-id <ocid> --image-id <ocid> [--ssh-public-key <value>] [--user-data-b64 <value>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromOCIFlags(flags, true)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(*compartment) == "" || strings.TrimSpace(*ad) == "" || strings.TrimSpace(*subnetID) == "" || strings.TrimSpace(*imageID) == "" {
		fatal(fmt.Errorf("--compartment, --ad, --subnet-id, and --image-id are required"))
	}
	metadata := map[string]any{}
	if value := strings.TrimSpace(*sshPublicKey); value != "" {
		metadata["ssh_authorized_keys"] = value
	}
	if value := strings.TrimSpace(*userDataB64); value != "" {
		metadata["user_data"] = value
	}
	body := map[string]any{
		"availabilityDomain": strings.TrimSpace(*ad),
		"compartmentId":      strings.TrimSpace(*compartment),
		"displayName":        strings.TrimSpace(*displayName),
		"shape":              strings.TrimSpace(*shape),
		"shapeConfig": map[string]any{
			"ocpus":       *ocpus,
			"memoryInGBs": *memoryGB,
		},
		"sourceDetails": map[string]any{
			"sourceType":          "image",
			"sourceId":            strings.TrimSpace(*imageID),
			"bootVolumeSizeInGBs": *bootVolumeGB,
		},
		"createVnicDetails": map[string]any{
			"assignPublicIp": *assignPublicIP,
			"displayName":    "oracular-vnic",
			"subnetId":       strings.TrimSpace(*subnetID),
		},
	}
	if len(metadata) > 0 {
		body["metadata"] = metadata
	}
	if strings.TrimSpace(*jsonBody) != "" {
		if err := json.Unmarshal([]byte(strings.TrimSpace(*jsonBody)), &body); err != nil {
			fatal(fmt.Errorf("invalid --json-body: %w", err))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := ociDo(ctx, runtime, ociRequest{Method: http.MethodPost, Path: "/20160918/instances", JSONBody: body})
	if err != nil {
		printOCIError(err)
		return
	}
	printOCIResponse(resp, *jsonOut, *raw)
}

func cmdOCIOracular(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si oci oracular <cloud-init|tenancy>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "cloud-init", "cloudinit":
		cmdOCIOracularCloudInit(rest)
	case "tenancy":
		cmdOCIOracularTenancy(rest)
	default:
		printUnknown("oci oracular", sub)
	}
}

func cmdOCIOracularCloudInit(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("oci oracular cloud-init", flag.ExitOnError)
	sshPort := fs.Int("ssh-port", 7129, "ssh port")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si oci oracular cloud-init [--ssh-port <port>] [--json]")
		return
	}
	userData, err := ociCloudInitUserData(*sshPort)
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{"ssh_port": *sshPort, "user_data_b64": userData})
		return
	}
	fmt.Println(userData)
}

func cmdOCIOracularTenancy(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("oci oracular tenancy", flag.ExitOnError)
	flags := bindOCICommonFlags(fs)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si oci oracular tenancy [--profile <name>] [--config-file <path>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromOCIFlags(flags, false)
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{"profile": runtime.Profile, "config_file": runtime.ConfigFile, "tenancy_ocid": runtime.TenancyOCID})
		return
	}
	fmt.Println(runtime.TenancyOCID)
}

func cmdOCIRaw(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("oci raw", flag.ExitOnError)
	flags := bindOCICommonFlags(fs)
	method := fs.String("method", http.MethodGet, "http method")
	path := fs.String("path", "", "api path (absolute url or path)")
	body := fs.String("body", "", "raw request body")
	jsonBody := fs.String("json-body", "", "json request body")
	service := fs.String("service", "core", "service endpoint (core|identity)")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	headers := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	fs.Var(&headers, "header", "header key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*path) == "" {
		printUsage("usage: si oci raw --method <GET|POST|PUT|DELETE> --path <api-path> [--service <core|identity>] [--param key=value] [--body raw|--json-body '{...}'] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromOCIFlags(flags, strings.ToLower(strings.TrimSpace(*flags.authStyle)) != "none")
	if err != nil {
		fatal(err)
	}
	pathValue := strings.TrimSpace(*path)
	if !strings.HasPrefix(pathValue, "http://") && !strings.HasPrefix(pathValue, "https://") {
		switch strings.ToLower(strings.TrimSpace(*service)) {
		case "identity", "iam":
			pathValue = ociIdentityURL(runtime.Region) + ensureLeadingSlash(pathValue)
		default:
			pathValue = runtime.BaseURL + ensureLeadingSlash(pathValue)
		}
	}
	var payload any
	if strings.TrimSpace(*jsonBody) != "" {
		payload = parseOCIJSONBody(strings.TrimSpace(*jsonBody), nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := ociDo(ctx, runtime, ociRequest{
		Method:   strings.ToUpper(strings.TrimSpace(*method)),
		Path:     pathValue,
		Params:   parseOCIParams(params),
		Headers:  parseOCIParams(headers),
		RawBody:  strings.TrimSpace(*body),
		JSONBody: payload,
	})
	if err != nil {
		printOCIError(err)
		return
	}
	printOCIResponse(resp, *jsonOut, *raw)
}

type ociCommonFlags struct {
	account    *string
	profile    *string
	configFile *string
	region     *string
	baseURL    *string
	authStyle  *string
}

func bindOCICommonFlags(fs *flag.FlagSet) ociCommonFlags {
	return ociCommonFlags{
		account:    fs.String("account", "", "account alias"),
		profile:    fs.String("profile", "", "oci profile name"),
		configFile: fs.String("config-file", "", "oci config file path"),
		region:     fs.String("region", "", "oci region"),
		baseURL:    fs.String("base-url", "", "core api base url"),
		authStyle:  fs.String("auth", "signature", "auth style (signature|none)"),
	}
}

func resolveRuntimeFromOCIFlags(flags ociCommonFlags, requireAuth bool) (ociRuntimeContext, error) {
	return resolveOCIRuntimeContext(ociRuntimeContextInput{
		AccountFlag:    strings.TrimSpace(valueOrEmpty(flags.account)),
		ProfileFlag:    strings.TrimSpace(valueOrEmpty(flags.profile)),
		ConfigFileFlag: strings.TrimSpace(valueOrEmpty(flags.configFile)),
		RegionFlag:     strings.TrimSpace(valueOrEmpty(flags.region)),
		BaseURLFlag:    strings.TrimSpace(valueOrEmpty(flags.baseURL)),
		AuthStyleFlag:  strings.TrimSpace(valueOrEmpty(flags.authStyle)),
		RequireAuth:    requireAuth,
	})
}

func resolveOCIRuntimeContext(input ociRuntimeContextInput) (ociRuntimeContext, error) {
	settings := loadSettingsOrDefault()
	alias, account := resolveOCIAccountSelection(settings, input.AccountFlag)

	authStyle := strings.ToLower(strings.TrimSpace(input.AuthStyleFlag))
	if authStyle == "" {
		authStyle = "signature"
	}
	switch authStyle {
	case "signature", "none":
	default:
		return ociRuntimeContext{}, fmt.Errorf("invalid oci auth style %q (expected signature|none)", authStyle)
	}

	profile := strings.TrimSpace(input.ProfileFlag)
	if profile == "" {
		profile = strings.TrimSpace(account.Profile)
	}
	if profile == "" {
		profile = strings.TrimSpace(settings.OCI.Profile)
	}
	if profile == "" {
		profile = strings.TrimSpace(os.Getenv("OCI_CLI_PROFILE"))
	}
	if profile == "" {
		profile = "DEFAULT"
	}

	configFile := strings.TrimSpace(input.ConfigFileFlag)
	if configFile == "" {
		configFile = strings.TrimSpace(account.ConfigFile)
	}
	if configFile == "" {
		configFile = strings.TrimSpace(settings.OCI.ConfigFile)
	}
	if configFile == "" {
		configFile = strings.TrimSpace(os.Getenv("OCI_CONFIG_FILE"))
	}
	if configFile == "" {
		configFile = "~/.oci/config"
	}
	configFile = expandTilde(configFile)

	region := strings.TrimSpace(input.RegionFlag)
	if region == "" {
		region = strings.TrimSpace(account.Region)
	}
	if region == "" {
		region = strings.TrimSpace(settings.OCI.Region)
	}
	if region == "" {
		region = strings.TrimSpace(os.Getenv("OCI_CLI_REGION"))
	}

	baseURL := strings.TrimSpace(input.BaseURLFlag)
	if baseURL == "" {
		baseURL = strings.TrimSpace(settings.OCI.APIBaseURL)
	}

	source := []string{}
	tenancyOCID := firstNonEmpty(strings.TrimSpace(account.TenancyOCID), resolveEnvReference(account.TenancyOCIDEnv))
	userOCID := firstNonEmpty(strings.TrimSpace(account.UserOCID), resolveEnvReference(account.UserOCIDEnv))
	fingerprint := firstNonEmpty(strings.TrimSpace(account.Fingerprint), resolveEnvReference(account.FingerprintEnv))
	privateKeyPath := firstNonEmpty(strings.TrimSpace(account.PrivateKeyPath), resolveEnvReference(account.PrivateKeyPathEnv))
	passphrase := resolveEnvReference(account.PassphraseEnv)
	if authStyle == "signature" {
		profileValues, err := parseOCIConfigProfile(configFile, profile)
		if err != nil {
			return ociRuntimeContext{}, err
		}
		source = append(source, "profile:"+profile)
		if tenancyOCID == "" {
			tenancyOCID = strings.TrimSpace(profileValues["tenancy"])
		}
		if userOCID == "" {
			userOCID = strings.TrimSpace(profileValues["user"])
		}
		if fingerprint == "" {
			fingerprint = strings.TrimSpace(profileValues["fingerprint"])
		}
		if privateKeyPath == "" {
			privateKeyPath = strings.TrimSpace(profileValues["key_file"])
		}
		if passphrase == "" {
			passphrase = strings.TrimSpace(profileValues["pass_phrase"])
		}
		if region == "" {
			region = strings.TrimSpace(profileValues["region"])
		}
	}
	if region == "" {
		region = "us-ashburn-1"
	}
	privateKeyPath = expandTilde(strings.TrimSpace(privateKeyPath))
	if privateKeyPath != "" && !filepath.IsAbs(privateKeyPath) {
		privateKeyPath = filepath.Join(filepath.Dir(configFile), privateKeyPath)
	}
	privateKeyPath = strings.TrimSpace(privateKeyPath)
	if baseURL == "" {
		baseURL = ociCoreURL(region)
	}
	if input.RequireAuth && authStyle != "signature" {
		return ociRuntimeContext{}, fmt.Errorf("oci signature auth is required for this command (set --auth signature)")
	}
	var privateKey *rsa.PrivateKey
	if authStyle == "signature" {
		var err error
		privateKey, err = loadOCIRSAPrivateKey(privateKeyPath, passphrase)
		if err != nil {
			return ociRuntimeContext{}, err
		}
	}
	if input.RequireAuth && privateKey == nil {
		return ociRuntimeContext{}, fmt.Errorf("oci signature private key is not configured")
	}
	return ociRuntimeContext{
		AccountAlias:   strings.TrimSpace(alias),
		Profile:        strings.TrimSpace(profile),
		ConfigFile:     strings.TrimSpace(configFile),
		Region:         strings.TrimSpace(region),
		BaseURL:        strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		AuthStyle:      authStyle,
		TenancyOCID:    strings.TrimSpace(tenancyOCID),
		UserOCID:       strings.TrimSpace(userOCID),
		Fingerprint:    strings.TrimSpace(fingerprint),
		PrivateKey:     privateKey,
		PrivateKeyPath: privateKeyPath,
		Source:         strings.Join(source, ","),
		LogPath:        resolveOCILogPath(settings),
	}, nil
}

func resolveEnvReference(envName string) string {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv(envName))
}

func parseOCIConfigProfile(configFile string, profile string) (map[string]string, error) {
	configFile = strings.TrimSpace(configFile)
	if configFile == "" {
		return nil, fmt.Errorf("oci config file path is required")
	}
	raw, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("read oci config %q: %w", configFile, err)
	}
	profile = strings.TrimSpace(profile)
	if profile == "" {
		profile = "DEFAULT"
	}
	profiles := map[string]map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	current := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.Contains(line, "]") {
			section := strings.TrimSpace(line[1:strings.Index(line, "]")])
			current = section
			if _, ok := profiles[current]; !ok {
				profiles[current] = map[string]string{}
			}
			continue
		}
		if current == "" {
			continue
		}
		splitIdx := strings.IndexAny(line, "=:")
		if splitIdx <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:splitIdx]))
		value := strings.TrimSpace(line[splitIdx+1:])
		value = strings.Trim(value, `"'`)
		profiles[current][key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse oci config %q: %w", configFile, err)
	}
	if values, ok := profiles[profile]; ok {
		return values, nil
	}
	return nil, fmt.Errorf("oci profile %q not found in %s", profile, configFile)
}

func loadOCIRSAPrivateKey(path string, passphrase string) (*rsa.PrivateKey, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("oci private key path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read oci private key %q: %w", path, err)
	}
	var block *pem.Block
	remaining := raw
	for {
		block, remaining = pem.Decode(remaining)
		if block == nil {
			return nil, fmt.Errorf("no private key PEM block found in %s", path)
		}
		if strings.Contains(block.Type, "PRIVATE KEY") {
			break
		}
	}
	keyDER := block.Bytes
	if x509.IsEncryptedPEMBlock(block) {
		if strings.TrimSpace(passphrase) == "" {
			return nil, fmt.Errorf("encrypted oci private key requires passphrase")
		}
		decoded, err := x509.DecryptPEMBlock(block, []byte(passphrase))
		if err != nil {
			return nil, fmt.Errorf("decrypt oci private key: %w", err)
		}
		keyDER = decoded
	}
	if key, err := x509.ParsePKCS1PrivateKey(keyDER); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(keyDER)
	if err != nil {
		return nil, fmt.Errorf("parse oci private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("oci private key must be RSA")
	}
	return key, nil
}

func resolveOCIAccountSelection(settings Settings, accountFlag string) (string, OCIAccountEntry) {
	selected := strings.TrimSpace(accountFlag)
	if selected == "" {
		selected = strings.TrimSpace(settings.OCI.DefaultAccount)
	}
	if selected == "" {
		selected = strings.TrimSpace(os.Getenv("OCI_DEFAULT_ACCOUNT"))
	}
	if selected == "" {
		aliases := ociAccountAliases(settings)
		if len(aliases) == 1 {
			selected = aliases[0]
		}
	}
	if selected == "" {
		return "", OCIAccountEntry{}
	}
	if entry, ok := settings.OCI.Accounts[selected]; ok {
		return selected, entry
	}
	return selected, OCIAccountEntry{}
}

func ociAccountAliases(settings Settings) []string {
	if len(settings.OCI.Accounts) == 0 {
		return nil
	}
	out := make([]string, 0, len(settings.OCI.Accounts))
	for alias := range settings.OCI.Accounts {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

func resolveOCILogPath(settings Settings) string {
	if value := strings.TrimSpace(os.Getenv("SI_OCI_LOG_FILE")); value != "" {
		return value
	}
	if value := strings.TrimSpace(settings.OCI.LogFile); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".si", "logs", "oci-core.log")
}

func formatOCIContext(runtime ociRuntimeContext) string {
	account := strings.TrimSpace(runtime.AccountAlias)
	if account == "" {
		account = "(default)"
	}
	return fmt.Sprintf("account=%s profile=%s region=%s auth=%s base=%s", account, runtime.Profile, runtime.Region, runtime.AuthStyle, runtime.BaseURL)
}

func ociCoreURL(region string) string {
	region = strings.TrimSpace(region)
	if region == "" {
		region = "us-ashburn-1"
	}
	return "https://iaas." + region + ".oraclecloud.com"
}

func ociIdentityURL(region string) string {
	region = strings.TrimSpace(region)
	if region == "" {
		region = "us-ashburn-1"
	}
	return "https://identity." + region + ".oraclecloud.com"
}

func ociKeyID(runtime ociRuntimeContext) string {
	return strings.TrimSpace(runtime.TenancyOCID) + "/" + strings.TrimSpace(runtime.UserOCID) + "/" + strings.TrimSpace(runtime.Fingerprint)
}

func ociSignRequest(httpReq *http.Request, runtime ociRuntimeContext, body []byte) error {
	if runtime.PrivateKey == nil {
		return fmt.Errorf("oci signature auth requires a private key")
	}
	keyID := ociKeyID(runtime)
	if strings.Count(keyID, "/") != 2 || strings.Contains(keyID, "//") {
		return fmt.Errorf("oci signature auth requires tenancy/user/fingerprint values")
	}
	dateValue := time.Now().UTC().Format(http.TimeFormat)
	httpReq.Header.Set("Date", dateValue)

	host := strings.TrimSpace(httpReq.URL.Host)
	if host == "" {
		return fmt.Errorf("oci request host is required")
	}
	httpReq.Host = host

	headersToSign := []string{"date", "(request-target)", "host"}
	method := strings.ToLower(strings.TrimSpace(httpReq.Method))
	if method == "post" || method == "put" || method == "patch" {
		if strings.TrimSpace(httpReq.Header.Get("Content-Type")) == "" {
			httpReq.Header.Set("Content-Type", "application/json")
		}
		sum := sha256.Sum256(body)
		httpReq.Header.Set("X-Content-Sha256", base64.StdEncoding.EncodeToString(sum[:]))
		httpReq.ContentLength = int64(len(body))
		httpReq.Header.Set("Content-Length", strconv.Itoa(len(body)))
		headersToSign = append(headersToSign, "content-length", "content-type", "x-content-sha256")
	}

	requestTarget := strings.ToLower(method) + " " + ociPathAndQuery(httpReq.URL)
	signingParts := make([]string, 0, len(headersToSign))
	for _, headerName := range headersToSign {
		value := ""
		switch headerName {
		case "(request-target)":
			value = requestTarget
		case "host":
			value = strings.ToLower(host)
		default:
			value = strings.TrimSpace(httpReq.Header.Get(headerName))
		}
		signingParts = append(signingParts, headerName+": "+value)
	}
	signingString := strings.Join(signingParts, "\n")
	digest := sha256.Sum256([]byte(signingString))
	signatureBytes, err := rsa.SignPKCS1v15(rand.Reader, runtime.PrivateKey, crypto.SHA256, digest[:])
	if err != nil {
		return fmt.Errorf("oci request signing failed: %w", err)
	}
	httpReq.Header.Set(
		"Authorization",
		fmt.Sprintf(
			`Signature version="1",keyId="%s",algorithm="rsa-sha256",headers="%s",signature="%s"`,
			keyID,
			strings.Join(headersToSign, " "),
			base64.StdEncoding.EncodeToString(signatureBytes),
		),
	)
	return nil
}

func ociPathAndQuery(u *url.URL) string {
	if u == nil {
		return "/"
	}
	path := strings.TrimSpace(u.EscapedPath())
	if path == "" {
		path = "/"
	}
	if strings.TrimSpace(u.RawQuery) == "" {
		return path
	}
	return path + "?" + u.RawQuery
}

func ociDo(ctx context.Context, runtime ociRuntimeContext, req ociRequest) (ociResponse, error) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		return ociResponse{}, fmt.Errorf("request path is required")
	}
	endpoint, err := resolveOCIURL(runtime.BaseURL, path, req.Params)
	if err != nil {
		return ociResponse{}, err
	}
	providerID := providers.OCICore
	return integrationruntime.DoHTTP(ctx, integrationruntime.HTTPExecutorOptions[ociResponse]{
		Provider:    providerID,
		Subject:     runtime.Profile,
		Method:      method,
		RequestPath: path,
		Endpoint:    endpoint,
		MaxRetries:  2,
		Client:      httpx.SharedClient(60 * time.Second),
		BuildRequest: func(callCtx context.Context, callMethod string, callEndpoint string) (*http.Request, error) {
			bodyBytes := []byte(nil)
			if strings.TrimSpace(req.RawBody) != "" {
				bodyBytes = []byte(req.RawBody)
			} else if req.JSONBody != nil {
				rawBody, marshalErr := json.Marshal(req.JSONBody)
				if marshalErr != nil {
					return nil, marshalErr
				}
				bodyBytes = rawBody
			}
			bodyReader := io.Reader(nil)
			if bodyBytes != nil {
				bodyReader = bytes.NewReader(bodyBytes)
			}
			httpReq, reqErr := http.NewRequestWithContext(callCtx, callMethod, callEndpoint, bodyReader)
			if reqErr != nil {
				return nil, reqErr
			}
			spec := providers.Resolve(providerID)
			httpReq.Header.Set("Accept", firstNonEmpty(spec.Accept, "application/json"))
			httpReq.Header.Set("User-Agent", firstNonEmpty(spec.UserAgent, "si-oci-core/1.0"))
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
			if runtime.AuthStyle == "signature" {
				if signErr := ociSignRequest(httpReq, runtime, bodyBytes); signErr != nil {
					return nil, signErr
				}
			}
			return httpReq, nil
		},
		NormalizeResponse:  normalizeOCIResponse,
		StatusCode:         func(resp ociResponse) int { return resp.StatusCode },
		NormalizeHTTPError: normalizeOCIHTTPError,
		IsRetryableNetwork: func(method string, _ error) bool { return netpolicy.IsSafeMethod(method) },
		IsRetryableHTTP: func(method string, statusCode int, _ http.Header, _ string) bool {
			if !netpolicy.IsSafeMethod(method) {
				return false
			}
			return statusCode == http.StatusTooManyRequests || statusCode >= 500
		},
		OnError: func(_ int, callErr error, duration time.Duration) {
			ociLogEvent(runtime.LogPath, map[string]any{
				"event":       "error",
				"profile":     runtime.Profile,
				"region":      runtime.Region,
				"method":      method,
				"path":        sanitizeURL(endpoint),
				"duration_ms": duration.Milliseconds(),
				"error":       redactOCISensitive(callErr.Error()),
			})
		},
	})
}

func resolveOCIURL(baseURL string, path string, params map[string]string) (string, error) {
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

func normalizeOCIResponse(httpResp *http.Response, body string) ociResponse {
	resp := ociResponse{
		StatusCode: httpResp.StatusCode,
		Status:     strings.TrimSpace(httpResp.Status),
		RequestID:  firstOCIHeader(httpResp.Header, "opc-request-id", "opc-client-request-id"),
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
	case []any:
		resp.List = convertAnyListToMapList(payload)
	}
	return resp
}

func normalizeOCIHTTPError(statusCode int, headers http.Header, body string) error {
	details := &ociAPIErrorDetails{
		StatusCode: statusCode,
		RequestID:  firstOCIHeader(headers, "opc-request-id", "opc-client-request-id"),
		RawBody:    strings.TrimSpace(body),
		Message:    "oci request failed",
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &parsed); err == nil {
		if value := strings.TrimSpace(stringifyWorkOSAny(parsed["code"])); value != "" {
			details.Code = value
		}
		if value := strings.TrimSpace(stringifyWorkOSAny(parsed["message"])); value != "" {
			details.Message = value
		}
	}
	if details.Message == "oci request failed" {
		details.Message = firstNonEmpty(strings.TrimSpace(body), http.StatusText(statusCode))
	}
	return details
}

func firstOCIHeader(headers http.Header, keys ...string) string {
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

func parseOCIParams(values []string) map[string]string {
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

func parseOCIJSONBody(raw string, fallback []string) map[string]any {
	trimmed := strings.TrimSpace(raw)
	if trimmed != "" {
		var obj map[string]any
		if err := json.Unmarshal([]byte(trimmed), &obj); err == nil && obj != nil {
			return obj
		}
		return map[string]any{"body": trimmed}
	}
	out := map[string]any{}
	for key, value := range parseOCIParams(fallback) {
		out[key] = coerceWorkOSValue(value)
	}
	return out
}

func printOCIResponse(resp ociResponse, jsonOut bool, raw bool) {
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
			fmt.Printf("  %s %s\n", padRightANSI(orDash(stringifyWorkOSAny(item["id"])), 40), orDash(stringifyWorkOSAny(item["displayName"])))
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

func printOCIError(err error) {
	if err == nil {
		return
	}
	var details *ociAPIErrorDetails
	if errors.As(err, &details) {
		fmt.Printf("%s %s\n", styleHeading("OCI error:"), styleError(details.Error()))
		if details.RequestID != "" {
			fmt.Printf("%s %s\n", styleHeading("Request ID:"), details.RequestID)
		}
		if details.RawBody != "" {
			fmt.Printf("%s %s\n", styleHeading("Body:"), truncateString(details.RawBody, 600))
		}
		return
	}
	fmt.Printf("%s %s\n", styleHeading("OCI error:"), styleError(err.Error()))
}

func ociLogEvent(path string, event map[string]any) {
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
			event[key] = redactOCISensitive(asString)
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

func redactOCISensitive(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	replacements := []struct {
		needle string
		repl   string
	}{
		{"authorization:", "authorization: ***"},
		{"signature=", "signature=***"},
	}
	masked := value
	for _, item := range replacements {
		if strings.Contains(strings.ToLower(masked), strings.ToLower(item.needle)) {
			masked = maskAfterToken(masked, item.needle, item.repl)
		}
	}
	return masked
}

func ensureLeadingSlash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	if strings.HasPrefix(value, "/") {
		return value
	}
	return "/" + value
}

func expandTilde(path string) string {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return path
}

func ociCloudInitUserData(sshPort int) (string, error) {
	if sshPort == 22 {
		return "", fmt.Errorf("refusing to configure OCI SSH port to 22; use a non-22 port")
	}
	if sshPort <= 0 || sshPort > 65535 {
		return "", fmt.Errorf("invalid ssh port %d", sshPort)
	}
	cloudConfig := fmt.Sprintf(`#cloud-config
write_files:
  - path: /etc/cloud/cloud.cfg.d/99-oracular-hostname.cfg
    permissions: '0644'
    content: |
      preserve_hostname: true
  - path: /etc/ssh/sshd_config.d/oracular-port.conf
    permissions: '0644'
    content: |
      Port %d
  - path: /etc/iptables/rules.v4
    permissions: '0644'
    content: |
      *filter
      :INPUT ACCEPT [0:0]
      :FORWARD ACCEPT [0:0]
      :OUTPUT ACCEPT [0:0]
      :InstanceServices - [0:0]
      -A INPUT -m state --state RELATED,ESTABLISHED -j ACCEPT
      -A INPUT -p icmp -j ACCEPT
      -A INPUT -i lo -j ACCEPT
      -A INPUT -p tcp -m state --state NEW -m tcp --dport %d -j ACCEPT
      -A INPUT -p tcp -m state --state NEW -m tcp --dport 80 -j ACCEPT
      -A INPUT -p tcp -m state --state NEW -m tcp --dport 443 -j ACCEPT
      -A INPUT -j REJECT --reject-with icmp-host-prohibited
      -A FORWARD -j REJECT --reject-with icmp-host-prohibited
      -A OUTPUT -d 169.254.0.0/16 -j InstanceServices
      -A InstanceServices -d 169.254.0.2/32 -p tcp -m owner --uid-owner 0 -m tcp --dport 3260 -j ACCEPT
      -A InstanceServices -d 169.254.2.0/24 -p tcp -m owner --uid-owner 0 -m tcp --dport 3260 -j ACCEPT
      -A InstanceServices -d 169.254.4.0/24 -p tcp -m owner --uid-owner 0 -m tcp --dport 3260 -j ACCEPT
      -A InstanceServices -d 169.254.5.0/24 -p tcp -m owner --uid-owner 0 -m tcp --dport 3260 -j ACCEPT
      -A InstanceServices -d 169.254.0.2/32 -p tcp -m tcp --dport 80 -j ACCEPT
      -A InstanceServices -d 169.254.169.254/32 -p udp -m udp --dport 53 -j ACCEPT
      -A InstanceServices -d 169.254.169.254/32 -p tcp -m tcp --dport 53 -j ACCEPT
      -A InstanceServices -d 169.254.0.3/32 -p tcp -m owner --uid-owner 0 -m tcp --dport 80 -j ACCEPT
      -A InstanceServices -d 169.254.0.4/32 -p tcp -m tcp --dport 80 -j ACCEPT
      -A InstanceServices -d 169.254.169.254/32 -p tcp -m tcp --dport 80 -j ACCEPT
      -A InstanceServices -d 169.254.169.254/32 -p udp -m udp --dport 67 -j ACCEPT
      -A InstanceServices -d 169.254.169.254/32 -p udp -m udp --dport 69 -j ACCEPT
      -A InstanceServices -d 169.254.169.254/32 -p udp --dport 123 -j ACCEPT
      -A InstanceServices -d 169.254.0.0/16 -p tcp -m tcp -j REJECT --reject-with tcp-reset
      -A InstanceServices -d 169.254.0.0/16 -p udp -m udp -j REJECT --reject-with icmp-port-unreachable
      COMMIT
runcmd:
  - bash -lc "hostnamectl set-hostname oracular"
  - bash -lc "systemctl enable --now ssh || true"
  - bash -lc "systemctl restart ssh.socket || true"
  - bash -lc "systemctl restart ssh || true"
  - bash -lc "iptables-restore < /etc/iptables/rules.v4"
  - bash -lc "systemctl enable --now netfilter-persistent || true"
  - bash -lc "systemctl restart netfilter-persistent || true"
`, sshPort, sshPort)
	return base64.StdEncoding.EncodeToString([]byte(cloudConfig)), nil
}
