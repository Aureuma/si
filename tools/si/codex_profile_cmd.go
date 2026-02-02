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
	withStatus := fs.Bool("status", false, "fetch usage limits for logged-in profiles")
	_ = fs.Parse(args)

	switch fs.NArg() {
	case 0:
		listCodexProfiles(*jsonOut, *withStatus)
	case 1:
		showCodexProfile(fs.Arg(0), *jsonOut, *withStatus)
	default:
		printUsage("usage: si profile [name] [--json] [--status]")
	}
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
				if res.Err != nil {
					items[i].StatusError = res.Err.Error()
					continue
				}
				items[i].FiveHourLeftPct = res.Status.FiveHourLeftPct
				items[i].FiveHourReset = res.Status.FiveHourReset
				items[i].WeeklyLeftPct = res.Status.WeeklyLeftPct
				items[i].WeeklyReset = res.Status.WeeklyReset
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
			if item.StatusError != "" {
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
		ID:              profile.ID,
		Name:            profile.Name,
		Email:           profile.Email,
		AuthCached:      status.Exists,
		AuthPath:        status.Path,
		FiveHourLeftPct: -1,
		WeeklyLeftPct:   -1,
	}
	if status.Exists {
		item.AuthUpdated = status.Modified.UTC().Format(time.RFC3339)
	}
	if withStatus && status.Exists {
		statuses := collectProfileStatuses([]codexProfileSummary{item})
		if res, ok := statuses[item.ID]; ok {
			if res.Err != nil {
				item.StatusError = res.Err.Error()
			} else {
				item.FiveHourLeftPct = res.Status.FiveHourLeftPct
				item.FiveHourReset = res.Status.FiveHourReset
				item.WeeklyLeftPct = res.Status.WeeklyLeftPct
				item.WeeklyReset = res.Status.WeeklyReset
			}
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
	if status.Exists {
		fmt.Printf("%s %s\n", styleHeading("Auth cached:"), "yes")
		fmt.Printf("%s %s\n", styleHeading("Auth updated:"), status.Modified.UTC().Format(time.RFC3339))
	} else {
		fmt.Printf("%s %s\n", styleHeading("Auth cached:"), "no")
	}
	if withStatus {
		if item.StatusError != "" {
			fmt.Printf("%s %s\n", styleHeading("Status error:"), item.StatusError)
		} else if status.Exists {
			fmt.Printf("%s %s\n", styleHeading("5h limit:"), formatLimitColumn(item.FiveHourLeftPct, item.FiveHourReset))
			fmt.Printf("%s %s\n", styleHeading("Weekly limit:"), formatLimitColumn(item.WeeklyLeftPct, item.WeeklyReset))
		} else {
			fmt.Printf("%s %s\n", styleHeading("Status:"), "unavailable (auth missing)")
		}
	}
}

func printCodexProfilesTable(items []codexProfileSummary, withStatus bool) {
	if len(items) == 0 {
		return
	}
	widthID := displayWidth("PROFILE")
	widthName := displayWidth("NAME")
	widthEmail := displayWidth("EMAIL")
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
		if withStatus {
			limit := formatLimitColumn(item.FiveHourLeftPct, item.FiveHourReset)
			if item.StatusError != "" {
				limit = "ERR"
			}
			if w := displayWidth(limit); w > width5h {
				width5h = w
			}
			limit = formatLimitColumn(item.WeeklyLeftPct, item.WeeklyReset)
			if item.StatusError != "" {
				limit = "ERR"
			}
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
			padRightANSI(styleHeading("AUTH"), 8),
			padRightANSI(styleHeading("5H"), width5h),
			padRightANSI(styleHeading("WEEKLY"), widthWeekly),
		)
	} else {
		fmt.Printf("%s %s %s %s\n",
			padRightANSI(styleHeading("PROFILE"), widthID),
			padRightANSI(styleHeading("NAME"), widthName),
			padRightANSI(styleHeading("EMAIL"), widthEmail),
			padRightANSI(styleHeading("AUTH"), 8),
		)
	}
	for _, item := range items {
		auth := "Missing"
		if item.AuthCached {
			auth = "Logged-In"
		}
		if withStatus {
			fiveHour := formatLimitColumn(item.FiveHourLeftPct, item.FiveHourReset)
			weekly := formatLimitColumn(item.WeeklyLeftPct, item.WeeklyReset)
			if item.StatusError != "" {
				fiveHour = "ERR"
				weekly = "ERR"
			}
			fmt.Printf("%s %s %s %s %s %s\n",
				padRightANSI(item.ID, widthID),
				padRightANSI(item.Name, widthName),
				padRightANSI(item.Email, widthEmail),
				padRightANSI(auth, 8),
				padRightANSI(fiveHour, width5h),
				padRightANSI(weekly, widthWeekly),
			)
		} else {
			fmt.Printf("%s %s %s %s\n",
				padRightANSI(item.ID, widthID),
				padRightANSI(item.Name, widthName),
				padRightANSI(item.Email, widthEmail),
				padRightANSI(auth, 8),
			)
		}
	}
}
