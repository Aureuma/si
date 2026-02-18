package main

import (
	"fmt"
	"strings"
)

type workspaceDefaultScope string

const (
	workspaceScopeCodex workspaceDefaultScope = "codex"
	workspaceScopeDyad  workspaceDefaultScope = "dyad"
)

type workspaceConfirmFunc func(prompt string, defaultYes bool) (bool, bool)

func workspaceScopeLabel(scope workspaceDefaultScope) string {
	switch scope {
	case workspaceScopeCodex:
		return "codex"
	case workspaceScopeDyad:
		return "dyad"
	default:
		return ""
	}
}

func workspaceDefaultValue(settings Settings, scope workspaceDefaultScope) string {
	switch scope {
	case workspaceScopeCodex:
		return strings.TrimSpace(settings.Codex.Workspace)
	case workspaceScopeDyad:
		return strings.TrimSpace(settings.Dyad.Workspace)
	default:
		return ""
	}
}

func setWorkspaceDefault(settings *Settings, scope workspaceDefaultScope, workspace string) bool {
	if settings == nil {
		return false
	}
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return false
	}
	if workspaceDefaultValue(*settings, scope) != "" {
		return false
	}
	switch scope {
	case workspaceScopeCodex:
		settings.Codex.Workspace = workspace
		return true
	case workspaceScopeDyad:
		settings.Dyad.Workspace = workspace
		return true
	default:
		return false
	}
}

func workspaceDefaultPrompt(scope workspaceDefaultScope, workspace string) string {
	label := workspaceScopeLabel(scope)
	if label == "" {
		return ""
	}
	return fmt.Sprintf("Save %s as default %s workspace in ~/.si/settings.toml?", workspace, label)
}

func ensureWorkspaceDefault(
	scope workspaceDefaultScope,
	settings *Settings,
	workspace string,
	interactive bool,
	confirm workspaceConfirmFunc,
	save func(Settings) error,
) (bool, error) {
	if !interactive || settings == nil {
		return false, nil
	}
	label := workspaceScopeLabel(scope)
	workspace = strings.TrimSpace(workspace)
	if label == "" || workspace == "" {
		return false, nil
	}
	if workspaceDefaultValue(*settings, scope) != "" {
		return false, nil
	}
	if confirm == nil {
		confirm = confirmYN
	}
	if save == nil {
		save = saveSettings
	}
	confirmed, ok := confirm(workspaceDefaultPrompt(scope, workspace), true)
	if !ok || !confirmed {
		return false, nil
	}
	if !setWorkspaceDefault(settings, scope, workspace) {
		return false, nil
	}
	if err := save(*settings); err != nil {
		return false, err
	}
	return true, nil
}

func maybePersistWorkspaceDefault(
	scope workspaceDefaultScope,
	settings *Settings,
	workspace string,
	interactive bool,
) {
	saved, err := ensureWorkspaceDefault(scope, settings, workspace, interactive, confirmYN, saveSettings)
	if err != nil {
		warnf("could not persist default %s workspace: %v", workspaceScopeLabel(scope), err)
		return
	}
	if saved {
		infof("saved default %s workspace: %s", workspaceScopeLabel(scope), strings.TrimSpace(workspace))
	}
}
