package stripebridge

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type Environment string

const (
	EnvLive    Environment = "live"
	EnvSandbox Environment = "sandbox"
)

func ParseEnvironment(raw string) (Environment, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	switch value {
	case "live":
		return EnvLive, nil
	case "sandbox":
		return EnvSandbox, nil
	case "test":
		return "", errors.New("environment `test` is not supported; use `sandbox`")
	case "":
		return "", errors.New("environment required (live|sandbox)")
	default:
		return "", fmt.Errorf("invalid environment %q (expected live|sandbox)", raw)
	}
}

type Context struct {
	AccountAlias string
	AccountID    string
	Environment  Environment
}

type ClientConfig struct {
	APIKey            string
	AccountID         string
	StripeContext     string
	BaseURL           string
	Timeout           time.Duration
	MaxNetworkRetries int64
	LogPath           string
	LogContext        map[string]string
	Logger            EventLogger
}

type Request struct {
	Method         string
	Path           string
	Params         map[string]string
	RawBody        string
	IdempotencyKey string
}

type Response struct {
	StatusCode     int               `json:"status_code"`
	Status         string            `json:"status"`
	RequestID      string            `json:"request_id,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Body           string            `json:"body,omitempty"`
	Data           map[string]any    `json:"data,omitempty"`
}

type APIErrorDetails struct {
	StatusCode    int    `json:"status_code,omitempty"`
	Type          string `json:"type,omitempty"`
	Code          string `json:"code,omitempty"`
	DeclineCode   string `json:"decline_code,omitempty"`
	Param         string `json:"param,omitempty"`
	Message       string `json:"message,omitempty"`
	DocURL        string `json:"doc_url,omitempty"`
	RequestID     string `json:"request_id,omitempty"`
	RequestLogURL string `json:"request_log_url,omitempty"`
	RawBody       string `json:"raw_body,omitempty"`
}

func (e *APIErrorDetails) Error() string {
	if e == nil {
		return "stripe api error"
	}
	parts := make([]string, 0, 6)
	if e.StatusCode > 0 {
		parts = append(parts, fmt.Sprintf("status=%d", e.StatusCode))
	}
	if e.Type != "" {
		parts = append(parts, "type="+e.Type)
	}
	if e.Code != "" {
		parts = append(parts, "code="+e.Code)
	}
	if e.Param != "" {
		parts = append(parts, "param="+e.Param)
	}
	if e.Message != "" {
		parts = append(parts, "message="+e.Message)
	}
	if len(parts) == 0 {
		return "stripe api error"
	}
	return "stripe api error: " + strings.Join(parts, ", ")
}
