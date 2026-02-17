package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"si/tools/si/internal/httpx"
)

const publishUsageText = "usage: si publish <catalog|devto|hashnode|reddit|hackernews|producthunt>"

const (
	publishDistributionKitURL = "https://distributionkit.com/"
	publishDevToBaseURL       = "https://dev.to"
	publishHashnodeBaseURL    = "https://gql.hashnode.com"
	publishRedditBaseURL      = "https://oauth.reddit.com"
	publishHNBaseURL          = "https://hacker-news.firebaseio.com"
	publishProductHuntURL     = "https://api.producthunt.com/v2/api/graphql"
)

var (
	publishCatalogRowRe  = regexp.MustCompile(`(?s)<tr class="platform-row[^>]*>.*?</tr>`)
	publishCatalogAttrRe = regexp.MustCompile(`data-([a-z-]+)="([^"]*)"`)
	publishCatalogDescRe = regexp.MustCompile(`(?s)<div class="text-\[13px\][^"]*">(.*?)</div>`)
	publishTagStripRe    = regexp.MustCompile(`(?s)<[^>]+>`)
)

type publishCatalogEntry struct {
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Score        string `json:"score,omitempty"`
	Pricing      string `json:"pricing"`
	Account      string `json:"account"`
	URL          string `json:"url"`
	Description  string `json:"description,omitempty"`
	APIProvider  string `json:"api_provider,omitempty"`
	APIAvailable bool   `json:"api_available"`
}

type publishHTTPResponse struct {
	StatusCode int               `json:"status_code"`
	Status     string            `json:"status"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	Data       any               `json:"data,omitempty"`
}

func cmdPublish(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, publishUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(publishUsageText)
	case "catalog", "platforms":
		cmdPublishCatalog(rest)
	case "devto", "dev.to":
		cmdPublishDevTo(rest)
	case "hashnode":
		cmdPublishHashnode(rest)
	case "reddit":
		cmdPublishReddit(rest)
	case "hackernews", "hn":
		cmdPublishHackerNews(rest)
	case "producthunt", "ph":
		cmdPublishProductHunt(rest)
	default:
		printUnknown("publish", sub)
		printUsage(publishUsageText)
	}
}

func cmdPublishCatalog(args []string) {
	if len(args) == 0 {
		cmdPublishCatalogList(nil)
		return
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "list":
		cmdPublishCatalogList(args[1:])
	default:
		printUnknown("publish catalog", sub)
		printUsage("usage: si publish catalog [list] [--pricing free-at-least|free|free+paid|paid|all] [--query term] [--limit n] [--json]")
	}
}

func cmdPublishCatalogList(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("publish catalog list", flag.ExitOnError)
	sourceURL := fs.String("source-url", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_CATALOG_URL")), publishDistributionKitURL), "catalog source url")
	pricing := fs.String("pricing", "free-at-least", "pricing filter: free-at-least|free|free+paid|paid|all")
	query := fs.String("query", "", "search term")
	limit := fs.Int("limit", 0, "max rows")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si publish catalog [list] [--pricing free-at-least|free|free+paid|paid|all] [--query term] [--limit n] [--json]")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	entries, err := fetchPublishCatalog(ctx, strings.TrimSpace(*sourceURL))
	if err != nil {
		fatal(err)
	}
	filtered := filterPublishCatalog(entries, strings.TrimSpace(*pricing), strings.TrimSpace(*query))
	if *limit > 0 && len(filtered) > *limit {
		filtered = filtered[:*limit]
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(filtered); err != nil {
			fatal(err)
		}
		return
	}
	if len(filtered) == 0 {
		infof("no publish platforms matched filter")
		return
	}
	headers := []string{
		styleHeading("PLATFORM"),
		styleHeading("PRICING"),
		styleHeading("ACCOUNT"),
		styleHeading("API"),
		styleHeading("URL"),
	}
	tableRows := make([][]string, 0, len(filtered))
	for _, entry := range filtered {
		api := "-"
		if entry.APIAvailable {
			api = entry.APIProvider
		}
		tableRows = append(tableRows, []string{
			orDash(entry.Name),
			orDash(entry.Pricing),
			orDash(entry.Account),
			orDash(api),
			orDash(entry.URL),
		})
	}
	printAlignedTable(headers, tableRows, 2)
}

func cmdPublishDevTo(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si publish devto <auth|article|raw>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "auth":
		cmdPublishDevToAuth(args[1:])
	case "article", "post":
		cmdPublishDevToArticle(args[1:])
	case "raw":
		cmdPublishDevToRaw(args[1:])
	default:
		printUnknown("publish devto", sub)
	}
}

func cmdPublishDevToAuth(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("publish devto auth", flag.ExitOnError)
	baseURL := fs.String("base-url", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_DEVTO_BASE_URL")), publishDevToBaseURL), "dev.to api base url")
	apiKey := fs.String("api-key", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_DEVTO_API_KEY")), strings.TrimSpace(os.Getenv("DEVTO_API_KEY"))), "dev.to api key")
	verify := fs.Bool("verify", true, "verify token with GET /api/users/me")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si publish devto auth [--json]")
		return
	}
	payload := map[string]any{
		"provider":      "devto",
		"base_url":      strings.TrimSpace(*baseURL),
		"api_key_set":   strings.TrimSpace(*apiKey) != "",
		"api_key_hint":  previewPublishSecret(*apiKey),
		"verify_status": "skipped",
	}
	if *verify && strings.TrimSpace(*apiKey) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		resp, err := publishDo(ctx, http.MethodGet, strings.TrimRight(strings.TrimSpace(*baseURL), "/")+"/api/users/me", map[string]string{
			"api-key": *apiKey,
			"Accept":  "application/json",
		}, "")
		if err != nil {
			payload["status"] = "error"
			payload["verify_error"] = err.Error()
		} else {
			payload["status"] = "ready"
			payload["verify_status"] = resp.StatusCode
			payload["verify"] = resp.Data
		}
	} else if strings.TrimSpace(*apiKey) != "" {
		payload["status"] = "ready"
	} else {
		payload["status"] = "error"
		payload["verify_error"] = "dev.to api key is required"
	}
	if *jsonOut {
		printJSON(payload)
		if status, _ := payload["status"].(string); status != "ready" {
			os.Exit(1)
		}
		return
	}
	if status, _ := payload["status"].(string); status == "ready" {
		fmt.Printf("%s %s\n", styleHeading("Publish dev.to auth:"), styleSuccess("ready"))
	} else {
		fmt.Printf("%s %s\n", styleHeading("Publish dev.to auth:"), styleError("error"))
	}
	fmt.Printf("%s %s\n", styleHeading("Base URL:"), strings.TrimSpace(*baseURL))
	fmt.Printf("%s %s\n", styleHeading("API key:"), previewPublishSecret(*apiKey))
	if errMsg, ok := payload["verify_error"].(string); ok && strings.TrimSpace(errMsg) != "" {
		fmt.Printf("%s %s\n", styleHeading("Verify:"), errMsg)
	}
}

func cmdPublishDevToArticle(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "published": true})
	fs := flag.NewFlagSet("publish devto article", flag.ExitOnError)
	baseURL := fs.String("base-url", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_DEVTO_BASE_URL")), publishDevToBaseURL), "dev.to api base url")
	apiKey := fs.String("api-key", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_DEVTO_API_KEY")), strings.TrimSpace(os.Getenv("DEVTO_API_KEY"))), "dev.to api key")
	title := fs.String("title", "", "article title")
	body := fs.String("body", "", "markdown body")
	tags := fs.String("tags", "", "comma-separated tags")
	canonicalURL := fs.String("canonical-url", "", "canonical url")
	published := fs.Bool("published", false, "publish immediately")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si publish devto article --title <title> --body <markdown> [--tags go,launch] [--published] [--json]")
		return
	}
	if strings.TrimSpace(*apiKey) == "" {
		fatal(fmt.Errorf("dev.to api key is required (use --api-key or DEVTO_API_KEY)"))
	}
	if strings.TrimSpace(*title) == "" || strings.TrimSpace(*body) == "" {
		fatal(fmt.Errorf("both --title and --body are required"))
	}
	payload := map[string]any{
		"article": map[string]any{
			"title":         strings.TrimSpace(*title),
			"published":     *published,
			"body_markdown": strings.TrimSpace(*body),
		},
	}
	if values := parsePublishCSV(strings.TrimSpace(*tags)); len(values) > 0 {
		payload["article"].(map[string]any)["tags"] = values
	}
	if value := strings.TrimSpace(*canonicalURL); value != "" {
		payload["article"].(map[string]any)["canonical_url"] = value
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := publishDo(ctx, http.MethodPost, strings.TrimRight(strings.TrimSpace(*baseURL), "/")+"/api/articles", map[string]string{
		"api-key":      strings.TrimSpace(*apiKey),
		"Accept":       "application/json",
		"Content-Type": "application/json",
	}, string(raw))
	if err != nil {
		fatal(err)
	}
	printPublishResponse(resp, *jsonOut)
}

func cmdPublishDevToRaw(args []string) {
	cmdPublishRawHTTP("devto", publishRawHTTPConfig{
		DefaultBaseURL: firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_DEVTO_BASE_URL")), publishDevToBaseURL),
		DefaultPath:    "/api/articles/me/all",
		TokenHeader:    "api-key",
		TokenEnv:       []string{"SI_PUBLISH_DEVTO_API_KEY", "DEVTO_API_KEY"},
	}, args)
}

func cmdPublishHashnode(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si publish hashnode <auth|post|raw>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "auth":
		cmdPublishHashnodeAuth(args[1:])
	case "post":
		cmdPublishHashnodePost(args[1:])
	case "raw":
		cmdPublishHashnodeRaw(args[1:])
	default:
		printUnknown("publish hashnode", sub)
	}
}

func cmdPublishHashnodeAuth(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("publish hashnode auth", flag.ExitOnError)
	baseURL := fs.String("base-url", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_HASHNODE_BASE_URL")), publishHashnodeBaseURL), "hashnode graphql url")
	token := fs.String("token", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_HASHNODE_TOKEN")), strings.TrimSpace(os.Getenv("HASHNODE_TOKEN"))), "hashnode token")
	verify := fs.Bool("verify", true, "verify token with viewer query")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si publish hashnode auth [--json]")
		return
	}
	payload := map[string]any{
		"provider":      "hashnode",
		"base_url":      strings.TrimSpace(*baseURL),
		"token_set":     strings.TrimSpace(*token) != "",
		"token_hint":    previewPublishSecret(*token),
		"verify_status": "skipped",
	}
	if *verify && strings.TrimSpace(*token) != "" {
		queryBody := `{"query":"query{me{id username}}"}`
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		resp, err := publishDo(ctx, http.MethodPost, strings.TrimSpace(*baseURL), map[string]string{
			"Authorization": "Bearer " + strings.TrimSpace(*token),
			"Accept":        "application/json",
			"Content-Type":  "application/json",
		}, queryBody)
		if err != nil {
			payload["status"] = "error"
			payload["verify_error"] = err.Error()
		} else {
			payload["status"] = "ready"
			payload["verify_status"] = resp.StatusCode
			payload["verify"] = resp.Data
		}
	} else if strings.TrimSpace(*token) != "" {
		payload["status"] = "ready"
	} else {
		payload["status"] = "error"
		payload["verify_error"] = "hashnode token is required"
	}
	if *jsonOut {
		printJSON(payload)
		if status, _ := payload["status"].(string); status != "ready" {
			os.Exit(1)
		}
		return
	}
	if status, _ := payload["status"].(string); status == "ready" {
		fmt.Printf("%s %s\n", styleHeading("Publish hashnode auth:"), styleSuccess("ready"))
	} else {
		fmt.Printf("%s %s\n", styleHeading("Publish hashnode auth:"), styleError("error"))
	}
	fmt.Printf("%s %s\n", styleHeading("GraphQL URL:"), strings.TrimSpace(*baseURL))
	fmt.Printf("%s %s\n", styleHeading("Token:"), previewPublishSecret(*token))
	if errMsg, ok := payload["verify_error"].(string); ok && strings.TrimSpace(errMsg) != "" {
		fmt.Printf("%s %s\n", styleHeading("Verify:"), errMsg)
	}
}

func cmdPublishHashnodePost(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("publish hashnode post", flag.ExitOnError)
	baseURL := fs.String("base-url", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_HASHNODE_BASE_URL")), publishHashnodeBaseURL), "hashnode graphql url")
	token := fs.String("token", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_HASHNODE_TOKEN")), strings.TrimSpace(os.Getenv("HASHNODE_TOKEN"))), "hashnode token")
	publicationID := fs.String("publication-id", "", "hashnode publication id")
	title := fs.String("title", "", "post title")
	content := fs.String("content-markdown", "", "markdown content")
	tags := fs.String("tags", "", "comma-separated tags")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si publish hashnode post --publication-id <id> --title <title> --content-markdown <md> [--tags go,saas] [--json]")
		return
	}
	if strings.TrimSpace(*token) == "" {
		fatal(fmt.Errorf("hashnode token is required (use --token or HASHNODE_TOKEN)"))
	}
	if strings.TrimSpace(*publicationID) == "" || strings.TrimSpace(*title) == "" || strings.TrimSpace(*content) == "" {
		fatal(fmt.Errorf("--publication-id, --title, and --content-markdown are required"))
	}
	tagInputs := make([]map[string]string, 0, 8)
	for _, tag := range parsePublishCSV(strings.TrimSpace(*tags)) {
		tagInputs = append(tagInputs, map[string]string{"name": tag})
	}
	query := "mutation PublishPost($input: PublishPostInput!){publishPost(input:$input){post{id title url slug}}}"
	variables := map[string]any{
		"input": map[string]any{
			"title":           strings.TrimSpace(*title),
			"contentMarkdown": strings.TrimSpace(*content),
			"publicationId":   strings.TrimSpace(*publicationID),
		},
	}
	if len(tagInputs) > 0 {
		variables["input"].(map[string]any)["tags"] = tagInputs
	}
	body, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := publishDo(ctx, http.MethodPost, strings.TrimSpace(*baseURL), map[string]string{
		"Authorization": "Bearer " + strings.TrimSpace(*token),
		"Accept":        "application/json",
		"Content-Type":  "application/json",
	}, string(body))
	if err != nil {
		fatal(err)
	}
	printPublishResponse(resp, *jsonOut)
}

func cmdPublishHashnodeRaw(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("publish hashnode raw", flag.ExitOnError)
	baseURL := fs.String("base-url", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_HASHNODE_BASE_URL")), publishHashnodeBaseURL), "hashnode graphql url")
	token := fs.String("token", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_HASHNODE_TOKEN")), strings.TrimSpace(os.Getenv("HASHNODE_TOKEN"))), "hashnode token")
	query := fs.String("query", "", "graphql query/mutation")
	variables := fs.String("variables", "", "json object string")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*query) == "" {
		printUsage("usage: si publish hashnode raw --query '<graphql>' [--variables '{\"k\":\"v\"}'] [--json]")
		return
	}
	req := map[string]any{"query": strings.TrimSpace(*query)}
	if strings.TrimSpace(*variables) != "" {
		var vars map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(*variables)), &vars); err != nil {
			fatal(fmt.Errorf("invalid --variables json: %w", err))
		}
		req["variables"] = vars
	}
	body, err := json.Marshal(req)
	if err != nil {
		fatal(err)
	}
	headers := map[string]string{
		"Accept":       "application/json",
		"Content-Type": "application/json",
	}
	if value := strings.TrimSpace(*token); value != "" {
		headers["Authorization"] = "Bearer " + value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := publishDo(ctx, http.MethodPost, strings.TrimSpace(*baseURL), headers, string(body))
	if err != nil {
		fatal(err)
	}
	printPublishResponse(resp, *jsonOut)
}

func cmdPublishReddit(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si publish reddit <auth|submit|raw>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "auth":
		cmdPublishRedditAuth(args[1:])
	case "submit", "post":
		cmdPublishRedditSubmit(args[1:])
	case "raw":
		cmdPublishRedditRaw(args[1:])
	default:
		printUnknown("publish reddit", sub)
	}
}

func cmdPublishRedditAuth(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("publish reddit auth", flag.ExitOnError)
	baseURL := fs.String("base-url", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_REDDIT_BASE_URL")), publishRedditBaseURL), "reddit oauth base url")
	token := fs.String("token", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_REDDIT_ACCESS_TOKEN")), strings.TrimSpace(os.Getenv("REDDIT_ACCESS_TOKEN"))), "reddit oauth access token")
	userAgent := fs.String("user-agent", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_REDDIT_USER_AGENT")), "si-publish/1.0"), "reddit user agent")
	verify := fs.Bool("verify", true, "verify token with GET /api/v1/me")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si publish reddit auth [--json]")
		return
	}
	payload := map[string]any{
		"provider":      "reddit",
		"base_url":      strings.TrimSpace(*baseURL),
		"token_set":     strings.TrimSpace(*token) != "",
		"token_hint":    previewPublishSecret(*token),
		"verify_status": "skipped",
	}
	if *verify && strings.TrimSpace(*token) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		resp, err := publishDo(ctx, http.MethodGet, strings.TrimRight(strings.TrimSpace(*baseURL), "/")+"/api/v1/me", map[string]string{
			"Authorization": "Bearer " + strings.TrimSpace(*token),
			"Accept":        "application/json",
			"User-Agent":    strings.TrimSpace(*userAgent),
		}, "")
		if err != nil {
			payload["status"] = "error"
			payload["verify_error"] = err.Error()
		} else {
			payload["status"] = "ready"
			payload["verify_status"] = resp.StatusCode
			payload["verify"] = resp.Data
		}
	} else if strings.TrimSpace(*token) != "" {
		payload["status"] = "ready"
	} else {
		payload["status"] = "error"
		payload["verify_error"] = "reddit access token is required"
	}
	if *jsonOut {
		printJSON(payload)
		if status, _ := payload["status"].(string); status != "ready" {
			os.Exit(1)
		}
		return
	}
	if status, _ := payload["status"].(string); status == "ready" {
		fmt.Printf("%s %s\n", styleHeading("Publish reddit auth:"), styleSuccess("ready"))
	} else {
		fmt.Printf("%s %s\n", styleHeading("Publish reddit auth:"), styleError("error"))
	}
	fmt.Printf("%s %s\n", styleHeading("Base URL:"), strings.TrimSpace(*baseURL))
	fmt.Printf("%s %s\n", styleHeading("Token:"), previewPublishSecret(*token))
	if errMsg, ok := payload["verify_error"].(string); ok && strings.TrimSpace(errMsg) != "" {
		fmt.Printf("%s %s\n", styleHeading("Verify:"), errMsg)
	}
}

func cmdPublishRedditSubmit(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "resubmit": true, "nsfw": true, "spoiler": true})
	fs := flag.NewFlagSet("publish reddit submit", flag.ExitOnError)
	baseURL := fs.String("base-url", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_REDDIT_BASE_URL")), publishRedditBaseURL), "reddit oauth base url")
	token := fs.String("token", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_REDDIT_ACCESS_TOKEN")), strings.TrimSpace(os.Getenv("REDDIT_ACCESS_TOKEN"))), "reddit oauth access token")
	userAgent := fs.String("user-agent", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_REDDIT_USER_AGENT")), "si-publish/1.0"), "reddit user agent")
	subreddit := fs.String("subreddit", "", "subreddit name")
	title := fs.String("title", "", "post title")
	kind := fs.String("kind", "self", "post kind: self|link")
	text := fs.String("text", "", "self post body")
	linkURL := fs.String("url", "", "link url")
	nsfw := fs.Bool("nsfw", false, "mark post nsfw")
	spoiler := fs.Bool("spoiler", false, "mark post spoiler")
	resubmit := fs.Bool("resubmit", true, "allow resubmit")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si publish reddit submit --subreddit <name> --title <title> [--kind self|link] [--text body] [--url link] [--json]")
		return
	}
	if strings.TrimSpace(*token) == "" {
		fatal(fmt.Errorf("reddit access token is required (use --token or REDDIT_ACCESS_TOKEN)"))
	}
	if strings.TrimSpace(*subreddit) == "" || strings.TrimSpace(*title) == "" {
		fatal(fmt.Errorf("--subreddit and --title are required"))
	}
	postKind := strings.ToLower(strings.TrimSpace(*kind))
	if postKind != "self" && postKind != "link" {
		fatal(fmt.Errorf("--kind must be self or link"))
	}
	if postKind == "self" && strings.TrimSpace(*text) == "" {
		fatal(fmt.Errorf("--text is required for self posts"))
	}
	if postKind == "link" && strings.TrimSpace(*linkURL) == "" {
		fatal(fmt.Errorf("--url is required for link posts"))
	}
	form := url.Values{}
	form.Set("sr", strings.TrimSpace(*subreddit))
	form.Set("title", strings.TrimSpace(*title))
	form.Set("kind", postKind)
	form.Set("api_type", "json")
	form.Set("resubmit", boolToString(*resubmit))
	if *nsfw {
		form.Set("nsfw", "true")
	}
	if *spoiler {
		form.Set("spoiler", "true")
	}
	if postKind == "self" {
		form.Set("text", strings.TrimSpace(*text))
	} else {
		form.Set("url", strings.TrimSpace(*linkURL))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := publishDo(ctx, http.MethodPost, strings.TrimRight(strings.TrimSpace(*baseURL), "/")+"/api/submit", map[string]string{
		"Authorization": "Bearer " + strings.TrimSpace(*token),
		"Accept":        "application/json",
		"Content-Type":  "application/x-www-form-urlencoded",
		"User-Agent":    strings.TrimSpace(*userAgent),
	}, form.Encode())
	if err != nil {
		fatal(err)
	}
	printPublishResponse(resp, *jsonOut)
}

func cmdPublishRedditRaw(args []string) {
	cmdPublishRawHTTP("reddit", publishRawHTTPConfig{
		DefaultBaseURL: firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_REDDIT_BASE_URL")), publishRedditBaseURL),
		DefaultPath:    "/api/v1/me",
		TokenHeader:    "Authorization",
		TokenPrefix:    "Bearer ",
		TokenEnv:       []string{"SI_PUBLISH_REDDIT_ACCESS_TOKEN", "REDDIT_ACCESS_TOKEN"},
		DefaultHeaders: map[string]string{"User-Agent": firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_REDDIT_USER_AGENT")), "si-publish/1.0")},
	}, args)
}

func cmdPublishHackerNews(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si publish hackernews <top|item|submit-url|raw>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "top":
		cmdPublishHackerNewsTop(args[1:])
	case "item":
		cmdPublishHackerNewsItem(args[1:])
	case "submit-url":
		cmdPublishHackerNewsSubmitURL(args[1:])
	case "raw":
		cmdPublishHackerNewsRaw(args[1:])
	default:
		printUnknown("publish hackernews", sub)
	}
}

func cmdPublishHackerNewsTop(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("publish hackernews top", flag.ExitOnError)
	baseURL := fs.String("base-url", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_HN_BASE_URL")), publishHNBaseURL), "hacker news firebase base url")
	limit := fs.Int("limit", 10, "number of stories")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si publish hackernews top [--limit 10] [--json]")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	idsResp, err := publishDo(ctx, http.MethodGet, strings.TrimRight(strings.TrimSpace(*baseURL), "/")+"/v0/topstories.json", map[string]string{"Accept": "application/json"}, "")
	if err != nil {
		fatal(err)
	}
	ids := make([]int64, 0, 64)
	switch typed := idsResp.Data.(type) {
	case []any:
		for _, value := range typed {
			switch num := value.(type) {
			case float64:
				ids = append(ids, int64(num))
			}
		}
	}
	if *limit > 0 && len(ids) > *limit {
		ids = ids[:*limit]
	}
	items := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		itemResp, itemErr := publishDo(ctx, http.MethodGet, strings.TrimRight(strings.TrimSpace(*baseURL), "/")+"/v0/item/"+strconv.FormatInt(id, 10)+".json", map[string]string{"Accept": "application/json"}, "")
		if itemErr != nil {
			continue
		}
		if item, ok := itemResp.Data.(map[string]any); ok {
			items = append(items, item)
		}
	}
	payload := map[string]any{
		"status_code": idsResp.StatusCode,
		"count":       len(items),
		"items":       items,
	}
	if *jsonOut {
		printJSON(payload)
		return
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		id := formatAny(item["id"])
		title := formatAny(item["title"])
		href := formatAny(item["url"])
		rows = append(rows, []string{id, title, href})
	}
	printAlignedRows(rows, 2, "")
}

func cmdPublishHackerNewsItem(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("publish hackernews item", flag.ExitOnError)
	baseURL := fs.String("base-url", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_HN_BASE_URL")), publishHNBaseURL), "hacker news firebase base url")
	id := fs.Int64("id", 0, "item id")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || *id <= 0 {
		printUsage("usage: si publish hackernews item --id <item-id> [--json]")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	resp, err := publishDo(ctx, http.MethodGet, strings.TrimRight(strings.TrimSpace(*baseURL), "/")+"/v0/item/"+strconv.FormatInt(*id, 10)+".json", map[string]string{"Accept": "application/json"}, "")
	if err != nil {
		fatal(err)
	}
	printPublishResponse(resp, *jsonOut)
}

func cmdPublishHackerNewsSubmitURL(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("publish hackernews submit-url", flag.ExitOnError)
	title := fs.String("title", "", "submission title")
	linkURL := fs.String("url", "", "submission url")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*title) == "" || strings.TrimSpace(*linkURL) == "" {
		printUsage("usage: si publish hackernews submit-url --title <title> --url <url> [--json]")
		return
	}
	submitURL := "https://news.ycombinator.com/submitlink?t=" + url.QueryEscape(strings.TrimSpace(*title)) + "&u=" + url.QueryEscape(strings.TrimSpace(*linkURL))
	payload := map[string]any{
		"provider":        "hackernews",
		"submit_url":      submitURL,
		"api_write_state": "hacker news official API is read-only; use submit_url in browser",
	}
	if *jsonOut {
		printJSON(payload)
		return
	}
	fmt.Printf("%s %s\n", styleHeading("Submit URL:"), submitURL)
	fmt.Printf("%s %s\n", styleHeading("Note:"), "Hacker News API is read-only for posts.")
}

func cmdPublishHackerNewsRaw(args []string) {
	cmdPublishRawHTTP("hackernews", publishRawHTTPConfig{
		DefaultBaseURL: firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_HN_BASE_URL")), publishHNBaseURL),
		DefaultPath:    "/v0/topstories.json",
	}, args)
}

func cmdPublishProductHunt(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, "usage: si publish producthunt <auth|posts|raw>")
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "auth":
		cmdPublishProductHuntAuth(args[1:])
	case "posts":
		cmdPublishProductHuntPosts(args[1:])
	case "raw":
		cmdPublishProductHuntRaw(args[1:])
	default:
		printUnknown("publish producthunt", sub)
	}
}

func cmdPublishProductHuntAuth(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("publish producthunt auth", flag.ExitOnError)
	baseURL := fs.String("base-url", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_PRODUCTHUNT_URL")), publishProductHuntURL), "product hunt graphql url")
	token := fs.String("token", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_PRODUCTHUNT_TOKEN")), strings.TrimSpace(os.Getenv("PRODUCTHUNT_TOKEN"))), "product hunt token")
	verify := fs.Bool("verify", true, "verify with graphql me query")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si publish producthunt auth [--json]")
		return
	}
	payload := map[string]any{
		"provider":      "producthunt",
		"graphql_url":   strings.TrimSpace(*baseURL),
		"token_set":     strings.TrimSpace(*token) != "",
		"token_hint":    previewPublishSecret(*token),
		"verify_status": "skipped",
	}
	if *verify && strings.TrimSpace(*token) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		resp, err := publishDo(ctx, http.MethodPost, strings.TrimSpace(*baseURL), map[string]string{
			"Authorization": "Bearer " + strings.TrimSpace(*token),
			"Accept":        "application/json",
			"Content-Type":  "application/json",
		}, `{"query":"query{viewer{user{id username}}}"}`)
		if err != nil {
			payload["status"] = "error"
			payload["verify_error"] = err.Error()
		} else {
			payload["status"] = "ready"
			payload["verify_status"] = resp.StatusCode
			payload["verify"] = resp.Data
		}
	} else if strings.TrimSpace(*token) != "" {
		payload["status"] = "ready"
	} else {
		payload["status"] = "error"
		payload["verify_error"] = "product hunt token is required"
	}
	if *jsonOut {
		printJSON(payload)
		if status, _ := payload["status"].(string); status != "ready" {
			os.Exit(1)
		}
		return
	}
	if status, _ := payload["status"].(string); status == "ready" {
		fmt.Printf("%s %s\n", styleHeading("Publish Product Hunt auth:"), styleSuccess("ready"))
	} else {
		fmt.Printf("%s %s\n", styleHeading("Publish Product Hunt auth:"), styleError("error"))
	}
	fmt.Printf("%s %s\n", styleHeading("GraphQL URL:"), strings.TrimSpace(*baseURL))
	fmt.Printf("%s %s\n", styleHeading("Token:"), previewPublishSecret(*token))
	if errMsg, ok := payload["verify_error"].(string); ok && strings.TrimSpace(errMsg) != "" {
		fmt.Printf("%s %s\n", styleHeading("Verify:"), errMsg)
	}
}

func cmdPublishProductHuntPosts(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("publish producthunt posts", flag.ExitOnError)
	baseURL := fs.String("base-url", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_PRODUCTHUNT_URL")), publishProductHuntURL), "product hunt graphql url")
	token := fs.String("token", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_PRODUCTHUNT_TOKEN")), strings.TrimSpace(os.Getenv("PRODUCTHUNT_TOKEN"))), "product hunt token")
	first := fs.Int("first", 10, "number of posts")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si publish producthunt posts [--first 10] [--json]")
		return
	}
	if strings.TrimSpace(*token) == "" {
		fatal(fmt.Errorf("product hunt token is required (use --token or PRODUCTHUNT_TOKEN)"))
	}
	if *first <= 0 {
		*first = 10
	}
	query := `query($first:Int!){posts(first:$first){edges{node{id name tagline url votesCount}}}}`
	body, err := json.Marshal(map[string]any{
		"query": query,
		"variables": map[string]any{
			"first": *first,
		},
	})
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	resp, err := publishDo(ctx, http.MethodPost, strings.TrimSpace(*baseURL), map[string]string{
		"Authorization": "Bearer " + strings.TrimSpace(*token),
		"Accept":        "application/json",
		"Content-Type":  "application/json",
	}, string(body))
	if err != nil {
		fatal(err)
	}
	printPublishResponse(resp, *jsonOut)
}

func cmdPublishProductHuntRaw(args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("publish producthunt raw", flag.ExitOnError)
	baseURL := fs.String("base-url", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_PRODUCTHUNT_URL")), publishProductHuntURL), "product hunt graphql url")
	token := fs.String("token", firstNonEmpty(strings.TrimSpace(os.Getenv("SI_PUBLISH_PRODUCTHUNT_TOKEN")), strings.TrimSpace(os.Getenv("PRODUCTHUNT_TOKEN"))), "product hunt token")
	query := fs.String("query", "", "graphql query/mutation")
	variables := fs.String("variables", "", "json object string")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*query) == "" {
		printUsage("usage: si publish producthunt raw --query '<graphql>' [--variables '{\"k\":\"v\"}'] [--json]")
		return
	}
	payload := map[string]any{"query": strings.TrimSpace(*query)}
	if strings.TrimSpace(*variables) != "" {
		var vars map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(*variables)), &vars); err != nil {
			fatal(fmt.Errorf("invalid --variables json: %w", err))
		}
		payload["variables"] = vars
	}
	body, err := json.Marshal(payload)
	if err != nil {
		fatal(err)
	}
	headers := map[string]string{
		"Accept":       "application/json",
		"Content-Type": "application/json",
	}
	if value := strings.TrimSpace(*token); value != "" {
		headers["Authorization"] = "Bearer " + value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	resp, err := publishDo(ctx, http.MethodPost, strings.TrimSpace(*baseURL), headers, string(body))
	if err != nil {
		fatal(err)
	}
	printPublishResponse(resp, *jsonOut)
}

type publishRawHTTPConfig struct {
	DefaultBaseURL string
	DefaultPath    string
	TokenHeader    string
	TokenPrefix    string
	TokenEnv       []string
	DefaultHeaders map[string]string
}

func cmdPublishRawHTTP(provider string, cfg publishRawHTTPConfig, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("publish "+provider+" raw", flag.ExitOnError)
	baseURL := fs.String("base-url", strings.TrimSpace(cfg.DefaultBaseURL), "api base url")
	method := fs.String("method", http.MethodGet, "http method")
	pathValue := fs.String("path", cfg.DefaultPath, "request path")
	body := fs.String("body", "", "raw request body")
	token := fs.String("token", firstNonEmpty(envValues(cfg.TokenEnv...)...), "api token")
	contentType := fs.String("content-type", "application/json", "request content-type")
	jsonOut := fs.Bool("json", false, "output json")
	params := multiFlag{}
	headers := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	fs.Var(&headers, "header", "request header key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage("usage: si publish " + provider + " raw --path </path> [--method GET] [--param key=value] [--header key=value] [--json]")
		return
	}
	endpoint, err := publishURLWithParams(strings.TrimSpace(*baseURL), strings.TrimSpace(*pathValue), parsePublishKV(params))
	if err != nil {
		fatal(err)
	}
	reqHeaders := map[string]string{
		"Accept": "application/json",
	}
	for key, value := range cfg.DefaultHeaders {
		reqHeaders[key] = value
	}
	for key, value := range parsePublishKV(headers) {
		reqHeaders[key] = value
	}
	if strings.TrimSpace(*body) != "" {
		reqHeaders["Content-Type"] = strings.TrimSpace(*contentType)
	}
	if strings.TrimSpace(cfg.TokenHeader) != "" && strings.TrimSpace(*token) != "" {
		reqHeaders[cfg.TokenHeader] = strings.TrimSpace(cfg.TokenPrefix) + strings.TrimSpace(*token)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := publishDo(ctx, strings.ToUpper(strings.TrimSpace(*method)), endpoint, reqHeaders, strings.TrimSpace(*body))
	if err != nil {
		fatal(err)
	}
	printPublishResponse(resp, *jsonOut)
}

func fetchPublishCatalog(ctx context.Context, sourceURL string) ([]publishCatalogEntry, error) {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		sourceURL = publishDistributionKitURL
	}
	resp, err := publishDo(ctx, http.MethodGet, sourceURL, map[string]string{
		"Accept": "text/html,application/xhtml+xml,application/json",
	}, "")
	if err != nil {
		return nil, err
	}
	rows := publishCatalogRowRe.FindAllString(resp.Body, -1)
	out := make([]publishCatalogEntry, 0, len(rows))
	for _, row := range rows {
		attrs := map[string]string{}
		for _, match := range publishCatalogAttrRe.FindAllStringSubmatch(row, -1) {
			if len(match) != 3 {
				continue
			}
			attrs[strings.TrimSpace(match[1])] = strings.TrimSpace(html.UnescapeString(match[2]))
		}
		name := strings.TrimSpace(attrs["platform-name"])
		if name == "" {
			continue
		}
		entry := publishCatalogEntry{
			Name:        name,
			Slug:        strings.TrimSpace(attrs["platform-slug"]),
			Score:       strings.TrimSpace(attrs["score"]),
			Pricing:     normalizePublishPricing(attrs["pricing"]),
			Account:     strings.TrimSpace(attrs["account"]),
			URL:         strings.TrimSpace(attrs["url"]),
			Description: extractPublishDescription(row),
		}
		entry.APIProvider, entry.APIAvailable = inferPublishAPIProvider(entry.Name, entry.Slug, entry.URL)
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func extractPublishDescription(row string) string {
	match := publishCatalogDescRe.FindStringSubmatch(row)
	if len(match) < 2 {
		return ""
	}
	value := strings.TrimSpace(match[1])
	value = publishTagStripRe.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	return normalizeWhitespace(value)
}

func normalizeWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func inferPublishAPIProvider(name string, slug string, rawURL string) (string, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	slug = strings.ToLower(strings.TrimSpace(slug))
	u, err := url.Parse(strings.TrimSpace(rawURL))
	host := ""
	if err == nil {
		host = strings.ToLower(strings.TrimSpace(u.Hostname()))
	}
	switch {
	case strings.Contains(host, "producthunt.com"), strings.Contains(name, "product hunt"), strings.Contains(slug, "product-hunt"):
		return "producthunt", true
	case strings.Contains(host, "dev.to"), strings.Contains(name, "dev.to"), slug == "dev-to":
		return "devto", true
	case strings.Contains(host, "hashnode.com"), strings.Contains(name, "hashnode"), strings.Contains(slug, "hashnode"):
		return "hashnode", true
	case strings.Contains(host, "reddit.com"), strings.Contains(name, "reddit"), strings.Contains(slug, "reddit"):
		return "reddit", true
	case strings.Contains(host, "news.ycombinator.com"), strings.Contains(name, "hacker news"), strings.Contains(slug, "hacker-news"):
		return "hackernews", true
	default:
		return "", false
	}
}

func filterPublishCatalog(entries []publishCatalogEntry, pricing string, query string) []publishCatalogEntry {
	pricing = strings.ToLower(strings.TrimSpace(pricing))
	query = strings.ToLower(strings.TrimSpace(query))
	out := make([]publishCatalogEntry, 0, len(entries))
	for _, entry := range entries {
		p := strings.ToLower(strings.TrimSpace(entry.Pricing))
		if !publishPricingMatches(pricing, p) {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(strings.Join([]string{
				entry.Name,
				entry.Slug,
				entry.Description,
				entry.URL,
				entry.APIProvider,
			}, " "))
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		out = append(out, entry)
	}
	return out
}

func publishPricingMatches(filter string, pricing string) bool {
	pricing = strings.ReplaceAll(strings.TrimSpace(strings.ToLower(pricing)), " ", "")
	switch strings.ReplaceAll(filter, " ", "") {
	case "", "free-at-least", "freeatleast":
		return pricing == "free" || pricing == "free+paid"
	case "free":
		return pricing == "free"
	case "free+paid", "freepluspaid":
		return pricing == "free+paid"
	case "paid":
		return pricing == "paid"
	case "all":
		return true
	default:
		return pricing == "free" || pricing == "free+paid"
	}
}

func normalizePublishPricing(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "free":
		return "Free"
	case "free+paid", "free + paid":
		return "Free + Paid"
	case "paid":
		return "Paid"
	default:
		return strings.TrimSpace(html.UnescapeString(raw))
	}
}

func parsePublishCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func parsePublishKV(values multiFlag) map[string]string {
	out := map[string]string{}
	for _, item := range values {
		parts := strings.SplitN(strings.TrimSpace(item), "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(parts[1])
	}
	return out
}

func publishURLWithParams(baseURL string, pathValue string, params map[string]string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", fmt.Errorf("base url is required")
	}
	pathValue = strings.TrimSpace(pathValue)
	if pathValue == "" {
		pathValue = "/"
	}
	var u *url.URL
	var err error
	if strings.HasPrefix(pathValue, "http://") || strings.HasPrefix(pathValue, "https://") {
		u, err = url.Parse(pathValue)
		if err != nil {
			return "", err
		}
	} else {
		base, parseErr := url.Parse(baseURL)
		if parseErr != nil {
			return "", parseErr
		}
		if !base.IsAbs() {
			return "", fmt.Errorf("base url must be absolute: %q", baseURL)
		}
		if !strings.HasPrefix(pathValue, "/") {
			pathValue = "/" + pathValue
		}
		ref := &url.URL{Path: pathValue}
		u = base.ResolveReference(ref)
	}
	q := u.Query()
	for key, value := range params {
		if strings.TrimSpace(key) == "" {
			continue
		}
		q.Set(strings.TrimSpace(key), strings.TrimSpace(value))
	}
	u.RawQuery = q.Encode()
	return strings.TrimSpace(u.String()), nil
}

func publishDo(ctx context.Context, method string, endpoint string, headers map[string]string, body string) (publishHTTPResponse, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return publishHTTPResponse{}, fmt.Errorf("endpoint is required")
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewBufferString(body))
	if err != nil {
		return publishHTTPResponse{}, err
	}
	for key, value := range headers {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "si-publish/1.0")
	}
	client := httpx.SharedClient(60 * time.Second)
	httpResp, err := client.Do(req)
	if err != nil {
		return publishHTTPResponse{}, err
	}
	defer httpResp.Body.Close()
	rawBody, _ := io.ReadAll(io.LimitReader(httpResp.Body, 10<<20))
	response := publishHTTPResponse{
		StatusCode: httpResp.StatusCode,
		Status:     strings.TrimSpace(httpResp.Status),
		Headers:    map[string]string{},
		Body:       strings.TrimSpace(string(rawBody)),
	}
	for key, values := range httpResp.Header {
		if len(values) == 0 {
			continue
		}
		response.Headers[key] = strings.Join(values, ",")
	}
	contentType := strings.ToLower(strings.TrimSpace(httpResp.Header.Get("Content-Type")))
	if strings.Contains(contentType, "application/json") || (strings.HasPrefix(strings.TrimSpace(response.Body), "{") || strings.HasPrefix(strings.TrimSpace(response.Body), "[")) {
		var parsed any
		if err := json.Unmarshal([]byte(response.Body), &parsed); err == nil {
			response.Data = parsed
		}
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return response, fmt.Errorf("publish api request failed: status=%d body=%s", httpResp.StatusCode, truncateForErr(response.Body, 400))
	}
	return response, nil
}

func truncateForErr(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max] + "..."
}

func previewPublishSecret(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "-"
	}
	if len(raw) <= 8 {
		return strings.Repeat("*", len(raw))
	}
	return raw[:4] + strings.Repeat("*", len(raw)-8) + raw[len(raw)-4:]
}

func printPublishResponse(resp publishHTTPResponse, jsonOut bool) {
	if jsonOut {
		printJSON(resp)
		return
	}
	fmt.Printf("%s %d (%s)\n", styleHeading("Status:"), resp.StatusCode, resp.Status)
	if resp.Data != nil {
		raw, err := json.MarshalIndent(resp.Data, "", "  ")
		if err == nil {
			fmt.Println(string(raw))
			return
		}
	}
	if strings.TrimSpace(resp.Body) != "" {
		fmt.Println(resp.Body)
	}
}

func boolToString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func envValues(keys ...string) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out = append(out, strings.TrimSpace(os.Getenv(key)))
	}
	return out
}

func printJSON(value any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		fatal(err)
	}
}

func formatAny(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		raw, _ := json.Marshal(typed)
		return strings.TrimSpace(string(raw))
	}
}
