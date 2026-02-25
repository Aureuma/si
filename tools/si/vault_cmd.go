package main

import (
	"os"
	"strings"
)

const vaultUsageText = "usage: si vault <keypair|keygen|status|check|hooks|encrypt|decrypt|restore|set|unset|get|list|ls|run>\n\nAlias:\n  si creds ..."

const vaultDockerUsageText = "usage: si vault docker <exec>"

var vaultActions = []subcommandAction{
	{Name: "keypair", Description: "ensure Sun-backed keypair for repo/env and print public key"},
	{Name: "keygen", Description: "alias for keypair"},
	{Name: "status", Description: "show vault repo/env, file, and key readiness"},
	{Name: "check", Description: "check .env files for plaintext values (hook-safe)"},
	{Name: "hooks", Description: "install/status/uninstall plaintext-guard pre-commit hooks"},
	{Name: "encrypt", Description: "encrypt dotenv values using SI_VAULT_PUBLIC_KEY"},
	{Name: "decrypt", Description: "decrypt dotenv values (stdout by default, --inplace optional)"},
	{Name: "restore", Description: "restore last encrypted backup after an inplace decrypt"},
	{Name: "set", Description: "set a key in dotenv (encrypted by default)"},
	{Name: "unset", Description: "remove a key from dotenv"},
	{Name: "get", Description: "get key value (optionally reveal decrypted value)"},
	{Name: "list", Description: "list dotenv keys and encryption state"},
	{Name: "ls", Description: "alias for list"},
	{Name: "run", Description: "run command with dotenv values decrypted at runtime"},
	{Name: "docker", Description: "run commands in containers with vault env"},
}

var vaultDockerActions = []subcommandAction{
	{Name: "exec", Description: "execute command in a container with vault env"},
}

func cmdVault(args []string) {
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(args, isInteractiveTerminal(), selectVaultAction)
	if showUsage {
		printUsage(vaultUsageText)
		return
	}
	if !ok {
		return
	}
	args = resolved
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(vaultUsageText)
	case "keypair":
		cmdVaultKeygen(rest)
	case "keygen":
		cmdVaultKeygen(rest)
	case "status":
		cmdVaultStatus(rest)
	case "check":
		cmdVaultCheck(rest)
	case "hooks", "hook":
		cmdVaultHooks(rest)
	case "encrypt":
		cmdVaultEncrypt(rest)
	case "decrypt":
		cmdVaultDecrypt(rest)
	case "restore":
		cmdVaultRestore(rest)
	case "set":
		cmdVaultSet(rest)
	case "unset":
		cmdVaultUnset(rest)
	case "get":
		cmdVaultGet(rest)
	case "list", "ls":
		cmdVaultList(rest)
	case "run":
		cmdVaultRun(rest)
	case "docker":
		cmdVaultDocker(rest)
	default:
		printUnknown("vault", cmd)
		printUsage(vaultUsageText)
		os.Exit(1)
	}
}

func selectVaultAction() (string, bool) {
	return selectSubcommandAction("Vault commands:", vaultActions)
}

func cmdVaultDocker(args []string) {
	resolved, showUsage, ok := resolveSubcommandDispatchArgs(args, isInteractiveTerminal(), selectVaultDockerAction)
	if showUsage {
		printUsage(vaultDockerUsageText)
		return
	}
	if !ok {
		return
	}
	args = resolved
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "exec":
		cmdVaultDockerExec(rest)
	default:
		printUnknown("vault docker", cmd)
		printUsage(vaultDockerUsageText)
		os.Exit(1)
	}
}

func selectVaultDockerAction() (string, bool) {
	return selectSubcommandAction("Vault docker commands:", vaultDockerActions)
}
