package main

import "strings"

const googlePlayUsageText = "usage: si google play <auth|context|doctor|app|listing|details|asset|release|raw|apply>"

func cmdGooglePlay(args []string) {
	if len(args) == 0 {
		printUsage(googlePlayUsageText)
		return
	}
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(googlePlayUsageText)
	case "auth":
		cmdGooglePlayAuth(rest)
	case "context":
		cmdGooglePlayContext(rest)
	case "doctor":
		cmdGooglePlayDoctor(rest)
	case "app", "application":
		cmdGooglePlayApp(rest)
	case "listing", "listings":
		cmdGooglePlayListing(rest)
	case "details", "detail":
		cmdGooglePlayDetails(rest)
	case "asset", "assets", "image", "images":
		cmdGooglePlayAsset(rest)
	case "release", "track", "tracks":
		cmdGooglePlayRelease(rest)
	case "raw":
		cmdGooglePlayRaw(rest)
	case "apply", "deploy":
		cmdGooglePlayApply(rest)
	default:
		printUnknown("google play", cmd)
		printUsage(googlePlayUsageText)
	}
}
