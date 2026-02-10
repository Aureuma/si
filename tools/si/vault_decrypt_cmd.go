package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"si/tools/si/internal/vault"
)

func cmdVaultDecrypt(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault decrypt", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path (overrides --vault-dir)")
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to host git root)")
	yes := fs.Bool("yes", false, "do not prompt (required for in-place decrypt in non-interactive mode)")
	stdout := fs.Bool("stdout", false, "write decrypted file to stdout (does not modify the file)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault decrypt [--file <path>] [--vault-dir <path>] [--stdout] [--yes]")
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
	if err := vaultRefuseNonInteractiveOSKeyring(vaultKeyConfigFromSettings(settings)); err != nil {
		fatal(err)
	}
	info, err := vault.LoadIdentity(vaultKeyConfigFromSettings(settings))
	if err != nil {
		fatal(err)
	}

	working := doc
	res, err := vault.DecryptDotenvValues(&working, info.Identity)
	if err != nil {
		fatal(err)
	}

	if *stdout {
		vaultAuditEvent(settings, target, "decrypt_stdout", map[string]any{
			"envFile":        filepath.Clean(target.File),
			"decryptedCount": len(res.DecryptedKeys),
		})
		_, _ = os.Stdout.Write(working.Bytes())
		return
	}

	// In-place decrypt is dangerous: it writes plaintext secrets to disk.
	if !res.Changed {
		fmt.Printf("file: %s\n", filepath.Clean(target.File))
		fmt.Printf("decrypted: %d\n", len(res.DecryptedKeys))
		return
	}
	if !*yes {
		confirmed, ok := confirmYN("Decrypt in place to plaintext? This will write secrets to disk.", false)
		if !ok {
			fatal(fmt.Errorf("non-interactive: re-run with --yes, or use --stdout"))
		}
		if !confirmed {
			fatal(fmt.Errorf("canceled"))
		}
	}

	if err := vault.WriteDotenvFileAtomic(target.File, working.Bytes()); err != nil {
		fatal(err)
	}
	vaultAuditEvent(settings, target, "decrypt_inplace", map[string]any{
		"envFile":        filepath.Clean(target.File),
		"decryptedCount": len(res.DecryptedKeys),
	})
	fmt.Printf("file: %s\n", filepath.Clean(target.File))
	fmt.Printf("decrypted: %d\n", len(res.DecryptedKeys))
}
