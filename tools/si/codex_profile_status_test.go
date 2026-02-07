package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchUsagePayloadWithClientParsesUsageAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Provided authentication token is expired. Please try signing in again.","code":"token_expired"},"status":401}`))
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	_, err := fetchUsagePayloadWithClient(context.Background(), client, srv.URL, profileAuthTokens{AccessToken: "x"})
	if err == nil {
		t.Fatalf("expected error")
	}
	var apiErr *usageAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected usageAPIError, got %T (%v)", err, err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unexpected status code %d", apiErr.StatusCode)
	}
	if apiErr.Code != "token_expired" {
		t.Fatalf("unexpected code %q", apiErr.Code)
	}
	if !strings.Contains(strings.ToLower(apiErr.Message), "expired") {
		t.Fatalf("unexpected message %q", apiErr.Message)
	}
	if !isExpiredAuthError(apiErr) {
		t.Fatalf("expected token-expired error to be recognized")
	}
}

func TestIsExpiredAuthErrorFalseForNonExpiredCode(t *testing.T) {
	err := &usageAPIError{
		StatusCode: http.StatusUnauthorized,
		Code:       "invalid_token",
		Message:    "invalid token",
	}
	if isExpiredAuthError(err) {
		t.Fatalf("expected non-expired error code to not match")
	}
}
