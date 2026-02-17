package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type paasReleaseBundleMetadata struct {
	SchemaVersion int               `json:"schema_version"`
	ReleaseID     string            `json:"release_id"`
	CreatedAt     string            `json:"created_at"`
	Context       string            `json:"context"`
	App           string            `json:"app"`
	ComposeFile   string            `json:"compose_file"`
	ComposeSHA256 string            `json:"compose_sha256"`
	Strategy      string            `json:"strategy"`
	Targets       []string          `json:"targets,omitempty"`
	Guardrails    map[string]string `json:"guardrails,omitempty"`
}

func ensurePaasReleaseBundle(app, releaseID, composeFile, bundleRoot, strategy string, targets []string, guardrails map[string]string) (string, string, error) {
	composePath := filepath.Clean(strings.TrimSpace(composeFile))
	if composePath == "" {
		return "", "", fmt.Errorf("compose file path is required")
	}
	rawCompose, err := os.ReadFile(composePath) // #nosec G304 -- local CLI operator input path.
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(rawCompose)

	resolvedRelease := strings.TrimSpace(releaseID)
	if resolvedRelease == "" {
		resolvedRelease = generatePaasReleaseID()
	}
	resolvedApp := strings.TrimSpace(app)
	if resolvedApp == "" {
		resolvedApp = "default-app"
	}

	root, err := resolvePaasReleaseBundleRoot(bundleRoot)
	if err != nil {
		return "", "", err
	}
	bundleDir := filepath.Join(root, sanitizePaasReleasePathSegment(resolvedApp), sanitizePaasReleasePathSegment(resolvedRelease))
	if err := os.MkdirAll(bundleDir, 0o700); err != nil {
		return "", "", err
	}
	bundleComposePath := filepath.Join(bundleDir, "compose.yaml")
	if err := os.WriteFile(bundleComposePath, rawCompose, 0o600); err != nil {
		return "", "", err
	}

	meta := paasReleaseBundleMetadata{
		SchemaVersion: 1,
		ReleaseID:     resolvedRelease,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		Context:       currentPaasContext(),
		App:           resolvedApp,
		ComposeFile:   composePath,
		ComposeSHA256: hex.EncodeToString(sum[:]),
		Strategy:      strings.TrimSpace(strategy),
		Targets:       append([]string(nil), targets...),
		Guardrails:    copyPaasFields(guardrails),
	}
	metaPath := filepath.Join(bundleDir, "release.json")
	enc, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", "", err
	}
	enc = append(enc, '\n')
	if err := os.WriteFile(metaPath, enc, 0o600); err != nil {
		return "", "", err
	}
	return bundleDir, metaPath, nil
}

func resolvePaasReleaseBundleRoot(assigned string) (string, error) {
	if candidate := strings.TrimSpace(assigned); candidate != "" {
		return filepath.Clean(candidate), nil
	}
	contextDir, err := resolvePaasContextDir(currentPaasContext())
	if err != nil {
		return "", err
	}
	return filepath.Join(contextDir, "releases"), nil
}

func sanitizePaasReleasePathSegment(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
			}
			lastDash = true
		}
	}
	out := strings.Trim(strings.TrimSpace(b.String()), "-._")
	if out == "" {
		return "unknown"
	}
	return out
}

func generatePaasReleaseID() string {
	return "rel-" + time.Now().UTC().Format("20060102T150405")
}

func copyPaasFields(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
