package githubbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"si/tools/si/internal/apibridge"
)

type Client struct {
	cfg ClientConfig
	api *apibridge.Client
}

func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("github token provider is required")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = "https://api.github.com"
	}
	if strings.TrimSpace(cfg.UserAgent) == "" {
		cfg.UserAgent = "si-github/1.0"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.Logger == nil && strings.TrimSpace(cfg.LogPath) != "" {
		cfg.Logger = NewJSONLLogger(strings.TrimSpace(cfg.LogPath))
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	api, err := apibridge.NewClient(apibridge.Config{
		Component:   "githubbridge",
		BaseURL:     cfg.BaseURL,
		UserAgent:   cfg.UserAgent,
		Timeout:     cfg.Timeout,
		MaxRetries:  cfg.MaxRetries,
		Logger:      cfg.Logger,
		LogContext:  cfg.LogContext,
		HTTPClient:  client,
		SanitizeURL: sanitizeURL,
		Redact:      RedactSensitive,
		RequestIDFromHeaders: func(h http.Header) string {
			if h == nil {
				return ""
			}
			return strings.TrimSpace(h.Get("X-GitHub-Request-Id"))
		},
		RetryDecider: func(ctx context.Context, attempt int, req apibridge.Request, resp *http.Response, body []byte, callErr error) apibridge.RetryDecision {
			_ = ctx
			method := req.Method
			if callErr != nil {
				if apibridge.IsSafeMethod(method) {
					return apibridge.RetryDecision{Retry: true, Wait: apibridge.BackoffDelay(attempt)}
				}
				return apibridge.RetryDecision{}
			}
			if resp == nil || !apibridge.IsSafeMethod(method) {
				return apibridge.RetryDecision{}
			}
			statusCode := resp.StatusCode
			// Preserve existing retry semantics (ignore Retry-After for now).
			if statusCode == http.StatusTooManyRequests || statusCode == http.StatusBadGateway || statusCode == http.StatusServiceUnavailable || statusCode == http.StatusGatewayTimeout {
				return apibridge.RetryDecision{Retry: true, Wait: apibridge.BackoffDelay(attempt)}
			}
			if statusCode == http.StatusForbidden {
				if strings.TrimSpace(resp.Header.Get("X-RateLimit-Remaining")) == "0" {
					return apibridge.RetryDecision{Retry: true, Wait: apibridge.BackoffDelay(attempt)}
				}
				lower := strings.ToLower(string(body))
				if strings.Contains(lower, "secondary rate limit") || strings.Contains(lower, "abuse") {
					return apibridge.RetryDecision{Retry: true, Wait: apibridge.BackoffDelay(attempt)}
				}
			}
			if statusCode >= 500 {
				return apibridge.RetryDecision{Retry: true, Wait: apibridge.BackoffDelay(attempt)}
			}
			return apibridge.RetryDecision{}
		},
	})
	if err != nil {
		return nil, err
	}
	return &Client{cfg: cfg, api: api}, nil
}

func (c *Client) Do(ctx context.Context, req Request) (Response, error) {
	if c == nil || c.api == nil || c.cfg.Provider == nil {
		return Response{}, fmt.Errorf("github client is not initialized")
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	headers := make(map[string]string, 6+len(req.Headers))
	headers["Accept"] = "application/vnd.github+json"
	headers["X-GitHub-Api-Version"] = "2022-11-28"
	for k, v := range req.Headers {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		headers[k] = v
	}

	apiResp, err := c.api.Do(ctx, apibridge.Request{
		Method:      method,
		Path:        req.Path,
		Params:      req.Params,
		Headers:     headers,
		RawBody:     req.RawBody,
		JSONBody:    req.JSONBody,
		ContentType: req.ContentType,
		LogFields: map[string]any{
			"auth_mode": string(c.cfg.Provider.Mode()),
		},
		Prepare: func(ctx context.Context, _ int, httpReq *http.Request) error {
			tok, err := c.cfg.Provider.Token(ctx, TokenRequest{Owner: req.Owner, Repo: req.Repo, InstallationID: req.InstallationID})
			if err != nil {
				return err
			}
			httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(tok.Value))
			return nil
		},
	})
	if err != nil {
		return Response{}, err
	}

	body := strings.TrimSpace(string(apiResp.Body))
	response := normalizeResponseParts(apiResp.StatusCode, apiResp.Status, apiResp.Headers, body)
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return response, nil
	}
	return Response{}, NormalizeHTTPError(response.StatusCode, apiResp.Headers, body)
}

func normalizeResponseParts(statusCode int, status string, headers http.Header, body string) Response {
	out := Response{}
	out.StatusCode = statusCode
	out.Status = status
	out.Body = RedactSensitive(body)
	out.RequestID = strings.TrimSpace(headers.Get("X-GitHub-Request-Id"))
	if len(headers) > 0 {
		keys := make([]string, 0, len(headers))
		for key := range headers {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.Headers = make(map[string]string, len(keys))
		for _, key := range keys {
			out.Headers[key] = RedactSensitive(strings.Join(headers.Values(key), ","))
		}
	}
	if out.Body == "" {
		return out
	}
	var payload any
	if json.Unmarshal([]byte(out.Body), &payload) != nil {
		return out
	}
	switch typed := payload.(type) {
	case map[string]any:
		out.Data = typed
	case []any:
		out.List = make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			out.List = append(out.List, obj)
		}
	}
	return out
}

func (c *Client) ListAll(ctx context.Context, req Request, maxPages int) ([]map[string]any, error) {
	if c == nil {
		return nil, fmt.Errorf("github client is not initialized")
	}
	if maxPages <= 0 {
		maxPages = 10
	}
	params := cloneParams(req.Params)
	if _, ok := params["per_page"]; !ok {
		params["per_page"] = "100"
	}
	page := 1
	items := make([]map[string]any, 0, 128)
	for ; page <= maxPages; page++ {
		params["page"] = strconv.Itoa(page)
		resp, err := c.Do(ctx, Request{
			Method:         req.Method,
			Path:           req.Path,
			Params:         params,
			Headers:        req.Headers,
			RawBody:        req.RawBody,
			JSONBody:       req.JSONBody,
			ContentType:    req.ContentType,
			Owner:          req.Owner,
			Repo:           req.Repo,
			InstallationID: req.InstallationID,
		})
		if err != nil {
			return nil, err
		}
		batch := extractList(resp)
		if len(batch) == 0 {
			break
		}
		items = append(items, batch...)
		next := ""
		if resp.Headers != nil {
			next = parseNextLink(resp.Headers["Link"])
		}
		if next == "" {
			break
		}
	}
	return items, nil
}

func extractList(resp Response) []map[string]any {
	if len(resp.List) > 0 {
		return resp.List
	}
	if resp.Data == nil {
		return nil
	}
	if raw, ok := resp.Data["items"].([]any); ok {
		out := make([]map[string]any, 0, len(raw))
		for _, item := range raw {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, obj)
		}
		return out
	}
	return nil
}

func sanitizeURL(raw string) string {
	raw = RedactSensitive(raw)
	if u, err := url.Parse(raw); err == nil {
		u.RawQuery = ""
		return u.String()
	}
	return raw
}

func cloneParams(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// Note: structured logging and retries are handled by apibridge.
