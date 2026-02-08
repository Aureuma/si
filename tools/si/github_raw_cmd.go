package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"

	"si/tools/si/internal/githubbridge"
)

func cmdGithubRaw(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github raw", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	method := fs.String("method", "GET", "http method")
	path := fs.String("path", "", "api path")
	body := fs.String("body", "", "raw request body")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*path) == "" {
		printUsage("usage: si github raw --method <GET|POST|PATCH|PUT|DELETE> --path <api-path> [--param key=value] [--body raw] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{
		AppID:          *appID,
		AppKey:         *appKey,
		InstallationID: *installationID,
	})
	printGithubContextBanner(runtime)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{
		Method:  strings.ToUpper(strings.TrimSpace(*method)),
		Path:    strings.TrimSpace(*path),
		Params:  parseGitHubParams(params),
		RawBody: strings.TrimSpace(*body),
		Owner:   runtime.Owner,
	})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubGraphQL(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github graphql", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	query := fs.String("query", "", "graphql query or mutation")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	vars := multiFlag{}
	fs.Var(&vars, "var", "graphql variable key=json_value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*query) == "" {
		printUsage("usage: si github graphql --query <query> [--var key=json] [--json]")
		return
	}
	payload := map[string]any{"query": strings.TrimSpace(*query)}
	if parsed := parseGraphQLVars(vars); len(parsed) > 0 {
		payload["variables"] = parsed
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{
		AppID:          *appID,
		AppKey:         *appKey,
		InstallationID: *installationID,
	})
	printGithubContextBanner(runtime)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{
		Method:   "POST",
		Path:     "/graphql",
		JSONBody: payload,
		Owner:    runtime.Owner,
	})
	if err != nil {
		printGithubError(err)
		return
	}
	if resp.Data != nil {
		if errs, ok := resp.Data["errors"].([]any); ok && len(errs) > 0 {
			printGithubError(fmt.Errorf("graphql returned errors"))
			if *jsonOut {
				printGithubResponse(resp, true, *raw)
				return
			}
			fmt.Println(styleDim("graphql errors:"))
			for _, item := range errs {
				rawItem, _ := json.Marshal(item)
				fmt.Printf("  %s\n", string(rawItem))
			}
		}
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func mustGithubClient(account string, owner string, baseURL string, overrides githubAuthOverrides) (githubRuntimeContext, githubBridgeClient) {
	runtime, err := resolveGithubRuntimeContext(account, owner, baseURL, overrides)
	if err != nil {
		fatal(err)
	}
	client, err := buildGithubClient(runtime)
	if err != nil {
		fatal(err)
	}
	return runtime, client
}

func printGithubContextBanner(runtime githubRuntimeContext) {
	fmt.Printf("%s %s\n", styleDim("github context:"), formatGithubContext(runtime))
}

func parseGitHubParams(values []string) map[string]string {
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

func parseGraphQLVars(values []string) map[string]any {
	out := map[string]any{}
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
		raw := strings.TrimSpace(parts[1])
		if raw == "" {
			continue
		}
		var decoded any
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			decoded = raw
		}
		out[key] = decoded
	}
	return out
}
