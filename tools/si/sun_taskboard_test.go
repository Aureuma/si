package main

import (
	"testing"
	"time"
)

func TestNormalizeSunTaskStatus(t *testing.T) {
	cases := map[string]string{
		"":            sunTaskStatusTodo,
		"open":        sunTaskStatusTodo,
		"claimed":     sunTaskStatusDoing,
		"in-progress": sunTaskStatusDoing,
		"done":        sunTaskStatusDone,
		"closed":      sunTaskStatusDone,
		"unknown":     sunTaskStatusTodo,
	}
	for in, want := range cases {
		if got := normalizeSunTaskStatus(in); got != want {
			t.Fatalf("normalizeSunTaskStatus(%q)=%q want %q", in, got, want)
		}
	}
}

func TestParseSunTaskStatusFilter(t *testing.T) {
	status, err := parseSunTaskStatusFilter("")
	if err != nil {
		t.Fatalf("unexpected error for empty status: %v", err)
	}
	if status != "" {
		t.Fatalf("expected empty filter, got %q", status)
	}
	status, err = parseSunTaskStatusFilter("claimed")
	if err != nil {
		t.Fatalf("unexpected error for claimed: %v", err)
	}
	if status != sunTaskStatusDoing {
		t.Fatalf("expected doing, got %q", status)
	}
	if _, err := parseSunTaskStatusFilter("not-a-status"); err == nil {
		t.Fatalf("expected invalid status to fail")
	}
}

func TestSunTaskboardSelectNextClaimable(t *testing.T) {
	now := time.Date(2026, 2, 22, 0, 0, 0, 0, time.UTC)
	tasks := []sunTaskboardTask{
		{
			ID:       "t1",
			Title:    "done",
			Status:   sunTaskStatusDone,
			Priority: sunTaskPriorityP1,
		},
		{
			ID:       "t2",
			Title:    "locked active",
			Status:   sunTaskStatusDoing,
			Priority: sunTaskPriorityP1,
			Assignment: &sunTaskboardLock{
				AgentID:        "agent-a",
				ClaimedAt:      now.Add(-1 * time.Minute).Format(time.RFC3339),
				LeaseSeconds:   600,
				LeaseExpiresAt: now.Add(9 * time.Minute).Format(time.RFC3339),
			},
		},
		{
			ID:        "t3",
			Title:     "claim me first",
			Status:    sunTaskStatusTodo,
			Priority:  sunTaskPriorityP1,
			CreatedAt: now.Add(-10 * time.Minute).Format(time.RFC3339),
		},
		{
			ID:        "t4",
			Title:     "lower priority",
			Status:    sunTaskStatusTodo,
			Priority:  sunTaskPriorityP2,
			CreatedAt: now.Add(-20 * time.Minute).Format(time.RFC3339),
		},
	}
	got := sunTaskboardSelectNextClaimable(tasks, now)
	if got < 0 || tasks[got].ID != "t3" {
		t.Fatalf("expected t3 to be selected, got idx=%d task=%+v", got, tasks[got])
	}
}

func TestSunTaskboardLockExpired(t *testing.T) {
	now := time.Date(2026, 2, 22, 0, 0, 0, 0, time.UTC)
	if !sunTaskboardLockExpired(sunTaskboardLock{
		AgentID:        "a",
		LeaseExpiresAt: now.Add(-1 * time.Second).Format(time.RFC3339),
	}, now) {
		t.Fatalf("expected explicit expiry to be considered expired")
	}
	if sunTaskboardLockExpired(sunTaskboardLock{
		AgentID:        "a",
		LeaseExpiresAt: now.Add(10 * time.Second).Format(time.RFC3339),
	}, now) {
		t.Fatalf("expected unexpired lock to remain active")
	}
}

func TestSunTaskboardResolveAgent(t *testing.T) {
	settings := Settings{}
	settings.Sun.TaskboardAgent = "agent-from-settings"
	got := sunTaskboardResolveAgent(settings, "", "dyad-main", "build-host")
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
