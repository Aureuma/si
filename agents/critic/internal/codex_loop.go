package internal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	codexAnsiRe    = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	codexTokenRe   = regexp.MustCompile(`^[A-Z0-9_]{3,64}$`)
	codexSessionRe = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
)

// RunCodexTurn runs a single Codex turn inside the target container using the interactive CLI.
// It returns the session id (when available) and the last captured response line.
func (m *Monitor) RunCodexTurn(ctx context.Context, container, sessionID, prompt, model, effort string) (string, string, error) {
	prompt = codexDyadPreamble(container, prompt)
	encoded := base64.StdEncoding.EncodeToString([]byte(prompt))

	workdir := strings.TrimSpace(os.Getenv("CODEX_WORKDIR"))
	if workdir == "" {
		workdir = "/workspace/apps"
	}

	if model == "" {
		model = envOr("CODEX_MODEL", "gpt-5.2-codex")
	}
	if effort == "" {
		effort = envOr("CODEX_REASONING_EFFORT", "medium")
	}
	effort = normalizeReasoningEffort(effort)
	codexArgs := fmt.Sprintf(
		"-m %s -c %s --dangerously-bypass-approvals-and-sandbox -C %s",
		shellQuote(model),
		shellQuote(fmt.Sprintf("model_reasoning_effort=%s", effort)),
		shellQuote(workdir),
	)

	var codexCmd string
	if strings.TrimSpace(sessionID) == "" {
		codexCmd = fmt.Sprintf("codex %s", codexArgs)
	} else {
		codexCmd = fmt.Sprintf("codex resume %s %s", codexArgs, shellQuote(sessionID))
	}

	turnTimeout := 8 * time.Minute
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline) - 30*time.Second
		if remaining > time.Minute {
			turnTimeout = remaining
		} else if remaining > 0 {
			turnTimeout = remaining
		}
	}
	idleTimeout := 4 * time.Second
	logWait := 5 * time.Second

	cmd := fmt.Sprintf(
		"PROMPT=$(printf '%%s' %s | base64 -d); SESSION_LOG=$(mktemp /tmp/codex-session-XXXX.jsonl); "+
			"codex-stdout-parser --command %s --prompt \"$PROMPT\" --session-log \"$SESSION_LOG\" "+
			"--session-log-wait %s --bracketed-paste --submit-seq '\\r' --idle-timeout %s --turn-timeout %s "+
			"--wait-ready=false --ready-regex '' --send-exit=false --flush-on-eof=false --max-turns 1 --exit-grace 2s",
		shellQuote(encoded),
		shellQuote(codexCmd),
		shellQuote(logWait.String()),
		shellQuote(idleTimeout.String()),
		shellQuote(turnTimeout.String()),
	)

	raw, err := m.ExecInContainerCapture(ctx, container, []string{"bash", "-lc", cmd})
	if err != nil {
		return sessionID, "", err
	}

	lastMsg := parseCodexParserOutput(raw)
	if lastMsg == "" {
		lastMsg = parseCodexOutput(raw)
	}
	if lastMsg == "" {
		return sessionID, "", fmt.Errorf("no codex output captured")
	}

	newSessionID := strings.TrimSpace(sessionID)
	if newSessionID == "" {
		newSessionID = parseCodexSessionID(raw)
	}
	if newSessionID == "" {
		if sid, err := m.latestCodexSessionID(ctx, container); err == nil {
			newSessionID = sid
		}
	}
	if newSessionID == "" {
		newSessionID = sessionID
	}
	return newSessionID, lastMsg, nil
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

func codexConfigForTask(task DyadTask) (string, string) {
	baseModel := envOr("CODEX_MODEL", "gpt-5.2-codex")
	baseEffort := envOr("CODEX_REASONING_EFFORT", "medium")

	level := normalizeComplexity(task.Complexity)
	if level == "" {
		level = normalizeComplexity(task.Priority)
	}

	model := baseModel
	effort := baseEffort
	if level != "" {
		modelEnv := strings.ToUpper("CODEX_MODEL_" + level)
		if v := strings.TrimSpace(os.Getenv(modelEnv)); v != "" {
			model = v
		}
		effortEnv := strings.ToUpper("CODEX_REASONING_EFFORT_" + level)
		if v := strings.TrimSpace(os.Getenv(effortEnv)); v != "" {
			effort = v
		} else {
			effort = level
		}
	}
	return model, normalizeReasoningEffort(effort)
}

func normalizeComplexity(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	switch v {
	case "":
		return ""
	case "low", "simple", "easy", "trivial", "minor", "small", "p3":
		return "low"
	case "medium", "normal", "standard", "moderate", "p2":
		return "medium"
	case "high", "hard", "complex", "critical", "urgent", "major", "blocker", "p0", "p1":
		return "high"
	}
	if n, err := strconv.Atoi(v); err == nil {
		switch {
		case n <= 2:
			return "low"
		case n == 3:
			return "medium"
		default:
			return "high"
		}
	}
	if strings.HasPrefix(v, "p0") || strings.HasPrefix(v, "p1") {
		return "high"
	}
	if strings.HasPrefix(v, "p2") {
		return "medium"
	}
	if strings.HasPrefix(v, "p3") || strings.HasPrefix(v, "p4") {
		return "low"
	}
	return ""
}

func normalizeReasoningEffort(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	switch v {
	case "low":
		return "medium"
	case "medium", "high", "xhigh":
		return v
	default:
		return strings.TrimSpace(value)
	}
}

func envOr(key, def string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	return val
}

func parseStateTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts
	}
	return time.Time{}
}

func stripLogTimestamps(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, fields[0]); err == nil {
			lines[i] = strings.Join(fields[1:], " ")
			continue
		}
		if _, err := time.Parse(time.RFC3339, fields[0]); err == nil {
			lines[i] = strings.Join(fields[1:], " ")
		}
	}
	return strings.Join(lines, "\n")
}

func (m *Monitor) actorLogContext(ctx context.Context, actor string, state map[string]string) (string, map[string]string) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return "", nil
	}
	lines := envIntAllowZero("CODEX_ACTOR_LOG_LINES", 32)
	if lines <= 0 {
		return "", nil
	}
	maxBytes := envIntAllowZero("CODEX_ACTOR_LOG_BYTES", 2400)
	if maxBytes <= 0 {
		maxBytes = 2400
	}
	since := parseStateTime(state["actor.logs.since"])
	text, last, err := m.fetchActorLogs(ctx, actor, since, lines, true)
	if err != nil {
		return "", nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	text = stripLogTimestamps(text)
	text = truncateLines(text, lines, maxBytes)
	if text == "" {
		return "", nil
	}
	update := map[string]string{}
	if !last.IsZero() {
		update["actor.logs.since"] = last.UTC().Format(time.RFC3339Nano)
	}
	return "Recent actor CLI output:\n" + text, update
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
	sessionID := strings.TrimSpace(state["codex.session_id"])
	if sessionID == "" {
		sessionID = strings.TrimSpace(state["codex.thread_id"])
	}
	attempts := state["codex.exec.attempts"]
	if attempts == "" {
		attempts = "0"
	}
	attemptCount := atoiDefault(attempts, 0)
	if v := strings.TrimSpace(state["task.complexity"]); v != "" {
		task.Complexity = v
	} else if v := strings.TrimSpace(state["codex.complexity"]); v != "" {
		task.Complexity = v
	}
	model, effort := codexConfigForTask(task)

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

	logContext, logState := m.actorLogContext(ctx, actor, state)
	contextParts := []string{}
	if attemptCount > 0 {
		if prev := strings.TrimSpace(state["codex.exec.last"]); prev != "" {
			contextParts = append(contextParts, "Previous Codex output:\n"+prev)
		}
	}
	if logContext != "" && attemptCount > 0 {
		contextParts = append(contextParts, logContext)
	}
	if len(contextParts) > 0 {
		prompt = strings.TrimSpace(prompt + "\n\nAdditional context:\n" + strings.Join(contextParts, "\n\n"))
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
	newSessionID, lastMsg, err := m.RunCodexTurn(turnCtx, actor, sessionID, fullPrompt, model, effort)
	if err != nil {
		updates := map[string]string{"codex.exec.error": err.Error()}
		if newSessionID != "" {
			updates["codex.session_id"] = newSessionID
			updates["codex.thread_id"] = newSessionID
		}
		for k, v := range logState {
			updates[k] = v
		}
		_ = m.updateDyadTask(ctx, map[string]interface{}{
			"id":     task.ID,
			"status": "blocked",
			"notes":  setState(task.Notes, updates),
		})
		return
	}

	result := strings.TrimSpace(lastMsg)
	nextStatus := "review"
	if result == "DONE" || strings.HasSuffix(result, "\nDONE") || strings.Contains(result, "\nDONE\n") {
		nextStatus = "done"
	}

	updates := map[string]string{
		"codex.exec.last":     truncateOneLine(result, 400),
		"codex.exec.attempts": fmt.Sprintf("%d", attemptCount+1),
	}
	if newSessionID != "" {
		updates["codex.session_id"] = newSessionID
		updates["codex.thread_id"] = newSessionID
	}
	for k, v := range logState {
		updates[k] = v
	}
	notes := setState(task.Notes, updates)

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
// - read actor stdout (via pod logs) and captured output,
// - decide next prompt based on previous output,
// - send next prompt via an interactive Codex session,
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
	sessionID := strings.TrimSpace(state["codex.session_id"])
	if sessionID == "" {
		sessionID = strings.TrimSpace(state["codex.thread_id"])
	}
	phase := state["codex_test.phase"]
	if phase == "" {
		phase = "1"
	}
	if v := strings.TrimSpace(state["task.complexity"]); v != "" {
		task.Complexity = v
	} else if v := strings.TrimSpace(state["codex.complexity"]); v != "" {
		task.Complexity = v
	}
	model, effort := codexConfigForTask(task)

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
	newSessionID, lastMsg, err := m.RunCodexTurn(turnCtx, actor, sessionID, nextPrompt, model, effort)
	if err != nil {
		updates := map[string]string{"codex_test.error": err.Error()}
		if newSessionID != "" {
			updates["codex.session_id"] = newSessionID
			updates["codex.thread_id"] = newSessionID
		}
		_ = m.updateDyadTask(ctx, map[string]interface{}{
			"id":     task.ID,
			"status": "blocked",
			"notes":  setState(task.Notes, updates),
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

	updates := map[string]string{
		"codex_test.phase":  nextPhase,
		"codex_test.last":   strings.TrimSpace(lastMsg),
		"codex_test.result": outcome,
	}
	if newSessionID != "" {
		updates["codex.session_id"] = newSessionID
		updates["codex.thread_id"] = newSessionID
	}
	notes := setState(task.Notes, updates)

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

type codexParserOutput struct {
	Status      string `json:"status"`
	FinalReport string `json:"final_report"`
}

func parseCodexParserOutput(raw string) string {
	last := ""
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var out codexParserOutput
		if err := json.Unmarshal([]byte(line), &out); err != nil {
			continue
		}
		if strings.TrimSpace(out.FinalReport) == "" {
			continue
		}
		if strings.HasPrefix(out.Status, "turn_complete") || out.Status == "" {
			last = strings.TrimSpace(out.FinalReport)
		}
	}
	return last
}

func parseCodexOutput(raw string) string {
	clean := stripANSI(raw)
	lines := strings.Split(clean, "\n")
	lastNonEmpty := ""
	lastToken := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lastNonEmpty = line
		if line == "DONE" {
			lastToken = line
			continue
		}
		if codexTokenRe.MatchString(line) {
			lastToken = line
		}
	}
	if lastToken != "" {
		return lastToken
	}
	return lastNonEmpty
}

func stripANSI(s string) string {
	return codexAnsiRe.ReplaceAllString(s, "")
}

func parseCodexSessionID(raw string) string {
	clean := stripANSI(raw)
	lines := strings.Split(clean, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "session") && !strings.Contains(lower, "conversation") {
			continue
		}
		if id := codexSessionRe.FindString(line); id != "" {
			return id
		}
	}
	return ""
}

func (m *Monitor) latestCodexSessionID(ctx context.Context, container string) (string, error) {
	cmd := `dir="${CODEX_HOME:-$HOME/.codex}/sessions"; ls -1t "$dir" 2>/dev/null | head -n 1`
	out, err := m.ExecInContainerCapture(ctx, container, []string{"bash", "-lc", cmd})
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(out)
	id = strings.TrimSuffix(id, ".json")
	return strings.TrimSpace(id), nil
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
