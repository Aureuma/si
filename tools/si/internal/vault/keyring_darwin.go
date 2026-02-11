//go:build darwin

package vault

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
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
		// The security tool often returns just an exit code (sometimes with no stderr),
		// which is otherwise surfaced as a useless "exit status N".
		return "", fmt.Errorf("macOS Keychain read failed for %q/%q: %s", service, account, formatSecurityError(err))
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
		return fmt.Errorf("macOS Keychain delete failed for %q/%q: %s", service, account, formatSecurityError(delErr))
	}
	// #nosec G204 -- args are validated and exec.Command does not invoke a shell.
	setCmd := exec.Command(
		"security",
		"add-generic-password",
		"-s", strings.TrimSpace(service),
		"-a", strings.TrimSpace(account),
		"-l", "si-vault age identity",
		"-w", secret,
		// Ensure /usr/bin/security can read this item without prompting.
		// Without this, `security find-generic-password -w` can fail with an opaque
		// "exit status 169" on some hosts.
		"-T", "/usr/bin/security",
		"-U",
	)
	if err := setCmd.Run(); err != nil {
		return fmt.Errorf("macOS Keychain write failed for %q/%q: %s", service, account, formatSecurityError(err))
	}
	return nil
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
	// Known exit code for "item not found" from `security find-generic-password`.
	// Still keep stderr parsing to handle variants and other subcommands.
	if code, ok := securityExitCode(exitErr); ok && code == 44 {
		return true
	}
	stderr := strings.ToLower(strings.TrimSpace(string(exitErr.Stderr)))
	return strings.Contains(stderr, "could not be found") || strings.Contains(stderr, "not found")
}

func formatSecurityError(err error) string {
	if err == nil {
		return ""
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return err.Error()
	}
	if code, ok := securityExitCode(exitErr); ok {
		stderr := strings.TrimSpace(string(exitErr.Stderr))
		if stderr == "" {
			if code == 169 {
				return fmt.Sprintf("exit status %d (Keychain item exists but can't be read in this context; check Keychain Access item Access Control for %q/%q, or delete it and re-run `si vault keygen`)", code, KeyringService, KeyringAccount)
			}
			return fmt.Sprintf("exit status %d", code)
		}
		return fmt.Sprintf("exit status %d: %s", code, stderr)
	}
	stderr := strings.TrimSpace(string(exitErr.Stderr))
	if stderr == "" {
		return err.Error()
	}
	return fmt.Sprintf("%s: %s", err.Error(), stderr)
}

func securityExitCode(exitErr *exec.ExitError) (int, bool) {
	if exitErr == nil {
		return 0, false
	}
	if exitErr.ProcessState != nil {
		return exitErr.ExitCode(), true
	}
	// Fallback: parse "exit status N".
	parts := strings.Fields(strings.TrimSpace(exitErr.Error()))
	if len(parts) >= 3 && parts[0] == "exit" && parts[1] == "status" {
		if n, err := strconv.Atoi(parts[2]); err == nil {
			return n, true
		}
	}
	return 0, false
}
