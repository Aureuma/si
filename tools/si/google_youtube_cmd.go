package main

import "strings"

const googleYouTubeUsageText = "usage: si google youtube <auth|context|doctor|search|channel|video|playlist|playlist-item|subscription|comment|caption|thumbnail|live|support|raw|report>"

func cmdGoogleYouTube(args []string) {
	if len(args) == 0 {
		printUsage(googleYouTubeUsageText)
		return
	}
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(googleYouTubeUsageText)
	case "auth":
		cmdGoogleYouTubeAuth(rest)
	case "context":
		cmdGoogleYouTubeContext(rest)
	case "doctor":
		cmdGoogleYouTubeDoctor(rest)
	case "search":
		cmdGoogleYouTubeSearch(rest)
	case "channel", "channels":
		cmdGoogleYouTubeChannel(rest)
	case "video", "videos":
		cmdGoogleYouTubeVideo(rest)
	case "playlist", "playlists":
		cmdGoogleYouTubePlaylist(rest)
	case "playlist-item", "playlist-items", "playlistitem", "playlistitems":
		cmdGoogleYouTubePlaylistItem(rest)
	case "subscription", "subscriptions":
		cmdGoogleYouTubeSubscription(rest)
	case "comment", "comments":
		cmdGoogleYouTubeComment(rest)
	case "caption", "captions":
		cmdGoogleYouTubeCaption(rest)
	case "thumbnail", "thumbnails":
		cmdGoogleYouTubeThumbnail(rest)
	case "live":
		cmdGoogleYouTubeLive(rest)
	case "support":
		cmdGoogleYouTubeSupport(rest)
	case "raw":
		cmdGoogleYouTubeRaw(rest)
	case "report":
		cmdGoogleYouTubeReport(rest)
	default:
		printUnknown("google youtube", cmd)
		printUsage(googleYouTubeUsageText)
	}
}
