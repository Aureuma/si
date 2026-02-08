package main

import "testing"

func TestParseGitHubOwnerRepo(t *testing.T) {
	owner, repo, err := parseGitHubOwnerRepo("org/repo", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "org" || repo != "repo" {
		t.Fatalf("unexpected parse result: %s/%s", owner, repo)
	}

	owner, repo, err = parseGitHubOwnerRepo("repo", "org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "org" || repo != "repo" {
		t.Fatalf("unexpected fallback parse: %s/%s", owner, repo)
	}

	if _, _, err := parseGitHubOwnerRepo("repo", ""); err == nil {
		t.Fatalf("expected error for missing owner")
	}
	if _, _, err := parseGitHubOwnerRepo("a/b/c", ""); err == nil {
		t.Fatalf("expected error for invalid owner/repo")
	}
}

func TestParseGitHubNumber(t *testing.T) {
	v, err := parseGitHubNumber("42", "number")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}
	if _, err := parseGitHubNumber("0", "number"); err == nil {
		t.Fatalf("expected error for zero")
	}
}

func TestRequireGithubConfirmationNonInteractive(t *testing.T) {
	if err := requireGithubConfirmation("delete repo", false); err == nil {
		t.Fatalf("expected non-interactive confirmation error")
	}
	if err := requireGithubConfirmation("delete repo", true); err != nil {
		t.Fatalf("force should skip confirmation: %v", err)
	}
}
