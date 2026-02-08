package main

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"golang.org/x/crypto/nacl/box"
)

func TestParseGitHubCSVInts(t *testing.T) {
	values := parseGitHubCSVInts("1, 2, x, 0, 3")
	if len(values) != 3 || values[0] != 1 || values[1] != 2 || values[2] != 3 {
		t.Fatalf("unexpected values: %#v", values)
	}
}

func TestEncryptGitHubSecretValue(t *testing.T) {
	pub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	encodedPub := base64.StdEncoding.EncodeToString(pub[:])
	sealed, err := encryptGitHubSecretValue(encodedPub, "hello")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	data, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(data) <= 32 {
		t.Fatalf("sealed payload too short: %d", len(data))
	}
}
