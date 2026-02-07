package main

import (
	"strings"
	"testing"
)

func TestPrintKeyValueMap_NoColor(t *testing.T) {
	prev := ansiEnabled
	ansiEnabled = false
	defer func() { ansiEnabled = prev }()
	out := captureStdout(t, func() {
		printKeyValueMap(map[string]any{"name": "demo", "active": true})
	})
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("unexpected ANSI in no-color output: %q", out)
	}
	if !strings.Contains(out, "name:") {
		t.Fatalf("expected key output, got %q", out)
	}
}

func TestPrintKeyValueMap_Color(t *testing.T) {
	prev := ansiEnabled
	ansiEnabled = true
	defer func() { ansiEnabled = prev }()
	out := captureStdout(t, func() {
		printKeyValueMap(map[string]any{"name": "demo"})
	})
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected ANSI output when color enabled: %q", out)
	}
}
