package integrationruntime

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"si/tools/si/internal/netpolicy"
	"si/tools/si/internal/providers"
)

type HTTPExecutorOptions[R any] struct {
	Provider    providers.ID
	Subject     string
	Method      string
	RequestPath string
	Endpoint    string
	MaxRetries  int
	Client      *http.Client

	BuildRequest       func(ctx context.Context, method string, endpoint string) (*http.Request, error)
	NormalizeResponse  func(httpResp *http.Response, body string) R
	StatusCode         func(resp R) int
	IsSuccess          func(resp R) bool
	NormalizeHTTPError func(statusCode int, headers http.Header, body string) error

	IsRetryableNetwork func(method string, callErr error) bool
	IsRetryableHTTP    func(method string, statusCode int, headers http.Header, body string) bool

	OnCacheHit func(resp R)
	OnRequest  func(attempt int)
	OnResponse func(attempt int, resp R, headers http.Header, duration time.Duration)
	OnError    func(attempt int, callErr error, duration time.Duration)

	DisableCache bool
}

func DoHTTP[R any](ctx context.Context, opts HTTPExecutorOptions[R]) (R, error) {
	var zero R
	if opts.Client == nil {
		return zero, fmt.Errorf("http client is required")
	}
	if opts.BuildRequest == nil {
		return zero, fmt.Errorf("build request hook is required")
	}
	if opts.NormalizeResponse == nil {
		return zero, fmt.Errorf("normalize response hook is required")
	}
	if opts.StatusCode == nil {
		return zero, fmt.Errorf("status code hook is required")
	}

	method := strings.ToUpper(strings.TrimSpace(opts.Method))
	if method == "" {
		method = http.MethodGet
	}
	endpoint := strings.TrimSpace(opts.Endpoint)
	if endpoint == "" {
		return zero, fmt.Errorf("endpoint is required")
	}

	cacheEnabled := !opts.DisableCache
	if cacheEnabled {
		if code, status, headers, body, ok := providers.CacheLookup(opts.Provider, opts.Subject, method, endpoint); ok {
			cachedResp := opts.NormalizeResponse(&http.Response{
				StatusCode: code,
				Status:     status,
				Header:     headers,
			}, body)
			if opts.OnCacheHit != nil {
				opts.OnCacheHit(cachedResp)
			}
			return cachedResp, nil
		}
	}

	attempts := opts.MaxRetries + 1
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		release, err := providers.Acquire(ctx, opts.Provider, opts.Subject, method, opts.RequestPath)
		if err != nil {
			return zero, err
		}
		req, err := opts.BuildRequest(ctx, method, endpoint)
		if err != nil {
			release()
			return zero, err
		}

		if opts.OnRequest != nil {
			opts.OnRequest(attempt)
		}
		start := time.Now().UTC()
		httpResp, callErr := opts.Client.Do(req)
		if callErr != nil {
			release()
			lastErr = callErr
			if opts.OnError != nil {
				opts.OnError(attempt, callErr, time.Since(start))
			}
			if attempt < attempts && opts.IsRetryableNetwork != nil && opts.IsRetryableNetwork(method, callErr) {
				if sleepErr := netpolicy.SleepForRetry(ctx, attempt, nil); sleepErr != nil {
					return zero, sleepErr
				}
				continue
			}
			return zero, callErr
		}

		bodyBytes, _ := io.ReadAll(httpResp.Body)
		_ = httpResp.Body.Close()
		body := strings.TrimSpace(string(bodyBytes))
		resp := opts.NormalizeResponse(httpResp, body)
		duration := time.Since(start)
		statusCode := opts.StatusCode(resp)
		providers.FeedbackWithLatency(opts.Provider, opts.Subject, statusCode, httpResp.Header, duration)
		if opts.OnResponse != nil {
			opts.OnResponse(attempt, resp, httpResp.Header, duration)
		}

		success := statusCode >= 200 && statusCode < 300
		if opts.IsSuccess != nil {
			success = opts.IsSuccess(resp)
		}
		if success {
			if cacheEnabled {
				providers.CacheStore(opts.Provider, opts.Subject, method, endpoint, statusCode, strings.TrimSpace(httpResp.Status), httpResp.Header, body)
				if !netpolicy.IsSafeMethod(method) {
					providers.CacheInvalidate(opts.Provider, opts.Subject)
				}
			}
			release()
			return resp, nil
		}

		apiErr := fmt.Errorf("request failed: status=%d", statusCode)
		if opts.NormalizeHTTPError != nil {
			apiErr = opts.NormalizeHTTPError(statusCode, httpResp.Header, body)
		}
		lastErr = apiErr
		if attempt < attempts && opts.IsRetryableHTTP != nil && opts.IsRetryableHTTP(method, statusCode, httpResp.Header, body) {
			release()
			if sleepErr := netpolicy.SleepForRetry(ctx, attempt, httpResp.Header); sleepErr != nil {
				return zero, sleepErr
			}
			continue
		}
		release()
		return zero, apiErr
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("request failed")
	}
	return zero, lastErr
}
