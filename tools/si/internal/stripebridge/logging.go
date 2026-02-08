package stripebridge

import (
	"si/tools/si/internal/apibridge"
)

type EventLogger interface{ Log(event map[string]any) }

type JSONLLogger = apibridge.JSONLLogger

func NewJSONLLogger(path string) *JSONLLogger {
	return apibridge.NewJSONLLogger(path)
}
