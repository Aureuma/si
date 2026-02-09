package main

import (
	"fmt"
	"os"
	"strings"
)

// confirmYN prompts for a y/n confirmation. Empty input returns the default.
// Returns (confirmed, ok). ok=false means canceled (Esc) or non-interactive.
func confirmYN(prompt string, defaultYes bool) (bool, bool) {
	if !isInteractiveTerminal() {
		return false, false
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "Confirm"
	}
	def := "N"
	if defaultYes {
		def = "Y"
	}
	for {
		fmt.Fprintf(os.Stdout, "%s [y/%s]: ", prompt, def)
		line, err := promptLine(os.Stdin)
		if err != nil {
			fatal(err)
		}
		if isEscCancelInput(line) {
			return false, false
		}
		line = strings.TrimSpace(strings.ToLower(line))
		if line == "" {
			return defaultYes, true
		}
		switch line {
		case "y", "yes":
			return true, true
		case "n", "no":
			return false, true
		default:
			fmt.Fprintln(os.Stdout, styleDim("please answer y or n (Esc to cancel)"))
		}
	}
}
