package youtubebridge

import (
	"net/http"
	"os"
	"testing"
)

func TestContractNormalizeSearchListFixture(t *testing.T) {
	body, err := os.ReadFile("testdata/contracts/search_list.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"X-Google-Request-Id": []string{"yt-1"}},
	}
	parsed := normalizeResponse(resp, string(body))
	if parsed.RequestID != "yt-1" {
		t.Fatalf("unexpected request id: %q", parsed.RequestID)
	}
	if len(parsed.List) != 1 {
		t.Fatalf("expected one search item, got: %d", len(parsed.List))
	}
}
