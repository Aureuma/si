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

	"si/tools/si/internal/vault"
)

const (
	heliaUsageText                    = "usage: si sun <auth|profile|vault|token|audit|taskboard|machine|doctor> ..."
	heliaAuthUsageText                = "usage: si sun auth <login|status|logout> ..."
	heliaProfileUsageText             = "usage: si sun profile <list|push|pull> ..."
	heliaVaultUsageText               = "usage: si sun vault backup <push|pull> ..."
	heliaTokenUsageText               = "usage: si sun token <list|create|revoke> ..."
	heliaAuditUsageText               = "usage: si sun audit list ..."
	heliaCodexProfileBundleKind       = "codex_profile_bundle"
	heliaVaultBackupKind              = "vault_backup"
	heliaPaasControlPlaneSnapshotKind = "paas_control_plane_snapshot"
)

type heliaCodexProfileBundle struct {
	ID       string          `json:"id"`
	Name     string          `json:"name,omitempty"`
	Email    string          `json:"email,omitempty"`
	AuthJSON json.RawMessage `json:"auth_json"`
	SyncedAt string          `json:"synced_at,omitempty"`
}

func cmdHelia(args []string) {
	if len(args) == 0 {
		printUsage(heliaUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(heliaUsageText)
	case "auth":
		cmdHeliaAuth(rest)
	case "profile", "profiles":
		cmdHeliaProfile(rest)
	case "vault":
		cmdHeliaVault(rest)
	case "token", "tokens":
		cmdHeliaToken(rest)
	case "audit":
		cmdHeliaAudit(rest)
	case "taskboard":
		cmdHeliaTaskboard(rest)
	case "machine":
		cmdHeliaMachine(rest)
	case "doctor":
		cmdHeliaDoctor(rest)
	default:
		printUnknown("sun", sub)
		printUsage(heliaUsageText)
		os.Exit(1)
	}
}

func cmdHeliaAuth(args []string) {
	if len(args) == 0 {
		printUsage(heliaAuthUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(heliaAuthUsageText)
	case "login":
		cmdHeliaAuthLogin(rest)
	case "status":
		cmdHeliaAuthStatus(rest)
	case "logout":
		cmdHeliaAuthLogout(rest)
	default:
		printUnknown("sun auth", sub)
		printUsage(heliaAuthUsageText)
		os.Exit(1)
	}
}

func cmdHeliaAuthLogin(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun auth login", flag.ExitOnError)
	urlFlag := fs.String("url", strings.TrimSpace(settings.Helia.BaseURL), "sun base url")
	tokenFlag := fs.String("token", envSunToken(), "sun bearer token")
	accountFlag := fs.String("account", strings.TrimSpace(settings.Helia.Account), "expected account slug")
	timeoutSeconds := fs.Int("timeout-seconds", settings.Helia.TimeoutSeconds, "http timeout seconds")
	autoSync := fs.Bool("auto-sync", settings.Helia.AutoSync, "enable automatic profile sync after login/swap")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun auth login [--url <url>] [--token <token>] [--account <slug>] [--timeout-seconds <n>] [--auto-sync]")
		return
	}

	token := strings.TrimSpace(*tokenFlag)
	if token == "" {
		token = strings.TrimSpace(settings.Helia.Token)
	}
	client, err := newHeliaClient(strings.TrimSpace(*urlFlag), token, time.Duration(*timeoutSeconds)*time.Second)
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
	persisted.Helia.BaseURL = strings.TrimSpace(*urlFlag)
	persisted.Helia.Token = token
	persisted.Helia.Account = who.AccountSlug
	persisted.Helia.TimeoutSeconds = *timeoutSeconds
	persisted.Helia.AutoSync = *autoSync
	if err := saveSettings(persisted); err != nil {
		fatal(err)
	}
	successf("sun auth configured for account %s at %s", who.AccountSlug, strings.TrimSpace(*urlFlag))
}

func cmdHeliaAuthStatus(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun auth status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "json output")
	timeoutSeconds := fs.Int("timeout-seconds", settings.Helia.TimeoutSeconds, "http timeout seconds")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun auth status [--json] [--timeout-seconds <n>]")
		return
	}
	client, err := heliaClientFromSettings(settings)
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
	fmt.Printf("%s %s\n", styleHeading("auto_sync:"), boolString(settings.Helia.AutoSync))
}

func cmdHeliaAuthLogout(args []string) {
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
	settings.Helia.Token = ""
	if *clearAccount {
		settings.Helia.Account = ""
	}
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	successf("sun auth token cleared")
}

func cmdHeliaProfile(args []string) {
	if len(args) == 0 {
		printUsage(heliaProfileUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(heliaProfileUsageText)
	case "list":
		cmdHeliaProfileList(rest)
	case "push":
		cmdHeliaProfilePush(rest)
	case "pull":
		cmdHeliaProfilePull(rest)
	default:
		printUnknown("sun profile", sub)
		printUsage(heliaProfileUsageText)
		os.Exit(1)
	}
}

func cmdHeliaProfileList(args []string) {
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
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	items, err := client.listObjects(context.Background(), heliaCodexProfileBundleKind, "", 200)
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

func cmdHeliaProfilePush(args []string) {
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
	client, err := heliaClientFromSettings(settings)
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
		bundle := heliaCodexProfileBundle{
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
		put, err := client.putObject(context.Background(), heliaCodexProfileBundleKind, profile.ID, payload, "application/json", map[string]interface{}{
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

func cmdHeliaProfilePull(args []string) {
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
	client, err := heliaClientFromSettings(settings)
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
		items, err := client.listObjects(context.Background(), heliaCodexProfileBundleKind, "", 200)
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
		payload, err := client.getPayload(context.Background(), heliaCodexProfileBundleKind, profileID)
		if err != nil {
			fatal(err)
		}
		var bundle heliaCodexProfileBundle
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

func cmdHeliaVault(args []string) {
	if len(args) == 0 {
		printUsage(heliaVaultUsageText)
		return
	}
	if strings.ToLower(strings.TrimSpace(args[0])) != "backup" {
		printUsage(heliaVaultUsageText)
		return
	}
	rest := args[1:]
	if len(rest) == 0 {
		printUsage(heliaVaultUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(rest[0]))
	switch sub {
	case "push":
		cmdHeliaVaultBackupPush(rest[1:])
	case "pull":
		cmdHeliaVaultBackupPull(rest[1:])
	case "help", "-h", "--help":
		printUsage(heliaVaultUsageText)
	default:
		printUnknown("sun vault backup", sub)
		printUsage(heliaVaultUsageText)
		os.Exit(1)
	}
}

func cmdHeliaVaultBackupPush(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun vault backup push", flag.ExitOnError)
	file := fs.String("file", resolveVaultPath(settings, ""), "vault file path")
	name := fs.String("name", strings.TrimSpace(settings.Helia.VaultBackup), "backup object name")
	allowPlaintext := fs.Bool("allow-plaintext", false, "allow plaintext vault values in backup payload")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun vault backup push [--file <path>] [--name <name>] [--allow-plaintext]")
		return
	}
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	path := strings.TrimSpace(*file)
	if path == "" {
		fatal(fmt.Errorf("vault file path required"))
	}
	backupName := strings.TrimSpace(*name)
	if backupName == "" {
		fatal(fmt.Errorf("backup name required (--name or helia.vault_backup)"))
	}
	path = expandTilde(path)
	data, err := os.ReadFile(path)
	if err != nil {
		fatal(err)
	}
	if !*allowPlaintext {
		doc, err := vault.ReadDotenvFile(path)
		if err != nil {
			fatal(fmt.Errorf("read vault dotenv: %w", err))
		}
		scan, err := vault.ScanDotenvEncryption(doc)
		if err != nil {
			fatal(fmt.Errorf("scan vault encryption: %w", err))
		}
		if len(scan.PlaintextKeys) > 0 {
			fatal(fmt.Errorf("vault file contains plaintext keys; run `si vault encrypt` first or re-run with --allow-plaintext"))
		}
	}
	result, err := client.putObject(context.Background(), heliaVaultBackupKind, backupName, data, "text/plain", map[string]interface{}{
		"path":   filepath.Base(path),
		"sha256": heliaPayloadSHA256Hex(data),
	}, nil)
	if err != nil {
		fatal(err)
	}
	successf("vault backup pushed (%s revision %d)", backupName, result.Result.Revision.Revision)
}

func cmdHeliaVaultBackupPull(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun vault backup pull", flag.ExitOnError)
	file := fs.String("file", resolveVaultPath(settings, ""), "vault file path")
	name := fs.String("name", strings.TrimSpace(settings.Helia.VaultBackup), "backup object name")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si sun vault backup pull [--file <path>] [--name <name>]")
		return
	}
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	backupName := strings.TrimSpace(*name)
	if backupName == "" {
		fatal(fmt.Errorf("backup name required (--name or helia.vault_backup)"))
	}
	meta, metaErr := heliaLookupObjectMeta(heliaContext(settings), client, heliaVaultBackupKind, backupName)
	if metaErr != nil {
		warnf("vault backup checksum verification preflight skipped: %v", metaErr)
	}
	data, err := client.getPayload(context.Background(), heliaVaultBackupKind, backupName)
	if err != nil {
		fatal(err)
	}
	if meta != nil && strings.TrimSpace(meta.Checksum) != "" {
		got := heliaPayloadSHA256Hex(data)
		want := strings.TrimSpace(meta.Checksum)
		if !strings.EqualFold(got, want) {
			fatal(fmt.Errorf("vault backup checksum mismatch for %s: expected %s got %s", backupName, want, got))
		}
	}
	if meta != nil && meta.SizeBytes > 0 && int64(len(data)) != meta.SizeBytes {
		fatal(fmt.Errorf("vault backup size mismatch for %s: expected %d bytes got %d", backupName, meta.SizeBytes, len(data)))
	}
	path := expandTilde(strings.TrimSpace(*file))
	if path == "" {
		fatal(fmt.Errorf("vault file path required"))
	}
	if err := writeFileAtomic0600(path, data); err != nil {
		fatal(err)
	}
	successf("vault backup pulled to %s", path)
}

func cmdHeliaToken(args []string) {
	if len(args) == 0 {
		printUsage(heliaTokenUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdHeliaTokenList(rest)
	case "create", "issue":
		cmdHeliaTokenCreate(rest)
	case "revoke":
		cmdHeliaTokenRevoke(rest)
	case "help", "-h", "--help":
		printUsage(heliaTokenUsageText)
	default:
		printUnknown("sun token", sub)
		printUsage(heliaTokenUsageText)
		os.Exit(1)
	}
}

func cmdHeliaTokenList(args []string) {
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
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	items, err := client.listTokens(heliaContext(settings), *includeRevoked, *limit)
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

func cmdHeliaTokenCreate(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("sun token create", flag.ExitOnError)
	label := fs.String("label", "si-cli", "token label")
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
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	issued, err := client.createToken(heliaContext(settings), strings.TrimSpace(*label), splitCSVScopes(*scopesCSV), *expiresHours)
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

func cmdHeliaTokenRevoke(args []string) {
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
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	if err := client.revokeToken(heliaContext(settings), strings.TrimSpace(*tokenID)); err != nil {
		fatal(err)
	}
	successf("revoked token %s", strings.TrimSpace(*tokenID))
}

func cmdHeliaAudit(args []string) {
	if len(args) == 0 {
		printUsage(heliaAuditUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdHeliaAuditList(rest)
	case "help", "-h", "--help":
		printUsage(heliaAuditUsageText)
	default:
		printUnknown("sun audit", sub)
		printUsage(heliaAuditUsageText)
		os.Exit(1)
	}
}

func cmdHeliaAuditList(args []string) {
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
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	items, err := client.listAuditEvents(heliaContext(settings), strings.TrimSpace(*action), strings.TrimSpace(*kind), strings.TrimSpace(*name), *limit)
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

func cmdHeliaDoctor(args []string) {
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
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	ctx := heliaContext(settings)
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

func heliaClientFromSettings(settings Settings) (*heliaClient, error) {
	baseURL := firstNonEmpty(envSunBaseURL(), strings.TrimSpace(settings.Helia.BaseURL))
	token := firstNonEmpty(envSunToken(), strings.TrimSpace(settings.Helia.Token))
	timeout := time.Duration(settings.Helia.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return newHeliaClient(baseURL, token, timeout)
}

func heliaContext(_ Settings) context.Context {
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
	return os.Rename(tmp.Name(), path)
}

func resolveVaultPath(settings Settings, explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit)
	}
	if strings.TrimSpace(settings.Vault.File) != "" {
		return strings.TrimSpace(settings.Vault.File)
	}
	return "~/.si/vault/.env"
}

func heliaPayloadSHA256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func heliaLookupObjectMeta(ctx context.Context, client *heliaClient, kind string, name string) (*heliaObjectMeta, error) {
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
