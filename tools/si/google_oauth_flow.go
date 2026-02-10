package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"si/tools/si/internal/youtubebridge"
)

type googleOAuthDeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int64  `json:"expires_in"`
	Interval        int64  `json:"interval"`
}

type googleOAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

func defaultGoogleYouTubeScopes() []string {
	return []string{
		"https://www.googleapis.com/auth/youtube.readonly",
		"https://www.googleapis.com/auth/youtube",
		"https://www.googleapis.com/auth/youtube.force-ssl",
		"https://www.googleapis.com/auth/youtube.upload",
	}
}

func startGoogleOAuthDeviceFlow(ctx context.Context, clientID string, scopes []string) (googleOAuthDeviceCodeResponse, error) {
	if strings.TrimSpace(clientID) == "" {
		return googleOAuthDeviceCodeResponse{}, fmt.Errorf("oauth client id is required")
	}
	if len(scopes) == 0 {
		scopes = defaultGoogleYouTubeScopes()
	}
	form := url.Values{}
	form.Set("client_id", strings.TrimSpace(clientID))
	form.Set("scope", strings.Join(scopes, " "))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/device/code", strings.NewReader(form.Encode()))
	if err != nil {
		return googleOAuthDeviceCodeResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return googleOAuthDeviceCodeResponse{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return googleOAuthDeviceCodeResponse{}, fmt.Errorf("device code request failed: %s", strings.TrimSpace(string(body)))
	}
	var parsed googleOAuthDeviceCodeResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return googleOAuthDeviceCodeResponse{}, err
	}
	if strings.TrimSpace(parsed.DeviceCode) == "" {
		return googleOAuthDeviceCodeResponse{}, fmt.Errorf("device code response missing device_code")
	}
	if parsed.Interval <= 0 {
		parsed.Interval = 5
	}
	return parsed, nil
}

func pollGoogleOAuthDeviceToken(ctx context.Context, clientID, clientSecret, deviceCode string, intervalSec int64, timeout time.Duration) (googleOAuthTokenResponse, error) {
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	if intervalSec <= 0 {
		intervalSec = 5
	}
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return googleOAuthTokenResponse{}, fmt.Errorf("device authorization timed out")
		}
		select {
		case <-ctx.Done():
			return googleOAuthTokenResponse{}, ctx.Err()
		default:
		}
		resp, err := exchangeGoogleOAuthDeviceToken(ctx, clientID, clientSecret, deviceCode)
		if err == nil {
			return resp, nil
		}
		apiErr, ok := err.(*youtubebridge.APIErrorDetails)
		if !ok {
			return googleOAuthTokenResponse{}, err
		}
		reason := strings.ToLower(strings.TrimSpace(apiErr.Reason))
		msg := strings.ToLower(strings.TrimSpace(apiErr.Message))
		switch {
		case strings.Contains(msg, "authorization_pending") || reason == "authorization_pending":
			// keep polling
		case strings.Contains(msg, "slow_down") || reason == "slow_down":
			intervalSec += 2
		case strings.Contains(msg, "access_denied") || reason == "access_denied":
			return googleOAuthTokenResponse{}, fmt.Errorf("device authorization denied")
		case strings.Contains(msg, "expired_token") || reason == "expired_token":
			return googleOAuthTokenResponse{}, fmt.Errorf("device code expired")
		default:
			return googleOAuthTokenResponse{}, err
		}
		time.Sleep(time.Duration(intervalSec) * time.Second)
	}
}

func exchangeGoogleOAuthDeviceToken(ctx context.Context, clientID, clientSecret, deviceCode string) (googleOAuthTokenResponse, error) {
	form := url.Values{}
	form.Set("client_id", strings.TrimSpace(clientID))
	if strings.TrimSpace(clientSecret) != "" {
		form.Set("client_secret", strings.TrimSpace(clientSecret))
	}
	form.Set("device_code", strings.TrimSpace(deviceCode))
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	return exchangeGoogleOAuthToken(ctx, form)
}

func refreshGoogleOAuthAccessToken(ctx context.Context, clientID, clientSecret, refreshToken string) (googleOAuthTokenResponse, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return googleOAuthTokenResponse{}, fmt.Errorf("refresh token is required")
	}
	form := url.Values{}
	form.Set("client_id", strings.TrimSpace(clientID))
	if strings.TrimSpace(clientSecret) != "" {
		form.Set("client_secret", strings.TrimSpace(clientSecret))
	}
	form.Set("refresh_token", strings.TrimSpace(refreshToken))
	form.Set("grant_type", "refresh_token")
	return exchangeGoogleOAuthToken(ctx, form)
}

func exchangeGoogleOAuthToken(ctx context.Context, form url.Values) (googleOAuthTokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return googleOAuthTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return googleOAuthTokenResponse{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return googleOAuthTokenResponse{}, youtubebridge.NormalizeHTTPError(resp.StatusCode, resp.Header, string(body))
	}
	var parsed googleOAuthTokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return googleOAuthTokenResponse{}, err
	}
	if strings.TrimSpace(parsed.AccessToken) == "" {
		return googleOAuthTokenResponse{}, fmt.Errorf("oauth token response missing access_token")
	}
	return parsed, nil
}

func googleOAuthExpiresAt(expiresIn int64) time.Time {
	if expiresIn <= 0 {
		return time.Time{}
	}
	return time.Now().UTC().Add(time.Duration(expiresIn) * time.Second)
}

type googleYouTubeTokenProvider struct {
	alias        string
	env          string
	clientID     string
	clientSecret string
	source       string
	mu           sync.Mutex
	accessToken  string
	expiresAt    time.Time
	refreshToken string
}

func buildGoogleYouTubeTokenProvider(runtime googleYouTubeRuntimeContext) (youtubebridge.TokenProvider, error) {
	provider := &googleYouTubeTokenProvider{
		alias:        strings.TrimSpace(runtime.AccountAlias),
		env:          strings.TrimSpace(runtime.Environment),
		clientID:     strings.TrimSpace(runtime.OAuth.ClientID),
		clientSecret: strings.TrimSpace(runtime.OAuth.ClientSecret),
		source:       strings.TrimSpace(runtime.TokenSource),
		accessToken:  strings.TrimSpace(runtime.OAuth.AccessToken),
		refreshToken: strings.TrimSpace(runtime.OAuth.RefreshToken),
	}
	if provider.refreshToken == "" {
		if entry, ok := loadGoogleOAuthTokenEntry(provider.alias, provider.env); ok {
			if provider.accessToken == "" {
				provider.accessToken = strings.TrimSpace(entry.AccessToken)
				provider.expiresAt = parseGoogleOAuthExpiry(entry.ExpiresAt)
			}
			provider.refreshToken = strings.TrimSpace(entry.RefreshToken)
			if provider.source == "" {
				provider.source = "store"
			}
		}
	}
	if provider.accessToken == "" && provider.refreshToken == "" {
		return nil, fmt.Errorf("oauth token not found (run `si google youtube auth login` or set GOOGLE_<ACCOUNT>_YOUTUBE_REFRESH_TOKEN)")
	}
	if provider.source == "" {
		provider.source = "runtime"
	}
	return provider, nil
}

func (p *googleYouTubeTokenProvider) Source() string {
	if p == nil {
		return ""
	}
	if strings.TrimSpace(p.source) != "" {
		return p.source
	}
	return "oauth"
}

func (p *googleYouTubeTokenProvider) Token(ctx context.Context) (youtubebridge.Token, error) {
	if p == nil {
		return youtubebridge.Token{}, fmt.Errorf("oauth token provider is nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().UTC()
	if strings.TrimSpace(p.accessToken) != "" {
		if p.expiresAt.IsZero() || now.Before(p.expiresAt.Add(-45*time.Second)) {
			return youtubebridge.Token{Value: p.accessToken, ExpiresAt: p.expiresAt}, nil
		}
	}
	if strings.TrimSpace(p.refreshToken) == "" {
		if strings.TrimSpace(p.accessToken) != "" {
			return youtubebridge.Token{Value: p.accessToken, ExpiresAt: p.expiresAt}, nil
		}
		return youtubebridge.Token{}, fmt.Errorf("oauth refresh token is missing")
	}
	if strings.TrimSpace(p.clientID) == "" {
		return youtubebridge.Token{}, fmt.Errorf("oauth client id is required to refresh token")
	}
	refreshed, err := refreshGoogleOAuthAccessToken(ctx, p.clientID, p.clientSecret, p.refreshToken)
	if err != nil {
		return youtubebridge.Token{}, err
	}
	p.accessToken = strings.TrimSpace(refreshed.AccessToken)
	if strings.TrimSpace(refreshed.RefreshToken) != "" {
		p.refreshToken = strings.TrimSpace(refreshed.RefreshToken)
	}
	p.expiresAt = googleOAuthExpiresAt(refreshed.ExpiresIn)
	_ = saveGoogleOAuthTokenEntry(p.alias, p.env, googleOAuthTokenEntry{
		AccountAlias: p.alias,
		Environment:  p.env,
		AccessToken:  p.accessToken,
		RefreshToken: p.refreshToken,
		TokenType:    refreshed.TokenType,
		Scope:        refreshed.Scope,
		ExpiresAt:    p.expiresAt.UTC().Format(time.RFC3339),
	})
	return youtubebridge.Token{Value: p.accessToken, ExpiresAt: p.expiresAt}, nil
}
