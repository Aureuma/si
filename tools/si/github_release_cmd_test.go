package main

import "testing"

func TestExpandReleaseUploadURL(t *testing.T) {
	u, err := expandReleaseUploadURL("https://uploads.github.com/repos/acme/repo/releases/1/assets{?name,label}", map[string]string{"name": "asset.bin", "label": "linux amd64"})
	if err != nil {
		t.Fatalf("expand url: %v", err)
	}
	if u != "https://uploads.github.com/repos/acme/repo/releases/1/assets?label=linux+amd64&name=asset.bin" &&
		u != "https://uploads.github.com/repos/acme/repo/releases/1/assets?name=asset.bin&label=linux+amd64" {
		t.Fatalf("unexpected upload url: %s", u)
	}
}

func TestExpandReleaseUploadURLMissing(t *testing.T) {
	if _, err := expandReleaseUploadURL("", map[string]string{"name": "x"}); err == nil {
		t.Fatalf("expected error for missing upload url")
	}
}
