package githubbridge

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
	return &Client{cfg: cfg, httpClient: client, log: cfg.Logger}, nil
}

func (c *Client) Do(ctx context.Context, req Request) (Response, error) {
	if c == nil || c.cfg.Provider == nil {
		return Response{}, fmt.Errorf("github client is not initialized")
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	u, err := resolveURL(c.cfg.BaseURL, req.Path, req.Params)
	if err != nil {
		return Response{}, err
	}
	attempts := c.cfg.MaxRetries + 1
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		token, tokenErr := c.cfg.Provider.Token(ctx, TokenRequest{Owner: req.Owner, Repo: req.Repo, InstallationID: req.InstallationID})
		if tokenErr != nil {
			return Response{}, tokenErr
		}
		httpReq, reqErr := c.buildRequest(ctx, method, u, token, req)
		if reqErr != nil {
			return Response{}, reqErr
		}
		start := time.Now().UTC()
		c.logEvent("request", map[string]any{
			"method":    method,
			"path":      sanitizeURL(u),
			"attempt":   attempt,
			"auth_mode": string(c.cfg.Provider.Mode()),
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
		response := normalizeResponse(httpResp, body)
		c.logEvent("response", map[string]any{
			"method":      method,
			"path":        sanitizeURL(u),
			"attempt":     attempt,
			"status":      response.StatusCode,
			"request_id":  response.RequestID,
			"duration_ms": time.Since(start).Milliseconds(),
		})
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return response, nil
		}
		apiErr := NormalizeHTTPError(response.StatusCode, httpResp.Header, body)
		lastErr = apiErr
		if attempt < attempts && isRetryableHTTP(method, response.StatusCode, httpResp.Header, body) {
			time.Sleep(backoffDelay(attempt))
			continue
		}
		return Response{}, apiErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("github request failed")
	}
	return Response{}, lastErr
}

func (c *Client) buildRequest(ctx context.Context, method string, endpoint string, token Token, req Request) (*http.Request, error) {
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
	httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token.Value))
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	httpReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")
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
	out.RequestID = strings.TrimSpace(httpResp.Header.Get("X-GitHub-Request-Id"))
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

func isRetryableNetwork(method string, _ error) bool {
	return isSafeMethod(method)
}

func isRetryableHTTP(method string, statusCode int, headers http.Header, body string) bool {
	if !isSafeMethod(method) {
		return false
	}
	if statusCode == http.StatusTooManyRequests || statusCode == http.StatusBadGateway || statusCode == http.StatusServiceUnavailable || statusCode == http.StatusGatewayTimeout {
		return true
	}
	if statusCode == http.StatusForbidden {
		if headers != nil && strings.TrimSpace(headers.Get("X-RateLimit-Remaining")) == "0" {
			return true
		}
		lower := strings.ToLower(body)
		if strings.Contains(lower, "secondary rate limit") || strings.Contains(lower, "abuse") {
			return true
		}
	}
	if statusCode >= 500 {
		return true
	}
	return false
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
		"component": "githubbridge",
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
