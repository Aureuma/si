package main

import (
	"fmt"
	"os"
	"strings"
)

type subcommandAction struct {
	Name        string
	Description string
}

func resolveSubcommandDispatchArgs(args []string, interactive bool, selectFn func() (string, bool)) ([]string, bool, bool) {
	if len(args) > 0 {
		return args, false, true
	}
	if !interactive {
		return nil, true, false
	}
	selected, ok := selectFn()
	if !ok {
		return nil, false, false
	}
	return []string{selected}, false, true
}

func selectSubcommandAction(title string, actions []subcommandAction) (string, bool) {
	if !isInteractiveTerminal() {
		return "", false
	}
	if len(actions) == 0 {
		return "", false
	}
	fmt.Println(styleHeading(title))
	options := make([]string, 0, len(actions))
	for i, action := range actions {
		fmt.Printf("  %2d) %-10s %s\n", i+1, action.Name, styleDim(action.Description))
		options = append(options, action.Name)
	}
	fmt.Printf("%s ", styleDim(fmt.Sprintf("Select command [1-%d] (Enter/Esc to cancel):", len(options))))
	line, err := promptLine(os.Stdin)
	if err != nil {
		fatal(err)
	}
	idx, err := parseMenuSelection(line, options)
	if err != nil {
		fmt.Println(styleDim("invalid selection"))
		return "", false
	}
	if idx < 0 {
		return "", false
	}
	return options[idx], true
}

func resolveUsageSubcommandArgs(args []string, usageText string) ([]string, bool) {
	if len(args) > 0 {
		return args, true
	}
	usage := strings.TrimSpace(usageText)
	options := parseUsageSubcommandOptions(usage)
	if !isInteractiveTerminal() || len(options) == 0 {
		printUsage(usageText)
		return nil, false
	}
	actions := make([]subcommandAction, 0, len(options))
	for _, option := range options {
		actions = append(actions, subcommandAction{Name: option, Description: "select " + option})
	}
	title := usageSubcommandTitle(usage)
	selected, ok := selectSubcommandAction(title, actions)
	if !ok {
		return nil, false
	}
	return []string{selected}, true
}

func usageSubcommandTitle(usage string) string {
	usage = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(usage), "usage:"))
	if usage == "" {
		return "Commands:"
	}
	if idx := strings.Index(usage, "<"); idx != -1 {
		usage = strings.TrimSpace(usage[:idx])
	}
	if usage == "" {
		return "Commands:"
	}
	return usage + " commands:"
}

func parseUsageSubcommandOptions(usage string) []string {
	fields := strings.Fields(usage)
	if len(fields) == 0 {
		return nil
	}
	var raw string
	for _, token := range fields {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if strings.HasPrefix(token, "[") {
			break
		}
		if strings.HasPrefix(token, "--") {
			continue
		}
		if strings.HasPrefix(token, "<") {
			end := strings.Index(token, ">")
			if end == -1 {
				return nil
			}
			raw = strings.TrimSpace(token[1:end])
			break
		}
	}
	if raw == "" || !strings.Contains(raw, "|") {
		return nil
	}
	parts := strings.Split(raw, "|")
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}
