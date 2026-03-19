package main

import (
	"fmt"
	"strings"
)

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

func printGithubContextBanner(runtime githubRuntimeContext, jsonOut bool) {
	if jsonOut {
		return
	}
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
