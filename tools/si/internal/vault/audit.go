package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type AuditSink interface {
	Log(event map[string]any)
}

type JSONLAudit struct {
	path string
	mu   sync.Mutex
}

func NewJSONLAudit(path string) *JSONLAudit {
	path, _ = ExpandHome(path)
	path = filepath.Clean(path)
	return &JSONLAudit{path: path}
}

func (l *JSONLAudit) Log(event map[string]any) {
	if l == nil || l.path == "" {
		return
	}
	if event == nil {
		event = map[string]any{}
	}
	if _, ok := event["ts"]; !ok {
		event["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
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
