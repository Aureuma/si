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

func cmdGithubPR(args []string) {
	if len(args) == 0 {
		printUsage("usage: si github pr <list|get|create|comment|merge> ...")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdGithubPRList(args[1:])
	case "get":
		cmdGithubPRGet(args[1:])
	case "create":
		cmdGithubPRCreate(args[1:])
	case "comment":
		cmdGithubPRComment(args[1:])
	case "merge":
		cmdGithubPRMerge(args[1:])
	default:
		printUnknown("github pr", args[0])
	}
}

func cmdGithubPRList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github pr list", flag.ExitOnError)
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
		printUsage("usage: si github pr list <owner/repo|repo> [--owner <owner>] [--param key=value] [--json]")
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
	items, err := client.ListAll(ctx, githubbridge.Request{Method: "GET", Path: path.Join("/repos", repoOwner, repoName, "pulls"), Params: parseGitHubParams(params), Owner: repoOwner, Repo: repoName}, *maxPages)
	if err != nil {
		printGithubError(err)
		return
	}
	resp := githubbridge.Response{StatusCode: 200, Status: "200 OK", List: items}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubPRGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github pr get", flag.ExitOnError)
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
		printUsage("usage: si github pr get <owner/repo|repo> <number> [--owner <owner>] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	number, err := parseGitHubNumber(fs.Arg(1), "pull request number")
	if err != nil {
		fatal(err)
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "GET", Path: path.Join("/repos", repoOwner, repoName, "pulls", strconv.Itoa(number)), Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubPRCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github pr create", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	head := fs.String("head", "", "head branch")
	base := fs.String("base", "", "base branch")
	title := fs.String("title", "", "title")
	body := fs.String("body", "", "body")
	draft := fs.Bool("draft", false, "create draft pull request")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si github pr create <owner/repo|repo> --head <branch> --base <branch> --title <title> [--body <text>] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(*head) == "" || strings.TrimSpace(*base) == "" || strings.TrimSpace(*title) == "" {
		fatal(fmt.Errorf("--head, --base, and --title are required"))
	}
	payload := parseGitHubBodyParams(params)
	payload["head"] = strings.TrimSpace(*head)
	payload["base"] = strings.TrimSpace(*base)
	payload["title"] = strings.TrimSpace(*title)
	if strings.TrimSpace(*body) != "" {
		payload["body"] = strings.TrimSpace(*body)
	}
	if *draft {
		payload["draft"] = true
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "POST", Path: path.Join("/repos", repoOwner, repoName, "pulls"), JSONBody: payload, Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubPRComment(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github pr comment", flag.ExitOnError)
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
		printUsage("usage: si github pr comment <owner/repo|repo> <number> --body <text> [--json]")
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
	number, err := parseGitHubNumber(fs.Arg(1), "pull request number")
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

func cmdGithubPRMerge(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github pr merge", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	method := fs.String("method", "merge", "merge method (merge|squash|rebase)")
	title := fs.String("title", "", "optional commit title")
	message := fs.String("message", "", "optional commit message")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github pr merge <owner/repo|repo> <number> [--method merge|squash|rebase] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	number, err := parseGitHubNumber(fs.Arg(1), "pull request number")
	if err != nil {
		fatal(err)
	}
	mergeMethod := strings.TrimSpace(strings.ToLower(*method))
	switch mergeMethod {
	case "merge", "squash", "rebase":
	default:
		fatal(fmt.Errorf("invalid --method %q (expected merge|squash|rebase)", *method))
	}
	payload := map[string]any{"merge_method": mergeMethod}
	if strings.TrimSpace(*title) != "" {
		payload["commit_title"] = strings.TrimSpace(*title)
	}
	if strings.TrimSpace(*message) != "" {
		payload["commit_message"] = strings.TrimSpace(*message)
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "PUT", Path: path.Join("/repos", repoOwner, repoName, "pulls", strconv.Itoa(number), "merge"), JSONBody: payload, Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}
