package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultRecipients(args []string) {
	if len(args) == 0 {
		printUsage("usage: si vault recipients list|add|remove")
		return
	}
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "list":
		cmdVaultRecipientsList(rest)
	case "add":
		cmdVaultRecipientsAdd(rest)
	case "remove":
		cmdVaultRecipientsRemove(rest)
	default:
		printUnknown("vault recipients", cmd)
		printUsage("usage: si vault recipients list|add|remove")
	}
}

func cmdVaultRecipientsList(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault recipients list", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path (overrides --vault-dir/--env)")
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to host git root)")
	env := fs.String("env", settings.Vault.DefaultEnv, "environment name (maps to .env.<env>)")
	fs.Parse(args)

	target, err := vaultResolveTarget(settings, *fileFlag, *vaultDir, *env, false, false)
	if err != nil {
		fatal(err)
	}
	data, err := os.ReadFile(target.File)
	if err != nil {
		fatal(err)
	}
	doc := vault.ParseDotenv(data)
	recipients := vault.ParseRecipientsFromDotenv(doc)
	fp := vault.RecipientsFingerprint(recipients)

	fmt.Printf("file: %s\n", filepath.Clean(target.File))
	fmt.Printf("trust fp: %s\n", fp)
	for _, r := range recipients {
		fmt.Printf("%s\n", strings.TrimSpace(r))
	}
}

func cmdVaultRecipientsAdd(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault recipients add", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path (overrides --vault-dir/--env)")
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to host git root)")
	env := fs.String("env", settings.Vault.DefaultEnv, "environment name (maps to .env.<env>)")
	fs.Parse(args)
	rest := fs.Args()
	if len(rest) != 1 {
		printUsage("usage: si vault recipients add <age1...> [--vault-dir <path>] [--env <name>]")
		return
	}
	recipient := strings.TrimSpace(rest[0])
	if recipient == "" {
		fatal(fmt.Errorf("recipient required"))
	}

	target, err := vaultResolveTarget(settings, *fileFlag, *vaultDir, *env, false, false)
	if err != nil {
		fatal(err)
	}
	data, err := os.ReadFile(target.File)
	if err != nil {
		fatal(err)
	}
	doc := vault.ParseDotenv(data)
	changed, err := vault.EnsureVaultHeader(&doc, []string{recipient})
	if err != nil {
		fatal(err)
	}
	if changed {
		if err := vault.WriteDotenvFileAtomic(target.File, doc.Bytes()); err != nil {
			fatal(err)
		}
	}

	// Update trust to match the intentional recipient change.
	fp, err := vaultTrustFingerprint(doc)
	if err != nil {
		fatal(err)
	}
	storePath := vaultTrustStorePath(settings)
	store, err := vault.LoadTrustStore(storePath)
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
	if err := store.Save(storePath); err != nil {
		fatal(err)
	}

	fmt.Printf("file: %s\n", filepath.Clean(target.File))
	if changed {
		fmt.Printf("recipient: added\n")
	} else {
		fmt.Printf("recipient: already present\n")
	}
}

func cmdVaultRecipientsRemove(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault recipients remove", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path (overrides --vault-dir/--env)")
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to host git root)")
	env := fs.String("env", settings.Vault.DefaultEnv, "environment name (maps to .env.<env>)")
	fs.Parse(args)
	rest := fs.Args()
	if len(rest) != 1 {
		printUsage("usage: si vault recipients remove <age1...> [--vault-dir <path>] [--env <name>]")
		return
	}
	recipient := strings.TrimSpace(rest[0])
	if recipient == "" {
		fatal(fmt.Errorf("recipient required"))
	}

	target, err := vaultResolveTarget(settings, *fileFlag, *vaultDir, *env, false, false)
	if err != nil {
		fatal(err)
	}
	data, err := os.ReadFile(target.File)
	if err != nil {
		fatal(err)
	}
	doc := vault.ParseDotenv(data)
	changed := vault.RemoveRecipient(&doc, recipient)
	if changed {
		if err := vault.WriteDotenvFileAtomic(target.File, doc.Bytes()); err != nil {
			fatal(err)
		}
	}

	// Update trust to match the intentional recipient change.
	if recipients := vault.ParseRecipientsFromDotenv(doc); len(recipients) == 0 {
		storePath := vaultTrustStorePath(settings)
		store, err := vault.LoadTrustStore(storePath)
		if err == nil {
			_ = store.Delete(target.RepoRoot, target.VaultDir, target.Env)
			_ = store.Save(storePath)
		}
		fatal(fmt.Errorf("no recipients remaining after removal"))
	}
	fp, err := vaultTrustFingerprint(doc)
	if err != nil {
		fatal(err)
	}
	storePath := vaultTrustStorePath(settings)
	store, err := vault.LoadTrustStore(storePath)
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
	if err := store.Save(storePath); err != nil {
		fatal(err)
	}

	fmt.Printf("file: %s\n", filepath.Clean(target.File))
	if changed {
		fmt.Printf("recipient: removed\n")
	} else {
		fmt.Printf("recipient: not present\n")
	}
}
