package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	turn := 0
	fmt.Println("fake-codex ready")
	printPrompt(cfg.promptChar)

	r := bufio.NewReader(os.Stdin)
	for {
		line, err := r.ReadString('\n')
		if err != nil && len(line) == 0 {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		turn++

		if cfg.delay > 0 {
			time.Sleep(cfg.delay)
		}

		handled, shouldExit := handleSpecial(line, cfg.promptChar)
		if handled {
			if shouldExit {
				return
			}
			continue
		}

		emitLong := false
		if cfg.longLines > 0 {
			emitLong = true
		} else if cfg.longIfContains != "" && strings.Contains(line, cfg.longIfContains) {
			emitLong = true
			cfg.longLines = 12000
		}

		if emitLong {
			for i := 1; i <= cfg.longLines; i++ {
				fmt.Printf("line %d\n", i)
			}
		} else {
			fmt.Println("ok")
		}

		member := os.Getenv("DYAD_MEMBER")
		if member == "" {
			member = "unknown"
		}
		sig := truncateRunes(line, 60)

		if cfg.noMarkers {
			emitBody(member, turn, sig, false)
		} else {
			fmt.Println("<<WORK_REPORT_BEGIN>>")
			emitBody(member, turn, sig, true)
			fmt.Println("<<WORK_REPORT_END>>")
		}
		printPrompt(cfg.promptChar)
	}
}

type config struct {
	promptChar     string
	delay          time.Duration
	longLines      int
	longIfContains string
	noMarkers      bool
}

func loadConfig() (*config, error) {
	promptChar := os.Getenv("FAKE_CODEX_PROMPT_CHAR")
	if promptChar == "" {
		promptChar = "›"
	}
	delaySeconds := os.Getenv("FAKE_CODEX_DELAY_SECONDS")
	if delaySeconds == "" {
		delaySeconds = "0"
	}
	delayValue, err := strconv.ParseFloat(delaySeconds, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid FAKE_CODEX_DELAY_SECONDS: %w", err)
	}

	longLines := 0
	if raw := os.Getenv("FAKE_CODEX_LONG_LINES"); raw != "" {
		longLines, err = strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid FAKE_CODEX_LONG_LINES: %w", err)
		}
	}

	noMarkers := false
	if raw := os.Getenv("FAKE_CODEX_NO_MARKERS"); raw != "" {
		noMarkers = raw != "0"
	}

	return &config{
		promptChar:     promptChar,
		delay:          time.Duration(delayValue * float64(time.Second)),
		longLines:      longLines,
		longIfContains: os.Getenv("FAKE_CODEX_LONG_IF_CONTAINS"),
		noMarkers:      noMarkers,
	}, nil
}

func printPrompt(promptChar string) {
	fmt.Printf("%s ", promptChar)
}

func handleSpecial(line string, promptChar string) (handled bool, shouldExit bool) {
	switch {
	case line == "/status":
		fmt.Println("status: ok")
	case strings.HasPrefix(line, "/model"):
		fmt.Println("model: gpt-5.2-codex")
	case strings.HasPrefix(line, "/approval"):
		fmt.Println("approval: auto")
	case strings.HasPrefix(line, "/sandbox"):
		fmt.Println("sandbox: workspace-write")
	case line == "/agents":
		fmt.Println("agents: actor,critic")
	case line == "/prompts":
		fmt.Println("prompts: available")
	case line == "/review":
		fmt.Println("review: started")
	case line == "/compact":
		fmt.Println("compact: done")
	case line == "/clear":
		fmt.Println("clear: done")
	case line == "/help":
		fmt.Println("help: commands listed")
	case line == "/logout":
		fmt.Println("logout: skipped")
	case line == "/vim":
		fmt.Println("vim: enabled")
	case line == "/":
		fmt.Println("menu: /status /model /approval")
	case line == "1":
		fmt.Println("menu-select: /status")
	case line == "2":
		fmt.Println("menu-select: /model")
	case strings.HasPrefix(line, "\x1b[A") || strings.HasPrefix(line, "^[[A"):
		fmt.Println("menu-select: /status")
	case strings.HasPrefix(line, "\x1b[B") || strings.HasPrefix(line, "^[[B"):
		fmt.Println("menu-select: /model")
	case line == "/exit":
		fmt.Println("bye")
		return true, true
	default:
		return false, false
	}
	printPrompt(promptChar)
	return true, false
}

func emitBody(member string, turn int, sig string, _ bool) {
	if member == "critic" {
		fmt.Println("Assessment:")
		fmt.Printf("- member: %s\n", member)
		fmt.Printf("- turn: %d\n", turn)
		fmt.Printf("- input_sig: %s\n", sig)
		fmt.Println("Risks:")
		fmt.Println("- none")
		fmt.Println("Required Fixes:")
		fmt.Println("- none")
		fmt.Println("Verification Steps:")
		fmt.Println("- none")
		fmt.Println("Next Actor Prompt:")
		fmt.Println("- proceed")
		fmt.Println("Continue Loop: yes")
		return
	}
	fmt.Println("Summary:")
	fmt.Printf("- member: %s\n", member)
	fmt.Printf("- turn: %d\n", turn)
	fmt.Printf("- input_sig: %s\n", sig)
	fmt.Println("Changes:")
	fmt.Println("- none")
	fmt.Println("Validation:")
	fmt.Println("- none")
	fmt.Println("Open Questions:")
	fmt.Println("- none")
	fmt.Println("Next Step for Critic:")
	fmt.Println("- proceed")
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
