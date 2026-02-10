package vault

import (
	"fmt"
	"strings"
	"unicode"
)

// ValidateKeyName applies conservative safety checks for dotenv keys that may be
// exported into process environments.
func ValidateKeyName(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("key required")
	}
	if len(key) > 512 {
		return fmt.Errorf("invalid key %q: too long", key)
	}
	for _, r := range key {
		switch r {
		case '=', 0, '\n', '\r':
			return fmt.Errorf("invalid key %q: contains forbidden character", key)
		}
		if unicode.IsSpace(r) {
			return fmt.Errorf("invalid key %q: whitespace is not allowed", key)
		}
		if !unicode.IsPrint(r) {
			return fmt.Errorf("invalid key %q: non-printable character is not allowed", key)
		}
	}
	return nil
}
