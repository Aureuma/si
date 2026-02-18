package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"si/tools/si/internal/githubbridge"
)

func cmdGithubGit(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si github git <credential|setup>")
	if !routedOK {
		return
	}
	args = routedArgs
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "credential":
		cmdGithubGitCredential(args[1:])
	case "setup":
		cmdGithubGitSetup(args[1:])
	default:
		printUnknown("github git", args[0])
	}
}

func cmdGithubGitCredential(args []string) {
	op := "get"
	if len(args) > 0 && !strings.HasPrefix(strings.TrimSpace(args[0]), "-") {
		value := strings.ToLower(strings.TrimSpace(args[0]))
		if value == "get" || value == "store" || value == "erase" {
			op = value
			args = args[1:]
		}
	}

	fs := flag.NewFlagSet("github git credential", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		if fs.NArg() == 1 {
			trailing := strings.ToLower(strings.TrimSpace(fs.Arg(0)))
			if trailing == "get" || trailing == "store" || trailing == "erase" {
				op = trailing
			} else {
				printUsage("usage: si github git credential [get|store|erase] [--account <alias>] [--owner <owner>] [--auth-mode <app|oauth>]")
				return
			}
		} else {
			printUsage("usage: si github git credential [get|store|erase] [--account <alias>] [--owner <owner>] [--auth-mode <app|oauth>]")
			return
		}
	}
	if op != "get" {
		return
	}

	req, err := readGitCredentialRequest(os.Stdin)
	if err != nil {
		fatal(err)
	}
	parsedOwner, parsedRepo := gitOwnerRepoFromCredentialPath(req.Path)
	ownerFlag := strings.TrimSpace(*owner)
	if ownerFlag == "" {
		ownerFlag = parsedOwner
	}
	runtime, err := resolveGithubRuntimeContext(*account, ownerFlag, *baseURL, githubAuthOverrides{
		AuthMode:       *authMode,
		AccessToken:    *token,
		AppID:          *appID,
		AppKey:         *appKey,
		InstallationID: *installationID,
	})
	if err != nil {
		fatal(err)
	}
	if !isGitCredentialHostAllowed(req.Host, runtime.BaseURL) {
		return
	}
	tokenResp, err := runtime.Provider.Token(context.Background(), githubbridge.TokenRequest{
		Owner: firstNonEmpty(runtime.Owner, parsedOwner),
		Repo:  parsedRepo,
	})
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(tokenResp.Value) == "" {
		fatal(fmt.Errorf("github auth token is empty"))
	}
	fmt.Fprintf(os.Stdout, "username=%s\n", "x-access-token")
	fmt.Fprintf(os.Stdout, "password=%s\n\n", tokenResp.Value)
}

type githubGitSetupResult struct {
	Root          string                     `json:"root"`
	DryRun        bool                       `json:"dry_run"`
	ReposScanned  int                        `json:"repos_scanned"`
	ReposUpdated  int                        `json:"repos_updated"`
	ReposSkipped  int                        `json:"repos_skipped"`
	Hosts         []string                   `json:"hosts"`
	HelperCommand string                     `json:"helper_command,omitempty"`
	Changes       []githubGitSetupRepoChange `json:"changes"`
}

type githubGitSetupRepoChange struct {
	Repo       string `json:"repo"`
	Remote     string `json:"remote"`
	Before     string `json:"before"`
	After      string `json:"after,omitempty"`
	PushBefore string `json:"push_before,omitempty"`
	PushAfter  string `json:"push_after,omitempty"`
	Changed    bool   `json:"changed"`
	Skipped    string `json:"skipped,omitempty"`
}

func cmdGithubGitSetup(args []string) {
	fs := flag.NewFlagSet("github git setup", flag.ExitOnError)
	root := fs.String("root", "", "root directory containing repositories (default: ~/Development)")
	remote := fs.String("remote", "origin", "remote name")
	dryRun := fs.Bool("dry-run", false, "preview changes without writing git config/remotes")
	noVault := fs.Bool("no-vault", false, "configure helper without wrapping through `si vault run`")
	vaultFile := fs.String("vault-file", "", "explicit vault env file for helper auth")
	vaultIdentityFile := fs.String("vault-identity-file", "", "identity key path exported as SI_VAULT_IDENTITY_FILE in helper")
	account := fs.String("account", "", "account alias passed to helper")
	owner := fs.String("owner", "", "owner/org passed to helper and auth probe")
	helperOwner := fs.String("helper-owner", "", "optional fixed owner/org for helper (defaults to deriving from git credential path)")
	baseURL := fs.String("base-url", "", "github api base url passed to helper")
	authMode := fs.String("auth-mode", "", "auth mode passed to helper")
	token := fs.String("token", "", "override oauth access token (probe only)")
	appID := fs.Int64("app-id", 0, "override app id (probe only)")
	appKey := fs.String("app-key", "", "override app private key pem (probe only)")
	installationID := fs.Int64("installation-id", 0, "override installation id (probe only)")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si github git setup [--root <path>] [--remote <name>] [--account <alias>] [--owner <owner>] [--helper-owner <owner>] [--vault-file <path>] [--vault-identity-file <path>] [--dry-run] [--json]")
		return
	}

	rootPath, err := resolveGitReposRoot(*root)
	if err != nil {
		fatal(err)
	}
	repos, err := listGitRepos(rootPath)
	if err != nil {
		fatal(err)
	}
	if len(repos) == 0 {
		fatal(fmt.Errorf("no git repositories found under %s", rootPath))
	}

	changes := make([]githubGitSetupRepoChange, 0, len(repos))
	hosts := map[string]struct{}{}
	var firstOwner string
	var firstRepo string

	for _, repoPath := range repos {
		change := githubGitSetupRepoChange{
			Repo:   repoPath,
			Remote: strings.TrimSpace(*remote),
		}
		before, err := gitRemoteGetURL(repoPath, strings.TrimSpace(*remote), false)
		if err != nil {
			change.Skipped = err.Error()
			changes = append(changes, change)
			continue
		}
		change.Before = before
		pushBefore, _ := gitRemoteGetURL(repoPath, strings.TrimSpace(*remote), true)
		change.PushBefore = pushBefore

		normalized, ok := normalizeGitHubRemoteURL(before)
		if !ok {
			change.Skipped = "remote is not a supported github URL"
			changes = append(changes, change)
			continue
		}
		change.After = normalized.URL
		if firstOwner == "" {
			firstOwner = normalized.Owner
			firstRepo = normalized.Repo
		}
		hosts[normalized.Host] = struct{}{}

		change.PushAfter = normalized.URL
		if strings.TrimSpace(pushBefore) != "" {
			if normalizedPush, pushOK := normalizeGitHubRemoteURL(pushBefore); pushOK {
				change.PushAfter = normalizedPush.URL
				hosts[normalizedPush.Host] = struct{}{}
			}
		}
		change.Changed = strings.TrimSpace(change.Before) != strings.TrimSpace(change.After) ||
			(strings.TrimSpace(change.PushBefore) != "" && strings.TrimSpace(change.PushBefore) != strings.TrimSpace(change.PushAfter))

		if change.Changed && !*dryRun {
			if err := gitRemoteSetURL(repoPath, strings.TrimSpace(*remote), change.After, false); err != nil {
				fatal(err)
			}
			if strings.TrimSpace(change.PushBefore) != "" {
				if err := gitRemoteSetURL(repoPath, strings.TrimSpace(*remote), change.PushAfter, true); err != nil {
					fatal(err)
				}
			}
		}
		changes = append(changes, change)
	}

	hostList := make([]string, 0, len(hosts))
	for host := range hosts {
		hostList = append(hostList, host)
	}
	sort.Strings(hostList)
	if len(hostList) == 0 {
		fatal(fmt.Errorf("no github remotes found under %s", rootPath))
	}

	probeOwner := strings.TrimSpace(*owner)
	if probeOwner == "" {
		probeOwner = firstOwner
	}
	if probeOwner == "" {
		fatal(fmt.Errorf("owner is required for auth probe; pass --owner"))
	}
	if err := probeGitHubGitAuth(*account, probeOwner, firstRepo, *baseURL, *authMode, *token, *appID, *appKey, *installationID); err != nil {
		fatal(err)
	}

	identityFile := strings.TrimSpace(*vaultIdentityFile)
	if identityFile == "" {
		identityFile = strings.TrimSpace(os.Getenv("SI_VAULT_IDENTITY_FILE"))
	}
	if identityFile == "" && !*noVault {
		settings := loadSettingsOrDefault()
		if strings.EqualFold(strings.TrimSpace(settings.Vault.KeyBackend), "file") {
			identityFile = strings.TrimSpace(expandTilde(settings.Vault.KeyFile))
		}
	}

	helperCommand := buildGitHubCredentialHelperCommand(githubGitHelperOptions{
		UseVault:       !*noVault,
		VaultFile:      strings.TrimSpace(*vaultFile),
		VaultIdentity:  identityFile,
		Account:        strings.TrimSpace(*account),
		Owner:          strings.TrimSpace(*helperOwner),
		BaseURL:        strings.TrimSpace(*baseURL),
		AuthMode:       strings.TrimSpace(*authMode),
		AccessToken:    strings.TrimSpace(*token),
		AppID:          *appID,
		AppKey:         strings.TrimSpace(*appKey),
		InstallationID: *installationID,
	})

	if !*dryRun {
		for _, host := range hostList {
			if err := gitConfigHostCredentialHelper(host, helperCommand); err != nil {
				fatal(err)
			}
			if err := gitConfigHostUseHTTPPath(host); err != nil {
				fatal(err)
			}
		}
	}

	result := githubGitSetupResult{
		Root:          rootPath,
		DryRun:        *dryRun,
		ReposScanned:  len(repos),
		ReposUpdated:  countChangedChanges(changes),
		ReposSkipped:  countSkippedChanges(changes),
		Hosts:         hostList,
		HelperCommand: helperCommand,
		Changes:       changes,
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			fatal(err)
		}
		return
	}

	fmt.Printf("%s %s\n", styleHeading("GitHub git setup:"), styleSuccess("completed"))
	fmt.Printf("%s %s\n", styleHeading("Root:"), rootPath)
	fmt.Printf("%s %d scanned, %d changed, %d skipped\n", styleHeading("Repos:"), result.ReposScanned, result.ReposUpdated, result.ReposSkipped)
	fmt.Printf("%s %s\n", styleHeading("Hosts:"), strings.Join(result.Hosts, ", "))
	if *dryRun {
		fmt.Printf("%s %s\n", styleHeading("Mode:"), "dry-run")
	}
	rows := make([][]string, 0, len(changes))
	for _, item := range changes {
		status := styleDim("skip")
		detail := item.Skipped
		if item.Changed {
			status = styleSuccess("set")
			detail = item.After
		} else if item.Skipped == "" {
			status = styleDim("ok")
			detail = item.After
		}
		rows = append(rows, []string{status, item.Repo, detail})
	}
	printAlignedRows(rows, 2, "  ")
}

type githubGitCredentialRequest struct {
	Protocol string
	Host     string
	Path     string
}

func readGitCredentialRequest(r io.Reader) (githubGitCredentialRequest, error) {
	scanner := bufio.NewScanner(r)
	payload := map[string]string{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		payload[strings.TrimSpace(strings.ToLower(key))] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return githubGitCredentialRequest{}, err
	}
	req := githubGitCredentialRequest{
		Protocol: strings.TrimSpace(payload["protocol"]),
		Host:     strings.TrimSpace(payload["host"]),
		Path:     strings.TrimSpace(payload["path"]),
	}
	if rawURL := strings.TrimSpace(payload["url"]); rawURL != "" {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return githubGitCredentialRequest{}, fmt.Errorf("parse credential url: %w", err)
		}
		if req.Protocol == "" {
			req.Protocol = strings.TrimSpace(parsed.Scheme)
		}
		if req.Host == "" {
			req.Host = strings.TrimSpace(parsed.Host)
		}
		if req.Path == "" {
			req.Path = strings.TrimSpace(parsed.Path)
		}
	}
	req.Host = normalizeGitHost(req.Host)
	if req.Host == "" {
		return githubGitCredentialRequest{}, fmt.Errorf("git credential request is missing host")
	}
	return req, nil
}

func gitOwnerRepoFromCredentialPath(path string) (string, string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", ""
	}
	path = strings.TrimPrefix(path, "/")
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", ""
	}
	owner := strings.TrimSpace(parts[0])
	repo := strings.TrimSpace(strings.TrimSuffix(parts[1], ".git"))
	if owner == "" || repo == "" {
		return "", ""
	}
	return owner, repo
}

func isGitCredentialHostAllowed(host string, baseURL string) bool {
	host = normalizeGitHost(host)
	if host == "" {
		return false
	}
	allowed := map[string]struct{}{"github.com": {}}
	if parsed, err := url.Parse(strings.TrimSpace(baseURL)); err == nil {
		baseHost := normalizeGitHost(parsed.Host)
		if baseHost != "" {
			allowed[baseHost] = struct{}{}
			if strings.HasPrefix(baseHost, "api.") {
				allowed[strings.TrimPrefix(baseHost, "api.")] = struct{}{}
			}
		}
	}
	_, ok := allowed[host]
	return ok
}

type githubRemoteNormalized struct {
	Host  string
	Owner string
	Repo  string
	URL   string
}

func normalizeGitHubRemoteURL(raw string) (githubRemoteNormalized, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return githubRemoteNormalized{}, false
	}

	host := ""
	path := ""

	switch {
	case strings.HasPrefix(raw, "git@"):
		withoutUser := strings.TrimPrefix(raw, "git@")
		parsedHost, parsedPath, ok := strings.Cut(withoutUser, ":")
		if !ok {
			return githubRemoteNormalized{}, false
		}
		host = parsedHost
		path = parsedPath
	default:
		u, err := url.Parse(raw)
		if err != nil || strings.TrimSpace(u.Host) == "" {
			return githubRemoteNormalized{}, false
		}
		host = u.Host
		path = strings.TrimPrefix(strings.TrimSpace(u.Path), "/")
	}

	host = normalizeGitHost(host)
	if !looksLikeGitHubHost(host) {
		return githubRemoteNormalized{}, false
	}

	owner, repo := gitOwnerRepoFromCredentialPath(path)
	if owner == "" || repo == "" {
		return githubRemoteNormalized{}, false
	}
	canonical := githubRemoteNormalized{
		Host:  host,
		Owner: owner,
		Repo:  repo,
		URL:   fmt.Sprintf("https://%s/%s/%s.git", host, owner, repo),
	}
	return canonical, true
}

func normalizeGitHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "ssh://")
	if strings.Contains(host, "@") {
		_, right, ok := strings.Cut(host, "@")
		if ok {
			host = right
		}
	}
	if idx := strings.Index(host, "/"); idx >= 0 {
		host = host[:idx]
	}
	return strings.TrimSpace(host)
}

func looksLikeGitHubHost(host string) bool {
	host = normalizeGitHost(host)
	return host == "github.com" || strings.Contains(host, "github")
}

func resolveGitReposRoot(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value != "" {
		return filepath.Clean(expandTilde(value)), nil
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		candidate := filepath.Join(home, "Development")
		if stat, statErr := os.Stat(candidate); statErr == nil && stat.IsDir() {
			return candidate, nil
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	parts := strings.Split(filepath.Clean(wd), string(filepath.Separator))
	for idx := 0; idx < len(parts); idx++ {
		if parts[idx] == "Development" {
			prefix := parts[:idx+1]
			if len(prefix) == 0 {
				return string(filepath.Separator) + "Development", nil
			}
			joined := strings.Join(prefix, string(filepath.Separator))
			if !strings.HasPrefix(joined, string(filepath.Separator)) {
				joined = string(filepath.Separator) + joined
			}
			return joined, nil
		}
	}
	return "", fmt.Errorf("unable to determine repo root, pass --root")
}

func listGitRepos(root string) ([]string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("root path is required")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	repos := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		repoPath := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(repoPath, ".git")); err == nil {
			repos = append(repos, repoPath)
		}
	}
	sort.Strings(repos)
	return repos, nil
}

func gitRemoteGetURL(repoPath string, remote string, push bool) (string, error) {
	args := []string{"-C", repoPath, "remote", "get-url"}
	if push {
		args = append(args, "--push")
	}
	args = append(args, remote)
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git remote get-url %s (%s): %w: %s", remote, repoPath, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func gitRemoteSetURL(repoPath string, remote string, value string, push bool) error {
	args := []string{"-C", repoPath, "remote", "set-url"}
	if push {
		args = append(args, "--push")
	}
	args = append(args, remote, value)
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git remote set-url %s (%s): %w: %s", remote, repoPath, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func probeGitHubGitAuth(account string, owner string, repo string, baseURL string, authMode string, accessToken string, appID int64, appKey string, installationID int64) error {
	runtime, err := resolveGithubRuntimeContext(account, owner, baseURL, githubAuthOverrides{
		AuthMode:       authMode,
		AccessToken:    accessToken,
		AppID:          appID,
		AppKey:         appKey,
		InstallationID: installationID,
	})
	if err != nil {
		return err
	}
	tokenResp, err := runtime.Provider.Token(context.Background(), githubbridge.TokenRequest{
		Owner: runtime.Owner,
		Repo:  strings.TrimSpace(repo),
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(tokenResp.Value) == "" {
		return fmt.Errorf("github auth probe returned empty token")
	}
	return nil
}

func gitConfigHostCredentialHelper(host string, helper string) error {
	host = normalizeGitHost(host)
	if host == "" {
		return fmt.Errorf("git credential host is required")
	}
	cmd := exec.Command("git", "config", "--global", "--replace-all", "credential.https://"+host+".helper", helper)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git config helper for %s: %w: %s", host, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func gitConfigHostUseHTTPPath(host string) error {
	host = normalizeGitHost(host)
	if host == "" {
		return fmt.Errorf("git credential host is required")
	}
	cmd := exec.Command("git", "config", "--global", "credential.https://"+host+".useHttpPath", "true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git config useHttpPath for %s: %w: %s", host, err, strings.TrimSpace(string(out)))
	}
	return nil
}

type githubGitHelperOptions struct {
	UseVault       bool
	VaultFile      string
	VaultIdentity  string
	Account        string
	Owner          string
	BaseURL        string
	AuthMode       string
	AccessToken    string
	AppID          int64
	AppKey         string
	InstallationID int64
}

func buildGitHubCredentialHelperCommand(opts githubGitHelperOptions) string {
	parts := make([]string, 0, 16)
	prefix := "!si"
	if strings.TrimSpace(opts.VaultIdentity) != "" {
		prefix = "!SI_VAULT_IDENTITY_FILE=" + shellQuote(strings.TrimSpace(opts.VaultIdentity)) + " si"
	}
	parts = append(parts, prefix)
	if opts.UseVault {
		parts = append(parts, "vault", "run")
		if strings.TrimSpace(opts.VaultFile) != "" {
			parts = append(parts, "--file", shellQuote(strings.TrimSpace(opts.VaultFile)))
		}
		parts = append(parts, "--", "si")
	}
	parts = append(parts, "github", "git", "credential")
	appendArg := func(flagName string, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		parts = append(parts, flagName, shellQuote(value))
	}
	appendArg("--account", opts.Account)
	appendArg("--owner", opts.Owner)
	appendArg("--base-url", opts.BaseURL)
	appendArg("--auth-mode", opts.AuthMode)
	appendArg("--token", opts.AccessToken)
	if opts.AppID > 0 {
		parts = append(parts, "--app-id", fmt.Sprintf("%d", opts.AppID))
	}
	appendArg("--app-key", opts.AppKey)
	if opts.InstallationID > 0 {
		parts = append(parts, "--installation-id", fmt.Sprintf("%d", opts.InstallationID))
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"\\$`") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func countChangedChanges(items []githubGitSetupRepoChange) int {
	total := 0
	for _, item := range items {
		if item.Changed {
			total++
		}
	}
	return total
}

func countSkippedChanges(items []githubGitSetupRepoChange) int {
	total := 0
	for _, item := range items {
		if strings.TrimSpace(item.Skipped) != "" {
			total++
		}
	}
	return total
}
