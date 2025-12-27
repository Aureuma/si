package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"silexa/agents/manager/internal/state"
)

func isUnknownUpdate(err error) bool {
	return strings.Contains(err.Error(), "unknown update")
}

func (s *server) signalUpdate(ctx context.Context, name string, out any, args ...any) error {
	payload, err := buildSignalPayload(name, args...)
	if err != nil {
		return err
	}
	if err := s.temporal.SignalWorkflow(ctx, s.workflowID, "", name, payload); err != nil {
		return err
	}
	return s.populateFromSignal(ctx, name, out, payload)
}

func buildSignalPayload(name string, args ...any) (any, error) {
	switch name {
	case "upsert_dyad":
		if update, ok := args[0].(state.DyadUpdate); ok {
			return update, nil
		}
	case "heartbeat":
		if hb, ok := args[0].(state.Heartbeat); ok {
			return hb, nil
		}
	case "add_human_task":
		if task, ok := args[0].(state.HumanTask); ok {
			return task, nil
		}
	case "complete_human_task":
		if id, ok := args[0].(int); ok {
			return id, nil
		}
	case "add_dyad_task":
		if task, ok := args[0].(state.DyadTask); ok {
			return task, nil
		}
	case "update_dyad_task":
		if task, ok := args[0].(state.DyadTask); ok {
			return task, nil
		}
	case "claim_dyad_task":
		if len(args) < 3 {
			return nil, fmt.Errorf("claim_dyad_task expects id, dyad, critic")
		}
		id, ok1 := args[0].(int)
		dyad, ok2 := args[1].(string)
		critic, ok3 := args[2].(string)
		if ok1 && ok2 && ok3 {
			return state.DyadTaskClaim{ID: id, Dyad: dyad, Critic: critic}, nil
		}
	case "add_feedback":
		if fb, ok := args[0].(state.Feedback); ok {
			return fb, nil
		}
	case "add_access_request":
		if ar, ok := args[0].(state.AccessRequest); ok {
			return ar, nil
		}
	case "resolve_access_request":
		if len(args) < 4 {
			return nil, fmt.Errorf("resolve_access_request expects id, status, by, notes")
		}
		id, ok1 := args[0].(int)
		status, ok2 := args[1].(string)
		by, ok3 := args[2].(string)
		notes, ok4 := args[3].(string)
		if ok1 && ok2 && ok3 && ok4 {
			return state.AccessResolve{ID: id, Status: status, By: by, Notes: notes}, nil
		}
	case "add_metric":
		if m, ok := args[0].(state.Metric); ok {
			return m, nil
		}
	case "set_dyad_digest_message_id":
		if id, ok := args[0].(int); ok {
			return id, nil
		}
	}
	return nil, fmt.Errorf("unsupported signal payload for %s", name)
}

func (s *server) populateFromSignal(ctx context.Context, name string, out any, args ...any) error {
	if out == nil {
		return nil
	}
	switch name {
	case "upsert_dyad":
		update, ok := args[0].(state.DyadUpdate)
		if !ok {
			return nil
		}
		var dyads []state.DyadSnapshot
		if err := s.query(ctx, "dyads", &dyads); err != nil {
			return err
		}
		for _, snapshot := range dyads {
			if snapshot.Dyad != update.Dyad {
				continue
			}
			if dest, ok := out.(*state.DyadRecord); ok {
				*dest = snapshotToRecord(snapshot)
			}
			break
		}
	case "heartbeat":
		var beats []state.Heartbeat
		if err := s.query(ctx, "beats", &beats); err != nil {
			return err
		}
		if dest, ok := out.(*state.Heartbeat); ok && len(beats) > 0 {
			*dest = beats[len(beats)-1]
		}
	case "add_human_task":
		var tasks []state.HumanTask
		if err := s.query(ctx, "human-tasks", &tasks); err != nil {
			return err
		}
		if dest, ok := out.(*state.HumanTask); ok && len(tasks) > 0 {
			*dest = tasks[len(tasks)-1]
		}
	case "complete_human_task":
		id, ok := args[0].(int)
		if !ok {
			return nil
		}
		var tasks []state.HumanTask
		if err := s.query(ctx, "human-tasks", &tasks); err != nil {
			return err
		}
		if dest, ok := out.(*bool); ok {
			for _, t := range tasks {
				if t.ID == id {
					*dest = t.Status == "done"
					return nil
				}
			}
			*dest = false
		}
	case "add_dyad_task":
		var tasks []state.DyadTask
		if err := s.query(ctx, "dyad-tasks", &tasks); err != nil {
			return err
		}
		if dest, ok := out.(*state.DyadTask); ok && len(tasks) > 0 {
			*dest = tasks[len(tasks)-1]
		}
	case "update_dyad_task":
		task, ok := args[0].(state.DyadTask)
		if !ok {
			return nil
		}
		var tasks []state.DyadTask
		if err := s.query(ctx, "dyad-tasks", &tasks); err != nil {
			return err
		}
		if dest, ok := out.(*state.UpdateResult); ok {
			updated, found := findDyadTask(tasks, task.ID)
			dest.Task = updated
			dest.Found = found
		}
	case "claim_dyad_task":
		payload, ok := args[0].(state.DyadTaskClaim)
		if !ok {
			return nil
		}
		var tasks []state.DyadTask
		if err := s.query(ctx, "dyad-tasks", &tasks); err != nil {
			return err
		}
		if dest, ok := out.(*state.ClaimResult); ok {
			updated, found := findDyadTask(tasks, payload.ID)
			dest.Task = updated
			dest.Found = found
			if found {
				dest.Code = http.StatusOK
			} else {
				dest.Code = http.StatusNotFound
			}
		}
	case "add_feedback":
		var feedback []state.Feedback
		if err := s.query(ctx, "feedback", &feedback); err != nil {
			return err
		}
		if dest, ok := out.(*state.Feedback); ok && len(feedback) > 0 {
			*dest = feedback[len(feedback)-1]
		}
	case "add_access_request":
		var access []state.AccessRequest
		if err := s.query(ctx, "access-requests", &access); err != nil {
			return err
		}
		if dest, ok := out.(*state.AccessRequest); ok && len(access) > 0 {
			*dest = access[len(access)-1]
		}
	case "resolve_access_request":
		payload, ok := args[0].(state.AccessResolve)
		if !ok {
			return nil
		}
		var access []state.AccessRequest
		if err := s.query(ctx, "access-requests", &access); err != nil {
			return err
		}
		if dest, ok := out.(*state.ResolveResult); ok {
			for _, req := range access {
				if req.ID == payload.ID {
					dest.Request = req
					dest.Found = true
					return nil
				}
			}
			dest.Found = false
		}
	case "add_metric":
		var metrics []state.Metric
		if err := s.query(ctx, "metrics", &metrics); err != nil {
			return err
		}
		if dest, ok := out.(*state.Metric); ok && len(metrics) > 0 {
			*dest = metrics[len(metrics)-1]
		}
	case "set_dyad_digest_message_id":
		var id int
		if err := s.query(ctx, "dyad-digest-message-id", &id); err != nil {
			return err
		}
		if dest, ok := out.(*int); ok {
			*dest = id
		}
	}
	return nil
}

func snapshotToRecord(snapshot state.DyadSnapshot) state.DyadRecord {
	return state.DyadRecord{
		Dyad:          snapshot.Dyad,
		Department:    snapshot.Department,
		Role:          snapshot.Role,
		Team:          snapshot.Team,
		Assignment:    snapshot.Assignment,
		Tags:          snapshot.Tags,
		Actor:         snapshot.Actor,
		Critic:        snapshot.Critic,
		Status:        snapshot.Status,
		Message:       snapshot.Message,
		Available:     snapshot.Available,
		LastHeartbeat: snapshot.LastHeartbeat,
		UpdatedAt:     snapshot.UpdatedAt,
	}
}
