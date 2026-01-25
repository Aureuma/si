package main

import (
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "codex":
		cmdCodex(args)
	case "docker":
		cmdDocker(args)
	case "dyad":
		cmdDyad(args)
	case "images":
		cmdImages(args)
	case "image":
		cmdImage(args)
	case "profile":
		cmdProfile(args)
	case "capability":
		cmdCapability(args)
	case "help", "-h", "--help":
		usage()
	default:
		printUnknown("", cmd)
		usage()
		os.Exit(1)
	}
}
