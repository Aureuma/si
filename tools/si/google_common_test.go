package main

import "testing"

func TestResolveGooglePlaceResourcePath(t *testing.T) {
	got, err := resolveGooglePlaceResourcePath("ChIJN1t_tDeuEmsRUsoyG83frY4")
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}
	if got != "/v1/places/ChIJN1t_tDeuEmsRUsoyG83frY4" {
		t.Fatalf("unexpected path: %q", got)
	}
}

func TestResolveGooglePhotoMediaPath(t *testing.T) {
	got, err := resolveGooglePhotoMediaPath("places/abc/photos/def")
	if err != nil {
		t.Fatalf("resolve photo path: %v", err)
	}
	if got != "/v1/places/abc/photos/def/media" {
		t.Fatalf("unexpected photo path: %q", got)
	}
}

func TestParseGoogleLatLng(t *testing.T) {
	point, err := parseGoogleLatLng("40.1,-70.2")
	if err != nil {
		t.Fatalf("parse latlng: %v", err)
	}
	if point["latitude"].(float64) != 40.1 {
		t.Fatalf("unexpected latitude: %#v", point)
	}
}
