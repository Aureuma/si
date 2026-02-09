package main

import "testing"

func TestStripANSIEscapes(t *testing.T) {
	input := "\x1b[31mhttps://example.com\x1b[0m\x1b]8;;https://alt.example.com\x07"
	input += " %1B%5B0m"
	got := stripANSIEscapes(input)
	want := "https://example.com "
	if got != want {
		t.Fatalf("stripANSIEscapes() = %q, want %q", got, want)
	}
}

func TestCleanLoginURL(t *testing.T) {
	input := "  https://example.com/abc?x=1).]>\"'  "
	got := cleanLoginURL(input)
	want := "https://example.com/abc?x=1"
	if got != want {
		t.Fatalf("cleanLoginURL() = %q, want %q", got, want)
	}
}

func TestPickLoginURLPrefersHTTPS(t *testing.T) {
	urls := []string{"http://example.com", "https://secure.example.com"}
	got := pickLoginURL(urls)
	want := "https://secure.example.com"
	if got != want {
		t.Fatalf("pickLoginURL() = %q, want %q", got, want)
	}
}

func TestPickLoginURLFallback(t *testing.T) {
	urls := []string{"http://example.com).", "http://second.example.com"}
	got := pickLoginURL(urls)
	want := "http://example.com"
	if got != want {
		t.Fatalf("pickLoginURL() = %q, want %q", got, want)
	}
}

func TestExpandLoginOpenCommand(t *testing.T) {
	profile := codexProfile{ID: "dev", Name: "Dev User", Email: "dev@example.com"}
	template := "open {url} --profile {profile} --name {profile_name} --email {profile_email}"
	url := "https://example.com/login?token=abc"
	got := expandLoginOpenCommand(template, url, profile)
	want := "open " + shellSingleQuote(url) +
		" --profile " + shellSingleQuote(profile.ID) +
		" --name " + shellSingleQuote(profile.Name) +
		" --email " + shellSingleQuote(profile.Email)
	if got != want {
		t.Fatalf("expandLoginOpenCommand() = %q, want %q", got, want)
	}
}
