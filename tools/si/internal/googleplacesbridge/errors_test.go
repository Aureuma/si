package googleplacesbridge

import (
	"net/http"
	"testing"
)

func TestRedactSensitive(t *testing.T) {
	input := "key=AIzaSyExampleSecret1234567890 and Bearer tok-123"
	out := RedactSensitive(input)
	if out == input {
		t.Fatalf("expected redaction")
	}
	if out == "" {
		t.Fatalf("unexpected empty output")
	}
}

func TestNormalizeHTTPError(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Google-Request-Id", "req-2")
	err := NormalizeHTTPError(400, headers, `{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"bad request"}}`)
	if err == nil {
		t.Fatalf("expected error")
	}
	if err.StatusCode != 400 {
		t.Fatalf("unexpected status code: %d", err.StatusCode)
	}
	if err.Status != "INVALID_ARGUMENT" {
		t.Fatalf("unexpected status: %q", err.Status)
	}
	if err.RequestID != "req-2" {
		t.Fatalf("unexpected request id: %q", err.RequestID)
	}
}
