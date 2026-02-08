package main

import "testing"

func TestNormalizeGoogleYouTubeAuthMode(t *testing.T) {
	cases := map[string]string{
		"api-key": "api-key",
		"apikey":  "api-key",
		"key":     "api-key",
		"oauth":   "oauth",
		"oauth2":  "oauth",
		"bearer":  "oauth",
		"":        "",
		"none":    "",
	}
	for input, expected := range cases {
		if got := normalizeGoogleYouTubeAuthMode(input); got != expected {
			t.Fatalf("normalizeGoogleYouTubeAuthMode(%q)=%q want %q", input, got, expected)
		}
	}
}

func TestResolveGoogleYouTubeAPIKeyByEnvironment(t *testing.T) {
	t.Setenv("GOOGLE_CORE_STAGING_YOUTUBE_API_KEY", "stage-key")
	value, source := resolveGoogleYouTubeAPIKey("core", GoogleYouTubeAccountEntry{}, "staging", "")
	if value != "stage-key" {
		t.Fatalf("unexpected key: %q", value)
	}
	if source != "env:GOOGLE_CORE_STAGING_YOUTUBE_API_KEY" {
		t.Fatalf("unexpected source: %q", source)
	}
}

func TestResolveGoogleYouTubeRuntimeContextFromEnv(t *testing.T) {
	t.Setenv("GOOGLE_TEST_YOUTUBE_API_KEY", "key-123")
	runtime, err := resolveGoogleYouTubeRuntimeContext(googleYouTubeRuntimeContextInput{AccountFlag: "test", EnvFlag: "prod", AuthModeFlag: "api-key"})
	if err != nil {
		t.Fatalf("resolve runtime: %v", err)
	}
	if runtime.AccountAlias != "test" {
		t.Fatalf("unexpected account alias: %q", runtime.AccountAlias)
	}
	if runtime.APIKey != "key-123" {
		t.Fatalf("unexpected api key: %q", runtime.APIKey)
	}
	if runtime.AuthMode != "api-key" {
		t.Fatalf("unexpected auth mode: %q", runtime.AuthMode)
	}
}
