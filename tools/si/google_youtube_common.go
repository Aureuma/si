package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type googleYouTubeCommonOptions struct {
	account       *string
	env           *string
	authMode      *string
	apiKey        *string
	baseURL       *string
	uploadBaseURL *string
	projectID     *string
	language      *string
	region        *string
	clientID      *string
	clientSecret  *string
	redirectURI   *string
	accessToken   *string
	refreshToken  *string
	jsonOut       *bool
	raw           *bool
}

func googleYouTubeCommonFlagSet(name string, args []string, allowRaw bool) (*flag.FlagSet, *googleYouTubeCommonOptions) {
	boolFlags := map[string]bool{"json": true}
	if allowRaw {
		boolFlags["raw"] = true
	}
	args = stripeFlagsFirst(args, boolFlags)
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	opts := &googleYouTubeCommonOptions{}
	opts.account = fs.String("account", "", "account alias")
	opts.env = fs.String("env", "", "environment (prod|staging|dev)")
	opts.authMode = fs.String("mode", "", "auth mode (api-key|oauth)")
	opts.apiKey = fs.String("api-key", "", "override youtube api key")
	opts.baseURL = fs.String("base-url", "", "youtube api base url")
	opts.uploadBaseURL = fs.String("upload-base-url", "", "youtube upload base url")
	opts.projectID = fs.String("project-id", "", "google project id")
	opts.language = fs.String("language", "", "language code")
	opts.region = fs.String("region", "", "region code")
	opts.clientID = fs.String("client-id", "", "oauth client id")
	opts.clientSecret = fs.String("client-secret", "", "oauth client secret")
	opts.redirectURI = fs.String("redirect-uri", "", "oauth redirect uri")
	opts.accessToken = fs.String("access-token", "", "oauth access token override")
	opts.refreshToken = fs.String("refresh-token", "", "oauth refresh token override")
	opts.jsonOut = fs.Bool("json", false, "output json")
	if allowRaw {
		opts.raw = fs.Bool("raw", false, "print raw response body")
	}
	return fs, opts
}

func (o *googleYouTubeCommonOptions) runtimeInput() googleYouTubeRuntimeContextInput {
	if o == nil {
		return googleYouTubeRuntimeContextInput{}
	}
	return googleYouTubeRuntimeContextInput{
		AccountFlag:       strings.TrimSpace(stringValue(o.account)),
		EnvFlag:           strings.TrimSpace(stringValue(o.env)),
		AuthModeFlag:      strings.TrimSpace(stringValue(o.authMode)),
		APIKeyFlag:        strings.TrimSpace(stringValue(o.apiKey)),
		BaseURLFlag:       strings.TrimSpace(stringValue(o.baseURL)),
		UploadBaseURLFlag: strings.TrimSpace(stringValue(o.uploadBaseURL)),
		ProjectIDFlag:     strings.TrimSpace(stringValue(o.projectID)),
		LanguageFlag:      strings.TrimSpace(stringValue(o.language)),
		RegionFlag:        strings.TrimSpace(stringValue(o.region)),
		ClientIDFlag:      strings.TrimSpace(stringValue(o.clientID)),
		ClientSecretFlag:  strings.TrimSpace(stringValue(o.clientSecret)),
		RedirectURIFlag:   strings.TrimSpace(stringValue(o.redirectURI)),
		AccessTokenFlag:   strings.TrimSpace(stringValue(o.accessToken)),
		RefreshTokenFlag:  strings.TrimSpace(stringValue(o.refreshToken)),
	}
}

func (o *googleYouTubeCommonOptions) mustClient() (googleYouTubeRuntimeContext, googleYouTubeBridgeClient) {
	return mustGoogleYouTubeClient(o.runtimeInput())
}

func (o *googleYouTubeCommonOptions) json() bool {
	if o == nil || o.jsonOut == nil {
		return false
	}
	return *o.jsonOut
}

func (o *googleYouTubeCommonOptions) rawEnabled() bool {
	if o == nil || o.raw == nil {
		return false
	}
	return *o.raw
}

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func googleYouTubeParts(raw string, fallback string) string {
	value := strings.TrimSpace(raw)
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func parseGoogleYouTubeBody(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if strings.HasPrefix(raw, "@") {
		path := strings.TrimSpace(strings.TrimPrefix(raw, "@"))
		if path == "" {
			return "", fmt.Errorf("body file path is empty")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return raw, nil
}

func parseGoogleYouTubeIDsCSV(raw string) string {
	values := parseGoogleCSVList(raw)
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, ",")
}

func googleYouTubeRequireOAuth(runtime googleYouTubeRuntimeContext, operation string) error {
	if strings.TrimSpace(runtime.AuthMode) == "oauth" {
		return nil
	}
	if strings.TrimSpace(operation) == "" {
		operation = "this command"
	}
	return fmt.Errorf("%s requires oauth mode; run with --mode oauth and login via `si google youtube auth login`", operation)
}

func googleYouTubeLogPathForSettings(settings Settings) string {
	if value := strings.TrimSpace(resolveGoogleYouTubeLogPath(settings)); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".si", "logs", "google-youtube.log")
}

func googleYouTubeAllAccountAliases(settings Settings) []string {
	aliases := googleYouTubeAccountAliases(settings)
	if len(aliases) == 0 {
		return nil
	}
	out := append([]string{}, aliases...)
	sort.Strings(out)
	return out
}
