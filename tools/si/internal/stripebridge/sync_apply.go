package stripebridge

import (
	"context"
	"fmt"
	"strings"
)

type ApplyOptions struct {
	DryRun         bool
	IdempotencyKey string
}

type ApplyResult struct {
	Applied   int               `json:"applied"`
	Skipped   int               `json:"skipped"`
	Failures  int               `json:"failures"`
	Errors    []string          `json:"errors,omitempty"`
	Mappings  map[string]string `json:"mappings,omitempty"`
	Completed []SyncAction      `json:"completed,omitempty"`
}

func ApplyLiveToSandboxPlan(ctx context.Context, sandbox *Client, plan SyncPlan, opts ApplyOptions) (ApplyResult, error) {
	if sandbox == nil {
		return ApplyResult{}, fmt.Errorf("sandbox client is required")
	}
	result := ApplyResult{
		Mappings: map[string]string{},
	}
	for key, value := range plan.Mappings {
		result.Mappings[key] = value
	}

	for _, action := range plan.Actions {
		if action.Action == SyncActionArchive && strings.TrimSpace(action.SandboxID) == "" {
			result.Skipped++
			continue
		}
		if opts.DryRun {
			result.Skipped++
			result.Completed = append(result.Completed, action)
			continue
		}
		if err := applyAction(ctx, sandbox, action, result.Mappings, opts.IdempotencyKey); err != nil {
			result.Failures++
			result.Errors = append(result.Errors, fmt.Sprintf("%s %s: %v", action.Family, action.Action, err))
			continue
		}
		result.Applied++
		result.Completed = append(result.Completed, action)
	}
	if result.Failures > 0 {
		return result, fmt.Errorf("sync apply completed with %d failure(s)", result.Failures)
	}
	return result, nil
}

func applyAction(ctx context.Context, sandbox *Client, action SyncAction, mappings map[string]string, idempotencyKey string) error {
	objectName, err := familyObjectName(action.Family)
	if err != nil {
		return err
	}
	spec, err := ResolveObject(objectName)
	if err != nil {
		return err
	}
	params := flattenPayload(action.Payload)
	switch action.Action {
	case SyncActionCreate:
		resp, err := sandbox.ExecuteCRUD(ctx, spec, CRUDCreate, "", params, idempotencyKey)
		if err != nil {
			return err
		}
		if resp.Data != nil {
			if createdID, ok := stringField(resp.Data, "id"); ok && strings.TrimSpace(action.LiveID) != "" {
				mappings[action.LiveID] = strings.TrimSpace(createdID)
			}
		}
		return nil
	case SyncActionUpdate:
		target := strings.TrimSpace(action.SandboxID)
		if target == "" && strings.TrimSpace(action.LiveID) != "" {
			target = strings.TrimSpace(mappings[action.LiveID])
		}
		if target == "" {
			return fmt.Errorf("missing sandbox id for update")
		}
		_, err := sandbox.ExecuteCRUD(ctx, spec, CRUDUpdate, target, params, idempotencyKey)
		return err
	case SyncActionArchive:
		target := strings.TrimSpace(action.SandboxID)
		if target == "" {
			return fmt.Errorf("missing sandbox id for archive")
		}
		archive := map[string]string{"active": "false"}
		_, err := sandbox.ExecuteCRUD(ctx, spec, CRUDUpdate, target, archive, idempotencyKey)
		return err
	default:
		return fmt.Errorf("unsupported sync action %s", action.Action)
	}
}

func flattenPayload(payload map[string]any) map[string]string {
	out := map[string]string{}
	for key, value := range payload {
		flattenValue(out, key, value)
	}
	return out
}

func flattenValue(out map[string]string, key string, value any) {
	switch typed := value.(type) {
	case nil:
		return
	case string:
		out[key] = typed
	case bool:
		if typed {
			out[key] = "true"
		} else {
			out[key] = "false"
		}
	case int:
		out[key] = fmt.Sprintf("%d", typed)
	case int64:
		out[key] = fmt.Sprintf("%d", typed)
	case float64:
		out[key] = fmt.Sprintf("%g", typed)
	case map[string]any:
		for childKey, childValue := range typed {
			flattenValue(out, key+"["+childKey+"]", childValue)
		}
	default:
		out[key] = fmt.Sprintf("%v", typed)
	}
}

func familyObjectName(family SyncFamily) (string, error) {
	switch family {
	case SyncFamilyProducts:
		return "product", nil
	case SyncFamilyPrices:
		return "price", nil
	case SyncFamilyCoupons:
		return "coupon", nil
	case SyncFamilyPromotionCodes:
		return "promotion_code", nil
	case SyncFamilyTaxRates:
		return "tax_rate", nil
	case SyncFamilyShippingRates:
		return "shipping_rate", nil
	default:
		return "", fmt.Errorf("unsupported sync family %s", family)
	}
}
