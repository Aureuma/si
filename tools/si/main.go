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
	if !dispatchRootCommand(cmd, args) {
		printUnknown("", cmd)
		usage()
		os.Exit(1)
	}
}
