package main

import (
	"bufio"
	"context"
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

type githubGitRemoteAuthRepoChange struct {
	Repo       string `json:"repo"`
	Remote     string `json:"remote"`
	Owner      string `json:"owner,omitempty"`
	Name       string `json:"name,omitempty"`
	Before     string `json:"before,omitempty"`
	PushBefore string `json:"push_before,omitempty"`
	After      string `json:"after,omitempty"`
	Changed    bool   `json:"changed"`
	Tracking   string `json:"tracking,omitempty"`
	Skipped    string `json:"skipped,omitempty"`
	Error      string `json:"error,omitempty"`
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

func parseGitHubCloneSource(raw string) (githubRemoteNormalized, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return githubRemoteNormalized{}, fmt.Errorf("repository source is required")
	}

	if strings.Contains(raw, "://") || strings.HasPrefix(raw, "git@") {
		if normalized, ok := normalizeGitHubRemoteURL(raw); ok {
			return normalized, nil
		}
		return githubRemoteNormalized{}, fmt.Errorf("repository source must be a supported github URL or owner/repo")
	}

	if strings.HasPrefix(strings.ToLower(raw), "github.com/") {
		if normalized, ok := normalizeGitHubRemoteURL("https://" + raw); ok {
			return normalized, nil
		}
		return githubRemoteNormalized{}, fmt.Errorf("repository source must be a supported github URL or owner/repo")
	}

	owner, repo := gitOwnerRepoFromCredentialPath(raw)
	if owner == "" || repo == "" {
		return githubRemoteNormalized{}, fmt.Errorf("repository source must be <owner/repo> or a github URL")
	}

	return githubRemoteNormalized{
		Host:  "github.com",
		Owner: owner,
		Repo:  repo,
		URL:   fmt.Sprintf("https://github.com/%s/%s.git", owner, repo),
	}, nil
}

func planGitCloneDestination(root string, repoName string, dest string) string {
	root = filepath.Clean(strings.TrimSpace(root))
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return filepath.Join(root, strings.TrimSpace(repoName))
	}
	if filepath.IsAbs(dest) {
		return filepath.Clean(dest)
	}
	return filepath.Join(root, dest)
}

func ensureCloneDestinationAvailable(destination string) error {
	if strings.TrimSpace(destination) == "" {
		return fmt.Errorf("destination path is required")
	}
	stat, err := os.Stat(destination)
	switch {
	case err == nil && stat.IsDir():
		return fmt.Errorf("destination already exists: %s", destination)
	case err == nil && !stat.IsDir():
		return fmt.Errorf("destination path exists and is not a directory: %s", destination)
	case err != nil && !os.IsNotExist(err):
		return err
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create clone parent dir %s: %w", parent, err)
	}
	return nil
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

func resolveGitReposRoot(flagSet bool, value string, settings *Settings, cwd string) (string, error) {
	resolved, err := resolveWorkspaceRootDirectory(
		flagSet,
		strings.TrimSpace(value),
		strings.TrimSpace(os.Getenv("SI_WORKSPACE_ROOT")),
		settings,
		cwd,
	)
	if err != nil {
		return "", err
	}
	return resolved.Path, nil
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

func gitCloneRepository(remoteURL string, destination string, remote string) error {
	args := []string{"clone"}
	remote = strings.TrimSpace(remote)
	if remote != "" && remote != "origin" {
		args = append(args, "--origin", remote)
	}
	args = append(args, strings.TrimSpace(remoteURL), strings.TrimSpace(destination))
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone (%s): %w: %s", destination, err, strings.TrimSpace(string(out)))
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
	parts = append(parts, "!si")
	if opts.UseVault {
		parts = append(parts, "vault", "run")
		if strings.TrimSpace(opts.VaultFile) != "" {
			parts = append(parts, "--scope", shellQuote(strings.TrimSpace(opts.VaultFile)))
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

func buildGitHubRemoteURLWithPAT(rawCanonicalURL string, pat string) (string, error) {
	rawCanonicalURL = strings.TrimSpace(rawCanonicalURL)
	if rawCanonicalURL == "" {
		return "", fmt.Errorf("github remote url is required")
	}
	pat = strings.TrimSpace(pat)
	if pat == "" {
		return "", fmt.Errorf("github PAT is required")
	}
	u, err := url.Parse(rawCanonicalURL)
	if err != nil {
		return "", fmt.Errorf("parse github remote url: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(u.Scheme), "https") {
		return "", fmt.Errorf("github remote url must use https")
	}
	if normalizeGitHost(u.Host) == "" {
		return "", fmt.Errorf("github remote url host is required")
	}
	u.User = url.User(pat)
	return u.String(), nil
}

func redactGitRemotePATURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.User == nil {
		return raw
	}
	username := strings.TrimSpace(u.User.Username())
	if username == "" {
		return raw
	}
	u.User = url.User(maskCredentialValue(username))
	return u.String()
}

func maskCredentialValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "****"
	}
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "..." + value[len(value)-4:]
}

func ensureGitBranchTracking(repoPath string, remote string, dryRun bool) (string, error) {
	branch, err := gitCurrentBranch(repoPath)
	if err != nil {
		return "", err
	}
	if branch == "" {
		if dryRun {
			return "would-skip-detached", nil
		}
		return "detached", nil
	}
	if dryRun {
		return "would-set", nil
	}
	if err := gitSetBranchConfig(repoPath, branch, "remote", remote); err != nil {
		return "", err
	}
	if err := gitSetBranchConfig(repoPath, branch, "merge", "refs/heads/"+branch); err != nil {
		return "", err
	}
	return "set", nil
}

func gitCurrentBranch(repoPath string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse branch (%s): %w: %s", repoPath, err, strings.TrimSpace(string(out)))
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" || branch == "HEAD" {
		return "", nil
	}
	return branch, nil
}

func gitSetBranchConfig(repoPath string, branch string, key string, value string) error {
	name := fmt.Sprintf("branch.%s.%s", strings.TrimSpace(branch), strings.TrimSpace(key))
	cmd := exec.Command("git", "-C", repoPath, "config", name, value)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git config %s (%s): %w: %s", name, repoPath, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func countRemoteAuthChanged(items []githubGitRemoteAuthRepoChange) int {
	total := 0
	for _, item := range items {
		if item.Changed && strings.TrimSpace(item.Error) == "" {
			total++
		}
	}
	return total
}

func countRemoteAuthSkipped(items []githubGitRemoteAuthRepoChange) int {
	total := 0
	for _, item := range items {
		if strings.TrimSpace(item.Skipped) != "" {
			total++
		}
	}
	return total
}

func countRemoteAuthErrored(items []githubGitRemoteAuthRepoChange) int {
	total := 0
	for _, item := range items {
		if strings.TrimSpace(item.Error) != "" {
			total++
		}
	}
	return total
}
