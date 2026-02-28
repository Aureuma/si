package orbitals

import (
	"testing"
	"time"
)

func TestBuildGatewayShardsByNamespaceAndSlot(t *testing.T) {
	catalog := Catalog{
		SchemaVersion: SchemaVersion,
		Entries: []CatalogEntry{
			{
				Manifest: Manifest{
					SchemaVersion: SchemaVersion,
					ID:            "acme/alpha",
					Namespace:     "acme",
					Install:       InstallSpec{Type: InstallTypeNone},
					Integration: IntegrationSpec{
						Capabilities: []string{"issues.create", "issues.read"},
					},
				},
			},
			{
				Manifest: Manifest{
					SchemaVersion: SchemaVersion,
					ID:            "acme/beta",
					Namespace:     "acme",
					Install:       InstallSpec{Type: InstallTypeNone},
					Integration: IntegrationSpec{
						Capabilities: []string{"chat.send"},
					},
				},
			},
			{
				Manifest: Manifest{
					SchemaVersion: SchemaVersion,
					ID:            "saas/gamma",
					Namespace:     "saas",
					Install:       InstallSpec{Type: InstallTypeNone},
					Integration: IntegrationSpec{
						Capabilities: []string{"crm.read"},
					},
				},
			},
		},
	}

	index, shards, err := BuildGateway(catalog, GatewayBuildOptions{
		Registry:          "global",
		SlotsPerNamespace: 8,
		GeneratedAt:       time.Date(2026, 2, 22, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildGateway: %v", err)
	}
	if index.Registry != "global" {
		t.Fatalf("registry=%q", index.Registry)
	}
	if index.TotalEntries != 3 {
		t.Fatalf("total_entries=%d", index.TotalEntries)
	}
	if index.SlotsPerNamespace != 8 {
		t.Fatalf("slots=%d", index.SlotsPerNamespace)
	}
	if len(index.Namespaces) != 2 {
		t.Fatalf("namespaces=%#v", index.Namespaces)
	}
	if len(shards) == 0 {
		t.Fatalf("expected shards")
	}
	for _, summary := range index.Shards {
		if summary.Key == "" || summary.Checksum == "" {
			t.Fatalf("invalid shard summary: %#v", summary)
		}
		shard, ok := shards[summary.Key]
		if !ok {
			t.Fatalf("missing shard %q", summary.Key)
		}
		if len(shard.Entries) != summary.Count {
			t.Fatalf("shard count mismatch key=%s summary=%d entries=%d", summary.Key, summary.Count, len(shard.Entries))
		}
	}
}

func TestMaterializeGatewayCatalogFiltersEntries(t *testing.T) {
	catalog := Catalog{
		SchemaVersion: SchemaVersion,
		Entries: []CatalogEntry{
			{
				Manifest: Manifest{
					SchemaVersion: SchemaVersion,
					ID:            "acme/alpha",
					Namespace:     "acme",
					Install:       InstallSpec{Type: InstallTypeNone},
					Integration: IntegrationSpec{
						Capabilities: []string{"chat.send"},
					},
				},
			},
			{
				Manifest: Manifest{
					SchemaVersion: SchemaVersion,
					ID:            "acme/beta",
					Namespace:     "acme",
					Install:       InstallSpec{Type: InstallTypeNone},
					Integration: IntegrationSpec{
						Capabilities: []string{"issue.create"},
					},
				},
			},
			{
				Manifest: Manifest{
					SchemaVersion: SchemaVersion,
					ID:            "saas/gamma",
					Namespace:     "saas",
					Install:       InstallSpec{Type: InstallTypeNone},
					Integration: IntegrationSpec{
						Capabilities: []string{"chat.send"},
					},
				},
			},
		},
	}
	index, shards, err := BuildGateway(catalog, GatewayBuildOptions{Registry: "global", SlotsPerNamespace: 4})
	if err != nil {
		t.Fatalf("BuildGateway: %v", err)
	}

	filtered := MaterializeGatewayCatalog(index, shards, GatewaySelectFilter{
		Namespace:  "acme",
		Capability: "chat.send",
	})
	if len(filtered.Entries) != 1 {
		t.Fatalf("filtered entries=%#v", filtered.Entries)
	}
	if filtered.Entries[0].Manifest.ID != "acme/alpha" {
		t.Fatalf("unexpected filtered entry=%s", filtered.Entries[0].Manifest.ID)
	}

	prefixed := MaterializeGatewayCatalog(index, shards, GatewaySelectFilter{
		Prefix: "saas/",
	})
	if len(prefixed.Entries) != 1 || prefixed.Entries[0].Manifest.ID != "saas/gamma" {
		t.Fatalf("prefix filtered entries=%#v", prefixed.Entries)
	}

	limited := MaterializeGatewayCatalog(index, shards, GatewaySelectFilter{Limit: 2})
	if len(limited.Entries) != 2 {
		t.Fatalf("expected limit to be enforced, got %d", len(limited.Entries))
	}
}

func TestGatewayShardObjectNameRoundTrip(t *testing.T) {
	name, err := GatewayShardObjectName("global", "acme--03")
	if err != nil {
		t.Fatalf("GatewayShardObjectName: %v", err)
	}
	if name != "global:acme:03" {
		t.Fatalf("object name=%q", name)
	}
	key, err := ParseGatewayShardObjectName("global", name)
	if err != nil {
		t.Fatalf("ParseGatewayShardObjectName: %v", err)
	}
	if key != "acme--03" {
		t.Fatalf("round trip key=%q", key)
	}
}
