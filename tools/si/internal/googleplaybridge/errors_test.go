package googleplaybridge

import (
	"net/http"
	"strings"
	"testing"
)

func TestRedactSensitive(t *testing.T) {
	raw := `Authorization: Bearer abc.def.ghi assertion=eyJhbGciOi... access_token=ya29.token refresh_token=1//token client_email=svc@example.iam.gserviceaccount.com -----BEGIN PRIVATE KEY-----abc-----END PRIVATE KEY-----`
	clean := RedactSensitive(raw)
	for _, leak := range []string{"abc.def.ghi", "ya29.token", "1//token", "svc@example", "-----BEGIN PRIVATE KEY-----abc"} {
		if strings.Contains(clean, leak) {
			t.Fatalf("expected %q to be redacted: %s", leak, clean)
		}
	}
}

func TestNormalizeHTTPError(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Google-Request-Id", "req-1")
	err := NormalizeHTTPError(403, headers, `{"error":{"code":403,"message":"The caller does not have permission","status":"PERMISSION_DENIED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"ACCESS_TOKEN_SCOPE_INSUFFICIENT"}]}}`)
	if err.StatusCode != 403 {
		t.Fatalf("unexpected status code: %d", err.StatusCode)
	}
	if err.Code != 403 {
		t.Fatalf("unexpected code: %d", err.Code)
	}
	if err.Status != "PERMISSION_DENIED" {
		t.Fatalf("unexpected status: %q", err.Status)
	}
	if err.RequestID != "req-1" {
		t.Fatalf("unexpected request id: %q", err.RequestID)
	}
	if len(err.Details) != 1 {
		t.Fatalf("unexpected details: %#v", err.Details)
	}
}
