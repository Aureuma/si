package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAppleAppStoreE2E_AppCreateAllowPartial(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	var bundleCreateCalls atomic.Int64
	var appCreateCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/bundleIds" && r.Method == http.MethodGet:
			if got := r.URL.Query().Get("filter[identifier]"); got != "com.acme.partial" {
				t.Fatalf("unexpected bundle filter: %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		case r.URL.Path == "/v1/bundleIds" && r.Method == http.MethodPost:
			bundleCreateCalls.Add(1)
			if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
				t.Fatalf("missing bearer token: %q", got)
			}
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			data, _ := payload["data"].(map[string]any)
			attrs, _ := data["attributes"].(map[string]any)
			if attrs["identifier"] != "com.acme.partial" {
				t.Fatalf("unexpected identifier payload: %#v", payload)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"id": "bundle-1", "type": "bundleIds"},
			})
		case r.URL.Path == "/v1/apps" && r.Method == http.MethodGet:
			if got := r.URL.Query().Get("filter[bundleId]"); got != "com.acme.partial" {
				t.Fatalf("unexpected app bundle filter: %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		case r.URL.Path == "/v1/apps" && r.Method == http.MethodPost:
			appCreateCalls.Add(1)
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":[{"status":"403","code":"FORBIDDEN","title":"Forbidden","detail":"app creation disabled"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	keyFile := writeAppleAppStoreTestKeyFile(t)
	stdout, stderr, err := runSICommand(t, map[string]string{},
		"apple", "appstore", "app", "create",
		"--base-url", server.URL,
		"--bundle-id", "com.acme.partial",
		"--bundle-name", "Acme Partial",
		"--app-name", "Acme App",
		"--sku", "ACME-PARTIAL-001",
		"--primary-locale", "en-US",
		"--issuer-id", "issuer-1",
		"--key-id", "key-1",
		"--private-key-file", keyFile,
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if created, _ := payload["bundle_created"].(bool); !created {
		t.Fatalf("expected bundle_created=true: %#v", payload)
	}
	if created, _ := payload["app_created"].(bool); created {
		t.Fatalf("expected app_created=false due partial flow: %#v", payload)
	}
	if strings.TrimSpace(stringifyAny(payload["app_create_error"])) == "" {
		t.Fatalf("expected app_create_error in partial flow: %#v", payload)
	}
	if bundleCreateCalls.Load() != 1 {
		t.Fatalf("expected one bundle create call, got %d", bundleCreateCalls.Load())
	}
	if appCreateCalls.Load() != 1 {
		t.Fatalf("expected one app create call, got %d", appCreateCalls.Load())
	}
}

func TestAppleAppStoreE2E_ListingUpdateCreatesVersionAndLocalizations(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	var appInfoLocalizationCreateCalls atomic.Int64
	var versionCreateCalls atomic.Int64
	var versionLocalizationCreateCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/apps" && r.Method == http.MethodGet:
			if got := r.URL.Query().Get("filter[bundleId]"); got != "com.acme.listing" {
				t.Fatalf("unexpected app bundle filter: %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"id": "app-1", "type": "apps"}},
			})
		case r.URL.Path == "/v1/apps/app-1/appInfos" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"id": "app-info-1", "type": "appInfos"}},
			})
		case r.URL.Path == "/v1/appInfos/app-info-1/appInfoLocalizations" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		case r.URL.Path == "/v1/appInfoLocalizations" && r.Method == http.MethodPost:
			appInfoLocalizationCreateCalls.Add(1)
			raw, _ := io.ReadAll(r.Body)
			body := string(raw)
			if !strings.Contains(body, `"locale":"en-US"`) {
				t.Fatalf("missing locale in app-info localization payload: %s", body)
			}
			if !strings.Contains(body, `"name":"Acme Listing"`) {
				t.Fatalf("missing name in app-info localization payload: %s", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"id": "app-info-loc-1", "type": "appInfoLocalizations"},
			})
		case r.URL.Path == "/v1/apps/app-1/appStoreVersions" && r.Method == http.MethodGet:
			if got := r.URL.Query().Get("filter[versionString]"); got != "1.2.3" {
				t.Fatalf("unexpected version filter: %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		case r.URL.Path == "/v1/appStoreVersions" && r.Method == http.MethodPost:
			versionCreateCalls.Add(1)
			raw, _ := io.ReadAll(r.Body)
			body := string(raw)
			if !strings.Contains(body, `"versionString":"1.2.3"`) {
				t.Fatalf("missing versionString payload: %s", body)
			}
			if !strings.Contains(body, `"platform":"IOS"`) {
				t.Fatalf("missing platform payload: %s", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"id": "version-1", "type": "appStoreVersions"},
			})
		case r.URL.Path == "/v1/appStoreVersions/version-1/appStoreVersionLocalizations" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		case r.URL.Path == "/v1/appStoreVersionLocalizations" && r.Method == http.MethodPost:
			versionLocalizationCreateCalls.Add(1)
			raw, _ := io.ReadAll(r.Body)
			body := string(raw)
			if !strings.Contains(body, `"locale":"en-US"`) {
				t.Fatalf("missing locale in version localization payload: %s", body)
			}
			if !strings.Contains(body, `"description":"Long description"`) {
				t.Fatalf("missing description payload: %s", body)
			}
			if !strings.Contains(body, `"whatsNew":"Bug fixes"`) {
				t.Fatalf("missing whatsNew payload: %s", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"id": "version-loc-1", "type": "appStoreVersionLocalizations"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	keyFile := writeAppleAppStoreTestKeyFile(t)
	stdout, stderr, err := runSICommand(t, map[string]string{},
		"apple", "appstore", "listing", "update",
		"--base-url", server.URL,
		"--bundle-id", "com.acme.listing",
		"--locale", "en-US",
		"--version", "1.2.3",
		"--create-version",
		"--name", "Acme Listing",
		"--description", "Long description",
		"--whats-new", "Bug fixes",
		"--issuer-id", "issuer-1",
		"--key-id", "key-1",
		"--private-key-file", keyFile,
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if updated, _ := payload["app_info_updated"].(bool); !updated {
		t.Fatalf("expected app_info_updated=true: %#v", payload)
	}
	if updated, _ := payload["version_info_updated"].(bool); !updated {
		t.Fatalf("expected version_info_updated=true: %#v", payload)
	}
	if appInfoLocalizationCreateCalls.Load() != 1 {
		t.Fatalf("expected one appInfo localization create call, got %d", appInfoLocalizationCreateCalls.Load())
	}
	if versionCreateCalls.Load() != 1 {
		t.Fatalf("expected one version create call, got %d", versionCreateCalls.Load())
	}
	if versionLocalizationCreateCalls.Load() != 1 {
		t.Fatalf("expected one version localization create call, got %d", versionLocalizationCreateCalls.Load())
	}
}

func writeAppleAppStoreTestKeyFile(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal ecdsa key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	path := filepath.Join(t.TempDir(), "AuthKey_TEST.p8")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("write private key file: %v", err)
	}
	return path
}
