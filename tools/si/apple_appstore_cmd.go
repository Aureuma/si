package main

import "strings"

const appleAppStoreUsageText = "usage: si apple appstore <auth|context|doctor|app|listing|raw|apply>"

func cmdAppleAppStore(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, appleAppStoreUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(appleAppStoreUsageText)
	case "auth":
		cmdAppleAppStoreAuth(rest)
	case "context":
		cmdAppleAppStoreContext(rest)
	case "doctor":
		cmdAppleAppStoreDoctor(rest)
	case "app", "apps":
		cmdAppleAppStoreApp(rest)
	case "listing", "listings", "metadata":
		cmdAppleAppStoreListing(rest)
	case "raw":
		cmdAppleAppStoreRaw(rest)
	case "apply", "deploy":
		cmdAppleAppStoreApply(rest)
	default:
		printUnknown("apple appstore", cmd)
		printUsage(appleAppStoreUsageText)
	}
}
