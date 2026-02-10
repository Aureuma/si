package githubbridge

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestContractNormalizeRepoGetFixture(t *testing.T) {
	body, err := os.ReadFile("testdata/contracts/repo_get.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	headers := http.Header{}
	headers.Set("X-GitHub-Request-Id", "req-1")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     headers,
	}
	parsed := normalizeResponse(resp, string(body))
	if parsed.StatusCode != 200 {
		t.Fatalf("unexpected status: %d", parsed.StatusCode)
	}
	if parsed.RequestID != "req-1" {
		t.Fatalf("unexpected request id: %q", parsed.RequestID)
	}
	if strings.TrimSpace(parsed.Data["full_name"].(string)) != "octocat/Hello-World" {
		t.Fatalf("unexpected full_name payload: %#v", parsed.Data)
	}
}
