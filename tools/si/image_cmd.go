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
	"sort"
	"strconv"
	"strings"
	"time"

	"si/tools/si/internal/httpx"
)

type imageProvider string

const (
	imageProviderUnsplash imageProvider = "unsplash"
	imageProviderPexels   imageProvider = "pexels"
	imageProviderPixabay  imageProvider = "pixabay"
)

const imageUsageText = "usage: si image <unsplash|pexels|pixabay>"

type imageRuntime struct {
	Provider imageProvider
	BaseURL  string
	APIKey   string
	Source   string
}

type imageRequest struct {
	Method  string
	Path    string
	Params  map[string]string
	Headers map[string]string
	Body    string
}

type imageResponse struct {
	StatusCode int               `json:"status_code"`
	Status     string            `json:"status"`
	RequestID  string            `json:"request_id,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	Data       map[string]any    `json:"data,omitempty"`
	List       []map[string]any  `json:"list,omitempty"`
}

type imageAPIErrorDetails struct {
	Provider   imageProvider `json:"provider,omitempty"`
	StatusCode int           `json:"status_code,omitempty"`
	Message    string        `json:"message,omitempty"`
	RequestID  string        `json:"request_id,omitempty"`
	RawBody    string        `json:"raw_body,omitempty"`
}

func (e *imageAPIErrorDetails) Error() string {
	if e == nil {
		return "image api error"
	}
	parts := make([]string, 0, 5)
	if e.Provider != "" {
		parts = append(parts, "provider="+string(e.Provider))
	}
	if e.StatusCode > 0 {
		parts = append(parts, fmt.Sprintf("status_code=%d", e.StatusCode))
	}
	if strings.TrimSpace(e.Message) != "" {
		parts = append(parts, "message="+e.Message)
	}
	if strings.TrimSpace(e.RequestID) != "" {
		parts = append(parts, "request_id="+e.RequestID)
	}
	if len(parts) == 0 {
		return "image api error"
	}
	return "image api error: " + strings.Join(parts, ", ")
}

func cmdImage(args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, imageUsageText)
	if !routedOK {
		return
	}
	args = routedArgs
	provider := normalizeImageProvider(args[0])
	rest := args[1:]
	switch provider {
	case imageProviderUnsplash, imageProviderPexels, imageProviderPixabay:
		cmdImageProvider(provider, rest)
	case "":
		printUnknown("image", strings.TrimSpace(args[0]))
		printUsage(imageUsageText)
	default:
		printUnknown("image", strings.TrimSpace(args[0]))
		printUsage(imageUsageText)
	}
}

func cmdImageProvider(provider imageProvider, args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, fmt.Sprintf("usage: si image %s <auth|search|raw>", provider))
	if !routedOK {
		return
	}
	args = routedArgs
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]
	switch sub {
	case "help", "-h", "--help":
		printUsage(fmt.Sprintf("usage: si image %s <auth|search|raw>", provider))
	case "auth":
		cmdImageAuth(provider, rest)
	case "search":
		cmdImageSearch(provider, rest)
	case "raw":
		cmdImageRaw(provider, rest)
	default:
		printUnknown("image "+string(provider), sub)
		printUsage(fmt.Sprintf("usage: si image %s <auth|search|raw>", provider))
	}
}

func cmdImageAuth(provider imageProvider, args []string) {
	routedArgs, routedOK := resolveUsageSubcommandArgs(args, fmt.Sprintf("usage: si image %s auth status [--json]", provider))
	if !routedOK {
		return
	}
	args = routedArgs
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "status":
		cmdImageAuthStatus(provider, args[1:])
	default:
		printUnknown("image "+string(provider)+" auth", args[0])
		printUsage(fmt.Sprintf("usage: si image %s auth status [--json]", provider))
	}
}

func cmdImageAuthStatus(provider imageProvider, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true})
	fs := flag.NewFlagSet("image auth status", flag.ExitOnError)
	apiKey := fs.String("api-key", "", "override api key")
	baseURL := fs.String("base-url", "", "api base url")
	jsonOut := fs.Bool("json", false, "output json")
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		printUsage(fmt.Sprintf("usage: si image %s auth status [--api-key <key>] [--base-url <url>] [--json]", provider))
		return
	}
	runtime, err := resolveImageRuntime(provider, strings.TrimSpace(*apiKey), strings.TrimSpace(*baseURL))
	ok := err == nil
	payload := map[string]any{
		"ok":       ok,
		"provider": provider,
		"base_url": imageBaseURL(provider, strings.TrimSpace(*baseURL)),
		"source":   "",
	}
	if ok {
		payload["source"] = runtime.Source
	} else {
		payload["error"] = err.Error()
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(payload); encErr != nil {
			fatal(encErr)
		}
		if !ok {
			os.Exit(1)
		}
		return
	}
	if ok {
		successf("%s auth configured (%s)", provider, runtime.Source)
		return
	}
	fatal(err)
}

func cmdImageSearch(provider imageProvider, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("image search", flag.ExitOnError)
	query := fs.String("query", "", "search query")
	page := fs.Int("page", 1, "page number")
	perPage := fs.Int("per-page", 10, "items per page")
	orientation := fs.String("orientation", "", "orientation (unsplash only: landscape|portrait|squarish)")
	apiKey := fs.String("api-key", "", "override api key")
	baseURL := fs.String("base-url", "", "api base url")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*query) == "" {
		printUsage(fmt.Sprintf("usage: si image %s search --query <text> [--page <n>] [--per-page <n>] [--param key=value] [--json]", provider))
		return
	}
	runtime, err := resolveImageRuntime(provider, strings.TrimSpace(*apiKey), strings.TrimSpace(*baseURL))
	if err != nil {
		fatal(err)
	}
	searchPath, searchParams := providerSearchDefaults(provider)
	searchParams["query"] = strings.TrimSpace(*query)
	searchParams["page"] = strconv.Itoa(imageMaxInt(1, *page))
	searchParams["per_page"] = strconv.Itoa(imageClampInt(imageMaxInt(1, *perPage), 1, 80))
	if provider == imageProviderPixabay {
		searchParams["q"] = searchParams["query"]
		delete(searchParams, "query")
	}
	if provider == imageProviderUnsplash && strings.TrimSpace(*orientation) != "" {
		searchParams["orientation"] = strings.TrimSpace(*orientation)
	}
	for key, value := range parseImageMap(params) {
		searchParams[key] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := imageDo(ctx, runtime, imageRequest{
		Method: http.MethodGet,
		Path:   searchPath,
		Params: searchParams,
	})
	if err != nil {
		printImageError(err)
		return
	}
	printImageResponse(resp, *jsonOut, *raw)
}

func cmdImageRaw(provider imageProvider, args []string) {
	args = stripeFlagsFirst(args, map[string]bool{"json": true, "raw": true})
	fs := flag.NewFlagSet("image raw", flag.ExitOnError)
	method := fs.String("method", http.MethodGet, "http method")
	path := fs.String("path", "", "api path")
	body := fs.String("body", "", "raw request body")
	apiKey := fs.String("api-key", "", "override api key")
	baseURL := fs.String("base-url", "", "api base url")
	jsonOut := fs.Bool("json", false, "output json")
	raw := fs.Bool("raw", false, "print raw response body")
	params := multiFlag{}
	headers := multiFlag{}
	fs.Var(&params, "param", "query parameter key=value (repeatable)")
	fs.Var(&headers, "header", "header key=value (repeatable)")
	_ = fs.Parse(args)
	if fs.NArg() > 0 || strings.TrimSpace(*path) == "" {
		printUsage(fmt.Sprintf("usage: si image %s raw --method <GET|POST> --path <api-path> [--param key=value] [--header key=value] [--body raw] [--json]", provider))
		return
	}
	runtime, err := resolveImageRuntime(provider, strings.TrimSpace(*apiKey), strings.TrimSpace(*baseURL))
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := imageDo(ctx, runtime, imageRequest{
		Method:  strings.ToUpper(strings.TrimSpace(*method)),
		Path:    strings.TrimSpace(*path),
		Params:  parseImageMap(params),
		Headers: parseImageMap(headers),
		Body:    strings.TrimSpace(*body),
	})
	if err != nil {
		printImageError(err)
		return
	}
	printImageResponse(resp, *jsonOut, *raw)
}

func resolveImageRuntime(provider imageProvider, apiKey string, baseURLOverride string) (imageRuntime, error) {
	key, source, err := resolveImageAPIKey(provider, apiKey)
	if err != nil {
		return imageRuntime{}, err
	}
	return imageRuntime{
		Provider: provider,
		BaseURL:  imageBaseURL(provider, baseURLOverride),
		APIKey:   key,
		Source:   source,
	}, nil
}

func resolveImageAPIKey(provider imageProvider, override string) (string, string, error) {
	override = strings.TrimSpace(override)
	if override != "" {
		return override, "flag:--api-key", nil
	}
	candidates := []string{}
	switch provider {
	case imageProviderUnsplash:
		candidates = []string{"SI_IMAGE_UNSPLASH_ACCESS_KEY", "UNSPLASH_ACCESS_KEY"}
	case imageProviderPexels:
		candidates = []string{"SI_IMAGE_PEXELS_API_KEY", "PEXELS_API_KEY"}
	case imageProviderPixabay:
		candidates = []string{"SI_IMAGE_PIXABAY_API_KEY", "PIXABAY_API_KEY"}
	}
	for _, key := range candidates {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return value, "env:" + key, nil
		}
	}
	return "", "", fmt.Errorf("%s api key missing (set --api-key or %s)", provider, strings.Join(candidates, " / "))
}

func imageBaseURL(provider imageProvider, override string) string {
	override = strings.TrimSpace(override)
	if override != "" {
		return strings.TrimRight(override, "/")
	}
	switch provider {
	case imageProviderUnsplash:
		return firstNonEmpty(strings.TrimSpace(os.Getenv("SI_IMAGE_UNSPLASH_API_BASE_URL")), "https://api.unsplash.com")
	case imageProviderPexels:
		return firstNonEmpty(strings.TrimSpace(os.Getenv("SI_IMAGE_PEXELS_API_BASE_URL")), "https://api.pexels.com")
	case imageProviderPixabay:
		return firstNonEmpty(strings.TrimSpace(os.Getenv("SI_IMAGE_PIXABAY_API_BASE_URL")), "https://pixabay.com")
	default:
		return ""
	}
}

func providerSearchDefaults(provider imageProvider) (string, map[string]string) {
	switch provider {
	case imageProviderUnsplash:
		return "/search/photos", map[string]string{}
	case imageProviderPexels:
		return "/v1/search", map[string]string{}
	case imageProviderPixabay:
		return "/api/", map[string]string{"safesearch": "true"}
	default:
		return "/", map[string]string{}
	}
}

func imageDo(ctx context.Context, runtime imageRuntime, req imageRequest) (imageResponse, error) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	endpoint, err := resolveImageURL(runtime.BaseURL, req.Path, req.Params)
	if err != nil {
		return imageResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(strings.TrimSpace(req.Body)))
	if err != nil {
		return imageResponse{}, err
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", "si-image/1.0")
	switch runtime.Provider {
	case imageProviderUnsplash:
		httpReq.Header.Set("Authorization", "Client-ID "+strings.TrimSpace(runtime.APIKey))
	case imageProviderPexels:
		httpReq.Header.Set("Authorization", strings.TrimSpace(runtime.APIKey))
	case imageProviderPixabay:
		values := httpReq.URL.Query()
		if strings.TrimSpace(values.Get("key")) == "" {
			values.Set("key", strings.TrimSpace(runtime.APIKey))
		}
		httpReq.URL.RawQuery = values.Encode()
	}
	for key, value := range req.Headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		httpReq.Header.Set(key, strings.TrimSpace(value))
	}
	client := httpx.SharedClient(45 * time.Second)
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return imageResponse{}, err
	}
	defer httpResp.Body.Close()
	bodyBytes, _ := io.ReadAll(httpResp.Body)
	body := strings.TrimSpace(string(bodyBytes))
	resp := normalizeImageResponse(httpResp, body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return imageResponse{}, normalizeImageHTTPError(runtime.Provider, resp.StatusCode, httpResp.Header, body)
	}
	return resp, nil
}

func resolveImageURL(baseURL string, path string, params map[string]string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", fmt.Errorf("base url is required")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		u, err := url.Parse(path)
		if err != nil {
			return "", err
		}
		imageAddQuery(u, params)
		return u.String(), nil
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	rel, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	u := base.ResolveReference(rel)
	imageAddQuery(u, params)
	return u.String(), nil
}

func imageAddQuery(u *url.URL, params map[string]string) {
	if u == nil || len(params) == 0 {
		return
	}
	q := u.Query()
	for key, value := range params {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		q.Set(key, strings.TrimSpace(value))
	}
	u.RawQuery = q.Encode()
}

func normalizeImageResponse(httpResp *http.Response, body string) imageResponse {
	out := imageResponse{}
	if httpResp == nil {
		return out
	}
	out.StatusCode = httpResp.StatusCode
	out.Status = httpResp.Status
	out.Body = strings.TrimSpace(body)
	out.RequestID = resolveImageRequestID(httpResp.Header)
	out.Headers = imageFlattenHeaders(httpResp.Header)
	if out.Body == "" {
		return out
	}
	var parsed any
	if err := json.Unmarshal([]byte(out.Body), &parsed); err != nil {
		return out
	}
	switch typed := parsed.(type) {
	case map[string]any:
		out.Data = typed
		for _, key := range []string{"results", "photos", "hits", "items"} {
			if raw, ok := typed[key].([]any); ok {
				out.List = imageAnySliceToMaps(raw)
				return out
			}
		}
	case []any:
		out.List = imageAnySliceToMaps(typed)
	}
	return out
}

func normalizeImageHTTPError(provider imageProvider, statusCode int, headers http.Header, rawBody string) error {
	details := &imageAPIErrorDetails{
		Provider:   provider,
		StatusCode: statusCode,
		RequestID:  resolveImageRequestID(headers),
		RawBody:    strings.TrimSpace(rawBody),
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(rawBody)), &parsed); err == nil {
		for _, key := range []string{"error", "message", "error_message"} {
			if value, ok := parsed[key].(string); ok && strings.TrimSpace(value) != "" {
				details.Message = strings.TrimSpace(value)
				break
			}
		}
		if details.Message == "" {
			if values, ok := parsed["errors"].([]any); ok && len(values) > 0 {
				messages := make([]string, 0, len(values))
				for _, item := range values {
					text := strings.TrimSpace(stringifySocialAny(item))
					if text != "" {
						messages = append(messages, text)
					}
				}
				details.Message = strings.Join(messages, "; ")
			}
		}
	}
	if strings.TrimSpace(details.Message) == "" {
		details.Message = firstNonEmpty(strings.TrimSpace(rawBody), "request failed")
	}
	return details
}

func resolveImageRequestID(headers http.Header) string {
	for _, key := range []string{"X-Request-Id", "X-Request-ID", "CF-Ray"} {
		if value := strings.TrimSpace(headers.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func imageFlattenHeaders(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = strings.Join(headers.Values(key), ",")
	}
	return out
}

func imageAnySliceToMaps(values []any) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		obj, ok := value.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, obj)
	}
	return out
}

func printImageResponse(resp imageResponse, jsonOut bool, raw bool) {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(resp); err != nil {
			fatal(err)
		}
		return
	}
	fmt.Printf("%s %s (%d)\n", styleHeading("Image API:"), resp.Status, resp.StatusCode)
	if strings.TrimSpace(resp.RequestID) != "" {
		fmt.Printf("%s %s\n", styleHeading("Request ID:"), resp.RequestID)
	}
	if raw {
		body := strings.TrimSpace(resp.Body)
		if body == "" {
			body = "{}"
		}
		fmt.Println(body)
		return
	}
	if len(resp.List) > 0 {
		fmt.Printf("%s %d\n", styleHeading("Items:"), len(resp.List))
		for _, item := range resp.List {
			fmt.Printf("  %s\n", socialSummarizeItem(item))
		}
		return
	}
	if len(resp.Data) > 0 {
		printSocialKeyValueMap(resp.Data)
		return
	}
	if strings.TrimSpace(resp.Body) != "" {
		fmt.Println(resp.Body)
	}
}

func printImageError(err error) {
	if err == nil {
		return
	}
	var details *imageAPIErrorDetails
	if !errors.As(err, &details) || details == nil {
		fatal(err)
		return
	}
	fmt.Fprintln(os.Stderr, styleError("image api error"))
	fmt.Fprintln(os.Stderr, styleError(fmt.Sprintf("  provider: %s", details.Provider)))
	fmt.Fprintln(os.Stderr, styleError(fmt.Sprintf("  status_code: %d", details.StatusCode)))
	if strings.TrimSpace(details.Message) != "" {
		fmt.Fprintln(os.Stderr, styleError("  message: "+details.Message))
	}
	if strings.TrimSpace(details.RequestID) != "" {
		fmt.Fprintln(os.Stderr, styleError("  request_id: "+details.RequestID))
	}
	if strings.TrimSpace(details.RawBody) != "" {
		fmt.Fprintln(os.Stderr, styleDim("raw:"))
		fmt.Fprintln(os.Stderr, details.RawBody)
	}
	os.Exit(1)
}

func normalizeImageProvider(raw string) imageProvider {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "unsplash":
		return imageProviderUnsplash
	case "pexels":
		return imageProviderPexels
	case "pixabay":
		return imageProviderPixabay
	default:
		return ""
	}
}

func parseImageMap(values []string) map[string]string {
	out := map[string]string{}
	for _, entry := range values {
		parts := strings.SplitN(strings.TrimSpace(entry), "=", 2)
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

func imageClampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func imageMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
