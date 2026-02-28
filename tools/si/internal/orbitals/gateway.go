package orbitals

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"
)

const (
	GatewaySchemaVersion            = 1
	GatewayDefaultSlotsPerNamespace = 16
	GatewayMaxSlotsPerNamespace     = 256
)

type GatewayBuildOptions struct {
	Registry          string
	SlotsPerNamespace int
	GeneratedAt       time.Time
}

type GatewayIndex struct {
	SchemaVersion     int                     `json:"schema_version,omitempty"`
	Registry          string                  `json:"registry"`
	GeneratedAt       string                  `json:"generated_at"`
	SlotsPerNamespace int                     `json:"slots_per_namespace"`
	TotalEntries      int                     `json:"total_entries"`
	Shards            []GatewayShardSummary   `json:"shards"`
	Namespaces        []GatewayNamespaceIndex `json:"namespaces,omitempty"`
}

type GatewayShardSummary struct {
	Key          string   `json:"key"`
	Namespace    string   `json:"namespace"`
	Slot         int      `json:"slot"`
	Count        int      `json:"count"`
	Capabilities []string `json:"capabilities,omitempty"`
	Checksum     string   `json:"checksum"`
}

type GatewayNamespaceIndex struct {
	Namespace string   `json:"namespace"`
	Count     int      `json:"count"`
	Shards    []string `json:"shards"`
}

type GatewayShard struct {
	SchemaVersion int            `json:"schema_version,omitempty"`
	Registry      string         `json:"registry"`
	Key           string         `json:"key"`
	Namespace     string         `json:"namespace"`
	Slot          int            `json:"slot"`
	Entries       []CatalogEntry `json:"entries"`
}

type GatewaySelectFilter struct {
	Namespace  string
	Capability string
	Prefix     string
	Limit      int
}

func NormalizeGatewayRegistryName(raw string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return "", fmt.Errorf("gateway registry name required")
	}
	if !orbitIDSegmentPattern.MatchString(normalized) {
		return "", fmt.Errorf("invalid gateway registry name %q", raw)
	}
	return normalized, nil
}

func NormalizeGatewaySlotsPerNamespace(slots int) (int, error) {
	if slots <= 0 {
		return GatewayDefaultSlotsPerNamespace, nil
	}
	if slots > GatewayMaxSlotsPerNamespace {
		return 0, fmt.Errorf("slots_per_namespace cannot exceed %d", GatewayMaxSlotsPerNamespace)
	}
	return slots, nil
}

func GatewayShardKey(orbitID string, slotsPerNamespace int) (key string, namespace string, slot int, err error) {
	if err := ValidateOrbitID(orbitID); err != nil {
		return "", "", 0, err
	}
	namespace = NamespaceFromID(orbitID)
	if namespace == "" {
		return "", "", 0, fmt.Errorf("orbit id %q has no namespace", orbitID)
	}
	slotsPerNamespace, err = NormalizeGatewaySlotsPerNamespace(slotsPerNamespace)
	if err != nil {
		return "", "", 0, err
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(orbitID))
	slot = int(hasher.Sum32() % uint32(slotsPerNamespace))
	key = fmt.Sprintf("%s--%02d", namespace, slot)
	return key, namespace, slot, nil
}

func BuildGateway(catalog Catalog, options GatewayBuildOptions) (GatewayIndex, map[string]GatewayShard, error) {
	registry, err := NormalizeGatewayRegistryName(options.Registry)
	if err != nil {
		return GatewayIndex{}, nil, err
	}
	slotsPerNamespace, err := NormalizeGatewaySlotsPerNamespace(options.SlotsPerNamespace)
	if err != nil {
		return GatewayIndex{}, nil, err
	}
	generatedAt := options.GeneratedAt.UTC()
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	type capabilitySet map[string]bool
	shards := map[string]GatewayShard{}
	shardCapabilities := map[string]capabilitySet{}
	namespaceShards := map[string]map[string]bool{}
	namespaceCounts := map[string]int{}
	totalEntries := 0

	for _, entry := range catalog.Entries {
		normalizeManifest(&entry.Manifest)
		if err := ValidateManifest(entry.Manifest); err != nil {
			return GatewayIndex{}, nil, fmt.Errorf("invalid catalog entry %q: %w", entry.Manifest.ID, err)
		}
		key, namespace, slot, err := GatewayShardKey(entry.Manifest.ID, slotsPerNamespace)
		if err != nil {
			return GatewayIndex{}, nil, err
		}
		shard := shards[key]
		if shard.Key == "" {
			shard = GatewayShard{
				SchemaVersion: GatewaySchemaVersion,
				Registry:      registry,
				Key:           key,
				Namespace:     namespace,
				Slot:          slot,
				Entries:       []CatalogEntry{},
			}
		}
		shard.Entries = append(shard.Entries, entry)
		shards[key] = shard

		if _, ok := shardCapabilities[key]; !ok {
			shardCapabilities[key] = capabilitySet{}
		}
		for _, capability := range entry.Manifest.Integration.Capabilities {
			if trimmed := strings.TrimSpace(capability); trimmed != "" {
				shardCapabilities[key][trimmed] = true
			}
		}
		if _, ok := namespaceShards[namespace]; !ok {
			namespaceShards[namespace] = map[string]bool{}
		}
		namespaceShards[namespace][key] = true
		namespaceCounts[namespace]++
		totalEntries++
	}

	shardKeys := make([]string, 0, len(shards))
	for key := range shards {
		shardKeys = append(shardKeys, key)
	}
	sort.Strings(shardKeys)

	shardSummaries := make([]GatewayShardSummary, 0, len(shardKeys))
	for _, key := range shardKeys {
		shard := shards[key]
		sort.Slice(shard.Entries, func(i, j int) bool {
			return shard.Entries[i].Manifest.ID < shard.Entries[j].Manifest.ID
		})
		shards[key] = shard
		checksum, err := gatewayShardChecksum(shard)
		if err != nil {
			return GatewayIndex{}, nil, err
		}
		shardSummaries = append(shardSummaries, GatewayShardSummary{
			Key:          key,
			Namespace:    shard.Namespace,
			Slot:         shard.Slot,
			Count:        len(shard.Entries),
			Capabilities: capabilitiesFromSet(shardCapabilities[key]),
			Checksum:     checksum,
		})
	}

	namespaceNames := make([]string, 0, len(namespaceShards))
	for namespace := range namespaceShards {
		namespaceNames = append(namespaceNames, namespace)
	}
	sort.Strings(namespaceNames)
	namespaces := make([]GatewayNamespaceIndex, 0, len(namespaceNames))
	for _, namespace := range namespaceNames {
		keys := make([]string, 0, len(namespaceShards[namespace]))
		for key := range namespaceShards[namespace] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		namespaces = append(namespaces, GatewayNamespaceIndex{
			Namespace: namespace,
			Count:     namespaceCounts[namespace],
			Shards:    keys,
		})
	}

	index := GatewayIndex{
		SchemaVersion:     GatewaySchemaVersion,
		Registry:          registry,
		GeneratedAt:       generatedAt.Format(time.RFC3339),
		SlotsPerNamespace: slotsPerNamespace,
		TotalEntries:      totalEntries,
		Shards:            shardSummaries,
		Namespaces:        namespaces,
	}
	return index, shards, nil
}

func SelectGatewayShards(index GatewayIndex, filter GatewaySelectFilter) []string {
	namespace := strings.TrimSpace(filter.Namespace)
	capability := strings.TrimSpace(filter.Capability)
	keys := make([]string, 0)
	for _, shard := range index.Shards {
		if namespace != "" && shard.Namespace != namespace {
			continue
		}
		if capability != "" && !containsString(shard.Capabilities, capability) {
			continue
		}
		keys = append(keys, shard.Key)
	}
	sort.Strings(keys)
	return keys
}

func MaterializeGatewayCatalog(index GatewayIndex, shards map[string]GatewayShard, filter GatewaySelectFilter) Catalog {
	prefix := strings.TrimSpace(filter.Prefix)
	capability := strings.TrimSpace(filter.Capability)
	limit := filter.Limit
	if limit < 0 {
		limit = 0
	}

	keys := SelectGatewayShards(index, filter)
	out := Catalog{
		SchemaVersion: SchemaVersion,
		Entries:       []CatalogEntry{},
	}
	seen := map[string]bool{}
	for _, key := range keys {
		shard, ok := shards[key]
		if !ok {
			continue
		}
		for _, entry := range shard.Entries {
			id := strings.TrimSpace(entry.Manifest.ID)
			if id == "" || seen[id] {
				continue
			}
			if prefix != "" && !strings.HasPrefix(id, prefix) {
				continue
			}
			if capability != "" && !containsString(entry.Manifest.Integration.Capabilities, capability) {
				continue
			}
			seen[id] = true
			out.Entries = append(out.Entries, entry)
			if limit > 0 && len(out.Entries) >= limit {
				sort.Slice(out.Entries, func(i, j int) bool {
					return out.Entries[i].Manifest.ID < out.Entries[j].Manifest.ID
				})
				return out
			}
		}
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		return out.Entries[i].Manifest.ID < out.Entries[j].Manifest.ID
	})
	return out
}

func GatewayShardObjectName(registry string, shardKey string) (string, error) {
	registry, err := NormalizeGatewayRegistryName(registry)
	if err != nil {
		return "", err
	}
	shardKey = strings.TrimSpace(shardKey)
	if shardKey == "" {
		return "", fmt.Errorf("shard key required")
	}
	segments := strings.Split(shardKey, "--")
	if len(segments) != 2 {
		return "", fmt.Errorf("invalid shard key %q", shardKey)
	}
	namespace := strings.TrimSpace(segments[0])
	if !orbitIDSegmentPattern.MatchString(namespace) {
		return "", fmt.Errorf("invalid shard namespace %q", namespace)
	}
	slotValue := strings.TrimSpace(segments[1])
	if slotValue == "" {
		return "", fmt.Errorf("invalid shard slot in %q", shardKey)
	}
	return registry + ":" + namespace + ":" + slotValue, nil
}

func ParseGatewayShardObjectName(registry string, name string) (string, error) {
	registry, err := NormalizeGatewayRegistryName(registry)
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	prefix := registry + ":"
	if !strings.HasPrefix(name, prefix) {
		return "", fmt.Errorf("object %q does not belong to registry %q", name, registry)
	}
	parts := strings.Split(strings.TrimPrefix(name, prefix), ":")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid shard object name %q", name)
	}
	return parts[0] + "--" + parts[1], nil
}

func gatewayShardChecksum(shard GatewayShard) (string, error) {
	body, err := json.Marshal(shard.Entries)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func capabilitiesFromSet(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	values := make([]string, 0, len(set))
	for capability := range set {
		values = append(values, capability)
	}
	sort.Strings(values)
	return normalizeStringList(values)
}
