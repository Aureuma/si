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
	// Keep a single authoritative vault format version. Header and current
	// ciphertext prefix are intentionally coupled to this value.
	VaultFormatVersionCurrent = "v2"
	VaultHeaderVersionBodyV1  = "si-vault:v1"
	VaultHeaderVersionBody    = "si-vault:" + VaultFormatVersionCurrent
	VaultHeaderVersionLine    = "# " + VaultHeaderVersionBody
	VaultRecipientPrefix      = "# si-vault:recipient "

	EncryptedValuePrefixV1 = "encrypted:si:v1:"
	EncryptedValuePrefixV2 = "encrypted:si:" + VaultFormatVersionCurrent + ":"

	// Legacy compact prefix kept for backwards-compatible decrypt support.
	EncryptedValuePrefixV2Legacy = "es2:"
)

const (
	ageMagicLine          = "age-encryption.org/v1\n"
	ageStanzaX25519Prefix = "-> X25519 "
	ageMACLinePrefix      = "\n--- "
)

func IsEncryptedValueV1(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, EncryptedValuePrefixV1) ||
		strings.HasPrefix(value, EncryptedValuePrefixV2) ||
		strings.HasPrefix(value, EncryptedValuePrefixV2Legacy)
}

func ValidateEncryptedValueV1(ciphertext string) error {
	raw, err := decodeCiphertextPayload(ciphertext)
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(raw, []byte(ageMagicLine)) {
		return fmt.Errorf("invalid ciphertext payload: not age format")
	}
	if !bytes.Contains(raw, []byte(ageMACLinePrefix)) {
		return fmt.Errorf("invalid ciphertext payload: missing age MAC stanza")
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
	raw := buf.Bytes()
	compactPrefix := []byte(ageMagicLine + ageStanzaX25519Prefix)
	if bytes.HasPrefix(raw, compactPrefix) {
		enc := base64.RawURLEncoding.EncodeToString(raw[len(compactPrefix):])
		return EncryptedValuePrefixV2 + enc, nil
	}
	enc := base64.RawURLEncoding.EncodeToString(raw)
	return EncryptedValuePrefixV1 + enc, nil
}

func DecryptStringV1(ciphertext string, identity *age.X25519Identity) (string, error) {
	if identity == nil {
		return "", fmt.Errorf("identity required")
	}
	raw, err := decodeCiphertextPayload(ciphertext)
	if err != nil {
		return "", err
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

func decodeCiphertextPayload(ciphertext string) ([]byte, error) {
	ciphertext = strings.TrimSpace(ciphertext)
	decodeAny := func(payload string) ([]byte, error) {
		// Backwards-compat: older versions (and some users) may have stored ciphertext
		// using StdEncoding/URLEncoding (with padding) rather than RawURLEncoding.
		payload = strings.TrimSpace(payload)
		if payload == "" {
			return nil, fmt.Errorf("invalid ciphertext payload: empty")
		}
		var lastErr error
		for _, enc := range []*base64.Encoding{
			base64.RawURLEncoding,
			base64.URLEncoding,
			base64.RawStdEncoding,
			base64.StdEncoding,
		} {
			raw, err := enc.DecodeString(payload)
			if err == nil {
				return raw, nil
			}
			lastErr = err
		}
		return nil, fmt.Errorf("invalid ciphertext payload: %w", lastErr)
	}
	switch {
	case strings.HasPrefix(ciphertext, EncryptedValuePrefixV2):
		payload := strings.TrimPrefix(ciphertext, EncryptedValuePrefixV2)
		raw, err := decodeAny(payload)
		if err != nil {
			return nil, err
		}
		prefix := []byte(ageMagicLine + ageStanzaX25519Prefix)
		out := make([]byte, 0, len(prefix)+len(raw))
		out = append(out, prefix...)
		out = append(out, raw...)
		return out, nil
	case strings.HasPrefix(ciphertext, EncryptedValuePrefixV2Legacy):
		payload := strings.TrimPrefix(ciphertext, EncryptedValuePrefixV2Legacy)
		raw, err := decodeAny(payload)
		if err != nil {
			return nil, err
		}
		prefix := []byte(ageMagicLine + ageStanzaX25519Prefix)
		out := make([]byte, 0, len(prefix)+len(raw))
		out = append(out, prefix...)
		out = append(out, raw...)
		return out, nil
	case strings.HasPrefix(ciphertext, EncryptedValuePrefixV1):
		payload := strings.TrimPrefix(ciphertext, EncryptedValuePrefixV1)
		raw, err := decodeAny(payload)
		if err != nil {
			return nil, err
		}
		return raw, nil
	default:
		return nil, fmt.Errorf(
			"value is not %s, %s, or %s ciphertext",
			EncryptedValuePrefixV2,
			EncryptedValuePrefixV2Legacy,
			EncryptedValuePrefixV1,
		)
	}
}
