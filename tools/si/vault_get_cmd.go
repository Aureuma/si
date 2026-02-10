package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultGet(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault get", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path (overrides --vault-dir/--env)")
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to host git root)")
	env := fs.String("env", settings.Vault.DefaultEnv, "environment name (maps to .env.<env>)")
	reveal := fs.Bool("reveal", false, "print the decrypted value to stdout")
	fs.Parse(args)

	rest := fs.Args()
	if len(rest) != 1 {
		printUsage("usage: si vault get <KEY> [--vault-dir <path>] [--env <name>] [--reveal]")
		return
	}
	key := strings.TrimSpace(rest[0])
	if err := vault.ValidateKeyName(key); err != nil {
		fatal(err)
	}

	target, err := vaultResolveTarget(settings, *fileFlag, *vaultDir, *env, false, false)
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
	raw, ok := doc.Lookup(key)
	if !ok {
		fatal(fmt.Errorf("key not found: %s", key))
	}
	val, err := vault.NormalizeDotenvValue(raw)
	if err != nil {
		fatal(err)
	}
	if vault.IsEncryptedValueV1(val) {
		if !*reveal {
			fmt.Printf("file: %s\n", filepath.Clean(target.File))
			fmt.Printf("%s: encrypted (use --reveal)\n", key)
			return
		}
		info, err := vault.LoadIdentity(vaultKeyConfigFromSettings(settings))
		if err != nil {
			fatal(err)
		}
		plain, err := vault.DecryptStringV1(val, info.Identity)
		if err != nil {
			fatal(err)
		}
		vaultAuditEvent(settings, target, "reveal", map[string]any{
			"envFile":   filepath.Clean(target.File),
			"key":       key,
			"encrypted": true,
		})
		fmt.Print(plain)
		if !strings.HasSuffix(plain, "\n") {
			fmt.Print("\n")
		}
		return
	}

	if !*reveal {
		fmt.Printf("file: %s\n", filepath.Clean(target.File))
		fmt.Printf("%s: plaintext (run `si vault encrypt` to encrypt)\n", key)
		return
	}
	vaultAuditEvent(settings, target, "reveal", map[string]any{
		"envFile":   filepath.Clean(target.File),
		"key":       key,
		"encrypted": false,
	})
	fmt.Print(val)
	if !strings.HasSuffix(val, "\n") {
		fmt.Print("\n")
	}
}
