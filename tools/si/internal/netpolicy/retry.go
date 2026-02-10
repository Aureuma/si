package netpolicy

import (
	"context"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func IsSafeMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func RetryAfterDelay(headers http.Header) (time.Duration, bool) {
	if headers == nil {
		return 0, false
	}
	raw := strings.TrimSpace(headers.Get("Retry-After"))
	if raw == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second, true
	}
	if when, err := http.ParseTime(raw); err == nil {
		d := time.Until(when)
		if d < 0 {
			return 0, true
		}
		return d, true
	}
	return 0, false
}

func BackoffJitterDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := 300 * time.Millisecond
	delay := base * time.Duration(1<<(attempt-1))
	if delay > 3*time.Second {
		delay = 3 * time.Second
	}
	if delay <= 1*time.Millisecond {
		return delay
	}
	min := delay / 2
	jitter := time.Duration(rand.Int63n(int64(delay-min) + 1))
	return min + jitter
}

func RetryDelay(attempt int, headers http.Header) time.Duration {
	if d, ok := RetryAfterDelay(headers); ok {
		if d < 0 {
			return 0
		}
		if d > 15*time.Second {
			return 15 * time.Second
		}
		return d
	}
	return BackoffJitterDelay(attempt)
}

func SleepForRetry(ctx context.Context, attempt int, headers http.Header) error {
	d := RetryDelay(attempt, headers)
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
