package main

import (
	"bytes"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"si/tools/si/internal/stripebridge"
)

func TestPrintSyncPlan(t *testing.T) {
	plan := stripebridge.SyncPlan{
		GeneratedAt: time.Date(2026, 2, 7, 20, 0, 0, 0, time.UTC),
		Actions: []stripebridge.SyncAction{
			{
				Family:    stripebridge.SyncFamilyProducts,
				Action:    stripebridge.SyncActionCreate,
				LiveID:    "prod_live_1",
				SandboxID: "",
				Reason:    "missing in sandbox",
			},
		},
	}
	out := captureStdout(t, func() {
		printSyncPlan(plan)
	})
	if !strings.Contains(out, "Sync plan generated:") {
		t.Fatalf("missing header: %q", out)
	}
	if !strings.Contains(out, "products") {
		t.Fatalf("missing family output: %q", out)
	}
}

func TestStripeSyncNoArgsShowsUsage(t *testing.T) {
	out := captureStdout(t, func() {
		cmdStripeSync(nil)
	})
	if !strings.Contains(out, stripeSyncUsageText) {
		t.Fatalf("expected usage output, got %q", out)
	}
}

func TestStripeSyncModeWithoutActionShowsUsage(t *testing.T) {
	out := captureStdout(t, func() {
		cmdStripeSync([]string{"live-to-sandbox"})
	})
	if !strings.Contains(out, stripeSyncUsageText) {
		t.Fatalf("expected usage output, got %q", out)
	}
}

func TestStripeSyncActionSets(t *testing.T) {
	if len(stripeSyncModeActions) == 0 {
		t.Fatalf("stripe sync mode actions should not be empty")
	}
	if len(stripeSyncActionActions) == 0 {
		t.Fatalf("stripe sync action actions should not be empty")
	}
	modeNames := make([]string, 0, len(stripeSyncModeActions))
	for _, action := range stripeSyncModeActions {
		modeNames = append(modeNames, action.Name)
	}
	if !slices.Contains(modeNames, "live-to-sandbox") {
		t.Fatalf("missing live-to-sandbox mode: %v", modeNames)
	}
	actionNames := make([]string, 0, len(stripeSyncActionActions))
	for _, action := range stripeSyncActionActions {
		actionNames = append(actionNames, action.Name)
	}
	if !slices.Contains(actionNames, "plan") || !slices.Contains(actionNames, "apply") {
		t.Fatalf("missing stripe sync actions: %v", actionNames)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	return buf.String()
}
