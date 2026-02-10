package googleplacesbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"si/tools/si/internal/httpx"
	"si/tools/si/internal/integrationruntime"
	"si/tools/si/internal/providers"
)

type Client struct {
	cfg        ClientConfig
	httpClient *http.Client
	log        EventLogger
}

func NewClient(cfg ClientConfig) (*Client, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("google places api key is required")
	}
	spec := providers.Resolve(providers.GooglePlaces)
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = spec.BaseURL
	}
	if strings.TrimSpace(cfg.UserAgent) == "" {
		cfg.UserAgent = spec.UserAgent
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
		client = httpx.SharedClient(cfg.Timeout)
	}
	return &Client{cfg: cfg, httpClient: client, log: cfg.Logger}, nil
}

func (c *Client) Do(ctx context.Context, req Request) (Response, error) {
	if c == nil {
		return Response{}, fmt.Errorf("google places client is not initialized")
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	endpoint, err := resolveURL(c.cfg.BaseURL, req.Path, req.Params)
	if err != nil {
		return Response{}, err
	}
	subject := strings.TrimSpace(c.cfg.LogContext["account_alias"])
	return integrationruntime.DoHTTP(ctx, integrationruntime.HTTPExecutorOptions[Response]{
		Provider:    providers.GooglePlaces,
		Subject:     subject,
		Method:      method,
		RequestPath: req.Path,
		Endpoint:    endpoint,
		MaxRetries:  c.cfg.MaxRetries,
		Client:      c.httpClient,
		BuildRequest: func(callCtx context.Context, callMethod string, callEndpoint string) (*http.Request, error) {
			return c.buildRequest(callCtx, callMethod, callEndpoint, req)
		},
		NormalizeResponse: normalizeResponse,
		StatusCode: func(resp Response) int {
			return resp.StatusCode
		},
		NormalizeHTTPError: func(statusCode int, headers http.Header, body string) error {
			return NormalizeHTTPError(statusCode, headers, body)
		},
		IsRetryableNetwork: isRetryableNetwork,
		IsRetryableHTTP: func(callMethod string, statusCode int, _ http.Header, _ string) bool {
			return isRetryableHTTP(callMethod, statusCode)
		},
		OnCacheHit: func(resp Response) {
			c.logEvent("cache_hit", map[string]any{
				"method": method,
				"path":   sanitizeURL(endpoint),
				"status": resp.StatusCode,
			})
		},
		OnRequest: func(attempt int) {
			c.logEvent("request", map[string]any{
				"method":     method,
				"path":       sanitizeURL(endpoint),
				"attempt":    attempt,
				"field_mask": RedactSensitive(strings.TrimSpace(req.FieldMask)),
			})
		},
		OnResponse: func(attempt int, resp Response, _ http.Header, duration time.Duration) {
			c.logEvent("response", map[string]any{
				"method":      method,
				"path":        sanitizeURL(endpoint),
				"attempt":     attempt,
				"status":      resp.StatusCode,
				"request_id":  resp.RequestID,
				"duration_ms": duration.Milliseconds(),
			})
		},
	})
}

func (c *Client) buildRequest(ctx context.Context, method string, endpoint string, req Request) (*http.Request, error) {
	bodyReader := io.Reader(nil)
	if strings.TrimSpace(req.RawBody) != "" {
		bodyReader = strings.NewReader(req.RawBody)
	} else if req.JSONBody != nil {
		raw, err := json.Marshal(req.JSONBody)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(raw)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("X-Goog-Api-Key", strings.TrimSpace(c.cfg.APIKey))
	spec := providers.Resolve(providers.GooglePlaces)
	accept := strings.TrimSpace(spec.Accept)
	if accept == "" {
		accept = "application/json"
	}
	httpReq.Header.Set("Accept", accept)
	httpReq.Header.Set("User-Agent", c.cfg.UserAgent)
	if fieldMask := strings.TrimSpace(req.FieldMask); fieldMask != "" {
		httpReq.Header.Set("X-Goog-FieldMask", fieldMask)
	}
	if bodyReader != nil {
		contentType := strings.TrimSpace(req.ContentType)
		if contentType == "" {
			contentType = "application/json"
		}
		httpReq.Header.Set("Content-Type", contentType)
	}
	for key, value := range req.Headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		httpReq.Header.Set(key, value)
	}
	return httpReq, nil
}

func normalizeResponse(httpResp *http.Response, body string) Response {
	out := Response{}
	if httpResp == nil {
		return out
	}
	out.StatusCode = httpResp.StatusCode
	out.Status = httpResp.Status
	out.Body = RedactSensitive(body)
	out.RequestID = strings.TrimSpace(httpResp.Header.Get("X-Request-Id"))
	if out.RequestID == "" {
		out.RequestID = strings.TrimSpace(httpResp.Header.Get("X-Google-Request-Id"))
	}
	if len(httpResp.Header) > 0 {
		headers := make([]string, 0, len(httpResp.Header))
		for key := range httpResp.Header {
			headers = append(headers, key)
		}
		sort.Strings(headers)
		out.Headers = make(map[string]string, len(headers))
		for _, key := range headers {
			out.Headers[key] = RedactSensitive(strings.Join(httpResp.Header.Values(key), ","))
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

func resolveURL(baseURL string, path string, params map[string]string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("request path is required")
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		u, err := url.Parse(path)
		if err != nil {
			return "", err
		}
		addQuery(u, params)
		return u.String(), nil
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	rel, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	u := base.ResolveReference(rel)
	addQuery(u, params)
	return u.String(), nil
}

func addQuery(u *url.URL, params map[string]string) {
	if u == nil || len(params) == 0 {
		return
	}
	q := u.Query()
	for key, value := range params {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		q.Set(key, strings.TrimSpace(value))
	}
	u.RawQuery = q.Encode()
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

func isRetryableNetwork(method string, _ error) bool {
	return isSafeMethod(method)
}

func isRetryableHTTP(method string, statusCode int) bool {
	if !isSafeMethod(method) {
		return false
	}
	switch statusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return statusCode >= 500
}

func isSafeMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func (c *Client) logEvent(kind string, fields map[string]any) {
	if c == nil || c.log == nil {
		return
	}
	event := map[string]any{
		"component": "googleplacesbridge",
		"event":     kind,
	}
	for key, value := range c.cfg.LogContext {
		event["ctx_"+key] = RedactSensitive(strings.TrimSpace(value))
	}
	for key, value := range fields {
		event[key] = value
	}
	c.log.Log(event)
}
