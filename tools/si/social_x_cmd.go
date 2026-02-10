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

const socialXUsageText = "usage: si social x <auth|context|doctor|user|tweet|search|raw|report>"

func cmdSocialX(args []string) {
	if len(args) == 0 {
		printUsage(socialXUsageText)
		return
	}
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(socialXUsageText)
	case "auth":
		cmdSocialPlatformAuth(socialPlatformX, rest)
	case "context":
		cmdSocialPlatformContext(socialPlatformX, rest)
	case "doctor":
		cmdSocialPlatformDoctor(socialPlatformX, rest)
	case "user", "users":
		cmdSocialXUser(rest)
	case "tweet", "tweets":
		cmdSocialXTweet(rest)
	case "search":
		cmdSocialXSearch(rest)
	case "raw":
		cmdSocialPlatformRaw(socialPlatformX, rest)
	case "report":
		cmdSocialPlatformReport(socialPlatformX, rest)
	default:
		printUnknown("social x", cmd)
		printUsage(socialXUsageText)
	}
}

func cmdSocialXUser(args []string) {
	if len(args) == 0 {
		printUsage("usage: si social x user <me|get|by-username> [flags]")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "me":
		cmdSocialXUserMe(args[1:])
	case "get":
		cmdSocialXUserGet(args[1:])
	case "by-username", "username":
		cmdSocialXUserByUsername(args[1:])
	default:
		printUnknown("social x user", args[0])
	}
}

func cmdSocialXUserMe(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social x user me", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override bearer token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	userFields := fs.String("user-fields", "id,name,username,verified,public_metrics", "comma-separated user fields")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si social x user me [--user-fields ...] [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformX,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	values := parseSocialParams(params)
	if strings.TrimSpace(*userFields) != "" {
		values["user.fields"] = strings.TrimSpace(*userFields)
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/users/me",
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialXUserGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social x user get", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override bearer token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	userFields := fs.String("user-fields", "id,name,username,verified,public_metrics", "comma-separated user fields")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si social x user get <user_id> [--json]")
		return
	}
	userID := strings.TrimSpace(fs.Arg(0))
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformX,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	values := parseSocialParams(params)
	values["user.fields"] = strings.TrimSpace(*userFields)
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/users/" + userID,
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialXUserByUsername(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social x user by-username", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override bearer token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	username := fs.String("username", "", "username (without @, defaults from context)")
	userFields := fs.String("user-fields", "id,name,username,verified,public_metrics", "comma-separated user fields")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage("usage: si social x user by-username [<username>] [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformX,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	argUsername := ""
	if fs.NArg() == 1 {
		argUsername = fs.Arg(0)
	}
	selected := strings.TrimPrefix(firstNonEmpty(argUsername, *username, runtime.XUsername), "@")
	if strings.TrimSpace(selected) == "" {
		fatal(fmt.Errorf("x username is required"))
	}
	values := parseSocialParams(params)
	values["user.fields"] = strings.TrimSpace(*userFields)
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/users/by/username/" + selected,
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialXTweet(args []string) {
	if len(args) == 0 {
		printUsage("usage: si social x tweet <get|create|delete> [flags]")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "get":
		cmdSocialXTweetGet(args[1:])
	case "create", "post":
		cmdSocialXTweetCreate(args[1:])
	case "delete", "remove":
		cmdSocialXTweetDelete(args[1:])
	default:
		printUnknown("social x tweet", args[0])
	}
}

func cmdSocialXTweetGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social x tweet get", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override bearer token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	tweetFields := fs.String("tweet-fields", "id,text,author_id,created_at,public_metrics", "comma-separated tweet fields")
	expansions := fs.String("expansions", "author_id", "comma-separated expansions")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si social x tweet get <tweet_id> [--json]")
		return
	}
	tweetID := strings.TrimSpace(fs.Arg(0))
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformX,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	values := parseSocialParams(params)
	values["tweet.fields"] = strings.TrimSpace(*tweetFields)
	if strings.TrimSpace(*expansions) != "" {
		values["expansions"] = strings.TrimSpace(*expansions)
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/tweets/" + tweetID,
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialXTweetCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social x tweet create", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override bearer token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	text := fs.String("text", "", "tweet text")
	replyTo := fs.String("reply-to", "", "tweet id to reply to")
	quoteID := fs.String("quote-tweet-id", "", "quoted tweet id")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*text) == "" {
		printUsage("usage: si social x tweet create --text <tweet text> [--reply-to <id>] [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformX,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	body := parseSocialJSONBody("", params)
	body["text"] = strings.TrimSpace(*text)
	if value := strings.TrimSpace(*replyTo); value != "" {
		body["reply"] = map[string]any{"in_reply_to_tweet_id": value}
	}
	if value := strings.TrimSpace(*quoteID); value != "" {
		body["quote_tweet_id"] = value
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method:   http.MethodPost,
		Path:     "/tweets",
		JSONBody: body,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialXTweetDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social x tweet delete", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override bearer token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si social x tweet delete <tweet_id> [--json]")
		return
	}
	tweetID := strings.TrimSpace(fs.Arg(0))
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformX,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodDelete,
		Path:   "/tweets/" + tweetID,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialXSearch(args []string) {
	if len(args) == 0 {
		printUsage("usage: si social x search recent --query <q> [flags]")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "recent", "list":
		cmdSocialXSearchRecent(args[1:])
	default:
		printUnknown("social x search", args[0])
	}
}

func cmdSocialXSearchRecent(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social x search recent", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override bearer token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	query := fs.String("query", "", "search query")
	maxResults := fs.Int("max-results", 10, "results per page (10-100)")
	nextToken := fs.String("next-token", "", "pagination token")
	tweetFields := fs.String("tweet-fields", "id,text,author_id,created_at,public_metrics", "comma-separated tweet fields")
	userFields := fs.String("user-fields", "id,name,username,verified,public_metrics", "comma-separated user fields")
	expansions := fs.String("expansions", "author_id", "comma-separated expansions")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*query) == "" {
		printUsage("usage: si social x search recent --query <q> [--max-results <n>] [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformX,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	values := parseSocialParams(params)
	values["query"] = strings.TrimSpace(*query)
	if *maxResults > 0 {
		if *maxResults < 10 {
			*maxResults = 10
		}
		if *maxResults > 100 {
			*maxResults = 100
		}
		values["max_results"] = strconv.Itoa(*maxResults)
	}
	if strings.TrimSpace(*nextToken) != "" {
		values["next_token"] = strings.TrimSpace(*nextToken)
	}
	values["tweet.fields"] = strings.TrimSpace(*tweetFields)
	values["user.fields"] = strings.TrimSpace(*userFields)
	if strings.TrimSpace(*expansions) != "" {
		values["expansions"] = strings.TrimSpace(*expansions)
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/tweets/search/recent",
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}
