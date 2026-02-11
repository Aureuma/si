package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultTrust(args []string) {
	if len(args) == 0 {
		printUsage("usage: si vault trust status|accept|forget")
		return
	}
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "status":
		cmdVaultTrustStatus(rest)
	case "accept":
		cmdVaultTrustAccept(rest)
	case "forget":
		cmdVaultTrustForget(rest)
	default:
		printUnknown("vault trust", cmd)
		printUsage("usage: si vault trust status|accept|forget")
	}
}

func cmdVaultTrustStatus(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault trust status", flag.ExitOnError)
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
	fp, err := vaultTrustFingerprint(doc)
	if err != nil {
		fatal(err)
	}

	store, err := vault.LoadTrustStore(vaultTrustStorePath(settings))
	if err != nil {
		fatal(err)
	}
	entry, ok := store.Find(target.RepoRoot, target.File)

	fmt.Printf("env file:  %s\n", filepath.Clean(target.File))
	fmt.Printf("current fp: %s\n", fp)
	if !ok {
		fmt.Printf("stored fp:  (none)\n")
		fmt.Printf("trust:      untrusted\n")
		return
	}
	fmt.Printf("stored fp:  %s\n", strings.TrimSpace(entry.Fingerprint))
	if strings.TrimSpace(entry.Fingerprint) == fp {
		fmt.Printf("trust:      ok\n")
	} else {
		fmt.Printf("trust:      mismatch\n")
	}
}

func cmdVaultTrustAccept(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault trust accept", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path (defaults to the configured vault.file)")
	yes := fs.Bool("yes", false, "do not prompt")
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
	fp, err := vaultTrustFingerprint(doc)
	if err != nil {
		fatal(err)
	}

	if !*yes {
		if !isInteractiveTerminal() {
			fatal(fmt.Errorf("non-interactive: use --yes to accept trust"))
		}
		fmt.Printf("%s ", styleDim(fmt.Sprintf("Accept vault trust for %s with fingerprint %s? [y/N]:", filepath.Clean(target.File), fp)))
		line, err := promptLine(os.Stdin)
		if err != nil {
			fatal(err)
		}
		line = strings.ToLower(strings.TrimSpace(line))
		if line != "y" && line != "yes" {
			return
		}
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
	fmt.Printf("trusted: %s\n", filepath.Clean(target.File))
}

func cmdVaultTrustForget(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault trust forget", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path (defaults to the configured vault.file)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	target, err := vaultResolveTarget(settings, strings.TrimSpace(*fileFlag), true)
	if err != nil {
		fatal(err)
	}

	storePath := vaultTrustStorePath(settings)
	store, err := vault.LoadTrustStore(storePath)
	if err != nil {
		fatal(err)
	}
	if !store.Delete(target.RepoRoot, target.File) {
		fmt.Printf("trust: no entry for %s\n", filepath.Clean(target.File))
		return
	}
	if err := store.Save(storePath); err != nil {
		fatal(err)
	}
	fmt.Printf("trust: removed for %s\n", filepath.Clean(target.File))
}
