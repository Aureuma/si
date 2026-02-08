package apibridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type Client struct {
	cfg        Config
	httpClient *http.Client
}

func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("base url is required")
	}
	if strings.TrimSpace(cfg.Component) == "" {
		cfg.Component = "apibridge"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.SanitizeURL == nil {
		cfg.SanitizeURL = StripQuery
	}
	if cfg.Redact == nil {
		cfg.Redact = func(value string) string { return value }
	}
	if cfg.RetryDecider == nil {
		cfg.RetryDecider = DefaultRetryDecider
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	return &Client{cfg: cfg, httpClient: client}, nil
}

func (c *Client) Do(ctx context.Context, req Request) (Response, error) {
	if c == nil || c.httpClient == nil {
		return Response{}, fmt.Errorf("client is not initialized")
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	endpoint := strings.TrimSpace(req.URL)
	if endpoint == "" {
		base := strings.TrimSpace(req.BaseURL)
		if base == "" {
			base = c.cfg.BaseURL
		}
		u, err := ResolveURL(base, req.Path, req.Params)
		if err != nil {
			return Response{}, err
		}
		endpoint = u
	}

	attempts := c.cfg.MaxRetries + 1
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		httpReq, err := c.buildRequest(ctx, method, endpoint, req)
		if err != nil {
			return Response{}, err
		}
		if req.Prepare != nil {
			if err := req.Prepare(ctx, attempt, httpReq); err != nil {
				return Response{}, err
			}
		}
		start := time.Now().UTC()
		c.logEvent("request", req, map[string]any{
			"method":  method,
			"url":     c.cfg.SanitizeURL(endpoint),
			"attempt": attempt,
		})

		httpResp, callErr := c.httpClient.Do(httpReq)
		if callErr != nil {
			lastErr = callErr
			dec := c.cfg.RetryDecider(ctx, attempt, withMethod(req, method), nil, nil, callErr)
			if attempt < attempts && dec.Retry {
				sleep(dec.Wait)
				continue
			}
			c.logEvent("error", req, map[string]any{
				"method":      method,
				"url":         c.cfg.SanitizeURL(endpoint),
				"attempt":     attempt,
				"duration_ms": time.Since(start).Milliseconds(),
				"error":       callErr.Error(),
			})
			return Response{}, callErr
		}

		bodyBytes, _ := io.ReadAll(httpResp.Body)
		_ = httpResp.Body.Close()

		resp := Response{
			StatusCode: httpResp.StatusCode,
			Status:     httpResp.Status,
			Headers:    cloneHeader(httpResp.Header),
			Body:       bodyBytes,
			Duration:   time.Since(start),
		}
		if c.cfg.RequestIDFromHeaders != nil {
			resp.RequestID = strings.TrimSpace(c.cfg.RequestIDFromHeaders(resp.Headers))
		}

		c.logEvent("response", req, map[string]any{
			"method":      method,
			"url":         c.cfg.SanitizeURL(endpoint),
			"attempt":     attempt,
			"status":      resp.StatusCode,
			"request_id":  resp.RequestID,
			"duration_ms": resp.Duration.Milliseconds(),
		})

		dec := c.cfg.RetryDecider(ctx, attempt, withMethod(req, method), httpResp, bodyBytes, nil)
		if attempt < attempts && dec.Retry {
			sleep(dec.Wait)
			continue
		}
		return resp, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("request failed")
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
	if ua := strings.TrimSpace(c.cfg.UserAgent); ua != "" {
		httpReq.Header.Set("User-Agent", ua)
	}
	for key, value := range req.Headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		httpReq.Header.Set(key, value)
	}
	if bodyReader != nil {
		contentType := strings.TrimSpace(req.ContentType)
		if contentType == "" {
			contentType = "application/json"
		}
		httpReq.Header.Set("Content-Type", contentType)
	}
	return httpReq, nil
}

func (c *Client) logEvent(kind string, req Request, fields map[string]any) {
	if c == nil || c.cfg.Logger == nil {
		return
	}
	event := map[string]any{
		"component": c.cfg.Component,
		"event":     kind,
	}
	if len(c.cfg.LogContext) > 0 {
		keys := make([]string, 0, len(c.cfg.LogContext))
		for k := range c.cfg.LogContext {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			event["ctx_"+k] = c.cfg.Redact(c.cfg.LogContext[k])
		}
	}
	if len(req.LogFields) > 0 {
		for k, v := range req.LogFields {
			event[k] = v
		}
	}
	for k, v := range fields {
		event[k] = v
	}
	c.cfg.Logger.Log(event)
}

func cloneHeader(h http.Header) http.Header {
	if len(h) == 0 {
		return http.Header{}
	}
	out := make(http.Header, len(h))
	for k, vv := range h {
		cp := make([]string, 0, len(vv))
		cp = append(cp, vv...)
		out[k] = cp
	}
	return out
}

func withMethod(req Request, method string) Request {
	req.Method = method
	return req
}

func sleep(d time.Duration) {
	if d <= 0 {
		return
	}
	time.Sleep(d)
}
