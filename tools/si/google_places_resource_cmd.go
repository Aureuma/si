package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"si/tools/si/internal/googleplacesbridge"
)

func cmdGooglePlacesAutocomplete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{
		"json":                      true,
		"raw":                       true,
		"include-query-predictions": true,
		"allow-wildcard-mask":       true,
	})
	fs := flag.NewFlagSet("google places autocomplete", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	apiKey := fs.String("api-key", "", "override google places api key")
	baseURL := fs.String("base-url", "", "google places api base url")
	projectID := fs.String("project-id", "", "google project id")
	language := fs.String("language", "", "language code")
	region := fs.String("region", "", "region code")
	input := fs.String("input", "", "autocomplete input text")
	session := fs.String("session", "", "session token")
	includeQueryPredictions := fs.Bool("include-query-predictions", false, "include query predictions")
	origin := fs.String("origin", "", "origin lat,lng")
	locationBias := fs.String("location-bias", "", "locationBias JSON object")
	locationRestriction := fs.String("location-restriction", "", "locationRestriction JSON object")
	inputOffset := fs.Int("input-offset", -1, "input offset")
	fieldMask := fs.String("field-mask", "", "response field mask")
	fieldPreset := fs.String("field-preset", "autocomplete-basic", "field mask preset")
	allowWildcard := fs.Bool("allow-wildcard-mask", false, "allow wildcard field mask")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*input) == "" {
		printUsage("usage: si google places autocomplete --input <text> [--session <token>] [--include-query-predictions] [--location-bias <json>] [--region <cc>] [--language <lc>] [--json]")
		return
	}

	runtime, client := mustGooglePlacesClient(googlePlacesRuntimeContextInput{
		AccountFlag:   *account,
		EnvFlag:       *env,
		APIKeyFlag:    *apiKey,
		BaseURLFlag:   *baseURL,
		ProjectIDFlag: *projectID,
		LanguageFlag:  *language,
		RegionFlag:    *region,
	})
	printGooglePlacesContextBanner(runtime, *jsonOut)

	mask := resolveGoogleFieldMask("autocomplete", *fieldMask, *fieldPreset, false, *allowWildcard, *jsonOut)
	body := map[string]any{
		"input": strings.TrimSpace(*input),
	}
	if value := strings.TrimSpace(*session); value != "" {
		body["sessionToken"] = value
	}
	if *includeQueryPredictions {
		body["includeQueryPredictions"] = true
	}
	if *inputOffset >= 0 {
		body["inputOffset"] = *inputOffset
	}
	if runtime.LanguageCode != "" {
		body["languageCode"] = runtime.LanguageCode
	}
	if runtime.RegionCode != "" {
		body["regionCode"] = runtime.RegionCode
	}
	if point, err := parseGoogleLatLng(*origin); err != nil {
		fatal(err)
	} else if point != nil {
		body["origin"] = point
	}
	if bias, err := parseGoogleJSONMap(*locationBias, "location-bias"); err != nil {
		fatal(err)
	} else if bias != nil {
		body["locationBias"] = bias
	}
	if restriction, err := parseGoogleJSONMap(*locationRestriction, "location-restriction"); err != nil {
		fatal(err)
	} else if restriction != nil {
		body["locationRestriction"] = restriction
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, googleplacesbridge.Request{
		Method:    http.MethodPost,
		Path:      "/v1/places:autocomplete",
		Params:    parseGoogleParams(params),
		JSONBody:  body,
		FieldMask: mask,
	})
	if err != nil {
		printGooglePlacesError(err)
		return
	}
	if !*jsonOut && strings.TrimSpace(*session) == "" {
		warnf("autocomplete without --session may increase billing; use `si google places session new`")
	}
	printGooglePlacesResponse(resp, *jsonOut, *raw)
}

func cmdGooglePlacesSearchText(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{
		"json":                true,
		"raw":                 true,
		"open-now":            true,
		"strict-type-filter":  true,
		"allow-wildcard-mask": true,
		"all":                 true,
	})
	fs := flag.NewFlagSet("google places search-text", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	apiKey := fs.String("api-key", "", "override google places api key")
	baseURL := fs.String("base-url", "", "google places api base url")
	projectID := fs.String("project-id", "", "google project id")
	language := fs.String("language", "", "language code")
	region := fs.String("region", "", "region code")
	query := fs.String("query", "", "text query")
	pageSize := fs.Int("page-size", 0, "max results per page")
	pageToken := fs.String("page-token", "", "page token")
	includedType := fs.String("included-type", "", "included type")
	excludedType := fs.String("excluded-type", "", "excluded type")
	strictTypeFilter := fs.Bool("strict-type-filter", false, "enable strict type filtering")
	openNow := fs.Bool("open-now", false, "only open places")
	minRating := fs.Float64("min-rating", -1, "minimum rating")
	rankPreference := fs.String("rank", "", "rank preference (distance|relevance|popularity)")
	locationBias := fs.String("location-bias", "", "locationBias JSON object")
	locationRestriction := fs.String("location-restriction", "", "locationRestriction JSON object")
	fieldMask := fs.String("field-mask", "", "response field mask")
	fieldPreset := fs.String("field-preset", "search-basic", "field mask preset")
	allowWildcard := fs.Bool("allow-wildcard-mask", false, "allow wildcard field mask")
	allPages := fs.Bool("all", false, "fetch all pages")
	maxPages := fs.Int("max-pages", 5, "maximum pages when --all is set")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*query) == "" {
		printUsage("usage: si google places search-text --query <text> [--page-size <n>] [--field-mask <mask>] [--json]")
		return
	}

	runtime, client := mustGooglePlacesClient(googlePlacesRuntimeContextInput{
		AccountFlag:   *account,
		EnvFlag:       *env,
		APIKeyFlag:    *apiKey,
		BaseURLFlag:   *baseURL,
		ProjectIDFlag: *projectID,
		LanguageFlag:  *language,
		RegionFlag:    *region,
	})
	printGooglePlacesContextBanner(runtime, *jsonOut)
	mask := resolveGoogleFieldMask("search-text", *fieldMask, *fieldPreset, true, *allowWildcard, *jsonOut)

	body := map[string]any{"textQuery": strings.TrimSpace(*query)}
	if *pageSize > 0 {
		body["maxResultCount"] = *pageSize
	}
	if value := strings.TrimSpace(*pageToken); value != "" {
		body["pageToken"] = value
	}
	if value := strings.TrimSpace(*includedType); value != "" {
		body["includedType"] = value
	}
	if value := strings.TrimSpace(*excludedType); value != "" {
		body["excludedType"] = value
	}
	if *strictTypeFilter {
		body["strictTypeFiltering"] = true
	}
	if *openNow {
		body["openNow"] = true
	}
	if *minRating >= 0 {
		body["minRating"] = *minRating
	}
	if rank := normalizeGoogleRankPreference(*rankPreference); rank != "" {
		body["rankPreference"] = rank
	}
	if runtime.LanguageCode != "" {
		body["languageCode"] = runtime.LanguageCode
	}
	if runtime.RegionCode != "" {
		body["regionCode"] = runtime.RegionCode
	}
	if bias, err := parseGoogleJSONMap(*locationBias, "location-bias"); err != nil {
		fatal(err)
	} else if bias != nil {
		body["locationBias"] = bias
	}
	if restriction, err := parseGoogleJSONMap(*locationRestriction, "location-restriction"); err != nil {
		fatal(err)
	} else if restriction != nil {
		body["locationRestriction"] = restriction
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if *allPages {
		items, nextToken, err := googlePlacesSearchAllPages(ctx, client, "/v1/places:searchText", body, parseGoogleParams(params), mask, *maxPages)
		if err != nil {
			printGooglePlacesError(err)
			return
		}
		payload := map[string]any{
			"operation":       "search-text",
			"count":           len(items),
			"next_page_token": nextToken,
			"items":           items,
		}
		if *jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(payload); err != nil {
				fatal(err)
			}
			return
		}
		fmt.Printf("%s %s (%d)\n", styleHeading("Google Places search-text:"), "ok", len(items))
		if nextToken != "" {
			fmt.Printf("%s %s\n", styleHeading("Next page token:"), nextToken)
		}
		for _, item := range items {
			fmt.Printf("  %s\n", summarizeGooglePlacesItem(item))
		}
		return
	}

	resp, err := client.Do(ctx, googleplacesbridge.Request{
		Method:    http.MethodPost,
		Path:      "/v1/places:searchText",
		Params:    parseGoogleParams(params),
		JSONBody:  body,
		FieldMask: mask,
	})
	if err != nil {
		printGooglePlacesError(err)
		return
	}
	printGooglePlacesResponse(resp, *jsonOut, *raw)
}

func cmdGooglePlacesSearchNearby(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{
		"json":                true,
		"raw":                 true,
		"open-now":            true,
		"allow-wildcard-mask": true,
		"all":                 true,
	})
	fs := flag.NewFlagSet("google places search-nearby", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	apiKey := fs.String("api-key", "", "override google places api key")
	baseURL := fs.String("base-url", "", "google places api base url")
	projectID := fs.String("project-id", "", "google project id")
	language := fs.String("language", "", "language code")
	region := fs.String("region", "", "region code")
	center := fs.String("center", "", "center lat,lng")
	radius := fs.Float64("radius", 0, "radius meters")
	includedType := fs.String("included-type", "", "included type (comma-separated)")
	excludedType := fs.String("excluded-type", "", "excluded type (comma-separated)")
	rankPreference := fs.String("rank", "distance", "rank preference (distance|popularity)")
	pageSize := fs.Int("page-size", 0, "max results per page")
	pageToken := fs.String("page-token", "", "page token")
	openNow := fs.Bool("open-now", false, "only open places")
	fieldMask := fs.String("field-mask", "", "response field mask")
	fieldPreset := fs.String("field-preset", "search-basic", "field mask preset")
	allowWildcard := fs.Bool("allow-wildcard-mask", false, "allow wildcard field mask")
	allPages := fs.Bool("all", false, "fetch all pages")
	maxPages := fs.Int("max-pages", 5, "maximum pages when --all is set")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*center) == "" || *radius <= 0 {
		printUsage("usage: si google places search-nearby --center <lat,lng> --radius <meters> [--included-type <type>] [--field-mask <mask>] [--json]")
		return
	}

	location, err := parseGoogleLatLng(*center)
	if err != nil {
		fatal(err)
	}

	runtime, client := mustGooglePlacesClient(googlePlacesRuntimeContextInput{
		AccountFlag:   *account,
		EnvFlag:       *env,
		APIKeyFlag:    *apiKey,
		BaseURLFlag:   *baseURL,
		ProjectIDFlag: *projectID,
		LanguageFlag:  *language,
		RegionFlag:    *region,
	})
	printGooglePlacesContextBanner(runtime, *jsonOut)
	mask := resolveGoogleFieldMask("search-nearby", *fieldMask, *fieldPreset, true, *allowWildcard, *jsonOut)

	body := map[string]any{
		"locationRestriction": map[string]any{
			"circle": map[string]any{
				"center": location,
				"radius": *radius,
			},
		},
	}
	if values := parseGoogleCSVList(*includedType); len(values) > 0 {
		body["includedTypes"] = values
	}
	if values := parseGoogleCSVList(*excludedType); len(values) > 0 {
		body["excludedTypes"] = values
	}
	if value := normalizeGoogleNearbyRankPreference(*rankPreference); value != "" {
		body["rankPreference"] = value
	}
	if *pageSize > 0 {
		body["maxResultCount"] = *pageSize
	}
	if value := strings.TrimSpace(*pageToken); value != "" {
		body["pageToken"] = value
	}
	if *openNow {
		body["openNow"] = true
	}
	if runtime.LanguageCode != "" {
		body["languageCode"] = runtime.LanguageCode
	}
	if runtime.RegionCode != "" {
		body["regionCode"] = runtime.RegionCode
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if *allPages {
		items, nextToken, err := googlePlacesSearchAllPages(ctx, client, "/v1/places:searchNearby", body, parseGoogleParams(params), mask, *maxPages)
		if err != nil {
			printGooglePlacesError(err)
			return
		}
		payload := map[string]any{
			"operation":       "search-nearby",
			"count":           len(items),
			"next_page_token": nextToken,
			"items":           items,
		}
		if *jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(payload); err != nil {
				fatal(err)
			}
			return
		}
		fmt.Printf("%s %s (%d)\n", styleHeading("Google Places search-nearby:"), "ok", len(items))
		if nextToken != "" {
			fmt.Printf("%s %s\n", styleHeading("Next page token:"), nextToken)
		}
		for _, item := range items {
			fmt.Printf("  %s\n", summarizeGooglePlacesItem(item))
		}
		return
	}

	resp, err := client.Do(ctx, googleplacesbridge.Request{
		Method:    http.MethodPost,
		Path:      "/v1/places:searchNearby",
		Params:    parseGoogleParams(params),
		JSONBody:  body,
		FieldMask: mask,
	})
	if err != nil {
		printGooglePlacesError(err)
		return
	}
	printGooglePlacesResponse(resp, *jsonOut, *raw)
}

func cmdGooglePlacesDetails(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "allow-wildcard-mask": true})
	fs := flag.NewFlagSet("google places details", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	apiKey := fs.String("api-key", "", "override google places api key")
	baseURL := fs.String("base-url", "", "google places api base url")
	projectID := fs.String("project-id", "", "google project id")
	language := fs.String("language", "", "language code")
	region := fs.String("region", "", "region code")
	session := fs.String("session", "", "session token from autocomplete")
	fieldMask := fs.String("field-mask", "", "response field mask")
	fieldPreset := fs.String("field-preset", "details-basic", "field mask preset")
	allowWildcard := fs.Bool("allow-wildcard-mask", false, "allow wildcard field mask")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si google places details <place_id_or_name> [--session <token>] [--field-mask <mask>] [--json]")
		return
	}
	resourcePath, err := resolveGooglePlaceResourcePath(fs.Arg(0))
	if err != nil {
		fatal(err)
	}

	runtime, client := mustGooglePlacesClient(googlePlacesRuntimeContextInput{
		AccountFlag:   *account,
		EnvFlag:       *env,
		APIKeyFlag:    *apiKey,
		BaseURLFlag:   *baseURL,
		ProjectIDFlag: *projectID,
		LanguageFlag:  *language,
		RegionFlag:    *region,
	})
	printGooglePlacesContextBanner(runtime, *jsonOut)
	mask := resolveGoogleFieldMask("details", *fieldMask, *fieldPreset, true, *allowWildcard, *jsonOut)

	queryParams := parseGoogleParams(params)
	if runtime.LanguageCode != "" {
		queryParams["languageCode"] = runtime.LanguageCode
	}
	if runtime.RegionCode != "" {
		queryParams["regionCode"] = runtime.RegionCode
	}
	if value := strings.TrimSpace(*session); value != "" {
		queryParams["sessionToken"] = value
	} else if !*jsonOut {
		warnf("details without --session may not be billed with autocomplete session pricing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, googleplacesbridge.Request{
		Method:    http.MethodGet,
		Path:      resourcePath,
		Params:    queryParams,
		FieldMask: mask,
	})
	if err != nil {
		printGooglePlacesError(err)
		return
	}
	printGooglePlacesResponse(resp, *jsonOut, *raw)
}

func cmdGooglePlacesPhoto(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google places photo <get|download> ...")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "get":
		cmdGooglePlacesPhotoGet(rest)
	case "download":
		cmdGooglePlacesPhotoDownload(rest)
	default:
		printUnknown("google places photo", sub)
		printUsage("usage: si google places photo <get|download> ...")
	}
}

func cmdGooglePlacesPhotoGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "follow": true})
	fs := flag.NewFlagSet("google places photo get", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	apiKey := fs.String("api-key", "", "override google places api key")
	baseURL := fs.String("base-url", "", "google places api base url")
	projectID := fs.String("project-id", "", "google project id")
	maxWidth := fs.Int("max-width", 0, "max width in pixels")
	maxHeight := fs.Int("max-height", 0, "max height in pixels")
	follow := fs.Bool("follow", false, "follow redirect and return binary body")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si google places photo get <photo_name> [--max-width <px>] [--max-height <px>] [--json]")
		return
	}
	path, err := resolveGooglePhotoMediaPath(fs.Arg(0))
	if err != nil {
		fatal(err)
	}
	runtime, client := mustGooglePlacesClient(googlePlacesRuntimeContextInput{
		AccountFlag:   *account,
		EnvFlag:       *env,
		APIKeyFlag:    *apiKey,
		BaseURLFlag:   *baseURL,
		ProjectIDFlag: *projectID,
	})
	printGooglePlacesContextBanner(runtime, *jsonOut)
	queryParams := parseGoogleParams(params)
	if *maxWidth > 0 {
		queryParams["maxWidthPx"] = strconv.Itoa(*maxWidth)
	}
	if *maxHeight > 0 {
		queryParams["maxHeightPx"] = strconv.Itoa(*maxHeight)
	}
	if !*follow {
		queryParams["skipHttpRedirect"] = "true"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, googleplacesbridge.Request{
		Method: http.MethodGet,
		Path:   path,
		Params: queryParams,
	})
	if err != nil {
		printGooglePlacesError(err)
		return
	}
	printGooglePlacesResponse(resp, *jsonOut, *raw)
}

func cmdGooglePlacesPhotoDownload(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("google places photo download", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	apiKey := fs.String("api-key", "", "override google places api key")
	baseURL := fs.String("base-url", "", "google places api base url")
	projectID := fs.String("project-id", "", "google project id")
	output := fs.String("output", "", "output file path")
	maxWidth := fs.Int("max-width", 0, "max width in pixels")
	maxHeight := fs.Int("max-height", 0, "max height in pixels")
	jsonOut := fs.Bool("json", false, "output json")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 || strings.TrimSpace(*output) == "" {
		printUsage("usage: si google places photo download <photo_name> --output <path> [--max-width <px>] [--max-height <px>] [--json]")
		return
	}
	path, err := resolveGooglePhotoMediaPath(fs.Arg(0))
	if err != nil {
		fatal(err)
	}
	runtime, _ := mustGooglePlacesClient(googlePlacesRuntimeContextInput{
		AccountFlag:   *account,
		EnvFlag:       *env,
		APIKeyFlag:    *apiKey,
		BaseURLFlag:   *baseURL,
		ProjectIDFlag: *projectID,
	})
	printGooglePlacesContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	bytesWritten, contentType, err := downloadGooglePlacePhoto(ctx, runtime, path, *maxWidth, *maxHeight, parseGoogleParams(params), strings.TrimSpace(*output))
	if err != nil {
		printGooglePlacesError(err)
		return
	}
	payload := map[string]any{
		"output":        strings.TrimSpace(*output),
		"bytes_written": bytesWritten,
		"content_type":  contentType,
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	successf("downloaded photo to %s (%d bytes)", strings.TrimSpace(*output), bytesWritten)
}

func downloadGooglePlacePhoto(ctx context.Context, runtime googlePlacesRuntimeContext, path string, maxWidth int, maxHeight int, params map[string]string, output string) (int64, string, error) {
	endpoint, err := url.Parse(strings.TrimRight(runtime.BaseURL, "/") + path)
	if err != nil {
		return 0, "", err
	}
	q := endpoint.Query()
	for key, value := range params {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		q.Set(key, strings.TrimSpace(value))
	}
	if maxWidth > 0 {
		q.Set("maxWidthPx", strconv.Itoa(maxWidth))
	}
	if maxHeight > 0 {
		q.Set("maxHeightPx", strconv.Itoa(maxHeight))
	}
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("X-Goog-Api-Key", strings.TrimSpace(runtime.APIKey))
	req.Header.Set("User-Agent", "si-google-places/1.0")
	httpClient := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return 0, "", googleplacesbridge.NormalizeHTTPError(resp.StatusCode, resp.Header, string(body))
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o750); err != nil {
		return 0, "", err
	}
	if err := os.WriteFile(output, content, 0o600); err != nil {
		return 0, "", err
	}
	return int64(len(content)), strings.TrimSpace(resp.Header.Get("Content-Type")), nil
}

func cmdGooglePlacesTypes(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google places types <list|validate>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGooglePlacesTypesList(rest)
	case "validate":
		cmdGooglePlacesTypesValidate(rest)
	default:
		printUnknown("google places types", sub)
		printUsage("usage: si google places types <list|validate>")
	}
}

func cmdGooglePlacesTypesList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("google places types list", flag.ExitOnError)
	group := fs.String("group", "", "type group (food|lodging|transport|outdoor|culture|shopping|services|health|education|government|business|sports|all)")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google places types list [--group <name>] [--json]")
		return
	}
	catalog := googlePlacesTypeCatalog()
	selectedGroup := strings.ToLower(strings.TrimSpace(*group))
	if selectedGroup != "" && selectedGroup != "all" {
		if _, ok := catalog[selectedGroup]; !ok {
			fatal(fmt.Errorf("unknown type group %q", selectedGroup))
		}
	}
	var payload map[string]any
	if selectedGroup == "" || selectedGroup == "all" {
		payload = map[string]any{"groups": catalog}
	} else {
		payload = map[string]any{"group": selectedGroup, "types": catalog[selectedGroup]}
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	if selectedGroup != "" && selectedGroup != "all" {
		fmt.Printf("%s %s\n", styleHeading("Group:"), selectedGroup)
		for _, item := range catalog[selectedGroup] {
			fmt.Printf("  %s\n", item)
		}
		return
	}
	groups := make([]string, 0, len(catalog))
	for groupName := range catalog {
		groups = append(groups, groupName)
	}
	sort.Strings(groups)
	for _, groupName := range groups {
		fmt.Printf("%s %s (%d)\n", styleHeading("Group:"), groupName, len(catalog[groupName]))
		for _, item := range catalog[groupName] {
			fmt.Printf("  %s\n", item)
		}
	}
}

func cmdGooglePlacesTypesValidate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("google places types validate", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si google places types validate <type> [--json]")
		return
	}
	typeName := strings.ToLower(strings.TrimSpace(fs.Arg(0)))
	catalog := googlePlacesTypeCatalog()
	group := ""
	valid := false
	for groupName, values := range catalog {
		for _, value := range values {
			if value == typeName {
				valid = true
				group = groupName
				break
			}
		}
		if valid {
			break
		}
	}
	suggestions := make([]string, 0, 5)
	if !valid {
		for _, values := range catalog {
			for _, value := range values {
				if strings.Contains(value, typeName) || strings.Contains(typeName, value) {
					suggestions = append(suggestions, value)
				}
			}
		}
		sort.Strings(suggestions)
		if len(suggestions) > 5 {
			suggestions = suggestions[:5]
		}
	}
	payload := map[string]any{
		"type":        typeName,
		"valid":       valid,
		"group":       group,
		"suggestions": suggestions,
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		if !valid {
			os.Exit(1)
		}
		return
	}
	if valid {
		successf("type %s is valid (group: %s)", typeName, group)
		return
	}
	fmt.Fprintf(os.Stderr, "%s\n", styleError("unknown place type: "+typeName))
	if len(suggestions) > 0 {
		fmt.Fprintf(os.Stderr, "%s %s\n", styleHeading("Suggestions:"), strings.Join(suggestions, ", "))
	}
	os.Exit(1)
}

func cmdGooglePlacesRaw(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "allow-wildcard-mask": true})
	fs := flag.NewFlagSet("google places raw", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	apiKey := fs.String("api-key", "", "override google places api key")
	baseURL := fs.String("base-url", "", "google places api base url")
	projectID := fs.String("project-id", "", "google project id")
	language := fs.String("language", "", "language code")
	region := fs.String("region", "", "region code")
	method := fs.String("method", http.MethodGet, "http method")
	path := fs.String("path", "", "api path")
	body := fs.String("body", "", "raw request body")
	fieldMask := fs.String("field-mask", "", "response field mask")
	fieldPreset := fs.String("field-preset", "", "field mask preset")
	allowWildcard := fs.Bool("allow-wildcard-mask", false, "allow wildcard field mask")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*path) == "" {
		printUsage("usage: si google places raw --method <GET|POST> --path <api-path> [--param key=value] [--body raw] [--field-mask <mask>] [--json]")
		return
	}
	runtime, client := mustGooglePlacesClient(googlePlacesRuntimeContextInput{
		AccountFlag:   *account,
		EnvFlag:       *env,
		APIKeyFlag:    *apiKey,
		BaseURLFlag:   *baseURL,
		ProjectIDFlag: *projectID,
		LanguageFlag:  *language,
		RegionFlag:    *region,
	})
	printGooglePlacesContextBanner(runtime, *jsonOut)
	mask := resolveGoogleFieldMask("raw", *fieldMask, *fieldPreset, false, *allowWildcard, *jsonOut)
	rawBody := strings.TrimSpace(*body)
	if strings.HasPrefix(rawBody, "@") {
		data, err := os.ReadFile(strings.TrimPrefix(rawBody, "@"))
		if err != nil {
			fatal(err)
		}
		rawBody = string(data)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, googleplacesbridge.Request{
		Method:    strings.ToUpper(strings.TrimSpace(*method)),
		Path:      strings.TrimSpace(*path),
		Params:    parseGoogleParams(params),
		RawBody:   rawBody,
		FieldMask: mask,
	})
	if err != nil {
		printGooglePlacesError(err)
		return
	}
	printGooglePlacesResponse(resp, *jsonOut, *raw)
}

func cmdGooglePlacesReport(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google places report <usage|sessions> [flags]")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "usage":
		cmdGooglePlacesReportUsage(rest)
	case "sessions":
		cmdGooglePlacesReportSessions(rest)
	default:
		printUnknown("google places report", sub)
		printUsage("usage: si google places report <usage|sessions> [flags]")
	}
}

func cmdGooglePlacesReportUsage(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("google places report usage", flag.ExitOnError)
	sinceRaw := fs.String("since", "", "start timestamp (unix seconds or RFC3339)")
	untilRaw := fs.String("until", "", "end timestamp (unix seconds or RFC3339)")
	account := fs.String("account", "", "filter account alias")
	env := fs.String("env", "", "filter environment")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google places report usage [--since <ts>] [--until <ts>] [--account <alias>] [--env <prod|staging|dev>] [--json]")
		return
	}
	since, until, err := parseGoogleReportWindow(*sinceRaw, *untilRaw)
	if err != nil {
		fatal(err)
	}
	settings := loadSettingsOrDefault()
	logPath := resolveGooglePlacesLogPath(settings)
	if strings.TrimSpace(logPath) == "" {
		fatal(fmt.Errorf("google places log path is not configured"))
	}
	report, err := buildGooglePlacesUsageReport(logPath, strings.TrimSpace(*account), normalizeGoogleEnvironment(*env), since, until)
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fatal(err)
		}
		return
	}
	printGooglePlacesUsageReport(report)
}

func cmdGooglePlacesReportSessions(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("google places report sessions", flag.ExitOnError)
	sinceRaw := fs.String("since", "", "start timestamp (unix seconds or RFC3339)")
	untilRaw := fs.String("until", "", "end timestamp (unix seconds or RFC3339)")
	account := fs.String("account", "", "filter account alias")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google places report sessions [--since <ts>] [--until <ts>] [--account <alias>] [--json]")
		return
	}
	since, until, err := parseGoogleReportWindow(*sinceRaw, *untilRaw)
	if err != nil {
		fatal(err)
	}
	report, err := buildGooglePlacesSessionsReport(strings.TrimSpace(*account), since, until)
	if err != nil {
		fatal(err)
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fatal(err)
		}
		return
	}
	printGooglePlacesSessionsReport(report)
}

func normalizeGoogleRankPreference(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "", "relevance", "relevance_high":
		return "RELEVANCE"
	case "distance":
		return "DISTANCE"
	case "popularity":
		return "POPULARITY"
	default:
		return strings.ToUpper(value)
	}
}

func normalizeGoogleNearbyRankPreference(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "", "distance":
		return "DISTANCE"
	case "popularity", "relevance":
		return "POPULARITY"
	default:
		return strings.ToUpper(value)
	}
}

func googlePlacesSearchAllPages(ctx context.Context, client googlePlacesBridgeClient, endpoint string, body map[string]any, params map[string]string, fieldMask string, maxPages int) ([]map[string]any, string, error) {
	if maxPages <= 0 {
		maxPages = 5
	}
	items := make([]map[string]any, 0, 200)
	nextToken := ""
	requestBody := cloneGoogleMap(body)
	if requestBody == nil {
		requestBody = map[string]any{}
	}
	for page := 1; page <= maxPages; page++ {
		resp, err := client.Do(ctx, googleplacesbridge.Request{
			Method:    http.MethodPost,
			Path:      endpoint,
			Params:    params,
			JSONBody:  requestBody,
			FieldMask: fieldMask,
		})
		if err != nil {
			return nil, "", err
		}
		batch := extractGoogleList(resp)
		if len(batch) > 0 {
			items = append(items, batch...)
		}
		nextToken = ""
		if resp.Data != nil {
			if token, ok := resp.Data["nextPageToken"].(string); ok {
				nextToken = strings.TrimSpace(token)
			}
		}
		if nextToken == "" {
			break
		}
		requestBody["pageToken"] = nextToken
	}
	return items, nextToken, nil
}

type googlePlacesUsageLogEvent struct {
	Time       time.Time
	Event      string
	Method     string
	Path       string
	StatusCode int
	DurationMS int64
	Account    string
	Env        string
	RequestID  string
}

func buildGooglePlacesUsageReport(logPath string, account string, env string, since *time.Time, until *time.Time) (map[string]any, error) {
	file, err := openLocalFile(logPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{
				"log_path":       logPath,
				"requests":       0,
				"responses":      0,
				"errors":         0,
				"average_ms":     0,
				"status_buckets": map[string]int{},
				"top_endpoints":  []map[string]any{},
			}, nil
		}
		return nil, err
	}
	defer file.Close()

	events, err := readGooglePlacesUsageEvents(file)
	if err != nil {
		return nil, err
	}
	requestCount := 0
	responseCount := 0
	errorCount := 0
	totalDuration := int64(0)
	durationCount := 0
	statusBuckets := map[string]int{}
	endpointCounts := map[string]int{}
	requestIDs := map[string]struct{}{}

	for _, event := range events {
		if !event.Time.IsZero() && !inGoogleTimeRange(event.Time, since, until) {
			continue
		}
		if account != "" && !strings.EqualFold(strings.TrimSpace(account), strings.TrimSpace(event.Account)) {
			continue
		}
		if env != "" && !strings.EqualFold(strings.TrimSpace(env), strings.TrimSpace(event.Env)) {
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(event.Method)) + " " + parseGooglePathRef(event.Path)
		switch event.Event {
		case "request":
			requestCount++
			endpointCounts[key]++
		case "response":
			responseCount++
			if event.StatusCode >= 400 {
				errorCount++
			}
			bucket := googleStatusBucket(event.StatusCode)
			statusBuckets[bucket]++
			if event.DurationMS > 0 {
				totalDuration += event.DurationMS
				durationCount++
			}
			if strings.TrimSpace(event.RequestID) != "" {
				requestIDs[event.RequestID] = struct{}{}
			}
		}
	}

	average := int64(0)
	if durationCount > 0 {
		average = totalDuration / int64(durationCount)
	}
	topEndpoints := make([]map[string]any, 0, len(endpointCounts))
	for key, count := range endpointCounts {
		topEndpoints = append(topEndpoints, map[string]any{"endpoint": key, "count": count})
	}
	sort.Slice(topEndpoints, func(i, j int) bool {
		left := int(topEndpoints[i]["count"].(int))
		right := int(topEndpoints[j]["count"].(int))
		if left == right {
			return topEndpoints[i]["endpoint"].(string) < topEndpoints[j]["endpoint"].(string)
		}
		return left > right
	})
	if len(topEndpoints) > 10 {
		topEndpoints = topEndpoints[:10]
	}

	out := map[string]any{
		"log_path":           logPath,
		"requests":           requestCount,
		"responses":          responseCount,
		"errors":             errorCount,
		"average_ms":         average,
		"status_buckets":     statusBuckets,
		"unique_request_ids": len(requestIDs),
		"top_endpoints":      topEndpoints,
		"filter_account":     account,
		"filter_environment": env,
		"window_since":       googleReportTimeString(since),
		"window_until":       googleReportTimeString(until),
	}
	return out, nil
}

func readGooglePlacesUsageEvents(r io.Reader) ([]googlePlacesUsageLogEvent, error) {
	decoder := json.NewDecoder(r)
	events := []googlePlacesUsageLogEvent{}
	for {
		var entry map[string]any
		if err := decoder.Decode(&entry); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		event := googlePlacesUsageLogEvent{}
		if ts, ok := entry["ts"].(string); ok {
			event.Time, _ = time.Parse(time.RFC3339Nano, strings.TrimSpace(ts))
			event.Time = event.Time.UTC()
		}
		event.Event, _ = entry["event"].(string)
		event.Method, _ = entry["method"].(string)
		event.Path, _ = entry["path"].(string)
		event.Account, _ = entry["ctx_account_alias"].(string)
		event.Env, _ = entry["ctx_environment"].(string)
		event.RequestID, _ = entry["request_id"].(string)
		event.StatusCode = readGoogleInt(entry["status"])
		event.DurationMS = int64(readGoogleInt(entry["duration_ms"]))
		events = append(events, event)
	}
	return events, nil
}

func buildGooglePlacesSessionsReport(account string, since *time.Time, until *time.Time) (map[string]any, error) {
	store, err := loadGooglePlacesSessionStore()
	if err != nil {
		return nil, err
	}
	rows := make([]googlePlacesSessionEntry, 0, len(store.Sessions))
	active := 0
	ended := 0
	for _, entry := range store.Sessions {
		if account != "" && !strings.EqualFold(account, strings.TrimSpace(entry.AccountAlias)) {
			continue
		}
		createdAt, ok := parseGoogleRFC3339(entry.CreatedAt)
		if !ok {
			continue
		}
		if !inGoogleTimeRange(createdAt, since, until) {
			continue
		}
		rows = append(rows, entry)
		if strings.TrimSpace(entry.EndedAt) == "" {
			active++
		} else {
			ended++
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].CreatedAt > rows[j].CreatedAt
	})
	return map[string]any{
		"total":          len(rows),
		"active":         active,
		"ended":          ended,
		"filter_account": account,
		"window_since":   googleReportTimeString(since),
		"window_until":   googleReportTimeString(until),
		"sessions":       rows,
	}, nil
}

func printGooglePlacesUsageReport(report map[string]any) {
	fmt.Printf("%s %s\n", styleHeading("Google Places usage report:"), orDash(stringifyGooglePlacesAny(report["log_path"])))
	fmt.Printf("%s %s\n", styleHeading("Requests:"), stringifyGooglePlacesAny(report["requests"]))
	fmt.Printf("%s %s\n", styleHeading("Responses:"), stringifyGooglePlacesAny(report["responses"]))
	fmt.Printf("%s %s\n", styleHeading("Errors:"), stringifyGooglePlacesAny(report["errors"]))
	fmt.Printf("%s %s\n", styleHeading("Avg duration:"), stringifyGooglePlacesAny(report["average_ms"])+"ms")
	if buckets, ok := report["status_buckets"].(map[string]int); ok && len(buckets) > 0 {
		keys := make([]string, 0, len(buckets))
		for key := range buckets {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		fmt.Printf("%s\n", styleHeading("Status buckets:"))
		for _, key := range keys {
			fmt.Printf("  %s %d\n", padRightANSI(key, 4), buckets[key])
		}
	}
	if top, ok := report["top_endpoints"].([]map[string]any); ok && len(top) > 0 {
		fmt.Printf("%s\n", styleHeading("Top endpoints:"))
		for _, item := range top {
			fmt.Printf("  %s %s\n", padRightANSI(stringifyGooglePlacesAny(item["count"]), 4), stringifyGooglePlacesAny(item["endpoint"]))
		}
	}
}

func printGooglePlacesSessionsReport(report map[string]any) {
	fmt.Printf("%s %s\n", styleHeading("Google Places sessions report:"), stringifyGooglePlacesAny(report["total"]))
	fmt.Printf("%s %s\n", styleHeading("Active:"), stringifyGooglePlacesAny(report["active"]))
	fmt.Printf("%s %s\n", styleHeading("Ended:"), stringifyGooglePlacesAny(report["ended"]))
	sessions, _ := report["sessions"].([]googlePlacesSessionEntry)
	if len(sessions) == 0 {
		return
	}
	fmt.Printf("%s\n", styleHeading("Sessions:"))
	for _, item := range sessions {
		status := "active"
		if strings.TrimSpace(item.EndedAt) != "" {
			status = "ended"
		}
		fmt.Printf("  %s %s %s\n", padRightANSI(item.Token, 38), padRightANSI(status, 8), item.CreatedAt)
	}
}

func googleStatusBucket(statusCode int) string {
	switch {
	case statusCode >= 500:
		return "5xx"
	case statusCode >= 400:
		return "4xx"
	case statusCode >= 300:
		return "3xx"
	case statusCode >= 200:
		return "2xx"
	case statusCode > 0:
		return "1xx"
	default:
		return "unknown"
	}
}

func readGoogleInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}

func googleReportTimeString(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func cloneGoogleMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func extractGoogleList(resp googleplacesbridge.Response) []map[string]any {
	if len(resp.List) > 0 {
		return resp.List
	}
	if resp.Data == nil {
		return nil
	}
	if raw, ok := resp.Data["places"].([]any); ok {
		out := make([]map[string]any, 0, len(raw))
		for _, item := range raw {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, obj)
		}
		return out
	}
	if raw, ok := resp.Data["suggestions"].([]any); ok {
		out := make([]map[string]any, 0, len(raw))
		for _, item := range raw {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, obj)
		}
		return out
	}
	return nil
}

func googlePlacesTypeCatalog() map[string][]string {
	return map[string][]string{
		"business": {
			"accounting", "atm", "bank", "city_hall", "courthouse", "embassy", "fire_station", "insurance_agency", "lawyer", "local_government_office", "moving_company", "post_office", "real_estate_agency", "storage", "travel_agency",
		},
		"culture": {
			"art_gallery", "library", "museum", "performing_arts_theater", "tourist_attraction", "zoo",
		},
		"education": {
			"primary_school", "school", "secondary_school", "university",
		},
		"food": {
			"bakery", "bar", "cafe", "coffee_shop", "fast_food_restaurant", "meal_delivery", "meal_takeaway", "restaurant", "sandwich_shop", "steak_house",
		},
		"government": {
			"city_hall", "courthouse", "fire_station", "police", "post_office", "local_government_office",
		},
		"health": {
			"dentist", "doctor", "drugstore", "hospital", "medical_lab", "pharmacy", "physiotherapist", "veterinary_care",
		},
		"lodging": {
			"campground", "extended_stay_hotel", "hostel", "hotel", "lodging", "motel", "resort_hotel",
		},
		"outdoor": {
			"amusement_park", "beach", "campground", "dog_park", "hiking_area", "national_park", "park", "playground", "state_park",
		},
		"services": {
			"beauty_salon", "car_rental", "car_repair", "car_wash", "electrician", "florist", "funeral_home", "gym", "hair_care", "laundry", "locksmith", "painter", "pet_store", "plumber", "roofing_contractor", "spa",
		},
		"shopping": {
			"book_store", "clothing_store", "convenience_store", "department_store", "electronics_store", "furniture_store", "grocery_store", "hardware_store", "home_goods_store", "jewelry_store", "liquor_store", "market", "shopping_mall", "store", "supermarket",
		},
		"sports": {
			"athletic_field", "fitness_center", "golf_course", "gym", "ice_skating_rink", "sports_complex", "stadium", "swimming_pool",
		},
		"transport": {
			"airport", "bus_station", "bus_stop", "electric_vehicle_charging_station", "gas_station", "light_rail_station", "parking", "subway_station", "taxi_stand", "train_station", "transit_station",
		},
	}
}
