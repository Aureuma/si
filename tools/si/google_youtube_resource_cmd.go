package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"si/tools/si/internal/apibridge"
	"si/tools/si/internal/youtubebridge"
)

func cmdGoogleYouTubeSearch(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google youtube search <list>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "list":
		cmdGoogleYouTubeSearchList(args[1:])
	default:
		printUnknown("google youtube search", sub)
		printUsage("usage: si google youtube search <list>")
	}
}

func cmdGoogleYouTubeSearchList(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube search list", args, true)
	part := fs.String("part", "id,snippet", "resource parts")
	query := fs.String("query", "", "search query")
	searchType := fs.String("type", "video", "search type (video|channel|playlist)")
	maxResults := fs.Int("max-results", 10, "max results")
	pageToken := fs.String("page-token", "", "page token")
	order := fs.String("order", "", "order (date|rating|relevance|title|videoCount|viewCount)")
	channelID := fs.String("channel-id", "", "channel id filter")
	publishedAfter := fs.String("published-after", "", "publishedAfter RFC3339")
	publishedBefore := fs.String("published-before", "", "publishedBefore RFC3339")
	allPages := fs.Bool("all", false, "fetch all pages")
	maxPages := fs.Int("max-pages", 5, "maximum pages when --all")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*query) == "" {
		printUsage("usage: si google youtube search list --query <text> [--type <video|channel|playlist>] [--max-results <n>] [--all] [--json]")
		return
	}
	runtime, client := common.mustClient()
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "id,snippet")
	q["q"] = strings.TrimSpace(*query)
	q["maxResults"] = strconv.Itoa(clampInt(*maxResults, 1, 50))
	if value := strings.TrimSpace(*searchType); value != "" {
		q["type"] = value
	}
	if value := strings.TrimSpace(*pageToken); value != "" {
		q["pageToken"] = value
	}
	if value := strings.TrimSpace(*order); value != "" {
		q["order"] = value
	}
	if value := strings.TrimSpace(*channelID); value != "" {
		q["channelId"] = value
	}
	if value := strings.TrimSpace(*publishedAfter); value != "" {
		q["publishedAfter"] = value
	}
	if value := strings.TrimSpace(*publishedBefore); value != "" {
		q["publishedBefore"] = value
	}
	if runtime.LanguageCode != "" {
		q["relevanceLanguage"] = runtime.LanguageCode
	}
	if runtime.RegionCode != "" {
		q["regionCode"] = runtime.RegionCode
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if *allPages {
		items, err := client.ListAll(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/search", Params: q}, *maxPages)
		if err != nil {
			printGoogleYouTubeError(err)
			return
		}
		payload := map[string]any{"operation": "search.list", "count": len(items), "items": items}
		if common.json() {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(payload); err != nil {
				fatal(err)
			}
			return
		}
		fmt.Printf("%s %s (%d)\n", styleHeading("YouTube search.list:"), styleSuccess("ok"), len(items))
		for _, item := range items {
			fmt.Printf("  %s\n", summarizeGoogleYouTubeItem(item))
		}
		return
	}
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/search", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeSupport(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google youtube support <languages|regions|categories>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "languages", "language":
		cmdGoogleYouTubeSupportLanguages(rest)
	case "regions", "region":
		cmdGoogleYouTubeSupportRegions(rest)
	case "categories", "category", "video-categories":
		cmdGoogleYouTubeSupportCategories(rest)
	default:
		printUnknown("google youtube support", sub)
		printUsage("usage: si google youtube support <languages|regions|categories>")
	}
}

func cmdGoogleYouTubeSupportLanguages(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube support languages", args, true)
	part := fs.String("part", "snippet", "resource parts")
	hl := fs.String("hl", "", "language override")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google youtube support languages [--hl <language>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "snippet")
	q["hl"] = firstNonEmpty(strings.TrimSpace(*hl), runtime.LanguageCode, "en_US")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/i18nLanguages", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeSupportRegions(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube support regions", args, true)
	part := fs.String("part", "snippet", "resource parts")
	hl := fs.String("hl", "", "language override")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google youtube support regions [--hl <language>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "snippet")
	q["hl"] = firstNonEmpty(strings.TrimSpace(*hl), runtime.LanguageCode, "en_US")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/i18nRegions", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeSupportCategories(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube support categories", args, true)
	part := fs.String("part", "snippet", "resource parts")
	region := fs.String("region", "", "region code")
	hl := fs.String("hl", "", "language override")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google youtube support categories [--region <cc>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "snippet")
	q["regionCode"] = firstNonEmpty(strings.TrimSpace(*region), runtime.RegionCode, "US")
	if value := firstNonEmpty(strings.TrimSpace(*hl), runtime.LanguageCode); value != "" {
		q["hl"] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/videoCategories", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeChannel(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google youtube channel <list|get|mine|update>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list", "get":
		cmdGoogleYouTubeChannelList(rest)
	case "mine":
		cmdGoogleYouTubeChannelMine(rest)
	case "update":
		cmdGoogleYouTubeChannelUpdate(rest)
	default:
		printUnknown("google youtube channel", sub)
		printUsage("usage: si google youtube channel <list|get|mine|update>")
	}
}

func cmdGoogleYouTubeChannelList(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube channel list", args, true)
	part := fs.String("part", "id,snippet,contentDetails,statistics", "resource parts")
	ids := fs.String("id", "", "channel id (csv)")
	forHandle := fs.String("for-handle", "", "channel handle")
	forUsername := fs.String("for-username", "", "channel username")
	mine := fs.Bool("mine", false, "fetch authenticated user channels")
	maxResults := fs.Int("max-results", 10, "max results")
	pageToken := fs.String("page-token", "", "page token")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google youtube channel list [--id <id>] [--for-handle <handle>] [--mine] [--json]")
		return
	}
	runtime, client := common.mustClient()
	if *mine {
		if err := googleYouTubeRequireOAuth(runtime, "channel list --mine"); err != nil {
			fatal(err)
		}
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "id,snippet,contentDetails,statistics")
	if value := parseGoogleYouTubeIDsCSV(*ids); value != "" {
		q["id"] = value
	}
	if value := strings.TrimSpace(*forHandle); value != "" {
		q["forHandle"] = value
	}
	if value := strings.TrimSpace(*forUsername); value != "" {
		q["forUsername"] = value
	}
	if *mine {
		q["mine"] = "true"
	}
	q["maxResults"] = strconv.Itoa(clampInt(*maxResults, 1, 50))
	if value := strings.TrimSpace(*pageToken); value != "" {
		q["pageToken"] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/channels", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeChannelMine(args []string) {
	cmdGoogleYouTubeChannelList(append([]string{"--mine"}, args...))
}

func cmdGoogleYouTubeChannelUpdate(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube channel update", args, true)
	part := fs.String("part", "snippet,brandingSettings,status", "resource parts")
	body := fs.String("body", "", "raw json body or @file")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*body) == "" {
		printUsage("usage: si google youtube channel update --body <json|@file> [--part <parts>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "channel update"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	rawBody, err := parseGoogleYouTubeBody(*body)
	if err != nil {
		fatal(err)
	}
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "snippet,brandingSettings,status")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodPut, Path: "/youtube/v3/channels", Params: q, RawBody: rawBody})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeVideo(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google youtube video <list|get|update|delete|upload|rate|get-rating>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list", "get":
		cmdGoogleYouTubeVideoList(rest)
	case "update":
		cmdGoogleYouTubeVideoUpdate(rest)
	case "delete", "remove", "rm":
		cmdGoogleYouTubeVideoDelete(rest)
	case "upload":
		cmdGoogleYouTubeVideoUpload(rest)
	case "rate":
		cmdGoogleYouTubeVideoRate(rest)
	case "get-rating", "rating":
		cmdGoogleYouTubeVideoGetRating(rest)
	default:
		printUnknown("google youtube video", sub)
		printUsage("usage: si google youtube video <list|get|update|delete|upload|rate|get-rating>")
	}
}

func cmdGoogleYouTubeVideoList(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube video list", args, true)
	part := fs.String("part", "id,snippet,contentDetails,statistics,status", "resource parts")
	ids := fs.String("id", "", "video id (csv)")
	chart := fs.String("chart", "", "chart mode (mostPopular)")
	myRating := fs.String("my-rating", "", "my rating (like|dislike)")
	region := fs.String("region", "", "region code")
	maxResults := fs.Int("max-results", 10, "max results")
	pageToken := fs.String("page-token", "", "page token")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google youtube video list [--id <id>] [--chart mostPopular] [--my-rating like|dislike] [--json]")
		return
	}
	runtime, client := common.mustClient()
	if strings.TrimSpace(*myRating) != "" {
		if err := googleYouTubeRequireOAuth(runtime, "video list --my-rating"); err != nil {
			fatal(err)
		}
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "id,snippet,contentDetails,statistics,status")
	if value := parseGoogleYouTubeIDsCSV(*ids); value != "" {
		q["id"] = value
	}
	if value := strings.TrimSpace(*chart); value != "" {
		q["chart"] = value
	}
	if value := strings.TrimSpace(*myRating); value != "" {
		q["myRating"] = value
	}
	if value := firstNonEmpty(strings.TrimSpace(*region), runtime.RegionCode); value != "" {
		q["regionCode"] = value
	}
	q["maxResults"] = strconv.Itoa(clampInt(*maxResults, 1, 50))
	if value := strings.TrimSpace(*pageToken); value != "" {
		q["pageToken"] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/videos", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeVideoUpdate(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube video update", args, true)
	part := fs.String("part", "snippet,status,recordingDetails", "resource parts")
	body := fs.String("body", "", "raw json body or @file")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*body) == "" {
		printUsage("usage: si google youtube video update --body <json|@file> [--part <parts>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "video update"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	rawBody, err := parseGoogleYouTubeBody(*body)
	if err != nil {
		fatal(err)
	}
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "snippet,status,recordingDetails")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodPut, Path: "/youtube/v3/videos", Params: q, RawBody: rawBody})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeVideoDelete(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube video delete", args, true)
	id := fs.String("id", "", "video id")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*id) == "" {
		printUsage("usage: si google youtube video delete --id <video-id> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "video delete"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["id"] = strings.TrimSpace(*id)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodDelete, Path: "/youtube/v3/videos", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeVideoRate(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube video rate", args, true)
	id := fs.String("id", "", "video id")
	rating := fs.String("rating", "", "rating (like|dislike|none)")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*id) == "" || strings.TrimSpace(*rating) == "" {
		printUsage("usage: si google youtube video rate --id <video-id> --rating <like|dislike|none> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "video rate"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["id"] = strings.TrimSpace(*id)
	q["rating"] = strings.ToLower(strings.TrimSpace(*rating))
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodPost, Path: "/youtube/v3/videos/rate", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeVideoGetRating(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube video get-rating", args, true)
	id := fs.String("id", "", "video id (csv)")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*id) == "" {
		printUsage("usage: si google youtube video get-rating --id <video-id[,video-id]> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "video get-rating"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["id"] = parseGoogleYouTubeIDsCSV(*id)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/videos/getRating", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeVideoUpload(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube video upload", args, true)
	filePath := fs.String("file", "", "video file path")
	title := fs.String("title", "", "video title")
	description := fs.String("description", "", "video description")
	privacy := fs.String("privacy", "private", "privacy status (private|unlisted|public)")
	tagsRaw := fs.String("tags", "", "comma-separated tags")
	category := fs.String("category", "22", "video category id")
	part := fs.String("part", "snippet,status", "resource parts")
	resumable := fs.Bool("resumable", true, "use resumable upload")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*filePath) == "" {
		printUsage("usage: si google youtube video upload --file <path> [--title <title>] [--description <text>] [--privacy <private|unlisted|public>] [--json]")
		return
	}
	runtime, _ := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "video upload"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	meta := map[string]any{
		"snippet": map[string]any{
			"title":       strings.TrimSpace(*title),
			"description": strings.TrimSpace(*description),
			"categoryId":  strings.TrimSpace(*category),
		},
		"status": map[string]any{
			"privacyStatus": strings.TrimSpace(*privacy),
		},
	}
	if tags := parseGoogleCSVList(*tagsRaw); len(tags) > 0 {
		metaSnippet := meta["snippet"].(map[string]any)
		metaSnippet["tags"] = tags
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	resp, err := googleYouTubeUploadVideo(ctx, runtime, strings.TrimSpace(*filePath), meta, googleYouTubeParts(*part, "snippet,status"), *resumable)
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubePlaylist(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google youtube playlist <list|get|create|update|delete>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list", "get":
		cmdGoogleYouTubePlaylistList(rest)
	case "create", "insert":
		cmdGoogleYouTubePlaylistCreate(rest)
	case "update":
		cmdGoogleYouTubePlaylistUpdate(rest)
	case "delete", "remove", "rm":
		cmdGoogleYouTubePlaylistDelete(rest)
	default:
		printUnknown("google youtube playlist", sub)
		printUsage("usage: si google youtube playlist <list|get|create|update|delete>")
	}
}

func cmdGoogleYouTubePlaylistList(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube playlist list", args, true)
	part := fs.String("part", "id,snippet,contentDetails,status", "resource parts")
	ids := fs.String("id", "", "playlist id (csv)")
	channelID := fs.String("channel-id", "", "channel id")
	mine := fs.Bool("mine", false, "fetch current user playlists")
	maxResults := fs.Int("max-results", 10, "max results")
	pageToken := fs.String("page-token", "", "page token")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google youtube playlist list [--id <id>] [--channel-id <id>] [--mine] [--json]")
		return
	}
	runtime, client := common.mustClient()
	if *mine {
		if err := googleYouTubeRequireOAuth(runtime, "playlist list --mine"); err != nil {
			fatal(err)
		}
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "id,snippet,contentDetails,status")
	if value := parseGoogleYouTubeIDsCSV(*ids); value != "" {
		q["id"] = value
	}
	if value := strings.TrimSpace(*channelID); value != "" {
		q["channelId"] = value
	}
	if *mine {
		q["mine"] = "true"
	}
	q["maxResults"] = strconv.Itoa(clampInt(*maxResults, 1, 50))
	if value := strings.TrimSpace(*pageToken); value != "" {
		q["pageToken"] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/playlists", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubePlaylistCreate(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube playlist create", args, true)
	title := fs.String("title", "", "playlist title")
	description := fs.String("description", "", "playlist description")
	privacy := fs.String("privacy", "private", "privacy status")
	part := fs.String("part", "snippet,status", "resource parts")
	body := fs.String("body", "", "raw json body or @file (overrides title/description/privacy)")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || (strings.TrimSpace(*title) == "" && strings.TrimSpace(*body) == "") {
		printUsage("usage: si google youtube playlist create --title <title> [--description <text>] [--privacy <private|unlisted|public>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "playlist create"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	requestBody := ""
	if strings.TrimSpace(*body) != "" {
		var err error
		requestBody, err = parseGoogleYouTubeBody(*body)
		if err != nil {
			fatal(err)
		}
	} else {
		raw, err := json.Marshal(map[string]any{
			"snippet": map[string]any{"title": strings.TrimSpace(*title), "description": strings.TrimSpace(*description)},
			"status":  map[string]any{"privacyStatus": strings.TrimSpace(*privacy)},
		})
		if err != nil {
			fatal(err)
		}
		requestBody = string(raw)
	}
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "snippet,status")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodPost, Path: "/youtube/v3/playlists", Params: q, RawBody: requestBody})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubePlaylistUpdate(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube playlist update", args, true)
	part := fs.String("part", "snippet,status", "resource parts")
	body := fs.String("body", "", "raw json body or @file")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*body) == "" {
		printUsage("usage: si google youtube playlist update --body <json|@file> [--part <parts>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "playlist update"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	rawBody, err := parseGoogleYouTubeBody(*body)
	if err != nil {
		fatal(err)
	}
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "snippet,status")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodPut, Path: "/youtube/v3/playlists", Params: q, RawBody: rawBody})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubePlaylistDelete(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube playlist delete", args, true)
	id := fs.String("id", "", "playlist id")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*id) == "" {
		printUsage("usage: si google youtube playlist delete --id <playlist-id> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "playlist delete"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["id"] = strings.TrimSpace(*id)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodDelete, Path: "/youtube/v3/playlists", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubePlaylistItem(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google youtube playlist-item <list|add|update|remove>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGoogleYouTubePlaylistItemList(rest)
	case "add", "create", "insert":
		cmdGoogleYouTubePlaylistItemAdd(rest)
	case "update":
		cmdGoogleYouTubePlaylistItemUpdate(rest)
	case "remove", "delete", "rm":
		cmdGoogleYouTubePlaylistItemRemove(rest)
	default:
		printUnknown("google youtube playlist-item", sub)
		printUsage("usage: si google youtube playlist-item <list|add|update|remove>")
	}
}

func cmdGoogleYouTubePlaylistItemList(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube playlist-item list", args, true)
	part := fs.String("part", "id,snippet,contentDetails,status", "resource parts")
	ids := fs.String("id", "", "playlist item id (csv)")
	playlistID := fs.String("playlist-id", "", "playlist id")
	maxResults := fs.Int("max-results", 10, "max results")
	pageToken := fs.String("page-token", "", "page token")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || (strings.TrimSpace(*ids) == "" && strings.TrimSpace(*playlistID) == "") {
		printUsage("usage: si google youtube playlist-item list (--playlist-id <id> | --id <item-id>) [--json]")
		return
	}
	runtime, client := common.mustClient()
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "id,snippet,contentDetails,status")
	if value := parseGoogleYouTubeIDsCSV(*ids); value != "" {
		q["id"] = value
	}
	if value := strings.TrimSpace(*playlistID); value != "" {
		q["playlistId"] = value
	}
	q["maxResults"] = strconv.Itoa(clampInt(*maxResults, 1, 50))
	if value := strings.TrimSpace(*pageToken); value != "" {
		q["pageToken"] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/playlistItems", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubePlaylistItemAdd(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube playlist-item add", args, true)
	playlistID := fs.String("playlist-id", "", "playlist id")
	videoID := fs.String("video-id", "", "video id")
	position := fs.Int("position", -1, "position in playlist")
	part := fs.String("part", "snippet", "resource parts")
	body := fs.String("body", "", "raw json body or @file (overrides flags)")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || ((strings.TrimSpace(*playlistID) == "" || strings.TrimSpace(*videoID) == "") && strings.TrimSpace(*body) == "") {
		printUsage("usage: si google youtube playlist-item add --playlist-id <id> --video-id <id> [--position <n>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "playlist-item add"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	requestBody := ""
	if strings.TrimSpace(*body) != "" {
		var err error
		requestBody, err = parseGoogleYouTubeBody(*body)
		if err != nil {
			fatal(err)
		}
	} else {
		payload := map[string]any{"snippet": map[string]any{"playlistId": strings.TrimSpace(*playlistID), "resourceId": map[string]any{"kind": "youtube#video", "videoId": strings.TrimSpace(*videoID)}}}
		if *position >= 0 {
			payload["snippet"].(map[string]any)["position"] = *position
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			fatal(err)
		}
		requestBody = string(raw)
	}
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "snippet")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodPost, Path: "/youtube/v3/playlistItems", Params: q, RawBody: requestBody})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubePlaylistItemUpdate(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube playlist-item update", args, true)
	part := fs.String("part", "snippet", "resource parts")
	body := fs.String("body", "", "raw json body or @file")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*body) == "" {
		printUsage("usage: si google youtube playlist-item update --body <json|@file> [--part <parts>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "playlist-item update"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	rawBody, err := parseGoogleYouTubeBody(*body)
	if err != nil {
		fatal(err)
	}
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "snippet")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodPut, Path: "/youtube/v3/playlistItems", Params: q, RawBody: rawBody})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubePlaylistItemRemove(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube playlist-item remove", args, true)
	id := fs.String("id", "", "playlist item id")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*id) == "" {
		printUsage("usage: si google youtube playlist-item remove --id <playlist-item-id> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "playlist-item remove"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["id"] = strings.TrimSpace(*id)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodDelete, Path: "/youtube/v3/playlistItems", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeSubscription(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google youtube subscription <list|create|delete>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGoogleYouTubeSubscriptionList(rest)
	case "create", "insert":
		cmdGoogleYouTubeSubscriptionCreate(rest)
	case "delete", "remove", "rm":
		cmdGoogleYouTubeSubscriptionDelete(rest)
	default:
		printUnknown("google youtube subscription", sub)
		printUsage("usage: si google youtube subscription <list|create|delete>")
	}
}

func cmdGoogleYouTubeSubscriptionList(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube subscription list", args, true)
	part := fs.String("part", "id,snippet,contentDetails,subscriberSnippet", "resource parts")
	ids := fs.String("id", "", "subscription id (csv)")
	channelID := fs.String("channel-id", "", "channel id")
	mine := fs.Bool("mine", true, "fetch authenticated subscriptions")
	forChannelID := fs.String("for-channel-id", "", "subscriptions to a specific channel")
	maxResults := fs.Int("max-results", 10, "max results")
	pageToken := fs.String("page-token", "", "page token")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google youtube subscription list [--mine] [--channel-id <id>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "subscription list"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "id,snippet,contentDetails,subscriberSnippet")
	if value := parseGoogleYouTubeIDsCSV(*ids); value != "" {
		q["id"] = value
	}
	if value := strings.TrimSpace(*channelID); value != "" {
		q["channelId"] = value
	}
	if value := strings.TrimSpace(*forChannelID); value != "" {
		q["forChannelId"] = value
	}
	if *mine {
		q["mine"] = "true"
	}
	q["maxResults"] = strconv.Itoa(clampInt(*maxResults, 1, 50))
	if value := strings.TrimSpace(*pageToken); value != "" {
		q["pageToken"] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/subscriptions", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeSubscriptionCreate(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube subscription create", args, true)
	channelID := fs.String("channel-id", "", "channel id to subscribe")
	part := fs.String("part", "snippet", "resource parts")
	body := fs.String("body", "", "raw json body or @file (overrides flags)")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || (strings.TrimSpace(*channelID) == "" && strings.TrimSpace(*body) == "") {
		printUsage("usage: si google youtube subscription create --channel-id <id> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "subscription create"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	requestBody := ""
	if strings.TrimSpace(*body) != "" {
		var err error
		requestBody, err = parseGoogleYouTubeBody(*body)
		if err != nil {
			fatal(err)
		}
	} else {
		raw, err := json.Marshal(map[string]any{"snippet": map[string]any{"resourceId": map[string]any{"kind": "youtube#channel", "channelId": strings.TrimSpace(*channelID)}}})
		if err != nil {
			fatal(err)
		}
		requestBody = string(raw)
	}
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "snippet")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodPost, Path: "/youtube/v3/subscriptions", Params: q, RawBody: requestBody})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeSubscriptionDelete(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube subscription delete", args, true)
	id := fs.String("id", "", "subscription id")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*id) == "" {
		printUsage("usage: si google youtube subscription delete --id <subscription-id> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "subscription delete"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["id"] = strings.TrimSpace(*id)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodDelete, Path: "/youtube/v3/subscriptions", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeComment(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google youtube comment <list|get|create|update|delete|thread>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGoogleYouTubeCommentList(rest)
	case "get":
		cmdGoogleYouTubeCommentGet(rest)
	case "create", "insert":
		cmdGoogleYouTubeCommentCreate(rest)
	case "update":
		cmdGoogleYouTubeCommentUpdate(rest)
	case "delete", "remove", "rm":
		cmdGoogleYouTubeCommentDelete(rest)
	case "thread", "threads":
		cmdGoogleYouTubeCommentThread(rest)
	default:
		printUnknown("google youtube comment", sub)
		printUsage("usage: si google youtube comment <list|get|create|update|delete|thread>")
	}
}

func cmdGoogleYouTubeCommentList(args []string) {
	// list comment threads, which is the common public/read workflow
	cmdGoogleYouTubeCommentThreadList(args)
}

func cmdGoogleYouTubeCommentGet(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube comment get", args, true)
	part := fs.String("part", "id,snippet", "resource parts")
	ids := fs.String("id", "", "comment id (csv)")
	maxResults := fs.Int("max-results", 20, "max results")
	pageToken := fs.String("page-token", "", "page token")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*ids) == "" {
		printUsage("usage: si google youtube comment get --id <comment-id[,comment-id]> [--json]")
		return
	}
	runtime, client := common.mustClient()
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "id,snippet")
	q["id"] = parseGoogleYouTubeIDsCSV(*ids)
	q["maxResults"] = strconv.Itoa(clampInt(*maxResults, 1, 100))
	if value := strings.TrimSpace(*pageToken); value != "" {
		q["pageToken"] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/comments", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeCommentCreate(args []string) {
	// create top-level comment thread
	cmdGoogleYouTubeCommentThreadCreate(args)
}

func cmdGoogleYouTubeCommentUpdate(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube comment update", args, true)
	part := fs.String("part", "snippet", "resource parts")
	body := fs.String("body", "", "raw json body or @file")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*body) == "" {
		printUsage("usage: si google youtube comment update --body <json|@file> [--part <parts>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "comment update"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	rawBody, err := parseGoogleYouTubeBody(*body)
	if err != nil {
		fatal(err)
	}
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "snippet")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodPut, Path: "/youtube/v3/comments", Params: q, RawBody: rawBody})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeCommentDelete(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube comment delete", args, true)
	id := fs.String("id", "", "comment id")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*id) == "" {
		printUsage("usage: si google youtube comment delete --id <comment-id> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "comment delete"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["id"] = strings.TrimSpace(*id)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodDelete, Path: "/youtube/v3/comments", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeCommentThread(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google youtube comment thread <list|create>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGoogleYouTubeCommentThreadList(rest)
	case "create", "insert":
		cmdGoogleYouTubeCommentThreadCreate(rest)
	default:
		printUnknown("google youtube comment thread", sub)
		printUsage("usage: si google youtube comment thread <list|create>")
	}
}

func cmdGoogleYouTubeCommentThreadList(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube comment thread list", args, true)
	part := fs.String("part", "id,snippet,replies", "resource parts")
	videoID := fs.String("video-id", "", "video id")
	channelID := fs.String("channel-id", "", "channel id")
	allThreadsRelatedToChannel := fs.String("all-threads-related-to-channel-id", "", "channel id")
	ids := fs.String("id", "", "thread id (csv)")
	maxResults := fs.Int("max-results", 20, "max results")
	order := fs.String("order", "", "order (time|relevance)")
	searchTerms := fs.String("search-terms", "", "search terms")
	pageToken := fs.String("page-token", "", "page token")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || (strings.TrimSpace(*videoID) == "" && strings.TrimSpace(*channelID) == "" && strings.TrimSpace(*allThreadsRelatedToChannel) == "" && strings.TrimSpace(*ids) == "") {
		printUsage("usage: si google youtube comment thread list (--video-id <id> | --channel-id <id> | --id <thread-id>) [--json]")
		return
	}
	runtime, client := common.mustClient()
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "id,snippet,replies")
	if value := strings.TrimSpace(*videoID); value != "" {
		q["videoId"] = value
	}
	if value := strings.TrimSpace(*channelID); value != "" {
		q["channelId"] = value
	}
	if value := strings.TrimSpace(*allThreadsRelatedToChannel); value != "" {
		q["allThreadsRelatedToChannelId"] = value
	}
	if value := parseGoogleYouTubeIDsCSV(*ids); value != "" {
		q["id"] = value
	}
	if value := strings.TrimSpace(*order); value != "" {
		q["order"] = value
	}
	if value := strings.TrimSpace(*searchTerms); value != "" {
		q["searchTerms"] = value
	}
	q["maxResults"] = strconv.Itoa(clampInt(*maxResults, 1, 100))
	if value := strings.TrimSpace(*pageToken); value != "" {
		q["pageToken"] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/commentThreads", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeCommentThreadCreate(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube comment thread create", args, true)
	part := fs.String("part", "snippet", "resource parts")
	videoID := fs.String("video-id", "", "video id")
	text := fs.String("text", "", "comment text")
	body := fs.String("body", "", "raw json body or @file (overrides flags)")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || ((strings.TrimSpace(*videoID) == "" || strings.TrimSpace(*text) == "") && strings.TrimSpace(*body) == "") {
		printUsage("usage: si google youtube comment thread create --video-id <id> --text <message> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "comment thread create"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	requestBody := ""
	if strings.TrimSpace(*body) != "" {
		var err error
		requestBody, err = parseGoogleYouTubeBody(*body)
		if err != nil {
			fatal(err)
		}
	} else {
		raw, err := json.Marshal(map[string]any{
			"snippet": map[string]any{
				"videoId": strings.TrimSpace(*videoID),
				"topLevelComment": map[string]any{
					"snippet": map[string]any{"textOriginal": strings.TrimSpace(*text)},
				},
			},
		})
		if err != nil {
			fatal(err)
		}
		requestBody = string(raw)
	}
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "snippet")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodPost, Path: "/youtube/v3/commentThreads", Params: q, RawBody: requestBody})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeCaption(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google youtube caption <list|upload|update|delete|download>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGoogleYouTubeCaptionList(rest)
	case "upload", "insert":
		cmdGoogleYouTubeCaptionUpload(rest)
	case "update":
		cmdGoogleYouTubeCaptionUpdate(rest)
	case "delete", "remove", "rm":
		cmdGoogleYouTubeCaptionDelete(rest)
	case "download", "get":
		cmdGoogleYouTubeCaptionDownload(rest)
	default:
		printUnknown("google youtube caption", sub)
		printUsage("usage: si google youtube caption <list|upload|update|delete|download>")
	}
}

func cmdGoogleYouTubeCaptionList(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube caption list", args, true)
	part := fs.String("part", "id,snippet", "resource parts")
	videoID := fs.String("video-id", "", "video id")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*videoID) == "" {
		printUsage("usage: si google youtube caption list --video-id <id> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "caption list"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "id,snippet")
	q["videoId"] = strings.TrimSpace(*videoID)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/captions", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeCaptionUpload(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube caption upload", args, true)
	videoID := fs.String("video-id", "", "video id")
	filePath := fs.String("file", "", "caption file path")
	language := fs.String("language", "en", "caption language")
	name := fs.String("name", "", "caption name")
	draft := fs.Bool("draft", false, "mark caption as draft")
	part := fs.String("part", "snippet", "resource parts")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*videoID) == "" || strings.TrimSpace(*filePath) == "" {
		printUsage("usage: si google youtube caption upload --video-id <id> --file <path> [--language <code>] [--name <label>] [--json]")
		return
	}
	runtime, _ := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "caption upload"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	meta := map[string]any{"snippet": map[string]any{"videoId": strings.TrimSpace(*videoID), "language": strings.TrimSpace(*language), "name": strings.TrimSpace(*name), "isDraft": *draft}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	resp, err := googleYouTubeMultipartUpload(ctx, runtime, "/youtube/v3/captions", googleYouTubeParts(*part, "snippet"), strings.TrimSpace(*filePath), "application/octet-stream", meta)
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeCaptionUpdate(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube caption update", args, true)
	part := fs.String("part", "snippet", "resource parts")
	body := fs.String("body", "", "raw json body or @file")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*body) == "" {
		printUsage("usage: si google youtube caption update --body <json|@file> [--part <parts>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "caption update"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	rawBody, err := parseGoogleYouTubeBody(*body)
	if err != nil {
		fatal(err)
	}
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "snippet")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodPut, Path: "/youtube/v3/captions", Params: q, RawBody: rawBody})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeCaptionDelete(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube caption delete", args, true)
	id := fs.String("id", "", "caption id")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*id) == "" {
		printUsage("usage: si google youtube caption delete --id <caption-id> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "caption delete"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["id"] = strings.TrimSpace(*id)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodDelete, Path: "/youtube/v3/captions", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeCaptionDownload(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube caption download", args, false)
	id := fs.String("id", "", "caption id")
	output := fs.String("output", "", "output file path")
	tfmt := fs.String("format", "", "caption format (sbv,scc,srt,vtt)")
	tlang := fs.String("translate", "", "translate language")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*id) == "" || strings.TrimSpace(*output) == "" {
		printUsage("usage: si google youtube caption download --id <caption-id> --output <path> [--format <vtt|srt|...>] [--translate <lang>] [--json]")
		return
	}
	runtime, _ := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "caption download"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := map[string]string{}
	if value := strings.TrimSpace(*tfmt); value != "" {
		q["tfmt"] = value
	}
	if value := strings.TrimSpace(*tlang); value != "" {
		q["tlang"] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	bytesWritten, contentType, err := googleYouTubeDownloadMedia(ctx, runtime, "/youtube/v3/captions/"+strings.TrimSpace(*id), q, strings.TrimSpace(*output))
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	payload := map[string]any{"output": strings.TrimSpace(*output), "bytes_written": bytesWritten, "content_type": contentType}
	if common.json() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	successf("downloaded caption to %s (%d bytes)", strings.TrimSpace(*output), bytesWritten)
}

func cmdGoogleYouTubeThumbnail(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google youtube thumbnail <set>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "set":
		cmdGoogleYouTubeThumbnailSet(rest)
	default:
		printUnknown("google youtube thumbnail", sub)
		printUsage("usage: si google youtube thumbnail <set>")
	}
}

func cmdGoogleYouTubeThumbnailSet(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube thumbnail set", args, true)
	videoID := fs.String("video-id", "", "video id")
	filePath := fs.String("file", "", "thumbnail image path")
	contentType := fs.String("content-type", "image/jpeg", "thumbnail content-type")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*videoID) == "" || strings.TrimSpace(*filePath) == "" {
		printUsage("usage: si google youtube thumbnail set --video-id <id> --file <path> [--content-type image/jpeg] [--json]")
		return
	}
	runtime, _ := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "thumbnail set"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	resp, err := googleYouTubeMediaUpload(ctx, runtime, "/youtube/v3/thumbnails/set", map[string]string{"videoId": strings.TrimSpace(*videoID)}, strings.TrimSpace(*filePath), strings.TrimSpace(*contentType))
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeLive(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google youtube live <broadcast|stream|chat>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "broadcast", "broadcasts":
		cmdGoogleYouTubeLiveBroadcast(rest)
	case "stream", "streams":
		cmdGoogleYouTubeLiveStream(rest)
	case "chat":
		cmdGoogleYouTubeLiveChat(rest)
	default:
		printUnknown("google youtube live", sub)
		printUsage("usage: si google youtube live <broadcast|stream|chat>")
	}
}

func cmdGoogleYouTubeLiveBroadcast(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google youtube live broadcast <list|get|create|update|delete|bind|transition>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list", "get":
		cmdGoogleYouTubeLiveBroadcastList(rest)
	case "create", "insert":
		cmdGoogleYouTubeLiveBroadcastCreate(rest)
	case "update":
		cmdGoogleYouTubeLiveBroadcastUpdate(rest)
	case "delete", "remove", "rm":
		cmdGoogleYouTubeLiveBroadcastDelete(rest)
	case "bind":
		cmdGoogleYouTubeLiveBroadcastBind(rest)
	case "transition":
		cmdGoogleYouTubeLiveBroadcastTransition(rest)
	default:
		printUnknown("google youtube live broadcast", sub)
		printUsage("usage: si google youtube live broadcast <list|get|create|update|delete|bind|transition>")
	}
}

func cmdGoogleYouTubeLiveBroadcastList(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube live broadcast list", args, true)
	part := fs.String("part", "id,snippet,contentDetails,status", "resource parts")
	ids := fs.String("id", "", "broadcast id (csv)")
	broadcastStatus := fs.String("status", "", "broadcast status (active|all|completed|upcoming)")
	broadcastType := fs.String("type", "", "broadcast type (all|event|persistent)")
	mine := fs.Bool("mine", true, "fetch authenticated broadcasts")
	maxResults := fs.Int("max-results", 10, "max results")
	pageToken := fs.String("page-token", "", "page token")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google youtube live broadcast list [--mine] [--id <id>] [--status <active|all|completed|upcoming>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "live broadcast list"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "id,snippet,contentDetails,status")
	if value := parseGoogleYouTubeIDsCSV(*ids); value != "" {
		q["id"] = value
	}
	if *mine {
		q["mine"] = "true"
	}
	if value := strings.TrimSpace(*broadcastStatus); value != "" {
		q["broadcastStatus"] = value
	}
	if value := strings.TrimSpace(*broadcastType); value != "" {
		q["broadcastType"] = value
	}
	q["maxResults"] = strconv.Itoa(clampInt(*maxResults, 1, 50))
	if value := strings.TrimSpace(*pageToken); value != "" {
		q["pageToken"] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/liveBroadcasts", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeLiveBroadcastCreate(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube live broadcast create", args, true)
	part := fs.String("part", "snippet,status,contentDetails", "resource parts")
	body := fs.String("body", "", "raw json body or @file")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*body) == "" {
		printUsage("usage: si google youtube live broadcast create --body <json|@file> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "live broadcast create"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	rawBody, err := parseGoogleYouTubeBody(*body)
	if err != nil {
		fatal(err)
	}
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "snippet,status,contentDetails")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodPost, Path: "/youtube/v3/liveBroadcasts", Params: q, RawBody: rawBody})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeLiveBroadcastUpdate(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube live broadcast update", args, true)
	part := fs.String("part", "snippet,status,contentDetails", "resource parts")
	body := fs.String("body", "", "raw json body or @file")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*body) == "" {
		printUsage("usage: si google youtube live broadcast update --body <json|@file> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "live broadcast update"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	rawBody, err := parseGoogleYouTubeBody(*body)
	if err != nil {
		fatal(err)
	}
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "snippet,status,contentDetails")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodPut, Path: "/youtube/v3/liveBroadcasts", Params: q, RawBody: rawBody})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeLiveBroadcastDelete(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube live broadcast delete", args, true)
	id := fs.String("id", "", "broadcast id")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*id) == "" {
		printUsage("usage: si google youtube live broadcast delete --id <broadcast-id> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "live broadcast delete"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["id"] = strings.TrimSpace(*id)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodDelete, Path: "/youtube/v3/liveBroadcasts", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeLiveBroadcastBind(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube live broadcast bind", args, true)
	id := fs.String("id", "", "broadcast id")
	streamID := fs.String("stream-id", "", "live stream id")
	part := fs.String("part", "id,snippet,contentDetails,status", "resource parts")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*id) == "" || strings.TrimSpace(*streamID) == "" {
		printUsage("usage: si google youtube live broadcast bind --id <broadcast-id> --stream-id <stream-id> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "live broadcast bind"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["id"] = strings.TrimSpace(*id)
	q["streamId"] = strings.TrimSpace(*streamID)
	q["part"] = googleYouTubeParts(*part, "id,snippet,contentDetails,status")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodPost, Path: "/youtube/v3/liveBroadcasts/bind", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeLiveBroadcastTransition(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube live broadcast transition", args, true)
	id := fs.String("id", "", "broadcast id")
	status := fs.String("status", "live", "broadcast status (testing|live|complete)")
	part := fs.String("part", "id,snippet,contentDetails,status", "resource parts")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*id) == "" {
		printUsage("usage: si google youtube live broadcast transition --id <broadcast-id> --status <testing|live|complete> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "live broadcast transition"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["id"] = strings.TrimSpace(*id)
	q["broadcastStatus"] = strings.TrimSpace(*status)
	q["part"] = googleYouTubeParts(*part, "id,snippet,contentDetails,status")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodPost, Path: "/youtube/v3/liveBroadcasts/transition", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeLiveStream(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google youtube live stream <list|get|create|update|delete>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list", "get":
		cmdGoogleYouTubeLiveStreamList(rest)
	case "create", "insert":
		cmdGoogleYouTubeLiveStreamCreate(rest)
	case "update":
		cmdGoogleYouTubeLiveStreamUpdate(rest)
	case "delete", "remove", "rm":
		cmdGoogleYouTubeLiveStreamDelete(rest)
	default:
		printUnknown("google youtube live stream", sub)
		printUsage("usage: si google youtube live stream <list|get|create|update|delete>")
	}
}

func cmdGoogleYouTubeLiveStreamList(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube live stream list", args, true)
	part := fs.String("part", "id,snippet,cdn,status", "resource parts")
	ids := fs.String("id", "", "stream id (csv)")
	mine := fs.Bool("mine", true, "fetch authenticated streams")
	maxResults := fs.Int("max-results", 10, "max results")
	pageToken := fs.String("page-token", "", "page token")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google youtube live stream list [--mine] [--id <id>] [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "live stream list"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "id,snippet,cdn,status")
	if value := parseGoogleYouTubeIDsCSV(*ids); value != "" {
		q["id"] = value
	}
	if *mine {
		q["mine"] = "true"
	}
	q["maxResults"] = strconv.Itoa(clampInt(*maxResults, 1, 50))
	if value := strings.TrimSpace(*pageToken); value != "" {
		q["pageToken"] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/liveStreams", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeLiveStreamCreate(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube live stream create", args, true)
	part := fs.String("part", "snippet,cdn,status", "resource parts")
	body := fs.String("body", "", "raw json body or @file")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*body) == "" {
		printUsage("usage: si google youtube live stream create --body <json|@file> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "live stream create"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	rawBody, err := parseGoogleYouTubeBody(*body)
	if err != nil {
		fatal(err)
	}
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "snippet,cdn,status")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodPost, Path: "/youtube/v3/liveStreams", Params: q, RawBody: rawBody})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeLiveStreamUpdate(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube live stream update", args, true)
	part := fs.String("part", "snippet,cdn,status", "resource parts")
	body := fs.String("body", "", "raw json body or @file")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*body) == "" {
		printUsage("usage: si google youtube live stream update --body <json|@file> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "live stream update"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	rawBody, err := parseGoogleYouTubeBody(*body)
	if err != nil {
		fatal(err)
	}
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "snippet,cdn,status")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodPut, Path: "/youtube/v3/liveStreams", Params: q, RawBody: rawBody})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeLiveStreamDelete(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube live stream delete", args, true)
	id := fs.String("id", "", "stream id")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*id) == "" {
		printUsage("usage: si google youtube live stream delete --id <stream-id> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "live stream delete"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["id"] = strings.TrimSpace(*id)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodDelete, Path: "/youtube/v3/liveStreams", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeLiveChat(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google youtube live chat <list|create|delete>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "list":
		cmdGoogleYouTubeLiveChatList(rest)
	case "create", "insert", "post":
		cmdGoogleYouTubeLiveChatCreate(rest)
	case "delete", "remove", "rm":
		cmdGoogleYouTubeLiveChatDelete(rest)
	default:
		printUnknown("google youtube live chat", sub)
		printUsage("usage: si google youtube live chat <list|create|delete>")
	}
}

func cmdGoogleYouTubeLiveChatList(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube live chat list", args, true)
	part := fs.String("part", "id,snippet,authorDetails", "resource parts")
	liveChatID := fs.String("live-chat-id", "", "live chat id")
	maxResults := fs.Int("max-results", 10, "max results")
	pageToken := fs.String("page-token", "", "page token")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*liveChatID) == "" {
		printUsage("usage: si google youtube live chat list --live-chat-id <id> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "live chat list"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "id,snippet,authorDetails")
	q["liveChatId"] = strings.TrimSpace(*liveChatID)
	q["maxResults"] = strconv.Itoa(clampInt(*maxResults, 1, 2000))
	if value := strings.TrimSpace(*pageToken); value != "" {
		q["pageToken"] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodGet, Path: "/youtube/v3/liveChat/messages", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeLiveChatCreate(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube live chat create", args, true)
	part := fs.String("part", "snippet", "resource parts")
	liveChatID := fs.String("live-chat-id", "", "live chat id")
	text := fs.String("text", "", "message text")
	body := fs.String("body", "", "raw json body or @file (overrides flags)")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || ((strings.TrimSpace(*liveChatID) == "" || strings.TrimSpace(*text) == "") && strings.TrimSpace(*body) == "") {
		printUsage("usage: si google youtube live chat create --live-chat-id <id> --text <message> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "live chat create"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	requestBody := ""
	if strings.TrimSpace(*body) != "" {
		var err error
		requestBody, err = parseGoogleYouTubeBody(*body)
		if err != nil {
			fatal(err)
		}
	} else {
		raw, err := json.Marshal(map[string]any{"snippet": map[string]any{"liveChatId": strings.TrimSpace(*liveChatID), "type": "textMessageEvent", "textMessageDetails": map[string]any{"messageText": strings.TrimSpace(*text)}}})
		if err != nil {
			fatal(err)
		}
		requestBody = string(raw)
	}
	q := parseGoogleParams(params)
	q["part"] = googleYouTubeParts(*part, "snippet")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodPost, Path: "/youtube/v3/liveChat/messages", Params: q, RawBody: requestBody})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeLiveChatDelete(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube live chat delete", args, true)
	id := fs.String("id", "", "live chat message id")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*id) == "" {
		printUsage("usage: si google youtube live chat delete --id <message-id> [--json]")
		return
	}
	runtime, client := common.mustClient()
	if err := googleYouTubeRequireOAuth(runtime, "live chat delete"); err != nil {
		fatal(err)
	}
	printGoogleYouTubeContextBanner(runtime, common.json())
	q := parseGoogleParams(params)
	q["id"] = strings.TrimSpace(*id)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{Method: http.MethodDelete, Path: "/youtube/v3/liveChat/messages", Params: q})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeRaw(args []string) {
	fs, common := googleYouTubeCommonFlagSet("google youtube raw", args, true)
	method := fs.String("method", http.MethodGet, "http method")
	path := fs.String("path", "", "api path")
	body := fs.String("body", "", "raw request body or @file")
	contentType := fs.String("content-type", "", "request content type")
	useUpload := fs.Bool("upload", false, "use upload base url")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*path) == "" {
		printUsage("usage: si google youtube raw --method <GET|POST|PUT|DELETE> --path <api-path> [--param key=value] [--body raw|@file] [--upload] [--json]")
		return
	}
	runtime, client := common.mustClient()
	printGoogleYouTubeContextBanner(runtime, common.json())
	rawBody, err := parseGoogleYouTubeBody(*body)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Do(ctx, youtubebridge.Request{
		Method:      strings.ToUpper(strings.TrimSpace(*method)),
		Path:        strings.TrimSpace(*path),
		Params:      parseGoogleParams(params),
		RawBody:     rawBody,
		ContentType: strings.TrimSpace(*contentType),
		UseUpload:   *useUpload,
	})
	if err != nil {
		printGoogleYouTubeError(err)
		return
	}
	printGoogleYouTubeResponse(resp, common.json(), common.rawEnabled())
}

func cmdGoogleYouTubeReport(args []string) {
	if len(args) == 0 {
		printUsage("usage: si google youtube report <usage|quota>")
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "usage":
		cmdGoogleYouTubeReportUsage(rest)
	case "quota":
		cmdGoogleYouTubeReportQuota(rest)
	default:
		printUnknown("google youtube report", sub)
		printUsage("usage: si google youtube report <usage|quota>")
	}
}

func cmdGoogleYouTubeReportUsage(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("google youtube report usage", flag.ExitOnError)
	sinceRaw := fs.String("since", "", "start timestamp (unix seconds or RFC3339)")
	untilRaw := fs.String("until", "", "end timestamp (unix seconds or RFC3339)")
	account := fs.String("account", "", "filter account alias")
	env := fs.String("env", "", "filter environment")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google youtube report usage [--since <ts>] [--until <ts>] [--account <alias>] [--env <prod|staging|dev>] [--json]")
		return
	}
	since, until, err := parseGoogleReportWindow(*sinceRaw, *untilRaw)
	if err != nil {
		fatal(err)
	}
	settings := loadSettingsOrDefault()
	logPath := googleYouTubeLogPathForSettings(settings)
	if strings.TrimSpace(logPath) == "" {
		fatal(fmt.Errorf("google youtube log path is not configured"))
	}
	report, err := buildGoogleYouTubeUsageReport(logPath, strings.TrimSpace(*account), normalizeGoogleEnvironment(*env), since, until, settings.Google.YouTube.QuotaBudgetDaily)
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
	printGoogleYouTubeUsageReport(report)
}

func cmdGoogleYouTubeReportQuota(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "estimate": true})
	fs := flag.NewFlagSet("google youtube report quota", flag.ExitOnError)
	account := fs.String("account", "", "filter account alias")
	env := fs.String("env", "", "filter environment")
	estimate := fs.Bool("estimate", true, "estimate quota from local logs")
	sinceRaw := fs.String("since", "", "start timestamp (unix seconds or RFC3339); default: start of current UTC day")
	untilRaw := fs.String("until", "", "end timestamp (unix seconds or RFC3339); default: now")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si google youtube report quota [--account <alias>] [--env <prod|staging|dev>] [--since <ts>] [--until <ts>] [--json]")
		return
	}
	var since *time.Time
	var until *time.Time
	if strings.TrimSpace(*sinceRaw) == "" && strings.TrimSpace(*untilRaw) == "" {
		now := time.Now().UTC()
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		since = &start
		until = &now
	} else {
		parsedSince, parsedUntil, err := parseGoogleReportWindow(*sinceRaw, *untilRaw)
		if err != nil {
			fatal(err)
		}
		since = parsedSince
		until = parsedUntil
	}
	settings := loadSettingsOrDefault()
	logPath := googleYouTubeLogPathForSettings(settings)
	report, err := buildGoogleYouTubeUsageReport(logPath, strings.TrimSpace(*account), normalizeGoogleEnvironment(*env), since, until, settings.Google.YouTube.QuotaBudgetDaily)
	if err != nil {
		fatal(err)
	}
	payload := map[string]any{
		"account":            strings.TrimSpace(*account),
		"environment":        normalizeGoogleEnvironment(*env),
		"window_since":       report["window_since"],
		"window_until":       report["window_until"],
		"quota_budget_daily": report["quota_budget_daily"],
		"estimated_quota":    report["estimated_quota"],
		"quota_remaining":    report["quota_remaining"],
		"estimated":          *estimate,
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s\\n", styleHeading("Google YouTube quota report:"), styleSuccess("ok"))
	fmt.Printf("%s %s\\n", styleHeading("Window:"), fmt.Sprintf("%s -> %s", stringifyGoogleYouTubeAny(payload["window_since"]), stringifyGoogleYouTubeAny(payload["window_until"])))
	fmt.Printf("%s %s\\n", styleHeading("Budget:"), stringifyGoogleYouTubeAny(payload["quota_budget_daily"]))
	fmt.Printf("%s %s\\n", styleHeading("Estimated used:"), stringifyGoogleYouTubeAny(payload["estimated_quota"]))
	fmt.Printf("%s %s\\n", styleHeading("Remaining:"), stringifyGoogleYouTubeAny(payload["quota_remaining"]))
}

func googleYouTubeUploadVideo(ctx context.Context, runtime googleYouTubeRuntimeContext, filePath string, metadata map[string]any, part string, resumable bool) (youtubebridge.Response, error) {
	if !resumable {
		return googleYouTubeMultipartUpload(ctx, runtime, "/youtube/v3/videos", part, filePath, "application/octet-stream", metadata)
	}
	videoBytes, err := readLocalFile(filePath)
	if err != nil {
		return youtubebridge.Response{}, err
	}
	metaRaw, err := json.Marshal(metadata)
	if err != nil {
		return youtubebridge.Response{}, err
	}
	initURL, err := apibridge.JoinURL(runtime.UploadBaseURL, "/youtube/v3/videos", map[string]string{"uploadType": "resumable", "part": part})
	if err != nil {
		return youtubebridge.Response{}, err
	}
	initClient, err := newGoogleYouTubeHTTPClient(runtime.UploadBaseURL, 90*time.Second)
	if err != nil {
		return youtubebridge.Response{}, err
	}
	initResp, err := initClient.Do(ctx, apibridge.Request{
		Method:      http.MethodPost,
		URL:         initURL,
		BodyBytes:   metaRaw,
		ContentType: "application/json",
		Headers: map[string]string{
			"Accept":                  "application/json",
			"X-Upload-Content-Length": strconv.Itoa(len(videoBytes)),
			"X-Upload-Content-Type":   detectContentTypeByPath(filePath, "application/octet-stream"),
		},
		Prepare: func(ctx context.Context, _ int, httpReq *http.Request) error {
			_ = ctx
			return googleYouTubeApplyAuth(runtime, httpReq)
		},
	})
	if err != nil {
		return youtubebridge.Response{}, err
	}
	if initResp.StatusCode < 200 || initResp.StatusCode >= 300 {
		return youtubebridge.Response{}, youtubebridge.NormalizeHTTPError(initResp.StatusCode, initResp.Headers, string(initResp.Body))
	}
	location := strings.TrimSpace(initResp.Headers.Get("Location"))
	if location == "" {
		return youtubebridge.Response{}, fmt.Errorf("resumable upload did not return location header")
	}

	uploadClient, err := newGoogleYouTubeHTTPClient(runtime.UploadBaseURL, 20*time.Minute)
	if err != nil {
		return youtubebridge.Response{}, err
	}
	uploadResp, err := uploadClient.Do(ctx, apibridge.Request{
		Method:      http.MethodPut,
		URL:         location,
		BodyBytes:   videoBytes,
		ContentType: detectContentTypeByPath(filePath, "application/octet-stream"),
		Headers: map[string]string{
			"Accept":         "application/json",
			"Content-Length": strconv.Itoa(len(videoBytes)),
		},
		Prepare: func(ctx context.Context, _ int, httpReq *http.Request) error {
			_ = ctx
			return googleYouTubeApplyAuth(runtime, httpReq)
		},
	})
	if err != nil {
		return youtubebridge.Response{}, err
	}
	if uploadResp.StatusCode < 200 || uploadResp.StatusCode >= 300 {
		return youtubebridge.Response{}, youtubebridge.NormalizeHTTPError(uploadResp.StatusCode, uploadResp.Headers, string(uploadResp.Body))
	}
	return normalizeGoogleYouTubeHTTPResponse(uploadResp.StatusCode, uploadResp.Status, uploadResp.Headers, string(uploadResp.Body)), nil
}

func newGoogleYouTubeHTTPClient(baseURL string, timeout time.Duration) (*apibridge.Client, error) {
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	return apibridge.NewClient(apibridge.Config{
		Component:   "google-youtube-cli",
		BaseURL:     strings.TrimSpace(baseURL),
		UserAgent:   "si-youtube/1.0",
		Timeout:     timeout,
		MaxRetries:  0,
		Redact:      youtubebridge.RedactSensitive,
		SanitizeURL: apibridge.StripQuery,
		RequestIDFromHeaders: func(h http.Header) string {
			if h == nil {
				return ""
			}
			if v := strings.TrimSpace(h.Get("X-Google-Request-Id")); v != "" {
				return v
			}
			return strings.TrimSpace(h.Get("X-Request-Id"))
		},
	})
}

func googleYouTubeMultipartUpload(ctx context.Context, runtime googleYouTubeRuntimeContext, path string, part string, filePath string, contentType string, metadata map[string]any) (youtubebridge.Response, error) {
	fileBytes, err := readLocalFile(filePath)
	if err != nil {
		return youtubebridge.Response{}, err
	}
	metaRaw, err := json.Marshal(metadata)
	if err != nil {
		return youtubebridge.Response{}, err
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	metaHeader := make(textproto.MIMEHeader)
	metaHeader.Set("Content-Type", "application/json; charset=UTF-8")
	metaPart, err := writer.CreatePart(metaHeader)
	if err != nil {
		return youtubebridge.Response{}, err
	}
	if _, err := metaPart.Write(metaRaw); err != nil {
		return youtubebridge.Response{}, err
	}
	mediaHeader := make(textproto.MIMEHeader)
	mediaHeader.Set("Content-Type", detectContentTypeByPath(filePath, contentType))
	mediaPart, err := writer.CreatePart(mediaHeader)
	if err != nil {
		return youtubebridge.Response{}, err
	}
	if _, err := mediaPart.Write(fileBytes); err != nil {
		return youtubebridge.Response{}, err
	}
	if err := writer.Close(); err != nil {
		return youtubebridge.Response{}, err
	}
	endpoint, err := apibridge.JoinURL(runtime.UploadBaseURL, path, map[string]string{"uploadType": "multipart", "part": part})
	if err != nil {
		return youtubebridge.Response{}, err
	}
	client, err := newGoogleYouTubeHTTPClient(runtime.UploadBaseURL, 10*time.Minute)
	if err != nil {
		return youtubebridge.Response{}, err
	}
	resp, err := client.Do(ctx, apibridge.Request{
		Method:      http.MethodPost,
		URL:         endpoint,
		BodyBytes:   body.Bytes(),
		ContentType: writer.FormDataContentType(),
		Headers: map[string]string{
			"Accept": "application/json",
		},
		Prepare: func(ctx context.Context, _ int, httpReq *http.Request) error {
			_ = ctx
			return googleYouTubeApplyAuth(runtime, httpReq)
		},
	})
	if err != nil {
		return youtubebridge.Response{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return youtubebridge.Response{}, youtubebridge.NormalizeHTTPError(resp.StatusCode, resp.Headers, string(resp.Body))
	}
	return normalizeGoogleYouTubeHTTPResponse(resp.StatusCode, resp.Status, resp.Headers, string(resp.Body)), nil
}

func googleYouTubeMediaUpload(ctx context.Context, runtime googleYouTubeRuntimeContext, path string, query map[string]string, filePath string, contentType string) (youtubebridge.Response, error) {
	fileBytes, err := readLocalFile(filePath)
	if err != nil {
		return youtubebridge.Response{}, err
	}
	q := map[string]string{"uploadType": "media"}
	for key, value := range query {
		q[key] = value
	}
	endpoint, err := apibridge.JoinURL(runtime.UploadBaseURL, path, q)
	if err != nil {
		return youtubebridge.Response{}, err
	}
	client, err := newGoogleYouTubeHTTPClient(runtime.UploadBaseURL, 5*time.Minute)
	if err != nil {
		return youtubebridge.Response{}, err
	}
	resp, err := client.Do(ctx, apibridge.Request{
		Method:      http.MethodPost,
		URL:         endpoint,
		BodyBytes:   fileBytes,
		ContentType: detectContentTypeByPath(filePath, contentType),
		Headers: map[string]string{
			"Accept":         "application/json",
			"Content-Length": strconv.Itoa(len(fileBytes)),
		},
		Prepare: func(ctx context.Context, _ int, httpReq *http.Request) error {
			_ = ctx
			return googleYouTubeApplyAuth(runtime, httpReq)
		},
	})
	if err != nil {
		return youtubebridge.Response{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return youtubebridge.Response{}, youtubebridge.NormalizeHTTPError(resp.StatusCode, resp.Headers, string(resp.Body))
	}
	return normalizeGoogleYouTubeHTTPResponse(resp.StatusCode, resp.Status, resp.Headers, string(resp.Body)), nil
}

func googleYouTubeDownloadMedia(ctx context.Context, runtime googleYouTubeRuntimeContext, path string, query map[string]string, output string) (int64, string, error) {
	endpoint, err := apibridge.JoinURL(runtime.BaseURL, path, query)
	if err != nil {
		return 0, "", err
	}
	client, err := newGoogleYouTubeHTTPClient(runtime.BaseURL, 90*time.Second)
	if err != nil {
		return 0, "", err
	}
	resp, err := client.Do(ctx, apibridge.Request{
		Method: http.MethodGet,
		URL:    endpoint,
		Headers: map[string]string{
			"Accept": "*/*",
		},
		Prepare: func(ctx context.Context, _ int, httpReq *http.Request) error {
			_ = ctx
			return googleYouTubeApplyAuth(runtime, httpReq)
		},
	})
	if err != nil {
		return 0, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, "", youtubebridge.NormalizeHTTPError(resp.StatusCode, resp.Headers, string(resp.Body))
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o750); err != nil {
		return 0, "", err
	}
	if err := os.WriteFile(output, resp.Body, 0o600); err != nil {
		return 0, "", err
	}
	return int64(len(resp.Body)), strings.TrimSpace(resp.Headers.Get("Content-Type")), nil
}

func googleYouTubeApplyAuth(runtime googleYouTubeRuntimeContext, req *http.Request) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	mode := strings.TrimSpace(runtime.AuthMode)
	if mode == "oauth" {
		provider, err := buildGoogleYouTubeTokenProvider(runtime)
		if err != nil {
			return err
		}
		token, err := provider.Token(req.Context())
		if err != nil {
			return err
		}
		if strings.TrimSpace(token.Value) == "" {
			return fmt.Errorf("oauth token provider returned empty token")
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token.Value))
		return nil
	}
	apiKey := strings.TrimSpace(runtime.APIKey)
	if apiKey == "" {
		return fmt.Errorf("youtube api key is required for api-key mode")
	}
	q := req.URL.Query()
	if strings.TrimSpace(q.Get("key")) == "" {
		q.Set("key", apiKey)
		req.URL.RawQuery = q.Encode()
	}
	return nil
}

func normalizeGoogleYouTubeHTTPResponse(statusCode int, status string, headers http.Header, body string) youtubebridge.Response {
	response := youtubebridge.Response{StatusCode: statusCode, Status: status, Body: youtubebridge.RedactSensitive(strings.TrimSpace(body))}
	requestID := strings.TrimSpace(headers.Get("X-Google-Request-Id"))
	if requestID == "" {
		requestID = strings.TrimSpace(headers.Get("X-Request-Id"))
	}
	response.RequestID = requestID
	if len(headers) > 0 {
		response.Headers = map[string]string{}
		keys := make([]string, 0, len(headers))
		for key := range headers {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			response.Headers[key] = youtubebridge.RedactSensitive(strings.Join(headers.Values(key), ","))
		}
	}
	if strings.TrimSpace(body) == "" {
		return response
	}
	var parsed any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return response
	}
	switch typed := parsed.(type) {
	case map[string]any:
		response.Data = typed
		if items, ok := typed["items"].([]any); ok {
			response.List = make([]map[string]any, 0, len(items))
			for _, item := range items {
				if obj, ok := item.(map[string]any); ok {
					response.List = append(response.List, obj)
				}
			}
		}
	case []any:
		response.List = make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if obj, ok := item.(map[string]any); ok {
				response.List = append(response.List, obj)
			}
		}
	}
	return response
}

func detectContentTypeByPath(path string, fallback string) string {
	lower := strings.ToLower(strings.TrimSpace(path))
	switch {
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".vtt"):
		return "text/vtt"
	case strings.HasSuffix(lower, ".srt"):
		return "application/x-subrip"
	case strings.HasSuffix(lower, ".sbv"):
		return "text/plain"
	case strings.HasSuffix(lower, ".mp4"):
		return "video/mp4"
	case strings.HasSuffix(lower, ".mov"):
		return "video/quicktime"
	case strings.HasSuffix(lower, ".mkv"):
		return "video/x-matroska"
	case strings.HasSuffix(lower, ".webm"):
		return "video/webm"
	case strings.HasSuffix(lower, ".avi"):
		return "video/x-msvideo"
	}
	if strings.TrimSpace(fallback) != "" {
		return strings.TrimSpace(fallback)
	}
	return "application/octet-stream"
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

type youtubeUsageLogEvent struct {
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

func buildGoogleYouTubeUsageReport(logPath string, account string, env string, since *time.Time, until *time.Time, quotaBudget int64) (map[string]any, error) {
	file, err := openLocalFile(logPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{
				"log_path":           logPath,
				"requests":           0,
				"responses":          0,
				"errors":             0,
				"average_ms":         0,
				"estimated_quota":    0,
				"quota_budget_daily": quotaBudget,
				"quota_remaining":    quotaBudget,
				"status_buckets":     map[string]int{},
				"top_endpoints":      []map[string]any{},
			}, nil
		}
		return nil, err
	}
	defer file.Close()
	events, err := readGoogleYouTubeUsageEvents(file)
	if err != nil {
		return nil, err
	}
	requestCount := 0
	responseCount := 0
	errorCount := 0
	totalDuration := int64(0)
	durationCount := 0
	estimatedQuota := int64(0)
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
		pathRef := parseGooglePathRef(event.Path)
		key := strings.ToUpper(strings.TrimSpace(event.Method)) + " " + pathRef
		switch event.Event {
		case "request":
			requestCount++
			endpointCounts[key]++
			estimatedQuota += estimateYouTubeQuota(pathRef)
		case "response":
			responseCount++
			if event.StatusCode >= 400 {
				errorCount++
			}
			statusBuckets[googleStatusBucket(event.StatusCode)]++
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
		left := topEndpoints[i]["count"].(int)
		right := topEndpoints[j]["count"].(int)
		if left == right {
			return topEndpoints[i]["endpoint"].(string) < topEndpoints[j]["endpoint"].(string)
		}
		return left > right
	})
	if len(topEndpoints) > 10 {
		topEndpoints = topEndpoints[:10]
	}
	remaining := int64(0)
	if quotaBudget > 0 {
		remaining = quotaBudget - estimatedQuota
		if remaining < 0 {
			remaining = 0
		}
	}
	out := map[string]any{
		"log_path":           logPath,
		"requests":           requestCount,
		"responses":          responseCount,
		"errors":             errorCount,
		"average_ms":         average,
		"estimated_quota":    estimatedQuota,
		"quota_budget_daily": quotaBudget,
		"quota_remaining":    remaining,
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

func readGoogleYouTubeUsageEvents(r io.Reader) ([]youtubeUsageLogEvent, error) {
	decoder := json.NewDecoder(r)
	events := []youtubeUsageLogEvent{}
	for {
		var entry map[string]any
		if err := decoder.Decode(&entry); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		event := youtubeUsageLogEvent{}
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

func estimateYouTubeQuota(pathRef string) int64 {
	pathRef = strings.TrimSpace(pathRef)
	switch {
	case strings.Contains(pathRef, "/youtube/v3/search"):
		return 100
	case strings.Contains(pathRef, "/youtube/v3/videos") && strings.Contains(pathRef, "uploadType="):
		return 1600
	case strings.Contains(pathRef, "/youtube/v3/captions") && strings.Contains(pathRef, "uploadType="):
		return 400
	case strings.Contains(pathRef, "/youtube/v3/liveBroadcasts"), strings.Contains(pathRef, "/youtube/v3/liveStreams"):
		return 50
	default:
		return 1
	}
}

func printGoogleYouTubeUsageReport(report map[string]any) {
	fmt.Printf("%s %s\n", styleHeading("Google YouTube usage report:"), orDash(stringifyGoogleYouTubeAny(report["log_path"])))
	fmt.Printf("%s %s\n", styleHeading("Requests:"), stringifyGoogleYouTubeAny(report["requests"]))
	fmt.Printf("%s %s\n", styleHeading("Responses:"), stringifyGoogleYouTubeAny(report["responses"]))
	fmt.Printf("%s %s\n", styleHeading("Errors:"), stringifyGoogleYouTubeAny(report["errors"]))
	fmt.Printf("%s %s\n", styleHeading("Avg duration:"), stringifyGoogleYouTubeAny(report["average_ms"])+"ms")
	fmt.Printf("%s %s / %s\n", styleHeading("Estimated quota:"), stringifyGoogleYouTubeAny(report["estimated_quota"]), stringifyGoogleYouTubeAny(report["quota_budget_daily"]))
	fmt.Printf("%s %s\n", styleHeading("Quota remaining:"), stringifyGoogleYouTubeAny(report["quota_remaining"]))
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
			fmt.Printf("  %s %s\n", padRightANSI(stringifyGoogleYouTubeAny(item["count"]), 4), stringifyGoogleYouTubeAny(item["endpoint"]))
		}
	}
}
