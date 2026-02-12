package main

import (
	"fmt"
	"os"
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
