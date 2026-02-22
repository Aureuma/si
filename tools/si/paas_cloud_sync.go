package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	paasSyncBackendGit   = "git"
	paasSyncBackendHelia = "helia"
	paasSyncBackendDual  = "dual"

	paasSyncBackendEnvKey  = "SI_PAAS_SYNC_BACKEND"
	paasSnapshotNameEnvKey = "SI_PAAS_SNAPSHOT_NAME"
)

type paasSyncBackendResolution struct {
	Mode   string
	Source string
}

type paasControlPlaneSnapshot struct {
	SchemaVersion   int                      `json:"schema_version"`
	GeneratedAt     string                   `json:"generated_at"`
	Context         string                   `json:"context"`
	ContextConfig   paasContextConfig        `json:"context_config"`
	Targets         paasTargetStore          `json:"targets"`
	DeployHistory   paasDeployHistoryStore   `json:"deploy_history"`
	WebhookMappings paasWebhookMappingStore  `json:"webhook_mappings"`
	Addons          paasAddonStore           `json:"addons"`
	BlueGreenPolicy paasBlueGreenPolicyStore `json:"bluegreen_policy"`
	Agents          paasAgentStore           `json:"agents"`
	AgentApprovals  paasAgentApprovalStore   `json:"agent_approvals"`
	IncidentQueue   []paasIncidentQueueEntry `json:"incident_queue"`
}

type paasCloudSyncSummary struct {
	Context         string `json:"context"`
	ObjectName      string `json:"object_name"`
	Revision        int64  `json:"revision,omitempty"`
	Targets         int    `json:"targets"`
	DeployApps      int    `json:"deploy_apps"`
	WebhookMappings int    `json:"webhook_mappings"`
	AddonApps       int    `json:"addon_apps"`
	BlueGreenApps   int    `json:"bluegreen_apps"`
	Agents          int    `json:"agents"`
	Approvals       int    `json:"approvals"`
	Incidents       int    `json:"incidents"`
	GeneratedAt     string `json:"generated_at"`
}

func normalizePaasSyncBackend(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "git", "local":
		return paasSyncBackendGit
	case "sun", "helia", "cloud":
		return paasSyncBackendHelia
	case "dual", "both":
		return paasSyncBackendDual
	default:
		return ""
	}
}

func resolvePaasSyncBackend(settings Settings) (paasSyncBackendResolution, error) {
	if envRaw := strings.TrimSpace(os.Getenv(paasSyncBackendEnvKey)); envRaw != "" {
		mode := normalizePaasSyncBackend(envRaw)
		if mode == "" {
			return paasSyncBackendResolution{}, fmt.Errorf("invalid %s %q (expected git, sun, helia, or dual)", paasSyncBackendEnvKey, envRaw)
		}
		return paasSyncBackendResolution{Mode: mode, Source: "env"}, nil
	}
	if cfgRaw := strings.TrimSpace(settings.Paas.SyncBackend); cfgRaw != "" {
		mode := normalizePaasSyncBackend(cfgRaw)
		if mode == "" {
			return paasSyncBackendResolution{}, fmt.Errorf("invalid paas.sync_backend %q (expected git, sun, helia, or dual)", cfgRaw)
		}
		return paasSyncBackendResolution{Mode: mode, Source: "settings"}, nil
	}
	if settings.Helia.AutoSync {
		return paasSyncBackendResolution{Mode: paasSyncBackendDual, Source: "legacy_helia_auto_sync"}, nil
	}
	return paasSyncBackendResolution{Mode: paasSyncBackendGit, Source: "default"}, nil
}

func resolvePaasSnapshotObjectName(settings Settings, contextName string) string {
	if fromEnv := strings.TrimSpace(os.Getenv(paasSnapshotNameEnvKey)); fromEnv != "" {
		return sanitizePaasReleasePathSegment(fromEnv)
	}
	if fromSettings := strings.TrimSpace(settings.Paas.SnapshotName); fromSettings != "" {
		return sanitizePaasReleasePathSegment(fromSettings)
	}
	resolvedContext := resolvePaasContextName(contextName)
	return sanitizePaasReleasePathSegment(resolvedContext)
}

func isPaasCloudMutationCommand(command string) bool {
	tokens := strings.Fields(strings.ToLower(strings.TrimSpace(command)))
	if len(tokens) == 0 {
		return false
	}
	if tokens[0] == "cloud" {
		return false
	}
	sub := ""
	if len(tokens) > 1 {
		sub = tokens[1]
	}
	sub2 := ""
	if len(tokens) > 2 {
		sub2 = tokens[2]
	}
	sub3 := ""
	if len(tokens) > 3 {
		sub3 = tokens[3]
	}
	switch tokens[0] {
	case "target":
		return sub != "list" && sub != "check"
	case "app":
		if sub == "list" || sub == "status" {
			return false
		}
		if sub == "addon" && (sub2 == "list" || sub2 == "contract") {
			return false
		}
		return true
	case "deploy":
		if sub == "webhook" && sub2 == "map" && sub3 == "list" {
			return false
		}
		return true
	case "rollback":
		return true
	case "logs":
		return false
	case "alert":
		if sub == "history" {
			return false
		}
		if sub == "policy" && sub2 == "show" {
			return false
		}
		return true
	case "secret":
		return sub != "get" && sub != "list"
	case "ai":
		return false
	case "context":
		return sub != "list" && sub != "show" && sub != "export"
	case "doctor":
		return false
	case "agent":
		return sub != "status" && sub != "logs"
	case "events":
		return false
	case "backup":
		return sub != "status" && sub != "contract"
	case "taskboard":
		return sub != "show" && sub != "list"
	default:
		return false
	}
}

func maybeHeliaAutoSyncPaasControlPlane(command string) error {
	if !isPaasCloudMutationCommand(command) {
		return nil
	}
	settings := loadSettingsOrDefault()
	resolution, err := resolvePaasSyncBackend(settings)
	if err != nil {
		return err
	}
	if resolution.Mode == paasSyncBackendGit {
		return nil
	}
	requireHelia := resolution.Mode == paasSyncBackendHelia
	summary, err := pushPaasControlPlaneSnapshotToHelia(currentPaasContext(), "", nil)
	if err != nil {
		if requireHelia {
			return fmt.Errorf("sun paas auto-sync failed (%s): %w", strings.TrimSpace(command), err)
		}
		warnf("sun paas auto-sync skipped (%s): %v", strings.TrimSpace(command), err)
		return nil
	}
	_ = summary
	return nil
}

func pushPaasControlPlaneSnapshotToHelia(contextName string, explicitObjectName string, expectedRevision *int64) (paasCloudSyncSummary, error) {
	settings := loadSettingsOrDefault()
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		return paasCloudSyncSummary{}, err
	}
	snapshot, err := buildPaasControlPlaneSnapshot(contextName)
	if err != nil {
		return paasCloudSyncSummary{}, err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return paasCloudSyncSummary{}, err
	}
	if key, sensitive := detectPaasSensitiveMetadataKey(payload); sensitive {
		return paasCloudSyncSummary{}, fmt.Errorf("refusing cloud push because key %q appears secret-like", key)
	}
	objectName := strings.TrimSpace(explicitObjectName)
	if objectName == "" {
		objectName = resolvePaasSnapshotObjectName(settings, snapshot.Context)
	}
	ctx, cancel := context.WithTimeout(heliaContext(settings), 20*time.Second)
	defer cancel()
	put, err := client.putObject(ctx, heliaPaasControlPlaneSnapshotKind, objectName, payload, "application/json", map[string]interface{}{
		"context":        snapshot.Context,
		"schema_version": snapshot.SchemaVersion,
		"generated_at":   snapshot.GeneratedAt,
	}, expectedRevision)
	if err != nil {
		return paasCloudSyncSummary{}, err
	}
	return summarizePaasCloudSnapshot(snapshot, objectName, put.Result.Revision.Revision), nil
}

func pullPaasControlPlaneSnapshotFromHelia(contextName string, explicitObjectName string, replace bool) (paasCloudSyncSummary, error) {
	settings := loadSettingsOrDefault()
	client, err := heliaClientFromSettings(settings)
	if err != nil {
		return paasCloudSyncSummary{}, err
	}
	resolvedContext := resolvePaasContextName(contextName)
	objectName := strings.TrimSpace(explicitObjectName)
	if objectName == "" {
		objectName = resolvePaasSnapshotObjectName(settings, resolvedContext)
	}
	ctx, cancel := context.WithTimeout(heliaContext(settings), 20*time.Second)
	defer cancel()
	payload, err := client.getPayload(ctx, heliaPaasControlPlaneSnapshotKind, objectName)
	if err != nil {
		return paasCloudSyncSummary{}, err
	}
	if key, sensitive := detectPaasSensitiveMetadataKey(payload); sensitive {
		return paasCloudSyncSummary{}, fmt.Errorf("refusing cloud pull because key %q appears secret-like", key)
	}
	var snapshot paasControlPlaneSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return paasCloudSyncSummary{}, fmt.Errorf("invalid paas cloud snapshot payload: %w", err)
	}
	if snapshot.SchemaVersion != 1 {
		return paasCloudSyncSummary{}, fmt.Errorf("unsupported paas cloud snapshot schema_version %d", snapshot.SchemaVersion)
	}
	summary, err := applyPaasControlPlaneSnapshot(resolvedContext, snapshot, replace)
	if err != nil {
		return paasCloudSyncSummary{}, err
	}
	summary.ObjectName = objectName
	return summary, nil
}

func buildPaasControlPlaneSnapshot(contextName string) (paasControlPlaneSnapshot, error) {
	resolvedContext := resolvePaasContextName(contextName)
	contextConfig, err := loadPaasContextConfig(resolvedContext)
	if err != nil {
		if !os.IsNotExist(err) {
			return paasControlPlaneSnapshot{}, err
		}
		contextConfig, err = defaultPaasContextConfig(resolvedContext)
		if err != nil {
			return paasControlPlaneSnapshot{}, err
		}
	}
	targets, err := loadPaasTargetStore(resolvedContext)
	if err != nil {
		return paasControlPlaneSnapshot{}, err
	}
	deployHistory, err := loadPaasDeployHistoryStoreForContext(resolvedContext)
	if err != nil {
		return paasControlPlaneSnapshot{}, err
	}
	webhooks, err := loadPaasWebhookMappingStore(resolvedContext)
	if err != nil {
		return paasControlPlaneSnapshot{}, err
	}
	addons, err := loadPaasAddonStoreForContext(resolvedContext)
	if err != nil {
		return paasControlPlaneSnapshot{}, err
	}
	blueGreenPolicy, err := loadPaasBlueGreenPolicyStoreForContext(resolvedContext)
	if err != nil {
		return paasControlPlaneSnapshot{}, err
	}
	agents, _, err := loadPaasAgentStore(resolvedContext)
	if err != nil {
		return paasControlPlaneSnapshot{}, err
	}
	agentApprovals, _, err := loadPaasAgentApprovalStore(resolvedContext)
	if err != nil {
		return paasControlPlaneSnapshot{}, err
	}
	incidentQueue, _, err := loadPaasIncidentQueueEntries(resolvedContext)
	if err != nil {
		return paasControlPlaneSnapshot{}, err
	}
	return paasControlPlaneSnapshot{
		SchemaVersion:   1,
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		Context:         resolvedContext,
		ContextConfig:   normalizePaasSnapshotContextConfig(contextConfig, resolvedContext),
		Targets:         targets,
		DeployHistory:   deployHistory,
		WebhookMappings: webhooks,
		Addons:          addons,
		BlueGreenPolicy: blueGreenPolicy,
		Agents:          agents,
		AgentApprovals:  agentApprovals,
		IncidentQueue:   incidentQueue,
	}, nil
}

func applyPaasControlPlaneSnapshot(contextName string, snapshot paasControlPlaneSnapshot, replace bool) (paasCloudSyncSummary, error) {
	resolvedContext := resolvePaasContextName(contextName)
	if strings.TrimSpace(contextName) == "" && strings.TrimSpace(snapshot.Context) != "" {
		resolvedContext = resolvePaasContextName(snapshot.Context)
	}
	incomingConfig := normalizePaasSnapshotContextConfig(snapshot.ContextConfig, resolvedContext)
	existingConfig, existingErr := loadPaasContextConfig(resolvedContext)
	if existingErr != nil && !os.IsNotExist(existingErr) {
		return paasCloudSyncSummary{}, existingErr
	}
	if _, err := initializePaasContextLayout(resolvedContext, incomingConfig.Type, incomingConfig.StateRoot, incomingConfig.VaultFile); err != nil {
		return paasCloudSyncSummary{}, err
	}
	if existingErr == nil {
		incomingConfig.CreatedAt = firstNonEmptyString(existingConfig.CreatedAt, incomingConfig.CreatedAt)
		// Keep machine-local filesystem settings stable across pulls.
		if strings.TrimSpace(existingConfig.StateRoot) != "" {
			incomingConfig.StateRoot = strings.TrimSpace(existingConfig.StateRoot)
		}
		if strings.TrimSpace(existingConfig.VaultFile) != "" {
			incomingConfig.VaultFile = strings.TrimSpace(existingConfig.VaultFile)
		}
		if !replace && strings.TrimSpace(existingConfig.Type) != "" {
			incomingConfig.Type = strings.TrimSpace(existingConfig.Type)
		}
	}
	incomingConfig.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := savePaasContextConfig(incomingConfig); err != nil {
		return paasCloudSyncSummary{}, err
	}

	targetStore := snapshot.Targets
	deployStore := snapshot.DeployHistory
	webhookStore := snapshot.WebhookMappings
	addonStore := snapshot.Addons
	blueGreenStore := snapshot.BlueGreenPolicy
	agentStore := snapshot.Agents
	approvalStore := snapshot.AgentApprovals
	incidentQueue := snapshot.IncidentQueue

	if !replace {
		existingTargets, err := loadPaasTargetStore(resolvedContext)
		if err != nil {
			return paasCloudSyncSummary{}, err
		}
		targetStore = mergePaasTargetStore(existingTargets, targetStore)

		existingDeploys, err := loadPaasDeployHistoryStoreForContext(resolvedContext)
		if err != nil {
			return paasCloudSyncSummary{}, err
		}
		deployStore = mergePaasDeployHistoryStore(existingDeploys, deployStore)

		existingWebhooks, err := loadPaasWebhookMappingStore(resolvedContext)
		if err != nil {
			return paasCloudSyncSummary{}, err
		}
		webhookStore = mergePaasWebhookMappingStore(existingWebhooks, webhookStore)

		existingAddons, err := loadPaasAddonStoreForContext(resolvedContext)
		if err != nil {
			return paasCloudSyncSummary{}, err
		}
		addonStore = mergePaasAddonStore(existingAddons, addonStore)

		existingBlueGreen, err := loadPaasBlueGreenPolicyStoreForContext(resolvedContext)
		if err != nil {
			return paasCloudSyncSummary{}, err
		}
		blueGreenStore = mergePaasBlueGreenPolicyStore(existingBlueGreen, blueGreenStore)

		existingAgents, _, err := loadPaasAgentStore(resolvedContext)
		if err != nil {
			return paasCloudSyncSummary{}, err
		}
		agentStore = mergePaasAgentStore(existingAgents, agentStore)

		existingApprovals, _, err := loadPaasAgentApprovalStore(resolvedContext)
		if err != nil {
			return paasCloudSyncSummary{}, err
		}
		approvalStore = mergePaasAgentApprovalStore(existingApprovals, approvalStore)

		existingIncidents, _, err := loadPaasIncidentQueueEntries(resolvedContext)
		if err != nil {
			return paasCloudSyncSummary{}, err
		}
		incidentQueue = mergePaasIncidentQueueEntries(existingIncidents, incidentQueue)
	}

	if err := savePaasTargetStore(resolvedContext, targetStore); err != nil {
		return paasCloudSyncSummary{}, err
	}
	if err := savePaasDeployHistoryStoreForContext(resolvedContext, deployStore); err != nil {
		return paasCloudSyncSummary{}, err
	}
	if err := savePaasWebhookMappingStore(resolvedContext, webhookStore); err != nil {
		return paasCloudSyncSummary{}, err
	}
	if err := savePaasAddonStoreForContext(resolvedContext, addonStore); err != nil {
		return paasCloudSyncSummary{}, err
	}
	if err := savePaasBlueGreenPolicyStoreForContext(resolvedContext, blueGreenStore); err != nil {
		return paasCloudSyncSummary{}, err
	}
	if _, err := savePaasAgentStore(resolvedContext, agentStore); err != nil {
		return paasCloudSyncSummary{}, err
	}
	if _, err := savePaasAgentApprovalStore(resolvedContext, approvalStore); err != nil {
		return paasCloudSyncSummary{}, err
	}
	if _, err := savePaasIncidentQueueEntries(resolvedContext, incidentQueue); err != nil {
		return paasCloudSyncSummary{}, err
	}

	return summarizePaasCloudSnapshot(paasControlPlaneSnapshot{
		Context:         resolvedContext,
		GeneratedAt:     snapshot.GeneratedAt,
		Targets:         targetStore,
		DeployHistory:   deployStore,
		WebhookMappings: webhookStore,
		Addons:          addonStore,
		BlueGreenPolicy: blueGreenStore,
		Agents:          agentStore,
		AgentApprovals:  approvalStore,
		IncidentQueue:   incidentQueue,
	}, "", 0), nil
}

func summarizePaasCloudSnapshot(snapshot paasControlPlaneSnapshot, objectName string, revision int64) paasCloudSyncSummary {
	return paasCloudSyncSummary{
		Context:         strings.TrimSpace(snapshot.Context),
		ObjectName:      strings.TrimSpace(objectName),
		Revision:        revision,
		Targets:         len(snapshot.Targets.Targets),
		DeployApps:      len(snapshot.DeployHistory.Apps),
		WebhookMappings: len(snapshot.WebhookMappings.Mappings),
		AddonApps:       len(snapshot.Addons.Apps),
		BlueGreenApps:   len(snapshot.BlueGreenPolicy.Apps),
		Agents:          len(snapshot.Agents.Agents),
		Approvals:       len(snapshot.AgentApprovals.Decisions),
		Incidents:       len(snapshot.IncidentQueue),
		GeneratedAt:     strings.TrimSpace(snapshot.GeneratedAt),
	}
}

func defaultPaasContextConfig(contextName string) (paasContextConfig, error) {
	resolvedContext := resolvePaasContextName(contextName)
	root, err := resolvePaasStateRoot()
	if err != nil {
		return paasContextConfig{}, err
	}
	contextDir, err := resolvePaasContextDir(resolvedContext)
	if err != nil {
		return paasContextConfig{}, err
	}
	return paasContextConfig{
		Name:      resolvedContext,
		Type:      "internal-dogfood",
		StateRoot: root,
		VaultFile: filepath.Join(contextDir, "vault", "secrets.env"),
		CreatedAt: "",
		UpdatedAt: "",
	}, nil
}

func normalizePaasSnapshotContextConfig(in paasContextConfig, contextName string) paasContextConfig {
	out := in
	out.Name = resolvePaasContextName(firstNonEmptyString(strings.TrimSpace(contextName), strings.TrimSpace(in.Name)))
	if strings.TrimSpace(out.Type) == "" {
		out.Type = "internal-dogfood"
	}
	if strings.TrimSpace(out.StateRoot) == "" {
		if root, err := resolvePaasStateRoot(); err == nil {
			out.StateRoot = root
		}
	}
	if strings.TrimSpace(out.VaultFile) == "" {
		if contextDir, err := resolvePaasContextDir(out.Name); err == nil {
			out.VaultFile = filepath.Join(contextDir, "vault", "secrets.env")
		}
	}
	out.CreatedAt = strings.TrimSpace(out.CreatedAt)
	out.UpdatedAt = strings.TrimSpace(out.UpdatedAt)
	return out
}

func mergePaasAddonStore(base, incoming paasAddonStore) paasAddonStore {
	out := paasAddonStore{Apps: map[string][]paasAddonRecord{}}
	for app, rows := range base.Apps {
		appKey := sanitizePaasReleasePathSegment(app)
		for _, row := range rows {
			out.Apps[appKey] = upsertPaasAddonRecord(out.Apps[appKey], normalizePaasAddonRecord(appKey, row))
		}
	}
	for app, rows := range incoming.Apps {
		appKey := sanitizePaasReleasePathSegment(app)
		for _, row := range rows {
			out.Apps[appKey] = upsertPaasAddonRecord(out.Apps[appKey], normalizePaasAddonRecord(appKey, row))
		}
	}
	return out
}

func mergePaasBlueGreenPolicyStore(base, incoming paasBlueGreenPolicyStore) paasBlueGreenPolicyStore {
	out := paasBlueGreenPolicyStore{Apps: map[string]paasBlueGreenAppPolicy{}}
	for app, policy := range base.Apps {
		appKey := sanitizePaasReleasePathSegment(app)
		normalized := paasBlueGreenAppPolicy{Targets: map[string]paasBlueGreenTargetPolicy{}}
		for target, targetPolicy := range policy.Targets {
			normalized.Targets[sanitizePaasReleasePathSegment(target)] = normalizePaasBlueGreenTargetPolicy(targetPolicy)
		}
		out.Apps[appKey] = normalized
	}
	for app, policy := range incoming.Apps {
		appKey := sanitizePaasReleasePathSegment(app)
		existing := out.Apps[appKey]
		if existing.Targets == nil {
			existing.Targets = map[string]paasBlueGreenTargetPolicy{}
		}
		for target, targetPolicy := range policy.Targets {
			targetKey := sanitizePaasReleasePathSegment(target)
			existing.Targets[targetKey] = normalizePaasBlueGreenTargetPolicy(targetPolicy)
		}
		out.Apps[appKey] = existing
	}
	return out
}

func mergePaasAgentStore(base, incoming paasAgentStore) paasAgentStore {
	byName := map[string]paasAgentConfig{}
	for _, row := range base.Agents {
		normalized := normalizePaasAgentConfig(row)
		key := strings.ToLower(strings.TrimSpace(normalized.Name))
		if key == "" {
			continue
		}
		byName[key] = normalized
	}
	for _, row := range incoming.Agents {
		normalized := normalizePaasAgentConfig(row)
		key := strings.ToLower(strings.TrimSpace(normalized.Name))
		if key == "" {
			continue
		}
		byName[key] = normalized
	}
	out := paasAgentStore{Agents: make([]paasAgentConfig, 0, len(byName))}
	for _, row := range byName {
		out.Agents = append(out.Agents, row)
	}
	sortPaasAgentConfigs(out.Agents)
	return out
}

func mergePaasAgentApprovalStore(base, incoming paasAgentApprovalStore) paasAgentApprovalStore {
	byRunID := map[string]paasAgentApprovalDecision{}
	for _, row := range base.Decisions {
		runID := strings.TrimSpace(row.RunID)
		if runID == "" {
			continue
		}
		byRunID[runID] = row
	}
	for _, row := range incoming.Decisions {
		runID := strings.TrimSpace(row.RunID)
		if runID == "" {
			continue
		}
		byRunID[runID] = row
	}
	out := paasAgentApprovalStore{Decisions: make([]paasAgentApprovalDecision, 0, len(byRunID))}
	for _, row := range byRunID {
		out.Decisions = append(out.Decisions, row)
	}
	sortPaasAgentApprovalDecisions(out.Decisions)
	return out
}

func mergePaasIncidentQueueEntries(base, incoming []paasIncidentQueueEntry) []paasIncidentQueueEntry {
	merged := map[string]paasIncidentQueueEntry{}
	for _, row := range base {
		normalized, ok := normalizePaasIncidentQueueEntry(row)
		if !ok {
			continue
		}
		merged[normalized.Key] = normalized
	}
	for _, row := range incoming {
		normalized, ok := normalizePaasIncidentQueueEntry(row)
		if !ok {
			continue
		}
		existing, exists := merged[normalized.Key]
		if !exists {
			merged[normalized.Key] = normalized
			continue
		}
		existingLast := parsePaasIncidentQueueTimestamp(existing.LastSeen)
		incomingLast := parsePaasIncidentQueueTimestamp(normalized.LastSeen)
		if incomingLast.After(existingLast) {
			existing.Incident = normalized.Incident
			existing.Status = normalized.Status
			existing.LastSeen = normalized.LastSeen
		}
		existingFirst := parsePaasIncidentQueueTimestamp(existing.FirstSeen)
		incomingFirst := parsePaasIncidentQueueTimestamp(normalized.FirstSeen)
		if existingFirst.IsZero() || (!incomingFirst.IsZero() && incomingFirst.Before(existingFirst)) {
			existing.FirstSeen = normalized.FirstSeen
		}
		if normalized.SeenCount > existing.SeenCount {
			existing.SeenCount = normalized.SeenCount
		}
		if existing.SeenCount < 1 {
			existing.SeenCount = 1
		}
		merged[normalized.Key] = existing
	}
	out := make([]paasIncidentQueueEntry, 0, len(merged))
	for _, row := range merged {
		out = append(out, row)
	}
	sortPaasIncidentQueueEntries(out)
	return out
}

func normalizePaasIncidentQueueEntry(in paasIncidentQueueEntry) (paasIncidentQueueEntry, bool) {
	out := in
	out.Key = strings.TrimSpace(out.Key)
	if out.Key == "" {
		out.Key = resolvePaasIncidentQueueEntryKey(out.Incident)
	}
	if out.Key == "" {
		return paasIncidentQueueEntry{}, false
	}
	out.Status = normalizePaasIncidentStatus(out.Status)
	if out.Status == "" {
		out.Status = normalizePaasIncidentStatus(out.Incident.Status)
	}
	out.FirstSeen = strings.TrimSpace(out.FirstSeen)
	if out.FirstSeen == "" {
		out.FirstSeen = strings.TrimSpace(out.Incident.TriggeredAt)
	}
	out.LastSeen = strings.TrimSpace(out.LastSeen)
	if out.LastSeen == "" {
		out.LastSeen = strings.TrimSpace(out.Incident.TriggeredAt)
	}
	if out.SeenCount < 1 {
		out.SeenCount = 1
	}
	return out, true
}
