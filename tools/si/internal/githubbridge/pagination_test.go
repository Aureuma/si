package githubbridge

import "testing"

func TestParseNextLink(t *testing.T) {
	header := `<https://api.github.com/repositories/1/issues?page=2>; rel="next", <https://api.github.com/repositories/1/issues?page=3>; rel="last"`
	next := parseNextLink(header)
	if next == "" {
		t.Fatalf("expected next link")
	}
	if next != "https://api.github.com/repositories/1/issues?page=2" {
		t.Fatalf("unexpected next link: %q", next)
	}
}
