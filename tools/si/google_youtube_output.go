package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"si/tools/si/internal/youtubebridge"
)

func printGoogleYouTubeResponse(resp youtubebridge.Response, jsonOut bool, raw bool) {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(resp); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s (%d)\n", styleHeading("YouTube API:"), resp.Status, resp.StatusCode)
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
		printGoogleYouTubeKeyValueMap(resp.Data)
		return
	}
	if len(resp.List) > 0 {
		fmt.Printf("%s %d\n", styleHeading("Items:"), len(resp.List))
		for _, item := range resp.List {
			fmt.Printf("  %s\n", summarizeGoogleYouTubeItem(item))
		}
		return
	}
	if strings.TrimSpace(resp.Body) != "" {
		fmt.Println(strings.TrimSpace(resp.Body))
	}
}

func printGoogleYouTubeError(err error) {
	if err == nil {
		return
	}
	var details *youtubebridge.APIErrorDetails
	if !errors.As(err, &details) || details == nil {
		fatal(err)
		return
	}
	if strings.TrimSpace(err.Error()) != "" && strings.TrimSpace(err.Error()) != strings.TrimSpace(details.Error()) {
		fmt.Fprintln(os.Stderr, styleError(err.Error()))
	}
	fmt.Fprintln(os.Stderr, styleError("google youtube error"))
	if details.StatusCode > 0 {
		fmt.Fprintln(os.Stderr, styleError(fmt.Sprintf("  status_code: %d", details.StatusCode)))
	}
	if details.Code > 0 {
		fmt.Fprintln(os.Stderr, styleError(fmt.Sprintf("  code: %d", details.Code)))
	}
	if details.Status != "" {
		fmt.Fprintln(os.Stderr, styleError("  status: "+details.Status))
	}
	if details.Reason != "" {
		fmt.Fprintln(os.Stderr, styleError("  reason: "+details.Reason))
	}
	if details.Message != "" {
		fmt.Fprintln(os.Stderr, styleError("  message: "+details.Message))
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

func stringifyGoogleYouTubeAny(value any) string {
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
		for _, key := range []string{"title", "text", "videoId", "channelId", "playlistId"} {
			if value, ok := typed[key].(string); ok && strings.TrimSpace(value) != "" {
				return value
			}
		}
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

func summarizeGoogleYouTubeItem(item map[string]any) string {
	id := "-"
	for _, key := range []string{"id", "videoId", "channelId", "playlistId"} {
		if value, ok := item[key]; ok {
			id = stringifyGoogleYouTubeAny(value)
			if strings.TrimSpace(id) != "" && id != "-" {
				break
			}
		}
	}
	if id == "-" {
		if ident, ok := item["id"].(map[string]any); ok {
			for _, key := range []string{"videoId", "channelId", "playlistId", "kind"} {
				if value, ok := ident[key]; ok {
					id = stringifyGoogleYouTubeAny(value)
					if strings.TrimSpace(id) != "" && id != "-" {
						break
					}
				}
			}
		}
	}
	label := ""
	if snippet, ok := item["snippet"].(map[string]any); ok {
		for _, key := range []string{"title", "channelTitle", "description"} {
			if value, ok := snippet[key]; ok {
				label = stringifyGoogleYouTubeAny(value)
				if strings.TrimSpace(label) != "" && label != "-" {
					break
				}
			}
		}
	}
	if label == "" || label == "-" {
		for _, key := range []string{"title", "kind", "etag"} {
			if value, ok := item[key]; ok {
				label = stringifyGoogleYouTubeAny(value)
				if strings.TrimSpace(label) != "" && label != "-" {
					break
				}
			}
		}
	}
	if label == "" {
		label = "-"
	}
	return id + " " + label
}

func printGoogleYouTubeKeyValueMap(data map[string]any) {
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
		rows = append(rows, [2]string{styleHeading(key + ":"), stringifyGoogleYouTubeAny(data[key])})
	}
	printKeyValueTable(rows)
}

func previewGoogleYouTubeSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "-"
	}
	secret = youtubebridge.RedactSensitive(secret)
	if len(secret) <= 10 {
		return secret
	}
	return secret[:8] + "..."
}
