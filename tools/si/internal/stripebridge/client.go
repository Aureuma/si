package stripebridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"si/tools/si/internal/providers"
)

type Client struct {
	cfg        ClientConfig
	httpClient *http.Client
	log        EventLogger
}

func NewClient(cfg ClientConfig) (*Client, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("stripe api key is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxNetworkRetries < 0 {
		cfg.MaxNetworkRetries = 0
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = providers.Resolve(providers.Stripe).BaseURL
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
	if c == nil || c.httpClient == nil {
		return Response{}, fmt.Errorf("stripe client is not initialized")
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		return Response{}, fmt.Errorf("request path is required")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	content, reqPath, err := buildRequestContent(path, method, req.Params, req.RawBody)
	if err != nil {
		return Response{}, err
	}
	endpoint, err := resolveURL(c.cfg.BaseURL, reqPath)
	if err != nil {
		return Response{}, err
	}
	subject := strings.TrimSpace(c.cfg.LogContext["account_alias"])
	start := time.Now().UTC()
	release, err := providers.Acquire(ctx, providers.Stripe, subject, method, path)
	if err != nil {
		return Response{}, err
	}
	defer release()
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	idempotencyDefaulted := false
	if idempotencyKey == "" && method == http.MethodPost {
		idempotencyKey = defaultIdempotencyKey(reqPath, content)
		idempotencyDefaulted = true
	}
	c.logEvent("request", map[string]any{
		"method":           method,
		"path":             reqPath,
		"idempotency_key":  RedactSensitive(idempotencyKey),
		"idempotency_auto": idempotencyDefaulted,
		"params_count":     len(req.Params),
		"body_bytes":       len(content),
	})
	maxAttempts := int(c.cfg.MaxNetworkRetries) + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		httpReq, reqErr := c.buildRequest(ctx, method, endpoint, content, req.RawBody, idempotencyKey)
		if reqErr != nil {
			return Response{}, reqErr
		}
		attemptStart := time.Now().UTC()
		httpResp, callErr := c.httpClient.Do(httpReq)
		if callErr != nil {
			lastErr = callErr
			if attempt < maxAttempts && isRetryableNetwork(method, idempotencyKey, callErr) {
				time.Sleep(retryDelay(attempt, nil))
				continue
			}
			c.logEvent("error", map[string]any{
				"method":      method,
				"path":        reqPath,
				"duration_ms": time.Since(attemptStart).Milliseconds(),
				"error":       RedactSensitive(callErr.Error()),
			})
			return Response{}, NormalizeAPIError(callErr, "")
		}
		bodyBytes, _ := io.ReadAll(httpResp.Body)
		_ = httpResp.Body.Close()
		body := strings.TrimSpace(string(bodyBytes))
		resp := normalizeResponse(httpResp, body)
		duration := time.Since(attemptStart)
		providers.FeedbackWithLatency(providers.Stripe, subject, resp.StatusCode, httpResp.Header, duration)
		c.logEvent("response", map[string]any{
			"method":      method,
			"path":        reqPath,
			"attempt":     attempt,
			"duration_ms": duration.Milliseconds(),
			"status":      resp.StatusCode,
			"request_id":  resp.RequestID,
		})
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		apiErr := NormalizeHTTPError(resp.StatusCode, httpResp.Header, body)
		lastErr = apiErr
		c.logEvent("error", map[string]any{
			"method":      method,
			"path":        reqPath,
			"attempt":     attempt,
			"duration_ms": duration.Milliseconds(),
			"status":      apiErr.StatusCode,
			"type":        apiErr.Type,
			"code":        apiErr.Code,
			"request_id":  apiErr.RequestID,
			"error":       RedactSensitive(apiErr.Error()),
		})
		if attempt < maxAttempts && isRetryableHTTP(method, idempotencyKey, resp.StatusCode) {
			time.Sleep(retryDelay(attempt, httpResp.Header))
			continue
		}
		return Response{}, apiErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("stripe request failed")
	}
	c.logEvent("error", map[string]any{
		"method":      method,
		"path":        reqPath,
		"duration_ms": time.Since(start).Milliseconds(),
		"error":       RedactSensitive(lastErr.Error()),
	})
	return Response{}, NormalizeAPIError(lastErr, "")
}

func (c *Client) buildRequest(ctx context.Context, method string, endpoint string, content string, rawBody string, idempotencyKey string) (*http.Request, error) {
	bodyReader := io.Reader(nil)
	if strings.TrimSpace(content) != "" {
		bodyReader = strings.NewReader(content)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return nil, err
	}
	spec := providers.Resolve(providers.Stripe)
	httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.cfg.APIKey))
	httpReq.Header.Set("Accept", firstNonEmpty(strings.TrimSpace(spec.Accept), "application/json"))
	httpReq.Header.Set("User-Agent", firstNonEmpty(strings.TrimSpace(spec.UserAgent), "si-stripe/1.0"))
	if strings.TrimSpace(c.cfg.AccountID) != "" {
		httpReq.Header.Set("Stripe-Account", strings.TrimSpace(c.cfg.AccountID))
	}
	if strings.TrimSpace(c.cfg.StripeContext) != "" {
		httpReq.Header.Set("Stripe-Context", strings.TrimSpace(c.cfg.StripeContext))
	}
	if strings.TrimSpace(idempotencyKey) != "" {
		httpReq.Header.Set("Idempotency-Key", strings.TrimSpace(idempotencyKey))
	}
	if strings.TrimSpace(content) != "" {
		contentType := detectStripeContentType(strings.TrimSpace(httpReq.URL.Path), rawBody)
		httpReq.Header.Set("Content-Type", contentType)
	}
	return httpReq, nil
}

func detectStripeContentType(path string, rawBody string) string {
	if strings.HasPrefix(strings.TrimSpace(path), "/v2/") {
		return "application/json"
	}
	rawBody = strings.TrimSpace(rawBody)
	if strings.HasPrefix(rawBody, "{") || strings.HasPrefix(rawBody, "[") {
		return "application/json"
	}
	return "application/x-www-form-urlencoded"
}

func resolveURL(baseURL string, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("request path is required")
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		u, err := url.Parse(path)
		if err != nil {
			return "", err
		}
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
	return base.ResolveReference(rel).String(), nil
}

func normalizeResponse(httpResp *http.Response, body string) Response {
	out := Response{}
	if httpResp == nil {
		return out
	}
	out.StatusCode = httpResp.StatusCode
	out.Status = strings.TrimSpace(httpResp.Status)
	out.RequestID = strings.TrimSpace(httpResp.Header.Get("Request-Id"))
	out.IdempotencyKey = strings.TrimSpace(httpResp.Header.Get("Idempotency-Key"))
	out.Body = RedactSensitive(strings.TrimSpace(body))
	if len(httpResp.Header) > 0 {
		headers := make([]string, 0, len(httpResp.Header))
		for key := range httpResp.Header {
			headers = append(headers, key)
		}
		sort.Strings(headers)
		out.Headers = make(map[string]string, len(headers))
		for _, key := range headers {
			val := strings.Join(httpResp.Header.Values(key), ",")
			out.Headers[key] = RedactSensitive(val)
		}
	}
	if out.Body != "" {
		var parsed map[string]any
		if json.Unmarshal([]byte(out.Body), &parsed) == nil {
			out.Data = parsed
		}
	}
	return out
}

func isRetryableNetwork(method string, idempotencyKey string, _ error) bool {
	return canRetryMethod(method, idempotencyKey)
}

func isRetryableHTTP(method string, idempotencyKey string, statusCode int) bool {
	if !canRetryMethod(method, idempotencyKey) {
		return false
	}
	switch statusCode {
	case http.StatusConflict, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return statusCode >= 500
}

func canRetryMethod(method string, idempotencyKey string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodDelete:
		return true
	case http.MethodPost:
		return strings.TrimSpace(idempotencyKey) != ""
	default:
		return false
	}
}

func retryDelay(attempt int, headers http.Header) time.Duration {
	if seconds, ok := retryAfterSeconds(headers); ok {
		return time.Duration(seconds * float64(time.Second))
	}
	if attempt < 1 {
		attempt = 1
	}
	base := 500 * time.Millisecond
	delay := base * time.Duration(1<<(attempt-1))
	if delay > 5*time.Second {
		return 5 * time.Second
	}
	return delay
}

func retryAfterSeconds(headers http.Header) (float64, bool) {
	if headers == nil {
		return 0, false
	}
	raw := strings.TrimSpace(headers.Get("Retry-After"))
	if raw == "" {
		return 0, false
	}
	if asInt, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if asInt < 0 {
			return 0, false
		}
		return float64(asInt), true
	}
	if when, err := http.ParseTime(raw); err == nil {
		seconds := time.Until(when).Seconds()
		if seconds < 0 {
			return 0, true
		}
		return seconds, true
	}
	return 0, false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func buildRequestContent(path string, method string, params map[string]string, rawBody string) (content string, reqPath string, err error) {
	reqPath = path
	if strings.TrimSpace(rawBody) != "" {
		return rawBody, reqPath, nil
	}
	values := url.Values{}
	for key, value := range params {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		values.Add(key, value)
	}
	if len(values) == 0 {
		return "", reqPath, nil
	}
	if method == http.MethodGet || method == http.MethodDelete {
		delimiter := "?"
		if strings.Contains(reqPath, "?") {
			delimiter = "&"
		}
		reqPath = reqPath + delimiter + values.Encode()
		return "", reqPath, nil
	}
	if strings.HasPrefix(reqPath, "/v2/") {
		obj := map[string]string{}
		for key, vals := range values {
			if len(vals) == 0 {
				obj[key] = ""
				continue
			}
			obj[key] = vals[len(vals)-1]
		}
		body, marshalErr := json.Marshal(obj)
		if marshalErr != nil {
			return "", "", marshalErr
		}
		return string(body), reqPath, nil
	}
	return values.Encode(), reqPath, nil
}

func defaultIdempotencyKey(path string, content string) string {
	seed := fmt.Sprintf("%d|%s|%s", time.Now().UTC().UnixNano(), strings.TrimSpace(path), strings.TrimSpace(content))
	sum := sha256.Sum256([]byte(seed))
	return "si_" + hex.EncodeToString(sum[:16])
}

func (c *Client) logEvent(kind string, fields map[string]any) {
	if c == nil || c.log == nil {
		return
	}
	event := map[string]any{
		"component": "stripebridge",
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
