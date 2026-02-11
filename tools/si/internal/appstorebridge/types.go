package appstorebridge

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
	TokenProvider TokenProvider
	BaseURL       string
	UserAgent     string
	Timeout       time.Duration
	MaxRetries    int
	Logger        EventLogger
	LogPath       string
	LogContext    map[string]string
	HTTPClient    *http.Client
	DisableCache  bool
}

type Request struct {
	Method      string
	Path        string
	Params      map[string]string
	Headers     map[string]string
	RawBody     string
	JSONBody    any
	ContentType string
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
	Code       string           `json:"code,omitempty"`
	Title      string           `json:"title,omitempty"`
	Detail     string           `json:"detail,omitempty"`
	RequestID  string           `json:"request_id,omitempty"`
	Errors     []map[string]any `json:"errors,omitempty"`
	RawBody    string           `json:"raw_body,omitempty"`
}

func (e *APIErrorDetails) Error() string {
	if e == nil {
		return "apple appstore api error"
	}
	parts := make([]string, 0, 6)
	if e.StatusCode > 0 {
		parts = append(parts, fmt.Sprintf("status_code=%d", e.StatusCode))
	}
	if strings.TrimSpace(e.Code) != "" {
		parts = append(parts, "code="+e.Code)
	}
	if strings.TrimSpace(e.Title) != "" {
		parts = append(parts, "title="+e.Title)
	}
	if strings.TrimSpace(e.Detail) != "" {
		parts = append(parts, "detail="+e.Detail)
	}
	if len(parts) == 0 {
		return "apple appstore api error"
	}
	return "apple appstore api error: " + strings.Join(parts, ", ")
}
