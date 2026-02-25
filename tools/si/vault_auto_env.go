package main

import (
	"os"
	"sort"
	"strings"

	"si/tools/si/internal/vault"
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
	settings := loadSettingsOrDefault()
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
	target, err := resolveSIVaultTarget("", "", "")
	if err != nil {
		return 0, err
	}
	doc, err := vault.ReadDotenvFile(target.EnvFile)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	material, err := ensureSIVaultKeyMaterial(settings, target)
	if err != nil {
		return 0, err
	}
	values, _, err := decryptDotenvValues(doc, siVaultPrivateKeyCandidates(material))
	if err != nil {
		return 0, err
	}
	if len(values) == 0 {
		return 0, nil
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	setCount := 0
	for _, key := range keys {
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if setErr := os.Setenv(key, values[key]); setErr != nil {
			continue
		}
		setCount++
	}

	return setCount, nil
}
