package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultFmt(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault fmt", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path (overrides --vault-dir/--env)")
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to host git root)")
	env := fs.String("env", settings.Vault.DefaultEnv, "environment name (maps to .env.<env>)")
	all := fs.Bool("all", false, "format all .env.* files in the vault dir")
	check := fs.Bool("check", false, "fail if formatting would make changes")
	fs.Parse(args)
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault fmt [--vault-dir <path>] [--env <name>] [--all] [--check]")
		return
	}

	if *all {
		target, err := vaultResolveTarget(settings, "", *vaultDir, *env, false, true)
		if err != nil {
			fatal(err)
		}
		entries, err := os.ReadDir(target.VaultDir)
		if err != nil {
			fatal(err)
		}
		anyChanged := false
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasPrefix(name, ".env.") {
				continue
			}
			path := filepath.Join(target.VaultDir, name)
			changed, err := vaultFormatOne(path, *check)
			if err != nil {
				fatal(err)
			}
			anyChanged = anyChanged || changed
		}
		if *check && anyChanged {
			fatal(fmt.Errorf("vault fmt --check: changes required"))
		}
		return
	}

	target, err := vaultResolveTarget(settings, *fileFlag, *vaultDir, *env, false, false)
	if err != nil {
		fatal(err)
	}
	changed, err := vaultFormatOne(target.File, *check)
	if err != nil {
		fatal(err)
	}
	if *check && changed {
		fatal(fmt.Errorf("vault fmt --check: %s needs formatting", filepath.Clean(target.File)))
	}
}

func vaultFormatOne(path string, check bool) (bool, error) {
	doc, err := vault.ReadDotenvFile(path)
	if err != nil {
		return false, err
	}
	formatted, changed, err := vault.FormatVaultDotenv(doc)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	if check {
		return true, nil
	}
	if err := vault.WriteDotenvFileAtomic(path, formatted.Bytes()); err != nil {
		return false, err
	}
	return true, nil
}
