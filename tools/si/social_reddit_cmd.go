package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const socialRedditUsageText = "usage: si social reddit <auth|context|doctor|profile|subreddit|post|comment|raw|report>"

func cmdSocialReddit(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, socialRedditUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(socialRedditUsageText)
	case "auth":
		cmdSocialPlatformAuth(socialPlatformReddit, rest)
	case "context":
		cmdSocialPlatformContext(socialPlatformReddit, rest)
	case "doctor":
		cmdSocialPlatformDoctor(socialPlatformReddit, rest)
	case "profile", "me", "user":
		cmdSocialRedditProfile(rest)
	case "subreddit", "sr":
		cmdSocialRedditSubreddit(rest)
	case "post", "posts", "submission":
		cmdSocialRedditPost(rest)
	case "comment", "comments":
		cmdSocialRedditComment(rest)
	case "raw":
		cmdSocialPlatformRaw(socialPlatformReddit, rest)
	case "report":
		cmdSocialPlatformReport(socialPlatformReddit, rest)
	default:
		printUnknown("social reddit", cmd)
		printUsage(socialRedditUsageText)
	}
}

func cmdSocialRedditProfile(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social reddit profile", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si social reddit profile [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformReddit,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	values := parseSocialParams(params)
	if strings.TrimSpace(values["raw_json"]) == "" {
		values["raw_json"] = "1"
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/api/v1/me",
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialRedditSubreddit(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si social reddit subreddit <get|posts> [flags]")
	if !routedOK {
		return
	}
	args = routedArgs
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "get", "about":
		cmdSocialRedditSubredditGet(args[1:])
	case "posts", "list", "hot", "new", "top", "rising", "controversial":
		cmdSocialRedditSubredditPosts(args)
	default:
		printUnknown("social reddit subreddit", args[0])
	}
}

func cmdSocialRedditSubredditGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social reddit subreddit get", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	name := fs.String("name", "", "subreddit name")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage("usage: si social reddit subreddit get [<subreddit>] [--json]")
		return
	}
	argSubreddit := ""
	if fs.NArg() == 1 {
		argSubreddit = fs.Arg(0)
	}
	subreddit := socialNormalizeRedditSubreddit(firstNonEmpty(argSubreddit, *name))
	if subreddit == "" {
		fatal(fmt.Errorf("subreddit is required"))
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformReddit,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	values := parseSocialParams(params)
	if strings.TrimSpace(values["raw_json"]) == "" {
		values["raw_json"] = "1"
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/r/" + url.PathEscape(subreddit) + "/about",
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialRedditSubredditPosts(args []string) {
	if len(args) > 0 {
		sub := strings.ToLower(strings.TrimSpace(args[0]))
		switch sub {
		case "posts", "list":
			args = args[1:]
		case "hot", "new", "top", "rising", "controversial":
			args = append([]string{"--sort", sub}, args[1:]...)
		}
	}
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social reddit subreddit posts", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	subredditFlag := fs.String("subreddit", "", "subreddit name")
	sort := fs.String("sort", "hot", "listing sort (hot|new|top|rising|controversial)")
	limit := fs.Int("limit", 25, "result limit (1-100)")
	after := fs.String("after", "", "pagination token")
	before := fs.String("before", "", "pagination token")
	timeRange := fs.String("time", "", "time range for top/controversial (hour|day|week|month|year|all)")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage("usage: si social reddit subreddit posts [<subreddit>] [--sort hot|new|top|rising|controversial] [--limit <n>] [--json]")
		return
	}
	argSubreddit := ""
	if fs.NArg() == 1 {
		argSubreddit = fs.Arg(0)
	}
	subreddit := socialNormalizeRedditSubreddit(firstNonEmpty(argSubreddit, *subredditFlag))
	if subreddit == "" {
		fatal(fmt.Errorf("subreddit is required"))
	}
	sortValue, ok := socialNormalizeRedditSort(*sort)
	if !ok {
		fatal(fmt.Errorf("invalid --sort %q (expected hot|new|top|rising|controversial)", *sort))
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformReddit,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	values := parseSocialParams(params)
	if strings.TrimSpace(values["raw_json"]) == "" {
		values["raw_json"] = "1"
	}
	if *limit > 0 {
		if *limit < 1 {
			*limit = 1
		}
		if *limit > 100 {
			*limit = 100
		}
		values["limit"] = strconv.Itoa(*limit)
	}
	if value := strings.TrimSpace(*after); value != "" {
		values["after"] = value
	}
	if value := strings.TrimSpace(*before); value != "" {
		values["before"] = value
	}
	if (sortValue == "top" || sortValue == "controversial") && strings.TrimSpace(*timeRange) != "" {
		values["t"] = strings.TrimSpace(*timeRange)
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/r/" + url.PathEscape(subreddit) + "/" + sortValue,
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialRedditPost(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si social reddit post <get|create|delete> [flags]")
	if !routedOK {
		return
	}
	args = routedArgs
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "get":
		cmdSocialRedditPostGet(args[1:])
	case "create", "submit":
		cmdSocialRedditPostCreate(args[1:])
	case "delete", "remove":
		cmdSocialRedditPostDelete(args[1:])
	default:
		printUnknown("social reddit post", args[0])
	}
}

func cmdSocialRedditPostGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social reddit post get", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	id := fs.String("id", "", "post id or fullname (t3_xxx)")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage("usage: si social reddit post get <post_id_or_fullname> [--json]")
		return
	}
	argID := ""
	if fs.NArg() == 1 {
		argID = fs.Arg(0)
	}
	thingID := socialNormalizeRedditThingID(firstNonEmpty(argID, *id), "t3")
	if thingID == "" {
		fatal(fmt.Errorf("post id is required"))
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformReddit,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	values := parseSocialParams(params)
	if strings.TrimSpace(values["raw_json"]) == "" {
		values["raw_json"] = "1"
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/by_id/" + url.PathEscape(thingID),
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialRedditPostCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "send-replies": true, "resubmit": true, "nsfw": true, "spoiler": true})
	fs := flag.NewFlagSet("social reddit post create", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	subreddit := fs.String("subreddit", "", "subreddit name")
	title := fs.String("title", "", "post title")
	kind := fs.String("kind", "self", "post kind (self|link)")
	text := fs.String("text", "", "self post body")
	linkURL := fs.String("url", "", "link URL for link posts")
	sendReplies := fs.Bool("send-replies", true, "notify inbox for replies")
	resubmit := fs.Bool("resubmit", false, "allow resubmitting same URL")
	nsfw := fs.Bool("nsfw", false, "mark post nsfw")
	spoiler := fs.Bool("spoiler", false, "mark post spoiler")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra form parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si social reddit post create --subreddit <name> --title <title> [--kind self|link] [--text <body>] [--url <link>] [--json]")
		return
	}
	kindValue := strings.ToLower(strings.TrimSpace(*kind))
	if kindValue == "" {
		kindValue = "self"
	}
	if kindValue != "self" && kindValue != "link" {
		fatal(fmt.Errorf("invalid --kind %q (expected self|link)", *kind))
	}
	subredditValue := socialNormalizeRedditSubreddit(*subreddit)
	if subredditValue == "" {
		fatal(fmt.Errorf("--subreddit is required"))
	}
	if strings.TrimSpace(*title) == "" {
		fatal(fmt.Errorf("--title is required"))
	}
	if kindValue == "link" && strings.TrimSpace(*linkURL) == "" {
		fatal(fmt.Errorf("--url is required when --kind=link"))
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformReddit,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	form := url.Values{}
	form.Set("api_type", "json")
	form.Set("raw_json", "1")
	form.Set("sr", subredditValue)
	form.Set("title", strings.TrimSpace(*title))
	form.Set("kind", kindValue)
	form.Set("resubmit", strconv.FormatBool(*resubmit))
	form.Set("sendreplies", strconv.FormatBool(*sendReplies))
	if kindValue == "self" && strings.TrimSpace(*text) != "" {
		form.Set("text", strings.TrimSpace(*text))
	}
	if kindValue == "link" {
		form.Set("url", strings.TrimSpace(*linkURL))
	}
	if *nsfw {
		form.Set("nsfw", "true")
	}
	if *spoiler {
		form.Set("spoiler", "true")
	}
	for key, value := range parseSocialParams(params) {
		form.Set(key, value)
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method:      http.MethodPost,
		Path:        "/api/submit",
		RawBody:     form.Encode(),
		ContentType: "application/x-www-form-urlencoded",
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialRedditPostDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social reddit post delete", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra form parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si social reddit post delete <post_id_or_fullname> [--json]")
		return
	}
	thingID := socialNormalizeRedditThingID(fs.Arg(0), "t3")
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformReddit,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	form := url.Values{}
	form.Set("api_type", "json")
	form.Set("raw_json", "1")
	form.Set("id", thingID)
	for key, value := range parseSocialParams(params) {
		form.Set(key, value)
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method:      http.MethodPost,
		Path:        "/api/del",
		RawBody:     form.Encode(),
		ContentType: "application/x-www-form-urlencoded",
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialRedditComment(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si social reddit comment <list|create|delete> [flags]")
	if !routedOK {
		return
	}
	args = routedArgs
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list", "get":
		cmdSocialRedditCommentList(args[1:])
	case "create", "add", "reply":
		cmdSocialRedditCommentCreate(args[1:])
	case "delete", "remove":
		cmdSocialRedditCommentDelete(args[1:])
	default:
		printUnknown("social reddit comment", args[0])
	}
}

func cmdSocialRedditCommentList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social reddit comment list", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	postID := fs.String("post-id", "", "post id or fullname (t3_xxx)")
	limit := fs.Int("limit", 25, "max comments")
	depth := fs.Int("depth", 1, "comment depth")
	sort := fs.String("sort", "confidence", "comment sort")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage("usage: si social reddit comment list <post_id_or_fullname> [--limit <n>] [--json]")
		return
	}
	argPostID := ""
	if fs.NArg() == 1 {
		argPostID = fs.Arg(0)
	}
	thingID := socialNormalizeRedditThingID(firstNonEmpty(argPostID, *postID), "t3")
	if thingID == "" {
		fatal(fmt.Errorf("post id is required"))
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformReddit,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	values := parseSocialParams(params)
	if strings.TrimSpace(values["raw_json"]) == "" {
		values["raw_json"] = "1"
	}
	if *limit > 0 {
		values["limit"] = strconv.Itoa(*limit)
	}
	if *depth > 0 {
		values["depth"] = strconv.Itoa(*depth)
	}
	if value := strings.TrimSpace(*sort); value != "" {
		values["sort"] = value
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/comments/" + url.PathEscape(strings.TrimPrefix(thingID, "t3_")),
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialRedditCommentCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social reddit comment create", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	text := fs.String("text", "", "comment text")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra form parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 || strings.TrimSpace(*text) == "" {
		printUsage("usage: si social reddit comment create <thing_id> --text <text> [--json]")
		return
	}
	thingID := socialNormalizeRedditThingID(fs.Arg(0), "t3")
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformReddit,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	form := url.Values{}
	form.Set("api_type", "json")
	form.Set("raw_json", "1")
	form.Set("thing_id", thingID)
	form.Set("text", strings.TrimSpace(*text))
	for key, value := range parseSocialParams(params) {
		form.Set(key, value)
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method:      http.MethodPost,
		Path:        "/api/comment",
		RawBody:     form.Encode(),
		ContentType: "application/x-www-form-urlencoded",
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialRedditCommentDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social reddit comment delete", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra form parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si social reddit comment delete <comment_id_or_fullname> [--json]")
		return
	}
	thingID := socialNormalizeRedditThingID(fs.Arg(0), "t1")
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformReddit,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	form := url.Values{}
	form.Set("api_type", "json")
	form.Set("raw_json", "1")
	form.Set("id", thingID)
	for key, value := range parseSocialParams(params) {
		form.Set(key, value)
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method:      http.MethodPost,
		Path:        "/api/del",
		RawBody:     form.Encode(),
		ContentType: "application/x-www-form-urlencoded",
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func socialNormalizeRedditSubreddit(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.TrimPrefix(value, "https://www.reddit.com/")
	value = strings.TrimPrefix(value, "https://reddit.com/")
	value = strings.TrimPrefix(value, "r/")
	value = strings.Trim(value, "/")
	if strings.Contains(value, "/") {
		parts := strings.Split(value, "/")
		for i, part := range parts {
			if strings.EqualFold(strings.TrimSpace(part), "r") && i+1 < len(parts) {
				value = strings.TrimSpace(parts[i+1])
				break
			}
		}
	}
	return strings.TrimSpace(value)
}

func socialNormalizeRedditSort(raw string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "hot", true
	}
	switch value {
	case "hot", "new", "top", "rising", "controversial":
		return value, true
	default:
		return "", false
	}
}

func socialNormalizeRedditThingID(raw string, defaultKind string) string {
	value := strings.TrimSpace(raw)
	value = strings.TrimSpace(strings.Trim(value, "/"))
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") {
		if parsed, err := url.Parse(value); err == nil {
			value = strings.Trim(parsed.Path, "/")
		}
	}
	if strings.HasPrefix(value, "t1_") || strings.HasPrefix(value, "t2_") || strings.HasPrefix(value, "t3_") || strings.HasPrefix(value, "t4_") || strings.HasPrefix(value, "t5_") || strings.HasPrefix(value, "t6_") {
		return value
	}
	parts := strings.Split(value, "/")
	if len(parts) >= 3 && strings.EqualFold(parts[0], "comments") {
		value = strings.TrimSpace(parts[1])
	} else if len(parts) > 0 {
		value = strings.TrimSpace(parts[len(parts)-1])
	}
	if value == "" {
		return ""
	}
	if strings.Contains(value, "_") {
		head := strings.SplitN(value, "_", 2)[0]
		if len(head) == 2 && strings.HasPrefix(head, "t") {
			return value
		}
	}
	kind := strings.ToLower(strings.TrimSpace(defaultKind))
	if kind == "" {
		kind = "t3"
	}
	return kind + "_" + value
}
