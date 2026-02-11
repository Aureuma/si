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
	var files multiFlag
	fs.Var(&files, "file", "explicit env file path (repeatable; defaults to the configured vault.file when omitted)")
	yes := fs.Bool("yes", false, "do not prompt (required for in-place decrypt in non-interactive mode)")
	stdout := fs.Bool("stdout", false, "write decrypted file to stdout (does not modify the file)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault decrypt [--file <path>]... [--stdout] [--yes]")
		return
	}

	if *stdout && len(files) > 1 {
		fatal(fmt.Errorf("--stdout does not support multiple --file values"))
	}

	if err := vaultRefuseNonInteractiveOSKeyring(vaultKeyConfigFromSettings(settings)); err != nil {
		fatal(err)
	}
	info, err := vault.LoadIdentity(vaultKeyConfigFromSettings(settings))
	if err != nil {
		fatal(err)
	}

	runOne := func(file string) {
		target, err := vaultResolveTarget(settings, file, false)
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
		if res.Changed {
			if err := vault.WriteDotenvFileAtomic(target.File, working.Bytes()); err != nil {
				fatal(err)
			}
		}
		vaultAuditEvent(settings, target, "decrypt_inplace", map[string]any{
			"envFile":        filepath.Clean(target.File),
			"decryptedCount": len(res.DecryptedKeys),
		})
		fmt.Printf("file: %s\n", filepath.Clean(target.File))
		fmt.Printf("decrypted: %d\n", len(res.DecryptedKeys))
	}

	// If doing an in-place decrypt for multiple files, confirm once up-front.
	if !*stdout && len(files) > 1 && !*yes {
		confirmed, ok := confirmYN(fmt.Sprintf("Decrypt %d files in place to plaintext? This will write secrets to disk.", len(files)), false)
		if !ok {
			fatal(fmt.Errorf("non-interactive: re-run with --yes, or use --stdout"))
		}
		if !confirmed {
			fatal(fmt.Errorf("canceled"))
		}
	}

	if len(files) == 0 {
		if !*stdout && !*yes {
			confirmed, ok := confirmYN("Decrypt in place to plaintext? This will write secrets to disk.", false)
			if !ok {
				fatal(fmt.Errorf("non-interactive: re-run with --yes, or use --stdout"))
			}
			if !confirmed {
				fatal(fmt.Errorf("canceled"))
			}
		}
		runOne("")
		return
	}

	if !*stdout && len(files) == 1 && !*yes {
		confirmed, ok := confirmYN("Decrypt in place to plaintext? This will write secrets to disk.", false)
		if !ok {
			fatal(fmt.Errorf("non-interactive: re-run with --yes, or use --stdout"))
		}
		if !confirmed {
			fatal(fmt.Errorf("canceled"))
		}
	}
	for _, file := range files {
		runOne(file)
	}
}
