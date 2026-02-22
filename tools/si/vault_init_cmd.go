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
	fileFlag := fs.String("file", "", "env file path to bootstrap")
	setDefault := fs.Bool("set-default", false, "set the target file as vault.file in settings")
	keyBackend := fs.String("key-backend", "", "override key backend: keyring, keychain, or file")
	keyFile := fs.String("key-file", "", "override key file path (for key-backend=file)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault init [--file <path>] [--set-default] [--key-backend <keyring|keychain|file>] [--key-file <path>]")
		return
	}

	// Resolve target file (may not exist yet).
	target, err := vaultResolveTarget(settings, strings.TrimSpace(*fileFlag), true)
	if err != nil {
		fatal(err)
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
		if err := os.MkdirAll(filepath.Dir(target.File), 0o700); err != nil {
			fatal(err)
		}
		if err := vaultWriteDotenvFileAtomic(target.File, doc.Bytes()); err != nil {
			fatal(err)
		}
	}

	// Trust the current recipient set (TOFU) for this repo/file.
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
		File:        target.File,
		Fingerprint: fp,
	})
	if err := store.Save(trustPath); err != nil {
		fatal(err)
	}

	// Persist settings changes (default file and key overrides, when provided).
	settingsChanged := false
	fileFlagProvided := strings.TrimSpace(*fileFlag) != ""
	defaults := defaultSettings()
	applySettingsDefaults(&defaults)
	defaultVaultPathAbs, defaultVaultPathErr := vault.CleanAbs(defaults.Vault.File)
	if cur := strings.TrimSpace(settings.Vault.File); cur == "" {
		settings.Vault.File = target.File
		settingsChanged = true
	} else if fileFlagProvided {
		curAbs, curErr := vault.CleanAbs(cur)
		curIsImplicitDefault := curErr == nil && defaultVaultPathErr == nil && filepath.Clean(curAbs) == filepath.Clean(defaultVaultPathAbs)
		curMissing := true
		if curErr == nil {
			if info, statErr := os.Stat(curAbs); statErr == nil && info.Mode().IsRegular() {
				curMissing = false
			}
		}
		if curIsImplicitDefault && curMissing {
			settings.Vault.File = target.File
			settingsChanged = true
		}
	} else if *setDefault {
		if abs, err := vault.CleanAbs(cur); err != nil || filepath.Clean(abs) != filepath.Clean(target.File) {
			// Normalize to an absolute path so subsequent commands behave the same from any cwd.
			settings.Vault.File = target.File
			settingsChanged = true
		}
	}
	if keyBackendOverride != "" {
		settings.Vault.KeyBackend = keyBackendOverride
		settingsChanged = true
	}
	if keyFileOverride != "" {
		settings.Vault.KeyFile = keyFileOverride
		settingsChanged = true
	}
	if settingsChanged {
		if err := saveSettings(settings); err != nil {
			fatal(err)
		}
	}

	vaultAuditEvent(settings, target, "init", map[string]any{
		"envFile":    filepath.Clean(target.File),
		"recipient":  recipient,
		"trustFp":    fp,
		"keyCreated": createdKey,
		"keyBackend": vault.NormalizeKeyBackend(keyCfg.Backend),
		"setDefault": *setDefault,
	})

	fmt.Printf("env file:  %s\n", filepath.Clean(target.File))
	fmt.Printf("recipient: %s\n", recipient)
	fmt.Printf("trust fp:  %s\n", fp)
	if settingsChanged && *setDefault {
		fmt.Printf("default:   updated\n")
	}
	if createdKey {
		fmt.Printf("key:       created (backend=%s)\n", vault.NormalizeKeyBackend(keyCfg.Backend))
	} else {
		fmt.Printf("key:       ok (backend=%s)\n", vault.NormalizeKeyBackend(keyCfg.Backend))
	}
	if err := maybeHeliaAutoBackupVault("vault_init", target.File); err != nil {
		fatal(err)
	}
}
