package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types"
	"golang.org/x/term"

	shared "si/agents/shared/docker"
)

type dyadRow struct {
	Dyad   string
	Role   string
	Actor  string
	Critic string
}

type dyadAction struct {
	Name        string
	Description string
}

var dyadActions = []dyadAction{
	{Name: "spawn", Description: "create actor+critic dyad"},
	{Name: "list", Description: "list dyads"},
	{Name: "status", Description: "show dyad status"},
	{Name: "peek", Description: "peek into actor+critic tmux sessions"},
	{Name: "exec", Description: "run command in dyad (alias: run)"},
	{Name: "logs", Description: "show dyad logs"},
	{Name: "start", Description: "start dyad containers"},
	{Name: "stop", Description: "stop dyad containers"},
	{Name: "restart", Description: "restart dyad containers"},
	{Name: "remove", Description: "remove dyad"},
	{Name: "cleanup", Description: "remove stopped dyad containers"},
}

func isInteractiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func normalizeDyadCommand(cmd string) string {
	switch strings.ToLower(strings.TrimSpace(cmd)) {
	case "ps":
		return "list"
	case "teardown", "destroy", "rm", "delete":
		return "remove"
	case "run":
		return "exec"
	case "up":
		return "start"
	case "down":
		return "stop"
	default:
		return strings.ToLower(strings.TrimSpace(cmd))
	}
}

func normalizeDyadMember(member, fallback string) (string, error) {
	member = strings.ToLower(strings.TrimSpace(member))
	if member == "" {
		member = strings.ToLower(strings.TrimSpace(fallback))
	}
	if member == "" {
		member = "actor"
	}
	switch member {
	case "actor", "critic":
		return member, nil
	default:
		return "", fmt.Errorf("invalid member %q (expected actor or critic)", member)
	}
}

func parseMenuSelection(line string, options []string) (int, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return -1, nil
	}
	if isEscCancelInput(line) {
		return -1, nil
	}
	idx, err := strconv.Atoi(line)
	if err == nil {
		if idx < 1 || idx > len(options) {
			return -1, fmt.Errorf("invalid selection")
		}
		return idx - 1, nil
	}
	line = strings.TrimPrefix(line, "/")
	for i, option := range options {
		if strings.EqualFold(line, option) {
			return i, nil
		}
	}
	return -1, fmt.Errorf("invalid selection")
}

func promptLine(r io.Reader) (string, error) {
	if f, ok := r.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return promptTTYLine(f)
	}
	reader := bufio.NewReader(r)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func promptTTYLine(f *os.File) (string, error) {
	fd := int(f.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		reader := bufio.NewReader(f)
		line, readErr := reader.ReadString('\n')
		if readErr != nil && readErr != io.EOF {
			return "", readErr
		}
		return strings.TrimSpace(line), nil
	}
	defer func() { _ = term.Restore(fd, state) }()

	buf := make([]byte, 0, 64)
	one := []byte{0}
	for {
		n, readErr := f.Read(one)
		if readErr != nil {
			if readErr == io.EOF {
				// term.MakeRaw disables output post-processing, so emit CRLF explicitly.
				fmt.Fprint(os.Stdout, "\r\n")
				return strings.TrimSpace(string(buf)), nil
			}
			return "", readErr
		}
		if n == 0 {
			continue
		}
		b := one[0]
		switch b {
		case '\r', '\n':
			fmt.Fprint(os.Stdout, "\r\n")
			return strings.TrimSpace(string(buf)), nil
		case 0x1b:
			// ESC cancels immediately across interactive prompts.
			fmt.Fprint(os.Stdout, "\r\n")
			return "\x1b", nil
		case 0x7f, 0x08:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				fmt.Fprint(os.Stdout, "\b \b")
			}
		default:
			if b >= 0x20 && b != 0x7f {
				buf = append(buf, b)
				fmt.Fprintf(os.Stdout, "%c", b)
			}
		}
		if readErr == io.EOF {
			fmt.Fprint(os.Stdout, "\r\n")
			return strings.TrimSpace(string(buf)), nil
		}
	}
}

func promptRequired(prompt string) (string, bool) {
	prompt = strings.TrimSpace(strings.TrimSuffix(prompt, ":"))
	fmt.Printf("%s ", styleDim(fmt.Sprintf("%s (Esc to cancel):", prompt)))
	line, err := promptLine(os.Stdin)
	if err != nil {
		fatal(err)
	}
	if isEscCancelInput(line) {
		return "", false
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", false
	}
	return line, true
}

func promptWithDefault(prompt, def string) (string, bool) {
	def = strings.TrimSpace(def)
	if def == "" {
		return promptRequired(prompt)
	}
	prompt = strings.TrimSpace(strings.TrimSuffix(prompt, ":"))
	fmt.Printf("%s ", styleDim(fmt.Sprintf("%s (default %s, Esc to cancel):", prompt, def)))
	line, err := promptLine(os.Stdin)
	if err != nil {
		fatal(err)
	}
	if isEscCancelInput(line) {
		return "", false
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, true
	}
	return line, true
}

func selectDyadAction() (string, bool) {
	if !isInteractiveTerminal() {
		return "", false
	}
	fmt.Println(styleHeading("Dyad commands:"))
	options := make([]string, 0, len(dyadActions))
	for i, action := range dyadActions {
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

func buildDyadRows(containers []types.Container) []dyadRow {
	rows := map[string]*dyadRow{}
	for _, c := range containers {
		dyad := strings.TrimSpace(c.Labels[shared.LabelDyad])
		if dyad == "" {
			continue
		}
		item, ok := rows[dyad]
		if !ok {
			item = &dyadRow{
				Dyad: dyad,
				Role: strings.TrimSpace(c.Labels[shared.LabelRole]),
			}
			rows[dyad] = item
		}
		member := strings.TrimSpace(c.Labels[shared.LabelMember])
		switch member {
		case "actor":
			item.Actor = c.State
		case "critic":
			item.Critic = c.State
		}
	}
	keys := make([]string, 0, len(rows))
	for key := range rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]dyadRow, 0, len(keys))
	for _, key := range keys {
		out = append(out, *rows[key])
	}
	return out
}

func printDyadRows(rows []dyadRow) {
	headers := []string{
		styleHeading("DYAD"),
		styleHeading("ROLE"),
		styleHeading("ACTOR"),
		styleHeading("CRITIC"),
	}
	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		tableRows = append(tableRows, []string{
			row.Dyad,
			row.Role,
			styleStatus(row.Actor),
			styleStatus(row.Critic),
		})
	}
	printAlignedTable(headers, tableRows, 2)
}

func selectDyadName(action string) (string, bool) {
	client, err := shared.NewClient()
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	containers, err := client.ListContainers(context.Background(), true, map[string]string{shared.LabelApp: shared.DyadAppLabel})
	if err != nil {
		fatal(err)
	}
	rows := buildDyadRows(containers)
	if len(rows) == 0 {
		infof("no dyads found")
		return "", false
	}

	if !isInteractiveTerminal() {
		printDyadRows(rows)
		fmt.Println(styleDim("re-run with: si dyad " + action + " <name>"))
		return "", false
	}

	fmt.Println(styleHeading("Available dyads:"))
	printDyadRows(rows)
	options := make([]string, 0, len(rows))
	for _, row := range rows {
		options = append(options, row.Dyad)
	}
	fmt.Printf("%s ", styleDim(fmt.Sprintf("Select dyad [1-%d] or name (Enter/Esc to cancel):", len(options))))
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

func selectDyadMember(action string, defaultMember string) (string, bool) {
	if !isInteractiveTerminal() {
		member, err := normalizeDyadMember(defaultMember, "actor")
		if err != nil {
			fatal(err)
		}
		return member, true
	}
	options := []string{"actor", "critic"}
	defaultMember = strings.ToLower(strings.TrimSpace(defaultMember))
	prompt := fmt.Sprintf("Select %s member [1-%d] (default %s, Esc to cancel):", action, len(options), defaultMember)
	fmt.Printf("%s ", styleDim(prompt))
	line, err := promptLine(os.Stdin)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(line) == "" {
		member, err := normalizeDyadMember(defaultMember, "actor")
		if err != nil {
			fatal(err)
		}
		return member, true
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

func selectDyadRole(defaultRole string) (string, bool) {
	options := []string{"generic", "research", "infra", "web"}
	defaultRole = strings.ToLower(strings.TrimSpace(defaultRole))
	if defaultRole == "" {
		defaultRole = "generic"
	}
	if !isInteractiveTerminal() {
		return defaultRole, true
	}
	fmt.Printf("%s ", styleDim(fmt.Sprintf("Select dyad role [1-%d] (default %s, Esc to cancel):", len(options), defaultRole)))
	line, err := promptLine(os.Stdin)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(line) == "" {
		return defaultRole, true
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

func dyadSpawnBoolFlags() map[string]bool {
	return map[string]bool{
		"docker-socket": true,
		"skip-auth":     true,
		"autopilot":     true,
	}
}

func splitDyadSpawnArgs(args []string) (string, []string) {
	name := ""
	flagArgs := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	boolFlags := dyadSpawnBoolFlags()
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "" {
			continue
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			if name == "" {
				name = arg
			} else {
				positionals = append(positionals, arg)
			}
			continue
		}
		flagArgs = append(flagArgs, arg)
		flagName := strings.TrimLeft(arg, "-")
		if idx := strings.Index(flagName, "="); idx != -1 {
			continue
		}
		if boolFlags[flagName] {
			if i+1 < len(args) && isBoolLiteral(args[i+1]) {
				flagArgs[len(flagArgs)-1] = arg + "=" + strings.ToLower(strings.TrimSpace(args[i+1]))
				i++
			}
			continue
		}
		if i+1 < len(args) {
			flagArgs = append(flagArgs, args[i+1])
			i++
		}
	}
	out := append(flagArgs, positionals...)
	return name, out
}

func isBoolLiteral(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "false", "1", "0", "t", "f":
		return true
	default:
		return false
	}
}
