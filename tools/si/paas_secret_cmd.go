package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"si/tools/si/internal/vault"
)

var paasSecretActions = []subcommandAction{
	{Name: "set", Description: "set encrypted secret for app/target"},
	{Name: "get", Description: "read secret metadata or value"},
	{Name: "unset", Description: "remove secret key"},
	{Name: "list", Description: "list secret keys for app/target"},
	{Name: "key", Description: "print computed vault key name"},
}

const (
	paasSecretSetUsageText   = "usage: si paas secret set --app <slug> [--target <id>] --name <key> --value <text> [--file <path>] [--json]"
	paasSecretGetUsageText   = "usage: si paas secret get --app <slug> [--target <id>] --name <key> [--reveal] [--file <path>] [--json]"
	paasSecretUnsetUsageText = "usage: si paas secret unset --app <slug> [--target <id>] --name <key> [--file <path>] [--json]"
	paasSecretListUsageText  = "usage: si paas secret list --app <slug> [--target <id>] [--file <path>] [--json]"
	paasSecretKeyUsageText   = "usage: si paas secret key --app <slug> [--target <id>] --name <key> [--json]"
)

func cmdPaasSecret(args []string) {
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(args, isInteractiveTerminal(), selectPaasSecretAction)
	if showUsage {
		printUsage(paasSecretUsageText)
		return
	}
	if !ok {
		return
	}
	args = resolved
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(paasSecretUsageText)
	case "set":
		cmdPaasSecretSet(rest)
	case "get":
		cmdPaasSecretGet(rest)
	case "unset":
		cmdPaasSecretUnset(rest)
	case "list":
		cmdPaasSecretList(rest)
	case "key":
		cmdPaasSecretKey(rest)
	default:
		printUnknown("paas secret", sub)
		printUsage(paasSecretUsageText)
	}
}

func selectPaasSecretAction() (string, bool) {
	return selectSubcommandAction("PaaS secret commands:", paasSecretActions)
}

func cmdPaasSecretSet(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas secret set", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "target id (default current)")
	name := fs.String("name", "", "logical secret key name")
	value := fs.String("value", "", "plaintext value (will be encrypted)")
	fileFlag := fs.String("file", "", "explicit vault file")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasSecretSetUsageText)
		return
	}
	if !requirePaasValue(*app, "app", paasSecretSetUsageText) || !requirePaasValue(*name, "name", paasSecretSetUsageText) || !requirePaasValue(*value, "value", paasSecretSetUsageText) {
		return
	}
	if jsonOut {
		fatal(fmt.Errorf("--json is not supported for secret set; use `si paas secret key --json` for machine-readable key mapping"))
	}
	vaultKey, resolvedTarget := mustPaasSecretVaultKey(*app, *target, *name)
	vaultArgs := []string{vaultKey, *value}
	if strings.TrimSpace(*fileFlag) != "" {
		vaultArgs = append(vaultArgs, "--file", strings.TrimSpace(*fileFlag))
	}
	cmdVaultSet(vaultArgs)
	fmt.Printf("paas secret set: app=%s target=%s key=%s vault_key=%s\n", strings.TrimSpace(*app), resolvedTarget, strings.TrimSpace(*name), vaultKey)
}

func cmdPaasSecretGet(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas secret get", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "target id (default current)")
	name := fs.String("name", "", "logical secret key name")
	reveal := fs.Bool("reveal", false, "reveal decrypted value")
	fileFlag := fs.String("file", "", "explicit vault file")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasSecretGetUsageText)
		return
	}
	if !requirePaasValue(*app, "app", paasSecretGetUsageText) || !requirePaasValue(*name, "name", paasSecretGetUsageText) {
		return
	}
	if jsonOut {
		fatal(fmt.Errorf("--json is not supported for secret get"))
	}
	vaultKey, resolvedTarget := mustPaasSecretVaultKey(*app, *target, *name)
	vaultArgs := []string{vaultKey}
	if *reveal {
		vaultArgs = append(vaultArgs, "--reveal")
	}
	if strings.TrimSpace(*fileFlag) != "" {
		vaultArgs = append(vaultArgs, "--file", strings.TrimSpace(*fileFlag))
	}
	cmdVaultGet(vaultArgs)
	_ = resolvedTarget
}

func cmdPaasSecretUnset(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas secret unset", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "target id (default current)")
	name := fs.String("name", "", "logical secret key name")
	fileFlag := fs.String("file", "", "explicit vault file")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasSecretUnsetUsageText)
		return
	}
	if !requirePaasValue(*app, "app", paasSecretUnsetUsageText) || !requirePaasValue(*name, "name", paasSecretUnsetUsageText) {
		return
	}
	if jsonOut {
		fatal(fmt.Errorf("--json is not supported for secret unset"))
	}
	vaultKey, resolvedTarget := mustPaasSecretVaultKey(*app, *target, *name)
	vaultArgs := []string{vaultKey}
	if strings.TrimSpace(*fileFlag) != "" {
		vaultArgs = append(vaultArgs, "--file", strings.TrimSpace(*fileFlag))
	}
	cmdVaultUnset(vaultArgs)
	_ = resolvedTarget
}

func cmdPaasSecretList(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas secret list", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "target id (default current)")
	fileFlag := fs.String("file", "", "explicit vault file")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasSecretListUsageText)
		return
	}
	if !requirePaasValue(*app, "app", paasSecretListUsageText) {
		return
	}
	resolvedTarget := resolvePaasSecretTarget(strings.TrimSpace(*target))
	prefix := paasSecretKeyPrefix(strings.TrimSpace(*app), resolvedTarget)

	settings := loadSettingsOrDefault()
	targetPath, err := vaultResolveTarget(settings, strings.TrimSpace(*fileFlag), false)
	if err != nil {
		fatal(err)
	}
	doc, err := vault.ReadDotenvFile(targetPath.File)
	if err != nil {
		fatal(err)
	}
	entries, err := vault.Entries(doc)
	if err != nil {
		fatal(err)
	}
	matches := make([]string, 0)
	for _, row := range entries {
		if strings.HasPrefix(row.Key, prefix) {
			matches = append(matches, row.Key)
		}
	}
	if jsonOut {
		payload := map[string]any{
			"ok":         true,
			"command":    "secret list",
			"context":    currentPaasContext(),
			"mode":       "live",
			"app":        strings.TrimSpace(*app),
			"target":     resolvedTarget,
			"key_prefix": prefix,
			"count":      len(matches),
			"data":       matches,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("paas secret list: app=%s target=%s file=%s\n", strings.TrimSpace(*app), resolvedTarget, filepath.Clean(targetPath.File))
	for _, key := range matches {
		fmt.Printf("  %s\n", key)
	}
}

func cmdPaasSecretKey(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas secret key", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "target id (default current)")
	name := fs.String("name", "", "logical secret key name")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasSecretKeyUsageText)
		return
	}
	if !requirePaasValue(*app, "app", paasSecretKeyUsageText) || !requirePaasValue(*name, "name", paasSecretKeyUsageText) {
		return
	}
	vaultKey, resolvedTarget := mustPaasSecretVaultKey(*app, *target, *name)
	if jsonOut {
		emitPaasSecretJSON("key", *app, resolvedTarget, *name, vaultKey, true)
		return
	}
	fmt.Printf("%s\n", vaultKey)
}

func mustPaasSecretVaultKey(app string, target string, name string) (string, string) {
	appSlug := sanitizePaasSecretSegment(app)
	if appSlug == "" {
		fatal(fmt.Errorf("invalid app value %q", strings.TrimSpace(app)))
	}
	resolvedTarget := resolvePaasSecretTarget(target)
	segment := sanitizePaasSecretSegment(name)
	if segment == "" {
		fatal(fmt.Errorf("invalid name value %q", strings.TrimSpace(name)))
	}
	vaultKey := fmt.Sprintf("%sVAR_%s", paasSecretKeyPrefix(appSlug, resolvedTarget), segment)
	if err := vault.ValidateKeyName(vaultKey); err != nil {
		fatal(err)
	}
	return vaultKey, resolvedTarget
}

func resolvePaasSecretTarget(raw string) string {
	selected := strings.TrimSpace(raw)
	if selected != "" {
		return sanitizePaasSecretSegment(selected)
	}
	store, err := loadPaasTargetStore(currentPaasContext())
	if err == nil && strings.TrimSpace(store.CurrentTarget) != "" {
		return sanitizePaasSecretSegment(store.CurrentTarget)
	}
	return "GLOBAL"
}

func paasSecretKeyPrefix(app string, target string) string {
	return fmt.Sprintf("PAAS__CTX_%s__APP_%s__TARGET_%s__", sanitizePaasSecretSegment(currentPaasContext()), sanitizePaasSecretSegment(app), sanitizePaasSecretSegment(target))
}

func sanitizePaasSecretSegment(raw string) string {
	value := strings.ToUpper(strings.TrimSpace(raw))
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
			}
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	return out
}

func emitPaasSecretJSON(op string, app string, target string, name string, vaultKey string, ok bool) {
	payload := map[string]any{
		"ok":        ok,
		"command":   "secret " + op,
		"context":   currentPaasContext(),
		"mode":      "live",
		"app":       strings.TrimSpace(app),
		"target":    target,
		"name":      strings.TrimSpace(name),
		"vault_key": vaultKey,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		fatal(err)
	}
}
