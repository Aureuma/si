package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"si/tools/si/internal/httpx"
	"si/tools/si/internal/integrationruntime"
	"si/tools/si/internal/netpolicy"
	"si/tools/si/internal/providers"
)

type socialPlatform string

const (
	socialPlatformUnknown   socialPlatform = ""
	socialPlatformHelp      socialPlatform = "help"
	socialPlatformFacebook  socialPlatform = "facebook"
	socialPlatformInstagram socialPlatform = "instagram"
	socialPlatformX         socialPlatform = "x"
	socialPlatformLinkedIn  socialPlatform = "linkedin"
	socialPlatformReddit    socialPlatform = "reddit"
)

type socialRuntimeContext struct {
	Platform                socialPlatform
	AccountAlias            string
	Environment             string
	BaseURL                 string
	APIVersion              string
	AuthStyle               string
	Token                   string
	Source                  string
	LogPath                 string
	FacebookPageID          string
	InstagramBusinessID     string
	XUserID                 string
	XUsername               string
	LinkedInPersonURN       string
	LinkedInOrganizationURN string
	RedditUsername          string
}

type socialRuntimeContextInput struct {
	Platform    socialPlatform
	AccountFlag string
	EnvFlag     string
	BaseURLFlag string
	APIVerFlag  string
	AuthFlag    string
	TokenFlag   string
}

type socialRequest struct {
	Method      string
	Path        string
	Params      map[string]string
	Headers     map[string]string
	RawBody     string
	JSONBody    any
	ContentType string
}

type socialResponse struct {
	StatusCode int               `json:"status_code"`
	Status     string            `json:"status"`
	RequestID  string            `json:"request_id,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	Data       map[string]any    `json:"data,omitempty"`
	List       []map[string]any  `json:"list,omitempty"`
}

type socialAPIErrorDetails struct {
	Platform   socialPlatform `json:"platform,omitempty"`
	StatusCode int            `json:"status_code,omitempty"`
	Code       int            `json:"code,omitempty"`
	Status     string         `json:"status,omitempty"`
	Type       string         `json:"type,omitempty"`
	Message    string         `json:"message,omitempty"`
	RequestID  string         `json:"request_id,omitempty"`
	RawBody    string         `json:"raw_body,omitempty"`
}

func (e *socialAPIErrorDetails) Error() string {
	if e == nil {
		return "social api error"
	}
	parts := make([]string, 0, 6)
	if e.Platform != "" {
		parts = append(parts, "platform="+string(e.Platform))
	}
	if e.StatusCode > 0 {
		parts = append(parts, fmt.Sprintf("status_code=%d", e.StatusCode))
	}
	if e.Code > 0 {
		parts = append(parts, fmt.Sprintf("code=%d", e.Code))
	}
	if strings.TrimSpace(e.Status) != "" {
		parts = append(parts, "status="+e.Status)
	}
	if strings.TrimSpace(e.Type) != "" {
		parts = append(parts, "type="+e.Type)
	}
	if strings.TrimSpace(e.Message) != "" {
		parts = append(parts, "message="+e.Message)
	}
	if len(parts) == 0 {
		return "social api error"
	}
	return "social api error: " + strings.Join(parts, ", ")
}

func normalizeSocialPlatform(raw string) socialPlatform {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "help", "-h", "--help":
		return socialPlatformHelp
	case "facebook", "fb":
		return socialPlatformFacebook
	case "instagram", "ig":
		return socialPlatformInstagram
	case "x", "twitter", "x-twitter", "x_twitter":
		return socialPlatformX
	case "linkedin", "li":
		return socialPlatformLinkedIn
	case "reddit", "rd":
		return socialPlatformReddit
	default:
		return socialPlatformUnknown
	}
}

func normalizeSocialEnvironment(raw string) string {
	env := strings.ToLower(strings.TrimSpace(raw))
	switch env {
	case "prod", "staging", "dev":
		return env
	default:
		return ""
	}
}

func parseSocialEnvironment(raw string) (string, error) {
	env := normalizeSocialEnvironment(raw)
	if env == "" {
		if strings.TrimSpace(raw) == "" {
			return "", fmt.Errorf("environment required (prod|staging|dev)")
		}
		if strings.EqualFold(strings.TrimSpace(raw), "test") {
			return "", fmt.Errorf("environment `test` is not supported; use `staging` or `dev`")
		}
		return "", fmt.Errorf("invalid environment %q (expected prod|staging|dev)", raw)
	}
	return env, nil
}

func normalizeSocialAuthStyle(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "none", "public", "unauth", "anonymous":
		return "none"
	case "bearer", "oauth", "oauth2":
		return "bearer"
	case "query", "query-token", "token-query":
		return "query"
	default:
		return ""
	}
}

func socialPlatformLabel(platform socialPlatform) string {
	switch platform {
	case socialPlatformFacebook:
		return "facebook"
	case socialPlatformInstagram:
		return "instagram"
	case socialPlatformX:
		return "x"
	case socialPlatformLinkedIn:
		return "linkedin"
	case socialPlatformReddit:
		return "reddit"
	default:
		return "social"
	}
}

func socialProviderID(platform socialPlatform) providers.ID {
	switch platform {
	case socialPlatformFacebook:
		return providers.SocialFacebook
	case socialPlatformInstagram:
		return providers.SocialInstagram
	case socialPlatformX:
		return providers.SocialX
	case socialPlatformLinkedIn:
		return providers.SocialLinkedIn
	case socialPlatformReddit:
		return providers.SocialReddit
	default:
		return ""
	}
}

func resolveSocialRuntimeContext(input socialRuntimeContextInput) (socialRuntimeContext, error) {
	settings := loadSettingsOrDefault()
	platform := input.Platform
	if platform == socialPlatformUnknown {
		return socialRuntimeContext{}, fmt.Errorf("social platform is required")
	}
	providerID := socialProviderID(platform)
	spec := providers.Resolve(providerID)
	alias, account := resolveSocialAccountSelection(settings, input.AccountFlag)
	env := strings.TrimSpace(input.EnvFlag)
	if env == "" {
		env = strings.TrimSpace(settings.Social.DefaultEnv)
	}
	if env == "" {
		env = strings.TrimSpace(os.Getenv("SOCIAL_DEFAULT_ENV"))
	}
	if env == "" {
		env = "prod"
	}
	parsedEnv, err := parseSocialEnvironment(env)
	if err != nil {
		return socialRuntimeContext{}, err
	}

	cfg := socialPlatformSettings(settings, platform)

	baseURL := strings.TrimSpace(input.BaseURLFlag)
	if baseURL == "" {
		baseURL = strings.TrimSpace(cfg.APIBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("SOCIAL_" + strings.ToUpper(socialPlatformLabel(platform)) + "_API_BASE_URL"))
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(spec.BaseURL)
	}
	if strings.TrimSpace(baseURL) == "" {
		return socialRuntimeContext{}, fmt.Errorf("base URL is required")
	}

	apiVersion := strings.TrimSpace(input.APIVerFlag)
	if apiVersion == "" {
		apiVersion = strings.TrimSpace(cfg.APIVersion)
	}
	if apiVersion == "" {
		apiVersion = strings.TrimSpace(os.Getenv("SOCIAL_" + strings.ToUpper(socialPlatformLabel(platform)) + "_API_VERSION"))
	}
	if apiVersion == "" {
		apiVersion = strings.TrimSpace(spec.APIVersion)
	}

	authStyle := normalizeSocialAuthStyle(input.AuthFlag)
	if authStyle == "" {
		authStyle = normalizeSocialAuthStyle(cfg.AuthStyle)
	}
	if authStyle == "" {
		authStyle = normalizeSocialAuthStyle(os.Getenv("SOCIAL_" + strings.ToUpper(socialPlatformLabel(platform)) + "_AUTH_STYLE"))
	}
	if authStyle == "" {
		authStyle = normalizeSocialAuthStyle(spec.AuthStyle)
	}
	if authStyle == "" {
		authStyle = socialDefaultAuthStyle(platform)
	}

	token, tokenSource := resolveSocialAccessToken(platform, alias, account, strings.TrimSpace(input.TokenFlag))
	if authStyle != "none" && strings.TrimSpace(token) == "" {
		prefix := socialAccountEnvPrefix(alias, account)
		if prefix == "" {
			prefix = "SOCIAL_<ACCOUNT>_"
		}
		return socialRuntimeContext{}, fmt.Errorf("%s access token not found (set --token, %s%s_ACCESS_TOKEN, or %s_ACCESS_TOKEN)", socialPlatformLabel(platform), prefix, strings.ToUpper(socialPlatformLabel(platform)), strings.ToUpper(socialPlatformLabel(platform)))
	}

	ids, idSource := resolveSocialDefaultIDs(platform, alias, account)
	logPath := resolveSocialLogPath(settings, platform)
	source := strings.Join(nonEmpty(tokenSource, idSource), ",")
	return socialRuntimeContext{
		Platform:                platform,
		AccountAlias:            alias,
		Environment:             parsedEnv,
		BaseURL:                 strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIVersion:              strings.Trim(strings.TrimSpace(apiVersion), "/"),
		AuthStyle:               authStyle,
		Token:                   token,
		Source:                  source,
		LogPath:                 logPath,
		FacebookPageID:          ids.FacebookPageID,
		InstagramBusinessID:     ids.InstagramBusinessID,
		XUserID:                 ids.XUserID,
		XUsername:               ids.XUsername,
		LinkedInPersonURN:       ids.LinkedInPersonURN,
		LinkedInOrganizationURN: ids.LinkedInOrganizationURN,
		RedditUsername:          ids.RedditUsername,
	}, nil
}

type socialDefaultIDs struct {
	FacebookPageID          string
	InstagramBusinessID     string
	XUserID                 string
	XUsername               string
	LinkedInPersonURN       string
	LinkedInOrganizationURN string
	RedditUsername          string
}

func resolveSocialDefaultIDs(platform socialPlatform, alias string, account SocialAccountSetting) (socialDefaultIDs, string) {
	ids := socialDefaultIDs{
		FacebookPageID:          strings.TrimSpace(account.FacebookPageID),
		InstagramBusinessID:     strings.TrimSpace(account.InstagramBusinessID),
		XUserID:                 strings.TrimSpace(account.XUserID),
		XUsername:               strings.TrimSpace(account.XUsername),
		LinkedInPersonURN:       strings.TrimSpace(account.LinkedInPersonURN),
		LinkedInOrganizationURN: strings.TrimSpace(account.LinkedInOrganizationURN),
		RedditUsername:          strings.TrimSpace(account.RedditUsername),
	}
	sources := make([]string, 0, 6)
	if ids.FacebookPageID != "" {
		sources = append(sources, "settings.facebook_page_id")
	}
	if ids.InstagramBusinessID != "" {
		sources = append(sources, "settings.instagram_business_id")
	}
	if ids.XUserID != "" {
		sources = append(sources, "settings.x_user_id")
	}
	if ids.XUsername != "" {
		sources = append(sources, "settings.x_username")
	}
	if ids.LinkedInPersonURN != "" {
		sources = append(sources, "settings.linkedin_person_urn")
	}
	if ids.LinkedInOrganizationURN != "" {
		sources = append(sources, "settings.linkedin_organization_urn")
	}
	if ids.RedditUsername != "" {
		sources = append(sources, "settings.reddit_username")
	}
	prefix := socialAccountEnvPrefix(alias, account)
	if value := strings.TrimSpace(resolveSocialEnv(alias, account, "FACEBOOK_PAGE_ID")); value != "" {
		ids.FacebookPageID = value
		sources = append(sources, "env:"+prefix+"FACEBOOK_PAGE_ID")
	}
	if value := strings.TrimSpace(os.Getenv("FACEBOOK_PAGE_ID")); value != "" {
		ids.FacebookPageID = value
		sources = append(sources, "env:FACEBOOK_PAGE_ID")
	}
	if value := strings.TrimSpace(resolveSocialEnv(alias, account, "INSTAGRAM_BUSINESS_ID")); value != "" {
		ids.InstagramBusinessID = value
		sources = append(sources, "env:"+prefix+"INSTAGRAM_BUSINESS_ID")
	}
	if value := strings.TrimSpace(os.Getenv("INSTAGRAM_BUSINESS_ID")); value != "" {
		ids.InstagramBusinessID = value
		sources = append(sources, "env:INSTAGRAM_BUSINESS_ID")
	}
	if value := strings.TrimSpace(resolveSocialEnv(alias, account, "X_USER_ID")); value != "" {
		ids.XUserID = value
		sources = append(sources, "env:"+prefix+"X_USER_ID")
	}
	if value := strings.TrimSpace(os.Getenv("X_USER_ID")); value != "" {
		ids.XUserID = value
		sources = append(sources, "env:X_USER_ID")
	}
	if value := strings.TrimSpace(resolveSocialEnv(alias, account, "X_USERNAME")); value != "" {
		ids.XUsername = value
		sources = append(sources, "env:"+prefix+"X_USERNAME")
	}
	if value := strings.TrimSpace(os.Getenv("X_USERNAME")); value != "" {
		ids.XUsername = value
		sources = append(sources, "env:X_USERNAME")
	}
	if value := strings.TrimSpace(resolveSocialEnv(alias, account, "LINKEDIN_PERSON_URN")); value != "" {
		ids.LinkedInPersonURN = value
		sources = append(sources, "env:"+prefix+"LINKEDIN_PERSON_URN")
	}
	if value := strings.TrimSpace(os.Getenv("LINKEDIN_PERSON_URN")); value != "" {
		ids.LinkedInPersonURN = value
		sources = append(sources, "env:LINKEDIN_PERSON_URN")
	}
	if value := strings.TrimSpace(resolveSocialEnv(alias, account, "LINKEDIN_ORGANIZATION_URN")); value != "" {
		ids.LinkedInOrganizationURN = value
		sources = append(sources, "env:"+prefix+"LINKEDIN_ORGANIZATION_URN")
	}
	if value := strings.TrimSpace(os.Getenv("LINKEDIN_ORGANIZATION_URN")); value != "" {
		ids.LinkedInOrganizationURN = value
		sources = append(sources, "env:LINKEDIN_ORGANIZATION_URN")
	}
	if value := strings.TrimSpace(resolveSocialEnv(alias, account, "REDDIT_USERNAME")); value != "" {
		ids.RedditUsername = value
		sources = append(sources, "env:"+prefix+"REDDIT_USERNAME")
	}
	if value := strings.TrimSpace(os.Getenv("REDDIT_USERNAME")); value != "" {
		ids.RedditUsername = value
		sources = append(sources, "env:REDDIT_USERNAME")
	}
	filtered := make([]string, 0, len(sources))
	seen := map[string]bool{}
	for _, entry := range sources {
		entry = strings.TrimSpace(entry)
		if entry == "" || seen[entry] {
			continue
		}
		seen[entry] = true
		filtered = append(filtered, entry)
	}
	return ids, strings.Join(filtered, ",")
}

func socialPlatformSettings(settings Settings, platform socialPlatform) SocialPlatformSettings {
	switch platform {
	case socialPlatformFacebook:
		return settings.Social.Facebook
	case socialPlatformInstagram:
		return settings.Social.Instagram
	case socialPlatformX:
		return settings.Social.X
	case socialPlatformLinkedIn:
		return settings.Social.LinkedIn
	case socialPlatformReddit:
		return settings.Social.Reddit
	default:
		return SocialPlatformSettings{}
	}
}

func socialDefaultBaseURL(platform socialPlatform) string {
	switch platform {
	case socialPlatformFacebook, socialPlatformInstagram:
		return "https://graph.facebook.com"
	case socialPlatformX:
		return "https://api.twitter.com"
	case socialPlatformLinkedIn:
		return "https://api.linkedin.com"
	case socialPlatformReddit:
		return "https://oauth.reddit.com"
	default:
		return ""
	}
}

func socialDefaultAPIVersion(platform socialPlatform) string {
	switch platform {
	case socialPlatformFacebook, socialPlatformInstagram:
		return "v22.0"
	case socialPlatformX:
		return "2"
	case socialPlatformLinkedIn:
		return "v2"
	case socialPlatformReddit:
		return ""
	default:
		return ""
	}
}

func socialDefaultAuthStyle(platform socialPlatform) string {
	switch platform {
	case socialPlatformFacebook, socialPlatformInstagram:
		return "query"
	case socialPlatformX, socialPlatformLinkedIn:
		return "bearer"
	case socialPlatformReddit:
		return "bearer"
	default:
		return "bearer"
	}
}

func resolveSocialAccessToken(platform socialPlatform, alias string, account SocialAccountSetting, override string) (string, string) {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override), "flag:--token"
	}
	if ref := strings.TrimSpace(socialAccountTokenEnvRef(platform, account)); ref != "" {
		if value := strings.TrimSpace(os.Getenv(ref)); value != "" {
			return value, "env:" + ref
		}
	}
	prefix := socialAccountEnvPrefix(alias, account)
	switch platform {
	case socialPlatformFacebook:
		if value := strings.TrimSpace(resolveSocialEnv(alias, account, "FACEBOOK_ACCESS_TOKEN")); value != "" {
			return value, "env:" + prefix + "FACEBOOK_ACCESS_TOKEN"
		}
		if value := strings.TrimSpace(os.Getenv("FACEBOOK_ACCESS_TOKEN")); value != "" {
			return value, "env:FACEBOOK_ACCESS_TOKEN"
		}
		if value := strings.TrimSpace(os.Getenv("META_ACCESS_TOKEN")); value != "" {
			return value, "env:META_ACCESS_TOKEN"
		}
	case socialPlatformInstagram:
		if value := strings.TrimSpace(resolveSocialEnv(alias, account, "INSTAGRAM_ACCESS_TOKEN")); value != "" {
			return value, "env:" + prefix + "INSTAGRAM_ACCESS_TOKEN"
		}
		if value := strings.TrimSpace(os.Getenv("INSTAGRAM_ACCESS_TOKEN")); value != "" {
			return value, "env:INSTAGRAM_ACCESS_TOKEN"
		}
		if value := strings.TrimSpace(os.Getenv("META_ACCESS_TOKEN")); value != "" {
			return value, "env:META_ACCESS_TOKEN"
		}
	case socialPlatformX:
		if value := strings.TrimSpace(resolveSocialEnv(alias, account, "X_BEARER_TOKEN")); value != "" {
			return value, "env:" + prefix + "X_BEARER_TOKEN"
		}
		if value := strings.TrimSpace(resolveSocialEnv(alias, account, "X_ACCESS_TOKEN")); value != "" {
			return value, "env:" + prefix + "X_ACCESS_TOKEN"
		}
		if value := strings.TrimSpace(os.Getenv("X_BEARER_TOKEN")); value != "" {
			return value, "env:X_BEARER_TOKEN"
		}
		if value := strings.TrimSpace(os.Getenv("TWITTER_BEARER_TOKEN")); value != "" {
			return value, "env:TWITTER_BEARER_TOKEN"
		}
	case socialPlatformLinkedIn:
		if value := strings.TrimSpace(resolveSocialEnv(alias, account, "LINKEDIN_ACCESS_TOKEN")); value != "" {
			return value, "env:" + prefix + "LINKEDIN_ACCESS_TOKEN"
		}
		if value := strings.TrimSpace(os.Getenv("LINKEDIN_ACCESS_TOKEN")); value != "" {
			return value, "env:LINKEDIN_ACCESS_TOKEN"
		}
	case socialPlatformReddit:
		if value := strings.TrimSpace(resolveSocialEnv(alias, account, "REDDIT_ACCESS_TOKEN")); value != "" {
			return value, "env:" + prefix + "REDDIT_ACCESS_TOKEN"
		}
		if value := strings.TrimSpace(os.Getenv("REDDIT_ACCESS_TOKEN")); value != "" {
			return value, "env:REDDIT_ACCESS_TOKEN"
		}
		if value := strings.TrimSpace(os.Getenv("SI_PUBLISH_REDDIT_ACCESS_TOKEN")); value != "" {
			return value, "env:SI_PUBLISH_REDDIT_ACCESS_TOKEN"
		}
	}
	return "", ""
}

func socialAccountTokenEnvRef(platform socialPlatform, account SocialAccountSetting) string {
	switch platform {
	case socialPlatformFacebook:
		return strings.TrimSpace(account.FacebookAccessTokenEnv)
	case socialPlatformInstagram:
		return strings.TrimSpace(account.InstagramAccessTokenEnv)
	case socialPlatformX:
		return strings.TrimSpace(account.XAccessTokenEnv)
	case socialPlatformLinkedIn:
		return strings.TrimSpace(account.LinkedInAccessTokenEnv)
	case socialPlatformReddit:
		return strings.TrimSpace(account.RedditAccessTokenEnv)
	default:
		return ""
	}
}

func resolveSocialAccountSelection(settings Settings, accountFlag string) (string, SocialAccountSetting) {
	selected := strings.TrimSpace(accountFlag)
	if selected == "" {
		selected = strings.TrimSpace(settings.Social.DefaultAccount)
	}
	if selected == "" {
		selected = strings.TrimSpace(os.Getenv("SOCIAL_DEFAULT_ACCOUNT"))
	}
	if selected == "" {
		aliases := socialAccountAliases(settings)
		if len(aliases) == 1 {
			selected = aliases[0]
		}
	}
	if selected == "" {
		return "", SocialAccountSetting{}
	}
	if entry, ok := settings.Social.Accounts[selected]; ok {
		return selected, entry
	}
	return selected, SocialAccountSetting{}
}

func socialAccountAliases(settings Settings) []string {
	if len(settings.Social.Accounts) == 0 {
		return nil
	}
	out := make([]string, 0, len(settings.Social.Accounts))
	for alias := range settings.Social.Accounts {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

func socialAccountEnvPrefix(alias string, account SocialAccountSetting) string {
	if prefix := strings.TrimSpace(account.VaultPrefix); prefix != "" {
		if strings.HasSuffix(prefix, "_") {
			return strings.ToUpper(prefix)
		}
		return strings.ToUpper(prefix) + "_"
	}
	alias = slugUpper(alias)
	if alias == "" {
		return ""
	}
	return "SOCIAL_" + alias + "_"
}

func resolveSocialEnv(alias string, account SocialAccountSetting, key string) string {
	prefix := socialAccountEnvPrefix(alias, account)
	if prefix != "" {
		if value := strings.TrimSpace(os.Getenv(prefix + key)); value != "" {
			return value
		}
	}
	return ""
}

func socialVersionedPath(runtime socialRuntimeContext, rawPath string) (string, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return "", fmt.Errorf("request path is required")
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path, nil
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	version := strings.Trim(strings.TrimSpace(runtime.APIVersion), "/")
	if version == "" {
		return path, nil
	}
	if strings.HasPrefix(path, "/"+version+"/") || path == "/"+version {
		return path, nil
	}
	return "/" + version + path, nil
}

func socialDo(ctx context.Context, runtime socialRuntimeContext, req socialRequest) (socialResponse, error) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	providerID := socialProviderID(runtime.Platform)
	path, err := socialVersionedPath(runtime, req.Path)
	if err != nil {
		return socialResponse{}, err
	}
	params := cloneStringMap(req.Params)
	if runtime.AuthStyle == "query" && strings.TrimSpace(runtime.Token) != "" {
		if strings.TrimSpace(params["access_token"]) == "" {
			params["access_token"] = strings.TrimSpace(runtime.Token)
		}
	}
	endpoint, err := resolveSocialURL(runtime.BaseURL, path, params)
	if err != nil {
		return socialResponse{}, err
	}

	return integrationruntime.DoHTTP(ctx, integrationruntime.HTTPExecutorOptions[socialResponse]{
		Provider:    providerID,
		Subject:     runtime.AccountAlias,
		Method:      method,
		RequestPath: path,
		Endpoint:    endpoint,
		MaxRetries:  2,
		Client:      httpx.SharedClient(45 * time.Second),
		BuildRequest: func(callCtx context.Context, callMethod string, callEndpoint string) (*http.Request, error) {
			bodyReader := io.Reader(nil)
			if strings.TrimSpace(req.RawBody) != "" {
				bodyReader = strings.NewReader(req.RawBody)
			} else if req.JSONBody != nil {
				raw, marshalErr := json.Marshal(req.JSONBody)
				if marshalErr != nil {
					return nil, marshalErr
				}
				bodyReader = bytes.NewReader(raw)
			}
			httpReq, reqErr := http.NewRequestWithContext(callCtx, callMethod, callEndpoint, bodyReader)
			if reqErr != nil {
				return nil, reqErr
			}
			spec := providers.Resolve(providerID)
			accept := strings.TrimSpace(spec.Accept)
			if accept == "" {
				accept = "application/json"
			}
			httpReq.Header.Set("Accept", accept)
			userAgent := strings.TrimSpace(spec.UserAgent)
			if userAgent == "" {
				userAgent = "si-social/1.0"
			}
			httpReq.Header.Set("User-Agent", userAgent)
			for key, value := range spec.DefaultHeaders {
				key = strings.TrimSpace(key)
				if key == "" {
					continue
				}
				httpReq.Header.Set(key, strings.TrimSpace(value))
			}
			for key, value := range req.Headers {
				key = strings.TrimSpace(key)
				if key == "" {
					continue
				}
				httpReq.Header.Set(key, value)
			}
			if runtime.AuthStyle == "bearer" && strings.TrimSpace(runtime.Token) != "" {
				httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(runtime.Token))
			}
			if runtime.Platform == socialPlatformLinkedIn && strings.TrimSpace(httpReq.Header.Get("LinkedIn-Version")) == "" {
				httpReq.Header.Set("LinkedIn-Version", strings.TrimPrefix(strings.TrimSpace(runtime.APIVersion), "v"))
			}
			if bodyReader != nil {
				contentType := strings.TrimSpace(req.ContentType)
				if contentType == "" {
					contentType = "application/json"
				}
				httpReq.Header.Set("Content-Type", contentType)
			}
			return httpReq, nil
		},
		NormalizeResponse: normalizeSocialResponse,
		StatusCode: func(resp socialResponse) int {
			return resp.StatusCode
		},
		NormalizeHTTPError: func(statusCode int, headers http.Header, body string) error {
			return normalizeSocialHTTPError(runtime.Platform, statusCode, headers, body)
		},
		IsRetryableNetwork: socialIsRetryableNetwork,
		IsRetryableHTTP:    socialIsRetryableHTTP,
		OnCacheHit: func(resp socialResponse) {
			socialLogEvent(runtime.LogPath, map[string]any{
				"ts":          time.Now().UTC().Format(time.RFC3339Nano),
				"event":       "cache_hit",
				"platform":    socialPlatformLabel(runtime.Platform),
				"account":     runtime.AccountAlias,
				"environment": runtime.Environment,
				"method":      method,
				"path":        sanitizeURL(endpoint),
				"status_code": resp.StatusCode,
			})
		},
		OnResponse: func(_ int, resp socialResponse, _ http.Header, duration time.Duration) {
			socialLogEvent(runtime.LogPath, map[string]any{
				"ts":          time.Now().UTC().Format(time.RFC3339Nano),
				"event":       "response",
				"platform":    socialPlatformLabel(runtime.Platform),
				"account":     runtime.AccountAlias,
				"environment": runtime.Environment,
				"method":      method,
				"path":        sanitizeURL(endpoint),
				"status_code": resp.StatusCode,
				"request_id":  resp.RequestID,
				"duration_ms": duration.Milliseconds(),
			})
		},
		OnError: func(_ int, callErr error, duration time.Duration) {
			socialLogEvent(runtime.LogPath, map[string]any{
				"ts":          time.Now().UTC().Format(time.RFC3339Nano),
				"event":       "error",
				"platform":    socialPlatformLabel(runtime.Platform),
				"account":     runtime.AccountAlias,
				"environment": runtime.Environment,
				"method":      method,
				"path":        sanitizeURL(endpoint),
				"duration_ms": duration.Milliseconds(),
				"error":       redactSocialSensitive(callErr.Error()),
			})
		},
	})
}

func socialIsRetryableNetwork(method string, _ error) bool {
	return netpolicy.IsSafeMethod(method)
}

func socialIsRetryableHTTP(method string, statusCode int, _ http.Header, _ string) bool {
	if !netpolicy.IsSafeMethod(method) {
		return false
	}
	switch statusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return statusCode >= 500
}

func normalizeSocialResponse(httpResp *http.Response, body string) socialResponse {
	out := socialResponse{}
	if httpResp == nil {
		return out
	}
	out.StatusCode = httpResp.StatusCode
	out.Status = httpResp.Status
	out.Body = redactSocialSensitive(strings.TrimSpace(body))
	out.RequestID = resolveSocialRequestID(httpResp.Header)
	out.Headers = flattenHeaders(httpResp.Header)
	if strings.TrimSpace(body) == "" {
		return out
	}
	var parsed any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return out
	}
	switch typed := parsed.(type) {
	case map[string]any:
		if raw, ok := typed["data"].([]any); ok {
			out.List = anySliceToMaps(raw)
			out.Data = typed
			return out
		}
		if raw, ok := typed["items"].([]any); ok {
			out.List = anySliceToMaps(raw)
			out.Data = typed
			return out
		}
		if raw, ok := typed["elements"].([]any); ok {
			out.List = anySliceToMaps(raw)
			out.Data = typed
			return out
		}
		if raw, ok := typed["value"].([]any); ok {
			out.List = anySliceToMaps(raw)
			out.Data = typed
			return out
		}
		if inner, ok := typed["data"].(map[string]any); ok {
			out.Data = inner
			return out
		}
		out.Data = typed
	case []any:
		out.List = anySliceToMaps(typed)
	}
	return out
}

func resolveSocialRequestID(headers http.Header) string {
	candidates := []string{
		"X-Request-ID",
		"X-Request-Id",
		"X-Correlation-ID",
		"x-fb-trace-id",
		"x-twitter-response-tags",
		"x-li-request-id",
	}
	for _, key := range candidates {
		if value := strings.TrimSpace(headers.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func flattenHeaders(headers http.Header) map[string]string {
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
		out[key] = redactSocialSensitive(strings.Join(headers.Values(key), ","))
	}
	return out
}

func normalizeSocialHTTPError(platform socialPlatform, statusCode int, headers http.Header, rawBody string) *socialAPIErrorDetails {
	details := &socialAPIErrorDetails{
		Platform:   platform,
		StatusCode: statusCode,
		RequestID:  resolveSocialRequestID(headers),
		RawBody:    redactSocialSensitive(strings.TrimSpace(rawBody)),
	}
	body := strings.TrimSpace(rawBody)
	if body == "" {
		details.Message = "empty response body"
		return details
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		details.Message = redactSocialSensitive(body)
		return details
	}
	if errObj, ok := parsed["error"].(map[string]any); ok {
		if value, ok := readSocialIntLike(errObj["code"]); ok {
			details.Code = int(value)
		}
		if value, ok := errObj["type"].(string); ok {
			details.Type = redactSocialSensitive(strings.TrimSpace(value))
		}
		if value, ok := errObj["message"].(string); ok {
			details.Message = redactSocialSensitive(strings.TrimSpace(value))
		}
		if value, ok := errObj["error_subcode"]; ok {
			if subcode, ok := readSocialIntLike(value); ok && details.Code == 0 {
				details.Code = int(subcode)
			}
		}
		if value, ok := errObj["fbtrace_id"].(string); ok && strings.TrimSpace(details.RequestID) == "" {
			details.RequestID = strings.TrimSpace(value)
		}
	}
	if details.Message == "" {
		if value, ok := parsed["message"].(string); ok {
			details.Message = redactSocialSensitive(strings.TrimSpace(value))
		}
	}
	if details.Message == "" {
		if value, ok := parsed["detail"].(string); ok {
			details.Message = redactSocialSensitive(strings.TrimSpace(value))
		}
	}
	if details.Status == "" {
		if value, ok := parsed["status"].(string); ok {
			details.Status = redactSocialSensitive(strings.TrimSpace(value))
		}
	}
	if details.Status == "" {
		if value, ok := parsed["title"].(string); ok {
			details.Status = redactSocialSensitive(strings.TrimSpace(value))
		}
	}
	if details.Code == 0 {
		if value, ok := readSocialIntLike(parsed["status"]); ok {
			details.Code = int(value)
		}
	}
	if details.Code == 0 {
		if value, ok := readSocialIntLike(parsed["serviceErrorCode"]); ok {
			details.Code = int(value)
		}
	}
	if details.Code == 0 && statusCode > 0 {
		details.Code = statusCode
	}
	if details.Message == "" {
		details.Message = "social api request failed"
	}
	return details
}

func parseSocialParams(values []string) map[string]string {
	out := map[string]string{}
	for _, entry := range values {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
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

func parseSocialJSONBody(raw string, fallback []string) map[string]any {
	trimmed := strings.TrimSpace(raw)
	if trimmed != "" {
		var obj map[string]any
		if err := json.Unmarshal([]byte(trimmed), &obj); err == nil && obj != nil {
			return obj
		}
		return map[string]any{"body": trimmed}
	}
	out := map[string]any{}
	for key, value := range parseSocialParams(fallback) {
		parsed := any(value)
		switch strings.ToLower(value) {
		case "true":
			parsed = true
		case "false":
			parsed = false
		case "null":
			parsed = nil
		default:
			if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
				parsed = intVal
			} else if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
				parsed = floatVal
			}
		}
		out[key] = parsed
	}
	return out
}

func resolveSocialLogPath(settings Settings, platform socialPlatform) string {
	if value := strings.TrimSpace(os.Getenv("SI_SOCIAL_LOG_FILE")); value != "" {
		return value
	}
	if value := strings.TrimSpace(settings.Social.LogFile); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("SI_SOCIAL_" + strings.ToUpper(socialPlatformLabel(platform)) + "_LOG_FILE")); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".si", "logs", "social-"+socialPlatformLabel(platform)+".log")
}

func socialLogEvent(path string, event map[string]any) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	if event == nil {
		event = map[string]any{}
	}
	if _, ok := event["ts"]; !ok {
		event["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	for key, value := range event {
		if asString, ok := value.(string); ok {
			event[key] = redactSocialSensitive(asString)
		}
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = file.Write(raw)
	_ = file.Close()
}

func loadSocialLogEvents(path string) ([]map[string]any, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	events := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		events = append(events, obj)
	}
	return events, nil
}

func formatSocialContext(runtime socialRuntimeContext) string {
	account := strings.TrimSpace(runtime.AccountAlias)
	if account == "" {
		account = "(default)"
	}
	return fmt.Sprintf("platform=%s account=%s env=%s auth=%s base=%s version=%s", socialPlatformLabel(runtime.Platform), account, runtime.Environment, runtime.AuthStyle, runtime.BaseURL, runtime.APIVersion)
}

func mustSocialRuntime(input socialRuntimeContextInput) socialRuntimeContext {
	runtime, err := resolveSocialRuntimeContext(input)
	if err != nil {
		fatal(err)
	}
	return runtime
}

func printSocialContextBanner(runtime socialRuntimeContext, jsonOut bool) {
	if jsonOut {
		return
	}
	fmt.Printf("%s %s\n", styleDim("social context:"), formatSocialContext(runtime))
}

func socialPlatformTokenEnvKey(platform socialPlatform) string {
	switch platform {
	case socialPlatformFacebook:
		return "FACEBOOK_ACCESS_TOKEN"
	case socialPlatformInstagram:
		return "INSTAGRAM_ACCESS_TOKEN"
	case socialPlatformX:
		return "X_BEARER_TOKEN"
	case socialPlatformLinkedIn:
		return "LINKEDIN_ACCESS_TOKEN"
	case socialPlatformReddit:
		return "REDDIT_ACCESS_TOKEN"
	default:
		return "ACCESS_TOKEN"
	}
}

func socialPlatformDefaultProbePath(platform socialPlatform, authStyle string) string {
	if normalizeSocialAuthStyle(authStyle) == "none" {
		if method, path, ok := providers.PublicProbe(socialProviderID(platform)); ok {
			_ = method
			if strings.TrimSpace(path) != "" {
				return path
			}
		}
	}
	switch platform {
	case socialPlatformFacebook, socialPlatformInstagram:
		return "/me"
	case socialPlatformX:
		return "/users/me"
	case socialPlatformLinkedIn:
		return "/me"
	case socialPlatformReddit:
		return "/api/v1/me"
	default:
		return "/"
	}
}

func socialPlatformDefaultProbeParams(platform socialPlatform, authStyle string) map[string]string {
	if normalizeSocialAuthStyle(authStyle) == "none" {
		return map[string]string{}
	}
	switch platform {
	case socialPlatformFacebook:
		return map[string]string{"fields": "id,name"}
	case socialPlatformInstagram:
		return map[string]string{"fields": "id,username,account_type"}
	default:
		return map[string]string{}
	}
}

func socialPlatformRequiresQueryToken(platform socialPlatform) bool {
	return platform == socialPlatformFacebook || platform == socialPlatformInstagram
}

func socialPlatformContextID(platform socialPlatform, runtime socialRuntimeContext) string {
	switch platform {
	case socialPlatformFacebook:
		return runtime.FacebookPageID
	case socialPlatformInstagram:
		return runtime.InstagramBusinessID
	case socialPlatformX:
		if runtime.XUserID != "" {
			return runtime.XUserID
		}
		return runtime.XUsername
	case socialPlatformLinkedIn:
		if runtime.LinkedInPersonURN != "" {
			return runtime.LinkedInPersonURN
		}
		return runtime.LinkedInOrganizationURN
	case socialPlatformReddit:
		return runtime.RedditUsername
	default:
		return ""
	}
}

func socialResolveIDArg(arg string, fallback string, label string) (string, error) {
	value := strings.TrimSpace(arg)
	if value == "" {
		value = strings.TrimSpace(fallback)
	}
	if value == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	return value, nil
}

func socialParseFlagSet(name string, args []string, boolFlags map[string]bool) (*flag.FlagSet, []string) {
	args = stripeFlagsFirst(args, boolFlags)
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	_ = fs.Parse(args)
	return fs, args
}

func socialSummarizeItem(item map[string]any) string {
	if len(item) == 0 {
		return "-"
	}
	id := "-"
	for _, key := range []string{"id", "name", "urn", "username", "handle"} {
		if value, ok := item[key]; ok {
			id = stringifySocialAny(value)
			if id != "-" && strings.TrimSpace(id) != "" {
				break
			}
		}
	}
	title := "-"
	for _, key := range []string{"title", "text", "message", "headline", "status"} {
		if value, ok := item[key]; ok {
			title = stringifySocialAny(value)
			if title != "-" && strings.TrimSpace(title) != "" {
				break
			}
		}
	}
	return strings.TrimSpace(id + " " + title)
}

func stringifySocialAny(value any) string {
	switch typed := value.(type) {
	case nil:
		return "-"
	case string:
		if strings.TrimSpace(typed) == "" {
			return "-"
		}
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case map[string]any:
		raw, _ := json.Marshal(typed)
		return string(raw)
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprintf("%v", typed)
		}
		return string(raw)
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func readSocialIntLike(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), true
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func anySliceToMaps(values []any) []map[string]any {
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

func resolveSocialURL(baseURL string, path string, params map[string]string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("request path is required")
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		u, err := url.Parse(path)
		if err != nil {
			return "", err
		}
		addSocialQuery(u, params)
		return u.String(), nil
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	rel, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	u := base.ResolveReference(rel)
	addSocialQuery(u, params)
	return u.String(), nil
}

func addSocialQuery(u *url.URL, params map[string]string) {
	if u == nil || len(params) == 0 {
		return
	}
	query := u.Query()
	for key, value := range params {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		query.Set(key, strings.TrimSpace(value))
	}
	u.RawQuery = query.Encode()
}

func sanitizeURL(raw string) string {
	raw = redactSocialSensitive(raw)
	if u, err := url.Parse(raw); err == nil {
		u.RawQuery = ""
		return u.String()
	}
	return raw
}

var (
	reSocialBearer = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._-]+\b`)
	reSocialToken  = regexp.MustCompile(`(?i)(access_token=)([^&\s]+)`)
)

func redactSocialSensitive(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	value = reSocialBearer.ReplaceAllString(value, "Bearer ***")
	value = reSocialToken.ReplaceAllString(value, "$1***")
	return value
}

func previewSocialSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "-"
	}
	secret = redactSocialSensitive(secret)
	if len(secret) <= 10 {
		return secret
	}
	return secret[:8] + "..."
}
