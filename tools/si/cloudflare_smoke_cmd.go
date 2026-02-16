package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"si/tools/si/internal/cloudflarebridge"
)

type cloudflareSmokeSpec struct {
	Name            string
	PathTemplate    string
	RequiresAccount bool
	Params          map[string]string
}

type cloudflareSmokeResult struct {
	Name       string `json:"name"`
	Path       string `json:"path,omitempty"`
	OK         bool   `json:"ok"`
	Skipped    bool   `json:"skipped,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	ErrorCode  int    `json:"error_code,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

func cmdCloudflareSmoke(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("cloudflare smoke", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	zone := fs.String("zone", "", "zone name")
	zoneID := fs.String("zone-id", "", "zone id")
	apiToken := fs.String("api-token", "", "override cloudflare api token")
	baseURL := fs.String("base-url", "", "cloudflare api base url")
	accountID := fs.String("account-id", "", "cloudflare account id")
	jsonOut := fs.Bool("json", false, "output json")
	noFail := fs.Bool("no-fail", false, "exit zero even when one or more integration checks fail")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si cloudflare smoke [--account <alias>] [--env <prod|staging|dev>] [--zone-id <zone>] [--no-fail] [--json]")
		return
	}

	runtime, client := mustCloudflareClient(*account, *env, *zone, *zoneID, *apiToken, *baseURL, *accountID)
	specs := cloudflareSmokeSpecs(runtime)
	results := make([]cloudflareSmokeResult, 0, len(specs))

	passCount := 0
	failCount := 0
	skipCount := 0

	for _, spec := range specs {
		result := runCloudflareSmokeCheck(runtime, client, spec)
		results = append(results, result)
		if result.Skipped {
			skipCount++
			continue
		}
		if result.OK {
			passCount++
			continue
		}
		failCount++
	}

	allOK := failCount == 0
	payload := map[string]any{
		"ok": allOK,
		"context": map[string]string{
			"account_alias": runtime.AccountAlias,
			"account_id":    runtime.AccountID,
			"environment":   runtime.Environment,
			"zone_id":       runtime.ZoneID,
			"zone_name":     runtime.ZoneName,
			"source":        runtime.Source,
			"base_url":      runtime.BaseURL,
		},
		"summary": map[string]int{
			"pass": passCount,
			"fail": failCount,
			"skip": skipCount,
		},
		"checks": results,
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		if !allOK && !*noFail {
			os.Exit(1)
		}
		return
	}

	if allOK {
		fmt.Printf("%s %s\n", styleHeading("Cloudflare smoke:"), styleSuccess("ok"))
	} else {
		fmt.Printf("%s %s\n", styleHeading("Cloudflare smoke:"), styleError("issues found"))
	}
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatCloudflareContext(runtime))
	fmt.Printf("%s pass=%d fail=%d skip=%d\n", styleHeading("Summary:"), passCount, failCount, skipCount)

	rows := make([][]string, 0, len(results))
	for _, result := range results {
		status := styleSuccess("PASS")
		if result.Skipped {
			status = styleDim("SKIP")
		} else if !result.OK {
			status = styleError("FAIL")
		}
		detail := strings.TrimSpace(result.Detail)
		if detail == "" {
			detail = "-"
		}
		codeText := "-"
		if result.StatusCode > 0 {
			codeText = fmt.Sprintf("%d", result.StatusCode)
		}
		rows = append(rows, []string{status, result.Name, codeText, detail})
	}
	printAlignedRows(rows, 2, "  ")

	if !allOK && !*noFail {
		os.Exit(1)
	}
}

func cloudflareSmokeSpecs(runtime cloudflareRuntimeContext) []cloudflareSmokeSpec {
	accountFilter := strings.TrimSpace(runtime.AccountID)
	zonesParams := map[string]string{"per_page": "1"}
	if accountFilter != "" {
		zonesParams["account.id"] = accountFilter
	}
	return []cloudflareSmokeSpec{
		{Name: "token_verify", PathTemplate: "/user/tokens/verify"},
		{Name: "accounts", PathTemplate: "/accounts"},
		{Name: "zones_by_account", PathTemplate: "/zones", Params: zonesParams},
		{Name: "account_details", PathTemplate: "/accounts/{account_id}", RequiresAccount: true},
		{Name: "workers_scripts", PathTemplate: "/accounts/{account_id}/workers/scripts", RequiresAccount: true},
		{Name: "pages_projects", PathTemplate: "/accounts/{account_id}/pages/projects", RequiresAccount: true},
		{Name: "r2_buckets", PathTemplate: "/accounts/{account_id}/r2/buckets", RequiresAccount: true},
		{Name: "d1_databases", PathTemplate: "/accounts/{account_id}/d1/database", RequiresAccount: true},
		{Name: "kv_namespaces", PathTemplate: "/accounts/{account_id}/storage/kv/namespaces", RequiresAccount: true},
		{Name: "queues", PathTemplate: "/accounts/{account_id}/queues", RequiresAccount: true},
		{Name: "access_apps", PathTemplate: "/accounts/{account_id}/access/apps", RequiresAccount: true},
		{Name: "tunnels", PathTemplate: "/accounts/{account_id}/cfd_tunnel", RequiresAccount: true},
		{Name: "lb_pools", PathTemplate: "/accounts/{account_id}/load_balancers/pools", RequiresAccount: true},
		{Name: "email_addresses", PathTemplate: "/accounts/{account_id}/email/routing/addresses", RequiresAccount: true},
	}
}

func runCloudflareSmokeCheck(runtime cloudflareRuntimeContext, client cloudflareBridgeClient, spec cloudflareSmokeSpec) cloudflareSmokeResult {
	if spec.RequiresAccount && strings.TrimSpace(runtime.AccountID) == "" {
		return cloudflareSmokeResult{
			Name:    spec.Name,
			Path:    spec.PathTemplate,
			Skipped: true,
			Detail:  "missing account id",
		}
	}
	path, err := cloudflareResolvePath(spec.PathTemplate, runtime, "")
	if err != nil {
		return cloudflareSmokeResult{
			Name:   spec.Name,
			Path:   spec.PathTemplate,
			OK:     false,
			Detail: err.Error(),
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, cloudflarebridge.Request{
		Method: "GET",
		Path:   path,
		Params: spec.Params,
	})
	if err != nil {
		result := cloudflareSmokeResult{
			Name:   spec.Name,
			Path:   path,
			OK:     false,
			Detail: err.Error(),
		}
		var apiErr *cloudflarebridge.APIErrorDetails
		if errors.As(err, &apiErr) && apiErr != nil {
			result.StatusCode = apiErr.StatusCode
			result.ErrorCode = apiErr.Code
			result.RequestID = strings.TrimSpace(apiErr.RequestID)
			if strings.TrimSpace(apiErr.Message) != "" {
				result.Detail = apiErr.Message
			}
		}
		return result
	}
	return cloudflareSmokeResult{
		Name:       spec.Name,
		Path:       path,
		OK:         true,
		StatusCode: resp.StatusCode,
		RequestID:  strings.TrimSpace(resp.RequestID),
		Detail:     summarizeCloudflareResponse(resp),
	}
}
