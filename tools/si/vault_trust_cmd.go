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
	fileFlag := fs.String("file", "", "explicit env file path (overrides --vault-dir/--env)")
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to host git root)")
	env := fs.String("env", settings.Vault.DefaultEnv, "environment name (maps to .env.<env>)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	target, err := vaultResolveTarget(settings, *fileFlag, *vaultDir, *env, false, false)
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
	url := vaultRepoURL(target)

	store, err := vault.LoadTrustStore(vaultTrustStorePath(settings))
	if err != nil {
		fatal(err)
	}
	entry, ok := store.Find(target.RepoRoot, target.VaultDir, target.Env)

	fmt.Printf("vault dir: %s\n", filepath.Clean(target.VaultDir))
	fmt.Printf("env file:  %s\n", filepath.Clean(target.File))
	if url != "" {
		fmt.Printf("vault url: %s\n", url)
	}
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
	fileFlag := fs.String("file", "", "explicit env file path (overrides --vault-dir/--env)")
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to host git root)")
	env := fs.String("env", settings.Vault.DefaultEnv, "environment name (maps to .env.<env>)")
	yes := fs.Bool("yes", false, "do not prompt")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	target, err := vaultResolveTarget(settings, *fileFlag, *vaultDir, *env, false, false)
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
		fmt.Printf("%s ", styleDim(fmt.Sprintf("Accept vault trust for %s (%s) with fingerprint %s? [y/N]:", filepath.Clean(target.VaultDir), target.Env, fp)))
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
		VaultDir:    target.VaultDir,
		Env:         target.Env,
		VaultRepo:   vaultRepoURL(target),
		Fingerprint: fp,
	})
	if err := store.Save(storePath); err != nil {
		fatal(err)
	}
	fmt.Printf("trusted: %s (%s)\n", filepath.Clean(target.VaultDir), target.Env)
}

func cmdVaultTrustForget(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault trust forget", flag.ExitOnError)
	fileFlag := fs.String("file", "", "explicit env file path (overrides --vault-dir/--env)")
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to host git root)")
	env := fs.String("env", settings.Vault.DefaultEnv, "environment name (maps to .env.<env>)")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	target, err := vaultResolveTarget(settings, *fileFlag, *vaultDir, *env, true, true)
	if err != nil {
		fatal(err)
	}

	storePath := vaultTrustStorePath(settings)
	store, err := vault.LoadTrustStore(storePath)
	if err != nil {
		fatal(err)
	}
	if !store.Delete(target.RepoRoot, target.VaultDir, target.Env) {
		fmt.Printf("trust: no entry for %s (%s)\n", filepath.Clean(target.VaultDir), target.Env)
		return
	}
	if err := store.Save(storePath); err != nil {
		fatal(err)
	}
	fmt.Printf("trust: removed for %s (%s)\n", filepath.Clean(target.VaultDir), target.Env)
}
