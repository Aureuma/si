package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	heliaUsageText              = "usage: si helia <auth|profile|vault> ..."
	heliaAuthUsageText          = "usage: si helia auth <login|status|logout> ..."
	heliaProfileUsageText       = "usage: si helia profile <list|push|pull> ..."
	heliaVaultUsageText         = "usage: si helia vault backup <push|pull> ..."
	heliaCodexProfileBundleKind = "codex_profile_bundle"
	heliaVaultBackupKind        = "vault_backup"
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
	default:
		printUnknown("helia", sub)
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
		printUnknown("helia auth", sub)
		printUsage(heliaAuthUsageText)
		os.Exit(1)
	}
}

func cmdHeliaAuthLogin(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("helia auth login", flag.ExitOnError)
	urlFlag := fs.String("url", strings.TrimSpace(settings.Helia.BaseURL), "helia base url")
	tokenFlag := fs.String("token", strings.TrimSpace(os.Getenv("SI_HELIA_TOKEN")), "helia bearer token")
	accountFlag := fs.String("account", strings.TrimSpace(settings.Helia.Account), "expected account slug")
	timeoutSeconds := fs.Int("timeout-seconds", settings.Helia.TimeoutSeconds, "http timeout seconds")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si helia auth login [--url <url>] [--token <token>] [--account <slug>] [--timeout-seconds <n>]")
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
	if err := saveSettings(persisted); err != nil {
		fatal(err)
	}
	successf("helia auth configured for account %s at %s", who.AccountSlug, strings.TrimSpace(*urlFlag))
}

func cmdHeliaAuthStatus(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("helia auth status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "json output")
	timeoutSeconds := fs.Int("timeout-seconds", settings.Helia.TimeoutSeconds, "http timeout seconds")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si helia auth status [--json] [--timeout-seconds <n>]")
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
}

func cmdHeliaAuthLogout(args []string) {
	fs := flag.NewFlagSet("helia auth logout", flag.ExitOnError)
	clearAccount := fs.Bool("clear-account", false, "also clear stored helia account")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si helia auth logout [--clear-account]")
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
	successf("helia auth token cleared")
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
		printUnknown("helia profile", sub)
		printUsage(heliaProfileUsageText)
		os.Exit(1)
	}
}

func cmdHeliaProfileList(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("helia profile list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si helia profile list [--json]")
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
	fs := flag.NewFlagSet("helia profile push", flag.ExitOnError)
	profileKey := fs.String("profile", "", "profile id or email")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si helia profile push [--profile <id>] [--json]")
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
	fs := flag.NewFlagSet("helia profile pull", flag.ExitOnError)
	profileKey := fs.String("profile", "", "profile id or email")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si helia profile pull [--profile <id>] [--json]")
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
		printUnknown("helia vault backup", sub)
		printUsage(heliaVaultUsageText)
		os.Exit(1)
	}
}

func cmdHeliaVaultBackupPush(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("helia vault backup push", flag.ExitOnError)
	file := fs.String("file", resolveVaultPath(settings, ""), "vault file path")
	name := fs.String("name", "default", "backup object name")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si helia vault backup push [--file <path>] [--name <name>]")
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
	path = expandTilde(path)
	data, err := os.ReadFile(path)
	if err != nil {
		fatal(err)
	}
	result, err := client.putObject(context.Background(), heliaVaultBackupKind, strings.TrimSpace(*name), data, "text/plain", map[string]interface{}{
		"path": filepath.Base(path),
	}, nil)
	if err != nil {
		fatal(err)
	}
	successf("vault backup pushed (%s revision %d)", strings.TrimSpace(*name), result.Result.Revision.Revision)
}

func cmdHeliaVaultBackupPull(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("helia vault backup pull", flag.ExitOnError)
	file := fs.String("file", resolveVaultPath(settings, ""), "vault file path")
	name := fs.String("name", "default", "backup object name")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si helia vault backup pull [--file <path>] [--name <name>]")
		return
	}
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		fatal(err)
	}
	data, err := client.getPayload(context.Background(), heliaVaultBackupKind, strings.TrimSpace(*name))
	if err != nil {
		fatal(err)
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

func heliaClientFromSettings(settings Settings) (*heliaClient, error) {
	baseURL := firstNonEmpty(strings.TrimSpace(os.Getenv("SI_HELIA_BASE_URL")), strings.TrimSpace(settings.Helia.BaseURL))
	token := firstNonEmpty(strings.TrimSpace(os.Getenv("SI_HELIA_TOKEN")), strings.TrimSpace(settings.Helia.Token))
	timeout := time.Duration(settings.Helia.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return newHeliaClient(baseURL, token, timeout)
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
	tmp, err := os.CreateTemp(dir, ".helia-*")
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
