package main

import (
	"errors"
	"strings"
	"testing"
)

func TestWorkspaceDefaultValue(t *testing.T) {
	settings := defaultSettings()
	settings.Codex.Workspace = " /tmp/codex "
	settings.Dyad.Workspace = " /tmp/dyad "

	if got := workspaceDefaultValue(settings, workspaceScopeCodex); got != "/tmp/codex" {
		t.Fatalf("unexpected codex workspace: %q", got)
	}
	if got := workspaceDefaultValue(settings, workspaceScopeDyad); got != "/tmp/dyad" {
		t.Fatalf("unexpected dyad workspace: %q", got)
	}
}

func TestSetWorkspaceDefault(t *testing.T) {
	settings := defaultSettings()
	if changed := setWorkspaceDefault(&settings, workspaceScopeCodex, " /tmp/codex "); !changed {
		t.Fatalf("expected codex workspace to be set")
	}
	if settings.Codex.Workspace != "/tmp/codex" {
		t.Fatalf("unexpected codex workspace: %q", settings.Codex.Workspace)
	}

	// Should not overwrite an already-configured default.
	if changed := setWorkspaceDefault(&settings, workspaceScopeCodex, "/tmp/other"); changed {
		t.Fatalf("expected existing codex workspace to remain unchanged")
	}
	if settings.Codex.Workspace != "/tmp/codex" {
		t.Fatalf("unexpected codex workspace after overwrite attempt: %q", settings.Codex.Workspace)
	}
}

func TestEnsureWorkspaceDefaultPersistsWhenConfirmed(t *testing.T) {
	settings := defaultSettings()
	var promptSeen string
	saved := false
	confirm := func(prompt string, defaultYes bool) (bool, bool) {
		promptSeen = prompt
		if !defaultYes {
			t.Fatalf("expected defaultYes=true")
		}
		return true, true
	}
	save := func(in Settings) error {
		saved = true
		if got := strings.TrimSpace(in.Codex.Workspace); got != "/tmp/workspace" {
			t.Fatalf("unexpected saved codex workspace: %q", got)
		}
		return nil
	}

	changed, err := ensureWorkspaceDefault(
		workspaceScopeCodex,
		&settings,
		"/tmp/workspace",
		true,
		confirm,
		save,
	)
	if err != nil {
		t.Fatalf("ensureWorkspaceDefault() unexpected err: %v", err)
	}
	if !changed {
		t.Fatalf("expected workspace default to be persisted")
	}
	if !saved {
		t.Fatalf("expected save callback to be invoked")
	}
	if !strings.Contains(promptSeen, "/tmp/workspace") || !strings.Contains(promptSeen, "codex") {
		t.Fatalf("unexpected prompt: %q", promptSeen)
	}
}

func TestEnsureWorkspaceDefaultSkipsWhenDeclined(t *testing.T) {
	settings := defaultSettings()
	saveCalled := false
	confirm := func(prompt string, defaultYes bool) (bool, bool) {
		return false, true
	}
	save := func(in Settings) error {
		saveCalled = true
		return nil
	}

	changed, err := ensureWorkspaceDefault(
		workspaceScopeDyad,
		&settings,
		"/tmp/workspace",
		true,
		confirm,
		save,
	)
	if err != nil {
		t.Fatalf("ensureWorkspaceDefault() unexpected err: %v", err)
	}
	if changed {
		t.Fatalf("expected no change when user declines")
	}
	if saveCalled {
		t.Fatalf("did not expect save callback when user declines")
	}
	if settings.Dyad.Workspace != "" {
		t.Fatalf("expected dyad workspace to remain empty, got %q", settings.Dyad.Workspace)
	}
}

func TestEnsureWorkspaceDefaultSkipsWhenNonInteractive(t *testing.T) {
	settings := defaultSettings()
	confirmCalled := false
	confirm := func(prompt string, defaultYes bool) (bool, bool) {
		confirmCalled = true
		return true, true
	}
	save := func(in Settings) error {
		t.Fatalf("save should not be called in non-interactive mode")
		return nil
	}

	changed, err := ensureWorkspaceDefault(
		workspaceScopeCodex,
		&settings,
		"/tmp/workspace",
		false,
		confirm,
		save,
	)
	if err != nil {
		t.Fatalf("ensureWorkspaceDefault() unexpected err: %v", err)
	}
	if changed {
		t.Fatalf("expected no change in non-interactive mode")
	}
	if confirmCalled {
		t.Fatalf("did not expect confirmation prompt in non-interactive mode")
	}
}

func TestEnsureWorkspaceDefaultPropagatesSaveError(t *testing.T) {
	settings := defaultSettings()
	confirm := func(prompt string, defaultYes bool) (bool, bool) { return true, true }
	saveErr := errors.New("save failed")
	save := func(in Settings) error { return saveErr }

	changed, err := ensureWorkspaceDefault(
		workspaceScopeCodex,
		&settings,
		"/tmp/workspace",
		true,
		confirm,
		save,
	)
	if changed {
		t.Fatalf("expected changed=false when save fails")
	}
	if !errors.Is(err, saveErr) {
		t.Fatalf("expected save error, got %v", err)
	}
}
