package githubbridge

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

var (
	reGithubToken     = regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]+\b`)
	reGithubPatLong   = regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]+\b`)
	reBearerToken     = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._-]+\b`)
	rePrivateKeyBlock = regexp.MustCompile(`-----BEGIN [A-Z ]+PRIVATE KEY-----[\s\S]*?-----END [A-Z ]+PRIVATE KEY-----`)
	reJWTLike         = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9._-]+\.[A-Za-z0-9._-]+\b`)
)

func RedactSensitive(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	value = reGithubToken.ReplaceAllString(value, "gh*_***")
	value = reGithubPatLong.ReplaceAllString(value, "github_pat_***")
	value = reBearerToken.ReplaceAllString(value, "Bearer ***")
	value = rePrivateKeyBlock.ReplaceAllString(value, "-----BEGIN PRIVATE KEY-----***-----END PRIVATE KEY-----")
	value = reJWTLike.ReplaceAllString(value, "eyJ***.***.***")
	return value
}

func NormalizeHTTPError(statusCode int, headers http.Header, rawBody string) *APIErrorDetails {
	details := &APIErrorDetails{
		StatusCode: statusCode,
		RawBody:    RedactSensitive(strings.TrimSpace(rawBody)),
	}
	if headers != nil {
		details.RequestID = strings.TrimSpace(headers.Get("X-GitHub-Request-Id"))
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
	if value, ok := parsed["message"].(string); ok {
		details.Message = RedactSensitive(strings.TrimSpace(value))
	}
	if value, ok := parsed["documentation_url"].(string); ok {
		details.DocumentationURL = RedactSensitive(strings.TrimSpace(value))
	}
	if value, ok := parsed["type"].(string); ok {
		details.Type = RedactSensitive(strings.TrimSpace(value))
	}
	if value, ok := parsed["code"].(string); ok {
		details.Code = RedactSensitive(strings.TrimSpace(value))
	}
	if list, ok := parsed["errors"].([]any); ok {
		details.Errors = make([]map[string]any, 0, len(list))
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
			details.Errors = append(details.Errors, clean)
		}
	}
	if strings.TrimSpace(details.Message) == "" {
		details.Message = "github api request failed"
	}
	return details
}
