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
	if raw[0] == '\'' && raw[len(raw)-1] != '\'' {
		return "", fmt.Errorf("invalid quoted value: missing closing single quote")
	}
	if raw[0] == '"' && raw[len(raw)-1] != '"' {
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
