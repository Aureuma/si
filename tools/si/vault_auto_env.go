package main

import (
	"os"
	"sort"
	"strings"
)

const siVaultAutoEnvKey = "SI_VAULT_AUTO_ENV"

var autoVaultEnvRootCommands = map[string]struct{}{
	"stripe":       {},
	"github":       {},
	"cloudflare":   {},
	"cf":           {},
	"google":       {},
	"apple":        {},
	"social":       {},
	"workos":       {},
	"aws":          {},
	"gcp":          {},
	"openai":       {},
	"oci":          {},
	"image":        {},
	"images":       {},
	"publish":      {},
	"pub":          {},
	"providers":    {},
	"provider":     {},
	"integrations": {},
	"apis":         {},
	"paas":         {},
}

func maybeAutoHydrateVaultEnvForRootCommand(cmd string) {
	if !vaultAutoHydrationEnabled() {
		return
	}
	if !shouldAutoHydrateVaultEnvForRootCommand(cmd) {
		return
	}
	settings := loadVaultSettingsOrFail()
	_, _ = hydrateProcessEnvFromSunVault(settings, "root:"+strings.TrimSpace(cmd))
}

func vaultAutoHydrationEnabled() bool {
	raw := strings.TrimSpace(os.Getenv(siVaultAutoEnvKey))
	if raw == "" {
		return true
	}
	switch strings.ToLower(raw) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func shouldAutoHydrateVaultEnvForRootCommand(cmd string) bool {
	_, ok := autoVaultEnvRootCommands[strings.ToLower(strings.TrimSpace(cmd))]
	return ok
}

func hydrateProcessEnvFromSunVault(settings Settings, source string) (int, error) {
	_ = source
	target, err := vaultResolveTarget(settings, "", true)
	if err != nil {
		return 0, err
	}
	rawValues, supported, err := vaultSunKVLoadRawValues(settings, target)
	if err != nil {
		return 0, err
	}
	if !supported || len(rawValues) == 0 {
		return 0, nil
	}

	keys := make([]string, 0, len(rawValues))
	for key := range rawValues {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	setCount := 0
	for _, key := range keys {
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		value, ok := resolveVaultRawValue(settings, target.File, rawValues[key])
		if !ok {
			continue
		}
		if setErr := os.Setenv(key, value); setErr != nil {
			continue
		}
		setCount++
	}

	return setCount, nil
}
