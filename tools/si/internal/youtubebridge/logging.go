package youtubebridge

import "si/tools/si/internal/apibridge"

type JSONLLogger = apibridge.JSONLLogger

func NewJSONLLogger(path string) *JSONLLogger {
	return apibridge.NewJSONLLogger(path)
}
