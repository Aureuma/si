package stripebridge

import (
	"strings"
	"testing"
)

func TestRedactSensitive(t *testing.T) {
	raw := "token sk_live_ABC123456789 bearer Bearer xyzabc pii pi_123_secret_456"
	redacted := RedactSensitive(raw)
	if redacted == raw {
		t.Fatalf("expected redaction")
	}
	if strings.Contains(redacted, "sk_live_ABC123456789") {
		t.Fatalf("secret key leaked: %q", redacted)
	}
	if strings.Contains(redacted, "Bearer xyzabc") {
		t.Fatalf("bearer token leaked: %q", redacted)
	}
	if strings.Contains(redacted, "pi_123_secret_456") {
		t.Fatalf("client secret leaked: %q", redacted)
	}
}
