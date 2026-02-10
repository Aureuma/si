package stripebridge

import (
	"net/http"
	"os"
	"testing"
)

func TestContractNormalizeProductFixture(t *testing.T) {
	body, err := os.ReadFile("testdata/contracts/product_get.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	httpResp := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Request-Id": []string{"req_stripe_1"}},
	}
	parsed := normalizeResponse(httpResp, string(body))
	if parsed.RequestID != "req_stripe_1" {
		t.Fatalf("unexpected request id: %q", parsed.RequestID)
	}
	if parsed.Data["id"] != "prod_123" {
		t.Fatalf("unexpected product payload: %#v", parsed.Data)
	}
}
