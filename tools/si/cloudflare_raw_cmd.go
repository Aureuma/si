package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"

	"si/tools/si/internal/cloudflarebridge"
)

func cmdCloudflareRaw(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("cloudflare raw", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	zone := fs.String("zone", "", "zone name")
	zoneID := fs.String("zone-id", "", "zone id")
	apiToken := fs.String("api-token", "", "override cloudflare api token")
	baseURL := fs.String("base-url", "", "cloudflare api base url")
	accountID := fs.String("account-id", "", "cloudflare account id")
	method := fs.String("method", "GET", "http method")
	path := fs.String("path", "", "api path")
	body := fs.String("body", "", "raw request body")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query/body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*path) == "" {
		printUsage("usage: si cloudflare raw --method <GET|POST|PATCH|PUT|DELETE> --path <api-path> [--param key=value] [--body raw] [--json]")
		return
	}
	runtime, client := mustCloudflareClient(*account, *env, *zone, *zoneID, *apiToken, *baseURL, *accountID)
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	request := cloudflarebridge.Request{
		Method: strings.ToUpper(strings.TrimSpace(*method)),
		Path:   strings.TrimSpace(*path),
		Params: parseCloudflareParams(params),
	}
	if strings.TrimSpace(*body) != "" {
		request.RawBody = strings.TrimSpace(*body)
	}
	resp, err := client.Do(ctx, request)
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func cmdCloudflareAnalytics(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si cloudflare analytics <http|security|cache> [flags]")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	args = args[1:]
	path := ""
	switch sub {
	case "http":
		path = "/zones/{zone_id}/analytics/dashboard"
	case "security":
		path = "/zones/{zone_id}/firewall/events"
	case "cache":
		path = "/zones/{zone_id}/analytics/colos"
	default:
		printUnknown("cloudflare analytics", sub)
		return
	}
	cmdCloudflareScopedRead(args, "cloudflare analytics "+sub, path)
}

func cmdCloudflareLogs(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si cloudflare logs <job|received> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "job", "jobs":
		cmdCloudflareLogsJobs(rest)
	case "received", "download":
		cmdCloudflareScopedRead(rest, "cloudflare logs received", "/zones/{zone_id}/logs/received")
	default:
		printUnknown("cloudflare logs", sub)
	}
}

func cmdCloudflareLogsJobs(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si cloudflare logs job <list|get|create|update|delete>")
	if !routedOK {
		return
	}
	args = routedArgs
	spec := cloudflareResourceSpec{
		Name:         "log job",
		Scope:        cloudflareScopeZone,
		ListPath:     "/zones/{zone_id}/logpush/jobs",
		ResourcePath: "/zones/{zone_id}/logpush/jobs/{id}",
	}
	cmdCloudflareResourceFamily(args, spec, "usage: si cloudflare logs job <list|get|create|update|delete> ...")
}

func cmdCloudflareReport(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("cloudflare report", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	zone := fs.String("zone", "", "zone name")
	zoneID := fs.String("zone-id", "", "zone id")
	apiToken := fs.String("api-token", "", "override cloudflare api token")
	baseURL := fs.String("base-url", "", "cloudflare api base url")
	accountID := fs.String("account-id", "", "cloudflare account id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	from := fs.String("from", "", "from timestamp (iso8601)")
	to := fs.String("to", "", "to timestamp (iso8601)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si cloudflare report <traffic-summary|security-events|cache-summary|billing-summary> [--from <iso>] [--to <iso>] [--json]")
		return
	}
	preset := strings.ToLower(strings.TrimSpace(fs.Arg(0)))
	paths := map[string]string{
		"traffic-summary": "/zones/{zone_id}/analytics/dashboard",
		"security-events": "/zones/{zone_id}/firewall/events",
		"cache-summary":   "/zones/{zone_id}/analytics/colos",
		"billing-summary": "/accounts/{account_id}/billing/subscriptions",
	}
	endpoint, ok := paths[preset]
	if !ok {
		fatal(fmt.Errorf("unknown report preset %q", preset))
	}
	runtime, client := mustCloudflareClient(*account, *env, *zone, *zoneID, *apiToken, *baseURL, *accountID)
	resolvedPath, err := cloudflareResolvePath(endpoint, runtime, "")
	if err != nil {
		fatal(err)
	}
	params := map[string]string{}
	if value := strings.TrimSpace(*from); value != "" {
		params["since"] = value
	}
	if value := strings.TrimSpace(*to); value != "" {
		params["until"] = value
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, cloudflarebridge.Request{Method: "GET", Path: resolvedPath, Params: params})
	if err != nil {
		printCloudflareError(err)
		return
	}
	if *jsonOut || *raw {
		printCloudflareResponse(resp, *jsonOut, *raw)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Report:"), preset)
	printCloudflareResponse(resp, false, false)
}

func cmdCloudflareScopedRead(args []string, command string, pathTemplate string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet(command, flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	zone := fs.String("zone", "", "zone name")
	zoneID := fs.String("zone-id", "", "zone id")
	apiToken := fs.String("api-token", "", "override cloudflare api token")
	baseURL := fs.String("base-url", "", "cloudflare api base url")
	accountID := fs.String("account-id", "", "cloudflare account id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si " + command + " [--param key=value] [--json]")
		return
	}
	runtime, client := mustCloudflareClient(*account, *env, *zone, *zoneID, *apiToken, *baseURL, *accountID)
	resolvedPath, err := cloudflareResolvePath(pathTemplate, runtime, "")
	if err != nil {
		fatal(err)
	}
	printCloudflareContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, cloudflarebridge.Request{Method: "GET", Path: resolvedPath, Params: parseCloudflareParams(params)})
	if err != nil {
		printCloudflareError(err)
		return
	}
	printCloudflareResponse(resp, *jsonOut, *raw)
}

func parseCloudflareParams(values []string) map[string]string {
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

func parseCloudflareBodyParams(values []string) map[string]any {
	out := map[string]any{}
	for key, value := range parseCloudflareParams(values) {
		trim := strings.TrimSpace(value)
		if trim == "" {
			out[key] = ""
			continue
		}
		var decoded any
		if strings.HasPrefix(trim, "{") || strings.HasPrefix(trim, "[") || trim == "true" || trim == "false" || trim == "null" {
			if err := json.Unmarshal([]byte(trim), &decoded); err == nil {
				out[key] = decoded
				continue
			}
		}
		out[key] = trim
	}
	return out
}
