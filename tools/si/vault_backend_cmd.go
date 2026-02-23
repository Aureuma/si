package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const vaultBackendUsageText = "usage: si vault backend <status|use> ..."

func cmdVaultBackend(args []string) {
	if len(args) == 0 {
		printUsage(vaultBackendUsageText)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "status":
		cmdVaultBackendStatus(rest)
	case "use", "set":
		cmdVaultBackendUse(rest)
	case "help", "-h", "--help":
		printUsage(vaultBackendUsageText)
	default:
		printUnknown("vault backend", sub)
		printUsage(vaultBackendUsageText)
		os.Exit(1)
	}
}

func cmdVaultBackendStatus(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault backend status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si vault backend status [--json]")
		return
	}
	resolution, err := resolveVaultSyncBackend(settings)
	if err != nil {
		fatal(err)
	}
	report := map[string]any{
		"mode":               resolution.Mode,
		"source":             resolution.Source,
		"sun_backup_enabled": resolution.Mode == vaultSyncBackendSun,
		"sun_backup_strict":  resolution.Mode == vaultSyncBackendSun,
	}
	if *jsonOut {
		printJSON(report)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("mode:"), resolution.Mode)
	fmt.Printf("%s %s\n", styleHeading("source:"), resolution.Source)
	fmt.Printf("%s %s\n", styleHeading("sun_backup_enabled:"), boolString(resolution.Mode == vaultSyncBackendSun))
	fmt.Printf("%s %s\n", styleHeading("sun_backup_strict:"), boolString(resolution.Mode == vaultSyncBackendSun))
}

func cmdVaultBackendUse(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault backend use", flag.ExitOnError)
	modeFlag := fs.String("mode", "", "vault sync backend mode: git or sun")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si vault backend use --mode <git|sun>")
		return
	}
	mode := normalizeVaultSyncBackend(*modeFlag)
	if mode == "" {
		fatal(fmt.Errorf("invalid --mode %q (expected git or sun)", strings.TrimSpace(*modeFlag)))
	}
	settings.Vault.SyncBackend = mode
	if err := saveSettings(settings); err != nil {
		fatal(err)
	}
	successf("vault sync backend set to %s", mode)
	if mode == vaultSyncBackendSun {
		token := firstNonEmpty(envSunToken(), strings.TrimSpace(settings.Sun.Token))
		if token == "" {
			warnf("sun token not configured; run `si sun auth login --url <sun-url> --token <token> --account <slug>`")
		}
	}
}
