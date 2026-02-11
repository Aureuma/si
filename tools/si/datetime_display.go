package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

func parseISODateTime(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, true
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, true
	}
	if parsed, err := time.ParseInLocation("2006-01-02", value, time.Local); err == nil {
		return parsed, true
	}
	return time.Time{}, false
}

func formatCalendarDate(t time.Time, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	loc := time.Local
	if now.Location() != nil {
		loc = now.Location()
	}
	localT := t.In(loc)
	localNow := now.In(loc)
	if localT.Year() == localNow.Year() {
		return localT.Format("Jan 2")
	}
	return localT.Format("Jan 2, 2006")
}

func formatGitHubRelativeTime(target time.Time, now time.Time) string {
	if target.IsZero() {
		return ""
	}
	loc := time.Local
	if now.Location() != nil {
		loc = now.Location()
	}
	target = target.In(loc)
	now = now.In(loc)
	diff := target.Sub(now)
	if diff >= 0 {
		if diff < time.Minute {
			return "in less than a minute"
		}
		minutes := int(math.Ceil(diff.Minutes()))
		if minutes < 60 {
			return "in " + pluralizeDurationUnit(minutes, "minute")
		}
		hours := int(math.Ceil(diff.Hours()))
		if hours < 24 {
			return "in " + pluralizeDurationUnit(hours, "hour")
		}
		days := int(math.Ceil(diff.Hours() / 24.0))
		return "in " + pluralizeDurationUnit(days, "day")
	}
	past := -diff
	if past < time.Minute {
		return "just now"
	}
	minutes := int(math.Floor(past.Minutes()))
	if minutes < 1 {
		minutes = 1
	}
	if minutes < 60 {
		return pluralizeDurationUnit(minutes, "minute") + " ago"
	}
	hours := int(math.Floor(past.Hours()))
	if hours < 1 {
		hours = 1
	}
	if hours < 24 {
		return pluralizeDurationUnit(hours, "hour") + " ago"
	}
	days := int(math.Floor(past.Hours() / 24.0))
	if days < 1 {
		days = 1
	}
	return pluralizeDurationUnit(days, "day") + " ago"
}

func formatDateWithGitHubRelative(t time.Time, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	abs := formatCalendarDate(t, now)
	rel := formatGitHubRelativeTime(t, now)
	if rel == "" {
		return abs
	}
	return fmt.Sprintf("%s, %s", abs, rel)
}

func formatDateWithGitHubRelativeNow(t time.Time) string {
	return formatDateWithGitHubRelative(t, time.Now())
}

func formatISODateWithGitHubRelative(raw string, now time.Time) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "-"
	}
	parsed, ok := parseISODateTime(value)
	if !ok {
		return value
	}
	return formatDateWithGitHubRelative(parsed, now)
}

func formatISODateWithGitHubRelativeNow(raw string) string {
	return formatISODateWithGitHubRelative(raw, time.Now())
}

func pluralizeDurationUnit(value int, unit string) string {
	if value == 1 {
		return fmt.Sprintf("%d %s", value, unit)
	}
	return fmt.Sprintf("%d %ss", value, unit)
}
