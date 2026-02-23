package main

import "testing"

func TestEnvWithoutGitVars(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"GIT_DIR=/tmp/repo/.git",
		"GIT_WORK_TREE=/tmp/repo",
		"HOME=/home/tester",
	}
	got := envWithoutGitVars(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 env vars after filtering, got %d: %#v", len(got), got)
	}
	if got[0] != "PATH=/usr/bin" || got[1] != "HOME=/home/tester" {
		t.Fatalf("unexpected filtered env: %#v", got)
	}
}
