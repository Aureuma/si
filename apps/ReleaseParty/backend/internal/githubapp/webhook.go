package githubapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// VerifyWebhook verifies the GitHub webhook payload signature.
// Supports both "sha256=" and (legacy) "sha1=" signatures. We primarily use sha256.
func (a *App) VerifyWebhook(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	_ = r.Body.Close()

	sig256 := strings.TrimSpace(r.Header.Get("X-Hub-Signature-256"))
	if sig256 != "" {
		if err := verifySig("sha256", sig256, body, []byte(a.Secret)); err != nil {
			return nil, err
		}
		return body, nil
	}
	sig1 := strings.TrimSpace(r.Header.Get("X-Hub-Signature"))
	if sig1 != "" {
		return nil, fmt.Errorf("sha1 signature not supported (provide X-Hub-Signature-256)")
	}
	return nil, fmt.Errorf("missing webhook signature header")
}

func verifySig(kind, header string, body, secret []byte) error {
	prefix := kind + "="
	if !strings.HasPrefix(header, prefix) {
		return fmt.Errorf("invalid signature header prefix")
	}
	wantHex := strings.TrimPrefix(header, prefix)
	got := hmacSum(kind, body, secret)
	gotHex := hex.EncodeToString(got)
	if !hmac.Equal([]byte(wantHex), []byte(gotHex)) {
		return fmt.Errorf("invalid webhook signature")
	}
	return nil
}

func hmacSum(kind string, body, secret []byte) []byte {
	var mac hashHash
	switch kind {
	case "sha256":
		mac = hmac.New(sha256.New, secret)
	default:
		mac = hmac.New(sha256.New, secret)
	}
	_, _ = mac.Write(body)
	return mac.Sum(nil)
}

type hashHash interface {
	Write([]byte) (int, error)
	Sum([]byte) []byte
}

