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
	Dept   string
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
	{Name: "run", Description: "run command in dyad (alias: exec)"},
	{Name: "logs", Description: "show dyad logs"},
	{Name: "restart", Description: "restart dyad containers"},
	{Name: "remove", Description: "remove dyad"},
	{Name: "cleanup", Description: "remove stopped dyad containers"},
	{Name: "login", Description: "copy codex login into dyad (alias: copy-login)"},
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
	case "start":
		return "restart"
	case "login", "codex-login-copy":
		return "copy-login"
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
	reader := bufio.NewReader(r)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func promptRequired(prompt string) (string, bool) {
	fmt.Printf("%s ", styleDim(prompt))
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
	fmt.Printf("%s ", styleDim(fmt.Sprintf("%s (default %s):", prompt, def)))
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
	fmt.Printf("%s ", styleDim(fmt.Sprintf("Select command [1-%d] (or press Enter to cancel):", len(options))))
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
				Dept: strings.TrimSpace(c.Labels[shared.LabelDept]),
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
	widths := map[string]int{"dyad": 4, "role": 4, "dept": 4, "actor": 5, "critic": 6}
	for _, row := range rows {
		widths["dyad"] = max(widths["dyad"], len(row.Dyad))
		widths["role"] = max(widths["role"], len(row.Role))
		widths["dept"] = max(widths["dept"], len(row.Dept))
		widths["actor"] = max(widths["actor"], len(row.Actor))
		widths["critic"] = max(widths["critic"], len(row.Critic))
	}

	fmt.Printf("%s  %s  %s  %s  %s\n",
		padRightANSI(styleHeading("DYAD"), widths["dyad"]),
		padRightANSI(styleHeading("ROLE"), widths["role"]),
		padRightANSI(styleHeading("DEPT"), widths["dept"]),
		padRightANSI(styleHeading("ACTOR"), widths["actor"]),
		padRightANSI(styleHeading("CRITIC"), widths["critic"]),
	)
	for _, row := range rows {
		fmt.Printf("%s  %s  %s  %s  %s\n",
			padRightANSI(row.Dyad, widths["dyad"]),
			padRightANSI(row.Role, widths["role"]),
			padRightANSI(row.Dept, widths["dept"]),
			padRightANSI(styleStatus(row.Actor), widths["actor"]),
			padRightANSI(styleStatus(row.Critic), widths["critic"]),
		)
	}
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
	fmt.Printf("%s ", styleDim(fmt.Sprintf("Select dyad [1-%d] or name (or press Enter to cancel):", len(options))))
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
	prompt := fmt.Sprintf("Select %s member [1-%d] (default %s):", action, len(options), defaultMember)
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
	fmt.Printf("%s ", styleDim(fmt.Sprintf("Select dyad role [1-%d] (default %s):", len(options), defaultRole)))
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
