package main

import (
	"errors"
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
	rotate := fs.Bool("rotate", false, "rotate (replace) the existing identity (DANGEROUS: can break decryption of existing ciphertext)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault keygen [--key-backend <keyring|keychain|file>] [--key-file <path>] [--rotate]")
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

	// Avoid hanging on OS keychain/keyring prompts in non-interactive contexts.
	// If you want non-interactive decryption, prefer SI_VAULT_IDENTITY(_FILE) or file backend.
	if vault.NormalizeKeyBackend(keyCfg.Backend) == "keyring" && !isInteractiveTerminal() {
		if strings.TrimSpace(os.Getenv("SI_VAULT_IDENTITY")) == "" &&
			strings.TrimSpace(os.Getenv("SI_VAULT_PRIVATE_KEY")) == "" &&
			strings.TrimSpace(os.Getenv("SI_VAULT_IDENTITY_FILE")) == "" {
			fatal(fmt.Errorf("non-interactive: refusing to access OS keychain/keyring (use --key-backend file, or set SI_VAULT_IDENTITY/SI_VAULT_IDENTITY_FILE, or run interactively)"))
		}
	}

	var info *vault.IdentityInfo
	created := false
	if *rotate {
		// Best-effort: show the previous recipient to make rotation more explicit.
		if prev, err := vault.LoadIdentity(keyCfg); err == nil {
			fmt.Printf("previous:  %s\n", prev.Identity.Recipient().String())
		}
		next, err := vault.RotateIdentity(keyCfg)
		if err != nil {
			fatal(err)
		}
		info = next
		created = true
	} else {
		var err error
		info, created, err = vault.EnsureIdentity(keyCfg)
		if err != nil {
			if errors.Is(err, vault.ErrIdentityInvalid) {
				fatal(fmt.Errorf("%w\n\nRefusing to overwrite an existing invalid identity.\nFix: delete the keychain/keyring entry (%q/%q) or run `si vault keygen --rotate` to replace it.\nWARNING: rotating will prevent decrypting secrets encrypted to the old recipient unless you have a backup.", err, vault.KeyringService, vault.KeyringAccount))
			}
			fatal(err)
		}
	}

	recipient := info.Identity.Recipient().String()
	fmt.Printf("recipient: %s\n", recipient)
	if *rotate {
		fmt.Printf("key:       rotated (%s)\n", info.Source)
		fmt.Printf("warning:   rotating the identity can permanently break decryption for secrets encrypted to the old recipient unless you have a backup\n")
	} else if created {
		fmt.Printf("key:       created (%s)\n", info.Source)
		fmt.Printf("warning:   losing this identity will prevent decrypting secrets encrypted to this recipient\n")
	} else {
		fmt.Printf("key:       ok (%s)\n", info.Source)
	}
	if strings.TrimSpace(info.Path) != "" {
		fmt.Printf("path:      %s\n", strings.TrimSpace(info.Path))
	}
}
