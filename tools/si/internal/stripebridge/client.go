package stripebridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	stripe "github.com/stripe/stripe-go/v83"
	"github.com/stripe/stripe-go/v83/rawrequest"
)

type rawRequester interface {
	RawRequest(method string, path string, content string, params *stripe.RawParams) (*stripe.APIResponse, error)
}

type Client struct {
	cfg ClientConfig
	raw rawRequester
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
	}, nil
}

func newClientWithRaw(cfg ClientConfig, raw rawRequester) (*Client, error) {
	if raw == nil {
		return nil, fmt.Errorf("raw requester is required")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("stripe api key is required")
	}
	return &Client{cfg: cfg, raw: raw}, nil
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
	rawParams := &stripe.RawParams{}
	if strings.TrimSpace(c.cfg.AccountID) != "" {
		rawParams.SetStripeAccount(strings.TrimSpace(c.cfg.AccountID))
	}
	if strings.TrimSpace(c.cfg.StripeContext) != "" {
		rawParams.StripeContext = strings.TrimSpace(c.cfg.StripeContext)
	}
	if strings.TrimSpace(req.IdempotencyKey) != "" {
		rawParams.SetIdempotencyKey(strings.TrimSpace(req.IdempotencyKey))
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
		return Response{}, ctx.Err()
	case pack := <-resultCh:
		if pack.err != nil {
			return Response{}, NormalizeAPIError(pack.err, "")
		}
		return normalizeResponse(pack.resp), nil
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
