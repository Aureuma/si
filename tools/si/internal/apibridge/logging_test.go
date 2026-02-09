package apibridge

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestJSONLLogger_WritesOneLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs", "api.log")
	l := NewJSONLLogger(path)
	l.Log(map[string]any{"event": "request", "k": "v"})

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		t.Fatalf("expected 1 line")
	}
	line := sc.Bytes()
	var obj map[string]any
	if err := json.Unmarshal(line, &obj); err != nil {
		t.Fatalf("json: %v", err)
	}
	if obj["event"] != "request" || obj["k"] != "v" {
		t.Fatalf("unexpected object: %#v", obj)
	}
	if _, ok := obj["ts"]; !ok {
		t.Fatalf("expected ts field")
	}
	if sc.Scan() {
		t.Fatalf("expected exactly 1 line")
	}
}
