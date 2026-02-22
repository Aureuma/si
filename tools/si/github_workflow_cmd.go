package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"si/tools/si/internal/githubbridge"
)

func cmdGithubWorkflow(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si github workflow <list|run|runs|logs|watch> ...")
	if !routedOK {
		return
	}
	args = routedArgs
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdGithubWorkflowList(args[1:])
	case "run":
		cmdGithubWorkflowRun(args[1:])
	case "runs":
		cmdGithubWorkflowRuns(args[1:])
	case "logs":
		cmdGithubWorkflowLogs(args[1:])
	case "watch":
		cmdGithubWorkflowWatch(args[1:])
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

type githubWorkflowRunStatus struct {
	ID         int
	Name       string
	Status     string
	Conclusion string
	HTMLURL    string
	HeadBranch string
	Event      string
}

func cmdGithubWorkflowWatch(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github workflow watch", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	intervalSeconds := fs.Int("interval-seconds", 10, "poll interval in seconds")
	timeoutSeconds := fs.Int("timeout-seconds", 1800, "max wait time in seconds")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github workflow watch <owner/repo|repo> <run-id> [--interval-seconds <n>] [--timeout-seconds <n>] [--json]")
		return
	}
	if *intervalSeconds <= 0 {
		fatal(fmt.Errorf("--interval-seconds must be > 0"))
	}
	if *timeoutSeconds <= 0 {
		fatal(fmt.Errorf("--timeout-seconds must be > 0"))
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
	if !*jsonOut {
		fmt.Printf("%s waiting for run %d on %s/%s (interval=%ds timeout=%ds)\n", styleHeading("GitHub workflow watch:"), runID, repoOwner, repoName, *intervalSeconds, *timeoutSeconds)
	}
	deadline := time.Now().Add(time.Duration(*timeoutSeconds) * time.Second)
	lastState := ""
	for {
		if time.Now().After(deadline) {
			fatal(fmt.Errorf("workflow run %d timed out after %d seconds", runID, *timeoutSeconds))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		resp, err := client.Do(ctx, githubbridge.Request{Method: "GET", Path: path.Join("/repos", repoOwner, repoName, "actions", "runs", strconv.Itoa(runID)), Owner: repoOwner, Repo: repoName})
		cancel()
		if err != nil {
			printGithubError(err)
			return
		}
		status := githubWorkflowRunStatusFromData(resp.Data)
		state := status.Status + "|" + status.Conclusion
		if !*jsonOut && state != lastState {
			title := strings.TrimSpace(status.Name)
			if title == "" {
				title = strconv.Itoa(runID)
			}
			fmt.Printf("%s status=%s conclusion=%s branch=%s event=%s title=%s\n", time.Now().Format(time.RFC3339), orDash(status.Status), orDash(status.Conclusion), orDash(status.HeadBranch), orDash(status.Event), title)
			lastState = state
		}
		if strings.EqualFold(status.Status, "completed") {
			printGithubResponse(resp, *jsonOut, *raw)
			if githubWorkflowRunIsFailureConclusion(status.Conclusion) {
				if !*jsonOut {
					fmt.Fprintf(os.Stderr, "%s workflow run %d finished with conclusion=%s\n", styleError("github workflow failed:"), runID, orDash(status.Conclusion))
				}
				os.Exit(1)
			}
			return
		}
		time.Sleep(time.Duration(*intervalSeconds) * time.Second)
	}
}

func githubWorkflowRunStatusFromData(data map[string]any) githubWorkflowRunStatus {
	return githubWorkflowRunStatus{
		ID:         githubWorkflowInt(data["id"]),
		Name:       strings.TrimSpace(stringifyGitHubAny(data["name"])),
		Status:     strings.TrimSpace(stringifyGitHubAny(data["status"])),
		Conclusion: strings.TrimSpace(stringifyGitHubAny(data["conclusion"])),
		HTMLURL:    strings.TrimSpace(stringifyGitHubAny(data["html_url"])),
		HeadBranch: strings.TrimSpace(stringifyGitHubAny(data["head_branch"])),
		Event:      strings.TrimSpace(stringifyGitHubAny(data["event"])),
	}
}

func githubWorkflowInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	default:
		return 0
	}
}

func githubWorkflowRunIsFailureConclusion(conclusion string) bool {
	switch strings.ToLower(strings.TrimSpace(conclusion)) {
	case "success", "skipped", "neutral":
		return false
	default:
		return true
	}
}
