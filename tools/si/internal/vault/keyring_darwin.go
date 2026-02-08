//go:build darwin

package vault

import (
	"errors"
	"os"
	"strings"

	"github.com/keybase/go-keychain"
)

func keyringGet(service, account string) (string, error) {
	query := keychain.NewItem()
	query.SetSecClass(keychain.SecClassGenericPassword)
	query.SetService(strings.TrimSpace(service))
	query.SetAccount(strings.TrimSpace(account))
	query.SetMatchLimit(keychain.MatchLimitOne)
	query.SetReturnData(true)
	results, err := keychain.QueryItem(query)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "", os.ErrNotExist
	}
	secret := strings.TrimSpace(string(results[0].Data))
	if secret == "" {
		return "", os.ErrNotExist
	}
	return secret, nil
}

func keyringSet(service, account, secret string) error {
	service = strings.TrimSpace(service)
	account = strings.TrimSpace(account)
	secret = strings.TrimSpace(secret)

	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(service)
	item.SetAccount(account)
	item.SetLabel("si-vault age identity")
	item.SetData([]byte(secret))

	if err := keychain.AddItem(item); err != nil {
		if errors.Is(err, keychain.ErrorDuplicateItem) || err == keychain.ErrorDuplicateItem {
			query := keychain.NewItem()
			query.SetSecClass(keychain.SecClassGenericPassword)
			query.SetService(service)
			query.SetAccount(account)

			update := keychain.NewItem()
			update.SetData([]byte(secret))
			return keychain.UpdateItem(query, update)
		}
		return err
	}
	return nil
}
