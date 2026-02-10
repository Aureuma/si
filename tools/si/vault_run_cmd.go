package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultRun(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault run", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path (overrides --vault-dir/--env)")
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to host git root)")
	env := fs.String("env", settings.Vault.DefaultEnv, "environment name (maps to .env.<env>)")
	allowPlaintext := fs.Bool("allow-plaintext", false, "allow running even if plaintext keys exist (not recommended)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	rest := fs.Args()
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		printUsage("usage: si vault run [--vault-dir <path>] [--env <name>] -- <cmd...>")
		return
	}

	target, err := vaultResolveTarget(settings, *fileFlag, *vaultDir, *env, false, false)
	if err != nil {
		fatal(err)
	}
	doc, err := vault.ReadDotenvFile(target.File)
	if err != nil {
		fatal(err)
	}
	if _, err := vaultRequireTrusted(settings, target, doc); err != nil {
		fatal(err)
	}
	info, err := vault.LoadIdentity(vaultKeyConfigFromSettings(settings))
	if err != nil {
		fatal(err)
	}
	dec, err := vault.DecryptEnv(doc, info.Identity)
	if err != nil {
		fatal(err)
	}
	if len(dec.PlaintextKeys) > 0 {
		sort.Strings(dec.PlaintextKeys)
		if !*allowPlaintext {
			fatal(fmt.Errorf("vault file contains plaintext keys: %s (run `si vault encrypt` or pass --allow-plaintext)", strings.Join(dec.PlaintextKeys, ", ")))
		}
		warnf("vault file contains plaintext keys (allowed): %s", strings.Join(dec.PlaintextKeys, ", "))
	}

	keys := make([]string, 0, len(dec.Values))
	for k := range dec.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	vaultAuditEvent(settings, target, "run", map[string]any{
		"envFile":      target.File,
		"cmd0":         rest[0],
		"argsLen":      len(rest) - 1,
		"keysCount":    len(keys),
		"decryptCount": len(dec.DecryptedKeys),
		"plainCount":   len(dec.PlaintextKeys),
	})

	// #nosec G204 -- command is explicitly provided by the local operator.
	cmd := exec.CommandContext(context.Background(), rest[0], rest[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = append(os.Environ(), envPairs(dec.Values)...)
	if err := cmd.Run(); err != nil {
		fatal(err)
	}
}

func envPairs(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+values[k])
	}
	return out
}
