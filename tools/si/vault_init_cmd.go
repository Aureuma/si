package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultInit(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault init", flag.ExitOnError)
	submoduleURL := fs.String("submodule-url", "", "git URL for the vault repo (submodule)")
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to host git root)")
	env := fs.String("env", settings.Vault.DefaultEnv, "environment name (maps to .env.<env>)")
	ignoreDirty := fs.Bool("ignore-dirty", true, "set ignore=dirty for the vault submodule in .gitmodules")
	keyBackend := fs.String("key-backend", "", "override key backend: keyring or file")
	keyFile := fs.String("key-file", "", "override key file path (for key-backend=file)")
	fs.Parse(args)
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault init --submodule-url <git-url> [--vault-dir <path>] [--ignore-dirty] [--env <name>]")
		return
	}

	target, err := vaultResolveTarget(settings, "", *vaultDir, *env, true, true)
	if err != nil {
		fatal(err)
	}

	// Bootstrap the vault dir as a submodule when missing.
	if !vault.IsDir(target.VaultDir) {
		if strings.TrimSpace(*submoduleURL) == "" {
			fatal(fmt.Errorf("vault dir not found (%s); provide --submodule-url to add it as a git submodule", filepath.Clean(target.VaultDir)))
		}
		if err := vault.GitSubmoduleAdd(target.RepoRoot, *submoduleURL, target.VaultDirRel); err != nil {
			// A common failure mode for brand-new repos is a missing/invalid remote HEAD.
			// Git may clone the repo but fail to checkout, returning an error while leaving
			// a usable git working directory behind.
			if vault.IsDir(target.VaultDir) {
				warnf("vault submodule add failed; attempting checkout recovery: %v", err)
				if recErr := vault.GitEnsureCheckout(target.VaultDir); recErr != nil {
					fatal(fmt.Errorf("%w (recovery failed: %v)", err, recErr))
				}
			} else {
				fatal(err)
			}
		}
	}

	// Ensure the submodule checkout exists (when this vault dir is configured as a submodule).
	if sm, err := vault.GitSubmoduleStatus(target.RepoRoot, target.VaultDirRel); err == nil && sm != nil && sm.Present {
		if err := vault.GitSubmoduleUpdate(target.RepoRoot, target.VaultDirRel); err != nil {
			fatal(err)
		}
	}
	if vault.IsDir(target.VaultDir) {
		if err := vault.GitEnsureCheckout(target.VaultDir); err != nil {
			fatal(fmt.Errorf("vault repo checkout failed: %w", err))
		}
	}
	if *ignoreDirty {
		_, _ = vault.EnsureGitmodulesIgnoreDirty(target.RepoRoot, target.VaultDirRel)
	}

	// Ensure we have a device identity and public recipient.
	keyCfg := vaultKeyConfigFromSettings(settings)
	if strings.TrimSpace(*keyBackend) != "" {
		keyCfg.Backend = strings.TrimSpace(*keyBackend)
	}
	if strings.TrimSpace(*keyFile) != "" {
		keyCfg.KeyFile = strings.TrimSpace(*keyFile)
	}
	identityInfo, createdKey, err := vault.EnsureIdentity(keyCfg)
	if err != nil {
		fatal(err)
	}
	recipient := identityInfo.Identity.Recipient().String()

	// Read or create the env file.
	var doc vault.DotenvFile
	data, err := os.ReadFile(target.File)
	if err != nil {
		if !os.IsNotExist(err) {
			fatal(err)
		}
		doc = vault.ParseDotenv(nil)
	} else {
		doc = vault.ParseDotenv(data)
	}
	changed, err := vault.EnsureVaultHeader(&doc, []string{recipient})
	if err != nil {
		fatal(err)
	}
	if changed || len(data) == 0 {
		if err := vault.WriteDotenvFileAtomic(target.File, doc.Bytes()); err != nil {
			fatal(err)
		}
	}

	// Trust the current recipient set (TOFU) for this repo/vault/env.
	trustPath := vaultTrustStorePath(settings)
	store, err := vault.LoadTrustStore(trustPath)
	if err != nil {
		fatal(err)
	}
	fp, err := vaultTrustFingerprint(doc)
	if err != nil {
		fatal(err)
	}
	store.Upsert(vault.TrustEntry{
		RepoRoot:    target.RepoRoot,
		VaultDir:    target.VaultDir,
		Env:         target.Env,
		VaultRepo:   vaultRepoURL(target),
		Fingerprint: fp,
	})
	if err := store.Save(trustPath); err != nil {
		fatal(err)
	}

	vaultAuditEvent(settings, target, "init", map[string]any{
		"envFile":    filepath.Clean(target.File),
		"recipient":  recipient,
		"trustFp":    fp,
		"keyCreated": createdKey,
		"keySource":  identityInfo.Source,
	})

	fmt.Printf("vault dir: %s\n", filepath.Clean(target.VaultDir))
	fmt.Printf("env file:  %s\n", filepath.Clean(target.File))
	fmt.Printf("recipient: %s\n", recipient)
	fmt.Printf("trust fp:  %s\n", fp)
	if createdKey {
		fmt.Printf("key:       created (%s)\n", identityInfo.Source)
	} else {
		fmt.Printf("key:       ok (%s)\n", identityInfo.Source)
	}
}
