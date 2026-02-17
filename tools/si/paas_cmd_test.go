package main

import (
	"encoding/json"
	"strings"
	"testing"
)

type paasTestEnvelope struct {
	OK      bool              `json:"ok"`
	Command string            `json:"command"`
	Context string            `json:"context"`
	Mode    string            `json:"mode"`
	Fields  map[string]string `json:"fields"`
}

type paasTargetListPayload struct {
	OK            bool         `json:"ok"`
	Command       string       `json:"command"`
	Context       string       `json:"context"`
	Mode          string       `json:"mode"`
	CurrentTarget string       `json:"current_target"`
	Count         int          `json:"count"`
	Data          []paasTarget `json:"data"`
}

func TestPaasNoArgsShowsUsageInNonInteractiveMode(t *testing.T) {
	out := captureStdout(t, func() {
		cmdPaas(nil)
	})
	if !strings.Contains(out, paasUsageText) {
		t.Fatalf("expected usage output, got %q", out)
	}
}

func TestPaasSubcommandNoArgsShowsUsageInNonInteractiveMode(t *testing.T) {
	tests := []struct {
		name   string
		invoke func()
		usage  string
	}{
		{name: "target", invoke: func() { cmdPaasTarget(nil) }, usage: paasTargetUsageText},
		{name: "app", invoke: func() { cmdPaasApp(nil) }, usage: paasAppUsageText},
		{name: "alert", invoke: func() { cmdPaasAlert(nil) }, usage: paasAlertUsageText},
		{name: "ai", invoke: func() { cmdPaasAI(nil) }, usage: paasAIUsageText},
		{name: "context", invoke: func() { cmdPaasContext(nil) }, usage: paasContextUsageText},
		{name: "agent", invoke: func() { cmdPaasAgent(nil) }, usage: paasAgentUsageText},
		{name: "events", invoke: func() { cmdPaasEvents(nil) }, usage: paasEventsUsageText},
	}
	for _, tc := range tests {
		out := captureStdout(t, tc.invoke)
		if !strings.Contains(out, tc.usage) {
			t.Fatalf("%s expected usage output, got %q", tc.name, out)
		}
	}
}

func TestPaasJSONOutputContractTargetAdd(t *testing.T) {
	out := captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-1", "--host", "10.0.0.4", "--user", "root", "--json"})
	})
	env := parsePaasEnvelope(t, out)
	if !env.OK {
		t.Fatalf("expected ok=true envelope: %#v", env)
	}
	if env.Command != "target add" {
		t.Fatalf("expected command=target add, got %q", env.Command)
	}
	if env.Mode != "scaffold" {
		t.Fatalf("expected mode=scaffold, got %q", env.Mode)
	}
	if env.Context != defaultPaasContext {
		t.Fatalf("expected default context, got %q", env.Context)
	}
	if env.Fields["name"] != "edge-1" {
		t.Fatalf("expected name field, got %#v", env.Fields)
	}
	if env.Fields["host"] != "10.0.0.4" {
		t.Fatalf("expected host field, got %#v", env.Fields)
	}
	if env.Fields["user"] != "root" {
		t.Fatalf("expected user field, got %#v", env.Fields)
	}
}

func TestPaasContextFlagPropagatesAndResets(t *testing.T) {
	withContext := captureStdout(t, func() {
		cmdPaas([]string{"--context", "internal-dogfood", "app", "list", "--json"})
	})
	withEnv := parsePaasEnvelope(t, withContext)
	if withEnv.Context != "internal-dogfood" {
		t.Fatalf("expected context=internal-dogfood, got %q", withEnv.Context)
	}

	defaultContext := captureStdout(t, func() {
		cmdPaas([]string{"app", "list", "--json"})
	})
	defaultEnv := parsePaasEnvelope(t, defaultContext)
	if defaultEnv.Context != defaultPaasContext {
		t.Fatalf("expected context reset to default, got %q", defaultEnv.Context)
	}
}

func TestPaasTargetCRUDWithLocalStore(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv(paasStateRootEnvKey, stateRoot)

	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-1", "--host", "10.0.0.4", "--user", "root"})
	})
	captureStdout(t, func() {
		cmdPaas([]string{"target", "add", "--name", "edge-2", "--host", "10.0.0.5", "--user", "admin"})
	})

	listRaw := captureStdout(t, func() {
		cmdPaas([]string{"target", "list", "--json"})
	})
	listPayload := parseTargetListPayload(t, listRaw)
	if listPayload.Command != "target list" {
		t.Fatalf("expected command=target list, got %q", listPayload.Command)
	}
	if listPayload.Mode != "live" {
		t.Fatalf("expected mode=live, got %q", listPayload.Mode)
	}
	if listPayload.Count != 2 {
		t.Fatalf("expected count=2, got %d", listPayload.Count)
	}
	if listPayload.CurrentTarget != "edge-1" {
		t.Fatalf("expected current target edge-1, got %q", listPayload.CurrentTarget)
	}

	captureStdout(t, func() {
		cmdPaas([]string{"target", "use", "--target", "edge-2"})
	})
	afterUseRaw := captureStdout(t, func() {
		cmdPaas([]string{"target", "list", "--json"})
	})
	afterUse := parseTargetListPayload(t, afterUseRaw)
	if afterUse.CurrentTarget != "edge-2" {
		t.Fatalf("expected current target edge-2, got %q", afterUse.CurrentTarget)
	}

	captureStdout(t, func() {
		cmdPaas([]string{"target", "remove", "--target", "edge-1"})
	})
	afterRemoveRaw := captureStdout(t, func() {
		cmdPaas([]string{"target", "list", "--json"})
	})
	afterRemove := parseTargetListPayload(t, afterRemoveRaw)
	if afterRemove.Count != 1 {
		t.Fatalf("expected count=1 after remove, got %d", afterRemove.Count)
	}
	if len(afterRemove.Data) != 1 || afterRemove.Data[0].Name != "edge-2" {
		t.Fatalf("expected edge-2 remaining, got %#v", afterRemove.Data)
	}
}

func TestPaasDeployInvalidStrategyShowsUsage(t *testing.T) {
	out := captureStdout(t, func() {
		cmdPaas([]string{"deploy", "--strategy", "invalid"})
	})
	if !strings.Contains(out, paasDeployUsageText) {
		t.Fatalf("expected deploy usage for invalid strategy, got %q", out)
	}
}

func TestPaasCommandActionSetsArePopulated(t *testing.T) {
	tests := []struct {
		name    string
		actions []subcommandAction
	}{
		{name: "paas", actions: paasActions},
		{name: "paas target", actions: paasTargetActions},
		{name: "paas app", actions: paasAppActions},
		{name: "paas alert", actions: paasAlertActions},
		{name: "paas ai", actions: paasAIActions},
		{name: "paas context", actions: paasContextActions},
		{name: "paas agent", actions: paasAgentActions},
		{name: "paas events", actions: paasEventsActions},
	}
	for _, tc := range tests {
		if len(tc.actions) == 0 {
			t.Fatalf("%s actions should not be empty", tc.name)
		}
		for _, action := range tc.actions {
			if action.Name == "" {
				t.Fatalf("%s action name should not be empty", tc.name)
			}
			if action.Description == "" {
				t.Fatalf("%s action description should not be empty", tc.name)
			}
		}
	}
}

func TestPaasActionNamesMatchDispatchSwitches(t *testing.T) {
	expectActionNames(t, "paas", paasActions, []string{"target", "app", "deploy", "rollback", "logs", "alert", "ai", "context", "agent", "events"})
	expectActionNames(t, "paas target", paasTargetActions, []string{"add", "list", "check", "use", "remove", "bootstrap"})
	expectActionNames(t, "paas app", paasAppActions, []string{"init", "list", "status", "remove"})
	expectActionNames(t, "paas alert", paasAlertActions, []string{"setup-telegram", "test", "history"})
	expectActionNames(t, "paas ai", paasAIActions, []string{"plan", "inspect", "fix"})
	expectActionNames(t, "paas context", paasContextActions, []string{"create", "list", "use", "show", "remove"})
	expectActionNames(t, "paas agent", paasAgentActions, []string{"enable", "disable", "status", "logs", "run-once", "approve", "deny"})
	expectActionNames(t, "paas events", paasEventsActions, []string{"list"})
}

func TestNormalizeImagePlatformArch(t *testing.T) {
	tests := map[string]string{
		"":              "",
		"linux/amd64":   "amd64",
		"amd64":         "amd64",
		"linux/aarch64": "arm64",
		"arm64":         "arm64",
	}
	for input, expected := range tests {
		got := normalizeImagePlatformArch(input)
		if got != expected {
			t.Fatalf("normalizeImagePlatformArch(%q) = %q, expected %q", input, got, expected)
		}
	}
}

func parsePaasEnvelope(t *testing.T, raw string) paasTestEnvelope {
	t.Helper()
	var env paasTestEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("decode envelope: %v output=%q", err, raw)
	}
	return env
}

func parseTargetListPayload(t *testing.T, raw string) paasTargetListPayload {
	t.Helper()
	var payload paasTargetListPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode target list payload: %v output=%q", err, raw)
	}
	return payload
}
