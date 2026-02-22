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
	fileFlag := fs.String("file", "", "explicit env file path (defaults to the configured vault.file)")
	all := fs.Bool("all", false, "format all .env* files in the same directory as the target file")
	check := fs.Bool("check", false, "fail if formatting would make changes")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault fmt [--file <path>] [--all] [--check]")
		return
	}

	target, err := vaultResolveTarget(settings, strings.TrimSpace(*fileFlag), false)
	if err != nil {
		fatal(err)
	}

	if *all {
		dir := filepath.Dir(target.File)
		entries, err := os.ReadDir(dir)
		if err != nil {
			fatal(err)
		}
		anyChanged := false
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if name != ".env" && !strings.HasPrefix(name, ".env.") {
				continue
			}
			path := filepath.Join(dir, name)
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
	if err := vaultWriteDotenvFileAtomic(path, formatted.Bytes()); err != nil {
		return false, err
	}
	maybeHeliaAutoBackupVault("vault_fmt", path)
	return true, nil
}
