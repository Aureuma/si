package main

import (
	"context"
	"flag"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"si/tools/si/internal/githubbridge"
)

func cmdGithubIssue(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si github issue <list|get|create|comment|close|reopen> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdGithubIssueList(args[1:])
	case "get":
		cmdGithubIssueGet(args[1:])
	case "create":
		cmdGithubIssueCreate(args[1:])
	case "comment":
		cmdGithubIssueComment(args[1:])
	case "close":
		cmdGithubIssueClose(args[1:])
	case "reopen":
		cmdGithubIssueReopen(args[1:])
	default:
		printUnknown("github issue", args[0])
	}
}

func cmdGithubIssueList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github issue list", flag.ExitOnError)
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
	maxPages := fs.Int("max-pages", 5, "max pages to fetch")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si github issue list <owner/repo|repo> [--owner <owner>] [--param key=value] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	items, err := client.ListAll(ctx, githubbridge.Request{Method: "GET", Path: path.Join("/repos", repoOwner, repoName, "issues"), Params: parseGitHubParams(params), Owner: repoOwner, Repo: repoName}, *maxPages)
	if err != nil {
		printGithubError(err)
		return
	}
	resp := githubbridge.Response{StatusCode: 200, Status: "200 OK", List: items}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubIssueGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github issue get", flag.ExitOnError)
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
	if fs.NArg() != 2 {
		printUsage("usage: si github issue get <owner/repo|repo> <number> [--owner <owner>] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	number, err := parseGitHubNumber(fs.Arg(1), "issue number")
	if err != nil {
		fatal(err)
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "GET", Path: path.Join("/repos", repoOwner, repoName, "issues", strconv.Itoa(number)), Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubIssueCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github issue create", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	title := fs.String("title", "", "issue title")
	body := fs.String("body", "", "issue body")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si github issue create <owner/repo|repo> --title <title> [--body <text>] [--json]")
		return
	}
	if strings.TrimSpace(*title) == "" {
		fatal(fmt.Errorf("--title is required"))
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	payload := parseGitHubBodyParams(params)
	payload["title"] = strings.TrimSpace(*title)
	if strings.TrimSpace(*body) != "" {
		payload["body"] = strings.TrimSpace(*body)
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "POST", Path: path.Join("/repos", repoOwner, repoName, "issues"), JSONBody: payload, Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubIssueComment(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github issue comment", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	body := fs.String("body", "", "comment body")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github issue comment <owner/repo|repo> <number> --body <text> [--json]")
		return
	}
	if strings.TrimSpace(*body) == "" {
		fatal(fmt.Errorf("--body is required"))
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	number, err := parseGitHubNumber(fs.Arg(1), "issue number")
	if err != nil {
		fatal(err)
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "POST", Path: path.Join("/repos", repoOwner, repoName, "issues", strconv.Itoa(number), "comments"), JSONBody: map[string]any{"body": strings.TrimSpace(*body)}, Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubIssueClose(args []string) {
	cmdGithubIssueSetState(args, "closed")
}

func cmdGithubIssueReopen(args []string) {
	cmdGithubIssueSetState(args, "open")
}

func cmdGithubIssueSetState(args []string, state string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github issue state", flag.ExitOnError)
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
	if fs.NArg() != 2 {
		printUsage("usage: si github issue " + map[string]string{"closed": "close", "open": "reopen"}[state] + " <owner/repo|repo> <number> [--owner <owner>] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	number, err := parseGitHubNumber(fs.Arg(1), "issue number")
	if err != nil {
		fatal(err)
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "PATCH", Path: path.Join("/repos", repoOwner, repoName, "issues", strconv.Itoa(number)), JSONBody: map[string]any{"state": state}, Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}
