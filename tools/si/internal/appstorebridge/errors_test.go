package appstorebridge

import (
	"strings"
	"testing"
)

func TestRedactSensitive(t *testing.T) {
	raw := `Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.abc.def -----BEGIN PRIVATE KEY-----secret-----END PRIVATE KEY-----`
	clean := RedactSensitive(raw)
	if strings.Contains(clean, "abc.def") {
		t.Fatalf("expected jwt payload redacted: %s", clean)
	}
	if strings.Contains(clean, "secret") {
		t.Fatalf("expected private key redacted: %s", clean)
	}
}
