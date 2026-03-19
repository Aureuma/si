package main

const appleAppStoreUsageText = "usage: si apple appstore <auth|context|doctor|app|listing|raw|apply>"

func cmdAppleAppStore(args []string) {
	delegated, err := runAppleAppStoreCommand(args)
	requireRustCLIDelegation("apple appstore", delegated, err)
}
