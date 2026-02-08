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
	fileFlag := fs.String("file", "", "explicit env file path (overrides --vault-dir/--env)")
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to host git root)")
	env := fs.String("env", settings.Vault.DefaultEnv, "environment name (maps to .env.<env>)")
	fs.Parse(args)
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault status [--vault-dir <path>] [--env <name>]")
		return
	}

	target, err := vaultResolveTarget(settings, *fileFlag, *vaultDir, *env, true, true)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("repo root: %s\n", filepath.Clean(target.RepoRoot))
	fmt.Printf("vault dir: %s\n", filepath.Clean(target.VaultDir))
	fmt.Printf("env:      %s\n", strings.TrimSpace(target.Env))
	fmt.Printf("env file: %s\n", filepath.Clean(target.File))

	if target.RepoRoot != "" && target.VaultDirRel != "" {
		if sm, err := vault.GitSubmoduleStatus(target.RepoRoot, target.VaultDirRel); err == nil && sm != nil && sm.Present {
			fmt.Printf("submodule: %s%s %s%s\n", sm.Prefix, sm.Commit, sm.Path, strings.TrimSpace(sm.Meta))
		}
	}

	if vault.IsDir(target.VaultDir) {
		if url, err := vault.GitRemoteOriginURL(target.VaultDir); err == nil && strings.TrimSpace(url) != "" {
			fmt.Printf("vault url: %s\n", strings.TrimSpace(url))
		}
		if head, err := vault.GitHeadCommit(target.VaultDir); err == nil && strings.TrimSpace(head) != "" {
			fmt.Printf("vault head: %s\n", strings.TrimSpace(head))
		}
		if dirty, err := vault.GitDirty(target.VaultDir); err == nil {
			if dirty {
				fmt.Printf("vault dirty: yes\n")
			} else {
				fmt.Printf("vault dirty: no\n")
			}
		}
	}

	// Trust + recipients fingerprint.
	if data, err := os.ReadFile(target.File); err == nil {
		doc := vault.ParseDotenv(data)
		fp, fpErr := vaultTrustFingerprint(doc)
		if fpErr == nil {
			store, storeErr := vault.LoadTrustStore(vaultTrustStorePath(settings))
			if storeErr != nil {
				fmt.Printf("trust:     error (%v)\n", storeErr)
			} else if entry, ok := store.Find(target.RepoRoot, target.VaultDir, target.Env); !ok {
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
	if info, err := vault.LoadIdentity(keyCfg); err == nil {
		fmt.Printf("key:       ok (%s)\n", info.Source)
	} else {
		fmt.Printf("key:       missing (%v)\n", err)
	}
}
