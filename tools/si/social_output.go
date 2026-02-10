package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

func printSocialResponse(resp socialResponse, jsonOut bool, raw bool) {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(resp); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s (%d)\n", styleHeading("Social API:"), resp.Status, resp.StatusCode)
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
		printSocialKeyValueMap(resp.Data)
		return
	}
	if len(resp.List) > 0 {
		fmt.Printf("%s %d\n", styleHeading("Items:"), len(resp.List))
		for _, item := range resp.List {
			fmt.Printf("  %s\n", socialSummarizeItem(item))
		}
		return
	}
	if strings.TrimSpace(resp.Body) != "" {
		fmt.Println(strings.TrimSpace(resp.Body))
	}
}

func printSocialError(err error) {
	if err == nil {
		return
	}
	var details *socialAPIErrorDetails
	if !errors.As(err, &details) || details == nil {
		fatal(err)
		return
	}
	if strings.TrimSpace(err.Error()) != "" && strings.TrimSpace(err.Error()) != strings.TrimSpace(details.Error()) {
		fmt.Fprintln(os.Stderr, styleError(err.Error()))
	}
	fmt.Fprintln(os.Stderr, styleError("social api error"))
	if details.Platform != "" {
		fmt.Fprintln(os.Stderr, styleError("  platform: "+socialPlatformLabel(details.Platform)))
	}
	if details.StatusCode > 0 {
		fmt.Fprintln(os.Stderr, styleError(fmt.Sprintf("  status_code: %d", details.StatusCode)))
	}
	if details.Code > 0 {
		fmt.Fprintln(os.Stderr, styleError(fmt.Sprintf("  code: %d", details.Code)))
	}
	if details.Status != "" {
		fmt.Fprintln(os.Stderr, styleError("  status: "+details.Status))
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
	if details.RawBody != "" {
		fmt.Fprintln(os.Stderr, styleDim("raw:"))
		fmt.Fprintln(os.Stderr, details.RawBody)
	}
	os.Exit(1)
}

func printSocialKeyValueMap(data map[string]any) {
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
		fmt.Printf("%s %s\n", padRightANSI(styleHeading(key+":"), maxWidth+1), stringifySocialAny(data[key]))
	}
}
