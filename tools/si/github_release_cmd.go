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

func cmdGithubRelease(args []string) {
	if len(args) == 0 {
		printUsage("usage: si github release <list|get|create|upload|delete> ...")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdGithubReleaseList(args[1:])
	case "get":
		cmdGithubReleaseGet(args[1:])
	case "create":
		cmdGithubReleaseCreate(args[1:])
	case "upload":
		cmdGithubReleaseUpload(args[1:])
	case "delete":
		cmdGithubReleaseDelete(args[1:])
	default:
		printUnknown("github release", args[0])
	}
}

func cmdGithubReleaseList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github release list", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	maxPages := fs.Int("max-pages", 5, "max pages to fetch")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si github release list <owner/repo|repo> [--owner <owner>] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	printGithubContextBanner(runtime)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	items, err := client.ListAll(ctx, githubbridge.Request{Method: "GET", Path: path.Join("/repos", repoOwner, repoName, "releases"), Params: parseGitHubParams(params), Owner: repoOwner, Repo: repoName}, *maxPages)
	if err != nil {
		printGithubError(err)
		return
	}
	resp := githubbridge.Response{StatusCode: 200, Status: "200 OK", List: items}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubReleaseGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github release get", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github release get <owner/repo|repo> <tag|id> [--owner <owner>] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	releaseRef := strings.TrimSpace(fs.Arg(1))
	if releaseRef == "" {
		fatal(fmt.Errorf("release tag or id is required"))
	}
	endpoint := path.Join("/repos", repoOwner, repoName, "releases", releaseRef)
	if _, err := strconv.Atoi(releaseRef); err != nil {
		endpoint = path.Join("/repos", repoOwner, repoName, "releases", "tags", releaseRef)
	}
	printGithubContextBanner(runtime)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "GET", Path: endpoint, Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubReleaseCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "draft": true, "prerelease": true})
	fs := flag.NewFlagSet("github release create", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	tag := fs.String("tag", "", "release tag name")
	title := fs.String("title", "", "release title")
	notes := fs.String("notes", "", "release notes body")
	notesFile := fs.String("notes-file", "", "path to release notes file")
	target := fs.String("target", "", "target commitish")
	draft := fs.Bool("draft", false, "create as draft")
	prerelease := fs.Bool("prerelease", false, "mark prerelease")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si github release create <owner/repo|repo> --tag <tag> --title <title> [--notes <text>|--notes-file <path>] [--json]")
		return
	}
	if strings.TrimSpace(*tag) == "" || strings.TrimSpace(*title) == "" {
		fatal(fmt.Errorf("--tag and --title are required"))
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	notesText := strings.TrimSpace(*notes)
	if strings.TrimSpace(*notesFile) != "" {
		rawNotes, readErr := os.ReadFile(strings.TrimSpace(*notesFile))
		if readErr != nil {
			fatal(readErr)
		}
		notesText = string(rawNotes)
	}
	payload := parseGitHubBodyParams(params)
	payload["tag_name"] = strings.TrimSpace(*tag)
	payload["name"] = strings.TrimSpace(*title)
	if notesText != "" {
		payload["body"] = notesText
	}
	if strings.TrimSpace(*target) != "" {
		payload["target_commitish"] = strings.TrimSpace(*target)
	}
	if *draft {
		payload["draft"] = true
	}
	if *prerelease {
		payload["prerelease"] = true
	}
	printGithubContextBanner(runtime)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "POST", Path: path.Join("/repos", repoOwner, repoName, "releases"), JSONBody: payload, Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubReleaseUpload(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github release upload", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	assetPath := fs.String("asset", "", "asset file path")
	label := fs.String("label", "", "asset label")
	contentType := fs.String("content-type", "application/octet-stream", "asset content type")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github release upload <owner/repo|repo> <tag|id> --asset <path> [--label <label>] [--content-type <type>] [--json]")
		return
	}
	if strings.TrimSpace(*assetPath) == "" {
		fatal(fmt.Errorf("--asset is required"))
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	releaseRef := strings.TrimSpace(fs.Arg(1))
	if releaseRef == "" {
		fatal(fmt.Errorf("release tag or id is required"))
	}
	releaseID, err := resolveReleaseID(context.Background(), client, repoOwner, repoName, releaseRef)
	if err != nil {
		fatal(err)
	}
	assetBytes, err := os.ReadFile(strings.TrimSpace(*assetPath))
	if err != nil {
		fatal(err)
	}
	assetName := path.Base(strings.TrimSpace(*assetPath))
	query := map[string]string{"name": assetName}
	if strings.TrimSpace(*label) != "" {
		query["label"] = strings.TrimSpace(*label)
	}
	uploadURL := "https://uploads.github.com" + path.Join("/repos", repoOwner, repoName, "releases", strconv.Itoa(releaseID), "assets")
	printGithubContextBanner(runtime)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "POST", Path: uploadURL, Params: query, RawBody: string(assetBytes), ContentType: strings.TrimSpace(*contentType), Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubReleaseDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("github release delete", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github release delete <owner/repo|repo> <tag|id> [--force] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	releaseRef := strings.TrimSpace(fs.Arg(1))
	releaseID, err := resolveReleaseID(context.Background(), client, repoOwner, repoName, releaseRef)
	if err != nil {
		fatal(err)
	}
	if err := requireGithubConfirmation("delete release "+releaseRef+" from "+repoOwner+"/"+repoName, *force); err != nil {
		fatal(err)
	}
	printGithubContextBanner(runtime)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "DELETE", Path: path.Join("/repos", repoOwner, repoName, "releases", strconv.Itoa(releaseID)), Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func resolveReleaseID(ctx context.Context, client githubBridgeClient, owner string, repo string, ref string) (int, error) {
	if id, err := strconv.Atoi(strings.TrimSpace(ref)); err == nil && id > 0 {
		return id, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "GET", Path: path.Join("/repos", owner, repo, "releases", "tags", strings.TrimSpace(ref)), Owner: owner, Repo: repo})
	if err != nil {
		return 0, err
	}
	if resp.Data == nil {
		return 0, fmt.Errorf("release response missing data")
	}
	rawID, ok := resp.Data["id"]
	if !ok {
		return 0, fmt.Errorf("release response missing id")
	}
	switch typed := rawID.(type) {
	case float64:
		return int(typed), nil
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	default:
		return 0, fmt.Errorf("invalid release id type")
	}
}
