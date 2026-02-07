package main

import (
	"testing"

	"si/tools/si/internal/stripebridge"
)

func TestStripeE2E_AuthAndRaw(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stripe e2e in short mode")
	}
	if envOr("SI_STRIPE_E2E", "") != "1" {
		t.Skip("set SI_STRIPE_E2E=1 to run live stripe e2e tests")
	}
	if envOr("SI_STRIPE_API_KEY", "") == "" {
		t.Skip("SI_STRIPE_API_KEY not configured")
	}
	runtime, err := resolveStripeRuntimeContext("", "sandbox", "")
	if err != nil {
		t.Fatalf("resolve runtime: %v", err)
	}
	client, err := buildStripeClient(runtime)
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	resp, err := client.Do(t.Context(), stripebridge.Request{
		Method: "GET",
		Path:   "/v1/balance",
	})
	if err != nil {
		t.Fatalf("balance request failed: %v", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("unexpected status code: %d", resp.StatusCode)
	}
}
