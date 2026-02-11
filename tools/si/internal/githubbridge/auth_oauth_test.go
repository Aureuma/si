package githubbridge

import (
	"context"
	"testing"
)

func TestNewOAuthProvider_RequiresToken(t *testing.T) {
	if _, err := NewOAuthProvider(OAuthProviderConfig{}); err == nil {
		t.Fatalf("expected error for missing token")
	}
}

func TestOAuthProviderTokenNormalizesPrefixes(t *testing.T) {
	provider, err := NewOAuthProvider(OAuthProviderConfig{AccessToken: "Bearer tok_123", TokenSource: "env:GITHUB_TOKEN"})
	if err != nil {
		t.Fatalf("new oauth provider: %v", err)
	}
	if mode := provider.Mode(); mode != AuthModeOAuth {
		t.Fatalf("unexpected mode: %s", mode)
	}
	if src := provider.Source(); src != "env:GITHUB_TOKEN" {
		t.Fatalf("unexpected source: %q", src)
	}
	tok, err := provider.Token(context.Background(), TokenRequest{})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if tok.Value != "tok_123" {
		t.Fatalf("unexpected token value: %q", tok.Value)
	}
}
