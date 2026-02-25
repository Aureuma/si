package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var paasEnvAssignmentPattern = regexp.MustCompile(`^\s*(?:-\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*[:=]\s*(.+?)\s*$`)

type paasPlaintextSecretFinding struct {
	Path string
	Line int
	Key  string
}

type paasVaultGuardrailResult struct {
	File           string
	RecipientCount int
	Trusted        bool
	TrustWarning   string
}

func resolvePaasComposePlaintextFindings(composeFile string) ([]paasPlaintextSecretFinding, error) {
	path := filepath.Clean(strings.TrimSpace(composeFile))
	if path == "" {
		return nil, fmt.Errorf("compose file path is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	findings := make([]paasPlaintextSecretFinding, 0)
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		match := paasEnvAssignmentPattern.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(match[1]))
		if !isPaasSecretLikeKey(key) {
			continue
		}
		value := normalizePaasSecretCandidateValue(match[2])
		if value == "" || !isPaasPlaintextSecretValue(value) {
			continue
		}
		findings = append(findings, paasPlaintextSecretFinding{
			Path: path,
			Line: lineNo,
			Key:  key,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return findings, nil
}

func isPaasSecretLikeKey(key string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(key))
	if normalized == "" {
		return false
	}
	for _, marker := range []string{"SECRET", "TOKEN", "PASSWORD", "PASS", "API_KEY", "ACCESS_KEY", "PRIVATE_KEY", "CLIENT_SECRET", "CREDENTIAL"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func normalizePaasSecretCandidateValue(raw string) string {
	value := strings.TrimSpace(raw)
	if idx := strings.Index(value, " #"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	value = strings.Trim(value, "\"'")
	return strings.TrimSpace(value)
}

func isPaasPlaintextSecretValue(value string) bool {
	candidate := strings.TrimSpace(value)
	if candidate == "" {
		return false
	}
	lower := strings.ToLower(candidate)
	switch lower {
	case "null", "~", "{}", "[]":
		return false
	}
	if strings.HasPrefix(candidate, "${") {
		return false
	}
	if strings.HasPrefix(candidate, "$") {
		return false
	}
	if strings.HasPrefix(strings.ToUpper(candidate), "ENC[") {
		return false
	}
	return true
}

func enforcePaasPlaintextSecretGuardrail(composeFile string, allowPlaintextSecrets bool) (map[string]string, error) {
	findings, err := resolvePaasComposePlaintextFindings(composeFile)
	if err != nil {
		return nil, err
	}
	fields := map[string]string{
		"compose_secret_findings": intString(len(findings)),
	}
	if len(findings) == 0 {
		fields["compose_secret_guardrail"] = "ok"
		return fields, nil
	}
	if allowPlaintextSecrets {
		fields["compose_secret_guardrail"] = "bypassed"
		return fields, nil
	}
	var b strings.Builder
	b.WriteString("plaintext secret assignments detected in compose file; migrate values to `si paas secret set` and variable references")
	for _, finding := range findings {
		fmt.Fprintf(&b, "\n  - %s:%d (%s=<redacted>)", filepath.Clean(finding.Path), finding.Line, finding.Key)
	}
	b.WriteString("\nuse --allow-plaintext-secrets to bypass (unsafe)")
	return nil, fmt.Errorf("%s", b.String())
}

func runPaasVaultDeployGuardrail(vaultFile string, allowUntrustedVault bool) (paasVaultGuardrailResult, error) {
	_ = allowUntrustedVault
	settings := loadSettingsOrDefault()
	target, err := vaultResolveTarget(settings, resolvePaasContextVaultFile(strings.TrimSpace(vaultFile)), false)
	if err != nil {
		return paasVaultGuardrailResult{}, err
	}
	values, used, sunErr := vaultSunKVLoadRawValues(settings, target)
	if sunErr != nil {
		return paasVaultGuardrailResult{}, sunErr
	}
	if !used {
		return paasVaultGuardrailResult{}, fmt.Errorf("sun vault unavailable: run `si sun auth login --url <url> --token <token> --account <slug>`")
	}
	return paasVaultGuardrailResult{
		File:           strings.TrimSpace(target.File),
		RecipientCount: 1,
		Trusted:        true,
		TrustWarning:   fmt.Sprintf("sun-kv keys=%d", len(values)),
	}, nil
}

func enforcePaasSecretRevealGuardrail(reveal bool, allowPlaintext bool) error {
	if !reveal {
		return nil
	}
	if allowPlaintext {
		return nil
	}
	return fmt.Errorf("--reveal requires --allow-plaintext to avoid accidental secret leakage")
}
