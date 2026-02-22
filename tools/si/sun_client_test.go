package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewSunClientValidation(t *testing.T) {
	if _, err := newSunClient("", "token", time.Second); err == nil {
		t.Fatalf("expected base url validation error")
	}
	if _, err := newSunClient("http://127.0.0.1:8080", "", time.Second); err == nil {
		t.Fatalf("expected token validation error")
	}
	if _, err := newSunClient("http://example.com", "token", time.Second); err == nil {
		t.Fatalf("expected non-local insecure http URL to be rejected")
	}
	t.Setenv("SI_SUN_ALLOW_INSECURE_HTTP", "1")
	if _, err := newSunClient("http://example.com", "token", time.Second); err != nil {
		t.Fatalf("expected insecure http override to permit URL, got: %v", err)
	}
}

func TestSunClientRoundTripMethods(t *testing.T) {
	payloadBytes := []byte(`{"ok":true}`)
	var revokedID string
	var gatewayIndexPayload []byte
	var gatewayShardPayload []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/readyz" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer token123" {
			http.Error(w, `{"error":"missing auth"}`, http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/v1/integrations/registries/team":
			var body struct {
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			gatewayIndexPayload = append([]byte{}, body.Payload...)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"object":   map[string]any{"latest_revision": 7},
					"revision": map[string]any{"revision": 7},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/integrations/registries/team":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"registry": "team",
				"index":    json.RawMessage(gatewayIndexPayload),
			})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/integrations/registries/team/shards/acme--03":
			var body struct {
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			gatewayShardPayload = append([]byte{}, body.Payload...)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"object":   map[string]any{"latest_revision": 8},
					"revision": map[string]any{"revision": 8},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/integrations/registries/team/shards/acme--03":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"registry": "team",
				"shard":    "acme--03",
				"payload":  json.RawMessage(gatewayShardPayload),
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/auth/whoami":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"account_id":   "acc-1",
				"account_slug": "acme",
				"token_id":     "tok-1",
				"scopes":       []string{"objects:read", "objects:write"},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/objects") && !strings.HasSuffix(r.URL.Path, "/payload"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{{
					"kind":            sunCodexProfileBundleKind,
					"name":            "cadma",
					"latest_revision": 3,
					"checksum":        "abc",
					"content_type":    "application/json",
					"size_bytes":      42,
					"created_at":      "2026-01-01T00:00:00Z",
					"updated_at":      "2026-01-02T00:00:00Z",
				}},
			})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/objects/"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			payload := strings.TrimSpace(formatAny(body["payload_base64"]))
			if payload == "" {
				http.Error(w, `{"error":"missing payload"}`, http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"object":   map[string]any{"latest_revision": 4},
					"revision": map[string]any{"revision": 4},
				},
			})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/payload"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(payloadBytes)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/tokens"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{{
					"token_id":   "tok-2",
					"label":      "device",
					"scopes":     []string{"objects:read"},
					"created_at": "2026-02-22T00:00:00Z",
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/tokens":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"account":  map[string]any{"id": "acc-1", "slug": "acme"},
				"token":    "hlia_new_token",
				"token_id": "new-token",
				"label":    "new",
				"scopes":   []string{"objects:read"},
			})
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/tokens/") && strings.HasSuffix(r.URL.Path, "/revoke"):
			parts := strings.Split(r.URL.Path, "/")
			revokedID = parts[len(parts)-2]
			_ = json.NewEncoder(w).Encode(map[string]any{"revoked": true})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/audit"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{{
					"id":         1,
					"action":     "put_object",
					"kind":       sunCodexProfileBundleKind,
					"name":       "cadma",
					"created_at": "2026-02-22T00:00:00Z",
				}},
			})
		default:
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := newSunClient(server.URL, "token123", 3*time.Second)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ctx := context.Background()
	if err := client.ready(ctx); err != nil {
		t.Fatalf("ready: %v", err)
	}
	who, err := client.whoAmI(ctx)
	if err != nil {
		t.Fatalf("whoami: %v", err)
	}
	if who.AccountSlug != "acme" {
		t.Fatalf("unexpected account slug: %s", who.AccountSlug)
	}

	items, err := client.listObjects(ctx, sunCodexProfileBundleKind, "", 10)
	if err != nil {
		t.Fatalf("list objects: %v", err)
	}
	if len(items) != 1 || items[0].Name != "cadma" {
		t.Fatalf("unexpected list result: %+v", items)
	}

	put, err := client.putObject(ctx, sunCodexProfileBundleKind, "cadma", []byte(`{"x":1}`), "application/json", nil, nil)
	if err != nil {
		t.Fatalf("put object: %v", err)
	}
	if put.Result.Revision.Revision != 4 {
		t.Fatalf("unexpected revision: %d", put.Result.Revision.Revision)
	}

	gotPayload, err := client.getPayload(ctx, sunCodexProfileBundleKind, "cadma")
	if err != nil {
		t.Fatalf("get payload: %v", err)
	}
	if base64.StdEncoding.EncodeToString(gotPayload) != base64.StdEncoding.EncodeToString(payloadBytes) {
		t.Fatalf("unexpected payload: %s", string(gotPayload))
	}

	tokens, err := client.listTokens(ctx, true, 10)
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if len(tokens) != 1 || tokens[0].TokenID != "tok-2" {
		t.Fatalf("unexpected tokens response: %+v", tokens)
	}

	issued, err := client.createToken(ctx, "new", []string{"objects:read"}, 1)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if issued.TokenID != "new-token" {
		t.Fatalf("unexpected issued token: %+v", issued)
	}

	if err := client.revokeToken(ctx, "new-token"); err != nil {
		t.Fatalf("revoke token: %v", err)
	}
	if revokedID != "new-token" {
		t.Fatalf("unexpected revoked token id: %s", revokedID)
	}

	auditRows, err := client.listAuditEvents(ctx, "", sunCodexProfileBundleKind, "", 10)
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	if len(auditRows) != 1 || auditRows[0].Kind != sunCodexProfileBundleKind {
		t.Fatalf("unexpected audit rows: %+v", auditRows)
	}

	indexPayload := []byte(`{"registry":"team","shards":[{"key":"acme--03","namespace":"acme"}]}`)
	indexPut, err := client.putIntegrationRegistryIndex(ctx, "team", indexPayload, nil)
	if err != nil {
		t.Fatalf("put integration registry index: %v", err)
	}
	if indexPut.Result.Revision.Revision != 7 {
		t.Fatalf("unexpected index revision: %d", indexPut.Result.Revision.Revision)
	}
	indexBack, err := client.getIntegrationRegistryIndex(ctx, "team")
	if err != nil {
		t.Fatalf("get integration registry index: %v", err)
	}
	if strings.TrimSpace(string(indexBack)) != strings.TrimSpace(string(indexPayload)) {
		t.Fatalf("unexpected index payload: %s", string(indexBack))
	}

	shardPayload := []byte(`{"registry":"team","key":"acme--03","entries":[{"manifest":{"id":"acme/chat"}}]}`)
	shardPut, err := client.putIntegrationRegistryShard(ctx, "team", "acme--03", shardPayload, nil)
	if err != nil {
		t.Fatalf("put integration registry shard: %v", err)
	}
	if shardPut.Result.Revision.Revision != 8 {
		t.Fatalf("unexpected shard revision: %d", shardPut.Result.Revision.Revision)
	}
	shardBack, err := client.getIntegrationRegistryShard(ctx, "team", "acme--03")
	if err != nil {
		t.Fatalf("get integration registry shard: %v", err)
	}
	if strings.TrimSpace(string(shardBack)) != strings.TrimSpace(string(shardPayload)) {
		t.Fatalf("unexpected shard payload: %s", string(shardBack))
	}
}
