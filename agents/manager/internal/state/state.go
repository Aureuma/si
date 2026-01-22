package state

import (
	"sort"
	"strings"
	"time"
)

func addHeartbeat(st *State, hb Heartbeat, now time.Time) {
	st.Beats = append(st.Beats, hb)
	if len(st.Beats) > 1000 {
		st.Beats = st.Beats[len(st.Beats)-1000:]
	}
	updateDyadFromHeartbeat(st, hb, now)
}

func upsertDyad(st *State, update DyadUpdate, now time.Time) DyadRecord {
	dyad := strings.TrimSpace(update.Dyad)
	if dyad == "" {
		return DyadRecord{}
	}
	for i := range st.Dyads {
		if st.Dyads[i].Dyad != dyad {
			continue
		}
		if update.Department != "" {
			st.Dyads[i].Department = update.Department
		}
		if update.Role != "" {
			st.Dyads[i].Role = update.Role
		}
		if update.Team != "" {
			st.Dyads[i].Team = update.Team
		}
		if update.Assignment != "" {
			st.Dyads[i].Assignment = update.Assignment
		}
		if update.Tags != nil {
			st.Dyads[i].Tags = update.Tags
		}
		if update.Actor != "" {
			st.Dyads[i].Actor = update.Actor
		}
		if update.Critic != "" {
			st.Dyads[i].Critic = update.Critic
		}
		if update.Status != "" {
			st.Dyads[i].Status = update.Status
		}
		if update.Message != "" {
			st.Dyads[i].Message = update.Message
		}
		if update.Available != nil {
			st.Dyads[i].Available = *update.Available
		}
		st.Dyads[i].UpdatedAt = now
		return st.Dyads[i]
	}
	record := DyadRecord{
		Dyad:       dyad,
		Available:  true,
		UpdatedAt:  now,
		Department: update.Department,
		Role:       update.Role,
		Team:       update.Team,
		Assignment: update.Assignment,
		Tags:       update.Tags,
		Actor:      update.Actor,
		Critic:     update.Critic,
		Status:     update.Status,
		Message:    update.Message,
	}
	if update.Available != nil {
		record.Available = *update.Available
	}
	st.Dyads = append(st.Dyads, record)
	return record
}

func updateDyadFromHeartbeat(st *State, hb Heartbeat, now time.Time) {
	dyad := strings.TrimSpace(hb.Dyad)
	if dyad == "" {
		dyad = dyadFromContainer(hb.Actor)
	}
	if dyad == "" {
		dyad = dyadFromContainer(hb.Critic)
	}
	if dyad == "" {
		return
	}
	for i := range st.Dyads {
		if st.Dyads[i].Dyad != dyad {
			continue
		}
		if hb.Department != "" {
			st.Dyads[i].Department = hb.Department
		}
		if hb.Role != "" {
			st.Dyads[i].Role = hb.Role
		}
		if hb.Actor != "" {
			st.Dyads[i].Actor = hb.Actor
		}
		if hb.Critic != "" {
			st.Dyads[i].Critic = hb.Critic
		}
		if hb.Status != "" {
			st.Dyads[i].Status = hb.Status
		}
		if hb.Message != "" {
			st.Dyads[i].Message = hb.Message
		}
		st.Dyads[i].LastHeartbeat = now
		st.Dyads[i].UpdatedAt = now
		return
	}
}

func addHumanTask(st *State, task HumanTask, now time.Time) HumanTask {
	task.ID = st.NextTaskID
	st.NextTaskID++
	if task.Status == "" {
		task.Status = "todo"
	}
	task.CreatedAt = now
	st.Tasks = append(st.Tasks, task)
	return task
}

func completeHumanTask(st *State, id int, now time.Time) bool {
	if id <= 0 {
		return false
	}
	for i := range st.Tasks {
		if st.Tasks[i].ID != id {
			continue
		}
		if st.Tasks[i].Status == "done" {
			return true
		}
		st.Tasks[i].Status = "done"
		st.Tasks[i].CompletedAt = &now
		return true
	}
	return false
}

func addDyadTask(st *State, task DyadTask, now time.Time) DyadTask {
	task.ID = st.NextDyadTaskID
	st.NextDyadTaskID++
	if task.Status == "" {
		task.Status = "todo"
	}
	task.CreatedAt = now
	task.UpdatedAt = now
	st.DyadTasks = append(st.DyadTasks, task)
	return task
}

func updateDyadTask(st *State, updated DyadTask, now time.Time) (DyadTask, bool) {
	for i := range st.DyadTasks {
		if st.DyadTasks[i].ID != updated.ID {
			continue
		}
		if updated.Title != "" {
			st.DyadTasks[i].Title = updated.Title
		}
		if updated.Description != "" {
			st.DyadTasks[i].Description = updated.Description
		}
		if updated.Kind != "" {
			st.DyadTasks[i].Kind = updated.Kind
		}
		if updated.Status != "" {
			st.DyadTasks[i].Status = updated.Status
		}
		if updated.Priority != "" {
			st.DyadTasks[i].Priority = updated.Priority
		}
		if updated.Complexity != "" {
			st.DyadTasks[i].Complexity = updated.Complexity
		}
		if updated.Dyad != "" {
			st.DyadTasks[i].Dyad = updated.Dyad
		}
		if updated.Actor != "" {
			st.DyadTasks[i].Actor = updated.Actor
		}
		if updated.Critic != "" {
			st.DyadTasks[i].Critic = updated.Critic
		}
		if updated.RequestedBy != "" {
			st.DyadTasks[i].RequestedBy = updated.RequestedBy
		}
		if updated.Notes != "" {
			st.DyadTasks[i].Notes = updated.Notes
		}
		if updated.Link != "" {
			st.DyadTasks[i].Link = updated.Link
		}
		if updated.TelegramMessageID != 0 {
			st.DyadTasks[i].TelegramMessageID = updated.TelegramMessageID
		}
		if updated.ClaimedBy != "" {
			st.DyadTasks[i].ClaimedBy = updated.ClaimedBy
			if !updated.ClaimedAt.IsZero() {
				st.DyadTasks[i].ClaimedAt = updated.ClaimedAt
			} else if st.DyadTasks[i].ClaimedAt.IsZero() {
				st.DyadTasks[i].ClaimedAt = now
			}
		}
		if !updated.HeartbeatAt.IsZero() {
			st.DyadTasks[i].HeartbeatAt = updated.HeartbeatAt
		}
		st.DyadTasks[i].UpdatedAt = now
		return st.DyadTasks[i], true
	}
	return DyadTask{}, false
}

func claimDyadTask(st *State, id int, dyad string, critic string, now time.Time) (DyadTask, int, bool) {
	for i := range st.DyadTasks {
		if st.DyadTasks[i].ID != id {
			continue
		}
		if st.DyadTasks[i].Status == "done" {
			return DyadTask{}, 409, false
		}
		if dyad != "" && st.DyadTasks[i].Dyad != "" && st.DyadTasks[i].Dyad != dyad {
			return DyadTask{}, 409, false
		}
		if st.DyadTasks[i].ClaimedBy != "" && st.DyadTasks[i].ClaimedBy != critic {
			if !st.DyadTasks[i].HeartbeatAt.IsZero() && now.Sub(st.DyadTasks[i].HeartbeatAt) < 5*time.Minute {
				return DyadTask{}, 409, false
			}
		}
		if dyad != "" && st.DyadTasks[i].Dyad == "" {
			st.DyadTasks[i].Dyad = dyad
		}
		if critic != "" {
			if st.DyadTasks[i].ClaimedBy == "" || st.DyadTasks[i].ClaimedBy != critic {
				st.DyadTasks[i].ClaimedBy = critic
				st.DyadTasks[i].ClaimedAt = now
			}
			st.DyadTasks[i].HeartbeatAt = now
		}
		if st.DyadTasks[i].Status == "todo" {
			st.DyadTasks[i].Status = "in_progress"
		}
		st.DyadTasks[i].UpdatedAt = now
		return st.DyadTasks[i], 200, true
	}
	return DyadTask{}, 404, false
}

func addFeedback(st *State, fb Feedback, now time.Time) Feedback {
	fb.ID = st.NextFeedbackID
	st.NextFeedbackID++
	fb.CreatedAt = now
	st.Feedbacks = append(st.Feedbacks, fb)
	return fb
}

func addAccessRequest(st *State, ar AccessRequest, now time.Time) AccessRequest {
	ar.ID = st.NextAccessID
	st.NextAccessID++
	if ar.Status == "" {
		ar.Status = "pending"
	}
	ar.CreatedAt = now
	st.Access = append(st.Access, ar)
	return ar
}

func resolveAccessRequest(st *State, id int, status string, by string, notes string, now time.Time) (AccessRequest, bool) {
	if id <= 0 {
		return AccessRequest{}, false
	}
	for i := range st.Access {
		if st.Access[i].ID != id {
			continue
		}
		st.Access[i].Status = status
		st.Access[i].ResolvedAt = &now
		st.Access[i].ResolvedBy = by
		if notes != "" {
			st.Access[i].Notes = notes
		}
		return st.Access[i], true
	}
	return AccessRequest{}, false
}

func addMetric(st *State, m Metric, now time.Time) Metric {
	m.ID = st.NextMetricID
	st.NextMetricID++
	m.CreatedAt = now
	st.Metrics = append(st.Metrics, m)
	return m
}

func buildDyadSnapshots(list []DyadRecord, now time.Time) []DyadSnapshot {
	out := make([]DyadSnapshot, 0, len(list))
	cutoff := 5 * time.Minute
	for _, rec := range list {
		state := "unknown"
		ageSec := int64(0)
		if !rec.LastHeartbeat.IsZero() {
			age := now.Sub(rec.LastHeartbeat)
			ageSec = int64(age.Seconds())
			if age <= cutoff {
				state = "online"
			} else {
				state = "stale"
			}
		}
		available := rec.Available
		if !available && rec.UpdatedAt.IsZero() {
			available = true
		}
		out = append(out, DyadSnapshot{
			Dyad:                rec.Dyad,
			Department:          rec.Department,
			Role:                rec.Role,
			Team:                rec.Team,
			Assignment:          rec.Assignment,
			Tags:                rec.Tags,
			Actor:               rec.Actor,
			Critic:              rec.Critic,
			Status:              rec.Status,
			Message:             rec.Message,
			Available:           available,
			State:               state,
			LastHeartbeat:       rec.LastHeartbeat,
			LastHeartbeatAgeSec: ageSec,
			UpdatedAt:           rec.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Department != out[j].Department {
			return out[i].Department < out[j].Department
		}
		return out[i].Dyad < out[j].Dyad
	})
	return out
}

func dyadFromContainer(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	prefixes := []string{"actor-", "critic-", "silexa-actor-", "silexa-critic-"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return strings.TrimPrefix(name, prefix)
		}
	}
	if idx := strings.Index(name, "_"); idx != -1 {
		trimmed := name[idx+1:]
		for _, prefix := range []string{"actor-", "critic-"} {
			if strings.HasPrefix(trimmed, prefix) {
				return strings.TrimPrefix(trimmed, prefix)
			}
		}
	}
	return ""
}
