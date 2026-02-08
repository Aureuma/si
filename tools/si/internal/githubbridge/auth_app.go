package githubbridge

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"si/tools/si/internal/apibridge"
	"si/tools/si/internal/providers"
)

type AppProviderConfig struct {
	AppID          int64
	InstallationID int64
	PrivateKeyPEM  string
	BaseURL        string
	Owner          string
	Repo           string
	TokenSource    string
	HTTPClient     *http.Client
}

type AppProvider struct {
	cfg        AppProviderConfig
	key        *rsa.PrivateKey
	httpClient *http.Client
	mu         sync.Mutex
	cached     Token
}

func NewAppProvider(cfg AppProviderConfig) (*AppProvider, error) {
	if cfg.AppID <= 0 {
		return nil, fmt.Errorf("github app id is required")
	}
	privateKey := normalizePrivateKey(cfg.PrivateKeyPEM)
	if strings.TrimSpace(privateKey) == "" {
		return nil, fmt.Errorf("github app private key is required")
	}
	key, err := parseRSAPrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = "https://api.github.com"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &AppProvider{cfg: cfg, key: key, httpClient: client}, nil
}

func (p *AppProvider) Mode() AuthMode {
	return AuthModeApp
}

func (p *AppProvider) Source() string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.cfg.TokenSource)
}

func (p *AppProvider) Token(ctx context.Context, req TokenRequest) (Token, error) {
	if p == nil || p.key == nil {
		return Token{}, fmt.Errorf("app provider not initialized")
	}
	p.mu.Lock()
	if strings.TrimSpace(p.cached.Value) != "" && time.Until(p.cached.ExpiresAt) > time.Minute {
		cached := p.cached
		p.mu.Unlock()
		return cached, nil
	}
	p.mu.Unlock()

	installationID := p.resolveInstallationID(ctx, req)
	if installationID <= 0 {
		return Token{}, fmt.Errorf("github app installation id is required (set installation id or owner/repo context)")
	}
	jwtToken, err := p.signedJWT(time.Now().UTC())
	if err != nil {
		return Token{}, err
	}
	accessToken, err := p.exchangeInstallationToken(ctx, installationID, jwtToken)
	if err != nil {
		return Token{}, err
	}
	p.mu.Lock()
	p.cached = accessToken
	p.mu.Unlock()
	return accessToken, nil
}

func (p *AppProvider) resolveInstallationID(ctx context.Context, req TokenRequest) int64 {
	if p == nil {
		return 0
	}
	if req.InstallationID > 0 {
		return req.InstallationID
	}
	if p.cfg.InstallationID > 0 {
		return p.cfg.InstallationID
	}
	owner := strings.TrimSpace(req.Owner)
	repo := strings.TrimSpace(req.Repo)
	if owner == "" {
		owner = strings.TrimSpace(p.cfg.Owner)
	}
	if repo == "" {
		repo = strings.TrimSpace(p.cfg.Repo)
	}
	if owner == "" {
		return 0
	}
	id, err := p.lookupInstallationID(ctx, owner, repo)
	if err != nil {
		return 0
	}
	return id
}

func (p *AppProvider) signedJWT(now time.Time) (string, error) {
	claims := map[string]any{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": strconv.FormatInt(p.cfg.AppID, 10),
	}
	header := map[string]any{"alg": "RS256", "typ": "JWT"}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	signingInput := enc.EncodeToString(headerJSON) + "." + enc.EncodeToString(claimsJSON)
	hash := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + enc.EncodeToString(sig), nil
}

func (p *AppProvider) exchangeInstallationToken(ctx context.Context, installationID int64, jwtToken string) (Token, error) {
	u, err := apibridge.ResolveURL(p.cfg.BaseURL, fmt.Sprintf("/app/installations/%d/access_tokens", installationID), nil)
	if err != nil {
		return Token{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return Token{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+jwtToken)
	spec := providers.Specs[providers.GitHub]
	httpReq.Header.Set("Accept", spec.Accept)
	for k, v := range spec.DefaultHeaders {
		httpReq.Header.Set(k, v)
	}
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return Token{}, err
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	body := strings.TrimSpace(string(bodyBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Token{}, NormalizeHTTPError(resp.StatusCode, resp.Header, body)
	}
	var payload struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return Token{}, fmt.Errorf("decode installation token response: %w", err)
	}
	if strings.TrimSpace(payload.Token) == "" {
		return Token{}, fmt.Errorf("installation token response missing token")
	}
	expiresAt, _ := time.Parse(time.RFC3339, strings.TrimSpace(payload.ExpiresAt))
	return Token{Value: strings.TrimSpace(payload.Token), ExpiresAt: expiresAt}, nil
}

func (p *AppProvider) lookupInstallationID(ctx context.Context, owner string, repo string) (int64, error) {
	jwtToken, err := p.signedJWT(time.Now().UTC())
	if err != nil {
		return 0, err
	}
	try := []string{}
	if strings.TrimSpace(owner) != "" && strings.TrimSpace(repo) != "" {
		try = append(try, fmt.Sprintf("/repos/%s/%s/installation", url.PathEscape(owner), url.PathEscape(repo)))
	}
	if strings.TrimSpace(owner) != "" {
		try = append(try,
			fmt.Sprintf("/orgs/%s/installation", url.PathEscape(owner)),
			fmt.Sprintf("/users/%s/installation", url.PathEscape(owner)),
		)
	}
	for _, path := range try {
		u, urlErr := apibridge.ResolveURL(p.cfg.BaseURL, path, nil)
		if urlErr != nil {
			continue
		}
		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if reqErr != nil {
			continue
		}
			httpReq.Header.Set("Authorization", "Bearer "+jwtToken)
			spec := providers.Specs[providers.GitHub]
			httpReq.Header.Set("Accept", spec.Accept)
			for k, v := range spec.DefaultHeaders {
				httpReq.Header.Set(k, v)
			}
			resp, callErr := p.httpClient.Do(httpReq)
			if callErr != nil {
				continue
			}
		bodyBytes, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			continue
		}
		var payload struct {
			ID int64 `json:"id"`
		}
		if json.Unmarshal(bodyBytes, &payload) != nil {
			continue
		}
		if payload.ID > 0 {
			return payload.ID, nil
		}
	}
	return 0, fmt.Errorf("unable to resolve github app installation id for owner=%s repo=%s", owner, repo)
}

func normalizePrivateKey(value string) string {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "\\n") {
		value = strings.ReplaceAll(value, "\\n", "\n")
	}
	return value
}

func parseRSAPrivateKey(value string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, fmt.Errorf("invalid github app private key pem")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse github app private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("github app private key must be RSA")
	}
	return key, nil
}
