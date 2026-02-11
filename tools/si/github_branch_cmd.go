package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"si/tools/si/internal/githubbridge"
)

func cmdGithubBranch(args []string) {
	if len(args) == 0 {
		printUsage("usage: si github branch <list|get|create|delete|protect|unprotect> ...")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdGithubBranchList(args[1:])
	case "get":
		cmdGithubBranchGet(args[1:])
	case "create":
		cmdGithubBranchCreate(args[1:])
	case "delete", "remove":
		cmdGithubBranchDelete(args[1:])
	case "protect":
		cmdGithubBranchProtect(args[1:])
	case "unprotect":
		cmdGithubBranchUnprotect(args[1:])
	default:
		printUnknown("github branch", args[0])
	}
}

func cmdGithubBranchList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github branch list", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	protected := fs.String("protected", "", "filter protected branches (true|false)")
	maxPages := fs.Int("max-pages", 10, "max pages to fetch")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si github branch list <owner/repo|repo> [--owner <owner>] [--protected true|false] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	values := parseGitHubParams(params)
	if value := strings.TrimSpace(*protected); value != "" {
		if value != "true" && value != "false" {
			fatal(fmt.Errorf("invalid --protected %q (expected true|false)", value))
		}
		values["protected"] = value
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	items, err := client.ListAll(ctx, githubbridge.Request{
		Method: "GET",
		Path:   path.Join("/repos", repoOwner, repoName, "branches"),
		Params: values,
		Owner:  repoOwner,
		Repo:   repoName,
	}, *maxPages)
	if err != nil {
		printGithubError(err)
		return
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{"repo": repoOwner + "/" + repoName, "count": len(items), "data": items}); err != nil {
			fatal(err)
		}
		return
	}
	if *raw {
		rawBody, _ := json.Marshal(items)
		fmt.Println(string(rawBody))
		return
	}
	fmt.Printf("%s %s (%d)\n", styleHeading("Branch list:"), repoOwner+"/"+repoName, len(items))
	for _, item := range items {
		fmt.Printf("  %s\n", summarizeGitHubBranchItem(item))
	}
}

func cmdGithubBranchGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github branch get", flag.ExitOnError)
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
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github branch get <owner/repo|repo> <branch> [--owner <owner>] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	branchName := normalizeGitHubBranchName(fs.Arg(1))
	if branchName == "" {
		fatal(fmt.Errorf("branch is required"))
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{
		Method: "GET",
		Path:   path.Join("/repos", repoOwner, repoName, "branches") + "/" + escapeGitHubPathSegment(branchName),
		Params: parseGitHubParams(params),
		Owner:  repoOwner,
		Repo:   repoName,
	})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubBranchCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github branch create", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	name := fs.String("name", "", "new branch name")
	from := fs.String("from", "", "base branch name (defaults to repo default branch)")
	sha := fs.String("sha", "", "base commit SHA (overrides --from)")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() < 1 || fs.NArg() > 2 {
		printUsage("usage: si github branch create <owner/repo|repo> [branch] [--name <branch>] [--from <base_branch>|--sha <commit_sha>] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	branchName := normalizeGitHubBranchName(firstNonEmpty(*name, argOrEmpty(fs, 1)))
	if branchName == "" {
		fatal(fmt.Errorf("branch name is required (use [branch] or --name)"))
	}
	if strings.EqualFold(strings.TrimSpace(*sha), strings.TrimSpace(*from)) && strings.TrimSpace(*sha) != "" {
		fatal(fmt.Errorf("--sha and --from must not be the same value"))
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	baseSHA := strings.TrimSpace(*sha)
	baseSource := "--sha"
	if baseSHA == "" {
		resolvedSHA, source, resolveErr := resolveGitHubBranchCreateSHA(ctx, client, repoOwner, repoName, strings.TrimSpace(*from))
		if resolveErr != nil {
			fatal(resolveErr)
		}
		baseSHA = resolvedSHA
		baseSource = source
	}
	payload := map[string]any{
		"ref": "refs/heads/" + branchName,
		"sha": baseSHA,
	}
	resp, err := client.Do(ctx, githubbridge.Request{
		Method:   "POST",
		Path:     path.Join("/repos", repoOwner, repoName, "git", "refs"),
		JSONBody: payload,
		Owner:    repoOwner,
		Repo:     repoName,
	})
	if err != nil {
		printGithubError(err)
		return
	}
	if resp.Data == nil {
		resp.Data = map[string]any{}
	}
	if _, ok := resp.Data["base_sha_source"]; !ok {
		resp.Data["base_sha_source"] = baseSource
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubBranchDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("github branch delete", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	force := fs.Bool("force", false, "confirm branch deletion")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github branch delete <owner/repo|repo> <branch> [--owner <owner>] --force [--json]")
		return
	}
	if !*force {
		fatal(fmt.Errorf("branch deletion requires --force"))
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	branchName := normalizeGitHubBranchName(fs.Arg(1))
	if branchName == "" {
		fatal(fmt.Errorf("branch is required"))
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{
		Method: "DELETE",
		Path:   path.Join("/repos", repoOwner, repoName, "git", "refs", "heads") + "/" + escapeGitHubPathSegment(branchName),
		Owner:  repoOwner,
		Repo:   repoName,
	})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubBranchProtect(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{
		"json":                            true,
		"raw":                             true,
		"strict":                          true,
		"enforce-admins":                  true,
		"dismiss-stale-reviews":           true,
		"require-code-owner-reviews":      true,
		"require-last-push-approval":      true,
		"require-conversation-resolution": true,
		"allow-force-pushes":              true,
		"allow-deletions":                 true,
		"disable-status-checks":           true,
		"disable-pr-reviews":              true,
		"disable-restrictions":            true,
		"block-creations":                 true,
		"require-linear-history":          true,
		"lock-branch":                     true,
		"allow-fork-syncing":              true,
	})
	fs := flag.NewFlagSet("github branch protect", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	strictChecks := fs.Bool("strict", true, "require branches to be up to date before merge")
	enforceAdmins := fs.Bool("enforce-admins", true, "enforce branch protection for admins")
	requiredApprovals := fs.Int("required-approvals", 1, "required approving review count (0-6)")
	dismissStaleReviews := fs.Bool("dismiss-stale-reviews", false, "dismiss stale pull request approvals")
	requireCodeOwnerReviews := fs.Bool("require-code-owner-reviews", false, "require code owner reviews")
	requireLastPushApproval := fs.Bool("require-last-push-approval", false, "require last push approval")
	requireConversationResolution := fs.Bool("require-conversation-resolution", true, "require all conversations resolved before merge")
	allowForcePushes := fs.Bool("allow-force-pushes", false, "allow force pushes")
	allowDeletions := fs.Bool("allow-deletions", false, "allow branch deletions")
	disableStatusChecks := fs.Bool("disable-status-checks", false, "disable required status checks")
	disablePRReviews := fs.Bool("disable-pr-reviews", false, "disable pull request review requirement")
	disableRestrictions := fs.Bool("disable-restrictions", false, "disable push restrictions")
	blockCreations := fs.Bool("block-creations", false, "block branch creation")
	requireLinearHistory := fs.Bool("require-linear-history", false, "require linear commit history")
	lockBranch := fs.Bool("lock-branch", false, "lock branch (read-only)")
	allowForkSyncing := fs.Bool("allow-fork-syncing", false, "allow fork syncing when branch is locked")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	requiredChecks := multiFlag{}
	restrictUsers := multiFlag{}
	restrictTeams := multiFlag{}
	restrictApps := multiFlag{}
	fs.Var(&requiredChecks, "required-check", "required status check context (repeatable)")
	fs.Var(&restrictUsers, "restrict-user", "allow pushes only for user login (repeatable)")
	fs.Var(&restrictTeams, "restrict-team", "allow pushes only for team slug (repeatable)")
	fs.Var(&restrictApps, "restrict-app", "allow pushes only for app slug (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github branch protect <owner/repo|repo> <branch> [--required-check <context> ...] [--required-approvals <0-6>] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	branchName := normalizeGitHubBranchName(fs.Arg(1))
	if branchName == "" {
		fatal(fmt.Errorf("branch is required"))
	}
	checks := uniqueNonEmpty(requiredChecks)
	users := uniqueNonEmpty(restrictUsers)
	teams := uniqueNonEmpty(restrictTeams)
	apps := uniqueNonEmpty(restrictApps)
	approvals := *requiredApprovals
	if approvals < 0 {
		approvals = 0
	}
	if approvals > 6 {
		approvals = 6
	}

	var requiredStatusChecks any
	if !*disableStatusChecks && len(checks) > 0 {
		requiredStatusChecks = map[string]any{
			"strict": *strictChecks,
			"checks": checks,
		}
	}
	var requiredPRReviews any
	if !*disablePRReviews {
		requiredPRReviews = map[string]any{
			"dismiss_stale_reviews":           *dismissStaleReviews,
			"require_code_owner_reviews":      *requireCodeOwnerReviews,
			"require_last_push_approval":      *requireLastPushApproval,
			"required_approving_review_count": approvals,
		}
	}
	var restrictions any
	if !*disableRestrictions && (len(users) > 0 || len(teams) > 0 || len(apps) > 0) {
		restrictions = map[string]any{
			"users": users,
			"teams": teams,
			"apps":  apps,
		}
	}
	payload := map[string]any{
		"required_status_checks":           requiredStatusChecks,
		"enforce_admins":                   *enforceAdmins,
		"required_pull_request_reviews":    requiredPRReviews,
		"restrictions":                     restrictions,
		"required_conversation_resolution": *requireConversationResolution,
		"allow_force_pushes":               *allowForcePushes,
		"allow_deletions":                  *allowDeletions,
		"block_creations":                  *blockCreations,
		"required_linear_history":          *requireLinearHistory,
		"lock_branch":                      *lockBranch,
		"allow_fork_syncing":               *allowForkSyncing,
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{
		Method:   "PUT",
		Path:     path.Join("/repos", repoOwner, repoName, "branches") + "/" + escapeGitHubPathSegment(branchName) + "/protection",
		JSONBody: payload,
		Owner:    repoOwner,
		Repo:     repoName,
	})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubBranchUnprotect(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("github branch unprotect", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	force := fs.Bool("force", false, "confirm branch protection removal")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github branch unprotect <owner/repo|repo> <branch> [--owner <owner>] --force [--json]")
		return
	}
	if !*force {
		fatal(fmt.Errorf("branch protection removal requires --force"))
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	branchName := normalizeGitHubBranchName(fs.Arg(1))
	if branchName == "" {
		fatal(fmt.Errorf("branch is required"))
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{
		Method: "DELETE",
		Path:   path.Join("/repos", repoOwner, repoName, "branches") + "/" + escapeGitHubPathSegment(branchName) + "/protection",
		Owner:  repoOwner,
		Repo:   repoName,
	})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func resolveGitHubBranchCreateSHA(ctx context.Context, client githubBridgeClient, owner string, repo string, fromBranch string) (string, string, error) {
	if client == nil {
		return "", "", fmt.Errorf("github client is required")
	}
	selectedFrom := normalizeGitHubBranchName(fromBranch)
	if selectedFrom == "" {
		repoResp, err := client.Do(ctx, githubbridge.Request{Method: "GET", Path: path.Join("/repos", owner, repo), Owner: owner, Repo: repo})
		if err != nil {
			return "", "", err
		}
		defaultBranch := strings.TrimSpace(stringifyGitHubAny(repoResp.Data["default_branch"]))
		defaultBranch = normalizeGitHubBranchName(defaultBranch)
		if defaultBranch == "" || defaultBranch == "-" {
			defaultBranch = "main"
		}
		selectedFrom = defaultBranch
	}
	branchResp, err := client.Do(ctx, githubbridge.Request{
		Method: "GET",
		Path:   path.Join("/repos", owner, repo, "branches") + "/" + escapeGitHubPathSegment(selectedFrom),
		Owner:  owner,
		Repo:   repo,
	})
	if err != nil {
		return "", "", err
	}
	commit, _ := branchResp.Data["commit"].(map[string]any)
	sha := strings.TrimSpace(stringifyGitHubAny(commit["sha"]))
	if sha == "" || sha == "-" {
		return "", "", fmt.Errorf("base commit sha not found for branch %q", selectedFrom)
	}
	return sha, "branch:" + selectedFrom, nil
}

func normalizeGitHubBranchName(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.TrimPrefix(value, "refs/heads/")
	value = strings.TrimPrefix(value, "heads/")
	return strings.Trim(value, "/")
}

func escapeGitHubPathSegment(value string) string {
	value = strings.TrimSpace(value)
	return url.PathEscape(value)
}

func summarizeGitHubBranchItem(item map[string]any) string {
	name := strings.TrimSpace(stringifyGitHubAny(item["name"]))
	if name == "" || name == "-" {
		name = "(unknown)"
	}
	protected := false
	if value, ok := item["protected"].(bool); ok {
		protected = value
	}
	sha := "-"
	if commit, ok := item["commit"].(map[string]any); ok {
		sha = strings.TrimSpace(stringifyGitHubAny(commit["sha"]))
		if len(sha) > 12 {
			sha = sha[:12]
		}
	}
	icon := "open"
	if protected {
		icon = "protected"
	}
	return fmt.Sprintf("%s %s (%s)", name, sha, icon)
}

func uniqueNonEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func argOrEmpty(fs *flag.FlagSet, idx int) string {
	if fs == nil {
		return ""
	}
	if idx < 0 || idx >= fs.NArg() {
		return ""
	}
	return strings.TrimSpace(fs.Arg(idx))
}
