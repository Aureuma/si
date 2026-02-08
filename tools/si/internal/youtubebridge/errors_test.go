package youtubebridge

import (
	"strings"
	"testing"
)

func TestRedactSensitive(t *testing.T) {
	in := `Bearer abc.def key=AIzaSyAaaaaaaaaaaaaaaaaaaaaaaaaaaaa client_secret=s3cr3tValue refresh_token=rToken123 access_token=aToken456`
	out := RedactSensitive(in)
	if out == in {
		t.Fatalf("expected redaction")
	}
	for _, needle := range []string{"s3cr3tValue", "rToken123", "aToken456", "abc.def"} {
		if strings.Contains(out, needle) {
			t.Fatalf("redaction leaked %q: %s", needle, out)
		}
	}
}

func TestNormalizeHTTPErrorParsesReason(t *testing.T) {
	raw := `{"error":{"code":403,"message":"quota exceeded","status":"PERMISSION_DENIED","errors":[{"reason":"quotaExceeded","message":"quota exceeded"}]}}`
	err := NormalizeHTTPError(403, nil, raw)
	if err == nil {
		t.Fatalf("expected error")
	}
	if err.Reason != "quotaExceeded" {
		t.Fatalf("unexpected reason: %q", err.Reason)
	}
	if err.Message != "quota exceeded" {
		t.Fatalf("unexpected message: %q", err.Message)
	}
}
