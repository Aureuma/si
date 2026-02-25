package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	paasSecretSetUsageText   = "usage: si paas secret set --app <slug> [--target <id>] [--namespace <name>] --name <key> --value <text> [--file <scope>] [--json]"
	paasSecretGetUsageText   = "usage: si paas secret get --app <slug> [--target <id>] [--namespace <name>] --name <key> [--reveal --allow-plaintext] [--file <scope>] [--json]"
	paasSecretUnsetUsageText = "usage: si paas secret unset --app <slug> [--target <id>] [--namespace <name>] --name <key> [--file <scope>] [--json]"
	paasSecretListUsageText  = "usage: si paas secret list --app <slug> [--target <id>] [--namespace <name>] [--file <scope>] [--json]"
	paasSecretKeyUsageText   = "usage: si paas secret key --app <slug> [--target <id>] [--namespace <name>] --name <key> [--json]"
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
	namespace := fs.String("namespace", "", "secret namespace (default context namespace)")
	name := fs.String("name", "", "logical secret key name")
	value := fs.String("value", "", "plaintext value (will be encrypted)")
	fileFlag := fs.String("file", "", "vault scope (compat alias)")
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
	resolvedNamespace := resolvePaasSecretNamespace(*namespace)
	vaultKey, resolvedTarget := mustPaasSecretVaultKey(*app, *target, resolvedNamespace, *name)
	vaultArgs := []string{vaultKey, *value}
	if resolvedVaultFile := resolvePaasContextVaultFile(*fileFlag); strings.TrimSpace(resolvedVaultFile) != "" {
		vaultArgs = append(vaultArgs, "--file", strings.TrimSpace(resolvedVaultFile))
	}
	cmdVaultSet(vaultArgs)
	fmt.Printf("paas secret set: app=%s target=%s namespace=%s key=%s vault_key=%s\n", strings.TrimSpace(*app), resolvedTarget, resolvedNamespace, strings.TrimSpace(*name), vaultKey)
}

func cmdPaasSecretGet(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas secret get", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "target id (default current)")
	namespace := fs.String("namespace", "", "secret namespace (default context namespace)")
	name := fs.String("name", "", "logical secret key name")
	reveal := fs.Bool("reveal", false, "reveal decrypted value")
	allowPlaintext := fs.Bool("allow-plaintext", false, "required with --reveal to acknowledge plaintext output risk")
	fileFlag := fs.String("file", "", "vault scope (compat alias)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasSecretGetUsageText)
		return
	}
	if !requirePaasValue(*app, "app", paasSecretGetUsageText) || !requirePaasValue(*name, "name", paasSecretGetUsageText) {
		return
	}
	if err := enforcePaasSecretRevealGuardrail(*reveal, *allowPlaintext); err != nil {
		fatal(err)
	}
	if jsonOut {
		fatal(fmt.Errorf("--json is not supported for secret get"))
	}
	resolvedNamespace := resolvePaasSecretNamespace(*namespace)
	vaultKey, resolvedTarget := mustPaasSecretVaultKey(*app, *target, resolvedNamespace, *name)
	vaultArgs := []string{vaultKey}
	if *reveal {
		vaultArgs = append(vaultArgs, "--reveal")
	}
	if resolvedVaultFile := resolvePaasContextVaultFile(*fileFlag); strings.TrimSpace(resolvedVaultFile) != "" {
		vaultArgs = append(vaultArgs, "--file", strings.TrimSpace(resolvedVaultFile))
	}
	cmdVaultGet(vaultArgs)
	_ = resolvedTarget
}

func cmdPaasSecretUnset(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas secret unset", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "target id (default current)")
	namespace := fs.String("namespace", "", "secret namespace (default context namespace)")
	name := fs.String("name", "", "logical secret key name")
	fileFlag := fs.String("file", "", "vault scope (compat alias)")
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
	resolvedNamespace := resolvePaasSecretNamespace(*namespace)
	vaultKey, resolvedTarget := mustPaasSecretVaultKey(*app, *target, resolvedNamespace, *name)
	vaultArgs := []string{vaultKey}
	if resolvedVaultFile := resolvePaasContextVaultFile(*fileFlag); strings.TrimSpace(resolvedVaultFile) != "" {
		vaultArgs = append(vaultArgs, "--file", strings.TrimSpace(resolvedVaultFile))
	}
	cmdVaultUnset(vaultArgs)
	_ = resolvedTarget
}

func cmdPaasSecretList(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas secret list", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "target id (default current)")
	namespace := fs.String("namespace", "", "secret namespace (default context namespace)")
	fileFlag := fs.String("file", "", "vault scope (compat alias)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasSecretListUsageText)
		return
	}
	if !requirePaasValue(*app, "app", paasSecretListUsageText) {
		return
	}
	resolvedNamespace := resolvePaasSecretNamespace(*namespace)
	resolvedTarget := resolvePaasSecretTarget(strings.TrimSpace(*target))
	prefix := paasSecretKeyPrefix(resolvedNamespace, strings.TrimSpace(*app), resolvedTarget)

	settings := loadSettingsOrDefault()
	targetPath, err := vaultResolveTarget(settings, resolvePaasContextVaultFile(strings.TrimSpace(*fileFlag)), false)
	if err != nil {
		fatal(err)
	}
	values, used, err := vaultSunKVLoadRawValues(settings, targetPath)
	if err != nil {
		fatal(err)
	}
	if !used {
		fatal(fmt.Errorf("sun vault unavailable: run `si sun auth login --url <url> --token <token> --account <slug>`"))
	}
	matches := make([]string, 0)
	for key := range values {
		if strings.HasPrefix(key, prefix) {
			matches = append(matches, key)
		}
	}
	sort.Strings(matches)
	if jsonOut {
		payload := map[string]any{
			"ok":         true,
			"command":    "secret list",
			"context":    currentPaasContext(),
			"mode":       "live",
			"app":        strings.TrimSpace(*app),
			"namespace":  resolvedNamespace,
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
	fmt.Printf("paas secret list: app=%s target=%s namespace=%s scope=%s\n", strings.TrimSpace(*app), resolvedTarget, resolvedNamespace, strings.TrimSpace(targetPath.File))
	for _, key := range matches {
		fmt.Printf("  %s\n", key)
	}
}

func cmdPaasSecretKey(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas secret key", flag.ExitOnError)
	app := fs.String("app", "", "app slug")
	target := fs.String("target", "", "target id (default current)")
	namespace := fs.String("namespace", "", "secret namespace (default context namespace)")
	name := fs.String("name", "", "logical secret key name")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasSecretKeyUsageText)
		return
	}
	if !requirePaasValue(*app, "app", paasSecretKeyUsageText) || !requirePaasValue(*name, "name", paasSecretKeyUsageText) {
		return
	}
	resolvedNamespace := resolvePaasSecretNamespace(*namespace)
	vaultKey, resolvedTarget := mustPaasSecretVaultKey(*app, *target, resolvedNamespace, *name)
	if jsonOut {
		emitPaasSecretJSON("key", *app, resolvedNamespace, resolvedTarget, *name, vaultKey, true)
		return
	}
	fmt.Printf("%s\n", vaultKey)
}

func mustPaasSecretVaultKey(app string, target string, namespace string, name string) (string, string) {
	appSlug := sanitizePaasSecretSegment(app)
	if appSlug == "" {
		fatal(fmt.Errorf("invalid app value %q", strings.TrimSpace(app)))
	}
	namespaceSlug := resolvePaasSecretNamespace(namespace)
	resolvedTarget := resolvePaasSecretTarget(target)
	segment := sanitizePaasSecretSegment(name)
	if segment == "" {
		fatal(fmt.Errorf("invalid name value %q", strings.TrimSpace(name)))
	}
	vaultKey := fmt.Sprintf("%sVAR_%s", paasSecretKeyPrefix(namespaceSlug, appSlug, resolvedTarget), segment)
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

func paasSecretKeyPrefix(namespace string, app string, target string) string {
	return fmt.Sprintf("PAAS__CTX_%s__NS_%s__APP_%s__TARGET_%s__", sanitizePaasSecretSegment(currentPaasContext()), sanitizePaasSecretSegment(namespace), sanitizePaasSecretSegment(app), sanitizePaasSecretSegment(target))
}

func resolvePaasSecretNamespace(raw string) string {
	if value := sanitizePaasSecretSegment(raw); value != "" {
		return value
	}
	return "DEFAULT"
}

func resolvePaasContextVaultFile(fileFlag string) string {
	if strings.TrimSpace(fileFlag) != "" {
		return strings.TrimSpace(fileFlag)
	}
	if strings.TrimSpace(os.Getenv("SI_VAULT_FILE")) != "" {
		return ""
	}
	contextDir, err := resolvePaasContextDir(currentPaasContext())
	if err != nil {
		return ""
	}
	return filepath.Join(contextDir, "vault", "secrets.env")
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

func emitPaasSecretJSON(op string, app string, namespace string, target string, name string, vaultKey string, ok bool) {
	payload := map[string]any{
		"ok":        ok,
		"command":   "secret " + op,
		"context":   currentPaasContext(),
		"mode":      "live",
		"app":       strings.TrimSpace(app),
		"namespace": strings.TrimSpace(namespace),
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
