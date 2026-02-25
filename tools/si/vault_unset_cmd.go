package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultUnset(args []string) {
	fs := flag.NewFlagSet("vault unset", flag.ExitOnError)
	envFile := fs.String("env-file", defaultSIVaultDotenvFile, "dotenv file path")
	fileAlias := fs.String("file", "", "alias for --env-file")
	scopeAlias := fs.String("scope", "", "alias for --env")
	repoFlag := fs.String("repo", "", "vault repo slug")
	envFlag := fs.String("env", "", "vault environment")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() != 1 {
		printUsage("usage: si vault unset <KEY> [--env-file <path>] [--repo <slug>] [--env <name>]")
		return
	}
	key := strings.TrimSpace(fs.Arg(0))
	if err := vault.ValidateKeyName(key); err != nil {
		fatal(err)
	}
	envName := strings.TrimSpace(*envFlag)
	if envName == "" {
		envName = strings.TrimSpace(*scopeAlias)
	}
	fileValue := strings.TrimSpace(*envFile)
	if strings.TrimSpace(*fileAlias) != "" {
		fileValue = strings.TrimSpace(*fileAlias)
	}
	target, err := resolveSIVaultTarget(strings.TrimSpace(*repoFlag), envName, fileValue)
	if err != nil {
		fatal(err)
	}
	doc, err := vault.ReadDotenvFile(target.EnvFile)
	if err != nil {
		fatal(err)
	}
	changed, err := doc.Unset(key)
	if err != nil {
		fatal(err)
	}
	if !changed {
		fmt.Printf("key not found: %s\n", key)
		return
	}
	if err := writeDotenv(target.EnvFile, doc); err != nil {
		fatal(err)
	}
	fmt.Printf("file:   %s\n", filepath.Clean(target.EnvFile))
	fmt.Printf("unset:  %s\n", key)
}
