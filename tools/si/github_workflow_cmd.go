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

func cmdGithubWorkflow(args []string) {
	if len(args) == 0 {
		printUsage("usage: si github workflow <list|run|runs|logs> ...")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdGithubWorkflowList(args[1:])
	case "run":
		cmdGithubWorkflowRun(args[1:])
	case "runs":
		cmdGithubWorkflowRuns(args[1:])
	case "logs":
		cmdGithubWorkflowLogs(args[1:])
	default:
		printUnknown("github workflow", args[0])
	}
}

func cmdGithubWorkflowList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github workflow list", flag.ExitOnError)
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
		printUsage("usage: si github workflow list <owner/repo|repo> [--owner <owner>] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "GET", Path: path.Join("/repos", repoOwner, repoName, "actions", "workflows"), Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	if workflows, ok := resp.Data["workflows"].([]any); ok {
		list := make([]map[string]any, 0, len(workflows))
		for _, item := range workflows {
			if obj, ok := item.(map[string]any); ok {
				list = append(list, obj)
			}
		}
		resp.List = list
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubWorkflowRun(args []string) {
	if len(args) > 0 {
		sub := strings.ToLower(strings.TrimSpace(args[0]))
		switch sub {
		case "get":
			cmdGithubWorkflowRunGet(args[1:])
			return
		case "cancel":
			cmdGithubWorkflowRunCancel(args[1:])
			return
		case "rerun":
			cmdGithubWorkflowRunRerun(args[1:])
			return
		}
	}
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github workflow run", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	ref := fs.String("ref", "", "git ref (branch/tag/sha)")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	inputs := multiFlag{}
	fs.Var(&inputs, "input", "workflow input key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github workflow run <owner/repo|repo> <workflow-id|file> --ref <ref> [--input key=value] [--json]")
		return
	}
	if strings.TrimSpace(*ref) == "" {
		fatal(fmt.Errorf("--ref is required"))
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	workflow := strings.TrimSpace(fs.Arg(1))
	if workflow == "" {
		fatal(fmt.Errorf("workflow id/file is required"))
	}
	payload := map[string]any{"ref": strings.TrimSpace(*ref)}
	if len(inputs) > 0 {
		inputMap := map[string]string{}
		for key, value := range parseGitHubParams(inputs) {
			inputMap[key] = value
		}
		payload["inputs"] = inputMap
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "POST", Path: path.Join("/repos", repoOwner, repoName, "actions", "workflows", workflow, "dispatches"), JSONBody: payload, Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubWorkflowRuns(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github workflow runs", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	workflow := fs.String("workflow", "", "optional workflow id/file")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si github workflow runs <owner/repo|repo> [--workflow <id|file>] [--param key=value] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	p := path.Join("/repos", repoOwner, repoName, "actions", "runs")
	if strings.TrimSpace(*workflow) != "" {
		p = path.Join("/repos", repoOwner, repoName, "actions", "workflows", strings.TrimSpace(*workflow), "runs")
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "GET", Path: p, Params: parseGitHubParams(params), Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	if runs, ok := resp.Data["workflow_runs"].([]any); ok {
		list := make([]map[string]any, 0, len(runs))
		for _, item := range runs {
			if obj, ok := item.(map[string]any); ok {
				list = append(list, obj)
			}
		}
		resp.List = list
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubWorkflowRunGet(args []string) {
	cmdGithubWorkflowRunAction(args, "get")
}

func cmdGithubWorkflowRunCancel(args []string) {
	cmdGithubWorkflowRunAction(args, "cancel")
}

func cmdGithubWorkflowRunRerun(args []string) {
	cmdGithubWorkflowRunAction(args, "rerun")
}

func cmdGithubWorkflowRunAction(args []string, action string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github workflow run action", flag.ExitOnError)
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
		printUsage("usage: si github workflow run " + action + " <owner/repo|repo> <run-id> [--owner <owner>] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	runID, err := parseGitHubNumber(fs.Arg(1), "run id")
	if err != nil {
		fatal(err)
	}
	method := "GET"
	p := path.Join("/repos", repoOwner, repoName, "actions", "runs", strconv.Itoa(runID))
	switch action {
	case "cancel":
		method = "POST"
		p = path.Join(p, "cancel")
	case "rerun":
		method = "POST"
		p = path.Join(p, "rerun")
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: method, Path: p, Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubWorkflowLogs(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github workflow logs", flag.ExitOnError)
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
		printUsage("usage: si github workflow logs <owner/repo|repo> <run-id> [--owner <owner>] [--raw]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	runID, err := parseGitHubNumber(fs.Arg(1), "run id")
	if err != nil {
		fatal(err)
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "GET", Path: path.Join("/repos", repoOwner, repoName, "actions", "runs", strconv.Itoa(runID), "logs"), Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}
