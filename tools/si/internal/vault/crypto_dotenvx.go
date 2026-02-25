package vault

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"filippo.io/age"
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
	if plain == "" {
		// Canonical empty-value encoding. ECIES in our stack cannot decrypt
		// ciphertext produced from zero-length plaintext reliably.
		return SIVaultEncryptedPrefix, nil
	}
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
	if IsEncryptedValueV1(ciphertext) {
		return decryptLegacySIVaultAgeValue(ciphertext)
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
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		// Backward-compatibility: older/broken encrypted placeholders such as
		// "encrypted:" or "encrypted:si-vault:" should round-trip as empty.
		return "", nil
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
			if strings.Contains(strings.ToLower(decErr.Error()), "invalid length of message") && len(blob) == 97 {
				// Backward-compatibility for previously emitted ECIES ciphertext for
				// empty plaintext values. Those payloads decode but do not decrypt.
				return "", nil
			}
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

func decryptLegacySIVaultAgeValue(ciphertext string) (string, error) {
	identityRaw := strings.TrimSpace(os.Getenv("SI_VAULT_IDENTITY"))
	if identityRaw == "" {
		return "", fmt.Errorf("legacy si-vault ciphertext requires SI_VAULT_IDENTITY")
	}
	identity, err := age.ParseX25519Identity(identityRaw)
	if err != nil {
		return "", fmt.Errorf("parse SI_VAULT_IDENTITY: %w", err)
	}
	plain, err := DecryptStringV1(ciphertext, identity)
	if err != nil {
		return "", fmt.Errorf("decrypt legacy value: %w", err)
	}
	return plain, nil
}
