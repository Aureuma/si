package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	sunUsageText                    = "usage: si sun <auth|profile|token|audit|taskboard|machine|doctor> ..."
	sunAuthUsageText                = "usage: si sun auth <login|status|logout> ..."
	sunProfileUsageText             = "usage: si sun profile <list|push|pull> ..."
	sunTokenUsageText               = "usage: si sun token <list|create|revoke> ..."
	sunAuditUsageText               = "usage: si sun audit list ..."
	sunCodexProfileBundleKind       = "codex_profile_bundle"
	sunVaultIdentityKind            = "vault_identity"
	sunPaasControlPlaneSnapshotKind = "paas_control_plane_snapshot"
)

type sunCodexProfileBundle struct {
	ID       string          `json:"id"`
	Name     string          `json:"name,omitempty"`
	Email    string          `json:"email,omitempty"`
	AuthJSON json.RawMessage `json:"auth_json"`
	SyncedAt string          `json:"synced_at,omitempty"`
}

func cmdSun(args []string) {
	if len(args) == 0 {
		printUsage(sunUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(sunUsageText)
	case "auth":
		cmdSunAuth(rest)
	case "profile", "profiles":
		cmdSunProfile(rest)
	case "token", "tokens":
		cmdSunToken(rest)
	case "audit":
		cmdSunAudit(rest)
	case "taskboard":
		cmdSunTaskboard(rest)
	case "machine":
		cmdSunMachine(rest)
	case "doctor":
		cmdSunDoctor(rest)
	default:
		printUnknown("sun", sub)
		printUsage(sunUsageText)
		os.Exit(1)
	}
}

func cmdSunAuth(args []string) {
	if len(args) == 0 {
		printUsage(sunAuthUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(sunAuthUsageText)
	case "login":
		cmdSunAuthLogin(rest)
	case "status":
		cmdSunAuthStatus(rest)
	case "logout":
		cmdSunAuthLogout(rest)
	default:
		printUnknown("sun auth", sub)
		printUsage(sunAuthUsageText)
		os.Exit(1)
	}
}

func cmdSunAuthLogin(args []string) {
	settings := loadSettingsOrDefault()
	autoSyncProvided := flagProvided(args, "auto-sync")
	urlProvided := flagProvided(args, "url")
	accountProvided := flagProvided(args, "account")
	fs := flag.NewFlagSet("sun auth login", flag.ExitOnError)
	urlFlag := fs.String("url", strings.TrimSpace(settings.Sun.BaseURL), "sun base url")
	tokenFlag := fs.String("token", envSunToken(), "sun bearer token")
	googleLogin := fs.Bool("google", false, "authenticate via aureuma.ai Google OAuth")
	loginURLFlag := fs.String("login-url", firstNonEmpty(envSunLoginURL(), defaultSunGoogleLoginURL), "browser login start URL")
	openBrowser := fs.Bool("open-browser", true, "open browser automatically for --google")
	accountFlag := fs.String("account", strings.TrimSpace(settings.Sun.Account), "expected account slug")
	timeoutSeconds := fs.Int("timeout-seconds", settings.Sun.TimeoutSeconds, "http timeout seconds")
	autoSync := fs.Bool("auto-sync", settings.Sun.AutoSync, "enable automatic profile sync after login/swap")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun auth login [--url <url>] [--token <token>] [--google] [--login-url <url>] [--open-browser] [--account <slug>] [--timeout-seconds <n>] [--auto-sync]")
		return
	}

	token := strings.TrimSpace(*tokenFlag)
	if *googleLogin {
		result, err := runSunBrowserAuthFlow(strings.TrimSpace(*loginURLFlag), time.Duration(*timeoutSeconds)*time.Second, *openBrowser)
		if err != nil {
			fatal(err)
		}
		token = strings.TrimSpace(result.Token)
		if !autoSyncProvided && result.AutoSync {
			*autoSync = true
		}
		if strings.TrimSpace(result.BaseURL) != "" && (!urlProvided || strings.TrimSpace(*urlFlag) == "") {
			*urlFlag = strings.TrimSpace(result.BaseURL)
		}
		if strings.TrimSpace(result.Account) != "" && (!accountProvided || strings.TrimSpace(*accountFlag) == "") {
			*accountFlag = strings.TrimSpace(result.Account)
		}
	} else if token == "" {
		token = strings.TrimSpace(settings.Sun.Token)
	}
	client, err := newSunClient(strings.TrimSpace(*urlFlag), token, time.Duration(*timeoutSeconds)*time.Second)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSeconds)*time.Second)
	defer cancel()
	who, err := client.whoAmI(ctx)
	if err != nil {
		fatal(err)
	}
	expectedAccount := strings.TrimSpace(*accountFlag)
	if expectedAccount != "" && !strings.EqualFold(expectedAccount, who.AccountSlug) {
		fatal(fmt.Errorf("account mismatch: expected %q, got %q", expectedAccount, who.AccountSlug))
	}
	persisted, err := loadSettings()
	if err != nil {
		fatal(err)
	}
	persisted.Sun.BaseURL = strings.TrimSpace(*urlFlag)
	persisted.Sun.Token = token
	persisted.Sun.Account = who.AccountSlug
	persisted.Sun.TimeoutSeconds = *timeoutSeconds
	persisted.Sun.AutoSync = *autoSync
	// Sun-authenticated machines should default to Sun-backed vault sync.
	persisted.Vault.SyncBackend = vaultSyncBackendSun
	if err := saveSettings(persisted); err != nil {
		fatal(err)
	}
	successf("sun auth configured for account %s at %s", who.AccountSlug, strings.TrimSpace(*urlFlag))
}

func cmdSunAuthStatus(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun auth status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "json output")
	timeoutSeconds := fs.Int("timeout-seconds", settings.Sun.TimeoutSeconds, "http timeout seconds")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun auth status [--json] [--timeout-seconds <n>]")
		return
	}
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	client.http.Timeout = time.Duration(*timeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSeconds)*time.Second)
	defer cancel()
	who, err := client.whoAmI(ctx)
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSON(map[string]interface{}{
			"base_url": client.baseURL,
			"whoami":   who,
		})
		return
	}
	fmt.Printf("%s %s\n", styleHeading("base_url:"), client.baseURL)
	fmt.Printf("%s %s\n", styleHeading("account:"), who.AccountSlug)
	fmt.Printf("%s %s\n", styleHeading("token_id:"), who.TokenID)
	fmt.Printf("%s %s\n", styleHeading("scopes:"), strings.Join(who.Scopes, ","))
	fmt.Printf("%s %s\n", styleHeading("auto_sync:"), boolString(settings.Sun.AutoSync))
}

func cmdSunAuthLogout(args []string) {
	fs := flag.NewFlagSet("sun auth logout", flag.ExitOnError)
	clearAccount := fs.Bool("clear-account", false, "also clear stored sun account")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun auth logout [--clear-account]")
		return
	}
	settings, err := loadSettings()
	if err != nil {
		fatal(err)
	}
	settings.Sun.Token = ""
	if *clearAccount {
		settings.Sun.Account = ""
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	successf("sun auth token cleared")
}

func cmdSunProfile(args []string) {
	if len(args) == 0 {
		printUsage(sunProfileUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(sunProfileUsageText)
	case "list":
		cmdSunProfileList(rest)
	case "push":
		cmdSunProfilePush(rest)
	case "pull":
		cmdSunProfilePull(rest)
	default:
		printUnknown("sun profile", sub)
		printUsage(sunProfileUsageText)
		os.Exit(1)
	}
}

func cmdSunProfileList(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun profile list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun profile list [--json]")
		return
	}
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	items, err := client.listObjects(context.Background(), sunCodexProfileBundleKind, "", 200)
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSON(items)
		return
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{item.Name, fmt.Sprintf("%d", item.LatestRevision), item.UpdatedAt, fmt.Sprintf("%d", item.SizeBytes)})
	}
	printAlignedTable([]string{styleHeading("PROFILE"), styleHeading("REV"), styleHeading("UPDATED"), styleHeading("BYTES")}, rows, 2)
}

func cmdSunProfilePush(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun profile push", flag.ExitOnError)
	profileKey := fs.String("profile", "", "profile id or email")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun profile push [--profile <id>] [--json]")
		return
	}
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	profiles, err := resolveTargetProfiles(strings.TrimSpace(*profileKey))
	if err != nil {
		fatal(err)
	}
	results := make([]map[string]interface{}, 0, len(profiles))
	for _, profile := range profiles {
		authPath, err := codexProfileAuthPath(profile)
		if err != nil {
			fatal(err)
		}
		if err := codexAuthValidationError(authPath); err != nil {
			fatal(fmt.Errorf("profile %s auth cache invalid: %w", profile.ID, err))
		}
		// #nosec G304 -- authPath resolves from local profile location.
		authBytes, err := os.ReadFile(authPath)
		if err != nil {
			fatal(err)
		}
		bundle := sunCodexProfileBundle{
			ID:       profile.ID,
			Name:     profile.Name,
			Email:    profile.Email,
			AuthJSON: authBytes,
			SyncedAt: time.Now().UTC().Format(time.RFC3339),
		}
		payload, err := json.Marshal(bundle)
		if err != nil {
			fatal(err)
		}
		put, err := client.putObject(context.Background(), sunCodexProfileBundleKind, profile.ID, payload, "application/json", map[string]interface{}{
			"profile_id": profile.ID,
			"name":       profile.Name,
			"email":      profile.Email,
		}, nil)
		if err != nil {
			fatal(err)
		}
		results = append(results, map[string]interface{}{
			"profile":  profile.ID,
			"revision": put.Result.Revision.Revision,
		})
	}
	if *jsonOut {
		printJSON(results)
		return
	}
	for _, item := range results {
		successf("pushed profile %s (revision %v)", item["profile"], item["revision"])
	}
}

func cmdSunProfilePull(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun profile pull", flag.ExitOnError)
	profileKey := fs.String("profile", "", "profile id or email")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun profile pull [--profile <id>] [--json]")
		return
	}
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	targets := make([]string, 0)
	if key := strings.TrimSpace(*profileKey); key != "" {
		profile, err := requireCodexProfile(key)
		if err == nil {
			targets = append(targets, profile.ID)
		} else {
			targets = append(targets, key)
		}
	} else {
		items, err := client.listObjects(context.Background(), sunCodexProfileBundleKind, "", 200)
		if err != nil {
			fatal(err)
		}
		for _, item := range items {
			targets = append(targets, item.Name)
		}
	}
	if len(targets) == 0 {
		infof("no cloud profiles found")
		return
	}
	pulledProfiles := make([]codexProfile, 0, len(targets))
	results := make([]map[string]interface{}, 0, len(targets))
	for _, profileID := range targets {
		payload, err := client.getPayload(context.Background(), sunCodexProfileBundleKind, profileID)
		if err != nil {
			fatal(err)
		}
		var bundle sunCodexProfileBundle
		if err := json.Unmarshal(payload, &bundle); err != nil {
			fatal(fmt.Errorf("profile %s payload invalid: %w", profileID, err))
		}
		if strings.TrimSpace(bundle.ID) == "" {
			bundle.ID = profileID
		}
		profile := codexProfile{ID: strings.TrimSpace(bundle.ID), Name: strings.TrimSpace(bundle.Name), Email: strings.TrimSpace(bundle.Email)}
		if profile.ID == "" {
			fatal(fmt.Errorf("profile payload missing id"))
		}
		dir, err := ensureCodexProfileDir(profile)
		if err != nil {
			fatal(err)
		}
		authPath := filepath.Join(dir, "auth.json")
		if err := writeFileAtomic0600(authPath, bundle.AuthJSON); err != nil {
			fatal(err)
		}
		if err := codexAuthValidationError(authPath); err != nil {
			fatal(fmt.Errorf("downloaded auth cache invalid for %s: %w", profile.ID, err))
		}
		pulledProfiles = append(pulledProfiles, profile)
		results = append(results, map[string]interface{}{"profile": profile.ID, "auth_path": authPath})
	}
	if err := upsertCodexProfilesInSettings(pulledProfiles); err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSON(results)
		return
	}
	for _, item := range results {
		successf("pulled profile %s -> %s", item["profile"], item["auth_path"])
	}
}

func cmdSunToken(args []string) {
	if len(args) == 0 {
		printUsage(sunTokenUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdSunTokenList(rest)
	case "create", "issue":
		cmdSunTokenCreate(rest)
	case "revoke":
		cmdSunTokenRevoke(rest)
	case "help", "-h", "--help":
		printUsage(sunTokenUsageText)
	default:
		printUnknown("sun token", sub)
		printUsage(sunTokenUsageText)
		os.Exit(1)
	}
}

func cmdSunTokenList(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun token list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "json output")
	includeRevoked := fs.Bool("include-revoked", false, "include revoked tokens")
	limit := fs.Int("limit", 100, "max tokens")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun token list [--include-revoked] [--limit <n>] [--json]")
		return
	}
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	items, err := client.listTokens(sunContext(settings), *includeRevoked, *limit)
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSON(items)
		return
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		revoked := "-"
		if strings.TrimSpace(item.RevokedAt) != "" {
			revoked = item.RevokedAt
		}
		expires := "-"
		if strings.TrimSpace(item.ExpiresAt) != "" {
			expires = item.ExpiresAt
		}
		rows = append(rows, []string{item.TokenID, item.Label, strings.Join(item.Scopes, ","), expires, revoked, item.LastUsedAt})
	}
	printAlignedTable([]string{styleHeading("TOKEN_ID"), styleHeading("LABEL"), styleHeading("SCOPES"), styleHeading("EXPIRES"), styleHeading("REVOKED"), styleHeading("LAST_USED")}, rows, 2)
}

func cmdSunTokenCreate(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun token create", flag.ExitOnError)
	label := fs.String("label", "si", "token label")
	scopesCSV := fs.String("scopes", "objects:read,objects:write", "comma-separated scopes")
	expiresHours := fs.Int("expires-hours", 0, "optional expiry in hours")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun token create [--label <label>] [--scopes <csv>] [--expires-hours <n>] [--json]")
		return
	}
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	issued, err := client.createToken(sunContext(settings), strings.TrimSpace(*label), splitCSVScopes(*scopesCSV), *expiresHours)
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSON(issued)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("token_id:"), issued.TokenID)
	fmt.Printf("%s %s\n", styleHeading("token:"), issued.Token)
	fmt.Printf("%s %s\n", styleHeading("scopes:"), strings.Join(issued.Scopes, ","))
	if issued.ExpiresAt != "" {
		fmt.Printf("%s %s\n", styleHeading("expires_at:"), issued.ExpiresAt)
	}
	fmt.Println("store this token securely; it is only shown once")
}

func cmdSunTokenRevoke(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun token revoke", flag.ExitOnError)
	tokenID := fs.String("token-id", "", "token id to revoke")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun token revoke --token-id <id>")
		return
	}
	if strings.TrimSpace(*tokenID) == "" {
		fatal(fmt.Errorf("--token-id is required"))
	}
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	if err := client.revokeToken(sunContext(settings), strings.TrimSpace(*tokenID)); err != nil {
		fatal(err)
	}
	successf("revoked token %s", strings.TrimSpace(*tokenID))
}

func cmdSunAudit(args []string) {
	if len(args) == 0 {
		printUsage(sunAuditUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdSunAuditList(rest)
	case "help", "-h", "--help":
		printUsage(sunAuditUsageText)
	default:
		printUnknown("sun audit", sub)
		printUsage(sunAuditUsageText)
		os.Exit(1)
	}
}

func cmdSunAuditList(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun audit list", flag.ExitOnError)
	action := fs.String("action", "", "filter action")
	kind := fs.String("kind", "", "filter kind")
	name := fs.String("name", "", "filter name")
	limit := fs.Int("limit", 200, "max rows")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun audit list [--action <action>] [--kind <kind>] [--name <name>] [--limit <n>] [--json]")
		return
	}
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	items, err := client.listAuditEvents(sunContext(settings), strings.TrimSpace(*action), strings.TrimSpace(*kind), strings.TrimSpace(*name), *limit)
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		printJSON(items)
		return
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{fmt.Sprintf("%d", item.ID), item.CreatedAt, item.Action, item.Kind, item.Name, item.TokenID})
	}
	printAlignedTable([]string{styleHeading("ID"), styleHeading("AT"), styleHeading("ACTION"), styleHeading("KIND"), styleHeading("NAME"), styleHeading("TOKEN_ID")}, rows, 2)
}

func cmdSunDoctor(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun doctor", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun doctor [--json]")
		return
	}
	client, err := sunClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	ctx := sunContext(settings)
	readinessErr := client.ready(ctx)
	who, whoErr := client.whoAmI(ctx)
	report := map[string]interface{}{
		"base_url":     client.baseURL,
		"readiness_ok": readinessErr == nil,
		"whoami_ok":    whoErr == nil,
		"whoami":       who,
	}
	if readinessErr != nil {
		report["readiness_error"] = readinessErr.Error()
	}
	if whoErr != nil {
		report["whoami_error"] = whoErr.Error()
	}
	if *jsonOut {
		printJSON(report)
		if readinessErr != nil || whoErr != nil {
			os.Exit(1)
		}
		return
	}
	if readinessErr == nil {
		successf("sun readiness: ok")
	} else {
		warnf("sun readiness failed: %v", readinessErr)
	}
	if whoErr == nil {
		successf("sun auth: ok (%s)", who.AccountSlug)
	} else {
		warnf("sun auth failed: %v", whoErr)
	}
	if readinessErr != nil || whoErr != nil {
		os.Exit(1)
	}
}

func sunClientFromSettings(settings Settings) (*sunClient, error) {
	baseURL := firstNonEmpty(envSunBaseURL(), strings.TrimSpace(settings.Sun.BaseURL))
	token := firstNonEmpty(envSunToken(), strings.TrimSpace(settings.Sun.Token))
	timeout := time.Duration(settings.Sun.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return newSunClient(baseURL, token, timeout)
}

func sunContext(_ Settings) context.Context {
	return context.Background()
}

func splitCSVScopes(csv string) []string {
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		normalized := strings.TrimSpace(part)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func resolveTargetProfiles(profileKey string) ([]codexProfile, error) {
	if profileKey != "" {
		item, err := requireCodexProfile(profileKey)
		if err != nil {
			return nil, err
		}
		return []codexProfile{item}, nil
	}
	profiles := codexProfiles()
	if len(profiles) == 0 {
		return nil, fmt.Errorf("no codex profiles configured")
	}
	return profiles, nil
}

func upsertCodexProfilesInSettings(profiles []codexProfile) error {
	if len(profiles) == 0 {
		return nil
	}
	settings, err := loadSettings()
	if err != nil {
		return err
	}
	if settings.Codex.Profiles.Entries == nil {
		settings.Codex.Profiles.Entries = map[string]CodexProfileEntry{}
	}
	for _, profile := range profiles {
		entry := settings.Codex.Profiles.Entries[profile.ID]
		entry.Name = profile.Name
		entry.Email = profile.Email
		status := codexProfileAuthStatus(profile)
		entry.AuthPath = status.Path
		if status.Exists {
			entry.AuthUpdated = status.Modified.UTC().Format(time.RFC3339)
		}
		settings.Codex.Profiles.Entries[profile.ID] = entry
	}
	return saveSettings(settings)
}

func writeFileAtomic0600(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".sun-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		// Some environments bind-mount dotenv files directly. Replacing those
		// mount targets via rename(2) can fail even when direct write is allowed.
		// If the target already exists as a regular file, fall back to in-place write.
		if info, statErr := os.Stat(path); statErr == nil && info.Mode().IsRegular() {
			if writeErr := os.WriteFile(path, data, 0o600); writeErr == nil {
				return os.Chmod(path, 0o600)
			}
		}
		return err
	}
	return nil
}

func sunPayloadSHA256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func sunLookupObjectMeta(ctx context.Context, client *sunClient, kind string, name string) (*sunObjectMeta, error) {
	items, err := client.listObjects(ctx, kind, name, 5)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if strings.EqualFold(strings.TrimSpace(items[i].Name), strings.TrimSpace(name)) {
			item := items[i]
			return &item, nil
		}
	}
	return nil, nil
}
