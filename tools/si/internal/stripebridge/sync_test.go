package stripebridge

import "testing"

func TestParseSyncFamilies(t *testing.T) {
	families, err := ParseSyncFamilies([]string{"products,prices", "coupons"})
	if err != nil {
		t.Fatalf("parse families: %v", err)
	}
	if len(families) != 3 {
		t.Fatalf("unexpected family count: %d", len(families))
	}
	if _, err := ParseSyncFamilies([]string{"unknown"}); err == nil {
		t.Fatalf("expected unknown family error")
	}
}

func TestPlanFamilyDetectsCreateAndUpdate(t *testing.T) {
	live := []map[string]any{
		{
			"id":     "prod_live_1",
			"name":   "Pro",
			"active": true,
			"metadata": map[string]any{
				"tier": "gold",
			},
		},
	}
	sandbox := []map[string]any{
		{
			"id":     "prod_sbx_1",
			"name":   "Old",
			"active": true,
			"metadata": map[string]any{
				"si_live_id": "prod_live_1",
			},
		},
	}
	actions, mappings := planFamily(SyncFamilyProducts, live, sandbox)
	if len(actions) != 1 {
		t.Fatalf("expected one update action, got %d", len(actions))
	}
	if actions[0].Action != SyncActionUpdate {
		t.Fatalf("expected update action, got %s", actions[0].Action)
	}
	if mappings["prod_live_1"] != "prod_sbx_1" {
		t.Fatalf("expected mapping to sandbox id")
	}
}
