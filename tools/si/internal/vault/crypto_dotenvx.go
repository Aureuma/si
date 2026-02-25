package vault

import (
	"encoding/base64"
	"fmt"
	"strings"

	ecies "github.com/ecies/go/v2"
)

const (
	SIVaultPublicKeyName  = "SI_VAULT_PUBLIC_KEY"
	SIVaultPrivateKeyName = "SI_VAULT_PRIVATE_KEY"

	SIVaultEncryptedPrefix = "encrypted:si-vault:"
	dotenvxEncryptedPrefix = "encrypted:"
)

func GenerateSIVaultKeyPair() (publicKeyHex string, privateKeyHex string, err error) {
	key, err := ecies.GenerateKey()
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(key.PublicKey.Hex(true)), strings.TrimSpace(key.Hex()), nil
}

func IsSIVaultEncryptedValue(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, SIVaultEncryptedPrefix) || strings.HasPrefix(trimmed, dotenvxEncryptedPrefix)
}

func EncryptSIVaultValue(plain string, publicKeyHex string) (string, error) {
	publicKeyHex = strings.TrimSpace(publicKeyHex)
	if publicKeyHex == "" {
		return "", fmt.Errorf("public key is required")
	}
	pub, err := ecies.NewPublicKeyFromHex(publicKeyHex)
	if err != nil {
		return "", fmt.Errorf("parse public key: %w", err)
	}
	cipher, err := ecies.Encrypt(pub, []byte(plain))
	if err != nil {
		return "", fmt.Errorf("encrypt value: %w", err)
	}
	return SIVaultEncryptedPrefix + base64.StdEncoding.EncodeToString(cipher), nil
}

func DecryptSIVaultValue(ciphertext string, privateKeyHexes []string) (string, error) {
	ciphertext = strings.TrimSpace(ciphertext)
	if ciphertext == "" {
		return "", fmt.Errorf("ciphertext is empty")
	}
	encoded := ""
	switch {
	case strings.HasPrefix(ciphertext, SIVaultEncryptedPrefix):
		encoded = strings.TrimPrefix(ciphertext, SIVaultEncryptedPrefix)
	case strings.HasPrefix(ciphertext, dotenvxEncryptedPrefix):
		encoded = strings.TrimPrefix(ciphertext, dotenvxEncryptedPrefix)
	default:
		return "", fmt.Errorf("value is not encrypted")
	}
	blob, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	var lastErr error
	for _, candidate := range privateKeyHexes {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		priv, parseErr := ecies.NewPrivateKeyFromHex(candidate)
		if parseErr != nil {
			lastErr = fmt.Errorf("parse private key: %w", parseErr)
			continue
		}
		plain, decErr := ecies.Decrypt(priv, blob)
		if decErr != nil {
			lastErr = fmt.Errorf("decrypt value: %w", decErr)
			continue
		}
		return string(plain), nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("private key is required")
	}
	return "", lastErr
}
