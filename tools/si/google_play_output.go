package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"si/tools/si/internal/googleplaybridge"
)

func printGooglePlayResponse(resp googleplaybridge.Response, jsonOut bool, raw bool) {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(resp); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s (%d)\n", styleHeading("Google Play API:"), resp.Status, resp.StatusCode)
	if strings.TrimSpace(resp.RequestID) != "" {
		fmt.Printf("%s %s\n", styleHeading("Request ID:"), resp.RequestID)
	}
	if raw {
		body := strings.TrimSpace(resp.Body)
		if body == "" {
			body = "{}"
		}
		fmt.Println(body)
		return
	}
	if len(resp.Data) > 0 {
		printGooglePlayKeyValueMap(resp.Data)
		return
	}
	if len(resp.List) > 0 {
		fmt.Printf("%s %d\n", styleHeading("Items:"), len(resp.List))
		for _, item := range resp.List {
			fmt.Printf("  %s\n", summarizeGooglePlayItem(item))
		}
		return
	}
	if strings.TrimSpace(resp.Body) != "" {
		fmt.Println(strings.TrimSpace(resp.Body))
	}
}

func printGooglePlayError(err error) {
	if err == nil {
		return
	}
	var details *googleplaybridge.APIErrorDetails
	if !errors.As(err, &details) || details == nil {
		fatal(err)
		return
	}
	if strings.TrimSpace(err.Error()) != "" && strings.TrimSpace(err.Error()) != strings.TrimSpace(details.Error()) {
		fmt.Fprintln(os.Stderr, styleError(err.Error()))
	}
	fmt.Fprintln(os.Stderr, styleError("google play error"))
	if details.StatusCode > 0 {
		fmt.Fprintln(os.Stderr, styleError(fmt.Sprintf("  status_code: %d", details.StatusCode)))
	}
	if details.Code > 0 {
		fmt.Fprintln(os.Stderr, styleError(fmt.Sprintf("  code: %d", details.Code)))
	}
	if details.Status != "" {
		fmt.Fprintln(os.Stderr, styleError("  status: "+details.Status))
	}
	if details.Message != "" {
		fmt.Fprintln(os.Stderr, styleError("  message: "+details.Message))
	}
	if details.RequestID != "" {
		fmt.Fprintln(os.Stderr, styleError("  request_id: "+details.RequestID))
	}
	if len(details.Details) > 0 {
		fmt.Fprintln(os.Stderr, styleDim("details:"))
		for _, item := range details.Details {
			raw, _ := json.Marshal(item)
			fmt.Fprintln(os.Stderr, string(raw))
		}
	}
	if details.RawBody != "" {
		fmt.Fprintln(os.Stderr, styleDim("raw:"))
		fmt.Fprintln(os.Stderr, details.RawBody)
	}
	os.Exit(1)
}

func summarizeGooglePlayItem(item map[string]any) string {
	id := "-"
	for _, key := range []string{"id", "track", "language", "versionCode", "url", "packageName", "title"} {
		if value, ok := item[key]; ok {
			id = stringifyGooglePlayAny(value)
			if strings.TrimSpace(id) != "" && id != "-" {
				break
			}
		}
	}
	title := ""
	for _, key := range []string{"title", "name", "status", "shortDescription", "fullDescription", "language"} {
		if value, ok := item[key]; ok {
			title = stringifyGooglePlayAny(value)
			if strings.TrimSpace(title) != "" && title != "-" {
				break
			}
		}
	}
	if title == "" {
		title = "-"
	}
	return id + " " + title
}

func stringifyGooglePlayAny(value any) string {
	switch typed := value.(type) {
	case nil:
		return "-"
	case string:
		if strings.TrimSpace(typed) == "" {
			return "-"
		}
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case map[string]any:
		raw, _ := json.Marshal(typed)
		return string(raw)
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprintf("%v", typed)
		}
		return string(raw)
	}
}

func printGooglePlayKeyValueMap(data map[string]any) {
	if len(data) == 0 {
		fmt.Println("{}")
		return
	}
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([][2]string, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, [2]string{styleHeading(key + ":"), stringifyGooglePlayAny(data[key])})
	}
	printKeyValueTable(rows)
}
