package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

type output struct {
	Turn           int    `json:"turn"`
	CapturedAt     string `json:"captured_at"`
	Status         string `json:"status"`
	ReadyForPrompt bool   `json:"ready_for_prompt"`
	FinalReport    string `json:"final_report"`
	Source         string `json:"source,omitempty"`
}

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type parser struct {
	mu         sync.Mutex
	promptRe   *regexp.Regexp
	ignoreRe   *regexp.Regexp
	readyRe    *regexp.Regexp
	endRe      *regexp.Regexp
	mode       string
	source     string
	stripAnsi  bool
	flushOnEOF bool
	eofReady   bool
	stripEnd   bool
	turn       int
	lastLine   string
	block      []string
	lastBlock  []string
	useSession bool
	logWait    time.Duration
	sessionLog string
	sessionIdx int
	onPrompt   func()
	onReady    func()
	onOutput   func()
	onEmit     func()
}

func newParser(promptRe, ignoreRe, readyRe, endRe *regexp.Regexp, mode, source string, stripAnsi, flushOnEOF, eofReady, stripEnd bool, useSession bool, logWait time.Duration, sessionLog string) *parser {
	return &parser{
		promptRe:   promptRe,
		ignoreRe:   ignoreRe,
		readyRe:    readyRe,
		endRe:      endRe,
		mode:       mode,
		source:     source,
		stripAnsi:  stripAnsi,
		flushOnEOF: flushOnEOF,
		eofReady:   eofReady,
		stripEnd:   stripEnd,
		turn:       1,
		useSession: useSession,
		logWait:    logWait,
		sessionLog: sessionLog,
	}
}

func (p *parser) emit(status string, ready bool) {
	p.mu.Lock()
	final := ""
	if p.mode == "last-line" {
		final = strings.TrimSpace(p.lastLine)
	} else {
		if len(p.block) == 0 {
			p.block = p.lastBlock
		}
		final = strings.TrimSpace(strings.Join(p.block, "\n"))
	}
	p.mu.Unlock()
	if p.useSession {
		if msg := p.nextSessionMessage(); msg != "" {
			final = msg
		}
	}
	if final == "" {
		p.mu.Lock()
		p.block = nil
		p.lastBlock = nil
		p.lastLine = ""
		p.mu.Unlock()
		return
	}
	p.mu.Lock()
	out := output{
		Turn:           p.turn,
		CapturedAt:     time.Now().UTC().Format(time.RFC3339),
		Status:         status,
		ReadyForPrompt: ready,
		FinalReport:    final,
		Source:         p.source,
	}
	p.turn++
	p.block = nil
	p.lastBlock = nil
	p.lastLine = ""
	p.mu.Unlock()
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "encode error: %v\n", err)
	}
	if p.onEmit != nil {
		p.onEmit()
	}
}

func (p *parser) disableFlush() {
	p.mu.Lock()
	p.flushOnEOF = false
	p.mu.Unlock()
}

func (p *parser) handleLine(line string) {
	if p.stripAnsi {
		line = stripANSI(line)
	}
	line = strings.TrimRight(line, "\r\n")
	trimmed := strings.TrimSpace(line)

	if p.promptRe.MatchString(trimmed) {
		p.emit("turn_complete", true)
		if p.onPrompt != nil {
			p.onPrompt()
		}
		return
	}
	if trimmed == "" {
		p.mu.Lock()
		if len(p.block) > 0 {
			p.lastBlock = append([]string(nil), p.block...)
			p.block = nil
		}
		p.mu.Unlock()
		return
	}
	if p.ignoreRe != nil && p.ignoreRe.MatchString(line) {
		return
	}
	if p.endRe != nil && p.endRe.MatchString(trimmed) {
		if p.stripEnd {
			p.emit("turn_complete_end", true)
			if p.onPrompt != nil {
				p.onPrompt()
			}
			return
		}
	}
	p.mu.Lock()
	p.block = append(p.block, line)
	p.lastLine = line
	p.mu.Unlock()
	if p.onOutput != nil {
		p.onOutput()
	}
	if p.readyRe != nil && p.readyRe.MatchString(trimmed) {
		if p.onReady != nil {
			p.onReady()
		}
	}
	if p.endRe != nil && p.endRe.MatchString(trimmed) && !p.stripEnd {
		p.emit("turn_complete_end", true)
		if p.onPrompt != nil {
			p.onPrompt()
		}
	}
}

func stripANSI(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if i+1 < len(s) {
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
				default:
					i++
					continue
				}
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func (p *parser) flushEOF() {
	p.mu.Lock()
	flush := p.flushOnEOF
	eofReady := p.eofReady
	p.mu.Unlock()
	if !flush {
		return
	}
	if eofReady {
		p.emit("turn_complete_exit", true)
		return
	}
	p.emit("eof", false)
}

func (p *parser) hasContent() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lastLine != "" {
		return true
	}
	if len(p.block) > 0 {
		return true
	}
	if len(p.lastBlock) > 0 {
		return true
	}
	return false
}

func (p *parser) nextSessionMessage() string {
	logPath := strings.TrimSpace(p.sessionLog)
	if logPath == "" {
		return ""
	}
	deadline := time.Now().Add(p.logWait)
	for {
		msgs, _ := readAgentMessages(logPath)
		p.mu.Lock()
		idx := p.sessionIdx
		p.mu.Unlock()
		if idx < len(msgs) {
			msg := strings.TrimSpace(msgs[idx])
			p.mu.Lock()
			p.sessionIdx++
			p.mu.Unlock()
			if msg != "" {
				return msg
			}
		}
		if p.logWait <= 0 || time.Now().After(deadline) {
			return ""
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func main() {
	var (
		promptRegex string
		ignoreRegex string
		readyRegex  string
		endRegex    string
		mode        string
		source      string
		stripAnsi   bool
		bufferBytes int
		flushOnEOF  bool
		command     string
		promptFile  string
		prompts     stringList
		sendExit    bool
		promptDelay time.Duration
		typeDelay   time.Duration
		pasteMode   bool
		sessionLog  string
		logWait     time.Duration
		rawLogPath  string
		startDelay  time.Duration
		idleTimeout time.Duration
		turnTimeout time.Duration
		submitSeq   string
		term        string
		lang        string
		waitReady   bool
		stripEnd    bool
		eofReady    bool
		maxTurns    int
		exitGrace   time.Duration
	)

	flag.StringVar(&promptRegex, "prompt-regex", `^(>\\s*|codex>\\s*|you>\\s*|user>\\s*)$`, "regex that marks the start of a new turn (flushes the current report)")
	flag.StringVar(&readyRegex, "ready-regex", `(?i)(context left|openai codex|>_)`, "regex that indicates the CLI is ready for the next prompt")
	flag.StringVar(&ignoreRegex, "ignore-regex", `^(\\s*[│╭╰╮╯╞╡╤╧╪─]+.*|\\s*>_.*|\\s*OpenAI Codex.*|\\s*model:.*|\\s*directory:.*|\\s*Tip:.*|\\s*›.*|\\s*↳.*|\\s*•\\s*(Working|Preparing).*|\\s*\\d+%\\s+context\\s+left.*)$`, "regex for lines to ignore (optional)")
	flag.StringVar(&endRegex, "end-regex", "^DONE$", "regex that marks end-of-turn output (optional)")
	flag.StringVar(&mode, "mode", "block", "report mode: block (last non-empty block) or last-line")
	flag.StringVar(&source, "source", "", "optional source label added to JSON")
	flag.BoolVar(&stripAnsi, "strip-ansi", true, "strip ANSI escape sequences")
	flag.IntVar(&bufferBytes, "buffer-bytes", 1024*1024, "max line length to buffer")
	flag.BoolVar(&flushOnEOF, "flush-on-eof", true, "emit a final report on EOF")
	flag.StringVar(&command, "command", "", "command to run in a PTY (enables active parsing)")
	flag.Var(&prompts, "prompt", "prompt to send (repeatable)")
	flag.StringVar(&promptFile, "prompt-file", "", "file with one prompt per line (optional)")
	flag.BoolVar(&sendExit, "send-exit", true, "send `exit` after prompts are done (PTY mode)")
	flag.DurationVar(&promptDelay, "prompt-delay", 200*time.Millisecond, "delay before sending each prompt (PTY mode)")
	flag.DurationVar(&typeDelay, "type-delay", 0, "delay between typed characters (PTY mode)")
	flag.BoolVar(&pasteMode, "bracketed-paste", false, "send prompts using bracketed paste sequences (PTY mode)")
	flag.StringVar(&sessionLog, "session-log", "", "path to a Codex TUI session log (PTY mode)")
	flag.DurationVar(&logWait, "session-log-wait", 2*time.Second, "wait for session log message before emitting (PTY mode)")
	flag.StringVar(&rawLogPath, "raw-log", "", "path to write raw PTY output for debugging (PTY mode)")
	flag.DurationVar(&startDelay, "start-delay", 800*time.Millisecond, "delay before sending the first prompt (PTY mode)")
	flag.DurationVar(&idleTimeout, "idle-timeout", 2*time.Second, "idle time before treating a turn as complete (PTY mode)")
	flag.DurationVar(&turnTimeout, "turn-timeout", 2*time.Minute, "timeout waiting for a turn to complete (PTY mode)")
	flag.StringVar(&submitSeq, "submit-seq", "\\r", "escape sequence to submit a prompt (e.g. \"\\r\" or \"\\x1b[13u\")")
	flag.StringVar(&term, "term", "xterm-256color", "TERM to set when running the PTY command")
	flag.StringVar(&lang, "lang", "en_US.UTF-8", "LANG/LC_ALL to set when running the PTY command")
	flag.BoolVar(&waitReady, "wait-ready", true, "wait for readiness before sending the first prompt (PTY mode)")
	flag.BoolVar(&stripEnd, "strip-end", true, "strip end marker line from final report")
	flag.BoolVar(&eofReady, "eof-ready", false, "mark EOF as ready for the next prompt (useful for single-command runs)")
	flag.IntVar(&maxTurns, "max-turns", 0, "stop after emitting N turns (PTY mode)")
	flag.DurationVar(&exitGrace, "exit-grace", 2*time.Second, "grace period before killing the child after max-turns (PTY mode)")
	flag.Parse()

	promptRe, err := regexp.Compile(promptRegex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid prompt-regex: %v\n", err)
		os.Exit(2)
	}
	var ignoreRe *regexp.Regexp
	if strings.TrimSpace(ignoreRegex) != "" {
		ignoreRe, err = regexp.Compile(ignoreRegex)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid ignore-regex: %v\n", err)
			os.Exit(2)
		}
	}
	var readyRe *regexp.Regexp
	if strings.TrimSpace(readyRegex) != "" {
		readyRe, err = regexp.Compile(readyRegex)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid ready-regex: %v\n", err)
			os.Exit(2)
		}
	}
	var endRe *regexp.Regexp
	if strings.TrimSpace(endRegex) != "" {
		endRe, err = regexp.Compile(endRegex)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid end-regex: %v\n", err)
			os.Exit(2)
		}
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "block" && mode != "last-line" {
		fmt.Fprintf(os.Stderr, "invalid mode: %s (expected block or last-line)\n", mode)
		os.Exit(2)
	}

	parser := newParser(promptRe, ignoreRe, readyRe, endRe, mode, source, stripAnsi, flushOnEOF, eofReady, stripEnd, strings.TrimSpace(sessionLog) != "", logWait, sessionLog)

	if command == "" {
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 0, 64*1024), bufferBytes)
		for scanner.Scan() {
			parser.handleLine(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "read error: %v\n", err)
		}
		parser.flushEOF()
		return
	}

	if promptFile != "" {
		b, err := os.ReadFile(promptFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "prompt-file read error: %v\n", err)
			os.Exit(2)
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			prompts = append(prompts, line)
		}
	}

	runWithPTY(command, prompts, promptDelay, typeDelay, pasteMode, sendExit, bufferBytes, rawLogPath, startDelay, idleTimeout, turnTimeout, decodeEscapes(submitSeq), term, lang, waitReady, strings.TrimSpace(sessionLog), parser, maxTurns, exitGrace)
}

func runWithPTY(command string, prompts []string, promptDelay, typeDelay time.Duration, pasteMode, sendExit bool, bufferBytes int, rawLogPath string, startDelay, idleTimeout, turnTimeout time.Duration, submitSeq, term, lang string, waitReadyFirst bool, sessionLog string, parser *parser, maxTurns int, exitGrace time.Duration) {
	cmd := exec.Command("bash", "-lc", command)
	cmd.Env = os.Environ()
	if strings.TrimSpace(term) != "" {
		cmd.Env = append(cmd.Env, "TERM="+term)
	}
	if strings.TrimSpace(lang) != "" {
		cmd.Env = append(cmd.Env, "LANG="+lang, "LC_ALL="+lang)
	}
	if strings.TrimSpace(sessionLog) != "" {
		cmd.Env = append(cmd.Env, "CODEX_TUI_RECORD_SESSION=true", "CODEX_TUI_SESSION_LOG_PATH="+sessionLog)
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pty start error: %v\n", err)
		os.Exit(1)
	}
	_ = pty.Setsize(ptmx, &pty.Winsize{Cols: 120, Rows: 40})
	defer func() { _ = ptmx.Close() }()

	turnDoneCh := make(chan struct{}, 1)
	readyCh := make(chan struct{}, 1)
	activityCh := make(chan struct{}, 1)
	exitCh := make(chan struct{}, 1)
	doneCh := make(chan struct{})
	doneOnce := sync.Once{}
	signalDone := func() {
		doneOnce.Do(func() {
			close(doneCh)
		})
	}
	stateMu := sync.Mutex{}
	turnActive := false
	outputSeen := false
	emitCount := 0
	terminated := sync.Once{}

	terminate := func() {
		terminated.Do(func() {
			_ = ptmx.Close()
			if cmd.Process != nil {
				_ = cmd.Process.Signal(syscall.SIGTERM)
				if exitGrace > 0 {
					go func() {
						time.Sleep(exitGrace)
						_ = cmd.Process.Kill()
					}()
				}
			}
			select {
			case exitCh <- struct{}{}:
			default:
			}
			signalDone()
		})
	}

	setTurnActive := func() {
		stateMu.Lock()
		turnActive = true
		outputSeen = false
		stateMu.Unlock()
	}
	setTurnDone := func() {
		stateMu.Lock()
		turnActive = false
		outputSeen = false
		stateMu.Unlock()
		select {
		case turnDoneCh <- struct{}{}:
		default:
		}
	}
	parser.onPrompt = func() {
		setTurnDone()
	}
	parser.onReady = func() {
		stateMu.Lock()
		active := turnActive
		seen := outputSeen
		stateMu.Unlock()
		if active && seen && parser.hasContent() {
			parser.emit("turn_complete_ready", true)
			setTurnDone()
			return
		}
		select {
		case readyCh <- struct{}{}:
		default:
		}
	}
	parser.onOutput = func() {
		stateMu.Lock()
		if turnActive {
			outputSeen = true
		}
		stateMu.Unlock()
		select {
		case activityCh <- struct{}{}:
		default:
		}
	}
	parser.onEmit = func() {
		emitCount++
		if maxTurns > 0 && emitCount >= maxTurns {
			parser.disableFlush()
			terminate()
		}
	}

	go func() {
		readPTY(ptmx, ptmx, bufferBytes, rawLogPath, parser)
		signalDone()
	}()

	sendPrompt := func(prompt string) {
		if strings.TrimSpace(prompt) == "" {
			return
		}
		time.Sleep(promptDelay)
		write := func(data []byte) {
			if len(data) == 0 {
				return
			}
			_, _ = ptmx.Write(data)
		}
		if pasteMode {
			write([]byte("\x1b[200~"))
			write([]byte(prompt))
			write([]byte("\x1b[201~"))
		} else if typeDelay > 0 {
			for _, r := range prompt {
				write([]byte(string(r)))
				time.Sleep(typeDelay)
			}
			time.Sleep(typeDelay)
		} else {
			write([]byte(prompt))
		}
		if submitSeq != "" {
			write([]byte(submitSeq))
		}
	}

	waitTurnDone := func() bool {
		select {
		case <-turnDoneCh:
			return true
		case <-time.After(turnTimeout):
			fmt.Fprintf(os.Stderr, "timeout waiting for turn completion\n")
			return false
		}
	}
	waitReady := func() bool {
		if !waitReadyFirst {
			return true
		}
		select {
		case <-readyCh:
			return true
		case <-time.After(turnTimeout):
			fmt.Fprintf(os.Stderr, "timeout waiting for readiness\n")
			return false
		}
	}

	go func() {
		timer := time.NewTimer(idleTimeout)
		defer timer.Stop()
		for {
			select {
			case <-activityCh:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(idleTimeout)
			case <-timer.C:
				stateMu.Lock()
				active := turnActive
				seen := outputSeen
				stateMu.Unlock()
				if active && seen && parser.hasContent() {
					parser.emit("turn_complete_idle", true)
					setTurnDone()
				}
				timer.Reset(idleTimeout)
			case <-doneCh:
				return
			case <-exitCh:
				return
			}
		}
	}()

	first := true
	for _, prompt := range prompts {
		if first {
			if startDelay > 0 {
				time.Sleep(startDelay)
			}
			if !waitReady() {
				break
			}
			first = false
		} else if !waitTurnDone() {
			break
		}
		sendPrompt(prompt)
		setTurnActive()
	}

	if sendExit {
		if waitTurnDone() {
			sendPrompt("exit")
		}
	}

	select {
	case <-doneCh:
	case <-exitCh:
	case <-time.After(turnTimeout + idleTimeout + exitGrace + 2*time.Second):
	}
	waitProcess(cmd, exitGrace+2*time.Second)
}

func waitProcess(cmd *exec.Cmd, timeout time.Duration) {
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()
	if timeout <= 0 {
		<-waitCh
		return
	}
	select {
	case <-waitCh:
		return
	case <-time.After(timeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	select {
	case <-waitCh:
	case <-time.After(500 * time.Millisecond):
	}
}

type sessionLogEntry struct {
	Kind    string `json:"kind"`
	Payload struct {
		Msg struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"msg"`
	} `json:"payload"`
}

func readAgentMessages(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var messages []string
	for scanner.Scan() {
		line := strings.ReplaceAll(scanner.Text(), "\x00", "")
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var entry sessionLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Kind != "codex_event" {
			continue
		}
		if entry.Payload.Msg.Type == "agent_message" && strings.TrimSpace(entry.Payload.Msg.Message) != "" {
			messages = append(messages, entry.Payload.Msg.Message)
		}
	}
	if err := scanner.Err(); err != nil {
		return messages, err
	}
	return messages, nil
}

func readPTY(r io.Reader, w io.Writer, bufferBytes int, rawLogPath string, parser *parser) {
	buf := make([]byte, 4096)
	line := bytes.Buffer{}
	seq := []byte{0x1b, '[', '6', 'n'}
	seqMatch := 0
	var rawLog io.Writer
	if strings.TrimSpace(rawLogPath) != "" {
		f, err := os.OpenFile(rawLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			fmt.Fprintf(os.Stderr, "raw-log open error: %v\n", err)
		} else {
			defer f.Close()
			rawLog = f
		}
	}

	for {
		n, err := r.Read(buf)
		if n > 0 {
			if rawLog != nil {
				_, _ = rawLog.Write(buf[:n])
			}
			for _, b := range buf[:n] {
				if b == seq[seqMatch] {
					seqMatch++
					if seqMatch == len(seq) {
						_, _ = w.Write([]byte("\x1b[24;80R"))
						seqMatch = 0
					}
				} else {
					seqMatch = 0
				}

				if b == '\n' {
					parser.handleLine(line.String())
					line.Reset()
					continue
				}
				if b == '\r' {
					if line.Len() > 0 {
						parser.handleLine(line.String())
						line.Reset()
					}
					continue
				}
				if line.Len() < bufferBytes {
					line.WriteByte(b)
				}
			}
		}
		if err != nil {
			break
		}
	}
	if line.Len() > 0 {
		parser.handleLine(line.String())
	}
	parser.flushEOF()
}

func decodeEscapes(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	quoted := value
	if !strings.HasPrefix(quoted, "\"") {
		quoted = "\"" + quoted + "\""
	}
	unquoted, err := strconv.Unquote(quoted)
	if err != nil {
		return value
	}
	return unquoted
}
