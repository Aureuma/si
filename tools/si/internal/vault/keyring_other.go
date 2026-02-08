//go:build !darwin && !linux

package vault

import (
	"fmt"
	"os"
)

func keyringGet(service, account string) (string, error) {
	_ = service
	_ = account
	return "", os.ErrNotExist
}

func keyringSet(service, account, secret string) error {
	_ = secret
	return fmt.Errorf("keyring backend not supported on this platform (service=%q account=%q)", service, account)
}
