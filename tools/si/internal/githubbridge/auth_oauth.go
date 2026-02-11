package githubbridge

import (
	"context"
	"fmt"
	"strings"
)

type OAuthProviderConfig struct {
	AccessToken string
	TokenSource string
}

type OAuthProvider struct {
	cfg OAuthProviderConfig
}

func NewOAuthProvider(cfg OAuthProviderConfig) (*OAuthProvider, error) {
	if strings.TrimSpace(cfg.AccessToken) == "" {
		return nil, fmt.Errorf("github oauth access token is required")
	}
	return &OAuthProvider{cfg: cfg}, nil
}

func (p *OAuthProvider) Mode() AuthMode {
	return AuthModeOAuth
}

func (p *OAuthProvider) Source() string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.cfg.TokenSource)
}

func (p *OAuthProvider) Token(_ context.Context, _ TokenRequest) (Token, error) {
	if p == nil {
		return Token{}, fmt.Errorf("oauth provider not initialized")
	}
	value := strings.TrimSpace(p.cfg.AccessToken)
	if value == "" {
		return Token{}, fmt.Errorf("github oauth access token is required")
	}
	value = strings.TrimPrefix(value, "Bearer ")
	value = strings.TrimPrefix(value, "bearer ")
	value = strings.TrimPrefix(value, "token ")
	value = strings.TrimSpace(value)
	if value == "" {
		return Token{}, fmt.Errorf("github oauth access token is required")
	}
	return Token{Value: value}, nil
}
