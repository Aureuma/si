package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

func requireCloudflareConfirmation(action string, force bool) error {
	if force {
		return nil
	}
	action = strings.TrimSpace(action)
	if action == "" {
		action = "continue"
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return fmt.Errorf("refusing to %s without confirmation in non-interactive mode; use --force", action)
	}
	fmt.Printf("%s ", styleWarn(fmt.Sprintf("Confirm %s? type `yes` to continue (Esc to cancel):", action)))
	line, err := promptLine(os.Stdin)
	if err != nil {
		return err
	}
	if isEscCancelInput(line) {
		return fmt.Errorf("operation canceled")
	}
	if strings.EqualFold(strings.TrimSpace(line), "yes") {
		return nil
	}
	return fmt.Errorf("operation canceled")
}
