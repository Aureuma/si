package vault

type DotenvEncryptionScan struct {
	EncryptedKeys []string
	PlaintextKeys []string
	EmptyKeys     []string
}

// ScanDotenvEncryption inspects a dotenv file and classifies assignments as encrypted/plaintext/empty.
// It does not decrypt ciphertext and does not require an identity.
func ScanDotenvEncryption(doc DotenvFile) (DotenvEncryptionScan, error) {
	scan := DotenvEncryptionScan{}
	for _, line := range doc.Lines {
		assign, ok := parseAssignment(line.Text)
		if !ok {
			continue
		}
		if err := ValidateKeyName(assign.Key); err != nil {
			return DotenvEncryptionScan{}, err
		}
		val, err := NormalizeDotenvValue(assign.ValueRaw)
		if err != nil {
			return DotenvEncryptionScan{}, err
		}
		if val == "" {
			scan.EmptyKeys = append(scan.EmptyKeys, assign.Key)
			continue
		}
		if IsEncryptedValueV1(val) {
			if err := ValidateEncryptedValueV1(val); err != nil {
				return DotenvEncryptionScan{}, err
			}
			scan.EncryptedKeys = append(scan.EncryptedKeys, assign.Key)
			continue
		}
		scan.PlaintextKeys = append(scan.PlaintextKeys, assign.Key)
	}
	return scan, nil
}
