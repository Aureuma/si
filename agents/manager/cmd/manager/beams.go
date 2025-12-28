package main

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"silexa/agents/manager/internal/beam"
	"silexa/agents/manager/internal/state"
)

func (s *server) maybeStartBeamWorkflow(ctx context.Context, task state.DyadTask) {
	if !shouldStartBeam(task) {
		return
	}
	req := beam.Request{
		TaskID: task.ID,
		Kind:   task.Kind,
		Dyad:   task.Dyad,
		Actor:  task.Actor,
		Critic: task.Critic,
	}
	options := client.StartWorkflowOptions{
		ID:        beam.WorkflowID(task.Kind, task.ID),
		TaskQueue: s.taskQueue,
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
	}
	_, err := s.temporal.ExecuteWorkflow(ctx, options, beam.BeamWorkflow, req)
	if err == nil {
		return
	}
	var already *serviceerror.WorkflowExecutionAlreadyStarted
	if errors.As(err, &already) {
		return
	}
	s.logger.Printf("beam start error (task=%d kind=%s): %v", task.ID, task.Kind, err)
}

func shouldStartBeam(task state.DyadTask) bool {
	if task.ID <= 0 {
		return false
	}
	kind := strings.ToLower(strings.TrimSpace(task.Kind))
	if !strings.HasPrefix(kind, "beam.") {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(task.Status))
	return status != "done"
}

func (s *server) startBeamReconciler() {
	interval := durationEnv("BEAM_RECONCILE_INTERVAL", time.Minute)
	if interval <= 0 {
		return
	}
	logger := s.logger
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			var tasks []state.DyadTask
			if err := s.query(ctx, "dyad-tasks", &tasks); err != nil {
				cancel()
				logger.Printf("beam reconcile query error: %v", err)
				continue
			}
			for _, task := range tasks {
				s.maybeStartBeamWorkflow(ctx, task)
			}
			cancel()
		}
	}()
}

func durationEnv(key string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		log.Printf("invalid duration %s=%q: %v", key, raw, err)
		return def
	}
	return d
}
