package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

type fieldMaskInput struct {
	Operation          string
	Mask               string
	Preset             string
	Required           bool
	AllowWildcard      bool
	NonInteractiveFail bool
}

func resolveGooglePlacesFieldMask(input fieldMaskInput) (string, error) {
	mask := strings.TrimSpace(input.Mask)
	preset := strings.TrimSpace(strings.ToLower(input.Preset))
	operation := strings.TrimSpace(strings.ToLower(input.Operation))
	if mask == "" && preset != "" {
		value, ok := googlePlacesFieldMaskPresets(operation)[preset]
		if !ok {
			return "", fmt.Errorf("unknown field mask preset %q for operation %q", preset, operation)
		}
		mask = value
	}
	if mask == "" {
		if defaultPreset := defaultGooglePlacesFieldMaskPreset(operation); defaultPreset != "" {
			mask = defaultPreset
		}
	}
	mask = normalizeFieldMask(mask)
	if input.Required && mask == "" {
		return "", fmt.Errorf("field mask is required for %s (use --field-mask or --field-preset)", operation)
	}
	if strings.Contains(mask, "*") {
		if input.AllowWildcard {
			return mask, nil
		}
		if input.NonInteractiveFail || !term.IsTerminal(int(os.Stdout.Fd())) || !term.IsTerminal(int(os.Stdin.Fd())) {
			return "", fmt.Errorf("wildcard field mask is blocked for %s; use --allow-wildcard-mask to override", operation)
		}
		return "", fmt.Errorf("wildcard field mask requires --allow-wildcard-mask")
	}
	return mask, nil
}

func normalizeFieldMask(mask string) string {
	if strings.TrimSpace(mask) == "" {
		return ""
	}
	parts := strings.Split(mask, ",")
	clean := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		clean = append(clean, part)
	}
	return strings.Join(clean, ",")
}

func defaultGooglePlacesFieldMaskPreset(operation string) string {
	operation = strings.ToLower(strings.TrimSpace(operation))
	switch operation {
	case "autocomplete":
		return googlePlacesFieldMaskPresets(operation)["autocomplete-basic"]
	case "search-text", "search-nearby":
		return googlePlacesFieldMaskPresets(operation)["search-basic"]
	case "details":
		return googlePlacesFieldMaskPresets(operation)["details-basic"]
	default:
		return ""
	}
}

func googlePlacesFieldMaskPresets(operation string) map[string]string {
	searchBasic := "places.id,places.name,places.displayName,places.formattedAddress"
	searchDiscovery := "places.id,places.name,places.displayName,places.formattedAddress,places.primaryType,places.location,places.googleMapsUri"
	searchContact := "places.id,places.name,places.displayName,places.formattedAddress,places.internationalPhoneNumber,places.websiteUri,places.regularOpeningHours"
	searchRating := "places.id,places.name,places.displayName,places.formattedAddress,places.rating,places.userRatingCount"
	detailsBasic := "id,name,displayName,formattedAddress,googleMapsUri,location"
	detailsDiscovery := "id,name,displayName,formattedAddress,googleMapsUri,location,primaryType,types,viewport"
	detailsContact := "id,name,displayName,formattedAddress,internationalPhoneNumber,websiteUri,regularOpeningHours"
	detailsRating := "id,name,displayName,formattedAddress,rating,userRatingCount,reviews"
	autocompleteBasic := "suggestions.placePrediction.placeId,suggestions.placePrediction.text.text,suggestions.queryPrediction.text.text"

	presets := map[string]string{
		"autocomplete-basic": autocompleteBasic,
		"search-basic":       searchBasic,
		"search-discovery":   searchDiscovery,
		"search-contact":     searchContact,
		"search-rating":      searchRating,
		"details-basic":      detailsBasic,
		"details-discovery":  detailsDiscovery,
		"details-contact":    detailsContact,
		"details-rating":     detailsRating,
	}
	return presets
}

func googlePlacesFieldMaskCostHint(mask string) string {
	mask = strings.ToLower(strings.TrimSpace(mask))
	if mask == "" {
		return ""
	}
	score := 0
	for _, keyword := range []string{"reviews", "rating", "regularopeninghours", "internationalphonenumber", "websiteuri", "evchargeoptions", "generativesummary"} {
		if strings.Contains(mask, keyword) {
			score++
		}
	}
	switch {
	case strings.Contains(mask, "*"):
		return "very high (wildcard fields)"
	case score >= 4:
		return "high"
	case score >= 2:
		return "medium"
	default:
		return "low"
	}
}
