package stripebridge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func listSyncFamily(ctx context.Context, client *Client, family SyncFamily) ([]map[string]any, error) {
	path, params, err := syncFamilyListPath(family)
	if err != nil {
		return nil, err
	}
	return client.ListAll(ctx, path, params, -1)
}

func syncFamilyListPath(family SyncFamily) (string, map[string]string, error) {
	switch family {
	case SyncFamilyProducts:
		return "/v1/products", map[string]string{"limit": "100"}, nil
	case SyncFamilyPrices:
		return "/v1/prices", map[string]string{"limit": "100"}, nil
	case SyncFamilyCoupons:
		return "/v1/coupons", map[string]string{"limit": "100"}, nil
	case SyncFamilyPromotionCodes:
		return "/v1/promotion_codes", map[string]string{"limit": "100"}, nil
	case SyncFamilyTaxRates:
		return "/v1/tax_rates", map[string]string{"limit": "100"}, nil
	case SyncFamilyShippingRates:
		return "/v1/shipping_rates", map[string]string{"limit": "100"}, nil
	default:
		return "", nil, fmt.Errorf("unsupported sync family %s", family)
	}
}

func planFamily(family SyncFamily, liveItems []map[string]any, sandboxItems []map[string]any) ([]SyncAction, map[string]string) {
	actions := make([]SyncAction, 0)
	mappings := map[string]string{}
	sandboxByLive := map[string]map[string]any{}
	sandboxByID := map[string]map[string]any{}

	for _, item := range sandboxItems {
		id, _ := stringField(item, "id")
		id = strings.TrimSpace(id)
		if id != "" {
			sandboxByID[id] = item
		}
		liveID := strings.TrimSpace(liveIDFromMetadata(item))
		if liveID != "" {
			sandboxByLive[liveID] = item
		}
	}

	liveIDs := map[string]struct{}{}
	for _, liveItem := range liveItems {
		liveID, _ := stringField(liveItem, "id")
		liveID = strings.TrimSpace(liveID)
		if liveID == "" {
			continue
		}
		liveIDs[liveID] = struct{}{}
		payload := payloadForSyncFamily(family, liveItem, nil)
		match := sandboxByLive[liveID]
		if match == nil {
			actions = append(actions, SyncAction{
				Family:  family,
				Action:  SyncActionCreate,
				LiveID:  liveID,
				Reason:  "missing in sandbox",
				Payload: payload,
			})
			continue
		}
		sandboxID, _ := stringField(match, "id")
		sandboxID = strings.TrimSpace(sandboxID)
		if sandboxID != "" {
			mappings[liveID] = sandboxID
		}
		expectedSandbox := payloadForSyncFamily(family, liveItem, mappings)
		currentSandbox := payloadForSyncFamily(family, match, nil)
		if !equivalentPayload(expectedSandbox, currentSandbox) {
			actions = append(actions, SyncAction{
				Family:    family,
				Action:    SyncActionUpdate,
				LiveID:    liveID,
				SandboxID: sandboxID,
				Reason:    "drift detected",
				Payload:   expectedSandbox,
			})
		}
	}

	for _, sandboxItem := range sandboxItems {
		liveID := strings.TrimSpace(liveIDFromMetadata(sandboxItem))
		if liveID == "" {
			continue
		}
		if _, ok := liveIDs[liveID]; ok {
			continue
		}
		sandboxID, _ := stringField(sandboxItem, "id")
		actions = append(actions, SyncAction{
			Family:    family,
			Action:    SyncActionArchive,
			LiveID:    liveID,
			SandboxID: strings.TrimSpace(sandboxID),
			Reason:    "orphaned in sandbox",
			Payload:   map[string]any{"active": false},
		})
	}

	sort.Slice(actions, func(i, j int) bool {
		if actions[i].Action != actions[j].Action {
			return actions[i].Action < actions[j].Action
		}
		if actions[i].Family != actions[j].Family {
			return actions[i].Family < actions[j].Family
		}
		return actions[i].LiveID < actions[j].LiveID
	})
	return actions, mappings
}

func liveIDFromMetadata(item map[string]any) string {
	meta, ok := item["metadata"].(map[string]any)
	if !ok || meta == nil {
		return ""
	}
	val, _ := meta["si_live_id"].(string)
	return strings.TrimSpace(val)
}

func equivalentPayload(a map[string]any, b map[string]any) bool {
	aj, errA := json.Marshal(a)
	bj, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(aj) == string(bj)
}

func payloadForSyncFamily(family SyncFamily, item map[string]any, mappings map[string]string) map[string]any {
	out := map[string]any{}
	if item == nil {
		return out
	}
	switch family {
	case SyncFamilyProducts:
		copyStringFields(out, item, "name", "description", "unit_label", "tax_code")
		copyBoolField(out, item, "active")
		copyBoolField(out, item, "shippable")
		copyMetadataWithLiveID(out, item)
	case SyncFamilyPrices:
		copyStringFields(out, item, "currency", "nickname", "lookup_key")
		copyBoolField(out, item, "active")
		copyNumberField(out, item, "unit_amount")
		copyStringFieldMapped(out, item, "product", mappings)
		copyNested(out, item, "recurring")
		copyMetadataWithLiveID(out, item)
	case SyncFamilyCoupons:
		copyStringFields(out, item, "name", "duration")
		copyNumberField(out, item, "amount_off")
		copyNumberField(out, item, "percent_off")
		copyStringFields(out, item, "currency")
		copyNested(out, item, "applies_to")
		copyMetadataWithLiveID(out, item)
	case SyncFamilyPromotionCodes:
		copyStringFields(out, item, "code")
		copyBoolField(out, item, "active")
		copyNested(out, item, "restrictions")
		copyStringFieldMapped(out, item, "coupon", mappings)
		copyMetadataWithLiveID(out, item)
	case SyncFamilyTaxRates:
		copyStringFields(out, item, "display_name", "description", "jurisdiction", "country", "state", "tax_type")
		copyBoolField(out, item, "inclusive")
		copyBoolField(out, item, "active")
		copyNumberField(out, item, "percentage")
		copyMetadataWithLiveID(out, item)
	case SyncFamilyShippingRates:
		copyStringFields(out, item, "display_name", "tax_behavior", "type")
		copyBoolField(out, item, "active")
		copyNested(out, item, "fixed_amount")
		copyNested(out, item, "delivery_estimate")
		copyMetadataWithLiveID(out, item)
	}
	return out
}

func copyStringFields(dst map[string]any, src map[string]any, keys ...string) {
	for _, key := range keys {
		if val, ok := src[key].(string); ok && strings.TrimSpace(val) != "" {
			dst[key] = val
		}
	}
}

func copyBoolField(dst map[string]any, src map[string]any, key string) {
	if val, ok := src[key].(bool); ok {
		dst[key] = val
	}
}

func copyNumberField(dst map[string]any, src map[string]any, key string) {
	switch val := src[key].(type) {
	case float64:
		dst[key] = int64(val)
	case int64, int32, int, uint, uint64:
		dst[key] = val
	}
}

func copyNested(dst map[string]any, src map[string]any, key string) {
	if val, ok := src[key].(map[string]any); ok && len(val) > 0 {
		dst[key] = val
	}
}

func copyStringFieldMapped(dst map[string]any, src map[string]any, key string, mappings map[string]string) {
	val, ok := src[key].(string)
	if !ok || strings.TrimSpace(val) == "" {
		return
	}
	val = strings.TrimSpace(val)
	if mapped, ok := mappings[val]; ok && strings.TrimSpace(mapped) != "" {
		dst[key] = mapped
		return
	}
	dst[key] = val
}

func copyMetadataWithLiveID(dst map[string]any, src map[string]any) {
	meta := map[string]any{}
	if current, ok := src["metadata"].(map[string]any); ok {
		for key, value := range current {
			meta[key] = value
		}
	}
	if liveID, ok := stringField(src, "id"); ok && strings.TrimSpace(liveID) != "" {
		meta["si_live_id"] = strings.TrimSpace(liveID)
	}
	if len(meta) > 0 {
		dst["metadata"] = meta
	}
}
