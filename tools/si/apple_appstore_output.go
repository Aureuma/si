package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"si/tools/si/internal/appstorebridge"
)

func printAppleAppStoreResponse(resp appstorebridge.Response, jsonOut bool, raw bool) {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(resp); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s (%d)\n", styleHeading("Apple App Store API:"), resp.Status, resp.StatusCode)
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
		printAppleAppStoreKeyValueMap(resp.Data)
		if len(resp.List) > 0 {
			fmt.Printf("%s %d\n", styleHeading("Items:"), len(resp.List))
		}
		return
	}
	if len(resp.List) > 0 {
		fmt.Printf("%s %d\n", styleHeading("Items:"), len(resp.List))
		for _, item := range resp.List {
			fmt.Printf("  %s\n", summarizeAppleAppStoreItem(item))
		}
		return
	}
	if strings.TrimSpace(resp.Body) != "" {
		fmt.Println(strings.TrimSpace(resp.Body))
	}
}

func printAppleAppStoreError(err error) {
	if err == nil {
		return
	}
	var details *appstorebridge.APIErrorDetails
	if !errors.As(err, &details) || details == nil {
		fatal(err)
		return
	}
	if strings.TrimSpace(err.Error()) != "" && strings.TrimSpace(err.Error()) != strings.TrimSpace(details.Error()) {
		fmt.Fprintln(os.Stderr, styleError(err.Error()))
	}
	fmt.Fprintln(os.Stderr, styleError("apple appstore error"))
	if details.StatusCode > 0 {
		fmt.Fprintln(os.Stderr, styleError(fmt.Sprintf("  status_code: %d", details.StatusCode)))
	}
	if details.Code != "" {
		fmt.Fprintln(os.Stderr, styleError("  code: "+details.Code))
	}
	if details.Title != "" {
		fmt.Fprintln(os.Stderr, styleError("  title: "+details.Title))
	}
	if details.Detail != "" {
		fmt.Fprintln(os.Stderr, styleError("  detail: "+details.Detail))
	}
	if details.RequestID != "" {
		fmt.Fprintln(os.Stderr, styleError("  request_id: "+details.RequestID))
	}
	if len(details.Errors) > 0 {
		fmt.Fprintln(os.Stderr, styleDim("errors:"))
		for _, item := range details.Errors {
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

func summarizeAppleAppStoreItem(item map[string]any) string {
	if len(item) == 0 {
		return "-"
	}
	id := strings.TrimSpace(parseAppleAnyString(item["id"]))
	if id == "" {
		id = "-"
	}
	itemType := strings.TrimSpace(parseAppleAnyString(item["type"]))
	attrs, _ := item["attributes"].(map[string]any)
	title := ""
	for _, key := range []string{"name", "bundleId", "locale", "versionString", "subtitle", "appVersionState"} {
		if attrs != nil {
			if value := strings.TrimSpace(parseAppleAnyString(attrs[key])); value != "" {
				title = value
				break
			}
		}
	}
	if title == "" {
		title = "-"
	}
	if itemType == "" {
		return id + " " + title
	}
	return id + " " + itemType + " " + title
}

func printAppleAppStoreKeyValueMap(data map[string]any) {
	if len(data) == 0 {
		fmt.Println("{}")
		return
	}
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	maxWidth := 0
	for _, key := range keys {
		if len(key) > maxWidth {
			maxWidth = len(key)
		}
	}
	for _, key := range keys {
		value := parseAppleAnyString(data[key])
		if value == "" {
			raw, _ := json.Marshal(data[key])
			value = string(raw)
		}
		fmt.Printf("%s %s\n", padRightANSI(styleHeading(key+":"), maxWidth+1), value)
	}
}
