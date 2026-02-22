package main

import (
	"testing"
	"time"
)

func TestParseGitHubProjectRefURL(t *testing.T) {
	ref, err := parseGitHubProjectRef("https://github.com/orgs/Aureuma/projects/7/views/4")
	if err != nil {
		t.Fatalf("parseGitHubProjectRef: %v", err)
	}
	if ref.Organization != "Aureuma" {
		t.Fatalf("organization=%q want=Aureuma", ref.Organization)
	}
	if ref.Number != 7 {
		t.Fatalf("number=%d want=7", ref.Number)
	}
	if ref.ProjectID != "" {
		t.Fatalf("project id should be empty for org/number refs")
	}
}

func TestParseGitHubProjectRefOrgNumber(t *testing.T) {
	ref, err := parseGitHubProjectRef("Aureuma/12")
	if err != nil {
		t.Fatalf("parseGitHubProjectRef: %v", err)
	}
	if ref.Organization != "Aureuma" || ref.Number != 12 {
		t.Fatalf("unexpected ref: %#v", ref)
	}
}

func TestParseGitHubProjectRefProjectID(t *testing.T) {
	ref, err := parseGitHubProjectRef("PVT_kwDOB2x6Nc4ArlO7")
	if err != nil {
		t.Fatalf("parseGitHubProjectRef: %v", err)
	}
	if ref.ProjectID != "PVT_kwDOB2x6Nc4ArlO7" {
		t.Fatalf("project id=%q", ref.ProjectID)
	}
	if ref.Number != 0 || ref.Organization != "" {
		t.Fatalf("unexpected ref: %#v", ref)
	}
}

func TestGitHubProjectResolveSingleSelectOptionID(t *testing.T) {
	field := githubProjectFieldDescriptor{
		Name: "Status",
		Options: []githubProjectFieldOption{
			{ID: "opt_todo", Name: "Todo"},
			{ID: "opt_prog", Name: "In Progress"},
		},
	}
	got, err := githubProjectResolveSingleSelectOptionID(field, "in progress")
	if err != nil {
		t.Fatalf("githubProjectResolveSingleSelectOptionID: %v", err)
	}
	if got != "opt_prog" {
		t.Fatalf("option id=%q want=opt_prog", got)
	}
}

func TestGitHubProjectResolveIterationIDCurrent(t *testing.T) {
	now := time.Now().UTC()
	past := now.AddDate(0, 0, -14).Format("2006-01-02")
	recent := now.AddDate(0, 0, -3).Format("2006-01-02")
	future := now.AddDate(0, 0, 7).Format("2006-01-02")
	field := githubProjectFieldDescriptor{
		Name: "Iteration",
		Iterations: []githubProjectIteration{
			{ID: "iter_past", Title: "Old", StartDate: past},
			{ID: "iter_recent", Title: "Current", StartDate: recent},
			{ID: "iter_future", Title: "Future", StartDate: future},
		},
	}
	got, err := githubProjectResolveIterationID(field, "@current")
	if err != nil {
		t.Fatalf("githubProjectResolveIterationID: %v", err)
	}
	if got != "iter_recent" {
		t.Fatalf("iteration id=%q want=iter_recent", got)
	}
}

func TestGitHubProjectResolveIterationIDByTitleAndDate(t *testing.T) {
	field := githubProjectFieldDescriptor{
		Name: "Iteration",
		Iterations: []githubProjectIteration{
			{ID: "iter_a", Title: "Sprint 14", StartDate: "2026-02-01"},
			{ID: "iter_b", Title: "Sprint 15", StartDate: "2026-02-15"},
		},
	}
	byTitle, err := githubProjectResolveIterationID(field, "sprint 15")
	if err != nil {
		t.Fatalf("githubProjectResolveIterationID by title: %v", err)
	}
	if byTitle != "iter_b" {
		t.Fatalf("by title=%q want=iter_b", byTitle)
	}
	byDate, err := githubProjectResolveIterationID(field, "2026-02-01")
	if err != nil {
		t.Fatalf("githubProjectResolveIterationID by date: %v", err)
	}
	if byDate != "iter_a" {
		t.Fatalf("by date=%q want=iter_a", byDate)
	}
}
