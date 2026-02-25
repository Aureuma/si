package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"si/tools/si/internal/vault"
)

const vaultRecipientsUsageText = "usage: si vault recipients list|add|remove"

var vaultRecipientsActions = []subcommandAction{
	{Name: "list", Description: "list recipients and trust fingerprint"},
	{Name: "add", Description: "add a recipient to vault metadata"},
	{Name: "remove", Description: "remove a recipient from vault metadata"},
}

func cmdVaultRecipients(args []string) {
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(args, isInteractiveTerminal(), selectVaultRecipientsAction)
	if showUsage {
		printUsage(vaultRecipientsUsageText)
		return
	}
	if !ok {
		return
	}
	args = resolved
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
		printUsage(vaultRecipientsUsageText)
	}
}

func selectVaultRecipientsAction() (string, bool) {
	return selectSubcommandAction("Vault recipients commands:", vaultRecipientsActions)
}

func cmdVaultRecipientsList(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault recipients list", flag.ExitOnError)
	fileFlag := fs.String("file", "", "vault scope (preferred: --scope)")
	scopeFlag := fs.String("scope", "", "vault scope")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	scope := strings.TrimSpace(*scopeFlag)
	if scope == "" {
		scope = strings.TrimSpace(*fileFlag)
	}

	target, err := vaultResolveTarget(settings, scope, false)
	if err != nil {
		fatal(err)
	}
	if backend, backendErr := resolveVaultSyncBackend(settings); backendErr == nil && backend.Mode == vaultSyncBackendSun {
		identity, idErr := vaultEnsureStrictSunIdentity(settings, "vault_recipients_list")
		if idErr != nil {
			fatal(idErr)
		}
		if identity == nil {
			fatal(fmt.Errorf("sun vault identity unavailable"))
		}
		recipient := strings.TrimSpace(identity.Recipient().String())
		recipients := []string{recipient}
		fp := vault.RecipientsFingerprint(recipients)
		fmt.Printf("scope: %s\n", strings.TrimSpace(target.File))
		fmt.Printf("source: sun-identity\n")
		fmt.Printf("trust fp: %s\n", fp)
		fmt.Printf("%s\n", recipient)
		return
	}

	doc, err := vault.ReadDotenvFile(target.File)
	if err != nil {
		fatal(err)
	}
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
	fileFlag := fs.String("file", "", "vault scope (preferred: --scope)")
	scopeFlag := fs.String("scope", "", "vault scope")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	rest := fs.Args()
	if len(rest) != 1 {
		printUsage("usage: si vault recipients add <age1...> [--file <path>]")
		return
	}
	recipient := strings.TrimSpace(rest[0])
	if recipient == "" {
		fatal(fmt.Errorf("recipient required"))
	}
	if backend, backendErr := resolveVaultSyncBackend(settings); backendErr == nil && backend.Mode == vaultSyncBackendSun {
		fatal(fmt.Errorf("vault recipients add is not supported in Sun remote vault mode; rotate identity with `si vault keygen --rotate` if needed"))
	}
	scope := strings.TrimSpace(*scopeFlag)
	if scope == "" {
		scope = strings.TrimSpace(*fileFlag)
	}

	target, err := vaultResolveTarget(settings, scope, false)
	if err != nil {
		fatal(err)
	}
	doc, err := vault.ReadDotenvFile(target.File)
	if err != nil {
		fatal(err)
	}
	changed, err := vault.EnsureVaultHeader(&doc, []string{recipient})
	if err != nil {
		fatal(err)
	}
	if changed {
		if err := vaultWriteDotenvFileAtomic(target.File, doc.Bytes()); err != nil {
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
		File:        target.File,
		Fingerprint: fp,
	})
	if err := store.Save(storePath); err != nil {
		fatal(err)
	}

	fmt.Printf("file: %s\n", filepath.Clean(target.File))
	if changed {
		fmt.Printf("recipient: added\n")
		if err := maybeSunAutoBackupVault("vault_recipients_add", target.File); err != nil {
			fatal(err)
		}
	} else {
		fmt.Printf("recipient: already present\n")
	}
}

func cmdVaultRecipientsRemove(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault recipients remove", flag.ExitOnError)
	fileFlag := fs.String("file", "", "vault scope (preferred: --scope)")
	scopeFlag := fs.String("scope", "", "vault scope")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	rest := fs.Args()
	if len(rest) != 1 {
		printUsage("usage: si vault recipients remove <age1...> [--file <path>]")
		return
	}
	recipient := strings.TrimSpace(rest[0])
	if recipient == "" {
		fatal(fmt.Errorf("recipient required"))
	}
	if backend, backendErr := resolveVaultSyncBackend(settings); backendErr == nil && backend.Mode == vaultSyncBackendSun {
		fatal(fmt.Errorf("vault recipients remove is not supported in Sun remote vault mode; rotate identity with `si vault keygen --rotate` if needed"))
	}
	scope := strings.TrimSpace(*scopeFlag)
	if scope == "" {
		scope = strings.TrimSpace(*fileFlag)
	}

	target, err := vaultResolveTarget(settings, scope, false)
	if err != nil {
		fatal(err)
	}
	doc, err := vault.ReadDotenvFile(target.File)
	if err != nil {
		fatal(err)
	}
	changed := vault.RemoveRecipient(&doc, recipient)
	if changed {
		if err := vaultWriteDotenvFileAtomic(target.File, doc.Bytes()); err != nil {
			fatal(err)
		}
	}

	// Update trust to match the intentional recipient change.
	if recipients := vault.ParseRecipientsFromDotenv(doc); len(recipients) == 0 {
		storePath := vaultTrustStorePath(settings)
		store, err := vault.LoadTrustStore(storePath)
		if err == nil {
			_ = store.Delete(target.RepoRoot, target.File)
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
		File:        target.File,
		Fingerprint: fp,
	})
	if err := store.Save(storePath); err != nil {
		fatal(err)
	}

	fmt.Printf("file: %s\n", filepath.Clean(target.File))
	if changed {
		fmt.Printf("recipient: removed\n")
		if err := maybeSunAutoBackupVault("vault_recipients_remove", target.File); err != nil {
			fatal(err)
		}
	} else {
		fmt.Printf("recipient: not present\n")
	}
}
