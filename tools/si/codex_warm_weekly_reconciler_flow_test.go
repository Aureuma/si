package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReconcileWarmWeeklyProfile_ReadyWhenSignalsPresent(t *testing.T) {
	restore := stubWarmupDeps(t, stubWarmupDepsOptions{
		auth: profileAuthTokens{AccessToken: "x"},
		fetchPayloads: []fetchPayloadResult{
			{payload: usagePayload{RateLimit: &usageRateLimit{Secondary: &usageWindow{
				UsedPercent:        0.0,
				ResetAfterSeconds:  ptrInt64(3600),
				LimitWindowSeconds: ptrInt64(int64((7 * 24 * time.Hour).Seconds())),
			}}}},
		},
	})
	defer restore()

	now := time.Date(2026, 2, 10, 1, 0, 0, 0, time.UTC)
	entry := &warmWeeklyProfileState{}
	out := reconcileWarmWeeklyProfile(now, codexProfile{ID: "america"}, entry, warmWeeklyReconcileOptions{
		ForceBootstrap: false,
		MaxAttempts:    1,
		Prompt:         weeklyWarmPrompt,
		Trigger:        "test",
	}, weeklyWarmExecOptions{Quiet: true})

	if out != "ready" {
		t.Fatalf("expected ready, got %q (err=%q)", out, entry.LastError)
	}
	if entry.LastResult != "ready" {
		t.Fatalf("expected entry ready, got %q", entry.LastResult)
	}
	if entry.NextDue == "" {
		t.Fatalf("expected next due to be set")
	}
	if entry.LastWeeklyReset == "" {
		t.Fatalf("expected last weekly reset to be set")
	}
	if !entry.LastWeeklyUsedOK {
		t.Fatalf("expected used ok to be true")
	}
}

func TestReconcileWarmWeeklyProfile_BootstrapSucceedsWhenResetAppears(t *testing.T) {
	restore := stubWarmupDeps(t, stubWarmupDepsOptions{
		auth: profileAuthTokens{AccessToken: "x"},
		fetchPayloads: []fetchPayloadResult{
			// Before: reset info missing, forcing bootstrap.
			{payload: usagePayload{RateLimit: &usageRateLimit{Secondary: &usageWindow{
				UsedPercent: 0.0,
			}}}},
			// After: reset info appears (even if used percent is unchanged).
			{payload: usagePayload{RateLimit: &usageRateLimit{Secondary: &usageWindow{
				UsedPercent:        0.0,
				ResetAfterSeconds:  ptrInt64(3600),
				LimitWindowSeconds: ptrInt64(int64((7 * 24 * time.Hour).Seconds())),
			}}}},
		},
	})
	defer restore()

	now := time.Date(2026, 2, 10, 1, 0, 0, 0, time.UTC)
	entry := &warmWeeklyProfileState{}
	out := reconcileWarmWeeklyProfile(now, codexProfile{ID: "america"}, entry, warmWeeklyReconcileOptions{
		ForceBootstrap: false,
		MaxAttempts:    1,
		Prompt:         weeklyWarmPrompt,
		Trigger:        "test",
	}, weeklyWarmExecOptions{Quiet: true})

	if out != "warmed" {
		t.Fatalf("expected warmed, got %q (err=%q)", out, entry.LastError)
	}
	if entry.LastWeeklyReset == "" {
		t.Fatalf("expected last weekly reset to be set")
	}
}

func TestReconcileWarmWeeklyProfile_VerifyPollRetriesTransientErrors(t *testing.T) {
	restore := stubWarmupDeps(t, stubWarmupDepsOptions{
		auth: profileAuthTokens{AccessToken: "x"},
		fetchPayloads: []fetchPayloadResult{
			// Before (signals missing).
			{payload: usagePayload{RateLimit: &usageRateLimit{Secondary: &usageWindow{UsedPercent: 0.0}}}},
			// After: fail twice, then succeed with reset info.
			{err: errors.New("transient")},
			{err: errors.New("transient")},
			{payload: usagePayload{RateLimit: &usageRateLimit{Secondary: &usageWindow{
				UsedPercent:        0.0,
				ResetAfterSeconds:  ptrInt64(3600),
				LimitWindowSeconds: ptrInt64(int64((7 * 24 * time.Hour).Seconds())),
			}}}},
		},
	})
	defer restore()

	now := time.Date(2026, 2, 10, 1, 0, 0, 0, time.UTC)
	entry := &warmWeeklyProfileState{}
	out := reconcileWarmWeeklyProfile(now, codexProfile{ID: "america"}, entry, warmWeeklyReconcileOptions{
		ForceBootstrap: false,
		MaxAttempts:    1,
		Prompt:         weeklyWarmPrompt,
		Trigger:        "test",
	}, weeklyWarmExecOptions{Quiet: true})

	if out != "warmed" {
		t.Fatalf("expected warmed, got %q (err=%q)", out, entry.LastError)
	}
}

func TestReconcileWarmWeeklyProfile_WarmsWhenWindowAdvances(t *testing.T) {
	runCalls := 0
	nextReset := time.Date(2026, 2, 21, 1, 32, 26, 0, time.UTC)
	restore := stubWarmupDeps(t, stubWarmupDepsOptions{
		auth: profileAuthTokens{AccessToken: "x"},
		fetchPayloads: []fetchPayloadResult{
			{payload: usagePayload{RateLimit: &usageRateLimit{Secondary: &usageWindow{
				UsedPercent:        0.0,
				ResetAt:            ptrInt64(nextReset.Unix()),
				LimitWindowSeconds: ptrInt64(int64((7 * 24 * time.Hour).Seconds())),
			}}}},
			{payload: usagePayload{RateLimit: &usageRateLimit{Secondary: &usageWindow{
				UsedPercent:        0.0,
				ResetAt:            ptrInt64(nextReset.Unix()),
				LimitWindowSeconds: ptrInt64(int64((7 * 24 * time.Hour).Seconds())),
			}}}},
		},
		runCalls: &runCalls,
	})
	defer restore()

	now := time.Date(2026, 2, 14, 1, 0, 0, 0, time.UTC)
	entry := &warmWeeklyProfileState{
		LastWeeklyReset: "2026-02-13T14:33:56Z",
		LastResult:      "ready",
	}
	out := reconcileWarmWeeklyProfile(now, codexProfile{ID: "einsteina"}, entry, warmWeeklyReconcileOptions{
		ForceBootstrap: false,
		MaxAttempts:    1,
		Prompt:         weeklyWarmPrompt,
		Trigger:        "test",
	}, weeklyWarmExecOptions{Quiet: true})

	if out != "warmed" {
		t.Fatalf("expected warmed on weekly window advance, got %q (err=%q)", out, entry.LastError)
	}
	if runCalls != 1 {
		t.Fatalf("expected one warm run, got %d", runCalls)
	}
}

type fetchPayloadResult struct {
	payload usagePayload
	err     error
}

type stubWarmupDepsOptions struct {
	auth          profileAuthTokens
	fetchPayloads []fetchPayloadResult
	runErr        error
	runCalls      *int
}

func stubWarmupDeps(t *testing.T, opts stubWarmupDepsOptions) func() {
	t.Helper()

	origLoadAuth := loadProfileAuthTokensFn
	origFetch := fetchUsagePayloadFn
	origRun := runWeeklyWarmPromptFn

	loadProfileAuthTokensFn = func(profile codexProfile) (profileAuthTokens, error) {
		return opts.auth, nil
	}
	runWeeklyWarmPromptFn = func(profile codexProfile, prompt string, execOpts weeklyWarmExecOptions) error {
		if opts.runCalls != nil {
			*opts.runCalls = *opts.runCalls + 1
		}
		return opts.runErr
	}

	queue := append([]fetchPayloadResult(nil), opts.fetchPayloads...)
	fetchUsagePayloadFn = func(ctx context.Context, url string, auth profileAuthTokens) (usagePayload, error) {
		_ = url
		_ = auth
		if len(queue) == 0 {
			t.Fatalf("fetchUsagePayload called more times than expected")
		}
		next := queue[0]
		queue = queue[1:]
		if next.err != nil {
			return usagePayload{}, next.err
		}
		return next.payload, nil
	}

	return func() {
		loadProfileAuthTokensFn = origLoadAuth
		fetchUsagePayloadFn = origFetch
		runWeeklyWarmPromptFn = origRun
	}
}

func ptrInt64(v int64) *int64 { return &v }
