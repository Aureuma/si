package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type githubProjectGraphQLPayload struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

func TestGitHubE2E_ProjectList(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer oauth-token-123" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		var payload githubProjectGraphQLPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode graphql payload: %v", err)
		}
		if !strings.Contains(payload.Query, "projectsV2") {
			t.Fatalf("unexpected query: %s", payload.Query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"organization": map[string]any{
					"projectsV2": map[string]any{
						"nodes": []map[string]any{
							{"id": "PVT_1", "number": 7, "title": "Roadmap", "public": true, "closed": false, "url": "https://github.com/orgs/Aureuma/projects/7"},
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GITHUB_TEST_OAUTH_ACCESS_TOKEN": "oauth-token-123",
	}, "github", "project", "list", "Aureuma", "--account", "test", "--auth-mode", "oauth", "--base-url", server.URL, "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if payload["organization"] != "Aureuma" {
		t.Fatalf("unexpected organization payload: %#v", payload)
	}
	if int(payload["count"].(float64)) != 1 {
		t.Fatalf("unexpected count payload: %#v", payload)
	}
}

func TestGitHubE2E_ProjectListGraphQLErrorFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"organization": map[string]any{
					"projectsV2": map[string]any{"nodes": []any{nil}},
				},
			},
			"errors": []map[string]any{
				{"message": "Resource not accessible by personal access token", "type": "FORBIDDEN"},
			},
		})
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GITHUB_TEST_OAUTH_ACCESS_TOKEN": "oauth-token-123",
	}, "github", "project", "list", "Aureuma", "--account", "test", "--auth-mode", "oauth", "--base-url", server.URL, "--json")
	if err == nil {
		t.Fatalf("expected command failure when graphql errors are returned\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "graphql returned errors") {
		t.Fatalf("expected stderr to include graphql error message, got: %s", stderr)
	}
	if !strings.Contains(stdout, "Resource not accessible by personal access token") {
		t.Fatalf("expected stdout JSON payload to include error details, got: %s", stdout)
	}
}

func TestGitHubE2E_ProjectItemAddIssue(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			http.NotFound(w, r)
			return
		}
		var payload githubProjectGraphQLPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode graphql payload: %v", err)
		}
		switch {
		case strings.Contains(payload.Query, "projectV2(number:$number)"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"organization": map[string]any{
						"projectV2": map[string]any{"id": "PVT_proj_7", "number": 7},
					},
				},
			})
		case strings.Contains(payload.Query, "repository(owner:$owner, name:$repo)"):
			vars := payload.Variables
			if vars["owner"] != "acme" || vars["repo"] != "sandbox" || int(vars["number"].(float64)) != 42 {
				t.Fatalf("unexpected issue lookup vars: %#v", vars)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"repository": map[string]any{
						"issue": map[string]any{"id": "I_42", "number": 42, "title": "Issue 42"},
					},
				},
			})
		case strings.Contains(payload.Query, "addProjectV2ItemById"):
			vars := payload.Variables
			if vars["projectId"] != "PVT_proj_7" {
				t.Fatalf("unexpected project id: %#v", vars)
			}
			if vars["contentId"] != "I_42" {
				t.Fatalf("unexpected content id: %#v", vars)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"addProjectV2ItemById": map[string]any{
						"item": map[string]any{"id": "PVTI_1", "type": "ISSUE"},
					},
				},
			})
		default:
			t.Fatalf("unexpected query: %s", payload.Query)
		}
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GITHUB_TEST_OAUTH_ACCESS_TOKEN": "oauth-token-123",
	}, "github", "project", "item-add", "Aureuma/7", "--repo", "acme/sandbox", "--issue", "42", "--account", "test", "--auth-mode", "oauth", "--base-url", server.URL, "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	item, ok := payload["item"].(map[string]any)
	if !ok || item["id"] != "PVTI_1" {
		t.Fatalf("unexpected item payload: %#v", payload)
	}
}

func TestGitHubE2E_ProjectItemSetSingleSelectByName(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			http.NotFound(w, r)
			return
		}
		var payload githubProjectGraphQLPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode graphql payload: %v", err)
		}
		switch {
		case strings.Contains(payload.Query, "projectV2(number:$number)"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"organization": map[string]any{
						"projectV2": map[string]any{"id": "PVT_proj_7", "number": 7},
					},
				},
			})
		case strings.Contains(payload.Query, "fields(first:100)"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"node": map[string]any{
						"fields": map[string]any{
							"nodes": []map[string]any{
								{"id": "F_STATUS", "name": "Status", "dataType": "SINGLE_SELECT", "options": []map[string]any{{"id": "OPT_TODO", "name": "Todo"}, {"id": "OPT_PROGRESS", "name": "In Progress"}}},
							},
						},
					},
				},
			})
		case strings.Contains(payload.Query, "updateProjectV2ItemFieldValue"):
			vars := payload.Variables
			if vars["projectId"] != "PVT_proj_7" || vars["itemId"] != "PVTI_1" || vars["fieldId"] != "F_STATUS" {
				t.Fatalf("unexpected update vars: %#v", vars)
			}
			value, ok := vars["value"].(map[string]any)
			if !ok {
				t.Fatalf("missing value payload: %#v", vars)
			}
			if value["singleSelectOptionId"] != "OPT_PROGRESS" {
				t.Fatalf("unexpected option id payload: %#v", value)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"updateProjectV2ItemFieldValue": map[string]any{
						"projectV2Item": map[string]any{"id": "PVTI_1"},
					},
				},
			})
		default:
			t.Fatalf("unexpected query: %s", payload.Query)
		}
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GITHUB_TEST_OAUTH_ACCESS_TOKEN": "oauth-token-123",
	}, "github", "project", "item-set", "Aureuma/7", "PVTI_1", "--field", "Status", "--single-select", "In Progress", "--account", "test", "--auth-mode", "oauth", "--base-url", server.URL, "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	if payload["field_id"] != "F_STATUS" {
		t.Fatalf("unexpected field id payload: %#v", payload)
	}
}

func TestGitHubE2E_ProjectUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e-style subprocess test in short mode")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			http.NotFound(w, r)
			return
		}
		var payload githubProjectGraphQLPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode graphql payload: %v", err)
		}
		switch {
		case strings.Contains(payload.Query, "projectV2(number:$number)"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"organization": map[string]any{
						"projectV2": map[string]any{"id": "PVT_proj_7", "number": 7},
					},
				},
			})
		case strings.Contains(payload.Query, "updateProjectV2(input:$input)"):
			input, ok := payload.Variables["input"].(map[string]any)
			if !ok {
				t.Fatalf("missing input payload: %#v", payload.Variables)
			}
			if input["projectId"] != "PVT_proj_7" {
				t.Fatalf("unexpected project id: %#v", input)
			}
			if input["title"] != "Roadmap" {
				t.Fatalf("unexpected title: %#v", input)
			}
			if input["shortDescription"] != "Delivery plan" {
				t.Fatalf("unexpected shortDescription: %#v", input)
			}
			if input["public"] != true {
				t.Fatalf("unexpected public flag: %#v", input)
			}
			if input["closed"] != false {
				t.Fatalf("unexpected closed flag: %#v", input)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"updateProjectV2": map[string]any{
						"projectV2": map[string]any{
							"id":               "PVT_proj_7",
							"number":           7,
							"title":            "Roadmap",
							"shortDescription": "Delivery plan",
							"public":           true,
							"closed":           false,
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected query: %s", payload.Query)
		}
	}))
	defer server.Close()

	stdout, stderr, err := runSICommand(t, map[string]string{
		"GITHUB_TEST_OAUTH_ACCESS_TOKEN": "oauth-token-123",
	}, "github", "project", "update", "Aureuma/7", "--title", "Roadmap", "--description", "Delivery plan", "--public", "true", "--closed", "false", "--account", "test", "--auth-mode", "oauth", "--base-url", server.URL, "--json")
	if err != nil {
		t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json output parse failed: %v\nstdout=%s", err, stdout)
	}
	project, ok := payload["project"].(map[string]any)
	if !ok {
		t.Fatalf("missing project payload: %#v", payload)
	}
	if project["title"] != "Roadmap" {
		t.Fatalf("unexpected project title payload: %#v", project)
	}
}
