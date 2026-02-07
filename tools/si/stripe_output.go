package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"si/tools/si/internal/stripebridge"
)

func printStripeResponse(resp stripebridge.Response, jsonOut bool, raw bool) {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(resp); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s (%d)\n", styleHeading("Stripe API:"), resp.Status, resp.StatusCode)
	if strings.TrimSpace(resp.RequestID) != "" {
		fmt.Printf("%s %s\n", styleHeading("Request ID:"), resp.RequestID)
	}
	if strings.TrimSpace(resp.IdempotencyKey) != "" {
		fmt.Printf("%s %s\n", styleHeading("Idempotency Key:"), resp.IdempotencyKey)
	}
	if raw {
		body := strings.TrimSpace(resp.Body)
		if body == "" {
			body = "{}"
		}
		fmt.Println(body)
		return
	}
	if len(resp.Data) == 0 {
		fmt.Println(strings.TrimSpace(resp.Body))
		return
	}
	printKeyValueMap(resp.Data)
}

func printStripeError(err error) {
	if err == nil {
		return
	}
	var details *stripebridge.APIErrorDetails
	if !errors.As(err, &details) || details == nil {
		fatal(err)
		return
	}
	if strings.TrimSpace(err.Error()) != "" && strings.TrimSpace(err.Error()) != strings.TrimSpace(details.Error()) {
		fmt.Fprintln(os.Stderr, styleError(err.Error()))
	}
	fmt.Fprintln(os.Stderr, styleError("stripe error"))
	if details.StatusCode > 0 {
		fmt.Fprintln(os.Stderr, styleError(fmt.Sprintf("  status: %d", details.StatusCode)))
	}
	if details.Type != "" {
		fmt.Fprintln(os.Stderr, styleError("  type: "+details.Type))
	}
	if details.Code != "" {
		fmt.Fprintln(os.Stderr, styleError("  code: "+details.Code))
	}
	if details.DeclineCode != "" {
		fmt.Fprintln(os.Stderr, styleError("  decline_code: "+details.DeclineCode))
	}
	if details.Param != "" {
		fmt.Fprintln(os.Stderr, styleError("  param: "+details.Param))
	}
	if details.Message != "" {
		fmt.Fprintln(os.Stderr, styleError("  message: "+details.Message))
	}
	if details.RequestID != "" {
		fmt.Fprintln(os.Stderr, styleError("  request_id: "+details.RequestID))
	}
	if details.DocURL != "" {
		fmt.Fprintln(os.Stderr, styleError("  doc_url: "+details.DocURL))
	}
	if details.RequestLogURL != "" {
		fmt.Fprintln(os.Stderr, styleError("  request_log_url: "+details.RequestLogURL))
	}
	if details.RawBody != "" {
		fmt.Fprintln(os.Stderr, styleDim("raw:"))
		fmt.Fprintln(os.Stderr, details.RawBody)
	}
	os.Exit(1)
}

func printKeyValueMap(data map[string]any) {
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
		fmt.Printf("%s %s\n", padRightANSI(styleHeading(key+":"), maxWidth+1), stringifyAny(data[key]))
	}
}

func stringifyAny(value any) string {
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
