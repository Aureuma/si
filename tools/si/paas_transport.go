package main

import (
	"os"
	"strings"
)

const (
	paasAuthMethodKey      = "key"
	paasAuthMethodPassword = "password"
	paasAuthMethodLocal    = "local"
)

func normalizePaasAuthMethod(raw string) string {
	method := strings.ToLower(strings.TrimSpace(raw))
	switch method {
	case "", paasAuthMethodKey:
		return paasAuthMethodKey
	case paasAuthMethodPassword:
		return paasAuthMethodPassword
	case paasAuthMethodLocal:
		return paasAuthMethodLocal
	default:
		return method
	}
}

func isValidPaasAuthMethod(raw string) bool {
	switch normalizePaasAuthMethod(raw) {
	case paasAuthMethodKey, paasAuthMethodPassword, paasAuthMethodLocal:
		return true
	default:
		return false
	}
}

func isPaasLocalTarget(target paasTarget) bool {
	if normalizePaasAuthMethod(target.AuthMethod) == paasAuthMethodLocal {
		return true
	}
	host := strings.ToLower(strings.TrimSpace(target.Host))
	switch host {
	case "", "localhost", "127.0.0.1", "::1":
		return true
	}
	hostname, err := os.Hostname()
	if err != nil {
		return false
	}
	return strings.EqualFold(host, strings.TrimSpace(hostname))
}
