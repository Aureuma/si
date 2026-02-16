package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

func cmdGooglePlacesSession(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si google places session <new|inspect|end|list>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "new", "create":
		cmdGooglePlacesSessionNew(rest)
	case "inspect", "get":
		cmdGooglePlacesSessionInspect(rest)
	case "end", "close", "delete", "remove":
		cmdGooglePlacesSessionEnd(rest)
	case "list":
		cmdGooglePlacesSessionList(rest)
	default:
		printUnknown("google places session", sub)
		printUsage("usage: si google places session <new|inspect|end|list>")
	}
}

func cmdGooglePlacesSessionNew(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("google places session new", flag.ExitOnError)
	token := fs.String("token", "", "explicit session token")
	account := fs.String("account", "", "account alias metadata")
	note := fs.String("note", "", "optional note")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google places session new [--token <token>] [--account <alias>] [--note <text>] [--json]")
		return
	}
	store, err := loadGooglePlacesSessionStore()
	if err != nil {
		fatal(err)
	}
	value := strings.TrimSpace(*token)
	if value == "" {
		value, err = generateGooglePlacesSessionToken()
		if err != nil {
			fatal(err)
		}
	}
	if _, exists := store.Sessions[value]; exists {
		fatal(fmt.Errorf("session token already exists: %s", value))
	}
	alias := strings.TrimSpace(*account)
	if alias == "" {
		alias = googlePlacesDefaultAccountAlias()
	}
	now := googlePlacesNowRFC3339()
	entry := googlePlacesSessionEntry{
		Token:        value,
		AccountAlias: alias,
		CreatedAt:    now,
		UpdatedAt:    now,
		Note:         strings.TrimSpace(*note),
	}
	store.Sessions[value] = entry
	if err := saveGooglePlacesSessionStore(store); err != nil {
		fatal(err)
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(entry); err != nil {
			fatal(err)
		}
		return
	}
	successf("google places session created: %s", value)
}

func cmdGooglePlacesSessionInspect(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("google places session inspect", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si google places session inspect <token> [--json]")
		return
	}
	token := strings.TrimSpace(fs.Arg(0))
	store, err := loadGooglePlacesSessionStore()
	if err != nil {
		fatal(err)
	}
	entry, ok := store.Sessions[token]
	if !ok {
		fatal(fmt.Errorf("session token not found: %s", token))
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(entry); err != nil {
			fatal(err)
		}
		return
	}
	status := "active"
	if strings.TrimSpace(entry.EndedAt) != "" {
		status = "ended"
	}
	fmt.Printf("%s %s\n", styleHeading("Session token:"), entry.Token)
	fmt.Printf("%s %s\n", styleHeading("Status:"), status)
	fmt.Printf("%s %s\n", styleHeading("Created:"), formatISODateWithGitHubRelativeNow(entry.CreatedAt))
	fmt.Printf("%s %s\n", styleHeading("Updated:"), formatISODateWithGitHubRelativeNow(entry.UpdatedAt))
	fmt.Printf("%s %s\n", styleHeading("Ended:"), orDash(formatISODateWithGitHubRelativeNow(entry.EndedAt)))
	fmt.Printf("%s %s\n", styleHeading("Account:"), orDash(entry.AccountAlias))
	fmt.Printf("%s %s\n", styleHeading("Note:"), orDash(entry.Note))
}

func cmdGooglePlacesSessionEnd(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("google places session end", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si google places session end <token> [--json]")
		return
	}
	token := strings.TrimSpace(fs.Arg(0))
	store, err := loadGooglePlacesSessionStore()
	if err != nil {
		fatal(err)
	}
	entry, ok := store.Sessions[token]
	if !ok {
		fatal(fmt.Errorf("session token not found: %s", token))
	}
	now := googlePlacesNowRFC3339()
	entry.UpdatedAt = now
	if strings.TrimSpace(entry.EndedAt) == "" {
		entry.EndedAt = now
	}
	store.Sessions[token] = entry
	if err := saveGooglePlacesSessionStore(store); err != nil {
		fatal(err)
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(entry); err != nil {
			fatal(err)
		}
		return
	}
	successf("google places session ended: %s", token)
}

func cmdGooglePlacesSessionList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("google places session list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google places session list [--json]")
		return
	}
	store, err := loadGooglePlacesSessionStore()
	if err != nil {
		fatal(err)
	}
	rows := make([]googlePlacesSessionEntry, 0, len(store.Sessions))
	for _, entry := range store.Sessions {
		rows = append(rows, entry)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt > rows[j].CreatedAt })
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rows); err != nil {
			fatal(err)
		}
		return
	}
	if len(rows) == 0 {
		infof("no google places sessions tracked")
		return
	}
	headers := []string{
		styleHeading("TOKEN"),
		styleHeading("STATUS"),
		styleHeading("ACCOUNT"),
		styleHeading("CREATED"),
	}
	tableRows := make([][]string, 0, len(rows))
	for _, item := range rows {
		status := "active"
		if strings.TrimSpace(item.EndedAt) != "" {
			status = "ended"
		}
		tableRows = append(tableRows, []string{
			item.Token,
			status,
			orDash(item.AccountAlias),
			formatISODateWithGitHubRelativeNow(item.CreatedAt),
		})
	}
	printAlignedTable(headers, tableRows, 2)
}
