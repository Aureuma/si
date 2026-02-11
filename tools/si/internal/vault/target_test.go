package vault

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResolveTargetDefaultsToDotenvInVaultDir(t *testing.T) {
	repo := initGitRepoForTargetTest(t)
	vaultDir := filepath.Join(repo, "vault")
	if err := os.MkdirAll(vaultDir, 0o700); err != nil {
		t.Fatalf("mkdir vault dir: %v", err)
	}

	path := filepath.Join(vaultDir, ".env")
	if err := os.WriteFile(path, []byte("A=1\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	target, err := ResolveTarget(ResolveOptions{
		CWD:                  repo,
		VaultDir:             "vault",
		AllowMissingVaultDir: false,
		AllowMissingFile:     false,
	})
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	got, _ := filepath.EvalSymlinks(target.File)
	want, _ := filepath.EvalSymlinks(path)
	if got != want {
		t.Fatalf("file=%q want %q", target.File, path)
	}
}

func TestResolveTargetExplicitFile(t *testing.T) {
	repo := initGitRepoForTargetTest(t)
	vaultDir := filepath.Join(repo, "vault")
	if err := os.MkdirAll(vaultDir, 0o700); err != nil {
		t.Fatalf("mkdir vault dir: %v", err)
	}
	path := filepath.Join(vaultDir, ".env.prod")
	if err := os.WriteFile(path, []byte("A=1\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	target, err := ResolveTarget(ResolveOptions{
		CWD:                  repo,
		File:                 path,
		AllowMissingVaultDir: false,
		AllowMissingFile:     false,
	})
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	got, _ := filepath.EvalSymlinks(target.File)
	want, _ := filepath.EvalSymlinks(path)
	if got != want {
		t.Fatalf("file=%q want %q", target.File, path)
	}
}

func TestResolveTargetExplicitFileOutsideGitRepo(t *testing.T) {
	base := t.TempDir()
	vaultDir := filepath.Join(base, "secrets")
	if err := os.MkdirAll(vaultDir, 0o700); err != nil {
		t.Fatalf("mkdir vault dir: %v", err)
	}
	path := filepath.Join(vaultDir, ".env.custom")
	if err := os.WriteFile(path, []byte("A=1\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	target, err := ResolveTarget(ResolveOptions{
		CWD:              base,
		File:             path,
		AllowMissingFile: false,
	})
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if target.RepoRoot != "" {
		t.Fatalf("expected empty repo root for non-git path, got %q", target.RepoRoot)
	}
	got, _ := filepath.EvalSymlinks(target.File)
	want, _ := filepath.EvalSymlinks(path)
	if got != want {
		t.Fatalf("file=%q want %q", target.File, path)
	}
}

func initGitRepoForTargetTest(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not found: %v", err)
	}
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, string(out))
	}
	return repo
}
