package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

type profileStatusResult struct {
	ID     string
	Status codexStatus
	Err    error
}

type profileAuthTokens struct {
	AccessToken string `json:"access_token"`
	AccountID   string `json:"account_id"`
}

type profileAuthFile struct {
	Tokens *profileAuthTokens `json:"tokens"`
}

type usagePayload struct {
	Email     string          `json:"email"`
	PlanType  string          `json:"plan_type"`
	RateLimit *usageRateLimit `json:"rate_limit"`
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

	status, err := fetchUsageStatus(ctx, client, usageURL, auth)
	if err != nil {
		return profileStatusResult{ID: profile.ID, Err: err}
	}

	return profileStatusResult{ID: profile.ID, Status: status}
}

func loadProfileAuthTokens(profile codexProfile) (profileAuthTokens, error) {
	path, err := codexProfileAuthPath(profile)
	if err != nil {
		return profileAuthTokens{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return profileAuthTokens{}, err
	}
	var auth profileAuthFile
	if err := json.Unmarshal(raw, &auth); err != nil {
		return profileAuthTokens{}, err
	}
	if auth.Tokens == nil {
		return profileAuthTokens{}, errors.New("auth tokens missing")
	}
	if strings.TrimSpace(auth.Tokens.AccessToken) == "" {
		return profileAuthTokens{}, errors.New("access token missing")
	}
	return *auth.Tokens, nil
}

func fetchUsageStatus(ctx context.Context, client *http.Client, usageURL string, auth profileAuthTokens) (codexStatus, error) {
	if strings.TrimSpace(usageURL) == "" {
		return codexStatus{}, errors.New("usage url missing")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usageURL, nil)
	if err != nil {
		return codexStatus{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+auth.AccessToken)
	if strings.TrimSpace(auth.AccountID) != "" {
		req.Header.Set("ChatGPT-Account-Id", strings.TrimSpace(auth.AccountID))
	}

	resp, err := client.Do(req)
	if err != nil {
		return codexStatus{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return codexStatus{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 400 {
			snippet = snippet[:400]
		}
		return codexStatus{}, fmt.Errorf("usage api status %d: %s", resp.StatusCode, snippet)
	}

	var payload usagePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return codexStatus{}, err
	}

	return codexStatusFromUsage(payload, time.Now()), nil
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
	if window.LimitWindowSeconds != nil && *window.LimitWindowSeconds > 0 {
		minutes := math.Round(float64(*window.LimitWindowSeconds) / 60.0)
		remainingMinutes = int(math.Round(minutes * left / 100.0))
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

func formatLimitColumn(pct float64, reset string) string {
	if pct < 0 {
		return "-"
	}
	reset = strings.TrimSpace(reset)
	if pct == 0 && reset == "" {
		return "-"
	}
	if reset != "" {
		return fmt.Sprintf("%.0f%% (%s)", pct, reset)
	}
	return fmt.Sprintf("%.0f%%", pct)
}
