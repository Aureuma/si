package youtubebridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"si/tools/si/internal/apibridge"
	"si/tools/si/internal/providers"
)

type Client struct {
	cfg ClientConfig
	api *apibridge.Client
}

func NewClient(cfg ClientConfig) (*Client, error) {
	cfg.AuthMode = normalizeAuthMode(cfg.AuthMode)
	if cfg.AuthMode == "" {
		cfg.AuthMode = AuthModeAPIKey
	}
	if cfg.AuthMode == AuthModeAPIKey && strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("youtube api key is required for api-key mode")
	}
	if cfg.AuthMode == AuthModeOAuth && cfg.TokenProvider == nil {
		return nil, fmt.Errorf("token provider is required for oauth mode")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = providers.Specs[providers.YouTube].BaseURL
	}
	if strings.TrimSpace(cfg.UploadBaseURL) == "" {
		cfg.UploadBaseURL = providers.Specs[providers.YouTube].UploadBaseURL
	}
	if strings.TrimSpace(cfg.UserAgent) == "" {
		cfg.UserAgent = providers.Specs[providers.YouTube].UserAgent
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
		Component:   "youtubebridge",
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
			spec := providers.Specs[providers.YouTube]
			for _, k := range spec.RequestIDHeaders {
				if v := strings.TrimSpace(h.Get(k)); v != "" {
					return v
				}
			}
			return ""
		},
		RetryDecider: func(ctx context.Context, attempt int, req apibridge.Request, resp *http.Response, _ []byte, callErr error) apibridge.RetryDecision {
			_ = ctx
			if callErr != nil {
				if apibridge.IsSafeMethod(req.Method) {
					return apibridge.RetryDecision{Retry: true, Wait: apibridge.BackoffDelay(attempt)}
				}
				return apibridge.RetryDecision{}
			}
			if resp == nil || !apibridge.IsSafeMethod(req.Method) {
				return apibridge.RetryDecision{}
			}
			switch resp.StatusCode {
			case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
				return apibridge.RetryDecision{Retry: true, Wait: apibridge.BackoffDelay(attempt)}
			default:
				if resp.StatusCode >= 500 {
					return apibridge.RetryDecision{Retry: true, Wait: apibridge.BackoffDelay(attempt)}
				}
				return apibridge.RetryDecision{}
			}
		},
	})
	if err != nil {
		return nil, err
	}
	return &Client{cfg: cfg, api: api}, nil
}

func (c *Client) Do(ctx context.Context, req Request) (Response, error) {
	if c == nil || c.api == nil {
		return Response{}, fmt.Errorf("youtube client is not initialized")
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	base, params := c.resolveRequestBaseParams(req)
	headers := make(map[string]string, 4+len(req.Headers))
	headers["Accept"] = providers.Specs[providers.YouTube].Accept
	for k, v := range req.Headers {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		headers[k] = v
	}

	logFields := map[string]any{
		"auth_mode": string(c.cfg.AuthMode),
		"use_upload": req.UseUpload,
	}
	if c.cfg.TokenProvider != nil {
		logFields["auth_source"] = c.cfg.TokenProvider.Source()
	}

	apiResp, err := c.api.Do(ctx, apibridge.Request{
		Method:      method,
		BaseURL:     base,
		Path:        req.Path,
		Params:      params,
		Headers:     headers,
		RawBody:     req.RawBody,
		JSONBody:    req.JSONBody,
		ContentType: req.ContentType,
		LogFields:   logFields,
		Prepare: func(ctx context.Context, _ int, httpReq *http.Request) error {
			if c.cfg.AuthMode != AuthModeOAuth {
				return nil
			}
			tok, err := c.cfg.TokenProvider.Token(ctx)
			if err != nil {
				return err
			}
			if strings.TrimSpace(tok.Value) == "" {
				return fmt.Errorf("oauth token provider returned empty token")
			}
			httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(tok.Value))
			return nil
		},
	})
	if err != nil {
		return Response{}, err
	}

	body := strings.TrimSpace(string(apiResp.Body))
	resp := normalizeResponseParts(apiResp.StatusCode, apiResp.Status, apiResp.Headers, body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	return Response{}, NormalizeHTTPError(resp.StatusCode, apiResp.Headers, body)
}

func (c *Client) resolveRequestBaseParams(req Request) (string, map[string]string) {
	base := c.cfg.BaseURL
	if req.UseUpload {
		base = c.cfg.UploadBaseURL
	}
	params := cloneParams(req.Params)
	if c.cfg.AuthMode == AuthModeAPIKey {
		if strings.TrimSpace(params["key"]) == "" {
			params["key"] = strings.TrimSpace(c.cfg.APIKey)
		}
	}
	return base, params
}

func normalizeResponseParts(statusCode int, status string, headers http.Header, body string) Response {
	out := Response{}
	out.StatusCode = statusCode
	out.Status = status
	out.Body = RedactSensitive(body)
	out.RequestID = strings.TrimSpace(headers.Get("X-Google-Request-Id"))
	if out.RequestID == "" {
		out.RequestID = strings.TrimSpace(headers.Get("X-Request-Id"))
	}
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
	if strings.TrimSpace(body) == "" {
		return out
	}
	var parsed any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return out
	}
	switch typed := parsed.(type) {
	case []any:
		out.List = anySliceToMaps(typed)
		return out
	case map[string]any:
		out.Data = typed
		if items, ok := typed["items"].([]any); ok {
			out.List = anySliceToMaps(items)
		}
		return out
	default:
		return out
	}
}

func (c *Client) ListAll(ctx context.Context, req Request, maxPages int) ([]map[string]any, error) {
	if c == nil {
		return nil, fmt.Errorf("youtube client is not initialized")
	}
	if maxPages <= 0 {
		maxPages = 10
	}
	items := make([]map[string]any, 0, 128)
	params := cloneParams(req.Params)
	body := cloneMap(req.JSONBody)
	for page := 1; page <= maxPages; page++ {
		resp, err := c.Do(ctx, Request{
			Method:      req.Method,
			Path:        req.Path,
			Params:      params,
			Headers:     req.Headers,
			RawBody:     req.RawBody,
			JSONBody:    body,
			ContentType: req.ContentType,
			UseUpload:   req.UseUpload,
		})
		if err != nil {
			return nil, err
		}
		if len(resp.List) > 0 {
			items = append(items, resp.List...)
		}
		next := ""
		if resp.Data != nil {
			if token, ok := resp.Data["nextPageToken"].(string); ok {
				next = strings.TrimSpace(token)
			}
		}
		if next == "" {
			break
		}
		if body != nil {
			body["pageToken"] = next
		} else {
			params["pageToken"] = next
		}
	}
	return items, nil
}

func anySliceToMaps(values []any) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		obj, ok := value.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, obj)
	}
	return out
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

func cloneMap(in any) map[string]any {
	if in == nil {
		return nil
	}
	obj, ok := in.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]any, len(obj))
	for key, value := range obj {
		out[key] = value
	}
	return out
}

// Note: structured logging and retries are handled by apibridge.

func normalizeAuthMode(mode AuthMode) AuthMode {
	switch AuthMode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case AuthModeOAuth:
		return AuthModeOAuth
	case AuthModeAPIKey:
		return AuthModeAPIKey
	default:
		return ""
	}
}
