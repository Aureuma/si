package apibridge

import (
	"context"
	"net/http"
	"time"
)

type EventLogger interface {
	Log(event map[string]any)
}

type Request struct {
	Method      string
	BaseURL     string            // Optional per-request override.
	URL         string            // Optional fully-qualified URL; if set, Path/BaseURL are ignored.
	Path        string            // API path (e.g. /v1/items). Resolved against BaseURL.
	Params      map[string]string // Query params added to URL.
	Headers     map[string]string
	RawBody     string
	JSONBody    any
	ContentType string

	// Optional hook invoked after the request is constructed and before sending.
	// Intended for per-attempt auth (e.g. fetching a short-lived token).
	Prepare func(ctx context.Context, attempt int, httpReq *http.Request) error

	// Optional structured fields added to request/response log events.
	LogFields map[string]any
}

type Response struct {
	StatusCode int
	Status     string
	RequestID  string
	Headers    http.Header
	Body       []byte
	Duration   time.Duration
}

type RetryDecision struct {
	Retry bool
	Wait  time.Duration
}

type RetryDecider func(ctx context.Context, attempt int, req Request, resp *http.Response, body []byte, callErr error) RetryDecision

type Config struct {
	BaseURL    string
	UserAgent  string
	Timeout    time.Duration
	MaxRetries int

	Logger     EventLogger
	LogContext map[string]string

	HTTPClient *http.Client

	// SanitizeURL is used for log output only (never changes the actual request).
	// If nil, the engine strips the query string.
	SanitizeURL func(raw string) string

	// RequestIDFromHeaders extracts a request correlation identifier from response headers.
	RequestIDFromHeaders func(h http.Header) string

	// RetryDecider determines whether to retry and how long to wait between attempts.
	// If nil, DefaultRetryDecider is used.
	RetryDecider RetryDecider
}

