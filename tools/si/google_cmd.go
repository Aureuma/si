package main

import "strings"

const googleUsageText = "usage: si google <places|play|youtube|youtube-data>"

func cmdGoogle(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, googleUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(googleUsageText)
	case "places":
		cmdGooglePlaces(rest)
	case "play", "google-play", "googleplay":
		cmdGooglePlay(rest)
	case "youtube", "yt", "youtube-data", "youtube_data":
		cmdGoogleYouTube(rest)
	default:
		printUnknown("google", cmd)
		printUsage(googleUsageText)
	}
}
