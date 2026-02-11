package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func writeExecutable(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestParseAction(t *testing.T) {
	step, err := parseAction("wait_contains:status: ok|3s", 5*time.Second)
	if err != nil {
		t.Fatalf("parseAction: %v", err)
	}
	if step.Kind != actionWaitContains {
		t.Fatalf("kind = %q", step.Kind)
	}
	if step.Arg != "status: ok" {
		t.Fatalf("arg = %q", step.Arg)
	}
	if step.Timeout != 3*time.Second {
		t.Fatalf("timeout = %s", step.Timeout)
	}
}

func TestParseActionWaitPromptTimeout(t *testing.T) {
	step, err := parseAction("wait_prompt:750ms", 5*time.Second)
	if err != nil {
		t.Fatalf("parseAction: %v", err)
	}
	if step.Kind != actionWaitPrompt {
		t.Fatalf("kind = %q", step.Kind)
	}
	if step.Timeout != 750*time.Millisecond {
		t.Fatalf("timeout = %s", step.Timeout)
	}
}

func TestRunPlan_AllSlashCommandsAndNavigation(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	fake := writeExecutable(t, "fake-codex.sh", fakeCodexDriverScript)
	promptRe := regexp.MustCompile(`^›\s*$`)
	r, err := newRunner("exec "+shellQuote(fake), promptRe, 1<<20, 3*time.Second)
	if err != nil {
		t.Fatalf("newRunner: %v", err)
	}
	defer r.close()

	actions, err := normalizeActions([]string{
		"wait_prompt",
		"send:/status",
		"wait_contains:status: ok",
		"wait_prompt",
		"send:/model gpt-5.2-codex",
		"wait_contains:model: gpt-5.2-codex",
		"wait_prompt",
		"send:/approval auto",
		"wait_contains:approval: auto",
		"wait_prompt",
		"send:/sandbox workspace-write",
		"wait_contains:sandbox: workspace-write",
		"wait_prompt",
		"send:/agents",
		"wait_contains:agents: actor,critic",
		"wait_prompt",
		"send:/prompts",
		"wait_contains:prompts: available",
		"wait_prompt",
		"send:/review",
		"wait_contains:review: started",
		"wait_prompt",
		"send:/compact",
		"wait_contains:compact: done",
		"wait_prompt",
		"send:/clear",
		"wait_contains:clear: done",
		"wait_prompt",
		"send:/help",
		"wait_contains:help: commands listed",
		"wait_prompt",
		"send:/logout",
		"wait_contains:logout: skipped",
		"wait_prompt",
		"send:/vim",
		"wait_contains:vim: enabled",
		"wait_prompt",
		"send:/",
		"wait_contains:menu: /status /model /approval",
		"wait_prompt",
		"send:/exit",
		"wait_contains:bye",
	}, 3*time.Second)
	if err != nil {
		t.Fatalf("normalizeActions: %v", err)
	}

	if err := runPlan(r, actions); err != nil {
		t.Fatalf("runPlan: %v", err)
	}
	if err := r.waitExit(2 * time.Second); err != nil {
		t.Fatalf("waitExit: %v\noutput:\n%s", err, r.outputString())
	}

	out := stripANSI(r.outputString())
	if !strings.Contains(out, "bye") {
		t.Fatalf("expected exit marker in output, got:\n%s", out)
	}
}

func TestRunPlan_MenuNavigationSelection(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	fake := writeExecutable(t, "fake-codex-menu.sh", fakeCodexDriverScript)
	promptRe := regexp.MustCompile(`^›\s*$`)
	r, err := newRunner("exec "+shellQuote(fake), promptRe, 1<<20, 3*time.Second)
	if err != nil {
		t.Fatalf("newRunner: %v", err)
	}
	defer r.close()

	actions, err := normalizeActions([]string{
		"wait_prompt",
		"send:/",
		"wait_contains:menu: /status /model /approval",
		"wait_prompt",
		"send:2",
		"wait_contains:menu-select: /model",
		"wait_prompt",
		"send:1",
		"wait_contains:menu-select: /status",
		"wait_prompt",
		"send:/exit",
		"wait_contains:bye",
	}, 3*time.Second)
	if err != nil {
		t.Fatalf("normalizeActions: %v", err)
	}
	if err := runPlan(r, actions); err != nil {
		t.Fatalf("runPlan: %v\noutput:\n%s", err, r.outputString())
	}
	if err := r.waitExit(2 * time.Second); err != nil {
		t.Fatalf("waitExit: %v\noutput:\n%s", err, r.outputString())
	}
}

func TestRunPlanTimesOutWithoutPrompt(t *testing.T) {
	fake := writeExecutable(t, "fake-no-prompt.sh", "#!/usr/bin/env bash\nset -euo pipefail\necho no-prompt\nsleep 2\n")
	promptRe := regexp.MustCompile(`^›\s*$`)
	r, err := newRunner("exec "+shellQuote(fake), promptRe, 1<<20, 400*time.Millisecond)
	if err != nil {
		t.Fatalf("newRunner: %v", err)
	}
	defer r.close()

	actions, err := normalizeActions([]string{"wait_prompt"}, 400*time.Millisecond)
	if err != nil {
		t.Fatalf("normalizeActions: %v", err)
	}
	err = runPlan(r, actions)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timeout waiting for prompt") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestDecodeKeyArrow(t *testing.T) {
	got, err := decodeKey("down")
	if err != nil {
		t.Fatalf("decodeKey: %v", err)
	}
	if got != "\x1b[B" {
		t.Fatalf("decodeKey(down) = %q", got)
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

const fakeCodexDriverScript = `#!/usr/bin/env bash
set -euo pipefail
prompt="› "

print_prompt() {
  printf "%s" "$prompt"
}

echo "fake-codex driver ready"
print_prompt
while IFS= read -r line; do
  case "$line" in
    /status)
      echo "status: ok"
      ;;
    /model*)
      echo "model: gpt-5.2-codex"
      ;;
    /approval*)
      echo "approval: auto"
      ;;
    /sandbox*)
      echo "sandbox: workspace-write"
      ;;
    /agents)
      echo "agents: actor,critic"
      ;;
    /prompts)
      echo "prompts: available"
      ;;
    /review)
      echo "review: started"
      ;;
    /compact)
      echo "compact: done"
      ;;
    /clear)
      echo "clear: done"
      ;;
    /help)
      echo "help: commands listed"
      ;;
    /logout)
      echo "logout: skipped"
      ;;
    /vim)
      echo "vim: enabled"
      ;;
    /)
      echo "menu: /status /model /approval"
      ;;
    1)
      echo "menu-select: /status"
      ;;
    2)
      echo "menu-select: /model"
      ;;
    $'\033[A'*|'^[[A'*)
      echo "menu-select: /status"
      ;;
    $'\033[B'*|'^[[B'*)
      echo "menu-select: /model"
      ;;
    /exit)
      echo "bye"
      exit 0
      ;;
    *)
      echo "echo:$line"
      ;;
  esac
  print_prompt
done
`
