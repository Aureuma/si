package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type appleAppStoreCommonOptions struct {
	account        *string
	env            *string
	bundleID       *string
	locale         *string
	platform       *string
	issuerID       *string
	keyID          *string
	privateKey     *string
	privateKeyFile *string
	projectID      *string
	baseURL        *string
	jsonOut        *bool
	raw            *bool
}

func appleAppStoreCommonFlagSet(name string, args []string, allowRaw bool) (*flag.FlagSet, *appleAppStoreCommonOptions) {
	_ = args
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	opts := &appleAppStoreCommonOptions{}
	opts.account = fs.String("account", "", "account alias")
	opts.env = fs.String("env", "", "environment (prod|staging|dev)")
	opts.bundleID = fs.String("bundle-id", "", "app bundle identifier (e.g. com.example.app)")
	opts.locale = fs.String("locale", "", "localization code (e.g. en-US)")
	opts.platform = fs.String("platform", "", "platform (IOS|MAC_OS|TV_OS|VISION_OS)")
	opts.issuerID = fs.String("issuer-id", "", "App Store Connect API issuer id")
	opts.keyID = fs.String("key-id", "", "App Store Connect API key id")
	opts.privateKey = fs.String("private-key", "", "App Store Connect API private key PEM or @file")
	opts.privateKeyFile = fs.String("private-key-file", "", "path to App Store Connect API private key (.p8)")
	opts.projectID = fs.String("project-id", "", "project/workspace identifier for context grouping")
	opts.baseURL = fs.String("base-url", "", "App Store Connect API base url")
	opts.jsonOut = fs.Bool("json", false, "output json")
	if allowRaw {
		opts.raw = fs.Bool("raw", false, "print raw response body")
	}
	return fs, opts
}

func (o *appleAppStoreCommonOptions) runtimeInput() appleAppStoreRuntimeContextInput {
	if o == nil {
		return appleAppStoreRuntimeContextInput{}
	}
	return appleAppStoreRuntimeContextInput{
		AccountFlag:    strings.TrimSpace(stringValue(o.account)),
		EnvFlag:        strings.TrimSpace(stringValue(o.env)),
		BundleIDFlag:   strings.TrimSpace(stringValue(o.bundleID)),
		LocaleFlag:     strings.TrimSpace(stringValue(o.locale)),
		PlatformFlag:   strings.TrimSpace(stringValue(o.platform)),
		IssuerIDFlag:   strings.TrimSpace(stringValue(o.issuerID)),
		KeyIDFlag:      strings.TrimSpace(stringValue(o.keyID)),
		PrivateKeyFlag: strings.TrimSpace(stringValue(o.privateKey)),
		PrivateKeyFile: strings.TrimSpace(stringValue(o.privateKeyFile)),
		ProjectIDFlag:  strings.TrimSpace(stringValue(o.projectID)),
		BaseURLFlag:    strings.TrimSpace(stringValue(o.baseURL)),
	}
}

func (o *appleAppStoreCommonOptions) mustClient() (appleAppStoreRuntimeContext, appleAppStoreBridgeClient) {
	return mustAppleAppStoreClient(o.runtimeInput())
}

func (o *appleAppStoreCommonOptions) json() bool {
	if o == nil || o.jsonOut == nil {
		return false
	}
	return *o.jsonOut
}

func (o *appleAppStoreCommonOptions) rawEnabled() bool {
	if o == nil || o.raw == nil {
		return false
	}
	return *o.raw
}

func parseAppleAppStoreBody(raw string) (string, error) {
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

func parseAppleAppStoreJSONBody(raw string) (map[string]any, error) {
	body, err := parseAppleAppStoreBody(raw)
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

func normalizeApplePlatform(raw string) string {
	value := strings.ToUpper(strings.TrimSpace(raw))
	switch value {
	case "IOS", "MAC_OS", "TV_OS", "VISION_OS":
		return value
	default:
		return ""
	}
}

func normalizeAppleLocale(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	return value
}

func parseAppleAnyString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		raw, _ := json.Marshal(typed)
		return string(raw)
	}
}

func loadAppleAppStoreMetadataBundle(metadataDir string) (map[string]map[string]any, map[string]map[string]any, map[string]any, error) {
	metadataDir = strings.TrimSpace(metadataDir)
	if metadataDir == "" {
		metadataDir = "appstore"
	}
	info, err := os.Stat(metadataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil, nil
		}
		return nil, nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, nil, fmt.Errorf("metadata dir is not a directory: %s", metadataDir)
	}
	appInfo := map[string]map[string]any{}
	versionInfo := map[string]map[string]any{}
	versionMeta := map[string]any{}
	for _, folder := range []string{"app-info", "version"} {
		dirPath := filepath.Join(metadataDir, folder)
		if stat, err := os.Stat(dirPath); err == nil && stat.IsDir() {
			entries, err := os.ReadDir(dirPath)
			if err != nil {
				return nil, nil, nil, err
			}
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
					continue
				}
				path := filepath.Join(dirPath, entry.Name())
				raw, err := readLocalFile(path)
				if err != nil {
					return nil, nil, nil, err
				}
				payload := map[string]any{}
				if err := json.Unmarshal(raw, &payload); err != nil {
					return nil, nil, nil, fmt.Errorf("parse %s: %w", path, err)
				}
				locale := strings.TrimSpace(parseAppleAnyString(payload["locale"]))
				if locale == "" {
					locale = strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
				}
				delete(payload, "locale")
				switch folder {
				case "app-info":
					appInfo[locale] = payload
				case "version":
					versionInfo[locale] = payload
				}
			}
		}
	}
	versionMetaPath := filepath.Join(metadataDir, "version.json")
	if _, err := os.Stat(versionMetaPath); err == nil {
		raw, err := readLocalFile(versionMetaPath)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := json.Unmarshal(raw, &versionMeta); err != nil {
			return nil, nil, nil, fmt.Errorf("parse %s: %w", versionMetaPath, err)
		}
	}
	if len(appInfo) == 0 {
		appInfo = nil
	}
	if len(versionInfo) == 0 {
		versionInfo = nil
	}
	if len(versionMeta) == 0 {
		versionMeta = nil
	}
	return appInfo, versionInfo, versionMeta, nil
}
