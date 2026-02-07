package stripebridge

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	stripe "github.com/stripe/stripe-go/v83"
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
	details := &APIErrorDetails{
		Message: strings.TrimSpace(err.Error()),
		RawBody: RedactSensitive(strings.TrimSpace(rawBody)),
	}
	var stripeErr *stripe.Error
	if errors.As(err, &stripeErr) && stripeErr != nil {
		details.StatusCode = stripeErr.HTTPStatusCode
		details.Type = string(stripeErr.Type)
		details.Code = string(stripeErr.Code)
		details.DeclineCode = string(stripeErr.DeclineCode)
		details.Param = stripeErr.Param
		if strings.TrimSpace(stripeErr.Msg) != "" {
			details.Message = stripeErr.Msg
		}
		details.DocURL = stripeErr.DocURL
		details.RequestID = stripeErr.RequestID
		details.RequestLogURL = stripeErr.RequestLogURL
	}
	if details.RawBody == "" {
		if data, marshalErr := json.Marshal(err); marshalErr == nil {
			details.RawBody = RedactSensitive(string(data))
		}
	}
	details.Message = RedactSensitive(details.Message)
	details.Param = RedactSensitive(details.Param)
	details.DocURL = RedactSensitive(details.DocURL)
	details.RequestLogURL = RedactSensitive(details.RequestLogURL)
	return details
}
