package stripebridge

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type SyncFamily string

const (
	SyncFamilyProducts       SyncFamily = "products"
	SyncFamilyPrices         SyncFamily = "prices"
	SyncFamilyCoupons        SyncFamily = "coupons"
	SyncFamilyPromotionCodes SyncFamily = "promotion_codes"
	SyncFamilyTaxRates       SyncFamily = "tax_rates"
	SyncFamilyShippingRates  SyncFamily = "shipping_rates"
)

type SyncActionType string

const (
	SyncActionCreate  SyncActionType = "create"
	SyncActionUpdate  SyncActionType = "update"
	SyncActionArchive SyncActionType = "archive"
)

type SyncAction struct {
	Family    SyncFamily     `json:"family"`
	Action    SyncActionType `json:"action"`
	LiveID    string         `json:"live_id,omitempty"`
	SandboxID string         `json:"sandbox_id,omitempty"`
	Reason    string         `json:"reason,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

type SyncPlan struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Families    []SyncFamily      `json:"families"`
	Actions     []SyncAction      `json:"actions"`
	Summary     map[string]int    `json:"summary"`
	Mappings    map[string]string `json:"mappings,omitempty"`
}

func ParseSyncFamilies(raw []string) ([]SyncFamily, error) {
	if len(raw) == 0 {
		return []SyncFamily{
			SyncFamilyProducts,
			SyncFamilyPrices,
			SyncFamilyCoupons,
			SyncFamilyPromotionCodes,
			SyncFamilyTaxRates,
			SyncFamilyShippingRates,
		}, nil
	}
	out := make([]SyncFamily, 0, len(raw))
	seen := map[SyncFamily]struct{}{}
	for _, token := range raw {
		parts := strings.Split(token, ",")
		for _, part := range parts {
			part = strings.TrimSpace(strings.ToLower(part))
			if part == "" {
				continue
			}
			part = strings.ReplaceAll(part, "-", "_")
			family := SyncFamily(part)
			if !isSupportedFamily(family) {
				return nil, fmt.Errorf("unsupported sync family %q", part)
			}
			if _, ok := seen[family]; ok {
				continue
			}
			seen[family] = struct{}{}
			out = append(out, family)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func isSupportedFamily(family SyncFamily) bool {
	switch family {
	case SyncFamilyProducts, SyncFamilyPrices, SyncFamilyCoupons, SyncFamilyPromotionCodes, SyncFamilyTaxRates, SyncFamilyShippingRates:
		return true
	default:
		return false
	}
}

func BuildLiveToSandboxPlan(ctx context.Context, live *Client, sandbox *Client, families []SyncFamily) (SyncPlan, error) {
	if live == nil || sandbox == nil {
		return SyncPlan{}, fmt.Errorf("live and sandbox clients are required")
	}
	if len(families) == 0 {
		return SyncPlan{}, fmt.Errorf("at least one sync family is required")
	}
	plan := SyncPlan{
		GeneratedAt: time.Now().UTC(),
		Families:    append([]SyncFamily(nil), families...),
		Summary:     map[string]int{},
		Mappings:    map[string]string{},
	}
	for _, family := range families {
		liveItems, err := listSyncFamily(ctx, live, family)
		if err != nil {
			return SyncPlan{}, fmt.Errorf("list live %s: %w", family, err)
		}
		sandboxItems, err := listSyncFamily(ctx, sandbox, family)
		if err != nil {
			return SyncPlan{}, fmt.Errorf("list sandbox %s: %w", family, err)
		}
		familyActions, mappings := planFamily(family, liveItems, sandboxItems)
		plan.Actions = append(plan.Actions, familyActions...)
		for k, v := range mappings {
			plan.Mappings[k] = v
		}
	}
	for _, action := range plan.Actions {
		plan.Summary[string(action.Action)]++
	}
	return plan, nil
}
