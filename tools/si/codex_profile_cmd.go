package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

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

	printCodexProfilesTable(items, withStatus, false)
	if withStatus {
		for _, msg := range profileStatusWarnings(items) {
			fmt.Printf("%s %s\n", styleWarn("warning:"), msg)
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
		fmt.Printf("%s %s\n", styleHeading("Auth updated:"), formatDateWithGitHubRelativeNow(status.Modified))
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

func printCodexProfilesTable(items []codexProfileSummary, withStatus bool, includeProfile bool) {
	if len(items) == 0 {
		return
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		name := profileNameForTable(item.Name)
		auth := profileAuthLabel(item)
		email := profileEmailForTable(item.Email)
		if withStatus {
			if includeProfile {
				rows = append(rows, []string{
					item.ID,
					name,
					email,
					auth,
					profileFiveHourDisplay(item),
					profileWeeklyDisplay(item),
				})
				continue
			}
			rows = append(rows, []string{
				name,
				email,
				auth,
				profileFiveHourDisplay(item),
				profileWeeklyDisplay(item),
			})
			continue
		}
		if includeProfile {
			rows = append(rows, []string{item.ID, name, email, auth})
			continue
		}
		rows = append(rows, []string{name, email, auth})
	}

	headers := []string{styleHeading("NAME"), styleHeading("EMAIL"), styleHeading("AUTH")}
	if includeProfile {
		headers = []string{
			styleHeading("PROFILE"),
			styleHeading("NAME"),
			styleHeading("EMAIL"),
			styleHeading("AUTH"),
		}
	}
	if withStatus {
		headers = append(headers, styleHeading("5H"), styleHeading("WEEKLY"))
	}
	printAlignedTable(headers, rows, 2)
}

func profileNameForTable(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	first, _ := utf8.DecodeRuneInString(name)
	if first == utf8.RuneError || unicode.IsLetter(first) || unicode.IsNumber(first) {
		return name
	}
	parts := strings.SplitN(name, " ", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return name
	}
	return parts[0] + " " + parts[1]
}

func profileEmailForTable(email string) string {
	email = strings.TrimSpace(email)
	at := strings.Index(email, "@")
	if at <= 0 || at >= len(email)-1 {
		return email
	}
	domain := email[at+1:]
	first, size := utf8.DecodeRuneInString(domain)
	if first == utf8.RuneError && size == 0 {
		return email
	}
	return email[:at+1] + string(first) + "…"
}

func profileAuthLabel(item codexProfileSummary) string {
	if item.AuthCached && !isProfileAuthError(item.StatusError) {
		return "Logged-In"
	}
	return "Missing"
}

func profileFiveHourDisplay(item codexProfileSummary) string {
	if !item.AuthCached || isProfileAuthError(item.StatusError) {
		return "-"
	}
	if strings.TrimSpace(item.StatusError) == "" {
		return formatLimitColumn(item.FiveHourLeftPct, item.FiveHourReset, item.FiveHourRemaining)
	}
	return "ERR"
}

func profileWeeklyDisplay(item codexProfileSummary) string {
	if !item.AuthCached || isProfileAuthError(item.StatusError) {
		return "-"
	}
	if strings.TrimSpace(item.StatusError) == "" {
		return formatLimitColumn(item.WeeklyLeftPct, item.WeeklyReset, item.WeeklyRemaining)
	}
	return "ERR"
}

func profileStatusWarnings(items []codexProfileSummary) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.StatusError) == "" {
			continue
		}
		// Auth errors are reflected as AUTH=Missing; avoid noisy warnings.
		if isProfileAuthError(item.StatusError) || !item.AuthCached {
			continue
		}
		out = append(out, fmt.Sprintf("profile %s status error: %s", item.ID, summarizeProfileStatusError(item.ID, item.StatusError)))
	}
	return out
}

func isProfileAuthError(raw string) bool {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return false
	}
	keywords := []string{
		"auth",
		"token",
		"unauthorized",
		"forbidden",
		"credential",
		"refresh",
		"login",
	}
	for _, key := range keywords {
		if strings.Contains(raw, key) {
			return true
		}
	}
	return false
}

func summarizeProfileStatusError(profileID string, raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return "unknown error"
	}
	if strings.Contains(msg, "auth cache not found") {
		return fmt.Sprintf("auth cache missing; run `si login %s`", strings.TrimSpace(profileID))
	}
	if strings.Contains(msg, "refresh_token_reused") {
		return fmt.Sprintf("token refresh failed (refresh token reused); run `si login %s`", strings.TrimSpace(profileID))
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "token expired") || strings.Contains(lower, "token_expired") || strings.Contains(lower, "token is expired") {
		return fmt.Sprintf("token expired; run `si login %s`", strings.TrimSpace(profileID))
	}
	const maxLen = 180
	if len(msg) > maxLen {
		return strings.TrimSpace(msg[:maxLen-1]) + "..."
	}
	return msg
}
