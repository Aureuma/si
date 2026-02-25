package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const vaultSyncUsageText = "usage: si vault sync <push|pull|status> ..."

func cmdVaultSync(args []string) {
	if len(args) == 0 {
		printUsage(vaultSyncUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "push":
		fatal(fmt.Errorf("vault sync push is not supported in Sun remote vault mode (vault data is already stored in Sun KV)"))
	case "pull":
		fatal(fmt.Errorf("vault sync pull is not supported in Sun remote vault mode (no local vault file materialization)"))
	case "status":
		cmdVaultSyncStatus(rest)
	case "help", "-h", "--help":
		printUsage(vaultSyncUsageText)
	default:
		printUnknown("vault sync", sub)
		printUsage(vaultSyncUsageText)
		os.Exit(1)
	}
}

func cmdVaultSyncStatus(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault sync status", flag.ExitOnError)
	file := fs.String("file", resolveVaultPath(settings, ""), "vault scope (preferred: --scope)")
	scopeFlag := fs.String("scope", "", "vault scope")
	name := fs.String("name", strings.TrimSpace(settings.Sun.VaultBackup), "backup object name")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si vault sync status [--scope <name>] [--name <name>] [--json]")
		return
	}
	scope := strings.TrimSpace(*scopeFlag)
	if scope == "" {
		scope = strings.TrimSpace(*file)
	}

	backend, err := resolveVaultSyncBackend(settings)
	if err != nil {
		fatal(err)
	}
	backupName := strings.TrimSpace(*name)
	if backupName == "" {
		backupName = "default"
	}

	report := map[string]interface{}{
		"mode":        backend.Mode,
		"source":      backend.Source,
		"scope":       vaultNormalizeScope(scope),
		"backup_name": backupName,
		"sun_base_url": firstNonEmpty(
			envSunBaseURL(),
			strings.TrimSpace(settings.Sun.BaseURL),
		),
		"sun_account": firstNonEmpty(
			strings.TrimSpace(settings.Sun.Account),
			"",
		),
	}
	target, targetErr := vaultResolveTargetStatus(settings, scope)
	if targetErr == nil {
		report["kv_kind"] = vaultSunKVKind(target)
	}

	client, clientErr := sunClientFromSettings(settings)
	if clientErr != nil {
		report["sun_configured"] = false
		report["sun_error"] = clientErr.Error()
	} else {
		report["sun_configured"] = true
		meta, metaErr := sunLookupObjectMeta(sunContext(settings), client, sunVaultBackupKind, backupName)
		if metaErr != nil {
			report["backup_lookup_error"] = metaErr.Error()
		} else if meta != nil {
			report["backup_exists"] = true
			report["backup_revision"] = meta.LatestRevision
			report["backup_checksum"] = strings.TrimSpace(meta.Checksum)
			report["backup_updated_at"] = strings.TrimSpace(meta.UpdatedAt)
			report["backup_size_bytes"] = meta.SizeBytes
		} else {
			report["backup_exists"] = false
		}
		if targetErr == nil {
			if items, listErr := client.listObjects(sunContext(settings), vaultSunKVKind(target), "", sunVaultKVListLimit); listErr != nil {
				report["kv_lookup_error"] = listErr.Error()
			} else {
				keyCount := 0
				for _, item := range items {
					if vaultSunKVMetaBool(item.Metadata, "deleted") {
						continue
					}
					keyCount++
				}
				report["kv_key_count"] = keyCount
			}
		}
	}

	if *jsonOut {
		printJSON(report)
		return
	}

	fmt.Printf("%s %s\n", styleHeading("mode:"), report["mode"])
	fmt.Printf("%s %s\n", styleHeading("source:"), report["source"])
	fmt.Printf("%s %s\n", styleHeading("scope:"), report["scope"])
	fmt.Printf("%s %s\n", styleHeading("backup_name:"), report["backup_name"])
	fmt.Printf("%s %s\n", styleHeading("sun_base_url:"), report["sun_base_url"])
	if report["kv_kind"] != nil {
		fmt.Printf("%s %v\n", styleHeading("kv_kind:"), report["kv_kind"])
	}
	if report["sun_account"] != "" {
		fmt.Printf("%s %s\n", styleHeading("sun_account:"), report["sun_account"])
	}
	fmt.Printf("%s %s\n", styleHeading("sun_configured:"), boolString(report["sun_configured"] == true))
	if msg, ok := report["sun_error"].(string); ok && strings.TrimSpace(msg) != "" {
		fmt.Printf("%s %s\n", styleHeading("sun_error:"), msg)
	}
	if msg, ok := report["backup_lookup_error"].(string); ok && strings.TrimSpace(msg) != "" {
		fmt.Printf("%s %s\n", styleHeading("backup_lookup_error:"), msg)
		return
	}
	if msg, ok := report["kv_lookup_error"].(string); ok && strings.TrimSpace(msg) != "" {
		fmt.Printf("%s %s\n", styleHeading("kv_lookup_error:"), msg)
	}
	if report["kv_key_count"] != nil {
		fmt.Printf("%s %v\n", styleHeading("kv_key_count:"), report["kv_key_count"])
	}
	if exists, _ := report["backup_exists"].(bool); exists {
		fmt.Printf("%s %v\n", styleHeading("backup_revision:"), report["backup_revision"])
		fmt.Printf("%s %v\n", styleHeading("backup_checksum:"), report["backup_checksum"])
		fmt.Printf("%s %v\n", styleHeading("backup_updated_at:"), report["backup_updated_at"])
		fmt.Printf("%s %v\n", styleHeading("backup_size_bytes:"), report["backup_size_bytes"])
	} else {
		fmt.Printf("%s %s\n", styleHeading("backup_exists:"), boolString(false))
	}
}
