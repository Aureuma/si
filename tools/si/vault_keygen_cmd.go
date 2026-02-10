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
	keyBackend := fs.String("key-backend", "", "override key backend: keyring, keychain, or file")
	keyFile := fs.String("key-file", "", "override key file path (for key-backend=file)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault keygen [--key-backend <keyring|keychain|file>] [--key-file <path>]")
		return
	}

	keyCfg := vaultKeyConfigFromSettings(settings)
	if strings.TrimSpace(*keyBackend) != "" {
		keyCfg.Backend = strings.TrimSpace(*keyBackend)
	}
	if strings.TrimSpace(*keyFile) != "" {
		keyCfg.KeyFile = strings.TrimSpace(*keyFile)
	}

	// If the selected backend is the OS secure store, avoid file fallback unless
	// the user explicitly asked for it.
	switch vault.NormalizeKeyBackend(keyCfg.Backend) {
	case "keyring":
		if strings.TrimSpace(*keyFile) == "" && strings.TrimSpace(os.Getenv("SI_VAULT_KEY_FILE")) == "" {
			keyCfg.KeyFile = ""
		}
	}

	info, created, err := vault.EnsureIdentity(keyCfg)
	if err != nil {
		fatal(err)
	}

	recipient := info.Identity.Recipient().String()
	fmt.Printf("recipient: %s\n", recipient)
	if created {
		fmt.Printf("key:       created (%s)\n", info.Source)
	} else {
		fmt.Printf("key:       ok (%s)\n", info.Source)
	}
	if strings.TrimSpace(info.Path) != "" {
		fmt.Printf("path:      %s\n", strings.TrimSpace(info.Path))
	}
}
