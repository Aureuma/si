package main

import (
	"context"
	"flag"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const socialInstagramUsageText = "usage: si social instagram <auth|context|doctor|profile|media|comment|insights|raw|report>"

func cmdSocialInstagram(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, socialInstagramUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(socialInstagramUsageText)
	case "auth":
		cmdSocialPlatformAuth(socialPlatformInstagram, rest)
	case "context":
		cmdSocialPlatformContext(socialPlatformInstagram, rest)
	case "doctor":
		cmdSocialPlatformDoctor(socialPlatformInstagram, rest)
	case "profile", "account", "me":
		cmdSocialInstagramProfile(rest)
	case "media":
		cmdSocialInstagramMedia(rest)
	case "comment", "comments":
		cmdSocialInstagramComment(rest)
	case "insights", "insight":
		cmdSocialInstagramInsights(rest)
	case "raw":
		cmdSocialPlatformRaw(socialPlatformInstagram, rest)
	case "report":
		cmdSocialPlatformReport(socialPlatformInstagram, rest)
	default:
		printUnknown("social instagram", cmd)
		printUsage(socialInstagramUsageText)
	}
}

func cmdSocialInstagramProfile(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social instagram profile", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	fields := fs.String("fields", "id,username,account_type,media_count", "fields to request")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si social instagram profile [--fields id,username] [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformInstagram,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
	})
	values := parseSocialParams(params)
	values["fields"] = strings.TrimSpace(*fields)
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/me",
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialInstagramMedia(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si social instagram media <list|get|create|publish> [flags]")
	if !routedOK {
		return
	}
	args = routedArgs
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdSocialInstagramMediaList(args[1:])
	case "get":
		cmdSocialInstagramMediaGet(args[1:])
	case "create":
		cmdSocialInstagramMediaCreate(args[1:])
	case "publish":
		cmdSocialInstagramMediaPublish(args[1:])
	default:
		printUnknown("social instagram media", args[0])
	}
}

func cmdSocialInstagramMediaList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social instagram media list", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	igID := fs.String("ig-id", "", "instagram business account id (defaults from context)")
	fields := fs.String("fields", "id,caption,media_type,media_url,permalink,timestamp", "fields to request")
	limit := fs.Int("limit", 25, "max media records")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage("usage: si social instagram media list [<ig_id>] [--limit <n>] [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformInstagram,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
	})
	argID := ""
	if fs.NArg() == 1 {
		argID = fs.Arg(0)
	}
	id, err := socialResolveIDArg(firstNonEmpty(argID, *igID), runtime.InstagramBusinessID, "instagram business account id")
	if err != nil {
		fatal(err)
	}
	values := parseSocialParams(params)
	values["fields"] = strings.TrimSpace(*fields)
	if *limit > 0 {
		values["limit"] = strconv.Itoa(*limit)
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	resp, callErr := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/" + id + "/media",
		Params: values,
	})
	if callErr != nil {
		printSocialError(callErr)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialInstagramMediaGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social instagram media get", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	fields := fs.String("fields", "id,caption,media_type,media_url,permalink,thumbnail_url,timestamp", "fields to request")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si social instagram media get <media_id> [--json]")
		return
	}
	mediaID := strings.TrimSpace(fs.Arg(0))
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformInstagram,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
	})
	values := parseSocialParams(params)
	values["fields"] = strings.TrimSpace(*fields)
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/" + mediaID,
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialInstagramMediaCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "is-carousel-item": true})
	fs := flag.NewFlagSet("social instagram media create", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	igID := fs.String("ig-id", "", "instagram business account id (defaults from context)")
	imageURL := fs.String("image-url", "", "public image URL")
	videoURL := fs.String("video-url", "", "public video URL")
	mediaType := fs.String("media-type", "", "media type (IMAGE|VIDEO|REELS|STORIES|CAROUSEL)")
	carouselItem := fs.Bool("is-carousel-item", false, "mark media as carousel item")
	caption := fs.String("caption", "", "caption text")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 1 || (strings.TrimSpace(*imageURL) == "" && strings.TrimSpace(*videoURL) == "") {
		printUsage("usage: si social instagram media create [<ig_id>] --image-url <url>|--video-url <url> [--caption <text>] [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformInstagram,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
	})
	argID := ""
	if fs.NArg() == 1 {
		argID = fs.Arg(0)
	}
	id, err := socialResolveIDArg(firstNonEmpty(argID, *igID), runtime.InstagramBusinessID, "instagram business account id")
	if err != nil {
		fatal(err)
	}
	body := parseSocialJSONBody("", params)
	if value := strings.TrimSpace(*imageURL); value != "" {
		body["image_url"] = value
	}
	if value := strings.TrimSpace(*videoURL); value != "" {
		body["video_url"] = value
	}
	if value := strings.TrimSpace(*caption); value != "" {
		body["caption"] = value
	}
	if value := strings.TrimSpace(*mediaType); value != "" {
		body["media_type"] = strings.ToUpper(value)
	}
	if *carouselItem {
		body["is_carousel_item"] = true
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, callErr := socialDo(ctx, runtime, socialRequest{
		Method:   http.MethodPost,
		Path:     "/" + id + "/media",
		JSONBody: body,
	})
	if callErr != nil {
		printSocialError(callErr)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialInstagramMediaPublish(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social instagram media publish", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	igID := fs.String("ig-id", "", "instagram business account id (defaults from context)")
	creationID := fs.String("creation-id", "", "creation id returned by media create")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 1 || strings.TrimSpace(*creationID) == "" {
		printUsage("usage: si social instagram media publish [<ig_id>] --creation-id <id> [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformInstagram,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
	})
	argID := ""
	if fs.NArg() == 1 {
		argID = fs.Arg(0)
	}
	id, err := socialResolveIDArg(firstNonEmpty(argID, *igID), runtime.InstagramBusinessID, "instagram business account id")
	if err != nil {
		fatal(err)
	}
	body := parseSocialJSONBody("", params)
	body["creation_id"] = strings.TrimSpace(*creationID)
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	resp, callErr := socialDo(ctx, runtime, socialRequest{
		Method:   http.MethodPost,
		Path:     "/" + id + "/media_publish",
		JSONBody: body,
	})
	if callErr != nil {
		printSocialError(callErr)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialInstagramComment(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si social instagram comment <list|create> [flags]")
	if !routedOK {
		return
	}
	args = routedArgs
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdSocialInstagramCommentList(args[1:])
	case "create":
		cmdSocialInstagramCommentCreate(args[1:])
	default:
		printUnknown("social instagram comment", args[0])
	}
}

func cmdSocialInstagramCommentList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social instagram comment list", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	fields := fs.String("fields", "id,text,timestamp,username", "fields to request")
	limit := fs.Int("limit", 50, "max comments")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si social instagram comment list <media_id> [--json]")
		return
	}
	mediaID := strings.TrimSpace(fs.Arg(0))
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformInstagram,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
	})
	values := parseSocialParams(params)
	values["fields"] = strings.TrimSpace(*fields)
	if *limit > 0 {
		values["limit"] = strconv.Itoa(*limit)
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/" + mediaID + "/comments",
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialInstagramCommentCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social instagram comment create", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	message := fs.String("message", "", "comment text")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 || strings.TrimSpace(*message) == "" {
		printUsage("usage: si social instagram comment create <media_id> --message <text> [--json]")
		return
	}
	mediaID := strings.TrimSpace(fs.Arg(0))
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformInstagram,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
	})
	body := parseSocialJSONBody("", params)
	body["message"] = strings.TrimSpace(*message)
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method:   http.MethodPost,
		Path:     "/" + mediaID + "/comments",
		JSONBody: body,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialInstagramInsights(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si social instagram insights <account|media> [flags]")
	if !routedOK {
		return
	}
	args = routedArgs
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "account":
		cmdSocialInstagramInsightsAccount(args[1:])
	case "media":
		cmdSocialInstagramInsightsMedia(args[1:])
	default:
		printUnknown("social instagram insights", args[0])
	}
}

func cmdSocialInstagramInsightsAccount(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social instagram insights account", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	igID := fs.String("ig-id", "", "instagram business account id (defaults from context)")
	metric := fs.String("metric", "impressions,reach,profile_views", "comma-separated metrics")
	period := fs.String("period", "day", "period (day|week|days_28)")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage("usage: si social instagram insights account [<ig_id>] [--metric a,b] [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformInstagram,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
	})
	argID := ""
	if fs.NArg() == 1 {
		argID = fs.Arg(0)
	}
	id, err := socialResolveIDArg(firstNonEmpty(argID, *igID), runtime.InstagramBusinessID, "instagram business account id")
	if err != nil {
		fatal(err)
	}
	values := parseSocialParams(params)
	values["metric"] = strings.TrimSpace(*metric)
	values["period"] = strings.TrimSpace(*period)
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	resp, callErr := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/" + id + "/insights",
		Params: values,
	})
	if callErr != nil {
		printSocialError(callErr)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialInstagramInsightsMedia(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social instagram insights media", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	metric := fs.String("metric", "engagement,impressions,reach,saved,video_views", "comma-separated metrics")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si social instagram insights media <media_id> [--metric a,b] [--json]")
		return
	}
	mediaID := strings.TrimSpace(fs.Arg(0))
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformInstagram,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
	})
	values := parseSocialParams(params)
	values["metric"] = strings.TrimSpace(*metric)
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/" + mediaID + "/insights",
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}
