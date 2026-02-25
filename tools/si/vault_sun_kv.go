package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"si/tools/si/internal/vault"
)

const (
	sunVaultKVKindPrefix      = "vault_kv."
	sunVaultKVMetadataVersion = 1
	sunVaultKVListLimit       = 500
	sunObjectKeyMaxLen        = 128
	sunVaultKVScopedHashChars = 16
)

func vaultSunKVKind(target vault.Target) string {
	return vaultSunKVKindForScope(vaultSunKVScope(target))
}

func vaultSunKVScope(target vault.Target) string {
	scope := vaultNormalizeScope(target.File)
	if strings.TrimSpace(scope) == "" {
		return defaultVaultScope
	}
	return scope
}

func vaultSunKVKindForScope(scope string) string {
	scope = vaultNormalizeScope(scope)
	if strings.TrimSpace(scope) == "" {
		scope = defaultVaultScope
	}
	if !strings.Contains(scope, "/") {
		kind := sunVaultKVKindPrefix + scope
		if len(kind) <= sunObjectKeyMaxLen {
			return kind
		}
		// Defensive fallback; normalized non-namespaced scopes should already fit.
		scope = strings.Trim(scope, "-_/.:")
		if len(scope) > maxVaultScopeLen {
			scope = strings.Trim(scope[:maxVaultScopeLen], "-_/.:")
		}
		if scope == "" {
			scope = defaultVaultScope
		}
		return sunVaultKVKindPrefix + scope
	}

	// Scoped namespaces (repo/env) retain readability while adding a hash suffix
	// to avoid collisions when normalized/truncated.
	human := strings.ReplaceAll(scope, "/", ".")
	sum := sha256.Sum256([]byte(scope))
	hashSuffix := "s." + hex.EncodeToString(sum[:])[:sunVaultKVScopedHashChars]
	maxHumanLen := sunObjectKeyMaxLen - len(sunVaultKVKindPrefix) - 1 - len(hashSuffix)
	if maxHumanLen < 1 {
		return sunVaultKVKindPrefix + hashSuffix
	}
	if len(human) > maxHumanLen {
		human = strings.Trim(human[:maxHumanLen], "-_.:")
		if human == "" {
			return sunVaultKVKindPrefix + hashSuffix
		}
	}
	return sunVaultKVKindPrefix + human + "." + hashSuffix
}

func vaultSunKVTargetHashes(target vault.Target) (repoHash string, fileHash string) {
	repo := strings.TrimSpace(target.RepoRoot)
	file := vaultSunKVScope(target)
	repoSum := sha256.Sum256([]byte(repo))
	fileSum := sha256.Sum256([]byte(file))
	return hex.EncodeToString(repoSum[:8]), hex.EncodeToString(fileSum[:8])
}

func vaultSunKVClient(settings Settings) (*sunClient, bool, error) {
	backend, err := resolveVaultSyncBackend(settings)
	if err != nil {
		return nil, false, err
	}
	if backend.Mode != vaultSyncBackendSun {
		return nil, false, nil
	}
	strict := vaultSyncBackendStrictSun(settings, backend)
	client, err := sunClientFromSettings(settings)
	if err != nil {
		if strict {
			return nil, strict, err
		}
		return nil, strict, nil
	}
	return client, strict, nil
}

func vaultSunKVMetaBool(meta map[string]interface{}, key string) bool {
	if meta == nil {
		return false
	}
	value, ok := meta[key]
	if !ok {
		return false
	}
	return isTruthyFlagValue(formatAny(value))
}

func vaultSunKVLoadRawValues(settings Settings, target vault.Target) (map[string]string, bool, error) {
	client, strict, err := vaultSunKVClient(settings)
	if err != nil {
		return nil, true, fmt.Errorf("sun vault key read failed: %w", err)
	}
	if client == nil {
		return nil, false, nil
	}

	kind := vaultSunKVKind(target)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	items, err := client.listObjects(ctx, kind, "", sunVaultKVListLimit)
	if err != nil {
		if strict {
			return nil, true, fmt.Errorf("sun vault key list failed: %w", err)
		}
		warnf("sun vault key list skipped: %v", err)
		return nil, false, nil
	}
	if len(items) == 0 {
		return nil, true, nil
	}

	keys := make([]string, 0, len(items))
	metaByKey := map[string]sunObjectMeta{}
	for _, item := range items {
		key := strings.TrimSpace(item.Name)
		if key == "" {
			continue
		}
		if vaultSunKVMetaBool(item.Metadata, "deleted") {
			continue
		}
		keys = append(keys, key)
		metaByKey[key] = item
	}
	sort.Strings(keys)
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		payload, getErr := client.getPayload(ctx, kind, key)
		if getErr != nil {
			if strict {
				return nil, true, fmt.Errorf("sun vault key payload read failed (%s): %w", key, getErr)
			}
			warnf("sun vault key payload read skipped (%s): %v", key, getErr)
			continue
		}
		raw := strings.TrimSpace(string(payload))
		if raw == "" && !vaultSunKVMetaBool(metaByKey[key].Metadata, "allow_empty") {
			continue
		}
		values[key] = raw
	}
	return values, true, nil
}

func vaultSunKVGetRawValue(settings Settings, target vault.Target, key string) (string, bool, bool, error) {
	client, strict, err := vaultSunKVClient(settings)
	if err != nil {
		return "", false, true, fmt.Errorf("sun vault key read failed: %w", err)
	}
	if client == nil {
		return "", false, false, nil
	}
	kind := vaultSunKVKind(target)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	items, err := client.listObjects(ctx, kind, key, 1)
	if err != nil {
		if strict {
			return "", false, true, fmt.Errorf("sun vault key metadata read failed (%s): %w", key, err)
		}
		warnf("sun vault key metadata read skipped (%s): %v", key, err)
		return "", false, false, nil
	}
	if len(items) == 0 {
		return "", false, true, nil
	}
	item := items[0]
	if !strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(key)) {
		return "", false, true, nil
	}
	if vaultSunKVMetaBool(item.Metadata, "deleted") {
		return "", false, true, nil
	}
	payload, err := client.getPayload(ctx, kind, key)
	if err != nil {
		if strict {
			return "", false, true, fmt.Errorf("sun vault key payload read failed (%s): %w", key, err)
		}
		warnf("sun vault key payload read skipped (%s): %v", key, err)
		return "", false, false, nil
	}
	return strings.TrimSpace(string(payload)), true, true, nil
}

func vaultSunKVPutRawValue(settings Settings, target vault.Target, key string, rawValue string, source string, deleted bool) error {
	client, strict, err := vaultSunKVClient(settings)
	if err != nil {
		return fmt.Errorf("sun vault key write failed: %w", err)
	}
	if client == nil {
		return nil
	}
	if err := vault.ValidateKeyName(key); err != nil {
		return err
	}
	kind := vaultSunKVKind(target)
	repoHash, fileHash := vaultSunKVTargetHashes(target)
	changedAt := time.Now().UTC().Format(time.RFC3339Nano)
	metadata := map[string]interface{}{
		"version":      sunVaultKVMetadataVersion,
		"scope":        vaultSunKVScope(target),
		"repo_hash":    repoHash,
		"file_hash":    fileHash,
		"file":         filepath.Clean(strings.TrimSpace(target.File)),
		"key":          key,
		"operation":    "set",
		"deleted":      false,
		"changed_at":   changedAt,
		"value_sha256": sunPayloadSHA256Hex([]byte(rawValue)),
	}
	payload := []byte(strings.TrimSpace(rawValue) + "\n")
	if deleted {
		metadata["operation"] = "unset"
		metadata["deleted"] = true
		delete(metadata, "value_sha256")
		payload = []byte{}
	}
	if strings.TrimSpace(source) != "" {
		metadata["source"] = strings.TrimSpace(source)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := client.putObject(ctx, kind, key, payload, "text/plain", metadata, nil); err != nil {
		if strict {
			return fmt.Errorf("sun vault key write failed (%s): %w", key, err)
		}
		warnf("sun vault key write skipped (%s): %v", key, err)
	}
	return nil
}

type vaultSunKVMirrorResult struct {
	Pushed    int
	Tombstone int
}

func vaultSunKVMirrorDoc(settings Settings, target vault.Target, doc vault.DotenvFile, source string) (vaultSunKVMirrorResult, error) {
	result := vaultSunKVMirrorResult{}
	client, strict, err := vaultSunKVClient(settings)
	if err != nil {
		return result, fmt.Errorf("sun vault key mirror failed: %w", err)
	}
	if client == nil {
		return result, nil
	}
	entries, err := vault.Entries(doc)
	if err != nil {
		return result, err
	}
	local := map[string]string{}
	for _, entry := range entries {
		local[entry.Key] = vault.RenderDotenvValuePlain(entry.ValueRaw)
	}
	keys := make([]string, 0, len(local))
	for key := range local {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := vaultSunKVPutRawValue(settings, target, key, local[key], source, false); err != nil {
			if strict {
				return result, err
			}
			warnf("%v", err)
			continue
		}
		result.Pushed++
	}

	kind := vaultSunKVKind(target)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	items, err := client.listObjects(ctx, kind, "", sunVaultKVListLimit)
	if err != nil {
		if strict {
			return result, fmt.Errorf("sun vault key mirror list failed: %w", err)
		}
		warnf("sun vault key mirror list skipped: %v", err)
		return result, nil
	}
	for _, item := range items {
		key := strings.TrimSpace(item.Name)
		if key == "" {
			continue
		}
		if _, ok := local[key]; ok {
			continue
		}
		if vaultSunKVMetaBool(item.Metadata, "deleted") {
			continue
		}
		if err := vaultSunKVPutRawValue(settings, target, key, "", source, true); err != nil {
			if strict {
				return result, err
			}
			warnf("%v", err)
			continue
		}
		result.Tombstone++
	}
	return result, nil
}

func vaultSunKVListHistory(settings Settings, target vault.Target, key string, limit int) ([]sunObjectRevision, bool, error) {
	client, strict, err := vaultSunKVClient(settings)
	if err != nil {
		return nil, true, fmt.Errorf("sun vault key history failed: %w", err)
	}
	if client == nil {
		return nil, false, nil
	}
	kind := vaultSunKVKind(target)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	items, err := client.listRevisions(ctx, kind, key, limit)
	if err != nil {
		if strict {
			return nil, true, fmt.Errorf("sun vault key history failed (%s): %w", key, err)
		}
		warnf("sun vault key history skipped (%s): %v", key, err)
		return nil, false, nil
	}
	return items, true, nil
}
