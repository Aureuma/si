package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

type stepList []string

func (s *stepList) String() string {
	return strings.Join(*s, ",")
}

func (s *stepList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type actionKind string

const (
	actionWaitPrompt   actionKind = "wait_prompt"
	actionSend         actionKind = "send"
	actionType         actionKind = "type"
	actionKey          actionKind = "key"
	actionSleep        actionKind = "sleep"
	actionWaitContains actionKind = "wait_contains"
)

type action struct {
	Kind    actionKind
	Arg     string
	Timeout time.Duration
}

type runner struct {
	mu       sync.Mutex
	output   []byte
	maxBytes int

	cmd     *exec.Cmd
	ptmx    *os.File
	doneCh  chan error
	prompt  *regexp.Regexp
	poll    time.Duration
	defWait time.Duration
}

func newRunner(command string, promptRe *regexp.Regexp, maxBytes int, defWait time.Duration) (*runner, error) {
	if strings.TrimSpace(command) == "" {
		return nil, errors.New("command required")
	}
	if promptRe == nil {
		return nil, errors.New("prompt regex required")
	}
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	if defWait <= 0 {
		defWait = 20 * time.Second
	}
	cmd := exec.Command("bash", "-lc", command)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	r := &runner{
		cmd:      cmd,
		ptmx:     ptmx,
		doneCh:   make(chan error, 1),
		prompt:   promptRe,
		poll:     50 * time.Millisecond,
		maxBytes: maxBytes,
		defWait:  defWait,
	}
	go r.readLoop()
	go func() {
		r.doneCh <- cmd.Wait()
	}()
	return r, nil
}

func (r *runner) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := r.ptmx.Read(buf)
		if n > 0 {
			r.appendOutput(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (r *runner) appendOutput(chunk []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(chunk) >= r.maxBytes {
		r.output = append([]byte(nil), chunk[len(chunk)-r.maxBytes:]...)
		return
	}
	need := len(r.output) + len(chunk) - r.maxBytes
	if need > 0 {
		r.output = append([]byte(nil), r.output[need:]...)
	}
	r.output = append(r.output, chunk...)
}

func (r *runner) outputString() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(append([]byte(nil), r.output...))
}

func (r *runner) tail(lines int) string {
	if lines <= 0 {
		lines = 80
	}
	text := r.outputString()
	parts := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	if len(parts) <= lines {
		return strings.Join(parts, "\n")
	}
	return strings.Join(parts[len(parts)-lines:], "\n")
}

func (r *runner) send(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	_, err := r.ptmx.Write([]byte(text))
	return err
}

func (r *runner) sendLine(line string) error {
	if _, err := r.ptmx.Write([]byte(line)); err != nil {
		return err
	}
	_, err := r.ptmx.Write([]byte("\n"))
	return err
}

func decodeKey(name string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "enter", "return":
		return "\r", nil
	case "tab":
		return "\t", nil
	case "esc", "escape":
		return "\x1b", nil
	case "up", "arrowup":
		return "\x1b[A", nil
	case "down", "arrowdown":
		return "\x1b[B", nil
	case "left", "arrowleft":
		return "\x1b[D", nil
	case "right", "arrowright":
		return "\x1b[C", nil
	case "ctrl-c":
		return "\x03", nil
	default:
		return "", fmt.Errorf("unsupported key %q", name)
	}
}

func (r *runner) waitPrompt(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = r.defWait
	}
	deadline := time.Now().Add(timeout)
	for {
		if r.prompt.MatchString(lastLine(stripANSI(r.outputString()))) {
			return nil
		}
		select {
		case err := <-r.doneCh:
			if err != nil {
				return fmt.Errorf("process exited while waiting for prompt: %w", err)
			}
			return errors.New("process exited while waiting for prompt")
		default:
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for prompt")
		}
		time.Sleep(r.poll)
	}
}

func (r *runner) waitContains(substr string, timeout time.Duration) error {
	substr = strings.TrimSpace(substr)
	if substr == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = r.defWait
	}
	deadline := time.Now().Add(timeout)
	for {
		if strings.Contains(stripANSI(r.outputString()), substr) {
			return nil
		}
		select {
		case err := <-r.doneCh:
			if err != nil {
				return fmt.Errorf("process exited while waiting for %q: %w", substr, err)
			}
			return fmt.Errorf("process exited while waiting for %q", substr)
		default:
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for %q", substr)
		}
		time.Sleep(r.poll)
	}
}

func (r *runner) waitExit(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	select {
	case err := <-r.doneCh:
		return err
	case <-time.After(timeout):
		return errors.New("timeout waiting for process exit")
	}
}

func (r *runner) close() {
	if r == nil || r.ptmx == nil {
		return
	}
	_ = r.ptmx.Close()
}

func parseDurationOr(def string, fallback time.Duration) time.Duration {
	def = strings.TrimSpace(def)
	if def == "" {
		return fallback
	}
	d, err := time.ParseDuration(def)
	if err != nil {
		return fallback
	}
	return d
}

func parseAction(spec string, defaultWait time.Duration) (action, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return action{}, errors.New("empty action")
	}
	if strings.EqualFold(spec, string(actionWaitPrompt)) {
		return action{Kind: actionWaitPrompt, Timeout: defaultWait}, nil
	}
	if strings.HasPrefix(spec, string(actionWaitPrompt)+":") || strings.HasPrefix(spec, string(actionWaitPrompt)+"=") {
		parts := strings.SplitN(spec, ":", 2)
		if len(parts) != 2 {
			parts = strings.SplitN(spec, "=", 2)
		}
		return action{Kind: actionWaitPrompt, Timeout: parseDurationOr(parts[1], defaultWait)}, nil
	}

	parseWithArg := func(kind actionKind) (action, bool) {
		prefix1 := string(kind) + ":"
		prefix2 := string(kind) + " "
		if strings.HasPrefix(spec, prefix1) {
			return action{Kind: kind, Arg: strings.TrimSpace(spec[len(prefix1):]), Timeout: defaultWait}, true
		}
		if strings.HasPrefix(spec, prefix2) {
			return action{Kind: kind, Arg: strings.TrimSpace(spec[len(prefix2):]), Timeout: defaultWait}, true
		}
		return action{}, false
	}

	if out, ok := parseWithArg(actionSend); ok {
		return out, nil
	}
	if out, ok := parseWithArg(actionType); ok {
		return out, nil
	}
	if out, ok := parseWithArg(actionKey); ok {
		return out, nil
	}
	if out, ok := parseWithArg(actionWaitContains); ok {
		// Optional timeout suffix: wait_contains:<text>|2s
		if parts := strings.SplitN(out.Arg, "|", 2); len(parts) == 2 {
			out.Arg = strings.TrimSpace(parts[0])
			out.Timeout = parseDurationOr(parts[1], defaultWait)
		}
		return out, nil
	}
	if out, ok := parseWithArg(actionSleep); ok {
		out.Timeout = parseDurationOr(out.Arg, defaultWait)
		out.Arg = ""
		return out, nil
	}
	return action{}, fmt.Errorf("unsupported action %q", spec)
}

func readScript(path string) ([]string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	out := []string{}
	s := bufio.NewScanner(file)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, s.Err()
}

func normalizeActions(raw []string, defaultWait time.Duration) ([]action, error) {
	out := make([]action, 0, len(raw))
	for _, spec := range raw {
		step, err := parseAction(spec, defaultWait)
		if err != nil {
			return nil, err
		}
		out = append(out, step)
	}
	return out, nil
}

func runPlan(r *runner, actions []action) error {
	for i, step := range actions {
		idx := i + 1
		switch step.Kind {
		case actionWaitPrompt:
			if err := r.waitPrompt(step.Timeout); err != nil {
				return fmt.Errorf("step %d wait_prompt: %w\n--- tail ---\n%s", idx, err, r.tail(80))
			}
		case actionSend:
			if err := r.sendLine(step.Arg); err != nil {
				return fmt.Errorf("step %d send: %w", idx, err)
			}
		case actionType:
			if err := r.send(step.Arg); err != nil {
				return fmt.Errorf("step %d type: %w", idx, err)
			}
		case actionKey:
			decoded, err := decodeKey(step.Arg)
			if err != nil {
				return fmt.Errorf("step %d key: %w", idx, err)
			}
			if err := r.send(decoded); err != nil {
				return fmt.Errorf("step %d key send: %w", idx, err)
			}
		case actionSleep:
			time.Sleep(step.Timeout)
		case actionWaitContains:
			if err := r.waitContains(step.Arg, step.Timeout); err != nil {
				return fmt.Errorf("step %d wait_contains: %w\n--- tail ---\n%s", idx, err, r.tail(80))
			}
		default:
			return fmt.Errorf("step %d unsupported action kind %q", idx, step.Kind)
		}
	}
	return nil
}

func lastLine(text string) string {
	text = strings.ReplaceAll(text, "\r", "")
	parts := strings.Split(text, "\n")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func stripANSI(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) {
			switch s[i+1] {
			case '[':
				i += 2
				for i < len(s) {
					c := s[i]
					if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
						i++
						break
					}
					i++
				}
				continue
			case ']':
				i += 2
				for i < len(s) {
					if s[i] == 0x07 {
						i++
						break
					}
					if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func main() {
	var (
		command       string
		scriptPath    string
		promptRegex   string
		defaultWait   time.Duration
		finalWait     time.Duration
		maxBytes      int
		printOutput   bool
		noInitialWait bool
	)
	steps := stepList{}

	flag.StringVar(&command, "command", "", "interactive command to run")
	flag.StringVar(&scriptPath, "script", "", "path to action script")
	flag.Var(&steps, "step", "action step (repeatable)")
	flag.StringVar(&promptRegex, "prompt-regex", `^›\s*$`, "prompt regex")
	flag.DurationVar(&defaultWait, "wait", 20*time.Second, "default wait timeout")
	flag.DurationVar(&finalWait, "final-wait", 2*time.Second, "wait for process exit after plan")
	flag.IntVar(&maxBytes, "max-bytes", 1<<20, "max output bytes retained")
	flag.BoolVar(&printOutput, "print-output", false, "print captured output at end")
	flag.BoolVar(&noInitialWait, "no-initial-wait", false, "skip automatic initial wait_prompt")
	flag.Parse()

	if strings.TrimSpace(command) == "" {
		fmt.Fprintln(os.Stderr, "-command is required")
		os.Exit(2)
	}
	promptRe, err := regexp.Compile(promptRegex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid prompt regex: %v\n", err)
		os.Exit(2)
	}
	scriptSteps, err := readScript(scriptPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read script: %v\n", err)
		os.Exit(2)
	}
	raw := make([]string, 0, len(scriptSteps)+len(steps)+1)
	if !noInitialWait {
		raw = append(raw, "wait_prompt")
	}
	raw = append(raw, scriptSteps...)
	raw = append(raw, steps...)
	actions, err := normalizeActions(raw, defaultWait)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse actions: %v\n", err)
		os.Exit(2)
	}
	r, err := newRunner(command, promptRe, maxBytes, defaultWait)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start: %v\n", err)
		os.Exit(1)
	}
	defer r.close()

	if err := runPlan(r, actions); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	if err := r.waitExit(finalWait); err != nil {
		fmt.Fprintf(os.Stderr, "wait exit: %v\n", err)
		if printOutput {
			fmt.Print(r.outputString())
		}
		os.Exit(1)
	}
	if printOutput {
		fmt.Print(r.outputString())
	}
}
