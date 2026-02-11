package main

import "strings"

const appleUsageText = "usage: si apple <appstore|app-store|app-store-connect>"

func cmdApple(args []string) {
	if len(args) == 0 {
		printUsage(appleUsageText)
		return
	}
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(appleUsageText)
	case "appstore", "app-store", "app_store", "app-store-connect", "app_store_connect", "asc":
		cmdAppleAppStore(rest)
	default:
		printUnknown("apple", cmd)
		printUsage(appleUsageText)
	}
}
