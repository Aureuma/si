//go:build darwin

package vault

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode"
)

func keyringGet(service, account string) (string, error) {
	if _, err := exec.LookPath("security"); err != nil {
		return "", os.ErrNotExist
	}
	service, err := validateSecurityAttr("service", service)
	if err != nil {
		return "", err
	}
	account, err = validateSecurityAttr("account", account)
	if err != nil {
		return "", err
	}
	// #nosec G204 -- args are validated and exec.Command does not invoke a shell.
	cmd := exec.Command("security", "find-generic-password", "-s", strings.TrimSpace(service), "-a", strings.TrimSpace(account), "-w")
	out, err := cmd.Output()
	if err != nil {
		if isSecurityNotFound(err) {
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
	service = strings.TrimSpace(service)
	account = strings.TrimSpace(account)
	secret = strings.TrimSpace(secret)
	if _, err := exec.LookPath("security"); err != nil {
		return err
	}
	var err error
	service, err = validateSecurityAttr("service", service)
	if err != nil {
		return err
	}
	account, err = validateSecurityAttr("account", account)
	if err != nil {
		return err
	}
	// Delete existing value first; ignore not-found.
	// #nosec G204 -- args are validated and exec.Command does not invoke a shell.
	delCmd := exec.Command("security", "delete-generic-password", "-s", strings.TrimSpace(service), "-a", strings.TrimSpace(account))
	if delErr := delCmd.Run(); delErr != nil && !isSecurityNotFound(delErr) {
		return delErr
	}
	// #nosec G204 -- args are validated and exec.Command does not invoke a shell.
	setCmd := exec.Command(
		"security",
		"add-generic-password",
		"-s", strings.TrimSpace(service),
		"-a", strings.TrimSpace(account),
		"-l", "si-vault age identity",
		"-w", secret,
		"-U",
	)
	return setCmd.Run()
}

func validateSecurityAttr(name, value string) (string, error) {
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

func isSecurityNotFound(err error) bool {
	if err == nil {
		return false
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return false
	}
	stderr := strings.ToLower(strings.TrimSpace(string(exitErr.Stderr)))
	return strings.Contains(stderr, "could not be found") || strings.Contains(stderr, "not found")
}
