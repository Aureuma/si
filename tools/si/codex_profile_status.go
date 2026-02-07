package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	shared "si/agents/shared/docker"
)

type profileStatusResult struct {
	ID     string
	Status codexStatus
	Err    error
}

type profileAuthTokens struct {
	AccessToken  string `json:"access_token"`
	AccountID    string `json:"account_id"`
	IDToken      string `json:"id_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

type profileAuthFile struct {
	Tokens      *profileAuthTokens `json:"tokens"`
	LastRefresh string             `json:"last_refresh,omitempty"`
}

type tokenRefreshResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
}

type usagePayload struct {
	Email     string          `json:"email"`
	PlanType  string          `json:"plan_type"`
	RateLimit *usageRateLimit `json:"rate_limit"`
}

type usageAPIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
	Status int `json:"status"`
}

type usageAPIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *usageAPIError) Error() string {
	if e == nil {
		return "usage api error"
	}
	if strings.TrimSpace(e.Code) != "" && strings.TrimSpace(e.Message) != "" {
		return fmt.Sprintf("usage api status %d (%s): %s", e.StatusCode, e.Code, e.Message)
	}
	if strings.TrimSpace(e.Message) != "" {
		return fmt.Sprintf("usage api status %d: %s", e.StatusCode, e.Message)
	}
	if strings.TrimSpace(e.Code) != "" {
		return fmt.Sprintf("usage api status %d (%s)", e.StatusCode, e.Code)
	}
	return fmt.Sprintf("usage api status %d", e.StatusCode)
}

type usageRateLimit struct {
	Primary   *usageWindow `json:"primary_window"`
	Secondary *usageWindow `json:"secondary_window"`
}

type usageWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds *int64  `json:"limit_window_seconds"`
	ResetAt            *int64  `json:"reset_at"`
	ResetAfterSeconds  *int64  `json:"reset_after_seconds"`
}

func collectProfileStatuses(items []codexProfileSummary) map[string]profileStatusResult {
	profiles := make([]codexProfile, 0, len(items))
	for _, item := range items {
		if !item.AuthCached {
			continue
		}
		profiles = append(profiles, codexProfile{
			ID:    item.ID,
			Name:  item.Name,
			Email: item.Email,
		})
	}
	if len(profiles) == 0 {
		return map[string]profileStatusResult{}
	}

	usageURL := profileUsageURL()
	client := &http.Client{Timeout: 20 * time.Second}
	concurrency := profileStatusConcurrency(len(profiles))

	results := make(chan profileStatusResult, len(profiles))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, profile := range profiles {
		profile := profile
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results <- fetchProfileStatus(profile, client, usageURL)
		}()
	}
	wg.Wait()
	close(results)

	out := make(map[string]profileStatusResult, len(profiles))
	for res := range results {
		out[res.ID] = res
	}
	return out
}

func profileUsageURL() string {
	if val := strings.TrimSpace(os.Getenv("SI_CODEX_USAGE_URL")); val != "" {
		return val
	}
	return "https://chatgpt.com/backend-api/wham/usage"
}

func profileStatusConcurrency(count int) int {
	if count <= 1 {
		return 1
	}
	cpu := runtime.NumCPU()
	if cpu < 2 {
		cpu = 2
	}
	max := cpu
	if max > 6 {
		max = 6
	}
	if count < max {
		return count
	}
	return max
}

func fetchProfileStatus(profile codexProfile, client *http.Client, usageURL string) profileStatusResult {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	auth, err := loadProfileAuthTokens(profile)
	if err != nil {
		return profileStatusResult{ID: profile.ID, Err: err}
	}

	if strings.TrimSpace(auth.AccessToken) == "" && strings.TrimSpace(auth.RefreshToken) != "" {
		if refreshed, refreshErr := refreshProfileAuthTokens(ctx, client, profile, auth); refreshErr == nil {
			auth = refreshed
		}
	}

	syncedFromContainer := false
	status, err := fetchUsageStatus(ctx, client, usageURL, auth)
	if err != nil && isExpiredAuthError(err) {
		refreshed, refreshErr := refreshProfileAuthTokens(ctx, client, profile, auth)
		if refreshErr == nil {
			auth = refreshed
			status, err = fetchUsageStatus(ctx, client, usageURL, refreshed)
		} else {
			synced, syncErr := syncProfileAuthFromContainer(ctx, profile)
			if syncErr == nil {
				syncedFromContainer = true
				auth = synced
				if strings.TrimSpace(auth.AccessToken) == "" && strings.TrimSpace(auth.RefreshToken) != "" {
					if refreshed, refreshErr := refreshProfileAuthTokens(ctx, client, profile, auth); refreshErr == nil {
						auth = refreshed
					}
				}
				status, err = fetchUsageStatus(ctx, client, usageURL, auth)
			} else {
				return profileStatusResult{ID: profile.ID, Err: fmt.Errorf("usage token expired; refresh failed (%v) and container auth sync failed: %w", refreshErr, syncErr)}
			}
		}
	}
	if err != nil && isAuthFailureError(err) && !syncedFromContainer {
		synced, syncErr := syncProfileAuthFromContainer(ctx, profile)
		if syncErr == nil {
			auth = synced
			if strings.TrimSpace(auth.AccessToken) == "" && strings.TrimSpace(auth.RefreshToken) != "" {
				if refreshed, refreshErr := refreshProfileAuthTokens(ctx, client, profile, auth); refreshErr == nil {
					auth = refreshed
				}
			}
			status, err = fetchUsageStatus(ctx, client, usageURL, auth)
		}
	}
	if err != nil {
		return profileStatusResult{ID: profile.ID, Err: err}
	}

	return profileStatusResult{ID: profile.ID, Status: status}
}

func loadProfileAuthTokens(profile codexProfile) (profileAuthTokens, error) {
	_, auth, err := loadProfileAuthFile(profile)
	if err != nil {
		return profileAuthTokens{}, err
	}
	return *auth.Tokens, nil
}

func loadProfileAuthFile(profile codexProfile) (string, profileAuthFile, error) {
	path, err := codexProfileAuthPath(profile)
	if err != nil {
		return "", profileAuthFile{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", profileAuthFile{}, err
	}
	var auth profileAuthFile
	if err := json.Unmarshal(raw, &auth); err != nil {
		return "", profileAuthFile{}, err
	}
	if auth.Tokens == nil {
		return "", profileAuthFile{}, errors.New("auth tokens missing")
	}
	if strings.TrimSpace(auth.Tokens.AccessToken) == "" {
		return "", profileAuthFile{}, errors.New("access token missing")
	}
	return path, auth, nil
}

func saveProfileAuthFile(path string, auth profileAuthFile) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("auth path missing")
	}
	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("empty auth payload")
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "auth-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func refreshProfileAuthTokens(ctx context.Context, client *http.Client, profile codexProfile, current profileAuthTokens) (profileAuthTokens, error) {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	if strings.TrimSpace(current.RefreshToken) == "" {
		return profileAuthTokens{}, errors.New("refresh token missing")
	}
	clientID, err := profileOAuthClientID(current)
	if err != nil {
		return profileAuthTokens{}, err
	}
	refreshURL := profileTokenURL()
	reqBody := map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     clientID,
		"refresh_token": strings.TrimSpace(current.RefreshToken),
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return profileAuthTokens{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshURL, strings.NewReader(string(body)))
	if err != nil {
		return profileAuthTokens{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return profileAuthTokens{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return profileAuthTokens{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &usageAPIError{StatusCode: resp.StatusCode}
		var parsed usageAPIErrorResponse
		if err := json.Unmarshal(respBody, &parsed); err == nil {
			apiErr.Code = strings.TrimSpace(parsed.Error.Code)
			apiErr.Message = strings.TrimSpace(parsed.Error.Message)
		}
		if apiErr.Message == "" {
			snippet := strings.TrimSpace(string(respBody))
			if len(snippet) > 200 {
				snippet = snippet[:200]
			}
			apiErr.Message = snippet
		}
		return profileAuthTokens{}, apiErr
	}
	var refreshed tokenRefreshResponse
	if err := json.Unmarshal(respBody, &refreshed); err != nil {
		return profileAuthTokens{}, err
	}
	if strings.TrimSpace(refreshed.AccessToken) == "" {
		return profileAuthTokens{}, errors.New("refreshed access token missing")
	}

	updated := current
	updated.AccessToken = strings.TrimSpace(refreshed.AccessToken)
	if strings.TrimSpace(refreshed.IDToken) != "" {
		updated.IDToken = strings.TrimSpace(refreshed.IDToken)
	}
	if strings.TrimSpace(refreshed.RefreshToken) != "" {
		updated.RefreshToken = strings.TrimSpace(refreshed.RefreshToken)
	}

	path, authFile, err := loadProfileAuthFile(profile)
	if err != nil {
		return profileAuthTokens{}, err
	}
	authFile.Tokens = &updated
	authFile.LastRefresh = time.Now().UTC().Format(time.RFC3339Nano)
	if err := saveProfileAuthFile(path, authFile); err != nil {
		return profileAuthTokens{}, err
	}
	return updated, nil
}

func profileTokenURL() string {
	if val := strings.TrimSpace(os.Getenv("SI_CODEX_TOKEN_URL")); val != "" {
		return val
	}
	return "https://auth.openai.com/oauth/token"
}

func profileOAuthClientID(auth profileAuthTokens) (string, error) {
	idToken := strings.TrimSpace(auth.IDToken)
	if idToken == "" {
		return "", errors.New("id token missing")
	}
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return "", errors.New("invalid id token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var claims struct {
		Aud interface{} `json:"aud"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", err
	}
	switch aud := claims.Aud.(type) {
	case string:
		aud = strings.TrimSpace(aud)
		if aud != "" {
			return aud, nil
		}
	case []interface{}:
		for _, item := range aud {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s), nil
			}
		}
	}
	return "", errors.New("oauth client id missing in id token")
}

func fetchUsageStatus(ctx context.Context, client *http.Client, usageURL string, auth profileAuthTokens) (codexStatus, error) {
	payload, err := fetchUsagePayloadWithClient(ctx, client, usageURL, auth)
	if err != nil {
		return codexStatus{}, err
	}
	return codexStatusFromUsage(payload, time.Now()), nil
}

func fetchUsagePayload(ctx context.Context, usageURL string, auth profileAuthTokens) (usagePayload, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	return fetchUsagePayloadWithClient(ctx, client, usageURL, auth)
}

func fetchUsagePayloadWithClient(ctx context.Context, client *http.Client, usageURL string, auth profileAuthTokens) (usagePayload, error) {
	if strings.TrimSpace(usageURL) == "" {
		return usagePayload{}, errors.New("usage url missing")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usageURL, nil)
	if err != nil {
		return usagePayload{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+auth.AccessToken)
	if strings.TrimSpace(auth.AccountID) != "" {
		req.Header.Set("ChatGPT-Account-Id", strings.TrimSpace(auth.AccountID))
	}

	resp, err := client.Do(req)
	if err != nil {
		return usagePayload{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return usagePayload{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &usageAPIError{StatusCode: resp.StatusCode}
		var parsed usageAPIErrorResponse
		if err := json.Unmarshal(body, &parsed); err == nil {
			apiErr.Code = strings.TrimSpace(parsed.Error.Code)
			apiErr.Message = strings.TrimSpace(parsed.Error.Message)
		}
		if apiErr.Message == "" {
			snippet := strings.TrimSpace(string(body))
			if len(snippet) > 200 {
				snippet = snippet[:200]
			}
			apiErr.Message = snippet
		}
		return usagePayload{}, apiErr
	}

	var payload usagePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return usagePayload{}, err
	}
	return payload, nil
}

func isExpiredAuthError(err error) bool {
	var apiErr *usageAPIError
	if errors.As(err, &apiErr) {
		return strings.EqualFold(strings.TrimSpace(apiErr.Code), "token_expired")
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "token_expired") || strings.Contains(msg, "token is expired")
}

func isRefreshTokenReusedError(err error) bool {
	var apiErr *usageAPIError
	if errors.As(err, &apiErr) {
		return strings.EqualFold(strings.TrimSpace(apiErr.Code), "refresh_token_reused")
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "refresh_token_reused")
}

func isAuthFailureError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *usageAPIError
	if errors.As(err, &apiErr) {
		code := strings.ToLower(strings.TrimSpace(apiErr.Code))
		if apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden {
			return true
		}
		if strings.Contains(code, "token") || strings.Contains(code, "auth") || strings.Contains(code, "credential") {
			return true
		}
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "token_expired") ||
		strings.Contains(msg, "invalid token") ||
		strings.Contains(msg, "refresh token") ||
		strings.Contains(msg, "authentication") ||
		strings.Contains(msg, "unauthorized")
}

func syncProfileAuthFromContainer(ctx context.Context, profile codexProfile) (profileAuthTokens, error) {
	client, err := shared.NewClient()
	if err != nil {
		return profileAuthTokens{}, err
	}
	defer client.Close()

	refs, err := codexContainersByProfile(ctx, client, profile.ID)
	if err != nil {
		return profileAuthTokens{}, err
	}
	if len(refs) == 0 {
		return profileAuthTokens{}, fmt.Errorf("no codex container found for profile %s", profile.ID)
	}
	preferred := choosePreferredCodexContainer(refs, codexContainerName(profile.ID))
	id, _, err := client.ContainerByName(ctx, preferred.Name)
	if err != nil {
		return profileAuthTokens{}, err
	}
	if strings.TrimSpace(id) == "" {
		return profileAuthTokens{}, fmt.Errorf("codex container %s not found", preferred.Name)
	}
	if err := cacheCodexAuthFromContainer(ctx, client, id, profile); err != nil {
		return profileAuthTokens{}, err
	}
	return loadProfileAuthTokens(profile)
}

func codexStatusFromUsage(payload usagePayload, now time.Time) codexStatus {
	status := codexStatus{
		Source:          "usage-api",
		FiveHourLeftPct: -1,
		WeeklyLeftPct:   -1,
	}
	status.AccountEmail = strings.TrimSpace(payload.Email)
	status.AccountPlan = strings.TrimSpace(payload.PlanType)
	if payload.RateLimit == nil {
		return status
	}

	left, remaining, reset := usageWindowRemaining(payload.RateLimit.Primary, now)
	status.FiveHourLeftPct = left
	status.FiveHourRemaining = remaining
	status.FiveHourReset = reset

	left, remaining, reset = usageWindowRemaining(payload.RateLimit.Secondary, now)
	status.WeeklyLeftPct = left
	status.WeeklyRemaining = remaining
	status.WeeklyReset = reset

	return status
}

func usageWindowRemaining(window *usageWindow, now time.Time) (float64, int, string) {
	if window == nil {
		return -1, 0, ""
	}
	used := window.UsedPercent
	if used < 0 || used > 100 {
		return -1, 0, ""
	}
	left := 100 - used
	remainingMinutes := 0
	if window.ResetAt != nil && *window.ResetAt > 0 {
		resetAt := time.Unix(*window.ResetAt, 0)
		if resetAt.After(now) {
			remainingMinutes = int(math.Ceil(resetAt.Sub(now).Minutes()))
		}
	}
	if remainingMinutes <= 0 && window.ResetAfterSeconds != nil && *window.ResetAfterSeconds > 0 {
		remainingMinutes = int(math.Ceil(float64(*window.ResetAfterSeconds) / 60.0))
	}
	if window.LimitWindowSeconds != nil && *window.LimitWindowSeconds > 0 {
		minutes := math.Round(float64(*window.LimitWindowSeconds) / 60.0)
		if remainingMinutes <= 0 {
			remainingMinutes = int(math.Round(minutes * left / 100.0))
		}
	}
	reset := usageResetAt(window, now)
	return left, remainingMinutes, reset
}

func usageResetAt(window *usageWindow, now time.Time) string {
	if window == nil {
		return ""
	}
	if window.ResetAt != nil && *window.ResetAt > 0 {
		return formatResetAt(time.Unix(*window.ResetAt, 0).Local())
	}
	if window.ResetAfterSeconds != nil && *window.ResetAfterSeconds > 0 {
		return formatResetAt(now.Add(time.Duration(*window.ResetAfterSeconds) * time.Second))
	}
	return ""
}

func formatLimitColumn(pct float64, reset string, remainingMinutes int) string {
	if pct < 0 {
		return "-"
	}
	reset = strings.TrimSpace(reset)
	if pct == 0 && reset == "" {
		return "-"
	}
	remaining := formatRemainingDuration(remainingMinutes)
	if reset != "" && remaining != "" {
		return styleLimitTextByPct(fmt.Sprintf("%.0f%% (%s, in %s)", pct, reset, remaining), pct)
	}
	if reset != "" {
		return styleLimitTextByPct(fmt.Sprintf("%.0f%% (%s)", pct, reset), pct)
	}
	if remaining != "" {
		return styleLimitTextByPct(fmt.Sprintf("%.0f%% (in %s)", pct, remaining), pct)
	}
	return styleLimitTextByPct(fmt.Sprintf("%.0f%%", pct), pct)
}

func formatLimitDetail(pct float64, reset string, remainingMinutes int) string {
	if pct < 0 {
		return "-"
	}
	remaining := formatRemainingDuration(remainingMinutes)
	switch {
	case reset != "" && remaining != "":
		return styleLimitTextByPct(fmt.Sprintf("%.0f%% left (resets %s, in %s)", pct, reset, remaining), pct)
	case reset != "":
		return styleLimitTextByPct(fmt.Sprintf("%.0f%% left (resets %s)", pct, reset), pct)
	case remaining != "":
		return styleLimitTextByPct(fmt.Sprintf("%.0f%% left (in %s)", pct, remaining), pct)
	default:
		return styleLimitTextByPct(fmt.Sprintf("%.0f%% left", pct), pct)
	}
}

func formatRemainingDuration(minutes int) string {
	if minutes <= 0 {
		return ""
	}
	hours := minutes / 60
	mins := minutes % 60
	switch {
	case hours > 0 && mins > 0:
		return fmt.Sprintf("%dh%02dm", hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}
