package main

import "testing"

func TestGitHubWorkflowRunStatusFromData(t *testing.T) {
	status := githubWorkflowRunStatusFromData(map[string]any{
		"id":          float64(42),
		"name":        " CI ",
		"status":      " completed ",
		"conclusion":  " success ",
		"html_url":    " https://github.com/Aureuma/si/actions/runs/42 ",
		"head_branch": " main ",
		"event":       " push ",
	})
	if status.ID != 42 {
		t.Fatalf("id=%d", status.ID)
	}
	if status.Name != "CI" {
		t.Fatalf("name=%q", status.Name)
	}
	if status.Status != "completed" {
		t.Fatalf("status=%q", status.Status)
	}
	if status.Conclusion != "success" {
		t.Fatalf("conclusion=%q", status.Conclusion)
	}
	if status.HTMLURL != "https://github.com/Aureuma/si/actions/runs/42" {
		t.Fatalf("html_url=%q", status.HTMLURL)
	}
	if status.HeadBranch != "main" {
		t.Fatalf("head_branch=%q", status.HeadBranch)
	}
	if status.Event != "push" {
		t.Fatalf("event=%q", status.Event)
	}
}

func TestGitHubWorkflowInt(t *testing.T) {
	if got := githubWorkflowInt(int64(7)); got != 7 {
		t.Fatalf("int64 got=%d", got)
	}
	if got := githubWorkflowInt(float64(8)); got != 8 {
		t.Fatalf("float64 got=%d", got)
	}
	if got := githubWorkflowInt("9"); got != 0 {
		t.Fatalf("string got=%d", got)
	}
}

func TestGitHubWorkflowRunIsFailureConclusion(t *testing.T) {
	nonFailures := []string{"success", "skipped", "neutral", " Success "}
	for _, value := range nonFailures {
		if githubWorkflowRunIsFailureConclusion(value) {
			t.Fatalf("expected non-failure for %q", value)
		}
	}

	failures := []string{"", "failure", "cancelled", "timed_out", "action_required", "stale"}
	for _, value := range failures {
		if !githubWorkflowRunIsFailureConclusion(value) {
			t.Fatalf("expected failure for %q", value)
		}
	}
}
