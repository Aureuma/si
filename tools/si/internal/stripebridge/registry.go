package stripebridge

import (
	"fmt"
	"sort"
	"strings"
)

type CRUDOp string

const (
	CRUDList   CRUDOp = "list"
	CRUDGet    CRUDOp = "get"
	CRUDCreate CRUDOp = "create"
	CRUDUpdate CRUDOp = "update"
	CRUDDelete CRUDOp = "delete"
)

type ObjectSpec struct {
	Name         string
	Aliases      []string
	ListPath     string
	ResourcePath string
	Supports     map[CRUDOp]bool
	DeleteHint   string
}

func (s ObjectSpec) SupportsOp(op CRUDOp) bool {
	if s.Supports == nil {
		return false
	}
	return s.Supports[op]
}

var registry = []ObjectSpec{
	{Name: "product", Aliases: []string{"products"}, ListPath: "/v1/products", ResourcePath: "/v1/products/%s", Supports: supports(CRUDList, CRUDGet, CRUDCreate, CRUDUpdate, CRUDDelete)},
	{Name: "price", Aliases: []string{"prices"}, ListPath: "/v1/prices", ResourcePath: "/v1/prices/%s", Supports: supports(CRUDList, CRUDGet, CRUDCreate, CRUDUpdate), DeleteHint: "Stripe prices are archived by updating `active=false`."},
	{Name: "coupon", Aliases: []string{"coupons"}, ListPath: "/v1/coupons", ResourcePath: "/v1/coupons/%s", Supports: supports(CRUDList, CRUDGet, CRUDCreate, CRUDUpdate, CRUDDelete)},
	{Name: "promotion_code", Aliases: []string{"promotion-codes", "promotion_codes"}, ListPath: "/v1/promotion_codes", ResourcePath: "/v1/promotion_codes/%s", Supports: supports(CRUDList, CRUDGet, CRUDCreate, CRUDUpdate)},
	{Name: "tax_rate", Aliases: []string{"tax-rates", "tax_rates"}, ListPath: "/v1/tax_rates", ResourcePath: "/v1/tax_rates/%s", Supports: supports(CRUDList, CRUDGet, CRUDCreate, CRUDUpdate)},
	{Name: "shipping_rate", Aliases: []string{"shipping-rates", "shipping_rates"}, ListPath: "/v1/shipping_rates", ResourcePath: "/v1/shipping_rates/%s", Supports: supports(CRUDList, CRUDGet, CRUDCreate, CRUDUpdate)},
	{Name: "customer", Aliases: []string{"customers"}, ListPath: "/v1/customers", ResourcePath: "/v1/customers/%s", Supports: supports(CRUDList, CRUDGet, CRUDCreate, CRUDUpdate, CRUDDelete)},
	{Name: "payment_intent", Aliases: []string{"payment-intents", "payment_intents"}, ListPath: "/v1/payment_intents", ResourcePath: "/v1/payment_intents/%s", Supports: supports(CRUDList, CRUDGet, CRUDCreate, CRUDUpdate)},
	{Name: "subscription", Aliases: []string{"subscriptions"}, ListPath: "/v1/subscriptions", ResourcePath: "/v1/subscriptions/%s", Supports: supports(CRUDList, CRUDGet, CRUDCreate, CRUDUpdate, CRUDDelete)},
	{Name: "invoice", Aliases: []string{"invoices"}, ListPath: "/v1/invoices", ResourcePath: "/v1/invoices/%s", Supports: supports(CRUDList, CRUDGet, CRUDCreate, CRUDUpdate, CRUDDelete)},
	{Name: "refund", Aliases: []string{"refunds"}, ListPath: "/v1/refunds", ResourcePath: "/v1/refunds/%s", Supports: supports(CRUDList, CRUDGet, CRUDCreate, CRUDUpdate)},
	{Name: "charge", Aliases: []string{"charges"}, ListPath: "/v1/charges", ResourcePath: "/v1/charges/%s", Supports: supports(CRUDList, CRUDGet, CRUDCreate, CRUDUpdate)},
	{Name: "account", Aliases: []string{"accounts"}, ListPath: "/v1/accounts", ResourcePath: "/v1/accounts/%s", Supports: supports(CRUDList, CRUDGet, CRUDCreate, CRUDUpdate)},
	{Name: "organization", Aliases: []string{"organizations"}, ListPath: "/v1/organizations", ResourcePath: "/v1/organizations/%s", Supports: supports(CRUDList, CRUDGet)},
	{Name: "balance_transaction", Aliases: []string{"balance-transactions", "balance_transactions"}, ListPath: "/v1/balance_transactions", ResourcePath: "/v1/balance_transactions/%s", Supports: supports(CRUDList, CRUDGet)},
	{Name: "payout", Aliases: []string{"payouts"}, ListPath: "/v1/payouts", ResourcePath: "/v1/payouts/%s", Supports: supports(CRUDList, CRUDGet, CRUDCreate, CRUDUpdate)},
	{Name: "payment_method", Aliases: []string{"payment-methods", "payment_methods"}, ListPath: "/v1/payment_methods", ResourcePath: "/v1/payment_methods/%s", Supports: supports(CRUDList, CRUDGet, CRUDCreate, CRUDUpdate)},
}

func supports(ops ...CRUDOp) map[CRUDOp]bool {
	out := make(map[CRUDOp]bool, len(ops))
	for _, op := range ops {
		out[op] = true
	}
	return out
}

func ResolveObject(name string) (ObjectSpec, error) {
	norm := normalizeObjectName(name)
	if norm == "" {
		return ObjectSpec{}, fmt.Errorf("object is required")
	}
	for _, item := range registry {
		if normalizeObjectName(item.Name) == norm {
			return item, nil
		}
		for _, alias := range item.Aliases {
			if normalizeObjectName(alias) == norm {
				return item, nil
			}
		}
	}
	return ObjectSpec{}, fmt.Errorf("unknown object %q (supported: %s)", name, strings.Join(SupportedObjects(), ", "))
}

func SupportedObjects() []string {
	out := make([]string, 0, len(registry))
	for _, item := range registry {
		out = append(out, item.Name)
	}
	sort.Strings(out)
	return out
}

func normalizeObjectName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "-", "_")
	return value
}
