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
	fileFlag := fs.String("file", "", "explicit env file path to bootstrap (overrides --vault-dir)")
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to host git root)")
	ignoreDirty := fs.Bool("ignore-dirty", true, "set ignore=dirty for the vault submodule in .gitmodules")
	installHooks := fs.Bool("hooks", true, "install git pre-commit hook to block plaintext dotenv commits (best effort)")
	keyBackend := fs.String("key-backend", "", "override key backend: keyring, keychain, or file")
	keyFile := fs.String("key-file", "", "override key file path (for key-backend=file)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault init --submodule-url <git-url> [--file <path>] [--vault-dir <path>] [--ignore-dirty]")
		return
	}

	target, err := vaultResolveTarget(settings, *fileFlag, *vaultDir, true, true)
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
				originURL, oerr := vault.GitRemoteOriginURL(target.VaultDir)
				if oerr != nil {
					fatal(err)
				}
				warnf("vault submodule add failed; attempting checkout recovery + submodule re-register: %v", err)
				if recErr := vault.GitEnsureCheckout(target.VaultDir); recErr != nil {
					fatal(fmt.Errorf("%w (recovery failed: %v)", err, recErr))
				}
				branch, berr := vault.GitCurrentBranch(target.VaultDir)
				if berr != nil || strings.TrimSpace(branch) == "" {
					branch = "main"
				}
				if regErr := vault.GitSubmoduleAddForce(target.RepoRoot, originURL, target.VaultDirRel, branch); regErr != nil {
					fatal(fmt.Errorf("%w (recovery failed: %v)", err, regErr))
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
	keyBackendOverride := strings.TrimSpace(*keyBackend)
	keyFileOverride := strings.TrimSpace(*keyFile)
	keyCfg := vaultKeyConfigFromSettings(settings)
	if keyBackendOverride != "" {
		keyCfg.Backend = keyBackendOverride
	}
	if keyFileOverride != "" {
		keyCfg.KeyFile = keyFileOverride
	}
	if vault.NormalizeKeyBackend(keyCfg.Backend) == "keyring" && !isInteractiveTerminal() {
		if strings.TrimSpace(os.Getenv("SI_VAULT_IDENTITY")) == "" &&
			strings.TrimSpace(os.Getenv("SI_VAULT_PRIVATE_KEY")) == "" &&
			strings.TrimSpace(os.Getenv("SI_VAULT_IDENTITY_FILE")) == "" {
			fatal(fmt.Errorf("non-interactive: refusing to access OS keychain/keyring (set SI_VAULT_IDENTITY/SI_VAULT_IDENTITY_FILE or use --key-backend file)"))
		}
	}
	identityInfo, createdKey, err := vault.EnsureIdentity(keyCfg)
	if err != nil {
		fatal(err)
	}
	recipient := identityInfo.Identity.Recipient().String()
	if keyBackendOverride != "" || keyFileOverride != "" {
		if keyBackendOverride != "" {
			settings.Vault.KeyBackend = keyBackendOverride
		}
		if keyFileOverride != "" {
			settings.Vault.KeyFile = keyFileOverride
		}
		if err := saveSettings(settings); err != nil {
			fatal(err)
		}
	}

	// Read or create the env file.
	var doc vault.DotenvFile
	fileMissing := false
	doc, err = vault.ReadDotenvFile(target.File)
	if err != nil {
		if !os.IsNotExist(err) {
			fatal(err)
		}
		doc = vault.ParseDotenv(nil)
		fileMissing = true
	}
	changed, err := vault.EnsureVaultHeader(&doc, []string{recipient})
	if err != nil {
		fatal(err)
	}
	if changed || fileMissing {
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
		File:        target.File,
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
	if *installHooks {
		// Best-effort: hooks are local-only, but they prevent accidental plaintext commits during day-to-day work.
		if hooksDir, err := vault.GitHooksDir(target.VaultDir); err == nil {
			hookPath := filepath.Join(hooksDir, "pre-commit")
			exe, _ := os.Executable()
			script := renderVaultPreCommitHook(exe)
			if err := writeHookFile(hookPath, script, false); err != nil {
				warnf("hooks: not installed (%v) (run `si vault hooks install --force`)", err)
			} else {
				fmt.Printf("hooks:     installed (%s)\n", filepath.Clean(hookPath))
			}
		} else {
			warnf("hooks: not installed (%v) (run `si vault hooks install`)", err)
		}
	}
}
