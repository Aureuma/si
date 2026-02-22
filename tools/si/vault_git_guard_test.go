package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseVaultGitLsFilesLine(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		ok     bool
		tr     bool
		skip   bool
		assume bool
	}{
		{name: "empty", line: "", ok: false},
		{name: "tracked normal", line: "H .env.prod", ok: true, tr: true},
		{name: "assume unchanged", line: "h .env.prod", ok: true, tr: true, assume: true},
		{name: "skip worktree", line: "S .env.prod", ok: true, tr: true, skip: true},
		{name: "both flags", line: "s .env.prod", ok: true, tr: true, skip: true, assume: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseVaultGitLsFilesLine(tc.line)
			if ok != tc.ok {
				t.Fatalf("ok=%v want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if got.Tracked != tc.tr || got.SkipWorktree != tc.skip || got.AssumeUnchanged != tc.assume {
				t.Fatalf("flags=%+v want tracked=%v skip=%v assume=%v", got, tc.tr, tc.skip, tc.assume)
			}
		})
	}
}

func TestVaultGitIndexFlagsForPathDetectsSkipWorktreeAndAssumeUnchanged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	envFile := filepath.Join(repo, ".env.prod")
	if out, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, string(out))
	}
	if err := os.WriteFile(envFile, []byte("A=1\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if out, err := exec.Command("git", "-C", repo, "add", ".env.prod").CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v: %s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repo, "update-index", "--skip-worktree", "--", ".env.prod").CombinedOutput(); err != nil {
		t.Fatalf("set skip-worktree failed: %v: %s", err, string(out))
	}

	_, _, flags, err := vaultGitIndexFlagsForPath(envFile)
	if err != nil {
		t.Fatalf("vaultGitIndexFlagsForPath: %v", err)
	}
	if !flags.Tracked || !flags.SkipWorktree || flags.AssumeUnchanged {
		t.Fatalf("unexpected flags after skip-worktree: %+v", flags)
	}

	if out, err := exec.Command("git", "-C", repo, "update-index", "--assume-unchanged", "--", ".env.prod").CombinedOutput(); err != nil {
		t.Fatalf("set assume-unchanged failed: %v: %s", err, string(out))
	}
	_, _, flags, err = vaultGitIndexFlagsForPath(envFile)
	if err != nil {
		t.Fatalf("vaultGitIndexFlagsForPath second call: %v", err)
	}
	if !flags.Tracked || !flags.SkipWorktree || !flags.AssumeUnchanged {
		t.Fatalf("unexpected flags after both bits set: %+v", flags)
	}
}
