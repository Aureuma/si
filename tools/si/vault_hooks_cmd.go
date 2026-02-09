package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"si/tools/si/internal/vault"
)

func cmdVaultHooks(args []string) {
	if len(args) == 0 {
		printUsage("usage: si vault hooks <install|status|uninstall>")
		return
	}
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "install":
		cmdVaultHooksInstall(rest)
	case "status":
		cmdVaultHooksStatus(rest)
	case "uninstall", "remove", "rm":
		cmdVaultHooksUninstall(rest)
	default:
		printUnknown("vault hooks", cmd)
		printUsage("usage: si vault hooks <install|status|uninstall>")
	}
}

func cmdVaultHooksInstall(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault hooks install", flag.ExitOnError)
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to git root; use '.' when running inside the vault repo)")
	force := fs.Bool("force", false, "overwrite existing hook")
	_ = fs.Parse(args)
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault hooks install [--vault-dir <path>] [--force]")
		return
	}

	target, err := vaultResolveTarget(settings, "", *vaultDir, settings.Vault.DefaultEnv, true, true)
	if err != nil {
		fatal(err)
	}
	repoDir := strings.TrimSpace(target.VaultDir)
	if repoDir == "" || !vault.IsDir(repoDir) {
		// Fallback: allow running inside the vault repo itself when vault dir isn't resolvable.
		repoDir = strings.TrimSpace(target.RepoRoot)
	}
	if repoDir == "" || !vault.IsDir(repoDir) {
		fatal(fmt.Errorf("vault dir not found (set --vault-dir or run inside the vault repo)"))
	}

	hooksDir, err := vault.GitHooksDir(repoDir)
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		fatal(err)
	}

	hookPath := filepath.Join(hooksDir, "pre-commit")
	exe, _ := os.Executable()
	script := renderVaultPreCommitHook(exe)

	if err := writeHookFile(hookPath, script, *force); err != nil {
		fatal(err)
	}
	fmt.Printf("installed: %s\n", filepath.Clean(hookPath))
}

func cmdVaultHooksStatus(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault hooks status", flag.ExitOnError)
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to git root; use '.' when running inside the vault repo)")
	_ = fs.Parse(args)
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault hooks status [--vault-dir <path>]")
		return
	}

	target, err := vaultResolveTarget(settings, "", *vaultDir, settings.Vault.DefaultEnv, true, true)
	if err != nil {
		fatal(err)
	}
	repoDir := strings.TrimSpace(target.VaultDir)
	if repoDir == "" || !vault.IsDir(repoDir) {
		repoDir = strings.TrimSpace(target.RepoRoot)
	}
	if repoDir == "" || !vault.IsDir(repoDir) {
		fatal(fmt.Errorf("vault dir not found (set --vault-dir or run inside the vault repo)"))
	}
	hooksDir, err := vault.GitHooksDir(repoDir)
	if err != nil {
		fatal(err)
	}
	hookPath := filepath.Join(hooksDir, "pre-commit")
	data, err := os.ReadFile(hookPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("pre-commit: missing (%s)\n", filepath.Clean(hookPath))
			return
		}
		fatal(err)
	}
	if isVaultHookScript(data) {
		fmt.Printf("pre-commit: installed (%s)\n", filepath.Clean(hookPath))
		return
	}
	fmt.Printf("pre-commit: present (not managed by si) (%s)\n", filepath.Clean(hookPath))
}

func cmdVaultHooksUninstall(args []string) {
	settings := loadSettingsOrDefault()
	fs := flag.NewFlagSet("vault hooks uninstall", flag.ExitOnError)
	vaultDir := fs.String("vault-dir", settings.Vault.Dir, "vault directory (relative to git root; use '.' when running inside the vault repo)")
	_ = fs.Parse(args)
	if len(fs.Args()) != 0 {
		printUsage("usage: si vault hooks uninstall [--vault-dir <path>]")
		return
	}

	target, err := vaultResolveTarget(settings, "", *vaultDir, settings.Vault.DefaultEnv, true, true)
	if err != nil {
		fatal(err)
	}
	repoDir := strings.TrimSpace(target.VaultDir)
	if repoDir == "" || !vault.IsDir(repoDir) {
		repoDir = strings.TrimSpace(target.RepoRoot)
	}
	if repoDir == "" || !vault.IsDir(repoDir) {
		fatal(fmt.Errorf("vault dir not found (set --vault-dir or run inside the vault repo)"))
	}
	hooksDir, err := vault.GitHooksDir(repoDir)
	if err != nil {
		fatal(err)
	}
	hookPath := filepath.Join(hooksDir, "pre-commit")
	data, err := os.ReadFile(hookPath)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		fatal(err)
	}
	if !isVaultHookScript(data) {
		fatal(fmt.Errorf("refusing to remove non-si hook: %s", filepath.Clean(hookPath)))
	}
	if err := os.Remove(hookPath); err != nil {
		fatal(err)
	}
	fmt.Printf("removed: %s\n", filepath.Clean(hookPath))
}

func renderVaultPreCommitHook(selfExe string) string {
	selfExe = strings.TrimSpace(selfExe)
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("set -e\n")
	b.WriteString("# si-vault:hook pre-commit v1\n")
	if selfExe != "" {
		b.WriteString("SI_BIN_DEFAULT=")
		b.WriteString(shellSingleQuote(selfExe))
		b.WriteString("\n")
	} else {
		b.WriteString("SI_BIN_DEFAULT=\n")
	}
	b.WriteString("SI_BIN=${SI_BIN:-$SI_BIN_DEFAULT}\n")
	b.WriteString("if [ -n \"$SI_BIN\" ] && [ -x \"$SI_BIN\" ]; then\n")
	b.WriteString("  exec \"$SI_BIN\" vault check --staged --all --vault-dir .\n")
	b.WriteString("fi\n")
	b.WriteString("if command -v si >/dev/null 2>&1; then\n")
	b.WriteString("  exec si vault check --staged --all --vault-dir .\n")
	b.WriteString("fi\n")
	b.WriteString("echo \"[si vault] error: si not found (install si or set SI_BIN)\" >&2\n")
	b.WriteString("exit 1\n")
	return b.String()
}

func isVaultHookScript(data []byte) bool {
	txt := string(data)
	return strings.Contains(txt, "si-vault:hook pre-commit v1")
}

func writeHookFile(path string, contents string, force bool) error {
	path = filepath.Clean(path)
	if data, err := os.ReadFile(path); err == nil {
		if isVaultHookScript(data) {
			// Idempotent.
			return os.WriteFile(path, []byte(contents), 0o755)
		}
		if !force {
			return fmt.Errorf("hook already exists (use --force): %s", path)
		}
	}
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		return err
	}
	return nil
}

