package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultRestore(args []string) {
	fs := flag.NewFlagSet("vault restore", flag.ExitOnError)
	envFile := fs.String("env-file", defaultSIVaultDotenvFile, "path to dotenv file")
	fileAlias := fs.String("file", "", "alias for --env-file")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() > 0 {
		printUsage("usage: si vault restore [--env-file <path>]")
		return
	}
	pathValue := strings.TrimSpace(*envFile)
	if strings.TrimSpace(*fileAlias) != "" {
		pathValue = strings.TrimSpace(*fileAlias)
	}
	if pathValue == "" {
		pathValue = defaultSIVaultDotenvFile
	}
	if !filepath.IsAbs(pathValue) {
		cwd, err := os.Getwd()
		if err != nil {
			fatal(err)
		}
		pathValue = filepath.Join(cwd, pathValue)
	}
	data, backupPath, err := loadEncryptedRestoreBackup(pathValue)
	if err != nil {
		if os.IsNotExist(err) {
			fatal(fmt.Errorf("no encrypted restore backup for %s", filepath.Clean(pathValue)))
		}
		fatal(err)
	}
	if err := vault.WriteDotenvFileAtomic(pathValue, data); err != nil {
		fatal(err)
	}
	_ = os.Remove(backupPath)
	fmt.Printf("restored: %s\n", filepath.Clean(pathValue))
}
