package main

import (
	"testing"
	"time"
)

func TestNormalizeHeliaTaskStatus(t *testing.T) {
	cases := map[string]string{
		"":            heliaTaskStatusTodo,
		"open":        heliaTaskStatusTodo,
		"claimed":     heliaTaskStatusDoing,
		"in-progress": heliaTaskStatusDoing,
		"done":        heliaTaskStatusDone,
		"closed":      heliaTaskStatusDone,
		"unknown":     heliaTaskStatusTodo,
	}
	for in, want := range cases {
		if got := normalizeHeliaTaskStatus(in); got != want {
			t.Fatalf("normalizeHeliaTaskStatus(%q)=%q want %q", in, got, want)
		}
	}
}

func TestParseHeliaTaskStatusFilter(t *testing.T) {
	status, err := parseHeliaTaskStatusFilter("")
	if err != nil {
		t.Fatalf("unexpected error for empty status: %v", err)
	}
	if status != "" {
		t.Fatalf("expected empty filter, got %q", status)
	}
	status, err = parseHeliaTaskStatusFilter("claimed")
	if err != nil {
		t.Fatalf("unexpected error for claimed: %v", err)
	}
	if status != heliaTaskStatusDoing {
		t.Fatalf("expected doing, got %q", status)
	}
	if _, err := parseHeliaTaskStatusFilter("not-a-status"); err == nil {
		t.Fatalf("expected invalid status to fail")
	}
}

func TestHeliaTaskboardSelectNextClaimable(t *testing.T) {
	now := time.Date(2026, 2, 22, 0, 0, 0, 0, time.UTC)
	tasks := []heliaTaskboardTask{
		{
			ID:       "t1",
			Title:    "done",
			Status:   heliaTaskStatusDone,
			Priority: heliaTaskPriorityP1,
		},
		{
			ID:       "t2",
			Title:    "locked active",
			Status:   heliaTaskStatusDoing,
			Priority: heliaTaskPriorityP1,
			Assignment: &heliaTaskboardLock{
				AgentID:        "agent-a",
				ClaimedAt:      now.Add(-1 * time.Minute).Format(time.RFC3339),
				LeaseSeconds:   600,
				LeaseExpiresAt: now.Add(9 * time.Minute).Format(time.RFC3339),
			},
		},
		{
			ID:        "t3",
			Title:     "claim me first",
			Status:    heliaTaskStatusTodo,
			Priority:  heliaTaskPriorityP1,
			CreatedAt: now.Add(-10 * time.Minute).Format(time.RFC3339),
		},
		{
			ID:        "t4",
			Title:     "lower priority",
			Status:    heliaTaskStatusTodo,
			Priority:  heliaTaskPriorityP2,
			CreatedAt: now.Add(-20 * time.Minute).Format(time.RFC3339),
		},
	}
	got := heliaTaskboardSelectNextClaimable(tasks, now)
	if got < 0 || tasks[got].ID != "t3" {
		t.Fatalf("expected t3 to be selected, got idx=%d task=%+v", got, tasks[got])
	}
}

func TestHeliaTaskboardLockExpired(t *testing.T) {
	now := time.Date(2026, 2, 22, 0, 0, 0, 0, time.UTC)
	if !heliaTaskboardLockExpired(heliaTaskboardLock{
		AgentID:        "a",
		LeaseExpiresAt: now.Add(-1 * time.Second).Format(time.RFC3339),
	}, now) {
		t.Fatalf("expected explicit expiry to be considered expired")
	}
	if heliaTaskboardLockExpired(heliaTaskboardLock{
		AgentID:        "a",
		LeaseExpiresAt: now.Add(10 * time.Second).Format(time.RFC3339),
	}, now) {
		t.Fatalf("expected unexpired lock to remain active")
	}
}

func TestHeliaTaskboardResolveAgent(t *testing.T) {
	settings := Settings{}
	settings.Helia.TaskboardAgent = "agent-from-settings"
	got := heliaTaskboardResolveAgent(settings, "", "dyad-main", "build-host")
	if got.AgentID != "agent-from-settings" {
		t.Fatalf("agent id=%q", got.AgentID)
	}
	if got.Dyad != "dyad-main" {
		t.Fatalf("dyad=%q", got.Dyad)
	}
	if got.Machine != "build-host" {
		t.Fatalf("machine=%q", got.Machine)
	}
}
