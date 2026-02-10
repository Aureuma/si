package youtubebridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type JSONLLogger struct {
	path string
	mu   sync.Mutex
}

func NewJSONLLogger(path string) *JSONLLogger {
	path = filepath.Clean(path)
	return &JSONLLogger{path: path}
}

func (l *JSONLLogger) Log(event map[string]any) {
	if l == nil || l.path == "" {
		return
	}
	if event == nil {
		event = map[string]any{}
	}
	event["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	data = append(data, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()

	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	file, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = file.Write(data)
	_ = file.Close()
}
