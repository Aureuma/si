package main

import "strings"

const stripeUsageText = "usage: si stripe <auth|context|doctor|object|raw|report|sync>"

func cmdStripe(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, stripeUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(stripeUsageText)
	case "auth":
		cmdStripeAuth(rest)
	case "context":
		cmdStripeContext(rest)
	case "doctor":
		cmdStripeDoctor(rest)
	case "object":
		cmdStripeObject(rest)
	case "raw":
		cmdStripeRaw(rest)
	case "report":
		cmdStripeReport(rest)
	case "sync":
		cmdStripeSync(rest)
	default:
		printUnknown("stripe", cmd)
		printUsage(stripeUsageText)
	}
}
