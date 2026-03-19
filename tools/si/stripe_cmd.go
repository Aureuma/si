package main

const stripeUsageText = "usage: si stripe <auth|context|doctor|object|raw|report|sync>"

func cmdStripe(args []string) {
	delegated, err := runStripeCommand(args)
	requireRustCLIDelegation("stripe", delegated, err)
}
