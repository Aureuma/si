package main

import "strings"

const socialUsageText = "usage: si social <facebook|instagram|x|linkedin|reddit>"

func cmdSocial(args []string) {
	if len(args) == 0 {
		printUsage(socialUsageText)
		return
	}
	cmd := normalizeSocialPlatform(args[0])
	rest := args[1:]
	switch cmd {
	case socialPlatformHelp:
		printUsage(socialUsageText)
	case socialPlatformFacebook:
		cmdSocialFacebook(rest)
	case socialPlatformInstagram:
		cmdSocialInstagram(rest)
	case socialPlatformX:
		cmdSocialX(rest)
	case socialPlatformLinkedIn:
		cmdSocialLinkedIn(rest)
	case socialPlatformReddit:
		cmdSocialReddit(rest)
	default:
		value := strings.TrimSpace(args[0])
		printUnknown("social", value)
		printUsage(socialUsageText)
	}
}
