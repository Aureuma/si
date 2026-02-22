package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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

func TestLoginOpenCommandForBrowser(t *testing.T) {
	if got := loginOpenCommandForBrowser("safari"); got != "safari-profile" {
		t.Fatalf("expected safari-profile, got %q", got)
	}
	if got := loginOpenCommandForBrowser("chrome"); got != "chrome-profile" {
		t.Fatalf("expected chrome-profile, got %q", got)
	}
	if got := loginOpenCommandForBrowser("firefox"); got != "" {
		t.Fatalf("expected empty command for unsupported browser, got %q", got)
	}
}

func TestNormalizeLoginDefaultBrowser(t *testing.T) {
	if got := normalizeLoginDefaultBrowser("  SAFARI "); got != "safari" {
		t.Fatalf("expected safari, got %q", got)
	}
	if got := normalizeLoginDefaultBrowser("Chrome"); got != "chrome" {
		t.Fatalf("expected chrome, got %q", got)
	}
	if got := normalizeLoginDefaultBrowser("edge"); got != "" {
		t.Fatalf("expected empty browser for unsupported value, got %q", got)
	}
}

func TestBoolEnv(t *testing.T) {
	t.Setenv("SI_BOOL_TEST", "yes")
	if got, ok := boolEnv("SI_BOOL_TEST"); !ok || !got {
		t.Fatalf("expected yes -> true, got ok=%v value=%v", ok, got)
	}
	t.Setenv("SI_BOOL_TEST", "0")
	if got, ok := boolEnv("SI_BOOL_TEST"); !ok || got {
		t.Fatalf("expected 0 -> false, got ok=%v value=%v", ok, got)
	}
	t.Setenv("SI_BOOL_TEST", "unknown")
	if _, ok := boolEnv("SI_BOOL_TEST"); ok {
		t.Fatalf("expected unknown bool value to be rejected")
	}
}

func TestIsLikelyHeadlessMachineForcedOverride(t *testing.T) {
	t.Setenv("SI_HEADLESS", "1")
	if !isLikelyHeadlessMachine() {
		t.Fatalf("expected SI_HEADLESS=1 to force headless")
	}
	t.Setenv("SI_HEADLESS", "0")
	if isLikelyHeadlessMachine() {
		t.Fatalf("expected SI_HEADLESS=0 to force non-headless")
	}
}

func TestIsLikelyHeadlessMachineLinuxDisplayHeuristic(t *testing.T) {
	t.Setenv("SI_HEADLESS", "")
	t.Setenv("HEADLESS", "")
	t.Setenv("CI", "")
	if runtime.GOOS != "linux" {
		t.Skip("linux-only display heuristic")
	}
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if !isLikelyHeadlessMachine() {
		t.Fatalf("expected linux with no DISPLAY/WAYLAND_DISPLAY to be headless")
	}
}

func TestOpenLoginURLChromeProfileCommand(t *testing.T) {
	oldOpenChrome := openChromeProfileURLFn
	t.Cleanup(func() {
		openChromeProfileURLFn = oldOpenChrome
	})
	called := false
	openChromeProfileURLFn = func(url string, profile codexProfile) error {
		called = true
		if url != "https://example.com/login" {
			t.Fatalf("unexpected url: %q", url)
		}
		if profile.ID != "dev" {
			t.Fatalf("unexpected profile: %#v", profile)
		}
		return nil
	}
	openLoginURL("https://example.com/login", codexProfile{ID: "dev"}, "chrome-profile", "")
	if !called {
		t.Fatalf("expected chrome profile opener to be called")
	}
}

func TestSelectChromeProfileDir(t *testing.T) {
	profiles := map[string]chromeProfileMeta{
		"Profile 2": {Directory: "Profile 2", Name: "Dev User"},
		"Default":   {Directory: "Default", Name: "Personal"},
	}
	got := selectChromeProfileDir(profiles, codexProfile{ID: "dev", Name: "Dev User"})
	if got != "Profile 2" {
		t.Fatalf("expected Profile 2, got %q", got)
	}
	got = selectChromeProfileDir(profiles, codexProfile{ID: "missing", Name: "Missing"})
	if got != "Default" {
		t.Fatalf("expected Default fallback, got %q", got)
	}
}

func TestSafariProfileNameCandidatesStripsLeadingEmoji(t *testing.T) {
	profile := codexProfile{ID: "america", Name: "🗽 America"}
	got := safariProfileNameCandidates(profile, "")
	if len(got) < 3 {
		t.Fatalf("expected at least 3 candidates, got %v", got)
	}
	if got[0] != "🗽 America" {
		t.Fatalf("expected first candidate to keep original name, got %q", got[0])
	}
	if got[1] != "America" {
		t.Fatalf("expected second candidate to strip emoji, got %q", got[1])
	}
	if got[2] != "america" {
		t.Fatalf("expected fallback profile id candidate, got %q", got[2])
	}
}

func TestChromeLocalStatePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", home)
	}
	path := chromeLocalStatePath()
	if path == "" {
		t.Fatalf("expected local state path")
	}
	if filepath.Base(path) != "Local State" {
		t.Fatalf("expected Local State path, got %q", path)
	}
	if runtime.GOOS != "windows" && !strings.HasPrefix(path, home) {
		t.Fatalf("expected path under home dir %q, got %q", home, path)
	}
}

func TestSafariAccessibilityHint(t *testing.T) {
	msg := safariAccessibilityHintForOS("darwin", fmt.Errorf("System Events got an error: Not authorized"))
	if !strings.Contains(strings.ToLower(msg), "accessibility") {
		t.Fatalf("expected accessibility hint, got %q", msg)
	}
}

func TestSafariAccessibilityHintFallbackMessage(t *testing.T) {
	msg := safariAccessibilityHintForOS("darwin", fmt.Errorf("exit status 1"))
	if !strings.Contains(strings.ToLower(msg), "enable it in system settings") {
		t.Fatalf("expected generic accessibility guidance, got %q", msg)
	}
}

func TestSafariAccessibilityHintNonDarwinEmpty(t *testing.T) {
	msg := safariAccessibilityHintForOS("linux", fmt.Errorf("System Events got an error"))
	if msg != "" {
		t.Fatalf("expected empty hint off darwin, got %q", msg)
	}
}
