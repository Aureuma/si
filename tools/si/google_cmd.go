package main

import "strings"

const googleUsageText = "usage: si google <places|youtube>"

func cmdGoogle(args []string) {
	if len(args) == 0 {
		printUsage(googleUsageText)
		return
	}
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(googleUsageText)
	case "places":
		cmdGooglePlaces(rest)
	case "youtube", "yt":
		cmdGoogleYouTube(rest)
	default:
		printUnknown("google", cmd)
		printUsage(googleUsageText)
	}
}
