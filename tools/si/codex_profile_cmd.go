package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func cmdProfile(args []string) {
	fs := flag.NewFlagSet("profile", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	noStatus := fs.Bool("no-status", false, "disable usage status lookup and limit columns")
	nameArg, filtered := splitProfileNameAndFlags(args)
	_ = fs.Parse(filtered)
	withStatus := !*noStatus
	nameArg = strings.TrimSpace(nameArg)
	rest := fs.Args()
	if nameArg == "" && len(rest) > 0 {
		nameArg = strings.TrimSpace(rest[0])
		rest = rest[1:]
	}
	if len(rest) > 0 {
		printUsage("usage: si profile [name] [--json] [--no-status]")
		return
	}

	switch {
	case nameArg == "":
		listCodexProfiles(*jsonOut, withStatus)
	case nameArg != "":
		showCodexProfile(nameArg, *jsonOut, withStatus)
	}
}

func splitProfileNameAndFlags(args []string) (string, []string) {
	return splitNameAndFlags(args, map[string]bool{
		"json":      true,
		"no-status": true,
	})
}

func listCodexProfiles(jsonOut bool, withStatus bool) {
	items := codexProfileSummaries()
	if len(items) == 0 {
		infof("no profiles configured")
		return
	}
	if withStatus {
		statuses := collectProfileStatuses(items)
		for i := range items {
			if res, ok := statuses[items[i].ID]; ok {
				applyProfileStatusResult(&items[i], res)
			}
		}
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(items); err != nil {
			fatal(err)
		}
		return
	}

	printCodexProfilesTable(items, withStatus)
	if withStatus {
		for _, item := range items {
			if item.StatusError != "" && item.AuthCached {
				warnf("profile %s status error: %s", item.ID, item.StatusError)
			}
		}
	}
}

func showCodexProfile(key string, jsonOut bool, withStatus bool) {
	profile, err := requireCodexProfile(key)
	if err != nil {
		fatal(err)
	}
	status := codexProfileAuthStatus(profile)
	item := codexProfileSummary{
		ID:                profile.ID,
		Name:              profile.Name,
		Email:             profile.Email,
		AuthCached:        status.Exists,
		AuthPath:          status.Path,
		FiveHourLeftPct:   -1,
		FiveHourRemaining: -1,
		WeeklyLeftPct:     -1,
		WeeklyRemaining:   -1,
	}
	if status.Exists {
		item.AuthUpdated = status.Modified.UTC().Format(time.RFC3339)
	}
	if withStatus && status.Exists {
		statuses := collectProfileStatuses([]codexProfileSummary{item})
		if res, ok := statuses[item.ID]; ok {
			applyProfileStatusResult(&item, res)
		}
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
	if item.AuthCached {
		fmt.Printf("%s %s\n", styleHeading("Auth cached:"), "yes")
		fmt.Printf("%s %s\n", styleHeading("Auth updated:"), status.Modified.UTC().Format(time.RFC3339))
	} else {
		fmt.Printf("%s %s\n", styleHeading("Auth cached:"), "no")
	}
	if withStatus {
		if item.StatusError != "" {
			fmt.Printf("%s %s\n", styleHeading("Status error:"), item.StatusError)
		} else if status.Exists {
			fmt.Printf("%s %s\n", styleHeading("5h limit:"), formatLimitColumn(item.FiveHourLeftPct, item.FiveHourReset, item.FiveHourRemaining))
			fmt.Printf("%s %s\n", styleHeading("Weekly limit:"), formatLimitColumn(item.WeeklyLeftPct, item.WeeklyReset, item.WeeklyRemaining))
		} else {
			fmt.Printf("%s %s\n", styleHeading("Status:"), "unavailable (auth missing)")
		}
	}
}

func applyProfileStatusResult(item *codexProfileSummary, res profileStatusResult) {
	if item == nil {
		return
	}
	if res.Err != nil {
		item.StatusError = res.Err.Error()
		return
	}
	item.FiveHourLeftPct = res.Status.FiveHourLeftPct
	item.FiveHourReset = res.Status.FiveHourReset
	item.FiveHourRemaining = res.Status.FiveHourRemaining
	item.WeeklyLeftPct = res.Status.WeeklyLeftPct
	item.WeeklyReset = res.Status.WeeklyReset
	item.WeeklyRemaining = res.Status.WeeklyRemaining
}

func printCodexProfilesTable(items []codexProfileSummary, withStatus bool) {
	if len(items) == 0 {
		return
	}
	widthID := displayWidth("PROFILE")
	widthName := displayWidth("NAME")
	widthEmail := displayWidth("EMAIL")
	widthAuth := displayWidth("AUTH")
	width5h := displayWidth("5H")
	widthWeekly := displayWidth("WEEKLY")
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
		auth := profileAuthLabel(item)
		if w := displayWidth(auth); w > widthAuth {
			widthAuth = w
		}
		if withStatus {
			limit := profileFiveHourDisplay(item)
			if w := displayWidth(limit); w > width5h {
				width5h = w
			}
			limit = profileWeeklyDisplay(item)
			if w := displayWidth(limit); w > widthWeekly {
				widthWeekly = w
			}
		}
	}

	if withStatus {
		fmt.Printf("%s %s %s %s %s %s\n",
			padRightANSI(styleHeading("PROFILE"), widthID),
			padRightANSI(styleHeading("NAME"), widthName),
			padRightANSI(styleHeading("EMAIL"), widthEmail),
			padRightANSI(styleHeading("AUTH"), widthAuth),
			padRightANSI(styleHeading("5H"), width5h),
			padRightANSI(styleHeading("WEEKLY"), widthWeekly),
		)
	} else {
		fmt.Printf("%s %s %s %s\n",
			padRightANSI(styleHeading("PROFILE"), widthID),
			padRightANSI(styleHeading("NAME"), widthName),
			padRightANSI(styleHeading("EMAIL"), widthEmail),
			padRightANSI(styleHeading("AUTH"), widthAuth),
		)
	}
	for _, item := range items {
		auth := profileAuthLabel(item)
		if withStatus {
			fiveHour := profileFiveHourDisplay(item)
			weekly := profileWeeklyDisplay(item)
			fmt.Printf("%s %s %s %s %s %s\n",
				padRightANSI(item.ID, widthID),
				padRightANSI(item.Name, widthName),
				padRightANSI(item.Email, widthEmail),
				padRightANSI(auth, widthAuth),
				padRightANSI(fiveHour, width5h),
				padRightANSI(weekly, widthWeekly),
			)
		} else {
			fmt.Printf("%s %s %s %s\n",
				padRightANSI(item.ID, widthID),
				padRightANSI(item.Name, widthName),
				padRightANSI(item.Email, widthEmail),
				padRightANSI(auth, widthAuth),
			)
		}
	}
}

func profileAuthLabel(item codexProfileSummary) string {
	if item.AuthCached {
		return "Logged-In"
	}
	if strings.TrimSpace(item.StatusError) != "" {
		return "Error"
	}
	return "Missing"
}

func profileFiveHourDisplay(item codexProfileSummary) string {
	if strings.TrimSpace(item.StatusError) == "" {
		return formatLimitColumn(item.FiveHourLeftPct, item.FiveHourReset, item.FiveHourRemaining)
	}
	if !item.AuthCached {
		return "AUTH: " + strings.TrimSpace(item.StatusError)
	}
	return "ERR"
}

func profileWeeklyDisplay(item codexProfileSummary) string {
	if strings.TrimSpace(item.StatusError) == "" {
		return formatLimitColumn(item.WeeklyLeftPct, item.WeeklyReset, item.WeeklyRemaining)
	}
	if !item.AuthCached {
		return "-"
	}
	return "ERR"
}
