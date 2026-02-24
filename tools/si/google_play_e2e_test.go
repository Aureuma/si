package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGooglePlayE2E_ReleaseUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	var tokenCalls atomic.Int64
	var uploadCalls atomic.Int64
	var trackCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			tokenCalls.Add(1)
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			if !strings.Contains(string(body), "grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Ajwt-bearer") {
				t.Fatalf("unexpected token request body: %s", string(body))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ya29.e2e", "expires_in": 3600, "token_type": "Bearer"})
		case r.URL.Path == "/androidpublisher/v3/applications/com.acme.app/edits" && r.Method == http.MethodPost:
			if got := r.Header.Get("Authorization"); got != "Bearer ya29.e2e" {
				t.Fatalf("unexpected auth header for edits insert: %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "edit-1"})
		case r.URL.Path == "/upload/androidpublisher/v3/applications/com.acme.app/edits/edit-1/bundles" && r.Method == http.MethodPost:
			uploadCalls.Add(1)
			if got := r.URL.Query().Get("uploadType"); got != "media" {
				t.Fatalf("unexpected uploadType: %q", got)
			}
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			if string(body) != "bundle-bytes" {
				t.Fatalf("unexpected bundle body: %q", string(body))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"versionCode": "123"})
		case r.URL.Path == "/androidpublisher/v3/applications/com.acme.app/edits/edit-1/tracks/internal" && r.Method == http.MethodPut:
			trackCalls.Add(1)
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			releases, _ := payload["releases"].([]any)
			if len(releases) != 1 {
				t.Fatalf("unexpected releases payload: %#v", payload)
			}
			release, _ := releases[0].(map[string]any)
			if release["status"] != "completed" {
				t.Fatalf("unexpected status: %#v", release)
			}
			codes, _ := release["versionCodes"].([]any)
			if len(codes) != 1 || codes[0] != "123" {
				t.Fatalf("unexpected versionCodes: %#v", release)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"track": "internal", "releases": releases})
		case r.URL.Path == "/androidpublisher/v3/applications/com.acme.app/edits/edit-1:commit" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "edit-1"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	aabPath := filepath.Join(t.TempDir(), "app-release.aab")
	if err := os.WriteFile(aabPath, []byte("bundle-bytes"), 0o600); err != nil {
		t.Fatalf("write aab: %v", err)
	}

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GOOGLE_TEST_PLAY_SERVICE_ACCOUNT_JSON": testGooglePlayServiceAccountJSON(t, server.URL+"/token"),
	},
		"google", "play", "release", "upload",
		"--account", "test",
		"--package", "com.acme.app",
		"--base-url", server.URL,
		"--upload-base-url", server.URL,
		"--aab", aabPath,
		"--track", "internal",
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if _, ok := payload["track_update"].(map[string]any); !ok {
		t.Fatalf("missing track_update payload: %#v", payload)
	}
	if tokenCalls.Load() < 1 {
		t.Fatalf("expected token endpoint call")
	}
	if uploadCalls.Load() != 1 {
		t.Fatalf("expected one bundle upload call, got %d", uploadCalls.Load())
	}
	if trackCalls.Load() != 1 {
		t.Fatalf("expected one track update call, got %d", trackCalls.Load())
	}
}

func TestGooglePlayE2E_ReleaseUploadChangesNotSentForReview(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	var trackCalls atomic.Int64
	var commitCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ya29.e2e", "expires_in": 3600, "token_type": "Bearer"})
		case r.URL.Path == "/androidpublisher/v3/applications/com.acme.app/edits" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "edit-1"})
		case r.URL.Path == "/androidpublisher/v3/applications/com.acme.app/edits/edit-1/tracks/production" && r.Method == http.MethodPut:
			trackCalls.Add(1)
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["track"] != "production" {
				t.Fatalf("unexpected track payload: %#v", payload)
			}
			releases, _ := payload["releases"].([]any)
			if len(releases) != 1 {
				t.Fatalf("unexpected releases payload: %#v", payload)
			}
			release, _ := releases[0].(map[string]any)
			if release["status"] != "inProgress" {
				t.Fatalf("unexpected release status: %#v", release)
			}
			if got, _ := release["userFraction"].(float64); got != 0.25 {
				t.Fatalf("unexpected userFraction: %#v", release)
			}
			if release["name"] != "Rollout 1" {
				t.Fatalf("unexpected release name: %#v", release)
			}
			codes, _ := release["versionCodes"].([]any)
			if len(codes) != 2 || codes[0] != "200" || codes[1] != "201" {
				t.Fatalf("unexpected versionCodes: %#v", release)
			}
			notes, _ := release["releaseNotes"].([]any)
			if len(notes) != 2 {
				t.Fatalf("unexpected releaseNotes: %#v", release)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"track": "production", "releases": releases})
		case r.URL.Path == "/androidpublisher/v3/applications/com.acme.app/edits/edit-1:commit" && r.Method == http.MethodPost:
			commitCalls.Add(1)
			if got := r.URL.Query().Get("changesNotSentForReview"); got != "true" {
				t.Fatalf("expected changesNotSentForReview=true, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "edit-1", "committed": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GOOGLE_TEST_PLAY_SERVICE_ACCOUNT_JSON": testGooglePlayServiceAccountJSON(t, server.URL+"/token"),
	},
		"google", "play", "release", "upload",
		"--account", "test",
		"--package", "com.acme.app",
		"--base-url", server.URL,
		"--track", "production",
		"--status", "inProgress",
		"--user-fraction", "0.25",
		"--release-name", "Rollout 1",
		"--release-note", "en-US=Hello",
		"--release-note", "fr-FR=Bonjour",
		"--version-code", "200",
		"--version-code", "201,200",
		"--changes-not-sent-for-review",
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if _, ok := payload["track_update"].(map[string]any); !ok {
		t.Fatalf("missing track_update payload: %#v", payload)
	}
	editResult, _ := payload["edit_result"].(map[string]any)
	data, _ := editResult["data"].(map[string]any)
	if data["committed"] != true {
		t.Fatalf("unexpected edit_result payload: %#v", payload)
	}
	if trackCalls.Load() != 1 {
		t.Fatalf("expected one track update call, got %d", trackCalls.Load())
	}
	if commitCalls.Load() != 1 {
		t.Fatalf("expected one commit call, got %d", commitCalls.Load())
	}
}

func TestGooglePlayE2E_ListingUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	var listingPatchCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ya29.e2e", "expires_in": 3600, "token_type": "Bearer"})
		case r.URL.Path == "/androidpublisher/v3/applications/com.acme.app/edits" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "edit-1"})
		case r.URL.Path == "/androidpublisher/v3/applications/com.acme.app/edits/edit-1/listings/en-US" && r.Method == http.MethodPatch:
			listingPatchCalls.Add(1)
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["title"] != "Acme App" {
				t.Fatalf("unexpected title payload: %#v", payload)
			}
			if payload["shortDescription"] != "Short text" {
				t.Fatalf("unexpected short description payload: %#v", payload)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"language": "en-US"})
		case r.URL.Path == "/androidpublisher/v3/applications/com.acme.app/edits/edit-1:commit" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "edit-1"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GOOGLE_TEST_PLAY_SERVICE_ACCOUNT_JSON": testGooglePlayServiceAccountJSON(t, server.URL+"/token"),
	},
		"google", "play", "listing", "update",
		"--account", "test",
		"--package", "com.acme.app",
		"--language", "en-US",
		"--title", "Acme App",
		"--short-description", "Short text",
		"--base-url", server.URL,
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if status, _ := payload["status"].(string); status == "" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if listingPatchCalls.Load() != 1 {
		t.Fatalf("expected one listing patch call, got %d", listingPatchCalls.Load())
	}
}

func TestGooglePlayE2E_ListingUpdateBodyFileAndChangesNotSentForReview(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	var listingPatchCalls atomic.Int64
	var commitCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ya29.e2e", "expires_in": 3600, "token_type": "Bearer"})
		case r.URL.Path == "/androidpublisher/v3/applications/com.acme.app/edits" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "edit-1"})
		case r.URL.Path == "/androidpublisher/v3/applications/com.acme.app/edits/edit-1/listings/en-US" && r.Method == http.MethodPatch:
			listingPatchCalls.Add(1)
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["title"] != "CLI Title" {
				t.Fatalf("expected CLI title override, got %#v", payload)
			}
			if payload["shortDescription"] != "Body short" {
				t.Fatalf("unexpected shortDescription payload: %#v", payload)
			}
			if payload["fullDescription"] != "CLI full description" {
				t.Fatalf("expected CLI fullDescription override, got %#v", payload)
			}
			if payload["video"] != "https://example.com/video" {
				t.Fatalf("unexpected video payload: %#v", payload)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"language": "en-US", "updated": true})
		case r.URL.Path == "/androidpublisher/v3/applications/com.acme.app/edits/edit-1:commit" && r.Method == http.MethodPost:
			commitCalls.Add(1)
			if got := r.URL.Query().Get("changesNotSentForReview"); got != "true" {
				t.Fatalf("expected changesNotSentForReview=true, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "edit-1", "committed": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	bodyPath := filepath.Join(t.TempDir(), "listing.json")
	if err := os.WriteFile(bodyPath, []byte(`{"title":"Body Title","shortDescription":"Body short","fullDescription":"Body full","video":"https://example.com/video"}`), 0o600); err != nil {
		t.Fatalf("write listing body: %v", err)
	}

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GOOGLE_TEST_PLAY_SERVICE_ACCOUNT_JSON": testGooglePlayServiceAccountJSON(t, server.URL+"/token"),
	},
		"google", "play", "listing", "update",
		"--account", "test",
		"--package", "com.acme.app",
		"--language", "en-US",
		"--body", "@"+bodyPath,
		"--title", "CLI Title",
		"--full-description", "CLI full description",
		"--changes-not-sent-for-review",
		"--base-url", server.URL,
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	data, _ := payload["data"].(map[string]any)
	if data["committed"] != true {
		t.Fatalf("unexpected commit payload: %#v", payload)
	}
	if listingPatchCalls.Load() != 1 {
		t.Fatalf("expected one listing patch call, got %d", listingPatchCalls.Load())
	}
	if commitCalls.Load() != 1 {
		t.Fatalf("expected one commit call, got %d", commitCalls.Load())
	}
}

func TestGooglePlayE2E_CustomAppCreate(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ya29.e2e", "expires_in": 3600, "token_type": "Bearer"})
		case r.URL.Path == "/playcustomapp/v1/accounts/123/customApps" && r.Method == http.MethodPost:
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["title"] != "Acme Custom" {
				t.Fatalf("unexpected title payload: %#v", payload)
			}
			if payload["languageCode"] != "en-US" {
				t.Fatalf("unexpected language payload: %#v", payload)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"packageName": "com.acme.custom"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GOOGLE_TEST_PLAY_SERVICE_ACCOUNT_JSON": testGooglePlayServiceAccountJSON(t, server.URL+"/token"),
	},
		"google", "play", "app", "create",
		"--account", "test",
		"--developer-account", "123",
		"--title", "Acme Custom",
		"--language", "en-US",
		"--custom-app-base-url", server.URL,
		"--base-url", server.URL,
		"--json",
	)
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	data, _ := payload["data"].(map[string]any)
	if data["packageName"] != "com.acme.custom" {
		t.Fatalf("unexpected custom app payload: %#v", payload)
	}
}
