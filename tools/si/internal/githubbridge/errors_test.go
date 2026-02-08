package githubbridge

import (
	"net/http"
	"strings"
	"testing"
)

func TestRedactSensitive(t *testing.T) {
	raw := "token ghp_ABC123 github_pat_123 Bearer abc.def.ghi -----BEGIN PRIVATE KEY-----X-----END PRIVATE KEY-----"
	redacted := RedactSensitive(raw)
	for _, forbidden := range []string{"ghp_ABC123", "github_pat_123", "Bearer abc.def.ghi", "BEGIN PRIVATE KEY-----X"} {
		if strings.Contains(redacted, forbidden) {
			t.Fatalf("leaked secret %q in %q", forbidden, redacted)
		}
	}
}

func TestNormalizeHTTPError(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-GitHub-Request-Id", "abc123")
	err := NormalizeHTTPError(401, headers, `{"message":"bad token","documentation_url":"https://docs.github.com"}`)
	if err == nil {
		t.Fatalf("expected error details")
	}
	if err.StatusCode != 401 {
		t.Fatalf("expected status 401, got %d", err.StatusCode)
	}
	if err.RequestID != "abc123" {
		t.Fatalf("expected request id, got %q", err.RequestID)
	}
	if err.Message != "bad token" {
		t.Fatalf("unexpected message: %q", err.Message)
	}
}
