package vault

import (
	"fmt"
	"strconv"
	"strings"
)

func NormalizeDotenvValue(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if raw[0] == '\'' && (len(raw) < 2 || raw[len(raw)-1] != '\'') {
		return "", fmt.Errorf("invalid quoted value: missing closing single quote")
	}
	if raw[0] == '"' && (len(raw) < 2 || raw[len(raw)-1] != '"') {
		return "", fmt.Errorf("invalid quoted value: missing closing double quote")
	}
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return raw[1 : len(raw)-1], nil
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		out, err := strconv.Unquote(raw)
		if err != nil {
			return "", fmt.Errorf("invalid quoted value: %w", err)
		}
		return out, nil
	}
	return raw, nil
}

// RenderDotenvValuePlain formats a plaintext value so that ParseDotenv+NormalizeDotenvValue
// will read back the same value (especially around '#' comment parsing).
//
// This intentionally does not aim to preserve original quoting style; encryption
// normalizes away the original representation.
func RenderDotenvValuePlain(value string) string {
	if value == "" {
		return ""
	}
	if needsDotenvQuotes(value) {
		return strconv.Quote(value)
	}
	return value
}

func needsDotenvQuotes(value string) bool {
	if value == "" {
		return false
	}
	if value[0] == '#' {
		return true
	}
	// Leading/trailing whitespace would be trimmed by parsers and/or our own normalizer.
	if strings.HasPrefix(value, " ") || strings.HasPrefix(value, "\t") || strings.HasSuffix(value, " ") || strings.HasSuffix(value, "\t") {
		return true
	}
	// Preserve literal newlines safely.
	if strings.ContainsAny(value, "\r\n") {
		return true
	}
	// Our dotenv parser treats " <ws>#..." as an inline comment delimiter.
	for i := 1; i < len(value); i++ {
		if value[i] == '#' && (value[i-1] == ' ' || value[i-1] == '\t') {
			return true
		}
	}
	return false
}
