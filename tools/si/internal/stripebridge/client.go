package stripebridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	stripe "github.com/stripe/stripe-go/v83"
	"github.com/stripe/stripe-go/v83/rawrequest"

	"si/tools/si/internal/providers"
)

type rawRequester interface {
	RawRequest(method string, path string, content string, params *stripe.RawParams) (*stripe.APIResponse, error)
}

type Client struct {
	cfg ClientConfig
	raw rawRequester
	log EventLogger
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
	if cfg.BaseURL == "" {
		cfg.BaseURL = stripe.APIURL
	}
	if cfg.Logger == nil && strings.TrimSpace(cfg.LogPath) != "" {
		cfg.Logger = NewJSONLLogger(strings.TrimSpace(cfg.LogPath))
	}
	backendCfg := &stripe.BackendConfig{
		HTTPClient:        &http.Client{Timeout: cfg.Timeout},
		MaxNetworkRetries: stripe.Int64(cfg.MaxNetworkRetries),
		URL:               stripe.String(cfg.BaseURL),
	}
	if strings.TrimSpace(cfg.StripeContext) != "" {
		backendCfg.StripeContext = stripe.String(strings.TrimSpace(cfg.StripeContext))
	}
	backend := stripe.GetBackendWithConfig(stripe.APIBackend, backendCfg)
	rawBackend, ok := backend.(stripe.RawRequestBackend)
	if !ok {
		return nil, fmt.Errorf("stripe backend does not support raw requests")
	}
	return &Client{
		cfg: cfg,
		raw: rawrequest.Client{B: rawBackend, Key: strings.TrimSpace(cfg.APIKey)},
		log: cfg.Logger,
	}, nil
}

func newClientWithRaw(cfg ClientConfig, raw rawRequester) (*Client, error) {
	if raw == nil {
		return nil, fmt.Errorf("raw requester is required")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("stripe api key is required")
	}
	return &Client{cfg: cfg, raw: raw, log: cfg.Logger}, nil
}

func (c *Client) Do(ctx context.Context, req Request) (Response, error) {
	if c == nil || c.raw == nil {
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
	rawParams := &stripe.RawParams{}
	if strings.TrimSpace(c.cfg.AccountID) != "" {
		rawParams.SetStripeAccount(strings.TrimSpace(c.cfg.AccountID))
	}
	if strings.TrimSpace(c.cfg.StripeContext) != "" {
		rawParams.StripeContext = strings.TrimSpace(c.cfg.StripeContext)
	}
	if idempotencyKey != "" {
		rawParams.SetIdempotencyKey(idempotencyKey)
	}

	type responsePack struct {
		resp *stripe.APIResponse
		err  error
	}
	resultCh := make(chan responsePack, 1)
	go func() {
		resp, callErr := c.raw.RawRequest(method, reqPath, content, rawParams)
		resultCh <- responsePack{resp: resp, err: callErr}
	}()

	select {
	case <-ctx.Done():
		c.logEvent("error", map[string]any{
			"method":      method,
			"path":        reqPath,
			"duration_ms": time.Since(start).Milliseconds(),
			"error":       RedactSensitive(ctx.Err().Error()),
		})
		return Response{}, ctx.Err()
	case pack := <-resultCh:
		duration := time.Since(start)
		if pack.err != nil {
			apiErr := NormalizeAPIError(pack.err, "")
			if apiErr != nil && apiErr.StatusCode > 0 {
				providers.FeedbackWithLatency(providers.Stripe, subject, apiErr.StatusCode, nil, duration)
			}
			fields := map[string]any{
				"method":      method,
				"path":        reqPath,
				"duration_ms": duration.Milliseconds(),
				"error":       RedactSensitive(apiErr.Error()),
			}
			if apiErr != nil {
				fields["status"] = apiErr.StatusCode
				fields["type"] = apiErr.Type
				fields["code"] = apiErr.Code
				fields["request_id"] = apiErr.RequestID
			}
			c.logEvent("error", fields)
			return Response{}, apiErr
		}
		resp := normalizeResponse(pack.resp)
		var feedbackHeaders http.Header
		if pack.resp != nil {
			feedbackHeaders = pack.resp.Header
		}
		providers.FeedbackWithLatency(providers.Stripe, subject, resp.StatusCode, feedbackHeaders, duration)
		c.logEvent("response", map[string]any{
			"method":      method,
			"path":        reqPath,
			"duration_ms": duration.Milliseconds(),
			"status":      resp.StatusCode,
			"request_id":  resp.RequestID,
		})
		return resp, nil
	}
}

func normalizeResponse(resp *stripe.APIResponse) Response {
	out := Response{}
	if resp == nil {
		return out
	}
	out.StatusCode = resp.StatusCode
	out.Status = resp.Status
	out.RequestID = strings.TrimSpace(resp.RequestID)
	out.IdempotencyKey = strings.TrimSpace(resp.IdempotencyKey)
	out.Body = RedactSensitive(strings.TrimSpace(string(resp.RawJSON)))
	if len(resp.Header) > 0 {
		headers := make([]string, 0, len(resp.Header))
		for key := range resp.Header {
			headers = append(headers, key)
		}
		sort.Strings(headers)
		out.Headers = make(map[string]string, len(headers))
		for _, key := range headers {
			val := strings.Join(resp.Header.Values(key), ",")
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
