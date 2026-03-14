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
	current := workspaceDefaultValue(*settings, scope)
	if current != "" && !configuredDirectoryMissing(current) {
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
	current := workspaceDefaultValue(*settings, scope)
	if current != "" && !configuredDirectoryMissing(current) {
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

func setWorkspaceRootDefault(settings *Settings, root string) bool {
	if settings == nil {
		return false
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return false
	}
	current := strings.TrimSpace(settings.Paths.WorkspaceRoot)
	if current != "" && !configuredDirectoryMissing(current) {
		return false
	}
	settings.Paths.WorkspaceRoot = root
	return true
}

func workspaceRootDefaultPrompt(root string) string {
	return fmt.Sprintf("Save %s as default workspace root in ~/.si/settings.toml?", root)
}

func ensureWorkspaceRootDefault(
	settings *Settings,
	root string,
	interactive bool,
	confirm workspaceConfirmFunc,
	save func(Settings) error,
) (bool, error) {
	if !interactive || settings == nil {
		return false, nil
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return false, nil
	}
	current := strings.TrimSpace(settings.Paths.WorkspaceRoot)
	if current != "" && !configuredDirectoryMissing(current) {
		return false, nil
	}
	if confirm == nil {
		confirm = confirmYN
	}
	if save == nil {
		save = saveSettings
	}
	confirmed, ok := confirm(workspaceRootDefaultPrompt(root), true)
	if !ok || !confirmed {
		return false, nil
	}
	if !setWorkspaceRootDefault(settings, root) {
		return false, nil
	}
	if err := save(*settings); err != nil {
		return false, err
	}
	return true, nil
}

func maybePersistWorkspaceRootDefault(settings *Settings, root string, interactive bool) {
	saved, err := ensureWorkspaceRootDefault(settings, root, interactive, confirmYN, saveSettings)
	if err != nil {
		warnf("could not persist default workspace root: %v", err)
		return
	}
	if saved {
		infof("saved default workspace root: %s", strings.TrimSpace(root))
	}
}

func dyadConfigsDefaultPrompt(configs string) string {
	return fmt.Sprintf("Save %s as default dyad configs in ~/.si/settings.toml?", configs)
}

func setDyadConfigsDefault(settings *Settings, configs string) bool {
	if settings == nil {
		return false
	}
	configs = strings.TrimSpace(configs)
	if configs == "" {
		return false
	}
	current := strings.TrimSpace(settings.Dyad.Configs)
	if current != "" && !configuredDirectoryMissing(current) {
		return false
	}
	settings.Dyad.Configs = configs
	return true
}

func ensureDyadConfigsDefault(
	settings *Settings,
	configs string,
	interactive bool,
	confirm workspaceConfirmFunc,
	save func(Settings) error,
) (bool, error) {
	if !interactive || settings == nil {
		return false, nil
	}
	configs = strings.TrimSpace(configs)
	if configs == "" {
		return false, nil
	}
	current := strings.TrimSpace(settings.Dyad.Configs)
	if current != "" && !configuredDirectoryMissing(current) {
		return false, nil
	}
	if confirm == nil {
		confirm = confirmYN
	}
	if save == nil {
		save = saveSettings
	}
	confirmed, ok := confirm(dyadConfigsDefaultPrompt(configs), true)
	if !ok || !confirmed {
		return false, nil
	}
	if !setDyadConfigsDefault(settings, configs) {
		return false, nil
	}
	if err := save(*settings); err != nil {
		return false, err
	}
	return true, nil
}

func maybePersistDyadConfigsDefault(settings *Settings, configs string, interactive bool) {
	saved, err := ensureDyadConfigsDefault(settings, configs, interactive, confirmYN, saveSettings)
	if err != nil {
		warnf("could not persist default dyad configs: %v", err)
		return
	}
	if saved {
		infof("saved default dyad configs: %s", strings.TrimSpace(configs))
	}
}
