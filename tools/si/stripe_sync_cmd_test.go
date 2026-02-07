package main

import (
	"bytes"
	"io"
	"os"
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
