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
	if cfg.Provider == nil {
		return nil, fmt.Errorf("github token provider is required")
	}
	spec := providers.Resolve(providers.GitHub)
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
	subject := strings.TrimSpace(c.cfg.LogContext["account_alias"])
	return integrationruntime.DoHTTP(ctx, integrationruntime.HTTPExecutorOptions[Response]{
		Provider:    providers.GitHub,
		Subject:     subject,
		Method:      method,
		RequestPath: req.Path,
		Endpoint:    u,
		MaxRetries:  c.cfg.MaxRetries,
		Client:      c.httpClient,
		BuildRequest: func(callCtx context.Context, callMethod string, endpoint string) (*http.Request, error) {
			token, tokenErr := c.cfg.Provider.Token(callCtx, TokenRequest{
				Owner:          req.Owner,
				Repo:           req.Repo,
				InstallationID: req.InstallationID,
			})
			if tokenErr != nil {
				return nil, tokenErr
			}
			return c.buildRequest(callCtx, callMethod, endpoint, token, req)
		},
		NormalizeResponse: normalizeResponse,
		StatusCode: func(resp Response) int {
			return resp.StatusCode
		},
		NormalizeHTTPError: func(statusCode int, headers http.Header, body string) error {
			return NormalizeHTTPError(statusCode, headers, body)
		},
		IsRetryableNetwork: isRetryableNetwork,
		IsRetryableHTTP: func(callMethod string, statusCode int, headers http.Header, body string) bool {
			return isRetryableHTTP(callMethod, statusCode, headers, body)
		},
		OnCacheHit: func(resp Response) {
			c.logEvent("cache_hit", map[string]any{
				"method": method,
				"path":   sanitizeURL(u),
				"status": resp.StatusCode,
			})
		},
		OnRequest: func(attempt int) {
			c.logEvent("request", map[string]any{
				"method":    method,
				"path":      sanitizeURL(u),
				"attempt":   attempt,
				"auth_mode": string(c.cfg.Provider.Mode()),
			})
		},
		OnResponse: func(attempt int, response Response, _ http.Header, duration time.Duration) {
			c.logEvent("response", map[string]any{
				"method":      method,
				"path":        sanitizeURL(u),
				"attempt":     attempt,
				"status":      response.StatusCode,
				"request_id":  response.RequestID,
				"duration_ms": duration.Milliseconds(),
			})
		},
	})
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
	spec := providers.Resolve(providers.GitHub)
	accept := strings.TrimSpace(spec.Accept)
	if accept == "" {
		accept = "application/vnd.github+json"
	}
	httpReq.Header.Set("Accept", accept)
	if value := strings.TrimSpace(spec.DefaultHeaders["X-GitHub-Api-Version"]); value != "" {
		httpReq.Header.Set("X-GitHub-Api-Version", value)
	} else {
		httpReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
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
