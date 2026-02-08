package githubbridge

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type AuthMode string

const (
	AuthModeApp AuthMode = "app"
)

func ParseAuthMode(raw string) (AuthMode, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case string(AuthModeApp):
		return AuthModeApp, nil
	case "":
		return "", fmt.Errorf("auth mode required (app)")
	default:
		return "", fmt.Errorf("invalid auth mode %q (expected app)", raw)
	}
}

type TokenRequest struct {
	Owner          string
	Repo           string
	InstallationID int64
}

type Token struct {
	Value     string
	ExpiresAt time.Time
}

type TokenProvider interface {
	Mode() AuthMode
	Source() string
	Token(ctx context.Context, req TokenRequest) (Token, error)
}

type EventLogger interface {
	Log(event map[string]any)
}

type ClientConfig struct {
	BaseURL    string
	UserAgent  string
	Timeout    time.Duration
	MaxRetries int
	Provider   TokenProvider
	Logger     EventLogger
	LogPath    string
	LogContext map[string]string
	HTTPClient *http.Client
}

type Request struct {
	Method         string
	Path           string
	Params         map[string]string
	Headers        map[string]string
	RawBody        string
	JSONBody       any
	ContentType    string
	Owner          string
	Repo           string
	InstallationID int64
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
	StatusCode       int              `json:"status_code,omitempty"`
	Message          string           `json:"message,omitempty"`
	DocumentationURL string           `json:"documentation_url,omitempty"`
	RequestID        string           `json:"request_id,omitempty"`
	Code             string           `json:"code,omitempty"`
	Type             string           `json:"type,omitempty"`
	Errors           []map[string]any `json:"errors,omitempty"`
	RawBody          string           `json:"raw_body,omitempty"`
}

func (e *APIErrorDetails) Error() string {
	if e == nil {
		return "github api error"
	}
	parts := make([]string, 0, 5)
	if e.StatusCode > 0 {
		parts = append(parts, fmt.Sprintf("status=%d", e.StatusCode))
	}
	if strings.TrimSpace(e.Type) != "" {
		parts = append(parts, "type="+e.Type)
	}
	if strings.TrimSpace(e.Code) != "" {
		parts = append(parts, "code="+e.Code)
	}
	if strings.TrimSpace(e.Message) != "" {
		parts = append(parts, "message="+e.Message)
	}
	if len(parts) == 0 {
		return "github api error"
	}
	return "github api error: " + strings.Join(parts, ", ")
}
