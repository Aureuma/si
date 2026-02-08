package main

import "strings"

const vaultUsageText = "usage: si vault <init|status|fmt|encrypt|set|unset|get|list|run|docker|trust|recipients>\n\nAlias:\n  si creds ..."

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
	case "status":
		cmdVaultStatus(rest)
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
	}
}

func cmdVaultInit(args []string)        { fatal(errNotImplemented("vault init")) }
func cmdVaultStatus(args []string)      { fatal(errNotImplemented("vault status")) }
func cmdVaultFmt(args []string)         { fatal(errNotImplemented("vault fmt")) }
func cmdVaultEncrypt(args []string)     { fatal(errNotImplemented("vault encrypt")) }
func cmdVaultSet(args []string)         { fatal(errNotImplemented("vault set")) }
func cmdVaultUnset(args []string)       { fatal(errNotImplemented("vault unset")) }
func cmdVaultGet(args []string)         { fatal(errNotImplemented("vault get")) }
func cmdVaultList(args []string)        { fatal(errNotImplemented("vault list")) }
func cmdVaultRun(args []string)         { fatal(errNotImplemented("vault run")) }
func cmdVaultDockerExec(args []string)  { fatal(errNotImplemented("vault docker exec")) }
func cmdVaultTrust(args []string)       { fatal(errNotImplemented("vault trust")) }
func cmdVaultRecipients(args []string)  { fatal(errNotImplemented("vault recipients")) }

func errNotImplemented(name string) error {
	return &notImplementedError{name: name}
}

type notImplementedError struct{ name string }

func (e *notImplementedError) Error() string {
	if strings.TrimSpace(e.name) == "" {
		return "not implemented"
	}
	return e.name + ": not implemented"
}

