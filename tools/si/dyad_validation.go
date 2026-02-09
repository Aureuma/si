package main

import (
	"fmt"
	"strings"
)

func validateDyadSpawnOptionValue(flagName, value string) error {
	flagName = strings.TrimSpace(flagName)
	value = strings.TrimSpace(value)
	if flagName == "" {
		return fmt.Errorf("flag name required")
	}
	if value == "" {
		return fmt.Errorf("missing -%s value", flagName)
	}
	// Common footgun: `--role --department engineering` -> role becomes `--department`.
	if strings.HasPrefix(value, "-") && value != "-" {
		return fmt.Errorf("invalid -%s value %q (looks like another flag)", flagName, value)
	}
	return nil
}

