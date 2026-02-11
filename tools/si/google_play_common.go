package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type googlePlayCommonOptions struct {
	account          *string
	env              *string
	packageName      *string
	language         *string
	projectID        *string
	developerAccount *string
	serviceJSON      *string
	serviceFile      *string
	baseURL          *string
	uploadBaseURL    *string
	customAppBaseURL *string
	jsonOut          *bool
	raw              *bool
}

func googlePlayCommonFlagSet(name string, args []string, allowRaw bool) (*flag.FlagSet, *googlePlayCommonOptions) {
	_ = args
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	opts := &googlePlayCommonOptions{}
	opts.account = fs.String("account", "", "account alias")
	opts.env = fs.String("env", "", "environment (prod|staging|dev)")
	opts.packageName = fs.String("package", "", "android package name (e.g. com.example.app)")
	opts.language = fs.String("language", "", "language code (BCP-47, e.g. en-US)")
	opts.projectID = fs.String("project-id", "", "google project id")
	opts.developerAccount = fs.String("developer-account", "", "google play developer account id")
	opts.serviceJSON = fs.String("service-account-json", "", "service account json payload or @path")
	opts.serviceFile = fs.String("service-account-file", "", "path to service account json file")
	opts.baseURL = fs.String("base-url", "", "google play api base url")
	opts.uploadBaseURL = fs.String("upload-base-url", "", "google play upload api base url")
	opts.customAppBaseURL = fs.String("custom-app-base-url", "", "google play custom app api base url")
	opts.jsonOut = fs.Bool("json", false, "output json")
	if allowRaw {
		opts.raw = fs.Bool("raw", false, "print raw response body")
	}
	return fs, opts
}

func (o *googlePlayCommonOptions) runtimeInput() googlePlayRuntimeContextInput {
	if o == nil {
		return googlePlayRuntimeContextInput{}
	}
	return googlePlayRuntimeContextInput{
		AccountFlag:          strings.TrimSpace(stringValue(o.account)),
		EnvFlag:              strings.TrimSpace(stringValue(o.env)),
		PackageFlag:          strings.TrimSpace(stringValue(o.packageName)),
		LanguageFlag:         strings.TrimSpace(stringValue(o.language)),
		ProjectIDFlag:        strings.TrimSpace(stringValue(o.projectID)),
		DeveloperAccountFlag: strings.TrimSpace(stringValue(o.developerAccount)),
		ServiceAccountJSON:   strings.TrimSpace(stringValue(o.serviceJSON)),
		ServiceAccountFile:   strings.TrimSpace(stringValue(o.serviceFile)),
		BaseURLFlag:          strings.TrimSpace(stringValue(o.baseURL)),
		UploadBaseURLFlag:    strings.TrimSpace(stringValue(o.uploadBaseURL)),
		CustomAppBaseURLFlag: strings.TrimSpace(stringValue(o.customAppBaseURL)),
	}
}

func (o *googlePlayCommonOptions) mustClient() (googlePlayRuntimeContext, googlePlayBridgeClient) {
	return mustGooglePlayClient(o.runtimeInput())
}

func (o *googlePlayCommonOptions) json() bool {
	if o == nil || o.jsonOut == nil {
		return false
	}
	return *o.jsonOut
}

func (o *googlePlayCommonOptions) rawEnabled() bool {
	if o == nil || o.raw == nil {
		return false
	}
	return *o.raw
}

func parseGooglePlayBody(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if strings.HasPrefix(raw, "@") {
		path := strings.TrimSpace(strings.TrimPrefix(raw, "@"))
		if path == "" {
			return "", fmt.Errorf("body file path is empty")
		}
		data, err := readLocalFile(path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return raw, nil
}

func parseGooglePlayJSONBody(raw string) (map[string]any, error) {
	body, err := parseGooglePlayBody(raw)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(body) == "" {
		return nil, nil
	}
	decoded := map[string]any{}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		return nil, fmt.Errorf("invalid json body: %w", err)
	}
	return decoded, nil
}

func resolveGooglePlayPackage(runtime googlePlayRuntimeContext, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value != "" {
		return value, nil
	}
	if strings.TrimSpace(runtime.DefaultPackageName) != "" {
		return strings.TrimSpace(runtime.DefaultPackageName), nil
	}
	return "", fmt.Errorf("package name is required (set --package or configure google.play.accounts.<alias>.default_package_name)")
}

func resolveGooglePlayLanguage(runtime googlePlayRuntimeContext, value string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	if strings.TrimSpace(runtime.DefaultLanguageCode) != "" {
		return strings.TrimSpace(runtime.DefaultLanguageCode)
	}
	return "en-US"
}

func parseGooglePlayVersionCodes(values []string) ([]int64, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]int64, 0, len(values))
	seen := map[int64]struct{}{}
	for _, raw := range values {
		for _, token := range strings.Split(raw, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			value, err := strconv.ParseInt(token, 10, 64)
			if err != nil || value <= 0 {
				return nil, fmt.Errorf("invalid version code %q", token)
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func parseGooglePlayReleaseNotes(values []string) ([]map[string]any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	notes := make([]map[string]any, 0, len(values))
	for _, entry := range values {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid release note %q (expected <language>=<text>)", entry)
		}
		lang := strings.TrimSpace(parts[0])
		text := strings.TrimSpace(parts[1])
		if lang == "" || text == "" {
			return nil, fmt.Errorf("invalid release note %q (language and text required)", entry)
		}
		notes = append(notes, map[string]any{"language": lang, "text": text})
	}
	return notes, nil
}

func normalizeGooglePlayReleaseStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "statusunspecified", "unspecified":
		return ""
	case "draft":
		return "draft"
	case "inprogress", "in_progress", "rollout", "rolling":
		return "inProgress"
	case "halted", "paused":
		return "halted"
	case "completed", "full", "published", "production":
		return "completed"
	default:
		return ""
	}
}

func normalizeGooglePlayImageType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "phonescreenshots", "phone", "phone_screenshots", "phone-screenshots":
		return "phoneScreenshots"
	case "seveninchscreenshots", "7inch", "7-inch", "seven_inch", "seven-inch", "seven_inch_screenshots", "seven-inch-screenshots":
		return "sevenInchScreenshots"
	case "teninchscreenshots", "10inch", "10-inch", "ten_inch", "ten-inch", "ten_inch_screenshots", "ten-inch-screenshots":
		return "tenInchScreenshots"
	case "tvscreenshots", "tv", "tv_screenshots", "tv-screenshots":
		return "tvScreenshots"
	case "wearscreenshots", "wear", "wear_screenshots", "wear-screenshots":
		return "wearScreenshots"
	case "icon", "appicon", "app_icon", "app-icon":
		return "icon"
	case "featuregraphic", "feature_graphic", "feature-graphic":
		return "featureGraphic"
	case "tvbanner", "tv_banner", "tv-banner":
		return "tvBanner"
	default:
		return ""
	}
}

func collectGooglePlayImageUploads(imagesRoot string) (map[string]map[string][]string, error) {
	imagesRoot = strings.TrimSpace(imagesRoot)
	if imagesRoot == "" {
		return nil, nil
	}
	info, err := os.Stat(imagesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("images path must be a directory: %s", imagesRoot)
	}
	result := map[string]map[string][]string{}
	languageDirs, err := os.ReadDir(imagesRoot)
	if err != nil {
		return nil, err
	}
	for _, langEntry := range languageDirs {
		if !langEntry.IsDir() {
			continue
		}
		lang := strings.TrimSpace(langEntry.Name())
		if lang == "" {
			continue
		}
		langPath := filepath.Join(imagesRoot, lang)
		typeDirs, err := os.ReadDir(langPath)
		if err != nil {
			return nil, err
		}
		for _, typeEntry := range typeDirs {
			if !typeEntry.IsDir() {
				continue
			}
			imageType := normalizeGooglePlayImageType(typeEntry.Name())
			if imageType == "" {
				continue
			}
			typePath := filepath.Join(langPath, typeEntry.Name())
			files, err := os.ReadDir(typePath)
			if err != nil {
				return nil, err
			}
			for _, file := range files {
				if file.IsDir() {
					continue
				}
				name := strings.ToLower(strings.TrimSpace(file.Name()))
				if !(strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".jpeg") || strings.HasSuffix(name, ".webp")) {
					continue
				}
				if result[lang] == nil {
					result[lang] = map[string][]string{}
				}
				result[lang][imageType] = append(result[lang][imageType], filepath.Join(typePath, file.Name()))
			}
		}
	}
	for lang := range result {
		for imageType := range result[lang] {
			sort.Strings(result[lang][imageType])
		}
	}
	return result, nil
}
