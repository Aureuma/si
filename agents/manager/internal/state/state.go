package state

import (
	"sort"
	"strings"
	"time"

	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowID = "silexa-state"
	TaskQueue  = "silexa-state"
)

func StateWorkflow(ctx workflow.Context, input State) error {
	st := input
	if st.StartedAt.IsZero() {
		st.StartedAt = workflow.Now(ctx).UTC()
	}
	if st.NextTaskID == 0 {
		st.NextTaskID = 1
	}
	if st.NextDyadTaskID == 0 {
		st.NextDyadTaskID = 1
	}
	if st.NextFeedbackID == 0 {
		st.NextFeedbackID = 1
	}
	if st.NextAccessID == 0 {
		st.NextAccessID = 1
	}
	if st.NextMetricID == 0 {
		st.NextMetricID = 1
	}

	_ = workflow.SetQueryHandler(ctx, "dyads", func() ([]DyadSnapshot, error) {
		return buildDyadSnapshots(st.Dyads, workflow.Now(ctx).UTC()), nil
	})
	_ = workflow.SetQueryHandler(ctx, "beats", func() ([]Heartbeat, error) {
		return append([]Heartbeat(nil), st.Beats...), nil
	})
	_ = workflow.SetQueryHandler(ctx, "human-tasks", func() ([]HumanTask, error) {
		return append([]HumanTask(nil), st.Tasks...), nil
	})
	_ = workflow.SetQueryHandler(ctx, "dyad-tasks", func() ([]DyadTask, error) {
		return append([]DyadTask(nil), st.DyadTasks...), nil
	})
	_ = workflow.SetQueryHandler(ctx, "feedback", func() ([]Feedback, error) {
		return append([]Feedback(nil), st.Feedbacks...), nil
	})
	_ = workflow.SetQueryHandler(ctx, "access-requests", func() ([]AccessRequest, error) {
		return append([]AccessRequest(nil), st.Access...), nil
	})
	_ = workflow.SetQueryHandler(ctx, "metrics", func() ([]Metric, error) {
		return append([]Metric(nil), st.Metrics...), nil
	})
	_ = workflow.SetQueryHandler(ctx, "dyad-digest-message-id", func() (int, error) {
		return st.DyadDigestMessageID, nil
	})
	_ = workflow.SetQueryHandler(ctx, "healthz", func() (map[string]interface{}, error) {
		openTasks := 0
		for _, t := range st.Tasks {
			if t.Status != "done" {
				openTasks++
			}
		}
		pendingAccess := 0
		for _, a := range st.Access {
			if a.Status == "pending" {
				pendingAccess++
			}
		}
		beatsCount := len(st.Beats)
		metricsCount := len(st.Metrics)
		uptime := int64(0)
		if !st.StartedAt.IsZero() {
			uptime = int64(workflow.Now(ctx).UTC().Sub(st.StartedAt).Seconds())
		}
		resp := map[string]interface{}{
			"status":         "ok",
			"tasks_open":     openTasks,
			"access_pending": pendingAccess,
			"metrics_count":  metricsCount,
			"beats_recent":   beatsCount,
			"uptime_seconds": uptime,
		}
		if beatsCount > 0 {
			last := st.Beats[beatsCount-1].When
			resp["last_beat"] = last.UTC()
		}
		return resp, nil
	})

	_ = workflow.SetUpdateHandler(ctx, "upsert_dyad", func(update DyadUpdate) (DyadRecord, error) {
		now := workflow.Now(ctx).UTC()
		return upsertDyad(&st, update, now), nil
	})
	_ = workflow.SetUpdateHandler(ctx, "heartbeat", func(hb Heartbeat) (Heartbeat, error) {
		now := workflow.Now(ctx).UTC()
		hb.When = now
		addHeartbeat(&st, hb, now)
		return hb, nil
	})
	_ = workflow.SetUpdateHandler(ctx, "add_human_task", func(task HumanTask) (HumanTask, error) {
		now := workflow.Now(ctx).UTC()
		return addHumanTask(&st, task, now), nil
	})
	_ = workflow.SetUpdateHandler(ctx, "complete_human_task", func(id int) (bool, error) {
		now := workflow.Now(ctx).UTC()
		return completeHumanTask(&st, id, now), nil
	})
	_ = workflow.SetUpdateHandler(ctx, "add_dyad_task", func(task DyadTask) (DyadTask, error) {
		now := workflow.Now(ctx).UTC()
		return addDyadTask(&st, task, now), nil
	})
	_ = workflow.SetUpdateHandler(ctx, "update_dyad_task", func(task DyadTask) (UpdateResult, error) {
		now := workflow.Now(ctx).UTC()
		updated, ok := updateDyadTask(&st, task, now)
		return UpdateResult{Task: updated, Found: ok}, nil
	})
	_ = workflow.SetUpdateHandler(ctx, "claim_dyad_task", func(id int, dyad string, critic string) (ClaimResult, error) {
		now := workflow.Now(ctx).UTC()
		updated, code, ok := claimDyadTask(&st, id, dyad, critic, now)
		return ClaimResult{Task: updated, Code: code, Found: ok}, nil
	})
	_ = workflow.SetUpdateHandler(ctx, "add_feedback", func(fb Feedback) (Feedback, error) {
		now := workflow.Now(ctx).UTC()
		return addFeedback(&st, fb, now), nil
	})
	_ = workflow.SetUpdateHandler(ctx, "add_access_request", func(ar AccessRequest) (AccessRequest, error) {
		now := workflow.Now(ctx).UTC()
		return addAccessRequest(&st, ar, now), nil
	})
	_ = workflow.SetUpdateHandler(ctx, "resolve_access_request", func(id int, status string, by string, notes string) (ResolveResult, error) {
		now := workflow.Now(ctx).UTC()
		updated, ok := resolveAccessRequest(&st, id, status, by, notes, now)
		return ResolveResult{Request: updated, Found: ok}, nil
	})
	_ = workflow.SetUpdateHandler(ctx, "add_metric", func(m Metric) (Metric, error) {
		now := workflow.Now(ctx).UTC()
		return addMetric(&st, m, now), nil
	})
	_ = workflow.SetUpdateHandler(ctx, "set_dyad_digest_message_id", func(id int) (int, error) {
		st.DyadDigestMessageID = id
		return st.DyadDigestMessageID, nil
	})

	upsertDyadCh := workflow.GetSignalChannel(ctx, "upsert_dyad")
	heartbeatCh := workflow.GetSignalChannel(ctx, "heartbeat")
	addHumanTaskCh := workflow.GetSignalChannel(ctx, "add_human_task")
	completeHumanTaskCh := workflow.GetSignalChannel(ctx, "complete_human_task")
	addDyadTaskCh := workflow.GetSignalChannel(ctx, "add_dyad_task")
	updateDyadTaskCh := workflow.GetSignalChannel(ctx, "update_dyad_task")
	claimDyadTaskCh := workflow.GetSignalChannel(ctx, "claim_dyad_task")
	addFeedbackCh := workflow.GetSignalChannel(ctx, "add_feedback")
	addAccessRequestCh := workflow.GetSignalChannel(ctx, "add_access_request")
	resolveAccessRequestCh := workflow.GetSignalChannel(ctx, "resolve_access_request")
	addMetricCh := workflow.GetSignalChannel(ctx, "add_metric")
	setDyadDigestMessageIDCh := workflow.GetSignalChannel(ctx, "set_dyad_digest_message_id")

	for {
		selector := workflow.NewSelector(ctx)
		selector.AddReceive(upsertDyadCh, func(c workflow.ReceiveChannel, _ bool) {
			var update DyadUpdate
			c.Receive(ctx, &update)
			now := workflow.Now(ctx).UTC()
			upsertDyad(&st, update, now)
		})
		selector.AddReceive(heartbeatCh, func(c workflow.ReceiveChannel, _ bool) {
			var hb Heartbeat
			c.Receive(ctx, &hb)
			now := workflow.Now(ctx).UTC()
			hb.When = now
			addHeartbeat(&st, hb, now)
		})
		selector.AddReceive(addHumanTaskCh, func(c workflow.ReceiveChannel, _ bool) {
			var task HumanTask
			c.Receive(ctx, &task)
			now := workflow.Now(ctx).UTC()
			addHumanTask(&st, task, now)
		})
		selector.AddReceive(completeHumanTaskCh, func(c workflow.ReceiveChannel, _ bool) {
			var id int
			c.Receive(ctx, &id)
			now := workflow.Now(ctx).UTC()
			completeHumanTask(&st, id, now)
		})
		selector.AddReceive(addDyadTaskCh, func(c workflow.ReceiveChannel, _ bool) {
			var task DyadTask
			c.Receive(ctx, &task)
			now := workflow.Now(ctx).UTC()
			addDyadTask(&st, task, now)
		})
		selector.AddReceive(updateDyadTaskCh, func(c workflow.ReceiveChannel, _ bool) {
			var task DyadTask
			c.Receive(ctx, &task)
			now := workflow.Now(ctx).UTC()
			updateDyadTask(&st, task, now)
		})
		selector.AddReceive(claimDyadTaskCh, func(c workflow.ReceiveChannel, _ bool) {
			var payload DyadTaskClaim
			c.Receive(ctx, &payload)
			now := workflow.Now(ctx).UTC()
			claimDyadTask(&st, payload.ID, payload.Dyad, payload.Critic, now)
		})
		selector.AddReceive(addFeedbackCh, func(c workflow.ReceiveChannel, _ bool) {
			var fb Feedback
			c.Receive(ctx, &fb)
			now := workflow.Now(ctx).UTC()
			addFeedback(&st, fb, now)
		})
		selector.AddReceive(addAccessRequestCh, func(c workflow.ReceiveChannel, _ bool) {
			var ar AccessRequest
			c.Receive(ctx, &ar)
			now := workflow.Now(ctx).UTC()
			addAccessRequest(&st, ar, now)
		})
		selector.AddReceive(resolveAccessRequestCh, func(c workflow.ReceiveChannel, _ bool) {
			var payload AccessResolve
			c.Receive(ctx, &payload)
			now := workflow.Now(ctx).UTC()
			resolveAccessRequest(&st, payload.ID, payload.Status, payload.By, payload.Notes, now)
		})
		selector.AddReceive(addMetricCh, func(c workflow.ReceiveChannel, _ bool) {
			var m Metric
			c.Receive(ctx, &m)
			now := workflow.Now(ctx).UTC()
			addMetric(&st, m, now)
		})
		selector.AddReceive(setDyadDigestMessageIDCh, func(c workflow.ReceiveChannel, _ bool) {
			var id int
			c.Receive(ctx, &id)
			st.DyadDigestMessageID = id
		})
		selector.AddFuture(workflow.NewTimer(ctx, time.Hour), func(f workflow.Future) {
			_ = f.Get(ctx, nil)
		})
		selector.Select(ctx)
	}
}

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
