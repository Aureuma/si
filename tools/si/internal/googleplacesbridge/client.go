package googleplacesbridge

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
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("google places api key is required")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = providers.Specs[providers.GooglePlaces].BaseURL
	}
	if strings.TrimSpace(cfg.UserAgent) == "" {
		cfg.UserAgent = providers.Specs[providers.GooglePlaces].UserAgent
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
		Component:   "googleplacesbridge",
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
			spec := providers.Specs[providers.GooglePlaces]
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
			wait := apibridge.BackoffDelay(attempt)
			if d, ok := apibridge.RetryAfterDelay(resp.Header); ok {
				wait = d
			}
			switch resp.StatusCode {
			case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
				return apibridge.RetryDecision{Retry: true, Wait: wait}
			default:
				if resp.StatusCode >= 500 {
					return apibridge.RetryDecision{Retry: true, Wait: wait}
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
		return Response{}, fmt.Errorf("google places client is not initialized")
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	headers := make(map[string]string, 6+len(req.Headers))
	headers["X-Goog-Api-Key"] = strings.TrimSpace(c.cfg.APIKey)
	headers["Accept"] = providers.Specs[providers.GooglePlaces].Accept
	if fieldMask := strings.TrimSpace(req.FieldMask); fieldMask != "" {
		headers["X-Goog-FieldMask"] = fieldMask
	}
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
			"field_mask": RedactSensitive(strings.TrimSpace(req.FieldMask)),
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

func normalizeResponseParts(statusCode int, status string, headers http.Header, body string) Response {
	out := Response{}
	out.StatusCode = statusCode
	out.Status = status
	out.Body = RedactSensitive(body)
	out.RequestID = strings.TrimSpace(headers.Get("X-Request-Id"))
	if out.RequestID == "" {
		out.RequestID = strings.TrimSpace(headers.Get("X-Google-Request-Id"))
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
	if places, ok := obj["places"].([]any); ok {
		out.List = anySliceToMaps(places)
		out.Data = obj
		return out
	}
	if suggestions, ok := obj["suggestions"].([]any); ok {
		out.List = anySliceToMaps(suggestions)
		out.Data = obj
		return out
	}
	if place, ok := obj["place"].(map[string]any); ok {
		out.Data = place
		return out
	}
	out.Data = obj
	return out
}

func (c *Client) ListAll(ctx context.Context, req Request, maxPages int, tokenField string) ([]map[string]any, error) {
	if c == nil {
		return nil, fmt.Errorf("google places client is not initialized")
	}
	if maxPages <= 0 {
		maxPages = 10
	}
	if strings.TrimSpace(tokenField) == "" {
		tokenField = "nextPageToken"
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
			FieldMask:   req.FieldMask,
		})
		if err != nil {
			return nil, err
		}
		batch := extractList(resp)
		if len(batch) > 0 {
			items = append(items, batch...)
		}
		next := ""
		if resp.Data != nil {
			if token, ok := resp.Data[tokenField].(string); ok {
				next = strings.TrimSpace(token)
			}
		}
		if next == "" {
			break
		}
		if body != nil {
			body[tokenField] = next
		} else {
			params[tokenField] = next
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
	if raw, ok := resp.Data["places"].([]any); ok {
		return anySliceToMaps(raw)
	}
	if raw, ok := resp.Data["suggestions"].([]any); ok {
		return anySliceToMaps(raw)
	}
	return nil
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
