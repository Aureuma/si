package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"si/tools/si/internal/googleplaybridge"
)

type googlePlayEditOptions struct {
	Validate                bool
	ChangesNotSentForReview bool
	AutoCommit              bool
}

func googlePlayRunEdit(ctx context.Context, client googlePlayBridgeClient, packageName string, opts googlePlayEditOptions, fn func(editID string) error) (googleplaybridge.Response, error) {
	insertResp, err := client.Do(ctx, googleplaybridge.Request{
		Method:   http.MethodPost,
		Path:     fmt.Sprintf("/androidpublisher/v3/applications/%s/edits", url.PathEscape(packageName)),
		JSONBody: map[string]any{},
	})
	if err != nil {
		return googleplaybridge.Response{}, err
	}
	editID := strings.TrimSpace(anyToString(insertResp.Data["id"]))
	if editID == "" {
		return googleplaybridge.Response{}, fmt.Errorf("edits.insert response missing id")
	}
	if err := fn(editID); err != nil {
		_, _ = client.Do(ctx, googleplaybridge.Request{Method: http.MethodDelete, Path: fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s", url.PathEscape(packageName), url.PathEscape(editID))})
		return googleplaybridge.Response{}, err
	}
	if !opts.AutoCommit {
		_, _ = client.Do(ctx, googleplaybridge.Request{Method: http.MethodDelete, Path: fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s", url.PathEscape(packageName), url.PathEscape(editID))})
		return googleplaybridge.Response{StatusCode: 200, Status: "200 OK", Data: map[string]any{"editId": editID, "deleted": true}}, nil
	}
	if opts.Validate {
		return client.Do(ctx, googleplaybridge.Request{
			Method: http.MethodPost,
			Path:   fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s:validate", url.PathEscape(packageName), url.PathEscape(editID)),
		})
	}
	params := map[string]string{}
	if opts.ChangesNotSentForReview {
		params["changesNotSentForReview"] = "true"
	}
	return client.Do(ctx, googleplaybridge.Request{
		Method: http.MethodPost,
		Path:   fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s:commit", url.PathEscape(packageName), url.PathEscape(editID)),
		Params: params,
	})
}

func cmdGooglePlayApp(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google play app create --title <name> [--developer-account <id>] [--language <code>] [--organization <org>] [--json]")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "create":
		cmdGooglePlayAppCreate(rest)
	default:
		printUnknown("google play app", sub)
		printUsage("usage: si google play app create --title <name> [--developer-account <id>] [--language <code>] [--organization <org>] [--json]")
	}
}

func cmdGooglePlayAppCreate(args []string) {
	fs, common := googlePlayCommonFlagSet("google play app create", args, false)
	title := fs.String("title", "", "app title")
	organizations := multiFlag{}
	fs.Var(&organizations, "organization", "organization id to grant access (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*title) == "" {
		printUsage("usage: si google play app create --title <name> [--developer-account <id>] [--language <code>] [--organization <org>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	accountID := strings.TrimSpace(stringValue(common.developerAccount))
	if accountID == "" {
		accountID = strings.TrimSpace(runtime.DeveloperAccountID)
	}
	if accountID == "" {
		fatal(fmt.Errorf("developer account id is required (set --developer-account or configure google.play.accounts.<alias>.developer_account_id)"))
	}
	lang := strings.TrimSpace(stringValue(common.language))
	if lang == "" {
		lang = resolveGooglePlayLanguage(runtime, "")
	}
	body := map[string]any{
		"title":        strings.TrimSpace(*title),
		"languageCode": lang,
	}
	orgs := parseGoogleCSVList(strings.Join(organizations, ","))
	if len(orgs) > 0 {
		body["organizations"] = orgs
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, googleplaybridge.Request{
		Method:           http.MethodPost,
		Path:             fmt.Sprintf("/playcustomapp/v1/accounts/%s/customApps", url.PathEscape(accountID)),
		JSONBody:         body,
		UseCustomAppBase: true,
	})
	if err != nil {
		printGooglePlayError(err)
		return
	}
	printGooglePlayResponse(resp, common.json(), common.rawEnabled())
}

func cmdGooglePlayListing(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google play listing <get|list|update>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "get":
		cmdGooglePlayListingGet(rest)
	case "list":
		cmdGooglePlayListingList(rest)
	case "update", "set", "patch":
		cmdGooglePlayListingUpdate(rest)
	default:
		printUnknown("google play listing", sub)
		printUsage("usage: si google play listing <get|list|update>")
	}
}

func cmdGooglePlayListingGet(args []string) {
	fs, common := googlePlayCommonFlagSet("google play listing get", args, true)
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google play listing get --package <name> [--language <code>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	packageName, err := resolveGooglePlayPackage(runtime, stringValue(common.packageName))
	if err != nil {
		fatal(err)
	}
	lang := resolveGooglePlayLanguage(runtime, strings.TrimSpace(stringValue(common.language)))
	printGooglePlayContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var resp googleplaybridge.Response
	_, err = googlePlayRunEdit(ctx, client, packageName, googlePlayEditOptions{AutoCommit: false}, func(editID string) error {
		path := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s/listings/%s", url.PathEscape(packageName), url.PathEscape(editID), url.PathEscape(lang))
		result, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodGet, Path: path})
		if err != nil {
			return err
		}
		resp = result
		return nil
	})
	if err != nil {
		printGooglePlayError(err)
		return
	}
	printGooglePlayResponse(resp, common.json(), common.rawEnabled())
}

func cmdGooglePlayListingList(args []string) {
	fs, common := googlePlayCommonFlagSet("google play listing list", args, true)
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google play listing list --package <name> [--json]")
		return
	}
	runtime, client := common.mustClient()
	packageName, err := resolveGooglePlayPackage(runtime, stringValue(common.packageName))
	if err != nil {
		fatal(err)
	}
	printGooglePlayContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var resp googleplaybridge.Response
	_, err = googlePlayRunEdit(ctx, client, packageName, googlePlayEditOptions{AutoCommit: false}, func(editID string) error {
		path := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s/listings", url.PathEscape(packageName), url.PathEscape(editID))
		result, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodGet, Path: path})
		if err != nil {
			return err
		}
		resp = result
		return nil
	})
	if err != nil {
		printGooglePlayError(err)
		return
	}
	printGooglePlayResponse(resp, common.json(), common.rawEnabled())
}

func cmdGooglePlayListingUpdate(args []string) {
	fs, common := googlePlayCommonFlagSet("google play listing update", args, false)
	title := fs.String("title", "", "listing title")
	shortDescription := fs.String("short-description", "", "short description")
	fullDescription := fs.String("full-description", "", "full description")
	video := fs.String("video", "", "video url")
	bodyRaw := fs.String("body", "", "json body or @file")
	validate := fs.Bool("validate", false, "validate edit instead of commit")
	changesNotSent := fs.Bool("changes-not-sent-for-review", false, "commit with changesNotSentForReview=true")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google play listing update --package <name> [--language <code>] [--title <text>] [--short-description <text>] [--full-description <text>] [--video <url>] [--body @listing.json]")
		return
	}
	runtime, client := common.mustClient()
	packageName, err := resolveGooglePlayPackage(runtime, stringValue(common.packageName))
	if err != nil {
		fatal(err)
	}
	lang := resolveGooglePlayLanguage(runtime, strings.TrimSpace(stringValue(common.language)))
	payload, err := parseGooglePlayJSONBody(*bodyRaw)
	if err != nil {
		fatal(err)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if strings.TrimSpace(*title) != "" {
		payload["title"] = strings.TrimSpace(*title)
	}
	if strings.TrimSpace(*shortDescription) != "" {
		payload["shortDescription"] = strings.TrimSpace(*shortDescription)
	}
	if strings.TrimSpace(*fullDescription) != "" {
		payload["fullDescription"] = strings.TrimSpace(*fullDescription)
	}
	if strings.TrimSpace(*video) != "" {
		payload["video"] = strings.TrimSpace(*video)
	}
	if len(payload) == 0 {
		fatal(fmt.Errorf("no listing fields provided"))
	}
	printGooglePlayContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	commitResp, err := googlePlayRunEdit(ctx, client, packageName, googlePlayEditOptions{AutoCommit: true, Validate: *validate, ChangesNotSentForReview: *changesNotSent}, func(editID string) error {
		path := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s/listings/%s", url.PathEscape(packageName), url.PathEscape(editID), url.PathEscape(lang))
		_, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodPatch, Path: path, JSONBody: payload})
		return err
	})
	if err != nil {
		printGooglePlayError(err)
		return
	}
	printGooglePlayResponse(commitResp, common.json(), common.rawEnabled())
}

func cmdGooglePlayDetails(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google play details <get|update>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "get":
		cmdGooglePlayDetailsGet(rest)
	case "update", "set", "patch":
		cmdGooglePlayDetailsUpdate(rest)
	default:
		printUnknown("google play details", sub)
		printUsage("usage: si google play details <get|update>")
	}
}

func cmdGooglePlayDetailsGet(args []string) {
	fs, common := googlePlayCommonFlagSet("google play details get", args, true)
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google play details get --package <name> [--json]")
		return
	}
	runtime, client := common.mustClient()
	packageName, err := resolveGooglePlayPackage(runtime, stringValue(common.packageName))
	if err != nil {
		fatal(err)
	}
	printGooglePlayContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var resp googleplaybridge.Response
	_, err = googlePlayRunEdit(ctx, client, packageName, googlePlayEditOptions{AutoCommit: false}, func(editID string) error {
		path := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s/details", url.PathEscape(packageName), url.PathEscape(editID))
		result, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodGet, Path: path})
		if err != nil {
			return err
		}
		resp = result
		return nil
	})
	if err != nil {
		printGooglePlayError(err)
		return
	}
	printGooglePlayResponse(resp, common.json(), common.rawEnabled())
}

func cmdGooglePlayDetailsUpdate(args []string) {
	fs, common := googlePlayCommonFlagSet("google play details update", args, false)
	contactEmail := fs.String("contact-email", "", "support email")
	contactPhone := fs.String("contact-phone", "", "support phone")
	contactWebsite := fs.String("contact-website", "", "support website")
	defaultLanguage := fs.String("default-language", "", "default language code")
	bodyRaw := fs.String("body", "", "json body or @file")
	validate := fs.Bool("validate", false, "validate edit instead of commit")
	changesNotSent := fs.Bool("changes-not-sent-for-review", false, "commit with changesNotSentForReview=true")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google play details update --package <name> [--contact-email <email>] [--contact-phone <phone>] [--contact-website <url>] [--default-language <code>] [--body @details.json]")
		return
	}
	runtime, client := common.mustClient()
	packageName, err := resolveGooglePlayPackage(runtime, stringValue(common.packageName))
	if err != nil {
		fatal(err)
	}
	payload, err := parseGooglePlayJSONBody(*bodyRaw)
	if err != nil {
		fatal(err)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if strings.TrimSpace(*contactEmail) != "" {
		payload["contactEmail"] = strings.TrimSpace(*contactEmail)
	}
	if strings.TrimSpace(*contactPhone) != "" {
		payload["contactPhone"] = strings.TrimSpace(*contactPhone)
	}
	if strings.TrimSpace(*contactWebsite) != "" {
		payload["contactWebsite"] = strings.TrimSpace(*contactWebsite)
	}
	if strings.TrimSpace(*defaultLanguage) != "" {
		payload["defaultLanguage"] = strings.TrimSpace(*defaultLanguage)
	}
	if len(payload) == 0 {
		fatal(fmt.Errorf("no app details fields provided"))
	}
	printGooglePlayContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	commitResp, err := googlePlayRunEdit(ctx, client, packageName, googlePlayEditOptions{AutoCommit: true, Validate: *validate, ChangesNotSentForReview: *changesNotSent}, func(editID string) error {
		path := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s/details", url.PathEscape(packageName), url.PathEscape(editID))
		_, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodPatch, Path: path, JSONBody: payload})
		return err
	})
	if err != nil {
		printGooglePlayError(err)
		return
	}
	printGooglePlayResponse(commitResp, common.json(), common.rawEnabled())
}

func cmdGooglePlayAsset(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google play asset <list|upload|clear>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGooglePlayAssetList(rest)
	case "upload":
		cmdGooglePlayAssetUpload(rest)
	case "clear", "deleteall":
		cmdGooglePlayAssetClear(rest)
	default:
		printUnknown("google play asset", sub)
		printUsage("usage: si google play asset <list|upload|clear>")
	}
}

func cmdGooglePlayAssetList(args []string) {
	fs, common := googlePlayCommonFlagSet("google play asset list", args, true)
	imageTypeRaw := fs.String("type", "", "image type (phoneScreenshots|sevenInchScreenshots|tenInchScreenshots|tvScreenshots|wearScreenshots|icon|featureGraphic|tvBanner)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*imageTypeRaw) == "" {
		printUsage("usage: si google play asset list --package <name> --type <image-type> [--language <code>] [--json]")
		return
	}
	imageType := normalizeGooglePlayImageType(*imageTypeRaw)
	if imageType == "" {
		fatal(fmt.Errorf("invalid image type %q", *imageTypeRaw))
	}
	runtime, client := common.mustClient()
	packageName, err := resolveGooglePlayPackage(runtime, stringValue(common.packageName))
	if err != nil {
		fatal(err)
	}
	lang := resolveGooglePlayLanguage(runtime, strings.TrimSpace(stringValue(common.language)))
	printGooglePlayContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var resp googleplaybridge.Response
	_, err = googlePlayRunEdit(ctx, client, packageName, googlePlayEditOptions{AutoCommit: false}, func(editID string) error {
		path := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s/listings/%s/%s", url.PathEscape(packageName), url.PathEscape(editID), url.PathEscape(lang), url.PathEscape(imageType))
		result, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodGet, Path: path})
		if err != nil {
			return err
		}
		resp = result
		return nil
	})
	if err != nil {
		printGooglePlayError(err)
		return
	}
	printGooglePlayResponse(resp, common.json(), common.rawEnabled())
}

func cmdGooglePlayAssetClear(args []string) {
	fs, common := googlePlayCommonFlagSet("google play asset clear", args, false)
	imageTypeRaw := fs.String("type", "", "image type")
	validate := fs.Bool("validate", false, "validate edit instead of commit")
	changesNotSent := fs.Bool("changes-not-sent-for-review", false, "commit with changesNotSentForReview=true")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*imageTypeRaw) == "" {
		printUsage("usage: si google play asset clear --package <name> --type <image-type> [--language <code>] [--validate]")
		return
	}
	imageType := normalizeGooglePlayImageType(*imageTypeRaw)
	if imageType == "" {
		fatal(fmt.Errorf("invalid image type %q", *imageTypeRaw))
	}
	runtime, client := common.mustClient()
	packageName, err := resolveGooglePlayPackage(runtime, stringValue(common.packageName))
	if err != nil {
		fatal(err)
	}
	lang := resolveGooglePlayLanguage(runtime, strings.TrimSpace(stringValue(common.language)))
	printGooglePlayContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	commitResp, err := googlePlayRunEdit(ctx, client, packageName, googlePlayEditOptions{AutoCommit: true, Validate: *validate, ChangesNotSentForReview: *changesNotSent}, func(editID string) error {
		path := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s/listings/%s/%s", url.PathEscape(packageName), url.PathEscape(editID), url.PathEscape(lang), url.PathEscape(imageType))
		_, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodDelete, Path: path})
		return err
	})
	if err != nil {
		printGooglePlayError(err)
		return
	}
	printGooglePlayResponse(commitResp, common.json(), common.rawEnabled())
}

func cmdGooglePlayAssetUpload(args []string) {
	fs, common := googlePlayCommonFlagSet("google play asset upload", args, false)
	imageTypeRaw := fs.String("type", "", "image type")
	clearFirst := fs.Bool("clear-first", false, "clear existing images for this type before upload")
	validate := fs.Bool("validate", false, "validate edit instead of commit")
	changesNotSent := fs.Bool("changes-not-sent-for-review", false, "commit with changesNotSentForReview=true")
	files := multiFlag{}
	fs.Var(&files, "file", "image file path (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*imageTypeRaw) == "" || len(files) == 0 {
		printUsage("usage: si google play asset upload --package <name> --type <image-type> --file <path> [--file <path>] [--language <code>] [--clear-first]")
		return
	}
	imageType := normalizeGooglePlayImageType(*imageTypeRaw)
	if imageType == "" {
		fatal(fmt.Errorf("invalid image type %q", *imageTypeRaw))
	}
	runtime, client := common.mustClient()
	packageName, err := resolveGooglePlayPackage(runtime, stringValue(common.packageName))
	if err != nil {
		fatal(err)
	}
	lang := resolveGooglePlayLanguage(runtime, strings.TrimSpace(stringValue(common.language)))
	filePaths := make([]string, 0, len(files))
	for _, file := range files {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		if _, err := os.Stat(file); err != nil {
			fatal(fmt.Errorf("invalid --file %q: %w", file, err))
		}
		filePaths = append(filePaths, file)
	}
	if len(filePaths) == 0 {
		fatal(fmt.Errorf("at least one valid --file is required"))
	}
	sort.Strings(filePaths)
	printGooglePlayContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	commitResp, err := googlePlayRunEdit(ctx, client, packageName, googlePlayEditOptions{AutoCommit: true, Validate: *validate, ChangesNotSentForReview: *changesNotSent}, func(editID string) error {
		basePath := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s/listings/%s/%s", url.PathEscape(packageName), url.PathEscape(editID), url.PathEscape(lang), url.PathEscape(imageType))
		if *clearFirst {
			if _, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodDelete, Path: basePath}); err != nil {
				return err
			}
		}
		for _, file := range filePaths {
			_, err := client.Do(ctx, googleplaybridge.Request{
				Method:      http.MethodPost,
				Path:        "/upload" + basePath,
				Params:      map[string]string{"uploadType": "media"},
				UseUpload:   true,
				MediaPath:   file,
				ContentType: detectContentTypeByPath(file, "image/*"),
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		printGooglePlayError(err)
		return
	}
	printGooglePlayResponse(commitResp, common.json(), common.rawEnabled())
}

func cmdGooglePlayRelease(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google play release <upload|status|promote|set-status>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "upload":
		cmdGooglePlayReleaseUpload(rest)
	case "status":
		cmdGooglePlayReleaseStatus(rest)
	case "promote":
		cmdGooglePlayReleasePromote(rest)
	case "set-status", "halt", "resume":
		cmdGooglePlayReleaseSetStatus(sub, rest)
	default:
		printUnknown("google play release", sub)
		printUsage("usage: si google play release <upload|status|promote|set-status>")
	}
}

func cmdGooglePlayReleaseUpload(args []string) {
	fs, common := googlePlayCommonFlagSet("google play release upload", args, false)
	aab := fs.String("aab", "", "path to Android App Bundle (.aab)")
	apk := fs.String("apk", "", "path to Android APK (.apk)")
	track := fs.String("track", "internal", "track name (internal|alpha|beta|production|custom)")
	statusRaw := fs.String("status", "", "release status (draft|inProgress|halted|completed)")
	userFraction := fs.Float64("user-fraction", 0, "rollout fraction (0.0-1.0 for inProgress)")
	releaseName := fs.String("release-name", "", "release name")
	inAppUpdatePriority := fs.Int("in-app-update-priority", -1, "in-app update priority (0-5)")
	validate := fs.Bool("validate", false, "validate edit instead of commit")
	changesNotSent := fs.Bool("changes-not-sent-for-review", false, "commit with changesNotSentForReview=true")
	versionCodesRaw := multiFlag{}
	releaseNotesRaw := multiFlag{}
	fs.Var(&versionCodesRaw, "version-code", "version code (repeatable or csv)")
	fs.Var(&releaseNotesRaw, "release-note", "release note entry <language>=<text> (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google play release upload --package <name> [--aab <file>|--apk <file>] [--track <track>] [--status <status>] [--user-fraction <0-1>] [--release-note <lang=text>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	packageName, err := resolveGooglePlayPackage(runtime, stringValue(common.packageName))
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(*aab) != "" && strings.TrimSpace(*apk) != "" {
		fatal(fmt.Errorf("use either --aab or --apk in a single release upload"))
	}
	if strings.TrimSpace(*aab) != "" {
		if _, err := os.Stat(strings.TrimSpace(*aab)); err != nil {
			fatal(err)
		}
	}
	if strings.TrimSpace(*apk) != "" {
		if _, err := os.Stat(strings.TrimSpace(*apk)); err != nil {
			fatal(err)
		}
	}
	versionCodes, err := parseGooglePlayVersionCodes(versionCodesRaw)
	if err != nil {
		fatal(err)
	}
	releaseNotes, err := parseGooglePlayReleaseNotes(releaseNotesRaw)
	if err != nil {
		fatal(err)
	}
	if strings.TrimSpace(*aab) == "" && strings.TrimSpace(*apk) == "" && len(versionCodes) == 0 {
		fatal(fmt.Errorf("provide --aab, --apk, or at least one --version-code"))
	}
	status := normalizeGooglePlayReleaseStatus(*statusRaw)
	if status == "" {
		if *userFraction > 0 {
			status = "inProgress"
		} else {
			status = "completed"
		}
	}
	if *userFraction < 0 || *userFraction > 1 {
		fatal(fmt.Errorf("--user-fraction must be between 0 and 1"))
	}
	if status != "inProgress" && *userFraction > 0 {
		warnf("ignoring --user-fraction because status=%s", status)
		*userFraction = 0
	}
	if *inAppUpdatePriority > 5 {
		fatal(fmt.Errorf("--in-app-update-priority must be between 0 and 5"))
	}
	printGooglePlayContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	var trackResp googleplaybridge.Response
	commitResp, err := googlePlayRunEdit(ctx, client, packageName, googlePlayEditOptions{AutoCommit: true, Validate: *validate, ChangesNotSentForReview: *changesNotSent}, func(editID string) error {
		codes := append([]int64{}, versionCodes...)
		if strings.TrimSpace(*aab) != "" {
			path := fmt.Sprintf("/upload/androidpublisher/v3/applications/%s/edits/%s/bundles", url.PathEscape(packageName), url.PathEscape(editID))
			uploadResp, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodPost, Path: path, Params: map[string]string{"uploadType": "media"}, UseUpload: true, MediaPath: strings.TrimSpace(*aab), ContentType: "application/octet-stream"})
			if err != nil {
				return err
			}
			if code := parseGooglePlayVersionCode(uploadResp.Data["versionCode"]); code > 0 {
				codes = append(codes, code)
			}
		}
		if strings.TrimSpace(*apk) != "" {
			path := fmt.Sprintf("/upload/androidpublisher/v3/applications/%s/edits/%s/apks", url.PathEscape(packageName), url.PathEscape(editID))
			uploadResp, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodPost, Path: path, Params: map[string]string{"uploadType": "media"}, UseUpload: true, MediaPath: strings.TrimSpace(*apk), ContentType: "application/vnd.android.package-archive"})
			if err != nil {
				return err
			}
			if code := parseGooglePlayVersionCode(uploadResp.Data["versionCode"]); code > 0 {
				codes = append(codes, code)
			}
		}
		codes = uniqueSortedVersionCodes(codes)
		if len(codes) == 0 {
			return fmt.Errorf("no version codes resolved for release")
		}
		release := map[string]any{
			"status":       status,
			"versionCodes": versionCodesToStrings(codes),
		}
		if *userFraction > 0 {
			release["userFraction"] = *userFraction
		}
		if strings.TrimSpace(*releaseName) != "" {
			release["name"] = strings.TrimSpace(*releaseName)
		}
		if len(releaseNotes) > 0 {
			release["releaseNotes"] = releaseNotes
		}
		if *inAppUpdatePriority >= 0 {
			release["inAppUpdatePriority"] = *inAppUpdatePriority
		}
		trackPayload := map[string]any{
			"track":    strings.TrimSpace(*track),
			"releases": []any{release},
		}
		path := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s/tracks/%s", url.PathEscape(packageName), url.PathEscape(editID), url.PathEscape(strings.TrimSpace(*track)))
		result, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodPut, Path: path, JSONBody: trackPayload})
		if err != nil {
			return err
		}
		trackResp = result
		return nil
	})
	if err != nil {
		printGooglePlayError(err)
		return
	}
	if common.json() {
		payload := map[string]any{
			"track_update": trackResp,
			"edit_result":  commitResp,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	printGooglePlayResponse(trackResp, false, false)
	printGooglePlayResponse(commitResp, false, false)
}

func cmdGooglePlayReleaseStatus(args []string) {
	fs, common := googlePlayCommonFlagSet("google play release status", args, true)
	track := fs.String("track", "", "optional track name")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google play release status --package <name> [--track <track>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	packageName, err := resolveGooglePlayPackage(runtime, stringValue(common.packageName))
	if err != nil {
		fatal(err)
	}
	printGooglePlayContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var resp googleplaybridge.Response
	_, err = googlePlayRunEdit(ctx, client, packageName, googlePlayEditOptions{AutoCommit: false}, func(editID string) error {
		path := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s/tracks", url.PathEscape(packageName), url.PathEscape(editID))
		if strings.TrimSpace(*track) != "" {
			path += "/" + url.PathEscape(strings.TrimSpace(*track))
		}
		result, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodGet, Path: path})
		if err != nil {
			return err
		}
		resp = result
		return nil
	})
	if err != nil {
		printGooglePlayError(err)
		return
	}
	printGooglePlayResponse(resp, common.json(), common.rawEnabled())
}

func cmdGooglePlayReleasePromote(args []string) {
	fs, common := googlePlayCommonFlagSet("google play release promote", args, false)
	fromTrack := fs.String("from", "internal", "source track")
	toTrack := fs.String("to", "production", "destination track")
	statusRaw := fs.String("status", "completed", "release status for destination")
	userFraction := fs.Float64("user-fraction", 0, "rollout fraction (0.0-1.0)")
	releaseName := fs.String("release-name", "", "release name")
	validate := fs.Bool("validate", false, "validate edit instead of commit")
	changesNotSent := fs.Bool("changes-not-sent-for-review", false, "commit with changesNotSentForReview=true")
	releaseNotesRaw := multiFlag{}
	fs.Var(&releaseNotesRaw, "release-note", "release note entry <language>=<text> (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google play release promote --package <name> [--from <track>] [--to <track>] [--status <status>] [--user-fraction <0-1>] [--release-note <lang=text>]")
		return
	}
	runtime, client := common.mustClient()
	packageName, err := resolveGooglePlayPackage(runtime, stringValue(common.packageName))
	if err != nil {
		fatal(err)
	}
	releaseNotes, err := parseGooglePlayReleaseNotes(releaseNotesRaw)
	if err != nil {
		fatal(err)
	}
	status := normalizeGooglePlayReleaseStatus(*statusRaw)
	if status == "" {
		status = "completed"
	}
	if *userFraction < 0 || *userFraction > 1 {
		fatal(fmt.Errorf("--user-fraction must be between 0 and 1"))
	}
	printGooglePlayContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	var trackResp googleplaybridge.Response
	commitResp, err := googlePlayRunEdit(ctx, client, packageName, googlePlayEditOptions{AutoCommit: true, Validate: *validate, ChangesNotSentForReview: *changesNotSent}, func(editID string) error {
		sourcePath := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s/tracks/%s", url.PathEscape(packageName), url.PathEscape(editID), url.PathEscape(strings.TrimSpace(*fromTrack)))
		sourceResp, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodGet, Path: sourcePath})
		if err != nil {
			return err
		}
		codes := extractGooglePlayTrackVersionCodes(sourceResp.Data)
		if len(codes) == 0 {
			return fmt.Errorf("source track %s has no releases/version codes", strings.TrimSpace(*fromTrack))
		}
		release := map[string]any{"status": status, "versionCodes": versionCodesToStrings(codes)}
		if *userFraction > 0 {
			release["userFraction"] = *userFraction
		}
		if strings.TrimSpace(*releaseName) != "" {
			release["name"] = strings.TrimSpace(*releaseName)
		}
		if len(releaseNotes) > 0 {
			release["releaseNotes"] = releaseNotes
		}
		targetPayload := map[string]any{"track": strings.TrimSpace(*toTrack), "releases": []any{release}}
		targetPath := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s/tracks/%s", url.PathEscape(packageName), url.PathEscape(editID), url.PathEscape(strings.TrimSpace(*toTrack)))
		result, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodPut, Path: targetPath, JSONBody: targetPayload})
		if err != nil {
			return err
		}
		trackResp = result
		return nil
	})
	if err != nil {
		printGooglePlayError(err)
		return
	}
	if common.json() {
		payload := map[string]any{"track_update": trackResp, "edit_result": commitResp}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	printGooglePlayResponse(trackResp, false, false)
	printGooglePlayResponse(commitResp, false, false)
}

func cmdGooglePlayReleaseSetStatus(mode string, args []string) {
	fs, common := googlePlayCommonFlagSet("google play release set-status", args, false)
	track := fs.String("track", "production", "track name")
	statusRaw := fs.String("status", "", "release status (draft|inProgress|halted|completed)")
	userFraction := fs.Float64("user-fraction", 0, "rollout fraction for inProgress status")
	validate := fs.Bool("validate", false, "validate edit instead of commit")
	changesNotSent := fs.Bool("changes-not-sent-for-review", false, "commit with changesNotSentForReview=true")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google play release set-status --package <name> --track <track> --status <status> [--user-fraction <0-1>]")
		return
	}
	runtime, client := common.mustClient()
	packageName, err := resolveGooglePlayPackage(runtime, stringValue(common.packageName))
	if err != nil {
		fatal(err)
	}
	status := normalizeGooglePlayReleaseStatus(*statusRaw)
	if status == "" {
		switch mode {
		case "halt":
			status = "halted"
		case "resume":
			status = "inProgress"
		default:
			fatal(fmt.Errorf("--status is required"))
		}
	}
	if *userFraction < 0 || *userFraction > 1 {
		fatal(fmt.Errorf("--user-fraction must be between 0 and 1"))
	}
	printGooglePlayContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var trackResp googleplaybridge.Response
	commitResp, err := googlePlayRunEdit(ctx, client, packageName, googlePlayEditOptions{AutoCommit: true, Validate: *validate, ChangesNotSentForReview: *changesNotSent}, func(editID string) error {
		path := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s/tracks/%s", url.PathEscape(packageName), url.PathEscape(editID), url.PathEscape(strings.TrimSpace(*track)))
		current, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodGet, Path: path})
		if err != nil {
			return err
		}
		releasesRaw, _ := current.Data["releases"].([]any)
		if len(releasesRaw) == 0 {
			return fmt.Errorf("track %s has no releases", strings.TrimSpace(*track))
		}
		releases := make([]any, 0, len(releasesRaw))
		for _, entry := range releasesRaw {
			release, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			release["status"] = status
			if status == "inProgress" && *userFraction > 0 {
				release["userFraction"] = *userFraction
			} else {
				delete(release, "userFraction")
			}
			releases = append(releases, release)
		}
		if len(releases) == 0 {
			return fmt.Errorf("track %s has no mutable releases", strings.TrimSpace(*track))
		}
		payload := map[string]any{"track": strings.TrimSpace(*track), "releases": releases}
		result, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodPut, Path: path, JSONBody: payload})
		if err != nil {
			return err
		}
		trackResp = result
		return nil
	})
	if err != nil {
		printGooglePlayError(err)
		return
	}
	if common.json() {
		payload := map[string]any{"track_update": trackResp, "edit_result": commitResp}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	printGooglePlayResponse(trackResp, false, false)
	printGooglePlayResponse(commitResp, false, false)
}

func cmdGooglePlayRaw(args []string) {
	fs, common := googlePlayCommonFlagSet("google play raw", args, true)
	method := fs.String("method", http.MethodGet, "http method")
	path := fs.String("path", "", "api path")
	body := fs.String("body", "", "raw request body or @file")
	contentType := fs.String("content-type", "", "request content type")
	useUpload := fs.Bool("upload", false, "use upload base url")
	useCustomApp := fs.Bool("custom-app", false, "use custom app base url")
	mediaFile := fs.String("media-file", "", "upload media file path")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*path) == "" {
		printUsage("usage: si google play raw --method <GET|POST|PUT|DELETE> --path <api-path> [--param key=value] [--body raw|@file] [--upload] [--custom-app] [--media-file <path>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	printGooglePlayContextBanner(runtime, common.json())
	rawBody, err := parseGooglePlayBody(*body)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	resp, err := client.Do(ctx, googleplaybridge.Request{
		Method:           strings.ToUpper(strings.TrimSpace(*method)),
		Path:             strings.TrimSpace(*path),
		Params:           parseGoogleParams(params),
		RawBody:          rawBody,
		ContentType:      strings.TrimSpace(*contentType),
		UseUpload:        *useUpload,
		UseCustomAppBase: *useCustomApp,
		MediaPath:        strings.TrimSpace(*mediaFile),
	})
	if err != nil {
		printGooglePlayError(err)
		return
	}
	printGooglePlayResponse(resp, common.json(), common.rawEnabled())
}

func cmdGooglePlayApply(args []string) {
	fs, common := googlePlayCommonFlagSet("google play apply", args, false)
	metadataDir := fs.String("metadata-dir", "play-store", "metadata directory")
	aab := fs.String("aab", "", "path to Android App Bundle (.aab)")
	apk := fs.String("apk", "", "path to Android APK (.apk)")
	track := fs.String("track", "", "release track for uploaded artifact")
	statusRaw := fs.String("status", "", "release status")
	userFraction := fs.Float64("user-fraction", 0, "rollout fraction (0.0-1.0)")
	releaseName := fs.String("release-name", "", "release name")
	validate := fs.Bool("validate", false, "validate edit instead of commit")
	changesNotSent := fs.Bool("changes-not-sent-for-review", false, "commit with changesNotSentForReview=true")
	releaseNotesRaw := multiFlag{}
	versionCodesRaw := multiFlag{}
	fs.Var(&releaseNotesRaw, "release-note", "release note entry <language>=<text> (repeatable)")
	fs.Var(&versionCodesRaw, "version-code", "version code (repeatable or csv)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google play apply --package <name> [--metadata-dir play-store] [--aab <file>|--apk <file>] [--track <track>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	packageName, err := resolveGooglePlayPackage(runtime, stringValue(common.packageName))
	if err != nil {
		fatal(err)
	}
	versionCodes, err := parseGooglePlayVersionCodes(versionCodesRaw)
	if err != nil {
		fatal(err)
	}
	releaseNotes, err := parseGooglePlayReleaseNotes(releaseNotesRaw)
	if err != nil {
		fatal(err)
	}
	metadataPath := strings.TrimSpace(*metadataDir)
	detailsPayload, listingPayloads, imageUploads, err := loadGooglePlayMetadataBundle(metadataPath)
	if err != nil {
		fatal(err)
	}
	artifactTrack := strings.TrimSpace(*track)
	if artifactTrack == "" && (strings.TrimSpace(*aab) != "" || strings.TrimSpace(*apk) != "") {
		artifactTrack = "internal"
	}
	if *userFraction < 0 || *userFraction > 1 {
		fatal(fmt.Errorf("--user-fraction must be between 0 and 1"))
	}
	status := normalizeGooglePlayReleaseStatus(*statusRaw)
	if status == "" && artifactTrack != "" {
		if *userFraction > 0 {
			status = "inProgress"
		} else {
			status = "completed"
		}
	}
	printGooglePlayContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	var summary map[string]any
	commitResp, err := googlePlayRunEdit(ctx, client, packageName, googlePlayEditOptions{AutoCommit: true, Validate: *validate, ChangesNotSentForReview: *changesNotSent}, func(editID string) error {
		summary = map[string]any{"details_updated": false, "listings_updated": 0, "images_uploaded": 0, "track_updated": false}
		if len(detailsPayload) > 0 {
			path := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s/details", url.PathEscape(packageName), url.PathEscape(editID))
			if _, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodPatch, Path: path, JSONBody: detailsPayload}); err != nil {
				return err
			}
			summary["details_updated"] = true
		}
		if len(listingPayloads) > 0 {
			langs := make([]string, 0, len(listingPayloads))
			for lang := range listingPayloads {
				langs = append(langs, lang)
			}
			sort.Strings(langs)
			for _, lang := range langs {
				path := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s/listings/%s", url.PathEscape(packageName), url.PathEscape(editID), url.PathEscape(lang))
				if _, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodPatch, Path: path, JSONBody: listingPayloads[lang]}); err != nil {
					return err
				}
			}
			summary["listings_updated"] = len(langs)
		}
		if len(imageUploads) > 0 {
			uploaded := 0
			langs := make([]string, 0, len(imageUploads))
			for lang := range imageUploads {
				langs = append(langs, lang)
			}
			sort.Strings(langs)
			for _, lang := range langs {
				types := make([]string, 0, len(imageUploads[lang]))
				for imageType := range imageUploads[lang] {
					types = append(types, imageType)
				}
				sort.Strings(types)
				for _, imageType := range types {
					basePath := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s/listings/%s/%s", url.PathEscape(packageName), url.PathEscape(editID), url.PathEscape(lang), url.PathEscape(imageType))
					if _, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodDelete, Path: basePath}); err != nil {
						return err
					}
					for _, file := range imageUploads[lang][imageType] {
						if _, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodPost, Path: "/upload" + basePath, Params: map[string]string{"uploadType": "media"}, UseUpload: true, MediaPath: file, ContentType: detectContentTypeByPath(file, "image/*")}); err != nil {
							return err
						}
						uploaded++
					}
				}
			}
			summary["images_uploaded"] = uploaded
		}
		if artifactTrack != "" {
			codes := append([]int64{}, versionCodes...)
			if strings.TrimSpace(*aab) != "" {
				path := fmt.Sprintf("/upload/androidpublisher/v3/applications/%s/edits/%s/bundles", url.PathEscape(packageName), url.PathEscape(editID))
				uploadResp, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodPost, Path: path, Params: map[string]string{"uploadType": "media"}, UseUpload: true, MediaPath: strings.TrimSpace(*aab), ContentType: "application/octet-stream"})
				if err != nil {
					return err
				}
				if code := parseGooglePlayVersionCode(uploadResp.Data["versionCode"]); code > 0 {
					codes = append(codes, code)
				}
			}
			if strings.TrimSpace(*apk) != "" {
				path := fmt.Sprintf("/upload/androidpublisher/v3/applications/%s/edits/%s/apks", url.PathEscape(packageName), url.PathEscape(editID))
				uploadResp, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodPost, Path: path, Params: map[string]string{"uploadType": "media"}, UseUpload: true, MediaPath: strings.TrimSpace(*apk), ContentType: "application/vnd.android.package-archive"})
				if err != nil {
					return err
				}
				if code := parseGooglePlayVersionCode(uploadResp.Data["versionCode"]); code > 0 {
					codes = append(codes, code)
				}
			}
			codes = uniqueSortedVersionCodes(codes)
			if len(codes) == 0 {
				return fmt.Errorf("artifact track requested but no version codes resolved")
			}
			release := map[string]any{"status": firstNonEmpty(status, "completed"), "versionCodes": versionCodesToStrings(codes)}
			if *userFraction > 0 {
				release["userFraction"] = *userFraction
			}
			if strings.TrimSpace(*releaseName) != "" {
				release["name"] = strings.TrimSpace(*releaseName)
			}
			if len(releaseNotes) > 0 {
				release["releaseNotes"] = releaseNotes
			}
			path := fmt.Sprintf("/androidpublisher/v3/applications/%s/edits/%s/tracks/%s", url.PathEscape(packageName), url.PathEscape(editID), url.PathEscape(artifactTrack))
			payload := map[string]any{"track": artifactTrack, "releases": []any{release}}
			if _, err := client.Do(ctx, googleplaybridge.Request{Method: http.MethodPut, Path: path, JSONBody: payload}); err != nil {
				return err
			}
			summary["track_updated"] = true
			summary["track"] = artifactTrack
			summary["version_codes"] = versionCodesToStrings(codes)
		}
		if !summary["details_updated"].(bool) && summary["listings_updated"].(int) == 0 && summary["images_uploaded"].(int) == 0 && !summary["track_updated"].(bool) {
			return fmt.Errorf("no changes detected in metadata/apply inputs")
		}
		return nil
	})
	if err != nil {
		printGooglePlayError(err)
		return
	}
	if common.json() {
		payload := map[string]any{"summary": summary, "edit_result": commitResp}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s details_updated=%v listings_updated=%d images_uploaded=%d track_updated=%v\n",
		styleHeading("Apply summary:"),
		summary["details_updated"],
		summary["listings_updated"],
		summary["images_uploaded"],
		summary["track_updated"],
	)
	printGooglePlayResponse(commitResp, false, false)
}

func loadGooglePlayMetadataBundle(metadataDir string) (map[string]any, map[string]map[string]any, map[string]map[string][]string, error) {
	metadataDir = strings.TrimSpace(metadataDir)
	if metadataDir == "" {
		metadataDir = "play-store"
	}
	info, err := os.Stat(metadataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil, nil
		}
		return nil, nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, nil, fmt.Errorf("metadata dir is not a directory: %s", metadataDir)
	}
	details := map[string]any{}
	detailsPath := filepath.Join(metadataDir, "details.json")
	if _, err := os.Stat(detailsPath); err == nil {
		raw, err := readLocalFile(detailsPath)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := json.Unmarshal(raw, &details); err != nil {
			return nil, nil, nil, fmt.Errorf("parse %s: %w", detailsPath, err)
		}
	}
	listings := map[string]map[string]any{}
	listingsDir := filepath.Join(metadataDir, "listings")
	if stat, err := os.Stat(listingsDir); err == nil && stat.IsDir() {
		entries, err := os.ReadDir(listingsDir)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := strings.ToLower(strings.TrimSpace(entry.Name()))
			if !(strings.HasSuffix(name, ".json")) {
				continue
			}
			path := filepath.Join(listingsDir, entry.Name())
			raw, err := readLocalFile(path)
			if err != nil {
				return nil, nil, nil, err
			}
			payload := map[string]any{}
			if err := json.Unmarshal(raw, &payload); err != nil {
				return nil, nil, nil, fmt.Errorf("parse %s: %w", path, err)
			}
			lang := strings.TrimSpace(anyToString(payload["language"]))
			if lang == "" {
				lang = strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			}
			lang = strings.TrimSpace(lang)
			if lang == "" {
				return nil, nil, nil, fmt.Errorf("listing file %s missing language", path)
			}
			delete(payload, "language")
			listings[lang] = payload
		}
	}
	images, err := collectGooglePlayImageUploads(filepath.Join(metadataDir, "images"))
	if err != nil {
		return nil, nil, nil, err
	}
	if len(details) == 0 {
		details = nil
	}
	if len(listings) == 0 {
		listings = nil
	}
	if len(images) == 0 {
		images = nil
	}
	return details, listings, images, nil
}

func parseGooglePlayVersionCode(value any) int64 {
	s := strings.TrimSpace(anyToString(value))
	if s == "" {
		return 0
	}
	parsed, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	if parsed <= 0 {
		return 0
	}
	return parsed
}

func uniqueSortedVersionCodes(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}
	seen := map[int64]struct{}{}
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func versionCodesToStrings(values []int64) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strconv.FormatInt(value, 10))
	}
	return out
}

func extractGooglePlayTrackVersionCodes(track map[string]any) []int64 {
	if len(track) == 0 {
		return nil
	}
	releasesRaw, _ := track["releases"].([]any)
	codes := make([]int64, 0, 16)
	for _, entry := range releasesRaw {
		release, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		codesRaw, _ := release["versionCodes"].([]any)
		for _, codeRaw := range codesRaw {
			if code := parseGooglePlayVersionCode(codeRaw); code > 0 {
				codes = append(codes, code)
			}
		}
	}
	return uniqueSortedVersionCodes(codes)
}
