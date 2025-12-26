package main

import (
	"context"
	"net/http"
	"strings"

	"silexa/agents/manager/internal/state"
)

type dyadPolicy struct {
	RequireAssignment bool
	AllowUnassigned   bool
	RequireRegistered bool
	EnforceAvailable  bool
	MaxOpenPerDyad    int
	AllowPool         bool
}

func loadDyadPolicy() dyadPolicy {
	return dyadPolicy{
		RequireAssignment: boolEnv("DYAD_REQUIRE_ASSIGNMENT", true),
		AllowUnassigned:   boolEnv("DYAD_ALLOW_UNASSIGNED", true),
		RequireRegistered: boolEnv("DYAD_REQUIRE_REGISTERED", true),
		EnforceAvailable:  boolEnv("DYAD_ENFORCE_AVAILABLE", true),
		MaxOpenPerDyad:    intEnv("DYAD_MAX_OPEN_PER_DYAD", 10),
		AllowPool:         boolEnv("DYAD_ALLOW_POOL", true),
	}
}

func normalizeStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

func isPoolDyad(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(name, "pool:")
}

func (s *server) validateDyadPolicy(ctx context.Context, dyad string, status string, existingDyad string, taskID int, tasks []state.DyadTask, dyads []state.DyadSnapshot) (int, string, error) {
	policy := s.policy
	dyad = strings.TrimSpace(dyad)
	existingDyad = strings.TrimSpace(existingDyad)
	status = normalizeStatus(status)
	if status == "" {
		status = "todo"
	}

	if dyad == "" || isPoolDyad(dyad) {
		if isPoolDyad(dyad) && !policy.AllowPool {
			return http.StatusBadRequest, "pool dyads not allowed", nil
		}
		if policy.RequireAssignment && !policy.AllowUnassigned && dyad == "" {
			return http.StatusConflict, "dyad assignment required", nil
		}
		if policy.RequireAssignment && status != "todo" {
			return http.StatusConflict, "dyad assignment required for non-todo status", nil
		}
		return 0, "", nil
	}

	if policy.RequireRegistered || policy.EnforceAvailable {
		if dyads == nil {
			if err := s.query(ctx, "dyads", &dyads); err != nil {
				return 0, "", err
			}
		}
		rec, ok := findDyadSnapshot(dyads, dyad)
		if (policy.RequireRegistered || policy.EnforceAvailable) && !ok {
			return http.StatusConflict, "dyad not registered", nil
		}
		if policy.EnforceAvailable && ok && !rec.Available {
			return http.StatusConflict, "dyad unavailable", nil
		}
	}

	if policy.MaxOpenPerDyad > 0 && dyad != existingDyad {
		if tasks == nil {
			if err := s.query(ctx, "dyad-tasks", &tasks); err != nil {
				return 0, "", err
			}
		}
		if countOpenDyadTasks(tasks, dyad, taskID) >= policy.MaxOpenPerDyad {
			return http.StatusConflict, "dyad at capacity", nil
		}
	}

	return 0, "", nil
}

func countOpenDyadTasks(tasks []state.DyadTask, dyad string, excludeID int) int {
	count := 0
	for _, t := range tasks {
		if excludeID > 0 && t.ID == excludeID {
			continue
		}
		if t.Dyad != dyad {
			continue
		}
		if normalizeStatus(t.Status) == "done" {
			continue
		}
		count++
	}
	return count
}

func findDyadSnapshot(list []state.DyadSnapshot, dyad string) (state.DyadSnapshot, bool) {
	for _, rec := range list {
		if rec.Dyad == dyad {
			return rec, true
		}
	}
	return state.DyadSnapshot{}, false
}
