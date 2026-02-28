package main

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/nacl/box"
)

func TestParseGitHubCSVInts(t *testing.T) {
	values := parseGitHubCSVInts("1, 2, x, 0, 3")
	if len(values) != 3 || values[0] != 1 || values[1] != 2 || values[2] != 3 {
		t.Fatalf("unexpected values: %#v", values)
	}
}

func TestEncryptGitHubSecretValue(t *testing.T) {
	pub, priv, err := box.GenerateKey(rand.Reader)
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
	var ephemeralPub [32]byte
	copy(ephemeralPub[:], data[:32])
	nonceHash, err := blake2b.New(24, nil)
	if err != nil {
		t.Fatalf("nonce hash init: %v", err)
	}
	if _, err := nonceHash.Write(ephemeralPub[:]); err != nil {
		t.Fatalf("nonce hash write ephemeral: %v", err)
	}
	if _, err := nonceHash.Write(pub[:]); err != nil {
		t.Fatalf("nonce hash write recipient: %v", err)
	}
	nonceBytes := nonceHash.Sum(nil)
	var nonce [24]byte
	copy(nonce[:], nonceBytes)
	opened, ok := box.Open(nil, data[32:], &nonce, &ephemeralPub, priv)
	if !ok {
		t.Fatalf("decrypt sealed payload failed")
	}
	if got := string(opened); got != "hello" {
		t.Fatalf("unexpected plaintext: %q", got)
	}
}
