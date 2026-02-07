package main

import (
	"context"
	"fmt"
	"testing"

	"si/tools/si/internal/stripebridge"
)

type mockStripeClient struct {
	doFn      func(ctx context.Context, req stripebridge.Request) (stripebridge.Response, error)
	listAllFn func(ctx context.Context, path string, params map[string]string, limit int) ([]map[string]any, error)
}

func (m *mockStripeClient) Do(ctx context.Context, req stripebridge.Request) (stripebridge.Response, error) {
	if m.doFn == nil {
		return stripebridge.Response{}, fmt.Errorf("not implemented")
	}
	return m.doFn(ctx, req)
}

func (m *mockStripeClient) ListAll(ctx context.Context, path string, params map[string]string, limit int) ([]map[string]any, error) {
	if m.listAllFn == nil {
		return nil, fmt.Errorf("not implemented")
	}
	return m.listAllFn(ctx, path, params, limit)
}

func (m *mockStripeClient) ExecuteCRUD(ctx context.Context, spec stripebridge.ObjectSpec, op stripebridge.CRUDOp, id string, params map[string]string, idempotencyKey string) (stripebridge.Response, error) {
	return stripebridge.Response{}, fmt.Errorf("not implemented")
}

func TestRunStripeReportRevenueSummary(t *testing.T) {
	client := &mockStripeClient{
		listAllFn: func(ctx context.Context, path string, params map[string]string, limit int) ([]map[string]any, error) {
			if path != "/v1/balance_transactions" {
				t.Fatalf("unexpected path: %s", path)
			}
			return []map[string]any{
				{"amount": float64(1000), "fee": float64(100), "currency": "usd"},
				{"amount": float64(500), "fee": float64(50), "currency": "usd"},
			}, nil
		},
	}
	report, err := runStripeReport(context.Background(), client, "revenue-summary", nil, nil, 100)
	if err != nil {
		t.Fatalf("report failed: %v", err)
	}
	if report["gross_amount"] != int64(1500) {
		t.Fatalf("unexpected gross amount: %v", report["gross_amount"])
	}
	if report["net_amount"] != int64(1350) {
		t.Fatalf("unexpected net amount: %v", report["net_amount"])
	}
}

func TestRunStripeReportPaymentIntentStatus(t *testing.T) {
	client := &mockStripeClient{
		listAllFn: func(ctx context.Context, path string, params map[string]string, limit int) ([]map[string]any, error) {
			return []map[string]any{
				{"status": "succeeded"},
				{"status": "requires_payment_method"},
				{"status": "succeeded"},
			}, nil
		},
	}
	report, err := runStripeReport(context.Background(), client, "payment-intent-status", nil, nil, 100)
	if err != nil {
		t.Fatalf("report failed: %v", err)
	}
	statusCounts, ok := report["status_counts"].(map[string]any)
	if !ok {
		t.Fatalf("status_counts missing")
	}
	if statusCounts["succeeded"] != 2 {
		t.Fatalf("unexpected succeeded count: %v", statusCounts["succeeded"])
	}
}
