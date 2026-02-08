package googleplacesbridge

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

var (
	reGoogleAPIKey = regexp.MustCompile(`AIza[0-9A-Za-z_-]{20,}`)
	reBearerToken  = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._-]+\b`)
	reKeyParam     = regexp.MustCompile(`(?i)([?&](?:key|api_key)=)([^&\s]+)`)
)

func RedactSensitive(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	value = reGoogleAPIKey.ReplaceAllString(value, "AIza***")
	value = reBearerToken.ReplaceAllString(value, "Bearer ***")
	value = reKeyParam.ReplaceAllString(value, "$1***")
	return value
}

func NormalizeHTTPError(statusCode int, headers http.Header, rawBody string) *APIErrorDetails {
	details := &APIErrorDetails{
		StatusCode: statusCode,
		RawBody:    RedactSensitive(strings.TrimSpace(rawBody)),
	}
	if headers != nil {
		details.RequestID = strings.TrimSpace(headers.Get("X-Request-Id"))
		if details.RequestID == "" {
			details.RequestID = strings.TrimSpace(headers.Get("X-Google-Request-Id"))
		}
	}
	if details.StatusCode == 0 {
		details.StatusCode = -1
	}
	body := strings.TrimSpace(rawBody)
	if body == "" {
		details.Message = "empty response body"
		return details
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		details.Message = RedactSensitive(body)
		return details
	}
	errObj, ok := parsed["error"].(map[string]any)
	if !ok {
		if msg, ok := parsed["message"].(string); ok && strings.TrimSpace(msg) != "" {
			details.Message = RedactSensitive(strings.TrimSpace(msg))
		} else {
			details.Message = "google places api request failed"
		}
		return details
	}
	if value, ok := errObj["code"].(float64); ok {
		details.Code = int(value)
	}
	if details.Code == 0 && details.StatusCode > 0 {
		details.Code = details.StatusCode
	}
	if status, ok := errObj["status"].(string); ok {
		details.Status = RedactSensitive(strings.TrimSpace(status))
	}
	if msg, ok := errObj["message"].(string); ok {
		details.Message = RedactSensitive(strings.TrimSpace(msg))
	}
	if list, ok := errObj["details"].([]any); ok {
		details.Details = make([]map[string]any, 0, len(list))
		for _, item := range list {
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
			details.Details = append(details.Details, clean)
		}
	}
	if strings.TrimSpace(details.Message) == "" {
		details.Message = "google places api request failed"
	}
	return details
}
