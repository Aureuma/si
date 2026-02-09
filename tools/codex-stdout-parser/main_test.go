package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() {
		os.Stdout = old
	}()
	outCh := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		outCh <- string(b)
	}()
	fn()
	_ = w.Close()
	return <-outCh
}

func parseJSONOutputs(t *testing.T, raw string) []output {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	events := []output{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var out output
		if err := json.Unmarshal([]byte(line), &out); err != nil {
			t.Fatalf("unmarshal output %q: %v", line, err)
		}
		events = append(events, out)
	}
	return events
}

func TestParserMultipleTurns(t *testing.T) {
	promptRe := regexp.MustCompile("^$")
	endRe := regexp.MustCompile("^DONE$")
	p := newParser(promptRe, nil, nil, endRe, "block", "", true, true, false, true, false, 0, "")

	raw := captureStdout(t, func() {
		p.handleLine("first")
		p.handleLine("DONE")
		p.handleLine("second")
		p.handleLine("DONE")
	})

	events := parseJSONOutputs(t, raw)
	if len(events) != 2 {
		t.Fatalf("expected 2 outputs, got %d: %q", len(events), raw)
	}
	if events[0].FinalReport != "first" {
		t.Fatalf("turn1 final report = %q", events[0].FinalReport)
	}
	if events[1].FinalReport != "second" {
		t.Fatalf("turn2 final report = %q", events[1].FinalReport)
	}
	if events[0].Turn != 1 || events[1].Turn != 2 {
		t.Fatalf("unexpected turn numbers: %+v", events)
	}
}

func TestParserUsesSessionLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "session.jsonl")
	logLine := `{"kind":"codex_event","payload":{"msg":{"type":"agent_message","message":"SESSION_OK"}}}`
	if err := os.WriteFile(logPath, []byte(logLine+"\n"), 0o600); err != nil {
		t.Fatalf("write session log: %v", err)
	}

	promptRe := regexp.MustCompile("^$")
	endRe := regexp.MustCompile("^DONE$")
	p := newParser(promptRe, nil, nil, endRe, "block", "", true, true, false, true, true, 0*time.Second, logPath)

	raw := captureStdout(t, func() {
		p.handleLine("ignored")
		p.handleLine("DONE")
	})
	events := parseJSONOutputs(t, raw)
	if len(events) != 1 {
		t.Fatalf("expected 1 output, got %d: %q", len(events), raw)
	}
	if events[0].FinalReport != "SESSION_OK" {
		t.Fatalf("final report = %q", events[0].FinalReport)
	}
}

func TestReadAgentMessagesSkipsNoise(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "session.jsonl")
	line := `{"kind":"codex_event","payload":{"msg":{"type":"agent_message","message":"OK"}}}`
	payload := []byte("not-json\n")
	payload = append(payload, []byte(line)...)
	payload = append(payload, 0x00, '\n')
	payload = append(payload, []byte(`{"kind":"other","payload":{"msg":{"type":"agent_message","message":"skip"}}}`)...)
	if err := os.WriteFile(logPath, payload, 0o600); err != nil {
		t.Fatalf("write session log: %v", err)
	}
	msgs, err := readAgentMessages(logPath)
	if err != nil {
		t.Fatalf("read agent messages: %v", err)
	}
	if len(msgs) != 1 || strings.TrimSpace(msgs[0]) != "OK" {
		t.Fatalf("unexpected messages: %#v", msgs)
	}
}

func TestStripANSI_RemovesCSIAndOSC(t *testing.T) {
	input := "\x1b[31mred\x1b[0m plain \x1b]0;title\x07done"
	got := stripANSI(input)
	want := "red plain done"
	if got != want {
		t.Fatalf("stripANSI(%q) = %q, want %q", input, got, want)
	}
}

func TestDecodeEscapes_UnquotesAndPreservesEscapes(t *testing.T) {
	got := decodeEscapes(`\r\n`)
	if got != "\r\n" {
		t.Fatalf("decodeEscapes(\\\\r\\\\n) = %q, want CRLF", got)
	}
}

func TestDecodeEscapes_QuotedInput(t *testing.T) {
	got := decodeEscapes(`"tab:\t"`)
	if got != "tab:\t" {
		t.Fatalf("decodeEscapes quoted = %q, want tab escape", got)
	}
}

func TestDecodeEscapes_Invalid(t *testing.T) {
	input := `\q`
	got := decodeEscapes(input)
	if got != input {
		t.Fatalf("decodeEscapes invalid = %q, want %q", got, input)
	}
}

func TestDecodeEscapes_Empty(t *testing.T) {
	got := decodeEscapes("   ")
	if got != "" {
		t.Fatalf("decodeEscapes empty = %q, want empty string", got)
	}
}

func TestStripANSI_PreservesIncompleteEscape(t *testing.T) {
	input := "start\x1b[end"
	got := stripANSI(input)
	want := "startnd"
	if got != want {
		t.Fatalf("stripANSI incomplete = %q, want %q", got, want)
	}
}

func TestParserFlushEOF_Default(t *testing.T) {
	promptRe := regexp.MustCompile("^$")
	p := newParser(promptRe, nil, nil, nil, "block", "", true, true, false, true, false, 0, "")

	raw := captureStdout(t, func() {
		p.handleLine("hello")
		p.flushEOF()
	})
	events := parseJSONOutputs(t, raw)
	if len(events) != 1 {
		t.Fatalf("expected 1 output, got %d: %q", len(events), raw)
	}
	if events[0].Status != "eof" || events[0].ReadyForPrompt {
		t.Fatalf("unexpected eof status: %+v", events[0])
	}
	if events[0].FinalReport != "hello" {
		t.Fatalf("final report = %q", events[0].FinalReport)
	}
}

func TestParserFlushEOF_Ready(t *testing.T) {
	promptRe := regexp.MustCompile("^$")
	p := newParser(promptRe, nil, nil, nil, "block", "", true, true, true, true, false, 0, "")

	raw := captureStdout(t, func() {
		p.handleLine("hello")
		p.flushEOF()
	})
	events := parseJSONOutputs(t, raw)
	if len(events) != 1 {
		t.Fatalf("expected 1 output, got %d: %q", len(events), raw)
	}
	if events[0].Status != "turn_complete_exit" || !events[0].ReadyForPrompt {
		t.Fatalf("unexpected ready status: %+v", events[0])
	}
}

func TestParserFlushEOF_Disabled(t *testing.T) {
	promptRe := regexp.MustCompile("^$")
	p := newParser(promptRe, nil, nil, nil, "block", "", true, false, false, true, false, 0, "")

	raw := captureStdout(t, func() {
		p.handleLine("hello")
		p.flushEOF()
	})

	if strings.TrimSpace(raw) != "" {
		t.Fatalf("expected no output when flush-on-eof disabled, got %q", raw)
	}
}

func TestParserLastLineMode(t *testing.T) {
	promptRe := regexp.MustCompile("^$")
	endRe := regexp.MustCompile("^DONE$")
	p := newParser(promptRe, nil, nil, endRe, "last-line", "", true, true, false, true, false, 0, "")

	raw := captureStdout(t, func() {
		p.handleLine("first")
		p.handleLine("second")
		p.handleLine("DONE")
	})

	events := parseJSONOutputs(t, raw)
	if len(events) != 1 {
		t.Fatalf("expected 1 output, got %d: %q", len(events), raw)
	}
	if events[0].FinalReport != "second" {
		t.Fatalf("final report = %q, want last line", events[0].FinalReport)
	}
}

func TestReadAgentMessages_MissingFile(t *testing.T) {
	_, err := readAgentMessages(filepath.Join(t.TempDir(), "missing.jsonl"))
	if err == nil {
		t.Fatal("expected error for missing session log file")
	}
}
