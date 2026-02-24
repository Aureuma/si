package main

import (
	"os"
	"strings"
)

const vaultUsageText = "usage: si vault <init|keygen|use|status|check|hooks|fmt|encrypt|decrypt|set|unset|get|list|history|run|docker|trust|recipients|backend|sync>\n\nAlias:\n  si creds ..."

const vaultDockerUsageText = "usage: si vault docker <exec>"

var vaultActions = []subcommandAction{
	{Name: "init", Description: "initialize vault metadata for a dotenv file"},
	{Name: "status", Description: "show vault configuration and readiness"},
	{Name: "check", Description: "validate vault formatting and encryption rules"},
	{Name: "fmt", Description: "format vault dotenv files"},
	{Name: "encrypt", Description: "encrypt plaintext values in vault file"},
	{Name: "decrypt", Description: "decrypt values (guarded for safety)"},
	{Name: "set", Description: "set/update encrypted secret value"},
	{Name: "get", Description: "inspect a key (optionally reveal)"},
	{Name: "list", Description: "list keys and encryption status"},
	{Name: "history", Description: "show cloud revision history for a key"},
	{Name: "run", Description: "run a command with decrypted env injection"},
	{Name: "docker", Description: "run commands in containers with vault env"},
	{Name: "trust", Description: "manage trust state for vault files"},
	{Name: "recipients", Description: "manage recipient keys"},
	{Name: "backend", Description: "manage vault sync backend mode"},
	{Name: "sync", Description: "push/pull vault backups with Sun"},
	{Name: "keygen", Description: "create or load vault identity key"},
	{Name: "use", Description: "set the default vault file in settings"},
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
	case "init":
		cmdVaultInit(rest)
	case "keygen":
		cmdVaultKeygen(rest)
	case "use":
		cmdVaultUse(rest)
	case "status":
		cmdVaultStatus(rest)
	case "check":
		cmdVaultCheck(rest)
	case "hooks", "hook":
		cmdVaultHooks(rest)
	case "fmt":
		cmdVaultFmt(rest)
	case "encrypt":
		cmdVaultEncrypt(rest)
	case "decrypt":
		cmdVaultDecrypt(rest)
	case "set":
		cmdVaultSet(rest)
	case "unset":
		cmdVaultUnset(rest)
	case "get":
		cmdVaultGet(rest)
	case "list":
		cmdVaultList(rest)
	case "history":
		cmdVaultHistory(rest)
	case "run":
		cmdVaultRun(rest)
	case "docker":
		cmdVaultDocker(rest)
	case "trust":
		cmdVaultTrust(rest)
	case "recipients", "recipient":
		cmdVaultRecipients(rest)
	case "backend":
		cmdVaultBackend(rest)
	case "sync":
		cmdVaultSync(rest)
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
