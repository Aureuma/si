package cloudflarebridge

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

type AuthMode string

const (
	AuthModeToken AuthMode = "token"
)

type EventLogger interface {
	Log(event map[string]any)
}

type ClientConfig struct {
	APIToken   string
	BaseURL    string
	UserAgent  string
	Timeout    time.Duration
	MaxRetries int
	Logger     EventLogger
	LogPath    string
	LogContext map[string]string
	HTTPClient *http.Client
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
	Success    bool              `json:"success"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	Data       map[string]any    `json:"data,omitempty"`
	List       []map[string]any  `json:"list,omitempty"`
	Messages   []map[string]any  `json:"messages,omitempty"`
}

type APIErrorDetails struct {
	StatusCode       int              `json:"status_code,omitempty"`
	Code             int              `json:"code,omitempty"`
	Type             string           `json:"type,omitempty"`
	Message          string           `json:"message,omitempty"`
	DocumentationURL string           `json:"documentation_url,omitempty"`
	RequestID        string           `json:"request_id,omitempty"`
	Errors           []map[string]any `json:"errors,omitempty"`
	RawBody          string           `json:"raw_body,omitempty"`
}

func (e *APIErrorDetails) Error() string {
	if e == nil {
		return "cloudflare api error"
	}
	parts := make([]string, 0, 5)
	if e.StatusCode > 0 {
		parts = append(parts, fmt.Sprintf("status=%d", e.StatusCode))
	}
	if e.Code > 0 {
		parts = append(parts, fmt.Sprintf("code=%d", e.Code))
	}
	if strings.TrimSpace(e.Type) != "" {
		parts = append(parts, "type="+e.Type)
	}
	if strings.TrimSpace(e.Message) != "" {
		parts = append(parts, "message="+e.Message)
	}
	if len(parts) == 0 {
		return "cloudflare api error"
	}
	return "cloudflare api error: " + strings.Join(parts, ", ")
}
