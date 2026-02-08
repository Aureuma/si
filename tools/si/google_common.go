package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func parseGoogleParams(values []string) map[string]string {
	out := map[string]string{}
	for _, entry := range values {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(parts[1])
	}
	return out
}

func parseGoogleLatLng(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid lat,lng %q", raw)
	}
	lat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return nil, fmt.Errorf("invalid latitude %q", parts[0])
	}
	lng, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return nil, fmt.Errorf("invalid longitude %q", parts[1])
	}
	return map[string]any{"latitude": lat, "longitude": lng}, nil
}

func parseGoogleJSONMap(raw string, fieldName string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("invalid %s json: %w", fieldName, err)
	}
	return decoded, nil
}

func parseGoogleCSVList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func resolveGooglePlaceResourcePath(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("place id or place name is required")
	}
	if strings.HasPrefix(value, "/v1/") {
		return value, nil
	}
	if strings.HasPrefix(value, "places/") {
		return "/v1/" + value, nil
	}
	if strings.HasPrefix(value, "/places/") {
		return "/v1" + value, nil
	}
	return "/v1/places/" + value, nil
}

func resolveGooglePhotoMediaPath(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("photo name is required")
	}
	if strings.HasPrefix(value, "/v1/") && strings.Contains(value, "/photos/") {
		if strings.HasSuffix(value, "/media") {
			return value, nil
		}
		return value + "/media", nil
	}
	if strings.HasPrefix(value, "places/") && strings.Contains(value, "/photos/") {
		if strings.HasSuffix(value, "/media") {
			return "/v1/" + value, nil
		}
		return "/v1/" + value + "/media", nil
	}
	if strings.HasPrefix(value, "/places/") && strings.Contains(value, "/photos/") {
		if strings.HasSuffix(value, "/media") {
			return "/v1" + value, nil
		}
		return "/v1" + value + "/media", nil
	}
	return "", fmt.Errorf("photo name must look like places/<place_id>/photos/<photo_id>")
}

func resolveGoogleFieldMask(operation string, mask string, preset string, required bool, allowWildcard bool, jsonOut bool) string {
	resolved, err := resolveGooglePlacesFieldMask(fieldMaskInput{
		Operation:          operation,
		Mask:               mask,
		Preset:             preset,
		Required:           required,
		AllowWildcard:      allowWildcard,
		NonInteractiveFail: true,
	})
	if err != nil {
		fatal(err)
	}
	if jsonOut {
		return resolved
	}
	hint := googlePlacesFieldMaskCostHint(resolved)
	if strings.TrimSpace(resolved) != "" {
		fmt.Printf("%s %s\n", styleHeading("Field mask:"), resolved)
	}
	if hint != "" {
		var styled string
		switch hint {
		case "low":
			styled = styleSuccess(hint)
		case "medium":
			styled = styleWarn(hint)
		default:
			styled = styleError(hint)
		}
		fmt.Printf("%s %s\n", styleHeading("Cost hint:"), styled)
	}
	return resolved
}

func parseGoogleReportWindow(sinceRaw string, untilRaw string) (*time.Time, *time.Time, error) {
	since, err := parseReportTime(sinceRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid --since: %w", err)
	}
	until, err := parseReportTime(untilRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid --until: %w", err)
	}
	if since != nil && until != nil && since.After(*until) {
		return nil, nil, fmt.Errorf("--since must be <= --until")
	}
	return since, until, nil
}

func inGoogleTimeRange(ts time.Time, since *time.Time, until *time.Time) bool {
	if ts.IsZero() {
		return false
	}
	if since != nil && ts.Before(*since) {
		return false
	}
	if until != nil && ts.After(*until) {
		return false
	}
	return true
}

func parseGoogleRFC3339(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func parseGooglePathRef(path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return "-"
	}
	value = strings.TrimPrefix(value, "https://places.googleapis.com")
	value = strings.TrimPrefix(value, "http://places.googleapis.com")
	if value == "" {
		return "/"
	}
	return value
}
