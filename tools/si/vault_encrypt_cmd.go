package main

import (
	"flag"
	"fmt"
	"path/filepath"

	"filippo.io/age"
	"si/tools/si/internal/vault"
)

func cmdVaultEncrypt(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault encrypt", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path (overrides --vault-dir)")
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to host git root)")
	format := fs.Bool("format", false, "run `si vault fmt` after encrypting")
	reencrypt := fs.Bool("reencrypt", false, "re-encrypt already-encrypted values (intentional git noise)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault encrypt [--file <path>] [--vault-dir <path>] [--format] [--reencrypt]")
		return
	}

	target, err := vaultResolveTarget(settings, *fileFlag, *vaultDir, false, false)
	if err != nil {
		fatal(err)
	}
	doc, err := vault.ReadDotenvFile(target.File)
	if err != nil {
		fatal(err)
	}
	if _, err := vaultRequireTrusted(settings, target, doc); err != nil {
		fatal(err)
	}

	var identity *age.X25519Identity
	if *reencrypt {
		if err := vaultRefuseNonInteractiveOSKeyring(vaultKeyConfigFromSettings(settings)); err != nil {
			fatal(err)
		}
		info, err := vault.LoadIdentity(vaultKeyConfigFromSettings(settings))
		if err != nil {
			fatal(err)
		}
		identity = info.Identity
	}

	res, err := vault.EncryptDotenvValues(&doc, identity, *reencrypt)
	if err != nil {
		fatal(err)
	}
	if res.Changed {
		if err := vault.WriteDotenvFileAtomic(target.File, doc.Bytes()); err != nil {
			fatal(err)
		}
	}
	if *format {
		formatted, changed, err := vault.FormatVaultDotenv(doc)
		if err != nil {
			fatal(err)
		}
		if changed {
			if err := vault.WriteDotenvFileAtomic(target.File, formatted.Bytes()); err != nil {
				fatal(err)
			}
		}
	}

	vaultAuditEvent(settings, target, "encrypt", map[string]any{
		"envFile":          filepath.Clean(target.File),
		"reencrypt":        *reencrypt,
		"encryptedCount":   len(res.EncryptedKeys),
		"reencryptedCount": len(res.ReencryptedKeys),
		"skippedEncrypted": res.SkippedEncrypted,
	})

	fmt.Printf("file: %s\n", filepath.Clean(target.File))
	fmt.Printf("encrypted: %d\n", len(res.EncryptedKeys))
	if *reencrypt {
		fmt.Printf("reencrypted: %d\n", len(res.ReencryptedKeys))
	}
}
