package apibridge

import (
	"context"
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

func BackoffDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := 300 * time.Millisecond
	d := base * time.Duration(1<<(attempt-1))
	if d > 3*time.Second {
		return 3 * time.Second
	}
	return d
}

// RetryAfterDelay parses Retry-After in either seconds or HTTP-date form.
// Returns (delay, ok). If ok is false, callers should use BackoffDelay.
func RetryAfterDelay(h http.Header) (time.Duration, bool) {
	if h == nil {
		return 0, false
	}
	raw := strings.TrimSpace(h.Get("Retry-After"))
	if raw == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(raw); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(raw); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0, true
		}
		return d, true
	}
	return 0, false
}

func DefaultRetryDecider(ctx context.Context, attempt int, req Request, resp *http.Response, _ []byte, callErr error) RetryDecision {
	_ = ctx
	method := req.Method
	if callErr != nil {
		if IsSafeMethod(method) {
			return RetryDecision{Retry: true, Wait: BackoffDelay(attempt)}
		}
		return RetryDecision{}
	}
	if resp == nil {
		return RetryDecision{}
	}
	if !IsSafeMethod(method) {
		return RetryDecision{}
	}
	status := resp.StatusCode
	if status == http.StatusTooManyRequests || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout || status >= 500 {
		if d, ok := RetryAfterDelay(resp.Header); ok {
			return RetryDecision{Retry: true, Wait: d}
		}
		return RetryDecision{Retry: true, Wait: BackoffDelay(attempt)}
	}
	return RetryDecision{}
}
