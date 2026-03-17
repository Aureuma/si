package main

import (
	"strings"
)

const buildUsageText = "usage: si build <image> [args...]"

func cmdBuild(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, buildUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "image":
		cmdBuildImage(args[1:])
	case "help", "-h", "--help":
		printUsage(buildUsageText)
	default:
		printUnknown("build", args[0])
		printUsage(buildUsageText)
	}
}
