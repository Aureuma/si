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

const socialLinkedInUsageText = "usage: si social linkedin <auth|context|doctor|profile|organization|post|raw|report>"

func cmdSocialLinkedIn(args []string) {
	if len(args) == 0 {
		printUsage(socialLinkedInUsageText)
		return
	}
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(socialLinkedInUsageText)
	case "auth":
		cmdSocialPlatformAuth(socialPlatformLinkedIn, rest)
	case "context":
		cmdSocialPlatformContext(socialPlatformLinkedIn, rest)
	case "doctor":
		cmdSocialPlatformDoctor(socialPlatformLinkedIn, rest)
	case "profile", "me":
		cmdSocialLinkedInProfile(rest)
	case "organization", "org":
		cmdSocialLinkedInOrganization(rest)
	case "post", "posts", "ugc":
		cmdSocialLinkedInPost(rest)
	case "raw":
		cmdSocialPlatformRaw(socialPlatformLinkedIn, rest)
	case "report":
		cmdSocialPlatformReport(socialPlatformLinkedIn, rest)
	default:
		printUnknown("social linkedin", cmd)
		printUsage(socialLinkedInUsageText)
	}
}

func cmdSocialLinkedInProfile(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social linkedin profile", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	projection := fs.String("projection", "", "optional linkedin projection expression")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si social linkedin profile [--projection (id,localizedFirstName,localizedLastName)] [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformLinkedIn,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	values := parseSocialParams(params)
	if strings.TrimSpace(*projection) != "" {
		values["projection"] = strings.TrimSpace(*projection)
	}
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

func cmdSocialLinkedInOrganization(args []string) {
	if len(args) == 0 {
		printUsage("usage: si social linkedin organization get <org_id|urn>")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "get":
		cmdSocialLinkedInOrganizationGet(args[1:])
	default:
		printUnknown("social linkedin organization", args[0])
	}
}

func cmdSocialLinkedInOrganizationGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social linkedin organization get", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	org := fs.String("org", "", "organization id or urn (defaults from context)")
	projection := fs.String("projection", "", "optional projection")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 1 {
		printUsage("usage: si social linkedin organization get [<org_id|urn>] [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformLinkedIn,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	argOrg := ""
	if fs.NArg() == 1 {
		argOrg = fs.Arg(0)
	}
	value := strings.TrimSpace(firstNonEmpty(argOrg, *org, runtime.LinkedInOrganizationURN))
	if value == "" {
		fatal(fmt.Errorf("organization id or urn is required"))
	}
	orgID := value
	if strings.HasPrefix(value, "urn:li:organization:") {
		orgID = strings.TrimPrefix(value, "urn:li:organization:")
	}
	paramsMap := parseSocialParams(params)
	if strings.TrimSpace(*projection) != "" {
		paramsMap["projection"] = strings.TrimSpace(*projection)
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/organizations/" + strings.TrimSpace(orgID),
		Params: paramsMap,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialLinkedInPost(args []string) {
	if len(args) == 0 {
		printUsage("usage: si social linkedin post <list|get|create|delete> [flags]")
		return
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		cmdSocialLinkedInPostList(args[1:])
	case "get":
		cmdSocialLinkedInPostGet(args[1:])
	case "create":
		cmdSocialLinkedInPostCreate(args[1:])
	case "delete", "remove":
		cmdSocialLinkedInPostDelete(args[1:])
	default:
		printUnknown("social linkedin post", args[0])
	}
}

func cmdSocialLinkedInPostList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social linkedin post list", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	author := fs.String("author", "", "author urn (defaults from context person/org)")
	count := fs.Int("count", 20, "number of posts to return")
	start := fs.Int("start", 0, "offset")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si social linkedin post list [--author <urn>] [--count <n>] [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformLinkedIn,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	selectedAuthor := strings.TrimSpace(firstNonEmpty(*author, runtime.LinkedInPersonURN, runtime.LinkedInOrganizationURN))
	if selectedAuthor == "" {
		fatal(fmt.Errorf("author urn is required (set --author or configure linkedin_person_urn/linkedin_organization_urn)"))
	}
	values := parseSocialParams(params)
	values["q"] = "authors"
	values["authors"] = "List(" + selectedAuthor + ")"
	if *count > 0 {
		values["count"] = strconv.Itoa(*count)
	}
	if *start > 0 {
		values["start"] = strconv.Itoa(*start)
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/ugcPosts",
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialLinkedInPostGet(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social linkedin post get", flag.ExitOnError)
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
	if fs.NArg() != 1 {
		printUsage("usage: si social linkedin post get <ugc_post_urn_or_id> [--json]")
		return
	}
	postID := strings.TrimSpace(fs.Arg(0))
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformLinkedIn,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	values := parseSocialParams(params)
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodGet,
		Path:   "/ugcPosts/" + url.PathEscape(postID),
		Params: values,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialLinkedInPostCreate(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social linkedin post create", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	author := fs.String("author", "", "author urn (defaults from context person/org)")
	text := fs.String("text", "", "post text")
	articleURL := fs.String("article-url", "", "optional article URL")
	visibility := fs.String("visibility", "PUBLIC", "PUBLIC or CONNECTIONS")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "extra body parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*text) == "" {
		printUsage("usage: si social linkedin post create --text <text> [--author <urn>] [--article-url <url>] [--json]")
		return
	}
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformLinkedIn,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	selectedAuthor := strings.TrimSpace(firstNonEmpty(*author, runtime.LinkedInPersonURN, runtime.LinkedInOrganizationURN))
	if selectedAuthor == "" {
		fatal(fmt.Errorf("author urn is required (set --author or configure linkedin_person_urn/linkedin_organization_urn)"))
	}
	body := map[string]any{
		"author":         selectedAuthor,
		"lifecycleState": "PUBLISHED",
		"specificContent": map[string]any{
			"com.linkedin.ugc.ShareContent": map[string]any{
				"shareCommentary": map[string]any{
					"text": strings.TrimSpace(*text),
				},
				"shareMediaCategory": "NONE",
			},
		},
		"visibility": map[string]any{
			"com.linkedin.ugc.MemberNetworkVisibility": strings.ToUpper(strings.TrimSpace(*visibility)),
		},
	}
	if value := strings.TrimSpace(*articleURL); value != "" {
		body["specificContent"] = map[string]any{
			"com.linkedin.ugc.ShareContent": map[string]any{
				"shareCommentary": map[string]any{
					"text": strings.TrimSpace(*text),
				},
				"shareMediaCategory": "ARTICLE",
				"media": []map[string]any{{
					"status":      "READY",
					"originalUrl": value,
				}},
			},
		}
	}
	for key, value := range parseSocialJSONBody("", params) {
		body[key] = value
	}
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method:   http.MethodPost,
		Path:     "/ugcPosts",
		JSONBody: body,
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}

func cmdSocialLinkedInPostDelete(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("social linkedin post delete", flag.ExitOnError)
	account := fs.String("account", "", "account alias")
	env := fs.String("env", "", "environment (prod|staging|dev)")
	token := fs.String("token", "", "override access token")
	baseURL := fs.String("base-url", "", "api base url")
	apiVersion := fs.String("api-version", "", "api version")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		printUsage("usage: si social linkedin post delete <ugc_post_urn_or_id> [--json]")
		return
	}
	postID := strings.TrimSpace(fs.Arg(0))
	runtime := mustSocialRuntime(socialRuntimeContextInput{
		Platform:    socialPlatformLinkedIn,
		AccountFlag: *account,
		EnvFlag:     *env,
		BaseURLFlag: *baseURL,
		APIVerFlag:  *apiVersion,
		TokenFlag:   *token,
		AuthFlag:    "bearer",
	})
	printSocialContextBanner(runtime, *jsonOut)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	resp, err := socialDo(ctx, runtime, socialRequest{
		Method: http.MethodDelete,
		Path:   "/ugcPosts/" + url.PathEscape(postID),
	})
	if err != nil {
		printSocialError(err)
		return
	}
	printSocialResponse(resp, *jsonOut, *raw)
}
