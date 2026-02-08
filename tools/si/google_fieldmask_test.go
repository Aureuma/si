package main

import "testing"

func TestResolveGooglePlacesFieldMaskPreset(t *testing.T) {
	mask, err := resolveGooglePlacesFieldMask(fieldMaskInput{Operation: "search-text", Preset: "search-basic", Required: true, NonInteractiveFail: true})
	if err != nil {
		t.Fatalf("resolve mask: %v", err)
	}
	if mask == "" {
		t.Fatalf("expected non-empty mask")
	}
}

func TestResolveGooglePlacesFieldMaskWildcardBlocked(t *testing.T) {
	_, err := resolveGooglePlacesFieldMask(fieldMaskInput{Operation: "details", Mask: "*", Required: true, NonInteractiveFail: true})
	if err == nil {
		t.Fatalf("expected wildcard error")
	}
}

func TestGooglePlacesFieldMaskCostHint(t *testing.T) {
	if got := googlePlacesFieldMaskCostHint("id,name"); got != "low" {
		t.Fatalf("unexpected hint: %q", got)
	}
	if got := googlePlacesFieldMaskCostHint("reviews,rating,regularOpeningHours,websiteUri"); got != "high" {
		t.Fatalf("unexpected high hint: %q", got)
	}
}
