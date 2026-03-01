package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"si/tools/si/internal/githubbridge"
)

const releasemindUsageText = "usage: si releasemind <play|github>"

func cmdReleasemind(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, releasemindUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(releasemindUsageText)
	case "play":
		cmdReleasemindPlay(rest)
	case "github", "gh":
		cmdReleasemindGithub(rest)
	default:
		printUnknown("releasemind", cmd)
		printUsage(releasemindUsageText)
	}
}

func cmdReleasemindPlay(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si releasemind play <plan>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "plan", "prepare", "generate":
		cmdReleasemindPlayPlan(rest)
	default:
		printUnknown("releasemind play", sub)
		printUsage("usage: si releasemind play <plan>")
	}
}

func cmdReleasemindPlayPlan(args []string) {
	fs := flag.NewFlagSet("releasemind play plan", flag.ExitOnError)
	repoPath := fs.String("repo-path", ".", "local repository path to inspect")
	plannerRepo := fs.String("planner-repo", "", "path to releasemind repository")
	writePath := fs.String("write", "", "optional output file path")
	pretty := fs.Bool("pretty", true, "pretty print JSON output")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si releasemind play plan [--repo-path <path>] [--planner-repo <path>] [--write <file>] [--pretty=true|false]")
		return
	}

	resolvedRepoPath, err := filepath.Abs(strings.TrimSpace(*repoPath))
	if err != nil {
		fatal(err)
	}
	if _, err := os.Stat(resolvedRepoPath); err != nil {
		fatal(err)
	}

	resolvedPlannerRepo, err := resolveReleasemindPlannerRepo(strings.TrimSpace(*plannerRepo))
	if err != nil {
		fatal(err)
	}
	engineDir := filepath.Join(resolvedPlannerRepo, "engine", "playrelease")
	if _, err := os.Stat(filepath.Join(engineDir, "go.mod")); err != nil {
		fatal(fmt.Errorf("planner not found at %s", engineDir))
	}

	cmdArgs := []string{"run", "./cmd/rm-playrelease", "--repo-path", resolvedRepoPath}
	if !*pretty {
		cmdArgs = append(cmdArgs, "--pretty=false")
	}
	if strings.TrimSpace(*writePath) != "" {
		resolvedWritePath, err := filepath.Abs(strings.TrimSpace(*writePath))
		if err != nil {
			fatal(err)
		}
		cmdArgs = append(cmdArgs, "--write", resolvedWritePath)
	}

	cmd := exec.Command("go", cmdArgs...) // #nosec G204 -- command and args are controlled by fixed command and validated inputs.
	cmd.Dir = engineDir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		fatal(fmt.Errorf("planner failed: %s", msg))
	}
	if stderr.Len() > 0 {
		warnf("%s", strings.TrimSpace(stderr.String()))
	}
	if _, err := fmt.Fprint(os.Stdout, stdout.String()); err != nil {
		fatal(err)
	}
}

func resolveReleasemindPlannerRepo(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	if fromEnv := strings.TrimSpace(os.Getenv("SI_RELEASEMIND_REPO")); fromEnv != "" {
		return filepath.Abs(fromEnv)
	}
	candidates := []string{
		"../releasemind",
		"/home/shawn/Development/releasemind",
	}
	for _, candidate := range candidates {
		resolved, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(resolved, "engine", "playrelease", "go.mod")); err == nil {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("unable to locate releasemind planner repo; pass --planner-repo or set SI_RELEASEMIND_REPO")
}

func cmdReleasemindGithub(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si releasemind github <install-status>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "install-status", "installation-status", "status":
		cmdReleasemindGithubInstallStatus(rest)
	default:
		printUnknown("releasemind github", sub)
		printUsage("usage: si releasemind github <install-status>")
	}
}

func cmdReleasemindGithubInstallStatus(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("releasemind github install-status", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	repo := fs.String("repo", "", "repository owner/repo (required)")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*repo) == "" {
		printUsage("usage: si releasemind github install-status --repo <owner/repo> [--account <alias>] [--owner <owner>] [--auth-mode <app|oauth>] [--json]")
		return
	}
	repoOwner, repoName, err := parseGitHubOwnerRepo(*repo, "")
	if err != nil {
		fatal(err)
	}
	ownerValue := strings.TrimSpace(*owner)
	if ownerValue == "" {
		ownerValue = repoOwner
	}
	runtime, err := resolveGithubRuntimeContext(*account, ownerValue, *baseURL, githubAuthOverrides{
		AuthMode:       *authMode,
		AccessToken:    *token,
		AppID:          *appID,
		AppKey:         *appKey,
		InstallationID: *installationID,
	})
	fullRepo := repoOwner + "/" + repoName
	if err != nil {
		payload := map[string]any{
			"ok":        false,
			"repo":      fullRepo,
			"auth_mode": strings.TrimSpace(*authMode),
			"error":     err.Error(),
			"obstacles": []string{
				"GitHub authentication is not configured for non-interactive access",
			},
			"next_steps": []string{
				"Provide app auth (app id + private key [+ installation id]) or oauth token",
				"Retry: si releasemind github install-status --repo " + fullRepo + " --json",
			},
		}
		if *jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if encodeErr := enc.Encode(payload); encodeErr != nil {
				fatal(encodeErr)
			}
			os.Exit(1)
		}
		fmt.Printf("%s %s\n", styleHeading("ReleaseMind GitHub install check:"), styleError("blocked"))
		fmt.Printf("%s %s\n", styleHeading("Repository:"), fullRepo)
		fmt.Printf("%s %s\n", styleHeading("Error:"), err.Error())
		fmt.Println(styleHeading("Next steps:"))
		fmt.Println("  - Provide app auth (app id + private key [+ installation id]) or oauth token")
		fmt.Printf("  - Retry: si releasemind github install-status --repo %s --json\n", fullRepo)
		os.Exit(1)
	}
	client, err := buildGithubClient(runtime)
	if err != nil {
		fatal(err)
	}
	path := fmt.Sprintf("/repos/%s/%s", url.PathEscape(repoOwner), url.PathEscape(repoName))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{
		Method:         http.MethodGet,
		Path:           path,
		Owner:          repoOwner,
		Repo:           repoName,
		InstallationID: *installationID,
	})
	if err != nil {
		payload := map[string]any{
			"ok":        false,
			"repo":      fullRepo,
			"auth_mode": runtime.AuthMode,
			"context":   runtime,
			"error":     err.Error(),
		}
		statusCode := 0
		apiMessage := strings.TrimSpace(err.Error())
		var details *githubbridge.APIErrorDetails
		if errors.As(err, &details) {
			statusCode = details.StatusCode
			if msg := strings.TrimSpace(details.Message); msg != "" {
				apiMessage = msg
			}
			payload["api_error"] = details
		}
		obstacles := []string{}
		nextSteps := []string{}
		if runtime.AuthMode == githubbridge.AuthModeApp && (statusCode == 403 || statusCode == 404) {
			obstacles = append(obstacles,
				fmt.Sprintf("GitHub App token cannot access %s (status=%d)", fullRepo, statusCode),
				"Repository may not be granted to the current GitHub App installation",
			)
			nextSteps = append(nextSteps,
				"Install or grant the GitHub App to the target repository/org in GitHub settings",
				"After grant, rerun: si releasemind github install-status --repo "+fullRepo,
			)
		}
		if len(obstacles) == 0 {
			obstacles = append(obstacles, fmt.Sprintf("Repository access check failed: %s", apiMessage))
		}
		payload["obstacles"] = obstacles
		if len(nextSteps) > 0 {
			payload["next_steps"] = nextSteps
		}

		if *jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if encodeErr := enc.Encode(payload); encodeErr != nil {
				fatal(encodeErr)
			}
			os.Exit(1)
		}

		fmt.Printf("%s %s\n", styleHeading("ReleaseMind GitHub install check:"), styleError("blocked"))
		fmt.Printf("%s %s\n", styleHeading("Context:"), formatGithubContext(runtime))
		fmt.Printf("%s %s\n", styleHeading("Repository:"), fullRepo)
		fmt.Printf("%s %s\n", styleHeading("Error:"), apiMessage)
		rows := make([][]string, 0, len(obstacles))
		for _, item := range obstacles {
			rows = append(rows, []string{styleError("ERR"), item})
		}
		printAlignedRows(rows, 2, "  ")
		if len(nextSteps) > 0 {
			fmt.Println(styleHeading("Next steps:"))
			for _, step := range nextSteps {
				fmt.Printf("  - %s\n", step)
			}
		}
		os.Exit(1)
	}

	payload := map[string]any{
		"ok":        true,
		"repo":      fullRepo,
		"auth_mode": runtime.AuthMode,
		"context":   runtime,
		"repository": map[string]any{
			"id":             resp.Data["id"],
			"node_id":        resp.Data["node_id"],
			"full_name":      resp.Data["full_name"],
			"private":        resp.Data["private"],
			"default_branch": resp.Data["default_branch"],
		},
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}

	fmt.Printf("%s %s\n", styleHeading("ReleaseMind GitHub install check:"), styleSuccess("ok"))
	fmt.Printf("%s %s\n", styleHeading("Context:"), formatGithubContext(runtime))
	fmt.Printf("%s %s\n", styleHeading("Repository:"), fullRepo)
	fmt.Printf("%s %s\n", styleHeading("Result:"), "GitHub credentials can access repository for ReleaseMind automation")
}
