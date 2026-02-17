package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	paasDeployWebhookUsageText    = "usage: si paas deploy webhook <ingest|map> [args...]"
	paasDeployWebhookMapUsageText = "usage: si paas deploy webhook map <add|list|remove> [args...]"
)

type paasWebhookMapping struct {
	Provider            string `json:"provider"`
	Repo                string `json:"repo"`
	Branch              string `json:"branch"`
	App                 string `json:"app"`
	ComposeFile         string `json:"compose_file,omitempty"`
	Targets             string `json:"targets,omitempty"`
	Strategy            string `json:"strategy,omitempty"`
	MaxParallel         int    `json:"max_parallel,omitempty"`
	ContinueOnError     bool   `json:"continue_on_error,omitempty"`
	Apply               bool   `json:"apply,omitempty"`
	RemoteDir           string `json:"remote_dir,omitempty"`
	AllowUntrustedVault bool   `json:"allow_untrusted_vault,omitempty"`
	CreatedAt           string `json:"created_at,omitempty"`
	UpdatedAt           string `json:"updated_at,omitempty"`
}

type paasWebhookMappingStore struct {
	Mappings []paasWebhookMapping `json:"mappings"`
}

type paasGitHubPushPayload struct {
	Ref        string `json:"ref"`
	Repository struct {
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
		CloneURL string `json:"clone_url"`
	} `json:"repository"`
}

func cmdPaasDeployWebhook(args []string) {
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(args, isInteractiveTerminal(), func() (string, bool) {
		return selectSubcommandAction("Deploy webhook commands:", []subcommandAction{
			{Name: "ingest", Description: "ingest and validate git webhook payloads"},
			{Name: "map", Description: "manage webhook trigger mappings"},
		})
	})
	if showUsage {
		printUsage(paasDeployWebhookUsageText)
		return
	}
	if !ok {
		return
	}
	if len(resolved) == 0 {
		printUsage(paasDeployWebhookUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(resolved[0]))
	switch sub {
	case "help", "-h", "--help":
		printUsage(paasDeployWebhookUsageText)
	case "ingest":
		cmdPaasDeployWebhookIngest(resolved[1:])
	case "map":
		cmdPaasDeployWebhookMap(resolved[1:])
	default:
		if strings.HasPrefix(sub, "-") {
			cmdPaasDeployWebhookIngest(resolved)
			return
		}
		printUnknown("paas deploy webhook", sub)
		printUsage(paasDeployWebhookUsageText)
	}
}

func cmdPaasDeployWebhookMap(args []string) {
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(args, isInteractiveTerminal(), func() (string, bool) {
		return selectSubcommandAction("Deploy webhook mapping commands:", []subcommandAction{
			{Name: "add", Description: "add or update app/branch trigger mapping"},
			{Name: "list", Description: "list webhook trigger mappings"},
			{Name: "remove", Description: "remove a webhook trigger mapping"},
		})
	})
	if showUsage {
		printUsage(paasDeployWebhookMapUsageText)
		return
	}
	if !ok {
		return
	}
	if len(resolved) == 0 {
		printUsage(paasDeployWebhookMapUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(resolved[0]))
	switch sub {
	case "help", "-h", "--help":
		printUsage(paasDeployWebhookMapUsageText)
	case "add":
		cmdPaasDeployWebhookMapAdd(resolved[1:])
	case "list":
		cmdPaasDeployWebhookMapList(resolved[1:])
	case "remove", "rm", "delete":
		cmdPaasDeployWebhookMapRemove(resolved[1:])
	default:
		printUnknown("paas deploy webhook map", sub)
		printUsage(paasDeployWebhookMapUsageText)
	}
}

func cmdPaasDeployWebhookMapAdd(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas deploy webhook map add", flag.ExitOnError)
	provider := fs.String("provider", "github", "webhook provider")
	repo := fs.String("repo", "", "repository (owner/repo or URL)")
	branch := fs.String("branch", "", "branch name")
	app := fs.String("app", "", "mapped app slug")
	composeFile := fs.String("compose-file", "compose.yaml", "compose file for mapped deploy")
	targets := fs.String("targets", "", "mapped target ids csv or all")
	strategy := fs.String("strategy", "serial", "mapped deploy strategy (serial|rolling|canary|parallel)")
	maxParallel := fs.Int("max-parallel", 1, "mapped deploy max parallel operations")
	continueOnError := fs.Bool("continue-on-error", false, "mapped deploy continue on target error")
	applyRemote := fs.Bool("apply", false, "mapped deploy applies remotely")
	remoteDir := fs.String("remote-dir", "/opt/si/paas/releases", "mapped deploy remote release root")
	allowUntrustedVault := fs.Bool("allow-untrusted-vault", false, "mapped deploy allows untrusted vault fingerprint (unsafe)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas deploy webhook map add --repo <owner/repo|url> --branch <name> --app <slug> [--provider github] [--compose-file <path>] [--targets <id1,id2|all>] [--strategy <serial|rolling|canary|parallel>] [--max-parallel <n>] [--continue-on-error] [--apply] [--remote-dir <path>] [--allow-untrusted-vault] [--json]")
		return
	}
	normalizedProvider := normalizePaasWebhookProvider(*provider)
	if normalizedProvider == "" {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass a supported webhook provider",
			fmt.Errorf("invalid --provider %q", strings.TrimSpace(*provider)),
		), nil)
	}
	normalizedRepo := normalizePaasWebhookRepo(*repo)
	normalizedBranch := normalizePaasWebhookBranch(*branch)
	if normalizedRepo == "" || normalizedBranch == "" || strings.TrimSpace(*app) == "" {
		printUsage("usage: si paas deploy webhook map add --repo <owner/repo|url> --branch <name> --app <slug> [--provider github] [--compose-file <path>] [--targets <id1,id2|all>] [--strategy <serial|rolling|canary|parallel>] [--max-parallel <n>] [--continue-on-error] [--apply] [--remote-dir <path>] [--allow-untrusted-vault] [--json]")
		return
	}
	normalizedStrategy := strings.ToLower(strings.TrimSpace(*strategy))
	if !isValidDeployStrategy(normalizedStrategy) {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass --strategy serial, rolling, canary, or parallel",
			fmt.Errorf("invalid --strategy %q", strings.TrimSpace(*strategy)),
		), nil)
	}
	if *maxParallel < 1 {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass a positive value for --max-parallel",
			fmt.Errorf("invalid --max-parallel %d", *maxParallel),
		), nil)
	}
	store, err := loadPaasWebhookMappingStore(currentPaasContext())
	if err != nil {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureUnknown,
			"mapping_load",
			"",
			"verify context state permissions and retry",
			err,
		), nil)
	}
	now := utcNowRFC3339()
	row := paasWebhookMapping{
		Provider:            normalizedProvider,
		Repo:                normalizedRepo,
		Branch:              normalizedBranch,
		App:                 strings.TrimSpace(*app),
		ComposeFile:         strings.TrimSpace(*composeFile),
		Targets:             strings.TrimSpace(*targets),
		Strategy:            normalizedStrategy,
		MaxParallel:         *maxParallel,
		ContinueOnError:     *continueOnError,
		Apply:               *applyRemote,
		RemoteDir:           strings.TrimSpace(*remoteDir),
		AllowUntrustedVault: *allowUntrustedVault,
		UpdatedAt:           now,
	}
	idx := findPaasWebhookMapping(store, normalizedProvider, normalizedRepo, normalizedBranch)
	if idx >= 0 {
		row.CreatedAt = store.Mappings[idx].CreatedAt
		if strings.TrimSpace(row.CreatedAt) == "" {
			row.CreatedAt = now
		}
		store.Mappings[idx] = row
	} else {
		row.CreatedAt = now
		store.Mappings = append(store.Mappings, row)
	}
	if err := savePaasWebhookMappingStore(currentPaasContext(), store); err != nil {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureUnknown,
			"mapping_save",
			"",
			"verify context state permissions and retry",
			err,
		), nil)
	}
	printPaasScaffold("deploy webhook map add", map[string]string{
		"provider":              row.Provider,
		"repo":                  row.Repo,
		"branch":                row.Branch,
		"app":                   row.App,
		"compose_file":          row.ComposeFile,
		"targets":               row.Targets,
		"strategy":              row.Strategy,
		"max_parallel":          intString(row.MaxParallel),
		"continue_on_error":     boolString(row.ContinueOnError),
		"apply":                 boolString(row.Apply),
		"remote_dir":            row.RemoteDir,
		"allow_untrusted_vault": boolString(row.AllowUntrustedVault),
	}, jsonOut)
}

func cmdPaasDeployWebhookMapList(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas deploy webhook map list", flag.ExitOnError)
	provider := fs.String("provider", "", "optional provider filter")
	app := fs.String("app", "", "optional app filter")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas deploy webhook map list [--provider <name>] [--app <slug>] [--json]")
		return
	}
	store, err := loadPaasWebhookMappingStore(currentPaasContext())
	if err != nil {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureUnknown,
			"mapping_load",
			"",
			"verify context state permissions and retry",
			err,
		), nil)
	}
	data := make([]paasWebhookMapping, 0, len(store.Mappings))
	filterProvider := normalizePaasWebhookProvider(*provider)
	filterApp := strings.TrimSpace(*app)
	for _, row := range store.Mappings {
		if filterProvider != "" && !strings.EqualFold(row.Provider, filterProvider) {
			continue
		}
		if filterApp != "" && !strings.EqualFold(row.App, filterApp) {
			continue
		}
		data = append(data, row)
	}
	if jsonOut {
		payload := map[string]any{
			"ok":      true,
			"command": "deploy webhook map list",
			"context": currentPaasContext(),
			"mode":    "live",
			"count":   len(data),
			"data":    data,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fields := map[string]string{
		"count": intString(len(data)),
	}
	printPaasScaffold("deploy webhook map list", fields, false)
	for _, row := range data {
		fmt.Printf("  - provider=%s repo=%s branch=%s app=%s strategy=%s targets=%s\n",
			row.Provider, row.Repo, row.Branch, row.App, row.Strategy, row.Targets)
	}
}

func cmdPaasDeployWebhookMapRemove(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas deploy webhook map remove", flag.ExitOnError)
	provider := fs.String("provider", "github", "webhook provider")
	repo := fs.String("repo", "", "repository (owner/repo or URL)")
	branch := fs.String("branch", "", "branch name")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas deploy webhook map remove --repo <owner/repo|url> --branch <name> [--provider github] [--json]")
		return
	}
	normalizedProvider := normalizePaasWebhookProvider(*provider)
	normalizedRepo := normalizePaasWebhookRepo(*repo)
	normalizedBranch := normalizePaasWebhookBranch(*branch)
	if normalizedProvider == "" || normalizedRepo == "" || normalizedBranch == "" {
		printUsage("usage: si paas deploy webhook map remove --repo <owner/repo|url> --branch <name> [--provider github] [--json]")
		return
	}
	store, err := loadPaasWebhookMappingStore(currentPaasContext())
	if err != nil {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureUnknown,
			"mapping_load",
			"",
			"verify context state permissions and retry",
			err,
		), nil)
	}
	idx := findPaasWebhookMapping(store, normalizedProvider, normalizedRepo, normalizedBranch)
	if idx < 0 {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureWebhookMapping,
			"mapping_resolve",
			"",
			"create a mapping first with `si paas deploy webhook map add`",
			fmt.Errorf("no mapping found for %s %s %s", normalizedProvider, normalizedRepo, normalizedBranch),
		), nil)
	}
	store.Mappings = append(store.Mappings[:idx], store.Mappings[idx+1:]...)
	if err := savePaasWebhookMappingStore(currentPaasContext(), store); err != nil {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureUnknown,
			"mapping_save",
			"",
			"verify context state permissions and retry",
			err,
		), nil)
	}
	printPaasScaffold("deploy webhook map remove", map[string]string{
		"provider": normalizedProvider,
		"repo":     normalizedRepo,
		"branch":   normalizedBranch,
	}, jsonOut)
}

func cmdPaasDeployWebhookIngest(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas deploy webhook ingest", flag.ExitOnError)
	provider := fs.String("provider", "github", "webhook provider")
	event := fs.String("event", "push", "webhook event type")
	payloadFile := fs.String("payload-file", "", "payload file path ('-' for stdin)")
	signature := fs.String("signature", "", "webhook signature header value")
	secret := fs.String("secret", "", "webhook secret value")
	secretEnv := fs.String("secret-env", "", "environment variable containing webhook secret")
	allowUnsigned := fs.Bool("allow-unsigned", false, "allow unsigned payloads (unsafe)")
	dispatch := fs.Bool("dispatch", false, "dispatch mapped deploy command")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si paas deploy webhook ingest --payload-file <path|-> [--provider github] [--event push] [--signature <sha256=...>] [--secret <value>|--secret-env <NAME>] [--dispatch] [--allow-unsigned] [--json]")
		return
	}
	normalizedProvider := normalizePaasWebhookProvider(*provider)
	normalizedEvent := strings.ToLower(strings.TrimSpace(*event))
	if normalizedProvider == "" {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass a supported provider value",
			fmt.Errorf("invalid --provider %q", strings.TrimSpace(*provider)),
		), nil)
	}
	if normalizedEvent == "" {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureInvalidArgument,
			"flag_validation",
			"",
			"pass a webhook event name (for example push)",
			fmt.Errorf("missing --event"),
		), nil)
	}
	payload, err := readPaasWebhookPayload(strings.TrimSpace(*payloadFile))
	if err != nil {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureWebhookPayload,
			"payload_read",
			"",
			"pass a valid --payload-file path or '-' for stdin",
			err,
		), nil)
	}
	secretValue := strings.TrimSpace(*secret)
	if secretValue == "" && strings.TrimSpace(*secretEnv) != "" {
		secretValue = strings.TrimSpace(os.Getenv(strings.TrimSpace(*secretEnv)))
	}
	if !*allowUnsigned {
		if err := verifyPaasWebhookSignature(payload, secretValue, strings.TrimSpace(*signature)); err != nil {
			failPaasDeploy(jsonOut, newPaasOperationFailure(
				paasFailureWebhookAuth,
				"auth_validate",
				"",
				"set matching webhook --secret/--secret-env and pass a valid --signature",
				err,
			), nil)
		}
	}
	repo, branch, err := parsePaasWebhookPayload(normalizedProvider, normalizedEvent, payload)
	if err != nil {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureWebhookPayload,
			"payload_parse",
			"",
			"pass a valid provider event payload",
			err,
		), nil)
	}
	store, err := loadPaasWebhookMappingStore(currentPaasContext())
	if err != nil {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureUnknown,
			"mapping_load",
			"",
			"verify context state permissions and retry",
			err,
		), nil)
	}
	idx := findPaasWebhookMapping(store, normalizedProvider, repo, branch)
	if idx < 0 {
		failPaasDeploy(jsonOut, newPaasOperationFailure(
			paasFailureWebhookMapping,
			"mapping_resolve",
			"",
			"create a matching mapping with `si paas deploy webhook map add`",
			fmt.Errorf("no mapping found for provider=%s repo=%s branch=%s", normalizedProvider, repo, branch),
		), map[string]string{
			"provider": normalizedProvider,
			"repo":     repo,
			"branch":   branch,
			"event":    normalizedEvent,
		})
	}
	mapping := store.Mappings[idx]
	fields := map[string]string{
		"provider":      normalizedProvider,
		"event":         normalizedEvent,
		"repo":          repo,
		"branch":        branch,
		"auth_verified": boolString(!*allowUnsigned),
		"mapped_app":    mapping.App,
		"mapped_branch": mapping.Branch,
		"mapped_repo":   mapping.Repo,
		"dispatch":      boolString(*dispatch),
	}
	deployArgs := buildPaasDeployArgsFromWebhookMapping(mapping, jsonOut)
	fields["trigger_command"] = "si paas deploy " + strings.Join(deployArgs, " ")
	if *dispatch {
		if eventPath := recordPaasDeployEvent("deploy webhook", "accepted", fields, nil); strings.TrimSpace(eventPath) != "" {
			fields["event_log"] = eventPath
		}
		cmdPaasDeploy(deployArgs)
		return
	}
	if eventPath := recordPaasDeployEvent("deploy webhook", "accepted", fields, nil); strings.TrimSpace(eventPath) != "" {
		fields["event_log"] = eventPath
	}
	printPaasScaffold("deploy webhook ingest", fields, jsonOut)
}

func buildPaasDeployArgsFromWebhookMapping(mapping paasWebhookMapping, jsonOut bool) []string {
	args := []string{
		"--app", strings.TrimSpace(mapping.App),
		"--compose-file", strings.TrimSpace(mapping.ComposeFile),
		"--strategy", strings.TrimSpace(mapping.Strategy),
		"--max-parallel", intString(mapping.MaxParallel),
	}
	if strings.TrimSpace(mapping.Targets) != "" {
		args = append(args, "--targets", strings.TrimSpace(mapping.Targets))
	}
	if mapping.ContinueOnError {
		args = append(args, "--continue-on-error")
	}
	if mapping.Apply {
		args = append(args, "--apply", "--remote-dir", strings.TrimSpace(mapping.RemoteDir))
	}
	if mapping.AllowUntrustedVault {
		args = append(args, "--allow-untrusted-vault")
	}
	if jsonOut {
		args = append(args, "--json")
	}
	return args
}

func resolvePaasWebhookMappingStorePath(contextName string) (string, error) {
	contextDir, err := resolvePaasContextDir(contextName)
	if err != nil {
		return "", err
	}
	return filepath.Join(contextDir, "webhooks", "mappings.json"), nil
}

func loadPaasWebhookMappingStore(contextName string) (paasWebhookMappingStore, error) {
	path, err := resolvePaasWebhookMappingStorePath(contextName)
	if err != nil {
		return paasWebhookMappingStore{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return paasWebhookMappingStore{Mappings: []paasWebhookMapping{}}, nil
		}
		return paasWebhookMappingStore{}, err
	}
	var store paasWebhookMappingStore
	if err := json.Unmarshal(raw, &store); err != nil {
		return paasWebhookMappingStore{}, fmt.Errorf("invalid webhook mapping store: %w", err)
	}
	out := make([]paasWebhookMapping, 0, len(store.Mappings))
	for _, row := range store.Mappings {
		if normalized, ok := normalizePaasWebhookMappingRow(row); ok {
			out = append(out, normalized)
		}
	}
	store.Mappings = out
	return store, nil
}

func savePaasWebhookMappingStore(contextName string, store paasWebhookMappingStore) error {
	path, err := resolvePaasWebhookMappingStorePath(contextName)
	if err != nil {
		return err
	}
	rows := make([]paasWebhookMapping, 0, len(store.Mappings))
	for _, row := range store.Mappings {
		if normalized, ok := normalizePaasWebhookMappingRow(row); ok {
			rows = append(rows, normalized)
		}
	}
	store.Mappings = rows
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func normalizePaasWebhookMappingRow(row paasWebhookMapping) (paasWebhookMapping, bool) {
	row.Provider = normalizePaasWebhookProvider(row.Provider)
	row.Repo = normalizePaasWebhookRepo(row.Repo)
	row.Branch = normalizePaasWebhookBranch(row.Branch)
	row.App = strings.TrimSpace(row.App)
	row.ComposeFile = strings.TrimSpace(row.ComposeFile)
	row.Targets = strings.TrimSpace(row.Targets)
	row.Strategy = strings.ToLower(strings.TrimSpace(row.Strategy))
	if row.Strategy == "" {
		row.Strategy = "serial"
	}
	if !isValidDeployStrategy(row.Strategy) {
		row.Strategy = "serial"
	}
	if row.MaxParallel < 1 {
		row.MaxParallel = 1
	}
	row.RemoteDir = strings.TrimSpace(row.RemoteDir)
	if row.RemoteDir == "" {
		row.RemoteDir = "/opt/si/paas/releases"
	}
	row.CreatedAt = strings.TrimSpace(row.CreatedAt)
	row.UpdatedAt = strings.TrimSpace(row.UpdatedAt)
	return row, row.Provider != "" && row.Repo != "" && row.Branch != "" && row.App != ""
}

func findPaasWebhookMapping(store paasWebhookMappingStore, provider, repo, branch string) int {
	for i, row := range store.Mappings {
		if strings.EqualFold(strings.TrimSpace(row.Provider), strings.TrimSpace(provider)) &&
			strings.EqualFold(strings.TrimSpace(row.Repo), strings.TrimSpace(repo)) &&
			strings.EqualFold(strings.TrimSpace(row.Branch), strings.TrimSpace(branch)) {
			return i
		}
	}
	return -1
}

func normalizePaasWebhookProvider(value string) string {
	provider := strings.ToLower(strings.TrimSpace(value))
	switch provider {
	case "", "github":
		return "github"
	default:
		return ""
	}
}

func normalizePaasWebhookRepo(value string) string {
	repo := strings.ToLower(strings.TrimSpace(value))
	if repo == "" {
		return ""
	}
	repo = strings.TrimPrefix(repo, "https://")
	repo = strings.TrimPrefix(repo, "http://")
	repo = strings.TrimPrefix(repo, "ssh://")
	repo = strings.TrimPrefix(repo, "git@")
	repo = strings.TrimPrefix(repo, "github.com/")
	if idx := strings.Index(repo, "github.com/"); idx >= 0 {
		repo = repo[idx+len("github.com/"):]
	}
	repo = strings.TrimPrefix(repo, ":")
	repo = strings.TrimPrefix(repo, "/")
	repo = strings.TrimSuffix(repo, ".git")
	repo = strings.TrimSpace(repo)
	return repo
}

func normalizePaasWebhookBranch(value string) string {
	branch := strings.TrimSpace(value)
	branch = strings.TrimPrefix(branch, "refs/heads/")
	return strings.TrimSpace(branch)
}

func readPaasWebhookPayload(path string) ([]byte, error) {
	value := strings.TrimSpace(path)
	if value == "" {
		return nil, fmt.Errorf("missing --payload-file")
	}
	if value == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(value)
}

func verifyPaasWebhookSignature(payload []byte, secret, signature string) error {
	key := strings.TrimSpace(secret)
	signed := strings.TrimSpace(signature)
	if key == "" {
		return fmt.Errorf("missing webhook secret")
	}
	if signed == "" {
		return fmt.Errorf("missing webhook signature")
	}
	token := strings.TrimPrefix(strings.ToLower(signed), "sha256=")
	if token == "" {
		return fmt.Errorf("empty webhook signature")
	}
	provided, err := hex.DecodeString(token)
	if err != nil {
		return fmt.Errorf("invalid webhook signature encoding")
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write(payload)
	expected := mac.Sum(nil)
	if !hmac.Equal(provided, expected) {
		return fmt.Errorf("webhook signature mismatch")
	}
	return nil
}

func parsePaasWebhookPayload(provider, event string, payload []byte) (string, string, error) {
	switch provider {
	case "github":
		if event != "push" {
			return "", "", fmt.Errorf("unsupported github event %q", event)
		}
		var doc paasGitHubPushPayload
		if err := json.Unmarshal(payload, &doc); err != nil {
			return "", "", fmt.Errorf("decode github push payload: %w", err)
		}
		repo := normalizePaasWebhookRepo(doc.Repository.FullName)
		if repo == "" {
			repo = normalizePaasWebhookRepo(doc.Repository.CloneURL)
		}
		if repo == "" {
			repo = normalizePaasWebhookRepo(doc.Repository.HTMLURL)
		}
		branch := normalizePaasWebhookBranch(doc.Ref)
		if repo == "" || branch == "" {
			return "", "", fmt.Errorf("github payload missing repository or branch")
		}
		return repo, branch, nil
	default:
		return "", "", fmt.Errorf("unsupported provider %q", provider)
	}
}
