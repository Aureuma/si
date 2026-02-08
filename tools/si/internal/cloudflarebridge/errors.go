package cloudflarebridge

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var (
	reBearerToken     = regexp.MustCompile(`(?i)\\bBearer\\s+[A-Za-z0-9._-]+\\b`)
	reAPIToken        = regexp.MustCompile(`(?i)\\b[A-Za-z0-9_-]{36,}\\b`)
	rePrivateKeyBlock = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*-----END [A-Z ]*PRIVATE KEY-----`)
	reJWTLike         = regexp.MustCompile(`\\beyJ[A-Za-z0-9_-]+\\.[A-Za-z0-9._-]+\\.[A-Za-z0-9._-]+\\b`)
)

func RedactSensitive(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	value = reBearerToken.ReplaceAllString(value, "Bearer ***")
	value = rePrivateKeyBlock.ReplaceAllString(value, "-----BEGIN PRIVATE KEY-----***-----END PRIVATE KEY-----")
	value = reJWTLike.ReplaceAllString(value, "eyJ***.***.***")
	value = reAPIToken.ReplaceAllStringFunc(value, func(raw string) string {
		if strings.HasPrefix(strings.ToLower(raw), "http") {
			return raw
		}
		if len(raw) <= 10 {
			return "***"
		}
		return raw[:6] + "***"
	})
	return value
}

func NormalizeHTTPError(statusCode int, headers http.Header, rawBody string) *APIErrorDetails {
	details := &APIErrorDetails{
		StatusCode: statusCode,
		RawBody:    RedactSensitive(strings.TrimSpace(rawBody)),
	}
	if headers != nil {
		details.RequestID = strings.TrimSpace(headers.Get("CF-Ray"))
		if details.RequestID == "" {
			details.RequestID = strings.TrimSpace(headers.Get("X-Request-ID"))
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
	if errorsList, ok := parsed["errors"].([]any); ok {
		details.Errors = make([]map[string]any, 0, len(errorsList))
		for _, entry := range errorsList {
			obj, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			clean := map[string]any{}
			for key, value := range obj {
				switch typed := value.(type) {
				case string:
					clean[key] = RedactSensitive(typed)
				default:
					clean[key] = typed
				}
			}
			details.Errors = append(details.Errors, clean)
			if details.Code == 0 {
				details.Code = toInt(obj["code"])
			}
			if details.Message == "" {
				if msg, ok := obj["message"].(string); ok {
					details.Message = RedactSensitive(strings.TrimSpace(msg))
				}
			}
		}
	}
	if msg := strings.TrimSpace(toString(parsed["message"])); msg != "" && details.Message == "" {
		details.Message = RedactSensitive(msg)
	}
	if typ := strings.TrimSpace(toString(parsed["type"])); typ != "" {
		details.Type = RedactSensitive(typ)
	}
	if doc := strings.TrimSpace(toString(parsed["documentation_url"])); doc != "" {
		details.DocumentationURL = RedactSensitive(doc)
	}
	if details.Message == "" {
		details.Message = "cloudflare api request failed"
	}
	return details
}

func toInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}

func toString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		raw, _ := json.Marshal(typed)
		return string(raw)
	}
}
