package beam

import (
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"silexa/agents/manager/internal/state"
)

const (
	activityFetchDyadTask      = "FetchDyadTask"
	activityCheckCodexLogin    = "CheckCodexLogin"
	activityStartCodexLogin    = "StartCodexLogin"
	activityStartSocatForward  = "StartSocatForwarder"
	activityStopSocatForward   = "StopSocatForwarder"
	activitySendTelegram       = "SendTelegram"
	activityApplyDyadResources = "ApplyDyadResources"
	activityWaitDyadReady      = "WaitDyadReady"
	activityResetCodexState    = "ResetCodexState"
	activityHasOpenDyadTask    = "HasOpenDyadTask"
	activityCreateDyadTask     = "CreateDyadTask"
)

func BeamWorkflow(ctx workflow.Context, req Request) error {
	kind := normalizeKind(req.Kind)
	switch kind {
	case KindCodexLogin:
		return codexLoginWorkflow(ctx, req)
	case KindDyadBootstrap:
		return dyadBootstrapWorkflow(ctx, req)
	case KindCodexAccountReset:
		return codexAccountResetWorkflow(ctx, req)
	default:
		if req.TaskID <= 0 {
			return nil
		}
		activityOpts := workflow.ActivityOptions{
			StartToCloseTimeout: 30 * time.Second,
			RetryPolicy: &temporal.RetryPolicy{
				MaximumAttempts: 1,
			},
		}
		var current state.DyadTask
		if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, activityOpts), activityFetchDyadTask, req.TaskID).Get(ctx, &current); err != nil {
			_ = signalUpdateTask(ctx, state.DyadTask{
				ID:     req.TaskID,
				Status: "blocked",
				Notes:  fmt.Sprintf("[beam] unsupported kind: %s", req.Kind),
			})
			return nil
		}
		note := ensureNote(current.Notes, fmt.Sprintf("[beam] unsupported kind: %s", req.Kind))
		_ = signalUpdateTask(ctx, state.DyadTask{ID: req.TaskID, Status: "blocked", Notes: note})
		return nil
	}
}

func dyadBootstrapWorkflow(ctx workflow.Context, req Request) error {
	logger := workflow.GetLogger(ctx)
	if req.TaskID <= 0 {
		return fmt.Errorf("task id required")
	}
	dyad := strings.TrimSpace(req.Dyad)
	if dyad == "" {
		_ = signalUpdateTask(ctx, state.DyadTask{
			ID:     req.TaskID,
			Status: "blocked",
			Notes:  appendNote("", "[beam.dyad_bootstrap] missing dyad assignment"),
		})
		return fmt.Errorf("dyad required")
	}

	_ = signalClaimTask(ctx, state.DyadTaskClaim{
		ID:     req.TaskID,
		Dyad:   dyad,
		Critic: "temporal-beam",
	})
	_ = signalUpdateTask(ctx, state.DyadTask{
		ID:     req.TaskID,
		Status: "in_progress",
	})

	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 6 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    5,
		},
	}
	noRetryOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	}

	var current state.DyadTask
	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, activityOpts), activityFetchDyadTask, req.TaskID).Get(ctx, &current); err != nil {
		logger.Error("fetch dyad task", "error", err)
		return err
	}

	stateLines := parseState(current.Notes)
	role := strings.TrimSpace(stateLines["beam.dyad_bootstrap.role"])
	dept := strings.TrimSpace(stateLines["beam.dyad_bootstrap.department"])
	if role == "" {
		role = "generic"
	}
	if dept == "" {
		dept = role
	}

	avail := true
	_ = signalUpsertDyad(ctx, state.DyadUpdate{
		Dyad:       dyad,
		Role:       role,
		Department: dept,
		Actor:      req.Actor,
		Critic:     req.Critic,
		Status:     "bootstrapping",
		Available:  &avail,
	})

	applyReq := DyadBootstrapRequest{
		Dyad:       dyad,
		Role:       role,
		Department: dept,
		Actor:      req.Actor,
		Critic:     req.Critic,
	}
	var result DyadBootstrapResult
	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, activityOpts), activityApplyDyadResources, applyReq).Get(ctx, &result); err != nil {
		notes := ensureNote(current.Notes, "[beam.dyad_bootstrap] apply failed: "+err.Error())
		_ = signalUpdateTask(ctx, state.DyadTask{ID: req.TaskID, Status: "blocked", Notes: notes})
		_ = signalUpsertDyad(ctx, state.DyadUpdate{
			Dyad:   dyad,
			Status: "bootstrap_failed",
		})
		return err
	}

	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, activityOpts), activityWaitDyadReady, applyReq).Get(ctx, nil); err != nil {
		notes := ensureNote(current.Notes, "[beam.dyad_bootstrap] readiness failed: "+err.Error())
		_ = signalUpdateTask(ctx, state.DyadTask{ID: req.TaskID, Status: "blocked", Notes: notes})
		_ = signalUpsertDyad(ctx, state.DyadUpdate{
			Dyad:   dyad,
			Status: "bootstrap_failed",
		})
		return err
	}

	readyNote := fmt.Sprintf("[beam.dyad_bootstrap] containers ready (actor=%s critic=%s)", result.ActorContainer, result.CriticContainer)
	_ = signalUpdateTask(ctx, state.DyadTask{ID: req.TaskID, Status: "done", Notes: ensureNote(current.Notes, readyNote)})
	_ = signalUpsertDyad(ctx, state.DyadUpdate{
		Dyad:   dyad,
		Status: "active",
	})

	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, noRetryOpts), activityFetchDyadTask, req.TaskID).Get(ctx, &current); err != nil {
		logger.Warn("refresh dyad task after bootstrap", "error", err)
	}
	return nil
}

func codexLoginWorkflow(ctx workflow.Context, req Request) error {
	logger := workflow.GetLogger(ctx)
	if req.TaskID <= 0 {
		return fmt.Errorf("task id required")
	}
	dyad := strings.TrimSpace(req.Dyad)
	actor := normalizeContainerName(req.Actor)
	if actor == "" {
		actor = "actor"
	}
	if dyad == "" {
		_ = signalUpdateTask(ctx, state.DyadTask{
			ID:     req.TaskID,
			Status: "blocked",
			Notes:  appendNote("", "[beam.codex_login] missing dyad assignment"),
		})
		return fmt.Errorf("dyad required")
	}

	_ = signalClaimTask(ctx, state.DyadTaskClaim{
		ID:     req.TaskID,
		Dyad:   dyad,
		Critic: "temporal-beam",
	})
	_ = signalUpdateTask(ctx, state.DyadTask{
		ID:     req.TaskID,
		Status: "in_progress",
	})

	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    20 * time.Second,
			MaximumAttempts:    5,
		},
	}
	noRetryOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	}

	var current state.DyadTask
	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, activityOpts), activityFetchDyadTask, req.TaskID).Get(ctx, &current); err != nil {
		logger.Error("fetch dyad task", "error", err)
		return err
	}
	stateLines := parseState(current.Notes)
	port := atoiDefault(stateLines["beam.codex_login.port"], 0)
	forwardPort := atoiDefault(stateLines["beam.codex_login.forward_port"], 0)

	var loginStatus CodexLoginStatus
	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, activityOpts), activityCheckCodexLogin, CodexLoginCheck{
		Dyad:  dyad,
		Actor: actor,
	}).Get(ctx, &loginStatus); err != nil {
		return err
	}
	if loginStatus.LoggedIn {
		notes := ensureNote(current.Notes, "[beam.codex_login] already logged in")
		_ = signalUpdateTask(ctx, state.DyadTask{ID: req.TaskID, Status: "done", Notes: notes})
		return nil
	}

	var startInfo CodexLoginStart
	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, noRetryOpts), activityStartCodexLogin, CodexLoginRequest{
		Dyad:        dyad,
		Actor:       actor,
		Port:        port,
		ForwardPort: forwardPort,
	}).Get(ctx, &startInfo); err != nil {
		notes := ensureNote(current.Notes, "[beam.codex_login] failed to start login: "+err.Error())
		_ = signalUpdateTask(ctx, state.DyadTask{ID: req.TaskID, Status: "blocked", Notes: notes})
		return err
	}

	forwardName := fmt.Sprintf("%s-codex-forward-%d", actor, startInfo.Port)
	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, noRetryOpts), activityStartSocatForward, SocatForwarderRequest{
		Dyad:       dyad,
		Actor:      actor,
		Name:       forwardName,
		ListenPort: startInfo.ForwardPort,
		TargetPort: startInfo.Port,
	}).Get(ctx, nil); err != nil {
		notes := ensureNote(current.Notes, "[beam.codex_login] socat forward failed: "+err.Error())
		_ = signalUpdateTask(ctx, state.DyadTask{ID: req.TaskID, Status: "blocked", Notes: notes})
		return err
	}

	message := buildTelegramMessage(startInfo.HostPort, startInfo.AuthURL)
	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, noRetryOpts), activitySendTelegram, TelegramMessage{
		Message: message,
	}).Get(ctx, nil); err != nil {
		notes := ensureNote(current.Notes, "[beam.codex_login] telegram notify failed: "+err.Error())
		_ = signalUpdateTask(ctx, state.DyadTask{ID: req.TaskID, Status: "blocked", Notes: notes})
		return err
	}

	waitNote := fmt.Sprintf("[beam.codex_login] sent host port + URL to telegram (dyad=%s); waiting for browser callback", dyad)
	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, activityOpts), activityFetchDyadTask, req.TaskID).Get(ctx, &current); err != nil {
		logger.Warn("refresh dyad task before wait", "error", err)
	}
	notes := ensureNote(current.Notes, waitNote)
	_ = signalUpdateTask(ctx, state.DyadTask{ID: req.TaskID, Status: "blocked", Notes: notes})

	deadline := workflow.Now(ctx).Add(20 * time.Minute)
	pollInterval := 15 * time.Second
	for {
		if workflow.Now(ctx).After(deadline) {
			if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, activityOpts), activityFetchDyadTask, req.TaskID).Get(ctx, &current); err != nil {
				logger.Warn("refresh dyad task before timeout", "error", err)
			}
			timeoutNote := ensureNote(current.Notes, "[beam.codex_login] timed out waiting for browser callback")
			_ = signalUpdateTask(ctx, state.DyadTask{ID: req.TaskID, Status: "blocked", Notes: timeoutNote})
			_ = workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, noRetryOpts), activityStopSocatForward, SocatForwarderStop{
				Dyad:  dyad,
				Actor: actor,
				Name:  forwardName,
			}).Get(ctx, nil)
			return fmt.Errorf("codex login timeout for dyad %s", dyad)
		}
		workflow.Sleep(ctx, pollInterval)
		if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, activityOpts), activityCheckCodexLogin, CodexLoginCheck{
			Dyad:  dyad,
			Actor: actor,
		}).Get(ctx, &loginStatus); err != nil {
			logger.Warn("codex login status check failed", "error", err)
			continue
		}
		if loginStatus.LoggedIn {
			_ = workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, noRetryOpts), activityStopSocatForward, SocatForwarderStop{
				Dyad:  dyad,
				Actor: actor,
				Name:  forwardName,
			}).Get(ctx, nil)
			if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, activityOpts), activityFetchDyadTask, req.TaskID).Get(ctx, &current); err != nil {
				logger.Warn("refresh dyad task before completion", "error", err)
			}
			doneNote := ensureNote(current.Notes, "[beam.codex_login] login completed")
			_ = signalUpdateTask(ctx, state.DyadTask{ID: req.TaskID, Status: "done", Notes: doneNote})
			return nil
		}
	}
}

func codexAccountResetWorkflow(ctx workflow.Context, req Request) error {
	logger := workflow.GetLogger(ctx)
	if req.TaskID <= 0 {
		return fmt.Errorf("task id required")
	}
	dyad := strings.TrimSpace(req.Dyad)
	if dyad == "" {
		_ = signalUpdateTask(ctx, state.DyadTask{
			ID:     req.TaskID,
			Status: "blocked",
			Notes:  appendNote("", "[beam.codex_account_reset] missing dyad assignment"),
		})
		return fmt.Errorf("dyad required")
	}

	_ = signalClaimTask(ctx, state.DyadTaskClaim{
		ID:     req.TaskID,
		Dyad:   dyad,
		Critic: "temporal-beam",
	})
	_ = signalUpdateTask(ctx, state.DyadTask{
		ID:     req.TaskID,
		Status: "in_progress",
	})

	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 3 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    20 * time.Second,
			MaximumAttempts:    3,
		},
	}
	noRetryOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	}

	var current state.DyadTask
	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, activityOpts), activityFetchDyadTask, req.TaskID).Get(ctx, &current); err != nil {
		logger.Error("fetch dyad task", "error", err)
		return err
	}
	stateLines := parseState(current.Notes)
	targets := parseCSVList(stateLines["beam.codex_account_reset.targets"])
	paths := parseCSVList(stateLines["beam.codex_account_reset.paths"])
	if len(targets) == 0 {
		targets = []string{"actor", "critic"}
	}
	if len(paths) == 0 {
		paths = defaultCodexResetPaths()
	}

	var result CodexResetResult
	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, activityOpts), activityResetCodexState, CodexResetRequest{
		Dyad:    dyad,
		Targets: targets,
		Paths:   paths,
	}).Get(ctx, &result); err != nil {
		notes := ensureNote(current.Notes, "[beam.codex_account_reset] reset failed: "+err.Error())
		_ = signalUpdateTask(ctx, state.DyadTask{ID: req.TaskID, Status: "blocked", Notes: notes})
		return err
	}

	loginDelay := 30 * time.Second
	workflow.Sleep(ctx, loginDelay)

	loginNote := "[beam.codex_account_reset] login task already open"
	open := false
	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, activityOpts), activityHasOpenDyadTask, DyadTaskCheck{
		Dyad: dyad,
		Kind: KindCodexLogin,
	}).Get(ctx, &open); err != nil {
		logger.Warn("login task check failed", "error", err)
		loginNote = "[beam.codex_account_reset] login task check failed"
	} else if !open {
		err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, activityOpts), activityCreateDyadTask, state.DyadTask{
			Title:       fmt.Sprintf("Beam: Codex login for %s", dyad),
			Description: "Authenticate Codex CLI after account reset.",
			Kind:        KindCodexLogin,
			Priority:    "high",
			Dyad:        dyad,
			Actor:       "actor",
			Critic:      "critic",
			RequestedBy: "beam.codex_account_reset",
			Notes:       "[beam.codex_login.reason]=account_reset",
		}).Get(ctx, nil)
		if err != nil {
			logger.Warn("login task create failed", "error", err)
			loginNote = "[beam.codex_account_reset] failed to create login task"
		} else {
			loginNote = fmt.Sprintf("[beam.codex_account_reset] queued login task after %s", loginDelay)
		}
	}

	note := fmt.Sprintf("[beam.codex_account_reset] cleared %s", strings.Join(result.Targets, ","))
	if len(result.Paths) > 0 {
		note = fmt.Sprintf("%s (paths=%s)", note, strings.Join(result.Paths, ","))
	}
	note = ensureNote(note, loginNote)
	if err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, noRetryOpts), activityFetchDyadTask, req.TaskID).Get(ctx, &current); err != nil {
		logger.Warn("refresh dyad task after reset", "error", err)
	}
	_ = signalUpdateTask(ctx, state.DyadTask{
		ID:     req.TaskID,
		Status: "done",
		Notes:  ensureNote(current.Notes, note),
	})
	return nil
}

func signalUpdateTask(ctx workflow.Context, task state.DyadTask) error {
	fut := workflow.SignalExternalWorkflow(ctx, state.WorkflowID, "", "update_dyad_task", task)
	return fut.Get(ctx, nil)
}

func signalClaimTask(ctx workflow.Context, claim state.DyadTaskClaim) error {
	fut := workflow.SignalExternalWorkflow(ctx, state.WorkflowID, "", "claim_dyad_task", claim)
	return fut.Get(ctx, nil)
}

func signalUpsertDyad(ctx workflow.Context, update state.DyadUpdate) error {
	fut := workflow.SignalExternalWorkflow(ctx, state.WorkflowID, "", "upsert_dyad", update)
	return fut.Get(ctx, nil)
}

func appendNote(existing string, note string) string {
	existing = strings.TrimSpace(existing)
	note = strings.TrimSpace(note)
	if note == "" {
		return existing
	}
	if existing == "" {
		return note
	}
	return strings.TrimSpace(existing + "\n" + note)
}

func ensureNote(existing string, note string) string {
	existing = strings.TrimSpace(existing)
	note = strings.TrimSpace(note)
	if note == "" {
		return existing
	}
	if strings.Contains(existing, note) {
		return existing
	}
	return appendNote(existing, note)
}
