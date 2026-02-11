package googleplaybridge

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Token struct {
	Value     string
	ExpiresAt time.Time
}

type TokenProvider interface {
	Token(ctx context.Context) (Token, error)
	Source() string
}

type EventLogger interface {
	Log(event map[string]any)
}

type ClientConfig struct {
	TokenProvider      TokenProvider
	BaseURL            string
	UploadBaseURL      string
	CustomAppBaseURL   string
	UserAgent          string
	Timeout            time.Duration
	MaxRetries         int
	Logger             EventLogger
	LogPath            string
	LogContext         map[string]string
	HTTPClient         *http.Client
	DisableCache       bool
	DisableProviderACL bool
}

type Request struct {
	Method           string
	Path             string
	Params           map[string]string
	Headers          map[string]string
	RawBody          string
	JSONBody         any
	ContentType      string
	UseUpload        bool
	UseCustomAppBase bool
	MediaPath        string
}

type Response struct {
	StatusCode int               `json:"status_code"`
	Status     string            `json:"status"`
	RequestID  string            `json:"request_id,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	Data       map[string]any    `json:"data,omitempty"`
	List       []map[string]any  `json:"list,omitempty"`
}

type APIErrorDetails struct {
	StatusCode int              `json:"status_code,omitempty"`
	Code       int              `json:"code,omitempty"`
	Status     string           `json:"status,omitempty"`
	Message    string           `json:"message,omitempty"`
	RequestID  string           `json:"request_id,omitempty"`
	Details    []map[string]any `json:"details,omitempty"`
	RawBody    string           `json:"raw_body,omitempty"`
}

func (e *APIErrorDetails) Error() string {
	if e == nil {
		return "google play api error"
	}
	parts := make([]string, 0, 5)
	if e.StatusCode > 0 {
		parts = append(parts, fmt.Sprintf("status_code=%d", e.StatusCode))
	}
	if e.Code > 0 {
		parts = append(parts, fmt.Sprintf("code=%d", e.Code))
	}
	if strings.TrimSpace(e.Status) != "" {
		parts = append(parts, "status="+e.Status)
	}
	if strings.TrimSpace(e.Message) != "" {
		parts = append(parts, "message="+e.Message)
	}
	if len(parts) == 0 {
		return "google play api error"
	}
	return "google play api error: " + strings.Join(parts, ", ")
}
