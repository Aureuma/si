package netpolicy

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestRetryAfterDelaySeconds(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "2")
	d, ok := RetryAfterDelay(h)
	if !ok {
		t.Fatalf("expected retry-after parse success")
	}
	if d != 2*time.Second {
		t.Fatalf("unexpected retry-after duration: %s", d)
	}
}

func TestRetryDelayClampsLongRetryAfter(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "120")
	d := RetryDelay(1, h)
	if d != 15*time.Second {
		t.Fatalf("expected clamp to 15s, got %s", d)
	}
}

func TestSleepForRetryContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	h := http.Header{}
	h.Set("Retry-After", "1")
	err := SleepForRetry(ctx, 1, h)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}
