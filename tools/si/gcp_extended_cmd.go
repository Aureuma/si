package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"
)

const (
	gcpIAMBaseURL                = "https://iam.googleapis.com"
	gcpAPIKeysBaseURL            = "https://apikeys.googleapis.com"
	gcpCloudResourceManagerBase  = "https://cloudresourcemanager.googleapis.com"
	gcpGenerativeLanguageBaseURL = "https://generativelanguage.googleapis.com"
)

func cmdGCPAI(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si gcp ai <gemini|vertex> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "gemini", "generativelanguage":
		cmdGCPGemini(rest)
	case "vertex":
		cmdGCPVertex(rest)
	default:
		printUnknown("gcp ai", sub)
		printUsage("usage: si gcp ai <gemini|vertex> ...")
	}
}

func cmdGCPAPIKey(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si gcp apikey <list|get|create|update|delete|lookup|undelete>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGCPAPIKeyList(rest)
	case "get":
		cmdGCPAPIKeyGet(rest)
	case "create":
		cmdGCPAPIKeyCreate(rest)
	case "update":
		cmdGCPAPIKeyUpdate(rest)
	case "delete", "remove", "rm":
		cmdGCPAPIKeyDelete(rest)
	case "lookup":
		cmdGCPAPIKeyLookup(rest)
	case "undelete", "restore":
		cmdGCPAPIKeyUndelete(rest)
	default:
		printUnknown("gcp apikey", sub)
	}
}

func cmdGCPAPIKeyList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp apikey list", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	limit := fs.Int("limit", 100, "maximum keys")
	showDeleted := fs.Bool("show-deleted", false, "include deleted keys")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si gcp apikey list [--limit N] [--show-deleted] [--project <id>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, true, gcpAPIKeysBaseURL, true)
	if err != nil {
		fatal(err)
	}
	query := parseGCPParams(params)
	if *limit > 0 {
		query["pageSize"] = fmt.Sprintf("%d", *limit)
	}
	if *showDeleted {
		query["showDeleted"] = "true"
	}
	path := "/v2/projects/" + runtime.ProjectID + "/locations/global/keys"
	runGCPRequest(runtime, gcpRequest{Method: http.MethodGet, Path: path, Params: query}, *jsonOut, *raw)
}

func cmdGCPAPIKeyGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp apikey get", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	name := fs.String("name", "", "key resource name or key id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*name)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si gcp apikey get <name|key_id> [--project <id>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, true, gcpAPIKeysBaseURL, true)
	if err != nil {
		fatal(err)
	}
	resourceName, err := normalizeGCPAPIKeyName(runtime.ProjectID, id)
	if err != nil {
		fatal(err)
	}
	path := "/v2/" + resourceName
	runGCPRequest(runtime, gcpRequest{Method: http.MethodGet, Path: path}, *jsonOut, *raw)
}

func cmdGCPAPIKeyCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp apikey create", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	displayName := fs.String("display-name", "", "key display name")
	restrictionsJSON := fs.String("restrictions-json", "", "json object for key restrictions")
	body := fs.String("body", "", "raw json body")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si gcp apikey create --display-name <name> [--restrictions-json '{...}'] [--project <id>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, true, gcpAPIKeysBaseURL, true)
	if err != nil {
		fatal(err)
	}
	request := gcpRequest{Method: http.MethodPost, Path: "/v2/projects/" + runtime.ProjectID + "/locations/global/keys"}
	if strings.TrimSpace(*body) != "" {
		request.RawBody = strings.TrimSpace(*body)
	} else {
		payload := parseGCPJSONBody("", params)
		if value := strings.TrimSpace(*displayName); value != "" {
			payload["displayName"] = value
		}
		if strings.TrimSpace(*restrictionsJSON) != "" {
			var restrictions any
			if err := json.Unmarshal([]byte(strings.TrimSpace(*restrictionsJSON)), &restrictions); err != nil {
				fatal(fmt.Errorf("invalid --restrictions-json: %w", err))
			}
			payload["restrictions"] = restrictions
		}
		if strings.TrimSpace(stringifyWorkOSAny(payload["displayName"])) == "" {
			fatal(fmt.Errorf("--display-name is required"))
		}
		request.JSONBody = payload
	}
	runGCPRequest(runtime, request, *jsonOut, *raw)
}

func cmdGCPAPIKeyUpdate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp apikey update", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	name := fs.String("name", "", "key resource name or key id")
	displayName := fs.String("display-name", "", "new key display name")
	restrictionsJSON := fs.String("restrictions-json", "", "json object for key restrictions")
	updateMask := fs.String("update-mask", "", "field mask (comma-separated)")
	body := fs.String("body", "", "raw json body")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*name)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si gcp apikey update <name|key_id> [--display-name <name>] [--restrictions-json '{...}'] [--update-mask <mask>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, true, gcpAPIKeysBaseURL, true)
	if err != nil {
		fatal(err)
	}
	resourceName, err := normalizeGCPAPIKeyName(runtime.ProjectID, id)
	if err != nil {
		fatal(err)
	}
	query := map[string]string{}
	mask := strings.TrimSpace(*updateMask)
	request := gcpRequest{Method: http.MethodPatch, Path: "/v2/" + resourceName, Params: query}
	if strings.TrimSpace(*body) != "" {
		request.RawBody = strings.TrimSpace(*body)
	} else {
		payload := parseGCPJSONBody("", params)
		if value := strings.TrimSpace(*displayName); value != "" {
			payload["displayName"] = value
			if mask == "" {
				mask = "displayName"
			}
		}
		if strings.TrimSpace(*restrictionsJSON) != "" {
			var restrictions any
			if err := json.Unmarshal([]byte(strings.TrimSpace(*restrictionsJSON)), &restrictions); err != nil {
				fatal(fmt.Errorf("invalid --restrictions-json: %w", err))
			}
			payload["restrictions"] = restrictions
			if mask == "" {
				mask = "restrictions"
			}
		}
		if len(payload) == 0 {
			fatal(fmt.Errorf("at least one update field is required"))
		}
		request.JSONBody = payload
	}
	if strings.TrimSpace(mask) != "" {
		request.Params["updateMask"] = strings.TrimSpace(mask)
	}
	runGCPRequest(runtime, request, *jsonOut, *raw)
}

func cmdGCPAPIKeyDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("gcp apikey delete", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	name := fs.String("name", "", "key resource name or key id")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*name)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si gcp apikey delete <name|key_id> [--force] [--project <id>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, true, gcpAPIKeysBaseURL, true)
	if err != nil {
		fatal(err)
	}
	resourceName, err := normalizeGCPAPIKeyName(runtime.ProjectID, id)
	if err != nil {
		fatal(err)
	}
	if err := requireGCPConfirmation("delete API key "+resourceName, *force); err != nil {
		fatal(err)
	}
	runGCPRequest(runtime, gcpRequest{Method: http.MethodDelete, Path: "/v2/" + resourceName}, *jsonOut, *raw)
}

func cmdGCPAPIKeyUndelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("gcp apikey undelete", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	name := fs.String("name", "", "key resource name or key id")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*name)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si gcp apikey undelete <name|key_id> [--force] [--project <id>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, true, gcpAPIKeysBaseURL, true)
	if err != nil {
		fatal(err)
	}
	resourceName, err := normalizeGCPAPIKeyName(runtime.ProjectID, id)
	if err != nil {
		fatal(err)
	}
	if err := requireGCPConfirmation("undelete API key "+resourceName, *force); err != nil {
		fatal(err)
	}
	runGCPRequest(runtime, gcpRequest{Method: http.MethodPost, Path: "/v2/" + resourceName + ":undelete", JSONBody: map[string]any{}}, *jsonOut, *raw)
}

func cmdGCPAPIKeyLookup(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp apikey lookup", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	keyString := fs.String("key-string", "", "api key string to look up")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if strings.TrimSpace(*keyString) == "" || fs.NArg() > 0 {
		printUsage("usage: si gcp apikey lookup --key-string <api_key> [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, true, gcpAPIKeysBaseURL, true)
	if err != nil {
		fatal(err)
	}
	runGCPRequest(runtime, gcpRequest{Method: http.MethodGet, Path: "/v2/keys:lookupKey", Params: map[string]string{"keyString": strings.TrimSpace(*keyString)}}, *jsonOut, *raw)
}

func cmdGCPIAM(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si gcp iam <service-account|service-account-key|policy|role>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "service-account", "sa", "service-accounts":
		cmdGCPIAMServiceAccount(rest)
	case "service-account-key", "sa-key", "service-account-keys", "key", "keys":
		cmdGCPIAMServiceAccountKey(rest)
	case "policy", "iam-policy":
		cmdGCPIAMPolicy(rest)
	case "role", "roles":
		cmdGCPIAMRole(rest)
	default:
		printUnknown("gcp iam", sub)
	}
}

func cmdGCPIAMServiceAccount(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si gcp iam service-account <list|get|create|delete>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGCPIAMServiceAccountList(rest)
	case "get":
		cmdGCPIAMServiceAccountGet(rest)
	case "create":
		cmdGCPIAMServiceAccountCreate(rest)
	case "delete", "remove", "rm":
		cmdGCPIAMServiceAccountDelete(rest)
	default:
		printUnknown("gcp iam service-account", sub)
	}
}

func cmdGCPIAMServiceAccountList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp iam service-account list", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	limit := fs.Int("limit", 100, "maximum accounts")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si gcp iam service-account list [--limit N] [--project <id>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, true, gcpIAMBaseURL, true)
	if err != nil {
		fatal(err)
	}
	query := parseGCPParams(params)
	if *limit > 0 {
		query["pageSize"] = fmt.Sprintf("%d", *limit)
	}
	path := "/v1/projects/" + runtime.ProjectID + "/serviceAccounts"
	runGCPRequest(runtime, gcpRequest{Method: http.MethodGet, Path: path, Params: query}, *jsonOut, *raw)
}

func cmdGCPIAMServiceAccountGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp iam service-account get", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	name := fs.String("name", "", "service account resource name or email")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*name)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si gcp iam service-account get <name|email> [--project <id>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, true, gcpIAMBaseURL, true)
	if err != nil {
		fatal(err)
	}
	resourceName := normalizeGCPServiceAccountName(runtime.ProjectID, id)
	runGCPRequest(runtime, gcpRequest{Method: http.MethodGet, Path: "/v1/" + resourceName}, *jsonOut, *raw)
}

func cmdGCPIAMServiceAccountCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp iam service-account create", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	accountID := fs.String("account-id", "", "service account id (without domain)")
	displayName := fs.String("display-name", "", "display name")
	description := fs.String("description", "", "description")
	body := fs.String("body", "", "raw json body")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if strings.TrimSpace(*accountID) == "" || fs.NArg() > 0 {
		printUsage("usage: si gcp iam service-account create --account-id <id> [--display-name <name>] [--description <text>] [--project <id>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, true, gcpIAMBaseURL, true)
	if err != nil {
		fatal(err)
	}
	request := gcpRequest{Method: http.MethodPost, Path: "/v1/projects/" + runtime.ProjectID + "/serviceAccounts"}
	if strings.TrimSpace(*body) != "" {
		request.RawBody = strings.TrimSpace(*body)
	} else {
		payload := parseGCPJSONBody("", params)
		payload["accountId"] = strings.TrimSpace(*accountID)
		sa := map[string]any{}
		if value := strings.TrimSpace(*displayName); value != "" {
			sa["displayName"] = value
		}
		if value := strings.TrimSpace(*description); value != "" {
			sa["description"] = value
		}
		if len(sa) > 0 {
			payload["serviceAccount"] = sa
		}
		request.JSONBody = payload
	}
	runGCPRequest(runtime, request, *jsonOut, *raw)
}

func cmdGCPIAMServiceAccountDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("gcp iam service-account delete", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	name := fs.String("name", "", "service account resource name or email")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*name)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si gcp iam service-account delete <name|email> [--force] [--project <id>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, true, gcpIAMBaseURL, true)
	if err != nil {
		fatal(err)
	}
	resourceName := normalizeGCPServiceAccountName(runtime.ProjectID, id)
	if err := requireGCPConfirmation("delete service account "+resourceName, *force); err != nil {
		fatal(err)
	}
	runGCPRequest(runtime, gcpRequest{Method: http.MethodDelete, Path: "/v1/" + resourceName}, *jsonOut, *raw)
}

func cmdGCPIAMServiceAccountKey(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si gcp iam service-account-key <list|create|delete>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGCPIAMServiceAccountKeyList(rest)
	case "create":
		cmdGCPIAMServiceAccountKeyCreate(rest)
	case "delete", "remove", "rm":
		cmdGCPIAMServiceAccountKeyDelete(rest)
	default:
		printUnknown("gcp iam service-account-key", sub)
	}
}

func cmdGCPIAMServiceAccountKeyList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp iam service-account-key list", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	sa := fs.String("service-account", "", "service account resource name or email")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if strings.TrimSpace(*sa) == "" || fs.NArg() > 0 {
		printUsage("usage: si gcp iam service-account-key list --service-account <name|email> [--project <id>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, true, gcpIAMBaseURL, true)
	if err != nil {
		fatal(err)
	}
	saName := normalizeGCPServiceAccountName(runtime.ProjectID, strings.TrimSpace(*sa))
	path := "/v1/" + saName + "/keys"
	runGCPRequest(runtime, gcpRequest{Method: http.MethodGet, Path: path, Params: parseGCPParams(params)}, *jsonOut, *raw)
}

func cmdGCPIAMServiceAccountKeyCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp iam service-account-key create", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	sa := fs.String("service-account", "", "service account resource name or email")
	privateKeyType := fs.String("private-key-type", "TYPE_GOOGLE_CREDENTIALS_FILE", "private key type")
	keyAlg := fs.String("key-algorithm", "KEY_ALG_RSA_2048", "key algorithm")
	body := fs.String("body", "", "raw json body")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if strings.TrimSpace(*sa) == "" || fs.NArg() > 0 {
		printUsage("usage: si gcp iam service-account-key create --service-account <name|email> [--private-key-type <type>] [--key-algorithm <alg>] [--project <id>] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, true, gcpIAMBaseURL, true)
	if err != nil {
		fatal(err)
	}
	saName := normalizeGCPServiceAccountName(runtime.ProjectID, strings.TrimSpace(*sa))
	request := gcpRequest{Method: http.MethodPost, Path: "/v1/" + saName + "/keys"}
	if strings.TrimSpace(*body) != "" {
		request.RawBody = strings.TrimSpace(*body)
	} else {
		payload := parseGCPJSONBody("", params)
		if _, ok := payload["privateKeyType"]; !ok {
			payload["privateKeyType"] = strings.TrimSpace(*privateKeyType)
		}
		if _, ok := payload["keyAlgorithm"]; !ok {
			payload["keyAlgorithm"] = strings.TrimSpace(*keyAlg)
		}
		request.JSONBody = payload
	}
	runGCPRequest(runtime, request, *jsonOut, *raw)
}

func cmdGCPIAMServiceAccountKeyDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("gcp iam service-account-key delete", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	name := fs.String("name", "", "full service account key resource name")
	sa := fs.String("service-account", "", "service account resource name or email")
	key := fs.String("key", "", "key id when --service-account is used")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si gcp iam service-account-key delete --name <full_key_name> | --service-account <name|email> --key <id> [--force] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, true, gcpIAMBaseURL, true)
	if err != nil {
		fatal(err)
	}
	keyName := strings.TrimSpace(*name)
	if keyName == "" {
		if strings.TrimSpace(*sa) == "" || strings.TrimSpace(*key) == "" {
			fatal(fmt.Errorf("provide --name or (--service-account and --key)"))
		}
		saName := normalizeGCPServiceAccountName(runtime.ProjectID, strings.TrimSpace(*sa))
		keyName = saName + "/keys/" + strings.TrimSpace(*key)
	}
	if err := requireGCPConfirmation("delete service account key "+keyName, *force); err != nil {
		fatal(err)
	}
	runGCPRequest(runtime, gcpRequest{Method: http.MethodDelete, Path: "/v1/" + keyName}, *jsonOut, *raw)
}

func cmdGCPIAMPolicy(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si gcp iam policy <get|set|test-permissions>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "get":
		cmdGCPIAMPolicyGet(rest)
	case "set":
		cmdGCPIAMPolicySet(rest)
	case "test-permissions", "test":
		cmdGCPIAMPolicyTestPermissions(rest)
	default:
		printUnknown("gcp iam policy", sub)
	}
}

func cmdGCPIAMPolicyGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp iam policy get", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	resource := fs.String("resource", "", "resource name, default projects/<project>")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si gcp iam policy get [--resource projects/<id>] [--project <id>] [--json]")
		return
	}
	runtime, resourceName := mustResolveIAMPolicyRuntime(flags, strings.TrimSpace(*resource))
	path := "/v1/" + resourceName + ":getIamPolicy"
	runGCPRequest(runtime, gcpRequest{Method: http.MethodPost, Path: path, JSONBody: map[string]any{}}, *jsonOut, *raw)
}

func cmdGCPIAMPolicySet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp iam policy set", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	resource := fs.String("resource", "", "resource name, default projects/<project>")
	policyJSON := fs.String("policy-json", "", "policy json object")
	body := fs.String("body", "", "raw json body")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si gcp iam policy set [--resource projects/<id>] --policy-json '{...}' [--project <id>] [--json]")
		return
	}
	runtime, resourceName := mustResolveIAMPolicyRuntime(flags, strings.TrimSpace(*resource))
	request := gcpRequest{Method: http.MethodPost, Path: "/v1/" + resourceName + ":setIamPolicy"}
	if strings.TrimSpace(*body) != "" {
		request.RawBody = strings.TrimSpace(*body)
	} else {
		payload := parseGCPJSONBody("", params)
		if strings.TrimSpace(*policyJSON) != "" {
			var policy map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(*policyJSON)), &policy); err != nil {
				fatal(fmt.Errorf("invalid --policy-json: %w", err))
			}
			payload["policy"] = policy
		}
		if _, ok := payload["policy"]; !ok {
			fatal(fmt.Errorf("policy payload required: set --policy-json or --body/--param with policy"))
		}
		request.JSONBody = payload
	}
	runGCPRequest(runtime, request, *jsonOut, *raw)
}

func cmdGCPIAMPolicyTestPermissions(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp iam policy test-permissions", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	resource := fs.String("resource", "", "resource name, default projects/<project>")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	permissions := multiFlag{}
	fs.Var(&permissions, "permission", "permission to test (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || len(permissions) == 0 {
		printUsage("usage: si gcp iam policy test-permissions [--resource projects/<id>] --permission <perm> [--permission <perm>] [--project <id>] [--json]")
		return
	}
	runtime, resourceName := mustResolveIAMPolicyRuntime(flags, strings.TrimSpace(*resource))
	perms := make([]string, 0, len(permissions))
	for _, perm := range permissions {
		perm = strings.TrimSpace(perm)
		if perm != "" {
			perms = append(perms, perm)
		}
	}
	if len(perms) == 0 {
		fatal(fmt.Errorf("at least one --permission is required"))
	}
	path := "/v1/" + resourceName + ":testIamPermissions"
	runGCPRequest(runtime, gcpRequest{Method: http.MethodPost, Path: path, JSONBody: map[string]any{"permissions": perms}}, *jsonOut, *raw)
}

func cmdGCPIAMRole(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si gcp iam role <list|get>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGCPIAMRoleList(rest)
	case "get":
		cmdGCPIAMRoleGet(rest)
	default:
		printUnknown("gcp iam role", sub)
	}
}

func cmdGCPIAMRoleList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp iam role list", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	parent := fs.String("parent", "", "parent resource for custom roles, e.g. organizations/123 or projects/my-project")
	limit := fs.Int("limit", 100, "maximum roles")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si gcp iam role list [--parent <resource>] [--limit N] [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, true, gcpIAMBaseURL, true)
	if err != nil {
		fatal(err)
	}
	path := "/v1/roles"
	if strings.TrimSpace(*parent) != "" {
		path = "/v1/" + strings.Trim(strings.TrimSpace(*parent), "/") + "/roles"
	}
	query := parseGCPParams(params)
	if *limit > 0 {
		query["pageSize"] = fmt.Sprintf("%d", *limit)
	}
	runGCPRequest(runtime, gcpRequest{Method: http.MethodGet, Path: path, Params: query}, *jsonOut, *raw)
}

func cmdGCPIAMRoleGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp iam role get", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	name := fs.String("name", "", "role name, e.g. roles/viewer or projects/<id>/roles/<name>")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*name)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		printUsage("usage: si gcp iam role get <role_name> [--json]")
		return
	}
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, true, gcpIAMBaseURL, true)
	if err != nil {
		fatal(err)
	}
	path := "/v1/" + strings.Trim(id, "/")
	runGCPRequest(runtime, gcpRequest{Method: http.MethodGet, Path: path}, *jsonOut, *raw)
}

func cmdGCPGemini(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si gcp gemini <models|generate|embed|count-tokens|batch-embed|image|raw>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "models", "model":
		cmdGCPGeminiModels(rest)
	case "generate", "generate-content":
		cmdGCPGeminiGenerate(rest)
	case "embed", "embed-content":
		cmdGCPGeminiEmbed(rest)
	case "count-tokens", "count":
		cmdGCPGeminiCountTokens(rest)
	case "batch-embed", "batch", "batch-embed-contents":
		cmdGCPGeminiBatchEmbed(rest)
	case "image":
		cmdGCPGeminiImage(rest)
	case "raw":
		cmdGCPGeminiRaw(rest)
	default:
		printUnknown("gcp gemini", sub)
	}
}

func cmdGCPGeminiImage(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si gcp gemini image <generate>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "generate":
		cmdGCPGeminiImageGenerate(rest)
	default:
		printUnknown("gcp gemini image", sub)
	}
}

func cmdGCPGeminiImageGenerate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("gcp gemini image generate", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	apiKey := fs.String("api-key", "", "gemini api key")
	model := fs.String("model", "gemini-2.0-flash-preview-image-generation", "model id")
	prompt := fs.String("prompt", "", "image generation prompt")
	output := fs.String("output", "", "output image path (png)")
	transparent := fs.Bool("transparent", false, "ask model for transparent background")
	jsonBody := fs.String("json-body", "", "full request json override")
	jsonOut := fs.Bool("json", false, "output json")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*output) == "" {
		printUsage("usage: si gcp gemini image generate --prompt <text> --output <path> [--transparent] [--model <id>] [--api-key <key>] [--json]")
		return
	}
	if strings.TrimSpace(*prompt) == "" && strings.TrimSpace(*jsonBody) == "" {
		printUsage("usage: si gcp gemini image generate --prompt <text> --output <path> [--transparent] [--model <id>] [--api-key <key>] [--json]")
		return
	}

	runtime, key := mustResolveGeminiRuntime(flags, strings.TrimSpace(*apiKey))
	query := parseGCPParams(params)
	if key != "" {
		query["key"] = key
	}

	requestBody := map[string]any{}
	if strings.TrimSpace(*jsonBody) != "" {
		requestBody = parseGCPJSONBody(strings.TrimSpace(*jsonBody), nil)
	} else {
		promptText := strings.TrimSpace(*prompt)
		if *transparent {
			promptText += "\n\nReturn a PNG with a transparent background (alpha channel) and no canvas fill."
		}
		requestBody = map[string]any{
			"contents": []map[string]any{{
				"role":  "user",
				"parts": []map[string]any{{"text": promptText}},
			}},
			"generationConfig": map[string]any{
				"responseModalities": []string{"TEXT", "IMAGE"},
			},
		}
	}

	pathValue := "/v1beta/" + normalizeGeminiModelName(strings.TrimSpace(*model)) + ":generateContent"
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	resp, err := gcpDo(ctx, runtime, gcpRequest{
		Method:   http.MethodPost,
		Path:     pathValue,
		Params:   query,
		JSONBody: requestBody,
	})
	if err != nil {
		printGCPError(err)
		return
	}

	mimeType, encoded, note, err := extractGeminiInlineImage(resp)
	if err != nil {
		fatal(err)
	}
	rawImage, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		fatal(fmt.Errorf("decode gemini image payload: %w", err))
	}

	outputPath := filepath.Clean(strings.TrimSpace(*output))
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(outputPath, rawImage, 0o644); err != nil {
		fatal(err)
	}

	if *jsonOut {
		payload := map[string]any{
			"ok":          true,
			"model":       strings.TrimSpace(*model),
			"output":      outputPath,
			"mime_type":   mimeType,
			"bytes":       len(rawImage),
			"description": note,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	successf("gemini image generated: %s", outputPath)
	fmt.Printf("  model=%s\n", strings.TrimSpace(*model))
	fmt.Printf("  mime_type=%s\n", mimeType)
	fmt.Printf("  bytes=%d\n", len(rawImage))
	if strings.TrimSpace(note) != "" {
		fmt.Printf("  note=%s\n", note)
	}
}

func extractGeminiInlineImage(resp gcpResponse) (string, string, string, error) {
	data := resp.Data
	if len(data) == 0 && strings.TrimSpace(resp.Body) != "" {
		var bodyData map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(resp.Body)), &bodyData); err == nil {
			data = bodyData
		}
	}
	candidatesRaw, ok := data["candidates"].([]any)
	if !ok || len(candidatesRaw) == 0 {
		return "", "", "", fmt.Errorf("gemini response did not contain candidates")
	}
	var firstText string
	for _, candidate := range candidatesRaw {
		candidateMap, ok := candidate.(map[string]any)
		if !ok {
			continue
		}
		contentMap, ok := candidateMap["content"].(map[string]any)
		if !ok {
			continue
		}
		partsRaw, ok := contentMap["parts"].([]any)
		if !ok {
			continue
		}
		for _, part := range partsRaw {
			partMap, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if textValue := strings.TrimSpace(stringifyWorkOSAny(partMap["text"])); textValue != "" && firstText == "" {
				firstText = textValue
			}
			inlineData, ok := partMap["inlineData"].(map[string]any)
			if !ok {
				continue
			}
			mimeType := strings.TrimSpace(stringifyWorkOSAny(inlineData["mimeType"]))
			encoded := strings.TrimSpace(stringifyWorkOSAny(inlineData["data"]))
			if encoded == "" {
				continue
			}
			if mimeType == "" {
				mimeType = "image/png"
			}
			return mimeType, encoded, firstText, nil
		}
	}
	return "", "", "", fmt.Errorf("gemini response did not contain inline image data")
}

func cmdGCPGeminiModels(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si gcp gemini models <list|get> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true})
		fs := flag.NewFlagSet("gcp gemini models list", flag.ExitOnError)
		flags := bindGCPCommonFlags(fs)
		apiKey := fs.String("api-key", "", "gemini api key")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		params := multiFlag{}
		fs.Var(&params, "param", "query parameter key=value (repeatable)")
		_ = fs.Parse(args)
		if fs.NArg() > 0 {
			printUsage("usage: si gcp gemini models list [--api-key <key>] [--json]")
			return
		}
		runtime, key := mustResolveGeminiRuntime(flags, strings.TrimSpace(*apiKey))
		query := parseGCPParams(params)
		if key != "" {
			query["key"] = key
		}
		runGCPRequest(runtime, gcpRequest{Method: http.MethodGet, Path: "/v1beta/models", Params: query}, *jsonOut, *raw)
	case "get":
		args = stripeFlagsFirst(rest, map[string]bool{"json": true, "raw": true})
		fs := flag.NewFlagSet("gcp gemini models get", flag.ExitOnError)
		flags := bindGCPCommonFlags(fs)
		apiKey := fs.String("api-key", "", "gemini api key")
		model := fs.String("model", "", "model id or models/<id>")
		jsonOut := fs.Bool("json", false, "output json")
		raw := fs.Bool("raw", false, "print raw response body")
		params := multiFlag{}
		fs.Var(&params, "param", "query parameter key=value (repeatable)")
		_ = fs.Parse(args)
		name := strings.TrimSpace(*model)
		if name == "" && fs.NArg() == 1 {
			name = strings.TrimSpace(fs.Arg(0))
		}
		if name == "" || fs.NArg() > 1 {
			printUsage("usage: si gcp gemini models get <model> [--api-key <key>] [--json]")
			return
		}
		runtime, key := mustResolveGeminiRuntime(flags, strings.TrimSpace(*apiKey))
		query := parseGCPParams(params)
		if key != "" {
			query["key"] = key
		}
		runGCPRequest(runtime, gcpRequest{Method: http.MethodGet, Path: "/v1beta/" + normalizeGeminiModelName(name), Params: query}, *jsonOut, *raw)
	default:
		printUnknown("gcp gemini models", sub)
	}
}

func cmdGCPGeminiGenerate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp gemini generate", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	apiKey := fs.String("api-key", "", "gemini api key")
	model := fs.String("model", "gemini-2.0-flash", "model id")
	prompt := fs.String("prompt", "", "prompt text")
	system := fs.String("system", "", "system instruction")
	jsonBody := fs.String("json-body", "", "full request json")
	temperature := fs.Float64("temperature", -1, "generation temperature")
	maxTokens := fs.Int("max-output-tokens", 0, "generation max output tokens")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si gcp gemini generate --prompt <text> [--model <id>] [--api-key <key>] [--json]")
		return
	}
	runtime, key := mustResolveGeminiRuntime(flags, strings.TrimSpace(*apiKey))
	query := parseGCPParams(params)
	if key != "" {
		query["key"] = key
	}
	requestBody := map[string]any{}
	if strings.TrimSpace(*jsonBody) != "" {
		requestBody = parseGCPJSONBody(strings.TrimSpace(*jsonBody), nil)
	} else {
		if strings.TrimSpace(*prompt) == "" {
			fatal(fmt.Errorf("--prompt or --json-body is required"))
		}
		requestBody["contents"] = []map[string]any{{
			"role":  "user",
			"parts": []map[string]any{{"text": strings.TrimSpace(*prompt)}},
		}}
		if value := strings.TrimSpace(*system); value != "" {
			requestBody["systemInstruction"] = map[string]any{"parts": []map[string]any{{"text": value}}}
		}
		generationConfig := map[string]any{}
		if *temperature >= 0 {
			generationConfig["temperature"] = *temperature
		}
		if *maxTokens > 0 {
			generationConfig["maxOutputTokens"] = *maxTokens
		}
		if len(generationConfig) > 0 {
			requestBody["generationConfig"] = generationConfig
		}
	}
	path := "/v1beta/" + normalizeGeminiModelName(strings.TrimSpace(*model)) + ":generateContent"
	runGCPRequest(runtime, gcpRequest{Method: http.MethodPost, Path: path, Params: query, JSONBody: requestBody}, *jsonOut, *raw)
}

func cmdGCPGeminiEmbed(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp gemini embed", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	apiKey := fs.String("api-key", "", "gemini api key")
	model := fs.String("model", "text-embedding-004", "model id")
	text := fs.String("text", "", "input text")
	jsonBody := fs.String("json-body", "", "full request json")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si gcp gemini embed --text <text> [--model <id>] [--api-key <key>] [--json]")
		return
	}
	runtime, key := mustResolveGeminiRuntime(flags, strings.TrimSpace(*apiKey))
	query := parseGCPParams(params)
	if key != "" {
		query["key"] = key
	}
	requestBody := map[string]any{}
	if strings.TrimSpace(*jsonBody) != "" {
		requestBody = parseGCPJSONBody(strings.TrimSpace(*jsonBody), nil)
	} else {
		if strings.TrimSpace(*text) == "" {
			fatal(fmt.Errorf("--text or --json-body is required"))
		}
		requestBody["content"] = map[string]any{"parts": []map[string]any{{"text": strings.TrimSpace(*text)}}}
	}
	path := "/v1beta/" + normalizeGeminiModelName(strings.TrimSpace(*model)) + ":embedContent"
	runGCPRequest(runtime, gcpRequest{Method: http.MethodPost, Path: path, Params: query, JSONBody: requestBody}, *jsonOut, *raw)
}

func cmdGCPGeminiCountTokens(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp gemini count-tokens", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	apiKey := fs.String("api-key", "", "gemini api key")
	model := fs.String("model", "gemini-2.0-flash", "model id")
	text := fs.String("text", "", "input text")
	jsonBody := fs.String("json-body", "", "full request json")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si gcp gemini count-tokens --text <text> [--model <id>] [--api-key <key>] [--json]")
		return
	}
	runtime, key := mustResolveGeminiRuntime(flags, strings.TrimSpace(*apiKey))
	query := parseGCPParams(params)
	if key != "" {
		query["key"] = key
	}
	requestBody := map[string]any{}
	if strings.TrimSpace(*jsonBody) != "" {
		requestBody = parseGCPJSONBody(strings.TrimSpace(*jsonBody), nil)
	} else {
		if strings.TrimSpace(*text) == "" {
			fatal(fmt.Errorf("--text or --json-body is required"))
		}
		requestBody["contents"] = []map[string]any{{"role": "user", "parts": []map[string]any{{"text": strings.TrimSpace(*text)}}}}
	}
	path := "/v1beta/" + normalizeGeminiModelName(strings.TrimSpace(*model)) + ":countTokens"
	runGCPRequest(runtime, gcpRequest{Method: http.MethodPost, Path: path, Params: query, JSONBody: requestBody}, *jsonOut, *raw)
}

func cmdGCPGeminiBatchEmbed(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp gemini batch-embed", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	apiKey := fs.String("api-key", "", "gemini api key")
	model := fs.String("model", "text-embedding-004", "model id")
	jsonBody := fs.String("json-body", "", "full request json")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	texts := multiFlag{}
	params := multiFlag{}
	fs.Var(&texts, "text", "input text (repeatable)")
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si gcp gemini batch-embed --text <text> --text <text> [--model <id>] [--api-key <key>] [--json]")
		return
	}
	runtime, key := mustResolveGeminiRuntime(flags, strings.TrimSpace(*apiKey))
	query := parseGCPParams(params)
	if key != "" {
		query["key"] = key
	}
	requestBody := map[string]any{}
	if strings.TrimSpace(*jsonBody) != "" {
		requestBody = parseGCPJSONBody(strings.TrimSpace(*jsonBody), nil)
	} else {
		requests := make([]map[string]any, 0, len(texts))
		for _, text := range texts {
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			requests = append(requests, map[string]any{"content": map[string]any{"parts": []map[string]any{{"text": text}}}})
		}
		if len(requests) == 0 {
			fatal(fmt.Errorf("at least one --text is required when --json-body is not provided"))
		}
		requestBody["requests"] = requests
	}
	path := "/v1beta/" + normalizeGeminiModelName(strings.TrimSpace(*model)) + ":batchEmbedContents"
	runGCPRequest(runtime, gcpRequest{Method: http.MethodPost, Path: path, Params: query, JSONBody: requestBody}, *jsonOut, *raw)
}

func cmdGCPGeminiRaw(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp gemini raw", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	apiKey := fs.String("api-key", "", "gemini api key")
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
		printUsage("usage: si gcp gemini raw --method <GET|POST|PATCH|DELETE> --path <api-path> [--api-key <key>] [--json]")
		return
	}
	runtime, key := mustResolveGeminiRuntime(flags, strings.TrimSpace(*apiKey))
	query := parseGCPParams(params)
	if key != "" {
		query["key"] = key
	}
	var payload any
	if strings.TrimSpace(*jsonBody) != "" {
		payload = parseGCPJSONBody(strings.TrimSpace(*jsonBody), nil)
	}
	runGCPRequest(runtime, gcpRequest{Method: strings.ToUpper(strings.TrimSpace(*method)), Path: strings.TrimSpace(*path), Params: query, Headers: parseGCPParams(headers), RawBody: strings.TrimSpace(*body), JSONBody: payload}, *jsonOut, *raw)
}

func cmdGCPVertex(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si gcp vertex <model|endpoint|batch|pipeline|operation|raw>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "model", "models":
		cmdGCPVertexModel(rest)
	case "endpoint", "endpoints":
		cmdGCPVertexEndpoint(rest)
	case "batch", "batch-prediction", "batch-prediction-job", "batch-prediction-jobs":
		cmdGCPVertexBatch(rest)
	case "pipeline", "pipeline-job", "pipeline-jobs":
		cmdGCPVertexPipeline(rest)
	case "operation", "operations":
		cmdGCPVertexOperation(rest)
	case "raw":
		cmdGCPVertexRaw(rest)
	default:
		printUnknown("gcp vertex", sub)
	}
}

func cmdGCPVertexModel(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si gcp vertex model <list|get>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGCPVertexListGet("model", "models", rest)
	case "get":
		cmdGCPVertexListGet("model", "models", append([]string{"--get"}, rest...))
	default:
		printUnknown("gcp vertex model", sub)
	}
}

func cmdGCPVertexEndpoint(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si gcp vertex endpoint <list|get|create|delete|predict>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGCPVertexListGet("endpoint", "endpoints", rest)
	case "get":
		cmdGCPVertexListGet("endpoint", "endpoints", append([]string{"--get"}, rest...))
	case "create":
		cmdGCPVertexCreate("endpoint", "endpoints", rest)
	case "delete", "remove", "rm":
		cmdGCPVertexDelete("endpoint", "endpoints", rest)
	case "predict":
		cmdGCPVertexEndpointPredict(rest)
	default:
		printUnknown("gcp vertex endpoint", sub)
	}
}

func cmdGCPVertexBatch(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si gcp vertex batch <list|get|create|cancel|delete>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGCPVertexListGet("batch job", "batchPredictionJobs", rest)
	case "get":
		cmdGCPVertexListGet("batch job", "batchPredictionJobs", append([]string{"--get"}, rest...))
	case "create":
		cmdGCPVertexCreate("batch job", "batchPredictionJobs", rest)
	case "cancel":
		cmdGCPVertexCancel("batch job", "batchPredictionJobs", rest)
	case "delete", "remove", "rm":
		cmdGCPVertexDelete("batch job", "batchPredictionJobs", rest)
	default:
		printUnknown("gcp vertex batch", sub)
	}
}

func cmdGCPVertexPipeline(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si gcp vertex pipeline <list|get|create|cancel|delete>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGCPVertexListGet("pipeline job", "pipelineJobs", rest)
	case "get":
		cmdGCPVertexListGet("pipeline job", "pipelineJobs", append([]string{"--get"}, rest...))
	case "create":
		cmdGCPVertexCreate("pipeline job", "pipelineJobs", rest)
	case "cancel":
		cmdGCPVertexCancel("pipeline job", "pipelineJobs", rest)
	case "delete", "remove", "rm":
		cmdGCPVertexDelete("pipeline job", "pipelineJobs", rest)
	default:
		printUnknown("gcp vertex pipeline", sub)
	}
}

func cmdGCPVertexOperation(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si gcp vertex operation <list|get|cancel|delete>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGCPVertexListGet("operation", "operations", rest)
	case "get":
		cmdGCPVertexListGet("operation", "operations", append([]string{"--get"}, rest...))
	case "cancel":
		cmdGCPVertexCancel("operation", "operations", rest)
	case "delete", "remove", "rm":
		cmdGCPVertexDelete("operation", "operations", rest)
	default:
		printUnknown("gcp vertex operation", sub)
	}
}

func cmdGCPVertexRaw(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp vertex raw", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	location := fs.String("location", "", "vertex ai location")
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
		printUsage("usage: si gcp vertex raw --path <api-path> [--location <loc>] [--json]")
		return
	}
	runtime, _ := mustResolveVertexRuntime(flags, strings.TrimSpace(*location), true)
	var payload any
	if strings.TrimSpace(*jsonBody) != "" {
		payload = parseGCPJSONBody(strings.TrimSpace(*jsonBody), nil)
	}
	runGCPRequest(runtime, gcpRequest{Method: strings.ToUpper(strings.TrimSpace(*method)), Path: strings.TrimSpace(*path), Params: parseGCPParams(params), Headers: parseGCPParams(headers), RawBody: strings.TrimSpace(*body), JSONBody: payload}, *jsonOut, *raw)
}

func cmdGCPVertexListGet(noun string, collection string, args []string) {
	getMode := false
	if len(args) > 0 && strings.TrimSpace(args[0]) == "--get" {
		getMode = true
		args = args[1:]
	}
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp vertex "+noun+" list/get", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	location := fs.String("location", "", "vertex ai location")
	name := fs.String("name", "", noun+" name")
	limit := fs.Int("limit", 100, "maximum items")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	runtime, resolvedLocation := mustResolveVertexRuntime(flags, strings.TrimSpace(*location), true)
	id := strings.TrimSpace(*name)
	if getMode && id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if !getMode && fs.NArg() > 0 {
		if fs.NArg() == 1 {
			id = strings.TrimSpace(fs.Arg(0))
			getMode = true
		} else {
			fatal(fmt.Errorf("unexpected positional args"))
		}
	}
	query := parseGCPParams(params)
	if *limit > 0 {
		query["pageSize"] = fmt.Sprintf("%d", *limit)
	}
	if getMode {
		if id == "" {
			fatal(fmt.Errorf("resource name/id is required for get"))
		}
		resourceName := normalizeVertexResourceName(runtime.ProjectID, resolvedLocation, collection, id)
		runGCPRequest(runtime, gcpRequest{Method: http.MethodGet, Path: "/v1/" + resourceName, Params: query}, *jsonOut, *raw)
		return
	}
	path := "/v1/projects/" + runtime.ProjectID + "/locations/" + resolvedLocation + "/" + collection
	runGCPRequest(runtime, gcpRequest{Method: http.MethodGet, Path: path, Params: query}, *jsonOut, *raw)
}

func cmdGCPVertexCreate(noun string, collection string, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp vertex "+noun+" create", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	location := fs.String("location", "", "vertex ai location")
	body := fs.String("body", "", "raw json body")
	jsonBody := fs.String("json-body", "", "json request body")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		fatal(fmt.Errorf("unexpected positional args"))
	}
	runtime, resolvedLocation := mustResolveVertexRuntime(flags, strings.TrimSpace(*location), true)
	request := gcpRequest{Method: http.MethodPost, Path: "/v1/projects/" + runtime.ProjectID + "/locations/" + resolvedLocation + "/" + collection}
	if strings.TrimSpace(*body) != "" {
		request.RawBody = strings.TrimSpace(*body)
	} else if strings.TrimSpace(*jsonBody) != "" {
		request.JSONBody = parseGCPJSONBody(strings.TrimSpace(*jsonBody), nil)
	} else {
		request.JSONBody = parseGCPJSONBody("", params)
		if len(request.JSONBody.(map[string]any)) == 0 {
			fatal(fmt.Errorf("provide --json-body, --body, or at least one --param"))
		}
	}
	runGCPRequest(runtime, request, *jsonOut, *raw)
}

func cmdGCPVertexDelete(noun string, collection string, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("gcp vertex "+noun+" delete", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	location := fs.String("location", "", "vertex ai location")
	name := fs.String("name", "", noun+" name or id")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*name)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		fatal(fmt.Errorf("usage: si gcp vertex %s delete <name|id> [--location <loc>] [--force] [--json]", strings.ReplaceAll(noun, " ", "-")))
	}
	runtime, resolvedLocation := mustResolveVertexRuntime(flags, strings.TrimSpace(*location), true)
	resourceName := normalizeVertexResourceName(runtime.ProjectID, resolvedLocation, collection, id)
	if err := requireGCPConfirmation("delete vertex "+noun+" "+resourceName, *force); err != nil {
		fatal(err)
	}
	runGCPRequest(runtime, gcpRequest{Method: http.MethodDelete, Path: "/v1/" + resourceName}, *jsonOut, *raw)
}

func cmdGCPVertexCancel(noun string, collection string, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("gcp vertex "+noun+" cancel", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	location := fs.String("location", "", "vertex ai location")
	name := fs.String("name", "", noun+" name or id")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*name)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		fatal(fmt.Errorf("usage: si gcp vertex %s cancel <name|id> [--location <loc>] [--force] [--json]", strings.ReplaceAll(noun, " ", "-")))
	}
	runtime, resolvedLocation := mustResolveVertexRuntime(flags, strings.TrimSpace(*location), true)
	resourceName := normalizeVertexResourceName(runtime.ProjectID, resolvedLocation, collection, id)
	if err := requireGCPConfirmation("cancel vertex "+noun+" "+resourceName, *force); err != nil {
		fatal(err)
	}
	runGCPRequest(runtime, gcpRequest{Method: http.MethodPost, Path: "/v1/" + resourceName + ":cancel", JSONBody: map[string]any{}}, *jsonOut, *raw)
}

func cmdGCPVertexEndpointPredict(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("gcp vertex endpoint predict", flag.ExitOnError)
	flags := bindGCPCommonFlags(fs)
	location := fs.String("location", "", "vertex ai location")
	endpoint := fs.String("endpoint", "", "endpoint name or id")
	body := fs.String("body", "", "raw json body")
	jsonBody := fs.String("json-body", "", "json request body")
	instancesJSON := fs.String("instances-json", "", "json array of prediction instances")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	id := strings.TrimSpace(*endpoint)
	if id == "" && fs.NArg() == 1 {
		id = strings.TrimSpace(fs.Arg(0))
	}
	if id == "" || fs.NArg() > 1 {
		fatal(fmt.Errorf("usage: si gcp vertex endpoint predict <endpoint|id> [--instances-json '[...]'] [--json-body '{...}'] [--location <loc>] [--json]"))
	}
	runtime, resolvedLocation := mustResolveVertexRuntime(flags, strings.TrimSpace(*location), true)
	endpointName := normalizeVertexResourceName(runtime.ProjectID, resolvedLocation, "endpoints", id)
	request := gcpRequest{Method: http.MethodPost, Path: "/v1/" + endpointName + ":predict"}
	if strings.TrimSpace(*body) != "" {
		request.RawBody = strings.TrimSpace(*body)
	} else if strings.TrimSpace(*jsonBody) != "" {
		request.JSONBody = parseGCPJSONBody(strings.TrimSpace(*jsonBody), nil)
	} else if strings.TrimSpace(*instancesJSON) != "" {
		var instances any
		if err := json.Unmarshal([]byte(strings.TrimSpace(*instancesJSON)), &instances); err != nil {
			fatal(fmt.Errorf("invalid --instances-json: %w", err))
		}
		request.JSONBody = map[string]any{"instances": instances}
	} else {
		payload := parseGCPJSONBody("", params)
		if len(payload) == 0 {
			fatal(fmt.Errorf("provide --instances-json, --json-body, --body, or --param"))
		}
		request.JSONBody = payload
	}
	runGCPRequest(runtime, request, *jsonOut, *raw)
}

func runGCPRequest(runtime gcpRuntimeContext, req gcpRequest, jsonOut bool, raw bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := gcpDo(ctx, runtime, req)
	if err != nil {
		printGCPError(err)
		return
	}
	printGCPResponse(resp, jsonOut, raw)
}

func mustResolveIAMPolicyRuntime(flags gcpCommonFlags, resource string) (gcpRuntimeContext, string) {
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, true, gcpCloudResourceManagerBase, true)
	if err != nil {
		fatal(err)
	}
	resourceName := strings.Trim(resource, "/")
	if resourceName == "" {
		resourceName = "projects/" + runtime.ProjectID
	}
	return runtime, resourceName
}

func mustResolveGeminiRuntime(flags gcpCommonFlags, apiKeyOverride string) (gcpRuntimeContext, string) {
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, false, gcpGenerativeLanguageBaseURL, false)
	if err != nil {
		fatal(err)
	}
	alias, account := resolveGCPAccountSelection(loadSettingsOrDefault(), strings.TrimSpace(valueOrEmpty(flags.account)))
	apiKey, apiKeySource := resolveGCPAPIKey(alias, account, apiKeyOverride)
	if strings.TrimSpace(runtime.AccessToken) == "" && strings.TrimSpace(apiKey) == "" {
		fatal(fmt.Errorf("gemini auth requires --api-key, GCP_<ACCOUNT>_API_KEY / GEMINI_API_KEY / GOOGLE_API_KEY, or OAuth access token"))
	}
	runtime.Source = strings.Join(nonEmpty(runtime.Source, apiKeySource), ",")
	return runtime, strings.TrimSpace(apiKey)
}

func mustResolveVertexRuntime(flags gcpCommonFlags, location string, requireToken bool) (gcpRuntimeContext, string) {
	resolvedLocation := resolveVertexLocation(location)
	baseURL := fmt.Sprintf("https://%s-aiplatform.googleapis.com", resolvedLocation)
	runtime, err := resolveRuntimeFromGCPFlagsWithBase(flags, requireToken, baseURL, true)
	if err != nil {
		fatal(err)
	}
	return runtime, resolvedLocation
}

func resolveVertexLocation(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = strings.TrimSpace(os.Getenv("GCP_VERTEX_LOCATION"))
	}
	if value == "" {
		value = strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_LOCATION"))
	}
	if value == "" {
		value = "us-central1"
	}
	return value
}

func normalizeGeminiModelName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "models/gemini-2.0-flash"
	}
	if strings.HasPrefix(value, "models/") {
		return value
	}
	return "models/" + value
}

func normalizeGCPAPIKeyName(projectID string, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("api key name is required")
	}
	if strings.HasPrefix(value, "projects/") {
		return strings.Trim(value, "/"), nil
	}
	if strings.TrimSpace(projectID) == "" {
		return "", fmt.Errorf("project id is required to expand key id")
	}
	return "projects/" + strings.TrimSpace(projectID) + "/locations/global/keys/" + strings.Trim(value, "/"), nil
}

func normalizeGCPServiceAccountName(projectID string, value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "projects/") {
		return strings.Trim(value, "/")
	}
	if strings.Contains(value, "@") {
		return "projects/" + strings.TrimSpace(projectID) + "/serviceAccounts/" + value
	}
	return strings.Trim(value, "/")
}

func normalizeVertexResourceName(projectID string, location string, collection string, value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "projects/") {
		return strings.Trim(value, "/")
	}
	return "projects/" + strings.TrimSpace(projectID) + "/locations/" + strings.TrimSpace(location) + "/" + strings.Trim(collection, "/") + "/" + strings.Trim(value, "/")
}

func resolveGCPAPIKey(alias string, account GCPAccountEntry, override string) (string, string) {
	if value := strings.TrimSpace(override); value != "" {
		return value, "flag:--api-key"
	}
	if ref := strings.TrimSpace(account.APIKeyEnv); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	if value := strings.TrimSpace(resolveGCPEnv(alias, account, "API_KEY")); value != "" {
		return value, "env:" + gcpAccountEnvPrefix(alias, account) + "API_KEY"
	}
	if value := strings.TrimSpace(os.Getenv("GEMINI_API_KEY")); value != "" {
		return value, "env:GEMINI_API_KEY"
	}
	if value := strings.TrimSpace(os.Getenv("GOOGLE_API_KEY")); value != "" {
		return value, "env:GOOGLE_API_KEY"
	}
	if value := strings.TrimSpace(os.Getenv("GCP_API_KEY")); value != "" {
		return value, "env:GCP_API_KEY"
	}
	return "", ""
}

func requireGCPConfirmation(action string, force bool) error {
	if force {
		return nil
	}
	action = strings.TrimSpace(action)
	if action == "" {
		action = "continue"
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return fmt.Errorf("refusing to %s without confirmation in non-interactive mode; use --force", action)
	}
	fmt.Printf("%s ", styleWarn(fmt.Sprintf("Confirm %s? type `yes` to continue (Esc to cancel):", action)))
	line, err := promptLine(os.Stdin)
	if err != nil {
		return err
	}
	if isEscCancelInput(line) {
		return fmt.Errorf("operation canceled")
	}
	if strings.EqualFold(strings.TrimSpace(line), "yes") {
		return nil
	}
	return fmt.Errorf("operation canceled")
}
