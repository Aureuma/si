package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	shared "silexa/agents/shared/docker"
)

type reportOptions struct {
	CaptureMode     string
	KeepTmux        bool
	Debug           bool
	PromptLines     int
	AllowMcpStartup bool
	ReadyTimeout    time.Duration
	TurnTimeout     time.Duration
	PollInterval    time.Duration
	SubmitAttempts  int
	SubmitDelay     time.Duration
	LockTimeout     time.Duration
	LockStaleAfter  time.Duration
	TmuxPrefix      string
	Ansi            bool
}

type codexTurnReport struct {
	Prompt string `json:"prompt"`
	Report string `json:"report"`
	Raw    string `json:"raw,omitempty"`
}

type promptSegment struct {
	Prompt string
	Lines  []string
	Raw    []string
}

const tmuxReportPrefix = "si-codex-report-"

func cmdCodexReport(args []string) {
	fs := flag.NewFlagSet("codex report", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output JSON")
	rawOut := fs.Bool("raw", false, "include raw segment output")
	ansiOut := fs.Bool("ansi", false, "preserve ANSI colors in report output")
	turnTimeout := fs.Duration("turn-timeout", 60*time.Second, "timeout per prompt")
	readyTimeout := fs.Duration("ready-timeout", 30*time.Second, "timeout waiting for prompt")
	pollInterval := fs.Duration("poll-interval", 300*time.Millisecond, "poll interval for capture")
	submitAttempts := fs.Int("submit-attempts", 2, "prompt submit attempts")
	submitDelay := fs.Duration("submit-delay", 4*time.Second, "delay before re-submitting prompt")
	promptLines := fs.Int("prompt-lines", 3, "prompt lines to scan for readiness")
	allowMcp := fs.Bool("allow-mcp-startup", false, "allow prompt detection during MCP startup")
	tmuxCapture := fs.String("tmux-capture", "main", "tmux capture mode: alt|main")
	tmuxKeep := fs.Bool("tmux-keep", false, "keep tmux session after run")
	debug := fs.Bool("debug", false, "debug tmux report capture")
	lockTimeout := fs.Duration("lock-timeout", 2*time.Second, "lock wait time")
	lockStale := fs.Duration("lock-stale", 5*time.Minute, "lock staleness before removal")
	promptsFile := fs.String("prompts-file", "", "file with prompts (one per line)")
	var prompts multiFlag
	fs.Var(&prompts, "prompt", "prompt to send (repeatable)")
	name := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = args[0]
		args = args[1:]
	}
	_ = fs.Parse(args)

	if name == "" {
		if fs.NArg() < 1 {
			printUsage("usage: si codex report <name> --prompt '...'")
			return
		}
		name = fs.Arg(0)
	}
	if err := validateSlug(name); err != nil {
		fatal(err)
	}
	if err := loadPrompts(&prompts, *promptsFile); err != nil {
		fatal(err)
	}
	if len(prompts) == 0 {
		fatal(errors.New("no prompts provided"))
	}

	opts := reportOptions{
		CaptureMode:     strings.ToLower(strings.TrimSpace(*tmuxCapture)),
		KeepTmux:        *tmuxKeep,
		Debug:           *debug,
		PromptLines:     *promptLines,
		AllowMcpStartup: *allowMcp,
		ReadyTimeout:    *readyTimeout,
		TurnTimeout:     *turnTimeout,
		PollInterval:    *pollInterval,
		SubmitAttempts:  *submitAttempts,
		SubmitDelay:     *submitDelay,
		LockTimeout:     *lockTimeout,
		LockStaleAfter:  *lockStale,
		TmuxPrefix:      tmuxReportPrefix,
		Ansi:            *ansiOut,
	}
	if opts.PromptLines <= 0 {
		opts.PromptLines = 3
	}
	if opts.ReadyTimeout <= 0 {
		opts.ReadyTimeout = 30 * time.Second
	}
	if opts.TurnTimeout <= 0 {
		opts.TurnTimeout = 60 * time.Second
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 300 * time.Millisecond
	}
	if opts.SubmitAttempts <= 0 {
		opts.SubmitAttempts = 2
	}
	if opts.SubmitDelay <= 0 {
		opts.SubmitDelay = 4 * time.Second
	}
	if opts.LockTimeout <= 0 {
		opts.LockTimeout = 2 * time.Second
	}
	if opts.LockStaleAfter <= 0 {
		opts.LockStaleAfter = 5 * time.Minute
	}
	switch opts.CaptureMode {
	case "alt", "main":
	default:
		fatal(fmt.Errorf("invalid tmux capture mode: %s", opts.CaptureMode))
	}

	unlock, lockErr := acquireCodexLock("report", name, opts.LockTimeout, opts.LockStaleAfter)
	if lockErr != nil {
		fatal(lockErr)
	}
	defer unlock()

	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()

	ctx := context.Background()
	containerName := codexContainerName(name)
	id, _, err := client.ContainerByName(ctx, containerName)
	if err != nil {
		fatal(err)
	}
	if id == "" {
		fatal(fmt.Errorf("codex container %s not found", containerName))
	}

	if err := ensureTmuxAvailable(); err != nil {
		fatal(err)
	}
	cleanupStaleTmuxSessions(ctx, opts.TmuxPrefix, 30*time.Minute, statusOptions{Debug: opts.Debug})

	reportCtx, reportCancel := context.WithTimeout(ctx, opts.ReadyTimeout+opts.TurnTimeout*time.Duration(len(prompts))+10*time.Second)
	defer reportCancel()

	output, reports, err := fetchCodexReportsViaTmux(reportCtx, id, prompts, opts)
	if err != nil {
		fatal(err)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(reports); err != nil {
			fatal(err)
		}
		return
	}

	for i, rep := range reports {
		fmt.Printf("%s %d: %s\n", styleSection("Turn"), i+1, rep.Prompt)
		if rep.Report != "" {
			fmt.Println(rep.Report)
		}
		if *rawOut {
			fmt.Println(styleDim("-- raw --"))
			fmt.Println(rep.Raw)
		}
		if i < len(reports)-1 {
			fmt.Println()
		}
	}

	_ = output
}

func loadPrompts(dst *multiFlag, path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		*dst = append(*dst, line)
	}
	return scanner.Err()
}

func fetchCodexReportsViaTmux(ctx context.Context, containerID string, prompts []string, opts reportOptions) (string, []codexTurnReport, error) {
	session := fmt.Sprintf("%s%s-%d", opts.TmuxPrefix, containerID, time.Now().UnixNano())
	paneTarget := session + ":0.0"
	cmd := buildTmuxCodexCommand(containerID)
	if opts.KeepTmux {
		cmd = cmd + "; exec bash"
	}
	_, _ = tmuxOutput(ctx, "kill-session", "-t", session)
	if _, err := tmuxOutput(ctx, "new-session", "-d", "-s", session, "bash", "-lc", cmd); err != nil {
		return "", nil, err
	}
	if opts.Debug {
		debugf(statusOptions{Debug: true}, "tmux session: %s", session)
	}
	defer func() {
		if opts.KeepTmux {
			return
		}
		_, _ = tmuxOutput(context.Background(), "kill-session", "-t", session)
	}()

	_, _ = tmuxOutput(ctx, "resize-pane", "-t", paneTarget, "-x", "160", "-y", "60")

	_, err := waitForPromptReady(ctx, paneTarget, opts)
	if err != nil {
		return "", nil, err
	}

	captureOpts := statusOptions{CaptureMode: opts.CaptureMode}
	output, err := tmuxCapture(ctx, paneTarget, captureOpts)
	if err != nil {
		return "", nil, err
	}
	cleanOutput := stripANSI(output)
	segments := parsePromptSegmentsDual(cleanOutput, output)
	promptIndex := len(segments) - 1
	if promptIndex < 0 {
		promptIndex = 0
	}

	reports := make([]codexTurnReport, 0, len(prompts))
	for _, prompt := range prompts {
		_ = tmuxSendKeys(ctx, paneTarget, "C-u")
		_ = tmuxSendLiteral(ctx, paneTarget, prompt)
		_ = tmuxSendKeys(ctx, paneTarget, "Enter")

		tmpOutput, report, err := waitForTurnReport(ctx, paneTarget, opts, promptIndex)
		if err != nil {
			return tmpOutput, reports, err
		}
		segmentRaw := ""
		if report != "" {
			cleanOutput = stripANSI(tmpOutput)
			segments = parsePromptSegmentsDual(cleanOutput, tmpOutput)
			if promptIndex < len(segments) {
				segmentRaw = strings.Join(segments[promptIndex].Raw, "\n")
			}
		}
		reports = append(reports, codexTurnReport{Prompt: prompt, Report: report, Raw: strings.TrimSpace(segmentRaw)})
		output = tmpOutput
		cleanOutput = stripANSI(output)
		segments = parsePromptSegmentsDual(cleanOutput, output)
		promptIndex = len(segments) - 1
		if promptIndex < 0 {
			promptIndex = 0
		}
	}

	_ = tmuxSendLiteral(ctx, paneTarget, "/exit")
	_ = tmuxSendKeys(ctx, paneTarget, "Enter")

	return output, reports, nil
}

func waitForPromptReady(ctx context.Context, target string, opts reportOptions) (string, error) {
	deadline := time.Now().Add(opts.ReadyTimeout)
	captureOpts := statusOptions{CaptureMode: opts.CaptureMode}
	var lastOutput string
	for time.Now().Before(deadline) {
		output, err := tmuxCapture(ctx, target, captureOpts)
		if err == nil && strings.TrimSpace(output) != "" {
			lastOutput = output
		}
		if isPromptReady(stripANSI(output), statusOptions{PromptLines: opts.PromptLines, AllowMcpStartup: opts.AllowMcpStartup}) {
			return output, nil
		}
		time.Sleep(opts.PollInterval)
	}
	if lastOutput == "" {
		return "", errors.New("timeout waiting for codex prompt")
	}
	return lastOutput, errors.New("timeout waiting for codex prompt")
}

func waitForTurnReport(ctx context.Context, target string, opts reportOptions, promptIndex int) (string, string, error) {
	deadline := time.Now().Add(opts.TurnTimeout)
	captureOpts := statusOptions{CaptureMode: opts.CaptureMode}
	var lastOutput string
	attempts := 1
	lastSubmit := time.Now()
	for time.Now().Before(deadline) {
		output, err := tmuxCapture(ctx, target, captureOpts)
		if err == nil && strings.TrimSpace(output) != "" {
			lastOutput = output
		}
		clean := stripANSI(output)
		segments := parsePromptSegments(clean)
		if len(segments) <= promptIndex {
			time.Sleep(opts.PollInterval)
			continue
		}
		report := extractReportLinesFromLines(segments[promptIndex].Raw, segments[promptIndex].Lines, opts.Ansi)
		if len(segments) > promptIndex+1 && report != "" {
			return output, report, nil
		}
		if report == "" && isPromptReady(clean, statusOptions{PromptLines: opts.PromptLines, AllowMcpStartup: opts.AllowMcpStartup}) {
			if attempts < opts.SubmitAttempts && time.Since(lastSubmit) > opts.SubmitDelay {
				_ = tmuxSendKeys(ctx, target, "Enter")
				attempts++
				lastSubmit = time.Now()
			}
		}
		time.Sleep(opts.PollInterval)
	}
	if lastOutput == "" {
		return "", "", errors.New("timeout waiting for codex report")
	}
	return lastOutput, "", errors.New("timeout waiting for codex report")
}

func parsePromptSegments(raw string) []promptSegment {
	return parsePromptSegmentsDual(raw, raw)
}

func parsePromptSegmentsDual(clean, raw string) []promptSegment {
	cleanLines := strings.Split(clean, "\n")
	rawLines := strings.Split(raw, "\n")
	if len(rawLines) < len(cleanLines) {
		pad := make([]string, len(cleanLines)-len(rawLines))
		rawLines = append(rawLines, pad...)
	}
	if len(cleanLines) < len(rawLines) {
		pad := make([]string, len(rawLines)-len(cleanLines))
		cleanLines = append(cleanLines, pad...)
	}
	segments := make([]promptSegment, 0, 8)
	var current *promptSegment
	for i, line := range cleanLines {
		rawLine := rawLines[i]
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "›") {
			if current != nil {
				segments = append(segments, *current)
			}
			prompt := strings.TrimSpace(strings.TrimPrefix(trimmed, "›"))
			current = &promptSegment{Prompt: prompt}
			continue
		}
		if current != nil {
			current.Lines = append(current.Lines, line)
			current.Raw = append(current.Raw, rawLine)
		}
	}
	if current != nil {
		segments = append(segments, *current)
	}
	return segments
}

func extractReportLines(lines []string) string {
	return extractReportLinesFromLines(lines, lines, false)
}

func extractReportLinesFromLines(rawLines, cleanLines []string, ansi bool) string {
	max := len(cleanLines)
	if len(rawLines) < max {
		max = len(rawLines)
	}
	type block struct {
		raw   []string
		clean []string
	}
	var blocks []block
	var current block
	inReport := false
	workedLineRaw := ""
	workedLineClean := ""
	for i := 0; i < max; i++ {
		raw := strings.TrimRight(rawLines[i], " \t")
		clean := strings.TrimRight(cleanLines[i], " \t")
		cleanCore := strings.TrimLeft(clean, " ")
		if strings.Contains(strings.ToLower(cleanCore), "worked for") {
			workedLineRaw = raw
			workedLineClean = clean
		}
		if strings.HasPrefix(cleanCore, "• ") {
			inReport = true
			current.raw = append(current.raw, raw)
			current.clean = append(current.clean, clean)
			continue
		}
		if !inReport {
			continue
		}
		if strings.TrimSpace(clean) == "" {
			if len(current.raw) > 0 {
				blocks = append(blocks, current)
				current = block{}
			}
			inReport = false
			continue
		}
		if strings.HasPrefix(clean, "  ") {
			current.raw = append(current.raw, raw)
			current.clean = append(current.clean, clean)
			continue
		}
		core := strings.TrimSpace(clean)
		if strings.HasPrefix(core, "⚠") || strings.HasPrefix(core, "Tip:") || strings.HasPrefix(core, "›") {
			if len(current.raw) > 0 {
				blocks = append(blocks, current)
			}
			current = block{}
			break
		}
		if strings.HasPrefix(core, "• Starting MCP") || strings.HasPrefix(core, "• Starting") {
			if len(current.raw) > 0 {
				blocks = append(blocks, current)
			}
			current = block{}
			break
		}
		current.raw = append(current.raw, raw)
		current.clean = append(current.clean, clean)
	}
	if len(current.raw) > 0 {
		blocks = append(blocks, current)
	}
	for i := len(blocks) - 1; i >= 0; i-- {
		block := blocks[i]
		if len(block.raw) == 0 {
			continue
		}
		if isTransientReport(block.clean) {
			continue
		}
		out := block.clean
		workedLine := workedLineClean
		if ansi {
			out = block.raw
			workedLine = workedLineRaw
		}
		for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
			out = out[:len(out)-1]
		}
		if workedLine != "" && !containsLine(out, workedLine) {
			out = append(out, workedLine)
		}
		return strings.Join(out, "\n")
	}
	return ""
}

func isTransientReport(lines []string) bool {
	if len(lines) == 0 {
		return true
	}
	head := strings.TrimSpace(lines[0])
	if strings.HasPrefix(head, "• Working") || strings.Contains(head, "esc to interrupt") {
		return true
	}
	if strings.HasPrefix(head, "• Starting MCP") {
		return true
	}
	return false
}

func containsLine(lines []string, needle string) bool {
	for _, line := range lines {
		if line == needle {
			return true
		}
	}
	return false
}
