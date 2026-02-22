package main

import (
	"testing"
	"time"
)

func TestLoginOutputHandlerHeadlessDisablesWatcher(t *testing.T) {
	handler := loginOutputHandler(true, true, "safari-profile", codexProfile{ID: "dev"}, "")
	if handler != nil {
		t.Fatalf("expected nil output handler in headless mode")
	}
}

func TestLoginOutputHandlerNonHeadlessParsesCodeAndURL(t *testing.T) {
	oldOpen := openLoginURLFn
	oldCopy := copyDeviceCodeToClipboardFn
	t.Cleanup(func() {
		openLoginURLFn = oldOpen
		copyDeviceCodeToClipboardFn = oldCopy
	})

	urlCh := make(chan string, 1)
	codeCh := make(chan string, 1)
	openLoginURLFn = func(url string, _ codexProfile, _ string, _ string) {
		urlCh <- url
	}
	copyDeviceCodeToClipboardFn = func(code string) {
		codeCh <- code
	}

	handler := loginOutputHandler(false, true, "chrome-profile", codexProfile{ID: "dev"}, "")
	if handler == nil {
		t.Fatalf("expected output handler")
	}
	handler([]byte("Open: https://example.com/device\nCode: ABCD-EFGH\n"))

	select {
	case got := <-urlCh:
		if got != "https://example.com/device" {
			t.Fatalf("unexpected URL: %q", got)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("expected URL callback")
	}
	select {
	case got := <-codeCh:
		if got != "ABCD-EFGH" {
			t.Fatalf("unexpected code: %q", got)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("expected code callback")
	}
}

func TestLoginOutputHandlerNonHeadlessOpenURLDisabledStillCopiesCode(t *testing.T) {
	oldOpen := openLoginURLFn
	oldCopy := copyDeviceCodeToClipboardFn
	t.Cleanup(func() {
		openLoginURLFn = oldOpen
		copyDeviceCodeToClipboardFn = oldCopy
	})

	calledOpen := false
	codeCh := make(chan string, 1)
	openLoginURLFn = func(_ string, _ codexProfile, _ string, _ string) {
		calledOpen = true
	}
	copyDeviceCodeToClipboardFn = func(code string) {
		codeCh <- code
	}

	handler := loginOutputHandler(false, false, "", codexProfile{ID: "dev"}, "")
	if handler == nil {
		t.Fatalf("expected output handler")
	}
	handler([]byte("Code: WXYZ-9999\n"))

	select {
	case got := <-codeCh:
		if got != "WXYZ-9999" {
			t.Fatalf("unexpected code: %q", got)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("expected code callback")
	}
	if calledOpen {
		t.Fatalf("did not expect URL opener callback")
	}
}
