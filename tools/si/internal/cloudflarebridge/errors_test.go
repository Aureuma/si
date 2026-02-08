package cloudflarebridge

import (
	"net/http"
	"strings"
	"testing"
)

func TestRedactSensitive(t *testing.T) {
	raw := "Bearer abc.def.ghi token_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	redacted := RedactSensitive(raw)
	if strings.Contains(redacted, "abc.def.ghi") {
		t.Fatalf("bearer token leaked: %q", redacted)
	}
}

func TestNormalizeHTTPError(t *testing.T) {
	headers := http.Header{}
	headers.Set("CF-Ray", "ray-123")
	err := NormalizeHTTPError(401, headers, `{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`)
	if err == nil {
		t.Fatalf("expected error details")
	}
	if err.StatusCode != 401 {
		t.Fatalf("expected status 401, got %d", err.StatusCode)
	}
	if err.RequestID != "ray-123" {
		t.Fatalf("expected request id, got %q", err.RequestID)
	}
	if err.Code != 10000 {
		t.Fatalf("expected code 10000, got %d", err.Code)
	}
	if err.Message != "Authentication error" {
		t.Fatalf("unexpected message: %q", err.Message)
	}
}
