package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncCodexSkillsCopiesBundleIntoCodexAndShared(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(home, ".codex")
	if err := os.MkdirAll(filepath.Join(home, ".si"), 0o700); err != nil {
		t.Fatalf("mkdir .si: %v", err)
	}
	bundle := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(filepath.Join(bundle, "si-vault-ops"), 0o755); err != nil {
		t.Fatalf("mkdir bundle skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "si-vault-ops", "SKILL.md"), []byte("name: si-vault-ops"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(bundle, "si-vault-ops", "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	execPath := filepath.Join(bundle, "si-vault-ops", "scripts", "check.sh")
	if err := os.WriteFile(execPath, []byte("#!/usr/bin/env bash\necho ok\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv("SI_CODEX_SKILLS_BUNDLE_DIR", bundle)
	if err := syncCodexSkills(home, codexHome); err != nil {
		t.Fatalf("syncCodexSkills: %v", err)
	}

	// Codex target
	targetSkill := filepath.Join(codexHome, "skills", "si-vault-ops", "SKILL.md")
	if _, err := os.Stat(targetSkill); err != nil {
		t.Fatalf("missing target skill file: %v", err)
	}
	targetScript := filepath.Join(codexHome, "skills", "si-vault-ops", "scripts", "check.sh")
	info, err := os.Stat(targetScript)
	if err != nil {
		t.Fatalf("missing target script: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected executable mode for copied script, got %v", info.Mode())
	}

	// Shared mirror under ~/.si
	sharedSkill := filepath.Join(home, ".si", "codex", "skills", "si-vault-ops", "SKILL.md")
	if _, err := os.Stat(sharedSkill); err != nil {
		t.Fatalf("missing shared skill file: %v", err)
	}
}

func TestSyncCodexSkillsMissingBundleReturnsNil(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(home, ".codex")
	t.Setenv("SI_CODEX_SKILLS_BUNDLE_DIR", filepath.Join(t.TempDir(), "missing-skills"))
	if err := syncCodexSkills(home, codexHome); err != nil {
		t.Fatalf("expected nil error for missing bundle, got: %v", err)
	}
	// Target dir should still exist because codex-init creates it before syncing.
	if _, err := os.Stat(filepath.Join(codexHome, "skills")); err != nil {
		t.Fatalf("expected codex skills dir to exist: %v", err)
	}
}

func TestParseArgsExecForwarding(t *testing.T) {
	quiet, execArgs := parseArgs([]string{"--quiet", "--exec", "bash", "-lc", "echo hi"})
	if !quiet {
		t.Fatalf("expected quiet=true")
	}
	got := strings.Join(execArgs, " ")
	if got != "bash -lc echo hi" {
		t.Fatalf("unexpected exec args: %q", got)
	}
}
