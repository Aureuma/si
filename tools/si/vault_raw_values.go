package main

import "si/tools/si/internal/vault"

func loadVaultRawValuesFromTargetFile(targetFile string) (map[string]string, error) {
	doc, err := vault.ReadDotenvFile(targetFile)
	if err != nil {
		return nil, err
	}
	entries, err := vault.Entries(doc)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(entries))
	for _, entry := range entries {
		values[entry.Key] = entry.ValueRaw
	}
	return values, nil
}
