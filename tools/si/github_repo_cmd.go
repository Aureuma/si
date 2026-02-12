package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"si/tools/si/internal/githubbridge"
)

func cmdGithubRepo(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si github repo <list|get|create|update|archive|delete> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdGithubRepoList(args[1:])
	case "get":
		cmdGithubRepoGet(args[1:])
	case "create":
		cmdGithubRepoCreate(args[1:])
	case "update":
		cmdGithubRepoUpdate(args[1:])
	case "archive":
		cmdGithubRepoArchive(args[1:])
	case "delete":
		cmdGithubRepoDelete(args[1:])
	default:
		printUnknown("github repo", args[0])
	}
}

func cmdGithubRepoList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github repo list", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "owner/org (defaults to context owner)")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	maxPages := fs.Int("max-pages", 10, "max pages to fetch")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	selectedOwner := strings.TrimSpace(*owner)
	if fs.NArg() > 1 {
		printUsage("usage: si github repo list [owner] [--param key=value] [--max-pages N] [--json]")
		return
	}
	if fs.NArg() == 1 {
		selectedOwner = strings.TrimSpace(fs.Arg(0))
	}
	runtime, client := mustGithubClient(*account, selectedOwner, *baseURL, githubAuthOverrides{
		AuthMode:       *authMode,
		AccessToken:    *token,
		AppID:          *appID,
		AppKey:         *appKey,
		InstallationID: *installationID,
	})
	if strings.TrimSpace(selectedOwner) == "" {
		selectedOwner = strings.TrimSpace(runtime.Owner)
	}
	if selectedOwner == "" {
		fatal(fmt.Errorf("owner is required (use --owner, context owner, or positional owner)"))
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	items, err := client.ListAll(ctx, githubbridge.Request{
		Method: "GET",
		Path:   path.Join("/users", selectedOwner, "repos"),
		Params: parseGitHubParams(params),
		Owner:  selectedOwner,
	}, *maxPages)
	if err != nil {
		printGithubError(err)
		return
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{"owner": selectedOwner, "count": len(items), "data": items}); err != nil {
			fatal(err)
		}
		return
	}
	if *raw {
		rawBody, _ := json.Marshal(items)
		fmt.Println(string(rawBody))
		return
	}
	fmt.Printf("%s %s (%d)\n", styleHeading("Repository list:"), selectedOwner, len(items))
	for _, item := range items {
		fmt.Printf("  %s\n", summarizeGitHubItem(item))
	}
}

func cmdGithubRepoGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github repo get", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si github repo get <owner/repo|repo> [--owner <owner>] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{
		AuthMode:       *authMode,
		AccessToken:    *token,
		AppID:          *appID,
		AppKey:         *appKey,
		InstallationID: *installationID,
	})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "GET", Path: path.Join("/repos", repoOwner, repoName), Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubRepoCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github repo create", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "owner/org (defaults to context owner)")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	name := fs.String("name", "", "repository name")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage("usage: si github repo create [name] [--owner <owner>] [--param key=value] [--json]")
		return
	}
	repoName := strings.TrimSpace(*name)
	if repoName == "" && fs.NArg() == 1 {
		repoName = strings.TrimSpace(fs.Arg(0))
	}
	if repoName == "" {
		fatal(fmt.Errorf("repo name is required (use positional name or --name)"))
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{
		AuthMode:       *authMode,
		AccessToken:    *token,
		AppID:          *appID,
		AppKey:         *appKey,
		InstallationID: *installationID,
	})
	selectedOwner := strings.TrimSpace(*owner)
	if selectedOwner == "" {
		selectedOwner = strings.TrimSpace(runtime.Owner)
	}
	if selectedOwner == "" {
		fatal(fmt.Errorf("owner is required (use --owner or context owner)"))
	}
	body := parseGitHubBodyParams(params)
	body["name"] = repoName
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "POST", Path: path.Join("/orgs", selectedOwner, "repos"), JSONBody: body, Owner: selectedOwner})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubRepoUpdate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github repo update", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si github repo update <owner/repo|repo> [--owner <owner>] [--param key=value] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{
		AuthMode:       *authMode,
		AccessToken:    *token,
		AppID:          *appID,
		AppKey:         *appKey,
		InstallationID: *installationID,
	})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	body := parseGitHubBodyParams(params)
	if len(body) == 0 {
		fatal(fmt.Errorf("at least one --param key=value is required"))
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "PATCH", Path: path.Join("/repos", repoOwner, repoName), JSONBody: body, Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubRepoArchive(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("github repo archive", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	force := fs.Bool("force", false, "skip confirmation prompt")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si github repo archive <owner/repo|repo> [--owner <owner>] [--force] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{
		AuthMode:       *authMode,
		AccessToken:    *token,
		AppID:          *appID,
		AppKey:         *appKey,
		InstallationID: *installationID,
	})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	if err := requireGithubConfirmation("archive repository "+repoOwner+"/"+repoName, *force); err != nil {
		fatal(err)
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "PATCH", Path: path.Join("/repos", repoOwner, repoName), JSONBody: map[string]any{"archived": true}, Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubRepoDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("github repo delete", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	force := fs.Bool("force", false, "skip confirmation prompt")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si github repo delete <owner/repo|repo> [--owner <owner>] [--force] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{
		AuthMode:       *authMode,
		AccessToken:    *token,
		AppID:          *appID,
		AppKey:         *appKey,
		InstallationID: *installationID,
	})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	if err := requireGithubConfirmation("delete repository "+repoOwner+"/"+repoName, *force); err != nil {
		fatal(err)
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "DELETE", Path: path.Join("/repos", repoOwner, repoName), Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}
