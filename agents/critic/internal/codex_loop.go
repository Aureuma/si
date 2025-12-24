package internal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// RunCodexTurn runs a single Codex turn inside the target container using stdin piping.
// It returns the thread/session id and the last agent_message text from the JSONL stream.
func (m *Monitor) RunCodexTurn(ctx context.Context, container, threadID, prompt string) (string, string, error) {
	prompt = codexDyadPreamble(container, prompt)
	encoded := base64.StdEncoding.EncodeToString([]byte(prompt))

	workdir := strings.TrimSpace(os.Getenv("CODEX_WORKDIR"))
	if workdir == "" {
		workdir = "/workspace/apps"
	}

	model := strings.TrimSpace(os.Getenv("CODEX_MODEL"))
	if model == "" {
		model = "gpt-5.1-codex-max"
	}
	effort := strings.TrimSpace(os.Getenv("CODEX_REASONING_EFFORT"))
	if effort == "" {
		effort = "high"
	}
	codexPrefix := fmt.Sprintf(
		"codex -m %s -c %s --dangerously-bypass-approvals-and-sandbox",
		shellQuote(model),
		shellQuote(fmt.Sprintf("model_reasoning_effort=%s", effort)),
	)

	var cmd string
	if strings.TrimSpace(threadID) == "" {
		cmd = fmt.Sprintf(
			"cd %s && printf '%%s' %q | base64 -d | %s exec --skip-git-repo-check --json - | tee /proc/1/fd/1",
			shellQuote(workdir),
			encoded,
			codexPrefix,
		)
	} else {
		cmd = fmt.Sprintf(
			"cd %s && printf '%%s' %q | base64 -d | %s exec --skip-git-repo-check --json resume %s - | tee /proc/1/fd/1",
			shellQuote(workdir),
			encoded,
			codexPrefix,
			shellQuote(threadID),
		)
	}

	raw, err := m.ExecInContainerCapture(ctx, container, []string{"bash", "-lc", cmd})
	if err != nil {
		return threadID, "", err
	}

	parsedThread, lastMsg := parseCodexJSONL(raw)
	if parsedThread == "" {
		parsedThread = threadID
	}
	if lastMsg == "" {
		return parsedThread, "", fmt.Errorf("no agent_message found in codex output")
	}
	return parsedThread, lastMsg, nil
}

func codexDyadPreamble(container, prompt string) string {
	dyad := strings.TrimSpace(os.Getenv("DYAD_NAME"))
	dept := strings.TrimSpace(os.Getenv("DEPARTMENT"))
	role := strings.TrimSpace(os.Getenv("ROLE"))
	if role == "" {
		role = "critic"
	}

	// Keep this short; it is included on every turn.
	preamble := strings.TrimSpace(fmt.Sprintf(
		`SILEXA DYAD CONTEXT
- dyad: %s
- department: %s
- executor: critic (driving an actor)
- target actor container: %s

Instructions:
- Do the work requested below inside the repo.
- Keep output concise and operational.`,
		emptyIf(dyad, "unknown"),
		emptyIf(dept, "unknown"),
		container,
	))

	p := strings.TrimSpace(prompt)
	if p == "" {
		return preamble
	}
	return preamble + "\n\nTask:\n" + p
}

func emptyIf(v, def string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	return v
}

func (m *Monitor) DriveCodexExecTask(ctx context.Context, dyad string, task DyadTask) {
	actor := task.Actor
	if actor == "" {
		actor = m.ActorContainer
	}
	if actor == "" {
		return
	}

	_ = m.claimDyadTask(ctx, task.ID, dyad)

	state := parseState(task.Notes)
	threadID := state["codex.thread_id"]
	attempts := state["codex.exec.attempts"]
	if attempts == "" {
		attempts = "0"
	}

	prompt := strings.TrimSpace(task.Description)
	if prompt == "" {
		prompt = strings.TrimSpace(task.Title)
	}
	if prompt == "" {
		_ = m.updateDyadTask(ctx, map[string]interface{}{
			"id":     task.ID,
			"status": "blocked",
			"notes":  setState(task.Notes, map[string]string{"codex.exec.error": "empty task description/title"}),
		})
		return
	}

	// Ensure Codex returns a stable completion token so the critic can decide when to stop.
	fullPrompt := strings.TrimSpace(fmt.Sprintf(
		`You are the infra actor working inside a repo. Complete the requested work and then output a final line exactly: DONE

Task:
%s`,
		prompt,
	))

	turnCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	newThreadID, lastMsg, err := m.RunCodexTurn(turnCtx, actor, threadID, fullPrompt)
	if err != nil {
		_ = m.updateDyadTask(ctx, map[string]interface{}{
			"id":     task.ID,
			"status": "blocked",
			"notes":  setState(task.Notes, map[string]string{"codex.thread_id": newThreadID, "codex.exec.error": err.Error()}),
		})
		return
	}

	result := strings.TrimSpace(lastMsg)
	nextStatus := "review"
	if result == "DONE" || strings.HasSuffix(result, "\nDONE") || strings.Contains(result, "\nDONE\n") {
		nextStatus = "done"
	}

	notes := setState(task.Notes, map[string]string{
		"codex.thread_id":     newThreadID,
		"codex.exec.last":     truncateOneLine(result, 400),
		"codex.exec.attempts": fmt.Sprintf("%d", atoiDefault(attempts, 0)+1),
	})

	_ = m.updateDyadTask(ctx, map[string]interface{}{
		"id":     task.ID,
		"status": nextStatus,
		"notes":  notes,
	})
}

func atoiDefault(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	n := def
	_, _ = fmt.Sscanf(s, "%d", &n)
	return n
}

func truncateOneLine(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) > max {
		return s[:max]
	}
	return s
}

// DriveCodexLoopTest proves the critic can:
// - read actor stdout (via docker logs) and captured output,
// - decide next prompt based on previous output,
// - send next prompt via stdin piping,
// - repeat multiple turns and finalize.
func (m *Monitor) DriveCodexLoopTest(ctx context.Context, dyad string, task DyadTask) {
	actor := task.Actor
	if actor == "" {
		actor = m.ActorContainer
	}
	if actor == "" {
		return
	}

	state := parseState(task.Notes)
	threadID := state["codex.thread_id"]
	phase := state["codex_test.phase"]
	if phase == "" {
		phase = "1"
	}

	// Ensure we claim before doing work (prevents multi-critic contention).
	_ = m.claimDyadTask(ctx, task.ID, dyad)

	nextPrompt, expected := codexTestPrompt(phase, state["codex_test.last"])
	if nextPrompt == "" {
		_ = m.updateDyadTask(ctx, map[string]interface{}{
			"id":     task.ID,
			"status": "done",
			"notes":  setState(task.Notes, map[string]string{"codex_test.phase": "done"}),
		})
		return
	}

	turnCtx, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()
	newThreadID, lastMsg, err := m.RunCodexTurn(turnCtx, actor, threadID, nextPrompt)
	if err != nil {
		_ = m.updateDyadTask(ctx, map[string]interface{}{
			"id":     task.ID,
			"status": "blocked",
			"notes":  setState(task.Notes, map[string]string{"codex.thread_id": newThreadID, "codex_test.error": err.Error()}),
		})
		return
	}

	outcome := "ok"
	if expected != "" && strings.TrimSpace(lastMsg) != expected {
		outcome = fmt.Sprintf("unexpected_output (want=%q got=%q)", expected, strings.TrimSpace(lastMsg))
	}

	nextPhase := "2"
	switch phase {
	case "1":
		nextPhase = "2"
	case "2":
		nextPhase = "3"
	case "3":
		nextPhase = "done"
	}

	notes := setState(task.Notes, map[string]string{
		"codex.thread_id":   newThreadID,
		"codex_test.phase":  nextPhase,
		"codex_test.last":   strings.TrimSpace(lastMsg),
		"codex_test.result": outcome,
	})

	status := "in_progress"
	if nextPhase == "done" && outcome == "ok" {
		status = "done"
	}
	if strings.HasPrefix(outcome, "unexpected_output") {
		status = "blocked"
	}
	_ = m.updateDyadTask(ctx, map[string]interface{}{"id": task.ID, "status": status, "notes": notes})
}

func codexTestPrompt(phase, last string) (prompt string, expected string) {
	switch phase {
	case "1":
		return "You are a test harness. Return exactly this text with no extra whitespace:\nTURN1_OK", "TURN1_OK"
	case "2":
		return fmt.Sprintf("Previous output was %q. Return exactly this text with no extra whitespace:\nTURN2_OK", strings.TrimSpace(last)), "TURN2_OK"
	case "3":
		return "Final step. Return exactly this text with no extra whitespace:\nTURN3_OK", "TURN3_OK"
	default:
		return "", ""
	}
}

func parseCodexJSONL(raw string) (threadID string, lastText string) {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var evt map[string]interface{}
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		if t, _ := evt["type"].(string); t == "thread.started" {
			if id, _ := evt["thread_id"].(string); id != "" {
				threadID = id
			}
		}
		if t, _ := evt["type"].(string); t == "item.completed" {
			item, _ := evt["item"].(map[string]interface{})
			if item == nil {
				continue
			}
			// Different Codex CLI builds emit different "item" types. We treat any
			// relevant text payload as a usable "last message" for task progression.
			if txt, _ := item["text"].(string); strings.TrimSpace(txt) != "" {
				it, _ := item["type"].(string)
				switch strings.ToLower(strings.TrimSpace(it)) {
				case "agent_message", "assistant_message", "final", "final_answer", "message", "reasoning":
					lastText = txt
				}
			}
			if out, _ := item["aggregated_output"].(string); strings.TrimSpace(out) != "" {
				// Fallback when the run primarily executed commands and didn't emit an agent_message.
				lastText = out
			}
		}
	}
	// Final fallback: if we saw JSONL but no structured text, return a raw tail.
	if strings.TrimSpace(lastText) == "" {
		lines := strings.Split(strings.TrimSpace(raw), "\n")
		if len(lines) > 0 {
			start := 0
			if len(lines) > 25 {
				start = len(lines) - 25
			}
			lastText = strings.Join(lines[start:], "\n")
		}
	}
	return threadID, lastText
}

func parseState(notes string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(notes, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[") || !strings.Contains(line, "]=") {
			continue
		}
		end := strings.Index(line, "]=")
		if end <= 1 {
			continue
		}
		key := strings.TrimSpace(line[1:end])
		val := strings.TrimSpace(line[end+2:])
		if key != "" {
			out[key] = val
		}
	}
	return out
}

func setState(notes string, kv map[string]string) string {
	lines := []string{}
	seen := map[string]bool{}
	// update existing state lines
	for _, line := range strings.Split(notes, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[") && strings.Contains(trim, "]=") {
			end := strings.Index(trim, "]=")
			key := strings.TrimSpace(trim[1:end])
			if v, ok := kv[key]; ok {
				lines = append(lines, fmt.Sprintf("[%s]=%s", key, v))
				seen[key] = true
				continue
			}
		}
		if trim != "" {
			lines = append(lines, line)
		}
	}
	// append missing keys
	for k, v := range kv {
		if seen[k] {
			continue
		}
		lines = append(lines, fmt.Sprintf("[%s]=%s", k, v))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
