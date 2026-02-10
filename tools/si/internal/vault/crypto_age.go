package vault

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"

	"filippo.io/age"
)

const (
	VaultHeaderVersionLine = "# si-vault:v1"
	VaultRecipientPrefix   = "# si-vault:recipient "

	EncryptedValuePrefixV1 = "encrypted:si:v1:"
)

func IsEncryptedValueV1(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), EncryptedValuePrefixV1)
}

func ValidateEncryptedValueV1(ciphertext string) error {
	ciphertext = strings.TrimSpace(ciphertext)
	if !strings.HasPrefix(ciphertext, EncryptedValuePrefixV1) {
		return fmt.Errorf("value is not %s ciphertext", EncryptedValuePrefixV1)
	}
	payload := strings.TrimPrefix(ciphertext, EncryptedValuePrefixV1)
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return fmt.Errorf("invalid ciphertext payload: %w", err)
	}
	const ageMagic = "age-encryption.org/v1"
	if len(raw) < len(ageMagic) || string(raw[:len(ageMagic)]) != ageMagic {
		return fmt.Errorf("invalid ciphertext payload: not age format")
	}
	return nil
}

func ParseRecipientsFromDotenv(f DotenvFile) []string {
	out := []string{}
	for _, line := range f.Lines {
		recipient, ok := parseRecipientLine(line.Text)
		if !ok {
			continue
		}
		out = append(out, recipient)
	}
	return out
}

func RecipientsFingerprint(recipients []string) string {
	uniq := map[string]struct{}{}
	for _, r := range recipients {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		uniq[r] = struct{}{}
	}
	sorted := make([]string, 0, len(uniq))
	for r := range uniq {
		sorted = append(sorted, r)
	}
	sort.Strings(sorted)

	h := sha256.New()
	for _, r := range sorted {
		_, _ = h.Write([]byte(r))
		_, _ = h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func EncryptStringV1(plaintext string, recipientStrs []string) (string, error) {
	recipients := make([]age.Recipient, 0, len(recipientStrs))
	for _, r := range recipientStrs {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		parsed, err := age.ParseX25519Recipient(r)
		if err != nil {
			return "", fmt.Errorf("invalid recipient %q: %w", r, err)
		}
		recipients = append(recipients, parsed)
	}
	if len(recipients) == 0 {
		return "", fmt.Errorf("no recipients configured (missing %q header lines)", VaultRecipientPrefix)
	}

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipients...)
	if err != nil {
		return "", err
	}
	if _, err := io.WriteString(w, plaintext); err != nil {
		_ = w.Close()
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding.EncodeToString(buf.Bytes())
	return EncryptedValuePrefixV1 + enc, nil
}

func DecryptStringV1(ciphertext string, identity *age.X25519Identity) (string, error) {
	if identity == nil {
		return "", fmt.Errorf("identity required")
	}
	ciphertext = strings.TrimSpace(ciphertext)
	if !strings.HasPrefix(ciphertext, EncryptedValuePrefixV1) {
		return "", fmt.Errorf("value is not %s ciphertext", EncryptedValuePrefixV1)
	}
	payload := strings.TrimPrefix(ciphertext, EncryptedValuePrefixV1)
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("invalid ciphertext payload: %w", err)
	}
	const ageMagic = "age-encryption.org/v1"
	if len(raw) < len(ageMagic) || string(raw[:len(ageMagic)]) != ageMagic {
		return "", fmt.Errorf("invalid ciphertext payload: not age format")
	}
	r, err := age.Decrypt(bytes.NewReader(raw), identity)
	if err != nil {
		return "", err
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func GenerateIdentity() (*age.X25519Identity, error) {
	return age.GenerateX25519Identity()
}
