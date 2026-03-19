package main

import "strings"

const providersUsageText = "usage: si providers <characteristics|health>"

func cmdProviders(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, providersUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(providersUsageText)
	case "characteristics", "chars", "status", "list":
		cmdProvidersCharacteristics(rest)
	case "health":
		cmdProvidersHealth(rest)
	default:
		printUnknown("providers", sub)
		printUsage(providersUsageText)
	}
}

func cmdProvidersCharacteristics(args []string) {
	delegated, err := runProvidersCharacteristicsCommand(args)
	requireRustCLIDelegation("providers characteristics", delegated, err)
}

func cmdProvidersHealth(args []string) {
	delegated, err := runProvidersHealthCommand(args)
	requireRustCLIDelegation("providers health", delegated, err)
}
