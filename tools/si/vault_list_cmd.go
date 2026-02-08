package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"si/tools/si/internal/vault"
)

func cmdVaultList(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault list", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path (overrides --vault-dir/--env)")
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to host git root)")
	env := fs.String("env", settings.Vault.DefaultEnv, "environment name (maps to .env.<env>)")
	fs.Parse(args)

	if len(fs.Args()) != 0 {
		printUsage("usage: si vault list [--vault-dir <path>] [--env <name>]")
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
	entries := vault.Entries(doc)

	fmt.Printf("file: %s\n", filepath.Clean(target.File))
	for _, e := range entries {
		if e.Encrypted {
			fmt.Printf("%s\t(encrypted)\n", e.Key)
		} else {
			fmt.Printf("%s\t(plaintext)\n", e.Key)
		}
	}
}
