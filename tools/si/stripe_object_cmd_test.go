package main

import "testing"

func TestParseStripeParams(t *testing.T) {
	params := parseStripeParams([]string{
		"name=Sample",
		"metadata[team]=infra",
		"invalid",
		"=nope",
		"currency=usd",
	})
	if len(params) != 3 {
		t.Fatalf("unexpected param count: %d (%v)", len(params), params)
	}
	if params["name"] != "Sample" {
		t.Fatalf("expected name param")
	}
	if params["metadata[team]"] != "infra" {
		t.Fatalf("expected metadata param")
	}
	if params["currency"] != "usd" {
		t.Fatalf("expected currency param")
	}
}

func TestInferStripeObjectName(t *testing.T) {
	item := map[string]any{"name": "Starter"}
	if got := inferStripeObjectName(item); got != "Starter" {
		t.Fatalf("unexpected object name %q", got)
	}
	item = map[string]any{"email": "user@example.com"}
	if got := inferStripeObjectName(item); got != "user@example.com" {
		t.Fatalf("unexpected object name %q", got)
	}
}

func TestStripeFlagsFirst(t *testing.T) {
	args := stripeFlagsFirst([]string{"product", "--limit", "1", "--json"}, map[string]bool{"json": true})
	if len(args) != 4 {
		t.Fatalf("unexpected args length: %d (%v)", len(args), args)
	}
	if args[0] != "--limit" || args[1] != "1" || args[2] != "--json" || args[3] != "product" {
		t.Fatalf("unexpected reordered args: %v", args)
	}
}

func TestStripeFlagsFirstKeepsUnknownBooleanFlagWithoutValue(t *testing.T) {
	args := stripeFlagsFirst([]string{"--transparent", "--output", "hero.png", "--json"}, map[string]bool{"json": true})
	if len(args) != 4 {
		t.Fatalf("unexpected args length: %d (%v)", len(args), args)
	}
	if args[0] != "--transparent" || args[1] != "--output" || args[2] != "hero.png" || args[3] != "--json" {
		t.Fatalf("unexpected reordered args: %v", args)
	}
}

func TestStripeFlagsFirstKeepsNegativeNumericFlagValues(t *testing.T) {
	args := stripeFlagsFirst([]string{"--offset", "-1", "--json"}, map[string]bool{"json": true})
	if len(args) != 3 {
		t.Fatalf("unexpected args length: %d (%v)", len(args), args)
	}
	if args[0] != "--offset" || args[1] != "-1" || args[2] != "--json" {
		t.Fatalf("unexpected reordered args: %v", args)
	}
}
