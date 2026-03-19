package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/nacl/box"
)

func encryptGitHubSecretValue(base64PublicKey string, plaintext string) (string, error) {
	pubBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(base64PublicKey))
	if err != nil {
		return "", fmt.Errorf("decode github public key: %w", err)
	}
	if len(pubBytes) != 32 {
		return "", fmt.Errorf("invalid github public key length: %d", len(pubBytes))
	}
	var recipientPub [32]byte
	copy(recipientPub[:], pubBytes)
	ephemeralPub, ephemeralPriv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate ephemeral key: %w", err)
	}
	nonceHash, err := blake2b.New(24, nil)
	if err != nil {
		return "", fmt.Errorf("init nonce hash: %w", err)
	}
	if _, err := nonceHash.Write(ephemeralPub[:]); err != nil {
		return "", fmt.Errorf("hash ephemeral key: %w", err)
	}
	if _, err := nonceHash.Write(recipientPub[:]); err != nil {
		return "", fmt.Errorf("hash recipient key: %w", err)
	}
	nonceBytes := nonceHash.Sum(nil)
	var nonce [24]byte
	copy(nonce[:], nonceBytes)
	sealed := box.Seal(nil, []byte(plaintext), &nonce, &recipientPub, ephemeralPriv)
	out := make([]byte, 0, len(ephemeralPub)+len(sealed))
	out = append(out, ephemeralPub[:]...)
	out = append(out, sealed...)
	return base64.StdEncoding.EncodeToString(out), nil
}

func parseGitHubCSVInts(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int64, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		value, err := strconv.ParseInt(part, 10, 64)
		if err != nil || value <= 0 {
			continue
		}
		out = append(out, value)
	}
	return out
}
