package vault

import (
	"fmt"
	"sort"
	"strings"

	"filippo.io/age"
)

type DecryptResult struct {
	Values        map[string]string
	DecryptedKeys []string
	PlaintextKeys []string
}

func DecryptEnv(doc DotenvFile, identity *age.X25519Identity) (DecryptResult, error) {
	values := map[string]string{}
	decrypted := []string{}
	plaintext := []string{}

	for _, line := range doc.Lines {
		assign, ok := parseAssignment(line.Text)
		if !ok {
			continue
		}
		if err := ValidateKeyName(assign.Key); err != nil {
			return DecryptResult{}, err
		}
		raw := assign.ValueRaw
		val, err := NormalizeDotenvValue(raw)
		if err != nil {
			return DecryptResult{}, err
		}
		if IsEncryptedValueV1(val) {
			if identity == nil {
				return DecryptResult{}, fmt.Errorf("decrypt requires a vault identity (set SI_VAULT_IDENTITY or configure vault.key_backend)")
			}
			plain, err := DecryptStringV1(val, identity)
			if err != nil {
				return DecryptResult{}, err
			}
			values[assign.Key] = plain
			decrypted = append(decrypted, assign.Key)
			continue
		}
		values[assign.Key] = val
		plaintext = append(plaintext, assign.Key)
	}

	return DecryptResult{
		Values:        values,
		DecryptedKeys: decrypted,
		PlaintextKeys: plaintext,
	}, nil
}

type EncryptResult struct {
	Changed          bool
	EncryptedKeys    []string
	ReencryptedKeys  []string
	SkippedEncrypted int
}

func EncryptDotenvValues(doc *DotenvFile, identity *age.X25519Identity, reencrypt bool) (EncryptResult, error) {
	if doc == nil {
		return EncryptResult{}, nil
	}
	recipients := ParseRecipientsFromDotenv(*doc)
	return EncryptDotenvValuesWithRecipients(doc, recipients, identity, reencrypt)
}

func EncryptDotenvValuesWithRecipients(doc *DotenvFile, recipients []string, identity *age.X25519Identity, reencrypt bool) (EncryptResult, error) {
	if doc == nil {
		return EncryptResult{}, nil
	}
	if len(recipients) == 0 {
		return EncryptResult{}, fmt.Errorf("no recipients found (expected %q lines)", VaultRecipientPrefix)
	}
	result := EncryptResult{}

	for i := range doc.Lines {
		assign, ok := parseAssignment(doc.Lines[i].Text)
		if !ok {
			continue
		}
		if err := ValidateKeyName(assign.Key); err != nil {
			return EncryptResult{}, err
		}
		val, err := NormalizeDotenvValue(assign.ValueRaw)
		if err != nil {
			return EncryptResult{}, err
		}
		if IsEncryptedValueV1(val) {
			if !reencrypt {
				result.SkippedEncrypted++
				continue
			}
			if identity == nil {
				return EncryptResult{}, fmt.Errorf("reencrypt requires a vault identity (set SI_VAULT_IDENTITY or configure vault.key_backend)")
			}
			plain, err := DecryptStringV1(val, identity)
			if err != nil {
				return EncryptResult{}, err
			}
			next, err := EncryptStringV1(plain, recipients)
			if err != nil {
				return EncryptResult{}, err
			}
			line := renderAssignmentPreserveLayout(assign, assign.Key, next, assign.Comment)
			if line != doc.Lines[i].Text {
				doc.Lines[i].Text = line
				result.Changed = true
			}
			result.ReencryptedKeys = append(result.ReencryptedKeys, assign.Key)
			continue
		}
		next, err := EncryptStringV1(val, recipients)
		if err != nil {
			return EncryptResult{}, err
		}
		line := renderAssignmentPreserveLayout(assign, assign.Key, next, assign.Comment)
		if line != doc.Lines[i].Text {
			doc.Lines[i].Text = line
			result.Changed = true
		}
		result.EncryptedKeys = append(result.EncryptedKeys, assign.Key)
	}

	return result, nil
}

type DecryptDotenvResult struct {
	Changed        bool
	DecryptedKeys  []string
	SkippedPlain   int
	SkippedMissing int
	MissingKeys    []string
}

func normalizeKeyFilter(keys []string) map[string]struct{} {
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		out[k] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// DecryptDotenvValues decrypts encrypted values in-place, preserving line layout/comments.
// It keeps the vault header (recipients) intact so the file can be re-encrypted later.
func DecryptDotenvValues(doc *DotenvFile, identity *age.X25519Identity) (DecryptDotenvResult, error) {
	return DecryptDotenvKeys(doc, identity, nil)
}

// DecryptDotenvKeys decrypts only the selected keys (when keys is non-empty).
// When keys is empty/nil, it decrypts all encrypted keys (same behavior as DecryptDotenvValues).
func DecryptDotenvKeys(doc *DotenvFile, identity *age.X25519Identity, keys []string) (DecryptDotenvResult, error) {
	if doc == nil {
		return DecryptDotenvResult{}, nil
	}
	filter := normalizeKeyFilter(keys)
	found := map[string]struct{}{}
	result := DecryptDotenvResult{}
	for i := range doc.Lines {
		assign, ok := parseAssignment(doc.Lines[i].Text)
		if !ok {
			continue
		}
		if err := ValidateKeyName(assign.Key); err != nil {
			return DecryptDotenvResult{}, err
		}
		if filter != nil {
			if _, ok := filter[assign.Key]; !ok {
				continue
			}
		}
		val, err := NormalizeDotenvValue(assign.ValueRaw)
		if err != nil {
			return DecryptDotenvResult{}, err
		}
		if !IsEncryptedValueV1(val) {
			result.SkippedPlain++
			continue
		}
		if identity == nil {
			return DecryptDotenvResult{}, fmt.Errorf("decrypt requires a vault identity (set SI_VAULT_IDENTITY or configure vault.key_backend)")
		}
		plain, err := DecryptStringV1(val, identity)
		if err != nil {
			return DecryptDotenvResult{}, err
		}
		rendered := RenderDotenvValuePlain(plain)
		line := renderAssignmentPreserveLayout(assign, assign.Key, rendered, assign.Comment)
		if line != doc.Lines[i].Text {
			doc.Lines[i].Text = line
			result.Changed = true
		}
		result.DecryptedKeys = append(result.DecryptedKeys, assign.Key)
		if filter != nil {
			found[assign.Key] = struct{}{}
		}
	}

	if filter != nil {
		for k := range filter {
			if _, ok := found[k]; ok {
				continue
			}
			result.SkippedMissing++
			result.MissingKeys = append(result.MissingKeys, k)
		}
		sort.Strings(result.MissingKeys)
	}
	return result, nil
}
