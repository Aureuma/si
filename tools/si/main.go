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
	case "version", "--version", "-v":
		printVersion()
	case "spawn", "respawn", "list", "ps", "status", "report", "login", "exec", "run", "logs", "tail", "clone", "remove", "rm", "delete", "stop", "start", "warmup":
		if !dispatchCodexCommand(cmd, args) {
			printUnknown("", cmd)
			usage()
			os.Exit(1)
		}
	case "docker":
		cmdDocker(args)
	case "dyad":
		cmdDyad(args)
	case "images":
		cmdImages(args)
	case "image":
		cmdImage(args)
	case "persona":
		cmdPersona(args)
	case "skill":
		cmdSkill(args)
	case "help", "-h", "--help":
		usage()
	default:
		printUnknown("", cmd)
		usage()
		os.Exit(1)
	}
}
