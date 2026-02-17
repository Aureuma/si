package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func cmdPaasDoctor(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas doctor", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasDoctorUsageText)
		return
	}
	checks, failureCount := runPaasDoctorChecks()
	ok := failureCount == 0
	if jsonOut {
		payload := map[string]any{
			"ok":      ok,
			"command": "doctor",
			"context": currentPaasContext(),
			"mode":    "live",
			"count":   len(checks),
			"checks":  checks,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		status := "succeeded"
		if !ok {
			status = "failed"
		}
		_ = recordPaasAuditEvent("doctor", status, "live", map[string]string{
			"check_count":   intString(len(checks)),
			"failure_count": intString(failureCount),
		}, nil)
		if !ok {
			os.Exit(1)
		}
		return
	}

	if ok {
		fmt.Printf("%s %s\n", styleHeading("paas doctor:"), styleSuccess("ok"))
	} else {
		fmt.Printf("%s %s\n", styleHeading("paas doctor:"), styleError("issues found"))
	}
	rows := make([][]string, 0, len(checks))
	for _, check := range checks {
		status := styleSuccess("OK")
		if !check.OK {
			status = styleError("ERR")
		}
		rows = append(rows, []string{status, strings.TrimSpace(check.Name), strings.TrimSpace(check.Detail)})
	}
	if len(rows) > 0 {
		printAlignedRows(rows, 2, "  ")
	}
	status := "succeeded"
	if !ok {
		status = "failed"
	}
	_ = recordPaasAuditEvent("doctor", status, "live", map[string]string{
		"check_count":   intString(len(checks)),
		"failure_count": intString(failureCount),
	}, nil)
	if !ok {
		os.Exit(1)
	}
}

func runPaasDoctorChecks() ([]doctorCheck, int) {
	checks := make([]doctorCheck, 0, 4)
	failureCount := 0
	push := func(name string, ok bool, detail string) {
		check := doctorCheck{
			Name:   strings.TrimSpace(name),
			OK:     ok,
			Detail: strings.TrimSpace(detail),
		}
		checks = append(checks, check)
		if !ok {
			failureCount++
		}
	}

	stateRoot, stateErr := resolvePaasStateRoot()
	repoRoot, repoFound := resolveGitRepoRoot()
	allowRepoState := isTruthyFlagValue(os.Getenv(paasAllowRepoStateEnvKey))
	switch {
	case stateErr != nil:
		push("state_root_resolved", false, fmt.Sprintf("failed to resolve state root: %v", stateErr))
	case !repoFound:
		push("state_root_outside_repo", true, fmt.Sprintf("repository root not detected; state root=%s", filepath.Clean(stateRoot)))
	case isPathWithin(stateRoot, repoRoot):
		detail := fmt.Sprintf("state root %q is inside repository %q", filepath.Clean(stateRoot), filepath.Clean(repoRoot))
		if allowRepoState {
			detail += fmt.Sprintf("; override %s=true is active (unsafe)", paasAllowRepoStateEnvKey)
		}
		push("state_root_outside_repo", false, detail)
	default:
		push("state_root_outside_repo", true, fmt.Sprintf("state root %q is outside repository %q", filepath.Clean(stateRoot), filepath.Clean(repoRoot)))
	}

	if !repoFound {
		push("context_vault_outside_repo", true, "repository root not detected; skipping vault path policy check")
		push("repo_private_state_artifacts", true, "repository root not detected; skipping contamination scan")
		push("repo_plaintext_secret_exposure", true, "repository root not detected; skipping secret exposure scan")
		return checks, failureCount
	}

	vaultIssues, vaultErr := findPaasContextVaultRepoPolicyIssues(repoRoot)
	switch {
	case vaultErr != nil:
		push("context_vault_outside_repo", false, fmt.Sprintf("failed to evaluate context vault mappings: %v", vaultErr))
	case len(vaultIssues) == 0:
		push("context_vault_outside_repo", true, "all context vault mappings resolve outside repository")
	default:
		push("context_vault_outside_repo", false, summarizePaasDoctorIssues("vault paths inside repository", vaultIssues))
	}

	privateStatePaths, secretExposureFindings, scanErr := scanPaasRepoStateContamination(repoRoot)
	switch {
	case scanErr != nil:
		push("repo_private_state_artifacts", false, fmt.Sprintf("failed to scan repository for private state artifacts: %v", scanErr))
		push("repo_plaintext_secret_exposure", false, fmt.Sprintf("failed to scan repository for secret exposure: %v", scanErr))
	case len(privateStatePaths) == 0:
		push("repo_private_state_artifacts", true, "no private PaaS state artifacts detected under repository")
	default:
		push("repo_private_state_artifacts", false, summarizePaasDoctorIssues("private state artifacts detected", privateStatePaths))
	}
	if scanErr == nil {
		if len(secretExposureFindings) == 0 {
			push("repo_plaintext_secret_exposure", true, "no plaintext secret exposure detected in repository PaaS state artifacts")
		} else {
			push("repo_plaintext_secret_exposure", false, summarizePaasDoctorIssues("secret exposure findings detected", secretExposureFindings))
		}
	}
	return checks, failureCount
}

func findPaasContextVaultRepoPolicyIssues(repoRoot string) ([]string, error) {
	rows, err := listPaasContextConfigs()
	if err != nil {
		return nil, err
	}
	issues := make([]string, 0)
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		vaultFile := strings.TrimSpace(row.VaultFile)
		if vaultFile == "" {
			contextDir, err := resolvePaasContextDir(name)
			if err != nil {
				continue
			}
			vaultFile = filepath.Join(contextDir, "vault", "secrets.env")
		}
		if isPathWithin(vaultFile, repoRoot) {
			issues = append(issues, fmt.Sprintf("%s:%s", name, filepath.Clean(vaultFile)))
		}
	}
	sort.Strings(issues)
	return issues, nil
}

func scanPaasRepoStateContamination(repoRoot string) ([]string, []string, error) {
	privateState := make([]string, 0)
	secrets := make([]string, 0)
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := strings.TrimSpace(d.Name())
		if d.IsDir() {
			switch strings.ToLower(name) {
			case ".git":
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return nil
		}
		if !isPaasPrivateStateArtifactPath(rel) {
			return nil
		}
		rel = normalizePaasDoctorRelPath(rel)
		privateState = append(privateState, rel)

		hasSecretPathMarkers := isPaasSecretLeakPath(rel)
		hasSecretContent := false
		if !hasSecretPathMarkers {
			hasSecretContent, err = fileContainsPaasSecretIndicators(path)
			if err != nil {
				return err
			}
		}
		if hasSecretPathMarkers || hasSecretContent {
			secrets = append(secrets, rel)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	privateState = dedupeAndSortStrings(privateState)
	secrets = dedupeAndSortStrings(secrets)
	return privateState, secrets, nil
}

func normalizePaasDoctorRelPath(path string) string {
	return filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
}

func isPaasPrivateStateArtifactPath(relPath string) bool {
	rel := strings.ToLower(normalizePaasDoctorRelPath(relPath))
	if rel == "" {
		return false
	}
	if rel == paasContextCurrentFileName {
		return true
	}
	if !strings.Contains(rel, "/contexts/") && !strings.HasPrefix(rel, "contexts/") {
		return false
	}
	suffixes := []string{
		"/config.json",
		"/targets.json",
		"/deployments.json",
		"/addons.json",
		"/bluegreen.json",
		"/webhooks/mappings.json",
		"/alerts/telegram.json",
		"/alerts/policy.json",
		"/events/deployments.jsonl",
		"/events/alerts.jsonl",
		"/events/audit.jsonl",
		"/vault/secrets.env",
		"/state.db",
	}
	for _, suffix := range suffixes {
		if strings.HasSuffix(rel, suffix) {
			return true
		}
	}
	for _, marker := range []string{"/releases/", "/cache/", "/vault/", "/events/"} {
		if strings.Contains(rel, marker) {
			return true
		}
	}
	return false
}

func isPaasSecretLeakPath(relPath string) bool {
	rel := strings.ToLower(normalizePaasDoctorRelPath(relPath))
	if rel == "" {
		return false
	}
	if strings.Contains(rel, "/vault/") {
		return true
	}
	if strings.HasSuffix(rel, "secrets.env") {
		return true
	}
	return strings.HasSuffix(rel, ".env") && (strings.Contains(rel, "/contexts/") || strings.HasPrefix(rel, "contexts/"))
}

func fileContainsPaasSecretIndicators(path string) (bool, error) {
	file, err := os.Open(path) // #nosec G304 -- repository scan path.
	if err != nil {
		return false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(strings.ToLower(line), "<redacted>") {
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
		if !isPaasPlaintextSecretValue(value) {
			continue
		}
		return true, nil
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func summarizePaasDoctorIssues(prefix string, issues []string) string {
	rows := dedupeAndSortStrings(issues)
	if len(rows) == 0 {
		return strings.TrimSpace(prefix)
	}
	const maxPreview = 5
	preview := rows
	if len(preview) > maxPreview {
		preview = preview[:maxPreview]
	}
	if len(rows) > len(preview) {
		return fmt.Sprintf("%s: %s (+%d more)", strings.TrimSpace(prefix), strings.Join(preview, ", "), len(rows)-len(preview))
	}
	return fmt.Sprintf("%s: %s", strings.TrimSpace(prefix), strings.Join(preview, ", "))
}

func dedupeAndSortStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	sort.Strings(out)
	return out
}
