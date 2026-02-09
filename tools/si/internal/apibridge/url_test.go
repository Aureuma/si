package apibridge

import "testing"

func TestResolveURL_Relative(t *testing.T) {
	u, err := ResolveURL("https://api.example.com/base", "/v1/items", map[string]string{"a": "1", "b": "2"})
	if err != nil {
		t.Fatalf("ResolveURL: %v", err)
	}
	// Query order is stable (sorted) in ResolveURL.
	want := "https://api.example.com/v1/items?a=1&b=2"
	if u != want {
		t.Fatalf("got %q want %q", u, want)
	}
}

func TestResolveURL_Absolute(t *testing.T) {
	u, err := ResolveURL("https://api.example.com", "https://other.example.com/v1/items?x=1", map[string]string{"a": "2"})
	if err != nil {
		t.Fatalf("ResolveURL: %v", err)
	}
	// Existing query preserved, then added/overwritten.
	want := "https://other.example.com/v1/items?a=2&x=1"
	if u != want {
		t.Fatalf("got %q want %q", u, want)
	}
}

func TestJoinURL_AppendsToBasePath(t *testing.T) {
	u, err := JoinURL("https://www.googleapis.com/upload", "/youtube/v3/videos", map[string]string{"uploadType": "resumable"})
	if err != nil {
		t.Fatalf("JoinURL: %v", err)
	}
	want := "https://www.googleapis.com/upload/youtube/v3/videos?uploadType=resumable"
	if u != want {
		t.Fatalf("got %q want %q", u, want)
	}
}

func TestStripQuery(t *testing.T) {
	if got := StripQuery("https://x.test/a?b=1&c=2"); got != "https://x.test/a" {
		t.Fatalf("got %q", got)
	}
}
