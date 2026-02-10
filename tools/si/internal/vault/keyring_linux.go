//go:build linux

package vault

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode"
)

func keyringGet(service, account string) (string, error) {
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return "", os.ErrNotExist
	}
	service, err := validateSecretToolAttr("service", service)
	if err != nil {
		return "", err
	}
	account, err = validateSecretToolAttr("account", account)
	if err != nil {
		return "", err
	}
	// #nosec G204 -- args are validated and exec.Command does not invoke a shell.
	cmd := exec.Command("secret-tool", "lookup", "service", strings.TrimSpace(service), "account", strings.TrimSpace(account))
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", os.ErrNotExist
		}
		return "", err
	}
	secret := strings.TrimSpace(string(out))
	if secret == "" {
		return "", os.ErrNotExist
	}
	return secret, nil
}

func keyringSet(service, account, secret string) error {
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return err
	}
	var err error
	service, err = validateSecretToolAttr("service", service)
	if err != nil {
		return err
	}
	account, err = validateSecretToolAttr("account", account)
	if err != nil {
		return err
	}
	// #nosec G204 -- args are validated and exec.Command does not invoke a shell.
	cmd := exec.Command("secret-tool", "store", "--label=si-vault age identity", "service", strings.TrimSpace(service), "account", strings.TrimSpace(account))
	cmd.Stdin = bytes.NewBufferString(secret)
	return cmd.Run()
}

func validateSecretToolAttr(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s required", name)
	}
	for _, r := range value {
		switch r {
		case 0, '\n', '\r':
			return "", fmt.Errorf("invalid %s: contains forbidden character", name)
		}
		if unicode.IsSpace(r) {
			return "", fmt.Errorf("invalid %s: whitespace is not allowed", name)
		}
		if !unicode.IsPrint(r) {
			return "", fmt.Errorf("invalid %s: non-printable character is not allowed", name)
		}
	}
	return value, nil
}
