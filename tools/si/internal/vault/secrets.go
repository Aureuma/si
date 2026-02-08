package vault

import (
	"fmt"

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
	if len(recipients) == 0 {
		return EncryptResult{}, fmt.Errorf("no recipients found (expected %q lines)", VaultRecipientPrefix)
	}
	result := EncryptResult{}

	for i := range doc.Lines {
		assign, ok := parseAssignment(doc.Lines[i].Text)
		if !ok {
			continue
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
			line := renderAssignment(assign.Leading, assign.Export, assign.Key, next, assign.Comment)
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
		line := renderAssignment(assign.Leading, assign.Export, assign.Key, next, assign.Comment)
		if line != doc.Lines[i].Text {
			doc.Lines[i].Text = line
			result.Changed = true
		}
		result.EncryptedKeys = append(result.EncryptedKeys, assign.Key)
	}

	return result, nil
}
