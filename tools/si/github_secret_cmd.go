package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/nacl/box"

	"si/tools/si/internal/githubbridge"
)

func cmdGithubSecret(args []string) {
	if len(args) == 0 {
		printUsage("usage: si github secret <repo|env|org> ...")
		return
	}
	scope := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch scope {
	case "repo":
		cmdGithubRepoSecret(rest)
	case "env":
		cmdGithubEnvSecret(rest)
	case "org":
		cmdGithubOrgSecret(rest)
	default:
		printUnknown("github secret", scope)
	}
}

func cmdGithubRepoSecret(args []string) {
	if len(args) == 0 {
		printUsage("usage: si github secret repo <set|delete> ...")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "set":
		cmdGithubRepoSecretSet(args[1:])
	case "delete":
		cmdGithubRepoSecretDelete(args[1:])
	default:
		printUnknown("github secret repo", args[0])
	}
}

func cmdGithubRepoSecretSet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github secret repo set", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	value := fs.String("value", "", "plaintext secret value")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github secret repo set <owner/repo|repo> <name> --value <plaintext> [--json]")
		return
	}
	if strings.TrimSpace(*value) == "" {
		fatal(fmt.Errorf("--value is required"))
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	name := strings.TrimSpace(fs.Arg(1))
	if name == "" {
		fatal(fmt.Errorf("secret name is required"))
	}
	printGithubContextBanner(runtime, *jsonOut)
	if err := upsertGithubSecret(context.Background(), client, secretScopeRepo{Owner: repoOwner, Repo: repoName}, name, strings.TrimSpace(*value)); err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(githubbridge.Response{StatusCode: 201, Status: "201 Created", Data: map[string]any{"scope": "repo", "name": name, "owner": repoOwner, "repo": repoName}}, *jsonOut, *raw)
}

func cmdGithubRepoSecretDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("github secret repo delete", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github secret repo delete <owner/repo|repo> <name> [--force] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	name := strings.TrimSpace(fs.Arg(1))
	if err := requireGithubConfirmation("delete repo secret "+name+" from "+repoOwner+"/"+repoName, *force); err != nil {
		fatal(err)
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "DELETE", Path: path.Join("/repos", repoOwner, repoName, "actions", "secrets", name), Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubEnvSecret(args []string) {
	if len(args) == 0 {
		printUsage("usage: si github secret env <set|delete> ...")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "set":
		cmdGithubEnvSecretSet(args[1:])
	case "delete":
		cmdGithubEnvSecretDelete(args[1:])
	default:
		printUnknown("github secret env", args[0])
	}
}

func cmdGithubEnvSecretSet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github secret env set", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	value := fs.String("value", "", "plaintext secret value")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 3 {
		printUsage("usage: si github secret env set <owner/repo|repo> <environment> <name> --value <plaintext> [--json]")
		return
	}
	if strings.TrimSpace(*value) == "" {
		fatal(fmt.Errorf("--value is required"))
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	envName := strings.TrimSpace(fs.Arg(1))
	name := strings.TrimSpace(fs.Arg(2))
	printGithubContextBanner(runtime, *jsonOut)
	if err := upsertGithubSecret(context.Background(), client, secretScopeEnv{Owner: repoOwner, Repo: repoName, Env: envName}, name, strings.TrimSpace(*value)); err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(githubbridge.Response{StatusCode: 201, Status: "201 Created", Data: map[string]any{"scope": "env", "environment": envName, "name": name, "owner": repoOwner, "repo": repoName}}, *jsonOut, *raw)
}

func cmdGithubEnvSecretDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("github secret env delete", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "default owner/org")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 3 {
		printUsage("usage: si github secret env delete <owner/repo|repo> <environment> <name> [--force] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	repoOwner, repoName, err := parseGitHubOwnerRepo(fs.Arg(0), runtime.Owner)
	if err != nil {
		fatal(err)
	}
	envName := strings.TrimSpace(fs.Arg(1))
	name := strings.TrimSpace(fs.Arg(2))
	if err := requireGithubConfirmation("delete environment secret "+name+" from "+repoOwner+"/"+repoName+" env "+envName, *force); err != nil {
		fatal(err)
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "DELETE", Path: path.Join("/repos", repoOwner, repoName, "environments", envName, "secrets", name), Owner: repoOwner, Repo: repoName})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

func cmdGithubOrgSecret(args []string) {
	if len(args) == 0 {
		printUsage("usage: si github secret org <set|delete> ...")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "set":
		cmdGithubOrgSecretSet(args[1:])
	case "delete":
		cmdGithubOrgSecretDelete(args[1:])
	default:
		printUnknown("github secret org", args[0])
	}
}

func cmdGithubOrgSecretSet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("github secret org set", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "org owner")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	value := fs.String("value", "", "plaintext secret value")
	visibility := fs.String("visibility", "private", "all|private|selected")
	repos := fs.String("repos", "", "comma-separated repository ids for selected visibility")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github secret org set <org> <name> --value <plaintext> [--visibility all|private|selected] [--repos id,id] [--json]")
		return
	}
	if strings.TrimSpace(*value) == "" {
		fatal(fmt.Errorf("--value is required"))
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	org := strings.TrimSpace(fs.Arg(0))
	if org == "" {
		org = strings.TrimSpace(runtime.Owner)
	}
	if org == "" {
		fatal(fmt.Errorf("org is required"))
	}
	name := strings.TrimSpace(fs.Arg(1))
	if name == "" {
		fatal(fmt.Errorf("secret name is required"))
	}
	printGithubContextBanner(runtime, *jsonOut)
	if err := upsertGithubSecret(context.Background(), client, secretScopeOrg{Org: org, Visibility: strings.TrimSpace(*visibility), RepoIDs: parseGitHubCSVInts(*repos)}, name, strings.TrimSpace(*value)); err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(githubbridge.Response{StatusCode: 201, Status: "201 Created", Data: map[string]any{"scope": "org", "org": org, "name": name}}, *jsonOut, *raw)
}

func cmdGithubOrgSecretDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "force": true})
	fs := flag.NewFlagSet("github secret org delete", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	owner := fs.String("owner", "", "org owner")
	baseURL := fs.String("base-url", "", "github api base url")
	authMode := fs.String("auth-mode", "", "auth mode (app|oauth)")
	token := fs.String("token", "", "override oauth access token")
	appID := fs.Int64("app-id", 0, "override app id")
	appKey := fs.String("app-key", "", "override app private key pem")
	installationID := fs.Int64("installation-id", 0, "override installation id")
	force := fs.Bool("force", false, "skip confirmation prompt")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		printUsage("usage: si github secret org delete <org> <name> [--force] [--json]")
		return
	}
	runtime, client := mustGithubClient(*account, *owner, *baseURL, githubAuthOverrides{AuthMode: *authMode, AccessToken: *token, AppID: *appID, AppKey: *appKey, InstallationID: *installationID})
	org := strings.TrimSpace(fs.Arg(0))
	if org == "" {
		org = strings.TrimSpace(runtime.Owner)
	}
	name := strings.TrimSpace(fs.Arg(1))
	if err := requireGithubConfirmation("delete org secret "+name+" from "+org, *force); err != nil {
		fatal(err)
	}
	printGithubContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, githubbridge.Request{Method: "DELETE", Path: path.Join("/orgs", org, "actions", "secrets", name), Owner: org})
	if err != nil {
		printGithubError(err)
		return
	}
	printGithubResponse(resp, *jsonOut, *raw)
}

type githubSecretKey struct {
	Key   string `json:"key"`
	KeyID string `json:"key_id"`
}

type secretScope interface {
	publicKeyPath() string
	upsertPath(name string) string
	mutatePayloadBase(keyID string) map[string]any
	requestOwnerRepo() (string, string)
}

type secretScopeRepo struct {
	Owner string
	Repo  string
}

func (s secretScopeRepo) publicKeyPath() string {
	return path.Join("/repos", s.Owner, s.Repo, "actions", "secrets", "public-key")
}
func (s secretScopeRepo) upsertPath(name string) string {
	return path.Join("/repos", s.Owner, s.Repo, "actions", "secrets", name)
}
func (s secretScopeRepo) mutatePayloadBase(keyID string) map[string]any {
	return map[string]any{"key_id": keyID}
}
func (s secretScopeRepo) requestOwnerRepo() (string, string) { return s.Owner, s.Repo }

type secretScopeEnv struct {
	Owner string
	Repo  string
	Env   string
}

func (s secretScopeEnv) publicKeyPath() string {
	return path.Join("/repos", s.Owner, s.Repo, "environments", s.Env, "secrets", "public-key")
}
func (s secretScopeEnv) upsertPath(name string) string {
	return path.Join("/repos", s.Owner, s.Repo, "environments", s.Env, "secrets", name)
}
func (s secretScopeEnv) mutatePayloadBase(keyID string) map[string]any {
	return map[string]any{"key_id": keyID}
}
func (s secretScopeEnv) requestOwnerRepo() (string, string) { return s.Owner, s.Repo }

type secretScopeOrg struct {
	Org        string
	Visibility string
	RepoIDs    []int64
}

func (s secretScopeOrg) publicKeyPath() string {
	return path.Join("/orgs", s.Org, "actions", "secrets", "public-key")
}
func (s secretScopeOrg) upsertPath(name string) string {
	return path.Join("/orgs", s.Org, "actions", "secrets", name)
}
func (s secretScopeOrg) mutatePayloadBase(keyID string) map[string]any {
	out := map[string]any{"key_id": keyID}
	v := strings.ToLower(strings.TrimSpace(s.Visibility))
	if v == "" {
		v = "private"
	}
	switch v {
	case "all", "private", "selected":
		out["visibility"] = v
	default:
		out["visibility"] = "private"
	}
	if out["visibility"] == "selected" && len(s.RepoIDs) > 0 {
		out["selected_repository_ids"] = s.RepoIDs
	}
	return out
}
func (s secretScopeOrg) requestOwnerRepo() (string, string) { return s.Org, "" }

func upsertGithubSecret(ctx context.Context, client githubBridgeClient, scope secretScope, name string, value string) error {
	owner, repo := scope.requestOwnerRepo()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	keyResp, err := client.Do(ctx, githubbridge.Request{Method: "GET", Path: scope.publicKeyPath(), Owner: owner, Repo: repo})
	if err != nil {
		return err
	}
	if keyResp.Data == nil {
		return fmt.Errorf("github secret public key response missing data")
	}
	key := githubSecretKey{}
	if err := decodeGitHubMap(keyResp.Data, &key); err != nil {
		return err
	}
	if strings.TrimSpace(key.Key) == "" || strings.TrimSpace(key.KeyID) == "" {
		return fmt.Errorf("github secret public key response missing key/key_id")
	}
	encrypted, err := encryptGitHubSecretValue(key.Key, value)
	if err != nil {
		return err
	}
	payload := scope.mutatePayloadBase(strings.TrimSpace(key.KeyID))
	payload["encrypted_value"] = encrypted
	_, err = client.Do(ctx, githubbridge.Request{Method: "PUT", Path: scope.upsertPath(name), JSONBody: payload, Owner: owner, Repo: repo})
	return err
}

func encryptGitHubSecretValue(base64PublicKey string, plaintext string) (string, error) {
	pubBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(base64PublicKey))
	if err != nil {
		return "", fmt.Errorf("decode github public key: %w", err)
	}
	if len(pubBytes) != 32 {
		return "", fmt.Errorf("invalid github public key length: %d", len(pubBytes))
	}
	var recipientPub [32]byte
	copy(recipientPub[:], pubBytes)
	ephemeralPub, ephemeralPriv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate ephemeral key: %w", err)
	}
	nonceBytes := blake2b.Sum256(append(ephemeralPub[:], recipientPub[:]...))
	var nonce [24]byte
	copy(nonce[:], nonceBytes[:24])
	sealed := box.Seal(nil, []byte(plaintext), &nonce, &recipientPub, ephemeralPriv)
	out := make([]byte, 0, len(ephemeralPub)+len(sealed))
	out = append(out, ephemeralPub[:]...)
	out = append(out, sealed...)
	return base64.StdEncoding.EncodeToString(out), nil
}

func parseGitHubCSVInts(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int64, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		value, err := strconv.ParseInt(part, 10, 64)
		if err != nil || value <= 0 {
			continue
		}
		out = append(out, value)
	}
	return out
}

func decodeGitHubMap(src map[string]any, dst any) error {
	raw, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}
