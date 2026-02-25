package main

import (
	"os"
	"sort"
	"strings"

	"filippo.io/age"

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
	backend, err := resolveVaultSyncBackend(settings)
	if err != nil {
		return 0, err
	}
	if backend.Mode != vaultSyncBackendSun {
		return 0, nil
	}

	target, err := vaultResolveTargetStatus(settings, "")
	if err != nil {
		return 0, err
	}
	values, used, err := vaultSunKVLoadRawValues(settings, target)
	if err != nil {
		return 0, err
	}
	if !used || len(values) == 0 {
		return 0, nil
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	identityLoaded := false
	var identity *age.X25519Identity

	setCount := 0
	for _, key := range keys {
		raw := values[key]
		normalized, normalizeErr := vault.NormalizeDotenvValue(raw)
		if normalizeErr != nil {
			continue
		}

		plain := normalized
		if vault.IsEncryptedValueV1(normalized) {
			if !identityLoaded {
				id, identityErr := vaultEnsureStrictSunIdentity(settings, source)
				if identityErr == nil {
					identity = id
				}
				identityLoaded = true
			}
			if identity == nil {
				continue
			}
			decrypted, decryptErr := vault.DecryptStringV1(normalized, identity)
			if decryptErr != nil {
				continue
			}
			plain = decrypted
		}

		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if setErr := os.Setenv(key, plain); setErr != nil {
			continue
		}
		setCount++
	}

	return setCount, nil
}
