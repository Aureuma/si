package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type paasContextMetadataSnapshot struct {
	Targets         paasTargetStore         `json:"targets"`
	DeployHistory   paasDeployHistoryStore  `json:"deploy_history"`
	WebhookMappings paasWebhookMappingStore `json:"webhook_mappings"`
}

type paasContextMetadataExport struct {
	SchemaVersion int                         `json:"schema_version"`
	ExportedAt    string                      `json:"exported_at"`
	Context       string                      `json:"context"`
	Metadata      paasContextMetadataSnapshot `json:"metadata"`
}

type paasContextMetadataSummary struct {
	Context         string `json:"context"`
	Path            string `json:"path"`
	Targets         int    `json:"targets"`
	DeployApps      int    `json:"deploy_apps"`
	WebhookMappings int    `json:"webhook_mappings"`
}

func cmdPaasContextExport(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas context export", flag.ExitOnError)
	name := fs.String("name", "", "context name")
	output := fs.String("output", "", "output file path")
	force := fs.Bool("force", false, "overwrite output file")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasContextExportUsageText)
		return
	}
	if !requirePaasValue(*output, "output", paasContextExportUsageText) {
		return
	}
	summary, err := exportPaasContextMetadata(strings.TrimSpace(*name), strings.TrimSpace(*output), *force)
	if err != nil {
		failPaasCommand("context export", jsonOut, newPaasOperationFailure(
			paasFailureUnknown,
			"context_export",
			"",
			"verify output path permissions and ensure metadata contains no secret material",
			err,
		), nil)
	}
	printPaasScaffold("context export", map[string]string{
		"context":          summary.Context,
		"path":             summary.Path,
		"targets":          intString(summary.Targets),
		"deploy_apps":      intString(summary.DeployApps),
		"webhook_mappings": intString(summary.WebhookMappings),
		"force":            boolString(*force),
	}, jsonOut)
}

func cmdPaasContextImport(args []string) {
	args, jsonOut := parsePaasJSONFlag(args)
	fs := flag.NewFlagSet("paas context import", flag.ExitOnError)
	name := fs.String("name", "", "context name")
	input := fs.String("input", "", "input file path")
	replace := fs.Bool("replace", false, "replace existing metadata")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(paasContextImportUsageText)
		return
	}
	if !requirePaasValue(*input, "input", paasContextImportUsageText) {
		return
	}
	summary, err := importPaasContextMetadata(strings.TrimSpace(*name), strings.TrimSpace(*input), *replace)
	if err != nil {
		failPaasCommand("context import", jsonOut, newPaasOperationFailure(
			paasFailureUnknown,
			"context_import",
			"",
			"verify import payload is scrubbed non-secret metadata and retry",
			err,
		), nil)
	}
	printPaasScaffold("context import", map[string]string{
		"context":          summary.Context,
		"path":             summary.Path,
		"targets":          intString(summary.Targets),
		"deploy_apps":      intString(summary.DeployApps),
		"webhook_mappings": intString(summary.WebhookMappings),
		"replace":          boolString(*replace),
	}, jsonOut)
}

func exportPaasContextMetadata(contextName, outputPath string, force bool) (paasContextMetadataSummary, error) {
	resolvedContext := resolvePaasContextName(contextName)
	targets, err := loadPaasTargetStore(resolvedContext)
	if err != nil {
		return paasContextMetadataSummary{}, err
	}
	deployHistory, err := loadPaasDeployHistoryStoreForContext(resolvedContext)
	if err != nil {
		return paasContextMetadataSummary{}, err
	}
	webhooks, err := loadPaasWebhookMappingStore(resolvedContext)
	if err != nil {
		return paasContextMetadataSummary{}, err
	}

	payload := paasContextMetadataExport{
		SchemaVersion: 1,
		ExportedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Context:       resolvedContext,
		Metadata: paasContextMetadataSnapshot{
			Targets:         targets,
			DeployHistory:   deployHistory,
			WebhookMappings: webhooks,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return paasContextMetadataSummary{}, err
	}
	if key, sensitive := detectPaasSensitiveMetadataKey(raw); sensitive {
		return paasContextMetadataSummary{}, fmt.Errorf("refusing export because key %q appears secret-like", key)
	}

	resolvedPath := filepath.Clean(outputPath)
	if _, err := os.Stat(resolvedPath); err == nil && !force {
		return paasContextMetadataSummary{}, fmt.Errorf("output already exists: %s (pass --force to overwrite)", resolvedPath)
	}
	if err := os.MkdirAll(filepath.Dir(resolvedPath), 0o700); err != nil {
		return paasContextMetadataSummary{}, err
	}
	pretty, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return paasContextMetadataSummary{}, err
	}
	pretty = append(pretty, '\n')
	if err := os.WriteFile(resolvedPath, pretty, 0o600); err != nil {
		return paasContextMetadataSummary{}, err
	}
	return paasContextMetadataSummary{
		Context:         resolvedContext,
		Path:            resolvedPath,
		Targets:         len(targets.Targets),
		DeployApps:      len(deployHistory.Apps),
		WebhookMappings: len(webhooks.Mappings),
	}, nil
}

func importPaasContextMetadata(contextName, inputPath string, replace bool) (paasContextMetadataSummary, error) {
	resolvedPath := filepath.Clean(inputPath)
	raw, err := os.ReadFile(resolvedPath) // #nosec G304 -- operator provided local import path.
	if err != nil {
		return paasContextMetadataSummary{}, err
	}
	if key, sensitive := detectPaasSensitiveMetadataKey(raw); sensitive {
		return paasContextMetadataSummary{}, fmt.Errorf("refusing import because key %q appears secret-like", key)
	}
	var payload paasContextMetadataExport
	if err := json.Unmarshal(raw, &payload); err != nil {
		return paasContextMetadataSummary{}, fmt.Errorf("invalid metadata import payload: %w", err)
	}
	if payload.SchemaVersion != 1 {
		return paasContextMetadataSummary{}, fmt.Errorf("unsupported schema_version %d", payload.SchemaVersion)
	}

	resolvedContext := resolvePaasContextName(contextName)
	if strings.TrimSpace(contextName) == "" && strings.TrimSpace(payload.Context) != "" {
		resolvedContext = resolvePaasContextName(payload.Context)
	}

	targetStore := payload.Metadata.Targets
	deployStore := payload.Metadata.DeployHistory
	webhookStore := payload.Metadata.WebhookMappings
	if !replace {
		existingTargets, err := loadPaasTargetStore(resolvedContext)
		if err != nil {
			return paasContextMetadataSummary{}, err
		}
		targetStore = mergePaasTargetStore(existingTargets, targetStore)

		existingDeploys, err := loadPaasDeployHistoryStoreForContext(resolvedContext)
		if err != nil {
			return paasContextMetadataSummary{}, err
		}
		deployStore = mergePaasDeployHistoryStore(existingDeploys, deployStore)

		existingWebhooks, err := loadPaasWebhookMappingStore(resolvedContext)
		if err != nil {
			return paasContextMetadataSummary{}, err
		}
		webhookStore = mergePaasWebhookMappingStore(existingWebhooks, webhookStore)
	}

	if err := savePaasTargetStore(resolvedContext, targetStore); err != nil {
		return paasContextMetadataSummary{}, err
	}
	if err := savePaasDeployHistoryStoreForContext(resolvedContext, deployStore); err != nil {
		return paasContextMetadataSummary{}, err
	}
	if err := savePaasWebhookMappingStore(resolvedContext, webhookStore); err != nil {
		return paasContextMetadataSummary{}, err
	}
	return paasContextMetadataSummary{
		Context:         resolvedContext,
		Path:            resolvedPath,
		Targets:         len(targetStore.Targets),
		DeployApps:      len(deployStore.Apps),
		WebhookMappings: len(webhookStore.Mappings),
	}, nil
}

func mergePaasTargetStore(base, incoming paasTargetStore) paasTargetStore {
	out := paasTargetStore{
		CurrentTarget: strings.TrimSpace(base.CurrentTarget),
		Targets:       append([]paasTarget(nil), base.Targets...),
	}
	for _, row := range incoming.Targets {
		normalized := normalizePaasTarget(row)
		if strings.TrimSpace(normalized.Name) == "" {
			continue
		}
		idx := findPaasTarget(out, normalized.Name)
		if idx >= 0 {
			out.Targets[idx] = normalized
		} else {
			out.Targets = append(out.Targets, normalized)
		}
	}
	if current := strings.TrimSpace(incoming.CurrentTarget); current != "" && findPaasTarget(out, current) >= 0 {
		out.CurrentTarget = current
	}
	return out
}

func mergePaasDeployHistoryStore(base, incoming paasDeployHistoryStore) paasDeployHistoryStore {
	out := paasDeployHistoryStore{Apps: map[string]paasAppDeployHistory{}}
	for key, row := range base.Apps {
		out.Apps[key] = row
	}
	for app, row := range incoming.Apps {
		key := sanitizePaasReleasePathSegment(app)
		current := out.Apps[key]
		if strings.TrimSpace(row.CurrentRelease) != "" {
			current.CurrentRelease = strings.TrimSpace(row.CurrentRelease)
		}
		for _, release := range row.History {
			current.History = appendUniquePaasRelease(current.History, release)
		}
		if strings.TrimSpace(current.CurrentRelease) == "" && len(current.History) > 0 {
			current.CurrentRelease = current.History[len(current.History)-1]
		}
		if strings.TrimSpace(row.UpdatedAt) != "" {
			current.UpdatedAt = strings.TrimSpace(row.UpdatedAt)
		}
		out.Apps[key] = current
	}
	return out
}

func mergePaasWebhookMappingStore(base, incoming paasWebhookMappingStore) paasWebhookMappingStore {
	out := paasWebhookMappingStore{
		Mappings: append([]paasWebhookMapping(nil), base.Mappings...),
	}
	for _, row := range incoming.Mappings {
		normalized, ok := normalizePaasWebhookMappingRow(row)
		if !ok {
			continue
		}
		idx := findPaasWebhookMapping(out, normalized.Provider, normalized.Repo, normalized.Branch)
		if idx >= 0 {
			out.Mappings[idx] = normalized
			continue
		}
		out.Mappings = append(out.Mappings, normalized)
	}
	return out
}

func resolvePaasContextName(name string) string {
	resolved := strings.TrimSpace(name)
	if resolved == "" {
		return currentPaasContext()
	}
	return resolved
}

func detectPaasSensitiveMetadataKey(raw []byte) (string, bool) {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", false
	}
	needle := []string{"secret", "password", "token", "credential", "private_key", "ssh_key"}
	return detectSensitiveKeyRecursive(payload, "", needle)
}

func detectSensitiveKeyRecursive(value any, prefix string, needles []string) (string, bool) {
	switch row := value.(type) {
	case map[string]any:
		for key, child := range row {
			lower := strings.ToLower(strings.TrimSpace(key))
			for _, needle := range needles {
				if strings.Contains(lower, needle) {
					path := key
					if prefix != "" {
						path = prefix + "." + key
					}
					return path, true
				}
			}
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			if nestedKey, found := detectSensitiveKeyRecursive(child, path, needles); found {
				return nestedKey, true
			}
		}
	case []any:
		for index, child := range row {
			path := fmt.Sprintf("%s[%d]", prefix, index)
			if nestedKey, found := detectSensitiveKeyRecursive(child, path, needles); found {
				return nestedKey, true
			}
		}
	}
	return "", false
}
