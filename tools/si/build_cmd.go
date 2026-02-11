package main

import (
	"strings"
)

const buildUsageText = "usage: si build <image|self> [args...]"

func cmdBuild(args []string) {
	if len(args) == 0 {
		printUsage(buildUsageText)
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "image":
		cmdBuildImage(args[1:])
	case "self":
		cmdBuildSelf(args[1:])
	case "help", "-h", "--help":
		printUsage(buildUsageText)
	default:
		printUnknown("build", args[0])
		printUsage(buildUsageText)
	}
}

func cmdBuildSelf(args []string) {
	if len(args) == 0 {
		// Default: upgrade installed si from the current checkout.
		cmdSelfBuild(nil)
		return
	}
	head := strings.ToLower(strings.TrimSpace(args[0]))
	// Preserve flag-first UX: `si build self --no-upgrade --output ./si`.
	if strings.HasPrefix(head, "-") {
		cmdSelfBuild(args)
		return
	}
	switch head {
	case "build":
		cmdSelfBuild(args[1:])
	case "upgrade":
		cmdSelfUpgrade(args[1:])
	case "run":
		cmdSelfRun(args[1:])
	case "help", "-h", "--help":
		printUsage("usage: si build self [build|upgrade|run] [args...]")
	default:
		printUnknown("build self", args[0])
		printUsage("usage: si build self [build|upgrade|run] [args...]")
	}
}
