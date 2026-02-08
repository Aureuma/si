package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"si/tools/si/internal/cloudflarebridge"
)

func printCloudflareResponse(resp cloudflarebridge.Response, jsonOut bool, raw bool) {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(resp); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s (%d)\n", styleHeading("Cloudflare API:"), resp.Status, resp.StatusCode)
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
		printCloudflareKeyValueMap(resp.Data)
		return
	}
	if len(resp.List) > 0 {
		fmt.Printf("%s %d\n", styleHeading("Items:"), len(resp.List))
		for _, item := range resp.List {
			fmt.Printf("  %s\n", summarizeCloudflareItem(item))
		}
		return
	}
	if strings.TrimSpace(resp.Body) != "" {
		fmt.Println(strings.TrimSpace(resp.Body))
	}
}

func printCloudflareError(err error) {
	if err == nil {
		return
	}
	var details *cloudflarebridge.APIErrorDetails
	if !errors.As(err, &details) || details == nil {
		fatal(err)
		return
	}
	if strings.TrimSpace(err.Error()) != "" && strings.TrimSpace(err.Error()) != strings.TrimSpace(details.Error()) {
		fmt.Fprintln(os.Stderr, styleError(err.Error()))
	}
	fmt.Fprintln(os.Stderr, styleError("cloudflare error"))
	if details.StatusCode > 0 {
		fmt.Fprintln(os.Stderr, styleError(fmt.Sprintf("  status: %d", details.StatusCode)))
	}
	if details.Code > 0 {
		fmt.Fprintln(os.Stderr, styleError(fmt.Sprintf("  code: %d", details.Code)))
	}
	if details.Type != "" {
		fmt.Fprintln(os.Stderr, styleError("  type: "+details.Type))
	}
	if details.Message != "" {
		fmt.Fprintln(os.Stderr, styleError("  message: "+details.Message))
	}
	if details.RequestID != "" {
		fmt.Fprintln(os.Stderr, styleError("  request_id: "+details.RequestID))
	}
	if details.DocumentationURL != "" {
		fmt.Fprintln(os.Stderr, styleError("  documentation_url: "+details.DocumentationURL))
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

func summarizeCloudflareItem(item map[string]any) string {
	id := "-"
	for _, key := range []string{"id", "zone_id", "name", "tag"} {
		if value, ok := item[key]; ok {
			id = stringifyCloudflareAny(value)
			if strings.TrimSpace(id) != "" && id != "-" {
				break
			}
		}
	}
	title := ""
	for _, key := range []string{"name", "hostname", "email", "status", "content", "value", "type"} {
		if value, ok := item[key]; ok {
			title = stringifyCloudflareAny(value)
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

func stringifyCloudflareAny(value any) string {
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
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprintf("%v", typed)
		}
		return string(raw)
	}
}

func printCloudflareKeyValueMap(data map[string]any) {
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
		fmt.Printf("%s %s\n", padRightANSI(styleHeading(key+":"), maxWidth+1), stringifyCloudflareAny(data[key]))
	}
}
