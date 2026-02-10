package vault

import (
	"fmt"
	"strings"
	"unicode"
)

// ValidateEnvName ensures environment names cannot influence path traversal
// when mapped to .env.<name> files.
func ValidateEnvName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("env name required")
	}
	if len(name) > 128 {
		return fmt.Errorf("invalid env name %q: too long", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid env name %q: path separators are not allowed", name)
	}
	for _, r := range name {
		switch r {
		case 0, '\n', '\r':
			return fmt.Errorf("invalid env name %q: contains forbidden character", name)
		}
		if unicode.IsSpace(r) {
			return fmt.Errorf("invalid env name %q: whitespace is not allowed", name)
		}
		if !unicode.IsPrint(r) {
			return fmt.Errorf("invalid env name %q: non-printable character is not allowed", name)
		}
	}
	return nil
}
