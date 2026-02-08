package cloudflarebridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	cfg        ClientConfig
	httpClient *http.Client
	log        EventLogger
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
	return &Client{cfg: cfg, httpClient: client, log: cfg.Logger}, nil
}

func (c *Client) Do(ctx context.Context, req Request) (Response, error) {
	if c == nil {
		return Response{}, fmt.Errorf("cloudflare client is not initialized")
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	endpoint, err := resolveURL(c.cfg.BaseURL, req.Path, req.Params)
	if err != nil {
		return Response{}, err
	}
	attempts := c.cfg.MaxRetries + 1
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		httpReq, reqErr := c.buildRequest(ctx, method, endpoint, req)
		if reqErr != nil {
			return Response{}, reqErr
		}
		start := time.Now().UTC()
		c.logEvent("request", map[string]any{
			"method":  method,
			"path":    sanitizeURL(endpoint),
			"attempt": attempt,
		})
		httpResp, callErr := c.httpClient.Do(httpReq)
		if callErr != nil {
			lastErr = callErr
			if attempt < attempts && isRetryableNetwork(method, callErr) {
				time.Sleep(backoffDelay(attempt))
				continue
			}
			return Response{}, callErr
		}
		bodyBytes, _ := io.ReadAll(httpResp.Body)
		_ = httpResp.Body.Close()
		body := strings.TrimSpace(string(bodyBytes))
		resp := normalizeResponse(httpResp, body)
		c.logEvent("response", map[string]any{
			"method":      method,
			"path":        sanitizeURL(endpoint),
			"attempt":     attempt,
			"status":      resp.StatusCode,
			"request_id":  resp.RequestID,
			"duration_ms": time.Since(start).Milliseconds(),
		})
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && resp.Success {
			return resp, nil
		}
		apiErr := NormalizeHTTPError(resp.StatusCode, httpResp.Header, body)
		lastErr = apiErr
		if attempt < attempts && isRetryableHTTP(method, resp.StatusCode) {
			time.Sleep(backoffDelay(attempt))
			continue
		}
		return Response{}, apiErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("cloudflare request failed")
	}
	return Response{}, lastErr
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
	httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.cfg.APIToken))
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", c.cfg.UserAgent)
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
	out.Success = out.StatusCode >= 200 && out.StatusCode < 300
	out.RequestID = strings.TrimSpace(httpResp.Header.Get("CF-Ray"))
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

func backoffDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := 300 * time.Millisecond
	d := base * time.Duration(1<<(attempt-1))
	if d > 3*time.Second {
		return 3 * time.Second
	}
	return d
}

func (c *Client) logEvent(kind string, fields map[string]any) {
	if c == nil || c.log == nil {
		return
	}
	event := map[string]any{
		"component": "cloudflarebridge",
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
