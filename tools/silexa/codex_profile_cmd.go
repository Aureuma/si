package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
)

func cmdProfile(args []string) {
fs := flag.NewFlagSet("profile", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)

	switch fs.NArg() {
	case 0:
		listCodexProfiles(*jsonOut)
	case 1:
		showCodexProfile(fs.Arg(0), *jsonOut)
	default:
		printUsage("usage: si profile [name] [--json]")
	}
}

func listCodexProfiles(jsonOut bool) {
	items := codexProfileSummaries()
	if len(items) == 0 {
		infof("no profiles configured")
		return
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(items); err != nil {
			fatal(err)
		}
		return
	}

	printCodexProfilesTable(items)
}

func showCodexProfile(key string, jsonOut bool) {
	profile, err := requireCodexProfile(key)
	if err != nil {
		fatal(err)
	}
	status := codexProfileAuthStatus(profile)
	item := codexProfileSummary{
		ID:         profile.ID,
		Name:       profile.Name,
		Email:      profile.Email,
		AuthCached: status.Exists,
		AuthPath:   status.Path,
	}
	if status.Exists {
		item.AuthUpdated = status.Modified.UTC().Format(time.RFC3339)
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(item); err != nil {
			fatal(err)
		}
		return
	}

	fmt.Printf("%s %s\n", styleHeading("Profile:"), profile.Name)
	fmt.Printf("%s %s\n", styleHeading("ID:"), profile.ID)
	fmt.Printf("%s %s\n", styleHeading("Email:"), profile.Email)
	if status.Path != "" {
		fmt.Printf("%s %s\n", styleHeading("Auth path:"), status.Path)
	}
	if status.Exists {
		fmt.Printf("%s %s\n", styleHeading("Auth cached:"), "yes")
		fmt.Printf("%s %s\n", styleHeading("Auth updated:"), status.Modified.UTC().Format(time.RFC3339))
	} else {
		fmt.Printf("%s %s\n", styleHeading("Auth cached:"), "no")
	}
}

func printCodexProfilesTable(items []codexProfileSummary) {
	if len(items) == 0 {
		return
	}
	widthID := displayWidth("PROFILE")
	widthName := displayWidth("NAME")
	widthEmail := displayWidth("EMAIL")
	for _, item := range items {
		if w := displayWidth(item.ID); w > widthID {
			widthID = w
		}
		if w := displayWidth(item.Name); w > widthName {
			widthName = w
		}
		if w := displayWidth(item.Email); w > widthEmail {
			widthEmail = w
		}
	}

	fmt.Printf("%s %s %s %s\n",
		padRightANSI(styleHeading("PROFILE"), widthID),
		padRightANSI(styleHeading("NAME"), widthName),
		padRightANSI(styleHeading("EMAIL"), widthEmail),
		padRightANSI(styleHeading("AUTH"), 8),
	)
	for _, item := range items {
		auth := "Missing"
		if item.AuthCached {
			auth = "Logged-In"
		}
		fmt.Printf("%s %s %s %s\n",
			padRightANSI(item.ID, widthID),
			padRightANSI(item.Name, widthName),
			padRightANSI(item.Email, widthEmail),
			padRightANSI(auth, 8),
		)
	}
}
