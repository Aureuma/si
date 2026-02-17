package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const paasAllowRepoStateEnvKey = "SI_PAAS_ALLOW_REPO_STATE"

func enforcePaasStateRootIsolationGuardrail() error {
	if allow := strings.ToLower(strings.TrimSpace(os.Getenv(paasAllowRepoStateEnvKey))); allow == "1" || allow == "true" || allow == "yes" || allow == "on" {
		return nil
	}
	stateRoot, err := resolvePaasStateRoot()
	if err != nil {
		return nil
	}
	repoRoot, ok := resolveGitRepoRoot()
	if !ok {
		return nil
	}
	cleanState := filepath.Clean(stateRoot)
	cleanRepo := filepath.Clean(repoRoot)
	if !isPathWithin(cleanState, cleanRepo) {
		return nil
	}
	return fmt.Errorf("unsafe state root %q is inside repository %q; move SI_PAAS_STATE_ROOT outside repo or set %s=true to override (unsafe)", cleanState, cleanRepo, paasAllowRepoStateEnvKey)
}

func resolveGitRepoRoot() (string, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	current := filepath.Clean(cwd)
	for {
		gitPath := filepath.Join(current, ".git")
		if info, err := os.Stat(gitPath); err == nil && (info.IsDir() || info.Mode().IsRegular()) {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return "", false
}

func isPathWithin(path, root string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	rel = filepath.Clean(rel)
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func redactPaasSensitiveFields(fields map[string]string) map[string]string {
	if len(fields) == 0 {
		return fields
	}
	out := make(map[string]string, len(fields))
	for key, value := range fields {
		normalized := strings.ToLower(strings.TrimSpace(key))
		switch {
		case isPaasSensitiveFieldKey(normalized):
			out[key] = "<redacted>"
		default:
			out[key] = value
		}
	}
	return out
}

func isPaasSensitiveFieldKey(key string) bool {
	if key == "" {
		return false
	}
	if strings.Contains(key, "compose_secret_guardrail") || strings.Contains(key, "compose_secret_findings") {
		return false
	}
	if strings.Contains(key, "count") || strings.Contains(key, "findings") || strings.Contains(key, "guardrail") {
		return false
	}
	for _, marker := range []string{"secret", "token", "password", "credential", "private_key", "api_key"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}
