package appstorebridge

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
	if cfg.TokenProvider == nil {
		return nil, fmt.Errorf("apple appstore token provider is required")
	}
	spec := providers.Resolve(providers.AppleAppStore)
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
		return Response{}, fmt.Errorf("apple appstore client is not initialized")
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	endpoint, err := c.resolveRequestURL(req)
	if err != nil {
		return Response{}, err
	}
	subject := strings.TrimSpace(c.cfg.LogContext["account_alias"])
	return integrationruntime.DoHTTP(ctx, integrationruntime.HTTPExecutorOptions[Response]{
		Provider:     providers.AppleAppStore,
		Subject:      subject,
		Method:       method,
		RequestPath:  req.Path,
		Endpoint:     endpoint,
		MaxRetries:   c.cfg.MaxRetries,
		Client:       c.httpClient,
		DisableCache: c.cfg.DisableCache,
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
				"method":  method,
				"path":    sanitizeURL(endpoint),
				"attempt": attempt,
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

func (c *Client) resolveRequestURL(req Request) (string, error) {
	params := cloneParams(req.Params)
	return resolveURL(c.cfg.BaseURL, req.Path, params)
}

func (c *Client) buildRequest(ctx context.Context, method, endpoint string, req Request) (*http.Request, error) {
	var bodyReader *bytes.Reader
	if strings.TrimSpace(req.RawBody) != "" {
		bodyReader = bytes.NewReader([]byte(req.RawBody))
	} else if req.JSONBody != nil {
		raw, err := json.Marshal(req.JSONBody)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(raw)
	}
	var body io.Reader
	if bodyReader != nil {
		body = bodyReader
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	spec := providers.Resolve(providers.AppleAppStore)
	accept := strings.TrimSpace(spec.Accept)
	if accept == "" {
		accept = "application/json"
	}
	httpReq.Header.Set("Accept", accept)
	httpReq.Header.Set("User-Agent", c.cfg.UserAgent)
	tok, err := c.cfg.TokenProvider.Token(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(tok.Value) == "" {
		return nil, fmt.Errorf("apple appstore token provider returned empty token")
	}
	httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(tok.Value))
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
	out.RequestID = strings.TrimSpace(httpResp.Header.Get("x-request-id"))
	if out.RequestID == "" {
		out.RequestID = strings.TrimSpace(httpResp.Header.Get("X-Request-ID"))
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
	switch typed := parsed.(type) {
	case []any:
		out.List = anySliceToMaps(typed)
		return out
	case map[string]any:
		out.Data = typed
		if payload, ok := typed["data"].([]any); ok {
			out.List = anySliceToMaps(payload)
		}
		return out
	default:
		return out
	}
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
		"component": "appstorebridge",
		"event":     kind,
	}
	for key, value := range c.cfg.LogContext {
		event["ctx_"+key] = RedactSensitive(strings.TrimSpace(value))
	}
	if c.cfg.TokenProvider != nil {
		event["auth_source"] = c.cfg.TokenProvider.Source()
	}
	for key, value := range fields {
		event[key] = value
	}
	c.log.Log(event)
}
