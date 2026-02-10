package stripebridge

import (
	"net/http"
	"os"
	"testing"

	stripe "github.com/stripe/stripe-go/v83"
)

func TestContractNormalizeProductFixture(t *testing.T) {
	body, err := os.ReadFile("testdata/contracts/product_get.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	resp := &stripe.APIResponse{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		RequestID:  "req_stripe_1",
		RawJSON:    body,
	}
	parsed := normalizeResponse(resp)
	if parsed.RequestID != "req_stripe_1" {
		t.Fatalf("unexpected request id: %q", parsed.RequestID)
	}
	if parsed.Data["id"] != "prod_123" {
		t.Fatalf("unexpected product payload: %#v", parsed.Data)
	}
}
