package main

import (
	"context"
	"flag"
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
	fs.Parse(args)

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
	data, err := os.ReadFile(target.File)
	if err != nil {
		fatal(err)
	}
	doc := vault.ParseDotenv(data)
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
		warnf("vault file contains plaintext keys: %s (consider `si vault encrypt`)", strings.Join(dec.PlaintextKeys, ", "))
	}

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
