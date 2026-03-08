package main

import (
	"fmt"
	"os"
	"strings"
)

var vaultContainerBlockedCommands = map[string]struct{}{
	"keypair": {},
	"keygen":  {},
	"status":  {},
	"check":   {},
	"hooks":   {},
	"encrypt": {},
	"decrypt": {},
	"restore": {},
	"set":     {},
	"unset":   {},
	"get":     {},
	"list":    {},
	"ls":      {},
	"run":     {},
}

func vaultGuardContainerLocalAccess(command string) error {
	command = strings.ToLower(strings.TrimSpace(command))
	if command == "" {
		return nil
	}
	if _, blocked := vaultContainerBlockedCommands[command]; !blocked {
		return nil
	}
	if !isSIRuntimeContainerContext() {
		return nil
	}
	return fmt.Errorf(
		"si vault %s is blocked inside SI runtime containers; use `si fort` for runtime secret access",
		command,
	)
}

func isSIRuntimeContainerContext() bool {
	if strings.TrimSpace(os.Getenv("SI_CODEX_PROFILE_ID")) != "" {
		return true
	}
	home := strings.TrimSpace(os.Getenv("HOME"))
	user := strings.TrimSpace(os.Getenv("USER"))
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if home == "/home/si" && (codexHome == "/home/si/.codex" || user == "si") {
		if _, err := os.Stat("/.dockerenv"); err == nil {
			return true
		}
	}
	return false
}
