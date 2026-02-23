package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultDecrypt(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault decrypt", flag.ExitOnError)
	var files multiFlag
	fs.Var(&files, "file", "explicit env file path (repeatable; defaults to the configured vault.file when omitted)")
	inPlace := fs.Bool("in-place", false, "decrypt in place to plaintext on disk (DANGEROUS)")
	yes := fs.Bool("yes", false, "do not prompt (required for in-place decrypt in non-interactive mode)")
	// Default is stdout. This flag remains for compatibility and explicitness.
	stdout := fs.Bool("stdout", false, "write decrypted file to stdout (default; does not modify the file)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	rawKeys := fs.Args()
	keys := make([]string, 0, len(rawKeys))
	for _, k := range rawKeys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if err := vault.ValidateKeyName(k); err != nil {
			fatal(err)
		}
		keys = append(keys, k)
	}

	modeStdout := true
	if *inPlace {
		modeStdout = false
	}
	// --stdout is accepted but is redundant; if both are set, stdout wins.
	if *stdout {
		modeStdout = true
	}

	if modeStdout && len(files) > 1 {
		fatal(fmt.Errorf("stdout mode does not support multiple --file values (use --in-place for multi-file)"))
	}

	if err := vaultEnsureSunIdentityEnv(settings, "vault_decrypt"); err != nil {
		fatal(err)
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
		// If positional args are provided, only decrypt those keys.
		// Example:
		//   si vault decrypt --stdout STRIPE_API_KEY
		res, err := vault.DecryptDotenvKeys(&working, info.Identity, keys)
		if err != nil {
			fatal(err)
		}

		if modeStdout {
			vaultAuditEvent(settings, target, "decrypt_stdout", map[string]any{
				"envFile":        filepath.Clean(target.File),
				"decryptedCount": len(res.DecryptedKeys),
				"keyCount":       len(keys),
				"missingCount":   res.SkippedMissing,
			})
			_, _ = os.Stdout.Write(working.Bytes())
			return
		}

		// In-place decrypt is dangerous: it writes plaintext secrets to disk.
		if res.Changed {
			if err := vaultWriteDotenvFileAtomic(target.File, working.Bytes()); err != nil {
				fatal(err)
			}
		}
		vaultAuditEvent(settings, target, "decrypt_inplace", map[string]any{
			"envFile":        filepath.Clean(target.File),
			"decryptedCount": len(res.DecryptedKeys),
			"keyCount":       len(keys),
			"missingCount":   res.SkippedMissing,
		})
		fmt.Printf("file: %s\n", filepath.Clean(target.File))
		fmt.Printf("decrypted: %d\n", len(res.DecryptedKeys))
	}

	// If doing an in-place decrypt for multiple files, confirm once up-front.
	if !modeStdout && len(files) > 1 && !*yes {
		prompt := fmt.Sprintf("Decrypt %d files in place to plaintext? This will write secrets to disk.", len(files))
		if len(keys) > 0 {
			prompt = fmt.Sprintf("Decrypt selected keys in %d files in place to plaintext? This will write secrets to disk.", len(files))
		}
		confirmed, ok := confirmYN(prompt, false)
		if !ok {
			fatal(fmt.Errorf("non-interactive: re-run with --in-place --yes"))
		}
		if !confirmed {
			fatal(fmt.Errorf("canceled"))
		}
	}

	if len(files) == 0 {
		if !modeStdout && !*yes {
			prompt := "Decrypt in place to plaintext? This will write secrets to disk."
			if len(keys) > 0 {
				prompt = "Decrypt selected keys in place to plaintext? This will write secrets to disk."
			}
			confirmed, ok := confirmYN(prompt, false)
			if !ok {
				fatal(fmt.Errorf("non-interactive: re-run with --in-place --yes"))
			}
			if !confirmed {
				fatal(fmt.Errorf("canceled"))
			}
		}
		runOne("")
		return
	}

	if !modeStdout && len(files) == 1 && !*yes {
		prompt := "Decrypt in place to plaintext? This will write secrets to disk."
		if len(keys) > 0 {
			prompt = "Decrypt selected keys in place to plaintext? This will write secrets to disk."
		}
		confirmed, ok := confirmYN(prompt, false)
		if !ok {
			fatal(fmt.Errorf("non-interactive: re-run with --in-place --yes"))
		}
		if !confirmed {
			fatal(fmt.Errorf("canceled"))
		}
	}
	for _, file := range files {
		runOne(file)
	}
}
