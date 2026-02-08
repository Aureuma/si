//go:build linux

package vault

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
)

func keyringGet(service, account string) (string, error) {
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return "", os.ErrNotExist
	}
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
	cmd := exec.Command("secret-tool", "store", "--label=si-vault age identity", "service", strings.TrimSpace(service), "account", strings.TrimSpace(account))
	cmd.Stdin = bytes.NewBufferString(secret)
	return cmd.Run()
}
