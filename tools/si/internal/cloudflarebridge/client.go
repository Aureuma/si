package cloudflarebridge

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
	if strings.TrimSpace(cfg.APIToken) == "" {
		return nil, fmt.Errorf("cloudflare api token is required")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = "https://api.cloudflare.com/client/v4"
	}
	if strings.TrimSpace(cfg.UserAgent) == "" {
		cfg.UserAgent = "si-cloudflare/1.0"
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
		Component:   "cloudflarebridge",
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
			if v := strings.TrimSpace(h.Get("CF-Ray")); v != "" {
				return v
			}
			return strings.TrimSpace(h.Get("X-Request-ID"))
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
		return Response{}, fmt.Errorf("cloudflare client is not initialized")
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	headers := make(map[string]string, 4+len(req.Headers))
	headers["Authorization"] = "Bearer " + strings.TrimSpace(c.cfg.APIToken)
	headers["Accept"] = "application/json"
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
	})
	if err != nil {
		return Response{}, err
	}

	body := strings.TrimSpace(string(apiResp.Body))
	resp := normalizeResponseParts(apiResp.StatusCode, apiResp.Status, apiResp.Headers, body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && resp.Success {
		return resp, nil
	}
	return Response{}, NormalizeHTTPError(resp.StatusCode, apiResp.Headers, body)
}

func normalizeResponseParts(statusCode int, status string, headers http.Header, body string) Response {
	out := Response{}
	out.StatusCode = statusCode
	out.Status = status
	out.Body = RedactSensitive(body)
	out.Success = out.StatusCode >= 200 && out.StatusCode < 300
	out.RequestID = strings.TrimSpace(headers.Get("CF-Ray"))
	if out.RequestID == "" {
		out.RequestID = strings.TrimSpace(headers.Get("X-Request-ID"))
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
	obj, ok := parsed.(map[string]any)
	if !ok {
		return out
	}
	if success, ok := obj["success"].(bool); ok {
		out.Success = success
	}
	if msgs, ok := obj["messages"].([]any); ok {
		out.Messages = anySliceToMaps(msgs)
	}
	if result, ok := obj["result"]; ok {
		switch typed := result.(type) {
		case map[string]any:
			out.Data = typed
		case []any:
			out.List = anySliceToMaps(typed)
		default:
			out.Data = map[string]any{"value": typed}
		}
		return out
	}
	out.Data = obj
	return out
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

func (c *Client) ListAll(ctx context.Context, req Request, maxPages int) ([]map[string]any, error) {
	if c == nil {
		return nil, fmt.Errorf("cloudflare client is not initialized")
	}
	if maxPages <= 0 {
		maxPages = 10
	}
	params := cloneParams(req.Params)
	if _, ok := params["per_page"]; !ok {
		params["per_page"] = "100"
	}
	if _, ok := params["page"]; !ok {
		params["page"] = "1"
	}
	items := make([]map[string]any, 0, 128)
	for page := 1; page <= maxPages; page++ {
		params["page"] = strconv.Itoa(page)
		resp, err := c.Do(ctx, Request{
			Method:      req.Method,
			Path:        req.Path,
			Params:      params,
			Headers:     req.Headers,
			RawBody:     req.RawBody,
			JSONBody:    req.JSONBody,
			ContentType: req.ContentType,
		})
		if err != nil {
			return nil, err
		}
		batch := extractList(resp)
		if len(batch) == 0 {
			break
		}
		items = append(items, batch...)
		if len(batch) < atoiDefault(params["per_page"], 100) {
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
	if raw, ok := resp.Data["result"].([]any); ok {
		return anySliceToMaps(raw)
	}
	if raw, ok := resp.Data["items"].([]any); ok {
		return anySliceToMaps(raw)
	}
	if raw, ok := resp.Data["data"].([]any); ok {
		return anySliceToMaps(raw)
	}
	return nil
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

func atoiDefault(raw string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

// Note: structured logging and retries are handled by apibridge.
