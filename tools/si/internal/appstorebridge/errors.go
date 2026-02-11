package appstorebridge

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var (
	reBearerToken   = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._-]+\b`)
	reJWTLike       = regexp.MustCompile(`\b[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
	rePrivateKeyPEM = regexp.MustCompile(`(?s)(-----BEGIN PRIVATE KEY-----).*?(-----END PRIVATE KEY-----)`)
)

func RedactSensitive(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	value = reBearerToken.ReplaceAllString(value, "Bearer ***")
	value = reJWTLike.ReplaceAllString(value, "***.***.***")
	value = rePrivateKeyPEM.ReplaceAllString(value, "$1***$2")
	return value
}

func NormalizeHTTPError(statusCode int, headers http.Header, rawBody string) *APIErrorDetails {
	details := &APIErrorDetails{
		StatusCode: statusCode,
		RawBody:    RedactSensitive(strings.TrimSpace(rawBody)),
	}
	if headers != nil {
		details.RequestID = strings.TrimSpace(headers.Get("x-request-id"))
		if details.RequestID == "" {
			details.RequestID = strings.TrimSpace(headers.Get("X-Request-ID"))
		}
	}
	if details.StatusCode == 0 {
		details.StatusCode = -1
	}
	body := strings.TrimSpace(rawBody)
	if body == "" {
		details.Detail = "empty response body"
		return details
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		details.Detail = RedactSensitive(body)
		return details
	}
	errs, ok := parsed["errors"].([]any)
	if !ok || len(errs) == 0 {
		if msg, ok := parsed["message"].(string); ok && strings.TrimSpace(msg) != "" {
			details.Detail = RedactSensitive(strings.TrimSpace(msg))
		} else {
			details.Detail = "apple appstore api request failed"
		}
		return details
	}
	details.Errors = make([]map[string]any, 0, len(errs))
	for _, item := range errs {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		clean := map[string]any{}
		for k, v := range obj {
			switch typed := v.(type) {
			case string:
				clean[k] = RedactSensitive(typed)
			default:
				clean[k] = typed
			}
		}
		details.Errors = append(details.Errors, clean)
	}
	if len(details.Errors) > 0 {
		first := details.Errors[0]
		if code, ok := first["code"].(string); ok {
			details.Code = strings.TrimSpace(code)
		}
		if title, ok := first["title"].(string); ok {
			details.Title = strings.TrimSpace(title)
		}
		if detail, ok := first["detail"].(string); ok {
			details.Detail = strings.TrimSpace(detail)
		}
		if statusStr, ok := first["status"].(string); ok {
			if parsedStatus, err := strconv.Atoi(strings.TrimSpace(statusStr)); err == nil && parsedStatus > 0 {
				details.StatusCode = parsedStatus
			}
		}
	}
	if strings.TrimSpace(details.Detail) == "" {
		details.Detail = "apple appstore api request failed"
	}
	return details
}
