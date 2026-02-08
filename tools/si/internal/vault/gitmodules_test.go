package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureGitmodulesIgnoreDirty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitmodules")
	if err := os.WriteFile(path, []byte(""+
		"[submodule \"vault\"]\n"+
		"\tpath = vault\n"+
		"\turl = git@example.com:org/vault.git\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	changed, err := EnsureGitmodulesIgnoreDirty(dir, "vault")
	if err != nil {
		t.Fatalf("EnsureGitmodulesIgnoreDirty: %v", err)
	}
	if !changed {
		t.Fatalf("expected change")
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(out), "ignore = dirty") {
		t.Fatalf("expected ignore=dirty, got:\n%s", string(out))
	}
	changed, err = EnsureGitmodulesIgnoreDirty(dir, "vault")
	if err != nil {
		t.Fatalf("EnsureGitmodulesIgnoreDirty2: %v", err)
	}
	if changed {
		t.Fatalf("expected no change")
	}
}
