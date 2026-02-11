package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultStatus(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault status", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path (defaults to the configured vault.file)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault status [--file <path>]")
		return
	}

	target, err := vaultResolveTarget(settings, strings.TrimSpace(*fileFlag), true)
	if err != nil {
		fatal(err)
	}

	if strings.TrimSpace(target.RepoRoot) == "" {
		fmt.Printf("repo root: (none)\n")
	} else {
		fmt.Printf("repo root: %s\n", filepath.Clean(target.RepoRoot))
	}
	fmt.Printf("env file:  %s\n", filepath.Clean(target.File))

	// Trust + recipients fingerprint.
	if doc, err := vault.ReadDotenvFile(target.File); err == nil {
		fp, fpErr := vaultTrustFingerprint(doc)
		if fpErr == nil {
			store, storeErr := vault.LoadTrustStore(vaultTrustStorePath(settings))
			if storeErr != nil {
				fmt.Printf("trust:     error (%v)\n", storeErr)
			} else if entry, ok := store.Find(target.RepoRoot, target.File); !ok {
				fmt.Printf("trust:     untrusted (%s)\n", fp)
			} else if strings.TrimSpace(entry.Fingerprint) != fp {
				fmt.Printf("trust:     mismatch (stored %s, current %s)\n", strings.TrimSpace(entry.Fingerprint), fp)
			} else {
				fmt.Printf("trust:     ok (%s)\n", fp)
			}
		} else {
			fmt.Printf("trust:     unavailable (%v)\n", fpErr)
		}
	} else if os.IsNotExist(err) {
		fmt.Printf("trust:     unavailable (env file missing)\n")
	} else if err != nil {
		fmt.Printf("trust:     error (%v)\n", err)
	}

	// Identity status (no secrets printed).
	keyCfg := vaultKeyConfigFromSettings(settings)
	backend := vault.NormalizeKeyBackend(keyCfg.Backend)
	if err := vaultRefuseNonInteractiveOSKeyring(keyCfg); err != nil {
		fmt.Printf("key:       unavailable (backend=%s)\n", backend)
		return
	}
	info, err := vault.LoadIdentity(keyCfg)
	if err == nil {
		// Always show the configured backend; optionally add source detail when it differs.
		if info.Source != "" && info.Source != backend {
			fmt.Printf("key:       ok (backend=%s, source=%s)\n", backend, strings.TrimSpace(info.Source))
		} else {
			fmt.Printf("key:       ok (backend=%s)\n", backend)
		}
		return
	}
	fmt.Printf("key:       missing (backend=%s)\n", backend)
}
