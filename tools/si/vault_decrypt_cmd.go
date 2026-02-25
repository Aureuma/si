package main

import (
	"flag"
	"fmt"
	"sort"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultDecrypt(args []string) {
	settings := loadSettingsOrDefault()
	args = stripeFlagsFirst(args, map[string]bool{"in-place": true, "yes": true, "stdout": true})
	fs := flag.NewFlagSet("vault decrypt", flag.ExitOnError)
	var files multiFlag
	fs.Var(&files, "file", "vault scope (repeatable; preferred: --scope)")
	scopeFlag := fs.String("scope", "", "vault scope")
	inPlace := fs.Bool("in-place", false, "unsupported in Sun remote vault mode")
	yes := fs.Bool("yes", false, "accepted for compatibility")
	// Default remains stdout. This flag is retained for compatibility.
	stdout := fs.Bool("stdout", false, "write decrypted values to stdout (default)")
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
	// --stdout is accepted but redundant; if both are set, stdout wins.
	if *stdout {
		modeStdout = true
	}
	if !modeStdout {
		_ = yes
		fatal(fmt.Errorf("vault decrypt --in-place is not supported in Sun remote vault mode (no local vault file)"))
	}
	if scope := strings.TrimSpace(*scopeFlag); scope != "" {
		files = append(files, scope)
	}

	if modeStdout && len(files) > 1 {
		fatal(fmt.Errorf("stdout mode does not support multiple --file values"))
	}
	identity, err := vaultEnsureStrictSunIdentity(settings, "vault_decrypt")
	if err != nil {
		fatal(err)
	}
	if identity == nil {
		fatal(fmt.Errorf("sun vault identity unavailable"))
	}

	runOne := func(scope string) {
		target, err := vaultResolveTarget(settings, scope, false)
		if err != nil {
			fatal(err)
		}
		values, used, sunErr := vaultSunKVLoadRawValues(settings, target)
		if sunErr != nil {
			fatal(sunErr)
		}
		if !used {
			fatal(fmt.Errorf("sun vault unavailable: run `si sun auth login --url <url> --token <token> --account <slug>`"))
		}

		selected := make([]string, 0)
		if len(keys) == 0 {
			for key := range values {
				selected = append(selected, key)
			}
		} else {
			selected = append(selected, keys...)
		}
		sort.Strings(selected)

		lines := make([]string, 0, len(selected))
		decryptedCount := 0
		missingCount := 0
		for _, key := range selected {
			raw, ok := values[key]
			if !ok {
				missingCount++
				continue
			}
			val, normErr := vault.NormalizeDotenvValue(raw)
			if normErr != nil {
				fatal(fmt.Errorf("normalize %s: %w", key, normErr))
			}
			plain := val
			if vault.IsEncryptedValueV1(val) {
				dec, decErr := vault.DecryptStringV1(val, identity)
				if decErr != nil {
					fatal(fmt.Errorf("decrypt %s: %w", key, decErr))
				}
				plain = dec
				decryptedCount++
			}
			lines = append(lines, key+"="+vault.RenderDotenvValuePlain(plain))
		}

		vaultAuditEvent(settings, target, "decrypt_stdout", map[string]any{
			"scope":          strings.TrimSpace(target.File),
			"decryptedCount": decryptedCount,
			"keyCount":       len(selected),
			"missingCount":   missingCount,
			"source":         "sun-kv",
		})
		if len(lines) == 0 {
			return
		}
		fmt.Print(strings.Join(lines, "\n"))
		fmt.Print("\n")
	}

	if len(files) == 0 {
		runOne("")
		return
	}
	for _, file := range files {
		runOne(file)
	}
}
