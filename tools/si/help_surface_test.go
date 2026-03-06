package main

import (
	"strings"
	"testing"
)

func TestHelpSurfaceDoesNotExecuteCommands(t *testing.T) {
	t.Parallel()

	cases := [][]string{
		{"logs", "--help"},
		{"tail", "--help"},
		{"stop", "--help"},
		{"start", "--help"},
		{"status", "help"},
		{"report", "help"},
		{"login", "help"},
		{"swap", "help"},
		{"run", "help"},
		{"persona", "help"},
		{"skill", "help"},
		{"image", "--help"},
		{"images", "--help"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(strings.Join(tc, "_"), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, err := runSICommand(t, map[string]string{}, tc...)
			if err != nil {
				t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
			}
			combined := strings.ToLower(strings.TrimSpace(stdout + "\n" + stderr))
			if !strings.Contains(combined, "usage") {
				t.Fatalf("expected usage output\nstdout=%q\nstderr=%q", stdout, stderr)
			}
		})
	}
}
