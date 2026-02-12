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
	fileFlag := fs.String("file", "", "explicit env file path (defaults to the configured vault.file)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	target, err := vaultResolveTarget(settings, strings.TrimSpace(*fileFlag), false)
	if err != nil {
		fatal(err)
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
	fileFlag := fs.String("file", "", "explicit env file path (defaults to the configured vault.file)")
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

	target, err := vaultResolveTarget(settings, strings.TrimSpace(*fileFlag), false)
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
		File:        target.File,
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
	fileFlag := fs.String("file", "", "explicit env file path (defaults to the configured vault.file)")
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

	target, err := vaultResolveTarget(settings, strings.TrimSpace(*fileFlag), false)
	if err != nil {
		fatal(err)
	}
	doc, err := vault.ReadDotenvFile(target.File)
	if err != nil {
		fatal(err)
	}
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
	} else {
		fmt.Printf("recipient: not present\n")
	}
}
