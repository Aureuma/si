package main

const appleUsageText = "usage: si apple <appstore|app-store|app-store-connect>"

func cmdApple(args []string) {
	delegated, err := runAppleCommand(args)
	requireRustCLIDelegation("apple", delegated, err)
}
