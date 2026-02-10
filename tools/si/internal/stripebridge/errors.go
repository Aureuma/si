package stripebridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

var (
	reSecretKey      = regexp.MustCompile(`\b(?:sk|rk|pk)_(?:live|test|sandbox)_[A-Za-z0-9]+\b`)
	reBearerToken    = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._-]+\b`)
	reClientSecretPI = regexp.MustCompile(`\bpi_[A-Za-z0-9]+_secret_[A-Za-z0-9]+\b`)
)

func RedactSensitive(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	value = reSecretKey.ReplaceAllStringFunc(value, func(raw string) string {
		if len(raw) <= 12 {
			return "***"
		}
		return raw[:8] + "***"
	})
	value = reBearerToken.ReplaceAllString(value, "Bearer ***")
	value = reClientSecretPI.ReplaceAllString(value, "pi_***_secret_***")
	return value
}

func NormalizeAPIError(err error, rawBody string) *APIErrorDetails {
	if err == nil {
		return nil
	}
	if details, ok := err.(*APIErrorDetails); ok && details != nil {
		cloned := *details
		cloned.Message = RedactSensitive(strings.TrimSpace(cloned.Message))
		cloned.Param = RedactSensitive(strings.TrimSpace(cloned.Param))
		cloned.DocURL = RedactSensitive(strings.TrimSpace(cloned.DocURL))
		cloned.RequestLogURL = RedactSensitive(strings.TrimSpace(cloned.RequestLogURL))
		cloned.RawBody = RedactSensitive(strings.TrimSpace(cloned.RawBody))
		return &cloned
	}
	details := &APIErrorDetails{
		Message: strings.TrimSpace(err.Error()),
		RawBody: RedactSensitive(strings.TrimSpace(rawBody)),
	}
	var timeoutErr interface{ Timeout() bool }
	if errors.As(err, &timeoutErr) && timeoutErr.Timeout() {
		details.Type = "network_error"
	}
	parsed := parseStripeErrorFields(rawBody)
	if details.Type == "" {
		details.Type = parsed.Type
	}
	if details.Code == "" {
		details.Code = parsed.Code
	}
	if details.DeclineCode == "" {
		details.DeclineCode = parsed.DeclineCode
	}
	if details.Param == "" {
		details.Param = parsed.Param
	}
	if details.Message == "" {
		details.Message = parsed.Message
	}
	if details.DocURL == "" {
		details.DocURL = parsed.DocURL
	}
	if details.RequestLogURL == "" {
		details.RequestLogURL = parsed.RequestLogURL
	}
	details.Message = RedactSensitive(details.Message)
	details.Param = RedactSensitive(details.Param)
	details.DocURL = RedactSensitive(details.DocURL)
	details.RequestLogURL = RedactSensitive(details.RequestLogURL)
	return details
}

func NormalizeHTTPError(statusCode int, headers http.Header, rawBody string) *APIErrorDetails {
	parsed := parseStripeErrorFields(rawBody)
	requestID := strings.TrimSpace(firstHeaderValue(headers, "Request-Id", "X-Request-Id"))
	details := &APIErrorDetails{
		StatusCode:    statusCode,
		Type:          parsed.Type,
		Code:          parsed.Code,
		DeclineCode:   parsed.DeclineCode,
		Param:         parsed.Param,
		Message:       parsed.Message,
		DocURL:        parsed.DocURL,
		RequestID:     requestID,
		RequestLogURL: parsed.RequestLogURL,
		RawBody:       RedactSensitive(strings.TrimSpace(rawBody)),
	}
	if details.Message == "" {
		details.Message = strings.TrimSpace(http.StatusText(statusCode))
	}
	if details.Message == "" {
		details.Message = fmt.Sprintf("http status %d", statusCode)
	}
	details.Message = RedactSensitive(details.Message)
	details.Param = RedactSensitive(strings.TrimSpace(details.Param))
	details.DocURL = RedactSensitive(strings.TrimSpace(details.DocURL))
	details.RequestLogURL = RedactSensitive(strings.TrimSpace(details.RequestLogURL))
	return details
}

func parseStripeErrorFields(rawBody string) APIErrorDetails {
	rawBody = strings.TrimSpace(rawBody)
	if rawBody == "" {
		return APIErrorDetails{}
	}
	var payload map[string]any
	if jsonErr := unmarshalJSONMap(rawBody, &payload); jsonErr != nil {
		return APIErrorDetails{Message: rawBody}
	}
	errorObj := payload
	if nested, ok := payload["error"].(map[string]any); ok && nested != nil {
		errorObj = nested
	}
	return APIErrorDetails{
		Type:          asString(errorObj["type"]),
		Code:          asString(errorObj["code"]),
		DeclineCode:   asString(errorObj["decline_code"]),
		Param:         asString(errorObj["param"]),
		Message:       firstNonBlankString(asString(errorObj["message"]), asString(payload["message"])),
		DocURL:        asString(errorObj["doc_url"]),
		RequestLogURL: asString(errorObj["request_log_url"]),
	}
}

func unmarshalJSONMap(raw string, dst *map[string]any) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(dst)
}

func asString(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func firstNonBlankString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func firstHeaderValue(headers http.Header, keys ...string) string {
	if headers == nil {
		return ""
	}
	for _, key := range keys {
		if value := strings.TrimSpace(headers.Get(strings.TrimSpace(key))); value != "" {
			return value
		}
	}
	return ""
}
