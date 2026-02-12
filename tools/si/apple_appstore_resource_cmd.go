package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"si/tools/si/internal/appstorebridge"
)

type appleListingMutation struct {
	BundleID      string
	Locale        string
	VersionString string
	CreateVersion bool
	Platform      string
	AppInfoAttrs  map[string]any
	VersionAttrs  map[string]any
}

func cmdAppleAppStoreApp(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si apple appstore app <list|get|create>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdAppleAppStoreAppList(rest)
	case "get":
		cmdAppleAppStoreAppGet(rest)
	case "create":
		cmdAppleAppStoreAppCreate(rest)
	default:
		printUnknown("apple appstore app", sub)
		printUsage("usage: si apple appstore app <list|get|create>")
	}
}

func cmdAppleAppStoreAppList(args []string) {
	fs, common := appleAppStoreCommonFlagSet("apple appstore app list", args, true)
	limit := fs.Int("limit", 20, "max apps")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si apple appstore app list [--bundle-id <id>] [--limit <n>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	printAppleAppStoreContextBanner(runtime, common.json())
	params := map[string]string{}
	if *limit > 0 {
		params["limit"] = fmt.Sprintf("%d", *limit)
	}
	if value := strings.TrimSpace(stringValue(common.bundleID)); value != "" {
		params["filter[bundleId]"] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, appstorebridge.Request{Method: http.MethodGet, Path: "/v1/apps", Params: params})
	if err != nil {
		printAppleAppStoreError(err)
		return
	}
	printAppleAppStoreResponse(resp, common.json(), common.rawEnabled())
}

func cmdAppleAppStoreAppGet(args []string) {
	fs, common := appleAppStoreCommonFlagSet("apple appstore app get", args, true)
	appID := fs.String("app-id", "", "app id")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si apple appstore app get [--app-id <id>|--bundle-id <id>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	printAppleAppStoreContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resolvedAppID := strings.TrimSpace(*appID)
	if resolvedAppID == "" {
		bundleID := firstNonEmpty(strings.TrimSpace(stringValue(common.bundleID)), strings.TrimSpace(runtime.BundleID))
		if bundleID == "" {
			fatal(fmt.Errorf("--app-id or --bundle-id is required"))
		}
		app, err := appleFindAppByBundleID(ctx, client, bundleID)
		if err != nil {
			printAppleAppStoreError(err)
			return
		}
		if app == nil {
			fatal(fmt.Errorf("app not found for bundle id %s", bundleID))
		}
		resolvedAppID = strings.TrimSpace(parseAppleAnyString(app["id"]))
	}
	resp, err := client.Do(ctx, appstorebridge.Request{Method: http.MethodGet, Path: "/v1/apps/" + url.PathEscape(resolvedAppID)})
	if err != nil {
		printAppleAppStoreError(err)
		return
	}
	printAppleAppStoreResponse(resp, common.json(), common.rawEnabled())
}

func cmdAppleAppStoreAppCreate(args []string) {
	fs, common := appleAppStoreCommonFlagSet("apple appstore app create", args, false)
	bundleName := fs.String("bundle-name", "", "bundle id display name")
	appName := fs.String("app-name", "", "app record name")
	sku := fs.String("sku", "", "app sku for app record creation")
	primaryLocale := fs.String("primary-locale", "", "app primary locale (e.g. en-US)")
	skipBundleCreate := fs.Bool("skip-bundle-create", false, "do not create bundle id if missing")
	allowPartial := fs.Bool("allow-partial", true, "succeed when bundle id creation works but app record create is unavailable")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si apple appstore app create --bundle-id <id> [--bundle-name <name>] [--platform <IOS|MAC_OS|TV_OS|VISION_OS>] [--app-name <name> --sku <sku> --primary-locale <locale>]")
		return
	}
	runtime, client := common.mustClient()
	bundleID := firstNonEmpty(strings.TrimSpace(stringValue(common.bundleID)), strings.TrimSpace(runtime.BundleID))
	if bundleID == "" {
		fatal(fmt.Errorf("--bundle-id is required"))
	}
	platform := normalizeApplePlatform(firstNonEmpty(strings.TrimSpace(stringValue(common.platform)), strings.TrimSpace(runtime.Platform), "IOS"))
	if platform == "" {
		fatal(fmt.Errorf("invalid platform"))
	}
	printAppleAppStoreContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	bundleResource, err := appleFindBundleIDResource(ctx, client, bundleID)
	bundleCreated := false
	if err != nil {
		printAppleAppStoreError(err)
		return
	}
	if bundleResource == nil {
		if *skipBundleCreate {
			fatal(fmt.Errorf("bundle id %s not found and --skip-bundle-create is set", bundleID))
		}
		payload := map[string]any{
			"data": map[string]any{
				"type": "bundleIds",
				"attributes": map[string]any{
					"identifier": bundleID,
					"name":       firstNonEmpty(strings.TrimSpace(*bundleName), bundleID),
				},
				"relationships": map[string]any{
					"platform": map[string]any{
						"data": map[string]any{"type": "bundleIdPlatforms", "id": platform},
					},
				},
			},
		}
		resp, createErr := client.Do(ctx, appstorebridge.Request{Method: http.MethodPost, Path: "/v1/bundleIds", JSONBody: payload})
		if createErr != nil {
			printAppleAppStoreError(createErr)
			return
		}
		bundleCreated = true
		bundleResource = appleFirstDataResource(resp)
	}
	result := map[string]any{
		"bundle_id":          bundleID,
		"bundle_created":     bundleCreated,
		"bundle_resource_id": "",
		"app_created":        false,
		"app_id":             "",
	}
	if bundleResource != nil {
		result["bundle_resource_id"] = strings.TrimSpace(parseAppleAnyString(bundleResource["id"]))
	}
	appResource, err := appleFindAppByBundleID(ctx, client, bundleID)
	if err != nil {
		printAppleAppStoreError(err)
		return
	}
	if appResource != nil {
		result["app_id"] = strings.TrimSpace(parseAppleAnyString(appResource["id"]))
	}
	if appResource == nil && strings.TrimSpace(*appName) != "" {
		if strings.TrimSpace(*sku) == "" {
			fatal(fmt.Errorf("--sku is required when --app-name is provided"))
		}
		if strings.TrimSpace(*primaryLocale) == "" {
			*primaryLocale = firstNonEmpty(strings.TrimSpace(runtime.Locale), "en-US")
		}
		bundleResourceID := strings.TrimSpace(parseAppleAnyString(result["bundle_resource_id"]))
		if bundleResourceID == "" {
			fatal(fmt.Errorf("bundle resource id not resolved for %s", bundleID))
		}
		appPayload := map[string]any{
			"data": map[string]any{
				"type": "apps",
				"attributes": map[string]any{
					"name":          strings.TrimSpace(*appName),
					"sku":           strings.TrimSpace(*sku),
					"primaryLocale": strings.TrimSpace(*primaryLocale),
				},
				"relationships": map[string]any{
					"bundleId": map[string]any{
						"data": map[string]any{"type": "bundleIds", "id": bundleResourceID},
					},
				},
			},
		}
		createResp, createErr := client.Do(ctx, appstorebridge.Request{Method: http.MethodPost, Path: "/v1/apps", JSONBody: appPayload})
		if createErr != nil {
			if *allowPartial {
				warnf("app record creation is unavailable via current API/account permissions; bundle id created and retained: %v", createErr)
				result["app_create_error"] = createErr.Error()
			} else {
				printAppleAppStoreError(createErr)
				return
			}
		} else {
			created := appleFirstDataResource(createResp)
			if created != nil {
				result["app_created"] = true
				result["app_id"] = strings.TrimSpace(parseAppleAnyString(created["id"]))
			}
		}
	}
	if common.json() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			fatal(err)
		}
		return
	}
	successf("apple appstore create flow completed")
	fmt.Printf("%s %s\n", styleHeading("Bundle ID:"), bundleID)
	fmt.Printf("%s %s\n", styleHeading("Bundle created:"), boolString(bundleCreated))
	fmt.Printf("%s %s\n", styleHeading("App ID:"), orDash(parseAppleAnyString(result["app_id"])))
	if text := strings.TrimSpace(parseAppleAnyString(result["app_create_error"])); text != "" {
		warnf("app record create warning: %s", text)
	}
}

func cmdAppleAppStoreListing(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si apple appstore listing <get|update>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "get":
		cmdAppleAppStoreListingGet(rest)
	case "update", "set", "patch":
		cmdAppleAppStoreListingUpdate(rest)
	default:
		printUnknown("apple appstore listing", sub)
		printUsage("usage: si apple appstore listing <get|update>")
	}
}

func cmdAppleAppStoreListingGet(args []string) {
	fs, common := appleAppStoreCommonFlagSet("apple appstore listing get", args, true)
	versionString := fs.String("version", "", "app store version string (optional)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si apple appstore listing get --bundle-id <id> [--locale <code>] [--version <version>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	bundleID := firstNonEmpty(strings.TrimSpace(stringValue(common.bundleID)), strings.TrimSpace(runtime.BundleID))
	if bundleID == "" {
		fatal(fmt.Errorf("bundle id is required (--bundle-id or configured default_bundle_id)"))
	}
	locale := firstNonEmpty(strings.TrimSpace(stringValue(common.locale)), strings.TrimSpace(runtime.Locale), "en-US")
	printAppleAppStoreContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	appResource, err := appleFindAppByBundleID(ctx, client, bundleID)
	if err != nil {
		printAppleAppStoreError(err)
		return
	}
	if appResource == nil {
		fatal(fmt.Errorf("app not found for bundle id %s", bundleID))
	}
	appID := strings.TrimSpace(parseAppleAnyString(appResource["id"]))
	appInfoID, err := appleResolveAppInfoID(ctx, client, appID)
	if err != nil {
		printAppleAppStoreError(err)
		return
	}
	appInfoLoc, err := appleFindAppInfoLocalization(ctx, client, appInfoID, locale)
	if err != nil {
		printAppleAppStoreError(err)
		return
	}
	var versionLoc map[string]any
	if strings.TrimSpace(*versionString) != "" {
		versionID, err := appleResolveAppStoreVersionID(ctx, client, appID, runtime.Platform, strings.TrimSpace(*versionString), false)
		if err != nil {
			printAppleAppStoreError(err)
			return
		}
		if versionID != "" {
			versionLoc, err = appleFindAppStoreVersionLocalization(ctx, client, versionID, locale)
			if err != nil {
				printAppleAppStoreError(err)
				return
			}
		}
	}
	payload := map[string]any{
		"bundle_id":             bundleID,
		"app_id":                appID,
		"locale":                locale,
		"app_info_localization": appInfoLoc,
		"version_localization":  versionLoc,
	}
	if common.json() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	raw, _ := json.MarshalIndent(payload, "", "  ")
	fmt.Println(string(raw))
}

func cmdAppleAppStoreListingUpdate(args []string) {
	fs, common := appleAppStoreCommonFlagSet("apple appstore listing update", args, false)
	versionString := fs.String("version", "", "app store version string")
	createVersion := fs.Bool("create-version", false, "create app store version when missing and --version is provided")
	appInfoBody := fs.String("app-info-body", "", "app-info json body or @file")
	versionBody := fs.String("version-body", "", "version-localization json body or @file")
	name := fs.String("name", "", "localized app name")
	subtitle := fs.String("subtitle", "", "localized subtitle")
	privacyPolicyURL := fs.String("privacy-policy-url", "", "privacy policy URL")
	description := fs.String("description", "", "localized description")
	keywords := fs.String("keywords", "", "localized keywords")
	marketingURL := fs.String("marketing-url", "", "localized marketing URL")
	promotionalText := fs.String("promotional-text", "", "localized promotional text")
	supportURL := fs.String("support-url", "", "localized support URL")
	whatsNew := fs.String("whats-new", "", "localized what's new")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si apple appstore listing update --bundle-id <id> [--locale <code>] [--version <version>] [--create-version] [--name <text>] [--subtitle <text>] [--description <text>] [--whats-new <text>] [--app-info-body @file] [--version-body @file]")
		return
	}
	runtime, client := common.mustClient()
	bundleID := firstNonEmpty(strings.TrimSpace(stringValue(common.bundleID)), strings.TrimSpace(runtime.BundleID))
	if bundleID == "" {
		fatal(fmt.Errorf("bundle id is required (--bundle-id or configured default_bundle_id)"))
	}
	locale := firstNonEmpty(strings.TrimSpace(stringValue(common.locale)), strings.TrimSpace(runtime.Locale), "en-US")
	appInfoAttrs, err := parseAppleAppStoreJSONBody(*appInfoBody)
	if err != nil {
		fatal(err)
	}
	versionAttrs, err := parseAppleAppStoreJSONBody(*versionBody)
	if err != nil {
		fatal(err)
	}
	if appInfoAttrs == nil {
		appInfoAttrs = map[string]any{}
	}
	if versionAttrs == nil {
		versionAttrs = map[string]any{}
	}
	if strings.TrimSpace(*name) != "" {
		appInfoAttrs["name"] = strings.TrimSpace(*name)
	}
	if strings.TrimSpace(*subtitle) != "" {
		appInfoAttrs["subtitle"] = strings.TrimSpace(*subtitle)
	}
	if strings.TrimSpace(*privacyPolicyURL) != "" {
		appInfoAttrs["privacyPolicyUrl"] = strings.TrimSpace(*privacyPolicyURL)
	}
	if strings.TrimSpace(*description) != "" {
		versionAttrs["description"] = strings.TrimSpace(*description)
	}
	if strings.TrimSpace(*keywords) != "" {
		versionAttrs["keywords"] = strings.TrimSpace(*keywords)
	}
	if strings.TrimSpace(*marketingURL) != "" {
		versionAttrs["marketingUrl"] = strings.TrimSpace(*marketingURL)
	}
	if strings.TrimSpace(*promotionalText) != "" {
		versionAttrs["promotionalText"] = strings.TrimSpace(*promotionalText)
	}
	if strings.TrimSpace(*supportURL) != "" {
		versionAttrs["supportUrl"] = strings.TrimSpace(*supportURL)
	}
	if strings.TrimSpace(*whatsNew) != "" {
		versionAttrs["whatsNew"] = strings.TrimSpace(*whatsNew)
	}
	if len(appInfoAttrs) == 0 && len(versionAttrs) == 0 {
		fatal(fmt.Errorf("no listing fields provided"))
	}
	printAppleAppStoreContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	summary, err := appleApplyListingMutation(ctx, client, runtime, appleListingMutation{
		BundleID:      bundleID,
		Locale:        locale,
		VersionString: strings.TrimSpace(*versionString),
		CreateVersion: *createVersion,
		Platform:      runtime.Platform,
		AppInfoAttrs:  appInfoAttrs,
		VersionAttrs:  versionAttrs,
	})
	if err != nil {
		printAppleAppStoreError(err)
		return
	}
	if common.json() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(summary); err != nil {
			fatal(err)
		}
		return
	}
	raw, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Println(string(raw))
}

func cmdAppleAppStoreRaw(args []string) {
	fs, common := appleAppStoreCommonFlagSet("apple appstore raw", args, true)
	method := fs.String("method", http.MethodGet, "http method")
	path := fs.String("path", "", "api path")
	body := fs.String("body", "", "raw request body or @file")
	contentType := fs.String("content-type", "", "request content type")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*path) == "" {
		printUsage("usage: si apple appstore raw --method <GET|POST|PATCH|DELETE> --path <api-path> [--param key=value] [--body raw|@file] [--json]")
		return
	}
	runtime, client := common.mustClient()
	printAppleAppStoreContextBanner(runtime, common.json())
	rawBody, err := parseAppleAppStoreBody(*body)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	resp, err := client.Do(ctx, appstorebridge.Request{
		Method:      strings.ToUpper(strings.TrimSpace(*method)),
		Path:        strings.TrimSpace(*path),
		Params:      parseGoogleParams(params),
		RawBody:     rawBody,
		ContentType: strings.TrimSpace(*contentType),
	})
	if err != nil {
		printAppleAppStoreError(err)
		return
	}
	printAppleAppStoreResponse(resp, common.json(), common.rawEnabled())
}

func cmdAppleAppStoreApply(args []string) {
	fs, common := appleAppStoreCommonFlagSet("apple appstore apply", args, false)
	metadataDir := fs.String("metadata-dir", "appstore", "metadata directory")
	version := fs.String("version", "", "version string override")
	createVersion := fs.Bool("create-version", false, "create app store version when missing")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si apple appstore apply --bundle-id <id> [--metadata-dir appstore] [--version <version>] [--create-version] [--json]")
		return
	}
	runtime, client := common.mustClient()
	bundleID := firstNonEmpty(strings.TrimSpace(stringValue(common.bundleID)), strings.TrimSpace(runtime.BundleID))
	if bundleID == "" {
		fatal(fmt.Errorf("bundle id is required (--bundle-id or configured default_bundle_id)"))
	}
	appInfoByLocale, versionByLocale, versionMeta, err := loadAppleAppStoreMetadataBundle(*metadataDir)
	if err != nil {
		fatal(err)
	}
	if len(appInfoByLocale) == 0 && len(versionByLocale) == 0 {
		fatal(fmt.Errorf("no listing metadata found in %s", *metadataDir))
	}
	versionString := strings.TrimSpace(*version)
	if versionString == "" && versionMeta != nil {
		versionString = strings.TrimSpace(parseAppleAnyString(versionMeta["version"]))
	}
	if !*createVersion && versionMeta != nil {
		if value, ok := versionMeta["create_version"].(bool); ok {
			*createVersion = value
		}
	}
	printAppleAppStoreContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	summary := map[string]any{
		"bundle_id":            bundleID,
		"version":              versionString,
		"locales_applied":      0,
		"app_info_updated":     0,
		"version_info_updated": 0,
		"results":              []map[string]any{},
	}
	locales := map[string]struct{}{}
	for locale := range appInfoByLocale {
		locales[locale] = struct{}{}
	}
	for locale := range versionByLocale {
		locales[locale] = struct{}{}
	}
	orderedLocales := make([]string, 0, len(locales))
	for locale := range locales {
		orderedLocales = append(orderedLocales, locale)
	}
	sort.Strings(orderedLocales)
	for _, locale := range orderedLocales {
		appAttrs := map[string]any{}
		for k, v := range appInfoByLocale[locale] {
			appAttrs[k] = v
		}
		versionAttrs := map[string]any{}
		for k, v := range versionByLocale[locale] {
			versionAttrs[k] = v
		}
		mutation := appleListingMutation{
			BundleID:      bundleID,
			Locale:        locale,
			VersionString: versionString,
			CreateVersion: *createVersion,
			Platform:      runtime.Platform,
			AppInfoAttrs:  appAttrs,
			VersionAttrs:  versionAttrs,
		}
		result, err := appleApplyListingMutation(ctx, client, runtime, mutation)
		if err != nil {
			printAppleAppStoreError(err)
			return
		}
		summary["locales_applied"] = summary["locales_applied"].(int) + 1
		if updated, _ := result["app_info_updated"].(bool); updated {
			summary["app_info_updated"] = summary["app_info_updated"].(int) + 1
		}
		if updated, _ := result["version_info_updated"].(bool); updated {
			summary["version_info_updated"] = summary["version_info_updated"].(int) + 1
		}
		summary["results"] = append(summary["results"].([]map[string]any), result)
	}
	if common.json() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(summary); err != nil {
			fatal(err)
		}
		return
	}
	raw, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Println(string(raw))
}

func appleApplyListingMutation(ctx context.Context, client appleAppStoreBridgeClient, runtime appleAppStoreRuntimeContext, mutation appleListingMutation) (map[string]any, error) {
	bundleID := strings.TrimSpace(mutation.BundleID)
	if bundleID == "" {
		return nil, fmt.Errorf("bundle id is required")
	}
	locale := firstNonEmpty(strings.TrimSpace(mutation.Locale), strings.TrimSpace(runtime.Locale), "en-US")
	appResource, err := appleFindAppByBundleID(ctx, client, bundleID)
	if err != nil {
		return nil, err
	}
	if appResource == nil {
		return nil, fmt.Errorf("app not found for bundle id %s", bundleID)
	}
	appID := strings.TrimSpace(parseAppleAnyString(appResource["id"]))
	summary := map[string]any{
		"bundle_id":            bundleID,
		"app_id":               appID,
		"locale":               locale,
		"version":              strings.TrimSpace(mutation.VersionString),
		"app_info_updated":     false,
		"version_info_updated": false,
	}
	if len(mutation.AppInfoAttrs) > 0 {
		appInfoID, err := appleResolveAppInfoID(ctx, client, appID)
		if err != nil {
			return nil, err
		}
		localized, err := appleFindAppInfoLocalization(ctx, client, appInfoID, locale)
		if err != nil {
			return nil, err
		}
		if localized != nil {
			locID := strings.TrimSpace(parseAppleAnyString(localized["id"]))
			payload := map[string]any{
				"data": map[string]any{
					"id":         locID,
					"type":       "appInfoLocalizations",
					"attributes": mutation.AppInfoAttrs,
				},
			}
			if _, err := client.Do(ctx, appstorebridge.Request{Method: http.MethodPatch, Path: "/v1/appInfoLocalizations/" + url.PathEscape(locID), JSONBody: payload}); err != nil {
				return nil, err
			}
		} else {
			payload := map[string]any{
				"data": map[string]any{
					"type":       "appInfoLocalizations",
					"attributes": mergeStringMap(map[string]any{"locale": locale}, mutation.AppInfoAttrs),
					"relationships": map[string]any{
						"appInfo": map[string]any{"data": map[string]any{"type": "appInfos", "id": appInfoID}},
					},
				},
			}
			if _, err := client.Do(ctx, appstorebridge.Request{Method: http.MethodPost, Path: "/v1/appInfoLocalizations", JSONBody: payload}); err != nil {
				return nil, err
			}
		}
		summary["app_info_updated"] = true
	}
	if len(mutation.VersionAttrs) > 0 || strings.TrimSpace(mutation.VersionString) != "" {
		versionID, err := appleResolveAppStoreVersionID(ctx, client, appID, firstNonEmpty(mutation.Platform, runtime.Platform, "IOS"), strings.TrimSpace(mutation.VersionString), mutation.CreateVersion)
		if err != nil {
			return nil, err
		}
		if versionID != "" && len(mutation.VersionAttrs) > 0 {
			localized, err := appleFindAppStoreVersionLocalization(ctx, client, versionID, locale)
			if err != nil {
				return nil, err
			}
			if localized != nil {
				locID := strings.TrimSpace(parseAppleAnyString(localized["id"]))
				payload := map[string]any{"data": map[string]any{"id": locID, "type": "appStoreVersionLocalizations", "attributes": mutation.VersionAttrs}}
				if _, err := client.Do(ctx, appstorebridge.Request{Method: http.MethodPatch, Path: "/v1/appStoreVersionLocalizations/" + url.PathEscape(locID), JSONBody: payload}); err != nil {
					return nil, err
				}
			} else {
				payload := map[string]any{
					"data": map[string]any{
						"type":       "appStoreVersionLocalizations",
						"attributes": mergeStringMap(map[string]any{"locale": locale}, mutation.VersionAttrs),
						"relationships": map[string]any{
							"appStoreVersion": map[string]any{"data": map[string]any{"type": "appStoreVersions", "id": versionID}},
						},
					},
				}
				if _, err := client.Do(ctx, appstorebridge.Request{Method: http.MethodPost, Path: "/v1/appStoreVersionLocalizations", JSONBody: payload}); err != nil {
					return nil, err
				}
			}
			summary["version_info_updated"] = true
			summary["version_id"] = versionID
		}
	}
	return summary, nil
}

func appleFindAppByBundleID(ctx context.Context, client appleAppStoreBridgeClient, bundleID string) (map[string]any, error) {
	bundleID = strings.TrimSpace(bundleID)
	if bundleID == "" {
		return nil, fmt.Errorf("bundle id is required")
	}
	resp, err := client.Do(ctx, appstorebridge.Request{
		Method: http.MethodGet,
		Path:   "/v1/apps",
		Params: map[string]string{
			"filter[bundleId]": bundleID,
			"limit":            "1",
		},
	})
	if err != nil {
		return nil, err
	}
	return appleFirstDataResource(resp), nil
}

func appleFindBundleIDResource(ctx context.Context, client appleAppStoreBridgeClient, identifier string) (map[string]any, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("bundle id identifier is required")
	}
	resp, err := client.Do(ctx, appstorebridge.Request{Method: http.MethodGet, Path: "/v1/bundleIds", Params: map[string]string{"filter[identifier]": identifier, "limit": "1"}})
	if err != nil {
		return nil, err
	}
	return appleFirstDataResource(resp), nil
}

func appleResolveAppInfoID(ctx context.Context, client appleAppStoreBridgeClient, appID string) (string, error) {
	resp, err := client.Do(ctx, appstorebridge.Request{Method: http.MethodGet, Path: fmt.Sprintf("/v1/apps/%s/appInfos", url.PathEscape(appID)), Params: map[string]string{"limit": "1"}})
	if err != nil {
		return "", err
	}
	resource := appleFirstDataResource(resp)
	if resource == nil {
		return "", fmt.Errorf("app info not found for app %s", appID)
	}
	return strings.TrimSpace(parseAppleAnyString(resource["id"])), nil
}

func appleFindAppInfoLocalization(ctx context.Context, client appleAppStoreBridgeClient, appInfoID string, locale string) (map[string]any, error) {
	resp, err := client.Do(ctx, appstorebridge.Request{Method: http.MethodGet, Path: fmt.Sprintf("/v1/appInfos/%s/appInfoLocalizations", url.PathEscape(appInfoID)), Params: map[string]string{"filter[locale]": locale, "limit": "1"}})
	if err != nil {
		return nil, err
	}
	return appleFirstDataResource(resp), nil
}

func appleResolveAppStoreVersionID(ctx context.Context, client appleAppStoreBridgeClient, appID string, platform string, versionString string, createIfMissing bool) (string, error) {
	params := map[string]string{"limit": "1", "filter[platform]": platform}
	if strings.TrimSpace(versionString) != "" {
		params["filter[versionString]"] = strings.TrimSpace(versionString)
	}
	resp, err := client.Do(ctx, appstorebridge.Request{Method: http.MethodGet, Path: fmt.Sprintf("/v1/apps/%s/appStoreVersions", url.PathEscape(appID)), Params: params})
	if err != nil {
		return "", err
	}
	resource := appleFirstDataResource(resp)
	if resource != nil {
		return strings.TrimSpace(parseAppleAnyString(resource["id"])), nil
	}
	if !createIfMissing || strings.TrimSpace(versionString) == "" {
		if strings.TrimSpace(versionString) == "" {
			return "", fmt.Errorf("app store version not found; provide --version and --create-version")
		}
		return "", fmt.Errorf("app store version %s not found (set --create-version to create)", versionString)
	}
	payload := map[string]any{
		"data": map[string]any{
			"type": "appStoreVersions",
			"attributes": map[string]any{
				"platform":      platform,
				"versionString": strings.TrimSpace(versionString),
			},
			"relationships": map[string]any{
				"app": map[string]any{
					"data": map[string]any{"type": "apps", "id": appID},
				},
			},
		},
	}
	createResp, err := client.Do(ctx, appstorebridge.Request{Method: http.MethodPost, Path: "/v1/appStoreVersions", JSONBody: payload})
	if err != nil {
		return "", err
	}
	created := appleFirstDataResource(createResp)
	if created == nil {
		return "", fmt.Errorf("create app store version returned empty resource")
	}
	return strings.TrimSpace(parseAppleAnyString(created["id"])), nil
}

func appleFindAppStoreVersionLocalization(ctx context.Context, client appleAppStoreBridgeClient, versionID string, locale string) (map[string]any, error) {
	resp, err := client.Do(ctx, appstorebridge.Request{Method: http.MethodGet, Path: fmt.Sprintf("/v1/appStoreVersions/%s/appStoreVersionLocalizations", url.PathEscape(versionID)), Params: map[string]string{"filter[locale]": locale, "limit": "1"}})
	if err != nil {
		return nil, err
	}
	return appleFirstDataResource(resp), nil
}

func appleFirstDataResource(resp appstorebridge.Response) map[string]any {
	if len(resp.List) > 0 {
		return resp.List[0]
	}
	if resp.Data == nil {
		return nil
	}
	if dataObj, ok := resp.Data["data"].(map[string]any); ok {
		return dataObj
	}
	if dataList, ok := resp.Data["data"].([]any); ok {
		for _, item := range dataList {
			if obj, ok := item.(map[string]any); ok {
				return obj
			}
		}
	}
	return nil
}

func mergeStringMap(base map[string]any, overrides map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overrides {
		out[k] = v
	}
	return out
}
