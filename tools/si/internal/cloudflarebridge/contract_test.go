package cloudflarebridge

import (
	"net/http"
	"os"
	"testing"
)

func TestContractNormalizeZonesListFixture(t *testing.T) {
	body, err := os.ReadFile("testdata/contracts/zones_list.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	headers := http.Header{}
	headers.Set("CF-Ray", "ray-1")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     headers,
	}
	parsed := normalizeResponse(resp, string(body))
	if !parsed.Success {
		t.Fatalf("expected success from fixture")
	}
	if parsed.RequestID != "ray-1" {
		t.Fatalf("unexpected request id: %q", parsed.RequestID)
	}
	if len(parsed.List) != 1 {
		t.Fatalf("expected one zone in result list, got: %d", len(parsed.List))
	}
}
