package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const socialFacebookUsageText = "usage: si social facebook <auth|context|doctor|profile|page|post|comment|insights|raw|report>"

func cmdSocialFacebook(args []string) {
	if len(args) == 0 {
		printUsage(socialFacebookUsageText)
		return
	}
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(socialFacebookUsageText)
	case "auth":
		cmdSocialPlatformAuth(socialPlatformFacebook, rest)
	case "context":
		cmdSocialPlatformContext(socialPlatformFacebook, rest)
	case "doctor":
		cmdSocialPlatformDoctor(socialPlatformFacebook, rest)
	case "profile", "me":
		cmdSocialFacebookProfile(rest)
	case "page", "pages":
		cmdSocialFacebookPage(rest)
	case "post", "posts":
		cmdSocialFacebookPost(rest)
	case "comment", "comments":
		cmdSocialFacebookComment(rest)
	case "insights", "insight":
		cmdSocialFacebookInsights(rest)
	case "raw":
		cmdSocialPlatformRaw(socialPlatformFacebook, rest)
	case "report":
		cmdSocialPlatformReport(socialPlatformFacebook, rest)
	default:
		printUnknown("social facebook", cmd)
		printUsage(socialFacebookUsageText)
	}
}

func cmdSocialFacebookProfile(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social facebook profile", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	fields := fs.String("fields", "id,name", "fields to request")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si social facebook profile [--fields id,name] [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformFacebook,
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

func cmdSocialFacebookPage(args []string) {
	if len(args) == 0 {
		printUsage("usage: si social facebook page <list|get> [flags]")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdSocialFacebookPageList(args[1:])
	case "get":
		cmdSocialFacebookPageGet(args[1:])
	default:
		printUnknown("social facebook page", args[0])
	}
}

func cmdSocialFacebookPageList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social facebook page list", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	fields := fs.String("fields", "id,name,category", "fields to request")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si social facebook page list [--fields id,name,category] [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformFacebook,
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
		Path:   "/me/accounts",
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialFacebookPageGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social facebook page get", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	pageID := fs.String("page-id", "", "facebook page id (defaults from context)")
	fields := fs.String("fields", "id,name,category,fan_count", "fields to request")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage("usage: si social facebook page get [<page_id>] [--fields id,name] [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformFacebook,
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
	id, err := socialResolveIDArg(firstNonEmpty(argID, *pageID), runtime.FacebookPageID, "facebook page id")
	if err != nil {
		fatal(err)
	}
	values := parseSocialParams(params)
	values["fields"] = strings.TrimSpace(*fields)
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	resp, callErr := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/" + strings.TrimSpace(id),
		Params: values,
	})
	if callErr != nil {
		printSocialError(callErr)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialFacebookPost(args []string) {
	if len(args) == 0 {
		printUsage("usage: si social facebook post <list|get|create|delete> [flags]")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdSocialFacebookPostList(args[1:])
	case "get":
		cmdSocialFacebookPostGet(args[1:])
	case "create":
		cmdSocialFacebookPostCreate(args[1:])
	case "delete", "remove":
		cmdSocialFacebookPostDelete(args[1:])
	default:
		printUnknown("social facebook post", args[0])
	}
}

func cmdSocialFacebookPostList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social facebook post list", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	pageID := fs.String("page-id", "", "facebook page id (defaults from context)")
	limit := fs.Int("limit", 25, "max posts to return")
	fields := fs.String("fields", "id,message,created_time,permalink_url", "fields to request")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage("usage: si social facebook post list [<page_id>] [--limit <n>] [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformFacebook,
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
	id, err := socialResolveIDArg(firstNonEmpty(argID, *pageID), runtime.FacebookPageID, "facebook page id")
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
		Path:   "/" + strings.TrimSpace(id) + "/feed",
		Params: values,
	})
	if callErr != nil {
		printSocialError(callErr)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialFacebookPostGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social facebook post get", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	fields := fs.String("fields", "id,message,created_time,permalink_url", "fields to request")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si social facebook post get <post_id> [--json]")
		return
	}
	postID := strings.TrimSpace(fs.Arg(0))
	if postID == "" {
		fatal(fmt.Errorf("post id is required"))
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformFacebook,
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
		Path:   "/" + postID,
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialFacebookPostCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true, "published": true})
	fs := flag.NewFlagSet("social facebook post create", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	pageID := fs.String("page-id", "", "facebook page id (defaults from context)")
	message := fs.String("message", "", "post text message")
	link := fs.String("link", "", "optional link")
	published := fs.Bool("published", true, "publish immediately")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra form parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 1 || strings.TrimSpace(*message) == "" {
		printUsage("usage: si social facebook post create [<page_id>] --message <text> [--link <url>] [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformFacebook,
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
	id, err := socialResolveIDArg(firstNonEmpty(argID, *pageID), runtime.FacebookPageID, "facebook page id")
	if err != nil {
		fatal(err)
	}
	body := parseSocialJSONBody("", params)
	body["message"] = strings.TrimSpace(*message)
	if strings.TrimSpace(*link) != "" {
		body["link"] = strings.TrimSpace(*link)
	}
	body["published"] = *published
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	resp, callErr := socialDo(ctx, runtime, socialRequest{
		Method:   http.MethodPost,
		Path:     "/" + strings.TrimSpace(id) + "/feed",
		JSONBody: body,
	})
	if callErr != nil {
		printSocialError(callErr)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialFacebookPostDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social facebook post delete", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si social facebook post delete <post_id> [--json]")
		return
	}
	postID := strings.TrimSpace(fs.Arg(0))
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformFacebook,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
	})
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodDelete,
		Path:   "/" + postID,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialFacebookComment(args []string) {
	if len(args) == 0 {
		printUsage("usage: si social facebook comment <list|create> [flags]")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdSocialFacebookCommentList(args[1:])
	case "create":
		cmdSocialFacebookCommentCreate(args[1:])
	default:
		printUnknown("social facebook comment", args[0])
	}
}

func cmdSocialFacebookCommentList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social facebook comment list", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	fields := fs.String("fields", "id,message,created_time,from", "fields to request")
	limit := fs.Int("limit", 50, "max comments to return")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si social facebook comment list <object_id> [--json]")
		return
	}
	objectID := strings.TrimSpace(fs.Arg(0))
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformFacebook,
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
		Path:   "/" + objectID + "/comments",
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialFacebookCommentCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social facebook comment create", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	message := fs.String("message", "", "comment message")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra form parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 || strings.TrimSpace(*message) == "" {
		printUsage("usage: si social facebook comment create <object_id> --message <text> [--json]")
		return
	}
	objectID := strings.TrimSpace(fs.Arg(0))
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformFacebook,
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
		Path:     "/" + objectID + "/comments",
		JSONBody: body,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialFacebookInsights(args []string) {
	if len(args) == 0 {
		printUsage("usage: si social facebook insights <page|post> [flags]")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "page":
		cmdSocialFacebookInsightsPage(args[1:])
	case "post":
		cmdSocialFacebookInsightsPost(args[1:])
	default:
		printUnknown("social facebook insights", args[0])
	}
}

func cmdSocialFacebookInsightsPage(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social facebook insights page", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	pageID := fs.String("page-id", "", "facebook page id (defaults from context)")
	metrics := fs.String("metric", "page_impressions,page_engaged_users,page_post_engagements", "comma-separated insight metrics")
	period := fs.String("period", "day", "insights period (day|week|days_28|lifetime)")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage("usage: si social facebook insights page [<page_id>] [--metric a,b] [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformFacebook,
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
	id, err := socialResolveIDArg(firstNonEmpty(argID, *pageID), runtime.FacebookPageID, "facebook page id")
	if err != nil {
		fatal(err)
	}
	values := parseSocialParams(params)
	values["metric"] = strings.TrimSpace(*metrics)
	values["period"] = strings.TrimSpace(*period)
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	resp, callErr := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/" + strings.TrimSpace(id) + "/insights",
		Params: values,
	})
	if callErr != nil {
		printSocialError(callErr)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialFacebookInsightsPost(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social facebook insights post", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	metrics := fs.String("metric", "post_impressions,post_clicks,post_engaged_users", "comma-separated insight metrics")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si social facebook insights post <post_id> [--metric a,b] [--json]")
		return
	}
	postID := strings.TrimSpace(fs.Arg(0))
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformFacebook,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
	})
	values := parseSocialParams(params)
	values["metric"] = strings.TrimSpace(*metrics)
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/" + postID + "/insights",
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}
