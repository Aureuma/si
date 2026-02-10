package main

import (
	"os"
	"strings"
)

const vaultUsageText = "usage: si vault <init|keygen|status|check|hooks|fmt|encrypt|set|unset|get|list|run|docker|trust|recipients>\n\nAlias:\n  si creds ..."

func cmdVault(args []string) {
	if len(args) == 0 {
		printUsage(vaultUsageText)
		return
	}
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(vaultUsageText)
	case "init":
		cmdVaultInit(rest)
	case "keygen":
		cmdVaultKeygen(rest)
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
	case "set":
		cmdVaultSet(rest)
	case "unset":
		cmdVaultUnset(rest)
	case "get":
		cmdVaultGet(rest)
	case "list":
		cmdVaultList(rest)
	case "run":
		cmdVaultRun(rest)
	case "docker":
		cmdVaultDocker(rest)
	case "trust":
		cmdVaultTrust(rest)
	case "recipients", "recipient":
		cmdVaultRecipients(rest)
	default:
		printUnknown("vault", cmd)
		printUsage(vaultUsageText)
		os.Exit(1)
	}
}

func cmdVaultDocker(args []string) {
	if len(args) == 0 {
		printUsage("usage: si vault docker <exec>")
		return
	}
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "exec":
		cmdVaultDockerExec(rest)
	default:
		printUnknown("vault docker", cmd)
		printUsage("usage: si vault docker <exec>")
		os.Exit(1)
	}
}
