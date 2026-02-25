package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultKeygen(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault keygen", flag.ExitOnError)
	keyBackend := fs.String("key-backend", "", "deprecated in sun mode; ignored")
	keyFile := fs.String("key-file", "", "deprecated in sun mode; ignored")
	rotate := fs.Bool("rotate", false, "rotate (replace) the existing identity (DANGEROUS: can break decryption of existing ciphertext)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault keygen [--rotate]")
		return
	}
	if strings.TrimSpace(*keyBackend) != "" || strings.TrimSpace(*keyFile) != "" {
		warnf("--key-backend/--key-file are ignored in Sun remote vault mode")
	}

	if *rotate {
		if prev, err := vaultEnsureStrictSunIdentity(settings, "vault_keygen"); err == nil && prev != nil {
			fmt.Printf("previous:  %s\n", strings.TrimSpace(prev.Recipient().String()))
		}
		next, err := vault.GenerateIdentity()
		if err != nil {
			fatal(err)
		}
		if err := vaultPersistIdentityToSun(settings, next, "vault_keygen_rotate"); err != nil {
			fatal(err)
		}
		if err := os.Setenv("SI_VAULT_IDENTITY", strings.TrimSpace(next.String())); err != nil {
			fatal(err)
		}
		fmt.Printf("recipient: %s\n", strings.TrimSpace(next.Recipient().String()))
		fmt.Printf("key:       rotated (sun)\n")
		fmt.Printf("warning:   rotating the identity can permanently break decryption for secrets encrypted to the old recipient unless you have a backup\n")
		return
	}
	info, err := vaultEnsureStrictSunIdentity(settings, "vault_keygen")
	if err != nil {
		fatal(err)
	}
	if info == nil {
		fatal(fmt.Errorf("sun vault identity unavailable"))
	}
	fmt.Printf("recipient: %s\n", strings.TrimSpace(info.Recipient().String()))
	fmt.Printf("key:       ok (sun)\n")
}
