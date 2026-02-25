package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func cmdVaultStatus(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault status", flag.ExitOnError)
	fileFlag := fs.String("file", "", "vault scope (preferred: --scope)")
	scopeFlag := fs.String("scope", "", "vault scope")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault status [--scope <name>]")
		return
	}
	scope := strings.TrimSpace(*scopeFlag)
	if scope == "" {
		scope = strings.TrimSpace(*fileFlag)
	}

	target, err := vaultResolveTargetStatus(settings, scope)
	if err != nil {
		fatal(err)
	}
	if shouldEnforceVaultRepoScope(settings) {
		if err := vaultValidateImplicitTargetRepoScope(target); err != nil {
			warnf("%v", err)
		}
	}

	fmt.Printf("scope:     %s\n", strings.TrimSpace(target.File))
	fmt.Printf("trust:     n/a (sun-managed)\n")
	fmt.Printf("local:     disabled\n")
	failed := false
	if identity, err := vaultEnsureStrictSunIdentity(settings, "vault_status"); err != nil {
		fmt.Printf("key:       error (%v)\n", err)
		failed = true
	} else if identity == nil {
		fmt.Printf("key:       unavailable\n")
		failed = true
	} else {
		fmt.Printf("key:       ok (sun identity)\n")
	}
	if values, used, sunErr := vaultSunKVLoadRawValues(settings, target); sunErr != nil {
		fmt.Printf("cloud_kv:  error (%v)\n", sunErr)
		failed = true
	} else if used {
		fmt.Printf("cloud_kv:  ok (%d keys)\n", len(values))
	} else {
		fmt.Printf("cloud_kv:  unavailable\n")
		failed = true
	}
	if failed {
		os.Exit(1)
	}
}
